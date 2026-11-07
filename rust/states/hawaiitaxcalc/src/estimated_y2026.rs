//! Estimated tax for tax year 2026. Ported from estimated_y2026.go.
//!
//! Every figure here is checked by the Go package's estimated_test.go
//! against an extract in testdata/, listed in testdata/SOURCES.md:
//!
//!   - the $500 threshold, the 60% and 100% tests, the 2/3 of 1% rate, the
//!     April 20 stop and the due dates on the 20th: HRS § 235-97;
//!   - the same threshold, tests and dates for 2026, and the weekend and
//!     holiday rule: Tax Facts 2019-3 (revised February 2026) and the 2026
//!     dates the Form N-11 instructions (Rev. 2025) print;
//!   - the line arithmetic, the .00667 factor, the month from the 21st to the
//!     20th, the January 31 file-and-pay date and the Schedule A periods and
//!     percentages: Form N-210 and its instructions (Rev. 2025), which are the
//!     latest published;
//!   - the January 31 rule and the late-start schedule: HAR § 18-235-97;
//!   - the weekend and holiday rule for tax due dates: HRS § 231-21, with the
//!     state holidays of HRS § 8-1.
//!
//! The 2026 Form N-210 is not published. The 2025 form's arithmetic is used
//! with the 2026 dates, a year on from the dates it prints. Its percentages,
//! threshold and rate are fixed in HRS § 235-97, which no act has changed
//! since 2003, so no 2025 figure is a rate of that year standing in for
//! 2026's. The rate is fixed by statute, not set for each period, so the
//! months in 2027 up to the April 20 stop are charged at it too.
//!
//! June 20, 2026 is a Saturday and September 20, 2026 a Sunday, so those
//! installments are due Monday, June 22 and Monday, September 21. No HRS § 8-1
//! holiday falls on a 2026 due date or the business day after one, so the
//! calendar lists none; the test checks the moved dates against the § 8-1
//! holidays of those months.

use estimated::{Error, Month, must_date as d};
use money::{DOLLAR, percent};

use crate::estimated_rules::EstimatedYear;

/// The 2026 figures.
pub(crate) fn year_2026() -> Result<EstimatedYear, Error> {
    let calendar = estimated::new_calendar(
        "HRS § 231-21: weekends; no HRS § 8-1 holiday falls on a 2026 due date or the business day after one",
    )
    .build()?;

    let monthly = estimated::parse_factor("0.00667")?;

    Ok(EstimatedYear {
        year: 2026,
        threshold: DOLLAR * 500,
        this_year_percent: percent(60),
        prior_percent: percent(100),
        dues: vec![
            d(2026, Month::APRIL, 20),
            d(2026, Month::JUNE, 20),
            d(2026, Month::SEPTEMBER, 20),
            d(2027, Month::JANUARY, 20),
        ],
        calendar,
        file_and_pay_by: d(2027, Month::JANUARY, 31),
        stop: d(2027, Month::APRIL, 20),
        monthly,
        annual_rate: percent(8),
        rate_source: "HRS § 235-97(f): two-thirds of one per cent a month; Form N-210 (Rev. 2025), line 20: .00667".to_string(),
        period_ends: vec![
            d(2026, Month::MARCH, 31),
            d(2026, Month::MAY, 31),
            d(2026, Month::AUGUST, 31),
            d(2026, Month::DECEMBER, 31),
        ],
        annualised_percentages: vec![percent(15), percent(30), percent(45), percent(60)],
    })
}
