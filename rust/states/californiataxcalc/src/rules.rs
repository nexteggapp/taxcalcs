//! California's estimated tax rules, Rev. & Tax. Code § 19136 and the
//! sections after it, as the shared [`estimated::EstimatedTax`] interface.
//! Ported from estimated.go.
//!
//! § 19136(a) adopts 26 U.S.C. § 6654 except as California provides
//! otherwise, and California provides otherwise often enough that nothing
//! here borrows from the federal crate: the installments are 30%, 40%, 0%
//! and 30%; the threshold is $500; there is no prior-year test at $1,000,000
//! of AGI; the annualised percentages differ; and the rate is set
//! half-yearly.
//!
//! Where the Franchise Tax Board's forms and the statute disagree, the
//! figure follows the form and the result's divergences say what the statute
//! says.

use std::any::Any;

use estimated::{
    Basis, Calendar, Date, Error, EstimatedTax, Explanation, Facts, Installment, LedgerEntry,
    Penalty, RateSeries, Requirement, TargetOptionBuilder,
};
use filing::Status as FilingStatus;
use money::{Rate, USCents, percent};
use working::{Divergence, Notes, PositionKind, Reading};

use crate::worksheet_i::{WorksheetIColumn, worksheet_i};

/// The facts California's rules read that no other jurisdiction does. Carry
/// them in [`estimated::Facts::state`].
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct EstimatedFacts {
    group_return: bool,
    worksheet_i: Vec<WorksheetIColumn>,
}

impl EstimatedFacts {
    /// A Form 540NR filer's four FTB 5805 Worksheet I columns, for the
    /// annualised method, or none.
    pub fn worksheet_i(&self) -> Vec<WorksheetIColumn> {
        self.worksheet_i.clone()
    }

    /// Reports a nonresident electing to be included in a group return under
    /// § 18537, whom § 19136(j) takes out of the penalty from 2026.
    pub fn group_return(&self) -> bool {
        self.group_return
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
    /// Marks a nonresident who elects to file in a group return.
    pub fn group_return(&mut self) -> &mut Self {
        self.e.group_return = true;
        self
    }

    /// Sets a Form 540NR filer's Worksheet I columns, one for each
    /// annualisation period.
    pub fn worksheet_i(&mut self, columns: &[WorksheetIColumn]) -> &mut Self {
        self.e.worksheet_i = columns.to_vec();
        self
    }

    /// Refuses Worksheet I with other than four columns.
    pub fn build(&self) -> Result<EstimatedFacts, Error> {
        let e = self.e.clone();
        let n = e.worksheet_i.len();
        if n != 0 && n != 4 {
            return Err(Error::Invalid(format!(
                "californiataxcalc: Worksheet I has four columns, not {n}"
            )));
        }
        Ok(e)
    }
}

/// A part-year resident: some California residence, but not for the whole
/// year.
fn moved_during_year(f: &Facts) -> bool {
    for p in f.residence() {
        if p.state() == estimated::CALIFORNIA.as_str() {
            return files_540nr(f);
        }
    }
    false
}

/// A part-year resident on the regular method, whose target and
/// installments FTB 5805 doesn't change for the move.
fn regular_part_year(f: &Facts) -> bool {
    moved_during_year(f) && !f.elections().annualised()
}

/// The sentence a part-year resident's reason ends with when last year's
/// tax sets the target: FTB 5805 Part II line 5 takes it as filed.
fn prior_note(f: &Facts, chosen: Basis) -> &'static str {
    if regular_part_year(f) && chosen == Basis::PriorYearTax {
        return " California doesn't reduce last year's tax for the months you lived elsewhere.";
    }
    ""
}

/// What a part-year resident's penalty reason adds: FTB 5805 keeps the
/// January installment after a move.
const PART_YEAR_CONTEXT: &str = "The January payment still counts after a move.";

/// A nonresident or part-year resident: residence was given, and none of it
/// is California for the whole year.
fn files_540nr(f: &Facts) -> bool {
    let residence = f.residence();
    !residence.is_empty()
        && !estimated::resident_all_year(&residence, &estimated::CALIFORNIA, f.tax_year())
}

/// One tax year's published figures.
pub(crate) struct EstimatedYear {
    pub(crate) year: i64,

    /// What tax less withholding must reach before any penalty applies:
    /// $500, or $250 for a separate return.
    pub(crate) threshold: USCents,
    pub(crate) threshold_separate: USCents,

    /// The prior-year California AGI above which the prior-year test is
    /// 110%, and the current-year California AGI at or above which there is
    /// no prior-year test at all.
    pub(crate) prior_year_step: USCents,
    pub(crate) prior_year_step_separate: USCents,
    pub(crate) high_income: USCents,
    pub(crate) high_income_separate: USCents,

    pub(crate) dues: Vec<Date>,
    pub(crate) calendar: Calendar,
    pub(crate) file_and_pay_by: Date,
    pub(crate) stop: Date,
    pub(crate) period_ends: Vec<Date>,
    pub(crate) rates: RateSeries,

    /// § 19136(j), operative from 2026.
    pub(crate) group_return_exempt: bool,
}

/// § 19136.1(a): 30%, 40%, nothing, and 30% of the required annual payment.
/// § 19136(e)(2)(A) deems withholding paid in the same shares.
fn installment_shares() -> [Rate; 4] {
    [percent(30), percent(40), Rate(0), percent(30)]
}

/// § 19136.1(b), FTB 5805 Part III line 15.
fn annualised_percentages() -> [Rate; 4] {
    [percent(27), percent(63), percent(63), percent(90)]
}

/// The year this build carries, or `None`.
fn estimated_year(year: i64) -> Option<Result<EstimatedYear, Error>> {
    match year {
        2026 => Some(crate::estimated_y2026::year_2026()),
        _ => None,
    }
}

/// Returns California's estimated tax rules for a tax year, or
/// [`Error::NoRules`] for a year this build does not carry.
pub fn estimated_tax(year: i64) -> Result<Box<dyn EstimatedTax>, Error> {
    match estimated_year(year) {
        Some(y) => Ok(Box::new(CaliforniaEstimated { y: y? })),
        None => Err(estimated::no_rules(&estimated::CALIFORNIA, year)),
    }
}

struct CaliforniaEstimated {
    y: EstimatedYear,
}

#[derive(Default)]
struct Assessment {
    requirement: Requirement,
    exempt: bool,
    tax_known: bool,
    installments: Vec<USCents>,
    excused: bool,
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

fn separate(s: FilingStatus) -> bool {
    s == FilingStatus::MarriedFilingSeparately
}

/// A question whose answer can change the target. It is asked only when no
/// exception sets the target.
struct TargetQuestion {
    position: PositionKind,
    question: String,
    effect: String,
}

#[derive(Default)]
struct TargetQuestions(Vec<TargetQuestion>);

impl TargetQuestions {
    fn ask(&mut self, position: PositionKind, question: String, effect: String) {
        self.0.push(TargetQuestion {
            position,
            question,
            effect,
        });
    }

    /// Adds the questions to `notes` unless an exception set the target.
    fn ask_unless_exempt(self, notes: &mut Notes, exempt: bool) {
        if exempt {
            return;
        }
        for t in self.0 {
            notes.ask(t.position, t.question, t.effect);
        }
    }
}

/// Words an amount under `limit`: "leaves $742, under $1,000", "leaves just
/// under $1,000" when whole dollars would show the two as equal, or "leaves
/// a $136 refund" below zero. `past` uses "left".
fn left_under(past: bool, amount: USCents, limit: USCents) -> String {
    let (verb, words): (&str, fn(USCents) -> String) = if past {
        ("left", estimated::left)
    } else {
        ("leaves", estimated::leaves)
    };
    if amount.0 < 0 {
        return words(amount);
    }
    if amount.whole() == limit.whole() {
        return format!("{verb} just under {}", limit.whole());
    }
    format!("{}, under {}", words(amount), limit.whole())
}

impl CaliforniaEstimated {
    fn extras(&self, f: &Facts) -> Result<EstimatedFacts, Error> {
        match f.state() {
            None => Ok(EstimatedFacts::default()),
            Some(s) => match s.downcast_ref::<EstimatedFacts>() {
                Some(e) => Ok(e.clone()),
                None => Err(invalid(format!(
                    "californiataxcalc: the facts carry {}, not californiataxcalc.EstimatedFacts",
                    type_name(s)
                ))),
            },
        }
    }

    fn check(&self, f: &Facts) -> Result<(), Error> {
        if f.jurisdiction() != estimated::CALIFORNIA {
            return Err(invalid(format!(
                "californiataxcalc: these are California's rules, and the facts are for {}",
                f.jurisdiction()
            )));
        }
        if f.tax_year() != self.y.year {
            return Err(invalid(format!(
                "californiataxcalc: these are the {} rules, and the facts are for {}",
                self.y.year,
                f.tax_year()
            )));
        }
        if f.farmer_or_fisher() {
            return Err(Error::with_detail(
                Error::Unsupported,
                "farmers and fishermen make one payment by January 15 under FTB 5805F",
            ));
        }
        if f.elections().annualised() {
            for (i, p) in f.periods().iter().enumerate() {
                if p.end() != self.y.period_ends[i] {
                    return Err(invalid(format!(
                        "californiataxcalc: FTB 5805 Part III period {} ends on {}, not {}",
                        i + 1,
                        self.y.period_ends[i],
                        p.end()
                    )));
                }
            }
        }
        Ok(())
    }

    /// The places the FTB's forms and the Revenue and Taxation Code part ways
    /// on a rule every California computation uses. They are found in
    /// research/estimated-payments/ca.md and checked there against both.
    fn divergences(&self, extras: &EstimatedFacts) -> Result<Vec<Divergence>, Error> {
        // The FTB 5805 Question 3 wording on withholding "not withheld in
        // equal installments" is not a divergence: Worksheet II and §
        // 19136(e)(2)(A) both credit it 30%, 40%, 0% and 30%, and
        // research/estimated-payments/ca.md finds only the question's
        // wording loose, not the rule.
        Ok(vec![
            working::new_divergence("Nonresidents in a group return")
                .form("This exception isn't in the 2026 Form 540-ES instructions or the 2025 FTB 5805. It reaches only a nonresident in a group return, not a single resident.",
                    "2026 Instructions for Form 540-ES; FTB 5805 (2025) and its instructions")
                .statute("From 2026, a nonresident who elects to join a group return has no penalty.",
                    "Cal. Rev. & Tax. Code § 19136(j), added by Stats. 2025, ch. 73 (AB 1518); FTB bill analysis of AB 1518")
                .uses(Reading::Statute)
                .affects(extras.group_return && self.y.group_return_exempt)
                .build()?,
            working::new_divergence("Underpayments caused by the FTB")
                .form("The waivers listed cover a casualty or disaster, and retirement or disability. An underpayment the FTB caused isn't mentioned.",
                    "FTB 5805 (2025) instructions, General Information C")
                .statute("An FTB levy, processing action or collection action can be made in error. There's no penalty to the extent it created or increased the underpayment.",
                    "Cal. Rev. & Tax. Code § 19136.7(a)")
                // Nothing in the facts says the FTB caused an underpayment.
                .uses(Reading::Form)
                .affects(false)
                .build()?,
        ])
    }

    fn assess(&self, f: &Facts) -> Result<Assessment, Error> {
        self.check(f)?;
        let extras = self.extras(f)?;
        let y = &self.y;
        let mut a = Assessment::default();
        let mut target_qs = TargetQuestions::default();
        let divergences = self.divergences(&extras)?;
        a.notes.diverges(&divergences);

        let (threshold, step, high) = if separate(f.filing_status()) {
            (
                y.threshold_separate,
                y.prior_year_step_separate,
                y.high_income_separate,
            )
        } else {
            (y.threshold, y.prior_year_step, y.high_income)
        };
        let tax_opt = f.this_year().tax();
        let tax_known = tax_opt.is_some();
        let tax = tax_opt.unwrap_or_default();
        a.tax_known = tax_known;
        let withheld = f.withheld_total();
        let prior = f.prior_year();
        let has_prior = prior.is_some();
        let last = y.year - 1;

        // 90% of this year's tax. § 6654(d)(1)(B)(i), adopted by § 19136(a).
        let this_year = if tax_known {
            estimated::new_target_option(Basis::ThisYearTax, percent(90), percent(90).of(tax))
                .build()?
        } else {
            estimated::new_target_option(Basis::ThisYearTax, percent(90), USCents(0))
                .unavailable(estimated::NEEDS_THIS_YEAR_TAX)
                .build()?
        };
        if tax_known {
            a.notes.rule(
                "Cal. Rev. & Tax. Code § 19136(a); 26 U.S.C. § 6654(d)(1)(B)(i)",
                format!(
                    "90% of this year's California tax of {} is {}",
                    tax,
                    this_year.amount()
                ),
            );
        }

        // Last year's tax: 100%, 110% above the step, and none at all at high
        // income this year.
        let prior_tax = prior
            .and_then(|p| p.figures())
            .and_then(|pf| pf.tax())
            .unwrap_or_default();
        let this_agi_opt = f.this_year().state_agi();
        let unavailable = |why: String| -> TargetOptionBuilder {
            let mut b = estimated::new_target_option(Basis::PriorYearTax, percent(100), USCents(0));
            b.unavailable(why);
            b
        };
        let prior_option = match prior {
            None => {
                target_qs.ask(
                    PositionKind::PriorYearReturn,
                    format!("What was the total tax on your {last} California return?"),
                    "With it, last year's California tax could set your target, not this year's."
                        .into(),
                );
                unavailable(format!("Needs your {last} California return"))
            }
            Some(p) if !p.filed() => {
                unavailable(format!("You didn't file a {last} California return"))
            }
            Some(p) if p.months() != 12 => unavailable(format!(
                "Your {last} California return covered {} months, and this test needs all 12",
                p.months()
            )),
            Some(_) if this_agi_opt.is_some_and(|agi| agi >= high) => {
                let this_agi = this_agi_opt.unwrap_or_default();
                a.notes.rule(
                    "Cal. Rev. & Tax. Code § 19136.3(a)",
                    format!(
                        "California AGI of {this_agi} is at least {high}, so the prior-year test is not available"
                    ),
                );
                unavailable(format!(
                    "Not available at {} or more of California AGI",
                    high.whole()
                ))
            }
            Some(_) if this_agi_opt.is_none() => {
                target_qs.ask(
                    PositionKind::AdjustedGrossIncome,
                    format!("What do you expect your {} California AGI to be?", y.year),
                    format!(
                        "If it's under {}, last year's tax could set your target.",
                        high.whole()
                    ),
                );
                unavailable(format!(
                    "Needs your {} California AGI, as the test is closed at {} or more",
                    y.year,
                    high.whole()
                ))
            }
            Some(p) => {
                let pf = p.figures().unwrap_or_default();
                let prior_agi_opt = pf.state_agi();
                let prior_agi = prior_agi_opt.unwrap_or_default();
                let mut rate = percent(100);
                match prior_agi_opt {
                    None => {
                        rate = percent(110);
                        target_qs.ask(
                            PositionKind::AdjustedGrossIncome,
                            format!("What was your {last} California AGI?"),
                            format!(
                                "Until then, the target is 110% of last year's tax. At {} or less it would be 100%.",
                                step.whole()
                            ),
                        );
                    }
                    Some(agi) if agi > step => rate = percent(110),
                    Some(_) => {}
                }
                let amount = rate.of(prior_tax);
                let why = if prior_agi_opt.is_none() {
                    format!("{last} California AGI was not given")
                } else if rate == percent(110) {
                    format!("{last} California AGI of {prior_agi} is more than {step}")
                } else {
                    format!("{last} California AGI of {prior_agi} is not more than {step}")
                };
                a.notes.rule(
                    "Cal. Rev. & Tax. Code § 19136(a); 26 U.S.C. § 6654(d)(1)(B)(ii), (C)",
                    format!(
                        "{why}, so the prior-year test is {rate} of the {prior_tax} tax on the {last} return, or {amount}"
                    ),
                );
                estimated::new_target_option(Basis::PriorYearTax, rate, amount)
            }
        };
        let prior_opt = prior_option.build()?;

        let mut rb = estimated::new_requirement();
        rb.option(this_year.clone()).option(prior_opt.clone());
        let (mut chosen, mut target) = (Basis::ThisYearTax, this_year.amount());
        if prior_opt.available() && (!tax_known || prior_opt.amount() < target) {
            (chosen, target) = (Basis::PriorYearTax, prior_opt.amount());
        }
        if !tax_known {
            target_qs.ask(
                PositionKind::ThisYearTax,
                format!("What do you expect your {} California tax to be?", y.year),
                estimated::without_this_year_tax(prior_opt.available()),
            );
        }

        // The exceptions that mean no penalty can arise.
        let no_liability = prior.is_some_and(|p| {
            p.months() == 12 && ((p.filed() && prior_tax.0 == 0) || p.not_required_to_file())
        }) && has_prior;
        let (mut prior_small, mut prior_small_known) = (false, false);
        let mut prior_withheld = USCents(0);
        if let Some(p) = prior
            && p.filed()
        {
            let pf = p.figures().unwrap_or_default();
            if let Some(w) = pf.withheld() {
                prior_withheld = w;
                (prior_small, prior_small_known) = (prior_tax - w < threshold, true);
            }
            if !prior_small_known && !no_liability {
                a.notes.ask(
                    PositionKind::PriorYearReturn,
                    format!("How much California tax was withheld for you in {last}?"),
                    format!(
                        "Under {} of last year's tax after withholding means no estimated payments.",
                        threshold.whole()
                    ),
                );
            }
        }

        let note = prior_note(f, chosen);
        if extras.group_return && y.group_return_exempt {
            a.exempt = true;
            let group = estimated::new_exception("Nonresident in a group return").build()?;
            rb.required(
                false,
                format!(
                    "You're a nonresident included in a group return. From {}, California charges no penalty for that. No estimated payments are needed.",
                    y.year
                ),
            )
            .exempt(group);
            a.notes.rule(
                "Cal. Rev. & Tax. Code § 19136(j)",
                format!(
                    "a nonresident electing a group return under § 18537 is not subject to the penalty for {}",
                    y.year
                ),
            );
        } else if no_liability {
            a.exempt = true;
            let none = estimated::new_exception("No California tax liability last year").build()?;
            rb.required(
                false,
                format!(
                    "You had no California tax liability for all of {last}. No estimated payments are needed for {}.",
                    y.year
                ),
            )
            .exempt(none);
            a.notes.rule(
                "Cal. Rev. & Tax. Code § 19136(a); 26 U.S.C. § 6654(e)(2); FTB 5805 (2025)",
                format!(
                    "the {last} return showed no liability for a full 12 months, so there is no penalty for {}",
                    y.year
                ),
            );
        } else if tax_known && tax - withheld < threshold {
            a.exempt = true;
            let under =
                estimated::exception_below("Tax less withholding", tax - withheld, threshold)?;
            let why = if withheld.0 == 0 {
                format!(
                    "You have no California withholding, and your California tax is {}. That's under {}, so no estimated payments are needed.",
                    tax.whole(),
                    threshold.whole()
                )
            } else if withheld >= tax {
                format!(
                    "Your California withholding of {} covers your {} California tax. No estimated payments are needed.",
                    withheld.whole(),
                    tax.whole()
                )
            } else {
                format!(
                    "Your California tax is {}, and {} was withheld. That {}. No estimated payments are needed.",
                    tax.whole(),
                    withheld.whole(),
                    left_under(false, tax - withheld, threshold)
                )
            };
            rb.required(false, why).exempt(under);
            a.notes.rule(
                "Cal. Rev. & Tax. Code § 19136(c)(2); FTB 5805 (2025), Part II line 4",
                format!(
                    "{} of tax less {} withheld is {}, under {}, so there is no penalty",
                    tax,
                    withheld,
                    tax - withheld,
                    threshold
                ),
            );
        } else if prior_small {
            a.exempt = true;
            let under = estimated::exception_below(
                "Last year's tax less withholding",
                prior_tax - prior_withheld,
                threshold,
            )?;
            rb.required(
                false,
                format!(
                    "Your {last} California tax was {}, and {} was withheld. That {}. California charges no penalty when either year's tax is under {}. No estimated payments are needed.",
                    prior_tax.whole(),
                    prior_withheld.whole(),
                    left_under(true, prior_tax - prior_withheld, threshold),
                    threshold.whole()
                ),
            )
            .exempt(under);
            a.notes.rule(
                "Cal. Rev. & Tax. Code § 19136(c)(2); FTB 5805 (2025)",
                format!(
                    "the {last} tax less credits and withholding was under {threshold}, so there is no penalty for {}",
                    y.year
                ),
            );
        } else if !tax_known && !prior_opt.available() {
            rb.required(
                false,
                format!(
                    "Your target is 90% of your {} California tax. Where that test is open, it may instead be 100% of your {last} tax. Without one of those figures, there's no target to pay toward yet.",
                    y.year
                ),
            )
            .no_target();
        } else if !tax_known && withheld >= target {
            rb.required(
                false,
                format!(
                    "Your California withholding of {} reaches {} of your {last} tax, {}. No estimated payments are needed, whatever your {} tax is.{note}",
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
                    "Your target is {} of your {last} California tax, {}. After {} of withholding, that's {} of estimated payments. That keeps you clear of a penalty whatever your {} tax turns out to be.{note}",
                    prior_opt.percent(),
                    target.whole(),
                    withheld.whole(),
                    (target - withheld).whole(),
                    y.year
                ),
            )
            .choose(chosen);
        } else if withheld >= target {
            rb.required(
                false,
                format!(
                    "Your California withholding of {} already reaches your {} target. No estimated payments are needed.{note}",
                    withheld.whole(),
                    target.whole()
                ),
            )
            .choose(chosen);
        } else if withheld.0 == 0 {
            rb.required(
                true,
                format!(
                    "You have no California withholding, and your California tax is {}. That's {} or more, and your target is {}. You need estimated payments of {} across the year.{note}",
                    tax.whole(),
                    threshold.whole(),
                    target.whole(),
                    target.whole()
                ),
            )
            .choose(chosen);
        } else {
            rb.required(
                true,
                format!(
                    "Your California tax of {} less {} withheld leaves {}. That's {} or more, and your target is {}. You need {} of estimated payments across the year.{note}",
                    tax.whole(),
                    withheld.whole(),
                    (tax - withheld).whole(),
                    threshold.whole(),
                    target.whole(),
                    (target - withheld).whole()
                ),
            )
            .choose(chosen);
        }
        a.requirement = rb.build()?;
        target_qs.ask_unless_exempt(&mut a.notes, a.exempt);

        let cumulative =
            estimated::cumulative_shares(a.requirement.target(), &installment_shares())?;
        a.installments = estimated::increments(&cumulative);
        let dues = self.dues();
        a.notes.rule(
            "Cal. Rev. & Tax. Code § 19136.1(a); 2026 Instructions for Form 540-ES, Section D",
            format!(
                "30% of {}, {}, is due {}; 40%, {}, is due {}; nothing is due {}; and 30%, {}, is due {}",
                a.requirement.target(),
                a.installments[0],
                dues[0].long(),
                a.installments[1],
                dues[1].long(),
                dues[2].long(),
                a.installments[3],
                dues[3].long()
            ),
        );

        if !extras.worksheet_i().is_empty() && !files_540nr(f) {
            return Err(invalid(
                "californiataxcalc: Worksheet I is for a Form 540NR filer, and the residence shows a full-year California resident or none".into(),
            ));
        }
        if files_540nr(f) {
            a.notes.rule(
                "FTB 5805 (2025) instructions, Part II, lines 1 and 5",
                "a Form 540NR filer's required annual payment is figured from the tax on Form 540NR, with no further proration; Worksheet I prorates only the annualised method",
            );
        }
        if f.elections().annualised() && !a.exempt {
            let mut taxes: Vec<USCents> = f.periods().iter().map(|p| p.annualised_tax()).collect();
            if files_540nr(f) {
                let cols = extras.worksheet_i();
                if cols.len() == 4 {
                    for (i, c) in cols.iter().enumerate() {
                        let w = worksheet_i(i as i64, *c)?;
                        taxes[i] = w.line14c();
                        a.notes.rule(
                            "FTB 5805 (2025) instructions, Worksheet I and Form 540NR Instructions for Part III",
                            format!(
                                "period {}: California AGI annualised to {}, {} of it taxable after the prorated deduction, {} of prorated tax after exemption credits (line P), and {} after credits and other tax (Part III line 14c)",
                                i + 1,
                                w.f(),
                                w.k(),
                                w.p(),
                                w.line14c()
                            ),
                        );
                    }
                    a.notes.ask(
                        PositionKind::UnsettledRule,
                        "",
                        "Worksheet I doesn't say how its rates on lines C, H and N round. The figures keep them exact and round each amount to the cent.",
                    );
                } else {
                    a.notes.ask(
                        PositionKind::JurisdictionFact,
                        "What are your FTB 5805 Worksheet I figures for each period?",
                        "Until then, each period's annualised tax isn't prorated to California.",
                    );
                }
            }
            let ann = estimated::annualised(&taxes, &annualised_percentages(), &a.installments)?;
            a.notes.rule(
                "Cal. Rev. & Tax. Code § 19136.1(b); FTB 5805 (2025), Part III",
                format!(
                    "under the annualised method the installments are {}, {}, {} and {}: 27%, 63%, 63% and 90% of the annualised tax, \
                     less earlier installments, and never more than the regular 30%, 40%, 0% and 30% plus any shortfall carried forward",
                    ann[0], ann[1], ann[2], ann[3]
                ),
            );
            a.installments = ann;
        }

        // Filing and paying by January 31st excuses the last installment.
        // January 31, 2027 is a Sunday; the 540-ES prints January 31 and says
        // nothing of moving it, so it is not moved here.
        let filed = f.return_filed();
        if !filed.is_zero() && !filed.after(y.file_and_pay_by) && !a.exempt {
            a.excused = true;
            a.notes.rule(
                "2026 Instructions for Form 540-ES, Section D; 26 U.S.C. § 6654(h)",
                format!(
                    "the return was filed and paid on {}, by {}, so the January installment is excused",
                    filed.long(),
                    y.file_and_pay_by.long()
                ),
            );
        }
        if a.exempt {
            a.installments = vec![USCents(0); dues.len()];
        }
        if regular_part_year(f) && !a.exempt {
            a.notes.ask(
                PositionKind::JurisdictionFact,
                "Did your income come unevenly through the year?",
                "If so, the annualised method with FTB 5805 Worksheet I can lower your payments.",
            );
        }
        Ok(a)
    }

    fn dues(&self) -> Vec<Date> {
        estimated::move_dates(&self.y.calendar, &self.y.dues)
    }

    fn ledger(&self, f: &Facts, a: &Assessment) -> Result<(Vec<LedgerEntry>, Notes), Error> {
        let dues = self.dues();
        let credits = if f.elections().withholding_as_withheld() {
            estimated::credits_as_withheld(f, &dues)?
        } else {
            estimated::credits(f, &dues, &installment_shares())?
        };
        let mut required = a.installments.clone();
        if a.excused {
            required[3] = USCents(0);
        }
        let ledger = estimated::apply(&dues, &required, &credits)?;
        let mut notes = Notes::default();
        if f.elections().withholding_as_withheld() {
            notes.rule(
                "FTB 5805 (2025), Part I, Question 3",
                format!(
                    "by election, the {} withheld is credited on the dates it was withheld",
                    f.withheld_total()
                ),
            );
        } else {
            notes.rule(
                "Cal. Rev. & Tax. Code § 19136(e)(2)(A); FTB 5805 (2025) Worksheet II, line 2",
                format!(
                    "the {} withheld is treated as paid 30%, 40%, 0% and 30% on the due dates, whenever it was actually withheld",
                    f.withheld_total()
                ),
            );
        }
        Ok((ledger, notes))
    }
}

/// The installments from the ledger, with the January installment excused
/// when filing early excused it. September needs nothing of its own, but
/// 70% of the year is needed by it, so it is met or short.
fn schedule_of(a: &Assessment, ledger: &[LedgerEntry]) -> Result<Vec<Installment>, Error> {
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
    Ok(out)
}

impl EstimatedTax for CaliforniaEstimated {
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
                    "with no {} California return to measure against, there is no target to schedule",
                    self.y.year - 1
                ),
            ));
        }
        let (ledger, notes) = self.ledger(f, &a)?;
        let out = schedule_of(&a, &ledger)?;
        Ok(working::new_result(out)
            .with(&a.notes)
            .with(&notes)
            .build()?)
    }

    fn penalty(&self, f: &Facts, as_of: Date) -> Result<working::Result<Penalty>, Error> {
        if as_of.is_zero() {
            return Err(invalid(
                "californiataxcalc: a penalty needs the date to figure it to".into(),
            ));
        }
        let a = self.assess(f)?;
        if !a.requirement.target_known() {
            return Err(Error::with_detail(
                Error::NeedsThisYearTax,
                format!(
                    "with no {} California return to measure against, there is no target to figure a penalty from",
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
        let waiver_answered = f.positions().waiver();
        let waiver = waiver_answered.unwrap_or_default();
        let (ledger, mut notes) = self.ledger(f, &a)?;
        let sched = schedule_of(&a, &ledger)?;
        if a.exempt {
            let p = estimated::new_penalty(stop)
                .waiver(waiver)
                .because(a.requirement.clone(), &sched)
                .build()?;
            return Ok(working::new_result(p).with(&a.notes).build()?);
        }

        let (mut p, unrated) =
            estimated::simple_penalty(&ledger, stop, &y.rates, waiver, &a.requirement, &sched)?;
        if regular_part_year(f) {
            p = p.to_builder().context(PART_YEAR_CONTEXT).build()?;
        }
        if !a.tax_known {
            p = p.to_builder().upper_bound().build()?;
            notes.rule(
                "Cal. Rev. & Tax. Code § 19136(a); 26 U.S.C. § 6654(d)(1)(B)",
                format!(
                    "this year's California tax was not given, so the penalty is figured against {} of the {} tax alone; \
                     the required payment is the lesser of that and 90% of this year's tax, so {} is the most the penalty can be, and it is less if this year's tax turns out lower",
                    a.requirement.options()[1].percent(),
                    y.year - 1,
                    p.total()
                ),
            );
        }
        if paid_in_full {
            notes.rule(
                "Cal. Rev. & Tax. Code § 19136(a); 26 U.S.C. § 6654(b)(2); FTB 5805 (2025) Worksheet II",
                format!(
                    "the return was filed with the whole balance paid on {}, so every shortfall still outstanding was paid that day",
                    stop.long()
                ),
            );
        }
        notes.rule(
            "Cal. Rev. & Tax. Code §§ 19136(b), 19521; FTB 5805 (2025) Worksheet II, Part II",
            format!(
                "each shortfall is charged simple interest at the FTB's half-yearly rate, by the day, from its due date until paid or {}, \
                 with each payment applied to the oldest shortfall first; the penalty to {} is {}",
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
        if p.total().0 > 0 && waiver_answered.is_none() {
            notes.ask(
                PositionKind::PenaltyWaiver,
                "Are you asking the FTB to waive the penalty?",
                "The FTB can waive it for a casualty, disaster or other unusual event. Retiring after 62 or disability counts with reasonable cause; then none is due.",
            );
        }
        if let Some(o) = estimated::standard_waiver(f, "the Franchise Tax Board", 62)
            && p.total().0 > 0
        {
            p = p.to_builder().waiver_outcome(o).build()?;
        }
        Ok(working::new_result(p).with(&a.notes).with(&notes).build()?)
    }

    fn explanation(&self) -> Explanation {
        california_explanation()
    }
}

/// Written by hand and checked against research/estimated-payments/ca.md.
fn california_explanation() -> Explanation {
    estimated::new_explanation(
        "California follows the federal idea of paying tax through the year. Payments are front-loaded: 70% of the year's requirement is due by mid-June. No penalty applies unless at least $500 is due after withholding.")
        .paragraph("Withholding and estimated payments must reach a target to avoid the penalty. The target is the smaller of 90% of this year's tax or 100% of last year's. As federally, last year's figure needs a full-year return. It becomes 110% if last year's California AGI was over $150,000. That's $75,000 if married filing separately. Unlike federal, it's unavailable once this year's California AGI is $1,000,000. That's $500,000 if married filing separately. Then only 90% of this year's tax will do.")
        .paragraph("The payments are not equal quarters. The split is 30% April 15, 40% June 15, none September 15 and 30% January 15. Withholding counts as paid in the same proportions on those dates. That holds whenever it was actually taken. A date on a weekend or holiday shifts to the next business day. Filing and paying in full by January 31 replaces the January payment.")
        .paragraph("There's no penalty if this year's tax less withholding is under $500. That's $250 if married filing separately. There's also none if last year's tax was under that. No liability on a full-year California return last year also means no penalty.")
        .paragraph("The penalty is interest on each shortfall for the days it stays unpaid. The Franchise Tax Board sets the rate each half-year from the federal rate. It stops on April 15 of the next year. The annualised method on FTB 5805 can lower early payments for uneven income. It uses 27%, 63%, 63% and 90% of annualised tax. The FTB can waive the penalty for a casualty or disaster. It can also waive it for retiring after 62 or disability, with reasonable cause. There's none to the extent a law passed during the year caused the underpayment.")
        .versus_federal("California's payments are 30%, 40%, 0% and 30%, not four equal quarters. So 70% is due by June. The threshold is $500, not $1,000. Either year's balance under it excuses the penalty. Last year's test isn't available at $1,000,000 of California AGI. The annualised percentages are 27%, 63%, 63% and 90%. The rate is set half-yearly.")
        .source("Cal. Rev. & Tax. Code §§ 19136, 19136.1, 19136.3, 19136.7, 19521")
        .source("2026 Instructions for Form 540-ES")
        .source("FTB 5805 (2025) and its instructions")
        .source("FTB, Interest and estimate penalty rates (updated 06/22/2026)")
        .build()
        .unwrap_or_default()
}
