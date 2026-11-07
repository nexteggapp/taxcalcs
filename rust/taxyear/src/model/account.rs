//! Expands an AccountModel, one bank or brokerage account's interest and
//! dividends, into credits and months. Ported from the Go package
//! `taxyear/model/account`.
//!
//! Its slots are months. A month is evidence once it has ended by as_of; the
//! month holding as_of gets the next source's amount less what is already in
//! it, floored at zero. The fill order is the employer's: a YearTotal for the
//! lines it states, then evidence, then AtRate from its start, then the
//! default, the pace so far. A 1099-INT or 1099-DIV states the year's boxes,
//! and the credits stay as items. See docs/models.md.

use std::collections::{HashMap, HashSet};

use money::USCents;
use proto::scenariosv1::{
    self,
    __buffa::oneof::account_part::{Part, at_rate::Balance, from_forms::form::Form as FormKind},
    __buffa::oneof::r#match::To,
    AccountFigures, AccountModel, AccountPart, AccountProjection, AccountTotals, CreditKind,
    ExpectedDocument, ItemKind, ItemStatus, Origin, account_part,
    origin::Basis,
};

use super::{Context, Error, Notes};
use crate::internal::civil::{self, Date};
use crate::model;

/// An account item's lines, indexed as the constants below.
type Figures = [USCents; 8];

const INTEREST: usize = 0;
const TREASURY: usize = 1;
const ORDINARY: usize = 2;
const QUALIFIED: usize = 3;
const GAIN_DISTRIBUTIONS: usize = 4;
const SECTION_199A: usize = 5;
const EXEMPT: usize = 6;
const FOREIGN: usize = 7;

fn plus(mut f: Figures, o: &Figures) -> Figures {
    for i in 0..f.len() {
        f[i] += o[i];
    }
    f
}

fn is_zero(f: &Figures) -> bool {
    f.iter().all(|v| v.0 == 0)
}

fn figures_proto(f: &Figures) -> AccountFigures {
    let m = |c: USCents| model::money(c).into();
    AccountFigures {
        interest: m(f[INTEREST]),
        treasury_interest: m(f[TREASURY]),
        ordinary_dividends: m(f[ORDINARY]),
        qualified_dividends: m(f[QUALIFIED]),
        capital_gain_distributions: m(f[GAIN_DISTRIBUTIONS]),
        section_199a_dividends: m(f[SECTION_199A]),
        tax_exempt_interest: m(f[EXEMPT]),
        foreign_tax_paid: m(f[FOREIGN]),
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
    f: Figures,
    est: Vec<String>,
}

pub(crate) fn origin(basis: Basis, part: &str, evidence: &[&str]) -> Origin {
    Origin {
        basis: basis.into(),
        part: part.to_string(),
        evidence: evidence.iter().map(|e| e.to_string()).collect(),
        ..Default::default()
    }
}

/// Expands one account model.
pub fn expand(ctx: &Context, m: &AccountModel) -> Result<(AccountProjection, Notes), Error> {
    let mut e = Expansion {
        ctx,
        m,
        notes: Notes::default(),
        items: Vec::new(),
        forms: Vec::new(),
        end: Date::default(),
    };
    e.evidence()?;
    e.rest()?;
    e.reconcile();
    let p = e.projection();
    Ok((p, e.notes))
}

struct Expansion<'a> {
    ctx: &'a Context,
    m: &'a AccountModel,
    notes: Notes,
    items: Vec<Item>,
    forms: Vec<&'a account_part::from_forms::Form>,
    end: Date,
}

/// The lines a line word names in an item id.
const LINE_WORD: [&str; 8] = [
    "interest",
    "treasury_interest",
    "dividend",
    "qualified_dividend",
    "capital_gain_distribution",
    "section_199a_dividend",
    "tax_exempt_interest",
    "foreign_tax",
];

/// A rate is an AtRate part with the day it takes effect.
struct Rate<'a> {
    start: Date,
    part: &'a AccountPart,
    at: &'a account_part::AtRate,
}

impl<'a> Expansion<'a> {
    fn prefix(&self) -> String {
        model::item_id(&["accounts", &self.m.id])
    }

    fn path(&self) -> String {
        format!("model.accounts[{}]", self.m.id)
    }

    /// Makes the made items add up to the year's 1099s, which state the
    /// whole year: an ADJUSTMENT on December 31 for each line the forms
    /// state that the credits don't, such as foreign tax a bank entry never
    /// shows. The qualified share is the boxes' alone, as the items don't
    /// carry it.
    fn reconcile(&mut self) {
        if self.forms.is_empty() {
            return;
        }
        let want = self.form_boxes();
        let mut have = Figures::default();
        let mut diff = Figures::default();
        for it in &self.items {
            if it.status == ItemStatus::ITEM_STATUS_MADE {
                have = plus(have, &it.f);
            }
        }
        for l in 0..8 {
            if l != QUALIFIED {
                diff[l] = want[l] - have[l];
            }
        }
        if is_zero(&diff) {
            return;
        }
        self.items.push(Item {
            id: model::item_id(&[&self.prefix(), "forms", "adjustment"]),
            date: self.ctx.year_end(),
            status: ItemStatus::ITEM_STATUS_MADE,
            kind: ItemKind::ITEM_KIND_ADJUSTMENT,
            origin: origin(Basis::BASIS_DOCUMENT, "", &[]),
            f: diff,
            est: Vec::new(),
        });
    }

    fn evidence(&mut self) -> Result<(), Error> {
        let m = self.m;
        let mut replaced = HashSet::new();
        for p in &m.parts {
            if let Some(Part::FromForms(ff)) = &p.part {
                for f in &ff.forms {
                    for r in &f.replaces {
                        replaced.insert(format!("{}|{}", r.vault_ref, r.name));
                    }
                }
            }
        }
        for p in &m.parts {
            match &p.part {
                Some(Part::FromCredits(fc)) => {
                    for c in &fc.credits {
                        if matches!(c.matches.to, Some(To::NotThisSource(_))) {
                            continue;
                        }
                        let d = match civil::from_field(&c.date) {
                            Ok(d) if !d.is_zero() => d,
                            _ => {
                                return Err(Error::Expansion(format!(
                                    "{}: credit {} has no date",
                                    self.path(),
                                    c.id
                                )));
                            }
                        };
                        let mut f = Figures::default();
                        f[line_of(c.kind.as_known().unwrap_or_default())] = model::value(&c.amount);
                        self.items.push(Item {
                            id: model::item_id(&[&self.prefix(), &d.to_string(), "credit", &c.id]),
                            date: d,
                            status: ItemStatus::ITEM_STATUS_MADE,
                            kind: ItemKind::ITEM_KIND_CREDIT,
                            origin: origin(Basis::BASIS_LEDGER, &p.id, &[&c.id]),
                            f,
                            est: Vec::new(),
                        });
                    }
                }
                Some(Part::FromForms(ff)) => {
                    for f in &ff.forms {
                        if model::usable(&f.source)
                            && !replaced
                                .contains(&format!("{}|{}", f.source.vault_ref, f.source.name))
                        {
                            self.forms.push(f);
                        }
                    }
                }
                Some(Part::Closed(c)) => {
                    self.end = civil::from_field(&c.on).unwrap_or_default();
                }
                Some(Part::Closes(c)) => {
                    let d = civil::from_field(&c.on).unwrap_or_default();
                    if d.after(self.ctx.evidence_through()) {
                        self.end = d;
                    }
                }
                _ => {}
            }
        }
        Ok(())
    }

    /// Adds the year-end distributions and fills the rest of the year. A
    /// whole-year form states the year, so a model with one fills nothing
    /// more.
    fn rest(&mut self) -> Result<(), Error> {
        self.distributions()?;
        if !self.forms.is_empty() {
            return Ok(());
        }
        let through = self.ctx.evidence_through();
        let months: Vec<Date> = self
            .ctx
            .months()
            .into_iter()
            .filter(|mo| {
                mo.after(through) && (self.end.is_zero() || !model::month_of(self.end).before(*mo))
            })
            .collect();
        if months.is_empty() {
            return Ok(());
        }
        let rates = self.rates()?;
        let rate_from = rates.first().map(|r| r.start).unwrap_or_default();
        if self.m.default_part.is_set() {
            self.pace(months[months.len() - 1], rate_from);
        } else {
            let prefix = self.prefix();
            self.notes.ask(model::new_question(
                model::item_id(&[&prefix, "default"]),
                prefix,
                format!("What will {} pay for the rest of the year?", self.m.name),
                "Nothing is counted for its months after the evidence until you say.",
                format!("{}.default_part", self.path()),
                &[],
            ));
        }
        let mut monthly: HashMap<String, Item> = HashMap::new();
        for mo in &months {
            for r in &rates {
                if !r.start.after(*mo) {
                    let interest = self.month_interest(r.at);
                    let m = self.month_item(&mut monthly, *mo, &r.part.id);
                    m.f[INTEREST] = interest;
                }
            }
        }
        self.year_total(&months, &mut monthly);
        self.keep(&monthly, &months);
        Ok(())
    }

    /// The default, the pace so far: each kind of credit recurs at the
    /// median gap between its credits so far, at their average amount, on
    /// the cadence's dates through until. A date in a month already ended
    /// gets nothing, since the month's evidence is in; one in the month of
    /// as_of gets the average less what is already in the month. Interest
    /// stops where an AtRate part starts.
    fn pace(&mut self, until: Date, rate_from: Date) {
        let mut dates: HashMap<usize, Vec<Date>> = HashMap::new();
        let mut total: HashMap<usize, USCents> = HashMap::new();
        let mut in_month: HashMap<(usize, Date), USCents> = HashMap::new();
        for it in &self.items {
            if it.status != ItemStatus::ITEM_STATUS_MADE {
                continue;
            }
            for (l, v) in it.f.iter().enumerate() {
                if v.0 != 0 {
                    dates.entry(l).or_default().push(it.date);
                    *total.entry(l).or_default() += *v;
                    *in_month.entry((l, model::month_of(it.date))).or_default() += *v;
                }
            }
        }
        let through = self.ctx.evidence_through();
        let part = self.m.default_part.id.clone();
        for l in 0..8 {
            let Some(ds) = dates.get(&l) else {
                continue;
            };
            // Foreign tax is what has been taken so far: it follows
            // dividends from abroad, which needn't recur, so it isn't paced.
            if ds.is_empty() || l == FOREIGN {
                continue;
            }
            let avg = total.get(&l).copied().unwrap_or_default() / ds.len() as i64;
            let mut last = ds[0];
            for d in ds {
                last = civil::later(last, *d);
            }
            for d in model::cadence_of(ds).through(last, until) {
                if !d.after(through)
                    || (l == INTEREST && !rate_from.is_zero() && !d.before(rate_from))
                    || self.closed_by(d)
                {
                    continue;
                }
                let mut f = Figures::default();
                let have = in_month
                    .get(&(l, model::month_of(d)))
                    .copied()
                    .unwrap_or_default();
                f[l] = (avg - have).max(USCents(0));
                self.items.push(Item {
                    id: model::item_id(&[&self.prefix(), &d.to_string(), LINE_WORD[l]]),
                    date: d,
                    status: ItemStatus::ITEM_STATUS_EXPECTED,
                    kind: ItemKind::ITEM_KIND_CREDIT,
                    origin: origin(Basis::BASIS_ASSUMED, &part, &[]),
                    f,
                    est: Vec::new(),
                });
            }
        }
    }

    /// Reports that the account has closed before d.
    fn closed_by(&self, d: Date) -> bool {
        !self.end.is_zero() && d.after(self.end)
    }

    /// Returns the month's item, made when first asked for.
    fn month_item<'m>(
        &self,
        monthly: &'m mut HashMap<String, Item>,
        mo: Date,
        part: &str,
    ) -> &'m mut Item {
        let prefix = self.prefix();
        monthly.entry(mo.to_string()).or_insert_with(|| Item {
            id: model::item_id(&[&prefix, &mo.to_string(), "month"]),
            date: mo,
            status: ItemStatus::ITEM_STATUS_EXPECTED,
            kind: ItemKind::ITEM_KIND_MONTH,
            origin: origin(Basis::BASIS_ASSUMED, part, &[]),
            f: Figures::default(),
            est: Vec::new(),
        })
    }

    /// Adds the month items with any figure, in month order.
    fn keep(&mut self, monthly: &HashMap<String, Item>, months: &[Date]) {
        for mo in months {
            if let Some(m) = monthly.get(&mo.to_string())
                && !is_zero(&m.f)
            {
                self.items.push(m.clone());
            }
        }
    }

    /// Adds each YearEndDistribution, on its date or December 31, unless its
    /// answer says a credit is it.
    fn distributions(&mut self) -> Result<(), Error> {
        let m = self.m;
        for p in &m.parts {
            let Some(Part::YearEndDistribution(y)) = &p.part else {
                continue;
            };
            if !y.actual.is.is_empty() {
                continue;
            }
            let mut d =
                civil::from_field(&y.amounts.date).map_err(|e| Error::Expansion(e.to_string()))?;
            if d.is_zero() {
                d = self.ctx.year_end();
            }
            if self.closed_by(d) {
                continue;
            }
            let mut f = Figures::default();
            f[ORDINARY] = model::value(&y.amounts.ordinary_dividends);
            f[GAIN_DISTRIBUTIONS] = model::value(&y.amounts.capital_gain_distributions);
            self.items.push(Item {
                id: model::item_id(&[&self.prefix(), &d.to_string(), "distribution", &p.id]),
                date: d,
                status: ItemStatus::ITEM_STATUS_EXPECTED,
                kind: ItemKind::ITEM_KIND_DISTRIBUTION,
                origin: origin(Basis::BASIS_ASSUMED, &p.id, &[]),
                f,
                est: Vec::new(),
            });
        }
        Ok(())
    }

    /// The AtRate parts, each from its start clipped to the day after the
    /// evidence.
    fn rates(&self) -> Result<Vec<Rate<'a>>, Error> {
        let m = self.m;
        let mut out = Vec::new();
        for p in &m.parts {
            if let Some(Part::AtRate(r)) = &p.part {
                let from =
                    civil::from_field(&r.from).map_err(|e| Error::Expansion(e.to_string()))?;
                let mut start = self.ctx.evidence_through().add_days(1);
                if from.after(start) {
                    start = from;
                }
                out.push(Rate {
                    start,
                    part: p,
                    at: r,
                });
            }
        }
        Ok(out)
    }

    /// Replaces, for each line a YearTotal states, every expected figure on
    /// that line with equal shares of the year less the evidence, one a
    /// remaining month.
    fn year_total(&mut self, months: &[Date], monthly: &mut HashMap<String, Item>) {
        let am = self.m;
        for p in &am.parts {
            let Some(Part::YearTotal(yt)) = &p.part else {
                continue;
            };
            let lines = [
                &yt.interest,
                &yt.treasury_interest,
                &yt.ordinary_dividends,
                &yt.qualified_dividends,
                &yt.capital_gain_distributions,
                &yt.section_199a_dividends,
                &yt.tax_exempt_interest,
                &yt.foreign_tax_paid,
            ];
            let mut evidence = Figures::default();
            for it in &self.items {
                if it.status == ItemStatus::ITEM_STATUS_MADE {
                    evidence = plus(evidence, &it.f);
                }
            }
            for (l, stated) in lines.iter().enumerate() {
                if !stated.is_set() {
                    continue;
                }
                for it in self.items.iter_mut() {
                    if it.status == ItemStatus::ITEM_STATUS_EXPECTED
                        && it.kind != ItemKind::ITEM_KIND_DISTRIBUTION
                    {
                        it.f[l] = USCents(0);
                    }
                }
                let shares = model::share(
                    (model::value(*stated) - evidence[l]).max(USCents(0)),
                    months.len() as i64,
                );
                for (i, v) in shares.into_iter().enumerate() {
                    let m = self.month_item(monthly, months[i], &p.id);
                    m.f[l] = v;
                    m.origin.part = p.id.clone();
                }
            }
        }
        self.items
            .retain(|it| it.status == ItemStatus::ITEM_STATUS_MADE || !is_zero(&it.f));
    }

    /// A month's interest at a rate a year on the balance the part sets, or
    /// the model's balance plus what it adds.
    fn month_interest(&self, r: &account_part::AtRate) -> USCents {
        let mut balance = model::value(&self.m.balance);
        match &r.balance {
            Some(Balance::SetBalance(b)) => balance = model::value(&**b),
            Some(Balance::Add(a)) => balance += model::value(&**a),
            None => {}
        }
        let (rate, _) = model::percent(r.rate.as_option());
        model::of_fine(rate, balance) / 12
    }

    fn projection(&mut self) -> AccountProjection {
        let mut out = AccountProjection {
            id: self.m.id.clone(),
            so_far_through: self.ctx.evidence_through().field(),
            ..Default::default()
        };
        let mut runs = model::Runs::default();
        for it in self.items.iter_mut() {
            let r = runs.run(it.status == ItemStatus::ITEM_STATUS_MADE, &it.origin.part);
            for (l, word) in LINE_WORD.iter().enumerate() {
                it.f[l] = r.round(word, it.f[l]);
            }
        }
        let (mut made, mut rest, mut all) =
            (Figures::default(), Figures::default(), Figures::default());
        for it in &self.items {
            out.items.push(scenariosv1::Item {
                id: it.id.clone(),
                date: it.date.field(),
                status: it.status.into(),
                kind: it.kind.into(),
                origin: it.origin.clone().into(),
                estimated: it.est.clone(),
                figures: Some(figures_proto(&it.f).into()),
                ..Default::default()
            });
            all = plus(all, &it.f);
            if it.status == ItemStatus::ITEM_STATUS_MADE {
                made = plus(made, &it.f);
            } else {
                rest = plus(rest, &it.f);
            }
        }
        model::sort_items(&mut out.items);
        let mut year_boxes = self.boxes(all);
        let mut so_far_boxes = self.boxes(made);
        if !self.forms.is_empty() {
            year_boxes = self.form_boxes();
            so_far_boxes = year_boxes;
        }
        let mut rest_boxes = Figures::default();
        for l in 0..8 {
            rest_boxes[l] = year_boxes[l] - so_far_boxes[l];
        }
        let totals = |items: &Figures, boxes: &Figures| AccountTotals {
            items: figures_proto(items).into(),
            boxes: figures_proto(boxes).into(),
            ..Default::default()
        };
        out.so_far = totals(&made, &so_far_boxes).into();
        out.rest = totals(&rest, &rest_boxes).into();
        out.year = totals(&all, &year_boxes).into();
        if self.forms.is_empty() {
            let due = model::forms_due(self.ctx.year()).field();
            if (all[INTEREST] + all[TREASURY] + all[EXEMPT]).0 > 0 {
                out.expected.push(ExpectedDocument {
                    form: "Form1099Int".to_string(),
                    due: due.clone(),
                    ..Default::default()
                });
            }
            if (all[ORDINARY] + all[GAIN_DISTRIBUTIONS]).0 > 0 {
                out.expected.push(ExpectedDocument {
                    form: "Form1099Div".to_string(),
                    due,
                    ..Default::default()
                });
            }
        }
        out
    }

    /// The item totals with the qualified share of ordinary dividends
    /// estimated from last year's, until this year's 1099-DIV arrives: the
    /// model's last_year_qualified_share, else last year's 1099-DIV. With no
    /// such form, none is counted as qualified, so tax is not understated.
    fn boxes(&self, mut f: Figures) -> Figures {
        let ly = &self.m.last_year.form;
        if f[QUALIFIED].0 == 0 && f[ORDINARY].0 > 0 {
            let (share, ok) = model::percent(self.m.last_year_qualified_share.as_option());
            if ok {
                f[QUALIFIED] = model::of_fine(share, f[ORDINARY]);
            } else {
                let o = model::value(&ly.ordinary_dividends);
                if o.0 > 0 {
                    f[QUALIFIED] =
                        model::scale(f[ORDINARY], model::value(&ly.qualified_dividends), o);
                }
            }
        }
        f
    }

    fn form_boxes(&self) -> Figures {
        let mut f = Figures::default();
        for form in &self.forms {
            match &form.form {
                Some(FormKind::Form1099Int(i)) => {
                    // Form 1099-INT (26 U.S.C. §§ 61(a)(4), 103; Schedule B Part I; Form 1040 lines 2a–2b):
                    // Box 1 (interest_income) and box 3 (savings_bond_and_treasury_interest, exempt from
                    // state income tax under 31 U.S.C. § 3124) are taxable interest; box 8 is tax-exempt
                    // municipal bond interest (§ 103, Form 1040 line 2a); box 6 is foreign tax paid (§ 901).
                    f[INTEREST] += model::value(&i.interest_income);
                    f[TREASURY] += model::value(&i.savings_bond_and_treasury_interest);
                    f[EXEMPT] += model::value(&i.tax_exempt_interest);
                    f[FOREIGN] += model::value(&i.foreign_tax_paid);
                }
                Some(FormKind::Form1099Div(d)) => {
                    // Form 1099-DIV (26 U.S.C. §§ 1(h)(11), 61(a)(7), 199A, 852; Schedule B Part II; Form 1040 lines 3a–3b):
                    // Box 1a is total ordinary dividends, of which box 1b is qualified dividends taxed at
                    // capital gain rates under § 1(h)(11); box 2a is total capital gain distributions
                    // (Schedule D line 13); box 5 is § 199A REIT/RIC dividends; box 12 is exempt-interest
                    // dividends (§ 852(b)(5), Form 1040 line 2a); box 7 is foreign tax paid (§ 901).
                    f[ORDINARY] += model::value(&d.ordinary_dividends);
                    f[QUALIFIED] += model::value(&d.qualified_dividends);
                    f[GAIN_DISTRIBUTIONS] += model::value(&d.total_capital_gain_distribution);
                    f[SECTION_199A] += model::value(&d.section_199a_dividends);
                    f[EXEMPT] += model::value(&d.exempt_interest_dividends);
                    f[FOREIGN] += model::value(&d.foreign_tax_paid);
                }
                Some(FormKind::Form1099Oid(o)) => {
                    // Form 1099-OID (26 U.S.C. §§ 1272, 1276, 171; Publication 1212; Schedule B Part I):
                    // Taxable non-Treasury OID reported as interest on Schedule B is the sum of box 1
                    // (original_issue_discount), box 2 (other_periodic_interest), and box 5 (market_discount
                    // accrued under § 1276/§ 1278), reduced by box 6 (acquisition_premium under § 1272(a)(7))
                    // and box 10 (bond_premium amortized under § 171), floored at zero because excess
                    // amortizable bond premium is not deducted as negative interest on Schedule B.
                    // Box 8 (original_issue_discount_on_treasury_obligations) is tracked separately as
                    // U.S. Treasury interest (exempt from state tax under 31 U.S.C. § 3124), and box 11
                    // (tax_exempt_oid) flows to tax-exempt interest on Form 1040 line 2a.
                    let taxable_oid = model::value(&o.original_issue_discount)
                        + model::value(&o.other_periodic_interest)
                        + model::value(&o.market_discount)
                        - model::value(&o.acquisition_premium)
                        - model::value(&o.bond_premium);
                    if taxable_oid.0 > 0 {
                        f[INTEREST] += taxable_oid;
                    }
                    f[TREASURY] += model::value(&o.original_issue_discount_on_treasury_obligations);
                    f[EXEMPT] += model::value(&o.tax_exempt_oid);
                }
                None => {}
            }
        }
        f
    }
}

fn line_of(k: CreditKind) -> usize {
    match k {
        CreditKind::CREDIT_KIND_TREASURY_INTEREST => TREASURY,
        CreditKind::CREDIT_KIND_DIVIDEND => ORDINARY,
        CreditKind::CREDIT_KIND_CAPITAL_GAIN_DISTRIBUTION => GAIN_DISTRIBUTIONS,
        CreditKind::CREDIT_KIND_TAX_EXEMPT_INTEREST => EXEMPT,
        CreditKind::CREDIT_KIND_FOREIGN_TAX => FOREIGN,
        _ => INTEREST,
    }
}
