//! Estimated tax for tax year 2026.
//!
//! Every figure here is checked by the Go package's estimated_test.go
//! against an extract in its testdata/, listed in testdata/SOURCES.md:
//!
//!   - the $1,000 threshold, the 90% and 100% tests, the 25% installment,
//!     the due dates, the weekend and holiday rule, the January 31
//!     file-and-pay date, the April 15 stop and the 1% a month: Form
//!     CT-1040ES (Rev. 01/26), for 2026;
//!   - the line arithmetic, the installment, withholding and PE Tax Credit
//!     percentages, the annualised periods and percentages, the months from
//!     the 16th to the 15th, Boxes E, F and G and the January 31 rule's
//!     effect on Schedule B: Form CT-2210 (Rev. 12/25) and its instructions,
//!     for 2025, which are the latest published;
//!   - the same rules in Conn. Gen. Stat. §§ 12-701(a)(12) and 12-722, and
//!     the 2026 Supplement, which leaves both unchanged.
//!
//! The 2026 CT-2210 is not published. The 2025 form's arithmetic is used
//! with the 2026 dates from CT-1040ES, and its Schedule A period ends a year
//! on. No figure it prints is a yearly rate: the 1% a month is
//! § 12-722(a)'s.
//!
//! The rate is set by statute, not by notice, so the months from January 1,
//! 2027 to the April 15, 2027 stop have a rate and nothing is reported as
//! unpublished.
//!
//! No Connecticut holiday list is checked in. § 12-722(c) does not move a
//! due date; CT-1040ES does, for a Saturday, Sunday or legal holiday. The
//! four 2026 due dates it prints are weekdays, and the test checks the
//! computed dates against them. A later year whose date falls on a
//! Connecticut holiday needs that list before it ships.

use estimated::{Error, Month, must_date as d};
use money::{DOLLAR, Rate, percent};

use crate::estimated::EstimatedYear;

/// Connecticut's figures for 2026.
pub(crate) fn year_2026() -> Result<EstimatedYear, Error> {
    let calendar =
        estimated::new_calendar("weekends only; no Connecticut holiday list is checked in")
            .build()?;

    Ok(EstimatedYear {
        year: 2026,
        threshold: DOLLAR * 1_000,
        this_year_share: percent(90),
        prior_year_share: percent(100),
        shares: vec![percent(25), percent(25), percent(25), percent(25)],
        annualised: vec![Rate(2250), Rate(4500), Rate(6750), Rate(9000)],
        dues: vec![
            d(2026, Month::APRIL, 15),
            d(2026, Month::JUNE, 15),
            d(2026, Month::SEPTEMBER, 15),
            d(2027, Month::JANUARY, 15),
        ],
        calendar,
        file_and_pay_by: d(2027, Month::JANUARY, 31),
        stop: d(2027, Month::APRIL, 15),
        period_ends: vec![
            d(2026, Month::MARCH, 31),
            d(2026, Month::MAY, 31),
            d(2026, Month::AUGUST, 31),
            d(2026, Month::DECEMBER, 31),
        ],
        monthly_rate: percent(1),
    })
}
