//! Estimated tax for tax year 2026.
//!
//! Every figure here is checked by the Go package's estimated_test.go
//! against an extract in its testdata/, listed in testdata/SOURCES.md:
//!
//!   - the $1,000 filing test, the $11,950, $23,900 and $11,950 VAGI floors,
//!     the due dates, the late-start groups, the weekend and holiday rule,
//!     the March 1 rule and the $1,000 small-underpayment exception: the 2026
//!     Form 760ES (Rev. 09/25);
//!   - the $1,000 exception again, the 90% and 100% shares and the May 1
//!     stop: Code of Virginia § 58.1-492, as amended by 2026 Acts c. 7, and
//!     the VAGI floors again: § 58.1-321;
//!   - the line arithmetic, the exceptions' percentages, the period ends, the
//!     90% of Exception 4 and the .00025 daily rate for 9%: Form 760C for
//!     2025 and its instructions, which are the latest published;
//!   - the rates, 8% from April 1 to June 30, 2026 and 9% from July 1 to
//!     December 31, 2026: Tax Bulletins 26-2, 26-3 and 26-4;
//!   - the holidays: § 2.2-3300.
//!
//! The 2026 Form 760C is not published. The 2025 form's arithmetic is used
//! with the 2026 dates, and with the $1,000 the 2026 law sets in place of the
//! 2025 form's $150.
//!
//! The 2025 form prints .00025 a day for 9% and no factor for 8%. Days at 9%
//! are charged at .00025, as the form does. Days at 8% are charged at 8%
//! times the days over 365, as 23VAC10-112-42 writes the computation, and the
//! result reports it.
//!
//! Under § 58.1-15(B), days from January 1 to May 1, 2027 take the rate for
//! March 2027, which is not published. The rates end on December 31, 2026
//! and the penalty reports the gap.
//!
//! No 2026 due date moves. May 1, June 15 and September 15, 2026 and January
//! 15, 2027 are weekdays and none is a Virginia legal holiday. March 1, 2027,
//! the last day of the March 1 rule, is a Monday. May 1, 2027 is a Saturday,
//! but it ends the period of the underpayment under § 58.1-492(B)(1) rather
//! than being a day to file or pay, so it is not moved.

use estimated::{Error, Month, must_date as d};
use money::{DOLLAR, Rate, percent};

use crate::estimated::{EstimatedYear, new_rate_period};

/// The 2026 figures.
pub(crate) fn year_2026() -> Result<EstimatedYear, Error> {
    // § 2.2-3300, January 2026 to April 2027. July 4, 2026 is a Saturday, so
    // the Friday before is the holiday. Days the Governor or the President
    // appoints are not known in advance and are not listed.
    let calendar = estimated::new_calendar(
        "Code of Virginia § 2.2-3300, applied by Form 760ES (2026), Section II",
    )
    .holiday(d(2026, Month::JANUARY, 1), "New Year's Day")
    .holiday(d(2026, Month::JANUARY, 19), "Martin Luther King, Jr., Day")
    .holiday(d(2026, Month::FEBRUARY, 16), "George Washington Day")
    .holiday(d(2026, Month::MAY, 25), "Memorial Day")
    .holiday(d(2026, Month::JUNE, 19), "Juneteenth")
    .holiday(d(2026, Month::JULY, 3), "Independence Day, observed")
    .holiday(d(2026, Month::SEPTEMBER, 7), "Labor Day")
    .holiday(
        d(2026, Month::OCTOBER, 12),
        "Columbus Day and Yorktown Victory Day",
    )
    .holiday(d(2026, Month::NOVEMBER, 3), "Election Day")
    .holiday(d(2026, Month::NOVEMBER, 11), "Veterans Day")
    .holiday(d(2026, Month::NOVEMBER, 26), "Thanksgiving Day")
    .holiday(
        d(2026, Month::NOVEMBER, 27),
        "the Friday after Thanksgiving",
    )
    .holiday(d(2026, Month::DECEMBER, 25), "Christmas Day")
    .holiday(d(2027, Month::JANUARY, 1), "New Year's Day")
    .holiday(d(2027, Month::JANUARY, 18), "Martin Luther King, Jr., Day")
    .holiday(d(2027, Month::FEBRUARY, 15), "George Washington Day")
    .build()?;

    Ok(EstimatedYear {
        year: 2026,
        this_year_share: percent(90),
        prior_year_share: percent(100),
        filing_threshold: DOLLAR * 1_000,
        small_underpayment: DOLLAR * 1_000,
        agi_single: DOLLAR * 11_950,
        agi_joint: DOLLAR * 23_900,
        agi_separate: DOLLAR * 11_950,
        dues: vec![
            d(2026, Month::MAY, 1),
            d(2026, Month::JUNE, 15),
            d(2026, Month::SEPTEMBER, 15),
            d(2027, Month::JANUARY, 15),
        ],
        later_groups: vec![
            d(2026, Month::APRIL, 16),
            d(2026, Month::JUNE, 2),
            d(2026, Month::SEPTEMBER, 2),
        ],
        calendar,
        file_and_pay_by: d(2027, Month::MARCH, 1),
        stop: d(2027, Month::MAY, 1),
        period_ends: vec![
            d(2026, Month::APRIL, 30),
            d(2026, Month::MAY, 31),
            d(2026, Month::AUGUST, 31),
            d(2026, Month::DECEMBER, 31),
        ],
        exception_shares: vec![percent(25), percent(50), percent(75), percent(100)],
        annualised: vec![Rate(2250), Rate(4500), Rate(6750)],
        income_to_date: percent(90),
        rates: vec![
            new_rate_period(
                d(2026, Month::APRIL, 1),
                d(2026, Month::JUNE, 30),
                percent(8),
                "",
                "Tax Bulletin 26-2 (March 10, 2026): 8% for the second quarter of 2026",
            )?,
            new_rate_period(
                d(2026, Month::JULY, 1),
                d(2026, Month::DECEMBER, 31),
                percent(9),
                "0.00025",
                "Tax Bulletins 26-3 and 26-4: 9% for the third and fourth quarters of 2026; Form 760C (2025), line 23: .00025 a day for 9%",
            )?,
        ],
    })
}
