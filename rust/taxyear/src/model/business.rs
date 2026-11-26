//! Expands a BusinessModel, one self-employment business, into receipts,
//! fees, expenses and months. Ported from the Go package
//! `taxyear/model/business`.
//!
//! Its slots are months, filled as an account's are: a YearTotal for the
//! lines it states, then evidence, then Monthly from its start, then the
//! default, the pace so far. A client's 1099-NEC or 1099-K states that
//! client's receipts for the year, and an ADJUSTMENT reconciles the bank
//! receipts from the client to it. See docs/models.md.

use std::collections::{BTreeMap, HashMap};

use money::USCents;
use proto::scenariosv1::{
    self,
    __buffa::oneof::business_part::{Part, from_forms::form::Form as FormKind},
    __buffa::oneof::r#match::To,
    BusinessEntryKind, BusinessFigures, BusinessModel, BusinessProjection, BusinessTotals,
    ClientForm, ExpectedDocument, ItemKind, ItemStatus, Origin, ScheduleCExpense, ScheduleCLines,
    origin::Basis,
};

use super::account::origin;
use super::{Context, Error, Notes};
use crate::internal::civil::{self, Date};
use crate::model;

/// Receipts, fees and expenses.
type Lines = [USCents; 3];

const RECEIPTS: usize = 0;
const FEES: usize = 1;
const EXPENSES: usize = 2;

fn plus(mut l: Lines, o: &Lines) -> Lines {
    for i in 0..l.len() {
        l[i] += o[i];
    }
    l
}

fn lines_proto(l: &Lines, client: &str) -> BusinessFigures {
    BusinessFigures {
        receipts: model::money(l[RECEIPTS]).into(),
        fees: model::money(l[FEES]).into(),
        expenses: model::money(l[EXPENSES]).into(),
        client: client.to_string(),
        ..Default::default()
    }
}

#[derive(Clone, Debug)]
struct Item {
    id: String,
    date: Date,
    status: ItemStatus,
    kind: ItemKind,
    origin: Origin,
    l: Lines,
    client: String,
}

struct Expansion<'a> {
    ctx: &'a Context,
    m: &'a BusinessModel,
    notes: Notes,
    items: Vec<Item>,
    end: Date,
    /// Each client's receipts a form states. Go keeps a map and reconciles
    /// in its random order; this reconciles in the clients' order.
    stated: BTreeMap<String, USCents>,
    /// The items of expenses bought once, on Schedule C line 13, which the
    /// pace so far leaves out.
    one_off: HashMap<String, bool>,
}

/// Expands one business model.
pub fn expand(ctx: &Context, m: &BusinessModel) -> Result<(BusinessProjection, Notes), Error> {
    let mut e = Expansion {
        ctx,
        m,
        notes: Notes::default(),
        items: Vec::new(),
        end: Date::default(),
        stated: BTreeMap::new(),
        one_off: HashMap::new(),
    };
    e.evidence()?;
    e.reconcile();
    e.rest();
    let p = e.projection();
    Ok((p, e.notes))
}

/// The client a form is from: the form's client, or when it names none, its
/// payer. A form states that client's receipts for the whole year, so the
/// ledger's receipts from them are reconciled to it rather than added to
/// it.
fn form_client(client: &str, payer: &str) -> String {
    if !client.is_empty() {
        return client.to_string();
    }
    payer.to_string()
}

/// Schedule C's lines from the items: line 1 the receipts, line 2 the fees
/// and refunds, line 28 the expenses, line 31 what is left.
fn totals(l: &Lines) -> BusinessTotals {
    BusinessTotals {
        items: lines_proto(l, "").into(),
        boxes: ScheduleCLines {
            gross_receipts: model::money(l[RECEIPTS]).into(),
            returns_and_allowances: model::money(l[FEES]).into(),
            total_expenses: model::money(l[EXPENSES]).into(),
            net_profit: model::money(l[RECEIPTS] - l[FEES] - l[EXPENSES]).into(),
            ..Default::default()
        }
        .into(),
        ..Default::default()
    }
}

impl<'a> Expansion<'a> {
    fn prefix(&self) -> String {
        model::item_id(&["businesses", &self.m.id])
    }

    fn path(&self) -> String {
        format!("model.businesses[{}]", self.m.id)
    }

    fn evidence(&mut self) -> Result<(), Error> {
        let m = self.m;
        for p in &m.parts {
            match &p.part {
                Some(Part::FromLedger(fl)) => {
                    for en in &fl.entries {
                        if matches!(en.matches.to, Some(To::NotThisSource(_))) {
                            continue;
                        }
                        let d = match civil::from_field(&en.date) {
                            Ok(d) if !d.is_zero() => d,
                            _ => {
                                return Err(Error::Expansion(format!(
                                    "{}: entry {} has no date",
                                    self.path(),
                                    en.id
                                )));
                            }
                        };
                        let mut l = Lines::default();
                        let (mut kind, mut word) = (ItemKind::ITEM_KIND_RECEIPT, "receipt");
                        if en.kind == BusinessEntryKind::BUSINESS_ENTRY_KIND_FEE {
                            l[FEES] = model::value(&en.amount);
                            (kind, word) = (ItemKind::ITEM_KIND_FEE, "fee");
                        } else if en.kind == BusinessEntryKind::BUSINESS_ENTRY_KIND_EXPENSE {
                            l[EXPENSES] = model::value(&en.amount);
                            (kind, word) = (ItemKind::ITEM_KIND_EXPENSE, "expense");
                        } else {
                            l[RECEIPTS] = model::value(&en.amount);
                        }
                        let id = model::item_id(&[&self.prefix(), &d.to_string(), word, &en.id]);
                        self.one_off.insert(
                            id.clone(),
                            en.line == ScheduleCExpense::SCHEDULE_C_EXPENSE_DEPRECIATION,
                        );
                        self.items.push(Item {
                            id,
                            date: d,
                            status: ItemStatus::ITEM_STATUS_MADE,
                            kind,
                            l,
                            client: en.client.clone(),
                            origin: origin(Basis::BASIS_LEDGER, &p.id, &[&en.id]),
                        });
                    }
                }
                Some(Part::FromForms(ff)) => {
                    for f in &ff.forms {
                        if !model::usable(&f.source) {
                            continue;
                        }
                        match &f.form {
                            Some(FormKind::Form1099Nec(n)) => {
                                // Form 1099-NEC box 1 (nonemployee_compensation) reports gross self-employment
                                // receipts from the client for Schedule C Part I line 1 (26 U.S.C. §§ 61, 1402).
                                *self
                                    .stated
                                    .entry(form_client(&f.client, &n.payer.name))
                                    .or_default() += model::value(&n.nonemployee_compensation);
                            }
                            Some(FormKind::Form1099K(k)) => {
                                // Form 1099-K box 1a (gross_payment_transactions) reports gross payment card /
                                // third-party network transactions under 26 U.S.C. § 6050W for Schedule C line 1.
                                *self
                                    .stated
                                    .entry(form_client(&f.client, &k.filer.name))
                                    .or_default() += model::value(&k.gross_payment_transactions);
                            }
                            Some(FormKind::Form1099Misc(mi)) => {
                                // Form 1099-MISC attached to a trade or business (Schedule C Part I line 1):
                                // Sum the gross business-related payment boxes reported by the payer: box 1 (rents),
                                // box 2 (royalties), box 3 (other_income), box 5 (fishing_boat_proceeds), box 6
                                // (medical_and_health_care_payments), box 9 (crop_insurance_proceeds), box 10
                                // (gross_proceeds_paid_to_attorney), box 11 (fish_purchased_for_resale), and
                                // box 15 (nonqualified_deferred_compensation).
                                let receipts = model::value(&mi.other_income)
                                    + model::value(&mi.medical_and_health_care_payments)
                                    + model::value(&mi.crop_insurance_proceeds)
                                    + model::value(&mi.fishing_boat_proceeds)
                                    + model::value(&mi.fish_purchased_for_resale)
                                    + model::value(&mi.gross_proceeds_paid_to_attorney)
                                    + model::value(&mi.nonqualified_deferred_compensation)
                                    + model::value(&mi.rents)
                                    + model::value(&mi.royalties);
                                *self
                                    .stated
                                    .entry(form_client(&f.client, &mi.payer.name))
                                    .or_default() += receipts;
                            }
                            None => {}
                        }
                    }
                }
                Some(Part::Stopped(s)) => {
                    self.end = civil::from_field(&s.on).unwrap_or_default();
                }
                Some(Part::Stops(s)) => {
                    let d = civil::from_field(&s.on).unwrap_or_default();
                    if d.after(self.ctx.evidence_through()) {
                        self.end = d;
                    }
                }
                _ => {}
            }
        }
        Ok(())
    }

    /// Adds, for each client a form states, an ADJUSTMENT for the form less
    /// the bank receipts from that client, dated December 31.
    fn reconcile(&mut self) {
        let mut add = Vec::new();
        for (client, want) in &self.stated {
            let have: USCents = self
                .items
                .iter()
                .filter(|it| &it.client == client)
                .map(|it| it.l[RECEIPTS])
                .sum();
            if *want == have {
                continue;
            }
            let mut l = Lines::default();
            l[RECEIPTS] = *want - have;
            add.push(Item {
                id: model::item_id(&[&self.prefix(), "client", client, "adjustment"]),
                date: self.ctx.year_end(),
                status: ItemStatus::ITEM_STATUS_MADE,
                kind: ItemKind::ITEM_KIND_ADJUSTMENT,
                l,
                client: client.clone(),
                origin: origin(Basis::BASIS_DOCUMENT, "", &[]),
            });
        }
        self.items.extend(add);
    }

    /// Fills the months after the evidence. Receipts from a client a form
    /// states are left out of the pace, since the form states that client's
    /// year.
    fn rest(&mut self) {
        let (evidence, in_month) = self.evidence_by_month();
        let through = self.ctx.evidence_through();
        let months: Vec<Date> = self
            .ctx
            .months()
            .into_iter()
            .filter(|mo| {
                mo.after(through) && (self.end.is_zero() || !model::month_of(self.end).before(*mo))
            })
            .collect();
        let (pace, pace_months) = self.pace(&in_month);
        for (idx, mo) in months.iter().enumerate() {
            let key = mo.to_string();
            let (mut l, mut part) =
                self.month(*mo, pace, pace_months.get(&key).copied().unwrap_or(false));
            let (p, shares) = self.year_total(&evidence, months.len() as i64, idx);
            if !p.is_empty() {
                for (i, v) in shares.iter().enumerate() {
                    if let Some(v) = v {
                        l[i] = *v;
                    }
                }
                part = p;
            }
            let have = in_month.get(&key).copied().unwrap_or_default();
            for i in 0..l.len() {
                l[i] = (l[i] - have[i]).max(USCents(0));
            }
            if l == Lines::default() {
                continue;
            }
            self.items.push(Item {
                id: model::item_id(&[&self.prefix(), &key, "month"]),
                date: *mo,
                status: ItemStatus::ITEM_STATUS_EXPECTED,
                kind: ItemKind::ITEM_KIND_MONTH,
                l,
                client: String::new(),
                origin: origin(Basis::BASIS_ASSUMED, &part, &[]),
            });
        }
    }

    /// Whether an item's receipts are from a client a form states.
    fn stated_client(&self, client: &str) -> bool {
        self.stated.contains_key(client) && !client.is_empty()
    }

    /// Adds the made items, the whole year and each month's, leaving out
    /// receipts from a client a form states.
    fn evidence_by_month(&self) -> (Lines, HashMap<String, Lines>) {
        let mut evidence = Lines::default();
        let mut in_month: HashMap<String, Lines> = HashMap::new();
        for it in &self.items {
            if it.status != ItemStatus::ITEM_STATUS_MADE {
                continue;
            }
            let mut l = it.l;
            if self.stated_client(&it.client) {
                l[RECEIPTS] = USCents(0);
            }
            evidence = plus(evidence, &l);
            let k = model::month_of(it.date).to_string();
            let m = in_month.entry(k).or_default();
            *m = plus(*m, &l);
        }
        (evidence, in_month)
    }

    /// The default, the pace so far: the months with entries recur at the
    /// median gap between them, at the average month's figures, through
    /// the year's end. Expenses bought once, on Schedule C line 13, and
    /// receipts from a client a form states, are left out of the average.
    /// One month so far recurs a year on. It returns the average and the
    /// months it falls on.
    fn pace(&self, in_month: &HashMap<String, Lines>) -> (Lines, HashMap<String, bool>) {
        let mut recurring: HashMap<String, Lines> = HashMap::new();
        for it in &self.items {
            if it.status != ItemStatus::ITEM_STATUS_MADE {
                continue;
            }
            let mut l = it.l;
            if self.stated_client(&it.client) {
                l[RECEIPTS] = USCents(0);
            }
            if self.one_off.get(&it.id).copied().unwrap_or(false) {
                l[EXPENSES] = USCents(0);
            }
            let k = model::month_of(it.date).to_string();
            let m = recurring.entry(k).or_default();
            *m = plus(*m, &l);
        }
        let mut months = Vec::new();
        let mut total = Lines::default();
        let limit = self.ctx.evidence_through().add_days(31);
        for mo in self.ctx.months() {
            let k = mo.to_string();
            if in_month.contains_key(&k) && !mo.after(limit) {
                months.push(mo);
                total = plus(total, &recurring.get(&k).copied().unwrap_or_default());
            }
        }
        let mut on = HashMap::new();
        if months.is_empty() || !self.m.default_part.is_set() {
            return (Lines::default(), on);
        }
        let mut avg = Lines::default();
        for i in 0..avg.len() {
            avg[i] = total[i] / months.len() as i64;
        }
        for d in model::cadence_of(&months).through(months[months.len() - 1], self.ctx.year_end()) {
            on.insert(d.to_string(), true);
        }
        // The month of as_of is the next source's less what is in it.
        let as_of = model::month_of(self.ctx.as_of()).to_string();
        if in_month.get(&as_of).copied().unwrap_or_default() != Lines::default() {
            on.insert(as_of, true);
        }
        (avg, on)
    }

    /// A month's figures by a Monthly part from its start, else the pace so
    /// far on the months its cadence falls on, and the part.
    fn month(&self, mo: Date, pace: Lines, paced: bool) -> (Lines, String) {
        let mut l = Lines::default();
        let mut part = self.m.default_part.id.clone();
        if paced {
            l = pace;
        }
        for p in &self.m.parts {
            if let Some(Part::Monthly(mon)) = &p.part {
                let from = civil::from_field(&mon.from).unwrap_or_default();
                if !from.after(mo) {
                    l = [
                        model::value(&mon.receipts),
                        USCents(0),
                        model::value(&mon.expenses),
                    ];
                    part = p.id.clone();
                }
            }
        }
        (l, part)
    }

    /// A YearTotal's part and, for each line it states, the month idx's
    /// share of the year less the evidence.
    fn year_total(&self, evidence: &Lines, n: i64, idx: usize) -> (String, [Option<USCents>; 3]) {
        let mut out = [None; 3];
        for p in &self.m.parts {
            let Some(Part::YearTotal(yt)) = &p.part else {
                continue;
            };
            for (i, stated) in [&yt.receipts, &yt.fees, &yt.expenses]
                .into_iter()
                .enumerate()
            {
                if stated.is_set() {
                    let v =
                        model::share((model::value(stated) - evidence[i]).max(USCents(0)), n)[idx];
                    out[i] = Some(v);
                }
            }
            return (p.id.clone(), out);
        }
        (String::new(), out)
    }

    fn projection(&mut self) -> BusinessProjection {
        let mut out = BusinessProjection {
            id: self.m.id.clone(),
            so_far_through: self.ctx.evidence_through().field(),
            ..Default::default()
        };
        self.round_items();
        let (mut made, mut rest) = (Lines::default(), Lines::default());
        for it in &self.items {
            out.items.push(scenariosv1::Item {
                id: it.id.clone(),
                date: it.date.field(),
                status: it.status.into(),
                kind: it.kind.into(),
                origin: it.origin.clone().into(),
                figures: Some(lines_proto(&it.l, &it.client).into()),
                ..Default::default()
            });
            if it.status == ItemStatus::ITEM_STATUS_MADE {
                made = plus(made, &it.l);
            } else {
                rest = plus(rest, &it.l);
            }
        }
        model::sort_items(&mut out.items);
        out.so_far = totals(&made).into();
        out.rest = totals(&rest).into();
        out.year = totals(&plus(made, &rest)).into();
        for c in &self.m.clients {
            if (c.form == ClientForm::CLIENT_FORM_1099_NEC
                || c.form == ClientForm::CLIENT_FORM_1099_K)
                && !self.stated.contains_key(&c.name)
            {
                let form = if c.form == ClientForm::CLIENT_FORM_1099_K {
                    "Form1099K"
                } else {
                    "Form1099Nec"
                };
                out.expected.push(ExpectedDocument {
                    form: form.to_string(),
                    due: model::forms_due(self.ctx.year()).field(),
                    ..Default::default()
                });
            }
        }
        out
    }

    /// Rounds each item's lines to the dollar with a running total per
    /// line, the made items first (see [`model::Rounder`]).
    fn round_items(&mut self) {
        let mut runs = model::Runs::default();
        for it in self.items.iter_mut() {
            let r = runs.run(it.status == ItemStatus::ITEM_STATUS_MADE, &it.origin.part);
            for k in 0..it.l.len() {
                it.l[k] = r.round(&k.to_string(), it.l[k]);
            }
        }
    }
}
