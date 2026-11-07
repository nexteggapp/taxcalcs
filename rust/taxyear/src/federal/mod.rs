//! Computes a scenario's federal return with the rules engine,
//! `returns::rules::federal`: [`inputs`] turns the projection into the
//! engine's ReturnInputs, and [`compute`] runs the engine and reads what the
//! taxyear layer needs from its ReturnOutputs. Ported from the Go package
//! `taxyear/federal`. See docs/returns.md.

use std::cell::OnceCell;
use std::rc::Rc;

use buffa::MessageField;
use filing::Status;
use money::USCents;
use proto::federalv1::Form1040;
use proto::scenariosv1::{ScenarioInputs, ScenarioSummary, Working, YearProjection};
use proto::{commonv1, factsv1, returnsv1};
use returns::preparer;

use crate::model::{self, Context, Notes};

mod inputs;
mod summary;
mod working;

pub use inputs::{filing_status, inputs};

/// An error computing the federal return; its text is the Go error's.
#[derive(Debug, Clone, PartialEq, Eq, thiserror::Error)]
pub enum Error {
    /// ErrNoRules is returned for a tax year the rules engine has no
    /// parameters for.
    #[error("federal: no rules for this tax year")]
    NoRules,
    /// The rules engine could not be loaded.
    #[error("{0}")]
    Load(preparer::Error),
    /// The rules engine failed on the return's inputs.
    #[error("rules engine: {0}")]
    Engine(preparer::Error),
}

/// HasRules reports whether the rules engine has parameters for a year.
pub fn has_rules(year: i64) -> bool {
    let Ok(s) = server() else {
        return false;
    };
    let Ok(resp) = s.list_tax_years(&returnsv1::ListTaxYearsRequest::default()) else {
        return false;
    };
    resp.tax_years.iter().any(|y| i64::from(y.tax_year) == year)
}

/// Return is a scenario's federal return and what went into it. It is made
/// by [`compute`].
#[derive(Clone, Debug, Default)]
pub struct Return {
    // The messages are boxed: they are tens of kilobytes each, too much
    // for a debug build's stack frames.
    inputs: Box<returnsv1::ReturnInputs>,
    out: Box<returnsv1::ReturnOutputs>,
    /// Whether the return is the outputs' federal forms, as Go's pointer
    /// shares them; otherwise it is `own`.
    shared: bool,
    own: Box<factsv1::FederalReturn>,
    summary: ScenarioSummary,
    work: Vec<Working>,
    notes: Notes,
    status: Status,
}

impl Return {
    /// Form returns the federal return.
    pub fn form(&self) -> &factsv1::FederalReturn {
        if self.shared {
            &self.out.federal.forms
        } else {
            &self.own
        }
    }

    /// The federal return, to change, as Go's caller changes the shared
    /// one.
    pub fn form_mut(&mut self) -> &mut factsv1::FederalReturn {
        if self.shared {
            self.out
                .federal
                .get_or_insert_default()
                .forms
                .get_or_insert_default()
        } else {
            &mut self.own
        }
    }

    /// Inputs returns what the rules engine was given.
    pub fn inputs(&self) -> &returnsv1::ReturnInputs {
        &self.inputs
    }

    /// Outputs returns what the rules engine gave back.
    pub fn outputs(&self) -> &returnsv1::ReturnOutputs {
        &self.out
    }

    /// Working returns each line's working, by path.
    pub fn working(&self) -> &[Working] {
        &self.work
    }

    /// Notes returns the questions the return raised and what it leaves
    /// out.
    pub fn notes(&self) -> &Notes {
        &self.notes
    }

    /// Summary returns the totals and limits no form line holds.
    pub fn summary(&self) -> &ScenarioSummary {
        &self.summary
    }

    /// The summary, to change: it is shared with the Return.
    pub fn summary_mut(&mut self) -> &mut ScenarioSummary {
        &mut self.summary
    }

    /// Status is the filing status the return uses.
    pub fn status(&self) -> Status {
        self.status
    }

    /// AGI is adjusted gross income, Form 1040 line 11.
    pub fn agi(&self) -> USCents {
        model::value(&self.form().form_1040.adjusted_gross_income)
    }

    /// TotalTax is Form 1040 line 24.
    pub fn total_tax(&self) -> USCents {
        model::value(&self.form().form_1040.total_tax)
    }

    /// ExcessSocialSecurity is Schedule 3 line 11: Social Security tax
    /// withheld across a person's jobs over the year's maximum.
    pub fn excess_social_security(&self) -> USCents {
        model::value(&self.form().schedule_3.excess_social_security_withheld)
    }

    /// Tax2210 is the tax the estimated-payment rules measure, Form 2210
    /// line 4: lines 22 and 23 less the refundable credits, leaving out the
    /// § 4973 taxes on excess contributions, which § 6654(f) doesn't count.
    pub fn tax_2210(&self) -> USCents {
        let form = self.form();
        let f = &form.form_1040;
        let tax = model::value(&f.tax_after_credits) + model::value(&f.other_taxes);
        let refundable = model::value(&f.earned_income_credit)
            + model::value(&f.additional_child_tax_credit)
            + model::value(&f.american_opportunity_credit)
            + model::value(&form.schedule_3.net_premium_tax_credit)
            + model::value(&form.schedule_3.credit_for_federal_tax_on_fuels);
        USCents(0).max(tax - refundable - excess_contribution_tax(form))
    }

    /// SplitOverpayment divides line 34, the overpayment, into line 35a, the
    /// refund, and line 36, applied to next year's estimated tax, as the
    /// filed return divides it: the filed return's line 36, held to this
    /// overpayment, and the rest refunded. Without a filed return the
    /// overpayment is left whole, since dividing it is the household's
    /// choice.
    pub fn split_overpayment(&mut self, filed: Option<&Form1040>) {
        let f = self.form_mut().form_1040.get_or_insert_default();
        let over = model::value(&f.overpaid);
        let Some(filed) = filed.filter(|_| over > USCents(0)) else {
            f.refunded = MessageField::none();
            f.applied_to_next_year = MessageField::none();
            return;
        };
        let applied = over.min(USCents(0).max(model::value(&filed.applied_to_next_year)));
        f.applied_to_next_year = MessageField::some(model::money(applied));
        f.refunded = MessageField::some(model::money(over - applied));
    }
}

/// ExcessContributionTax is the § 4973 tax on excess contributions, Form
/// 5329 lines 25, 33, 41, 49 and 51, across the return's Forms 5329.
pub fn excess_contribution_tax(f: &factsv1::FederalReturn) -> USCents {
    let mut t = USCents(0);
    for x in &f.form_5329 {
        t += model::value(&x.additional_tax_on_traditional_ira_excess)
            + model::value(&x.additional_tax_on_roth_ira_excess)
            + model::value(&x.additional_tax_on_coverdell_esa_excess)
            + model::value(&x.additional_tax_on_archer_msa_excess)
            + model::value(&x.additional_tax_on_hsa_excess)
            + model::value(&x.additional_tax_on_able_excess);
    }
    t
}

thread_local! {
    static SERVER: OnceCell<Result<Rc<preparer::Server>, preparer::Error>> =
        const { OnceCell::new() };
}

/// server is the ReturnPreparer over the shared rules engine, which is
/// loaded and typechecked once (per thread, as the engine is).
fn server() -> Result<Rc<preparer::Server>, Error> {
    SERVER
        .with(|cell| {
            cell.get_or_init(|| preparer::Server::new().map(Rc::new))
                .clone()
        })
        .map_err(Error::Load)
}

/// Load loads and typechecks the rules engine, which [`compute`] otherwise
/// does the first time it runs.
pub fn load() -> Result<(), Error> {
    server().map(|_| ())
}

/// Compute runs the rules engine on a scenario's projection, with the
/// engine's line-by-line working. It returns [`Error::NoRules`] for a year
/// the engine has no parameters for.
pub fn compute(
    ctx: &Context,
    proj: &YearProjection,
    in_: &ScenarioInputs,
) -> Result<Return, Error> {
    compute_with_working(ctx, proj, in_, true)
}

/// Compute runs the rules engine on a scenario's projection, with the
/// engine's line-by-line working when `with_working` is set. It returns
/// [`Error::NoRules`] for a year the engine has no parameters for.
pub fn compute_with_working(
    ctx: &Context,
    proj: &YearProjection,
    in_: &ScenarioInputs,
    with_working: bool,
) -> Result<Return, Error> {
    if !has_rules(ctx.year()) {
        return Err(Error::NoRules);
    }
    let s = server()?;
    let (status, asked) = filing_status(&in_.facts);
    let (inputs, notes) = boxed_inputs(ctx, proj, in_);
    let (inputs, out) = prepare(&s, inputs, with_working)?;
    let shared = out.federal.forms.is_set();
    let mut r = Return {
        inputs,
        out,
        shared,
        status,
        ..Default::default()
    };
    let year = ctx.year();
    let form = r.form_mut();
    if form.form_1040.is_unset() {
        form.form_1040 = MessageField::some(Form1040 {
            tax_year: year as i32,
            ..Default::default()
        });
    }
    if asked {
        r.notes.ask(model::new_question(
            "federal/filing-status",
            "federal.form_1040.filing_status",
            "What is your filing status?",
            "Single is used until you say.",
            "facts.elections",
            &[],
        ));
    }
    r.notes.merge(&notes);
    r.work = working::working(&r.out);
    r.diagnostics();
    r.summary = summary::summarise(ctx, proj, in_, &mut r);
    Ok(r)
}

/// The inputs, boxed. This and [`prepare`] keep the return's messages,
/// tens of kilobytes each, off compute's stack frame, which in a debug
/// build holds every temporary.
fn boxed_inputs(
    ctx: &Context,
    proj: &YearProjection,
    in_: &ScenarioInputs,
) -> (Box<returnsv1::ReturnInputs>, Notes) {
    let (inputs, notes) = inputs(ctx, proj, in_);
    (Box::new(inputs), notes)
}

/// Prepares the return from the inputs, giving them back with the outputs.
fn prepare(
    s: &preparer::Server,
    inputs: Box<returnsv1::ReturnInputs>,
    with_working: bool,
) -> Result<(Box<returnsv1::ReturnInputs>, Box<returnsv1::ReturnOutputs>), Error> {
    let mut inputs = inputs;
    let mut req: Box<returnsv1::PrepareRequest> = Box::default();
    req.include_working = with_working;
    std::mem::swap(req.inputs.get_or_insert_default(), &mut *inputs);
    let mut resp = match s.prepare(&req) {
        Ok(r) => r,
        Err(e) => return Err(Error::Engine(e)),
    };
    std::mem::swap(req.inputs.get_or_insert_default(), &mut *inputs);
    let mut out: Box<returnsv1::ReturnOutputs> = Box::default();
    std::mem::swap(resp.outputs.get_or_insert_default(), &mut *out);
    Ok((inputs, out))
}

/// priorStandardDeduction is last year's standard deduction for last
/// year's return's filing status and the household's ages and blindness,
/// from the rules engine. It is None for a year without rules.
fn prior_standard_deduction(
    ctx: &Context,
    f: &Form1040,
) -> Option<MessageField<commonv1::Money, buffa::Inline<commonv1::Money>>> {
    let year = ctx.year() - 1;
    if !has_rules(year) {
        return None;
    }
    let s = server().ok()?;
    let status = inputs::status_of(f.filing_status);
    let mut hh = returnsv1::Household {
        marital_status: inputs::marital_of(status).into(),
        taxpayer: MessageField::some(returnsv1::Person {
            id: inputs::YOU_ID.to_string(),
            blind: Some(f.filer_blind),
            ..Default::default()
        }),
        ..Default::default()
    };
    if status == Status::MarriedFilingJointly || status == Status::MarriedFilingSeparately {
        hh.spouse = MessageField::some(returnsv1::Person {
            id: inputs::SPOUSE_ID.to_string(),
            blind: Some(f.spouse_blind),
            ..Default::default()
        });
    }
    for p in &ctx.facts().people {
        if p.relationship == factsv1::Relationship::RELATIONSHIP_YOU && p.date_of_birth.year != 0 {
            hh.taxpayer.get_or_insert_default().date_of_birth = p.date_of_birth.clone();
        } else if p.relationship == factsv1::Relationship::RELATIONSHIP_SPOUSE
            && hh.spouse.is_set()
            && p.date_of_birth.year != 0
        {
            hh.spouse.get_or_insert_default().date_of_birth = p.date_of_birth.clone();
        }
    }
    let resp = s
        .prepare(&returnsv1::PrepareRequest {
            inputs: MessageField::some(returnsv1::ReturnInputs {
                tax_year: year as i32,
                federal: MessageField::some(returnsv1::Federal1040Inputs {
                    household: MessageField::some(hh),
                    elections: MessageField::some(returnsv1::Elections {
                        filing_status: inputs::election_of(status).into(),
                        ..Default::default()
                    }),
                    ..Default::default()
                }),
                ..Default::default()
            }),
            ..Default::default()
        })
        .ok()?;
    Some(
        resp.outputs
            .federal
            .workpapers
            .standard_deduction
            .effective_standard_deduction
            .clone(),
    )
}

#[cfg(test)]
mod tests;
