//! Rounding a run of items to the dollar, ported from Go's
//! `taxyear/model/rounding.go`.

use std::collections::HashMap;

use buffa::{MessageField, ProtoBox};
use money::USCents;
use proto::commonv1;
use proto::scenariosv1::{self, __buffa::oneof::item::Figures, ItemStatus};

use super::{dollars, money, value};

/// Rounds a run of items' amounts to the dollar, line by line, so every
/// total of them adds the displayed parts and is no further from the exact
/// sum than rounding it would be (docs/results.md). Each amount is shown as
/// the change in its line's rounded running total, half away from zero, so a
/// run of $5,833.33 paychecks shows $5,833, $5,833, $5,834. A run is one
/// part's items so far, or for the rest of the year, each with its own
/// Rounder: a part's line then adds to its own exact sum rounded, the year
/// is so far's and the rest's sum, and every total adds the items shown.
#[derive(Clone, Debug, Default)]
pub struct Rounder {
    exact: HashMap<String, USCents>,
}

impl Rounder {
    /// Starts a run.
    pub fn new() -> Rounder {
        Rounder::default()
    }

    /// Returns the next amount of the named line, rounded.
    pub fn round(&mut self, line: &str, v: USCents) -> USCents {
        let before = self.exact.get(line).copied().unwrap_or_default();
        self.exact.insert(line.to_string(), before + v);
        dollars(before + v) - dollars(before)
    }
}

/// Rounds the money of each item's figures with a [`Rounder`] for each run
/// (see [`run_of`]): every singular amount of the figures message, by its
/// field. A sale lot's gain is then its rounded proceeds less its rounded
/// basis plus its rounded wash sale adjustment, as Form 8949's columns work
/// it out; a lot with no proceeds, a planned gain, rounds its gain alone.
pub fn round_items(items: &mut [scenariosv1::Item]) {
    let mut runs = Runs::default();
    for it in items.iter_mut() {
        round_item(&mut runs, it);
    }
}

/// Rounds one item in its run, as [`round_items`] does each in turn.
pub(crate) fn round_item(runs: &mut Runs, it: &mut scenariosv1::Item) {
    let r = runs.run(it.status == ItemStatus::ITEM_STATUS_MADE, &it.origin.part);
    round_figures(r, it);
}

/// Names the run an item is rounded in: its part's, so far or for the rest
/// of the year.
pub fn run_of(made: bool, part: &str) -> String {
    if made {
        return format!("made/{part}");
    }
    format!("expected/{part}")
}

/// Keeps a Rounder for each run.
#[derive(Clone, Debug, Default)]
pub struct Runs(HashMap<String, Rounder>);

impl Runs {
    /// Returns the run's Rounder: Go's `Runs.For`.
    pub fn run(&mut self, made: bool, part: &str) -> &mut Rounder {
        self.0.entry(run_of(made, part)).or_default()
    }
}

/// Rounds one Money field when it is set, under the line `which.name`.
fn round_field<P: ProtoBox<commonv1::Money>>(
    r: &mut Rounder,
    which: &str,
    name: &str,
    f: &mut MessageField<commonv1::Money, P>,
) {
    if let Some(m) = f.as_option_mut() {
        m.minor = r.round(&format!("{which}.{name}"), value(&*m)).0;
    }
}

/// Go ranges over the populated fields of the item's figures by
/// reflection; these are the singular Money fields of each figures message,
/// which a test checks against the descriptors.
fn round_figures(r: &mut Rounder, it: &mut scenariosv1::Item) {
    let Some(figures) = it.figures.as_mut() else {
        return;
    };
    match figures {
        Figures::Paycheck(f) => {
            let w = "paycheck";
            round_field(r, w, "gross", &mut f.gross);
            round_field(r, w, "federal_withheld", &mut f.federal_withheld);
            round_field(
                r,
                w,
                "social_security_withheld",
                &mut f.social_security_withheld,
            );
            round_field(r, w, "medicare_withheld", &mut f.medicare_withheld);
            round_field(r, w, "roth_401k", &mut f.roth_401k);
            round_field(r, w, "roth_403b", &mut f.roth_403b);
            round_field(r, w, "net", &mut f.net);
        }
        Figures::Business(f) => {
            let w = "business";
            round_field(r, w, "receipts", &mut f.receipts);
            round_field(r, w, "fees", &mut f.fees);
            round_field(r, w, "expenses", &mut f.expenses);
        }
        Figures::Account(f) => {
            let w = "account";
            round_field(r, w, "interest", &mut f.interest);
            round_field(r, w, "treasury_interest", &mut f.treasury_interest);
            round_field(r, w, "ordinary_dividends", &mut f.ordinary_dividends);
            round_field(r, w, "qualified_dividends", &mut f.qualified_dividends);
            round_field(
                r,
                w,
                "capital_gain_distributions",
                &mut f.capital_gain_distributions,
            );
            round_field(
                r,
                w,
                "section_199a_dividends",
                &mut f.section_199a_dividends,
            );
            round_field(r, w, "tax_exempt_interest", &mut f.tax_exempt_interest);
            round_field(r, w, "foreign_tax_paid", &mut f.foreign_tax_paid);
        }
        Figures::Sale(f) => {
            let w = "sale";
            round_field(r, w, "proceeds", &mut f.proceeds);
            round_field(r, w, "basis", &mut f.basis);
            round_field(r, w, "wash_sale_disallowed", &mut f.wash_sale_disallowed);
            if f.proceeds.is_set() {
                f.gain =
                    money(value(&f.proceeds) - value(&f.basis) + value(&f.wash_sale_disallowed))
                        .into();
            } else {
                round_field(r, w, "gain", &mut f.gain);
            }
        }
        Figures::Contribution(f) => {
            round_field(r, "contribution", "amount", &mut f.amount);
        }
        Figures::Itemised(f) => {
            round_field(r, "itemised", "amount", &mut f.amount);
        }
        Figures::OtherIncome(f) => {
            let w = "other_income";
            round_field(r, w, "gross", &mut f.gross);
            round_field(r, w, "taxable", &mut f.taxable);
            round_field(r, w, "federal_withheld", &mut f.federal_withheld);
            round_field(r, w, "state_withheld", &mut f.state_withheld);
            round_field(r, w, "expenses", &mut f.expenses);
        }
        Figures::Payment(f) => {
            round_field(r, "payment", "amount", &mut f.amount);
        }
    }
    if let Some(t) = it.take_home.as_option_mut() {
        t.minor = r.round("take_home", value(&*t)).0;
    }
}

/// The singular Money fields [`round_figures`] rounds, by figures message,
/// for the test that checks them against the descriptors.
#[cfg(test)]
pub(crate) const ROUNDED: &[(&str, &[&str])] = &[
    (
        "nexteggapp.taxprotos.facts.v1.PayFigures",
        &[
            "gross",
            "federal_withheld",
            "social_security_withheld",
            "medicare_withheld",
            "roth_401k",
            "roth_403b",
            "net",
        ],
    ),
    (
        "nexteggapp.taxcalcs.scenarios.v1.BusinessFigures",
        &["receipts", "fees", "expenses"],
    ),
    (
        "nexteggapp.taxcalcs.scenarios.v1.AccountFigures",
        &[
            "interest",
            "treasury_interest",
            "ordinary_dividends",
            "qualified_dividends",
            "capital_gain_distributions",
            "section_199a_dividends",
            "tax_exempt_interest",
            "foreign_tax_paid",
        ],
    ),
    (
        "nexteggapp.taxcalcs.scenarios.v1.SaleLot",
        &["proceeds", "basis", "wash_sale_disallowed", "gain"],
    ),
    (
        "nexteggapp.taxcalcs.scenarios.v1.ContributionFigures",
        &["amount"],
    ),
    (
        "nexteggapp.taxcalcs.scenarios.v1.ItemisedFigures",
        &["amount"],
    ),
    (
        "nexteggapp.taxcalcs.scenarios.v1.OtherIncomeFigures",
        &[
            "gross",
            "taxable",
            "federal_withheld",
            "state_withheld",
            "expenses",
        ],
    ),
    (
        "nexteggapp.taxcalcs.scenarios.v1.TaxPaymentFigures",
        &["amount"],
    ),
];

#[cfg(test)]
mod tests {
    use buffa_descriptor::{DescriptorPool, FieldKind, SingularKind};

    use super::*;

    /// Every singular Money field of each figures message, from the
    /// descriptors, so a field added to a message can't go unrounded.
    #[test]
    fn rounded_fields_are_every_money_field() {
        let pool = DescriptorPool::decode(proto::DESCRIPTORS).unwrap();
        let money_idx = pool
            .message_index("nexteggapp.taxprotos.common.v1.Money")
            .unwrap();
        let item = pool
            .message_by_name("nexteggapp.taxcalcs.scenarios.v1.Item")
            .unwrap();
        let figures: Vec<String> = item
            .fields()
            .iter()
            .filter(|f| f.oneof_index().is_some())
            .map(|f| match f.kind() {
                FieldKind::Singular(SingularKind::Message(i)) => {
                    pool.message(i).full_name().to_string()
                }
                k => panic!("figures field {} is {k:?}", f.name()),
            })
            .collect();
        let listed: Vec<&str> = ROUNDED.iter().map(|(m, _)| *m).collect();
        assert_eq!(figures, listed);
        for (name, fields) in ROUNDED {
            let m = pool.message_by_name(name).unwrap();
            let want: Vec<&str> = m
                .fields()
                .iter()
                .filter(|f| f.kind() == FieldKind::Singular(SingularKind::Message(money_idx)))
                .map(|f| f.name())
                .collect();
            assert_eq!(&want, fields, "{name}");
        }
    }

    #[test]
    fn rounds_a_run_of_paychecks() {
        let mut r = Rounder::new();
        let got: Vec<i64> = [583_333, 583_333, 583_334]
            .iter()
            .map(|&v| r.round("gross", USCents(v)).0)
            .collect();
        assert_eq!(got, [583_300, 583_400, 583_300]);
    }
}
