//! The published tables, by tax year and filing status.

use std::collections::BTreeMap;
use std::sync::OnceLock;

use crate::{Error, FilingStatus, Rate, USCents, invalid};

/// Bracket is one band of the rate table: the threshold it starts above, and
/// the rate on income in it.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Hash)]
pub struct Bracket {
    /// The taxable income this bracket begins above. The lowest bracket has
    /// `over` zero.
    pub over: USCents,
    /// The rate on income in this bracket.
    pub rate: Rate,
}

/// Every published figure for one tax year and filing status.
///
/// One struct rather than a lookup per figure, because they are published
/// together in one revenue procedure and revised together each year.
#[derive(Clone, Debug, Default, PartialEq, Eq, Hash)]
pub struct Table {
    /// The tax year.
    pub year: i64,
    /// The filing status.
    pub status: FilingStatus,

    /// The § 1(j)(2) rate table, lowest bracket first.
    pub ordinary: Vec<Bracket>,

    /// The § 63(c)(2) amount.
    pub standard_deduction: USCents,

    /// The § 63(f) amount, per qualifying condition. A taxpayer who is both
    /// 65 or older and blind gets it twice.
    pub additional_aged_blind: USCents,

    /// The first § 1(j)(5)(B) threshold. Long-term gains are taxed at zero up
    /// to it, 15% up to [`Table::max_15_rate_amount`], and 20% above it. Both
    /// are measured against total taxable income rather than against the
    /// gain.
    pub max_zero_rate_amount: USCents,
    /// The second § 1(j)(5)(B) threshold.
    pub max_15_rate_amount: USCents,

    /// Caps the old age, survivors and disability insurance part of
    /// self-employment tax. Published by the Social Security Administration
    /// rather than by the IRS, so it does not come from the same revenue
    /// procedure as the rest of this table.
    pub social_security_wage_base: USCents,

    /// The § 1401(b)(2) figure over which the extra 0.9% applies. Statutory
    /// and not adjusted for inflation.
    pub additional_medicare_threshold: USCents,

    /// The § 6654(d)(1)(C)(i) prior-year adjusted gross income above which
    /// the safe harbour rises from 100% to 110% of the prior year's tax.
    /// Statutory and not adjusted for inflation.
    pub prior_year_agi_step: USCents,
}

/// Every year and status this build carries.
pub(crate) type Tables = BTreeMap<i64, BTreeMap<FilingStatus, Table>>;

/// The tables, built on first use from each year's module.
fn tables() -> &'static Tables {
    static TABLES: OnceLock<Tables> = OnceLock::new();
    TABLES.get_or_init(|| {
        let mut t = Tables::new();
        crate::y2025::register(&mut t);
        crate::y2026::register(&mut t);
        t
    })
}

/// Records a year's tables. Each year's module calls it.
pub(crate) fn register(tables: &mut Tables, year: i64, by_status: BTreeMap<FilingStatus, Table>) {
    tables.insert(year, by_status);
}

/// Returns the published figures for a tax year and filing status.
///
/// It reports [`Error::NoTable`] for a year this build does not carry.
/// Adding a year is a deliberate commit, which is what stops a stale build
/// applying last year's law to this year's income.
pub fn table_for(year: i64, status: FilingStatus) -> Result<Table, Error> {
    if status == FilingStatus::Unspecified {
        return Err(invalid!("ustaxcalc: no filing status given"));
    }
    let Some(by_status) = tables().get(&year) else {
        return Err(Error::with_detail(Error::NoTable, year));
    };
    let Some(t) = by_status.get(&status) else {
        return Err(Error::with_detail(
            Error::NoTable,
            format!("{year} has no table for {status}"),
        ));
    };
    Ok(t.clone())
}

/// Returns every tax year this build carries a table for. Go leaves the
/// order unspecified; here it is ascending.
pub fn years() -> Vec<i64> {
    tables().keys().copied().collect()
}
