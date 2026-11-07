//! Estimated tax for tax year 2026.
//!
//! Every figure here is checked by the Go package's estimated_test.go
//! against an extract in its testdata/, listed in testdata/SOURCES.md:
//!
//!   - the $400 threshold, the filing thresholds, the due dates and the
//!     February 15 rule: the 2026 Form NJ-1040-ES and N.J.S.A. 54A:8-4;
//!   - the 80% and 100% tests, the quarters, the four exceptions and their
//!     percentages, the periods, the April 15 stop and the month-or-part
//!     rule: Form NJ-2210 for 2025, whose 2026 edition is not published;
//!   - the 10.00% rate for 2026: TB-21(R).
//!
//! The NJ-2210 rate schedule runs a year's rate to January 15 of the next
//! year, so 10.00% covers the months that start through January 15, 2027.
//!
//! No New Jersey holiday list is checked in. The four 2026 due dates are
//! weekdays, and the test checks the computed dates against the printed
//! ones.

use estimated::{Error, Month};
use money::{DOLLAR, percent};

use crate::estimated::{EstimatedYear, date};

pub(crate) fn year_2026() -> Result<EstimatedYear, Error> {
    let calendar =
        estimated::new_calendar("weekends only; no New Jersey holiday list is checked in")
            .build()?;
    Ok(EstimatedYear {
        year: 2026,
        threshold: DOLLAR * 400,
        filing_threshold: DOLLAR * 20_000,
        filing_threshold_single: DOLLAR * 10_000,
        this_year_share: percent(80),
        prior_year_share: percent(100),
        annualised_share: percent(80),
        income_share: percent(90),
        shares: vec![percent(25), percent(25), percent(25), percent(25)],
        dues: vec![
            date(2026, Month::APRIL, 15),
            date(2026, Month::JUNE, 15),
            date(2026, Month::SEPTEMBER, 15),
            date(2027, Month::JANUARY, 15),
        ],
        calendar,
        period_ends: vec![
            date(2026, Month::MARCH, 31),
            date(2026, Month::MAY, 31),
            date(2026, Month::AUGUST, 31),
            date(2026, Month::DECEMBER, 31),
        ],
        file_and_pay_by: date(2027, Month::FEBRUARY, 15),
        stop: date(2027, Month::APRIL, 15),
        rate: percent(10),
        rate_to: date(2027, Month::JANUARY, 15),
        rate_source: "TB-21(R): 10.00% for January 1 – December 31, 2026; Form NJ-2210 (2025), Option 2 schedule",
        unrated: "New Jersey sets each year's rate from prime on December 1 of the year before",
    })
}
