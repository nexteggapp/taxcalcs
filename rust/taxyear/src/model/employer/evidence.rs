//! A job's evidence: paystubs, deposits and W-2s, and the items they make.
//! Ported from evidence.go.

use std::collections::{HashMap, HashSet};

use money::USCents;
use proto::factsv1::{self, PaycheckKind};
use proto::scenariosv1::{
    self, __buffa::oneof::employer_part::Part, __buffa::oneof::r#match::To, EmployerPart, ItemKind,
    ItemStatus, Origin, origin::Basis,
};

use super::pay::{Line, Pay, pay_of, set_line};
use super::{Deposit, Draft, Job, Stub, abs, item_kind, kind_word};
use crate::internal::civil::{self, Date};
use crate::model::{self, Error, new_question};

/// How many days from a paystub a deposit may be and pair with it.
const PAIR_WINDOW: i64 = 3;

/// The slot a match names, if any.
fn match_slot(m: &scenariosv1::Match) -> Option<&proto::commonv1::Date> {
    match &m.to {
        Some(To::Slot(d)) => Some(d),
        _ => None,
    }
}

/// The item a match names, or "".
fn match_item(m: &scenariosv1::Match) -> &str {
    match &m.to {
        Some(To::Item(s)) => s,
        _ => "",
    }
}

fn not_this_source(m: &scenariosv1::Match) -> bool {
    matches!(m.to, Some(To::NotThisSource(_)))
}

/// Identifies a document by its store reference, or its name when it has
/// none.
fn ref_key(r: &factsv1::DocumentRef) -> String {
    if !r.vault_ref.is_empty() {
        return format!("v:{}", r.vault_ref);
    }
    format!("n:{}", r.name)
}

impl<'a> Job<'a> {
    /// Collects the job's usable paystubs, deposits and W-2s. A document the
    /// household removed, and an entry answered as not this source's, count
    /// for nothing.
    pub(super) fn read_evidence(&mut self) -> Result<(), Error> {
        let m = self.m;
        let mut replaced = HashSet::new();
        for part in &m.parts {
            if let Some(Part::FromW2(w)) = &part.part {
                for r in &w.replaces {
                    replaced.insert(ref_key(r));
                }
            }
        }
        for part in &m.parts {
            match &part.part {
                Some(Part::FromPaystubs(_)) => self.read_stubs(part)?,
                Some(Part::FromDeposits(_)) => self.read_deposits(part)?,
                Some(Part::FromW2(w))
                    if model::usable(&w.source)
                        && !replaced.contains(&ref_key(&w.source))
                        && (w.w2.tax_year == 0
                            || i64::from(w.w2.tax_year) == self.ctx.year()
                            || w.source.use_anyway) =>
                {
                    self.w2s.push(&*w.w2);
                    self.w2_parts.push(part.id.clone());
                }
                _ => {}
            }
        }
        self.stubs.sort_by_key(|s| s.date);
        self.deposits.sort_by_key(|d| d.date);
        for (i, s) in self.stubs.iter().enumerate() {
            self.latest = civil::later(self.latest, s.date);
            if s.kind == PaycheckKind::PAYCHECK_KIND_REGULAR {
                self.regular = Some(i);
            }
        }
        for d in &self.deposits {
            self.latest = civil::later(self.latest, d.date);
        }
        if !self.w2s.is_empty() {
            self.latest = civil::later(self.latest, self.ctx.year_end());
        }
        Ok(())
    }

    /// Reads a part's paystubs. Another year's paystub counts only when the
    /// household says to use it anyway.
    fn read_stubs(&mut self, part: &EmployerPart) -> Result<(), Error> {
        let Some(Part::FromPaystubs(fp)) = &part.part else {
            return Ok(());
        };
        for s in &fp.stubs {
            if !model::usable(&s.source) || not_this_source(&s.matches) {
                continue;
            }
            let d = match civil::from_field(&s.paystub.pay_date) {
                Ok(d) if !d.is_zero() => d,
                _ => {
                    return Err(Error::Expansion(format!(
                        "{}: paystub {} has no pay date",
                        self.path(),
                        s.id
                    )));
                }
            };
            if d.year() != self.ctx.year() && !s.source.use_anyway {
                continue;
            }
            let mut st = Stub {
                id: s.id.clone(),
                part: part.id.clone(),
                date: d,
                kind: s.paystub.kind,
                this: pay_of(&s.paystub.this_paycheck),
                ytd: Pay::default(),
                has_ytd: false,
                status: s.paystub.withholding_status,
                matches: (*s.matches).clone(),
                item: 0,
                paired: Vec::new(),
            };
            if let Some(y) = s.paystub.year_to_date.as_option() {
                st.ytd = pay_of(y);
                st.has_ytd = true;
            }
            self.stubs.push(st);
        }
        Ok(())
    }

    /// Reads a part's deposits in the tax year.
    fn read_deposits(&mut self, part: &EmployerPart) -> Result<(), Error> {
        let Some(Part::FromDeposits(fd)) = &part.part else {
            return Ok(());
        };
        for dep in &fd.deposits {
            if not_this_source(&dep.matches) {
                continue;
            }
            let d = match civil::from_field(&dep.date) {
                Ok(d) if !d.is_zero() => d,
                _ => {
                    return Err(Error::Expansion(format!(
                        "{}: deposit {} has no date",
                        self.path(),
                        dep.id
                    )));
                }
            };
            if d.year() != self.ctx.year() {
                continue;
            }
            self.deposits.push(Deposit::of(dep, &part.id, d));
        }
        Ok(())
    }

    /// Turns the evidence into items: each regular paystub fills the pay
    /// date nearest it, a bonus, vest or final one is additive, deposits
    /// pair with the paystub they are, and the rest fill a pay date no
    /// paystub fills or are extra paychecks.
    pub(super) fn match_evidence(&mut self) {
        self.filled = HashMap::new();
        for s in 0..self.stubs.len() {
            self.place_stub(s);
        }
        self.pair_explicit();
        for s in 0..self.stubs.len() {
            if self.stubs[s].this.net.set {
                self.pair_by_net(s);
            }
        }
        for s in 0..self.stubs.len() {
            if !self.stubs[s].this.net.set {
                self.pair_by_date(s);
            }
        }
        for s in 0..self.stubs.len() {
            let mut take = USCents(0);
            let item = self.stubs[s].item;
            for &d in &self.stubs[s].paired {
                take += self.deposits[d].amount;
                self.items[item]
                    .origin
                    .evidence
                    .push(self.deposits[d].id.clone());
            }
            if !self.stubs[s].paired.is_empty() {
                self.items[item].take_home = set_line(take);
            }
        }
        self.slot_deposits();
    }

    /// Makes a paystub's item.
    fn place_stub(&mut self, si: usize) {
        let s = self.stubs[si].clone();
        let mut d = Draft {
            date: s.date,
            status: ItemStatus::ITEM_STATUS_MADE,
            kind: item_kind(s.kind),
            origin: Origin {
                basis: Basis::BASIS_DOCUMENT.into(),
                part: s.part.clone(),
                evidence: vec![s.id.clone()],
                ..Default::default()
            },
            p: s.this.clone(),
            ..Draft::default()
        };
        let idx = self.items.len();
        self.stubs[si].item = idx;
        if s.kind != PaycheckKind::PAYCHECK_KIND_REGULAR {
            d.id = model::item_id(&[
                &self.item_prefix(),
                &s.date.to_string(),
                kind_word(s.kind),
                &s.id,
            ]);
            self.items.push(d);
            return;
        }
        let Some(slot) = self.slot_for(s.date, Some(&s.matches)) else {
            d.id = model::item_id(&[&self.item_prefix(), &s.date.to_string(), "regular", &s.id]);
            self.notes.ask(new_question(
                model::item_id(&[&self.item_prefix(), "paystub", &s.id]),
                d.id.clone(),
                format!(
                    "Which pay date is your {} paystub from {} for?",
                    s.date.short(),
                    self.m.name
                ),
                "It counts as an extra paycheck until you say.",
                format!(
                    "{}.parts[{}].from_paystubs.stubs[{}].matches",
                    self.path(),
                    s.part,
                    s.id
                ),
                &["A pay date", "Not this job's"],
            ));
            self.items.push(d);
            return;
        };
        d.id = self.slot_id(slot);
        self.filled.insert(slot.to_string(), idx);
        self.items.push(d);
    }

    /// Returns the pay date a paystub or deposit fills: the one its answer
    /// names, else the nearest within half a pay period that nothing has
    /// filled.
    fn slot_for(&self, at: Date, m: Option<&scenariosv1::Match>) -> Option<Date> {
        if let Some(slot) = m.and_then(match_slot)
            && let Ok(d) = civil::from_proto(Some(slot))
            && !self.filled.contains_key(&d.to_string())
        {
            return Some(d);
        }
        let (mut best, mut best_days) = (Date::default(), self.half_period() + 1);
        for &s in &self.slots {
            let days = abs(at.days_from(s));
            if days < best_days && !self.filled.contains_key(&s.to_string()) {
                (best, best_days) = (s, days);
            }
        }
        if best.is_zero() { None } else { Some(best) }
    }

    /// Pairs each deposit whose answer names an item with that paystub's
    /// item.
    fn pair_explicit(&mut self) {
        for d in 0..self.deposits.len() {
            let want = match_item(&self.deposits[d].matches).to_string();
            if want.is_empty() {
                continue;
            }
            for s in 0..self.stubs.len() {
                if self.items[self.stubs[s].item].id == want {
                    self.stubs[s].paired.push(d);
                    self.deposits[d].used = true;
                }
            }
        }
    }

    /// Pairs the deposits within three days of a paystub that sum to its
    /// net pay, so split deposits pair too. Two or more sets that do are a
    /// question; until it is answered the set nearest in date pairs, and of
    /// sets as near, the one with fewer deposits.
    fn pair_by_net(&mut self, s: usize) {
        let mut cands = self.candidates(s);
        cands.truncate(12);
        let net = self.stubs[s].this.net.v;
        let mut sets = Vec::new();
        for mask in 1usize..(1 << cands.len()) {
            let mut t = USCents(0);
            let mut set = Vec::new();
            for (i, &c) in cands.iter().enumerate() {
                if mask & (1 << i) != 0 {
                    t += self.deposits[c].amount;
                    set.push(c);
                }
            }
            if t == net {
                sets.push(set);
            }
        }
        self.pair_nearest(s, sets);
    }

    /// The deposits within three days of a paystub that nothing has paired
    /// yet.
    fn candidates(&self, s: usize) -> Vec<usize> {
        let at = self.stubs[s].date;
        (0..self.deposits.len())
            .filter(|&i| {
                let d = &self.deposits[i];
                !d.used
                    && match_slot(&d.matches).is_none()
                    && abs(d.date.days_from(at)) <= PAIR_WINDOW
            })
            .collect()
    }

    /// Pairs a paystub with no net pay with the deposits within three days
    /// of it, those on one day taken as one set. Sets on two or more days
    /// are a question; until it is answered the set nearest in date pairs.
    fn pair_by_date(&mut self, s: usize) {
        let mut by_day: Vec<(String, Vec<usize>)> = Vec::new();
        for d in self.candidates(s) {
            let k = self.deposits[d].date.to_string();
            match by_day.iter_mut().find(|(day, _)| *day == k) {
                Some((_, set)) => set.push(d),
                None => by_day.push((k, vec![d])),
            }
        }
        let sets = by_day.into_iter().map(|(_, set)| set).collect();
        self.pair_nearest(s, sets);
    }

    /// Pairs the set nearest the paystub in date, asking when there is more
    /// than one.
    fn pair_nearest(&mut self, s: usize, mut sets: Vec<Vec<usize>>) {
        if sets.is_empty() {
            return;
        }
        let at = self.stubs[s].date;
        let distance = |set: &[usize]| -> i64 {
            set.iter()
                .map(|&d| abs(self.deposits[d].date.days_from(at)))
                .sum()
        };
        sets.sort_by(|a, b| {
            distance(a)
                .cmp(&distance(b))
                .then_with(|| a.len().cmp(&b.len()))
        });
        if sets.len() > 1 {
            let st = &self.stubs[s];
            self.notes.ask(new_question(
                model::item_id(&[&self.item_prefix(), "paystub", &st.id, "deposits"]),
                self.items[st.item].id.clone(),
                format!(
                    "Which deposits are your {} paystub from {}? More than one set could be.",
                    st.date.short(),
                    self.m.name
                ),
                "The deposits nearest the paystub's date are counted as it until you say.",
                format!(
                    "{}.parts[{}].from_deposits.deposits",
                    self.path(),
                    self.deposits[sets[0][0]].part
                ),
                &[],
            ));
        }
        for &d in &sets[0] {
            self.deposits[d].used = true;
            self.stubs[s].paired.push(d);
        }
    }

    /// Fills the pay dates no paystub fills from the deposits left: the
    /// deposits nearest one pay date, within three days of each other, are
    /// that paycheck, copying the latest regular paystub before it. Any
    /// other deposit is an extra paycheck with nothing withheld.
    fn slot_deposits(&mut self) {
        for d in 0..self.deposits.len() {
            if self.deposits[d].used {
                continue;
            }
            let Some(slot) = self.slot_for(self.deposits[d].date, Some(&self.deposits[d].matches))
            else {
                self.extra_deposit(d);
                continue;
            };
            let mut set = vec![d];
            self.deposits[d].used = true;
            for o in 0..self.deposits.len() {
                let od = &self.deposits[o];
                if !od.used
                    && match_slot(&od.matches).is_none()
                    && abs(od.date.days_from(self.deposits[d].date)) <= PAIR_WINDOW
                    && self.slot_for(od.date, None) == Some(slot)
                {
                    self.deposits[o].used = true;
                    set.push(o);
                }
            }
            let idx = self.deposit_paycheck(slot, &set);
            self.filled.insert(slot.to_string(), idx);
        }
    }

    /// The paycheck a set of deposits is: the template, the latest regular
    /// paystub before it, copied and marked as estimated, paid as the
    /// deposits. With no template it is gross pay of the deposits, nothing
    /// withheld, and the result asks.
    fn deposit_paycheck(&mut self, slot: Date, set: &[usize]) -> usize {
        let mut take = USCents(0);
        let mut ids = Vec::new();
        for &d in set {
            take += self.deposits[d].amount;
            ids.push(self.deposits[d].id.clone());
        }
        let mut d = Draft {
            id: self.slot_id(slot),
            date: slot,
            status: ItemStatus::ITEM_STATUS_MADE,
            kind: ItemKind::ITEM_KIND_PAYCHECK,
            origin: Origin {
                basis: Basis::BASIS_LEDGER.into(),
                part: self.deposits[set[0]].part.clone(),
                evidence: ids,
                ..Default::default()
            },
            take_home: set_line(take),
            ..Draft::default()
        };
        if let Some((p, tmpl)) = self.template(slot) {
            d.estimated = copied_paths(&p);
            d.p = p;
            d.origin.template = tmpl;
        } else {
            d.p = Pay {
                gross: set_line(take),
                federal: set_line(USCents(0)),
                ss: set_line(USCents(0)),
                medicare: set_line(USCents(0)),
                ..Pay::default()
            };
            d.estimated = vec!["paycheck.gross".to_string()];
            self.notes.ask(new_question(
                model::item_id(&[&self.item_prefix(), "deposit", &self.deposits[set[0]].id]),
                d.id.clone(),
                format!(
                    "What was your gross pay on {} from {}? There is no paystub to read it from.",
                    slot.short(),
                    self.m.name
                ),
                format!(
                    "Your {take} deposit counts as gross pay with nothing withheld until a paystub arrives."
                ),
                format!("{}.default_part.each_paycheck", self.path()),
                &[],
            ));
        }
        self.items.push(d);
        self.items.len() - 1
    }

    /// The paycheck a pay date with no paystub of its own copies: the
    /// latest regular paystub on or before it; else the first after it;
    /// else the assumed PayRate in effect on it, or the earliest. It
    /// returns the figures, net left unset, and the paystub's or part's id.
    pub(super) fn template(&self, at: Date) -> Option<(Pay, String)> {
        if let Some(s) = self.regular_before(at) {
            let mut p = s.this.clone();
            p.net = Line::default();
            return Some((p, s.id.clone()));
        }
        let mut in_effect: Option<&EmployerPart> = None;
        let mut earliest: Option<&EmployerPart> = None;
        let (mut in_from, mut earliest_from) = (Date::default(), Date::default());
        for part in &self.m.parts {
            let Some(Part::PayRate(r)) = &part.part else {
                continue;
            };
            let from = civil::from_field(&r.from).unwrap_or_default();
            if !from.after(at) && (in_effect.is_none() || !from.before(in_from)) {
                (in_effect, in_from) = (Some(part), from);
            }
            if earliest.is_none() || from.before(earliest_from) {
                (earliest, earliest_from) = (Some(part), from);
            }
        }
        let best = in_effect.or(earliest)?;
        let Some(Part::PayRate(r)) = &best.part else {
            return None;
        };
        let mut p = pay_of(&r.paycheck);
        p.net = Line::default();
        Some((p, best.id.clone()))
    }

    /// The latest regular paystub on or before the date, else the earliest
    /// after it.
    fn regular_before(&self, at: Date) -> Option<&Stub> {
        let (mut before, mut after) = (None, None);
        for s in &self.stubs {
            if s.kind != PaycheckKind::PAYCHECK_KIND_REGULAR {
                continue;
            }
            if !s.date.after(at) {
                before = Some(s);
            } else if after.is_none() {
                after = Some(s);
            }
        }
        before.or(after)
    }

    /// A deposit no paystub or pay date explains: an extra paycheck, its
    /// gross the template's gross, or the deposit's with no template, and
    /// nothing withheld. The result asks what it is.
    fn extra_deposit(&mut self, di: usize) {
        self.deposits[di].used = true;
        let dep = self.deposits[di].clone();
        let mut d = Draft {
            id: model::item_id(&[
                &self.item_prefix(),
                &dep.date.to_string(),
                "deposit",
                &dep.id,
            ]),
            date: dep.date,
            status: ItemStatus::ITEM_STATUS_MADE,
            kind: ItemKind::ITEM_KIND_DEPOSIT,
            origin: Origin {
                basis: Basis::BASIS_LEDGER.into(),
                part: dep.part.clone(),
                evidence: vec![dep.id.clone()],
                ..Default::default()
            },
            take_home: set_line(dep.amount),
            ..Draft::default()
        };
        let mut gross = dep.amount;
        if let Some((p, tmpl)) = self.template(dep.date)
            && p.gross.set
        {
            gross = p.gross.v;
            d.origin.template = tmpl;
        }
        d.p = Pay {
            gross: set_line(gross),
            federal: set_line(USCents(0)),
            ss: set_line(USCents(0)),
            medicare: set_line(USCents(0)),
            ..Pay::default()
        };
        d.estimated = vec!["paycheck.gross".to_string()];
        let at = d.id.clone();
        self.items.push(d);
        self.notes.ask(new_question(
            model::item_id(&[&self.item_prefix(), "deposit", &dep.id]),
            at,
            format!(
                "What is the {} deposit on {} from {}?",
                dep.amount,
                dep.date.short(),
                self.m.name
            ),
            format!("It counts as {gross} of extra pay with nothing withheld until you say."),
            format!(
                "{}.parts[{}].from_deposits.deposits[{}].matches",
                self.path(),
                dep.part,
                dep.id
            ),
            &["A pay date", "Part of a paystub", "Not pay"],
        ));
    }

    /// Adds an ADJUSTMENT at the latest paystub with a year-to-date column,
    /// for the difference between that column and the items through its
    /// date: pay dates with neither a paystub nor a deposit, and anything
    /// the paystubs read differently.
    pub(super) fn year_to_date(&mut self) {
        let Some(at) = self.stubs.iter().rev().find(|s| s.has_ytd).cloned() else {
            return;
        };
        self.ytd_at = at.date;
        let through: Vec<usize> = self
            .made()
            .into_iter()
            .filter(|&i| !self.items[i].date.after(at.date))
            .collect();
        let got = self.sum_of(&through);
        let mut diff = Pay::default();
        let pick = |want: Line, have: Line| -> Line {
            if !want.set {
                return Line::default();
            }
            set_line(want.v - have.v)
        };
        diff.gross = pick(at.ytd.gross, got.gross);
        diff.federal = pick(at.ytd.federal, got.federal);
        diff.ss = pick(at.ytd.ss, got.ss);
        diff.medicare = pick(at.ytd.medicare, got.medicare);
        diff.roth401k = pick(at.ytd.roth401k, got.roth401k);
        diff.roth403b = pick(at.ytd.roth403b, got.roth403b);
        for (k, v) in &at.ytd.pretax {
            diff.set_pretax(*k, pick(*v, got.pretax_line(*k)));
        }
        for s in &at.ytd.states {
            let h = got.state(&s.state).unwrap_or_default();
            let i = diff.state_index(&s.state);
            diff.states[i].wages = pick(s.wages, h.wages);
            diff.states[i].withheld = pick(s.withheld, h.withheld);
        }
        let diff = diff.nonzero();
        if diff.empty() {
            return;
        }
        let id = format!("{}/adjustment", self.items[at.item].id);
        self.items.push(Draft {
            id,
            date: at.date,
            status: ItemStatus::ITEM_STATUS_MADE,
            kind: ItemKind::ITEM_KIND_ADJUSTMENT,
            origin: Origin {
                basis: Basis::BASIS_DOCUMENT.into(),
                part: at.part.clone(),
                evidence: vec![at.id.clone()],
                ..Default::default()
            },
            p: diff,
            ..Draft::default()
        });
    }
}

/// Lists every set line of copied figures, which a deposit's paycheck marks
/// as estimated.
pub(super) fn copied_paths(p: &Pay) -> Vec<String> {
    let mut out = Vec::new();
    let mut add = |l: Line, path: String| {
        if l.set {
            out.push(path);
        }
    };
    add(p.gross, "paycheck.gross".to_string());
    for (i, k) in super::pay::PRETAX_KINDS.iter().enumerate() {
        if let Some(l) = p.pretax.get(k) {
            add(*l, format!("paycheck.pretax[{i}].amount"));
        }
    }
    add(p.federal, "paycheck.federal_withheld".to_string());
    add(p.ss, "paycheck.social_security_withheld".to_string());
    add(p.medicare, "paycheck.medicare_withheld".to_string());
    add(p.roth401k, "paycheck.roth_401k".to_string());
    add(p.roth403b, "paycheck.roth_403b".to_string());
    for s in &p.states {
        add(
            s.wages,
            format!("paycheck.states[{}].state_income", s.state),
        );
        add(
            s.withheld,
            format!("paycheck.states[{}].tax_withheld", s.state),
        );
    }
    out
}
