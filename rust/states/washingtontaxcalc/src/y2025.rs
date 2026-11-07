//! Tax year 2025.
//!
//! The standard deduction and the charitable donation figures are the
//! Department of Revenue's own capital gains tax page, which is where the
//! inflation-adjusted amounts for a year are published. The 2.9% surcharge
//! is ESSB 5813, chapter 421 of the Laws of 2025, as the Department
//! summarised that session. The extracts are in the Go package's testdata,
//! and its tables_test.go checks every figure below against them.
//!
//! The 7% base rate is RCW 82.87.040(1)(a) and has not moved since the tax
//! was created by ESSB 5096 in 2021, so it is not indexed and appears in no
//! annual publication.

use money::{DOLLAR, Rate, percent};

use crate::tables::Table;

pub(crate) fn table_2025() -> Table {
    Table {
        year: 2025,

        standard_deduction: DOLLAR * 278_000,

        base_rate: percent(7),

        surcharge_threshold: DOLLAR * 1_000_000,
        surcharge_rate: Rate(290), // 2.9%

        charitable_deduction_threshold: DOLLAR * 278_000,
        charitable_deduction_cap: DOLLAR * 111_000,
    }
}
