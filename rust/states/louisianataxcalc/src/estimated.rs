//! Louisiana's estimated tax rules, La. R.S. 47:116 to 47:118, as the shared
//! [`EstimatedTax`] interface.
//!
//! The penalty is computed as Form R-210R computes it: a required annual
//! payment, the lesser of this year's share and last year's tax, divided
//! into the installments due; payments carried forward from one period to
//! the next; exceptions that excuse an installment outright; and a printed
//! daily factor on each shortfall for the days it was unpaid.
//!
//! Act 307 of 2026 changed the rules for tax periods beginning on or after
//! January 1, 2026. It took effect on May 22, 2026, after the department's
//! latest penalty form, R-210R-i (1/26), and its estimated tax page (updated
//! February 27, 2025) were published, and the legislature's statute page for
//! R.S. 47:118 still prints the earlier text. Where those documents describe
//! the old law, the figure follows the Act, and the result's Divergences
//! name the out-of-date document. Everywhere else the figure follows the
//! form, and the Divergences say where the statute reads otherwise.

use std::any::Any;

use estimated::{
    Basis, Calendar, Date, Error, EstimatedTax, Explanation, FactorSeries, Facts, Installment,
    LedgerEntry, Month, Penalty, Requirement, Rounding, TargetOption, WaiverKind, WaiverOutcome,
};
use money::{Rate, USCents, percent};
use num_bigint::BigInt;
use working::{Divergence, DivergenceBuilder, Notes, PositionKind, Reading};

/// jurisdiction is Louisiana's postal code.
const JURISDICTION: estimated::Jurisdiction = estimated::LOUISIANA;

/// The facts Louisiana's rules read that no other jurisdiction does. Carry
/// them in [`Facts::state`].
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct EstimatedFacts {
    prior_facts_tax: Option<USCents>,
    joint_statute_threshold: bool,
    joint_threshold_answered: bool,
}

impl EstimatedFacts {
    /// The tax for R.S. 47:118(D)(1)(b) as Act 307 words it from 2026: last
    /// year's return figures taxed at this year's 3% rate and this year's
    /// standard deduction ($12,875, or $25,750 for a joint return or a head
    /// of household, IT-540ESi (2026) line 3), less last year's credits as
    /// R-210R Exception 3 line 5 subtracts them, or `None` if not given.
    /// The caller computes it; this crate holds no Louisiana income tax
    /// tables.
    pub fn prior_year_facts_tax(&self) -> Option<USCents> {
        self.prior_facts_tax
    }

    /// Whether a joint return is held to the statute's $1,000 rather than
    /// the department's $2,000, or `None` if the caller did not say. R.S.
    /// 47:116(A) states one $1,000 figure; IT-540ESi and R-210R Exception 1
    /// use $2,000 for a joint return.
    pub fn statute_joint_threshold(&self) -> Option<bool> {
        self.joint_threshold_answered
            .then_some(self.joint_statute_threshold)
    }
}

/// Builds [`EstimatedFacts`].
#[derive(Clone, Debug, Default)]
pub struct EstimatedFactsBuilder {
    e: EstimatedFacts,
}

/// Starts an [`EstimatedFacts`] with nothing given.
pub fn new_estimated_facts() -> EstimatedFactsBuilder {
    EstimatedFactsBuilder::default()
}

impl EstimatedFactsBuilder {
    /// Gives the tax on last year's facts at this year's rate.
    pub fn prior_year_facts_tax(&mut self, a: USCents) -> &mut Self {
        self.e.prior_facts_tax = Some(a);
        self
    }

    /// Answers the joint threshold question: true holds a joint return to
    /// $1,000, false to $2,000.
    pub fn statute_joint_threshold(&mut self, statute: bool) -> &mut Self {
        (
            self.e.joint_statute_threshold,
            self.e.joint_threshold_answered,
        ) = (statute, true);
        self
    }

    /// Refuses a negative tax.
    pub fn build(&self) -> Result<EstimatedFacts, Error> {
        if self.e.prior_facts_tax.unwrap_or_default().0 < 0 {
            return Err(Error::Invalid(
                "louisianataxcalc: last year's facts cannot give a negative tax".into(),
            ));
        }
        Ok(self.e)
    }
}

/// One tax year's published figures.
#[derive(Clone, Debug)]
pub(crate) struct EstimatedYear {
    pub(crate) year: i64,

    /// The share of this year's tax in the required annual payment and in
    /// R-210R Exception 5.
    pub(crate) this_year_share: Rate,

    /// What this year's tax less withholding must exceed before any penalty
    /// can arise; `threshold_joint` is the department's figure for a joint
    /// return.
    pub(crate) threshold: USCents,
    pub(crate) threshold_joint: USCents,

    pub(crate) dues: Vec<Date>,

    /// The first days of R-210R's second, third and fourth rows: a
    /// requirement first met on or after `later_groups[i]` starts at
    /// installment i+2.
    pub(crate) later_groups: Vec<Date>,

    pub(crate) calendar: Calendar,
    pub(crate) file_and_pay_by: Date,
    pub(crate) stop: Date,
    pub(crate) period_ends: Vec<Date>,
    pub(crate) daily: FactorSeries,
}

/// The 25%, 50%, 75% and 100% R-210R Exceptions 2 and 3 apply to last
/// year's figures, and the equal part of withholding R.S. 47:118(E)(2)
/// deems paid on each installment date.
fn quarters() -> [Rate; 4] {
    [percent(25), percent(25), percent(25), percent(25)]
}

/// Returns Louisiana's estimated tax rules for a tax year, or
/// [`Error::NoRules`] for a year this build does not carry.
pub fn estimated_tax(year: i64) -> Result<Box<dyn EstimatedTax>, Error> {
    let y = match year {
        2026 => crate::estimated_y2026::year_2026()?,
        _ => return Err(estimated::no_rules(&JURISDICTION, year)),
    };
    Ok(Box::new(LouisianaEstimated { y }))
}

struct LouisianaEstimated {
    y: EstimatedYear,
}

/// What the three questions share.
#[derive(Clone, Debug, Default)]
struct Assessment {
    requirement: Requirement,
    exempt: bool,
    dues: Vec<Date>,

    /// Whether this year's tax was given.
    tax_known: bool,
    /// Whether the regular installments rest on last year's tax alone, so
    /// the penalty is the most it can be.
    upper_bound: bool,

    /// R-210R line 5 in each column: the required annual payment divided by
    /// the number of installments, in whole dollars, and nothing before the
    /// requirement was first met.
    regular: Vec<USCents>,

    /// The running totals that excuse an installment, R-210R Exceptions 2,
    /// 3 and 5, each with the reason it gives.
    exceptions: Vec<Exception>,

    excused_january: bool,
    notes: Notes,
}

#[derive(Clone, Debug, Default)]
struct Exception {
    cum: Vec<USCents>,
    why: &'static str,
}

/// Go's `%T` of the extras, as far as it can be told from here.
fn type_name(v: &(dyn Any + Send + Sync)) -> &'static str {
    if v.is::<&str>() || v.is::<String>() {
        "string"
    } else {
        "an unknown type"
    }
}

/// When Act 307 took effect against the documents that predate it.
const OUT_OF_DATE: &str = "Act 307 of 2026 took effect May 22, 2026, for tax periods beginning on or after January 1, 2026; R-210R-i is revised 1/26, LDR's estimated tax page was updated February 27, 2025, and the legislature's R.S. 47:118 page still prints the text before the Act";

/// What an out-of-date document's reading says of itself.
const PREDATES: &str = " These documents predate Act 307 and are out of date for 2026.";

/// Which divergences change anything for one household's facts.
#[derive(Clone, Copy, Debug, Default)]
struct Affected {
    share: bool,
    annualised: bool,
    facts: bool,
    joint: bool,
    withholding: bool,
    part_year: bool,
    late_start: bool,
    joint_statute: bool,
}

fn build_all(bs: &[DivergenceBuilder]) -> Result<Vec<Divergence>, Error> {
    let mut out = Vec::new();
    for b in bs {
        out.push(b.build()?);
    }
    Ok(out)
}

/// The order payments are applied in, read from the checked-in R-210R-i and
/// R.S. 47:118. It affects a ledger in which a payment on or after a later
/// due date made up an earlier shortfall.
fn ledger_divergence(ledger: &[LedgerEntry]) -> Result<Divergence, Error> {
    let mut affects = false;
    for (i, l) in ledger.iter().enumerate() {
        for p in l.pieces() {
            if let Some(on) = p.paid_on()
                && i + 1 < ledger.len()
                && !on.before(ledger[i + 1].due())
            {
                affects = true;
            }
        }
    }
    Ok(working::new_divergence("Which shortfall a payment pays first")
        .form(
            "Each period's overpayment or underpayment is carried to the next. A payment first makes up an earlier shortfall.",
            "R-210R-i (1/26), Lines 7 to 9 and the example under Line 7",
        )
        .statute(
            "A payment on a due date goes first to that date's installment. Only what exceeds that date's installment pays an earlier shortfall.",
            "La. R.S. 47:118(C)(2)",
        )
        .uses(Reading::Form)
        .affects(affects)
        .build()?)
}

/// An option's label and amount, for the middle of a sentence.
fn option_label(o: &TargetOption) -> String {
    let l = o.label();
    let mut chars = l.chars();
    let l = match chars.next() {
        Some(c) => c.to_lowercase().collect::<String>() + chars.as_str(),
        None => String::new(),
    };
    format!("{l}, {}", o.amount().whole())
}

fn due_list(ds: &[Date]) -> String {
    let mut s = String::new();
    for (i, d) in ds.iter().enumerate() {
        if i == 0 {
        } else if i == ds.len() - 1 {
            s.push_str(" and ");
        } else {
            s.push_str(", ");
        }
        s.push_str(&d.long());
    }
    s
}

/// Whether an installment is excused, and why: the January installment for
/// a return filed and paid by January 31, or a date an exception covers,
/// with what the exception needed by it.
#[derive(Clone, Debug, Default)]
struct Standing {
    excused: bool,
    why: &'static str,
    needs: USCents,
}

/// A question whose answer can change the target, asked only when no
/// exception sets it.
struct Ask {
    kind: PositionKind,
    question: String,
    effect: String,
}

impl LouisianaEstimated {
    fn extras(&self, f: &Facts) -> Result<EstimatedFacts, Error> {
        match f.state() {
            None => Ok(EstimatedFacts::default()),
            Some(s) => match s.downcast_ref::<EstimatedFacts>() {
                Some(e) => Ok(*e),
                None => Err(Error::Invalid(format!(
                    "louisianataxcalc: the facts carry {}, not louisianataxcalc.EstimatedFacts",
                    type_name(s)
                ))),
            },
        }
    }

    fn check(&self, f: &Facts) -> Result<(), Error> {
        if f.jurisdiction() != JURISDICTION {
            return Err(Error::Invalid(format!(
                "louisianataxcalc: these are Louisiana's rules, and the facts are for {}",
                f.jurisdiction()
            )));
        }
        if f.tax_year() != self.y.year {
            return Err(Error::Invalid(format!(
                "louisianataxcalc: these are the {} rules, and the facts are for {}",
                self.y.year,
                f.tax_year()
            )));
        }
        if f.farmer_or_fisher() {
            return Err(Error::with_detail(
                Error::Unsupported,
                "a farmer or fisherman with two-thirds of gross income from farming or fishing makes one payment by January 15 at 66 2/3%, which is not built",
            ));
        }
        if f.elections().annualised() {
            for (i, p) in f.periods().iter().enumerate() {
                let want = self.y.period_ends.get(i).copied().unwrap_or_default();
                if p.end() != want {
                    return Err(Error::Invalid(format!(
                        "louisianataxcalc: R-210R Exception 5 period {} ends on {}, not {}",
                        i + 1,
                        want,
                        p.end()
                    )));
                }
            }
        }
        Ok(())
    }

    /// Where Louisiana's documents and R.S. 47:116 to 47:118 part ways on a
    /// rule the requirement uses. The first three are where a document
    /// predates Act 307 and the figure follows the Act. They are found in
    /// research/estimated-payments/la.md. The rules the ledger and the
    /// penalty use are in [`ledger_divergence`] and `penalty_divergences`.
    fn divergences(&self, a: Affected) -> Result<Vec<Divergence>, Error> {
        let joint = if a.joint_statute {
            Reading::Statute
        } else {
            Reading::Form
        };
        build_all(&[
            working::new_divergence("This year's share of the tax")
                .form(format!("The required payment and Exception 5 use 90% of this year's tax.{PREDATES}"),
                    format!("R-210R-i (1/26), Section 1 and Line 14; LDR, Declaration of Estimated Income Taxes (updated February 27, 2025); {OUT_OF_DATE}"))
                .statute("The required payment and income-to-date exception use 70% of this year's tax.",
                    "La. R.S. 47:118(B)(1)(a), (B)(2)(b)(i) and (D)(2), as amended by Act 307 of 2026, §§ 1 and 3")
                .uses(Reading::Statute)
                .affects(a.share)
                .clone(),
            working::new_divergence("The annualized income exception")
                .form(format!("Exception 4 annualizes income to each calendar quarter. It excuses a payment that reaches 90% of the tax on it.{PREDATES}"),
                    format!("R-210R-i (1/26), Line 13; LDR, Declaration of Estimated Income Taxes (updated February 27, 2025); {OUT_OF_DATE}"))
                .statute("Exception 4 is repealed for 2026. The income-to-date exception, at 70%, remains.",
                    "Act 307 of 2026, § 2, repealing La. R.S. 47:118(D)(1)(c)")
                .uses(Reading::Statute)
                .affects(a.annualised)
                .clone(),
            working::new_divergence("Last year's figures at this year's rate")
                .form("Exception 3 uses last year's taxable income, after last year's deductions. It taxes that at this year's rate and filing status, less last year's credits. This form predates Act 307 and is out of date for 2026.",
                    format!("R-210R-i (1/26), Line 12, Exception 3 worksheet; {OUT_OF_DATE}"))
                .statute("Last year's figures are taxed at this year's rate and standard deduction.",
                    "La. R.S. 47:118(D)(1)(b), as amended by Act 307 of 2026, §§ 1 and 3")
                .uses(Reading::Statute)
                .affects(a.facts)
                .clone(),
            working::new_divergence("The threshold for a joint return")
                .form("No penalty when this year's tax less credits and withholding is $1,000 or less. On a joint return the figure is $2,000.",
                    "IT-540ESi (2026), Who Must Make Estimated Tax Payments; R-210R-i (1/26), Line 10, Exception 1")
                .statute("One threshold of $1,000 for everyone. Act 307 didn't change it.",
                    "La. R.S. 47:116(A); La. R.S. 47:118(D)")
                .uses(joint)
                .affects(a.joint)
                .clone(),
            working::new_divergence("When withholding counts as paid")
                .form("Withholding may be spread over the periods by the number of months in each. Or it may be counted on the dates withheld. The form doesn't define the months.",
                    "R-210R-i (1/26), General Information, Applying Credits and Withholding")
                .statute("An equal part counts as paid on each due date. You can show the dates withheld instead.",
                    "La. R.S. 47:118(E)(2)")
                .uses(Reading::Statute)
                .affects(a.withholding)
                .clone(),
            working::new_divergence("Last year's tests after a part-year return")
                .form("Last year's tax and figures aren't used after a part-year resident return.",
                    "R-210R-i (1/26), Line 3, Line 11 Exception 2 and Line 12 Exception 3")
                .statute("The required payment and the exceptions use last year's return. They set no condition of full-year residence.",
                    "La. R.S. 47:118(B)(2)(b)(ii) and (D)(1)(a), (b)")
                .uses(Reading::Form)
                .affects(a.part_year)
                .clone(),
            working::new_divergence("Needing to pay only after April 1")
                .form("If you first need to pay after April 1, only the dates left are due. Each is 1/3, 1/2 or all of the required payment.",
                    "R-210R-i (1/26), General Information table, Lines 4 and 5; IT-540ESi (2026), Line 11")
                .statute("Each installment for the penalty is 25% of the required payment. The fewer, equal installments are how a late declaration is paid.",
                    "La. R.S. 47:118(B)(2)(a); La. R.S. 47:117(A), 47:117.1(A)")
                .uses(Reading::Form)
                .affects(a.late_start)
                .clone(),
        ])
    }

    /// The stop date, which Act 307 moved, and the daily rate. The stop date
    /// affects a penalty charged after the old April 15 stop, and the rate
    /// any penalty charged.
    fn penalty_divergences(&self, p: &Penalty) -> Result<Vec<Divergence>, Error> {
        let old = estimated::must_date(self.y.year + 1, Month::APRIL, 15);
        let late = p
            .lines()
            .iter()
            .any(|l| l.parts().iter().any(|part| part.to().after(old)));
        build_all(&[
            working::new_divergence("When the penalty stops")
                .form(format!("The penalty stops April 15, the 15th day of the fourth month after year end.{PREDATES}"),
                    format!("R-210R-i (1/26), Section 1 and Line 17; LDR, Declaration of Estimated Income Taxes (updated February 27, 2025); {OUT_OF_DATE}"))
                .statute(format!("The penalty runs to the 15th day of the fifth month after the year ends. For {} that is {}.", self.y.year, self.y.stop.long()),
                    "La. R.S. 47:118(C)(1), as amended by Act 307 of 2026, §§ 1 and 3")
                .uses(Reading::Statute)
                .affects(late)
                .clone(),
            working::new_divergence("The daily rate")
                .form("Each day is charged at a printed 0.0329%, or 0.000329 a day.",
                    "R-210R-i (1/26), Line 18")
                .statute("Twelve percent a year, 0.000328767 a day over 365 days.",
                    "La. R.S. 47:118(A)")
                .uses(Reading::Form)
                .affects(p.total().0 > 0)
                .clone(),
        ])
    }

    /// A date as a person reads it: "Aug 31" in the tax year, and "Jan 15,
    /// 2027" outside it.
    fn short_date(&self, d: Date) -> String {
        if d.year() == self.y.year {
            return d.format("Jan 2");
        }
        d.format("Jan 2, 2006")
    }

    /// R-210R Exception 1's figure for the filing status, and whether the
    /// joint question needs asking.
    fn threshold_for(&self, s: filing::Status, e: &EstimatedFacts) -> (USCents, bool) {
        if s != filing::Status::MarriedFilingJointly {
            return (self.y.threshold, false);
        }
        let statute = e.statute_joint_threshold();
        if statute == Some(true) {
            return (self.y.threshold, false);
        }
        (self.y.threshold_joint, statute.is_none())
    }

    fn assess(&self, f: &Facts) -> Result<Assessment, Error> {
        self.check(f)?;
        let extras = self.extras(f)?;
        let y = &self.y;
        let last = y.year - 1;
        let mut a = Assessment {
            dues: estimated::move_dates(&y.calendar, &y.dues),
            ..Assessment::default()
        };

        let tax_opt = f.this_year().tax();
        let tax_known = tax_opt.is_some();
        let tax = tax_opt.unwrap_or_default();
        a.tax_known = tax_known;
        let withheld = f.withheld_total();
        let prior_ret = f.prior_year();
        let has_prior = prior_ret.is_some();
        let prior = prior_ret.unwrap_or_default();
        let mut prior_tax = USCents(0);
        if has_prior && let Some(pf) = prior.figures() {
            prior_tax = pf.tax().unwrap_or_default();
        }
        let resident_opt = prior.full_year_resident();
        let residence_answered = resident_opt.is_some();
        let resident = resident_opt.unwrap_or_default();

        // R-210R line 1, at Act 307's 70%.
        let share = y.this_year_share;
        let this_year = if tax_known {
            estimated::new_target_option(Basis::ThisYearTax, share, share.of(tax)).build()?
        } else {
            estimated::new_target_option(Basis::ThisYearTax, share, USCents(0))
                .unavailable(estimated::NEEDS_THIS_YEAR_TAX)
                .build()?
        };
        if tax_known {
            a.notes.rule(
                "La. R.S. 47:118(B)(2)(b)(i), as amended by Act 307 of 2026; R-210R-i (1/26), Line 1",
                format!(
                    "{} of this year's Louisiana tax after credits of {} is {}",
                    share,
                    tax,
                    this_year.amount()
                ),
            );
        }

        let mut target_asks: Vec<Ask> = Vec::new();

        // Why last year's return cannot be used, or "" when it can.
        let mut prior_blocked = String::new();
        if !has_prior {
            prior_blocked = format!("Needs your {last} Louisiana return");
            target_asks.push(Ask {
                kind: PositionKind::PriorYearReturn,
                question: format!(
                    "Did you file a {last} Louisiana return, and what tax did it show?"
                ),
                effect: format!(
                    "With it, last year's tax could set your target instead of {} of this year's.",
                    y.this_year_share
                ),
            });
        } else if !prior.filed() {
            prior_blocked = format!("You didn't file a {last} Louisiana return");
        } else if !residence_answered {
            prior_blocked =
                format!("Needs whether you were a full-year Louisiana resident in {last}");
            target_asks.push(Ask {
                kind: PositionKind::Residency,
                question: format!("Were you a full-year Louisiana resident in {last}?"),
                effect:
                    "If you were, last year's return can set your target; a part-year return can't."
                        .into(),
            });
        } else if !resident {
            prior_blocked = format!("You filed a part-year resident return for {last}");
        }

        // R-210R line 2: 100% of last year's tax.
        let prior_opt = if prior_blocked.is_empty() {
            estimated::new_target_option(Basis::PriorYearTax, percent(100), prior_tax).build()?
        } else {
            estimated::new_target_option(Basis::PriorYearTax, percent(100), USCents(0))
                .unavailable(prior_blocked.clone())
                .build()?
        };
        if prior_opt.available() {
            a.notes.rule(
                "La. R.S. 47:118(B)(2)(b)(ii); R-210R-i (1/26), Line 2",
                format!("100% of the {prior_tax} tax on the {last} return is {prior_tax}"),
            );
        }

        // R.S. 47:118(D)(1)(b), Exception 3: last year's facts at this
        // year's rate and standard deduction, supplied by the caller.
        let facts_opt_in = extras.prior_year_facts_tax();
        let facts_known = facts_opt_in.is_some();
        let facts_tax = facts_opt_in.unwrap_or_default();
        const FACTS_LABEL: &str = "Last year's figures at this year's rate";
        let facts_opt = if !prior_blocked.is_empty() {
            estimated::new_target_option(Basis::PriorYearIncome, percent(100), USCents(0))
                .label(FACTS_LABEL)
                .unavailable(prior_blocked.clone())
                .build()?
        } else if !facts_known {
            estimated::new_target_option(Basis::PriorYearIncome, percent(100), USCents(0))
                .label(FACTS_LABEL)
                .unavailable(format!(
                    "Needs the Louisiana tax on your {last} figures at the {} rate. It uses the {} standard deduction",
                    y.year, y.year
                ))
                .build()?
        } else {
            estimated::new_target_option(Basis::PriorYearIncome, percent(100), facts_tax)
                .label(FACTS_LABEL)
                .build()?
        };
        if facts_opt.available() {
            a.notes.rule(
                "La. R.S. 47:118(D)(1)(b), as amended by Act 307 of 2026",
                format!(
                    "the {last} return's facts at the {} rate and standard deduction give {facts_tax}; an installment is excused when payments by its date reach 25%, 50%, 75% or 100% of it",
                    y.year
                ),
            );
        }

        let mut rb = estimated::new_requirement();
        rb.option(this_year.clone())
            .option(prior_opt.clone())
            .option(facts_opt.clone());
        let (mut chosen, mut target, mut found) = (Basis::Unspecified, USCents(0), false);
        let mut chosen_opt = TargetOption::default();
        for o in [&this_year, &prior_opt, &facts_opt] {
            if o.available() && (!found || o.amount() < target) {
                (chosen, target, found, chosen_opt) = (o.basis(), o.amount(), true, o.clone());
            }
        }

        let (threshold, ask_joint) = self.threshold_for(f.filing_status(), &extras);
        if !tax_known {
            a.notes.ask(
                PositionKind::ThisYearTax,
                format!("What do you expect your {} Louisiana tax to be?", y.year),
                format!(
                    "No payments are needed at {} or less after withholding. The target may be {share} of your tax, if lower.",
                    threshold.whole()
                ),
            );
        }

        if tax_known && tax - withheld <= threshold {
            a.exempt = true;
            let ex =
                estimated::exception_at_most("Tax less withholding", tax - withheld, threshold)?;
            let mut why = format!(
                "Your Louisiana tax after credits is {}, and {} is withheld. That leaves {}, which is {} or less. No estimated payments are needed.",
                tax.whole(),
                withheld.whole(),
                (tax - withheld).whole(),
                threshold.whole()
            );
            if tax < withheld {
                why = format!(
                    "Your Louisiana tax after credits is {}, and {} is withheld. That {}, so no estimated payments are needed.",
                    tax.whole(),
                    withheld.whole(),
                    estimated::leaves(tax - withheld)
                );
            } else if tax == withheld {
                why = format!(
                    "Your withholding of {} covers your Louisiana tax after credits, {}. No estimated payments are needed.",
                    withheld.whole(),
                    tax.whole()
                );
            }
            rb.required(false, why).exempt(ex);
            a.notes.rule(
                "La. R.S. 47:116(A), 47:118(D); IT-540ESi (2026); R-210R-i (1/26), Line 10, Exception 1",
                format!(
                    "{} of tax less {} withheld is {}, not more than {}, so there is no penalty",
                    tax,
                    withheld,
                    tax - withheld,
                    threshold
                ),
            );
        } else if !found {
            rb.required(
                false,
                format!(
                    "Your target comes from your {} Louisiana tax or your {last} return. Until you have one, there's no target.",
                    y.year
                ),
            )
            .no_target();
        } else if !tax_known && withheld >= target {
            rb.required(
                false,
                format!(
                    "Your target is {}. Your withholding of {} already reaches it. So no estimated payments are needed whatever your {} tax turns out to be.",
                    option_label(&chosen_opt),
                    withheld.whole(),
                    y.year
                ),
            )
            .choose(chosen);
        } else if !tax_known {
            rb.required(
                true,
                format!(
                    "Your target is {}. It keeps you clear of a penalty whatever your {} tax turns out to be. After {} of withholding, you need {} of estimated payments.",
                    option_label(&chosen_opt),
                    y.year,
                    withheld.whole(),
                    (target - withheld).whole()
                ),
            )
            .choose(chosen);
        } else if target.0 == 0 {
            rb.required(
                false,
                format!(
                    "Your target is {}. No estimated payments are needed.",
                    option_label(&chosen_opt)
                ),
            )
            .choose(chosen);
        } else if withheld >= target {
            rb.required(
                false,
                format!(
                    "Your Louisiana withholding of {} already reaches your target of {}. No estimated payments are needed.",
                    withheld.whole(),
                    target.whole()
                ),
            )
            .choose(chosen);
        } else {
            let mut lead = format!(
                "Your Louisiana tax less withholding leaves {}, which is more than {}.",
                (tax - withheld).whole(),
                threshold.whole()
            );
            if (tax - withheld).whole() == threshold.whole() {
                lead = format!(
                    "Your Louisiana tax less withholding is more than {}.",
                    threshold.whole()
                );
            }
            rb.required(
                true,
                format!(
                    "{lead} Your target is {}. After withholding, you need {} of estimated payments.",
                    option_label(&chosen_opt),
                    (target - withheld).whole()
                ),
            )
            .choose(chosen);
        }
        if !a.exempt {
            for q in &target_asks {
                a.notes.ask(q.kind, q.question.clone(), q.effect.clone());
            }
        }
        a.requirement = rb.build()?;
        let joint_statute = extras.statute_joint_threshold().unwrap_or_default();
        let met = f.requirement_first_met();
        let live = !a.exempt && found;
        let annualised = f.elections().annualised();
        let divergences = self.divergences(Affected {
            share: live
                && tax_known
                && (!prior_opt.available() || share.of(tax) < prior_opt.amount() || annualised),
            annualised: live && annualised,
            facts: live && facts_opt.available(),
            joint: f.filing_status() == filing::Status::MarriedFilingJointly
                && tax_known
                && tax - withheld > y.threshold
                && tax - withheld <= y.threshold_joint,
            withholding: live && withheld.0 > 0 && !f.elections().withholding_as_withheld(),
            part_year: !a.exempt && has_prior && prior.filed() && residence_answered && !resident,
            late_start: live && !met.is_zero() && !met.before(y.later_groups[0]),
            joint_statute,
        })?;
        a.notes.diverges(&divergences);
        if ask_joint
            && tax_known
            && tax - withheld > y.threshold
            && tax - withheld <= y.threshold_joint
        {
            a.notes.ask(
                PositionKind::UnsettledRule,
                "",
                format!(
                    "The law sets one {} threshold; the department gives joint returns {}. With {} due after withholding, your figures follow the department.",
                    y.threshold.whole(),
                    y.threshold_joint.whole(),
                    (tax - withheld).whole()
                ),
            );
        }
        if a.exempt || !found {
            a.regular = vec![USCents(0); a.dues.len()];
            return Ok(a);
        }
        if prior_blocked.is_empty() && !facts_known {
            a.notes.ask(
                PositionKind::JurisdictionFact,
                format!(
                    "What is the tax on your {last} figures at the {} rate and standard deduction?",
                    y.year
                ),
                "Paying 25%, 50%, 75% and 100% of it by the four dates also avoids the penalty.",
            );
        }

        // R-210R line 3: the lesser of lines 1 and 2. Without this year's
        // tax it is line 2, the most line 3 can be. Last year's facts are
        // open only with last year's return, so there is always a line 2
        // when there is a target.
        let mut line3 = this_year.amount();
        if !tax_known {
            (line3, a.upper_bound) = (prior_opt.amount(), true);
        } else if prior_opt.available() && prior_opt.amount() < line3 {
            line3 = prior_opt.amount();
        }

        let first = self.late_start(f, &mut a);
        let n = a.dues.len() - first;
        a.regular = vec![USCents(0); a.dues.len()];
        // R-210R line 5: line 3 divided by n, rounded to the whole dollar.
        let each = Rounding::RoundToDollar.div(&BigInt::from(line3.0), &BigInt::from(n));
        for r in a.regular.iter_mut().skip(first) {
            *r = each;
        }
        a.notes.rule(
            "La. R.S. 47:118(B)(2); R-210R-i (1/26), Lines 3 to 5",
            format!(
                "the required annual payment of {line3} divided by {n} installments is {each}, in whole dollars, due on each of {}",
                due_list(&a.dues[first..])
            ),
        );

        // Exceptions 2 and 3, a quarter of last year's figure a date.
        if prior_opt.available() && prior.months() == 12 && prior_tax.0 > 0 {
            let cum = estimated::cumulative_shares(prior_tax, &quarters())?;
            a.exceptions.push(Exception {
                cum,
                why: "Exception 2: payments reached last year's tax for the dates so far",
            });
        }
        if facts_opt.available() {
            let cum = estimated::cumulative_shares(facts_tax, &quarters())?;
            a.exceptions.push(Exception {
                cum,
                why: "Exception 3: payments so far reached last year's facts at this year's rate",
            });
        }

        // Exception 5: 70% of the tax on the income actually earned to each
        // period end. The caller's Period::annualised_tax carries that tax.
        if annualised {
            let mut cum = vec![USCents(0); a.dues.len()];
            for (i, p) in f.periods().iter().enumerate() {
                if let Some(c) = cum.get_mut(i) {
                    *c = share.of(p.annualised_tax());
                }
            }
            a.notes.rule(
                "La. R.S. 47:118(D)(2), as amended by Act 307 of 2026; R-210R-i (1/26), Line 14",
                format!(
                    "an installment is also excused when payments by its date reach {share} of the tax on the income earned to March 31, May 31, August 31 and December 31: {}, {}, {} and {}",
                    cum[0], cum[1], cum[2], cum[3]
                ),
            );
            a.exceptions.push(Exception {
                cum,
                why: "Exception 5: payments reached 70% of the tax on income earned to period end",
            });
        }

        // January 31: R.S. 47:116(F). January 31, 2027 is a Sunday;
        // IT-540ESi prints January 31 and says nothing of moving it, so it
        // is not moved.
        let filed = f.return_filed();
        if !filed.is_zero() && !filed.after(y.file_and_pay_by) {
            a.excused_january = true;
            a.notes.rule(
                "La. R.S. 47:116(F); IT-540ESi (2026), Exception to the January 15th Declaration Requirement",
                format!(
                    "the return was filed and paid on {}, by {}, so the January installment is excused; earlier shortfalls are not",
                    filed.long(),
                    y.file_and_pay_by.long()
                ),
            );
        }
        Ok(a)
    }

    /// The first installment the requirement applies to, from 0.
    fn late_start(&self, f: &Facts, a: &mut Assessment) -> usize {
        let y = &self.y;
        let met = f.requirement_first_met();
        if met.is_zero() {
            a.notes.ask(
                PositionKind::RequirementFirstMet,
                format!(
                    "When did you first expect to need estimated payments for {}?",
                    y.year
                ),
                format!(
                    "Until then, Jan 1 is assumed. A start after {} means fewer, larger payments.",
                    self.short_date(y.later_groups[0].add_days(-1))
                ),
            );
            return 0;
        }
        let mut first = 0;
        for (i, g) in y.later_groups.iter().enumerate() {
            if !met.before(*g) {
                first = i + 1;
            }
        }
        if first > 0 {
            a.notes.rule(
                "La. R.S. 47:117(A), 47:117.1(A); R-210R-i (1/26), General Information table and Line 4",
                format!(
                    "the requirement was first met on {}, so {} installments are required, from {}",
                    met.long(),
                    y.dues.len() - first,
                    a.dues[first].long()
                ),
            );
        }
        first
    }

    /// Applies the credits to the regular installments as R-210R lines 6 to
    /// 9 do.
    fn ledger(&self, f: &Facts, a: &Assessment) -> Result<(Vec<LedgerEntry>, Notes), Error> {
        let credits = if f.elections().withholding_as_withheld() {
            estimated::credits_as_withheld(f, &a.dues)?
        } else {
            estimated::credits(f, &a.dues, &quarters())?
        };
        let mut required = a.regular.clone();
        if a.excused_january {
            required[3] = USCents(0);
        }
        let ledger = estimated::apply(&a.dues, &required, &credits)?;
        let mut notes = Notes::default();
        if f.elections().withholding_as_withheld() {
            notes.rule(
                "La. R.S. 47:118(E)(2); R-210R-i (1/26), Applying Credits and Withholding",
                format!(
                    "by election, the {} withheld is counted on the dates it was withheld",
                    f.withheld_total()
                ),
            );
        } else {
            notes.rule(
                "La. R.S. 47:118(E)(2)",
                format!(
                    "an equal part of the {} withheld is treated as paid on each installment date, whenever it was actually withheld",
                    f.withheld_total()
                ),
            );
        }
        notes.rule(
            "R-210R-i (1/26), Lines 6 to 9",
            "each period's overpayment is carried to the next, and a payment after a due date first makes up the oldest shortfall, on the day it is paid",
        );
        Ok((ledger, notes))
    }

    fn standings(&self, a: &Assessment, ledger: &[LedgerEntry]) -> Vec<Standing> {
        ledger
            .iter()
            .enumerate()
            .map(|(i, l)| {
                if a.excused_january && i == 3 {
                    return Standing {
                        excused: true,
                        why: "the return was filed and paid by January 31",
                        needs: USCents(0),
                    };
                }
                for e in &a.exceptions {
                    if l.credited_by() >= e.cum[i] {
                        return Standing {
                            excused: true,
                            why: e.why,
                            needs: e.cum[i],
                        };
                    }
                }
                Standing::default()
            })
            .collect()
    }

    /// The due dates with the regular running total needed by each, excused
    /// where an exception covers it.
    fn installments(
        &self,
        a: &Assessment,
        ledger: &[LedgerEntry],
    ) -> Result<Vec<Installment>, Error> {
        let mut out = Vec::with_capacity(ledger.len());
        for (i, s) in self.standings(a, ledger).iter().enumerate() {
            let l = &ledger[i];
            let mut b = estimated::new_installment(i as i64 + 1, l.due());
            b.needed_by(l.needed_by())
                .credited_by(l.credited_by())
                .shortfall(l.shortfall());
            if s.excused {
                b.excused();
            }
            out.push(b.build()?);
        }
        Ok(out)
    }
}

/// Decides a waiver asked for. La. R.S. 47:118(I) lets the secretary waive
/// the penalty for good faith, presumed for extraordinary circumstances
/// beyond the individual's control, on an application within a year of the
/// return's due date. It has no retirement or disability waiver.
fn louisiana_waiver(f: &Facts) -> Option<WaiverOutcome> {
    match f.positions().waiver()? {
        WaiverKind::None => None,
        WaiverKind::Casualty => Some(estimated::waiver_eligible(
            "You'd apply for a waiver on Form R-20128 within a year of the return's due date. Louisiana's revenue secretary may grant it for events beyond your control.",
        )),
        _ => Some(estimated::waiver_refused(
            "Louisiana has no waiver for retirement or disability. The department waives only for good faith, such as events beyond your control.",
        )),
    }
}

impl EstimatedTax for LouisianaEstimated {
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
                    "with no {} Louisiana return to measure against, there is no target to schedule",
                    self.y.year - 1
                ),
            ));
        }
        let (ledger, mut notes) = self.ledger(f, &a)?;
        let out = self.installments(&a, &ledger)?;
        for (i, s) in self.standings(&a, &ledger).iter().enumerate() {
            let l = &ledger[i];
            if !s.excused || l.shortfall().0 == 0 {
            } else if s.needs.0 > 0 {
                notes.rule(
                    "La. R.S. 47:118(D); R-210R-i (1/26), Section 3",
                    format!(
                        "the {} installment was {} short and bears no penalty under {}, which needs {} by then",
                        l.due().long(),
                        l.shortfall(),
                        s.why,
                        s.needs
                    ),
                );
            } else {
                notes.rule(
                    "La. R.S. 47:118(D); R-210R-i (1/26), Section 3",
                    format!(
                        "the {} installment was {} short and bears no penalty under {}",
                        l.due().long(),
                        l.shortfall(),
                        s.why
                    ),
                );
            }
        }
        let order = ledger_divergence(&ledger)?;
        notes.diverges(&[order]);
        Ok(working::new_result(out)
            .with(&a.notes)
            .with(&notes)
            .build()?)
    }

    fn penalty(&self, f: &Facts, as_of: Date) -> Result<working::Result<Penalty>, Error> {
        if as_of.is_zero() {
            return Err(Error::Invalid(
                "louisianataxcalc: a penalty needs the date to figure it to".into(),
            ));
        }
        let a = self.assess(f)?;
        let y = &self.y;
        if !a.requirement.target_known() {
            return Err(Error::with_detail(
                Error::NeedsThisYearTax,
                format!(
                    "with no {} Louisiana return to measure against, there is no target to figure a penalty from",
                    y.year - 1
                ),
            ));
        }
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
        let schedule = self.installments(&a, &ledger)?;
        let order = ledger_divergence(&ledger)?;
        if a.exempt {
            let p = estimated::new_penalty(stop)
                .waiver(waiver)
                .because(a.requirement.clone(), &schedule)
                .build()?;
            let mut ds = vec![order];
            ds.extend(self.penalty_divergences(&p)?);
            let mut dn = Notes::default();
            dn.diverges(&ds);
            return Ok(working::new_result(p).with(&a.notes).with(&dn).build()?);
        }
        let standings = self.standings(&a, &ledger);
        let mut pb = estimated::new_penalty(stop);
        pb.waiver(waiver).because(a.requirement.clone(), &schedule);
        for (i, s) in standings.iter().enumerate() {
            let l = &ledger[i];
            if s.excused || l.shortfall().0 == 0 {
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
                if !gap.is_zero() {
                    return Err(Error::Invalid(format!(
                        "louisianataxcalc: the daily factor does not cover {gap}"
                    )));
                }
                for p in parts {
                    lb.part(p);
                }
            }
            let line = lb.days(last_day.days_since(l.due())).build()?;
            pb.line(line);
        }
        if a.upper_bound {
            pb.upper_bound();
        }
        let mut p = pb.build()?;
        if a.upper_bound {
            notes.rule(
                "La. R.S. 47:118(B)(2)(b); R-210R-i (1/26), Line 3",
                format!(
                    "this year's Louisiana tax was not given, so the penalty is figured against 100% of the {} tax alone; the required annual payment is the lesser of that and 70% of this year's tax, so {} is the most the penalty can be, and it is less if this year's tax turns out lower",
                    y.year - 1,
                    p.total()
                ),
            );
        }
        if paid_in_full {
            notes.rule(
                "La. R.S. 47:118(C)(2); R-210R-i (1/26), Line 16",
                format!(
                    "the return was filed with the whole balance paid on {}, so every shortfall still outstanding was paid that day",
                    stop.long()
                ),
            );
        }
        notes.rule(
            "La. R.S. 47:118(A), (C)(1), as amended by Act 307 of 2026; R-210R-i (1/26), Lines 15 to 19",
            format!(
                "each shortfall no exception covers is charged 0.000329 a day, to the cent, from its due date until paid or {}; the penalty to {} is {}",
                y.stop.long(),
                stop.long(),
                p.total()
            ),
        );
        if p.total().0 > 0 && !waiver_answered {
            notes.ask(
                PositionKind::PenaltyWaiver,
                "Are you asking Louisiana on Form R-20128 to waive your penalty for good faith?",
                "Circumstances beyond your control are presumed good faith. So is declaring and paying before the return is due; apply within a year after.",
            );
        }
        if let Some(o) = louisiana_waiver(f)
            && p.total().0 > 0
        {
            p = p.to_builder().waiver_outcome(o).build()?;
        }
        let mut ds = vec![order];
        ds.extend(self.penalty_divergences(&p)?);
        notes.diverges(&ds);
        Ok(working::new_result(p).with(&a.notes).with(&notes).build()?)
    }

    /// Written by hand and checked against research/estimated-payments/la.md.
    fn explanation(&self) -> Explanation {
        estimated::new_explanation(
            "Louisiana expects estimated payments once you'll have more than $1,000 due. That's after credits and withholding, and $2,000 for a joint return. From 2026 you avoid the penalty by paying the lesser of two amounts. One is 70% of this year's tax, and the other is all of last year's.",
        )
        .paragraph("Act 307, a 2026 law, cut the required share of this year's tax from 90% to 70%. The other figure is 100% of last year's Louisiana tax. There's no higher figure for high incomes. A third test taxes last year's figures at this year's rate. It uses this year's standard deduction too. A payment date is also excused if what you paid by then reaches that tax.")
        .paragraph("Equal payments are due April 15, June 15, September 15 and the next January 15. A date on a weekend or holiday is due the next business day instead. Withholding counts as paid in equal parts on the four dates. You can instead show when it was actually withheld. Filing your return with full payment by January 31 replaces the January payment. Earlier payments are still due.")
        .paragraph("The penalty is 12% a year on each shortfall, charged by the day until it's paid. From 2026 it stops on May 15 of the next year rather than April 15. A payment first makes up an earlier shortfall, then counts toward the next date. Another test taxes the income you actually earned to the end of each period. Paying 70% of that tax by the next due date also excuses that date. The department can waive the penalty if you acted in good faith. Good faith is presumed for circumstances beyond your control. It is also presumed if you declared and paid before the return was due. You apply on Form R-20128 within a year of the return's due date.")
        .versus_federal("Unlike federal, Louisiana uses 70% of this year's tax, not 90%. It has no 110% figure at higher incomes. Its threshold is $1,000 after withholding, or $2,000 on a joint return. A third test taxes last year's figures at this year's rate. Starting late means fewer, larger installments. The income-to-date test has no annualizing. The rate is a fixed 12%, and the penalty runs to May 15.")
        .source("La. R.S. 47:116, 47:117, 47:117.1, 47:118")
        .source("Act 307 of the 2026 Regular Session (HB 633)")
        .source("IT-540ESi (2026), Declaration of Estimated Tax for Individuals: General Information and Instructions")
        .source("R-210R-i (1/26), Underpayment of Individual Income Tax Penalty Computation, 2025")
        .source("Louisiana Department of Revenue, 2026 Legislative Summaries (June 26, 2026)")
        .build()
        .unwrap_or_default()
    }
}
