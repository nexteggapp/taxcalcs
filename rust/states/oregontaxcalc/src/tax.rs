//! The rate chart, the federal tax subtraction and the exemption credit.
//! Ported from tax.go.

use filing::Status;
use money::{Rate, USCents};

use crate::tables::{Error, Table, table_for};

/// One bracket of the rate chart and what the taxpayer's income put in it.
///
/// Returned even for bands the income never reached, with `income` and `tax`
/// zero, because a caller showing its working renders the whole ladder.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct Band {
    /// The rate on income in the band.
    pub rate: Rate,

    /// Where the band starts.
    pub from: USCents,
    /// Where the band ends; zero on the top band, which has no upper bound.
    pub to: USCents,

    /// How much of the taxpayer's Oregon taxable income fell in this band.
    pub income: USCents,
    /// The rate applied to `income` and then rounded to a whole dollar,
    /// which is what Oregon's own published figures do. See
    /// `round_to_dollar`.
    pub tax: USCents,
}

/// The rate chart applied to one taxpayer's Oregon taxable income.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Ladder {
    /// Every band, lowest first.
    pub bands: Vec<Band>,

    /// The sum of every band's rounded tax, so it is always a whole number of
    /// dollars.
    pub total: USCents,

    /// The rate on the next dollar of income.
    pub marginal: Rate,

    /// How much more income fits before the next band starts. Zero on the
    /// top band.
    pub room_in_bracket: USCents,
}

/// Rounds to whole dollars, halves away from zero.
///
/// Oregon applies this to each bracket rather than to the total, which is
/// not a detail. A joint filer at $50,000: the exact bracket amounts are
/// $418.00, $904.50 and $2,432.50, summing to $3,755.00. Oregon's published
/// rate chart says $3,756, which is what rounding each one first gives.
/// Rounding only the total would give $3,755 and be wrong against every
/// figure Oregon prints.
fn round_to_dollar(a: USCents) -> USCents {
    if a.0 < 0 {
        return USCents(-((-a.0 + 50) / 100 * 100));
    }
    USCents((a.0 + 50) / 100 * 100)
}

/// Applies the ORS 316.037 rate chart to Oregon taxable income.
///
/// `taxable` is income after the Oregon deduction and the federal tax
/// subtraction, from [`crate::taxable_income`]. It includes any capital gain
/// in full, because Oregon has no preferential rate for one.
///
/// This is the tax before credits. The exemption credit from
/// [`exemption_credit`] is subtracted from it.
///
/// A negative amount is treated as zero.
pub fn ordinary_income_tax(
    year: i64,
    status: Status,
    taxable: USCents,
) -> Result<working::Result<Ladder>, Error> {
    let table = table_for(year, status)?;
    ladder_from(&table, taxable)
}

fn ladder_from(table: &Table, taxable: USCents) -> Result<working::Result<Ladder>, Error> {
    let taxable = taxable.max(USCents(0));
    let mut ladder = Ladder {
        marginal: table.ordinary.first().map(|b| b.rate).unwrap_or_default(),
        ..Ladder::default()
    };
    for (i, bracket) in table.ordinary.iter().enumerate() {
        let mut band = Band {
            rate: bracket.rate,
            from: bracket.over,
            ..Band::default()
        };
        if let Some(next) = table.ordinary.get(i + 1) {
            band.to = next.over;
        }

        let mut top = taxable;
        if band.to.0 != 0 && top > band.to {
            top = band.to;
        }
        if top > band.from {
            band.income = top - band.from;
            band.tax = round_to_dollar(bracket.rate.of(band.income));
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

    let top_rate = table.ordinary.last().map(|b| b.rate).unwrap_or_default();
    let total = ladder.total;
    Ok(working::new_result(ladder)
        .rule(
            format!("Or. Rev. Stat. § 316.037, as adjusted for {}", table.year),
            format!(
                "{taxable} of Oregon taxable income through the {top_rate} chart for a {} filer gives {total}, each bracket rounded to a whole dollar",
                table.status
            ),
        )
        .build()?)
}

/// Returns how much federal income tax an Oregon filer may subtract.
///
/// `federal_tax_liability` starts from Form 1040 line 22 and is reduced by
/// the refundable credits other than the earned income credit, which is what
/// line 10 of the worksheet in the Form OR-40 instructions works out. It
/// reaches Form OR-40 at line 10.
///
/// `federal_agi` is Form 1040 line 11, and decides the cap, which falls in
/// five steps and reaches nothing.
///
/// Both are plain amounts. This crate does not import the federal
/// calculators, so the figures can come from anywhere.
///
/// Oregon is unusual in allowing this at all, and it is the single largest
/// subtraction on most Oregon returns.
pub fn federal_tax_subtraction(
    year: i64,
    status: Status,
    federal_tax_liability: USCents,
    federal_agi: USCents,
) -> Result<working::Result<USCents>, Error> {
    let table = table_for(year, status)?;
    let federal_tax_liability = federal_tax_liability.max(USCents(0));

    // The last step whose floor the income reaches.
    let mut maximum = table
        .federal_tax_subtraction
        .first()
        .map(|s| s.maximum)
        .unwrap_or_default();
    for step in &table.federal_tax_subtraction {
        if federal_agi >= step.at_least {
            maximum = step.maximum;
        }
    }

    let allowed = federal_tax_liability.min(maximum);

    Ok(working::new_result(allowed)
        .rule(
            format!("Or. Rev. Stat. § 316.680(1)(b), as adjusted for {}", table.year),
            format!(
                "federal tax of {federal_tax_liability} against a maximum of {maximum} at {federal_agi} of adjusted gross income gives a subtraction of {allowed}"
            ),
        )
        .build()?)
}

/// Returns the ORS 316.085 credit.
///
/// `exemptions` is the number claimed, which is the filer, a spouse on a
/// joint return, and each dependent.
///
/// The credit disappears entirely above the income limit rather than
/// tapering, so a dollar of extra income can cost a family several hundred.
/// That cliff is Oregon's rule and not a simplification here.
pub fn exemption_credit(
    year: i64,
    status: Status,
    federal_agi: USCents,
    exemptions: i64,
) -> Result<working::Result<USCents>, Error> {
    let table = table_for(year, status)?;
    let exemptions = exemptions.max(0);

    if federal_agi > table.exemption_credit_agi_limit {
        return Ok(working::new_result(USCents(0))
            .rule(
                "Or. Rev. Stat. § 316.085(4)",
                format!(
                    "adjusted gross income of {federal_agi} is above {}, so the credit is nothing for all {exemptions} exemption(s)",
                    table.exemption_credit_agi_limit
                ),
            )
            .build()?);
    }

    let credit = table.exemption_credit * exemptions;
    Ok(working::new_result(credit)
        .rule(
            format!("Or. Rev. Stat. § 316.085, as adjusted for {}", table.year),
            format!(
                "{exemptions} exemption(s) at {} give {credit}",
                table.exemption_credit
            ),
        )
        .build()?)
}

/// Returns tax as a proportion of income, rounded to the nearest basis
/// point.
pub fn effective_rate(tax: USCents, income: USCents) -> Rate {
    if income.0 <= 0 {
        return Rate(0);
    }
    Rate(((tax.0 * 10_000 + income.0 / 2) / income.0) as i32)
}
