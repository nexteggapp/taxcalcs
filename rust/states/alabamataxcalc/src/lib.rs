//! Package alabamataxcalc computes Alabama's estimated individual income tax
//! for 2026: whether estimated payments are needed, what is due by each date,
//! and the underpayment penalty. Ported from the Go package
//! `alabamataxcalc`.
//!
//! # Only estimated tax so far
//!
//! There is no income tax calculator here yet. A caller passes the year's tax
//! in as [`estimated::Figures::tax`]: Form 2210AL Part I line 3, which is the
//! net tax after credits (Form 40 line 18, or Form 40NR line 20) less the
//! other payments on Schedule CP, Section B, line 1. Last year's Alabama AGI
//! goes in the prior return's [`estimated::Figures::state_agi`].
//!
//! # How Alabama differs
//!
//! Code of Ala. § 40-18-80 adopts the federal rules, 26 U.S.C. § 6654, with a
//! $500 threshold in place of $1,000. The department's rate follows the
//! federal underpayment rate each quarter. The forms leave out three rules
//! the statute keeps: the January 31 file-and-pay rule, the exception for a
//! year with no liability, and the rules for nonresident aliens. The figures
//! follow the forms, and each result's divergences say where the statute
//! reads otherwise.
//!
//! # What is not built
//!
//! Farmers and fishermen, who file Form 2210AL Part IV, get
//! [`estimated::Error::Unsupported`]. Local occupational taxes are not
//! covered; no source found shows that any of them takes estimated payments.
//!
//! Start at [`estimated_tax`].

// The Go file estimated.go. A module named `estimated` would shadow the
// crate of that name.
mod estimated_y2026;
mod rules;

pub use rules::{EstimatedFacts, EstimatedFactsBuilder, estimated_tax, new_estimated_facts};
