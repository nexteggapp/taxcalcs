//! Tennessee levies no individual income tax, so its estimated tax rules are
//! that there is nothing to estimate. They still answer the shared
//! [`EstimatedTax`] questions, so that a caller asking about any state gets
//! an answer rather than an error: no payments are required, there are no
//! due dates, and the penalty is zero.
//!
//! Every state with no individual income tax is built in this same shape,
//! each in its own crate, because a jurisdiction crate depends on no other.

use std::any::Any;

use estimated::{
    Date, Error, EstimatedTax, Explanation, Facts, Installment, Jurisdiction, Penalty, Requirement,
};
use working::Notes;

use crate::estimated_y2026::year_2026;

/// jurisdiction is Tennessee's postal code.
const JURISDICTION: Jurisdiction = estimated::TENNESSEE;

/// One tax year's finding and the documents it rests on.
#[derive(Clone, Debug)]
pub(crate) struct EstimatedYear {
    pub(crate) year: i64,
    /// What Requirement says, in plain words.
    pub(crate) why: &'static str,
    /// Each document the finding rests on: its citation, and what it says,
    /// quoted.
    pub(crate) basis: Vec<Finding>,
}

/// A document's citation, and what it says.
#[derive(Clone, Debug)]
pub(crate) struct Finding {
    pub(crate) cite: &'static str,
    pub(crate) says: &'static str,
}

/// Returns Tennessee's estimated tax rules for a tax year, or
/// [`Error::NoRules`] for a year this build does not carry.
pub fn estimated_tax(year: i64) -> Result<Box<dyn EstimatedTax>, Error> {
    let y = match year {
        2026 => year_2026(),
        _ => return Err(estimated::no_rules(&JURISDICTION, year)),
    };
    Ok(Box::new(TennesseeEstimated {
        y,
        explanation: tennessee_explanation()?,
    }))
}

struct TennesseeEstimated {
    y: EstimatedYear,
    explanation: Explanation,
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

impl TennesseeEstimated {
    fn check(&self, f: &Facts) -> Result<(), Error> {
        if f.jurisdiction() != JURISDICTION {
            return Err(Error::Invalid(format!(
                "tennesseetaxcalc: these are Tennessee's rules, and the facts are for {}",
                f.jurisdiction()
            )));
        }
        if f.tax_year() != self.y.year {
            return Err(Error::Invalid(format!(
                "tennesseetaxcalc: these are the {} rules, and the facts are for {}",
                self.y.year,
                f.tax_year()
            )));
        }
        if let Some(s) = f.state() {
            return Err(Error::Invalid(format!(
                "tennesseetaxcalc: Tennessee's rules read no extras, and the facts carry {}",
                type_name(s)
            )));
        }
        Ok(())
    }

    /// Cites each document the finding rests on.
    fn notes(&self) -> Notes {
        let mut n = Notes::default();
        for b in &self.y.basis {
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

impl EstimatedTax for TennesseeEstimated {
    /// Never required and always exempt: with no tax there is no target, and
    /// no penalty can arise.
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
                "tennesseetaxcalc: a penalty needs the date to figure it to".into(),
            ));
        }
        self.check(f)?;
        let req = self.requirement_value()?;
        let p = estimated::new_penalty(as_of).because(req, &[]).build()?;
        Ok(working::new_result(p).with(&self.notes()).build()?)
    }

    fn explanation(&self) -> Explanation {
        self.explanation.clone()
    }
}

/// Written by hand and checked against research/estimated-payments/tn.md.
fn tennessee_explanation() -> Result<Explanation, Error> {
    estimated::new_explanation(
        "Tennessee has no income tax on individuals for 2026. It therefore has no estimated payments and no underpayment penalty.",
    )
    .paragraph("Tennessee's constitution bars state or local taxes on payroll or earned income. The Hall tax reached only interest and dividends. It was repealed for tax periods beginning on or after January 1, 2021. With nothing to pay the state during the year, nothing can be penalised as late.")
    .versus_federal("Federal estimated tax applies exactly as usual to Tennessee residents. Tennessee adds nothing to it.")
    .source("Tennessee Department of Revenue, Hall Income Tax")
    .source("Tenn. Const. art. II, § 28 (Tennessee Blue Book 2025-2026)")
    .build()
}
