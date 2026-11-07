//! Estimated tax for tax year 2026.
//!
//! The figures come from the documents listed below. The Go package's
//! estimated_test.go checks the statutes' figures against their extracts in
//! its testdata/, and Treasury's against the verified quotes in
//! testdata/research-mi.md, because www.michigan.gov refused the downloads.
//! testdata/SOURCES.md lists the figures no quote covers.
//!
//!   - the $500 threshold and the due dates: MCL 206.301(1) and the 2026
//!     MI-1040ES (Rev. 04-25);
//!   - the 90%, 100% and 110% tests and the $150,000 and $75,000 steps: the
//!     2026 MI-1040ES, the 2025 MI-2210 (Rev. 10-25) and RAB 2021-17;
//!   - the line arithmetic, the withholding quarters, the annualised periods
//!     and percentages, the 25% and 10% penalties with their $25 and $10
//!     minimums, the exceptions and the April 15 stop: the 2025 MI-2210 and
//!     its instructions, which are the latest published, and RAB 2021-17;
//!   - the January 31 rule: RAB 2021-17 and the 2026 MI-1040ES, with MCL 8.6
//!     moving January 31, 2027, a Sunday, to Monday, February 1;
//!   - the daily interest factors: RAB 2026-5 for 2026 and RAB 2026-10 for
//!     January 1 to June 30, 2027.
//!
//! The 2026 MI-2210 is not published. The 2025 form's arithmetic is used
//! with the 2026 dates and rates. No statute behind these rules was amended
//! in 2024, 2025 or 2026 (research/estimated-payments/mi.md), so no form
//! here predates the law it describes.
//!
//! The published factors run to June 30, 2027, past the April 15, 2027
//! stop, so no 2026 penalty reaches a day without a rate.
//!
//! No Michigan holiday list is checked in. The four 2026 due dates the
//! MI-1040ES prints are weekdays, and the test checks the computed dates
//! against them.

use estimated::{Error, Month, Rounding, must_date as d};
use money::{DOLLAR, Rate, percent};

use crate::estimated::EstimatedYear;

/// Michigan's figures for 2026.
pub(crate) fn year_2026() -> Result<EstimatedYear, Error> {
    let calendar =
        estimated::new_calendar("MCL 8.6, weekends only; no Michigan holiday list is checked in")
            .build()?;

    let daily = estimated::new_factor_series(Rounding::RoundToDollar)
        .period(
            d(2026, Month::JANUARY, 1),
            d(2026, Month::JUNE, 30),
            estimated::parse_factor("0.0002324")?,
            Rate(848),
            "RAB 2026-5: January 1, 2026 to June 30, 2026, 8.48%, 0.0002324 a day",
        )
        .period(
            d(2026, Month::JULY, 1),
            d(2026, Month::DECEMBER, 31),
            estimated::parse_factor("0.0002150")?,
            Rate(785),
            "RAB 2026-5: July 1, 2026 to December 31, 2026, 7.85%, 0.0002150 a day",
        )
        .period(
            d(2027, Month::JANUARY, 1),
            d(2027, Month::JUNE, 30),
            estimated::parse_factor("0.0002129")?,
            Rate(777),
            "RAB 2026-10: January 1, 2027 to June 30, 2027, 7.77%, 0.0002129 a day",
        )
        .build()?;

    Ok(EstimatedYear {
        year: 2026,
        threshold: DOLLAR * 500,
        prior_year_step: DOLLAR * 150_000,
        prior_year_step_separate: DOLLAR * 75_000,
        dues: vec![
            d(2026, Month::APRIL, 15),
            d(2026, Month::JUNE, 15),
            d(2026, Month::SEPTEMBER, 15),
            d(2027, Month::JANUARY, 15),
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
        late_groups: vec![
            d(2026, Month::APRIL, 1),
            d(2026, Month::JUNE, 1),
            d(2026, Month::SEPTEMBER, 1),
        ],
        daily,
        not_paid_rate: percent(25),
        not_paid_minimum: DOLLAR * 25,
        short_rate: percent(10),
        short_minimum: DOLLAR * 10,
    })
}
