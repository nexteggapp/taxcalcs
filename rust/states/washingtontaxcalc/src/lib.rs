//! Computes Washington's capital gains excise tax. Ported from the Go
//! package `washingtontaxcalc`.
//!
//! # Washington has no personal income tax
//!
//! That is the whole reason this crate is shaped differently from its
//! siblings. There is no ordinary income tax here, no rate table by filing
//! status, no standard deduction against wages, and no taxable income
//! function. Not because they are unimplemented — because Washington does not
//! levy the tax they would compute. A stub returning zero would be worse than
//! nothing: it would let a caller loop over three states, get a number back
//! from each, and never learn that one of those numbers means something
//! different.
//!
//! What Washington does levy is an excise tax on the sale or exchange of
//! long-term capital assets, under chapter 82.87 RCW. It is an excise tax on
//! a transaction rather than a tax on income, which is the distinction the
//! Washington Supreme Court turned on in Quinn v. State when it upheld the
//! tax in 2023. [`excise_tax`] computes it.
//!
//! # Two rates since 2025
//!
//! The tax was a flat 7% from its creation. ESSB 5813, chapter 421 of the
//! Laws of 2025, added 2.9% on gains above one million dollars with effect
//! from 1 January 2025, so a large gain now meets two rates.
//!
//! # What a caller must work out first
//!
//! [`excise_tax`] takes Washington capital gains, which is a narrower thing
//! than a federal Schedule D total. Real estate is exempt, so are retirement
//! accounts, timber, commercial fishing privileges and depreciable business
//! assets; and only gains allocated to Washington count at all. This crate
//! does not do that filtering, and returns the allocation question in
//! [`working::Result::required`] rather than pretending it did.
//!
//! # Paying it: one payment, no quarterly estimates
//!
//! [`estimated_tax`] is Washington's side of the shared
//! [`estimated::EstimatedTax`] trait, for tax year 2026. There are no
//! quarterly estimates. The whole tax is due by the federal return's due
//! date, April 15, 2027, and can be paid from October 15, 2026. A late
//! payment owes 9%, 19% or 29% of the late tax by lateness, and interest from
//! April 30, 2027. A filer with an extension who paid an estimate on time
//! owes no late payment penalty, but owes 5% of the whole tax if the estimate
//! was under 80% of it and $1,000 or more short. Whether there was an
//! extension goes in [`EstimatedFacts`].
//!
//! Not built: the late filing penalty of RCW 82.87.110(6), an overpayment of
//! last year's tax applied forward ([`estimated::Error::Unsupported`]), and
//! the income tax that chapter 82A RCW imposes from 2028.
//!
//! Start at [`excise_tax`] for the tax and [`estimated_tax`] for paying it.

mod estimated;
mod estimated_y2026;
mod excise;
mod tables;
mod y2025;

pub use estimated::{EstimatedFacts, EstimatedFactsBuilder, estimated_tax, new_estimated_facts};
pub use excise::{Deductions, ExciseTaxResult, Facts, excise_tax};
pub use tables::{Error, Table, table_for, years};
