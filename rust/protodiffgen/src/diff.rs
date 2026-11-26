//! Computing the smallest diff from one message to another: diff.go.

use std::sync::Arc;

use buffa_descriptor::{
    DynamicMessage, FieldDescriptor, MapKey, MapValue, ReflectMessage, ReflectMessageMut, Value,
};

use crate::Error;
use crate::apply::order_of;
use crate::dynamic::{
    containing_oneof, desc, is_empty, is_map, list, map, map_value, message, message_type,
    mutable_message, new_message, set, value, which_oneof,
};
use crate::equal::{index_keys, item_key, key_value, less_key};
use crate::myers::myers;
use crate::plan::{FPlan, Kind, Plan, Plans, Rules, plan_for, rules_for};
use crate::validate::{check_data, key_field};

/// Writes into out, a diff message, the smallest diff from a to b. out is
/// reset first. The diff is in normal form: it holds no entry that changes
/// nothing, keyed list changes are in key order, and indexed edits come
/// from a Myers sequence diff, in index order.
///
/// # Errors
///
/// When out is not a patch body for a's type, b is of another type, or a
/// or b has unknown fields or a keyed list with an unset or repeated key.
pub fn diff_dynamic(
    a: &DynamicMessage,
    b: &DynamicMessage,
    out: &mut DynamicMessage,
) -> Result<(), Error> {
    let (ps, p) = plan_for(out.pool(), out.message_index())?;
    p.bind(&ps, a.pool(), a.message_index())?;
    let (an, bn) = (
        a.message_descriptor().full_name().to_string(),
        b.message_descriptor().full_name().to_string(),
    );
    if an != bn {
        return Err(Error::Mismatch(format!("a is {an} but b is {bn}")));
    }
    p.bind(&ps, b.pool(), b.message_index())?;
    check_data(&ps, Some(&p), a, "a")?;
    check_data(&ps, Some(&p), b, "b")?;
    *out = DynamicMessage::new(Arc::clone(out.pool()), out.message_index());
    let df = Differ {
        rules: rules_for(&ps.pool, p.body),
    };
    df.message(&ps, &p, a, b, out)
}

struct Differ {
    rules: Rules,
}

/// One change to a keyed list or map: the key, the op by number, and its
/// value.
struct KeyChange {
    key: MapKey,
    op: u32,
    val: Option<Value>,
}

impl Differ {
    /// Writes into out the patch from a to b. a may be an empty message.
    fn message(
        &self,
        ps: &Plans,
        p: &Plan,
        a: &DynamicMessage,
        b: &DynamicMessage,
        out: &mut DynamicMessage,
    ) -> Result<(), Error> {
        p.bind(ps, a.pool(), a.message_index())?;
        let pool = Arc::clone(&ps.pool);
        let (apool, bpool) = (Arc::clone(a.pool()), Arc::clone(b.pool()));
        let (amd, bmd) = (desc(&apool, a), desc(&bpool, b));
        let mut clears: Vec<i32> = Vec::new();
        for fp in &p.fields {
            let (Some(tfd), Some(bfd)) = (amd.field(fp.num), bmd.field(fp.num)) else {
                continue;
            };
            let dfd = fp.diff.fd(&pool);
            let (mut ha, hb) = (a.has(tfd), b.has(bfd));
            if let Some((oi, o)) = containing_oneof(amd, tfd)
                && !o.is_synthetic()
            {
                let ca = which_oneof(amd, a, oi);
                let cb = bmd
                    .oneofs()
                    .iter()
                    .position(|x| x.name() == o.name())
                    .and_then(|bi| which_oneof(bmd, b, bi));
                if cb != Some(fp.num) {
                    if ha && cb.is_none() {
                        clears.push(fp.num as i32);
                    }
                    continue;
                }
                if ca != Some(fp.num) {
                    // A new case starts from empty.
                    ha = false;
                }
            }
            match fp.kind {
                Kind::Scalar => {
                    let (av, bv) = (value(a, tfd), value(b, bfd));
                    if crate::dynamic::has_presence(tfd) {
                        if ha && !hb {
                            clears.push(fp.num as i32);
                        } else if hb && (!ha || !self.rules.equal_value(tfd, &av, &bv)) {
                            set(out, dfd, bv);
                        }
                    } else if !self.rules.equal_value(tfd, &av, &bv) {
                        set(out, dfd, bv);
                    }
                }
                Kind::Atomic => {
                    if ha && !hb {
                        clears.push(fp.num as i32);
                    } else if hb
                        && (!ha
                            || !self
                                .rules
                                .equal(&message(&apool, a, tfd), &message(&bpool, b, bfd)))
                    {
                        set(
                            out,
                            dfd,
                            Value::Message(message(&bpool, b, bfd).into_owned()),
                        );
                    }
                }
                Kind::Patch => {
                    if ha && !hb {
                        clears.push(fp.num as i32);
                        continue;
                    }
                    if !hb {
                        continue;
                    }
                    let bm = message(&bpool, b, bfd);
                    let base = if ha {
                        message(&apool, a, tfd)
                    } else {
                        std::borrow::Cow::Owned(new_message(&bpool, bm.message_index()))
                    };
                    let Some(sub) = ps.sub(fp.sub) else { continue };
                    let Some(sm) = mutable_message(&pool, out, dfd) else {
                        continue;
                    };
                    self.message(ps, sub, &base, &bm, sm)?;
                    if ha && is_empty(sm) {
                        out.clear(dfd);
                    }
                }
                Kind::Keyed => {
                    if is_map(tfd) {
                        self.map_field(ps, fp, tfd, map(a, tfd), map(b, bfd), out)?;
                    } else {
                        self.keyed(ps, fp, tfd, list(a, tfd), list(b, bfd), out)?;
                    }
                }
                Kind::Indexed => self.indexed(ps, fp, tfd, list(a, tfd), list(b, bfd), out)?,
                Kind::Replace => {
                    let (la, lb) = (list(a, tfd), list(b, bfd));
                    if !self.rules.equal_list(tfd, la, lb)
                        && let (Some(items), Some(w)) = (fp.items, mutable_message(&pool, out, dfd))
                    {
                        set(w, items.fd(&pool), Value::List(lb.to_vec()));
                    }
                }
            }
        }
        if !clears.is_empty() {
            clears.sort_unstable();
            set(
                out,
                p.clear.fd(&pool),
                Value::List(clears.into_iter().map(Value::I32).collect()),
            );
        }
        Ok(())
    }

    /// A new, empty patch body for fp's patch op.
    fn new_patch(&self, ps: &Plans, fp: &FPlan) -> Option<DynamicMessage> {
        let pfd = fp.patch?.fd(&ps.pool);
        Some(new_message(&ps.pool, message_type(pfd)?))
    }

    fn keyed(
        &self,
        ps: &Plans,
        fp: &FPlan,
        tfd: &FieldDescriptor,
        a: &[Value],
        b: &[Value],
        out: &mut DynamicMessage,
    ) -> Result<(), Error> {
        let pool = &ps.pool;
        // The items' pool: a's and b's lists hold messages of one type.
        let ipool = match a.first().or(b.first()) {
            Some(Value::Message(m)) => Arc::clone(m.pool()),
            _ => return Ok(()),
        };
        let Some(key_fd) = key_field(&ipool, fp, tfd) else {
            return Ok(());
        };
        let ai = index_keys(a, key_fd).unwrap_or_default();
        let bi = index_keys(b, key_fd).unwrap_or_default();
        let (Some(remove), Some(add_or_set)) = (
            fp.remove.map(|f| f.fd(pool).number()),
            fp.add.or(fp.set).map(|f| f.fd(pool).number()),
        ) else {
            return Ok(());
        };
        let patch = fp.patch.map(|f| f.fd(pool).number());
        let mut changes = Vec::new();
        for v in a {
            let Value::Message(item) = v else { continue };
            let Some(k) = item_key(item, key_fd) else {
                continue;
            };
            if !bi.contains_key(&k) {
                changes.push(KeyChange {
                    key: k,
                    op: remove,
                    val: None,
                });
            }
        }
        let mut added = Vec::new();
        for v in b {
            let Value::Message(item) = v else { continue };
            let Some(k) = item_key(item, key_fd) else {
                continue;
            };
            let Some(&i) = ai.get(&k) else {
                changes.push(KeyChange {
                    key: k.clone(),
                    op: add_or_set,
                    val: Some(v.clone()),
                });
                added.push(k);
                continue;
            };
            let Value::Message(ai_item) = &a[i] else {
                continue;
            };
            let Some(patch) = patch else { continue };
            let Some(sub) = ps.sub(fp.sub) else {
                if !self.rules.equal(ai_item, item) {
                    changes.push(KeyChange {
                        key: k,
                        op: patch,
                        val: Some(v.clone()),
                    });
                }
                continue;
            };
            let Some(mut pm) = self.new_patch(ps, fp) else {
                continue;
            };
            self.message(ps, sub, ai_item, item, &mut pm)?;
            if !is_empty(&pm) {
                changes.push(KeyChange {
                    key: k,
                    op: patch,
                    val: Some(Value::Message(pm)),
                });
            }
        }
        self.emit(ps, fp, changes, out);

        if let Some(order) = fp.order {
            added.sort_by(order_of);
            let mut def: Vec<MapKey> = Vec::new();
            for v in a {
                let Value::Message(item) = v else { continue };
                if let Some(k) = item_key(item, key_fd)
                    && bi.contains_key(&k)
                {
                    def.push(k);
                }
            }
            def.extend(added);
            let same = b.iter().enumerate().all(|(j, v)| match v {
                Value::Message(item) => item_key(item, key_fd).as_ref() == def.get(j),
                _ => true,
            });
            if !same {
                let keys = b
                    .iter()
                    .filter_map(|v| match v {
                        Value::Message(item) => {
                            Some(value(item, crate::equal::key_fd_of(item, key_fd)))
                        }
                        _ => None,
                    })
                    .collect();
                set(out, order.fd(pool), Value::List(keys));
            }
        }
        Ok(())
    }

    fn map_field(
        &self,
        ps: &Plans,
        fp: &FPlan,
        tfd: &FieldDescriptor,
        a: &MapValue,
        b: &MapValue,
        out: &mut DynamicMessage,
    ) -> Result<(), Error> {
        let pool = &ps.pool;
        let num = |f: Option<crate::plan::FieldRef>| f.map(|f| f.fd(pool).number());
        let (Some(remove), set_op, add, patch) =
            (num(fp.remove), num(fp.set), num(fp.add), num(fp.patch))
        else {
            return Ok(());
        };
        let mut changes = Vec::new();
        for (k, _) in a.iter() {
            if b.get(k).is_none() {
                changes.push(KeyChange {
                    key: k.clone(),
                    op: remove,
                    val: None,
                });
            }
        }
        let vk = map_value(tfd);
        for (k, v) in b.iter() {
            if let Some(set_op) = set_op {
                let same = a.get(k).is_some_and(|av| self.rules.equal_kind(vk, av, v));
                if !same {
                    changes.push(KeyChange {
                        key: k.clone(),
                        op: set_op,
                        val: Some(v.clone()),
                    });
                }
                continue;
            }
            match a.get(k) {
                None => {
                    if let Some(add) = add {
                        changes.push(KeyChange {
                            key: k.clone(),
                            op: add,
                            val: Some(v.clone()),
                        });
                    }
                }
                Some(av) => {
                    let (Some(sub), Some(patch), Value::Message(av), Value::Message(bv)) =
                        (ps.sub(fp.sub), patch, av, v)
                    else {
                        continue;
                    };
                    let Some(mut pm) = self.new_patch(ps, fp) else {
                        continue;
                    };
                    self.message(ps, sub, av, bv, &mut pm)?;
                    if !is_empty(&pm) {
                        changes.push(KeyChange {
                            key: k.clone(),
                            op: patch,
                            val: Some(Value::Message(pm)),
                        });
                    }
                }
            }
        }
        self.emit(ps, fp, changes, out);
        Ok(())
    }

    /// Appends change wrappers to out in key order.
    fn emit(&self, ps: &Plans, fp: &FPlan, mut changes: Vec<KeyChange>, out: &mut DynamicMessage) {
        if changes.is_empty() {
            return;
        }
        let pool = &ps.pool;
        let (Some(wrap), Some(key)) = (fp.wrap, fp.key) else {
            return;
        };
        let kfd = key.fd(pool);
        let wmd = pool.message(wrap);
        changes.sort_by(|a, b| {
            if less_key(&a.key, &b.key) {
                std::cmp::Ordering::Less
            } else if less_key(&b.key, &a.key) {
                std::cmp::Ordering::Greater
            } else {
                std::cmp::Ordering::Equal
            }
        });
        let dfd = fp.diff.fd(pool);
        let mut l = list(out, dfd).to_vec();
        for c in changes {
            let mut w = new_message(pool, wrap);
            set(&mut w, kfd, key_value(kfd, &c.key));
            let Some(ofd) = wmd.field(c.op) else { continue };
            match (c.val, message_type(ofd)) {
                (Some(v), _) => set(&mut w, ofd, v),
                (None, Some(t)) => set(&mut w, ofd, Value::Message(new_message(pool, t))),
                (None, None) => {}
            }
            l.push(Value::Message(w));
        }
        set(out, dfd, Value::List(l));
    }

    fn indexed(
        &self,
        ps: &Plans,
        fp: &FPlan,
        tfd: &FieldDescriptor,
        a: &[Value],
        b: &[Value],
        out: &mut DynamicMessage,
    ) -> Result<(), Error> {
        let rs = myers(a.len(), b.len(), &mut |i, j| {
            self.rules.equal_value(tfd, &a[i], &b[j])
        });
        if rs.is_empty() {
            return Ok(());
        }
        let pool = &ps.pool;
        let (Some(wrap), Some(index), Some(remove), Some(patch), Some(insert), Some(items)) =
            (fp.wrap, fp.index, fp.remove, fp.patch, fp.insert, fp.items)
        else {
            return Ok(());
        };
        let (ifd, rfd, pfd, insfd, itemsfd) = (
            index.fd(pool),
            remove.fd(pool),
            patch.fd(pool),
            insert.fd(pool),
            items.fd(pool),
        );
        let dfd = fp.diff.fd(pool);
        let mut l = list(out, dfd).to_vec();
        let edit = |index: usize| {
            let mut e = new_message(pool, wrap);
            set(&mut e, ifd, Value::U32(index as u32));
            e
        };
        for r in rs {
            let pairs = r.removed.len().min(r.added.len());
            for (t, &i) in r.removed.iter().enumerate() {
                let mut e = edit(i);
                if t >= pairs {
                    if let Some(rt) = message_type(rfd) {
                        set(&mut e, rfd, Value::Message(new_message(pool, rt)));
                    }
                    l.push(Value::Message(e));
                    continue;
                }
                let nv = &b[r.added[t]];
                match (ps.sub(fp.sub), &a[i], nv) {
                    (Some(sub), Value::Message(av), Value::Message(bv)) => {
                        let Some(pt) = message_type(pfd) else {
                            continue;
                        };
                        let mut pm = new_message(pool, pt);
                        self.message(ps, sub, av, bv, &mut pm)?;
                        set(&mut e, pfd, Value::Message(pm));
                    }
                    _ => set(&mut e, pfd, nv.clone()),
                }
                l.push(Value::Message(e));
            }
            if r.added.len() > pairs {
                let mut e = edit(r.next);
                if let Some(it) = message_type(insfd) {
                    let mut ins = new_message(pool, it);
                    let vals = r.added[pairs..].iter().map(|&j| b[j].clone()).collect();
                    set(&mut ins, itemsfd, Value::List(vals));
                    set(&mut e, insfd, Value::Message(ins));
                }
                l.push(Value::Message(e));
            }
        }
        set(out, dfd, Value::List(l));
        Ok(())
    }
}
