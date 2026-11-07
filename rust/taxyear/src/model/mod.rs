//! What every model's expansion shares: the year being computed, money and
//! date conversions, item ids, the state lived in on a date, and questions.
//! Ported from the Go package `taxyear/model`.
//!
//! Each model's own rules are in a module beneath this one, such as
//! `model::account`. See docs/models.md.

use std::sync::Arc;

use buffa::{MessageField, ProtoBox};
use money::USCents;
use proto::{commonv1, factsv1, scenariosv1};

use crate::internal::civil::{self, Date};

mod cadence;
mod months;
mod questions;
mod rounding;

pub mod account;
pub mod broker;
pub mod business;
pub mod itemised;
pub mod otherincome;
pub mod retirement;
pub mod taxpaid;

pub use cadence::{Cadence, cadence_of};
pub use months::{decimal, forms_due, month_of, of_fine, percent};
pub use questions::{Notes, Question, new_question};
pub use rounding::{Rounder, Runs, round_items, run_of};

/// An error from making a context or expanding a model, with Go's text.
#[derive(Clone, Debug, PartialEq, Eq, thiserror::Error)]
pub enum Error {
    /// The tax year is outside 1900 to 9999.
    #[error("model: tax year {0} is not a year")]
    NotAYear(i64),
    /// as_of is not a day.
    #[error("model: as_of: {0}")]
    AsOf(civil::Error),
    /// as_of is not given.
    #[error("model: as_of is not set")]
    AsOfNotSet,
    /// A model's inputs can't be expanded; the text says where and why.
    #[error("{0}")]
    Expansion(String),
}

/// Context is the year a scenario is computed for: its tax year, as_of and
/// facts. It is read-only once made, and cheap to clone.
#[derive(Clone, Debug)]
pub struct Context {
    year: i64,
    as_of: Date,
    facts: Arc<factsv1::TaxYearFacts>,
}

impl Context {
    /// Returns the context for a scenario. as_of must be a real day; facts
    /// may be nil, which reads as a household with no facts.
    pub fn new(
        year: i64,
        as_of: Option<&commonv1::Date>,
        facts: Option<Arc<factsv1::TaxYearFacts>>,
    ) -> Result<Context, Error> {
        if !(1900..=9999).contains(&year) {
            return Err(Error::NotAYear(year));
        }
        let d = civil::from_proto(as_of).map_err(Error::AsOf)?;
        if d.is_zero() {
            return Err(Error::AsOfNotSet);
        }
        Ok(Context {
            year,
            as_of: d,
            facts: facts.unwrap_or_default(),
        })
    }

    /// The tax year.
    pub fn year(&self) -> i64 {
        self.year
    }

    /// The day the year is computed as of. Anything dated after it is the
    /// rest of the year.
    pub fn as_of(&self) -> Date {
        self.as_of
    }

    /// The household's facts.
    pub fn facts(&self) -> &factsv1::TaxYearFacts {
        &self.facts
    }

    /// The household's facts, shared.
    pub fn facts_arc(&self) -> &Arc<factsv1::TaxYearFacts> {
        &self.facts
    }

    /// January 1 of the tax year.
    pub fn year_start(&self) -> Date {
        civil::must(self.year, 1, 1)
    }

    /// December 31 of the tax year.
    pub fn year_end(&self) -> Date {
        civil::must(self.year, 12, 31)
    }

    /// Reports whether as_of is after the tax year.
    pub fn finished(&self) -> bool {
        self.as_of.after(self.year_end())
    }

    /// Returns the person with the id, or nil.
    pub fn person(&self, id: &str) -> Option<&factsv1::Person> {
        self.facts.people.iter().find(|p| p.id == id)
    }

    /// Returns a person's age on December 31 of the tax year, and false
    /// when their date of birth is not given.
    pub fn age_at_year_end(&self, person: &str) -> (i64, bool) {
        let born = self
            .person(person)
            .map(|p| p.date_of_birth.year)
            .unwrap_or(0);
        if born == 0 {
            return (0, false);
        }
        (self.year - i64::from(born), true)
    }

    /// Returns the state the household lived in on d: the period that holds
    /// it; in a gap, the period before; before every period, the first. It
    /// returns "" when the facts give no residence.
    pub fn lived_in(&self, d: Date) -> String {
        struct Span<'a> {
            state: &'a str,
            from: Date,
            to: Date,
        }
        let mut spans = Vec::new();
        for r in &self.facts.residence {
            let (Ok(mut from), Ok(to)) = (civil::from_field(&r.from), civil::from_field(&r.to))
            else {
                continue;
            };
            if from.is_zero() {
                from = self.year_start();
            }
            spans.push(Span {
                state: &r.state,
                from,
                to,
            });
        }
        if spans.is_empty() {
            return String::new();
        }
        spans.sort_by_key(|s| s.from);
        let mut state = spans[0].state;
        for s in &spans {
            if s.from.after(d) {
                break;
            }
            state = s.state;
            if s.to.is_zero() || !d.after(s.to) {
                return s.state.to_string();
            }
        }
        state.to_string()
    }
}

/// Anything a Money is read from: an optional message field, a reference
/// that may be nil, or a Money.
pub trait MoneyRef {
    /// The Money, or nil.
    fn money_ref(&self) -> Option<&commonv1::Money>;
}

impl<P: ProtoBox<commonv1::Money>> MoneyRef for MessageField<commonv1::Money, P> {
    fn money_ref(&self) -> Option<&commonv1::Money> {
        self.as_option()
    }
}

impl MoneyRef for Option<&commonv1::Money> {
    fn money_ref(&self) -> Option<&commonv1::Money> {
        *self
    }
}

impl MoneyRef for commonv1::Money {
    fn money_ref(&self) -> Option<&commonv1::Money> {
        Some(self)
    }
}

/// Returns a Money's amount and whether it is set. A Money in any currency
/// but USD, or none, is read as USD: every form here is filed in it.
pub fn cents<M: MoneyRef + ?Sized>(m: &M) -> (USCents, bool) {
    match m.money_ref() {
        None => (USCents(0), false),
        Some(m) => (USCents(m.minor), true),
    }
}

/// Returns a Money's amount, zero when unset.
pub fn value<M: MoneyRef + ?Sized>(m: &M) -> USCents {
    cents(m).0
}

/// Returns the amount as a USD Money.
pub fn money(c: USCents) -> commonv1::Money {
    commonv1::Money {
        minor: c.0,
        currency: "USD".to_string(),
        ..Default::default()
    }
}

/// Returns the amount as a Money when set, else nil.
pub fn money_if_set(c: USCents, set: bool) -> Option<commonv1::Money> {
    if !set {
        return None;
    }
    Some(money(c))
}

/// Joins the parts of an item's id: the model's list and id, then the date,
/// kind and any evidence or part id, such as
/// "employers/acme/2026-10-15/regular".
pub fn item_id(parts: &[&str]) -> String {
    parts.join("/")
}

/// Divides total into n parts that add back to it exactly, the first parts
/// taking any remainder a cent each. n must be positive.
pub fn share(total: USCents, n: i64) -> Vec<USCents> {
    let each = total.0 / n;
    let mut rem = total.0 - each * n;
    let mut step = 1;
    if rem < 0 {
        step = -1;
        rem = -rem;
    }
    (0..n)
        .map(|i| USCents(if i < rem { each + step } else { each }))
        .collect()
}

/// Returns v × num / den, rounded half away from zero, or v when den is
/// zero.
pub fn scale(v: USCents, num: USCents, den: USCents) -> USCents {
    if den.0 == 0 {
        return v;
    }
    // Go's int64 arithmetic wraps rather than failing.
    let p = v.0.wrapping_mul(num.0);
    let d = den.0;
    let q = (p.wrapping_abs().wrapping_add(d.wrapping_abs() / 2)) / d.wrapping_abs();
    if (p < 0) != (d < 0) {
        return USCents(q.wrapping_neg());
    }
    USCents(q)
}

/// Rounds an amount to whole dollars, half away from zero, as a figure made
/// from cents is rounded once.
pub fn dollars(c: USCents) -> USCents {
    if c.0 < 0 {
        return -dollars(-c);
    }
    USCents((c.0 + 50) / 100 * 100)
}

/// Reports whether a document counts: not removed by the household.
pub fn usable(r: &factsv1::DocumentRef) -> bool {
    !r.removed
}

/// Orders items by date, then id, as every projection lists them.
pub fn sort_items(items: &mut [scenariosv1::Item]) {
    items.sort_by(|a, b| {
        compare_dates(&a.date, &b.date)
            .cmp(&0)
            .then_with(|| a.id.cmp(&b.id))
    });
}

fn compare_dates(a: &commonv1::Date, b: &commonv1::Date) -> i32 {
    for (x, y) in [(a.year, b.year), (a.month, b.month), (a.day, b.day)] {
        if x != y {
            return if x < y { -1 } else { 1 };
        }
    }
    0
}
