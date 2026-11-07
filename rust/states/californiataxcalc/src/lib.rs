//! Package californiataxcalc computes California personal income tax.
//! Ported from the Go package `californiataxcalc`.
//!
//! It is a library for writing tax software. A caller supplies figures and
//! gets back a figure, the published rule that produced it, and any choice
//! the caller still owes.
//!
//! # It does not import the federal calculators
//!
//! California starts from federal adjusted gross income, and this crate takes
//! that as a plain [`money::USCents`] argument rather than as a federal
//! result type. Somebody holding the figure from a spreadsheet, a prior-year
//! return or another library can use these calculators, and nothing here
//! drags in the federal rate tables.
//!
//! # California has no capital gains rate
//!
//! Rev. & Tax. Code § 18031 conforms to the federal definition of gross
//! income but not to 26 U.S.C. § 1(h), so a long-term gain is taxed at the
//! same rates as wages. There is deliberately no long-term gains tax here. A
//! caller must fold the whole gain into ordinary taxable income, and
//! [`taxable_income`] does that when given it.
//!
//! # Rates are data, and a missing year is an error
//!
//! Every published figure is in a table keyed by tax year and filing status.
//! A year with no table is [`Error::NoTable`] rather than the nearest year's
//! figures. [`years`] reports what this build carries.
//!
//! # Estimated tax
//!
//! [`estimated_tax`] answers the shared estimated tax questions for
//! California, with [`EstimatedFacts`] for the facts only California reads,
//! and [`worksheet_i`] works FTB 5805 Worksheet I for a Form 540NR filer.
//!
//! Start at [`taxable_income`], [`ordinary_income_tax`] and
//! [`exemption_credit`].

// The Go file estimated.go. A module named `estimated` would shadow the
// crate of that name.
mod estimated_y2026;
mod income;
mod rules;
mod tables;
mod tax;
mod worksheet_i;
mod y2025;

pub use income::{
    Adjustments, Election, Exemptions, Income, Residency, exemption_credit, taxable_income,
};
pub use rules::{EstimatedFacts, EstimatedFactsBuilder, estimated_tax, new_estimated_facts};
pub use tables::{Bracket, Error, Table, table_for, years};
pub use tax::{Band, Ladder, behavioral_health_services_tax, effective_rate, ordinary_income_tax};
pub use worksheet_i::{
    WorksheetIColumn, WorksheetIColumnBuilder, WorksheetILines, new_worksheet_i_column, worksheet_i,
};
