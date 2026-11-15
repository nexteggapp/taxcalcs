//! United States federal income tax, ported from the Go package
//! `ustaxcalc`.
//!
//! It is a library for writing tax software. A caller supplies figures and
//! gets back a figure, the published rule that produced it, and any choice
//! the caller still owes.
//!
//! # Facts and positions
//!
//! Part of a tax figure is arithmetic on numbers. The rest depends on a
//! choice the numbers do not determine: whether to itemise, which lots a sale
//! consumed, whether a traditional IRA contribution is deductible given
//! workplace-plan coverage, whether two securities are substantially
//! identical for the wash-sale rule, whether an activity is a trade or
//! business.
//!
//! A calculator never picks one of those silently. It either takes the
//! position as an argument or returns it in [`working::Result::required`],
//! and the caller decides what that means. A tax product turns it into a
//! question. A preparer's script reads it from a configuration. Something
//! else renders a form field.
//!
//! # Rates are data, and a missing year is an error
//!
//! Every published figure is in a table keyed by tax year and filing status.
//! A year with no table is [`Error::NoTable`] rather than the nearest year's
//! figures, because a table that is right in December is wrong in January
//! and a silent fallback produces a confident number under the wrong law.
//!
//! # Money
//!
//! [`USCents`] is an integer count of cents and [`Rate`] is an integer count
//! of basis points, so nothing here is a binary float. Tax figures are
//! summed, and 0.10 has no exact float representation.
//!
//! Start at [`ordinary_income_tax`], [`long_term_gains_tax`],
//! [`self_employment_tax`] and [`required_annual_payment`].
//! [`net_capital_gains`] nets Schedule D, and [`tax_on_income`] puts
//! qualified dividends and net capital gain through the gains rates and the
//! rest through the bands.
//!
//! # From Go
//!
//! Go's exported names are snake_case functions and CamelCase types, with
//! acronyms in Rust's style (`IRAContribution` is [`IraContribution`],
//! `SECA` is [`Seca`]). Builders take `&mut self` and return `&mut Self`.
//! Go's `time.Time` arguments and fields are `estimated::Date`. There is
//! no `init`: the year tables are built on first use. The gRPC service
//! `ustaxcalcsvc` is not ported.

use std::fmt;

mod additional;
mod aliases;
mod brackets;
mod capgains;
mod capital;
mod deductions;
mod estimated;
mod estimated_y2026;
mod ira;
mod itemised;
mod safeharbour;
mod seca;
mod tables;
mod y2025;
mod y2026;

pub use self::estimated::estimated_tax;
pub use additional::{
    RetirementDistribution, StateRefund, StateRefundBuilder, distribution_income,
    early_distribution_tax, early_distribution_tax_other, handled_distribution_codes,
    new_retirement_distribution, new_state_refund, rental_loss_allowed, state_refund_taxable,
    unhandled_distribution_codes,
};
pub use aliases::*;
pub use brackets::{Band, Ladder, effective_rate, ordinary_income_tax};
pub use capgains::{GainsTax, long_term_gains_tax};
pub use capital::{
    CapitalGains, CapitalGainsBuilder, CapitalNet, TaxWithGains, aged_65_or_older,
    net_capital_gains, new_capital_gains, tax_on_income,
};
pub use deductions::{Conditions, Deduction, Election, deduction_taken, standard_deduction};
pub use ira::{
    IraContribution, IraContributionBuilder, IraLimits, ira_contribution_limits, ira_deduction,
    new_ira_contribution,
};
pub use itemised::itemised_limits;
pub use safeharbour::{
    Instalment, SafeHarbour, instalments, required_annual_payment, withholding_credited_by,
};
pub use seca::{Seca, self_employment_tax};
pub use tables::{Bracket, Table, table_for, years};

/// An error from a calculator here. Its text is the Go error's, and
/// [`Error::is`] answers as Go's `errors.Is` does.
#[derive(Clone, Debug, PartialEq, Eq, thiserror::Error)]
pub enum Error {
    /// Go's `ErrNoTable`: no published table is held for a tax year.
    ///
    /// Calculators return this rather than applying an adjacent year's
    /// figures. A table that is right in December is wrong in January, and
    /// a silent fallback produces a confident number under the wrong law.
    #[error("ustaxcalc: no published rate table for this tax year")]
    NoTable,

    /// Another error wrapped with Go's `%w`: the whole message, and the
    /// error it wraps, which [`Error::is`] sees through.
    #[error("{text}")]
    Wrapped {
        /// The wrapped error.
        err: Box<Error>,
        /// The whole message.
        text: String,
    },

    /// Any other error, as Go's `errors.New` or `fmt.Errorf` without `%w`.
    #[error("{0}")]
    Invalid(String),

    /// An error from a working builder.
    #[error(transparent)]
    Working(#[from] working::Error),
}

impl Error {
    /// Reports whether `self` is `target` or wraps it, as Go's `errors.Is`.
    pub fn is(&self, target: &Error) -> bool {
        if self == target {
            return true;
        }
        match self {
            Error::Wrapped { err, .. } => err.is(target),
            _ => false,
        }
    }

    /// `err` wrapped in a message, as Go's `fmt.Errorf` with `%w`; `text` is
    /// the whole message.
    pub fn wrap(err: Error, text: impl Into<String>) -> Error {
        Error::Wrapped {
            err: Box::new(err),
            text: text.into(),
        }
    }

    /// `err` with `detail` after it, as Go's `fmt.Errorf("%w: ...", err)`.
    pub fn with_detail(err: Error, detail: impl fmt::Display) -> Error {
        let text = format!("{err}: {detail}");
        Error::wrap(err, text)
    }
}

/// An error from these calculators met inside the estimated tax rules keeps
/// its text; a working error stays one.
impl From<Error> for ::estimated::Error {
    fn from(e: Error) -> Self {
        match e {
            Error::Working(w) => ::estimated::Error::Working(w),
            e => ::estimated::Error::Invalid(e.to_string()),
        }
    }
}

/// Builds an [`Error::Invalid`] from a format string, as Go's `fmt.Errorf`
/// without `%w` or `errors.New`.
macro_rules! invalid {
    ($($arg:tt)*) => { $crate::Error::Invalid(format!($($arg)*)) };
}
pub(crate) use invalid;
