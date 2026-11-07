//! How often a source pays, ported from Go's `taxyear/model/cadence.go`.

use crate::internal::civil::{self, Date};

/// How often a source pays, read from the dates it paid so far: a number of
/// months, or for gaps under 25 days a number of days.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct Cadence {
    months: i64,
    days: i64,
}

/// Returns the median gap between dates, so monthly, quarterly and
/// semiannual payers each fall out of their own evidence. With one date, or
/// none, it is annual. A gap of 25 days or more is counted in months.
pub fn cadence_of(dates: &[Date]) -> Cadence {
    let mut ds = dates.to_vec();
    ds.sort();
    let mut gaps = Vec::new();
    for i in 1..ds.len() {
        let g = ds[i].days_from(ds[i - 1]);
        if g > 0 {
            gaps.push(g);
        }
    }
    if gaps.is_empty() {
        return Cadence {
            months: 12,
            days: 0,
        };
    }
    gaps.sort();
    let n = gaps.len();
    let mut median = gaps[n / 2];
    if n % 2 == 0 {
        median = (gaps[n / 2 - 1] + gaps[n / 2]) / 2;
    }
    if median < 25 {
        return Cadence {
            months: 0,
            days: median,
        };
    }
    let months = (median as f64 / 30.44 + 0.5) as i64;
    Cadence {
        months: months.max(1),
        days: 0,
    }
}

impl Cadence {
    /// The cadence in months, or 0 for one counted in days.
    pub fn months(self) -> i64 {
        self.months
    }

    /// Returns the date one step after d. A month step keeps the day of the
    /// month, a month's last day staying its last day, and clamps to a
    /// shorter month's end.
    pub fn next(self, d: Date) -> Date {
        if self.months == 0 {
            return d.add_days(self.days);
        }
        let m = d.month() - 1 + self.months;
        let (year, month) = (d.year() + m / 12, m % 12 + 1);
        let end = civil::end_of_month(year, month);
        if d == civil::end_of_month(d.year(), d.month()) || d.day() >= end.day() {
            return end;
        }
        civil::must(year, month, d.day())
    }

    /// Returns the dates after the last one, step by step, up to and
    /// including until.
    pub fn through(self, last: Date, until: Date) -> Vec<Date> {
        let mut out = Vec::new();
        let mut d = self.next(last);
        while !d.after(until) {
            out.push(d);
            d = self.next(d);
        }
        out
    }
}
