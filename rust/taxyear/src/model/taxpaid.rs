//! Expands a TaxPaidModel, one jurisdiction's tax payments, into dated
//! payment items. Its slots are the jurisdiction's estimated payment due
//! dates, which the estimated-payment rules give. Ported from the Go package
//! `taxyear/model/taxpaid`.
//!
//! [`expand`] reads the known payments and the planned ones; the default
//! part, SpreadTheRest, needs the rules' target, so [`Payments::spread`]
//! fills it once taxyear/paid has asked the rules. See docs/models.md.

use std::collections::HashSet;

use money::USCents;
use proto::factsv1::{
    self, __buffa::oneof::payment_purpose::Purpose, AdjacentPayment, PaymentPurpose,
};
use proto::scenariosv1::{
    self, __buffa::oneof::item::Figures, __buffa::oneof::tax_paid_default::Rest,
    __buffa::oneof::tax_paid_part::Part, ItemKind, ItemStatus, TaxPaidModel, TaxPaidPart,
    TaxPaidProjection, TaxPaidTotals, TaxPaymentFigures, origin::Basis, tax_paid_part,
};

use super::account::origin;
use super::{Context, Error, Notes};
use crate::internal::civil::{self, Date};
use crate::model;

/// One payment the rules read: its date, amount, whether it was last year's
/// overpayment, and whether it is made by as_of.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct Payment {
    on: Date,
    amount: USCents,
    from_last_year: bool,
    made: bool,
}

impl Payment {
    /// The payment's date.
    pub fn on(&self) -> Date {
        self.on
    }

    /// The payment's amount.
    pub fn amount(&self) -> USCents {
        self.amount
    }

    /// Reports whether it is last year's overpayment applied.
    pub fn from_last_year(&self) -> bool {
        self.from_last_year
    }

    /// Reports whether it is made by as_of, or is last year's overpayment.
    pub fn made(&self) -> bool {
        self.made
    }
}

/// One jurisdiction's payments being expanded.
#[derive(Clone, Debug)]
pub struct Payments {
    ctx: Context,
    m: TaxPaidModel,
    items: Vec<scenariosv1::Item>,
    notes: Notes,
    /// The due dates a PlannedPayment fills.
    planned: HashSet<String>,
    spread: bool,
    /// The rules' due dates, once open has been given them.
    dues: Vec<Date>,
}

fn estimated_for(p: &PaymentPurpose) -> i32 {
    match p.purpose {
        Some(Purpose::EstimatedFor(y)) => y,
        _ => 0,
    }
}

fn balance_for(p: &PaymentPurpose) -> i32 {
    match p.purpose {
        Some(Purpose::BalanceFor(y)) => y,
        _ => 0,
    }
}

fn payment_of(it: &scenariosv1::Item) -> &TaxPaymentFigures {
    match &it.figures {
        Some(Figures::Payment(f)) => f,
        _ => <TaxPaymentFigures as buffa::DefaultInstance>::default_instance(),
    }
}

/// Reads a tax paid model's payments made, last year's overpayment and
/// planned payments. A payment counts toward the year its purpose says; one
/// with no purpose counts toward neither, and the result asks.
pub fn expand(
    ctx: &Context,
    m: &TaxPaidModel,
    adjacent: &[AdjacentPayment],
) -> Result<Payments, Error> {
    let mut p = Payments {
        ctx: ctx.clone(),
        m: m.clone(),
        items: Vec::new(),
        notes: Notes::default(),
        planned: HashSet::new(),
        spread: false,
        dues: Vec::new(),
    };
    for part in &m.parts {
        match &part.part {
            Some(Part::PaymentMade(pm)) => p.made(part, pm)?,
            Some(Part::OverpaymentApplied(o)) => p.overpayment(part, o),
            Some(Part::PlannedPayment(pp)) => p.planned_payment(part, pp)?,
            None => {}
        }
    }
    for a in adjacent {
        if a.jurisdiction != m.jurisdiction || !for_year(a, ctx.year()) {
            continue;
        }
        let d = match civil::from_field(&a.date) {
            Ok(d) if !d.is_zero() => d,
            _ => {
                return Err(Error::Expansion(format!(
                    "facts.adjacent_payments[{}].date is not a day",
                    a.id
                )));
            }
        };
        p.items.push(scenariosv1::Item {
            id: model::item_id(&[&p.prefix(), &d.to_string(), "payment", &a.id]),
            date: d.field(),
            status: ItemStatus::ITEM_STATUS_MADE.into(),
            kind: ItemKind::ITEM_KIND_PAYMENT.into(),
            origin: origin(Basis::BASIS_LEDGER, "", &[&a.id]).into(),
            figures: Some(
                TaxPaymentFigures {
                    amount: a.amount.as_option().cloned().into(),
                    purpose: a.purpose.as_option().cloned().into(),
                    counts: i64::from(estimated_for(&a.purpose)) == ctx.year(),
                    ..Default::default()
                }
                .into(),
            ),
            ..Default::default()
        });
    }
    p.spread = matches!(m.default_part.rest, Some(Rest::SpreadTheRest(_)));
    Ok(p)
}

/// Reports whether an adjacent year's payment is for the year: an estimated
/// payment toward it, or its balance paid with the return. Only an
/// estimated payment counts toward the year's installments.
pub fn for_year(a: &factsv1::AdjacentPayment, year: i64) -> bool {
    let p = &a.purpose;
    i64::from(estimated_for(p)) == year || i64::from(balance_for(p)) == year
}

fn slot_item(prefix: &str, due: Date, part: &str, amount: USCents) -> scenariosv1::Item {
    scenariosv1::Item {
        id: model::item_id(&[prefix, &due.to_string(), "payment"]),
        date: due.field(),
        status: ItemStatus::ITEM_STATUS_EXPECTED.into(),
        kind: ItemKind::ITEM_KIND_PAYMENT.into(),
        origin: origin(Basis::BASIS_ASSUMED, part, &[]).into(),
        figures: Some(
            TaxPaymentFigures {
                amount: model::money(amount).into(),
                counts: true,
                ..Default::default()
            }
            .into(),
        ),
        ..Default::default()
    }
}

fn totals(items: &[scenariosv1::Item]) -> TaxPaidTotals {
    let (mut all, mut counted, mut over) = (USCents(0), USCents(0), USCents(0));
    for it in items {
        let f = payment_of(it);
        let v = model::value(&f.amount);
        all += v;
        if f.counts {
            counted += v;
        }
        if f.from_last_year {
            over += v;
        }
    }
    TaxPaidTotals {
        items: model::money(all).into(),
        counted: model::money(counted).into(),
        overpayment_applied: model::money(over).into(),
        ..Default::default()
    }
}

impl Payments {
    /// US or a state's postal code.
    pub fn jurisdiction(&self) -> &str {
        &self.m.jurisdiction
    }

    /// Returns what the expansion asked.
    pub fn notes(&self) -> &Notes {
        &self.notes
    }

    /// When payments could first be expected, or zero for January 1.
    pub fn payments_from(&self) -> Date {
        civil::from_field(&self.m.payments_from).unwrap_or_default()
    }

    fn prefix(&self) -> String {
        model::item_id(&["tax_paid", &self.m.id])
    }

    fn path(&self) -> String {
        format!("model.tax_paid[{}]", self.m.id)
    }

    /// Reads one PaymentMade: its date and amount as typed, else from the
    /// confirmation, else the transfer. A transfer and a confirmation are
    /// one item.
    fn made(&mut self, part: &TaxPaidPart, pm: &tax_paid_part::PaymentMade) -> Result<(), Error> {
        let mut date = pm.date.as_option();
        if date.is_none() {
            date = pm.confirmation.confirmation.paid_on.as_option();
        }
        if date.is_none() {
            date = pm.transfer.date.as_option();
        }
        let d = match civil::from_proto(date) {
            Ok(d) if !d.is_zero() => d,
            _ => {
                return Err(Error::Expansion(format!(
                    "{}.parts[{}].payment_made has no date",
                    self.path(),
                    part.id
                )));
            }
        };
        let mut amount = pm.amount.as_option();
        if amount.is_none() {
            amount = pm.confirmation.confirmation.amount.as_option();
        }
        if amount.is_none() {
            amount = pm.transfer.amount.as_option();
        }
        let mut o = origin(Basis::BASIS_ASSUMED, &part.id, &[]);
        if pm.transfer.is_set() {
            o.basis = Basis::BASIS_LEDGER.into();
            o.evidence.push(pm.transfer.id.clone());
        }
        if pm.confirmation.is_set() {
            o.basis = Basis::BASIS_DOCUMENT.into();
            o.evidence.push(pm.confirmation.id.clone());
        }
        let prefix = self.prefix();
        let id = model::item_id(&[&prefix, &d.to_string(), "payment", &part.id]);
        let year = self.ctx.year();
        let counts = i64::from(estimated_for(&pm.purpose)) == year;
        if !pm.purpose.is_set() {
            self.notes.ask(model::new_question(
                model::item_id(&[&prefix, "purpose", &part.id]),
                id.clone(),
                format!(
                    "What was your {} payment to {} on {} for?",
                    model::value(&amount),
                    self.m.jurisdiction,
                    d.short()
                ),
                "It counts toward neither year until you say.",
                format!("{}.parts[{}].payment_made.purpose", self.path(), part.id),
                &[
                    &format!("Estimated tax for {year}"),
                    &format!("Tax for {}", year - 1),
                    "Not tax",
                ],
            ));
        }
        self.items.push(scenariosv1::Item {
            id,
            date: d.field(),
            status: ItemStatus::ITEM_STATUS_MADE.into(),
            kind: ItemKind::ITEM_KIND_PAYMENT.into(),
            origin: o.into(),
            figures: Some(
                TaxPaymentFigures {
                    amount: model::money(model::value(&amount)).into(),
                    purpose: pm.purpose.clone(),
                    counts,
                    ..Default::default()
                }
                .into(),
            ),
            ..Default::default()
        });
        Ok(())
    }

    fn overpayment(&mut self, part: &TaxPaidPart, o: &tax_paid_part::OverpaymentApplied) {
        let d = match civil::from_field(&o.credited) {
            Ok(d) if !d.is_zero() => d,
            _ => self.ctx.year_start(),
        };
        self.items.push(scenariosv1::Item {
            id: model::item_id(&[&self.prefix(), &d.to_string(), "payment", &part.id]),
            date: d.field(),
            status: ItemStatus::ITEM_STATUS_MADE.into(),
            kind: ItemKind::ITEM_KIND_PAYMENT.into(),
            origin: origin(Basis::BASIS_DOCUMENT, &part.id, &[&o.prior_year]).into(),
            figures: Some(
                TaxPaymentFigures {
                    amount: model::money(model::value(&o.amount)).into(),
                    counts: true,
                    from_last_year: true,
                    ..Default::default()
                }
                .into(),
            ),
            ..Default::default()
        });
    }

    fn planned_payment(
        &mut self,
        part: &TaxPaidPart,
        pp: &tax_paid_part::PlannedPayment,
    ) -> Result<(), Error> {
        let d = match civil::from_field(&pp.due) {
            Ok(d) if !d.is_zero() => d,
            _ => {
                return Err(Error::Expansion(format!(
                    "{}.parts[{}].planned_payment.due is not a day",
                    self.path(),
                    part.id
                )));
            }
        };
        self.planned.insert(d.to_string());
        let item = slot_item(&self.prefix(), d, &part.id, model::value(&pp.amount));
        self.items.push(item);
        Ok(())
    }

    /// Sorts and rounds the items, as Go does to a copy of its slice of
    /// pointers: the order of the items is the sorted one, which a stable
    /// sort of the original order gives again, and the rounding changes the
    /// items themselves.
    fn sorted_rounded(&mut self) {
        model::sort_items(&mut self.items);
        model::round_items(&mut self.items);
    }

    /// Returns every payment that counts toward this year, in date order.
    pub fn payments(&mut self) -> Vec<Payment> {
        self.sorted_rounded();
        let mut out = Vec::new();
        for it in &self.items {
            let f = payment_of(it);
            if !f.counts {
                continue;
            }
            let d = civil::from_field(&it.date).unwrap_or_default();
            out.push(Payment {
                on: d,
                amount: model::value(&f.amount),
                from_last_year: f.from_last_year,
                made: f.from_last_year || it.status == ItemStatus::ITEM_STATUS_MADE,
            });
        }
        out
    }

    /// Returns the due dates after as_of the default part may fill: on or
    /// after payments_from, and not named by a PlannedPayment. It returns
    /// none when the default part is not SpreadTheRest.
    pub fn open(&mut self, dues: &[Date]) -> Vec<Date> {
        self.dues = dues.to_vec();
        if !self.spread {
            return Vec::new();
        }
        let from = self.payments_from();
        dues.iter()
            .copied()
            .filter(|d| {
                d.after(self.ctx.as_of())
                    && !self.planned.contains(&d.to_string())
                    && (from.is_zero() || !d.before(from))
            })
            .collect()
    }

    /// Fills the open due dates with equal payments adding to total, the
    /// rest of the target, as SpreadTheRest says.
    pub fn spread(&mut self, open: &[Date], total: USCents) {
        if total.0 <= 0 || open.is_empty() {
            return;
        }
        let prefix = self.prefix();
        for (i, v) in model::share(total, open.len() as i64)
            .into_iter()
            .enumerate()
        {
            let item = slot_item(&prefix, open[i], &self.m.default_part.id, v);
            self.items.push(item);
        }
    }

    /// Returns the jurisdiction's projection.
    pub fn projection(&mut self) -> TaxPaidProjection {
        self.sorted_rounded();
        let items = self.items.clone();
        let (made, expected): (Vec<scenariosv1::Item>, Vec<scenariosv1::Item>) = items
            .iter()
            .cloned()
            .partition(|it| it.status == ItemStatus::ITEM_STATUS_MADE);
        TaxPaidProjection {
            id: self.m.id.clone(),
            jurisdiction: self.m.jurisdiction.clone(),
            so_far_through: self.ctx.as_of().field(),
            so_far: totals(&made).into(),
            rest: totals(&expected).into(),
            year: totals(&items).into(),
            can_add: vec![TaxPaidPart {
                part: Some(Part::PlannedPayment(Box::new(
                    tax_paid_part::PlannedPayment {
                        due: self.next_due().field(),
                        amount: model::money(USCents(0)).into(),
                        ..Default::default()
                    },
                ))),
                ..Default::default()
            }],
            items,
            ..Default::default()
        }
    }

    /// The day a new planned payment defaults to: the first due date after
    /// as_of that no planned payment names, or the day after as_of while the
    /// rules' due dates aren't known.
    fn next_due(&self) -> Date {
        for d in &self.dues {
            if d.after(self.ctx.as_of()) && !self.planned.contains(&d.to_string()) {
                return *d;
            }
        }
        self.ctx.as_of().add_days(1)
    }
}
