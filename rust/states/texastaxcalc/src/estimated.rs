//! Texas levies no individual income tax, so its estimated tax rules are
//! that there is nothing to estimate. They still answer the shared
//! [`EstimatedTax`] questions, so that a caller asking about any state gets
//! an answer rather than an error: no payments are required, there are no
//! due dates, and the penalty is zero.
//!
//! Every state with no individual income tax is built in this same shape,
//! each in its own crate, because a jurisdiction crate depends on no other.

use std::any::Any;

use estimated::{Date, Error, EstimatedTax, Explanation, Facts, Installment, Penalty, Requirement};
use working::Notes;

/// Texas's postal code.
const JURISDICTION: estimated::Jurisdiction = estimated::TEXAS;

/// One tax year's finding and the documents it rests on.
pub(crate) struct EstimatedYear {
    pub(crate) year: i64,

    /// What Requirement says, in plain words.
    pub(crate) why: &'static str,

    /// Each document the finding rests on: its citation, and what it says,
    /// quoted.
    pub(crate) basis: Vec<Finding>,
}

/// One document's citation and what it says.
pub(crate) struct Finding {
    pub(crate) cite: &'static str,
    pub(crate) says: &'static str,
}

/// Returns Texas's estimated tax rules for a tax year, or
/// [`Error::NoRules`] for a year this build does not carry.
pub fn estimated_tax(year: i64) -> Result<Box<dyn EstimatedTax>, Error> {
    let y = match year {
        2026 => crate::estimated_y2026::year_2026(),
        _ => return Err(estimated::no_rules(&JURISDICTION, year)),
    };
    Ok(Box::new(TexasEstimated { y }))
}

struct TexasEstimated {
    y: EstimatedYear,
}

/// Go's `%T` of a jurisdiction's extras, for the types a caller is likely to
/// pass by mistake.
fn type_name(v: &(dyn Any + Send + Sync)) -> &'static str {
    if v.is::<String>() || v.is::<&str>() {
        "string"
    } else if v.is::<i64>() || v.is::<i32>() {
        "int"
    } else {
        "unknown"
    }
}

impl TexasEstimated {
    fn check(&self, f: &Facts) -> Result<(), Error> {
        if f.jurisdiction() != JURISDICTION {
            return Err(Error::Invalid(format!(
                "texastaxcalc: these are Texas's rules, and the facts are for {}",
                f.jurisdiction()
            )));
        }
        if f.tax_year() != self.y.year {
            return Err(Error::Invalid(format!(
                "texastaxcalc: these are the {} rules, and the facts are for {}",
                self.y.year,
                f.tax_year()
            )));
        }
        if let Some(s) = f.state() {
            return Err(Error::Invalid(format!(
                "texastaxcalc: Texas's rules read no extras, and the facts carry {}",
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
    fn requirement(&self) -> Result<Requirement, Error> {
        let none = estimated::new_exception("No income tax").build()?;
        estimated::new_requirement()
            .required(false, self.y.why)
            .exempt(none)
            .build()
    }
}

impl EstimatedTax for TexasEstimated {
    /// Never required and always exempt: with no tax there is no target,
    /// and no penalty can arise.
    fn requirement(&self, f: &Facts) -> Result<working::Result<Requirement>, Error> {
        self.check(f)?;
        let req = self.requirement()?;
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
                "texastaxcalc: a penalty needs the date to figure it to".into(),
            ));
        }
        self.check(f)?;
        let req = self.requirement()?;
        let p = estimated::new_penalty(as_of).because(req, &[]).build()?;
        Ok(working::new_result(p).with(&self.notes()).build()?)
    }

    fn explanation(&self) -> Explanation {
        texas_explanation()
    }
}

/// Written by hand and checked against research/estimated-payments/tx.md.
fn texas_explanation() -> Explanation {
    estimated::new_explanation(
        "Texas has no individual income tax. It therefore has no estimated payments and no underpayment penalty.",
    )
    .paragraph("Texas does not tax the income of individuals. Texas voters adopted Proposition 4 in November 2019. Since then, the Texas constitution bars a tax on the net incomes of individuals. The bar covers an individual's share of partnership income. With nothing to pay the state during the year, nothing can be penalised as late.")
    .versus_federal("Federal estimated tax applies exactly as usual to Texas residents. Texas adds nothing to it.")
    .source("Tex. Const. art. VIII, § 24-a, added by H.J.R. 38, 86th Leg. (2019)")
    .source("Office of the Texas Governor, proclamation of the November 5, 2019 constitutional amendment election")
    .build()
    .unwrap_or_default()
}
