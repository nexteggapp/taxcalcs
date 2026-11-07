//! Estimated tax for tax year 2026.
//!
//! Every figure here is checked by the Go package's estimated_test.go against
//! an extract in its testdata/, listed in testdata/SOURCES.md:
//!
//!   - the 90% and 100% tests, the 25% installments, the dates, the
//!     annualized percentages, the January 31 rule and the April 15 stop:
//!     N.C. Gen. Stat. § 105-163.15;
//!   - the $1,000 threshold, the line arithmetic and the January column rule:
//!     Form D-422 for 2025, whose 2026 edition is not published, and Form
//!     NC-40 for 2026;
//!   - the 7% rate for all of 2026: the department's Interest Rate page.
//!
//! § 105-163.15(f) takes the threshold from 26 U.S.C. § 6654(e); the figure
//! here is the $1,000 the department's forms print.
//!
//! The rate from January 1, 2027 is not published. The series ends on
//! December 31, 2026 and the penalty reports the gap.
//!
//! No North Carolina holiday list is checked in. The four 2026 due dates are
//! weekdays, and the test checks the computed dates against the printed ones.

use estimated::{Error, Month};
use money::{DOLLAR, Rate, percent};

use crate::rules::{EstimatedYear, date};

pub(crate) fn year_2026() -> Result<EstimatedYear, Error> {
    let calendar =
        estimated::new_calendar("weekends only; no North Carolina holiday list is checked in")
            .build()?;
    let rates = estimated::new_rate_series()
        .period(
            date(2026, Month::JANUARY, 1),
            date(2026, Month::DECEMBER, 31),
            percent(7),
            "NCDOR, Interest Rate: 7% for January 1, 2025 through December 31, 2026, and for July 1 through December 31, 2026",
        )
        .build()?;
    Ok(EstimatedYear {
        year: 2026,
        threshold: DOLLAR * 1_000,
        this_year_share: percent(90),
        prior_year_share: percent(100),
        shares: vec![percent(25), percent(25), percent(25), percent(25)],
        annualised: vec![Rate(2250), Rate(4500), Rate(6750), Rate(9000)],
        dues: vec![
            date(2026, Month::APRIL, 15),
            date(2026, Month::JUNE, 15),
            date(2026, Month::SEPTEMBER, 15),
            date(2027, Month::JANUARY, 15),
        ],
        calendar,
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
