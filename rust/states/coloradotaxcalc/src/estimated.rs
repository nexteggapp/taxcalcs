//! Colorado's estimated tax rules, C.R.S. § 39-22-605, as the shared
//! [`EstimatedTax`] interface. The interest rate is set under
//! C.R.S. § 39-21-110.5.
//!
//! Colorado's section is written on the federal pattern, but it is Colorado's
//! own text, not an adoption of 26 U.S.C. § 6654, and nothing here borrows
//! from the federal package. The current-year test is 70%; the penalty is
//! interest at Colorado's regular rate for each calendar year; the annualised
//! method is the department's own schedule on DR 0204 Part 4, open only to a
//! filer who annualises federally.
//!
//! The figure follows the department's forms, DR 0104EP for 2026 and DR 0204
//! for 2025, and the result's Divergences say where C.R.S. § 39-22-605 reads
//! otherwise. The section was last amended in 2022 (SB 22-233), and the Red
//! Books for 2025 and 2026 list no change to it or to § 39-21-110.5, so no
//! form here predates the law it describes. The one place the figure does
//! not follow the form is the interest rate: DR 0204 (2025) prints the rates
//! for a 2025 underpayment, and a 2026 underpayment is charged 2026's and
//! 2027's.

use std::any::Any;

use estimated::{
    Basis, Calendar, Date, Error, EstimatedTax, Explanation, Facts, Installment, LedgerEntry,
    Penalty, RateSeries, Requirement, TargetOptionBuilder, WaiverKind, WaiverOutcome,
};
use money::{Rate, USCents, percent};
use working::{Divergence, Notes, PositionKind, Reading};

/// Colorado's postal code.
const JURISDICTION: estimated::Jurisdiction = estimated::COLORADO;

/// The facts Colorado's rules read that no other jurisdiction does. Carry
/// them in [`estimated::FactsBuilder::state`].
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct EstimatedFacts {
    prior_farmer: bool,
    prior_farmer_answered: bool,
    federal_annualised: bool,
    federal_annualised_answered: bool,
}

impl EstimatedFacts {
    /// Whether last year's return showed at least two-thirds of gross income
    /// from farming or fishing, which makes the filer a farmer or fisherman
    /// this year under C.R.S. § 39-22-605(2)(a)(II), and whether the caller
    /// said: `(farmer, answered)`. This year's share is
    /// [`Facts::farm_share`] and [`Facts::fish_share`].
    pub fn prior_year_farmer_or_fisher(&self) -> (bool, bool) {
        (self.prior_farmer, self.prior_farmer_answered)
    }

    /// Whether the filer elected annualised installments for federal income
    /// tax, which C.R.S. § 39-22-605(6)(d) requires before the annualised
    /// method is open in Colorado, and whether the caller said:
    /// `(annualised, answered)`.
    pub fn federal_annualised(&self) -> (bool, bool) {
        (self.federal_annualised, self.federal_annualised_answered)
    }
}

/// Builds [`EstimatedFacts`].
#[derive(Clone, Debug, Default)]
pub struct EstimatedFactsBuilder {
    e: EstimatedFacts,
}

/// Starts Colorado's extras.
pub fn new_estimated_facts() -> EstimatedFactsBuilder {
    EstimatedFactsBuilder::default()
}

impl EstimatedFactsBuilder {
    /// Answers whether last year's return was a farmer's or fisherman's.
    pub fn prior_year_farmer_or_fisher(&mut self, farmer: bool) -> &mut Self {
        (self.e.prior_farmer, self.e.prior_farmer_answered) = (farmer, true);
        self
    }

    /// Answers whether the filer annualises federally.
    pub fn federal_annualised(&mut self, annualised: bool) -> &mut Self {
        (
            self.e.federal_annualised,
            self.e.federal_annualised_answered,
        ) = (annualised, true);
        self
    }

    /// Has nothing to refuse: each fact is a yes or a no.
    pub fn build(&self) -> Result<EstimatedFacts, Error> {
        Ok(self.e)
    }
}

/// One tax year's published figures.
pub(crate) struct EstimatedYear {
    pub(crate) year: i64,

    /// DR 0204 Exception 2's statutory exemption: no penalty when tax less
    /// withholding is not more than it.
    pub(crate) threshold: USCents,

    /// The current-year test; `prior_percent` and `prior_percent_high` the
    /// prior-year test at or below and above the step in last year's federal
    /// AGI.
    pub(crate) this_year_percent: Rate,
    pub(crate) prior_percent: Rate,
    pub(crate) prior_percent_high: Rate,
    pub(crate) prior_year_step: USCents,
    pub(crate) prior_year_step_separate: USCents,

    pub(crate) dues: Vec<Date>,
    pub(crate) calendar: Calendar,
    pub(crate) file_and_pay_by: Date,
    pub(crate) stop: Date,

    /// DR 0204 Part 4 lines 20, 22 and 25, and `tax_rate` the rate line 24
    /// applies.
    pub(crate) period_ends: Vec<Date>,
    pub(crate) factors: Vec<Rate>,
    pub(crate) annualised_percentages: Vec<Rate>,
    pub(crate) tax_rate: Rate,

    /// The § 39-21-110.5 regular rate, one period for each calendar year, as
    /// far as it has been published.
    pub(crate) rates: RateSeries,
}

/// C.R.S. § 39-22-605(6)(a) and DR 0204 line 7: each installment is a
/// quarter of the required annual payment.
fn quarters() -> [Rate; 4] {
    [percent(25), percent(25), percent(25), percent(25)]
}

/// Returns Colorado's estimated tax rules for a tax year, or
/// [`Error::NoRules`] for a year this build does not carry.
pub fn estimated_tax(year: i64) -> Result<Box<dyn EstimatedTax>, Error> {
    let y = match year {
        2026 => crate::estimated_y2026::year_2026()?,
        _ => return Err(estimated::no_rules(&JURISDICTION, year)),
    };
    Ok(Box::new(ColoradoEstimated { y }))
}

struct ColoradoEstimated {
    y: EstimatedYear,
}

/// What the three questions share.
#[derive(Default)]
struct Assessment {
    requirement: Requirement,
    exempt: bool,
    tax_known: bool,
    dues: Vec<Date>,

    /// Each installment's own amount: a quarter of the target, or DR 0204
    /// line 26 under the annualised method.
    installments: Vec<USCents>,
    excused: bool,

    notes: Notes,
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

/// Which divergences change these facts' figures or answers.
#[derive(Default)]
struct Affected {
    threshold: bool,
    rates: bool,
    no_liability: bool,
    waiver: bool,
    moved: bool,
}

fn separate(s: filing::Status) -> bool {
    s == filing::Status::MarriedFilingSeparately
}

/// "Aug 31" inside the tax year and "Jan 15, 2027" outside it.
fn short_date(d: Date, year: i64) -> String {
    let m: String = d.month().to_string().chars().take(3).collect();
    if d.year() == year {
        return format!("{m} {}", d.day());
    }
    format!("{m} {}, {}", d.day(), d.year())
}

/// Prints an annualisation factor as the form does: 4, 2.4, 1.5.
fn factor_string(r: Rate) -> String {
    let (whole, frac) = (r.0 / 10_000, r.0 % 10_000);
    if frac == 0 {
        return whole.to_string();
    }
    format!("{whole}.{frac:04}")
        .trim_end_matches('0')
        .to_string()
}

impl ColoradoEstimated {
    fn extras(&self, f: &Facts) -> Result<EstimatedFacts, Error> {
        match f.state() {
            None => Ok(EstimatedFacts::default()),
            Some(s) => match s.downcast_ref::<EstimatedFacts>() {
                Some(e) => Ok(*e),
                None => Err(Error::Invalid(format!(
                    "coloradotaxcalc: the facts carry {}, not coloradotaxcalc.EstimatedFacts",
                    type_name(s)
                ))),
            },
        }
    }

    fn check(&self, f: &Facts, extras: &EstimatedFacts) -> Result<(), Error> {
        let (farmer_last_year, _) = extras.prior_year_farmer_or_fisher();
        if f.jurisdiction() != JURISDICTION {
            return Err(Error::Invalid(format!(
                "coloradotaxcalc: these are Colorado's rules, and the facts are for {}",
                f.jurisdiction()
            )));
        }
        if f.tax_year() != self.y.year {
            return Err(Error::Invalid(format!(
                "coloradotaxcalc: these are the {} rules, and the facts are for {}",
                self.y.year,
                f.tax_year()
            )));
        }
        if f.farmer_or_fisher() || farmer_last_year {
            return Err(Error::with_detail(
                Error::Unsupported,
                "C.R.S. § 39-22-605(10) gives farmers and fishermen one installment by January 15 at 50%, which is not built",
            ));
        }
        if f.elections().annualised() {
            for (i, p) in f.periods().iter().enumerate() {
                let want = self.y.period_ends.get(i).copied().unwrap_or_default();
                if p.end() != want {
                    return Err(Error::Invalid(format!(
                        "coloradotaxcalc: DR 0204 Part 4 period {} ends on {}, not {}",
                        i + 1,
                        want,
                        p.end()
                    )));
                }
            }
        }
        Ok(())
    }

    /// Where the department's forms and C.R.S. § 39-22-605 part ways on a
    /// rule every Colorado computation uses. They are found in
    /// research/estimated-payments/co.md and checked against the statute's
    /// text in the Go package's testdata/.
    fn divergences(&self, inn: &Affected) -> Result<Vec<Divergence>, Error> {
        let y = &self.y;
        let rates: Vec<String> = y
            .rates
            .periods()
            .iter()
            .map(|rp| format!("{} for {}", rp.rate(), rp.from().year()))
            .collect();
        let mut out = Vec::new();
        for b in [
            working::new_divergence("Tax of exactly $1,000 after withholding")
                .form("No penalty when tax less withholding is $1,000 or less. DR 0204 subtracts a $1,000 exemption and charges nothing at zero. DR 0104EP asks for payments only above $1,000.",
                    "DR 0204 (09/25/25), Part 1, Exception 2, lines 2(a) to 3; DR 0104EP (06/10/25), General Rule")
                .statute("No penalty only when tax less withholding is less than $1,000. Exactly $1,000 is subject to it.",
                    "C.R.S. § 39-22-605(7)(a)")
                .uses(Reading::Form)
                .affects(inn.threshold)
                .clone(),
            working::new_divergence("The interest rates DR 0204 prints")
                .form(format!("Lines 15 and 18 charge 12% to December 31 and 11% after it. Those are the rates for {} and {}, which fit a {} underpayment.", y.year - 1, y.year, y.year - 1),
                    "DR 0204 (09/25/25), Part 3, lines 15 and 18")
                .statute(format!("The rate is fixed for each calendar year as the prime rate plus three points. It is set each July 1 for the next January 1: {}.", rates.join(" and ")),
                    "C.R.S. §§ 39-22-605(3), 39-21-110.5(2), (3); CDOR, Tax Topics: Penalties and Interest")
                .uses(Reading::Statute)
                .affects(inn.rates)
                .clone(),
            working::new_divergence("No liability last year")
                .form("The form lists two exceptions. One is a farmer or fisherman who files and pays by March 1. The other is tax of $1,000 or less after withholding. No liability last year isn't one of them.",
                    "DR 0204 (09/25/25), Part 1 and its instructions")
                .statute("No penalty if last year was twelve months and you had no liability for it. You must also have been a Colorado resident throughout it.",
                    "C.R.S. § 39-22-605(7)(b)")
                .uses(Reading::Form)
                .affects(inn.no_liability)
                .clone(),
            working::new_divergence("Waivers for good cause, retirement or disability")
                .form("The form and its instructions describe no waiver.",
                    "DR 0204 (09/25/25) and its instructions")
                .statute("The executive director can find that the underpayment was due to good cause. There is no penalty to that extent. No penalty either if you retired after 62 or became disabled this year or last. That waiver needs reasonable cause, not willful neglect.",
                    "C.R.S. § 39-22-605(7)(c), (d)")
                .uses(Reading::Form)
                .affects(inn.waiver)
                .clone(),
            working::new_divergence("Due dates on a weekend or holiday")
                .form(format!("A due date on a weekend or legal holiday moves to the next business day. No {} due date moves.", y.year),
                    "DR 0104EP (06/10/25), Due Dates; DR 0204 (09/25/25), Part 3 instructions; CDOR, Individual Income Tax | Estimated Payments")
                .statute("A due date on a legal federal holiday moves to the adjusted federal due date. Weekends aren't mentioned.",
                    "C.R.S. § 39-22-605(5)(c)")
                .uses(Reading::Form)
                .affects(inn.moved)
                .clone(),
        ] {
            out.push(b.build()?);
        }
        Ok(out)
    }

    fn assess(&self, f: &Facts) -> Result<Assessment, Error> {
        let extras = self.extras(f)?;
        self.check(f, &extras)?;
        let y = &self.y;
        let mut a = Assessment::default();
        let mut inn = Affected::default();

        let step = if separate(f.filing_status()) {
            y.prior_year_step_separate
        } else {
            y.prior_year_step
        };
        let tax_opt = f.this_year().tax();
        let tax_known = tax_opt.is_some();
        let tax = tax_opt.unwrap_or_default();
        a.tax_known = tax_known;
        let withheld = f.withheld_total();
        let prior = f.prior_year();
        let has_prior = prior.is_some();
        let prior = prior.unwrap_or_default();
        let last = y.year - 1;

        // 70% of this year's tax. § 39-22-605(6)(b)(I).
        let this_year = if tax_known {
            estimated::new_target_option(
                Basis::ThisYearTax,
                y.this_year_percent,
                y.this_year_percent.of(tax),
            )
            .build()?
        } else {
            estimated::new_target_option(Basis::ThisYearTax, y.this_year_percent, USCents(0))
                .unavailable(estimated::NEEDS_THIS_YEAR_TAX)
                .build()?
        };
        if tax_known {
            a.notes.rule(
                "C.R.S. § 39-22-605(6)(b)(I); DR 0204 (09/25/25), line 4(b)",
                format!(
                    "{} of this year's Colorado tax of {} is {}",
                    y.this_year_percent,
                    tax,
                    this_year.amount()
                ),
            );
        }

        // Last year's tax: 100%, or 110% above the step in last year's
        // federal AGI, for a filed twelve-month Colorado return.
        let prior_tax = prior.figures().and_then(|pf| pf.tax()).unwrap_or_default();
        let unavailable = |why: String| -> TargetOptionBuilder {
            let mut b =
                estimated::new_target_option(Basis::PriorYearTax, y.prior_percent, USCents(0));
            b.unavailable(why);
            b
        };
        let mut ask_agi = false;
        let prior_option = if !has_prior {
            unavailable(format!("Needs your {last} Colorado return"))
        } else if prior.not_required_to_file() {
            unavailable(format!(
                "You weren't required to file a {last} Colorado return"
            ))
        } else if !prior.filed() {
            unavailable(format!("You didn't file a {last} Colorado return"))
        } else if prior.months() != 12 {
            unavailable(format!(
                "Your {last} Colorado return covered {} months, not a full year",
                prior.months()
            ))
        } else {
            let pf = prior.figures().unwrap_or_default();
            let prior_agi = pf.federal_agi();
            let agi = prior_agi.unwrap_or_default();
            let mut rate = y.prior_percent;
            let mut why = format!("{last} federal AGI of {agi} is not more than {step}");
            match prior_agi {
                None => {
                    rate = y.prior_percent_high;
                    why = format!("{last} federal AGI was not given");
                    ask_agi = true;
                }
                Some(agi) if agi > step => {
                    rate = y.prior_percent_high;
                    why = format!("{last} federal AGI of {agi} is more than {step}");
                }
                Some(_) => {}
            }
            let amount = rate.of(prior_tax);
            a.notes.rule(
                "C.R.S. § 39-22-605(6)(b)(II), (c); DR 0204 (09/25/25), lines 5(a) to 5(c)",
                format!(
                    "{why}, so the prior-year test is {rate} of the {prior_tax} tax on the {last} Colorado return, or {amount}"
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
        // § 39-22-605(7)(b): no liability last year. DR 0204 does not list it,
        // so it is reported rather than applied; see the divergences.
        let no_liability = has_prior
            && prior.months() == 12
            && ((prior.filed() && prior_tax.0 == 0) || prior.not_required_to_file());
        if no_liability {
            let resident_opt = prior.full_year_resident();
            let answered = resident_opt.is_some();
            let resident = resident_opt.unwrap_or(false);
            inn.no_liability = resident || !answered;
            if answered && resident {
                a.notes.rule(
                    "C.R.S. § 39-22-605(7)(b)",
                    format!(
                        "you had no Colorado liability for {last}, a twelve-month year, as a resident throughout it, so the statute excuses any penalty for {}; \
                         DR 0204 does not list this exception, so the figures here are the form's, and the exception is yours to claim",
                        y.year
                    ),
                );
            } else if !answered {
                a.notes.rule(
                    "C.R.S. § 39-22-605(7)(b)",
                    format!(
                        "you had no Colorado liability for {last}, a twelve-month year; if you were a Colorado resident throughout it, the statute excuses any penalty for {}, \
                         though DR 0204 does not list the exception, so the figures here are the form's",
                        y.year
                    ),
                );
            }
        }

        let mut spread = "Pay a quarter by each date.";
        let (federal, answered) = extras.federal_annualised();
        if f.elections().annualised() && (federal || !answered) {
            spread = "Spread them over the annualised installments on DR 0204 Part 4.";
        }
        if !tax_known && !prior_opt.available() {
            rb.required(
                false,
                format!(
                    "Your target needs your {} Colorado tax, as your {last} return can't set one.",
                    y.year
                ),
            )
            .no_target();
        } else if !tax_known && withheld >= target {
            rb.required(
                false,
                format!(
                    "Your withholding of {} already meets {} of your {last} Colorado tax. That comes to {}. No estimated payments are needed, whatever your {} tax turns out to be.",
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
                    "Your target is {} of your {last} Colorado tax, {}. Meeting it keeps you clear of a penalty whatever your {} tax is. After {} of withholding, you need {} of estimated payments. {spread}",
                    prior_opt.percent(),
                    target.whole(),
                    y.year,
                    withheld.whole(),
                    (target - withheld).whole()
                ),
            )
            .choose(chosen);
        } else if tax - withheld <= y.threshold {
            a.exempt = true;
            inn.threshold = tax - withheld == y.threshold;
            let exception =
                estimated::exception_at_most("Tax less withholding", tax - withheld, y.threshold)?;
            let mut at_most = format!(
                "That's {} or less, so no estimated payments are needed.",
                y.threshold.whole()
            );
            if tax < withheld {
                at_most = "No estimated payments are needed.".to_string();
            }
            rb.required(
                false,
                format!(
                    "Your Colorado tax of {} less {} withheld {}. {at_most}",
                    tax.whole(),
                    withheld.whole(),
                    estimated::leaves(tax - withheld)
                ),
            )
            .exempt(exception);
            a.notes.rule(
                "DR 0204 (09/25/25), Part 1, Exception 2; C.R.S. § 39-22-605(7)(a)",
                format!(
                    "{tax} of tax less the {} exemption and {withheld} withheld is not more than zero, so there is no penalty",
                    y.threshold
                ),
            );
        } else if withheld >= target {
            rb.required(
                false,
                format!(
                    "Your Colorado withholding is {}. That already meets the {} required for the year. No estimated payments are needed.",
                    withheld.whole(),
                    target.whole()
                ),
            )
            .choose(chosen);
        } else {
            rb.required(
                true,
                format!(
                    "Your Colorado tax of {} less {} of withholding leaves {}. That's more than {}, and withholding is short of the {} required. You need {} of estimated payments. {spread}",
                    tax.whole(),
                    withheld.whole(),
                    (tax - withheld).whole(),
                    y.threshold.whole(),
                    target.whole(),
                    (target - withheld).whole()
                ),
            )
            .choose(chosen);
        }
        a.requirement = rb.build()?;
        // These answers change the target. An exception sets the target, so
        // they are asked only when none applies.
        if !a.exempt && !has_prior {
            a.notes.ask(
                PositionKind::PriorYearReturn,
                format!("Did you file a {last} Colorado return, and what was its tax?"),
                format!(
                    "With it, {} of last year's tax may set a target below {} of this year's.",
                    y.prior_percent, y.this_year_percent
                ),
            );
        }
        if !a.exempt && ask_agi {
            a.notes.ask(
                PositionKind::AdjustedGrossIncome,
                format!("What was your {last} federal AGI?"),
                format!(
                    "Until then, the target is {} of last year's tax rather than {}. The higher figure applies to federal AGI over {}.",
                    y.prior_percent_high,
                    y.prior_percent,
                    step.whole()
                ),
            );
        }
        if !a.exempt && !tax_known {
            let effect = if prior_opt.available() {
                "Without it, the target can only come from last year's tax.".to_string()
            } else {
                format!("Without it, there's no target, because your {last} return can't set one.")
            };
            a.notes.ask(
                PositionKind::ThisYearTax,
                format!("What do you expect your {} Colorado tax to be?", y.year),
                effect,
            );
        }
        if !a.exempt {
            let (_, answered) = extras.prior_year_farmer_or_fisher();
            if !answered {
                a.notes.ask(
                    PositionKind::JurisdictionFact,
                    format!(
                        "Did your {last} return show two-thirds of gross income from farming or fishing?"
                    ),
                    "Until then, the figures assume not, as farmers and fishermen have other rules.",
                );
            }
        }

        // The due dates. § 39-22-605(5)(b) fixes them; the forms move one that
        // falls on a weekend or legal holiday.
        a.dues = estimated::move_dates(&y.calendar, &y.dues);
        for (moved, due) in a.dues.iter().zip(&y.dues) {
            if moved != due {
                inn.moved = true;
                a.notes.rule(
                    "DR 0104EP (06/10/25), Due Dates",
                    format!(
                        "the installment due {} falls on a weekend or legal holiday, so it is due {}",
                        due.long(),
                        moved.long()
                    ),
                );
            }
        }
        let cumulative = estimated::cumulative_shares(a.requirement.target(), &quarters())?;
        a.installments = estimated::increments(&cumulative);
        a.notes.rule(
            "C.R.S. § 39-22-605(5), (6)(a); DR 0204 (09/25/25), line 7",
            format!(
                "a quarter of {}, {}, is due on each of {}, {}, {} and {}",
                a.requirement.target(),
                a.installments[0],
                a.dues[0].long(),
                a.dues[1].long(),
                a.dues[2].long(),
                a.dues[3].long()
            ),
        );

        if f.elections().annualised() && !a.exempt {
            self.annualise(f, &extras, &mut a);
        }

        // § 39-22-605(9): filing and paying by January 31 excuses the fourth
        // installment. January 31, 2027 is a Sunday. Neither the statute nor
        // DR 0204 says the date moves, so it is not moved here.
        let filed = f.return_filed();
        if !filed.is_zero() && !filed.after(y.file_and_pay_by) && !a.exempt {
            a.excused = true;
            a.notes.rule(
                "C.R.S. § 39-22-605(9); DR 0204 (09/25/25), Part 3 instructions",
                format!(
                    "the return was filed and paid on {}, by {}, so no penalty is computed on the fourth installment",
                    filed.long(),
                    y.file_and_pay_by.long()
                ),
            );
        }
        let moved = y.calendar.next_business_day(y.file_and_pay_by);
        if !a.exempt && filed.after(y.file_and_pay_by) && !filed.after(moved) {
            a.notes.ask(
                PositionKind::UnsettledRule,
                "",
                format!(
                    "DR 0204 and the law don't say if the {} deadline, a {}, moves. Filing and paying on {}, the next business day, is counted late.",
                    short_date(y.file_and_pay_by, y.year),
                    y.file_and_pay_by.weekday(),
                    short_date(filed, y.year)
                ),
            );
        }
        if a.exempt {
            a.installments = vec![USCents(0); a.dues.len()];
        }

        // Which divergences change these figures turns on the shortfalls.
        inn.no_liability = inn.no_liability && !a.exempt;
        if !a.exempt {
            let (ledger, _) = self.ledger(f, &a)?;
            let short = ledger.iter().any(|l| l.shortfall().0 > 0);
            let waiver = f.positions().waiver().unwrap_or_default();
            inn.rates = short;
            inn.waiver = short && waiver != WaiverKind::None;
        }
        let divergences = self.divergences(&inn)?;
        a.notes.diverges(&divergences);
        Ok(a)
    }

    /// Replaces the installments with DR 0204 Part 4 line 26: the period's
    /// Colorado taxable income, times the factor, times the tax rate, times
    /// the applicable percentage, less the installments before it. The form
    /// enters line 26 on line 7 as it stands. It does not compare it with
    /// the regular installment or carry a reduction forward, as the federal
    /// Schedule AI does, and § 39-22-605(6)(d) leaves the method to the
    /// department.
    fn annualise(&self, f: &Facts, extras: &EstimatedFacts, a: &mut Assessment) {
        let y = &self.y;
        let (federal, answered) = extras.federal_annualised();
        if answered && !federal {
            a.notes.rule(
                "C.R.S. § 39-22-605(6)(d); DR 0204 (09/25/25), Part 4 instructions",
                "the annualised method is open only to a filer who elects annualised installments for federal income tax, which you did not, so the regular installments are used",
            );
            return;
        }
        if !answered {
            a.notes.ask(
                PositionKind::JurisdictionFact,
                "Did you elect annualised installments for federal income tax?",
                "Colorado allows annualising only if you did; until then, the figures assume so.",
            );
        }
        let periods = f.periods();
        let mut out = Vec::with_capacity(periods.len());
        let mut before = USCents(0);
        let mut lines = Vec::with_capacity(periods.len());
        for (i, p) in periods.iter().enumerate() {
            let annualised = y.factors[i].of(p.income());
            let annual_tax = y.tax_rate.of(annualised).max(USCents(0));
            let inst = (y.annualised_percentages[i].of(annual_tax) - before).max(USCents(0));
            before += inst;
            out.push(inst);
            lines.push(format!(
                "{} to {}, times {}, is {annualised}, taxed at {} is {annual_tax}, and {} of that less earlier installments is {inst}",
                p.income(),
                p.end().long(),
                factor_string(y.factors[i]),
                y.tax_rate,
                y.annualised_percentages[i]
            ));
        }
        a.installments = out;
        a.notes.rule(
            "C.R.S. § 39-22-605(6)(d); DR 0204 (09/25/25), Part 4, lines 20 to 26",
            format!(
                "under the annualised method each installment is DR 0204 line 26: {}",
                lines.join("; ")
            ),
        );
    }

    /// Applies the credits to the installments, with the fourth set to
    /// nothing when filing early excused it.
    fn ledger(&self, f: &Facts, a: &Assessment) -> Result<(Vec<LedgerEntry>, Notes), Error> {
        let credits = if f.elections().withholding_as_withheld() {
            estimated::credits_as_withheld(f, &a.dues)?
        } else {
            estimated::credits(f, &a.dues, &quarters())?
        };
        let mut required = a.installments.clone();
        if a.excused {
            required[3] = USCents(0);
        }
        let ledger = estimated::apply(&a.dues, &required, &credits)?;
        let mut notes = Notes::default();
        if f.elections().withholding_as_withheld() {
            notes.rule(
                "C.R.S. § 39-22-605(8)(a)",
                format!(
                    "the {} withheld is credited on the dates it was actually withheld, which the filer must establish",
                    f.withheld_total()
                ),
            );
        } else {
            notes.rule(
                "C.R.S. § 39-22-605(8)(a)",
                format!(
                    "the {} withheld is treated as paid in four equal parts on the due dates, whenever it was actually withheld",
                    f.withheld_total()
                ),
            );
        }
        notes.rule(
            "C.R.S. § 39-22-605(4)(c); DR 0104EP (06/10/25), Carryforward of Estimated Overpayment",
            "a payment is credited against the unpaid installments in the order they fall due, and last year's overpayment carried forward counts toward the first",
        );
        Ok((ledger, notes))
    }

    /// The schedule the ledger gives, with January excused when the return
    /// was filed and paid early.
    fn installments(
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

impl EstimatedTax for ColoradoEstimated {
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
                    "with no {} Colorado return to measure against, there is no target to schedule",
                    self.y.year - 1
                ),
            ));
        }
        let (ledger, notes) = self.ledger(f, &a)?;
        let out = self.installments(&a, &ledger)?;
        Ok(working::new_result(out)
            .with(&a.notes)
            .with(&notes)
            .build()?)
    }

    fn penalty(&self, f: &Facts, as_of: Date) -> Result<working::Result<Penalty>, Error> {
        if as_of.is_zero() {
            return Err(Error::Invalid(
                "coloradotaxcalc: a penalty needs the date to figure it to".into(),
            ));
        }
        let a = self.assess(f)?;
        if !a.requirement.target_known() {
            return Err(Error::with_detail(
                Error::NeedsThisYearTax,
                format!(
                    "with no {} Colorado return to measure against, there is no target to figure a penalty from",
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
        let waiver_answered = f.positions().waiver();
        let waiver = waiver_answered.unwrap_or_default();
        let (ledger, mut notes) = self.ledger(f, &a)?;
        let schedule = self.installments(&a, &ledger)?;
        if a.exempt {
            let p = estimated::new_penalty(stop)
                .waiver(waiver)
                .because(a.requirement.clone(), &schedule)
                .build()?;
            return Ok(working::new_result(p).with(&a.notes).build()?);
        }

        let (mut p, unrated) =
            estimated::simple_penalty(&ledger, stop, &y.rates, waiver, &a.requirement, &schedule)?;
        if !a.tax_known {
            p = p.to_builder().upper_bound().build()?;
            notes.rule(
                "C.R.S. § 39-22-605(6)(b)",
                format!(
                    "this year's Colorado tax was not given, so the penalty is figured against {} of the {} tax alone; \
                     the required payment is the lesser of that and {} of this year's tax, so {} is the most the penalty can be, and it is less if this year's tax turns out lower",
                    a.requirement.options()[1].percent(),
                    y.year - 1,
                    y.this_year_percent,
                    p.total()
                ),
            );
        }
        if paid_in_full {
            notes.rule(
                "C.R.S. § 39-22-605(4)(b)(II)",
                format!(
                    "the return was filed with the whole balance paid on {}, so every shortfall still outstanding was paid that day",
                    stop.long()
                ),
            );
        }
        let rates: Vec<String> = y
            .rates
            .periods()
            .iter()
            .map(|rp| {
                format!(
                    "{} from {} to {}",
                    rp.rate(),
                    rp.from().long(),
                    rp.to().long()
                )
            })
            .collect();
        notes.rule(
            "C.R.S. §§ 39-22-605(3), (4), 39-21-110.5; DR 0204 (09/25/25), Part 3, lines 12 to 19",
            format!(
                "each shortfall is charged simple interest at Colorado's regular rate for each calendar year ({}), by the day over the days in the year, from its due date until paid or {}; \
                 the discounted rate does not apply to estimated tax (C.R.S. § 39-21-109(5)); the penalty to {} is {}",
                rates.join("; "),
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
                    short_date(unrated.add_days(-1), 0)
                ),
            );
        }
        if p.total().0 > 0 && waiver_answered.is_none() {
            notes.ask(
                PositionKind::PenaltyWaiver,
                "Are you asking the executive director to waive the penalty?",
                "The executive director can waive it for good cause for the underpayment. Retiring after 62 or becoming disabled, with reasonable cause, also counts.",
            );
        }
        if waiver == WaiverKind::Casualty {
            notes.rule(
                "C.R.S. § 39-22-605(7)(c)",
                "Colorado has no waiver for a casualty or disaster as such; it is waived only if the executive director finds the underpayment was due to good cause",
            );
        }
        if let Some(o) = colorado_waiver(f)
            && p.total().0 > 0
        {
            p = p.to_builder().waiver_outcome(o).build()?;
        }
        Ok(working::new_result(p).with(&a.notes).with(&notes).build()?)
    }

    fn explanation(&self) -> Explanation {
        colorado_explanation()
    }
}

/// Decides the waiver asked for under § 39-22-605(7)(c) and (d).
/// Retirement after 62 or disability is the federal waiver. There is no
/// casualty waiver as such, only one for good cause.
fn colorado_waiver(f: &Facts) -> Option<WaiverOutcome> {
    match f.positions().waiver() {
        None | Some(WaiverKind::None) => None,
        Some(WaiverKind::Casualty) => Some(estimated::waiver_eligible(
            "Colorado has no waiver for a casualty or disaster as such, only for good cause. The executive director decides how far you show good cause for the shortfall.",
        )),
        Some(_) => estimated::standard_waiver(f, "the executive director", 62),
    }
}

/// Written by hand and checked against research/estimated-payments/co.md.
fn colorado_explanation() -> Explanation {
    estimated::new_explanation(
        "Colorado expects estimated payments when more than $1,000 of tax will be due. That is the tax left after withholding and credits. The penalty is interest on each quarter's shortfall, at Colorado's own rate.",
    )
    .paragraph("You avoid the penalty if withholding and estimated payments reach a target. The target is the smaller of 70% of this year's tax or 100% of last year's. Above $150,000 of federal adjusted gross income last year, the 100% is 110%. The limit is $75,000 if you are married filing separately. Last year's figure needs a Colorado return for a full twelve-month year.")
    .paragraph("Pay a quarter by April 15, June 15, September 15 and the next January 15. A date on a weekend or legal holiday shifts to the next business day. Withholding counts as paid evenly across the four dates. You can instead show when it was actually withheld. An overpayment carried forward from last year counts toward April. Filing and paying in full by January 31 removes the January payment's penalty.")
    .paragraph("The penalty form looks at this year's tax less withholding. It charges nothing when that is $1,000 or less. The statute says less than $1,000, so the two differ only at exactly $1,000. The statute also excuses you if you had no Colorado tax liability last year. You must have lived in Colorado all of that year. The department can also waive the penalty for good cause. It can waive it too if you retired after 62 or became disabled. None of those three is on the form, DR 0204, so you have to ask for them.")
    .paragraph("The penalty is interest at Colorado's regular rate, set once each calendar year. That is 11% for 2026 and 10% for 2027. It runs by the day from each due date until the shortfall is paid. It stops on April 15 of the next year at the latest. A shortfall carried past December 31 changes rate on January 1. The lower discounted rate Colorado gives on other late tax doesn't apply. If you annualise uneven income federally, you can annualise for Colorado too. DR 0204 Part 4 uses 17.5%, 35%, 52.5% and 70% of the tax on annualised income.")
    .versus_federal("Colorado differs from federal where a number can change. The current-year test is 70%, not 90%. Last year's figure needs a filed Colorado return. The rate is Colorado's own, set once a year rather than every quarter. It is 11% for 2026 and 10% for 2027. You can annualise only if you annualise federally. The no-liability exception needs Colorado residence all year. The form omits it.")
    .source("C.R.S. §§ 39-22-605, 39-21-109, 39-21-110.5")
    .source("DR 0104EP, Individual Estimated Income Tax, 2026 (06/10/25)")
    .source("DR 0204, Computation of Penalty Due Based on Underpayment of Colorado Individual Estimated Tax, 2025 (09/25/25)")
    .source("CDOR, Tax Topics: Penalties and Interest")
    .build()
    .unwrap_or_default()
}
