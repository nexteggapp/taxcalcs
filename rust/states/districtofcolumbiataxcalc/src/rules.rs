//! The District of Columbia's estimated tax rules, D.C. Code § 47-4203 and §
//! 47-1812.08(i), as the shared [`estimated::EstimatedTax`] interface. The
//! interest rate is D.C. Code § 47-4201(d).
//!
//! The District's section is written on the federal pattern, but it is the
//! District's own text, not an adoption of 26 U.S.C. § 6654, and nothing here
//! borrows from the federal crate. The prior-year test is always 110%; the
//! threshold is $100; the charge is interest at a fixed 10%, which D-2210
//! turns into one printed factor for each span between due dates.
//!
//! The figure follows the Office of Tax and Revenue's forms, D-40ES for 2026
//! and D-2210 for 2025, and the result's divergences say where the D.C. Code
//! reads otherwise. Sections 47-4201, 47-4203 and 47-1812.08(i) were not
//! changed in substance in 2024, 2025 or 2026; D.C. Law 26-89 (effective
//! February 12, 2026) only moved the power to make regulations under §
//! 47-1812.08(i) to the Chief Financial Officer. So no form here predates the
//! law it describes.

use std::any::Any;

use estimated::{
    Basis, Calendar, Date, Error, EstimatedTax, Explanation, Factor, Facts, Installment,
    Jurisdiction, LedgerEntry, PartKind, Penalty, PenaltyPart, Requirement, Rounding,
    TargetOptionBuilder, WaiverKind,
};
use money::{Rate, USCents};
use num_bigint::BigInt;
use working::{Divergence, Notes, PositionKind, Reading};

/// The District's postal code.
const JURISDICTION: Jurisdiction = estimated::DISTRICT_OF_COLUMBIA;

/// One tax year's published figures.
pub(crate) struct EstimatedYear {
    pub(crate) year: i64,

    /// $100: D-40ES asks for payments when the tax after withholding and
    /// credits is that or more, and D-2210 charges no interest when the tax
    /// less withholding and payments is less.
    pub(crate) threshold: USCents,

    /// D-2210 lines 2 and 4.
    pub(crate) this_year_percent: Rate,
    pub(crate) prior_percent: Rate,

    /// D-2210's exception for a balance under this share of the year's tax.
    pub(crate) remaining: Rate,

    pub(crate) dues: Vec<Date>,

    /// The first days of the groups in § 47-1812.08(i)(4)(A) to (C): a
    /// requirement first met on or after `later_groups[i]` makes the first
    /// declaration due on `dues[i+1]`.
    pub(crate) later_groups: Vec<Date>,

    pub(crate) calendar: Calendar,

    /// § 47-4203(c)(2)(B): the period of an underpayment ends no later than
    /// the 15th day of the 4th month after the year, which is also the end
    /// of D-2210's fourth span.
    pub(crate) stop: Date,

    /// D-2210 line 10, one for each span from a due date to the next, and
    /// from the last to `stop`.
    pub(crate) factors: Vec<Factor>,

    /// The § 47-4201(d)(2) rate the factors stand for, for the working.
    pub(crate) rate: Rate,

    /// The ends of the 3, 5 and 8 months of § 47-4203(b)(2), and the year.
    pub(crate) period_ends: Vec<Date>,
}

/// D-2210 line 6 and § 47-4203(b)(1)(B): each installment is 25% of the
/// required annual payment. § 47-4203(d)(3) deems withholding paid in the
/// same equal parts.
fn quarters() -> [Rate; 4] {
    [
        money::percent(25),
        money::percent(25),
        money::percent(25),
        money::percent(25),
    ]
}

/// The year this build carries, or `None`.
fn estimated_year(year: i64) -> Option<Result<EstimatedYear, Error>> {
    match year {
        2026 => Some(crate::estimated_y2026::year_2026()),
        _ => None,
    }
}

/// Returns the District of Columbia's estimated tax rules for a tax year, or
/// [`Error::NoRules`] for a year this build does not carry.
pub fn estimated_tax(year: i64) -> Result<Box<dyn EstimatedTax>, Error> {
    match estimated_year(year) {
        Some(y) => Ok(Box::new(DistrictEstimated { y: y? })),
        None => Err(estimated::no_rules(&JURISDICTION, year)),
    }
}

struct DistrictEstimated {
    y: EstimatedYear,
}

/// What the three questions share.
#[derive(Default)]
struct Assessment {
    requirement: Requirement,
    exempt: bool,
    tax_known: bool,
    tax: USCents,
    dues: Vec<Date>,

    /// Each installment's own amount: a quarter of the target, or the form's
    /// proration by income under the annualised method.
    installments: Vec<USCents>,

    /// A return filed with the balance paid by the fourth due date, which
    /// takes the place of the fourth payment.
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

/// For each divergence, whether the other reading would give these facts a
/// different answer.
#[derive(Default)]
struct Affects {
    who: bool,
    under100: bool,
    prior_test: bool,
    no_liability: bool,
    remaining: bool,
    list110: bool,
    annualised: bool,
    factors: bool,
    late_start: bool,
    waiver: bool,
    holiday: bool,
}

/// Which of D-2210's exceptions excuses the year once the estimated
/// payments are counted.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum ExcuseKind {
    None,
    Threshold,
    Remaining,
}

/// A date as a person reads it: "Jun 15" in the tax year, and "Jan 15,
/// 2027" outside it.
fn short(d: Date, year: i64) -> String {
    let month = d.month().to_string();
    let m = &month[..3];
    if d.year() == year {
        return format!("{m} {}", d.day());
    }
    format!("{m} {}, {}", d.day(), d.year())
}

/// a × b ÷ c, rounded half up, for amounts that are not negative.
fn mul_div(a: USCents, b: USCents, c: USCents) -> USCents {
    let n = BigInt::from(a.0) * BigInt::from(b.0);
    Rounding::RoundToCent.div(&n, &BigInt::from(c.0))
}

impl DistrictEstimated {
    fn check(&self, f: &Facts) -> Result<(), Error> {
        if f.jurisdiction() != JURISDICTION {
            return Err(invalid(format!(
                "districtofcolumbiataxcalc: these are the District of Columbia's rules, and the facts are for {}",
                f.jurisdiction()
            )));
        }
        if f.tax_year() != self.y.year {
            return Err(invalid(format!(
                "districtofcolumbiataxcalc: these are the {} rules, and the facts are for {}",
                self.y.year,
                f.tax_year()
            )));
        }
        if let Some(s) = f.state() {
            return Err(invalid(format!(
                "districtofcolumbiataxcalc: the District's rules read no extras, and the facts carry {}",
                type_name(s)
            )));
        }
        if f.elections().annualised() {
            for (i, p) in f.periods().iter().enumerate() {
                if p.end() != self.y.period_ends[i] {
                    return Err(invalid(format!(
                        "districtofcolumbiataxcalc: annualisation period {} ends on {}, not {}",
                        i + 1,
                        self.y.period_ends[i],
                        p.end()
                    )));
                }
            }
        }
        Ok(())
    }

    /// Where the Office of Tax and Revenue's forms and the D.C. Code part
    /// ways on a rule every District computation uses. They are found in
    /// research/estimated-payments/dc.md and checked against the Code's text
    /// in testdata/. The figures follow the forms, except on the 110% item in
    /// the exception list, where they follow the form's lines 6 to 11, which
    /// measure each installment as the statute does.
    fn divergences(&self, af: &Affects) -> Result<Vec<Divergence>, Error> {
        Ok(vec![
            working::new_divergence("Who must make estimated payments")
                .form("You need payments if you expect $100 or more due after withholding and credits. The penalty form's own instructions say more than $100.",
                    "D-40ES (REV. 03/2026), Who must file a Form D-40ES?; 2025 D-40 booklet")
                .statute("The test is the tax on income not subject to withholding. A declaration is needed only when it will be more than $100. Exactly $100 therefore doesn't need one.",
                    "D.C. Code § 47-1812.08(i)(1)")
                .uses(Reading::Form)
                .affects(af.who)
                .build()?,
            working::new_divergence("Tax under $100")
                .form("Take the year's tax less withholding and estimated payments. Under $100 means no interest, with no condition that the payments were on time. The front of the form counts only withholding and credits.",
                    "D-2210 (05/2025), front, item A, and instructions, Are there any exceptions")
                .statute("Take the tax less credits and estimated payments that were timely made. No interest is due when that is under $100.",
                    "D.C. Code § 47-4203(e)(1)")
                .uses(Reading::Form)
                .affects(af.under100)
                .build()?,
            working::new_divergence("The prior-year test")
                .form("110% of last year's tax is open only if you filed a DC return. You must also have lived in DC all of last year.",
                    "D-2210 (05/2025), front, item B, and instructions for lines 4 and 5; D-40ES (REV. 03/2026)")
                .statute("The test is 110% of the tax on last year's return. It needs a return consisting of 12 months, with no condition of residence.",
                    "D.C. Code § 47-4203(b)(1)(B)(ii)(II)")
                .uses(Reading::Form)
                .affects(af.prior_test)
                .build()?,
            working::new_divergence("No liability last year")
                .form("No interest if you had no DC tax liability last year. You must also have lived in DC the entire 12 months of it.",
                    "D-2210 (05/2025), instructions, Are there any exceptions")
                .statute("No interest if last year had no tax liability, with no condition of residence.",
                    "D.C. Code § 47-4203(e)(2)")
                .uses(Reading::Form)
                .affects(af.no_liability)
                .build()?,
            working::new_divergence("A balance under 10% of the year's tax")
                .form("Take the tax left after credits, estimated payments and withholding. There's no interest if it is less than 10% of the year's DC tax, whenever paid.",
                    "D-2210 (05/2025), instructions, Are there any exceptions")
                .statute("There's no such exception. Interest is excused only in the five cases the law lists.",
                    "D.C. Code § 47-4203(e)")
                .uses(Reading::Form)
                .affects(af.remaining)
                .build()?,
            working::new_divergence("The 110% item in the exception list")
                .form("The form's list of exceptions has an item for 110% of last year's tax. It excuses interest once estimated payments and withholding reach that amount. The item is listed without saying by when.",
                    "D-2210 (05/2025), front, item B, instructions, Are there any exceptions, and lines 6 to 11")
                .statute("Each installment is 25% of the lesser of two figures. They are 90% of this year's tax and 110% of last year's. An installment is short by whatever wasn't paid by its due date. The form's lines for each period measure it the same way.",
                    "D.C. Code § 47-4203(b)(1)(B), (c)(2)(A)")
                .uses(Reading::Statute)
                .affects(af.list110)
                .build()?,
            working::new_divergence("The annualised income method")
                .form("Each period's share of the required payment is a proration. It is the income to that period over the year's income.",
                    "D-2210 (05/2025), instructions for line 7")
                .statute("The law uses 22.5%, 45%, 67.5% and 90% of the tax on annualised income. Income is annualised for the first 3, 5 and 8 months and the year. A part-year return can't use it except under regulations.",
                    "D.C. Code § 47-4203(b)(2)")
                .uses(Reading::Form)
                .affects(af.annualised)
                .build()?,
            working::new_divergence("The interest factors")
                .form("The form prints a factor for the whole span to the next due date. Each period's running underpayment on its due date is multiplied by it. The factors are .0175, .0265, .0351 and .0259. A payment during a span counts only from the next due date.",
                    "D-2210 (05/2025), lines 8 to 11")
                .statute("Interest is 10% a year compounded daily, on each installment's underpayment. It runs from the due date until paid or Apr 15 of the next year. A payment during a month is treated as made at the end of that month.",
                    "D.C. Code §§ 47-4201(d)(2), 47-4203(c)")
                .uses(Reading::Form)
                .affects(af.factors)
                .build()?,
            working::new_divergence("A requirement first met after Apr 1")
                .form("A quarter of the year's amount is due on each of the four dates. That holds whenever the requirement arose.",
                    "D-2210 (05/2025), lines 6 and 7")
                .statute("The installments fall on the declaration dates. If first met after Apr 1, Jun 1 or Sep 1, those are Jun 15, Sep 15 or Jan 15. The law doesn't say how the installments are then sized.",
                    "D.C. Code §§ 47-4203(a), 47-1812.08(i)(4)")
                .uses(Reading::Form)
                .affects(af.late_start)
                .build()?,
            working::new_divergence("Retirement, disability, casualty and death")
                .form("The form lists no waiver for retirement, disability or a casualty. Nor does it list one for the taxpayer's death.",
                    "D-2210 (05/2025), instructions, Are there any exceptions")
                .statute("The Mayor can find you retired after 62 or became disabled this year or last. With reasonable cause, there is then no interest. None is due if a casualty, disaster or other unusual event makes it inequitable. There is none for the year of death.",
                    "D.C. Code § 47-4203(e)(3) to (5)")
                .uses(Reading::Form)
                .affects(af.waiver)
                .build()?,
            working::new_divergence("Due dates on a weekend or holiday")
                .form("A voucher due on a weekend or legal holiday is due the next business day. The form doesn't say which holidays count. No 2026 due date moves.",
                    "D-40ES (REV. 03/2026), When are your vouchers due?")
                .statute("The declaration is due by Apr 15, Jun 15, Sep 15 or Jan 15 of the next year. There is no rule for a weekend or holiday.",
                    "D.C. Code § 47-1812.08(i)(4)")
                .uses(Reading::Form)
                .affects(af.holiday)
                .build()?,
        ])
    }

    fn assess(&self, f: &Facts) -> Result<Assessment, Error> {
        self.check(f)?;
        let y = &self.y;
        let mut a = Assessment::default();

        let tax_opt = f.this_year().tax();
        let tax_known = tax_opt.is_some();
        let tax = tax_opt.unwrap_or_default();
        (a.tax_known, a.tax) = (tax_known, tax);
        let withheld = f.withheld_total();
        let prior = f.prior_year();
        let has_prior = prior.is_some();
        let last = y.year - 1;
        let prior_tax = prior
            .and_then(|p| p.figures())
            .and_then(|pf| pf.tax())
            .unwrap_or_default();
        let residence = prior.and_then(|p| p.full_year_resident());
        let (resident, residence_answered) = (residence == Some(true), residence.is_some());

        if f.farmer_or_fisher() {
            a.notes.rule(
                "D.C. Code § 47-4203; D-2210 (05/2025)",
                "neither has a separate rule for farmers or fishermen, so the general rules apply",
            );
        }

        // 90% of this year's tax. D-2210 lines 1 and 2.
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
                "D-2210 (05/2025), lines 1 and 2; D.C. Code § 47-4203(b)(1)(B)(i)",
                format!(
                    "{} of this year's DC tax of {} is {}",
                    y.this_year_percent,
                    tax,
                    this_year.amount()
                ),
            );
        }

        // 110% of last year's tax, for a filer who filed and lived in DC all
        // year. D-2210 lines 3 to 5.
        let mut residence_questions: Vec<String> = Vec::new();
        let unavailable = |why: String| -> TargetOptionBuilder {
            let mut b =
                estimated::new_target_option(Basis::PriorYearTax, y.prior_percent, USCents(0));
            b.unavailable(why);
            b
        };
        let prior_option = match prior {
            None => unavailable(format!("Needs your {last} DC return")),
            Some(p) if p.not_required_to_file() || !p.filed() => {
                unavailable(format!("You didn't file a {last} DC return"))
            }
            Some(p) if p.months() != 12 => unavailable(format!(
                "Your {last} return covered {} months, not 12",
                p.months()
            )),
            Some(_) if !residence_answered => {
                residence_questions.push(format!("the {} option is open", y.prior_percent));
                unavailable(format!("Needs whether you lived in DC all of {last}"))
            }
            Some(_) if !resident => {
                a.notes.rule(
                    "D.C. Code § 47-4203(b)(1)(B)(ii)(II)",
                    format!(
                        "the statute asks only for a filed 12-month {last} return, which would allow {} of the {} tax on it, {}; D-2210 requires a full year in DC, so the figures here are the form's",
                        y.prior_percent,
                        prior_tax,
                        y.prior_percent.of(prior_tax)
                    ),
                );
                unavailable(format!("You didn't live in DC all of {last}"))
            }
            Some(_) => {
                let amount = y.prior_percent.of(prior_tax);
                a.notes.rule(
                    "D-2210 (05/2025), lines 3 and 4; D.C. Code § 47-4203(b)(1)(B)(ii)(II)",
                    format!(
                        "{} of the {} tax on the {last} DC return is {}; there is no 100% tier and no income test",
                        y.prior_percent, prior_tax, amount
                    ),
                );
                estimated::new_target_option(Basis::PriorYearTax, y.prior_percent, amount)
            }
        };
        let prior_opt = prior_option.build()?;

        let mut rb = estimated::new_requirement();
        rb.option(this_year.clone()).option(prior_opt.clone());
        let (mut chosen, mut target) = (Basis::ThisYearTax, this_year.amount());
        if prior_opt.available() && (!tax_known || prior_opt.amount() < target) {
            (chosen, target) = (Basis::PriorYearTax, prior_opt.amount());
        }
        if tax_known && prior_opt.available() {
            a.notes.rule(
                "D-2210 (05/2025), line 5",
                format!(
                    "the required annual payment is the lesser of {} and {}, {}",
                    this_year.amount(),
                    prior_opt.amount(),
                    target
                ),
            );
        }
        // No liability last year, for a filer who lived in DC all of it.
        let no_liability = prior.is_some_and(|p| {
            p.months() == 12 && ((p.filed() && prior_tax.0 == 0) || p.not_required_to_file())
        });
        if no_liability && !residence_answered {
            residence_questions.push("having no DC tax that year excuses you".into());
        }
        if no_liability && residence_answered && !resident {
            a.notes.rule(
                "D.C. Code § 47-4203(e)(2)",
                format!(
                    "you had no DC liability for {last}, and the statute excuses the interest for {} on that alone; D-2210 also requires that you lived in DC all of {last}, so the figures here are the form's, and the exception is yours to claim",
                    y.year
                ),
            );
        }
        if !residence_questions.is_empty() {
            a.notes.ask(
                PositionKind::Residency,
                format!("Did you live in DC all of {last}?"),
                format!(
                    "Your answer decides whether {}.",
                    residence_questions.join(". It also decides whether ")
                ),
            );
        }

        let mut remaining_exempt = false;
        if no_liability && residence_answered && resident {
            a.exempt = true;
            let none = estimated::new_exception("No DC tax last year").build()?;
            rb.required(
                false,
                format!(
                    "You had no DC tax for {last} and lived in DC all of it. No estimated payments are needed for {}.",
                    y.year
                ),
            )
            .exempt(none);
            a.notes.rule(
                "D-2210 (05/2025), instructions, Are there any exceptions; D.C. Code § 47-4203(e)(2)",
                format!(
                    "no DC liability for {last}, as a resident all 12 months, so there is no interest for {}",
                    y.year
                ),
            );
        } else if !tax_known && !prior_opt.available() {
            rb.required(
                false,
                format!(
                    "No {last} DC return sets a target. There is none until your {} DC tax is figured.",
                    y.year
                ),
            )
            .no_target();
        } else if !tax_known && withheld >= target {
            rb.required(
                false,
                format!(
                    "Your withholding of {} covers {} of your {last} DC tax, {}. No estimated payments are needed, whatever your {} tax turns out to be.",
                    withheld.whole(),
                    y.prior_percent,
                    target.whole(),
                    y.year
                ),
            )
            .choose(chosen);
        } else if !tax_known {
            rb.required(
                true,
                format!(
                    "Your target is {} of your {last} DC tax, {}. It keeps you clear of interest whatever your {} tax turns out to be. After {} of withholding, that's {} of estimated payments. Pay a quarter by each date.",
                    y.prior_percent,
                    target.whole(),
                    y.year,
                    withheld.whole(),
                    (target - withheld).whole()
                ),
            )
            .choose(chosen);
        } else if tax - withheld < y.threshold {
            a.exempt = true;
            let below =
                estimated::exception_below("Tax less withholding", tax - withheld, y.threshold)?;
            let under = if tax < withheld {
                String::new()
            } else {
                format!(", under {}", y.threshold.whole())
            };
            rb.required(
                false,
                format!(
                    "Your DC tax is {}, and {} is withheld. That {}{}. No estimated payments are needed.",
                    tax.whole(),
                    withheld.whole(),
                    estimated::leaves(tax - withheld),
                    under
                ),
            )
            .exempt(below);
            a.notes.rule(
                "D-40ES (REV. 03/2026); D-2210 (05/2025), front, item A",
                format!(
                    "{} of tax less {} withheld is {}, under {}, so there is no interest",
                    tax,
                    withheld,
                    tax - withheld,
                    y.threshold
                ),
            );
        } else if self.under_remaining(tax, tax - withheld) {
            a.exempt = true;
            remaining_exempt = true;
            let below = estimated::exception_below(
                "Tax less withholding",
                tax - withheld,
                self.remaining_limit(tax),
            )?;
            rb.required(
                false,
                format!(
                    "Your withholding of {} leaves {} of your {} of DC tax to pay. That is less than {} of it, so no interest is charged. No estimated payments are needed.",
                    withheld.whole(),
                    (tax - withheld).whole(),
                    tax.whole(),
                    y.remaining
                ),
            )
            .exempt(below);
            a.notes.rule(
                "D-2210 (05/2025), instructions, Are there any exceptions",
                format!(
                    "{} of tax less {} withheld leaves {}, less than {} of the tax, so there is no interest",
                    tax,
                    withheld,
                    tax - withheld,
                    y.remaining
                ),
            );
            self.remaining_open(&mut a.notes);
        } else if withheld >= target {
            rb.required(
                false,
                format!(
                    "Your DC withholding of {} covers the {} needed for the year. No estimated payments are needed.",
                    withheld.whole(),
                    target.whole()
                ),
            )
            .choose(chosen);
        } else {
            rb.required(
                true,
                format!(
                    "Your DC tax is {}, and {} is withheld. That leaves {}, which is {} or more. You need {} of estimated payments to reach your {} target. Pay a quarter by each date.",
                    tax.whole(),
                    withheld.whole(),
                    (tax - withheld).whole(),
                    y.threshold.whole(),
                    (target - withheld).whole(),
                    target.whole()
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
                format!("Did you file a {last} DC return, and what was its tax?"),
                format!(
                    "With it, {} of last year's tax may be a lower target than {} of this year's.",
                    y.prior_percent, y.this_year_percent
                ),
            );
        }
        if !a.exempt && !tax_known {
            let until = if a.requirement.target_known() {
                "your target can only come from last year's tax"
            } else {
                "there's no target"
            };
            a.notes.ask(
                PositionKind::ThisYearTax,
                format!("What do you expect your {} DC tax to be?", y.year),
                format!(
                    "Until then, {until}. Whether the {} and {} exceptions apply stays open.",
                    y.threshold.whole(),
                    y.remaining
                ),
            );
        }

        // The due dates. § 47-1812.08(i)(4) fixes them, and D-40ES moves one
        // that falls on a weekend or legal holiday.
        a.dues = estimated::move_dates(&y.calendar, &y.dues);
        let mut moved = false;
        for i in 0..a.dues.len() {
            if a.dues[i] != y.dues[i] {
                moved = true;
                a.notes.rule(
                    "D-40ES (REV. 03/2026), When are your vouchers due?",
                    format!(
                        "the voucher due {} falls on a weekend or legal holiday, so it is due {}",
                        y.dues[i].long(),
                        a.dues[i].long()
                    ),
                );
            }
        }
        if !moved {
            a.notes.rule(
                "D-40ES (REV. 03/2026), When are your vouchers due?",
                "a voucher due on a Saturday, Sunday or legal holiday is due the next business day; no due date this year is a Saturday or Sunday, and no list of the holidays that count is published",
            );
        }
        let cumulative = estimated::cumulative_shares(a.requirement.target(), &quarters())?;
        a.installments = estimated::increments(&cumulative);
        a.notes.rule(
            "D-2210 (05/2025), lines 6 and 7; D.C. Code § 47-4203(b)(1)(B)",
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
            self.annualise(f, &mut a);
        }

        let mut late_start = false;
        let met = f.requirement_first_met();
        if !met.is_zero() && !a.exempt {
            for i in (0..y.later_groups.len()).rev() {
                if !met.before(y.later_groups[i]) {
                    late_start = true;
                    a.notes.ask(
                        PositionKind::UnsettledRule,
                        "",
                        format!(
                            "First needing to pay on {} makes your first payment due {}. The law doesn't say how much; the figures charge a quarter on all four dates.",
                            short(met, y.year),
                            short(a.dues[i + 1], y.year)
                        ),
                    );
                    break;
                }
            }
        }

        // A return filed with the balance paid by January 15 takes the place
        // of the fourth payment. D-40ES says not to send the last voucher; §
        // 47-1812.08(i)(6) treats the return as the declaration. Neither
        // excuses an earlier shortfall.
        let filed = f.return_filed();
        if !filed.is_zero() && !filed.after(a.dues[3]) && !a.exempt {
            a.excused_january = true;
            a.notes.rule(
                "D-40ES (REV. 03/2026), Filing a return before last payment; D.C. Code § 47-1812.08(i)(6)",
                format!(
                    "the return was filed and the balance paid on {}, by {}, so it takes the place of the fourth payment; interest on an earlier shortfall still runs",
                    filed.long(),
                    a.dues[3].long()
                ),
            );
        }
        if a.exempt {
            a.installments = vec![USCents(0); a.dues.len()];
        }

        let mut af = Affects {
            who: !a.exempt && tax_known && tax - withheld == y.threshold,
            prior_test: !a.exempt
                && prior.is_some_and(|p| p.filed() && p.months() == 12)
                && residence_answered
                && !resident
                && (!tax_known || y.prior_percent.of(prior_tax) < this_year.amount()),
            no_liability: no_liability && residence_answered && !resident,
            remaining: remaining_exempt,
            annualised: f.elections().annualised() && !a.exempt,
            late_start,
            holiday: moved,
            ..Affects::default()
        };
        if a.requirement.target_known() && !a.exempt {
            let (ledger, _) = self.ledger(f, &a)?;
            let any_short = ledger.iter().any(|l| l.shortfall().0 > 0);
            let mut scratch = Notes::default();
            let kind = self.excuse(f, &a, &mut scratch);
            let mut paid = withheld;
            for p in f.payments() {
                paid += p.amount();
            }
            let waiver = f.positions().waiver().unwrap_or_default();
            af.under100 = any_short && kind == ExcuseKind::Threshold;
            af.remaining = af.remaining || kind == ExcuseKind::Remaining;
            af.list110 = any_short && prior_opt.available() && paid >= prior_opt.amount();
            af.factors = any_short && kind == ExcuseKind::None;
            af.waiver = af.factors && waiver != WaiverKind::None;
        }
        let ds = self.divergences(&af)?;
        a.notes.diverges(&ds);
        Ok(a)
    }

    /// D-2210's 10% exception: what is left to pay is less than the share of
    /// the tax.
    fn under_remaining(&self, tax: USCents, left: USCents) -> bool {
        left.max(USCents(0)).0 * 10_000 < i64::from(self.y.remaining.0) * tax.0
    }

    /// The share of the tax, rounded up to the cent, so that an amount is
    /// under it exactly when [`Self::under_remaining`] holds.
    fn remaining_limit(&self, tax: USCents) -> USCents {
        let n = i64::from(self.y.remaining.0) * tax.max(USCents(0)).0;
        USCents((n + 9_999) / 10_000)
    }

    fn remaining_open(&self, n: &mut Notes) {
        n.ask(
            PositionKind::UnsettledRule,
            "",
            format!(
                "The form excuses a balance less than {} of the year's tax. The law doesn't include that exception, and the figures follow the form.",
                self.y.remaining
            ),
        );
    }

    /// Replaces the installments with D-2210's proration: the required
    /// annual payment times the income to each period's end over the year's
    /// income. The form does not say where the periods end; the statute's 3,
    /// 5 and 8 months are used.
    fn annualise(&self, f: &Facts, a: &mut Assessment) {
        let periods = f.periods();
        let Some(last) = periods.last() else {
            return;
        };
        let total = last.income();
        let target = a.requirement.target();
        if total.0 <= 0 {
            a.notes.rule(
                "D-2210 (05/2025), instructions for line 7",
                format!(
                    "the year's income of {total} cannot be divided by, so the regular installments are used"
                ),
            );
            return;
        }
        let mut cumulative = vec![USCents(0); periods.len()];
        let mut prev = USCents(0);
        let mut lines = Vec::with_capacity(periods.len());
        for (i, p) in periods.iter().enumerate() {
            let share = p.income().max(USCents(0)).min(total);
            cumulative[i] = prev.max(mul_div(target, share, total));
            if i == periods.len() - 1 {
                cumulative[i] = target;
            }
            prev = cumulative[i];
            lines.push(format!(
                "{} of income to {} gives {}",
                p.income(),
                p.end().long(),
                cumulative[i]
            ));
        }
        a.installments = estimated::increments(&cumulative);
        a.notes.rule(
            "D-2210 (05/2025), instructions for line 7",
            format!(
                "under the annualised method each period's running total is {target} times the income to the period's end over the year's {total}: {}",
                lines.join("; ")
            ),
        );
        a.notes.ask(
            PositionKind::UnsettledRule,
            "",
            "The form spreads your required payment by the income earned to each period. The figures use it, not the law's 22.5%, 45%, 67.5% and 90% of annualised tax.",
        );
    }

    /// Applies the credits to the installments.
    fn ledger(&self, f: &Facts, a: &Assessment) -> Result<(Vec<LedgerEntry>, Notes), Error> {
        let credits = if f.elections().withholding_as_withheld() {
            estimated::credits_as_withheld(f, &a.dues)?
        } else {
            estimated::credits(f, &a.dues, &quarters())?
        };
        let ledger = estimated::apply(&a.dues, &a.installments, &credits)?;
        let mut notes = Notes::default();
        if f.elections().withholding_as_withheld() {
            notes.rule(
                "D.C. Code § 47-4203(d)(3)",
                format!(
                    "the {} withheld is credited on the dates it was actually withheld, which the filer must establish for all of it",
                    f.withheld_total()
                ),
            );
        } else {
            notes.rule(
                "D.C. Code § 47-4203(d)(3)",
                format!(
                    "the {} withheld is treated as paid in four equal parts on the due dates, whenever it was actually withheld; D-2210 does not say",
                    f.withheld_total()
                ),
            );
        }
        notes.rule(
            "D.C. Code § 47-4203(d)(1), (4); D-40ES (REV. 03/2026), Applying an overpayment",
            "a payment is credited against the unpaid installments in the order they fall due, and last year's overpayment applied forward counts toward the first",
        );
        Ok((ledger, notes))
    }

    /// Whether D-2210's $100 or 10% exception excuses the whole year once
    /// the estimated payments are counted, and adds the working. The payments
    /// counted are those made by the fourth due date, and last year's
    /// overpayment applied forward.
    fn excuse(&self, f: &Facts, a: &Assessment, notes: &mut Notes) -> ExcuseKind {
        if !a.tax_known || a.exempt {
            return ExcuseKind::None;
        }
        let y = &self.y;
        let mut paid = f.withheld_total();
        for p in f.payments() {
            if p.from_prior_year() || !p.on().after(a.dues[3]) {
                paid += p.amount();
            }
        }
        let left = (a.tax - paid).max(USCents(0));
        if left < y.threshold {
            notes.rule(
                "D-2210 (05/2025), instructions, Are there any exceptions",
                format!(
                    "{} of tax less {} of withholding and estimated payments made by {} leaves {}, under {}, so there is no interest",
                    a.tax,
                    paid,
                    a.dues[3].long(),
                    left,
                    y.threshold
                ),
            );
            return ExcuseKind::Threshold;
        }
        if self.under_remaining(a.tax, left) {
            notes.rule(
                "D-2210 (05/2025), instructions, Are there any exceptions",
                format!(
                    "{} of tax less {} of withholding and estimated payments made by {} leaves {}, less than {} of the tax, so there is no interest",
                    a.tax,
                    paid,
                    a.dues[3].long(),
                    left,
                    y.remaining
                ),
            );
            self.remaining_open(notes);
            return ExcuseKind::Remaining;
        }
        ExcuseKind::None
    }

    /// Where D-2210's span from installment `i` ends: the next due date, or
    /// April 15 after the last.
    fn span_end(&self, a: &Assessment, i: usize) -> Date {
        if i + 1 < a.dues.len() {
            return a.dues[i + 1];
        }
        self.y.stop
    }
}

/// The ledger's due dates, excused where a return filed by January 15
/// replaces the fourth, or where an exception excuses the year.
fn installments(
    a: &Assessment,
    ledger: &[LedgerEntry],
    excused: bool,
) -> Result<Vec<Installment>, Error> {
    let mut out = Vec::with_capacity(ledger.len());
    for (i, l) in ledger.iter().enumerate() {
        let mut b = estimated::new_installment(i as i64 + 1, l.due());
        b.needed_by(l.needed_by())
            .credited_by(l.credited_by())
            .shortfall(l.shortfall());
        if excused || (a.excused_january && i == 3) {
            b.excused();
        }
        out.push(b.build()?);
    }
    Ok(out)
}

impl EstimatedTax for DistrictEstimated {
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
                    "with no {} DC return to measure against, there is no target to schedule",
                    self.y.year - 1
                ),
            ));
        }
        let (ledger, mut notes) = self.ledger(f, &a)?;
        let excused = self.excuse(f, &a, &mut notes) != ExcuseKind::None;
        let out = installments(&a, &ledger, excused)?;
        Ok(working::new_result(out)
            .with(&a.notes)
            .with(&notes)
            .build()?)
    }

    fn penalty(&self, f: &Facts, as_of: Date) -> Result<working::Result<Penalty>, Error> {
        if as_of.is_zero() {
            return Err(invalid(
                "districtofcolumbiataxcalc: a penalty needs the date to figure it to".into(),
            ));
        }
        let a = self.assess(f)?;
        if !a.requirement.target_known() {
            return Err(Error::with_detail(
                Error::NeedsThisYearTax,
                format!(
                    "with no {} DC return to measure against, there is no target to figure a penalty from",
                    self.y.year - 1
                ),
            ));
        }
        let y = &self.y;
        let mut stop = as_of;
        if y.stop.before(stop) {
            stop = y.stop;
        }
        let waiver_answered = f.positions().waiver();
        let waiver = waiver_answered.unwrap_or_default();
        let (ledger, mut notes) = self.ledger(f, &a)?;
        let excused = self.excuse(f, &a, &mut notes) != ExcuseKind::None;
        let schedule = installments(&a, &ledger, excused)?;
        if a.exempt {
            let p = estimated::new_penalty(stop)
                .waiver(waiver)
                .because(a.requirement.clone(), &schedule)
                .build()?;
            return Ok(working::new_result(p).with(&a.notes).build()?);
        }
        if excused {
            let p = estimated::new_penalty(stop)
                .waiver(waiver)
                .because(a.requirement.clone(), &schedule)
                .build()?;
            return Ok(working::new_result(p).with(&a.notes).with(&notes).build()?);
        }

        // D-2210 lines 7 to 11: each column's required running total less
        // what was paid by its due date, times the factor for the span that
        // follows.
        let mut pb = estimated::new_penalty(stop);
        pb.waiver(waiver).because(a.requirement.clone(), &schedule);
        let mut cut = Vec::new();
        for (i, l) in ledger.iter().enumerate() {
            let mut under = (l.needed_by() - l.credited_by()).max(USCents(0));
            if i == 3 && a.excused_january {
                under = USCents(0);
            }
            if under.0 == 0 || !stop.after(l.due()) {
                continue;
            }
            let end = self.span_end(&a, i);
            let mut to = end;
            if stop.before(end) {
                to = stop;
                cut.push(format!(
                    "{} of the {} days from {}",
                    to.days_since(l.due()),
                    end.days_since(l.due()),
                    l.due().long()
                ));
            }
            let part = span_charge(
                under,
                l.due(),
                to,
                end.days_since(l.due()),
                &y.factors[i],
                y.rate,
                format!(
                    "D-2210 (05/2025), line 10: {} for the period from {}",
                    y.factors[i],
                    l.due().long()
                ),
            )?;
            let line = estimated::new_penalty_line(i as i64 + 1, l.due(), under)
                .days(to.days_since(l.due()))
                .part(part)
                .build()?;
            pb.line(line);
        }
        let mut p = pb.build()?;
        if !a.tax_known {
            p = p.to_builder().upper_bound().build()?;
            notes.rule(
                "D-2210 (05/2025), line 5",
                format!(
                    "this year's DC tax was not given, so the interest is figured against {} of the {} tax alone; \
                     the required payment is the lesser of that and {} of this year's tax, and the $100 and 10% exceptions can only excuse more, so {} is the most the interest can be",
                    y.prior_percent,
                    y.year - 1,
                    y.this_year_percent,
                    p.total()
                ),
            );
        }
        let factors: Vec<String> = y.factors.iter().map(|fa| fa.to_string()).collect();
        notes.rule(
            "D-2210 (05/2025), lines 8 to 11; D.C. Code §§ 47-4201(d)(2), 47-4203(c)",
            format!(
                "each period's underpayment, the running total required less withholding and payments credited by its due date, is multiplied by the factor for the span to the next due date or {}, {}, \
                 and a payment during a span counts from the next due date; D-2210 does not say how to round, and the D-40 booklet says to enter whole dollars, so each charge is rounded to the whole dollar; \
                 D-2210 gives no derivation of the factors, but they match 10% compounded daily as an annual rate, 10.5156%, times 61, 92, 122 and 90 days over 365, and 2026's spans have the same lengths, so the 2025 factors are used; the interest to {} is {}",
                y.stop.long(),
                factors.join(", "),
                stop.long(),
                p.total()
            ),
        );
        if !cut.is_empty() {
            notes.rule(
                "D-2210 (05/2025), line 10",
                format!(
                    "D-2210 prints a factor only for a whole span; figured to {}, a span still running is charged that share of its factor by days: {}",
                    stop.long(),
                    cut.join("; ")
                ),
            );
        }
        let filed = f.return_filed();
        if !filed.is_zero() && filed.after(a.dues[3]) {
            notes.rule(
                "D-2210 (05/2025), lines 8 to 11",
                format!(
                    "the return was filed and paid on {}, after {}; D-2210 charges the fourth period's factor to {} whatever day the balance was paid",
                    filed.long(),
                    a.dues[3].long(),
                    y.stop.long()
                ),
            );
        }
        if p.total().0 > 0 && waiver_answered.is_none() {
            notes.ask(
                PositionKind::PenaltyWaiver,
                "Are you asking for the interest to be waived?",
                "It can be for retiring after 62, becoming disabled, or a casualty or disaster. The Office of Tax and Revenue decides; the interest shown is before any waiver.",
            );
        }
        if waiver_answered.is_some() && waiver != WaiverKind::None {
            notes.rule(
                "D.C. Code § 47-4203(e)(3), (4)",
                format!(
                    "you ask for a waiver for {waiver}; the Mayor decides it, and D-2210 does not list it, so the interest shown is before it"
                ),
            );
        }
        // § 47-4203(e)(3) and (4) are the federal pair.
        if let Some(o) = estimated::standard_waiver(f, "the Office of Tax and Revenue", 62)
            && p.total().0 > 0
        {
            p = p.to_builder().waiver_outcome(o).build()?;
        }
        Ok(working::new_result(p).with(&a.notes).with(&notes).build()?)
    }

    fn explanation(&self) -> Explanation {
        district_explanation()
    }
}

/// Charges `principal` at a factor D-2210 prints for a whole span of
/// `span_days` days. A span cut short at `to` is charged the share of the
/// factor its days are of the span's. Each charge is rounded to the whole
/// dollar.
fn span_charge(
    principal: USCents,
    from: Date,
    to: Date,
    span_days: i64,
    f: &Factor,
    annual: Rate,
    source: String,
) -> Result<PenaltyPart, Error> {
    let days = to.days_since(from);
    if days >= span_days {
        return estimated::span_factor(
            principal,
            from,
            to,
            f,
            annual,
            Rounding::RoundToDollar,
            source,
        );
    }
    let (units, places) = factor_digits(f)?;
    let n = BigInt::from(principal.0) * BigInt::from(units) * BigInt::from(days);
    let d = BigInt::from(10).pow(places) * BigInt::from(span_days);
    let charge = Rounding::RoundToDollar.div(&n, &d);
    estimated::new_penalty_part(PartKind::Interest, principal, annual)
        .span(from, to)
        .days(days)
        .charge(charge)
        .source(source)
        .build()
}

/// Reads a printed factor back into its digits and the number of them after
/// the point, which [`Factor`] keeps private.
fn factor_digits(f: &Factor) -> Result<(i64, u32), Error> {
    let printed = f.to_string();
    let (whole, frac) = printed.split_once('.').unwrap_or((&printed, ""));
    let mut units: i64 = 0;
    for c in whole.chars().chain(frac.chars()) {
        let Some(digit) = c.to_digit(10) else {
            return Err(invalid(format!(
                "districtofcolumbiataxcalc: {} is not a printed factor",
                working::quote(&printed)
            )));
        };
        units = units * 10 + i64::from(digit);
    }
    Ok((units, frac.len() as u32))
}

/// Written by hand and checked against research/estimated-payments/dc.md.
fn district_explanation() -> Explanation {
    estimated::new_explanation(
        "Take your DC tax due after withholding and credits. If it is $100 or more, the District expects estimated payments. The charge for paying too little or too late is called underpayment interest. Form D-2210 figures it with a printed factor for each period.")
        .paragraph("There is no interest if withholding and estimated payments reach a target. The target is the smaller of 90% of this year's DC tax or 110% of last year's. The District has no 100% tier: last year's figure is 110% at every income. The form lets you use it only if you filed a DC return for last year. Otherwise only the 90% figure counts.")
        .paragraph("Pay a quarter each by April 15, June 15, September 15 and the next January 15. A date on a weekend or legal holiday becomes the next business day. Withholding counts as paid evenly on the four dates. You can instead show when all of it was actually withheld. An overpayment carried from last year counts toward April. File your return and pay the balance by January 15 to skip the January payment. Interest on any earlier shortfall still runs.")
        .paragraph("D-2210 takes the amount still short at each due date. That counts everything required and paid so far. It is multiplied by a factor for the span to the next due date. The factors are .0175, .0265, .0351 and .0259, the last running to April 15. A payment made between due dates counts only from the next due date. The factors appear to spread the law's rate over each span's days. That rate is 10% a year compounded daily, about 10.5% a year. The law charges the actual days until payment, rounded up to the month's end. Its figure can therefore differ from the form's.")
        .paragraph("No interest is due if tax less withholding and estimated payments is under $100. None is due either if you had no DC tax last year and lived in DC all of it. The form also excuses a balance under 10% of the year's tax. The law doesn't mention that. The law excuses a casualty or disaster. So is someone who retired after 62 or became disabled, with reasonable cause. None of these is on D-2210, so you have to ask the Office of Tax and Revenue.")
        .versus_federal("The threshold is $100, not $1,000. Last year's figure is always 110%, with no income test. The charge is a fixed 10% compounded daily. The form excuses a balance under 10% of the year's tax. Its annualised method is a proration by income. Filing and paying by January 15, not January 31, replaces the last payment.")
        .source("D.C. Code §§ 47-4201(d), 47-4203, 47-1812.08(i)")
        .source("D-40ES, Estimated Payment for Individual Income Tax, 2026 (REV. 03/2026)")
        .source("D-2210, Underpayment of Estimated Income Tax By Individuals, 2025 (revised 05/2025)")
        .source("2025 D-40 booklet")
        .build()
        .unwrap_or_default()
}
