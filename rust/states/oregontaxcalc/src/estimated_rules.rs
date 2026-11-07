//! Oregon's estimated tax rules, ORS 316.557 to 316.589, as the shared
//! [`estimated::EstimatedTax`] trait. Ported from estimated.go and
//! export_test.go.
//!
//! Oregon's rules look like the federal ones and differ in the details that
//! change a number. The prior-year test is 100% at every income. Farmers and
//! fishers owe no estimates at all. Due dates move for Oregon's own legal
//! holidays. The charge is interest at the rate set under ORS 305.220, and
//! Form OR-10 computes it on a running balance from one event to the next,
//! rounding each line to a whole dollar.
//!
//! The figure follows the Department of Revenue's documents, Publication
//! OR-ESTIMATE for 2026 and the Form OR-10 instructions for 2025, and the
//! result's Divergences say where the statutes or rules read otherwise. No
//! act of the 2026 session amended ORS 316.557 to 316.589, 305.220 or
//! 187.010, so no form here predates the law it describes.

use std::any::Any;
use std::sync::LazyLock;

use estimated::{
    Basis, Calendar, Date, Error, EstimatedTax, Explanation, FactorSeries, Facts, Installment,
    Jurisdiction, Month, Payment, Penalty, PenaltyPart, Requirement,
};
use money::{Rate, USCents};
use working::{Divergence, Notes, PositionKind, Reading};

/// The jurisdiction code these rules answer for.
const OREGON: Jurisdiction = estimated::OREGON;

/// The facts Oregon's rules read that no other jurisdiction does. Carry them
/// in [`estimated::FactsBuilder::state`].
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct EstimatedFacts {
    prior_filed_first: bool,
    prior_filed_first_answered: bool,
    gross_income_test: bool,
    gross_income_answered: bool,
    business_periods: Vec<i64>,
    business_answered: bool,
}

impl EstimatedFacts {
    /// The installment periods, numbered from 1, in which a part-year
    /// resident had income from a trade or business in Oregon while living
    /// elsewhere, and whether the caller said, as `(periods, answered)`. The
    /// Form OR-10 instructions count those periods with the periods of
    /// residence.
    pub fn business_income_in(&self) -> (Vec<i64>, bool) {
        (self.business_periods.clone(), self.business_answered)
    }

    /// Whether the 2025 return was or will be filed before the 2026 return,
    /// and whether the caller said, as `(filed_first, answered)`.
    /// Publication OR-ESTIMATE and OAR 150-316-0493(3)(a)(B) allow the
    /// prior-year test only then.
    pub fn prior_return_filed_first(&self) -> (bool, bool) {
        (self.prior_filed_first, self.prior_filed_first_answered)
    }

    /// Whether ORS 316.563(1)'s gross income test is met, and whether the
    /// caller said, as `(met, answered)`. The test is more than $1,000 of
    /// gross income other than wages, or gross income over $20,000 ($10,000
    /// for a joint declaration where both spouses have wages, $5,000 for a
    /// married filer who cannot declare jointly). A caller who gives this
    /// year's gross income above $20,000 need not answer it.
    pub fn gross_income_test_met(&self) -> (bool, bool) {
        (self.gross_income_test, self.gross_income_answered)
    }
}

/// Builds [`EstimatedFacts`].
#[derive(Clone, Debug, Default)]
pub struct EstimatedFactsBuilder {
    e: EstimatedFacts,
}

/// Starts [`EstimatedFacts`] with nothing said.
pub fn new_estimated_facts() -> EstimatedFactsBuilder {
    EstimatedFactsBuilder::default()
}

impl EstimatedFactsBuilder {
    /// Answers whether last year's return was or will be filed first.
    pub fn prior_return_filed_first(&mut self, filed_first: bool) -> &mut Self {
        self.e.prior_filed_first = filed_first;
        self.e.prior_filed_first_answered = true;
        self
    }

    /// Answers whether ORS 316.563(1)'s gross income test is met.
    pub fn gross_income_test_met(&mut self, met: bool) -> &mut Self {
        self.e.gross_income_test = met;
        self.e.gross_income_answered = true;
        self
    }

    /// Sets the installment periods, from 1 to 4, with Oregon business
    /// income while living elsewhere. Calling it with none answers that
    /// there were none.
    pub fn business_income_in(&mut self, periods: &[i64]) -> &mut Self {
        self.e.business_periods = periods.to_vec();
        self.e.business_answered = true;
        self
    }

    /// Refuses an installment period outside 1 to 4.
    pub fn build(&self) -> Result<EstimatedFacts, Error> {
        for &n in &self.e.business_periods {
            if !(1..=4).contains(&n) {
                return Err(Error::Invalid(format!(
                    "oregontaxcalc: installment period {n} is not 1 to 4"
                )));
            }
        }
        Ok(self.e.clone())
    }
}

/// One tax year's published figures.
#[derive(Clone, Debug)]
pub(crate) struct EstimatedYear {
    pub(crate) year: i64,

    /// The tax after credits less withholding below which there is no
    /// interest.
    pub(crate) threshold: USCents,

    pub(crate) this_year_share: Rate,
    pub(crate) prior_year_share: Rate,

    pub(crate) dues: Vec<Date>,

    /// The first days of the late-start chart's three rows: a requirement
    /// first met on or after `late_starts[i]` is paid in 3-i equal
    /// installments, from installment i+2.
    pub(crate) late_starts: Vec<Date>,

    pub(crate) calendar: Calendar,
    pub(crate) stop: Date,
    pub(crate) period_ends: Vec<Date>,
    pub(crate) annualised: Vec<Rate>,

    /// The highest of ORS 316.563(1)'s gross income figures. Gross income
    /// above it meets the test for every filer.
    pub(crate) gross_income_test: USCents,

    /// The factor Form OR-10 prints for each calendar year.
    pub(crate) daily: FactorSeries,
}

/// Checks a year's figures as Go's `registerEstimated` does, which panics
/// where this returns the error. Oregon's rate changes only on January 1
/// (OAR 150-305-0144(2)), so each daily factor must cover one whole calendar
/// year.
pub(crate) fn validate(y: EstimatedYear) -> Result<EstimatedYear, Error> {
    for p in y.daily.periods() {
        let jan1 = estimated::must_date(p.from().year(), Month::JANUARY, 1);
        if p.from() != jan1 || p.to() != jan1.add_months(12).add_days(-1) {
            return Err(Error::Invalid(format!(
                "oregontaxcalc: the factor from {} to {} is not one calendar year",
                p.from(),
                p.to()
            )));
        }
    }
    Ok(y)
}

fn year_figures(year: i64) -> Result<EstimatedYear, Error> {
    match year {
        2026 => crate::estimated_y2026::year_2026(),
        _ => Err(estimated::no_rules(&OREGON, year)),
    }
}

/// Returns Oregon's estimated tax rules for a tax year, or
/// [`estimated::Error::NoRules`] for a year this build does not carry.
///
/// This year's tax, [`estimated::Figures::tax`], is the tax after all
/// credits, refundable ones included, as line 3 of the Form OR-10 required
/// annual payment worksheet: Form OR-40 line 31 less lines 32 and 37 to 39 on
/// the 2025 form. Last year's figures carry the same amount for that year,
/// and the 2025 kicker is among those credits. [`estimated::Facts::farm_share`]
/// and `fish_share` are the larger of last year's shares and this year's,
/// since either year's two-thirds is enough.
pub fn estimated_tax(year: i64) -> Result<Box<dyn EstimatedTax>, Error> {
    Ok(Box::new(OregonEstimated {
        y: year_figures(year)?,
    }))
}

/// Returns the Oregon legal holidays that move a tax year's estimated tax due
/// dates, or [`estimated::Error::NoRules`] for a year this build does not
/// carry.
pub fn estimated_calendar(year: i64) -> Result<Calendar, Error> {
    Ok(year_figures(year)?.calendar)
}

/// Runs the Form OR-10 underpayment interest worksheet on the given dates
/// and daily factors. It is here so that a test can reproduce the
/// instructions' 2025 example with 2025's dates and factor, which the 2026
/// rules do not carry; Go exports it to its tests only, from export_test.go.
/// It returns the worksheet lines that charged interest, in order.
pub fn worksheet_interest(
    dues: &[Date],
    required: &[USCents],
    withheld: &[USCents],
    payments: &[Payment],
    stop: Date,
    s: &FactorSeries,
) -> Result<(Vec<PenaltyPart>, Date), Error> {
    let (periods, unrated) = worksheet(dues, required, withheld, payments, stop, s)?;
    let parts = periods.into_iter().flat_map(|p| p.parts).collect();
    Ok((parts, unrated))
}

struct OregonEstimated {
    y: EstimatedYear,
}

/// What the three questions share.
#[derive(Clone, Default)]
struct Assessment {
    requirement: Requirement,
    exempt: bool,
    tax_known: bool,
    dues: Vec<Date>,

    /// The installment a late starter's payments start at, from 0.
    first: usize,
    /// For a part-year resident, the installment periods the required annual
    /// payment is divided over; `None` for anyone else.
    counted: Option<Vec<bool>>,

    /// Each installment's required payment.
    required: Vec<USCents>,

    // What each divergence turns on in these facts.
    gross_not_met: bool,
    worksheet_differs: bool,
    exactly_threshold: bool,
    prior_differs: bool,

    notes: Notes,
}

/// Go's `%T` for the extras a caller is most likely to pass by mistake.
fn type_name(s: &(dyn Any + Send + Sync)) -> &'static str {
    if s.is::<&str>() || s.is::<String>() {
        "string"
    } else {
        "another jurisdiction's extras"
    }
}

/// Residence in Oregon for part of the year only. A filer who gives no
/// residence, or none in Oregon, is not a part-year resident.
fn part_year(f: &Facts) -> bool {
    let mut days = 0;
    for p in f.residence() {
        if p.state() == OREGON.as_str() {
            days += p.to().days_since(p.from()) + 1;
        }
    }
    let jan1 = estimated::must_date(f.tax_year(), Month::JANUARY, 1);
    days > 0 && days < jan1.add_months(12).days_since(jan1)
}

/// "Jan 15" inside the tax year and "Jan 15, 2027" outside it.
fn short_date(d: Date, year: i64) -> String {
    let m: String = d.month().to_string().chars().take(3).collect();
    if d.year() == year {
        return format!("{m} {}", d.day());
    }
    format!("{m} {}, {}", d.day(), d.year())
}

/// What the target leaves to pay after withholding, for a Why.
fn target_why(target: USCents, withheld: USCents) -> String {
    if withheld.0 == 0 {
        return format!("Your target is {}, in equal installments.", target.whole());
    }
    format!(
        "Your target is {}. After withholding that's {}, in equal installments.",
        target.whole(),
        (target - withheld).whole()
    )
}

/// What estimated payments the target leaves after withholding, for a Why.
fn left_why(target: USCents, withheld: USCents) -> String {
    if withheld.0 == 0 {
        return format!(
            "That's {} of estimated payments, in equal installments.",
            target.whole()
        );
    }
    format!(
        "After {} of withholding, that's {} of estimated payments. Pay it in equal installments.",
        withheld.whole(),
        (target - withheld).whole()
    )
}

/// What tax less withholding leaves, for a Why.
fn owed_why(tax: USCents, withheld: USCents) -> String {
    if withheld.0 == 0 {
        return format!("Your Oregon tax is {}, with no withholding.", tax.whole());
    }
    format!(
        "Your Oregon tax of {} less {} withheld {}.",
        tax.whole(),
        withheld.whole(),
        estimated::leaves(tax - withheld)
    )
}

/// The running total of `total` paid in equal parts on the counted
/// installments, and nothing on the others.
fn shares_over(total: USCents, counted: &[bool]) -> Result<Vec<USCents>, Error> {
    let n = count(counted);
    let mut out = vec![USCents(0); counted.len()];
    if n == 0 {
        return Ok(out);
    }
    let eq = estimated::equal_shares(total, n as i64, 0)?;
    let mut k = 0;
    for (i, &c) in counted.iter().enumerate() {
        if c {
            k += 1;
        }
        if k > 0 {
            out[i] = eq[k - 1];
        }
    }
    Ok(out)
}

fn count(counted: &[bool]) -> usize {
    counted.iter().filter(|c| **c).count()
}

/// Names the counted installments by due date.
fn counted_list(counted: &[bool], dues: &[Date]) -> String {
    counted
        .iter()
        .enumerate()
        .filter(|(_, c)| **c)
        .map(|(i, _)| dues[i].long())
        .collect::<Vec<_>>()
        .join(" and ")
}

impl OregonEstimated {
    fn extras(&self, f: &Facts) -> Result<EstimatedFacts, Error> {
        match f.state() {
            None => Ok(EstimatedFacts::default()),
            Some(s) => match s.downcast_ref::<EstimatedFacts>() {
                Some(e) => Ok(e.clone()),
                None => Err(Error::Invalid(format!(
                    "oregontaxcalc: the facts carry {}, not oregontaxcalc.EstimatedFacts",
                    type_name(s)
                ))),
            },
        }
    }

    fn check(&self, f: &Facts) -> Result<(), Error> {
        if f.jurisdiction() != OREGON {
            return Err(Error::Invalid(format!(
                "oregontaxcalc: these are Oregon's rules, and the facts are for {}",
                f.jurisdiction()
            )));
        }
        if f.tax_year() != self.y.year {
            return Err(Error::Invalid(format!(
                "oregontaxcalc: these are the {} rules, and the facts are for {}",
                self.y.year,
                f.tax_year()
            )));
        }
        if f.elections().annualised() {
            for (i, p) in f.periods().iter().enumerate() {
                let Some(&end) = self.y.period_ends.get(i) else {
                    return Err(Error::Invalid(format!(
                        "oregontaxcalc: the annualised income worksheet has no period {}",
                        i + 1
                    )));
                };
                if p.end() != end {
                    return Err(Error::Invalid(format!(
                        "oregontaxcalc: annualised income worksheet period {} ends on {}, not {}",
                        i + 1,
                        end,
                        p.end()
                    )));
                }
            }
        }
        Ok(())
    }

    /// Where the department's documents and Oregon's statutes or rules part
    /// ways on a rule every Oregon computation uses. They are found in
    /// research/estimated-payments/or.md, section 10 and its verification,
    /// and checked against the extracts in testdata/. Each is marked as
    /// affecting these facts only where the readings give them different
    /// figures.
    fn divergences(&self, f: &Facts, a: &Assessment) -> Result<Vec<Divergence>, Error> {
        let mut short = false;
        if !a.exempt && a.requirement.target_known() {
            let (schedule, _) = self.installments(f, a)?;
            short = schedule.iter().any(|i| i.shortfall().0 > 0);
        }
        let mut out = Vec::new();
        for b in [
            working::new_divergence("Who must declare")
                .form("Only the $1,000 test on tax after credits less withholding decides who must pay. There is no gross income test.",
                    "Publication OR-ESTIMATE (2026), In general; Form OR-10 instructions (2025), Do I owe interest")
                .statute("Only two cases require a declaration. One is expected gross income with more than $1,000 other than wages. The other is expected gross income over $20,000, $10,000 or $5,000, by case. The interest sections don't refer to this test.",
                    "ORS 316.563(1); ORS 316.587, 316.588")
                .uses(Reading::Form).affects(a.gross_not_met).clone(),
            working::new_divergence("When payments are required")
                .form("Publication OR-ESTIMATE worksheet line 23 looks at the required annual payment. It requires payments only when that less withholding is $1,000 or more. Form OR-10 instead tests tax after credits less withholding. It charges interest on any shortfall once that is $1,000 or more.",
                    "Publication OR-ESTIMATE (2026), worksheet line 23; Form OR-10 instructions (2025), required annual payment worksheet line 5")
                .statute("No interest only when tax after credits, less withholding, is under $1,000. The rule looks at the required annual payment less withholding. Payments aren't required when that is under $1,000.",
                    "ORS 316.588(1); OAR 150-316-0493(3)(d)")
                .uses(Reading::Form).affects(a.worksheet_differs).clone(),
            working::new_divergence("Exactly $1,000")
                .form("Publication OR-ESTIMATE page 6 tests tax after credits less withholding. It charges interest when that is more than $1,000. Its page 1 and Form OR-10 line 5 use $1,000 or more.",
                    "Publication OR-ESTIMATE (2026), pages 1 and 6; Form OR-10 instructions (2025), required annual payment worksheet line 5")
                .statute("The requirement and the interest are waived only below $1,000. Exactly $1,000 is therefore charged.",
                    "ORS 316.563(2), 316.588(1); OAR 150-316-0465(1)(a)")
                .uses(Reading::Form).affects(a.exactly_threshold).clone(),
            working::new_divergence("The prior-year return")
                .form("The prior-year test needs a 12-month 2025 return filed before the 2026 return. A part-year return qualifies.",
                    "Publication OR-ESTIMATE (2026), Safe harbor; OAR 150-316-0493(3)(a)(B), (b)")
                .statute("The test needs only a 12-month prior year and the return filed for it.",
                    "ORS 316.587(8)(b)")
                .uses(Reading::Form).affects(a.prior_differs).clone(),
            working::new_divergence("A requirement first met after April 1")
                .form("Publication OR-ESTIMATE has a late starter pay in 3, 2 or 1 equal installments. They start June 15, September 15 or January 15. The Form OR-10 instructions divide the required annual payment by four. That applies to everyone but part-year residents and annualisers.",
                    "Publication OR-ESTIMATE (2026), If income starts or changes after April 1; Form OR-10 instructions (2025), line 3")
                .statute("A declaration may first be required after April 1, June 1 or September 1. It is then paid in three, two or one equal installments. Each required installment follows that schedule.",
                    "ORS 316.577, 316.579(3) to (5), 316.587(8)")
                .uses(Reading::Statute).affects(a.first > 0).clone(),
            working::new_divergence("The daily interest factor")
                .form("Each day is charged at a printed factor of 0.000219 for 2026. That is 8% divided by 365 and rounded to six places. Each worksheet line is rounded to a whole dollar.",
                    "Form OR-10 instructions (2025), line 4 and the underpayment interest worksheet")
                .statute("Interest is 8% a year from January 1, 2026. The daily rate is the annual rate divided by 365, without rounding.",
                    "ORS 316.587(1), 305.220; OAR 150-305-0140(1), (3)(b)")
                .uses(Reading::Form).affects(short).clone(),
        ] {
            out.push(b.build()?);
        }
        Ok(out)
    }

    /// The requirement and installments, with the divergences for these
    /// facts.
    fn assess(&self, f: &Facts) -> Result<Assessment, Error> {
        let mut a = self.measure(f)?;
        let divergences = self.divergences(f, &a)?;
        a.notes.diverges(&divergences);
        Ok(a)
    }

    fn measure(&self, f: &Facts) -> Result<Assessment, Error> {
        self.check(f)?;
        let extras = self.extras(f)?;
        let y = &self.y;
        let last = y.year - 1;
        let mut a = Assessment {
            dues: estimated::move_dates(&y.calendar, &y.dues),
            ..Assessment::default()
        };

        let tax_opt = f.this_year().tax();
        let tax_known = tax_opt.is_some();
        let tax = tax_opt.unwrap_or_default();
        a.tax_known = tax_known;
        let withheld = f.withheld_total();
        let has_prior = f.prior_year().is_some();
        let prior = f.prior_year().unwrap_or_default();
        let prior_tax = if has_prior {
            prior.figures().and_then(|pf| pf.tax()).unwrap_or_default()
        } else {
            USCents(0)
        };

        // 90% of this year's tax after all credits.
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
                "ORS 316.587(8)(a); OAR 150-316-0493(3)(a)(A); Publication OR-ESTIMATE (2026), worksheet line 20a",
                format!(
                    "{} of this year's Oregon tax after credits of {tax} is {}",
                    y.this_year_share,
                    this_year.amount()
                ),
            );
        }

        // 100% of last year's tax after all credits, on a 12-month return
        // filed before this year's.
        let (filed_first, filed_first_answered) = extras.prior_return_filed_first();
        let unavailable = |why: String| {
            estimated::new_target_option(Basis::PriorYearTax, y.prior_year_share, USCents(0))
                .unavailable(why)
                .build()
        };
        // Questions about the target, asked only when no exception sets it.
        let mut unless_exempt: Vec<(PositionKind, String, String)> = Vec::new();
        let prior_opt = if !has_prior {
            unless_exempt.push((
                PositionKind::PriorYearReturn,
                format!("Did you file a {last} Oregon return, and what was its tax?"),
                format!(
                    "With it, {} of last year's tax may be a lower target than {} of this year's.",
                    y.prior_year_share, y.this_year_share
                ),
            ));
            unavailable(format!("No {last} Oregon return on record"))?
        } else if !prior.filed() {
            unavailable(format!("You didn't file a {last} Oregon return"))?
        } else if prior.months() != 12 {
            unavailable(format!(
                "Your {last} Oregon return covered {} months, not 12",
                prior.months()
            ))?
        } else if filed_first_answered && !filed_first {
            let pa = y.prior_year_share.of(prior_tax);
            if !tax_known || pa < this_year.amount() {
                a.prior_differs = true;
            }
            unavailable(format!(
                "Your {last} Oregon return wasn't filed before your {} return",
                y.year
            ))?
        } else {
            a.notes.rule(
                "ORS 316.587(8)(b); OAR 150-316-0493(3)(a)(B), (b); Publication OR-ESTIMATE (2026), worksheet line 20b",
                format!(
                    "{} of the {prior_tax} tax after all credits on the {last} return is {}; Oregon has no higher percentage at any income, a part-year return qualifies, \
                     and the credits Form OR-10 subtracts include the kicker (2025 Form OR-40 line 32), so a {last} kicker lowers this figure",
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

        let mut rb = estimated::new_requirement();
        rb.option(this_year.clone()).option(prior_opt.clone());
        let (mut chosen, mut target) = (Basis::ThisYearTax, this_year.amount());
        if prior_opt.available() && (!tax_known || prior_opt.amount() < target) {
            (chosen, target) = (Basis::PriorYearTax, prior_opt.amount());
        }
        if !tax_known {
            unless_exempt.push((
                PositionKind::ThisYearTax,
                format!(
                    "What do you expect your {} Oregon tax after credits to be?",
                    y.year
                ),
                estimated::without_this_year_tax(prior_opt.available()),
            ));
        }
        if prior_opt.available() && chosen == Basis::PriorYearTax && !filed_first_answered {
            unless_exempt.push((
                PositionKind::JurisdictionFact,
                format!(
                    "Will you file your {last} Oregon return before your {} one?",
                    y.year
                ),
                format!(
                    "Only then can {} of last year's tax set your target; the figures assume it.",
                    y.prior_year_share
                ),
            ));
        }

        // Exception 2: no liability last year, as a full-year resident, for
        // 12 months.
        let no_liability = has_prior
            && prior.months() == 12
            && ((prior.filed() && prior_tax.0 == 0) || prior.not_required_to_file());
        let resident_opt = prior.full_year_resident();
        let residence_answered = resident_opt.is_some();
        let resident = resident_opt.unwrap_or_default();
        if no_liability && !residence_answered {
            a.notes.ask(
                PositionKind::Residency,
                format!("Were you an Oregon resident all of {last}?"),
                format!(
                    "If you were, having no Oregon tax that year means no interest for {}.",
                    y.year
                ),
            );
        }
        if no_liability && residence_answered && !resident {
            a.notes.rule(
                "ORS 316.587(5)(a); Form OR-10 instructions (2025), Exception 2",
                format!(
                    "Exception 2 is not open to a filer who was not a full-year resident in {last}, though the prior-year test still is"
                ),
            );
        }

        // ORS 316.563(1)'s gross income test, which the forms do not apply.
        let mut gross_met = true;
        let gross = f.this_year().gross_income();
        if gross.is_none_or(|g| g <= y.gross_income_test) {
            let (met, answered) = extras.gross_income_test_met();
            if answered {
                gross_met = met;
            }
            if !answered
                && !f.farmer_or_fisher()
                && !(no_liability && resident)
                && !(tax_known && tax - withheld < y.threshold)
            {
                a.notes.ask(
                    PositionKind::JurisdictionFact,
                    format!(
                        "Is your gross income over {}, or over $1,000 other than wages?",
                        y.gross_income_test.whole()
                    ),
                    format!(
                        "Some married filers use $10,000 or $5,000 in place of {}. If not, the law needs no declaration; forms still charge interest on shortfalls.",
                        y.gross_income_test.whole()
                    ),
                );
            }
        }

        if f.farmer_or_fisher() {
            a.exempt = true;
            let ex = estimated::new_exception("Two-thirds of gross income from farming or fishing")
                .build()?;
            rb.required(
                false,
                format!(
                    "At least two-thirds of your gross income is from farming or fishing. Oregon requires no estimated payments for {}. It charges no underpayment interest.",
                    y.year
                ),
            )
            .exempt(ex);
            a.notes.rule(
                "ORS 316.573(1); Form OR-10 instructions (2025), Exception 1; Publication OR-ESTIMATE (2026), Exception for farmers and commercial fishermen",
                format!(
                    "farming and fishing are {} and {} of gross income, at least two-thirds together, so no estimated payments are required and there is no interest",
                    f.farm_share(),
                    f.fish_share()
                ),
            );
        } else if no_liability && residence_answered && resident {
            a.exempt = true;
            let ex = estimated::new_exception("No Oregon tax last year").build()?;
            rb.required(
                false,
                format!(
                    "You had no Oregon tax for {last} as a full-year resident on a 12-month return. No estimated payments are needed. Oregon charges no underpayment interest for {}.",
                    y.year
                ),
            )
            .exempt(ex);
            a.notes.rule(
                "ORS 316.587(5)(a); Form OR-10 instructions (2025), Exception 2",
                format!(
                    "the {last} return showed no tax after credits for a full-year resident over 12 months, so there is no interest for {}",
                    y.year
                ),
            );
        } else if tax_known && tax - withheld < y.threshold {
            a.exempt = true;
            let below =
                estimated::exception_below("Tax less withholding", tax - withheld, y.threshold)?;
            let why = if withheld > tax {
                format!(
                    "Your Oregon tax of {} less {} withheld {}. No estimated payments are needed.",
                    tax.whole(),
                    withheld.whole(),
                    estimated::leaves(tax - withheld)
                )
            } else if withheld == tax {
                format!(
                    "Your Oregon withholding of {} covers your tax of {}. No estimated payments are needed.",
                    withheld.whole(),
                    tax.whole()
                )
            } else {
                format!(
                    "{} That is under {}, so no estimated payments are needed. No underpayment interest is charged.",
                    owed_why(tax, withheld),
                    y.threshold.whole()
                )
            };
            rb.required(false, why).exempt(below);
            a.notes.rule(
                "ORS 316.588(1); OAR 150-316-0465(1)(a); Form OR-10 instructions (2025), required annual payment worksheet line 5",
                format!(
                    "{tax} of tax after credits less {withheld} of withholding is {}, under {}, so there is no interest",
                    tax - withheld,
                    y.threshold
                ),
            );
        } else if !tax_known && !prior_opt.available() {
            rb.required(
                false,
                format!(
                    "With no {last} Oregon return, your target is {} of your {} tax.",
                    y.this_year_share, y.year
                ),
            )
            .no_target();
        } else if !gross_met {
            a.gross_not_met = true;
            rb.required(
                false,
                format!(
                    "You don't meet Oregon's gross income test. The law then requires no declaration of estimated tax. Oregon's forms don't apply that test. Form OR-10 still charges interest on a shortfall against the {} needed.",
                    target.whole()
                ),
            )
            .choose(chosen);
            a.notes.rule(
                "ORS 316.563(1); ORS 316.587, 316.588",
                "the gross income test is not met, so no declaration is required; the interest sections do not refer to the test, so the interest is figured as Form OR-10 figures it",
            );
        } else if !tax_known && withheld >= target {
            rb.required(
                false,
                format!(
                    "Your withholding of {} covers {} of your {last} tax, {}. No estimated payments are needed, whatever your {} tax turns out to be.",
                    withheld.whole(),
                    y.prior_year_share,
                    target.whole(),
                    y.year
                ),
            )
            .choose(chosen);
        } else if !tax_known {
            rb.required(
                true,
                format!(
                    "Your target is {} of your {last} tax, {}. It keeps you clear of interest whatever your {} tax turns out to be. {}",
                    y.prior_year_share,
                    target.whole(),
                    y.year,
                    left_why(target, withheld)
                ),
            )
            .choose(chosen);
        } else if withheld >= target {
            let why = if target.0 == 0 {
                format!(
                    "Your {last} Oregon tax was $0, so your target is $0. No estimated payments are needed."
                )
            } else {
                format!(
                    "Your Oregon withholding of {} covers the {} needed for the year. No estimated payments are needed.",
                    withheld.whole(),
                    target.whole()
                )
            };
            rb.required(false, why).choose(chosen);
        } else if target - withheld < y.threshold {
            a.worksheet_differs = true;
            rb.required(
                true,
                format!(
                    "{} That is {} or more, so any shortfall is charged interest. Your target is {}. Paying the {} left after withholding in equal installments avoids it. Oregon's worksheet doesn't require it.",
                    owed_why(tax, withheld),
                    y.threshold.whole(),
                    target.whole(),
                    (target - withheld).whole()
                ),
            )
            .choose(chosen);
        } else {
            rb.required(
                true,
                format!(
                    "{} That is {} or more, so estimated payments are needed. {}",
                    owed_why(tax, withheld),
                    y.threshold.whole(),
                    target_why(target, withheld)
                ),
            )
            .choose(chosen);
        }
        a.exactly_threshold = tax_known && tax - withheld == y.threshold;
        if a.exempt {
            a.prior_differs = false;
        }
        a.requirement = rb.build()?;
        if !a.exempt {
            for (k, q, e) in unless_exempt {
                a.notes.ask(k, q, e);
            }
        }
        if a.exempt || !a.requirement.target_known() {
            a.required = vec![USCents(0); a.dues.len()];
            return Ok(a);
        }

        let regular = if part_year(f) {
            // A part-year resident: the target in equal parts over the
            // installment periods of residence or Oregon business income.
            let extras = self.extras(f)?;
            let counted = self.periods_counted(f, &extras, &mut a);
            let cum = shares_over(a.requirement.target(), &counted)?;
            let regular = estimated::increments(&cum);
            a.notes.rule(
                "Form OR-10 instructions (2025), Line 3, Note for part-year residents",
                format!(
                    "{} is divided over the {} installment periods of Oregon residence or Oregon business income, {}; the other periods need $0",
                    a.requirement.target(),
                    count(&counted),
                    counted_list(&counted, &a.dues)
                ),
            );
            a.counted = Some(counted);
            regular
        } else {
            // The installments: equal parts of the target, from the first
            // date the filer's requirement applies.
            a.first = self.first_installment(f, &mut a);
            let cum = estimated::equal_shares(
                a.requirement.target(),
                a.dues.len() as i64,
                a.first as i64,
            )?;
            let regular = estimated::increments(&cum);
            let n = a.dues.len() - a.first;
            a.notes.rule(
                "ORS 316.579(2) to (5); OAR 150-316-0493(4)(a)(A); Publication OR-ESTIMATE (2026), Estimated tax payment due dates",
                format!(
                    "{} is due in {n} equal installments of about {}, from {}; a date on a Saturday, Sunday or Oregon legal holiday moves to the next business day",
                    a.requirement.target(),
                    regular[a.first],
                    a.dues[a.first].long()
                ),
            );
            regular
        };
        a.required = regular.clone();

        if f.elections().annualised() {
            let taxes: Vec<USCents> = f.periods().iter().map(|p| p.annualised_tax()).collect();
            let ann = estimated::annualised(&taxes, &y.annualised, &regular)?;
            a.notes.rule(
                "ORS 316.587(8)(c); OAR 150-316-0493(4); Form OR-10 instructions (2025), annualized income worksheet lines 23 to 31",
                format!(
                    "under the annualised method the installments are {}, {}, {} and {}: 22.5%, 45%, 67.5% and 90% of the net annualised tax, less earlier installments, \
                     and never more than the regular installment plus any amount the method lowered before",
                    ann[0], ann[1], ann[2], ann[3]
                ),
            );
            a.required = ann;
        }
        Ok(a)
    }

    /// The installment a filer's payments start at, from 0, under the
    /// late-start chart in Publication OR-ESTIMATE and ORS 316.579(3) to (5).
    fn first_installment(&self, f: &Facts, a: &mut Assessment) -> usize {
        let y = &self.y;
        let met = f.requirement_first_met();
        if met.is_zero() {
            a.notes.ask(
                PositionKind::RequirementFirstMet,
                format!(
                    "When did you first expect to need estimated payments for {}?",
                    y.year
                ),
                format!(
                    "Until then, the figures use four installments. A start after {} would mean three, two or one.",
                    short_date(y.late_starts[0].add_days(-1), y.year)
                ),
            );
            return 0;
        }
        let first = estimated::late_start(met, &y.late_starts).max(0) as usize;
        if first > 0 {
            a.notes.ask(
                PositionKind::UnsettledRule,
                "",
                format!(
                    "The law and OR-ESTIMATE have you pay {} equal installments from {}. The Form OR-10 instructions say four, and the figures follow the law.",
                    y.dues.len() - first,
                    short_date(y.dues[first], y.year)
                ),
            );
        }
        first
    }

    /// Which installment periods a part-year resident's required annual
    /// payment is divided over: each period with a day of Oregon residence,
    /// and each the filer had Oregon business income in. A period runs from
    /// the day after the previous due date, or January 1, to its own due
    /// date, as the instructions' "use the payment due dates as a guide"
    /// reads.
    fn periods_counted(&self, f: &Facts, extras: &EstimatedFacts, a: &mut Assessment) -> Vec<bool> {
        let y = &self.y;
        let mut counted = vec![false; a.dues.len()];
        let mut partial = false;
        let mut from = estimated::must_date(y.year, Month::JANUARY, 1);
        let dec31 = estimated::must_date(y.year, Month::DECEMBER, 31);
        for (i, &due) in a.dues.iter().enumerate() {
            let to = if due.after(dec31) { dec31 } else { due };
            let mut days = 0;
            for p in f.residence() {
                if p.state() != OREGON.as_str() {
                    continue;
                }
                let (mut start, mut end) = (p.from(), p.to());
                if start.before(from) {
                    start = from;
                }
                if end.after(to) {
                    end = to;
                }
                if !end.before(start) {
                    days += end.days_since(start) + 1;
                }
            }
            if days > 0 {
                counted[i] = true;
                partial = partial || days < to.days_since(from) + 1;
            }
            from = due.add_days(1);
        }
        let (business, answered) = extras.business_income_in();
        for n in business {
            if let Some(c) = counted.get_mut((n - 1) as usize) {
                *c = true;
            }
        }
        if !answered {
            a.notes.ask(
                PositionKind::JurisdictionFact,
                "Did you have Oregon business income in an installment period spent elsewhere?",
                "If so, that period counts too; until then, only your Oregon periods count.",
            );
        }
        if partial {
            a.notes.ask(
                PositionKind::UnsettledRule,
                "",
                "Form OR-10 doesn't say if a partly resident period counts; the figures count it.",
            );
        }
        if !f.requirement_first_met().is_zero() {
            a.notes.ask(
                PositionKind::UnsettledRule,
                "",
                "The Form OR-10 instructions don't say if the late-start chart applies part-year. The figures divide by your periods of residence alone.",
            );
        }
        counted
    }

    /// The withholding deemed paid on each due date, and the dated payments.
    /// Withholding is spread in equal parts over the installments the filer
    /// owes, unless the filer shows the dates it was withheld; then it counts
    /// on those dates and none is deemed. Last year's overpayment applied
    /// forward counts on the first due date.
    fn credits(
        &self,
        f: &Facts,
        a: &Assessment,
    ) -> Result<(Vec<USCents>, Vec<Payment>, Notes), Error> {
        let mut notes = Notes::default();
        let mut deemed = vec![USCents(0); a.dues.len()];
        let mut payments = Vec::new();
        if f.elections().withholding_as_withheld() {
            for w in f.withholding() {
                payments.push(
                    estimated::new_payment()
                        .on(w.on())
                        .amount(w.amount())
                        .build()?,
                );
            }
            notes.rule(
                "ORS 316.587(6); Form OR-10 instructions (2025), lines 1, 6, 11 and 17",
                format!(
                    "the {} withheld is credited on the dates it was withheld, as shown",
                    f.withheld_total()
                ),
            );
        } else if let Some(counted) = &a.counted {
            let cum = shares_over(f.withheld_total(), counted)?;
            deemed = estimated::increments(&cum);
            notes.rule(
                "Form OR-10 instructions (2025), lines 1, 6, 11 and 17",
                format!(
                    "the {} withheld is divided by the {} installment periods used for the required installments, and treated as paid in equal parts on their due dates",
                    f.withheld_total(),
                    count(counted)
                ),
            );
        } else {
            let cum =
                estimated::equal_shares(f.withheld_total(), a.dues.len() as i64, a.first as i64)?;
            deemed = estimated::increments(&cum);
            notes.rule(
                "ORS 316.587(6); Form OR-10 instructions (2025), lines 1, 6, 11 and 17",
                format!(
                    "the {} withheld is treated as paid in {} equal parts on the due dates, whenever it was actually withheld",
                    f.withheld_total(),
                    a.dues.len() - a.first
                ),
            );
        }
        for p in f.payments() {
            if p.from_prior_year() {
                payments.push(
                    estimated::new_payment()
                        .on(a.dues[0])
                        .amount(p.amount())
                        .build()?,
                );
            } else {
                payments.push(p);
            }
        }
        Ok((deemed, payments, notes))
    }

    /// The schedule: withholding deemed paid on the due dates and the dated
    /// payments, applied to the earliest installment first.
    fn installments(&self, f: &Facts, a: &Assessment) -> Result<(Vec<Installment>, Notes), Error> {
        let (deemed, payments, mut notes) = self.credits(f, a)?;
        let mut credits = Vec::new();
        for (i, &w) in deemed.iter().enumerate() {
            if w.0 > 0 {
                credits.push(estimated::new_payment().on(a.dues[i]).amount(w).build()?);
            }
        }
        credits.extend(payments);
        let ledger = estimated::apply(&a.dues, &a.required, &credits)?;
        let mut out = Vec::with_capacity(ledger.len());
        for (i, l) in ledger.iter().enumerate() {
            out.push(
                estimated::new_installment(i as i64 + 1, l.due())
                    .needed_by(l.needed_by())
                    .credited_by(l.credited_by())
                    .shortfall(l.shortfall())
                    .build()?,
            );
        }
        notes.rule(
            "ORS 316.579(10), 316.587(4)",
            "a payment is applied to the earliest unpaid installment first, and any excess to the next one due",
        );
        Ok((out, notes))
    }
}

impl EstimatedTax for OregonEstimated {
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
                    "with no {} Oregon return to measure against, there is no target to schedule",
                    self.y.year - 1
                ),
            ));
        }
        let (out, notes) = self.installments(f, &a)?;
        Ok(working::new_result(out)
            .with(&a.notes)
            .with(&notes)
            .build()?)
    }

    fn penalty(&self, f: &Facts, as_of: Date) -> Result<working::Result<Penalty>, Error> {
        if as_of.is_zero() {
            return Err(Error::Invalid(
                "oregontaxcalc: a penalty needs the date to figure it to".to_string(),
            ));
        }
        let a = self.assess(f)?;
        if !a.requirement.target_known() {
            return Err(Error::with_detail(
                Error::NeedsThisYearTax,
                format!(
                    "with no {} Oregon return to measure against, there is no target to figure interest from",
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
        let waiver_answer = f.positions().waiver();
        let waiver = waiver_answer.unwrap_or_default();
        let (schedule, _) = self.installments(f, &a)?;
        if a.exempt {
            let p = estimated::new_penalty(stop)
                .waiver(waiver)
                .because(a.requirement.clone(), &schedule)
                .build()?;
            return Ok(working::new_result(p).with(&a.notes).build()?);
        }

        let (deemed, payments, mut notes) = self.credits(f, &a)?;
        let (periods, unrated) =
            worksheet(&a.dues, &a.required, &deemed, &payments, stop, &y.daily)?;
        let mut pb = estimated::new_penalty(stop);
        pb.waiver(waiver).because(a.requirement.clone(), &schedule);
        if !a.tax_known {
            pb.upper_bound();
        }
        for (i, p) in periods.iter().enumerate() {
            if !p.reached || p.shortfall.0 <= 0 {
                continue;
            }
            let mut lb = estimated::new_penalty_line(i as i64 + 1, a.dues[i], p.shortfall);
            let mut days = 0;
            for part in &p.parts {
                lb.part(part.clone());
                days += part.days();
            }
            pb.line(lb.days(days).build()?);
        }
        let mut pen = pb.build()?;
        if pen.total().0 == 0 && pen.short_due_dates() > 0 {
            // Form OR-10 rounds each line's interest to the nearest whole
            // dollar, so a short due date can owe none.
            pen = pen
                .to_builder()
                .no_penalty(
                    "No penalty: Oregon's worksheet rounds the interest on the shortfall to $0.",
                )
                .build()?;
        }
        if !a.tax_known {
            notes.rule(
                "ORS 316.587(8); OAR 150-316-0493(3)(a)",
                format!(
                    "this year's Oregon tax was not given, so the interest is figured against {} of the {} tax alone; \
                     the required annual payment is the lesser of that and 90% of this year's tax, so {} is the most the interest can be, and it is less if this year's tax turns out lower",
                    y.prior_year_share,
                    y.year - 1,
                    pen.total()
                ),
            );
        }
        if paid_in_full {
            notes.rule(
                "ORS 316.587(3)(b); Form OR-10 instructions (2025), line 4",
                format!(
                    "the return was filed with the whole balance paid on {}, so the balance stopped accruing interest that day",
                    stop.long()
                ),
            );
        }
        notes.rule(
            "ORS 316.587(1) to (4); Form OR-10 instructions (2025), line 4 and the underpayment interest worksheet",
            format!(
                "interest is charged on the running balance of installments less withholding and payments, from one event to the next, at the printed daily rate, \
                 each line rounded to a whole dollar, until the balance is paid or {}; Oregon has no January 31 waiver; the interest to {} is {}",
                y.stop.long(),
                stop.long(),
                pen.total()
            ),
        );
        if !unrated.is_zero() {
            pen = pen
                .to_builder()
                .rated_through(unrated.add_days(-1))
                .build()?;
            notes.ask(
                PositionKind::UnpublishedRate,
                "",
                format!(
                    "The rate for the days after {} isn't published yet.",
                    short_date(unrated.add_days(-1), 0)
                ),
            );
        }
        if pen.total().0 > 0 && waiver_answer.is_none() {
            notes.ask(
                PositionKind::PenaltyWaiver,
                "Was the shortfall due to a casualty, disaster or other unusual circumstance?",
                "Retiring at 62 or older or a disability, with reasonable cause, counts too. Oregon decides case by case; until then, the interest is shown in full.",
            );
        }
        // ORS 316.587(5)(b), (c) and Form OR-10 Exceptions 3 and 4 grant the
        // federal pair of waivers.
        if let Some(o) = estimated::standard_waiver(f, "the Oregon Department of Revenue", 62)
            && pen.total().0 > 0
        {
            pen = pen.to_builder().waiver_outcome(o).build()?;
        }
        Ok(working::new_result(pen)
            .with(&a.notes)
            .with(&notes)
            .build()?)
    }

    fn explanation(&self) -> Explanation {
        OREGON_EXPLANATION.clone()
    }
}

/// Orders the events on one date as the Form OR-10 worksheet lists them:
/// withholding, then the required installment, then payments.
#[derive(Clone, Copy, Debug, PartialEq, Eq, PartialOrd, Ord)]
enum EventKind {
    Withholding,
    Installment,
    Payment,
}

#[derive(Clone, Copy, Debug)]
struct WorksheetEvent {
    on: Date,
    kind: EventKind,
    amount: USCents,
}

/// One installment's block of the worksheet.
#[derive(Clone, Debug, Default)]
struct PeriodInterest {
    /// Whether the installment fell due by the stop date.
    reached: bool,

    /// The running balance just after the installment fell due, which
    /// includes any earlier installment still unpaid.
    shortfall: USCents,

    /// The worksheet lines in the block that charged interest.
    parts: Vec<PenaltyPart>,
}

/// The Form OR-10 underpayment interest worksheet. It is the one place
/// Oregon's arithmetic differs from [`estimated::apply`] and
/// [`estimated::simple_penalty`]: the interest is on the running balance
/// between events, and each line is rounded to a whole dollar, rather than
/// on each installment's shortfall piece by piece.
///
/// Each due date has a withholding line, then a required installment line;
/// each payment is a line. The withholding line charges the balance after
/// the withholding for that one day. Every other line charges its balance
/// for the days after its date up to and including the next event's date,
/// or the day before it when the next event is a withholding line. A line
/// whose days fall in two factor periods is split, as the worksheet's rate
/// change line splits it. Lines after `stop` are not reached.
///
/// The returned date, `unrated`, is the first day no factor covers, or the
/// zero Date.
fn worksheet(
    dues: &[Date],
    required: &[USCents],
    deemed: &[USCents],
    payments: &[Payment],
    stop: Date,
    s: &FactorSeries,
) -> Result<(Vec<PeriodInterest>, Date), Error> {
    if required.len() != dues.len() || deemed.len() != dues.len() {
        return Err(Error::Invalid(
            "oregontaxcalc: one required installment and one withholding amount per due date"
                .to_string(),
        ));
    }
    let mut evs = Vec::new();
    for (i, &d) in dues.iter().enumerate() {
        evs.push(WorksheetEvent {
            on: d,
            kind: EventKind::Withholding,
            amount: deemed[i],
        });
        evs.push(WorksheetEvent {
            on: d,
            kind: EventKind::Installment,
            amount: required[i],
        });
    }
    for p in payments {
        evs.push(WorksheetEvent {
            on: p.on(),
            kind: EventKind::Payment,
            amount: p.amount(),
        });
    }
    evs.sort_by(|a, b| a.on.days().cmp(&b.on.days()).then(a.kind.cmp(&b.kind)));

    let mut out = vec![PeriodInterest::default(); dues.len()];
    let mut period: Option<usize> = None;
    let mut balance = USCents(0);
    let mut unrated = Date::default();
    for (k, e) in evs.iter().enumerate() {
        if e.on.after(stop) {
            break;
        }
        match e.kind {
            EventKind::Withholding => {
                period = Some(period.map_or(0, |p| p + 1));
                balance -= e.amount;
            }
            EventKind::Installment => {
                balance += e.amount;
                if let Some(p) = period {
                    out[p].reached = true;
                    out[p].shortfall = balance.max(USCents(0));
                }
            }
            EventKind::Payment => {
                balance -= e.amount;
            }
        }
        let (mut from, mut to) = (e.on, stop);
        if e.kind == EventKind::Withholding {
            from = e.on.add_days(-1);
            to = e.on;
        } else if let Some(next) = evs.get(k + 1) {
            to = if next.kind == EventKind::Withholding {
                next.on.add_days(-1)
            } else {
                next.on
            };
        }
        if stop.before(to) {
            to = stop;
        }
        let Some(p) = period else {
            continue;
        };
        if balance.0 <= 0 || !to.after(from) {
            continue;
        }
        let (parts, gap) = estimated::daily_factor(balance, from, to, s)?;
        if !gap.is_zero() && (unrated.is_zero() || gap.before(unrated)) {
            unrated = gap;
        }
        out[p].parts.extend(parts);
    }
    Ok((out, unrated))
}

/// Written by hand and checked against research/estimated-payments/or.md.
static OREGON_EXPLANATION: LazyLock<Explanation> = LazyLock::new(|| {
    estimated::new_explanation(
        "Take your 2026 Oregon tax after credits, less withholding. If that is $1,000 or more, Oregon expects estimated payments. The charge for paying late is interest on the unpaid balance, by the day.")
        .paragraph("There is no interest if withholding and estimated payments reach a target. It is the lesser of 90% of this year's tax after credits or 100% of last year's. Last year's figure is 100% at every income, with no 110% tier. It needs a full 12-month return filed before this year's. If you had no Oregon return last year, only the 90% figure is available.")
        .paragraph("Pay a quarter by April 15, June 15, September 15 and the next January 15. You can instead pay the whole amount by April 15. A date on a weekend or an Oregon legal holiday shifts to the next business day. Withholding counts as paid evenly on those dates. You can instead show when it was withheld. If you first need to pay after April 1, you pay fewer, larger installments. They are three, two or one equal installments from the next date.")
        .paragraph("The interest runs on the unpaid balance from one event to the next. Each installment raises the balance, and withholding and payments lower it. Payments go to the oldest unpaid installment first. The rate is 8% for 2026, charged at a daily rate the department prints. Interest stops when the balance is paid or on April 15 of the next year. Uneven income can use the annualised method on Form OR-10. It uses 22.5%, 45%, 67.5% and 90% of the annualised tax.")
        .paragraph("There is no interest if your tax after credits less withholding is under $1,000. There is none either for a full-year resident with no Oregon tax last year. The department can also excuse it for a casualty or disaster. It can do the same for retiring at 62 or older or becoming disabled. That needs reasonable cause.")
        .versus_federal("Unlike federal, the prior-year test is 100% at every income, with no 110% tier. Due dates move for Oregon's own legal holidays. The charge is interest on a running balance, with each line in whole dollars. A late starter pays in fewer, larger installments.")
        .source("ORS 316.557 to 316.589, 305.220 and 187.010 (2025 edition)")
        .source("OAR 150-316-0465 to 150-316-0497; OAR 150-305-0140 and 150-305-0144")
        .source("Publication OR-ESTIMATE, Oregon Estimated Income Tax Instructions, 2026 (150-101-026, Rev. 10-07-25)")
        .source("Form OR-10 Instructions, Underpayment of Oregon Estimated Tax, 2025 (150-101-031-1, Rev. 9-16-25)")
        .build()
        .unwrap_or_default()
});
