//! Iowa's estimated tax rules, Iowa Code § 422.16(12), as the shared
//! [`EstimatedTax`] interface.
//!
//! § 422.16(12)(d)(3) adopts the method of 26 U.S.C. § 6654 at Iowa's own
//! interest rate under § 421.7. The shape is therefore federal: the lesser
//! of 90% of this year's tax and 100% or 110% of last year's, four equal
//! installments, and interest by the day. The figures and dates are Iowa's:
//! installments at the end of April, June, September and January, a $1,000
//! threshold from 2026, a late-start table that gives fewer installments,
//! and a penalty that runs to April 30.
//!
//! The figure follows the Department of Revenue's forms, the 2026 IA 1040ES
//! and the 2025 IA 2210, except that the 2025 IA 2210's $200 threshold
//! predates the act that set $1,000 for 2026, so the act's figure is used.
//! The result's Divergences say where form and law differ.

use std::any::Any;

use estimated::{
    Basis, Calendar, Date, Error, EstimatedTax, Explanation, FactorSeries, Facts, Installment,
    Jurisdiction, LedgerEntry, Penalty, PenaltyLine, PenaltyPart, Requirement, TargetOptionBuilder,
    WaiverCause, WaiverKind, WaiverOutcome,
};
use filing::Status;
use money::{Rate, USCents};
use working::{Divergence, Notes, PositionKind, Reading};

use crate::estimated_y2026::year_2026;

/// jurisdiction is Iowa's postal code.
const JURISDICTION: Jurisdiction = estimated::IOWA;

/// The facts Iowa's rules read that no other jurisdiction does. Carry them
/// in [`Facts::state`].
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct EstimatedFacts {
    prior_modifications: Option<USCents>,
}

impl EstimatedFacts {
    /// Last year's Iowa net modifications, IA 1040 line 3. IA 2210 line 7
    /// adds them to last year's federal AGI to decide whether the prior-year
    /// test is 110% rather than 100%. They can be negative.
    pub fn prior_year_modifications(&self) -> Option<USCents> {
        self.prior_modifications
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
    /// Sets last year's Iowa net modifications.
    pub fn prior_year_modifications(&mut self, a: USCents) -> &mut Self {
        self.e.prior_modifications = Some(a);
        self
    }

    /// Accepts any modifications, since they can be negative.
    pub fn build(&self) -> Result<EstimatedFacts, Error> {
        Ok(self.e)
    }
}

/// One tax year's published figures.
#[derive(Clone, Debug)]
pub(crate) struct EstimatedYear {
    pub(crate) year: i64,
    /// § 422.16(12)(a)(1): estimates are required only when the tax after
    /// withholding can be expected to reach it.
    pub(crate) threshold: USCents,
    /// The 2025 IA 2210's small-balance test, lines 3 and 6, which predates
    /// the act that set threshold. It is reported, not used.
    pub(crate) form_threshold: USCents,
    /// IA 2210 lines 4 and 7. The high share applies when last year's
    /// federal AGI plus Iowa net modifications exceeds prior_year_step, or
    /// prior_year_step_separate for a married filing separate federal
    /// return.
    pub(crate) this_year_share: Rate,
    pub(crate) prior_year_share: Rate,
    pub(crate) prior_year_share_high: Rate,
    pub(crate) prior_year_step: USCents,
    pub(crate) prior_year_step_separate: USCents,
    pub(crate) dues: Vec<Date>,
    /// The first days of the IA 2210 table's second, third and fourth rows:
    /// a requirement first met on or after later_groups[i] starts at
    /// installment i+2.
    pub(crate) later_groups: Vec<Date>,
    pub(crate) calendar: Calendar,
    /// IA 2210 line 23b: the penalty runs no later than April 30 of the next
    /// year.
    pub(crate) stop: Date,
    /// The factor IA 2210 line 24 prints for each calendar year.
    pub(crate) daily: FactorSeries,
}

/// IA 2210 line 20: an equal part of the withholding is treated as paid on
/// each installment date.
const QUARTERS: [Rate; 4] = [Rate(2500), Rate(2500), Rate(2500), Rate(2500)];

/// Returns Iowa's estimated tax rules for a tax year, or [`Error::NoRules`]
/// for a year this build does not carry.
pub fn estimated_tax(year: i64) -> Result<Box<dyn EstimatedTax>, Error> {
    let y = match year {
        2026 => year_2026()?,
        _ => return Err(estimated::no_rules(&JURISDICTION, year)),
    };
    Ok(Box::new(IowaEstimated {
        y,
        explanation: iowa_explanation()?,
    }))
}

struct IowaEstimated {
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
    /// The installment a late starter's requirement starts at, from 0.
    /// installments are each installment's own amount; those before first
    /// are zero.
    first: i64,
    installments: Vec<USCents>,
    notes: Notes,
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

/// A charge on a piece of a shortfall from one day to another.
type Charge<'a> = &'a dyn Fn(USCents, Date, Date) -> Result<(Vec<PenaltyPart>, Date), Error>;

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

/// "Apr 30" inside the tax year and "Feb 1, 2027" outside it.
fn short_date(d: Date, year: i64) -> String {
    let month = d.month().to_string();
    let m = month.get(..3).unwrap_or(&month);
    if d.year() == year {
        return format!("{m} {}", d.day());
    }
    format!("{m} {}, {}", d.day(), d.year())
}

fn total(lines: &[PenaltyLine]) -> USCents {
    lines.iter().map(|l| l.total()).sum()
}

/// Each installment the ledger gives.
fn schedule_of(ledger: &[LedgerEntry]) -> Result<Vec<Installment>, Error> {
    let mut out = Vec::with_capacity(ledger.len());
    for (i, l) in ledger.iter().enumerate() {
        out.push(
            estimated::new_installment(i as i64 + 1, l.due())
                .needed_by(l.needed_by())
                .credited_by(l.credited_by())
                .shortfall(l.shortfall())
                .build()?,
        );
    }
    Ok(out)
}

impl IowaEstimated {
    fn extras(&self, f: &Facts) -> Result<EstimatedFacts, Error> {
        match f.state() {
            None => Ok(EstimatedFacts::default()),
            Some(s) => match s.downcast_ref::<EstimatedFacts>() {
                Some(e) => Ok(*e),
                None => Err(Error::Invalid(format!(
                    "iowataxcalc: the facts carry {}, not iowataxcalc.EstimatedFacts",
                    type_name(s)
                ))),
            },
        }
    }

    fn check(&self, f: &Facts) -> Result<(), Error> {
        if f.jurisdiction() != JURISDICTION {
            return Err(Error::Invalid(format!(
                "iowataxcalc: these are Iowa's rules, and the facts are for {}",
                f.jurisdiction()
            )));
        }
        if f.tax_year() != self.y.year {
            return Err(Error::Invalid(format!(
                "iowataxcalc: these are the {} rules, and the facts are for {}",
                self.y.year,
                f.tax_year()
            )));
        }
        if f.farmer_or_fisher() {
            return Err(Error::with_detail(
                Error::Unsupported,
                "farmers and fishers with two-thirds of gross income from farming or fishing file IA 2210F, which is not built",
            ));
        }
        if f.elections().annualised() {
            return Err(Error::with_detail(
                Error::Unsupported,
                "the annualised method is IA 2210 Schedule AI (45-011), which is not built",
            ));
        }
        Ok(())
    }

    /// Where the Department of Revenue's forms and Iowa law part ways on a
    /// rule every Iowa computation uses. They are found in
    /// research/estimated-payments/ia.md and checked against the extracts
    /// in testdata/. The affects flags say whether each changes anything for
    /// the facts.
    fn divergences(
        &self,
        threshold: bool,
        factor: bool,
        waiver: bool,
    ) -> Result<Vec<Divergence>, Error> {
        let y = &self.y;
        let mut out = Vec::new();
        for b in [
            working::new_divergence("The small-balance threshold")
                .form(
                    format!(
                        "The {} IA 2210 charges nothing below {} of tax after credits. It also charges nothing below {} of tax after withholding. It was printed before the law raised the threshold for {}.",
                        y.year - 1,
                        y.form_threshold.whole(),
                        y.form_threshold.whole(),
                        y.year
                    ),
                    "2025 IA 2210 (45-007a, 10/22/2025), lines 3 and 6; no 2026 IA 2210 is published",
                )
                .statute(
                    format!(
                        "From {}, no payments are needed below {} of Iowa tax after withholding. No penalty can arise below it either.",
                        y.year,
                        y.threshold.whole()
                    ),
                    "Iowa Code § 422.16(12)(a)(1) (2026), amended by 2025 Iowa Acts ch. 148, § 59, effective January 1, 2026, and applying under § 60 to tax years beginning on or after that date",
                )
                .uses(Reading::Statute)
                .affects(threshold),
            working::new_divergence("The daily interest factor")
                .form(
                    "Each day is charged the printed factor of .00027397. That is 10% divided by 365, rounded to eight places.",
                    "2025 IA 2210 (45-007b), lines 24a and 24b, and its instructions for line 24 (01/01/26 - 12/31/26)",
                )
                .statute(
                    format!(
                        "The charge is Iowa's interest rate for the year, 10% for {}. Divided by 365, that is 0.000273973 a day.",
                        y.year
                    ),
                    "Iowa Code §§ 422.16(12)(d)(3), 421.7(2); IDR press release, October 21, 2025",
                )
                .uses(Reading::Form)
                .affects(factor),
            working::new_divergence("The waiver for disability in the year before")
                .form(
                    "The waiver covers retiring at 62 or later, or disability, in the tax year. That is the year the payments were for. Disability in the year before isn't mentioned.",
                    "2025 IA 2210 instructions (45-007d), Waiver of Penalty",
                )
                .statute(
                    "The waiver also covers disability in the year before, with reasonable cause.",
                    "Iowa Admin. Code r. 701—308.6(2)(b), implementing Iowa Code § 422.16; IDR, Estimated Income Tax Payments, Can Penalty Be Waived?",
                )
                .uses(Reading::Form)
                .affects(waiver),
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
        let mut step = y.prior_year_step;
        if f.filing_status() == Status::MarriedFilingSeparately {
            step = y.prior_year_step_separate;
        }
        let tax_opt = f.this_year().tax();
        let tax_known = tax_opt.is_some();
        let tax = tax_opt.unwrap_or_default();
        a.tax_known = tax_known;
        let withheld = f.withheld_total();
        let prior_opt = f.prior_year();
        let has_prior = prior_opt.is_some();
        let prior = prior_opt.unwrap_or_default();
        let last = y.year - 1;

        // IA 2210 lines 3 and 4: 90% of the tax after refundable credits.
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
                "Iowa Code § 422.16(12)(d)(3); 2025 IA 2210, lines 3 and 4",
                format!(
                    "{} of this year's Iowa tax after refundable credits of {} is {}",
                    y.this_year_share,
                    tax,
                    this_year.amount()
                ),
            );
        }
        // IA 2210 line 7: last year's tax, 100% or 110%. A twelve-month year
        // with no tax, or one with no return because none was required,
        // gives a required payment of nothing.
        let mut no_liability = false;
        let unavailable = |why: String| -> TargetOptionBuilder {
            let mut b =
                estimated::new_target_option(Basis::PriorYearTax, y.prior_year_share, USCents(0));
            b.unavailable(why);
            b
        };
        let prior_option = if !has_prior {
            unavailable(format!("Needs your {last} Iowa return"))
        } else if prior.months() != 12 {
            unavailable(format!(
                "Your {last} Iowa return covered {} months, not 12",
                prior.months()
            ))
        } else if prior.not_required_to_file() {
            no_liability = true;
            estimated::new_target_option(Basis::PriorYearTax, y.prior_year_share, USCents(0))
        } else if !prior.filed() {
            unavailable(format!("You didn't file a {last} Iowa return"))
        } else {
            let pf = prior.figures().unwrap_or_default();
            let prior_tax = pf.tax().unwrap_or_default();
            if prior_tax.0 == 0 {
                no_liability = true;
                estimated::new_target_option(Basis::PriorYearTax, y.prior_year_share, USCents(0))
            } else {
                let agi = pf.federal_agi();
                let mods = extras.prior_year_modifications();
                let mut share = y.prior_year_share;
                let applied = match (agi, mods) {
                    (None, _) => {
                        share = y.prior_year_share_high;
                        a.notes.ask(
                            PositionKind::AdjustedGrossIncome,
                            format!("What was your {last} federal AGI?"),
                            format!(
                                "It's {} if AGI plus Iowa net modifications tops {}, and {} otherwise. Until then, last year's test uses {}.",
                                y.prior_year_share_high,
                                step.whole(),
                                y.prior_year_share,
                                y.prior_year_share_high
                            ),
                        );
                        format!("{last} federal AGI was not given")
                    }
                    (Some(_), None) => {
                        share = y.prior_year_share_high;
                        a.notes.ask(
                            PositionKind::JurisdictionFact,
                            format!(
                                "What were your {last} Iowa net modifications (IA 1040 line 3)?"
                            ),
                            format!(
                                "With federal AGI they set last year's test, {} or {}; until then it's {}.",
                                y.prior_year_share, y.prior_year_share_high, y.prior_year_share_high
                            ),
                        );
                        format!("{last} Iowa net modifications were not given")
                    }
                    (Some(agi), Some(mods)) if agi + mods > step => {
                        share = y.prior_year_share_high;
                        format!(
                            "{last} federal AGI of {agi} plus {mods} of Iowa net modifications is more than {step}"
                        )
                    }
                    (Some(agi), Some(mods)) => format!(
                        "{last} federal AGI of {agi} plus {mods} of Iowa net modifications is not more than {step}"
                    ),
                };
                let amount = share.of(prior_tax);
                a.notes.rule(
                    "Iowa Code § 422.16(12)(d)(3); 2025 IA 2210, line 7 and How to Avoid Underpayment Penalty",
                    format!(
                        "{applied}, so the prior-year test is {share} of the {prior_tax} tax on the {last} return, or {amount}"
                    ),
                );
                estimated::new_target_option(Basis::PriorYearTax, share, amount)
            }
        };
        let prior_opt = prior_option.build()?;

        let mut rb = estimated::new_requirement();
        rb.option(this_year.clone()).option(prior_opt.clone());
        let (mut chosen, mut target) = (Basis::ThisYearTax, this_year.amount());
        if prior_opt.available() && (!tax_known || prior_opt.amount() < target) {
            (chosen, target) = (Basis::PriorYearTax, prior_opt.amount());
        }
        if !tax_known && !no_liability {
            a.notes.ask(
                PositionKind::ThisYearTax,
                format!("What do you expect your {} Iowa tax to be?", y.year),
                format!(
                    "It sets the {} option and decides whether you're under the {} threshold.",
                    y.this_year_share,
                    y.threshold.whole()
                ),
            );
        }
        if no_liability {
            a.exempt = true;
            let none = estimated::new_exception("No Iowa tax due last year").build()?;
            rb.required(
                false,
                format!(
                    "You had no Iowa tax due for {last}, a full twelve-month year. No estimated payments are needed for {}, and no penalty can arise.",
                    y.year
                ),
            )
            .exempt(none);
            a.notes.rule(
                "Iowa Code § 422.16(12)(d)(3); Iowa Admin. Code r. 701—308.6(1)(a); 2025 IA 2210, lines 7 and 8",
                format!(
                    "the {last} year covered 12 months with no Iowa tax, so the smaller of the two tests is nothing and there is no penalty for {}",
                    y.year
                ),
            );
        } else if !tax_known && !prior_opt.available() {
            rb.required(
                false,
                format!(
                    "Your target needs your {} Iowa tax, because your {last} return can't set one.",
                    y.year
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
                    "Your target is {} of your {last} tax, {}. After {} of withholding, that's {} of estimated payments. That keeps you clear of a penalty whatever your {} tax is.",
                    prior_opt.percent(),
                    target.whole(),
                    withheld.whole(),
                    (target - withheld).whole(),
                    y.year
                ),
            )
            .choose(chosen);
        } else if tax - withheld < y.threshold {
            a.exempt = true;
            let under =
                estimated::exception_below("Tax less withholding", tax - withheld, y.threshold)?;
            let mut leaves = format!(
                "is {}, under {}",
                (tax - withheld).whole(),
                y.threshold.whole()
            );
            let n = near(tax - withheld, y.threshold);
            if n != (tax - withheld).whole() {
                leaves = format!("is {n}");
            }
            if tax < withheld {
                leaves = estimated::leaves(tax - withheld);
            }
            rb.required(
                false,
                format!(
                    "Your Iowa tax of {} less {} withheld {leaves}. No estimated payments are needed, and no penalty can arise.",
                    tax.whole(),
                    withheld.whole()
                ),
            )
            .exempt(under);
            a.notes.rule(
                "Iowa Code § 422.16(12)(a)(1), as amended by 2025 Iowa Acts ch. 148, § 59; 2026 IA 1040ES, line 15",
                format!(
                    "{tax} of tax less {withheld} withheld is {}, under {}, so no estimated payments are required and there is no penalty",
                    tax - withheld,
                    y.threshold
                ),
            );
        } else if withheld >= target {
            rb.required(
                false,
                format!(
                    "Your Iowa withholding of {} already reaches your target of {}. No estimated payments are needed.",
                    withheld.whole(),
                    target.whole()
                ),
            )
            .choose(chosen);
        } else {
            rb.required(
                true,
                format!(
                    "Your Iowa tax of {} less {} withheld is {}, at least {}. You need {} of estimated payments across the year. That reaches your target of {}.",
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
        // Under an exception last year's return can't change the target.
        if !has_prior && !a.exempt {
            a.notes.ask(
                PositionKind::PriorYearReturn,
                format!("Did you file a {last} Iowa return, and what was its tax?"),
                format!(
                    "With it, your target could be {} of last year's tax, not {} of this year's.",
                    y.prior_year_share, y.this_year_share
                ),
            );
        }
        // The due dates: § 421.9A moves one on a weekend or holiday.
        a.dues = estimated::move_dates(&y.calendar, &y.dues);
        for (i, due) in a.dues.iter().enumerate() {
            if *due != y.dues[i] {
                a.notes.rule(
                    "Iowa Code § 421.9A; 2026 IA 1040ES, Due dates",
                    format!(
                        "the installment due {} falls on a weekend or holiday, so it is due {}",
                        y.dues[i].long(),
                        due.long()
                    ),
                );
            }
        }
        // A late starter: the IA 2210 table.
        let first_met = f.requirement_first_met();
        if !first_met.is_zero() {
            a.first = estimated::late_start(first_met, &y.later_groups);
        } else if a.requirement.required() {
            a.notes.ask(
                PositionKind::RequirementFirstMet,
                format!(
                    "When did you first expect to need estimated payments for {}?",
                    y.year
                ),
                format!(
                    "If it was after {}, your installments are fewer and larger. They start at the next due date.",
                    short_date(y.later_groups[0].add_days(-1), y.year)
                ),
            );
        }
        if a.exempt {
            a.installments = vec![USCents(0); a.dues.len()];
            a.first = 0;
        } else {
            let n = a.dues.len() as i64 - a.first;
            let cum = estimated::equal_shares(target, a.dues.len() as i64, a.first)?;
            a.installments = estimated::increments(&cum);
            if a.first == 0 {
                a.notes.rule(
                    "Iowa Code § 422.16(12)(b)(1); 2026 IA 1040ES, line 17 and Estimated Tax Payment Schedule; 2025 IA 2210, line 17",
                    format!(
                        "a quarter of {target}, {}, is due on each of {}, {}, {} and {}",
                        a.installments[0],
                        a.dues[0].long(),
                        a.dues[1].long(),
                        a.dues[2].long(),
                        a.dues[3].long()
                    ),
                );
            } else {
                a.notes.rule(
                    "2025 IA 2210, Period Requirements First Met table and line 17",
                    format!(
                        "the requirement was first met on {}, so {target} is divided among {n} installments, the first due {}; nothing is due before it",
                        first_met.long(),
                        a.dues[a.first as usize].long()
                    ),
                );
            }
        }
        // Whether each divergence changes anything for these facts.
        let threshold =
            tax_known && tax - withheld >= y.form_threshold && tax - withheld < y.threshold;
        let mut factor = false;
        if !a.exempt && a.requirement.target_known() {
            factor = self.factor_affects(f, &a)?;
        }
        // The readings differ on the waiver only for disability in the year
        // before.
        let pos = f.positions();
        let waiver = pos.waiver().unwrap_or_default();
        let disabled_year_before = waiver == WaiverKind::RetiredOrDisabled
            && pos.waiver_cause() == WaiverCause::Disability
            && pos.waiver_on().year() == last;
        let divergences = self.divergences(threshold, factor, !a.exempt && disabled_year_before)?;
        a.notes.diverges(&divergences);
        Ok(a)
    }

    /// Whether the printed daily factor and the rate it stands for give a
    /// different penalty, figured to the latest day the penalty can run to.
    fn factor_affects(&self, f: &Facts, a: &Assessment) -> Result<bool, Error> {
        let y = &self.y;
        let (ledger, _) = self.ledger(f, a)?;
        let mut stop = y.stop;
        let filed = f.return_filed();
        if !filed.is_zero() && filed.before(stop) {
            stop = filed;
        }
        let mut rb = estimated::new_rate_series();
        for p in y.daily.periods() {
            rb.period(p.from(), p.to(), p.rate(), p.source());
        }
        let rates = rb.build()?;
        let (printed, _) = self.lines(&ledger, stop, &|principal, from, to| {
            estimated::daily_factor(principal, from, to, &y.daily)
        })?;
        let (exact, _) = self.lines(&ledger, stop, &|principal, from, to| {
            estimated::simple_daily(principal, from, to, &rates)
        })?;
        Ok(total(&printed) != total(&exact))
    }

    /// Charges each piece of each shortfall from its due date to the day it
    /// was paid or to `stop`, by `charge`.
    fn lines(
        &self,
        ledger: &[LedgerEntry],
        stop: Date,
        charge: Charge<'_>,
    ) -> Result<(Vec<PenaltyLine>, Date), Error> {
        let mut out = Vec::new();
        let mut unrated = Date::default();
        for l in ledger {
            if l.shortfall().0 <= 0 {
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
                let (parts, gap) = charge(piece.amount(), l.due(), to)?;
                if !gap.is_zero() && (unrated.is_zero() || gap.before(unrated)) {
                    unrated = gap;
                }
                for p in parts {
                    lb.part(p);
                }
            }
            out.push(lb.days(last_day.days_since(l.due())).build()?);
        }
        Ok((out, unrated))
    }

    /// Applies the credits to the installments.
    fn ledger(&self, f: &Facts, a: &Assessment) -> Result<(Vec<LedgerEntry>, Notes), Error> {
        let credits = if f.elections().withholding_as_withheld() {
            estimated::credits_as_withheld(f, &a.dues)?
        } else {
            estimated::credits(f, &a.dues, &QUARTERS)?
        };
        let ledger = estimated::apply(&a.dues, &a.installments, &credits)?;
        let mut notes = Notes::default();
        if f.elections().withholding_as_withheld() {
            notes.rule(
                "2025 IA 2210, line 20 instructions",
                format!(
                    "by election, the {} withheld is credited on the dates it was withheld",
                    f.withheld_total()
                ),
            );
        } else {
            notes.rule(
                "2025 IA 2210, line 20 instructions",
                format!(
                    "the {} withheld is treated as paid in four equal parts on the due dates, whenever it was actually withheld",
                    f.withheld_total()
                ),
            );
        }
        notes.rule(
            "2025 IA 2210, line 21 instructions",
            "each payment goes to the earliest unpaid installment, whichever one it was meant for, and any excess is carried to the next",
        );
        Ok((ledger, notes))
    }

    /// Decides a waiver asked for under Iowa Admin. Code r. 701—308.6(2) and
    /// the 2025 IA 2210 instructions: a casualty, disaster or other unusual
    /// circumstance; or retirement at 62 or later, or disability, in the tax
    /// year, with reasonable cause. The form's tax year is followed, and the
    /// rule's preceding year for disability is the waiver divergence.
    /// Whether retiring in the preceding year counts is open in both, and is
    /// asked. It returns `None` when no waiver was asked for.
    fn waiver_outcome(&self, f: &Facts, notes: &mut Notes) -> Option<WaiverOutcome> {
        const AUTHORITY: &str = "the Iowa Department of Revenue";
        const AUTHORITY_TITLE: &str = "The Iowa Department of Revenue";
        let pos = f.positions();
        match pos.waiver() {
            None | Some(WaiverKind::None) => return None,
            Some(WaiverKind::Casualty) => return estimated::standard_waiver(f, AUTHORITY, 62),
            Some(WaiverKind::RetiredOrDisabled) => {}
        }
        let (year, on) = (self.y.year, pos.waiver_on());
        if pos.waiver_cause() == WaiverCause::Disability {
            if on.is_zero() {
                return Some(estimated::waiver_undecided(format!(
                    "The disability waiver needs the date you became disabled, which must be in {year}."
                )));
            }
            if on.year() != year {
                return Some(estimated::waiver_refused(format!(
                    "You became disabled in {}. Iowa's form waives the penalty only for becoming disabled in {year}.",
                    on.year()
                )));
            }
            return Some(estimated::waiver_eligible(format!(
                "You became disabled in {}. {AUTHORITY_TITLE} can waive the penalty for reasonable cause.",
                on.year()
            )));
        }
        if on.is_zero() {
            return Some(estimated::waiver_undecided(format!(
                "The waiver for retirement needs the date you retired, which must be in {year}."
            )));
        }
        if on.year() == year - 1 {
            notes.ask(
                PositionKind::UnsettledRule,
                "",
                format!(
                    "Iowa's form waives the penalty for retiring at 62 or later in {year}. Neither it nor Iowa's rule says whether retiring in {} also counts.",
                    year - 1
                ),
            );
            return Some(estimated::waiver_undecided(format!(
                "You retired in {}. Iowa hasn't said whether its waiver covers retiring in the year before.",
                on.year()
            )));
        }
        if on.year() != year {
            return Some(estimated::waiver_refused(format!(
                "You retired in {}, and Iowa's waiver covers retiring in {year}.",
                on.year()
            )));
        }
        let born = pos.born();
        if born.is_zero() {
            return Some(estimated::waiver_undecided(
                "The retirement waiver needs your date of birth. It shows you were 62 or older when you retired.",
            ));
        }
        let mut age = on.year() - born.year();
        if on.month().0 < born.month().0
            || (on.month().0 == born.month().0 && on.day() < born.day())
        {
            age -= 1;
        }
        if age < 62 {
            return Some(estimated::waiver_refused(format!(
                "You were {age} when you retired. Iowa's waiver needs you to have been 62 or older."
            )));
        }
        Some(estimated::waiver_eligible(format!(
            "You retired in {} at {age}. {AUTHORITY_TITLE} can waive the penalty for reasonable cause.",
            on.year()
        )))
    }
}

impl EstimatedTax for IowaEstimated {
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
                    "with no {} Iowa return to measure against, there is no target to schedule",
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

    fn penalty(&self, f: &Facts, as_of: Date) -> Result<working::Result<Penalty>, Error> {
        if as_of.is_zero() {
            return Err(Error::Invalid(
                "iowataxcalc: a penalty needs the date to figure it to".into(),
            ));
        }
        let a = self.assess(f)?;
        if !a.requirement.target_known() {
            return Err(Error::with_detail(
                Error::NeedsThisYearTax,
                format!(
                    "with no {} Iowa return to measure against, there is no target to figure a penalty from",
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
        let (ledger, mut notes) = self.ledger(f, &a)?;
        let sched = schedule_of(&ledger)?;
        if a.exempt {
            let p = estimated::new_penalty(stop)
                .waiver(waiver)
                .because(a.requirement.clone(), &sched)
                .build()?;
            return Ok(working::new_result(p).with(&a.notes).build()?);
        }
        let mut pb = estimated::new_penalty(stop);
        pb.waiver(waiver).because(a.requirement.clone(), &sched);
        if !a.tax_known {
            pb.upper_bound();
        }
        let (lines, unrated) = self.lines(&ledger, stop, &|principal, from, to| {
            estimated::daily_factor(principal, from, to, &y.daily)
        })?;
        for l in lines {
            pb.line(l);
        }
        let mut p = pb.build()?;
        if !a.tax_known {
            notes.rule(
                "Iowa Code § 422.16(12)(d)(3); 2025 IA 2210, line 8",
                format!(
                    "this year's Iowa tax was not given, so the penalty is figured against {} of the {} tax alone; the required payment is the smaller of that and 90% of this year's tax, so {} is the most the penalty can be, and it is less if this year's tax turns out lower",
                    a.requirement.options()[1].percent(),
                    y.year - 1,
                    p.total()
                ),
            );
        }
        if paid_in_full {
            notes.rule(
                "2025 IA 2210, line 22 instructions",
                format!(
                    "the return was filed with the whole balance paid on {}, so every shortfall still outstanding was paid that day",
                    stop.long()
                ),
            );
        }
        notes.rule(
            "Iowa Code §§ 422.16(12)(d)(3), 421.7; 2025 IA 2210, lines 23a to 26",
            format!(
                "each shortfall is charged the printed daily factor for its calendar year, to the cent, for the days after its due date until paid or {}; the penalty to {} is {}",
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
                    short_date(unrated.add_days(-1), y.year)
                ),
            );
        }
        if p.total().0 > 0 && waiver_answer.is_none() {
            notes.ask(
                PositionKind::PenaltyWaiver,
                "Are you asking for a waiver for a casualty, disaster, retirement or disability?",
                "Retiring must be at 62 or later; it and disability need reasonable cause. Other unusual events count; the figures show the full penalty until a waiver.",
            );
        }
        if p.total().0 > 0
            && let Some(o) = self.waiver_outcome(f, &mut notes)
        {
            p = p.to_builder().waiver_outcome(o).build()?;
        }
        Ok(working::new_result(p).with(&a.notes).with(&notes).build()?)
    }

    fn explanation(&self) -> Explanation {
        self.explanation.clone()
    }
}

/// Written by hand and checked against research/estimated-payments/ia.md.
fn iowa_explanation() -> Result<Explanation, Error> {
    estimated::new_explanation(
        "Iowa expects estimated payments once you expect $1,000 or more of tax due. That is Iowa tax beyond your withholding for 2026, up from $200 before. The penalty is interest, by the day, on each installment's shortfall.",
    )
    .paragraph("You avoid the penalty if withholding and estimated payments reach a target. It is the smaller of 90% of this year's Iowa tax or 100% of last year's. The 100% becomes 110% if federal AGI plus Iowa net modifications tops $150,000. The limit is $75,000 if married filing separately. Last year must be a full twelve months. If you had no Iowa tax due for it, no penalty is due this year.")
    .paragraph("A quarter is due at the end of each of April, June, September and January. For 2026 that is April 30, June 30 and September 30, and February 1, 2027. January 31 is a Sunday. Withholding is spread evenly over those dates unless you show when it was taken. If you first expected to need payments after April 1, start at the next date. The year's amount is then split over fewer, larger installments.")
    .paragraph("The penalty is charged at Iowa's interest rate, 10% a year for 2026. It is charged for each day an installment stays short. It stops on April 30 of the next year, when the return is due. A payment goes to the oldest shortfall first. The department can waive the penalty for a casualty or disaster. It can also do so if you retired at 62 or later or became disabled. That needs reasonable cause.")
    .versus_federal("Iowa uses the federal method with its own figures and dates. Payments fall at the end of April, June, September and January. The 110% test adds Iowa's net modifications to federal AGI. A late starter pays in fewer installments. The rate is set yearly, not quarterly. The penalty runs to April 30, not April 15. Farmers and fishers use IA 2210F.")
    .source("Iowa Code §§ 422.16(12), 421.7, 421.9A; 2025 Iowa Acts ch. 148, §§ 59, 60")
    .source("Iowa Admin. Code r. 701—308.6")
    .source("2026 IA 1040ES, Iowa Individual Estimated Income Tax Instructions")
    .source("2025 IA 2210, Iowa Underpayment of Estimated Tax by Individuals, and its instructions")
    .source("Iowa Department of Revenue, IDR Announces 2026 Individual Income Tax and Interest Rates (October 21, 2025)")
    .build()
}
