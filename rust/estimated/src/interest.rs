//! Interest: rate series, and interest accrued daily, compounded or by the
//! month.

use money::{FineRate, Rate, USCents};
use num_bigint::BigInt;
use num_integer::Integer;
use num_traits::Signed;

use crate::bigfloat::BigFloat;
use crate::date::{Month, days_in_year, earlier};
use crate::output::{PartKind, PenaltyPart, new_penalty_part};
use crate::{Date, Error, invalid};

/// An interest rate and the days it applies to, both ends included, with the
/// notice that set it.
///
/// The rate is held in millionths, so that a rate published to four decimal
/// places, as Maryland's 10.8133% is, is charged as published.
#[derive(Clone, Debug, Default, PartialEq, Eq, Hash)]
pub struct RatePeriod {
    from: Date,
    to: Date,
    rate: FineRate,
    source: String,
}

impl RatePeriod {
    /// The first day.
    pub fn from(&self) -> Date {
        self.from
    }

    /// The last day.
    pub fn to(&self) -> Date {
        self.to
    }

    /// The rate to the nearest basis point. It is exact for a period added
    /// with [`RateSeriesBuilder::period`].
    pub fn rate(&self) -> Rate {
        self.rate.nearest()
    }

    /// The rate as published.
    pub fn fine_rate(&self) -> FineRate {
        self.rate
    }

    /// Cites the notice, revenue ruling or page that published the rate.
    pub fn source(&self) -> String {
        self.source.clone()
    }
}

/// A run of rate periods with no gap between them.
///
/// A rate is a dated series rather than a number because an underpayment
/// can span two periods with different rates, and the penalty forms charge
/// each span at its own. The series ends where the last published rate ends;
/// days after that have no rate, and the accrual helpers report them rather
/// than carry the last rate forward.
#[derive(Clone, Debug, Default, PartialEq, Eq, Hash)]
pub struct RateSeries {
    periods: Vec<RatePeriod>,
}

impl RateSeries {
    /// Returns the periods, earliest first.
    pub fn periods(&self) -> Vec<RatePeriod> {
        self.periods.clone()
    }

    /// The last day the series has a rate for.
    pub fn ends(&self) -> Date {
        self.periods.last().map(|p| p.to).unwrap_or_default()
    }

    /// Returns the period that covers `d`.
    pub fn on(&self, d: Date) -> Option<RatePeriod> {
        self.on_ref(d).cloned()
    }

    fn on_ref(&self, d: Date) -> Option<&RatePeriod> {
        self.periods
            .iter()
            .find(|p| !d.before(p.from) && !d.after(p.to))
    }
}

/// Builds a [`RateSeries`].
#[derive(Clone, Debug, Default)]
pub struct RateSeriesBuilder {
    s: RateSeries,
}

/// Starts a rate series.
pub fn new_rate_series() -> RateSeriesBuilder {
    RateSeriesBuilder::default()
}

impl RateSeriesBuilder {
    /// Adds the rate for the days `from` through `to`, both included.
    pub fn period(
        &mut self,
        from: Date,
        to: Date,
        rate: Rate,
        source: impl Into<String>,
    ) -> &mut Self {
        self.fine_period(from, to, rate.fine(), source)
    }

    /// Adds a rate that basis points cannot hold for the days `from` through
    /// `to`, both included.
    pub fn fine_period(
        &mut self,
        from: Date,
        to: Date,
        rate: FineRate,
        source: impl Into<String>,
    ) -> &mut Self {
        self.s.periods.push(RatePeriod {
            from,
            to,
            rate,
            source: source.into(),
        });
        self
    }

    /// Checks every period is sourced and runs forward, and that each
    /// starts the day after the one before ends. A gap would leave days with
    /// no rate that nothing reported, and an overlap would give a day two.
    pub fn build(&self) -> Result<RateSeries, Error> {
        let ps = &self.s.periods;
        if ps.is_empty() {
            return Err(invalid!(
                "estimated: a rate series needs at least one period"
            ));
        }
        for (i, p) in ps.iter().enumerate() {
            if p.from.is_zero() || p.to.is_zero() || p.to.before(p.from) {
                return Err(invalid!(
                    "estimated: rate period {} to {} does not run forward",
                    p.from,
                    p.to
                ));
            }
            if p.rate.0 < 0 {
                return Err(invalid!("estimated: rate {} is negative", p.rate));
            }
            if p.source.is_empty() {
                return Err(invalid!(
                    "estimated: the rate from {} has no source",
                    p.from
                ));
            }
            if i > 0 && p.from != ps[i - 1].to.add_days(1) {
                return Err(invalid!(
                    "estimated: the rate from {} does not follow the period ending {}",
                    p.from,
                    ps[i - 1].to
                ));
            }
        }
        Ok(self.s.clone())
    }
}

/// A run of days under one rate and one length of year: interest for the
/// days after `from`, up to and including `to`.
struct Segment<'a> {
    from: Date,
    to: Date,
    period: &'a RatePeriod,
}

/// Splits the days after `from` up to and including `to` at each rate
/// period's end, and at a year end where a leap year starts or stops. A
/// span that crosses into a year of the same length stays whole, as FTB 5805
/// counts July 1, 2025 to April 15, 2026 as one span of 289 days. It returns
/// the first day no rate covers, or the zero Date when every day is covered.
fn segments(from: Date, to: Date, s: &RateSeries) -> (Vec<Segment<'_>>, Date) {
    let mut out = Vec::new();
    let mut cur = from;
    while cur.before(to) {
        let day = cur.add_days(1);
        let Some(p) = s.on_ref(day) else {
            return (out, day);
        };
        let mut end = earlier(to, p.to);
        if days_in_year(day.year) != days_in_year(day.year + 1) {
            end = earlier(
                end,
                Date {
                    year: day.year,
                    month: Month::DECEMBER,
                    day: 31,
                },
            );
        }
        out.push(Segment {
            from: cur,
            to: end,
            period: p,
        });
        cur = end;
    }
    (out, Date::default())
}

/// The low 64 bits of `n`, with its sign, as Go's `big.Int.Int64`.
pub(crate) fn low_i64(n: &BigInt) -> i64 {
    let (sign, digits) = n.to_u64_digits();
    let v = digits.first().copied().unwrap_or(0) as i64;
    if sign == num_bigint::Sign::Minus {
        v.wrapping_neg()
    } else {
        v
    }
}

/// Divides `n` by `d`, rounding half away from zero, as [`Rate::of`] does.
pub(crate) fn round_div(n: &BigInt, d: &BigInt) -> USCents {
    let (mut q, r) = n.div_rem(d);
    if r.abs() * 2 >= d.abs() {
        if (n.is_negative() && d.is_positive()) || (n.is_positive() && d.is_negative()) {
            q -= 1;
        } else {
            q += 1;
        }
    }
    USCents(low_i64(&q))
}

/// Accrues simple interest on `principal` for the days after `from` up to
/// and including `to`, one part for each rate period and calendar year the
/// days fall in.
///
/// Each part is principal × days ÷ days in the year × rate, rounded to the
/// cent, with the rate as published: the computation on Form 2210's penalty
/// worksheet and FTB 5805 Worksheet II, which divide by 365, or 366 in a
/// leap year. A part never spans years of different lengths.
///
/// The returned date, `unrated`, is the first day the series has no rate
/// for, or the zero Date. The parts stop the day before it, and the caller
/// must report the gap.
pub fn simple_daily(
    principal: USCents,
    from: Date,
    to: Date,
    s: &RateSeries,
) -> Result<(Vec<PenaltyPart>, Date), Error> {
    let (segs, unrated) = segments(from, to, s);
    let mut parts = Vec::with_capacity(segs.len());
    for g in segs {
        let days = g.to.days_since(g.from);
        let n = BigInt::from(principal.0) * BigInt::from(days) * BigInt::from(g.period.rate.0);
        let d = BigInt::from(1_000_000 * days_in_year(g.to.year));
        parts.push(
            new_penalty_part(PartKind::Interest, principal, Rate(0))
                .fine_rate(g.period.rate)
                .span(g.from, g.to)
                .days(days)
                .charge(round_div(&n, &d))
                .source(g.period.source.clone())
                .build()?,
        );
    }
    Ok((parts, unrated))
}

/// Accrues interest compounded daily on `principal`, as 26 U.S.C. § 6622
/// compounds interest on a federal underpayment of tax and as several states
/// compound their estimated tax penalty.
///
/// Each part's charge is the balance at its start, principal plus the
/// charges before it, times (1 + rate ÷ days in the year) raised to the
/// part's days, less the balance; rounded to the cent.
/// [`PenaltyPart::principal`] is the original principal throughout. The
/// arithmetic is Go's 256-bit `big.Float`, rounded as it rounds.
pub fn compound_daily(
    principal: USCents,
    from: Date,
    to: Date,
    s: &RateSeries,
) -> Result<(Vec<PenaltyPart>, Date), Error> {
    let (segs, unrated) = segments(from, to, s);
    let mut balance = BigFloat::from_i64(principal.0);
    let mut parts = Vec::with_capacity(segs.len());
    for g in segs {
        let days = g.to.days_since(g.from);
        let daily = BigFloat::from_i64(g.period.rate.0)
            .quo(&BigFloat::from_i64(1_000_000 * days_in_year(g.to.year)));
        let mut factor = BigFloat::from_i64(1);
        let step = BigFloat::from_i64(1).add(&daily);
        for _ in 0..days {
            factor = factor.mul(&step);
        }
        let grown = balance.mul(&factor);
        let interest = grown.sub(&balance);
        let charge = round_float(&interest);
        parts.push(
            new_penalty_part(PartKind::Interest, principal, Rate(0))
                .fine_rate(g.period.rate)
                .span(g.from, g.to)
                .days(days)
                .charge(charge)
                .source(g.period.source.clone())
                .build()?,
        );
        balance = balance.add(&BigFloat::from_i64(charge.0));
    }
    Ok((parts, unrated))
}

fn round_float(f: &BigFloat) -> USCents {
    let half = if f.is_negative() {
        BigFloat::half().neg()
    } else {
        BigFloat::half()
    };
    USCents(low_i64(&f.add(&half).int()))
}

/// Counts the months from `from` to `to` with any part of a month counted as
/// a whole one. April 15th to May 15th is one month; to May 16th is two.
/// Zero when `to` is not after `from`.
pub fn months_or_part(from: Date, to: Date) -> i64 {
    if !to.after(from) {
        return 0;
    }
    let mut k = 1;
    while from.add_months(k).before(to) {
        k += 1;
    }
    k
}

/// Charges `monthly_rate` of `principal` for each month or part of a month
/// from `from` to `to`, as a state that charges "per month or fraction of a
/// month" does. The charge is rounded once, on the whole span.
pub fn monthly(
    principal: USCents,
    from: Date,
    to: Date,
    monthly_rate: Rate,
    source: impl Into<String>,
) -> Result<PenaltyPart, Error> {
    let months = months_or_part(from, to);
    let n = BigInt::from(principal.0) * BigInt::from(monthly_rate.0) * BigInt::from(months);
    let charge = round_div(&n, &BigInt::from(10_000));
    new_penalty_part(PartKind::Monthly, principal, monthly_rate)
        .span(from, to)
        .months(months)
        .charge(charge)
        .source(source)
        .build()
}
