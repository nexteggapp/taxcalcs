//! Estimated tax for tax year 2026.
//!
//! Every figure here is checked by the Go package's estimated_test.go
//! against an extract in its testdata/, listed in testdata/SOURCES.md:
//!
//!   - the $100 declaration threshold, the 90% and 100% tests, the due
//!     dates, the weekend and holiday rule and equal installments: the 2026
//!     Form MO-1040ES and RSMo 143.521;
//!   - the $500 stop, the exceptions and their percentages, the late-start
//!     table, the January 31 rule, withholding in equal parts and the April
//!     15 stop: Form MO-2210 for 2025, whose 2026 edition is not published,
//!     with its dates moved a year;
//!   - the 7% rate for 2026: 12 CSR 10-41.010, emergency amendment, and the
//!     department's Statutory Interest Rates page, which also posts 7% for
//!     2027.
//!
//! No Missouri holiday list is checked in. The four 2026 due dates are
//! weekdays, and the test checks the computed dates against the printed
//! ones.

use estimated::{Error, Month};
use money::{DOLLAR, percent};

use crate::estimated::{EstimatedYear, date};

/// The 2026 figures.
pub(crate) fn year_2026() -> Result<EstimatedYear, Error> {
    let calendar =
        estimated::new_calendar("weekends only; no Missouri holiday list is checked in").build()?;
    let rates = estimated::new_rate_series()
        .period(
            date(2026, Month::JANUARY, 1),
            date(2026, Month::DECEMBER, 31),
            percent(7),
            "12 CSR 10-41.010 (emergency amendment effective January 1, 2026): 7% for 2026",
        )
        .period(
            date(2027, Month::JANUARY, 1),
            date(2027, Month::DECEMBER, 31),
            percent(7),
            "Missouri DOR, Statutory Interest Rates: 7% for 2027",
        )
        .build()?;

    Ok(EstimatedYear {
        year: 2026,
        threshold: DOLLAR * 500,
        declaration: DOLLAR * 100,
        this_year_share: percent(90),
        prior_year_share: percent(100),
        income_share: percent(90),
        withholding_shares: vec![percent(25), percent(25), percent(25), percent(25)],
        dues: vec![
            date(2026, Month::APRIL, 15),
            date(2026, Month::JUNE, 15),
            date(2026, Month::SEPTEMBER, 15),
            date(2027, Month::JANUARY, 15),
        ],
        calendar,
        later_groups: vec![
            date(2026, Month::APRIL, 2),
            date(2026, Month::JUNE, 2),
            date(2026, Month::SEPTEMBER, 2),
        ],
        period_ends: vec![
            date(2026, Month::MARCH, 31),
            date(2026, Month::MAY, 31),
            date(2026, Month::AUGUST, 31),
            date(2026, Month::DECEMBER, 31),
        ],
        file_and_pay_by: date(2027, Month::JANUARY, 31),
        deemed_paid: date(2027, Month::JANUARY, 15),
        stop: date(2027, Month::APRIL, 15),
        rates,
    })
}
