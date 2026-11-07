//! Maxing out a person's workplace plan across their jobs. Ported from
//! maxout.go.

use money::{Rate, USCents};
use proto::scenariosv1::{
    __buffa::oneof::retirement_part::Part as RetirementKind, EmployerModel, ItemStatus,
    RetirementPart, RetirementPlan,
};

use super::pay::{K401, K403B, Kind, Pay, set_line};
use super::withhold::state_base;
use super::{Draft, Job, Notes};
use crate::internal::civil;
use crate::model::{self, Context, Error, new_question};
use crate::payroll;

/// One expected paycheck max-out may set, with what it can take: the job
/// and the item by index.
struct PlanSlot {
    job: usize,
    item: usize,
    room: USCents,
    v: USCents,
}

/// A paycheck's plan line and its catch-up part.
#[derive(Clone, Copy, Debug, Default)]
pub(super) struct PlanShare {
    pub(super) v: USCents,
    pub(super) catch: USCents,
}

/// Applies a person's MaxOutWorkplacePlan or WorkplacePlanTotal part to
/// their jobs, as the models doc's "Maxing out a workplace plan" says:
///
///  1. The amount is the § 402(g) limit plus the § 414(v) catch-up for the
///     person's age, or the typed total held to that, less every job's
///     deferrals so far, pre-tax and Roth, on pay dates through as_of.
///  2. It is shared equally across the person's expected regular paychecks
///     after as_of at every job with a 401(k) or 403(b), or whose plan isn't
///     said, whose share counts as Roth and is asked about. None pays out
///     less than $0 after its withholding; what one can't take goes to the
///     others.
///  3. Each job keeps its latest paystub's pre-tax and Roth split. The
///     catch-up is Roth when last year's box 3 from that employer exceeded
///     $150,000; without that W-2 it stays pre-tax and the result asks.
///  4. Box 1 falls by the pre-tax share. Computed withholding is computed
///     again by [`Job::projection`]; typed withholding moves at the
///     paycheck's own rate.
///
/// One-offs and final payments are never set. jobs may hold other people's
/// jobs, which are left alone. It returns what it asked.
pub fn max_out(
    ctx: &Context,
    person: &str,
    part: &RetirementPart,
    jobs: &mut [Job<'_>],
) -> Result<Notes, Error> {
    let mut notes = Notes::default();
    let mut mine = Vec::new();
    // typed is the deferrals on paychecks to come that a YearTotal part
    // fills, which max-out can't change.
    let (mut so_far, mut typed) = (USCents(0), USCents(0));
    for (i, j) in jobs.iter().enumerate() {
        if j.person() != person {
            continue;
        }
        if j.done {
            return Err(Error::Expansion(
                "employer: MaxOut after Projection".to_string(),
            ));
        }
        so_far += j.sum_of(&j.covered()).deferrals();
        typed += j.sum_of(&j.typed_ahead()).deferrals();
        if has_plan(j.m) {
            mine.push(i);
        }
        if j.m.retirement_plan == RetirementPlan::RETIREMENT_PLAN_UNSPECIFIED {
            // A job whose plan isn't said takes its share with no pre-tax
            // effect, as Roth, so the assumption doesn't lower the tax.
            mine.push(i);
            notes.ask(new_question(
                model::item_id(&[&j.item_prefix(), "retirement-plan"]),
                j.item_prefix(),
                format!(
                    "Does {} have a 401(k) or 403(b), and are your deferrals there pre-tax or Roth?",
                    j.m.name
                ),
                "Its paychecks to come take a share of your workplace plan, counted as Roth, with no pre-tax effect, until you say.",
                format!("{}.retirement_plan", j.path()),
                &[],
            ));
        }
    }
    let (age, known) = ctx.age_at_year_end(person);
    let (base, mut catch_up) = match payroll::plan_limit(ctx.year(), age) {
        Ok(l) => l,
        Err(e) if e.is(&ustaxcalc::Error::NoTable) => {
            notes.leave_out(format!(
                "Maxing out the workplace plan, which needs the § 402(g) limit for {}",
                ctx.year()
            ));
            return Ok(notes);
        }
        Err(e) => return Err(Error::Expansion(e.to_string())),
    };
    if !known {
        catch_up = USCents(0);
        notes.ask(new_question(
            format!("retirement/{person}/date-of-birth"),
            "model.retirement",
            "What is your date of birth? The workplace plan limit is higher from 50.",
            "No catch-up is counted until you say.",
            format!("facts.people[{person}].date_of_birth"),
            &[],
        ));
    }
    let mut target = base + catch_up;
    if let Some(RetirementKind::WorkplacePlanTotal(t)) = &part.part {
        target = target.min(model::value(&t.total));
    }
    let zero = USCents(0);
    // What max-out would put on the typed paychecks too is kept for their
    // workings' automatic.
    let all = zero.max(target - so_far);
    let slots = share_out(jobs, &mine, all, true);
    for (s, catch) in each_share(&slots, all, zero.max(all - zero.max(base - so_far))) {
        let j = &mut jobs[s.job];
        if j.items[s.item].typed_year {
            let id = j.items[s.item].id.clone();
            j.plan_typed.insert(id, PlanShare { v: s.v, catch });
        }
    }
    let left = zero.max(target - so_far - typed);
    let slots = share_out(jobs, &mine, left, false);
    for (s, catch) in each_share(
        &slots,
        left,
        zero.max(left - zero.max(base - so_far - typed)),
    ) {
        let j = &mut jobs[s.job];
        j.with_item(s.item, |j, d| j.set_plan(d, s.v, catch, &mut notes));
    }
    Ok(notes)
}

/// Each slot and its catch-up share, cut from the running total so the
/// shares add to the whole catch-up to the cent.
fn each_share(slots: &[PlanSlot], left: USCents, catch_left: USCents) -> Vec<(&PlanSlot, USCents)> {
    let (mut through, mut catch_so_far) = (USCents(0), USCents(0));
    let mut out = Vec::new();
    for s in slots {
        through += s.v;
        let catch = model::scale(through, catch_left, left) - catch_so_far;
        catch_so_far += catch;
        out.push((s, catch));
    }
    out
}

fn has_plan(m: &EmployerModel) -> bool {
    m.retirement_plan == RetirementPlan::RETIREMENT_PLAN_401K
        || m.retirement_plan == RetirementPlan::RETIREMENT_PLAN_403B
}

/// Shares left equally across the jobs' plan slots, smallest room first, so
/// a paycheck that can't take an equal share passes the rest on. The
/// paychecks a YearTotal part fills are left out unless typed is set.
fn share_out(jobs: &[Job<'_>], mine: &[usize], mut left: USCents, typed: bool) -> Vec<PlanSlot> {
    let mut slots = Vec::new();
    for &ji in mine {
        let j = &jobs[ji];
        for (i, d) in j.items.iter().enumerate() {
            if d.plan_slot
                && d.status == ItemStatus::ITEM_STATUS_EXPECTED
                && d.date.after(j.ctx.as_of())
                && (typed || !d.typed_year)
            {
                slots.push(PlanSlot {
                    job: ji,
                    item: i,
                    room: j.room(d),
                    v: USCents(0),
                });
            }
        }
    }
    let mut order: Vec<usize> = (0..slots.len()).collect();
    order.sort_by_key(|&i| slots[i].room);
    let n = order.len();
    for (k, &i) in order.iter().enumerate() {
        let share = left / (n - k) as i64;
        let s = &mut slots[i];
        s.v = s.room.min(share);
        left -= s.v;
    }
    slots
}

/// A state a paycheck is worked in, its share, and the rate it is withheld
/// at: the latest paystub's withholding over its wages there.
struct WorkRate {
    state: String,
    days: USCents,
    total: USCents,
    withheld: USCents,
    of: USCents,
}

/// How a job's catch-up is deferred.
#[derive(Clone, Copy, PartialEq, Eq)]
enum CatchUpRule {
    /// Follows the latest paystub's pre-tax and Roth split.
    Split,
    /// All Roth.
    Roth,
    /// None allowed: it must be Roth and the plan has no Roth option,
    /// § 1.414(v)-2(b)(2).
    None,
}

impl<'a> Job<'a> {
    /// The paychecks after as_of and the evidence that a YearTotal part
    /// fills.
    fn typed_ahead(&self) -> Vec<usize> {
        let cut = civil::later(self.latest, self.ctx.as_of());
        self.typed_year()
            .into_iter()
            .filter(|&i| self.items[i].date.after(cut))
            .collect()
    }

    /// The items up to the latest evidence or as_of, whichever is later:
    /// the made ones and the pay dates already past, whose deferrals are so
    /// far and which max-out can't change.
    fn covered(&self) -> Vec<usize> {
        let cut = civil::later(self.latest, self.ctx.as_of());
        (0..self.items.len())
            .filter(|&i| {
                let d = &self.items[i];
                d.status == ItemStatus::ITEM_STATUS_MADE || !d.date.after(cut)
            })
            .collect()
    }

    /// The most a paycheck's plan line can take and still pay out nothing
    /// less than $0. Social Security, Medicare, typed state withholding and
    /// a set amount per paycheck don't fall with a deferral. Typed federal
    /// and state withholding at the paystub's rate fall with taxable pay,
    /// at rate r, so taxable pay T must cover the fixed amounts F and r of
    /// itself: T is at least F / (1 - r).
    fn room(&self, d: &Draft) -> USCents {
        let mut fixed = self.fixed_out(d);
        let mut num = USCents(0);
        let den = d.p.taxable();
        if !d.compute_federal {
            num += d.p.federal.v;
        }
        // A state's wages are taxable pay and a part a deferral doesn't
        // move, such as the payroll HSA California taxes; the rate on that
        // part is fixed.
        if den.0 > 0 {
            for w in self.work_rates(d) {
                num += model::scale(model::scale(den, w.days, w.total), w.withheld, w.of);
                let extra = state_base(&w.state, &d.p) - d.p.taxable()
                    + d.state_diff.get(&w.state).copied().unwrap_or_default();
                fixed += model::scale(model::scale(extra, w.days, w.total), w.withheld, w.of);
            }
        }
        let mut least = fixed;
        let zero = USCents(0);
        if self.split().0.0 == 0 {
            // A Roth deferral lowers no withholding.
            return zero.max(d.p.gross.v - self.other_pretax(&d.p) - fixed - num);
        }
        if den.0 <= 0 {
        } else if num >= den {
            return zero;
        } else if num.0 > 0 {
            least = model::scale(fixed, den, den - num) + USCents(1);
        }
        zero.max(d.p.gross.v - self.other_pretax(&d.p) - least)
    }

    /// What a paycheck pays out that a deferral doesn't lower: Social
    /// Security and Medicare, typed or at 6.2% and 1.45% of the pay they are
    /// on, typed state withholding, and the employer's state withholding per
    /// paycheck.
    fn fixed_out(&self, d: &Draft) -> USCents {
        let (mut ss, mut medicare) = (d.p.ss.v, d.p.medicare.v);
        if !d.p.ss.set || d.compute_ss {
            ss = Rate(620).of(d.p.fica());
        }
        if !d.p.medicare.set || d.compute_medicare {
            medicare = Rate(145).of(d.p.fica());
        }
        let mut out = ss + medicare;
        for s in &d.typed_states {
            out += s.withheld.v;
        }
        for s in &self.m.state_withholding {
            out += model::value(&s.per_paycheck);
        }
        out
    }

    /// The states a paycheck with no typed state line is withheld in at the
    /// latest paystub's rate.
    fn work_rates(&self, d: &Draft) -> Vec<WorkRate> {
        let Some(regular) = self.regular_stub() else {
            return Vec::new();
        };
        if !d.typed_states.is_empty() || !self.m.state_withholding.is_empty() {
            return Vec::new();
        }
        let mut out = Vec::new();
        for w in self.work_states(d.date) {
            if let Some(l) = regular.this.state(&w.state)
                && l.wages.v.0 > 0
            {
                out.push(WorkRate {
                    state: w.state,
                    days: w.days,
                    total: w.total,
                    withheld: l.withheld.v,
                    of: l.wages.v,
                });
            }
        }
        out
    }

    /// The job's plan lines: its pre-tax kind, and whether Roth deferrals go
    /// on the 403(b) line.
    fn plan_kinds(&self) -> (Kind, bool) {
        if self.m.retirement_plan == RetirementPlan::RETIREMENT_PLAN_403B {
            return (K403B, true);
        }
        (K401, false)
    }

    /// A paycheck's pre-tax lines other than the plan's.
    fn other_pretax(&self, p: &Pay) -> USCents {
        let (k, _) = self.plan_kinds();
        p.pretax_total() - p.pretax_line(k).v
    }

    /// Sets one paycheck's plan lines to v, catch of it catch-up, split as
    /// the latest paystub splits pre-tax and Roth.
    pub(super) fn set_plan(
        &self,
        d: &mut Draft,
        mut v: USCents,
        catch: USCents,
        notes: &mut Notes,
    ) {
        let (kind, b403) = self.plan_kinds();
        let (pre, roth) = self.split();
        let regular = v - catch;
        let mut pre_tax = model::scale(regular, pre, pre + roth);
        if catch.0 > 0 {
            match self.catch_up(notes) {
                // All of the catch-up is Roth.
                CatchUpRule::Roth => {}
                CatchUpRule::None => v -= catch,
                CatchUpRule::Split => pre_tax += model::scale(catch, pre, pre + roth),
            }
        }
        let roth_v = v - pre_tax;
        let before = d.p.taxable();
        d.p.set_pretax(kind, set_line(pre_tax));
        if b403 {
            d.p.roth403b = set_line(roth_v);
        } else {
            d.p.roth401k = set_line(roth_v);
        }
        d.estimate(&format!(
            "paycheck.pretax[{}].amount",
            pretax_index(&d.p, kind)
        ));
        let after = d.p.taxable();
        if before == after {
            return;
        }
        if !d.compute_federal {
            d.p.federal = set_line(model::scale(d.p.federal.v, after, before));
        }
        for s in &mut d.typed_states {
            if s.withheld.set {
                s.withheld = set_line(model::scale(s.withheld.v, after, before));
            }
            if s.wages.set {
                s.wages = set_line(s.wages.v + after - before);
            }
        }
    }

    /// The latest paystub's plan lines, pre-tax and Roth, which max-out
    /// keeps; all pre-tax without one.
    fn split(&self) -> (USCents, USCents) {
        if self.m.retirement_plan == RetirementPlan::RETIREMENT_PLAN_UNSPECIFIED {
            return (USCents(0), USCents(1));
        }
        let Some(regular) = self.regular_stub() else {
            return (USCents(1), USCents(0));
        };
        let (kind, b403) = self.plan_kinds();
        let pre = regular.this.pretax_line(kind).v;
        let roth = if b403 {
            regular.this.roth403b.v
        } else {
            regular.this.roth401k.v
        };
        if (pre + roth).0 == 0 {
            return (USCents(1), USCents(0));
        }
        (pre, roth)
    }

    /// Decides a job's catch-up. It must be Roth when last year's box 3
    /// from the employer exceeded $150,000, § 414(v)(7), and the plan has a
    /// Roth option; with none, no catch-up is allowed. Without last year's
    /// W-2, or without the plan's roth_option, the rule isn't applied: the
    /// catch-up follows the paystub's split, and the result asks.
    fn catch_up(&self, notes: &mut Notes) -> CatchUpRule {
        let Some(w) = self.m.last_year.as_option().and_then(|l| l.w2.as_option()) else {
            notes.ask(new_question(
                model::item_id(&[&self.item_prefix(), "last-year-w2"]),
                self.item_prefix(),
                format!(
                    "Did your Social Security wages from {} last year, W-2 box 3, exceed {}?",
                    self.m.name,
                    payroll::ROTH_CATCH_UP_WAGES
                ),
                "Above it, a catch-up contribution must be Roth. It is counted as your paychecks split it until you say.",
                format!("{}.last_year", self.path()),
                &[],
            ));
            return CatchUpRule::Split;
        };
        if model::value(&w.social_security_wages) <= payroll::ROTH_CATCH_UP_WAGES {
            return CatchUpRule::Split;
        }
        match self.m.roth_option {
            None => {
                notes.ask(new_question(
                    model::item_id(&[&self.item_prefix(), "roth-option"]),
                    self.item_prefix(),
                    format!(
                        "Does {}'s workplace plan take Roth contributions? Your catch-up must be Roth.",
                        self.m.name
                    ),
                    "Without a Roth option no catch-up is allowed. It is counted as your paychecks split it until you say.",
                    format!("{}.roth_option", self.path()),
                    &["Yes", "No"],
                ));
                CatchUpRule::Split
            }
            Some(true) => CatchUpRule::Roth,
            Some(false) => CatchUpRule::None,
        }
    }
}

/// The position of a pre-tax kind in the item's written figures.
fn pretax_index(p: &Pay, kind: Kind) -> usize {
    p.proto()
        .pretax
        .iter()
        .position(|x| x.kind.to_i32() == kind)
        .unwrap_or(0)
}
