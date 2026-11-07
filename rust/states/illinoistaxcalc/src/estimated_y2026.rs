//! Estimated tax for tax year 2026.
//!
//! Every figure here is checked by the Go package's estimated_test.go
//! against an extract in its testdata/, listed in testdata/SOURCES.md:
//!
//!   - the $1,000 threshold, the 90% and 100% measures, the four equal
//!     installments and their dates, and the farmer and nursing-home
//!     exceptions: the 2026 IL-1040-ES (R-09/26);
//!   - the line arithmetic, the 2% and 10% rates and the 30-day boundary,
//!     the weekend and holiday rule, the withholding and pass-through rules,
//!     the other exceptions, and the annualised periods and percentages: the
//!     2025 IL-2210 and its instructions (R-12/25), which are the latest
//!     published;
//!   - the 7% interest rate for 2025 and 2026, used only when the caller
//!     asks for interest: the department's Interest Rates page.
//!
//! The 2026 IL-2210 is not published. The 2025 form's arithmetic is used
//! with the 2026 figures from the IL-1040-ES. The statute side of each
//! figure is research/estimated-payments/il.md, copied into testdata,
//! because ilga.gov refused the statutes to a script.
//!
//! The interest rate from January 1, 2027 is not published. The series ends
//! on December 31, 2026 and the penalty reports the gap when interest is
//! asked for.
//!
//! No Illinois holiday list is checked in. The four 2026 due dates the
//! IL-1040-ES prints are weekdays, and the test checks the computed dates
//! against them.

use estimated::{Error, Month, must_date as d};
use money::{DOLLAR, Rate, percent};

use crate::estimated::EstimatedYear;

/// Illinois's 2026 figures.
pub(crate) fn year_2026() -> Result<EstimatedYear, Error> {
    let calendar =
        estimated::new_calendar("weekends only; no Illinois holiday list is checked in").build()?;
    let interest = estimated::new_rate_series()
        .period(
            d(2025, Month::JANUARY, 1),
            d(2026, Month::DECEMBER, 31),
            percent(7),
            "IDOR, Interest Rates: 01/01/2025 - 12/31/2026, 7%",
        )
        .build()?;
    Ok(EstimatedYear {
        year: 2026,
        threshold: DOLLAR * 1_000,
        rule_threshold: DOLLAR * 500,
        this_year: percent(90),
        prior_year: percent(100),
        dues: vec![
            d(2026, Month::APRIL, 15),
            d(2026, Month::JUNE, 15),
            d(2026, Month::SEPTEMBER, 15),
            d(2027, Month::JANUARY, 15),
        ],
        calendar,
        period_ends: vec![
            d(2026, Month::MARCH, 31),
            d(2026, Month::MAY, 31),
            d(2026, Month::AUGUST, 31),
            d(2026, Month::DECEMBER, 31),
        ],
        applicable: vec![Rate(2250), percent(45), Rate(6750), percent(90)],
        late_days: 30,
        low: percent(2),
        high: percent(10),
        interest,
    })
}
