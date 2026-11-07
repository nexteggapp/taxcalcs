//! New Jersey's estimated gross income tax rules, N.J.S.A. 54A:8-4, 54A:8-5
//! and 54A:9-6(c) and (d), with interest under N.J.S.A. 54:49-3, as the
//! shared [`EstimatedTax`] trait. New Jersey calls the charge interest.
//!
//! New Jersey keeps the older federal rules. The underpayment is measured
//! against the lesser of 80% of this year's tax and 100% of last year's, a
//! quarter by each date. Four exceptions each take the interest off a
//! quarter whose payments reach them: last year's tax, last year's income at
//! this year's rates, 80% of the tax on annualized income, and 90% of the
//! tax on income to date. Interest is prime plus 3% for each month or part
//! of a month, 10% for 2026.
//!
//! The figure follows Form NJ-2210 for 2025, the latest published, with the
//! 2026 dates and rate, and the result's Divergences say where the statute
//! reads otherwise. N.J.S.A. 54A:8-4, 54A:8-5, 54A:9-6 and 54:49-3 carry no
//! amendment after 2005 (research/estimated-payments/nj.md), so the form
//! does not predate the law it describes.
//!
//! Option 2 of the NJ-2210 instructions counts each month or part of a month
//! from the due date, and its rate schedule gives a month the rate of the
//! period it starts in: for 2025, 10.75% to January 15, 2026 and 10.00% from
//! January 16. A 2026 underpayment is charged 10.00% for months starting up
//! to January 15, 2027. The rate from January 16, 2027 is set from the prime
//! rate on December 1, 2026 and is not published, so later months are not
//! charged and the penalty reports the gap. Every due date is the fifteenth,
//! so the months each quarter's balance runs line up with the due dates, and
//! figuring each period's balance, as Option 1 does, gives Option 2's
//! months.
//!
//! Interest compounds at the end of each calendar year; only months from
//! January 16, 2027 would carry 2026's interest, and they are not charged.
//!
//! Not built: the exception for an installment due on or after the
//! taxpayer's death (54A:9-6(c)), as [`Facts`] carries no date of death;
//! Form NJ-2210NR, which scales Exceptions 2 to 4 by a nonresident's income
//! percentage, which a caller can apply to the amounts it passes; and
//! estates and trusts.

use std::any::Any;
use std::sync::LazyLock;

use estimated::{
    Basis, Calendar, Date, Error, EstimatedTax, Explanation, Facts, Installment, Jurisdiction,
    Month, PartKind, Payment, Penalty, PenaltyPart, Requirement, WaiverKind,
};
use filing::Status;
use money::{DOLLAR, Rate, USCents, percent};
use working::{Divergence, Notes, PositionKind, Reading};

use crate::estimated_y2026::year_2026;

/// New Jersey's postal code.
const JURISDICTION: Jurisdiction = estimated::NEW_JERSEY;

/// The facts New Jersey's rules read that no other jurisdiction does. Carry
/// them in [`Facts::state`].
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct EstimatedFacts {
    refundable_credits: Option<USCents>,
    prior_income_tax: Option<USCents>,
    tax_to_date: Option<Vec<USCents>>,
    overpayment_april: Option<bool>,
    moves: Option<bool>,
}

impl EstimatedFacts {
    /// This year's refundable credits and excess contributions, Form
    /// NJ-1040 lines 56 and 58 to 65, or `None` when not given. NJ-2210 line
    /// 6 counts them with the tax withheld, line 55, a quarter on each date.
    pub fn refundable_credits(&self) -> Option<USCents> {
        self.refundable_credits
    }

    /// The tax on last year's gross income less this year's exemptions at
    /// this year's rates, less the credit for tax paid to other
    /// jurisdictions: the NJ-2210 Exception II worksheet, line 6.
    pub fn prior_year_income_at_this_years_rates(&self) -> Option<USCents> {
        self.prior_income_tax
    }

    /// The tax on the actual taxable income from January 1 to March 31, May
    /// 31 and August 31, less the credit for tax paid to other
    /// jurisdictions: the NJ-2210 Exception IV worksheet, line 4, before the
    /// 90%.
    pub fn tax_on_income_to_date(&self) -> Option<Vec<USCents>> {
        self.tax_to_date.clone()
    }

    /// Whether last year's overpayment applied forward counts toward the
    /// April installment, as Form NJ-1040-ES says, rather than the June one,
    /// as NJ-2210 line 6 says; `None` when the caller did not say.
    pub fn prior_year_overpayment_in_april(&self) -> Option<bool> {
        self.overpayment_april
    }

    /// Whether a return filed and paid on Tuesday, February 16, 2027 counts
    /// as filed by February 15, which is Presidents' Day; `None` when the
    /// caller did not say. No 2026 source says.
    pub fn february_fifteenth_moves(&self) -> Option<bool> {
        self.moves
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
    /// Sets the refundable credits.
    pub fn refundable_credits(&mut self, a: USCents) -> &mut Self {
        self.e.refundable_credits = Some(a);
        self
    }

    /// Sets the tax on last year's income at this year's rates.
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

    /// Answers whether last year's overpayment counts in April.
    pub fn prior_year_overpayment_in_april(&mut self, april: bool) -> &mut Self {
        self.e.overpayment_april = Some(april);
        self
    }

    /// Answers whether February 15 moves to the next business day.
    pub fn february_fifteenth_moves(&mut self, moves: bool) -> &mut Self {
        self.e.moves = Some(moves);
        self
    }

    /// Refuses negative amounts.
    pub fn build(&self) -> Result<EstimatedFacts, Error> {
        let e = &self.e;
        if e.refundable_credits.unwrap_or_default().0 < 0
            || e.prior_income_tax.unwrap_or_default().0 < 0
        {
            return Err(Error::Invalid(
                "newjerseytaxcalc: credits and taxes cannot be negative".into(),
            ));
        }
        if e.tax_to_date.iter().flatten().any(|t| t.0 < 0) {
            return Err(Error::Invalid(
                "newjerseytaxcalc: the tax on income to date cannot be negative".into(),
            ));
        }
        Ok(e.clone())
    }
}

/// One tax year's published figures.
#[derive(Clone, Debug)]
pub(crate) struct EstimatedYear {
    pub(crate) year: i64,

    /// NJ-2210 line 3.
    pub(crate) threshold: USCents,

    /// The gross income at or below which no estimated payments are needed
    /// on joint, head of household and surviving spouse returns.
    pub(crate) filing_threshold: USCents,
    /// The same on the other returns.
    pub(crate) filing_threshold_single: USCents,

    pub(crate) this_year_share: Rate,
    pub(crate) prior_year_share: Rate,

    /// Exception 3: 80% of the annualized tax.
    pub(crate) annualised_share: Rate,
    /// Exception 4: 90% of the tax on income to date.
    pub(crate) income_share: Rate,

    pub(crate) shares: Vec<Rate>,
    pub(crate) dues: Vec<Date>,
    pub(crate) calendar: Calendar,
    pub(crate) period_ends: Vec<Date>,

    /// N.J.S.A. 54A:8-4(h): the return filed and paid by then takes the
    /// place of the January installment.
    pub(crate) file_and_pay_by: Date,

    pub(crate) stop: Date,

    /// The annual rate for the months that start by `rate_to`, from
    /// TB-21(R) and the NJ-2210 rate schedule's pattern.
    pub(crate) rate: Rate,
    pub(crate) rate_to: Date,
    pub(crate) rate_source: &'static str,
    /// Why the months after `rate_to` have no rate.
    pub(crate) unrated: &'static str,
}

/// Returns New Jersey's estimated tax rules for a tax year, or
/// [`Error::NoRules`] for a year this build does not carry.
pub fn estimated_tax(year: i64) -> Result<Box<dyn EstimatedTax>, Error> {
    let y = match year {
        2026 => year_2026()?,
        _ => return Err(estimated::no_rules(&JURISDICTION, year)),
    };
    Ok(Box::new(NewJerseyEstimated { y }))
}

struct NewJerseyEstimated {
    y: EstimatedYear,
}

#[derive(Clone, Debug, Default)]
struct Assessment {
    requirement: Requirement,
    exempt: bool,
    tax_known: bool,
    dues: Vec<Date>,
    installments: Vec<USCents>,

    /// The running totals that excuse a quarter, by exception, or `None`
    /// where it is not open; -1 where it does not apply to a quarter.
    exceptions: [Option<Vec<USCents>>; 4],

    filed_by_feb_15: bool,
    extras: EstimatedFacts,
    notes: Notes,
}

/// The facts each divergence's readings turn on.
#[derive(Clone, Copy, Debug, Default)]
struct Touches {
    at_threshold: bool,
    short_prior_year: bool,
    late_start: bool,
    high_income_exceptions: bool,
    unpaid_at_year_end: bool,
    overpayment: bool,
    overpayment_april: bool,
}

/// N.J.S.A. 54A:9-6(d)(3)'s last-year income above which Exceptions 1 and 2
/// are figured at 110%. It decides only whether that divergence affects a
/// household.
const HIGH_INCOME: USCents = USCents(150_000 * DOLLAR.0);
/// The same, married filing separately.
const HIGH_INCOME_SEPARATE: USCents = USCents(75_000 * DOLLAR.0);

/// Go's `%T` for the extras a caller passed, as far as Rust can tell it.
fn type_name(v: &(dyn Any + Send + Sync)) -> &'static str {
    if v.is::<&str>() || v.is::<String>() {
        "string"
    } else if v.is::<i64>() {
        "int"
    } else if v.is::<bool>() {
        "bool"
    } else {
        "unknown"
    }
}

impl Assessment {
    /// Names the exception that takes the interest off quarter `i`, or "".
    fn excused(&self, i: usize, paid_by: USCents) -> &'static str {
        const NAMES: [&str; 4] = [
            "Exception 1, last year's tax",
            "Exception 2, last year's income at this year's rates",
            "Exception 3, annualized income",
            "Exception 4, income to date",
        ];
        for (k, ex) in self.exceptions.iter().enumerate() {
            if let Some(ex) = ex
                && ex[i].0 >= 0
                && paid_by >= ex[i]
            {
                return NAMES[k];
            }
        }
        ""
    }
}

impl NewJerseyEstimated {
    fn extras(&self, f: &Facts) -> Result<EstimatedFacts, Error> {
        match f.state() {
            None => Ok(EstimatedFacts::default()),
            Some(s) => match s.downcast_ref::<EstimatedFacts>() {
                Some(e) => Ok(e.clone()),
                None => Err(Error::Invalid(format!(
                    "newjerseytaxcalc: the facts carry {}, not newjerseytaxcalc.EstimatedFacts",
                    type_name(s)
                ))),
            },
        }
    }

    fn check(&self, f: &Facts) -> Result<(), Error> {
        if f.jurisdiction() != JURISDICTION {
            return Err(Error::Invalid(format!(
                "newjerseytaxcalc: these are New Jersey's rules, and the facts are for {}",
                f.jurisdiction()
            )));
        }
        if f.tax_year() != self.y.year {
            return Err(Error::Invalid(format!(
                "newjerseytaxcalc: these are the {} rules, and the facts are for {}",
                self.y.year,
                f.tax_year()
            )));
        }
        if i64::from(f.farm_share().0) * 3 >= 20_000 {
            return Err(Error::with_detail(
                Error::Unsupported,
                "a farmer with two-thirds of income from farming pays in full by January 15 at two-thirds of the tax, which is not built",
            ));
        }
        if f.elections().annualised() {
            for (i, p) in f.periods().iter().enumerate() {
                if let Some(end) = self.y.period_ends.get(i)
                    && p.end() != *end
                {
                    return Err(Error::Invalid(format!(
                        "newjerseytaxcalc: NJ-2210 Exception III period {} ends on {}, not {}",
                        i + 1,
                        end,
                        p.end()
                    )));
                }
            }
        }
        Ok(())
    }

    /// Where the Division's forms and the statutes part ways on a rule every
    /// New Jersey computation uses. They are in
    /// research/estimated-payments/nj.md, except the late start, Exception
    /// 2's 110% and the rate's start, found reading NJ-2210 against the
    /// statutes and TB-21(R).
    fn divergences(&self, t: Touches) -> Result<Vec<Divergence>, Error> {
        let overpayment_reading = if t.overpayment_april {
            Reading::Statute
        } else {
            Reading::Form
        };
        let mut out = Vec::new();
        for b in [
            working::new_divergence("The $400 boundary")
                .form(
                    "The form stops only if tax less withholding and credits is under $400. Exactly $400 goes on to the computation.",
                    "Form NJ-2210 (2025), line 3 and its instruction",
                )
                .statute(
                    "A declaration is needed only if tax is expected to exceed credits by over $400. Exactly $400 requires none.",
                    "N.J.S.A. 54A:8-4(a), (f)",
                )
                .uses(Reading::Form)
                .affects(t.at_threshold),
            working::new_divergence("A twelve-month prior year")
                .form(
                    "100% of last year's tax counts only if that return covered a full 12 months.",
                    "Form NJ-2210 (2025) instructions, In General",
                )
                .statute(
                    "The underpayment is measured against 100% of the tax on last year's return. There's no twelve-month condition. The condition is in Exception 1 only.",
                    "N.J.S.A. 54A:9-6(c)(2), (d)(1)(A)",
                )
                .uses(Reading::Form)
                .affects(t.short_prior_year),
            working::new_divergence("A requirement that arises late")
                .form(
                    "The form divides the required amount by four in every case. Form NJ-1040-ES tells a late starter to pay equal parts on the dates left.",
                    "Form NJ-2210 (2025), line 5; 2026 Form NJ-1040-ES, Changes in Income, Exemptions, etc.",
                )
                .statute(
                    "A declaration first due June 15 is paid in three equal installments. One due September 15 is paid in two, and one due January 15 in full.",
                    "N.J.S.A. 54A:8-4(d), 54A:8-5(a)(2)-(4)",
                )
                .uses(Reading::Form)
                .affects(t.late_start),
            // A zero prior year also makes the required amount zero, so the
            // readings give the same figures.
            working::new_divergence("Exception 1 with no prior-year return")
                .form(
                    "Last year's tax is zero if no return was required, so Exception 1 is met.",
                    "Form NJ-2210 (2025) instructions, Exception 1",
                )
                .statute(
                    "Exception 1 needs a return showing a liability filed for a twelve-month year.",
                    "N.J.S.A. 54A:9-6(d)(1)(A)",
                )
                .uses(Reading::Form)
                .affects(false),
            working::new_divergence("110% in Exceptions 1 and 2")
                .form(
                    "Exceptions 1 and 2 are 25%, 50%, 75% and 100%, at any income. They apply to last year's tax and to the tax on last year's income. The Division's notice charges interest only against the lesser of 80% and 100%.",
                    "Form NJ-2210 (2025), lines 15 and 16; Notice Concerning Estimated Tax for Taxpayers Earning Over $150,000",
                )
                .statute(
                    "Above $150,000 of last year's taxable gross income, both are figured at 110%. The line is $75,000 if married filing separately.",
                    "N.J.S.A. 54A:9-6(d)(3)",
                )
                .uses(Reading::Form)
                .affects(t.high_income_exceptions),
            working::new_divergence("When a year's rate starts")
                .form(
                    "The rate schedule gives each month the rate of the period it starts in. The new year's rate starts January 16.",
                    "Form NJ-2210 (2025), Option 2, Interest Rate Schedule",
                )
                .statute(
                    "Each calendar year's rate starts January 1, at three points over prime. It applies for each month or fraction of a month.",
                    "TB-21(R), Interest Rate Assessed on Tax Balances for 2026; N.J.S.A. 54:49-3",
                )
                .uses(Reading::Form)
                .affects(t.unpaid_at_year_end),
            working::new_divergence("Last year's overpayment")
                .form(
                    "Form NJ-2210 counts last year's overpayment applied forward in column B. That's the June installment.",
                    "Form NJ-2210 (2025), Line 6 instructions",
                )
                .statute(
                    "Form NJ-1040-ES credits it to the first quarterly installment. The estimated tax statutes don't address it.",
                    "2026 Form NJ-1040-ES, Completing the NJ-1040-ES Voucher; N.J.S.A. 54A:9-6",
                )
                .uses(overpayment_reading)
                .affects(t.overpayment),
        ] {
            out.push(b.build()?);
        }
        Ok(out)
    }

    fn assess(&self, f: &Facts) -> Result<Assessment, Error> {
        self.check(f)?;
        let extras = self.extras(f)?;
        let y = &self.y;
        let mut a = Assessment {
            extras: extras.clone(),
            dues: estimated::move_dates(&y.calendar, &y.dues),
            ..Assessment::default()
        };
        let last = y.year - 1;

        let tax_opt = f.this_year().tax();
        let tax_known = tax_opt.is_some();
        let tax = tax_opt.unwrap_or_default();
        a.tax_known = tax_known;
        let refundable_opt = extras.refundable_credits();
        let credited = f.withheld_total() + refundable_opt.unwrap_or_default();
        let prior_opt = f.prior_year();
        let has_prior = prior_opt.is_some();
        let prior = prior_opt.unwrap_or_default();
        let mut prior_tax = USCents(0);
        if has_prior && let Some(pf) = prior.figures() {
            prior_tax = pf.tax().unwrap_or_default();
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
                "N.J.S.A. 54A:9-6(c)(1); Form NJ-2210 (2025), line 4a",
                format!(
                    "{} of this year's New Jersey tax of {} is {}",
                    y.this_year_share,
                    tax,
                    this_year.amount()
                ),
            );
        }

        let unavailable = |why: String| {
            let mut b =
                estimated::new_target_option(Basis::PriorYearTax, y.prior_year_share, USCents(0));
            b.unavailable(why);
            b
        };
        let mut exception_one = false;
        let prior_option = if !has_prior {
            unavailable(format!("Needs your {last} New Jersey return"))
        } else if prior.not_required_to_file() && prior.months() == 12 {
            exception_one = true;
            a.notes.rule(
                "Form NJ-2210 (2025) instructions, Exception 1",
                format!("no {last} return was required, so last year's tax is zero"),
            );
            estimated::new_target_option(Basis::PriorYearTax, y.prior_year_share, USCents(0))
        } else if !prior.filed() {
            unavailable(format!("You didn't file a {last} New Jersey return"))
        } else if prior.months() != 12 {
            unavailable(format!(
                "Your {last} New Jersey return covered {} months, not a full year",
                prior.months()
            ))
        } else {
            exception_one = true;
            a.notes.rule(
                "N.J.S.A. 54A:9-6(c)(2); Form NJ-2210 (2025), line 4b",
                format!(
                    "the prior-year test is {} of the {} tax on the {last} return",
                    y.prior_year_share, prior_tax
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

        let gross_opt = f.this_year().gross_income();
        let filing_threshold = match f.filing_status() {
            Status::MarriedFilingJointly | Status::HeadOfHousehold | Status::SurvivingSpouse => {
                y.filing_threshold
            }
            _ => y.filing_threshold_single,
        };
        let gross_exempt = gross_opt.is_some_and(|g| g <= filing_threshold);
        let gross = gross_opt.unwrap_or_default();
        if gross_exempt {
            let exception = estimated::exception_at_most("Gross income", gross, filing_threshold)?;
            a.exempt = true;
            rb.required(
                false,
                format!(
                    "Your gross income of {} is at or below the {} filing threshold. You don't need to make New Jersey estimated payments.",
                    gross.whole(),
                    filing_threshold.whole()
                ),
            )
            .exempt(exception);
            a.notes.rule(
                "2026 Form NJ-1040-ES, Minimum Income Filing Threshold",
                format!(
                    "gross income of {gross} is at or below {filing_threshold}, so no estimated payments are needed"
                ),
            );
        } else if tax_known && tax - credited < y.threshold {
            let exception = estimated::exception_below(
                "Tax less withholding and credits",
                tax - credited,
                y.threshold,
            )?;
            a.exempt = true;
            let under = if tax < credited {
                String::new()
            } else {
                format!(", under {}", y.threshold.whole())
            };
            rb.required(
                false,
                format!(
                    "Your New Jersey tax is {}, and withholding and credits come to {}. That {}{}. No estimated payments are needed.",
                    tax.whole(),
                    credited.whole(),
                    estimated::leaves(tax - credited),
                    under
                ),
            )
            .exempt(exception);
            a.notes.rule(
                "Form NJ-2210 (2025), line 3",
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
                    "Your target needs your {} New Jersey tax. A full-year {last} New Jersey return could also set it. There's no target yet.",
                    y.year
                ),
            )
            .no_target();
        } else if !tax_known && credited >= target {
            rb.required(
                false,
                format!(
                    "Your withholding and credits of {} already reach your {last} tax, {}. No estimated payments are needed, whatever your {} tax is.",
                    credited.whole(),
                    target.whole(),
                    y.year
                ),
            )
            .choose(chosen);
        } else if !tax_known {
            rb.required(
                true,
                format!(
                    "Your target is your {last} tax, {}. That keeps you clear of interest whatever your {} tax turns out to be. Withholding and credits of {} leave {} of estimated payments. Pay a quarter by each due date.",
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
                    "Your New Jersey withholding and credits come to {}. That reaches the {} needed for the year. No estimated payments are needed.",
                    credited.whole(),
                    target.whole()
                ),
            )
            .choose(chosen);
        } else {
            rb.required(
                true,
                format!(
                    "Your New Jersey tax is {}, and withholding and credits come to {}. That leaves {}, which is {} or more. You need estimated payments of {}, a quarter by each due date.",
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
                format!("Did you file a {last} New Jersey return, and what was its tax?"),
                format!(
                    "With it, 100% of your {last} tax could be a lower target than 80% of this year's."
                ),
            );
        }
        if !tax_known && !a.exempt {
            let question = format!("What do you expect your {} New Jersey tax to be?", y.year);
            if prior_opt.available() {
                a.notes.ask(
                    PositionKind::ThisYearTax,
                    question,
                    format!(
                        "With it, 80% of it could be a lower target than your {last} tax. It also decides whether the {} threshold excuses you.",
                        y.threshold.whole()
                    ),
                );
            } else {
                a.notes.ask(
                    PositionKind::ThisYearTax,
                    question,
                    format!(
                        "Without it there's no target. There's no full-year {last} New Jersey return to measure against."
                    ),
                );
            }
        }
        if !a.exempt && refundable_opt.is_none() {
            a.notes.ask(
                PositionKind::JurisdictionFact,
                "What New Jersey refundable credits and excess contributions do you expect?",
                format!(
                    "For {} they count with withholding, a quarter on each due date. Until then, the figures count none.",
                    y.year
                ),
            );
        }

        let cumulative = estimated::cumulative_shares(a.requirement.target(), &y.shares)?;
        a.installments = estimated::increments(&cumulative);
        a.notes.rule(
            "N.J.S.A. 54A:8-5(a)(1); Form NJ-2210 (2025), line 5",
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

        // The four exceptions, as running totals by each date.
        let quarters = [percent(25), percent(50), percent(75), percent(100)];
        let n = a.dues.len();
        if exception_one {
            a.exceptions[0] = Some((0..n).map(|i| quarters[i].of(prior_tax)).collect());
        }
        if let Some(amount) = extras.prior_year_income_at_this_years_rates() {
            a.exceptions[1] = Some((0..n).map(|i| quarters[i].of(amount)).collect());
            a.notes.rule(
                "N.J.S.A. 54A:9-6(d)(1)(B); Form NJ-2210 (2025), line 16, Exception 2",
                format!(
                    "a quarter is excused when payments by its date reach 25%, 50%, 75% and 100% of {amount}, the tax on {last} income at {} rates",
                    y.year
                ),
            );
        }
        if f.elections().annualised() {
            let mut ex = vec![USCents(-1); 4];
            for (i, p) in f.periods().iter().take(3).enumerate() {
                ex[i] = y.annualised_share.of(quarters[i].of(p.annualised_tax()));
            }
            a.notes.rule(
                "N.J.S.A. 54A:9-6(d)(1)(C); Form NJ-2210 (2025), line 17, Exception 3",
                format!(
                    "the first three quarters are also excused when payments reach {}, {} and {}: 20%, 40% and 60% of the tax on annualized income",
                    ex[0], ex[1], ex[2]
                ),
            );
            a.exceptions[2] = Some(ex);
        }
        if let Some(taxes) = extras.tax_on_income_to_date() {
            let mut ex = vec![USCents(-1); 4];
            for (i, t) in taxes.iter().enumerate() {
                ex[i] = y.income_share.of(*t);
            }
            a.notes.rule(
                "N.J.S.A. 54A:9-6(d)(2); Form NJ-2210 (2025), line 18, Exception 4",
                format!(
                    "the first three quarters are also excused when payments reach {}, {} and {}: 90% of the tax on the income to date",
                    ex[0], ex[1], ex[2]
                ),
            );
            a.exceptions[3] = Some(ex);
        }

        // N.J.S.A. 54A:8-4(h). February 15, 2027 is a Monday and Presidents'
        // Day, and no source yet says whether the day moves.
        let mut file_by = y.file_and_pay_by;
        let moves = extras.february_fifteenth_moves();
        if moves == Some(true) {
            file_by = file_by.add_days(1);
        }
        let filed = f.return_filed();
        if !filed.is_zero() && !a.exempt {
            if moves.is_none() && filed == file_by.add_days(1) {
                a.notes.ask(
                    PositionKind::UnsettledRule,
                    "",
                    format!(
                        "You filed and paid on {}, the day after Presidents' Day. No 2026 source says if that counts as on time, and the figures treat it as late.",
                        filed.long()
                    ),
                );
            }
            if !filed.after(file_by) {
                a.filed_by_feb_15 = true;
                a.notes.rule(
                    "N.J.S.A. 54A:8-4(h); 2026 Form NJ-1040-ES, Changes in Income, Exemptions, etc.",
                    format!(
                        "the return was filed and paid on {}, by {}, so no January installment is needed",
                        filed.long(),
                        file_by.long()
                    ),
                );
            }
        }
        if a.exempt {
            a.installments = vec![USCents(0); a.dues.len()];
        }

        let mut t = Touches {
            at_threshold: !gross_exempt && tax_known && tax - credited == y.threshold,
            short_prior_year: !a.exempt && has_prior && prior.filed() && prior.months() != 12,
            overpayment_april: extras.prior_year_overpayment_in_april().unwrap_or(false),
            ..Touches::default()
        };
        let met = f.requirement_first_met();
        if !a.exempt && !met.is_zero() && met.after(a.dues[0]) {
            t.late_start = true;
        }
        if !a.exempt {
            if f.payments().iter().any(|p| p.from_prior_year()) {
                t.overpayment = true;
            }
            let exception_two = extras.prior_year_income_at_this_years_rates().is_some();
            if exception_one || exception_two {
                let limit = if f.filing_status() == Status::MarriedFilingSeparately {
                    HIGH_INCOME_SEPARATE
                } else {
                    HIGH_INCOME
                };
                let income = if has_prior {
                    prior
                        .figures()
                        .and_then(|pf| pf.gross_income().or_else(|| pf.state_agi()))
                } else {
                    None
                };
                t.high_income_exceptions = income.is_none_or(|i| i > limit);
            }
        }
        if !a.exempt && a.requirement.target_known() {
            let (credits, _) = self.credits(f, &a)?;
            let year_end = date(y.year, Month::DECEMBER, 31);
            let by_year_end: USCents = credits
                .iter()
                .filter(|c| !c.on().after(year_end))
                .map(|c| c.amount())
                .sum();
            let by_september: USCents = credits
                .iter()
                .filter(|c| !c.on().after(a.dues[2]))
                .map(|c| c.amount())
                .sum();
            let needed = a.installments[0] + a.installments[1] + a.installments[2];
            t.unpaid_at_year_end = by_year_end < needed && a.excused(2, by_september).is_empty();
        }
        let divergences = self.divergences(t)?;
        a.notes.diverges(&divergences);
        Ok(a)
    }

    /// Withholding, refundable credits and payments, dated as NJ-2210 line 6
    /// counts them, earliest first.
    fn credits(&self, f: &Facts, a: &Assessment) -> Result<(Vec<Payment>, Notes), Error> {
        let mut notes = Notes::default();
        let mut out = Vec::new();
        let add = |out: &mut Vec<Payment>, on: Date, amount: USCents| -> Result<(), Error> {
            if amount.0 <= 0 {
                return Ok(());
            }
            out.push(estimated::new_payment().on(on).amount(amount).build()?);
            Ok(())
        };
        let refundable = a.extras.refundable_credits().unwrap_or_default();
        let mut spread = refundable;
        if f.elections().withholding_as_withheld() {
            for w in f.withholding() {
                add(&mut out, w.on(), w.amount())?;
            }
            notes.rule(
                "Form NJ-2210 (2025), Line 6 instructions",
                format!(
                    "as shown, the {} withheld is counted on the dates it was withheld; the {} of credits is counted a quarter on each date",
                    f.withheld_total(),
                    refundable
                ),
            );
        } else {
            spread += f.withheld_total();
            notes.rule(
                "Form NJ-2210 (2025), Line 6 instructions",
                format!(
                    "the {} withheld and {} of credits are treated as paid a quarter on each due date",
                    f.withheld_total(),
                    refundable
                ),
            );
        }
        let cum = estimated::cumulative_shares(spread, &self.y.shares)?;
        for (i, amt) in estimated::increments(&cum).into_iter().enumerate() {
            add(&mut out, a.dues[i], amt)?;
        }
        let april = a.extras.prior_year_overpayment_in_april();
        for p in f.payments() {
            let mut on = p.on();
            if p.from_prior_year() {
                on = if april == Some(true) {
                    a.dues[0]
                } else {
                    a.dues[1]
                };
                if april.is_none() {
                    notes.ask(
                        PositionKind::UnsettledRule,
                        "",
                        format!(
                            "Form NJ-1040-ES counts your {} overpayment toward the April installment. Form NJ-2210 counts it toward June's, and the figures follow Form NJ-2210.",
                            self.y.year - 1
                        ),
                    );
                }
            }
            add(&mut out, on, p.amount())?;
        }
        out.sort_by(|x, y| {
            if x.on().before(y.on()) {
                std::cmp::Ordering::Less
            } else if y.on().before(x.on()) {
                std::cmp::Ordering::Greater
            } else {
                std::cmp::Ordering::Equal
            }
        });
        Ok((out, notes))
    }

    /// The installments, with the credits applied earliest first. A quarter
    /// an exception covers is excused.
    fn schedule_of(
        &self,
        a: &Assessment,
        credits: &[Payment],
        notes: &mut Notes,
    ) -> Result<Vec<Installment>, Error> {
        let mut required = a.installments.clone();
        if a.filed_by_feb_15 {
            required[3] = USCents(0);
        }
        let ledger = estimated::apply(&a.dues, &required, credits)?;
        let mut out = Vec::with_capacity(ledger.len());
        for (i, l) in ledger.iter().enumerate() {
            let why = a.excused(i, l.credited_by());
            let mut b = estimated::new_installment(i as i64 + 1, l.due());
            b.needed_by(l.needed_by())
                .credited_by(l.credited_by())
                .shortfall(l.shortfall());
            let january_replaced = a.filed_by_feb_15 && i == 3;
            if january_replaced || !why.is_empty() {
                b.excused();
            }
            if l.shortfall().0 > 0 && !why.is_empty() && !january_replaced {
                notes.rule(
                    "N.J.S.A. 54A:9-6(d); Form NJ-2210 (2025), Part II",
                    format!(
                        "the {} quarter was {} short and bears no interest under {}",
                        l.due().long(),
                        l.shortfall(),
                        why
                    ),
                );
            }
            out.push(b.build()?);
        }
        Ok(out)
    }

    /// Charges `principal` for each month or part of a month from `from` to
    /// `to` at the year's rate, for the months that start by the rate's last
    /// day. It returns the part, if any month is rated, and the first day of
    /// a month with no rate, or the zero Date.
    fn monthly(
        &self,
        principal: USCents,
        from: Date,
        to: Date,
    ) -> Result<(Option<PenaltyPart>, Date), Error> {
        let y = &self.y;
        let months = estimated::months_or_part(from, to);
        let mut rated = 0;
        for k in 0..months {
            let start = from.add_months(k).add_days(1);
            if start.after(y.rate_to) {
                break;
            }
            rated += 1;
        }
        let mut unrated = Date::default();
        if rated < months {
            unrated = from.add_months(rated).add_days(1);
        }
        if rated == 0 {
            return Ok((None, unrated));
        }
        let end = if rated < months {
            from.add_months(rated)
        } else {
            to
        };
        let n = principal.0 * i64::from(y.rate.0) * rated;
        let charge = USCents((n + 60_000) / 120_000);
        let p = estimated::new_penalty_part(PartKind::Interest, principal, y.rate)
            .span(from, end)
            .days(end.days_since(from))
            .months(rated)
            .charge(charge)
            .source(y.rate_source)
            .build()?;
        Ok((Some(p), unrated))
    }
}

impl EstimatedTax for NewJerseyEstimated {
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
                    "with no {} New Jersey return to measure against, there is no target to schedule",
                    self.y.year - 1
                ),
            ));
        }
        let (credits, mut notes) = self.credits(f, &a)?;
        let out = self.schedule_of(&a, &credits, &mut notes)?;
        Ok(working::new_result(out)
            .with(&a.notes)
            .with(&notes)
            .build()?)
    }

    fn penalty(&self, f: &Facts, as_of: Date) -> Result<working::Result<Penalty>, Error> {
        if as_of.is_zero() {
            return Err(Error::Invalid(
                "newjerseytaxcalc: a penalty needs the date to figure it to".into(),
            ));
        }
        let a = self.assess(f)?;
        if !a.requirement.target_known() {
            return Err(Error::with_detail(
                Error::NeedsThisYearTax,
                format!(
                    "with no {} New Jersey return to measure against, there is no target to figure the interest from",
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
        let waiver_opt = f.positions().waiver();
        let waiver = waiver_opt.unwrap_or_default();
        let (credits, mut notes) = self.credits(f, &a)?;
        let mut schedule_notes = Notes::default();
        let schedule = self.schedule_of(&a, &credits, &mut schedule_notes)?;
        if a.exempt {
            let p = estimated::new_penalty(stop)
                .waiver(waiver)
                .because(a.requirement.clone(), &schedule)
                .build()?;
            return Ok(working::new_result(p).with(&a.notes).build()?);
        }

        // NJ-2210 Option 1's columns, with Option 2's months: each period's
        // balance, this quarter's amount and what an unexcused earlier
        // quarter left, less what was paid in the period, charged for each
        // month or part of a month until paid or the next due date. A
        // quarter an exception excuses carries nothing forward, as GIT-8's
        // example does.
        let mut pb = estimated::new_penalty(stop);
        pb.waiver(waiver).because(a.requirement.clone(), &schedule);
        let mut unrated = Date::default();
        let (mut carry, mut paid_by) = (USCents(0), USCents(0));
        let mut prev = Date::default();
        for (k, &due) in a.dues.iter().enumerate() {
            let paid_in: USCents = credits
                .iter()
                .filter(|c| c.on().after(prev) && !c.on().after(due))
                .map(|c| c.amount())
                .sum();
            paid_by += paid_in;
            prev = due;
            let mut amount = a.installments[k];
            if a.filed_by_feb_15 && k == 3 {
                amount = USCents(0);
            }
            let balance = amount + carry - paid_in;
            carry = balance;
            if balance.0 <= 0 || !due.before(stop) {
                continue;
            }
            let why = a.excused(k, paid_by);
            if !why.is_empty() {
                carry = USCents(0);
                notes.rule(
                    "N.J.S.A. 54A:9-6(d); Form NJ-2210 (2025), Part II; GIT-8 (January 2026)",
                    format!(
                        "no interest is charged for the {} quarter, under {}",
                        due.long(),
                        why
                    ),
                );
                continue;
            }
            let mut end = y.stop;
            if k + 1 < a.dues.len() {
                end = a.dues[k + 1];
            }
            if stop.before(end) {
                end = stop;
            }
            let mut lb = estimated::new_penalty_line(k as i64 + 1, due, balance);
            let (mut left, mut last_day) = (balance, due);
            let mut charge = |amount: USCents, to: Date| -> Result<(), Error> {
                let (part, gap) = self.monthly(amount, due, to)?;
                if !gap.is_zero() && (unrated.is_zero() || gap.before(unrated)) {
                    unrated = gap;
                }
                if let Some(part) = part {
                    lb.part(part);
                }
                Ok(())
            };
            for c in &credits {
                if left.0 <= 0 || !c.on().after(due) || !c.on().before(end) {
                    continue;
                }
                let piece = left.min(c.amount());
                charge(piece, c.on())?;
                left -= piece;
                last_day = c.on();
            }
            if left.0 > 0 {
                charge(left, end)?;
                last_day = end;
            }
            let line = lb.days(last_day.days_since(due)).build()?;
            pb.line(line);
        }
        if !a.tax_known {
            pb.upper_bound();
        }
        let mut p = pb.build()?;
        if !a.tax_known {
            notes.rule(
                "N.J.S.A. 54A:9-6(c)",
                format!(
                    "this year's New Jersey tax was not given, so the interest is figured against the {} tax alone; \
                     the required amount is the lesser of that and 80% of this year's tax, so {} is the most the interest can be, and it is less if this year's tax turns out lower",
                    y.year - 1,
                    p.total()
                ),
            );
        }
        notes.rule(
            "N.J.S.A. 54A:9-6(c), 54:49-3; Form NJ-2210 (2025), Option 2; TB-21(R)",
            format!(
                "each period's balance is charged {} a year for each month or part of a month, from its due date until paid or the next due date, and the last to {}; \
                 the interest to {} is {}",
                y.rate,
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
                    "The rate for the days after {} isn't published yet. {}.",
                    unrated.add_days(-1).long(),
                    y.unrated
                ),
            );
        }
        if waiver_opt.is_some() && waiver != WaiverKind::None {
            notes.rule(
                "N.J.S.A. 54:49-11(a)",
                "the Director may waive only interest above three points over prime, so there is no waiver of this interest for casualty, retirement or disability; it is shown in full",
            );
            if p.total().0 > 0 {
                let o = estimated::waiver_refused(
                    "New Jersey can waive only interest above three points over prime. This interest can't be waived for retirement, disability, casualty or disaster.",
                );
                p = p.to_builder().waiver_outcome(o).build()?;
            }
        }
        Ok(working::new_result(p).with(&a.notes).with(&notes).build()?)
    }

    fn explanation(&self) -> Explanation {
        NEW_JERSEY_EXPLANATION.clone()
    }
}

/// Written by hand and checked against research/estimated-payments/nj.md.
static NEW_JERSEY_EXPLANATION: LazyLock<Explanation> = LazyLock::new(|| {
    estimated::new_explanation(
        "New Jersey expects estimated payments above a $400 threshold. That's your tax after withholding and credits. Falling short costs interest at prime plus 3%, 10% for 2026. It's charged for each month or part of a month.",
    )
    .paragraph("Withholding, refundable credits and estimated payments must reach a target. The target is the smaller of 80% of this year's tax or 100% of last year's. A quarter is due by April 15, June 15, September 15 and January 15. There is no 110% figure in practice. The Division charges interest only against those two amounts.")
    .paragraph("A quarter also avoids interest if payments so far reach one of four measures. The first is last year's tax. The second is the tax on last year's income at this year's rates. The third is 80% of the tax on your income annualized to date. The fourth is 90% of the tax on your actual income to date. The last two do not apply to January. Filing and paying in full by February 15 replaces the January payment.")
    .paragraph("Interest runs from each due date until the shortfall is paid. It stops on April 15 of the next year. Any part of a month counts as a whole month. Withholding and refundable credits count as paid a quarter on each date. You can show other dates instead. The Division cannot waive this interest for reasonable cause. Nor can it waive it for a casualty, retirement or disability.")
    .versus_federal("New Jersey's current-year test is 80% rather than 90%. Its threshold is $400 rather than $1,000, and the 110% tier never applies. Four exceptions excuse a quarter outright. Interest is charged by the month at prime plus 3%. Filing by February 15, not January 31, replaces the January payment. There is no casualty, retirement or disability waiver.")
    .source("N.J.S.A. 54A:8-4, 54A:8-5, 54A:9-6, 54:49-3, 54:49-11")
    .source("Form NJ-2210, 2025 Underpayment of Estimated Tax, and its instructions")
    .source("2026 Form NJ-1040-ES")
    .source("GIT-8, Estimating Income Taxes (January 2026)")
    .source("TB-21(R), Interest Rate Assessed on Tax Balances for 2026")
    .source("Notice Concerning Estimated Tax for Taxpayers Earning Over $150,000")
    .build()
    .unwrap_or_default()
});

/// A shorthand for the year files.
pub(crate) fn date(y: i64, m: Month, d: i64) -> Date {
    estimated::must_date(y, m, d)
}
