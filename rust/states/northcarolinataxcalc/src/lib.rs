//! Package northcarolinataxcalc computes North Carolina's estimated income
//! tax for 2026: whether estimated payments are needed, what is due by each
//! date, and the interest on underpaid estimated tax. Ported from the Go
//! package `northcarolinataxcalc`.
//!
//! # Only estimated tax so far
//!
//! There is no income tax calculator here yet. A caller passes the tax in as
//! [`estimated::Figures::tax`], Form D-422 line 3: Form D-400 line 15 less
//! the credits on line 16.
//!
//! # How North Carolina differs
//!
//! There is no 110% tier, and the no-liability exception has no twelve-month
//! or residence condition. Nothing waives the interest for a casualty,
//! retirement or disability. The rate is set by the Secretary of Revenue for
//! each half-year, 7% for 2026. A return filed and paid by January 31 counts
//! as paid on January 15.
//!
//! # What is not built
//!
//! Farmers and fishermen, who make one installment by January 15 or file and
//! pay by March 1, get [`estimated::Error::Unsupported`]. Estates and trusts
//! are outside the section. North Carolina has no local income tax.
//!
//! Start at [`estimated_tax`].

// The Go file estimated.go. A module named `estimated` would shadow the
// crate of that name.
mod estimated_y2026;
mod rules;

pub use rules::{EstimatedFacts, EstimatedFactsBuilder, estimated_tax, new_estimated_facts};
