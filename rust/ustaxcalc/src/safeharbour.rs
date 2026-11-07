//! The § 6654 required annual payment, its installments, and the credit for
//! withholding.

use estimated::{Date, Month};

use crate::{Error, FilingStatus, Rate, USCents, percent, table_for};

/// The total that has to be paid across the year to avoid the § 6654
/// underpayment penalty, and which of the two tests produced it.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Hash)]
pub struct SafeHarbour {
    /// The lesser of the two figures below, which is what § 6654(d)(1)(B)
    /// asks for.
    pub required: USCents,

    /// 90% of this year's tax.
    pub current_year_test: USCents,

    /// 100% of last year's tax, or 110% where last year's adjusted gross
    /// income passed the § 6654(d)(1)(C) step.
    pub prior_year_test: USCents,

    /// The percentage the prior year test used, so a caller can state why
    /// it was one or the other.
    pub prior_year_rate: Rate,
}

/// Returns what § 6654 requires be paid across the year.
///
/// `current_year_tax` is the estimate of this year's liability, which moves
/// as the year goes on. `prior_year_tax` and `prior_year_agi` come from the
/// return filed for last year and do not move at all. The prior year test is
/// the one worth leaning on for that reason: it is a fixed target, and
/// meeting it avoids the penalty whatever this year turns out to be.
///
/// It does not model the § 6654(d)(2) annualised income instalment method,
/// which can lower an instalment for someone whose income arrived unevenly.
/// A taxpayer entitled to that pays no more than this says, so the figure is
/// safe in the direction that matters.
///
/// A prior year that is not a full twelve months, or in which the taxpayer
/// filed no return, disqualifies the prior year test under
/// § 6654(d)(1)(B)(ii). Pass `prior_year_filed` false in that case and the
/// current year test is used alone.
pub fn required_annual_payment(
    year: i64,
    status: FilingStatus,
    current_year_tax: USCents,
    prior_year_tax: USCents,
    prior_year_agi: USCents,
    prior_year_filed: bool,
) -> Result<working::Result<SafeHarbour>, Error> {
    let table = table_for(year, status)?;

    let mut harbour = SafeHarbour {
        current_year_test: percent(90).of(current_year_tax),
        ..SafeHarbour::default()
    };
    let mut n = working::Notes::default();
    n.rule(
        "26 U.S.C. § 6654(d)(1)(B)(i)",
        format!(
            "90% of this year's estimated tax of {} is {}",
            current_year_tax, harbour.current_year_test
        ),
    );

    if !prior_year_filed {
        harbour.required = harbour.current_year_test;
        n.rule(
            "26 U.S.C. § 6654(d)(1)(B)(ii)",
            "no return was filed for a full prior year, so only the current year test is available",
        );
        return Ok(working::new_result(harbour).with(&n).build()?);
    }

    harbour.prior_year_rate = percent(100);
    if prior_year_agi > table.prior_year_agi_step {
        harbour.prior_year_rate = percent(110);
    }
    harbour.prior_year_test = harbour.prior_year_rate.of(prior_year_tax);

    harbour.required = harbour.current_year_test;
    if harbour.prior_year_test < harbour.required {
        harbour.required = harbour.prior_year_test;
    }

    n.rule(
        "26 U.S.C. § 6654(d)(1)(C)(i)",
        format!(
            "last year's adjusted gross income of {} against the {} step makes the prior year test {} of {}, or {}",
            prior_year_agi,
            table.prior_year_agi_step,
            harbour.prior_year_rate,
            prior_year_tax,
            harbour.prior_year_test
        ),
    );
    n.rule(
        "26 U.S.C. § 6654(d)(1)(B)",
        format!("the lesser of the two tests is {}", harbour.required),
    );

    Ok(working::new_result(harbour).with(&n).build()?)
}

/// One quarterly payment: when it is due, and what the running total has to
/// reach by then.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Hash)]
pub struct Instalment {
    /// The statutory due date.
    pub due: Date,

    /// What must have been paid by this date, counting everything paid for
    /// earlier instalments. § 6654(d)(1)(A) makes each instalment 25% of the
    /// required annual payment, and the test is applied to the running total
    /// rather than to one quarter in isolation.
    pub cumulative_target: USCents,
}

/// Returns the four § 6654(c) payment dates and the running total due by
/// each.
///
/// The dates are the statutory April 15, June 15 and September 15 of the tax
/// year and January 15 of the next. They are not moved for a weekend or a
/// public holiday, which § 7503 does do. A caller rendering a deadline
/// should apply that shift itself, because which days are holidays is not
/// something this function should carry a calendar for.
pub fn instalments(year: i64, required_annual: USCents) -> Vec<Instalment> {
    let dates = [
        estimated::date_of(year, Month::APRIL, 15),
        estimated::date_of(year, Month::JUNE, 15),
        estimated::date_of(year, Month::SEPTEMBER, 15),
        estimated::date_of(year + 1, Month::JANUARY, 15),
    ];

    dates
        .iter()
        .enumerate()
        .map(|(i, &due)| Instalment {
            due,
            // Computed from the annual figure rather than by accumulating a
            // quarter, so rounding cannot make the fourth target miss the
            // total.
            cumulative_target: Rate(2500 * (i as i32 + 1)).of(required_annual),
        })
        .collect()
}

/// Returns how much of a year's withholding counts toward the target at one
/// instalment date.
///
/// § 6654(g) treats tax withheld as paid in equal parts across the four
/// instalments however it actually came out, so someone who fixes their W-4
/// in December still gets a quarter of that withholding credited against the
/// April instalment. This is why withholding and estimated payments cannot
/// be added up the same way.
///
/// `quarter` is 1 through 4. Anything outside that returns zero.
pub fn withholding_credited_by(total_withholding: USCents, quarter: i64) -> USCents {
    if !(1..=4).contains(&quarter) {
        return USCents(0);
    }
    Rate(2500 * quarter as i32).of(total_withholding)
}
