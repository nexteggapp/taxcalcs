//! Nevada for tax year 2026.
//!
//! The finding is checked by the tests against the extracts in the Go
//! package's testdata/, listed in testdata/SOURCES.md:
//!
//!   - nv-const-art10-sec1-9.txt: the Nevada Constitution, Article 10,
//!     Section 1(9), from the Legislature's site.

use crate::estimated::{EstimatedYear, Finding};

/// Nevada's finding for 2026.
pub(crate) fn year_2026() -> EstimatedYear {
    EstimatedYear {
        year: 2026,
        why: "Nevada's constitution bars an income tax on wages or personal income. There is no Nevada income tax to estimate for 2026. No estimated payments are required.",
        basis: &[Finding {
            cite: "Nev. Const. art. 10, § 1(9)",
            says: "the constitution says: \"No income tax shall be levied upon the wages or personal income of natural persons.\" There is no Nevada income tax to estimate.",
        }],
    }
}
