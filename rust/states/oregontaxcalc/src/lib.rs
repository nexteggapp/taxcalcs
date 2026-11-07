//! oregontaxcalc computes Oregon personal income tax, and Oregon's estimated
//! tax and its underpayment interest. Ported from the Go package
//! `oregontaxcalc`.
//!
//! It is a library for writing tax software. A caller supplies figures and
//! gets back a figure, the published rule that produced it, and any choice
//! the caller still owes.
//!
//! # It does not import the federal calculators
//!
//! Oregon starts from federal adjusted gross income and subtracts part of the
//! federal tax itself, so it needs two federal figures. Both are plain
//! [`money::USCents`] arguments rather than a federal result type. Somebody
//! holding them from a spreadsheet or another library can use these
//! calculators.
//!
//! # Oregon rounds each bracket to a whole dollar
//!
//! This is the thing most likely to surprise. Oregon's published figures are
//! not the exact arithmetic: the tax in each bracket is rounded to the
//! nearest whole dollar and the rounded amounts are summed. A joint filer at
//! $50,000 owes $3,756, where the exact sum is $3,755.00 and rounding the
//! exact sum gives $3,755. Both published rate charts and all 502 bands of
//! the published tax table agree with the per-bracket rule and disagree with
//! the other two, which is what [`ordinary_income_tax`] implements. See the
//! Go package's tables_test.go.
//!
//! # Oregon has no capital gains rate
//!
//! ORS 316.048 taxes Oregon taxable income at one set of rates, so a
//! long-term gain is taxed like wages. There is deliberately no
//! `long_term_gains_tax` here.
//!
//! # Rates are data, and a missing year is an error
//!
//! Every published figure is in a table keyed by tax year and filing status.
//! A year with no table is [`Error::NoTable`] rather than the nearest year's
//! figures. [`years`] reports what this build carries.
//!
//! # Estimated tax
//!
//! [`estimated_tax`] is Oregon's estimated tax rules for 2026, as the shared
//! [`estimated::EstimatedTax`] trait: whether payments are needed, what is
//! due by each date, and the underpayment interest. The prior-year test is
//! 100% at every income. Farmers and fishers with two-thirds of their gross
//! income from it owe nothing. Due dates move for Oregon's own legal
//! holidays, which [`estimated_calendar`] returns. Form OR-10 charges interest
//! on a running balance from one event to the next, each line rounded to a
//! whole dollar, and that is what is computed. The caller passes the tax in
//! as [`estimated::Figures::tax`], after all credits, with Oregon's own facts
//! in [`EstimatedFacts`].
//!
//! Part-year residents get estimated.ErrUnsupported in the Go package's own
//! words; the code divides their required annual payment over their periods
//! of residence. Form OR-10's Exception 5, for a first-year S corporation
//! shareholder, is not applied, and a change of filing status between the two
//! years is left to the caller, who passes last year's tax already combined
//! or allocated.
//!
//! Start at [`taxable_income`], [`ordinary_income_tax`] and
//! [`federal_tax_subtraction`], or at [`estimated_tax`].

mod estimated_rules;
mod estimated_y2026;
mod income;
mod tables;
mod tax;
mod y2025;

pub use estimated_rules::{
    EstimatedFacts, EstimatedFactsBuilder, estimated_calendar, estimated_tax, new_estimated_facts,
    worksheet_interest,
};
pub use income::{
    Adjustments, Conditions, Deduction, Election, Income, Residency, standard_deduction,
    taxable_income,
};
pub use tables::{Bracket, Error, SubtractionStep, Table, table_for, years};
pub use tax::{
    Band, Ladder, effective_rate, exemption_credit, federal_tax_subtraction, ordinary_income_tax,
};
