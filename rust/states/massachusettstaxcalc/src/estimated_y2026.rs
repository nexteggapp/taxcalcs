//! Estimated tax for tax year 2026.
//!
//! Every figure here is checked by the Go package's estimated_test.go against
//! an extract in its testdata/, listed in testdata/SOURCES.md:
//!
//!   - the $400 threshold, the due dates, the weekend and holiday rule, the
//!     late-start dates and the 2026 surtax threshold: the 2026 Form 1-ES;
//!   - the 80% and 100% tests, the $400 exception, the line arithmetic, the
//!     withholding rule, the April 15 stop and the annualised percentages and
//!     periods: Form M-2210 for 2025, the latest final form, and the draft
//!     2026 Form M-2210, which prints the same arithmetic with the 2026
//!     dates;
//!   - the late-start groups: AP 241 § 241.3, which agrees with the 1-ES;
//!   - the rates, 7% for the second quarter of 2026 and 8% for the third and
//!     fourth: TIR 26-5, which the draft 2026 M-2210 lines 20 to 22 repeat;
//!   - the statute side of every rule: M.G.L. c. 62B §§ 13 and 14 and c. 62C
//!     § 32(a);
//!   - the holidays: M.G.L. c. 4 § 7, clause Eighteenth.
//!
//! The rate from January 1, 2027 is not published; the draft 2026 M-2210
//! prints "X%". The rates end on December 31, 2026 and the penalty reports
//! the gap.
//!
//! No 2026 due date falls on a weekend or a Massachusetts legal holiday, so
//! none moves. Patriots' Day is April 20, 2026 and Bunker Hill Day June 17.
//! DOR's estimated tax web page (updated 06/18/2026) gives the second date as
//! June 16, 2026; the statute, the 1-ES and the draft M-2210 all say June 15,
//! a Monday, which is used.
//!
//! No act in 2025 or 2026 amended c. 62B §§ 13 or 14 or c. 62C § 32
//! (research/estimated-payments/ma.md), so no form here predates the law.

use estimated::{Error, Month, must_date as d};
use money::{DOLLAR, percent};

use crate::rules::EstimatedYear;

pub(crate) fn year_2026() -> Result<EstimatedYear, Error> {
    // Clause Eighteenth's legal holidays from the first due date to the
    // last. A holiday on a Sunday moves to the Monday; July 4, 2026 is a
    // Saturday and stays. Bunker Hill Day is a holiday in Suffolk county
    // only.
    let calendar =
        estimated::new_calendar("M.G.L. c. 4 § 7, clause Eighteenth, April 2026 to January 2027")
            .holiday(
                d(2026, Month::APRIL, 20),
                "the third Monday in April (Patriots' Day)",
            )
            .holiday(d(2026, Month::MAY, 25), "the last Monday in May")
            .holiday(
                d(2026, Month::JUNE, 17),
                "Bunker Hill Day, June seventeenth, Suffolk county only",
            )
            .holiday(d(2026, Month::JUNE, 19), "June nineteenth")
            .holiday(d(2026, Month::JULY, 4), "July fourth")
            .holiday(
                d(2026, Month::SEPTEMBER, 7),
                "the first Monday in September",
            )
            .holiday(d(2026, Month::OCTOBER, 12), "the second Monday in October")
            .holiday(d(2026, Month::NOVEMBER, 11), "November eleventh")
            .holiday(d(2026, Month::NOVEMBER, 26), "Thanksgiving Day")
            .holiday(d(2026, Month::DECEMBER, 25), "Christmas Day")
            .holiday(d(2027, Month::JANUARY, 1), "January first")
            .holiday(d(2027, Month::JANUARY, 18), "the third Monday in January")
            .build()?;

    let rates = estimated::new_rate_series()
        .period(
            d(2026, Month::APRIL, 1),
            d(2026, Month::JUNE, 30),
            percent(7),
            "TIR 26-5 (09/01/2026): 7%, second quarter of 2026; draft 2026 Form M-2210 line 20",
        )
        .period(
            d(2026, Month::JULY, 1),
            d(2026, Month::SEPTEMBER, 30),
            percent(8),
            "TIR 26-5 (09/01/2026): 8%, third quarter of 2026; draft 2026 Form M-2210 line 21",
        )
        .period(
            d(2026, Month::OCTOBER, 1),
            d(2026, Month::DECEMBER, 31),
            percent(8),
            "TIR 26-5 (09/01/2026): 8%, fourth quarter of 2026; draft 2026 Form M-2210 line 22",
        )
        .build()?;

    Ok(EstimatedYear {
        year: 2026,
        floor: DOLLAR * 400,
        this_year_share: percent(80),
        prior_year_share: percent(100),
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
        stop: d(2027, Month::APRIL, 15),
        last_charged: d(2027, Month::APRIL, 14),
        period_ends: vec![
            d(2026, Month::MARCH, 31),
            d(2026, Month::MAY, 31),
            d(2026, Month::AUGUST, 31),
            d(2026, Month::DECEMBER, 31),
        ],
        applicable: vec![percent(20), percent(40), percent(60), percent(80)],
        rates,
    })
}
