//! Estimated tax for tax year 2026.
//!
//! Every figure here is checked by the Go package's estimated_test.go
//! against an extract in its testdata/, listed in testdata/SOURCES.md:
//!
//!   - the federal condition, the $1,000 prior-year gate, the 90% and 100%
//!     tests, the mover rule and the quarters: N.D.C.C. §§ 57-38-62(1) and
//!     57-38-63, and Form ND-1ES for 2026, which prints the 2026 dates;
//!   - the $1,000 threshold on this year's tax: § 57-38-62(3) and ND-1ES
//!     line 10;
//!   - Items C and D, the payment application rule, the 12% rate, the day
//!     count rounded to three places and the $5.00 rule: Schedule ND-1UT for
//!     2025, the latest the department publishes;
//!   - the quarter of withholding deemed withheld by each date: ND-1UT
//!     line 8; the installments are equal quarters of the required payment,
//!     § 57-38-63 and ND-1UT line 7;
//!   - the annualized percentages: federal Form 2210 Schedule AI, which
//!     ND-1UT Item B adopts;
//!   - the weekend and holiday rule: § 1-03-05, with the holidays of
//!     § 1-03-01.
//!
//! The 12% rate is fixed rather than reset each period, so every day to
//! April 15, 2027 has a rate.
//!
//! The calendar holds weekends only. No 2026 due date, and not April 15,
//! 2027, falls on a § 1-03-01 holiday, and the test checks the computed
//! dates against both the printed ones and that list.
//!
//! `fourth_quarter_by` is the federal date for 2026: January 31, 2027 is a
//! Sunday, so the next business day, February 1. Schedule ND-1UT for 2025
//! printed February 1, 2026 for the same rule, a Sunday.

use estimated::{Error, Month};
use money::{DOLLAR, Rate, percent};

use crate::estimated::{EstimatedYear, date};

/// The 2026 figures.
pub(crate) fn year_2026() -> Result<EstimatedYear, Error> {
    let calendar = estimated::new_calendar(
        "weekends only; no 2026 due date falls on a holiday in N.D.C.C. § 1-03-01",
    )
    .build()?;
    Ok(EstimatedYear {
        year: 2026,
        threshold: DOLLAR * 1_000,
        prior_gate: DOLLAR * 1_000,
        this_year_share: percent(90),
        prior_year_share: percent(100),
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
        item_d: vec![
            date(2026, Month::APRIL, 16),
            date(2026, Month::JUNE, 1),
            date(2026, Month::SEPTEMBER, 1),
        ],
        fourth_quarter_by: date(2027, Month::FEBRUARY, 1),
        stop: date(2027, Month::APRIL, 15),
        rate: percent(12),
        day_basis: 365,
        places: 3,
        small_interest: DOLLAR * 5,
    })
}
