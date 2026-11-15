//! The shared shape of estimated tax: the facts a jurisdiction's rules read,
//! the answers they give, and the arithmetic several jurisdictions do the
//! same way. Ported from the Go package `estimated`.
//!
//! It holds no jurisdiction's rules. Those are code, one implementation per
//! jurisdiction, in that jurisdiction's own crate: the federal rules in
//! ustaxcalc, California's in californiataxcalc. What is shared is the
//! question each implementation answers, which is [`EstimatedTax`], and the
//! helpers: interest accrued daily, compounded or by the month, or charged at
//! a factor a form prints; due dates moved for weekends and a holiday
//! [`Calendar`]; cumulative-share schedules; the annualised installment
//! recapture; and applying payments to installments in the order the penalty
//! forms do.
//!
//! # Every value is immutable
//!
//! No struct here has a public field. Each is read through accessors that
//! return copies of any vector, and built through a builder whose `build`
//! validates. A jurisdiction's rules therefore never see facts that `build`
//! would have refused, and a caller cannot change an answer after it was
//! computed.
//!
//! # From Go
//!
//! Go's `NewX` constructors are `new_x` functions, and builder methods take
//! `&mut self` and return `&mut Self`. A Go constant of an enumerated type,
//! as `StatusMet`, is an enum variant, as [`Status::Met`]; Go's `(value,
//! bool)` accessors return an [`Option`]; Go's errors are [`Error`], whose
//! [`Error::is`] is `errors.Is`.
//!
//! # Dependencies
//!
//! The standard library, num-bigint, and this workspace's money, filing and
//! working crates. Nothing else, and no protobuf.
//!
//! See docs/estimated-tax.md for the design.

use std::borrow::Cow;
use std::fmt;

mod bigfloat;
mod date;
mod factor;
mod facts;
mod interest;
mod output;
mod schedule;
mod waiver;

pub use date::{
    Calendar, CalendarBuilder, Date, Holiday, Month, Weekday, date_of, must_date, new_calendar,
    new_date,
};
pub use factor::{
    Factor, FactorPeriod, FactorSeries, FactorSeriesBuilder, Rounding, daily_factor,
    new_factor_series, parse_factor, span_factor,
};
pub use facts::{
    Elections, ElectionsBuilder, Facts, FactsBuilder, Figures, FiguresBuilder, Payment,
    PaymentBuilder, Period, PeriodBuilder, Person, PersonBuilder, Positions, PositionsBuilder,
    PriorReturn, PriorReturnBuilder, ResidencePeriod, ResidencePeriodBuilder, WaiverCause,
    WaiverKind, Withheld, WithheldBuilder, new_elections, new_facts, new_figures, new_payment,
    new_period, new_person, new_positions, new_prior_return, new_residence_period, new_withheld,
    resident_all_year,
};
pub use interest::{
    RatePeriod, RateSeries, RateSeriesBuilder, compound_daily, monthly, months_or_part,
    new_rate_series, simple_daily,
};
pub use output::{
    Basis, Explanation, ExplanationBuilder, Installment, InstallmentBuilder, PartKind, Penalty,
    PenaltyBuilder, PenaltyLine, PenaltyLineBuilder, PenaltyPart, PenaltyPartBuilder, Requirement,
    RequirementBuilder, Status, TargetOption, TargetOptionBuilder, exception_applies,
    exception_at_most, exception_below, leaves, left, new_exception, new_explanation,
    new_installment, new_penalty, new_penalty_line, new_penalty_part, new_requirement,
    new_target_option, status_of, without_this_year_tax,
};
pub use schedule::{
    Column, LedgerEntry, Piece, annualised, apply, columns, credits, credits_as_withheld,
    cumulative_shares, equal_shares, increments, late_start, move_dates, simple_penalty,
};
pub use waiver::{
    WaiverOutcome, standard_waiver, waiver_eligible, waiver_eligible_except, waiver_refused,
    waiver_undecided,
};

/// Names a taxing jurisdiction by postal code: "US" for the federal
/// government, "CA" for California.
#[derive(Clone, Debug, Default, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub struct Jurisdiction(Cow<'static, str>);

impl Jurisdiction {
    /// The jurisdiction with this code, which need not be one of the
    /// constants: [`FactsBuilder::build`] is what refuses a bad one.
    pub fn new(code: impl Into<String>) -> Jurisdiction {
        Jurisdiction(Cow::Owned(code.into()))
    }

    /// The jurisdiction with this code, in a constant.
    pub const fn from_static(code: &'static str) -> Jurisdiction {
        Jurisdiction(Cow::Borrowed(code))
    }

    /// The postal code, as Go's `string(j)`.
    pub fn as_str(&self) -> &str {
        &self.0
    }
}

/// Renders the postal code.
impl fmt::Display for Jurisdiction {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(&self.0)
    }
}

macro_rules! jurisdictions {
    ($($(#[$doc:meta])* $name:ident = $code:literal,)*) => {
        $( $(#[$doc])* pub const $name: Jurisdiction = Jurisdiction(Cow::Borrowed($code)); )*

        /// Returns every jurisdiction: the federal government first, then
        /// the states and the District of Columbia in alphabetical order of
        /// their names.
        pub fn jurisdictions() -> Vec<Jurisdiction> {
            vec![$($name),*]
        }
    };
}

jurisdictions! {
    /// The federal government.
    FEDERAL = "US",
    /// Alabama.
    ALABAMA = "AL",
    /// Alaska.
    ALASKA = "AK",
    /// Arizona.
    ARIZONA = "AZ",
    /// Arkansas.
    ARKANSAS = "AR",
    /// California.
    CALIFORNIA = "CA",
    /// Colorado.
    COLORADO = "CO",
    /// Connecticut.
    CONNECTICUT = "CT",
    /// Delaware.
    DELAWARE = "DE",
    /// The District of Columbia.
    DISTRICT_OF_COLUMBIA = "DC",
    /// Florida.
    FLORIDA = "FL",
    /// Georgia.
    GEORGIA = "GA",
    /// Hawaii.
    HAWAII = "HI",
    /// Idaho.
    IDAHO = "ID",
    /// Illinois.
    ILLINOIS = "IL",
    /// Indiana.
    INDIANA = "IN",
    /// Iowa.
    IOWA = "IA",
    /// Kansas.
    KANSAS = "KS",
    /// Kentucky.
    KENTUCKY = "KY",
    /// Louisiana.
    LOUISIANA = "LA",
    /// Maine.
    MAINE = "ME",
    /// Maryland.
    MARYLAND = "MD",
    /// Massachusetts.
    MASSACHUSETTS = "MA",
    /// Michigan.
    MICHIGAN = "MI",
    /// Minnesota.
    MINNESOTA = "MN",
    /// Mississippi.
    MISSISSIPPI = "MS",
    /// Missouri.
    MISSOURI = "MO",
    /// Montana.
    MONTANA = "MT",
    /// Nebraska.
    NEBRASKA = "NE",
    /// Nevada.
    NEVADA = "NV",
    /// New Hampshire.
    NEW_HAMPSHIRE = "NH",
    /// New Jersey.
    NEW_JERSEY = "NJ",
    /// New Mexico.
    NEW_MEXICO = "NM",
    /// New York.
    NEW_YORK = "NY",
    /// North Carolina.
    NORTH_CAROLINA = "NC",
    /// North Dakota.
    NORTH_DAKOTA = "ND",
    /// Ohio.
    OHIO = "OH",
    /// Oklahoma.
    OKLAHOMA = "OK",
    /// Oregon.
    OREGON = "OR",
    /// Pennsylvania.
    PENNSYLVANIA = "PA",
    /// Rhode Island.
    RHODE_ISLAND = "RI",
    /// South Carolina.
    SOUTH_CAROLINA = "SC",
    /// South Dakota.
    SOUTH_DAKOTA = "SD",
    /// Tennessee.
    TENNESSEE = "TN",
    /// Texas.
    TEXAS = "TX",
    /// Utah.
    UTAH = "UT",
    /// Vermont.
    VERMONT = "VT",
    /// Virginia.
    VIRGINIA = "VA",
    /// Washington.
    WASHINGTON = "WA",
    /// West Virginia.
    WEST_VIRGINIA = "WV",
    /// Wisconsin.
    WISCONSIN = "WI",
    /// Wyoming.
    WYOMING = "WY",
}

/// An error from this package or from a jurisdiction's rules. Its text is
/// the Go error's, and [`Error::is`] answers as Go's `errors.Is` does.
#[derive(Clone, Debug, PartialEq, Eq, thiserror::Error)]
pub enum Error {
    /// Go's `ErrNoRules`: no estimated tax rules are built for a
    /// jurisdiction and year. It is never answered with an adjacent year's
    /// rules.
    #[error("estimated: no estimated tax rules for this jurisdiction and year")]
    NoRules,

    /// Go's `ErrUnsupported`: facts that put a household under rules this
    /// build does not implement, such as the separate rules for farmers and
    /// fishermen. An error rather than an answer under the wrong rules.
    #[error("estimated: these facts need rules this build does not implement")]
    Unsupported,

    /// Go's `ErrNeedsThisYearTax`: a figure cannot be worked out without
    /// this year's tax. Schedule and Penalty return it when no option for
    /// the target can be measured without it, and a jurisdiction whose
    /// penalty is measured against this year's tax in every case, as
    /// Pennsylvania's is, returns it from Penalty when the answer depends on
    /// that tax. Requirement never returns it: it reports the question in
    /// Required instead.
    #[error("estimated: this figure needs this year's tax")]
    NeedsThisYearTax,

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

/// Builds an [`Error::Invalid`] from a format string, as Go's `fmt.Errorf`
/// without `%w`.
macro_rules! invalid {
    ($($arg:tt)*) => { $crate::Error::Invalid(format!($($arg)*)) };
}
pub(crate) use invalid;

/// The [`Error::NoRules`] a jurisdiction's constructor returns for a year it
/// does not carry, worded as every Go constructor words it:
/// `fmt.Errorf("%w: %s %d", estimated.ErrNoRules, j, year)`.
pub fn no_rules(j: &Jurisdiction, year: i64) -> Error {
    Error::with_detail(Error::NoRules, format!("{j} {year}"))
}

/// What an option measured on this year's tax says when the tax was not
/// given.
pub const NEEDS_THIS_YEAR_TAX: &str = "Needs this year's tax";

/// A rule on which a form and its statute disagree. It is working's type, so
/// that [`working::Result`] can carry it.
pub use working::Divergence;

/// Starts a [`Divergence`] on the named rule.
pub fn new_divergence(rule: impl Into<String>) -> working::DivergenceBuilder {
    working::new_divergence(rule)
}

/// One jurisdiction's estimated tax rules for one tax year.
///
/// Every method returns the figure with its working: `applied` cites each
/// rule in the caller's numbers, `required` lists what the caller has not
/// said and the default used instead, and `divergences` lists where the
/// jurisdiction's form and statute disagree on a rule that was used.
pub trait EstimatedTax {
    /// Whether estimated payments are needed, and what total keeps the
    /// household clear of the penalty.
    fn requirement(&self, f: &Facts) -> Result<working::Result<Requirement>, Error>;

    /// Each due date, what is needed by it, and what has been credited by
    /// it.
    fn schedule(&self, f: &Facts) -> Result<working::Result<Vec<Installment>>, Error>;

    /// The underpayment penalty accrued up to `as_of`, or up to the date the
    /// rules stop it, or up to [`Facts::return_filed`], whichever is
    /// earliest. Every payment in the facts counts on its own date, so
    /// planned payments and a later `as_of` give the penalty if the plan is
    /// kept.
    fn penalty(&self, f: &Facts, as_of: Date) -> Result<working::Result<Penalty>, Error>;

    /// How this jurisdiction's estimated tax works, in plain words.
    fn explanation(&self) -> Explanation;
}

impl<T: EstimatedTax + ?Sized> EstimatedTax for Box<T> {
    fn requirement(&self, f: &Facts) -> Result<working::Result<Requirement>, Error> {
        (**self).requirement(f)
    }
    fn schedule(&self, f: &Facts) -> Result<working::Result<Vec<Installment>>, Error> {
        (**self).schedule(f)
    }
    fn penalty(&self, f: &Facts, as_of: Date) -> Result<working::Result<Penalty>, Error> {
        (**self).penalty(f, as_of)
    }
    fn explanation(&self) -> Explanation {
        (**self).explanation()
    }
}

impl<T: EstimatedTax + ?Sized> EstimatedTax for &T {
    fn requirement(&self, f: &Facts) -> Result<working::Result<Requirement>, Error> {
        (**self).requirement(f)
    }
    fn schedule(&self, f: &Facts) -> Result<working::Result<Vec<Installment>>, Error> {
        (**self).schedule(f)
    }
    fn penalty(&self, f: &Facts, as_of: Date) -> Result<working::Result<Penalty>, Error> {
        (**self).penalty(f, as_of)
    }
    fn explanation(&self) -> Explanation {
        (**self).explanation()
    }
}
