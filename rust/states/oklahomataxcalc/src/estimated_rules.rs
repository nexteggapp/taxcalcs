//! Oklahoma's estimated income tax rules, 68 O.S. §§ 2385.7, 2385.9 and
//! 2385.13, as the shared [`estimated::EstimatedTax`] trait. Ported from
//! estimated.go.
//!
//! The shape is federal: the lesser of a share of this year's tax and 100%
//! of last year's, four equal quarters, and interest by the day on each
//! quarter's shortfall. The numbers are not. This year's share is 70%, there
//! is no higher prior-year share at any income, payments are needed from
//! $500 of tax over withholding, and the interest is 20% a year, fixed by
//! statute. Farmers are not required to make payments at all.
//!
//! The figure follows the Oklahoma Tax Commission's forms, OW-8-ES (revised
//! 6-2025) for 2026 and OW-8-P for 2025, and the result's Divergences say
//! where the statutes read otherwise. §§ 2385.7 and 2385.9 were last amended
//! by Laws 2000, c. 314, and § 2385.13 by Laws 2021, c. 436
//! (testdata/os68-2385.txt), so no form here predates the law it describes.

use std::any::Any;
use std::sync::LazyLock;

use estimated::{
    Basis, Calendar, Date, Error, EstimatedTax, Explanation, Facts, Installment, Jurisdiction,
    LedgerEntry, Penalty, RateSeries, Requirement, WaiverKind,
};
use money::{Rate, USCents};
use working::{Divergence, Notes, PositionKind, Reading};

/// Oklahoma's postal code.
const JURISDICTION: Jurisdiction = estimated::OKLAHOMA;

/// The facts Oklahoma's rules read that no other jurisdiction does. Carry
/// them in [`estimated::FactsBuilder::state`].
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct EstimatedFacts {
    prior_farm_share: Option<Rate>,
}

impl EstimatedFacts {
    /// Last year's gross income from farming as a share of last year's gross
    /// income from all sources, if the caller gave it. 68 O.S.
    /// § 2385.7(B)(2) excuses a filer who was a farmer in either year. This
    /// year's share is [`estimated::Facts::farm_share`].
    pub fn prior_year_farm_share(&self) -> Option<Rate> {
        self.prior_farm_share
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
    /// Sets last year's farm share.
    pub fn prior_year_farm_share(&mut self, r: Rate) -> &mut Self {
        self.e.prior_farm_share = Some(r);
        self
    }

    /// Refuses a share below nothing or above the whole.
    pub fn build(&self) -> Result<EstimatedFacts, Error> {
        let share = self.e.prior_farm_share.unwrap_or_default();
        if share.0 < 0 || share.0 > 10_000 {
            return Err(Error::Invalid(format!(
                "oklahomataxcalc: a farm share of {share} is not a share of one income"
            )));
        }
        Ok(self.e)
    }
}

/// A share written as the statute writes it, so that two-thirds is not
/// rounded to a number of basis points.
#[derive(Clone, Copy, Debug)]
pub(crate) struct Fraction {
    pub(crate) num: i64,
    pub(crate) den: i64,
}

impl Fraction {
    /// Reports whether `r` is at least the fraction.
    fn reaches(self, r: Rate) -> bool {
        i64::from(r.0) * self.den >= self.num * 10_000
    }
}

/// One tax year's published figures.
#[derive(Clone, Debug)]
pub(crate) struct EstimatedYear {
    pub(crate) year: i64,

    /// § 2385.7(A) and OW-8-P line 6: payments are needed, and interest can
    /// arise, only when tax less withholding is this or more.
    pub(crate) threshold: USCents,

    /// § 2385.13(C) and OW-8-P line 3: no interest when the tax is under it.
    pub(crate) small_tax: USCents,

    /// § 2385.9(B): 70% of this year's tax, or 100% of the tax on last year's
    /// 12-month return.
    pub(crate) this_year_share: Rate,
    pub(crate) prior_year_share: Rate,

    /// § 2385.9(A), four equal installments; OW-8-P line 10 spreads
    /// withholding in the same parts.
    pub(crate) shares: Vec<Rate>,

    /// The applicable percentages of OW-8-P-SUP-I line 16, and the ends of
    /// its columns.
    pub(crate) annualised: Vec<Rate>,
    pub(crate) period_ends: Vec<Date>,

    pub(crate) dues: Vec<Date>,

    /// The days the OTC is closed: Saturdays, Sundays and the § 82.1
    /// holidays.
    pub(crate) calendar: Calendar,

    /// § 2385.9(A)(1): a return filed and paid by this date waives the
    /// January installment. The forms do not apply it.
    pub(crate) file_and_pay_by: Date,

    /// § 2385.13(B): the interest runs no later than the fifteenth day of
    /// the fourth month after the year.
    pub(crate) stop: Date,

    /// § 2385.13(A), 20% a year.
    pub(crate) rates: RateSeries,

    /// § 2385.7(B)(2): sixty-six and two-thirds percent.
    pub(crate) farm_share: Fraction,
}

fn year(year: i64) -> Result<EstimatedYear, Error> {
    match year {
        2026 => crate::estimated_y2026::year_2026(),
        _ => Err(estimated::no_rules(&JURISDICTION, year)),
    }
}

/// Returns Oklahoma's estimated tax rules for a tax year, or
/// [`estimated::Error::NoRules`] for a year this build does not carry.
pub fn estimated_tax(year_: i64) -> Result<Box<dyn EstimatedTax>, Error> {
    Ok(Box::new(OklahomaEstimated { y: year(year_)? }))
}

/// Returns the holidays Oklahoma's rules move a due date for in a tax year,
/// from 25 O.S. § 82.1, or [`estimated::Error::NoRules`]. A caller planning
/// payment dates can move them the same way.
pub fn estimated_calendar(year_: i64) -> Result<Calendar, Error> {
    Ok(year(year_)?.calendar)
}

struct OklahomaEstimated {
    y: EstimatedYear,
}

/// What the three questions share.
#[derive(Clone, Default)]
struct Assessment {
    requirement: Requirement,
    exempt: bool,
    tax_known: bool,
    dues: Vec<Date>,

    /// Each installment's own amount.
    installments: Vec<USCents>,

    /// Whether the return was filed and paid by the § 2385.9(A)(1) date,
    /// moved for a weekend or holiday. The waiver is reported, not applied.
    january_waiver: bool,
    file_by: Date,

    // What each divergence turns on in these facts.
    residence_open: bool,
    withholding_rule: bool,
    small_tax_exempt: bool,

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

/// What the target leaves to pay after withholding, for a Why.
fn target_why(target: USCents, withheld: USCents) -> String {
    if withheld.0 == 0 {
        return format!(
            "Your target is {}, a quarter by each due date.",
            target.whole()
        );
    }
    format!(
        "Your target is {}, or {} after withholding. Pay a quarter by each due date.",
        target.whole(),
        (target - withheld).whole()
    )
}

/// What estimated payments the target leaves after withholding, for a Why.
fn left_why(target: USCents, withheld: USCents) -> String {
    if withheld.0 == 0 {
        return format!(
            "That's {} of estimated payments, a quarter by each due date.",
            target.whole()
        );
    }
    format!(
        "After {} of withholding, that's {} of estimated payments. Pay a quarter by each due date.",
        withheld.whole(),
        (target - withheld).whole()
    )
}

/// What tax less withholding comes to, for a Why.
fn owed_why(tax: USCents, withheld: USCents) -> String {
    if withheld.0 == 0 {
        return format!("Your Oklahoma tax is {}, with no withholding.", tax.whole());
    }
    format!(
        "Your Oklahoma tax of {} less {} withheld is {}.",
        tax.whole(),
        withheld.whole(),
        (tax - withheld).whole()
    )
}

impl OklahomaEstimated {
    fn extras(&self, f: &Facts) -> Result<EstimatedFacts, Error> {
        match f.state() {
            None => Ok(EstimatedFacts::default()),
            Some(s) => match s.downcast_ref::<EstimatedFacts>() {
                Some(e) => Ok(*e),
                None => Err(Error::Invalid(format!(
                    "oklahomataxcalc: the facts carry {}, not oklahomataxcalc.EstimatedFacts",
                    type_name(s)
                ))),
            },
        }
    }

    fn check(&self, f: &Facts) -> Result<(), Error> {
        if f.jurisdiction() != JURISDICTION {
            return Err(Error::Invalid(format!(
                "oklahomataxcalc: these are Oklahoma's rules, and the facts are for {}",
                f.jurisdiction()
            )));
        }
        if f.tax_year() != self.y.year {
            return Err(Error::Invalid(format!(
                "oklahomataxcalc: these are the {} rules, and the facts are for {}",
                self.y.year,
                f.tax_year()
            )));
        }
        if f.elections().annualised() {
            for (i, p) in f.periods().iter().enumerate() {
                let Some(&end) = self.y.period_ends.get(i) else {
                    return Err(Error::Invalid(format!(
                        "oklahomataxcalc: OW-8-P-SUP-I has no column {}",
                        i + 1
                    )));
                };
                if p.end() != end {
                    return Err(Error::Invalid(format!(
                        "oklahomataxcalc: OW-8-P-SUP-I column {} ends on {}, not {}",
                        i + 1,
                        end,
                        p.end()
                    )));
                }
            }
        }
        Ok(())
    }

    /// Where the OTC's forms and the statutes part ways on a rule an Oklahoma
    /// computation uses. They come from research/estimated-payments/ok.md,
    /// sections 10 and Verification, and were checked against the extracts in
    /// testdata/. Each is marked as affecting these facts only where the
    /// readings give them different figures.
    fn divergences(
        &self,
        f: &Facts,
        a: &Assessment,
        january_short: bool,
    ) -> Result<Vec<Divergence>, Error> {
        let married = !a.exempt && f.filing_status() == filing::Status::MarriedFilingSeparately;
        let mut out = Vec::new();
        for b in [
            working::new_divergence("No liability last year")
                .form("No interest is due if you didn't have to file last year, wherever you lived.",
                    "OW-8-P (2025), Part 1 line 7")
                .statute("No interest if you lived in Oklahoma all of a 12-month prior year. You must also have had no liability for it. Someone who wasn't a resident all year isn't covered.",
                    "68 O.S. § 2385.13(C)")
                .uses(Reading::Form).affects(a.residence_open).clone(),
            working::new_divergence("A return filed and paid by January 31")
                .form("No form waives the January 15 installment for filing and paying by January 31. OW-8-P has no step for it.",
                    "OW-8-ES (revised 6-2025); OW-8-P (2025), Part 3; Form 511 instructions (2025), line 40")
                .statute("Filing and paying the tax due by January 31 waives the January 15 installment.",
                    "68 O.S. § 2385.9(A)(1)")
                .uses(Reading::Form).affects(a.january_waiver && january_short).clone(),
            working::new_divergence("Who must make estimated payments")
                .form("Payments are required when tax will exceed withholding by $500 or more. Withholding must also be under the smaller of two figures. They are 70% of this year's tax and last year's tax.",
                    "OW-8-ES (revised 6-2025), Who Must Make Estimated Payments; Form 511 instructions (2025), Estimated Income Tax")
                .statute("Payments are required when tax is expected to top withholding by $500 or more. It sets no condition on withholding reaching the required annual payment.",
                    "68 O.S. § 2385.7(A)")
                .uses(Reading::Form).affects(a.withholding_rule).clone(),
            working::new_divergence("Married individuals")
                .form("Each return tests its own tax and withholding, whatever the filing status.",
                    "OW-8-ES (revised 6-2025), worksheet and Who Must Make Estimated Payments")
                .statute("For married people the test is their combined tax over withholding. A spouse filing separately is tested with the other spouse's figures too.",
                    "68 O.S. § 2385.7(A)(2)")
                .uses(Reading::Form).affects(married).clone(),
            working::new_divergence("The $1,000 test")
                .form("No interest if tax after refundable and nonrefundable credits is under $1,000.",
                    "OW-8-P (2025), Part 1 lines 1 to 3; Form 511 instructions (2025), line 40")
                .statute("No interest if the tax shown on the return is under $1,000. It doesn't say whether that is before or after credits.",
                    "68 O.S. § 2385.13(C)")
                .uses(Reading::Form).affects(a.small_tax_exempt).clone(),
            working::new_divergence("The farmer share")
                .form("A farmer has at least 66.67% (two-thirds) of gross income from farming. That can be this year or last.",
                    "Form 511 instructions (2025), Estimated Income Tax and line 22; OW-8-ES (revised 6-2025), Who Must Make Estimated Payments")
                .statute("At least sixty-six and two-thirds percent (66 2/3%), this year or last year. There is no carry-forward beyond one year. Both come to the same share.",
                    "68 O.S. § 2385.7(B)(2)")
                .uses(Reading::Form).affects(false).clone(),
        ] {
            out.push(b.build()?);
        }
        Ok(out)
    }

    fn assess(&self, f: &Facts) -> Result<Assessment, Error> {
        self.check(f)?;
        let extras = self.extras(f)?;
        let y = &self.y;
        let mut a = Assessment::default();

        let tax_opt = f.this_year().tax();
        let tax_known = tax_opt.is_some();
        let tax = tax_opt.unwrap_or_default();
        a.tax_known = tax_known;
        let withheld = f.withheld_total();
        let has_prior = f.prior_year().is_some();
        let prior = f.prior_year().unwrap_or_default();
        let last = y.year - 1;
        let prior_tax = prior.figures().and_then(|pf| pf.tax()).unwrap_or_default();

        // § 2385.9(B)(1): 70% of this year's tax.
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
                "68 O.S. § 2385.9(B)(1); OW-8-P (2025), Part 1 line 4",
                format!(
                    "{} of this year's Oklahoma tax of {tax} is {}",
                    y.this_year_share,
                    this_year.amount()
                ),
            );
        }

        // § 2385.9(B)(2): 100% of the tax on last year's 12-month return.
        // There is no higher share at any income.
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
                format!("Did you file a {last} Oklahoma return, and what was its tax?"),
                format!(
                    "Your target could then be {} of last year's tax, if under {} of this year's.",
                    y.prior_year_share, y.this_year_share
                ),
            ));
            unavailable(format!("No {last} Oklahoma return on record"))?
        } else if prior.not_required_to_file() {
            unavailable(format!(
                "You weren't required to file a {last} Oklahoma return"
            ))?
        } else if !prior.filed() {
            unavailable(format!("You didn't file a {last} Oklahoma return"))?
        } else if prior.months() != 12 {
            unavailable(format!(
                "Your {last} Oklahoma return covered {} months, not 12",
                prior.months()
            ))?
        } else {
            a.notes.rule(
                "68 O.S. § 2385.9(B)(2); OW-8-P (2025), Part 1 line 7",
                format!(
                    "{} of the {prior_tax} tax on the 12-month {last} return is {}; Oklahoma has no higher share at higher income",
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
                    "What do you expect your {} Oklahoma tax after credits to be?",
                    y.year
                ),
                estimated::without_this_year_tax(prior_opt.available()),
            ));
        }

        // § 2385.7(B)(2): a farmer this year or last is not required to pay.
        let prior_farm = extras.prior_year_farm_share();
        let farmer = y.farm_share.reaches(f.farm_share())
            || prior_farm.is_some_and(|r| y.farm_share.reaches(r));

        if farmer {
            a.exempt = true;
            let mut year = y.year;
            if !y.farm_share.reaches(f.farm_share()) {
                year = last;
            }
            let ex = estimated::new_exception("Two-thirds of gross income from farming").build()?;
            rb.required(
                false,
                format!(
                    "At least two-thirds of your {year} gross income is from farming. Oklahoma requires no estimated payments for {} and charges no interest. Mark the box on Form 511 line 22 and include your federal return.",
                    y.year
                ),
            )
            .exempt(ex);
            a.notes.rule(
                "68 O.S. § 2385.7(B)(2); Form 511 instructions (2025), line 22",
                format!(
                    "farming was at least 66 2/3% of gross income in {year}, so no estimated payments are required for {}",
                    y.year
                ),
            );
        } else if has_prior && prior.not_required_to_file() {
            a.exempt = true;
            let ex = estimated::new_exception("Not required to file last year").build()?;
            rb.required(
                false,
                format!(
                    "You weren't required to file an Oklahoma return for {last}. Oklahoma's form then charges no {} interest and needs no estimated payments.",
                    y.year
                ),
            )
            .exempt(ex);
            a.notes.rule(
                "OW-8-P (2025), Part 1 line 7",
                format!(
                    "no {last} return was required, so there is no interest for {}",
                    y.year
                ),
            );
            let resident = prior.full_year_resident();
            a.residence_open = resident != Some(true);
            match resident {
                None => {
                    a.notes.ask(
                        PositionKind::Residency,
                        format!("Did you live in Oklahoma all of {last}?"),
                        "Until then the figures follow the form, excusing anyone not required to file. The law excuses only a full-year resident.",
                    );
                }
                Some(false) => {
                    a.notes.ask(
                        PositionKind::UnsettledRule,
                        "",
                        format!(
                            "The form excuses anyone not required to file, and the figures follow it. The law excuses only full-year residents and would use {} of this year's tax.",
                            y.this_year_share
                        ),
                    );
                }
                Some(true) => {}
            }
        } else if !tax_known && !prior_opt.available() {
            rb.required(
                false,
                format!(
                    "With no full-year {last} Oklahoma return, your target is {} of your {} tax.",
                    y.this_year_share, y.year
                ),
            )
            .no_target();
        } else if !tax_known && withheld >= target {
            rb.required(
                false,
                format!(
                    "Your withholding of {} already reaches {} of your {last} tax, {}. No estimated payments are needed, whatever your {} tax is.",
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
                    "Your target is {} of your {last} tax, {}. That keeps you clear of interest whatever your {} tax is. {}",
                    prior_opt.percent(),
                    target.whole(),
                    y.year,
                    left_why(target, withheld)
                ),
            )
            .choose(chosen);
        } else if tax < y.small_tax {
            a.exempt = true;
            a.small_tax_exempt = true;
            let below = estimated::exception_below("Tax after credits", tax, y.small_tax)?;
            rb.required(
                false,
                format!(
                    "Your Oklahoma tax after credits of {} is under {}. No interest is charged, and no estimated payments are needed.",
                    tax.whole(),
                    y.small_tax.whole()
                ),
            )
            .exempt(below);
            a.notes.rule(
                "68 O.S. § 2385.13(C); OW-8-P (2025), Part 1 line 3",
                format!(
                    "{tax} of tax after credits is under {}, so there is no interest",
                    y.small_tax
                ),
            );
            a.notes.ask(
                PositionKind::UnsettledRule,
                "",
                format!(
                    "The law doesn't say if the {} test is on tax before or after credits. The figures follow the form's tax after credits.",
                    y.small_tax.whole()
                ),
            );
        } else if tax - withheld < y.threshold {
            a.exempt = true;
            let below =
                estimated::exception_below("Tax less withholding", tax - withheld, y.threshold)?;
            let why = if withheld > tax {
                format!(
                    "Your Oklahoma tax of {} less {} withheld {}. No estimated payments are needed.",
                    tax.whole(),
                    withheld.whole(),
                    estimated::leaves(tax - withheld)
                )
            } else if withheld == tax {
                format!(
                    "Your Oklahoma withholding of {} covers your {} tax. No estimated payments are needed.",
                    withheld.whole(),
                    tax.whole()
                )
            } else {
                format!(
                    "{} That's under {}. No estimated payments are needed, and no interest can arise.",
                    owed_why(tax, withheld),
                    y.threshold.whole()
                )
            };
            rb.required(false, why).exempt(below);
            a.notes.rule(
                "68 O.S. § 2385.7(A); OW-8-P (2025), Part 1 line 6",
                format!(
                    "{tax} of tax less {withheld} withheld is {}, under {}, so there is no interest",
                    tax - withheld,
                    y.threshold
                ),
            );
        } else if withheld >= target {
            a.withholding_rule = true;
            let why = if target.0 == 0 {
                format!(
                    "Your {last} Oklahoma tax was $0, so your target is $0. No estimated payments are needed."
                )
            } else {
                format!(
                    "Your Oklahoma withholding of {} reaches the year's {} target. No estimated payments are needed.",
                    withheld.whole(),
                    target.whole()
                )
            };
            rb.required(false, why).choose(chosen);
            a.notes.rule(
                "OW-8-P (2025), Part 1 line 8",
                format!(
                    "{withheld} of withholding is at least the required annual payment of {target}, so there is no interest"
                ),
            );
        } else {
            rb.required(
                true,
                format!(
                    "{} That's at least {}, so estimated payments are needed. {}",
                    owed_why(tax, withheld),
                    y.threshold.whole(),
                    target_why(target, withheld)
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
        if !a.exempt && prior_farm.is_none() {
            a.notes.ask(
                PositionKind::JurisdictionFact,
                format!("What share of your {last} gross income came from farming?"),
                format!(
                    "If it was two-thirds or more, no estimated payments are needed for {}.",
                    y.year
                ),
            );
        }
        if !a.exempt && f.filing_status() == filing::Status::MarriedFilingSeparately {
            a.notes.rule(
                "68 O.S. § 2385.7(A)(2); OW-8-ES (revised 6-2025)",
                "this return's own tax and withholding are tested, as the form does; the statute tests married individuals' combined liability",
            );
        }

        // § 2385.9(A) fixes the dates. OW-8-ES moves one that falls on a
        // weekend or a day the OTC is closed to the next business day.
        a.dues = estimated::move_dates(&y.calendar, &y.dues);
        for i in 0..a.dues.len() {
            if a.dues[i] != y.dues[i] {
                a.notes.rule(
                    "OW-8-ES (revised 6-2025), Due Dates; 25 O.S. § 82.1",
                    format!(
                        "the installment due {} falls on a weekend or legal holiday, so it is due {}",
                        y.dues[i].long(),
                        a.dues[i].long()
                    ),
                );
            }
        }
        let cumulative = estimated::cumulative_shares(a.requirement.target(), &y.shares)?;
        a.installments = estimated::increments(&cumulative);
        a.notes.rule(
            "68 O.S. § 2385.9(A); OW-8-P (2025), Part 2 line 9",
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
            let taxes: Vec<USCents> = f.periods().iter().map(|p| p.annualised_tax()).collect();
            let ann = estimated::annualised(&taxes, &y.annualised, &a.installments)?;
            a.notes.rule(
                "68 O.S. § 2385.9(C); OW-8-P-SUP-I (2025), lines 16 to 24",
                format!(
                    "under the annualised method the installments are {}, {}, {} and {}: {}, {}, {} and {} of the annualised tax, \
                     less earlier installments, and never more than the regular quarter plus any reduction not yet made up",
                    ann[0],
                    ann[1],
                    ann[2],
                    ann[3],
                    y.annualised[0],
                    y.annualised[1],
                    y.annualised[2],
                    y.annualised[3]
                ),
            );
            a.installments = ann;
        }

        // § 2385.9(A)(1): filing and paying by January 31 waives the January
        // installment. January 31, 2027 is a Sunday, and 25 O.S. § 82.1(C)
        // lets an act due on a holiday be done the next business day. The
        // forms do not apply the waiver, so it is reported, not applied.
        a.file_by = y.calendar.next_business_day(y.file_and_pay_by);
        let filed = f.return_filed();
        if !filed.is_zero() && !filed.after(a.file_by) && !a.exempt {
            a.january_waiver = true;
            let mut applied = format!(
                "the return was filed and paid on {}, by {}",
                filed.long(),
                a.file_by.long()
            );
            if a.file_by != y.file_and_pay_by {
                applied += &format!(
                    " ({} is a weekend or legal holiday, so the date moves to {})",
                    y.file_and_pay_by.long(),
                    a.file_by.long()
                );
            }
            a.notes.rule(
                "68 O.S. § 2385.9(A)(1); 25 O.S. § 82.1(C)",
                format!(
                    "{applied}; the statute waives the January 15 installment, but the forms have no step for it, so it is not waived"
                ),
            );
        }

        if a.exempt {
            a.installments = vec![USCents(0); a.dues.len()];
        }
        let (ledger, _) = self.ledger(f, &a)?;
        let january_short = ledger.last().is_some_and(|l| l.shortfall().0 > 0);
        let divergences = self.divergences(f, &a, january_short)?;
        a.notes.diverges(&divergences);
        Ok(a)
    }

    /// Applies withholding and payments to the installments as OW-8-P Part 3
    /// does: withholding a quarter on each due date unless the filer shows
    /// the dates it was withheld, payments on their dates, and each payment
    /// to the earliest balance first.
    fn ledger(&self, f: &Facts, a: &Assessment) -> Result<(Vec<LedgerEntry>, Notes), Error> {
        let credits = if f.elections().withholding_as_withheld() {
            estimated::credits_as_withheld(f, &a.dues)?
        } else {
            estimated::credits(f, &a.dues, &self.y.shares)?
        };
        let ledger = estimated::apply(&a.dues, &a.installments, &credits)?;
        let mut notes = Notes::default();
        if f.elections().withholding_as_withheld() {
            notes.rule(
                "OW-8-P (2025), line 10",
                format!(
                    "the {} withheld is credited on the dates it was withheld, as the form allows when you can show them",
                    f.withheld_total()
                ),
            );
            notes.ask(
                PositionKind::UnsettledRule,
                "",
                "Oklahoma's form allows withholding on the dates it was taken. It gives no way to show them, and the figures use those dates.",
            );
        } else {
            notes.rule(
                "OW-8-P (2025), line 10 and Part 3",
                format!(
                    "the {} withheld is treated as paid in four equal parts on the due dates, whenever it was actually withheld",
                    f.withheld_total()
                ),
            );
        }
        Ok((ledger, notes))
    }

    /// The schedule from the ledger.
    fn installments(
        &self,
        a: &Assessment,
        ledger: &[LedgerEntry],
    ) -> Result<Vec<Installment>, Error> {
        let mut out = Vec::with_capacity(ledger.len());
        let mut needed = USCents(0);
        for (i, l) in ledger.iter().enumerate() {
            needed += a.installments[i];
            out.push(
                estimated::new_installment(i as i64 + 1, l.due())
                    .needed_by(needed)
                    .credited_by(l.credited_by())
                    .shortfall(l.shortfall())
                    .build()?,
            );
        }
        Ok(out)
    }
}

impl EstimatedTax for OklahomaEstimated {
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
                    "with no {} Oklahoma return to measure against, there is no target to schedule",
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

    /// The interest as OW-8-P Part 3 figures it: each installment's balance,
    /// after the payments applied to it earliest first, times the days it
    /// stays unpaid, times 20%, divided by 365. It runs from the due date to
    /// the day paid, the day the return is filed with the balance, or April
    /// 15 of the next year, whichever is earliest.
    fn penalty(&self, f: &Facts, as_of: Date) -> Result<working::Result<Penalty>, Error> {
        if as_of.is_zero() {
            return Err(Error::Invalid(
                "oklahomataxcalc: interest needs the date to figure it to".to_string(),
            ));
        }
        let a = self.assess(f)?;
        if !a.requirement.target_known() {
            return Err(Error::with_detail(
                Error::NeedsThisYearTax,
                format!(
                    "with no {} Oklahoma return to measure against, there is no target to figure interest from",
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
        if !unrated.is_zero() {
            // The statute's rate covers every day to the stop, so this is a
            // defect in the year's table.
            return Err(Error::Invalid(format!(
                "oklahomataxcalc: the 2026 rates do not reach {unrated}"
            )));
        }
        if !a.tax_known {
            p = p.to_builder().upper_bound().build()?;
            notes.rule(
                "68 O.S. § 2385.9(B)",
                format!(
                    "this year's tax was not given, so the interest is figured against {} of the {} tax alone; \
                     the required annual payment is the lesser of that and {} of this year's tax, so {} is the most the interest can be, and it is less if this year's tax turns out lower",
                    a.requirement.options()[1].percent(),
                    y.year - 1,
                    y.this_year_share,
                    p.total()
                ),
            );
        }
        if paid_in_full {
            notes.rule(
                "68 O.S. § 2385.13(B); OW-8-P (2025), Part 3, Column A",
                format!(
                    "the return was filed with the whole balance paid on {}, so every balance still outstanding was paid that day",
                    stop.long()
                ),
            );
        }
        if a.january_waiver {
            for l in p.lines() {
                if l.installment() == 4 && l.total().0 > 0 {
                    notes.rule(
                        "68 O.S. § 2385.9(A)(1)",
                        format!(
                            "the January 15 installment carries {} of interest, which the statute's waiver would remove; the forms do not apply it, so it is included",
                            l.total()
                        ),
                    );
                }
            }
        }
        let rate = y
            .rates
            .periods()
            .first()
            .map(|r| r.rate())
            .unwrap_or_default();
        notes.rule(
            "68 O.S. § 2385.13(A), (B); OW-8-P (2025), Part 3",
            format!(
                "each installment's balance is charged {rate} a year, by the day over 365, from its due date until paid or {}, \
                 with each payment applied to the earliest balance first; the interest to {} is {}",
                y.stop.long(),
                stop.long(),
                p.total()
            ),
        );
        if waiver_answer.is_some() && waiver != WaiverKind::None {
            notes.rule(
                "68 O.S. §§ 2385.7, 2385.13",
                format!(
                    "Oklahoma's estimated tax statutes have no waiver for {waiver}; after a disaster the Tax Commission postpones due dates by order, which is not built; the interest shown is not reduced"
                ),
            );
            if p.total().0 > 0 {
                let o = if waiver == WaiverKind::Casualty {
                    estimated::waiver_refused(
                        "Oklahoma has no waiver of this interest for a casualty or disaster. After some disasters, the Tax Commission postpones the due dates by order.",
                    )
                } else {
                    estimated::waiver_refused(
                        "Oklahoma has no waiver of this interest for retirement or disability.",
                    )
                };
                p = p.to_builder().waiver_outcome(o).build()?;
            }
        }
        Ok(working::new_result(p).with(&a.notes).with(&notes).build()?)
    }

    fn explanation(&self) -> Explanation {
        OKLAHOMA_EXPLANATION.clone()
    }
}

/// Written by hand and checked against research/estimated-payments/ok.md.
static OKLAHOMA_EXPLANATION: LazyLock<Explanation> = LazyLock::new(|| {
    estimated::new_explanation(
        "Oklahoma expects estimated payments once tax tops withholding by $500 or more. Paying too little by a due date costs interest of 20% a year on the shortfall.")
        .paragraph("You avoid interest if withholding plus estimated payments reach a target. It's the smaller of 70% of this year's tax or all the tax on last year's return. Last year's figure needs a twelve-month return and is the same at every income.")
        .paragraph("A quarter is due April 15, June 15, September 15 and the next January 15. A date on a weekend or Tax Commission closure shifts to the next business day. Withholding counts as paid evenly across the four dates, whenever it was taken.")
        .paragraph("There is no interest if this year's tax after credits is under $1,000. There is none if it is less than $500 above your withholding. Nor is there any if you didn't have to file an Oklahoma return last year.")
        .paragraph("Oklahoma calls the charge interest. It is 20% a year, set by statute, and charged by the day from each due date. It runs until the shortfall is paid, or April 15 of the next year if sooner. A payment goes first to the oldest shortfall. For uneven income, the annualised method can lower early payments. Its form is OW-8-P-SUP-I. The statute also waives the January payment if you file and pay by January 31. The Tax Commission's forms don't apply that. There is no waiver for a casualty, retirement or disability.")
        .versus_federal("Unlike federal, payments start at $500 over withholding, not $1,000. This year's share is 70%, not 90%. Last year's share is 100% at every income, with no 110% tier. The interest is a fixed 20% a year. The annualised percentages are 17.5%, 35%, 52.5% and 70%. There's no casualty or disability waiver.")
        .source("68 O.S. §§ 2385.7, 2385.9, 2385.13")
        .source("Form OW-8-ES, Oklahoma Individual Estimated Tax, tax year 2026 (revised 6-2025)")
        .source("Form OW-8-P, Underpayment of Estimated Tax Worksheet, 2025")
        .source("Forms OW-8-ES-SUP (2026) and OW-8-P-SUP-I (2025), annualised income installment method")
        .source("2025 Oklahoma Resident Individual Income Tax Forms and Instructions (Form 511 packet), lines 22 and 40")
        .build()
        .unwrap_or_default()
});
