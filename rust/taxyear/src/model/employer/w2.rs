//! A W-2's year as the job's pay. Ported from w2.go.

use money::USCents;
use proto::federalv1::FormW2;
use proto::scenariosv1::{ItemKind, ItemStatus, Origin, origin::Basis};

use super::pay::{HSA, K401, K403B, Line, OTHER_BENEFITS, Pay, set_line};
use super::{Draft, Job};
use crate::model;

/// How a W-2's box 14 names the other pre-tax benefits, which leave boxes
/// 1, 3 and 5 as a cafeteria plan's do.
const BOX14_BENEFITS: &str = "other pre-tax benefits";

impl<'a> Job<'a> {
    /// Makes the job's items add up to its W-2s, which state the whole
    /// year: an ADJUSTMENT on December 31 for the W-2s' pay less the made
    /// items'. Pay dates with a deposit and no paystub read their gross from
    /// a template, and the W-2 says what they were.
    pub(super) fn read_w2s(&mut self) {
        if self.w2s.is_empty() {
            return;
        }
        let mut want = Pay::default();
        for w in &self.w2s {
            want = want.plus(&w2_pay(w, model::value(&self.m.employer_hsa)));
        }
        let got = self.sum_of(&self.made());
        let mut diff = Pay::default();
        let pick = |w: Line, h: Line| -> Line {
            if !w.set {
                return Line::default();
            }
            set_line(w.v - h.v)
        };
        diff.gross = pick(want.gross, got.gross);
        diff.federal = pick(want.federal, got.federal);
        diff.ss = pick(want.ss, got.ss);
        diff.medicare = pick(want.medicare, got.medicare);
        diff.roth401k = pick(want.roth401k, got.roth401k);
        diff.roth403b = pick(want.roth403b, got.roth403b);
        for (k, v) in &want.pretax {
            diff.set_pretax(*k, pick(*v, got.pretax_line(*k)));
        }
        for s in &want.states {
            let h = got.state(&s.state).unwrap_or_default();
            let i = diff.state_index(&s.state);
            diff.states[i].wages = pick(s.wages, h.wages);
            diff.states[i].withheld = pick(s.withheld, h.withheld);
        }
        let diff = diff.nonzero();
        if diff.empty() {
            return;
        }
        let end = self.ctx.year_end();
        self.items.push(Draft {
            id: model::item_id(&[&self.item_prefix(), &end.to_string(), "w2", "adjustment"]),
            date: end,
            status: ItemStatus::ITEM_STATUS_MADE,
            kind: ItemKind::ITEM_KIND_ADJUSTMENT,
            origin: Origin {
                basis: Basis::BASIS_DOCUMENT.into(),
                part: self.w2_parts[0].clone(),
                ..Default::default()
            },
            p: diff,
            ..Draft::default()
        });
    }
}

/// A W-2 as the year's pay: gross is box 1 plus every pre-tax line, the HSA
/// line is box 12 code W less the employer's own contribution, and the
/// other benefits are box 14's.
fn w2_pay(w: &FormW2, employer_hsa: USCents) -> Pay {
    let mut p = Pay::default();
    let add = |p: &mut Pay, k: i32, v: USCents| {
        let l = p.pretax_line(k).plus(set_line(v));
        p.set_pretax(k, l);
    };
    for c in &w.coded_amounts {
        let v = model::value(&c.amount);
        match c.code.to_uppercase().as_str() {
            "D" => add(&mut p, K401, v),
            "E" => add(&mut p, K403B, v),
            "W" => add(&mut p, HSA, v - employer_hsa),
            "AA" => p.roth401k = p.roth401k.plus(set_line(v)),
            "BB" => p.roth403b = p.roth403b.plus(set_line(v)),
            _ => {}
        }
    }
    for o in &w.other_items {
        if o.description.trim().to_lowercase() == BOX14_BENEFITS {
            add(&mut p, OTHER_BENEFITS, model::value(&o.amount));
        }
    }
    p.gross = set_line(model::value(&w.wages_tips_other_compensation) + p.pretax_total());
    p.federal = set_line(model::value(&w.federal_tax_withheld));
    p.ss = set_line(model::value(&w.social_security_tax_withheld));
    p.medicare = set_line(model::value(&w.medicare_tax_withheld));
    for s in &w.state_tax_withholding {
        let i = p.state_index(&s.state);
        p.states[i].wages = p.states[i]
            .wages
            .plus(set_line(model::value(&s.state_income)));
        p.states[i].withheld = p.states[i]
            .withheld
            .plus(set_line(model::value(&s.tax_withheld)));
    }
    p
}
