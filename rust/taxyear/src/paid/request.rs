//! The ReturnInputs that ask a jurisdiction's estimated tax rules, ported
//! from Go's `taxyear/paid/request.go`.

use buffa::{EnumValue, Inline, MessageField};
use money::USCents;
use proto::factsv1::{self, __buffa::oneof::election::Choice, prior_year_return};
use proto::returnsv1::{
    self, __buffa::oneof::estimated_state_inputs::Inputs as StateInputs,
    __buffa::oneof::estimated_tax_inputs::ThisYear, estimated_tax_inputs as eti,
};
use proto::{commonv1, federalv1};

use super::{Error, Prior, Runner};
use crate::internal::civil;
use crate::model;

/// A money field of a returns/v1 message.
type MoneyField = MessageField<commonv1::Money, Inline<commonv1::Money>>;

fn usd(c: USCents) -> MoneyField {
    model::money(c).into()
}

impl Prior {
    /// Reports a return on record for last year: a status, or figures.
    fn has_prior(&self) -> bool {
        self.status != prior_year_return::Status::STATUS_UNSPECIFIED || self.tax_set
    }

    /// Reports last year's figures from a return filed.
    fn has_figures(&self) -> bool {
        self.has_prior() && self.status != prior_year_return::Status::STATUS_NONE && self.tax_set
    }
}

impl Runner<'_> {
    /// The ReturnInputs that asks the jurisdiction's estimated tax rules:
    /// this year's tax as a caller estimate, last year's return, the
    /// residence, the withholding, the payments — made only, or every one
    /// that counts — and the elections. It is asked as of December 31 of
    /// the next year, after every due date, so the penalty runs as far as
    /// the rules let it and every payment given counts as made; the payments
    /// planned are told apart by leaving them out.
    pub(super) fn request(&mut self, made_only: bool) -> Result<returnsv1::ReturnInputs, Error> {
        let year = self.in_.ctx.year();
        let mut est = returnsv1::EstimatedTaxInputs {
            as_of: civil::must(year + 1, 12, 31).field(),
            jurisdictions: vec![self.j.clone()],
            ..Default::default()
        };
        let mut fed = returnsv1::Federal1040Inputs {
            elections: MessageField::some(returnsv1::Elections {
                filing_status: EnumValue::from(self.in_.status as i32),
                ..Default::default()
            }),
            ..Default::default()
        };
        let mut states = Vec::new();
        if self.j != "US" {
            states.push(returnsv1::StateInputs {
                state: self.j.clone(),
                ..Default::default()
            });
        }
        self.this_year(&mut est)?;
        self.prior_year(&mut est, &mut fed);
        self.residence(&mut est);
        self.payments(&mut est, made_only)?;
        self.elections(&mut est);
        if let Some(s) = self.state_inputs() {
            est.state_inputs.push(s);
        }
        Ok(returnsv1::ReturnInputs {
            schema_version: 1,
            tax_year: year as i32,
            federal: MessageField::some(fed),
            states,
            estimated_tax: MessageField::some(est),
            ..Default::default()
        })
    }

    /// Gives this year's tax, when known, and the withholding, in total and
    /// by date: a caller estimate, or last year's figures alone when there
    /// is neither.
    fn this_year(&mut self, est: &mut returnsv1::EstimatedTaxInputs) -> Result<(), Error> {
        let withheld = self.withholding()?;
        let mut cj = eti::caller_estimate::Jurisdiction {
            jurisdiction: self.j.clone(),
            ..Default::default()
        };
        if self.tax_known {
            cj.tax = usd(self.tax);
        }
        let mut total = USCents(0);
        for w in &withheld {
            total += w.v;
            est.withholding.push(eti::Withholding {
                jurisdiction: self.j.clone(),
                date: w.on.field(),
                amount: usd(w.v),
                ..Default::default()
            });
        }
        if !withheld.is_empty() {
            cj.withheld = usd(total);
        }
        if cj.tax.is_unset() && cj.withheld.is_unset() {
            est.this_year = Some(ThisYear::LastYearOnly(Box::default()));
            return Ok(());
        }
        est.this_year = Some(ThisYear::CallerEstimate(Box::new(eti::CallerEstimate {
            jurisdictions: vec![cj],
            ..Default::default()
        })));
        Ok(())
    }

    /// Gives last year's return: federally, ReturnInputs.prior_year,
    /// federal_prior_return and whether the household were US residents all
    /// year; for a state, its PriorYear entry, with the federal AGI the
    /// state's return started from in ReturnInputs.prior_year. No return on
    /// record is federal_prior_return unset, or an entry that names the
    /// state alone.
    fn prior_year(
        &self,
        est: &mut returnsv1::EstimatedTaxInputs,
        fed: &mut returnsv1::Federal1040Inputs,
    ) {
        let p = &self.prior;
        let status = if !p.has_prior() {
            eti::PriorReturn::PRIOR_RETURN_UNSPECIFIED
        } else if p.status == prior_year_return::Status::STATUS_NONE {
            eti::PriorReturn::PRIOR_RETURN_NOT_FILED
        } else {
            eti::PriorReturn::PRIOR_RETURN_FULL_YEAR
        };
        if p.has_figures() && p.agi_set {
            fed.prior_year = MessageField::some(returnsv1::PriorYear {
                prior_year_agi: usd(p.agi),
                ..Default::default()
            });
        }
        if self.j == "US" {
            est.federal_prior_return = status.into();
            if p.has_prior()
                && let Some(full) = p.full_year
            {
                est.prior_year_us_citizen_or_resident = Some(full);
            }
            if p.has_figures() {
                fed.prior_year.get_or_insert_default().prior_year_total_tax = usd(p.tax);
            }
            return;
        }
        let mut py = eti::PriorYear {
            jurisdiction: self.j.clone(),
            r#return: status.into(),
            ..Default::default()
        };
        if p.has_prior()
            && let Some(full) = p.full_year
        {
            py.residency = if full {
                eti::Residency::RESIDENCY_FULL_YEAR
            } else {
                eti::Residency::RESIDENCY_PART_YEAR
            }
            .into();
        }
        if p.has_figures() {
            py.tax = usd(p.tax);
            if p.state_agi_set {
                py.agi = usd(p.state_agi);
            }
            py.filing_status = EnumValue::<federalv1::FilingStatus>::from(self.in_.status as i32);
        }
        est.prior_years.push(py);
    }

    /// Gives a state the facts' residence periods, an open end read as the
    /// year's first or last day.
    fn residence(&self, est: &mut returnsv1::EstimatedTaxInputs) {
        if self.j == "US" {
            return;
        }
        let ctx = &self.in_.ctx;
        for res in &ctx.facts().residence {
            let (Ok(mut from), Ok(mut to)) =
                (civil::from_field(&res.from), civil::from_field(&res.to))
            else {
                continue;
            };
            if from.is_zero() {
                from = ctx.year_start();
            }
            if to.is_zero() {
                to = ctx.year_end();
            }
            est.residence.push(eti::ResidencePeriod {
                state: res.state.clone(),
                from: from.field(),
                to: to.field(),
                ..Default::default()
            });
        }
    }

    /// Gives the jurisdiction's payments that count, and when payments could
    /// first be expected. Last year's overpayment applied is credited on
    /// January 1, so it meets the first installment, as it does on the
    /// return.
    fn payments(
        &mut self,
        est: &mut returnsv1::EstimatedTaxInputs,
        made_only: bool,
    ) -> Result<(), Error> {
        let year = self.in_.ctx.year();
        let start = self.in_.ctx.year_start();
        let j = self.j.clone();
        let Some(p) = self.in_.payments_for(&j) else {
            return Ok(());
        };
        for x in p.payments() {
            if made_only && !x.made() {
                continue;
            }
            let on = if x.from_last_year() { start } else { x.on() };
            if on.is_zero() {
                return Err(Error::MissingDate);
            }
            est.payments.push(returnsv1::EstimatedPayment {
                jurisdiction: j.clone(),
                date_paid: on.field(),
                designated_tax_year: year as i32,
                amount: usd(x.amount()),
                ..Default::default()
            });
        }
        let from = p.payments_from();
        if !from.is_zero() {
            est.requirement_first_met = from.field();
        }
        Ok(())
    }

    /// Gives the withholding-dates election and a waiver request.
    /// Withholding is credited evenly unless the household elected
    /// otherwise.
    fn elections(&self, est: &mut returnsv1::EstimatedTaxInputs) {
        est.withholding_credit = eti::WithholdingCredit::WITHHOLDING_CREDIT_EVENLY.into();
        for x in &self.in_.ctx.facts().elections {
            match &x.choice {
                Some(Choice::WithholdingDates(w)) if w.jurisdiction == self.j && w.as_withheld => {
                    est.withholding_credit =
                        eti::WithholdingCredit::WITHHOLDING_CREDIT_AS_WITHHELD.into();
                }
                Some(Choice::Waiver(w)) if w.jurisdiction == self.j => {
                    if let Some(wv) = waiver_of(w) {
                        est.waiver = MessageField::some(wv);
                    }
                }
                _ => {}
            }
        }
    }

    /// The state's own EstimatedStateInputs entry, for a state whose rules
    /// read a figure the core doesn't hold, or None: this year's AGI as the
    /// state measures it, this year's federal AGI, and last year's
    /// withholding and taxable income.
    fn state_inputs(&self) -> Option<returnsv1::EstimatedStateInputs> {
        let state_agi = self.state_agi();
        let agi = || state_agi.map_or_else(MessageField::none, usd);
        let p = &self.prior;
        let prior_withheld = || {
            if !p.has_figures() || !p.withheld_set {
                return MessageField::none();
            }
            usd(p.withheld)
        };
        let inputs = match self.j.as_str() {
            "AL" => StateInputs::Al(Box::new(returnsv1::AlabamaEstimatedInputs {
                alabama_agi: agi(),
                ..Default::default()
            })),
            "AZ" => StateInputs::Az(Box::new(returnsv1::ArizonaEstimatedInputs {
                federal_agi: usd(self.in_.federal_agi),
                ..Default::default()
            })),
            "CA" => StateInputs::Ca(Box::new(returnsv1::CaliforniaEstimatedInputs {
                california_agi: agi(),
                prior_year_withholding: prior_withheld(),
                ..Default::default()
            })),
            "IL" => {
                let w = prior_withheld();
                if w.is_unset() {
                    return None;
                }
                StateInputs::Il(Box::new(returnsv1::IllinoisEstimatedInputs {
                    prior_year_withheld: w,
                    ..Default::default()
                }))
            }
            "MD" => StateInputs::Md(Box::new(returnsv1::MarylandEstimatedInputs {
                maryland_agi: agi(),
                ..Default::default()
            })),
            "MI" => StateInputs::Mi(Box::new(returnsv1::MichiganEstimatedInputs {
                prior_year_withheld: prior_withheld(),
                ..Default::default()
            })),
            "NM" => {
                let w = prior_withheld();
                if w.is_unset() {
                    return None;
                }
                StateInputs::Nm(Box::new(returnsv1::NewMexicoEstimatedInputs {
                    prior_year_withheld: w,
                    ..Default::default()
                }))
            }
            "PA" => {
                let mut pa = returnsv1::PennsylvaniaEstimatedInputs {
                    prior_year_withheld: prior_withheld(),
                    ..Default::default()
                };
                if p.has_figures() && p.taxable_set {
                    pa.prior_year_taxable_income = usd(p.taxable);
                }
                StateInputs::Pa(Box::new(pa))
            }
            "VA" => StateInputs::Va(Box::new(returnsv1::VirginiaEstimatedInputs {
                virginia_agi: agi(),
                ..Default::default()
            })),
            _ => return None,
        };
        Some(returnsv1::EstimatedStateInputs {
            state: self.j.clone(),
            inputs: Some(inputs),
            ..Default::default()
        })
    }
}

/// The waiver asked for, or None while the question is unanswered.
fn waiver_of(w: &factsv1::WaiverRequest) -> Option<eti::Waiver> {
    use factsv1::waiver_request::Reason;
    let cause = match w.reason.as_known() {
        Some(Reason::REASON_NONE) => eti::WaiverCause::WAIVER_CAUSE_NONE,
        Some(Reason::REASON_CASUALTY) => eti::WaiverCause::WAIVER_CAUSE_CASUALTY,
        Some(Reason::REASON_DISASTER) => eti::WaiverCause::WAIVER_CAUSE_DISASTER,
        Some(Reason::REASON_RETIRED) => eti::WaiverCause::WAIVER_CAUSE_RETIREMENT,
        Some(Reason::REASON_DISABLED) => eti::WaiverCause::WAIVER_CAUSE_DISABILITY,
        _ => return None,
    };
    let mut out = eti::Waiver {
        cause: cause.into(),
        ..Default::default()
    };
    if cause != eti::WaiverCause::WAIVER_CAUSE_NONE
        && let Ok(d) = civil::from_field(&w.on)
        && !d.is_zero()
    {
        out.occurred = d.field();
    }
    Some(out)
}
