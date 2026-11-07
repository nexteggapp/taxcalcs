//! Estimated tax for tax year 2026. Ported from estimated_y2026.go.
//!
//! Every figure here is checked by the Go package's estimated_test.go
//! against an extract in testdata/, listed in testdata/SOURCES.md:
//!
//!   - the $1,000 threshold, the 90% and 100% tests, the due dates, the
//!     late-start chart and the next-business-day rule: Publication
//!     OR-ESTIMATE (2026), with the due dates also in the Form OR-40-V
//!     instructions;
//!   - the worksheet arithmetic, the exceptions, the annualised periods and
//!     percentages, the April 15 stop and the 0.000219 daily rate for 2026:
//!     the Form OR-10 instructions (2025), which are the latest published;
//!   - the 8% rate from January 1, 2026: OAR 150-305-0140;
//!   - the Oregon legal holidays: ORS 187.010;
//!   - the $20,000 gross income figure: ORS 316.563(1).
//!
//! The 2026 Form OR-10 is not published. The 2025 instructions' worksheets
//! are used with the 2026 dates and rate, which those instructions already
//! print.
//!
//! The rate from January 1, 2027 is not published. OAR 150-305-0140 lists
//! rates only through January 1, 2026, so the factors end on December 31,
//! 2026 and the penalty reports the gap.
//!
//! The calendar holds the ORS 187.010 holidays from January 1, 2026 to April
//! 15, 2027. ORS 187.020 adds any day the Governor appoints; none can be
//! listed in advance. None of the four 2026 due dates falls on a weekend or
//! holiday.

use estimated::{Error, Month, Rounding, must_date as d};
use money::{DOLLAR, Rate, percent};

use crate::estimated_rules::{EstimatedYear, validate};

/// The 2026 figures.
pub(crate) fn year_2026() -> Result<EstimatedYear, Error> {
    let calendar = estimated::new_calendar(
        "ORS 187.010 (2025 edition), legal holidays; ORS 187.020 days appointed by the Governor are not listed",
    )
    .holiday(d(2026, Month::JANUARY, 1), "New Year's Day")
    .holiday(
        d(2026, Month::JANUARY, 19),
        "Martin Luther King, Jr.'s Birthday",
    )
    .holiday(d(2026, Month::FEBRUARY, 16), "Presidents Day")
    .holiday(d(2026, Month::MAY, 25), "Memorial Day")
    .holiday(d(2026, Month::JUNE, 19), "Juneteenth")
    .holiday(
        d(2026, Month::JULY, 3),
        "Independence Day, observed on the Friday before",
    )
    .holiday(d(2026, Month::JULY, 4), "Independence Day")
    .holiday(d(2026, Month::SEPTEMBER, 7), "Labor Day")
    .holiday(d(2026, Month::NOVEMBER, 11), "Veterans Day")
    .holiday(d(2026, Month::NOVEMBER, 26), "Thanksgiving Day")
    .holiday(d(2026, Month::DECEMBER, 25), "Christmas Day")
    .holiday(d(2027, Month::JANUARY, 1), "New Year's Day")
    .holiday(
        d(2027, Month::JANUARY, 18),
        "Martin Luther King, Jr.'s Birthday",
    )
    .holiday(d(2027, Month::FEBRUARY, 15), "Presidents Day")
    .build()?;

    let factor = estimated::parse_factor("0.000219")?;
    let daily = estimated::new_factor_series(Rounding::RoundToDollar)
        .period(
            d(2026, Month::JANUARY, 1),
            d(2026, Month::DECEMBER, 31),
            factor,
            percent(8),
            "Form OR-10 instructions (2025), underpayment interest worksheet: 0.000219 a day from 01/01/26; OAR 150-305-0140(1): 8% from January 1, 2026",
        )
        .build()?;

    validate(EstimatedYear {
        year: 2026,
        threshold: DOLLAR * 1_000,
        this_year_share: percent(90),
        prior_year_share: percent(100),
        dues: vec![
            d(2026, Month::APRIL, 15),
            d(2026, Month::JUNE, 15),
            d(2026, Month::SEPTEMBER, 15),
            d(2027, Month::JANUARY, 15),
        ],
        late_starts: vec![
            d(2026, Month::APRIL, 2),
            d(2026, Month::JUNE, 2),
            d(2026, Month::SEPTEMBER, 2),
        ],
        calendar,
        stop: d(2027, Month::APRIL, 15),
        period_ends: vec![
            d(2026, Month::MARCH, 31),
            d(2026, Month::MAY, 31),
            d(2026, Month::AUGUST, 31),
            d(2026, Month::DECEMBER, 31),
        ],
        annualised: vec![Rate(2_250), Rate(4_500), Rate(6_750), Rate(9_000)],
        gross_income_test: DOLLAR * 20_000,
        daily,
    })
}
