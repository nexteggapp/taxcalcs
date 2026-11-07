//! The chapter 82.87 RCW capital gains excise tax.

use money::{Rate, USCents};
use working::{Notes, PositionKind};

use crate::tables::{Error, table_for};

/// What a filer subtracts from their Washington capital gains before the
/// rates apply.
///
/// The standard deduction is not here: it is the same for everybody and
/// comes from the table. These are the ones that depend on what the filer
/// did.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct Deductions {
    /// Qualified donations to Washington-directed organisations. Only the
    /// part above the year's threshold is deductible, and the deduction is
    /// capped, so giving away a million does not remove a million of gain.
    pub charitable_donations: USCents,

    /// Gain from the sale of all or substantially all of a qualified
    /// family-owned small business, which RCW 82.87.070 deducts in full.
    pub family_owned_small_business: USCents,
}

/// The things about a filer, rather than about their figures, that the tax
/// turns on.
///
/// Both start false and each unanswered one comes back in
/// [`working::Result::required`]. They exist so that those positions can be
/// answered rather than only reported: a caller who has done the work says
/// so, and stops being told about it.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct Facts {
    /// Confirms that the caller has already allocated the gain to Washington
    /// under RCW 82.87.100 and removed the classes RCW 82.87.050 exempts.
    ///
    /// This crate cannot check it. Allocation of gain on intangible property
    /// turns on where the individual was domiciled when the sale settled,
    /// which is a fact about the seller that no figure carries.
    pub gain_allocated: bool,

    /// Confirms that the lots each sale consumed are settled, so that basis
    /// and holding period are known and the gain really is long-term.
    pub lots_identified: bool,
}

/// The tax on a year's Washington capital gains.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct ExciseTaxResult {
    /// What the caller supplied, before deductions.
    pub washington_capital_gains: USCents,

    /// The standard deduction actually subtracted.
    pub standard_deduction: USCents,
    /// The charitable deduction actually subtracted, which is not what was
    /// given.
    pub charitable_deduction: USCents,
    /// The family business deduction actually subtracted.
    pub family_business_deduction: USCents,

    /// The gain the rates applied to. Zero where the deductions covered
    /// everything, which is the ordinary case.
    pub taxable: USCents,

    /// `taxable` below the surcharge threshold and above it: all of it.
    pub at_base_rate: USCents,
    /// The part of `taxable` above the surcharge threshold. The surcharge is
    /// on top of the base rate rather than instead of it, so it is taxed at
    /// both and appears in both figures.
    pub at_surcharge_rate: USCents,

    /// What the base rate produced.
    pub base_tax: USCents,
    /// What the surcharge rate produced.
    pub surcharge_tax: USCents,
    /// The sum of `base_tax` and `surcharge_tax`.
    pub total: USCents,
}

/// Computes the chapter 82.87 RCW excise tax on a year's Washington capital
/// gains.
///
/// `washington_capital_gains` is long-term capital gain allocated to
/// Washington and with the exempt classes already removed. That is a
/// narrower figure than a federal Schedule D total and this crate cannot
/// derive it: real estate, retirement accounts, timber, commercial fishing
/// privileges and depreciable business assets are all exempt, and gain is
/// allocated to Washington by where the seller was domiciled rather than by
/// where the asset was. The allocation question comes back in
/// [`working::Result::required`].
///
/// A loss, or a gain below the standard deduction, produces no tax and no
/// filing obligation.
pub fn excise_tax(
    year: i64,
    washington_capital_gains: USCents,
    d: Deductions,
    f: Facts,
) -> Result<working::Result<ExciseTaxResult>, Error> {
    let table = table_for(year)?;

    let mut out = ExciseTaxResult {
        washington_capital_gains,
        standard_deduction: table.standard_deduction,
        ..ExciseTaxResult::default()
    };

    // Only donations above the threshold count, and only up to the cap.
    let over = d.charitable_donations - table.charitable_deduction_threshold;
    if over.0 > 0 {
        out.charitable_deduction = over;
        if out.charitable_deduction > table.charitable_deduction_cap {
            out.charitable_deduction = table.charitable_deduction_cap;
        }
    }
    out.family_business_deduction = d.family_owned_small_business;

    out.taxable = washington_capital_gains
        - out.standard_deduction
        - out.charitable_deduction
        - out.family_business_deduction;
    if out.taxable.0 < 0 {
        out.taxable = USCents(0);
    }

    let mut positions = Notes::default();
    if !f.gain_allocated {
        positions.require(
            PositionKind::CapitalAssetSitus,
            "RCW 82.87.100 allocates the gain on intangible property to Washington by \
             where the individual was domiciled when the sale settled, not by where the \
             asset or the broker was. Whether a gain is Washington's at all is a fact \
             about the seller that these figures do not carry. Answer it with \
             Facts.GainAllocated",
        );
    }
    if !f.lots_identified {
        positions.require(
            PositionKind::LotSelection,
            "which lots a sale consumed decides its basis and whether the gain is \
             long-term at all, and RCW 82.87.040 reaches only long-term gain. Answer it \
             with Facts.LotsIdentified",
        );
    }

    if out.taxable.0 == 0 {
        return Ok(working::new_result(out)
            .rule(
                format!("RCW 82.87.060(1), as adjusted for {}", table.year),
                format!(
                    "{} of Washington capital gains is not above the deductions of {}, so no tax is due and no return is required",
                    washington_capital_gains,
                    out.standard_deduction + out.charitable_deduction + out.family_business_deduction
                ),
            )
            .with(&positions)
            .build()?);
    }

    out.at_base_rate = out.taxable;
    let over = out.taxable - table.surcharge_threshold;
    if over.0 > 0 {
        out.at_surcharge_rate = over;
    }

    out.base_tax = table.base_rate.of(out.at_base_rate);
    out.surcharge_tax = table.surcharge_rate.of(out.at_surcharge_rate);
    out.total = out.base_tax + out.surcharge_tax;

    let mut notes = Notes::default();
    notes.rule(
        format!("RCW 82.87.060(1), as adjusted for {}", table.year),
        format!(
            "{} of Washington capital gains, less a standard deduction of {} and {} of other deductions, leaves {} taxable",
            washington_capital_gains,
            out.standard_deduction,
            out.charitable_deduction + out.family_business_deduction,
            out.taxable
        ),
    );
    notes.rule(
        "RCW 82.87.040(1)(a)",
        format!(
            "{} on all {} of taxable gain gives {}",
            table.base_rate, out.at_base_rate, out.base_tax
        ),
    );
    if out.at_surcharge_rate.0 > 0 {
        notes.rule(
            "RCW 82.87.040(1)(b), as enacted by ESSB 5813, ch. 421, Laws of 2025",
            format!(
                "a further {} on the {} above {} gives {}, so the top of the gain meets {} in total",
                table.surcharge_rate,
                out.at_surcharge_rate,
                table.surcharge_threshold,
                out.surcharge_tax,
                Rate(table.base_rate.0 + table.surcharge_rate.0)
            ),
        );
    }

    Ok(working::new_result(out)
        .with(&notes)
        .with(&positions)
        .build()?)
}
