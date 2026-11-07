//! Applying a diff: apply.go.

use std::collections::{BTreeMap, HashMap, HashSet};
use std::sync::Arc;

use buffa_descriptor::{DynamicMessage, FieldDescriptor, MapKey, MapValue, Value};

use crate::Error;
use crate::dynamic::{desc, is_map, list, map, map_key, message, mutable_message, set, value};
use crate::equal::{index_keys, item_key, key_of, less_key};
use crate::plan::{FPlan, Kind, Plan, Plans, plan_for};
use crate::validate::{key_field, num, op_of, validate_dynamic, wrapper_key};

/// Validates d against a, then returns a with d applied. a is not changed.
///
/// # Errors
///
/// Why d doesn't apply to a, as [`validate_dynamic`] reports it.
pub fn apply_dynamic(a: &DynamicMessage, d: &DynamicMessage) -> Result<DynamicMessage, Error> {
    validate_dynamic(a, d)?;
    let mut out = a.clone();
    let (ps, p) = plan_for(d.pool(), d.message_index())?;
    apply(&ps, &p, &mut out, d);
    Ok(out)
}

/// Changes m by d, which validation has accepted.
pub(crate) fn apply(ps: &Plans, p: &Plan, m: &mut DynamicMessage, d: &DynamicMessage) {
    let pool = Arc::clone(&ps.pool);
    let tpool = Arc::clone(m.pool());
    let tmd = desc(&tpool, m);
    for v in list(d, p.clear.fd(&pool)) {
        if let Value::I32(n) = *v
            && let Some(tfd) = u32::try_from(n).ok().and_then(|n| tmd.field(n))
        {
            use buffa_descriptor::ReflectMessageMut as _;
            m.clear(tfd);
        }
    }
    for fp in &p.fields {
        if !fp.present(&pool, d) {
            continue;
        }
        let Some(tfd) = tmd.field(fp.num) else {
            continue;
        };
        let dfd = fp.diff.fd(&pool);
        match fp.kind {
            Kind::Scalar => set(m, tfd, value(d, dfd)),
            Kind::Atomic => set(m, tfd, Value::Message(message(&pool, d, dfd).into_owned())),
            Kind::Patch => {
                // Mutable starts an unset message, or a new oneof case, from
                // empty.
                if let (Some(sub), Some(sm)) = (ps.sub(fp.sub), mutable_message(&tpool, m, tfd)) {
                    apply(ps, sub, sm, &message(&pool, d, dfd));
                }
            }
            Kind::Keyed => {
                if is_map(tfd) {
                    let out = apply_map(ps, fp, map(m, tfd), list(d, dfd));
                    set(m, tfd, Value::Map(out));
                } else {
                    let out = apply_keyed(ps, fp, &tpool, tfd, list(m, tfd), d);
                    set(m, tfd, Value::List(out));
                }
            }
            Kind::Indexed => {
                let out = apply_indexed(ps, fp, list(m, tfd), list(d, dfd));
                set(m, tfd, Value::List(out));
            }
            Kind::Replace => {
                use buffa_descriptor::ReflectMessageMut as _;
                m.clear(tfd);
                let w = message(&pool, d, dfd);
                if let Some(items) = fp.items {
                    let src = list(&w, items.fd(&pool));
                    if !src.is_empty() {
                        set(m, tfd, Value::List(src.to_vec()));
                    }
                }
            }
        }
    }
}

fn apply_keyed(
    ps: &Plans,
    fp: &FPlan,
    tpool: &Arc<buffa_descriptor::DescriptorPool>,
    tfd: &FieldDescriptor,
    l: &[Value],
    d: &DynamicMessage,
) -> Vec<Value> {
    let pool = &ps.pool;
    let mut items = l.to_vec();
    let Some(key_fd) = key_field(tpool, fp, tfd) else {
        return items;
    };
    let pos = index_keys(l, key_fd).unwrap_or_default();
    let mut removed = HashSet::new();
    let mut adds: Vec<(MapKey, Value)> = Vec::new();
    let (add, remove, patch) = (num(ps, fp.add), num(ps, fp.remove), num(ps, fp.patch));
    for w in list(d, fp.diff.fd(pool)) {
        let Value::Message(w) = w else { continue };
        let Some(k) = wrapper_key(ps, fp, w) else {
            continue;
        };
        let Some(op) = op_of(ps, fp, w) else { continue };
        let Some(ofd) = desc(pool, w).field(op) else {
            continue;
        };
        if Some(op) == add {
            adds.push((k, Value::Message(message(pool, w, ofd).into_owned())));
        } else if Some(op) == remove {
            if let Some(&j) = pos.get(&k) {
                removed.insert(j);
            }
        } else if Some(op) == patch {
            let Some(&j) = pos.get(&k) else { continue };
            match (ps.sub(fp.sub), &mut items[j]) {
                (None, item) => *item = Value::Message(message(pool, w, ofd).into_owned()),
                (Some(sub), Value::Message(item)) => apply(ps, sub, item, &message(pool, w, ofd)),
                (Some(_), _) => {}
            }
        }
    }
    adds.sort_by(|a, b| order_of(&a.0, &b.0));
    let mut result: Vec<Value> = items
        .into_iter()
        .enumerate()
        .filter(|(i, _)| !removed.contains(i))
        .map(|(_, v)| v)
        .collect();
    result.extend(adds.into_iter().map(|(_, v)| v));
    if let Some(order) = fp.order {
        let ofd = order.fd(pool);
        use buffa_descriptor::ReflectMessage as _;
        if d.has(ofd) {
            let mut rank = HashMap::new();
            for (i, v) in list(d, ofd).iter().enumerate() {
                if let Some(k) = key_of(ofd, v) {
                    rank.insert(k, i);
                }
            }
            let rank_of = |v: &Value| match v {
                Value::Message(item) => item_key(item, key_fd)
                    .and_then(|k| rank.get(&k).copied())
                    .unwrap_or(0),
                _ => 0,
            };
            result.sort_by_key(|v| rank_of(v));
        }
    }
    result
}

/// The order less_key gives.
pub(crate) fn order_of(a: &MapKey, b: &MapKey) -> std::cmp::Ordering {
    if less_key(a, b) {
        std::cmp::Ordering::Less
    } else if less_key(b, a) {
        std::cmp::Ordering::Greater
    } else {
        std::cmp::Ordering::Equal
    }
}

fn apply_map(ps: &Plans, fp: &FPlan, mp: &MapValue, changes: &[Value]) -> MapValue {
    let pool = &ps.pool;
    let mut out: BTreeMap<MapKey, Value> = mp.iter().map(|(k, v)| (k.clone(), v.clone())).collect();
    let (add, remove, patch, setop) = (
        num(ps, fp.add),
        num(ps, fp.remove),
        num(ps, fp.patch),
        num(ps, fp.set),
    );
    let Some(kfd) = fp.key.map(|f| f.fd(pool)) else {
        return mp.clone();
    };
    for w in changes {
        let Value::Message(w) = w else { continue };
        let Some(k) = map_key(&value(w, kfd)) else {
            continue;
        };
        let Some(op) = op_of(ps, fp, w) else { continue };
        let Some(ofd) = desc(pool, w).field(op) else {
            continue;
        };
        if Some(op) == remove {
            out.remove(&k);
        } else if Some(op) == setop || Some(op) == add {
            out.insert(k, value(w, ofd));
        } else if Some(op) == patch
            && let Some(sub) = ps.sub(fp.sub)
        {
            // Mutable starts an unset value from empty; validation has
            // made sure the key is there.
            if let Some(Value::Message(item)) = out.get_mut(&k) {
                apply(ps, sub, item, &message(pool, w, ofd));
            }
        }
    }
    MapValue::from_entries(out.into_iter().collect())
}

/// Builds the list by walking base indexes 0 to n-1: at each it emits the
/// insert there, then the element, patched or left out if removed. Then it
/// emits the insert at n.
fn apply_indexed(ps: &Plans, fp: &FPlan, l: &[Value], edits: &[Value]) -> Vec<Value> {
    let pool = &ps.pool;
    let n = l.len();
    let mut base = l.to_vec();
    let mut inserts: HashMap<usize, Vec<Value>> = HashMap::new();
    let mut removed = HashSet::new();
    let (insert, remove, patch) = (num(ps, fp.insert), num(ps, fp.remove), num(ps, fp.patch));
    let Some(ifd) = fp.index.map(|f| f.fd(pool)) else {
        return base;
    };
    for e in edits {
        let Value::Message(e) = e else { continue };
        let idx = match value(e, ifd) {
            Value::U32(i) => i as usize,
            _ => 0,
        };
        let Some(op) = op_of(ps, fp, e) else { continue };
        let Some(ofd) = desc(pool, e).field(op) else {
            continue;
        };
        if Some(op) == insert {
            if let Some(items) = fp.items {
                let ins = message(pool, e, ofd);
                inserts
                    .entry(idx)
                    .or_default()
                    .extend(list(&ins, items.fd(pool)).iter().cloned());
            }
        } else if Some(op) == remove {
            removed.insert(idx);
        } else if Some(op) == patch {
            match (ps.sub(fp.sub), base.get_mut(idx)) {
                (Some(sub), Some(Value::Message(item))) => {
                    apply(ps, sub, item, &message(pool, e, ofd))
                }
                (None, Some(item)) => *item = value(e, ofd),
                _ => {}
            }
        }
    }
    let mut result = Vec::with_capacity(n);
    for (i, v) in base.into_iter().enumerate() {
        if let Some(ins) = inserts.remove(&i) {
            result.extend(ins);
        }
        if !removed.contains(&i) {
            result.push(v);
        }
    }
    if let Some(ins) = inserts.remove(&n) {
        result.extend(ins);
    }
    result
}
