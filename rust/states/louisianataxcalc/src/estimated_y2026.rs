//! Louisiana estimated tax for tax year 2026.
//!
//! Every figure here is checked by the Go package's estimated_test.go
//! against an extract in its testdata/, listed in testdata/SOURCES.md:
//!
//!   - the 70% share, the May 15 stop and the repeal of the annualized
//!     method: Act 307 of 2026, which applies to tax periods beginning on or
//!     after January 1, 2026 and took effect May 22, 2026 (LDR's 2026
//!     Legislative Summaries);
//!   - the $1,000 and $2,000 thresholds, the four due dates, the weekend
//!     rule and the January 31 file-and-pay date: IT-540ESi (2026);
//!   - the late-start groups, line 5's whole dollars, the carryforward, the
//!     exceptions, the Exception 5 periods and the 0.000329 daily factor:
//!     R-210R-i (1/26), the latest published, for 2025;
//!   - the 12% rate, 100% of last year's tax and the equal part of
//!     withholding: R.S. 47:118, unchanged by Act 307 on those points.
//!
//! No 2026 R-210R is published. The 2025 form's arithmetic is used, with Act
//! 307's figures where the form predates it.
//!
//! The rate is fixed by R.S. 47:118(A) and is not reset, so one factor
//! covers every day to the May 15, 2027 stop. May 15, 2027 is a Saturday; it
//! ends the penalty period rather than being a due date, and nothing says
//! it moves.
//!
//! No Louisiana holiday list is checked in. The four 2026 due dates
//! IT-540ESi prints are weekdays, and the test checks the computed dates
//! against them.

use estimated::{Error, Month, Rounding, must_date as d};
use money::{DOLLAR, percent};

use crate::estimated::EstimatedYear;

/// Louisiana's figures for 2026.
pub(crate) fn year_2026() -> Result<EstimatedYear, Error> {
    let calendar =
        estimated::new_calendar("weekends only; no Louisiana holiday list is checked in")
            .build()?;

    let factor = estimated::parse_factor("0.000329")?;
    let daily = estimated::new_factor_series(Rounding::RoundToCent)
        .period(
            d(2026, Month::JANUARY, 1),
            d(2027, Month::MAY, 15),
            factor,
            percent(12),
            "R-210R-i (1/26), Line 18: 12 percent per year, 0.0329 percent a day; La. R.S. 47:118(A)",
        )
        .build()?;

    Ok(EstimatedYear {
        year: 2026,
        this_year_share: percent(70),
        threshold: DOLLAR * 1_000,
        threshold_joint: DOLLAR * 2_000,
        dues: vec![
            d(2026, Month::APRIL, 15),
            d(2026, Month::JUNE, 15),
            d(2026, Month::SEPTEMBER, 15),
            d(2027, Month::JANUARY, 15),
        ],
        later_groups: vec![
            d(2026, Month::APRIL, 2),
            d(2026, Month::JUNE, 2),
            d(2026, Month::SEPTEMBER, 2),
        ],
        calendar,
        file_and_pay_by: d(2027, Month::JANUARY, 31),
        stop: d(2027, Month::MAY, 15),
        period_ends: vec![
            d(2026, Month::MARCH, 31),
            d(2026, Month::MAY, 31),
            d(2026, Month::AUGUST, 31),
            d(2026, Month::DECEMBER, 31),
        ],
        daily,
    })
}
