//! Capital gains tax payments for tax year 2026.
//!
//! Every figure here is checked by the Go package's estimated_test.go
//! against an extract in its testdata/, listed in testdata/SOURCES.md:
//!
//!   - the April 15, 2027 due date and the October 15, 2026 first day for a
//!     prepayment: the department's special notice of August 3, 2026, and
//!     RCW 82.87.110(3) and 82.87.115(1);
//!   - the 9%, 19% and 29% late payment penalty and its $5 minimum, and the
//!     80%, $1,000 and 5% of the penalty on a low estimate: RCW 82.32.090(1)
//!     and (10);
//!   - interest from April 30, 2027: RCW 82.32.050(1)(c)(ii);
//!   - the 6% rate for 2027: the department's interest rate tables;
//!   - Memorial Day, May 31, 2027: RCW 1.16.050(1)(e).
//!
//! The rate for 2028 is set on January 1, 2028 and is not published. The
//! rates end on December 31, 2027 and the penalty reports the gap.
//!
//! The due date is the federal return's due date (RCW 82.87.110(1)(a)),
//! which moves for District of Columbia holidays too: WAC 458-20-300 Example
//! 1 puts 2023's on April 18. April 15 and 16, 2027 are a Thursday and a
//! Friday and no holiday falls on the 15th, so the 2027 date does not move.
//! The calendar lists only Memorial Day: of the RCW 1.16.050 holidays for
//! 2027 it is the one that falls on a date these rules use, May 31, the last
//! day of the month after the due date.

use estimated::{Date, Error, Month};
use money::{DOLLAR, percent};

use crate::estimated::EstimatedYear;

pub(crate) fn year_2026() -> Result<EstimatedYear, Error> {
    let d = estimated::must_date;

    let calendar = estimated::new_calendar(
        "RCW 1.16.050(1) and (5), the 2027 state legal holidays that fall on a date these rules use",
    )
    .holiday(d(2027, Month::MAY, 31), "Memorial Day")
    .build()?;

    // The department publishes one rate per calendar year.
    let calendar_year =
        |y: i64| -> (Date, Date) { (d(y, Month::JANUARY, 1), d(y, Month::DECEMBER, 31)) };
    let (from_2027, to_2027) = calendar_year(2027);
    let interest = estimated::new_rate_series()
        .period(
            from_2027,
            to_2027,
            percent(6),
            "Department of Revenue, Interest rate tables, Excise tax: 6% for 2027; RCW 82.32.050(2)",
        )
        .build()?;

    Ok(EstimatedYear {
        year: 2026,
        due: d(2027, Month::APRIL, 15),
        prepay_from: d(2026, Month::OCTOBER, 15),
        calendar,
        late_totals: vec![percent(9), percent(19), percent(29)],
        late_minimum: DOLLAR * 5,
        underpaid_share: percent(80),
        underpaid_floor: DOLLAR * 1_000,
        underpaid_rate: percent(5),
        interest_from: d(2027, Month::APRIL, 30),
        interest,
    })
}
