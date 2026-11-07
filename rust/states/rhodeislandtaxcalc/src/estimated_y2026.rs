//! Estimated tax for tax year 2026.
//!
//! Every figure here is checked by the Go package's estimated_test.go
//! against an extract in its testdata/, listed in testdata/SOURCES.md:
//!
//!   - the due dates and the February 15 date for a return filed in place of
//!     the January payment: the 2026 RI-1040ES;
//!   - the 80% and 100% options, the $250 floor, the division by four, the
//!     withholding rule, the January 31 date, the April 15 stop, and the
//!     annualised periods and percentages: the 2025 RI-2210 and its
//!     instructions, the latest published;
//!   - the 12% rate for calendar year 2026: ADV 2025-23, which the 2025
//!     RI-1040 instructions repeat;
//!   - the $2 minimum and the April 15 stop: R.I. Gen. Laws § 44-30-84;
//!   - the weekend and holiday rule: § 44-30-91(b); the holidays: § 25-1-1.
//!
//! The 2026 RI-2210 is not published. The 2025 form's arithmetic is used
//! with the 2026 dates from the RI-1040ES. The 2025 form says to count days
//! in column C to January 1 on line 19a and to December 31 on line 18; the
//! days are counted through, as § 44-30-84(b) charges for the whole period.
//!
//! The rate for 2027 is set on January 1, 2027 under § 44-1-7(b) and is not
//! published. The rates end on December 31, 2026 and the penalty reports the
//! gap.
//!
//! The holidays are § 25-1-1's from the first due date to February 16,
//! 2027, the last date these rules move. Two kinds are left out: election
//! day, because whether 2026 holds a general election of state officers is
//! not in the checked-in documents, and days a governor or president
//! appoints, such as Thanksgiving, because no proclamation is checked in.
//! Neither falls on a date these rules move. No 2026 due date is a weekend
//! or a holiday, so none moves; February 15, 2027 is Washington's Birthday,
//! so the RI-1040ES date moves to February 16.

use estimated::{Error, Month, must_date as d};
use money::{DOLLAR, percent};

use crate::estimated::EstimatedYear;

/// Rhode Island's 2026 figures.
pub(crate) fn year_2026() -> Result<EstimatedYear, Error> {
    let calendar = estimated::new_calendar("R.I. Gen. Laws § 25-1-1, April 2026 to February 2027")
        .holiday(
            d(2026, Month::MAY, 4),
            "the fourth day of May (Rhode Island Independence Day)",
        )
        .holiday(
            d(2026, Month::MAY, 25),
            "the last Monday of May (Memorial Day)",
        )
        .holiday(
            d(2026, Month::JUNE, 19),
            "the nineteenth day of June (Juneteenth National Freedom Day)",
        )
        .holiday(
            d(2026, Month::JULY, 6),
            "the fourth day of July, a Saturday, celebrated the following Monday (Independence Day)",
        )
        .holiday(
            d(2026, Month::AUGUST, 10),
            "the second Monday of August (Victory Day)",
        )
        .holiday(
            d(2026, Month::SEPTEMBER, 7),
            "the first Monday of September (Labor Day)",
        )
        .holiday(
            d(2026, Month::OCTOBER, 12),
            "the second Monday of October (Columbus Day)",
        )
        .holiday(
            d(2026, Month::NOVEMBER, 11),
            "the eleventh day of November (Veterans' Day)",
        )
        .holiday(
            d(2026, Month::DECEMBER, 25),
            "the twenty-fifth day of December (Christmas Day)",
        )
        .holiday(
            d(2027, Month::JANUARY, 1),
            "the first day of January (New Year's Day)",
        )
        .holiday(
            d(2027, Month::JANUARY, 18),
            "the third Monday of January (Dr. Martin Luther King, Jr.'s Birthday)",
        )
        .holiday(
            d(2027, Month::FEBRUARY, 15),
            "the third Monday of February (Washington's Birthday)",
        )
        .build()?;
    let rates = estimated::new_rate_series()
        .period(
            d(2026, Month::JANUARY, 1),
            d(2026, Month::DECEMBER, 31),
            percent(12),
            "ADV 2025-23 (November 26, 2025): 12.00% for calendar year 2026, other tax types; RI-1040 instructions (2025): 12% (0.1200)",
        )
        .build()?;
    Ok(EstimatedYear {
        year: 2026,
        floor: DOLLAR * 250,
        this_year_share: percent(80),
        prior_year_share: percent(100),
        dues: vec![
            d(2026, Month::APRIL, 15),
            d(2026, Month::JUNE, 15),
            d(2026, Month::SEPTEMBER, 15),
            d(2027, Month::JANUARY, 15),
        ],
        calendar,
        penalty_form_by: d(2027, Month::JANUARY, 31),
        coupon_by: d(2027, Month::FEBRUARY, 15),
        stop: d(2027, Month::APRIL, 15),
        period_ends: vec![
            d(2026, Month::MARCH, 31),
            d(2026, Month::MAY, 31),
            d(2026, Month::AUGUST, 31),
            d(2026, Month::DECEMBER, 31),
        ],
        applicable: vec![percent(20), percent(40), percent(60), percent(80)],
        rates,
        minimum: DOLLAR * 2,
    })
}
