//! Computing every scenario of a file: compute.go.

use std::collections::{HashMap, HashSet};
use std::sync::Arc;

use proto::factsv1::__buffa::oneof::document::Form;
use proto::federalv1::Form1040;
use proto::scenariosv1::{
    self, Item, NotIncluded, Residency, ScenarioInputs, ScenarioResult, StateResult, Statement,
    TaxPaidModel, TaxYearModel, TaxYearScenarios, Working, YearProjection, YearStatus,
};

use crate::cache::{Cache, Key, ScenarioEntry, SharedEntry, scenario_key};
use crate::dependents::{dependent_rows, dependents};
use crate::federal;
use crate::internal::civil;
use crate::model::{self, Context, Notes, employer, retirement, taxpaid};
use crate::models::ModelSet;
use crate::paid;
use crate::resolve::{chain_problem, problem};
use crate::summary::summarise;
use crate::validate::{check_file_with, validate_with};
use crate::working::add_workings;
use crate::{Error, TaxYearResult, goquote};

/// ComputedBy names this library in a result.
pub const COMPUTED_BY: &str = "github.com/nexteggapp/taxcalcs/taxyear 0.1.0";

/// Compute computes every scenario in the file. A problem with the file as
/// a whole is in the result's problems and stops every scenario; a problem
/// in one scenario is in its own problems, and the others still compute.
/// [`Cache::compute`] is the same, reusing work from earlier calls.
///
/// # Errors
///
/// None today: Go's is for a nil file only.
pub fn compute(file: &TaxYearScenarios) -> Result<TaxYearResult, Error> {
    Cache::new(file.scenarios.len() + 1, &[]).compute(file)
}

/// compute is Compute through a cache.
pub(crate) fn compute_with(file: &TaxYearScenarios, c: &Cache) -> TaxYearResult {
    let mut out = scenariosv1::TaxYearResult {
        schema_version: file.schema_version,
        tax_year: file.tax_year,
        as_of: file.as_of.clone(),
        computed_by: COMPUTED_BY.to_string(),
        ..Default::default()
    };
    let (root, shared) = c.shared_for(file);
    out.problems = check_file_with(file, &shared.identifiers);
    out.problems.extend(shared.ids.iter().cloned());
    if !out.problems.is_empty() {
        return TaxYearResult::new(out, HashMap::new());
    }
    out.status = year_status(file).into();
    out.problems.extend(shared.checks.problems.iter().cloned());
    if !out.problems.is_empty() {
        return TaxYearResult::new(out, HashMap::new());
    }
    let mut inputs = HashMap::new();
    for r in resolve(c, file, root, &shared) {
        let res = match (r.failed, &r.entry) {
            (Some(failed), _) => Arc::new(failed),
            (None, Some(entry)) => {
                if let Some(i) = &entry.inputs {
                    inputs.insert(r.id.clone(), Arc::clone(i));
                }
                c.computed(file, entry, &r.id)
            }
            (None, None) => continue,
        };
        out.scenarios.push((*res).clone());
    }
    TaxYearResult::new(out, inputs)
}

/// cachedScenario is one scenario in the file's order: its cache entry, or
/// the result of a scenario that failed before it had one.
struct CachedScenario {
    id: String,
    entry: Option<Arc<ScenarioEntry>>,
    failed: Option<ScenarioResult>,
}

/// resolve finds each scenario's entry down its parent chain, by the rules
/// [`crate::resolve`] states.
fn resolve(
    c: &Cache,
    file: &TaxYearScenarios,
    root: Key,
    shared: &SharedEntry,
) -> Vec<CachedScenario> {
    let mut by_id: HashMap<String, (Key, Arc<ScenarioInputs>)> = HashMap::new();
    let mut out = Vec::new();
    for (i, s) in file.scenarios.iter().enumerate() {
        let text = chain_problem(i, s, |id| by_id.contains_key(id));
        if !text.is_empty() {
            let res = ScenarioResult {
                scenario: s.id.clone(),
                problems: vec![problem(&format!("scenarios[{}]", s.id), text)],
                ..Default::default()
            };
            out.push(CachedScenario {
                id: s.id.clone(),
                entry: None,
                failed: Some(res),
            });
            continue;
        }
        let (parent_key, parent_inputs) = if i > 0 {
            match by_id.get(&s.parent) {
                Some((k, p)) => (*k, Arc::clone(p)),
                None => continue,
            }
        } else {
            (root, Arc::clone(&shared.inputs))
        };
        let k = scenario_key(&parent_key, s);
        let e = c.scenario_for(k, &parent_inputs, s, shared);
        if let Some(i) = &e.inputs {
            by_id.insert(s.id.clone(), (k, Arc::clone(i)));
        }
        out.push(CachedScenario {
            id: s.id.clone(),
            entry: Some(e),
            failed: None,
        });
    }
    out
}

pub(crate) fn year_status(file: &TaxYearScenarios) -> YearStatus {
    let year = i64::from(file.tax_year);
    if !federal::has_rules(year) {
        return YearStatus::YEAR_STATUS_NO_RULES;
    }
    let as_of = civil::from_field(&file.as_of).unwrap_or_default();
    if as_of.after(civil::must(year, 12, 31)) {
        return YearStatus::YEAR_STATUS_FINISHED;
    }
    YearStatus::YEAR_STATUS_IN_PROGRESS
}

/// computeScenario fills one scenario's result from its resolved inputs,
/// keeping each model's finished projection in c, which may be None.
pub(crate) fn compute_scenario(
    c: Option<&Cache>,
    file: &TaxYearScenarios,
    in_: &ScenarioInputs,
    res: &mut ScenarioResult,
) {
    let facts = in_.facts.as_option().map(|f| Arc::new(f.clone()));
    let ctx = match Context::new(i64::from(file.tax_year), file.as_of.as_option(), facts) {
        Ok(ctx) => ctx,
        Err(e) => {
            res.problems.push(problem("as_of", e.to_string()));
            return;
        }
    };
    let mut ms = ModelSet::new(c, &ctx, in_);
    res.problems = validate_with(&ctx, in_, &ms);
    if !res.problems.is_empty() {
        return;
    }
    let mut notes = Notes::default();
    res.dependents = dependents(&ctx, &mut notes);
    let mut working = Vec::new();
    let (mut proj, payments) = match project(&mut ms, in_, &mut notes, &mut working) {
        Ok(p) => p,
        Err(e) => {
            res.problems.push(problem("model", e.to_string()));
            return;
        }
    };
    res.projection = proj.clone().into();
    res.working = working;
    let dup = duplicate_item(&proj);
    if !dup.is_empty() {
        res.problems.push(problem(
            "projection",
            format!("Two items share the id {}.", goquote::quote(&dup)),
        ));
        return;
    }
    let with_working = c.is_none_or(|c| !c.no_working);
    let mut ret = match federal::compute_with_working(&ctx, &proj, in_, with_working) {
        Ok(r) => Some(r),
        Err(federal::Error::NoRules) => {
            notes.leave_out(format!(
                "The federal return: this build has no rate tables for {}",
                ctx.year()
            ));
            None
        }
        Err(e) => {
            res.problems.push(problem("federal", e.to_string()));
            return;
        }
    };
    if let Some(ret) = ret.as_mut() {
        if let Err(e) = answer_payments(&ctx, in_, &mut proj, payments, ret, res, &mut notes) {
            res.problems.push(problem("estimated", e.to_string()));
            return;
        }
        res.projection = proj.clone().into();
        res.federal = ret.form().clone().into();
        let rows = dependent_rows(&ctx, &res.dependents);
        if let Some(f) = res
            .federal
            .as_option_mut()
            .and_then(|r| r.form_1040.as_option_mut())
            && f.dependents.is_empty()
        {
            f.dependents = rows;
        }
        res.working.extend(ret.working().iter().cloned());
        notes.merge(ret.notes());
    }
    res.states = states(&ctx, &proj);
    summarise(&ctx, in_, ret.as_ref(), res);
    add_workings(res, &ms.working);
    for q in notes.questions() {
        res.questions.push(q.proto());
    }
    for t in notes.left_out() {
        res.not_included.push(NotIncluded {
            text: statement(t).into(),
            changes_federal_tax: true,
            ..Default::default()
        });
    }
    res.not_included.push(NotIncluded {
        text: statement("State income tax, which isn't worked out yet".to_string()).into(),
        ..Default::default()
    });
}

fn statement(text: String) -> Statement {
    Statement {
        text,
        ..Default::default()
    }
}

/// answerPayments runs the estimated-payment rules, fills the tax paid
/// models' defaults, and finishes the return's payments lines.
fn answer_payments(
    ctx: &Context,
    in_: &ScenarioInputs,
    proj: &mut YearProjection,
    payments: Vec<taxpaid::Payments>,
    ret: &mut federal::Return,
    res: &mut ScenarioResult,
    notes: &mut Notes,
) -> Result<(), paid::Error> {
    let mut b = paid::new_inputs(Some(ctx), ret.status(), proj);
    b.federal(ret.tax_2210(), ret.agi(), ret.excess_social_security())
        .overrides(&in_.overrides);
    for p in payments {
        b.payments(p);
    }
    let mut inputs = b.build()?;
    let answer = paid::compute(&mut inputs)?;
    res.estimated = answer.estimated().to_vec();
    notes.merge(answer.notes());
    proj.tax_paid.clear();
    for mut p in inputs.into_payments() {
        proj.tax_paid.push(p.projection());
    }
    ret.split_overpayment(filed_federal(ctx));
    Ok(())
}

/// holdHsa holds each person's HSA contributions through payroll to their
/// limit, where it is known.
fn hold_hsa(
    ctx: &Context,
    m: &TaxYearModel,
    jobs: &mut [employer::Job<'_>],
    notes: &mut Notes,
) -> Result<(), model::Error> {
    let mut seen = HashSet::new();
    for e in &m.employers {
        if !seen.insert(e.person.clone()) {
            continue;
        }
        let (limit, ok) = retirement::hsa_limit(ctx, &e.person, notes);
        if !ok {
            continue;
        }
        employer::hold_hsa(&e.person, limit, jobs)?;
    }
    Ok(())
}

/// filedFederal is the year's own filed Form 1040 once the year is over,
/// or None.
fn filed_federal(ctx: &Context) -> Option<&Form1040> {
    if !ctx.finished() {
        return None;
    }
    for d in &ctx.facts().unassigned {
        if let Some(Form::FederalReturn(r)) = &d.form
            && let Some(f) = r.form_1040.as_option()
            && model::usable(&d.source)
            && i64::from(f.tax_year) == ctx.year()
        {
            return Some(f);
        }
    }
    None
}

/// project expands every model, then runs the adjustment parts: each
/// person's workplace plan, then each job's computed withholding.
fn project(
    ms: &mut ModelSet<'_>,
    in_: &ScenarioInputs,
    notes: &mut Notes,
    working: &mut Vec<Working>,
) -> Result<(YearProjection, Vec<taxpaid::Payments>), model::Error> {
    let ctx = ms.ctx;
    let m = &*in_.model;
    let mut proj = YearProjection::default();
    let mut jobs = Vec::new();
    for e in &m.employers {
        jobs.push(employer::expand(ctx, e, &in_.overrides)?);
    }
    for r in &m.retirement {
        if let Some(part) = retirement::plan_part(r) {
            let n = employer::max_out(ctx, &r.person, part, &mut jobs)?;
            notes.merge(&n);
        }
        let p = ms.retirement(r)?;
        proj.retirement.push(p);
    }
    hold_hsa(ctx, m, &mut jobs, notes)?;
    for j in jobs.iter_mut() {
        let p = j.projection()?;
        proj.employers.push(p);
        notes.merge(j.notes());
        working.extend(j.workings().iter().cloned());
    }
    ms.expand_others(m, &mut proj, notes)?;
    let mut payments = Vec::new();
    for t in with_adjacent(ctx, &m.tax_paid) {
        let mut p = taxpaid::expand(ctx, &t, &ctx.facts().adjacent_payments)?;
        notes.merge(p.notes());
        proj.tax_paid.push(p.projection());
        payments.push(p);
    }
    Ok((proj, payments))
}

/// withAdjacent is the tax paid models, and an empty one for each
/// jurisdiction an adjacent year's payment for this year was made to that
/// has no model, so the payment is counted. It is named tax-paid-<code>.
fn with_adjacent(ctx: &Context, ms: &[TaxPaidModel]) -> Vec<TaxPaidModel> {
    let mut has: HashSet<String> = ms.iter().map(|t| t.jurisdiction.clone()).collect();
    let mut out = ms.to_vec();
    for a in &ctx.facts().adjacent_payments {
        let j = &a.jurisdiction;
        if has.contains(j) || !taxpaid::for_year(a, ctx.year()) {
            continue;
        }
        has.insert(j.clone());
        out.push(TaxPaidModel {
            id: format!("tax-paid-{}", j.to_lowercase()),
            jurisdiction: j.clone(),
            ..Default::default()
        });
    }
    out
}

/// duplicateItem returns an item id two items share, checking the models
/// doc's Validate check 2, or "".
fn duplicate_item(p: &YearProjection) -> String {
    let mut seen = HashSet::new();
    for items in all_items(p) {
        for it in items {
            if !seen.insert(it.id.as_str()) {
                return it.id.clone();
            }
        }
    }
    String::new()
}

/// allItems lists every projection's items.
pub(crate) fn all_items(p: &YearProjection) -> Vec<&[Item]> {
    let mut out: Vec<&[Item]> = Vec::new();
    out.extend(p.employers.iter().map(|x| x.items.as_slice()));
    out.extend(p.businesses.iter().map(|x| x.items.as_slice()));
    out.extend(p.accounts.iter().map(|x| x.items.as_slice()));
    out.extend(p.brokers.iter().map(|x| x.items.as_slice()));
    out.extend(p.retirement.iter().map(|x| x.items.as_slice()));
    out.extend(p.itemised.iter().map(|x| x.items.as_slice()));
    out.extend(p.other_income.iter().map(|x| x.items.as_slice()));
    out.extend(p.tax_paid.iter().map(|x| x.items.as_slice()));
    out
}

/// states lists each state lived or worked in, with its residency, and no
/// return: state income tax isn't worked out yet.
fn states(ctx: &Context, proj: &YearProjection) -> Vec<StateResult> {
    let mut lived: HashMap<&str, i64> = HashMap::new();
    let mut order: Vec<&str> = Vec::new();
    for r in &ctx.facts().residence {
        if !lived.contains_key(r.state.as_str()) {
            order.push(&r.state);
        }
        *lived.entry(&r.state).or_insert(0) += 1;
    }
    let mut worked = HashSet::new();
    for e in &proj.employers {
        for s in &e.year.boxes.states {
            if !lived.contains_key(s.state.as_str()) && worked.insert(s.state.as_str()) {
                order.push(&s.state);
            }
        }
    }
    let mut out = Vec::new();
    for s in order {
        let n = lived.get(s).copied().unwrap_or(0);
        let r = if n > 0 && lived.len() == 1 {
            Residency::RESIDENCY_RESIDENT
        } else if n > 0 {
            Residency::RESIDENCY_PART_YEAR
        } else {
            Residency::RESIDENCY_NONRESIDENT
        };
        out.push(StateResult {
            jurisdiction: s.to_string(),
            residency: r.into(),
            ..Default::default()
        });
    }
    out
}
