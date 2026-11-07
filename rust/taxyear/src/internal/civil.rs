//! A calendar date with no time zone, for the dates a tax year's items fall
//! on. Ported from the Go package `taxyear/internal/civil`.
//!
//! Go keeps a `time.Time` at midnight UTC; this keeps the count of days from
//! January 1 of year 1, which is Go's zero time, so the zero [`Date`] is the
//! same day in both and compares as the earliest.

use std::fmt;

use proto::commonv1;

/// A day of the week, as Go's `time.Weekday` numbers them.
#[derive(Clone, Copy, Debug, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub enum Weekday {
    /// Sunday, 0.
    Sunday = 0,
    /// Monday, 1.
    Monday = 1,
    /// Tuesday, 2.
    Tuesday = 2,
    /// Wednesday, 3.
    Wednesday = 3,
    /// Thursday, 4.
    Thursday = 4,
    /// Friday, 5.
    Friday = 5,
    /// Saturday, 6.
    Saturday = 6,
}

/// The error [`new`] gives for a day that does not exist.
#[derive(Clone, Debug, PartialEq, Eq, thiserror::Error)]
#[error("civil: {year:04}-{month:02}-{day:02} is not a day")]
pub struct Error {
    /// The year asked for.
    pub year: i64,
    /// The month asked for.
    pub month: i64,
    /// The day asked for.
    pub day: i64,
}

/// A calendar day. The zero Date is no date.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub struct Date {
    /// Days from January 1 of year 1, proleptic Gregorian.
    days: i64,
}

/// Days from 1970-01-01 to 0001-01-01.
const UNIX_TO_YEAR_ONE: i64 = 719_162;

/// The days from 1970-01-01 of a proleptic Gregorian date, for any year.
fn days_from_civil(y: i64, m: i64, d: i64) -> i64 {
    let y = if m <= 2 { y - 1 } else { y };
    let era = y.div_euclid(400);
    let yoe = y - era * 400;
    let mp = (m + 9) % 12;
    let doy = (153 * mp + 2) / 5 + d - 1;
    let doe = yoe * 365 + yoe / 4 - yoe / 100 + doy;
    era * 146_097 + doe - 719_468
}

/// The date of a count of days from 1970-01-01.
fn civil_from_days(z: i64) -> (i64, i64, i64) {
    let z = z + 719_468;
    let era = z.div_euclid(146_097);
    let doe = z - era * 146_097;
    let yoe = (doe - doe / 1460 + doe / 36_524 - doe / 146_096) / 365;
    let y = yoe + era * 400;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let d = doy - (153 * mp + 2) / 5 + 1;
    let m = if mp < 10 { mp + 3 } else { mp - 9 };
    (if m <= 2 { y + 1 } else { y }, m, d)
}

/// The date Go's `time.Date(year, month, day, ...)` gives, normalising a
/// month or day out of range as Go does.
fn normalised(year: i64, month: i64, day: i64) -> Date {
    let m0 = month - 1;
    let (y, m) = (year + m0.div_euclid(12), m0.rem_euclid(12) + 1);
    Date {
        days: days_from_civil(y, m, 1) + (day - 1) + UNIX_TO_YEAR_ONE,
    }
}

/// Returns the date, or an error when the day does not exist.
pub fn new(year: i64, month: i64, day: i64) -> Result<Date, Error> {
    let d = normalised(year, month, day);
    if d.year() != year || d.month() != month || d.day() != day {
        return Err(Error { year, month, day });
    }
    Ok(d)
}

/// [`new`] for a date known to exist. It panics otherwise, as Go's does.
pub fn must(year: i64, month: i64, day: i64) -> Date {
    match new(year, month, day) {
        Ok(d) => d,
        Err(e) => panic!("{e}"),
    }
}

/// Converts a proto date. A nil date is the zero Date and no error; a
/// partial or impossible one is an error.
pub fn from_proto(d: Option<&commonv1::Date>) -> Result<Date, Error> {
    match d {
        None => Ok(Date::default()),
        Some(d) => new(i64::from(d.year), i64::from(d.month), i64::from(d.day)),
    }
}

/// [`from_proto`] for an optional message field.
pub fn from_field<P: buffa::ProtoBox<commonv1::Date>>(
    d: &buffa::MessageField<commonv1::Date, P>,
) -> Result<Date, Error> {
    from_proto(d.as_option())
}

impl Date {
    fn parts(self) -> (i64, i64, i64) {
        civil_from_days(self.days - UNIX_TO_YEAR_ONE)
    }

    /// Returns the date as a proto, or nil for the zero Date.
    pub fn proto(self) -> Option<commonv1::Date> {
        if self.is_zero() {
            return None;
        }
        let (y, m, d) = self.parts();
        Some(commonv1::Date {
            year: y as i32,
            month: m as i32,
            day: d as i32,
            ..Default::default()
        })
    }

    /// [`Date::proto`] as an optional message field.
    pub fn field<P: buffa::ProtoBox<commonv1::Date>>(
        self,
    ) -> buffa::MessageField<commonv1::Date, P> {
        self.proto().into()
    }

    /// Reports whether d is no date.
    pub fn is_zero(self) -> bool {
        self.days == 0
    }

    /// The date's year.
    pub fn year(self) -> i64 {
        self.parts().0
    }

    /// The date's month, 1 to 12.
    pub fn month(self) -> i64 {
        self.parts().1
    }

    /// The date's day of the month.
    pub fn day(self) -> i64 {
        self.parts().2
    }

    /// The date's day of the week. January 1 of year 1 was a Monday.
    pub fn weekday(self) -> Weekday {
        match (self.days + 1).rem_euclid(7) {
            0 => Weekday::Sunday,
            1 => Weekday::Monday,
            2 => Weekday::Tuesday,
            3 => Weekday::Wednesday,
            4 => Weekday::Thursday,
            5 => Weekday::Friday,
            _ => Weekday::Saturday,
        }
    }

    /// Returns the date n days later, or earlier for a negative n.
    pub fn add_days(self, n: i64) -> Date {
        Date {
            days: self.days + n,
        }
    }

    /// Returns -1, 0 or 1 as d is before, the same as or after o.
    pub fn compare(self, o: Date) -> i64 {
        match self.days.cmp(&o.days) {
            std::cmp::Ordering::Less => -1,
            std::cmp::Ordering::Equal => 0,
            std::cmp::Ordering::Greater => 1,
        }
    }

    /// Reports whether d is before o.
    pub fn before(self, o: Date) -> bool {
        self.days < o.days
    }

    /// Reports whether d is after o.
    pub fn after(self, o: Date) -> bool {
        self.days > o.days
    }

    /// Returns the days from o to d: positive when d is later. Go measures
    /// it as a `time.Duration`, which saturates about 292 years out, so a
    /// span longer than that is 106,751 days either way.
    pub fn days_from(self, o: Date) -> i64 {
        const LIMIT: i64 = 106_751;
        (self.days - o.days).clamp(-LIMIT, LIMIT)
    }

    /// The date as a sentence writes it, "Aug 14, 2026".
    pub fn long(self) -> String {
        let (y, m, d) = self.parts();
        format!("{} {d}, {}", month_abbrev(m), go_year(y))
    }

    /// The date without its year, "Aug 14".
    pub fn short(self) -> String {
        let (_, m, d) = self.parts();
        format!("{} {d}", month_abbrev(m))
    }

    /// Returns d, or the Friday before when d falls on a weekend, as
    /// payroll moves a pay date. Holidays are not moved.
    pub fn business_day_on_or_before(self) -> Date {
        match self.weekday() {
            Weekday::Saturday => self.add_days(-1),
            Weekday::Sunday => self.add_days(-2),
            _ => self,
        }
    }
}

/// Go's "2006" layout element: four digits, with a sign for a year before
/// year 0.
fn go_year(y: i64) -> String {
    if y < 0 {
        format!("-{:04}", -y)
    } else {
        format!("{y:04}")
    }
}

fn month_abbrev(m: i64) -> &'static str {
    const NAMES: [&str; 12] = [
        "Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec",
    ];
    NAMES[((m - 1).rem_euclid(12)) as usize]
}

/// The ISO form, 2026-08-14, or "" for the zero Date.
impl fmt::Display for Date {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        if self.is_zero() {
            return Ok(());
        }
        let (y, m, d) = self.parts();
        write!(f, "{}-{m:02}-{d:02}", go_year(y))
    }
}

/// Returns the last day of the month of year and month.
pub fn end_of_month(year: i64, month: i64) -> Date {
    normalised(year, month + 1, 0)
}

/// Returns the later of a and b, treating the zero Date as earliest.
pub fn later(a: Date, b: Date) -> Date {
    if a.after(b) { a } else { b }
}

#[cfg(test)]
mod tests {
    use super::*;

    // Go's TestStringMatchesFormat: the hand-written form is time.Format's.
    #[test]
    fn string_matches_format() {
        for (d, want) in [
            (must(2026, 1, 5), "2026-01-05"),
            (must(2026, 12, 31), "2026-12-31"),
            (must(999, 10, 10), "0999-10-10"),
            (Date::default(), ""),
        ] {
            assert_eq!(d.to_string(), want);
        }
    }

    #[test]
    fn round_trips() {
        for y in [1, 4, 100, 400, 1900, 1999, 2000, 2024, 2026, 9999] {
            for m in 1..=12 {
                let e = end_of_month(y, m);
                assert_eq!((e.year(), e.month()), (y, m));
                assert_eq!(e.add_days(1).day(), 1);
                assert_eq!(new(y, m, e.day()), Ok(e));
                assert!(new(y, m, e.day() + 1).is_err());
            }
        }
        assert!(must(1, 1, 1).is_zero());
        assert_eq!(must(2026, 8, 14).weekday(), Weekday::Friday);
        assert_eq!(must(2026, 8, 14).long(), "Aug 14, 2026");
        assert_eq!(must(2026, 8, 14).short(), "Aug 14");
        assert_eq!(
            new(2026, 2, 29).map_err(|e| e.to_string()),
            Err("civil: 2026-02-29 is not a day".to_string())
        );
    }
}
