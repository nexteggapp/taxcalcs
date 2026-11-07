//! Estimated tax for tax year 2026.
//!
//! Every figure here is checked by the Go package's estimated_test.go
//! against an extract in its testdata/, listed in testdata/SOURCES.md:
//!
//!   - the $300 threshold, the 90%, 100% and 110% tests, the $150,000 and
//!     $75,000 steps, the 25% installments, the dates, the January 31 rule,
//!     the annualized percentages and the April 15 stop: Tax Law § 685(c)
//!     and (d), the 2026 IT-2105-I and the 2025 IT-2105.9-I, whose 2026
//!     edition is not published;
//!   - the rates, 8.5% for April to June 2026 and 9.5% for July to
//!     December: the department's quarterly interest rate pages for
//!     § 697(j).
//!
//! The rate from January 1, 2027 is not published. The series ends on
//! December 31, 2026 and the penalty reports the gap.
//!
//! No New York holiday list is checked in. The four 2026 due dates are
//! weekdays, and the test checks the computed dates against the printed
//! ones.

use estimated::{Error, Month, must_date as d};
use money::{DOLLAR, Rate, percent};

use crate::estimated::EstimatedYear;

/// New York's 2026 figures.
pub(crate) fn year_2026() -> Result<EstimatedYear, Error> {
    let calendar =
        estimated::new_calendar("weekends only; no New York holiday list is checked in").build()?;
    let q2 = "DTF, Interest rates: 4/1/2026–6/30/2026: § 697(j) underpayment rate 8.5%";
    let q3 = "DTF, Interest rates: 7/1/2026–9/30/2026: § 697(j) underpayment rate 9.5%";
    let q4 = "DTF, Interest rates: 10/1/2026–12/31/2026: § 697(j) underpayment rate 9.5%";
    let rates = estimated::new_rate_series()
        .period(
            d(2026, Month::APRIL, 1),
            d(2026, Month::JUNE, 15),
            Rate(850),
            q2,
        )
        .period(
            d(2026, Month::JUNE, 16),
            d(2026, Month::JUNE, 30),
            Rate(850),
            q2,
        )
        .period(
            d(2026, Month::JULY, 1),
            d(2026, Month::SEPTEMBER, 15),
            Rate(950),
            q3,
        )
        .period(
            d(2026, Month::SEPTEMBER, 16),
            d(2026, Month::SEPTEMBER, 30),
            Rate(950),
            q3,
        )
        .period(
            d(2026, Month::OCTOBER, 1),
            d(2026, Month::DECEMBER, 31),
            Rate(950),
            q4,
        )
        .build()?;
    Ok(EstimatedYear {
        year: 2026,
        threshold: DOLLAR * 300,
        this_year_share: percent(90),
        prior_year_share: percent(100),
        prior_year_high: percent(110),
        step: DOLLAR * 150_000,
        step_separate: DOLLAR * 75_000,
        shares: vec![percent(25), percent(25), percent(25), percent(25)],
        annualised: vec![Rate(2250), Rate(4500), Rate(6750), Rate(9000)],
        dues: vec![
            d(2026, Month::APRIL, 15),
            d(2026, Month::JUNE, 15),
            d(2026, Month::SEPTEMBER, 15),
            d(2027, Month::JANUARY, 15),
        ],
        calendar,
        period_ends: vec![
            d(2026, Month::MARCH, 31),
            d(2026, Month::MAY, 31),
            d(2026, Month::AUGUST, 31),
            d(2026, Month::DECEMBER, 31),
        ],
        file_and_pay_by: d(2027, Month::JANUARY, 31),
        stop: d(2027, Month::APRIL, 15),
        rates,
    })
}
