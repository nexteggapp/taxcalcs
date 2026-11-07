//! Net long-term capital gain at the § 1(h) rates.

use crate::tables::Table;
use crate::{Error, FilingStatus, PositionKind, USCents, percent, table_for};

/// Net long-term capital gain split across the three rates.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Hash)]
pub struct GainsTax {
    /// How much of the gain the 0% rate reached.
    pub at_zero: USCents,
    /// How much of the gain the 15% rate reached.
    pub at_15: USCents,
    /// How much of the gain the 20% rate reached.
    pub at_20: USCents,

    /// The tax on the gain.
    pub total: USCents,
}

/// Taxes net long-term capital gain under § 1(h) and § 1(j)(5).
///
/// `taxable_ordinary` is taxable income with the gain removed, and
/// `net_long_term_gain` is the gain itself. The two are separate arguments
/// because the rate on a gain is set by where it lands when stacked on top
/// of ordinary income, not by the size of the gain. A $5,000 gain is taxed
/// at zero for one taxpayer and at 20% for another.
///
/// This models the three headline rates. It does not model the 28% rate on
/// collectibles under § 1(h)(4) or the 25% rate on unrecaptured § 1250 gain
/// under § 1(h)(6), and a caller holding either must not fold them into
/// `net_long_term_gain`. It also does not model the § 1411 net investment
/// income tax, which is a separate 3.8% charge rather than a capital gains
/// rate.
pub fn long_term_gains_tax(
    year: i64,
    status: FilingStatus,
    taxable_ordinary: USCents,
    net_long_term_gain: USCents,
) -> Result<working::Result<GainsTax>, Error> {
    let table = table_for(year, status)?;
    gains_from(&table, taxable_ordinary, net_long_term_gain)
}

/// The computation, for [`crate::tax_on_income`] to reuse with its table.
pub(crate) fn gains_from(
    table: &Table,
    mut taxable_ordinary: USCents,
    net_long_term_gain: USCents,
) -> Result<working::Result<GainsTax>, Error> {
    let zero = USCents(0);
    if taxable_ordinary < zero {
        taxable_ordinary = zero;
    }
    if net_long_term_gain <= zero {
        // A net loss is not taxed here. What a loss does — offsetting
        // ordinary income up to the § 1211(b) limit and carrying forward —
        // belongs to the caller working out taxable income, before this is
        // reached.
        return Ok(working::new_result(GainsTax::default())
            .rule(
                "26 U.S.C. § 1(h)",
                "no net long-term gain, so no tax at the gains rates",
            )
            .build()?);
    }

    let total = taxable_ordinary + net_long_term_gain;

    // The gain is stacked above ordinary income. Only the part of the stack
    // that sits below a threshold gets that threshold's rate.
    let mut at_zero = table.max_zero_rate_amount - taxable_ordinary;
    if at_zero < zero {
        at_zero = zero;
    }
    if at_zero > net_long_term_gain {
        at_zero = net_long_term_gain;
    }

    let mut at_20 = total - table.max_15_rate_amount;
    if at_20 < zero {
        at_20 = zero;
    }
    if at_20 > net_long_term_gain {
        at_20 = net_long_term_gain;
    }

    let mut at_15 = net_long_term_gain - at_zero - at_20;
    if at_15 < zero {
        at_15 = zero;
    }

    let mut gains = GainsTax {
        at_zero,
        at_15,
        at_20,
        total: zero,
    };
    gains.total = percent(15).of(at_15) + percent(20).of(at_20);

    Ok(working::new_result(gains)
        .rule(
            format!(
                "26 U.S.C. § 1(h) and § 1(j)(5)(B), as adjusted for {}",
                table.year
            ),
            format!(
                "{} of gain stacked on {} of ordinary taxable income: {} at 0% up to {}, {} at 15% up to {}, {} at 20%",
                net_long_term_gain,
                taxable_ordinary,
                at_zero,
                table.max_zero_rate_amount,
                at_15,
                table.max_15_rate_amount,
                at_20
            ),
        )
        .require(
            PositionKind::LotSelection,
            "which lots a sale consumed decides its basis and whether the gain is \
             long-term at all. § 1.1012-1(c) takes first in, first out unless the seller \
             identified specific lots at the time of sale",
        )
        .build()?)
}
