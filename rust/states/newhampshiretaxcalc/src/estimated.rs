//! New Hampshire levies no individual income tax, so its estimated tax rules
//! are that there is nothing to estimate. They still answer the shared
//! [`EstimatedTax`] questions, so that a caller asking about any state gets
//! an answer rather than an error: no payments are required, there are no
//! due dates, and the penalty is zero.
//!
//! Every state with no individual income tax is built in this same shape,
//! each in its own crate, because a jurisdiction crate imports no other.

use std::any::Any;

use estimated::{Date, Error, EstimatedTax, Explanation, Facts, Installment, Penalty, Requirement};
use working::Notes;

/// jurisdiction is New Hampshire's postal code.
const JURISDICTION: estimated::Jurisdiction = estimated::NEW_HAMPSHIRE;

/// One tax year's finding and the documents it rests on.
#[derive(Clone, Copy, Debug)]
pub(crate) struct EstimatedYear {
    pub(crate) year: i64,

    /// What Requirement says, in plain words.
    pub(crate) why: &'static str,

    /// Each document the finding rests on: its citation, and what it says,
    /// quoted.
    pub(crate) basis: &'static [Finding],
}

/// One document's citation and what it says.
#[derive(Clone, Copy, Debug)]
pub(crate) struct Finding {
    pub(crate) cite: &'static str,
    pub(crate) says: &'static str,
}

/// The years this build carries.
fn estimated_year(year: i64) -> Option<EstimatedYear> {
    match year {
        2026 => Some(crate::estimated_y2026::year_2026()),
        _ => None,
    }
}

/// Returns New Hampshire's estimated tax rules for a tax year, or
/// [`Error::NoRules`] for a year this build does not carry.
pub fn estimated_tax(year: i64) -> Result<Box<dyn EstimatedTax>, Error> {
    match estimated_year(year) {
        Some(y) => Ok(Box::new(NewHampshireEstimated { y })),
        None => Err(estimated::no_rules(&JURISDICTION, year)),
    }
}

/// Go's `%T` of the extras, as far as it can be told from here.
fn type_name(v: &(dyn Any + Send + Sync)) -> &'static str {
    if v.is::<&str>() || v.is::<String>() {
        "string"
    } else {
        "an unknown type"
    }
}

struct NewHampshireEstimated {
    y: EstimatedYear,
}

impl NewHampshireEstimated {
    fn check(&self, f: &Facts) -> Result<(), Error> {
        if f.jurisdiction() != JURISDICTION {
            return Err(Error::Invalid(format!(
                "newhampshiretaxcalc: these are New Hampshire's rules, and the facts are for {}",
                f.jurisdiction()
            )));
        }
        if f.tax_year() != self.y.year {
            return Err(Error::Invalid(format!(
                "newhampshiretaxcalc: these are the {} rules, and the facts are for {}",
                self.y.year,
                f.tax_year()
            )));
        }
        if let Some(s) = f.state() {
            return Err(Error::Invalid(format!(
                "newhampshiretaxcalc: New Hampshire's rules read no extras, and the facts carry {}",
                type_name(s)
            )));
        }
        Ok(())
    }

    /// Cites each document the finding rests on.
    fn notes(&self) -> Notes {
        let mut n = Notes::default();
        for b in self.y.basis {
            n.rule(b.cite, b.says);
        }
        n
    }

    /// The exception that there is no income tax, as the target.
    fn requirement_value(&self) -> Result<Requirement, Error> {
        let none = estimated::new_exception("No income tax").build()?;
        estimated::new_requirement()
            .required(false, self.y.why)
            .exempt(none)
            .build()
    }
}

impl EstimatedTax for NewHampshireEstimated {
    /// Never required and always exempt: with no tax there is no target,
    /// and no penalty can arise.
    fn requirement(&self, f: &Facts) -> Result<working::Result<Requirement>, Error> {
        self.check(f)?;
        let req = self.requirement_value()?;
        Ok(working::new_result(req).with(&self.notes()).build()?)
    }

    /// No installments, because nothing is due.
    fn schedule(&self, f: &Facts) -> Result<working::Result<Vec<Installment>>, Error> {
        self.check(f)?;
        Ok(working::new_result(Vec::new())
            .with(&self.notes())
            .build()?)
    }

    /// Zero, figured to `as_of`.
    fn penalty(&self, f: &Facts, as_of: Date) -> Result<working::Result<Penalty>, Error> {
        if as_of.is_zero() {
            return Err(Error::Invalid(
                "newhampshiretaxcalc: a penalty needs the date to figure it to".into(),
            ));
        }
        self.check(f)?;
        let req = self.requirement_value()?;
        let p = estimated::new_penalty(as_of).because(req, &[]).build()?;
        Ok(working::new_result(p).with(&self.notes()).build()?)
    }

    /// Written by hand and checked against research/estimated-payments/nh.md.
    fn explanation(&self) -> Explanation {
        estimated::new_explanation(
            "New Hampshire has no individual income tax for 2026. It therefore has no estimated payments and no underpayment penalty.",
        )
        .paragraph("New Hampshire does not tax wages. Its interest and dividends tax was repealed from January 1, 2025. It last applied for 2024. With nothing to pay the state during the year, nothing can be penalised as late.")
        .versus_federal("Federal estimated tax applies exactly as usual to New Hampshire residents. New Hampshire adds no individual income tax to it.")
        .source("N.H. RSA ch. 77, Taxation of Incomes (repealed)")
        .source("New Hampshire Department of Revenue Administration, Interest & Dividends Tax")
        .build()
        .unwrap_or_default()
    }
}
