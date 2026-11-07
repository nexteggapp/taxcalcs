//! The withholding left unset on a job's expected items. Ported from
//! withhold.go.

use buffa::EnumValue;
use filing::Status;
use money::USCents;
use proto::federalv1::FilingStatus;
use proto::scenariosv1::{
    __buffa::oneof::work_place::Place, ItemKind, ItemStatus, SupplementalMethod,
};

use super::fill::of_fine;
use super::pay::{HSA, Pay, StateLine, set_line};
use super::{Draft, Job};
use crate::internal::civil::Date;
use crate::model::{self, Error, new_question};
use crate::payroll::{self, W4};

/// One state the work on a date is done in, and its share.
#[derive(Clone, Debug)]
pub(super) struct WorkShare {
    pub(super) state: String,
    pub(super) days: USCents,
    pub(super) total: USCents,
}

fn whole(state: &str) -> WorkShare {
    WorkShare {
        state: state.to_string(),
        days: USCents(1),
        total: USCents(1),
    }
}

fn payroll_error(e: ustaxcalc::Error) -> Error {
    Error::Expansion(e.to_string())
}

impl<'a> Job<'a> {
    /// Where the job's work is done on a date, by `work`: one state; the
    /// state lived in that day; or each state of a split by its days. Never
    /// the copied paystub's states.
    pub(super) fn work_states(&self, on: Date) -> Vec<WorkShare> {
        match &self.m.work.place {
            Some(Place::State(s)) if !s.is_empty() => return vec![whole(s)],
            Some(Place::Split(split)) => {
                let total: USCents = split.days.iter().map(|s| USCents(i64::from(s.days))).sum();
                let mut out = Vec::new();
                for s in &split.days {
                    if total.0 > 0 {
                        out.push(WorkShare {
                            state: s.state.clone(),
                            days: USCents(i64::from(s.days)),
                            total,
                        });
                    }
                }
                return out;
            }
            _ => {}
        }
        let s = self.ctx.lived_in(on);
        if !s.is_empty() {
            return vec![whole(&s)];
        }
        if let Some(r) = self.regular_stub()
            && let Some(first) = r.this.states.first()
        {
            return vec![whole(&first.state)];
        }
        Vec::new()
    }

    /// The W-4 federal withholding is figured from: the model's, else the
    /// latest paystub's filing status alone, else single alone.
    fn w4(&self) -> Result<W4, ustaxcalc::Error> {
        let Some(f) = self.m.w4.as_option().and_then(|w| w.form.as_option()) else {
            let mut status = Status::Single;
            if let Some(r) = self.regular_stub()
                && r.status != FilingStatus::FILING_STATUS_UNSPECIFIED
            {
                status = status_of(r.status);
            }
            return payroll::new_w4(status).build();
        };
        let mut b = payroll::new_w4(status_of(f.filing_status));
        b.dependents(model::value(&f.dependents))
            .other_income(model::value(&f.other_income))
            .deductions(model::value(&f.deductions))
            .extra(model::value(&f.extra_withholding));
        if f.multiple_jobs {
            b.multiple_jobs();
        }
        if f.exempt {
            b.exempt();
        }
        b.build()
    }

    /// Computes every withholding line left unset on the expected items, in
    /// date order: federal income tax by Publication 15-T or the
    /// supplemental rules, Social Security up to the wage base and Medicare
    /// with the extra 0.9% over $200,000 for this job, and each state's.
    pub(super) fn withhold(&mut self) -> Result<(), Error> {
        self.items
            .sort_by(|a, b| a.date.compare(b.date).cmp(&0).then_with(|| a.id.cmp(&b.id)));
        let w4 = self
            .w4()
            .map_err(|e| Error::Expansion(format!("{}.w4: {e}", self.path())))?;
        let periods = super::frequency_of(self.m.pay_frequency).periods();
        let (mut fica_before, mut supplemental_before) = (USCents(0), USCents(0));
        for i in 0..self.items.len() {
            let d = &self.items[i];
            if d.status == ItemStatus::ITEM_STATUS_EXPECTED
                && d.kind != ItemKind::ITEM_KIND_ADJUSTMENT
            {
                self.with_item(i, |j, d| {
                    j.withhold_one(d, &w4, periods, fica_before, supplemental_before)
                })?;
            }
            let d = &self.items[i];
            fica_before += d.p.fica();
            if d.kind == ItemKind::ITEM_KIND_BONUS || d.kind == ItemKind::ITEM_KIND_VEST {
                supplemental_before += d.p.gross.v;
            }
        }
        Ok(())
    }

    fn withhold_one(
        &mut self,
        d: &mut Draft,
        w4: &W4,
        periods: i64,
        fica_before: USCents,
        supplemental_before: USCents,
    ) -> Result<(), Error> {
        let year = self.ctx.year();
        let ss = payroll::social_security(year, d.p.fica(), fica_before).map_err(payroll_error)?;
        if d.compute_ss || d.p.ss.v > ss {
            // A copied or typed line stops at the wage base for this
            // employer.
            d.p.ss = set_line(ss);
            d.estimate("paycheck.social_security_withheld");
        }
        if d.compute_medicare {
            d.p.medicare = set_line(payroll::medicare(d.p.fica(), fica_before));
            d.estimate("paycheck.medicare_withheld");
        }
        if d.compute_federal {
            let v = self.federal(d, w4, periods, supplemental_before)?;
            d.p.federal = set_line(v);
            d.estimate("paycheck.federal_withheld");
        }
        if d.compute_states {
            self.states(d);
        }
        Ok(())
    }

    /// The federal income tax withheld from one expected item.
    fn federal(
        &self,
        d: &Draft,
        w4: &W4,
        periods: i64,
        supplemental_before: USCents,
    ) -> Result<USCents, Error> {
        let year = self.ctx.year();
        if !d.supplemental {
            if periods == 0 {
                return Err(Error::Expansion(format!(
                    "{}.pay_frequency is not set",
                    self.path()
                )));
            }
            let (v, _) = payroll::federal_withholding(year, w4, d.p.taxable(), periods)
                .map_err(payroll_error)?;
            return Ok(v);
        }
        let amount = d.p.gross.v;
        if let Some(r) = d.federal_rate {
            return Ok(of_fine(r, amount));
        }
        if self.m.supplemental == SupplementalMethod::SUPPLEMENTAL_METHOD_AGGREGATE && periods > 0 {
            if let Some(with) = self.period_paycheck(d.date) {
                let (both, _) =
                    payroll::federal_withholding(year, w4, with.p.taxable() + amount, periods)
                        .map_err(payroll_error)?;
                return Ok(USCents(0).max(both - with.p.federal.v));
            }
            let (v, _) =
                payroll::federal_withholding(year, w4, amount, periods).map_err(payroll_error)?;
            return Ok(v);
        }
        Ok(payroll::flat_supplemental(amount, supplemental_before))
    }

    /// The regular paycheck a one-off is paid with: the first on or after
    /// its date.
    fn period_paycheck(&self, on: Date) -> Option<&Draft> {
        self.items
            .iter()
            .find(|d| d.kind == ItemKind::ITEM_KIND_PAYCHECK && !d.date.before(on))
    }

    /// Writes an expected item's state lines for where the work is done on
    /// its date. A line a part typed wins; then the employer's
    /// state_withholding, for a regular paycheck; then the latest paystub's
    /// rate for that state. A state that taxes no wages has its wages and
    /// nothing withheld. Failing all, the result asks for the amount per
    /// paycheck and counts nothing withheld.
    pub(super) fn states(&mut self, d: &mut Draft) {
        let mut lines = Vec::new();
        for w in self.work_states(d.date) {
            let diff = d.state_diff.get(&w.state).copied().unwrap_or_default();
            let Some(mut l) = typed_line(&d.typed_states, &w.state) else {
                // A state that taxes no wages still has its wages, with
                // nothing withheld.
                let wages = model::scale(state_base(&w.state, &d.p) + diff, w.days, w.total);
                let withheld = self.state_withheld(d, &w.state, wages);
                lines.push(StateLine {
                    state: w.state.clone(),
                    wages: set_line(wages),
                    withheld: set_line(withheld),
                });
                d.estimate(&format!("paycheck.states[{}].state_income", w.state));
                if !payroll::no_wage_tax(&w.state) {
                    d.estimate(&format!("paycheck.states[{}].tax_withheld", w.state));
                }
                continue;
            };
            if !l.wages.set {
                l.wages = set_line(model::scale(
                    state_base(&l.state, &d.p) + diff,
                    w.days,
                    w.total,
                ));
                d.estimate(&format!("paycheck.states[{}].state_income", l.state));
            }
            if !l.withheld.set {
                l.withheld = set_line(self.state_withheld(d, &l.state, l.wages.v));
                d.estimate(&format!("paycheck.states[{}].tax_withheld", l.state));
            }
            lines.push(l);
        }
        d.p.states = lines;
    }

    /// One state's withholding on an expected item: a typed rate for a
    /// one-off; the employer's state_withholding for a regular paycheck; the
    /// latest paystub's rate for that state; for a new state, the latest
    /// paystub's rate across its states, asked about.
    fn state_withheld(&mut self, d: &Draft, state: &str, wages: USCents) -> USCents {
        if payroll::no_wage_tax(state) {
            return USCents(0);
        }
        if d.supplemental
            && let Some(r) = d.state_rate
        {
            return of_fine(r, wages);
        }
        if !d.supplemental && d.kind == ItemKind::ITEM_KIND_PAYCHECK {
            for s in &self.m.state_withholding {
                if s.state == state {
                    return model::value(&s.per_paycheck);
                }
            }
        }
        if let Some(r) = self.regular_stub()
            && let Some(l) = r.this.state(state)
            && l.wages.v.0 > 0
        {
            return model::scale(wages, l.withheld.v, l.wages.v);
        }
        // A new state: taxcalcs has no state's withholding method, so the
        // latest paystub's rate across its states stands in, and the result
        // asks for the amount.
        let (mut withheld, mut of) = (USCents(0), USCents(0));
        if let Some(r) = self.regular_stub() {
            for l in &r.this.states {
                withheld += l.withheld.v;
                of += l.wages.v;
            }
        }
        let mut effect = format!("Nothing is counted as withheld for {state} until you say.");
        if of.0 > 0 {
            effect = format!(
                "{state} withholding is counted at your latest paystub's state rate until you say."
            );
        }
        self.notes.ask(new_question(
            model::item_id(&[&self.item_prefix(), "state-withholding", state]),
            self.item_prefix(),
            format!(
                "How much {state} income tax does {} withhold from each paycheck?",
                self.m.name
            ),
            effect,
            format!("{}.state_withholding", self.path()),
            &[],
        ));
        if of.0 == 0 {
            return USCents(0);
        }
        model::scale(wages, withheld, of)
    }
}

/// Maps a form's filing status; unset reads as single.
fn status_of(s: EnumValue<FilingStatus>) -> Status {
    match s.as_known() {
        Some(FilingStatus::FILING_STATUS_MARRIED_FILING_JOINTLY) => Status::MarriedFilingJointly,
        Some(FilingStatus::FILING_STATUS_MARRIED_FILING_SEPARATELY) => {
            Status::MarriedFilingSeparately
        }
        Some(FilingStatus::FILING_STATUS_HEAD_OF_HOUSEHOLD) => Status::HeadOfHousehold,
        Some(FilingStatus::FILING_STATUS_QUALIFYING_SURVIVING_SPOUSE) => Status::SurvivingSpouse,
        _ => Status::Single,
    }
}

/// The line a part typed for a state, if any.
fn typed_line(lines: &[StateLine], state: &str) -> Option<StateLine> {
    lines.iter().find(|l| l.state == state).cloned()
}

/// The part of a paycheck a state's wages follow: box 1, and for
/// California box 1 with the payroll HSA, which California doesn't exclude.
/// A paystub's state wages are this and a difference of their own, so a
/// paycheck to come with another HSA line keeps California's wages with it.
pub(super) fn state_base(state: &str, p: &Pay) -> USCents {
    if state == "CA" {
        return p.taxable() + p.pretax_line(HSA).v;
    }
    p.taxable()
}
