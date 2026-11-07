//! Vermont's estimated income tax rules, 32 V.S.A. §§ 5851, 5852 and 5859,
//! with interest at the rate set under § 3108 and the penalty under
//! § 3202(b)(2), as the shared [`EstimatedTax`] interface.
//!
//! The required annual payment is the lesser of 90% of this year's tax and
//! 100% of last year's, paid a quarter on each due date. A shortfall is
//! charged twice: interest by the day at the year's rate, and a penalty of
//! 1% for each month or part of a month. No penalty or interest is charged
//! when this year's tax less withholding is under $500. Filers with
//! two-thirds of their income from farming or fishing make no estimated
//! payments.
//!
//! The figure follows the Department of Taxes' worksheets, IN-152 and
//! IN-152A (Rev. 10/25), the latest published, and the result's Divergences
//! say where 32 V.S.A. reads otherwise. The statutes were last amended in
//! 2024 (§ 5859, effective July 1, 2024), before the worksheets' revision,
//! so no form here predates the law it describes
//! (research/estimated-payments/vt.md, 2026 changes).

use std::any::Any;

use estimated::{
    Basis, Calendar, Date, Error, EstimatedTax, Explanation, Facts, Installment, Jurisdiction,
    Month, PartKind, Payment, Penalty, PenaltyPart, RateSeries, Requirement, Rounding,
    TargetOptionBuilder, WaiverKind, Weekday,
};
use money::{Rate, USCents};
use num_bigint::BigInt;
use working::{Divergence, Notes, PositionKind, Reading};

use crate::estimated_y2026::year_2026;

/// jurisdiction is Vermont's postal code.
const JURISDICTION: Jurisdiction = estimated::VERMONT;

/// The facts Vermont's rules read that no other jurisdiction does. Carry
/// them in [`Facts::state`].
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct EstimatedFacts {
    prior_farmer: Option<bool>,
    federal_required: USCents,
    federal_installments: Option<Vec<USCents>>,
}

impl EstimatedFacts {
    /// Whether at least two-thirds of last year's gross income came from
    /// farming or fishing, or `None` when the caller has not said. The
    /// IN-111 instructions exempt a filer who meets the test in either year;
    /// [`Facts::farm_share`] and [`Facts::fish_share`] are this year's.
    pub fn prior_year_farmer_or_fisher(&self) -> Option<bool> {
        self.prior_farmer
    }

    /// The federal figures IN-152A scales: the federal required annual
    /// payment, Form 2210 line 9, and the four required installments from
    /// Form 2210 Schedule AI line 27. It is `None` when they were not given.
    pub fn federal_annualised(&self) -> Option<(USCents, Vec<USCents>)> {
        self.federal_installments
            .as_ref()
            .map(|i| (self.federal_required, i.clone()))
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
    /// Says whether two-thirds of last year's gross income came from farming
    /// or fishing.
    pub fn prior_year_farmer_or_fisher(&mut self, farmer: bool) -> &mut Self {
        self.e.prior_farmer = Some(farmer);
        self
    }

    /// Records the federal Form 2210 line 9 and Schedule AI line 27
    /// figures, for a filer who annualises federally.
    pub fn federal_annualised(&mut self, required: USCents, installments: &[USCents]) -> &mut Self {
        self.e.federal_required = required;
        self.e.federal_installments = Some(installments.to_vec());
        self
    }

    /// Refuses federal figures that IN-152A could not divide by: anything
    /// but four installments that are not negative, and a federal required
    /// annual payment that is not above zero.
    pub fn build(&self) -> Result<EstimatedFacts, Error> {
        if let Some(installments) = &self.e.federal_installments {
            if installments.len() != 4 {
                return Err(Error::Invalid(format!(
                    "vermonttaxcalc: Schedule AI has four installments, not {}",
                    installments.len()
                )));
            }
            if self.e.federal_required.0 <= 0 {
                return Err(Error::Invalid(
                    "vermonttaxcalc: IN-152A divides by the federal required annual payment, which must be above zero".into(),
                ));
            }
            for a in installments {
                if a.0 < 0 {
                    return Err(Error::Invalid(format!(
                        "vermonttaxcalc: a federal installment of {a} is negative"
                    )));
                }
            }
        }
        Ok(self.e.clone())
    }
}

/// One tax year's published figures.
#[derive(Clone, Debug)]
pub(crate) struct EstimatedYear {
    pub(crate) year: i64,
    /// The two shares of the required annual payment, 32 V.S.A. § 5851(2).
    pub(crate) this_year: Rate,
    pub(crate) prior_year: Rate,
    /// The tax less withholding below which no penalty or interest is
    /// charged.
    pub(crate) threshold: USCents,
    /// The part of the required annual payment due by each date, and the
    /// part of the withholding deemed paid on each.
    pub(crate) shares: Vec<Rate>,
    pub(crate) dues: Vec<Date>,
    pub(crate) calendar: Calendar,
    /// The last day interest and penalty run, IN-152 line 7.
    pub(crate) stop: Date,
    /// The annual interest rates, one period for each calendar year.
    pub(crate) rates: RateSeries,
    /// The days IN-152 lines 9a and 9b divide by.
    pub(crate) day_basis: i64,
    /// The penalty for each month or part of a month, IN-152 line 13 and 32
    /// V.S.A. § 3202(b)(2).
    pub(crate) monthly: Rate,
}

/// A year's figures. Each rate period must be one calendar year, because
/// § 3108 sets one rate for each and interest looks a rate up by its year.
fn year_for(year: i64) -> Result<EstimatedYear, Error> {
    let y = match year {
        2026 => year_2026()?,
        _ => return Err(estimated::no_rules(&JURISDICTION, year)),
    };
    for p in y.rates.periods() {
        if p.from() != estimated::must_date(p.from().year(), Month::JANUARY, 1)
            || p.to() != estimated::must_date(p.from().year(), Month::DECEMBER, 31)
        {
            return Err(Error::Invalid(format!(
                "vermonttaxcalc: the rate from {} to {} is not one calendar year",
                p.from(),
                p.to()
            )));
        }
    }
    Ok(y)
}

/// Returns Vermont's estimated tax rules for a tax year, or
/// [`Error::NoRules`] for a year this build does not carry.
pub fn estimated_tax(year: i64) -> Result<Box<dyn EstimatedTax>, Error> {
    let y = year_for(year)?;
    Ok(Box::new(VermontEstimated {
        y,
        explanation: vermont_explanation()?,
    }))
}

/// The holiday calendar the year's due dates move by, for the tests. Not
/// part of the crate's API.
#[doc(hidden)]
pub fn calendar(year: i64) -> Result<Calendar, Error> {
    Ok(year_for(year)?.calendar)
}

struct VermontEstimated {
    y: EstimatedYear,
    explanation: Explanation,
}

/// What the three questions share.
#[derive(Default)]
struct Assessment {
    requirement: Requirement,
    exempt: bool,
    tax_known: bool,
    dues: Vec<Date>,
    /// Each installment: IN-152 line 4, or IN-152A line 6 for a filer who
    /// annualises.
    required: Vec<USCents>,
    notes: Notes,
}

/// Records, for each divergence, whether its readings give these facts
/// different figures or answers.
#[derive(Clone, Copy, Default)]
struct Affects {
    threshold: bool,
    measured: bool,
    prior_figure: bool,
    withholding: bool,
    later: bool,
    annualised: bool,
    moved: bool,
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

/// `a × num ÷ den`, rounded to the cent with halves up: IN-152A line 6.
/// `den` is above zero.
fn scale(a: USCents, num: USCents, den: USCents) -> USCents {
    let n = BigInt::from(a.0) * BigInt::from(num.0);
    Rounding::RoundToCent.div(&n, &BigInt::from(den.0))
}

/// One IN-152 column after the credits have been applied.
#[derive(Clone, Debug, Default)]
struct Entry {
    due: Date,
    needed_by: USCents,
    credited_by: USCents,
    shortfall: USCents,
    /// What is left of the shortfall.
    outstanding: USCents,
    /// The day a later credit paid the last of it.
    paid_on: Date,
    /// How much of the shortfall later credits paid.
    repaid: USCents,
    /// How many credits paid some of it.
    pieces: i64,
}

/// Pays the oldest underpayments among the first `n` columns with `amount`
/// on `on`, and returns what is left.
fn pay_old(out: &mut [Entry], n: usize, mut amount: USCents, on: Date) -> USCents {
    for e in out.iter_mut().take(n) {
        if amount.0 <= 0 {
            break;
        }
        if e.outstanding.0 == 0 {
            continue;
        }
        let paid = std::cmp::min(e.outstanding, amount);
        e.outstanding -= paid;
        e.repaid += paid;
        e.pieces += 1;
        if e.outstanding.0 == 0 {
            e.paid_on = on;
        }
        amount -= paid;
    }
    amount
}

/// Applies the credits to the installments as IN-152 lines 5a to 7 do,
/// with 32 V.S.A. § 5859(b)(2) where the form is silent.
///
/// A credit counts toward the first installment due on or after its date
/// (line 5b). Credits dated on a due date go to that installment first, and
/// what is left pays any earlier underpayment, oldest first, as
/// § 5859(b)(2)(B) says. A credit between due dates is not on an
/// installment date, so it pays any earlier underpayment first, on its own
/// date. What is left over is held toward the next installment (line 5c).
/// Credits after the last due date pay the oldest underpayment first.
///
/// This belongs beside [`estimated::apply`], which pays the oldest
/// underpayment first with every credit, including one dated on a due date.
fn ledger(dues: &[Date], required: &[USCents], credits: &[Payment]) -> Vec<Entry> {
    let mut out = vec![Entry::default(); dues.len()];
    let (mut pool, mut credited, mut needed) = (USCents(0), USCents(0), USCents(0));
    let mut next = 0;
    for (i, &due) in dues.iter().enumerate() {
        while next < credits.len() && credits[next].on().before(due) {
            credited += credits[next].amount();
            pool += pay_old(&mut out, i, credits[next].amount(), credits[next].on());
            next += 1;
        }
        let mut on_due = USCents(0);
        while next < credits.len() && credits[next].on() == due {
            credited += credits[next].amount();
            on_due += credits[next].amount();
            next += 1;
        }
        needed += required[i];
        let avail = pool + on_due;
        let covered = std::cmp::min(avail, required[i]);
        pool = pay_old(&mut out, i, avail - covered, due);
        out[i] = Entry {
            due,
            needed_by: needed,
            credited_by: credited,
            shortfall: required[i] - covered,
            outstanding: required[i] - covered,
            ..Entry::default()
        };
    }
    while next < credits.len() {
        pay_old(
            &mut out,
            dues.len(),
            credits[next].amount(),
            credits[next].on(),
        );
        next += 1;
    }
    out
}

/// The open question a later payment raises.
fn repaid_note(n: &mut Notes, entries: &[Entry]) {
    if entries.iter().any(|e| e.repaid.0 > 0) {
        n.ask(
            PositionKind::UnsettledRule,
            "",
            "With the worksheet silent on later payments, the figures follow the law. A due-date payment goes to its quarter first, others to the oldest shortfall.",
        );
    }
}

/// The installments from the IN-152 columns.
fn schedule_of(entries: &[Entry]) -> Result<Vec<Installment>, Error> {
    let mut out = Vec::with_capacity(entries.len());
    for (i, e) in entries.iter().enumerate() {
        out.push(
            estimated::new_installment(i as i64 + 1, e.due)
                .needed_by(e.needed_by)
                .credited_by(e.credited_by)
                .shortfall(e.shortfall)
                .build()?,
        );
    }
    Ok(out)
}

impl VermontEstimated {
    fn extras(&self, f: &Facts) -> Result<EstimatedFacts, Error> {
        match f.state() {
            None => Ok(EstimatedFacts::default()),
            Some(s) => match s.downcast_ref::<EstimatedFacts>() {
                Some(e) => Ok(e.clone()),
                None => Err(Error::Invalid(format!(
                    "vermonttaxcalc: the facts carry {}, not vermonttaxcalc.EstimatedFacts",
                    type_name(s)
                ))),
            },
        }
    }

    fn check(&self, f: &Facts) -> Result<(), Error> {
        if f.jurisdiction() != JURISDICTION {
            return Err(Error::Invalid(format!(
                "vermonttaxcalc: these are Vermont's rules, and the facts are for {}",
                f.jurisdiction()
            )));
        }
        if f.tax_year() != self.y.year {
            return Err(Error::Invalid(format!(
                "vermonttaxcalc: these are the {} rules, and the facts are for {}",
                self.y.year,
                f.tax_year()
            )));
        }
        Ok(())
    }

    /// Where the Department's documents and 32 V.S.A. part ways on a rule
    /// every Vermont computation uses. They are found in
    /// research/estimated-payments/vt.md, question 10 and its verification,
    /// and in the statute extracts in testdata/.
    fn divergences(&self, af: Affects) -> Result<Vec<Divergence>, Error> {
        let mut out = Vec::new();
        for b in [
            working::new_divergence("The $500 threshold")
                .form(
                    "No penalty or interest when this year's tax less withholding is less than $500. The IN-114 voucher instead charges those with more than $500 due.",
                    "IN-111 Instructions (2025), Line 31; IN-114 Instructions (2026), How Do I Determine My Estimated Tax Liability?; IN-114 (2026), Underpayment Interest and Penalties",
                )
                .statute(
                    "The individual rules set no dollar threshold. The only $500 minimum tax requirement is in a separate declaration schedule.",
                    "32 V.S.A. §§ 5851, 5852; 32 V.S.A. § 5857",
                )
                .uses(Reading::Form)
                .affects(af.threshold),
            working::new_divergence("What a shortfall is measured against")
                .form(
                    "The target is the lesser of 90% of this year's tax and 100% of last year's. Each quarter's shortfall is measured against a quarter of it.",
                    "IN-152 (2025), lines 1 to 6",
                )
                .statute(
                    "The shortfall is measured against a quarter of 90% of this year's tax. Interest alone is excused if payments reach that or a second figure, if lower. The second figure is last year's facts at this year's rates.",
                    "32 V.S.A. § 5859(b)(1)(A), (c)",
                )
                .uses(Reading::Form)
                .affects(af.measured),
            working::new_divergence("The prior-year figure")
                .form(
                    "100% of last year's Vermont tax minus credits.",
                    "IN-152 (2025), line 2; 32 V.S.A. § 5851(2)(B)",
                )
                .statute(
                    "For the interest exception, it's the tax at this year's rates. That tax uses the facts on last year's return, under last year's law.",
                    "32 V.S.A. § 5859(c)(1)",
                )
                .uses(Reading::Form)
                .affects(af.prior_figure),
            working::new_divergence("When withholding counts")
                .form(
                    "Withholding counts as paid 25% on each due date. The form has no line for the dates it was withheld.",
                    "IN-152 (2025), line 5a",
                )
                .statute(
                    "An equal part counts as paid on each due date. You can instead show the dates it was actually withheld.",
                    "32 V.S.A. § 5852(c)",
                )
                .uses(Reading::Form)
                .affects(af.withholding),
            working::new_divergence("How a later payment is applied")
                .form(
                    "A quarter's payments above its installment go to the next quarter. The whole shortfall is charged until it's paid in full. The form doesn't say how a later payment reaches an earlier quarter.",
                    "IN-152 (2025), lines 5c, 6 and 7",
                )
                .statute(
                    "Each portion of the shortfall is charged until that portion is paid. A payment on a due date goes first to that date's installment. Only the part beyond that date's installment goes to an earlier shortfall.",
                    "32 V.S.A. § 5859(b)(2)(A)(ii), (B)",
                )
                .uses(Reading::Form)
                .affects(af.later),
            working::new_divergence("The annualised installment")
                .form(
                    "Each installment starts from the federal Schedule AI installment. It's divided by the federal required annual payment and multiplied by Vermont's.",
                    "IN-152A (2025), lines 3 to 6",
                )
                .statute(
                    "The law says \"24 percent\" of the federal required payment. That's reduced by the share of adjusted gross income that isn't Vermont income.",
                    "32 V.S.A. § 5852(b)",
                )
                .uses(Reading::Form)
                .affects(af.annualised),
            working::new_divergence("A due date on a weekend or holiday")
                .form(
                    "A weekend or federal or state holiday moves a due date to the next business day.",
                    "Vermont Department of Taxes, Timely Filing and Payments and State Holiday Closures pages",
                )
                .statute(
                    "An act due on a Sunday is valid on the following Monday. Saturdays and holidays aren't mentioned.",
                    "32 V.S.A. § 3004",
                )
                .uses(Reading::Form)
                .affects(af.moved),
        ] {
            out.push(b.build()?);
        }
        Ok(out)
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
        let mut af = Affects::default();
        for (i, due) in a.dues.iter().enumerate() {
            if *due != y.dues[i]
                && (y.dues[i].weekday() != Weekday::SUNDAY || *due != y.dues[i].add_days(1))
            {
                af.moved = true;
            }
        }
        let tax_opt = f.this_year().tax();
        let tax_known = tax_opt.is_some();
        let tax = tax_opt.unwrap_or_default();
        a.tax_known = tax_known;
        let withheld = f.withheld_total();
        // IN-152 line 1: 90% of this year's tax minus credits.
        let this_amount = y.this_year.of(tax);
        let this_year = if tax_known {
            estimated::new_target_option(Basis::ThisYearTax, y.this_year, this_amount).build()?
        } else {
            estimated::new_target_option(Basis::ThisYearTax, y.this_year, USCents(0))
                .unavailable(estimated::NEEDS_THIS_YEAR_TAX)
                .build()?
        };
        if tax_known {
            a.notes.rule(
                "32 V.S.A. § 5851(2)(A); IN-152 (2025), line 1",
                format!(
                    "{} of the {tax} tax minus credits is {this_amount}",
                    y.this_year
                ),
            );
        }
        // IN-152 line 2: 100% of last year's tax minus credits, from a filed
        // return covering twelve months.
        let prior_opt = f.prior_year();
        let has_prior = prior_opt.is_some();
        let prior = prior_opt.unwrap_or_default();
        let unavailable = |why: String| -> TargetOptionBuilder {
            let mut b = estimated::new_target_option(Basis::PriorYearTax, y.prior_year, USCents(0));
            b.unavailable(why);
            b
        };
        let prior_option = if !has_prior {
            unavailable(format!("Needs your {last} Vermont return"))
        } else if !prior.filed() {
            unavailable(format!("You didn't file a {last} Vermont return"))
        } else if prior.months() != 12 {
            unavailable(format!(
                "Your {last} Vermont return covered {} months, not 12",
                prior.months()
            ))
        } else {
            let prior_tax = prior.figures().and_then(|pf| pf.tax()).unwrap_or_default();
            let amount = y.prior_year.of(prior_tax);
            a.notes.rule(
                "32 V.S.A. § 5851(2)(B); IN-152 (2025), line 2",
                format!(
                    "{} of the {prior_tax} {last} tax minus credits is {amount}",
                    y.prior_year
                ),
            );
            estimated::new_target_option(Basis::PriorYearTax, y.prior_year, amount)
        };
        let prior_opt = prior_option.build()?;
        let mut rb = estimated::new_requirement();
        rb.option(this_year.clone()).option(prior_opt.clone());
        let (mut chosen, mut target) = (Basis::ThisYearTax, this_year.amount());
        if prior_opt.available() && (!tax_known || prior_opt.amount() < target) {
            (chosen, target) = (Basis::PriorYearTax, prior_opt.amount());
        }
        let prior_farmer_answer = extras.prior_year_farmer_or_fisher();
        let prior_farmer = prior_farmer_answer == Some(true);
        let owed = tax - withheld;
        if f.farmer_or_fisher() || prior_farmer {
            a.exempt = true;
            let which = if f.farmer_or_fisher() {
                y.year.to_string()
            } else {
                last.to_string()
            };
            let ex =
                estimated::new_exception("Two-thirds of income from farming or fishing").build()?;
            rb.required(
                false,
                format!(
                    "At least two-thirds of your {which} income was from farming or fishing. So Vermont doesn't require estimated payments."
                ),
            )
            .exempt(ex);
            a.notes.rule(
                "32 V.S.A. § 5852(a); IN-114 Instructions (2026); IN-111 Instructions (2025), Farmer/Fisherman",
                format!(
                    "two-thirds or more of the {which} gross income was from farming or fishing, so no estimated payments are required"
                ),
            );
        } else if !tax_known && !prior_opt.available() {
            let mut why = format!(
                "Your target is the smaller of two figures. They're {} of your {} Vermont tax and {} of your {last} tax.",
                y.this_year, y.year, y.prior_year
            );
            if has_prior {
                why = format!(
                    "Your target is {} of your {} Vermont tax. Last year's option isn't open to you.",
                    y.this_year, y.year
                );
            }
            rb.required(false, why).no_target();
        } else if !tax_known && withheld >= target {
            rb.required(
                false,
                format!(
                    "Your withholding of {} already reaches your {last} tax of {}. No estimated payments are needed, whatever your {} tax is.",
                    withheld.whole(),
                    target.whole(),
                    y.year
                ),
            )
            .choose(chosen);
        } else if !tax_known {
            rb.required(
                true,
                format!(
                    "Your target is your {last} tax, {}. That keeps you clear of the penalty whatever your {} tax turns out to be. After {} of withholding, that's {} of estimated payments. Pay a quarter by each date.",
                    target.whole(),
                    y.year,
                    withheld.whole(),
                    (target - withheld).whole()
                ),
            )
            .choose(chosen);
        } else if owed < y.threshold {
            a.exempt = true;
            let ex = estimated::exception_below("Tax less withholding", owed, y.threshold)?;
            let mut why = format!(
                "Your Vermont tax of {} less {} withheld is under {}. Vermont charges no penalty or interest, and no estimated payments are needed.",
                tax.whole(),
                withheld.whole(),
                y.threshold.whole()
            );
            if owed.0 < 0 {
                why = format!(
                    "Your Vermont tax of {} less {} withheld {}. Vermont charges no penalty or interest, and no estimated payments are needed.",
                    tax.whole(),
                    withheld.whole(),
                    estimated::leaves(owed)
                );
            }
            rb.required(false, why).exempt(ex);
            a.notes.rule(
                "IN-111 Instructions (2025), Line 31; IN-114 Instructions (2026)",
                format!(
                    "{tax} of tax less {withheld} of withholding is {owed}, less than {}, so there is no penalty or interest",
                    y.threshold
                ),
            );
        } else if withheld >= target {
            rb.required(
                false,
                format!(
                    "Your Vermont withholding of {} covers the {} needed for the year. No estimated payments are needed.",
                    withheld.whole(),
                    target.whole()
                ),
            )
            .choose(chosen);
        } else {
            rb.required(
                true,
                format!(
                    "Your Vermont tax of {} less {} withheld leaves {} due. Withholding falls short of your {} target. You need estimated payments of {}, a quarter by each date.",
                    tax.whole(),
                    withheld.whole(),
                    owed.whole(),
                    target.whole(),
                    (target - withheld).whole()
                ),
            )
            .choose(chosen);
        }
        a.requirement = rb.build()?;
        // An exception sets the target, so last year's return can't change
        // it.
        if !has_prior && !a.exempt {
            a.notes.ask(
                PositionKind::PriorYearReturn,
                format!("Did you file a full-year {last} Vermont return, and what was its tax?"),
                format!(
                    "With it, {} of last year's tax could set a lower target.",
                    y.prior_year
                ),
            );
        }
        if !tax_known && !a.exempt {
            let mut effect = format!(
                "Without it, your target is your {last} tax. The penalty shown is the most it can be."
            );
            if !prior_opt.available() {
                effect = format!(
                    "With it, {} of this year's tax can set your target. A balance under {} would need no payments.",
                    y.this_year,
                    y.threshold.whole()
                );
            }
            a.notes.ask(
                PositionKind::ThisYearTax,
                format!("What do you expect your {} Vermont tax to be?", y.year),
                effect,
            );
        }
        if tax_known && !a.exempt && owed == y.threshold {
            a.notes.ask(
                PositionKind::UnsettledRule,
                "",
                format!(
                    "At exactly {} due, the instructions excuse only less than {}. The voucher charges only more, and the figures follow the instructions.",
                    y.threshold.whole(),
                    y.threshold.whole()
                ),
            );
        }
        if !a.exempt && prior_farmer_answer.is_none() {
            a.notes.ask(
                PositionKind::JurisdictionFact,
                format!("Did two-thirds of your {last} gross income come from farming or fishing?"),
                "If so, Vermont doesn't require estimated payments.",
            );
        }
        // What each divergence turns on in these facts.
        af.threshold =
            tax_known && !f.farmer_or_fisher() && !prior_farmer && owed.0 > 0 && owed < y.threshold;
        af.measured = !a.exempt && a.requirement.target_known() && chosen == Basis::PriorYearTax;
        af.prior_figure = af.measured;
        af.withholding = !a.exempt && f.elections().withholding_as_withheld() && withheld.0 > 0;
        if a.exempt {
            a.required = vec![USCents(0); a.dues.len()];
            return self.with_divergences(a, af);
        }
        let cum = estimated::cumulative_shares(target, &y.shares)?;
        a.required = estimated::increments(&cum);
        a.notes.rule(
            "32 V.S.A. § 5852(a); IN-152 (2025), line 4; IN-114 Instructions (2026)",
            format!(
                "{} of {target}, {}, is due on each of {}, {}, {} and {}",
                y.shares[0],
                a.required[0],
                a.dues[0].long(),
                a.dues[1].long(),
                a.dues[2].long(),
                a.dues[3].long()
            ),
        );
        if f.elections().annualised() {
            match extras.federal_annualised() {
                None => {
                    a.notes.ask(
                        PositionKind::JurisdictionFact,
                        "What are your federal Schedule AI installments and required annual payment?",
                        "Without them, the regular quarters apply. Vermont scales its annualised installments from the Form 2210 ones.",
                    );
                }
                Some((fed_required, fed_installments)) => {
                    af.annualised = true;
                    for (i, fi) in fed_installments.iter().enumerate() {
                        a.required[i] = scale(*fi, target, fed_required);
                    }
                    a.notes.rule(
                        "IN-152A (2025), lines 3 to 6; IN-114 Instructions (2026)",
                        format!(
                            "each federal Schedule AI installment divided by the federal {fed_required} and multiplied by the Vermont {target} gives {}, {}, {} and {}",
                            a.required[0], a.required[1], a.required[2], a.required[3]
                        ),
                    );
                }
            }
        }
        if !f.requirement_first_met().is_zero() {
            a.notes.rule(
                "IN-152 (2025), line 4",
                "IN-152 has no rule for a requirement first met during the year; every installment is due from the first date",
            );
        }
        if a.requirement.target_known() {
            let (cs, _) = self.credits(f, &a)?;
            for e in ledger(&a.dues, &a.required, &cs) {
                if e.pieces > 1 || (e.pieces == 1 && e.outstanding.0 > 0) {
                    af.later = true;
                }
            }
        }
        self.with_divergences(a, af)
    }

    /// Adds the divergences to the assessment's notes.
    fn with_divergences(&self, mut a: Assessment, af: Affects) -> Result<Assessment, Error> {
        let ds = self.divergences(af)?;
        a.notes.diverges(&ds);
        Ok(a)
    }

    /// What IN-152 lines 5a and 5b count, in date order.
    fn credits(&self, f: &Facts, a: &Assessment) -> Result<(Vec<Payment>, Notes), Error> {
        let cs = estimated::credits(f, &a.dues, &self.y.shares)?;
        let mut notes = Notes::default();
        notes.rule(
            "IN-152 (2025), lines 5a to 5c",
            format!(
                "the {} withheld is treated as paid {} on each due date, whenever it was withheld; a payment counts toward the next due date on or after it, and a quarter's payments above its installment go to the next quarter",
                f.withheld_total(),
                self.y.shares[0]
            ),
        );
        if f.elections().withholding_as_withheld() {
            notes.rule(
                "IN-152 (2025), line 5a; 32 V.S.A. § 5852(c)",
                "IN-152 has no line for the dates withholding was taken, so it is spread evenly; the statute allows the actual dates, which the Divergences report",
            );
        }
        Ok((cs, notes))
    }

    /// IN-152 lines 8a to 9b on one shortfall: the days after the due date
    /// up to the full payment or January 1 of the next year at the due
    /// year's rate, and the days after that January 1 at the next year's.
    /// Each part is the shortfall × days ÷ the day basis × the rate, rounded
    /// to the cent.
    ///
    /// The returned date, unrated, is the first day no published rate
    /// covers, or the zero Date.
    fn interest(
        &self,
        shortfall: USCents,
        due: Date,
        paid: Date,
    ) -> Result<(Vec<PenaltyPart>, Date), Error> {
        let y = &self.y;
        let new_year = estimated::must_date(due.year() + 1, Month::JANUARY, 1);
        let mut spans = vec![(due, paid, due.year())];
        if paid.after(new_year) {
            spans = vec![
                (due, new_year, due.year()),
                (new_year, paid, due.year() + 1),
            ];
        }
        let mut parts = Vec::new();
        for (from, to, year) in spans {
            let Some(period) = y.rates.on(estimated::must_date(year, Month::JANUARY, 1)) else {
                return Ok((parts, from.add_days(1)));
            };
            let days = to.days_since(from);
            let n = BigInt::from(shortfall.0) * BigInt::from(days) * BigInt::from(period.rate().0);
            let charge = Rounding::RoundToCent.div(&n, &BigInt::from(10_000 * y.day_basis));
            parts.push(
                estimated::new_penalty_part(PartKind::Interest, shortfall, period.rate())
                    .span(from, to)
                    .days(days)
                    .charge(charge)
                    .source(period.source())
                    .build()?,
            );
        }
        Ok((parts, Date::default()))
    }
}

impl EstimatedTax for VermontEstimated {
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
                    "with no {} Vermont return to measure against, there is no target to schedule",
                    self.y.year - 1
                ),
            ));
        }
        let (cs, mut notes) = self.credits(f, &a)?;
        let entries = ledger(&a.dues, &a.required, &cs);
        let out = schedule_of(&entries)?;
        repaid_note(&mut notes, &entries);
        Ok(working::new_result(out)
            .with(&a.notes)
            .with(&notes)
            .build()?)
    }

    fn penalty(&self, f: &Facts, as_of: Date) -> Result<working::Result<Penalty>, Error> {
        if as_of.is_zero() {
            return Err(Error::Invalid(
                "vermonttaxcalc: a penalty needs the date to figure it to".into(),
            ));
        }
        let a = self.assess(f)?;
        if !a.requirement.target_known() {
            return Err(Error::with_detail(
                Error::NeedsThisYearTax,
                format!(
                    "with no {} Vermont return to measure against, there is no target to figure a penalty from",
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
        let waiver = f.positions().waiver().unwrap_or_default();
        let (cs, mut notes) = self.credits(f, &a)?;
        let entries = ledger(&a.dues, &a.required, &cs);
        let sched = schedule_of(&entries)?;
        if a.exempt {
            let p = estimated::new_penalty(stop)
                .waiver(waiver)
                .because(a.requirement.clone(), &sched)
                .build()?;
            return Ok(working::new_result(p).with(&a.notes).build()?);
        }
        let mut pb = estimated::new_penalty(stop);
        pb.waiver(waiver).because(a.requirement.clone(), &sched);
        let mut unrated = Date::default();
        for (i, e) in entries.iter().enumerate() {
            if e.shortfall.0 == 0 {
                continue;
            }
            // IN-152 line 7: the date full payment was made, or April 15.
            let mut paid = stop;
            if e.outstanding.0 == 0 && e.paid_on.before(stop) {
                paid = e.paid_on;
            }
            if !paid.after(e.due) {
                continue;
            }
            let mut lb = estimated::new_penalty_line(i as i64 + 1, e.due, e.shortfall);
            lb.days(paid.days_since(e.due));
            let (parts, gap) = self.interest(e.shortfall, e.due, paid)?;
            if !gap.is_zero() && (unrated.is_zero() || gap.before(unrated)) {
                unrated = gap;
            }
            for p in parts {
                lb.part(p);
            }
            let monthly = estimated::monthly(
                e.shortfall,
                e.due,
                paid,
                y.monthly,
                "32 V.S.A. § 3202(b)(2); IN-152 (2025), lines 12 and 13",
            )?;
            lb.part(monthly);
            pb.line(lb.build()?);
        }
        if !a.tax_known {
            pb.upper_bound();
        }
        let mut p = pb.build()?;
        if p.total().0 == 0 && p.upper_bound() {
            // No installment was short of the prior-year figure, which the
            // lesser can only lower, so the zero is exact.
            p = estimated::new_penalty(stop)
                .waiver(waiver)
                .because(a.requirement.clone(), &sched)
                .build()?;
        }
        notes.rule(
            "32 V.S.A. §§ 3108, 3202(b)(2), 5859(b); IN-152 (2025), lines 6 to 15",
            format!(
                "each quarter's whole underpayment is charged interest by the day, over {} days a year at each calendar year's rate, and {} for each month or part of a month, from its due date until it is paid in full or {}; the total to {} is {}",
                y.day_basis,
                y.monthly,
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
                    "Interest for {} isn't charged until its rate is published. The 1% a month penalty is charged in full.",
                    unrated.year()
                ),
            );
        }
        repaid_note(&mut notes, &entries);
        if let Some(k) = f.positions().waiver()
            && k != WaiverKind::None
        {
            notes.rule(
                "32 V.S.A. §§ 3202, 5851 to 5859; IN-152 (2025)",
                "Vermont's estimated tax statutes and worksheets provide no casualty, retirement or disability waiver; the penalty is shown in full",
            );
            if p.total().0 > 0 {
                let o = estimated::waiver_refused(
                    "Vermont's law has no waiver of the estimated tax penalty or interest. That holds for a casualty, disaster, retirement or disability.",
                );
                p = p.to_builder().waiver_outcome(o).build()?;
            }
        }
        Ok(working::new_result(p).with(&a.notes).with(&notes).build()?)
    }

    fn explanation(&self) -> Explanation {
        self.explanation.clone()
    }
}

/// Written by hand and checked against research/estimated-payments/vt.md.
fn vermont_explanation() -> Result<Explanation, Error> {
    estimated::new_explanation(
        "Vermont expects estimated payments when withholding will not cover your tax. If you fall short, it charges interest on each quarter's shortfall. It also charges a separate penalty of 1% a month. Neither applies if this year's tax less withholding is under $500.",
    )
    .paragraph("You are clear if withholding and estimated payments reach a target. The target is the smaller of 90% of this year's tax or 100% of last year's. Last year's figure counts only if you filed a full-year return. There is no 110% tier for high incomes.")
    .paragraph("The payments are a quarter each. They're due April 15, June 15, September 15 and the next January 15. Withholding counts as paid evenly across the four dates, whenever it was taken. If you annualise your federal estimates, you can annualise Vermont's too. They follow the federal installments in proportion.")
    .paragraph("Each quarter's shortfall is charged two ways. Both run until it is paid in full or until April 15 of the next year. Interest runs by the day at a rate set once each calendar year, 7.75% for 2026. The penalty is 1% of the shortfall for each month or part of a month. So a payment one day late costs a full 1%.")
    .paragraph("The Child Care Contribution applies to self-employed people working in Vermont. They should include it in their payments.")
    .versus_federal("Vermont differs from federal in six ways that change a number. Its threshold is $500, not $1,000. There's no 110% tier. A 1% monthly penalty comes on top of the interest. The whole shortfall is charged until it is paid in full. Withholding is always spread evenly. There's no casualty, retirement or disability waiver.")
    .source("32 V.S.A. §§ 3108, 3202(b)(2), 5851, 5852, 5859")
    .source("IN-114, Vermont Individual Income Estimated Tax Payment Voucher, and its instructions, 2026")
    .source("IN-152 and IN-152A, Underpayment of Estimated Individual Income Tax, 2025")
    .source("Vermont Department of Taxes, 2026 Interest Rate memo, November 4, 2025")
    .build()
}
