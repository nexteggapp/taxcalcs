//! A job's projection: overrides of its figures so far, the withholding
//! pass, the totals and W-2 boxes, and the parts that can be added. Ported
//! from projection.go.

use std::collections::{HashMap, HashSet};

use buffa::MessageField;

/// A Money field as the generated messages hold it.
type MoneyField = MessageField<Money, buffa::Inline<Money>>;
use money::USCents;
use proto::commonv1::Money;
use proto::factsv1::{self, PaycheckKind};
use proto::federalv1::FormW2;
use proto::scenariosv1::{
    self, __buffa::oneof::employer_part::Part, __buffa::oneof::item::Figures, EmployerPart,
    EmployerProjection, ExpectedDocument, Item, ItemStatus, Origin, Override, PayTotals, W2Boxes,
    employer_part, origin::Basis, w2boxes,
};

use super::pay::{HSA, K401, K403B, Line, Pay, set_line};
use super::withhold::state_base;
use super::{Draft, Job};
use crate::model::{self, Error};
use crate::payroll;

/// The start of the path of an override of an employer's figures so far:
/// "projection.employers[acme].so_far.items.".
pub fn override_prefix(id: &str) -> String {
    format!("projection.employers[{id}].so_far.items.")
}

fn d(c: USCents) -> USCents {
    model::dollars(c)
}

fn m(c: USCents) -> MoneyField {
    model::money(c).into()
}

impl<'a> Job<'a> {
    /// The day the figures so far run to: the latest evidence date, else
    /// as_of.
    fn so_far_through(&self) -> crate::internal::civil::Date {
        if !self.latest.is_zero() {
            return self.latest;
        }
        self.ctx.as_of()
    }

    /// Adds, for each override of a figure so far, an ADJUSTMENT dated
    /// so_far_through that keeps the items adding up to the typed figure.
    pub(super) fn apply_overrides(&mut self, overrides: &[Override]) -> Result<(), Error> {
        let prefix = override_prefix(&self.m.id);
        for o in overrides {
            let Some(field) = o.path.strip_prefix(&prefix) else {
                continue;
            };
            let have = self.sum_of(&self.made());
            let mut adj = Pay::default();
            set_field(&mut adj, &have, field, model::value(&o.amount))
                .map_err(|e| Error::Expansion(format!("override {}: {e}", o.path)))?;
            self.state_wages_follow(&mut adj, &have, field);
            let mut auto = Pay::default();
            let _ = set_field(&mut auto, &have, field, USCents(0));
            let automatic = -auto.line_sum();
            let mut w = scenariosv1::Working {
                path: o.path.clone(),
                automatic: m(d(automatic)),
                sources: self.source_ids(),
                ..Default::default()
            };
            if o.was.is_set() {
                w.sources_changed = model::value(&o.was) != automatic;
            }
            self.workings.push(w);
            let adj = adj.nonzero();
            if adj.empty() {
                continue;
            }
            self.items.push(Draft {
                id: model::item_id(&[&self.item_prefix(), "override", field]),
                date: self.so_far_through(),
                status: ItemStatus::ITEM_STATUS_MADE,
                kind: scenariosv1::ItemKind::ITEM_KIND_ADJUSTMENT,
                origin: Origin {
                    basis: Basis::BASIS_OVERRIDE.into(),
                    r#override: o.path.clone(),
                    ..Default::default()
                },
                p: adj,
                ..Draft::default()
            });
        }
        Ok(())
    }

    /// Moves the state wages so far with an override of gross pay or a
    /// pre-tax line, which moves box 1: in the state the figures so far
    /// were earned in, the latest when there are several. California's
    /// wages keep the HSA, which it taxes. A state's own wages line, typed,
    /// stands.
    fn state_wages_follow(&self, adj: &mut Pay, have: &Pay, field: &str) {
        if field.starts_with("states[") || have.states.is_empty() {
            return;
        }
        let mut st = have.states[have.states.len() - 1].state.clone();
        for w in self.work_states(self.so_far_through()) {
            if have.state(&w.state).is_some() {
                st = w.state;
            }
        }
        let mut diff = adj.gross.v;
        for (k, l) in &adj.pretax {
            if *k == HSA && st == "CA" {
                continue;
            }
            diff -= l.v;
        }
        if diff.0 != 0 {
            let i = adj.state_index(&st);
            adj.states[i].wages = set_line(diff);
        }
    }

    /// The ids of the parts and evidence the figures so far rest on.
    fn source_ids(&self) -> Vec<String> {
        let mut seen = HashSet::new();
        let mut out = Vec::new();
        for i in self.made() {
            let o = &self.items[i].origin;
            for id in std::iter::once(&o.part).chain(o.evidence.iter()) {
                if !id.is_empty() && seen.insert(id.clone()) {
                    out.push(id.clone());
                }
            }
        }
        out
    }

    /// Each override's working: the figure computed today, and whether it
    /// differs from the one the person saw when they typed it.
    pub fn workings(&self) -> &[scenariosv1::Working] {
        &self.workings
    }

    /// Computes the withholding left unset, in date order, and returns the
    /// job's projection. Call it once, after [`super::max_out`] and
    /// [`super::hold_hsa`].
    pub fn projection(&mut self) -> Result<EmployerProjection, Error> {
        if self.done {
            return Err(Error::Expansion(format!(
                "{}: Projection called twice",
                self.path()
            )));
        }
        self.done = true;
        let mut auto = Pay::default();
        if self.hsa_typed.0 != 0 || !self.plan_typed.is_empty() {
            auto = self.split_typed()?;
        }
        self.withhold()?;
        self.round_items();
        self.typed_working(&auto);
        let mut out = EmployerProjection {
            id: self.m.id.clone(),
            so_far_through: self.latest.field(),
            ..Default::default()
        };
        let (mut made, mut expected) = (Vec::new(), Vec::new());
        for (i, it) in self.items.iter().enumerate() {
            out.items.push(it.proto());
            if it.status == ItemStatus::ITEM_STATUS_MADE {
                made.push(i);
            } else {
                expected.push(i);
            }
        }
        model::sort_items(&mut out.items);
        let base =
            payroll::wage_base(self.ctx.year()).map_err(|e| Error::Expansion(e.to_string()))?;
        let (hsa_so_far, hsa_rest) = self.employer_hsa();
        let (ca_so_far, ca_rest) = california_hsa(
            &self.sum_of(&made),
            &self.sum_of(&expected),
            hsa_so_far,
            hsa_rest,
        );
        let all: Vec<usize> = (0..self.items.len()).collect();
        let mut so_far = self.totals(&made, base, hsa_so_far, ca_so_far);
        let mut rest = self.totals(&expected, base, hsa_rest, ca_rest);
        let mut year = self.totals(&all, base, USCents(0), USCents(0));
        let mut boxes = combine_boxes(&so_far.boxes, &rest.boxes, 1);
        boxes.social_security_wages = m(model::value(&boxes.medicare_wages).min(d(base)));
        year.boxes = boxes.into();
        if !self.w2s.is_empty() {
            // The W-2 states the boxes; an override of a figure so far moves
            // them as it moves the items.
            let mut boxes = w2_boxes(&self.w2s);
            let typed: Vec<usize> = (0..self.items.len())
                .filter(|&i| !self.items[i].origin.r#override.is_empty())
                .collect();
            if !typed.is_empty() {
                boxes = combine_boxes(
                    &boxes,
                    &self.totals(&typed, base, USCents(0), USCents(0)).boxes,
                    1,
                );
            }
            so_far.boxes = boxes.clone().into();
            year.boxes = boxes.into();
        }
        rest.boxes = combine_boxes(&year.boxes, &so_far.boxes, -1).into();
        out.so_far = so_far.into();
        out.rest = rest.into();
        out.year = year.into();
        if self.w2s.is_empty() && !self.items.is_empty() {
            out.expected = vec![ExpectedDocument {
                form: "FormW2".to_string(),
                due: model::forms_due(self.ctx.year()).field(),
                ..Default::default()
            }];
        }
        out.can_add = self.can_add();
        if (self.hsa_in_wages + self.hsa_in_income).0 != 0 {
            out.hsa_over_in_wages = m(d(self.hsa_in_wages));
            out.hsa_over_in_income = m(d(self.hsa_in_income));
            out.hsa_over = m(d(self.hsa_in_wages) + d(self.hsa_in_income));
        }
        Ok(out)
    }

    /// Splits the employer's HSA contribution for the year between so far
    /// and the rest by pay date: each pay date carries an equal share.
    fn employer_hsa(&self) -> (USCents, USCents) {
        let total = model::value(&self.m.employer_hsa);
        if total.0 == 0 || self.slots.is_empty() {
            return (USCents(0), total);
        }
        let through = self.so_far_through();
        let made = self.slots.iter().filter(|d| !d.after(through)).count() as i64;
        let so_far = model::scale(total, USCents(made), USCents(self.slots.len() as i64));
        (so_far, total - so_far)
    }

    /// The items' totals, and box totals worked out from them: box 1 is
    /// gross less pre-tax lines; box 3 gross less HSA and other benefits,
    /// capped at the wage base for this employer; box 5 the same, uncapped;
    /// box 16 the items' state wages, or box 1 split by days for a job
    /// worked across states. A job's California wages from its paychecks'
    /// lines add the employer's own HSA; split by days they are box 1's
    /// share. hsa is the employer's own HSA for the period, which box 12
    /// code W adds. Rounding happens once: so far's and the rest's box
    /// figures are each rounded to the dollar, half away from zero, and
    /// projection makes the year their sum.
    fn totals(&self, ds: &[usize], base: USCents, hsa: USCents, ca_hsa: USCents) -> PayTotals {
        let p = self.sum_of(ds);
        let mut take = Line::default();
        for &i in ds {
            take = take.plus(self.items[i].take_home);
        }
        let mut b = W2Boxes {
            wages: m(d(p.taxable())),
            federal_withheld: m(d(p.federal.v)),
            social_security_wages: m(d(p.fica()).min(d(base))),
            social_security_withheld: m(d(p.ss.v)),
            medicare_wages: m(d(p.fica())),
            medicare_withheld: m(d(p.medicare.v)),
            k401: m(d(p.pretax_line(K401).v)),
            k403b: m(d(p.pretax_line(K403B).v)),
            roth_401k: m(d(p.roth401k.v)),
            roth_403b: m(d(p.roth403b.v)),
            hsa: m(d(p.pretax_line(HSA).v + hsa)),
            ..Default::default()
        };
        let ca = |mut st: w2boxes::State, mut wages: USCents| -> w2boxes::State {
            if st.state == "CA" {
                st.employer_hsa = m(d(ca_hsa));
                wages += d(ca_hsa);
            }
            st.wages = m(wages);
            st
        };
        let state = |state: &str, withheld: USCents| w2boxes::State {
            state: state.to_string(),
            withheld: m(withheld),
            ..Default::default()
        };
        if matches!(
            self.m.work.place,
            Some(scenariosv1::__buffa::oneof::work_place::Place::Split(_))
        ) {
            let mut withheld: HashMap<&str, USCents> = HashMap::new();
            for s in &p.states {
                withheld.insert(&s.state, s.withheld.v);
            }
            // California's line has the employer's HSA, which it taxes,
            // beside its wages, as without a split.
            let mut seen = HashSet::new();
            for w in self.work_states(self.ctx.year_end()) {
                seen.insert(w.state.clone());
                // A state's base is box 1, and for California the payroll
                // HSA, each as printed, so a state with all the days adds
                // them up.
                let mut sbase = model::value(&b.wages);
                if w.state == "CA" {
                    sbase += shown_hsa(&p.proto());
                }
                let wh = withheld.get(w.state.as_str()).copied().unwrap_or_default();
                let st = ca(
                    state(&w.state, d(wh)),
                    d(model::scale(sbase, w.days, w.total)),
                );
                b.states.push(st);
            }
            // Tax a state withheld was paid there, whatever the split, so
            // its line keeps it, with the wages the items give it.
            for s in &p.states {
                if !seen.contains(&s.state) && (s.withheld.v.0 != 0 || s.wages.v.0 != 0) {
                    let mut wages = d(s.wages.v);
                    if s.state == "CA" && p.states.len() == 1 {
                        // Earned in California alone, as without a split.
                        wages =
                            model::value(&b.wages) + shown_hsa(&p.proto()) + d(self.typed_ca(ds));
                    }
                    let st = ca(state(&s.state, d(s.withheld.v)), wages);
                    b.states.push(st);
                }
            }
        } else {
            let mut added = false;
            let items = p.proto();
            for s in &items.states {
                let mut st = state(&s.state, d(model::value(&s.tax_withheld)));
                let mut wages = d(model::value(&s.state_income));
                if s.state == "CA" {
                    added = true;
                    if p.states.len() == 1 {
                        // Earned in California alone, the wages are box 1 and
                        // the payroll HSA, which California doesn't exclude
                        // (Schedule CA (540) line 1h adds code W back), each
                        // rounded, so they add up as displayed. The
                        // paystubs' and parts' own California lines aren't
                        // relied on; an override stands, by what it adds.
                        wages = model::value(&b.wages) + shown_hsa(&items) + d(self.typed_ca(ds));
                    }
                    // The employer's HSA is rounded once and added, so the
                    // wages are the pay and it; it is set when there is
                    // none, as the pay is the wages less it.
                    st.employer_hsa = m(d(ca_hsa));
                    wages += d(ca_hsa);
                }
                st.wages = m(wages);
                b.states.push(st);
            }
            if !added && ca_hsa.0 != 0 {
                b.states.push(w2boxes::State {
                    state: "CA".to_string(),
                    wages: m(d(ca_hsa)),
                    withheld: m(USCents(0)),
                    employer_hsa: m(d(ca_hsa)),
                    ..Default::default()
                });
            }
        }
        PayTotals {
            items: p.proto().into(),
            take_home: take.money().into(),
            boxes: b.into(),
            ..Default::default()
        }
    }

    /// What an override of California's wages so far adds beyond box 1 and
    /// the payroll HSA. A part has no field for California wages, so a
    /// part's California line, as a year total's, follows box 1 like the
    /// rest; an override of gross pay or a pre-tax line moves the line with
    /// box 1, and adds nothing.
    fn typed_ca(&self, ds: &[usize]) -> USCents {
        let mut out = USCents(0);
        for &i in ds {
            let dr = &self.items[i];
            if !dr.origin.r#override.ends_with(".states[CA].state_income") {
                continue;
            }
            if let Some(l) = dr.p.state("CA") {
                out += l.wages.v - state_base("CA", &dr.p);
            }
        }
        out
    }

    /// Lists the part kinds that can be added now, each filled with its
    /// defaults. Ids are left to the caller.
    fn can_add(&self) -> Vec<EmployerPart> {
        if !self.w2s.is_empty() {
            return Vec::new();
        }
        let mut out = Vec::new();
        let mut rate = employer_part::PayRate::default();
        if !self.latest.is_zero() {
            rate.from = self.latest.add_days(1).field();
        }
        if let Some(r) = self.regular_stub() {
            rate.paycheck = r.this.proto().into();
        }
        out.push(EmployerPart {
            part: Some(Part::PayRate(Box::new(rate))),
            ..Default::default()
        });
        out.push(EmployerPart {
            part: Some(Part::OneOff(Box::new(employer_part::OneOff {
                date: self.ctx.year_end().field(),
                kind: PaycheckKind::PAYCHECK_KIND_BONUS.into(),
                amount: m(USCents(0)),
                ..Default::default()
            }))),
            ..Default::default()
        });
        let (mut has_end, mut has_total) = (false, false);
        for p in &self.m.parts {
            has_end = has_end || matches!(p.part, Some(Part::Ends(_)) | Some(Part::Ended(_)));
            has_total = has_total || matches!(p.part, Some(Part::YearTotal(_)));
        }
        if !has_end && let Some(last) = self.slots.last() {
            out.push(EmployerPart {
                part: Some(Part::Ends(Box::new(employer_part::Ends {
                    last_paycheck: last.field(),
                    ..Default::default()
                }))),
                ..Default::default()
            });
        }
        if !has_total {
            let mut total = employer_part::YearTotal {
                year: self.sum_all().proto().into(),
                ..Default::default()
            };
            if let Some(r) = self.regular_stub() {
                total.typed_against = r.date.field();
            }
            out.push(EmployerPart {
                part: Some(Part::YearTotal(Box::new(total))),
                ..Default::default()
            });
        }
        out
    }
}

impl Draft {
    fn proto(&self) -> Item {
        Item {
            id: self.id.clone(),
            date: self.date.field(),
            status: self.status.into(),
            kind: self.kind.into(),
            origin: self.origin.clone().into(),
            estimated: self.estimated.clone(),
            figures: Some(Figures::Paycheck(Box::new(self.p.proto()))),
            take_home: self.take_home.money().into(),
            ..Default::default()
        }
    }
}

/// Sets, on adj, the line a so-far field path names to want less what the
/// items have.
fn set_field(adj: &mut Pay, have: &Pay, field: &str, want: USCents) -> Result<(), String> {
    match field {
        "gross" => adj.gross = set_line(want - have.gross.v),
        "federal_withheld" => adj.federal = set_line(want - have.federal.v),
        "social_security_withheld" => adj.ss = set_line(want - have.ss.v),
        "medicare_withheld" => adj.medicare = set_line(want - have.medicare.v),
        "roth_401k" => adj.roth401k = set_line(want - have.roth401k.v),
        "roth_403b" => adj.roth403b = set_line(want - have.roth403b.v),
        "net" => adj.net = set_line(want - have.net.v),
        _ => return set_list_field(adj, have, field, want),
    }
    Ok(())
}

/// Handles pretax[i].amount and states[XX].state_income or .tax_withheld. A
/// pre-tax line is named by its position in the figures so far.
fn set_list_field(adj: &mut Pay, have: &Pay, field: &str, want: USCents) -> Result<(), String> {
    let not_a_line = || format!("{} is not a line of pay figures", working::quote(field));
    let (Some(open), Some(close)) = (field.find('['), field.find(']')) else {
        return Err(not_a_line());
    };
    if close < open {
        return Err(not_a_line());
    }
    let (list, key, rest) = (&field[..open], &field[open + 1..close], &field[close + 1..]);
    if list == "pretax" && rest == ".amount" {
        let kinds = have.proto().pretax;
        let i = match key.parse::<i64>() {
            Ok(i) if i >= 0 && (i as usize) < kinds.len() => i as usize,
            _ => {
                return Err(format!(
                    "{} names no pre-tax line so far",
                    working::quote(field)
                ));
            }
        };
        let k = kinds[i].kind.to_i32();
        adj.set_pretax(k, set_line(want - have.pretax_line(k).v));
    } else if list == "states" && (rest == ".state_income" || rest == ".tax_withheld") {
        let h = have.state(key).unwrap_or_default();
        let i = adj.state_index(key);
        if rest == ".state_income" {
            adj.states[i].wages = set_line(want - h.wages.v);
        } else {
            adj.states[i].withheld = set_line(want - h.withheld.v);
        }
    } else {
        return Err(not_a_line());
    }
    Ok(())
}

/// The payroll HSA line of a period's item totals.
fn shown_hsa(f: &factsv1::PayFigures) -> USCents {
    f.pretax
        .iter()
        .filter(|x| x.kind.to_i32() == HSA)
        .map(|x| model::value(&x.amount))
        .sum()
}

/// California's share of the employer's own HSA so far and for the rest of
/// the year: California taxes it, so its wages include it, where other
/// states follow the federal exclusion. Each period's share is in
/// proportion to that period's California wages among its state wages.
/// With no state wages to come, the rest goes where so far's went when that
/// was California alone.
fn california_hsa(
    so_far: &Pay,
    rest: &Pay,
    hsa_so_far: USCents,
    hsa_rest: USCents,
) -> (USCents, USCents) {
    let share = |p: &Pay, hsa: USCents| -> Option<USCents> {
        let (mut ca, mut all) = (USCents(0), USCents(0));
        for s in &p.states {
            all += s.wages.v;
            if s.state == "CA" {
                ca += s.wages.v;
            }
        }
        if all.0 == 0 {
            return None;
        }
        Some(model::scale(hsa, ca, all))
    };
    let so = share(so_far, hsa_so_far);
    let ca_so_far = so.unwrap_or_default();
    let mut ca_rest = share(rest, hsa_rest);
    if ca_rest.is_none() && so.is_some() && ca_so_far == hsa_so_far {
        ca_rest = Some(hsa_rest);
    }
    (ca_so_far, ca_rest.unwrap_or_default())
}

fn add(dst: &mut MoneyField, v: &MoneyField) {
    *dst = m(model::value(&*dst) + model::value(v));
}

/// Adds the W-2s' boxes.
fn w2_boxes(ws: &[&FormW2]) -> W2Boxes {
    let mut b = W2Boxes::default();
    let mut states: Vec<w2boxes::State> = Vec::new();
    for w in ws {
        add(&mut b.wages, &w.wages_tips_other_compensation);
        add(&mut b.federal_withheld, &w.federal_tax_withheld);
        add(&mut b.social_security_wages, &w.social_security_wages);
        add(
            &mut b.social_security_withheld,
            &w.social_security_tax_withheld,
        );
        add(&mut b.medicare_wages, &w.medicare_wages_and_tips);
        add(&mut b.medicare_withheld, &w.medicare_tax_withheld);
        for c in &w.coded_amounts {
            match c.code.to_uppercase().as_str() {
                "D" => add(&mut b.k401, &c.amount),
                "E" => add(&mut b.k403b, &c.amount),
                "AA" => add(&mut b.roth_401k, &c.amount),
                "BB" => add(&mut b.roth_403b, &c.amount),
                "W" => add(&mut b.hsa, &c.amount),
                _ => {}
            }
        }
        for s in &w.state_tax_withholding {
            let i = match states.iter().position(|x| x.state == s.state) {
                Some(i) => i,
                None => {
                    states.push(w2boxes::State {
                        state: s.state.clone(),
                        ..Default::default()
                    });
                    states.len() - 1
                }
            };
            add(&mut states[i].wages, &s.state_income);
            add(&mut states[i].withheld, &s.tax_withheld);
        }
    }
    b.states = states;
    for f in [
        &mut b.wages,
        &mut b.federal_withheld,
        &mut b.social_security_wages,
        &mut b.social_security_withheld,
        &mut b.medicare_wages,
        &mut b.medicare_withheld,
        &mut b.k401,
        &mut b.k403b,
        &mut b.roth_401k,
        &mut b.roth_403b,
        &mut b.hsa,
    ] {
        // A W-2 prints cents; its boxes are a source, rounded once here.
        *f = m(d(model::value(&*f)));
    }
    for st in &mut b.states {
        st.wages = m(d(model::value(&st.wages)));
        st.withheld = m(d(model::value(&st.withheld)));
    }
    b
}

/// a's boxes plus sign times b's, field by field.
fn combine_boxes(a: &W2Boxes, b: &W2Boxes, sign: i64) -> W2Boxes {
    let c = |x: &MoneyField, y: &MoneyField| -> MoneyField {
        m(model::value(x) + model::value(y) * sign)
    };
    let none = MoneyField::default();
    let mut out = W2Boxes {
        wages: c(&a.wages, &b.wages),
        federal_withheld: c(&a.federal_withheld, &b.federal_withheld),
        social_security_wages: c(&a.social_security_wages, &b.social_security_wages),
        social_security_withheld: c(&a.social_security_withheld, &b.social_security_withheld),
        medicare_wages: c(&a.medicare_wages, &b.medicare_wages),
        medicare_withheld: c(&a.medicare_withheld, &b.medicare_withheld),
        k401: c(&a.k401, &b.k401),
        k403b: c(&a.k403b, &b.k403b),
        roth_401k: c(&a.roth_401k, &b.roth_401k),
        roth_403b: c(&a.roth_403b, &b.roth_403b),
        hsa: c(&a.hsa, &b.hsa),
        ..Default::default()
    };
    let mut seen = HashSet::new();
    for s in &a.states {
        seen.insert(s.state.clone());
        let o = b.states.iter().rfind(|x| x.state == s.state);
        let (ow, oh, oe) = match o {
            Some(o) => (&o.wages, &o.withheld, &o.employer_hsa),
            None => (&none, &none, &none),
        };
        let mut st = w2boxes::State {
            state: s.state.clone(),
            wages: c(&s.wages, ow),
            withheld: c(&s.withheld, oh),
            ..Default::default()
        };
        if s.employer_hsa.is_set() || oe.is_set() {
            st.employer_hsa = c(&s.employer_hsa, oe);
        }
        out.states.push(st);
    }
    for s in &b.states {
        if !seen.contains(&s.state) {
            let mut st = w2boxes::State {
                state: s.state.clone(),
                wages: c(&none, &s.wages),
                withheld: c(&none, &s.withheld),
                ..Default::default()
            };
            if s.employer_hsa.is_set() {
                st.employer_hsa = c(&none, &s.employer_hsa);
            }
            out.states.push(st);
        }
    }
    out
}
