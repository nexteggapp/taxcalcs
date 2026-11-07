//! The filing status a return is filed under, ported from the Go package
//! `filing`.
//!
//! # Why one type serves every jurisdiction here
//!
//! A filing status is not federal law that the states happen to copy. Each
//! state in this module requires the status on its return to be the status on
//! the federal return, by its own statute: California by Rev. & Tax. Code
//! § 18521(b), Oregon by ORS 316.122(1). A taxpayer cannot be single in Salem
//! and married in Washington, D.C., so a separate state enum would be five
//! names for the same five facts and a conversion function that can only be
//! the identity.
//!
//! A jurisdiction whose statuses genuinely differ should declare its own type
//! rather than stretch this one.

use std::fmt;

/// The status a return is filed under.
///
/// Surviving spouse is its own value even though 26 U.S.C. § 1(j)(2)(A) gives
/// it the same federal rate table and § 63(c)(2) the same standard deduction
/// as married filing jointly. The two differ elsewhere — § 63(f)(3) gives an
/// unmarried individual a larger additional deduction for age or blindness,
/// and a surviving spouse is not unmarried for that purpose.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, PartialOrd, Ord, Hash)]
#[repr(i32)]
pub enum Status {
    /// The zero value. Every calculator refuses it, so a caller that forgot
    /// to set a status gets an error rather than a single filer's tax.
    #[default]
    Unspecified = 0,
    /// Single.
    Single = 1,
    /// Married filing jointly.
    MarriedFilingJointly = 2,
    /// Married filing separately.
    MarriedFilingSeparately = 3,
    /// Head of household.
    HeadOfHousehold = 4,
    /// Qualifying surviving spouse.
    SurvivingSpouse = 5,
}

impl Status {
    /// Every status but [`Status::Unspecified`], in declaration order.
    pub const ALL: [Status; 5] = [
        Status::Single,
        Status::MarriedFilingJointly,
        Status::MarriedFilingSeparately,
        Status::HeadOfHousehold,
        Status::SurvivingSpouse,
    ];

    /// The status for Go's integer value, which is also the declaration
    /// order; anything outside 1..=5 is [`Status::Unspecified`].
    pub fn from_i32(v: i32) -> Status {
        match v {
            1 => Status::Single,
            2 => Status::MarriedFilingJointly,
            3 => Status::MarriedFilingSeparately,
            4 => Status::HeadOfHousehold,
            5 => Status::SurvivingSpouse,
            _ => Status::Unspecified,
        }
    }
}

/// Names the status as 26 U.S.C. § 1(j)(2) does.
impl fmt::Display for Status {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(match self {
            Status::Single => "single",
            Status::MarriedFilingJointly => "married filing jointly",
            Status::MarriedFilingSeparately => "married filing separately",
            Status::HeadOfHousehold => "head of household",
            Status::SurvivingSpouse => "surviving spouse",
            Status::Unspecified => "unspecified filing status",
        })
    }
}
