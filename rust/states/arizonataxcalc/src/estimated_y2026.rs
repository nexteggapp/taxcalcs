//! Estimated tax for tax year 2026.
//!
//! Every figure here is checked by the Go package's estimated_test.go
//! against an extract in its testdata/, listed in testdata/SOURCES.md:
//!
//!   - the $75,000 and $150,000 income thresholds, the 90% and 100% tests,
//!     the due dates and the February 1, 2027 file-and-pay date: the 2026
//!     Form 140ES and its instructions, and A.R.S. § 43-581(A);
//!   - the $1,000 Arizona threshold: A.R.S. § 43-581(E)(2) and Form 221;
//!   - the $1,000 federal exception, the line arithmetic, the quarterly rate
//!     periods, the January 1 compounding, the April 15 stop and the
//!     annualised periods and percentages: the 2025 Form 221 and its
//!     instructions, which are the latest published;
//!   - the 10% cap: A.R.S. § 42-1125(Q) and Form 221 line 29;
//!   - the 6% and 7% rates for 2026: the department's Interest Rates page.
//!
//! The 2026 Form 221 is not published. The 2025 form's arithmetic is used
//! with 2026's dates and rates; its printed 7% is 2025's rate, and the
//! divergence "The 2026 rates" says so.
//!
//! The rate from January 1, 2027 is not published. The series ends on
//! December 31, 2026 and the penalty reports the gap. April 15, 2027 is the
//! 2025 form's April 15 a year on.
//!
//! No Arizona holiday list is checked in. The four 2026 due dates the 140ES
//! prints are weekdays, and the test checks the computed dates against them.

use estimated::{Error, Month};
use money::{DOLLAR, Rate, percent};

use crate::estimated::EstimatedYear;

pub(crate) fn year_2026() -> Result<EstimatedYear, Error> {
    let d = estimated::must_date;

    let calendar =
        estimated::new_calendar("weekends only; no Arizona holiday list is checked in").build()?;

    // Form 221 Part B charges one rate period per calendar quarter. The
    // department's page publishes July to December 2026 as one row at 7%; it
    // is split by quarter here as the form splits its rate periods. The
    // series starts with the quarter the first installment falls in.
    const PAGE: &str = "ADOR, Interest Rates";
    let rates = estimated::new_rate_series()
        .period(
            d(2026, Month::APRIL, 1),
            d(2026, Month::JUNE, 30),
            percent(6),
            format!("{PAGE}, 4/1/2026 - 6/30/2026"),
        )
        .period(
            d(2026, Month::JULY, 1),
            d(2026, Month::SEPTEMBER, 30),
            percent(7),
            format!("{PAGE}, 7/1/2026 - 12/31/2026"),
        )
        .period(
            d(2026, Month::OCTOBER, 1),
            d(2026, Month::DECEMBER, 31),
            percent(7),
            format!("{PAGE}, 7/1/2026 - 12/31/2026"),
        )
        .build()?;

    Ok(EstimatedYear {
        year: 2026,
        income_threshold: DOLLAR * 75_000,
        joint_threshold: DOLLAR * 150_000,
        tax_threshold: DOLLAR * 1_000,
        federal_threshold: DOLLAR * 1_000,
        this_year_percent: percent(90),
        prior_year_percent: percent(100),
        penalty_cap: percent(10),
        dues: vec![
            d(2026, Month::APRIL, 15),
            d(2026, Month::JUNE, 15),
            d(2026, Month::SEPTEMBER, 15),
            d(2027, Month::JANUARY, 15),
        ],
        calendar,
        file_and_pay_by: d(2027, Month::FEBRUARY, 1),
        stop: d(2027, Month::APRIL, 15),
        period_ends: vec![
            d(2026, Month::MARCH, 31),
            d(2026, Month::MAY, 31),
            d(2026, Month::AUGUST, 31),
            d(2026, Month::DECEMBER, 31),
        ],
        annualised_percentages: vec![Rate(2250), Rate(4500), Rate(6750), Rate(9000)],
        rates,
    })
}
