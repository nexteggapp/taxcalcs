//! Estimated tax for tax year 2026.
//!
//! Every figure here is checked by the Go package's estimated_test.go against
//! an extract in its testdata/, listed in testdata/SOURCES.md:
//!
//!   - the $500 threshold, the 90% and 100% tests, the 25% installments, the
//!     late-start shares, the dates, the January rule, the annualized
//!     percentages and the April 15 stop: Mont. Code Ann. § 15-30-2512 and
//!     the 2026 Publication 1;
//!   - the line arithmetic, the exceptions, the periods and the 0.000192
//!     daily factor for 7%: Form EST-I for 2025, whose 2026 edition is not
//!     published;
//!   - the holidays: Mont. Code Ann. § 1-1-216, with § 1-1-307 moving a day
//!     that falls on one or on a Saturday;
//!   - the rate: § 15-1-216(7)(a)(i) takes the federal underpayment rate for
//!     the third quarter of the preceding year, which Rev. Rul. 2026-10 sets
//!     at 7% for the quarter from July 1, 2026. Publication 1 says tax year
//!     2026 uses the 2027 rate. The department has not yet set the 2027 rate
//!     by rule, and the penalty says so.
//!
//! The calendar lists the § 1-1-216 holidays from January 2026 to April 2027
//! except state general election day, whose date another title sets; no due
//! date here is near November 3, 2026.

use estimated::{Error, Month, Rounding, must_date as d};
use money::{DOLLAR, Rate, percent};

use crate::rules::EstimatedYear;

pub(crate) fn year_2026() -> Result<EstimatedYear, Error> {
    let calendar = estimated::new_calendar("Mont. Code Ann. § 1-1-216")
        .holiday(d(2026, Month::JANUARY, 1), "New Year's Day")
        .holiday(d(2026, Month::JANUARY, 19), "Martin Luther King Jr. Day")
        .holiday(d(2026, Month::FEBRUARY, 16), "Presidents' Day")
        .holiday(d(2026, Month::MAY, 25), "Memorial Day")
        .holiday(
            d(2026, Month::JULY, 3),
            "Independence Day (Saturday, so the Friday before)",
        )
        .holiday(d(2026, Month::SEPTEMBER, 7), "Labor Day")
        .holiday(
            d(2026, Month::OCTOBER, 12),
            "Indigenous Peoples' Day and Columbus Day",
        )
        .holiday(d(2026, Month::NOVEMBER, 11), "Veterans' Day")
        .holiday(d(2026, Month::NOVEMBER, 26), "Thanksgiving Day")
        .holiday(d(2026, Month::DECEMBER, 25), "Christmas Day")
        .holiday(d(2027, Month::JANUARY, 1), "New Year's Day")
        .holiday(d(2027, Month::JANUARY, 18), "Martin Luther King Jr. Day")
        .holiday(d(2027, Month::FEBRUARY, 15), "Presidents' Day")
        .build()?;

    let factor = estimated::parse_factor("0.000192")?;
    let daily = estimated::new_factor_series(Rounding::RoundToCent)
        .period(
            d(2026, Month::JANUARY, 1),
            d(2027, Month::APRIL, 15),
            factor,
            percent(7),
            "Form EST-I (2025): 0.000192 a day at 7%; Mont. Code Ann. § 15-1-216(7)(a)(i) and Rev. Rul. 2026-10: 7% for 2027",
        )
        .build()?;

    Ok(EstimatedYear {
        year: 2026,
        threshold: DOLLAR * 500,
        this_year_share: percent(90),
        prior_year_share: percent(100),
        shares: vec![percent(25), percent(25), percent(25), percent(25)],
        annualised: vec![Rate(2250), Rate(4500), Rate(6750), Rate(9000)],
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
        file_and_pay_by: d(2027, Month::JANUARY, 31),
        stop: d(2027, Month::APRIL, 15),
        daily,
    })
}
