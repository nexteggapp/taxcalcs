//! A year's months and the figures read with them, ported from Go's
//! `taxyear/model/months.go`.

use money::{FineRate, USCents};
use proto::commonv1;

use super::{Context, scale};
use crate::internal::civil::{self, Date, Weekday};

impl Context {
    /// Returns the last day of each month of the tax year: the slots of a
    /// model whose slots are months.
    pub fn months(&self) -> Vec<Date> {
        (1..=12)
            .map(|m| civil::end_of_month(self.year, m))
            .collect()
    }

    /// The last day of the last month that has ended by as_of: a month is
    /// evidence once it has ended. It is zero before the end of January.
    pub fn evidence_through(&self) -> Date {
        let mut last = Date::default();
        for m in self.months() {
            if !m.after(self.as_of) {
                last = m;
            }
        }
        last
    }

    /// How many months have ended by as_of.
    pub fn evidence_months(&self) -> i64 {
        self.months()
            .iter()
            .filter(|m| !m.after(self.as_of))
            .count() as i64
    }
}

/// Returns the last day of the month a date falls in.
pub fn month_of(d: Date) -> Date {
    civil::end_of_month(d.year(), d.month())
}

/// Reads a percentage Quantity as a rate in millionths, or false when
/// unset.
pub fn percent(q: Option<&commonv1::Quantity>) -> (FineRate, bool) {
    let Some(q) = q else {
        return (FineRate(0), false);
    };
    let mut r = q.units.wrapping_mul(10_000);
    for _ in 0..q.scale.max(0) {
        r /= 10;
    }
    (FineRate(r), true)
}

/// Applies a rate in millionths to an amount, rounding half away from
/// zero.
pub fn of_fine(r: FineRate, a: USCents) -> USCents {
    scale(a, USCents(r.0), USCents(1_000_000))
}

/// Reads a Quantity as units and a power of ten.
pub fn decimal(q: Option<&commonv1::Quantity>) -> (i64, i32) {
    q.map(|q| (q.units, q.scale)).unwrap_or((0, 0))
}

/// When the year's W-2s and 1099s are due to arrive: January 31 of the next
/// year, moved to the next weekday. 26 U.S.C. § 6051(a) and § 7503.
pub fn forms_due(year: i64) -> Date {
    let d = civil::must(year + 1, 1, 31);
    match d.weekday() {
        Weekday::Saturday => d.add_days(2),
        Weekday::Sunday => d.add_days(1),
        _ => d,
    }
}
