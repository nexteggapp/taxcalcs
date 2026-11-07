//! Rounding each model's box totals: round.go.
//!
//! Every model's box totals round once, as the employers' do: so far's and
//! the rest's are each rounded to the dollar, half away from zero, and the
//! year's is their sum. Item totals stay exact, since they are the items'
//! sum. The return then reads whole dollars.

use std::collections::HashMap;

use buffa::{Message, MessageName};
use buffa_descriptor::{DynamicMessage, FieldDescriptor, Value};
use money::USCents;
use proto::scenariosv1::{Form8949Box, Form8949Totals};

use crate::model;
use crate::reflect;

/// periods rounds so far's and the rest's Money fields and sets the year's
/// to their sum. The three are the same message type, holding Money
/// fields. An unset period is None.
pub(crate) fn periods<M: Message + MessageName + Default>(
    so_far: Option<&mut M>,
    rest: Option<&mut M>,
    year: Option<&mut M>,
) {
    let Some(year) = year else {
        return;
    };
    let (Some(mut y), mut s, mut r) = (
        reflect::to_dyn(&*year),
        so_far.as_deref().and_then(reflect::to_dyn),
        rest.as_deref().and_then(reflect::to_dyn),
    ) else {
        return;
    };
    let Some(md) = reflect::desc(&y) else {
        return;
    };
    for f in md.fields() {
        if !reflect::is_money(f) {
            continue;
        }
        let sv = dollars(s.as_ref(), f);
        let rv = dollars(r.as_ref(), f);
        if let Some(s) = s.as_mut() {
            set_money(s, f, sv);
        }
        if let Some(r) = r.as_mut() {
            set_money(r, f, rv);
        }
        if reflect::has(&y, f) || sv + rv != USCents(0) {
            set_money(&mut y, f, sv + rv);
        }
    }
    if let (Some(dst), Some(s)) = (so_far, s.as_ref().and_then(reflect::from_dyn)) {
        *dst = s;
    }
    if let (Some(dst), Some(r)) = (rest, r.as_ref().and_then(reflect::from_dyn)) {
        *dst = r;
    }
    if let Some(v) = reflect::from_dyn(&y) {
        *year = v;
    }
}

fn set_money(m: &mut DynamicMessage, f: &FieldDescriptor, c: USCents) {
    if let Some(v) = reflect::money_dyn(c.0) {
        reflect::set(m, f, Value::Message(v));
    }
}

fn dollars(m: Option<&DynamicMessage>, f: &FieldDescriptor) -> USCents {
    let Some(m) = m else {
        return USCents(0);
    };
    match reflect::message(m, f) {
        Some(v) => model::dollars(USCents(reflect::minor(v))),
        None => USCents(0),
    }
}

/// brokerYear rounds each Form 8949 box's columns for so far and the rest,
/// working out the gain from the rounded columns as the form does, and
/// adds them by box.
pub(crate) fn broker_year(
    so_far: &mut [Form8949Totals],
    rest: &mut [Form8949Totals],
) -> Vec<Form8949Totals> {
    let mut by_box: HashMap<i32, Form8949Totals> = HashMap::new();
    let mut add = |list: &mut [Form8949Totals]| {
        for b in list.iter_mut() {
            for m in [&mut b.proceeds, &mut b.basis, &mut b.adjustments] {
                if let Some(m) = m.as_option_mut() {
                    m.minor = model::dollars(USCents(m.minor)).0;
                }
            }
            // The form rounds by column: the gain is the rounded proceeds
            // less the rounded basis, plus the rounded adjustments.
            b.gain = model::money(
                model::value(&b.proceeds) - model::value(&b.basis) + model::value(&b.adjustments),
            )
            .into();
            let t = by_box
                .entry(b.r#box.to_i32())
                .or_insert_with(|| Form8949Totals {
                    r#box: b.r#box,
                    proceeds: model::money(USCents(0)).into(),
                    basis: model::money(USCents(0)).into(),
                    adjustments: model::money(USCents(0)).into(),
                    gain: model::money(USCents(0)).into(),
                    ..Default::default()
                });
            t.proceeds = model::money(model::value(&t.proceeds) + model::value(&b.proceeds)).into();
            t.basis = model::money(model::value(&t.basis) + model::value(&b.basis)).into();
            t.adjustments =
                model::money(model::value(&t.adjustments) + model::value(&b.adjustments)).into();
            t.gain = model::money(model::value(&t.gain) + model::value(&b.gain)).into();
        }
    };
    add(so_far);
    add(rest);
    let mut out = Vec::new();
    for k in (Form8949Box::FORM_8949_BOX_A as i32)..=(Form8949Box::FORM_8949_BOX_F as i32) {
        if let Some(t) = by_box.remove(&k) {
            out.push(t);
        }
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    /// Form 8949 rounds by column: the gain is the rounded proceeds less
    /// the rounded basis, not the exact gain rounded. $100.40 less $50.60
    /// is $49.80, which rounds to $50; the columns give $100 less $51, $49.
    #[test]
    fn form_8949_rounds_by_column() {
        let mut so_far = vec![Form8949Totals {
            r#box: Form8949Box::FORM_8949_BOX_A.into(),
            proceeds: model::money(USCents(10_040)).into(),
            basis: model::money(USCents(5_060)).into(),
            adjustments: model::money(USCents(0)).into(),
            gain: model::money(USCents(4_980)).into(),
            ..Default::default()
        }];
        let year = broker_year(&mut so_far, &mut []);
        assert_eq!(year[0].gain.minor, 4_900, "the gain");
        assert_eq!(so_far[0].gain.minor, 4_900, "so far's gain");
    }
}
