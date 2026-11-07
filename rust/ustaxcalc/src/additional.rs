//! Additional taxes and income: early distributions from retirement plans,
//! the § 469(i) rental loss allowance, and the taxable part of a state
//! income tax refund.

use crate::{
    DOLLAR, Error, FILING_STATUS_UNSPECIFIED, FilingStatus, MARRIED_FILING_SEPARATELY, Rate,
    SINGLE, USCents, invalid, table_for,
};

/// One Form 1099-R: the taxable amount in box 2a and the distribution code
/// in box 7.
#[derive(Clone, Debug, Default, PartialEq, Eq, Hash)]
pub struct RetirementDistribution {
    taxable: USCents,
    code: String,
}

impl RetirementDistribution {
    /// The taxable amount, box 2a.
    pub fn taxable(&self) -> USCents {
        self.taxable
    }

    /// The distribution code, box 7.
    pub fn code(&self) -> String {
        self.code.clone()
    }
}

/// A distribution's taxable amount and its box 7 code, such as "1" for an
/// early distribution with no known exception or "7" for a normal one.
pub fn new_retirement_distribution(
    taxable: USCents,
    code: impl Into<String>,
) -> Result<RetirementDistribution, Error> {
    let code = code.into();
    if taxable < USCents(0) {
        return Err(invalid!(
            "ustaxcalc: a distribution's taxable amount cannot be negative"
        ));
    }
    if code.is_empty() {
        return Err(invalid!("ustaxcalc: a distribution needs its box 7 code"));
    }
    Ok(RetirementDistribution { taxable, code })
}

/// The 10% additional tax of 26 U.S.C. § 72(t)(1) on the distributions with
/// code 1, Schedule 2 (Form 1040) line 8.
///
/// The Instructions for Form 5329 (2025) let a filer whose Forms 1099-R all
/// show code 1, and who owes the tax on the full amount, report 10% of it on
/// Schedule 2 line 8 without Form 5329. That is what this computes. An
/// exception claimed on Form 5329, and the other codes' rules, such as 25%
/// on a SIMPLE IRA's code S, are not computed;
/// [`early_distribution_tax_other`] reports a distribution with such a
/// code.
///
/// The Form 2210 instructions (2025) count Schedule 2 line 8's additional
/// tax on distributions in the tax the estimated tax rules measure.
pub fn early_distribution_tax(
    year: i64,
    ds: &[RetirementDistribution],
) -> Result<working::Result<USCents>, Error> {
    table_for(year, SINGLE)?;
    let mut subject = USCents(0);
    for d in ds {
        if d.code.contains('1') && !d.code.contains('P') {
            subject += d.taxable;
        }
    }
    let tax = Rate(1_000).of(subject);
    let mut n = working::Notes::default();
    if subject > USCents(0) {
        n.rule(
            "26 U.S.C. § 72(t)(1); Form 5329 (2025), Part I, line 4; Instructions for Form 5329 (2025)",
            format!(
                "10% of the {subject} of early distributions shown with code 1 is {tax}, on Schedule 2 (Form 1040), line 8"
            ),
        );
    }
    Ok(working::new_result(tax).with(&n).build()?)
}

/// Reports a distribution whose code isn't one
/// [`handled_distribution_codes`] lists, whose income or additional tax is
/// then not worked out.
pub fn early_distribution_tax_other(ds: &[RetirementDistribution]) -> bool {
    !unhandled_distribution_codes(ds).is_empty()
}

/// The Form 1099-R box 7 codes whose 2026 income and additional tax the
/// Instructions for Forms 1099-R and 5498 (2026) and the Form 1040
/// instructions settle:
///   - 1, early with no known exception: box 2a is income, and 10% is due;
///   - 2, early with an exception, 3, disability, 4, death, and 7, normal:
///     box 2a is income, with no additional tax;
///   - G and H, direct rollovers: box 2a is income, which the payer reports
///     as $0 for a rollover that isn't to a Roth IRA, with no additional
///     tax;
///   - P, taxable in 2025 or a previous year: none of it is 2026 income.
const HANDLED_CODES: &str = "12347GHP";

/// Lists the box 7 codes, alone or paired, whose income and additional tax
/// are worked out.
pub fn handled_distribution_codes() -> Vec<String> {
    ["1", "2", "3", "4", "7", "G", "H", "P"]
        .into_iter()
        .map(String::from)
        .collect()
}

/// The codes among `ds` that aren't handled, each once, in the order met.
pub fn unhandled_distribution_codes(ds: &[RetirementDistribution]) -> Vec<String> {
    let mut out: Vec<String> = Vec::new();
    for d in ds {
        for c in d.code.chars() {
            let s = c.to_string();
            if !HANDLED_CODES.contains(c) && !out.contains(&s) {
                out.push(s);
            }
        }
    }
    out
}

/// The 2026 income from the distributions: box 2a of each, except one coded
/// P, which was income in an earlier year. A distribution with an unhandled
/// code counts its box 2a, as the payer reports it.
pub fn distribution_income(ds: &[RetirementDistribution]) -> USCents {
    let mut income = USCents(0);
    for d in ds {
        if !d.code.contains('P') {
            income += d.taxable;
        }
    }
    income
}

/// The part of a net loss from rental real estate the filer actively
/// participated in that 26 U.S.C. § 469(i) lets offset other income: up to
/// $25,000, less half of modified AGI over $100,000; on a separate return,
/// $12,500 less half of modified AGI over $50,000 for a filer who lived
/// apart from their spouse all year, and nothing otherwise. `magi` is
/// adjusted gross income figured without the loss and without the IRA
/// deduction, among the amounts § 469(i)(3)(E) leaves out.
pub fn rental_loss_allowed(
    status: FilingStatus,
    loss: USCents,
    magi: USCents,
    lived_apart: bool,
) -> Result<working::Result<USCents>, Error> {
    let zero = USCents(0);
    if loss < zero {
        return Err(invalid!(
            "ustaxcalc: a rental loss is given as a positive amount"
        ));
    }
    let (mut limit, mut start) = (DOLLAR * 25_000, DOLLAR * 100_000);
    let mut n = working::Notes::default();
    if status == MARRIED_FILING_SEPARATELY {
        if !lived_apart {
            n.rule(
                "26 U.S.C. § 469(i)(5)(B)",
                "a married person filing separately who lived with their spouse at any time in the year gets no allowance, so none of the loss is allowed",
            );
            return Ok(working::new_result(zero).with(&n).build()?);
        }
        (limit, start) = (DOLLAR * 12_500, DOLLAR * 50_000);
    }
    limit = (limit - (magi - start).max(zero) / 2).max(zero);
    let allowed = loss.min(limit);
    n.rule(
        "26 U.S.C. § 469(i)(1) to (3), (5)",
        format!(
            "modified AGI of {magi} leaves an allowance of {limit}, so {allowed} of the {loss} rental loss is allowed"
        ),
    );
    Ok(working::new_result(allowed).with(&n).build()?)
}

/// What the State and Local Income Tax Refund Worksheet in the Form 1040
/// instructions reads: the refund, and last year's Schedule A.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Hash)]
pub struct StateRefund {
    refund: USCents,
    prior_income_tax: USCents,
    prior_salt_paid: USCents,
    prior_salt_deducted: USCents,
    prior_itemized: USCents,
    prior_status: FilingStatus,
    prior_boxes: i64,
    spouse_itemized: bool,
}

impl StateRefund {
    /// The state or local income tax refund on Form 1099-G.
    pub fn refund(&self) -> USCents {
        self.refund
    }
}

/// Builds a [`StateRefund`].
#[derive(Clone, Debug, Default)]
pub struct StateRefundBuilder {
    r: StateRefund,
}

/// Starts the worksheet for a refund of last year's state or local income
/// tax, for a filer whose last year's return used `status`.
pub fn new_state_refund(refund: USCents, status: FilingStatus) -> StateRefundBuilder {
    StateRefundBuilder {
        r: StateRefund {
            refund,
            prior_status: status,
            ..StateRefund::default()
        },
    }
}

impl StateRefundBuilder {
    /// The state and local income tax included in last year's Schedule A
    /// line 5d, which caps line 1.
    pub fn prior_income_tax(&mut self, a: USCents) -> &mut Self {
        self.r.prior_income_tax = a;
        self
    }

    /// Last year's Schedule A line 5d, the state and local taxes paid, and
    /// line 5e, what was deducted after the limit.
    pub fn prior_salt(&mut self, paid: USCents, deducted: USCents) -> &mut Self {
        (self.r.prior_salt_paid, self.r.prior_salt_deducted) = (paid, deducted);
        self
    }

    /// Last year's Schedule A line 17. Zero for a filer who took the
    /// standard deduction.
    pub fn prior_itemized(&mut self, a: USCents) -> &mut Self {
        self.r.prior_itemized = a;
        self
    }

    /// The number of worksheet line 6 boxes checked: each of you and a
    /// spouse 65 or older, and each blind, last year.
    pub fn prior_boxes(&mut self, n: i64) -> &mut Self {
        self.r.prior_boxes = n;
        self
    }

    /// Marks a separate return last year whose spouse itemized, for whom the
    /// worksheet skips the standard deduction.
    pub fn spouse_itemized_separately(&mut self) -> &mut Self {
        self.r.spouse_itemized = true;
        self
    }

    /// Refuses negative figures, more than four boxes, and a deduction after
    /// the limit larger than what was paid.
    pub fn build(&self) -> Result<StateRefund, Error> {
        let r = self.r;
        let zero = USCents(0);
        if r.refund < zero
            || r.prior_income_tax < zero
            || r.prior_salt_paid < zero
            || r.prior_salt_deducted < zero
            || r.prior_itemized < zero
        {
            return Err(invalid!(
                "ustaxcalc: a state refund worksheet figure cannot be negative"
            ));
        }
        if r.prior_boxes < 0 || r.prior_boxes > 4 {
            return Err(invalid!(
                "ustaxcalc: {} boxes on worksheet line 6",
                r.prior_boxes
            ));
        }
        if r.prior_salt_deducted > r.prior_salt_paid {
            return Err(invalid!(
                "ustaxcalc: more state and local tax deducted than paid"
            ));
        }
        if r.prior_status == FILING_STATUS_UNSPECIFIED {
            return Err(invalid!("ustaxcalc: last year's filing status is needed"));
        }
        Ok(r)
    }
}

/// The taxable part of a refund of last year's state or local income tax,
/// Schedule 1 (Form 1040) line 1, by the State and Local Income Tax Refund
/// Worksheet in the 2025 Form 1040 instructions. A refund is taxable only as
/// far as deducting the tax lowered last year's tax.
///
/// Lines 5 and 6 are last year's standard deduction and the addition for
/// age or blindness, from last year's table. The worksheet doesn't cover a
/// refund for an earlier year, the alternative minimum tax, unused credits
/// and the other cases its exception lists, which Publication 525 handles.
pub fn state_refund_taxable(year: i64, r: StateRefund) -> Result<working::Result<USCents>, Error> {
    let prior = table_for(year - 1, r.prior_status)?;
    let mut n = working::Notes::default();
    const CITE: &str = "Form 1040 instructions (2025), State and Local Income Tax Refund Worksheet—Schedule 1, Line 1";
    let mut done = |taxable: USCents, why: String| -> Result<working::Result<USCents>, Error> {
        n.rule(CITE, why);
        Ok(working::new_result(taxable).with(&n).build()?)
    };
    let line1 = r.refund.min(r.prior_income_tax);
    let mut line3 = line1;
    if r.prior_salt_paid > r.prior_salt_deducted {
        let line2 = r.prior_salt_paid - r.prior_salt_deducted;
        if line1 <= line2 {
            return done(
                USCents(0),
                format!(
                    "the {line1} refund is no more than the {line2} of state and local taxes the limit kept from being deducted, so none is taxable"
                ),
            );
        }
        line3 = line1 - line2;
    }
    let mut line8 = r.prior_itemized;
    if !(r.prior_status == MARRIED_FILING_SEPARATELY && r.spouse_itemized) {
        let line7 = prior.standard_deduction + prior.additional_aged_blind * r.prior_boxes;
        if line7 >= r.prior_itemized {
            return done(
                USCents(0),
                format!(
                    "last year's itemized deductions of {} were no more than the {line7} standard deduction, so none of the refund is taxable",
                    r.prior_itemized
                ),
            );
        }
        line8 = r.prior_itemized - line7;
    }
    let taxable = line3.min(line8);
    done(
        taxable,
        format!(
            "the smaller of {line3} of refund (line 3) and {line8} of itemized deductions over the standard deduction (line 8) is taxable: {taxable}"
        ),
    )
}
