//! Estimated tax for tax year 2026.
//!
//! Every figure here is checked by the Go package's estimated_test.go
//! against an extract in its testdata/, listed in testdata/SOURCES.md:
//!
//!   - the four 2026 due dates: the 2026 Form ES-40;
//!   - the $1,000 threshold, the 90%, 100% and 110% tests, the $150,000 and
//!     $75,000 steps, the quarters, the installment periods and the 10%
//!     penalty: the 2025 Schedule IT-2210 and its instructions, which are
//!     the latest published;
//!   - the annualised percentages: the 2025 Schedule IT-2210A;
//!   - the January 31 file-and-pay date, the weekend rule and the treatment
//!     of late payments: Information Bulletin #3 (March 2023);
//!   - the zero prior-year tax rule, effective January 1, 2026, and the 10%
//!     in the statute: the Indiana Code as quoted by
//!     research/estimated-payments/in.md, because iga.in.gov could not be
//!     fetched.
//!
//! No 2026 IT-2210 is published. The 2025 form's arithmetic is used with the
//! 2026 dates. The DOR's 2026 Legislative Synopsis lists no 2026 change to
//! the threshold, the percentages, the dates or the 10% rate; SEA 243 (2026)
//! rewords the penalty's reference to IC 6-8.1-10-2.1(b) from July 1, 2026
//! without changing it.
//!
//! No Indiana holiday list is checked in. The four 2026 due dates and
//! February 1, 2027 are weekdays, so only weekends move a date, and the
//! tests check the computed dates against the printed ones.

use estimated::{Error, Month};
use money::{DOLLAR, Rate, percent};

use crate::estimated::EstimatedYear;

pub(crate) fn year_2026() -> Result<EstimatedYear, Error> {
    let d = estimated::must_date;

    let calendar =
        estimated::new_calendar("weekends only; no Indiana holiday list is checked in").build()?;

    Ok(EstimatedYear {
        year: 2026,
        threshold: DOLLAR * 1_000,
        this_year: percent(90),
        prior: percent(100),
        prior_high: percent(110),
        step: DOLLAR * 150_000,
        step_separate: DOLLAR * 75_000,
        rate: percent(10),
        dues: vec![
            d(2026, Month::APRIL, 15),
            d(2026, Month::JUNE, 15),
            d(2026, Month::SEPTEMBER, 15),
            d(2027, Month::JANUARY, 15),
        ],
        calendar,
        file_and_pay_by: d(2027, Month::JANUARY, 31),
        period_ends: vec![
            d(2026, Month::MARCH, 31),
            d(2026, Month::MAY, 31),
            d(2026, Month::AUGUST, 31),
            d(2026, Month::DECEMBER, 31),
        ],
        annualised: vec![Rate(2250), Rate(4500), Rate(6750), Rate(9000)],
        zero_prior_tax: true,
    })
}
