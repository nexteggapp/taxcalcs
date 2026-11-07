//! New York's estimated tax rules, Tax Law § 685(c) and (d), with the rate
//! under § 697(j), as the shared [`EstimatedTax`] interface.
//!
//! One computation covers four taxes: New York State personal income tax,
//! the New York City resident tax, the Yonkers tax and the MCTMT on
//! self-employment earnings. They are paid on one voucher, Form IT-2105, and
//! the penalty for all of them is figured together on Form IT-2105.9. A
//! caller passes their total as [`estimated::Figures::tax`], with the City
//! and Yonkers part as [`estimated::Figures::local_tax`], and says which
//! taxes apply in [`EstimatedFacts`].
//!
//! New York follows the federal rules: four 25% installments, the lesser of
//! 90% of this year's tax or 100% of last year's, 110% above $150,000 of
//! last year's New York AGI, and payments applied to the earliest
//! installment first. It differs in the $300 threshold for each tax, and its
//! own rate: the federal short-term rate plus 5½ points, not below 7½%,
//! simple, set each quarter.
//!
//! The figure follows Form IT-2105.9 for 2025 and its instructions, the
//! latest published, with the 2026 dates and rates, and the result's
//! Divergences say where the statute reads otherwise. No change to
//! § 685(c) or (d) was found for 2026 (research/estimated-payments/ny.md),
//! so the form does not predate the law it describes.
//!
//! The rate for the third month after the tax year also covers April 1 to
//! 15. The rate from January 1, 2027 is not published, so those days are not
//! charged and the penalty reports the gap.
//!
//! Two rules the form states and the code follows have no source here
//! beyond it: the $300 test multiplied by the number of taxes, since the New
//! York City and Yonkers laws were not read; and 110% when last year's MCTD
//! self-employment earnings were over $150,000, which § 685(c)(3)(C) does
//! not state and which comes from the MCTMT article, not read.
//!
//! Not built: the exception for an installment due on or after the
//! taxpayer's death, § 685(d)(3), as [`Facts`] carries no date of death; the
//! 90-day extension of the first installment after a spouse's death; and
//! estates and trusts.

use std::any::Any;

use estimated::{
    Basis, Calendar, Date, Error, EstimatedTax, Explanation, Facts, Installment, Jurisdiction,
    LedgerEntry, Penalty, RateSeries, Requirement, TargetOptionBuilder, WaiverKind,
};
use filing::Status;
use money::{Rate, USCents};
use working::{Divergence, Notes, PositionKind, Reading};

use crate::estimated_y2026::year_2026;

/// jurisdiction is New York's postal code.
const JURISDICTION: Jurisdiction = estimated::NEW_YORK;

/// The facts New York's rules read that no other jurisdiction does. Carry
/// them in [`Facts::state`].
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct EstimatedFacts {
    nyc: bool,
    yonkers: bool,
    mctmt: bool,
    subject_answered: bool,

    mctd_earnings: Option<USCents>,

    source_income: bool,
    source_answered: bool,
    farmer_last: bool,
    farmer_answered: bool,
}

impl EstimatedFacts {
    /// Which taxes besides the state's the filer owes this year, as New York
    /// City, Yonkers and the MCTMT, or `None` when the caller has not said.
    /// Each of New York State, New York City and Yonkers adds $300 to the
    /// threshold; the MCTMT adds nothing.
    pub fn subject_to(&self) -> Option<(bool, bool, bool)> {
        self.subject_answered
            .then_some((self.nyc, self.yonkers, self.mctmt))
    }

    /// Last year's net earnings from self-employment allocated to the
    /// Metropolitan Commuter Transportation District, which raises the
    /// prior-year test to 110% above $150,000.
    pub fn prior_year_mctd_earnings(&self) -> Option<USCents> {
        self.mctd_earnings
    }

    /// Whether a filer who was not a New York resident all of last year had
    /// New York source income throughout it, which the no-liability
    /// exception needs, or `None` when the caller has not said.
    pub fn prior_year_new_york_source_income(&self) -> Option<bool> {
        self.source_answered.then_some(self.source_income)
    }

    /// Whether at least two-thirds of last year's federal gross income was
    /// from farming or fishing, which puts this year under the farmers'
    /// rules, or `None` when the caller has not said.
    pub fn farmer_last_year(&self) -> Option<bool> {
        self.farmer_answered.then_some(self.farmer_last)
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
    /// Says which of the New York City tax, the Yonkers tax and the MCTMT
    /// the filer owes this year.
    pub fn subject_to(&mut self, new_york_city: bool, yonkers: bool, mctmt: bool) -> &mut Self {
        self.e.nyc = new_york_city;
        self.e.yonkers = yonkers;
        self.e.mctmt = mctmt;
        self.e.subject_answered = true;
        self
    }

    /// Sets last year's MCTD self-employment earnings.
    pub fn prior_year_mctd_earnings(&mut self, a: USCents) -> &mut Self {
        self.e.mctd_earnings = Some(a);
        self
    }

    /// Says whether the filer had New York source income all last year.
    pub fn prior_year_new_york_source_income(&mut self, had: bool) -> &mut Self {
        self.e.source_income = had;
        self.e.source_answered = true;
        self
    }

    /// Says whether two-thirds of last year's federal gross income was from
    /// farming or fishing.
    pub fn farmer_last_year(&mut self, farmer: bool) -> &mut Self {
        self.e.farmer_last = farmer;
        self.e.farmer_answered = true;
        self
    }

    /// Refuses negative earnings.
    pub fn build(&self) -> Result<EstimatedFacts, Error> {
        if self.e.mctd_earnings.is_some_and(|v| v.0 < 0) {
            return Err(Error::Invalid(
                "newyorktaxcalc: MCTD earnings cannot be negative".into(),
            ));
        }
        Ok(self.e)
    }
}

/// One tax year's published figures.
#[derive(Clone, Debug)]
pub(crate) struct EstimatedYear {
    pub(crate) year: i64,

    /// § 685(d)(1) and IT-2105.9 line 15: $300 for each of the state, City
    /// and Yonkers taxes that apply.
    pub(crate) threshold: USCents,

    pub(crate) this_year_share: Rate,
    pub(crate) prior_year_share: Rate,
    pub(crate) prior_year_high: Rate,
    pub(crate) step: USCents,
    pub(crate) step_separate: USCents,

    pub(crate) shares: Vec<Rate>,
    pub(crate) annualised: Vec<Rate>,
    pub(crate) dues: Vec<Date>,
    pub(crate) calendar: Calendar,
    pub(crate) period_ends: Vec<Date>,

    /// § 685(c)(5)(B).
    pub(crate) file_and_pay_by: Date,

    pub(crate) stop: Date,

    /// The § 697(j) underpayment rate by quarter, split also at each due
    /// date, where IT-2105.9 ends a penalty period.
    pub(crate) rates: RateSeries,
}

/// Returns New York's estimated tax rules for a tax year, or
/// [`Error::NoRules`] for a year this build does not carry.
pub fn estimated_tax(year: i64) -> Result<Box<dyn EstimatedTax>, Error> {
    let y = match year {
        2026 => year_2026()?,
        _ => return Err(estimated::no_rules(&JURISDICTION, year)),
    };
    Ok(Box::new(NewYorkEstimated {
        y,
        explanation: new_york_explanation()?,
    }))
}

struct NewYorkEstimated {
    y: EstimatedYear,
    explanation: Explanation,
}

#[derive(Default)]
struct Assessment {
    requirement: Requirement,
    exempt: bool,
    tax_known: bool,
    dues: Vec<Date>,
    installments: Vec<USCents>,
    excused: bool,
    notes: Notes,
}

/// The facts each divergence's readings turn on.
#[derive(Default)]
struct Touches {
    filed_next_business_day: bool,
    casualty: bool,
    late_start: bool,
}

/// Go's `%T` for the extras a caller passed, as far as Rust can tell it.
fn type_name(v: &(dyn Any + Send + Sync)) -> &'static str {
    if v.is::<&str>() || v.is::<String>() {
        "string"
    } else if v.is::<()>() {
        "struct {}"
    } else {
        "unknown"
    }
}

impl NewYorkEstimated {
    fn extras(&self, f: &Facts) -> Result<EstimatedFacts, Error> {
        match f.state() {
            None => Ok(EstimatedFacts::default()),
            Some(s) => match s.downcast_ref::<EstimatedFacts>() {
                Some(e) => Ok(*e),
                None => Err(Error::Invalid(format!(
                    "newyorktaxcalc: the facts carry {}, not newyorktaxcalc.EstimatedFacts",
                    type_name(s)
                ))),
            },
        }
    }

    fn check(&self, f: &Facts, extras: &EstimatedFacts) -> Result<(), Error> {
        if f.jurisdiction() != JURISDICTION {
            return Err(Error::Invalid(format!(
                "newyorktaxcalc: these are New York's rules, and the facts are for {}",
                f.jurisdiction()
            )));
        }
        if f.tax_year() != self.y.year {
            return Err(Error::Invalid(format!(
                "newyorktaxcalc: these are the {} rules, and the facts are for {}",
                self.y.year,
                f.tax_year()
            )));
        }
        if f.farmer_or_fisher() {
            return Err(Error::with_detail(
                Error::Unsupported,
                "farmers and fishermen have one installment by January 15 at 66 2/3% and file federal Form 2210F, which is not built",
            ));
        }
        if extras.farmer_last_year() == Some(true) {
            return Err(Error::with_detail(
                Error::Unsupported,
                "two-thirds of last year's federal gross income from farming or fishing puts this year under the farmers' rules, which are not built",
            ));
        }
        if f.elections().annualised() {
            for (i, p) in f.periods().iter().enumerate() {
                let want = self.y.period_ends.get(i).copied().unwrap_or_default();
                if p.end() != want {
                    return Err(Error::Invalid(format!(
                        "newyorktaxcalc: IT-2105.9 Worksheet 1 period {} ends on {}, not {}",
                        i + 1,
                        want,
                        p.end()
                    )));
                }
            }
        }
        Ok(())
    }

    /// Where the department's forms and the statutes part ways on a rule
    /// every New York computation uses. They are in
    /// research/estimated-payments/ny.md and checked there against both.
    fn divergences(&self, t: &Touches) -> Result<Vec<Divergence>, Error> {
        let mut out = Vec::new();
        for b in [
            working::new_divergence("January 31, 2027 is a Sunday")
                .form(
                    "The instructions print January 31, 2027 and say nothing of moving it. The date is not moved.",
                    "IT-2105-I (2026), When to pay your estimated tax",
                )
                .statute(
                    "An act due on a weekend or public holiday may be done on the next business day. That would make a return filed and paid on February 1, 2027 timely.",
                    "N.Y. Gen. Constr. Law § 25-a(1); Tax Law § 685(c)(5)(B)",
                )
                .uses(Reading::Form)
                .affects(t.filed_next_business_day),
            working::new_divergence("Casualty or disaster")
                .form(
                    "The instructions list no waiver for a casualty or disaster. The retirement and disability waiver is listed.",
                    "IT-2105.9-I (2025), Exceptions to penalty",
                )
                .statute(
                    "The Department can find a penalty against equity and good conscience. That finding must rest on a casualty, disaster or other unusual circumstance. To that extent, no penalty is added.",
                    "Tax Law § 685(d)(4)(A)",
                )
                .uses(Reading::Statute)
                .affects(t.casualty),
            working::new_divergence("A requirement that arises late")
                .form(
                    "IT-2105-I covers a filer who first has to pay after April 15. It has them pay 50%, 75% or all of the estimated tax at the first payment. IT-2105.9 charges 25% in every column.",
                    "IT-2105-I (2026), worksheet line 28, Computation of installments; IT-2105.9-I (2025), Line 25",
                )
                .statute(
                    "Each required installment is 25% of the required annual payment. Only the annualized method lowers one.",
                    "Tax Law § 685(c)(3)(A), (4)",
                )
                .uses(Reading::Form)
                .affects(t.late_start),
        ] {
            out.push(b.build()?);
        }
        Ok(out)
    }

    fn assess(&self, f: &Facts) -> Result<Assessment, Error> {
        let extras = self.extras(f)?;
        self.check(f, &extras)?;
        let y = &self.y;
        let mut a = Assessment {
            dues: estimated::move_dates(&y.calendar, &y.dues),
            ..Assessment::default()
        };
        let last = y.year - 1;

        let share = f.farm_share().0 + f.fish_share().0;
        if share > 0 && extras.farmer_last_year().is_none() {
            a.notes.ask(
                PositionKind::JurisdictionFact,
                format!(
                    "Was at least 2/3 of your {last} federal gross income from farming or fishing?"
                ),
                "If so, New York's farmers' rules apply instead.",
            );
        }

        let tax_opt = f.this_year().tax();
        let tax_known = tax_opt.is_some();
        let tax = tax_opt.unwrap_or_default();
        a.tax_known = tax_known;
        let withheld = f.withheld_total();
        let prior_opt = f.prior_year();
        let has_prior = prior_opt.is_some();
        let prior = prior_opt.unwrap_or_default();
        let prior_figures = if has_prior {
            prior.figures().unwrap_or_default()
        } else {
            estimated::Figures::default()
        };
        let prior_tax = if has_prior {
            prior_figures.tax().unwrap_or_default()
        } else {
            USCents(0)
        };

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
                "Tax Law § 685(c)(3)(B)(i); IT-2105.9-I (2025), Part 1",
                format!(
                    "{} of this year's New York State, City, Yonkers and MCTMT tax of {} is {}",
                    y.this_year_share,
                    tax,
                    this_year.amount()
                ),
            );
        }

        let (nyc, yonkers, mctmt) = extras.subject_to().unwrap_or_default();
        let subject_answered = extras.subject_to().is_some();
        let mut step = y.step;
        if f.filing_status() == Status::MarriedFilingSeparately {
            step = y.step_separate;
        }
        let unavailable = |why: String| -> TargetOptionBuilder {
            let mut b =
                estimated::new_target_option(Basis::PriorYearTax, y.prior_year_share, USCents(0));
            b.unavailable(why);
            b
        };
        // Questions about the target, asked only when no exception sets it.
        let mut unless_exempt: Vec<(PositionKind, String, String)> = Vec::new();
        let prior_option = if !has_prior {
            unless_exempt.push((
                PositionKind::PriorYearReturn,
                format!("Did you file a {last} New York return, and what was its tax?"),
                format!(
                    "With it, 100% of your {last} tax could be a lower target than 90% of this year's."
                ),
            ));
            unavailable(format!("Needs your {last} New York return"))
        } else if !prior.filed() {
            unavailable(format!("You didn't file a {last} New York return"))
        } else if prior.months() != 12 {
            unavailable(format!(
                "Your {last} New York return covered {} months, not a full year",
                prior.months()
            ))
        } else {
            let agi = prior_figures.state_agi();
            let earnings = extras.prior_year_mctd_earnings();
            let mut rate = y.prior_year_share;
            let applied;
            match (agi, earnings) {
                (None, _) => {
                    rate = y.prior_year_high;
                    applied = format!("{last} New York AGI was not given");
                    unless_exempt.push((
                        PositionKind::AdjustedGrossIncome,
                        format!("What was your {last} New York adjusted gross income?"),
                        format!(
                            "The target is {} of your {last} tax until then, or {} at {} or less.",
                            y.prior_year_high,
                            y.prior_year_share,
                            step.whole()
                        ),
                    ));
                }
                (Some(agi), _) if agi > step => {
                    rate = y.prior_year_high;
                    applied = format!("{last} New York AGI of {agi} is more than {step}");
                }
                (Some(_), Some(earnings)) if earnings > step => {
                    rate = y.prior_year_high;
                    applied = format!(
                        "{last} net earnings from self-employment allocated to the MCTD of {earnings} are more than {step}"
                    );
                }
                (Some(_), None) if mctmt => {
                    rate = y.prior_year_high;
                    applied = format!("{last} MCTD earnings were not given");
                    unless_exempt.push((
                        PositionKind::JurisdictionFact,
                        format!(
                            "What were your {last} self-employment earnings allocated to the MCTD?"
                        ),
                        format!(
                            "The MCTD is the Metropolitan Commuter Transportation District. Until then, the target is {} of your {last} tax, the rate above {}.",
                            y.prior_year_high,
                            step.whole()
                        ),
                    ));
                }
                (Some(agi), _) => {
                    applied = format!("{last} New York AGI of {agi} is not more than {step}");
                }
            }
            let amount = rate.of(prior_tax);
            a.notes.rule(
                "Tax Law § 685(c)(3)(B)(ii), (C); IT-2105.9-I (2025), Line 16 worksheet",
                format!(
                    "{applied}, so the prior-year test is {rate} of the {prior_tax} tax on the {last} return, or {amount}"
                ),
            );
            estimated::new_target_option(Basis::PriorYearTax, rate, amount)
        };
        let prior_opt = prior_option.build()?;

        let mut rb = estimated::new_requirement();
        rb.option(this_year.clone()).option(prior_opt.clone());
        let (mut chosen, mut target) = (Basis::ThisYearTax, this_year.amount());
        if prior_opt.available() && (!tax_known || prior_opt.amount() < target) {
            (chosen, target) = (Basis::PriorYearTax, prior_opt.amount());
        }

        let mut taxes = 1;
        if nyc {
            taxes += 1;
        }
        if yonkers {
            taxes += 1;
        }
        let threshold = y.threshold * taxes;
        if !tax_known {
            if prior_opt.available() {
                unless_exempt.push((
                    PositionKind::ThisYearTax,
                    format!("What do you expect your {} New York tax to be?", y.year),
                    format!(
                        "90% of it may be a lower target than your {last} tax. It also decides whether the {} threshold excuses you.",
                        threshold.whole()
                    ),
                ));
            } else {
                unless_exempt.push((
                    PositionKind::ThisYearTax,
                    format!("What do you expect your {} New York tax to be?", y.year),
                    format!(
                        "Without it there's no target, as there's no full-year {last} New York return."
                    ),
                ));
            }
        }

        // § 685(d)(2): no liability last year, twelve months, and residence
        // or New York source income throughout it.
        let no_liability = has_prior
            && prior.months() == 12
            && ((prior.filed() && prior_tax.0 == 0) || prior.not_required_to_file());
        let mut qualifies = false;
        if no_liability {
            let residence = prior.full_year_resident();
            let source = extras.prior_year_new_york_source_income();
            if residence == Some(true) || source == Some(true) {
                qualifies = true;
            } else if residence.is_none() || source.is_none() {
                a.notes.ask(
                    PositionKind::Residency,
                    format!(
                        "All through {last}, did you have New York residency or New York source income?"
                    ),
                    format!(
                        "If so, having no New York tax liability for {last} means no penalty for {}.",
                        y.year
                    ),
                );
            }
        }

        if qualifies {
            let exception = estimated::new_exception("No New York tax due last year").build()?;
            a.exempt = true;
            rb.required(
                false,
                format!(
                    "You had no New York tax liability for {last}. You were a resident or had New York source income all year. That means no estimated payments are required for {}.",
                    y.year
                ),
            )
            .exempt(exception);
            a.notes.rule(
                "Tax Law § 685(d)(2); IT-2105.9-I (2025), Exceptions to penalty (2)",
                format!(
                    "the {last} return showed no liability for a full 12 months, so there is no penalty for {}",
                    y.year
                ),
            );
        } else if tax_known && tax - withheld < threshold {
            let exception =
                estimated::exception_below("Tax less withholding", tax - withheld, threshold)?;
            a.exempt = true;
            let mut text = format!(
                "Your New York tax of {} less {} withheld {}.",
                tax.whole(),
                withheld.whole(),
                estimated::leaves(tax - withheld)
            );
            if tax >= withheld {
                text += &format!(" That is under {}.", threshold.whole());
                text += &format!(
                    " The threshold is {} for each of the state, City and Yonkers taxes you pay.",
                    y.threshold.whole()
                );
            }
            text += " No estimated payments are needed.";
            rb.required(false, text).exempt(exception);
            a.notes.rule(
                "Tax Law § 685(d)(1); IT-2105.9-I (2025), Line 15 worksheet",
                format!(
                    "{tax} of tax less {withheld} withheld is {}, under {threshold} for {taxes} taxes, so there is no penalty",
                    tax - withheld
                ),
            );
        } else if !tax_known && !prior_opt.available() {
            rb.required(
                false,
                format!(
                    "Without your {} tax or a full-year {last} return, there's no New York target.",
                    y.year
                ),
            )
            .no_target();
        } else if !tax_known && withheld >= target {
            rb.required(
                false,
                format!(
                    "Your withholding of {} reaches {} of your {last} tax, {}. No estimated payments are needed, whatever your {} tax turns out to be.",
                    withheld.whole(),
                    prior_opt.percent(),
                    target.whole(),
                    y.year
                ),
            )
            .choose(chosen);
        } else if !tax_known {
            rb.required(
                true,
                format!(
                    "Your target is {} of your {last} tax, {}. It keeps you clear of a penalty whatever your {} tax turns out to be. After {} of withholding, you need {} of estimated payments. Pay a quarter by each due date.",
                    prior_opt.percent(),
                    target.whole(),
                    y.year,
                    withheld.whole(),
                    (target - withheld).whole()
                ),
            )
            .choose(chosen);
        } else if withheld >= target {
            rb.required(
                false,
                format!(
                    "Your New York withholding of {} reaches the {} needed for the year. No estimated payments are needed.",
                    withheld.whole(),
                    target.whole()
                ),
            )
            .choose(chosen);
        } else {
            rb.required(
                true,
                format!(
                    "Your New York tax of {} less {} withheld leaves {}. That is {} or more, so you need {} of estimated payments. Pay a quarter by each due date.",
                    tax.whole(),
                    withheld.whole(),
                    (tax - withheld).whole(),
                    threshold.whole(),
                    (target - withheld).whole()
                ),
            )
            .choose(chosen);
        }
        a.requirement = rb.build()?;
        if !a.exempt {
            for (k, q, e) in unless_exempt {
                a.notes.ask(k, q, e);
            }
        }
        if !a.exempt && !subject_answered {
            a.notes.ask(
                PositionKind::JurisdictionFact,
                "Which of the New York City and Yonkers taxes do you pay?",
                format!(
                    "Each adds {} to the threshold. Until then, only the state's {} counts.",
                    y.threshold.whole(),
                    y.threshold.whole()
                ),
            );
        }

        let cumulative = estimated::cumulative_shares(a.requirement.target(), &y.shares)?;
        a.installments = estimated::increments(&cumulative);
        a.notes.rule(
            "Tax Law § 685(c)(1), (3)(A); IT-2105.9-I (2025), Line 25",
            format!(
                "25% of {} is due on each of {}, {}, {} and {}",
                a.requirement.target(),
                a.dues[0].long(),
                a.dues[1].long(),
                a.dues[2].long(),
                a.dues[3].long()
            ),
        );

        if f.elections().annualised() && !a.exempt {
            let annual: Vec<USCents> = f.periods().iter().map(|p| p.annualised_tax()).collect();
            let ann = estimated::annualised(&annual, &y.annualised, &a.installments)?;
            a.notes.rule(
                "Tax Law § 685(c)(4); IT-2105.9-I (2025), Worksheets 1 to 4c",
                format!(
                    "under the annualized method the installments are {}, {}, {} and {}: 22.5%, 45%, 67.5% and 90% of the annualized tax, less earlier installments, and never more than the regular installment plus any reduction carried forward",
                    ann[0], ann[1], ann[2], ann[3]
                ),
            );
            a.installments = ann;
        }

        // § 685(c)(5)(B). January 31, 2027 is a Sunday; IT-2105-I prints the
        // date and says nothing of moving it, so it is not moved.
        let filed = f.return_filed();
        if !filed.is_zero() && !filed.after(y.file_and_pay_by) && !a.exempt {
            a.excused = true;
            a.notes.rule(
                "Tax Law § 685(c)(5)(B); IT-2105-I (2026)",
                format!(
                    "the return was filed and paid on {}, by {}, so the January installment bears no penalty",
                    filed.long(),
                    y.file_and_pay_by.long()
                ),
            );
        }
        if a.exempt {
            a.installments = vec![USCents(0); a.dues.len()];
        }

        let mut t = Touches::default();
        if !a.exempt {
            t.filed_next_business_day = !filed.is_zero()
                && filed.after(y.file_and_pay_by)
                && !filed.after(y.calendar.next_business_day(y.file_and_pay_by));
            let waiver = f.positions().waiver().unwrap_or_default();
            t.casualty = waiver == WaiverKind::Casualty;
            let met = f.requirement_first_met();
            t.late_start = !met.is_zero() && met.after(a.dues[0]);
        }
        let divergences = self.divergences(&t)?;
        a.notes.diverges(&divergences);
        Ok(a)
    }

    fn ledger(&self, f: &Facts, a: &Assessment) -> Result<(Vec<LedgerEntry>, Notes), Error> {
        let credits = if f.elections().withholding_as_withheld() {
            estimated::credits_as_withheld(f, &a.dues)?
        } else {
            estimated::credits(f, &a.dues, &self.y.shares)?
        };
        let mut required = a.installments.clone();
        if a.excused {
            required[3] = USCents(0);
        }
        let ledger = estimated::apply(&a.dues, &required, &credits)?;
        let mut notes = Notes::default();
        if f.elections().withholding_as_withheld() {
            notes.rule(
                "Tax Law § 685(c)(5)(A)",
                format!(
                    "as shown, the {} withheld is credited on the dates it was withheld",
                    f.withheld_total()
                ),
            );
        } else {
            notes.rule(
                "Tax Law § 685(c)(1), (5)(A); IT-2105.9-I (2025), Line 26",
                format!(
                    "the {} withheld is treated as paid in four equal parts on the due dates, and a payment goes first to the earliest installment",
                    f.withheld_total()
                ),
            );
        }
        Ok((ledger, notes))
    }

    /// The installments the ledger gives.
    fn schedule_of(
        &self,
        a: &Assessment,
        ledger: &[LedgerEntry],
    ) -> Result<Vec<Installment>, Error> {
        let mut out = Vec::with_capacity(ledger.len());
        let mut needed = USCents(0);
        for (i, l) in ledger.iter().enumerate() {
            needed += a.installments[i];
            let mut b = estimated::new_installment(i as i64 + 1, l.due());
            b.needed_by(needed)
                .credited_by(l.credited_by())
                .shortfall(l.shortfall());
            if a.excused && i == 3 {
                b.excused();
            }
            out.push(b.build()?);
        }
        Ok(out)
    }
}

impl EstimatedTax for NewYorkEstimated {
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
                    "with no {} New York return to measure against, there is no target to schedule",
                    self.y.year - 1
                ),
            ));
        }
        let (ledger, notes) = self.ledger(f, &a)?;
        let out = self.schedule_of(&a, &ledger)?;
        Ok(working::new_result(out)
            .with(&a.notes)
            .with(&notes)
            .build()?)
    }

    fn penalty(&self, f: &Facts, as_of: Date) -> Result<working::Result<Penalty>, Error> {
        if as_of.is_zero() {
            return Err(Error::Invalid(
                "newyorktaxcalc: a penalty needs the date to figure it to".into(),
            ));
        }
        let a = self.assess(f)?;
        if !a.requirement.target_known() {
            return Err(Error::with_detail(
                Error::NeedsThisYearTax,
                format!(
                    "with no {} New York return to measure against, there is no target to figure a penalty from",
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
        let waiver_answer = f.positions().waiver();
        let waiver = waiver_answer.unwrap_or_default();
        let (ledger, mut notes) = self.ledger(f, &a)?;
        let schedule = self.schedule_of(&a, &ledger)?;
        if a.exempt {
            let p = estimated::new_penalty(stop)
                .waiver(waiver)
                .because(a.requirement.clone(), &schedule)
                .build()?;
            return Ok(working::new_result(p).with(&a.notes).build()?);
        }

        // IT-2105.9 Part 3: each part of each shortfall, by the day ÷ 365, at
        // each period's rate, each period ending at the payment, the end of
        // the rate period or the next due date.
        let (mut p, unrated) =
            estimated::simple_penalty(&ledger, stop, &y.rates, waiver, &a.requirement, &schedule)?;
        if !a.tax_known {
            p = p.to_builder().upper_bound().build()?;
            notes.rule(
                "Tax Law § 685(c)(3)(B)",
                format!(
                    "this year's New York tax was not given, so the penalty is figured against {} of the {} tax alone; the required payment is the lesser of that and 90% of this year's tax, so {} is the most the penalty can be, and it is less if this year's tax turns out lower",
                    a.requirement.options()[1].percent(),
                    y.year - 1,
                    p.total()
                ),
            );
        }
        notes.rule(
            "Tax Law §§ 685(c)(1), 697(j); IT-2105.9-I (2025), Period of underpayment and Penalty rate",
            format!(
                "each part of each shortfall is charged the quarter's rate, simple, by the day ÷ 365, from its due date until paid or {}; the penalty to {} is {}",
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
        if p.total().0 > 0 && waiver_answer.is_none() {
            notes.ask(
                PositionKind::PenaltyWaiver,
                "Are you asking New York to waive the penalty, shown in full until you say?",
                format!(
                    "A casualty, disaster or other unusual circumstance can get it waived. So can retiring after 62 or disability in {} or {}, with reasonable cause.",
                    y.year - 1,
                    y.year
                ),
            );
        }
        // Tax Law § 685(d)(4) grants the federal pair of waivers.
        if let Some(o) = estimated::standard_waiver(f, "the New York Tax Department", 62)
            && p.total().0 > 0
        {
            p = p.to_builder().waiver_outcome(o).build()?;
        }
        Ok(working::new_result(p).with(&a.notes).with(&notes).build()?)
    }

    fn explanation(&self) -> Explanation {
        self.explanation.clone()
    }
}

/// Written by hand and checked against research/estimated-payments/ny.md.
fn new_york_explanation() -> Result<Explanation, Error> {
    estimated::new_explanation(
        "New York expects estimated payments once tax after withholding is $300 or more. That is your state, New York City or Yonkers tax. It also expects them if you have any MCTMT. All four taxes go on one voucher, and the penalty is figured on them together.",
    )
    .paragraph("The target is the smaller of 90% of this year's total or 100% of last year's. You are clear if withholding and estimated payments for all four taxes reach it. A quarter is due by each of April 15, June 15, September 15 and January 15. As federally, last year's figure can become 110%. That happens if last year's New York AGI was over $150,000. The step is $75,000 if married filing separately. New York adds the same step for MCTD self-employment earnings. Last year's figure needs a full-year return.")
    .paragraph("There is no penalty if your tax due after withholding is under $300 a tax. That is $300 for each of the state, City and Yonkers taxes you pay. Nor is there one for a New York resident all last year with no New York tax due. Filing and paying in full by January 31 clears the January payment's penalty.")
    .paragraph("The penalty is simple interest on each shortfall for the days it stays unpaid. It stops on April 15 of the next year. Each payment goes first to the oldest shortfall. New York sets its own rate each quarter. It is the federal short-term rate plus 5½ points, never below 7½%. For 2026 that was 8.5% from April to June and 9.5% from July. The Department can waive the penalty for a casualty. It can also waive it for retiring after 62 or disability with reasonable cause.")
    .versus_federal("New York's threshold is $300 for each tax rather than $1,000. The City, Yonkers and MCTMT taxes are added in and figured together. MCTD self-employment earnings over $150,000 can trigger 110% on their own. New York uses its own rate, not the federal underpayment rate. It is the federal short-term rate plus 5½ points, with a 7½% floor.")
    .source("N.Y. Tax Law §§ 685(c), (d), 697(j); N.Y. Gen. Constr. Law § 25-a")
    .source("IT-2105-I (2026), Instructions for Form IT-2105, Estimated Tax Payment Voucher for Individuals")
    .source("IT-2105.9-I (2025), Instructions for Form IT-2105.9, Underpayment of Estimated Tax")
    .source("Department of Taxation and Finance, Interest rates for each quarter of 2026")
    .build()
}
