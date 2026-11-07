//! Estimated tax for tax year 2026.
//!
//! Every figure here is checked by the Go package's estimated_test.go
//! against an extract in its testdata/, listed in testdata/SOURCES.md:
//!
//!   - the due dates, the 90% test and the 10% a year penalty: the 2026
//!     AR1000ES;
//!   - the $1,000 threshold, the 100% prior-year test, the four equal parts,
//!     the April 15 stop and the 365-day year: the 2025 AR2210 and its
//!     instructions, whose worked example also rounds each charge to the
//!     whole dollar;
//!   - the January 31 file-and-pay date: the AR2210 instructions, Exception
//!     2, and Subject 603;
//!   - the annualisation periods, the 22.5%, 45%, 67.5% and 90% and the
//!     daily factor of .00027397: the 2025 AR2210A.
//!
//! The 2026 AR2210 and AR2210A are not published. The 2025 forms'
//! arithmetic is used with the 2026 dates, a year on from the dates they
//! print. Their rules match the 2026 AR1000ES, and
//! research/estimated-payments/ar.md found no act changing them for 2026.
//! The 10% is fixed, not a rate reset each period, so the days in 2027 up to
//! the April 15 stop are charged at it too.
//!
//! No Arkansas holiday list is checked in. The four 2026 due dates the
//! AR1000ES prints are weekdays, and the test checks the computed dates
//! against them.

use estimated::{Error, Month, Rounding, must_date as d};
use money::{DOLLAR, Rate, percent};

use crate::estimated::EstimatedYear;

/// Arkansas's figures for 2026.
pub(crate) fn year_2026() -> Result<EstimatedYear, Error> {
    let calendar =
        estimated::new_calendar("weekends only; no Arkansas holiday list is checked in").build()?;

    let factor = estimated::parse_factor("0.00027397")?;
    let annualised_factor = estimated::new_factor_series(Rounding::RoundToDollar)
        .period(
            d(2026, Month::JANUARY, 1),
            d(2027, Month::APRIL, 15),
            factor,
            percent(10),
            "AR2210A (2025), line 43: .00027397 a day",
        )
        .build()?;

    Ok(EstimatedYear {
        year: 2026,
        threshold: DOLLAR * 1_000,
        this_year_percent: percent(90),
        prior_year_percent: percent(100),
        shares: vec![percent(25), percent(25), percent(25), percent(25)],
        dues: vec![
            d(2026, Month::APRIL, 15),
            d(2026, Month::JUNE, 15),
            d(2026, Month::SEPTEMBER, 15),
            d(2027, Month::JANUARY, 15),
        ],
        calendar,
        file_and_pay_by: d(2027, Month::JANUARY, 31),
        stop: d(2027, Month::APRIL, 15),
        rate: percent(10),
        rate_source: "AR1000ES (2026), Underestimate of Tax: ten percent (10%) per annum",
        year_days: 365,
        period_ends: vec![
            d(2026, Month::MARCH, 31),
            d(2026, Month::MAY, 31),
            d(2026, Month::AUGUST, 31),
            d(2026, Month::DECEMBER, 31),
        ],
        annualised_percentages: vec![Rate(2250), Rate(4500), Rate(6750), Rate(9000)],
        annualised_factor,
    })
}
