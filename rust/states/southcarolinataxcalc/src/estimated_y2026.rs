//! Estimated tax for tax year 2026.
//!
//! Every figure here is checked by the Go package's estimated_test.go
//! against an extract in its testdata/, listed in testdata/SOURCES.md:
//!
//!   - the $100 threshold, the 100% and 110% tests, the $150,000 step, the
//!     due dates, the late-start groups and the February 1 file-and-pay date:
//!     the 2026 SC1040ES;
//!   - the 90% test, the $75,000 step for a separate return, the 25%
//!     installments, the April 15 stop and the printed .07: the 2025 SC2210
//!     and its instructions, the latest published;
//!   - the annualisation periods and percentages, and the January 31 date
//!     before moving: 26 U.S.C. § 6654, which S.C. Code § 12-54-55(1) adopts
//!     and the SC2210 reaches through the federal Schedule AI;
//!   - the rates for 2026: SC Information Letter #26-19;
//!   - the legal holidays: SC Revenue Procedure #23-1.
//!
//! The 2026 SC2210 is not published. The 2025 form's arithmetic is used with
//! the 2026 figures. Its rate periods are kept, one rate period for each, and
//! each carries the rate Information Letter #26-19 publishes for it rather
//! than the .07 the 2025 form prints; the result reports the divergence.
//!
//! The rate from January 1, 2027 is not published. The series ends on
//! December 31, 2026 and the penalty reports the gap. No 2026 underpayment
//! runs before April 16, so the series starts with the period that holds it.
//!
//! Revenue Procedure #23-1 gives each holiday as a rule, such as the third
//! Monday in January, and moves one that falls on a Saturday to the Friday
//! before and one on a Sunday to the Monday after. The dates below apply
//! those rules to 2026 and to January to April 2027, the span the due dates
//! and the file-and-pay date can reach. December 26, 2026 is a Saturday whose
//! Friday is Christmas Day, so it adds no day.

use estimated::{Error, Month, must_date as d};
use money::{DOLLAR, Rate, percent};

use crate::estimated::EstimatedYear;

/// The 2026 figures.
pub(crate) fn year_2026() -> Result<EstimatedYear, Error> {
    let calendar =
        estimated::new_calendar("SC Revenue Procedure #23-1, legal holidays in South Carolina")
            .holiday(d(2026, Month::JANUARY, 1), "New Year's Day")
            .holiday(d(2026, Month::JANUARY, 19), "Martin Luther King, Jr. Day")
            .holiday(
                d(2026, Month::FEBRUARY, 16),
                "George Washington's Birthday/President's Day",
            )
            .holiday(
                d(2026, Month::MAY, 11),
                "Confederate Memorial Day (observed)",
            )
            .holiday(d(2026, Month::MAY, 25), "National Memorial Day")
            .holiday(
                d(2026, Month::JUNE, 19),
                "Juneteenth National Independence Day",
            )
            .holiday(d(2026, Month::JULY, 3), "Independence Day (observed)")
            .holiday(d(2026, Month::SEPTEMBER, 7), "Labor Day")
            .holiday(d(2026, Month::OCTOBER, 12), "Columbus Day")
            .holiday(d(2026, Month::NOVEMBER, 11), "Veteran's Day")
            .holiday(d(2026, Month::NOVEMBER, 26), "Thanksgiving Day")
            .holiday(d(2026, Month::NOVEMBER, 27), "Day after Thanksgiving")
            .holiday(d(2026, Month::DECEMBER, 24), "Christmas Eve")
            .holiday(d(2026, Month::DECEMBER, 25), "Christmas Day")
            .holiday(d(2027, Month::JANUARY, 1), "New Year's Day")
            .holiday(d(2027, Month::JANUARY, 18), "Martin Luther King, Jr. Day")
            .holiday(
                d(2027, Month::FEBRUARY, 15),
                "George Washington's Birthday/President's Day",
            )
            .build()?;

    let rates = estimated::new_rate_series()
        .period(
            d(2026, Month::APRIL, 1),
            d(2026, Month::JUNE, 30),
            percent(6),
            "SC Information Letter #26-19: 6%, 04/01/26 - 06/30/26",
        )
        .period(
            d(2026, Month::JULY, 1),
            d(2026, Month::SEPTEMBER, 30),
            percent(7),
            "SC Information Letter #26-19: 7%, 07/01/26 - 12/31/26",
        )
        .period(
            d(2026, Month::OCTOBER, 1),
            d(2026, Month::DECEMBER, 31),
            percent(7),
            "SC Information Letter #26-19: 7%, 07/01/26 - 12/31/26",
        )
        .build()?;

    Ok(EstimatedYear {
        year: 2026,
        threshold: DOLLAR * 100,
        this_year_percent: percent(90),
        prior_year_percent: percent(100),
        high_income_percent: percent(110),
        prior_year_step: DOLLAR * 150_000,
        prior_year_step_separate: DOLLAR * 75_000,
        shares: vec![percent(25), percent(25), percent(25), percent(25)],
        annualised_percentages: vec![Rate(2250), Rate(4500), Rate(6750), Rate(9000)],
        dues: vec![
            d(2026, Month::APRIL, 15),
            d(2026, Month::JUNE, 15),
            d(2026, Month::SEPTEMBER, 15),
            d(2027, Month::JANUARY, 15),
        ],
        calendar,
        late_groups: vec![
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
        form_rate: percent(7),
        rates,
    })
}
