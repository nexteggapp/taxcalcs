//! Field paths into a result: path.go.

use std::fmt;

use buffa_descriptor::{
    DynamicMessage, FieldDescriptor, FieldKind, ReflectMessage as _, SingularKind, Value,
};

use crate::{Error, goquote, reflect};

/// Path is a field path: proto field names joined by dots, a list item
/// named by its key in brackets, as docs/scenarios.md gives the grammar. A
/// key is an item's id; a jurisdiction or state in a list of them; else a
/// position from 0.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Path {
    steps: Vec<Step>,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
struct Step {
    field: String,
    key: String,
    has_key: bool,
}

/// ParsePath parses a field path, such as
/// "projection.employers[acme].year.boxes.wages". A key may hold dots and
/// slashes, as an item id does.
///
/// # Errors
///
/// [`Error::Path`] for a path that breaks the grammar.
pub fn parse_path(s: &str) -> Result<Path, Error> {
    if s.is_empty() {
        return Err(Error::Path("taxyear: an empty path".to_string()));
    }
    let b = s.as_bytes();
    let mut p = Path::default();
    let mut i = 0;
    while i < b.len() {
        let mut j = i;
        while j < b.len() && b[j] != b'.' && b[j] != b'[' {
            j += 1;
        }
        let mut st = Step {
            field: s[i..j].to_string(),
            ..Step::default()
        };
        if st.field.is_empty() {
            return Err(Error::Path(format!(
                "taxyear: {} has an empty step at {i}",
                goquote::quote(s)
            )));
        }
        if j < b.len() && b[j] == b'[' {
            let Some(end) = b[j..].iter().position(|&c| c == b']') else {
                return Err(Error::Path(format!(
                    "taxyear: {} has an unclosed [",
                    goquote::quote(s)
                )));
            };
            st.key = s[j + 1..j + end].to_string();
            st.has_key = true;
            j += end + 1;
        }
        p.steps.push(st);
        if j == b.len() {
            return Ok(p);
        }
        if b[j] != b'.' {
            return Err(Error::Path(format!(
                "taxyear: {} has {} after a key",
                goquote::quote(s),
                goquote::quote_rune(char::from(b[j]))
            )));
        }
        i = j + 1;
        if i == b.len() {
            return Err(Error::Path(format!(
                "taxyear: {} ends with a dot",
                goquote::quote(s)
            )));
        }
    }
    Ok(p)
}

impl fmt::Display for Path {
    /// Writes the path back.
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        for (i, s) in self.steps.iter().enumerate() {
            if i > 0 {
                f.write_str(".")?;
            }
            f.write_str(&s.field)?;
            if s.has_key {
                write!(f, "[{}]", s.key)?;
            }
        }
        Ok(())
    }
}

/// keyFields are the fields a list item's key is read from, in order.
const KEY_FIELDS: [&str; 6] = ["id", "jurisdiction", "state", "scenario", "path", "person"];

/// lookup follows a path into a message and returns the value it names
/// and that value's field.
pub(crate) fn lookup(m: &DynamicMessage, p: &Path) -> Option<(Value, &'static FieldDescriptor)> {
    enum Found<'a> {
        Item(&'a Value),
        Message(&'a DynamicMessage),
        Owned(Value),
    }
    let mut cur = Some(m);
    let mut found: Option<(Found<'_>, &'static FieldDescriptor)> = None;
    for (i, s) in p.steps.iter().enumerate() {
        let m = cur?;
        let fd = reflect::field_of(m, &s.field)?;
        match fd.kind() {
            FieldKind::List(_) => {
                let l = reflect::list(m, fd);
                if !s.has_key {
                    if i != p.steps.len() - 1 {
                        return None;
                    }
                    return Some((Value::List(l.to_vec()), fd));
                }
                let el = element(l, fd, &s.key)?;
                cur = reflect::as_message(el);
                found = Some((Found::Item(el), fd));
            }
            _ if s.has_key => return None,
            FieldKind::Singular(SingularKind::Message(_)) => {
                let sub = reflect::message(m, fd)?;
                cur = Some(sub);
                found = Some((Found::Message(sub), fd));
            }
            _ => {
                cur = None;
                found = Some((Found::Owned(m.get(fd).to_owned()), fd));
            }
        }
    }
    let (f, fd) = found?;
    let v = match f {
        Found::Item(v) => v.clone(),
        Found::Message(m) => Value::Message(m.clone()),
        Found::Owned(v) => v,
    };
    Some((v, fd))
}

/// element finds a list item by its key.
fn element<'a>(l: &'a [Value], fd: &FieldDescriptor, key: &str) -> Option<&'a Value> {
    if let Some(idx) = reflect::message_type(fd)
        && let Some(d) = reflect::desc_of(idx)
    {
        for name in KEY_FIELDS {
            let Some(kf) = reflect::field(d, name) else {
                continue;
            };
            if !reflect::is_string(kf) {
                continue;
            }
            return l
                .iter()
                .find(|v| reflect::as_message(v).is_some_and(|m| reflect::string(m, kf) == key));
        }
    }
    let n: i64 = key.parse().ok()?;
    let n = usize::try_from(n).ok()?;
    l.get(n)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_and_writes_back() {
        for s in [
            "projection.employers[acme].year.boxes.wages",
            "projection.employers[employers/acme/2026-10-15/regular].paycheck.gross",
            "estimated[US].tax_for_year",
            "a[x.y].b",
        ] {
            assert_eq!(parse_path(s).map(|p| p.to_string()), Ok(s.to_string()));
        }
        for (s, want) in [
            ("", "taxyear: an empty path"),
            ("a..b", "taxyear: \"a..b\" has an empty step at 2"),
            ("a[b", "taxyear: \"a[b\" has an unclosed ["),
            ("a[b]c", "taxyear: \"a[b]c\" has 'c' after a key"),
            ("a.", "taxyear: \"a.\" ends with a dot"),
            (".a", "taxyear: \".a\" has an empty step at 0"),
        ] {
            assert_eq!(
                parse_path(s).map_err(|e| e.to_string()),
                Err(want.to_string()),
                "{s}"
            );
        }
    }
}
