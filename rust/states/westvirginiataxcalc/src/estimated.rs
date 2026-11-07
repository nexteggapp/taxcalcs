//! West Virginia's estimated tax rules, W. Va. Code §§ 11-21-55, 11-21-56,
//! 11-10-17a and 11-10-18a, as the shared [`EstimatedTax`] interface.
//!
//! The figure follows the Tax Division's penalty form, Form IT-210 in the
//! 2025 IT-140 booklet, which is the latest published, and the IT-140ESI
//! (Sept 2023) for who must pay. The result's Divergences say where the Code
//! reads otherwise. The bill histories end in 1998 for § 11-10-18a, 1990 for
//! § 11-21-55, 1961 for § 11-21-56 and 2015 for § 11-10-17a, so neither form
//! predates the law it describes.

use std::any::Any;

use estimated::{
    Basis, Calendar, Date, Error, EstimatedTax, Explanation, FactorSeries, Facts, Installment,
    LedgerEntry, Penalty, Requirement, TargetOptionBuilder, WaiverKind, WaiverOutcome,
};
use money::{Rate, USCents, percent};
use working::{Divergence, Notes, PositionKind, Reading};

const JURISDICTION: estimated::Jurisdiction = estimated::WEST_VIRGINIA;

/// The facts West Virginia's rules read that no other jurisdiction does.
/// Carry them in [`Facts::state`].
///
/// For West Virginia, [`estimated::Figures::tax`] is the tax after credits,
/// Form IT-210 Part I line 3: Form IT-140 line 8 less lines 9 and 17 to 21.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct EstimatedFacts {
    prior_us_resident: bool,
    prior_us_resident_answered: bool,
}

impl EstimatedFacts {
    /// Whether the filer was a citizen or resident of the United States
    /// throughout last year, or `None` if the caller did not say. The
    /// exception for a filer who had no West Virginia liability last year
    /// needs it (W. Va. Code § 11-10-18a(e)(2)(C)). It is United States
    /// residence, not West Virginia residence, so it is not
    /// [`estimated::PriorReturn::full_year_resident`].
    pub fn prior_year_us_citizen_or_resident(&self) -> Option<bool> {
        self.prior_us_resident_answered
            .then_some(self.prior_us_resident)
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
    /// Says whether the filer was a US citizen or resident all last year.
    pub fn prior_year_us_citizen_or_resident(&mut self, yes: bool) -> &mut Self {
        (self.e.prior_us_resident, self.e.prior_us_resident_answered) = (yes, true);
        self
    }

    /// Has nothing to refuse: every combination of answers is possible.
    pub fn build(&self) -> Result<EstimatedFacts, Error> {
        Ok(self.e)
    }
}

/// One tax year's published figures.
#[derive(Clone, Debug)]
pub(crate) struct EstimatedYear {
    pub(crate) year: i64,

    /// The tax after credits less withholding below which no penalty
    /// applies, IT-210 Part I line 5. `small_share` is the IT-140ESI's 10%
    /// of this year's tax below which no payments are required.
    pub(crate) threshold: USCents,
    pub(crate) small_share: Rate,

    /// IT-210 Part I lines 6 and 7.
    pub(crate) this_year_share: Rate,
    pub(crate) prior_year_share: Rate,

    /// This year's tax above which a zero tax last year no longer gives a
    /// target of zero, IT-210 Part I line 8.
    pub(crate) no_liability_limit: USCents,

    pub(crate) dues: Vec<Date>,
    pub(crate) calendar: Calendar,

    /// The day after which a requirement first met gets fewer installments
    /// under § 11-21-55(d), which IT-210 does not apply.
    pub(crate) late_start_after: Date,

    /// The last day of January, § 11-10-18a(i), before it is moved for a
    /// weekend or holiday.
    pub(crate) file_and_pay_by: Date,

    pub(crate) stop: Date,
    pub(crate) period_ends: Vec<Date>,

    /// IT-210 Part II line 11: the share of each period's annualised tax due
    /// by its date, cumulatively.
    pub(crate) applicable: Vec<Rate>,

    /// The factor IT-210 prints for each calendar year.
    pub(crate) daily: FactorSeries,
}

/// IT-210 Part IV line 1: line 8 of Part I divided by four. Withholding is
/// spread across the dates in the same shares.
fn quarters() -> [Rate; 4] {
    [percent(25), percent(25), percent(25), percent(25)]
}

/// Returns West Virginia's estimated tax rules for a tax year, or
/// [`Error::NoRules`] for a year this build does not carry.
pub fn estimated_tax(year: i64) -> Result<Box<dyn EstimatedTax>, Error> {
    let y = match year {
        2026 => crate::estimated_y2026::year_2026()?,
        _ => return Err(estimated::no_rules(&JURISDICTION, year)),
    };
    Ok(Box::new(WestVirginiaEstimated { y }))
}

struct WestVirginiaEstimated {
    y: EstimatedYear,
}

/// What the three questions share.
#[derive(Clone, Debug, Default)]
struct Assessment {
    requirement: Requirement,
    exempt: bool,
    tax_known: bool,
    dues: Vec<Date>,

    /// Each installment IT-210 Part IV line 1 asks for: a quarter of Part I
    /// line 8, or Part II line 19 under the annualised method.
    required: Vec<USCents>,

    excused_january: bool,

    /// A target from last year's tax.
    prior_chosen: bool,
    /// A zero last year that IT-210 line 8 accepts and the Code's exception
    /// does not.
    line_eight_only: bool,

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

/// A question asked only when no exception sets the target.
struct Ask {
    kind: PositionKind,
    question: String,
    effect: String,
}

impl WestVirginiaEstimated {
    fn extras(&self, f: &Facts) -> Result<EstimatedFacts, Error> {
        match f.state() {
            None => Ok(EstimatedFacts::default()),
            Some(s) => match s.downcast_ref::<EstimatedFacts>() {
                Some(e) => Ok(*e),
                None => Err(Error::Invalid(format!(
                    "westvirginiataxcalc: the facts carry {}, not westvirginiataxcalc.EstimatedFacts",
                    type_name(s)
                ))),
            },
        }
    }

    fn check(&self, f: &Facts) -> Result<(), Error> {
        if f.jurisdiction() != JURISDICTION {
            return Err(Error::Invalid(format!(
                "westvirginiataxcalc: these are West Virginia's rules, and the facts are for {}",
                f.jurisdiction()
            )));
        }
        if f.tax_year() != self.y.year {
            return Err(Error::Invalid(format!(
                "westvirginiataxcalc: these are the {} rules, and the facts are for {}",
                self.y.year,
                f.tax_year()
            )));
        }
        if i64::from(f.farm_share().0) * 3 >= 20_000 {
            return Err(Error::with_detail(
                Error::Unsupported,
                "a farmer with two-thirds of gross income from farming makes one payment by January 15 at 66 2/3%, which is not built",
            ));
        }
        if f.elections().annualised() {
            for (i, p) in f.periods().iter().enumerate() {
                let want = self.y.period_ends.get(i).copied().unwrap_or_default();
                if p.end() != want {
                    return Err(Error::Invalid(format!(
                        "westvirginiataxcalc: IT-210 Part II period {} ends on {}, not {}",
                        i + 1,
                        want,
                        p.end()
                    )));
                }
            }
        }
        Ok(())
    }

    /// Where the Tax Division's documents and the Code part ways on a rule
    /// every West Virginia computation uses. Each affects the household only
    /// when its facts reach the rule.
    fn divergences(&self, f: &Facts, a: &Assessment) -> Result<Vec<Divergence>, Error> {
        let y = &self.y;
        let scheduled = a.requirement.target_known() && !a.exempt;
        let mut short = false;
        if scheduled {
            let (ledger, _) = self.ledger(f, a)?;
            for l in &ledger {
                short = short || l.shortfall().0 > 0;
            }
        }
        let (met, filed) = (f.requirement_first_met(), f.return_filed());
        let pay_by = y.calendar.next_business_day(y.file_and_pay_by);
        let mut out = Vec::new();
        for b in [
            // The Code's test reads income other than wages, which the facts
            // do not split out; the penalty is the same under both.
            working::new_divergence("Who must make estimated payments")
                .form("Payments are required when tax less West Virginia withholding is $600 or more. The exception is a balance under 10% of this year's tax.",
                    "IT-140ESI (Sept 2023), Who must make estimated tax payments?; 2025 IT-140 booklet, Credit for Estimated Tax")
                .statute("The test is your West Virginia income other than withheld wages. Payments are required if it's expected to exceed $400 plus personal exemptions. The law has no 10% test, and $600 is only the penalty exception.",
                    "W. Va. Code § 11-21-55(a); § 11-10-18a(e)(1)")
                .uses(Reading::Form)
                .affects(a.requirement.required())
                .clone(),
            working::new_divergence("The prior-year test")
                .form("The target is the smaller of 90% of this year's tax and 100% of last year's. Each payment is a quarter of it. A quarter that misses the prior-year amount is charged only on what it missed.",
                    "IT-210 (2025), Part I lines 6 to 8 and Part IV line 1")
                .statute("The shortfall is measured against a quarter of 90% of this year's tax. Last year's tax excuses a quarter only if payments to date reach its share.",
                    "W. Va. Code § 11-10-18a(b)(1) and (d)(1)")
                .uses(Reading::Form)
                .affects(a.prior_chosen && short)
                .clone(),
            working::new_divergence("No liability last year")
                .form("A zero tax last year means no payments while this year's tax is $5,000 or less. There's no condition of US citizenship or residence.",
                    "IT-210 (2025), Part I line 8")
                .statute("The penalty is excused only if last year's West Virginia tax was zero. Last year must be 12 months, spent as a US citizen or resident. This year's tax must be under $5,000. Otherwise 90% of this year's tax applies. The booklet says the same.",
                    "W. Va. Code § 11-10-18a(d)(1) and (e)(2); 2025 IT-140 booklet, Exceptions to the Penalty")
                .uses(Reading::Form)
                .affects(a.line_eight_only)
                .clone(),
            working::new_divergence("A requirement first met after April 1")
                .form("A quarter of the year's amount is due by every date for every filer.",
                    "IT-210 (2025), Part II line 15 and Part IV line 1")
                .statute("First meeting the requirement after April 1 gives fewer, equal installments. That's three from June 15, two from September 15, or all on January 15. The penalty divides 90% of the tax by that number.",
                    "W. Va. Code §§ 11-21-55(d), 11-21-56(a); § 11-10-18a(b)(1)")
                .uses(Reading::Form)
                .affects(scheduled && !met.is_zero() && met.after(y.late_start_after))
                .clone(),
            working::new_divergence("A return filed and paid by January 31")
                .form("The booklet excuses the January payment if you file and pay by February 1. The IT-140ESI says by January 31.",
                    "2025 IT-140 booklet, Exceptions to the Penalty; IT-210 (2025), Part IV line 2 instructions; IT-140ESI (Sept 2023), For Your Information")
                .statute("Filing and paying in full by the end of January excuses the January payment. A weekend or holiday moves that date to the next business day. Jan 31, 2027 is a Sunday, so the date is Monday, Feb 1, 2027.",
                    "W. Va. Code § 11-10-18a(i); § 2-2-1(e)")
                .uses(Reading::Statute)
                .affects(scheduled && filed == pay_by && pay_by != y.file_and_pay_by)
                .clone(),
            working::new_divergence("The daily interest factor")
                .form("Each day is charged at a printed factor of 0.000315. That's 11.5% divided by 365, rounded to six places.",
                    "IT-210 (2025), Part IV Section B and line 11 instructions")
                .statute("The charge is the yearly rate on the shortfall for its period, 11.5% for 2026. That's 0.00031507 a day.",
                    "W. Va. Code §§ 11-10-17a(c), 11-10-18a(a); TSD-365")
                .uses(Reading::Form)
                .affects(short)
                .clone(),
            // The IT-140ESI's 8% floor is below the 2026 rate, and no
            // six-month rate is published, so both readings give the same
            // rate.
            working::new_divergence("How the rate is set")
                .form("The IT-140ESI says the rate is set every six months and can't be less than 8%.",
                    "IT-140ESI (Sept 2023), What if I don't pay enough estimated tax?")
                .statute("Since 2017 the rate is set once a year, from January 1. It's the adjusted prime rate plus three points. The 8% floor applies only to years before 2017.",
                    "W. Va. Code § 11-10-17a(a) and (c)")
                .uses(Reading::Statute)
                .affects(false)
                .clone(),
            working::new_divergence("Withholding on the dates withheld")
                .form("One box in Part I puts all withholding on the dates it was withheld. Otherwise a quarter of it counts on each date.",
                    "IT-210 (2025), Part I and Part IV line 2 instructions")
                .statute("Wage withholding and other withholding may each be put on their actual dates. Either may instead be spread evenly, separately from the other.",
                    "W. Va. Code § 11-10-18a(g)")
                .uses(Reading::Form)
                .affects(scheduled && f.withheld_total().0 > 0)
                .clone(),
        ] {
            out.push(b.build()?);
        }
        Ok(out)
    }

    /// Answers the three questions and adds the divergences.
    fn assess(&self, f: &Facts) -> Result<Assessment, Error> {
        let mut a = self.figure(f)?;
        let ds = self.divergences(f, &a)?;
        a.notes.diverges(&ds);
        Ok(a)
    }

    /// The requirement and the required installments.
    fn figure(&self, f: &Facts) -> Result<Assessment, Error> {
        self.check(f)?;
        let extras = self.extras(f)?;
        let y = &self.y;
        let mut a = Assessment {
            dues: estimated::move_dates(&y.calendar, &y.dues),
            ..Assessment::default()
        };
        let last = y.year - 1;
        let tax_opt = f.this_year().tax();
        let tax_known = tax_opt.is_some();
        let tax = tax_opt.unwrap_or_default();
        a.tax_known = tax_known;
        let withheld = f.withheld_total();

        // IT-210 Part I line 6: 90% of this year's tax after credits.
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
                "W. Va. Code § 11-10-18a(b)(1); IT-210 (2025), Part I lines 3 and 6",
                format!(
                    "{} of this year's West Virginia tax after credits of {} is {}",
                    y.this_year_share,
                    tax,
                    this_year.amount()
                ),
            );
        }

        // IT-210 Part I lines 7 and 8: last year's tax after credits, for a
        // 12-month return, and the rule for a zero.
        let prior_ret = f.prior_year();
        let has_prior = prior_ret.is_some();
        let prior = prior_ret.unwrap_or_default();
        let mut prior_tax = USCents(0);
        if has_prior && let Some(pf) = prior.figures() {
            prior_tax = pf.tax().unwrap_or_default();
        }
        let unavailable = |why: String| -> TargetOptionBuilder {
            estimated::new_target_option(Basis::PriorYearTax, y.prior_year_share, USCents(0))
                .unavailable(why)
                .clone()
        };
        let mut no_liability = false;
        // A sentence for a rule that could not be applied without this
        // year's tax.
        let mut tax_needed_for = String::new();
        let mut asks: Vec<Ask> = Vec::new();
        let prior_option = if !has_prior {
            asks.push(Ask {
                kind: PositionKind::PriorYearReturn,
                question: format!(
                    "What was the tax after credits on your {last} West Virginia return?"
                ),
                effect: "With it, 100% of last year's tax could be your target.".into(),
            });
            unavailable(format!("Needs your {last} West Virginia return"))
        } else if prior.months() != 12 {
            unavailable(format!(
                "Your {last} West Virginia return covered {} months, not a full 12",
                prior.months()
            ))
        } else if prior.not_required_to_file() {
            // IT-210 line 7 has no figure without a return. The booklet's
            // Exceptions to the Penalty, which agrees with § 11-10-18a(e)(2),
            // covers a filer who was not required to file.
            let us_opt = extras.prior_year_us_citizen_or_resident();
            if !tax_known {
                tax_needed_for = format!(
                    "With no West Virginia tax for {last}, you're excused if it's under {}.",
                    y.no_liability_limit.whole()
                );
            } else if tax >= y.no_liability_limit {
            } else if us_opt.is_none() {
                asks.push(Ask {
                    kind: PositionKind::JurisdictionFact,
                    question: format!("Were you a US citizen or resident all of {last}?"),
                    effect: format!(
                        "If so, a zero tax that year and under {} this year excuses the penalty.",
                        y.no_liability_limit.whole()
                    ),
                });
            } else if us_opt == Some(true) {
                no_liability = true;
                a.notes.rule(
                    "W. Va. Code § 11-10-18a(e)(2); 2025 IT-140 booklet, Exceptions to the Penalty",
                    format!(
                        "no return was required for the 12 months of {last}, spent as a United States citizen or resident, and this year's tax of {} is less than {}, so no penalty can arise",
                        tax, y.no_liability_limit
                    ),
                );
            }
            unavailable(format!("You didn't file a {last} West Virginia return"))
        } else if !prior.filed() {
            unavailable(format!("You didn't file a {last} West Virginia return"))
        } else if prior_tax.0 == 0 && !tax_known {
            tax_needed_for = format!(
                "Your zero {last} tax counts only if it's {} or less.",
                y.no_liability_limit.whole()
            );
            unavailable(format!(
                "A zero {last} West Virginia tax counts only if this year's tax is {} or less",
                y.no_liability_limit.whole()
            ))
        } else if prior_tax.0 == 0 && tax > y.no_liability_limit {
            unavailable(format!(
                "A zero {last} West Virginia tax doesn't count if this year's tax is over {}",
                y.no_liability_limit.whole()
            ))
        } else if prior_tax.0 == 0 {
            no_liability = true;
            let us = extras.prior_year_us_citizen_or_resident();
            a.line_eight_only = !(us == Some(true) && tax < y.no_liability_limit);
            a.notes.rule(
                "IT-210 (2025), Part I lines 7 and 8",
                format!(
                    "the {last} return showed no tax, and this year's tax of {} is not more than {}, so line 8 is zero and no penalty can arise",
                    tax, y.no_liability_limit
                ),
            );
            estimated::new_target_option(Basis::PriorYearTax, y.prior_year_share, USCents(0))
        } else {
            let amount = y.prior_year_share.of(prior_tax);
            a.notes.rule(
                "W. Va. Code § 11-10-18a(d)(1); IT-210 (2025), Part I line 7",
                format!(
                    "{} of the {} tax after credits on the 12-month {last} return is {}",
                    y.prior_year_share, prior_tax, amount
                ),
            );
            estimated::new_target_option(Basis::PriorYearTax, y.prior_year_share, amount)
        };
        let prior_opt = prior_option.build()?;

        let mut rb = estimated::new_requirement();
        rb.option(this_year.clone()).option(prior_opt.clone());
        let (mut chosen, mut target) = (Basis::ThisYearTax, this_year.amount());
        if prior_opt.available() && (!tax_known || prior_opt.amount() < target) {
            (chosen, target) = (Basis::PriorYearTax, prior_opt.amount());
        }
        if !tax_known {
            let mut effect = format!(
                "With it, 90% of this year's tax could be your target and the {} test used.",
                y.threshold.whole()
            );
            if !tax_needed_for.is_empty() {
                effect.push(' ');
                effect.push_str(&tax_needed_for);
            }
            asks.push(Ask {
                kind: PositionKind::ThisYearTax,
                question: format!(
                    "What do you expect your {} West Virginia tax after credits to be?",
                    y.year
                ),
                effect,
            });
        }

        let owed = tax - withheld;
        if tax_known && owed < y.threshold {
            a.exempt = true;
            let under = estimated::exception_below("Tax less withholding", owed, y.threshold)?;
            let mut why = format!(
                "Your West Virginia tax of {} less {} withheld leaves {}. That's under {}, so no estimated payments are needed.",
                tax.whole(),
                withheld.whole(),
                owed.whole(),
                y.threshold.whole()
            );
            if owed.0 < 0 {
                why = format!(
                    "Withholding of {} covers your West Virginia tax of {}. That {}, so no estimated payments are needed.",
                    withheld.whole(),
                    tax.whole(),
                    estimated::leaves(owed)
                );
            } else if owed.0 == 0 {
                why = format!(
                    "Your withholding of {} covers your West Virginia tax of {}. No estimated payments are needed.",
                    withheld.whole(),
                    tax.whole()
                );
            } else if owed.whole() == y.threshold.whole() {
                why = format!(
                    "Your West Virginia tax less withholding is just under {}. No estimated payments are needed.",
                    y.threshold.whole()
                );
            }
            rb.required(false, why).exempt(under);
            a.notes.rule(
                "W. Va. Code § 11-10-18a(e)(1); IT-210 (2025), Part I line 5",
                format!(
                    "{} of tax less {} of withholding is {}, under {}, so there is no penalty",
                    tax, withheld, owed, y.threshold
                ),
            );
        } else if no_liability {
            a.exempt = true;
            let none = estimated::new_exception("No West Virginia tax due last year").build()?;
            rb.required(
                false,
                format!(
                    "You had no West Virginia tax for {last}, and your {} tax is {}. No estimated payments are needed, and no penalty can arise.",
                    y.year,
                    tax.whole()
                ),
            )
            .exempt(none);
        } else if !tax_known && !prior_opt.available() {
            rb.required(
                false,
                format!(
                    "Your {last} return doesn't give a target. Your target is 90% of your {} West Virginia tax, unless an exception applies.",
                    y.year
                ),
            )
            .no_target();
        } else if !tax_known && withheld >= target {
            rb.required(
                false,
                format!(
                    "Your withholding of {} reaches {} of your {last} tax, {}. No estimated payments are needed, whatever your {} tax is.",
                    withheld.whole(),
                    prior_opt.percent(),
                    target.whole(),
                    y.year
                ),
            )
            .choose(chosen);
        } else if !tax_known {
            let mut why = format!(
                "Your target is {} of your {last} West Virginia tax, {}. That keeps you clear of a penalty whatever your {} tax turns out to be. ",
                prior_opt.percent(),
                target.whole(),
                y.year
            );
            if withheld.0 > 0 {
                why.push_str(&format!(
                    "After {} of withholding, that's {} of estimated payments. Pay a quarter by each date.",
                    withheld.whole(),
                    (target - withheld).whole()
                ));
            } else {
                why.push_str("Pay it in estimated payments, a quarter by each date.");
            }
            rb.required(true, why).choose(chosen);
        } else if withheld >= target && owed < y.small_share.of(tax) {
            let mut why = format!(
                "Your West Virginia withholding of {} leaves {} of tax. That's less than {} of your {} tax, so no estimated payments are needed. Withholding already reaches the {} required.",
                withheld.whole(),
                owed.whole(),
                y.small_share,
                tax.whole(),
                target.whole()
            );
            if owed.whole() == y.small_share.of(tax).whole() {
                why = format!(
                    "Your West Virginia withholding leaves just less than {} of your {} tax. No estimated payments are needed. Withholding already reaches the {} required.",
                    y.small_share,
                    tax.whole(),
                    target.whole()
                );
            }
            rb.required(false, why).choose(chosen);
            a.notes.rule(
                "IT-140ESI (Sept 2023), Who must make estimated tax payments?",
                format!(
                    "{} of tax after withholding is less than {} of the {} tax, so no payments are required",
                    owed, y.small_share, tax
                ),
            );
        } else if withheld >= target {
            rb.required(
                false,
                format!(
                    "Your West Virginia withholding of {} covers the year's {} target. No estimated payments are needed.",
                    withheld.whole(),
                    target.whole()
                ),
            )
            .choose(chosen);
        } else if withheld.0 == 0 {
            rb.required(
                true,
                format!(
                    "Your West Virginia tax of {} is {} or more, with no withholding. You need estimated payments of {}, a quarter by each date.",
                    tax.whole(),
                    y.threshold.whole(),
                    target.whole()
                ),
            )
            .choose(chosen);
        } else {
            rb.required(
                true,
                format!(
                    "Your West Virginia tax of {} less {} withheld leaves {}. That's {} or more, and withholding is short of the {} required. You need estimated payments of {}, a quarter by each date.",
                    tax.whole(),
                    withheld.whole(),
                    owed.whole(),
                    y.threshold.whole(),
                    target.whole(),
                    (target - withheld).whole()
                ),
            )
            .choose(chosen);
        }
        a.requirement = rb.build()?;
        if !a.exempt {
            for q in &asks {
                a.notes.ask(q.kind, q.question.clone(), q.effect.clone());
            }
        }
        if let Some(c) = a.requirement.chosen()
            && c.basis() == Basis::PriorYearTax
        {
            a.prior_chosen = true;
        }
        if a.exempt || !a.requirement.target_known() {
            a.required = vec![USCents(0); a.dues.len()];
            return Ok(a);
        }

        let cum = estimated::cumulative_shares(target, &quarters())?;
        a.required = estimated::increments(&cum);
        a.notes.rule(
            "W. Va. Code § 11-21-56(a)(1); IT-210 (2025), Part IV line 1",
            format!(
                "a quarter of {}, {}, is due on each of {}, {}, {} and {}",
                target,
                a.required[0],
                a.dues[0].long(),
                a.dues[1].long(),
                a.dues[2].long(),
                a.dues[3].long()
            ),
        );

        // IT-210 Part II: the annualised method with its recapture, lines 12
        // to 19. The caller supplies line 10 for each period.
        if f.elections().annualised() {
            let taxes: Vec<USCents> = f.periods().iter().map(|p| p.annualised_tax()).collect();
            a.required = estimated::annualised(&taxes, &y.applicable, &a.required)?;
            a.notes.rule(
                "W. Va. Code § 11-10-18a(d)(2); IT-210 (2025), Part II lines 11 to 19",
                format!(
                    "under the annualised method 22.5%, 45%, 67.5% and 90% of each period's annualised tax, less what earlier dates required, against the regular quarter plus any amount carried, give {}, {}, {} and {}",
                    a.required[0], a.required[1], a.required[2], a.required[3]
                ),
            );
        }

        let met = f.requirement_first_met();
        if !met.is_zero() && met.after(y.late_start_after) {
            a.notes.rule(
                "IT-210 (2025), Part IV line 1",
                format!(
                    "the requirement was first met on {}, after {}; IT-210 asks every filer for a quarter by every date, so the quarters are used",
                    met.long(),
                    y.late_start_after.long()
                ),
            );
        }

        // January 31, 2027 is a Sunday. § 2-2-1(e) moves it to February 1,
        // the date the 2025 booklet prints.
        let pay_by = y.calendar.next_business_day(y.file_and_pay_by);
        let filed = f.return_filed();
        if !filed.is_zero() && !filed.after(pay_by) {
            a.excused_january = true;
            a.required[3] = USCents(0);
            a.notes.rule(
                "W. Va. Code § 11-10-18a(i), § 2-2-1(e); IT-210 (2025), Part IV line 2 instructions",
                format!(
                    "the return was filed and paid on {}, by {}, so there is no penalty on the January installment",
                    filed.long(),
                    pay_by.long()
                ),
            );
        }
        Ok(a)
    }

    /// Applies the credits to the required installments, oldest shortfall
    /// first, as IT-210 Part IV Section B says.
    fn ledger(&self, f: &Facts, a: &Assessment) -> Result<(Vec<LedgerEntry>, Notes), Error> {
        let mut notes = Notes::default();
        let credits = if f.elections().withholding_as_withheld() {
            notes.rule(
                "W. Va. Code § 11-10-18a(g)(1); IT-210 (2025), Part I box and Part IV line 2 instructions",
                format!(
                    "the {} withheld counts on the dates it was withheld, as the box in Part I elects; estimated payments count on the day they were paid",
                    f.withheld_total()
                ),
            );
            estimated::credits_as_withheld(f, &a.dues)?
        } else {
            notes.rule(
                "W. Va. Code § 11-10-18a(g)(1); IT-210 (2025), Part IV line 2 instructions",
                format!(
                    "the {} withheld is treated as paid a quarter on each due date, whenever it was actually withheld; estimated payments count on the day they were paid",
                    f.withheld_total()
                ),
            );
            estimated::credits(f, &a.dues, &quarters())?
        };
        if let Some(p) = f.payments().iter().find(|p| p.from_prior_year()) {
            notes.rule(
                "IT-210 (2025), Part IV line 2 instructions",
                format!(
                    "last year's overpayment of {} applied forward is treated as paid on {}, as IT-210 does for a return filed by its due date",
                    p.amount(),
                    a.dues[0].long()
                ),
            );
        }
        let ledger = estimated::apply(&a.dues, &a.required, &credits)?;
        Ok((ledger, notes))
    }

    /// Prints a date as "Aug 31" in the tax year and "Jan 15, 2027" outside
    /// it.
    fn short(&self, d: Date) -> String {
        let month = d.month().to_string();
        let m: String = month.chars().take(3).collect();
        if d.year() == self.y.year {
            return format!("{m} {}", d.day());
        }
        format!("{m} {}, {}", d.day(), d.year())
    }
}

/// The ledger as due dates. A January installment the return excuses is
/// marked so.
fn installments(a: &Assessment, ledger: &[LedgerEntry]) -> Result<Vec<Installment>, Error> {
    let mut out = Vec::with_capacity(ledger.len());
    for (i, l) in ledger.iter().enumerate() {
        let mut b = estimated::new_installment(i as i64 + 1, l.due());
        b.needed_by(l.needed_by())
            .credited_by(l.credited_by())
            .shortfall(l.shortfall());
        if a.excused_january && i == 3 {
            b.excused();
        }
        out.push(b.build()?);
    }
    Ok(out)
}

/// Decides the waiver asked for. § 11-10-18a(e)(3) gives the federal
/// casualty waiver. There is no retirement or disability waiver; the 2025
/// IT-140 booklet offers a waiver for reasonable cause, and neither says
/// whether retiring or becoming disabled is one. `None` when no waiver was
/// asked for.
fn waiver_outcome(f: &Facts, notes: &mut Notes) -> Option<WaiverOutcome> {
    let kind = f.positions().waiver()?;
    if kind == WaiverKind::None {
        return None;
    }
    if kind == WaiverKind::Casualty {
        return estimated::standard_waiver(f, "the Tax Commissioner", 62);
    }
    notes.ask(
        PositionKind::UnsettledRule,
        "",
        "West Virginia's rules don't say if retiring or disability is reasonable cause. Reasonable cause lets the Tax Division waive the penalty.",
    );
    Some(estimated::waiver_undecided(
        "West Virginia has no waiver for retirement or disability. The Tax Division can waive the penalty if you show reasonable cause.",
    ))
}

impl EstimatedTax for WestVirginiaEstimated {
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
                    "with no {} West Virginia return to measure against, there is no target to schedule",
                    self.y.year - 1
                ),
            ));
        }
        let (ledger, notes) = self.ledger(f, &a)?;
        let out = installments(&a, &ledger)?;
        Ok(working::new_result(out)
            .with(&a.notes)
            .with(&notes)
            .build()?)
    }

    fn penalty(&self, f: &Facts, as_of: Date) -> Result<working::Result<Penalty>, Error> {
        if as_of.is_zero() {
            return Err(Error::Invalid(
                "westvirginiataxcalc: a penalty needs the date to figure it to".into(),
            ));
        }
        let a = self.assess(f)?;
        if !a.requirement.target_known() {
            return Err(Error::with_detail(
                Error::NeedsThisYearTax,
                format!(
                    "with no {} West Virginia return to measure against, there is no target to figure a penalty from",
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
        let waiver_opt = f.positions().waiver();
        let waiver_answered = waiver_opt.is_some();
        let waiver = waiver_opt.unwrap_or_default();
        let (ledger, mut notes) = self.ledger(f, &a)?;
        let schedule = installments(&a, &ledger)?;
        if a.exempt {
            let p = estimated::new_penalty(stop)
                .waiver(waiver)
                .because(a.requirement.clone(), &schedule)
                .build()?;
            return Ok(working::new_result(p).with(&a.notes).build()?);
        }

        let mut pb = estimated::new_penalty(stop);
        pb.waiver(waiver).because(a.requirement.clone(), &schedule);
        if !a.tax_known {
            pb.upper_bound();
        }
        let mut unrated = Date::default();
        for l in &ledger {
            if l.shortfall().0 <= 0 {
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
            let line = lb.days(last_day.days_since(l.due())).build()?;
            pb.line(line);
        }
        let mut p = pb.build()?;
        notes.rule(
            "W. Va. Code § 11-10-18a(a) and (c); IT-210 (2025), Part IV Section B",
            format!(
                "each shortfall is charged the daily factor for its calendar year, by the day, from its due date until paid or {}; each charge is rounded to the whole dollar, as the booklet's entries are; the penalty to {} is {}",
                y.stop.long(),
                stop.long(),
                p.total()
            ),
        );
        if !a.tax_known {
            notes.rule(
                "IT-210 (2025), Part I line 8",
                format!(
                    "this year's West Virginia tax was not given, so the penalty is figured against {} of the {} tax alone; the required payment is the lesser of that and 90% of this year's tax, so {} is the most the penalty can be",
                    a.requirement.options()[1].percent(),
                    y.year - 1,
                    p.total()
                ),
            );
        }
        if paid_in_full {
            notes.rule(
                "W. Va. Code § 11-10-18a(c)(2); IT-210 (2025), Part IV Section B",
                format!(
                    "the return was filed with the whole balance paid on {}, so the penalty stops that day",
                    stop.long()
                ),
            );
        }
        if !unrated.is_zero() {
            p = p.to_builder().rated_through(unrated.add_days(-1)).build()?;
        }
        if !unrated.is_zero() {
            notes.ask(
                PositionKind::UnpublishedRate,
                "",
                format!(
                    "The rate for the days after {} isn't published yet.",
                    self.short(unrated.add_days(-1))
                ),
            );
        }
        if waiver_answered && waiver == WaiverKind::Casualty {
            notes.rule(
                "W. Va. Code § 11-10-18a(e)(3)",
                "the Tax Commissioner may waive the penalty for a casualty, disaster or other unusual circumstance; the penalty shown is before any waiver",
            );
        } else if waiver_answered && waiver == WaiverKind::RetiredOrDisabled {
            notes.rule(
                "W. Va. Code § 11-10-18a(e); 2025 IT-140 booklet, Waiver of Penalty",
                "West Virginia has no waiver for retirement or disability; the Tax Division can waive for reasonable cause on a written request, and the penalty shown is before any waiver",
            );
        } else if p.total().0 > 0 {
            notes.rule(
                "W. Va. Code § 11-10-18a(e)(3); 2025 IT-140 booklet, Waiver of Penalty",
                "the Tax Division can waive the penalty for a casualty or for reasonable cause on a written request; the penalty shown is before any waiver",
            );
        }
        if p.total().0 > 0
            && let Some(o) = waiver_outcome(f, &mut notes)
        {
            p = p.to_builder().waiver_outcome(o).build()?;
        }
        Ok(working::new_result(p).with(&a.notes).with(&notes).build()?)
    }

    /// Written by hand and checked against research/estimated-payments/wv.md.
    fn explanation(&self) -> Explanation {
        estimated::new_explanation(
            "West Virginia expects estimated payments if withholding leaves $600 or more due. The penalty is interest on each quarter's shortfall, at 11.5% a year for 2026.",
        )
        .paragraph("You avoid the penalty if withholding and estimated payments reach a target. The target is the smaller of 90% of this year's tax or 100% of last year's. A quarter of it is due by each date. Last year's figure counts only if that return covered a full 12 months. There is no 110% tier for higher incomes. A zero tax last year counts only while this year's tax is $5,000 or less. Then the penalty form asks for nothing. Above that, you need 90% of this year's tax.")
        .paragraph("The due dates are April 15, June 15, September 15 and the next January 15. A date on a weekend or a legal holiday becomes the next business day. Withholding counts as paid evenly across the four dates. You can instead show when it was actually withheld. Filing and paying in full by the end of January excuses the January payment. For 2026 that means by Monday, February 1, 2027.")
        .paragraph("The penalty is charged daily on each date's shortfall. It runs until the shortfall is paid or April 15 of the next year. A payment goes first to the oldest shortfall. The rate is set once a year at the prime rate plus three points. The form prints it as a daily factor, 0.000315 for 2026. Form IT-210's annualized method can lower early quarters for uneven income. The Tax Division can waive the penalty for a casualty or for reasonable cause.")
        .versus_federal("West Virginia differs from federal in ways that change a number. Its threshold is $600, not the federal $1,000. It has no 110% prior-year tier. A zero tax last year counts only while this year's tax is $5,000 or less. The rate is set once a year, 11.5% for 2026, not each quarter. There's no waiver for retirement or disability.")
        .source("W. Va. Code §§ 11-21-55, 11-21-56, 11-10-17a, 11-10-18a and 2-2-1")
        .source("West Virginia Tax Division, IT-140ESI, Estimated Tax Payments Instructions (Sept 2023)")
        .source("West Virginia Tax Division, 2025 Personal Income Tax Forms and Instructions, including Form IT-210")
        .source("West Virginia Tax Division, TSD-365, How Interest and Additions to Tax are Assessed")
        .build()
        .unwrap_or_default()
    }
}
