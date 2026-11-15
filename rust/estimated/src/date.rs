//! Calendar days, months, weekdays and holiday calendars.

use std::fmt;

use crate::{Error, invalid};

/// A month of the year, as Go's `time.Month`: January is 1. A value outside
/// 1 to 12 is allowed, as in Go, and arithmetic on dates carries it into the
/// year.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub struct Month(pub i64);

impl Month {
    /// January.
    pub const JANUARY: Month = Month(1);
    /// February.
    pub const FEBRUARY: Month = Month(2);
    /// March.
    pub const MARCH: Month = Month(3);
    /// April.
    pub const APRIL: Month = Month(4);
    /// May.
    pub const MAY: Month = Month(5);
    /// June.
    pub const JUNE: Month = Month(6);
    /// July.
    pub const JULY: Month = Month(7);
    /// August.
    pub const AUGUST: Month = Month(8);
    /// September.
    pub const SEPTEMBER: Month = Month(9);
    /// October.
    pub const OCTOBER: Month = Month(10);
    /// November.
    pub const NOVEMBER: Month = Month(11);
    /// December.
    pub const DECEMBER: Month = Month(12);
}

const MONTHS: [&str; 12] = [
    "January",
    "February",
    "March",
    "April",
    "May",
    "June",
    "July",
    "August",
    "September",
    "October",
    "November",
    "December",
];

/// The English name of the month, as Go's `Month.String`: "April", or
/// "%!Month(13)" for a value outside 1 to 12.
impl fmt::Display for Month {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self.0 {
            1..=12 => f.write_str(MONTHS[(self.0 - 1) as usize]),
            n => write!(f, "%!Month({n})"),
        }
    }
}

/// A day of the week, as Go's `time.Weekday`: Sunday is 0.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub struct Weekday(pub i64);

impl Weekday {
    /// Sunday.
    pub const SUNDAY: Weekday = Weekday(0);
    /// Monday.
    pub const MONDAY: Weekday = Weekday(1);
    /// Tuesday.
    pub const TUESDAY: Weekday = Weekday(2);
    /// Wednesday.
    pub const WEDNESDAY: Weekday = Weekday(3);
    /// Thursday.
    pub const THURSDAY: Weekday = Weekday(4);
    /// Friday.
    pub const FRIDAY: Weekday = Weekday(5);
    /// Saturday.
    pub const SATURDAY: Weekday = Weekday(6);
}

const DAYS: [&str; 7] = [
    "Sunday",
    "Monday",
    "Tuesday",
    "Wednesday",
    "Thursday",
    "Friday",
    "Saturday",
];

/// The English name of the day, as Go's `Weekday.String`.
impl fmt::Display for Weekday {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self.0 {
            0..=6 => f.write_str(DAYS[self.0 as usize]),
            n => write!(f, "%!Weekday({n})"),
        }
    }
}

/// Days from 1970-01-01 to the given proleptic Gregorian day, `m` in 1..=12.
fn days_from_civil(y: i64, m: i64, d: i64) -> i64 {
    let y = if m <= 2 { y - 1 } else { y };
    let era = y.div_euclid(400);
    let yoe = y - era * 400;
    let mp = (m + 9) % 12;
    let doy = (153 * mp + 2) / 5 + d - 1;
    let doe = yoe * 365 + yoe / 4 - yoe / 100 + doy;
    era * 146_097 + doe - 719_468
}

/// The year, month and day of a count of days from 1970-01-01.
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

/// The day Go's `time.Date(year, month, day, ...)` gives, as days from
/// 1970-01-01: a month or day out of range is carried, so February 30th is
/// March 2nd.
fn normalized(year: i64, month: i64, day: i64) -> i64 {
    let m0 = month - 1;
    let y = year + m0.div_euclid(12);
    let m = m0.rem_euclid(12) + 1;
    days_from_civil(y, m, 1) + day - 1
}

/// A calendar day, with no time of day and no zone.
///
/// Tax deadlines are days. Date holds only the year, month and day, so `==`
/// means the same day.
///
/// The zero Date, [`Date::default`], is "no date", which [`Date::is_zero`]
/// reports.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Hash)]
pub struct Date {
    pub(crate) year: i64,
    pub(crate) month: Month,
    pub(crate) day: i64,
}

/// Returns the date, or an error if the day does not exist. February 30th is
/// an error rather than March 2nd.
pub fn new_date(year: i64, month: Month, day: i64) -> Result<Date, Error> {
    let d = date_of(year, month, day);
    if d.year != year || d.month != month || d.day != day {
        return Err(invalid!(
            "estimated: {:04}-{:02}-{:02} is not a date",
            year,
            month.0,
            day
        ));
    }
    Ok(d)
}

/// [`new_date`] for a date written in source, which panics on a day that
/// does not exist.
///
/// # Panics
///
/// When the day does not exist, as Go's `MustDate` does.
pub fn must_date(year: i64, month: Month, day: i64) -> Date {
    match new_date(year, month, day) {
        Ok(d) => d,
        Err(e) => panic!("{e}"),
    }
}

/// The calendar day Go's `DateOf(time.Date(year, month, day, ...))` gives: a
/// month or day out of range is carried into the next, so February 30th is
/// March 2nd. It stands in for Go's `DateOf`, which takes a `time.Time`.
pub fn date_of(year: i64, month: Month, day: i64) -> Date {
    Date::from_days(normalized(year, month.0, day))
}

pub(crate) fn later(a: Date, b: Date) -> Date {
    if a.after(b) { a } else { b }
}

pub(crate) fn earlier(a: Date, b: Date) -> Date {
    if a.before(b) { a } else { b }
}

/// 365, or 366 in a leap year. Form 2210 and FTB 5805 both divide a year's
/// interest by the days in that year.
pub(crate) fn days_in_year(year: i64) -> i64 {
    Date {
        year,
        month: Month::DECEMBER,
        day: 31,
    }
    .days_since(Date {
        year: year - 1,
        month: Month::DECEMBER,
        day: 31,
    })
}

impl Date {
    /// The year.
    pub fn year(self) -> i64 {
        self.year
    }

    /// The month.
    pub fn month(self) -> Month {
        self.month
    }

    /// The day of the month.
    pub fn day(self) -> i64 {
        self.day
    }

    /// Whether this is the zero Date, "no date".
    pub fn is_zero(self) -> bool {
        self == Date::default()
    }

    /// The day of the week.
    pub fn weekday(self) -> Weekday {
        Weekday((self.days() + 4).rem_euclid(7))
    }

    /// The day as a count of days from 1970-01-01, which stands in for Go's
    /// `Time`, midnight UTC at the start of the day. A month or day out of
    /// range is carried, as `time.Date` carries it.
    pub fn days(self) -> i64 {
        normalized(self.year, self.month.0, self.day)
    }

    /// The date `days` days after 1970-01-01.
    pub fn from_days(days: i64) -> Date {
        let (year, month, day) = civil_from_days(days);
        Date {
            year,
            month: Month(month),
            day,
        }
    }

    /// Returns -1, 0 or +1 as `self` is before, the same as, or after `o`.
    pub fn compare(self, o: Date) -> i64 {
        match self.days().cmp(&o.days()) {
            std::cmp::Ordering::Less => -1,
            std::cmp::Ordering::Equal => 0,
            std::cmp::Ordering::Greater => 1,
        }
    }

    /// Whether `self` is before `o`.
    pub fn before(self, o: Date) -> bool {
        self.compare(o) < 0
    }

    /// Whether `self` is after `o`.
    pub fn after(self, o: Date) -> bool {
        self.compare(o) > 0
    }

    /// Returns the date `n` days later, or earlier for a negative `n`.
    pub fn add_days(self, n: i64) -> Date {
        Date::from_days(self.days() + n)
    }

    /// Returns the same day `n` months later. A day the later month does not
    /// have is clamped to its last day, so January 31st plus one month is
    /// February 28th rather than March 3rd.
    pub fn add_months(self, n: i64) -> Date {
        let first = Date::from_days(normalized(self.year, self.month.0 + n, 1));
        let last = Date::from_days(normalized(first.year, first.month.0 + 1, 0)).day;
        Date {
            year: first.year,
            month: first.month,
            day: self.day.min(last),
        }
    }

    /// Returns the number of days from `o` to `self`: positive when `self`
    /// is later. April 15th to June 10th is 56, which is how Form 2210 and
    /// FTB 5805 count. As in Go, whose durations hold about 292 years, the
    /// count saturates at 106,751 days either way.
    pub fn days_since(self, o: Date) -> i64 {
        (self.days() - o.days()).clamp(-106_751, 106_751)
    }

    /// Renders the date the way a form prints it: April 15, 2026.
    pub fn long(self) -> String {
        if self.is_zero() {
            return "no date".to_string();
        }
        format!("{} {}, {}", self.month, self.day, self.year)
    }

    /// The date in a Go `time.Format` layout, standing in for Go's
    /// `d.Time().Format(layout)`. It knows the date elements: "January",
    /// "Jan", "Monday", "Mon", "2006", "06", "01", "1", "02", "_2" and "2";
    /// anything else is copied as it is.
    pub fn format(self, layout: &str) -> String {
        let (y, m, d) = civil_from_days(self.days());
        let wd = self.weekday();
        let month = Month(m).to_string();
        let day = wd.to_string();
        let mut out = String::new();
        let mut rest = layout;
        while let Some(c) = rest.chars().next() {
            let tokens: [(&str, String); 11] = [
                ("January", month.clone()),
                ("Jan", month[..3].to_string()),
                ("Monday", day.clone()),
                ("Mon", day[..3].to_string()),
                ("2006", format!("{y:04}")),
                ("01", format!("{m:02}")),
                ("02", format!("{d:02}")),
                ("06", format!("{:02}", y.rem_euclid(100))),
                ("_2", format!("{d:>2}")),
                ("2", d.to_string()),
                ("1", m.to_string()),
            ];
            if let Some((t, v)) = tokens.iter().find(|(t, _)| rest.starts_with(t)) {
                // "15" is Go's hour, not the month 1 then the day 5.
                if !(*t == "1" && rest.starts_with("15")) {
                    out.push_str(v);
                    rest = &rest[t.len()..];
                    continue;
                }
            }
            out.push(c);
            rest = &rest[c.len_utf8()..];
        }
        out
    }
}

/// Renders the date as 2026-04-15, or "no date" for the zero Date.
impl fmt::Display for Date {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        if self.is_zero() {
            return f.write_str("no date");
        }
        write!(f, "{:04}-{:02}-{:02}", self.year, self.month.0, self.day)
    }
}

/// A day a tax authority treats as a legal holiday, and its name.
#[derive(Clone, Debug, Default, PartialEq, Eq, Hash)]
pub struct Holiday {
    on: Date,
    name: String,
}

impl Holiday {
    /// The day.
    pub fn on(&self) -> Date {
        self.on
    }

    /// The holiday's name.
    pub fn name(&self) -> String {
        self.name.clone()
    }
}

/// The set of legal holidays a jurisdiction moves a due date for. Saturdays
/// and Sundays are always skipped and are not listed.
///
/// Which days are holidays differs by jurisdiction. Federal deadlines move
/// for any legal holiday in the District of Columbia, which includes DC
/// Emancipation Day; Oregon's and Rhode Island's move for their own. Each
/// jurisdiction builds the calendar its own documents publish.
#[derive(Clone, Debug, Default, PartialEq, Eq, Hash)]
pub struct Calendar {
    holidays: Vec<Holiday>,
    source: String,
}

impl Calendar {
    /// Returns the holidays, earliest first.
    pub fn holidays(&self) -> Vec<Holiday> {
        self.holidays.clone()
    }

    /// Cites the document that lists the holidays.
    pub fn source(&self) -> String {
        self.source.clone()
    }

    /// The name of the holiday on `d`, if `d` is a listed holiday.
    pub fn is_holiday(&self, d: Date) -> Option<String> {
        self.holidays
            .iter()
            .find(|h| h.on == d)
            .map(|h| h.name.clone())
    }

    /// Reports whether `d` is neither a weekend nor a holiday.
    pub fn is_business_day(&self, d: Date) -> bool {
        let wd = d.weekday();
        if wd == Weekday::SATURDAY || wd == Weekday::SUNDAY {
            return false;
        }
        self.is_holiday(d).is_none()
    }

    /// Returns `d` if it is a business day, and otherwise the next day that
    /// is. This is 26 U.S.C. § 7503 and the rule every state here follows.
    pub fn next_business_day(&self, mut d: Date) -> Date {
        while !self.is_business_day(d) {
            d = d.add_days(1);
        }
        d
    }
}

/// Builds a [`Calendar`].
#[derive(Clone, Debug, Default)]
pub struct CalendarBuilder {
    c: Calendar,
}

/// Starts a calendar whose holidays come from `source`.
pub fn new_calendar(source: impl Into<String>) -> CalendarBuilder {
    CalendarBuilder {
        c: Calendar {
            holidays: Vec::new(),
            source: source.into(),
        },
    }
}

impl CalendarBuilder {
    /// Adds a holiday.
    pub fn holiday(&mut self, on: Date, name: impl Into<String>) -> &mut Self {
        self.c.holidays.push(Holiday {
            on,
            name: name.into(),
        });
        self
    }

    /// Checks the calendar names its source and has no undated or repeated
    /// holiday.
    pub fn build(&self) -> Result<Calendar, Error> {
        if self.c.source.is_empty() {
            return Err(invalid!(
                "estimated: a calendar needs the source that lists its holidays"
            ));
        }
        let mut hs = self.c.holidays.clone();
        hs.sort_by_key(|h| h.on.days());
        for (i, h) in hs.iter().enumerate() {
            if h.on.is_zero() || h.name.is_empty() {
                return Err(invalid!("estimated: a holiday needs a date and a name"));
            }
            if i > 0 && hs[i - 1].on == h.on {
                return Err(invalid!("estimated: {} is listed twice", h.on));
            }
        }
        Ok(Calendar {
            holidays: hs,
            source: self.c.source.clone(),
        })
    }
}
