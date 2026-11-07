//! Estimated tax for tax year 2026.
//!
//! The figures come from these documents:
//!
//!   - the 90% and 100% shares and the four due dates: the 2026 IN-114 and
//!     its instructions, and 32 V.S.A. §§ 5851 and 5852;
//!   - the $500 threshold: the 2025 IN-111 instructions, line 31;
//!   - the quarter shares, the 25% of withholding, the April 15 stop, the
//!     365-day basis and the 1% a month: the 2025 IN-152, the latest
//!     published;
//!   - the 7.75% rate for 2026: the Department's memo of November 4, 2025;
//!   - the holidays: the Department's State Holiday Closures page.
//!
//! The Go package's estimated_test.go checks them against the statute
//! extracts in its testdata/ and, for the Department's documents, which
//! tax.vermont.gov would not serve, against the quotes in
//! testdata/research-vt.md. The April 15 stop, the 365-day basis and the
//! holiday dates have no quote there and are not checked against any text.
//! testdata/SOURCES.md lists every such gap.
//!
//! The 2026 IN-152 is not published. The 2025 worksheet's arithmetic is used
//! with the 2026 dates and rate.
//!
//! The rate for 2027 is set under 32 V.S.A. § 3108 by December 15, 2026 and
//! is not published. The rates end on December 31, 2026 and the penalty
//! reports the gap.

use estimated::{Error, Month, must_date as d};
use money::{DOLLAR, Rate, percent};

use crate::estimated::EstimatedYear;

/// Vermont's 2026 figures.
pub(crate) fn year_2026() -> Result<EstimatedYear, Error> {
    // The State Holiday Closures page's 2026 and 2027 columns. Its 2026
    // Thanksgiving row reads "Thursday, November 28" and "Friday, November
    // 29", which are a Saturday and a Sunday in 2026, so those two are left
    // out. No due date is near them.
    let mut cb = estimated::new_calendar(
        "Vermont Department of Taxes, State Holiday Closures (tax.vermont.gov/node/10121), retrieved 2026-10-20",
    );
    for (on, name) in [
        (d(2026, Month::JANUARY, 1), "New Year's Day"),
        (d(2026, Month::JANUARY, 19), "Martin Luther King, Jr. Day"),
        (d(2026, Month::FEBRUARY, 16), "Presidents' Day"),
        (d(2026, Month::MARCH, 3), "Town Meeting Day"),
        (d(2026, Month::MAY, 25), "Memorial Day"),
        (d(2026, Month::JULY, 3), "Independence Day"),
        (d(2026, Month::SEPTEMBER, 7), "Labor Day"),
        (d(2026, Month::OCTOBER, 12), "Indigenous People's Day"),
        (d(2026, Month::NOVEMBER, 11), "Veterans' Day"),
        (d(2026, Month::DECEMBER, 25), "Christmas Day"),
        (d(2027, Month::JANUARY, 1), "New Year's Day"),
        (d(2027, Month::JANUARY, 18), "Martin Luther King, Jr. Day"),
        (d(2027, Month::FEBRUARY, 15), "Presidents' Day"),
        (d(2027, Month::MARCH, 2), "Town Meeting Day"),
        (d(2027, Month::MAY, 31), "Memorial Day"),
        (d(2027, Month::JUNE, 18), "Juneteenth"),
        (d(2027, Month::JULY, 5), "Independence Day"),
        (d(2027, Month::SEPTEMBER, 6), "Labor Day"),
        (d(2027, Month::OCTOBER, 11), "Indigenous People's Day"),
        (d(2027, Month::NOVEMBER, 11), "Veterans' Day"),
        (d(2027, Month::NOVEMBER, 25), "Thanksgiving Day"),
        (d(2027, Month::NOVEMBER, 26), "Day After Thanksgiving"),
        (d(2027, Month::DECEMBER, 24), "Christmas Day"),
    ] {
        cb.holiday(on, name);
    }
    let calendar = cb.build()?;
    let rates = estimated::new_rate_series()
        .period(
            d(2026, Month::JANUARY, 1),
            d(2026, Month::DECEMBER, 31),
            Rate(775),
            "Vermont Department of Taxes, 2026 Interest Rate memo, November 4, 2025: 7.75% for 2026 under 32 V.S.A. § 3108",
        )
        .build()?;
    let quarter = percent(25);
    Ok(EstimatedYear {
        year: 2026,
        this_year: percent(90),
        prior_year: percent(100),
        threshold: DOLLAR * 500,
        shares: vec![quarter, quarter, quarter, quarter],
        dues: vec![
            d(2026, Month::APRIL, 15),
            d(2026, Month::JUNE, 15),
            d(2026, Month::SEPTEMBER, 15),
            d(2027, Month::JANUARY, 15),
        ],
        calendar,
        stop: d(2027, Month::APRIL, 15),
        rates,
        day_basis: 365,
        monthly: percent(1),
    })
}
