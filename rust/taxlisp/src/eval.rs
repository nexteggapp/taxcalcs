//! The runtime evaluator for typechecked taxlisp modules, ported from the Go
//! package `taxlisp/eval`.
//!
//! Go evaluates the `Expression` protos directly. This port first lowers a
//! module into [`Node`]s, a reference-counted copy of the syntax tree with
//! each symbol interned and each special form resolved, so a closure body is
//! shared rather than copied and a variable lookup compares integers. The
//! semantics, the order of evaluation and every error message are Go's.
//!
//! Protobuf values are read and built reflectively over a
//! [`DescriptorPool`], as Go's `protoreflect` and `dynamicpb` do; a message
//! is held as a [`ProtoMsg`], whose submessages and lists are shared, so
//! reading a field or `proto-with` copies nothing below the level it changes.

use std::cell::RefCell;
use std::collections::{BTreeMap, HashMap};
use std::fmt;
use std::rc::Rc;
use std::sync::{Arc, OnceLock};

use buffa_descriptor::reflect::{ReflectMessageMut, Value as DynValue};
use buffa_descriptor::{
    DescriptorPool, DynamicMessage, EnumDescriptor, FieldDescriptor, FieldKind, MapValue,
    MessageDescriptor, MessageIndex, ScalarType, SingularKind,
};
use num_bigint::{BigInt, BigUint};
use num_integer::Integer;
use num_rational::BigRational;
use num_traits::{Signed, ToPrimitive, Zero};
use proto::taxlispv1;
use proto::taxlispv1::__buffa::oneof::atom::Primitive;
use proto::taxlispv1::__buffa::oneof::expression::Value as ExprValue;

use crate::parser::{go_format_float_g, quote};

const PROTO_MONEY_FULL_NAME: &str = "nexteggapp.taxprotos.common.v1.Money";
const PROTO_QUANTITY_FULL_NAME: &str = "nexteggapp.taxprotos.common.v1.Quantity";
const PROTO_DATE_FULL_NAME: &str = "nexteggapp.taxprotos.common.v1.Date";

crate::located_error! {
    /// An evaluation error, positioned at the failing expression when that
    /// has a source position.
    EvalError
}

static DEFAULT_POOL: OnceLock<Arc<DescriptorPool>> = OnceLock::new();

/// The descriptor pool of every schema in `proto::DESCRIPTORS`: the
/// typechecker's [`crate::types::global_files`], the counterpart of Go's
/// `protoregistry.GlobalFiles`, which an evaluator created with nil
/// registries uses.
pub fn default_pool() -> Result<&'static Arc<DescriptorPool>, EvalError> {
    if let Some(p) = DEFAULT_POOL.get() {
        return Ok(p);
    }
    let pool = crate::types::global_files().map_err(|e| EvalError::new(e.to_string()))?;
    Ok(DEFAULT_POOL.get_or_init(|| pool))
}

// ---------------------------------------------------------------------------
// Values
// ---------------------------------------------------------------------------

/// A runtime value in taxlisp. Each variant is one of Go's `Value`
/// implementations; [`Value::go_type`] is its Go type name, which `%T` in an
/// error message prints.
#[derive(Clone)]
pub enum Value {
    /// The singleton `nil` value of type `Nil` (Go's `NilVal`).
    Nil,
    /// A boolean (Go's `BoolVal`).
    Bool(bool),
    /// A 64-bit signed integer (Go's `IntVal`).
    Int(i64),
    /// A UTF-8 string (Go's `StringVal`).
    String(Rc<str>),
    /// A quoted symbol, including enum literals.
    Symbol(SymbolVal),
    /// An exact USD amount.
    Money(MoneyVal),
    /// A 64-bit IEEE-754 floating-point number (Go's `FloatVal`).
    Float(f64),
    /// A calendar date.
    Date(DateVal),
    /// An immutable slice of values.
    Slice(SliceVal),
    /// An immutable lookup table.
    Table(TableVal),
    /// An instance of a `defstruct`.
    Struct(StructVal),
    /// A protobuf message.
    ProtoMessage(ProtoMessageVal),
    /// A protobuf enum value.
    ProtoEnum(ProtoEnumVal),
    /// A closure created by `fn` or `def`.
    Closure(ClosureVal),
    /// A `defstruct` constructor.
    StructCtor(StructCtorVal),
    /// A built-in function.
    Builtin(BuiltinVal),
}

/// Nil is the singleton `nil` value.
pub const NIL: Value = Value::Nil;

impl Value {
    /// The Go type name of the value, as `%T` prints it (`eval.MoneyVal`).
    pub fn go_type(&self) -> &'static str {
        match self {
            Value::Nil => "eval.NilVal",
            Value::Bool(_) => "eval.BoolVal",
            Value::Int(_) => "eval.IntVal",
            Value::String(_) => "eval.StringVal",
            Value::Symbol(_) => "eval.SymbolVal",
            Value::Money(_) => "eval.MoneyVal",
            Value::Float(_) => "eval.FloatVal",
            Value::Date(_) => "eval.DateVal",
            Value::Slice(_) => "eval.SliceVal",
            Value::Table(_) => "eval.TableVal",
            Value::Struct(_) => "eval.StructVal",
            Value::ProtoMessage(_) => "eval.ProtoMessageVal",
            Value::ProtoEnum(_) => "eval.ProtoEnumVal",
            Value::Closure(_) => "eval.ClosureVal",
            Value::StructCtor(_) => "eval.StructCtorVal",
            Value::Builtin(_) => "eval.BuiltinVal",
        }
    }

    /// A string value.
    pub fn string(s: &str) -> Value {
        Value::String(Rc::from(s))
    }
}

impl fmt::Display for Value {
    /// Go's `String()` of each value type.
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Value::Nil => f.write_str("nil"),
            Value::Bool(b) => f.write_str(if *b { "true" } else { "false" }),
            Value::Int(i) => write!(f, "{i}"),
            Value::String(s) => f.write_str(&quote(s)),
            Value::Symbol(s) => write!(f, "'{}", s.full_name()),
            Value::Money(m) => write!(f, "#usd{}", quote(&m.format_cents())),
            Value::Float(v) => f.write_str(&format_float_val(*v)),
            Value::Date(d) => write!(f, "#date\"{:04}-{:02}-{:02}\"", d.year, d.month, d.day),
            Value::Slice(s) => {
                f.write_str("[")?;
                for (i, e) in s.elems.iter().enumerate() {
                    if i > 0 {
                        f.write_str(" ")?;
                    }
                    write!(f, "{e}")?;
                }
                f.write_str("]")
            }
            Value::Table(t) => {
                f.write_str("(table")?;
                for e in t.entries.iter() {
                    write!(f, " {} {}", e.key, e.value)?;
                }
                f.write_str(")")
            }
            Value::Struct(s) => {
                // Go prints the field map with %v: map[k:v ...], keys sorted.
                write!(f, "<{} map[", s.type_name)?;
                for (i, (k, v)) in s.fields.iter().enumerate() {
                    if i > 0 {
                        f.write_str(" ")?;
                    }
                    write!(f, "{k}:{v}")?;
                }
                f.write_str("]>")
            }
            Value::ProtoMessage(p) => write!(f, "<{}>", p.full_name()),
            Value::ProtoEnum(e) => {
                write!(f, "'{}/{}", parent_name(e.desc.full_name()), e.name())
            }
            Value::Closure(c) => {
                if c.0.name.is_empty() {
                    f.write_str("<fn>")
                } else {
                    write!(f, "<fn {}>", c.0.name)
                }
            }
            Value::StructCtor(s) => write!(f, "<struct-ctor {}>", s.name),
            Value::Builtin(b) => write!(f, "<builtin {}>", b.name),
        }
    }
}

impl fmt::Debug for Value {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{self}")
    }
}

/// Go's `FloatVal.String()`: the shortest representation, with ".0" when
/// it has neither a point nor an exponent.
fn format_float_val(v: f64) -> String {
    let mut s = go_format_float_g(v);
    if !s.contains(['.', 'e', 'E']) {
        s.push_str(".0");
    }
    s
}

/// The parent of a dotted protobuf full name (`a.b.C` -> `a.b`).
fn parent_name(full: &str) -> &str {
    match full.rfind('.') {
        Some(i) => &full[..i],
        None => "",
    }
}

/// A quoted symbol runtime value (including enum literals).
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct SymbolVal {
    /// The package alias, or "".
    pub package: Rc<str>,
    /// The symbol name.
    pub name: Rc<str>,
}

impl SymbolVal {
    /// A symbol value from its package alias and name.
    pub fn new(package: &str, name: &str) -> SymbolVal {
        SymbolVal {
            package: Rc::from(package),
            name: Rc::from(name),
        }
    }

    /// The symbol as written: `package/name`, or `name` alone.
    pub fn full_name(&self) -> String {
        if self.package.is_empty() {
            return self.name.to_string();
        }
        format!("{}/{}", self.package, self.name)
    }
}

/// An exact decimal USD monetary amount backed by a rational.
#[derive(Clone, Debug, PartialEq)]
pub struct MoneyVal {
    /// The amount in dollars.
    pub r: Rc<BigRational>,
}

impl MoneyVal {
    fn from_rat(r: BigRational) -> MoneyVal {
        MoneyVal { r: Rc::new(r) }
    }

    /// NewMoneyRat creates a MoneyVal from a rational (cloning it).
    pub fn new_rat(r: &BigRational) -> MoneyVal {
        MoneyVal::from_rat(r.clone())
    }

    /// NewMoneyCents creates a MoneyVal from integer cents.
    pub fn new_cents(cents: i64) -> MoneyVal {
        MoneyVal::from_rat(BigRational::new(BigInt::from(cents), BigInt::from(100)))
    }

    /// ParseMoney parses a decimal string into a MoneyVal, as Go's
    /// `big.Rat.SetString` does: an optionally signed decimal with an
    /// optional exponent, or a fraction `a/b`.
    pub fn parse(s: &str) -> Result<MoneyVal, EvalError> {
        match parse_rat(s.trim()) {
            Some(r) => Ok(MoneyVal::from_rat(r)),
            None => Err(EvalError::new(format!(
                "invalid money literal {}",
                quote(s)
            ))),
        }
    }

    /// Cents returns the amount rounded to the nearest cent
    /// (half-away-from-zero).
    pub fn cents(&self) -> i64 {
        let rounded = round_rat_scale(&self.r, 100);
        let num = rounded.numer() * BigInt::from(100);
        big_to_i64(&(num / rounded.denom()))
    }

    /// Dollars returns the amount rounded to the nearest whole dollar
    /// (half-away-from-zero).
    pub fn dollars(&self) -> i64 {
        let rounded = round_rat_scale(&self.r, 1);
        big_to_i64(&(rounded.numer() / rounded.denom()))
    }

    /// FormatCents formats the amount to 2 decimal places (e.g. "1250.50").
    pub fn format_cents(&self) -> String {
        format_rat_fixed(&self.r, 2)
    }
}

/// Go's `big.Int.Int64()`: the low 64 bits.
fn big_to_i64(b: &BigInt) -> i64 {
    match b.to_i64() {
        Some(v) => v,
        None => {
            let digits = b.magnitude().iter_u64_digits().next().unwrap_or(0);
            let v = digits as i64;
            if b.is_negative() { v.wrapping_neg() } else { v }
        }
    }
}

/// Parses what Go's `big.Rat.SetString` accepts for decimal input.
fn parse_rat(s: &str) -> Option<BigRational> {
    if let Some((a, b)) = s.split_once('/') {
        let n: BigInt = a.parse().ok()?;
        let d: BigInt = b.parse().ok()?;
        if d.is_zero() {
            return None;
        }
        return Some(BigRational::new(n, d));
    }
    let (neg, rest) = match s.as_bytes().first() {
        Some(b'-') => (true, &s[1..]),
        Some(b'+') => (false, &s[1..]),
        _ => (false, s),
    };
    let (mant, exp) = match rest.find(['e', 'E']) {
        Some(i) => (&rest[..i], rest[i + 1..].parse::<i64>().ok()?),
        None => (rest, 0),
    };
    let (int_part, frac_part) = match mant.split_once('.') {
        Some((a, b)) => (a, b),
        None => (mant, ""),
    };
    if int_part.is_empty() && frac_part.is_empty() {
        return None;
    }
    if !int_part
        .bytes()
        .chain(frac_part.bytes())
        .all(|c| c.is_ascii_digit())
    {
        return None;
    }
    let digits = format!("{int_part}{frac_part}");
    let n: BigInt = digits.parse().ok()?;
    let n = if neg { -n } else { n };
    Some(scale_by_pow10(n, exp - frac_part.len() as i64))
}

/// n * 10^k as a rational.
fn scale_by_pow10(n: BigInt, k: i64) -> BigRational {
    let ten = BigInt::from(10);
    if k >= 0 {
        BigRational::from_integer(n * num_traits::pow(ten, k as usize))
    } else {
        BigRational::new(n, num_traits::pow(ten, (-k) as usize))
    }
}

/// A calendar date value.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Default)]
pub struct DateVal {
    /// The year.
    pub year: i32,
    /// The month, 1-12.
    pub month: i32,
    /// The day of the month.
    pub day: i32,
}

fn compare_date(a: &DateVal, b: &DateVal) -> i32 {
    if a.year != b.year {
        return if a.year < b.year { -1 } else { 1 };
    }
    if a.month != b.month {
        return if a.month < b.month { -1 } else { 1 };
    }
    if a.day != b.day {
        return if a.day < b.day { -1 } else { 1 };
    }
    0
}

/// An immutable contiguous slice of values.
#[derive(Clone)]
pub struct SliceVal {
    /// The elements.
    pub elems: Rc<[Value]>,
}

impl SliceVal {
    /// A slice of the given elements.
    pub fn new(elems: Vec<Value>) -> SliceVal {
        SliceVal {
            elems: Rc::from(elems),
        }
    }
}

/// A key-value pair in a TableVal.
#[derive(Clone)]
pub struct TableEntry {
    /// The key.
    pub key: Value,
    /// The value.
    pub value: Value,
}

/// An immutable lookup table.
#[derive(Clone)]
pub struct TableVal {
    /// The entries, in source order.
    pub entries: Rc<[TableEntry]>,
}

/// An instance of a user-defined `defstruct`.
#[derive(Clone)]
pub struct StructVal {
    /// The struct's type name.
    pub type_name: Rc<str>,
    /// The field values by name.
    pub fields: Rc<BTreeMap<Rc<str>, Value>>,
}

/// A protobuf message value.
#[derive(Clone, PartialEq)]
pub struct ProtoMessageVal {
    /// The message.
    pub msg: Rc<ProtoMsg>,
}

impl ProtoMessageVal {
    /// The message's full protobuf name.
    pub fn full_name(&self) -> &'static str {
        self.msg.descriptor().full_name()
    }

    /// The message from a generated message, by a round trip through its
    /// encoding.
    pub fn from_message<M: buffa::Message + buffa::MessageName>(
        pool: &'static Arc<DescriptorPool>,
        m: &M,
    ) -> Result<ProtoMessageVal, EvalError> {
        let Some(idx) = pool.message_index(M::FULL_NAME) else {
            return Err(EvalError::new(format!(
                "no descriptor for {}",
                M::FULL_NAME
            )));
        };
        let bytes = m.encode_to_vec();
        let dm = DynamicMessage::decode_with_options(
            Arc::clone(pool),
            idx,
            &bytes,
            &decode_options(bytes.len()),
        )
        .map_err(|e| EvalError::new(format!("decoding {}: {e}", M::FULL_NAME)))?;
        Ok(ProtoMessageVal {
            msg: Rc::new(ProtoMsg::from_dynamic(pool, &dm)),
        })
    }

    /// The message as a generated message, by a round trip through its
    /// encoding.
    pub fn to_message<M: buffa::Message + Default>(&self) -> Result<M, EvalError> {
        let bytes = self.encode_to_vec()?;
        decode_options(bytes.len())
            .decode_from_slice(&bytes)
            .map_err(|e| EvalError::new(format!("decoding {}: {e}", self.full_name())))
    }

    /// The message's binary encoding.
    pub fn encode_to_vec(&self) -> Result<Vec<u8>, EvalError> {
        Ok(self.msg.to_dynamic()?.encode_to_vec())
    }
}

/// Decode limits for bytes this process encoded itself.
fn decode_options(len: usize) -> buffa::DecodeOptions {
    buffa::DecodeOptions::new()
        .with_element_memory_limit(len.saturating_mul(128).max(64 << 20))
        .with_unknown_field_limit(len.max(1 << 20))
}

/// A protobuf enum value.
#[derive(Clone, Copy)]
pub struct ProtoEnumVal {
    /// The enum's descriptor.
    pub desc: &'static EnumDescriptor,
    /// The value's number.
    pub number: i32,
}

impl ProtoEnumVal {
    /// The value's name, or "" for a number the enum does not declare.
    pub fn name(&self) -> &'static str {
        match self.desc.value(self.number) {
            Some(v) => v.name(),
            None => "",
        }
    }
}

/// A lexical closure created by `fn` or `def`.
#[derive(Clone)]
pub struct ClosureVal(Rc<Closure>);

struct Closure {
    name: Rc<str>,
    param_names: Rc<[SymId]>,
    body: Node,
    env: Rc<Env>,
}

/// Constructs a user-defined `defstruct` from positional arguments.
#[derive(Clone)]
pub struct StructCtorVal {
    /// The struct's name.
    pub name: Rc<str>,
    /// The field names, in declaration order.
    pub field_names: Rc<[Rc<str>]>,
}

/// The signature of a built-in function: the evaluator, the arguments and
/// the call site, for error positions.
pub type BuiltinFn = fn(&Evaluator, &[Value], Option<&Node>) -> Result<Value, EvalError>;

/// A built-in function.
#[derive(Clone, Copy)]
pub struct BuiltinVal {
    /// The function's name.
    pub name: &'static str,
    /// The function.
    pub func: BuiltinFn,
}

// ---------------------------------------------------------------------------
// Protobuf messages
// ---------------------------------------------------------------------------

/// A protobuf message held for evaluation: its descriptor and its set
/// fields by number. Submessages and repeated fields are shared, so a copy
/// is shallow; messages are never changed once built.
#[derive(Clone)]
pub struct ProtoMsg {
    pool: &'static Arc<DescriptorPool>,
    idx: MessageIndex,
    fields: BTreeMap<u32, PVal>,
}

impl PartialEq for ProtoMsg {
    /// Go's `proto.Equal`: the same message type with the same fields set to
    /// equal values.
    fn eq(&self, other: &Self) -> bool {
        self.idx == other.idx && self.fields == other.fields
    }
}

/// A field value of a [`ProtoMsg`].
#[derive(Clone, PartialEq)]
enum PVal {
    Bool(bool),
    I32(i32),
    I64(i64),
    U32(u32),
    U64(u64),
    F32(f32),
    F64(f64),
    Str(Rc<str>),
    Bytes(Rc<[u8]>),
    Enum(i32),
    Msg(Rc<ProtoMsg>),
    List(Rc<Vec<PVal>>),
    Map(Rc<MapValue>),
}

impl PVal {
    /// Whether an implicit-presence field holding this value is unset.
    fn is_default(&self) -> bool {
        match self {
            PVal::Bool(b) => !*b,
            PVal::I32(v) => *v == 0,
            PVal::I64(v) => *v == 0,
            PVal::U32(v) => *v == 0,
            PVal::U64(v) => *v == 0,
            PVal::F32(v) => v.to_bits() == 0,
            PVal::F64(v) => v.to_bits() == 0,
            PVal::Str(s) => s.is_empty(),
            PVal::Bytes(b) => b.is_empty(),
            PVal::Enum(v) => *v == 0,
            PVal::Msg(_) => false,
            PVal::List(l) => l.is_empty(),
            PVal::Map(m) => m.is_empty(),
        }
    }

    fn from_dyn(pool: &'static Arc<DescriptorPool>, v: &DynValue) -> PVal {
        match v {
            DynValue::Bool(b) => PVal::Bool(*b),
            DynValue::I32(x) => PVal::I32(*x),
            DynValue::I64(x) => PVal::I64(*x),
            DynValue::U32(x) => PVal::U32(*x),
            DynValue::U64(x) => PVal::U64(*x),
            DynValue::F32(x) => PVal::F32(*x),
            DynValue::F64(x) => PVal::F64(*x),
            DynValue::String(s) => PVal::Str(Rc::from(s.as_str())),
            DynValue::Bytes(b) => PVal::Bytes(Rc::from(b.as_slice())),
            DynValue::EnumNumber(n) => PVal::Enum(*n),
            DynValue::Message(m) => PVal::Msg(Rc::new(ProtoMsg::from_dynamic(pool, m))),
            DynValue::List(l) => {
                PVal::List(Rc::new(l.iter().map(|e| PVal::from_dyn(pool, e)).collect()))
            }
            DynValue::Map(m) => PVal::Map(Rc::new(m.clone())),
        }
    }

    fn to_dyn(&self) -> Result<DynValue, EvalError> {
        Ok(match self {
            PVal::Bool(b) => DynValue::Bool(*b),
            PVal::I32(x) => DynValue::I32(*x),
            PVal::I64(x) => DynValue::I64(*x),
            PVal::U32(x) => DynValue::U32(*x),
            PVal::U64(x) => DynValue::U64(*x),
            PVal::F32(x) => DynValue::F32(*x),
            PVal::F64(x) => DynValue::F64(*x),
            PVal::Str(s) => DynValue::String(s.to_string()),
            PVal::Bytes(b) => DynValue::Bytes(b.to_vec()),
            PVal::Enum(n) => DynValue::EnumNumber(*n),
            PVal::Msg(m) => DynValue::Message(m.to_dynamic()?),
            PVal::List(l) => {
                let mut out = Vec::with_capacity(l.len());
                for e in l.iter() {
                    out.push(e.to_dyn()?);
                }
                DynValue::List(out)
            }
            PVal::Map(m) => DynValue::Map((**m).clone()),
        })
    }
}

impl ProtoMsg {
    /// An empty message of the given type.
    fn new(pool: &'static Arc<DescriptorPool>, idx: MessageIndex) -> ProtoMsg {
        ProtoMsg {
            pool,
            idx,
            fields: BTreeMap::new(),
        }
    }

    /// The message's descriptor.
    pub fn descriptor(&self) -> &'static MessageDescriptor {
        let pool: &'static DescriptorPool = self.pool;
        pool.message(self.idx)
    }

    fn from_dynamic(pool: &'static Arc<DescriptorPool>, dm: &DynamicMessage) -> ProtoMsg {
        let mut m = ProtoMsg::new(pool, dm.message_index());
        let md = m.descriptor();
        for fd in md.fields() {
            if let Some(v) = dm.field_by_number(fd.number()) {
                let pv = PVal::from_dyn(pool, v);
                if is_implicit(fd) && pv.is_default() {
                    continue;
                }
                m.fields.insert(fd.number(), pv);
            }
        }
        m
    }

    /// The message as a buffa DynamicMessage.
    pub fn to_dynamic(&self) -> Result<DynamicMessage, EvalError> {
        let mut dm = DynamicMessage::new(Arc::clone(self.pool), self.idx);
        let md = self.descriptor();
        for (num, v) in &self.fields {
            let Some(fd) = md.field(*num) else { continue };
            dm.try_set(fd, v.to_dyn()?).map_err(|e| {
                EvalError::new(format!("setting {}.{}: {e}", md.full_name(), fd.name()))
            })?;
        }
        Ok(dm)
    }

    /// Go's `Message.Has`.
    fn has(&self, fd: &FieldDescriptor) -> bool {
        self.fields.contains_key(&fd.number())
    }

    fn get(&self, fd: &FieldDescriptor) -> Option<&PVal> {
        self.fields.get(&fd.number())
    }

    /// Go's `Message.Set`: setting a oneof member clears the others, and an
    /// implicit-presence field set to its zero value is unset.
    fn set(&mut self, fd: &FieldDescriptor, v: PVal) {
        if let Some(oi) = fd.oneof_index() {
            let md = self.descriptor();
            if let Some(od) = md.oneofs().get(oi as usize) {
                for &i in od.field_indices() {
                    if let Some(other) = md.fields().get(i as usize)
                        && other.number() != fd.number()
                    {
                        self.fields.remove(&other.number());
                    }
                }
            }
        }
        if is_implicit(fd) && v.is_default() {
            self.fields.remove(&fd.number());
            return;
        }
        self.fields.insert(fd.number(), v);
    }

    fn clear(&mut self, fd: &FieldDescriptor) {
        self.fields.remove(&fd.number());
    }

    /// Go's `Message.WhichOneof`.
    fn which_oneof(&self, oi: usize) -> Option<&'static FieldDescriptor> {
        let md = self.descriptor();
        let od = md.oneofs().get(oi)?;
        od.field_indices()
            .iter()
            .filter_map(|&i| md.fields().get(i as usize))
            .find(|fd| self.has(fd))
    }
}

/// Whether presence of the field is its holding a non-zero value: a proto3
/// scalar outside any oneof, or a repeated or map field.
fn is_implicit(fd: &FieldDescriptor) -> bool {
    match fd.kind() {
        FieldKind::List(_) | FieldKind::Map { .. } => true,
        FieldKind::Singular(SingularKind::Message(_)) => false,
        FieldKind::Singular(_) => {
            fd.oneof_index().is_none() && fd.presence() == buffa::editions::FieldPresence::Implicit
        }
    }
}

/// Go's `FieldDescriptors.ByName`, which matches the proto name only (buffa
/// also matches the JSON name).
fn field_by_name<'a>(md: &'a MessageDescriptor, name: &str) -> Option<&'a FieldDescriptor> {
    md.field_by_name(name).filter(|fd| fd.name() == name)
}

/// Go's `OneofDescriptors.ByName`, including synthetic oneofs.
fn oneof_by_name(md: &MessageDescriptor, name: &str) -> Option<usize> {
    md.oneofs().iter().position(|od| od.name() == name)
}

/// The element kind of a field and whether it is repeated; None for maps.
fn field_elem_kind(fd: &FieldDescriptor) -> Option<(SingularKind, bool)> {
    match fd.kind() {
        FieldKind::Singular(k) => Some((k, false)),
        FieldKind::List(k) => Some((k, true)),
        FieldKind::Map { .. } => None,
    }
}

// ---------------------------------------------------------------------------
// Environments
// ---------------------------------------------------------------------------

/// An interned symbol name.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash)]
pub struct SymId(u32);

#[derive(Default)]
struct Interner {
    ids: HashMap<Rc<str>, SymId>,
    names: Vec<Rc<str>>,
}

impl Interner {
    fn intern(&mut self, name: &str) -> SymId {
        if let Some(id) = self.ids.get(name) {
            return *id;
        }
        let id = SymId(self.names.len() as u32);
        let rc: Rc<str> = Rc::from(name);
        self.names.push(Rc::clone(&rc));
        self.ids.insert(rc, id);
        id
    }
}

/// A lexical environment frame. The root frame, the globals, is a table
/// indexed by symbol; a child frame is a short list searched from its most
/// recent binding, so a later binding of a name shadows an earlier one, as
/// a Go map's later Set does.
pub struct Env {
    parent: Option<Rc<Env>>,
    vars: Vars,
}

enum Vars {
    Global(RefCell<Vec<Option<Value>>>),
    Local(RefCell<Vec<(SymId, Value)>>),
}

impl Env {
    /// NewEnv creates a frame: a child of `parent`, or a root frame.
    pub fn new(parent: Option<Rc<Env>>) -> Rc<Env> {
        let vars = if parent.is_some() {
            Vars::Local(RefCell::new(Vec::new()))
        } else {
            Vars::Global(RefCell::new(Vec::new()))
        };
        Rc::new(Env { parent, vars })
    }

    /// Get looks up a variable in the lexical chain.
    pub fn get(&self, id: SymId) -> Option<Value> {
        let mut cur = Some(self);
        while let Some(e) = cur {
            match &e.vars {
                Vars::Global(v) => {
                    if let Some(Some(val)) = v.borrow().get(id.0 as usize) {
                        return Some(val.clone());
                    }
                }
                Vars::Local(v) => {
                    if let Some((_, val)) = v.borrow().iter().rev().find(|(k, _)| *k == id) {
                        return Some(val.clone());
                    }
                }
            }
            cur = e.parent.as_deref();
        }
        None
    }

    /// Set binds a variable in this frame.
    pub fn set(&self, id: SymId, v: Value) {
        match &self.vars {
            Vars::Global(vars) => {
                let mut vars = vars.borrow_mut();
                let i = id.0 as usize;
                if vars.len() <= i {
                    vars.resize(i + 1, None);
                }
                vars[i] = Some(v);
            }
            Vars::Local(vars) => vars.borrow_mut().push((id, v)),
        }
    }
}

// ---------------------------------------------------------------------------
// Lowered syntax
// ---------------------------------------------------------------------------

/// An expression lowered for evaluation: the `Expression`'s shape with
/// symbols interned and special forms resolved. Cloning shares it.
#[derive(Clone)]
pub struct Node(Rc<NodeData>);

struct NodeData {
    kind: Kind,
    pos: Option<Pos>,
}

/// A source position, kept when the syntax has a start line.
struct Pos {
    file: Rc<str>,
    line: i32,
    col: i32,
}

enum Kind {
    /// An Expression with neither an atom nor a list.
    None,
    Atom(Atom),
    List(ListNode),
}

enum Atom {
    None,
    Bool(bool),
    Int(i64),
    Str(Rc<str>),
    Money(MoneyVal),
    Float(f64),
    Date(DateVal),
    /// A keyword, with the symbol value `:name` it evaluates to.
    Keyword(Rc<str>, Value),
    Symbol(Sym),
}

struct Sym {
    package: Rc<str>,
    name: Rc<str>,
    /// fullSymbolName: `package/name` or `name`.
    full: Rc<str>,
    id: SymId,
}

struct ListNode {
    elems: Vec<Node>,
    form: Form,
    aux: Aux,
}

/// The special form a list's head names, or Call.
#[derive(Clone, Copy, PartialEq, Eq)]
enum Form {
    Empty,
    UseProto,
    Deftype,
    Defstruct,
    Assign,
    Def,
    Fn,
    Let,
    With,
    Withs,
    Do,
    If,
    Cond,
    Typecase,
    Isa,
    Quote,
    Slice,
    Table,
    Inst,
    MakeStruct,
    MakeProto,
    ProtoWith,
    Dot,
    Has,
    WhichOneof,
    Arith(ArithOp),
    Cmp(CmpOp),
    Is,
    Ne,
    And,
    Or,
    Call,
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum ArithOp {
    Add,
    Sub,
    Mul,
    Div,
    Min,
    Max,
}

impl ArithOp {
    fn name(self) -> &'static str {
        match self {
            ArithOp::Add => "+",
            ArithOp::Sub => "-",
            ArithOp::Mul => "*",
            ArithOp::Div => "/",
            ArithOp::Min => "min",
            ArithOp::Max => "max",
        }
    }
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum CmpOp {
    Lt,
    Le,
    Gt,
    Ge,
}

impl CmpOp {
    fn name(self) -> &'static str {
        match self {
            CmpOp::Lt => "<",
            CmpOp::Le => "<=",
            CmpOp::Gt => ">",
            CmpOp::Ge => ">=",
        }
    }
}

fn form_of(head: &str) -> Form {
    match head {
        "use-proto" => Form::UseProto,
        "deftype" => Form::Deftype,
        "defstruct" => Form::Defstruct,
        "=" => Form::Assign,
        "def" => Form::Def,
        "fn" => Form::Fn,
        "let" => Form::Let,
        "with" => Form::With,
        "withs" => Form::Withs,
        "do" => Form::Do,
        "if" => Form::If,
        "cond" => Form::Cond,
        "typecase" => Form::Typecase,
        "isa" => Form::Isa,
        "quote" => Form::Quote,
        "slice" => Form::Slice,
        "table" => Form::Table,
        "inst" => Form::Inst,
        "make-struct" => Form::MakeStruct,
        "make-proto" | "proto" => Form::MakeProto,
        "proto-with" => Form::ProtoWith,
        "." => Form::Dot,
        "has?" => Form::Has,
        "which-oneof" => Form::WhichOneof,
        "+" => Form::Arith(ArithOp::Add),
        "-" => Form::Arith(ArithOp::Sub),
        "*" => Form::Arith(ArithOp::Mul),
        "/" => Form::Arith(ArithOp::Div),
        "min" => Form::Arith(ArithOp::Min),
        "max" => Form::Arith(ArithOp::Max),
        "<" => Form::Cmp(CmpOp::Lt),
        "<=" => Form::Cmp(CmpOp::Le),
        ">" => Form::Cmp(CmpOp::Gt),
        ">=" => Form::Cmp(CmpOp::Ge),
        "is" => Form::Is,
        "!=" => Form::Ne,
        "and" => Form::And,
        "or" => Form::Or,
        _ => Form::Call,
    }
}

/// What a form needs that can be worked out once, before evaluation.
enum Aux {
    None,
    /// `fn`: the parameter names and the body wrapped in `do`.
    Fn(Result<Rc<[SymId]>, EvalError>, Option<Node>),
    /// `let` and `typecase`: the bound variable.
    Var(SymId),
    /// `with` and `withs`: the variable of each binding pair.
    Bindings(Vec<SymId>),
    /// `quote`: the symbol value.
    Quote(Result<Value, EvalError>),
    /// `.`, `has?` and `which-oneof`: the quoted path or name, and for `.`
    /// its dot-separated segments.
    Path(Result<(Rc<str>, Vec<Rc<str>>), EvalError>),
}

impl Node {
    fn new(kind: Kind, pos: Option<Pos>) -> Node {
        Node(Rc::new(NodeData { kind, pos }))
    }

    fn atom(&self) -> Option<&Atom> {
        match &self.0.kind {
            Kind::Atom(a) => Some(a),
            _ => None,
        }
    }

    fn list(&self) -> Option<&ListNode> {
        match &self.0.kind {
            Kind::List(l) => Some(l),
            _ => None,
        }
    }

    /// The list's elements; none for an atom (Go's GetList().GetExpressions()).
    fn elems(&self) -> &[Node] {
        match &self.0.kind {
            Kind::List(l) => &l.elems,
            _ => &[],
        }
    }

    fn sym(&self) -> Option<&Sym> {
        match self.atom() {
            Some(Atom::Symbol(s)) => Some(s),
            _ => None,
        }
    }

    /// symbolName: the full name of a symbol atom, or "".
    fn symbol_name(&self) -> &str {
        match self.sym() {
            Some(s) => &s.full,
            None => "",
        }
    }

    /// The keyword of a keyword atom, or "" (Go's GetAtom().GetKeyword()).
    fn keyword(&self) -> &str {
        match self.atom() {
            Some(Atom::Keyword(k, _)) => k,
            _ => "",
        }
    }
}

// ---------------------------------------------------------------------------
// The evaluator
// ---------------------------------------------------------------------------

/// Evaluator executes taxlisp modules and expressions.
pub struct Evaluator {
    /// The descriptors protobuf names resolve against.
    pub pool: &'static Arc<DescriptorPool>,
    /// `use-proto` aliases: alias to protobuf package.
    pub proto_aliases: RefCell<HashMap<String, String>>,
    /// The global environment.
    pub globals: Rc<Env>,
    symbols: RefCell<Interner>,
    nil_id: SymId,
}

/// Go's builtin table: each name and function, in registration order.
const BUILTINS: &[(&str, BuiltinFn)] = &[
    ("not", builtin_not),
    ("nil?", builtin_nil_p),
    ("empty-slice", builtin_empty_slice),
    ("len", builtin_len),
    ("empty?", builtin_empty_p),
    ("elt", builtin_elt),
    ("subslice", builtin_subslice),
    ("append", builtin_append),
    ("map", builtin_map),
    ("keep", builtin_keep),
    ("fold", builtin_fold),
    ("some", builtin_some),
    ("all", builtin_all),
    ("sum-money", builtin_sum_money),
    ("sum", builtin_sum),
    ("lookup", builtin_lookup),
    ("lookup-or", builtin_lookup_or),
    ("join-words", builtin_join_words),
    ("round-dollar", builtin_round_dollar),
    ("round-cents", builtin_round_cents),
    ("round-rate4", builtin_round_rate4),
    ("floor-float", builtin_floor_float),
    ("ceil-float", builtin_ceil_float),
    ("clamp-zero", builtin_clamp_zero),
    ("abs-money", builtin_abs_money),
    ("int-to-money", builtin_int_to_money),
    ("int-to-float", builtin_int_to_float),
    ("money-to-float", builtin_money_to_float),
    ("float-to-money", builtin_float_to_money),
    ("date-year", builtin_date_year),
    ("date-month", builtin_date_month),
    ("date-day", builtin_date_day),
];

/// The argument at `i`; Go indexes it unchecked and panics when it is
/// missing, which this reports as an error instead.
fn arg<'a>(args: &'a [Value], i: usize, expr: Option<&Node>) -> Result<&'a Value, EvalError> {
    args.get(i)
        .ok_or_else(|| err_at(expr, format!("missing argument {}", i + 1)))
}

fn builtin_not(_: &Evaluator, args: &[Value], expr: Option<&Node>) -> Result<Value, EvalError> {
    match arg(args, 0, expr)? {
        Value::Bool(b) => Ok(Value::Bool(!b)),
        v => Err(err_at(
            expr,
            format!("not expects Bool, got {}", v.go_type()),
        )),
    }
}

fn builtin_nil_p(_: &Evaluator, args: &[Value], expr: Option<&Node>) -> Result<Value, EvalError> {
    Ok(Value::Bool(matches!(arg(args, 0, expr)?, Value::Nil)))
}

fn builtin_empty_slice(_: &Evaluator, _: &[Value], _: Option<&Node>) -> Result<Value, EvalError> {
    Ok(Value::Slice(SliceVal::new(Vec::new())))
}

fn want_slice<'a>(
    v: &'a Value,
    expr: Option<&Node>,
    what: impl FnOnce(&str) -> String,
) -> Result<&'a SliceVal, EvalError> {
    match v {
        Value::Slice(s) => Ok(s),
        _ => Err(err_at(expr, what(v.go_type()))),
    }
}

fn builtin_len(_: &Evaluator, args: &[Value], expr: Option<&Node>) -> Result<Value, EvalError> {
    let s = want_slice(arg(args, 0, expr)?, expr, |t| {
        format!("len expects Slice, got {t}")
    })?;
    Ok(Value::Int(s.elems.len() as i64))
}

fn builtin_empty_p(_: &Evaluator, args: &[Value], expr: Option<&Node>) -> Result<Value, EvalError> {
    let s = want_slice(arg(args, 0, expr)?, expr, |t| {
        format!("empty? expects Slice, got {t}")
    })?;
    Ok(Value::Bool(s.elems.is_empty()))
}

fn builtin_elt(_: &Evaluator, args: &[Value], expr: Option<&Node>) -> Result<Value, EvalError> {
    let s = want_slice(arg(args, 0, expr)?, expr, |t| {
        format!("elt expects Slice, got {t}")
    })?;
    let idx = match arg(args, 1, expr)? {
        Value::Int(i) => *i,
        v => {
            return Err(err_at(
                expr,
                format!("elt expects Int index, got {}", v.go_type()),
            ));
        }
    };
    if idx < 0 || idx >= s.elems.len() as i64 {
        return Err(err_at(
            expr,
            format!("elt index {idx} out of bounds (len {})", s.elems.len()),
        ));
    }
    Ok(s.elems[idx as usize].clone())
}

fn builtin_subslice(
    _: &Evaluator,
    args: &[Value],
    expr: Option<&Node>,
) -> Result<Value, EvalError> {
    let s = want_slice(arg(args, 0, expr)?, expr, |t| {
        format!("subslice expects Slice, got {t}")
    })?;
    let (Value::Int(start), Value::Int(end)) = (arg(args, 1, expr)?, arg(args, 2, expr)?) else {
        return Err(err_at(expr, "subslice expects Int bounds".to_string()));
    };
    let n = s.elems.len() as i64;
    let mut lo = *start;
    let mut hi = *end;
    if lo < 0 {
        lo = 0;
    }
    if hi > n {
        hi = n;
    }
    if lo > hi {
        lo = hi;
    }
    if hi < 0 {
        // Go slices s.Elems[lo:hi] with a negative hi only when start and
        // end are both negative, which panics; report it instead.
        return Err(err_at(
            expr,
            format!("subslice bounds out of range [:{hi}]"),
        ));
    }
    Ok(Value::Slice(SliceVal::new(
        s.elems[lo as usize..hi as usize].to_vec(),
    )))
}

fn builtin_append(_: &Evaluator, args: &[Value], expr: Option<&Node>) -> Result<Value, EvalError> {
    let (Value::Slice(s1), Value::Slice(s2)) = (arg(args, 0, expr)?, arg(args, 1, expr)?) else {
        return Err(err_at(expr, "append expects two Slices".to_string()));
    };
    let mut out = Vec::with_capacity(s1.elems.len() + s2.elems.len());
    out.extend(s1.elems.iter().cloned());
    out.extend(s2.elems.iter().cloned());
    Ok(Value::Slice(SliceVal::new(out)))
}

fn builtin_map(ev: &Evaluator, args: &[Value], expr: Option<&Node>) -> Result<Value, EvalError> {
    let f = arg(args, 0, expr)?;
    let s = want_slice(arg(args, 1, expr)?, expr, |t| {
        format!("map expects Slice as second argument, got {t}")
    })?;
    let mut out = Vec::with_capacity(s.elems.len());
    for elem in s.elems.iter() {
        out.push(ev.apply(f, vec![elem.clone()], expr)?);
    }
    Ok(Value::Slice(SliceVal::new(out)))
}

fn builtin_keep(ev: &Evaluator, args: &[Value], expr: Option<&Node>) -> Result<Value, EvalError> {
    let f = arg(args, 0, expr)?;
    let s = want_slice(arg(args, 1, expr)?, expr, |t| {
        format!("keep expects Slice as second argument, got {t}")
    })?;
    let mut out = Vec::new();
    for elem in s.elems.iter() {
        match ev.apply(f, vec![elem.clone()], expr)? {
            Value::Bool(true) => out.push(elem.clone()),
            Value::Bool(false) => {}
            v => {
                return Err(err_at(
                    expr,
                    format!("keep predicate must return Bool, got {}", v.go_type()),
                ));
            }
        }
    }
    Ok(Value::Slice(SliceVal::new(out)))
}

fn builtin_fold(ev: &Evaluator, args: &[Value], expr: Option<&Node>) -> Result<Value, EvalError> {
    let f = arg(args, 0, expr)?;
    let mut acc = arg(args, 1, expr)?.clone();
    let s = want_slice(arg(args, 2, expr)?, expr, |t| {
        format!("fold expects Slice as third argument, got {t}")
    })?;
    for elem in s.elems.iter() {
        acc = ev.apply(f, vec![acc, elem.clone()], expr)?;
    }
    Ok(acc)
}

fn builtin_some(ev: &Evaluator, args: &[Value], expr: Option<&Node>) -> Result<Value, EvalError> {
    let f = arg(args, 0, expr)?;
    let s = want_slice(arg(args, 1, expr)?, expr, |t| {
        format!("some expects Slice as second argument, got {t}")
    })?;
    for elem in s.elems.iter() {
        if let Value::Bool(true) = ev.apply(f, vec![elem.clone()], expr)? {
            return Ok(Value::Bool(true));
        }
    }
    Ok(Value::Bool(false))
}

fn builtin_all(ev: &Evaluator, args: &[Value], expr: Option<&Node>) -> Result<Value, EvalError> {
    let f = arg(args, 0, expr)?;
    let s = want_slice(arg(args, 1, expr)?, expr, |t| {
        format!("all expects Slice as second argument, got {t}")
    })?;
    for elem in s.elems.iter() {
        if !matches!(ev.apply(f, vec![elem.clone()], expr)?, Value::Bool(true)) {
            return Ok(Value::Bool(false));
        }
    }
    Ok(Value::Bool(true))
}

fn builtin_sum_money(
    _: &Evaluator,
    args: &[Value],
    expr: Option<&Node>,
) -> Result<Value, EvalError> {
    let s = want_slice(arg(args, 0, expr)?, expr, |t| {
        format!("sum-money expects Slice, got {t}")
    })?;
    let mut total = BigRational::zero();
    for elem in s.elems.iter() {
        match elem {
            Value::Money(m) => total += &*m.r,
            v => {
                return Err(err_at(
                    expr,
                    format!("sum-money element must be Money, got {}", v.go_type()),
                ));
            }
        }
    }
    Ok(Value::Money(MoneyVal::from_rat(total)))
}

fn builtin_sum(ev: &Evaluator, args: &[Value], expr: Option<&Node>) -> Result<Value, EvalError> {
    let f = arg(args, 0, expr)?;
    let s = want_slice(arg(args, 1, expr)?, expr, |t| {
        format!("sum expects Slice as second argument, got {t}")
    })?;
    let mut total = BigRational::zero();
    for elem in s.elems.iter() {
        match ev.apply(f, vec![elem.clone()], expr)? {
            Value::Money(m) => total += &*m.r,
            v => {
                return Err(err_at(
                    expr,
                    format!("sum function must return Money, got {}", v.go_type()),
                ));
            }
        }
    }
    Ok(Value::Money(MoneyVal::from_rat(total)))
}

fn builtin_lookup(ev: &Evaluator, args: &[Value], expr: Option<&Node>) -> Result<Value, EvalError> {
    let Value::Table(tbl) = arg(args, 0, expr)? else {
        return Err(err_at(
            expr,
            format!(
                "lookup expects Table as first argument, got {}",
                args[0].go_type()
            ),
        ));
    };
    let key = arg(args, 1, expr)?;
    for entry in tbl.entries.iter() {
        if ev.values_equal(&entry.key, key) {
            return Ok(entry.value.clone());
        }
    }
    Ok(Value::Nil)
}

fn builtin_lookup_or(
    ev: &Evaluator,
    args: &[Value],
    expr: Option<&Node>,
) -> Result<Value, EvalError> {
    let Value::Table(tbl) = arg(args, 0, expr)? else {
        return Err(err_at(
            expr,
            format!(
                "lookup-or expects Table as first argument, got {}",
                args[0].go_type()
            ),
        ));
    };
    let key = arg(args, 1, expr)?;
    let fallback = arg(args, 2, expr)?;
    for entry in tbl.entries.iter() {
        if ev.values_equal(&entry.key, key) {
            return Ok(entry.value.clone());
        }
    }
    Ok(fallback.clone())
}

fn builtin_join_words(
    _: &Evaluator,
    args: &[Value],
    expr: Option<&Node>,
) -> Result<Value, EvalError> {
    let s = want_slice(arg(args, 0, expr)?, expr, |t| {
        format!("join-words expects Slice, got {t}")
    })?;
    let mut words: Vec<&str> = Vec::new();
    for e in s.elems.iter() {
        match e {
            Value::String(w) => {
                if !w.is_empty() {
                    words.push(w);
                }
            }
            v => {
                return Err(err_at(
                    expr,
                    format!("join-words expects String elements, got {}", v.go_type()),
                ));
            }
        }
    }
    Ok(Value::string(&words.join(" ")))
}

fn want_money<'a>(
    name: &str,
    args: &'a [Value],
    expr: Option<&Node>,
) -> Result<&'a MoneyVal, EvalError> {
    match arg(args, 0, expr)? {
        Value::Money(m) => Ok(m),
        v => Err(err_at(
            expr,
            format!("{name} expects Money, got {}", v.go_type()),
        )),
    }
}

fn want_float(name: &str, args: &[Value], expr: Option<&Node>) -> Result<f64, EvalError> {
    match arg(args, 0, expr)? {
        Value::Float(f) => Ok(*f),
        v => Err(err_at(
            expr,
            format!("{name} expects Float, got {}", v.go_type()),
        )),
    }
}

fn want_int(name: &str, args: &[Value], expr: Option<&Node>) -> Result<i64, EvalError> {
    match arg(args, 0, expr)? {
        Value::Int(i) => Ok(*i),
        v => Err(err_at(
            expr,
            format!("{name} expects Int, got {}", v.go_type()),
        )),
    }
}

fn want_date(name: &str, args: &[Value], expr: Option<&Node>) -> Result<DateVal, EvalError> {
    match arg(args, 0, expr)? {
        Value::Date(d) => Ok(*d),
        v => Err(err_at(
            expr,
            format!("{name} expects Date, got {}", v.go_type()),
        )),
    }
}

fn builtin_round_dollar(
    _: &Evaluator,
    args: &[Value],
    expr: Option<&Node>,
) -> Result<Value, EvalError> {
    let m = want_money("round-dollar", args, expr)?;
    Ok(Value::Money(MoneyVal::from_rat(round_rat_scale(&m.r, 1))))
}

fn builtin_round_cents(
    _: &Evaluator,
    args: &[Value],
    expr: Option<&Node>,
) -> Result<Value, EvalError> {
    let m = want_money("round-cents", args, expr)?;
    Ok(Value::Money(MoneyVal::from_rat(round_rat_scale(&m.r, 100))))
}

fn builtin_round_rate4(
    _: &Evaluator,
    args: &[Value],
    expr: Option<&Node>,
) -> Result<Value, EvalError> {
    let f = want_float("round-rate4", args, expr)?;
    Ok(Value::Float((f * 10000.0).round() / 10000.0))
}

fn builtin_floor_float(
    _: &Evaluator,
    args: &[Value],
    expr: Option<&Node>,
) -> Result<Value, EvalError> {
    Ok(Value::Float(want_float("floor-float", args, expr)?.floor()))
}

fn builtin_ceil_float(
    _: &Evaluator,
    args: &[Value],
    expr: Option<&Node>,
) -> Result<Value, EvalError> {
    Ok(Value::Float(want_float("ceil-float", args, expr)?.ceil()))
}

fn builtin_clamp_zero(
    _: &Evaluator,
    args: &[Value],
    expr: Option<&Node>,
) -> Result<Value, EvalError> {
    let m = want_money("clamp-zero", args, expr)?;
    if m.r.is_negative() {
        return Ok(Value::Money(MoneyVal::from_rat(BigRational::zero())));
    }
    Ok(Value::Money(m.clone()))
}

fn builtin_abs_money(
    _: &Evaluator,
    args: &[Value],
    expr: Option<&Node>,
) -> Result<Value, EvalError> {
    let m = want_money("abs-money", args, expr)?;
    Ok(Value::Money(MoneyVal::from_rat(m.r.abs())))
}

fn builtin_int_to_money(
    _: &Evaluator,
    args: &[Value],
    expr: Option<&Node>,
) -> Result<Value, EvalError> {
    let i = want_int("int-to-money", args, expr)?;
    Ok(Value::Money(MoneyVal::from_rat(BigRational::from_integer(
        BigInt::from(i),
    ))))
}

fn builtin_int_to_float(
    _: &Evaluator,
    args: &[Value],
    expr: Option<&Node>,
) -> Result<Value, EvalError> {
    Ok(Value::Float(want_int("int-to-float", args, expr)? as f64))
}

fn builtin_money_to_float(
    _: &Evaluator,
    args: &[Value],
    expr: Option<&Node>,
) -> Result<Value, EvalError> {
    let m = want_money("money-to-float", args, expr)?;
    Ok(Value::Float(rat_to_f64(&m.r)))
}

fn builtin_float_to_money(
    _: &Evaluator,
    args: &[Value],
    expr: Option<&Node>,
) -> Result<Value, EvalError> {
    let f = want_float("float-to-money", args, expr)?;
    Ok(Value::Money(MoneyVal::from_rat(float_to_rat(f))))
}

fn builtin_date_year(
    _: &Evaluator,
    args: &[Value],
    expr: Option<&Node>,
) -> Result<Value, EvalError> {
    Ok(Value::Int(i64::from(
        want_date("date-year", args, expr)?.year,
    )))
}

fn builtin_date_month(
    _: &Evaluator,
    args: &[Value],
    expr: Option<&Node>,
) -> Result<Value, EvalError> {
    Ok(Value::Int(i64::from(
        want_date("date-month", args, expr)?.month,
    )))
}

fn builtin_date_day(
    _: &Evaluator,
    args: &[Value],
    expr: Option<&Node>,
) -> Result<Value, EvalError> {
    Ok(Value::Int(i64::from(
        want_date("date-day", args, expr)?.day,
    )))
}

/// The result of resolving a protobuf name.
enum Resolved {
    Message(MessageIndex),
    Enum(&'static EnumDescriptor),
    /// A descriptor that is neither a message nor an enum (an enum value or
    /// a field, say), by full name.
    Other(String),
}

impl Evaluator {
    /// NewEvaluator creates a new Evaluator initialized with all built-in
    /// functions, resolving protobuf names against `pool` (the default
    /// pool when None, as Go uses the global registries for nil).
    pub fn new(pool: Option<&'static Arc<DescriptorPool>>) -> Result<Evaluator, EvalError> {
        let pool = match pool {
            Some(p) => p,
            None => default_pool()?,
        };
        let mut symbols = Interner::default();
        let nil_id = symbols.intern("nil");
        let ev = Evaluator {
            pool,
            proto_aliases: RefCell::new(HashMap::new()),
            globals: Env::new(None),
            symbols: RefCell::new(symbols),
            nil_id,
        };
        ev.register_builtins();
        Ok(ev)
    }

    fn register_builtins(&self) {
        self.globals.set(self.nil_id, Value::Nil);
        for (name, func) in BUILTINS {
            let id = self.intern(name);
            self.globals
                .set(id, Value::Builtin(BuiltinVal { name, func: *func }));
        }
    }

    /// The interned id of a symbol name.
    pub fn intern(&self, name: &str) -> SymId {
        self.symbols.borrow_mut().intern(name)
    }

    /// EvalModule evaluates all top-level forms in a ModuleFile, registering
    /// `use-proto`, `defstruct`, `=`, and `def` declarations in the globals.
    pub fn eval_module(&self, mf: &taxlispv1::ModuleFile) -> Result<Value, EvalError> {
        let nodes = self.lower_all(&mf.expressions);
        let mut last = Value::Nil;
        for n in &nodes {
            last = self.eval_node(n, &self.globals)?;
        }
        Ok(last)
    }

    /// Call invokes a named top-level function with the provided arguments.
    pub fn call(&self, name: &str, args: Vec<Value>) -> Result<Value, EvalError> {
        let id = self.intern(name);
        let Some(f) = self.globals.get(id) else {
            return Err(EvalError::new(format!(
                "undefined function {}",
                quote(name)
            )));
        };
        self.apply(&f, args, None)
    }

    /// Apply calls a runtime function value (closure, builtin, or struct
    /// constructor).
    pub fn apply(
        &self,
        f: &Value,
        args: Vec<Value>,
        call_site: Option<&Node>,
    ) -> Result<Value, EvalError> {
        match f {
            Value::Builtin(b) => (b.func)(self, &args, call_site),
            Value::StructCtor(c) => {
                if args.len() != c.field_names.len() {
                    return Err(err_at(
                        call_site,
                        format!(
                            "struct {} expects {} arguments, got {}",
                            c.name,
                            c.field_names.len(),
                            args.len()
                        ),
                    ));
                }
                let fields: BTreeMap<Rc<str>, Value> =
                    c.field_names.iter().cloned().zip(args).collect();
                Ok(Value::Struct(StructVal {
                    type_name: Rc::clone(&c.name),
                    fields: Rc::new(fields),
                }))
            }
            Value::Closure(c) => {
                let c = &c.0;
                if args.len() != c.param_names.len() {
                    return Err(err_at(
                        call_site,
                        format!(
                            "function {} expects {} arguments, got {}",
                            c.name,
                            c.param_names.len(),
                            args.len()
                        ),
                    ));
                }
                let frame = Env::new(Some(Rc::clone(&c.env)));
                for (p, a) in c.param_names.iter().zip(args) {
                    frame.set(*p, a);
                }
                self.eval_node(&c.body, &frame)
            }
            other => Err(err_at(
                call_site,
                format!(
                    "cannot call non-function value {other} ({})",
                    other.go_type()
                ),
            )),
        }
    }

    /// EvalExpr evaluates a single taxlisp Expression in the given lexical
    /// environment.
    pub fn eval_expr(
        &self,
        expr: &taxlispv1::Expression,
        env: &Rc<Env>,
    ) -> Result<Value, EvalError> {
        let node = self.lower(expr, &mut FileCache::default());
        self.eval_node(&node, env)
    }

    // -- lowering ----------------------------------------------------------

    fn lower_all(&self, exprs: &[taxlispv1::Expression]) -> Vec<Node> {
        let mut files = FileCache::default();
        exprs.iter().map(|e| self.lower(e, &mut files)).collect()
    }

    fn lower(&self, e: &taxlispv1::Expression, files: &mut FileCache) -> Node {
        match &e.value {
            None => Node::new(Kind::None, None),
            Some(ExprValue::Atom(a)) => {
                let pos = files.pos(a.syntax.as_option());
                Node::new(Kind::Atom(self.lower_atom(a)), pos)
            }
            Some(ExprValue::List(l)) => {
                let pos = files.pos(l.syntax.as_option());
                let elems: Vec<Node> = l.expressions.iter().map(|x| self.lower(x, files)).collect();
                Node::new(Kind::List(self.lower_list(elems)), pos)
            }
        }
    }

    fn lower_atom(&self, a: &taxlispv1::Atom) -> Atom {
        match &a.primitive {
            None => Atom::None,
            Some(Primitive::BoolValue(b)) => Atom::Bool(*b),
            Some(Primitive::IntValue(i)) => Atom::Int(*i),
            Some(Primitive::StringValue(s)) => Atom::Str(Rc::from(s.as_str())),
            Some(Primitive::Money(m)) => Atom::Money(MoneyVal::new_cents(m.minor)),
            Some(Primitive::FloatValue(f)) => Atom::Float(*f),
            Some(Primitive::Date(d)) => Atom::Date(DateVal {
                year: d.year,
                month: d.month,
                day: d.day,
            }),
            Some(Primitive::Keyword(k)) => Atom::Keyword(
                Rc::from(k.as_str()),
                Value::Symbol(SymbolVal::new("", &format!(":{k}"))),
            ),
            Some(Primitive::Symbol(s)) => {
                let full = full_symbol_name(&s.package, &s.name);
                let id = self.intern(&full);
                Atom::Symbol(Sym {
                    package: Rc::from(s.package.as_str()),
                    name: Rc::from(s.name.as_str()),
                    full: Rc::from(full.as_str()),
                    id,
                })
            }
        }
    }

    fn lower_list(&self, elems: Vec<Node>) -> ListNode {
        let form = match elems.first() {
            None => Form::Empty,
            Some(h) => form_of(h.symbol_name()),
        };
        let aux = match form {
            Form::Fn => {
                let params = match elems.get(1) {
                    Some(p) => self.parse_param_names(p),
                    None => Err(EvalError::new("parameter list must be a list".to_string())),
                };
                let body = elems.get(3..).map(|b| self.wrap_do(b));
                Aux::Fn(params, body)
            }
            Form::Let => Aux::Var(self.intern(elems.get(1).map_or("", |n| n.symbol_name()))),
            Form::Typecase => {
                let name = elems
                    .get(1)
                    .and_then(|n| n.elems().first())
                    .map_or("", |n| n.symbol_name());
                Aux::Var(self.intern(name))
            }
            Form::With | Form::Withs => {
                let b_list = elems.get(1).map_or(&[][..], |n| n.elems());
                let mut ids = Vec::new();
                for b in b_list.iter().step_by(2) {
                    let name = match b.list() {
                        Some(l) if l.elems.len() == 2 => l.elems[0].symbol_name(),
                        _ => b.symbol_name(),
                    };
                    ids.push(self.intern(name));
                }
                Aux::Bindings(ids)
            }
            Form::Quote => Aux::Quote(match elems.get(1) {
                Some(n) => match n.sym() {
                    Some(s) => Ok(Value::Symbol(SymbolVal {
                        package: Rc::clone(&s.package),
                        name: Rc::clone(&s.name),
                    })),
                    None => Err(err_at(Some(n), "quote expects a symbol".to_string())),
                },
                None => Err(EvalError::new("quote expects a symbol".to_string())),
            }),
            Form::Dot | Form::Has | Form::WhichOneof => Aux::Path(match elems.get(2) {
                Some(n) => match extract_quoted_or_string(n) {
                    Ok(s) => {
                        let segs = if form == Form::Dot {
                            s.split('.').map(Rc::from).collect()
                        } else {
                            Vec::new()
                        };
                        Ok((Rc::from(s.as_str()), segs))
                    }
                    Err(e) => Err(err_at(Some(n), e)),
                },
                None => Err(EvalError::new(
                    "expected quoted symbol or string".to_string(),
                )),
            }),
            _ => Aux::None,
        };
        ListNode { elems, form, aux }
    }

    /// wrapDo: one expression as is, or several in a `do`.
    fn wrap_do(&self, exprs: &[Node]) -> Node {
        if exprs.len() == 1 {
            return exprs[0].clone();
        }
        let do_sym = Node::new(
            Kind::Atom(Atom::Symbol(Sym {
                package: Rc::from(""),
                name: Rc::from("do"),
                full: Rc::from("do"),
                id: self.intern("do"),
            })),
            None,
        );
        let mut all = Vec::with_capacity(exprs.len() + 1);
        all.push(do_sym);
        all.extend(exprs.iter().cloned());
        Node::new(
            Kind::List(ListNode {
                elems: all,
                form: Form::Do,
                aux: Aux::None,
            }),
            None,
        )
    }

    // -- evaluation --------------------------------------------------------

    fn eval_node(&self, expr: &Node, env: &Rc<Env>) -> Result<Value, EvalError> {
        match &expr.0.kind {
            Kind::None => Ok(Value::Nil),
            Kind::Atom(a) => self.eval_atom(expr, a, env),
            Kind::List(l) => self.eval_list(expr, l, env),
        }
    }

    fn eval_atom(&self, expr: &Node, a: &Atom, env: &Rc<Env>) -> Result<Value, EvalError> {
        Ok(match a {
            Atom::None => Value::Nil,
            Atom::Bool(b) => Value::Bool(*b),
            Atom::Int(i) => Value::Int(*i),
            Atom::Str(s) => Value::String(Rc::clone(s)),
            Atom::Money(m) => Value::Money(m.clone()),
            Atom::Float(f) => Value::Float(*f),
            Atom::Date(d) => Value::Date(*d),
            Atom::Keyword(_, v) => v.clone(),
            Atom::Symbol(s) => {
                if s.id == self.nil_id {
                    return Ok(Value::Nil);
                }
                match env.get(s.id) {
                    Some(v) => v,
                    None => return Err(err_at(Some(expr), format!("unbound symbol {}", s.full))),
                }
            }
        })
    }

    // Each form is its own function, so that the frame this recursion
    // passes through stays small.
    fn eval_list(&self, expr: &Node, list: &ListNode, env: &Rc<Env>) -> Result<Value, EvalError> {
        let elems = &list.elems[..];
        match list.form {
            Form::Empty | Form::Deftype => Ok(Value::Nil),
            Form::UseProto => self.eval_use_proto(expr, elems),
            Form::Defstruct => self.eval_defstruct(expr, elems),
            Form::Assign => self.eval_assign(expr, elems, env),
            Form::Def => self.eval_def(expr, elems),
            Form::Fn => self.eval_fn(expr, list, env),
            Form::Let => self.eval_let(expr, list, env),
            Form::With | Form::Withs => self.eval_with(expr, list, env),
            Form::Do => self.eval_seq(&elems[1..], env),
            Form::If => self.eval_if(expr, elems, env),
            Form::Cond => self.eval_cond(expr, elems, env),
            Form::Typecase => self.eval_typecase(expr, list, env),
            Form::Isa => self.eval_isa(expr, elems, env),
            Form::Quote => match &list.aux {
                Aux::Quote(v) => v.clone(),
                _ => Err(err_at(Some(expr), "quote expects a symbol".to_string())),
            },
            Form::Slice => self.eval_slice(elems, env),
            Form::Table => self.eval_table(expr, elems, env),
            // Type arguments are erased at runtime; evaluate the underlying
            // generic function.
            Form::Inst => self.eval_node(at(elems, 1, expr)?, env),
            Form::MakeStruct => self.eval_make_struct(expr, elems, env),
            Form::MakeProto => self.eval_make_proto(expr, &elems[1..], env),
            Form::ProtoWith => self.eval_proto_with(expr, &elems[1..], env),
            Form::Dot => self.eval_field_access(expr, list, env),
            Form::Has => self.eval_has_field(expr, list, env),
            Form::WhichOneof => self.eval_which_oneof(expr, list, env),
            Form::Arith(op) => self.eval_arithmetic(expr, op, &elems[1..], env),
            Form::Cmp(op) => self.eval_comparison(expr, op, elems, env),
            Form::Is | Form::Ne => self.eval_is(expr, list.form == Form::Is, elems, env),
            Form::And | Form::Or => self.eval_and_or(list.form == Form::And, elems, env),
            Form::Call => self.eval_call(expr, elems, env),
        }
    }

    fn eval_assign(&self, expr: &Node, elems: &[Node], env: &Rc<Env>) -> Result<Value, EvalError> {
        let name = self.intern(at(elems, 1, expr)?.symbol_name());
        let val = self.eval_node(at(elems, 3, expr)?, env)?;
        self.globals.set(name, val.clone());
        Ok(val)
    }

    fn eval_fn(&self, expr: &Node, list: &ListNode, env: &Rc<Env>) -> Result<Value, EvalError> {
        let Aux::Fn(params, body) = &list.aux else {
            return Err(err_at(Some(expr), "malformed fn".to_string()));
        };
        let params = params.clone()?;
        let Some(body) = body else {
            return Err(err_at(Some(expr), "malformed fn".to_string()));
        };
        Ok(Value::Closure(ClosureVal(Rc::new(Closure {
            name: Rc::from(""),
            param_names: params,
            body: body.clone(),
            env: Rc::clone(env),
        }))))
    }

    fn eval_isa(&self, expr: &Node, elems: &[Node], env: &Rc<Env>) -> Result<Value, EvalError> {
        let val = self.eval_node(at(elems, 1, expr)?, env)?;
        Ok(Value::Bool(
            self.value_matches_type_expr(&val, at(elems, 2, expr)?),
        ))
    }

    fn eval_slice(&self, elems: &[Node], env: &Rc<Env>) -> Result<Value, EvalError> {
        let mut out = Vec::with_capacity(elems.len() - 1);
        for e in &elems[1..] {
            out.push(self.eval_node(e, env)?);
        }
        Ok(Value::Slice(SliceVal::new(out)))
    }

    fn eval_table(&self, expr: &Node, elems: &[Node], env: &Rc<Env>) -> Result<Value, EvalError> {
        let mut entries = Vec::new();
        let mut i = 1;
        while i < elems.len() {
            let k = self.eval_node(&elems[i], env)?;
            let v = self.eval_node(at(elems, i + 1, expr)?, env)?;
            entries.push(TableEntry { key: k, value: v });
            i += 2;
        }
        Ok(Value::Table(TableVal {
            entries: Rc::from(entries),
        }))
    }

    fn eval_is(
        &self,
        expr: &Node,
        is: bool,
        elems: &[Node],
        env: &Rc<Env>,
    ) -> Result<Value, EvalError> {
        let lv = self.eval_node(at(elems, 1, expr)?, env)?;
        let rv = self.eval_node(at(elems, 2, expr)?, env)?;
        let eq = self.values_equal(&lv, &rv);
        Ok(Value::Bool(if is { eq } else { !eq }))
    }

    fn eval_and_or(&self, is_and: bool, elems: &[Node], env: &Rc<Env>) -> Result<Value, EvalError> {
        for a in &elems[1..] {
            match self.eval_node(a, env)? {
                Value::Bool(b) => {
                    if is_and && !b {
                        return Ok(Value::Bool(false));
                    }
                    if !is_and && b {
                        return Ok(Value::Bool(true));
                    }
                }
                v => {
                    let name = if is_and { "and" } else { "or" };
                    return Err(err_at(Some(a), format!("{name} expects Bool, got {v}")));
                }
            }
        }
        Ok(Value::Bool(is_and))
    }

    fn eval_call(&self, expr: &Node, elems: &[Node], env: &Rc<Env>) -> Result<Value, EvalError> {
        let f = self.eval_node(&elems[0], env)?;
        let mut args = Vec::with_capacity(elems.len() - 1);
        for a in &elems[1..] {
            args.push(self.eval_node(a, env)?);
        }
        self.apply(&f, args, Some(expr))
    }

    fn eval_use_proto(&self, expr: &Node, elems: &[Node]) -> Result<Value, EvalError> {
        let e1 = at(elems, 1, expr)?;
        let pkg = extract_quoted_or_string(e1).map_err(|e| err_at(Some(e1), e))?;
        let e2 = at(elems, 2, expr)?;
        let alias = extract_quoted_or_string(e2).map_err(|e| err_at(Some(e2), e))?;
        self.proto_aliases.borrow_mut().insert(alias, pkg);
        Ok(Value::Nil)
    }

    fn eval_defstruct(&self, expr: &Node, elems: &[Node]) -> Result<Value, EvalError> {
        let name = parse_def_name(at(elems, 1, expr)?)?;
        let mut field_start = 2;
        if let Some(l2) = at(elems, 2, expr)?.list()
            && is_bare_symbol_list(&l2.elems)
        {
            field_start = 3;
        }
        let mut field_names: Vec<Rc<str>> = Vec::new();
        for f in elems.get(field_start..).unwrap_or(&[]) {
            if let Some(fl) = f.list()
                && fl.elems.len() == 2
            {
                field_names.push(Rc::from(fl.elems[0].symbol_name()));
            }
        }
        let ctor = Value::StructCtor(StructCtorVal {
            name: Rc::from(name.as_str()),
            field_names: Rc::from(field_names),
        });
        self.globals.set(self.intern(&name), ctor.clone());
        Ok(ctor)
    }

    fn eval_def(&self, expr: &Node, elems: &[Node]) -> Result<Value, EvalError> {
        let name = parse_def_name(at(elems, 1, expr)?)?;
        let param_names = self.parse_param_names(at(elems, 2, expr)?)?;
        let mut body_start = 4;
        while body_start + 1 < elems.len() && !elems[body_start].keyword().is_empty() {
            body_start += 2;
        }
        let Some(body) = elems.get(body_start..) else {
            return Err(err_at(Some(expr), "malformed def".to_string()));
        };
        let body = self.wrap_do(body);
        let cl = Value::Closure(ClosureVal(Rc::new(Closure {
            name: Rc::from(name.as_str()),
            param_names,
            body,
            env: Rc::clone(&self.globals),
        })));
        self.globals.set(self.intern(&name), cl.clone());
        Ok(cl)
    }

    fn eval_let(&self, expr: &Node, list: &ListNode, env: &Rc<Env>) -> Result<Value, EvalError> {
        let elems = &list.elems[..];
        let Aux::Var(var) = list.aux else {
            return Err(err_at(Some(expr), "malformed let".to_string()));
        };
        let (val_idx, body_start) = if elems.len() >= 5 { (3, 4) } else { (2, 3) };
        let val = self.eval_node(at(elems, val_idx, expr)?, env)?;
        let child = Env::new(Some(Rc::clone(env)));
        child.set(var, val);
        self.eval_seq(elems.get(body_start..).unwrap_or(&[]), &child)
    }

    fn eval_with(&self, expr: &Node, list: &ListNode, env: &Rc<Env>) -> Result<Value, EvalError> {
        let elems = &list.elems[..];
        let Aux::Bindings(ids) = &list.aux else {
            return Err(err_at(Some(expr), "malformed with".to_string()));
        };
        let b_list = at(elems, 1, expr)?.elems();
        let child = Env::new(Some(Rc::clone(env)));
        let eval_frame = if list.form == Form::Withs {
            &child
        } else {
            env
        };
        for (k, id) in ids.iter().enumerate() {
            let Some(val_expr) = b_list.get(2 * k + 1) else {
                return Err(err_at(Some(expr), "malformed binding list".to_string()));
            };
            let val = self.eval_node(val_expr, eval_frame)?;
            child.set(*id, val);
        }
        self.eval_seq(&elems[2..], &child)
    }

    fn eval_if(&self, expr: &Node, elems: &[Node], env: &Rc<Env>) -> Result<Value, EvalError> {
        let rest = &elems[1..];
        let mut i = 0;
        while i + 1 < rest.len() {
            match self.eval_node(&rest[i], env)? {
                Value::Bool(true) => return self.eval_node(&rest[i + 1], env),
                Value::Bool(false) => {}
                v => {
                    return Err(err_at(
                        Some(&rest[i]),
                        format!("if condition must evaluate to Bool, got {v}"),
                    ));
                }
            }
            i += 2;
        }
        match rest.last() {
            Some(last) => self.eval_node(last, env),
            None => Err(err_at(Some(expr), "malformed if".to_string())),
        }
    }

    fn eval_cond(&self, expr: &Node, elems: &[Node], env: &Rc<Env>) -> Result<Value, EvalError> {
        for clause in &elems[1..] {
            let pair = clause.elems();
            let cond_expr = at(pair, 0, expr)?;
            let then_expr = at(pair, 1, expr)?;
            if cond_expr.symbol_name() == "else" || cond_expr.keyword() == "else" {
                return self.eval_node(then_expr, env);
            }
            match self.eval_node(cond_expr, env)? {
                Value::Bool(true) => return self.eval_node(then_expr, env),
                Value::Bool(false) => {}
                v => {
                    return Err(err_at(
                        Some(cond_expr),
                        format!("cond condition must evaluate to Bool, got {v}"),
                    ));
                }
            }
        }
        Ok(Value::Nil)
    }

    fn eval_typecase(
        &self,
        expr: &Node,
        list: &ListNode,
        env: &Rc<Env>,
    ) -> Result<Value, EvalError> {
        let elems = &list.elems[..];
        let Aux::Var(var) = list.aux else {
            return Err(err_at(Some(expr), "malformed typecase".to_string()));
        };
        let bh = at(elems, 1, expr)?.elems();
        let scrut = self.eval_node(at(bh, 1, expr)?, env)?;
        let branches = &elems[2..];
        let mut i = 0;
        while i < branches.len() {
            let type_expr = &branches[i];
            let body_expr = at(branches, i + 1, expr)?;
            if self.value_matches_type_expr(&scrut, type_expr) {
                let child = Env::new(Some(Rc::clone(env)));
                child.set(var, scrut);
                return self.eval_node(body_expr, &child);
            }
            i += 2;
        }
        Err(err_at(
            Some(expr),
            format!(
                "non-exhaustive typecase for runtime value {scrut} ({})",
                scrut.go_type()
            ),
        ))
    }

    fn eval_make_struct(
        &self,
        expr: &Node,
        elems: &[Node],
        env: &Rc<Env>,
    ) -> Result<Value, EvalError> {
        // (make-struct StructType :field1 val1 :field2 val2 ...)
        let type_name = extract_struct_name(at(elems, 1, expr)?);
        let mut fields: BTreeMap<Rc<str>, Value> = BTreeMap::new();
        let mut i = 2;
        while i < elems.len() {
            let kw = elems[i].keyword();
            let val = self.eval_node(at(elems, i + 1, expr)?, env)?;
            fields.insert(Rc::from(kw), val);
            i += 2;
        }
        Ok(Value::Struct(StructVal {
            type_name: Rc::from(type_name),
            fields: Rc::new(fields),
        }))
    }

    fn eval_seq(&self, exprs: &[Node], env: &Rc<Env>) -> Result<Value, EvalError> {
        let mut last = Value::Nil;
        for e in exprs {
            last = self.eval_node(e, env)?;
        }
        Ok(last)
    }

    fn eval_arithmetic(
        &self,
        expr: &Node,
        op: ArithOp,
        args: &[Node],
        env: &Rc<Env>,
    ) -> Result<Value, EvalError> {
        let mut vals = Vec::with_capacity(args.len());
        for a in args {
            vals.push(self.eval_node(a, env)?);
        }
        arithmetic(expr, op, &vals)
    }
}

/// The arithmetic of evalArithmetic on its evaluated operands.
fn arithmetic(expr: &Node, op: ArithOp, vals: &[Value]) -> Result<Value, EvalError> {
    match op {
        ArithOp::Add | ArithOp::Sub | ArithOp::Min | ArithOp::Max => fold(expr, op, vals),
        ArithOp::Mul => multiply(expr, vals),
        ArithOp::Div => divide(expr, vals),
    }
}

/// The error for operands of different kinds.
fn mismatch(expr: &Node, op: ArithOp) -> EvalError {
    err_at(
        Some(expr),
        format!("mismatched operand types for {}", op.name()),
    )
}

/// `+`, `-`, `min` or `max` over any number of operands, all of the first
/// one's kind. `-` of one operand negates it.
fn fold(expr: &Node, op: ArithOp, vals: &[Value]) -> Result<Value, EvalError> {
    if op == ArithOp::Sub && vals.len() == 1 {
        match &vals[0] {
            Value::Money(m) => return Ok(Value::Money(MoneyVal::from_rat(-&*m.r))),
            Value::Float(f) => return Ok(Value::Float(-f)),
            Value::Int(i) => return Ok(Value::Int(i.wrapping_neg())),
            _ => {}
        }
    }
    let Some((first, rest)) = vals.split_first() else {
        return Err(err_at(
            Some(expr),
            format!("{} needs an argument", op.name()),
        ));
    };
    let folded = match first {
        Value::Money(m) => fold_as(
            op,
            (*m.r).clone(),
            rest,
            |v| match v {
                Value::Money(r) => Some((*r.r).clone()),
                _ => None,
            },
            |a, b| a + b,
            |a, b| a - b,
        )
        .map(|r| Value::Money(MoneyVal::from_rat(r))),
        Value::Float(f) => fold_as(
            op,
            *f,
            rest,
            |v| match v {
                Value::Float(r) => Some(*r),
                _ => None,
            },
            |a, b| a + b,
            |a, b| a - b,
        )
        .map(Value::Float),
        Value::Int(i) => fold_as(
            op,
            *i,
            rest,
            |v| match v {
                Value::Int(r) => Some(*r),
                _ => None,
            },
            i64::wrapping_add,
            i64::wrapping_sub,
        )
        .map(Value::Int),
        other => {
            return Err(err_at(
                Some(expr),
                format!("unsupported type {} for {}", other.go_type(), op.name()),
            ));
        }
    };
    folded.ok_or_else(|| mismatch(expr, op))
}

/// Folds `rest` into `first` by `op`, reading each operand with `get`;
/// `None` when one is not of the same kind. `min` and `max` keep the earlier
/// operand on a tie, as Go's loop does.
fn fold_as<T: PartialOrd>(
    op: ArithOp,
    first: T,
    rest: &[Value],
    get: impl Fn(&Value) -> Option<T>,
    add: impl Fn(T, T) -> T,
    sub: impl Fn(T, T) -> T,
) -> Option<T> {
    let mut acc = first;
    for v in rest {
        let x = get(v)?;
        acc = match op {
            ArithOp::Add => add(acc, x),
            ArithOp::Sub => sub(acc, x),
            ArithOp::Min if x < acc => x,
            ArithOp::Max if x > acc => x,
            _ => acc,
        };
    }
    Some(acc)
}

/// `*` of two operands: money by a number, or two numbers.
fn multiply(expr: &Node, vals: &[Value]) -> Result<Value, EvalError> {
    let (lv, rv) = (at_val(vals, 0, expr)?, at_val(vals, 1, expr)?);
    let out = match (lv, rv) {
        (Value::Money(l), Value::Float(r)) => {
            Value::Money(MoneyVal::from_rat(&*l.r * float_to_rat(*r)))
        }
        (Value::Money(l), Value::Int(r)) => Value::Money(MoneyVal::from_rat(
            &*l.r * BigRational::from_integer(BigInt::from(*r)),
        )),
        (Value::Float(l), Value::Money(r)) => {
            Value::Money(MoneyVal::from_rat(float_to_rat(*l) * &*r.r))
        }
        (Value::Float(l), Value::Float(r)) => Value::Float(l * r),
        (Value::Float(l), Value::Int(r)) => Value::Float(l * *r as f64),
        (Value::Int(l), Value::Money(r)) => Value::Money(MoneyVal::from_rat(
            BigRational::from_integer(BigInt::from(*l)) * &*r.r,
        )),
        (Value::Int(l), Value::Float(r)) => Value::Float(*l as f64 * r),
        (Value::Int(l), Value::Int(r)) => Value::Int(l.wrapping_mul(*r)),
        _ => {
            return Err(err_at(
                Some(expr),
                format!(
                    "unsupported types {} and {} for *",
                    lv.go_type(),
                    rv.go_type()
                ),
            ));
        }
    };
    Ok(out)
}

/// `/` of two operands. Money over money is a Float ratio; integers divide
/// as Go's do, truncating.
fn divide(expr: &Node, vals: &[Value]) -> Result<Value, EvalError> {
    let (lv, rv) = (at_val(vals, 0, expr)?, at_val(vals, 1, expr)?);
    let zero = || err_at(Some(expr), "division by zero".to_string());
    let out = match (lv, rv) {
        (Value::Money(l), Value::Float(r)) => {
            if *r == 0.0 {
                return Err(zero());
            }
            let d = float_to_rat(*r);
            if d.is_zero() {
                // Go's big.Rat.Quo panics on a zero divisor,
                // which a NaN or infinite float gives.
                return Err(zero());
            }
            Value::Money(MoneyVal::from_rat(&*l.r / d))
        }
        (Value::Money(l), Value::Int(r)) => {
            if *r == 0 {
                return Err(zero());
            }
            Value::Money(MoneyVal::from_rat(
                &*l.r / BigRational::from_integer(BigInt::from(*r)),
            ))
        }
        (Value::Money(l), Value::Money(r)) => {
            if r.r.is_zero() {
                return Err(zero());
            }
            Value::Float(rat_to_f64(&(&*l.r / &*r.r)))
        }
        (Value::Float(l), Value::Float(r)) => {
            if *r == 0.0 {
                return Err(zero());
            }
            Value::Float(l / r)
        }
        (Value::Float(l), Value::Int(r)) => {
            if *r == 0 {
                return Err(zero());
            }
            Value::Float(l / *r as f64)
        }
        (Value::Int(l), r) => {
            let Value::Int(r) = r else {
                return Err(mismatch(expr, ArithOp::Div));
            };
            if *r == 0 {
                return Err(err_at(Some(expr), "integer division by zero".to_string()));
            }
            Value::Int(l.wrapping_div(*r))
        }
        _ => {
            return Err(err_at(
                Some(expr),
                format!(
                    "unsupported types {} and {} for /",
                    lv.go_type(),
                    rv.go_type()
                ),
            ));
        }
    };
    Ok(out)
}

impl Evaluator {
    fn eval_comparison(
        &self,
        expr: &Node,
        op: CmpOp,
        elems: &[Node],
        env: &Rc<Env>,
    ) -> Result<Value, EvalError> {
        let lv = self.eval_node(at(elems, 1, expr)?, env)?;
        let rv = self.eval_node(at(elems, 2, expr)?, env)?;
        comparison(expr, op, &lv, &rv)
    }
}

/// The comparison of evalComparison on its evaluated operands.
fn comparison(expr: &Node, op: CmpOp, lv: &Value, rv: &Value) -> Result<Value, EvalError> {
    let mismatch = || {
        err_at(
            Some(expr),
            format!("mismatched operand types for {}", op.name()),
        )
    };
    let cmp = match (lv, rv) {
        (Value::Money(l), r) => {
            let Value::Money(r) = r else {
                return Err(mismatch());
            };
            match l.r.cmp(&r.r) {
                std::cmp::Ordering::Less => -1,
                std::cmp::Ordering::Equal => 0,
                std::cmp::Ordering::Greater => 1,
            }
        }
        (Value::Float(l), r) => {
            let Value::Float(r) = r else {
                return Err(mismatch());
            };
            if l < r {
                -1
            } else if l > r {
                1
            } else {
                0
            }
        }
        (Value::Int(l), r) => {
            let Value::Int(r) = r else {
                return Err(mismatch());
            };
            l.cmp(r) as i32
        }
        (Value::Date(l), r) => {
            let Value::Date(r) = r else {
                return Err(mismatch());
            };
            compare_date(l, r)
        }
        _ => {
            return Err(err_at(
                Some(expr),
                format!("cannot compare {} with {}", lv.go_type(), op.name()),
            ));
        }
    };
    Ok(Value::Bool(match op {
        CmpOp::Lt => cmp < 0,
        CmpOp::Le => cmp <= 0,
        CmpOp::Gt => cmp > 0,
        CmpOp::Ge => cmp >= 0,
    }))
}

impl Evaluator {
    /// ValuesEqual reports whether two runtime values are equal.
    pub fn values_equal(&self, a: &Value, b: &Value) -> bool {
        match (a, b) {
            (Value::Nil, Value::Nil) => true,
            (Value::Bool(x), Value::Bool(y)) => x == y,
            (Value::Int(x), Value::Int(y)) => x == y,
            (Value::String(x), Value::String(y)) => x == y,
            (Value::Money(x), Value::Money(y)) => x.r == y.r,
            (Value::Float(x), Value::Float(y)) => x == y,
            (Value::Date(x), Value::Date(y)) => compare_date(x, y) == 0,
            (Value::Symbol(x), Value::Symbol(y)) => {
                x.package == y.package && short_enum_name(&x.name) == short_enum_name(&y.name)
            }
            (Value::Symbol(x), Value::ProtoEnum(y)) | (Value::ProtoEnum(y), Value::Symbol(x)) => {
                short_enum_name(&x.name) == short_enum_name(y.name())
            }
            (Value::ProtoEnum(x), Value::ProtoEnum(y)) => {
                x.desc.full_name() == y.desc.full_name() && x.number == y.number
            }
            (Value::ProtoMessage(x), Value::ProtoMessage(y)) => x.msg == y.msg,
            _ => false,
        }
    }

    fn value_matches_type_expr(&self, val: &Value, type_expr: &Node) -> bool {
        if let Some(sym) = type_expr.sym() {
            if sym.package.is_empty() {
                match &*sym.name {
                    "Nil" => return matches!(val, Value::Nil),
                    "Bool" => return matches!(val, Value::Bool(_)),
                    "Int" => return matches!(val, Value::Int(_)),
                    "String" => return matches!(val, Value::String(_)),
                    "Money" => return matches!(val, Value::Money(_)),
                    "Float" => return matches!(val, Value::Float(_)),
                    "Date" => return matches!(val, Value::Date(_)),
                    name => {
                        if let Value::Struct(sv) = val {
                            return &*sv.type_name == name;
                        }
                    }
                }
            } else if let Ok(desc) = self.resolve_proto_descriptor(&sym.package, &sym.name) {
                match desc {
                    Resolved::Message(idx) => {
                        return matches!(val, Value::ProtoMessage(pm) if pm.msg.idx == idx);
                    }
                    Resolved::Enum(d) => {
                        return matches!(val, Value::ProtoEnum(pe) if pe.desc.full_name() == d.full_name());
                    }
                    Resolved::Other(_) => {}
                }
            }
        }
        if let Some(l) = type_expr.list()
            && let Some(head) = l.elems.first()
        {
            match head.symbol_name() {
                "Union" => {
                    return l.elems[1..]
                        .iter()
                        .any(|m| self.value_matches_type_expr(val, m));
                }
                "Slice" => return matches!(val, Value::Slice(_)),
                head => {
                    if let Value::Struct(sv) = val {
                        return &*sv.type_name == head;
                    }
                }
            }
        }
        false
    }

    /// WrapProto wraps a generated message into a taxlisp value: a
    /// ProtoMessageVal, or a MoneyVal, FloatVal or DateVal for the
    /// taxprotos Money, Quantity and Date messages.
    pub fn wrap_proto<M: buffa::Message + buffa::MessageName>(
        &self,
        msg: &M,
    ) -> Result<Value, EvalError> {
        let pm = ProtoMessageVal::from_message(self.pool, msg)?;
        Ok(wrap_reflect_message(pm.msg))
    }

    fn new_proto_message(&self, idx: MessageIndex) -> ProtoMsg {
        ProtoMsg::new(self.pool, idx)
    }

    fn eval_make_proto(
        &self,
        expr: &Node,
        args: &[Node],
        env: &Rc<Env>,
    ) -> Result<Value, EvalError> {
        let a0 = at(args, 0, expr)?;
        let (pkg, name) = extract_quoted_symbol(a0).map_err(|e| err_at(Some(a0), e))?;
        let idx = match self.resolve_proto_descriptor(pkg, name) {
            Ok(Resolved::Message(idx)) => idx,
            Ok(Resolved::Enum(d)) => {
                return Err(err_at(
                    Some(a0),
                    format!("{} is not a protobuf message", d.full_name()),
                ));
            }
            Ok(Resolved::Other(full)) => {
                return Err(err_at(
                    Some(a0),
                    format!("{full} is not a protobuf message"),
                ));
            }
            Err(e) => return Err(err_at(Some(a0), e)),
        };
        let mut msg = self.new_proto_message(idx);
        self.populate_proto_fields(&mut msg, &args[1..], env)?;
        Ok(wrap_reflect_message(Rc::new(msg)))
    }

    fn eval_proto_with(
        &self,
        expr: &Node,
        args: &[Node],
        env: &Rc<Env>,
    ) -> Result<Value, EvalError> {
        let a0 = at(args, 0, expr)?;
        let base = self.eval_node(a0, env)?;
        let Value::ProtoMessage(pm) = &base else {
            return Err(err_at(
                Some(a0),
                format!(
                    "proto-with target must be a protobuf message, got {}",
                    base.go_type()
                ),
            ));
        };
        let mut cloned = (*pm.msg).clone();
        self.populate_proto_fields(&mut cloned, &args[1..], env)?;
        Ok(Value::ProtoMessage(ProtoMessageVal {
            msg: Rc::new(cloned),
        }))
    }

    fn populate_proto_fields(
        &self,
        msg: &mut ProtoMsg,
        kv_args: &[Node],
        env: &Rc<Env>,
    ) -> Result<(), EvalError> {
        let md = msg.descriptor();
        let mut i = 0;
        while i < kv_args.len() {
            let field_name = kv_args[i].keyword();
            let Some(fd) = field_by_name(md, field_name) else {
                return Err(err_at(
                    Some(&kv_args[i]),
                    format!(
                        "protobuf message {} has no field {}",
                        md.full_name(),
                        quote(field_name)
                    ),
                ));
            };
            let site = at(kv_args, i + 1, &kv_args[i])?;
            let val = self.eval_node(site, env)?;
            self.set_proto_field(site, msg, fd, val)?;
            i += 2;
        }
        Ok(())
    }

    fn set_proto_field(
        &self,
        site: &Node,
        msg: &mut ProtoMsg,
        fd: &FieldDescriptor,
        val: Value,
    ) -> Result<(), EvalError> {
        if let Value::Nil = val {
            msg.clear(fd);
            return Ok(());
        }
        let Some((kind, repeated)) = field_elem_kind(fd) else {
            return Err(err_at(
                Some(site),
                format!("unsupported map field {}", fd.name()),
            ));
        };
        if repeated {
            let Value::Slice(sv) = &val else {
                return Err(err_at(
                    Some(site),
                    format!(
                        "repeated field {} expects Slice, got {}",
                        fd.name(),
                        val.go_type()
                    ),
                ));
            };
            let mut list = Vec::with_capacity(sv.elems.len());
            for elem in sv.elems.iter() {
                list.push(self.value_to_proto_field_value(site, fd, kind, elem)?);
            }
            msg.set(fd, PVal::List(Rc::new(list)));
            return Ok(());
        }
        let pv = self.value_to_proto_field_value(site, fd, kind, &val)?;
        msg.set(fd, pv);
        Ok(())
    }

    fn value_to_proto_field_value(
        &self,
        site: &Node,
        fd: &FieldDescriptor,
        kind: SingularKind,
        val: &Value,
    ) -> Result<PVal, EvalError> {
        let expects = |what: &str| {
            err_at(
                Some(site),
                format!("field {} expects {what}, got {}", fd.name(), val.go_type()),
            )
        };
        match kind {
            SingularKind::Scalar(s) => match s {
                ScalarType::Bool => match val {
                    Value::Bool(b) => Ok(PVal::Bool(*b)),
                    _ => Err(expects("Bool")),
                },
                ScalarType::Int32 | ScalarType::Sint32 | ScalarType::Sfixed32 => match val {
                    Value::Int(i) => Ok(PVal::I32(*i as i32)),
                    _ => Err(expects("Int")),
                },
                ScalarType::Int64 | ScalarType::Sint64 | ScalarType::Sfixed64 => match val {
                    Value::Int(i) => Ok(PVal::I64(*i)),
                    _ => Err(expects("Int")),
                },
                ScalarType::Uint32 | ScalarType::Fixed32 => match val {
                    Value::Int(i) => Ok(PVal::U32(*i as u32)),
                    _ => Err(expects("Int")),
                },
                ScalarType::Uint64 | ScalarType::Fixed64 => match val {
                    Value::Int(i) => Ok(PVal::U64(*i as u64)),
                    _ => Err(expects("Int")),
                },
                ScalarType::Float => match val {
                    Value::Float(f) => Ok(PVal::F32(*f as f32)),
                    _ => Err(expects("Float")),
                },
                ScalarType::Double => match val {
                    Value::Float(f) => Ok(PVal::F64(*f)),
                    _ => Err(expects("Float")),
                },
                ScalarType::String => match val {
                    Value::String(s) => Ok(PVal::Str(Rc::clone(s))),
                    _ => Err(expects("String")),
                },
                ScalarType::Bytes => Err(err_at(
                    Some(site),
                    "unsupported field kind bytes".to_string(),
                )),
            },
            SingularKind::Enum(eidx) => {
                let ed = self.enum_desc(eidx);
                match val {
                    Value::ProtoEnum(v) => Ok(PVal::Enum(v.number)),
                    Value::Symbol(v) => {
                        let raw_name = short_enum_name(&v.name);
                        match ed.value_by_name(raw_name) {
                            Some(evd) => Ok(PVal::Enum(evd.number())),
                            None => Err(err_at(
                                Some(site),
                                format!("enum {} has no value {}", ed.full_name(), v.name),
                            )),
                        }
                    }
                    _ => Err(err_at(
                        Some(site),
                        format!(
                            "field {} expects enum {}, got {}",
                            fd.name(),
                            ed.full_name(),
                            val.go_type()
                        ),
                    )),
                }
            }
            SingularKind::Message(midx) => {
                if fd.is_delimited() {
                    return Err(err_at(
                        Some(site),
                        "unsupported field kind group".to_string(),
                    ));
                }
                let md = self.pool.message(midx);
                match md.full_name() {
                    PROTO_MONEY_FULL_NAME => {
                        let Value::Money(m) = val else {
                            return Err(expects("Money"));
                        };
                        let mut sub = self.new_proto_message(midx);
                        let minor = m.cents();
                        if let Some(f) = field_by_name(md, "currency") {
                            sub.set(f, PVal::Str(Rc::from("USD")));
                        }
                        if minor != 0
                            && let Some(f) = field_by_name(md, "minor")
                        {
                            sub.set(f, PVal::I64(minor));
                        }
                        Ok(PVal::Msg(Rc::new(sub)))
                    }
                    PROTO_QUANTITY_FULL_NAME => {
                        let Value::Float(f) = val else {
                            return Err(expects("Float"));
                        };
                        let mut sub = self.new_proto_message(midx);
                        let (units, scale) = float_to_quantity_units_scale(*f);
                        if units != 0
                            && let Some(fd) = field_by_name(md, "units")
                        {
                            sub.set(fd, PVal::I64(units));
                        }
                        if scale != 0
                            && let Some(fd) = field_by_name(md, "scale")
                        {
                            sub.set(fd, PVal::I32(scale));
                        }
                        Ok(PVal::Msg(Rc::new(sub)))
                    }
                    PROTO_DATE_FULL_NAME => {
                        let Value::Date(d) = val else {
                            return Err(expects("Date"));
                        };
                        let mut sub = self.new_proto_message(midx);
                        for (name, v) in [("year", d.year), ("month", d.month), ("day", d.day)] {
                            if let Some(fd) = field_by_name(md, name) {
                                sub.set(fd, PVal::I32(v));
                            }
                        }
                        Ok(PVal::Msg(Rc::new(sub)))
                    }
                    full => match val {
                        Value::ProtoMessage(pm) => Ok(PVal::Msg(Rc::clone(&pm.msg))),
                        _ => Err(err_at(
                            Some(site),
                            format!("field {} expects {full}, got {}", fd.name(), val.go_type()),
                        )),
                    },
                }
            }
        }
    }

    fn enum_desc(&self, idx: buffa_descriptor::EnumIndex) -> &'static EnumDescriptor {
        let pool: &'static DescriptorPool = self.pool;
        pool.enumeration(idx)
    }

    fn eval_field_access(
        &self,
        expr: &Node,
        list: &ListNode,
        env: &Rc<Env>,
    ) -> Result<Value, EvalError> {
        let elems = &list.elems[..];
        let mut cur = self.eval_node(at(elems, 1, expr)?, env)?;
        let Aux::Path(path) = &list.aux else {
            return Err(err_at(Some(expr), "malformed field access".to_string()));
        };
        let (_, segments) = path.as_ref().map_err(Clone::clone)?;
        let site = at(elems, 2, expr)?;
        for seg in segments {
            cur = match &cur {
                Value::Date(d) => match &**seg {
                    "year" => Value::Int(i64::from(d.year)),
                    "month" => Value::Int(i64::from(d.month)),
                    "day" => Value::Int(i64::from(d.day)),
                    _ => {
                        return Err(err_at(
                            Some(site),
                            format!("Date has no field {}", quote(seg)),
                        ));
                    }
                },
                Value::Struct(s) => match s.fields.get(&**seg) {
                    Some(v) => v.clone(),
                    None => {
                        return Err(err_at(
                            Some(site),
                            format!("struct {} has no field {}", s.type_name, quote(seg)),
                        ));
                    }
                },
                Value::ProtoMessage(pm) => self.read_proto_field_or_oneof(site, &pm.msg, seg)?,
                other => {
                    return Err(err_at(
                        Some(expr),
                        format!(
                            "cannot access field {} on value {other} ({})",
                            quote(seg),
                            other.go_type()
                        ),
                    ));
                }
            };
        }
        Ok(cur)
    }

    fn read_proto_field_or_oneof(
        &self,
        site: &Node,
        msg: &ProtoMsg,
        seg: &str,
    ) -> Result<Value, EvalError> {
        let md = msg.descriptor();
        if let Some(oi) = oneof_by_name(md, seg)
            && !md.oneofs()[oi].is_synthetic()
        {
            return Ok(match msg.which_oneof(oi) {
                Some(fd) => self.read_proto_field_value(msg, fd),
                None => Value::Nil,
            });
        }
        let Some(fd) = field_by_name(md, seg) else {
            return Err(err_at(
                Some(site),
                format!(
                    "protobuf message {} has no field or oneof {}",
                    md.full_name(),
                    quote(seg)
                ),
            ));
        };
        if let Some(oi) = fd.oneof_index() {
            let synthetic = md
                .oneofs()
                .get(oi as usize)
                .is_some_and(|od| od.is_synthetic());
            if !synthetic && !msg.has(fd) {
                return Ok(Value::Nil);
            }
        }
        Ok(self.read_proto_field_value(msg, fd))
    }

    fn read_proto_field_value(&self, msg: &ProtoMsg, fd: &FieldDescriptor) -> Value {
        let Some((kind, repeated)) = field_elem_kind(fd) else {
            // A map field, which no rule reads (Go would panic reading a
            // non-empty one).
            return Value::Nil;
        };
        if repeated {
            let elems = match msg.get(fd) {
                Some(PVal::List(l)) => l
                    .iter()
                    .map(|pv| self.proto_scalar_or_message_to_value(kind, Some(pv)))
                    .collect(),
                _ => Vec::new(),
            };
            return Value::Slice(SliceVal::new(elems));
        }
        if let SingularKind::Message(midx) = kind
            && !fd.is_delimited()
            && !msg.has(fd)
        {
            return match self.pool.message(midx).full_name() {
                PROTO_MONEY_FULL_NAME => Value::Money(MoneyVal::from_rat(BigRational::zero())),
                PROTO_QUANTITY_FULL_NAME => Value::Float(0.0),
                PROTO_DATE_FULL_NAME => Value::Date(DateVal::default()),
                _ => Value::ProtoMessage(ProtoMessageVal {
                    msg: Rc::new(self.new_proto_message(midx)),
                }),
            };
        }
        if fd.is_delimited() {
            return Value::Nil;
        }
        self.proto_scalar_or_message_to_value(kind, msg.get(fd))
    }

    /// protoScalarOrMessageToValue converts a field value (None for an
    /// unset field, which reads as its zero value).
    fn proto_scalar_or_message_to_value(&self, kind: SingularKind, pv: Option<&PVal>) -> Value {
        match kind {
            SingularKind::Scalar(s) => match s {
                ScalarType::Bool => Value::Bool(matches!(pv, Some(PVal::Bool(true)))),
                ScalarType::Int32 | ScalarType::Sint32 | ScalarType::Sfixed32 => {
                    Value::Int(match pv {
                        Some(PVal::I32(v)) => i64::from(*v),
                        _ => 0,
                    })
                }
                ScalarType::Int64 | ScalarType::Sint64 | ScalarType::Sfixed64 => {
                    Value::Int(match pv {
                        Some(PVal::I64(v)) => *v,
                        _ => 0,
                    })
                }
                ScalarType::Uint32 | ScalarType::Fixed32 => Value::Int(match pv {
                    Some(PVal::U32(v)) => i64::from(*v),
                    _ => 0,
                }),
                ScalarType::Uint64 | ScalarType::Fixed64 => Value::Int(match pv {
                    Some(PVal::U64(v)) => *v as i64,
                    _ => 0,
                }),
                ScalarType::Float => Value::Float(match pv {
                    Some(PVal::F32(v)) => f64::from(*v),
                    _ => 0.0,
                }),
                ScalarType::Double => Value::Float(match pv {
                    Some(PVal::F64(v)) => *v,
                    _ => 0.0,
                }),
                ScalarType::String => match pv {
                    Some(PVal::Str(s)) => Value::String(Rc::clone(s)),
                    _ => Value::String(Rc::from("")),
                },
                ScalarType::Bytes => Value::Nil,
            },
            SingularKind::Enum(eidx) => Value::ProtoEnum(ProtoEnumVal {
                desc: self.enum_desc(eidx),
                number: match pv {
                    Some(PVal::Enum(n)) => *n,
                    _ => 0,
                },
            }),
            SingularKind::Message(midx) => match pv {
                Some(PVal::Msg(m)) => wrap_reflect_message(Rc::clone(m)),
                _ => wrap_reflect_message(Rc::new(self.new_proto_message(midx))),
            },
        }
    }

    fn eval_has_field(
        &self,
        expr: &Node,
        list: &ListNode,
        env: &Rc<Env>,
    ) -> Result<Value, EvalError> {
        let elems = &list.elems[..];
        let a0 = at(elems, 1, expr)?;
        let target = self.eval_node(a0, env)?;
        let Value::ProtoMessage(pm) = &target else {
            return Err(err_at(
                Some(a0),
                format!(
                    "has? target must be a protobuf message, got {}",
                    target.go_type()
                ),
            ));
        };
        let Aux::Path(path) = &list.aux else {
            return Err(err_at(Some(expr), "malformed has?".to_string()));
        };
        let (field_name, _) = path.as_ref().map_err(Clone::clone)?;
        let md = pm.msg.descriptor();
        let Some(fd) = field_by_name(md, field_name) else {
            return Err(err_at(
                Some(at(elems, 2, expr)?),
                format!(
                    "protobuf message {} has no field {}",
                    md.full_name(),
                    quote(field_name)
                ),
            ));
        };
        Ok(Value::Bool(pm.msg.has(fd)))
    }

    fn eval_which_oneof(
        &self,
        expr: &Node,
        list: &ListNode,
        env: &Rc<Env>,
    ) -> Result<Value, EvalError> {
        let elems = &list.elems[..];
        let a0 = at(elems, 1, expr)?;
        let target = self.eval_node(a0, env)?;
        let Value::ProtoMessage(pm) = &target else {
            return Err(err_at(
                Some(a0),
                format!(
                    "which-oneof target must be a protobuf message, got {}",
                    target.go_type()
                ),
            ));
        };
        let Aux::Path(path) = &list.aux else {
            return Err(err_at(Some(expr), "malformed which-oneof".to_string()));
        };
        let (oneof_name, _) = path.as_ref().map_err(Clone::clone)?;
        let md = pm.msg.descriptor();
        let Some(oi) = oneof_by_name(md, oneof_name) else {
            return Err(err_at(
                Some(at(elems, 2, expr)?),
                format!(
                    "protobuf message {} has no oneof {}",
                    md.full_name(),
                    quote(oneof_name)
                ),
            ));
        };
        Ok(match pm.msg.which_oneof(oi) {
            Some(fd) => Value::string(fd.name()),
            None => Value::string(""),
        })
    }

    /// resolveProtoDescriptor finds `alias/Name` in the descriptor pool,
    /// with the error text of Go's `Files.FindDescriptorByName` (protoregistry.NotFound).
    fn resolve_proto_descriptor(&self, pkg_alias: &str, name: &str) -> Result<Resolved, String> {
        let full_name = {
            let aliases = self.proto_aliases.borrow();
            let full_pkg = aliases.get(pkg_alias).map_or(pkg_alias, String::as_str);
            format!("{full_pkg}.{name}")
        };
        let pool: &'static DescriptorPool = self.pool;
        if let Some(idx) = pool.message_index(&full_name) {
            return Ok(Resolved::Message(idx));
        }
        if let Some(ed) = pool.enum_by_name(&full_name) {
            return Ok(Resolved::Enum(ed));
        }
        if pool.extension_by_name(&full_name).is_some()
            || pool.service_by_name(&full_name).is_some()
        {
            return Ok(Resolved::Other(full_name));
        }
        let parent = parent_name(&full_name);
        let leaf = match full_name.rfind('.') {
            Some(i) => &full_name[i + 1..],
            None => full_name.as_str(),
        };
        // An enum value is scoped to its enum's parent, and a field to its
        // message.
        if let Some(md) = pool.message_by_name(parent)
            && (field_by_name(md, leaf).is_some() || md.oneofs().iter().any(|o| o.name() == leaf))
        {
            return Ok(Resolved::Other(full_name));
        }
        let is_enum_value = pool
            .enums()
            .iter()
            .any(|ed| parent_name(ed.full_name()) == parent && ed.value_by_name(leaf).is_some());
        if is_enum_value {
            return Ok(Resolved::Other(full_name));
        }
        Err("proto: not found".to_string())
    }
}

/// at returns elems[i], or an error where Go would index past the end and
/// panic.
fn at<'a>(elems: &'a [Node], i: usize, expr: &Node) -> Result<&'a Node, EvalError> {
    elems
        .get(i)
        .ok_or_else(|| err_at(Some(expr), "malformed expression".to_string()))
}

fn at_val<'a>(vals: &'a [Value], i: usize, expr: &Node) -> Result<&'a Value, EvalError> {
    vals.get(i)
        .ok_or_else(|| err_at(Some(expr), "missing operand".to_string()))
}

/// wrapReflectMessage turns the taxprotos Money, Quantity and Date messages
/// into Money, Float and Date values, and wraps any other message.
fn wrap_reflect_message(pm: Rc<ProtoMsg>) -> Value {
    let md = pm.descriptor();
    let int_field = |name: &str| -> i64 {
        match field_by_name(md, name).and_then(|fd| pm.get(fd)) {
            Some(PVal::I64(v)) => *v,
            Some(PVal::I32(v)) => i64::from(*v),
            Some(PVal::U32(v)) => i64::from(*v),
            Some(PVal::U64(v)) => *v as i64,
            _ => 0,
        }
    };
    match md.full_name() {
        PROTO_MONEY_FULL_NAME => Value::Money(MoneyVal::new_cents(int_field("minor"))),
        PROTO_QUANTITY_FULL_NAME => {
            let units = int_field("units");
            let scale = int_field("scale");
            Value::Float(units as f64 / pow10(scale))
        }
        PROTO_DATE_FULL_NAME => Value::Date(DateVal {
            year: int_field("year") as i32,
            month: int_field("month") as i32,
            day: int_field("day") as i32,
        }),
        _ => Value::ProtoMessage(ProtoMessageVal { msg: pm }),
    }
}

fn short_enum_name(s: &str) -> &str {
    match s.rfind('.') {
        Some(i) => &s[i + 1..],
        None => s,
    }
}

/// Interns source file names so positions share them.
#[derive(Default)]
struct FileCache {
    last: Option<(String, Rc<str>)>,
}

impl FileCache {
    fn pos(&mut self, syn: Option<&taxlispv1::SyntaxDetails>) -> Option<Pos> {
        let syn = syn?;
        if syn.start_line <= 0 {
            return None;
        }
        let file = if syn.file.is_empty() {
            "<input>"
        } else {
            syn.file.as_str()
        };
        let rc = match &self.last {
            Some((name, rc)) if name == file => Rc::clone(rc),
            _ => {
                let rc: Rc<str> = Rc::from(file);
                self.last = Some((file.to_string(), Rc::clone(&rc)));
                rc
            }
        };
        Some(Pos {
            file: rc,
            line: syn.start_line,
            col: syn.start_column,
        })
    }
}

// ---------------------------------------------------------------------------
// Arithmetic helpers
// ---------------------------------------------------------------------------

/// roundRatScale rounds r to the nearest 1/scale using half-away-from-zero
/// rounding.
fn round_rat_scale(r: &BigRational, scale: i64) -> BigRational {
    if r.is_zero() {
        return BigRational::zero();
    }
    let neg = r.is_negative();
    let scaled = r.abs() * BigRational::from_integer(BigInt::from(scale))
        + BigRational::new(BigInt::from(1), BigInt::from(2));
    // Add 1/2 for half-up on magnitude (half-away-from-zero).
    let mut int_part = scaled.numer() / scaled.denom();
    if neg {
        int_part = -int_part;
    }
    BigRational::new(int_part, BigInt::from(scale))
}

fn format_rat_fixed(r: &BigRational, decimals: u32) -> String {
    let scale = 10i64.pow(decimals);
    let rounded = round_rat_scale(r, scale);
    let sign = if rounded.is_negative() { "-" } else { "" };
    let abs = rounded.abs();
    let scale_big = BigInt::from(scale);
    let scaled_num = abs.numer() * &scale_big / abs.denom();
    let (whole, frac) = scaled_num.div_rem(&scale_big);
    if decimals == 0 {
        return format!("{sign}{whole}");
    }
    format!(
        "{sign}{whole}.{:0>width$}",
        frac.to_string(),
        width = decimals as usize
    )
}

/// floatToRat is the exact value of f's shortest decimal representation
/// (Go parses `strconv.FormatFloat(f, 'g', -1, 64)` into a big.Rat).
fn float_to_rat(f: f64) -> BigRational {
    if f.is_nan() || f.is_infinite() || f == 0.0 {
        return BigRational::zero();
    }
    let s = format!("{f:e}");
    match parse_rat(&s) {
        Some(r) => r,
        None => BigRational::zero(),
    }
}

/// Go's `big.Rat.Float64`: the nearest float64, ties to even.
fn rat_to_f64(r: &BigRational) -> f64 {
    if r.is_zero() {
        return 0.0;
    }
    let neg = r.is_negative();
    let a: BigUint = r.numer().magnitude().clone();
    let b: BigUint = r.denom().magnitude().clone();
    let exp = a.bits() as i64 - b.bits() as i64;
    // Scale so the quotient has 55 or 56 bits: 53 for the mantissa and the
    // rest, with the remainder, decide the rounding.
    let shift = 55 - exp;
    let (n, d) = if shift >= 0 {
        (a << (shift as usize), b)
    } else {
        (a, b << ((-shift) as usize))
    };
    let (q, rem) = n.div_rem(&d);
    let qbits = q.bits() as i64;
    let drop = (qbits - 53).max(0) as u32;
    let q64 = q.to_u64().unwrap_or(u64::MAX);
    let mut mant = if drop == 0 { q64 } else { q64 >> drop };
    if drop > 0 {
        let low = q64 & ((1u64 << drop) - 1);
        let half = 1u64 << (drop - 1);
        let sticky = !rem.is_zero();
        if low > half || (low == half && (sticky || mant & 1 == 1)) {
            mant += 1;
        }
    }
    let v = ldexp(mant as f64, i64::from(drop) - shift);
    if neg { -v } else { v }
}

fn ldexp(mut x: f64, mut e: i64) -> f64 {
    while e > 1000 {
        x *= 2f64.powi(1000);
        e -= 1000;
    }
    while e < -1000 {
        x *= 2f64.powi(-1000);
        e += 1000;
    }
    x * 2f64.powi(e as i32)
}

/// Go's `math.Pow10`, whose products of table entries are not always the
/// correctly rounded power.
fn pow10(n: i64) -> f64 {
    let tab = |i: i64| -> f64 { format!("1e{i}").parse().unwrap_or(f64::INFINITY) };
    if (0..=308).contains(&n) {
        return tab(32 * (n / 32)) * tab(n % 32);
    }
    if (-323..=0).contains(&n) {
        let m = -n;
        let neg: f64 = format!("1e-{}", 32 * (m / 32)).parse().unwrap_or(0.0);
        return neg / tab(m % 32);
    }
    if n > 0 { f64::INFINITY } else { 0.0 }
}

/// floatToQuantityUnitsScale: f as a decimal with at most 9 places, as
/// integer units and a scale.
fn float_to_quantity_units_scale(f: f64) -> (i64, i32) {
    if f == 0.0 || f.is_nan() || f.is_infinite() {
        return (0, 0);
    }
    let mut s = format!("{f:.9}");
    if s.contains('.') {
        let trimmed = s.trim_end_matches('0').trim_end_matches('.').to_string();
        s = trimmed;
    }
    if s.is_empty() || s == "-0" || s == "0" {
        return (0, 0);
    }
    if let Some(idx) = s.find('.') {
        let scale = (s.len() - idx - 1) as i32;
        let digits = format!("{}{}", &s[..idx], &s[idx + 1..]);
        return (go_parse_int(&digits), scale);
    }
    (go_parse_int(&s), 0)
}

/// strconv.ParseInt(s, 10, 64) ignoring the error: the value, saturated on
/// overflow, or 0 for a malformed string.
fn go_parse_int(s: &str) -> i64 {
    match s.parse::<i64>() {
        Ok(v) => v,
        Err(_) => {
            let (neg, digits) = match s.strip_prefix('-') {
                Some(d) => (true, d),
                None => (false, s.strip_prefix('+').unwrap_or(s)),
            };
            if !digits.is_empty() && digits.bytes().all(|c| c.is_ascii_digit()) {
                if neg { i64::MIN } else { i64::MAX }
            } else {
                0
            }
        }
    }
}

// ---------------------------------------------------------------------------
// Syntax helpers
// ---------------------------------------------------------------------------

fn parse_def_name(expr: &Node) -> Result<String, EvalError> {
    if let Some(s) = expr.sym() {
        return Ok(s.full.to_string());
    }
    if let Some(l) = expr.list()
        && let Some(first) = l.elems.first()
    {
        return Ok(first.symbol_name().to_string());
    }
    Err(err_at(Some(expr), "invalid definition header".to_string()))
}

impl Evaluator {
    fn parse_param_names(&self, expr: &Node) -> Result<Rc<[SymId]>, EvalError> {
        let Some(l) = expr.list() else {
            return Err(err_at(
                Some(expr),
                "parameter list must be a list".to_string(),
            ));
        };
        let mut names = Vec::with_capacity(l.elems.len());
        for p in &l.elems {
            match p.list() {
                Some(pl) if pl.elems.len() == 2 => {
                    names.push(self.intern(pl.elems[0].symbol_name()))
                }
                _ => return Err(err_at(Some(p), "parameter must be (name Type)".to_string())),
            }
        }
        Ok(Rc::from(names))
    }
}

fn is_bare_symbol_list(elems: &[Node]) -> bool {
    elems.iter().all(|e| {
        let s = e.symbol_name();
        !(s.is_empty() || s.as_bytes()[0].is_ascii_lowercase())
    })
}

fn extract_struct_name(expr: &Node) -> &str {
    let s = expr.symbol_name();
    if !s.is_empty() {
        return s;
    }
    if let Some(l) = expr.list()
        && let Some(first) = l.elems.first()
    {
        return first.symbol_name();
    }
    ""
}

fn full_symbol_name(package: &str, name: &str) -> String {
    if package.is_empty() {
        return name.to_string();
    }
    format!("{package}/{name}")
}

fn extract_quoted_or_string(expr: &Node) -> Result<String, String> {
    if let Some(a) = expr.atom() {
        if let Atom::Str(s) = a {
            return Ok(s.to_string());
        }
        if let Atom::Symbol(s) = a {
            return Ok(s.full.to_string());
        }
    }
    if let Some(l) = expr.list()
        && l.elems.len() == 2
        && l.elems[0].symbol_name() == "quote"
        && let Some(s) = l.elems[1].sym()
    {
        return Ok(s.full.to_string());
    }
    Err("expected quoted symbol or string".to_string())
}

fn extract_quoted_symbol(expr: &Node) -> Result<(&str, &str), String> {
    if let Some(s) = expr.sym() {
        return Ok((&s.package, &s.name));
    }
    if let Some(l) = expr.list()
        && l.elems.len() == 2
        && l.elems[0].symbol_name() == "quote"
        && let Some(s) = l.elems[1].sym()
    {
        return Ok((&s.package, &s.name));
    }
    Err("expected quoted symbol 'alias/TypeName".to_string())
}

/// errAt prefixes msg with the expression's source position, when it has
/// one.
fn err_at(expr: Option<&Node>, msg: String) -> EvalError {
    match expr.and_then(|e| e.0.pos.as_ref()) {
        Some(Pos { file, line, col }) => EvalError::at(
            crate::SourcePos {
                file: file.to_string(),
                line: (*line).into(),
                column: (*col).into(),
            },
            msg,
        ),
        None => EvalError::new(msg),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn rat_to_f64_matches_division() {
        for (n, d) in [
            (1i64, 3i64),
            (2, 3),
            (1, 10),
            (123456789, 100),
            (-7, 9),
            (1, 1 << 40),
        ] {
            let r = BigRational::new(BigInt::from(n), BigInt::from(d));
            assert_eq!(rat_to_f64(&r), n as f64 / d as f64, "{n}/{d}");
        }
    }

    #[test]
    fn quantity_units_scale() {
        assert_eq!(float_to_quantity_units_scale(27.5), (275, 1));
        assert_eq!(float_to_quantity_units_scale(39.0), (39, 0));
        assert_eq!(float_to_quantity_units_scale(1.0 / 3.0), (333333333, 9));
        assert_eq!(float_to_quantity_units_scale(-0.5), (-5, 1));
        assert_eq!(float_to_quantity_units_scale(1e-10), (0, 0));
    }

    #[test]
    fn fixed_formatting_matches_go() {
        // strconv.FormatFloat(f, 'f', 9, 64) and (f, 'f', 1, 64).
        assert_eq!(format!("{:.9}", 0.0000000005), "0.000000001");
        assert_eq!(format!("{:.1}", 0.125), "0.1");
        assert_eq!(format!("{:.0}", 2.5), "2");
        assert_eq!(format!("{:.0}", 27.5), "28");
    }

    #[test]
    fn money_rounding() {
        let m = MoneyVal::parse("-2.505").unwrap();
        assert_eq!(m.cents(), -251);
        assert_eq!(m.format_cents(), "-2.51");
        assert_eq!(MoneyVal::parse("2.5").unwrap().dollars(), 3);
        assert_eq!(MoneyVal::parse("0.004").unwrap().format_cents(), "0.00");
        assert_eq!(MoneyVal::parse("-0.004").unwrap().format_cents(), "0.00");
    }

    #[test]
    fn pow10_matches_table() {
        assert_eq!(pow10(0), 1.0);
        assert_eq!(pow10(1), 10.0);
        assert_eq!(pow10(22), 1e22);
        assert_eq!(pow10(-1), 0.1);
    }
}
