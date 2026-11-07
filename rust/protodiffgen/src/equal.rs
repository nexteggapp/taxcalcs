//! Comparing messages under the list rules: equal.go.

use std::collections::HashMap;
use std::sync::Arc;

use buffa_descriptor::{
    DescriptorPool, DynamicMessage, FieldDescriptor, MapKey, MapValue, ReflectMessage, ScalarType,
    SingularKind, Value,
};

use crate::dynamic::{
    by_name, desc, has_presence, is_list, is_map, key_to_value, map, map_key, map_value, message,
    message_type, singular, value,
};
use crate::plan::{FPlan, Kind, Rules};

/// Reports whether x and y are equal under the list rules of diff, a message
/// of the diff type; diff's contents are ignored.
///
/// An UNORDERED keyed list compares as a set by key, an ORDERED one in
/// order, and INDEXED and REPLACE lists and maps element by element,
/// recursively. Floats compare by their bits. Extension fields are not
/// compared.
pub fn equal_dynamic(x: &DynamicMessage, y: &DynamicMessage, diff: &DynamicMessage) -> bool {
    crate::plan::rules_for(diff.pool(), diff.message_index()).equal(x, y)
}

impl Rules {
    pub(crate) fn equal(&self, x: &DynamicMessage, y: &DynamicMessage) -> bool {
        let (xpool, ypool) = (Arc::clone(x.pool()), Arc::clone(y.pool()));
        let (xmd, ymd) = (desc(&xpool, x), desc(&ypool, y));
        if xmd.full_name() != ymd.full_name() {
            return false;
        }
        let p = self.plan(xmd.full_name());
        for fd in xmd.fields() {
            let Some(yfd) = ymd.field(fd.number()) else {
                return false;
            };
            if has_presence(fd) && x.has(fd) != y.has(yfd) {
                return false;
            }
            if is_map(fd) {
                if !self.equal_map(&xpool, fd, map(x, fd), map(y, yfd)) {
                    return false;
                }
            } else if is_list(fd) {
                let fp: Option<&FPlan> =
                    p.and_then(|p| p.by_num.get(&fd.number()).map(|&i| &p.fields[i]));
                let (xl, yl) = (crate::dynamic::list(x, fd), crate::dynamic::list(y, yfd));
                match fp {
                    Some(fp) if fp.kind == Kind::Keyed && fp.order.is_none() => {
                        if !self.equal_set(&xpool, fd, &fp.key_src, xl, yl) {
                            return false;
                        }
                    }
                    _ => {
                        if !self.equal_list(fd, xl, yl) {
                            return false;
                        }
                    }
                }
            } else if message_type(fd).is_some() {
                let (xm, ym) = (message(&xpool, x, fd), message(&ypool, y, yfd));
                if !self.equal(&xm, &ym) {
                    return false;
                }
            } else if !self.equal_value(fd, &value(x, fd), &value(y, yfd)) {
                return false;
            }
        }
        unknown_bytes(x) == unknown_bytes(y)
    }

    /// Compares two values of field fd, or two elements of it.
    pub(crate) fn equal_value(&self, fd: &FieldDescriptor, x: &Value, y: &Value) -> bool {
        self.equal_kind(singular(fd), x, y)
    }

    pub(crate) fn equal_kind(&self, kind: Option<SingularKind>, x: &Value, y: &Value) -> bool {
        match (kind, x, y) {
            (Some(SingularKind::Message(_)), Value::Message(a), Value::Message(b)) => {
                self.equal(a, b)
            }
            (Some(SingularKind::Scalar(ScalarType::Float)), Value::F32(a), Value::F32(b)) => {
                a.to_bits() == b.to_bits()
            }
            (Some(SingularKind::Scalar(ScalarType::Double)), Value::F64(a), Value::F64(b)) => {
                a.to_bits() == b.to_bits()
            }
            _ => x == y,
        }
    }

    pub(crate) fn equal_list(&self, fd: &FieldDescriptor, x: &[Value], y: &[Value]) -> bool {
        if x.len() != y.len() {
            return false;
        }
        x.iter().zip(y).all(|(a, b)| self.equal_value(fd, a, b))
    }

    fn equal_map(
        &self,
        _pool: &Arc<DescriptorPool>,
        fd: &FieldDescriptor,
        x: &MapValue,
        y: &MapValue,
    ) -> bool {
        if x.len() != y.len() {
            return false;
        }
        let vk = map_value(fd);
        x.iter().all(|(k, v)| match y.get(k) {
            Some(w) => self.equal_kind(vk, v, w),
            None => false,
        })
    }

    /// Compares two keyed lists as sets by key. Lists with a repeated or
    /// unset key compare element by element.
    fn equal_set(
        &self,
        pool: &Arc<DescriptorPool>,
        fd: &FieldDescriptor,
        key: &str,
        x: &[Value],
        y: &[Value],
    ) -> bool {
        if x.len() != y.len() {
            return false;
        }
        let Some(item) = message_type(fd) else {
            return self.equal_list(fd, x, y);
        };
        let imd = pool.message(item);
        let Some(kpos) = by_name(imd, key) else {
            return self.equal_list(fd, x, y);
        };
        let key_fd = &imd.fields()[kpos];
        let (Some(xm), Some(ym)) = (index_keys(x, key_fd), index_keys(y, key_fd)) else {
            return self.equal_list(fd, x, y);
        };
        for (k, &i) in &xm {
            let Some(&j) = ym.get(k) else {
                return false;
            };
            if !self.equal_value(fd, &x[i], &y[j]) {
                return false;
            }
        }
        true
    }
}

/// m's unknown fields, encoded.
fn unknown_bytes(m: &DynamicMessage) -> Vec<u8> {
    let mut b = Vec::new();
    m.unknown_fields().write_to(&mut b);
    b
}

/// Maps each item's key to its position. It returns None if a key is unset
/// or repeated.
pub(crate) fn index_keys(l: &[Value], key_fd: &FieldDescriptor) -> Option<HashMap<MapKey, usize>> {
    let mut m = HashMap::with_capacity(l.len());
    for (i, v) in l.iter().enumerate() {
        let Value::Message(item) = v else {
            return None;
        };
        if has_presence(key_fd) && !item.has(key_fd_of(item, key_fd)) {
            return None;
        }
        let k = item_key(item, key_fd)?;
        if m.insert(k, i).is_some() {
            return None;
        }
    }
    Some(m)
}

/// The descriptor of key_fd's field in item's own type: the same field,
/// found again in case item comes from another pool.
pub(crate) fn key_fd_of<'m>(
    item: &'m DynamicMessage,
    key_fd: &'m FieldDescriptor,
) -> &'m FieldDescriptor {
    item.message_descriptor()
        .field(key_fd.number())
        .unwrap_or(key_fd)
}

/// An item's key: keyOf of its key field.
pub(crate) fn item_key(item: &DynamicMessage, key_fd: &FieldDescriptor) -> Option<MapKey> {
    key_of(key_fd, &value(item, key_fd_of(item, key_fd)))
}

/// Returns a key value as a map key. An enum key is held as its number.
pub(crate) fn key_of(fd: &FieldDescriptor, v: &Value) -> Option<MapKey> {
    match (singular(fd), v) {
        (Some(SingularKind::Enum(_)), Value::EnumNumber(n)) => Some(MapKey::I32(*n)),
        _ => map_key(v),
    }
}

/// Turns a map key back into a value of field fd.
pub(crate) fn key_value(fd: &FieldDescriptor, k: &MapKey) -> Value {
    match (singular(fd), k) {
        (Some(SingularKind::Enum(_)), MapKey::I32(n)) => Value::EnumNumber(*n),
        _ => key_to_value(k),
    }
}

/// Orders keys: strings by bytes, numbers by value, false before true.
pub(crate) fn less_key(a: &MapKey, b: &MapKey) -> bool {
    match (a, b) {
        (MapKey::String(x), MapKey::String(y)) => x.as_bytes() < y.as_bytes(),
        (MapKey::Bool(x), MapKey::Bool(y)) => !x && *y,
        _ => a < b,
    }
}
