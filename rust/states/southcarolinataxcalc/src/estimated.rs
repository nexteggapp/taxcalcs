//! South Carolina's estimated tax rules, S.C. Code §§ 12-6-3910 and
//! 12-54-55, as the shared [`EstimatedTax`] interface.
//!
//! § 12-54-55(1) computes the penalty "in the same manner" as 26 U.S.C.
//! § 6654, with $100 in place of the $1,000 small-amount figure and no
//! penalty on an underpayment caused by personal service income earned in
//! another state that withheld its own tax. § 12-6-3910(A) fixes the four due
//! dates. The rate is set under § 12-54-25(D) in the same manner and at the
//! same time as the federal underpayment rate. This crate depends on no other
//! jurisdiction, so the § 6654 arithmetic is written here again.
//!
//! The figure follows the department's forms: the 2026 SC1040ES and the 2025
//! SC2210, the latest penalty form. Where they part from each other or from
//! the statute, the result's Divergences say so. Act 110 of 2026 changed how
//! the 2026 tax is computed after the SC1040ES was printed; it did not change
//! the estimated tax rules (research/estimated-payments/sc.md).

use std::any::Any;

use estimated::{
    Basis, Calendar, Date, Error, EstimatedTax, Explanation, Facts, Figures, Installment,
    LedgerEntry, Penalty, RateSeries, Requirement, TargetOptionBuilder,
};
use money::{Rate, USCents};
use working::{Divergence, Notes, PositionKind, Reading};

/// South Carolina's postal code.
const JURISDICTION: estimated::Jurisdiction = estimated::SOUTH_CAROLINA;

/// The facts South Carolina's rules read that no other jurisdiction built
/// here does. Carry them in [`estimated::FactsBuilder::state`].
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct EstimatedFacts {
    prior_farmer: bool,
    prior_farmer_answered: bool,
    other_state: bool,
    other_state_answered: bool,
    contractor: bool,
    contractor_answered: bool,
}

impl EstimatedFacts {
    /// Whether at least two-thirds of last year's gross income was from
    /// farming and fishing, and whether the caller said: `(farmer,
    /// answered)`. The SC2210 puts a filer under its farmer rules when
    /// either year's income was, and [`Facts`] carries only this year's
    /// share.
    pub fn prior_year_farmer_or_fisher(&self) -> (bool, bool) {
        (self.prior_farmer, self.prior_farmer_answered)
    }

    /// Whether the underpayment comes from personal service income earned in
    /// another state, on which that state's tax was withheld, and whether the
    /// caller said: `(yes, answered)`. § 12-54-55(1) charges no penalty on
    /// it. Answer yes only when the whole underpayment comes from that
    /// income; a share of it is not built.
    pub fn underpayment_from_other_state_income(&self) -> (bool, bool) {
        (self.other_state, self.other_state_answered)
    }

    /// Whether the filer is a nonresident working in South Carolina under
    /// contracts over $10,000 with 2% withheld from each payment, and whether
    /// the caller said: `(yes, answered)`. The SC1040ES excuses such a filer
    /// from making a declaration.
    pub fn nonresident_contractor(&self) -> (bool, bool) {
        (self.contractor, self.contractor_answered)
    }
}

/// Builds [`EstimatedFacts`].
#[derive(Clone, Debug, Default)]
pub struct EstimatedFactsBuilder {
    e: EstimatedFacts,
}

/// Starts South Carolina's extras.
pub fn new_estimated_facts() -> EstimatedFactsBuilder {
    EstimatedFactsBuilder::default()
}

impl EstimatedFactsBuilder {
    /// Answers whether two-thirds of last year's gross income was from
    /// farming and fishing.
    pub fn prior_year_farmer_or_fisher(&mut self, farmer: bool) -> &mut Self {
        (self.e.prior_farmer, self.e.prior_farmer_answered) = (farmer, true);
        self
    }

    /// Answers whether the whole underpayment comes from personal service
    /// income earned in another state that withheld its tax.
    pub fn underpayment_from_other_state_income(&mut self, yes: bool) -> &mut Self {
        (self.e.other_state, self.e.other_state_answered) = (yes, true);
        self
    }

    /// Answers whether the filer is a nonresident contractor with 2%
    /// withheld from each contract payment.
    pub fn nonresident_contractor(&mut self, yes: bool) -> &mut Self {
        (self.e.contractor, self.e.contractor_answered) = (yes, true);
        self
    }

    /// Has nothing to refuse: every fact is a yes or no.
    pub fn build(&self) -> Result<EstimatedFacts, Error> {
        Ok(self.e)
    }
}

/// One tax year's published figures.
pub(crate) struct EstimatedYear {
    pub(crate) year: i64,

    /// The small amount of § 12-54-55(1): no penalty when tax less
    /// withholding is under it.
    pub(crate) threshold: USCents,

    /// The 90%, 100% and 110% of § 6654(d)(1)(B) and (C).
    pub(crate) this_year_percent: Rate,
    pub(crate) prior_year_percent: Rate,
    pub(crate) high_income_percent: Rate,

    /// Last year's AGI above which the prior-year test is
    /// `high_income_percent`; `prior_year_step_separate` is the step on a
    /// married separate return.
    pub(crate) prior_year_step: USCents,
    pub(crate) prior_year_step_separate: USCents,

    /// Each installment's part of the required annual payment, and the part
    /// of the year's withholding deemed paid on each due date.
    pub(crate) shares: Vec<Rate>,

    /// § 6654(d)(2)(C)(ii), Schedule AI line 20.
    pub(crate) annualised_percentages: Vec<Rate>,

    pub(crate) dues: Vec<Date>,
    pub(crate) calendar: Calendar,

    /// The first days of the SC1040ES's later groups: a requirement first met
    /// on or after `late_groups[i]` has, on that form, its first payment at
    /// installment i+2.
    pub(crate) late_groups: Vec<Date>,

    /// § 6654(h)'s January 31, before moving for a weekend.
    pub(crate) file_and_pay_by: Date,

    /// The last day the penalty runs, April 15 of the next year.
    pub(crate) stop: Date,

    pub(crate) period_ends: Vec<Date>,

    /// The rate the SC2210 prints in every rate period.
    pub(crate) form_rate: Rate,

    /// The department's published rates, one period for each of the
    /// SC2210's rate periods, as far as they have been published.
    pub(crate) rates: RateSeries,
}

fn year(year: i64) -> Result<EstimatedYear, Error> {
    match year {
        2026 => crate::estimated_y2026::year_2026(),
        _ => Err(estimated::no_rules(&JURISDICTION, year)),
    }
}

/// Returns South Carolina's estimated tax rules for a tax year, or
/// [`Error::NoRules`] for a year this build does not carry.
pub fn estimated_tax(year_: i64) -> Result<Box<dyn EstimatedTax>, Error> {
    Ok(Box::new(SouthCarolinaEstimated { y: year(year_)? }))
}

/// Returns the legal holidays that move a due date for a tax year's
/// estimated tax, as SC Revenue Procedure #23-1 lists them, or
/// [`Error::NoRules`] for a year this build does not carry.
pub fn estimated_calendar(year_: i64) -> Result<Calendar, Error> {
    Ok(year(year_)?.calendar)
}

struct SouthCarolinaEstimated {
    y: EstimatedYear,
}

/// What the three questions share.
#[derive(Default)]
struct Assessment {
    requirement: Requirement,
    exempt: bool,
    tax_known: bool,
    dues: Vec<Date>,
    installments: Vec<USCents>,

    /// The January installment, for a return filed and paid by the moved
    /// January 31.
    excused: bool,

    /// An underpayment from income earned in another state, which bears no
    /// penalty.
    other_state: bool,

    notes: Notes,
}

/// For each divergence, whether its readings give these facts different
/// figures or answers.
#[derive(Default)]
struct Affects {
    act110: bool,
    rate: bool,
    exact_step: bool,
    separate_step: bool,
    no_liability: bool,
    january: bool,
    earlier: bool,
    other_state: bool,
    late_start: bool,
}

/// A question held back until it is known whether an exception sets the
/// target, as Go's deferred `targetAsks`.
type Ask = (PositionKind, String, String);

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

impl SouthCarolinaEstimated {
    fn extras(&self, f: &Facts) -> Result<EstimatedFacts, Error> {
        match f.state() {
            None => Ok(EstimatedFacts::default()),
            Some(s) => match s.downcast_ref::<EstimatedFacts>() {
                Some(e) => Ok(*e),
                None => Err(Error::Invalid(format!(
                    "southcarolinataxcalc: the facts carry {}, not southcarolinataxcalc.EstimatedFacts",
                    type_name(s)
                ))),
            },
        }
    }

    fn check(&self, f: &Facts, e: &EstimatedFacts) -> Result<(), Error> {
        let (farmer_last_year, _) = e.prior_year_farmer_or_fisher();
        if f.jurisdiction() != JURISDICTION {
            return Err(Error::Invalid(format!(
                "southcarolinataxcalc: these are South Carolina's rules, and the facts are for {}",
                f.jurisdiction()
            )));
        }
        if f.tax_year() != self.y.year {
            return Err(Error::Invalid(format!(
                "southcarolinataxcalc: these are the {} rules, and the facts are for {}",
                self.y.year,
                f.tax_year()
            )));
        }
        if f.farmer_or_fisher() || farmer_last_year {
            return Err(Error::with_detail(
                Error::Unsupported,
                "a farmer or fisherman with two-thirds of gross income from farming and fishing has the SC2210's farmer rules and the federal 2210-F, which are not built",
            ));
        }
        if f.elections().annualised() {
            for (i, p) in f.periods().iter().enumerate() {
                let want = self.y.period_ends.get(i).copied().unwrap_or_default();
                if p.end() != want {
                    return Err(Error::Invalid(format!(
                        "southcarolinataxcalc: Schedule AI period {} ends on {}, not {}",
                        i + 1,
                        want,
                        p.end()
                    )));
                }
            }
        }
        Ok(())
    }

    /// Where the department's forms part from each other or from §§
    /// 12-6-3910 and 12-54-55 and the § 6654 they adopt. They are found in
    /// research/estimated-payments/sc.md and checked against the extracts in
    /// the Go package's testdata/.
    fn divergences(&self, af: &Affects) -> Result<Vec<Divergence>, Error> {
        let y = &self.y;
        let mut out = Vec::new();
        for b in [
            working::new_divergence("The 2026 worksheet predates Act 110")
                .form("The 2026 worksheet figures the tax from federal taxable income at 0%, 3% and 6%. It was printed before Act 110 and is out of date. The act didn't change its thresholds, percentages and dates, which still apply.",
                    "SC1040ES (Rev. 10/13/25), 2026 Estimated Tax Worksheet lines 1 to 4 and Tax Computation Schedule")
                .statute("Act 110 of 2026 was approved March 30, 2026. It starts from federal AGI. It taxes 2026 income at 1.99%, or 5.21% less $966 from $30,000.",
                    "Act No. 110 of 2026 (H.4216), SECTION 8; SC Information Letter #26-20")
                .uses(Reading::Statute)
                .affects(af.act110)
                .clone(),
            working::new_divergence("The interest rate")
                .form(format!("The 2025 SC2210 prints {} (.07) in all four rate periods. That includes Jan 1 to Apr 15, 2026, when the published rate from Apr 1 was 6%.", y.form_rate),
                    "SC2210 (2025), Part III lines 19 to 25; SC Information Letter #26-19")
                .statute("The rate is set in the same manner as the federal underpayment rate. It is set at the same time as the federal underpayment rate. So each day carries the rate in effect that day. For 2026 that is 6% to June 30 and 7% after.",
                    "S.C. Code §§ 12-54-25(D), 12-54-55(1); 26 U.S.C. § 6654(a)")
                .uses(Reading::Statute)
                .affects(af.rate)
                .clone(),
            working::new_divergence("The 110% test at exactly $150,000")
                .form("The 2025 SC1040 instructions raise the prior-year test to 110%. They do so when AGI is \"$150,000 or more\". The SC1040ES and SC2210 say \"more than $150,000\".",
                    "SC1040 instructions (2025), line 33; SC1040ES (2026), Penalty for failure to file and pay; SC2210 (2025), Part I line 7")
                .statute("110% applies when last year's AGI exceeds $150,000.",
                    "S.C. Code § 12-54-55(1); 26 U.S.C. § 6654(d)(1)(C)(i)")
                .uses(Reading::Form)
                .affects(af.exact_step)
                .clone(),
            working::new_divergence("The married filing separately step")
                .form("The SC1040ES states only the $150,000 step. The SC2210 lowers it to $75,000 for a married separate return.",
                    "SC1040ES (2026), Penalty for failure to file and pay; SC2210 (2025), Part I line 7")
                .statute("On a separate return for the year the installment is for, the step is $75,000.",
                    "S.C. Code § 12-54-55(1); 26 U.S.C. § 6654(d)(1)(C)(ii)")
                .uses(Reading::Form)
                .affects(af.separate_step)
                .clone(),
            working::new_divergence("No tax liability last year")
                .form("No penalty after a full 12-month year with $0 liability. There is no condition of U.S. citizenship or residence.",
                    "SC2210 (2025), Exceptions to the penalty; SC1040ES (2026), Exceptions to filing")
                .statute("You must also have been a U.S. citizen or resident all of last year.",
                    "S.C. Code § 12-54-55(1); 26 U.S.C. § 6654(e)(2)(C)")
                .uses(Reading::Form)
                .affects(af.no_liability)
                .clone(),
            working::new_divergence("The January 31 date")
                .form("The 2025 SC2210 prints January 31 without moving it for a weekend. The 2026 SC1040ES prints February 1: January 31, 2027, moved past a Sunday.",
                    "SC2210 (2025), instructions; SC1040ES (2026), Payment due dates")
                .statute("A return filed and paid in full by January 31 excuses the fourth installment. A deadline on a weekend or legal holiday moves to the next business day.",
                    "26 U.S.C. § 6654(h); S.C. Code § 12-60-50(A), quoted in SC Revenue Procedure #23-1")
                .uses(Reading::Form)
                .affects(af.january)
                .clone(),
            working::new_divergence("Earlier installments after a return filed by January 31")
                .form("The earlier installments' periods of underpayment end on January 15. That applies when the return is filed and the tax paid by January 31.",
                    "SC2210 (2025), instructions, Period of underpayment")
                .statute("Each period runs until the underpayment is paid. That is the day the return's payment is made, or April 15 if earlier.",
                    "S.C. Code § 12-54-55(1); 26 U.S.C. § 6654(b)(2)")
                .uses(Reading::Form)
                .affects(af.earlier)
                .clone(),
            working::new_divergence("Income earned in another state")
                .form("The SC2210 lists a reason to request a waiver. It is an underpayment from personal service income earned in another state. That state withheld its tax. The 2026 SC1040ES says no penalty is charged on it.",
                    "SC2210 (2025), Waiver of penalty item 3; SC1040ES (2026), Penalty for failure to file and pay")
                .statute("The law says no interest or penalty is due on such an underpayment.",
                    "S.C. Code § 12-54-55(1)")
                .uses(Reading::Form)
                .affects(af.other_state)
                .clone(),
            working::new_divergence("A requirement first met after April 1")
                .form("For a late requirement, the SC1040ES starts payments June 15, Sep 15 or Jan 15. By that date you pay half, three-quarters or all of your estimated tax. The SC2210 requires 25% of the required annual payment by every date.",
                    "SC1040ES (2026), Payment due dates and worksheet line 11; SC2210 (2025), Part III line 9")
                .statute("There are four required installments, each 25% of the required annual payment. There is no rule for a requirement that arises late in the year.",
                    "S.C. Code §§ 12-6-3910(A)(3)(a), 12-54-55(1); 26 U.S.C. § 6654(c), (d)(1)(A)")
                .uses(Reading::Form)
                .affects(af.late_start)
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
        let mut af = Affects::default();

        let separate = f.filing_status() == filing::Status::MarriedFilingSeparately;
        let step = if separate {
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

        if tax_known {
            a.notes.rule(
                "Act No. 110 of 2026, SECTION 8; SC Information Letter #26-20",
                format!(
                    "this year's tax of {tax} is taken as figured under Act 110, not the SC1040ES worksheet (see Divergences)"
                ),
            );
        }

        // 90% of this year's tax: SC2210 Part I line 4.
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
                "S.C. Code § 12-54-55(1); 26 U.S.C. § 6654(d)(1)(B)(i); SC2210 (2025), Part I line 4",
                format!(
                    "{} of this year's South Carolina tax of {tax} is {}",
                    y.this_year_percent,
                    this_year.amount()
                ),
            );
        }

        // Last year's tax: SC2210 Part I line 7.
        let unavailable = |why: String| -> TargetOptionBuilder {
            let mut b =
                estimated::new_target_option(Basis::PriorYearTax, y.prior_year_percent, USCents(0));
            b.unavailable(why);
            b
        };
        let mut prior_tax = USCents(0);
        // Questions whose answer can change the target, asked only when no
        // exception sets it.
        let mut target_asks: Vec<Ask> = Vec::new();
        let prior_option = if !has_prior {
            target_asks.push((
                PositionKind::PriorYearReturn,
                format!("Did you file a {last} South Carolina return, and what was its tax?"),
                format!(
                    "With it, {} of last year's tax could set a lower target.",
                    y.prior_year_percent
                ),
            ));
            unavailable(format!("Needs your {last} South Carolina return"))
        } else if !prior.filed() && prior.not_required_to_file() {
            unavailable(format!(
                "You didn't need to file a {last} South Carolina return. There's no tax to measure against"
            ))
        } else if !prior.filed() {
            unavailable(format!("You didn't file a {last} South Carolina return"))
        } else if prior.months() != 12 {
            unavailable(format!(
                "Your {last} South Carolina return covered {} months, not 12",
                prior.months()
            ))
        } else {
            let pf = prior.figures().unwrap_or_default();
            prior_tax = pf.tax().unwrap_or_default();
            let agi_opt = pf.state_agi();
            let agi = agi_opt.unwrap_or_default();
            let mut rate = y.prior_year_percent;
            let mut why = format!("{last} South Carolina AGI of {agi} is not more than {step}");
            match agi_opt {
                None => {
                    rate = y.high_income_percent;
                    why = format!("{last} South Carolina AGI was not given");
                    target_asks.push((
                        PositionKind::AdjustedGrossIncome,
                        format!("What was your {last} South Carolina adjusted gross income?"),
                        format!(
                            "Until then, the target assumes over {}, so last year's test is {}.",
                            step.whole(),
                            y.high_income_percent
                        ),
                    ));
                }
                Some(agi) if agi > step => {
                    rate = y.high_income_percent;
                    why = format!("{last} South Carolina AGI of {agi} is more than {step}");
                }
                Some(_) => {}
            }
            if agi_opt.is_some() {
                if let Some(ask) = self.federal_agi_question(&pf, agi, step) {
                    target_asks.push(ask);
                }
                af.exact_step = agi == step;
                af.separate_step =
                    separate && agi > y.prior_year_step_separate && agi <= y.prior_year_step;
            }
            let amount = rate.of(prior_tax);
            a.notes.rule(
                "S.C. Code § 12-54-55(1); 26 U.S.C. § 6654(d)(1)(B)(ii), (C); SC1040ES (2026), Penalty for failure to file and pay; SC2210 (2025), Part I line 7",
                format!(
                    "{why}, so the prior-year test is {rate} of the {prior_tax} tax on the {last} SC1040, or {amount}"
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
        if !tax_known {
            target_asks.push((
                PositionKind::ThisYearTax,
                format!(
                    "What do you expect your {} South Carolina tax to be?",
                    y.year
                ),
                format!(
                    "With it, {} of this year's tax could be your target. A balance under {} would need no payments.",
                    y.this_year_percent,
                    y.threshold.whole()
                ),
            ));
        }

        // $0 liability for a full 12 months last year: SC2210 exception 1.
        // The forms state no citizenship or residence condition, so none is
        // applied.
        let no_liability = has_prior
            && prior.months() == 12
            && ((prior.filed() && prior_tax.0 == 0)
                || (!prior.filed() && prior.not_required_to_file()));
        let (contractor, contractor_answered) = extras.nonresident_contractor();

        if no_liability {
            a.exempt = true;
            let ex = estimated::new_exception("No tax due last year").build()?;
            rb.required(
                false,
                format!(
                    "Your {last} South Carolina tax was $0 for a full 12 months. No estimated payments are needed for {}.",
                    y.year
                ),
            )
            .exempt(ex);
            a.notes.rule(
                "SC2210 (2025), Exceptions to the penalty 1; SC1040ES (2026), Exceptions to filing; 26 U.S.C. § 6654(e)(2)",
                format!(
                    "the {last} liability was $0 for a full 12 months, so there is no penalty for {} (see Divergences on the residence condition)",
                    y.year
                ),
            );
        } else if !tax_known && !prior_opt.available() {
            let why = if has_prior {
                format!(
                    "Last year's option isn't open to you. Your target is {} of your {} South Carolina tax.",
                    y.this_year_percent, y.year
                )
            } else {
                format!(
                    "Your target is the smaller of two figures. They are {} of your {} South Carolina tax and {} of your {last} tax.",
                    y.this_year_percent, y.year, y.prior_year_percent
                )
            };
            rb.required(false, why).no_target();
        } else if !tax_known && withheld >= target {
            rb.required(
                false,
                format!(
                    "Your withholding of {} covers {} of your {last} tax, {}. No estimated payments are needed, whatever your {} tax turns out to be.",
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
                    "Your target is {} of your {last} tax, {}. It keeps you clear of the penalty whatever your {} tax turns out to be. After {} of withholding, that's {} of estimated payments. Pay a quarter by each date.",
                    prior_opt.percent(),
                    target.whole(),
                    y.year,
                    withheld.whole(),
                    (target - withheld).whole()
                ),
            )
            .choose(chosen);
        } else if tax - withheld < y.threshold {
            a.exempt = true;
            let ex =
                estimated::exception_below("Tax less withholding", tax - withheld, y.threshold)?;
            let why = if tax < withheld {
                format!(
                    "Your South Carolina tax is {}, and {} is withheld. That {}. No estimated payments are needed.",
                    tax.whole(),
                    withheld.whole(),
                    estimated::leaves(tax - withheld)
                )
            } else {
                format!(
                    "Your South Carolina tax of {} less {} withheld is under {}. No estimated payments are needed.",
                    tax.whole(),
                    withheld.whole(),
                    y.threshold.whole()
                )
            };
            rb.required(false, why).exempt(ex);
            a.notes.rule(
                "S.C. Code §§ 12-6-3910(A)(1), 12-54-55(1); SC2210 (2025), Part I lines 3 and 6",
                format!(
                    "{tax} of tax less {withheld} withheld is {}, under {}, so there is no penalty",
                    tax - withheld,
                    y.threshold
                ),
            );
        } else if withheld >= target {
            rb.required(
                false,
                format!(
                    "Your South Carolina withholding is {}. That covers the {} needed for the year. No estimated payments are needed.",
                    withheld.whole(),
                    target.whole()
                ),
            )
            .choose(chosen);
        } else if contractor {
            rb.required(
                false,
                format!(
                    "You're a nonresident working under contracts over $10,000. Each payment has 2% withheld. So you don't have to file a declaration of estimated tax. \
                     Withholding is {} short of the {} the penalty form measures against.",
                    (target - withheld).whole(),
                    target.whole()
                ),
            )
            .choose(chosen);
            a.notes.rule(
                "SC1040ES (2026), Exceptions to filing a Declaration of Estimated Tax",
                "a nonresident contractor with 2% withheld from each payment on contracts over $10,000 need not file a declaration",
            );
            a.notes.ask(
                PositionKind::JurisdictionFact,
                "Has the department said that excusing the declaration also excuses the penalty?",
                "Neither the form nor the law says, so until then the penalty follows the form.",
            );
        } else {
            rb.required(
                true,
                format!(
                    "Your South Carolina tax of {} less {} withheld leaves {} due. Withholding falls short of your {} target. You need estimated payments of {}, a quarter by each date.",
                    tax.whole(),
                    withheld.whole(),
                    (tax - withheld).whole(),
                    target.whole(),
                    (target - withheld).whole()
                ),
            )
            .choose(chosen);
        }
        a.requirement = rb.build()?;

        if !a.exempt {
            // An exception sets the target, so these answers can't change it.
            for (k, q, e) in target_asks {
                a.notes.ask(k, q, e);
            }
            let (_, answered) = extras.prior_year_farmer_or_fisher();
            if !answered {
                a.notes.ask(
                    PositionKind::JurisdictionFact,
                    format!(
                        "Did two-thirds of your {last} gross income come from farming and fishing?"
                    ),
                    "If so, the farmer rules apply; until then, your target assumes it didn't.",
                );
            }
            if a.requirement.required() && !contractor_answered {
                a.notes.ask(
                    PositionKind::JurisdictionFact,
                    "Are you a nonresident contractor in South Carolina, with 2% withheld?",
                    "That means 2% withheld from each payment on contracts over $10,000. If so, you don't have to file a declaration of estimated tax.",
                );
            }
        }

        let cumulative = estimated::cumulative_shares(a.requirement.target(), &y.shares)?;
        a.installments = estimated::increments(&cumulative);
        for (moved, due) in a.dues.iter().zip(&y.dues) {
            if moved != due {
                a.notes.rule(
                    "S.C. Code § 12-60-50(A); SC Revenue Procedure #23-1",
                    format!(
                        "the installment due {} falls on a Saturday, Sunday or legal holiday, so it is due {}",
                        due.long(),
                        moved.long()
                    ),
                );
            }
        }
        a.notes.rule(
            "S.C. Code § 12-6-3910(A)(3)(a); 26 U.S.C. § 6654(d)(1)(A); SC2210 (2025), Part III line 9",
            format!(
                "installments of {} of {}, {} each, are due {}, {}, {} and {}",
                y.shares[0],
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
            let ann = estimated::annualised(&taxes, &y.annualised_percentages, &a.installments)?;
            let ap = &y.annualised_percentages;
            a.notes.rule(
                "S.C. Code § 12-54-55(1); 26 U.S.C. § 6654(d)(2); SC2210 (2025), Annualized Income Installment Method",
                format!(
                    "under the annualised method, on federal Schedule AI for South Carolina purposes, the installments are {}, {}, {} and {}: {}, {}, {} and {} of the annualised tax, \
                     less earlier installments, and never more than the regular installment plus any shortfall the method carried forward",
                    ann[0], ann[1], ann[2], ann[3], ap[0], ap[1], ap[2], ap[3]
                ),
            );
            a.installments = ann;
        }

        af.late_start = self.late_start(f, &mut a);

        // § 6654(h): a return filed and paid by January 31 excuses the
        // January installment. January 31, 2027 is a Sunday; § 12-60-50 moves
        // it to February 1, as the 2026 SC1040ES prints it.
        let file_by = y.calendar.next_business_day(y.file_and_pay_by);
        let filed = f.return_filed();
        af.january = !a.exempt
            && !filed.is_zero()
            && filed.after(y.file_and_pay_by)
            && !filed.after(file_by);
        if !filed.is_zero() && !filed.after(file_by) && !a.exempt {
            a.excused = true;
            a.notes.rule(
                "26 U.S.C. § 6654(h); S.C. Code § 12-60-50(A); SC1040ES (2026), Payment due dates; SC2210 (2025), instructions",
                format!(
                    "the return was filed and paid on {}, by {} ({}, moved to the next business day), so the January installment is excused and the earlier installments' periods end on {}",
                    filed.long(),
                    file_by.long(),
                    y.file_and_pay_by.long(),
                    a.dues[3].long()
                ),
            );
        }

        if extras.underpayment_from_other_state_income().0 && !a.exempt {
            a.other_state = true;
            af.other_state = true;
            a.notes.rule(
                "S.C. Code § 12-54-55(1); SC1040ES (2026), Penalty for failure to file and pay",
                "the underpayment comes from personal service income earned in another state, which withheld its own tax, so no penalty is due on it",
            );
        }

        if a.exempt {
            a.installments = vec![USCents(0); a.dues.len()];
        }

        // What the rate and January readings turn on: a shortfall outstanding
        // while the rates differ, or after January 15.
        af.act110 = tax_known && !no_liability;
        af.no_liability = no_liability;
        if !a.exempt && a.requirement.target_known() {
            let (ledger, _) = self.ledger(f, &a)?;
            af.rate =
                !a.other_state && (ledger[0].shortfall().0 > 0 || ledger[1].shortfall().0 > 0);
            if a.excused && !a.other_state {
                for l in &ledger[..3] {
                    for piece in l.pieces() {
                        match piece.paid_on() {
                            Some(on) if !on.after(a.dues[3]) => {}
                            _ => af.earlier = true,
                        }
                    }
                }
            }
        }
        let divergences = self.divergences(&af)?;
        a.notes.diverges(&divergences);
        Ok(a)
    }

    /// The question that the 110% test's AGI is not settled, when the answer
    /// could change the percentage: federal AGI was not given, or lies on
    /// the other side of the step from South Carolina AGI.
    fn federal_agi_question(&self, pf: &Figures, agi: USCents, step: USCents) -> Option<Ask> {
        let last = self.y.year - 1;
        match pf.federal_agi() {
            None => Some((
                PositionKind::AdjustedGrossIncome,
                format!("What was your {last} federal adjusted gross income?"),
                format!(
                    "The law doesn't say which AGI the {} step for the 110% test reads. It could be federal or South Carolina AGI; your target uses South Carolina's.",
                    step.whole()
                ),
            )),
            Some(fed) if (fed > step) != (agi > step) => {
                let other = if fed > step {
                    self.y.high_income_percent
                } else {
                    self.y.prior_year_percent
                };
                Some((
                    PositionKind::AdjustedGrossIncome,
                    format!(
                        "Should the 110% test's {} step use federal AGI, not South Carolina's?",
                        step.whole()
                    ),
                    format!(
                        "Your target uses your South Carolina AGI of {}. Your federal AGI of {} would make the test {other}.",
                        agi.whole(),
                        fed.whole()
                    ),
                ))
            }
            Some(_) => None,
        }
    }

    /// Notes what the SC1040ES tells a filer whose requirement first arose
    /// after April 1, when the caller said so, and reports whether it did.
    /// The figure does not use it: the SC2210 and § 6654(d)(1)(A) require 25%
    /// by every date.
    fn late_start(&self, f: &Facts, a: &mut Assessment) -> bool {
        let met = f.requirement_first_met();
        if met.is_zero() || a.exempt {
            return false;
        }
        let mut first = 0;
        for (i, g) in self.y.late_groups.iter().enumerate() {
            if !met.before(*g) {
                first = i + 1;
            }
        }
        if first == 0 {
            return false;
        }
        let share = ["", "1/2", "3/4", "all"][first];
        a.notes.rule(
            "SC1040ES (2026), Payment due dates and worksheet line 11; SC2210 (2025), Part III line 9",
            format!(
                "the requirement first arose on {}; the SC1040ES would have your first payment be {share} of the estimate on {}, but the SC2210 requires {} by every date, so the earlier installments are charged here (see Divergences)",
                met.long(),
                a.dues[first].long(),
                self.y.shares[0]
            ),
        );
        true
    }

    /// Applies the credits to the installments, with the January installment
    /// set to nothing when filing early excused it.
    fn ledger(&self, f: &Facts, a: &Assessment) -> Result<(Vec<LedgerEntry>, Notes), Error> {
        let credits = if f.elections().withholding_as_withheld() {
            estimated::credits_as_withheld(f, &a.dues)?
        } else {
            estimated::credits(f, &a.dues, &self.y.shares)?
        };
        let mut required = a.installments.clone();
        if a.excused {
            required[3] = USCents(0);
        }
        let ledger = estimated::apply(&a.dues, &required, &credits)?;
        let mut notes = Notes::default();
        if f.elections().withholding_as_withheld() {
            notes.rule(
                "26 U.S.C. § 6654(g)(1); SC2210 (2025), Part II box D",
                format!(
                    "by election, the {} withheld is credited on the dates it was withheld",
                    f.withheld_total()
                ),
            );
        } else {
            notes.rule(
                "26 U.S.C. § 6654(g)(1); SC2210 (2025), How to calculate your underpayment",
                format!(
                    "the {} withheld is treated as paid in equal parts on the four due dates, whenever it was actually withheld",
                    f.withheld_total()
                ),
            );
        }
        notes.rule(
            "26 U.S.C. § 6654(b)(3); SC2210 (2025), Part III lines 10 to 17",
            "a payment after a due date pays the oldest unpaid installment first, whichever date it was meant for",
        );
        Ok((ledger, notes))
    }

    /// The installments from the ledger. The January installment of a return
    /// filed early, and every shortfall from income earned in another state,
    /// is excused.
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
            if (a.excused && i == 3) || a.other_state {
                b.excused();
            }
            out.push(b.build()?);
        }
        Ok(out)
    }
}

impl EstimatedTax for SouthCarolinaEstimated {
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
                    "with no {} South Carolina return to measure against, there is no target to schedule",
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
                "southcarolinataxcalc: a penalty needs the date to figure it to".into(),
            ));
        }
        let a = self.assess(f)?;
        if !a.requirement.target_known() {
            return Err(Error::with_detail(
                Error::NeedsThisYearTax,
                format!(
                    "with no {} South Carolina return to measure against, there is no target to figure a penalty from",
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
        if a.excused && a.dues[3].before(stop) {
            // SC2210 instructions: the earlier periods end on January 15.
            stop = a.dues[3];
        }
        let waiver_answered = f.positions().waiver();
        let waiver = waiver_answered.unwrap_or_default();
        let (ledger, mut notes) = self.ledger(f, &a)?;
        let schedule = self.installments(&a, &ledger)?;
        if a.exempt || a.other_state {
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
                "S.C. Code § 12-54-55(1); 26 U.S.C. § 6654(d)(1)(B)",
                format!(
                    "this year's South Carolina tax was not given, so the penalty is figured against {} of the {} tax alone; \
                     the required payment is the lesser of that and {} of this year's tax, so {} is the most the penalty can be, and it is less if this year's tax turns out lower",
                    a.requirement.options()[1].percent(),
                    y.year - 1,
                    y.this_year_percent,
                    p.total()
                ),
            );
        }
        if paid_in_full && !a.excused {
            notes.rule(
                "26 U.S.C. § 6654(b)(2)",
                format!(
                    "the return was filed with the whole balance paid on {}, so every shortfall still outstanding was paid that day",
                    stop.long()
                ),
            );
        }
        notes.rule(
            "S.C. Code §§ 12-54-25(D), 12-54-55(1); 26 U.S.C. § 6654(a), (b)(2); SC2210 (2025), Part III lines 18 to 26; SC Information Letter #26-19",
            format!(
                "each shortfall is charged simple interest at the department's rate for each period, divided by 365, for each day from its due date until paid or {}; \
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
                    y.rates.ends().format("Jan 2, 2006")
                ),
            );
        }
        if p.total().0 > 0 {
            // SC2210 waiver items 1 and 2 are the federal pair, at age 62.
            if let Some(o) = estimated::standard_waiver(f, "the department", 62) {
                p = p.to_builder().waiver_outcome(o).build()?;
            }
            if waiver_answered.is_none() {
                notes.ask(
                    PositionKind::PenaltyWaiver,
                    "Will you ask the department to waive the penalty?",
                    "Casualty, disaster, unusual events, retiring after 62 or disability can qualify. The last two need reasonable cause; the penalty shown is before any waiver.",
                );
            }
            let extras = self.extras(f).unwrap_or_default();
            if !extras.underpayment_from_other_state_income().1 {
                notes.ask(
                    PositionKind::JurisdictionFact,
                    "Is the underpayment from personal service income earned in another state?",
                    "Personal service income includes wages. If that state withheld its own tax, South Carolina charges no penalty on it.",
                );
            }
        }
        Ok(working::new_result(p).with(&a.notes).with(&notes).build()?)
    }

    fn explanation(&self) -> Explanation {
        south_carolina_explanation()
    }
}

/// Written by hand and checked against research/estimated-payments/sc.md.
fn south_carolina_explanation() -> Explanation {
    estimated::new_explanation(
        "South Carolina uses the federal estimated tax rules, but its threshold is $100. The federal threshold is $1,000. The penalty is interest on each quarter's shortfall.",
    )
    .paragraph("There is no penalty if withholding and estimated payments reach a target. The target is the smaller of 90% of this year's tax or 100% of last year's. The second figure is 110% if last year's AGI was over $150,000. The AGI limit is $75,000 for married filing separately. Last year's figure needs a South Carolina return covering a full twelve months. There is no penalty if this year's tax less withholding is under $100. There is also none after a full twelve months with $0 of South Carolina tax.")
    .paragraph("Pay a quarter each by April 15, June 15, September 15 and the next January 15. A weekend or South Carolina legal holiday moves a date to the next business day. The state list has days the federal one lacks, such as Confederate Memorial Day. Withholding counts as paid evenly on the four dates. You can instead show when it was actually withheld. Filing and paying the balance by February 1, 2027 excuses the January payment. The earlier shortfalls then stop accruing on January 15.")
    .paragraph("The penalty is simple interest on a shortfall for the days it stays unpaid. The department's rate follows the federal underpayment rate. Each day is charged the rate divided by 365. Interest stops on April 15 of the next year. A payment goes first to the oldest shortfall. For 2026 the rate is 6% from April to June and 7% after. If income came unevenly, the federal Schedule AI can lower the early payments. You complete it for South Carolina.")
    .paragraph("The department can waive the penalty for a casualty or disaster. It can also do so for retirement after 62 or disability, with reasonable cause. The 2026 estimate worksheet predates the 2026 rate cut. Use the new rates for this year's tax before comparing it with these targets.")
    .versus_federal("South Carolina differs from federal in four ways that can change a number. Its threshold is $100 rather than $1,000. There is no penalty on an underpayment from income earned in another state. That state must have withheld its tax. South Carolina has its own legal holidays. A return can be filed and paid by the end of January. Then earlier shortfalls stop accruing on January 15.")
    .source("S.C. Code §§ 12-6-3910, 12-54-25 and 12-54-55; 26 U.S.C. § 6654")
    .source("SC1040ES, Individual Declaration of Estimated Tax, 2026 (Rev. 10/13/25)")
    .source("SC2210, Underpayment of Estimated Tax by Individuals, Estates, and Trusts, 2025 (Rev. 12/02/25)")
    .source("SC Information Letter #26-19 (interest rate); SC Revenue Procedure #23-1 (legal holidays)")
    .source("Act No. 110 of 2026; SC Information Letter #26-20")
    .build()
    .unwrap_or_default()
}
