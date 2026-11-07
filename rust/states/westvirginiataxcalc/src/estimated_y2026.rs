//! Estimated tax for tax year 2026.
//!
//! Every figure here is checked by the Go package's estimated_test.go
//! against an extract in its testdata/, listed in testdata/SOURCES.md:
//!
//!   - the $600 threshold, the 90% and 100% tests, the 12-month condition,
//!     the $5,000 limit for a zero prior year, the quarters, the annualised
//!     periods and percentages, the April 15 stop, February 1 and the 11.5%
//!     rate with its 0.000315 daily factor: Form IT-210 and its instructions
//!     in the 2025 IT-140 booklet, which are the latest published;
//!   - the 10% test, the due dates and the weekend and holiday rule: the
//!     IT-140ESI (Sept 2023);
//!   - the 11.5% rate again: TSD-365;
//!   - the late-start date, the last day of January and the holidays:
//!     W. Va. Code §§ 11-21-55(d), 11-10-18a(i) and 2-2-1.
//!
//! No 2026 IT-140ES, IT-140ESI or IT-210 is published. The 2025 IT-210's
//! arithmetic is used with the 2026 dates and the 2026 rate it prints.
//!
//! The rate for 2027 is fixed from the prime rate on the first business day
//! of December 2026 (§ 11-10-17a(c)) and is not published. The factors end
//! on December 31, 2026 and the penalty reports the gap.

use estimated::{Error, Month, Rounding, must_date as d};
use money::{DOLLAR, Rate, percent};

use crate::estimated::EstimatedYear;

/// West Virginia's figures for 2026.
pub(crate) fn year_2026() -> Result<EstimatedYear, Error> {
    // The § 2-2-1(a) holidays from the first due date to the last date these
    // rules move, each moved under § 2-2-1(b): West Virginia Day and
    // Independence Day fall on Saturdays in 2026, so the Fridays before are
    // the holidays. Election days and days the Governor proclaims are left
    // out, because no document checked in gives their dates. None of these
    // falls on a date the rules move.
    let calendar = estimated::new_calendar(
        "W. Va. Code § 2-2-1(a) and (b), April 15, 2026 to February 1, 2027",
    )
    .holiday(d(2026, Month::MAY, 25), "Memorial Day")
    .holiday(
        d(2026, Month::JUNE, 19),
        "West Virginia Day, June 20 falling on a Saturday",
    )
    .holiday(
        d(2026, Month::JULY, 3),
        "Independence Day, July 4 falling on a Saturday",
    )
    .holiday(d(2026, Month::SEPTEMBER, 7), "Labor Day")
    .holiday(d(2026, Month::OCTOBER, 12), "Columbus Day")
    .holiday(d(2026, Month::NOVEMBER, 11), "Veterans' Day")
    .holiday(d(2026, Month::NOVEMBER, 26), "Thanksgiving Day")
    .holiday(d(2026, Month::NOVEMBER, 27), "Lincoln's Day")
    .holiday(d(2026, Month::DECEMBER, 25), "Christmas Day")
    .holiday(d(2027, Month::JANUARY, 1), "New Year's Day")
    .holiday(d(2027, Month::JANUARY, 18), "Martin Luther King's Birthday")
    .build()?;

    let factor = estimated::parse_factor("0.000315")?;
    let daily = estimated::new_factor_series(Rounding::RoundToDollar)
        .period(
            d(2026, Month::JANUARY, 1),
            d(2026, Month::DECEMBER, 31),
            factor,
            Rate(1150),
            "IT-210 (2025), Part IV Section B: 11.5%, 0.000315 a day, January 1 to December 31, 2026; TSD-365",
        )
        .build()?;

    Ok(EstimatedYear {
        year: 2026,
        threshold: DOLLAR * 600,
        small_share: percent(10),
        this_year_share: percent(90),
        prior_year_share: percent(100),
        no_liability_limit: DOLLAR * 5_000,
        dues: vec![
            d(2026, Month::APRIL, 15),
            d(2026, Month::JUNE, 15),
            d(2026, Month::SEPTEMBER, 15),
            d(2027, Month::JANUARY, 15),
        ],
        calendar,
        late_start_after: d(2026, Month::APRIL, 1),
        file_and_pay_by: d(2027, Month::JANUARY, 31),
        stop: d(2027, Month::APRIL, 15),
        period_ends: vec![
            d(2026, Month::MARCH, 31),
            d(2026, Month::MAY, 31),
            d(2026, Month::AUGUST, 31),
            d(2026, Month::DECEMBER, 31),
        ],
        applicable: vec![Rate(2250), Rate(4500), Rate(6750), Rate(9000)],
        daily,
    })
}
