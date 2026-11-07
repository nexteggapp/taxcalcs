//! Maine's estimated tax rules, 36 M.R.S. § 5228, as the shared
//! [`estimated::EstimatedTax`] trait. Ported from estimated.go.
//!
//! Maine differs from the federal rules in shape. No payments are needed if
//! either this year's tax after withholding or last year's liability is under
//! $1,000. There is no 110% tier. Instead, income not subject to Maine
//! withholding that is $500,000 or more above the same period of last year is
//! an "unusual event": 90% of the tax on it is due with that period's
//! installment, and last year's liability does not cover it. The penalty is
//! interest under § 186, charged by the month, a part of a month counting as
//! a whole one, and compounded monthly.
//!
//! The figure follows Form 2210ME (2025), the latest penalty form, and the
//! result's Divergences say where § 5228 reads otherwise. No form here is
//! followed against a later change in the law. P.L. 2025, c. 113 amended the
//! January 31 relief effective September 24, 2025, before the 2026 Form
//! 1040ES-ME was revised in July 2026, and the 2025 Form 2210ME carries the
//! relief. P.L. 2025, c. 650 took the pass-through entity tax out of estimated
//! tax; the caller's figure leaves it out, so no form's reading is used for
//! it.

use std::any::Any;
use std::sync::LazyLock;

use estimated::{
    Basis, Calendar, Date, Error, EstimatedTax, Explanation, Facts, Installment, Jurisdiction,
    LedgerEntry, PartKind, Penalty, PenaltyPart, Requirement, TargetOption, WaiverKind,
    WaiverOutcome,
};
use money::{Rate, USCents, percent};
use num_bigint::BigInt;
use working::{Divergence, Notes, PositionKind, Reading};

const JURISDICTION: Jurisdiction = estimated::MAINE;

/// The facts Maine's rules read that no other jurisdiction does. Carry them
/// in [`estimated::FactsBuilder::state`].
///
/// They answer whether the year had an unusual event under 36 M.R.S.
/// § 5228(1)(D): taxable income not subject to Maine withholding, received in
/// one installment's period, that exceeds what was received in the same
/// period of last year by at least $500,000. Either all four periods are
/// given or [`EstimatedFactsBuilder::no_unusual_event`] says there was none.
/// Left unanswered, the rules assume none and say so in Required.
///
/// The periods are taken to be those that § 5228(5)(A) and the Annualized
/// Income Installment Worksheet close each installment at: January 1 to March
/// 31 for the first, April 1 to May 31, June 1 to August 31, and September 1
/// to December 31. § 5228 speaks of "that portion of the tax year applicable
/// to the required installment" and does not date it.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct EstimatedFacts {
    income: [Option<(USCents, USCents)>; 4],
    event_tax: [Option<USCents>; 4],
    none: bool,
    pass_through: bool,
    pt_answered: bool,
}

impl EstimatedFacts {
    /// The Maine taxable income not subject to Maine withholding received in
    /// installment `n`'s period (1 to 4) this year and in the same period
    /// last year, as `(this_year, last_year)`, if it was given.
    pub fn income_not_withheld(&self, n: i64) -> Option<(USCents, USCents)> {
        if !(1..=4).contains(&n) {
            return None;
        }
        self.income[(n - 1) as usize]
    }

    /// The Maine tax on the income that makes installment `n`'s period an
    /// unusual event, the excess over last year's same period, if it was
    /// given. 90% of it is due with that installment.
    pub fn unusual_event_tax(&self, n: i64) -> Option<USCents> {
        if !(1..=4).contains(&n) {
            return None;
        }
        self.event_tax[(n - 1) as usize]
    }

    /// Whether the caller said no period had an unusual event.
    pub fn no_unusual_event(&self) -> bool {
        self.none
    }

    /// Whether all four conditions of § 5228(11) hold for the unusual event,
    /// and whether the caller said, as `(holds, answered)`: the filer is an
    /// owner in a pass-through entity, had no control over the distribution,
    /// did not actually or constructively receive it, and pays the tax on it
    /// by the next installment date after receiving it. The assessor must
    /// then waive the penalty on it or extend the time to pay.
    pub fn pass_through_waiver(&self) -> (bool, bool) {
        (self.pass_through, self.pt_answered)
    }

    fn answered(&self) -> bool {
        self.none || self.income[0].is_some()
    }
}

/// Builds [`EstimatedFacts`].
#[derive(Clone, Debug, Default)]
pub struct EstimatedFactsBuilder {
    e: EstimatedFacts,
    err: Option<Error>,
}

/// Starts [`EstimatedFacts`] with nothing said.
pub fn new_estimated_facts() -> EstimatedFactsBuilder {
    EstimatedFactsBuilder::default()
}

impl EstimatedFactsBuilder {
    /// Records installment `n`'s period (1 to 4): the income not subject to
    /// Maine withholding received in it this year and in the same period last
    /// year.
    pub fn income_not_withheld(
        &mut self,
        n: i64,
        this_year: USCents,
        last_year: USCents,
    ) -> &mut Self {
        if !(1..=4).contains(&n) {
            self.fail(format!("mainetaxcalc: there is no installment {n}"));
        } else if self.e.income[(n - 1) as usize].is_some() {
            self.fail(format!(
                "mainetaxcalc: installment {n}'s income is given twice"
            ));
        } else {
            self.e.income[(n - 1) as usize] = Some((this_year, last_year));
        }
        self
    }

    /// Records the Maine tax on the excess income in installment `n`'s
    /// period.
    pub fn unusual_event_tax(&mut self, n: i64, tax: USCents) -> &mut Self {
        if !(1..=4).contains(&n) {
            self.fail(format!("mainetaxcalc: there is no installment {n}"));
        } else if self.e.event_tax[(n - 1) as usize].is_some() {
            self.fail(format!(
                "mainetaxcalc: installment {n}'s unusual-event tax is given twice"
            ));
        } else {
            self.e.event_tax[(n - 1) as usize] = Some(tax);
        }
        self
    }

    /// Says no period had an unusual event.
    pub fn no_unusual_event(&mut self) -> &mut Self {
        self.e.none = true;
        self
    }

    /// Answers whether the four conditions of § 5228(11) hold.
    pub fn pass_through_waiver(&mut self, holds: bool) -> &mut Self {
        self.e.pass_through = holds;
        self.e.pt_answered = true;
        self
    }

    fn fail(&mut self, text: String) {
        if self.err.is_none() {
            self.err = Some(Error::Invalid(text));
        }
    }

    /// Requires all four periods or none, refuses periods together with
    /// `no_unusual_event`, a tax for a period whose income was not given, and
    /// a negative tax. Income can be negative, as a loss.
    pub fn build(&self) -> Result<EstimatedFacts, Error> {
        if let Some(err) = &self.err {
            return Err(err.clone());
        }
        let e = self.e;
        let given = e.income.iter().filter(|p| p.is_some()).count();
        if given != 0 && given != 4 {
            return Err(Error::Invalid(format!(
                "mainetaxcalc: income is given for {given} of the four periods; give all four or none"
            )));
        }
        if given == 4 && e.none {
            return Err(Error::Invalid(
                "mainetaxcalc: periods' income and NoUnusualEvent together".to_string(),
            ));
        }
        for (i, t) in e.event_tax.iter().enumerate() {
            if t.is_some() && e.income[i].is_none() {
                return Err(Error::Invalid(format!(
                    "mainetaxcalc: installment {} has an unusual-event tax and no income",
                    i + 1
                )));
            }
            let v = t.unwrap_or_default();
            if v.0 < 0 {
                return Err(Error::Invalid(format!(
                    "mainetaxcalc: an unusual-event tax of {v} is negative"
                )));
            }
        }
        Ok(e)
    }
}

/// The § 186 rate for the days `from` through `to`, both included, as a rate
/// per month.
#[derive(Clone, Debug)]
pub(crate) struct MonthlyRate {
    pub(crate) from: Date,
    pub(crate) to: Date,
    pub(crate) monthly: Rate,
    pub(crate) source: String,
}

/// One tax year's published figures.
#[derive(Clone, Debug)]
pub(crate) struct EstimatedYear {
    pub(crate) year: i64,

    /// § 5228(2)(C) and (D): no estimates when this year's tax after
    /// withholding, or last year's liability, is under it.
    pub(crate) threshold: USCents,

    /// § 5228(3)(B) and (A).
    pub(crate) this_year_share: Rate,
    pub(crate) prior_year_share: Rate,

    /// § 5228(1)(D): the increase over last year's same period at which
    /// non-withheld income is an unusual event.
    pub(crate) unusual_event: USCents,

    /// As printed or as the statute counts them, before § 153(2) moves them.
    pub(crate) dues: Vec<Date>,
    pub(crate) file_and_pay_by: Date,
    pub(crate) stop: Date,
    pub(crate) calendar: Calendar,

    pub(crate) period_ends: Vec<Date>,
    pub(crate) annualised: Vec<Rate>,

    pub(crate) rates: Vec<MonthlyRate>,
}

/// Checks the year's rates as Go's `registerEstimated` does, which panics
/// where this returns the error.
pub(crate) fn validate(y: EstimatedYear) -> Result<EstimatedYear, Error> {
    for (i, r) in y.rates.iter().enumerate() {
        if r.from.is_zero() || r.to.before(r.from) || r.monthly.0 <= 0 || r.source.is_empty() {
            return Err(Error::Invalid(format!(
                "mainetaxcalc: rate {i} for {} is malformed",
                y.year
            )));
        }
        if i > 0 && r.from != y.rates[i - 1].to.add_days(1) {
            return Err(Error::Invalid(format!(
                "mainetaxcalc: rate {i} for {} does not follow the one before",
                y.year
            )));
        }
    }
    Ok(y)
}

/// § 5228(5): four equal installments. Form 2210ME line 10a spreads
/// withholding the same way.
fn quarters() -> [Rate; 4] {
    [percent(25), percent(25), percent(25), percent(25)]
}

/// Returns Maine's estimated tax rules for a tax year, or
/// [`estimated::Error::NoRules`] for a year this build does not carry.
pub fn estimated_tax(year: i64) -> Result<Box<dyn EstimatedTax>, Error> {
    let y = match year {
        2026 => crate::estimated_y2026::year_2026()?,
        _ => return Err(estimated::no_rules(&JURISDICTION, year)),
    };
    Ok(Box::new(MaineEstimated { y }))
}

struct MaineEstimated {
    y: EstimatedYear,
}

#[derive(Clone, Default)]
struct Assessment {
    requirement: Requirement,
    exempt: bool,
    tax_known: bool,
    dues: Vec<Date>,
    installments: Vec<USCents>,
    excused: bool,
    notes: Notes,
}

/// Which divergences change anything for one household's facts.
struct Affected {
    liability: bool,
    this_year_test: bool,
    withholding: bool,
    january: bool,
}

/// Go's `%T` for the extras a caller is most likely to pass by mistake.
fn type_name(s: &(dyn Any + Send + Sync)) -> &'static str {
    if s.is::<&str>() || s.is::<String>() {
        "string"
    } else {
        "another jurisdiction's extras"
    }
}

/// An option's label and amount, for the middle of a sentence.
fn option_label(o: &TargetOption) -> String {
    let mut l = o.label();
    if !l.is_empty() && !l.starts_with("Maine") {
        let mut chars = l.chars();
        if let Some(first) = chars.next() {
            l = first.to_lowercase().chain(chars).collect();
        }
    }
    format!("{l}, {}", o.amount().whole())
}

/// The unusual events in the facts, after the pass-through waiver.
#[derive(Clone, Copy, Default)]
struct Events {
    /// 90% of each installment's unusual-event tax.
    part: [USCents; 4],
    /// Their sum.
    total: USCents,
    found: bool,
}

fn rate_text(rates: &[MonthlyRate]) -> String {
    if rates.len() == 1 {
        return format!(
            "{} a month, the rate for {},",
            rates[0].monthly,
            rates[0].from.year()
        );
    }
    "the § 186 monthly rate in force".to_string()
}

/// Charges `principal` for each month or part of a month after `from` up to
/// and including `to`, compounded monthly, as § 186 and Form 2210ME Part II
/// do. Month k runs from `from.add_months(k-1)` to `from.add_months(k)` and
/// is charged at the rate in force on the day it begins. Months under one
/// rate are one part.
///
/// The returned date, `unrated`, is the first day of the first month that
/// begins on a day no rate covers, or the zero Date. That month and those
/// after it are not charged, and the caller must report them.
///
/// This belongs in estimated beside [`estimated::monthly`], which charges a
/// simple rate by the month.
fn compound_monthly(
    principal: USCents,
    from: Date,
    to: Date,
    rates: &[MonthlyRate],
) -> Result<(Vec<PenaltyPart>, Date), Error> {
    let months = estimated::months_or_part(from, to);
    let rate_on = |d: Date| {
        rates
            .iter()
            .find(|r| !d.before(r.from) && !d.after(r.to))
            .cloned()
    };
    let mut parts = Vec::new();
    let mut balance = principal.0;
    let mut k = 1;
    while k <= months {
        let begins = from.add_months(k - 1).add_days(1);
        let Some(rate) = rate_on(begins) else {
            return Ok((parts, begins));
        };
        let start = from.add_months(k - 1);
        let mut n: u32 = 0;
        while k <= months {
            let b = from.add_months(k - 1).add_days(1);
            if b.before(rate.from) || b.after(rate.to) {
                break;
            }
            n += 1;
            k += 1;
        }
        let mut end = from.add_months(k - 1);
        if end.after(to) {
            end = to;
        }
        // balance × ((1 + m/10000)^n − 1), as balance × (s^n − d^n) ÷ d^n.
        let s = BigInt::from(10_000 + i64::from(rate.monthly.0)).pow(n);
        let d = BigInt::from(10_000).pow(n);
        let num = BigInt::from(balance) * (s - &d);
        let charge = round_cents(&num, &d);
        let part =
            estimated::new_penalty_part(PartKind::Interest, principal, Rate(rate.monthly.0 * 12))
                .span(start, end)
                .days(end.days_since(start))
                .months(i64::from(n))
                .charge(charge)
                .source(rate.source.clone())
                .build()?;
        parts.push(part);
        balance += charge.0;
    }
    Ok((parts, Date::default()))
}

/// `num ÷ den`, a non-negative amount of cents, rounded half up.
fn round_cents(num: &BigInt, den: &BigInt) -> USCents {
    // The floor of twice the amount, for an amount not below zero.
    let q: BigInt = (num * 2) / den;
    let q = (q + 1) / 2;
    USCents(i64::try_from(q).unwrap_or(i64::MAX))
}

/// Decides a waiver asked for. 36 M.R.S. § 5228(5) lets the State Tax
/// Assessor waive the penalty "for cause", without listing causes.
/// § 187-B(7) counts a natural disaster as reasonable cause for the penalties
/// it governs, and doesn't say it governs this one. Neither lists retirement
/// or disability.
fn maine_waiver(f: &Facts) -> Option<WaiverOutcome> {
    match f.positions().waiver()? {
        WaiverKind::None => None,
        WaiverKind::Casualty => Some(estimated::waiver_undecided(
            "Maine's law doesn't say if a casualty or disaster is cause for this penalty. Maine's State Tax Assessor can waive the penalty for cause.",
        )),
        WaiverKind::RetiredOrDisabled => Some(estimated::waiver_refused(
            "Maine has no waiver for retirement or disability as such, only one for cause. The State Tax Assessor decides it.",
        )),
    }
}

impl MaineEstimated {
    fn extras(&self, f: &Facts) -> Result<EstimatedFacts, Error> {
        match f.state() {
            None => Ok(EstimatedFacts::default()),
            Some(s) => match s.downcast_ref::<EstimatedFacts>() {
                Some(e) => Ok(*e),
                None => Err(Error::Invalid(format!(
                    "mainetaxcalc: the facts carry {}, not mainetaxcalc.EstimatedFacts",
                    type_name(s)
                ))),
            },
        }
    }

    fn check(&self, f: &Facts) -> Result<(), Error> {
        if f.jurisdiction() != JURISDICTION {
            return Err(Error::Invalid(format!(
                "mainetaxcalc: these are Maine's rules, and the facts are for {}",
                f.jurisdiction()
            )));
        }
        if f.tax_year() != self.y.year {
            return Err(Error::Invalid(format!(
                "mainetaxcalc: these are the {} rules, and the facts are for {}",
                self.y.year,
                f.tax_year()
            )));
        }
        if f.farmer_or_fisher() {
            return Err(Error::with_detail(
                Error::Unsupported,
                "§ 5228(3)(B), (4) and (10) give farmers and persons who fish commercially one installment at 66 2/3%, which is not built",
            ));
        }
        if f.elections().annualised() {
            for (i, p) in f.periods().iter().enumerate() {
                let Some(&end) = self.y.period_ends.get(i) else {
                    return Err(Error::Invalid(format!(
                        "mainetaxcalc: the Annualized Income Installment Worksheet has no period {}",
                        i + 1
                    )));
                };
                if p.end() != end {
                    return Err(Error::Invalid(format!(
                        "mainetaxcalc: Annualized Income Installment Worksheet period {} ends on {}, not {}",
                        i + 1,
                        end,
                        p.end()
                    )));
                }
            }
        }
        Ok(())
    }

    /// Where Maine Revenue Services' documents and § 5228 part ways on a rule
    /// a Maine computation uses. They are found in
    /// research/estimated-payments/me.md, item 10 and its verification.
    fn divergences(&self, a: &Affected) -> Result<Vec<Divergence>, Error> {
        let mut out = Vec::new();
        for b in [
            working::new_divergence("Last year's liability")
                .form("Last year's liability is the tax after refundable credits.",
                    "Form 2210ME (2025), line 5")
                .statute("The law reads last year's \"tax liability\". It doesn't say whether refundable credits reduce it.",
                    "36 M.R.S. § 5228(2)(D), (3)(A)")
                .uses(Reading::Form).affects(a.liability).clone(),
            working::new_divergence("The $1,000 test on this year's tax")
                .form("This year's tax less withholding is tested against $1,000.",
                    "Form 2210ME (2025), lines 1 to 4")
                .statute("This year's tax is reduced by allowable credits. No payments are needed when the result is under $1,000. Those credits include estimated payments as well as withholding.",
                    "36 M.R.S. § 5228(1)(A), (2)(C)")
                .uses(Reading::Form).affects(a.this_year_test).clone(),
            working::new_divergence("When withholding counts as paid")
                .form("Withholding is entered as 25% on each due date, whenever it was withheld. There's no line for the actual dates.",
                    "Form 2210ME (2025), line 10a")
                .statute("The installments can follow when the tax and the credits actually arose. The credits include withholding. You must show it with adequate records.",
                    "36 M.R.S. § 5228(1)(A), (5)(A)")
                .uses(Reading::Form).affects(a.withholding).clone(),
            working::new_divergence("A return filed and paid by January 31")
                .form("The estimated tax voucher doesn't mention the relief. The return instructions mention only paying in full. Form 2210ME skips the January penalty when the tax is paid by January 31.",
                    "Form 1040ES-ME (revised July 2026); Form 1040ME General Instructions (2025); Form 2210ME (2025)")
                .statute("You can file your return and pay in full by the last day of January. Then there's no penalty on the January payment.",
                    "36 M.R.S. § 5228(9), as amended by P.L. 2025, c. 113, Pt. C, § 12, effective September 24, 2025")
                .uses(Reading::Statute).affects(a.january).clone(),
        ] {
            out.push(b.build()?);
        }
        Ok(out)
    }

    fn unusual_events(
        &self,
        f: &Facts,
        extras: &EstimatedFacts,
        a: &mut Assessment,
    ) -> Result<Events, Error> {
        let y = &self.y;
        let mut ev = Events::default();
        if !extras.answered() {
            return Ok(ev);
        }
        let mut taxes = USCents(0);
        for i in 0..ev.part.len() {
            let n = i as i64 + 1;
            let (this, last) = extras.income_not_withheld(n).unwrap_or_default();
            if this - last < y.unusual_event {
                continue;
            }
            ev.found = true;
            let tax_opt = extras.unusual_event_tax(n);
            if tax_opt.is_none() {
                a.notes.ask(
                    PositionKind::JurisdictionFact,
                    format!(
                        "What is the Maine tax on the extra {} of income without Maine withholding?",
                        (this - last).whole()
                    ),
                    format!(
                        "90% of its tax is due with installment {n}, for the period it came in. For now, none of it is counted."
                    ),
                );
            }
            let tax = tax_opt.unwrap_or_default();
            taxes += tax;
            ev.part[i] = y.this_year_share.of(tax);
            ev.total += ev.part[i];
            a.notes.rule(
                "36 M.R.S. § 5228(1)(D), (3), (5)(D); Form 1040ES-ME (2026)",
                format!(
                    "installment {n}'s period had {this} of income not subject to Maine withholding against {last} in the same period of {}, {} more, at least {}, so it is an unusual event: \
                     90% of the {tax} tax on it, {}, is due with installment {n}, and last year's liability does not cover it",
                    y.year - 1,
                    this - last,
                    y.unusual_event,
                    ev.part[i]
                ),
            );
        }
        if !ev.found {
            return Ok(ev);
        }
        if let Some(tax) = f.this_year().tax()
            && taxes > tax
        {
            return Err(Error::Invalid(format!(
                "mainetaxcalc: the unusual-event tax of {taxes} is more than this year's tax of {tax}"
            )));
        }
        a.notes.rule(
            "36 M.R.S. § 5228(1)(D), (5)(A)",
            "an installment's period is taken as the months since the last one closed: January to March, April and May, June to August, and September to December; § 5228 does not date it",
        );
        let (holds, answered) = extras.pass_through_waiver();
        if holds {
            a.notes.rule(
                "36 M.R.S. § 5228(11)",
                "the four pass-through conditions hold, so the assessor must waive the penalty on the unusual event or extend the time to pay it; the installments are figured without it",
            );
            return Ok(Events::default());
        }
        if !answered {
            a.notes.ask(
                PositionKind::PenaltyWaiver,
                "Did the extra income come from a pass-through business you own?",
                "Its penalty must be waived if you didn't control or receive the distribution. Its tax must also be paid by the next due date; the figures charge it for now.",
            );
        }
        Ok(ev)
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
        let paid: USCents = f.payments().iter().map(|p| p.amount()).sum();
        let by = y.calendar.next_business_day(y.file_and_pay_by);
        let filed_by = !f.return_filed().is_zero() && !f.return_filed().after(by);
        let credited_by: USCents = f
            .payments()
            .iter()
            .filter(|p| !p.on().after(by))
            .map(|p| p.amount())
            .sum();
        let diverge = |a: &mut Assessment, liability: bool| -> Result<(), Error> {
            let ds = self.divergences(&Affected {
                liability,
                this_year_test: tax_known
                    && tax - withheld >= y.threshold
                    && tax - withheld - paid < y.threshold,
                withholding: !a.exempt && withheld.0 > 0,
                january: !a.exempt && (filed_by || (tax_known && withheld + credited_by >= tax)),
            })?;
            a.notes.diverges(&ds);
            Ok(())
        };

        // The $1,000 test on this year, Form 2210ME lines 1 to 4. It waives
        // the requirement whatever else is true, unusual events included.
        if tax_known && tax - withheld < y.threshold {
            a.exempt = true;
            let this_year = estimated::new_target_option(
                Basis::ThisYearTax,
                y.this_year_share,
                y.this_year_share.of(tax),
            )
            .build()?;
            let ex =
                estimated::exception_below("Tax less withholding", tax - withheld, y.threshold)?;
            let none = format!(
                " So no estimated payments are needed for {}, and no penalty applies.",
                y.year
            );
            let why = if tax < withheld {
                format!(
                    "Your Maine tax of {} less {} withheld {}.{none}",
                    tax.whole(),
                    withheld.whole(),
                    estimated::leaves(tax - withheld)
                )
            } else if tax == withheld {
                format!(
                    "Your withholding of {} covers your Maine tax of {}.{none}",
                    withheld.whole(),
                    tax.whole()
                )
            } else if (tax - withheld).whole() == y.threshold.whole() {
                format!(
                    "Your Maine tax less withholding is under {}.{none}",
                    y.threshold.whole()
                )
            } else {
                format!(
                    "Your Maine tax of {} less {} withheld leaves {}, under {}.{none}",
                    tax.whole(),
                    withheld.whole(),
                    (tax - withheld).whole(),
                    y.threshold.whole()
                )
            };
            a.requirement = estimated::new_requirement()
                .option(this_year)
                .required(false, why)
                .exempt(ex)
                .build()?;
            diverge(&mut a, false)?;
            a.notes.rule(
                "36 M.R.S. § 5228(2)(C); Form 2210ME (2025), lines 1 to 4",
                format!(
                    "{tax} of tax less {withheld} withheld is {}, under {}, so there is no requirement and no penalty",
                    tax - withheld,
                    y.threshold
                ),
            );
            a.installments = vec![USCents(0); a.dues.len()];
            return Ok(a);
        }

        let ev = self.unusual_events(f, &extras, &mut a)?;

        // 90% of this year's tax, § 5228(3)(B).
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
                "36 M.R.S. § 5228(3)(B); Form 1040ES-ME (2026)",
                format!(
                    "{} of this year's Maine tax of {tax} is {}",
                    y.this_year_share,
                    this_year.amount()
                ),
            );
        }

        // Last year's liability, § 5228(3)(A), and the $1,000 test on it,
        // § 5228(2)(D). Maine does not require last year's return to have
        // been filed; the test reads its liability.
        let has_prior = f.prior_year().is_some();
        let prior = f.prior_year().unwrap_or_default();
        let pf_opt = prior.figures();
        let has_figures = pf_opt.is_some();
        let prior_tax = pf_opt.and_then(|pf| pf.tax()).unwrap_or_default();
        let mut prior_small = false;
        let unavailable = |why: String| {
            estimated::new_target_option(Basis::PriorYearTax, y.prior_year_share, USCents(0))
                .unavailable(why)
                .build()
        };
        let liability_question = format!("What was your {last} Maine tax liability?");
        let liability_effect = format!(
            "Under {}, no estimated payments are needed, or else it can set your target.",
            y.threshold.whole()
        );
        let prior_opt = if !has_prior {
            a.notes.ask(
                PositionKind::PriorYearReturn,
                liability_question,
                liability_effect,
            );
            unavailable(format!("Needs your {last} Maine return"))?
        } else if !has_figures {
            a.notes.ask(
                PositionKind::PriorYearReturn,
                liability_question,
                liability_effect,
            );
            unavailable(format!("Needs your {last} Maine tax liability"))?
        } else if prior.months() != 12 {
            a.notes.ask(
                PositionKind::PriorYearReturn,
                format!(
                    "What liability does Form 2210ME give for your {}-month {last} return?",
                    prior.months()
                ),
                format!(
                    "For a short year, Form 2210ME uses another figure for the {} test. Without it, last year can't set your target.",
                    y.threshold.whole()
                ),
            );
            unavailable(format!(
                "Your {last} Maine return covered {} months, and this option needs a 12-month year",
                prior.months()
            ))?
        } else if prior_tax < y.threshold {
            prior_small = true;
            a.notes.rule(
                "36 M.R.S. § 5228(2)(D); Form 2210ME (2025), line 5",
                format!(
                    "the {last} Maine liability of {prior_tax} is under {}, which waives the requirement except for an unusual event, whose share is {}",
                    y.threshold, ev.total
                ),
            );
            estimated::new_target_option(Basis::PriorYearTax, y.prior_year_share, ev.total)
                .label(format!(
                    "Unusual events only; last year's tax was under {}",
                    y.threshold.whole()
                ))
                .build()?
        } else {
            let amount = y.prior_year_share.of(prior_tax) + ev.total;
            let mut b =
                estimated::new_target_option(Basis::PriorYearTax, y.prior_year_share, amount);
            if ev.found {
                b.label(format!(
                    "{} of last year's tax plus the unusual event's share",
                    y.prior_year_share
                ));
            }
            let mut applied = format!(
                "{} of the {prior_tax} liability on the {last} return is {}",
                y.prior_year_share,
                y.prior_year_share.of(prior_tax)
            );
            if ev.found {
                applied += &format!(
                    "; with {} for the unusual event, which it does not cover, the option is {amount}",
                    ev.total
                );
            }
            a.notes
                .rule("36 M.R.S. § 5228(3)(A); Form 1040ES-ME (2026)", applied);
            b.build()?
        };

        let mut rb = estimated::new_requirement();
        rb.option(this_year.clone()).option(prior_opt.clone());
        let (mut chosen, mut target) = (Basis::ThisYearTax, this_year.amount());
        if prior_opt.available() && (!tax_known || prior_opt.amount() < target) {
            (chosen, target) = (Basis::PriorYearTax, prior_opt.amount());
        }
        if !extras.answered() {
            a.notes.ask(
                PositionKind::JurisdictionFact,
                format!(
                    "Did any quarter's income without Maine withholding jump by {} or more?",
                    y.unusual_event.whole()
                ),
                format!(
                    "The jump is measured against the same part of {last}. If so, 90% of its tax is due that quarter; until then, the figures assume none."
                ),
            );
        }

        let chosen_opt = if chosen == Basis::PriorYearTax {
            &prior_opt
        } else {
            &this_year
        };
        if prior_small && ev.total.0 == 0 {
            a.exempt = true;
            let ex = estimated::exception_below("Last year's tax", prior_tax, y.threshold)?;
            let none = format!(
                " So no estimated payments are needed for {}, and no penalty applies.",
                y.year
            );
            let why = if prior_tax.whole() == y.threshold.whole() {
                format!(
                    "Your {last} Maine tax was under {}.{none}",
                    y.threshold.whole()
                )
            } else {
                format!(
                    "Your {last} Maine tax of {} was under {}.{none}",
                    prior_tax.whole(),
                    y.threshold.whole()
                )
            };
            rb.required(false, why).exempt(ex);
        } else if !tax_known && !prior_opt.available() {
            rb.required(
                false,
                format!(
                    "Your target comes from your {} Maine tax or your {last} liability. There's no target until you have one of them.",
                    y.year
                ),
            )
            .no_target();
        } else if !tax_known && withheld >= target {
            rb.required(
                false,
                format!(
                    "Your target is {}. Your withholding of {} already reaches it. So no estimated payments are needed, whatever your {} tax turns out to be.",
                    option_label(&prior_opt),
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
                    option_label(&prior_opt),
                    y.year,
                    withheld.whole(),
                    (target - withheld).whole()
                ),
            )
            .choose(chosen);
        } else if withheld >= target {
            rb.required(
                false,
                format!(
                    "Your Maine withholding of {} already reaches your target of {}. So no estimated payments are needed.",
                    withheld.whole(),
                    target.whole()
                ),
            )
            .choose(chosen);
        } else {
            rb.required(
                true,
                format!(
                    "Your Maine tax less withholding leaves {}, which is {} or more. Your target is {}. To reach it, you need {} of estimated payments.",
                    (tax - withheld).whole(),
                    y.threshold.whole(),
                    option_label(chosen_opt),
                    (target - withheld).whole()
                ),
            )
            .choose(chosen);
        }
        // Under last year's exception this year's tax can't change the
        // target.
        if !tax_known && !a.exempt {
            a.notes.ask(
                PositionKind::ThisYearTax,
                format!("What do you expect your {} Maine tax to be?", y.year),
                format!(
                    "{} of it may lower your target; under {} after withholding, none are due.",
                    y.this_year_share,
                    y.threshold.whole()
                ),
            );
        }
        a.requirement = rb.build()?;
        let liability_read = has_prior && has_figures && prior.months() == 12;
        diverge(
            &mut a,
            liability_read && (prior_small || chosen == Basis::PriorYearTax),
        )?;
        if a.exempt {
            a.installments = vec![USCents(0); a.dues.len()];
            return Ok(a);
        }

        // The installments: a quarter of the target less the unusual events,
        // and each event's 90% with its own installment, § 5228(5) and
        // (5)(D).
        let base = (a.requirement.target() - ev.total).max(USCents(0));
        let mut cum = estimated::cumulative_shares(base, &quarters())?;
        let mut events = USCents(0);
        for (i, c) in cum.iter_mut().enumerate() {
            events += ev.part[i];
            *c += events;
        }
        a.installments = estimated::increments(&cum);
        let mut applied = format!(
            "the installments due {}, {}, {} and {} are {}, {}, {} and {}: a quarter of {base} each",
            a.dues[0].long(),
            a.dues[1].long(),
            a.dues[2].long(),
            a.dues[3].long(),
            a.installments[0],
            a.installments[1],
            a.installments[2],
            a.installments[3]
        );
        if ev.found {
            applied += ", with each unusual event's 90% share in the installment for its period";
        }
        a.notes.rule(
            "36 M.R.S. § 5228(4), (5); Form 1040ES-ME (2026); Form 2210ME (2025), line 7",
            applied,
        );

        if f.elections().annualised() {
            let taxes: Vec<USCents> = f.periods().iter().map(|p| p.annualised_tax()).collect();
            let ann = estimated::annualised(&taxes, &y.annualised, &a.installments)?;
            a.notes.rule(
                "36 M.R.S. § 5228(5)(A); Annualized Income Installment Worksheet (2025); Form 2210ME (2025), line 17",
                format!(
                    "under the annualized method the installments are {}, {}, {} and {}: 22.5%, 45%, 67.5% and 90% of the annualized tax, \
                     less earlier installments, and never more than the regular installment plus any shortfall carried forward; the worksheet must be filed with Form 2210ME, line 17 checked, even when no penalty is due",
                    ann[0], ann[1], ann[2], ann[3]
                ),
            );
            a.installments = ann;
        }

        // § 5228(9): a return filed with the tax paid in full by the last day
        // of January. January 31, 2027 is a Sunday, and § 153(2) moves it.
        if filed_by {
            a.excused = true;
            a.notes.rule(
                "36 M.R.S. §§ 5228(9), 153(2); Form 2210ME (2025)",
                format!(
                    "the return was filed with the tax paid in full on {}, by {} ({} moved past the weekend), so the fourth installment bears no penalty",
                    f.return_filed().long(),
                    by.long(),
                    y.file_and_pay_by.long()
                ),
            );
        }
        Ok(a)
    }

    fn ledger(&self, f: &Facts, a: &Assessment) -> Result<(Vec<LedgerEntry>, Notes), Error> {
        let credits = estimated::credits(f, &a.dues, &quarters())?;
        let mut required = a.installments.clone();
        if a.excused {
            required[3] = USCents(0);
        }
        let ledger = estimated::apply(&a.dues, &required, &credits)?;
        let mut notes = Notes::default();
        notes.rule(
            "Form 2210ME (2025), line 10a",
            format!(
                "the {} withheld is entered as 25% on each due date, whenever it was actually withheld; \
                 a payment after a due date is applied to the oldest shortfall first, as Form 2210 does, since the research for this package does not reproduce Form 2210ME's own lines for it",
                f.withheld_total()
            ),
        );
        if f.elections().withholding_as_withheld() {
            notes.rule(
                "Form 2210ME (2025), line 10a; 36 M.R.S. § 5228(5)(A)",
                "Form 2210ME has no line for crediting withholding on the dates it was withheld, so it is spread evenly; see the divergence on when withholding counts as paid",
            );
        }
        Ok((ledger, notes))
    }
}

/// The due dates with what was needed and credited by each.
fn installments(a: &Assessment, ledger: &[LedgerEntry]) -> Result<Vec<Installment>, Error> {
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

impl EstimatedTax for MaineEstimated {
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
                    "with no {} Maine liability to measure against, there is no target to schedule",
                    self.y.year - 1
                ),
            ));
        }
        let (ledger, notes) = self.ledger(f, &a)?;
        let out = installments(&a, &ledger)?;
        Ok(working::new_result(out)
            .with(&a.notes)
            .with(&notes)
            .build()?)
    }

    fn penalty(&self, f: &Facts, as_of: Date) -> Result<working::Result<Penalty>, Error> {
        if as_of.is_zero() {
            return Err(Error::Invalid(
                "mainetaxcalc: a penalty needs the date to figure it to".to_string(),
            ));
        }
        let a = self.assess(f)?;
        if !a.requirement.target_known() {
            return Err(Error::with_detail(
                Error::NeedsThisYearTax,
                format!(
                    "with no {} Maine liability to measure against, there is no target to figure a penalty from",
                    self.y.year - 1
                ),
            ));
        }
        let y = &self.y;
        let mut stop = y.calendar.next_business_day(y.stop);
        if as_of.before(stop) {
            stop = as_of;
        }
        let mut paid_in_full = false;
        let filed = f.return_filed();
        if !filed.is_zero() && filed.before(stop) {
            (stop, paid_in_full) = (filed, true);
        }
        let waiver_answer = f.positions().waiver();
        let waiver = waiver_answer.unwrap_or_default();
        if a.exempt {
            let p = estimated::new_penalty(stop)
                .waiver(waiver)
                .because(a.requirement.clone(), &[])
                .build()?;
            return Ok(working::new_result(p).with(&a.notes).build()?);
        }

        let (ledger, mut notes) = self.ledger(f, &a)?;
        let schedule = installments(&a, &ledger)?;
        let mut pb = estimated::new_penalty(stop);
        pb.waiver(waiver).because(a.requirement.clone(), &schedule);
        let mut unrated = Date::default();
        for l in &ledger {
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
                let (parts, gap) = compound_monthly(piece.amount(), l.due(), to, &y.rates)?;
                if !gap.is_zero() && (unrated.is_zero() || gap.before(unrated)) {
                    unrated = gap;
                }
                for p in parts {
                    lb.part(p);
                }
            }
            let line = lb.days(last_day.days_since(l.due())).build()?;
            pb.line(line);
        }
        if !a.tax_known {
            pb.upper_bound();
        }
        let mut p = pb.build()?;
        if !a.tax_known {
            notes.rule(
                "36 M.R.S. § 5228(3)",
                format!(
                    "this year's Maine tax was not given, so the penalty is figured against the {} option alone; \
                     the required payment is the lesser of that and 90% of this year's tax, so {} is the most the penalty can be, and it is less if this year's tax turns out lower",
                    y.year - 1,
                    p.total()
                ),
            );
        }
        if paid_in_full {
            notes.rule(
                "36 M.R.S. § 5228(1)(C); Form 2210ME (2025), lines 12 and 13",
                format!(
                    "the return was filed with the whole balance paid on {}, so every shortfall still outstanding was paid that day",
                    stop.long()
                ),
            );
        }
        notes.rule(
            "36 M.R.S. §§ 5228(1)(C), (5), 186; Form 2210ME (2025), Part II",
            format!(
                "each shortfall is charged {} for each month or part of a month from its due date until paid or {}, compounded monthly, \
                 worked out exactly and rounded to the cent, since the research for this package does not reproduce the form's precomputed table; the penalty to {} is {}",
                rate_text(&y.rates),
                y.calendar.next_business_day(y.stop).long(),
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
                    "The Maine rate for the days after {} isn't published yet.",
                    unrated.add_days(-1).long()
                ),
            );
        }
        if let Some(k) = waiver_answer
            && k != WaiverKind::None
        {
            notes.rule(
                "36 M.R.S. § 5228(5)",
                format!(
                    "Maine has no {k} waiver of its own; the State Tax Assessor may waive or abate the penalty \"for cause\", and the penalty shown is before any waiver"
                ),
            );
        }
        if p.total().0 > 0 && waiver_answer.is_none() {
            notes.ask(
                PositionKind::PenaltyWaiver,
                "Are you asking Maine to waive your penalty for cause?",
                "Maine's assessor may waive the penalty for cause. Maine counts a natural disaster as reasonable cause for other penalties.",
            );
        }
        if let Some(o) = maine_waiver(f)
            && p.total().0 > 0
        {
            let undecided = o.undecided();
            p = p.to_builder().waiver_outcome(o).build()?;
            if undecided {
                notes.ask(
                    PositionKind::UnsettledRule,
                    "",
                    "Maine lists reasonable causes for other penalties, including a natural disaster. Its law doesn't say whether they count as cause to waive this one.",
                );
            }
        }
        Ok(working::new_result(p).with(&a.notes).with(&notes).build()?)
    }

    fn explanation(&self) -> Explanation {
        MAINE_EXPLANATION.clone()
    }
}

/// Written by hand and checked against research/estimated-payments/me.md.
static MAINE_EXPLANATION: LazyLock<Explanation> = LazyLock::new(|| {
    estimated::new_explanation(
        "Maine expects estimated payments when two figures are both $1,000 or more. They are your tax after withholding this year and last year's tax. The penalty is interest on each quarter's shortfall, counted in whole months.")
        .paragraph("You avoid the penalty if withholding and estimated payments reach a target. The target is the lesser of 90% of this year's Maine tax or 100% of last year's. Last year can set it only if it was a full 12-month year. There's no higher figure for high incomes. Maine asks for nothing if this year's tax after withholding is under $1,000. It also asks for nothing if last year's tax was under $1,000.")
        .paragraph("The exception is a sudden jump in income without Maine withholding. A rise of $500,000 or more on the same part of last year is an \"unusual event\". That quarter's payment must include 90% of the tax on the extra income. Last year's tax doesn't cover it, even when that tax was under $1,000.")
        .paragraph("A quarter is due April 15, June 15 and September 15, 2026, and January 15, 2027. A date on a weekend or Maine holiday is due the next business day. Withholding counts as paid evenly across the four dates. You can file and pay in full by the end of January. Then there's no penalty on the January payment. For 2026, the end of January is Monday, February 1, 2027.")
        .paragraph("The penalty is interest at 0.75% a month for 2026, compounded monthly. A part of a month counts as a whole one. A payment one day late costs a full month. It runs until the shortfall is paid or the return is due on April 15. The annualized worksheet can lower early payments for uneven income. The assessor may waive the penalty for cause.")
        .versus_federal("Federal needs no payments only if last year's tax was zero. Maine needs none if it was under $1,000. Maine has no 110% tier. A $500,000 income jump without withholding has 90% of its tax due that quarter. That holds whatever last year's tax was. The penalty is charged by the month, compounded monthly, at a fixed annual rate.")
        .source("36 M.R.S. §§ 153, 186, 5227, 5228")
        .source("Form 1040ES-ME, Estimated Tax for Individuals, 2026 (revised July 2026)")
        .source("Form 2210ME, Underpayment of Estimated Tax by Individuals, Estates, or Trusts, and its instructions, 2025")
        .source("Annualized Income Installment Worksheet for Underpayment of Estimated Tax, 2025")
        .source("4 M.R.S. § 1051, Legal holidays")
        .build()
        .unwrap_or_default()
});
