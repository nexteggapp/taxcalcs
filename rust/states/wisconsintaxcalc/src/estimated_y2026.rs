//! Estimated tax for tax year 2026.
//!
//! Every figure here is checked by the Go package's estimated_test.go
//! against an extract in its testdata/, listed in testdata/SOURCES.md:
//!
//!   - the $500 threshold, the 90% and 100% tests and the four due dates:
//!     the 2026 Form 1-ES instructions, and Wis. Stat. § 71.09(11) to (13);
//!   - the quarters, the withholding shares, the annualised periods and
//!     percentages, the line arithmetic and the 12% ÷ 365: the 2025 Schedule
//!     U and its instructions, which are the latest published, and
//!     §§ 71.09(13) and 71.84(1);
//!   - the January 31 file-and-pay date: § 71.09(14) and the 1-ES; the April
//!     15, 2027 stop: § 71.84(1);
//!   - the holidays: Wis. Stat. § 995.20, which § 990.001(4)(e) makes the
//!     legal holidays a due date moves for, with the two election days it
//!     names dated by § 5.02(5) and (12s).
//!
//! The 2026 Schedule U is not published. The 2025 form's arithmetic is used
//! with the 2026 dates.
//!
//! The rate is set by statute, not by notice. The series runs from April 16,
//! 2026, the first day interest can accrue, to the April 15, 2027 stop, and
//! nothing is reported as unpublished.
//!
//! The holidays are those § 995.20 sets from January 1, 2026 to the stop.
//! None falls on a Sunday in that span, so none moves to a Monday.

use estimated::{Error, Month, must_date as d};
use money::{DOLLAR, Rate, percent};

use crate::estimated::EstimatedYear;

/// Wisconsin's figures for 2026.
pub(crate) fn year_2026() -> Result<EstimatedYear, Error> {
    let calendar = estimated::new_calendar(
        "Wis. Stat. § 995.20, with § 5.02(5) and (12s), applied by § 990.001(4)(b), (c) and (e)",
    )
    .holiday(d(2026, Month::JANUARY, 1), "January 1")
    .holiday(d(2026, Month::JANUARY, 19), "the 3rd Monday in January")
    .holiday(d(2026, Month::FEBRUARY, 16), "the 3rd Monday in February")
    .holiday(d(2026, Month::MAY, 25), "the last Monday in May")
    .holiday(d(2026, Month::JUNE, 19), "June 19, Juneteenth Day")
    .holiday(d(2026, Month::JULY, 4), "July 4")
    .holiday(
        d(2026, Month::AUGUST, 11),
        "the partisan primary, the 2nd Tuesday in August",
    )
    .holiday(d(2026, Month::SEPTEMBER, 7), "Labor day")
    .holiday(d(2026, Month::OCTOBER, 12), "the 2nd Monday in October")
    .holiday(
        d(2026, Month::NOVEMBER, 3),
        "the general election, the Tuesday after the first Monday in November",
    )
    .holiday(d(2026, Month::NOVEMBER, 11), "November 11")
    .holiday(
        d(2026, Month::NOVEMBER, 26),
        "the 4th Thursday in November, Thanksgiving",
    )
    .holiday(d(2026, Month::DECEMBER, 25), "December 25")
    .holiday(d(2027, Month::JANUARY, 1), "January 1")
    .holiday(d(2027, Month::JANUARY, 18), "the 3rd Monday in January")
    .holiday(d(2027, Month::FEBRUARY, 15), "the 3rd Monday in February")
    .build()?;

    let rates = estimated::new_rate_series()
        .period(
            d(2026, Month::APRIL, 16),
            d(2027, Month::APRIL, 15),
            percent(12),
            "Wis. Stat. § 71.84(1): 12 percent per year; Schedule U (2025), lines 27 and 28: days ÷ 365 × .12",
        )
        .build()?;

    Ok(EstimatedYear {
        year: 2026,
        threshold: DOLLAR * 500,
        this_year_share: percent(90),
        prior_year_share: percent(100),
        shares: vec![percent(25), percent(25), percent(25), percent(25)],
        annualised: vec![Rate(2250), Rate(4500), Rate(6750), Rate(9000)],
        period_ends: vec![
            d(2026, Month::MARCH, 31),
            d(2026, Month::MAY, 31),
            d(2026, Month::AUGUST, 31),
            d(2026, Month::DECEMBER, 31),
        ],
        dues: vec![
            d(2026, Month::APRIL, 15),
            d(2026, Month::JUNE, 15),
            d(2026, Month::SEPTEMBER, 15),
            d(2027, Month::JANUARY, 15),
        ],
        calendar,
        file_and_pay_by: d(2027, Month::JANUARY, 31),
        stop: d(2027, Month::APRIL, 15),
        rates,
    })
}
