//! Rounding a job's items to the dollar. Ported from round.go.

use proto::scenariosv1::ItemStatus;

use super::Job;
use super::pay::{Line, Pay};
use crate::model::Runs;

impl Pay {
    /// Calls f with every set line of the pay and a name for it, and writes
    /// back what f leaves in it.
    fn each(&mut self, mut f: impl FnMut(&str, &mut Line)) {
        for (name, l) in [
            ("gross", &mut self.gross),
            ("federal", &mut self.federal),
            ("ss", &mut self.ss),
            ("medicare", &mut self.medicare),
            ("roth401k", &mut self.roth401k),
            ("roth403b", &mut self.roth403b),
            ("net", &mut self.net),
        ] {
            if l.set {
                f(name, l);
            }
        }
        for (k, l) in self.pretax.iter_mut() {
            if l.set {
                f(&format!("pretax{k}"), l);
            }
        }
        for s in &mut self.states {
            if s.wages.set {
                f(&format!("wages.{}", s.state), &mut s.wages);
            }
            if s.withheld.set {
                f(&format!("withheld.{}", s.state), &mut s.withheld);
            }
        }
    }
}

impl<'a> Job<'a> {
    /// Rounds each item's lines to the dollar with a running total per line
    /// in each part's run (see [`crate::model::round_items`]): each total
    /// then adds the displayed items, and a part's line is its own exact sum
    /// rounded.
    pub(super) fn round_items(&mut self) {
        let mut runs = Runs::default();
        for d in &mut self.items {
            let r = runs.run(d.status == ItemStatus::ITEM_STATUS_MADE, &d.origin.part);
            d.p.each(|name, l| l.v = r.round(name, l.v));
            if d.take_home.set {
                d.take_home.v = r.round("take_home", d.take_home.v);
            }
        }
    }
}
