//! The published tables, by tax year and filing status. Ported from
//! tables.go.

use std::collections::BTreeMap;
use std::sync::LazyLock;

use filing::Status;
use money::{Rate, USCents};

/// An error from the income tax calculators. Its text is the Go error's,
/// and [`Error::is`] answers as Go's `errors.Is` does.
#[derive(Clone, Debug, PartialEq, Eq, thiserror::Error)]
pub enum Error {
    /// Go's `ErrNoTable`: no published table is held for a tax year.
    ///
    /// Calculators return this rather than applying an adjacent year's
    /// figures. Oregon indexes its brackets, its standard deduction and its
    /// exemption credit every year under ORS 316.011, so last year's table is
    /// wrong in a way that looks right.
    #[error("oregontaxcalc: no published rate table for this tax year")]
    NoTable,

    /// Another error wrapped with Go's `%w`: the whole message, and the
    /// error it wraps.
    #[error("{text}")]
    Wrapped {
        /// The wrapped error.
        err: Box<Error>,
        /// The whole message.
        text: String,
    },

    /// Any other error, as Go's `errors.New`.
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

    /// `err` with `detail` after it, as Go's `fmt.Errorf("%w: ...", err)`.
    fn with_detail(err: Error, detail: impl std::fmt::Display) -> Error {
        let text = format!("{err}: {detail}");
        Error::Wrapped {
            err: Box::new(err),
            text,
        }
    }
}

/// One band of the rate chart: the taxable income it starts above, and the
/// rate on income in it.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct Bracket {
    /// The taxable income this bracket begins above. The lowest bracket has
    /// `over` zero.
    pub over: USCents,
    /// The rate on income in the bracket.
    pub rate: Rate,
}

/// One row of the Table 4 phase-out: the federal adjusted gross income it
/// applies from, and the largest federal tax subtraction allowed there.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct SubtractionStep {
    /// The federal adjusted gross income this row starts at. The first row
    /// starts at zero.
    pub at_least: USCents,

    /// The largest subtraction allowed. Zero on the last row, where the
    /// subtraction is gone entirely.
    pub maximum: USCents,
}

/// Every published figure for one tax year and filing status.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Table {
    /// The tax year.
    pub year: i64,
    /// The filing status.
    pub status: Status,

    /// The ORS 316.037 rate chart, lowest bracket first. Chart S for a single
    /// or separate filer, Chart J for everybody else.
    pub ordinary: Vec<Bracket>,

    /// The ORS 316.695(9) amount before any addition for age or blindness.
    ///
    /// A separate filer whose spouse itemises gets nothing at all, which is
    /// not in this figure because it depends on what the spouse did. See
    /// [`crate::Deduction::spouse_itemises`].
    pub standard_deduction: USCents,

    /// The addition per qualifying condition. Oregon gives an unmarried
    /// filer more, the same way the federal rules do, so this differs by
    /// status rather than being one figure.
    pub additional_aged_blind: USCents,

    /// The ORS 316.085 credit per exemption.
    pub exemption_credit: USCents,

    /// The federal adjusted gross income above which the exemption credit is
    /// nothing at all.
    ///
    /// A cliff rather than a taper: a dollar over the limit costs the whole
    /// credit for every exemption.
    pub exemption_credit_agi_limit: USCents,

    /// The ORS 316.680(1)(b) phase-out, lowest income first. Oregon is one of
    /// the few states that lets a filer subtract part of their federal
    /// income tax.
    pub federal_tax_subtraction: Vec<SubtractionStep>,
}

/// Every year and status this build carries.
static TABLES: LazyLock<BTreeMap<i64, BTreeMap<Status, Table>>> = LazyLock::new(|| {
    let mut t = BTreeMap::new();
    t.insert(2025, crate::y2025::tables());
    t
});

/// Returns the published figures for a tax year and filing status.
///
/// It reports [`Error::NoTable`] for a year this build does not carry.
pub fn table_for(year: i64, status: Status) -> Result<Table, Error> {
    if status == Status::Unspecified {
        return Err(Error::Invalid(
            "oregontaxcalc: no filing status given".to_string(),
        ));
    }
    let Some(by_status) = TABLES.get(&year) else {
        return Err(Error::with_detail(Error::NoTable, year));
    };
    match by_status.get(&status) {
        Some(t) => Ok(t.clone()),
        None => Err(Error::with_detail(
            Error::NoTable,
            format!("{year} has no table for {status}"),
        )),
    }
}

/// Returns every tax year this build carries a table for. Go's order is
/// unspecified; this one is ascending.
pub fn years() -> Vec<i64> {
    TABLES.keys().copied().collect()
}
