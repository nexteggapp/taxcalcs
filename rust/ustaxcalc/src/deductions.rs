//! The standard deduction and the choice between it and itemising.

use crate::tables::Table;
use crate::{
    Error, FilingStatus, MARRIED_FILING_JOINTLY, PositionKind, SURVIVING_SPOUSE, USCents, table_for,
};

/// What is subtracted from adjusted gross income, and which method produced
/// it.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Hash)]
pub struct Deduction {
    /// The deduction taken.
    pub amount: USCents,

    /// Whether `amount` is the itemised total rather than the standard
    /// deduction.
    pub itemised: bool,

    /// The standard deduction including any addition for age or blindness,
    /// whether or not it was the one taken. A caller showing its working
    /// renders both figures so the choice is checkable.
    pub standard: USCents,
}

/// The facts about a taxpayer that change the standard deduction.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Hash)]
pub struct Conditions {
    /// Adds one unit of the § 63(f) amount.
    pub aged_65_or_older: bool,
    /// Adds one unit of the § 63(f) amount. A taxpayer who is both gets it
    /// twice.
    pub blind: bool,

    /// Adds the same again on a joint return; ignored for every other
    /// filing status.
    pub spouse_aged_65_or_older: bool,
    /// Adds the same again on a joint return; ignored for every other
    /// filing status.
    pub spouse_blind: bool,
}

/// Returns the § 63(c) deduction with any § 63(f) addition for age or
/// blindness.
pub fn standard_deduction(
    year: i64,
    status: FilingStatus,
    c: Conditions,
) -> Result<working::Result<USCents>, Error> {
    let table = table_for(year, status)?;
    let (amount, units) = standard_from(&table, status, c);

    let mut b = working::new_result(amount);
    b.rule(
        format!("26 U.S.C. § 63(c)(2), as adjusted for {year}"),
        format!(
            "the standard deduction for a {} filer is {}",
            status, table.standard_deduction
        ),
    );
    if units > 0 {
        b.rule(
            "26 U.S.C. § 63(f)",
            format!(
                "{} addition(s) of {} for age or blindness bring it to {}",
                units, table.additional_aged_blind, amount
            ),
        );
    }
    Ok(b.build()?)
}

fn standard_from(table: &Table, status: FilingStatus, c: Conditions) -> (USCents, i64) {
    let mut units = 0;
    if c.aged_65_or_older {
        units += 1;
    }
    if c.blind {
        units += 1;
    }
    if status == MARRIED_FILING_JOINTLY || status == SURVIVING_SPOUSE {
        if c.spouse_aged_65_or_older {
            units += 1;
        }
        if c.spouse_blind {
            units += 1;
        }
    }
    (
        table.standard_deduction + table.additional_aged_blind * units,
        units,
    )
}

/// Which deduction method a taxpayer has chosen, where they have.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub enum Election {
    /// Takes whichever figure is bigger. Right for almost everybody, and the
    /// behaviour of a caller who has not thought about it. Go's
    /// `ElectLarger`.
    #[default]
    Larger,

    /// Takes the standard deduction whatever the itemised total is. Go's
    /// `ElectStandard`.
    Standard,

    /// Takes the itemised total whatever the standard deduction is. Go's
    /// `ElectItemise`.
    ///
    /// A taxpayer filing separately must itemise if their spouse does,
    /// however much smaller their own total is, so this is not merely the
    /// irrational case.
    Itemise,
}

/// Chooses between the standard deduction and an itemised total.
///
/// `itemised_total` is what the taxpayer's itemised deductions come to, or
/// zero where nothing has been totalled.
///
/// `election` settles the § 63(e) choice. Pass [`Election::Larger`] and the
/// bigger figure wins, which is what a taxpayer choosing freely does. The
/// position is still reported in that case, because the choice was made by
/// this function rather than by the taxpayer. Passing either of the others
/// answers it, and the position is not returned.
pub fn deduction_taken(
    year: i64,
    status: FilingStatus,
    c: Conditions,
    itemised_total: USCents,
    election: Election,
) -> Result<working::Result<Deduction>, Error> {
    let table = table_for(year, status)?;
    let (standard, units) = standard_from(&table, status, c);

    let mut taken = Deduction {
        amount: standard,
        itemised: false,
        standard,
    };
    if election == Election::Itemise {
        (taken.amount, taken.itemised) = (itemised_total, true);
    } else if election == Election::Standard {
        // Already the standard deduction.
    } else if itemised_total > standard {
        (taken.amount, taken.itemised) = (itemised_total, true);
    }

    let mut b = working::new_result(taken);
    b.rule(
        format!("26 U.S.C. § 63(c), as adjusted for {year}"),
        format!(
            "the standard deduction is {standard} after {units} addition(s) for age or blindness"
        ),
    );
    if itemised_total > USCents(0) {
        b.rule(
            "26 U.S.C. § 63(e)",
            format!(
                "itemised deductions of {} against a standard deduction of {}, so {} was taken",
                itemised_total, standard, taken.amount
            ),
        );
    }
    if election == Election::Larger {
        b.require(
            PositionKind::DeductionMethod,
            "§ 63(e) makes itemising an election rather than an outcome. A taxpayer \
             may itemise a smaller total, and one filing separately must itemise if \
             their spouse does",
        );
    }
    Ok(b.build()?)
}
