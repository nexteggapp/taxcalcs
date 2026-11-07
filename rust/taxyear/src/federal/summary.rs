//! The totals and limits no form line holds, ported from Go's
//! `taxyear/federal/summary.go`.

use std::collections::{HashMap, HashSet};

use buffa::{DefaultInstance, MessageField};
use filing::Status;
use money::USCents;
use proto::factsv1::{self, HsaCoverage};
use proto::federalv1::Form5329;
use proto::returnsv1;
use proto::scenariosv1::{
    self, __buffa::oneof::item::Figures, __buffa::oneof::itemised_model::Kind as ItemisedKind,
    __buffa::oneof::term::Of, OtherIncomeFigures, OtherIncomeKind, ScenarioInputs, ScenarioSummary,
    YearProjection,
};

use super::Return;
use super::inputs::{SPOUSE_ID, YOU_ID, money_of, money2};
use crate::model::{self, Context, retirement};
use crate::payroll;

/// summary reads the totals and limits no form line holds from the
/// engine's outputs and inputs: the standard deduction, state and local tax
/// by where it was paid, the itemised floors, the capital loss carried,
/// each person's retirement limits and each other income model's part of
/// the return.
struct Summary<'a> {
    ctx: &'a Context,
    proj: &'a YearProjection,
    in_: &'a ScenarioInputs,
    r: &'a mut Return,
    out: ScenarioSummary,
}

pub(super) fn summarise(
    ctx: &Context,
    proj: &YearProjection,
    in_: &ScenarioInputs,
    r: &mut Return,
) -> ScenarioSummary {
    let mut s = Summary {
        ctx,
        proj,
        in_,
        r,
        out: ScenarioSummary::default(),
    };
    s.out.standard_deduction =
        s.r.out
            .federal
            .workpapers
            .standard_deduction
            .effective_standard_deduction
            .clone();
    let form = s.r.form();
    let l = model::value(&form.form_1040.capital_gain_or_loss)
        - model::value(&form.schedule_d.net_gain_or_loss);
    if l > USCents(0) {
        s.out.capital_loss_carried = money_of(-l);
    }
    s.salt();
    s.itemised();
    s.retirement();
    s.other_income();
    s.out
}

fn other_of(it: &scenariosv1::Item) -> &OtherIncomeFigures {
    match &it.figures {
        Some(Figures::OtherIncome(x)) => x,
        _ => OtherIncomeFigures::default_instance(),
    }
}

impl Summary<'_> {
    /// salt is Schedule A line 5a by where it was paid, from the inputs,
    /// and the § 164(b)(7) limit where the engine held the taxes to it.
    fn salt(&mut self) {
        let fed = &self.r.inputs.federal;
        let (mut withheld, mut estimates) = (USCents(0), USCents(0));
        for w in &fed.documents.w2s {
            for st in &w.form.state_tax_withholding {
                withheld += model::value(&st.tax_withheld);
            }
        }
        for p in &fed.payments.estimated_payments {
            if p.jurisdiction != "US" {
                estimates += model::value(&p.amount);
            }
        }
        let balance = &fed
            .deductions
            .state_and_local_taxes
            .balance_paid_for_prior_years;
        let mut out = scenariosv1::SaltSummary {
            withholding: money_of(withheld),
            estimates: money_of(estimates),
            last_year_balance: money2(balance),
            ..Default::default()
        };
        let sa = &self.r.form().schedule_a;
        let after = model::value(&sa.state_and_local_taxes_after_cap);
        if after < model::value(&sa.total_state_and_local_taxes) {
            out.cap = money_of(after);
        }
        self.out.salt = MessageField::some(out);
    }

    /// itemised records the medical and charity floors under the first
    /// model of each kind: what Schedule A doesn't count of each.
    fn itemised(&mut self) {
        let (mut medical_id, mut charity_id) = (String::new(), String::new());
        for m in &self.in_.model.itemised {
            match &m.kind {
                Some(ItemisedKind::Medical(_)) if medical_id.is_empty() => {
                    medical_id = m.id.clone();
                }
                Some(ItemisedKind::Charity(_)) if charity_id.is_empty() => {
                    charity_id = m.id.clone();
                }
                _ => {}
            }
        }
        let sa = self.r.form().schedule_a.clone();
        if !medical_id.is_empty() {
            let counted = model::value(&sa.deductible_medical_and_dental);
            self.out.itemised.push(scenariosv1::ItemisedSummary {
                id: medical_id,
                floor: money_of(model::value(&sa.medical_and_dental_expenses) - counted),
                counted: money_of(counted),
                ..Default::default()
            });
        }
        if !charity_id.is_empty() {
            let gifts = model::value(&sa.gifts_by_cash_or_check)
                + model::value(&sa.gifts_other_than_cash_or_check)
                + model::value(&sa.charitable_carryover_from_prior_year);
            let counted = model::value(&sa.total_gifts_to_charity);
            let floor = gifts - counted;
            self.out.itemised.push(scenariosv1::ItemisedSummary {
                id: charity_id.clone(),
                floor: money_of(floor),
                counted: money_of(counted),
                ..Default::default()
            });
            self.out.itemised_limitation = money_of(floor);
            if floor > USCents(0) {
                self.charity_working(
                    &format!("summary.itemised[{charity_id}].floor"),
                    floor,
                    gifts,
                );
            }
        }
        self.out.itemised_bracket_limit = money_of(USCents(0));
    }

    /// charityWorking explains what Schedule A line 14 doesn't count of the
    /// gifts, and adds line 14 up from the gifts less it.
    fn charity_working(&mut self, at: &str, floor: USCents, gifts: USCents) {
        let cite = "26 U.S.C. § 170(b)(1)(I), added by Pub. L. 119-21, sec. 70425; 26 U.S.C. § 170(b)(1)(G)";
        self.r.work.push(scenariosv1::Working {
            path: at.to_string(),
            rules: vec![scenariosv1::AppliedRule {
                cite: cite.to_string(),
                applied: MessageField::some(scenariosv1::Statement {
                    text: format!(
                        "Of {gifts} of gifts to charity, {floor} doesn't count: from 2026 gifts count only above 0.5% of adjusted gross income, and up to 60% of it"
                    ),
                    states: vec![
                        at.to_string(),
                        "federal.schedule_a.total_gifts_to_charity".to_string(),
                    ],
                    ..Default::default()
                }),
                ..Default::default()
            }],
            ..Default::default()
        });
        let line = "federal.schedule_a.total_gifts_to_charity";
        let term = |path: String, sign: i32| scenariosv1::Term {
            of: Some(Of::Path(path)),
            sign,
            ..Default::default()
        };
        let mut terms: Vec<scenariosv1::Term> = [
            "gifts_by_cash_or_check",
            "gifts_other_than_cash_or_check",
            "charitable_carryover_from_prior_year",
        ]
        .iter()
        .map(|p| term(format!("federal.schedule_a.{p}"), 1))
        .collect();
        terms.push(term(at.to_string(), -1));
        for x in &mut self.r.work {
            if x.path == line {
                x.terms = terms;
                return;
            }
        }
        self.r.work.push(scenariosv1::Working {
            path: line.to_string(),
            terms,
            ..Default::default()
        });
    }

    /// retirement records each person's IRA year and limits. A limit that
    /// needs an answer asks for it: the date of birth, for the plan limit's
    /// catch-up; the HSA coverage, for someone with HSA contributions.
    fn retirement(&mut self) {
        let mut ids: Vec<String> = Vec::new();
        let mut seen = HashSet::new();
        let hh = self.r.inputs.federal.household.clone();
        let mut add = |id: &str| {
            if !id.is_empty() && seen.insert(id.to_string()) {
                ids.push(id.to_string());
            }
        };
        add(&hh.taxpayer.id);
        if self.r.status == Status::MarriedFilingJointly {
            add(&hh.spouse.id);
        }
        for m in &self.in_.model.retirement {
            add(&m.person);
        }
        for id in &ids {
            let fed = &self.r.inputs.federal;
            let ira = &self.r.out.federal.workpapers.ira_deduction;
            let mut out = scenariosv1::RetirementSummary {
                person: id.clone(),
                ..Default::default()
            };
            for p in &fed.retirement.persons {
                if &p.person == id {
                    out.ira_used = money_of(
                        model::value(&p.traditional_ira_contributions)
                            + model::value(&p.roth_ira_contributions),
                    );
                }
            }
            let mut deducted = model::value(&ira.taxpayer_deduction);
            if *id != hh.taxpayer.id {
                deducted = model::value(&ira.spouse_deduction);
            }
            let f = &self.r.out.federal.form_8889s;
            for (i, h) in fed.health.hsa_accounts.iter().enumerate() {
                if &h.person == id && i < f.len() {
                    deducted += model::value(&f[i].hsa_deduction);
                }
            }
            out.deducted = money_of(deducted);
            if let Some(f) = self.form_5329(id) {
                out.roth_excess =
                    money_of(model::value(&f.current_year_roth_ira_excess_contributions));
                out.ira_excise = money_of(
                    model::value(&f.additional_tax_on_traditional_ira_excess)
                        + model::value(&f.additional_tax_on_roth_ira_excess),
                );
            }
            self.limits(id, &mut out);
            self.out.retirement.push(out);
        }
    }

    /// form5329 is a person's Form 5329, matched by the name the engine
    /// prints on it, or for a return with one person, the only one.
    fn form_5329(&self, id: &str) -> Option<&Form5329> {
        let forms = &self.r.form().form_5329;
        let hh = &self.r.inputs.federal.household;
        for p in [&hh.taxpayer, &hh.spouse] {
            if p.id != id || p.first_name.is_empty() {
                continue;
            }
            for f in forms {
                if f.individual_name.trim() == p.first_name.trim() {
                    return Some(f);
                }
            }
        }
        if forms.len() == 1
            && (hh.spouse.is_unset() || hh.spouse.id.is_empty())
            && id == hh.taxpayer.id
        {
            return forms.first();
        }
        None
    }

    /// limits fills a person's workplace plan and HSA limits, which are the
    /// projection's, and asks what they need.
    fn limits(&mut self, id: &str, out: &mut scenariosv1::RetirementSummary) {
        let at = format!("summary.retirement[{id}]");
        let (age, aged) = self.ctx.age_at_year_end(id);
        if let Ok((base, catch_up)) = payroll::plan_limit(self.ctx.year(), age) {
            out.plan_limit = money_of(base + catch_up);
            if !aged {
                let q = model::new_question(
                    format!("retirement/{id}/date-of-birth"),
                    format!("{at}.plan_limit"),
                    format!(
                        "What is {} date of birth? The workplace plan limit is higher from 50.",
                        self.possessive(id)
                    ),
                    "No catch-up is counted until you say.",
                    format!("facts.people[{id}].date_of_birth"),
                    &[],
                );
                self.r.notes.ask(q);
            }
        }
        let (mut employer, mut contributed) = (USCents(0), USCents(0));
        for e in &self.in_.model.employers {
            if e.person == id {
                employer += model::value(&e.employer_hsa);
            }
        }
        let fed = &self.r.inputs.federal;
        for w in &fed.documents.w2s {
            if w.person == id {
                for c in &w.form.coded_amounts {
                    if c.code == "W" {
                        contributed += model::value(&c.amount);
                    }
                }
            }
        }
        for h in &fed.health.hsa_accounts {
            if h.person == id {
                contributed += model::value(&h.direct_contributions);
            }
        }
        out.hsa_employer = money_of(employer);
        let (limit, ok) = retirement::hsa_limit(self.ctx, id, &mut self.r.notes);
        let coverage = self
            .ctx
            .person(id)
            .map(|p| p.hsa_coverage.to_i32())
            .unwrap_or(0);
        if ok {
            out.hsa_limit = money_of(limit);
        } else if employer + contributed > USCents(0)
            && coverage == HsaCoverage::HSA_COVERAGE_UNSPECIFIED as i32
        {
            let q = model::new_question(
                format!("retirement/{id}/hsa-coverage"),
                format!("{at}.hsa_limit"),
                format!(
                    "What health plan coverage did {} HSA go with?",
                    self.possessive(id)
                ),
                "The HSA limit isn't counted until you say.",
                format!("facts.people[{id}].hsa_coverage"),
                &["Self-only", "Family", "No HSA-eligible plan"],
            );
            self.r.notes.ask(q);
        }
    }

    /// possessive is "your" for you, the person's name with "'s", or for a
    /// spouse without one "your spouse's".
    fn possessive(&self, id: &str) -> String {
        let p = self
            .ctx
            .person(id)
            .unwrap_or_else(|| factsv1::Person::default_instance());
        if p.relationship == factsv1::Relationship::RELATIONSHIP_YOU
            || p.name.is_empty() && id == YOU_ID
        {
            "your".to_string()
        } else if !p.name.is_empty() {
            format!("{}'s", p.name)
        } else if p.relationship == factsv1::Relationship::RELATIONSHIP_SPOUSE || id == SPOUSE_ID {
            "your spouse's".to_string()
        } else {
            format!("{id}'s")
        }
    }

    /// otherIncome records each other income model's part of the return. A
    /// line the engine works out over several models, such as the taxable
    /// part of Social Security benefits or a state refund, is shared by
    /// each model's part of what went into it.
    fn other_income(&mut self) {
        let form = self.r.form();
        let (f, sch1) = (&form.form_1040, &form.schedule_1);
        let fed = &self.r.inputs.federal;
        let docs = &fed.documents;
        let (mut ss_all, mut refund_all, mut early_all) = (USCents(0), USCents(0), USCents(0));
        for d in &docs.social_security {
            ss_all += model::value(&d.form.net_benefits);
        }
        for d in &docs.government_payments {
            refund_all += model::value(&d.form.state_or_local_refund);
        }
        for d in &docs.retirement_distributions {
            early_all += early(d);
        }
        let mut early_tax = USCents(0);
        for x in &form.form_5329 {
            early_tax += model::value(&x.additional_tax_on_early_distributions);
        }
        let mut by: HashMap<&str, &scenariosv1::OtherIncomeModel> = HashMap::new();
        for m in &self.in_.model.other_income {
            by.insert(&m.id, m);
        }
        let mut list = Vec::new();
        for o in &self.proj.other_income {
            let id = format!("other_income/{}", o.id);
            let mut out = scenariosv1::OtherIncomeSummary {
                id: o.id.clone(),
                ..Default::default()
            };
            let (mut withheld, mut taxable, mut early_part) = (USCents(0), USCents(0), USCents(0));
            let mut known = true;
            for it in &o.items {
                withheld += model::value(&other_of(it).federal_withheld);
            }
            let kind = by.get(o.id.as_str()).and_then(|m| m.kind.as_known());
            match kind {
                Some(OtherIncomeKind::OTHER_INCOME_KIND_RETIREMENT_WITHDRAWALS) => {
                    let prefix = format!("{id}/");
                    for d in &docs.retirement_distributions {
                        if d.id.starts_with(&prefix) {
                            taxable += model::value(&d.form.taxable_amount);
                            early_part += early(d);
                            // A distribution without a code is left out.
                            known = known && !d.form.distribution_codes.is_empty();
                        }
                    }
                }
                Some(OtherIncomeKind::OTHER_INCOME_KIND_SOCIAL_SECURITY) => {
                    for d in &docs.social_security {
                        if d.id == id {
                            let net = model::value(&d.form.net_benefits);
                            out.net_benefits = money_of(net);
                            taxable = model::scale(
                                model::value(&f.social_security_benefits_taxable),
                                net,
                                ss_all,
                            );
                        }
                    }
                }
                Some(OtherIncomeKind::OTHER_INCOME_KIND_STATE_REFUND) => {
                    for d in &docs.government_payments {
                        if d.id == id {
                            taxable = model::scale(
                                model::value(&sch1.taxable_state_local_refunds),
                                model::value(&d.form.state_or_local_refund),
                                refund_all,
                            );
                        }
                    }
                }
                Some(OtherIncomeKind::OTHER_INCOME_KIND_RENTAL) => {
                    for p in &fed.rentals {
                        if p.id == id {
                            taxable =
                                model::value(&p.rents_received) - model::value(&p.other_expenses);
                        }
                    }
                    if taxable < USCents(0) {
                        // The loss allowed is Schedule E's, shared by the
                        // losses.
                        known = fed.rentals.len() == 1;
                        let allowed = model::value(&sch1.rental_royalty_partnership_s_corp_trust);
                        if known {
                            out.loss_carried =
                                money_of(USCents(0).max(-taxable + USCents(0).min(allowed)));
                            taxable = USCents(0).min(allowed);
                        }
                    }
                }
                Some(
                    OtherIncomeKind::OTHER_INCOME_KIND_ALIMONY
                    | OtherIncomeKind::OTHER_INCOME_KIND_HOME_SALE,
                ) => known = false,
                _ => {
                    for it in &o.items {
                        let x = other_of(it);
                        if x.taxable.is_set() {
                            taxable += model::value(&x.taxable);
                        } else {
                            taxable += model::value(&x.gross);
                        }
                    }
                }
            }
            out.withheld = money_of(withheld);
            if known {
                out.taxable = money_of(taxable);
            }
            if early_part > USCents(0) {
                out.early_distribution_tax =
                    money_of(model::scale(early_tax, early_part, early_all));
            }
            list.push(out);
        }
        self.out.other_income.extend(list);
    }
}

/// early is a 1099-R's taxable amount coded 1, an early distribution with
/// no known exception, which Form 5329 line 1 counts.
fn early(d: &returnsv1::Form1099RDocument) -> USCents {
    if d.form.distribution_codes.iter().any(|c| c == "1") {
        return model::value(&d.form.taxable_amount);
    }
    USCents(0)
}
