//! The Go package's export_test.go exposes a year's holiday calendar and
//! rate series to its external tests, which check them against the
//! statutes. Here those tests sit inside the crate instead.

#![allow(clippy::unwrap_used)]

use estimated::{Calendar, Date, Month, RateSeries, Weekday};
use money::percent;

use crate::estimated::estimated_year;

/// The year's holiday calendar.
fn calendar(year: i64) -> Calendar {
    estimated_year(year).unwrap().unwrap().calendar
}

/// The year's rate series.
fn rates(year: i64) -> RateSeries {
    estimated_year(year).unwrap().unwrap().rates
}

fn day(y: i64, m: i64, d: i64) -> Date {
    estimated::must_date(y, Month(m), d)
}

/// The nth weekday of a month, or the last for n = -1.
fn nth(year: i64, m: i64, wd: Weekday, n: i64) -> Date {
    if n < 0 {
        let mut d = day(year, m + 1, 1).add_days(-1);
        while d.weekday() != wd {
            d = d.add_days(-1);
        }
        return d;
    }
    let mut d = day(year, m, 1);
    while d.weekday() != wd {
        d = d.add_days(1);
    }
    d.add_days(7 * (n - 1))
}

/// The 2026 due dates the 1-ES prints are each a business day.
#[test]
fn estimated_due_dates_are_business_days() {
    let cal = calendar(2026);
    for d in [
        day(2026, 4, 15),
        day(2026, 6, 15),
        day(2026, 9, 15),
        day(2027, 1, 15),
    ] {
        assert!(cal.is_business_day(d), "{d} is not a business day");
    }
}

/// The calendar is § 995.20's list from January 1, 2026 to the April 15,
/// 2027 stop, each day worked out here from the statute's own words, and it
/// moves a date on a weekend or one of those holidays to the next business
/// day.
#[test]
fn estimated_calendar_is_the_statutes_holidays() {
    let mut want = Vec::new();
    for y in [2026, 2027] {
        want.extend([
            day(y, 1, 1),
            nth(y, 1, Weekday::MONDAY, 3),
            nth(y, 2, Weekday::MONDAY, 3),
            nth(y, 5, Weekday::MONDAY, -1),
            day(y, 6, 19),
            day(y, 7, 4),
            nth(y, 8, Weekday::TUESDAY, 2), // § 5.02(12s)
            nth(y, 9, Weekday::MONDAY, 1),
            nth(y, 10, Weekday::MONDAY, 2),
            nth(y, 11, Weekday::MONDAY, 1).add_days(1), // § 5.02(5)
            day(y, 11, 11),
            nth(y, 11, Weekday::THURSDAY, 4),
            day(y, 12, 25),
        ]);
    }
    let mut in_span = Vec::new();
    for d in want {
        if !d.after(day(2027, 4, 15)) {
            assert_ne!(d.weekday(), Weekday::SUNDAY, "{d} is a Sunday");
            in_span.push(d);
        }
    }
    let got = calendar(2026).holidays();
    assert_eq!(got.len(), in_span.len());
    for (i, h) in got.iter().enumerate() {
        assert_eq!(h.on(), in_span[i], "holiday {} ({})", i + 1, h.name());
    }

    let cal = calendar(2026);
    for (inn, want) in [
        (day(2027, 1, 31), day(2027, 2, 1)),
        (day(2026, 7, 4), day(2026, 7, 6)),
        (day(2026, 9, 7), day(2026, 9, 8)),
        (day(2026, 11, 3), day(2026, 11, 4)),
        (day(2026, 11, 26), day(2026, 11, 27)),
        (day(2026, 12, 25), day(2026, 12, 28)),
        (day(2027, 1, 15), day(2027, 1, 15)),
    ] {
        assert_eq!(cal.next_business_day(inn), want, "{inn}");
    }
}

/// § 71.84(1)'s 12% runs without a break from the first day interest can
/// accrue to the stop, so no day is unrated.
#[test]
fn estimated_rate_is_statutory_to_the_stop() {
    let ps = rates(2026).periods();
    assert!(
        ps.len() == 1
            && ps[0].from() == day(2026, 4, 16)
            && ps[0].to() == day(2027, 4, 15)
            && ps[0].rate() == percent(12)
    );
}
