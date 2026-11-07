//! Estimated tax for tax year 2026.
//!
//! Every figure here is checked by the Go package's estimated_test.go
//! against an extract in its testdata/, listed in testdata/SOURCES.md:
//!
//!   - the $500 threshold and the four due dates: the 2026 IT 1040ES;
//!   - 90% and 100%, the 25%, 50%, 75% and 100% columns, the weekend and
//!     holiday rule, 365.25 days, factors printed to six places and the 7%
//!     rate for 2026: the 2025 IT/SD 2210 and its instructions, which are
//!     the latest published;
//!   - the April 15 end of the last span: ORC 5747.09(C), the return's due
//!     date, which the 2025 form's last factor runs to;
//!   - rounding each charge to the dollar: the 2025 IT 1040 booklet;
//!   - the holidays: ORC 1.14.
//!
//! The 2026 IT/SD 2210 is not published. The 2025 form's arithmetic is used
//! with the 2026 dates, and its factors are worked out by the formula the
//! 2025 instructions give, which the test checks gives every 2025 factor.
//!
//! ORC 5703.47(B) sets the rate for 2027 on October 15, 2026, and it is not
//! published. The rates end on December 31, 2026 and the penalty reports the
//! gap. That leaves 15 days of the third span and all of the fourth
//! uncharged.

use estimated::{Error, Month, Rounding, must_date as d};
use money::{DOLLAR, percent};

use crate::estimated::EstimatedYear;

/// Ohio's 2026 figures.
pub(crate) fn year_2026() -> Result<EstimatedYear, Error> {
    // ORC 1.14, January 2026 to April 2027. A day the governor or president
    // appoints is a holiday too and cannot be listed in advance. None of
    // these falls on a Sunday, so the Monday rule moves none. A Saturday is
    // no holiday under 1.14, which moves an act due on a day the office is
    // closed; the IT/SD 2210 instructions move a weekend date.
    let calendar = estimated::new_calendar("ORC 1.14 legal holidays; IT/SD 2210 (2025), page 4")
        .holiday(d(2026, Month::JANUARY, 1), "New Year's day")
        .holiday(d(2026, Month::JANUARY, 19), "Martin Luther King day")
        .holiday(d(2026, Month::FEBRUARY, 16), "Washington-Lincoln day")
        .holiday(d(2026, Month::MAY, 25), "Memorial day")
        .holiday(d(2026, Month::JUNE, 19), "Juneteenth day")
        .holiday(d(2026, Month::JULY, 4), "Independence day")
        .holiday(d(2026, Month::SEPTEMBER, 7), "Labor day")
        .holiday(d(2026, Month::OCTOBER, 12), "Columbus day")
        .holiday(d(2026, Month::NOVEMBER, 11), "Veterans' day")
        .holiday(d(2026, Month::NOVEMBER, 26), "Thanksgiving day")
        .holiday(d(2026, Month::DECEMBER, 25), "Christmas day")
        .holiday(d(2027, Month::JANUARY, 1), "New Year's day")
        .holiday(d(2027, Month::JANUARY, 18), "Martin Luther King day")
        .holiday(d(2027, Month::FEBRUARY, 15), "Washington-Lincoln day")
        .build()?;
    let rates = estimated::new_rate_series()
        .period(
            d(2026, Month::JANUARY, 1),
            d(2026, Month::DECEMBER, 31),
            percent(7),
            "7% for calendar year 2026: IT/SD 2210 (2025), page 4; 2025 IT 1040 booklet; Administrative Journal Entry of October 15, 2025 under ORC 5703.47",
        )
        .build()?;
    Ok(EstimatedYear {
        year: 2026,
        threshold: DOLLAR * 500,
        this_year_share: percent(90),
        prior_year_share: percent(100),
        shares: vec![percent(25), percent(25), percent(25), percent(25)],
        dues: vec![
            d(2026, Month::APRIL, 15),
            d(2026, Month::JUNE, 15),
            d(2026, Month::SEPTEMBER, 15),
            d(2027, Month::JANUARY, 15),
        ],
        calendar,
        stop: d(2027, Month::APRIL, 15),
        rates,
        day_count: 36525,
        places: 6,
        rounding: Rounding::RoundToDollar,
    })
}
