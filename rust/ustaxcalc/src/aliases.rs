//! The money, filing status and result types used to be declared in the Go
//! package, and are now declared in money, filing and working so that the
//! state calculators can use them without importing the federal rules. This
//! module is what keeps them reachable from here.
//!
//! # These are aliases, not wrappers
//!
//! An alias makes two names for one type, so a [`Rate`] and a
//! [`money::Rate`] are the same type. Nothing converts, nothing can drift,
//! and a caller can mix the two names freely.
//!
//! # One name did change
//!
//! USD is gone, replaced by [`USCents`]. The type has always held cents, so
//! USD(50_000) read as fifty thousand dollars and meant five hundred.

pub use money::{Rate, USCents};
pub use working::{PositionKind, Required, Rule};

/// One dollar, for writing an amount the way it is spoken.
pub const DOLLAR: USCents = money::DOLLAR;

/// The amount for a whole number of dollars.
pub fn dollars(d: i64) -> USCents {
    money::dollars(d)
}

/// The rate for a whole number of percent.
pub fn percent(p: i32) -> Rate {
    money::percent(p)
}

/// The status a return is filed under.
pub type FilingStatus = filing::Status;

/// No status; every calculator refuses it.
pub const FILING_STATUS_UNSPECIFIED: FilingStatus = filing::Status::Unspecified;
/// Single.
pub const SINGLE: FilingStatus = filing::Status::Single;
/// Married filing jointly.
pub const MARRIED_FILING_JOINTLY: FilingStatus = filing::Status::MarriedFilingJointly;
/// Married filing separately.
pub const MARRIED_FILING_SEPARATELY: FilingStatus = filing::Status::MarriedFilingSeparately;
/// Head of household.
pub const HEAD_OF_HOUSEHOLD: FilingStatus = filing::Status::HeadOfHousehold;
/// Qualifying surviving spouse.
pub const SURVIVING_SPOUSE: FilingStatus = filing::Status::SurvivingSpouse;

/// A computed figure, the rules that produced it, and the positions the
/// caller still owes.
pub type Result<T> = working::Result<T>;

/// No position; every builder refuses it.
pub const POSITION_UNSPECIFIED: PositionKind = PositionKind::Unspecified;
/// Whether to take the standard deduction or itemise.
pub const POSITION_DEDUCTION_METHOD: PositionKind = PositionKind::DeductionMethod;
/// Which lots a sale consumed.
pub const POSITION_LOT_SELECTION: PositionKind = PositionKind::LotSelection;
/// Whether a traditional IRA contribution is deductible.
pub const POSITION_IRA_DEDUCTIBILITY: PositionKind = PositionKind::IraDeductibility;
/// Whether a security bought inside the wash-sale window is substantially
/// identical to the one sold.
pub const POSITION_SUBSTANTIALLY_IDENTICAL: PositionKind = PositionKind::SubstantiallyIdentical;
/// Whether an activity is a trade or business.
pub const POSITION_TRADE_OR_BUSINESS: PositionKind = PositionKind::TradeOrBusiness;
/// Whether a dependent qualifies.
pub const POSITION_DEPENDENT_QUALIFIES: PositionKind = PositionKind::DependentQualifies;
