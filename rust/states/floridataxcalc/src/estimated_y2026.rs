//! Florida for tax year 2026.
//!
//! The finding is checked by the Go package's estimated_test.go against the
//! extracts in its testdata/, listed in testdata/SOURCES.md:
//!
//!   - dor-fl-gt800025.txt: the Department of Revenue's GT-800025, Tax
//!     Information for New Residents (R. 08/25).

use crate::rules::{EstimatedYear, Finding};

pub(crate) fn year_2026() -> EstimatedYear {
    EstimatedYear {
        year: 2026,
        why: "Florida has no personal income tax. There is no Florida income tax to estimate for 2026. No estimated payments are required.",
        basis: vec![Finding {
            cite: "Florida Department of Revenue, Tax Information for New Residents, GT-800025 (R. 08/25)",
            says: "the Department of Revenue says: \"Florida does not impose personal income tax, inheritance tax, gift taxes, or tax on intangible personal property.\" There is no Florida income tax to estimate.",
        }],
    }
}
