//! Delaware's estimated tax rules, 30 Del. C. §§ 1169 and 1170, and the
//! addition to tax for an underpayment, § 535(b), as the shared
//! [`EstimatedTax`] interface.
//!
//! Delaware's statute is short. § 1169 requires a declaration when the
//! estimated tax exceeds $800, § 1170 takes the federal dates unless the
//! State Tax Commissioner sets others, and § 535(b) adds 1 1/2% a month, or
//! fraction of a month, on an underpayment, determined "in a manner
//! consistent with the internal revenue laws". Everything else is on the
//! Division of Revenue's forms: the 90%, 100% and 110% tests, the April 30
//! first date, the $800 stop on the required payment, the part-year
//! exception, and a penalty charged at 0.0005 a day rather than by the
//! month. Nothing here borrows from the federal crate.
//!
//! The figure follows the Division's forms, PIT-EST for 2026 and PIT-UND for
//! 2025, and the result's Divergences say where the Delaware Code reads
//! otherwise. research/estimated-payments/de.md found no act in 2024 to 2026
//! that changes §§ 535, 1169 or 1170, so no form here predates the law it
//! describes.

use std::any::Any;

use estimated::{
    Basis, Calendar, Date, Error, EstimatedTax, Explanation, Factor, FactorSeries, Facts, Figures,
    Installment, Jurisdiction, LedgerEntry, Month, Payment, Penalty, PriorReturn, Requirement,
    ResidencePeriod, Rounding, TargetOption, WaiverCause, WaiverKind, WaiverOutcome,
};
use filing::Status;
use money::{Rate, USCents};
use working::{Divergence, Notes, PositionKind, Reading};

use crate::estimated_y2026::year_2026;

/// jurisdiction is Delaware's postal code.
const JURISDICTION: Jurisdiction = estimated::DELAWARE;

/// One tax year's published figures.
#[derive(Clone, Debug)]
pub(crate) struct EstimatedYear {
    pub(crate) year: i64,
    /// PIT-UND line E: no penalty when the required annual payment less
    /// withholding is this or less.
    pub(crate) threshold: USCents,
    /// The current-year test.
    pub(crate) this_year_percent: Rate,
    /// The prior-year test at or below the step in last year's adjusted
    /// gross income.
    pub(crate) prior_percent: Rate,
    /// The prior-year test above the step.
    pub(crate) prior_percent_high: Rate,
    pub(crate) prior_year_step: USCents,
    pub(crate) prior_year_step_separate: USCents,
    pub(crate) dues: Vec<Date>,
    /// The date PIT-UND line 39 counts each installment's days from: the day
    /// after April 30 for the first, the due date for the rest.
    pub(crate) line39: Vec<Date>,
    pub(crate) calendar: Calendar,
    /// Close the periods PIT-UND line 30 credits withholding to, and the
    /// periods Part 3 annualises.
    pub(crate) period_ends: Vec<Date>,
    /// The first days of PIT-EST item 7's three later groups: a requirement
    /// first arising on or after late_starts[i] is declared by installment
    /// i+2.
    pub(crate) late_starts: Vec<Date>,
    pub(crate) stop: Date,
    /// PIT-UND line 19, the share of the annualised tax due by each
    /// installment.
    pub(crate) applicable: Vec<Rate>,
    pub(crate) daily: FactorSeries,
    pub(crate) daily_factor: Factor,
    pub(crate) short_method_rate: Rate,
}

/// PIT-UND Part 1 line C and line 23: the required annual payment is made in
/// four equal installments.
const QUARTERS: [Rate; 4] = [Rate(2500), Rate(2500), Rate(2500), Rate(2500)];

/// Returns Delaware's estimated tax rules for a tax year, or
/// [`Error::NoRules`] for a year this build does not carry.
pub fn estimated_tax(year: i64) -> Result<Box<dyn EstimatedTax>, Error> {
    let y = match year {
        2026 => year_2026()?,
        _ => return Err(estimated::no_rules(&JURISDICTION, year)),
    };
    let explanation = delaware_explanation()?;
    Ok(Box::new(DelawareEstimated { y, explanation }))
}

struct DelawareEstimated {
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
    /// Each installment's own amount: a quarter of the target, or PIT-UND
    /// line 27 under the annualised method.
    installments: Vec<USCents>,
    notes: Notes,
}

/// For each divergence, whether the statute's reading would give these
/// facts a different answer.
#[derive(Clone, Copy, Default)]
struct Affects {
    by_day: bool,
    first_day: bool,
    threshold: bool,
    part_year: bool,
    late_start: bool,
    short_method: bool,
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

fn separate(s: Status) -> bool {
    s == Status::MarriedFilingSeparately
}

/// Whether the filer lived in Delaware for some of the year but not all of
/// it, or `None` when the caller did not say where the filer lived.
fn part_year(periods: &[ResidencePeriod], year: i64) -> Option<bool> {
    if periods.is_empty() {
        return None;
    }
    let jan1 = estimated::must_date(year, Month::JANUARY, 1);
    let dec31 = estimated::must_date(year, Month::DECEMBER, 31);
    let mut days_in = 0;
    let mut d = jan1;
    while !d.after(dec31) {
        if periods
            .iter()
            .any(|p| p.state() == JURISDICTION.as_str() && !d.before(p.from()) && !d.after(p.to()))
        {
            days_in += 1;
        }
        d = d.add_days(1);
    }
    Some(days_in > 0 && days_in <= dec31.days_since(jan1))
}

fn month_day(d: Date) -> String {
    format!("{} {}", d.month(), d.day())
}

/// A date as a person reads it: "Apr 30" in the tax year, and "Jan 15,
/// 2027" outside it.
fn short(d: Date, year: i64) -> String {
    let month = d.month().to_string();
    let m = month.get(..3).unwrap_or(&month);
    if d.year() == year {
        return format!("{m} {}", d.day());
    }
    format!("{m} {}, {}", d.day(), d.year())
}

/// Decides the waiver asked for, from the reasons PIT-UND's instructions and
/// PIT-EST item 11 list. Neither sets the federal conditions that the
/// penalty be inequitable or that the shortfall have reasonable cause.
/// Retirement or disability must fall in the tax year or the year before,
/// retirement after age 62.
fn delaware_waiver(f: &Facts) -> Option<WaiverOutcome> {
    let p = f.positions();
    match p.waiver() {
        None | Some(WaiverKind::None) => return None,
        Some(WaiverKind::Casualty) => {
            let what = match p.waiver_cause() {
                WaiverCause::Casualty => "a casualty",
                WaiverCause::Disaster => "a disaster",
                _ => "a casualty, disaster or other unusual circumstance",
            };
            return Some(estimated::waiver_eligible(format!(
                "A shortfall due to {what} qualifies. The Division of Revenue can waive the penalty for it."
            )));
        }
        Some(WaiverKind::RetiredOrDisabled) => {}
    }
    let o = estimated::standard_waiver(f, "the Division of Revenue", 62)?;
    if !o.eligible() {
        return Some(o);
    }
    let what = if p.waiver_cause() == WaiverCause::Disability {
        "became disabled"
    } else {
        "retired after reaching 62"
    };
    Some(estimated::waiver_eligible(format!(
        "You {what} in {}. The Division of Revenue lists that as a reason to waive the penalty.",
        p.waiver_on().year()
    )))
}

fn paid_on(f: &Facts, d: Date) -> bool {
    f.payments()
        .iter()
        .any(|p| !p.from_prior_year() && p.on() == d)
}

impl DelawareEstimated {
    fn check(&self, f: &Facts) -> Result<(), Error> {
        if f.jurisdiction() != JURISDICTION {
            return Err(Error::Invalid(format!(
                "delawaretaxcalc: these are Delaware's rules, and the facts are for {}",
                f.jurisdiction()
            )));
        }
        if f.tax_year() != self.y.year {
            return Err(Error::Invalid(format!(
                "delawaretaxcalc: these are the {} rules, and the facts are for {}",
                self.y.year,
                f.tax_year()
            )));
        }
        if let Some(s) = f.state() {
            return Err(Error::Invalid(format!(
                "delawaretaxcalc: Delaware's rules read no extras, and the facts carry {}",
                type_name(s)
            )));
        }
        if f.farmer_or_fisher() {
            return Err(Error::with_detail(
                Error::Unsupported,
                "a filer with two-thirds of gross income from farming or fishing pays 66 2/3% by January 15, or files and pays by March 1 (PIT-EST item 5), which is not built",
            ));
        }
        if f.elections().annualised() {
            for (i, p) in f.periods().iter().enumerate() {
                let want = self.y.period_ends.get(i).copied().unwrap_or_default();
                if p.end() != want {
                    return Err(Error::Invalid(format!(
                        "delawaretaxcalc: PIT-UND Part 3 period {} ends on {}, not {}",
                        i + 1,
                        want,
                        p.end()
                    )));
                }
            }
        }
        Ok(())
    }

    /// Where the Division's forms and the Delaware Code part ways on a rule
    /// every Delaware computation uses. They are found in
    /// research/estimated-payments/de.md and checked against the statute's
    /// text in testdata/. The figures follow the forms.
    fn divergences(&self, af: Affects) -> Result<Vec<Divergence>, Error> {
        let mut out = Vec::new();
        for b in [
            working::new_divergence("The penalty by the day or by the month")
                .form(
                    "The form charges an installment's underpayment 0.05% for each day it's late. One day costs 0.05%, and thirty days 1.5%.",
                    "PIT-UND (2025), lines 40 to 42 and their instructions",
                )
                .statute(
                    "The law charges 1.5% a month, or part of a month, on the underpayment. One day late costs a full 1.5%.",
                    "30 Del. C. § 535(b)",
                )
                .uses(Reading::Form)
                .affects(af.by_day),
            working::new_divergence("The day the first installment's penalty starts")
                .form(
                    "The form counts the first installment's late days from May 1. That is the day after its Apr 30 due date. It counts the other three from their due dates.",
                    "PIT-UND (2025), lines 39 and 40",
                )
                .statute(
                    "An installment not paid by its due date is underpaid from that date. That date is Apr 30 for the first. The charge runs for as long as it's underpaid.",
                    "30 Del. C. §§ 535(b), 1170(a); PIT-EST (2026), item 8",
                )
                .uses(Reading::Form)
                .affects(af.first_day),
            working::new_divergence("The $800 test")
                .form(
                    "Your required payment is the smaller of the two tests. There is no penalty when it less withholding is $800 or less.",
                    "PIT-UND (2025), Part 1, line E; PIT-UND instructions, Exceptions to the Penalty",
                )
                .statute(
                    "A declaration is needed when the tax can be expected to be more than $800. That tax is this year's tax less withholding. The law measures this year's tax, not the required payment.",
                    "30 Del. C. §§ 1169(a), (b), 535(b)",
                )
                .uses(Reading::Form)
                .affects(af.threshold),
            working::new_divergence("Part-year residents")
                .form(
                    "No penalty for anyone who was a Delaware resident for only part of the year.",
                    "PIT-UND (2025) instructions, Exceptions to the Penalty",
                )
                .statute(
                    "Every resident and nonresident must declare an estimated tax over $800. The penalty has no exception for a part-year resident.",
                    "30 Del. C. §§ 1169(a), 535(b)",
                )
                .uses(Reading::Form)
                .affects(af.part_year),
            working::new_divergence("A requirement first met after Apr 1")
                .form(
                    "Each installment is a quarter of your required payment. That holds whenever the requirement arose. Only the annualised method lowers the earlier ones.",
                    "PIT-UND (2025), Part 1, line C, and line 23",
                )
                .statute(
                    "The Commissioner may set the dates. The estimated tax form sets Jun 15, Sep 15 or Jan 15 as the first date. That is for someone first required after Apr 1, Jun 1 or Sep 1. The form sets equal installments on the remaining dates.",
                    "30 Del. C. § 1170(a); PIT-EST (2026), item 7",
                )
                .uses(Reading::Form)
                .affects(af.late_start),
            working::new_divergence("The optional Short Method")
                .form(
                    format!(
                        "You can use the form's Short Method if you made no estimated payments. You can also use it if you made four equal ones on time. It lets you pay 12% of the year's total underpayment instead. That is less 0.05% a day for anything paid before {}.",
                        short(self.y.stop, self.y.year)
                    ),
                    "PIT-UND (2025), Part 2, lines F to L, and its instructions",
                )
                .statute(
                    "The law charges 1.5% a month, or part of a month. It is charged on each installment's underpayment for as long as it's underpaid.",
                    "30 Del. C. § 535(b)",
                )
                .uses(Reading::Form)
                .affects(af.short_method),
        ] {
            out.push(b.build()?);
        }
        Ok(out)
    }

    fn assess(&self, f: &Facts) -> Result<Assessment, Error> {
        self.check(f)?;
        let y = &self.y;
        let mut a = Assessment::default();
        let tax_opt = f.this_year().tax();
        let tax_known = tax_opt.is_some();
        let tax = tax_opt.unwrap_or_default();
        a.tax_known = tax_known;
        let withheld = f.withheld_total();
        let prior_opt = f.prior_year();
        let has_prior = prior_opt.is_some();
        let prior = prior_opt.unwrap_or_default();
        let last = y.year - 1;
        let prior_figures = if has_prior {
            prior.figures().unwrap_or_default()
        } else {
            Figures::default()
        };
        let prior_tax = if has_prior {
            prior.figures().and_then(|pf| pf.tax()).unwrap_or_default()
        } else {
            USCents(0)
        };
        // 90% of this year's tax. PIT-EST item 10(a); PIT-UND line A.
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
                "PIT-EST (2026), item 10(a); PIT-UND (2025), Part 1, line A",
                format!(
                    "{} of this year's Delaware tax of {tax} is {}",
                    y.this_year_percent,
                    this_year.amount()
                ),
            );
        }
        let prior_opt =
            self.prior_option(f, &mut a, has_prior, &prior, &prior_figures, prior_tax)?;
        let mut rb = estimated::new_requirement();
        rb.option(this_year.clone()).option(prior_opt.clone());
        let (mut chosen, mut target) = (Basis::ThisYearTax, this_year.amount());
        if prior_opt.available() && (!tax_known || prior_opt.amount() < target) {
            (chosen, target) = (Basis::PriorYearTax, prior_opt.amount());
        }
        // No liability last year. PIT-UND states it with no condition on the
        // length of the year; PIT-EST (2026) item 10 requires a twelve-month
        // year, and that is the default.
        let mut no_liability =
            has_prior && ((prior.filed() && prior_tax.0 == 0) || prior.not_required_to_file());
        if no_liability && prior.months() != 12 {
            a.notes.ask(
                PositionKind::UnsettledRule,
                "",
                format!(
                    "The estimated tax form needs a twelve-month year with no Delaware tax. The penalty form sets no length, but your {}-month {last} doesn't excuse you.",
                    prior.months()
                ),
            );
            no_liability = false;
        }
        let residence = part_year(&f.residence(), y.year);
        let part = residence == Some(true);
        if no_liability {
            a.exempt = true;
            let mut why = format!("your {last} return showed no Delaware tax");
            if prior.not_required_to_file() {
                why = format!(
                    "no {last} Delaware return was required, which is taken as no Delaware liability"
                );
            }
            let none = estimated::new_exception("No Delaware tax last year").build()?;
            rb.required(
                false,
                format!(
                    "You had no Delaware tax for {last}, so no estimated payments are needed for {}.",
                    y.year
                ),
            )
            .exempt(none);
            a.notes.rule(
                "PIT-UND (2025) instructions, Exceptions to the Penalty; PIT-EST (2026), item 10",
                format!(
                    "{why}, a twelve-month year, so there is no penalty for {}",
                    y.year
                ),
            );
        } else if part {
            a.exempt = true;
            let ex = estimated::new_exception("Part-year Delaware resident").build()?;
            rb.required(
                false,
                format!(
                    "You were a Delaware resident for only part of {}. Delaware charges no estimated tax penalty to a part-year resident. No estimated payments are needed.",
                    y.year
                ),
            )
            .exempt(ex);
            a.notes.rule(
                "PIT-UND (2025) instructions, Exceptions to the Penalty",
                format!(
                    "you were a part-year resident during {}, so there is no penalty",
                    y.year
                ),
            );
        } else if !tax_known && !prior_opt.available() {
            rb.required(
                false,
                format!(
                    "No {last} Delaware return sets a target. There is no target until your {} Delaware tax is figured.",
                    y.year
                ),
            )
            .no_target();
        } else if target - withheld <= y.threshold {
            a.exempt = true;
            let small = estimated::exception_at_most(
                "Required payment less withholding",
                target - withheld,
                y.threshold,
            )?;
            let mut left = format!(
                "leaves {}, which is {} or less",
                (target - withheld).whole(),
                y.threshold.whole()
            );
            if target < withheld {
                left = estimated::leaves(target - withheld);
            }
            if tax_known {
                rb.required(
                    false,
                    format!(
                        "Your required payment is {}. Less {} of withholding, that {left}. No estimated payments are needed.",
                        target.whole(),
                        withheld.whole()
                    ),
                )
                .exempt(small);
            } else {
                rb.required(
                    false,
                    format!(
                        "Your required payment is at most {} of your {last} Delaware tax, {}. Less {} of withholding, that {left}. No estimated payments are needed, whatever your {} tax turns out to be.",
                        prior_opt.percent(),
                        target.whole(),
                        withheld.whole(),
                        y.year
                    ),
                )
                .exempt(small);
            }
            a.notes.rule(
                "PIT-UND (2025), Part 1, line E",
                format!(
                    "the required annual payment of {target} less {withheld} of withholding is {}, which is {} or less, so there is no penalty",
                    target - withheld,
                    y.threshold
                ),
            );
        } else if !tax_known {
            rb.required(
                true,
                format!(
                    "Your target is {} of your {last} Delaware tax, {}. It keeps you clear of a penalty whatever your {} tax turns out to be. After {} of withholding, that's {} of estimated payments. A quarter is due by each date.",
                    prior_opt.percent(),
                    target.whole(),
                    y.year,
                    withheld.whole(),
                    (target - withheld).whole()
                ),
            )
            .choose(chosen);
        } else {
            rb.required(
                true,
                format!(
                    "Your required payment is {}. After {} of withholding, the {} left is more than {}. You need estimated payments of {}, a quarter by each date.",
                    target.whole(),
                    withheld.whole(),
                    (target - withheld).whole(),
                    y.threshold.whole(),
                    (target - withheld).whole()
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
                format!("Did you file a {last} Delaware return, and what was its tax?"),
                format!(
                    "With it, {} of last year's tax could be your target. That is if it's lower than {} of this year's.",
                    y.prior_percent, y.this_year_percent
                ),
            );
        }
        if !a.exempt && !tax_known {
            let effect = if a.requirement.target_known() {
                "Until then, your target can only come from last year's tax."
            } else {
                "Until then, there's no target."
            };
            a.notes.ask(
                PositionKind::ThisYearTax,
                format!("What do you expect your {} Delaware tax to be?", y.year),
                effect,
            );
        }
        if !a.exempt && residence.is_none() && a.requirement.target_known() {
            a.notes.ask(
                PositionKind::Residency,
                format!("Were you a Delaware resident for only part of {}?", y.year),
                "If you were, Delaware charges you no estimated tax penalty.",
            );
        }
        // The due dates. PIT-EST item 8 prints them; 30 Del. C. § 551(d)
        // moves one that falls on a Saturday, Sunday or Delaware legal
        // holiday.
        a.dues = estimated::move_dates(&y.calendar, &y.dues);
        for (i, due) in a.dues.iter().enumerate() {
            if *due != y.dues[i] {
                a.notes.rule(
                    "30 Del. C. § 551(d)",
                    format!(
                        "the installment due {} falls on a Saturday, Sunday or legal holiday, so it is due {}",
                        y.dues[i].long(),
                        due.long()
                    ),
                );
            }
        }
        // The statute's $800 test is on this year's tax, so it can only
        // differ when that tax is given and no other exception applies.
        let mut af = Affects {
            part_year: part && !no_liability,
            threshold: !no_liability
                && !part
                && tax_known
                && ((target - withheld <= y.threshold) != (tax - withheld <= y.threshold)),
            ..Affects::default()
        };
        if a.exempt {
            a.installments = vec![USCents(0); a.dues.len()];
            self.diverge(&mut a, af)?;
            return Ok(a);
        }
        let cumulative = estimated::cumulative_shares(a.requirement.target(), &QUARTERS)?;
        a.installments = estimated::increments(&cumulative);
        a.notes.rule(
            "PIT-EST (2026), item 8; PIT-UND (2025), Part 1, line C, and line 23",
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
            self.annualise(f, &mut a)?;
        }
        af.late_start = self.late_start(f, &mut a);
        let (ledger, _) = self.ledger(f, &a)?;
        for (i, l) in ledger.iter().enumerate() {
            if l.shortfall().0 > 0 {
                af.by_day = true;
                af.first_day = af.first_day || i == 0;
            }
        }
        af.short_method = self.short_method(f, &a, y.stop).is_some();
        self.diverge(&mut a, af)?;
        Ok(a)
    }

    /// Adds the divergences to the assessment's notes.
    fn diverge(&self, a: &mut Assessment, af: Affects) -> Result<(), Error> {
        let ds = self.divergences(af)?;
        a.notes.diverges(&ds);
        Ok(())
    }

    /// PIT-UND line B: 100% of last year's Delaware tax, or 110% above the
    /// step in last year's adjusted gross income.
    fn prior_option(
        &self,
        f: &Facts,
        a: &mut Assessment,
        has_prior: bool,
        prior: &PriorReturn,
        pf: &Figures,
        prior_tax: USCents,
    ) -> Result<TargetOption, Error> {
        let y = &self.y;
        let last = y.year - 1;
        let unavailable = |why: String| -> Result<TargetOption, Error> {
            estimated::new_target_option(Basis::PriorYearTax, y.prior_percent, USCents(0))
                .unavailable(why)
                .build()
        };
        if !has_prior {
            return unavailable(format!("Needs your {last} Delaware return"));
        }
        if prior.not_required_to_file() {
            return unavailable(format!("You didn't need to file a {last} Delaware return"));
        }
        if !prior.filed() {
            return unavailable(format!("You didn't file a {last} Delaware return"));
        }
        let mut step = y.prior_year_step;
        if separate(f.filing_status()) {
            step = y.prior_year_step_separate;
        }
        let rate = |agi: USCents| -> Rate {
            if agi > step {
                y.prior_percent_high
            } else {
                y.prior_percent
            }
        };
        let percent;
        let why;
        match (pf.state_agi(), pf.federal_agi()) {
            (Some(state_agi), federal) => {
                percent = rate(state_agi);
                why = if percent == y.prior_percent_high {
                    format!("the {last} Delaware AGI of {state_agi} is more than {step}")
                } else {
                    format!("the {last} Delaware AGI of {state_agi} is not more than {step}")
                };
                match federal {
                    None => {
                        a.notes.ask(
                            PositionKind::AdjustedGrossIncome,
                            format!("What was your {last} federal adjusted gross income?"),
                            format!(
                                "Until then, your Delaware AGI is tested against the {} step. The penalty form uses Delaware AGI, and the estimated tax form uses federal AGI.",
                                step.whole()
                            ),
                        );
                    }
                    Some(federal_agi) if rate(federal_agi) != percent => {
                        a.notes.ask(
                            PositionKind::UnsettledRule,
                            "",
                            format!(
                                "Delaware AGI of {} gives {percent}, but federal AGI of {} gives {}. The prior-year percentage follows the penalty form's Delaware AGI.",
                                state_agi.whole(),
                                federal_agi.whole(),
                                rate(federal_agi)
                            ),
                        );
                    }
                    Some(_) => {}
                }
            }
            (None, Some(federal_agi)) => {
                percent = rate(federal_agi);
                why = if percent == y.prior_percent_high {
                    format!("the {last} federal AGI of {federal_agi} is more than {step}")
                } else {
                    format!("the {last} federal AGI of {federal_agi} is not more than {step}")
                };
                a.notes.ask(
                    PositionKind::AdjustedGrossIncome,
                    format!("What was your {last} Delaware adjusted gross income?"),
                    format!(
                        "Until then, your federal AGI is tested against the {} step. The estimated tax form uses federal AGI, and the penalty form uses Delaware AGI.",
                        step.whole()
                    ),
                );
            }
            (None, None) => {
                percent = y.prior_percent_high;
                why = format!("the {last} AGI was not given");
                a.notes.ask(
                    PositionKind::AdjustedGrossIncome,
                    format!("What was your {last} adjusted gross income?"),
                    format!(
                        "Until then, the prior-year option is {} of last year's tax. That is the figure above {}, rather than {}.",
                        y.prior_percent_high,
                        step.whole(),
                        y.prior_percent
                    ),
                );
            }
        }
        let amount = percent.of(prior_tax);
        a.notes.rule(
            "PIT-EST (2026), item 10(b); PIT-UND (2025), Part 1, line B",
            format!(
                "{why}, so the prior-year test is {percent} of the {prior_tax} tax on the {last} Delaware return, or {amount}; no Delaware source limits it to a twelve-month return"
            ),
        );
        estimated::new_target_option(Basis::PriorYearTax, percent, amount).build()
    }

    /// Replaces the installments with PIT-UND line 27: the applicable
    /// percentage of the period's annualised tax less the installments
    /// before it, or the regular quarter plus what the annualised method
    /// left out earlier, whichever is smaller. That is the federal
    /// recapture, which [`estimated::annualised`] computes.
    fn annualise(&self, f: &Facts, a: &mut Assessment) -> Result<(), Error> {
        let tax: Vec<USCents> = f.periods().iter().map(|p| p.annualised_tax()).collect();
        let out = estimated::annualised(&tax, &self.y.applicable, &a.installments)?;
        let ap = &self.y.applicable;
        a.notes.rule(
            "PIT-UND (2025), Part 3, lines 18 to 27",
            format!(
                "under the annualised method the installments are the smaller of {}, {}, {} and {} of the annualised tax, {}, {}, {} and {}, less the installments before, and the regular quarter with any shortfall carried forward: {}, {}, {} and {}",
                ap[0], ap[1], ap[2], ap[3], tax[0], tax[1], tax[2], tax[3], out[0], out[1], out[2],
                out[3]
            ),
        );
        a.installments = out;
        Ok(())
    }

    /// Notes PIT-EST item 7 for a filer who first expected to owe after
    /// April 1, and reports whether it applies. PIT-UND does not lower the
    /// earlier installments for it; see the divergences.
    fn late_start(&self, f: &Facts, a: &mut Assessment) -> bool {
        let met = f.requirement_first_met();
        let mut first = 0;
        for (i, g) in self.y.late_starts.iter().enumerate() {
            if !met.is_zero() && !met.before(*g) {
                first = i + 1;
            }
        }
        if first == 0 {
            return false;
        }
        a.notes.rule(
            "PIT-EST (2026), item 7; PIT-UND (2025), line 23",
            format!(
                "you first expected to owe on {}, so PIT-EST lets you declare by {} and pay in equal installments from then; PIT-UND still charges a quarter on each date, and the figures here follow it, so the earlier installments are short unless the annualised method lowers them",
                met.long(),
                a.dues[first].long()
            ),
        );
        true
    }

    /// What PIT-UND Part 4 counts toward each installment: withholding in
    /// the period it was withheld (line 30), credited on that period's due
    /// date; estimated payments on their dates (line 29); and last year's
    /// overpayment on the first date, as PIT-EST Method 1 applies it.
    fn credits(&self, f: &Facts, dues: &[Date]) -> Result<Vec<Payment>, Error> {
        let mut by_period = vec![USCents(0); dues.len()];
        for w in f.withholding() {
            for (i, end) in self.y.period_ends.iter().enumerate() {
                if !w.on().after(*end) {
                    by_period[i] += w.amount();
                    break;
                }
            }
        }
        let mut out = Vec::new();
        for (i, amount) in by_period.into_iter().enumerate() {
            if amount.0 == 0 {
                continue;
            }
            out.push(
                estimated::new_payment()
                    .on(dues[i])
                    .amount(amount)
                    .build()?,
            );
        }
        for p in f.payments() {
            if p.from_prior_year() {
                out.push(
                    estimated::new_payment()
                        .on(dues[0])
                        .amount(p.amount())
                        .build()?,
                );
            } else {
                out.push(p);
            }
        }
        Ok(out)
    }

    /// Applies the credits to the installments.
    fn ledger(&self, f: &Facts, a: &Assessment) -> Result<(Vec<LedgerEntry>, Notes), Error> {
        let credits = self.credits(f, &a.dues)?;
        let ledger = estimated::apply(&a.dues, &a.installments, &credits)?;
        let y = &self.y;
        let mut notes = Notes::default();
        notes.rule(
            "PIT-UND (2025) instructions, line 30",
            format!(
                "the {} withheld counts in the period it was withheld, January 1 to {} toward {}, then to {}, {} and {} toward the later dates; it is not spread evenly",
                f.withheld_total(),
                month_day(y.period_ends[0]),
                a.dues[0].long(),
                month_day(y.period_ends[1]),
                month_day(y.period_ends[2]),
                month_day(y.period_ends[3])
            ),
        );
        if f.elections().withholding_as_withheld() {
            notes.rule(
                "PIT-UND (2025) instructions, line 30",
                "Delaware has no election about withholding; PIT-UND always counts it in the period it was withheld",
            );
        }
        notes.rule(
            "PIT-UND (2025), lines 32 to 38; PIT-EST (2026), Method of Payments",
            "a payment first pays the oldest underpayment, on the day it is paid, and last year's overpayment applied forward counts toward the first installment",
        );
        Ok((ledger, notes))
    }

    /// The installments and the notes on how they were credited.
    fn schedule_of(&self, f: &Facts, a: &Assessment) -> Result<(Vec<Installment>, Notes), Error> {
        let (ledger, notes) = self.ledger(f, a)?;
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
        Ok((out, notes))
    }

    /// PIT-UND Part 2, when it is open: the required annual payment less
    /// payments and withholding, times 12%, less 0.0005 a day times that
    /// underpayment for each day before April 30 it was paid. It is open to
    /// a filer who made no estimated payments, or exactly four equal ones
    /// each on or before its due date. Last year's overpayment applied
    /// forward is taken to close it, since it is not one of four equal
    /// payments. PIT-UND also closes it when capital gain tax was paid on
    /// the filer's behalf, which the facts do not record; such a payment is
    /// an estimated payment here, and it closes the method the same way.
    fn short_method(&self, f: &Facts, a: &Assessment, stop: Date) -> Option<String> {
        let y = &self.y;
        let mut payments = f.payments();
        if payments.iter().any(|p| p.from_prior_year()) {
            return None;
        }
        payments.sort_by(|a, b| a.on().compare(b.on()).cmp(&0));
        let mut paid = USCents(0);
        if !payments.is_empty() {
            if payments.len() != a.dues.len() {
                return None;
            }
            for (i, p) in payments.iter().enumerate() {
                if p.amount() != payments[0].amount()
                    || p.on().after(a.dues[i])
                    || (i > 0 && !p.on().after(a.dues[i - 1]))
                {
                    return None;
                }
                paid += p.amount();
            }
        }
        let underpaid = a.requirement.target() - paid - f.withheld_total();
        if underpaid.0 <= 0 {
            return None;
        }
        let charge = y.short_method_rate.of(underpaid);
        let mut early = 0;
        let mut credit = USCents(0);
        if stop.before(y.stop) {
            early = y.stop.days_since(stop);
            credit = y.daily_factor.of(underpaid, early, Rounding::RoundToCent);
        }
        Some(format!(
            "you made no estimated payments or four equal ones on time, so you may use the Short Method instead: {} of the {underpaid} underpayment is {charge}, less {credit} for {early} days before {}, is {}",
            y.short_method_rate,
            y.stop.long(),
            charge - credit
        ))
    }
}

impl EstimatedTax for DelawareEstimated {
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
                    "with no {} Delaware return to measure against, there is no target to schedule",
                    self.y.year - 1
                ),
            ));
        }
        let (out, notes) = self.schedule_of(f, &a)?;
        Ok(working::new_result(out)
            .with(&a.notes)
            .with(&notes)
            .build()?)
    }

    fn penalty(&self, f: &Facts, as_of: Date) -> Result<working::Result<Penalty>, Error> {
        if as_of.is_zero() {
            return Err(Error::Invalid(
                "delawaretaxcalc: a penalty needs the date to figure it to".into(),
            ));
        }
        let a = self.assess(f)?;
        if !a.requirement.target_known() {
            return Err(Error::with_detail(
                Error::NeedsThisYearTax,
                format!(
                    "with no {} Delaware return to measure against, there is no target to figure a penalty from",
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
            stop = filed;
            paid_in_full = true;
        }
        let waiver_answer = f.positions().waiver();
        let waiver = waiver_answer.unwrap_or_default();
        let (schedule, _) = self.schedule_of(f, &a)?;
        if a.exempt {
            let p = estimated::new_penalty(stop)
                .waiver(waiver)
                .because(a.requirement.clone(), &schedule)
                .build()?;
            return Ok(working::new_result(p).with(&a.notes).build()?);
        }
        let (ledger, mut notes) = self.ledger(f, &a)?;
        let mut pb = estimated::new_penalty(stop);
        pb.waiver(waiver).because(a.requirement.clone(), &schedule);
        let mut first_short = false;
        for (i, l) in ledger.iter().enumerate() {
            if l.shortfall().0 <= 0 {
                continue;
            }
            let from = y.line39[i];
            let mut lb = estimated::new_penalty_line(l.number(), l.due(), l.shortfall());
            let mut last = from;
            for piece in l.pieces() {
                let mut to = stop;
                if let Some(on) = piece.paid_on()
                    && on.before(stop)
                {
                    to = on;
                }
                if !to.after(from) {
                    continue;
                }
                if to.after(last) {
                    last = to;
                }
                let (parts, gap) = estimated::daily_factor(piece.amount(), from, to, &y.daily)?;
                if !gap.is_zero() {
                    return Err(Error::Invalid(format!(
                        "delawaretaxcalc: the daily factor has no period for {gap}"
                    )));
                }
                for p in parts {
                    lb.part(p);
                }
            }
            let line = lb.days(last.days_since(from)).build()?;
            if i == 0 && line.total().0 > 0 {
                first_short = true;
            }
            pb.line(line);
        }
        let mut p = pb.build()?;
        if !a.tax_known {
            p = p.to_builder().upper_bound().build()?;
            notes.rule(
                "PIT-UND (2025), Part 1, lines A to C",
                format!(
                    "this year's Delaware tax was not given, so the penalty is figured against {} of the {} tax alone; the required annual payment is the lesser of that and {} of this year's tax, so {} is the most the penalty can be, and it is less if this year's tax turns out lower",
                    a.requirement.options()[1].percent(),
                    y.year - 1,
                    y.this_year_percent,
                    p.total()
                ),
            );
        }
        if paid_in_full {
            notes.rule(
                "PIT-UND (2025) instructions, line 40",
                format!(
                    "the return was filed with the whole balance paid on {}, so every underpayment still outstanding was paid that day",
                    stop.long()
                ),
            );
        }
        notes.rule(
            "30 Del. C. § 535(b); PIT-UND (2025), lines 39 to 43",
            format!(
                "each underpayment is charged {} for each day from {} (the first installment), {}, {} or {} until paid or {}, to the cent; the penalty to {} is {}",
                y.daily_factor,
                y.line39[0].long(),
                y.line39[1].long(),
                y.line39[2].long(),
                y.line39[3].long(),
                y.stop.long(),
                stop.long(),
                p.total()
            ),
        );
        if first_short {
            notes.ask(
                PositionKind::UnsettledRule,
                "",
                format!(
                    "The {} penalty form isn't published, so the figures follow the {} form. Its first installment's late days run from {}, not {}.",
                    y.year,
                    y.year - 1,
                    short(y.line39[0], y.year),
                    short(a.dues[0], y.year)
                ),
            );
        }
        let day = a.dues[3].add_days(1);
        if paid_on(f, day) {
            notes.ask(
                PositionKind::UnsettledRule,
                "",
                format!(
                    "The form's instructions apply your {} payment to the last installment. Its column stops at {}, so the figures count the payment a day late.",
                    short(day, y.year),
                    short(y.dues[3], y.year)
                ),
            );
        }
        if let Some(applied) = self.short_method(f, &a, stop) {
            notes.rule("PIT-UND (2025), Part 2, lines F to L", applied);
        }
        if p.total().0 > 0 && waiver_answer.is_none() {
            notes.ask(
                PositionKind::PenaltyWaiver,
                "Are you asking the Division to waive the penalty?",
                "The reasons are a casualty, disaster, retirement after 62 or disability. You ask in writing after assessment, and the penalty shown is before any waiver.",
            );
        }
        if waiver != WaiverKind::None {
            notes.rule(
                "PIT-UND (2025) instructions, Waiver of the Penalty; PIT-EST (2026), item 11",
                format!(
                    "a waiver for {waiver} is asked for in writing to the Tax Conferee, P.O. Box 8714, once the penalty has been assessed; PIT-UND is not filed for it"
                ),
            );
        }
        if let Some(o) = delaware_waiver(f)
            && p.total().0 > 0
        {
            p = p.to_builder().waiver_outcome(o).build()?;
        }
        Ok(working::new_result(p).with(&a.notes).with(&notes).build()?)
    }

    fn explanation(&self) -> Explanation {
        self.explanation.clone()
    }
}

/// Written by hand and checked against research/estimated-payments/de.md.
fn delaware_explanation() -> Result<Explanation, Error> {
    estimated::new_explanation(
        "Delaware expects estimated payments when your tax will be more than $800. This is your Delaware tax after withholding and credits. The penalty is 1.5% a month on each late installment. The Division's form charges it as 0.05% a day.",
    )
    .paragraph("To avoid the penalty, withholding and estimated payments must reach a target. The target is the smaller of two figures. They are 90% of this year's Delaware tax and 100% of last year's. The 100% is 110% if last year's adjusted gross income (AGI) was over $150,000. That line is $75,000 if you file separately on separate forms (filing status 3). The penalty form tests Delaware AGI. The estimated tax instructions say federal AGI. If the two fall on different sides of the line, settle it with the Division. No penalty is due if the target less your Delaware withholding is $800 or less. There is also none if you had no Delaware tax liability last year.")
    .paragraph("A quarter is due by April 30, June 15, September 15 and the next January 15. The first is April 30, not April 15, because Delaware's return is due April 30. A date on a weekend or Delaware holiday becomes the next business day. Withholding counts in the period it was taken out, not evenly across the year. Withholding from January to March counts toward April 30. April and May withholding counts toward June 15, and so on. If your income came unevenly, Form PIT-UND has an annualised method. It uses 22.5%, 45%, 67.5% and 90% of the tax on annualised income.")
    .paragraph("The law sets the penalty at 1.5% a month, or part of a month. The rate doesn't change with interest rates. The form charges by the day instead, 0.05% for each day an installment is late. Thirty days come to 1.5%. It runs from the due date, or May 1 for the first installment. It stops when the shortfall is paid, or on April 30 of the next year. The form's Short Method lets you pay 12% of the total shortfall instead. It is open if you made no estimated payments, or four equal ones on time.")
    .paragraph("The Division can waive the penalty in two cases. One is a casualty, disaster or other unusual circumstance. The other is that you retired after 62 or became disabled this year or last. You don't claim either on the form. You wait for the penalty to be assessed, then write to the Tax Conferee.")
    .versus_federal("Delaware's line is $800 of required payment less withholding, not $1,000 of tax. The penalty is a fixed 1.5% a month, not a rate set each quarter. It is charged at 0.05% a day, about 18% a year. Withholding counts when it was taken out, and there is a 12% Short Method. Federal Form 2210 is not accepted in place of PIT-UND.")
    .source("30 Del. C. §§ 535(b), 551(d), 1168, 1169, 1170")
    .source("Form PIT-EST, Declaration of Estimated Income Tax for Individuals, 2026 (revised 11/10/25)")
    .source("Form PIT-UND, Delaware Underpayment of Estimated Taxes, and its instructions, 2025 (revised 11/10/25)")
    .source("Form PIT-RES Resident Instructions, 2025, Requirement to File Estimated Taxes")
    .build()
}
