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
    /// figures. California indexes its brackets, its standard deduction and
    /// its exemption credits for inflation every year under Rev. & Tax. Code
    /// § 17041(h), so last year's table is wrong in a way that looks right.
    #[error("californiataxcalc: no published rate table for this tax year")]
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

/// One band of a rate schedule: the taxable income it starts above, and the
/// rate on income in it.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct Bracket {
    /// The taxable income this bracket begins above. The lowest bracket has
    /// `over` zero.
    pub over: USCents,
    /// The rate on income in the bracket.
    pub rate: Rate,

    /// The tax on all income below `over`, as the Franchise Tax Board prints
    /// it in the middle column of Schedule X, Y and Z. Held so the test can
    /// check this library's ladder against the published figure rather than
    /// against itself.
    pub base_tax: USCents,
}

/// Every published figure for one tax year and filing status.
///
/// One struct rather than a lookup per figure, because the Franchise Tax
/// Board publishes them together in one booklet and revises them together
/// each year.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Table {
    /// The tax year.
    pub year: i64,
    /// The filing status.
    pub status: Status,

    /// The Rev. & Tax. Code § 17041 rate schedule, lowest bracket first.
    /// Schedule X for a single or separate filer, Schedule Y for a joint
    /// filer or surviving spouse, Schedule Z for a head of household.
    ///
    /// Every kind of income is taxed by this one ladder. See the crate
    /// documentation on why there is no separate schedule for capital gain.
    pub ordinary: Vec<Bracket>,

    /// The Rev. & Tax. Code § 17073.5 amount. It is much smaller than the
    /// federal one, which is why a filer who takes the federal standard
    /// deduction may still itemise in California.
    pub standard_deduction: USCents,

    /// The Rev. & Tax. Code § 17054 credit, per exemption, for the filer, a
    /// spouse, and each additional claim for age or blindness. A credit
    /// against tax rather than a deduction from income, which is why it
    /// cannot be folded into [`crate::taxable_income`].
    pub personal_exemption_credit: USCents,

    /// The § 17054(d) credit for each dependent. It is much larger than the
    /// personal credit.
    pub dependent_exemption_credit: USCents,

    /// The federal adjusted gross income above which the exemption credits
    /// are reduced.
    pub exemption_phase_out_agi: USCents,
    /// The size of the step the reduction counts.
    pub exemption_phase_out_step: USCents,

    /// The reduction per exemption for each whole or part step of AGI above
    /// the threshold.
    pub exemption_phase_out_per_step: USCents,

    /// The taxable income above which the Rev. & Tax. Code § 17043
    /// surcharge applies.
    ///
    /// Named the Mental Health Services Tax until Proposition 1 renamed it
    /// for taxable years beginning on or after 1 January 2025.
    pub behavioral_health_threshold: USCents,
    /// The rate on the excess.
    pub behavioral_health_rate: Rate,
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
/// Adding a year is a deliberate commit, which is what stops a stale build
/// applying last year's law to this year's income.
pub fn table_for(year: i64, status: Status) -> Result<Table, Error> {
    if status == Status::Unspecified {
        return Err(Error::Invalid(
            "californiataxcalc: no filing status given".to_string(),
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
