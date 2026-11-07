//! The scenario's summary: summary.go.

use std::collections::HashMap;

use money::USCents;
use proto::commonv1;
use proto::factsv1::__buffa::oneof::{document::Form, payment_purpose::Purpose};
use proto::federalv1::Form1040;
use proto::scenariosv1::{
    __buffa::oneof::item::Figures, Estimated, EstimatedSummary, ItemStatus, PlannedPayment,
    ScenarioInputs, ScenarioResult, ScenarioSummary, TaxPaymentFigures,
};

use crate::federal;
use crate::internal::civil;
use crate::model::{self, Context};

/// dateKey is a date as a path key, YYYY-MM-DD.
fn date_key(d: Option<&commonv1::Date>) -> String {
    match civil::from_proto(d) {
        Ok(c) => c.to_string(),
        Err(_) => String::new(),
    }
}

/// summarise fills the scenario's summary: the return's parts, the wages
/// across employers, and each jurisdiction's tax paid against its tax.
pub(crate) fn summarise(
    ctx: &Context,
    in_: &ScenarioInputs,
    ret: Option<&federal::Return>,
    res: &mut ScenarioResult,
) {
    let mut summary = ScenarioSummary::default();
    if let Some(ret) = ret {
        summary = ret.summary().clone();
    }
    let mut gross = USCents(0);
    let mut pretax = USCents(0);
    for e in &res.projection.employers {
        let y = &e.year.items;
        gross += model::value(&y.gross);
        for p in &y.pretax {
            pretax += model::value(&p.amount);
        }
    }
    summary.wages_gross = model::money(gross).into();
    summary.wages_pretax = model::money(pretax).into();
    res.summary = summary.into();
    let sums: Vec<EstimatedSummary> = res
        .estimated
        .iter()
        .map(|e| estimated_summary(ctx, in_, res, e))
        .collect();
    for (e, s) in res.estimated.iter_mut().zip(sums) {
        e.summary = s.into();
    }
}

/// estimatedSummary adds up one jurisdiction's tax paid against its tax.
fn estimated_summary(
    ctx: &Context,
    in_: &ScenarioInputs,
    res: &ScenarioResult,
    e: &Estimated,
) -> EstimatedSummary {
    let j = e.jurisdiction.as_str();
    let f = res.federal.form_1040.as_option();
    let mut out = EstimatedSummary::default();
    let mut withheld = USCents(0);
    if j == "US" {
        withheld = model::value(&f.map(|f| &f.total_withheld).and_then(|m| m.as_option()));
    } else {
        for emp in &res.projection.employers {
            for st in &emp.year.boxes.states {
                if st.state == j {
                    withheld += model::value(&st.withheld);
                }
            }
        }
    }
    if j == "US" {
        // Social Security withheld over the limit is credited as
        // withholding.
        let over = model::value(&res.federal.schedule_3.excess_social_security_withheld);
        out.social_security_over = model::money(over).into();
        withheld += over;
    }
    out.withholding = model::money(withheld).into();
    let (paid, expected, balance) = payments(ctx, in_, res, j, &mut out);
    // Payments to come are planned, not paid: the return credits the
    // payments made.
    out.estimated = model::money(paid).into();
    out.paid = model::money(paid).into();
    out.expected = model::money(expected).into();
    let credited = withheld + paid;
    out.credited = model::money(credited).into();
    // Set when nothing is applied too, as the amount with the return is
    // the tax less what is credited, plus it.
    let applied = applied_to_next(ctx, j, f);
    out.applied_to_next_year = model::money(applied).into();
    let (mut tax, mut known) = model::cents(&e.tax_for_year);
    if j == "US"
        && let Some(f) = f
    {
        tax = model::value(&f.total_tax);
        known = true;
        let mut early = USCents(0);
        for x in &res.federal.form_5329 {
            early += model::value(&x.additional_tax_on_early_distributions);
        }
        out.early_distribution_tax = model::money(early).into();
        out.excess_contribution_tax =
            model::money(federal::excess_contribution_tax(&res.federal)).into();
        out.payments_tax = e.tax_for_year.clone();
    }
    if j == "US" && known {
        // Form 1040 line 38's penalty goes with the return, line 37.
        tax += model::value(
            &f.map(|f| &f.estimated_tax_penalty)
                .and_then(|m| m.as_option()),
        );
    }
    if known {
        out.with_return = model::money(tax - credited + applied).into();
        if let Some(b) = balance {
            out.left_with_return = model::money(tax - credited + applied - b).into();
        }
    }
    out
}

fn payment_of(f: &Option<Figures>) -> &TaxPaymentFigures {
    match f {
        Some(Figures::Payment(p)) => p,
        _ => <TaxPaymentFigures as buffa::DefaultInstance>::default_instance(),
    }
}

/// payments adds up a jurisdiction's payments that count for the year,
/// made and still to come, lists those planned ahead, and returns the
/// balance payments made toward the year's return, or None.
fn payments(
    ctx: &Context,
    in_: &ScenarioInputs,
    res: &ScenarioResult,
    j: &str,
    out: &mut EstimatedSummary,
) -> (USCents, USCents, Option<USCents>) {
    let mut defaults: HashMap<&str, &str> = HashMap::new();
    for m in &in_.model.tax_paid {
        defaults.insert(&m.id, &m.default_part.id);
    }
    let (mut paid, mut expected) = (USCents(0), USCents(0));
    let mut balance: Option<USCents> = None;
    for t in &res.projection.tax_paid {
        if t.jurisdiction != j {
            continue;
        }
        for it in &t.items {
            let p = payment_of(&it.figures);
            let balance_for = match p.purpose.purpose {
                Some(Purpose::BalanceFor(y)) => y,
                _ => 0,
            };
            let made = it.status.as_known() == Some(ItemStatus::ITEM_STATUS_MADE);
            if i64::from(balance_for) == ctx.year() && made {
                let b = model::value(&p.amount) + balance.unwrap_or(USCents(0));
                balance = Some(b);
            }
            if !p.counts {
                continue;
            }
            let v = model::value(&p.amount);
            if made {
                paid += v;
                continue;
            }
            expected += v;
            out.planned.push(PlannedPayment {
                id: date_key(it.date.as_option()),
                due: it.date.clone(),
                amount: p.amount.clone(),
                set: it.origin.part.as_str() != defaults.get(t.id.as_str()).copied().unwrap_or(""),
                ..Default::default()
            });
        }
    }
    (paid, expected, balance)
}

/// appliedToNext is the overpayment the year's filed return applied to
/// next year, once the year is over.
fn applied_to_next(ctx: &Context, j: &str, f: Option<&Form1040>) -> USCents {
    if !ctx.finished() {
        return USCents(0);
    }
    if j == "US" {
        return model::value(
            &f.map(|f| &f.applied_to_next_year)
                .and_then(|m| m.as_option()),
        );
    }
    let year = ctx.year();
    for d in &ctx.facts().unassigned {
        if !model::usable(&d.source) {
            continue;
        }
        match &d.form {
            Some(Form::CaliforniaReturn(c)) if j == "CA" => {
                if let Some(r) = c.form_540.as_option()
                    && i64::from(r.tax_year) == year
                {
                    return model::value(&r.applied_to_next_year_estimated_tax);
                }
            }
            Some(Form::OregonReturn(o)) if j == "OR" => {
                if let Some(r) = o.form_or40.as_option()
                    && i64::from(r.tax_year) == year
                {
                    return model::value(&r.applied_to_next_year);
                }
            }
            _ => {}
        }
    }
    USCents(0)
}
