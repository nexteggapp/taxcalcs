//! Wyoming for tax year 2026.
//!
//! The finding is checked by the tests against the extracts in the Go
//! package's testdata/, listed in testdata/SOURCES.md:
//!
//!   - ws-title39-ch7.txt: Wyoming Statutes title 39, chapter 7, from the
//!     Legislature's compiled title.
//!   - wyo-gov-about.txt: the State of Wyoming's About Wyoming page.

use crate::estimated::{EstimatedYear, Finding};

/// Wyoming's 2026 finding.
pub(crate) fn year_2026() -> EstimatedYear {
    EstimatedYear {
        year: 2026,
        why: "Wyoming has no individual income tax. There is no Wyoming income tax to estimate for 2026. No estimated payments are required.",
        basis: vec![
            Finding {
                cite: "Wyo. Stat. tit. 39, ch. 7, Income Taxes",
                says: "title 39 reads: \"CHAPTER 7 - INCOME TAXES 39-7-101. Repealed By Laws 1998, ch. 5, § 4.\" Wyoming levies no income tax.",
            },
            Finding {
                cite: "State of Wyoming, About Wyoming",
                says: "the state says: \"Wyoming does not possess an individual or corporate income tax.\" There is no Wyoming income tax to estimate.",
            },
        ],
    }
}
