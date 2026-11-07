//! Checking a diff against its target before it is applied: validate.go.

use std::collections::HashSet;
use std::sync::Arc;

use buffa_descriptor::{
    DescriptorPool, DynamicMessage, FieldDescriptor, MapKey, MapValue, ReflectMessage, Value,
};

use crate::Error;
use crate::dynamic::{
    by_name, containing_oneof, desc, has_presence, has_unknown, is_map, key_text, list, map,
    map_key, map_value, message, message_type, new_message, set_fields, value, which_oneof,
};
use crate::equal::{index_keys, item_key, key_fd_of, key_of};
use crate::plan::{FPlan, Kind, Plan, Plans, plan_for};

/// Reports whether d can be applied to a.
///
/// It refuses a diff that adds a key the list has, or removes or patches one
/// it lacks; has two entries for one key, or a change wrapper with no op;
/// has a wrapper key that differs from its add's key, or a patch that
/// changes a keyed item's key; clears a field without explicit presence, or
/// both sets and clears one field; has an order field that doesn't name
/// exactly the resulting keys, once each; in an indexed list, has a patch or
/// remove index outside [0, n) or an insert index outside [0, n], a second
/// insert at one index, two of patch and remove at one index, or an insert
/// with no elements; leaves an optional key unset in an add; or has unknown
/// fields, as a may not either.
///
/// # Errors
///
/// The refusal, with the path to what is refused.
pub fn validate_dynamic(a: &DynamicMessage, d: &DynamicMessage) -> Result<(), Error> {
    let (ps, p) = plan_for(d.pool(), d.message_index())?;
    p.bind(&ps, a.pool(), a.message_index())?;
    check_data(&ps, Some(&p), a, "a")?;
    validate_diff(&ps, &p, a, d)
}

/// validate_dynamic after a has been checked: it checks d alone, then d
/// against a.
pub(crate) fn validate_diff(
    ps: &Plans,
    p: &Plan,
    a: &DynamicMessage,
    d: &DynamicMessage,
) -> Result<(), Error> {
    if let Some(path) = find_unknown(d, "d") {
        return Err(Error::Refused(format!("{path} has unknown fields")));
    }
    validate(ps, p, a, d, "d")
}

fn refuse(text: String) -> Result<(), Error> {
    Err(Error::Refused(text))
}

fn validate(
    ps: &Plans,
    p: &Plan,
    a: &DynamicMessage,
    d: &DynamicMessage,
    at: &str,
) -> Result<(), Error> {
    let tpool = Arc::clone(a.pool());
    p.bind(ps, &tpool, a.message_index())?;
    let pool = &ps.pool;
    let amd = desc(&tpool, a);
    let mut set = HashSet::new();
    for fp in &p.fields {
        if d.has(fp.diff.fd(pool)) {
            set.insert(fp.num);
        }
    }
    for v in list(d, p.clear.fd(pool)) {
        let Value::I32(num) = *v else { continue };
        let tfd = u32::try_from(num).ok().and_then(|n| amd.field(n));
        match tfd {
            Some(tfd) if p.by_num.contains_key(&tfd.number()) => {
                if !has_presence(tfd) {
                    return refuse(format!(
                        "{at}: clears {}, which has no explicit presence; set it to its zero value instead",
                        tfd.name()
                    ));
                }
                if set.contains(&tfd.number()) {
                    return refuse(format!("{at}: both sets and clears {}", tfd.name()));
                }
            }
            _ => {
                return refuse(format!(
                    "{at}: clears field {num}, which {} doesn't have",
                    amd.full_name()
                ));
            }
        }
    }
    for fp in &p.fields {
        if !fp.present(pool, d) {
            continue;
        }
        let Some(tfd) = amd.field(fp.num) else {
            continue;
        };
        let here = format!("{at}.{}", tfd.name());
        let dfd = fp.diff.fd(pool);
        match fp.kind {
            Kind::Patch => {
                let mut base = message(&tpool, a, tfd);
                if let Some((oi, _)) = containing_oneof(amd, tfd)
                    && which_oneof(amd, a, oi) != Some(tfd.number())
                {
                    base = std::borrow::Cow::Owned(new_message(
                        &tpool,
                        message_type(tfd).unwrap_or(a.message_index()),
                    ));
                }
                if let Some(sub) = ps.sub(fp.sub) {
                    validate(ps, sub, &base, &message(pool, d, dfd), &here)?;
                }
            }
            Kind::Keyed => {
                if is_map(tfd) {
                    validate_map(ps, fp, tfd, map(a, tfd), list(d, dfd), &here)?;
                } else {
                    validate_keyed(ps, fp, &tpool, tfd, list(a, tfd), d, &here)?;
                }
            }
            Kind::Indexed => validate_indexed(ps, fp, list(a, tfd), list(d, dfd), &here)?,
            Kind::Scalar | Kind::Atomic | Kind::Replace => {}
        }
    }
    Ok(())
}

/// The op set in a change wrapper or edit, by number.
pub(crate) fn op_of(ps: &Plans, fp: &FPlan, w: &DynamicMessage) -> Option<u32> {
    let wmd = ps.pool.message(fp.wrap?);
    which_oneof(wmd, w, fp.op?)
}

/// The number of an optional field of a wrapper.
pub(crate) fn num(ps: &Plans, f: Option<crate::plan::FieldRef>) -> Option<u32> {
    f.map(|f| f.fd(&ps.pool).number())
}

/// The key a change wrapper names.
pub(crate) fn wrapper_key(ps: &Plans, fp: &FPlan, w: &DynamicMessage) -> Option<MapKey> {
    let kfd = fp.key?.fd(&ps.pool);
    key_of(kfd, &value(w, kfd))
}

/// The key field of a keyed list's items, in the target's pool.
pub(crate) fn key_field<'p>(
    tpool: &'p DescriptorPool,
    fp: &FPlan,
    tfd: &FieldDescriptor,
) -> Option<&'p FieldDescriptor> {
    let imd = tpool.message(message_type(tfd)?);
    by_name(imd, &fp.key_src).map(|i| &imd.fields()[i])
}

fn validate_keyed(
    ps: &Plans,
    fp: &FPlan,
    tpool: &Arc<DescriptorPool>,
    tfd: &FieldDescriptor,
    a: &[Value],
    d: &DynamicMessage,
    at: &str,
) -> Result<(), Error> {
    let Some(key_fd) = key_field(tpool, fp, tfd) else {
        return Ok(());
    };
    let pool = &ps.pool;
    let have = index_keys(a, key_fd).unwrap_or_default();
    let mut result: HashSet<MapKey> = have.keys().cloned().collect();
    let mut seen = HashSet::new();
    let (add, remove, patch) = (num(ps, fp.add), num(ps, fp.remove), num(ps, fp.patch));
    for w in list(d, fp.diff.fd(pool)) {
        let Value::Message(w) = w else { continue };
        let Some(k) = wrapper_key(ps, fp, w) else {
            continue;
        };
        let here = format!("{at}[{}]", key_text(&k));
        if !seen.insert(k.clone()) {
            return refuse(format!("{here}: two changes for one key"));
        }
        let pos = have.get(&k).copied();
        let op = op_of(ps, fp, w);
        let wmd = desc(pool, w);
        let op_fd = |n: u32| wmd.field(n);
        match op {
            None => return refuse(format!("{here}: a change with no op")),
            Some(o) if Some(o) == add => {
                if pos.is_some() {
                    return refuse(format!("{here}: adds a key the list has"));
                }
                let Some(ofd) = op_fd(o) else { continue };
                let item = message(pool, w, ofd);
                if has_presence(key_fd) && !item.has(key_fd_of(&item, key_fd)) {
                    return refuse(format!(
                        "{here}: the add leaves the key {} unset",
                        key_fd.name()
                    ));
                }
                let got = item_key(&item, key_fd);
                if got.as_ref() != Some(&k) {
                    return refuse(format!(
                        "{here}: the add's key is {}",
                        got.as_ref().map(key_text).unwrap_or_default()
                    ));
                }
                check_data(ps, ps.sub(fp.sub), &item, &here)?;
                result.insert(k);
            }
            Some(o) if Some(o) == remove => {
                if pos.is_none() {
                    return refuse(format!("{here}: removes a key the list lacks"));
                }
                result.remove(&k);
            }
            Some(o) if Some(o) == patch => {
                let Some(pos) = pos else {
                    return refuse(format!("{here}: patches a key the list lacks"));
                };
                let Some(ofd) = op_fd(o) else { continue };
                let Value::Message(item) = &a[pos] else {
                    continue;
                };
                let p = message(pool, w, ofd);
                let Some(sub) = ps.sub(fp.sub) else {
                    if has_presence(key_fd) && !p.has(key_fd_of(&p, key_fd))
                        || item_key(&p, key_fd).as_ref() != Some(&k)
                    {
                        return refuse(format!("{here}: the patch changes the key"));
                    }
                    continue;
                };
                validate(ps, sub, item, &p, &here)?;
                check_key_patch(ps, sub, key_fd, &k, &p, &here)?;
            }
            Some(_) => {}
        }
    }
    let Some(order) = fp.order else { return Ok(()) };
    let ofd = order.fd(pool);
    if !d.has(ofd) {
        return Ok(());
    }
    let mut named = HashSet::new();
    for v in list(d, ofd) {
        let k = key_of(ofd, v);
        let ok = k.as_ref().is_some_and(|k| result.contains(k));
        if !ok || !named.insert(k) {
            return refuse(format!(
                "{at}: the order doesn't name exactly the resulting keys, once each"
            ));
        }
    }
    if named.len() != result.len() {
        return refuse(format!(
            "{at}: the order doesn't name exactly the resulting keys, once each"
        ));
    }
    Ok(())
}

/// Refuses a patch that sets or clears a keyed item's key to anything but
/// the key it has.
fn check_key_patch(
    ps: &Plans,
    sub: &Plan,
    key_fd: &FieldDescriptor,
    k: &MapKey,
    patch: &DynamicMessage,
    at: &str,
) -> Result<(), Error> {
    let pool = &ps.pool;
    if let Some(&i) = sub.by_num.get(&key_fd.number()) {
        let dfd = sub.fields[i].diff.fd(pool);
        if patch.has(dfd) && key_of(key_fd, &value(patch, dfd)).as_ref() != Some(k) {
            return refuse(format!("{at}: the patch changes the key"));
        }
    }
    for v in list(patch, sub.clear.fd(pool)) {
        if let Value::I32(n) = v
            && i64::from(*n) == i64::from(key_fd.number())
        {
            return refuse(format!("{at}: the patch changes the key"));
        }
    }
    Ok(())
}

fn validate_map(
    ps: &Plans,
    fp: &FPlan,
    tfd: &FieldDescriptor,
    a: &MapValue,
    l: &[Value],
    at: &str,
) -> Result<(), Error> {
    let pool = &ps.pool;
    let mut seen = HashSet::new();
    let (add, remove, patch) = (num(ps, fp.add), num(ps, fp.remove), num(ps, fp.patch));
    let _ = map_value(tfd);
    for w in l {
        let Value::Message(w) = w else { continue };
        let Some(kfd) = fp.key.map(|f| f.fd(pool)) else {
            continue;
        };
        let Some(k) = map_key(&value(w, kfd)) else {
            continue;
        };
        let here = format!("{at}[{}]", key_text(&k));
        if !seen.insert(k.clone()) {
            return refuse(format!("{here}: two changes for one key"));
        }
        let wmd = desc(pool, w);
        match op_of(ps, fp, w) {
            None => return refuse(format!("{here}: a change with no op")),
            Some(o) if Some(o) == add => {
                if a.get(&k).is_some() {
                    return refuse(format!("{here}: adds a key the map has"));
                }
                if let Some(ofd) = wmd.field(o) {
                    check_data(ps, ps.sub(fp.sub), &message(pool, w, ofd), &here)?;
                }
            }
            Some(o) if Some(o) == remove => {
                if a.get(&k).is_none() {
                    return refuse(format!("{here}: removes a key the map lacks"));
                }
            }
            Some(o) if Some(o) == patch => {
                let Some(Value::Message(item)) = a.get(&k) else {
                    return refuse(format!("{here}: patches a key the map lacks"));
                };
                if let (Some(sub), Some(ofd)) = (ps.sub(fp.sub), wmd.field(o)) {
                    validate(ps, sub, item, &message(pool, w, ofd), &here)?;
                }
            }
            Some(_) => {}
        }
    }
    Ok(())
}

fn validate_indexed(
    ps: &Plans,
    fp: &FPlan,
    a: &[Value],
    l: &[Value],
    at: &str,
) -> Result<(), Error> {
    let pool = &ps.pool;
    let n = a.len() as u64;
    let mut inserts = HashSet::new();
    let mut changes = HashSet::new();
    let (insert, patch) = (num(ps, fp.insert), num(ps, fp.patch));
    let Some(ifd) = fp.index.map(|f| f.fd(pool)) else {
        return Ok(());
    };
    for e in l {
        let Value::Message(e) = e else { continue };
        let idx = match value(e, ifd) {
            Value::U32(i) => u64::from(i),
            _ => 0,
        };
        let here = format!("{at}[{idx}]");
        let emd = desc(pool, e);
        match op_of(ps, fp, e) {
            None => return refuse(format!("{here}: an edit with no op")),
            Some(o) if Some(o) == insert => {
                if idx > n {
                    return refuse(format!("{at}: insert index {idx} is outside [0, {n}]"));
                }
                if !inserts.insert(idx) {
                    return refuse(format!("{here}: a second insert at one index"));
                }
                let (Some(ofd), Some(items)) = (emd.field(o), fp.items) else {
                    continue;
                };
                let ins = message(pool, e, ofd);
                let items = list(&ins, items.fd(pool));
                if items.is_empty() {
                    return refuse(format!("{here}: an insert with no elements"));
                }
                if let Some(sub) = ps.sub(fp.sub) {
                    for item in items {
                        if let Value::Message(item) = item {
                            check_data(ps, Some(sub), item, &here)?;
                        }
                    }
                }
            }
            Some(o) => {
                let name = emd.field(o).map(FieldDescriptor::name).unwrap_or_default();
                if idx >= n {
                    return refuse(format!("{at}: {name} index {idx} is outside [0, {n})"));
                }
                if !changes.insert(idx) {
                    return refuse(format!("{here}: two of patch and remove at one index"));
                }
                if Some(o) == patch
                    && let (Some(sub), Some(ofd), Some(Value::Message(item))) =
                        (ps.sub(fp.sub), emd.field(o), a.get(idx as usize))
                {
                    validate(ps, sub, item, &message(pool, e, ofd), &here)?;
                }
            }
        }
    }
    Ok(())
}

/// Refuses a target message, or an item added by a diff, with unknown
/// fields, or with a keyed list whose keys are unset or repeated. p may be
/// None for a message that is set whole.
pub(crate) fn check_data(
    ps: &Plans,
    p: Option<&Plan>,
    m: &DynamicMessage,
    at: &str,
) -> Result<(), Error> {
    if let Some(path) = find_unknown(m, at) {
        return Err(Error::Refused(format!("{path} has unknown fields")));
    }
    if let Some(e) = check_keys(ps, p, m) {
        return Err(e.at(at));
    }
    Ok(())
}

/// A check's failure below the message it was given. The path to the
/// failure is built only once there is one, as the check returns through
/// each step to it, so a message that passes costs no strings.
#[derive(Debug, Default)]
struct PathError {
    /// Innermost first.
    steps: Vec<String>,
    msg: String,
}

impl PathError {
    fn new(msg: String) -> PathError {
        PathError {
            steps: Vec::new(),
            msg,
        }
    }

    /// Adds the step the failure is under.
    fn under(mut self, step: String) -> PathError {
        self.steps.push(step);
        self
    }

    fn path(&self, at: &str) -> String {
        let mut b = at.to_string();
        for s in self.steps.iter().rev() {
            b.push_str(s);
        }
        b
    }

    /// The failure as an error, its path starting from at.
    fn at(&self, at: &str) -> Error {
        Error::Refused(format!("{}: {}", self.path(at), self.msg))
    }
}

fn check_keys(ps: &Plans, p: Option<&Plan>, m: &DynamicMessage) -> Option<PathError> {
    let p = p?;
    let tpool = Arc::clone(m.pool());
    let md = desc(&tpool, m);
    for fp in &p.fields {
        let Some(tfd) = md.field(fp.num) else {
            continue;
        };
        if !m.has(tfd) {
            continue;
        }
        let e = match fp.kind {
            Kind::Patch => check_keys(ps, ps.sub(fp.sub), &message(&tpool, m, tfd)),
            Kind::Keyed => check_keyed(ps, fp, &tpool, tfd, m),
            Kind::Indexed => check_indexed(ps, fp, list(m, tfd)),
            Kind::Scalar | Kind::Atomic | Kind::Replace => None,
        };
        if let Some(e) = e {
            return Some(e.under(format!(".{}", tfd.name())));
        }
    }
    None
}

/// Checks a keyed list's or map's items.
fn check_keyed(
    ps: &Plans,
    fp: &FPlan,
    tpool: &Arc<DescriptorPool>,
    tfd: &FieldDescriptor,
    m: &DynamicMessage,
) -> Option<PathError> {
    if is_map(tfd) {
        let sub = ps.sub(fp.sub)?;
        for (k, v) in map(m, tfd).iter() {
            if let Value::Message(v) = v
                && let Some(e) = check_keys(ps, Some(sub), v)
            {
                return Some(e.under(format!("[{}]", key_text(k))));
            }
        }
        return None;
    }
    let key_fd = key_field(tpool, fp, tfd)?;
    let l = list(m, tfd);
    let mut seen = HashSet::with_capacity(l.len());
    for (i, item) in l.iter().enumerate() {
        let Value::Message(item) = item else { continue };
        if has_presence(key_fd) && !item.has(key_fd_of(item, key_fd)) {
            return Some(
                PathError::new(format!("the key {} is unset", key_fd.name()))
                    .under(format!("[{i}]")),
            );
        }
        let Some(k) = item_key(item, key_fd) else {
            continue;
        };
        if !seen.insert(k.clone()) {
            return Some(PathError::new(format!(
                "two items have the key {}",
                key_text(&k)
            )));
        }
        if let Some(e) = check_keys(ps, ps.sub(fp.sub), item) {
            return Some(e.under(format!("[{}]", key_text(&k))));
        }
    }
    None
}

/// Checks an indexed list's items.
fn check_indexed(ps: &Plans, fp: &FPlan, l: &[Value]) -> Option<PathError> {
    let sub = ps.sub(fp.sub)?;
    for (i, item) in l.iter().enumerate() {
        if let Value::Message(item) = item
            && let Some(e) = check_keys(ps, Some(sub), item)
        {
            return Some(e.under(format!("[{i}]")));
        }
    }
    None
}

/// Returns the path of the first message under m with unknown fields.
pub(crate) fn find_unknown(m: &DynamicMessage, at: &str) -> Option<String> {
    unknown_in(m).map(|e| e.path(at))
}

/// Returns the steps to the first message under m with unknown fields.
fn unknown_in(m: &DynamicMessage) -> Option<PathError> {
    if has_unknown(m) {
        return Some(PathError::default());
    }
    let md = m.message_descriptor();
    for n in set_fields(m) {
        let (Some(fd), Some(v)) = (md.field(n), m.field_by_number(n)) else {
            continue;
        };
        let e = match v {
            Value::Map(mv) => mv.iter().find_map(|(k, mv)| match mv {
                Value::Message(x) => unknown_in(x).map(|e| e.under(format!("[{}]", key_text(k)))),
                _ => None,
            }),
            Value::List(l) => l.iter().enumerate().find_map(|(i, x)| match x {
                Value::Message(x) => unknown_in(x).map(|e| e.under(format!("[{i}]"))),
                _ => None,
            }),
            Value::Message(x) => unknown_in(x),
            _ => None,
        };
        if let Some(e) = e {
            return Some(e.under(format!(".{}", fd.name())));
        }
    }
    None
}
