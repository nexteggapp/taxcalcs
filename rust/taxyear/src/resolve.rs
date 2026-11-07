//! Each scenario's inputs down its parent chain: resolve.go.

use std::collections::HashMap;
use std::sync::Arc;

use proto::scenariosv1::{
    Problem, Scenario, ScenarioInputs, ScenarioInputsDiff, Statement, TaxYearScenarios,
};

use crate::{Error, goquote, prune, reflect};

/// Resolved is one scenario's inputs: shared with each diff down its parent
/// chain applied in turn, Your plan's first.
#[derive(Clone, Debug)]
pub struct Resolved {
    scenario: Scenario,
    inputs: Arc<ScenarioInputs>,
}

impl Resolved {
    /// Scenario is the scenario.
    pub fn scenario(&self) -> &Scenario {
        &self.scenario
    }

    /// Inputs is its resolved inputs.
    pub fn inputs(&self) -> &ScenarioInputs {
        &self.inputs
    }
}

/// Resolve applies each scenario's changes down its parent chain with
/// protodiffgen's apply, which validates each diff against its parent
/// before applying it. Your plan must come first, with id "plan" and no
/// parent, and be the only scenario with none; every other parent must be
/// an earlier scenario, so no chain loops. A scenario whose diff fails, or
/// whose parent failed, is reported as a problem and left out; the others
/// still resolve.
pub fn resolve(file: &TaxYearScenarios) -> (Vec<Resolved>, HashMap<String, Vec<Problem>>) {
    let mut problems: HashMap<String, Vec<Problem>> = HashMap::new();
    let mut by_id: HashMap<String, Arc<ScenarioInputs>> = HashMap::new();
    let mut out = Vec::new();
    let shared = Arc::new(file.shared.as_option().cloned().unwrap_or_default());
    for (i, s) in file.scenarios.iter().enumerate() {
        let path = format!("scenarios[{}]", s.id);
        let mut fail = |text: String| {
            problems
                .entry(s.id.clone())
                .or_default()
                .push(problem(&path, text));
        };
        let text = chain_problem(i, s, |id| by_id.contains_key(id));
        if !text.is_empty() {
            fail(text);
            continue;
        }
        let parent = if i > 0 {
            by_id.get(&s.parent).cloned().unwrap_or_default()
        } else {
            Arc::clone(&shared)
        };
        let changes = s.changes.as_option().cloned().unwrap_or_default();
        match apply_changes(&parent, &changes) {
            Ok(inputs) => {
                let inputs = Arc::new(inputs);
                by_id.insert(s.id.clone(), Arc::clone(&inputs));
                out.push(Resolved {
                    scenario: s.clone(),
                    inputs,
                });
            }
            Err(e) => fail(changes_failed(&e)),
        }
    }
    (out, problems)
}

/// chainProblem is why the scenario at index i can't resolve before its
/// changes are applied, or "". resolved reports whether an earlier
/// scenario resolved.
pub(crate) fn chain_problem(i: usize, s: &Scenario, resolved: impl Fn(&str) -> bool) -> String {
    if i == 0 && (s.id != "plan" || !s.parent.is_empty()) {
        return format!(
            "The first scenario must be Your plan, with id plan and no parent; it is {} with parent {}.",
            goquote::quote(&s.id),
            goquote::quote(&s.parent)
        );
    }
    if i > 0 && s.parent.is_empty() {
        return "Only Your plan has no parent.".to_string();
    }
    if i > 0 && !resolved(&s.parent) {
        return format!(
            "Its parent, {}, is not an earlier scenario that resolved.",
            goquote::quote(&s.parent)
        );
    }
    if resolved(&s.id) {
        return "Another scenario has the same id.".to_string();
    }
    String::new()
}

/// ApplyChanges applies one scenario's changes to its parent's inputs, as
/// [`resolve`] does, and returns apply's error when they don't apply.
/// apply validates the diff before applying it, so it is not validated
/// separately. Neither argument is changed.
///
/// apply copies and checks the whole of the message it is given, and the
/// parent is mostly evidence the changes don't touch. So apply is given
/// only the part of the parent the changes touch (see prune), and the rest
/// is put back afterwards.
///
/// # Errors
///
/// [`Error::Apply`] with apply's text when the changes don't apply.
pub fn apply_changes(
    parent: &ScenarioInputs,
    changes: &ScenarioInputsDiff,
) -> Result<ScenarioInputs, Error> {
    let convert = || Error::Apply("taxyear: the inputs can't be reflected over".to_string());
    let p = reflect::to_dyn(parent).ok_or_else(convert)?;
    let c = reflect::to_dyn(changes).ok_or_else(convert)?;
    let pr = prune::prune(&p, &c);
    let mut next =
        protodiffgen::apply_dynamic(&pr.given, &c).map_err(|e| Error::Apply(e.to_string()))?;
    pr.restore(&mut next);
    reflect::from_dyn(&next).ok_or_else(convert)
}

/// changesFailed is the problem with a scenario whose changes don't apply.
pub(crate) fn changes_failed(err: &Error) -> String {
    format!("Its changes don't apply to its parent: {err}.")
}

/// problem is a problem at a path.
pub(crate) fn problem(path: &str, text: impl Into<String>) -> Problem {
    Problem {
        path: path.to_string(),
        message: Statement {
            text: text.into(),
            ..Default::default()
        }
        .into(),
        ..Default::default()
    }
}
