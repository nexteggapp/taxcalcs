//! Expands an EmployerModel into its paychecks, by the rules in
//! docs/models.md. Ported from the Go package `taxyear/model/employer`.
//!
//! [`expand`] reads the job's evidence and fills its pay dates;
//! [`max_out`] then sets the plan line on a person's expected paychecks
//! across their jobs; [`hold_hsa`] holds their HSA contributions to the
//! limit; [`Job::projection`] computes the withholding left unset, in date
//! order, and the totals.
//!
//! A job's items are held by index: a paystub, a filled pay date or a
//! max-out share names its item by its position in the job's items, which
//! only the withholding pass, after the last of those, reorders.
//!
//! Errors are [`Error::Expansion`] with Go's text.

use std::collections::HashMap;

use buffa::EnumValue;
use money::{FineRate, USCents};
use proto::factsv1::PaycheckKind;
use proto::federalv1::{FilingStatus, FormW2};
use proto::scenariosv1::{
    self, EmployerModel, ItemKind, ItemStatus, Origin, Override, PayFrequency,
    employer_part::from_deposits::Deposit as DepositProto,
};

use super::{Context, Error, Notes};
use crate::internal::civil::{self, Date, Weekday};
use crate::model;
use crate::payroll::Frequency;

mod evidence;
mod fill;
mod hsa;
mod maxout;
mod pay;
mod projection;
mod round;
mod w2;
mod withhold;

pub use hsa::hold_hsa;
pub use maxout::max_out;
pub use projection::override_prefix;

use pay::{Line, Pay, StateLine, sum_pay};

/// One employer model being expanded. It is made by [`expand`], changed
/// only by [`max_out`] and [`hold_hsa`], and read by [`Job::projection`].
#[derive(Clone, Debug)]
pub struct Job<'a> {
    ctx: &'a Context,
    m: &'a EmployerModel,
    slots: Vec<Date>,
    items: Vec<Draft>,
    notes: Notes,

    stubs: Vec<Stub>,
    deposits: Vec<Deposit>,
    w2s: Vec<&'a FormW2>,
    w2_parts: Vec<String>,

    /// The latest evidence date; zero with no evidence.
    latest: Date,
    /// The latest regular paystub, which SameAsLatest repeats, by index in
    /// stubs.
    regular: Option<usize>,
    /// The last pay date, known or assumed; zero for none.
    end: Date,
    /// The pay dates evidence fills, by date, each an index in items. A
    /// paystub can fill a pay date after its own date, so these are
    /// skipped when the assumed parts fill the rest.
    filled: HashMap<String, usize>,
    /// The date of the latest paystub with a year-to-date column, through
    /// which its ADJUSTMENT covers the pay dates; zero for none.
    ytd_at: Date,

    /// The overrides' workings.
    workings: Vec<scenariosv1::Working>,

    done: bool,
    /// The job's HSA contributions over the person's limit: taken off the
    /// paychecks to come, and on paychecks already made ([`hold_hsa`]).
    hsa_in_wages: USCents,
    hsa_in_income: USCents,
    /// The part of hsa_in_income on paychecks a YearTotal part fills, which
    /// the split would otherwise have taken off them.
    hsa_typed: USCents,
    /// What max-out would set on each paycheck a YearTotal part fills, by
    /// item id; it sets none of them.
    plan_typed: HashMap<String, maxout::PlanShare>,
}

/// One item while the job is expanded.
#[derive(Clone, Debug, Default)]
struct Draft {
    id: String,
    date: Date,
    status: ItemStatus,
    kind: ItemKind,
    origin: Origin,
    p: Pay,
    take_home: Line,
    /// Fields filled by a rule, by path in the item.
    estimated: Vec<String>,

    /// What the withholding pass computes on an expected item.
    compute_federal: bool,
    compute_ss: bool,
    compute_medicare: bool,
    compute_states: bool,
    /// Marks a bonus or vest withheld by the supplemental rules, and
    /// federal_rate a typed rate for it.
    supplemental: bool,
    federal_rate: Option<FineRate>,
    state_rate: Option<FineRate>,
    /// Marks an expected regular paycheck max-out may set.
    plan_slot: bool,
    /// Marks a paycheck a YearTotal part fills, whose figures the HSA
    /// limit doesn't change.
    typed_year: bool,
    /// State lines a part typed, which win.
    typed_states: Vec<StateLine>,
    /// Each state's wages less its base on the paystub copied: box 1, and
    /// for California box 1 with the payroll HSA (see state_base).
    state_diff: HashMap<String, USCents>,
}

impl Draft {
    fn estimate(&mut self, path: &str) {
        if self.estimated.iter().any(|e| e == path) {
            return;
        }
        self.estimated.push(path.to_string());
    }
}

/// One usable paystub.
#[derive(Clone, Debug)]
struct Stub {
    id: String,
    part: String,
    date: Date,
    kind: EnumValue<PaycheckKind>,
    this: Pay,
    ytd: Pay,
    has_ytd: bool,
    status: EnumValue<FilingStatus>,
    matches: scenariosv1::Match,
    /// The item the stub is, by index in items; set by matching.
    item: usize,
    /// The deposits paired with it, by index in deposits.
    paired: Vec<usize>,
}

/// One payroll deposit.
#[derive(Clone, Debug)]
struct Deposit {
    id: String,
    part: String,
    date: Date,
    amount: USCents,
    matches: scenariosv1::Match,
    used: bool,
}

impl Deposit {
    fn of(dep: &DepositProto, part: &str, date: Date) -> Deposit {
        Deposit {
            id: dep.id.clone(),
            part: part.to_string(),
            date,
            amount: model::value(&dep.amount),
            matches: (*dep.matches).clone(),
            used: false,
        }
    }
}

impl<'a> Job<'a> {
    /// The model's id.
    pub fn id(&self) -> &str {
        &self.m.id
    }

    /// The id of the person whose job it is.
    pub fn person(&self) -> &str {
        &self.m.person
    }

    /// What the expansion asked and left out.
    pub fn notes(&self) -> &Notes {
        &self.notes
    }
}

/// Reads an employer model's evidence and fills its pay dates by the models
/// doc's order: a whole-year part, then evidence, then an assumed period
/// part, then the default part. Withholding left unset is computed by
/// [`Job::projection`], after [`max_out`] has had its turn. overrides are
/// the scenario's overrides naming this model's figures so far.
pub fn expand<'a>(
    ctx: &'a Context,
    m: &'a EmployerModel,
    overrides: &[Override],
) -> Result<Job<'a>, Error> {
    let mut j = Job {
        ctx,
        m,
        slots: Vec::new(),
        items: Vec::new(),
        notes: Notes::default(),
        stubs: Vec::new(),
        deposits: Vec::new(),
        w2s: Vec::new(),
        w2_parts: Vec::new(),
        latest: Date::default(),
        regular: None,
        end: Date::default(),
        filled: HashMap::new(),
        ytd_at: Date::default(),
        workings: Vec::new(),
        done: false,
        hsa_in_wages: USCents(0),
        hsa_in_income: USCents(0),
        hsa_typed: USCents(0),
        plan_typed: HashMap::new(),
    };
    let first = civil::from_field(&m.first_pay_date)
        .map_err(|e| Error::Expansion(format!("employers[{}].first_pay_date: {e}", m.id)))?;
    let starts = civil::from_field(&m.starts)
        .map_err(|e| Error::Expansion(format!("employers[{}].starts: {e}", m.id)))?;
    j.slots = pay_dates_of(ctx.year(), frequency_of(m.pay_frequency), first, starts);
    j.read_evidence()?;
    j.match_evidence();
    j.year_to_date();
    // An override of a figure so far is in the evidence a year total is
    // measured against. A W-2 states the year, so with one the override
    // adjusts what the W-2 gives.
    let early = j.w2s.is_empty();
    if early {
        j.apply_overrides(overrides)?;
    }
    j.fill()?;
    j.read_w2s();
    if !early {
        j.apply_overrides(overrides)?;
    }
    Ok(j)
}

fn frequency_of(f: EnumValue<PayFrequency>) -> Frequency {
    match f.as_known() {
        Some(PayFrequency::PAY_FREQUENCY_WEEKLY) => Frequency::Weekly,
        Some(PayFrequency::PAY_FREQUENCY_BIWEEKLY) => Frequency::Biweekly,
        Some(PayFrequency::PAY_FREQUENCY_SEMI_MONTHLY) => Frequency::SemiMonthly,
        Some(PayFrequency::PAY_FREQUENCY_MONTHLY) => Frequency::Monthly,
        _ => Frequency::Unspecified,
    }
}

/// Returns a job's pay dates in the year: from first_pay_date by the
/// frequency, none before starts. Twice-monthly and monthly pay dates on a
/// weekend move to the Friday before, as payroll moves them.
pub fn pay_dates(year: i64, f: EnumValue<PayFrequency>, first: Date, starts: Date) -> Vec<Date> {
    pay_dates_of(year, frequency_of(f), first, starts)
}

fn pay_dates_of(year: i64, f: Frequency, first: Date, starts: Date) -> Vec<Date> {
    let jan1 = civil::must(year, 1, 1);
    let dec31 = civil::must(year, 12, 31);
    let mut out = Vec::new();
    match f {
        Frequency::Weekly | Frequency::Biweekly => {
            let step = if f == Frequency::Biweekly { 14 } else { 7 };
            let mut at = first;
            if at.is_zero() {
                // The first Friday of the year.
                at = jan1.add_days((Weekday::Friday as i64 - jan1.weekday() as i64 + 7) % 7);
            }
            while !at.before(jan1) {
                at = at.add_days(-step);
            }
            at = at.add_days(step);
            while !at.after(dec31) {
                out.push(at);
                at = at.add_days(step);
            }
        }
        Frequency::SemiMonthly => {
            let (d1, d2) = semi_monthly_days(first);
            for m in 1..=12 {
                out.push(day_in(year, m, d1).business_day_on_or_before());
                out.push(day_in(year, m, d2).business_day_on_or_before());
            }
        }
        Frequency::Monthly => {
            let mut day = 31;
            if !first.is_zero()
                && first.day() < civil::end_of_month(first.year(), first.month()).day()
            {
                day = first.day();
            }
            for m in 1..=12 {
                out.push(day_in(year, m, day).business_day_on_or_before());
            }
        }
        Frequency::Unspecified => {}
    }
    out.retain(|d| starts.is_zero() || !d.before(starts));
    out
}

/// The two days of the month a twice-monthly job pays: the 15th and the
/// last day, unless first_pay_date names another day d before the 15th,
/// which pays d and d + 15. 31 stands for the month's last day.
fn semi_monthly_days(first: Date) -> (i64, i64) {
    if first.is_zero() || first.day() >= 15 {
        return (15, 31);
    }
    (first.day(), first.day() + 15)
}

/// Day of the month, or its last day when the month is shorter.
fn day_in(year: i64, m: i64, day: i64) -> Date {
    let end = civil::end_of_month(year, m);
    if day >= end.day() {
        return end;
    }
    civil::must(year, m, day)
}

fn kind_word(k: EnumValue<PaycheckKind>) -> &'static str {
    match k.as_known() {
        Some(PaycheckKind::PAYCHECK_KIND_BONUS) => "bonus",
        Some(PaycheckKind::PAYCHECK_KIND_VEST) => "vest",
        Some(PaycheckKind::PAYCHECK_KIND_FINAL) => "final",
        _ => "regular",
    }
}

fn item_kind(k: EnumValue<PaycheckKind>) -> ItemKind {
    match k.as_known() {
        Some(PaycheckKind::PAYCHECK_KIND_BONUS) => ItemKind::ITEM_KIND_BONUS,
        Some(PaycheckKind::PAYCHECK_KIND_VEST) => ItemKind::ITEM_KIND_VEST,
        Some(PaycheckKind::PAYCHECK_KIND_FINAL) => ItemKind::ITEM_KIND_FINAL,
        _ => ItemKind::ITEM_KIND_PAYCHECK,
    }
}

impl<'a> Job<'a> {
    /// How far a paystub or deposit may be from a pay date and still be
    /// that pay date's.
    fn half_period(&self) -> i64 {
        match frequency_of(self.m.pay_frequency) {
            Frequency::Weekly => 3,
            Frequency::Biweekly | Frequency::SemiMonthly => 7,
            _ => 15,
        }
    }

    /// The model's path in the scenario's inputs.
    fn path(&self) -> String {
        format!("model.employers[{}]", self.m.id)
    }

    /// The start of every item id: "employers/acme".
    fn item_prefix(&self) -> String {
        model::item_id(&["employers", &self.m.id])
    }

    fn slot_id(&self, d: Date) -> String {
        model::item_id(&[&self.item_prefix(), &d.to_string(), "regular"])
    }

    /// The items read from evidence, by index.
    fn made(&self) -> Vec<usize> {
        (0..self.items.len())
            .filter(|&i| self.items[i].status == ItemStatus::ITEM_STATUS_MADE)
            .collect()
    }

    fn sum_of(&self, ds: &[usize]) -> Pay {
        sum_pay(ds.iter().map(|&i| &self.items[i].p))
    }

    fn sum_all(&self) -> Pay {
        sum_pay(self.items.iter().map(|d| &d.p))
    }

    fn regular_stub(&self) -> Option<&Stub> {
        self.regular.map(|i| &self.stubs[i])
    }

    /// Runs f on the item at i, taken out of the items while it runs, so f
    /// may read and change the job.
    fn with_item<T>(&mut self, i: usize, f: impl FnOnce(&mut Self, &mut Draft) -> T) -> T {
        let mut d = std::mem::take(&mut self.items[i]);
        let out = f(self, &mut d);
        self.items[i] = d;
        out
    }
}

fn abs(n: i64) -> i64 {
    n.abs()
}
