//! Nebraska's estimated tax rules, Neb. Rev. Stat. §§ 77-2769 and 77-2790
//! and 316 Neb. Admin. Code ch. 20, as the shared [`EstimatedTax`]
//! interface.
//!
//! Nebraska follows the federal rules: four 25% installments, the lesser of
//! 90% of this year's tax or 100% of last year's, 110% above $150,000 of
//! last year's federal AGI, and payments applied to the earliest installment
//! first. It differs in the $500 threshold, an exception for first-time
//! filers, and the rate: the delinquent tax rate under § 45-104.02, 8% a
//! year, simple.
//!
//! The figure follows the 2026 Form 2210N, and the result's Divergences say
//! where the statute or the regulation reads otherwise. No amendment to
//! §§ 77-2769, 77-2790 or 45-104.02 since 2010 changed these rules
//! (research/estimated-payments/ne.md), so the form does not predate the law
//! it describes.
//!
//! Line 22a of Form 2210N charges "the number of days in 2026 on line 21"
//! and line 22b is reserved, as the 2025 form did for 2025. Line 21 counts
//! the days to April 15, 2027, and the regulation runs the penalty to the
//! return's due date; Revenue Ruling 99-26-2 keeps 8% through 2028, so every
//! day is charged at 8% whichever line it falls on.
//!
//! Not built: tax paid for a nonresident by a pass-through entity or
//! fiduciary, which REG-20-007.05 counts on the last day of the entity's
//! year; a caller passes it as a dated payment.

use std::any::Any;

use estimated::{
    Basis, Calendar, Date, Error, EstimatedTax, Explanation, Facts, Installment, Month, Payment,
    Penalty, RateSeries, Requirement, TargetOptionBuilder,
};
use money::{DOLLAR, Rate, USCents};
use working::{Divergence, Notes, PositionKind, Reading};

/// jurisdiction is Nebraska's postal code.
const JURISDICTION: estimated::Jurisdiction = estimated::NEBRASKA;

/// The facts Nebraska's rules read that no other jurisdiction does. Carry
/// them in [`Facts::state`].
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct EstimatedFacts {
    first_time: bool,
    first_time_answered: bool,
    citizen: bool,
    citizen_answered: bool,
    farmer_last: bool,
    farmer_answered: bool,
    moves: bool,
    moves_answered: bool,
}

/// `Some(v)` when the caller said, as Go's `(v, answered)`.
fn answered(v: bool, answered: bool) -> Option<bool> {
    answered.then_some(v)
}

impl EstimatedFacts {
    /// Whether this is the filer's first Nebraska income tax return, which
    /// Form 2210N excuses from the penalty, or `None` if the caller did not
    /// say.
    pub fn first_time_filer(&self) -> Option<bool> {
        answered(self.first_time, self.first_time_answered)
    }

    /// Whether the filer was a U.S. citizen or resident for all of last
    /// year, which Form 2210N's no-liability exception needs, or `None` if
    /// the caller did not say.
    pub fn prior_year_us_citizen_or_resident(&self) -> Option<bool> {
        answered(self.citizen, self.citizen_answered)
    }

    /// Whether at least two-thirds of last year's gross income was from
    /// farming, ranching or fishing, which puts this year under the
    /// farmers' rules, or `None` if the caller did not say.
    pub fn farmer_last_year(&self) -> Option<bool> {
        answered(self.farmer_last, self.farmer_answered)
    }

    /// Whether a return filed and paid on Monday, February 1, 2027 counts as
    /// filed by January 31, a Sunday, or `None` if the caller did not say.
    /// Form 2210N does not say.
    pub fn january_thirty_first_moves(&self) -> Option<bool> {
        answered(self.moves, self.moves_answered)
    }
}

/// Builds [`EstimatedFacts`].
#[derive(Clone, Debug, Default)]
pub struct EstimatedFactsBuilder {
    e: EstimatedFacts,
}

/// Starts an [`EstimatedFacts`] with nothing answered.
pub fn new_estimated_facts() -> EstimatedFactsBuilder {
    EstimatedFactsBuilder::default()
}

impl EstimatedFactsBuilder {
    /// Says whether this is the filer's first Nebraska return.
    pub fn first_time_filer(&mut self, first: bool) -> &mut Self {
        (self.e.first_time, self.e.first_time_answered) = (first, true);
        self
    }

    /// Says whether the filer was a U.S. citizen or resident all last year.
    pub fn prior_year_us_citizen_or_resident(&mut self, all: bool) -> &mut Self {
        (self.e.citizen, self.e.citizen_answered) = (all, true);
        self
    }

    /// Says whether two-thirds of last year's gross income was from
    /// farming, ranching or fishing.
    pub fn farmer_last_year(&mut self, farmer: bool) -> &mut Self {
        (self.e.farmer_last, self.e.farmer_answered) = (farmer, true);
        self
    }

    /// Says whether January 31 moves to the next business day.
    pub fn january_thirty_first_moves(&mut self, moves: bool) -> &mut Self {
        (self.e.moves, self.e.moves_answered) = (moves, true);
        self
    }

    /// Returns the facts.
    pub fn build(&self) -> Result<EstimatedFacts, Error> {
        Ok(self.e)
    }
}

/// One tax year's published figures.
#[derive(Clone, Debug)]
pub(crate) struct EstimatedYear {
    pub(crate) year: i64,

    /// Form 2210N line 6.
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

    /// The Form 2210N line 9 rule: the balance paid with a return filed by
    /// `file_and_pay_by` counts as paid on `deemed_paid`.
    pub(crate) file_and_pay_by: Date,
    pub(crate) deemed_paid: Date,

    pub(crate) stop: Date,
    pub(crate) rates: RateSeries,
}

/// Returns Nebraska's estimated tax rules for a tax year, or
/// [`Error::NoRules`] for a year this build does not carry.
pub fn estimated_tax(year: i64) -> Result<Box<dyn EstimatedTax>, Error> {
    let y = match year {
        2026 => crate::estimated_y2026::year_2026()?,
        _ => return Err(estimated::no_rules(&JURISDICTION, year)),
    };
    Ok(Box::new(NebraskaEstimated { y }))
}

struct NebraskaEstimated {
    y: EstimatedYear,
}

#[derive(Clone, Debug, Default)]
struct Assessment {
    requirement: Requirement,
    exempt: bool,
    tax_known: bool,
    dues: Vec<Date>,
    installments: Vec<USCents>,
    filed_by_jan31: bool,
    notes: Notes,
}

/// Go's `%T` of the extras, as far as it can be told from here.
fn type_name(v: &(dyn Any + Send + Sync)) -> &'static str {
    if v.is::<&str>() || v.is::<String>() {
        "string"
    } else {
        "an unknown type"
    }
}

/// The touches are the facts each divergence's readings turn on.
#[derive(Clone, Copy, Debug, Default)]
struct Touches {
    between_thresholds: bool,
    no_liability_unproved: bool,
    first_time: bool,
    late_start: bool,
}

/// The $300 REG-20-001.01 still prints, which only decides whether the
/// threshold divergence affects a household.
const REGULATION_THRESHOLD: USCents = USCents(300 * DOLLAR.0);

impl NebraskaEstimated {
    fn extras(&self, f: &Facts) -> Result<EstimatedFacts, Error> {
        match f.state() {
            None => Ok(EstimatedFacts::default()),
            Some(s) => match s.downcast_ref::<EstimatedFacts>() {
                Some(e) => Ok(*e),
                None => Err(Error::Invalid(format!(
                    "nebraskataxcalc: the facts carry {}, not nebraskataxcalc.EstimatedFacts",
                    type_name(s)
                ))),
            },
        }
    }

    fn check(&self, f: &Facts, extras: &EstimatedFacts) -> Result<(), Error> {
        if f.jurisdiction() != JURISDICTION {
            return Err(Error::Invalid(format!(
                "nebraskataxcalc: these are Nebraska's rules, and the facts are for {}",
                f.jurisdiction()
            )));
        }
        if f.tax_year() != self.y.year {
            return Err(Error::Invalid(format!(
                "nebraskataxcalc: these are the {} rules, and the facts are for {}",
                self.y.year,
                f.tax_year()
            )));
        }
        if f.farmer_or_fisher() {
            return Err(Error::with_detail(
                Error::Unsupported,
                "farmers, ranchers and fishermen pay once by January 15 at 66 2/3% or file and pay by March 1, which is not built",
            ));
        }
        if extras.farmer_last_year() == Some(true) {
            return Err(Error::with_detail(
                Error::Unsupported,
                "two-thirds of last year's gross income from farming, ranching or fishing puts this year under the farmers' rules, which are not built",
            ));
        }
        if f.elections().annualised() {
            for (i, p) in f.periods().iter().enumerate() {
                let want = self.y.period_ends.get(i).copied().unwrap_or_default();
                if p.end() != want {
                    return Err(Error::Invalid(format!(
                        "nebraskataxcalc: annualization period {} ends on {}, not {}",
                        i + 1,
                        want,
                        p.end()
                    )));
                }
            }
        }
        Ok(())
    }

    /// Where the department's forms and the statute or regulation part ways
    /// on a rule every Nebraska computation uses. They are in
    /// research/estimated-payments/ne.md and checked there against both.
    fn divergences(&self, t: Touches) -> Result<Vec<Divergence>, Error> {
        let mut out = Vec::new();
        for b in [
            working::new_divergence("The $500 threshold")
                .form(
                    "No penalty when your tax after credits less withholding is under $500.",
                    "Form 2210N (2026), line 6 and Exceptions to the Penalty",
                )
                .statute(
                    "The statute agrees at $500, and the regulation still says $300.",
                    "Neb. Rev. Stat. § 77-2769(1)(a), amended by Laws 2008, LB915; 316 Neb. Admin. Code ch. 20, REG-20-001.01 and REG-20-007.06B (March 7, 2006)",
                )
                .uses(Reading::Form)
                .affects(t.between_thresholds)
                .clone(),
            working::new_divergence("No liability last year")
                .form(
                    format!(
                        "The exception needs no Nebraska liability for a full twelve-month {}. It also needs U.S. citizenship or residence for the whole year.",
                        self.y.year - 1
                    ),
                    "Form 2210N (2026), Exceptions to the Penalty, item 1",
                )
                .statute(
                    "The regulation's exception needs only zero liability for a full 12-month year.",
                    "316 Neb. Admin. Code ch. 20, REG-20-007.06A",
                )
                .uses(Reading::Form)
                .affects(t.no_liability_unproved)
                .clone(),
            working::new_divergence("First-time filers")
                .form(
                    "A first-time filer for Nebraska income tax has no penalty.",
                    "Form 2210N (2026), Exceptions to the Penalty, item 2; 2026 Form 1040N-ES",
                )
                .statute(
                    "Neither the statute nor the regulation's list of exceptions has it.",
                    "Neb. Rev. Stat. §§ 77-2769, 77-2790; 316 Neb. Admin. Code ch. 20, REG-20-007.06",
                )
                .uses(Reading::Form)
                .affects(t.first_time)
                .clone(),
            working::new_divergence("A requirement that arises late")
                .form(
                    "Form 2210N charges 25% in every column. The 1040N-ES worksheet has its own rule for a late starter. The first payment is ½, ¾ or all of the amount.",
                    "Form 2210N (2026), line 10; 2026 Form 1040N-ES, worksheet line 19",
                )
                .statute(
                    "The required amount is divided by the number of the year's installment dates.",
                    "316 Neb. Admin. Code ch. 20, REG-20-007.01",
                )
                .uses(Reading::Form)
                .affects(t.late_start)
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
                    "Was at least 2/3 of your {last} gross income from farming, ranching or fishing?"
                ),
                "If so, Nebraska's farmers' rules apply instead.",
            );
        }

        let tax_opt = f.this_year().tax();
        let tax_known = tax_opt.is_some();
        let tax = tax_opt.unwrap_or_default();
        a.tax_known = tax_known;
        let withheld = f.withheld_total();
        let prior = f.prior_year();
        let has_prior = prior.is_some();
        let prior = prior.unwrap_or_default();
        let mut prior_figures = estimated::Figures::default();
        let mut prior_tax = USCents(0);
        if has_prior {
            prior_figures = prior.figures().unwrap_or_default();
            prior_tax = prior_figures.tax().unwrap_or_default();
        }

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
                "316 Neb. Admin. Code ch. 20, REG-20-007.01; Form 2210N (2026), line 4",
                format!(
                    "{} of this year's Nebraska tax after credits of {} is {}",
                    y.this_year_share,
                    tax,
                    this_year.amount()
                ),
            );
        }

        let mut step = y.step;
        if f.filing_status() == filing::Status::MarriedFilingSeparately {
            step = y.step_separate;
        }
        let unavailable = |why: String| -> TargetOptionBuilder {
            estimated::new_target_option(Basis::PriorYearTax, y.prior_year_share, USCents(0))
                .unavailable(why)
                .clone()
        };
        let mut ask_agi = false;
        let prior_option = if !has_prior {
            unavailable(format!("Needs your {last} Nebraska return"))
        } else if !prior.filed() {
            unavailable(format!("You didn't file a {last} Nebraska return"))
        } else if prior.months() != 12 {
            unavailable(format!(
                "Your {last} Nebraska return covered {} months, not a full year",
                prior.months()
            ))
        } else {
            let agi_opt = prior_figures.federal_agi();
            let agi = agi_opt.unwrap_or_default();
            let mut rate = y.prior_year_share;
            let mut why = format!("{last} federal AGI of {agi} is not more than {step}");
            if agi_opt.is_none() {
                rate = y.prior_year_high;
                why = format!("{last} federal AGI was not given");
                ask_agi = true;
            } else if agi > step {
                rate = y.prior_year_high;
                why = format!("{last} federal AGI of {agi} is more than {step}");
            }
            let amount = rate.of(prior_tax);
            a.notes.rule(
                "316 Neb. Admin. Code ch. 20, REG-20-007.01; Form 2210N (2026), line 7",
                format!(
                    "{why}, so the prior-year test is {rate} of the {prior_tax} tax on the {last} return, or {amount}"
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

        let no_liability = has_prior
            && prior.months() == 12
            && ((prior.filed() && prior_tax.0 == 0) || prior.not_required_to_file());
        let citizen_opt = extras.prior_year_us_citizen_or_resident();
        let citizen_answered = citizen_opt.is_some();
        let citizen = citizen_opt.unwrap_or_default();
        if no_liability && !citizen_answered {
            a.notes.ask(
                PositionKind::JurisdictionFact,
                format!("Were you a U.S. citizen or resident for all of {last}?"),
                format!(
                    "If so, having no Nebraska tax liability for {last} means no penalty for {}.",
                    y.year
                ),
            );
        }
        let first = extras.first_time_filer().unwrap_or_default();

        if first {
            let exception = estimated::new_exception("First Nebraska income tax return").build()?;
            a.exempt = true;
            rb.required(false, "Nebraska charges a first-time filer no penalty. This is your first Nebraska income tax return. No estimated payments are required.").exempt(exception);
            a.notes.rule(
                "Form 2210N (2026), Exceptions to the Penalty, item 2",
                "a first-time filer for Nebraska income tax owes no penalty",
            );
            a.notes.ask(PositionKind::UnsettledRule, "", "Nebraska's first-time filer exception is on Forms 2210N and 1040N-ES. No statute or regulation has it, and the figures follow the forms.");
        } else if no_liability && citizen_answered && citizen {
            let exception = estimated::new_exception("No Nebraska tax due last year").build()?;
            a.exempt = true;
            rb.required(false, format!("You had no Nebraska tax liability for a full twelve-month {last}. You were also a U.S. citizen or resident all year. That means no estimated payments are required for {}.", y.year)).exempt(exception);
            a.notes.rule(
                "Form 2210N (2026), Exceptions to the Penalty, item 1; 316 Neb. Admin. Code ch. 20, REG-20-007.06A",
                format!(
                    "the {last} return showed no liability for a full 12 months, so there is no penalty for {}",
                    y.year
                ),
            );
        } else if tax_known && tax - withheld < y.threshold {
            let exception =
                estimated::exception_below("Tax less withholding", tax - withheld, y.threshold)?;
            a.exempt = true;
            let mut under = format!(", under {}", y.threshold.whole());
            if tax < withheld {
                under = String::new();
            }
            rb.required(
                false,
                format!(
                    "Your Nebraska tax after credits is {}, and {} was withheld. That {}{}. So no estimated payments are needed.",
                    tax.whole(),
                    withheld.whole(),
                    estimated::leaves(tax - withheld),
                    under
                ),
            )
            .exempt(exception);
            a.notes.rule(
                "Neb. Rev. Stat. § 77-2769(1)(a); Form 2210N (2026), line 6",
                format!(
                    "{} of tax less {} withheld is {}, under {}, so there is no penalty",
                    tax,
                    withheld,
                    tax - withheld,
                    y.threshold
                ),
            );
        } else if !tax_known && !prior_opt.available() {
            rb.required(
                false,
                format!(
                    "Your target needs your {} Nebraska tax or a full-year {last} Nebraska return. There's no target yet.",
                    y.year
                ),
            )
            .no_target();
        } else if !tax_known && withheld >= target {
            rb.required(
                false,
                format!(
                    "Your withholding of {} already reaches {} of your {last} tax, {}. No estimated payments are needed, whatever your {} tax turns out to be.",
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
                    "Your target is {} of your {last} tax, {}. That keeps you clear of a penalty whatever your {} tax turns out to be. After {} of withholding, you need {} of estimated payments. A quarter is due by each date.",
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
                    "Your Nebraska withholding of {} covers the {} needed for the year. No estimated payments are needed.",
                    withheld.whole(),
                    target.whole()
                ),
            )
            .choose(chosen);
        } else {
            rb.required(
                true,
                format!(
                    "Your Nebraska tax after credits is {}, and {} was withheld. That leaves {}, which is {} or more. You need {} of estimated payments, a quarter by each due date.",
                    tax.whole(),
                    withheld.whole(),
                    (tax - withheld).whole(),
                    y.threshold.whole(),
                    (target - withheld).whole()
                ),
            )
            .choose(chosen);
        }
        a.requirement = rb.build()?;
        // Under an exception none of these answers can change the target.
        if !a.exempt {
            if !has_prior {
                a.notes.ask(
                    PositionKind::PriorYearReturn,
                    format!("Did you file a {last} Nebraska return, and what was its tax?"),
                    format!(
                        "With it, 100% of your {last} tax could be a lower target than 90% of this year's."
                    ),
                );
            }
            let (high_share, low_share) = (y.prior_year_high, y.prior_year_share);
            if ask_agi {
                a.notes.ask(
                    PositionKind::AdjustedGrossIncome,
                    format!("What was your {last} federal adjusted gross income?"),
                    format!(
                        "Until then, the target is {high_share} of your {last} tax. With AGI of {} or less, it would be {low_share}.",
                        step.whole()
                    ),
                );
            }
            if !tax_known {
                if prior_opt.available() {
                    a.notes.ask(
                        PositionKind::ThisYearTax,
                        format!("What do you expect your {} Nebraska tax to be?", y.year),
                        format!(
                            "With it, 90% of it could be a lower target than your {last} tax. It also decides whether the {} threshold excuses you.",
                            y.threshold.whole()
                        ),
                    );
                } else {
                    a.notes.ask(
                        PositionKind::ThisYearTax,
                        format!("What do you expect your {} Nebraska tax to be?", y.year),
                        format!(
                            "Without it there's no target, as there's no full-year {last} Nebraska return."
                        ),
                    );
                }
            }
        }

        let cumulative = estimated::cumulative_shares(a.requirement.target(), &y.shares)?;
        a.installments = estimated::increments(&cumulative);
        a.notes.rule(
            "Neb. Rev. Stat. § 77-2769(5); Form 2210N (2026), lines 9 and 10",
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
            let taxes: Vec<USCents> = f.periods().iter().map(|p| p.annualised_tax()).collect();
            let ann = estimated::annualised(&taxes, &y.annualised, &a.installments)?;
            a.notes.rule(
                "Form 2210N (2026), Nebraska Income Tax on Annualized Income; Form 2210 Schedule AI",
                format!(
                    "under the annualized method, figured as on the federal schedule Form 2210N points to, the installments are {}, {}, {} and {}",
                    ann[0], ann[1], ann[2], ann[3]
                ),
            );
            a.installments = ann;
        }

        // The line 9 rule. January 31, 2027 is a Sunday, and Form 2210N does
        // not say whether the day moves.
        let mut file_by = y.file_and_pay_by;
        let moves_opt = extras.january_thirty_first_moves();
        let moves_answered = moves_opt.is_some();
        if moves_opt == Some(true) {
            file_by = y.calendar.next_business_day(file_by);
        }
        let filed = f.return_filed();
        if !filed.is_zero() && !a.exempt {
            if !moves_answered
                && filed.after(file_by)
                && !filed.after(y.calendar.next_business_day(file_by))
            {
                a.notes.ask(
                    PositionKind::UnsettledRule,
                    "",
                    format!(
                        "{} is a Sunday, and Form 2210N doesn't say if the date moves. The figures treat a return filed and paid on {} as late.",
                        file_by.long(),
                        filed.long()
                    ),
                );
            }
            if !filed.after(file_by) {
                a.filed_by_jan31 = true;
                a.notes.rule(
                    "Form 2210N (2026), instructions for line 9 and Exceptions to the Penalty, Note",
                    format!(
                        "the return was filed and paid on {}, by {}, so the balance counts as paid on {}",
                        filed.long(),
                        file_by.long(),
                        y.deemed_paid.long()
                    ),
                );
            }
        }
        if a.exempt {
            a.installments = vec![USCents(0); a.dues.len()];
        }

        let mut t = Touches {
            between_thresholds: !first
                && !(no_liability && citizen_answered && citizen)
                && tax_known
                && tax - withheld >= REGULATION_THRESHOLD
                && tax - withheld < y.threshold,
            no_liability_unproved: no_liability && !first && !(citizen_answered && citizen),
            first_time: first,
            late_start: false,
        };
        let met = f.requirement_first_met();
        if !a.exempt && !met.is_zero() && met.after(a.dues[0]) {
            t.late_start = true;
        }
        let divergences = self.divergences(t)?;
        a.notes.diverges(&divergences);
        Ok(a)
    }

    /// Withholding and payments, earliest first.
    fn credits(&self, f: &Facts, a: &Assessment) -> Result<(Vec<Payment>, Notes), Error> {
        let mut notes = Notes::default();
        let mut credits = if f.elections().withholding_as_withheld() {
            let c = estimated::credits_as_withheld(f, &a.dues);
            notes.rule(
                "316 Neb. Admin. Code ch. 20, REG-20-007.04; Form 2210N (2026), line 11",
                format!(
                    "by election, the {} withheld is credited on the dates it was withheld",
                    f.withheld_total()
                ),
            );
            c
        } else {
            let c = estimated::credits(f, &a.dues, &self.y.shares);
            notes.rule(
                "316 Neb. Admin. Code ch. 20, REG-20-007.04; Form 2210N (2026), line 11",
                format!(
                    "one-fourth of the {} withheld is treated as paid on each due date, whenever it was actually withheld",
                    f.withheld_total()
                ),
            );
            c
        }?;
        credits.sort_by(|a, b| a.on().compare(b.on()).cmp(&0));
        Ok((credits, notes))
    }

    /// The installments, with the credits applied earliest first.
    fn schedule_of(&self, a: &Assessment, credits: &[Payment]) -> Result<Vec<Installment>, Error> {
        let ledger = estimated::apply(&a.dues, &a.installments, credits)?;
        let mut out = Vec::with_capacity(ledger.len());
        for (i, l) in ledger.iter().enumerate() {
            let mut b = estimated::new_installment(i as i64 + 1, l.due());
            b.needed_by(l.needed_by())
                .credited_by(l.credited_by())
                .shortfall(l.shortfall());
            if a.filed_by_jan31 && i == 3 {
                b.excused();
            }
            out.push(b.build()?);
        }
        Ok(out)
    }
}

impl EstimatedTax for NebraskaEstimated {
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
                    "with no {} Nebraska return to measure against, there is no target to schedule",
                    self.y.year - 1
                ),
            ));
        }
        let (credits, notes) = self.credits(f, &a)?;
        let out = self.schedule_of(&a, &credits)?;
        Ok(working::new_result(out)
            .with(&a.notes)
            .with(&notes)
            .build()?)
    }

    fn penalty(&self, f: &Facts, as_of: Date) -> Result<working::Result<Penalty>, Error> {
        if as_of.is_zero() {
            return Err(Error::Invalid(
                "nebraskataxcalc: a penalty needs the date to figure it to".into(),
            ));
        }
        let a = self.assess(f)?;
        if !a.requirement.target_known() {
            return Err(Error::with_detail(
                Error::NeedsThisYearTax,
                format!(
                    "with no {} Nebraska return to measure against, there is no target to figure a penalty from",
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
        if a.filed_by_jan31 && y.deemed_paid.before(stop) {
            stop = y.deemed_paid;
        }
        let waiver_opt = f.positions().waiver();
        let waiver_answered = waiver_opt.is_some();
        let waiver = waiver_opt.unwrap_or_default();
        let (credits, mut notes) = self.credits(f, &a)?;
        let schedule = self.schedule_of(&a, &credits)?;
        if a.exempt {
            let p = estimated::new_penalty(stop)
                .waiver(waiver)
                .because(a.requirement.clone(), &schedule)
                .build()?;
            return Ok(working::new_result(p).with(&a.notes).build()?);
        }

        // Form 2210N lines 12 to 22: each column's underpayment, this one's
        // and any left from earlier, from its due date until paid or the
        // next due date, and the last column to April 15.
        let mut pb = estimated::new_penalty(stop);
        pb.waiver(waiver).because(a.requirement.clone(), &schedule);
        let mut unrated = Date::default();
        let mut required = Vec::with_capacity(a.dues.len());
        let mut sum = USCents(0);
        for k in 0..a.dues.len() {
            sum += a.installments[k];
            required.push(sum);
        }
        let cols = estimated::columns(&a.dues, &required, &credits, y.stop, stop)?;
        for col in cols {
            if col.under().0 <= 0 {
                continue;
            }
            let mut lb = estimated::new_penalty_line(col.number(), col.due(), col.under());
            for piece in col.pieces() {
                let to = piece.paid_on().unwrap_or(col.end());
                let (parts, gap) =
                    estimated::simple_daily(piece.amount(), col.due(), to, &y.rates)?;
                if !gap.is_zero() && (unrated.is_zero() || gap.before(unrated)) {
                    unrated = gap;
                }
                for p in parts {
                    lb.part(p);
                }
            }
            let line = lb.days(col.through().days_since(col.due())).build()?;
            pb.line(line);
        }
        if !a.tax_known {
            pb.upper_bound();
        }
        let mut p = pb.build()?;
        if !a.tax_known {
            notes.rule(
                "Form 2210N (2026), line 8",
                format!(
                    "this year's Nebraska tax was not given, so the penalty is figured against {} of the {} tax alone; the required payment is the lesser of that and 90% of this year's tax, so {} is the most the penalty can be, and it is less if this year's tax turns out lower",
                    a.requirement.options()[1].percent(),
                    y.year - 1,
                    p.total()
                ),
            );
        }
        notes.rule(
            "316 Neb. Admin. Code ch. 20, REG-20-007.03; Neb. Rev. Stat. § 45-104.02; Form 2210N (2026), lines 19 to 23",
            format!(
                "each column's underpayment is charged 8% a year, simple, by the day ÷ 365, from its due date until paid or the next due date, and the last column to {}; the penalty to {} is {}",
                y.stop.long(),
                stop.long(),
                p.total()
            ),
        );
        if !unrated.is_zero() {
            p = p.to_builder().rated_through(unrated.add_days(-1)).build()?;
        }
        if !unrated.is_zero() {
            notes.ask(
                PositionKind::UnpublishedRate,
                "",
                format!(
                    "The rate for the days after {} isn't published yet.",
                    unrated.add_days(-1).long()
                ),
            );
        }
        // REG-20-007.03 and Form 2210N grant the federal pair of waivers.
        if let Some(o) = estimated::standard_waiver(f, "Nebraska's Department of Revenue", 62)
            && p.total().0 > 0
        {
            p = p.to_builder().waiver_outcome(o).build()?;
        }
        if p.total().0 > 0 && !waiver_answered {
            notes.ask(
                PositionKind::PenaltyWaiver,
                "Do you want a waiver for an unusual event, retiring after 62 or disability?",
                "A casualty or disaster is an unusual event, and all three need reasonable cause. Until Nebraska waives it, the figures include the full penalty.",
            );
        }
        Ok(working::new_result(p).with(&a.notes).with(&notes).build()?)
    }

    /// Written by hand and checked against research/estimated-payments/ne.md.
    fn explanation(&self) -> Explanation {
        estimated::new_explanation(
            "Nebraska expects estimated payments once tax after credits exceeds withholding. The excess must be $500 or more. The penalty is 8% a year, simple interest, on each quarter's shortfall.",
        )
        .paragraph("Your target is the lesser of two figures. They are 90% of this year's Nebraska tax and 100% of last year's. You are clear if withholding and estimated payments reach the target. A quarter is due by April 15, June 15, September 15 and January 15. As federally, 100% becomes 110% if last year's federal AGI was over $150,000. That limit is $75,000 if married filing separately. Last year's figure needs a full-year return. Withholding counts as paid a quarter on each date. That holds unless you show when it was withheld.")
        .paragraph("There is no penalty if this year's tax less withholding is under $500. There is none if you had no Nebraska tax due on last year's full-year return. That case also needs U.S. citizenship or residence all last year. Your first Nebraska return has no penalty either. Filing and paying by January 31 counts the balance as paid on January 15.")
        .paragraph("The penalty runs daily from each due date until the shortfall is paid. It stops on April 15 of the next year. Each payment goes first to the oldest shortfall. The rate is Nebraska's delinquent tax rate, set every other year. It is 8% for 2026 and 2027. The department can waive the penalty for a casualty or disaster. It can also waive it for retiring after 62 or disability with reasonable cause.")
        .versus_federal("Nebraska's threshold is $500 rather than $1,000. A first-time Nebraska filer has no penalty. The rate is a flat 8% a year, set every other year, not a quarterly rate.")
        .source("Neb. Rev. Stat. §§ 77-2769, 77-2790, 45-104.02")
        .source("316 Neb. Admin. Code ch. 20, REG-20-001 and REG-20-007")
        .source("Form 2210N, Individual Underpayment of Estimated Tax, 2026, and its instructions")
        .source("2026 Form 1040N-ES")
        .source("Revenue Ruling 99-26-2, Interest Rate Assessed on State Taxes (August 7, 2026)")
        .build()
        .unwrap_or_default()
    }
}

/// A shorthand for the year files.
pub(crate) fn date(y: i64, m: Month, d: i64) -> Date {
    estimated::must_date(y, m, d)
}
