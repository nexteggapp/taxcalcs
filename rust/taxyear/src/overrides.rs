//! Overriding a model's figures so far: overrides.go.
//!
//! A job applies its own overrides, as its withholding reads them; tax paid
//! has none. Every other model's figure so far is overridden here, on its
//! finished projection: an ADJUSTMENT item dated so_far_through carries
//! the difference, and so far's and the year's totals and boxes move with
//! it, so every total that reads the figure reads the typed one.

use std::collections::HashSet;

use buffa::{Message, MessageName};
use buffa_descriptor::{DynamicMessage, FieldDescriptor, Value};
use money::USCents;
use proto::scenariosv1::{
    BrokerProjection, BrokerTotals, ContributionKind, Form8949Box, Form8949Totals, GainTerm, Item,
    ItemKind, ItemStatus, Origin, Override, SaleLot, Working, origin::Basis,
};

use crate::model;
use crate::reflect::{self, MONEY};
use crate::{goquote, round};

/// figuresCase is the Item figures field each list's items use.
fn figures_case(list: &str) -> &'static str {
    match list {
        "businesses" => "business",
        "accounts" => "account",
        "brokers" => "sale",
        "retirement" => "contribution",
        "itemised" => "itemised",
        "other_income" => "other_income",
        _ => "",
    }
}

/// overridesAt are the overrides of one model's figures so far, by the
/// line each names after so_far.items.
pub(crate) fn overrides_at(overrides: &[Override], at: &str) -> Vec<Override> {
    let prefix = format!("{at}.so_far.items.");
    overrides
        .iter()
        .filter(|o| o.path.starts_with(&prefix))
        .cloned()
        .collect()
}

fn not_a_line(line: &str) -> String {
    format!(
        "{} is not a line of the figures so far",
        goquote::quote(line)
    )
}

/// overridden returns a copy of a model's finished projection with its
/// overrides applied, and each override's working: the figure computed
/// today and the sources it rests on.
pub(crate) fn overridden<P: Message + MessageName + Default>(
    list: &str,
    at: &str,
    p: &P,
    overrides: &[Override],
) -> Result<(P, Vec<Working>), String> {
    let internal = || "taxyear: the projection can't be reflected over".to_string();
    let mut m = reflect::to_dyn(p).ok_or_else(internal)?;
    let mut out = Vec::new();
    let prefix = format!("{at}.so_far.items.");
    for o in overrides {
        let line = o.path.strip_prefix(&prefix).unwrap_or(&o.path).to_string();
        if list == "other_income" && line == "taxable" {
            stated_taxable(&mut m);
        }
        let have = line_of(list, &mut m, "so_far", &line)
            .map_err(|e| format!("override {}: {e}", o.path))?;
        let mut w = Working {
            path: o.path.clone(),
            automatic: model::money(have).into(),
            sources: made_sources(&m),
            ..Default::default()
        };
        if o.was.is_set() {
            w.sources_changed = model::value(&o.was) != have;
        }
        out.push(w);
        let delta = model::value(&o.amount) - have;
        if delta == USCents(0) {
            continue;
        }
        let it = adjustment(list, at, &m, &line, delta)
            .map_err(|e| format!("override {}: {e}", o.path))?;
        let it_dyn = reflect::to_dyn(&it).ok_or_else(internal)?;
        if let Some(fd) = reflect::field_of(&m, "items")
            && let Some(items) = reflect::list_mut(&mut m, fd)
        {
            items.push(Value::Message(it_dyn));
        }
        for period in ["so_far", "year"] {
            add_line(list, &mut m, period, &line, delta, &it);
        }
    }
    if list == "brokers" {
        let mut b: BrokerProjection = reflect::from_dyn(&m).ok_or_else(internal)?;
        {
            let BrokerProjection {
                so_far, rest, year, ..
            } = &mut b;
            let mut none_s = Vec::new();
            let mut none_r = Vec::new();
            let s = match so_far.as_option_mut() {
                Some(t) => &mut t.boxes,
                None => &mut none_s,
            };
            let r = match rest.as_option_mut() {
                Some(t) => &mut t.boxes,
                None => &mut none_r,
            };
            let boxes = round::broker_year(s, r);
            year.get_or_insert_default().boxes = boxes;
        }
        m = reflect::to_dyn(&b).ok_or_else(internal)?;
    }
    sort_items(&mut m);
    Ok((reflect::from_dyn(&m).ok_or_else(internal)?, out))
}

/// statedTaxable writes taxable on each of an other income projection's
/// items that leaves it unset, as its gross, which is what the totals
/// already count. An adjustment of taxable then adds up with the items.
fn stated_taxable(m: &mut DynamicMessage) {
    let Some(fd) = reflect::field_of(m, "items") else {
        return;
    };
    let Some(items) = reflect::list_mut(m, fd) else {
        return;
    };
    for v in items.iter_mut() {
        let Some(it) = reflect::as_message_mut(v) else {
            continue;
        };
        let Some(ff) = reflect::field_of(it, "other_income") else {
            continue;
        };
        if !reflect::has(it, ff) {
            continue;
        }
        let Some(f) = reflect::mutable(it, ff) else {
            continue;
        };
        let (Some(tf), Some(gf)) = (
            reflect::field_of(f, "taxable"),
            reflect::field_of(f, "gross"),
        ) else {
            continue;
        };
        if reflect::has(f, tf) {
            continue;
        }
        let gross = reflect::message(f, gf).map(reflect::minor).unwrap_or(0);
        set_money(f, tf, USCents(gross));
    }
}

fn set_money(m: &mut DynamicMessage, fd: &FieldDescriptor, c: USCents) {
    if let Some(v) = reflect::money_dyn(c.0) {
        reflect::set(m, fd, Value::Message(v));
    }
}

fn money_at(m: &DynamicMessage, fd: &FieldDescriptor) -> USCents {
    USCents(reflect::message(m, fd).map(reflect::minor).unwrap_or(0))
}

/// The period's items message, made if unset, as Go's Mutable makes it.
fn items_mut<'m>(m: &'m mut DynamicMessage, period: &str) -> Option<&'m mut DynamicMessage> {
    let totals = reflect::mutable_named(m, period)?;
    reflect::mutable_named(totals, "items")
}

/// lineOf is a period's item line of a projection: the money a line names
/// in its items, or for a list whose items are one amount, that amount.
fn line_of(
    list: &str,
    m: &mut DynamicMessage,
    period: &str,
    line: &str,
) -> Result<USCents, String> {
    let fd = items_line(list, m, period, line)?;
    let Some(items) = items_mut(m, period) else {
        return Ok(USCents(0));
    };
    match fd {
        None => Ok(USCents(reflect::minor(items))),
        Some(fd) => Ok(money_at(items, fd)),
    }
}

/// itemsLine makes a period's items message and finds the field a line
/// names in it; None means the items are the amount itself.
fn items_line(
    list: &str,
    m: &mut DynamicMessage,
    period: &str,
    line: &str,
) -> Result<Option<&'static FieldDescriptor>, String> {
    let Some(items) = items_mut(m, period) else {
        return Err(not_a_line(line));
    };
    if reflect::full_name(items) == MONEY {
        if line != "amount" {
            return Err(not_a_line(line));
        }
        return Ok(None);
    }
    let line = if list == "other_income" && line == "amount" {
        "gross"
    } else {
        line
    };
    match reflect::field_of(items, line) {
        Some(fd) if reflect::is_money(fd) => Ok(Some(fd)),
        _ => Err(not_a_line(line)),
    }
}

/// adjustment is the ADJUSTMENT item that carries delta on the line.
fn adjustment(
    list: &str,
    at: &str,
    m: &DynamicMessage,
    line: &str,
    delta: USCents,
) -> Result<Item, String> {
    let id = reflect::string_named(m, "id");
    let through = reflect::message_named(m, "so_far_through")
        .and_then(reflect::from_dyn::<proto::commonv1::Date>);
    let it = Item {
        id: model::item_id(&[list, id, "override", line]),
        date: through.into(),
        status: ItemStatus::ITEM_STATUS_MADE.into(),
        kind: ItemKind::ITEM_KIND_ADJUSTMENT.into(),
        origin: Origin {
            basis: Basis::BASIS_OVERRIDE.into(),
            r#override: format!("{at}.so_far.items.{line}"),
            ..Default::default()
        }
        .into(),
        ..Default::default()
    };
    let internal = || "taxyear: the item can't be reflected over".to_string();
    let mut r = reflect::to_dyn(&it).ok_or_else(internal)?;
    let Some(fd) = reflect::field_of(&r, figures_case(list)) else {
        return Err(format!("no figures for {list}"));
    };
    let Some(f) = reflect::mutable(&mut r, fd) else {
        return Err(format!("no figures for {list}"));
    };
    let set_named = |f: &mut DynamicMessage, name: &str, v: Value| {
        if let Some(fd) = reflect::field_of(f, name) {
            reflect::set(f, fd, v);
        }
    };
    let money = |c: USCents| reflect::money_dyn(c.0).map(Value::Message);
    match list {
        "itemised" => {
            if let Some(v) = money(delta) {
                set_named(f, "amount", v);
            }
        }
        "retirement" => {
            let k = match line {
                "traditional_ira" => ContributionKind::CONTRIBUTION_KIND_TRADITIONAL_IRA,
                "roth_ira" => ContributionKind::CONTRIBUTION_KIND_ROTH_IRA,
                "hsa" => ContributionKind::CONTRIBUTION_KIND_HSA,
                _ => return Err(not_a_line(line)),
            };
            set_named(f, "kind", Value::EnumNumber(k as i32));
            if let Some(v) = money(delta) {
                set_named(f, "amount", v);
            }
        }
        "brokers" => {
            let (term, bx) = broker_term(m, line);
            let mut s = SaleLot {
                term: term.into(),
                r#box: bx.into(),
                ..Default::default()
            };
            match line {
                "proceeds" => {
                    s.proceeds = model::money(delta).into();
                    s.gain = model::money(delta).into();
                }
                "basis" => {
                    s.basis = model::money(delta).into();
                    s.gain = model::money(-delta).into();
                }
                "short_term_gain" | "long_term_gain" => s.gain = model::money(delta).into(),
                _ => return Err(not_a_line(line)),
            }
            if let Some(v) = reflect::to_dyn(&s) {
                reflect::set(&mut r, fd, Value::Message(v));
            }
            return reflect::from_dyn(&r).ok_or_else(internal);
        }
        _ => {
            let line = if list == "other_income" && line == "amount" {
                "gross"
            } else {
                line
            };
            let Some(lf) = reflect::field_of(f, line) else {
                return Err(not_a_line(line));
            };
            if let Some(v) = money(delta) {
                reflect::set(f, lf, v);
            }
        }
    }
    reflect::from_dyn(&r).ok_or_else(internal)
}

/// brokerTerm is the term and Form 8949 box a broker's adjustment lot goes
/// in: a gain line's own term, else the term of the larger gain so far;
/// the box is the first so far has for the term, else A or D.
fn broker_term(m: &DynamicMessage, line: &str) -> (GainTerm, Form8949Box) {
    let b: BrokerProjection = reflect::from_dyn(m).unwrap_or_default();
    let mut term = GainTerm::GAIN_TERM_SHORT;
    match line {
        "long_term_gain" => term = GainTerm::GAIN_TERM_LONG,
        "short_term_gain" => {}
        _ => {
            let s = model::value(&b.so_far.items.short_term_gain);
            let l = model::value(&b.so_far.items.long_term_gain);
            if abs(l) > abs(s) {
                term = GainTerm::GAIN_TERM_LONG;
            }
        }
    }
    let long = term == GainTerm::GAIN_TERM_LONG;
    for x in &b.so_far.boxes {
        if is_long(x.r#box.to_i32()) == long {
            let bx = x
                .r#box
                .as_known()
                .unwrap_or(Form8949Box::FORM_8949_BOX_UNSPECIFIED);
            return (term, bx);
        }
    }
    if long {
        return (term, Form8949Box::FORM_8949_BOX_D);
    }
    (term, Form8949Box::FORM_8949_BOX_A)
}

fn is_long(b: i32) -> bool {
    b >= Form8949Box::FORM_8949_BOX_D as i32
}

fn abs(v: USCents) -> USCents {
    if v < USCents(0) {
        return -v;
    }
    v
}

/// add adds d to a money field, unset reading as zero.
fn add(dst: &mut DynamicMessage, fd: &FieldDescriptor, d: USCents) {
    let v = money_at(dst, fd);
    set_money(dst, fd, v + d);
}

/// addLine adds delta to a period's line and to the boxes the line
/// reaches.
fn add_line(
    list: &str,
    m: &mut DynamicMessage,
    period: &str,
    line: &str,
    delta: USCents,
    it: &Item,
) {
    let Ok(fd) = items_line(list, m, period, line) else {
        return;
    };
    if let Some(items) = items_mut(m, period) {
        match fd {
            None => {
                if let Some(mf) = reflect::field_of(items, "minor") {
                    let v = reflect::minor(items);
                    reflect::set(items, mf, Value::I64(v + delta.0));
                }
            }
            Some(fd) => {
                add(items, fd, delta);
                if list == "other_income"
                    && line == "amount"
                    && let Some(tf) = reflect::field_of(items, "taxable")
                    && reflect::has(items, tf)
                {
                    add(items, tf, delta);
                }
            }
        }
    }
    match list {
        "accounts" | "other_income" => {
            let Some(totals) = reflect::mutable_named(m, period) else {
                return;
            };
            let Some(boxes) = reflect::mutable_named(totals, "boxes") else {
                return;
            };
            if let Some(fd) = fd
                && let Some(bf) = reflect::field_of(boxes, fd.name())
            {
                add(boxes, bf, delta);
            }
            if list == "other_income"
                && line == "amount"
                && let Some(tf) = reflect::field_of(boxes, "taxable")
                && reflect::has(boxes, tf)
            {
                add(boxes, tf, delta);
            }
        }
        "itemised" => {
            // The kind's one Schedule A input, the one the year sets.
            let input = reflect::message_named(m, "year")
                .and_then(|y| reflect::message_named(y, "boxes"))
                .and_then(|yb| reflect::set_fields(yb).first().copied());
            if let Some(input) = input
                && let Some(totals) = reflect::mutable_named(m, period)
                && let Some(boxes) = reflect::mutable_named(totals, "boxes")
            {
                add(boxes, input, delta);
            }
        }
        "businesses" => {
            let Some(totals) = reflect::mutable_named(m, period) else {
                return;
            };
            let Some(boxes) = reflect::mutable_named(totals, "boxes") else {
                return;
            };
            let (at, sign) = match line {
                "receipts" => ("gross_receipts", 1),
                "fees" => ("returns_and_allowances", -1),
                "expenses" => ("total_expenses", -1),
                _ => return,
            };
            if let Some(f) = reflect::field_of(boxes, at) {
                add(boxes, f, delta);
            }
            if let Some(f) = reflect::field_of(boxes, "net_profit") {
                add(boxes, f, USCents(sign * delta.0));
            }
        }
        "brokers" => {
            if period != "so_far" {
                return;
            }
            let s = match &it.figures {
                Some(proto::scenariosv1::__buffa::oneof::item::Figures::Sale(s)) => (**s).clone(),
                _ => SaleLot::default(),
            };
            if line != "short_term_gain" && line != "long_term_gain" {
                // A proceeds or basis line moves the lot's gain, which is its
                // term's.
                let other = if s.term.as_known() == Some(GainTerm::GAIN_TERM_LONG) {
                    "long_term_gain"
                } else {
                    "short_term_gain"
                };
                for p in ["so_far", "year"] {
                    if let Ok(Some(f)) = items_line(list, m, p, other)
                        && let Some(i) = items_mut(m, p)
                    {
                        add(i, f, model::value(&s.gain));
                    }
                }
            }
            let Some(totals) = reflect::message_named(m, period) else {
                return;
            };
            let mut c: BrokerTotals = reflect::from_dyn(totals).unwrap_or_default();
            let at = c.boxes.iter().rposition(|b| b.r#box == s.r#box);
            let i = match at {
                Some(i) => i,
                None => {
                    c.boxes.push(Form8949Totals {
                        r#box: s.r#box,
                        proceeds: model::money(USCents(0)).into(),
                        basis: model::money(USCents(0)).into(),
                        adjustments: model::money(USCents(0)).into(),
                        gain: model::money(USCents(0)).into(),
                        ..Default::default()
                    });
                    c.boxes.len() - 1
                }
            };
            let bx = &mut c.boxes[i];
            bx.proceeds =
                model::money(model::value(&bx.proceeds) + model::value(&s.proceeds)).into();
            bx.basis = model::money(model::value(&bx.basis) + model::value(&s.basis)).into();
            if line == "short_term_gain" || line == "long_term_gain" {
                // A gain typed on its own is an adjustment, Form 8949
                // column (g).
                bx.adjustments = model::money(model::value(&bx.adjustments) + delta).into();
            }
            bx.gain = model::money(
                model::value(&bx.proceeds) - model::value(&bx.basis)
                    + model::value(&bx.adjustments),
            )
            .into();
            if let (Some(fd), Some(v)) = (reflect::field_of(m, period), reflect::to_dyn(&c)) {
                reflect::set(m, fd, Value::Message(v));
            }
        }
        _ => {}
    }
}

/// madeSources are the ids of the parts and evidence a projection's items
/// so far rest on.
fn made_sources(m: &DynamicMessage) -> Vec<String> {
    let mut seen = HashSet::new();
    let mut out = Vec::new();
    for v in reflect::list_named(m, "items") {
        let Some(it) = reflect::as_message(v).and_then(reflect::from_dyn::<Item>) else {
            continue;
        };
        if it.status.as_known() != Some(ItemStatus::ITEM_STATUS_MADE)
            || it.kind.as_known() == Some(ItemKind::ITEM_KIND_ADJUSTMENT)
        {
            continue;
        }
        let mut ids = vec![it.origin.part.clone()];
        ids.extend(it.origin.evidence.iter().cloned());
        for id in ids {
            if !id.is_empty() && seen.insert(id.clone()) {
                out.push(id);
            }
        }
    }
    out
}

/// sortItems puts a projection's items back in date order.
fn sort_items(m: &mut DynamicMessage) {
    let Some(fd) = reflect::field_of(m, "items") else {
        return;
    };
    let mut items: Vec<Item> = reflect::list(m, fd)
        .iter()
        .filter_map(reflect::as_message)
        .filter_map(reflect::from_dyn)
        .collect();
    model::sort_items(&mut items);
    let values: Vec<Value> = items
        .iter()
        .filter_map(reflect::to_dyn)
        .map(Value::Message)
        .collect();
    if !values.is_empty() {
        reflect::set(m, fd, Value::List(values));
    }
}

/// withOverrideWorkings merges each override's working into a model's
/// workings: the computed figure and sources go on the working of the same
/// path, or a new one.
pub(crate) fn with_override_workings(ws: &[Working], overrides: &[Working]) -> Vec<Working> {
    let mut out = ws.to_vec();
    for o in overrides {
        let mut found = false;
        for w in out.iter_mut() {
            if w.path == o.path {
                w.automatic = o.automatic.clone();
                w.sources = o.sources.clone();
                w.sources_changed = o.sources_changed;
                found = true;
            }
        }
        if !found {
            out.push(o.clone());
        }
    }
    out
}
