//! The expected items: gaps, the rest of the year, a year total, one-offs
//! and a final payment. Ported from fill.go.

use std::collections::{HashMap, HashSet};

use money::{FineRate, USCents};
use proto::commonv1;
use proto::factsv1::{self, PaycheckKind};
use proto::scenariosv1::{
    __buffa::oneof::employer_default::Rest, __buffa::oneof::employer_part::Part, EmployerPart,
    ItemKind, ItemStatus, Origin, employer_part::ends::FinalPayment, origin::Basis,
};

use super::evidence::copied_paths;
use super::pay::{Line, PRETAX_KINDS, Pay, StateLine, pay_of, set_line};
use super::withhold::state_base;
use super::{Draft, Job, abs, item_kind, kind_word};
use crate::internal::civil::{self, Date};
use crate::model::{self, Error, new_question};

/// A PayRate part with the day it takes effect.
#[derive(Clone, Copy)]
pub(super) struct Period<'a> {
    part: &'a EmployerPart,
    /// As typed; zero when unset.
    from: Date,
    /// Clipped to the day after the evidence.
    start: Date,
}

/// Which line of a pay a year total spreads.
#[derive(Clone, Copy)]
enum Field {
    Gross,
    Pretax(i32),
    Roth401k,
    Roth403b,
    Federal,
    Ss,
    Medicare,
}

fn set_field(d: &mut Draft, f: Field, v: USCents) {
    match f {
        Field::Gross => d.p.gross = set_line(v),
        Field::Pretax(k) => d.p.set_pretax(k, set_line(v)),
        Field::Roth401k => d.p.roth401k = set_line(v),
        Field::Roth403b => d.p.roth403b = set_line(v),
        Field::Federal => d.p.federal = set_line(v),
        Field::Ss => d.p.ss = set_line(v),
        Field::Medicare => d.p.medicare = set_line(v),
    }
}

impl<'a> Job<'a> {
    /// Makes the expected items: the covered pay dates nothing explains,
    /// the pay dates after the evidence up to any end, then the one-offs
    /// and a final payment.
    pub(super) fn fill(&mut self) -> Result<(), Error> {
        self.resolve_end()?;
        self.gaps();
        let before = self.sum_all();
        let rest = self.rest_slots();
        let periods = self.periods()?;
        let mut drafts = Vec::new();
        for slot in rest {
            let Some(d) = self.expected(slot, &periods) else {
                continue;
            };
            drafts.push(self.items.len());
            self.items.push(d);
        }
        self.year_total(&drafts, &before)?;
        self.one_offs()
    }

    /// Fills each covered pay date, up to the latest evidence, that has
    /// neither a paystub nor a deposit and that no year-to-date column
    /// covers: an expected paycheck copying the template, marked estimated,
    /// so nothing is understated. The template is the latest regular
    /// paystub before it, else the first after it, else an assumed PayRate.
    /// A W-2 states the year, so a job with one has no gaps.
    fn gaps(&mut self) {
        if !self.w2s.is_empty() || self.latest.is_zero() {
            return;
        }
        let starts = civil::from_field(&self.m.starts).unwrap_or_default();
        for slot in self.slots.clone() {
            if slot.after(self.latest)
                || self.filled.contains_key(&slot.to_string())
                || (!self.ytd_at.is_zero() && !slot.after(self.ytd_at))
                || (!starts.is_zero() && slot.before(starts))
                || (!self.end.is_zero() && slot.after(self.end))
            {
                continue;
            }
            let Some((p, tmpl)) = self.template(slot) else {
                continue;
            };
            let mut state_diff = HashMap::new();
            for st in &p.states {
                state_diff.insert(st.state.clone(), st.wages.v - state_base(&st.state, &p));
            }
            let mut d = Draft {
                id: self.slot_id(slot),
                date: slot,
                status: ItemStatus::ITEM_STATUS_EXPECTED,
                kind: ItemKind::ITEM_KIND_PAYCHECK,
                estimated: copied_paths(&p),
                origin: Origin {
                    basis: Basis::BASIS_ASSUMED.into(),
                    template: tmpl,
                    ..Default::default()
                },
                compute_federal: !p.federal.set,
                compute_ss: !p.ss.set,
                compute_medicare: !p.medicare.set,
                compute_states: true,
                state_diff,
                p,
                ..Draft::default()
            };
            d.p.states = Vec::new();
            self.filled.insert(slot.to_string(), self.items.len());
            self.items.push(d);
        }
    }

    /// The first day the assumed parts fill: the day after the evidence,
    /// else the job's first day, else January 1.
    fn rest_start(&self) -> Date {
        if !self.latest.is_zero() {
            return self.latest.add_days(1);
        }
        if let Ok(s) = civil::from_field(&self.m.starts)
            && !s.is_zero()
        {
            return s;
        }
        self.ctx.year_start()
    }

    /// The pay dates after the evidence and up to any end. A W-2 states the
    /// year, so a job with one has none.
    fn rest_slots(&self) -> Vec<Date> {
        if !self.w2s.is_empty() {
            return Vec::new();
        }
        self.slots
            .iter()
            .copied()
            .filter(|s| {
                !(s.before(self.rest_start())
                    || (!self.end.is_zero() && s.after(self.end))
                    || self.filled.contains_key(&s.to_string()))
            })
            .collect()
    }

    /// Reads the job's end part. A known end with evidence after it counts
    /// the evidence and asks about the date; an assumed end on or before
    /// the latest evidence stands aside.
    fn resolve_end(&mut self) -> Result<(), Error> {
        let m = self.m;
        for part in &m.parts {
            match &part.part {
                Some(Part::Ended(e)) => {
                    let d = match civil::from_field(&e.last_paycheck) {
                        Ok(d) if !d.is_zero() => d,
                        _ => {
                            return Err(Error::Expansion(format!(
                                "{}.parts[{}].ended.last_paycheck is not a day",
                                self.path(),
                                part.id
                            )));
                        }
                    };
                    self.end = d;
                    if self.latest.after(d) && self.w2s.is_empty() {
                        self.notes.ask(new_question(
                            model::item_id(&[&self.item_prefix(), "ended", &part.id]),
                            self.item_prefix(),
                            format!(
                                "Your evidence from {} runs to {}, after the last paycheck on {}. When was the last paycheck?",
                                m.name,
                                self.latest.short(),
                                d.short()
                            ),
                            "The evidence after it counts.",
                            format!("{}.parts[{}].ended.last_paycheck", self.path(), part.id),
                            &[],
                        ));
                    }
                }
                Some(Part::Ends(e)) => {
                    let d = match civil::from_field(&e.last_paycheck) {
                        Ok(d) if !d.is_zero() => d,
                        _ => {
                            return Err(Error::Expansion(format!(
                                "{}.parts[{}].ends.last_paycheck is not a day",
                                self.path(),
                                part.id
                            )));
                        }
                    };
                    // Evidence after an assumed end makes it stand aside.
                    // cmd/taxyear can't see when the end was added, so an
                    // end on the day of the latest evidence still counts.
                    if d.before(self.latest) {
                        self.stand_aside(
                            &part.id,
                            format!(
                                "Your {} evidence from {} is after this job's planned end on {}.",
                                self.latest.short(),
                                m.name,
                                d.short()
                            ),
                            &["Record the job as ended instead"],
                        );
                        continue;
                    }
                    self.end = d;
                    self.final_payment(&part.id, d, e.r#final.as_option());
                }
                _ => {}
            }
        }
        Ok(())
    }

    /// Records that an assumed part counts for nothing, and why.
    fn stand_aside(&mut self, part: &str, why: String, answers: &[&str]) {
        let mut all = answers.to_vec();
        all.push("Remove it");
        self.notes.ask(new_question(
            model::item_id(&[&self.item_prefix(), "stands-aside", part]),
            self.item_prefix(),
            why,
            "This change counts for nothing until it is changed or removed.",
            format!("{}.parts[{}]", self.path(), part),
            &all,
        ));
    }

    /// Returns the PayRate parts that count, each from the day it takes
    /// effect. An unset start is the day after the evidence, an earlier one
    /// is clipped to it, and of several clipped to one day the
    /// latest-started counts; the others stand aside.
    ///
    /// Go groups the parts in a map, so the order its stand-aside questions
    /// are asked in across days is random; here it is the order of each
    /// day's first part.
    fn periods(&mut self) -> Result<Vec<Period<'a>>, Error> {
        let m = self.m;
        let mut ps = Vec::new();
        for part in &m.parts {
            let Some(Part::PayRate(r)) = &part.part else {
                continue;
            };
            let from = civil::from_field(&r.from).map_err(|e| {
                Error::Expansion(format!(
                    "{}.parts[{}].pay_rate.from: {e}",
                    self.path(),
                    part.id
                ))
            })?;
            let mut start = self.rest_start();
            if !from.is_zero() && from.after(start) {
                start = from;
            }
            ps.push(Period { part, from, start });
        }
        let mut by_start: Vec<(Date, Vec<Period<'a>>)> = Vec::new();
        for p in ps {
            match by_start.iter_mut().find(|(s, _)| *s == p.start) {
                Some((_, g)) => g.push(p),
                None => by_start.push((p.start, vec![p])),
            }
        }
        let mut out = Vec::new();
        for (_, mut group) in by_start {
            group.sort_by_key(|p| started(p));
            let last = group.len() - 1;
            for p in &group[..last] {
                self.stand_aside(
                    &p.part.id,
                    format!(
                        "Your pay change from {} at {} starts before the evidence ends, and a later one replaces it.",
                        started(p).short(),
                        m.name
                    ),
                    &[],
                );
            }
            out.push(group[last]);
        }
        out.sort_by_key(|p| p.start);
        Ok(out)
    }

    /// The item that fills one pay date after the evidence: the period
    /// part in effect on it, else the default part.
    fn expected(&mut self, slot: Date, periods: &[Period<'a>]) -> Option<Draft> {
        let mut d = Draft {
            id: self.slot_id(slot),
            date: slot,
            status: ItemStatus::ITEM_STATUS_EXPECTED,
            kind: ItemKind::ITEM_KIND_PAYCHECK,
            plan_slot: true,
            compute_ss: true,
            compute_medicare: true,
            compute_states: true,
            origin: Origin {
                basis: Basis::BASIS_ASSUMED.into(),
                ..Default::default()
            },
            ..Draft::default()
        };
        let active = periods.iter().rfind(|p| !p.start.after(slot));
        if let Some(active) = active {
            d.origin.part = active.part.id.clone();
            if let Some(Part::PayRate(r)) = &active.part.part {
                typed(&mut d, &r.paycheck);
            }
            return Some(d);
        }
        let def = &*self.m.default_part;
        d.origin.part = def.id.clone();
        match &def.rest {
            Some(Rest::EachPaycheck(f)) => typed(&mut d, f),
            _ if self.regular.is_none() && self.deposits.is_empty() && self.has_year_total() => {
                // Nothing to repeat: the year total fills the pay date, and
                // withholding it doesn't state is computed.
                d.compute_federal = true;
            }
            Some(Rest::SameAsLatest(_)) => {
                if !self.same_as_latest(&mut d) {
                    return None;
                }
            }
            None => {
                self.notes.ask(new_question(
                    model::item_id(&[&self.item_prefix(), "default"]),
                    self.item_prefix(),
                    format!("What will {} pay for the rest of the year?", self.m.name),
                    "Nothing is counted for its pay dates after the evidence until you say.",
                    format!("{}.default_part", self.path()),
                    &[],
                ));
                return None;
            }
        }
        Some(d)
    }

    /// Fills an item from the latest regular paystub's current column. Its
    /// states are worked out from where the work is done, never the
    /// paystub's; its Social Security and Medicare tax at their rates.
    fn same_as_latest(&mut self, d: &mut Draft) -> bool {
        let Some(regular) = self.regular_stub().cloned() else {
            if let Some(last) = self.deposits.last().cloned() {
                d.p = Pay {
                    gross: set_line(last.amount),
                    ..Pay::default()
                };
                d.estimated = vec!["paycheck.gross".to_string()];
                d.compute_federal = true;
                self.notes.ask(new_question(
                    model::item_id(&[&self.item_prefix(), "no-paystub"]),
                    self.item_prefix(),
                    format!(
                        "What is gross pay on each paycheck from {}? There is no paystub to repeat.",
                        self.m.name
                    ),
                    format!(
                        "Each pay date counts the latest deposit, {}, as gross pay until you say.",
                        last.amount
                    ),
                    format!("{}.default_part.each_paycheck", self.path()),
                    &[],
                ));
                return true;
            }
            self.notes.ask(new_question(
                model::item_id(&[&self.item_prefix(), "no-paystub"]),
                self.item_prefix(),
                format!("What is gross pay on each paycheck from {}?", self.m.name),
                "Nothing is counted for its pay dates until you say.",
                format!("{}.default_part.each_paycheck", self.path()),
                &[],
            ));
            return false;
        };
        d.p = regular.this.clone();
        d.p.net = Line::default();
        d.compute_federal = !d.p.federal.set;
        d.state_diff = HashMap::new();
        for s in &d.p.states {
            d.state_diff.insert(
                s.state.clone(),
                s.wages.v - state_base(&s.state, &regular.this),
            );
        }
        d.p.states = Vec::new();
        true
    }

    /// Reports a YearTotal part.
    fn has_year_total(&self) -> bool {
        self.m
            .parts
            .iter()
            .any(|p| matches!(p.part, Some(Part::YearTotal(_))))
    }

    /// Applies a YearTotal part: each line it states is the year less the
    /// evidence, shared equally across the pay dates after the evidence. It
    /// stands aside once a paystub newer than the one it was typed against
    /// is read.
    fn year_total(&mut self, drafts: &[usize], before: &Pay) -> Result<(), Error> {
        let m = self.m;
        let Some(part) = m
            .parts
            .iter()
            .rfind(|p| matches!(p.part, Some(Part::YearTotal(_))))
        else {
            return Ok(());
        };
        let Some(Part::YearTotal(yt)) = &part.part else {
            return Ok(());
        };
        let typed = civil::from_field(&yt.typed_against).map_err(|e| {
            Error::Expansion(format!(
                "{}.parts[{}].year_total.typed_against: {e}",
                self.path(),
                part.id
            ))
        })?;
        for i in 0..self.stubs.len() {
            let date = self.stubs[i].date;
            if typed.is_zero() || date.after(typed) {
                self.stand_aside(
                    &part.id,
                    format!(
                        "Your total for the year from {} was typed before your {} paystub arrived.",
                        m.name,
                        date.short()
                    ),
                    &["Type it again"],
                );
                return Ok(());
            }
        }
        self.share_year(part, &pay_of(&yt.year), before, drafts);
        Ok(())
    }

    /// A state's line on the pay dates the evidence covers that no paystub
    /// fills, whose state lines are worked out with the withholding after
    /// the year total is shared: what they will be, from a copy.
    fn gap_state(&mut self, state: &str) -> StateLine {
        let mut out = StateLine {
            state: state.to_string(),
            ..StateLine::default()
        };
        for i in 0..self.items.len() {
            let d = &self.items[i];
            if d.status != ItemStatus::ITEM_STATUS_EXPECTED
                || !d.compute_states
                || d.date.after(self.latest)
            {
                continue;
            }
            let mut c = d.clone();
            self.states(&mut c);
            if let Some(l) = c.p.state(state) {
                out.wages = out.wages.plus(l.wages);
                out.withheld = out.withheld.plus(l.withheld);
            }
        }
        out
    }

    /// Records that a YearTotal part states the year figure at path, under
    /// the job's projection.
    fn year_total_working(&mut self, path: &str, part: &EmployerPart) {
        let at = format!("projection.employers[{}].{}", self.m.id, path);
        self.working(&at).parts = vec![part.id.clone()];
    }

    /// The job's working at path, made when first asked for.
    pub(super) fn working(&mut self, path: &str) -> &mut proto::scenariosv1::Working {
        let i = match self.workings.iter().position(|w| w.path == path) {
            Some(i) => i,
            None => {
                self.workings.push(proto::scenariosv1::Working {
                    path: path.to_string(),
                    ..Default::default()
                });
                self.workings.len() - 1
            }
        };
        &mut self.workings[i]
    }

    /// The item that carries what a YearTotal states and no pay date can:
    /// an expected adjustment on December 31, whose figures are fixed. It is
    /// made when first asked for.
    fn left_over(&mut self, part: &EmployerPart, adj: &mut Option<usize>) -> usize {
        if let Some(i) = *adj {
            return i;
        }
        let i = self.items.len();
        self.items.push(Draft {
            id: model::item_id(&[&self.item_prefix(), "year-total", "adjustment"]),
            date: self.ctx.year_end(),
            status: ItemStatus::ITEM_STATUS_EXPECTED,
            kind: ItemKind::ITEM_KIND_ADJUSTMENT,
            origin: Origin {
                basis: Basis::BASIS_ASSUMED.into(),
                part: part.id.clone(),
                ..Default::default()
            },
            typed_year: true,
            ..Draft::default()
        });
        *adj = Some(i);
        i
    }

    /// Shares one line a YearTotal states, less the evidence, across the
    /// drafts, or puts it on the adjustment when there are none.
    #[allow(clippy::too_many_arguments)]
    fn spread(
        &mut self,
        part: &EmployerPart,
        drafts: &[usize],
        adj: &mut Option<usize>,
        want: Line,
        have: Line,
        field: Field,
        path: &str,
    ) {
        if !want.set {
            return;
        }
        if path != "pretax" {
            self.year_total_working(&format!("year.items.{path}"), part);
        }
        let mut left = want.v - have.v;
        if left.0 < 0 {
            self.notes.ask(new_question(
                model::item_id(&[&self.item_prefix(), "year-total", path]),
                self.item_prefix(),
                format!(
                    "Your total for the year from {} is less than so far, for {}.",
                    self.m.name, path
                ),
                "Nothing more is counted for it this year.",
                format!("{}.parts[{}].year_total.year", self.path(), part.id),
                &[],
            ));
            left = USCents(0);
        }
        if drafts.is_empty() {
            let i = self.left_over(part, adj);
            set_field(&mut self.items[i], field, left);
            return;
        }
        for (i, v) in model::share(left, drafts.len() as i64)
            .into_iter()
            .enumerate()
        {
            set_field(&mut self.items[drafts[i]], field, v);
        }
    }

    /// Shares each line a YearTotal states, less the evidence, across the
    /// drafts. Withholding it doesn't state is computed. With no drafts, the
    /// lines go on one adjustment.
    fn share_year(&mut self, part: &EmployerPart, year: &Pay, so_far: &Pay, drafts: &[usize]) {
        let mut adj = None;
        self.spread(
            part,
            drafts,
            &mut adj,
            year.gross,
            so_far.gross,
            Field::Gross,
            "gross",
        );
        for k in PRETAX_KINDS {
            self.spread(
                part,
                drafts,
                &mut adj,
                year.pretax_line(k),
                so_far.pretax_line(k),
                Field::Pretax(k),
                "pretax",
            );
        }
        self.spread(
            part,
            drafts,
            &mut adj,
            year.roth401k,
            so_far.roth401k,
            Field::Roth401k,
            "roth_401k",
        );
        self.spread(
            part,
            drafts,
            &mut adj,
            year.roth403b,
            so_far.roth403b,
            Field::Roth403b,
            "roth_403b",
        );
        self.spread(
            part,
            drafts,
            &mut adj,
            year.federal,
            so_far.federal,
            Field::Federal,
            "federal_withheld",
        );
        self.spread(
            part,
            drafts,
            &mut adj,
            year.ss,
            so_far.ss,
            Field::Ss,
            "social_security_withheld",
        );
        self.spread(
            part,
            drafts,
            &mut adj,
            year.medicare,
            so_far.medicare,
            Field::Medicare,
            "medicare_withheld",
        );
        let mut held = HashSet::new();
        let default_id = self.m.default_part.id.clone();
        for &i in drafts {
            let p = self.items[i].origin.part.clone();
            if p != part.id && p != default_id && !held.contains(&p) {
                held.insert(p.clone());
                self.notes.ask(new_question(
                    model::item_id(&[&self.item_prefix(), "year-total", "holds", &p]),
                    self.item_prefix(),
                    format!(
                        "Your total for the year from {} holds over your pay change: it moves only the lines the total doesn't state.",
                        self.m.name
                    ),
                    "The typed total counts until you type it again.",
                    format!("{}.parts[{}].year_total.year", self.path(), part.id),
                    &["Type it again"],
                ));
            }
            let d = &mut self.items[i];
            d.origin.part = part.id.clone();
            d.typed_year = true;
            d.compute_federal = !year.federal.set;
            d.compute_ss = !year.ss.set;
            d.compute_medicare = !year.medicare.set;
        }
        self.share_states(part, year, so_far, drafts, &mut adj);
    }

    /// Shares each state line a YearTotal states, less the evidence, as
    /// typed state lines, which win. A state's line is shared over the
    /// drafts whose work is done in that state, so each paycheck's state
    /// follows its date. California's wages are box 1 and the payroll HSA,
    /// as its year's box is (see totals), so only its withholding is typed.
    /// A line with no draft in its state goes on the adjustment.
    fn share_states(
        &mut self,
        part: &EmployerPart,
        year: &Pay,
        so_far: &Pay,
        drafts: &[usize],
        adj: &mut Option<usize>,
    ) {
        for s in &year.states {
            if s.withheld.set {
                self.year_total_working(
                    &format!("year.items.states[{}].tax_withheld", s.state),
                    part,
                );
            }
            if s.wages.set && s.state != "CA" {
                self.year_total_working(
                    &format!("year.items.states[{}].state_income", s.state),
                    part,
                );
            }
            let within: Vec<usize> = drafts
                .iter()
                .copied()
                .filter(|&i| {
                    self.work_states(self.items[i].date)
                        .iter()
                        .any(|w| w.state == s.state)
                })
                .collect();
            let mut have = so_far.state(&s.state).unwrap_or_default();
            let gap = self.gap_state(&s.state);
            have.wages = have.wages.plus(gap.wages);
            have.withheld = have.withheld.plus(gap.withheld);
            if within.is_empty() {
                let mut l = StateLine {
                    state: s.state.clone(),
                    ..StateLine::default()
                };
                if s.wages.set && s.state != "CA" {
                    l.wages = set_line(USCents(0).max(s.wages.v - have.wages.v));
                }
                if s.withheld.set {
                    l.withheld = set_line(USCents(0).max(s.withheld.v - have.withheld.v));
                }
                if l.wages.set || l.withheld.set {
                    let a = self.left_over(part, adj);
                    self.items[a].p.states.push(l);
                }
                continue;
            }
            let n = within.len() as i64;
            let wages = (s.wages.set && s.state != "CA")
                .then(|| model::share(USCents(0).max(s.wages.v - have.wages.v), n));
            let withheld = s
                .withheld
                .set
                .then(|| model::share(USCents(0).max(s.withheld.v - have.withheld.v), n));
            for (i, &d) in within.iter().enumerate() {
                let mut l = StateLine {
                    state: s.state.clone(),
                    ..StateLine::default()
                };
                if let Some(w) = &wages {
                    l.wages = set_line(w[i]);
                }
                if let Some(w) = &withheld {
                    l.withheld = set_line(w[i]);
                }
                let d = &mut self.items[d];
                with_typed(&mut d.typed_states, l);
            }
        }
    }

    /// Adds each planned bonus or vest. Evidence of a bonus or vest within
    /// 31 days of one is asked about, "Is this the one you planned?"; until
    /// it is answered both count.
    fn one_offs(&mut self) -> Result<(), Error> {
        let m = self.m;
        for part in &m.parts {
            let Some(Part::OneOff(o)) = &part.part else {
                continue;
            };
            let date = match civil::from_field(&o.date) {
                Ok(d) if !d.is_zero() => d,
                _ => {
                    return Err(Error::Expansion(format!(
                        "{}.parts[{}].one_off.date is not a day",
                        self.path(),
                        part.id
                    )));
                }
            };
            if self.known_from(part, date) {
                continue;
            }
            let mut kind = o.kind;
            if kind != PaycheckKind::PAYCHECK_KIND_VEST {
                kind = PaycheckKind::PAYCHECK_KIND_BONUS.into();
            }
            self.items.push(Draft {
                id: model::item_id(&[
                    &self.item_prefix(),
                    &date.to_string(),
                    kind_word(kind),
                    &part.id,
                ]),
                date,
                status: ItemStatus::ITEM_STATUS_EXPECTED,
                kind: item_kind(kind),
                origin: Origin {
                    basis: Basis::BASIS_ASSUMED.into(),
                    part: part.id.clone(),
                    ..Default::default()
                },
                p: Pay {
                    gross: set_line(model::value(&o.amount)),
                    ..Pay::default()
                },
                compute_federal: true,
                compute_ss: true,
                compute_medicare: true,
                compute_states: true,
                supplemental: true,
                federal_rate: fine_rate(o.federal_rate.as_option()),
                state_rate: fine_rate(o.state_rate.as_option()),
                ..Draft::default()
            });
        }
        Ok(())
    }

    /// Reports whether a planned one-off's answer says a paystub is it, and
    /// otherwise asks about each bonus or vest paystub within 31 days.
    fn known_from(&mut self, part: &EmployerPart, date: Date) -> bool {
        let Some(Part::OneOff(o)) = &part.part else {
            return false;
        };
        let actual = &*o.actual;
        if !actual.is.is_empty() {
            return true;
        }
        let not: HashSet<&str> = actual.is_not.iter().map(String::as_str).collect();
        for i in 0..self.stubs.len() {
            let s = &self.stubs[i];
            if s.kind != PaycheckKind::PAYCHECK_KIND_BONUS
                && s.kind != PaycheckKind::PAYCHECK_KIND_VEST
            {
                continue;
            }
            if not.contains(s.id.as_str()) || abs(s.date.days_from(date)) > 31 {
                continue;
            }
            let q = new_question(
                model::item_id(&[&self.item_prefix(), "planned", &part.id, &s.id]),
                self.items[s.item].id.clone(),
                format!(
                    "Is your {} {} paystub from {} the one you planned for {}?",
                    s.date.short(),
                    kind_word(s.kind),
                    self.m.name,
                    date.short()
                ),
                "Both count until you say.",
                format!("{}.parts[{}].one_off.actual", self.path(), part.id),
                &["Yes", "No"],
            );
            self.notes.ask(q);
        }
        false
    }

    /// Adds the payment an assumed end pays with the last paycheck, withheld
    /// as given.
    fn final_payment(&mut self, part: &str, on: Date, f: Option<&FinalPayment>) {
        let Some(f) = f else {
            return;
        };
        if model::value(&f.amount).0 == 0 {
            return;
        }
        let mut d = Draft {
            id: model::item_id(&[&self.item_prefix(), &on.to_string(), "final", part]),
            date: on,
            status: ItemStatus::ITEM_STATUS_EXPECTED,
            kind: ItemKind::ITEM_KIND_FINAL,
            origin: Origin {
                basis: Basis::BASIS_ASSUMED.into(),
                part: part.to_string(),
                ..Default::default()
            },
            p: Pay {
                gross: set_line(model::value(&f.amount)),
                federal: set_line(model::value(&f.federal_withheld)),
                ..Pay::default()
            },
            compute_ss: true,
            compute_medicare: true,
            compute_states: true,
            ..Draft::default()
        };
        for st in self.work_states(on) {
            d.typed_states.push(StateLine {
                state: st.state,
                withheld: set_line(model::scale(
                    model::value(&f.state_withheld),
                    st.days,
                    st.total,
                )),
                ..StateLine::default()
            });
        }
        self.items.push(d);
    }
}

/// The day a period part says it starts: its from, or for an unset one the
/// day it is clipped to, which is the latest possible.
fn started(p: &Period<'_>) -> Date {
    if p.from.is_zero() {
        return p.start;
    }
    p.from
}

/// Fills an item from typed figures: a pre-tax line left unset is none, and
/// a withholding line left unset is computed.
fn typed(d: &mut Draft, f: &factsv1::PayFigures) {
    d.p = pay_of(f);
    d.p.net = Line::default();
    d.compute_federal = !d.p.federal.set;
    d.compute_ss = !d.p.ss.set;
    d.compute_medicare = !d.p.medicare.set;
    d.typed_states = std::mem::take(&mut d.p.states);
}

/// lines with l in place of a line for its state, keeping that line's
/// figures l leaves unset: a YearTotal's line wins over a pay change's.
fn with_typed(lines: &mut Vec<StateLine>, mut l: StateLine) {
    for x in lines.iter_mut() {
        if x.state == l.state {
            if !l.wages.set {
                l.wages = x.wages;
            }
            if !l.withheld.set {
                l.withheld = x.withheld;
            }
            *x = l;
            return;
        }
    }
    lines.push(l);
}

/// Reads a percentage as a rate in millionths, or none when unset.
fn fine_rate(q: Option<&commonv1::Quantity>) -> Option<FineRate> {
    let q = q?;
    let mut r = q.units.wrapping_mul(10_000);
    for _ in 0..q.scale {
        r /= 10;
    }
    Some(FineRate(r))
}

/// Applies a rate in millionths, rounding half away from zero.
pub(super) fn of_fine(r: FineRate, a: USCents) -> USCents {
    model::scale(a, USCents(r.0), USCents(1_000_000))
}
