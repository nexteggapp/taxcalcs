//! Package massachusettstaxcalc computes Massachusetts's estimated personal
//! income tax for 2026: whether estimated payments are needed, what is due by
//! each date, and the underpayment penalty. Ported from the Go package
//! `massachusettstaxcalc`.
//!
//! # Only estimated tax so far
//!
//! There is no income tax calculator here yet. A caller passes the year's
//! Massachusetts tax after credits, including the 4% surtax, as
//! [`estimated::Figures::tax`].
//!
//! # How Massachusetts differs
//!
//! The required annual payment is the lesser of 80% of this year's tax and
//! 100% of last year's, with no 110% tier. The threshold is $400. The penalty
//! is simple interest at a rate set each quarter, and the annualised method
//! uses 20%, 40%, 60% and 80%. There is no January 31 file-and-pay relief.
//!
//! # What is left open
//!
//! No source settles whether a requirement first met after March 31 lowers
//! the earlier installments. The department's reading is used and reported in
//! Required; [`EstimatedFacts`] can choose the statute's. Whether a part-year
//! prior return opens the 100% test is reported the same way.
//!
//! # What is not built
//!
//! Farmers and fishermen, who owe 66 2/3% and no penalty after filing and
//! paying by March 1, get [`estimated::Error::Unsupported`]. Fiduciaries
//! (Form M-2210F) and fiscal years are not covered.
//!
//! Start at [`estimated_tax`].

// The Go file estimated.go. A module named `estimated` would shadow the
// crate of that name.
mod estimated_y2026;
mod rules;

pub use rules::{
    EstimatedFacts, EstimatedFactsBuilder, LateStart, estimated_tax, new_estimated_facts,
};
