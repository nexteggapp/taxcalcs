//! Reads the options protodiffgen puts on generated messages and fields,
//! ported from protodiffgen's `internal/schema` (the parts `apply` uses).

use buffa::Message as _;
use buffa_descriptor::{FieldDescriptor, MessageDescriptor};

/// The number of both the change and the field option.
pub(crate) const EXTENSION_NUMBER: u32 = 52000;
/// The number of every patch body's clear field.
pub(crate) const CLEAR_NUMBER: u32 = 536870911;

/// Change.Kind: what a generated message is.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(crate) enum Kind {
    Unspecified,
    Patch,
    ListChange,
    Replace,
    ListEdit,
    Other,
}

impl Kind {
    fn of(n: i32) -> Kind {
        match n {
            0 => Kind::Unspecified,
            1 => Kind::Patch,
            2 => Kind::ListChange,
            3 => Kind::Replace,
            4 => Kind::ListEdit,
            _ => Kind::Other,
        }
    }
}

/// The field option of a generated field.
#[derive(Clone, Debug, Default)]
pub(crate) struct FieldSource {
    /// The full name of the source field.
    pub(crate) source: String,
    /// Whether the field is a change wrapper's key.
    pub(crate) key: bool,
    /// Whether the field holds a list's final key order.
    pub(crate) order: bool,
    /// Whether the field is an indexed edit's base index.
    pub(crate) index: bool,
}

/// The change option of a generated message: its source and kind.
pub(crate) fn message_change(md: &MessageDescriptor) -> Option<(String, Kind)> {
    let b = option_bytes(md.options().map(|o| o.encode_to_vec()))?;
    let mut source = String::new();
    let mut kind = Kind::Unspecified;
    scan(&b, &mut |num, field| match (num, field) {
        (1, Field::Bytes(v)) => source = String::from_utf8_lossy(v).into_owned(),
        (2, Field::Varint(n)) => kind = Kind::of(n as i32),
        _ => {}
    })?;
    Some((source, kind))
}

/// The field option of a generated field.
pub(crate) fn field_source_of(fd: &FieldDescriptor) -> Option<FieldSource> {
    let b = option_bytes(fd.options().map(|o| o.encode_to_vec()))?;
    let mut s = FieldSource::default();
    scan(&b, &mut |num, field| match (num, field) {
        (1, Field::Bytes(v)) => s.source = String::from_utf8_lossy(v).into_owned(),
        (2, Field::Varint(n)) => s.key = n != 0,
        (3, Field::Varint(n)) => s.order = n != 0,
        (4, Field::Varint(n)) => s.index = n != 0,
        _ => {}
    })?;
    Some(s)
}

/// The payload of field 52000 of an options message, encoded.
fn option_bytes(encoded: Option<Vec<u8>>) -> Option<Vec<u8>> {
    let b = encoded?;
    let mut out = Vec::new();
    let mut found = false;
    scan(&b, &mut |num, field| {
        if let (EXTENSION_NUMBER, Field::Bytes(v)) = (num, field) {
            // Repeated occurrences of a message field merge, which for these
            // messages is the same as concatenating them.
            out.extend_from_slice(v);
            found = true;
        }
    })?;
    found.then_some(out)
}

/// One field read off the wire.
enum Field<'a> {
    Varint(u64),
    Bytes(&'a [u8]),
}

/// Calls f with each varint and length-delimited field of b, skipping the
/// others. It returns None when b is malformed.
fn scan<'a>(mut b: &'a [u8], f: &mut dyn FnMut(u32, Field<'a>)) -> Option<()> {
    while !b.is_empty() {
        let tag = varint(&mut b)?;
        let num = u32::try_from(tag >> 3).ok()?;
        if num == 0 {
            return None;
        }
        match tag & 7 {
            0 => f(num, Field::Varint(varint(&mut b)?)),
            1 => b = b.get(8..)?,
            2 => {
                let n = usize::try_from(varint(&mut b)?).ok()?;
                let v = b.get(..n)?;
                b = b.get(n..)?;
                f(num, Field::Bytes(v));
            }
            3 => b = skip_group(b, num)?,
            5 => b = b.get(4..)?,
            _ => return None,
        }
    }
    Some(())
}

/// The rest of b after the group numbered num, whose start tag has been read.
fn skip_group(mut b: &[u8], num: u32) -> Option<&[u8]> {
    loop {
        let tag = varint(&mut b)?;
        let n = u32::try_from(tag >> 3).ok()?;
        match tag & 7 {
            0 => {
                varint(&mut b)?;
            }
            1 => b = b.get(8..)?,
            2 => {
                let len = usize::try_from(varint(&mut b)?).ok()?;
                b = b.get(len..)?;
            }
            3 => b = skip_group(b, n)?,
            4 if n == num => return Some(b),
            5 => b = b.get(4..)?,
            _ => return None,
        }
    }
}

fn varint(b: &mut &[u8]) -> Option<u64> {
    let mut v: u64 = 0;
    for i in 0..10 {
        let (&c, rest) = b.split_first()?;
        *b = rest;
        v |= u64::from(c & 0x7f) << (7 * i);
        if c < 0x80 {
            return Some(v);
        }
    }
    None
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn scans_options() {
        // FieldOptions { deprecated: true, 52000: { source: "a.B.c" key: true }, 52000: { index: true } }
        let mut b = vec![0x18, 0x01];
        let payload = [&[0x0a, 5][..], b"a.B.c", &[0x10, 0x01]].concat();
        for p in [payload, vec![0x20, 0x01]] {
            b.extend_from_slice(&[0x82, 0xb2, 0x19, p.len() as u8]);
            b.extend_from_slice(&p);
        }
        let got = option_bytes(Some(b)).expect("the option");
        let mut s = FieldSource::default();
        scan(&got, &mut |num, field| match (num, field) {
            (1, Field::Bytes(v)) => s.source = String::from_utf8_lossy(v).into_owned(),
            (2, Field::Varint(n)) => s.key = n != 0,
            (4, Field::Varint(n)) => s.index = n != 0,
            _ => {}
        })
        .expect("well formed");
        assert_eq!(
            (s.source.as_str(), s.key, s.order, s.index),
            ("a.B.c", true, false, true)
        );
        assert!(option_bytes(Some(vec![0x18, 0x01])).is_none());
        assert!(
            option_bytes(Some(vec![0x82, 0xb2, 0x19, 9, 1])).is_none(),
            "truncated"
        );
    }
}
