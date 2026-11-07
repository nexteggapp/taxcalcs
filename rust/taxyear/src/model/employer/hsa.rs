//! Holding a person's HSA contributions through payroll to their limit.
//! Ported from hsa.go.

use money::USCents;
use proto::scenariosv1::__buffa::oneof::term::Of;
use proto::scenariosv1::{self, AppliedRule, ItemStatus, Statement, Term};

use super::pay::{HSA, Pay, set_line};
use super::{Job, Notes};
use crate::model::{self, Error};

/// The path an HSA cut marks as estimated. Go formats the pre-tax kind with
/// %v, which prints the enum's name rather than the line's index.
const HSA_ESTIMATED: &str = "paycheck.pretax[PRETAX_KIND_HSA].amount";

/// Holds a person's HSA contributions through payroll, with their
/// employers' own, to their limit. Payroll contributions through a
/// cafeteria plan are employer contributions (Instructions for Form 8889,
/// line 9), and what employers put in over the limit is income (Excess
/// Employer Contributions). The excess comes off the payroll HSA lines of
/// the paychecks to come, the last job's first, so it is in their wages,
/// Social Security and Medicare wages, and the tax withheld on them; what
/// is over on paychecks already made is other income. A state's wage lines
/// stay: California, which taxes HSA contributions, has them in already.
/// Call it after max_out and before projection.
pub fn hold_hsa(person: &str, limit: USCents, jobs: &mut [Job<'_>]) -> Result<(), Error> {
    let mut mine = Vec::new();
    let (mut asked, mut employer) = (USCents(0), USCents(0));
    for (i, j) in jobs.iter().enumerate() {
        if j.person() != person {
            continue;
        }
        if j.done {
            return Err(Error::Expansion(
                "employer: HoldHsa after Projection".to_string(),
            ));
        }
        mine.push(i);
        asked += j.sum_all().pretax_line(HSA).v;
        employer += model::value(&j.m.employer_hsa);
    }
    let zero = USCents(0);
    let mut over = zero.max(asked - zero.max(limit - employer));
    // Employers' own contributions over the limit are income too, with no
    // payroll line to take them off.
    if let Some(&last) = mine.last()
        && employer > limit
    {
        jobs[last].hsa_in_income += employer - limit;
    }
    if over == zero && employer <= limit {
        return Ok(());
    }
    for &k in mine.iter().rev() {
        if over <= zero {
            break;
        }
        let j = &mut jobs[k];
        let cut = j.cut_hsa(over, false);
        j.hsa_in_wages += cut;
        over -= cut;
    }
    // What is left is on paychecks whose figures are fixed: first those a
    // YearTotal part fills, then those already made, the last job's first,
    // as the cut runs from it.
    for &k in mine.iter().rev() {
        if over <= zero {
            break;
        }
        let j = &mut jobs[k];
        let take = over.min(j.sum_of(&j.typed_year()).pretax_line(HSA).v);
        j.hsa_in_income += take;
        j.hsa_typed += take;
        over -= take;
    }
    for &k in mine.iter().rev() {
        if over <= zero {
            break;
        }
        let j = &mut jobs[k];
        let made = j.sum_of(&j.made()).pretax_line(HSA).v;
        let take = over.min(made);
        j.hsa_in_income += take;
        over -= take;
    }
    for &k in &mine {
        jobs[k].hsa_working(limit, asked, employer);
    }
    Ok(())
}

impl<'a> Job<'a> {
    /// The job's paychecks a YearTotal part fills.
    pub(super) fn typed_year(&self) -> Vec<usize> {
        (0..self.items.len())
            .filter(|&i| self.items[i].typed_year)
            .collect()
    }

    /// Takes up to want off the payroll HSA lines of the paychecks to come,
    /// the latest first, and returns what it took. With typed, it cuts only
    /// the paychecks a YearTotal part fills; without, only the others.
    pub(super) fn cut_hsa(&mut self, want: USCents, typed: bool) -> USCents {
        let mut took = USCents(0);
        for d in self.items.iter_mut().rev() {
            if took >= want {
                break;
            }
            if d.status != ItemStatus::ITEM_STATUS_EXPECTED || d.typed_year != typed {
                continue;
            }
            let l = d.p.pretax_line(HSA);
            if !l.set || l.v.0 <= 0 {
                continue;
            }
            let c = l.v.min(want - took);
            // Box 1 rises by the cut. California's wages, which hold the
            // contribution, stay as they were (see state_base); a state
            // following the federal exclusion rises with box 1. Social
            // Security and Medicare wages rise too, so their tax is worked
            // out again on them.
            d.p.set_pretax(HSA, set_line(l.v - c));
            d.compute_ss = true;
            d.compute_medicare = true;
            d.estimated.push(HSA_ESTIMATED.to_string());
            took += c;
        }
        took
    }

    /// Records how the job's excess was made.
    fn hsa_working(&mut self, limit: USCents, asked: USCents, employer: USCents) {
        let over = self.hsa_in_wages + self.hsa_in_income;
        if over.0 == 0 {
            return;
        }
        let at = format!("projection.employers[{}]", self.m.id);
        let d = model::dollars;
        let term = |path: String| Term {
            of: Some(Of::Path(path)),
            sign: 1,
            ..Default::default()
        };
        self.workings.push(scenariosv1::Working {
            path: format!("{at}.hsa_over"),
            terms: vec![
                term(format!("{at}.hsa_over_in_wages")),
                term(format!("{at}.hsa_over_in_income")),
            ],
            rules: vec![AppliedRule {
                cite: "26 U.S.C. § 223(b) and § 106(d); Instructions for Form 8889, line 9 and Excess Employer Contributions".to_string(),
                applied: Statement {
                    text: format!(
                        "{} through payroll and {} from employers is over the {} limit by {}; {} of it is this job's",
                        d(asked),
                        d(employer),
                        d(limit),
                        d(asked + employer - limit),
                        d(over)
                    ),
                    states: vec![format!("{at}.hsa_over")],
                    ..Default::default()
                }
                .into(),
                ..Default::default()
            }],
            ..Default::default()
        });
    }

    /// The year's figures as they would be if max-out had set the plan line
    /// on the paychecks a YearTotal part fills, and the HSA split had taken
    /// hsa_typed off them, with their withholding worked out again. It works
    /// on a copy of the items.
    pub(super) fn split_typed(&self) -> Result<Pay, Error> {
        let mut s = self.clone();
        s.notes = Notes::default();
        s.workings = Vec::new();
        for i in 0..s.items.len() {
            if let Some(p) = self.plan_typed.get(&s.items[i].id).copied() {
                s.with_item(i, |s, c| s.set_plan(c, p.v, p.catch, &mut Notes::default()));
            }
        }
        s.cut_hsa(self.hsa_typed, true);
        s.withhold()?;
        s.round_items();
        Ok(s.sum_all())
    }

    /// Records, for each year figure max-out or the HSA split would have
    /// changed on the paychecks a YearTotal part fills, the figure it would
    /// have made as automatic.
    pub(super) fn typed_working(&mut self, auto: &Pay) {
        if self.hsa_typed.0 == 0 && self.plan_typed.is_empty() {
            return;
        }
        let at = format!("projection.employers[{}]", self.m.id);
        let parts: Vec<String> = match self.typed_year().first() {
            Some(&i) => vec![self.items[i].origin.part.clone()],
            None => Vec::new(),
        };
        let add = |j: &mut Job<'a>, path: String, v: USCents| {
            let w = j.working(&path);
            w.automatic = model::money(v).into();
            w.parts = parts.clone();
        };
        let year = self.sum_all();
        for (field, have, was) in [
            ("federal_withheld", year.federal, auto.federal),
            ("social_security_withheld", year.ss, auto.ss),
            ("medicare_withheld", year.medicare, auto.medicare),
            ("roth_401k", year.roth401k, auto.roth401k),
            ("roth_403b", year.roth403b, auto.roth403b),
        ] {
            if have.v != was.v {
                add(self, format!("{at}.year.items.{field}"), was.v);
            }
        }
        for (i, x) in year.proto().pretax.iter().enumerate() {
            let k = x.kind.to_i32();
            if (k == HSA && self.hsa_typed.0 != 0) || auto.pretax_line(k).v != year.pretax_line(k).v
            {
                add(
                    self,
                    format!("{at}.year.items.pretax[{i}].amount"),
                    auto.pretax_line(k).v,
                );
            }
        }
        for l in &year.states {
            let was = auto.state(&l.state).unwrap_or_default();
            if was.withheld.v != l.withheld.v {
                add(
                    self,
                    format!("{at}.year.items.states[{}].tax_withheld", l.state),
                    was.withheld.v,
                );
            }
            if was.wages.v != l.wages.v {
                add(
                    self,
                    format!("{at}.year.items.states[{}].state_income", l.state),
                    was.wages.v,
                );
            }
        }
        if self.hsa_typed.0 == 0 {
            return;
        }
        let d = model::dollars;
        let (wages, income) = (
            d(self.hsa_in_wages + self.hsa_typed),
            d(self.hsa_in_income - self.hsa_typed),
        );
        add(self, format!("{at}.hsa_over_in_wages"), wages);
        add(self, format!("{at}.hsa_over_in_income"), income);
    }
}
