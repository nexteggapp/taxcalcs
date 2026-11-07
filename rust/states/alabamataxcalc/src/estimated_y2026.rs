//! Estimated tax for tax year 2026.
//!
//! Every figure here is checked by the Go package's estimated_test.go against
//! an extract in its testdata/, listed in testdata/SOURCES.md:
//!
//!   - the $500 threshold, the 90%, 100% and 110% tests, the $150,000 and
//!     $75,000 steps, the due dates and the late-start schedule: the 2026
//!     Form 40ES instructions;
//!   - the 25% installments, the Schedule AI periods and percentages, the
//!     April 15 stop and the printed 7% rate: the 2025 Form 2210AL and its
//!     instructions, the latest published;
//!   - the quarterly rates for 2026: the Department of Revenue's Quarterly
//!     Interest Rates page;
//!   - the January 31 date of § 6654(h), which no Alabama form prints: 26
//!     U.S.C. § 6654, which Code of Ala. § 40-18-80 adopts.
//!
//! The 2026 Form 2210AL is not published. The 2025 form's arithmetic is used
//! with the 2026 figures from the 40ES. Its line 11 prints 7%, which was the
//! rate for every quarter of 2025; for 2026 the department's published rates
//! are used for each quarter, and the result reports the divergence.
//!
//! The rate from January 1, 2027 is not published. The series ends on
//! December 31, 2026 and the penalty reports the gap. The first quarter of
//! 2026 is left out because no 2026 underpayment runs before April 16.
//!
//! No State holiday is in the calendar. The four 2026 due dates the 40ES
//! prints are weekdays and none is a State holiday under Code of Ala. §
//! 1-3-8; the test checks both.

use estimated::{Error, Month, must_date as d};
use money::{DOLLAR, Rate, percent};

use crate::rules::EstimatedYear;

pub(crate) fn year_2026() -> Result<EstimatedYear, Error> {
    let calendar = estimated::new_calendar(
        "weekends only; no 2026 due date falls on a Code of Ala. § 1-3-8 State holiday",
    )
    .build()?;

    let rates = estimated::new_rate_series()
        .period(
            d(2026, Month::APRIL, 1),
            d(2026, Month::JUNE, 30),
            percent(6),
            "ADOR, Quarterly Interest Rates: 2026, 2nd quarter",
        )
        .period(
            d(2026, Month::JULY, 1),
            d(2026, Month::SEPTEMBER, 30),
            percent(7),
            "ADOR, Quarterly Interest Rates: 2026, 3rd quarter",
        )
        .period(
            d(2026, Month::OCTOBER, 1),
            d(2026, Month::DECEMBER, 31),
            percent(7),
            "ADOR, Quarterly Interest Rates: 2026, 4th quarter",
        )
        .build()?;

    Ok(EstimatedYear {
        year: 2026,
        threshold: DOLLAR * 500,
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
