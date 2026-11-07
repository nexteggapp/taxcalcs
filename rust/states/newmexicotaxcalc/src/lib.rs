//! Computes New Mexico's estimated personal income tax for 2026: whether
//! estimated payments are needed, what is due by each date, and the
//! underpayment penalty. Ported from the Go package `newmexicotaxcalc`.
//!
//! # Only estimated tax so far
//!
//! There is no income tax calculator here yet. A caller passes the tax in as
//! [`estimated::FiguresBuilder::tax`], RPD-41272 line 4: PIT-1 line 22 less
//! the rebates and credits on lines 24 and 25.
//!
//! # How New Mexico differs
//!
//! No penalty unless tax less withholding is $1,000 or more, and no 110%
//! tier. First-year residents owe no estimated tax, and the no-liability
//! exception is for full-year residents. There is no annualized method; a
//! filer first required to pay after March 31 catches up at the next date.
//! The rate is the federal underpayment rate, quarter by quarter, and the
//! first quarter after the year also covers April 1 to 15.
//!
//! # What is not built
//!
//! Farmers and ranchers, who pay once by January 15 or file and pay by March
//! 1, get [`estimated::Error::Unsupported`]. New Mexico has no local income
//! tax.
//!
//! Start at [`estimated_tax`].

mod estimated;
mod estimated_y2026;

pub use estimated::{EstimatedFacts, EstimatedFactsBuilder, estimated_tax, new_estimated_facts};
