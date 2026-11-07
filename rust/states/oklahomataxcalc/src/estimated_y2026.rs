//! Estimated tax for tax year 2026. Ported from estimated_y2026.go.
//!
//! Every figure here is checked by the Go package's estimated_test.go
//! against an extract in testdata/, listed in testdata/SOURCES.md:
//!
//!   - the $500 threshold, the 70% and prior-year tests, the due dates, the
//!     weekend and holiday rule, the $1,000 rule and the 20% rate: Form
//!     OW-8-ES (revised 6-2025), for 2026;
//!   - the line arithmetic, the $1,000 and $500 stops, the prior-year stop,
//!     the even withholding, the earliest-first payment order and the
//!     20% ÷ 365 formula: Form OW-8-P, for 2025, which is the latest
//!     published;
//!   - the annualised periods, factors and percentages: Form OW-8-ES-SUP for
//!     2026 and Form OW-8-P-SUP-I for 2025;
//!   - the same rules, the January 31 date, the April 15 stop and the farmer
//!     share: 68 O.S. §§ 2385.7, 2385.9 and 2385.13, in the Legislature's
//!     Title 68 compilation;
//!   - the holidays: 25 O.S. § 82.1, in the Legislature's Title 25
//!     compilation.
//!
//! The 2026 OW-8-P is not published. The 2025 form's arithmetic is used with
//! the 2026 dates from OW-8-ES.
//!
//! The rate is set by statute, not by notice, so every day to the April 15,
//! 2027 stop has one. HB 2730 (2026) would replace it from November 1, 2026;
//! it passed the House and has not been enacted, so it is not used.
//!
//! The holidays are those § 82.1(A) fixes from April 2026 to February 2027.
//! The extra Christmas day for 2026, December 24 or December 28, is chosen by
//! the Governor's Executive Order, which is not checked in, so it is left
//! out; no due date is near it.

use estimated::{Error, Month, must_date as d};
use money::{DOLLAR, Rate, percent};

use crate::estimated_rules::{EstimatedYear, Fraction};

/// The 2026 figures.
pub(crate) fn year_2026() -> Result<EstimatedYear, Error> {
    let calendar =
        estimated::new_calendar("25 O.S. § 82.1(A), (C); OW-8-ES (revised 6-2025), Due Dates")
            .holiday(d(2026, Month::JANUARY, 1), "New Year's Day")
            .holiday(
                d(2026, Month::JANUARY, 19),
                "Martin Luther King, Jr.'s Birthday",
            )
            .holiday(d(2026, Month::FEBRUARY, 16), "Presidents' Day")
            .holiday(d(2026, Month::MAY, 25), "Memorial Day")
            .holiday(d(2026, Month::JULY, 3), "Independence Day, observed")
            .holiday(d(2026, Month::SEPTEMBER, 7), "Labor Day")
            .holiday(d(2026, Month::NOVEMBER, 11), "Veterans' Day")
            .holiday(d(2026, Month::NOVEMBER, 26), "Thanksgiving Day")
            .holiday(
                d(2026, Month::NOVEMBER, 27),
                "the day after Thanksgiving Day",
            )
            .holiday(d(2026, Month::DECEMBER, 25), "Christmas")
            .holiday(d(2027, Month::JANUARY, 1), "New Year's Day")
            .holiday(
                d(2027, Month::JANUARY, 18),
                "Martin Luther King, Jr.'s Birthday",
            )
            .holiday(d(2027, Month::FEBRUARY, 15), "Presidents' Day")
            .build()?;

    let rates = estimated::new_rate_series()
        .period(
            d(2026, Month::JANUARY, 1),
            d(2027, Month::APRIL, 15),
            percent(20),
            "68 O.S. § 2385.13(A): 20% a year, fixed by statute",
        )
        .build()?;

    Ok(EstimatedYear {
        year: 2026,
        threshold: DOLLAR * 500,
        small_tax: DOLLAR * 1_000,
        this_year_share: percent(70),
        prior_year_share: percent(100),
        shares: vec![percent(25), percent(25), percent(25), percent(25)],
        annualised: vec![Rate(1750), Rate(3500), Rate(5250), Rate(7000)],
        period_ends: vec![
            d(2026, Month::MARCH, 31),
            d(2026, Month::MAY, 31),
            d(2026, Month::AUGUST, 31),
            d(2026, Month::DECEMBER, 31),
        ],
        dues: vec![
            d(2026, Month::APRIL, 15),
            d(2026, Month::JUNE, 15),
            d(2026, Month::SEPTEMBER, 15),
            d(2027, Month::JANUARY, 15),
        ],
        calendar,
        file_and_pay_by: d(2027, Month::JANUARY, 31),
        stop: d(2027, Month::APRIL, 15),
        rates,
        farm_share: Fraction { num: 2, den: 3 },
    })
}
