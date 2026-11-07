//! Colorado estimated tax for tax year 2026.
//!
//! Every figure here is checked by the Go package's estimated_test.go
//! against an extract in its testdata/, listed in testdata/SOURCES.md:
//!
//!   - the $1,000 threshold, the 70%, 100% and 110% tests, the $150,000 and
//!     $75,000 steps, the due dates, the weekend and holiday rule and the
//!     4.4% rate: DR 0104EP for 2026 (06/10/25);
//!   - the line arithmetic, the January 31 file-and-pay date, the April 15
//!     stop and the annualised periods, factors and percentages: DR 0204 for
//!     2025 (09/25/25), which is the latest published and whose rules C.R.S.
//!     § 39-22-605 has not changed since;
//!   - the 11% rate for 2026 and the 10% rate for 2027: CDOR's Tax Topics:
//!     Penalties and Interest page, which publishes the rate fixed under
//!     C.R.S. § 39-21-110.5 for each calendar year.
//!
//! The 2026 DR 0204 is not published. The 2025 form's arithmetic is used
//! with the 2026 figures. The 2025 form prints 12% and 11% on lines 15 and
//! 18, which are the rates for 2025 and 2026; a 2026 underpayment runs into
//! 2027, so 2026's and 2027's rates are used and the divergence is reported.
//!
//! No Colorado holiday list is checked in. The four 2026 due dates DR 0104EP
//! prints are weekdays, and the test checks the computed dates against them.
//! The penalty stops by April 15, 2027, and the 2027 rate covers all of 2027,
//! so no day of a 2026 penalty is without a published rate.

use estimated::{Error, Month, must_date as d};
use money::{DOLLAR, Rate, percent};

use crate::estimated::EstimatedYear;

/// The 2026 figures.
pub(crate) fn year_2026() -> Result<EstimatedYear, Error> {
    let calendar =
        estimated::new_calendar("weekends only; no Colorado holiday list is checked in").build()?;

    // § 39-21-110.5(3): one rate for each calendar year, fixed on July 1 of
    // the year before.
    let rates = estimated::new_rate_series()
        .period(
            d(2026, Month::JANUARY, 1),
            d(2026, Month::DECEMBER, 31),
            percent(11),
            "CDOR, Tax Topics: Penalties and Interest: 2026 regular rate, 11%",
        )
        .period(
            d(2027, Month::JANUARY, 1),
            d(2027, Month::DECEMBER, 31),
            percent(10),
            "CDOR, Tax Topics: Penalties and Interest: 2027 regular rate, 10%",
        )
        .build()?;

    Ok(EstimatedYear {
        year: 2026,
        threshold: DOLLAR * 1_000,
        this_year_percent: percent(70),
        prior_percent: percent(100),
        prior_percent_high: percent(110),
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
        factors: vec![Rate(40_000), Rate(24_000), Rate(15_000), Rate(10_000)],
        annualised_percentages: vec![Rate(1_750), Rate(3_500), Rate(5_250), Rate(7_000)],
        tax_rate: Rate(440),
        rates,
    })
}
