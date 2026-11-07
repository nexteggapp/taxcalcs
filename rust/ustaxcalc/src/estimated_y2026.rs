//! Estimated tax for tax year 2026.
//!
//! Every figure here is checked by the Go package's estimated_test.go
//! against an extract in its testdata, listed in testdata/SOURCES.md:
//!
//!   - the $1,000 threshold, the 90%, 100% and 110% tests, the $150,000 and
//!     $75,000 steps and the payment dates: the 2026 Form 1040-ES;
//!   - the January 31 file-and-pay date, the April 15 stop and the Schedule
//!     AI periods and percentages: the 2025 Form 2210 and its instructions,
//!     whose rules are unchanged for 2026 and whose 2026 edition is not yet
//!     published;
//!   - the legal holidays: Publication 509 (2026);
//!   - the underpayment rates: Rev. Rul. 2026-5, 2026-10 and 2026-15.
//!
//! The rate for January 1 to April 15, 2027 is set by a revenue ruling the
//! IRS publishes in November 2026, and it is not out. The series ends on
//! December 31, 2026 and the penalty reports the gap rather than assume a
//! rate.
//!
//! Publication 509 (2027) is not out either, so 2027 has no holidays here.
//! The 2027 due dates the 2026 Form 1040-ES prints, January 15 and February
//! 1, are weekdays. The April 15, 2027 stop is the 2025 instructions' April
//! 15 a year on; the penalty worksheet does not move it for a weekend, so no
//! holiday list is needed for it.

use estimated::{Date, Month};

use crate::DOLLAR;
use crate::estimated::EstimatedYear;

/// A shorthand for the year modules.
fn date(y: i64, m: Month, d: i64) -> Date {
    estimated::must_date(y, m, d)
}

/// The 2026 figures.
pub(crate) fn year_2026() -> Result<EstimatedYear, estimated::Error> {
    let calendar = estimated::new_calendar("Publication 509 (2026), Legal holidays")
        .holiday(date(2026, Month::JANUARY, 1), "New Year's Day")
        .holiday(
            date(2026, Month::JANUARY, 19),
            "Birthday of Martin Luther King, Jr.",
        )
        .holiday(date(2026, Month::FEBRUARY, 16), "Washington's Birthday")
        .holiday(
            date(2026, Month::APRIL, 16),
            "District of Columbia Emancipation Day",
        )
        .holiday(date(2026, Month::MAY, 25), "Memorial Day")
        .holiday(
            date(2026, Month::JUNE, 19),
            "Juneteenth National Independence Day",
        )
        .holiday(date(2026, Month::JULY, 3), "Independence Day (observed)")
        .holiday(date(2026, Month::SEPTEMBER, 7), "Labor Day")
        .holiday(
            date(2026, Month::OCTOBER, 12),
            "Indigenous Peoples' Day (Columbus Day)",
        )
        .holiday(date(2026, Month::NOVEMBER, 11), "Veterans Day")
        .holiday(date(2026, Month::NOVEMBER, 26), "Thanksgiving Day")
        .holiday(date(2026, Month::DECEMBER, 25), "Christmas Day")
        .build()?;

    // § 6621(b): one rate per calendar quarter. The series starts with the
    // quarter the first installment falls in; the first quarter's rate never
    // applies to a 2026 underpayment.
    let rates = estimated::new_rate_series()
        .period(
            date(2026, Month::APRIL, 1),
            date(2026, Month::JUNE, 30),
            money::percent(6),
            "Rev. Rul. 2026-5, 2026-8 I.R.B. 542",
        )
        .period(
            date(2026, Month::JULY, 1),
            date(2026, Month::SEPTEMBER, 30),
            money::percent(7),
            "Rev. Rul. 2026-10, 2026-22 I.R.B. 1515",
        )
        .period(
            date(2026, Month::OCTOBER, 1),
            date(2026, Month::DECEMBER, 31),
            money::percent(7),
            "Rev. Rul. 2026-15, 2026-36 I.R.B. 216",
        )
        .build()?;

    Ok(EstimatedYear {
        year: 2026,
        threshold: DOLLAR * 1_000,
        calendar,
        file_and_pay_by: date(2027, Month::JANUARY, 31),
        stop: date(2027, Month::APRIL, 15),
        period_ends: vec![
            date(2026, Month::MARCH, 31),
            date(2026, Month::MAY, 31),
            date(2026, Month::AUGUST, 31),
            date(2026, Month::DECEMBER, 31),
        ],
        rates,
    })
}
