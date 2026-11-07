//! What the port needs of protoreflect, over buffa's [`DynamicMessage`].
//!
//! A descriptor is borrowed from its pool, so the helpers here take the
//! pool (an [`Arc`] the caller holds) rather than reaching it through a
//! message that is also being changed.

use std::borrow::Cow;
use std::sync::Arc;

use buffa_descriptor::{
    DescriptorPool, DynamicMessage, FieldDescriptor, FieldKind, MapKey, MapValue,
    MessageDescriptor, MessageIndex, OneofDescriptor, ReflectMessage, ReflectMessageMut,
    ScalarType, SingularKind, Value,
};

/// The descriptor of m, borrowed from pool, which must be m's.
pub(crate) fn desc<'p>(pool: &'p DescriptorPool, m: &DynamicMessage) -> &'p MessageDescriptor {
    pool.message(m.message_index())
}

/// The full name of a field: its message's full name and its name.
pub(crate) fn field_full_name(md: &MessageDescriptor, fd: &FieldDescriptor) -> String {
    format!("{}.{}", md.full_name(), fd.name())
}

/// The position in md's fields of the field with this proto name. Unlike
/// [`MessageDescriptor::field_by_name`], a JSON name doesn't match.
pub(crate) fn by_name(md: &MessageDescriptor, name: &str) -> Option<usize> {
    md.fields().iter().position(|f| f.name() == name)
}

/// Whether fd is a repeated field that is not a map: protoreflect's IsList.
pub(crate) fn is_list(fd: &FieldDescriptor) -> bool {
    matches!(fd.kind(), FieldKind::List(_))
}

/// Whether fd is a map.
pub(crate) fn is_map(fd: &FieldDescriptor) -> bool {
    matches!(fd.kind(), FieldKind::Map { .. })
}

/// The message type of fd: a message field's or a list's element type.
/// protoreflect's Message() is also non-nil for a map, its entry type, which
/// buffa doesn't name; [`has_message`] covers that.
pub(crate) fn message_type(fd: &FieldDescriptor) -> Option<MessageIndex> {
    match fd.kind() {
        FieldKind::Singular(SingularKind::Message(i))
        | FieldKind::List(SingularKind::Message(i)) => Some(i),
        _ => None,
    }
}

/// Whether protoreflect's Message() is non-nil for fd: a message field, a
/// list of messages, or a map.
pub(crate) fn has_message(fd: &FieldDescriptor) -> bool {
    message_type(fd).is_some() || is_map(fd)
}

/// The value type of a map field.
pub(crate) fn map_value(fd: &FieldDescriptor) -> Option<SingularKind> {
    match fd.kind() {
        FieldKind::Map { value, .. } => Some(value),
        _ => None,
    }
}

/// The singular kind of a field or of a list's elements.
pub(crate) fn singular(fd: &FieldDescriptor) -> Option<SingularKind> {
    match fd.kind() {
        FieldKind::Singular(k) | FieldKind::List(k) => Some(k),
        FieldKind::Map { .. } => None,
    }
}

/// protoreflect's Kind, as far as the port compares kinds.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(crate) enum GoKind {
    Scalar(ScalarType),
    Enum,
    Message,
}

/// The kind of fd, or of its elements.
pub(crate) fn go_kind(fd: &FieldDescriptor) -> GoKind {
    match singular(fd) {
        Some(SingularKind::Scalar(s)) => GoKind::Scalar(s),
        Some(SingularKind::Enum(_)) => GoKind::Enum,
        _ => GoKind::Message,
    }
}

/// Whether fd has explicit presence: protoreflect's HasPresence.
pub(crate) fn has_presence(fd: &FieldDescriptor) -> bool {
    !matches!(fd.kind(), FieldKind::List(_) | FieldKind::Map { .. })
        && fd.presence() != buffa::editions::FieldPresence::Implicit
}

/// The oneof fd belongs to, synthetic or not.
pub(crate) fn containing_oneof<'m>(
    md: &'m MessageDescriptor,
    fd: &FieldDescriptor,
) -> Option<(usize, &'m OneofDescriptor)> {
    let i = usize::from(fd.oneof_index()?);
    md.oneofs().get(i).map(|o| (i, o))
}

/// The number of the field set in m's oneof numbered oneof, if any.
pub(crate) fn which_oneof(md: &MessageDescriptor, m: &DynamicMessage, oneof: usize) -> Option<u32> {
    let o = md.oneofs().get(oneof)?;
    m.which_oneof(o).map(FieldDescriptor::number)
}

/// An empty message of type idx.
pub(crate) fn new_message(pool: &Arc<DescriptorPool>, idx: MessageIndex) -> DynamicMessage {
    DynamicMessage::new(Arc::clone(pool), idx)
}

/// m's message field fd, or an empty message of its type when it is unset.
pub(crate) fn message<'m>(
    pool: &Arc<DescriptorPool>,
    m: &'m DynamicMessage,
    fd: &FieldDescriptor,
) -> Cow<'m, DynamicMessage> {
    match m.field_by_number(fd.number()) {
        Some(Value::Message(v)) => Cow::Borrowed(v),
        _ => Cow::Owned(match message_type(fd) {
            Some(idx) => new_message(pool, idx),
            // Not a message field; never asked for.
            None => DynamicMessage::new(Arc::clone(pool), m.message_index()),
        }),
    }
}

static EMPTY_LIST: Vec<Value> = Vec::new();
static EMPTY_MAP: MapValue = MapValue::new();

/// m's list field fd; empty when unset.
pub(crate) fn list<'m>(m: &'m DynamicMessage, fd: &FieldDescriptor) -> &'m [Value] {
    match m.field_by_number(fd.number()) {
        Some(Value::List(v)) => v,
        _ => &EMPTY_LIST,
    }
}

/// m's map field fd; empty when unset.
pub(crate) fn map<'m>(m: &'m DynamicMessage, fd: &FieldDescriptor) -> &'m MapValue {
    match m.field_by_number(fd.number()) {
        Some(Value::Map(v)) => v,
        _ => &EMPTY_MAP,
    }
}

/// m's field fd as a value: its default when unset.
pub(crate) fn value(m: &DynamicMessage, fd: &FieldDescriptor) -> Value {
    m.get(fd).to_owned()
}

/// Sets m's field fd. Setting a oneof case clears the others.
pub(crate) fn set(m: &mut DynamicMessage, fd: &FieldDescriptor, v: Value) {
    m.set(fd, v);
}

/// m's message field fd, made empty first if unset: protoreflect's Mutable.
/// Making a oneof case clears the others.
pub(crate) fn mutable_message<'m>(
    pool: &Arc<DescriptorPool>,
    m: &'m mut DynamicMessage,
    fd: &FieldDescriptor,
) -> Option<&'m mut DynamicMessage> {
    if !matches!(m.field_by_number(fd.number()), Some(Value::Message(_))) {
        let idx = message_type(fd)?;
        m.set(fd, Value::Message(new_message(pool, idx)));
    }
    match m.field_mut(fd) {
        Some(Value::Message(v)) => Some(v),
        _ => None,
    }
}

/// A value as a map key, as protoreflect's Value.MapKey does; None for a
/// value no map key can hold.
pub(crate) fn map_key(v: &Value) -> Option<MapKey> {
    Some(match v {
        Value::Bool(b) => MapKey::Bool(*b),
        Value::I32(n) => MapKey::I32(*n),
        Value::I64(n) => MapKey::I64(*n),
        Value::U32(n) => MapKey::U32(*n),
        Value::U64(n) => MapKey::U64(*n),
        Value::String(s) => MapKey::String(s.clone()),
        _ => return None,
    })
}

/// A map key as a value: protoreflect's MapKey.Value.
pub(crate) fn key_to_value(k: &MapKey) -> Value {
    match k {
        MapKey::Bool(b) => Value::Bool(*b),
        MapKey::I32(n) => Value::I32(*n),
        MapKey::I64(n) => Value::I64(*n),
        MapKey::U32(n) => Value::U32(*n),
        MapKey::U64(n) => Value::U64(*n),
        MapKey::String(s) => Value::String(s.clone()),
    }
}

/// A key as fmt's %v prints it.
pub(crate) fn key_text(k: &MapKey) -> String {
    match k {
        MapKey::Bool(b) => b.to_string(),
        MapKey::I32(n) => n.to_string(),
        MapKey::I64(n) => n.to_string(),
        MapKey::U32(n) => n.to_string(),
        MapKey::U64(n) => n.to_string(),
        MapKey::String(s) => s.clone(),
    }
}

/// Whether m has no field set: unknown fields don't count.
pub(crate) fn is_empty(m: &DynamicMessage) -> bool {
    let mut empty = true;
    m.for_each_set(&mut |_, _| empty = false);
    empty
}

/// The numbers of m's set fields, in number order. protoreflect's Range
/// visits fields in an order of its own, which matters only for which of
/// two failures is reported first.
pub(crate) fn set_fields(m: &DynamicMessage) -> Vec<u32> {
    let mut out = Vec::new();
    m.for_each_set(&mut |fd, _| out.push(fd.number()));
    out
}

/// Whether m has unknown fields.
pub(crate) fn has_unknown(m: &DynamicMessage) -> bool {
    !m.unknown_fields().is_empty()
}
