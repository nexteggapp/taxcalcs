//! Where two diffs of one base both change something: overlap.go.

use std::collections::{HashMap, HashSet};
use std::fmt;

use buffa_descriptor::{DynamicMessage, MapKey, ReflectMessage, Value};

use crate::Error;
use crate::dynamic::{containing_oneof, key_text, list, message};
use crate::plan::{FPlan, Kind, Plan, Plans, plan_for};
use crate::validate::{num, op_of, wrapper_key};

/// One place two diffs overlap: an item's path from the root, such as
/// members[ada], and the field there, or 0 for the whole item.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Item {
    path: Vec<Step>,
    field: u32,
}

impl Item {
    /// The list items from the root to the item. It is empty for the root.
    pub fn path(&self) -> Vec<Step> {
        self.path.clone()
    }

    /// The number of the item's field both diffs change, or 0 when they
    /// overlap on the whole item.
    pub fn field(&self) -> u32 {
        self.field
    }
}

/// The item as a path, such as "members[ada] field 2".
impl fmt::Display for Item {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let parts: Vec<String> = self.path.iter().map(Step::to_string).collect();
        let mut out = parts.join(".");
        if self.field != 0 {
            if !out.is_empty() {
                out.push(' ');
            }
            out.push_str(&format!("field {}", self.field));
        }
        if out.is_empty() {
            return f.write_str("root");
        }
        f.write_str(&out)
    }
}

/// One list item on a path: by key in a keyed list, where an enum key is
/// held as its number, or by base index in an indexed one.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Step {
    list: String,
    key: Option<MapKey>,
    index: i64,
}

impl Step {
    /// The full name of the list field, such as "example.v1.Team.members".
    pub fn list(&self) -> &str {
        &self.list
    }

    /// The item's key; None for a step in an indexed list.
    pub fn key(&self) -> Option<&MapKey> {
        self.key.as_ref()
    }

    /// The item's base index in an indexed list, or -1 for a step in a
    /// keyed list.
    pub fn index(&self) -> i64 {
        self.index
    }
}

/// Whether s matches `^[A-Za-z0-9_.\-]*$`.
fn plain_key(s: &str) -> bool {
    s.bytes()
        .all(|c| c.is_ascii_alphanumeric() || c == b'_' || c == b'.' || c == b'-')
}

/// The step, such as "members[ada]" or "addresses[2]".
impl fmt::Display for Step {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let name = self.list.rsplit('.').next().unwrap_or(&self.list);
        if self.index >= 0 {
            return write!(f, "{name}[{}]", self.index);
        }
        let k = match &self.key {
            Some(MapKey::String(s)) if s.is_empty() || !plain_key(s) => go_quote(s),
            Some(k) => key_text(k),
            None => "<nil>".to_string(),
        };
        write!(f, "{name}[{k}]")
    }
}

/// s as Go's strconv.Quote writes it: in double quotes, with Go escapes
/// for quotes, backslashes and control characters. Other characters are
/// kept, which matches strconv for every printable character.
pub(crate) fn go_quote(s: &str) -> String {
    let mut out = String::with_capacity(s.len() + 2);
    out.push('"');
    for c in s.chars() {
        match c {
            '"' => out.push_str("\\\""),
            '\\' => out.push_str("\\\\"),
            '\u{7}' => out.push_str("\\a"),
            '\u{8}' => out.push_str("\\b"),
            '\u{c}' => out.push_str("\\f"),
            '\n' => out.push_str("\\n"),
            '\r' => out.push_str("\\r"),
            '\t' => out.push_str("\\t"),
            '\u{b}' => out.push_str("\\v"),
            c if (c as u32) < 0x20 || c == '\u{7f}' => {
                out.push_str(&format!("\\x{:02x}", c as u32))
            }
            c if ('\u{80}'..='\u{9f}').contains(&c) || c == '\u{ad}' => {
                out.push_str(&format!("\\u{:04x}", c as u32))
            }
            c if matches!(c, '\u{2028}' | '\u{2029}' | '\u{feff}')
                || ('\u{200b}'..='\u{200f}').contains(&c) =>
            {
                out.push_str(&format!("\\u{:04x}", c as u32))
            }
            c => out.push(c),
        }
    }
    out.push('"');
    out
}

/// Lists where two diffs of one base both change something.
///
/// An item is the root message or one keyed list item. Two diffs overlap
/// when both change the same field of the same item, or when one adds,
/// removes or replaces an ancestor of what the other changes. Setting an
/// atomic message, a REPLACE list or a new oneof case replaces everything
/// inside it. Changes inside a singular message count as changes to the
/// item's field that holds it. In an indexed list, two diffs overlap at a
/// base index when both patch or remove it, both insert at it, or one
/// patches or removes it and the other inserts at it.
///
/// # Errors
///
/// When the diffs are of different types, or not patch bodies.
pub fn overlap_dynamic(d1: &DynamicMessage, d2: &DynamicMessage) -> Result<Vec<Item>, Error> {
    if !std::sync::Arc::ptr_eq(d1.pool(), d2.pool()) || d1.message_index() != d2.message_index() {
        return Err(Error::Mismatch(format!(
            "d1 is {} but d2 is {}",
            d1.message_descriptor().full_name(),
            d2.message_descriptor().full_name()
        )));
    }
    let (ps, p) = plan_for(d1.pool(), d1.message_index())?;
    let mut o = Overlaps::default();
    o.message(&ps, &p, d1, d2, &[], 0);
    Ok(o.items)
}

/// An item as a value that can be compared: its field and each step's list,
/// key and index.
type ItemKey = (u32, Vec<(String, Option<MapKey>, i64)>);

#[derive(Default)]
struct Overlaps {
    items: Vec<Item>,
    seen: HashSet<ItemKey>,
}

impl Overlaps {
    fn add(&mut self, path: &[Step], field: u32) {
        let key = (
            field,
            path.iter()
                .map(|s| (s.list.clone(), s.key.clone(), s.index))
                .collect(),
        );
        if self.seen.insert(key) {
            self.items.push(Item {
                path: path.to_vec(),
                field,
            });
        }
    }

    /// Compares two patches of one item. top is the item's field that holds
    /// them, or 0 when they patch the item itself.
    fn message(
        &mut self,
        ps: &Plans,
        p: &Plan,
        x: &DynamicMessage,
        y: &DynamicMessage,
        path: &[Step],
        top: u32,
    ) {
        let pool = &ps.pool;
        let (cx, cy) = (clears(ps, p, x), clears(ps, p, y));
        let at = |fp: &FPlan| if top != 0 { top } else { fp.num };
        let touched = |m: &DynamicMessage, c: &HashSet<u32>, fp: &FPlan| {
            m.has(fp.diff.fd(pool)) || c.contains(&fp.num)
        };
        // Inside a singular message, any two changes outside list items are
        // changes to the one field of the item that holds it.
        if top != 0 && direct(ps, p, x) && direct(ps, p, y) {
            self.add(path, top);
        }
        for fp in &p.fields {
            let dfd = fp.diff.fd(pool);
            let (hx, hy) = (x.has(dfd), y.has(dfd));
            match fp.kind {
                Kind::Scalar | Kind::Atomic | Kind::Replace => {
                    if touched(x, &cx, fp) && touched(y, &cy, fp) {
                        self.add(path, at(fp));
                    }
                }
                Kind::Patch => {
                    if (cx.contains(&fp.num) && touched(y, &cy, fp)) || (cy.contains(&fp.num) && hx)
                    {
                        self.add(path, at(fp));
                    } else if hx
                        && hy
                        && let Some(sub) = ps.sub(fp.sub)
                    {
                        self.message(
                            ps,
                            sub,
                            &message(pool, x, dfd),
                            &message(pool, y, dfd),
                            path,
                            at(fp),
                        );
                    }
                }
                Kind::Keyed => {
                    self.keyed(ps, fp, list(x, dfd), list(y, dfd), path);
                    if let Some(order) = fp.order {
                        let ofd = order.fd(pool);
                        if x.has(ofd) && y.has(ofd) {
                            self.add(path, at(fp));
                        }
                    }
                }
                Kind::Indexed => self.indexed(ps, fp, list(x, dfd), list(y, dfd), path),
            }
        }
        // Two diffs that change different cases of one oneof overlap.
        let body = p.body_desc(pool);
        for i in 0..body.oneofs().len() {
            let (mut fx, mut fy): (Option<&FPlan>, Option<&FPlan>) = (None, None);
            for fp in &p.fields {
                if containing_oneof(body, fp.diff.fd(pool)).map(|(oi, _)| oi) != Some(i) {
                    continue;
                }
                if touched(x, &cx, fp) && fx.is_none() {
                    fx = Some(fp);
                }
                if touched(y, &cy, fp) && fy.is_none() {
                    fy = Some(fp);
                }
            }
            if let (Some(fx), Some(fy)) = (fx, fy)
                && fx.num != fy.num
            {
                self.add(path, at(fx));
            }
        }
    }

    fn keyed(&mut self, ps: &Plans, fp: &FPlan, x: &[Value], y: &[Value], path: &[Step]) {
        let pool = &ps.pool;
        let mut ym: HashMap<MapKey, &DynamicMessage> = HashMap::new();
        for w in y {
            if let Value::Message(w) = w
                && let Some(k) = wrapper_key(ps, fp, w)
            {
                ym.insert(k, w);
            }
        }
        let patch = num(ps, fp.patch);
        for wx in x {
            let Value::Message(wx) = wx else { continue };
            let Some(k) = wrapper_key(ps, fp, wx) else {
                continue;
            };
            let Some(wy) = ym.get(&k) else { continue };
            let mut step = path.to_vec();
            step.push(Step {
                list: fp.name.clone(),
                key: Some(k),
                index: -1,
            });
            let (ox, oy) = (op_of(ps, fp, wx), op_of(ps, fp, wy));
            if let (Some(sub), Some(pf)) = (ps.sub(fp.sub), fp.patch)
                && ox == patch
                && oy == patch
                && patch.is_some()
            {
                let pfd = pf.fd(pool);
                self.message(
                    ps,
                    sub,
                    &message(pool, wx, pfd),
                    &message(pool, wy, pfd),
                    &step,
                    0,
                );
                continue;
            }
            self.add(&step, 0);
        }
    }

    fn indexed(&mut self, ps: &Plans, fp: &FPlan, x: &[Value], y: &[Value], path: &[Step]) {
        #[derive(Default, Clone, Copy)]
        struct Marks {
            insert: bool,
            change: bool,
        }
        let pool = &ps.pool;
        let Some(ifd) = fp.index.map(|f| f.fd(pool)) else {
            return;
        };
        let insert = num(ps, fp.insert);
        let index_of = |e: &DynamicMessage| match crate::dynamic::value(e, ifd) {
            Value::U32(i) => i64::from(i),
            _ => 0,
        };
        let read = |l: &[Value]| {
            let mut m: HashMap<i64, Marks> = HashMap::new();
            for e in l {
                let Value::Message(e) = e else { continue };
                let marks = m.entry(index_of(e)).or_default();
                if op_of(ps, fp, e) == insert {
                    marks.insert = true;
                } else {
                    marks.change = true;
                }
            }
            m
        };
        let (mx, my) = (read(x), read(y));
        for e in x {
            let Value::Message(e) = e else { continue };
            let idx = index_of(e);
            let (Some(a), Some(b)) = (mx.get(&idx), my.get(&idx)) else {
                continue;
            };
            if (a.change && b.change)
                || (a.insert && b.insert)
                || (a.change && b.insert)
                || (a.insert && b.change)
            {
                let mut step = path.to_vec();
                step.push(Step {
                    list: fp.name.clone(),
                    key: None,
                    index: idx,
                });
                self.add(&step, 0);
            }
        }
    }
}

/// Reports whether m changes anything outside list items.
fn direct(ps: &Plans, p: &Plan, m: &DynamicMessage) -> bool {
    let pool = &ps.pool;
    if !list(m, p.clear.fd(pool)).is_empty() {
        return true;
    }
    for fp in &p.fields {
        if fp.order.is_some_and(|o| m.has(o.fd(pool))) {
            return true;
        }
        let dfd = fp.diff.fd(pool);
        if !m.has(dfd) {
            continue;
        }
        match fp.kind {
            Kind::Scalar | Kind::Atomic | Kind::Replace => return true,
            Kind::Patch => {
                if let Some(sub) = ps.sub(fp.sub)
                    && direct(ps, sub, &message(pool, m, dfd))
                {
                    return true;
                }
            }
            Kind::Keyed | Kind::Indexed => {}
        }
    }
    false
}

fn clears(ps: &Plans, p: &Plan, m: &DynamicMessage) -> HashSet<u32> {
    list(m, p.clear.fd(&ps.pool))
        .iter()
        .filter_map(|v| match v {
            Value::I32(n) => u32::try_from(*n).ok(),
            _ => None,
        })
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    fn step(list: &str, key: Option<MapKey>, index: i64) -> Step {
        Step {
            list: list.to_string(),
            key,
            index,
        }
    }

    /// apply's TestItem: an item prints as its path and field, and a key
    /// that isn't plain is quoted as Go quotes it.
    #[test]
    fn item_strings() {
        let it = Item {
            path: vec![
                step("pdgtest.v1.Root.groups", Some(MapKey::I64(3)), -1),
                step(
                    "pdgtest.v1.Group.seats",
                    Some(MapKey::String("x y".into())),
                    -1,
                ),
            ],
            field: 0,
        };
        assert_eq!(it.to_string(), r#"groups[3].seats["x y"]"#);
        let mut path = it.path();
        path[0] = step("", None, 0);
        assert_eq!(
            it.path()[0].list(),
            "pdgtest.v1.Root.groups",
            "Path() shares its items"
        );
        assert_eq!(
            Item {
                path: vec![],
                field: 0
            }
            .to_string(),
            "root"
        );
        assert_eq!(
            Item {
                path: vec![],
                field: 7
            }
            .to_string(),
            "field 7"
        );
        let indexed = Item {
            path: vec![step("pdgtest.v1.Root.lines", None, 4)],
            field: 2,
        };
        assert_eq!(indexed.to_string(), "lines[4] field 2");
        assert_eq!(indexed.path()[0].index(), 4);
        assert!(indexed.path()[0].key().is_none());
        let keys = [
            (MapKey::String(String::new()), r#"m[""]"#),
            (MapKey::String("a.b-c_D9".into()), "m[a.b-c_D9]"),
            (
                MapKey::String("a\"b\\c\n\t\u{1}\u{7f}é".into()),
                r#"m["a\"b\\c\n\t\x01\x7fé"]"#,
            ),
            (MapKey::Bool(false), "m[false]"),
            (MapKey::U64(18446744073709551615), "m[18446744073709551615]"),
            (MapKey::I32(-2), "m[-2]"),
        ];
        for (k, want) in keys {
            assert_eq!(step("x.m", Some(k), -1).to_string(), want);
        }
    }
}
