//! Computes Indiana's estimated income tax for 2026: whether estimated
//! payments are needed, what is due by each date, and the underpayment
//! penalty. Ported from the Go package `indianataxcalc`.
//!
//! # Only estimated tax so far
//!
//! There is no income tax calculator here yet. A caller passes this year's
//! state and county tax together as [`estimated::FiguresBuilder::tax`],
//! after credits and before withholding, the pass-through entity tax credit
//! and estimated payments: Schedule IT-2210 line 3. The county part goes in
//! [`estimated::FiguresBuilder::local_tax`] as well, and last year's figure
//! is IT-2210 line 7 before the 110%.
//!
//! # How Indiana differs
//!
//! The tests are the federal ones: 90% of this year's tax or 100% of last
//! year's, 110% above $150,000 of federal AGI, four quarters, and no penalty
//! under $1,000. The penalty is not interest. It is a flat 10% of each
//! quarter's shortfall, the same however late the payment, and a later
//! overpayment that makes up a shortfall does not reduce it. County income
//! tax counts in every figure. A part-year resident divides the yearly
//! figure over only the quarters of residence. From 2026 a filer with no
//! return and no liability last year owes no estimates.
//!
//! # What is not built
//!
//! Farmers and fishermen, who have one due date under IT-2210 Section A, get
//! [`estimated::Error::Unsupported`]; a filer who was one only last year is
//! not detected, because [`estimated::Facts`] carries this year's shares
//! alone. The form's rounding of every entry to whole dollars is not
//! applied: figures are kept to the cent. The rules for nonresident
//! partners' and beneficiaries' withholding in Bulletin #3 are not applied.
//!
//! Start at [`estimated_tax`].

mod estimated;
mod estimated_y2026;

pub use estimated::{EstimatedFacts, EstimatedFactsBuilder, estimated_tax, new_estimated_facts};
