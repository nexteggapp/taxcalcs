//! Estimated tax for tax year 2026.
//!
//! Every figure here is checked by the Go package's estimated_test.go
//! against an extract in its testdata/, listed in testdata/SOURCES.md:
//!
//!   - the $500 threshold, the 90% and 100% tests, the 12-month condition,
//!     the due dates, the weekend rule, the January 31 file-and-pay date and
//!     the annualised periods: the 2026 K-40ES;
//!   - the line arithmetic, the 25%, 50%, 75% and 100% shares, the
//!     annualised 22.5%, 45%, 67.5% and 90%, the zero for no prior-year
//!     return, and the 8% rate from January 1, 2026: Schedule K-210 (2025)
//!     and its instructions, which are the latest published;
//!   - the 8% rate again, as interest on unpaid tax: the 2025 K-40 booklet,
//!     line 29;
//!   - the late-start groups, the April 15 stop, the $1 floor and the
//!     calendar-year rate: K.S.A. 79-32,102, 79-32,107 and 79-2968.
//!
//! The 2026 Schedule K-210 is not published. The 2025 schedule's arithmetic
//! is used with the 2026 dates.
//!
//! The rate for 2027 is the federal underpayment rate in effect on July 1,
//! 2026, plus one point (K.S.A. 79-2968), and no Kansas document checked in
//! publishes it. The rates end on December 31, 2026 and the penalty reports
//! the gap.
//!
//! No Kansas holiday list is checked in. The four 2026 due dates the K-40ES
//! prints are weekdays, and the test checks the computed dates against them.

use estimated::{Error, Month, must_date as d};
use money::{DOLLAR, percent};

use crate::estimated::EstimatedYear;

/// The 2026 figures.
pub(crate) fn year_2026() -> Result<EstimatedYear, Error> {
    let calendar =
        estimated::new_calendar("weekends only; no Kansas holiday list is checked in").build()?;

    // K.S.A. 79-2968 fixes the rate for each calendar year.
    let rates = estimated::new_rate_series()
        .period(
            d(2026, Month::JANUARY, 1),
            d(2026, Month::DECEMBER, 31),
            percent(8),
            "Schedule K-210 (2025), instructions for lines 15 and 16: 8% from 1/1/26; K-40 (2025), line 29; K.S.A. 79-2968",
        )
        .build()?;

    Ok(EstimatedYear {
        year: 2026,
        threshold: DOLLAR * 500,
        this_year_share: percent(90),
        prior_year_share: percent(100),
        dues: vec![
            d(2026, Month::APRIL, 15),
            d(2026, Month::JUNE, 15),
            d(2026, Month::SEPTEMBER, 15),
            d(2027, Month::JANUARY, 15),
        ],
        calendar,
        later_groups: vec![
            d(2026, Month::APRIL, 2),
            d(2026, Month::JUNE, 2),
            d(2026, Month::SEPTEMBER, 2),
        ],
        file_and_pay_by: d(2027, Month::JANUARY, 31),
        stop: d(2027, Month::APRIL, 15),
        period_ends: vec![
            d(2026, Month::MARCH, 31),
            d(2026, Month::MAY, 31),
            d(2026, Month::AUGUST, 31),
            d(2026, Month::DECEMBER, 31),
        ],
        rates,
        de_minimis: DOLLAR,
    })
}
