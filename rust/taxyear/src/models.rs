//! Expanding the models a job's or tax paid's rules don't reach, through
//! the cache: models.go.
//!
//! A model is expanded on its own unless it is a job, whose withholding and
//! plan contributions depend on the household's retirement models, or tax
//! paid, which the estimated-payment rules fill. So each other model's
//! finished projection, its notes and its workings depend only on the model
//! and the scenario's year, as_of and facts, and a [`Cache`] keeps them by
//! a hash of those: an edit to one job re-expands no charity.
//!
//! Go also finds a message it hashed before by identity, which Rust's
//! copies have none of, so every key here is a hash of content; a job's
//! own checks, which Go keeps only by identity, are checked each time.

use std::any::Any;
use std::collections::HashMap;
use std::sync::Arc;

use buffa::{Message, MessageName};
use proto::scenariosv1::{
    AccountProjection, BrokerProjection, BusinessProjection, ItemisedProjection,
    OtherIncomeProjection, Override, RetirementModel, RetirementProjection, ScenarioInputs,
    TaxYearModel, Working, YearProjection,
};

use crate::cache::{Cache, Key, append_varint, hash};
use crate::canadd::{
    account_can_add, broker_can_add, business_can_add, itemised_can_add, other_income_can_add,
    retirement_can_add,
};
use crate::model::{
    Context, Error, Notes, account, broker, business, itemised, otherincome, retirement,
};
use crate::overrides::{overridden, overrides_at, with_override_workings};
use crate::round::{broker_year, periods};
use crate::validate::{CheckEntry, Sourced};
use crate::working::model_workings;

/// modelEntry is one model's finished projection: expanded, its boxes
/// rounded, its can_add filled, with its workings. Nothing in it is changed
/// once it is made.
pub(crate) struct ModelEntry {
    proj: Box<dyn Any + Send + Sync>,
    notes: Notes,
    working: Vec<Working>,
}

/// modelSet finds and keeps one scenario's models' entries.
pub(crate) struct ModelSet<'a> {
    /// None: nothing is kept.
    c: Option<&'a Cache>,
    pub(crate) ctx: &'a Context,
    /// The year, as_of and facts.
    key: Key,
    /// working is each kept model's workings, by its projection path.
    pub(crate) working: HashMap<String, Vec<Working>>,
    /// overrides are the scenario's overrides.
    overrides: Vec<Override>,
}

impl<'a> ModelSet<'a> {
    pub(crate) fn new(c: Option<&'a Cache>, ctx: &'a Context, in_: &ScenarioInputs) -> Self {
        let mut prefix = append_varint(b"taxyear.models.v1".to_vec(), ctx.year());
        let as_of = ctx.as_of().proto().unwrap_or_default();
        prefix.extend_from_slice(&hash(&[], &as_of));
        let mut ms = ModelSet {
            c,
            ctx,
            key: [0; 32],
            working: HashMap::new(),
            overrides: in_.overrides.clone(),
        };
        ms.key = ms.key_of(&String::from_utf8_lossy(&prefix), &*in_.facts);
        ms
    }

    /// A set that keeps nothing, for Validate.
    pub(crate) fn uncached(ctx: &'a Context) -> Self {
        ModelSet {
            c: None,
            ctx,
            key: [0; 32],
            working: HashMap::new(),
            overrides: Vec::new(),
        }
    }

    /// keyOf hashes a message after a prefix and the scenario's key.
    fn key_of<M: Message>(&self, prefix: &str, m: &M) -> Key {
        let mut p = format!("{prefix}\u{0}").into_bytes();
        p.extend_from_slice(&self.key);
        hash(&p, m)
    }

    /// checked returns a model's own checks, running them with make when
    /// the cache doesn't hold them. A job's are checked each time.
    pub(crate) fn checked(
        &self,
        s: &Sourced,
        make: impl FnOnce() -> Arc<CheckEntry>,
    ) -> Arc<CheckEntry> {
        let Some(c) = self.c else {
            return make();
        };
        if s.list == "employers" {
            return make();
        }
        let k = {
            let mut p = format!("check.{}\u{0}", s.list).into_bytes();
            p.extend_from_slice(&self.key);
            let mut h = p;
            h.extend_from_slice(&s.top.encode_to_vec());
            crate::cache::hash_bytes(&h)
        };
        {
            let mut g = c.lock();
            let t = g.tick();
            if let Some((used, e)) = g.checks.get_mut(&k) {
                *used = t;
                return Arc::clone(e);
            }
        }
        let e = make();
        let mut g = c.lock();
        let t = g.tick();
        g.checks.insert(k, (t, Arc::clone(&e)));
        e
    }

    /// entry returns a model's finished projection and notes, finishing it
    /// with make when the cache doesn't hold it.
    fn entry<M, P>(
        &mut self,
        list: &str,
        id: &str,
        m: &M,
        make: impl FnOnce() -> Result<(P, Notes), Error>,
    ) -> Result<(P, Notes), Error>
    where
        M: Message,
        P: Message + MessageName + Default + Clone + Send + Sync + 'static,
    {
        let at = format!("projection.{list}[{id}]");
        let overrides = overrides_at(&self.overrides, &at);
        let mut k: Key = [0; 32];
        if let Some(c) = self.c {
            k = self.key_of(list, m);
            if !overrides.is_empty() {
                let mut p = b"overrides\x00".to_vec();
                p.extend_from_slice(&k);
                k = hash(
                    &p,
                    &ScenarioInputs {
                        overrides: overrides.clone(),
                        ..Default::default()
                    },
                );
            }
            let found = {
                let mut g = c.lock();
                let t = g.tick();
                match g.models.get_mut(&k) {
                    Some((used, e)) => {
                        *used = t;
                        Some(Arc::clone(e))
                    }
                    None => None,
                }
            };
            if let Some(e) = found
                && let Some(p) = e.proj.downcast_ref::<P>()
            {
                self.working.insert(at, e.working.clone());
                return Ok((p.clone(), e.notes.clone()));
            }
        }
        let (mut p, notes) = make()?;
        let mut working = model_workings(&at, &p);
        if !overrides.is_empty() {
            let (q, ow) = overridden(list, &at, &p, &overrides).map_err(Error::Expansion)?;
            p = q;
            working = with_override_workings(&model_workings(&at, &p), &ow);
        }
        if let Some(c) = self.c {
            let e = Arc::new(ModelEntry {
                proj: Box::new(p.clone()),
                notes: notes.clone(),
                working: working.clone(),
            });
            let mut g = c.lock();
            let t = g.tick();
            g.models.insert(k, (t, e));
        }
        self.working.insert(at, working);
        Ok((p, notes))
    }

    /// expandOthers expands every model but the jobs and tax paid, each
    /// through the cache.
    pub(crate) fn expand_others(
        &mut self,
        m: &TaxYearModel,
        proj: &mut YearProjection,
        notes: &mut Notes,
    ) -> Result<(), Error> {
        let ctx = self.ctx;
        for b in &m.businesses {
            let (p, n) = self.entry::<_, BusinessProjection>("businesses", &b.id, b, || {
                let (mut p, n) = business::expand(ctx, b)?;
                {
                    let BusinessProjection {
                        so_far, rest, year, ..
                    } = &mut p;
                    periods(
                        so_far.as_option_mut().and_then(|t| t.boxes.as_option_mut()),
                        rest.as_option_mut().and_then(|t| t.boxes.as_option_mut()),
                        year.as_option_mut().and_then(|t| t.boxes.as_option_mut()),
                    );
                }
                p.can_add = business_can_add(ctx, b, &p);
                Ok((p, n))
            })?;
            proj.businesses.push(p);
            notes.merge(&n);
        }
        for a in &m.accounts {
            let (p, n) = self.entry::<_, AccountProjection>("accounts", &a.id, a, || {
                let (mut p, n) = account::expand(ctx, a)?;
                {
                    let AccountProjection {
                        so_far, rest, year, ..
                    } = &mut p;
                    periods(
                        so_far.as_option_mut().and_then(|t| t.boxes.as_option_mut()),
                        rest.as_option_mut().and_then(|t| t.boxes.as_option_mut()),
                        year.as_option_mut().and_then(|t| t.boxes.as_option_mut()),
                    );
                }
                p.can_add = account_can_add(ctx, a, &p);
                Ok((p, n))
            })?;
            proj.accounts.push(p);
            notes.merge(&n);
        }
        for b in &m.brokers {
            let (p, n) = self.entry::<_, BrokerProjection>("brokers", &b.id, b, || {
                let (mut p, n) = broker::expand(ctx, b)?;
                {
                    let BrokerProjection {
                        so_far, rest, year, ..
                    } = &mut p;
                    let mut none_s = Vec::new();
                    let mut none_r = Vec::new();
                    let s = match so_far.as_option_mut() {
                        Some(t) => &mut t.boxes,
                        None => &mut none_s,
                    };
                    let r = match rest.as_option_mut() {
                        Some(t) => &mut t.boxes,
                        None => &mut none_r,
                    };
                    let boxes = broker_year(s, r);
                    year.get_or_insert_default().boxes = boxes;
                }
                p.can_add = broker_can_add(ctx, b, &p);
                Ok((p, n))
            })?;
            proj.brokers.push(p);
            notes.merge(&n);
        }
        self.expand_rest(m, proj, notes)
    }

    fn expand_rest(
        &mut self,
        m: &TaxYearModel,
        proj: &mut YearProjection,
        notes: &mut Notes,
    ) -> Result<(), Error> {
        let ctx = self.ctx;
        for it in &m.itemised {
            let (p, n) = self.entry::<_, ItemisedProjection>("itemised", &it.id, it, || {
                let (mut p, n) = itemised::expand(ctx, it)?;
                {
                    let ItemisedProjection {
                        so_far, rest, year, ..
                    } = &mut p;
                    periods(
                        so_far.as_option_mut().and_then(|t| t.boxes.as_option_mut()),
                        rest.as_option_mut().and_then(|t| t.boxes.as_option_mut()),
                        year.as_option_mut().and_then(|t| t.boxes.as_option_mut()),
                    );
                }
                p.can_add = itemised_can_add(ctx, it, &p);
                Ok((p, n))
            })?;
            proj.itemised.push(p);
            notes.merge(&n);
        }
        for o in &m.other_income {
            let (p, n) =
                self.entry::<_, OtherIncomeProjection>("other_income", &o.id, o, || {
                    let (mut p, n) = otherincome::expand(ctx, o)?;
                    {
                        let OtherIncomeProjection {
                            so_far, rest, year, ..
                        } = &mut p;
                        periods(
                            so_far.as_option_mut().and_then(|t| t.boxes.as_option_mut()),
                            rest.as_option_mut().and_then(|t| t.boxes.as_option_mut()),
                            year.as_option_mut().and_then(|t| t.boxes.as_option_mut()),
                        );
                    }
                    p.can_add = other_income_can_add(ctx, o, &p);
                    Ok((p, n))
                })?;
            proj.other_income.push(p);
            notes.merge(&n);
        }
        Ok(())
    }

    /// retirement expands one retirement model through the cache.
    pub(crate) fn retirement(
        &mut self,
        r: &RetirementModel,
    ) -> Result<RetirementProjection, Error> {
        let ctx = self.ctx;
        let (p, _) = self.entry::<_, RetirementProjection>("retirement", &r.id, r, || {
            let mut p = retirement::expand(ctx, r)?;
            p.can_add = retirement_can_add(ctx, r);
            Ok((p, Notes::default()))
        })?;
        Ok(p)
    }
}
