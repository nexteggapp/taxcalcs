//! Illinois's estimated income tax rules, Illinois Income Tax Act §§ 803,
//! 804 and 806 (35 ILCS 5/803, 5/804, 5/806), with the penalty rates of the
//! Uniform Penalty and Interest Act § 3-3 (35 ILCS 735/3-3), as the shared
//! [`EstimatedTax`] interface.
//!
//! The penalty is not interest. Each amount paid late is charged once, 2% if
//! it is paid within 30 days of the due date and 10% if it is paid later or
//! not at all. The rest is close to federal in shape: 90% of this year's tax
//! or 100% of last year's, four equal installments, and a $1,000 threshold.
//!
//! The figure follows the department's forms, the 2026 IL-1040-ES and the
//! 2025 IL-2210 and its instructions, and the result's Divergences say where
//! the Act or the department's own rules read otherwise. No act changed
//! these rules for 2026 (research/estimated-payments/il.md, "2026 changes"),
//! so no form here predates the law it describes.

use std::any::Any;

use estimated::{
    Basis, Calendar, Date, Error, EstimatedTax, Explanation, Facts, Figures, Installment,
    Jurisdiction, PartKind, Penalty, PenaltyPart, RateSeries, Requirement, TargetOption,
    TargetOptionBuilder, WaiverCause, WaiverKind, WaiverOutcome,
};
use money::{Rate, USCents};
use working::{Divergence, Notes, PositionKind, Reading};

use crate::estimated_y2026::year_2026;

/// jurisdiction is Illinois's postal code.
const JURISDICTION: Jurisdiction = estimated::ILLINOIS;

/// The facts Illinois's rules read that no other jurisdiction does. Carry
/// them in [`Facts::state`].
///
/// [`Facts::withholding`] is Illinois income tax withheld from wages and
/// other income, Form IL-1040 line 25. Pass-through withholding, line 27, is
/// dated differently and comes here.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct EstimatedFacts {
    nursing_home: Option<bool>,
    pass_through: Vec<PassThroughWithholding>,
    pass_through_answered: bool,
    interest: Option<bool>,
    overpayment_late: Option<Date>,
}

impl EstimatedFacts {
    /// Whether the filer or spouse is 65 or older and lives permanently in a
    /// nursing home (Form IL-1040 line 34b), or `None` when the caller has
    /// not said.
    pub fn nursing_home(&self) -> Option<bool> {
        self.nursing_home
    }

    /// The pass-through withholding on the filer's behalf, Form IL-1040 line
    /// 27, one entry per entity, or `None` when the caller has not said.
    pub fn pass_through_withholding(&self) -> Option<Vec<PassThroughWithholding>> {
        self.pass_through_answered
            .then(|| self.pass_through.clone())
    }

    /// Whether interest is to be charged on a late installment on top of
    /// the penalty, or `None` when the caller has not said. No Illinois
    /// document settles it; see [`EstimatedTax::penalty`].
    pub fn interest_on_late_installments(&self) -> Option<bool> {
        self.interest
    }

    /// The day last year's overpayment was elected to this year's estimated
    /// tax, when the election came after the extended due date of last
    /// year's return. `Some` of the zero Date means it came in time; `None`
    /// means the caller has not said.
    pub fn overpayment_elected_late(&self) -> Option<Date> {
        self.overpayment_late
    }
}

/// Illinois tax a partnership, S corporation or trust withheld on the
/// filer's behalf, Schedule K-1-P or K-1-T, and the last day of that
/// entity's tax year.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct PassThroughWithholding {
    year_end: Date,
    amount: USCents,
}

impl PassThroughWithholding {
    /// The last day of the entity's tax year.
    pub fn year_end(&self) -> Date {
        self.year_end
    }

    /// The amount withheld.
    pub fn amount(&self) -> USCents {
        self.amount
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
    /// Answers whether the filer or spouse is 65 or older and lives
    /// permanently in a nursing home.
    pub fn nursing_home(&mut self, qualifies: bool) -> &mut Self {
        self.e.nursing_home = Some(qualifies);
        self
    }

    /// Adds the amount one entity withheld, and the last day of its tax
    /// year.
    pub fn pass_through_withholding(
        &mut self,
        entity_year_end: Date,
        amount: USCents,
    ) -> &mut Self {
        self.e.pass_through.push(PassThroughWithholding {
            year_end: entity_year_end,
            amount,
        });
        self.e.pass_through_answered = true;
        self
    }

    /// Answers that no entity withheld on the filer's behalf.
    pub fn no_pass_through_withholding(&mut self) -> &mut Self {
        self.e.pass_through_answered = true;
        self
    }

    /// Answers whether to charge interest on a late installment on top of
    /// the penalty.
    pub fn interest_on_late_installments(&mut self, charge: bool) -> &mut Self {
        self.e.interest = Some(charge);
        self
    }

    /// Answers that last year's overpayment was elected to this year by the
    /// extended due date of last year's return.
    pub fn overpayment_elected_on_time(&mut self) -> &mut Self {
        self.e.overpayment_late = Some(Date::default());
        self
    }

    /// Answers that last year's overpayment was elected to this year after
    /// the extended due date of last year's return, on the day given.
    pub fn overpayment_elected_late(&mut self, on: Date) -> &mut Self {
        self.e.overpayment_late = Some(on);
        self
    }

    /// Refuses pass-through withholding with no year end or a negative
    /// amount. Whether a date falls in the tax year is checked by the rules,
    /// which know the year.
    pub fn build(&self) -> Result<EstimatedFacts, Error> {
        for p in &self.e.pass_through {
            if p.year_end.is_zero() {
                return Err(Error::Invalid(
                    "illinoistaxcalc: pass-through withholding needs the entity's year end".into(),
                ));
            }
            if p.amount.0 < 0 {
                return Err(Error::Invalid(format!(
                    "illinoistaxcalc: pass-through withholding of {} is negative",
                    p.amount
                )));
            }
        }
        Ok(self.e.clone())
    }
}

/// One tax year's published figures.
#[derive(Clone, Debug)]
pub(crate) struct EstimatedYear {
    pub(crate) year: i64,
    /// IL-2210 line 7: no installment is required when this year's tax less
    /// credits and all withholding is this or less.
    pub(crate) threshold: USCents,
    /// The figure 86 Ill. Adm. Code 100.8000(a)(1) still prints, superseded
    /// by the Act. It is reported, not used.
    pub(crate) rule_threshold: USCents,
    /// The two measures of the required annual payment, IL-2210 lines 6 and
    /// 7.
    pub(crate) this_year: Rate,
    pub(crate) prior_year: Rate,
    pub(crate) dues: Vec<Date>,
    pub(crate) calendar: Calendar,
    /// IL-2210 Step 6: the first 3, 5 and 8 months, and the year.
    pub(crate) period_ends: Vec<Date>,
    /// Step 6's line 48 percentages.
    pub(crate) applicable: Vec<Rate>,
    /// The most days late that IL-2210's lower penalty rate covers.
    pub(crate) late_days: i64,
    /// The two rates.
    pub(crate) low: Rate,
    pub(crate) high: Rate,
    /// The uniform interest rate, used only when the caller asks for
    /// interest on late installments.
    pub(crate) interest: RateSeries,
}

/// 35 ILCS 5/803(d) and IL-2210 line 8: four equal installments. IL-2210
/// line 10a credits withholding in the same four parts.
const QUARTERS: [Rate; 4] = [Rate(2500), Rate(2500), Rate(2500), Rate(2500)];

/// Returns Illinois's estimated tax rules for a tax year, or
/// [`Error::NoRules`] for a year this build does not carry.
pub fn estimated_tax(year: i64) -> Result<Box<dyn EstimatedTax>, Error> {
    let y = match year {
        2026 => year_2026()?,
        _ => return Err(estimated::no_rules(&JURISDICTION, year)),
    };
    Ok(Box::new(IllinoisEstimated {
        y,
        explanation: illinois_explanation()?,
    }))
}

struct IllinoisEstimated {
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
    /// Each quarter's required installment, IL-2210 line 9b.
    installments: Vec<USCents>,
    extras: EstimatedFacts,
    notes: Notes,
}

/// Which divergences change anything for the facts.
#[derive(Clone, Copy, Default)]
struct Affects {
    no_liability: bool,
    short_year: bool,
    non_wage: bool,
    applying: bool,
    actual_dates: bool,
    threshold: bool,
    farmer: bool,
}

/// One installment after IL-2210 has applied withholding and the estimated
/// payments to it.
#[derive(Clone, Debug, Default)]
struct Quarter {
    number: i64,
    due: Date,
    /// Line 9b.
    required: USCents,
    /// Line 10c.
    withheld: USCents,
    /// Line 13 when positive: the installment less this quarter's
    /// withholding and any excess carried from earlier quarters.
    unpaid: USCents,
    /// The estimated payments that paid the unpaid amount, in order; a piece
    /// with no date was never paid.
    pieces: Vec<Piece>,
    credited_by: USCents,
}

impl Quarter {
    /// What was still unpaid on the due date.
    fn shortfall(&self) -> USCents {
        let mut s = self.unpaid;
        for p in &self.pieces {
            if !p.paid_on.is_zero() && !p.paid_on.after(self.due) {
                s -= p.amount;
            }
        }
        s
    }
}

/// An estimated payment as IL-2210 line 20 lists it.
#[derive(Clone, Copy, Debug)]
struct Pay {
    on: Date,
    amount: USCents,
}

/// Part of a quarter's unpaid amount and the day a payment paid it. The
/// zero Date means it was never paid.
#[derive(Clone, Copy, Debug, Default)]
struct Piece {
    amount: USCents,
    paid_on: Date,
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

/// "Apr 15" inside the tax year and "Jan 15, 2027" outside it.
fn short_date(d: Date, year: i64) -> String {
    let month = d.month().to_string();
    let m = month.get(..3).unwrap_or(&month);
    if d.year() == year {
        return format!("{m} {}", d.day());
    }
    format!("{m} {}, {}", d.day(), d.year())
}

/// The year's pass-through withholding.
fn pass_through_total(e: &EstimatedFacts) -> USCents {
    e.pass_through.iter().map(|p| p.amount).sum()
}

/// `amount` in whole dollars, or "just over" or "just under" `limit` when
/// rounding would show it as `limit`.
fn near(amount: USCents, limit: USCents) -> String {
    if amount > limit && amount.whole() == limit.whole() {
        return format!("just over {}", limit.whole());
    }
    if amount < limit && amount.whole() == limit.whole() {
        return format!("just under {}", limit.whole());
    }
    amount.whole()
}

/// The index of the first due date on or after `d`.
fn quarter_of(dues: &[Date], d: Date) -> usize {
    dues.iter()
        .position(|due| !d.after(*due))
        .unwrap_or(dues.len().saturating_sub(1))
}

/// The installments the ledger gives.
fn schedule_of(ledger: &[Quarter]) -> Result<Vec<Installment>, Error> {
    let mut out = Vec::with_capacity(ledger.len());
    let mut needed = USCents(0);
    for q in ledger {
        needed += q.required;
        out.push(
            estimated::new_installment(q.number, q.due)
                .needed_by(needed)
                .credited_by(q.credited_by)
                .shortfall(q.shortfall())
                .build()?,
        );
    }
    Ok(out)
}

/// Decides a waiver asked for. Illinois has no waiver for retirement or
/// disability. A casualty or disaster can be reasonable cause: 86 Ill. Adm.
/// Code 700.400(e)(5) names a fire or other casualty that destroyed the home
/// or business records, (f)(3) and (4) weigh a natural disaster, and
/// (e)(13) makes an IRS disaster extension reasonable cause for a payment
/// made by the extended date. It returns `None` when no waiver was asked
/// for.
fn waiver_outcome(f: &Facts) -> Option<WaiverOutcome> {
    let pos = f.positions();
    match pos.waiver() {
        None | Some(WaiverKind::None) => None,
        Some(WaiverKind::RetiredOrDisabled) => Some(estimated::waiver_refused(
            "Illinois has no waiver for retirement or disability, only for reasonable cause. The Illinois Department of Revenue decides reasonable cause.",
        )),
        Some(WaiverKind::Casualty) if pos.waiver_cause() == WaiverCause::Disaster => {
            Some(estimated::waiver_eligible(
                "Paying Illinois within an IRS disaster extension is reasonable cause. The Illinois Department of Revenue decides when a disaster lifts the penalty.",
            ))
        }
        Some(WaiverKind::Casualty) => Some(estimated::waiver_eligible(
            "Losing home or business records to fire or other casualty is reasonable cause. The Illinois Department of Revenue decides, and can lift the penalty.",
        )),
    }
}

impl EstimatedYear {
    /// The IL-2210 Penalty Worksheet 1 charge on one amount paid late: the
    /// low rate for up to late_days days, the high rate after that or when
    /// never paid, rounded to the cent. It returns `None` for an amount paid
    /// on or before its due date.
    fn flat_part(
        &self,
        principal: USCents,
        due: Date,
        paid_or_stop: Date,
    ) -> Result<Option<PenaltyPart>, Error> {
        let days = paid_or_stop.days_since(due);
        if days <= 0 {
            return Ok(None);
        }
        let mut rate = self.low;
        if days > self.late_days {
            rate = self.high;
        }
        let p = estimated::new_penalty_part(PartKind::Flat, principal, rate)
            .span(due, paid_or_stop)
            .days(days)
            .charge(rate.of(principal))
            .source(format!(
                "IL-2210 (R-12/25), Penalty rates: {days} days late, {rate}; 35 ILCS 735/3-3(b-25)(1)"
            ))
            .build()?;
        Ok(Some(p))
    }
}

impl IllinoisEstimated {
    fn extras(&self, f: &Facts) -> Result<EstimatedFacts, Error> {
        match f.state() {
            None => Ok(EstimatedFacts::default()),
            Some(s) => match s.downcast_ref::<EstimatedFacts>() {
                Some(e) => Ok(e.clone()),
                None => Err(Error::Invalid(format!(
                    "illinoistaxcalc: the facts carry {}, not illinoistaxcalc.EstimatedFacts",
                    type_name(s)
                ))),
            },
        }
    }

    fn check(&self, f: &Facts, e: &EstimatedFacts) -> Result<(), Error> {
        let y = &self.y;
        if f.jurisdiction() != JURISDICTION {
            return Err(Error::Invalid(format!(
                "illinoistaxcalc: these are Illinois's rules, and the facts are for {}",
                f.jurisdiction()
            )));
        }
        if f.tax_year() != y.year {
            return Err(Error::Invalid(format!(
                "illinoistaxcalc: these are the {} rules, and the facts are for {}",
                y.year,
                f.tax_year()
            )));
        }
        for p in &e.pass_through {
            if p.year_end.year() != y.year {
                return Err(Error::Invalid(format!(
                    "illinoistaxcalc: pass-through withholding for an entity year ending {} does not go on a {} return",
                    p.year_end, y.year
                )));
            }
        }
        if let Some(on) = e.overpayment_elected_late()
            && !on.is_zero()
            && on.year() < y.year
        {
            return Err(Error::Invalid(format!(
                "illinoistaxcalc: an overpayment elected on {on} is before tax year {}",
                y.year
            )));
        }
        if f.elections().annualised() {
            for (i, p) in f.periods().iter().enumerate() {
                let want = y.period_ends.get(i).copied().unwrap_or_default();
                if p.end() != want {
                    return Err(Error::Invalid(format!(
                        "illinoistaxcalc: IL-2210 Step 6 period {} ends on {}, not {}",
                        i + 1,
                        want,
                        p.end()
                    )));
                }
            }
        }
        Ok(())
    }

    /// Where the department's forms and the Act, or the department's own
    /// rules, part ways on a rule every Illinois computation uses. Most are
    /// in research/estimated-payments/il.md, item 10 and its verification;
    /// the first and fourth were found reading the forms beside the rules.
    fn divergences(&self, af: Affects) -> Result<Vec<Divergence>, Error> {
        let y = &self.y;
        let mut out = Vec::new();
        for b in [
            working::new_divergence("No liability last year")
                .form(
                    "Subtract credits and all Illinois withholding from last year's tax. If the result is zero, there's no penalty. So a year fully covered by withholding counts.",
                    "IL-2210 Instructions (R-12/25), What is late-payment penalty?",
                )
                .statute(
                    "There's no penalty only if you had no tax liability for a 12-month year before. Withholding pays the tax; it doesn't reduce the liability.",
                    "35 ILCS 5/804(d); 86 Ill. Adm. Code 100.8010(b)(2), (g)(3)",
                )
                .uses(Reading::Form)
                .affects(af.no_liability),
            working::new_divergence("Last year's return as a measure")
                .form(
                    "Column B takes last year's tax less credits with no condition on the return. The no-liability exception says nothing about the year's length.",
                    "IL-2210 (R-12/25), Step 2, Column B; IL-2210 Instructions (R-12/25)",
                )
                .statute(
                    "100% of last year's tax counts only if you filed a 12-month return showing tax. The no-liability exception also needs a 12-month year.",
                    "35 ILCS 5/804(c)(1)(B)(ii), (d); 86 Ill. Adm. Code 100.8010(d)(1)(A)(ii), (g)(3)",
                )
                .uses(Reading::Statute)
                .affects(af.short_year),
            working::new_divergence("Withholding not from wages")
                .form(
                    "All Illinois withholding is treated as paid in four equal parts. That includes 1099 withholding. Pay stubs or an employer letter can show the actual quarters instead.",
                    "IL-2210 Instructions (R-12/25), Line 10a; 86 Ill. Adm. Code 100.8010(e)(2)",
                )
                .statute(
                    "Withholding from wages is spread over the due dates. Other Illinois withholding is a payment on the date it is made.",
                    "35 ILCS 5/804(g)(1), (g)(3)",
                )
                .uses(Reading::Form)
                .affects(af.non_wage),
            working::new_divergence("How withholding meets the installments")
                .form(
                    "Each quarter's withholding comes off that quarter's installment. Any excess carries to later quarters only. Estimated payments then go to the oldest unpaid quarter.",
                    "IL-2210 (R-12/25), Step 2, lines 10a to 13; IL-2210 Instructions (R-12/25), Lines 10a, 10b and Penalty Worksheet 1",
                )
                .statute(
                    "Withholding is a payment on each due date. Pass-through withholding is a payment on the last day of the entity's year. Each goes to the oldest unpaid installment first.",
                    "35 ILCS 5/804(g); 86 Ill. Adm. Code 100.8010(e)(1), (e)(2), citing IITA § 709.5(b)",
                )
                .uses(Reading::Form)
                .affects(af.applying),
            working::new_divergence("Choosing actual withholding dates")
                .form(
                    "Line 10a takes one-fourth of withholding or the actual amount, for each quarter. The line doesn't say the choice must cover every quarter.",
                    "IL-2210 Instructions (R-12/25), Line 10a",
                )
                .statute(
                    "You can use actual dates only if you show the dates of everything withheld. Then all withholding counts on its dates.",
                    "35 ILCS 5/804(g)(1); 86 Ill. Adm. Code 100.8010(e)(2)",
                )
                .uses(Reading::Statute)
                .affects(af.actual_dates),
            working::new_divergence(format!("The {} threshold", y.threshold.whole()))
                .form(
                    format!(
                        "No installment is required at {} or less. The figure is this year's tax after credits and withholding.",
                        y.threshold.whole()
                    ),
                    "IL-1040-ES (R-09/26), Am I required to make estimated income tax payments?; IL-2210 (R-12/25), line 7",
                )
                .statute(
                    format!(
                        "The law's threshold has been {} since 2019. The department's rule still says {}, which the law has superseded.",
                        y.threshold.whole(),
                        y.rule_threshold.whole()
                    ),
                    "35 ILCS 5/803(a); 86 Ill. Adm. Code 100.8000(a)(1), 100.8010(g)(1)(A)",
                )
                .uses(Reading::Form)
                .affects(af.threshold),
            working::new_divergence("Exceptions the form does not list")
                .form(
                    format!(
                        "The form lists five cases with no penalty. Two are farmers and anyone 65 or older in a nursing home. Two are no return required last year and no liability last year. The fifth is {} or less this year.",
                        y.threshold.whole()
                    ),
                    "IL-2210 Instructions (R-12/25), What is late-payment penalty?",
                )
                .statute(
                    "The department's rule also excuses an underpayment after the taxpayer's death. Service in a combat zone with a federal extension is excused too. So are an innocent spouse to the extent relieved and a bankruptcy estate.",
                    "86 Ill. Adm. Code 100.8010(g)(1)(B)(iv), (g)(6), (g)(7), (g)(8)",
                )
                // No fact these exceptions turn on is carried.
                .uses(Reading::Form)
                .affects(false),
            working::new_divergence("The farmer test")
                .form(
                    "At least two-thirds of your total federal gross income is from farming.",
                    "IL-1040-ES (R-09/26), Are there any exceptions?; IL-1040 (R-12/25), line 34a",
                )
                .statute(
                    "Two-thirds of your total estimated gross income is from farming. The share is measured as the federal rule does. On a joint return the share is of the couple's combined income.",
                    "35 ILCS 5/803(e); 86 Ill. Adm. Code 100.8000(b)(2), (e)(3)",
                )
                .uses(Reading::Form)
                .affects(af.farmer),
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
            extras: extras.clone(),
            dues: estimated::move_dates(&y.calendar, &y.dues),
            ..Assessment::default()
        };
        let last = y.year - 1;
        let tax_opt = f.this_year().tax();
        let tax_known = tax_opt.is_some();
        let tax = tax_opt.unwrap_or_default();
        a.tax_known = tax_known;
        let withheld = f.withheld_total();
        let pass_through = pass_through_total(&extras);
        let all_withheld = withheld + pass_through;
        let prior_opt = f.prior_year();
        let has_prior = prior_opt.is_some();
        let prior = prior_opt.unwrap_or_default();
        let prior_figures = if has_prior {
            prior.figures().unwrap_or_default()
        } else {
            Figures::default()
        };
        let prior_tax = prior_figures.tax().unwrap_or_default();
        // 90% of this year's tax after credits, IL-2210 line 6.
        let this_year: TargetOption = if tax_known {
            estimated::new_target_option(Basis::ThisYearTax, y.this_year, y.this_year.of(tax))
                .build()?
        } else {
            estimated::new_target_option(Basis::ThisYearTax, y.this_year, USCents(0))
                .unavailable(estimated::NEEDS_THIS_YEAR_TAX)
                .build()?
        };
        if tax_known {
            a.notes.rule(
                "35 ILCS 5/804(c)(1)(B)(i); IL-2210 (R-12/25), lines 3 and 6",
                format!(
                    "{} of this year's Illinois tax after credits is {}",
                    y.this_year,
                    this_year.amount()
                ),
            );
        }
        // 100% of last year's tax after credits, IL-2210 Column B line 3,
        // for a filed 12-month return showing a liability.
        let unavailable = |why: String| -> TargetOptionBuilder {
            let mut b = estimated::new_target_option(Basis::PriorYearTax, y.prior_year, USCents(0));
            b.unavailable(why);
            b
        };
        let prior_option = if !has_prior {
            unavailable(format!("Needs your {last} Illinois return"))
        } else if prior.not_required_to_file() {
            unavailable(format!("You didn't have to file a {last} Illinois return"))
        } else if !prior.filed() {
            unavailable(format!("You didn't file a {last} Illinois return"))
        } else if prior.months() != 12 {
            unavailable(format!(
                "Your {last} Illinois return covered {} months, not 12",
                prior.months()
            ))
        } else if prior_tax.0 == 0 {
            unavailable(format!("Your {last} Illinois return showed no tax"))
        } else {
            a.notes.rule(
                "35 ILCS 5/804(c)(1)(B)(ii); IL-2210 (R-12/25), Column B line 3 and line 7",
                format!(
                    "{} of the {prior_tax} tax after credits on the {last} return is {}",
                    y.prior_year,
                    y.prior_year.of(prior_tax)
                ),
            );
            estimated::new_target_option(
                Basis::PriorYearTax,
                y.prior_year,
                y.prior_year.of(prior_tax),
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
                    "What do you expect your {} Illinois tax after credits to be?",
                    y.year
                ),
                format!(
                    "It sets the {} option and decides if you're within the {} threshold.",
                    y.this_year,
                    y.threshold.whole()
                ),
            );
        }
        // Last year's tax less credits and all withholding, the form's test
        // for no liability. Without last year's withholding the test is
        // taken on the tax alone, which the Act's reading also gives.
        let prior_withheld = prior_figures.withheld();
        let mut prior_no_liability = false;
        if has_prior && prior.filed() && prior.months() == 12 {
            if prior_tax.0 == 0 {
                prior_no_liability = true;
            } else if let Some(pw) = prior_withheld {
                prior_no_liability = (prior_tax - pw).0 <= 0;
            } else {
                a.notes.ask(
                    PositionKind::PriorYearReturn,
                    format!("How much Illinois tax was withheld for you in {last}?"),
                    format!(
                        "If it covered all of your {last} tax, there's no penalty for {}.",
                        y.year
                    ),
                );
            }
        }
        let nursing_home = extras.nursing_home();
        let mut by_no_liability = false;
        if i64::from(f.farm_share().0) * 3 >= 20_000 {
            a.exempt = true;
            let ex = estimated::new_exception("Two-thirds of gross income from farming").build()?;
            rb.required(
                false,
                format!(
                    "At least two-thirds of your gross income is from farming. Illinois requires no estimated payments for {} and charges no penalty.",
                    y.year
                ),
            )
            .exempt(ex);
            a.notes.rule(
                "35 ILCS 5/803(a), (e); IL-1040-ES (R-09/26), Are there any exceptions?; IL-1040 (R-12/25), line 34a",
                format!(
                    "{} of gross income is from farming, at least two-thirds, so no estimated payments are required; on a joint return the share is of the couple's combined income (86 Ill. Adm. Code 100.8000(e)(3))",
                    f.farm_share()
                ),
            );
        } else if nursing_home == Some(true) {
            a.exempt = true;
            let ex = estimated::new_exception("Living permanently in a nursing home").build()?;
            rb.required(
                false,
                "You or your spouse are 65 or older and live permanently in a nursing home. Illinois requires no estimated payments and charges no penalty.",
            )
            .exempt(ex);
            a.notes.rule(
                "35 ILCS 5/806; IL-1040-ES (R-09/26), Are there any exceptions?; IL-1040 (R-12/25), line 34b",
                "the filer or spouse is 65 or older and a permanent resident of a nursing home, so no estimated payments are required",
            );
        } else if has_prior && prior.not_required_to_file() {
            a.exempt = true;
            let ex = estimated::new_exception("No Illinois return required last year").build()?;
            rb.required(
                false,
                format!(
                    "You didn't have to file an Illinois return for {last}. Illinois therefore charges no estimated tax penalty for {}.",
                    y.year
                ),
            )
            .exempt(ex);
            a.notes.rule(
                "35 ILCS 5/804(d); IL-2210 Instructions (R-12/25); IL-1040 (R-12/25), line 34d",
                format!(
                    "no {last} Illinois return was required, so there is no penalty for {}",
                    y.year
                ),
            );
        } else if prior_no_liability {
            by_no_liability = true;
            a.exempt = true;
            let ex = estimated::new_exception("No Illinois tax due last year").build()?;
            rb.required(
                false,
                format!(
                    "Your {last} Illinois return showed no tax due after credits and withholding. Illinois therefore charges no estimated tax penalty for {}.",
                    y.year
                ),
            )
            .exempt(ex);
            a.notes.rule(
                "35 ILCS 5/804(d); IL-2210 Instructions (R-12/25), What is late-payment penalty?",
                format!(
                    "the {last} return's {prior_tax} of tax after credits less {} of withholding is zero or less, for a 12-month year, so there is no penalty for {}",
                    prior_withheld.unwrap_or_default(),
                    y.year
                ),
            );
        } else if tax_known && tax - all_withheld <= y.threshold {
            a.exempt = true;
            let ex = estimated::exception_at_most(
                "Tax less withholding",
                tax - all_withheld,
                y.threshold,
            )?;
            let mut leaves = format!("leaves {}", near(tax - all_withheld, y.threshold));
            let mut then = format!(
                "That is {} or less, so you need no estimated payments and have no penalty.",
                y.threshold.whole()
            );
            if tax < all_withheld {
                leaves = estimated::leaves(tax - all_withheld);
                then = "No estimated payments are needed and there's no penalty.".into();
            }
            rb.required(
                false,
                format!(
                    "Your Illinois tax after credits is {}, and {} was withheld. That {leaves}. {then}",
                    tax.whole(),
                    all_withheld.whole()
                ),
            )
            .exempt(ex);
            a.notes.rule(
                "35 ILCS 5/803(a); IL-2210 (R-12/25), lines 5 and 7; IL-1040-ES (R-09/26), Estimated Tax Worksheet line 10",
                format!(
                    "{tax} of tax less {withheld} of Illinois withholding and {pass_through} of pass-through withholding is {}, not more than {}, so no installment is required",
                    tax - all_withheld,
                    y.threshold
                ),
            );
        } else if !tax_known && !prior_opt.available() {
            rb.required(
                false,
                format!(
                    "Your target needs your {} Illinois tax, since your {last} return can't set one.",
                    y.year
                ),
            )
            .no_target();
        } else if !tax_known && all_withheld >= target {
            rb.required(
                false,
                format!(
                    "Your withholding of {} already reaches {} of your {last} tax, {}. No estimated payments are needed whatever your {} tax turns out to be.",
                    all_withheld.whole(),
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
                    "Your target is {} of your {last} tax, {}. After {} of withholding that's {} of estimated payments. A quarter by each date keeps you clear of a penalty whatever your {} tax is.",
                    prior_opt.percent(),
                    target.whole(),
                    all_withheld.whole(),
                    (target - all_withheld).whole(),
                    y.year
                ),
            )
            .choose(chosen);
        } else if all_withheld >= target {
            rb.required(
                false,
                format!(
                    "Your Illinois withholding of {} already reaches your target of {}. No estimated payments are needed.",
                    all_withheld.whole(),
                    target.whole()
                ),
            )
            .choose(chosen);
        } else {
            rb.required(
                true,
                format!(
                    "Your Illinois tax after credits is {}, and {} was withheld. That leaves {}. That is more than {}, so you need {} of estimated payments. Paying a quarter of that by each date reaches your target of {}.",
                    tax.whole(),
                    all_withheld.whole(),
                    near(tax - all_withheld, y.threshold),
                    y.threshold.whole(),
                    (target - all_withheld).whole(),
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
                format!("Did you file a {last} Illinois return, and what was its tax?"),
                format!(
                    "It can make {} of last year's tax your target, not {} of this year's. It also decides two exceptions.",
                    y.prior_year, y.this_year
                ),
            );
        }
        // What each divergence turns on in these facts.
        let mut af = Affects {
            no_liability: by_no_liability && prior_tax.0 > 0,
            short_year: !a.exempt
                && has_prior
                && prior.filed()
                && prior.months() != 12
                && (prior_tax.0 == 0
                    || !tax_known
                    || y.prior_year.of(prior_tax) < this_year.amount()),
            threshold: a.exempt
                && !by_no_liability
                && tax_known
                && tax - all_withheld > y.rule_threshold
                && tax - all_withheld <= y.threshold,
            farmer: f.farm_share().0 > 0,
            ..Affects::default()
        };
        if a.exempt || !a.requirement.target_known() {
            a.installments = vec![USCents(0); a.dues.len()];
            self.diverges(&mut a, af)?;
            return Ok(a);
        }
        if nursing_home.is_none() {
            a.notes.ask(
                PositionKind::JurisdictionFact,
                "Are you or your spouse 65 or older and living permanently in a nursing home?",
                "If so, Illinois requires no estimated payments.",
            );
        }
        if extras.pass_through_withholding().is_none() {
            a.notes.ask(
                PositionKind::JurisdictionFact,
                format!(
                    "Did a partnership, S corporation or trust withhold Illinois tax for you in {}?",
                    y.year
                ),
                "Any such withholding counts toward the quarter in which the entity's year ended.",
            );
        }
        let cum = estimated::cumulative_shares(a.requirement.target(), &QUARTERS)?;
        a.installments = estimated::increments(&cum);
        a.notes.rule(
            "35 ILCS 5/803(d), 5/804(c)(1)(A); IL-2210 (R-12/25), lines 8 and 9a; IL-1040-ES (R-09/26), When are my payments due?",
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
        if f.elections().annualised() {
            let taxes: Vec<USCents> = f.periods().iter().map(|p| p.annualised_tax()).collect();
            let ann = estimated::annualised(&taxes, &y.applicable, &a.installments)?;
            a.notes.rule(
                "35 ILCS 5/804(c)(2); IL-2210 (R-12/25), Step 6, lines 38 to 56",
                format!(
                    "under the annualised method the installments are {}, {}, {} and {}: {}, {}, {} and {} of the annualised tax after credits, less earlier installments, and never more than the regular installment plus any reduction carried forward",
                    ann[0],
                    ann[1],
                    ann[2],
                    ann[3],
                    y.applicable[0],
                    y.applicable[1],
                    y.applicable[2],
                    y.applicable[3]
                ),
            );
            a.installments = ann;
        }
        // The withholding rules matter only to withholding, and the order it
        // is applied in only when a quarter is still short at a later due
        // date.
        let (ledger, _) = self.ledger(f, &a)?;
        if all_withheld.0 > 0 {
            af.non_wage = withheld.0 > 0 && !f.elections().withholding_as_withheld();
            af.actual_dates = withheld.0 > 0 && f.elections().withholding_as_withheld();
            for q in &ledger[..ledger.len().saturating_sub(1)] {
                af.applying = af.applying || q.shortfall().0 > 0;
            }
        }
        self.diverges(&mut a, af)?;
        Ok(a)
    }

    /// Adds the divergences to `a`'s notes.
    fn diverges(&self, a: &mut Assessment, af: Affects) -> Result<(), Error> {
        let ds = self.divergences(af)?;
        a.notes.diverges(&ds);
        Ok(())
    }

    /// Applies withholding and payments to the installments as IL-2210 does.
    ///
    /// Withholding, IL-2210 lines 10a to 13: a quarter of the Illinois
    /// withholding on each due date, or by election what was withheld after
    /// the previous due date up to this one; and pass-through withholding in
    /// the quarter whose due date is the first on or after the entity's year
    /// end. Each quarter's withholding comes off that quarter's installment,
    /// and an excess carries to the next quarter, never back.
    ///
    /// Payments, Penalty Worksheet 1: estimated payments in date order, each
    /// to the oldest quarter with an unpaid amount, whatever quarter it was
    /// meant for. Last year's overpayment counts on the first due date, or on
    /// the day it was elected if that was late. A payment after every
    /// quarter is paid is not used.
    fn ledger(&self, f: &Facts, a: &Assessment) -> Result<(Vec<Quarter>, Notes), Error> {
        let mut notes = Notes::default();
        let n = a.dues.len();
        let mut withheld = vec![USCents(0); n];
        if f.elections().withholding_as_withheld() {
            for w in f.withholding() {
                withheld[quarter_of(&a.dues, w.on())] += w.amount();
            }
            notes.rule(
                "35 ILCS 5/804(g)(1); IL-2210 Instructions (R-12/25), Line 10a",
                format!(
                    "by election, backed by pay stubs or an employer letter, the {} withheld counts in the quarter in which it was withheld: {}, {}, {} and {}; IL-2210 must then be filed",
                    f.withheld_total(),
                    withheld[0],
                    withheld[1],
                    withheld[2],
                    withheld[3]
                ),
            );
        } else {
            let cum = estimated::cumulative_shares(f.withheld_total(), &QUARTERS)?;
            for (i, inc) in estimated::increments(&cum).into_iter().enumerate().take(n) {
                withheld[i] = inc;
            }
            notes.rule(
                "35 ILCS 5/804(g)(1); IL-2210 Instructions (R-12/25), Line 10a",
                format!(
                    "the {} of Illinois withholding is treated as paid in four equal parts, {} a quarter, whenever it was actually withheld",
                    f.withheld_total(),
                    withheld[0]
                ),
            );
        }
        for p in a.extras.pass_through_withholding().unwrap_or_default() {
            let i = quarter_of(&a.dues, p.year_end());
            withheld[i] += p.amount();
            notes.rule(
                "IL-2210 Instructions (R-12/25), Line 10b; 35 ILCS 5/804(g)(2)",
                format!(
                    "{} of pass-through withholding for an entity year ending {} counts in full toward the {} installment",
                    p.amount(),
                    p.year_end().long(),
                    a.dues[i].long()
                ),
            );
        }
        let mut pays = Vec::new();
        let mut prior_year = false;
        let late_answer = a.extras.overpayment_elected_late();
        let late = late_answer.unwrap_or_default();
        for p in f.payments() {
            let mut on = p.on();
            if p.from_prior_year() {
                prior_year = true;
                on = a.dues[0];
                if !late.is_zero() {
                    on = late;
                }
            }
            pays.push(Pay {
                on,
                amount: p.amount(),
            });
        }
        pays.sort_by(|x, y| x.on.compare(y.on).cmp(&0));
        if prior_year {
            if late.is_zero() {
                notes.rule(
                    "IL-2210 Instructions (R-12/25), Line 20; IL-1040-ES (R-09/26), Estimated Tax Worksheet line 11",
                    format!(
                        "last year's overpayment applied to this year counts as paid on {}, as it does when elected by the extended due date of last year's return",
                        a.dues[0].long()
                    ),
                );
            } else {
                notes.rule(
                    "IL-2210 Instructions (R-12/25), Line 20; IL-1040-ES (R-09/26), Estimated Tax Worksheet line 11",
                    format!(
                        "last year's overpayment was elected after the extended due date of last year's return, so it counts as paid on {}, the day of the election",
                        late.long()
                    ),
                );
            }
            if late_answer.is_none() {
                notes.ask(
                    PositionKind::JurisdictionFact,
                    format!(
                        "When did you elect to apply last year's overpayment to {}?",
                        self.y.year
                    ),
                    format!(
                        "It counts on {} if elected by your {} return's extended due date. Otherwise it counts on the day you elected it.",
                        short_date(a.dues[0], self.y.year),
                        self.y.year - 1
                    ),
                );
            }
        }
        let mut out = Vec::with_capacity(n);
        let mut carry = USCents(0);
        for (i, &w) in withheld.iter().enumerate() {
            let mut line13 = a.installments[i] - w - carry;
            carry = USCents(0);
            if line13.0 < 0 {
                carry = -line13;
                line13 = USCents(0);
            }
            out.push(Quarter {
                number: i as i64 + 1,
                due: a.dues[i],
                required: a.installments[i],
                withheld: w,
                unpaid: line13,
                ..Quarter::default()
            });
        }
        for p in &pays {
            let mut left = p.amount;
            for q in out.iter_mut() {
                if left.0 <= 0 {
                    break;
                }
                let owed = q.unpaid - q.pieces.iter().map(|pc| pc.amount).sum::<USCents>();
                if owed.0 <= 0 {
                    continue;
                }
                let paid = std::cmp::min(owed, left);
                q.pieces.push(Piece {
                    amount: paid,
                    paid_on: p.on,
                });
                left -= paid;
            }
        }
        let mut running_withheld = USCents(0);
        for q in out.iter_mut() {
            let owed = q.unpaid - q.pieces.iter().map(|pc| pc.amount).sum::<USCents>();
            if owed.0 > 0 {
                q.pieces.push(Piece {
                    amount: owed,
                    paid_on: Date::default(),
                });
            }
            running_withheld += q.withheld;
            q.credited_by = running_withheld;
            for p in &pays {
                if !p.on.after(q.due) {
                    q.credited_by += p.amount;
                }
            }
        }
        notes.rule(
            "IL-2210 (R-12/25), Step 2 lines 10c to 13 and Penalty Worksheet 1; IL-1040-ES (R-09/26), What if I do not make my payments?",
            "each quarter's withholding comes off that quarter's installment, with any excess carried to the next; each estimated payment then goes to the oldest quarter still unpaid, in the order paid",
        );
        Ok((out, notes))
    }
}

impl EstimatedTax for IllinoisEstimated {
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
                    "with no {} Illinois return to measure against, there is no target to schedule",
                    self.y.year - 1
                ),
            ));
        }
        let (ledger, notes) = self.ledger(f, &a)?;
        let out = schedule_of(&ledger)?;
        Ok(working::new_result(out)
            .with(&a.notes)
            .with(&notes)
            .build()?)
    }

    /// IL-2210 Penalty Worksheet 1 up to `as_of`, or to the day the return
    /// was filed and paid if that is earlier.
    ///
    /// Each amount paid late is charged once, at the low rate if it was paid
    /// within late_days of its due date and the high rate otherwise. An
    /// amount still unpaid on the stop date is charged as if paid that day:
    /// the low rate while it is within late_days, rising to the high rate
    /// after. No date stops the penalty earlier; IL-2210's worked example
    /// charges a payment made with a return filed after April 15.
    ///
    /// Interest is charged as well only when [`EstimatedFacts`] says so,
    /// because no Illinois document says whether it runs on a late
    /// installment. The penalty's total then includes it.
    fn penalty(&self, f: &Facts, as_of: Date) -> Result<working::Result<Penalty>, Error> {
        if as_of.is_zero() {
            return Err(Error::Invalid(
                "illinoistaxcalc: a penalty needs the date to figure it to".into(),
            ));
        }
        let a = self.assess(f)?;
        if !a.requirement.target_known() {
            return Err(Error::with_detail(
                Error::NeedsThisYearTax,
                format!(
                    "with no {} Illinois return to measure against, there is no target to figure a penalty from",
                    self.y.year - 1
                ),
            ));
        }
        let y = &self.y;
        let mut stop = as_of;
        let mut paid_in_full = false;
        let filed = f.return_filed();
        if !filed.is_zero() && filed.before(stop) {
            stop = filed;
            paid_in_full = true;
        }
        let waiver_answer = f.positions().waiver();
        let waiver = waiver_answer.unwrap_or_default();
        let (ledger, mut notes) = self.ledger(f, &a)?;
        let sched = schedule_of(&ledger)?;
        if a.exempt {
            let p = estimated::new_penalty(stop)
                .waiver(waiver)
                .because(a.requirement.clone(), &sched)
                .build()?;
            return Ok(working::new_result(p).with(&a.notes).build()?);
        }
        let interest_answer = a.extras.interest_on_late_installments();
        let interest = interest_answer == Some(true);
        let mut pb = estimated::new_penalty(stop);
        pb.waiver(waiver).because(a.requirement.clone(), &sched);
        if !a.tax_known {
            pb.upper_bound();
        }
        let mut unrated = Date::default();
        let (mut flat, mut accrued) = (USCents(0), USCents(0));
        for q in &ledger {
            if q.shortfall().0 <= 0 || !stop.after(q.due) {
                continue;
            }
            let mut lb = estimated::new_penalty_line(q.number, q.due, q.shortfall());
            let mut last_day = q.due;
            for pc in &q.pieces {
                let mut to = stop;
                if !pc.paid_on.is_zero() && pc.paid_on.before(stop) {
                    to = pc.paid_on;
                }
                let Some(part) = y.flat_part(pc.amount, q.due, to)? else {
                    continue;
                };
                flat += part.charge();
                lb.part(part);
                if to.after(last_day) {
                    last_day = to;
                }
                if interest {
                    let (parts, gap) = estimated::simple_daily(pc.amount, q.due, to, &y.interest)?;
                    if !gap.is_zero() && (unrated.is_zero() || gap.before(unrated)) {
                        unrated = gap;
                    }
                    for p in parts {
                        accrued += p.charge();
                        lb.part(p);
                    }
                }
            }
            pb.line(lb.days(last_day.days_since(q.due)).build()?);
        }
        let mut p = pb.build()?;
        if paid_in_full {
            notes.rule(
                "IL-2210 Instructions (R-12/25), Penalty Worksheet 1, Column F",
                format!(
                    "the return was filed with the whole balance paid on {}, so every amount still unpaid was paid that day",
                    stop.long()
                ),
            );
        }
        notes.rule(
            "35 ILCS 5/804(a); 35 ILCS 735/3-3(b-25)(1); IL-2210 (R-12/25), Penalty Worksheet 1",
            format!(
                "each amount paid after its due date is charged once, {} if paid 1 to {} days late and {} if paid later; an amount unpaid on {} is charged as of that day; the penalty is {flat}, which line 22 rounds to whole dollars for Form IL-1040 line 34",
                y.low,
                y.late_days,
                y.high,
                stop.long()
            ),
        );
        if !p.lines().is_empty() {
            if interest {
                notes.rule(
                    "35 ILCS 735/3-2; Publication 103 (R-09/25), Interest; IDOR, Interest Rates",
                    format!(
                        "as asked, simple interest at the uniform rate is charged on each late amount for the days after its due date until paid, divided by the days in the year: {accrued}, included in the total"
                    ),
                );
            }
            if interest_answer.is_none() {
                notes.ask(
                    PositionKind::JurisdictionFact,
                    "Do you want interest on a late quarter counted on top of the penalty?",
                    "Nothing Illinois publishes says if a late quarter bears interest. The Illinois penalty form figures none.",
                );
            }
            if waiver_answer.is_none() {
                notes.ask(
                    PositionKind::PenaltyWaiver,
                    "Do you claim reasonable cause, like a casualty or a federal disaster extension?",
                    "A casualty counts if it destroyed your home or records. Until Illinois lifts the penalty, the figures include it in full.",
                );
            }
        }
        if !a.tax_known {
            notes.rule(
                "35 ILCS 5/804(c)(1)(B); IL-2210 (R-12/25), line 7",
                format!(
                    "this year's Illinois tax was not given, so the penalty is figured against 100% of the {} tax alone; the required payment is the lesser of that and 90% of this year's tax, so {} is the most the penalty can be, and it is less if this year's tax turns out lower",
                    y.year - 1,
                    p.total()
                ),
            );
        }
        if !unrated.is_zero() {
            p = p.to_builder().rated_through(unrated.add_days(-1)).build()?;
            notes.ask(
                PositionKind::UnpublishedRate,
                "",
                format!(
                    "The rate for the days after {} isn't published yet.",
                    short_date(unrated.add_days(-1), y.year)
                ),
            );
        }
        if let Some(o) = waiver_outcome(f)
            && p.total().0 > 0
        {
            p = p.to_builder().waiver_outcome(o).build()?;
        }
        match waiver {
            WaiverKind::Casualty => {
                notes.rule(
                    "35 ILCS 5/804(e); 86 Ill. Adm. Code 700.400(e)(5)",
                    "Illinois has no casualty waiver as such; a casualty can be reasonable cause, which the department decides on a detailed explanation",
                );
            }
            WaiverKind::RetiredOrDisabled => {
                notes.rule(
                    "35 ILCS 5/804(e); 86 Ill. Adm. Code 100.8010(g)",
                    "Illinois has no exception for retirement or disability; the only route is reasonable cause, which the department decides",
                );
            }
            WaiverKind::None => {}
        }
        Ok(working::new_result(p).with(&a.notes).with(&notes).build()?)
    }

    fn explanation(&self) -> Explanation {
        self.explanation.clone()
    }
}

/// Written by hand and checked against research/estimated-payments/il.md.
fn illinois_explanation() -> Result<Explanation, Error> {
    estimated::new_explanation(
        "Illinois expects estimated payments if more than $1,000 will be due for 2026. The amount due is your tax after credits and withholding. A late amount costs a flat 2% if paid within 30 days, and 10% after that.",
    )
    .paragraph("To avoid the penalty, withholding and estimated payments must reach a target. It is the lower of 90% of this year's tax after credits or 100% of last year's. There is no higher prior-year figure at high income. Last year's figure counts only if you filed a full-year return that showed tax.")
    .paragraph("The payments are four equal quarters. They are due April 15, June 15 and September 15, 2026, and January 15, 2027. A date on a weekend or holiday shifts to the next business day. Withholding is treated as paid in four equal parts. If pay stubs or an employer letter show when it was taken, it counts then. A partnership, S corporation or trust may withhold for you. That withholding counts in the quarter the entity's year ended. Each payment goes to the oldest unpaid quarter first.")
    .paragraph("The penalty is not interest. Each late amount is charged once. The charge is 2% within 30 days of the due date, and 10% if paid later or never. A week late costs little, but 31 days late costs as much as never paying. The department will work out the penalty and bill you if you prefer. Nothing it publishes says whether it also charges interest on a late quarter. Its penalty form works out none.")
    .paragraph("Tax after credits and withholding of $1,000 or less this year means no penalty. There's also no penalty if you did not have to file last year. The same holds if last year's return showed no tax due after withholding. Uneven income can lower the early quarters through the annualised method. That method uses 22.5%, 45%, 67.5% and 90% of the tax on your income to date. Otherwise the department lifts the penalty only for reasonable cause. A fire that destroyed your records is one example.")
    .versus_federal("Illinois charges a flat 2% or 10% of each late amount, not interest by the day. Past 30 days, the cost is the same however late you pay. There's no 110% figure at high income. Filing by January 31 doesn't waive the January quarter.")
    .source("Illinois Income Tax Act §§ 803, 804, 806 (35 ILCS 5/803, 5/804, 5/806); Uniform Penalty and Interest Act §§ 3-2, 3-3, 3-8 (35 ILCS 735)")
    .source("IL-1040-ES, Estimated Income Tax Payments for Individuals, 2026 (R-09/26)")
    .source("IL-2210, Computation of Penalties for Individuals, and its instructions, 2025 (R-12/25)")
    .source("Publication 103, Penalties and Interest for Illinois Taxes (R-09/25)")
    .source("86 Ill. Adm. Code 100.8000, 100.8010, 700.220, 700.400")
    .build()
}
