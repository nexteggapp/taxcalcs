//! Estimated tax for tax year 2026.
//!
//! Every figure here is checked by the Go package's estimated_test.go
//! against an extract in its testdata/, listed in testdata/SOURCES.md:
//!
//!   - the $1,000 threshold, the 90% and 100% tests, the dates, the
//!     late-start schedule, the January 31 rule and the April 15 stop: NMSA
//!     1978 § 7-2-12.2 as Laws 2011, HB 470 enacted it;
//!   - the 25% installments, the line arithmetic, the weekend and holiday
//!     rule and the first-quarter rule for April 1 to 15: Form RPD-41272 for
//!     2025, whose 2026 edition is not published;
//!   - the rates, 6% from April 1 to June 30, 2026 and 7% from July 1 to
//!     December 31, 2026: the department's Penalty Interest Rates page.
//!
//! The rate from January 1, 2027 is not published. The series ends on
//! December 31, 2026 and the penalty reports the gap.
//!
//! No New Mexico holiday list is checked in. The four 2026 due dates are
//! weekdays, and the test checks the computed dates against the printed
//! ones.

use estimated::{Error, Month};
use money::{DOLLAR, percent};

use crate::estimated::{EstimatedYear, date};

pub(crate) fn year_2026() -> Result<EstimatedYear, Error> {
    let calendar =
        estimated::new_calendar("weekends only; no New Mexico holiday list is checked in")
            .build()?;
    let rates = estimated::new_rate_series()
        .period(
            date(2026, Month::APRIL, 1),
            date(2026, Month::JUNE, 30),
            percent(6),
            "Taxation and Revenue Department, Penalty Interest Rates: April 1, 2026 – June 30, 2026, 6%",
        )
        .period(
            date(2026, Month::JULY, 1),
            date(2026, Month::DECEMBER, 31),
            percent(7),
            "Taxation and Revenue Department, Penalty Interest Rates: July 1, 2026 – December 31, 2026, 7%",
        )
        .build()?;
    Ok(EstimatedYear {
        year: 2026,
        threshold: DOLLAR * 1_000,
        this_year_share: percent(90),
        prior_year_share: percent(100),
        shares: vec![percent(25), percent(25), percent(25), percent(25)],
        dues: vec![
            date(2026, Month::APRIL, 15),
            date(2026, Month::JUNE, 15),
            date(2026, Month::SEPTEMBER, 15),
            date(2027, Month::JANUARY, 15),
        ],
        calendar,
        later_groups: vec![
            date(2026, Month::APRIL, 1),
            date(2026, Month::JUNE, 1),
            date(2026, Month::SEPTEMBER, 1),
        ],
        file_and_pay_by: date(2027, Month::JANUARY, 31),
        stop: date(2027, Month::APRIL, 15),
        rates,
    })
}
