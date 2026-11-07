//! The rate schedule, the surcharge and the effective rate. Ported from
//! tax.go.

use filing::Status;
use money::{Rate, USCents};

use crate::tables::{Error, Table, table_for};

/// One bracket of a rate schedule and what the taxpayer's income put in it.
///
/// Returned even for bands the income never reached, with `income` and
/// `tax` zero, because a caller showing its working renders the whole ladder
/// and tints the row the taxpayer stopped in.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct Band {
    /// The rate on income in the band.
    pub rate: Rate,

    /// The band's lower bound.
    pub from: USCents,
    /// The band's upper bound, zero on the top band, which has none.
    pub to: USCents,

    /// How much of the taxpayer's California taxable income fell in this
    /// band.
    pub income: USCents,
    /// The rate applied to `income`.
    pub tax: USCents,
}

/// The rate schedule applied to one taxpayer's California taxable income.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Ladder {
    /// Every band, lowest first.
    pub bands: Vec<Band>,

    /// The sum of every band's tax.
    pub total: USCents,

    /// The rate on the next dollar of income.
    pub marginal: Rate,

    /// How much more income fits before the next band starts. Zero on the
    /// top band.
    pub room_in_bracket: USCents,
}

/// Applies the Rev. & Tax. Code § 17041 rate schedule to California taxable
/// income.
///
/// `taxable` is income after the California deduction, from
/// [`crate::taxable_income`]. It includes any capital gain in full:
/// California has no preferential rate for one, so a caller must not hold a
/// gain back the way the federal calculators require.
///
/// This is the tax before credits. The exemption credits from
/// [`crate::exemption_credit`] are subtracted from it, and the surcharge from
/// [`behavioral_health_services_tax`] is added after them.
///
/// A negative amount is treated as zero. Deductions exceeding income produce
/// no tax rather than a refundable negative one.
pub fn ordinary_income_tax(
    year: i64,
    status: Status,
    taxable: USCents,
) -> Result<working::Result<Ladder>, Error> {
    let table = table_for(year, status)?;
    ladder_from(&table, taxable)
}

/// The computation, split out so that the table test can reuse it without a
/// second lookup.
fn ladder_from(table: &Table, mut taxable: USCents) -> Result<working::Result<Ladder>, Error> {
    if taxable.0 < 0 {
        taxable = USCents(0);
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
        let mut top = taxable;
        if band.to.0 != 0 && top > band.to {
            top = band.to;
        }
        if top > band.from {
            band.income = top - band.from;
            band.tax = bracket.rate.of(band.income);
            ladder.total += band.tax;
            ladder.marginal = bracket.rate;
            ladder.room_in_bracket = if band.to.0 != 0 {
                band.to - taxable
            } else {
                USCents(0)
            };
        }
        ladder.bands.push(band);
    }

    let top_rate = table.ordinary[table.ordinary.len() - 1].rate;
    let total = ladder.total;
    Ok(working::new_result(ladder)
        .rule(
            format!(
                "Cal. Rev. & Tax. Code § 17041, as adjusted for {}",
                table.year
            ),
            format!(
                "{taxable} of California taxable income through the {top_rate} schedule for a {} filer gives {total}",
                table.status
            ),
        )
        .build()?)
}

/// Computes the Rev. & Tax. Code § 17043 surcharge on taxable income above
/// one million dollars.
///
/// One per cent of the excess, and nothing at all below the threshold.
/// Proposition 1 renamed it from the Mental Health Services Tax for taxable
/// years beginning on or after 1 January 2025; the charge itself did not
/// change.
///
/// It is a tax rather than a rate band, so it is added after the exemption
/// credits have reduced the tax from [`ordinary_income_tax`] and is not
/// itself reduced by them.
pub fn behavioral_health_services_tax(
    year: i64,
    status: Status,
    taxable: USCents,
) -> Result<working::Result<USCents>, Error> {
    let table = table_for(year, status)?;

    let excess = taxable - table.behavioral_health_threshold;
    if excess.0 <= 0 {
        return Ok(working::new_result(USCents(0))
            .rule(
                "Cal. Rev. & Tax. Code § 17043",
                format!(
                    "taxable income of {taxable} is not above {}, so no surcharge is due",
                    table.behavioral_health_threshold
                ),
            )
            .build()?);
    }

    let tax = table.behavioral_health_rate.of(excess);
    Ok(working::new_result(tax)
        .rule(
            "Cal. Rev. & Tax. Code § 17043",
            format!(
                "{excess} of taxable income above {}, at {}, gives {tax}",
                table.behavioral_health_threshold, table.behavioral_health_rate
            ),
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
    Rate(((tax.0 * 10000 + income.0 / 2) / income.0) as i32)
}
