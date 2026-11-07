//! Keeping work between computes: cache.go.

use std::collections::HashMap;
use std::sync::{Arc, Mutex, MutexGuard, OnceLock};

use buffa::Message;
use proto::scenariosv1::{Problem, Scenario, ScenarioInputs, ScenarioResult, TaxYearScenarios};
use sha2::{Digest, Sha256};

use crate::models::ModelEntry;
use crate::resolve::{apply_changes, changes_failed, problem};
use crate::validate::{CheckEntry, SharedChecks, check_ids, check_shared, identifiers};
use crate::{Error, TaxYearResult, compute, reflect};

/// Cache keeps work between calls to its compute, so an edit recomputes
/// only what it changed. It holds the checks on each file's shared inputs,
/// and each scenario's resolved inputs and result.
///
/// A scenario is keyed by a hash of its parent's key and the scenario
/// itself: its id, name, parent and changes. The chain's root is a hash of
/// the file's year, as_of and shared inputs. Two scenarios with one key
/// have the same resolved inputs and the same result. An edit to one
/// scenario therefore recomputes that scenario and its descendants, and an
/// edit to shared, the year or as_of recomputes every scenario.
///
/// The cache keeps the entries used most recently, up to its size, and is
/// safe for concurrent use. Everything in it is a copy, so a caller may
/// change a file after computing it.
pub struct Cache {
    size: usize,
    pub(crate) no_working: bool,
    inner: Mutex<Inner>,
}

impl std::fmt::Debug for Cache {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("Cache")
            .field("size", &self.size)
            .field("no_working", &self.no_working)
            .finish_non_exhaustive()
    }
}

/// What the cache holds, each entry with the clock of its last use.
#[derive(Default)]
pub(crate) struct Inner {
    clock: u64,
    shared: HashMap<Key, (u64, Arc<SharedEntry>)>,
    scenarios: HashMap<Key, (u64, Arc<ScenarioEntry>)>,
    pub(crate) models: HashMap<Key, (u64, Arc<ModelEntry>)>,
    pub(crate) checks: HashMap<Key, (u64, Arc<CheckEntry>)>,
}

impl Inner {
    /// How many scenarios the cache holds, for the tests.
    #[cfg(test)]
    pub(crate) fn scenario_count(&self) -> usize {
        self.scenarios.len()
    }

    /// How many models' projections the cache holds, for the tests.
    #[cfg(test)]
    pub(crate) fn model_count(&self) -> usize {
        self.models.len()
    }

    /// The next tick of the clock.
    pub(crate) fn tick(&mut self) -> u64 {
        self.clock += 1;
        self.clock
    }
}

/// key is a SHA-256 hash.
pub(crate) type Key = [u8; 32];

/// sharedEntry is the work on one file's shared inputs.
#[derive(Debug)]
pub(crate) struct SharedEntry {
    /// A copy of shared.
    pub(crate) inputs: Arc<ScenarioInputs>,
    /// The identifiers under shared.
    pub(crate) identifiers: Vec<Problem>,
    /// CheckIDs.
    pub(crate) ids: Vec<Problem>,
    pub(crate) checks: SharedChecks,
}

/// scenarioEntry is one scenario's resolved inputs and result.
#[derive(Debug)]
pub(crate) struct ScenarioEntry {
    /// None when its changes don't apply.
    pub(crate) inputs: Option<Arc<ScenarioInputs>>,
    /// Its changes failing, or CheckScenarios.
    problems: Vec<Problem>,
    /// Unset until computed.
    result: OnceLock<Arc<ScenarioResult>>,
}

/// An option of [`Cache::new`]: how a cache computes.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum CacheOption {
    /// WithoutEngineWorking leaves out the rules engine's line-by-line
    /// working: the result has no working for the federal return's lines,
    /// and no question about an input the engine found missing.
    WithoutEngineWorking,
}

impl Cache {
    /// NewCache returns a cache that keeps up to size scenarios, and as
    /// many files' shared inputs. Go refuses a size under 1; a size of 0
    /// here keeps one.
    pub fn new(size: usize, opts: &[CacheOption]) -> Cache {
        let mut c = Cache {
            size: size.max(1),
            no_working: false,
            inner: Mutex::new(Inner::default()),
        };
        for o in opts {
            match o {
                CacheOption::WithoutEngineWorking => c.no_working = true,
            }
        }
        c
    }

    /// Compute is [`crate::compute`], reusing the cache's work and adding
    /// to it. The result is the same as compute's for the same file.
    ///
    /// # Errors
    ///
    /// None today: Go's is for a nil file, which a reference can't be.
    pub fn compute(&self, file: &TaxYearScenarios) -> Result<TaxYearResult, Error> {
        let r = compute::compute_with(file, self);
        self.trim();
        Ok(r)
    }

    pub(crate) fn lock(&self) -> MutexGuard<'_, Inner> {
        match self.inner.lock() {
            Ok(g) => g,
            Err(p) => p.into_inner(),
        }
    }

    /// sharedFor returns the work on a file's shared inputs, doing it when
    /// the cache doesn't hold it.
    pub(crate) fn shared_for(&self, file: &TaxYearScenarios) -> (Key, Arc<SharedEntry>) {
        let k = root_key(file);
        {
            let mut g = self.lock();
            let t = g.tick();
            if let Some((used, e)) = g.shared.get_mut(&k) {
                *used = t;
                return (k, Arc::clone(e));
            }
        }
        let inputs = Arc::new(file.shared.as_option().cloned().unwrap_or_default());
        let mut idents = Vec::new();
        if let Some(d) = reflect::to_dyn(&*inputs) {
            identifiers("shared", &d, &mut idents);
        }
        let e = Arc::new(SharedEntry {
            ids: check_ids(&inputs),
            checks: check_shared(&inputs),
            identifiers: idents,
            inputs,
        });
        let mut g = self.lock();
        let t = g.tick();
        g.shared.insert(k, (t, Arc::clone(&e)));
        (k, e)
    }

    /// scenarioFor returns a scenario's entry, resolving and checking it
    /// when the cache doesn't hold it.
    pub(crate) fn scenario_for(
        &self,
        k: Key,
        parent: &ScenarioInputs,
        s: &Scenario,
        shared: &SharedEntry,
    ) -> Arc<ScenarioEntry> {
        {
            let mut g = self.lock();
            let t = g.tick();
            if let Some((used, e)) = g.scenarios.get_mut(&k) {
                *used = t;
                return Arc::clone(e);
            }
        }
        let changes = s.changes.as_option().cloned().unwrap_or_default();
        let e = match apply_changes(parent, &changes) {
            Err(err) => ScenarioEntry {
                inputs: None,
                problems: vec![problem(
                    &format!("scenarios[{}]", s.id),
                    changes_failed(&err),
                )],
                result: OnceLock::new(),
            },
            Ok(inputs) => ScenarioEntry {
                problems: shared.checks.scenario(&inputs),
                inputs: Some(Arc::new(inputs)),
                result: OnceLock::new(),
            },
        };
        let e = Arc::new(e);
        let mut g = self.lock();
        let t = g.tick();
        g.scenarios.insert(k, (t, Arc::clone(&e)));
        e
    }

    /// computed returns a scenario's result, computing it once.
    pub(crate) fn computed(
        &self,
        file: &TaxYearScenarios,
        e: &ScenarioEntry,
        id: &str,
    ) -> Arc<ScenarioResult> {
        if let Some(r) = e.result.get() {
            return Arc::clone(r);
        }
        let mut r = ScenarioResult {
            scenario: id.to_string(),
            problems: e.problems.clone(),
            ..Default::default()
        };
        if let Some(inputs) = &e.inputs
            && r.problems.is_empty()
        {
            compute::compute_scenario(Some(self), file, inputs, &mut r);
        }
        Arc::clone(e.result.get_or_init(|| Arc::new(r)))
    }

    /// trim drops the least recently used entries past the cache's size.
    fn trim(&self) {
        let size = self.size;
        let mut g = self.lock();
        trim_map(&mut g.scenarios, size);
        trim_map(&mut g.shared, size);
        trim_map(&mut g.models, 16 * size);
        trim_map(&mut g.checks, 16 * size);
    }
}

fn trim_map<E>(m: &mut HashMap<Key, (u64, E)>, size: usize) {
    if m.len() <= size {
        return;
    }
    let mut keys: Vec<(u64, Key)> = m.iter().map(|(k, (u, _))| (*u, *k)).collect();
    keys.sort();
    let drop = keys.len() - size;
    for (_, k) in keys.into_iter().take(drop) {
        m.remove(&k);
    }
}

/// hash is the SHA-256 of a message's encoding after a prefix.
pub(crate) fn hash<M: Message>(prefix: &[u8], m: &M) -> Key {
    let mut h = Sha256::new();
    h.update(prefix);
    h.update(m.encode_to_vec());
    h.finalize().into()
}

/// The hash of prefix alone.
pub(crate) fn hash_bytes(prefix: &[u8]) -> Key {
    Sha256::digest(prefix).into()
}

/// binary.AppendVarint: v zigzag encoded as a varint.
pub(crate) fn append_varint(mut b: Vec<u8>, v: i64) -> Vec<u8> {
    let mut u = ((v << 1) ^ (v >> 63)) as u64;
    while u >= 0x80 {
        b.push((u as u8) | 0x80);
        u >>= 7;
    }
    b.push(u as u8);
    b
}

/// rootKey keys a file's shared inputs, with its year and as_of.
fn root_key(file: &TaxYearScenarios) -> Key {
    let mut prefix = append_varint(b"taxyear.shared.v1".to_vec(), i64::from(file.tax_year));
    prefix.extend_from_slice(&hash(&[], &*file.as_of));
    match file.shared.as_option() {
        Some(s) => hash(&prefix, s),
        None => hash(&prefix, &ScenarioInputs::default()),
    }
}

/// scenarioKey keys a scenario under its parent's key.
pub(crate) fn scenario_key(parent: &Key, s: &Scenario) -> Key {
    let mut prefix = b"taxyear.scenario.v1".to_vec();
    prefix.extend_from_slice(parent);
    hash(&prefix, s)
}
