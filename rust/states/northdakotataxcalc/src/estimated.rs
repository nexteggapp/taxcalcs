//! North Dakota's estimated tax rules, N.D.C.C. §§ 57-38-62 and 57-38-63,
//! with the interest under § 57-38-45, as the shared [`EstimatedTax`]
//! interface. North Dakota calls the charge interest.
//!
//! Estimates are owed only by someone who must also pay federal estimated
//! tax, and only when last year's North Dakota net tax liability was $1,000
//! or more. The required payment is the lesser of 90% of this year's tax or
//! 100% of last year's, with no 110% tier and no twelve-month test, in four
//! 25% installments. The interest is a fixed 12% a year.
//!
//! The figure follows Schedule ND-1UT for 2025, the latest published, with
//! the 2026 dates, and Form ND-1ES for 2026. ND-1UT differs from the federal
//! form in two places that change the interest: a payment goes first to its
//! own quarter, and the day fraction is rounded to three places. The
//! result's Divergences say where the forms and the statute part ways.
//! § 57-38-62 and § 57-38-63 were not amended in 2025
//! (research/estimated-payments/nd.md), so the forms do not predate the law.

use std::any::Any;

use estimated::{
    Basis, Calendar, Date, Error, EstimatedTax, Explanation, Facts, Installment, Month, PartKind,
    Payment, Penalty, PenaltyPart, Requirement, ResidencePeriod, Rounding, TargetOption,
    TargetOptionBuilder, WaiverCause, WaiverKind, WaiverOutcome,
};
use money::{Rate, USCents, percent};
use working::{Divergence, Notes, PositionKind, Reading};

/// North Dakota's postal code.
const JURISDICTION: estimated::Jurisdiction = estimated::NORTH_DAKOTA;

/// The facts North Dakota's rules read that no other jurisdiction does.
/// Carry them in [`estimated::FactsBuilder::state`].
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct EstimatedFacts {
    federal: bool,
    federal_answered: bool,
    fourth: bool,
    fourth_answered: bool,
    annualised: bool,
    annualised_answered: bool,
    first_income: Date,
}

impl EstimatedFacts {
    /// Whether the filer must pay federal estimated tax for the year, under
    /// 26 U.S.C. § 6654, and whether the caller said: `(required,
    /// answered)`. North Dakota requires estimates of no one else.
    pub fn federal_estimates_required(&self) -> (bool, bool) {
        (self.federal, self.federal_answered)
    }

    /// Whether the federal January installment was not required because the
    /// federal return was filed and the federal tax paid by the federal date,
    /// and whether the caller said: `(excused, answered)`. This is Schedule
    /// ND-1UT Item C.
    pub fn federal_fourth_quarter_excused(&self) -> (bool, bool) {
        (self.fourth, self.fourth_answered)
    }

    /// Whether the annualized income installment method was used on the
    /// federal Form 2210, and whether the caller said: `(used, answered)`.
    /// North Dakota allows the method only then.
    pub fn annualised_federally(&self) -> (bool, bool) {
        (self.annualised, self.annualised_answered)
    }

    /// The day a nonresident, or a filer who moved into North Dakota this
    /// year, first received income North Dakota taxes, or `None` if the
    /// caller did not say. This is Schedule ND-1UT Item D.
    pub fn first_north_dakota_income(&self) -> Option<Date> {
        (!self.first_income.is_zero()).then_some(self.first_income)
    }
}

/// Builds [`EstimatedFacts`].
#[derive(Clone, Debug, Default)]
pub struct EstimatedFactsBuilder {
    e: EstimatedFacts,
}

/// Starts North Dakota's extras.
pub fn new_estimated_facts() -> EstimatedFactsBuilder {
    EstimatedFactsBuilder::default()
}

impl EstimatedFactsBuilder {
    /// Answers whether federal estimated tax is required.
    pub fn federal_estimates_required(&mut self, required: bool) -> &mut Self {
        (self.e.federal, self.e.federal_answered) = (required, true);
        self
    }

    /// Answers whether the federal January installment was excused.
    pub fn federal_fourth_quarter_excused(&mut self, excused: bool) -> &mut Self {
        (self.e.fourth, self.e.fourth_answered) = (excused, true);
        self
    }

    /// Answers whether the annualized method was used federally.
    pub fn annualised_federally(&mut self, used: bool) -> &mut Self {
        (self.e.annualised, self.e.annualised_answered) = (used, true);
        self
    }

    /// Gives the day North Dakota income was first received.
    pub fn first_north_dakota_income(&mut self, d: Date) -> &mut Self {
        self.e.first_income = d;
        self
    }

    /// Has nothing to refuse.
    pub fn build(&self) -> Result<EstimatedFacts, Error> {
        Ok(self.e)
    }
}

/// One tax year's published figures.
pub(crate) struct EstimatedYear {
    pub(crate) year: i64,

    /// § 57-38-62(3) and ND-1ES line 10: no interest when this year's tax
    /// less withholding is under it.
    pub(crate) threshold: USCents,

    /// § 57-38-62(1) and ND-1ES line 12: no estimates when last year's net
    /// tax liability was under it.
    pub(crate) prior_gate: USCents,

    pub(crate) this_year_share: Rate,
    pub(crate) prior_year_share: Rate,
    pub(crate) shares: Vec<Rate>,
    pub(crate) annualised: Vec<Rate>,
    pub(crate) dues: Vec<Date>,
    pub(crate) calendar: Calendar,
    pub(crate) period_ends: Vec<Date>,

    /// The first days of the Schedule ND-1UT Item D windows. Income first
    /// received on or after `item_d[i]` starts the installments at i+2.
    pub(crate) item_d: Vec<Date>,

    /// The federal file-and-pay date for the January installment: January
    /// 31, a Sunday in 2027, moved to the Monday.
    pub(crate) fourth_quarter_by: Date,

    pub(crate) stop: Date,

    /// ND-1UT lines 16c and 16d: the days divided by `day_basis` and rounded
    /// to `places`, then times `rate` and rounded to `places` again.
    pub(crate) rate: Rate,
    pub(crate) day_basis: i64,
    pub(crate) places: u32,

    /// ND-1UT line 17: on a return with no overpayment and no tax due,
    /// interest under it need not be sent with the return.
    pub(crate) small_interest: USCents,
}

/// Returns North Dakota's estimated tax rules for a tax year, or
/// [`Error::NoRules`] for a year this build does not carry.
pub fn estimated_tax(year: i64) -> Result<Box<dyn EstimatedTax>, Error> {
    let y = match year {
        2026 => crate::estimated_y2026::year_2026()?,
        _ => return Err(estimated::no_rules(&JURISDICTION, year)),
    };
    Ok(Box::new(NorthDakotaEstimated { y }))
}

struct NorthDakotaEstimated {
    y: EstimatedYear,
}

#[derive(Default)]
struct Assessment {
    requirement: Requirement,
    exempt: bool,
    tax_known: bool,
    dues: Vec<Date>,
    installments: Vec<USCents>,
    fourth_zero: bool,
    notes: Notes,
}

/// A question held back until it is known whether an exception sets the
/// target, as Go's deferred `unlessExempt`.
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

/// "Jan 15" inside the tax year and "Jan 15, 2027" outside it.
fn short_date(d: Date, year: i64) -> String {
    let m: String = d.month().to_string().chars().take(3).collect();
    if d.year() == year {
        return format!("{m} {}", d.day());
    }
    format!("{m} {}, {}", d.day(), d.year())
}

/// What the target leaves to pay after withholding, for a Why.
fn target_why(target: USCents, withheld: USCents) -> String {
    if withheld.0 == 0 {
        return format!("Your target is {}.", target.whole());
    }
    format!(
        "Your target is {}; after withholding that's {}.",
        target.whole(),
        (target - withheld).whole()
    )
}

/// What estimated payments the target leaves after withholding, for a Why.
fn left_why(target: USCents, withheld: USCents) -> String {
    if withheld.0 == 0 {
        return format!("That's {} of estimated payments.", target.whole());
    }
    format!(
        "After {} of withholding, that's {} of estimated payments.",
        withheld.whole(),
        (target - withheld).whole()
    )
}

/// What tax less withholding leaves, as a sentence for a Why.
fn owed_why(tax: USCents, withheld: USCents) -> String {
    if withheld.0 == 0 {
        return format!(
            "Your North Dakota tax is {}, with no withholding.",
            tax.whole()
        );
    }
    format!(
        "Your North Dakota tax is {}, and {} was withheld. That {}.",
        tax.whole(),
        withheld.whole(),
        estimated::leaves(tax - withheld)
    )
}

/// A shorthand for the year files.
pub(crate) fn date(y: i64, m: Month, d: i64) -> Date {
    estimated::must_date(y, m, d)
}

/// Reports whether `periods` show the filer arriving in North Dakota during
/// `year`, rather than living there from January 1 or never.
fn moved_in(periods: &[ResidencePeriod], year: i64) -> bool {
    let jan1 = date(year, Month::JANUARY, 1);
    let (mut arrived, mut from_start) = (false, false);
    for p in periods {
        if p.state() != JURISDICTION.as_str() {
            continue;
        }
        if p.from() == jan1 {
            from_start = true;
        } else if p.from().year() == year {
            arrived = true;
        }
    }
    arrived && !from_start
}

/// Part of a column's underpayment and the day it was paid or offset; the
/// zero Date if it never was.
#[derive(Clone, Copy, Debug, Default)]
struct Piece {
    amount: USCents,
    paid_on: Date,
}

/// One Schedule ND-1UT column after the credits are applied.
#[derive(Clone, Debug, Default)]
struct Column {
    due: Date,
    /// Line 7.
    #[allow(dead_code)]
    required: USCents,
    needed_by: USCents,
    credited_by: USCents,
    /// Line 15.
    shortfall: USCents,
    pieces: Vec<Piece>,
}

impl Column {
    fn outstanding(&self) -> USCents {
        let mut left = self.shortfall;
        for p in &self.pieces {
            if !p.paid_on.is_zero() {
                left -= p.amount;
            }
        }
        left
    }
}

/// Schedule ND-1UT lines 8 to 15 and the Part 3 application rule. A credit
/// counts in the column whose due date is the first on or after it. A
/// column's credits, with any overpayment carried in, pay that column first;
/// the excess pays earlier underpayments, earliest first, as of this
/// column's due date, and what is left carries on. A credit after the last
/// due date pays the earliest underpayment on its own date.
///
/// The returned flag reports a credit dated strictly between due dates that
/// arrived while an earlier column was still short, the one case where the
/// form's date-by-column reading and treating it as a late payment of the
/// earlier quarter give different interest.
fn apply_by_quarter(
    dues: &[Date],
    required: &[USCents],
    credits: &[Payment],
) -> (Vec<Column>, bool) {
    let zero = USCents(0);
    let mut cs = credits.to_vec();
    cs.sort_by_key(|c| c.on().days());
    let mut cols: Vec<Column> = vec![Column::default(); dues.len()];
    let mut between = false;
    let (mut carry, mut credited, mut needed) = (zero, zero, zero);
    let mut next = 0;
    for (i, due) in dues.iter().enumerate() {
        let due = *due;
        let mut in_column = zero;
        while next < cs.len() && !cs[next].on().after(due) {
            if i > 0 && cs[next].on().after(dues[i - 1]) && cs[next].on().before(due) {
                for col in &cols[..i] {
                    if col.outstanding() > zero {
                        between = true;
                    }
                }
            }
            in_column += cs[next].amount();
            credited += cs[next].amount();
            next += 1;
        }
        needed += required[i];
        let total = in_column + carry; // line 10
        cols[i] = Column {
            due,
            required: required[i],
            needed_by: needed,
            credited_by: credited,
            ..Column::default()
        };
        if total < required[i] {
            cols[i].shortfall = required[i] - total;
            carry = zero;
            continue;
        }
        let mut over = total - required[i]; // line 12
        for col in cols[..i].iter_mut() {
            if over <= zero {
                break;
            }
            let owed = col.outstanding();
            if owed <= zero {
                continue;
            }
            let paid = owed.min(over);
            col.pieces.push(Piece {
                amount: paid,
                paid_on: due,
            });
            over -= paid;
        }
        carry = over; // line 13
    }
    for c in &cs[next..] {
        let mut left = c.amount();
        for col in cols.iter_mut() {
            let owed = col.outstanding();
            if owed <= zero || left <= zero {
                continue;
            }
            let paid = owed.min(left);
            col.pieces.push(Piece {
                amount: paid,
                paid_on: c.on(),
            });
            left -= paid;
        }
    }
    for col in cols.iter_mut() {
        let owed = col.outstanding();
        if owed > zero {
            col.pieces.push(Piece {
                amount: owed,
                paid_on: Date::default(),
            });
        }
    }
    (cols, between)
}

/// 10 to the `n`.
fn pow10(n: u32) -> i64 {
    10_i64.pow(n)
}

/// Writes `units` at `places` decimal places, as "0.020".
fn printed(units: i64, places: u32) -> String {
    let p = pow10(places);
    format!(
        "{}.{:0width$}",
        units / p,
        units % p,
        width = places as usize
    )
}

impl EstimatedYear {
    /// Schedule ND-1UT lines 16c and 16d for a span of days: the fraction of
    /// the year and the interest factor, each in units of the last decimal
    /// place, both rounded half up.
    fn factor(&self, days: i64) -> (i64, i64) {
        let unit = pow10(self.places);
        let basis = self.day_basis;
        let fraction = (days * unit * 2 + basis) / (2 * basis);
        let hundred = i64::from(percent(100).0);
        let factor = (fraction * i64::from(self.rate.0) * 2 + hundred) / (2 * hundred);
        (fraction, factor)
    }

    /// ND-1UT line 16e for one piece: the amount times the factor, rounded
    /// to the cent.
    fn charge(&self, amount: USCents, from: Date, to: Date) -> Result<PenaltyPart, Error> {
        let days = to.days_since(from);
        let (fraction, factor) = self.factor(days);
        let source = format!(
            "Schedule ND-1UT (2025), lines 16b to 16e: {days} days ÷ {} = {}, × {} = {}",
            self.day_basis,
            printed(fraction, self.places),
            self.rate,
            printed(factor, self.places)
        );
        let mut c = USCents(0);
        if factor > 0 {
            let fac = estimated::parse_factor(&printed(factor, self.places))?;
            c = fac.of(amount, 1, Rounding::RoundToCent);
        }
        estimated::new_penalty_part(PartKind::Interest, amount, self.rate)
            .span(from, to)
            .days(days)
            .charge(c)
            .source(source)
            .build()
    }
}

impl NorthDakotaEstimated {
    fn extras(&self, f: &Facts) -> Result<EstimatedFacts, Error> {
        match f.state() {
            None => Ok(EstimatedFacts::default()),
            Some(s) => match s.downcast_ref::<EstimatedFacts>() {
                Some(e) => Ok(*e),
                None => Err(Error::Invalid(format!(
                    "northdakotataxcalc: the facts carry {}, not northdakotataxcalc.EstimatedFacts",
                    type_name(s)
                ))),
            },
        }
    }

    fn check(&self, f: &Facts) -> Result<(), Error> {
        if f.jurisdiction() != JURISDICTION {
            return Err(Error::Invalid(format!(
                "northdakotataxcalc: these are North Dakota's rules, and the facts are for {}",
                f.jurisdiction()
            )));
        }
        if f.tax_year() != self.y.year {
            return Err(Error::Invalid(format!(
                "northdakotataxcalc: these are the {} rules, and the facts are for {}",
                self.y.year,
                f.tax_year()
            )));
        }
        if f.farmer_or_fisher() {
            return Err(Error::with_detail(
                Error::Unsupported,
                "a qualified farmer uses 66 2/3% and may pay once by January 15, and the March 1 waivers apply; none of that is built",
            ));
        }
        if f.elections().annualised() {
            for (i, p) in f.periods().iter().enumerate() {
                let want = self.y.period_ends.get(i).copied().unwrap_or_default();
                if p.end() != want {
                    return Err(Error::Invalid(format!(
                        "northdakotataxcalc: Form 2210 Schedule AI period {} ends on {}, not {}",
                        i + 1,
                        want,
                        p.end()
                    )));
                }
            }
        }
        Ok(())
    }

    /// Where the department's forms and the statute part ways on a rule
    /// every North Dakota computation uses. The mover rule is conflict C1 in
    /// research/estimated-payments/nd.md; the rest were found reading
    /// Schedule ND-1UT against §§ 57-38-62 and 57-38-63. Each is marked as
    /// affecting these facts only where the readings give them different
    /// figures.
    fn divergences(
        &self,
        a: &Assessment,
        moved: bool,
        first: i64,
        cols: &[Column],
    ) -> Result<Vec<Divergence>, Error> {
        // order: a column fell short and a later credit could pay it, where
        // the form's own-quarter rule and the statute's date-paid rule can
        // differ.
        let (mut order, mut short) = (false, false);
        let last_credited = cols.last().map(|c| c.credited_by).unwrap_or_default();
        for c in cols {
            if c.shortfall.0 <= 0 {
                continue;
            }
            short = true;
            for p in &c.pieces {
                order = order || !p.paid_on.is_zero();
            }
            order = order || last_credited > c.credited_by;
        }
        let mut out = Vec::new();
        for b in [
            working::new_divergence("A filer who moved into North Dakota last year")
                .form("Form ND-1ES (2026) and the 2025 booklet keep the 100% prior-year option. They keep it for a mover who had North Dakota income before the move. Schedule ND-1UT (2025) line 6 does not.",
                    "Form ND-1ES (2026), Who must pay estimated tax, 4b; Schedule ND-1UT (2025), line 6")
                .statute("No one who moved into North Dakota last year can use the prior-year option. There is no exception for earlier North Dakota income.",
                    "N.D.C.C. § 57-38-62(1)(b)(1)")
                .uses(Reading::Statute)
                .affects(moved && !a.exempt)
                .clone(),
            working::new_divergence("The federal fourth-quarter exception")
                .form("You may not have to make the federal January payment. That happens if you filed and paid your federal return by the federal date. If so, the form enters zero for the fourth quarter.",
                    "Schedule ND-1UT (2025), Part 1 Item C and line 7")
                .statute("At least a quarter of the estimated tax is due on each of the four dates. No exception is tied to the federal return.",
                    "N.D.C.C. §§ 57-38-62(1), 57-38-63")
                .uses(Reading::Form)
                .affects(a.fourth_zero)
                .clone(),
            working::new_divergence("A nonresident or new resident with no North Dakota income until after April 15")
                .form("ND-1UT sets the split by when the income started. Apr 16 to May 31 means thirds from June 15. June to August means halves from September 15. Later, it is all due in January. ND-1ES says equal payments after the income starts. That differs for June 1 to 15 or September 1 to 15.",
                    "Schedule ND-1UT (2025), Part 1 Item D and line 7; Form ND-1ES (2026), Part-year requirement")
                .statute("At least a quarter of the estimated tax is due on each of the four dates. No rule covers a late start.",
                    "N.D.C.C. § 57-38-63")
                .uses(Reading::Form)
                .affects(first > 0)
                .clone(),
            working::new_divergence("Which shortfall a payment pays")
                .form("A quarter's payment goes first to that quarter. Only the excess pays an earlier shortfall, as of that later due date. So a payment between due dates stops earlier interest only at the next due date.",
                    "Schedule ND-1UT (2025), Instructions for Part 3 and line 16a")
                .statute("Interest runs from an installment's due date until that part of the tax is paid.",
                    "N.D.C.C. § 57-38-62(3)")
                .uses(Reading::Form)
                .affects(order)
                .clone(),
            working::new_divergence("How the days are counted")
                .form("The days late are divided by 365 and rounded to three decimal places. The result is multiplied by 12% and rounded to three places again. That factor then multiplies the underpayment.",
                    "Schedule ND-1UT (2025), lines 16b to 16e")
                .statute("Interest runs at a yearly rate, with no rounding of the fraction of the year.",
                    "N.D.C.C. § 57-38-62(3)")
                .uses(Reading::Form)
                .affects(short)
                .clone(),
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
            dues: estimated::move_dates(&y.calendar, &y.dues),
            ..Assessment::default()
        };
        let last = y.year - 1;

        let tax_opt = f.this_year().tax();
        let tax_known = tax_opt.is_some();
        let tax = tax_opt.unwrap_or_default();
        a.tax_known = tax_known;
        let withheld = f.withheld_total();
        let prior = f.prior_year();
        let has_prior = prior.is_some();
        let prior = prior.unwrap_or_default();
        let (mut prior_tax, mut prior_known) = (USCents(0), false);
        if has_prior {
            if let Some(t) = prior.figures().and_then(|pf| pf.tax()) {
                (prior_tax, prior_known) = (t, true);
            }
            if prior.not_required_to_file() {
                (prior_tax, prior_known) = (USCents(0), true);
            }
        }

        let this_year: TargetOption = if tax_known {
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
                "N.D.C.C. § 57-38-62(1)(a); Form ND-1ES (2026), line 11",
                format!(
                    "{} of this year's North Dakota net tax liability of {tax} is {}",
                    y.this_year_share,
                    this_year.amount()
                ),
            );
        }

        // A mover's residence last year: from the dated periods, or unknown
        // when only "not a full-year resident" was said.
        let moved = moved_in(&f.prior_residence(), last);
        // Questions about the target, asked only when no exception sets it.
        let mut unless_exempt: Vec<Ask> = Vec::new();
        if f.prior_residence().is_empty() && has_prior && prior.full_year_resident() == Some(false)
        {
            unless_exempt.push((
                PositionKind::Residency,
                format!("When did you live in North Dakota during {last}?"),
                format!("If you moved in that year, 100% of your {last} tax can't be your target."),
            ));
        }

        let unavailable = |why: String| -> TargetOptionBuilder {
            let mut b =
                estimated::new_target_option(Basis::PriorYearTax, y.prior_year_share, USCents(0));
            b.unavailable(why);
            b
        };
        let prior_effect = format!(
            "Under {}, no estimates are needed; otherwise 100% of it can be the target.",
            y.prior_gate.whole()
        );
        let prior_option = if !has_prior {
            unless_exempt.push((
                PositionKind::PriorYearReturn,
                format!(
                    "Did you file a {last} North Dakota return, and what was its net tax liability?"
                ),
                prior_effect.clone(),
            ));
            unavailable(format!("No {last} North Dakota return on record"))
        } else if prior.not_required_to_file() {
            unavailable(format!(
                "You weren't required to file a {last} North Dakota return"
            ))
        } else if !prior.filed() {
            unavailable(format!("You didn't file a {last} North Dakota return"))
        } else if !prior_known {
            unless_exempt.push((
                PositionKind::PriorYearReturn,
                format!("What was the net tax liability on your {last} North Dakota return?"),
                prior_effect.clone(),
            ));
            unavailable(format!("Needs your {last} North Dakota net tax liability"))
        } else if moved {
            a.notes.rule(
                "N.D.C.C. § 57-38-62(1)(b)(1); Schedule ND-1UT (2025), line 6",
                format!("a filer who moved into North Dakota during {last} must meet the 90% test"),
            );
            unavailable(format!("You moved into North Dakota during {last}"))
        } else {
            a.notes.rule(
                "N.D.C.C. § 57-38-62(1)(b); Form ND-1ES (2026), line 12",
                format!(
                    "the prior-year test is {} of the {prior_tax} net tax liability on the {last} return",
                    y.prior_year_share
                ),
            );
            estimated::new_target_option(
                Basis::PriorYearTax,
                y.prior_year_share,
                y.prior_year_share.of(prior_tax),
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
            unless_exempt.push((
                PositionKind::ThisYearTax,
                format!(
                    "What do you expect your {} North Dakota net tax liability to be?",
                    y.year
                ),
                estimated::without_this_year_tax(prior_opt.available()),
            ));
        }

        let (federal, federal_answered) = extras.federal_estimates_required();
        let mut exemption: Option<(String, TargetOption, &str, String)> = None;
        if federal_answered && !federal {
            let none = estimated::new_exception("No federal estimated tax required").build()?;
            exemption = Some((
                format!(
                    "You don't have to pay federal estimated tax for {}. North Dakota doesn't require estimated payments either.",
                    y.year
                ),
                none,
                "N.D.C.C. § 57-38-62(1); Form ND-1ES (2026), Who must pay estimated tax, 1",
                "only a filer subject to 26 U.S.C. § 6654 owes North Dakota estimated tax"
                    .to_string(),
            ));
        } else if prior_known && prior_tax < y.prior_gate {
            let below = estimated::exception_below(
                "Last year's North Dakota tax",
                prior_tax,
                y.prior_gate,
            )?;
            let why = if prior.not_required_to_file() {
                format!(
                    "You weren't required to file a {last} North Dakota return. No estimated payments are needed for {}.",
                    y.year
                )
            } else {
                format!(
                    "Your {last} North Dakota net tax liability was {}, under {}. No estimated payments are needed for {}.",
                    prior_tax.whole(),
                    y.prior_gate.whole(),
                    y.year
                )
            };
            exemption = Some((
                why,
                below,
                "N.D.C.C. § 57-38-62(1); Form ND-1ES (2026), line 12",
                format!(
                    "a {last} net tax liability of {prior_tax} is under {}, and a filer not required to file counts as zero, so no estimates are required",
                    y.prior_gate
                ),
            ));
        } else if tax_known && tax - withheld < y.threshold {
            let below =
                estimated::exception_below("Tax less withholding", tax - withheld, y.threshold)?;
            let why = if withheld > tax {
                format!(
                    "{} No estimated payments are needed.",
                    owed_why(tax, withheld)
                )
            } else if withheld == tax {
                format!(
                    "Your withholding of {} equals your North Dakota tax. No estimated payments are needed.",
                    withheld.whole()
                )
            } else {
                format!(
                    "{} That's under {}, so no estimated payments are needed.",
                    owed_why(tax, withheld),
                    y.threshold.whole()
                )
            };
            exemption = Some((
                why,
                below,
                "N.D.C.C. § 57-38-62(3); Form ND-1ES (2026), line 10",
                format!(
                    "{tax} of tax less {withheld} withheld is {}, under {}, so there is no interest",
                    tax - withheld,
                    y.threshold
                ),
            ));
        } else if !tax_known && !prior_opt.available() {
            rb.required(
                false,
                format!(
                    "With no usable {last} liability, your target is 90% of your {} North Dakota tax.",
                    y.year
                ),
            )
            .no_target();
        } else if !tax_known && withheld >= target {
            rb.required(
                false,
                format!(
                    "Your withholding of {} reaches 100% of your {last} liability, {}. No estimated payments are needed, whatever your {} tax turns out to be.",
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
                    "Your target is 100% of your {last} liability, {}. That keeps you clear of interest whatever your {} tax turns out to be. {}",
                    target.whole(),
                    y.year,
                    left_why(target, withheld)
                ),
            )
            .choose(chosen);
        } else if withheld >= target {
            rb.required(
                false,
                format!(
                    "Your North Dakota withholding of {} covers the {} needed for {}. No estimated payments are needed.",
                    withheld.whole(),
                    target.whole(),
                    y.year
                ),
            )
            .choose(chosen);
        } else {
            rb.required(
                true,
                format!(
                    "{} That's {} or more, so estimated payments are needed. {}",
                    owed_why(tax, withheld),
                    y.threshold.whole(),
                    target_why(target, withheld)
                ),
            )
            .choose(chosen);
        }
        if let Some((why, exception, cite, applied)) = exemption {
            a.exempt = true;
            rb.required(false, why).exempt(exception);
            a.notes.rule(cite, applied);
        }
        a.requirement = rb.build()?;
        if !a.exempt {
            for (k, q, e) in unless_exempt {
                a.notes.ask(k, q, e);
            }
        }
        if !a.exempt && !federal_answered {
            a.notes.ask(
                PositionKind::JurisdictionFact,
                format!("Do you have to pay federal estimated tax for {}?", y.year),
                "If you don't, North Dakota doesn't require estimated payments either.",
            );
        }

        // The installments: four quarters, or Item D's late start.
        let mut first = 0;
        if !a.exempt {
            let this_res = f.residence();
            let jan1 = date(y.year, Month::JANUARY, 1);
            let eligible = !this_res.is_empty()
                && !estimated::resident_all_year(&this_res, &JURISDICTION, y.year)
                && !this_res
                    .iter()
                    .any(|p| p.state() == JURISDICTION.as_str() && p.from() == jan1);
            let income = extras.first_north_dakota_income();
            if eligible && let Some(income) = income {
                first = estimated::late_start(income, &y.item_d);
                if first > 0 {
                    a.notes.rule(
                        "Schedule ND-1UT (2025), Part 1 Item D and line 7",
                        format!(
                            "North Dakota income was first received on {}, after {}, so the required payment is spread equally over the {} installments from {}",
                            income.long(),
                            y.dues[0].long(),
                            a.dues.len() as i64 - first,
                            a.dues[first as usize].long()
                        ),
                    );
                }
            } else if eligible {
                a.notes.ask(
                    PositionKind::RequirementFirstMet,
                    format!(
                        "When did you first receive North Dakota income in {}?",
                        y.year
                    ),
                    format!(
                        "If it was after {}, you pay in fewer, larger installments.",
                        short_date(y.dues[0], y.year)
                    ),
                );
            } else if income.is_some() && this_res.is_empty() {
                a.notes.ask(
                    PositionKind::Residency,
                    format!("Where did you live during {}?", y.year),
                    format!(
                        "North Dakota income begun after {} means fewer, larger installments. That holds if you weren't a North Dakota resident all year.",
                        short_date(y.dues[0], y.year)
                    ),
                );
            }
        }
        let cumulative =
            estimated::equal_shares(a.requirement.target(), a.dues.len() as i64, first)?;
        a.installments = estimated::increments(&cumulative);
        a.notes.rule(
            "N.D.C.C. § 57-38-63; Schedule ND-1UT (2025), line 7",
            format!(
                "of {}, {}, {}, {} and {} are due on {}, {}, {} and {}",
                a.requirement.target(),
                a.installments[0],
                a.installments[1],
                a.installments[2],
                a.installments[3],
                a.dues[0].long(),
                a.dues[1].long(),
                a.dues[2].long(),
                a.dues[3].long()
            ),
        );

        if f.elections().annualised() && !a.exempt {
            let (used, answered) = extras.annualised_federally();
            if answered && !used {
                a.notes.rule(
                    "Schedule ND-1UT (2025), Part 1 Item B; N.D. Admin. Code § 81-03-04-02(3)(c)",
                    "the annualized method is allowed only if it was used on the federal Form 2210, and it was not, so the regular installments are used",
                );
            } else {
                if !answered {
                    a.notes.ask(
                        PositionKind::JurisdictionFact,
                        "Did you use the annualized method on your federal Form 2210?",
                        "Until then, the figures use it, though North Dakota allows it only if you did.",
                    );
                }
                let annual: Vec<USCents> = f.periods().iter().map(|p| p.annualised_tax()).collect();
                let ann = estimated::annualised(&annual, &y.annualised, &a.installments)?;
                a.notes.rule(
                    "Schedule ND-1UT (2025), Part 1 Item B and line 7; Form 2210 (2025), Schedule AI line 20",
                    format!(
                        "under the annualized method the installments are {}, {}, {} and {}: 22.5%, 45%, 67.5% and 90% of the annualized North Dakota tax, \
                         less earlier installments, and never more than the regular installment plus any reduction carried forward",
                        ann[0], ann[1], ann[2], ann[3]
                    ),
                );
                a.installments = ann;
            }
        }

        // Item C: the federal fourth-quarter exception zeroes the January
        // column.
        if !a.exempt {
            let (excused, answered) = extras.federal_fourth_quarter_excused();
            if excused {
                a.fourth_zero = true;
                if let Some(last) = a.installments.last_mut() {
                    *last = USCents(0);
                }
                a.notes.rule(
                    "Schedule ND-1UT (2025), Part 1 Item C and line 7",
                    format!(
                        "the federal January installment was not required, the federal return having been filed and paid by {}, so nothing is due on {}",
                        y.fourth_quarter_by.long(),
                        a.dues[a.dues.len() - 1].long()
                    ),
                );
            } else if !answered {
                let filed = f.return_filed();
                if !filed.is_zero() && !filed.after(y.fourth_quarter_by) {
                    a.notes.ask(
                        PositionKind::JurisdictionFact,
                        format!(
                            "Did you file your federal return and pay the federal tax by {}?",
                            short_date(y.fourth_quarter_by, y.year)
                        ),
                        "If you did, nothing is due on your North Dakota January payment.",
                    );
                }
            }
        }
        if a.exempt {
            a.installments = vec![USCents(0); a.dues.len()];
        }
        let (cols, _) = self.ledger(f, &a)?;
        let divergences = self.divergences(&a, moved, first, &cols)?;
        a.notes.diverges(&divergences);
        Ok(a)
    }

    fn ledger(&self, f: &Facts, a: &Assessment) -> Result<(Vec<Column>, Notes), Error> {
        let credits = if f.elections().withholding_as_withheld() {
            estimated::credits_as_withheld(f, &a.dues)?
        } else {
            estimated::credits(f, &a.dues, &self.y.shares)?
        };
        let (cols, between) = apply_by_quarter(&a.dues, &a.installments, &credits);
        let mut notes = Notes::default();
        if f.elections().withholding_as_withheld() {
            notes.rule(
                "Schedule ND-1UT (2025), line 8 instructions",
                format!(
                    "as shown, the {} withheld is credited on the dates it was withheld",
                    f.withheld_total()
                ),
            );
        } else {
            notes.rule(
                "Schedule ND-1UT (2025), line 8 instructions",
                format!(
                    "one-fourth of the {} withheld is treated as withheld by each due date",
                    f.withheld_total()
                ),
            );
        }
        notes.rule(
            "Schedule ND-1UT (2025), Instructions for Part 3",
            "a payment counts in the quarter whose due date it falls before and pays that quarter first; only the excess pays an earlier underpayment, as of that quarter's due date",
        );
        if between {
            notes.ask(
                PositionKind::UnsettledRule,
                "",
                "North Dakota doesn't say if a payment between due dates stops interest that day. The figures stop an earlier quarter's interest at the next due date.",
            );
        }
        Ok((cols, notes))
    }

    /// The schedule from the columns. The January installment is excused
    /// under the federal fourth-quarter exception.
    fn installments(&self, a: &Assessment, cols: &[Column]) -> Result<Vec<Installment>, Error> {
        let mut out = Vec::with_capacity(cols.len());
        for (i, c) in cols.iter().enumerate() {
            let mut b = estimated::new_installment(i as i64 + 1, c.due);
            b.needed_by(c.needed_by)
                .credited_by(c.credited_by)
                .shortfall(c.shortfall);
            if a.fourth_zero && i == cols.len() - 1 {
                b.excused();
            }
            out.push(b.build()?);
        }
        Ok(out)
    }
}

/// Decides a waiver asked for. North Dakota has no casualty, retirement or
/// disability waiver; N.D.C.C. § 57-38-45(5) lets the Tax Commissioner waive
/// interest for good cause shown, and names no causes, so every request is
/// undecided.
fn north_dakota_waiver(f: &Facts) -> Option<WaiverOutcome> {
    let p = f.positions();
    let k = p.waiver()?;
    if k == WaiverKind::None {
        return None;
    }
    Some(if p.waiver_cause() == WaiverCause::Disaster {
        estimated::waiver_undecided(
            "North Dakota has no disaster waiver as such but follows IRS disaster extensions. The Tax Commissioner can waive all or part of the interest for good cause shown.",
        )
    } else if k == WaiverKind::Casualty {
        estimated::waiver_undecided(
            "North Dakota has no casualty waiver as such. The Tax Commissioner can waive all or part of the interest for good cause shown.",
        )
    } else {
        estimated::waiver_undecided(
            "North Dakota has no waiver for retirement or disability. The Tax Commissioner can waive all or part of the interest for good cause shown.",
        )
    })
}

impl EstimatedTax for NorthDakotaEstimated {
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
                    "with no {} North Dakota return to measure against, there is no target to schedule",
                    self.y.year - 1
                ),
            ));
        }
        let (cols, notes) = self.ledger(f, &a)?;
        let out = self.installments(&a, &cols)?;
        Ok(working::new_result(out)
            .with(&a.notes)
            .with(&notes)
            .build()?)
    }

    fn penalty(&self, f: &Facts, as_of: Date) -> Result<working::Result<Penalty>, Error> {
        if as_of.is_zero() {
            return Err(Error::Invalid(
                "northdakotataxcalc: interest needs the date to figure it to".into(),
            ));
        }
        let a = self.assess(f)?;
        if !a.requirement.target_known() {
            return Err(Error::with_detail(
                Error::NeedsThisYearTax,
                format!(
                    "with no {} North Dakota return to measure against, there is no target to figure the interest from",
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
        let waiver_asked = f.positions().waiver();
        let waiver = waiver_asked.unwrap_or_default();
        let (cols, mut notes) = self.ledger(f, &a)?;
        let schedule = self.installments(&a, &cols)?;
        if a.exempt {
            let p = estimated::new_penalty(stop)
                .waiver(waiver)
                .because(a.requirement.clone(), &schedule)
                .build()?;
            return Ok(working::new_result(p).with(&a.notes).build()?);
        }

        let mut pb = estimated::new_penalty(stop);
        pb.waiver(waiver).because(a.requirement.clone(), &schedule);
        for (i, c) in cols.iter().enumerate() {
            if c.shortfall.0 <= 0 || !c.due.before(stop) {
                continue;
            }
            let mut lb = estimated::new_penalty_line(i as i64 + 1, c.due, c.shortfall);
            let mut last_day = c.due;
            for p in &c.pieces {
                let mut to = stop;
                if !p.paid_on.is_zero() && p.paid_on.before(stop) {
                    to = p.paid_on;
                }
                if !to.after(c.due) {
                    continue;
                }
                lb.part(y.charge(p.amount, c.due, to)?);
                if to.after(last_day) {
                    last_day = to;
                }
            }
            pb.line(lb.days(last_day.days_since(c.due)).build()?);
        }
        if !a.tax_known {
            pb.upper_bound();
        }
        let mut p = pb.build()?;
        if !a.tax_known {
            notes.rule(
                "N.D.C.C. § 57-38-62(1)",
                format!(
                    "this year's North Dakota tax was not given, so the interest is figured against the {} liability alone; \
                     the required payment is the lesser of that and 90% of this year's tax, so {} is the most the interest can be, and it is less if this year's tax turns out lower",
                    y.year - 1,
                    p.total()
                ),
            );
        }
        notes.rule(
            "N.D.C.C. §§ 57-38-62(3), 57-38-45; Schedule ND-1UT (2025), lines 16a to 17",
            format!(
                "each quarter's underpayment is charged {} a year: the days late ÷ {}, rounded to {} places, × {}, rounded again, × the underpayment, from its due date until paid or {}; \
                 the interest to {} is {}",
                y.rate,
                y.day_basis,
                y.places,
                y.rate,
                y.stop.long(),
                stop.long(),
                p.total()
            ),
        );
        if p.total().0 > 0 && p.total() < y.small_interest {
            notes.rule(
                "Schedule ND-1UT (2025), line 17 instructions",
                format!(
                    "if the return shows no overpayment and no tax due, interest under {} need not be sent with it; the interest is shown in full",
                    y.small_interest
                ),
            );
        }
        if p.total().0 > 0 && waiver_asked.is_none() {
            notes.ask(
                PositionKind::PenaltyWaiver,
                "Do you have good cause for a waiver of interest, or an IRS disaster extension?",
                "Either one can remove the interest, which is shown in full until then. The Tax Commissioner decides a waiver for good cause.",
            );
        } else if waiver_asked.is_some() && waiver != WaiverKind::None {
            notes.rule(
                "N.D.C.C. § 57-38-45(5); Office of State Tax Commissioner, Disaster Information and Tax Implications",
                format!(
                    "North Dakota has no waiver for {waiver} as such; the Tax Commissioner may waive interest for good cause shown, and an IRS disaster extension carries over automatically; the interest is shown before any waiver"
                ),
            );
        }
        if let Some(o) = north_dakota_waiver(f)
            && p.total().0 > 0
        {
            p = p.to_builder().waiver_outcome(o).build()?;
            notes.ask(
                PositionKind::UnsettledRule,
                "",
                "North Dakota lets the Tax Commissioner waive interest for good cause shown. It doesn't say if a casualty, disaster, retirement or disability is good cause.",
            );
        }
        Ok(working::new_result(p).with(&a.notes).with(&notes).build()?)
    }

    fn explanation(&self) -> Explanation {
        north_dakota_explanation()
    }
}

/// Written by hand and checked against research/estimated-payments/nd.md.
fn north_dakota_explanation() -> Explanation {
    estimated::new_explanation(
        "North Dakota expects estimates only if you must also pay federal estimated tax. Your North Dakota tax last year must also have been $1,000 or more. This year's tax less withholding must also be $1,000 or more. Falling short costs interest at 12% a year.",
    )
    .paragraph("Your target is the smaller of 90% of this year's tax or 100% of last year's. You're clear if withholding and estimated payments reach it. A quarter is due by April 15, June 15, September 15 and January 15. There is no 110% figure for higher incomes. Last year's return need not cover a full year. If you weren't required to file last year, no estimates are due this year. Withholding counts as paid evenly unless you show when it was taken.")
    .paragraph("An early federal return and payment can excuse the federal January payment. If it does, nothing is due for North Dakota's January payment either. The annualized method is allowed only if you used it on your federal Form 2210.")
    .paragraph("Interest runs from each due date until the shortfall is paid. It stops on April 15 of the next year. A payment goes first to its own quarter. Only the excess pays an earlier shortfall, as of that later due date. The form rounds the fraction of the year to three decimal places. The Tax Commissioner can waive interest for good cause. North Dakota follows an IRS disaster extension automatically.")
    .versus_federal("Unlike federal, North Dakota adds a $1,000 test on last year's tax. North Dakota has no 110% tier or twelve-month test. The charge is a fixed 12% a year. A payment pays its own quarter first. The day count is rounded to three places.")
    .source("N.D.C.C. §§ 57-38-45, 57-38-62, 57-38-63, 1-03-05")
    .source("N.D. Admin. Code § 81-03-04-02")
    .source("Schedule ND-1UT, 2025 Underpayment or Late Payment of Estimated Income Tax for Individuals, and its instructions")
    .source("Form ND-1ES, Estimated Income Tax - Individuals, 2026")
    .source("Office of State Tax Commissioner, Disaster Information and Tax Implications")
    .build()
    .unwrap_or_default()
}
