//! Pennsylvania's estimated tax rules, Tax Reform Code of 1971 §§ 325, 326
//! and 352(d) (72 P.S. §§ 7325, 7326, 7352(d)), as the shared
//! [`estimated::EstimatedTax`] interface.
//!
//! Pennsylvania differs from the federal rules in shape, not only in
//! numbers. Whether payments are needed turns on income received without
//! withholding. The prior-year test is last year's taxable income at this
//! year's rate, not last year's tax. The penalty is measured against 90% of
//! this year's tax in every case, and the prior-year test and the
//! income-to-date test are exceptions that take the interest off an
//! installment rather than lowering it. A filer who first meets the
//! requirement late can have the earlier installments excused, but only
//! under a special exception.
//!
//! The figure follows the department's forms, REV-413 (I) for 2026 and
//! REV-1630 for 2025, and the result's divergences say where the Tax Reform
//! Code reads otherwise. No act since 2022 changed the estimated tax rules
//! (research/estimated-payments/pa.md), so no form here predates the law it
//! describes.

use std::any::Any;

use estimated::{
    Basis, Calendar, Date, Error, EstimatedTax, Explanation, FactorSeries, Facts, Figures,
    Installment, LedgerEntry, Penalty, PriorReturn, Requirement, TargetOptionBuilder, WaiverKind,
};
use money::{Rate, USCents, percent};
use working::{Divergence, Notes, PositionKind, Reading};

/// The facts Pennsylvania's rules read that no other jurisdiction does.
/// Carry them in [`estimated::Facts::state`].
///
/// Pennsylvania's REV-1630 starts from the tax before credits and subtracts
/// the credits together with withholding. [`estimated::Figures::tax`] is
/// after credits everywhere, so the credits come here, and the rules add
/// them back.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct EstimatedFacts {
    income_without_withholding: Option<USCents>,
    credits: Option<USCents>,
    prior_forgiveness: Option<USCents>,
    prior_full_forgiveness: Option<bool>,
}

impl EstimatedFacts {
    /// The year's PA-taxable income from which no employer withholds
    /// Pennsylvania tax: REV-414 (I) line 1. It decides whether the
    /// estimated tax rules apply at all.
    pub fn income_without_withholding(&self) -> Option<USCents> {
        self.income_without_withholding
    }

    /// This year's Tax Forgiveness, resident and other credits, PA-40 lines
    /// 21, 22 and 23. [`estimated::Figures::tax`] is already net of them;
    /// the 90% test is taken on the tax before them.
    pub fn credits(&self) -> Option<USCents> {
        self.credits
    }

    /// Last year's Tax Forgiveness Credit, PA-40 line 21, which REV-1630
    /// subtracts from the prior-year income test.
    pub fn prior_year_forgiveness(&self) -> Option<USCents> {
        self.prior_forgiveness
    }

    /// Whether the filer qualified for 100% Tax Forgiveness last year, which
    /// takes this year out of the penalty, or `None` when the caller did not
    /// say.
    pub fn prior_year_full_forgiveness(&self) -> Option<bool> {
        self.prior_full_forgiveness
    }
}

/// Builds [`EstimatedFacts`].
#[derive(Clone, Debug, Default)]
pub struct EstimatedFactsBuilder {
    e: EstimatedFacts,
}

/// Starts an [`EstimatedFacts`].
pub fn new_estimated_facts() -> EstimatedFactsBuilder {
    EstimatedFactsBuilder::default()
}

impl EstimatedFactsBuilder {
    /// Sets the year's income without Pennsylvania withholding.
    pub fn income_without_withholding(&mut self, a: USCents) -> &mut Self {
        self.e.income_without_withholding = Some(a);
        self
    }

    /// Sets this year's credits.
    pub fn credits(&mut self, a: USCents) -> &mut Self {
        self.e.credits = Some(a);
        self
    }

    /// Sets last year's Tax Forgiveness Credit.
    pub fn prior_year_forgiveness(&mut self, a: USCents) -> &mut Self {
        self.e.prior_forgiveness = Some(a);
        self
    }

    /// Says whether the filer qualified for 100% Tax Forgiveness last year.
    pub fn prior_year_full_forgiveness(&mut self, qualified: bool) -> &mut Self {
        self.e.prior_full_forgiveness = Some(qualified);
        self
    }

    /// Refuses negative credits. Income can be negative, as a loss.
    pub fn build(&self) -> Result<EstimatedFacts, Error> {
        let e = self.e;
        if e.credits.unwrap_or_default().0 < 0 || e.prior_forgiveness.unwrap_or_default().0 < 0 {
            return Err(Error::Invalid(
                "pennsylvaniataxcalc: credits cannot be negative".into(),
            ));
        }
        Ok(e)
    }
}

/// One tax year's published figures.
pub(crate) struct EstimatedYear {
    pub(crate) year: i64,

    /// The personal income tax rate, 72 P.S. § 7302.
    pub(crate) rate: Rate,

    /// The income not subject to withholding at which the rules start, and
    /// the tax after withholding and credits below which they do not.
    pub(crate) income_threshold: USCents,
    pub(crate) tax_threshold: USCents,

    pub(crate) dues: Vec<Date>,

    /// The first days of the REV-413 (I) table's second, third and fourth
    /// rows: a requirement first met on or after `later_groups[i]` starts at
    /// installment i+2.
    pub(crate) later_groups: Vec<Date>,

    pub(crate) calendar: Calendar,
    pub(crate) file_and_pay_by: Date,
    pub(crate) stop: Date,
    pub(crate) period_ends: Vec<Date>,

    /// The factor REV-1611 prints for each calendar year, one period a year,
    /// so that each year's days are charged and rounded on their own as
    /// REV-1630 lines 15a to 15c do.
    pub(crate) daily: FactorSeries,
}

/// REV-1630 line 5: line 4, after all withholding and credits, divided by
/// four. Withholding is therefore treated as paid in four equal parts, and
/// so are the credits.
fn quarters() -> [Rate; 4] {
    [percent(25), percent(25), percent(25), percent(25)]
}

/// The year this build carries, or `None`.
fn estimated_year(year: i64) -> Option<Result<EstimatedYear, Error>> {
    match year {
        2026 => Some(crate::estimated_y2026::year_2026()),
        _ => None,
    }
}

/// Returns Pennsylvania's estimated tax rules for a tax year, or
/// [`Error::NoRules`] for a year this build does not carry.
pub fn estimated_tax(year: i64) -> Result<Box<dyn EstimatedTax>, Error> {
    match estimated_year(year) {
        Some(y) => Ok(Box::new(PennsylvaniaEstimated { y: y? })),
        None => Err(estimated::no_rules(&estimated::PENNSYLVANIA, year)),
    }
}

struct PennsylvaniaEstimated {
    y: EstimatedYear,
}

/// What the three questions share.
#[derive(Default)]
struct Assessment {
    requirement: Requirement,
    exempt: bool,
    dues: Vec<Date>,

    /// Whether this year's tax was given. Without it the regular
    /// installments are measured against the prior-year test, and the
    /// penalty cannot be figured for an installment that misses it.
    tax_known: bool,

    /// Each installment REV-1630 line 5 requires: a quarter of 90% of the
    /// tax before credits, less the credits. The penalty is on a shortfall
    /// against these, whichever exception the filer meets.
    regular: Vec<USCents>,

    /// The running total of the smaller of the two tests, a quarter a time.
    /// Credits that reach it by a due date put that installment under
    /// Exception 1 if the prior-year test is the smaller.
    lesser: Vec<USCents>,

    /// The running totals that excuse an installment, or `None` where the
    /// exception is not open.
    exception1: Option<Vec<USCents>>,
    exception2: Option<Vec<USCents>>,

    /// This year's PA-40 lines 21 to 23.
    credits: USCents,

    /// The installment a late starter's requirement starts at, from 0; and
    /// whether the filer owed no estimates last year, which the special
    /// exception needs.
    first: usize,
    special: bool,

    excused_january: bool,

    notes: Notes,
}

/// Go's `%T` of a value handed in as a jurisdiction's extras, for the types
/// a caller is likely to have passed.
fn type_name(v: &(dyn Any + Send + Sync)) -> &'static str {
    if v.is::<String>() || v.is::<&str>() {
        "string"
    } else if v.is::<i64>() {
        "int"
    } else {
        "an unknown type"
    }
}

fn invalid(text: String) -> Error {
    Error::Invalid(text)
}

/// Which divergences the facts reach: where the two readings would give a
/// different answer, schedule or penalty.
#[derive(Clone, Copy, Default)]
struct Touches {
    /// Income without withholding of exactly the threshold.
    start: bool,
    /// A requirement first met after the first group starts.
    late_start: bool,
    /// A return filed and paid by January 31, not a September starter.
    january: bool,
    /// A filed return as a part-year resident last year.
    part_year: bool,
    /// A shortfall that bears interest.
    interest: bool,
}

/// Where each installment stands once the exceptions are applied.
#[derive(Clone, Copy, Default)]
struct Standing {
    /// The chosen target's running total.
    needed_by: USCents,
    excused: bool,
    /// The exception that excused it, for the working note.
    applied: &'static str,
}

// The exceptions a standing names, which only the working notes read.
const APPLIED_JANUARY: &str = "the return was filed and paid by January 31";
const APPLIED_SPECIAL: &str = "the special exception: no estimates were owed last year, and the whole catch-up was paid by the first date the requirement applied";
const APPLIED_EXCEPTION1: &str =
    "Exception 1: withholding and payments reached last year's income at this year's rate";
const APPLIED_EXCEPTION2: &str = "Exception 2: withholding and payments reached 90% of the tax on the income received to the period's end";

/// A question whose answer can change the target. It is asked only when no
/// exception sets the target.
struct TargetQuestion {
    position: PositionKind,
    question: String,
    effect: String,
}

#[derive(Default)]
struct TargetQuestions(Vec<TargetQuestion>);

impl TargetQuestions {
    fn ask(&mut self, position: PositionKind, question: String, effect: String) {
        self.0.push(TargetQuestion {
            position,
            question,
            effect,
        });
    }

    /// Adds the questions to `notes` unless an exception set the target.
    fn ask_unless_exempt(self, notes: &mut Notes, exempt: bool) {
        if exempt {
            return;
        }
        for t in self.0 {
            notes.ask(t.position, t.question, t.effect);
        }
    }
}

/// An amount under `limit`, as "$742, under $1,000", or "just under $1,000"
/// when whole dollars would show the two as equal.
fn left_under(amount: USCents, limit: USCents) -> String {
    if amount.whole() == limit.whole() {
        return format!("just under {}", limit.whole());
    }
    format!("{}, under {}", amount.whole(), limit.whole())
}

impl PennsylvaniaEstimated {
    fn extras(&self, f: &Facts) -> Result<EstimatedFacts, Error> {
        match f.state() {
            None => Ok(EstimatedFacts::default()),
            Some(s) => match s.downcast_ref::<EstimatedFacts>() {
                Some(e) => Ok(*e),
                None => Err(invalid(format!(
                    "pennsylvaniataxcalc: the facts carry {}, not pennsylvaniataxcalc.EstimatedFacts",
                    type_name(s)
                ))),
            },
        }
    }

    fn check(&self, f: &Facts) -> Result<(), Error> {
        if f.jurisdiction() != estimated::PENNSYLVANIA {
            return Err(invalid(format!(
                "pennsylvaniataxcalc: these are Pennsylvania's rules, and the facts are for {}",
                f.jurisdiction()
            )));
        }
        if f.tax_year() != self.y.year {
            return Err(invalid(format!(
                "pennsylvaniataxcalc: these are the {} rules, and the facts are for {}",
                self.y.year,
                f.tax_year()
            )));
        }
        if i64::from(f.farm_share().0) * 3 >= 20_000 {
            return Err(Error::with_detail(
                Error::Unsupported,
                "a farmer with two-thirds of gross income from farming files REV-1630A",
            ));
        }
        if f.elections().annualised() {
            for (i, p) in f.periods().iter().enumerate() {
                if p.end() != self.y.period_ends[i] {
                    return Err(invalid(format!(
                        "pennsylvaniataxcalc: REV-1630 Exception 2 period {} ends on {}, not {}",
                        i + 1,
                        self.y.period_ends[i],
                        p.end()
                    )));
                }
            }
        }
        Ok(())
    }

    /// Where the department's forms and the Tax Reform Code part ways on a
    /// rule every Pennsylvania computation uses. They are found in
    /// research/estimated-payments/pa.md, conflicts A to D and F, and checked
    /// there against both. Every figure follows the form.
    fn divergences(&self, t: Touches) -> Result<Vec<Divergence>, Error> {
        Ok(vec![
            working::new_divergence("When the rules start")
                .form("The rules apply at $14,000 or more of income not subject to withholding. They also need $430 or more of tax after withholding and credits.",
                    "REV-413 (I) (2026), Who Must Make Estimated Tax Payments, Penalty for Failure to Pay Estimated Taxes and Line 17")
                .statute("Payments are required only if that income is expected to exceed $14,000. Exactly $14,000 doesn't start the rules.",
                    "72 P.S. § 7325(a)(1)")
                .uses(Reading::Form)
                .affects(t.start)
                .build()?,
            working::new_divergence("A requirement first met after April 1")
                .form("Every installment is a quarter of the year's amount. Earlier ones are excused only under the special exception. It needs no estimates due the year before. The whole catch-up must be paid in the first period. Groups start April 1, June 1 and September 1.",
                    "REV-1630 (2025), Line 5 instructions and Determining the Underpayment Amount; REV-413 (I) (2026), Payment Due Dates Table")
                .statute("Nothing is due before the requirement arises. Every late starter pays in equal parts on the dates left. That's thirds from June 15 and halves from September 15. The groups start the day after April 1, June 1 and September 1.",
                    "72 P.S. §§ 7325(d), 7326(a); 61 Pa. Code § 115.9(a)")
                .uses(Reading::Form)
                .affects(t.late_start)
                .build()?,
            working::new_divergence("A return filed and paid by January 31")
                .form("Any final return filed and paid by January 31 counts as the fourth installment.",
                    "REV-1630 (2025), Line 6 instructions")
                .statute("The return replaces the January 15 payment only if that was your first payment. That's when the requirement arose after September 1.",
                    "72 P.S. § 7325(h); 61 Pa. Code § 115.9(a)(4)")
                .uses(Reading::Form)
                .affects(t.january)
                .build()?,
            working::new_divergence("The prior-year test after a part-year return")
                .form("Last year's income test is open only if you were a full-year resident that year.",
                    "REV-413 (I) (2026), Safe Harbor Rules and Line 10; REV-1630 (2025), Section II")
                .statute("The test uses last year's return, with no condition of full-year residence.",
                    "72 P.S. § 7352(d)(2)(A)")
                .uses(Reading::Form)
                .affects(t.part_year)
                .build()?,
            working::new_divergence("The daily interest factor")
                .form("Each day is charged at a printed factor of 0.000192. That's 7% divided by 365, rounded to six places.",
                    "REV-1630 (2025), Lines 15a to 15c; REV-1611 (11-25)")
                .statute("Interest is at the annual rate set for the year, 7% for 2026. That's 0.00019178 a day.",
                    "72 P.S. § 7352(d); 72 P.S. § 806; Interest Rate Notice, 55 Pa.B. 8829")
                .uses(Reading::Form)
                .affects(t.interest)
                .build()?,
        ])
    }

    fn assess(&self, f: &Facts) -> Result<Assessment, Error> {
        self.check(f)?;
        let extras = self.extras(f)?;
        let y = &self.y;
        let mut a = Assessment {
            dues: estimated::move_dates(&y.calendar, &y.dues),
            ..Assessment::default()
        };
        let mut target_qs = TargetQuestions::default();
        let last = y.year - 1;
        let tax_opt = f.this_year().tax();
        let tax_known = tax_opt.is_some();
        let tax = tax_opt.unwrap_or_default();
        a.tax_known = tax_known;
        let withheld = f.withheld_total();
        let credits_opt = extras.credits();
        let credits = credits_opt.unwrap_or_default();
        a.credits = credits;
        let prior = f.prior_year();
        let prior_figures = prior.and_then(|p| p.figures()).unwrap_or_default();
        let prior_tf_opt = extras.prior_year_forgiveness();
        let prior_tf = prior_tf_opt.unwrap_or_default();

        // The 90% test, REV-1630 lines 1a to 4, written as what withholding
        // and payments must reach: 90% of the tax before credits, less the
        // credits.
        let before = tax + credits;
        let this_amount = (percent(90).of(before) - credits).max(USCents(0));
        let this_year = if tax_known {
            estimated::new_target_option(Basis::ThisYearTax, percent(90), this_amount).build()?
        } else {
            estimated::new_target_option(Basis::ThisYearTax, percent(90), USCents(0))
                .unavailable(estimated::NEEDS_THIS_YEAR_TAX)
                .build()?
        };
        if tax_known {
            a.notes.rule(
                "72 P.S. § 7352(d)(1); REV-1630 (2025), Section I, lines 1a to 4",
                format!(
                    "90% of the {} tax before credits is {}; less {} of credits, withholding and payments must reach {}",
                    before,
                    percent(90).of(before),
                    credits,
                    this_amount
                ),
            );
        }

        // Exception 1: last year's taxable income at this year's rate, less
        // last year's Tax Forgiveness, for a full-year resident who filed.
        let residence = prior.and_then(|p| p.full_year_resident());
        let (resident, residence_answered) = (residence == Some(true), residence.is_some());
        let prior_income_opt = prior_figures.taxable_income();
        let unavailable = |why: String| -> TargetOptionBuilder {
            let mut b =
                estimated::new_target_option(Basis::PriorYearIncome, percent(100), USCents(0));
            b.unavailable(why);
            b
        };
        let prior_option = match prior {
            None => {
                target_qs.ask(
                    PositionKind::PriorYearReturn,
                    format!("What was the taxable income on your {last} Pennsylvania return?"),
                    format!(
                        "With it, last year's income at the {} rate could set your target instead.",
                        y.year
                    ),
                );
                unavailable(format!("Needs your {last} Pennsylvania return"))
            }
            Some(p) if !p.filed() => {
                unavailable(format!("You didn't file a {last} Pennsylvania return"))
            }
            Some(p) if p.months() != 12 => unavailable(format!(
                "Your {last} Pennsylvania return covered {} months; this test needs a full year",
                p.months()
            )),
            Some(_) if !residence_answered => {
                target_qs.ask(
                    PositionKind::Residency,
                    format!("Were you a full-year Pennsylvania resident in {last}?"),
                    "If you were, last year's income at this year's rate could set your target."
                        .into(),
                );
                unavailable(format!(
                    "Needs whether you were a full-year Pennsylvania resident in {last}"
                ))
            }
            Some(_) if !resident => unavailable(format!(
                "You weren't a full-year Pennsylvania resident in {last}"
            )),
            Some(_) => match prior_income_opt {
                None => {
                    target_qs.ask(
                        PositionKind::PriorYearReturn,
                        format!(
                            "What was your {last} Pennsylvania taxable income, line 11 of your PA-40?"
                        ),
                        "With it, last year's income at this year's rate could set your target."
                            .into(),
                    );
                    unavailable(format!("Needs your {last} Pennsylvania taxable income"))
                }
                Some(prior_income) => {
                    let at_rate = y.rate.of(prior_income);
                    let ex1 = (at_rate - prior_tf).max(USCents(0));
                    let amount = (ex1 - credits).max(USCents(0));
                    a.notes.rule(
                        "72 P.S. § 7352(d)(2)(A); REV-1630 (2025), Section II, line 12; REV-413 (I) (2026), lines 10 to 14",
                        format!(
                            "the {last} taxable income of {prior_income} at the {} rate of {} is {at_rate}; less {prior_tf} of {last} Tax Forgiveness, {ex1}; less this year's {credits} of credits, withholding and payments must reach {amount}",
                            y.year, y.rate
                        ),
                    );
                    if prior_tf_opt.is_none() {
                        target_qs.ask(
                            PositionKind::JurisdictionFact,
                            format!(
                                "What was your {last} Tax Forgiveness Credit, line 21 of your PA-40?"
                            ),
                            "Until then, nothing comes off the test on last year's income. That target can only be too high.".into(),
                        );
                    }
                    estimated::new_target_option(Basis::PriorYearIncome, percent(100), amount)
                }
            },
        };
        let prior_opt = prior_option.build()?;

        let mut rb = estimated::new_requirement();
        rb.option(this_year.clone()).option(prior_opt.clone());
        let (mut chosen, mut target) = (Basis::ThisYearTax, this_year.amount());
        if prior_opt.available() && (!tax_known || prior_opt.amount() < target) {
            (chosen, target) = (Basis::PriorYearIncome, prior_opt.amount());
        }
        if !tax_known {
            let effect = if prior_opt.available() {
                "Without it, the target can only come from last year's income."
            } else {
                "Without it, there's no target."
            };
            target_qs.ask(
                PositionKind::ThisYearTax,
                format!("What do you expect your {} Pennsylvania tax to be?", y.year),
                effect.into(),
            );
        }

        let income_opt = extras.income_without_withholding();
        let income = income_opt.unwrap_or_default();
        let forgiveness = extras.prior_year_full_forgiveness();
        if forgiveness == Some(true) {
            a.exempt = true;
            let forgiven = estimated::new_exception("Full Tax Forgiveness last year").build()?;
            rb.required(
                false,
                format!(
                    "You qualified for 100% Tax Forgiveness for {last}. Pennsylvania charges no estimated tax penalty for {}. No estimated payments are needed.",
                    y.year
                ),
            )
            .exempt(forgiven);
            a.notes.rule(
                "Act 85 of 2012; REV-1630 (2025), Before You Begin",
                format!(
                    "100% Tax Forgiveness for {last} takes {} out of the estimated tax penalty",
                    y.year
                ),
            );
        } else if income_opt.is_some() && income < y.income_threshold {
            a.exempt = true;
            let under = estimated::exception_below(
                "Income without withholding",
                income,
                y.income_threshold,
            )?;
            rb.required(
                false,
                format!(
                    "Your income without Pennsylvania withholding is {}. The estimated tax rules don't apply for {}.",
                    left_under(income, y.income_threshold),
                    y.year
                ),
            )
            .exempt(under);
            a.notes.rule(
                "72 P.S. § 7325(a); REV-413 (I) (2026), Penalty for Failure to Pay Estimated Taxes",
                format!(
                    "{} of income not subject to withholding is under {}, so no penalty applies",
                    income, y.income_threshold
                ),
            );
        } else if !tax_known && !prior_opt.available() {
            rb.required(
                false,
                format!(
                    "Your target is 90% of your {} Pennsylvania tax. Where that test is open, it may be your {last} taxable income at the {} rate. Without one of those figures, there's no target to pay toward yet.",
                    y.year, y.year
                ),
            )
            .no_target();
        } else if !tax_known && withheld >= target {
            rb.required(
                false,
                format!(
                    "Your {last} income at the {} rate is {}. Your Pennsylvania withholding of {} already reaches it. No estimated payments are needed, whatever your {} tax is.",
                    y.year,
                    target.whole(),
                    withheld.whole(),
                    y.year
                ),
            )
            .choose(chosen);
        } else if !tax_known {
            rb.required(
                true,
                format!(
                    "Your target is your {last} income at the {} rate, {}. After {} of withholding, that's {} of estimated payments. Pay a quarter by each date. That keeps you clear of a penalty whatever your {} tax turns out to be.",
                    y.year,
                    target.whole(),
                    withheld.whole(),
                    (target - withheld).whole(),
                    y.year
                ),
            )
            .choose(chosen);
        } else if tax - withheld < y.tax_threshold {
            a.exempt = true;
            let under = estimated::exception_below(
                "Tax less withholding and credits",
                tax - withheld,
                y.tax_threshold,
            )?;
            let why = if withheld.0 == 0 {
                format!(
                    "You have no Pennsylvania withholding, and your tax after credits is {}. That's under {}, so no estimated payments are needed.",
                    tax.whole(),
                    y.tax_threshold.whole()
                )
            } else if withheld > tax {
                format!(
                    "Your Pennsylvania tax after credits is {}, and {} was withheld. That {}, so no estimated payments are needed.",
                    tax.whole(),
                    withheld.whole(),
                    estimated::leaves(tax - withheld)
                )
            } else if withheld == tax {
                format!(
                    "Your Pennsylvania withholding covers your tax after credits of {}. No estimated payments are needed.",
                    tax.whole()
                )
            } else {
                format!(
                    "Your Pennsylvania tax after credits is {}, and {} was withheld. That leaves {}, so no estimated payments are needed.",
                    tax.whole(),
                    withheld.whole(),
                    left_under(tax - withheld, y.tax_threshold)
                )
            };
            rb.required(false, why).exempt(under);
            a.notes.rule(
                "REV-1630 (2025), Section I, line 3; REV-413 (I) (2026), Line 17",
                format!(
                    "{} of tax less {} of withholding and {} of credits is {}, under {}, so there is no penalty",
                    before,
                    withheld,
                    credits,
                    tax - withheld,
                    y.tax_threshold
                ),
            );
        } else if withheld >= target {
            rb.required(
                false,
                format!(
                    "Your Pennsylvania withholding of {} already reaches your {} target. No estimated payments are needed.",
                    withheld.whole(),
                    target.whole()
                ),
            )
            .choose(chosen);
        } else if withheld.0 == 0 {
            rb.required(
                true,
                format!(
                    "You have no Pennsylvania withholding, and your tax after credits is {}. That's {} or more, and your target is {}. You need estimated payments of {}, a quarter by each date.",
                    tax.whole(),
                    y.tax_threshold.whole(),
                    target.whole(),
                    target.whole()
                ),
            )
            .choose(chosen);
        } else {
            rb.required(
                true,
                format!(
                    "Your Pennsylvania tax after credits is {}, and {} was withheld. That leaves {}, which is {} or more, and your target is {}. You need {} of estimated payments, a quarter by each date.",
                    tax.whole(),
                    withheld.whole(),
                    (tax - withheld).whole(),
                    y.tax_threshold.whole(),
                    target.whole(),
                    (target - withheld).whole()
                ),
            )
            .choose(chosen);
        }
        a.requirement = rb.build()?;
        target_qs.ask_unless_exempt(&mut a.notes, a.exempt);
        if !a.exempt {
            if credits_opt.is_none() {
                a.notes.ask(
                    PositionKind::JurisdictionFact,
                    format!(
                        "What are your {} Tax Forgiveness, resident and other credits?",
                        y.year
                    ),
                    "They're PA-40 lines 21 to 23, and until then none are counted. They can only lower what you need to pay.",
                );
            }
            if income_opt.is_none() {
                a.notes.ask(
                    PositionKind::JurisdictionFact,
                    format!(
                        "How much of your {} income will have no Pennsylvania tax withheld?",
                        y.year
                    ),
                    format!(
                        "If it's under {}, the estimated tax rules don't apply to you.",
                        y.income_threshold.whole()
                    ),
                );
            }
            if forgiveness.is_none() {
                a.notes.ask(
                    PositionKind::JurisdictionFact,
                    format!("Did you qualify for 100% Tax Forgiveness for {last}?"),
                    format!(
                        "If you did, there's no estimated tax penalty for {}.",
                        y.year
                    ),
                );
            }
        }

        // What the divergences turn on, apart from the interest, which needs
        // the installments below.
        let first_met = f.requirement_first_met();
        let filed = f.return_filed();
        let mut t = Touches {
            start: !a.exempt && income_opt.is_some() && income == y.income_threshold,
            late_start: !a.exempt && !first_met.is_zero() && !first_met.before(y.later_groups[0]),
            january: !a.exempt
                && !filed.is_zero()
                && !filed.after(y.file_and_pay_by)
                && (first_met.is_zero() || !first_met.after(y.later_groups[2])),
            part_year: !a.exempt
                && prior.is_some_and(|p| p.filed())
                && residence_answered
                && !resident,
            interest: false,
        };

        if a.exempt {
            a.regular = vec![USCents(0); a.dues.len()];
            a.lesser = vec![USCents(0); a.dues.len()];
            self.diverge(&mut a, t)?;
            return Ok(a);
        }

        // The installments. The penalty is always measured against 90%; the
        // smaller test only excuses. Without this year's tax the 90% is not
        // known, and the installments are measured against the target
        // instead.
        let regular_of = if tax_known { this_amount } else { target };
        let cum = estimated::cumulative_shares(regular_of, &quarters())?;
        a.regular = estimated::increments(&cum);
        a.lesser = estimated::cumulative_shares(target, &quarters())?;
        if prior_opt.available() {
            a.exception1 = Some(estimated::cumulative_shares(
                prior_opt.amount(),
                &quarters(),
            )?);
        }
        a.notes.rule(
            "72 P.S. § 7326(a)(1); REV-1630 (2025), Section I, line 5; REV-413 (I) (2026), Payment Due Dates Table",
            format!(
                "a quarter of {}, {}, is due on each of {}, {}, {} and {}",
                regular_of,
                a.regular[0],
                a.dues[0].long(),
                a.dues[1].long(),
                a.dues[2].long(),
                a.dues[3].long()
            ),
        );
        if !tax_known {
            a.notes.rule(
                "72 P.S. § 7352(d)(2)(A); REV-1630 (2025), Section II, Exception 1",
                format!(
                    "this year's tax was not given, so each installment is measured against a quarter of the {last} income at the {} rate, {}; \
                     REV-1630 charges an installment that misses it on its shortfall against 90% of this year's tax",
                    y.year, a.regular[0]
                ),
            );
        }
        if chosen == Basis::PriorYearIncome && tax_known {
            a.notes.rule(
                "72 P.S. § 7352(d)(2)(A); REV-1630 (2025), Section II, Exception 1",
                format!(
                    "an installment is excused when withholding and payments by its date reach a quarter of {} for each date so far; \
                     if they do not, the penalty is on the shortfall against {} a quarter",
                    prior_opt.amount(),
                    a.regular[0]
                ),
            );
        }

        // Exception 2: 90% of the tax on the income actually received to
        // each period end, with no annualising.
        if f.elections().annualised() {
            let mut ex2 = vec![USCents(0); a.dues.len()];
            let ccum = estimated::cumulative_shares(credits, &quarters())?;
            for (i, p) in f.periods().iter().enumerate() {
                ex2[i] = (percent(90).of(y.rate.of(p.income())) - ccum[i]).max(USCents(0));
            }
            a.notes.rule(
                "72 P.S. § 7352(d)(2)(B); REV-1630 (2025), Section II, Exception 2",
                format!(
                    "an installment is also excused when withholding and payments by its date reach 90% of {} of the income received to the period's end, less a quarter of the credits for each date so far: {}, {}, {} and {}",
                    y.rate, ex2[0], ex2[1], ex2[2], ex2[3]
                ),
            );
            a.exception2 = Some(ex2);
        }

        // A late starter: REV-1630's special exception.
        (a.first, a.special) = self.late_start(
            f,
            &mut a,
            &prior_figures,
            prior,
            prior_tf,
            prior_tf_opt.is_some(),
        );

        // January 31: REV-1630 counts the payment with a final return filed
        // by then as the fourth installment. January 31, 2027 is a Sunday;
        // the form prints January 31 and says nothing of moving it, so it is
        // not moved.
        if !filed.is_zero() && !filed.after(y.file_and_pay_by) {
            a.excused_january = true;
            a.notes.rule(
                "REV-1630 (2025), Line 6 instructions; 72 P.S. § 7325(h)",
                format!(
                    "the return was filed and paid on {}, by {}, so the payment with it is the fourth installment",
                    filed.long(),
                    y.file_and_pay_by.long()
                ),
            );
        }

        // The daily factor matters only to a shortfall that bears interest.
        let (ledger, _) = self.ledger(f, &a)?;
        for (i, s) in self.standings(&a, &ledger).iter().enumerate() {
            if ledger[i].shortfall().0 > 0 && !s.excused {
                t.interest = true;
            }
        }
        self.diverge(&mut a, t)?;
        Ok(a)
    }

    /// Adds the divergences to the assessment's notes.
    fn diverge(&self, a: &mut Assessment, t: Touches) -> Result<(), Error> {
        let ds = self.divergences(t)?;
        a.notes.diverges(&ds);
        Ok(())
    }

    /// The installment the requirement first applies to, and whether the
    /// filer owed no estimated payments last year, which REV-1630's special
    /// exception needs before it excuses the earlier installments.
    fn late_start(
        &self,
        f: &Facts,
        a: &mut Assessment,
        pf: &Figures,
        prior: Option<PriorReturn>,
        prior_tf: USCents,
        prior_tf_known: bool,
    ) -> (usize, bool) {
        let y = &self.y;
        let last = y.year - 1;
        let (mut special, mut known) = (false, false);
        match prior {
            None => {}
            Some(p) if p.not_required_to_file() => {
                (special, known) = (true, true);
            }
            Some(p) if !p.filed() => {
                known = true;
            }
            Some(_) => {
                let prior_tax = pf.tax().unwrap_or_default();
                match pf.withheld() {
                    // Line A can only be larger with the forgiveness credit
                    // added.
                    Some(w) if prior_tax - w >= y.tax_threshold => {
                        known = true;
                    }
                    Some(w) if prior_tf_known => {
                        (special, known) = (prior_tax + prior_tf - w < y.tax_threshold, true);
                    }
                    _ => {}
                }
            }
        }
        let first_met = f.requirement_first_met();
        if first_met.is_zero() {
            if special || !known {
                a.notes.ask(
                    PositionKind::RequirementFirstMet,
                    format!(
                        "When did you first expect to need estimated payments for {}?",
                        y.year
                    ),
                    format!(
                        "The quarters before that date can be excused. That needs no estimated payments due from you for {last}."
                    ),
                );
            }
            return (0, special);
        }
        if !known {
            a.notes.ask(
                PositionKind::PriorYearReturn,
                format!(
                    "What were your {last} Pennsylvania tax, withholding and Tax Forgiveness Credit?"
                ),
                "They decide whether the quarters before you first needed to pay can be excused.",
            );
        }
        let mut first = 0;
        for (i, g) in y.later_groups.iter().enumerate() {
            if !first_met.before(*g) {
                first = i + 1;
            }
        }
        (first, special)
    }

    /// Applies the credits to the regular installments.
    fn ledger(&self, f: &Facts, a: &Assessment) -> Result<(Vec<LedgerEntry>, Notes), Error> {
        let credits = estimated::credits(f, &a.dues, &quarters())?;
        let mut required = a.regular.clone();
        if a.excused_january {
            required[3] = USCents(0);
        }
        let ledger = estimated::apply(&a.dues, &required, &credits)?;
        let mut notes = Notes::default();
        notes.rule(
            "REV-1630 (2025), Section I, lines 2 to 10 and the instructions for lines 14a to 14c",
            format!(
                "the {} withheld is treated as paid in four equal parts on the due dates, whenever it was actually withheld, \
                 and a payment after a due date first reduces the oldest underpayment, on the day it is paid",
                f.withheld_total()
            ),
        );
        if f.elections().withholding_as_withheld() {
            notes.rule(
                "REV-1630 (2025), Section I, line 5",
                "Pennsylvania has no election to credit withholding on the dates it was withheld, so it is spread evenly",
            );
        }
        Ok((ledger, notes))
    }

    fn standings(&self, a: &Assessment, ledger: &[LedgerEntry]) -> Vec<Standing> {
        // The special exception holds when the filer paid, by the first date
        // the requirement applied, everything due by then.
        let mut special_holds = false;
        if a.special && a.first > 0 {
            let mut need = a.lesser[a.first];
            if let Some(ex2) = &a.exception2 {
                need = need.min(ex2[a.first]);
            }
            special_holds = ledger[a.first].credited_by() >= need;
        }
        let mut out = Vec::with_capacity(ledger.len());
        for (i, l) in ledger.iter().enumerate() {
            let mut s = Standing {
                needed_by: a.lesser[i],
                ..Standing::default()
            };
            if let Some(ex2) = &a.exception2 {
                s.needed_by = s.needed_by.min(ex2[i]);
            }
            if special_holds && i < a.first {
                s.needed_by = USCents(0);
            }
            if a.excused_january && i == 3 {
                (s.excused, s.applied) = (true, APPLIED_JANUARY);
            } else if l.shortfall().0 == 0 {
            } else if special_holds && i < a.first {
                (s.excused, s.applied) = (true, APPLIED_SPECIAL);
            } else if a
                .exception1
                .as_ref()
                .is_some_and(|e| l.credited_by() >= e[i])
            {
                (s.excused, s.applied) = (true, APPLIED_EXCEPTION1);
            } else if a
                .exception2
                .as_ref()
                .is_some_and(|e| l.credited_by() >= e[i])
            {
                (s.excused, s.applied) = (true, APPLIED_EXCEPTION2);
            }
            out.push(s);
        }
        out
    }

    /// The installments from the ledger. An installment credited with what
    /// it needs is met and shows no shortfall: the shortfall against 90%
    /// that REV-1630 computes is excused by an exception and bears no
    /// interest. One short of what it needs keeps the form's shortfall.
    fn schedule_of(
        &self,
        a: &Assessment,
        ledger: &[LedgerEntry],
    ) -> Result<(Vec<Installment>, Notes), Error> {
        let mut notes = Notes::default();
        let mut out = Vec::with_capacity(ledger.len());
        for (i, s) in self.standings(a, ledger).iter().enumerate() {
            let l = &ledger[i];
            let mut shortfall = l.shortfall();
            if l.credited_by() >= s.needed_by {
                shortfall = USCents(0);
            }
            let mut b = estimated::new_installment(i as i64 + 1, l.due());
            b.needed_by(s.needed_by)
                .credited_by(l.credited_by())
                .shortfall(shortfall);
            if s.excused {
                b.excused();
            }
            out.push(b.build()?);
            if !s.applied.is_empty() && l.shortfall().0 > 0 {
                notes.rule(
                    "72 P.S. § 7352(d)(2); REV-1630 (2025), Section II",
                    format!(
                        "the {} installment was {} short and bears no interest under {}",
                        l.due().long(),
                        l.shortfall(),
                        s.applied
                    ),
                );
            }
        }
        Ok((out, notes))
    }
}

impl EstimatedTax for PennsylvaniaEstimated {
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
                    "with no {} Pennsylvania return to measure against, there is no target to schedule",
                    self.y.year - 1
                ),
            ));
        }
        let (ledger, notes) = self.ledger(f, &a)?;
        let (out, excusals) = self.schedule_of(&a, &ledger)?;
        Ok(working::new_result(out)
            .with(&a.notes)
            .with(&notes)
            .with(&excusals)
            .build()?)
    }

    fn penalty(&self, f: &Facts, as_of: Date) -> Result<working::Result<Penalty>, Error> {
        if as_of.is_zero() {
            return Err(invalid(
                "pennsylvaniataxcalc: a penalty needs the date to figure it to".into(),
            ));
        }
        let a = self.assess(f)?;
        if !a.requirement.target_known() {
            return Err(Error::with_detail(
                Error::NeedsThisYearTax,
                format!(
                    "with no {} Pennsylvania return to measure against, there is no target to figure a penalty from",
                    self.y.year - 1
                ),
            ));
        }
        let y = &self.y;
        let mut stop = as_of;
        if y.stop.before(stop) {
            stop = y.stop;
        }
        let filed = f.return_filed();
        if !filed.is_zero() && filed.before(stop) {
            stop = filed;
        }
        let waiver = f.positions().waiver().unwrap_or_default();
        let (ledger, mut notes) = self.ledger(f, &a)?;
        let (sched, _) = self.schedule_of(&a, &ledger)?;
        if a.exempt {
            let p = estimated::new_penalty(stop)
                .waiver(waiver)
                .because(a.requirement.clone(), &sched)
                .build()?;
            return Ok(working::new_result(p).with(&a.notes).build()?);
        }

        let standings = self.standings(&a, &ledger);
        if !a.tax_known {
            // REV-1630 charges an installment that misses both exceptions on
            // its shortfall against 90% of this year's tax. Without that tax
            // the penalty has no bound, unless no installment missed them.
            for (i, s) in standings.iter().enumerate() {
                if ledger[i].shortfall().0 > 0 && !s.excused {
                    return Err(Error::with_detail(
                        Error::NeedsThisYearTax,
                        format!(
                            "the {} installment did not reach the {} income at the {} rate, and REV-1630 charges it on its shortfall against 90% of this year's tax",
                            ledger[i].due().long(),
                            y.year - 1,
                            y.year
                        ),
                    ));
                }
            }
            notes.rule(
                "72 P.S. § 7352(d)(2)(A); REV-1630 (2025), Section II, Exception 1",
                format!(
                    "this year's tax was not given, but every installment reached the {} income at the {} rate by its date, so Exception 1 excuses it whatever this year's tax is",
                    y.year - 1,
                    y.year
                ),
            );
        }
        let mut pb = estimated::new_penalty(stop);
        pb.waiver(waiver).because(a.requirement.clone(), &sched);
        let mut unrated = Date::default();
        for (i, s) in standings.iter().enumerate() {
            let l = &ledger[i];
            if l.shortfall().0 == 0 || s.excused {
                continue;
            }
            let mut lb = estimated::new_penalty_line(l.number(), l.due(), l.shortfall());
            let mut last_day = l.due();
            for piece in l.pieces() {
                let mut to = stop;
                if let Some(on) = piece.paid_on()
                    && on.before(stop)
                {
                    to = on;
                }
                if !to.after(l.due()) {
                    continue;
                }
                if to.after(last_day) {
                    last_day = to;
                }
                let (parts, gap) = estimated::daily_factor(piece.amount(), l.due(), to, &y.daily)?;
                if !gap.is_zero() && (unrated.is_zero() || gap.before(unrated)) {
                    unrated = gap;
                }
                for p in parts {
                    lb.part(p);
                }
            }
            pb.line(lb.days(last_day.days_since(l.due())).build()?);
        }
        let mut p = pb.build()?;
        notes.rule(
            "72 P.S. § 7352(d)(1); REV-1630 (2025), Section III",
            format!(
                "each shortfall no exception covers is charged the daily factor for its calendar year, in whole dollars, from its due date until paid or {}; \
                 the penalty to {} is {}",
                y.stop.long(),
                stop.long(),
                p.total()
            ),
        );
        if !unrated.is_zero() {
            p = p.to_builder().rated_through(unrated.add_days(-1)).build()?;
            notes.ask(
                PositionKind::UnpublishedRate,
                "",
                format!(
                    "The rate for the days after {} isn't published yet.",
                    unrated.add_days(-1).long()
                ),
            );
        }
        if let Some(k) = f.positions().waiver()
            && k != WaiverKind::None
        {
            notes.rule(
                "Tax Reform Code § 2706; PA Personal Income Tax Guide (DSM-12)",
                "Pennsylvania's estimated tax rules have no casualty, retirement or disability waiver; a request to abate the penalty goes to the Board of Appeals",
            );
            if p.total().0 > 0 {
                let o = estimated::waiver_refused(
                    "Pennsylvania's estimated tax rules have no waiver for a casualty or disaster. They have none for retirement or disability; ask the Board of Appeals to abate.",
                );
                p = p.to_builder().waiver_outcome(o).build()?;
            }
        }
        Ok(working::new_result(p).with(&a.notes).with(&notes).build()?)
    }

    fn explanation(&self) -> Explanation {
        pennsylvania_explanation()
    }
}

/// Written by hand and checked against research/estimated-payments/pa.md.
fn pennsylvania_explanation() -> Explanation {
    estimated::new_explanation(
        "Pennsylvania expects estimated payments for 2026 if two things are true. You have $14,000 or more of income with no Pennsylvania tax withheld. You also have $430 or more due after withholding and credits. The penalty is interest on each quarter's shortfall.")
        .paragraph("Withholding, credits and estimated payments must reach a target. That avoids the penalty. The target is the smaller of two figures. One is 90% of this year's tax. The other is last year's Pennsylvania taxable income at this year's 3.07% rate. Last year's Tax Forgiveness Credit comes off that one. Pennsylvania does not use last year's tax itself, and there is no 110% tier.")
        .paragraph("The payments are a quarter each. They're due April 15, June 15, September 15 and the next January 15. Withholding and credits count evenly across the four dates, whenever they came. A final return filed and paid by January 31 counts as the January payment.")
        .paragraph("The penalty is figured against 90% of this year's tax in every quarter. The prior-year figure does not lower that amount. It excuses a quarter outright if payments by its date reach that figure so far. So does paying 90% of the tax on income actually received by each period's end. That helps when income arrives late in the year. The interest is charged by the day, at a rate set once each calendar year. It stops on April 15 of the next year.")
        .paragraph("You may only learn partway through the year that you must pay. The department still expects a quarter for each date. It charges interest on the earlier ones until you catch up. If none were due from you the year before, it excuses the earlier quarters. You must pay the whole catch-up by the first date that applies to you. There's no penalty in the year after one with 100% Tax Forgiveness.")
        .versus_federal("Pennsylvania's rules start from income without withholding, not $1,000 of tax. The prior-year test is last year's income at this year's rate. There's no 110% tier. It and the income-to-date test excuse a quarter rather than lowering it. The income-to-date test uses actual income without annualising. The rate is set for each calendar year. There's no casualty, retirement or disability waiver.")
        .source("Tax Reform Code of 1971 §§ 325, 326, 352(d) (72 P.S. §§ 7325, 7326, 7352(d)); Fiscal Code § 806")
        .source("REV-413 (I), Instructions for Estimating PA Personal Income Tax, 2026")
        .source("REV-1630, Underpayment of Estimated Tax by Individuals, and its instructions, 2025")
        .source("REV-1611, Interest Rate and Calculation Method for Title 72 Taxes (11-25)")
        .build()
        .unwrap_or_default()
}
