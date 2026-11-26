//! Answers each jurisdiction's estimated-payment rules for a scenario, and
//! fills each tax paid model's default part from the answer. Ported from
//! the Go package `taxyear/paid`.
//!
//! It reads the rules' inputs from the projection: withholding from the
//! employer and other income items on their dates, payments from the tax
//! paid items, last year's return from the facts. The rules are the taxlisp
//! estimated tax rules, asked through the return preparer as
//! ReturnOutputs.estimated, with this year's figures given as a caller
//! estimate. They are asked as of the end of the next year, so the penalty
//! runs as far as the rules let it, and the payments planned are left out or
//! put in to tell them apart from the payments made. The rules run three
//! times. The first pass, over the payments made, gives the requirement,
//! what each due date has been credited, and the penalty without the
//! planned payments. The second, over every payment, gives what is still
//! needed; SpreadTheRest shares it equally across the open due dates; the
//! third, over every payment again, gives the installments and the penalty.

mod output;
mod request;

use std::cell::OnceCell;
use std::collections::{BTreeSet, HashMap, HashSet};
use std::rc::Rc;

use buffa::Message as _;
use money::USCents;
use proto::factsv1::{
    self, __buffa::oneof::document::Form, __buffa::oneof::prior_year_return::Filed,
    prior_year_return,
};
use proto::returnsv1;
use proto::scenariosv1::{self, __buffa::oneof::item::Figures};
use proto::usv1::{
    DivergenceReading, EstimatedAnswer, EstimatedDivergence, EstimatedOutcome, EstimatedQuestion,
};
use returns::preparer;

use crate::internal::civil;
use crate::model::{self, Context, taxpaid};

use output::{date, explanation_of, installment_of, penalty_of, requirement_of, to_meet};

/// An error from answering the rules, with Go's text.
#[derive(Clone, Debug, PartialEq, Eq, thiserror::Error)]
pub enum Error {
    /// The inputs were built with no context.
    #[error("paid: no context")]
    NoContext,
    /// A date the rules need is missing.
    #[error("paid: a date is missing")]
    MissingDate,
    /// Any other refusal, with its text.
    #[error("{0}")]
    Other(String),
    /// One jurisdiction's error, as Go's `fmt.Errorf("estimated[%s]: %w")`.
    #[error("estimated[{jurisdiction}]: {err}")]
    Jurisdiction {
        /// US or a state's postal code.
        jurisdiction: String,
        /// The error.
        err: Box<Error>,
    },
    /// A date that is not a day.
    #[error(transparent)]
    Civil(#[from] civil::Error),
}

/// What the rules read for a scenario, besides the facts. It owns the
/// jurisdictions' expanded tax paid models, which [`compute`] fills; take
/// them back with [`Inputs::into_payments`].
#[derive(Clone, Debug)]
pub struct Inputs {
    ctx: Context,
    status: filing::Status,
    federal_tax: USCents,
    federal_agi: USCents,
    excess_ss: USCents,
    state_tax: HashMap<String, USCents>,
    proj: scenariosv1::YearProjection,
    /// Every tax paid model in the order given.
    payments: Vec<taxpaid::Payments>,
    /// The index in payments of each jurisdiction's model, the last given.
    by_jurisdiction: HashMap<String, usize>,
    overrides: Vec<scenariosv1::Override>,
}

/// Builds [`Inputs`].
#[derive(Clone, Debug)]
pub struct InputsBuilder {
    ctx: Option<Context>,
    status: filing::Status,
    federal_tax: USCents,
    federal_agi: USCents,
    excess_ss: USCents,
    state_tax: HashMap<String, USCents>,
    proj: scenariosv1::YearProjection,
    payments: Vec<taxpaid::Payments>,
    by_jurisdiction: HashMap<String, usize>,
    overrides: Vec<scenariosv1::Override>,
}

/// Starts the inputs for a scenario's context, filing status and
/// projection, its employer and other income items holding the withholding.
pub fn new_inputs(
    ctx: Option<&Context>,
    status: filing::Status,
    proj: &scenariosv1::YearProjection,
) -> InputsBuilder {
    InputsBuilder {
        ctx: ctx.cloned(),
        status,
        federal_tax: USCents(0),
        federal_agi: USCents(0),
        excess_ss: USCents(0),
        state_tax: HashMap::new(),
        proj: proj.clone(),
        payments: Vec::new(),
        by_jurisdiction: HashMap::new(),
        overrides: Vec::new(),
    }
}

impl InputsBuilder {
    /// Sets the federal tax the rules measure, Form 2210 line 2, the
    /// adjusted gross income, and Schedule 3 line 11's excess Social
    /// Security tax, which counts as withholding.
    pub fn federal(&mut self, tax: USCents, agi: USCents, excess_ss: USCents) -> &mut Self {
        self.federal_tax = tax;
        self.federal_agi = agi;
        self.excess_ss = excess_ss;
        self
    }

    /// Sets a state's tax for the year from the state return the rules
    /// prepared, which the rules measure when there is no filed return.
    pub fn state(&mut self, state: &str, tax: USCents) -> &mut Self {
        self.state_tax.insert(state.to_string(), tax);
        self
    }

    /// Adds a jurisdiction's expanded tax paid model. A later one for the
    /// same jurisdiction replaces it in the rules, as Go's map does, but is
    /// still handed back by [`Inputs::into_payments`].
    pub fn payments(&mut self, p: taxpaid::Payments) -> &mut Self {
        self.by_jurisdiction
            .insert(p.jurisdiction().to_string(), self.payments.len());
        self.payments.push(p);
        self
    }

    /// Sets the scenario's overrides, of which the estimated[j] ones are
    /// read.
    pub fn overrides(&mut self, o: &[scenariosv1::Override]) -> &mut Self {
        self.overrides = o.to_vec();
        self
    }

    /// Refuses inputs with no context.
    pub fn build(&self) -> Result<Inputs, Error> {
        let Some(ctx) = &self.ctx else {
            return Err(Error::NoContext);
        };
        Ok(Inputs {
            ctx: ctx.clone(),
            status: self.status,
            federal_tax: self.federal_tax,
            federal_agi: self.federal_agi,
            excess_ss: self.excess_ss,
            state_tax: self.state_tax.clone(),
            proj: self.proj.clone(),
            payments: self.payments.clone(),
            by_jurisdiction: self.by_jurisdiction.clone(),
            overrides: self.overrides.clone(),
        })
    }
}

impl Inputs {
    /// The tax paid models, in the order given.
    pub fn payments(&self) -> &[taxpaid::Payments] {
        &self.payments
    }

    /// The tax paid models, in the order given, each filled once
    /// [`compute`] has run.
    pub fn into_payments(self) -> Vec<taxpaid::Payments> {
        self.payments
    }

    /// The jurisdiction's tax paid model.
    fn payments_for(&mut self, j: &str) -> Option<&mut taxpaid::Payments> {
        let i = *self.by_jurisdiction.get(j)?;
        self.payments.get_mut(i)
    }

    /// Returns an estimated[j].<field> override's amount.
    fn override_of(&self, j: &str, field: &str) -> Option<USCents> {
        let path = format!("estimated[{j}].{field}");
        self.overrides
            .iter()
            .find(|o| o.path == path)
            .map(|o| model::value(&o.amount))
    }
}

/// Every jurisdiction's estimated figures, and what was asked.
#[derive(Clone, Debug, Default)]
pub struct Answer {
    estimated: Vec<scenariosv1::Estimated>,
    notes: model::Notes,
    paid_us: USCents,
}

impl Answer {
    /// Returns one Estimated per jurisdiction, US first.
    pub fn estimated(&self) -> &[scenariosv1::Estimated] {
        &self.estimated
    }

    /// Returns the questions the rules returned.
    pub fn notes(&self) -> &model::Notes {
        &self.notes
    }

    /// The year's federal estimated payments made, every one that counts,
    /// last year's overpayment and planned payments included.
    pub fn federal_payments(&self) -> USCents {
        self.paid_us
    }
}

/// Answers every jurisdiction: US, each with a tax paid model, and each
/// state lived or worked in. A tax paid model's SpreadTheRest default is
/// filled as it goes, so its projection is final after this.
pub fn compute(in_: &mut Inputs) -> Result<Answer, Error> {
    let mut out = Answer::default();
    for j in jurisdictions(in_) {
        let e = one(in_, &j, &mut out.notes).map_err(|err| Error::Jurisdiction {
            jurisdiction: j.clone(),
            err: Box::new(err),
        })?;
        out.estimated.push(e);
    }
    if let Some(p) = in_.payments_for("US") {
        // The return counts the payments made. A planned payment, and the
        // rest of the target SpreadTheRest shares out, are suggestions for
        // the due dates: they reach only the penalty if they are made.
        for x in p.payments() {
            if x.made() {
                out.paid_us += x.amount();
            }
        }
    }
    Ok(out)
}

/// US, then the states in postal-code order.
fn jurisdictions(in_: &Inputs) -> Vec<String> {
    let mut seen: BTreeSet<String> = BTreeSet::new();
    seen.insert("US".to_string());
    for j in in_.by_jurisdiction.keys() {
        seen.insert(j.clone());
    }
    for r in &in_.ctx.facts().residence {
        seen.insert(r.state.clone());
    }
    for e in &in_.proj.employers {
        for s in &e.year.boxes.states {
            seen.insert(s.state.clone());
        }
    }
    let mut out = vec!["US".to_string()];
    out.extend(seen.into_iter().filter(|j| j != "US" && !j.is_empty()));
    out
}

fn one(
    in_: &mut Inputs,
    j: &str,
    notes: &mut model::Notes,
) -> Result<scenariosv1::Estimated, Error> {
    let mut e = scenariosv1::Estimated {
        jurisdiction: j.to_string(),
        ..Default::default()
    };
    // The rules take this year's tax in whole dollars, as the return states
    // it and as they take the withholding.
    let (mut tax, mut tax_known) = (model::dollars(in_.federal_tax), j == "US");
    if j != "US" {
        (tax, tax_known) = filed_tax(in_, j);
        if !tax_known {
            (tax, tax_known) = match in_.state_tax.get(j) {
                Some(&t) => (t, true),
                None => (USCents(0), false),
            };
        }
    }
    if let Some(v) = in_.override_of(j, "tax_for_year") {
        (tax, tax_known) = (v, true);
    }
    if tax_known {
        e.tax_for_year = model::money(tax).into();
    }
    let mut prior = prior_return(in_, j);
    if let Some(v) = in_.override_of(j, "last_year_tax") {
        (prior.tax, prior.tax_set) = (v, true);
    }
    if let Some(v) = in_.override_of(j, "last_year_agi") {
        (prior.agi, prior.agi_set) = (v, true);
    }
    if let Some(v) = in_.override_of(j, "last_year_withheld") {
        (prior.withheld, prior.withheld_set) = (v, true);
    }
    e.last_year_tax = model::money_if_set(prior.tax, prior.tax_set).into();
    e.last_year_agi = model::money_if_set(prior.agi, prior.agi_set).into();
    let overridden = in_.override_of(j, "last_year_agi").is_some();
    if j != "US" && prior.state_agi_set && !overridden {
        e.last_year_agi = model::money(prior.state_agi).into();
    }
    e.last_year_withheld = model::money_if_set(prior.withheld, prior.withheld_set).into();
    let mut r = Runner {
        in_,
        j: j.to_string(),
        tax,
        tax_known,
        prior,
        notes,
        withheld: Vec::new(),
        settled: false,
        asked: HashMap::new(),
    };
    e.rules = r.answer().into();
    Ok(e)
}

/// A state's tax for a finished year from the year's own filed return, the
/// line the prior-year rules read as last year's tax: Form 540's total tax,
/// or Form OR-40's tax after all credits. While the year is in progress, or
/// without the return, the tax is the one the rules prepared, when they
/// prepared one.
fn filed_tax(in_: &Inputs, j: &str) -> (USCents, bool) {
    if !in_.ctx.finished() {
        return (USCents(0), false);
    }
    let year = in_.ctx.year() as i32;
    for d in &in_.ctx.facts().unassigned {
        if !model::usable(&d.source) {
            continue;
        }
        match &d.form {
            Some(Form::CaliforniaReturn(c)) if j == "CA" => {
                if let Some(f) = c.form_540.as_option()
                    && f.tax_year == year
                {
                    return model::cents(&f.total_tax);
                }
            }
            Some(Form::OregonReturn(o)) if j == "OR" => {
                if let Some(f) = o.form_or40.as_option()
                    && f.tax_year == year
                {
                    return model::cents(&f.tax_after_all_credits);
                }
            }
            _ => {}
        }
    }
    (USCents(0), false)
}

/// Last year's figures for one jurisdiction.
#[derive(Clone, Copy, Debug, Default)]
struct Prior {
    status: buffa::EnumValue<prior_year_return::Status>,
    tax: USCents,
    agi: USCents,
    withheld: USCents,
    state_agi: USCents,
    taxable: USCents,
    tax_set: bool,
    agi_set: bool,
    withheld_set: bool,
    state_agi_set: bool,
    taxable_set: bool,
    full_year: Option<bool>,
}

fn prior_return(in_: &Inputs, j: &str) -> Prior {
    let mut out = state_or_federal_prior(in_, j);
    // A state return need not print the federal AGI it starts from, so a
    // state's last-year AGI falls back to last year's federal return.
    if j != "US" && !out.agi_set {
        let us = state_or_federal_prior(in_, "US");
        if us.agi_set {
            (out.agi, out.agi_set) = (us.agi, true);
        }
    }
    out
}

fn state_or_federal_prior(in_: &Inputs, j: &str) -> Prior {
    for p in &in_.ctx.facts().prior_year {
        if p.jurisdiction != j || !p.people.is_empty() {
            continue;
        }
        let mut out = Prior {
            status: p.status,
            full_year: p.full_year_resident,
            ..Default::default()
        };
        match &p.filed {
            Some(Filed::Federal(f)) => {
                let f = &f.form_1040;
                (out.tax, out.tax_set) = (model::value(&f.total_tax), true);
                (out.agi, out.agi_set) = (model::value(&f.adjusted_gross_income), true);
                (out.withheld, out.withheld_set) = (model::value(&f.total_withheld), true);
            }
            Some(Filed::California(c)) => {
                let f = &c.form_540;
                (out.tax, out.tax_set) = model::cents(&f.total_tax);
                (out.agi, out.agi_set) = model::cents(&f.federal_adjusted_gross_income);
                (out.state_agi, out.state_agi_set) =
                    model::cents(&f.california_adjusted_gross_income);
                (out.taxable, out.taxable_set) = model::cents(&f.taxable_income);
                (out.withheld, out.withheld_set) = model::cents(&f.california_tax_withheld);
            }
            Some(Filed::Oregon(o)) => {
                let f = &o.form_or40;
                (out.tax, out.tax_set) = model::cents(&f.tax_after_all_credits);
                (out.agi, out.agi_set) = model::cents(&f.federal_adjusted_gross_income);
                (out.taxable, out.taxable_set) = model::cents(&f.taxable_income);
                (out.withheld, out.withheld_set) = model::cents(&f.oregon_tax_withheld);
            }
            _ if p.total_tax.is_set() => {
                (out.tax, out.tax_set) = (model::value(&p.total_tax), true);
                (out.agi, out.agi_set) = model::cents(&p.adjusted_gross_income);
            }
            _ => {}
        }
        return out;
    }
    Prior::default()
}

/// Asks one jurisdiction's rules.
struct Runner<'a> {
    in_: &'a mut Inputs,
    j: String,
    tax: USCents,
    tax_known: bool,
    prior: Prior,
    notes: &'a mut model::Notes,
    /// The withholding the facts pass, worked out once: each request the
    /// rules are asked passes the same.
    withheld: Vec<Dated>,
    settled: bool,
    /// Each answer by its request, so a request the same as an earlier one,
    /// such as every payment when none is planned, isn't asked again.
    asked: HashMap<Vec<u8>, returnsv1::EstimatedAnswer>,
}

thread_local! {
    static SERVER: OnceCell<Result<Rc<preparer::Server>, preparer::Error>> =
        const { OnceCell::new() };
}

/// The return preparer over the shared rules engine, which is loaded and
/// typechecked once (per thread, as the engine is).
fn server() -> Result<Rc<preparer::Server>, Error> {
    SERVER
        .with(|cell| {
            cell.get_or_init(|| preparer::Server::new().map(Rc::new))
                .clone()
        })
        .map_err(|e| Error::Other(e.to_string()))
}

impl Runner<'_> {
    fn answer(&mut self) -> EstimatedAnswer {
        match self.compute() {
            Ok(out) => out,
            Err(err) => self.outcome(EstimatedOutcome::ESTIMATED_OUTCOME_REFUSED, err.to_string()),
        }
    }

    /// An answer with only an outcome and its reason.
    fn outcome(&self, outcome: EstimatedOutcome, reason: String) -> EstimatedAnswer {
        EstimatedAnswer {
            outcome: outcome.into(),
            reason,
            this_year_tax_known: self.tax_known,
            ..Default::default()
        }
    }

    /// Asks the rules for the requirement over the payments made, fills the
    /// default part from the schedule over every payment, then asks for the
    /// installments and the penalty with the planned payments. The
    /// installments' credits and the penalty without the planned payments
    /// are the first pass's.
    fn compute(&mut self) -> Result<EstimatedAnswer, Error> {
        use returnsv1::EstimatedOutcome as R;
        let made = self.ask(true)?;
        match made.outcome.as_known() {
            Some(R::ESTIMATED_OUTCOME_COMPUTED) => {}
            Some(R::ESTIMATED_OUTCOME_NO_RULES) => {
                let reason = format!(
                    "No estimated-payment rules are built for {} for {}.",
                    self.j,
                    self.in_.ctx.year()
                );
                return Ok(self.outcome(EstimatedOutcome::ESTIMATED_OUTCOME_NO_RULES, reason));
            }
            Some(R::ESTIMATED_OUTCOME_UNSUPPORTED) => {
                return Ok(self.outcome(
                    EstimatedOutcome::ESTIMATED_OUTCOME_UNSUPPORTED,
                    made.reason.clone(),
                ));
            }
            _ => {
                return Ok(self.outcome(
                    EstimatedOutcome::ESTIMATED_OUTCOME_REFUSED,
                    made.reason.clone(),
                ));
            }
        }
        let mut out = self.outcome(EstimatedOutcome::ESTIMATED_OUTCOME_COMPUTED, String::new());
        let mut n = Notes::new(&self.j);
        self.computed(&made, &mut out, &mut n)?;
        out.questions = n.questions;
        out.divergences = n.divergences;
        Ok(out)
    }

    /// The rest of [`Runner::compute`], once the rules computed the
    /// requirement over the payments made.
    fn computed(
        &mut self,
        made: &returnsv1::EstimatedAnswer,
        out: &mut EstimatedAnswer,
        n: &mut Notes,
    ) -> Result<(), Error> {
        out.requirement = requirement_of(&made.requirement).into();
        out.explanation = explanation_of(&made.explanation).into();
        if !out.requirement.target_known {
            n.add(self.notes, made);
            return Ok(());
        }
        // The questions are those over every payment, once the default part
        // is filled: the penalty without the planned payments, and the one
        // before the default part was filled, are only steps, and ask
        // nothing.
        let pass1 = self.ask(false)?;
        let mut all = pass1;
        if self.spread(&all.installments) {
            all = self.ask(false)?;
        }
        n.add(self.notes, &all);
        if all.outcome.as_known() != Some(returnsv1::EstimatedOutcome::ESTIMATED_OUTCOME_COMPUTED) {
            return Err(Error::Other(format!(
                "paid: the rules answered {} over every payment: {}",
                outcome_name(all.outcome),
                all.reason
            )));
        }
        let today = self.in_.ctx.as_of();
        let (before, sched) = (&made.installments, &all.installments);
        out.to_meet = to_meet(before, today);
        if before.len() != sched.len() {
            return Err(Error::Other(format!(
                "paid: {} installments over the payments made, and {} over all of them",
                before.len(),
                sched.len()
            )));
        }
        let mut planned: HashSet<civil::Date> = HashSet::new();
        let j = self.j.clone();
        if let Some(p) = self.in_.payments_for(&j) {
            for x in p.payments() {
                if !x.made() && x.amount().0 > 0 && !x.on().is_zero() {
                    planned.insert(x.on());
                }
            }
        }
        for (k, i) in sched.iter().enumerate() {
            let due = date(&i.due);
            out.installments.push(installment_of(
                i,
                &before[k],
                today,
                !due.is_zero() && planned.contains(&due),
            ));
        }
        let exempt = out.requirement.exempt;
        out.penalty = penalty_of(&all.penalty, sched, self.tax_known, exempt).into();
        out.penalty_without_planned =
            penalty_of(&made.penalty, before, self.tax_known, exempt).into();
        Ok(())
    }

    /// Runs the rules over the payments made, or over every one that
    /// counts, and returns the jurisdiction's answer.
    fn ask(&mut self, made_only: bool) -> Result<returnsv1::EstimatedAnswer, Error> {
        let inputs = self.request(made_only)?;
        let key = inputs.encode_to_vec();
        if let Some(a) = self.asked.get(&key) {
            return Ok(a.clone());
        }
        let s = server()?;
        let req = returnsv1::PrepareRequest {
            inputs: buffa::MessageField::some(inputs),
            ..Default::default()
        };
        let resp = s
            .prepare(&req)
            .map_err(|e| Error::Other(format!("rules engine: {e}")))?;
        for a in &resp.outputs.estimated {
            if a.jurisdiction == self.j {
                self.asked.insert(key, a.clone());
                return Ok(a.clone());
            }
        }
        Err(Error::Other(format!(
            "paid: the rules gave no estimated answer for {}",
            self.j
        )))
    }

    /// Fills the tax paid model's open due dates with equal payments
    /// reaching the target: what the last installment still needs over
    /// every payment made and planned. It reports whether it added any.
    fn spread(&mut self, installments: &[returnsv1::EstimatedInstallment]) -> bool {
        let j = self.j.clone();
        let Some(p) = self.in_.payments_for(&j) else {
            return false;
        };
        let Some(last) = installments.last() else {
            return false;
        };
        let dues: Vec<civil::Date> = installments
            .iter()
            .map(|i| date(&i.due))
            .filter(|d| !d.is_zero())
            .collect();
        let open = p.open(&dues);
        if open.is_empty() {
            return false;
        }
        let rest = model::value(&last.needed_by) - model::value(&last.credited_by);
        p.spread(&open, rest);
        rest.0 > 0
    }

    /// This year's AGI as the state measures it: federal AGI less Treasury
    /// interest, which no state taxes, and in California plus every HSA
    /// contribution, which California doesn't exclude (Schedule CA line 1h
    /// adds W-2 box 12 code W, and takes back the federal HSA deduction).
    /// For someone who didn't live in California all year it is Form 540NR
    /// line 32, the California-source AGI: California wages, plus the rest
    /// of that AGI for the days lived there. A state lived in for none of
    /// the year has none.
    fn state_agi(&self) -> Option<USCents> {
        if self.j == "US" {
            return None;
        }
        let (days, all) = self.days_lived_in(&self.j);
        if days == 0 {
            return None;
        }
        let proj = &self.in_.proj;
        let mut agi = self.in_.federal_agi;
        for a in &proj.accounts {
            agi -= model::value(&a.year.boxes.treasury_interest);
        }
        if self.j != "CA" {
            return Some(agi);
        }
        let (mut wages_all, mut wages_here) = (USCents(0), USCents(0));
        for e in &proj.employers {
            let b = &e.year.boxes;
            agi += model::value(&b.hsa);
            wages_all += model::value(&b.wages) + model::value(&b.hsa);
            for st in &b.states {
                if st.state == "CA" {
                    wages_here += model::value(&st.wages);
                }
            }
        }
        for x in &proj.retirement {
            agi += model::value(&x.year.items.hsa);
        }
        if days == all {
            return Some(agi);
        }
        Some(wages_here + model::scale(agi - wages_all, USCents(days), USCents(all)))
    }

    /// The days of the year the residence periods put in a state, an open
    /// end read as December 31, and the year's days.
    fn days_lived_in(&self, state: &str) -> (i64, i64) {
        let ctx = &self.in_.ctx;
        let (start, end) = (ctx.year_start(), ctx.year_end());
        let all = end.days_from(start) + 1;
        let mut days = 0;
        for p in &ctx.facts().residence {
            if p.state != state {
                continue;
            }
            let (Ok(mut from), Ok(mut to)) = (civil::from_field(&p.from), civil::from_field(&p.to))
            else {
                continue;
            };
            if from.is_zero() || from.before(start) {
                from = start;
            }
            if to.is_zero() || to.after(end) {
                to = end;
            }
            if !to.before(from) {
                days += to.days_from(from) + 1;
            }
        }
        (days.min(all), all)
    }

    /// The jurisdiction's withholding on each item's date: federal or that
    /// state's, from the employer and other income items. For US, excess
    /// Social Security tax is withholding dated December 31, as Form 2210
    /// line 6 counts it.
    fn withholding(&mut self) -> Result<Vec<Dated>, Error> {
        if !self.settled {
            self.withheld = self.work_out_withholding()?;
            self.settled = true;
        }
        Ok(self.withheld.clone())
    }

    fn work_out_withholding(&self) -> Result<Vec<Dated>, Error> {
        let mut by_date: HashMap<String, USCents> = HashMap::new();
        let mut dates: HashMap<String, civil::Date> = HashMap::new();
        for (on, v) in self.collect_withholding()? {
            *by_date.entry(on.to_string()).or_insert(USCents(0)) += v;
            dates.insert(on.to_string(), on);
        }
        let mut settled = settle(&by_date, &dates);
        self.to_boxes(&mut settled);
        let (start, end) = (self.in_.ctx.year_start(), self.in_.ctx.year_end());
        for w in &settled {
            if w.on.is_zero() {
                return Err(Error::MissingDate);
            }
            if w.on.before(start) || w.on.after(end) {
                return Err(Error::Other(format!(
                    "paid: withholding on {} is outside tax year {}",
                    w.on,
                    self.in_.ctx.year()
                )));
            }
        }
        Ok(settled)
    }

    /// Makes the withholding add up to the year's figures in whole dollars,
    /// as the return shows them: the rules take rounded figures, so the
    /// difference from the exact sum goes on the last date. The employers'
    /// year boxes are already so far's and the rest's rounded and added;
    /// other income's withholding is rounded as a whole.
    fn to_boxes(&self, ws: &mut [Dated]) {
        if ws.is_empty() {
            return;
        }
        let proj = &self.in_.proj;
        let mut exact = USCents(0);
        for w in ws.iter() {
            exact += w.v;
        }
        let mut want = USCents(0);
        for e in &proj.employers {
            let b = &e.year.boxes;
            if self.j == "US" {
                want += model::value(&b.federal_withheld);
                continue;
            }
            for s in &b.states {
                if s.state == self.j {
                    want += model::value(&s.withheld);
                }
            }
        }
        let mut other = USCents(0);
        for o in &proj.other_income {
            for it in &o.items {
                if self.j == "US" {
                    other += model::value(&other_income_of(it).federal_withheld);
                }
            }
        }
        want += model::dollars(other);
        if self.j == "US" {
            want += self.in_.excess_ss;
        }
        if let Some(last) = ws.last_mut()
            && (last.v + want - exact).0 >= 0
        {
            last.v += want - exact;
        }
    }

    /// Each item's withholding for the jurisdiction, in the order Go's
    /// collectWithholding passes them to add.
    fn collect_withholding(&self) -> Result<Vec<(civil::Date, USCents)>, Error> {
        let proj = &self.in_.proj;
        let mut out = Vec::new();
        for e in &proj.employers {
            for it in &e.items {
                let d = civil::from_field(&it.date)?;
                let p = paycheck_of(it);
                let mut v = model::value(&p.federal_withheld);
                if self.j != "US" {
                    v = USCents(0);
                    for s in &p.states {
                        if s.state == self.j {
                            v += model::value(&s.tax_withheld);
                        }
                    }
                }
                out.push((d, v));
            }
        }
        for o in &proj.other_income {
            for it in &o.items {
                let d = civil::from_field(&it.date)?;
                let mut v = model::value(&other_income_of(it).federal_withheld);
                if self.j != "US" {
                    v = USCents(0);
                }
                out.push((d, v));
            }
        }
        if self.j == "US" {
            out.push((self.in_.ctx.year_end(), self.in_.excess_ss));
        }
        Ok(out)
    }
}

fn paycheck_of(it: &scenariosv1::Item) -> &factsv1::PayFigures {
    match &it.figures {
        Some(Figures::Paycheck(p)) => p,
        _ => <factsv1::PayFigures as buffa::DefaultInstance>::default_instance(),
    }
}

fn other_income_of(it: &scenariosv1::Item) -> &scenariosv1::OtherIncomeFigures {
    match &it.figures {
        Some(Figures::OtherIncome(o)) => o,
        _ => <scenariosv1::OtherIncomeFigures as buffa::DefaultInstance>::default_instance(),
    }
}

/// An amount on a date.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
struct Dated {
    on: civil::Date,
    v: USCents,
}

/// Returns the withholding by date with each negative amount, such as a
/// year-to-date adjustment correcting earlier paychecks, taken off the dates
/// before it, latest first, so no date's withholding is negative.
fn settle(by_date: &HashMap<String, USCents>, dates: &HashMap<String, civil::Date>) -> Vec<Dated> {
    let mut out: Vec<Dated> = by_date
        .iter()
        .map(|(k, &v)| Dated {
            on: dates.get(k).copied().unwrap_or_default(),
            v,
        })
        .collect();
    out.sort_by_key(|a| a.on);
    for i in 0..out.len() {
        let mut k = i;
        while out[i].v.0 < 0 && k > 0 {
            k -= 1;
            let take = out[k].v.min(-out[i].v);
            out[k].v -= take;
            out[i].v += take;
        }
        out[i].v = out[i].v.max(USCents(0));
    }
    out.retain(|d| d.v.0 != 0);
    out
}

/// The name Go's %v prints for a returns/v1 outcome.
fn outcome_name(o: buffa::EnumValue<returnsv1::EstimatedOutcome>) -> String {
    match o.as_known() {
        Some(k) => format!("{k:?}"),
        None => o.to_i32().to_string(),
    }
}

/// Collects one jurisdiction's questions and divergences, each once, and
/// adds the questions to the scenario's.
struct Notes {
    j: String,
    questions: Vec<EstimatedQuestion>,
    divergences: Vec<EstimatedDivergence>,
    seen: HashSet<String>,
}

impl Notes {
    fn new(j: &str) -> Notes {
        Notes {
            j: j.to_string(),
            questions: Vec::new(),
            divergences: Vec::new(),
            seen: HashSet::new(),
        }
    }

    /// Adds an answer's questions and divergences that aren't there yet.
    fn add(&mut self, scenario: &mut model::Notes, a: &returnsv1::EstimatedAnswer) {
        for q in &a.questions {
            let key = format!("q{}:{}", q.position, q.because);
            if !self.seen.insert(key) {
                continue;
            }
            self.questions.push(EstimatedQuestion {
                position: q.position.clone(),
                because: q.because.clone(),
                question: q.question.clone(),
                effect: q.effect.clone(),
                ..Default::default()
            });
            let text = if q.question.is_empty() {
                &q.because
            } else {
                &q.question
            };
            let effect = if q.effect.is_empty() {
                &q.because
            } else {
                &q.effect
            };
            scenario.ask(model::new_question(
                format!("estimated/{}/{}", self.j, q.position.replace(' ', "-")),
                format!("estimated[{}]", self.j),
                text.as_str(),
                effect.as_str(),
                "facts",
                &[],
            ));
        }
        for d in &a.divergences {
            if !self.seen.insert(format!("d:{}", d.rule)) {
                continue;
            }
            let uses = match d.uses.as_known() {
                Some(returnsv1::DivergenceReading::DIVERGENCE_READING_FORM) => {
                    DivergenceReading::DIVERGENCE_READING_FORM
                }
                Some(returnsv1::DivergenceReading::DIVERGENCE_READING_STATUTE) => {
                    DivergenceReading::DIVERGENCE_READING_STATUTE
                }
                _ => DivergenceReading::DIVERGENCE_READING_UNSPECIFIED,
            };
            self.divergences.push(EstimatedDivergence {
                rule: d.rule.clone(),
                form: d.form.clone(),
                form_source: d.form_source.clone(),
                statute: d.statute.clone(),
                statute_source: d.statute_source.clone(),
                uses: uses.into(),
                affects_household: d.affects_household,
                ..Default::default()
            });
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn d(m: i64, day: i64) -> civil::Date {
        civil::must(2026, m, day)
    }

    fn settled(entries: &[(civil::Date, i64)]) -> Vec<(String, i64)> {
        let mut by_date = HashMap::new();
        let mut dates = HashMap::new();
        for &(on, v) in entries {
            *by_date.entry(on.to_string()).or_insert(USCents(0)) += USCents(v);
            dates.insert(on.to_string(), on);
        }
        settle(&by_date, &dates)
            .into_iter()
            .map(|w| (w.on.to_string(), w.v.0))
            .collect()
    }

    #[test]
    fn settle_takes_negatives_off_earlier_dates() {
        // The latest date before gives first, and a date left at zero goes.
        let got = settled(&[(d(1, 15), 1000), (d(2, 15), 500), (d(3, 15), -700)]);
        assert_eq!(got, vec![("2026-01-15".to_string(), 800)]);
        // More negative than everything before: the date keeps nothing.
        let got = settled(&[(d(1, 15), 100), (d(2, 15), -300), (d(3, 15), 50)]);
        assert_eq!(got, vec![("2026-03-15".to_string(), 50)]);
        assert!(settled(&[]).is_empty());
    }

    #[test]
    fn build_refuses_no_context() {
        let proj = scenariosv1::YearProjection::default();
        let err = new_inputs(None, filing::Status::Single, &proj)
            .build()
            .err();
        assert_eq!(
            err.map(|e| e.to_string()).as_deref(),
            Some("paid: no context")
        );
    }
}
