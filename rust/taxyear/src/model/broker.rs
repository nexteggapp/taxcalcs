//! Expands a BrokerModel, one broker's sales, into sale lots. Ported from
//! the Go package `taxyear/model/broker`.
//!
//! It has no slots: every part adds items on its dates. The holdings
//! register is as of January 1; the year's purchases add lots to it, and a
//! trade sells the lots it names or, by the elected method, first in first
//! out; a 1099-B line is an item, and a trade the line reports is the same
//! item. A planned sale sells at a typed price or today's, 10% lower or
//! higher. See docs/models.md.

use std::collections::{HashMap, HashSet};

use buffa::EnumValue;
use money::USCents;
use proto::commonv1;
use proto::factsv1::{__buffa::oneof::election::Choice, lot_method::Method};
use proto::federalv1::form1099b::HoldingPeriod;
use proto::scenariosv1::{
    self,
    __buffa::oneof::broker_part::{Part, planned_sale::Price},
    __buffa::oneof::item::Figures,
    BrokerModel, BrokerProjection, BrokerTotals, ExpectedDocument, Form8949Box, Form8949Totals,
    GainTerm, ItemKind, ItemStatus, Origin, PlannedSaleTotals, PriceAssumption, SaleLot,
    SaleTotals, broker_part,
    origin::Basis,
};

use super::account::origin;
use super::{Context, Error, Notes};
use crate::internal::civil::{self, Date};
use crate::model;

/// The scale shares are counted at: millionths of a share.
const MICRO: i32 = 6;

fn micro_shares(q: &commonv1::Quantity) -> i64 {
    let (mut u, mut s) = (q.units, q.scale);
    while s < MICRO {
        u = u.wrapping_mul(10);
        s += 1;
    }
    while s > MICRO {
        u /= 10;
        s -= 1;
    }
    u
}

fn quantity(m: i64) -> commonv1::Quantity {
    commonv1::Quantity {
        units: m,
        scale: MICRO,
        ..Default::default()
    }
}

/// A lot in the register, with the shares not yet sold.
#[derive(Clone, Debug)]
struct Lot {
    id: String,
    holding: String,
    acquired: Date,
    shares: i64,
    left: i64,
    basis: USCents,
}

struct Expansion<'a> {
    ctx: &'a Context,
    m: &'a BrokerModel,
    notes: Notes,
    items: Vec<scenariosv1::Item>,
    lots: Vec<Lot>,
    fifo: bool,
    /// The latest trade or form date.
    through: Date,
}

fn sale_of(it: &scenariosv1::Item) -> &SaleLot {
    match &it.figures {
        Some(Figures::Sale(s)) => s,
        _ => <SaleLot as buffa::DefaultInstance>::default_instance(),
    }
}

/// Expands one broker model.
pub fn expand(ctx: &Context, m: &BrokerModel) -> Result<(BrokerProjection, Notes), Error> {
    let mut e = Expansion {
        ctx,
        m,
        notes: Notes::default(),
        items: Vec::new(),
        lots: Vec::new(),
        fifo: true,
        through: Date::default(),
    };
    for el in &ctx.facts().elections {
        if let Some(Choice::LotMethod(lm)) = &el.choice
            && lm.broker == m.id
        {
            e.fifo = lm.method != Method::METHOD_SPECIFIC_LOTS;
        }
    }
    for h in &m.holdings {
        for l in &h.lots {
            let d = civil::from_field(&l.acquired).unwrap_or_default();
            let sh = micro_shares(&l.shares);
            e.lots.push(Lot {
                id: l.id.clone(),
                holding: h.id.clone(),
                acquired: d,
                shares: sh,
                left: sh,
                basis: model::value(&l.basis),
            });
        }
    }
    e.lots.sort_by_key(|l| l.acquired);
    let reported = e.forms();
    e.trades(&reported)?;
    e.planned()?;
    let p = e.projection();
    Ok((p, e.notes))
}

/// Reports whether shares bought on acquired and sold on sold were held
/// more than one year, 26 U.S.C. § 1222.
fn long_term(acquired: Date, sold: Date) -> bool {
    if acquired.is_zero() || sold.is_zero() {
        return false;
    }
    let anniversary = civil::new(acquired.year() + 1, acquired.month(), acquired.day())
        .unwrap_or_else(|_| civil::must(acquired.year() + 1, 3, 1));
    sold.after(anniversary)
}

/// The Form 8949 box a 1099-B's checkbox prints, A to F, or unspecified
/// when it prints none or one that disagrees with the term: a short-term
/// box for a long-term sale is a misreading, not the form.
fn printed_box(checkbox: &str, term: GainTerm) -> Form8949Box {
    let b = match checkbox.trim().to_uppercase().as_str() {
        "A" => Form8949Box::FORM_8949_BOX_A,
        "B" => Form8949Box::FORM_8949_BOX_B,
        "C" => Form8949Box::FORM_8949_BOX_C,
        "D" => Form8949Box::FORM_8949_BOX_D,
        "E" => Form8949Box::FORM_8949_BOX_E,
        "F" => Form8949Box::FORM_8949_BOX_F,
        _ => Form8949Box::FORM_8949_BOX_UNSPECIFIED,
    };
    if b == Form8949Box::FORM_8949_BOX_UNSPECIFIED {
        return b;
    }
    if (b as i32 >= Form8949Box::FORM_8949_BOX_D as i32) != (term == GainTerm::GAIN_TERM_LONG) {
        return Form8949Box::FORM_8949_BOX_UNSPECIFIED;
    }
    b
}

fn form8949(term: GainTerm, on_form: bool, basis_reported: bool) -> Form8949Box {
    let long = term == GainTerm::GAIN_TERM_LONG;
    match () {
        _ if !on_form && long => Form8949Box::FORM_8949_BOX_F,
        _ if !on_form => Form8949Box::FORM_8949_BOX_C,
        _ if basis_reported && long => Form8949Box::FORM_8949_BOX_D,
        _ if basis_reported => Form8949Box::FORM_8949_BOX_A,
        _ if long => Form8949Box::FORM_8949_BOX_E,
        _ => Form8949Box::FORM_8949_BOX_B,
    }
}

fn term_of(v: &EnumValue<GainTerm>) -> GainTerm {
    v.as_known().unwrap_or_default()
}

/// A trade or a purchase, on its date.
struct Event<'a> {
    t: Option<&'a broker_part::from_trades::Trade>,
    buy: Option<&'a broker_part::purchases::Purchase>,
    part: &'a str,
    date: Date,
}

/// One lot's part of a sale: the lot as it was, or none for shares the
/// lots don't cover.
#[derive(Clone, Debug)]
struct Sold {
    l: Option<Lot>,
    shares: i64,
}

impl<'a> Expansion<'a> {
    fn prefix(&self) -> String {
        model::item_id(&["brokers", &self.m.id])
    }

    fn path(&self) -> String {
        format!("model.brokers[{}]", self.m.id)
    }

    /// Adds each 1099-B line, and returns the date and proceeds of each, so
    /// a trade a line reports is not counted twice.
    fn forms(&mut self) -> HashSet<String> {
        let m = self.m;
        let mut reported = HashSet::new();
        for p in &m.parts {
            let Some(Part::FromForms(ff)) = &p.part else {
                continue;
            };
            for st in &ff.statements {
                if !model::usable(&st.source) {
                    continue;
                }
                for (i, t) in st.statement.transactions.iter().enumerate() {
                    let sold = civil::from_field(&t.date_sold_or_disposed).unwrap_or_default();
                    let acq = civil::from_field(&t.date_acquired).unwrap_or_default();
                    let mut term = GainTerm::GAIN_TERM_SHORT;
                    if t.holding_period == HoldingPeriod::HOLDING_PERIOD_LONG_TERM
                        || (t.holding_period == HoldingPeriod::HOLDING_PERIOD_UNSPECIFIED
                            && long_term(acq, sold))
                    {
                        term = GainTerm::GAIN_TERM_LONG;
                    }
                    let mut b = printed_box(&t.form_8949_checkbox, term);
                    if b == Form8949Box::FORM_8949_BOX_UNSPECIFIED {
                        b = form8949(
                            term,
                            true,
                            t.basis_reported_to_irs && !t.noncovered_security,
                        );
                    }
                    let (proceeds, basis, wash) = (
                        model::value(&t.proceeds),
                        model::value(&t.cost_or_other_basis),
                        model::value(&t.wash_sale_loss_disallowed),
                    );
                    reported.insert(format!("{sold}|{}", proceeds.0));
                    self.through = civil::later(self.through, sold);
                    self.items.push(scenariosv1::Item {
                        id: model::item_id(&[
                            &self.prefix(),
                            &sold.to_string(),
                            "sale_lot",
                            &format!("{}-{i}", st.id),
                        ]),
                        date: sold.field(),
                        status: ItemStatus::ITEM_STATUS_MADE.into(),
                        kind: ItemKind::ITEM_KIND_SALE_LOT.into(),
                        origin: origin(Basis::BASIS_DOCUMENT, &p.id, &[&st.id]).into(),
                        figures: Some(
                            SaleLot {
                                acquired: acq.field(),
                                proceeds: model::money(proceeds).into(),
                                basis: model::money(basis).into(),
                                wash_sale_disallowed: model::money(wash).into(),
                                term: term.into(),
                                gain: model::money(proceeds - basis + wash).into(),
                                r#box: b.into(),
                                ..Default::default()
                            }
                            .into(),
                        ),
                        ..Default::default()
                    });
                }
            }
        }
        reported
    }

    /// Runs the year's purchases and trades in date order against the
    /// register, which is as of January 1: a purchase adds a lot, and a
    /// trade sells the lots it names, else first in, first out. Basis comes
    /// from the register.
    fn trades(&mut self, reported: &HashSet<String>) -> Result<(), Error> {
        let events = self.events()?;
        for ev in events {
            if let Some(b) = ev.buy {
                self.buy(b, ev.date);
                continue;
            }
            let Some(tr) = ev.t else {
                continue;
            };
            let proceeds = model::value(&tr.proceeds);
            let used = self.sell(
                &tr.holding,
                &tr.lots,
                micro_shares(&tr.shares),
                &tr.id,
                !tr.basis.is_set(),
            );
            self.through = civil::later(self.through, ev.date);
            if reported.contains(&format!("{}|{}", ev.date, proceeds.0)) {
                continue;
            }
            if tr.basis.is_set() {
                self.reported_trade(tr, ev.part, ev.date, &used);
                continue;
            }
            self.add_sales(
                ev.date,
                &tr.holding,
                &tr.id,
                &used,
                proceeds,
                micro_shares(&tr.shares),
                ItemStatus::ITEM_STATUS_MADE,
                origin(Basis::BASIS_LEDGER, ev.part, &[&tr.id]),
            );
        }
        Ok(())
    }

    /// The year's trades and purchases in date order, a day's purchases
    /// before its sales.
    fn events(&self) -> Result<Vec<Event<'a>>, Error> {
        let m = self.m;
        let mut events = Vec::new();
        for p in &m.parts {
            match &p.part {
                Some(Part::FromTrades(ft)) => {
                    for t in &ft.trades {
                        let d = match civil::from_field(&t.date) {
                            Ok(d) if !d.is_zero() => d,
                            _ => {
                                return Err(Error::Expansion(format!(
                                    "{}: trade {} has no date",
                                    self.path(),
                                    t.id
                                )));
                            }
                        };
                        events.push(Event {
                            t: Some(t),
                            buy: None,
                            part: &p.id,
                            date: d,
                        });
                    }
                }
                Some(Part::Purchases(ps)) => {
                    for b in &ps.purchases {
                        let d = match civil::from_field(&b.date) {
                            Ok(d) if !d.is_zero() => d,
                            _ => {
                                return Err(Error::Expansion(format!(
                                    "{}: purchase {} has no date",
                                    self.path(),
                                    b.id
                                )));
                            }
                        };
                        events.push(Event {
                            t: None,
                            buy: Some(b),
                            part: &p.id,
                            date: d,
                        });
                    }
                }
                _ => {}
            }
        }
        events.sort_by(|a, b| {
            a.date
                .cmp(&b.date)
                .then_with(|| b.buy.is_some().cmp(&a.buy.is_some()))
        });
        Ok(events)
    }

    /// A trade whose basis the broker reports: one item at that basis, its
    /// acquired date and box as given, else the lots' date and the box the
    /// holding period gives. The lots it sold still leave the register.
    fn reported_trade(
        &mut self,
        tr: &broker_part::from_trades::Trade,
        part: &str,
        on: Date,
        used: &[Sold],
    ) {
        let mut acquired = civil::from_field(&tr.acquired).unwrap_or_default();
        if acquired.is_zero()
            && let Some(Sold { l: Some(l), .. }) = used.first()
        {
            acquired = l.acquired;
        }
        let mut term = GainTerm::GAIN_TERM_SHORT;
        if long_term(acquired, on) {
            term = GainTerm::GAIN_TERM_LONG;
        }
        let mut b = tr.r#box;
        if b.to_i32() == Form8949Box::FORM_8949_BOX_UNSPECIFIED as i32 {
            b = form8949(term, false, false).into();
        } else if b.to_i32() >= Form8949Box::FORM_8949_BOX_D as i32 {
            term = GainTerm::GAIN_TERM_LONG;
        } else {
            term = GainTerm::GAIN_TERM_SHORT;
        }
        let (proceeds, basis, wash) = (
            model::value(&tr.proceeds),
            model::value(&tr.basis),
            model::value(&tr.wash_sale_disallowed),
        );
        self.items.push(scenariosv1::Item {
            id: model::item_id(&[&self.prefix(), &on.to_string(), "sale_lot", &tr.id]),
            date: on.field(),
            status: ItemStatus::ITEM_STATUS_MADE.into(),
            kind: ItemKind::ITEM_KIND_SALE_LOT.into(),
            origin: origin(Basis::BASIS_LEDGER, part, &[&tr.id]).into(),
            figures: Some(
                SaleLot {
                    holding: tr.holding.clone(),
                    acquired: acquired.field(),
                    shares: tr.shares.clone(),
                    proceeds: model::money(proceeds).into(),
                    basis: model::money(basis).into(),
                    wash_sale_disallowed: model::money(wash).into(),
                    term: term.into(),
                    gain: model::money(proceeds - basis + wash).into(),
                    r#box: b,
                    ..Default::default()
                }
                .into(),
            ),
            ..Default::default()
        });
    }

    /// Adds a purchase to the register as a lot of its own, bought on its
    /// date at its cost. The register is as of January 1, so the year's
    /// purchases are the lots it lacks.
    fn buy(&mut self, b: &broker_part::purchases::Purchase, on: Date) {
        let sh = micro_shares(&b.shares);
        self.lots.push(Lot {
            id: b.id.clone(),
            holding: b.holding.clone(),
            acquired: on,
            shares: sh,
            left: sh,
            basis: model::value(&b.cost),
        });
        self.lots.sort_by_key(|l| l.acquired);
        self.through = civil::later(self.through, on);
    }

    /// Takes shares from a holding's lots: those named, else first in first
    /// out. Under the specific-lots method a sale naming none is asked
    /// about, and so is one the lots don't cover, unless ask is false
    /// because the broker reports the sale's basis.
    fn sell(
        &mut self,
        holding: &str,
        named: &[String],
        mut shares: i64,
        entry: &str,
        ask: bool,
    ) -> Vec<Sold> {
        let mut out = Vec::new();
        let want: HashSet<&str> = named.iter().map(|s| s.as_str()).collect();
        let prefix = self.prefix();
        if named.is_empty() && !self.fifo && ask {
            self.notes.ask(model::new_question(
                model::item_id(&[&prefix, "lots", entry]),
                prefix.clone(),
                format!("Which lots did your sale {entry} at {} sell?", self.m.name),
                "The earliest-bought shares are counted as sold until you say.",
                format!("{}.parts", self.path()),
                &[],
            ));
        }
        for l in self.lots.iter_mut() {
            if shares == 0 {
                break;
            }
            if l.holding != holding
                || l.left == 0
                || (!named.is_empty() && !want.contains(l.id.as_str()))
            {
                continue;
            }
            let n = l.left.min(shares);
            l.left -= n;
            shares -= n;
            out.push(Sold {
                l: Some(l.clone()),
                shares: n,
            });
        }
        if shares > 0 && ask {
            self.notes.ask(model::new_question(
                model::item_id(&[&prefix, "basis", entry]),
                prefix.clone(),
                format!(
                    "What did the shares sold in {entry} at {} cost? The lots held don't cover the sale.",
                    self.m.name
                ),
                "The shares the lots don't cover are counted with no basis until you say.",
                format!("{}.holdings", self.path()),
                &[],
            ));
            out.push(Sold { l: None, shares });
        }
        out
    }

    /// Adds one item per holding period a sale's lots fall in.
    #[allow(clippy::too_many_arguments)]
    fn add_sales(
        &mut self,
        on: Date,
        holding: &str,
        entry: &str,
        used: &[Sold],
        proceeds: USCents,
        total: i64,
        status: ItemStatus,
        origin: Origin,
    ) {
        #[derive(Default)]
        struct Group {
            shares: i64,
            basis: USCents,
            acquired: Date,
            lots: Vec<String>,
        }
        let mut groups: HashMap<i32, Group> = HashMap::new();
        for u in used {
            let mut term = GainTerm::GAIN_TERM_SHORT;
            let mut basis = USCents(0);
            let mut acq = Date::default();
            if let Some(l) = &u.l {
                basis = model::scale(l.basis, USCents(u.shares), USCents(l.shares));
                acq = l.acquired;
                if long_term(acq, on) {
                    term = GainTerm::GAIN_TERM_LONG;
                }
            }
            let g = groups.entry(term as i32).or_insert_with(|| Group {
                acquired: acq,
                ..Default::default()
            });
            g.shares += u.shares;
            g.basis += basis;
            if let Some(l) = &u.l {
                g.lots.push(l.id.clone());
            }
        }
        let several = groups.len() > 1;
        for term in [GainTerm::GAIN_TERM_SHORT, GainTerm::GAIN_TERM_LONG] {
            let Some(g) = groups.get(&(term as i32)) else {
                continue;
            };
            let mut id = model::item_id(&[&self.prefix(), &on.to_string(), "sale_lot", entry]);
            if several {
                let word = if term == GainTerm::GAIN_TERM_SHORT {
                    "short"
                } else {
                    "long"
                };
                id = model::item_id(&[&id, word]);
            }
            let p = model::scale(proceeds, USCents(g.shares), USCents(total.max(1)));
            let lot_id = if g.lots.len() == 1 {
                g.lots[0].clone()
            } else {
                String::new()
            };
            self.items.push(scenariosv1::Item {
                id,
                date: on.field(),
                status: status.into(),
                kind: ItemKind::ITEM_KIND_SALE_LOT.into(),
                origin: origin.clone().into(),
                figures: Some(
                    SaleLot {
                        holding: holding.to_string(),
                        lot: lot_id,
                        acquired: g.acquired.field(),
                        shares: quantity(g.shares).into(),
                        proceeds: model::money(p).into(),
                        basis: model::money(g.basis).into(),
                        term: term.into(),
                        gain: model::money(p - g.basis).into(),
                        r#box: form8949(term, false, false).into(),
                        ..Default::default()
                    }
                    .into(),
                ),
                ..Default::default()
            });
        }
    }

    /// Adds the planned sales and gains.
    fn planned(&mut self) -> Result<(), Error> {
        let m = self.m;
        for p in &m.parts {
            match &p.part {
                Some(Part::PlannedSale(s)) if s.actual.is.is_empty() => {
                    let d = match civil::from_field(&s.date) {
                        Ok(d) if !d.is_zero() => d,
                        _ => {
                            return Err(Error::Expansion(format!(
                                "{}.parts[{}].planned_sale.date is not a day",
                                self.path(),
                                p.id
                            )));
                        }
                    };
                    let shares = micro_shares(&s.shares);
                    let price = self.price(s);
                    let proceeds = model::scale(price, USCents(shares), USCents(1_000_000));
                    let named = if s.lot.is_empty() {
                        Vec::new()
                    } else {
                        vec![s.lot.clone()]
                    };
                    let used = self.sell(&s.holding, &named, shares, &p.id, true);
                    let before = self.items.len();
                    self.add_sales(
                        d,
                        &s.holding,
                        &p.id,
                        &used,
                        proceeds,
                        shares,
                        ItemStatus::ITEM_STATUS_EXPECTED,
                        origin(Basis::BASIS_ASSUMED, &p.id, &[]),
                    );
                    let losses = self.items[before..]
                        .iter()
                        .filter(|it| model::value(&sale_of(it).gain).0 < 0)
                        .count();
                    for _ in 0..losses {
                        self.wash_sale(&p.id, &s.holding, d);
                    }
                }
                Some(Part::PlannedGain(g)) if g.actual.is.is_empty() => {
                    let d = match civil::from_field(&g.date) {
                        Ok(d) if !d.is_zero() => d,
                        _ => {
                            return Err(Error::Expansion(format!(
                                "{}.parts[{}].planned_gain.date is not a day",
                                self.path(),
                                p.id
                            )));
                        }
                    };
                    let term = term_of(&g.term);
                    self.items.push(scenariosv1::Item {
                        id: model::item_id(&[&self.prefix(), &d.to_string(), "sale_lot", &p.id]),
                        date: d.field(),
                        status: ItemStatus::ITEM_STATUS_EXPECTED.into(),
                        kind: ItemKind::ITEM_KIND_SALE_LOT.into(),
                        origin: origin(Basis::BASIS_ASSUMED, &p.id, &[]).into(),
                        figures: Some(
                            SaleLot {
                                term: g.term,
                                gain: model::money(model::value(&g.amount)).into(),
                                r#box: form8949(term, false, false).into(),
                                ..Default::default()
                            }
                            .into(),
                        ),
                        ..Default::default()
                    });
                }
                _ => {}
            }
        }
        Ok(())
    }

    /// A planned sale's price per share: typed, or the holding's price on
    /// as_of, 10% lower or higher.
    fn price(&self, s: &broker_part::PlannedSale) -> USCents {
        let assumption = match &s.price {
            Some(Price::PerShare(p)) => return model::value(&**p),
            Some(Price::Assumption(a)) => a.as_known().unwrap_or_default(),
            None => PriceAssumption::default(),
        };
        let mut today = USCents(0);
        for h in &self.m.holdings {
            if h.id == s.holding {
                today = model::value(&h.price);
            }
        }
        match assumption {
            PriceAssumption::PRICE_ASSUMPTION_TEN_PERCENT_LOWER => {
                model::scale(today, USCents(9), USCents(10))
            }
            PriceAssumption::PRICE_ASSUMPTION_TEN_PERCENT_HIGHER => {
                model::scale(today, USCents(11), USCents(10))
            }
            _ => today,
        }
    }

    /// Asks about a planned sale at a loss with a purchase of the same
    /// holding within 30 days either side, 26 U.S.C. § 1091.
    fn wash_sale(&mut self, part: &str, holding: &str, on: Date) {
        let m = self.m;
        for p in &m.parts {
            let Some(Part::Purchases(ps)) = &p.part else {
                continue;
            };
            for b in &ps.purchases {
                let d = civil::from_field(&b.date).unwrap_or_default();
                if b.holding == holding && !d.is_zero() && d.days_from(on).abs() <= 30 {
                    let prefix = self.prefix();
                    self.notes.ask(model::new_question(
                        model::item_id(&[&prefix, "wash-sale", part]),
                        prefix.clone(),
                        format!(
                            "Your planned sale on {} is at a loss, and you bought the same holding on {}. Is it a wash sale?",
                            on.short(),
                            d.short()
                        ),
                        "The loss counts in full until you say.",
                        format!("{}.parts[{part}]", self.path()),
                        &["Yes", "No"],
                    ));
                    return;
                }
            }
        }
    }

    fn projection(&mut self) -> BrokerProjection {
        model::sort_items(&mut self.items);
        model::round_items(&mut self.items);
        let mut out = BrokerProjection {
            id: self.m.id.clone(),
            items: self.items.clone(),
            so_far_through: self.through.field(),
            ..Default::default()
        };
        let (made, rest): (Vec<&scenariosv1::Item>, Vec<&scenariosv1::Item>) = self
            .items
            .iter()
            .partition(|it| it.status == ItemStatus::ITEM_STATUS_MADE);
        out.so_far = totals(&made).into();
        out.rest = totals(&rest).into();
        out.year = totals(&self.items.iter().collect::<Vec<_>>()).into();
        out.planned = self.planned_totals();
        if !self.items.is_empty() {
            let has_form = self
                .m
                .parts
                .iter()
                .any(|p| matches!(p.part, Some(Part::FromForms(_))));
            if !has_form {
                out.expected = vec![ExpectedDocument {
                    form: "Form1099B".to_string(),
                    due: model::forms_due(self.ctx.year()).field(),
                    ..Default::default()
                }];
            }
        }
        out
    }

    /// Adds each planned sale's or planned gain's lots, by part.
    fn planned_totals(&self) -> Vec<PlannedSaleTotals> {
        let mut out = Vec::new();
        for p in &self.m.parts {
            if !matches!(
                p.part,
                Some(Part::PlannedSale(_)) | Some(Part::PlannedGain(_))
            ) {
                continue;
            }
            let (mut short, mut long) = (USCents(0), USCents(0));
            for it in &self.items {
                if it.origin.part != p.id {
                    continue;
                }
                let s = sale_of(it);
                if s.term == GainTerm::GAIN_TERM_LONG {
                    long += model::value(&s.gain);
                } else {
                    short += model::value(&s.gain);
                }
            }
            out.push(PlannedSaleTotals {
                id: p.id.clone(),
                gain: model::money(short + long).into(),
                short_term_gain: model::money(short).into(),
                long_term_gain: model::money(long).into(),
                ..Default::default()
            });
        }
        out
    }
}

/// Adds the lots, and each Form 8949 box's columns.
fn totals(items: &[&scenariosv1::Item]) -> BrokerTotals {
    let (mut proceeds, mut basis, mut short, mut long) =
        (USCents(0), USCents(0), USCents(0), USCents(0));
    let mut boxes: HashMap<i32, Form8949Totals> = HashMap::new();
    for it in items {
        let s = sale_of(it);
        proceeds += model::value(&s.proceeds);
        basis += model::value(&s.basis);
        if s.term == GainTerm::GAIN_TERM_LONG {
            long += model::value(&s.gain);
        } else {
            short += model::value(&s.gain);
        }
        let b = boxes
            .entry(s.r#box.to_i32())
            .or_insert_with(|| Form8949Totals {
                r#box: s.r#box,
                proceeds: model::money(USCents(0)).into(),
                basis: model::money(USCents(0)).into(),
                adjustments: model::money(USCents(0)).into(),
                gain: model::money(USCents(0)).into(),
                ..Default::default()
            });
        b.proceeds = model::money(model::value(&b.proceeds) + model::value(&s.proceeds)).into();
        b.basis = model::money(model::value(&b.basis) + model::value(&s.basis)).into();
        b.adjustments =
            model::money(model::value(&b.adjustments) + model::value(&s.wash_sale_disallowed))
                .into();
        b.gain = model::money(model::value(&b.gain) + model::value(&s.gain)).into();
    }
    let mut out = BrokerTotals {
        items: SaleTotals {
            proceeds: model::money(proceeds).into(),
            basis: model::money(basis).into(),
            short_term_gain: model::money(short).into(),
            long_term_gain: model::money(long).into(),
            ..Default::default()
        }
        .into(),
        ..Default::default()
    };
    for k in Form8949Box::FORM_8949_BOX_A as i32..=Form8949Box::FORM_8949_BOX_F as i32 {
        if let Some(b) = boxes.remove(&k) {
            out.boxes.push(b);
        }
    }
    out
}
