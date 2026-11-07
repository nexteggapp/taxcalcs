//! Idaho for tax year 2026.
//!
//! No figure here is an estimated tax figure, because Idaho has none for
//! individuals. They are the extension prepayment test and the interest rate
//! the Explanation describes. Each is checked by the Go package's
//! estimated_test.go against an extract in its testdata/, listed in
//! testdata/SOURCES.md:
//!
//!   - the 80% and 100% tests, the $50 minimum and the 2% a month penalty:
//!     Idaho Code § 63-3033, and Form 51, which states the same tests for the
//!     2025 extension; no edition for the 2026 return is published, and
//!     § 63-3033 was last amended in 2014;
//!   - the 6% interest rate for 2027, the year a 2026 balance unpaid at the
//!     due date accrues in: the Tax Commission's Interest Rates page;
//!   - that individuals owe no estimated payments: Idaho Code § 63-3036A and
//!     the 2025 Form 40 instructions.

use money::{DOLLAR, percent};

use crate::estimated::EstimatedYear;

pub(crate) fn year_2026() -> EstimatedYear {
    EstimatedYear {
        year: 2026,
        extension_this_year: percent(80),
        extension_prior_year: percent(100),
        extension_minimum: DOLLAR * 50,
        extension_penalty: percent(2),
        interest: percent(6),
    }
}
