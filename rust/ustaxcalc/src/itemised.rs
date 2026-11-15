//! The limits Public Law 119-21 put on itemised deductions.

use crate::{Error, FilingStatus, Rate, USCents, invalid, table_for};

/// Applies the two limits Public Law 119-21 put on itemised deductions for
/// taxable years beginning after December 31, 2025:
///   - 26 U.S.C. § 170(b)(1)(I), sec. 70425: charitable contributions count
///     only to the extent they exceed 0.5% of the contribution base, taken
///     here as adjusted gross income;
///   - 26 U.S.C. § 68, sec. 70111: what is left is reduced by 2/37 of the
///     lesser of itemised deductions and the amount by which taxable income,
///     with them added back, exceeds where the 37% bracket begins. § 68(b)
///     applies it after every other limit.
///
/// `itemised` is Schedule A line 17 before these limits and after the
/// others, such as the state and local tax limit, and `charity` is the
/// charitable part of it. For an earlier year `itemised` is returned
/// unchanged.
pub fn itemised_limits(
    year: i64,
    status: FilingStatus,
    itemised: USCents,
    charity: USCents,
    agi: USCents,
) -> Result<working::Result<USCents>, Error> {
    let zero = USCents(0);
    if itemised < zero || charity < zero || charity > itemised {
        return Err(invalid!(
            "ustaxcalc: charity is part of itemised deductions, and neither can be negative"
        ));
    }
    let table = table_for(year, status)?;
    let mut n = working::Notes::default();
    if year < 2026 {
        return Ok(working::new_result(itemised).with(&n).build()?);
    }
    let floor = Rate(50).of(agi.max(zero));
    let disallowed = charity.min(floor);
    let after = itemised - disallowed;
    if charity > zero {
        n.rule(
            "26 U.S.C. § 170(b)(1)(I), added by Pub. L. 119-21, sec. 70425",
            format!(
                "charitable contributions count only above 0.5% of {agi}, {floor}, so {disallowed} of the {charity} is not deducted"
            ),
        );
    }
    let mut top = zero;
    for b in &table.ordinary {
        if b.rate == Rate(3700) {
            top = b.over;
        }
    }
    if top == zero {
        return Err(invalid!("ustaxcalc: no 37% bracket in the {year} table"));
    }
    // Taxable income without § 68, with the itemised deductions added back,
    // is adjusted gross income.
    let over = (agi - top).max(zero);
    let base = after.min(over);
    // Go's big.Int: exact, truncated toward zero, then back to int64.
    let reduction = USCents((i128::from(base.0) * 2 / 37) as i64);
    if over > zero {
        n.rule(
            "26 U.S.C. § 68, as amended by Pub. L. 119-21, sec. 70111",
            format!(
                "2/37 of the lesser of {after} of itemised deductions and the {over} by which income exceeds the 37% bracket's {top} is {reduction} off"
            ),
        );
    }
    Ok(working::new_result(after - reduction).with(&n).build()?)
}
