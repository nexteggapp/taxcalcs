//! Answers each jurisdiction's estimated-payment rules for a scenario, and
//! fills each tax paid model's default part from the answer. Ported from
//! the Go package `taxyear/paid`.
//!
//! It reads the rules' inputs from the projection: withholding from the
//! employer and other income items on their dates, payments from the tax
//! paid items, last year's return from the facts. The rules run twice. The
//! first pass, over the payments made by as_of and the planned ones, gives
//! what is still needed; SpreadTheRest shares it equally across the open due
//! dates; the second pass, over every payment, gives the installments and
//! the penalty.

mod output;

use std::collections::{BTreeSet, HashMap, HashSet};

use estimated::{EstimatedTax, Jurisdiction};
use money::USCents;
use proto::factsv1::{
    self, __buffa::oneof::document::Form, __buffa::oneof::election::Choice,
    __buffa::oneof::prior_year_return::Filed, prior_year_return, waiver_request,
};
use proto::scenariosv1::{self, __buffa::oneof::item::Figures};
use proto::usv1::{
    CalendarDate, DivergenceReading, EstimatedAnswer, EstimatedDivergence, EstimatedOutcome,
    EstimatedPenalty, EstimatedQuestion,
};

use crate::internal::civil;
use crate::model::{self, Context, taxpaid};

use output::{explanation_of, installment_of, penalty_of, requirement_of, to_meet};

/// An error from answering the rules, with Go's text.
#[derive(Clone, Debug, PartialEq, Eq, thiserror::Error)]
pub enum Error {
    /// The inputs were built with no context.
    #[error("paid: no context")]
    NoContext,
    /// A date the rules need is missing.
    #[error("paid: a date is missing")]
    MissingDate,
    /// The two schedules have different numbers of installments.
    #[error("paid: {made} installments over the payments made, and {all} over all of them")]
    Installments {
        /// Installments over the payments made.
        made: usize,
        /// Installments over every payment.
        all: usize,
    },
    /// One jurisdiction's error, as Go's `fmt.Errorf("estimated[%s]: %w")`.
    #[error("estimated[{jurisdiction}]: {err}")]
    Jurisdiction {
        /// US or a state's postal code.
        jurisdiction: String,
        /// The error.
        err: Box<Error>,
    },
    /// An error from the rules.
    #[error(transparent)]
    Estimated(#[from] estimated::Error),
    /// A date that is not a day.
    #[error(transparent)]
    Civil(#[from] civil::Error),
}

impl Error {
    /// Reports whether the error is or wraps `estimated::Error::Unsupported`,
    /// as Go's `errors.Is(err, estimated.ErrUnsupported)`.
    fn is_unsupported(&self) -> bool {
        match self {
            Error::Estimated(e) => e.is(&estimated::Error::Unsupported),
            Error::Jurisdiction { err, .. } => err.is_unsupported(),
            _ => false,
        }
    }
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
    let year = in_.ctx.year();
    let rules = match estimated_all::for_jurisdiction(&Jurisdiction::new(j), year) {
        Err(err) if err.is(&estimated::Error::NoRules) => {
            e.rules = EstimatedAnswer {
                outcome: EstimatedOutcome::ESTIMATED_OUTCOME_NO_RULES.into(),
                reason: format!("No estimated-payment rules are built for {j} for {year}."),
                this_year_tax_known: tax_known,
                ..Default::default()
            }
            .into();
            return Ok(e);
        }
        Err(err) => return Err(err.into()),
        Ok(r) => r,
    };
    let mut r = Runner {
        in_,
        j: j.to_string(),
        rules,
        tax,
        tax_known,
        prior,
        notes,
        withheld: Vec::new(),
        settled: false,
    };
    e.rules = r.answer().into();
    Ok(e)
}

/// A state's tax for a finished year from the year's own filed return, the
/// line the prior-year rules read as last year's tax: Form 540's total tax,
/// or Form OR-40's tax after all credits. While the year is in progress, or
/// without the return, the state's tax isn't worked out.
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
            None if p.total_tax.is_set() => {
                (out.tax, out.tax_set) = (model::value(&p.total_tax), true);
                (out.agi, out.agi_set) = model::cents(&p.adjusted_gross_income);
            }
            None => {}
        }
        return out;
    }
    Prior::default()
}

/// Asks one jurisdiction's rules.
struct Runner<'a> {
    in_: &'a mut Inputs,
    j: String,
    rules: Box<dyn EstimatedTax>,
    tax: USCents,
    tax_known: bool,
    prior: Prior,
    notes: &'a mut model::Notes,
    /// The withholding the facts pass, worked out once: each set of facts
    /// the rules are asked about passes the same.
    withheld: Vec<estimated::Withheld>,
    settled: bool,
}

impl Runner<'_> {
    fn answer(&mut self) -> EstimatedAnswer {
        match self.compute() {
            Ok(out) => out,
            Err(err) => EstimatedAnswer {
                outcome: if err.is_unsupported() {
                    EstimatedOutcome::ESTIMATED_OUTCOME_UNSUPPORTED
                } else {
                    EstimatedOutcome::ESTIMATED_OUTCOME_REFUSED
                }
                .into(),
                reason: err.to_string(),
                this_year_tax_known: self.tax_known,
                ..Default::default()
            },
        }
    }

    /// Asks the rules for the requirement over the payments made, fills the
    /// default part from the schedule over every payment, then asks for the
    /// installments and the penalty with and without the planned payments.
    fn compute(&mut self) -> Result<EstimatedAnswer, Error> {
        let mut out = EstimatedAnswer {
            outcome: EstimatedOutcome::ESTIMATED_OUTCOME_COMPUTED.into(),
            this_year_tax_known: self.tax_known,
            ..Default::default()
        };
        let mut n = Notes::new(&self.j);
        let r = self.compute_into(&mut out, &mut n);
        out.questions = n.questions;
        out.divergences = n.divergences;
        r.map(|()| out)
    }

    fn compute_into(&mut self, out: &mut EstimatedAnswer, n: &mut Notes) -> Result<(), Error> {
        let made_facts = self.facts(true)?;
        let req = self.rules.requirement(&made_facts)?;
        n.add(self.notes, &req.required(), &req.divergences());
        let value = req.value();
        out.requirement = requirement_of(&value).into();
        out.explanation = explanation_of(&self.rules.explanation()).into();
        if !value.target_known() {
            return Ok(());
        }
        let planned = self.facts(false)?;
        let pass1 = self.rules.schedule(&planned)?;
        n.add(self.notes, &pass1.required(), &pass1.divergences());
        self.spread(&pass1.value());
        self.schedule(out, &made_facts, n)
    }

    /// Fills the installments, what would meet each, and the penalties, over
    /// every payment once the default part is filled.
    fn schedule(
        &mut self,
        out: &mut EstimatedAnswer,
        made_facts: &estimated::Facts,
        n: &mut Notes,
    ) -> Result<(), Error> {
        let today = edate(self.in_.ctx.as_of())?;
        let before = self.rules.schedule(made_facts)?.value();
        out.to_meet = to_meet(&before, today);
        let all_facts = self.facts(false)?;
        let sched = self.rules.schedule(&all_facts)?;
        n.add(self.notes, &sched.required(), &sched.divergences());
        let sched = sched.value();
        if before.len() != sched.len() {
            return Err(Error::Installments {
                made: before.len(),
                all: sched.len(),
            });
        }
        let mut planned: HashSet<estimated::Date> = HashSet::new();
        let j = self.j.clone();
        if let Some(p) = self.in_.payments_for(&j) {
            for x in p.payments() {
                if let Ok(d) = edate(x.on())
                    && !x.made()
                    && x.amount().0 > 0
                {
                    planned.insert(d);
                }
            }
        }
        for (k, i) in sched.iter().enumerate() {
            out.installments.push(installment_of(
                i,
                &before[k],
                today,
                planned.contains(&i.due()),
            ));
        }
        let as_of = estimated::must_date(self.in_.ctx.year() + 1, estimated::Month::DECEMBER, 31);
        out.penalty = self.penalty(&all_facts, as_of, Some(n))?.into();
        out.penalty_without_planned = self.penalty(made_facts, as_of, None)?.into();
        Ok(())
    }

    /// Asks for the penalty as of a day, or none while it needs this year's
    /// tax. n, when given, collects its questions.
    fn penalty(
        &mut self,
        f: &estimated::Facts,
        as_of: estimated::Date,
        n: Option<&mut Notes>,
    ) -> Result<Option<EstimatedPenalty>, Error> {
        let pen = match self.rules.penalty(f, as_of) {
            Err(err) if err.is(&estimated::Error::NeedsThisYearTax) => return Ok(None),
            Err(err) => return Err(err.into()),
            Ok(p) => p,
        };
        if let Some(n) = n {
            n.add(self.notes, &pen.required(), &pen.divergences());
        }
        Ok(Some(penalty_of(&pen.value())))
    }

    /// Fills the tax paid model's open due dates with equal payments
    /// reaching the target: what the last installment still needs over
    /// every payment made and planned.
    fn spread(&mut self, installments: &[estimated::Installment]) {
        let j = self.j.clone();
        let Some(p) = self.in_.payments_for(&j) else {
            return;
        };
        let Some(last) = installments.last() else {
            return;
        };
        let dues: Vec<civil::Date> = installments
            .iter()
            .filter_map(|i| civil::new(i.due().year(), i.due().month().0, i.due().day()).ok())
            .collect();
        let open = p.open(&dues);
        if open.is_empty() {
            return;
        }
        p.spread(&open, last.needed_by() - last.credited_by());
    }

    /// Builds the rules' facts: this year's figures, last year's return,
    /// residence, withholding, the payments — made by as_of only, or every
    /// one that counts — and the elections.
    fn facts(&mut self, made_only: bool) -> Result<estimated::Facts, Error> {
        let mut b = estimated::new_facts(Jurisdiction::new(self.j.as_str()), self.in_.ctx.year());
        b.filing_status(self.in_.status);
        self.this_year(&mut b)?;
        self.prior_year(&mut b)?;
        self.residence(&mut b)?;
        self.withholding(&mut b)?;
        self.payments(&mut b, made_only)?;
        self.elections(&mut b)
    }

    fn this_year(&self, b: &mut estimated::FactsBuilder) -> Result<(), Error> {
        let mut fig = estimated::new_figures();
        if self.tax_known {
            fig.tax(self.tax);
        }
        fig.federal_agi(self.in_.federal_agi);
        if let Some(v) = self.state_agi() {
            fig.state_agi(v);
        }
        b.this_year(fig.build()?);
        Ok(())
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

    fn prior_year(&self, b: &mut estimated::FactsBuilder) -> Result<(), Error> {
        if let Some(pr) = self.prior_facts()? {
            b.prior_year(pr);
        }
        Ok(())
    }

    /// Passes the facts' residence periods, an open end read as December 31.
    fn residence(&self, b: &mut estimated::FactsBuilder) -> Result<(), Error> {
        let ctx = &self.in_.ctx;
        for res in &ctx.facts().residence {
            let (Ok(mut from), Ok(mut to)) =
                (civil::from_field(&res.from), civil::from_field(&res.to))
            else {
                continue;
            };
            if from.is_zero() {
                from = ctx.year_start();
            }
            if to.is_zero() {
                to = ctx.year_end();
            }
            let f = edate(from).unwrap_or_default();
            let t = edate(to).unwrap_or_default();
            let period = estimated::new_residence_period(res.state.as_str())
                .from(f)
                .to(t)
                .build()?;
            b.residence(&[period]);
        }
        Ok(())
    }

    /// Passes the jurisdiction's payments that count, and when payments
    /// could first be expected.
    fn payments(&mut self, b: &mut estimated::FactsBuilder, made_only: bool) -> Result<(), Error> {
        let j = self.j.clone();
        let Some(p) = self.in_.payments_for(&j) else {
            return Ok(());
        };
        for x in p.payments() {
            if made_only && !x.made() {
                continue;
            }
            let mut pb = estimated::new_payment();
            pb.amount(x.amount());
            if x.from_last_year() {
                pb.from_prior_year();
            } else {
                pb.on(edate(x.on())?);
            }
            b.paid(&[pb.build()?]);
        }
        let from = p.payments_from();
        if !from.is_zero() {
            b.requirement_first_met(edate(from)?);
        }
        Ok(())
    }

    /// Passes the withholding-dates election and a waiver request, and
    /// builds the facts.
    fn elections(&self, b: &mut estimated::FactsBuilder) -> Result<estimated::Facts, Error> {
        let mut el = estimated::new_elections();
        for x in &self.in_.ctx.facts().elections {
            match &x.choice {
                Some(Choice::WithholdingDates(w)) if w.jurisdiction == self.j && w.as_withheld => {
                    el.withholding_as_withheld();
                }
                Some(Choice::Waiver(w)) if w.jurisdiction == self.j => {
                    b.positions(positions_of(w)?);
                }
                _ => {}
            }
        }
        let e = el.build()?;
        Ok(b.elections(e).build()?)
    }

    fn prior_facts(&self) -> Result<Option<estimated::PriorReturn>, Error> {
        let p = &self.prior;
        if p.status == prior_year_return::Status::STATUS_UNSPECIFIED && !p.tax_set {
            return Ok(None);
        }
        let mut b = estimated::new_prior_return();
        if let Some(full) = p.full_year {
            b.full_year_resident(full);
        }
        if p.status == prior_year_return::Status::STATUS_NONE {
            return Ok(Some(b.filed(false).build()?));
        }
        b.filed(true);
        if p.tax_set {
            let mut f = estimated::new_figures();
            f.tax(p.tax);
            if p.agi_set {
                f.federal_agi(p.agi);
            }
            if p.withheld_set {
                f.withheld(p.withheld);
            }
            if p.state_agi_set {
                f.state_agi(p.state_agi);
            }
            if p.taxable_set {
                f.taxable_income(p.taxable);
            }
            b.figures(f.build()?);
        }
        Ok(Some(b.build()?))
    }

    /// Adds the jurisdiction's withholding on each item's date: federal or
    /// that state's, from the employer and other income items. For US,
    /// excess Social Security tax is withholding dated December 31, as Form
    /// 2210 line 6 counts it.
    fn withholding(&mut self, b: &mut estimated::FactsBuilder) -> Result<(), Error> {
        if !self.settled {
            self.withheld = self.work_out_withholding()?;
            self.settled = true;
        }
        b.withheld(&self.withheld);
        Ok(())
    }

    fn work_out_withholding(&self) -> Result<Vec<estimated::Withheld>, Error> {
        let mut by_date: HashMap<String, USCents> = HashMap::new();
        let mut dates: HashMap<String, civil::Date> = HashMap::new();
        for (on, v) in self.collect_withholding()? {
            *by_date.entry(on.to_string()).or_insert(USCents(0)) += v;
            dates.insert(on.to_string(), on);
        }
        let mut settled = settle(&by_date, &dates);
        self.to_boxes(&mut settled);
        let mut out = Vec::new();
        for w in &settled {
            let d = edate(w.on)?;
            out.push(estimated::new_withheld().on(d).amount(w.v).build()?);
        }
        Ok(out)
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

fn positions_of(w: &factsv1::WaiverRequest) -> Result<estimated::Positions, Error> {
    use waiver_request::Reason;
    let mut b = estimated::new_positions();
    let mut on = estimated::Date::default();
    if let Ok(d) = civil::from_field(&w.on)
        && !d.is_zero()
    {
        on = edate(d).unwrap_or_default();
    }
    match w.reason.as_known() {
        Some(Reason::REASON_UNSPECIFIED) => return Ok(estimated::Positions::default()),
        Some(Reason::REASON_NONE) => {
            b.waiver(estimated::WaiverKind::None);
        }
        Some(Reason::REASON_CASUALTY) => {
            b.waiver_for(estimated::WaiverCause::Casualty, on);
        }
        Some(Reason::REASON_DISASTER) => {
            b.waiver_for(estimated::WaiverCause::Disaster, on);
        }
        Some(Reason::REASON_RETIRED) => {
            b.waiver_for(estimated::WaiverCause::Retirement, on);
        }
        Some(Reason::REASON_DISABLED) => {
            b.waiver_for(estimated::WaiverCause::Disability, on);
        }
        None => {}
    }
    Ok(b.build()?)
}

fn edate(d: civil::Date) -> Result<estimated::Date, Error> {
    if d.is_zero() {
        return Err(Error::MissingDate);
    }
    Ok(estimated::new_date(
        d.year(),
        estimated::Month(d.month()),
        d.day(),
    )?)
}

fn wire(d: estimated::Date) -> buffa::MessageField<CalendarDate, buffa::Inline<CalendarDate>> {
    if d.is_zero() {
        return buffa::MessageField::none();
    }
    buffa::MessageField::some(CalendarDate {
        year: d.year() as i32,
        month: d.month().0 as i32,
        day: d.day() as i32,
        ..Default::default()
    })
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

    fn add(
        &mut self,
        scenario: &mut model::Notes,
        required: &[working::Required],
        divergences: &[estimated::Divergence],
    ) {
        for q in required {
            let key = format!("q{}:{}", q.position(), q.because());
            if !self.seen.insert(key) {
                continue;
            }
            self.questions.push(EstimatedQuestion {
                position: q.position().to_string(),
                because: q.because(),
                question: q.question(),
                effect: q.effect(),
                ..Default::default()
            });
            let (mut text, mut effect) = (q.question(), q.effect());
            if text.is_empty() {
                text = q.because();
            }
            if effect.is_empty() {
                effect = q.because();
            }
            scenario.ask(model::new_question(
                format!(
                    "estimated/{}/{}",
                    self.j,
                    q.position().to_string().replace(' ', "-")
                ),
                format!("estimated[{}]", self.j),
                text,
                effect,
                "facts",
                &[],
            ));
        }
        for d in divergences {
            let key = format!("d:{}", d.rule());
            if !self.seen.insert(key) {
                continue;
            }
            let uses = match d.uses() {
                working::Reading::Form => DivergenceReading::DIVERGENCE_READING_FORM,
                working::Reading::Statute => DivergenceReading::DIVERGENCE_READING_STATUTE,
                working::Reading::Unspecified => DivergenceReading::DIVERGENCE_READING_UNSPECIFIED,
            };
            self.divergences.push(EstimatedDivergence {
                rule: d.rule(),
                form: d.form(),
                form_source: d.form_source(),
                statute: d.statute(),
                statute_source: d.statute_source(),
                uses: uses.into(),
                affects_household: d.affects_household(),
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
    fn edate_refuses_a_missing_date() {
        assert_eq!(edate(civil::Date::default()), Err(Error::MissingDate));
        assert_eq!(
            edate(d(4, 15)).map(|x| (x.year(), x.month().0, x.day())),
            Ok((2026, 4, 15))
        );
        assert!(wire(estimated::Date::default()).is_unset());
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
