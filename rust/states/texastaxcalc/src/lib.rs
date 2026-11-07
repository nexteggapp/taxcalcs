//! Answers the shared estimated tax questions for Texas, which has no
//! individual income tax. Ported from the Go package `texastaxcalc`.
//!
//! Texas's constitution, as amended by H.J.R. 38 and adopted by the voters on
//! November 5, 2019, bars a tax on the net incomes of individuals.
//!
//! For 2026 the answer is that there is nothing to estimate. Requirement is
//! exempt with no target, Schedule has no due dates, and Penalty is zero. Each
//! result cites the document the finding rests on, which is checked into the
//! Go package's testdata/.
//!
//! Start at [`estimated_tax`].

mod estimated;
mod estimated_y2026;

pub use crate::estimated::estimated_tax;
