//! What taxyear needs of Go's protoreflect, over buffa-descriptor's
//! [`DynamicMessage`]: the generated messages carry no reflection of their
//! own, so a message taxyear reflects over is converted to a dynamic one of
//! protodiffgen's pool, the pool of every taxcalcs schema, and back.
//!
//! Every dynamic message here is of that one pool, which lives as long as
//! the program, so a descriptor is borrowed from it rather than from the
//! message, and a message can be changed while its descriptor is held.
//!
//! Go's Range visits a generated message's set fields in declaration order,
//! the order taxyear's workings and problems come out in, so
//! [`set_fields`] does too.

use std::sync::Arc;

use buffa_descriptor::{
    DescriptorPool, DynamicMessage, FieldDescriptor, FieldKind, MessageDescriptor, MessageIndex,
    OneofDescriptor, ReflectMessage, ReflectMessageMut, ScalarType, SingularKind, Value,
};

/// The full name of the Money message.
pub(crate) const MONEY: &str = "nexteggapp.taxprotos.common.v1.Money";

/// The pool every dynamic message here is of.
pub(crate) fn pool() -> Option<&'static Arc<DescriptorPool>> {
    protodiffgen::pool().ok()
}

/// m as a dynamic message, or None when its type is not in the pool, which
/// would be a broken build.
pub(crate) fn to_dyn<M: buffa::Message + buffa::MessageName>(m: &M) -> Option<DynamicMessage> {
    DynamicMessage::try_from_message(m, Arc::clone(pool()?)).ok()
}

/// A dynamic message as the generated message M, or None when it is not
/// one.
pub(crate) fn from_dyn<M: buffa::Message + Default>(m: &DynamicMessage) -> Option<M> {
    m.to_message().ok()
}

/// An empty dynamic message of type idx.
pub(crate) fn new_of(idx: MessageIndex) -> Option<DynamicMessage> {
    Some(DynamicMessage::new(Arc::clone(pool()?), idx))
}

/// The descriptor of a message of the pool.
pub(crate) fn desc(m: &DynamicMessage) -> Option<&'static MessageDescriptor> {
    Some(pool()?.message(m.message_index()))
}

/// The descriptor of a message type of the pool.
pub(crate) fn desc_of(idx: MessageIndex) -> Option<&'static MessageDescriptor> {
    Some(pool()?.message(idx))
}

/// m's type's full name.
pub(crate) fn full_name(m: &DynamicMessage) -> &'static str {
    desc(m).map(MessageDescriptor::full_name).unwrap_or("")
}

/// The message type of a field: a message field's, or a list's elements'.
pub(crate) fn message_type(fd: &FieldDescriptor) -> Option<MessageIndex> {
    match fd.kind() {
        FieldKind::Singular(SingularKind::Message(i))
        | FieldKind::List(SingularKind::Message(i)) => Some(i),
        _ => None,
    }
}

/// The full name of a field's message type, or "".
pub(crate) fn message_name(fd: &FieldDescriptor) -> &'static str {
    message_type(fd)
        .and_then(desc_of)
        .map(MessageDescriptor::full_name)
        .unwrap_or("")
}

/// Whether a field is a singular message: Go's Kind() == MessageKind and
/// !IsList() (a map is neither).
pub(crate) fn is_message(fd: &FieldDescriptor) -> bool {
    matches!(fd.kind(), FieldKind::Singular(SingularKind::Message(_)))
}

/// Whether a field's kind is a message, singular or a list: Go's Kind() ==
/// MessageKind for anything but a map.
pub(crate) fn is_message_kind(fd: &FieldDescriptor) -> bool {
    message_type(fd).is_some()
}

/// Whether a field is a list, not a map: Go's IsList.
pub(crate) fn is_list(fd: &FieldDescriptor) -> bool {
    matches!(fd.kind(), FieldKind::List(_))
}

/// Whether a field is a map.
pub(crate) fn is_map(fd: &FieldDescriptor) -> bool {
    matches!(fd.kind(), FieldKind::Map { .. })
}

/// Whether a field is a singular string.
pub(crate) fn is_string(fd: &FieldDescriptor) -> bool {
    matches!(
        fd.kind(),
        FieldKind::Singular(SingularKind::Scalar(ScalarType::String))
    )
}

/// Whether a singular field is a Money.
pub(crate) fn is_money(fd: &FieldDescriptor) -> bool {
    is_message(fd) && message_name(fd) == MONEY
}

/// The field named name, by its proto name.
pub(crate) fn field<'a>(md: &'a MessageDescriptor, name: &str) -> Option<&'a FieldDescriptor> {
    md.fields().iter().find(|f| f.name() == name)
}

/// The field of m named name.
pub(crate) fn field_of(m: &DynamicMessage, name: &str) -> Option<&'static FieldDescriptor> {
    field(desc(m)?, name)
}

/// The oneof of md named name.
pub(crate) fn oneof<'a>(md: &'a MessageDescriptor, name: &str) -> Option<&'a OneofDescriptor> {
    md.oneofs().iter().find(|o| o.name() == name)
}

/// The oneof a field belongs to, synthetic or not.
pub(crate) fn containing_oneof<'a>(
    md: &'a MessageDescriptor,
    fd: &FieldDescriptor,
) -> Option<&'a OneofDescriptor> {
    md.oneofs().get(usize::from(fd.oneof_index()?))
}

/// The field set in m's oneof, if any.
pub(crate) fn which_oneof(
    m: &DynamicMessage,
    o: &OneofDescriptor,
) -> Option<&'static FieldDescriptor> {
    let md = desc(m)?;
    o.field_indices()
        .iter()
        .filter_map(|&i| md.fields().get(usize::from(i)))
        .find(|fd| m.has(fd))
}

/// The field set in m's oneof named name, if any.
pub(crate) fn which_named(m: &DynamicMessage, name: &str) -> Option<&'static FieldDescriptor> {
    let o = oneof(desc(m)?, name)?;
    which_oneof(m, o)
}

/// m's set fields, in declaration order, as Go's Range visits them.
pub(crate) fn set_fields(m: &DynamicMessage) -> Vec<&'static FieldDescriptor> {
    let Some(md) = desc(m) else {
        return Vec::new();
    };
    md.fields().iter().filter(|fd| m.has(fd)).collect()
}

/// m's message field fd, if set.
pub(crate) fn message<'m>(
    m: &'m DynamicMessage,
    fd: &FieldDescriptor,
) -> Option<&'m DynamicMessage> {
    match m.field_by_number(fd.number()) {
        Some(Value::Message(v)) => Some(v),
        _ => None,
    }
}

/// m's message field named name, if set.
pub(crate) fn message_named<'m>(m: &'m DynamicMessage, name: &str) -> Option<&'m DynamicMessage> {
    message(m, field_of(m, name)?)
}

/// m's message field fd, made empty first if unset: Go's Mutable.
pub(crate) fn mutable<'m>(
    m: &'m mut DynamicMessage,
    fd: &FieldDescriptor,
) -> Option<&'m mut DynamicMessage> {
    if !matches!(m.field_by_number(fd.number()), Some(Value::Message(_))) {
        let empty = new_of(message_type(fd)?)?;
        m.try_set(fd, Value::Message(empty)).ok()?;
    }
    match m.field_mut(fd) {
        Some(Value::Message(v)) => Some(v),
        _ => None,
    }
}

/// m's message field named name, made empty first if unset.
pub(crate) fn mutable_named<'m>(
    m: &'m mut DynamicMessage,
    name: &str,
) -> Option<&'m mut DynamicMessage> {
    let fd = field_of(m, name)?;
    mutable(m, fd)
}

static EMPTY_LIST: Vec<Value> = Vec::new();

/// m's list field fd; empty when unset.
pub(crate) fn list<'m>(m: &'m DynamicMessage, fd: &FieldDescriptor) -> &'m [Value] {
    match m.field_by_number(fd.number()) {
        Some(Value::List(v)) => v,
        _ => &EMPTY_LIST,
    }
}

/// m's list field named name; empty when unset or not a field.
pub(crate) fn list_named<'m>(m: &'m DynamicMessage, name: &str) -> &'m [Value] {
    match field_of(m, name) {
        Some(fd) => list(m, fd),
        None => &EMPTY_LIST,
    }
}

/// m's list field fd, to change; made empty first if unset.
pub(crate) fn list_mut<'m>(
    m: &'m mut DynamicMessage,
    fd: &FieldDescriptor,
) -> Option<&'m mut Vec<Value>> {
    if !matches!(m.field_by_number(fd.number()), Some(Value::List(_))) {
        m.try_set(fd, Value::List(Vec::new())).ok()?;
    }
    match m.field_mut(fd) {
        Some(Value::List(v)) => Some(v),
        _ => None,
    }
}

/// m's string field fd; "" when unset.
pub(crate) fn string<'m>(m: &'m DynamicMessage, fd: &FieldDescriptor) -> &'m str {
    match m.field_by_number(fd.number()) {
        Some(Value::String(s)) => s,
        _ => "",
    }
}

/// m's string field named name; "" when unset or not a field.
pub(crate) fn string_named<'m>(m: &'m DynamicMessage, name: &str) -> &'m str {
    match field_of(m, name) {
        Some(fd) => string(m, fd),
        None => "",
    }
}

/// A value as a message, if it is one.
pub(crate) fn as_message(v: &Value) -> Option<&DynamicMessage> {
    match v {
        Value::Message(m) => Some(m),
        _ => None,
    }
}

/// A value as a message to change, if it is one.
pub(crate) fn as_message_mut(v: &mut Value) -> Option<&mut DynamicMessage> {
    match v {
        Value::Message(m) => Some(m),
        _ => None,
    }
}

/// Sets m's field fd; setting a oneof case clears the others. A value of
/// the wrong kind is dropped, as Go would panic on it.
pub(crate) fn set(m: &mut DynamicMessage, fd: &FieldDescriptor, v: Value) {
    let _ = m.try_set(fd, v);
}

/// Clears m's field fd.
pub(crate) fn clear(m: &mut DynamicMessage, fd: &FieldDescriptor) {
    let _ = m.try_clear(fd);
}

/// Whether m has the field fd set.
pub(crate) fn has(m: &DynamicMessage, fd: &FieldDescriptor) -> bool {
    m.has(fd)
}

/// A Money message's minor units, 0 when it is not set.
pub(crate) fn minor(m: &DynamicMessage) -> i64 {
    match field_of(m, "minor").and_then(|fd| m.field_by_number(fd.number())) {
        Some(Value::I64(n)) => *n,
        _ => 0,
    }
}

/// A Money message of minor units, USD, as model.Money makes it.
pub(crate) fn money_dyn(minor: i64) -> Option<DynamicMessage> {
    to_dyn(&crate::model::money(money::USCents(minor)))
}
