//! westvirginiataxcalc computes West Virginia's estimated personal income
//! tax for 2026: whether estimated payments are needed, what is due by each
//! date, and the underpayment penalty. Ported from the Go package
//! `westvirginiataxcalc`.
//!
//! # Only estimated tax so far
//!
//! There is no income tax calculator here yet. A caller passes the year's
//! tax after credits in as [`estimated::Figures::tax`], Form IT-210 Part I
//! line 3.
//!
//! # How West Virginia differs
//!
//! The rules follow the federal shape with different numbers. Payments are
//! needed once tax after withholding is $600 or more. The target is the
//! smaller of 90% of this year's tax and 100% of last year's, with no 110%
//! tier. A zero tax last year gives a target of zero only while this year's
//! tax is not more than $5,000. The penalty is charged by the day at a factor
//! the Tax Division prints for each calendar year, 0.000315 for 2026.
//!
//! # What is not built
//!
//! Farmers, who make one payment by January 15, get
//! [`estimated::Error::Unsupported`]. IT-210's short method is not built;
//! the regular method is open to every filer. Wage and other withholding
//! cannot be put on their dates separately, because [`estimated::Facts`]
//! does not tell them apart.
//!
//! Start at [`estimated_tax`].

mod estimated;
mod estimated_y2026;

pub use estimated::{EstimatedFacts, EstimatedFactsBuilder, estimated_tax, new_estimated_facts};
