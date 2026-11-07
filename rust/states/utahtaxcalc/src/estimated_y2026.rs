//! Utah's prepayment rules for tax year 2026. Ported from
//! estimated_y2026.go.
//!
//! Every figure here is checked by the Go package's estimated_test.go
//! against an extract in testdata/, listed in testdata/SOURCES.md:
//!
//!   - the 90% and 100% shares: Utah Code § 59-10-516(2)(b) and the TC-546
//!     worksheet (Rev. 10/26);
//!   - the 2% a month, 24% a year by the day and the 12% cap: Publication 58
//!     (Rev. 11/25) and Utah Code § 59-1-401(5)(b);
//!   - the due date: Utah Code § 59-10-514(1)(a)(i) makes the Utah return due
//!     when the federal one is, and the 2025 TC-40 instructions print April 15
//!     for a calendar year, so the 2026 return is due April 15, 2027;
//!   - the end of the extension: six months later, § 59-10-516(1)(b)(i), which
//!     the 2025 instructions print as October 15 for the 2025 return.
//!
//! No 2026 TC-40 instructions are published. The rules did not change for
//! 2026 (research/estimated-payments/ut.md, 2026 changes).
//!
//! No Utah holiday list is checked in. The TC-40 instructions move a due date
//! on a Saturday, Sunday or legal holiday to the next business day. April 15
//! and October 15, 2027 are a Thursday and a Friday, and the test checks
//! neither moves.

use estimated::{Error, Month, must_date as d};
use money::percent;

use crate::estimated_rules::EstimatedYear;

/// The 2026 figures.
pub(crate) fn year_2026() -> Result<EstimatedYear, Error> {
    let calendar =
        estimated::new_calendar("weekends only; no Utah holiday list is checked in").build()?;
    Ok(EstimatedYear {
        year: 2026,
        this_year: percent(90),
        prior_year: percent(100),
        due: d(2027, Month::APRIL, 15),
        extension_ends: d(2027, Month::OCTOBER, 15),
        calendar,
        penalty: percent(24),
        penalty_cap: percent(12),
        days_in_year: 365,
    })
}
