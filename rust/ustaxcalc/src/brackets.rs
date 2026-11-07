//! The § 1(j)(2) rate table applied to ordinary taxable income.

use crate::tables::Table;
use crate::{Error, FilingStatus, Rate, USCents, table_for};

/// One bracket of the rate table and what the taxpayer's income put in it.
///
/// Returned even for bands the income never reached, with `income` and
/// `tax` zero, because a caller showing its working renders the whole ladder
/// and tints the row the taxpayer stopped in.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Hash)]
pub struct Band {
    /// The band's rate.
    pub rate: Rate,

    /// The band's lower bound.
    pub from: USCents,
    /// The band's upper bound; zero on the top band, which has no upper
    /// bound.
    pub to: USCents,

    /// How much of the taxpayer's ordinary taxable income fell in this band.
    pub income: USCents,
    /// The rate applied to `income`.
    pub tax: USCents,
}

/// The rate table applied to one taxpayer's ordinary taxable income.
#[derive(Clone, Debug, Default, PartialEq, Eq, Hash)]
pub struct Ladder {
    /// Every band, lowest first.
    pub bands: Vec<Band>,

    /// The sum of every band's tax.
    pub total: USCents,

    /// The rate on the next dollar of ordinary income.
    pub marginal: Rate,

    /// How much more ordinary income fits before the next band starts. Zero
    /// on the top band.
    pub room_in_bracket: USCents,
}

/// Applies the § 1(j)(2) rate table to ordinary taxable income.
///
/// `taxable_ordinary` is income after deductions and with net long-term
/// capital gain already removed, because gains are taxed by
/// [`crate::long_term_gains_tax`] at their own rates rather than by this
/// ladder. Passing total taxable income here would tax the gain twice.
///
/// A negative amount is treated as zero. Deductions exceeding income produce
/// no tax rather than a refundable negative one.
pub fn ordinary_income_tax(
    year: i64,
    status: FilingStatus,
    taxable_ordinary: USCents,
) -> Result<working::Result<Ladder>, Error> {
    let table = table_for(year, status)?;
    ladder_from(&table, taxable_ordinary)
}

/// The computation, split out so that the gains calculator and the
/// sensitivity a caller runs can reuse it without a second table lookup.
pub(crate) fn ladder_from(
    table: &Table,
    mut taxable_ordinary: USCents,
) -> Result<working::Result<Ladder>, Error> {
    if taxable_ordinary < USCents(0) {
        taxable_ordinary = USCents(0);
    }

    let mut ladder = Ladder {
        marginal: table.ordinary[0].rate,
        ..Ladder::default()
    };
    for (i, bracket) in table.ordinary.iter().enumerate() {
        let mut band = Band {
            rate: bracket.rate,
            from: bracket.over,
            ..Band::default()
        };
        if i + 1 < table.ordinary.len() {
            band.to = table.ordinary[i + 1].over;
        }

        // How much income falls inside this band. The top band has no
        // ceiling, so everything above its floor lands in it.
        let mut top = taxable_ordinary;
        if band.to != USCents(0) && top > band.to {
            top = band.to;
        }
        if top > band.from {
            band.income = top - band.from;
            band.tax = bracket.rate.of(band.income);
            ladder.total += band.tax;
            ladder.marginal = bracket.rate;
            if band.to != USCents(0) {
                ladder.room_in_bracket = band.to - taxable_ordinary;
            } else {
                ladder.room_in_bracket = USCents(0);
            }
        }
        ladder.bands.push(band);
    }

    let top_rate = table.ordinary[table.ordinary.len() - 1].rate;
    let applied = format!(
        "{} of ordinary taxable income through the {} brackets for a {} filer gives {}",
        taxable_ordinary, top_rate, table.status, ladder.total
    );
    Ok(working::new_result(ladder)
        .rule(
            format!("26 U.S.C. § 1(j)(2), as adjusted for {}", table.year),
            applied,
        )
        .build()?)
}

/// Returns tax as a proportion of income, rounded to the nearest basis
/// point.
///
/// Zero for zero income, rather than an error. A taxpayer with no income has
/// an effective rate of nothing, and every caller of this is rendering a
/// percentage beside a marginal one.
pub fn effective_rate(tax: USCents, income: USCents) -> Rate {
    if income.0 <= 0 {
        return Rate(0);
    }
    // Go's int64 arithmetic wraps, and its conversion to int32 truncates.
    let n = tax.0.wrapping_mul(10000).wrapping_add(income.0 / 2);
    Rate((n / income.0) as i32)
}
