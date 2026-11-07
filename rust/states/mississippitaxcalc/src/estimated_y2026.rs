//! Estimated tax for tax year 2026.
//!
//! Every figure here is checked by the Go package's estimated_test.go
//! against an extract in its testdata/, listed in testdata/SOURCES.md:
//!
//!   - the $200 threshold, the 80% and 100% tests, the quarter installments,
//!     withholding in column A, the ½% a month and the months each column
//!     runs: Form 80-320 (Rev. 09/25) and its worked example;
//!   - the due dates: Rule 100(2) and the quarter table in Form 80-100 (2025);
//!   - the ½% rate: Miss. Code § 27-7-327(e), as amended by Laws 2014, H.B.
//!     799.
//!
//! No 2026 Form 80-320 is published. The 2025 form's arithmetic is used with
//! the 2026 dates. April 15, 2027 ends column D, as April 15 ends it in the
//! 2025 example's three months from January 15.
//!
//! The dates are not moved: no Mississippi source says whether a due date on
//! a weekend or holiday moves, and every date here is a weekday.

use estimated::{Month, must_date as d};
use money::{DOLLAR, Rate, percent};

use crate::estimated::EstimatedYear;

/// The 2026 figures.
pub(crate) fn year_2026() -> EstimatedYear {
    EstimatedYear {
        year: 2026,
        threshold: DOLLAR * 200,
        this_year_share: percent(80),
        prior_year_share: percent(100),
        shares: vec![percent(25), percent(25), percent(25), percent(25)],
        withholding_shares: vec![percent(100), Rate(0), Rate(0), Rate(0)],
        dues: vec![
            d(2026, Month::APRIL, 15),
            d(2026, Month::JUNE, 15),
            d(2026, Month::SEPTEMBER, 15),
            d(2027, Month::JANUARY, 15),
        ],
        stop: d(2027, Month::APRIL, 15),
        monthly: Rate(50),
        rate_source: "Miss. Code § 27-7-327(e); Form 80-320 (Rev. 09/25), line 9: ½% a month",
    }
}
