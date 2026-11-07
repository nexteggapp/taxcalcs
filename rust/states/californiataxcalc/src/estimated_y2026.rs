//! California estimated tax for tax year 2026.
//!
//! Every figure here is checked by the Go package's estimated_test.go against
//! an extract in its testdata/, listed in testdata/SOURCES.md:
//!
//!   - the $500 and $250 thresholds, the 90%, 100% and 110% tests, the
//!     $150,000 and $75,000 steps, the $1,000,000 and $500,000 limits, the
//!     30/40/0/30 shares, the due dates and the January 31 file-and-pay date:
//!     the 2026 Instructions for Form 540-ES;
//!   - the annualised percentages and periods, withholding in the same
//!     shares, and the penalty arithmetic: FTB 5805 (2025) and its
//!     instructions, which are the latest published and whose rules are
//!     unchanged for 2026;
//!   - the rates: the FTB's interest and estimate penalty rates page;
//!   - the group-return exception: the FTB's analysis of AB 1518, with the
//!     statute read in research/estimated-payments/ca.md.
//!
//! The FTB has not published the rate from January 1, 2027. The series ends
//! on December 31, 2026 and the penalty reports the gap.
//!
//! No FTB holiday list is checked in. Every due date the 540-ES prints is a
//! weekday, so they are moved for weekends only, and the test checks the
//! computed dates against the printed ones. A later year whose printed date
//! falls on a California holiday needs that list before it ships.

use estimated::{Error, Month, must_date as d};
use money::{DOLLAR, percent};

use crate::rules::EstimatedYear;

pub(crate) fn year_2026() -> Result<EstimatedYear, Error> {
    let calendar =
        estimated::new_calendar("weekends only; no FTB holiday list is checked in").build()?;

    // § 19521: the January rate applies July to December, the July rate
    // January to June. The FTB publishes each as a period on its rates page.
    let rates = estimated::new_rate_series()
        .period(
            d(2025, Month::JULY, 1),
            d(2026, Month::JUNE, 30),
            percent(7),
            "FTB, Interest and estimate penalty rates, 07/01/2025 – 06/30/2026",
        )
        .period(
            d(2026, Month::JULY, 1),
            d(2026, Month::DECEMBER, 31),
            percent(7),
            "FTB, Interest and estimate penalty rates, 07/01/2026 – 12/31/2026",
        )
        .build()?;

    Ok(EstimatedYear {
        year: 2026,
        threshold: DOLLAR * 500,
        threshold_separate: DOLLAR * 250,
        prior_year_step: DOLLAR * 150_000,
        prior_year_step_separate: DOLLAR * 75_000,
        high_income: DOLLAR * 1_000_000,
        high_income_separate: DOLLAR * 500_000,
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
        rates,
        group_return_exempt: true,
    })
}
