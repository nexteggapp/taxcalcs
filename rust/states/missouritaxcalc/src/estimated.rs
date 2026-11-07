//! Missouri's estimated tax rules, RSMo §§ 143.521, 143.531 and 143.761, as
//! the shared [`EstimatedTax`] interface.
//!
//! Missouri keeps the federal rules of the 1980s. The required annual payment
//! is the lesser of 90% of this year's tax and 100% of last year's, with no
//! 110% tier. Four exceptions each excuse an installment whose payments reach
//! them: last year's tax, last year's income at this year's rates, 90% of the
//! tax on annualized income, and 90% of the tax on income to date. A late
//! starter pays fewer, larger installments. The rate is set for each calendar
//! year.
//!
//! The figure follows Form MO-2210 for 2025, the latest published, with the
//! 2026 dates and rates, and the result's Divergences say where the statute
//! reads otherwise. The revisor shows no amendment of §§ 143.521 (1998),
//! 143.531 (1990) or 143.761 (1989), so the form does not predate the law it
//! describes (research/estimated-payments/mo.md).
//!
//! Kansas City's and St. Louis's earnings taxes are collected by the cities
//! and are not covered.

use std::any::Any;

use estimated::{
    Basis, Calendar, Date, Error, EstimatedTax, Explanation, Facts, Installment, Month, Payment,
    Penalty, PenaltyLineBuilder, RateSeries, Requirement, TargetOptionBuilder, WaiverKind,
};
use money::{Rate, USCents};
use working::{Divergence, Notes, PositionKind, Reading};

/// Missouri's postal code.
const JURISDICTION: estimated::Jurisdiction = estimated::MISSOURI;

/// The facts Missouri's rules read that no other jurisdiction does. Carry
/// them in [`estimated::FactsBuilder::state`].
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct EstimatedFacts {
    prior_income_tax: Option<USCents>,

    tax_to_date: Option<Vec<USCents>>,

    combat_from: Date,
    combat_to: Date,
    combat_answered: bool,

    statutory_threshold: bool,
    threshold_answered: bool,
    line_five: bool,
    line_five_answered: bool,
}

impl EstimatedFacts {
    /// The tax on last year's income at this year's rates and exemptions,
    /// MO-2210 Exception 2.
    pub fn prior_year_income_at_this_years_rates(&self) -> Option<USCents> {
        self.prior_income_tax
    }

    /// The tax on the taxable income from January 1 to March 31, May 31 and
    /// August 31, each figured as if it were the year's, MO-2210 Exception 4.
    pub fn tax_on_income_to_date(&self) -> Option<Vec<USCents>> {
        self.tax_to_date.clone()
    }

    /// Service in a combat zone under conditions that qualified for hostile
    /// fire pay, MO-2210 instruction I: from, to, and whether the caller
    /// said. The installments due during service are exempt. A zero span
    /// means no service.
    pub fn combat_zone(&self) -> (Date, Date, bool) {
        (self.combat_from, self.combat_to, self.combat_answered)
    }

    /// Whether the penalty is figured whenever tax less withholding is $100
    /// or more, the statute's declaration threshold, rather than the form's
    /// $500, and whether the caller said: `(apply, answered)`.
    pub fn statutory_threshold(&self) -> (bool, bool) {
        (self.statutory_threshold, self.threshold_answered)
    }

    /// Whether last year's tax counts toward the required annual payment
    /// whatever last year's return showed, as MO-2210 Part I line 5 is
    /// printed, rather than only for a twelve-month return showing a
    /// liability, as Exception 1 and the statute say; and whether the caller
    /// said: `(as_printed, answered)`.
    pub fn line_five_as_printed(&self) -> (bool, bool) {
        (self.line_five, self.line_five_answered)
    }
}

/// Builds [`EstimatedFacts`].
#[derive(Clone, Debug, Default)]
pub struct EstimatedFactsBuilder {
    e: EstimatedFacts,
}

/// Starts Missouri's extras.
pub fn new_estimated_facts() -> EstimatedFactsBuilder {
    EstimatedFactsBuilder::default()
}

impl EstimatedFactsBuilder {
    /// Gives the tax on last year's income at this year's rates.
    pub fn prior_year_income_at_this_years_rates(&mut self, a: USCents) -> &mut Self {
        self.e.prior_income_tax = Some(a);
        self
    }

    /// Gives the tax on the income to March 31, May 31 and August 31.
    pub fn tax_on_income_to_date(
        &mut self,
        march: USCents,
        may: USCents,
        august: USCents,
    ) -> &mut Self {
        self.e.tax_to_date = Some(vec![march, may, august]);
        self
    }

    /// Records service in a combat zone from one day to another.
    pub fn combat_zone(&mut self, from: Date, to: Date) -> &mut Self {
        (self.e.combat_from, self.e.combat_to, self.e.combat_answered) = (from, to, true);
        self
    }

    /// Answers that there was no service in a combat zone.
    pub fn no_combat_zone(&mut self) -> &mut Self {
        (self.e.combat_from, self.e.combat_to, self.e.combat_answered) =
            (Date::default(), Date::default(), true);
        self
    }

    /// Answers whether the statute's $100 threshold applies.
    pub fn statutory_threshold(&mut self, apply: bool) -> &mut Self {
        (self.e.statutory_threshold, self.e.threshold_answered) = (apply, true);
        self
    }

    /// Answers whether MO-2210 line 5 is read as printed.
    pub fn line_five_as_printed(&mut self, as_printed: bool) -> &mut Self {
        (self.e.line_five, self.e.line_five_answered) = (as_printed, true);
        self
    }

    /// Refuses negative taxes and a combat zone span that does not run
    /// forward.
    pub fn build(&self) -> Result<EstimatedFacts, Error> {
        let e = &self.e;
        if e.prior_income_tax.is_some_and(|a| a.0 < 0) {
            return Err(Error::Invalid(
                "missouritaxcalc: the tax on last year's income cannot be negative".into(),
            ));
        }
        if e.tax_to_date.iter().flatten().any(|t| t.0 < 0) {
            return Err(Error::Invalid(
                "missouritaxcalc: the tax on income to date cannot be negative".into(),
            ));
        }
        if e.combat_from.is_zero() != e.combat_to.is_zero() || e.combat_to.before(e.combat_from) {
            return Err(Error::Invalid(
                "missouritaxcalc: combat zone service needs a start and an end, in order".into(),
            ));
        }
        Ok(e.clone())
    }
}

/// One tax year's published figures.
pub(crate) struct EstimatedYear {
    pub(crate) year: i64,

    /// MO-2210 line 4, and `declaration` RSMo 143.521.1.
    pub(crate) threshold: USCents,
    pub(crate) declaration: USCents,

    /// MO-2210 lines 2 and 5; `income_share` is the 90% of Exceptions 3 and
    /// 4.
    pub(crate) this_year_share: Rate,
    pub(crate) prior_year_share: Rate,
    pub(crate) income_share: Rate,

    /// RSMo 143.761.5(2): an equal part on each date.
    pub(crate) withholding_shares: Vec<Rate>,

    pub(crate) dues: Vec<Date>,
    pub(crate) calendar: Calendar,

    /// The first days of the MO-2210 instruction B table's second, third and
    /// fourth rows: a requirement first met on or after `later_groups[i]`
    /// starts at installment i+2.
    pub(crate) later_groups: Vec<Date>,
    pub(crate) period_ends: Vec<Date>,

    /// MO-2210 instruction H: the balance paid with a return filed by
    /// `file_and_pay_by` counts as paid on `deemed_paid`.
    pub(crate) file_and_pay_by: Date,
    pub(crate) deemed_paid: Date,

    pub(crate) stop: Date,
    pub(crate) rates: RateSeries,
}

/// Returns Missouri's estimated tax rules for a tax year, or
/// [`Error::NoRules`] for a year this build does not carry.
pub fn estimated_tax(year: i64) -> Result<Box<dyn EstimatedTax>, Error> {
    let y = match year {
        2026 => crate::estimated_y2026::year_2026()?,
        _ => return Err(estimated::no_rules(&JURISDICTION, year)),
    };
    Ok(Box::new(MissouriEstimated { y }))
}

struct MissouriEstimated {
    y: EstimatedYear,
}

#[derive(Default)]
struct Assessment {
    requirement: Requirement,
    exempt: bool,
    tax_known: bool,
    dues: Vec<Date>,

    /// MO-2210 line 15 added up: what is required by each date.
    cumulative: Vec<USCents>,

    /// The installment a late starter's requirement starts at.
    first: i64,

    /// The running totals that excuse an installment, by exception, or
    /// empty where it is not open.
    exceptions: [Vec<USCents>; 4],

    combat_from: Date,
    combat_to: Date,
    combat_answered: bool,
    filed_by_jan31: bool,

    // What the divergences turn on: tax less withholding between the
    // statute's threshold and the form's, a prior-year return whose line 5
    // reading would change the target, and the readings answered.
    in_threshold_gap: bool,
    line_five_changes: bool,
    statutory: bool,
    line_five_printed: bool,
    tax_to_date: Vec<USCents>,

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

/// A shorthand for the year files.
pub(crate) fn date(y: i64, m: Month, d: i64) -> Date {
    estimated::must_date(y, m, d)
}

impl EstimatedYear {
    /// Prints `d` for a sentence: "Aug 31" in the tax year, "Jan 15, 2027"
    /// outside it.
    fn date(&self, d: Date) -> String {
        let m: String = d.month().to_string().chars().take(3).collect();
        if d.year() == self.year {
            return format!("{m} {}", d.day());
        }
        format!("{m} {}, {}", d.day(), d.year())
    }
}

impl Assessment {
    /// Why installment `i` bears no penalty, or "" when it does.
    fn excused(&self, i: usize, credited_by: USCents) -> &'static str {
        if !self.combat_from.is_zero()
            && !self.dues[i].before(self.combat_from)
            && !self.dues[i].after(self.combat_to)
        {
            return "instruction I: the installment fell due during service in a combat zone";
        }
        const NAMES: [&str; 4] = [
            "Exception 1, last year's tax",
            "Exception 2, last year's income at this year's rates",
            "Exception 3, annualized income",
            "Exception 4, income to date",
        ];
        for (k, ex) in self.exceptions.iter().enumerate() {
            if !ex.is_empty() && ex[i].0 >= 0 && i as i64 >= self.first && credited_by >= ex[i] {
                return NAMES[k];
            }
        }
        ""
    }
}

impl MissouriEstimated {
    fn extras(&self, f: &Facts) -> Result<EstimatedFacts, Error> {
        match f.state() {
            None => Ok(EstimatedFacts::default()),
            Some(s) => match s.downcast_ref::<EstimatedFacts>() {
                Some(e) => Ok(e.clone()),
                None => Err(Error::Invalid(format!(
                    "missouritaxcalc: the facts carry {}, not missouritaxcalc.EstimatedFacts",
                    type_name(s)
                ))),
            },
        }
    }

    fn check(&self, f: &Facts) -> Result<(), Error> {
        if f.jurisdiction() != JURISDICTION {
            return Err(Error::Invalid(format!(
                "missouritaxcalc: these are Missouri's rules, and the facts are for {}",
                f.jurisdiction()
            )));
        }
        if f.tax_year() != self.y.year {
            return Err(Error::Invalid(format!(
                "missouritaxcalc: these are the {} rules, and the facts are for {}",
                self.y.year,
                f.tax_year()
            )));
        }
        if i64::from(f.farm_share().0) * 3 >= 20_000 {
            return Err(Error::with_detail(
                Error::Unsupported,
                "a farmer with two-thirds of Missouri gross income from farming pays once at 66 2/3%, which is not built",
            ));
        }
        if f.elections().annualised() {
            for (i, p) in f.periods().iter().enumerate() {
                let want = self.y.period_ends.get(i).copied().unwrap_or_default();
                if p.end() != want {
                    return Err(Error::Invalid(format!(
                        "missouritaxcalc: MO-2210 Exception 3 period {} ends on {}, not {}",
                        i + 1,
                        want,
                        p.end()
                    )));
                }
            }
        }
        Ok(())
    }

    /// Where Form MO-2210 and the statute part ways on a rule every Missouri
    /// computation uses. The $500 stop and the zero prior year are in the
    /// research file; the other three were found reading MO-2210 against
    /// § 143.761. Each says whether it changes anything for `f`.
    fn divergences(&self, f: &Facts, a: &Assessment) -> Result<Vec<Divergence>, Error> {
        let (mut prior_short, mut to_date, mut carried) = (false, false, false);
        if !a.exempt && a.requirement.target_known() {
            let (credits, _) = self.credits(f, a)?;
            let ledger =
                estimated::apply(&a.dues, &estimated::increments(&a.cumulative), &credits)?;
            let c = a.requirement.chosen().unwrap_or_default();
            for (i, l) in ledger.iter().enumerate() {
                let short = l.credited_by() < a.cumulative[i];
                prior_short = prior_short || (short && c.basis() == Basis::PriorYearTax);
                if short && i < ledger.len() - 1 && !a.excused(i, l.credited_by()).is_empty() {
                    carried = true;
                }
                // Exception 4 by the form's 90% and by the statute's share of
                // it.
                if short && i < a.tax_to_date.len() && i as i64 >= a.first {
                    let shares = estimated::equal_shares(
                        self.y.income_share.of(a.tax_to_date[i]),
                        a.dues.len() as i64,
                        a.first,
                    )?;
                    if (l.credited_by() >= a.exceptions[3][i]) != (l.credited_by() >= shares[i]) {
                        to_date = true;
                    }
                }
            }
        }
        let stop_reading = if a.statutory {
            Reading::Statute
        } else {
            Reading::Form
        };
        let line_five_reading = if a.line_five_printed {
            Reading::Form
        } else {
            Reading::Statute
        };
        let mut out = Vec::new();
        for b in [
            working::new_divergence("What the penalty is measured against")
                .form("The required annual payment is the smaller of two amounts. They are 90% of this year's tax and last year's tax. The penalty is on each installment's shortfall against a quarter of it.",
                    "Form MO-2210 (2025), Part I lines 2 to 6 and Part III line 15")
                .statute("The shortfall is measured against installments of 90% of this year's tax. Last year's tax and the other tests only excuse an installment paid up to them.",
                    "RSMo 143.761.2, 143.761.4")
                .uses(Reading::Form)
                .affects(prior_short)
                .clone(),
            working::new_divergence("The $500 stop")
                .form("No penalty when tax after credits less withholding is under $500.",
                    "Form MO-2210 (2025), Part I line 4")
                .statute("Estimated payments are required once estimated tax reaches $100. That is the tax after credits and withholding. The penalty applies whenever they were required.",
                    "RSMo 143.521.1; 12 CSR 10-2.067(3)")
                .uses(stop_reading)
                .affects(a.in_threshold_gap)
                .clone(),
            working::new_divergence("A prior-year return with no tax, or a short year")
                .form("Line 5 takes last year's tax with no condition. A $0 tax there gives a required payment of $0. Exception 1 on the same form needs a twelve-month return showing tax.",
                    "Form MO-2210 (2025), Part I line 5 and instruction D, Exception 1")
                .statute("Last year's tax counts only from a twelve-month return that showed tax.",
                    "RSMo 143.761.4(1)")
                .uses(line_five_reading)
                .affects(a.line_five_changes)
                .clone(),
            working::new_divergence("Exception 4, tax on income to date")
                .form("Payments by each of the first 3 dates must reach 90% of the tax on the income. That income runs to the end of the month before the date.",
                    "Form MO-2210 (2025), line 24 and instruction D, Exception 4 and Example II")
                .statute("Payments must reach a quarter, a half and three-quarters of 90% of that tax. That is what each date would require if 90% of that tax were the estimated tax.",
                    "RSMo 143.761.4(3)")
                .uses(Reading::Form)
                .affects(to_date)
                .clone(),
            working::new_divergence("An excused installment's shortfall")
                .form("Line 19d carries an excused installment's shortfall into the next column. Line 25 charges it there unless that column's own exception applies.",
                    "Form MO-2210 (2025), lines 19b to 19d and 25, and the instructions for lines 19c, 19d and 25")
                .statute("An exception lifts the penalty on its installment's shortfall until it's paid.",
                    "RSMo 143.761.3, 143.761.4")
                .uses(Reading::Form)
                .affects(carried)
                .clone(),
        ] {
            out.push(b.build()?);
        }
        Ok(out)
    }

    /// The requirement and installments, with the divergences.
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
        let prior = f.prior_year();
        let has_prior = prior.is_some();
        let prior = prior.unwrap_or_default();
        let prior_tax = prior.figures().and_then(|pf| pf.tax()).unwrap_or_default();

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
                "RSMo 143.761.2; Form MO-2210 (2025), Part I line 2",
                format!(
                    "{} of this year's Missouri tax after credits of {} is {}",
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
        let mut exception_one = false;
        let (as_printed, line_five_answered) = extras.line_five_as_printed();
        let prior_option = if !has_prior {
            unavailable(format!("Needs your {last} Missouri return"))
        } else if !prior.filed() {
            unavailable(format!("You didn't file a {last} Missouri return"))
        } else if (prior.months() != 12 || prior_tax.0 == 0) && as_printed {
            a.notes.rule(
                "Form MO-2210 (2025), Part I line 5",
                format!(
                    "as the caller answered, line 5 takes the {} shown on the {last} return though it covered {} months",
                    prior_tax,
                    prior.months()
                ),
            );
            estimated::new_target_option(
                Basis::PriorYearTax,
                y.prior_year_share,
                y.prior_year_share.of(prior_tax),
            )
        } else if prior.months() != 12 || prior_tax.0 == 0 {
            let (mut why, mut what) = (
                format!("Your {last} Missouri return showed no tax"),
                "showed no tax".to_string(),
            );
            if prior.months() != 12 {
                why = format!(
                    "Your {last} Missouri return covered {} months, not twelve",
                    prior.months()
                );
                what = format!("covered {} months", prior.months());
            }
            if !line_five_answered {
                a.notes.ask(
                    PositionKind::JurisdictionFact,
                    format!("Should MO-2210 line 5 count your {last} tax though that return {what}?"),
                    "The form's Exception 1 and the statute need a full-year return showing tax. The figures follow them and don't count it.",
                );
            }
            unavailable(why)
        } else {
            exception_one = true;
            a.notes.rule(
                "RSMo 143.761.4(1); Form MO-2210 (2025), Part I line 5",
                format!(
                    "the prior-year test is {} of the {} tax on the {last} return, or {}",
                    y.prior_year_share,
                    prior_tax,
                    y.prior_year_share.of(prior_tax)
                ),
            );
            estimated::new_target_option(
                Basis::PriorYearTax,
                y.prior_year_share,
                y.prior_year_share.of(prior_tax),
            )
        };
        let prior_opt = prior_option.build()?;

        let mut rb = estimated::new_requirement();
        rb.option(this_year.clone()).option(prior_opt.clone());
        let (mut chosen, mut target) = (Basis::ThisYearTax, this_year.amount());
        if prior_opt.available() && (!tax_known || prior_opt.amount() < target) {
            (chosen, target) = (Basis::PriorYearTax, prior_opt.amount());
        }
        if !tax_known {
            a.notes.ask(
                PositionKind::ThisYearTax,
                format!(
                    "What do you expect your {} Missouri tax after credits to be?",
                    y.year
                ),
                format!(
                    "It sets the {} option and the {} test.",
                    y.this_year_share,
                    y.threshold.whole()
                ),
            );
        }
        // Line 5 as printed takes last year's tax from any filed return; the
        // statute only from a twelve-month return showing a liability.
        if has_prior && prior.filed() && (prior.months() != 12 || prior_tax.0 == 0) {
            a.line_five_changes =
                !tax_known || y.prior_year_share.of(prior_tax) < this_year.amount();
        }
        a.line_five_printed = as_printed;

        let mut threshold = y.threshold;
        let (statutory, threshold_answered) = extras.statutory_threshold();
        if statutory {
            threshold = y.declaration;
        }
        a.statutory = statutory;
        a.in_threshold_gap =
            tax_known && tax - withheld >= y.declaration && tax - withheld < y.threshold;
        if tax_known && tax - withheld < threshold {
            a.exempt = true;
            let small =
                estimated::exception_below("Tax less withholding", tax - withheld, threshold)?;
            let mut under = format!(", under {}", threshold.whole());
            if tax < withheld {
                under = String::new();
            }
            rb.required(
                false,
                format!(
                    "Your Missouri tax is {}, and {} was withheld. That {}{under}. No estimated payments are needed.",
                    tax.whole(),
                    withheld.whole(),
                    estimated::leaves(tax - withheld)
                ),
            )
            .exempt(small);
            a.notes.rule(
                "Form MO-2210 (2025), Part I line 4",
                format!(
                    "{} of tax less {} withheld is {}, under {}, so there is no penalty",
                    tax,
                    withheld,
                    tax - withheld,
                    threshold
                ),
            );
            if !threshold_answered && tax - withheld >= y.declaration {
                a.notes.ask(
                    PositionKind::JurisdictionFact,
                    format!(
                        "Should the penalty start at {} due after withholding, as the statute says?",
                        y.declaration.whole()
                    ),
                    format!(
                        "The figures use Form MO-2210's {} start, for which no authority was found.",
                        y.threshold.whole()
                    ),
                );
            }
        } else if !tax_known && !prior_opt.available() {
            rb.required(
                false,
                format!(
                    "There's no target yet without your {} Missouri tax or a full-year {last} return.",
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
                    "Your target is {} of your {last} tax, {}, whatever your {} tax is. After {} of withholding, you need {} of estimated payments.",
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
                    "Your Missouri withholding of {} already reaches the {} target. No estimated payments are needed.",
                    withheld.whole(),
                    target.whole()
                ),
            )
            .choose(chosen);
        } else {
            rb.required(
                true,
                format!(
                    "Your Missouri tax of {} less {} of withholding leaves {}. That is {} or more, so estimated payments are required. You need {} of them to reach the {} target.",
                    tax.whole(),
                    withheld.whole(),
                    (tax - withheld).whole(),
                    threshold.whole(),
                    (target - withheld).whole(),
                    target.whole()
                ),
            )
            .choose(chosen);
        }
        a.requirement = rb.build()?;
        // Under an exception last year's return can't change the target.
        if !has_prior && !a.exempt {
            a.notes.ask(
                PositionKind::PriorYearReturn,
                format!("Did you file a {last} Missouri return, and what was its tax?"),
                format!(
                    "With it, last year's tax could be a lower target than {} of this year's.",
                    y.this_year_share
                ),
            );
        }

        // A late starter: MO-2210 instruction B and line 15.
        let n = a.dues.len() as i64;
        a.first = estimated::late_start(f.requirement_first_met(), &y.later_groups);
        a.cumulative = vec![USCents(0); a.dues.len()];
        if !a.exempt {
            a.cumulative = estimated::equal_shares(a.requirement.target(), n, a.first)?;
        }
        let installments = estimated::increments(&a.cumulative);
        a.notes.rule(
            "RSMo 143.531.1; Form MO-2210 (2025), instruction B and line 15",
            format!(
                "{} is due in {} equal installments from {}: {}, {}, {} and {} on {}, {}, {} and {}",
                a.requirement.target(),
                4 - a.first,
                a.dues[a.first as usize].long(),
                installments[0],
                installments[1],
                installments[2],
                installments[3],
                a.dues[0].long(),
                a.dues[1].long(),
                a.dues[2].long(),
                a.dues[3].long()
            ),
        );

        // The exceptions, as running totals by each date.
        if exception_one {
            a.exceptions[0] = estimated::equal_shares(prior_tax, n, a.first)?;
        }
        if let Some(amount) = extras.prior_year_income_at_this_years_rates()
            && !(has_prior && prior.not_required_to_file())
        {
            a.exceptions[1] = estimated::equal_shares(amount, n, a.first)?;
            a.notes.rule(
                "RSMo 143.761.4(4); Form MO-2210 (2025), line 22, Exception 2",
                format!(
                    "an installment is excused when payments by its date reach its share of {amount}, the tax on {last} income at {} rates",
                    y.year
                ),
            );
        }
        if f.elections().annualised() {
            let mut ex = vec![USCents(0); a.dues.len()];
            for (i, p) in f.periods().iter().take(3).enumerate() {
                let shares =
                    estimated::equal_shares(y.income_share.of(p.annualised_tax()), n, a.first)?;
                ex[i] = shares[i];
            }
            ex[3] = USCents(-1);
            a.notes.rule(
                "RSMo 143.761.4(2); Form MO-2210 (2025), line 23, Exception 3",
                format!(
                    "the first three installments are also excused when payments reach {}, {} and {}: 90% of the tax on annualized income, a quarter, half and three-quarters of it",
                    ex[0], ex[1], ex[2]
                ),
            );
            a.exceptions[2] = ex;
        }
        if let Some(taxes) = extras.tax_on_income_to_date() {
            let mut ex = vec![USCents(0); a.dues.len()];
            for (i, t) in taxes.iter().enumerate() {
                if i as i64 >= a.first {
                    ex[i] = y.income_share.of(*t);
                }
            }
            ex[3] = USCents(-1);
            a.notes.rule(
                "RSMo 143.761.4(3); Form MO-2210 (2025), line 24, Exception 4",
                format!(
                    "the first three installments are also excused when payments reach {}, {} and {}: 90% of the tax on the income to date",
                    ex[0], ex[1], ex[2]
                ),
            );
            a.tax_to_date = taxes;
            a.exceptions[3] = ex;
        }
        (a.combat_from, a.combat_to, a.combat_answered) = extras.combat_zone();

        // Instruction H. January 31, 2027 is a Sunday; the 2025 form prints
        // January 31, 2026, a Saturday, and does not move it, so it is not
        // moved.
        let filed = f.return_filed();
        if !filed.is_zero() && !filed.after(y.file_and_pay_by) && !a.exempt {
            a.filed_by_jan31 = true;
            a.notes.rule(
                "RSMo 143.521.6; Form MO-2210 (2025), instruction H",
                format!(
                    "the return was filed and paid on {}, by {}, so the balance counts as paid on {}",
                    filed.long(),
                    y.file_and_pay_by.long(),
                    y.deemed_paid.long()
                ),
            );
        }
        Ok(a)
    }

    /// Withholding and payments, earliest first.
    fn credits(&self, f: &Facts, a: &Assessment) -> Result<(Vec<Payment>, Notes), Error> {
        let mut notes = Notes::default();
        let credits = if f.elections().withholding_as_withheld() {
            let c = estimated::credits_as_withheld(f, &a.dues);
            notes.rule(
                "RSMo 143.761.5(2); Form MO-2210 (2025), instruction F",
                format!(
                    "by election, the {} withheld is credited on the dates it was withheld",
                    f.withheld_total()
                ),
            );
            c
        } else {
            let c = estimated::credits(f, &a.dues, &self.y.withholding_shares);
            notes.rule(
                "RSMo 143.761.5(2); Form MO-2210 (2025), instruction F",
                format!(
                    "the {} withheld is treated as paid in four equal parts on the due dates, whenever it was actually withheld",
                    f.withheld_total()
                ),
            );
            c
        };
        let mut credits = credits?;
        credits.sort_by_key(|c| c.on().days());
        Ok((credits, notes))
    }

    /// The installments after the credits, with the credits they come from.
    /// An installment an exception covers is excused, and so is January's
    /// for a return filed and paid by January 31.
    fn schedule_with(
        &self,
        f: &Facts,
        a: &Assessment,
    ) -> Result<(Vec<Installment>, Vec<Payment>, Notes), Error> {
        let (credits, mut notes) = self.credits(f, a)?;
        let ledger = estimated::apply(&a.dues, &estimated::increments(&a.cumulative), &credits)?;
        let mut out = Vec::with_capacity(ledger.len());
        for (i, l) in ledger.iter().enumerate() {
            let mut b = estimated::new_installment(i as i64 + 1, l.due());
            b.needed_by(a.cumulative[i])
                .credited_by(l.credited_by())
                .shortfall(l.shortfall());
            let why = a.excused(i, l.credited_by());
            if a.filed_by_jan31 && i == 3 {
                b.excused();
            } else if !why.is_empty() {
                b.excused();
                if l.shortfall().0 > 0 {
                    notes.rule(
                        "RSMo 143.761.4; Form MO-2210 (2025), instruction D",
                        format!(
                            "the {} installment was {} short and bears no penalty under {why}",
                            l.due().long(),
                            l.shortfall()
                        ),
                    );
                }
            }
            out.push(b.build()?);
        }
        Ok((out, credits, notes))
    }
}

impl EstimatedTax for MissouriEstimated {
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
                    "with no {} Missouri return to measure against, there is no target to schedule",
                    self.y.year - 1
                ),
            ));
        }
        let (out, _, notes) = self.schedule_with(f, &a)?;
        Ok(working::new_result(out)
            .with(&a.notes)
            .with(&notes)
            .build()?)
    }

    fn penalty(&self, f: &Facts, as_of: Date) -> Result<working::Result<Penalty>, Error> {
        if as_of.is_zero() {
            return Err(Error::Invalid(
                "missouritaxcalc: a penalty needs the date to figure it to".into(),
            ));
        }
        let a = self.assess(f)?;
        if !a.requirement.target_known() {
            return Err(Error::with_detail(
                Error::NeedsThisYearTax,
                format!(
                    "with no {} Missouri return to measure against, there is no target to figure a penalty from",
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
        let waiver_asked = f.positions().waiver();
        let waiver = waiver_asked.unwrap_or_default();
        let (sched, credits, mut notes) = self.schedule_with(f, &a)?;
        if a.exempt {
            let p = estimated::new_penalty(stop)
                .waiver(waiver)
                .because(a.requirement.clone(), &sched)
                .build()?;
            return Ok(working::new_result(p).with(&a.notes).build()?);
        }

        // MO-2210 lines 19d to 28c: each column's total underpayment, from
        // its due date until paid or the next due date, each calendar year's
        // days at that year's rate.
        let mut pb = estimated::new_penalty(stop);
        pb.waiver(waiver).because(a.requirement.clone(), &sched);
        let mut unrated = Date::default();
        let mut early = false;
        let mut accrue = |lb: &mut PenaltyLineBuilder,
                          amount: USCents,
                          from: Date,
                          to: Date|
         -> Result<(), Error> {
            let (parts, gap) = estimated::simple_daily(amount, from, to, &y.rates)?;
            if !gap.is_zero() && (unrated.is_zero() || gap.before(unrated)) {
                unrated = gap;
            }
            for p in parts {
                lb.part(p);
            }
            Ok(())
        };
        let cols = estimated::columns(&a.dues, &a.cumulative, &credits, y.stop, stop)?;
        for col in &cols {
            let k = (col.number() - 1) as usize;
            if col.under().0 <= 0 {
                continue;
            }
            let why = a.excused(k, col.credited_by());
            if !why.is_empty() {
                notes.rule(
                    "RSMo 143.761.4; Form MO-2210 (2025), instruction D",
                    format!(
                        "no penalty is figured in the {} column, under {why}",
                        col.due().long()
                    ),
                );
                continue;
            }
            let mut lb = estimated::new_penalty_line(col.number(), col.due(), col.under());
            for piece in col.pieces() {
                let to = piece.paid_on().unwrap_or_else(|| col.end());
                accrue(&mut lb, piece.amount(), col.due(), to)?;
            }
            let line = lb.days(col.through().days_since(col.due())).build()?;
            if line.total().0 > 0 && k < a.dues.len() - 1 {
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
                "Form MO-2210 (2025), Part I line 6",
                format!(
                    "this year's Missouri tax was not given, so the penalty is figured against the {} tax alone; \
                     the required payment is the lesser of that and 90% of this year's tax, so {} is the most the penalty can be, and it is less if this year's tax turns out lower",
                    y.year - 1,
                    p.total()
                ),
            );
        }
        notes.rule(
            "RSMo 143.761.1, 143.761.3, 32.065; Form MO-2210 (2025), lines 25 to 28c",
            format!(
                "each column's total underpayment is charged by the day ÷ 365 at each calendar year's rate, from its due date until paid or the next due date, and the last column to {}; \
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
                    y.date(unrated.add_days(-1))
                ),
            );
        }
        if p.total().0 > 0 && !a.combat_answered {
            notes.ask(
                PositionKind::JurisdictionFact,
                format!(
                    "Did you serve in a combat zone with hostile fire pay during {}?",
                    y.year
                ),
                "Installments due during that service are exempt.",
            );
        }
        if early && f.requirement_first_met().is_zero() {
            notes.ask(
                PositionKind::RequirementFirstMet,
                format!(
                    "When did you first have to make estimated payments for {}?",
                    y.year
                ),
                "If it was after Apr 1, you pay fewer, larger installments.",
            );
        }
        if waiver_asked.is_some() && waiver != WaiverKind::None {
            notes.rule(
                "12 CSR 10-2.067(3)",
                "Missouri imposes the addition without regard to reasonable cause, and has no waiver for casualty, retirement or disability; the penalty is shown in full",
            );
            if p.total().0 > 0 {
                let o = estimated::waiver_refused(
                    "Missouri has no waiver for retirement, disability, casualty or disaster. The Department of Revenue charges the penalty even with reasonable cause.",
                );
                p = p.to_builder().waiver_outcome(o).build()?;
            }
        }
        Ok(working::new_result(p).with(&a.notes).with(&notes).build()?)
    }

    fn explanation(&self) -> Explanation {
        missouri_explanation()
    }
}

/// Written by hand and checked against research/estimated-payments/mo.md.
fn missouri_explanation() -> Explanation {
    estimated::new_explanation(
        "Missouri expects estimated payments once $100 or more is due after withholding. Its penalty form charges nothing under $500. The penalty is interest on each quarter's shortfall. The rate is set each calendar year, 7% for 2026 and 2027.",
    )
    .paragraph("Your target is 90% of this year's Missouri tax or 100% of last year's. It is whichever is smaller. You're clear if withholding and estimated payments cover a quarter by each date. The dates are April 15, June 15, September 15 and January 15. There is no 110% figure for higher incomes. Last year's figure counts only from a twelve-month return that showed tax. Withholding counts as paid evenly on the four dates. You can instead show when it was taken.")
    .paragraph("Missouri also excuses a quarter when payments so far reach one of four measures. Two are last year's tax and last year's income taxed at this year's rates. The other two are 90% of the tax on your income to date, annualized or actual. The last two do not apply to the January payment.")
    .paragraph("If you first had to pay later in the year, you pay fewer, larger installments. That is three from June 15, two from September 15, or one in January. A return filed and paid by January 31 counts as paid on January 15. The interest runs by the day from each due date until paid or April 15. Missouri does not waive it for reasonable cause.")
    .versus_federal("Missouri's prior-year test is always 100% and needs a return that showed tax. Last year's income at this year's rates is still a safe harbor. The annualized and to-date income tests excuse a quarter rather than lower it. They do not apply to the fourth. A late starter has fewer installments. The rate is set each calendar year. There is no waiver for casualty, retirement or disability.")
    .source("RSMo §§ 143.521, 143.531, 143.761, 32.065")
    .source("Form MO-2210, 2025 Underpayment of Estimated Tax by Individuals, and its instructions")
    .source("Form MO-1040ES, 2026 Declaration of Estimated Tax for Individuals (Revised 12-2025)")
    .source("12 CSR 10-41.010 (emergency amendment, 2026 rate); Missouri DOR, Statutory Interest Rates")
    .source("12 CSR 10-2.067")
    .build()
    .unwrap_or_default()
}
