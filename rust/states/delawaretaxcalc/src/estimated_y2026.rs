//! Estimated tax for tax year 2026.
//!
//! Every figure here is checked by the Go package's estimated_test.go
//! against an extract in its testdata/, listed in testdata/SOURCES.md:
//!
//!   - the $800 threshold, the 90%, 100% and 110% tests, the $150,000 and
//!     $75,000 steps, the four due dates and the late-start dates: PIT-EST
//!     for 2026 (revised 11/10/25) and its voucher, and the 2026 payment
//!     periods the 2025 PIT-RES instructions print;
//!   - the line arithmetic, the $800 stop on line E, the 0.0005 daily
//!     factor, the April 30 stop, the withholding periods, the annualised
//!     percentages and the Short Method's 12%: PIT-UND for 2025 (revision
//!     20250724) and its instructions (revised 11/10/25), which are the
//!     latest published.
//!
//! The 2026 PIT-UND is not published. The 2025 form's arithmetic is used
//! with the 2026 dates. Its line 39 prints 5/1/25 for the first installment,
//! the day after April 30, and the due date itself for the other three; the
//! 2026 dates here follow the same pattern, so the first is May 1, 2026.
//! Line 40 stops at April 30, 2026, the 2025 return's due date; for 2026
//! that is April 30, 2027, the date 30 Del. C. § 1168 sets.
//!
//! The 0.0005 a day is fixed by the form, and the 1 1/2% a month behind it
//! by 30 Del. C. § 535(b). Neither is set by a notice, so one period covers
//! every day a 2026 penalty can run, to April 30, 2027.
//!
//! No Delaware holiday list is checked in. The four 2026 due dates the forms
//! print and April 30, 2027 are weekdays, and the test checks the computed
//! dates against the printed ones.

use estimated::{Error, Month, Rounding, must_date as d};
use money::{DOLLAR, Rate, percent};

use crate::estimated::EstimatedYear;

/// Delaware's 2026 figures.
pub(crate) fn year_2026() -> Result<EstimatedYear, Error> {
    let calendar =
        estimated::new_calendar("weekends only; no Delaware holiday list is checked in").build()?;
    // Line 40's stop: April 30 of the next year, the return's due date.
    let stop = d(2027, Month::APRIL, 30);
    let factor = estimated::parse_factor("0.0005")?;
    // 1 1/2% a month is 18% over twelve months; the form's daily factor is
    // 1 1/2% over 30 days.
    let daily = estimated::new_factor_series(Rounding::RoundToCent)
        .period(
            d(2026, Month::JANUARY, 1),
            stop,
            factor.clone(),
            percent(18),
            "PIT-UND (2025), lines 40 to 42: 0.0005 a day; 30 Del. C. § 535(b): 1 1/2% a month",
        )
        .build()?;
    Ok(EstimatedYear {
        year: 2026,
        threshold: DOLLAR * 800,
        this_year_percent: percent(90),
        prior_percent: percent(100),
        prior_percent_high: percent(110),
        prior_year_step: DOLLAR * 150_000,
        prior_year_step_separate: DOLLAR * 75_000,
        dues: vec![
            d(2026, Month::APRIL, 30),
            d(2026, Month::JUNE, 15),
            d(2026, Month::SEPTEMBER, 15),
            d(2027, Month::JANUARY, 15),
        ],
        line39: vec![
            d(2026, Month::MAY, 1),
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
        late_starts: vec![
            d(2026, Month::APRIL, 2),
            d(2026, Month::JUNE, 2),
            d(2026, Month::SEPTEMBER, 2),
        ],
        stop,
        applicable: vec![Rate(2_250), Rate(4_500), Rate(6_750), Rate(9_000)],
        daily,
        daily_factor: factor,
        short_method_rate: percent(12),
    })
}
