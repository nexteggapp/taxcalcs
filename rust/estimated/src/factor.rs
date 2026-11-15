//! Some penalty forms do not charge an annual rate. They print a factor, a
//! decimal already worked out from the rate, and the filer multiplies by it:
//! Pennsylvania's REV-1611 prints 0.000192 a day for 7%, and Maryland's Form
//! 502UP prints one factor for each span between due dates. The printed
//! factor is rounded, so the form's figure and the rate's figure differ, and
//! a calculator that follows the form must use the factor as printed.

use std::fmt;

use money::{DOLLAR, Rate, USCents};
use num_bigint::BigInt;
use num_traits::Pow;

use crate::date::earlier;
use crate::interest::round_div;
use crate::output::{PartKind, PenaltyPart, new_penalty_part};
use crate::{Date, Error, invalid};

/// An interest factor as a form prints it.
#[derive(Clone, Debug, Default, PartialEq, Eq, Hash)]
pub struct Factor {
    /// The digits, without the point.
    units: i64,
    /// How many of them follow the point.
    places: u32,
    printed: String,
}

/// Reads a factor written as the form prints it, as "0.000192". It refuses
/// anything but digits with at most one point, and a factor of zero.
pub fn parse_factor(printed: &str) -> Result<Factor, Error> {
    let (whole, frac) = printed.split_once('.').unwrap_or((printed, ""));
    let digits = format!("{whole}{frac}");
    if digits.is_empty() || printed.matches('.').count() > 1 {
        return Err(invalid!(
            "estimated: {} is not a printed factor",
            working::quote(printed)
        ));
    }
    let mut units: i64 = 0;
    for c in digits.chars() {
        if !c.is_ascii_digit() {
            return Err(invalid!(
                "estimated: {} is not a printed factor",
                working::quote(printed)
            ));
        }
        if units > (1i64 << 62) / 10 {
            return Err(invalid!(
                "estimated: {} has too many digits",
                working::quote(printed)
            ));
        }
        units = units * 10 + i64::from(c as u8 - b'0');
    }
    if units == 0 {
        return Err(invalid!(
            "estimated: a factor of {} charges nothing",
            working::quote(printed)
        ));
    }
    Ok(Factor {
        units,
        places: frac.len() as u32,
        printed: printed.to_string(),
    })
}

/// The factor as printed.
impl fmt::Display for Factor {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(&self.printed)
    }
}

/// How a form rounds a charge it computes from a factor.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub enum Rounding {
    /// No rounding; a factor series refuses it.
    #[default]
    Unspecified,

    /// Rounds each charge to the nearest cent, half up.
    RoundToCent,

    /// Rounds each charge to the nearest whole dollar, half up, as REV-1630
    /// lines 15a to 15c do.
    RoundToDollar,
}

/// Names the rounding: "to the cent" or "to the whole dollar".
impl fmt::Display for Rounding {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(match self {
            Rounding::RoundToCent => "to the cent",
            Rounding::RoundToDollar => "to the whole dollar",
            Rounding::Unspecified => "unspecified rounding",
        })
    }
}

impl Rounding {
    fn unit(self) -> i64 {
        if self == Rounding::RoundToDollar {
            DOLLAR.0
        } else {
            1
        }
    }

    /// `n` ÷ `d` cents rounded to the unit `self` names, halves away from
    /// zero, so half up for amounts that are not negative. It is the
    /// rounding for a form that rounds each charge or installment to the
    /// whole dollar without first rounding it to the cent.
    pub fn div(self, n: &BigInt, d: &BigInt) -> USCents {
        let u = BigInt::from(self.unit());
        round_div(n, &(d * u)) * self.unit()
    }

    /// Rounds `a` to the unit `self` names, halves away from zero.
    pub fn round(self, a: USCents) -> USCents {
        self.div(&BigInt::from(a.0), &BigInt::from(1))
    }
}

impl Factor {
    /// principal × times × the factor, rounded half up as `r` says. `times`
    /// is the days for a daily factor and 1 for a factor printed for a whole
    /// span.
    pub fn of(&self, principal: USCents, times: i64, r: Rounding) -> USCents {
        let n = BigInt::from(principal.0) * BigInt::from(times) * BigInt::from(self.units);
        let d: BigInt = Pow::pow(BigInt::from(10), self.places) * BigInt::from(r.unit());
        round_div(&n, &d) * r.unit()
    }
}

/// A daily factor and the days it applies to, both ends included, with the
/// annual rate it stands for and the document that printed it.
#[derive(Clone, Debug, Default, PartialEq, Eq, Hash)]
pub struct FactorPeriod {
    from: Date,
    to: Date,
    factor: Factor,
    annual: Rate,
    source: String,
}

impl FactorPeriod {
    /// The first day.
    pub fn from(&self) -> Date {
        self.from
    }

    /// The last day.
    pub fn to(&self) -> Date {
        self.to
    }

    /// The factor.
    pub fn factor(&self) -> Factor {
        self.factor.clone()
    }

    /// The annual rate the factor stands for.
    pub fn rate(&self) -> Rate {
        self.annual
    }

    /// Cites the document that printed the factor.
    pub fn source(&self) -> String {
        self.source.clone()
    }
}

/// A run of daily factors with no gap between them, and how the form rounds
/// what each one charges.
///
/// Like a [`crate::RateSeries`], it ends where the last printed factor ends.
/// Days after that have no factor, and [`daily_factor`] reports them rather
/// than carry the last one forward.
#[derive(Clone, Debug, Default, PartialEq, Eq, Hash)]
pub struct FactorSeries {
    periods: Vec<FactorPeriod>,
    rounding: Rounding,
}

impl FactorSeries {
    /// Returns the periods, earliest first.
    pub fn periods(&self) -> Vec<FactorPeriod> {
        self.periods.clone()
    }

    /// How the form rounds each part.
    pub fn rounding(&self) -> Rounding {
        self.rounding
    }

    /// Returns the period that covers `d`.
    pub fn on(&self, d: Date) -> Option<FactorPeriod> {
        self.on_ref(d).cloned()
    }

    fn on_ref(&self, d: Date) -> Option<&FactorPeriod> {
        self.periods
            .iter()
            .find(|p| !d.before(p.from) && !d.after(p.to))
    }
}

/// Builds a [`FactorSeries`].
#[derive(Clone, Debug, Default)]
pub struct FactorSeriesBuilder {
    s: FactorSeries,
}

/// Starts a series whose parts are rounded as `r` says.
pub fn new_factor_series(r: Rounding) -> FactorSeriesBuilder {
    FactorSeriesBuilder {
        s: FactorSeries {
            periods: Vec::new(),
            rounding: r,
        },
    }
}

impl FactorSeriesBuilder {
    /// Adds a daily factor for the days `from` through `to`, both included.
    /// `annual` is the rate the factor was worked out from, for the working.
    pub fn period(
        &mut self,
        from: Date,
        to: Date,
        factor: Factor,
        annual: Rate,
        source: impl Into<String>,
    ) -> &mut Self {
        self.s.periods.push(FactorPeriod {
            from,
            to,
            factor,
            annual,
            source: source.into(),
        });
        self
    }

    /// Checks the rounding is known, and that every period has a factor and
    /// a source, runs forward, and starts the day after the one before
    /// ends.
    pub fn build(&self) -> Result<FactorSeries, Error> {
        let ps = &self.s.periods;
        if self.s.rounding != Rounding::RoundToCent && self.s.rounding != Rounding::RoundToDollar {
            return Err(invalid!(
                "estimated: a factor series needs the form's rounding"
            ));
        }
        if ps.is_empty() {
            return Err(invalid!(
                "estimated: a factor series needs at least one period"
            ));
        }
        for (i, p) in ps.iter().enumerate() {
            if p.from.is_zero() || p.to.is_zero() || p.to.before(p.from) {
                return Err(invalid!(
                    "estimated: factor period {} to {} does not run forward",
                    p.from,
                    p.to
                ));
            }
            if p.factor.units == 0 {
                return Err(invalid!(
                    "estimated: the period from {} has no factor",
                    p.from
                ));
            }
            if p.annual.0 < 0 {
                return Err(invalid!("estimated: rate {} is negative", p.annual));
            }
            if p.source.is_empty() {
                return Err(invalid!(
                    "estimated: the factor from {} has no source",
                    p.from
                ));
            }
            if i > 0 && p.from != ps[i - 1].to.add_days(1) {
                return Err(invalid!(
                    "estimated: the factor from {} does not follow the period ending {}",
                    p.from,
                    ps[i - 1].to
                ));
            }
        }
        Ok(self.s.clone())
    }
}

/// Charges `principal` at a printed daily factor for the days after `from`
/// up to and including `to`, one part for each period the days fall in.
///
/// Each part is principal × days × factor, rounded as the series says. This
/// is REV-1630 lines 14a to 15c: the days in each calendar year times the
/// factor REV-1611 prints for that year, in whole dollars.
///
/// The returned date, `unrated`, is the first day the series has no factor
/// for, or the zero Date. The parts stop the day before it, and the caller
/// must report the gap.
pub fn daily_factor(
    principal: USCents,
    from: Date,
    to: Date,
    s: &FactorSeries,
) -> Result<(Vec<PenaltyPart>, Date), Error> {
    let mut parts = Vec::new();
    let mut cur = from;
    while cur.before(to) {
        let day = cur.add_days(1);
        let Some(p) = s.on_ref(day) else {
            return Ok((parts, day));
        };
        let end = earlier(to, p.to);
        let days = end.days_since(cur);
        parts.push(
            new_penalty_part(PartKind::Interest, principal, p.annual)
                .span(cur, end)
                .days(days)
                .charge(p.factor.of(principal, days, s.rounding))
                .source(p.source.clone())
                .build()?,
        );
        cur = end;
    }
    Ok((parts, Date::default()))
}

/// Charges `principal` once at a factor a form prints for a whole span, as
/// Maryland's Form 502UP prints one for each span between due dates.
/// `annual` is the rate the factor stands for, for the working.
pub fn span_factor(
    principal: USCents,
    from: Date,
    to: Date,
    f: &Factor,
    annual: Rate,
    r: Rounding,
    source: impl Into<String>,
) -> Result<PenaltyPart, Error> {
    if r != Rounding::RoundToCent && r != Rounding::RoundToDollar {
        return Err(invalid!(
            "estimated: a factor's charge needs the form's rounding"
        ));
    }
    if f.units == 0 {
        return Err(invalid!("estimated: no factor given"));
    }
    new_penalty_part(PartKind::Interest, principal, annual)
        .span(from, to)
        .days(to.days_since(from))
        .charge(f.of(principal, 1, r))
        .source(source)
        .build()
}
