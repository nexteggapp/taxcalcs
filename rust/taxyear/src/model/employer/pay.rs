//! A paycheck's figures while a job is expanded. Ported from pay.go.

use std::collections::BTreeMap;

use buffa::EnumValue;
use money::USCents;
use proto::commonv1;
use proto::factsv1::{self, PretaxKind, pay_figures::Pretax};

use crate::model;

/// One pay line. Unset differs from zero, as on the proto.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub(super) struct Line {
    pub(super) v: USCents,
    pub(super) set: bool,
}

pub(super) fn set_line(v: USCents) -> Line {
    Line { v, set: true }
}

pub(super) fn line_of<M: model::MoneyRef + ?Sized>(m: &M) -> Line {
    let (v, set) = model::cents(m);
    Line { v, set }
}

impl Line {
    pub(super) fn plus(self, o: Line) -> Line {
        Line {
            v: self.v + o.v,
            set: self.set || o.set,
        }
    }

    pub(super) fn money(self) -> Option<commonv1::Money> {
        model::money_if_set(self.v, self.set)
    }
}

/// One state's wages and tax withheld.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub(super) struct StateLine {
    pub(super) state: String,
    pub(super) wages: Line,
    pub(super) withheld: Line,
}

/// A pre-tax kind as the key of a pay's pre-tax lines.
pub(super) type Kind = i32;

pub(super) const K401: Kind = PretaxKind::PRETAX_KIND_401K as i32;
pub(super) const K403B: Kind = PretaxKind::PRETAX_KIND_403B as i32;
pub(super) const HSA: Kind = PretaxKind::PRETAX_KIND_HSA as i32;
pub(super) const OTHER_BENEFITS: Kind = PretaxKind::PRETAX_KIND_OTHER_BENEFITS as i32;

/// The order pre-tax lines are written in.
pub(super) const PRETAX_KINDS: [Kind; 4] = [K401, K403B, HSA, OTHER_BENEFITS];

/// One paycheck's figures, or a sum of them.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub(super) struct Pay {
    pub(super) gross: Line,
    pub(super) pretax: BTreeMap<Kind, Line>,
    pub(super) federal: Line,
    pub(super) ss: Line,
    pub(super) medicare: Line,
    pub(super) roth401k: Line,
    pub(super) roth403b: Line,
    pub(super) net: Line,
    pub(super) states: Vec<StateLine>,
}

pub(super) fn pay_of(f: &factsv1::PayFigures) -> Pay {
    let mut p = Pay {
        gross: line_of(&f.gross),
        federal: line_of(&f.federal_withheld),
        ss: line_of(&f.social_security_withheld),
        medicare: line_of(&f.medicare_withheld),
        roth401k: line_of(&f.roth_401k),
        roth403b: line_of(&f.roth_403b),
        net: line_of(&f.net),
        ..Pay::default()
    };
    for x in &f.pretax {
        let k = x.kind.to_i32();
        let l = p.pretax_line(k).plus(line_of(&x.amount));
        p.set_pretax(k, l);
    }
    for s in &f.states {
        let i = p.state_index(&s.state);
        p.states[i].wages = p.states[i].wages.plus(line_of(&s.state_income));
        p.states[i].withheld = p.states[i].withheld.plus(line_of(&s.tax_withheld));
    }
    p
}

impl Pay {
    pub(super) fn pretax_line(&self, k: Kind) -> Line {
        self.pretax.get(&k).copied().unwrap_or_default()
    }

    pub(super) fn set_pretax(&mut self, k: Kind, l: Line) {
        self.pretax.insert(k, l);
    }

    /// Returns the index of the state's line, adding an unset one.
    pub(super) fn state_index(&mut self, state: &str) -> usize {
        if let Some(i) = self.states.iter().position(|s| s.state == state) {
            return i;
        }
        self.states.push(StateLine {
            state: state.to_string(),
            ..StateLine::default()
        });
        self.states.len() - 1
    }

    pub(super) fn state(&self, state: &str) -> Option<StateLine> {
        self.states.iter().find(|s| s.state == state).cloned()
    }

    /// Every pre-tax line.
    pub(super) fn pretax_total(&self) -> USCents {
        self.pretax.values().map(|l| l.v).sum()
    }

    /// Federal taxable wages, W-2 box 1's terms: gross less every pre-tax
    /// line.
    pub(super) fn taxable(&self) -> USCents {
        self.gross.v - self.pretax_total()
    }

    /// Social Security and Medicare wages before the cap: gross less HSA
    /// and other benefits.
    pub(super) fn fica(&self) -> USCents {
        self.gross.v - self.pretax_line(HSA).v - self.pretax_line(OTHER_BENEFITS).v
    }

    /// The workplace plan's pre-tax and Roth lines.
    pub(super) fn deferrals(&self) -> USCents {
        self.pretax_line(K401).v + self.pretax_line(K403B).v + self.roth401k.v + self.roth403b.v
    }

    pub(super) fn plus(&self, o: &Pay) -> Pay {
        let mut out = self.clone();
        out.gross = out.gross.plus(o.gross);
        for (k, v) in &o.pretax {
            let l = out.pretax_line(*k).plus(*v);
            out.set_pretax(*k, l);
        }
        out.federal = out.federal.plus(o.federal);
        out.ss = out.ss.plus(o.ss);
        out.medicare = out.medicare.plus(o.medicare);
        out.roth401k = out.roth401k.plus(o.roth401k);
        out.roth403b = out.roth403b.plus(o.roth403b);
        out.net = out.net.plus(o.net);
        for s in &o.states {
            let i = out.state_index(&s.state);
            out.states[i].wages = out.states[i].wages.plus(s.wages);
            out.states[i].withheld = out.states[i].withheld.plus(s.withheld);
        }
        out
    }

    /// Keeps only the lines with a nonzero amount.
    pub(super) fn nonzero(&self) -> Pay {
        let nz = |l: Line| if l.v.0 == 0 { Line::default() } else { l };
        let mut out = Pay {
            gross: nz(self.gross),
            federal: nz(self.federal),
            ss: nz(self.ss),
            medicare: nz(self.medicare),
            roth401k: nz(self.roth401k),
            roth403b: nz(self.roth403b),
            net: nz(self.net),
            ..Pay::default()
        };
        for (k, v) in &self.pretax {
            if v.v.0 != 0 {
                out.set_pretax(*k, *v);
            }
        }
        for s in &self.states {
            if s.wages.v.0 != 0 || s.withheld.v.0 != 0 {
                out.states.push(StateLine {
                    state: s.state.clone(),
                    wages: nz(s.wages),
                    withheld: nz(s.withheld),
                });
            }
        }
        out
    }

    /// Reports whether no line is set.
    pub(super) fn empty(&self) -> bool {
        let n = self.nonzero();
        !n.gross.set
            && !n.federal.set
            && !n.ss.set
            && !n.medicare.set
            && !n.roth401k.set
            && !n.roth403b.set
            && !n.net.set
            && n.pretax.is_empty()
            && n.states.is_empty()
    }

    /// Writes the figures, unset lines left unset, pre-tax lines in kind
    /// order and states by postal code.
    pub(super) fn proto(&self) -> factsv1::PayFigures {
        let mut out = factsv1::PayFigures {
            gross: self.gross.money().into(),
            federal_withheld: self.federal.money().into(),
            social_security_withheld: self.ss.money().into(),
            medicare_withheld: self.medicare.money().into(),
            roth_401k: self.roth401k.money().into(),
            roth_403b: self.roth403b.money().into(),
            net: self.net.money().into(),
            ..Default::default()
        };
        for k in PRETAX_KINDS {
            if let Some(l) = self.pretax.get(&k)
                && l.set
            {
                out.pretax.push(Pretax {
                    kind: EnumValue::from(k),
                    amount: l.money().into(),
                    ..Default::default()
                });
            }
        }
        let mut states = self.states.clone();
        states.sort_by(|a, b| a.state.cmp(&b.state));
        for s in states {
            if !s.wages.set && !s.withheld.set {
                continue;
            }
            out.states.push(commonv1::StateTaxWithholding {
                state: s.state,
                state_income: s.wages.money().into(),
                tax_withheld: s.withheld.money().into(),
                ..Default::default()
            });
        }
        out
    }

    /// Adds every line, for figures that hold one.
    pub(super) fn line_sum(&self) -> USCents {
        let mut t = self.gross.v
            + self.federal.v
            + self.ss.v
            + self.medicare.v
            + self.roth401k.v
            + self.roth403b.v
            + self.net.v
            + self.pretax_total();
        for s in &self.states {
            t += s.wages.v + s.withheld.v;
        }
        t
    }
}

/// Adds figures.
pub(super) fn sum_pay<'p>(ps: impl IntoIterator<Item = &'p Pay>) -> Pay {
    let mut t = Pay::default();
    for p in ps {
        t = t.plus(p);
    }
    t
}
