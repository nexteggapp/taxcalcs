//! Montana's estimated tax rules, Mont. Code Ann. § 15-30-2512 and §
//! 15-1-216(7), as the shared [`estimated::EstimatedTax`] interface. Montana
//! calls the charge interest on underpayment of estimated tax.
//!
//! Montana follows the federal shape: four 25% installments, the lesser of
//! 90% of this year's tax or 100% of last year's, and payments applied to the
//! earliest installment first. It differs in the $500 threshold, no 110%
//! tier, a full exemption for farmers and ranchers and for anybody who
//! retired after 62 or became disabled this year or last, and one rate for
//! the whole tax year: the rate in effect when the return is due, so 2027's
//! for 2026.
//!
//! The figure follows Form EST-I for 2025, the latest published, with the
//! 2026 dates, and the result's divergences say where the statute reads
//! otherwise. 2025 House Bill 863 added only § 15-30-2512(8), a waiver for
//! tax year 2024, so the form does not predate the law it describes
//! (research/estimated-payments/mt.md).
//!
//! Not built: the hardship extension of § 15-30-2512(7), which the
//! department grants on a written request, and short tax years.

use std::any::Any;

use estimated::{
    Basis, Calendar, Date, Error, EstimatedTax, Explanation, FactorSeries, Facts, Installment,
    Jurisdiction, LedgerEntry, Penalty, Requirement, TargetOption, TargetOptionBuilder,
    WaiverCause, WaiverKind, WaiverOutcome,
};
use money::{Rate, USCents};
use working::{Divergence, Notes, PositionKind, Reading};

/// Montana's postal code.
const JURISDICTION: Jurisdiction = estimated::MONTANA;

/// The facts Montana's rules read that no other jurisdiction does. Carry
/// them in [`estimated::Facts::state`].
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct EstimatedFacts {
    refundable_credits: Option<USCents>,
    composite: Option<bool>,
    equal: Option<bool>,
}

impl EstimatedFacts {
    /// This year's refundable credits, Form 2 lines 15 to 17, or `None` when
    /// the caller did not say. EST-I Part I line 3b counts them with
    /// withholding, a quarter on each date.
    pub fn refundable_credits(&self) -> Option<USCents> {
        self.refundable_credits
    }

    /// Whether the filer takes part in a partnership's or S corporation's
    /// composite return for this year, which ARM 42.17.314(5) takes out of
    /// estimated tax, or `None` when the caller did not say.
    pub fn composite_return(&self) -> Option<bool> {
        self.composite
    }

    /// Whether the filer paid approximately equal quarterly installments,
    /// which § 15-30-2512(5)(e) excuses from interest when the year's
    /// payments reach the required annual payment, or `None` when the caller
    /// did not say. Whether installments are approximately equal is a
    /// judgement the statute does not define.
    pub fn approximately_equal_installments(&self) -> Option<bool> {
        self.equal
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
    /// Sets this year's refundable credits.
    pub fn refundable_credits(&mut self, a: USCents) -> &mut Self {
        self.e.refundable_credits = Some(a);
        self
    }

    /// Says whether the filer is part of a composite return.
    pub fn composite_return(&mut self, participant: bool) -> &mut Self {
        self.e.composite = Some(participant);
        self
    }

    /// Says whether the filer paid approximately equal installments.
    pub fn approximately_equal_installments(&mut self, equal: bool) -> &mut Self {
        self.e.equal = Some(equal);
        self
    }

    /// Refuses negative refundable credits.
    pub fn build(&self) -> Result<EstimatedFacts, Error> {
        if self.e.refundable_credits.unwrap_or_default().0 < 0 {
            return Err(Error::Invalid(
                "montanataxcalc: refundable credits cannot be negative".into(),
            ));
        }
        Ok(self.e)
    }
}

/// One tax year's published figures.
pub(crate) struct EstimatedYear {
    pub(crate) year: i64,

    /// EST-I Part I line 4 and § 15-30-2512(1)(b)(i).
    pub(crate) threshold: USCents,

    pub(crate) this_year_share: Rate,
    pub(crate) prior_year_share: Rate,
    pub(crate) shares: Vec<Rate>,
    pub(crate) annualised: Vec<Rate>,

    pub(crate) dues: Vec<Date>,
    pub(crate) calendar: Calendar,
    pub(crate) period_ends: Vec<Date>,

    /// § 15-30-2512(5)(d)(ii): the last day of the month after the tax year,
    /// before moving for a weekend or holiday.
    pub(crate) file_and_pay_by: Date,

    pub(crate) stop: Date,

    /// The factor EST-I prints for the rate in effect on the return's due
    /// date, over every day a 2026 underpayment can run.
    pub(crate) daily: FactorSeries,
}

/// The year this build carries, or `None`.
fn estimated_year(year: i64) -> Option<Result<EstimatedYear, Error>> {
    match year {
        2026 => Some(crate::estimated_y2026::year_2026()),
        _ => None,
    }
}

/// Returns Montana's estimated tax rules for a tax year, or
/// [`Error::NoRules`] for a year this build does not carry.
pub fn estimated_tax(year: i64) -> Result<Box<dyn EstimatedTax>, Error> {
    match estimated_year(year) {
        Some(y) => Ok(Box::new(MontanaEstimated { y: y? })),
        None => Err(estimated::no_rules(&JURISDICTION, year)),
    }
}

struct MontanaEstimated {
    y: EstimatedYear,
}

#[derive(Default)]
struct Assessment {
    requirement: Requirement,
    exempt: bool,
    tax_known: bool,
    dues: Vec<Date>,
    installments: Vec<USCents>,
    refundable: USCents,
    excused: bool,
    paid_for_year: USCents,
    extras: EstimatedFacts,
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

/// The exception that took the filer out of estimated tax.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
enum Exemption {
    #[default]
    None,
    Farm,
    Retired,
    Composite,
    NoLiability,
    NotRequired,
    Threshold,
}

/// The facts each divergence's readings turn on.
#[derive(Default)]
struct Touches {
    exempt_by: Exemption,
    not_required_to_file: bool,
    at_threshold: bool,
    short: bool,
    paid_target: bool,
    equal: bool,
    equal_answered: bool,
}

impl MontanaEstimated {
    fn extras(&self, f: &Facts) -> Result<EstimatedFacts, Error> {
        match f.state() {
            None => Ok(EstimatedFacts::default()),
            Some(s) => match s.downcast_ref::<EstimatedFacts>() {
                Some(e) => Ok(*e),
                None => Err(invalid(format!(
                    "montanataxcalc: the facts carry {}, not montanataxcalc.EstimatedFacts",
                    type_name(s)
                ))),
            },
        }
    }

    fn check(&self, f: &Facts) -> Result<(), Error> {
        if f.jurisdiction() != JURISDICTION {
            return Err(invalid(format!(
                "montanataxcalc: these are Montana's rules, and the facts are for {}",
                f.jurisdiction()
            )));
        }
        if f.tax_year() != self.y.year {
            return Err(invalid(format!(
                "montanataxcalc: these are the {} rules, and the facts are for {}",
                self.y.year,
                f.tax_year()
            )));
        }
        if f.elections().annualised() {
            for (i, p) in f.periods().iter().enumerate() {
                if p.end() != self.y.period_ends[i] {
                    return Err(invalid(format!(
                        "montanataxcalc: EST-I Part III period {} ends on {}, not {}",
                        i + 1,
                        self.y.period_ends[i],
                        p.end()
                    )));
                }
            }
        }
        Ok(())
    }

    /// Where the department's form and publication and the statutes part
    /// ways on a rule every Montana computation uses. They are in
    /// research/estimated-payments/mt.md, checked there against both, except
    /// the daily factor, found reading EST-I against § 15-1-216(7).
    fn divergences(&self, t: &Touches) -> Result<Vec<Divergence>, Error> {
        let equal_reading = if t.equal_answered {
            Reading::Statute
        } else {
            Reading::Form
        };
        Ok(vec![
            working::new_divergence("The daily factor")
                .form("Each day is charged at a printed factor of 0.000192. That is 7% divided by 365, rounded to six places.",
                    "Form EST-I (2025), Part II line 14 and 2026 Interest Rate")
                .statute("Interest is the federal underpayment rate for last year's third quarter, 7%. The department computes it daily at 0.019178%.",
                    "Mont. Code Ann. § 15-1-216(7)(a)(i); Montana Department of Revenue, Interest and Penalties")
                .uses(Reading::Form)
                .affects(t.short)
                .build()?,
            working::new_divergence("No liability last year")
                .form("No estimated payments are required if last year's Montana liability was zero. That year's tax period must have covered 12 months.",
                    "Form EST-I (2025) instructions, Exceptions to Interest")
                .statute("The exception also needs you to be a U.S. citizen or resident all that year.",
                    "Mont. Code Ann. § 15-30-2512(1)(b)(ii)")
                .uses(Reading::Form)
                .affects(t.exempt_by == Exemption::NoLiability)
                .build()?,
            working::new_divergence("Not required to file last year")
                .form("A nonresident or part-year resident who didn't have to file last year is exempt. Publication 1 extends this to anybody not required to file.",
                    "Form EST-I (2025) instructions, Exceptions to Interest; 2026 Publication 1, Who Must Pay Estimated Tax")
                .statute("The statute lists no such exception. A department rule grants it to nonresidents and part-year residents.",
                    "Mont. Code Ann. § 15-30-2512(1)(b); ARM 42.17.314(1)")
                .uses(Reading::Form)
                .affects(
                    t.not_required_to_file
                        && matches!(
                            t.exempt_by,
                            Exemption::None | Exemption::NoLiability | Exemption::NotRequired
                        ),
                )
                .build()?,
            working::new_divergence("When payments are required")
                .form("Publication 1 measures tax after withholding and nonrefundable credits. It requires payments when that is more than $500. Form EST-I stops only when it's under $500.",
                    "2026 Publication 1, Who Must Pay Estimated Tax; Form EST-I (2025), Part I line 4")
                .statute("Payments aren't required when tax after credits and withholding is under $500. Exactly $500 requires them.",
                    "Mont. Code Ann. § 15-30-2512(1)(b)(i)")
                .uses(Reading::Form)
                .affects(t.at_threshold && t.exempt_by == Exemption::None)
                .build()?,
            working::new_divergence("Retirement after 62")
                .form("Publication 1 exempts a retiree at least 62 at the end of the previous tax year. It covers the year of retirement and one more. Form EST-I says retired and at least 62.",
                    "2026 Publication 1, Who Must Pay Estimated Tax; Form EST-I (2025) instructions, Exceptions to Interest")
                .statute("Retiring after 62, or becoming disabled, exempts that tax year and the next.",
                    "Mont. Code Ann. § 15-30-2512(1)(b)(iv)")
                .uses(Reading::Statute)
                .affects(t.exempt_by == Exemption::Retired)
                .build()?,
            working::new_divergence("Approximately equal installments")
                .form("The form lists no exception for approximately equal installments.",
                    "Form EST-I (2025) instructions, Exceptions to Interest")
                .statute("Your withholding and estimated payments must meet the required annual payment. If you also paid approximately equal quarterly installments, no interest is due.",
                    "Mont. Code Ann. § 15-30-2512(5)(e)")
                .uses(equal_reading)
                .affects(t.short && t.paid_target && (t.equal || !t.equal_answered))
                .build()?,
            working::new_divergence("The late payment penalty")
                .form("The form figures interest only.", "Form EST-I (2025), Part II")
                .statute("Estimated tax paid late is charged a late payment penalty as well as interest. It is 1.5% a month, up to 15%, from the return's original due date.",
                    "Mont. Code Ann. § 15-1-216(2)(b), (c); ARM 42.17.306(3)")
                .uses(Reading::Form)
                .affects(t.short)
                .build()?,
        ])
    }

    fn assess(&self, f: &Facts) -> Result<Assessment, Error> {
        self.check(f)?;
        let extras = self.extras(f)?;
        let y = &self.y;
        let mut a = Assessment {
            extras,
            dues: estimated::move_dates(&y.calendar, &y.dues),
            ..Assessment::default()
        };
        let last = y.year - 1;

        let tax_opt = f.this_year().tax();
        let tax_known = tax_opt.is_some();
        let tax = tax_opt.unwrap_or_default();
        a.tax_known = tax_known;
        let withheld = f.withheld_total();
        let refundable_opt = extras.refundable_credits();
        let refundable = refundable_opt.unwrap_or_default();
        a.refundable = refundable;
        let credited = withheld + refundable;
        let prior = f.prior_year();
        let has_prior = prior.is_some();
        let prior_tax = prior
            .and_then(|p| p.figures())
            .and_then(|pf| pf.tax())
            .unwrap_or_default();

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
                "Mont. Code Ann. § 15-30-2512(1)(a)(i); Form EST-I (2025), Part I line 2",
                format!(
                    "{} of this year's Montana tax after nonrefundable credits of {} is {}",
                    y.this_year_share,
                    tax,
                    this_year.amount()
                ),
            );
        }

        let unavailable = |why: String| -> TargetOptionBuilder {
            let mut b =
                estimated::new_target_option(Basis::PriorYearTax, y.prior_year_share, USCents(0));
            b.unavailable(why);
            b
        };
        let prior_option = match prior {
            None => unavailable(format!("Needs your {last} Montana return")),
            Some(p) if !p.filed() => {
                unavailable(format!("You didn't file a {last} Montana return"))
            }
            Some(p) if p.months() != 12 => unavailable(format!(
                "Your {last} Montana return covered {} months, not a full year",
                p.months()
            )),
            Some(_) => {
                a.notes.rule(
                    "Mont. Code Ann. § 15-30-2512(1)(a)(ii); Form EST-I (2025), Part I line 5",
                    format!(
                        "the prior-year test is {} of the {} tax after nonrefundable credits on the {last} return",
                        y.prior_year_share, prior_tax
                    ),
                );
                estimated::new_target_option(
                    Basis::PriorYearTax,
                    y.prior_year_share,
                    y.prior_year_share.of(prior_tax),
                )
            }
        };
        let prior_opt = prior_option.build()?;

        let mut rb = estimated::new_requirement();
        rb.option(this_year.clone()).option(prior_opt.clone());
        let (mut chosen, mut target) = (Basis::ThisYearTax, this_year.amount());
        if prior_opt.available() && (!tax_known || prior_opt.amount() < target) {
            (chosen, target) = (Basis::PriorYearTax, prior_opt.amount());
        }

        // The exceptions that take the filer out of estimated tax.
        let no_liability = prior.is_some_and(|p| {
            p.months() == 12 && ((p.filed() && prior_tax.0 == 0) || p.not_required_to_file())
        });
        let residence = f.residence();
        let this_resident =
            residence.is_empty() || estimated::resident_all_year(&residence, &JURISDICTION, y.year);
        let not_required = prior.is_some_and(|p| p.not_required_to_file()) && !this_resident;
        let retired = f
            .people()
            .iter()
            .any(|p| p.retired_after_62() || p.disabled());
        let composite = extras.composite_return() == Some(true);
        let mut exempt_by = Exemption::None;
        let mut exempt = |a: &mut Assessment,
                          rb: &mut estimated::RequirementBuilder,
                          by: Exemption,
                          exception: TargetOption,
                          why: String,
                          cite: &str,
                          applied: String| {
            a.exempt = true;
            exempt_by = by;
            rb.required(false, why).exempt(exception);
            a.notes.rule(cite, applied);
        };
        let named = |label: &str| estimated::new_exception(label).build();
        if i64::from(f.farm_share().0) * 3 >= 20_000 {
            exempt(
                &mut a,
                &mut rb,
                Exemption::Farm,
                named("Two-thirds or more of your income from farming or ranching")?,
                "At least two-thirds of your gross income is from farming or ranching. Montana requires no estimated payments from you and charges no interest.".into(),
                "Mont. Code Ann. § 15-30-2512(1)(a), (6)",
                format!(
                    "{} of gross income from farming or ranching is at least 66 2/3%, so a farmer or rancher owes no estimated tax",
                    f.farm_share()
                ),
            );
        } else if retired {
            exempt(
                &mut a,
                &mut rb,
                Exemption::Retired,
                named("Retired after reaching sixty-two, or disabled, this year or last")?,
                format!(
                    "You retired after 62 or became disabled in {last} or {}. Montana requires no estimated payments for {}.",
                    y.year, y.year
                ),
                "Mont. Code Ann. § 15-30-2512(1)(b)(iv)",
                "a filer who retired after reaching 62 or became disabled this year or last owes no estimated tax".into(),
            );
        } else if composite {
            exempt(
                &mut a,
                &mut rb,
                Exemption::Composite,
                named("Part of a composite return this year")?,
                "You're part of a composite return this year, so you need no estimated payments."
                    .into(),
                "ARM 42.17.314(5)",
                "a participant in a composite return for the current year is not required to pay estimated tax".into(),
            );
        } else if no_liability {
            exempt(
                &mut a,
                &mut rb,
                Exemption::NoLiability,
                named("No Montana tax due last year")?,
                format!(
                    "Your {last} Montana tax period covered 12 months and your liability was zero. No estimated payments are required for {}.",
                    y.year
                ),
                "Mont. Code Ann. § 15-30-2512(1)(b)(ii); Form EST-I (2025) instructions, Exceptions to Interest",
                format!(
                    "the {last} return showed no liability for a full 12 months, so there is no interest for {}",
                    y.year
                ),
            );
        } else if not_required {
            exempt(
                &mut a,
                &mut rb,
                Exemption::NotRequired,
                named("Not required to file a Montana return last year")?,
                format!(
                    "You aren't a full-year resident and didn't have to file a {last} Montana return. No estimated payments are required."
                ),
                "ARM 42.17.314(1); Form EST-I (2025) instructions, Exceptions to Interest",
                format!(
                    "a nonresident or part-year resident not required to file for {last} owes no estimated tax"
                ),
            );
        } else if tax_known && tax - credited < y.threshold {
            let exception = estimated::exception_below(
                "Tax less withholding and refundable credits",
                tax - credited,
                y.threshold,
            )?;
            let left = if tax < credited {
                format!("That leaves a {} refund.", (credited - tax).whole())
            } else {
                format!(
                    "That leaves {}, under {}.",
                    (tax - credited).whole(),
                    y.threshold.whole()
                )
            };
            exempt(
                &mut a,
                &mut rb,
                Exemption::Threshold,
                exception,
                format!(
                    "Your Montana tax is {}; withholding and refundable credits are {}. {left} No estimated payments are needed.",
                    tax.whole(),
                    credited.whole()
                ),
                "Mont. Code Ann. § 15-30-2512(1)(b)(i); Form EST-I (2025), Part I line 4",
                format!(
                    "{} of tax less {} of withholding and credits is {}, under {}, so there is no interest",
                    tax,
                    credited,
                    tax - credited,
                    y.threshold
                ),
            );
        } else if !tax_known && !prior_opt.available() {
            rb.required(
                false,
                format!(
                    "Your target needs your {} Montana tax or a full-year {last} Montana return. There's no target yet.",
                    y.year
                ),
            )
            .no_target();
        } else if !tax_known && credited >= target {
            rb.required(
                false,
                format!(
                    "Your withholding and refundable credits are {}. They already reach {} of your {last} tax, {}. No estimated payments are needed, whatever your {} tax turns out to be.",
                    credited.whole(),
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
                    "Your target is {} of your {last} tax, {}. It keeps you clear of interest whatever your {} tax turns out to be. Withholding and refundable credits cover {} of it. The other {} is due in estimated payments, a quarter by each due date.",
                    prior_opt.percent(),
                    target.whole(),
                    y.year,
                    credited.whole(),
                    (target - credited).whole()
                ),
            )
            .choose(chosen);
        } else if credited >= target {
            rb.required(
                false,
                format!(
                    "Your Montana withholding and refundable credits are {}. They already reach the {} needed for the year. No estimated payments are needed.",
                    credited.whole(),
                    target.whole()
                ),
            )
            .choose(chosen);
        } else {
            rb.required(
                true,
                format!(
                    "Your Montana tax is {}; withholding and refundable credits are {}. That leaves {}, at least {}, so estimated payments are needed. You need {} of them, a quarter by each due date.",
                    tax.whole(),
                    credited.whole(),
                    (tax - credited).whole(),
                    y.threshold.whole(),
                    (target - credited).whole()
                ),
            )
            .choose(chosen);
        }
        a.requirement = rb.build()?;
        // Under an exception neither return can change the target.
        if !has_prior && !a.exempt {
            a.notes.ask(
                PositionKind::PriorYearReturn,
                format!("Did you file a {last} Montana return, and what was its tax?"),
                format!(
                    "With it, 100% of your {last} tax could be a lower target than 90% of this year's."
                ),
            );
        }
        if !tax_known && !a.exempt {
            let effect = if prior_opt.available() {
                format!(
                    "With it, 90% of it could be a lower target than your {last} tax. It also decides whether the {} threshold excuses you.",
                    y.threshold.whole()
                )
            } else {
                format!(
                    "Without it there's no target. There's no full-year {last} Montana return to measure against."
                )
            };
            a.notes.ask(
                PositionKind::ThisYearTax,
                format!("What do you expect your {} Montana tax to be?", y.year),
                effect,
            );
        }
        if !a.exempt && prior.is_some_and(|p| p.not_required_to_file()) && residence.is_empty() {
            a.notes.ask(
                PositionKind::Residency,
                format!("Will you live in Montana all of {}?", y.year),
                format!(
                    "If you won't, you need no estimated payments. That is because you didn't have to file a {last} Montana return."
                ),
            );
        }
        if !a.exempt && refundable_opt.is_none() {
            a.notes.ask(
                PositionKind::JurisdictionFact,
                format!("What Montana refundable credits do you expect for {}?", y.year),
                "They count with withholding, a quarter on each due date. Until then the figures count none.",
            );
        }

        let mut cumulative = estimated::cumulative_shares(a.requirement.target(), &y.shares)?;
        // § 15-30-2512(3)(b): a requirement that arises after an installment
        // date catches up at the first installment that applies.
        let met = f.requirement_first_met();
        if !met.is_zero() {
            let n = a.dues.len() - 1;
            for (c, due) in cumulative.iter_mut().zip(&a.dues).take(n) {
                if met.after(*due) {
                    *c = USCents(0);
                }
            }
        }
        a.installments = estimated::increments(&cumulative);
        a.notes.rule(
            "Mont. Code Ann. § 15-30-2512(2)(a), (3); Form EST-I (2025), Part II line 1",
            format!(
                "of {}, {}, {}, {} and {} are due on {}, {}, {} and {}",
                a.requirement.target(),
                a.installments[0],
                a.installments[1],
                a.installments[2],
                a.installments[3],
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
                "Mont. Code Ann. § 15-30-2512(4); Form EST-I (2025), Part III",
                format!(
                    "under the annualized method the installments are {}, {}, {} and {}: 22.5%, 45%, 67.5% and 90% of the annualized tax, \
                     less earlier installments, and never more than the regular installment plus any reduction carried forward",
                    ann[0], ann[1], ann[2], ann[3]
                ),
            );
            a.installments = ann;
        }

        // § 15-30-2512(5)(d). January 31, 2027 is a Sunday, a legal holiday
        // in Montana, so § 1-1-307 moves the day to Monday, February 1.
        let file_by = y.calendar.next_business_day(y.file_and_pay_by);
        let filed = f.return_filed();
        if !filed.is_zero() && !filed.after(file_by) && !a.exempt {
            a.excused = true;
            a.notes.rule(
                "Mont. Code Ann. §§ 15-30-2512(5)(d), 1-1-307; Form EST-I (2025), Lines 12 through 15",
                format!(
                    "the return was filed and paid on {}, by {}, so the January installment bears no interest",
                    filed.long(),
                    file_by.long()
                ),
            );
        }
        if a.exempt {
            a.installments = vec![USCents(0); a.dues.len()];
        }

        a.paid_for_year = credited;
        for p in f.payments() {
            a.paid_for_year += p.amount();
        }
        let equal = extras.approximately_equal_installments();
        let mut t = Touches {
            exempt_by,
            not_required_to_file: prior.is_some_and(|p| p.not_required_to_file()),
            at_threshold: tax_known && tax - credited == y.threshold,
            paid_target: a.paid_for_year >= a.requirement.target(),
            equal: equal == Some(true),
            equal_answered: equal.is_some(),
            ..Touches::default()
        };
        if !a.exempt && a.requirement.target_known() {
            let (ledger, _) = self.ledger(f, &a)?;
            t.short = ledger.iter().any(|l| l.shortfall().0 > 0);
        }
        let divergences = self.divergences(&t)?;
        a.notes.diverges(&divergences);
        Ok(a)
    }

    /// Applies withholding, refundable credits and payments to the
    /// installments, earliest first.
    fn ledger(&self, f: &Facts, a: &Assessment) -> Result<(Vec<LedgerEntry>, Notes), Error> {
        let mut credits = estimated::credits(f, &a.dues, &self.y.shares)?;
        let cum = estimated::cumulative_shares(a.refundable, &self.y.shares)?;
        for (i, amt) in estimated::increments(&cum).into_iter().enumerate() {
            if amt.0 == 0 {
                continue;
            }
            credits.push(estimated::new_payment().on(a.dues[i]).amount(amt).build()?);
        }
        let mut required = a.installments.clone();
        if a.excused {
            required[3] = USCents(0);
        }
        let ledger = estimated::apply(&a.dues, &required, &credits)?;
        let mut notes = Notes::default();
        notes.rule(
            "Form EST-I (2025), Part II lines 2 and 12; Mont. Code Ann. § 15-30-2512(5)(c)",
            format!(
                "the {} withheld and the {} of refundable credits are treated as paid a quarter on each due date, and a payment goes first to the earliest unpaid installment",
                f.withheld_total(),
                a.refundable
            ),
        );
        if f.elections().withholding_as_withheld() {
            notes.rule(
                "Form EST-I (2025), Part II line 2",
                "Montana has no election to credit withholding on the dates it was withheld, so it is spread evenly",
            );
        }
        Ok((ledger, notes))
    }

    /// The installments and the ledger they come from.
    fn schedule_of(
        &self,
        f: &Facts,
        a: &Assessment,
    ) -> Result<(Vec<Installment>, Vec<LedgerEntry>, Notes), Error> {
        let (ledger, notes) = self.ledger(f, a)?;
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
        Ok((out, ledger, notes))
    }
}

impl EstimatedTax for MontanaEstimated {
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
                    "with no {} Montana return to measure against, there is no target to schedule",
                    self.y.year - 1
                ),
            ));
        }
        let (out, _, notes) = self.schedule_of(f, &a)?;
        Ok(working::new_result(out)
            .with(&a.notes)
            .with(&notes)
            .build()?)
    }

    fn penalty(&self, f: &Facts, as_of: Date) -> Result<working::Result<Penalty>, Error> {
        if as_of.is_zero() {
            return Err(invalid(
                "montanataxcalc: a penalty needs the date to figure it to".into(),
            ));
        }
        let a = self.assess(f)?;
        if !a.requirement.target_known() {
            return Err(Error::with_detail(
                Error::NeedsThisYearTax,
                format!(
                    "with no {} Montana return to measure against, there is no target to figure the interest from",
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
        let waiver_answered = f.positions().waiver();
        let waiver = waiver_answered.unwrap_or_default();
        let (schedule, ledger, mut notes) = self.schedule_of(f, &a)?;
        if a.exempt {
            let p = estimated::new_penalty(stop)
                .waiver(waiver)
                .because(a.requirement.clone(), &schedule)
                .build()?;
            return Ok(working::new_result(p).with(&a.notes).build()?);
        }

        // § 15-30-2512(5)(e): approximately equal installments that reach the
        // required annual payment owe nothing. EST-I does not list it.
        let paid_for_year = a.paid_for_year;
        let equal = a.extras.approximately_equal_installments();
        if equal == Some(true) && paid_for_year >= a.requirement.target() {
            notes.rule(
                "Mont. Code Ann. § 15-30-2512(5)(e)",
                format!(
                    "the year's {} reaches the {} required and, as the caller answered, was paid in approximately equal quarterly installments, so no interest is assessed",
                    paid_for_year,
                    a.requirement.target()
                ),
            );
            let p = estimated::new_penalty(stop)
                .waiver(waiver)
                .because(a.requirement.clone(), &schedule)
                .build()?;
            return Ok(working::new_result(p).with(&a.notes).with(&notes).build()?);
        }

        // EST-I Part II lines 12 to 14: each part of each shortfall for the
        // days until it was paid, at the printed factor, in cents.
        let mut pb = estimated::new_penalty(stop);
        pb.waiver(waiver).because(a.requirement.clone(), &schedule);
        let mut unrated = Date::default();
        let mut early = false;
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
            if line.total().0 > 0 && l.number() < ledger.len() as i64 {
                early = true;
            }
            pb.line(line);
        }
        if !a.tax_known {
            pb.upper_bound();
        }
        let mut p = pb.build()?;
        if !a.tax_known {
            notes.rule(
                "Mont. Code Ann. § 15-30-2512(1)(a)",
                format!(
                    "this year's Montana tax was not given, so the interest is figured against {} of the {} tax alone; \
                     the required payment is the lesser of that and 90% of this year's tax, so {} is the most the interest can be, and it is less if this year's tax turns out lower",
                    a.requirement.options()[1].percent(),
                    y.year - 1,
                    p.total()
                ),
            );
        }
        notes.rule(
            "Mont. Code Ann. §§ 15-30-2512(5), 15-1-216(7); ARM 42.2.505(5); Form EST-I (2025), Part II lines 12 to 15",
            format!(
                "each part of each shortfall is charged the daily factor for the rate in effect on the return's due date, from its due date until paid or {}; \
                 the interest to {} is {}",
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
        if let Some(o) = waiver_outcome(f)
            && p.total().0 > 0
        {
            p = p.to_builder().waiver_outcome(o).build()?;
        }
        if p.total().0 > 0 {
            notes.ask(
                PositionKind::UnpublishedRate,
                "",
                format!(
                    "Montana hasn't published its {} rate, which applies to {} estimates. The figures use the law's: the federal rate for the third quarter of {}, 7%.",
                    y.year + 1,
                    y.year,
                    y.year
                ),
            );
            notes.ask(
                PositionKind::UnsettledRule,
                "",
                "Montana's 1.5% a month late payment penalty may apply to a missed installment. No published source says whether it does, and the figures leave it out.",
            );
            if waiver_answered.is_none() {
                notes.ask(
                    PositionKind::PenaltyWaiver,
                    "Did a casualty, disaster or other unusual circumstance cause the shortfall?",
                    "If so and you ask, Montana can waive the interest for good cause. Until it does, the figures include the full interest.",
                );
            }
            if f.people().is_empty() {
                notes.ask(
                    PositionKind::JurisdictionFact,
                    format!(
                        "Did you retire after reaching 62, or become disabled, in {} or {}?",
                        y.year - 1,
                        y.year
                    ),
                    "If you did, no estimated payments are needed and no interest is due.",
                );
            }
            if a.extras.composite_return().is_none() {
                notes.ask(
                    PositionKind::JurisdictionFact,
                    format!(
                        "Are you part of a partnership's or S corporation's composite return for {}?",
                        y.year
                    ),
                    "If you are, no estimated payments are needed and no interest is due.",
                );
            }
            if equal.is_none() && paid_for_year >= a.requirement.target() {
                notes.ask(
                    PositionKind::JurisdictionFact,
                    "Did you pay in approximately equal quarterly installments?",
                    format!(
                        "Your payments reach the {} needed. If they were about equal, Montana law charges no interest.",
                        a.requirement.target().whole()
                    ),
                );
            }
            if early && f.requirement_first_met().is_zero() {
                notes.ask(
                    PositionKind::RequirementFirstMet,
                    format!(
                        "When in {} did your income first make estimated payments necessary?",
                        y.year
                    ),
                    "Until then, the figures assume you needed them from January 1. Montana lets you catch up at the next due date after that.",
                );
            }
        }
        Ok(working::new_result(p).with(&a.notes).with(&notes).build()?)
    }

    fn explanation(&self) -> Explanation {
        montana_explanation()
    }
}

/// Decides the waivers of § 15-30-2512(1)(b)(iii) and (iv). The window and
/// the age are the federal ones: retiring after reaching 62, or becoming
/// disabled, this year or last. Unlike federal law, that needs no reasonable
/// cause. A casualty or disaster needs the department to find good cause.
fn waiver_outcome(f: &Facts) -> Option<WaiverOutcome> {
    let o = estimated::standard_waiver(f, "the Montana Department of Revenue", 62)?;
    if !o.eligible() {
        return Some(o);
    }
    let p = f.positions();
    let kind = p.waiver().unwrap_or_default();
    if kind == WaiverKind::Casualty {
        let what = match p.waiver_cause() {
            WaiverCause::Casualty => "a casualty",
            WaiverCause::Disaster => "a disaster",
            _ => "a casualty, disaster or other unusual event",
        };
        return Some(estimated::waiver_eligible(format!(
            "The interest can be waived for {what}. The Montana Department of Revenue decides if it is good cause for the shortfall."
        )));
    }
    if p.waiver_cause() == WaiverCause::Disability {
        return Some(estimated::waiver_eligible(format!(
            "You became disabled in {}. Montana charges no interest for the year you become disabled or the year after.",
            p.waiver_on().year()
        )));
    }
    Some(estimated::waiver_eligible(format!(
        "You retired in {} after reaching 62. Montana charges no interest for the year you retire or the year after.",
        p.waiver_on().year()
    )))
}

/// Written by hand and checked against research/estimated-payments/mt.md.
fn montana_explanation() -> Explanation {
    estimated::new_explanation(
        "Montana expects estimated payments. They start once your tax after withholding and credits is $500 or more. Falling short costs interest, figured by the day on each quarter's shortfall.")
        .paragraph("Your target is 90% of this year's Montana tax or 100% of last year's. It is the smaller of the two. You are clear if withholding, refundable credits and estimated payments meet it. A quarter is due by April 15, June 15, September 15 and January 15. There is no 110% figure for higher incomes. Last year's figure needs a full-year return. Withholding counts as paid evenly across the four dates. A payment goes first to the earliest quarter still short.")
        .paragraph("Two groups need no estimated payments at all. One is anyone who retired after 62 or became disabled this year or last. The other is anyone with no Montana liability on last year's full-year return. Filing and paying in full by January 31 clears the January payment's interest. In 2027 that day is a Sunday, so Monday, February 1 counts.")
        .paragraph("Interest runs from each due date until the shortfall is paid. It stops on April 15 of the next year. Montana uses one rate all year, the rate in effect when the return is due. For 2026 estimates that is the 2027 rate, 7%. It is the federal rate for the third quarter of 2026. An annualized method can cut early payments when income comes late in the year.")
        .versus_federal("Montana's threshold is $500 rather than $1,000, and there is no 110% tier. Retiring after 62 or becoming disabled exempts you outright, not by waiver. The rate is set once for the year from the return's due date, not each quarter. A January 31 that falls on a weekend moves to the next business day.")
        .source("Mont. Code Ann. §§ 15-30-2512, 15-1-216, 1-1-216, 1-1-307")
        .source("Form EST-I, 2025 Interest on Underpayment of Estimated Tax, and its instructions")
        .source("2026 Montana Publication 1, Withholding and Estimated Payments")
        .source("ARM 42.17.314, 42.17.306, 42.2.505")
        .source("Montana Department of Revenue, Interest and Penalties; Rev. Rul. 2026-10")
        .build()
        .unwrap_or_default()
}
