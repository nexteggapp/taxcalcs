//! Expands a RetirementModel's additive parts, IRA and direct HSA
//! contributions, into items. Its adjustment parts, MaxOutWorkplacePlan and
//! WorkplacePlanTotal, change the person's employer items, and the
//! employer model's MaxOut applies them. Ported from the Go package
//! `taxyear/model/retirement`. See docs/models.md.

mod hsa;

pub use hsa::{LAST_MONTH_RULE, hsa_limit};

use proto::commonv1;
use proto::scenariosv1::{
    self,
    __buffa::oneof::retirement_part::{Part, from_forms::form::Form as FormKind},
    ContributionFigures, ContributionKind, ContributionTotals, IraKind, ItemKind, ItemStatus,
    RetirementModel, RetirementPart, RetirementProjection, RetirementTotals,
    origin::Basis,
};

use super::account::origin;
use super::{Context, Error};
use crate::internal::civil::{self, Date};
use crate::model;

/// Returns the person's plan adjustment part, or nil.
pub fn plan_part(m: &RetirementModel) -> Option<&RetirementPart> {
    m.parts.iter().find(|p| {
        matches!(
            p.part,
            Some(Part::MaxOutWorkplacePlan(_)) | Some(Part::WorkplacePlanTotal(_))
        )
    })
}

fn usd(minor: i64) -> commonv1::Money {
    commonv1::Money {
        minor,
        currency: "USD".to_string(),
        ..Default::default()
    }
}

fn contribution(
    id: String,
    d: Date,
    status: ItemStatus,
    basis: Basis,
    part: &str,
    evidence: &[&str],
    f: ContributionFigures,
) -> scenariosv1::Item {
    scenariosv1::Item {
        id,
        date: d.field(),
        status: status.into(),
        kind: ItemKind::ITEM_KIND_CONTRIBUTION.into(),
        origin: origin(basis, part, evidence).into(),
        figures: Some(f.into()),
        ..Default::default()
    }
}

/// Returns the person's contributions as items. An IRA contribution may be
/// dated up to next April 15 and still count for this year.
pub fn expand(ctx: &Context, m: &RetirementModel) -> Result<RetirementProjection, Error> {
    let mut out = RetirementProjection {
        id: m.id.clone(),
        ..Default::default()
    };
    let (mut so_far_trad, mut so_far_roth, mut so_far_hsa) = (0i64, 0i64, 0i64);
    let (mut rest_trad, mut rest_roth, mut rest_hsa) = (0i64, 0i64, 0i64);
    let prefix = model::item_id(&["retirement", &m.id]);
    let wrap = |p: &RetirementPart, e: civil::Error| {
        Error::Expansion(format!("model.retirement[{}].parts[{}]: {e}", m.id, p.id))
    };
    for p in &m.parts {
        let (mut date, f) = match &p.part {
            Some(Part::IraContribution(c)) => {
                let mut kind = ContributionKind::CONTRIBUTION_KIND_TRADITIONAL_IRA;
                if c.kind == IraKind::IRA_KIND_ROTH {
                    kind = ContributionKind::CONTRIBUTION_KIND_ROTH_IRA;
                    rest_roth += c.amount.minor;
                } else {
                    rest_trad += c.amount.minor;
                }
                let d = civil::from_field(&c.date).map_err(|e| wrap(p, e))?;
                (
                    d,
                    ContributionFigures {
                        kind: kind.into(),
                        amount: model::money(model::value(&c.amount)).into(),
                        ..Default::default()
                    },
                )
            }
            Some(Part::HsaContribution(c)) => {
                rest_hsa += c.amount.minor;
                let d = civil::from_field(&c.date).map_err(|e| wrap(p, e))?;
                (
                    d,
                    ContributionFigures {
                        kind: ContributionKind::CONTRIBUTION_KIND_HSA.into(),
                        amount: model::money(model::value(&c.amount)).into(),
                        ..Default::default()
                    },
                )
            }
            Some(Part::FromForms(ff)) => {
                for form in &ff.forms {
                    if !model::usable(&form.source) {
                        continue;
                    }
                    let d = ctx.year_end();
                    let made = |id: String, kind: ContributionKind, amount| {
                        contribution(
                            id,
                            d,
                            ItemStatus::ITEM_STATUS_MADE,
                            Basis::BASIS_DOCUMENT,
                            &p.id,
                            &[&form.id],
                            ContributionFigures {
                                kind: kind.into(),
                                amount: model::money(amount).into(),
                                ..Default::default()
                            },
                        )
                    };
                    match &form.form {
                        Some(FormKind::Form5498(f5498)) => {
                            // Form 5498 (26 U.S.C. §§ 219, 408, 408A; Schedule 1 line 20):
                            // Box 1 (ira_contributions) reports traditional IRA contributions made for the
                            // tax year, and box 10 (roth_ira_contributions) reports Roth IRA contributions.
                            // Rollovers (box 2) and Roth conversions (box 3) are excluded because they do
                            // not count against the annual § 219 / § 408A contribution limit.
                            let trad = model::value(&f5498.ira_contributions);
                            if trad.0 > 0 {
                                so_far_trad += trad.0;
                                out.items.push(made(
                                    model::item_id(&[
                                        &prefix,
                                        &d.to_string(),
                                        "contribution",
                                        &form.id,
                                        "trad",
                                    ]),
                                    ContributionKind::CONTRIBUTION_KIND_TRADITIONAL_IRA,
                                    trad,
                                ));
                            }
                            let roth = model::value(&f5498.roth_ira_contributions);
                            if roth.0 > 0 {
                                so_far_roth += roth.0;
                                out.items.push(made(
                                    model::item_id(&[
                                        &prefix,
                                        &d.to_string(),
                                        "contribution",
                                        &form.id,
                                        "roth",
                                    ]),
                                    ContributionKind::CONTRIBUTION_KIND_ROTH_IRA,
                                    roth,
                                ));
                            }
                        }
                        Some(FormKind::Form5498Sa(sa)) => {
                            // Form 5498-SA (26 U.S.C. §§ 220, 223; Form 8889 Part I line 2; Schedule 1 line 13):
                            // Sum box 2 (total_contributions_made_in_year), box 3 (contributions_made_in_next_year_for_year,
                            // designated for the tax year before the April filing deadline under § 223(d)(4)(B)),
                            // and box 1 (archer_msa_contributions). Box 4 (rollover_contributions) is excluded
                            // under § 223(f)(5).
                            let hsa = model::value(&sa.total_contributions_made_in_year)
                                + model::value(&sa.contributions_made_in_next_year_for_year)
                                + model::value(&sa.archer_msa_contributions);
                            if hsa.0 > 0 {
                                so_far_hsa += hsa.0;
                                out.items.push(made(
                                    model::item_id(&[
                                        &prefix,
                                        &d.to_string(),
                                        "contribution",
                                        &form.id,
                                    ]),
                                    ContributionKind::CONTRIBUTION_KIND_HSA,
                                    hsa,
                                ));
                            }
                        }
                        _ => {}
                    }
                }
                continue;
            }
            _ => continue,
        };
        if date.is_zero() {
            date = ctx.year_end();
        }
        out.items.push(contribution(
            model::item_id(&[&prefix, &date.to_string(), "contribution", &p.id]),
            date,
            ItemStatus::ITEM_STATUS_EXPECTED,
            Basis::BASIS_ASSUMED,
            &p.id,
            &[],
            f,
        ));
    }
    model::sort_items(&mut out.items);
    let totals = |trad: i64, roth: i64, hsa: i64| RetirementTotals {
        items: ContributionTotals {
            traditional_ira: usd(trad).into(),
            roth_ira: usd(roth).into(),
            hsa: usd(hsa).into(),
            ..Default::default()
        }
        .into(),
        ..Default::default()
    };
    out.so_far = totals(so_far_trad, so_far_roth, so_far_hsa).into();
    out.rest = totals(rest_trad, rest_roth, rest_hsa).into();
    out.year = totals(
        so_far_trad + rest_trad,
        so_far_roth + rest_roth,
        so_far_hsa + rest_hsa,
    )
    .into();
    Ok(out)
}
