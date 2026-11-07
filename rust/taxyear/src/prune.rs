//! Giving apply only the part of a message a diff touches: prune.go.
//!
//! pruned is the part of a message a diff touches, for apply to copy and
//! check instead of the whole, and how to put the rest back.
//!
//! At each message the diff patches, the part holds only the fields the
//! diff sets or clears. In a keyed list it holds only the items whose keys
//! the diff names, each pruned in turn when it is patched. A list whose
//! order the diff changes is held whole.

use std::collections::{HashMap, HashSet};
use std::sync::{Arc, Mutex};

use buffa::Message as _;
use buffa_descriptor::{DynamicMessage, FieldDescriptor, MessageDescriptor, Value};

use crate::reflect;

/// The number of protodiffgen's change and field options.
const EXTENSION_NUMBER: u32 = 52000;
/// Change.Kind's PATCH and LIST_CHANGE.
const PATCH: u64 = 1;
const LIST_CHANGE: u64 = 2;

/// A restore puts back into apply's result what was not given.
type Restore<'a> = Box<dyn Fn(&mut DynamicMessage) + 'a>;

/// pruned is the part given to apply, and its restore.
pub(crate) struct Pruned<'a> {
    pub(crate) given: DynamicMessage,
    restores: Vec<Restore<'a>>,
    m: &'a DynamicMessage,
    touched: HashSet<String>,
}

impl Pruned<'_> {
    /// restore puts what wasn't given back into apply's result, from the
    /// message it came from.
    pub(crate) fn restore(&self, r: &mut DynamicMessage) {
        for f in &self.restores {
            f(r);
        }
        put_back_untouched(r, self.m, &self.touched);
    }
}

/// body is what pruning reads from a diff message's descriptor: for each
/// of its fields, the target field it changes and how.
#[derive(Default)]
struct Body {
    fields: HashMap<String, DiffField>,
    /// ordered names the target lists whose order a field of the body sets.
    ordered: HashMap<String, &'static FieldDescriptor>,
}

#[derive(Clone, Default)]
struct DiffField {
    /// A singular patch body.
    patch: bool,
    /// A keyed list's changes.
    list: bool,
    /// For a keyed list: the change wrapper's key field, its patch field,
    /// and the key's name in the item.
    key: Option<&'static FieldDescriptor>,
    patch_fd: Option<&'static FieldDescriptor>,
    item_key: String,
}

static BODIES: Mutex<Option<HashMap<String, Arc<Body>>>> = Mutex::new(None);

fn body_of(md: &'static MessageDescriptor) -> Arc<Body> {
    if let Ok(g) = BODIES.lock()
        && let Some(b) = g.as_ref().and_then(|m| m.get(md.full_name()))
    {
        return Arc::clone(b);
    }
    let mut b = Body::default();
    for f in md.fields() {
        let src = field_source(f);
        if src.order {
            b.ordered.insert(last_name(&src.source).to_string(), f);
            continue;
        }
        let Some(fm) = reflect::message_type(f).and_then(reflect::desc_of) else {
            continue;
        };
        let change = change_kind(fm);
        if !reflect::is_list(f) && change == PATCH {
            b.fields.insert(
                f.name().to_string(),
                DiffField {
                    patch: true,
                    ..DiffField::default()
                },
            );
        } else if reflect::is_list(f) && change == LIST_CHANGE {
            b.fields.insert(f.name().to_string(), list_field(fm));
        }
    }
    let b = Arc::new(b);
    if let Ok(mut g) = BODIES.lock() {
        g.get_or_insert_with(HashMap::new)
            .insert(md.full_name().to_string(), Arc::clone(&b));
    }
    b
}

/// listField reads a keyed list's change wrapper.
fn list_field(wrapper: &'static MessageDescriptor) -> DiffField {
    let mut out = DiffField {
        list: true,
        ..DiffField::default()
    };
    for f in wrapper.fields() {
        let src = field_source(f);
        if src.key {
            out.key = Some(f);
            out.item_key = last_name(&src.source).to_string();
        } else if f.name() == "patch" {
            out.patch_fd = Some(f);
        }
    }
    out
}

fn last_name(full: &str) -> &str {
    full.rsplit('.').next().unwrap_or(full)
}

/// The field option protodiffgen puts on a generated field.
#[derive(Default)]
struct FieldSource {
    source: String,
    key: bool,
    order: bool,
}

fn field_source(fd: &FieldDescriptor) -> FieldSource {
    let mut s = FieldSource::default();
    if let Some(b) = option_bytes(fd.options().map(|o| o.encode_to_vec())) {
        scan(&b, &mut |num, field| match (num, field) {
            (1, Field::Bytes(v)) => s.source = String::from_utf8_lossy(v).into_owned(),
            (2, Field::Varint(n)) => s.key = n != 0,
            (3, Field::Varint(n)) => s.order = n != 0,
            _ => {}
        });
    }
    s
}

/// The kind of protodiffgen's change option on a generated message, 0 when
/// it has none.
fn change_kind(md: &MessageDescriptor) -> u64 {
    let mut kind = 0;
    if let Some(b) = option_bytes(md.options().map(|o| o.encode_to_vec())) {
        scan(&b, &mut |num, field| {
            if let (2, Field::Varint(n)) = (num, field) {
                kind = n;
            }
        });
    }
    kind
}

/// The payload of field 52000 of an options message, encoded.
fn option_bytes(encoded: Option<Vec<u8>>) -> Option<Vec<u8>> {
    let b = encoded?;
    let mut out = Vec::new();
    let mut found = false;
    scan(&b, &mut |num, field| {
        if let (EXTENSION_NUMBER, Field::Bytes(v)) = (num, field) {
            out.extend_from_slice(v);
            found = true;
        }
    })?;
    found.then_some(out)
}

enum Field<'a> {
    Varint(u64),
    Bytes(&'a [u8]),
}

/// Calls f with each varint and length-delimited field of b. It returns
/// None when b is malformed.
fn scan<'a>(mut b: &'a [u8], f: &mut dyn FnMut(u32, Field<'a>)) -> Option<()> {
    while !b.is_empty() {
        let tag = varint(&mut b)?;
        let num = u32::try_from(tag >> 3).ok()?;
        match tag & 7 {
            0 => f(num, Field::Varint(varint(&mut b)?)),
            1 => b = b.get(8..)?,
            2 => {
                let n = usize::try_from(varint(&mut b)?).ok()?;
                let v = b.get(..n)?;
                b = b.get(n..)?;
                f(num, Field::Bytes(v));
            }
            5 => b = b.get(4..)?,
            _ => return None,
        }
    }
    Some(())
}

fn varint(b: &mut &[u8]) -> Option<u64> {
    let mut v = 0u64;
    for i in 0..10 {
        let c = *b.get(i)?;
        v |= u64::from(c & 0x7f) << (7 * i);
        if c < 0x80 {
            *b = &b[i + 1..];
            return Some(v);
        }
    }
    None
}

/// A list item's key, comparable across lists.
fn key_of(v: &Value) -> String {
    format!("{v:?}")
}

/// prune returns the part of m that the diff d touches.
pub(crate) fn prune<'a>(m: &'a DynamicMessage, d: &'a DynamicMessage) -> Pruned<'a> {
    let mut given = match reflect::new_of(m.message_index()) {
        Some(g) => g,
        None => m.clone(),
    };
    let (Some(md), Some(dd)) = (reflect::desc(m), reflect::desc(d)) else {
        return Pruned {
            given: m.clone(),
            restores: Vec::new(),
            m,
            touched: HashSet::new(),
        };
    };
    let b = body_of(dd);
    let mut touched = HashSet::new();
    for fd in reflect::set_fields(d) {
        if fd.name() != "clear" {
            touched.insert(fd.name().to_string());
            continue;
        }
        for v in reflect::list(d, fd) {
            let n = match v {
                Value::I32(n) => i64::from(*n),
                Value::EnumNumber(n) => i64::from(*n),
                Value::I64(n) => *n,
                Value::U32(n) => i64::from(*n),
                _ => continue,
            };
            if let Some(f) = u32::try_from(n).ok().and_then(|n| md.field(n)) {
                touched.insert(f.name().to_string());
            }
        }
    }
    let mut restores: Vec<Restore<'a>> = Vec::new();
    for fd in reflect::set_fields(m) {
        if !touched.contains(fd.name()) {
            continue;
        }
        let Some(v) = m.field_by_number(fd.number()) else {
            continue;
        };
        let Some(dfd) = reflect::field(dd, fd.name()) else {
            reflect::set(&mut given, fd, v.clone());
            continue;
        };
        let df = b.fields.get(fd.name()).cloned().unwrap_or_default();
        let order = b.ordered.get(fd.name()).copied();
        let dv = reflect::message(d, dfd);
        if df.patch
            && let (Some(dv), Some(mv)) = (dv, reflect::as_message(v))
        {
            let sub = prune(mv, dv);
            reflect::set(&mut given, fd, Value::Message(sub.given.clone()));
            restores.push(Box::new(move |r: &mut DynamicMessage| {
                if reflect::has(r, fd)
                    && let Some(rm) = reflect::mutable(r, fd)
                {
                    sub.restore(rm);
                }
            }));
            continue;
        }
        if df.list && !reflect::is_map(fd) && order.is_none_or(|o| !reflect::has(d, o)) {
            let l = prune_list(
                fd,
                reflect::list(m, fd),
                reflect::list(d, dfd),
                &df,
                &mut restores,
            );
            reflect::set(&mut given, fd, Value::List(l));
            continue;
        }
        reflect::set(&mut given, fd, v.clone());
    }
    Pruned {
        given,
        restores,
        m,
        touched,
    }
}

/// pruneList gives apply the items of a keyed list whose keys the changes
/// name, each patched one pruned, and adds the restore that puts the list
/// back together: the base's items in order, each touched one as apply
/// left it or gone, then the items apply added, in its order.
fn prune_list<'a>(
    fd: &'static FieldDescriptor,
    base: &'a [Value],
    changes: &'a [Value],
    df: &DiffField,
    restores: &mut Vec<Restore<'a>>,
) -> Vec<Value> {
    let Some(key_fd) = reflect::message_type(fd)
        .and_then(reflect::desc_of)
        .and_then(|d| reflect::field(d, &df.item_key))
    else {
        return base.to_vec();
    };
    let item_key = |item: &DynamicMessage| -> String {
        use buffa_descriptor::ReflectMessage as _;
        key_of(&item.get(key_fd).to_owned())
    };
    let mut patched: HashMap<String, &'a DynamicMessage> = HashMap::new();
    let mut named: HashSet<String> = HashSet::new();
    for w in changes.iter().filter_map(reflect::as_message) {
        let Some(kf) = df.key else {
            continue;
        };
        use buffa_descriptor::ReflectMessage as _;
        let k = key_of(&w.get(kf).to_owned());
        named.insert(k.clone());
        if let Some(pf) = df.patch_fd
            && let Some(p) = reflect::message(w, pf)
        {
            patched.insert(k, p);
        }
    }
    let mut list = Vec::new();
    let mut subs: HashMap<String, Pruned<'a>> = HashMap::new();
    for item in base.iter().filter_map(reflect::as_message) {
        let k = item_key(item);
        if !named.contains(&k) {
            continue;
        }
        if let Some(p) = patched.get(&k) {
            let mut sub = prune(item, p);
            // apply finds the item by its key, which the patch leaves be.
            if let Some(kv) = item.field_by_number(key_fd.number()) {
                reflect::set(&mut sub.given, key_fd, kv.clone());
            }
            list.push(Value::Message(sub.given.clone()));
            subs.insert(k, sub);
            continue;
        }
        list.push(Value::Message(item.clone()));
    }
    restores.push(Box::new(move |r: &mut DynamicMessage| {
        let got: Vec<DynamicMessage> = reflect::list(r, fd)
            .iter()
            .filter_map(reflect::as_message)
            .cloned()
            .collect();
        let mut by_key: HashMap<String, DynamicMessage> = HashMap::new();
        for item in &got {
            by_key.insert(item_key(item), item.clone());
        }
        let mut out = Vec::new();
        let mut in_base = HashSet::new();
        for item in base.iter().filter_map(reflect::as_message) {
            let k = item_key(item);
            in_base.insert(k.clone());
            if !named.contains(&k) {
                out.push(Value::Message(item.clone()));
                continue;
            }
            if let Some(res) = by_key.get(&k) {
                let mut res = res.clone();
                if let Some(sub) = subs.get(&k) {
                    sub.restore(&mut res);
                }
                out.push(Value::Message(res));
            }
        }
        for item in &got {
            if !in_base.contains(&item_key(item)) {
                out.push(Value::Message(item.clone()));
            }
        }
        if out.is_empty() {
            reflect::clear(r, fd);
        } else {
            reflect::set(r, fd, Value::List(out));
        }
    }));
    list
}

/// putBackUntouched sets in r each field of m the diff didn't touch,
/// unless the diff touched another field of its oneof: then the diff chose
/// the case, and the old one is gone.
fn put_back_untouched(r: &mut DynamicMessage, m: &DynamicMessage, touched: &HashSet<String>) {
    let Some(md) = reflect::desc(m) else {
        return;
    };
    let mut chosen = HashSet::new();
    for name in touched {
        if let Some(f) = reflect::field(md, name)
            && let Some(o) = reflect::containing_oneof(md, f)
            && !o.is_synthetic()
        {
            chosen.insert(o.name().to_string());
        }
    }
    for fd in reflect::set_fields(m) {
        if touched.contains(fd.name()) {
            continue;
        }
        if let Some(o) = reflect::containing_oneof(md, fd)
            && !o.is_synthetic()
            && chosen.contains(o.name())
        {
            continue;
        }
        if let Some(v) = m.field_by_number(fd.number()) {
            reflect::set(r, fd, v.clone());
        }
    }
}
