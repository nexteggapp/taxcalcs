//! Estimated tax for tax year 2026. Ported from estimated_y2026.go.
//!
//! Every figure here is checked by the Go package's estimated_test.go
//! against an extract in testdata/, listed in testdata/SOURCES.md:
//!
//!   - the $5,000 income test and the $500 threshold: KRS 141.305(1)(a) and
//!     (2), and Form 2210-K (2025) line 5;
//!   - the 90%, 100% and 110% tests, the $150,000 and $75,000 steps, the due
//!     dates, and the September 1 and February 1, 2027 dates of the January
//!     note: the 2026 voucher instructions, 42A740-S4 (9-25), with the dates
//!     also on the 2026 Form 740-ES vouchers, 42A740ES (10/25);
//!   - the 25% installments and withholding, the annualised periods and
//!     percentages, the April 15 stop and the 9% rate for 2026: Form 2210-K
//!     (2025), 42A740-S1 (10-25), the latest published, used for 2026 because
//!     KRS 141.305 is unchanged since 2019;
//!   - the weekend rule: KY-TAM-19-02.
//!
//! revenue.ky.gov refused every download on 2026-10-20, so the department's
//! documents are checked against the quotes in
//! research/estimated-payments/ky.md, copied into testdata/research-ky.md.
//! The statutes and 103 KAR 1:040 are checked against the Legislative
//! Research Commission's own text.
//!
//! The rate from January 1, 2027 is set under KRS 131.183(1)(b)1.b by
//! November 15, 2026 and is not published. The series ends on December 31,
//! 2026 and the penalty reports the gap.
//!
//! No Kentucky holiday list is checked in. The four 2026 due dates are
//! weekdays, so they are moved for weekends only, and the test checks the
//! computed dates against the printed ones. January 31, 2027 is a Sunday, and
//! moving it gives the February 1, 2027 the voucher instructions print.

use estimated::{Error, Month, must_date as d};
use money::{DOLLAR, percent};

use crate::estimated_rules::EstimatedYear;

/// The 2026 figures.
pub(crate) fn year_2026() -> Result<EstimatedYear, Error> {
    let calendar =
        estimated::new_calendar("weekends only; no Kentucky holiday list is checked in").build()?;

    let rates = estimated::new_rate_series()
        .period(
            d(2026, Month::JANUARY, 1),
            d(2026, Month::DECEMBER, 31),
            percent(9),
            "Form 2210-K (2025), 42A740-S1 (10-25), line 21: 9% for calendar year 2026",
        )
        .build()?;

    Ok(EstimatedYear {
        year: 2026,
        income_test: DOLLAR * 5_000,
        threshold: DOLLAR * 500,
        prior_year_step: DOLLAR * 150_000,
        prior_year_step_separate: DOLLAR * 75_000,
        dues: vec![
            d(2026, Month::APRIL, 15),
            d(2026, Month::JUNE, 15),
            d(2026, Month::SEPTEMBER, 15),
            d(2027, Month::JANUARY, 15),
        ],
        calendar,
        late_start: d(2026, Month::SEPTEMBER, 1),
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
