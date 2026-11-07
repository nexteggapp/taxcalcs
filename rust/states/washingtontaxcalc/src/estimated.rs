//! Washington's payment rules for its capital gains excise tax, RCW
//! 82.87.110 and 82.87.115 with the penalties and interest of RCW 82.32.090
//! and 82.32.050, as the shared [`EstimatedTax`] trait.
//!
//! Washington has no quarterly estimated tax. The whole capital gains tax is
//! due in one payment on the federal return's due date, and a filing
//! extension does not move it. So there is one installment, no prior-year
//! safe harbor, no withholding and no annualised method. The penalty is not
//! interest on a shortfall: it is a late payment penalty of 9%, 19% or 29% by
//! lateness, or, for a filer with an extension who paid an estimate on time,
//! 5% of the whole tax when the estimate was under 80% of it. Interest runs
//! separately from the end of April.
//!
//! The figures follow the Department of Revenue where its guidance is
//! current, and the statute where the guidance predates SSB 5314 (chapter
//! 409, Laws of 2025). Each result's Divergences says where the two part
//! ways.

use std::any::Any;
use std::sync::LazyLock;

use estimated::{
    Basis, Calendar, Date, Error, EstimatedTax, Explanation, Facts, Installment, Jurisdiction,
    Month, PartKind, Penalty, RateSeries, Requirement, WaiverCause, WaiverKind, WaiverOutcome,
};
use money::{Rate, USCents, percent};
use working::{Divergence, Notes, PositionKind, Reading};

use crate::estimated_y2026::year_2026;

/// Washington's code.
const JURISDICTION: Jurisdiction = estimated::WASHINGTON;

/// The facts Washington's rules read that no other jurisdiction does. Carry
/// them in [`Facts::state`].
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct EstimatedFacts {
    extension: Option<bool>,
}

impl EstimatedFacts {
    /// Whether the filer had a federal filing extension and confirmed it with
    /// the department through My DOR by the due date (RCW 82.87.110(5); WAC
    /// 458-20-300(3)), or `None` when the caller did not say. It decides
    /// which penalty applies to tax paid after the due date.
    pub fn extension(&self) -> Option<bool> {
        self.extension
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
    /// Answers the extension question. False is an answer.
    pub fn extension(&mut self, granted: bool) -> &mut Self {
        self.e.extension = Some(granted);
        self
    }

    /// Has nothing to refuse: the one fact is a yes or a no.
    pub fn build(&self) -> Result<EstimatedFacts, Error> {
        Ok(self.e)
    }
}

/// One tax year's published figures.
#[derive(Clone, Debug)]
pub(crate) struct EstimatedYear {
    pub(crate) year: i64,

    /// The payment due date before any move for a weekend or holiday.
    pub(crate) due: Date,
    /// The first day a payment can be made.
    pub(crate) prepay_from: Date,

    /// The legal holidays that move a due date.
    pub(crate) calendar: Calendar,

    /// The RCW 82.32.090(1) totals: on the due date, after the last day of
    /// the next month, and after the last day of the month after that.
    pub(crate) late_totals: Vec<Rate>,
    /// The least that penalty can be.
    pub(crate) late_minimum: USCents,

    /// An estimated payment under `underpaid_share` of the tax, and short by
    /// at least `underpaid_floor`, owes `underpaid_rate` of the whole tax:
    /// RCW 82.32.090(10).
    pub(crate) underpaid_share: Rate,
    pub(crate) underpaid_floor: USCents,
    pub(crate) underpaid_rate: Rate,

    /// The day after which interest runs.
    pub(crate) interest_from: Date,
    /// The yearly rates.
    pub(crate) interest: RateSeries,
}

/// Returns Washington's capital gains tax payment rules for a tax year, or
/// [`Error::NoRules`] for a year this build does not carry.
pub fn estimated_tax(year: i64) -> Result<Box<dyn EstimatedTax>, Error> {
    let y = match year {
        2026 => year_2026()?,
        _ => return Err(estimated::no_rules(&JURISDICTION, year)),
    };
    Ok(Box::new(WashingtonEstimated { y }))
}

struct WashingtonEstimated {
    y: EstimatedYear,
}

/// What the three questions share.
#[derive(Clone, Debug, Default)]
struct Assessment {
    requirement: Requirement,
    tax: USCents,
    tax_known: bool,
    due: Date,
    extension: bool,
    paid_by_due: USCents,
    notes: Notes,
}

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

/// The first three letters of the month's name.
fn abbrev(m: Month) -> String {
    m.to_string().chars().take(3).collect()
}

impl WashingtonEstimated {
    /// Writes `d` as "Oct 15" inside the tax year and "Apr 15, 2027" outside
    /// it.
    fn short(&self, d: Date) -> String {
        if d.year() == self.y.year {
            return format!("{} {}", abbrev(d.month()), d.day());
        }
        format!("{} {}, {}", abbrev(d.month()), d.day(), d.year())
    }

    fn extras(&self, f: &Facts) -> Result<EstimatedFacts, Error> {
        match f.state() {
            None => Ok(EstimatedFacts::default()),
            Some(s) => match s.downcast_ref::<EstimatedFacts>() {
                Some(e) => Ok(*e),
                None => Err(Error::Invalid(format!(
                    "washingtontaxcalc: the facts carry {}, not washingtontaxcalc.EstimatedFacts",
                    type_name(s)
                ))),
            },
        }
    }

    fn check(&self, f: &Facts) -> Result<(), Error> {
        let y = &self.y;
        if f.jurisdiction() != JURISDICTION {
            return Err(Error::Invalid(format!(
                "washingtontaxcalc: these are Washington's rules, and the facts are for {}",
                f.jurisdiction()
            )));
        }
        if f.tax_year() != y.year {
            return Err(Error::Invalid(format!(
                "washingtontaxcalc: these are the {} rules, and the facts are for {}",
                y.year,
                f.tax_year()
            )));
        }
        if !f.withholding().is_empty() {
            return Err(Error::Invalid(
                "washingtontaxcalc: Washington's capital gains tax has no withholding; tax paid toward it is a payment".into(),
            ));
        }
        if f.elections().annualised() {
            return Err(Error::Invalid(
                "washingtontaxcalc: Washington's capital gains tax is one payment, so there is nothing to annualise".into(),
            ));
        }
        for p in f.payments() {
            if p.from_prior_year() {
                return Err(Error::with_detail(
                    Error::Unsupported,
                    format!(
                        "applying a {} overpayment to the {} capital gains tax is not built; the research found no rule for it",
                        y.year - 1,
                        y.year
                    ),
                ));
            }
            if p.on().before(y.prepay_from) {
                return Err(Error::Invalid(format!(
                    "washingtontaxcalc: a payment on {} is before {}, the first day RCW 82.87.115(1) allows one for {}",
                    p.on().long(),
                    y.prepay_from.long(),
                    y.year
                )));
            }
        }
        Ok(())
    }

    /// Where the department's guidance and the statute part ways on a rule
    /// these computations use. Two of the department's documents predate SSB
    /// 5314, so for those the statute is followed. Each affects the household
    /// only when its facts reach the rule: an extension with a payment by the
    /// due date for the first, a payment under 80% of the tax with one for
    /// the second, and tax unpaid at the due date for the third.
    fn divergences(&self, a: &Assessment) -> Result<Vec<Divergence>, Error> {
        let short = a.tax_known && a.paid_by_due < a.tax;
        let estimate = a.tax_known && a.extension && a.paid_by_due.0 > 0 && short;
        let low =
            estimate && a.paid_by_due.0 * 10_000 < i64::from(self.y.underpaid_share.0) * a.tax.0;
        let mut out = Vec::new();
        for b in [
            working::new_divergence("Late payment penalty after a timely estimated payment")
                .form(
                    "The department charges no late payment penalty on a balance paid later. That needs an extension, and your own estimate paid by the original due date. Only interest runs on that balance.",
                    "Department of Revenue, Capital gains tax page, Payments (retrieved 2026-10-20); Interim statement regarding late payment penalties (April 7, 2023)",
                )
                .statute(
                    "After the due date, interest and the usual penalties apply to the balance. Read alone, that includes the 9%, 19% and 29% late payment penalty.",
                    "RCW 82.87.110(3); RCW 82.32.090(1)",
                )
                .uses(Reading::Form)
                .affects(estimate),
            working::new_divergence("The penalty on a low estimated payment")
                .form(
                    "A 2023 department statement charges the assessment penalty for a low payment. Low means under 80% of the tax and at least $1,000 short. The penalty is 5% of the tax it finds due, rising to 15% and 25% if unpaid. Its web page gives the 80% test with no $1,000 floor. Both predate the 2025 act.",
                    "Department of Revenue, Interim statement regarding late payment penalties (April 7, 2023); Capital gains tax page, Payments (retrieved 2026-10-20)",
                )
                .statute(
                    "The penalty is 5% of the actual tax due. It applies if your estimated payment was under 80% of that tax. You must also have been at least $1,000 short. The 2025 act added this from July 27, 2025.",
                    "RCW 82.32.090(10), added by SSB 5314, chapter 409, Laws of 2025, § 14, in effect from July 27, 2025",
                )
                .uses(Reading::Statute)
                .affects(low),
            working::new_divergence("When interest starts")
                .form(
                    "The department's rules start interest on Jan 31 of the next year. One of their examples charges interest on a payment two days late. Both rules were certified before the 2025 act took effect.",
                    "WAC 458-20-228(7)(a) (certified 2/29/2024); WAC 458-20-300(4)(a), Example 3 (certified 8/23/2023)",
                )
                .statute(
                    "Interest on unpaid capital gains tax runs from the last day of the next April.",
                    "RCW 82.32.050(1)(c)(ii), added by SSB 5314, chapter 409, Laws of 2025, § 12, in effect from July 27, 2025",
                )
                .uses(Reading::Statute)
                .affects(short),
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
            due: y.calendar.next_business_day(y.due),
            ..Assessment::default()
        };
        if a.due != y.due {
            a.notes.rule(
                "RCW 1.12.070(3); WAC 458-20-228(4)(a)",
                format!(
                    "{} falls on a weekend or legal holiday, so the payment is due {}",
                    y.due.long(),
                    a.due.long()
                ),
            );
        }
        let tax = f.this_year().tax();
        a.tax = tax.unwrap_or_default();
        a.tax_known = tax.is_some();
        let extension = extras.extension();
        let answered = extension.is_some();
        a.extension = extension.unwrap_or(false);
        a.paid_by_due = paid_by(f, a.due);
        let divergences = self.divergences(&a)?;
        a.notes.diverges(&divergences);

        let this_year = if a.tax_known {
            estimated::new_target_option(Basis::ThisYearTax, percent(100), a.tax).build()?
        } else {
            estimated::new_target_option(Basis::ThisYearTax, percent(100), USCents(0))
                .unavailable(estimated::NEEDS_THIS_YEAR_TAX)
                .build()?
        };
        let prior_year = estimated::new_target_option(Basis::PriorYearTax, Rate(0), USCents(0))
            .label("Last year's tax")
            .unavailable("Washington's capital gains tax has no prior-year safe harbor")
            .build()?;

        let mut rb = estimated::new_requirement();
        rb.option(this_year).option(prior_year);
        if !a.tax_known {
            rb.required(
                false,
                format!(
                    "Washington has no prior-year safe harbor. Your target is your whole {} capital gains tax. It is due in one payment by {}.",
                    y.year,
                    self.short(a.due)
                ),
            )
            .no_target();
            a.notes.ask(
                PositionKind::ThisYearTax,
                format!(
                    "What do you expect your {} Washington capital gains tax to be?",
                    y.year
                ),
                format!(
                    "Without it there's no target. All of it is due by {}, and no other figure stands in for it.",
                    self.short(a.due)
                ),
            );
        } else if a.tax.0 == 0 {
            rb.required(
                false,
                format!(
                    "With no Washington capital gains tax for {}, nothing is due on {}.",
                    y.year,
                    self.short(a.due)
                ),
            )
            .choose(Basis::ThisYearTax);
        } else {
            rb.required(
                true,
                format!(
                    "Washington has no quarterly estimated payments for its capital gains tax. Your whole {} tax of {} is due in one payment by {}. You can pay it from {}.",
                    y.year,
                    a.tax.whole(),
                    self.short(a.due),
                    self.short(y.prepay_from)
                ),
            )
            .choose(Basis::ThisYearTax);
        }
        a.requirement = rb.build()?;

        if a.tax_known {
            a.notes.rule(
                "RCW 82.87.110(3); WAC 458-20-300(4)(a)",
                format!(
                    "the whole {} is due on or before {}, the federal return's due date, whatever extension is granted",
                    a.tax,
                    a.due.long()
                ),
            );
        }
        a.notes.rule(
            "RCW 82.87.115(1); Department of Revenue special notice, Prepayment of capital gains tax (August 3, 2026)",
            format!(
                "a payment for {} can be made from {}, six months before the due date",
                y.year,
                y.prepay_from.long()
            ),
        );
        if !answered && !(a.tax_known && a.tax.0 == 0) {
            a.notes.ask(
                PositionKind::JurisdictionFact,
                format!(
                    "Did you have a filing extension confirmed with the department by {}?",
                    self.short(a.due)
                ),
                "With one, and a payment by then, a later balance has no late payment penalty.",
            );
        }
        Ok(a)
    }
}

/// The total paid on or before `d`.
fn paid_by(f: &Facts, d: Date) -> USCents {
    f.payments()
        .iter()
        .filter(|p| !p.on().after(d))
        .map(|p| p.amount())
        .sum()
}

impl Assessment {
    /// The one payment: the whole tax, due on the due date.
    fn installment(&self) -> Result<Installment, Error> {
        estimated::new_installment(1, self.due)
            .needed_by(self.tax)
            .credited_by(self.paid_by_due)
            .shortfall((self.tax - self.paid_by_due).max(USCents(0)))
            .build()
    }
}

/// The last day of the nth month after `d`'s month.
fn last_day_of_month_after(d: Date, n: i64) -> Date {
    estimated::date_of(d.year(), Month(d.month().0 + n + 1), 0)
}

/// Part of the shortfall and the day it stopped being unpaid: the day it
/// was paid, or the day the penalty is figured to.
#[derive(Clone, Copy, Debug)]
struct Piece {
    amount: USCents,
    until: Date,
}

/// How much of the pieces was still unpaid at the end of `d`.
fn unpaid_after(pieces: &[Piece], d: Date) -> USCents {
    pieces
        .iter()
        .filter(|p| p.until.after(d))
        .map(|p| p.amount)
        .sum()
}

impl EstimatedTax for WashingtonEstimated {
    fn requirement(&self, f: &Facts) -> Result<working::Result<Requirement>, Error> {
        let a = self.assess(f)?;
        Ok(working::new_result(a.requirement).with(&a.notes).build()?)
    }

    fn schedule(&self, f: &Facts) -> Result<working::Result<Vec<Installment>>, Error> {
        let a = self.assess(f)?;
        if !a.requirement.target_known() {
            return Err(Error::with_detail(
                Error::NeedsThisYearTax,
                "Washington has no other figure to schedule against",
            ));
        }
        let inst = a.installment()?;
        let mut notes = Notes::default();
        notes.rule(
            "RCW 82.87.110(3)",
            format!(
                "one payment of {} is due {}; {} was paid by then, so {} is short",
                a.tax,
                a.due.long(),
                a.paid_by_due,
                inst.shortfall()
            ),
        );
        Ok(working::new_result(vec![inst])
            .with(&a.notes)
            .with(&notes)
            .build()?)
    }

    fn penalty(&self, f: &Facts, as_of: Date) -> Result<working::Result<Penalty>, Error> {
        if as_of.is_zero() {
            return Err(Error::Invalid(
                "washingtontaxcalc: a penalty needs the date to figure it to".into(),
            ));
        }
        let a = self.assess(f)?;
        if !a.requirement.target_known() {
            return Err(Error::with_detail(
                Error::NeedsThisYearTax,
                "Washington has no other figure to measure a late payment against",
            ));
        }
        let y = &self.y;
        let mut stop = as_of;
        let filed = f.return_filed();
        if !filed.is_zero() && filed.before(stop) {
            stop = filed;
        }
        let waiver_opt = f.positions().waiver();
        let waiver = waiver_opt.unwrap_or_default();
        let inst = a.installment()?;
        let mut pb = estimated::new_penalty(stop);
        pb.waiver(waiver)
            .because(a.requirement.clone(), std::slice::from_ref(&inst));
        let mut notes = Notes::default();

        let ledger = estimated::apply(&[a.due], &[a.tax], &f.payments())?;
        let Some(l) = ledger.first() else {
            return Err(Error::Invalid(
                "washingtontaxcalc: the ledger has no installment".into(),
            ));
        };
        if l.shortfall().0 == 0 || !stop.after(a.due) {
            let p = pb.build()?;
            if l.shortfall().0 == 0 {
                notes.rule(
                    "RCW 82.87.110(3)",
                    format!(
                        "the whole {} was paid by {}, so no penalty or interest arises",
                        a.tax,
                        a.due.long()
                    ),
                );
            }
            return Ok(working::new_result(p).with(&a.notes).with(&notes).build()?);
        }

        let mut pieces = Vec::new();
        let mut last_day = a.due;
        for pc in l.pieces() {
            let mut until = stop;
            if let Some(on) = pc.paid_on()
                && on.before(stop)
            {
                until = on;
            }
            pieces.push(Piece {
                amount: pc.amount(),
                until,
            });
            if until.after(last_day) {
                last_day = until;
            }
        }
        let mut lb = estimated::new_penalty_line(1, a.due, l.shortfall());
        lb.days(last_day.days_since(a.due));

        let estimate = l.credited_by();
        if a.extension && estimate.0 > 0 {
            // The department's reading: no late payment penalty, only the 5%
            // penalty on a low estimate. See the first divergence.
            notes.rule(
                "Department of Revenue, Capital gains tax page, Payments; RCW 82.32.090(12)",
                format!(
                    "with an extension, the {} paid by {} is an estimated payment, so the {} paid later owes no late payment penalty",
                    estimate,
                    a.due.long(),
                    l.shortfall()
                ),
            );
            let low = estimate.0 * 10_000 < i64::from(y.underpaid_share.0) * a.tax.0;
            if low && (l.shortfall() >= y.underpaid_floor) != (estimate >= y.underpaid_floor) {
                let outcome = if l.shortfall() >= y.underpaid_floor {
                    "draws the penalty"
                } else {
                    "draws no penalty"
                };
                notes.ask(
                    PositionKind::UnsettledRule,
                    "",
                    format!(
                        "The 5% test's \"at least {}\" could mean the shortfall or the payment. The figures read it as the shortfall, as the department does.",
                        y.underpaid_floor.whole()
                    ),
                );
                notes.rule(
                    "RCW 82.32.090(10)",
                    format!(
                        "the 5% penalty applies to a payment under {} of the tax \"and at least {}\"; the figures read that as the shortfall, as the department does, and being {} short of {} {}",
                        y.underpaid_share,
                        y.underpaid_floor.whole(),
                        l.shortfall(),
                        a.tax,
                        outcome
                    ),
                );
            }
            if low && l.shortfall() >= y.underpaid_floor {
                let charge = y.underpaid_rate.of(a.tax);
                // A penalty line refuses a part whose principal is more than
                // the shortfall, so the part carries the shortfall although
                // the charge is on the whole tax.
                let part =
                    estimated::new_penalty_part(PartKind::Flat, l.shortfall(), y.underpaid_rate)
                        .charge(charge)
                        .source(format!(
                            "RCW 82.32.090(10): {} of the {} actual tax due, not of the shortfall",
                            y.underpaid_rate, a.tax
                        ))
                        .build()?;
                lb.part(part);
                notes.rule(
                    "RCW 82.32.090(10)",
                    format!(
                        "the estimated payment of {} is under {} of the {} tax and {} short, so the penalty is {} of the whole tax, {}",
                        estimate,
                        y.underpaid_share,
                        a.tax,
                        l.shortfall(),
                        y.underpaid_rate,
                        charge
                    ),
                );
            } else {
                notes.rule(
                    "RCW 82.32.090(10)",
                    format!(
                        "the estimated payment of {} is at least {} of the {} tax, or short by under {}, so there is no penalty on it",
                        estimate, y.underpaid_share, a.tax, y.underpaid_floor
                    ),
                );
            }
        } else {
            let mut parts = Vec::new();
            let mut total = USCents(0);
            let mut prior = Rate(0);
            let mut partial = false;
            for (i, &total_rate) in y.late_totals.iter().enumerate() {
                let mut step = a.due;
                if i > 0 {
                    let raw = last_day_of_month_after(a.due, i as i64);
                    step = y.calendar.next_business_day(raw);
                    if pieces
                        .iter()
                        .any(|p| p.until.after(raw) && !p.until.after(step))
                    {
                        notes.ask(
                            PositionKind::UnsettledRule,
                            "",
                            format!(
                                "The law doesn't say if the {} step moves when {} is a legal holiday. The figures move it to {}, as Washington's day-count rule does.",
                                total_rate,
                                self.short(raw),
                                self.short(step)
                            ),
                        );
                        notes.rule(
                            "RCW 1.12.070(3); WAC 458-20-228(4)(a)",
                            format!(
                                "the step to {} is moved from {} to {}, as Washington's rule for counting days moves a deadline that falls on a legal holiday",
                                total_rate,
                                raw.long(),
                                step.long()
                            ),
                        );
                    }
                }
                let unpaid = unpaid_after(&pieces, step);
                let inc = Rate(total_rate.0 - prior.0);
                prior = total_rate;
                if unpaid.0 == 0 {
                    continue;
                }
                if i > 0 && unpaid < l.shortfall() {
                    partial = true;
                }
                let part = estimated::new_penalty_part(PartKind::Flat, unpaid, inc)
                    .span(a.due, step)
                    .charge(inc.of(unpaid))
                    .source(format!(
                        "RCW 82.32.090(1): a total of {} of tax not received by {}",
                        total_rate,
                        step.long()
                    ))
                    .build()?;
                total += part.charge();
                notes.rule(
                    "RCW 82.32.090(1)",
                    format!(
                        "{} was still unpaid after {}, so the late payment penalty reaches a total of {}; this step adds {} of it, {}",
                        unpaid,
                        step.long(),
                        total_rate,
                        inc,
                        part.charge()
                    ),
                );
                parts.push(part);
            }
            if partial {
                notes.ask(
                    PositionKind::UnsettledRule,
                    "",
                    format!(
                        "The law doesn't say how a part payment counts for steps {} and {}. Later steps are charged only on what was unpaid, not on the whole {}.",
                        y.late_totals[1],
                        y.late_totals[2],
                        l.shortfall().whole()
                    ),
                );
                notes.rule(
                    "RCW 82.32.090(1)",
                    format!(
                        "each later step is charged only on what was still unpaid, not on the whole {}",
                        l.shortfall().whole()
                    ),
                );
            }
            if total.0 > 0
                && total < y.late_minimum
                && let Some(first) = parts.first()
            {
                let raised =
                    estimated::new_penalty_part(PartKind::Flat, first.principal(), first.rate())
                        .span(first.from(), first.to())
                        .charge(first.charge() + y.late_minimum - total)
                        .source(format!(
                            "{}; raised so the penalty is not less than {}",
                            first.source(),
                            y.late_minimum
                        ))
                        .build()?;
                parts[0] = raised;
                notes.rule(
                    "RCW 82.32.090(1)",
                    format!(
                        "the late payment penalty of {} is raised to the {} minimum",
                        total, y.late_minimum
                    ),
                );
            }
            for p in parts {
                lb.part(p);
            }
        }

        // Interest, on the tax only, for the days after the end of April.
        let mut unrated = Date::default();
        let mut interest_charged = false;
        for p in &pieces {
            if !p.until.after(y.interest_from) {
                continue;
            }
            let (parts, gap) =
                estimated::simple_daily(p.amount, y.interest_from, p.until, &y.interest)?;
            if !gap.is_zero() && (unrated.is_zero() || gap.before(unrated)) {
                unrated = gap;
            }
            for part in parts {
                lb.part(part);
                interest_charged = true;
            }
        }
        if interest_charged {
            notes.rule(
                "RCW 82.32.050(1)(c)(ii), (2)",
                format!(
                    "interest runs on the unpaid tax from {} until it is paid or {}, at the department's yearly rate for each calendar year, charged by the day at the yearly rate divided by the days in the year",
                    y.interest_from.long(),
                    stop.long()
                ),
            );
            notes.ask(
                PositionKind::UnsettledRule,
                "",
                "Washington doesn't say how days are counted for its yearly interest rate. The figures charge it daily, at the yearly rate divided by the days in the year.",
            );
        } else {
            notes.rule(
                "RCW 82.32.050(1)(c)(ii)",
                format!(
                    "interest runs only after {}, so none has accrued",
                    y.interest_from.long()
                ),
            );
        }
        if !unrated.is_zero() {
            pb.rated_through(unrated.add_days(-1));
            notes.ask(
                PositionKind::UnpublishedRate,
                "",
                format!(
                    "The rate for the days after {} isn't published yet.",
                    self.short(unrated.add_days(-1))
                ),
            );
        }

        let line = lb.build()?;
        let mut p = pb.line(line).build()?;
        if p.total().0 > 0 {
            match waiver_opt {
                None => {
                    notes.ask(
                        PositionKind::PenaltyWaiver,
                        "Will you ask the department to waive the penalty?",
                        "It must waive it after five on-time years with no contact about the year. It must also waive it if things beyond your control caused it.",
                    );
                }
                Some(WaiverKind::Casualty) => {
                    notes.rule(
                        "RCW 82.32.105(1)",
                        "a casualty or disaster is a circumstance beyond the filer's control, for which the department must waive the penalties; the penalty is shown before the waiver",
                    );
                }
                Some(WaiverKind::RetiredOrDisabled) => {
                    notes.rule(
                        "RCW 82.32.105; RCW 82.87.110(7)",
                        "Washington has no waiver for retirement or disability as such; the penalty is shown before any waiver",
                    );
                }
                Some(WaiverKind::None) => {}
            }
            if let Some(o) = waiver_outcome(f, &mut notes) {
                p = p.to_builder().waiver_outcome(o).build()?;
            }
        }
        if !filed.is_zero() && filed.after(a.due) && !a.extension {
            notes.rule(
                "RCW 82.87.110(6)",
                "a return filed late also owes 5% of the tax for each month or part of one, up to 25%; that penalty is on the return, not the payment, and is not figured here",
            );
        }
        Ok(working::new_result(p).with(&a.notes).with(&notes).build()?)
    }

    fn explanation(&self) -> Explanation {
        WASHINGTON_EXPLANATION.clone()
    }
}

/// Decides the waiver asked for under RCW 82.32.105(1) and WAC
/// 458-20-228(9)(a): the department must waive the penalties when
/// circumstances beyond the filer's control caused the late payment. A
/// casualty or a serious illness is such a circumstance; retirement is not.
/// It returns `None` when no waiver was asked for.
fn waiver_outcome(f: &Facts, notes: &mut Notes) -> Option<WaiverOutcome> {
    let p = f.positions();
    let kind = p.waiver()?;
    if kind == WaiverKind::None {
        return None;
    }
    if kind == WaiverKind::Casualty {
        return Some(estimated::waiver_eligible_except(
            "A casualty or disaster beyond your control waives the penalty, but not interest. The department must waive it if it finds one caused the late payment.",
            &[PartKind::Interest],
        ));
    }
    match p.waiver_cause() {
        WaiverCause::Retirement => Some(estimated::waiver_refused(
            "Washington has no waiver for retirement, only for causes beyond your control. The department decides if one, such as a serious illness, made you pay late.",
        )),
        WaiverCause::Disability => {
            notes.ask(
                PositionKind::UnsettledRule,
                "",
                "Washington's rules count a sudden serious illness as beyond your control. They don't say whether a disability does.",
            );
            Some(estimated::waiver_undecided(
                "There is no waiver for disability as such, only for causes beyond your control. The department decides if one, such as a serious illness, made you pay late.",
            ))
        }
        _ => {
            notes.ask(
                PositionKind::UnsettledRule,
                "",
                "Washington's rules count a sudden serious illness as beyond your control. They don't say whether a disability does.",
            );
            Some(estimated::waiver_undecided(
                "Only causes beyond your control qualify, not retirement or disability as such. The department decides if one, such as a serious illness, made you pay late.",
            ))
        }
    }
}

/// Written by hand and checked against research/estimated-payments/wa.md and
/// the documents in the Go package's testdata/.
static WASHINGTON_EXPLANATION: LazyLock<Explanation> = LazyLock::new(|| {
    estimated::new_explanation(
        "Washington has no income tax for 2026 and no quarterly estimated payments. Its capital gains tax is one payment, due April 15, 2027 for the 2026 tax year.",
    )
    .paragraph("The capital gains tax is due on the day your federal return is due. A filing extension, federal or Washington, does not move that date. From the 2026 tax year you can pay early, from October 15, 2026, through My DOR. You can't prepay through tax preparation software.")
    .paragraph("For a late payment, the department adds a penalty of 9% of the unpaid tax. It rises to 19% if still unpaid at the end of May, and 29% at the end of June. The penalty is at least $5. Separately, interest runs on unpaid tax from April 30, 2027. It runs at the department's yearly rate, which is 6% for 2027.")
    .paragraph("If you have an extension, pay what you expect to be due by April 15. The department then charges no late payment penalty on a balance you pay later. Interest still runs on that balance. The penalty is instead 5% of the whole tax if you paid under 80% of it. That applies only if you were also at least $1,000 short.")
    .paragraph("You can ask the department to waive the late payment penalty. It must do so if you paid this tax on time in each of the previous five years. The department also must not have contacted you about the year. It also waives penalties caused by circumstances beyond your control. A late return has its own penalty of 5% of the tax a month, up to 25%.")
    .versus_federal("Washington differs from federal in kind. It has no quarterly payments or annualised method. It has no withholding and no safe harbor from last year's tax. The penalty is a share of the late tax, rising with lateness. Federal instead charges interest on each quarter's shortfall. Interest is charged separately from the end of April. The 80% test applies only with an extension.")
    .source("RCW 82.87.110 and 82.87.115; RCW 82.32.050, 82.32.090 and 82.32.105")
    .source("SSB 5314, chapter 409, Laws of 2025")
    .source("WAC 458-20-300 and WAC 458-20-228")
    .source("Department of Revenue, special notice, Prepayment of capital gains tax (August 3, 2026)")
    .source("Department of Revenue, Capital gains tax page and Interest rate tables (retrieved 2026-10-20)")
    .build()
    .unwrap_or_default()
});
