//! Virginia's estimated tax rules, Code of Virginia §§ 58.1-490 to 58.1-492
//! and § 58.1-15, as the shared [`EstimatedTax`] interface.
//!
//! Virginia follows the federal outline with different dates and a
//! different order. The four installments are due May 1, June 15, September
//! 15 and January 15. Each is measured against the lesser of 90% of this
//! year's tax and 100% of last year's. A payment made on an installment date
//! pays that installment before any earlier shortfall. Four exceptions cancel
//! the addition to tax for an installment without lowering its shortfall, and
//! there is no addition when the year's shortfalls total $1,000 or less. The
//! addition is interest at the federal underpayment rate plus 2%.
//!
//! The figure follows the 2026 Form 760ES and the 2025 Form 760C, the latest
//! published, and the result's Divergences say where the Code reads
//! otherwise. One rule on the 2025 Form 760C predates the law for 2026: 2026
//! Acts c. 7 raised the small-underpayment exception from $150 to $1,000 for
//! taxable years from January 1, 2026, so the law's $1,000 is computed.

use std::any::Any;

use estimated::{
    Basis, Calendar, Date, Error, EstimatedTax, Explanation, FactorSeries, Facts, Installment,
    Penalty, PenaltyPart, RateSeries, Requirement, Rounding, TargetOption, WaiverKind,
};
use money::{DOLLAR, Rate, USCents, percent};
use working::{Divergence, Notes, PositionKind, Reading};

/// Virginia's postal code.
const JURISDICTION: estimated::Jurisdiction = estimated::VIRGINIA;

/// The facts Virginia's rules read that no other jurisdiction does. Carry
/// them in [`estimated::FactsBuilder::state`].
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct EstimatedFacts {
    prior_year_income_tax: Option<USCents>,
    income_to_date_tax: Option<Vec<USCents>>,
    merchant_seaman: Rate,
    seaman_answered: bool,
}

impl EstimatedFacts {
    /// The tax on last year's income at this year's rates and personal
    /// exemptions, less last year's Spouse Tax Adjustment and credits: the
    /// Form 760C Exception 2 Worksheet, line 7. Payments that reach it by
    /// each date cancel the addition under Exception 2, whether or not a
    /// return was filed last year.
    pub fn prior_year_income_tax(&self) -> Option<USCents> {
        self.prior_year_income_tax
    }

    /// The Virginia tax on the taxable income actually received from January
    /// 1 to April 30, May 31 and August 31: Form 760C line 19f, one figure for
    /// each period. Exception 4 compares payments with 90% of it. `None` when
    /// the caller did not give it.
    pub fn income_to_date_tax(&self) -> Option<Vec<USCents>> {
        self.income_to_date_tax.clone()
    }

    /// The share of gross income from working as a merchant seaman, which
    /// counts with farming and fishing toward the two-thirds that puts a
    /// filer under Form 760F, and whether the caller said: `(share,
    /// answered)`.
    pub fn merchant_seaman_share(&self) -> (Rate, bool) {
        (self.merchant_seaman, self.seaman_answered)
    }
}

/// Builds [`EstimatedFacts`].
#[derive(Clone, Debug, Default)]
pub struct EstimatedFactsBuilder {
    e: EstimatedFacts,
}

/// Starts Virginia's extras.
pub fn new_estimated_facts() -> EstimatedFactsBuilder {
    EstimatedFactsBuilder::default()
}

impl EstimatedFactsBuilder {
    /// Gives the tax on last year's income at this year's rates.
    pub fn prior_year_income_tax(&mut self, a: USCents) -> &mut Self {
        self.e.prior_year_income_tax = Some(a);
        self
    }

    /// Gives the tax on the income received to April 30, May 31 and August
    /// 31.
    pub fn income_to_date_tax(
        &mut self,
        apr30: USCents,
        may31: USCents,
        aug31: USCents,
    ) -> &mut Self {
        self.e.income_to_date_tax = Some(vec![apr30, may31, aug31]);
        self
    }

    /// Gives the share of gross income from work as a merchant seaman.
    pub fn merchant_seaman_share(&mut self, share: Rate) -> &mut Self {
        (self.e.merchant_seaman, self.e.seaman_answered) = (share, true);
        self
    }

    /// Refuses a negative tax and a share outside 0% to 100%.
    pub fn build(&self) -> Result<EstimatedFacts, Error> {
        let e = self.e.clone();
        if e.prior_year_income_tax.is_some_and(|a| a.0 < 0) {
            return Err(Error::Invalid(
                "virginiataxcalc: the tax on last year's income cannot be negative".into(),
            ));
        }
        if e.income_to_date_tax.iter().flatten().any(|t| t.0 < 0) {
            return Err(Error::Invalid(
                "virginiataxcalc: the tax on income to date cannot be negative".into(),
            ));
        }
        if e.merchant_seaman.0 < 0 || e.merchant_seaman > percent(100) {
            return Err(Error::Invalid(format!(
                "virginiataxcalc: a merchant seaman share of {} is not a share",
                e.merchant_seaman
            )));
        }
        Ok(e)
    }
}

/// The rate for a run of days, and how the form charges it: at a daily
/// factor the form prints, or at the annual rate times the days over 365
/// where it prints none.
#[derive(Clone, Debug, Default)]
pub(crate) struct RatePeriodOf {
    from: Date,
    to: Date,
    rate: Rate,
    printed: bool,
    factor: FactorSeries,
    series: RateSeries,
}

/// Builds a period at a printed daily factor, or at the rate alone when
/// `factor` is empty.
pub(crate) fn new_rate_period(
    from: Date,
    to: Date,
    rate: Rate,
    factor: &str,
    source: &str,
) -> Result<RatePeriodOf, Error> {
    let mut p = RatePeriodOf {
        from,
        to,
        rate,
        ..RatePeriodOf::default()
    };
    if !factor.is_empty() {
        let f = estimated::parse_factor(factor)?;
        p.printed = true;
        p.factor = estimated::new_factor_series(Rounding::RoundToCent)
            .period(from, to, f, rate, source)
            .build()?;
    } else {
        p.series = estimated::new_rate_series()
            .period(from, to, rate, source)
            .build()?;
    }
    Ok(p)
}

/// One tax year's published figures.
pub(crate) struct EstimatedYear {
    pub(crate) year: i64,

    /// § 58.1-492(A)(1): the underpayment is measured against the lesser of
    /// 90% of this year's tax and 100% of last year's.
    pub(crate) this_year_share: Rate,
    pub(crate) prior_year_share: Rate,

    /// Form 760ES Section I: payments are required when tax after
    /// withholding and credits is expected to be more than it.
    pub(crate) filing_threshold: USCents,

    /// § 58.1-492(D)(3): no addition when the year's installment
    /// underpayments total this or less.
    pub(crate) small_underpayment: USCents,

    /// The VAGI below which no return and no estimated payments are
    /// required.
    pub(crate) agi_single: USCents,
    pub(crate) agi_joint: USCents,
    pub(crate) agi_separate: USCents,

    pub(crate) dues: Vec<Date>,

    /// The first days of the Form 760ES payment schedule's second, third and
    /// fourth rows: a requirement first met on or after `later_groups[i]`
    /// starts at installment i+2.
    pub(crate) later_groups: Vec<Date>,

    pub(crate) calendar: Calendar,
    pub(crate) file_and_pay_by: Date,
    pub(crate) stop: Date,

    /// The Form 760C Exception 3 and 4 periods. The fourth, which
    /// [`Facts`] requires, ends the year and is not read.
    pub(crate) period_ends: Vec<Date>,

    /// Form 760C lines 16 and 17: the share of last year's figure that
    /// payments must reach by each date.
    pub(crate) exception_shares: Vec<Rate>,

    /// Form 760C line 18g, and `income_to_date` line 19g.
    pub(crate) annualised: Vec<Rate>,
    pub(crate) income_to_date: Rate,

    pub(crate) rates: Vec<RatePeriodOf>,
}

/// Divides withholding into four equal parts, Form 760C line 8.
fn quarters() -> [Rate; 4] {
    [percent(25), percent(25), percent(25), percent(25)]
}

fn year(year: i64) -> Result<EstimatedYear, Error> {
    match year {
        2026 => crate::estimated_y2026::year_2026(),
        _ => Err(estimated::no_rules(&JURISDICTION, year)),
    }
}

/// Returns Virginia's estimated tax rules for a tax year, or
/// [`Error::NoRules`] for a year this build does not carry.
pub fn estimated_tax(year_: i64) -> Result<Box<dyn EstimatedTax>, Error> {
    Ok(Box::new(VirginiaEstimated { y: year(year_)? }))
}

/// The holiday calendar the year's rules move dates by, or an empty one for
/// a year this build does not carry. Go exports it to its tests only, so
/// they can show a date moving: no 2026 date the rules use falls on a
/// weekend or holiday.
#[doc(hidden)]
pub fn calendar(year_: i64) -> Calendar {
    year(year_).map(|y| y.calendar).unwrap_or_default()
}

/// One period of the year's rates, for the tests.
#[doc(hidden)]
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct RatePeriod {
    /// The first day.
    pub from: Date,
    /// The last day.
    pub to: Date,
    /// The annual rate.
    pub rate: Rate,
    /// The printed daily factor, or empty.
    pub factor: String,
}

/// The year's rate periods, for the tests; empty for a year this build does
/// not carry.
#[doc(hidden)]
pub fn rates(year_: i64) -> Vec<RatePeriod> {
    let Ok(y) = year(year_) else {
        return Vec::new();
    };
    y.rates
        .iter()
        .map(|p| RatePeriod {
            from: p.from,
            to: p.to,
            rate: p.rate,
            factor: if p.printed {
                p.factor
                    .periods()
                    .first()
                    .map(|f| f.factor().to_string())
                    .unwrap_or_default()
            } else {
                String::new()
            },
        })
        .collect()
}

struct VirginiaEstimated {
    y: EstimatedYear,
}

/// One of Form 760C's Part II exceptions: the running total that cancels the
/// addition for an installment, for the installments it covers.
#[derive(Clone, Debug)]
struct Exception {
    name: String,
    needed: Vec<USCents>,
    covers: usize,
}

/// What the three questions share.
#[derive(Default)]
struct Assessment {
    requirement: Requirement,
    exempt: bool,
    dues: Vec<Date>,

    /// Whether this year's tax was given. `regular_known` is whether the
    /// § 58.1-492(A)(1) installments could be measured at all: without this
    /// year's tax they are measured against last year's, which can only be
    /// too high, and with no return last year they cannot be measured.
    tax_known: bool,
    regular_known: bool,

    /// Each installment § 58.1-492(A)(1) and Form 760C line 7 require, and
    /// `lesser` their running total.
    regular: Vec<USCents>,
    lesser: Vec<USCents>,

    exceptions: Vec<Exception>,

    /// The installment a late starter's requirement starts at, from 0.
    first: i64,
    excused_january: bool,

    notes: Notes,
}

/// For each divergence, whether its readings give these facts different
/// figures or answers.
#[derive(Default)]
struct Affects {
    required: bool,
    small: bool,
    january: bool,
    late_start: bool,
    late_exceptions: bool,
    withholding: bool,
    daily_rate: bool,
}

/// Go's `%T` of a jurisdiction's extras, for the types a caller is likely to
/// pass by mistake.
fn type_name(v: &(dyn Any + Send + Sync)) -> &'static str {
    if v.is::<String>() || v.is::<&str>() {
        "string"
    } else if v.is::<i64>() || v.is::<i32>() {
        "int"
    } else {
        "unknown"
    }
}

fn basis_words(b: Basis, last: i64, year: i64) -> String {
    if b == Basis::PriorYearIncome {
        return format!("the tax on your {last} income at {year} rates");
    }
    format!("100% of your {last} tax")
}

/// Part of an installment's shortfall and the day it was paid, or the zero
/// Date if it never was.
#[derive(Clone, Copy, Debug, Default)]
struct Piece {
    amount: USCents,
    paid_on: Date,
}

/// One installment after the credits are applied.
#[derive(Clone, Debug, Default)]
struct Entry {
    due: Date,
    #[allow(dead_code)]
    required: USCents,
    credited_by: USCents,
    shortfall: USCents,
    pieces: Vec<Piece>,
}

impl Entry {
    fn outstanding(&self) -> USCents {
        let mut owed = self.shortfall;
        for p in &self.pieces {
            if !p.paid_on.is_zero() {
                owed -= p.amount;
            }
        }
        owed
    }
}

/// Withholding or a payment. `at_due` marks one counted on its installment
/// date, which pays that installment before an earlier shortfall.
#[derive(Clone, Copy, Debug)]
struct Credit {
    on: Date,
    amount: USCents,
    at_due: bool,
}

/// Pays the oldest of the first `n` shortfalls on `on`, and returns what is
/// left.
fn pay_oldest(out: &mut [Entry], n: usize, on: Date, amount: USCents) -> USCents {
    let mut left = amount;
    for e in out.iter_mut().take(n) {
        if left.0 <= 0 {
            break;
        }
        let owed = e.outstanding();
        if owed.0 <= 0 {
            continue;
        }
        let paid = owed.min(left);
        e.pieces.push(Piece {
            amount: paid,
            paid_on: on,
        });
        left -= paid;
    }
    left
}

/// Applies credits to the installments the way Form 760C Part I and
/// § 58.1-492(B)(2) do, which is not the order [`estimated::apply`] uses.
///
/// A credit on an installment date pays that installment first; only the
/// excess pays earlier shortfalls, on that date, and the rest carries
/// forward. This is Form 760C lines 11 and 12 and 23VAC10-112-42, Example 2.
/// A payment between installment dates pays the oldest shortfall first, on
/// its own date, and the rest counts toward the next installment, as the
/// line 13 example in the Form 760C instructions does. A payment after the
/// last date pays the oldest shortfall.
fn apply(dues: &[Date], required: &[USCents], credits: &[Credit]) -> Vec<Entry> {
    let mut cs = credits.to_vec();
    cs.sort_by_key(|c| c.on.days());
    let mut out: Vec<Entry> = vec![Entry::default(); dues.len()];
    let (mut pool, mut credited) = (USCents(0), USCents(0));
    let mut next = 0;
    for (i, due) in dues.iter().enumerate() {
        let due = *due;
        let mut at_due = USCents(0);
        while next < cs.len() && !cs[next].on.after(due) {
            let c = cs[next];
            next += 1;
            credited += c.amount;
            if c.at_due && c.on == due {
                at_due += c.amount;
                continue;
            }
            pool += pay_oldest(&mut out, i, c.on, c.amount);
        }
        let available = pool + at_due;
        let covered = available.min(required[i]);
        out[i] = Entry {
            due,
            required: required[i],
            credited_by: credited,
            shortfall: required[i] - covered,
            pieces: Vec::new(),
        };
        pool = pay_oldest(&mut out, i, due, available - covered);
    }
    let n = dues.len();
    for c in &cs[next..] {
        pay_oldest(&mut out, n, c.on, c.amount);
    }
    for e in out.iter_mut() {
        let owed = e.outstanding();
        if owed.0 > 0 {
            e.pieces.push(Piece {
                amount: owed,
                paid_on: Date::default(),
            });
        }
    }
    out
}

/// Where an installment stands once the exceptions are applied: excused
/// when an exception covers it, and charged when it is short and no
/// exception does.
#[derive(Clone, Debug, Default)]
struct Standing {
    excused: bool,
    charged: bool,
    why: String,
}

impl EstimatedYear {
    /// The VAGI below which no return is required, or `None` for a filing
    /// status Form 760ES gives none for.
    fn agi_floor(&self, s: filing::Status) -> Option<USCents> {
        match s {
            filing::Status::Single => Some(self.agi_single),
            filing::Status::MarriedFilingJointly => Some(self.agi_joint),
            filing::Status::MarriedFilingSeparately => Some(self.agi_separate),
            _ => None,
        }
    }

    /// Reports whether any day after `from` up to and including `to` is
    /// charged at a printed daily factor.
    fn printed_days(&self, from: Date, to: Date) -> bool {
        self.rates.iter().any(|p| {
            p.printed && to.after(p.from.add_days(-1)) && from.before(p.to) && to.after(from)
        })
    }

    /// The rate period covering `d`.
    fn rate_on(&self, d: Date) -> Option<&RatePeriodOf> {
        self.rates
            .iter()
            .find(|p| !d.before(p.from) && !d.after(p.to))
    }

    /// Charges `principal` for the days after `from` up to and including
    /// `to`, one part for each rate period. It returns the first day no rate
    /// covers, or the zero Date, and whether any day was at a rate with no
    /// printed factor.
    fn accrue(
        &self,
        principal: USCents,
        from: Date,
        to: Date,
    ) -> Result<(Vec<PenaltyPart>, Date, bool), Error> {
        let mut parts = Vec::new();
        let mut computed = false;
        let mut cur = from;
        while cur.before(to) {
            let day = cur.add_days(1);
            let Some(p) = self.rate_on(day) else {
                return Ok((parts, day, computed));
            };
            let mut end = to;
            if p.to.before(end) {
                end = p.to;
            }
            let (ps, _) = if p.printed {
                estimated::daily_factor(principal, cur, end, &p.factor)?
            } else {
                computed = true;
                estimated::simple_daily(principal, cur, end, &p.series)?
            };
            parts.extend(ps);
            cur = end;
        }
        Ok((parts, Date::default(), computed))
    }
}

impl VirginiaEstimated {
    fn extras(&self, f: &Facts) -> Result<EstimatedFacts, Error> {
        match f.state() {
            None => Ok(EstimatedFacts::default()),
            Some(s) => match s.downcast_ref::<EstimatedFacts>() {
                Some(e) => Ok(e.clone()),
                None => Err(Error::Invalid(format!(
                    "virginiataxcalc: the facts carry {}, not virginiataxcalc.EstimatedFacts",
                    type_name(s)
                ))),
            },
        }
    }

    fn check(&self, f: &Facts, e: &EstimatedFacts) -> Result<(), Error> {
        if f.jurisdiction() != JURISDICTION {
            return Err(Error::Invalid(format!(
                "virginiataxcalc: these are Virginia's rules, and the facts are for {}",
                f.jurisdiction()
            )));
        }
        if f.tax_year() != self.y.year {
            return Err(Error::Invalid(format!(
                "virginiataxcalc: these are the {} rules, and the facts are for {}",
                self.y.year,
                f.tax_year()
            )));
        }
        let share = Rate(f.farm_share().0 + f.fish_share().0 + e.merchant_seaman.0);
        if share > percent(100) {
            return Err(Error::Invalid(format!(
                "virginiataxcalc: farming {}, fishing {} and merchant seaman {} are not shares of one income",
                f.farm_share(),
                f.fish_share(),
                e.merchant_seaman
            )));
        }
        if i64::from(share.0) * 3 >= 20_000 {
            return Err(Error::with_detail(
                Error::Unsupported,
                "a filer with two-thirds of gross income from farming, fishing or work as a merchant seaman files Form 760F, which is not built",
            ));
        }
        if f.elections().annualised() {
            for (i, p) in f.periods().iter().enumerate() {
                let want = self.y.period_ends.get(i).copied().unwrap_or_default();
                if p.end() != want {
                    return Err(Error::with_detail(
                        Error::Unsupported,
                        format!(
                            "Form 760C Exception 3 period {} ends on {}, and a period ending {} is not one Virginia uses",
                            i + 1,
                            want,
                            p.end()
                        ),
                    ));
                }
            }
        }
        Ok(())
    }

    /// Where the Department's forms and the Code of Virginia or its
    /// regulations part ways on a rule a Virginia computation uses. They come
    /// from research/estimated-payments/va.md and from reading the documents
    /// in the Go package's testdata/ against each other.
    fn divergences(&self, af: &Affects) -> Result<Vec<Divergence>, Error> {
        let mut out = Vec::new();
        for b in [
            working::new_divergence("When estimated payments are required")
                .form("Payments are due if tax after withholding and credits will be more than $1,000. The exception is Virginia AGI under a floor for your filing status. The floor is $11,950 single or separate, and $23,900 married filing jointly.",
                    "Form 760ES (2026, Rev. 09/25), Section I")
                .statute("The amount is left to regulation. The regulation still requires a declaration if estimated tax is more than $150. Its AGI floors are $5,000 single and $8,000 married.",
                    "Code of Virginia § 58.1-490(A); 23VAC10-112-20")
                .uses(Reading::Form)
                .affects(af.required)
                .clone(),
            working::new_divergence("The small-underpayment exception")
                .form("The 2025 Form 760C cancels the charge when shortfalls total $150 or less. That is the 2025 figure. No 2026 Form 760C is published. The 2026 Form 760ES prints the 2026 law's $1,000.",
                    "Form 760C (2025, Rev. 04/26), line 14; Form 760ES (2026, Rev. 09/25), Section VIII")
                .statute("No charge for an underpayment of $1,000 or less for the year. That is as amended by 2026 Acts c. 7, for taxable years from January 1, 2026.",
                    "Code of Virginia § 58.1-492(D)(3); 2026 Acts c. 7, fifth and sixth enactments")
                .uses(Reading::Statute)
                .affects(af.small)
                .clone(),
            working::new_divergence("The January installment for a return filed by March 1")
                .form("A return filed and paid in full by March 1 excuses the January 15 payment. That holds for any filer. Form 760C has no such rule.",
                    "Form 760ES (2026, Rev. 09/25), Section IV; Form 760C (2025) and its instructions")
                .statute("A return filed and paid by March 1 can count as the declaration. It counts only if your declaration was first due January 15. Otherwise it counts as an amendment of it.",
                    "Code of Virginia § 58.1-490(I)")
                .uses(Reading::Form)
                .affects(af.january)
                .clone(),
            working::new_divergence("A requirement first met after April 15")
                .form("A requirement first met after April 15 is paid in thirds, halves or in full. They start June 15, September 15 or January 15, in that order. Even so, withholding is still divided by four. Exceptions 1 and 2 still use 25% steps.",
                    "Form 760ES (2026), Payment Schedule; Form 760C (2025), lines 5, 8, 16 and 17, and the instructions for line 5")
                .statute("The June 15 group states no starting date. Withholding is an equal part on each installment date that applies. Each exception is tested against what would have been required by the date.",
                    "Code of Virginia §§ 58.1-490(E), 58.1-491(A), 58.1-492(C)(1) and (D)(2)")
                .uses(Reading::Form)
                .affects(af.late_start)
                .clone(),
            working::new_divergence("Exceptions 3 and 4 and the January installment")
                .form("Exceptions 3 and 4 do not apply to the fourth installment.",
                    "Form 760C (2025), Part II, lines 18 and 19, and the instructions for them")
                .statute("The annualized and actual-income exceptions apply to any installment. They use the months ending before the month it's due.",
                    "Code of Virginia § 58.1-492(C)(1)(c) and (C)(2)")
                .uses(Reading::Form)
                .affects(af.late_exceptions)
                .clone(),
            working::new_divergence("Withholding on the dates withheld")
                .form("Withholding entered by installment period counts in that period's column. Any excess over the installment pays an earlier shortfall on that date.",
                    "Form 760C (2025), lines 8 and 11, and the instructions for them")
                .statute("Withholding counts as paid on the dates it was actually withheld. It pays an earlier shortfall on those dates.",
                    "Code of Virginia § 58.1-492(B)(2) and (D)(2)")
                .uses(Reading::Form)
                .affects(af.withholding)
                .clone(),
            working::new_divergence("The daily rate")
                .form("Each day at 9% is charged at a printed daily rate of .00025. That is 9.125% over 365 days.",
                    "Form 760C (2025), line 23; Form 760F (2025), line 11")
                .statute("The charge is at the rate for the period, 9% a year for the second half of 2026. The regulation applies it as the rate times the days over 365.",
                    "Code of Virginia §§ 58.1-15(A), 58.1-492(A); 23VAC10-112-42, Example 1")
                .uses(Reading::Form)
                .affects(af.daily_rate)
                .clone(),
        ] {
            out.push(b.build()?);
        }
        Ok(out)
    }

    /// `d` as a person reads it: "Dec 15" in the tax year, "Jan 15, 2027"
    /// outside it.
    fn date(&self, d: Date) -> String {
        if d.year() == self.y.year {
            return d.format("Jan 2");
        }
        d.format("Jan 2, 2006")
    }

    fn assess(&self, f: &Facts) -> Result<Assessment, Error> {
        let extras = self.extras(f)?;
        self.check(f, &extras)?;
        let y = &self.y;
        let last = y.year - 1;
        let mut a = Assessment {
            dues: estimated::move_dates(&y.calendar, &y.dues),
            ..Assessment::default()
        };
        let mut af = Affects::default();

        let tax_opt = f.this_year().tax();
        let tax_known = tax_opt.is_some();
        let tax = tax_opt.unwrap_or_default();
        a.tax_known = tax_known;
        let withheld = f.withheld_total();
        let prior = f.prior_year();
        let has_prior = prior.is_some();
        let prior = prior.unwrap_or_default();
        let prior_tax = prior.figures().and_then(|pf| pf.tax()).unwrap_or_default();

        let agi_opt = f.this_year().state_agi();
        let agi_known = agi_opt.is_some();
        let agi = agi_opt.unwrap_or_default();
        let floor_opt = y.agi_floor(f.filing_status());
        let has_floor = floor_opt.is_some();
        let floor = floor_opt.unwrap_or_default();
        // Under the income floor no target is needed, so nothing that sets
        // one is asked.
        let agi_exempt = agi_known && has_floor && agi < floor;

        // 90% of this year's tax.
        let this_year = if tax_known {
            estimated::new_target_option(
                Basis::ThisYearTax,
                y.this_year_share,
                y.this_year_share.of(tax),
            )
            .build()?
        } else {
            estimated::new_target_option(Basis::ThisYearTax, y.this_year_share, USCents(0))
                .unavailable(estimated::NEEDS_THIS_YEAR_TAX)
                .build()?
        };
        if tax_known {
            a.notes.rule(
                "Code of Virginia § 58.1-492(A)(1); Form 760C (2025), lines 1 and 2",
                format!(
                    "{} of this year's Virginia tax of {tax} is {}",
                    y.this_year_share,
                    this_year.amount()
                ),
            );
        }

        // 100% of the tax shown on last year's return, § 58.1-492(A)(1) and
        // Form 760C line 3. Neither states a condition of liability or of
        // twelve months; Exception 1 has both.
        let prior_year = if !has_prior {
            if !agi_exempt {
                a.notes.ask(
                    PositionKind::PriorYearReturn,
                    format!("Did you file a {last} Virginia return, and what was its tax?"),
                    format!(
                        "With it, {} of last year's tax could set a lower target.",
                        y.prior_year_share
                    ),
                );
            }
            estimated::new_target_option(Basis::PriorYearTax, y.prior_year_share, USCents(0))
                .unavailable(format!("Needs your {last} Virginia return"))
                .build()?
        } else if !prior.filed() {
            if !agi_exempt {
                a.notes.ask(
                    PositionKind::PriorYearReturn,
                    format!(
                        "Was any {last} Virginia tax due from you, though you didn't file a return?"
                    ),
                    "Neither the Code nor Form 760C says whether last year then counts as zero. Your target leaves it out, which can only raise it.",
                );
            }
            estimated::new_target_option(Basis::PriorYearTax, y.prior_year_share, USCents(0))
                .unavailable(format!("You didn't file a {last} Virginia return"))
                .build()?
        } else {
            a.notes.rule(
                "Code of Virginia § 58.1-492(A)(1); Form 760C (2025), lines 3 and 4",
                format!(
                    "{} of the {prior_tax} of tax on the {last} return is {}",
                    y.prior_year_share,
                    y.prior_year_share.of(prior_tax)
                ),
            );
            estimated::new_target_option(
                Basis::PriorYearTax,
                y.prior_year_share,
                y.prior_year_share.of(prior_tax),
            )
            .build()?
        };

        // Exception 2: last year's income at this year's rates, which needs
        // no return last year.
        let income_tax_opt = extras.prior_year_income_tax();
        let income_known = income_tax_opt.is_some();
        let income_tax = income_tax_opt.unwrap_or_default();
        let income = if income_known {
            estimated::new_target_option(Basis::PriorYearIncome, y.prior_year_share, income_tax)
                .build()?
        } else {
            estimated::new_target_option(Basis::PriorYearIncome, y.prior_year_share, USCents(0))
                .unavailable(format!(
                    "Needs the tax on your {last} income at {} rates",
                    y.year
                ))
                .build()?
        };

        let mut rb = estimated::new_requirement();
        rb.option(this_year.clone())
            .option(prior_year.clone())
            .option(income.clone());
        let mut chosen: Option<TargetOption> = None;
        for o in [&this_year, &prior_year, &income] {
            if o.available() && chosen.as_ref().is_none_or(|c| o.amount() < c.amount()) {
                chosen = Some(o.clone());
            }
        }
        let found = chosen.is_some();
        let chosen = chosen.unwrap_or_default();
        let target = chosen.amount();

        // The installments § 58.1-492(A)(1) requires: the lesser of the two
        // measures, or last year's alone when this year's tax is not known.
        let regular_of = if tax_known && prior_year.available() {
            a.regular_known = true;
            this_year.amount().min(prior_year.amount())
        } else if tax_known {
            a.regular_known = true;
            this_year.amount()
        } else if prior_year.available() {
            a.regular_known = true;
            prior_year.amount()
        } else {
            target
        };

        if !tax_known && !agi_exempt {
            let effect = if prior_year.available() {
                format!(
                    "Without it, each installment is measured against your {last} tax. That can only be too high."
                )
            } else if found {
                format!(
                    "Without it, your target is the tax on your {last} income at {} rates. No charge can be figured for an installment that misses it.",
                    y.year
                )
            } else {
                format!(
                    "It's the only figure that can set your target, at {}. It also decides the {} filing test.",
                    y.this_year_share,
                    y.filing_threshold.whole()
                )
            };
            a.notes.ask(
                PositionKind::ThisYearTax,
                format!("What do you expect your {} Virginia tax to be?", y.year),
                effect,
            );
        }

        if agi_exempt {
            a.exempt = true;
            let ex = estimated::exception_below("Virginia adjusted gross income", agi, floor)?;
            rb.required(
                false,
                format!(
                    "Your Virginia adjusted gross income of {} is under {}. You don't need to file a Virginia return or make estimated payments.",
                    agi.whole(),
                    floor.whole()
                ),
            )
            .exempt(ex);
            a.notes.rule(
                "Code of Virginia § 58.1-321(A); Form 760ES (2026), Section I; Form 760C (2025) instructions, Who Must File",
                format!(
                    "VAGI of {agi} for a {} filer is under {floor}, so no return is required and no Form 760C is needed",
                    f.filing_status()
                ),
            );
        } else if !found {
            rb.required(
                false,
                format!(
                    "Your target is the smallest of three figures. They are {} of your {} Virginia tax and {} of your {last} tax. The third is the tax on your {last} income at {} rates.",
                    y.this_year_share, y.year, y.prior_year_share, y.year
                ),
            )
            .no_target();
        } else if !tax_known && withheld >= target {
            rb.required(
                false,
                format!(
                    "Your target is {}, {}. Your withholding of {} already covers it. No estimated payments are needed, whatever your {} tax turns out to be.",
                    target.whole(),
                    basis_words(chosen.basis(), last, y.year),
                    withheld.whole(),
                    y.year
                ),
            )
            .choose(chosen.basis());
        } else if !tax_known {
            rb.required(
                true,
                format!(
                    "Your target is {}, {}. It keeps you clear of the charge whatever your {} tax turns out to be. After {} of withholding, that's {} of estimated payments.",
                    target.whole(),
                    basis_words(chosen.basis(), last, y.year),
                    y.year,
                    withheld.whole(),
                    (target - withheld).whole()
                ),
            )
            .choose(chosen.basis());
        } else if tax - withheld <= y.filing_threshold {
            let why = if tax < withheld {
                format!(
                    "Your Virginia tax of {} less {} withheld {}. Form 760ES therefore doesn't require estimated payments.",
                    tax.whole(),
                    withheld.whole(),
                    estimated::leaves(tax - withheld)
                )
            } else {
                format!(
                    "Your Virginia tax of {} less {} of withholding is {} or less. Form 760ES therefore doesn't require estimated payments.",
                    tax.whole(),
                    withheld.whole(),
                    y.filing_threshold.whole()
                )
            };
            rb.required(false, why).choose(chosen.basis());
            a.notes.rule(
                "Form 760ES (2026), Section I(d)",
                format!(
                    "{tax} of tax less {withheld} of withholding is {}, not more than {}, so estimated payments are not required",
                    tax - withheld,
                    y.filing_threshold
                ),
            );
        } else if withheld >= target {
            rb.required(
                false,
                format!(
                    "Your Virginia withholding of {} covers the {} needed for the year. No estimated payments are needed.",
                    withheld.whole(),
                    target.whole()
                ),
            )
            .choose(chosen.basis());
        } else {
            rb.required(
                true,
                format!(
                    "Your Virginia tax of {} less {} withheld leaves {} due. Withholding falls short of your {} target. You need estimated payments of {}.",
                    tax.whole(),
                    withheld.whole(),
                    (tax - withheld).whole(),
                    target.whole(),
                    (target - withheld).whole()
                ),
            )
            .choose(chosen.basis());
        }
        // The regulation's $150 test and AGI floors.
        let mut statute_floor = DOLLAR * 5_000;
        if f.filing_status() == filing::Status::MarriedFilingJointly
            || f.filing_status() == filing::Status::MarriedFilingSeparately
        {
            statute_floor = DOLLAR * 8_000;
        }
        af.required = (a.exempt && agi >= statute_floor)
            || (!a.exempt
                && tax_known
                && tax - withheld > DOLLAR * 150
                && tax - withheld <= y.filing_threshold);
        a.requirement = rb.build()?;
        if agi_known && !has_floor {
            a.notes.rule(
                "Form 760ES (2026), Section I",
                format!(
                    "Form 760ES gives VAGI floors only for single, married filing jointly and married filing separately filers, so none is applied to a {} filer",
                    f.filing_status()
                ),
            );
        }

        if a.exempt {
            a.regular = vec![USCents(0); a.dues.len()];
            a.lesser = vec![USCents(0); a.dues.len()];
            return self.with_divergences(a, &af);
        }
        if !income_known {
            a.notes.ask(
                PositionKind::JurisdictionFact,
                format!(
                    "What is the tax on your {last} income at {} rates and exemptions?",
                    y.year
                ),
                "With it, payments that reach that figure cancel the charge under Exception 2.",
            );
        }
        if !extras.merchant_seaman_share().1 {
            a.notes.ask(
                PositionKind::JurisdictionFact,
                "Is two-thirds of gross income from farming, fishing or merchant seaman work?",
                "If so, separate rules apply. Until then, your figures assume no merchant seaman income.",
            );
        }

        // A late starter pays in fewer equal installments. Form 760ES's
        // payment schedule and Form 760C line 5.
        let n = a.dues.len() as i64;
        a.first = estimated::late_start(f.requirement_first_met(), &y.later_groups);
        a.lesser = estimated::equal_shares(regular_of, n, a.first)?;
        a.regular = estimated::increments(&a.lesser);
        if a.first > 0 {
            a.notes.rule(
                "Code of Virginia §§ 58.1-490(E), 58.1-491(A); Form 760ES (2026), Payment Schedule; Form 760C (2025), line 5",
                format!(
                    "the requirement was first met on {}, so {regular_of} is due in {} equal installments from {}",
                    f.requirement_first_met().long(),
                    n - a.first,
                    a.dues[a.first as usize].long()
                ),
            );
        } else {
            a.notes.rule(
                "Code of Virginia § 58.1-491(A)(1); Form 760ES (2026), Section II; Form 760C (2025), line 7",
                format!(
                    "a quarter of {regular_of}, {}, is due on each of {}, {}, {} and {}",
                    a.regular[0],
                    a.dues[0].long(),
                    a.dues[1].long(),
                    a.dues[2].long(),
                    a.dues[3].long()
                ),
            );
        }

        // The exceptions, Form 760C Part II. Exception 1 asks for the same
        // amounts as installments measured on last year's tax, so it is
        // reported but rarely decides anything.
        if has_prior && prior.filed() {
            if prior.months() == 12 && prior_tax.0 > 0 {
                a.exceptions.push(Exception {
                    name: "Exception 1: payments reached last year's tax".to_string(),
                    needed: self.shares(prior_tax),
                    covers: 4,
                });
                a.notes.rule(
                    "Code of Virginia § 58.1-492(C)(1)(a); Form 760C (2025), line 16",
                    format!(
                        "Exception 1 is open: the {last} return covered 12 months and showed {prior_tax} of tax, and payments that reach 25%, 50%, 75% and 100% of it by the four dates cancel the addition"
                    ),
                );
            } else {
                a.notes.rule(
                    "Code of Virginia § 58.1-492(C)(1)(a); Form 760C (2025) instructions, line 16",
                    format!(
                        "Exception 1 is not open: it needs a 12-month {last} return showing tax, and the return covered {} months and showed {prior_tax}",
                        prior.months()
                    ),
                );
            }
        }
        if income_known {
            a.exceptions.push(Exception {
                name: format!(
                    "Exception 2: payments reached the tax on {last} income at {} rates",
                    y.year
                ),
                needed: self.shares(income_tax),
                covers: 4,
            });
        }
        if f.elections().annualised() {
            let mut needed = vec![USCents(0); y.annualised.len()];
            for (i, p) in f.periods().iter().take(y.annualised.len()).enumerate() {
                needed[i] = y.annualised[i].of(p.annualised_tax());
            }
            a.notes.rule(
                "Code of Virginia § 58.1-492(C)(1)(c); Form 760C (2025), line 18",
                format!(
                    "Exception 3 needs 22.5%, 45% and 67.5% of the tax on annualized income by the first three dates: {}, {} and {}",
                    needed[0], needed[1], needed[2]
                ),
            );
            let covers = needed.len();
            a.exceptions.push(Exception {
                name: "Exception 3: payments reached 90% of the tax on annualized income"
                    .to_string(),
                needed,
                covers,
            });
        }
        if let Some(taxes) = extras.income_to_date_tax() {
            let needed: Vec<USCents> = taxes.iter().map(|t| y.income_to_date.of(*t)).collect();
            a.notes.rule(
                "Code of Virginia § 58.1-492(C)(2); Form 760C (2025), line 19",
                format!(
                    "Exception 4 needs {} of the tax on the income received to April 30, May 31 and August 31 by the first three dates: {}, {} and {}",
                    y.income_to_date, needed[0], needed[1], needed[2]
                ),
            );
            let covers = needed.len();
            a.exceptions.push(Exception {
                name: "Exception 4: payments reached 90% of the tax on the income received to date"
                    .to_string(),
                needed,
                covers,
            });
        }

        // The March 1 rule, moved past a weekend or holiday.
        let by = y.calendar.next_business_day(y.file_and_pay_by);
        let filed = f.return_filed();
        if !filed.is_zero() && !filed.after(by) {
            a.excused_january = true;
            a.regular[3] = USCents(0);
            a.notes.rule(
                "Form 760ES (2026), Section IV; Code of Virginia § 58.1-490(I)",
                format!(
                    "the return was filed with the balance paid on {}, by {}, so the {} payment is not required",
                    filed.long(),
                    by.long(),
                    a.dues[3].long()
                ),
            );
        }

        // What the January, late-start, withholding and rate readings turn
        // on.
        af.january = a.excused_january && a.first != 3;
        af.late_start = a.first > 0;
        if a.requirement.target_known() {
            let (ledger, _) = self.ledger(f, &a)?;
            let (standings, total) = self.standings(&a, &ledger);
            af.small = a.regular_known && total > DOLLAR * 150 && total <= y.small_underpayment;
            let late = a.exceptions.iter().any(|e| e.covers < a.dues.len());
            af.late_exceptions = late && ledger[3].shortfall.0 > 0 && !standings[3].excused;
            for (i, s) in standings.iter().enumerate() {
                if !s.charged {
                    continue;
                }
                af.withholding =
                    af.withholding || (f.elections().withholding_as_withheld() && withheld.0 > 0);
                for pc in &ledger[i].pieces {
                    let mut to = y.stop;
                    if !pc.paid_on.is_zero() && pc.paid_on.before(to) {
                        to = pc.paid_on;
                    }
                    af.daily_rate = af.daily_rate || y.printed_days(ledger[i].due, to);
                }
            }
        }
        self.with_divergences(a, &af)
    }

    /// Adds the divergences to the assessment's notes.
    fn with_divergences(&self, mut a: Assessment, af: &Affects) -> Result<Assessment, Error> {
        let ds = self.divergences(af)?;
        a.notes.diverges(&ds);
        Ok(a)
    }

    /// Form 760C lines 16 and 17: `amount` times 25%, 50%, 75% and 100%.
    fn shares(&self, amount: USCents) -> Vec<USCents> {
        self.y
            .exception_shares
            .iter()
            .map(|s| s.of(amount))
            .collect()
    }

    /// Lists withholding and payments as Form 760C counts them.
    fn credits(&self, f: &Facts, dues: &[Date]) -> Result<Vec<Credit>, Error> {
        let mut out = Vec::new();
        let period = |on: Date| -> (Date, bool) {
            for d in dues {
                if !on.after(*d) {
                    return (*d, on == *d || on.before(dues[0]));
                }
            }
            (on, false)
        };
        if f.elections().withholding_as_withheld() {
            for w in f.withholding() {
                let (d, _) = period(w.on());
                out.push(Credit {
                    on: d,
                    amount: w.amount(),
                    at_due: true,
                });
            }
        } else {
            let cum = estimated::cumulative_shares(f.withheld_total(), &quarters())?;
            for (i, a) in estimated::increments(&cum).into_iter().enumerate() {
                if a.0 > 0 {
                    out.push(Credit {
                        on: dues[i],
                        amount: a,
                        at_due: true,
                    });
                }
            }
        }
        for p in f.payments() {
            let (d, on_due) = period(p.on());
            if p.from_prior_year() {
                out.push(Credit {
                    on: dues[0],
                    amount: p.amount(),
                    at_due: true,
                });
            } else if on_due {
                out.push(Credit {
                    on: d,
                    amount: p.amount(),
                    at_due: true,
                });
            } else {
                out.push(Credit {
                    on: p.on(),
                    amount: p.amount(),
                    at_due: false,
                });
            }
        }
        Ok(out)
    }

    fn ledger(&self, f: &Facts, a: &Assessment) -> Result<(Vec<Entry>, Notes), Error> {
        let credits = self.credits(f, &a.dues)?;
        let ledger = apply(&a.dues, &a.regular, &credits);
        let mut notes = Notes::default();
        if f.elections().withholding_as_withheld() {
            notes.rule(
                "Form 760C (2025), instructions for lines 8 to 10",
                format!(
                    "by election, the {} withheld is entered in the installment period it was withheld in",
                    f.withheld_total()
                ),
            );
        } else {
            notes.rule(
                "Code of Virginia § 58.1-492(D)(2); Form 760C (2025), instructions for lines 8 to 10",
                format!(
                    "the {} withheld is divided by four and treated as paid on the installment dates",
                    f.withheld_total()
                ),
            );
        }
        notes.rule(
            "Code of Virginia § 58.1-492(B)(2); Form 760C (2025), lines 11 to 14; 23VAC10-112-42, Example 2",
            "a payment on an installment date pays that installment first and then the oldest shortfall; a payment between dates pays the oldest shortfall on its own date",
        );
        Ok((ledger, notes))
    }

    /// Applies Form 760C line 14 and Part II, and returns the year's total
    /// underpayment. The small-underpayment exception is applied only when
    /// the regular installments are known.
    fn standings(&self, a: &Assessment, ledger: &[Entry]) -> (Vec<Standing>, USCents) {
        let total: USCents = ledger.iter().map(|l| l.shortfall).sum();
        let small = a.regular_known && total <= self.y.small_underpayment;
        let mut out = Vec::with_capacity(ledger.len());
        for (i, l) in ledger.iter().enumerate() {
            let mut s = Standing::default();
            let mut met = String::new();
            for e in &a.exceptions {
                if i < e.covers && met.is_empty() && l.credited_by >= e.needed[i] {
                    met = e.name.clone();
                }
            }
            if a.excused_january && i == 3 {
                (s.excused, s.why) = (true, "the return was filed and paid by March 1".to_string());
            } else if small {
                (s.excused, s.why) = (
                    true,
                    format!(
                        "the year's underpayments total {total}, which is {} or less",
                        self.y.small_underpayment
                    ),
                );
            } else if !met.is_empty() {
                (s.excused, s.why) = (true, met);
            }
            s.charged = l.shortfall.0 > 0 && !s.excused;
            out.push(s);
        }
        (out, total)
    }

    /// The schedule: each date's running total of the regular installments,
    /// what was credited by it, and its own shortfall.
    fn installments(
        &self,
        a: &Assessment,
        ledger: &[Entry],
        standings: &[Standing],
    ) -> Result<Vec<Installment>, Error> {
        let mut out = Vec::with_capacity(ledger.len());
        for (i, l) in ledger.iter().enumerate() {
            let mut b = estimated::new_installment(i as i64 + 1, l.due);
            b.needed_by(a.lesser[i])
                .credited_by(l.credited_by)
                .shortfall(l.shortfall);
            if standings[i].excused {
                b.excused();
            }
            out.push(b.build()?);
        }
        Ok(out)
    }

    /// Reports the late-start question when an early installment is charged
    /// and the caller did not say when the requirement arose.
    fn ask_first_met(&self, f: &Facts, s: &[Standing], notes: &mut Notes) {
        if !f.requirement_first_met().is_zero() {
            return;
        }
        if s.iter().take(3).any(|x| x.charged) {
            let by = self.date(self.y.later_groups[0].add_days(-1));
            notes.ask(
                PositionKind::RequirementFirstMet,
                format!(
                    "When did you first expect to need estimated payments for {}?",
                    self.y.year
                ),
                format!(
                    "Until then, your schedule assumes it was by {by}. A later date would give you fewer installments."
                ),
            );
        }
    }
}

impl EstimatedTax for VirginiaEstimated {
    fn requirement(&self, f: &Facts) -> Result<working::Result<Requirement>, Error> {
        let a = self.assess(f)?;
        Ok(working::new_result(a.requirement).with(&a.notes).build()?)
    }

    fn schedule(&self, f: &Facts) -> Result<working::Result<Vec<Installment>>, Error> {
        let a = self.assess(f)?;
        if !a.requirement.target_known() {
            return Err(Error::with_detail(
                Error::NeedsThisYearTax,
                format!(
                    "with no {} Virginia figure to measure against, there is no target to schedule",
                    self.y.year - 1
                ),
            ));
        }
        let (ledger, mut notes) = self.ledger(f, &a)?;
        let (standings, _) = self.standings(&a, &ledger);
        let out = self.installments(&a, &ledger, &standings)?;
        for (s, l) in standings.iter().zip(&ledger) {
            if !s.why.is_empty() && l.shortfall.0 > 0 {
                notes.rule(
                    "Code of Virginia § 58.1-492(C), (D)(3); Form 760C (2025), line 14 and Part II",
                    format!(
                        "the {} installment was {} short and bears no addition: {}",
                        l.due.long(),
                        l.shortfall,
                        s.why
                    ),
                );
            }
        }
        if !a.exempt {
            self.ask_first_met(f, &standings, &mut notes);
        }
        Ok(working::new_result(out)
            .with(&a.notes)
            .with(&notes)
            .build()?)
    }

    fn penalty(&self, f: &Facts, as_of: Date) -> Result<working::Result<Penalty>, Error> {
        if as_of.is_zero() {
            return Err(Error::Invalid(
                "virginiataxcalc: a penalty needs the date to figure it to".into(),
            ));
        }
        let a = self.assess(f)?;
        if !a.requirement.target_known() {
            return Err(Error::with_detail(
                Error::NeedsThisYearTax,
                format!(
                    "with no {} Virginia figure to measure against, there is no target to figure a penalty from",
                    self.y.year - 1
                ),
            ));
        }
        let y = &self.y;
        let mut stop = as_of;
        if y.stop.before(stop) {
            stop = y.stop;
        }
        let mut paid_in_full = false;
        let filed = f.return_filed();
        if !filed.is_zero() && filed.before(stop) {
            (stop, paid_in_full) = (filed, true);
        }
        let waiver_asked = f.positions().waiver();
        let waiver = waiver_asked.unwrap_or_default();
        let (ledger, mut notes) = self.ledger(f, &a)?;
        let (standings, total) = self.standings(&a, &ledger);
        let schedule = self.installments(&a, &ledger, &standings)?;
        if a.exempt {
            let p = estimated::new_penalty(stop)
                .waiver(waiver)
                .because(a.requirement.clone(), &schedule)
                .build()?;
            return Ok(working::new_result(p).with(&a.notes).build()?);
        }
        if !a.regular_known {
            // Without this year's tax or last year's return, the installments
            // are measured against 90% of this year's tax, which is not
            // known. Only an exception that covers every short installment
            // gives an answer.
            for (s, l) in standings.iter().zip(&ledger) {
                if s.charged {
                    return Err(Error::with_detail(
                        Error::NeedsThisYearTax,
                        format!(
                            "the {} installment met no exception, and Form 760C measures it against 90% of this year's tax",
                            l.due.long()
                        ),
                    ));
                }
            }
            notes.rule(
                "Code of Virginia § 58.1-492(C); Form 760C (2025), Part II",
                "this year's tax was not given, but every installment met an exception by its date, so no addition arises whatever this year's tax is",
            );
        } else if total <= y.small_underpayment && total.0 > 0 {
            notes.rule(
                "Code of Virginia § 58.1-492(D)(3); Form 760ES (2026), Section VIII(e)",
                format!(
                    "the year's underpayments total {total}, which is {} or less, so there is no addition to tax",
                    y.small_underpayment
                ),
            );
        }

        let mut pb = estimated::new_penalty(stop);
        pb.waiver(waiver).because(a.requirement.clone(), &schedule);
        let mut unrated = Date::default();
        let mut computed = false;
        for (i, (s, l)) in standings.iter().zip(&ledger).enumerate() {
            if !s.charged {
                continue;
            }
            let mut lb = estimated::new_penalty_line(i as i64 + 1, l.due, l.shortfall);
            let mut last_day = l.due;
            for p in &l.pieces {
                let mut to = stop;
                if !p.paid_on.is_zero() && p.paid_on.before(stop) {
                    to = p.paid_on;
                }
                if !to.after(l.due) {
                    continue;
                }
                if to.after(last_day) {
                    last_day = to;
                }
                let (parts, gap, used) = y.accrue(p.amount, l.due, to)?;
                computed = computed || used;
                if !gap.is_zero() && (unrated.is_zero() || gap.before(unrated)) {
                    unrated = gap;
                }
                for part in parts {
                    lb.part(part);
                }
            }
            pb.line(lb.days(last_day.days_since(l.due)).build()?);
        }
        let mut p = pb.build()?;
        if !a.tax_known && p.total().0 > 0 {
            p = p.to_builder().upper_bound().build()?;
            notes.rule(
                "Code of Virginia § 58.1-492(A)(1)",
                format!(
                    "this year's Virginia tax was not given, so each installment is measured against last year's tax alone; \
                     the installment is the lesser of that and 90% of this year's tax, so {} is the most the addition can be",
                    p.total()
                ),
            );
        }
        if paid_in_full {
            notes.rule(
                "Code of Virginia § 58.1-492(B)(2)",
                format!(
                    "the return was filed with the whole balance paid on {}, so every shortfall still outstanding was paid that day",
                    stop.long()
                ),
            );
        }
        notes.rule(
            "Code of Virginia §§ 58.1-15, 58.1-492(A), (B); Form 760C (2025), Part III",
            format!(
                "each shortfall no exception covers is charged for the days from its installment date until paid or {}, at the rate for each period; \
                 the addition to {} is {}",
                y.stop.long(),
                stop.long(),
                p.total()
            ),
        );
        if computed {
            notes.ask(
                PositionKind::UnpublishedRate,
                "",
                format!(
                    "No {} Form 760C is out, so the regulation's method applies. From {} to {}, 8% is charged as 8% times the days over 365.",
                    y.year,
                    self.date(y.rates[0].from),
                    self.date(y.rates[0].to)
                ),
            );
        }
        if !unrated.is_zero() {
            p = p.to_builder().rated_through(unrated.add_days(-1)).build()?;
            let last = y.rates.last().map(|r| r.to).unwrap_or_default();
            notes.ask(
                PositionKind::UnpublishedRate,
                "",
                format!(
                    "The rate for the days after {} isn't published yet.",
                    last.format("Jan 2, 2006")
                ),
            );
        }
        if waiver_asked.is_some() && waiver != WaiverKind::None {
            notes.rule(
                "Code of Virginia § 58.1-492; Form 760C (2025) and its instructions",
                "neither the Code nor the Department's forms provide a waiver of the addition for a casualty, retirement or disability, so the addition is shown in full",
            );
            if p.total().0 > 0 {
                p = p
                    .to_builder()
                    .waiver_outcome(estimated::waiver_refused(
                        "Virginia waives no penalty for a casualty, disaster, retirement or disability.",
                    ))
                    .build()?;
            }
        }
        self.ask_first_met(f, &standings, &mut notes);
        Ok(working::new_result(p).with(&a.notes).with(&notes).build()?)
    }

    fn explanation(&self) -> Explanation {
        virginia_explanation()
    }
}

/// Written by hand and checked against research/estimated-payments/va.md.
fn virginia_explanation() -> Explanation {
    estimated::new_explanation(
        "Take your 2026 Virginia tax after withholding and credits. If it will be more than $1,000, Virginia expects estimated payments. The charge for paying too little is interest on each installment's shortfall. There is none when the year's shortfalls total $1,000 or less.",
    )
    .paragraph("There is no charge if withholding and estimated payments reach a target. The target is the smaller of two figures. One is 90% of this year's Virginia tax. The other is 100% of the tax on last year's return. There is no 110% tier for high incomes. A return last year that showed no tax sets last year's figure at zero.")
    .paragraph("Four equal quarters are due in 2026 on May 1, June 15 and September 15. The fourth is due January 15, 2027. The first is due May 1, not April 15. Withholding counts as paid in equal parts on those dates. You can instead show when it was withheld. If you first need to pay later in the year, you pay fewer installments. They are three, two or one equal installments from the next date. You can file your return and pay in full by March 1, 2027. That takes the place of the January payment.")
    .paragraph("Each installment is measured on its own date. A payment made on an installment date counts toward that installment first. Only the rest goes back to an earlier shortfall. Four exceptions cancel an installment's charge without changing its shortfall. One is payments that reach last year's tax. That needs last year's return to cover twelve months and show tax. Another is last year's income taxed at this year's rates. The third is 90% of the tax on annualized income. The fourth uses income actually received to April 30, May 31 or August 31. It too is 90% of the tax on that income. The last two do not apply to the January installment.")
    .paragraph("The charge is interest at the federal underpayment rate plus 2%. It runs from the installment date until paid or May 1, 2027. For 2026 that is 8% from April to June and 9% from July to December. The rate for 2027 is not yet published. Virginia's form charges 9% as .00025 a day.")
    .versus_federal("The first payment is due May 1, and the charge runs to May 1. There's no 110% tier, and two more exceptions apply. There's no charge when the year's shortfalls total $1,000 or less. A payment on an installment date pays that installment before an earlier one. The rate is the federal rate plus 2%.")
    .source("Code of Virginia §§ 58.1-15, 58.1-321, 58.1-490, 58.1-491, 58.1-492; 2026 Acts c. 7")
    .source("23VAC10-112-20, -42 and -43")
    .source("Form 760ES, 2026 Virginia Estimated Income Tax Payment Vouchers and Instructions for Individuals (Rev. 09/25)")
    .source("Form 760C, Underpayment of Virginia Estimated Tax by Individuals, Estates and Trusts, and its instructions, 2025")
    .source("Tax Bulletins 26-2, 26-3 and 26-4")
    .build()
    .unwrap_or_default()
}
