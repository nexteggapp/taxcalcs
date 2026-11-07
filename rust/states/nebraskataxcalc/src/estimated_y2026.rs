//! Estimated tax for tax year 2026.
//!
//! Every figure here is checked by the Go package's estimated_test.go
//! against an extract in its testdata/, listed in testdata/SOURCES.md:
//!
//!   - the $500 threshold, the 90%, 100% and 110% tests, the $150,000 and
//!     $75,000 steps, the 25% columns, the dates, the January 31 rule, the
//!     April 15 stop and the 8% rate: the 2026 Form 2210N and its
//!     instructions;
//!   - the dates, the weekend and holiday rule and the annualized method's
//!     federal pattern: the 2026 Form 1040N-ES and Form 2210N;
//!   - the rate's source: REG-20-007.03 ties it to § 45-104.02, and Revenue
//!     Ruling 99-26-2 sets 8% from January 1, 2025 and for 2027 and 2028.
//!
//! No Nebraska holiday list is checked in. The four 2026 due dates are
//! weekdays, and the test checks the computed dates against the printed
//! ones.

use estimated::{Error, Month};
use money::{DOLLAR, Rate, percent};

use crate::estimated::{EstimatedYear, date};

/// Nebraska's figures for 2026.
pub(crate) fn year_2026() -> Result<EstimatedYear, Error> {
    let calendar =
        estimated::new_calendar("weekends only; no Nebraska holiday list is checked in").build()?;
    let rates = estimated::new_rate_series()
        .period(
            date(2026, Month::JANUARY, 1),
            date(2026, Month::DECEMBER, 31),
            percent(8),
            "Revenue Ruling 99-26-2: 8% from January 1, 2025; Form 2210N (2026), line 22a",
        )
        .period(
            date(2027, Month::JANUARY, 1),
            date(2028, Month::DECEMBER, 31),
            percent(8),
            "Revenue Ruling 99-26-2: 8% from January 1, 2027 through December 31, 2028",
        )
        .build()?;

    Ok(EstimatedYear {
        year: 2026,
        threshold: DOLLAR * 500,
        this_year_share: percent(90),
        prior_year_share: percent(100),
        prior_year_high: percent(110),
        step: DOLLAR * 150_000,
        step_separate: DOLLAR * 75_000,
        shares: vec![percent(25), percent(25), percent(25), percent(25)],
        annualised: vec![Rate(2250), Rate(4500), Rate(6750), Rate(9000)],
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
        file_and_pay_by: date(2027, Month::JANUARY, 31),
        deemed_paid: date(2027, Month::JANUARY, 15),
        stop: date(2027, Month::APRIL, 15),
        rates,
    })
}
