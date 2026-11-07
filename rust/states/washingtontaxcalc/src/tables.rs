//! The published figures for each tax year, and this crate's errors.

use money::{Rate, USCents};

use crate::y2025::table_2025;

/// An error from the excise tax calculator. Its text is the Go error's, and
/// [`Error::is`] answers as Go's `errors.Is` does.
#[derive(Clone, Debug, PartialEq, Eq, thiserror::Error)]
pub enum Error {
    /// Go's `ErrNoTable`: no published table is held for a tax year.
    ///
    /// Calculators return this rather than applying an adjacent year's
    /// figures. The Department of Revenue adjusts the standard deduction for
    /// inflation every year under RCW 82.87.150, so last year's figure is
    /// wrong in a way that looks right.
    #[error("washingtontaxcalc: no published figures for this tax year")]
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
}

/// Every published figure for one tax year.
///
/// Not keyed by filing status, unlike every other jurisdiction in this
/// module. RCW 82.87.060(1) gives one deduction "per individual, married
/// couple, or domestic partnership", so a couple filing jointly get the same
/// amount as one person rather than twice it. Nothing else here varies by
/// status either.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct Table {
    /// The tax year.
    pub year: i64,

    /// The RCW 82.87.060(1) amount, which the statute sets at $250,000 and
    /// RCW 82.87.150 adjusts for inflation each October. Gains below it are
    /// not taxed and no return is due.
    pub standard_deduction: USCents,

    /// The RCW 82.87.040(1)(a) rate on all taxable Washington capital gains.
    pub base_rate: Rate,

    /// The gain above which RCW 82.87.040(1)(b) adds `surcharge_rate` on top
    /// of `base_rate`. Added by ESSB 5813, chapter 421 of the Laws of 2025,
    /// with effect from 1 January 2025.
    ///
    /// Measured against taxable gains after the standard deduction, not
    /// against the gross gain.
    pub surcharge_threshold: USCents,
    /// The RCW 82.87.040(1)(b) rate on gain above `surcharge_threshold`.
    pub surcharge_rate: Rate,

    /// The amount of qualified charitable donations a filer must exceed
    /// before any of them are deductible.
    pub charitable_deduction_threshold: USCents,
    /// The most that may be deducted for charitable donations however much
    /// was given.
    pub charitable_deduction_cap: USCents,
}

/// Returns the published figures for a tax year.
///
/// It reports [`Error::NoTable`] for a year this build does not carry.
pub fn table_for(year: i64) -> Result<Table, Error> {
    match year {
        2025 => Ok(table_2025()),
        _ => Err(Error::Wrapped {
            err: Box::new(Error::NoTable),
            text: format!("{}: {year}", Error::NoTable),
        }),
    }
}

/// Returns every tax year this build carries figures for.
pub fn years() -> Vec<i64> {
    vec![2025]
}
