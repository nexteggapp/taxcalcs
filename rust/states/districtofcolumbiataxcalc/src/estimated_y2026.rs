//! District of Columbia estimated tax for tax year 2026.
//!
//! Every figure here is checked by the Go package's estimated_test.go against
//! an extract in its testdata/, listed in testdata/SOURCES.md:
//!
//!   - the $100 threshold, the 90% and 110% tests, the due dates and the
//!     weekend and holiday rule: D-40ES (REV. 03/2026), the 2026 booklet;
//!   - the line arithmetic, the $100 and 10% exceptions, the full-year
//!     residence condition and the four interest factors: D-2210 (revised
//!     05/2025), the form for 2025, which is the latest published;
//!   - the 10% rate: D.C. Code § 47-4201(d)(2); the April 15 stop and the
//!     annualisation periods of 3, 5 and 8 months: D.C. Code § 47-4203;
//!   - the later declaration dates for a requirement first met after April
//!     1: D.C. Code § 47-1812.08(i)(4).
//!
//! No 2026 D-2210 is published. The 2025 form's arithmetic is used with the
//! 2026 figures. Its factors are not 2025 figures: they are the District's
//! fixed 10% rate for spans of 61, 92, 122 and 90 days between the due dates
//! and April 15, and 2026's spans have the same lengths, so the printed
//! factors are used as they stand.
//!
//! No District holiday list is checked in. D-40ES moves a date that falls on
//! a Saturday, Sunday or legal holiday without saying which holidays count.
//! The four 2026 due dates D-40ES prints are weekdays, and the test checks
//! the computed dates against them.

use estimated::{Error, Month, must_date as d};
use money::{DOLLAR, percent};

use crate::rules::EstimatedYear;

pub(crate) fn year_2026() -> Result<EstimatedYear, Error> {
    let calendar = estimated::new_calendar(
        "weekends only; D-40ES (REV. 03/2026) moves a date for a legal holiday without listing them",
    )
    .build()?;

    let mut factors = Vec::new();
    for printed in [".0175", ".0265", ".0351", ".0259"] {
        factors.push(estimated::parse_factor(printed)?);
    }

    Ok(EstimatedYear {
        year: 2026,
        threshold: DOLLAR * 100,
        this_year_percent: percent(90),
        prior_percent: percent(110),
        remaining: percent(10),
        dues: vec![
            d(2026, Month::APRIL, 15),
            d(2026, Month::JUNE, 15),
            d(2026, Month::SEPTEMBER, 15),
            d(2027, Month::JANUARY, 15),
        ],
        later_groups: vec![
            d(2026, Month::APRIL, 2),
            d(2026, Month::JUNE, 2),
            d(2026, Month::SEPTEMBER, 2),
        ],
        calendar,
        stop: d(2027, Month::APRIL, 15),
        factors,
        rate: percent(10),
        period_ends: vec![
            d(2026, Month::MARCH, 31),
            d(2026, Month::MAY, 31),
            d(2026, Month::AUGUST, 31),
            d(2026, Month::DECEMBER, 31),
        ],
    })
}
