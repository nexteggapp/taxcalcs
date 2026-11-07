//! Estimated tax for tax year 2026. Ported from estimated_y2026.go.
//!
//! Every figure here is checked by the Go package's estimated_test.go
//! against an extract in testdata/, listed in testdata/SOURCES.md:
//!
//!   - the $500 threshold, the 90%, 100% and 110% tests, the $150,000 step,
//!     the $1 minimum for nonresidents, the 25% installments and their dates,
//!     the annualized periods and percentages, the January 31 and January 15
//!     rule, the April 15, 2027 stop and the 7% rate: the 2026 Schedule M15
//!     and its instructions, marked "FINAL DRAFT — 10/1/26";
//!   - the $1 minimum and the dates again: the department's Estimated Tax
//!     Payments for Individuals page;
//!   - the 7% rate for 2026: the department's interest rate table, and
//!     § 270C.40 subd. 4, which fixes one rate for the whole tax year.
//!
//! § 270C.40 subd. 4 takes the rate for January 1, 2026 for every day of a
//! 2026 underpayment, including the days in 2027, so the series runs to April
//! 15, 2027 and no later rate is needed.
//!
//! No Minnesota holiday list is checked in. The four 2026 due dates the M15
//! prints are weekdays, and the test checks the computed dates against them.

use estimated::{Error, Month, must_date as d};
use money::{DOLLAR, Rate, percent};

use crate::estimated_rules::EstimatedYear;

/// The 2026 figures.
pub(crate) fn year_2026() -> Result<EstimatedYear, Error> {
    let calendar =
        estimated::new_calendar("weekends only; no Minnesota holiday list is checked in")
            .build()?;
    let rates = estimated::new_rate_series()
        .period(
            d(2026, Month::JANUARY, 1),
            d(2027, Month::APRIL, 15),
            percent(7),
            "Minn. Stat. § 270C.40 subd. 4; 2026 Schedule M15, line 20: 7% for tax year 2026",
        )
        .build()?;
    Ok(EstimatedYear {
        year: 2026,
        threshold: DOLLAR * 500,
        this_year_share: percent(90),
        prior_year_share: percent(100),
        prior_year_high: percent(110),
        step: DOLLAR * 150_000,
        nonresident_minimum: DOLLAR,
        shares: vec![percent(25), percent(25), percent(25), percent(25)],
        annualised: vec![Rate(2250), Rate(4500), Rate(6750), Rate(9000)],
        dues: vec![
            d(2026, Month::APRIL, 15),
            d(2026, Month::JUNE, 15),
            d(2026, Month::SEPTEMBER, 15),
            d(2027, Month::JANUARY, 15),
        ],
        calendar,
        period_ends: vec![
            d(2026, Month::MARCH, 31),
            d(2026, Month::MAY, 31),
            d(2026, Month::AUGUST, 31),
            d(2026, Month::DECEMBER, 31),
        ],
        file_and_pay_by: d(2027, Month::JANUARY, 31),
        deemed_paid: d(2027, Month::JANUARY, 15),
        stop: d(2027, Month::APRIL, 15),
        rates,
    })
}
