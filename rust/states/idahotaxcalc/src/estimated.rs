//! Idaho's estimated tax rules for individuals, as the shared
//! [`EstimatedTax`] trait.
//!
//! There are none. Idaho Code § 63-3036A requires estimated payments of
//! corporations only, and the Form 40 instructions tell individuals that
//! Idaho doesn't require them. So Requirement says no payments are required
//! and no penalty can arise, Schedule has no installments, and Penalty is an
//! exact zero.
//!
//! What an individual does meet is the extension prepayment test of Idaho
//! Code § 63-3033: the automatic six-month extension to file holds only if
//! enough was paid by the April due date. That is a condition of the
//! extension, not an estimated payment, and it is described in the
//! Explanation rather than computed here.

use std::any::Any;

use estimated::{
    Date, Error, EstimatedTax, Explanation, Facts, Installment, Jurisdiction, Penalty, Requirement,
    WaiverKind,
};
use money::{Rate, USCents};
use working::{Notes, Reading};

use crate::estimated_y2026::year_2026;

/// Idaho's postal code.
const JURISDICTION: Jurisdiction = estimated::IDAHO;

/// One tax year's published figures. None of them is an estimated tax
/// figure; they are the extension prepayment test and the interest the
/// Explanation describes.
#[derive(Clone, Copy, Debug)]
pub(crate) struct EstimatedYear {
    pub(crate) year: i64,

    /// § 63-3033(a): the share of this year's tax that must be paid by the
    /// unextended due date for the automatic extension to hold.
    pub(crate) extension_this_year: Rate,
    /// § 63-3033(a): the share of the tax on last year's return that must be
    /// paid by the unextended due date for the automatic extension to hold.
    pub(crate) extension_prior_year: Rate,

    /// § 63-3033(b): a payment needed to meet the test of this much or less
    /// is not required.
    pub(crate) extension_minimum: USCents,

    /// § 63-3033(f)(1): the penalty a month, from the original due date,
    /// when the test is missed and the tax is paid by the extended due date.
    pub(crate) extension_penalty: Rate,

    /// The annual rate on tax unpaid after the return's due date in the year
    /// after the tax year, set under § 63-3045.
    pub(crate) interest: Rate,
}

/// Returns Idaho's estimated tax rules for a tax year, or
/// [`Error::NoRules`] for a year this build does not carry.
pub fn estimated_tax(year: i64) -> Result<Box<dyn EstimatedTax>, Error> {
    let y = match year {
        2026 => year_2026(),
        _ => return Err(estimated::no_rules(&JURISDICTION, year)),
    };
    Ok(Box::new(IdahoEstimated { y }))
}

struct IdahoEstimated {
    y: EstimatedYear,
}

/// Go's `%T` for the extras a caller passed, as far as Rust can tell it.
fn type_name(v: &(dyn Any + Send + Sync)) -> &'static str {
    if v.is::<&str>() || v.is::<String>() {
        "string"
    } else if v.is::<i64>() {
        "int"
    } else if v.is::<bool>() {
        "bool"
    } else {
        "unknown"
    }
}

impl IdahoEstimated {
    fn check(&self, f: &Facts) -> Result<(), Error> {
        if f.jurisdiction() != JURISDICTION {
            return Err(Error::Invalid(format!(
                "idahotaxcalc: these are Idaho's rules, and the facts are for {}",
                f.jurisdiction()
            )));
        }
        if f.tax_year() != self.y.year {
            return Err(Error::Invalid(format!(
                "idahotaxcalc: these are the {} rules, and the facts are for {}",
                self.y.year,
                f.tax_year()
            )));
        }
        if let Some(s) = f.state() {
            return Err(Error::Invalid(format!(
                "idahotaxcalc: Idaho's rules read no extras, and the facts carry {}",
                type_name(s)
            )));
        }
        Ok(())
    }

    /// The rule every answer rests on, and the divergence the Explanation's
    /// account of the extension test depends on.
    fn notes(&self) -> Result<Notes, Error> {
        let mut n = Notes::default();
        n.rule(
            "Idaho Code § 63-3036A(a); Form 40 instructions (2025), Filing Requirements",
            format!(
                "estimated tax payments are required of corporations only, and Idaho doesn't require them of individuals, so none are required for {} and no underpayment penalty arises",
                self.y.year
            ),
        );
        let d = working::new_divergence("What the extension's prior-year test measures")
            .form(
                "The Form 40 instructions count 100% of state income taxes paid the year before. Form 51's worksheet takes the total tax on last year's return.",
                "Form 40 instructions (2025, EIN00046 03-02-2026), Extension of Time to File; Form 51 (EFO00092 09-18-2025), Tax Payment Worksheet, line 1",
            )
            .statute(
                "The extension holds if you've paid the total tax due on last year's return. That test applies only if you filed one.",
                "Idaho Code § 63-3033(a)",
            )
            // The statute and Form 51 agree. No target, schedule or penalty
            // turns on the test, because Idaho has no estimated payments.
            .uses(Reading::Statute)
            .affects(false)
            .build()?;
        n.diverges(&[d]);
        Ok(n)
    }

    /// The exception that Idaho requires no estimated payments of
    /// individuals, as the target.
    fn requirement_value(&self) -> Result<Requirement, Error> {
        let y = &self.y;
        let none = estimated::new_exception("Idaho doesn't require estimated payments").build()?;
        estimated::new_requirement()
            .required(
                false,
                format!(
                    "Idaho does not require individuals to make estimated tax payments for {}. There is no underpayment penalty. \
                     To get the automatic extension to file, pay enough by the April due date. That is at least {} of the {} tax. Or it is {} of the tax on your {} Idaho return.",
                    y.year,
                    y.extension_this_year,
                    y.year,
                    y.extension_prior_year,
                    y.year - 1
                ),
            )
            .exempt(none)
            .build()
    }
}

impl EstimatedTax for IdahoEstimated {
    fn requirement(&self, f: &Facts) -> Result<working::Result<Requirement>, Error> {
        self.check(f)?;
        let req = self.requirement_value()?;
        let n = self.notes()?;
        Ok(working::new_result(req).with(&n).build()?)
    }

    fn schedule(&self, f: &Facts) -> Result<working::Result<Vec<Installment>>, Error> {
        self.check(f)?;
        let mut n = self.notes()?;
        n.rule(
            "Idaho Code § 63-3036A",
            format!(
                "an individual has no estimated tax installments, so there are no due dates for {}",
                self.y.year
            ),
        );
        Ok(working::new_result(Vec::new()).with(&n).build()?)
    }

    fn penalty(&self, f: &Facts, as_of: Date) -> Result<working::Result<Penalty>, Error> {
        if as_of.is_zero() {
            return Err(Error::Invalid(
                "idahotaxcalc: a penalty needs the date to figure it to".into(),
            ));
        }
        self.check(f)?;
        let mut stop = as_of;
        let filed = f.return_filed();
        if !filed.is_zero() && filed.before(stop) {
            stop = filed;
        }
        let waiver = f.positions().waiver().unwrap_or_default();
        let req = self.requirement_value()?;
        let p = estimated::new_penalty(stop)
            .waiver(waiver)
            .because(req, &[])
            .build()?;
        let mut n = self.notes()?;
        n.rule(
            "Idaho Code §§ 63-3036A, 63-3046A",
            format!(
                "interest on underpaid estimated tax is charged to corporations only, so an individual's penalty for {} is zero",
                self.y.year
            ),
        );
        if waiver != WaiverKind::None {
            n.rule(
                "Idaho Code § 63-3036A",
                "there is no individual estimated tax penalty, so there is nothing to waive",
            );
        }
        Ok(working::new_result(p).with(&n).build()?)
    }

    fn explanation(&self) -> Explanation {
        explanation_for(&self.y)
    }
}

/// Written by hand and checked against research/estimated-payments/id.md.
/// Its figures come from the year, so a changed figure changes the text.
fn explanation_for(y: &EstimatedYear) -> Explanation {
    estimated::new_explanation(
        "Idaho does not require individuals to make estimated tax payments. It has no underpayment penalty for them. You can still prepay at any time with Form 51.",
    )
    .paragraph("Idaho's estimated tax law applies only to corporations. If your Idaho withholding falls short, you pay the balance with your return. Nothing is charged for the quarters along the way.")
    .paragraph(format!(
        "What does matter is the April payment if you want more time to file. Idaho gives an automatic six-month extension to file if you pay enough in time. Your withholding and payments by April's due date must be at least one of these. One is {} of this year's tax. The other is {} of the tax on last year's return. The second option is open only if you filed an Idaho return last year. If you are {} or less short of qualifying, you qualify without paying it.",
        y.extension_this_year,
        y.extension_prior_year,
        y.extension_minimum.whole()
    ))
    .paragraph(format!(
        "If you miss that test, the extension does not protect you. A penalty of {} a month then runs on the whole balance until you pay. It counts from the original due date, if you pay by the extended due date. If you pay later, Idaho's late-filing and late-payment penalties apply instead. They also count from the original due date. Interest runs on tax unpaid after the original due date in every case. The rate is {} a year for {}.",
        y.extension_penalty,
        y.interest,
        y.year + 1
    ))
    .versus_federal(format!(
        "Federally, you pay quarterly if you expect $1,000 or more due after withholding. Each short quarter is charged interest. Idaho has neither: no quarterly payments and no penalty for the year. Its {}-or-{} test looks like the federal safe harbour. But it decides the extension to file, and is judged once at the April due date. It has no 110% tier for higher incomes.",
        y.extension_this_year, y.extension_prior_year
    ))
    .source("Idaho Code §§ 63-3033, 63-3036A")
    .source("Form 40 instructions, tax year 2025 (EIN00046, 03-02-2026)")
    .source("Form 51, Estimated Payment of Individual Income Tax (EFO00092, 09-18-2025)")
    .source("Idaho State Tax Commission, Interest Rates")
    .build()
    .unwrap_or_default()
}
