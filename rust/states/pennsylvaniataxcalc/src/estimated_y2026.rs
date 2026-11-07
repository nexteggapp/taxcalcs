//! Estimated tax for tax year 2026.
//!
//! Every figure here is checked by the Go package's estimated_test.go against
//! an extract in its testdata/, listed in testdata/SOURCES.md:
//!
//!   - the $14,000 income threshold, the $430 tax threshold, the 3.07% rate,
//!     the due dates, the late-start table, the 90% test and the prior-year
//!     income test: the 2026 REV-413 (I);
//!   - the line arithmetic, the exceptions, the special exception, the
//!     January 31 rule, the April 15 stop and the daily factor: the 2025
//!     REV-1630 and its instructions, which are the latest published;
//!   - the 7% rate and its daily factor for 2026: REV-1611 (11-25).
//!
//! The 2026 REV-1630 is not published. The 2025 form's arithmetic is used
//! with the 2026 figures from the REV-413 (I), and the special exception's
//! tax figure is the year's own threshold, as the 2025 form uses 2025's
//! $338.
//!
//! The rate for 2027 is set under Fiscal Code § 806 from the federal rate on
//! January 1, 2027, and it is not published. The rates end on December 31,
//! 2026 and the penalty reports the gap.
//!
//! No Pennsylvania holiday list is checked in. The four 2026 due dates the
//! REV-413 (I) prints are weekdays, and the test checks the computed dates
//! against them.

use estimated::{Error, Month, Rounding, must_date as d};
use money::{DOLLAR, Rate, percent};

use crate::rules::EstimatedYear;

pub(crate) fn year_2026() -> Result<EstimatedYear, Error> {
    let calendar =
        estimated::new_calendar("weekends only; no Pennsylvania holiday list is checked in")
            .build()?;

    let factor = estimated::parse_factor("0.000192")?;
    let daily = estimated::new_factor_series(Rounding::RoundToDollar)
        .period(
            d(2026, Month::JANUARY, 1),
            d(2026, Month::DECEMBER, 31),
            factor,
            percent(7),
            "REV-1611 (11-25): 7%, 0.000192 a day, 1/1/25 - 12/31/26",
        )
        .build()?;

    Ok(EstimatedYear {
        year: 2026,
        rate: Rate(307),
        income_threshold: DOLLAR * 14_000,
        tax_threshold: DOLLAR * 430,
        dues: vec![
            d(2026, Month::APRIL, 15),
            d(2026, Month::JUNE, 15),
            d(2026, Month::SEPTEMBER, 15),
            d(2027, Month::JANUARY, 15),
        ],
        later_groups: vec![
            d(2026, Month::APRIL, 1),
            d(2026, Month::JUNE, 1),
            d(2026, Month::SEPTEMBER, 1),
        ],
        calendar,
        file_and_pay_by: d(2027, Month::JANUARY, 31),
        stop: d(2027, Month::APRIL, 15),
        period_ends: vec![
            d(2026, Month::MARCH, 31),
            d(2026, Month::MAY, 31),
            d(2026, Month::AUGUST, 31),
            d(2026, Month::DECEMBER, 31),
        ],
        daily,
    })
}
