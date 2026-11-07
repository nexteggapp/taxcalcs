//! Estimated tax for tax year 2026.
//!
//! Every figure here is checked by the Go package's estimated_test.go
//! against an extract in its testdata/, listed in testdata/SOURCES.md:
//!
//!   - the $500 threshold, the quarter due by each date, the four due dates,
//!     the January 31 file-and-pay date and the weekend rule: the 2026
//!     Payment Voucher Worksheet (PVW), with Tax-General §§ 1-201, 10-815,
//!     10-820 and 10-902;
//!   - the $500 exception, the 90% and 110% tests, the first-time filer
//!     rule, the income periods, withholding on its dates, the cumulative
//!     columns and the months-late rule: Form 502UP (2025) and its
//!     instructions, the latest published, with Tax-General §§ 13-602 and
//!     13-604;
//!   - the 10.8133% rate for 2026: Form 502UP (2025) and the 2025 resident
//!     booklet;
//!   - the 9.995% rate for 2027: Maryland Register 53:18, September 4, 2026.
//!
//! The 2026 Form 502UP is not published, so the interest factors it will
//! print for 2026's spans are not known and none is carried here. The 2025
//! form's factors are for 2025's rates and are not used. The interest is
//! charged by the month under § 13-604(a), and the penalty says so in
//! Required.
//!
//! No Maryland holiday list is checked in. The four 2026 due dates the PVW
//! prints, and April 15, 2027, are weekdays; January 31, 2027 is a Sunday
//! and moves to Monday, February 1 under § 1-201(b).

use estimated::{Error, Month};
use money::{DOLLAR, FineRate, percent};

use crate::estimated::EstimatedYear;

pub(crate) fn year_2026() -> Result<EstimatedYear, Error> {
    let d = estimated::must_date;

    let calendar =
        estimated::new_calendar("weekends only; no Maryland holiday list is checked in").build()?;

    let rates = estimated::new_rate_series()
        .fine_period(
            d(2026, Month::JANUARY, 1),
            d(2026, Month::DECEMBER, 31),
            FineRate(108_133),
            "Form 502UP (2025), Line 16: 0.9011% a month after December 31, 2025; 2025 resident booklet: 10.8133% a year",
        )
        .fine_period(
            d(2027, Month::JANUARY, 1),
            d(2027, Month::DECEMBER, 31),
            FineRate(99_950),
            "Maryland Register 53:18 (September 4, 2026): 9.995% a year for 2027",
        )
        .build()?;

    Ok(EstimatedYear {
        year: 2026,
        threshold: DOLLAR * 500,
        this_year_share: percent(90),
        prior_year_share: percent(110),
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
        rates,
    })
}
