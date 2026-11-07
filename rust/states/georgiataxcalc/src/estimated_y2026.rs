//! Georgia estimated tax for tax year 2026.
//!
//! Every figure here is checked by the Go package's estimated_test.go
//! against an extract in its testdata/, listed in testdata/SOURCES.md:
//!
//!   - the 70% and 100% tests, the equal installments, the late-start chart,
//!     withholding treated as paid equally, Exceptions 1 to 3 and their 70%
//!     and 90%, the January 31 rule, the April 15 stop and the 9% rate: Form
//!     500 UET (Rev. 07/15/25), "For tax years 2025 and later";
//!   - the due dates, the weekend and holiday rule and the requirement test
//!     with its $1,000: Form 500-ES (Rev. 05/12/25), which the Department
//!     lists as the 2026 voucher;
//!   - the 4.99% rate, the $15,000 and $30,000 standard deductions and the
//!     $5,000 dependent exemption: HB 463 (2026), applicable to taxable years
//!     beginning on or after January 1, 2026. The 500-ES still prints the
//!     2025 figures, $12,000, $24,000 and $4,000, and the divergence says so.
//!
//! The 9% is fixed by the form rather than published as a series, so the
//! rate runs to the April 15, 2027 stop and no day goes unrated.
//!
//! No Georgia holiday list is checked in. The four 2026 due dates fall on a
//! Wednesday, a Monday, a Tuesday and a Friday, and the test checks the
//! computed dates against the ones the 500-ES prints.

use std::collections::BTreeMap;

use estimated::{Error, Month};
use filing::Status;
use money::{DOLLAR, Rate, percent};

use crate::estimated::EstimatedYear;

pub(crate) fn year_2026() -> Result<EstimatedYear, Error> {
    let d = estimated::must_date;

    let calendar =
        estimated::new_calendar("weekends only; no Georgia holiday list is checked in").build()?;
    let rates = estimated::new_rate_series()
        .period(
            d(2026, Month::JANUARY, 1),
            d(2027, Month::APRIL, 15),
            percent(9),
            "Form 500 UET (Rev. 07/15/25), line 20: 9 percent a year",
        )
        .build()?;

    Ok(EstimatedYear {
        year: 2026,
        rate: Rate(499),
        this_year_share: percent(70),
        prior_year_share: percent(100),
        unwithheld: DOLLAR * 1_000,
        exemption: DOLLAR * 5_000,
        standard: BTreeMap::from([
            (Status::Single, DOLLAR * 15_000),
            (Status::HeadOfHousehold, DOLLAR * 15_000),
            (Status::MarriedFilingSeparately, DOLLAR * 15_000),
            (Status::MarriedFilingJointly, DOLLAR * 30_000),
        ]),
        form_exemption: DOLLAR * 4_000,
        form_standard: BTreeMap::from([
            (Status::Single, DOLLAR * 12_000),
            (Status::HeadOfHousehold, DOLLAR * 12_000),
            (Status::MarriedFilingSeparately, DOLLAR * 12_000),
            (Status::MarriedFilingJointly, DOLLAR * 24_000),
        ]),
        dues: vec![
            d(2026, Month::APRIL, 15),
            d(2026, Month::JUNE, 15),
            d(2026, Month::SEPTEMBER, 15),
            d(2027, Month::JANUARY, 15),
        ],
        later_groups: vec![
            d(2026, Month::APRIL, 1),
            d(2026, Month::JUNE, 1),
            d(2026, Month::SEPTEMBER, 1),
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
        annualised_share: percent(70),
        actual_share: percent(90),
        rates,
    })
}
