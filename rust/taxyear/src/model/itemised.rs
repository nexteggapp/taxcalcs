//! Expands an ItemisedModel, one source of an itemised deduction, into
//! items: a mortgage's monthly interest, a property's tax installments,
//! gifts to charity and medical payments. Each reaches one Schedule A input.
//! Ported from the Go package `taxyear/model/itemised`. See docs/models.md.

use std::collections::HashSet;

use money::USCents;
use proto::commonv1;
use proto::scenariosv1::{
    self, __buffa::oneof::charity_part::Part as CharityKind, __buffa::oneof::itemised_model::Kind,
    __buffa::oneof::r#match::To, __buffa::oneof::medical_part::Part as MedicalKind,
    __buffa::oneof::mortgage_part::Part as MortgageKind,
    __buffa::oneof::property_tax_part::Part as PropertyTaxKind, CharityModel, ExpectedDocument,
    ItemKind, ItemStatus, ItemisedFigures, ItemisedModel, ItemisedProjection, ItemisedTotals,
    MedicalModel, MortgageModel, MortgagePart, Origin, PropertyTaxModel, RestOfYear,
    ScheduleAInputs, origin::Basis,
};

use super::account::origin;
use super::{Context, Error, Notes};
use crate::internal::civil::{self, Date};
use crate::model;

/// One itemised item while it is made.
#[derive(Clone, Debug)]
struct Item {
    id: String,
    date: Date,
    status: ItemStatus,
    kind: ItemKind,
    origin: Origin,
    amount: USCents,
    payee: String,
    /// The fields a rule filled, by path in the item.
    est: Vec<String>,
}

struct Expansion<'a> {
    ctx: &'a Context,
    m: &'a ItemisedModel,
    notes: Notes,
    items: Vec<Item>,
    /// A whole-year document's figure, such as a 1098's box 1, and
    /// whole_part the part that holds it.
    whole: USCents,
    has_whole: bool,
    whole_part: String,
    through: Date,
}

const MADE: ItemStatus = ItemStatus::ITEM_STATUS_MADE;
const EXPECTED: ItemStatus = ItemStatus::ITEM_STATUS_EXPECTED;
const DOCUMENT: Basis = Basis::BASIS_DOCUMENT;
const LEDGER: Basis = Basis::BASIS_LEDGER;
const ASSUMED: Basis = Basis::BASIS_ASSUMED;

/// Expands one itemised model.
pub fn expand(ctx: &Context, m: &ItemisedModel) -> Result<(ItemisedProjection, Notes), Error> {
    let mut e = Expansion {
        ctx,
        m,
        notes: Notes::default(),
        items: Vec::new(),
        whole: USCents(0),
        has_whole: false,
        whole_part: String::new(),
        through: Date::default(),
    };
    match &m.kind {
        Some(Kind::Mortgage(x)) => e.mortgage(x)?,
        Some(Kind::PropertyTax(x)) => e.property_tax(x)?,
        Some(Kind::Charity(x)) => e.charity(x),
        Some(Kind::Medical(x)) => e.medical(x),
        None => {}
    }
    let p = e.projection();
    Ok((p, e.notes))
}

/// A mortgage's loan as the evidence leaves it.
#[derive(Clone, Debug)]
struct LoanState<'a> {
    balance: USCents,
    payment: USCents,
    rate: Option<&'a commonv1::Quantity>,
    latest: Date,
    end: Date,
}

/// One property tax due date and its amount.
#[derive(Clone, Debug)]
struct Installment {
    due: Date,
    amount: USCents,
    paid: bool,
}

fn not_this_source(m: &scenariosv1::Match) -> bool {
    matches!(m.to, Some(To::NotThisSource(_)))
}

impl<'a> Expansion<'a> {
    fn prefix(&self) -> String {
        model::item_id(&["itemised", &self.m.id])
    }

    fn path(&self) -> String {
        format!("model.itemised[{}]", self.m.id)
    }

    #[allow(clippy::too_many_arguments)]
    fn add(
        &mut self,
        id: String,
        d: Date,
        status: ItemStatus,
        kind: ItemKind,
        basis: Basis,
        part: &str,
        evidence: &[&str],
        amount: USCents,
        payee: &str,
    ) {
        self.items.push(Item {
            id,
            date: d,
            status,
            kind,
            origin: origin(basis, part, evidence),
            amount,
            payee: payee.to_string(),
            est: Vec::new(),
        });
        if status == MADE {
            self.through = civil::later(self.through, d);
        }
    }

    /// A 1098 states the year's interest; statements are a month's interest
    /// each; the months after are a Refinanced loan's from its start, else a
    /// YearTotal's equal shares, else the default, amortised from the
    /// latest statement's balance and rate, or the loan's.
    fn mortgage(&mut self, m: &'a MortgageModel) -> Result<(), Error> {
        let st = self.mortgage_evidence(m)?;
        if self.has_whole {
            return Ok(());
        }
        let through = self.ctx.evidence_through();
        let months: Vec<Date> = self
            .ctx
            .months()
            .into_iter()
            .filter(|mo| {
                mo.after(model::month_of(st.latest))
                    && (st.end.is_zero() || !mo.after(model::month_of(st.end)))
                    && (!st.latest.is_zero() || mo.after(through))
            })
            .collect();
        let rests: Vec<(&str, &RestOfYear)> = m
            .parts
            .iter()
            .filter_map(|p| match &p.part {
                Some(MortgageKind::RestOfYear(r)) => Some((p.id.as_str(), &**r)),
                _ => None,
            })
            .collect();
        if self.rest_of_year(&rests, ItemKind::ITEM_KIND_INTEREST, "interest") {
            return Ok(());
        }
        let (shares, share_part) = self.mortgage_year_total(m, months.len());
        if st.balance.0 == 0
            && shares.is_none()
            && !months.is_empty()
            && self.repeat_interest(m, &months)
        {
            return Ok(());
        }
        self.amortise(m, st, &months, shares, &share_part);
        Ok(())
    }

    /// Reads a mortgage's 1098s, statements and end.
    fn mortgage_evidence(&mut self, m: &'a MortgageModel) -> Result<LoanState<'a>, Error> {
        let loan = &m.loan;
        let mut st = LoanState {
            balance: model::value(&loan.balance),
            payment: model::value(&loan.payment),
            rate: loan.rate.as_option(),
            latest: Date::default(),
            end: Date::default(),
        };
        for p in &m.parts {
            match &p.part {
                Some(MortgageKind::FromForm1098(f)) => {
                    if model::usable(&f.source) {
                        self.whole += model::value(&f.form.mortgage_interest_received);
                        self.has_whole = true;
                        self.whole_part = p.id.clone();
                    }
                }
                Some(MortgageKind::FromStatements(_)) => self.statements(p, &mut st)?,
                Some(MortgageKind::Repaid(r)) => {
                    st.end = civil::from_field(&r.on).unwrap_or_default();
                }
                Some(MortgageKind::PaidOff(o)) => {
                    let d = civil::from_field(&o.on).unwrap_or_default();
                    if d.after(st.latest) {
                        st.end = d;
                    }
                }
                _ => {}
            }
        }
        for p in &m.parts {
            if let Some(MortgageKind::PaidFromBank(_)) = &p.part {
                self.bank_months(p, &mut st);
            }
        }
        Ok(st)
    }

    /// Adds, for each bank payment in a month no statement covers, that
    /// month's interest: the latest statement's, else a month of the loan's
    /// rate on its balance. It is estimated, and with neither the result
    /// asks. The payments are evidence, so the latest evidence date moves.
    fn bank_months(&mut self, p: &MortgagePart, st: &mut LoanState<'a>) {
        let Some(MortgageKind::PaidFromBank(bank)) = &p.part else {
            return;
        };
        let mut covered: HashSet<String> = self
            .items
            .iter()
            .map(|it| model::month_of(it.date).to_string())
            .collect();
        let (mut interest, mut ok) = self.latest_interest();
        if !ok {
            let (pct, _) = model::percent(st.rate);
            interest = model::of_fine(pct, st.balance) / 12;
            ok = st.balance.0 > 0;
        }
        for pay in &bank.payments {
            let d = match civil::from_field(&pay.date) {
                Ok(d) if !d.is_zero() => d,
                _ => continue,
            };
            if not_this_source(&pay.matches) || covered.contains(&model::month_of(d).to_string()) {
                continue;
            }
            covered.insert(model::month_of(d).to_string());
            let prefix = self.prefix();
            if !ok {
                self.notes.ask(model::new_question(
                    model::item_id(&[&prefix, "loan"]),
                    prefix.clone(),
                    format!(
                        "What are the balance, rate and monthly payment of your loan with {}?",
                        self.m.name
                    ),
                    "A month paid from the bank with no statement counts no interest until you say.",
                    format!("{}.mortgage.loan", self.path()),
                    &[],
                ));
            }
            self.add(
                model::item_id(&[&prefix, &d.to_string(), "interest", &pay.id]),
                d,
                MADE,
                ItemKind::ITEM_KIND_INTEREST,
                LEDGER,
                &p.id,
                &[&pay.id],
                interest,
                "",
            );
            if let Some(last) = self.items.last_mut() {
                last.est = vec!["itemised.amount".to_string()];
            }
            st.latest = civil::later(st.latest, d);
        }
    }

    /// Adds a RestOfYear part's amount still to come, on its date or
    /// December 31, and reports whether there was one: it replaces what the
    /// default would fill after the evidence.
    fn rest_of_year(&mut self, parts: &[(&str, &RestOfYear)], kind: ItemKind, word: &str) -> bool {
        let mut found = false;
        for (id, r) in parts {
            let mut d = civil::from_field(&r.date).unwrap_or_default();
            if d.is_zero() {
                d = self.ctx.year_end();
            }
            self.add(
                model::item_id(&[&self.prefix(), &d.to_string(), word, id]),
                d,
                EXPECTED,
                kind,
                ASSUMED,
                id,
                &[],
                model::value(&r.amount),
                "",
            );
            found = true;
        }
        found
    }

    /// Adds each statement's month of interest, and keeps the latest one's
    /// balance and rate.
    fn statements(&mut self, p: &'a MortgagePart, st: &mut LoanState<'a>) -> Result<(), Error> {
        let Some(MortgageKind::FromStatements(fs)) = &p.part else {
            return Ok(());
        };
        for s in &fs.statements {
            if !model::usable(&s.source) || not_this_source(&s.matches) {
                continue;
            }
            let d = match civil::from_field(&s.statement.date) {
                Ok(d) if !d.is_zero() => d,
                _ => {
                    return Err(Error::Expansion(format!(
                        "{}: statement {} has no date",
                        self.path(),
                        s.id
                    )));
                }
            };
            self.add(
                model::item_id(&[&self.prefix(), &d.to_string(), "interest", &s.id]),
                d,
                MADE,
                ItemKind::ITEM_KIND_INTEREST,
                DOCUMENT,
                &p.id,
                &[&s.id],
                model::value(&s.statement.interest),
                "",
            );
            if d.after(st.latest) {
                st.latest = d;
                st.balance = model::value(&s.statement.balance);
                if s.statement.rate.is_set() {
                    st.rate = s.statement.rate.as_option();
                }
            }
        }
        Ok(())
    }

    /// Fills the months with the latest statement's interest and asks for
    /// the loan, when neither the statements nor the model give a balance to
    /// amortise. It reports whether it did.
    fn repeat_interest(&mut self, m: &MortgageModel, months: &[Date]) -> bool {
        let (repeat, ok) = self.latest_interest();
        if !ok {
            return false;
        }
        let prefix = self.prefix();
        self.notes.ask(model::new_question(
            model::item_id(&[&prefix, "loan"]),
            prefix.clone(),
            format!(
                "What are the balance, rate and monthly payment of your loan with {}?",
                self.m.name
            ),
            format!(
                "Each month still to come counts the latest statement's interest, {repeat}, until you say."
            ),
            format!("{}.mortgage.loan", self.path()),
            &[],
        ));
        for mo in months {
            self.add(
                model::item_id(&[&prefix, &mo.to_string(), "interest"]),
                *mo,
                EXPECTED,
                ItemKind::ITEM_KIND_INTEREST,
                ASSUMED,
                &m.default_part.id,
                &[],
                repeat,
                "",
            );
        }
        true
    }

    /// Fills the months: a month's interest is the balance at the rate, and
    /// the payment less it comes off the balance. A Refinanced part takes
    /// over from its start; a YearTotal's shares replace the interest.
    fn amortise(
        &mut self,
        m: &'a MortgageModel,
        mut st: LoanState<'a>,
        months: &[Date],
        shares: Option<Vec<USCents>>,
        share_part: &str,
    ) {
        let mut part = m.default_part.id.clone();
        let mut refinanced = HashSet::new();
        for (i, mo) in months.iter().enumerate() {
            for p in &m.parts {
                if let Some(MortgageKind::Refinanced(rf)) = &p.part {
                    if refinanced.contains(&p.id) {
                        continue;
                    }
                    let from = civil::from_field(&rf.from).unwrap_or_default();
                    if !from.after(*mo) {
                        refinanced.insert(p.id.clone());
                        part = p.id.clone();
                        st.rate = rf.loan.rate.as_option();
                        st.balance = model::value(&rf.loan.balance);
                        st.payment = model::value(&rf.loan.payment);
                    }
                }
            }
            let (pct, _) = model::percent(st.rate);
            let mut interest = model::of_fine(pct, st.balance) / 12;
            if st.payment > interest {
                st.balance -= st.payment - interest;
            }
            let mut filled_by = part.as_str();
            if let Some(shares) = &shares {
                interest = shares[i];
                filled_by = share_part;
            }
            let filled_by = filled_by.to_string();
            self.add(
                model::item_id(&[&self.prefix(), &mo.to_string(), "interest"]),
                *mo,
                EXPECTED,
                ItemKind::ITEM_KIND_INTEREST,
                ASSUMED,
                &filled_by,
                &[],
                interest,
                "",
            );
        }
    }

    /// A YearTotal's equal shares of the year less the evidence, or nil
    /// without one.
    fn mortgage_year_total(&self, m: &MortgageModel, n: usize) -> (Option<Vec<USCents>>, String) {
        for p in &m.parts {
            if let Some(MortgageKind::YearTotal(yt)) = &p.part
                && n > 0
            {
                let left = (model::value(&yt.interest) - self.made_total()).max(USCents(0));
                return (Some(model::share(left, n as i64)), p.id.clone());
            }
        }
        (None, String::new())
    }

    /// The latest statement's interest.
    fn latest_interest(&self) -> (USCents, bool) {
        let mut at = Date::default();
        let mut v = USCents(0);
        let mut found = false;
        for it in &self.items {
            if it.status == MADE && it.kind == ItemKind::ITEM_KIND_INTEREST && !it.date.before(at) {
                (at, v, found) = (it.date, it.amount, true);
            }
        }
        (v, found)
    }

    fn made_total(&self) -> USCents {
        self.items
            .iter()
            .filter(|it| it.status == MADE)
            .map(|it| it.amount)
            .sum()
    }

    /// The slots are this year's bill's due dates, else last year's moved to
    /// this year, else the typed ones. A bank payment fills the nearest
    /// installment whose due date it precedes, within 120 days. The unpaid
    /// installments after the latest payment are a PlannedInstallment's, a
    /// YearTotal's share, or the bill's amount.
    fn property_tax(&mut self, m: &PropertyTaxModel) -> Result<(), Error> {
        let mut slots = self.schedule(m);
        self.bank_payments(m, &mut slots)?;
        let rests: Vec<(&str, &RestOfYear)> = m
            .parts
            .iter()
            .filter_map(|p| match &p.part {
                Some(PropertyTaxKind::RestOfYear(r)) => Some((p.id.as_str(), &**r)),
                _ => None,
            })
            .collect();
        if self.rest_of_year(&rests, ItemKind::ITEM_KIND_INSTALLMENT, "installment") {
            return Ok(());
        }
        let open: Vec<&Installment> = slots
            .iter()
            .filter(|s| !s.paid && s.due.after(self.through) && s.due.year() == self.ctx.year())
            .collect();
        let mut shares: Option<Vec<USCents>> = None;
        let mut share_part = String::new();
        for p in &m.parts {
            if let Some(PropertyTaxKind::YearTotal(yt)) = &p.part
                && !open.is_empty()
            {
                let left = (model::value(&yt.total) - self.made_total()).max(USCents(0));
                shares = Some(model::share(left, open.len() as i64));
                share_part = p.id.clone();
            }
        }
        let mut adds = Vec::new();
        for (i, s) in open.iter().enumerate() {
            let (mut amount, on, mut part) = planned_installment(m, s);
            if let Some(shares) = &shares {
                amount = shares[i];
                part = share_part.clone();
            }
            adds.push((
                model::item_id(&[&self.prefix(), &s.due.to_string(), "installment"]),
                on,
                part,
                amount,
            ));
        }
        for (id, on, part, amount) in adds {
            self.add(
                id,
                on,
                EXPECTED,
                ItemKind::ITEM_KIND_INSTALLMENT,
                ASSUMED,
                &part,
                &[],
                amount,
                "",
            );
        }
        Ok(())
    }

    /// Adds each bank payment, filling the installment it pays.
    fn bank_payments(
        &mut self,
        m: &PropertyTaxModel,
        slots: &mut [Installment],
    ) -> Result<(), Error> {
        for p in &m.parts {
            let Some(PropertyTaxKind::PaidFromBank(bank)) = &p.part else {
                continue;
            };
            for pay in &bank.payments {
                if not_this_source(&pay.matches) {
                    continue;
                }
                let d = match civil::from_field(&pay.date) {
                    Ok(d) if !d.is_zero() => d,
                    _ => {
                        return Err(Error::Expansion(format!(
                            "{}: payment {} has no date",
                            self.path(),
                            pay.id
                        )));
                    }
                };
                let prefix = self.prefix();
                let mut id = model::item_id(&[&prefix, &d.to_string(), "installment", &pay.id]);
                match fill_slot(slots, d, &pay.matches) {
                    Some(i) => {
                        slots[i].paid = true;
                        id = model::item_id(&[&prefix, &slots[i].due.to_string(), "installment"]);
                    }
                    None => {
                        self.notes.ask(model::new_question(
                            model::item_id(&[&prefix, "payment", &pay.id]),
                            id.clone(),
                            format!(
                                "Which installment is your {} payment on {} for?",
                                model::value(&pay.amount),
                                d.short()
                            ),
                            "It counts as a payment of its own until you say.",
                            format!(
                                "{}.property_tax.parts[{}].paid_from_bank.payments[{}].matches",
                                self.path(),
                                p.id,
                                pay.id
                            ),
                            &[],
                        ));
                    }
                }
                self.add(
                    id,
                    d,
                    MADE,
                    ItemKind::ITEM_KIND_INSTALLMENT,
                    LEDGER,
                    &p.id,
                    &[&pay.id],
                    model::value(&pay.amount),
                    "",
                );
            }
        }
        Ok(())
    }

    /// The year's installments by the order in property_tax's comment.
    fn schedule(&self, m: &PropertyTaxModel) -> Vec<Installment> {
        let mut out = Vec::new();
        for p in &m.parts {
            let Some(PropertyTaxKind::FromBill(fb)) = &p.part else {
                continue;
            };
            for b in &fb.bills {
                if !model::usable(&b.source) {
                    continue;
                }
                for inst in &b.bill.installments {
                    if let Ok(d) = civil::from_field(&inst.due)
                        && !d.is_zero()
                    {
                        out.push(Installment {
                            due: d,
                            amount: model::value(&inst.amount),
                            paid: false,
                        });
                    }
                }
            }
        }
        if out.is_empty() {
            for inst in &m.last_year.bill.installments {
                if let Ok(d) = civil::from_field(&inst.due)
                    && !d.is_zero()
                    && let Ok(moved) = civil::new(d.year() + 1, d.month(), d.day())
                {
                    out.push(Installment {
                        due: moved,
                        amount: model::value(&inst.amount),
                        paid: false,
                    });
                }
            }
        }
        if out.is_empty() {
            for t in &m.due_dates {
                if let Ok(d) = civil::from_proto(Some(t))
                    && !d.is_zero()
                {
                    out.push(Installment {
                        due: d,
                        amount: USCents(0),
                        paid: false,
                    });
                }
            }
        }
        out.sort_by_key(|i| i.due);
        out
    }

    /// Every gift is additive; a receipt and its bank entry are one item;
    /// LikeLastYear repeats last year's gifts dated after its start.
    fn charity(&mut self, m: &CharityModel) {
        let kind = ItemKind::ITEM_KIND_GIFT;
        let prefix = self.prefix();
        for p in &m.parts {
            match &p.part {
                Some(CharityKind::GiftFromBank(g)) => {
                    for g in &g.gifts {
                        let d = civil::from_field(&g.date).unwrap_or_default();
                        self.add(
                            model::item_id(&[&prefix, &d.to_string(), "gift", &g.id]),
                            d,
                            MADE,
                            kind,
                            LEDGER,
                            &p.id,
                            &[&g.id],
                            model::value(&g.amount),
                            &g.payee,
                        );
                    }
                }
                Some(CharityKind::GiftWithReceipt(g)) => {
                    for g in &g.gifts {
                        if !model::usable(&g.source) {
                            continue;
                        }
                        let d = civil::from_field(&g.receipt.date).unwrap_or_default();
                        self.add(
                            model::item_id(&[&prefix, &d.to_string(), "gift", &g.id]),
                            d,
                            MADE,
                            kind,
                            DOCUMENT,
                            &p.id,
                            &[&g.id],
                            model::value(&g.receipt.amount),
                            &g.receipt.payee,
                        );
                    }
                }
                _ => {}
            }
        }
        let rests: Vec<(&str, &RestOfYear)> = m
            .parts
            .iter()
            .filter_map(|p| match &p.part {
                Some(CharityKind::RestOfYear(r)) => Some((p.id.as_str(), &**r)),
                _ => None,
            })
            .collect();
        self.rest_of_year(&rests, kind, "gift");
        for p in &m.parts {
            match &p.part {
                Some(CharityKind::PlannedGift(g)) if g.actual.is.is_empty() => {
                    let d = civil::from_field(&g.date).unwrap_or_default();
                    self.add(
                        model::item_id(&[&prefix, &d.to_string(), "gift", &p.id]),
                        d,
                        EXPECTED,
                        kind,
                        ASSUMED,
                        &p.id,
                        &[],
                        model::value(&g.amount),
                        &g.payee,
                    );
                }
                Some(CharityKind::LikeLastYear(l)) => {
                    let mut from = civil::from_field(&l.from).unwrap_or_default();
                    if from.is_zero() {
                        from = civil::later(self.through, self.ctx.as_of()).add_days(1);
                    }
                    for (i, g) in m.last_year.iter().enumerate() {
                        let last = match civil::from_field(&g.date) {
                            Ok(d) if !d.is_zero() => d,
                            _ => continue,
                        };
                        let d = match civil::new(self.ctx.year(), last.month(), last.day()) {
                            Ok(d) if !d.before(from) => d,
                            _ => continue,
                        };
                        self.add(
                            model::item_id(&[
                                &prefix,
                                &d.to_string(),
                                "gift",
                                &format!("{}-{i}", p.id),
                            ]),
                            d,
                            EXPECTED,
                            kind,
                            ASSUMED,
                            &p.id,
                            &[],
                            model::value(&g.amount),
                            &g.payee,
                        );
                    }
                }
                _ => {}
            }
        }
    }

    /// Every payment is additive; a YearTotal is the year's total, evidence
    /// included, adding the total less the evidence, floored at zero, dated
    /// December 31. Once the evidence exceeds it, it stands aside.
    fn medical(&mut self, m: &MedicalModel) {
        let kind = ItemKind::ITEM_KIND_PAYMENT;
        let prefix = self.prefix();
        for p in &m.parts {
            match &p.part {
                Some(MedicalKind::PaidFromBank(b)) => {
                    for x in &b.payments {
                        let d = civil::from_field(&x.date).unwrap_or_default();
                        self.add(
                            model::item_id(&[&prefix, &d.to_string(), "payment", &x.id]),
                            d,
                            MADE,
                            kind,
                            LEDGER,
                            &p.id,
                            &[&x.id],
                            model::value(&x.amount),
                            &x.payee,
                        );
                    }
                }
                Some(MedicalKind::PaidWithReceipt(r)) => {
                    for x in &r.payments {
                        if !model::usable(&x.source) {
                            continue;
                        }
                        let d = civil::from_field(&x.receipt.date).unwrap_or_default();
                        self.add(
                            model::item_id(&[&prefix, &d.to_string(), "payment", &x.id]),
                            d,
                            MADE,
                            kind,
                            DOCUMENT,
                            &p.id,
                            &[&x.id],
                            model::value(&x.receipt.amount),
                            &x.receipt.payee,
                        );
                    }
                }
                _ => {}
            }
        }
        let rests: Vec<(&str, &RestOfYear)> = m
            .parts
            .iter()
            .filter_map(|p| match &p.part {
                Some(MedicalKind::RestOfYear(r)) => Some((p.id.as_str(), &**r)),
                _ => None,
            })
            .collect();
        self.rest_of_year(&rests, kind, "payment");
        for p in &m.parts {
            match &p.part {
                Some(MedicalKind::PlannedExpense(x)) if x.actual.is.is_empty() => {
                    let d = civil::from_field(&x.date).unwrap_or_default();
                    self.add(
                        model::item_id(&[&prefix, &d.to_string(), "payment", &p.id]),
                        d,
                        EXPECTED,
                        kind,
                        ASSUMED,
                        &p.id,
                        &[],
                        model::value(&x.amount),
                        &x.payee,
                    );
                }
                Some(MedicalKind::YearTotal(y)) => {
                    let (total, have) = (model::value(&y.total), self.made_total());
                    if have > total {
                        self.notes.ask(model::new_question(
                            model::item_id(&[&prefix, "stands-aside", &p.id]),
                            prefix.clone(),
                            format!(
                                "Your medical payments so far, {have}, are more than your total for the year, {total}."
                            ),
                            "The total counts for nothing until it is changed or removed.",
                            format!("{}.medical.parts[{}]", self.path(), p.id),
                            &["Change it", "Remove it"],
                        ));
                        continue;
                    }
                    let end = self.ctx.year_end();
                    self.add(
                        model::item_id(&[&prefix, &end.to_string(), "payment", &p.id]),
                        end,
                        EXPECTED,
                        kind,
                        ASSUMED,
                        &p.id,
                        &[],
                        total - have,
                        "",
                    );
                }
                _ => {}
            }
        }
    }

    fn projection(&mut self) -> ItemisedProjection {
        let mut out = ItemisedProjection {
            id: self.m.id.clone(),
            so_far_through: self.through.field(),
            ..Default::default()
        };
        let mut runs = model::Runs::default();
        for it in self.items.iter_mut() {
            let r = runs.run(it.status == MADE, &it.origin.part);
            it.amount = r.round("amount", it.amount);
        }
        self.bring_to_whole();
        let (mut so_far, mut rest) = (USCents(0), USCents(0));
        for it in &self.items {
            out.items.push(scenariosv1::Item {
                id: it.id.clone(),
                date: it.date.field(),
                status: it.status.into(),
                kind: it.kind.into(),
                origin: it.origin.clone().into(),
                estimated: it.est.clone(),
                figures: Some(
                    ItemisedFigures {
                        amount: model::money(it.amount).into(),
                        payee: it.payee.clone(),
                        ..Default::default()
                    }
                    .into(),
                ),
                ..Default::default()
            });
            if it.status == MADE {
                so_far += it.amount;
            } else {
                rest += it.amount;
            }
        }
        model::sort_items(&mut out.items);
        let year = so_far + rest;
        let (mut year_box, mut so_far_box) = (year, so_far);
        if self.has_whole {
            (year_box, so_far_box) = (self.whole, self.whole);
        }
        let totals = |items: USCents, boxes: USCents| ItemisedTotals {
            items: model::money(items).into(),
            boxes: self.boxes(boxes).into(),
            ..Default::default()
        };
        out.so_far = totals(so_far, so_far_box).into();
        out.rest = totals(rest, year_box - so_far_box).into();
        out.year = totals(year, year_box).into();
        if matches!(self.m.kind, Some(Kind::Mortgage(_))) && !self.has_whole {
            out.expected = vec![ExpectedDocument {
                form: "Form1098".to_string(),
                due: model::forms_due(self.ctx.year()).field(),
                ..Default::default()
            }];
        }
        out
    }

    /// Brings the rounded items to a whole-year document's figure, which
    /// Schedule A takes: an adjustment on the year's last day holds what the
    /// statements leave out, or count beyond it, so the year's items are the
    /// 1098's interest.
    fn bring_to_whole(&mut self) {
        if !self.has_whole {
            return;
        }
        let have: USCents = self.items.iter().map(|it| it.amount).sum();
        let d = model::dollars(self.whole) - have;
        if d.0 != 0 {
            let end = self.ctx.year_end();
            let part = self.whole_part.clone();
            self.add(
                model::item_id(&[&self.prefix(), &end.to_string(), "1098", &part]),
                end,
                MADE,
                ItemKind::ITEM_KIND_ADJUSTMENT,
                DOCUMENT,
                &part,
                &[],
                d,
                "",
            );
        }
    }

    /// Puts an amount on the Schedule A input the model's kind reaches.
    fn boxes(&self, a: USCents) -> ScheduleAInputs {
        let mut b = ScheduleAInputs::default();
        match &self.m.kind {
            Some(Kind::Mortgage(_)) => b.mortgage_interest = model::money(a).into(),
            Some(Kind::PropertyTax(_)) => b.real_estate_taxes = model::money(a).into(),
            Some(Kind::Charity(_)) => b.gifts_by_cash = model::money(a).into(),
            Some(Kind::Medical(_)) => b.medical = model::money(a).into(),
            None => {}
        }
        b
    }
}

/// An open installment's amount, date and part: a PlannedInstallment's for
/// its due date, else the bill's on the due date by the default part.
fn planned_installment(m: &PropertyTaxModel, s: &Installment) -> (USCents, Date, String) {
    let (mut amount, mut on, mut part) = (s.amount, s.due, m.default_part.id.clone());
    for p in &m.parts {
        let Some(PropertyTaxKind::PlannedInstallment(pi)) = &p.part else {
            continue;
        };
        if civil::from_field(&pi.due).unwrap_or_default() == s.due {
            part = p.id.clone();
            let d = civil::from_field(&pi.paid).unwrap_or_default();
            if !d.is_zero() {
                on = d;
            }
            if pi.amount.is_set() {
                amount = model::value(&pi.amount);
            }
        }
    }
    (amount, on, part)
}

/// The installment a payment fills: the one its answer names, else the
/// nearest unpaid whose due date it precedes within 120 days.
fn fill_slot(slots: &[Installment], d: Date, m: &scenariosv1::Match) -> Option<usize> {
    if let Some(To::Slot(s)) = &m.to {
        let want = civil::from_proto(Some(&**s)).unwrap_or_default();
        if let Some(i) = slots.iter().position(|x| x.due == want) {
            return Some(i);
        }
    }
    slots
        .iter()
        .position(|x| !x.paid && !x.due.before(d) && x.due.days_from(d) <= 120)
}
