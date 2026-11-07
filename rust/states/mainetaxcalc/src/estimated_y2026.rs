//! Estimated tax for tax year 2026. Ported from estimated_y2026.go.
//!
//! Every figure here is checked by the Go package's estimated_test.go
//! against an extract in testdata/, listed in testdata/SOURCES.md:
//!
//!   - the $1,000 tests, the 90% and 100% shares, the 12-month condition, the
//!     $500,000 unusual event, four equal installments, the due months, the
//!     January 31 relief and the end of the penalty period: 36 M.R.S. § 5228;
//!   - the end of the penalty period as the return's due date: § 5227;
//!   - moving a date for a weekend or Maine legal holiday: § 153(2), with the
//!     holidays from 4 M.R.S. § 1051;
//!   - monthly compounding: § 186;
//!   - the printed 2026 dates: Form 1040ES-ME (2026);
//!   - 25% of withholding a quarter, the .75% monthly rate for 2026, a part of
//!     a month as a whole one, and the annualized periods and percentages:
//!     Form 2210ME (2025) and the 2025 Annualized Income Installment
//!     Worksheet.
//!
//! www.maine.gov refused every download of the Maine Revenue Services PDFs on
//! 2026-10-20, so the figures in the last two items are checked against the
//! verified quotes in research/estimated-payments/me.md, copied into
//! testdata/research-me-quotes.txt. The statutes come from the Revisor's site.
//!
//! No 2026 date moves. April 15, June 15 and September 15, 2026 and January
//! 15, 2027 are weekdays and none is a Maine legal holiday; Patriot's Day is
//! April 20, 2026 and April 19, 2027. January 31, 2027, the last day for the
//! file-and-pay relief, is a Sunday and moves to Monday, February 1.
//!
//! The § 186 rate for 2027 is not carried. The rates end on December 31, 2026
//! and the penalty reports the months after.

use estimated::{Error, Month, must_date as d};
use money::{DOLLAR, Rate, percent};

use crate::estimated_rules::{EstimatedYear, MonthlyRate, validate};

/// The 2026 figures.
pub(crate) fn year_2026() -> Result<EstimatedYear, Error> {
    // 4 M.R.S. § 1051, January 2026 to April 2027. The annual Thanksgiving is
    // "any day designated", not a date the statute fixes, and is left out; no
    // date this crate moves is in November. None of these falls on a Sunday,
    // so the Monday rule moves none.
    let calendar = estimated::new_calendar("4 M.R.S. § 1051, applied by 36 M.R.S. § 153(2)")
        .holiday(d(2026, Month::JANUARY, 1), "New Year's Day")
        .holiday(d(2026, Month::JANUARY, 19), "Martin Luther King, Jr., Day")
        .holiday(d(2026, Month::FEBRUARY, 16), "Washington's Birthday")
        .holiday(d(2026, Month::APRIL, 20), "Patriot's Day")
        .holiday(d(2026, Month::MAY, 25), "Memorial Day")
        .holiday(d(2026, Month::JUNE, 19), "Juneteenth")
        .holiday(d(2026, Month::JULY, 4), "the 4th of July")
        .holiday(d(2026, Month::SEPTEMBER, 7), "Labor Day")
        .holiday(d(2026, Month::OCTOBER, 12), "Indigenous Peoples Day")
        .holiday(d(2026, Month::NOVEMBER, 11), "Veterans Day")
        .holiday(d(2026, Month::DECEMBER, 25), "Christmas Day")
        .holiday(d(2027, Month::JANUARY, 1), "New Year's Day")
        .holiday(d(2027, Month::JANUARY, 18), "Martin Luther King, Jr., Day")
        .holiday(d(2027, Month::FEBRUARY, 15), "Washington's Birthday")
        .holiday(d(2027, Month::APRIL, 19), "Patriot's Day")
        .build()?;

    validate(EstimatedYear {
        year: 2026,
        threshold: DOLLAR * 1_000,
        this_year_share: percent(90),
        prior_year_share: percent(100),
        unusual_event: DOLLAR * 500_000,
        dues: vec![
            d(2026, Month::APRIL, 15),
            d(2026, Month::JUNE, 15),
            d(2026, Month::SEPTEMBER, 15),
            d(2027, Month::JANUARY, 15),
        ],
        file_and_pay_by: d(2027, Month::JANUARY, 31),
        stop: d(2027, Month::APRIL, 15),
        calendar,
        period_ends: vec![
            d(2026, Month::MARCH, 31),
            d(2026, Month::MAY, 31),
            d(2026, Month::AUGUST, 31),
            d(2026, Month::DECEMBER, 31),
        ],
        annualised: vec![Rate(2250), Rate(4500), Rate(6750), Rate(9000)],
        rates: vec![MonthlyRate {
            from: d(2026, Month::JANUARY, 1),
            to: d(2026, Month::DECEMBER, 31),
            monthly: Rate(75),
            source: "Form 2210ME (2025) instructions: .75% per month or fraction thereof, compounded monthly, for calendar year 2026".to_string(),
        }],
    })
}
