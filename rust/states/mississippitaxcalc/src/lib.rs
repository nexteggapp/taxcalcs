//! Computes Mississippi's estimated income tax for 2026: whether estimated
//! payments are needed, what is due by each date, and the interest on an
//! underestimate. Ported from the Go package `mississippitaxcalc`.
//!
//! # Only estimated tax so far
//!
//! There is no income tax calculator here yet. A caller passes the year's
//! liability in as [`estimated::Figures::tax`], Form 80-105 line 20 or Form
//! 80-205 line 22.
//!
//! # How Mississippi differs
//!
//! The current-year test is 80% and the prior-year test is 100%, with no
//! higher tier. Nothing is owed on a liability of $200 or less. All
//! withholding counts toward the April 15 installment. The charge is interest
//! of ½% a month on the running underestimate, from each due date until paid
//! or the next due date, with the last column running to April 15. There is no
//! annualized method, no January 31 rule and no published waiver.
//!
//! # What is not built
//!
//! Farmers and fishermen, who owe nothing if 80% is paid by January 15 or the
//! return is filed and paid by March 1, get [`estimated::Error::Unsupported`].
//!
//! Start at [`estimated_tax`].

mod estimated;
mod estimated_y2026;

pub use crate::estimated::{
    EstimatedFacts, EstimatedFactsBuilder, estimated_tax, new_estimated_facts,
};
