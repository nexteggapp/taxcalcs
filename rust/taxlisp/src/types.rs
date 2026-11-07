//! The static type system and typechecker for taxlisp, a strongly typed,
//! Arc-inspired functional Lisp with slices, tables, union types, explicit
//! generics, and native protobuf interaction, ported from the Go package
//! `taxlisp/types`.
//!
//! Go's checker reflects over `protoregistry.Files`; this one reflects over a
//! buffa [`DescriptorPool`], by default the one built from
//! [`proto::DESCRIPTORS`] ([`global_files`]).

use std::collections::{HashMap, HashSet};
use std::fmt;
use std::rc::Rc;
use std::sync::{Arc, OnceLock};

use buffa_descriptor::{DescriptorPool, FieldDescriptor, FieldKind, ScalarType, SingularKind};
use proto::taxlispv1;
use proto::taxlispv1::__buffa::oneof::atom::Primitive;
use proto::taxlispv1::__buffa::oneof::expression::Value;

use crate::parser::quote;

crate::located_error! {
    /// A type error, positioned at the expression it is about when that has
    /// a source location.
    TypeError
}

/// The descriptor pool of every schema in [`proto::DESCRIPTORS`], built
/// once: the counterpart of Go's `protoregistry.GlobalFiles`, which
/// `NewChecker(nil)` defaults to.
pub fn global_files() -> Result<Arc<DescriptorPool>, TypeError> {
    static POOL: OnceLock<Result<Arc<DescriptorPool>, String>> = OnceLock::new();
    POOL.get_or_init(|| {
        let opts = buffa::DecodeOptions::new().with_element_memory_limit(1 << 30);
        DescriptorPool::decode_with_options(proto::DESCRIPTORS, &opts)
            .map(Arc::new)
            .map_err(|e| format!("loading the protobuf descriptors: {e}"))
    })
    .clone()
    .map_err(TypeError::new)
}

/// PrimitiveKind enumerates the built-in scalar types.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash)]
pub enum PrimitiveKind {
    /// `Money`.
    Money,
    /// `Float`.
    Float,
    /// `Int`.
    Int,
    /// `Bool`.
    Bool,
    /// `String`.
    String,
    /// `Date`.
    Date,
    /// `Symbol`.
    Symbol,
    /// `Nil`.
    Nil,
}

impl PrimitiveKind {
    /// The kind's name, as written in source.
    pub fn as_str(self) -> &'static str {
        match self {
            PrimitiveKind::Money => "Money",
            PrimitiveKind::Float => "Float",
            PrimitiveKind::Int => "Int",
            PrimitiveKind::Bool => "Bool",
            PrimitiveKind::String => "String",
            PrimitiveKind::Date => "Date",
            PrimitiveKind::Symbol => "Symbol",
            PrimitiveKind::Nil => "Nil",
        }
    }

    /// The kind named `name`, if it is one.
    pub fn from_name(name: &str) -> Option<PrimitiveKind> {
        Some(match name {
            "Money" => PrimitiveKind::Money,
            "Float" => PrimitiveKind::Float,
            "Int" => PrimitiveKind::Int,
            "Bool" => PrimitiveKind::Bool,
            "String" => PrimitiveKind::String,
            "Date" => PrimitiveKind::Date,
            "Symbol" => PrimitiveKind::Symbol,
            "Nil" => PrimitiveKind::Nil,
            _ => return None,
        })
    }
}

/// Type represents a static type in taxlisp.
#[derive(Clone, Debug)]
pub enum Type {
    /// A built-in scalar type.
    Primitive(PrimitiveKind),
    /// A generic type parameter bound by `(def (fname T U) ...)` or
    /// `(defstruct Name (T U) ...)`.
    TypeVar(String),
    /// A contiguous slice `(Slice Elem)`.
    Slice(Box<Type>),
    /// An immutable map `(Table Key Value)`.
    Table(Box<Type>, Box<Type>),
    /// An unordered, flattened, deduplicated union `(Union T1 T2 ...)`; build
    /// one with [`new_union`].
    Union(Vec<Type>),
    /// A function type.
    Fn(FnType),
    /// An instantiated user-defined struct type.
    Struct(StructType),
    /// A protobuf message type, by full name.
    ProtoMessage(String),
    /// A protobuf enum type, by full name.
    ProtoEnum(String),
}

/// `Money`.
pub const TYPE_MONEY: Type = Type::Primitive(PrimitiveKind::Money);
/// `Float`.
pub const TYPE_FLOAT: Type = Type::Primitive(PrimitiveKind::Float);
/// `Int`.
pub const TYPE_INT: Type = Type::Primitive(PrimitiveKind::Int);
/// `Bool`.
pub const TYPE_BOOL: Type = Type::Primitive(PrimitiveKind::Bool);
/// `String`.
pub const TYPE_STRING: Type = Type::Primitive(PrimitiveKind::String);
/// `Date`.
pub const TYPE_DATE: Type = Type::Primitive(PrimitiveKind::Date);
/// `Symbol`.
pub const TYPE_SYMBOL: Type = Type::Primitive(PrimitiveKind::Symbol);
/// `Nil`.
pub const TYPE_NIL: Type = Type::Primitive(PrimitiveKind::Nil);

/// FnType is a function type `(Fn (P1 P2 ...) Return)` or a generic function
/// with TypeParams.
#[derive(Clone, Debug)]
pub struct FnType {
    /// The generic type parameters, empty for a monomorphic function.
    pub type_params: Vec<String>,
    /// The parameter types.
    pub params: Vec<Type>,
    /// The return type.
    pub ret: Box<Type>,
}

/// StructField describes a single named field of a StructDef.
#[derive(Clone, Debug)]
pub struct StructField {
    /// The field name.
    pub name: String,
    /// The field type, which may mention the struct's type parameters.
    pub typ: Type,
}

/// StructDef describes a nominal struct defined via `defstruct`.
#[derive(Clone, Debug)]
pub struct StructDef {
    /// The struct name.
    pub name: String,
    /// The generic type parameters.
    pub type_params: Vec<String>,
    /// The fields, in declaration order.
    pub fields: Vec<StructField>,
}

/// StructType is an instantiated user-defined struct type.
#[derive(Clone, Debug)]
pub struct StructType {
    /// The definition.
    pub def: Rc<StructDef>,
    /// The type arguments, one per type parameter.
    pub type_args: Vec<Type>,
}

fn join(types: &[Type]) -> String {
    types
        .iter()
        .map(Type::to_string)
        .collect::<Vec<_>>()
        .join(" ")
}

impl fmt::Display for Type {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Type::Primitive(k) => f.write_str(k.as_str()),
            Type::TypeVar(name) => f.write_str(name),
            Type::Slice(elem) => write!(f, "(Slice {elem})"),
            Type::Table(k, v) => write!(f, "(Table {k} {v})"),
            Type::Union(members) => write!(f, "(Union {})", join(members)),
            Type::Fn(fnt) => {
                let ps = join(&fnt.params);
                if !fnt.type_params.is_empty() {
                    return write!(
                        f,
                        "(forall ({}) (Fn ({}) {}))",
                        fnt.type_params.join(" "),
                        ps,
                        fnt.ret
                    );
                }
                write!(f, "(Fn ({}) {})", ps, fnt.ret)
            }
            Type::Struct(s) => {
                if s.type_args.is_empty() {
                    return f.write_str(&s.def.name);
                }
                write!(f, "({} {})", s.def.name, join(&s.type_args))
            }
            Type::ProtoMessage(name) | Type::ProtoEnum(name) => f.write_str(name),
        }
    }
}

impl Type {
    /// Go's `Equals`: structural equality, with structs compared by name
    /// and protobuf types by full name.
    pub fn equals(&self, o: &Type) -> bool {
        match (self, o) {
            (Type::Primitive(a), Type::Primitive(b)) => a == b,
            (Type::TypeVar(a), Type::TypeVar(b)) => a == b,
            (Type::Slice(a), Type::Slice(b)) => a.equals(b),
            (Type::Table(ak, av), Type::Table(bk, bv)) => ak.equals(bk) && av.equals(bv),
            (Type::Union(a), Type::Union(b)) => {
                a.len() == b.len() && a.iter().zip(b).all(|(x, y)| x.equals(y))
            }
            (Type::Fn(a), Type::Fn(b)) => {
                a.type_params.len() == b.type_params.len()
                    && a.params.len() == b.params.len()
                    && a.type_params == b.type_params
                    && a.params.iter().zip(&b.params).all(|(x, y)| x.equals(y))
                    && a.ret.equals(&b.ret)
            }
            (Type::Struct(a), Type::Struct(b)) => {
                a.def.name == b.def.name
                    && a.type_args.len() == b.type_args.len()
                    && a.type_args
                        .iter()
                        .zip(&b.type_args)
                        .all(|(x, y)| x.equals(y))
            }
            (Type::ProtoMessage(a), Type::ProtoMessage(b)) => a == b,
            (Type::ProtoEnum(a), Type::ProtoEnum(b)) => a == b,
            _ => false,
        }
    }
}

impl PartialEq for Type {
    fn eq(&self, other: &Type) -> bool {
        self.equals(other)
    }
}

/// NewUnion constructs a flattened, deduplicated UnionType (or returns the
/// sole member type if only one distinct member remains).
pub fn new_union(types: Vec<Type>) -> Type {
    fn add(flat: &mut Vec<Type>, t: Type) {
        if let Type::Union(members) = t {
            for m in members {
                add(flat, m);
            }
            return;
        }
        if flat.iter().any(|existing| existing.equals(&t)) {
            return;
        }
        flat.push(t);
    }
    let mut flat = Vec::new();
    for t in types {
        add(&mut flat, t);
    }
    if flat.len() == 1 {
        return flat.remove(0);
    }
    // Go's sort.Slice is an insertion sort, so stable, for the short slices
    // unions are.
    let mut keyed: Vec<(String, Type)> = flat.into_iter().map(|t| (t.to_string(), t)).collect();
    keyed.sort_by(|a, b| a.0.cmp(&b.0));
    Type::Union(keyed.into_iter().map(|(_, t)| t).collect())
}

impl StructType {
    /// FieldType returns the instantiated type of field `name` on this
    /// struct.
    pub fn field_type(&self, name: &str) -> Option<Type> {
        let mut subst = HashMap::new();
        for (i, tp) in self.def.type_params.iter().enumerate() {
            if let Some(arg) = self.type_args.get(i) {
                subst.insert(tp.clone(), arg.clone());
            }
        }
        self.def
            .fields
            .iter()
            .find(|f| f.name == name)
            .map(|f| substitute(&f.typ, &subst))
    }
}

/// IsAssignableTo reports whether a value of type `got` can be used where a
/// value of type `want` is expected (including Union introduction &
/// subsetting).
pub fn is_assignable_to(got: &Type, want: &Type) -> bool {
    if got.equals(want) {
        return true;
    }
    if let Type::Union(members) = got {
        return members.iter().all(|m| is_assignable_to(m, want));
    }
    if let Type::Union(members) = want {
        return members.iter().any(|m| is_assignable_to(got, m));
    }
    if let (Type::Slice(g), Type::Slice(w)) = (got, want) {
        return is_assignable_to(g, w);
    }
    false
}

/// Substitute replaces TypeVars in t according to subst.
pub fn substitute(t: &Type, subst: &HashMap<String, Type>) -> Type {
    if subst.is_empty() {
        return t.clone();
    }
    match t {
        Type::TypeVar(name) => subst.get(name).cloned().unwrap_or_else(|| t.clone()),
        Type::Slice(elem) => Type::Slice(Box::new(substitute(elem, subst))),
        Type::Table(k, v) => Type::Table(
            Box::new(substitute(k, subst)),
            Box::new(substitute(v, subst)),
        ),
        Type::Union(members) => new_union(members.iter().map(|m| substitute(m, subst)).collect()),
        Type::Fn(f) => {
            let filtered: HashMap<String, Type> = subst
                .iter()
                .filter(|(k, _)| !f.type_params.contains(k))
                .map(|(k, v)| (k.clone(), v.clone()))
                .collect();
            Type::Fn(FnType {
                type_params: f.type_params.clone(),
                params: f.params.iter().map(|p| substitute(p, &filtered)).collect(),
                ret: Box::new(substitute(&f.ret, &filtered)),
            })
        }
        Type::Struct(s) => Type::Struct(StructType {
            def: s.def.clone(),
            type_args: s.type_args.iter().map(|a| substitute(a, subst)).collect(),
        }),
        _ => t.clone(),
    }
}

/// Unify matches pattern type `pat` (containing TypeVars in `type_params`)
/// against concrete type `arg`, recording bindings in `subst`. The error is
/// the bare message, as Go's is.
pub fn unify(
    pat: &Type,
    arg: &Type,
    type_params: &HashSet<String>,
    subst: &mut HashMap<String, Type>,
) -> Result<(), String> {
    let mismatch = || format!("got {arg}, want {pat}");
    match pat {
        Type::TypeVar(name) => {
            if type_params.contains(name) {
                if let Some(existing) = subst.get(name) {
                    if is_assignable_to(arg, existing) {
                        return Ok(());
                    }
                    if is_assignable_to(existing, arg) {
                        subst.insert(name.clone(), arg.clone());
                        return Ok(());
                    }
                    return Err(format!(
                        "type parameter {name} bound to conflicting types {existing} and {arg}"
                    ));
                }
                subst.insert(name.clone(), arg.clone());
                return Ok(());
            }
            if !is_assignable_to(arg, pat) {
                return Err(mismatch());
            }
            Ok(())
        }
        Type::Slice(pelem) => {
            let Type::Slice(aelem) = arg else {
                return Err(mismatch());
            };
            unify(pelem, aelem, type_params, subst)
        }
        Type::Table(pk, pv) => {
            let Type::Table(ak, av) = arg else {
                return Err(mismatch());
            };
            unify(pk, ak, type_params, subst)?;
            unify(pv, av, type_params, subst)
        }
        Type::Fn(pf) => {
            let Type::Fn(af) = arg else {
                return Err(mismatch());
            };
            if pf.params.len() != af.params.len() {
                return Err(mismatch());
            }
            for (p, a) in pf.params.iter().zip(&af.params) {
                unify(p, a, type_params, subst)?;
            }
            unify(&pf.ret, &af.ret, type_params, subst)
        }
        Type::Struct(ps) => {
            let Type::Struct(as_) = arg else {
                return Err(mismatch());
            };
            if ps.def.name != as_.def.name || ps.type_args.len() != as_.type_args.len() {
                return Err(mismatch());
            }
            for (p, a) in ps.type_args.iter().zip(&as_.type_args) {
                unify(p, a, type_params, subst)?;
            }
            Ok(())
        }
        Type::Union(pm) => {
            // If pattern is `(Union T Nil)` and arg is `(Union Concrete Nil)`:
            // Match non-Nil against non-Nil when Nil is in both.
            if let Type::Union(am) = arg
                && pm.len() == 2
                && am.len() == 2
                && let (Some(p_non_nil), Some(a_non_nil)) = (non_nil_member(pm), non_nil_member(am))
            {
                return unify(p_non_nil, a_non_nil, type_params, subst);
            }
            if !is_assignable_to(arg, &substitute(pat, subst)) {
                return Err(mismatch());
            }
            Ok(())
        }
        _ => {
            if !is_assignable_to(arg, pat) {
                return Err(mismatch());
            }
            Ok(())
        }
    }
}

fn non_nil_member(members: &[Type]) -> Option<&Type> {
    if members.len() != 2 {
        return None;
    }
    if members[0].equals(&TYPE_NIL) {
        return Some(&members[1]);
    }
    if members[1].equals(&TYPE_NIL) {
        return Some(&members[0]);
    }
    None
}

/// TypeAlias defines a named or generic type alias via `(deftype ...)`.
#[derive(Clone, Debug)]
pub struct TypeAlias {
    /// The alias name.
    pub name: String,
    /// The generic type parameters.
    pub type_params: Vec<String>,
    /// The aliased type expression, parsed at each use.
    pub body: taxlispv1::Expression,
}

/// Checker holds the type environment, proto descriptor registry, and user
/// struct/type definitions across one or more modules.
#[derive(Clone, Debug)]
pub struct Checker {
    /// The protobuf descriptors types resolve against.
    pub files: Arc<DescriptorPool>,
    /// alias -> proto package prefix
    pub proto_aliases: HashMap<String, String>,
    /// The `defstruct` definitions, by name.
    pub structs: HashMap<String, Rc<StructDef>>,
    /// The `deftype` aliases, by name.
    pub aliases: HashMap<String, TypeAlias>,
    /// The types of global names: builtins and top-level definitions.
    pub globals: HashMap<String, Type>,
}

/// What Go's `Files.FindDescriptorByName` found.
enum Found {
    Message(String),
    Enum(String),
    Other,
}

fn tv(name: &str) -> Type {
    Type::TypeVar(name.to_string())
}

fn slice_of(t: Type) -> Type {
    Type::Slice(Box::new(t))
}

fn fn_of(type_params: &[&str], params: Vec<Type>, ret: Type) -> Type {
    Type::Fn(FnType {
        type_params: type_params.iter().map(|s| s.to_string()).collect(),
        params,
        ret: Box::new(ret),
    })
}

type Env = HashMap<String, Type>;
type TypeParams = HashSet<String>;

impl Checker {
    /// NewChecker creates a new typechecker backed by the given protobuf
    /// descriptors. Go's `NewChecker(nil)` is `Checker::new(global_files()?)`.
    pub fn new(files: Arc<DescriptorPool>) -> Checker {
        let mut c = Checker {
            files,
            proto_aliases: HashMap::new(),
            structs: HashMap::new(),
            aliases: HashMap::new(),
            globals: HashMap::new(),
        };
        c.register_builtins();
        c
    }

    fn register_builtins(&mut self) {
        let g = &mut self.globals;
        let t = || tv("T");
        g.insert("nil".into(), TYPE_NIL);
        g.insert("not".into(), fn_of(&[], vec![TYPE_BOOL], TYPE_BOOL));
        g.insert("nil?".into(), fn_of(&["T"], vec![t()], TYPE_BOOL));
        g.insert("empty-slice".into(), fn_of(&["T"], vec![], slice_of(t())));
        g.insert("len".into(), fn_of(&["T"], vec![slice_of(t())], TYPE_INT));
        g.insert(
            "empty?".into(),
            fn_of(&["T"], vec![slice_of(t())], TYPE_BOOL),
        );
        g.insert(
            "elt".into(),
            fn_of(&["T"], vec![slice_of(t()), TYPE_INT], t()),
        );
        g.insert(
            "subslice".into(),
            fn_of(
                &["T"],
                vec![slice_of(t()), TYPE_INT, TYPE_INT],
                slice_of(t()),
            ),
        );
        g.insert(
            "append".into(),
            fn_of(&["T"], vec![slice_of(t()), slice_of(t())], slice_of(t())),
        );
        g.insert(
            "map".into(),
            fn_of(
                &["T", "U"],
                vec![fn_of(&[], vec![t()], tv("U")), slice_of(t())],
                slice_of(tv("U")),
            ),
        );
        g.insert(
            "keep".into(),
            fn_of(
                &["T"],
                vec![fn_of(&[], vec![t()], TYPE_BOOL), slice_of(t())],
                slice_of(t()),
            ),
        );
        g.insert(
            "fold".into(),
            fn_of(
                &["T", "Acc"],
                vec![
                    fn_of(&[], vec![tv("Acc"), t()], tv("Acc")),
                    tv("Acc"),
                    slice_of(t()),
                ],
                tv("Acc"),
            ),
        );
        g.insert(
            "some".into(),
            fn_of(
                &["T"],
                vec![fn_of(&[], vec![t()], TYPE_BOOL), slice_of(t())],
                TYPE_BOOL,
            ),
        );
        g.insert(
            "all".into(),
            fn_of(
                &["T"],
                vec![fn_of(&[], vec![t()], TYPE_BOOL), slice_of(t())],
                TYPE_BOOL,
            ),
        );
        g.insert(
            "sum-money".into(),
            fn_of(&[], vec![slice_of(TYPE_MONEY)], TYPE_MONEY),
        );
        g.insert(
            "sum".into(),
            fn_of(
                &["T"],
                vec![fn_of(&[], vec![t()], TYPE_MONEY), slice_of(t())],
                TYPE_MONEY,
            ),
        );
        let table_kv = || Type::Table(Box::new(tv("K")), Box::new(tv("V")));
        g.insert(
            "lookup".into(),
            fn_of(
                &["K", "V"],
                vec![table_kv(), tv("K")],
                new_union(vec![tv("V"), TYPE_NIL]),
            ),
        );
        g.insert(
            "lookup-or".into(),
            fn_of(&["K", "V"], vec![table_kv(), tv("K"), tv("V")], tv("V")),
        );
        let unary = |p: Type, r: Type| fn_of(&[], vec![p], r);
        g.insert("round-dollar".into(), unary(TYPE_MONEY, TYPE_MONEY));
        g.insert(
            "join-words".into(),
            unary(slice_of(TYPE_STRING), TYPE_STRING),
        );
        g.insert("round-cents".into(), unary(TYPE_MONEY, TYPE_MONEY));
        g.insert("round-rate4".into(), unary(TYPE_FLOAT, TYPE_FLOAT));
        g.insert("floor-float".into(), unary(TYPE_FLOAT, TYPE_FLOAT));
        g.insert("ceil-float".into(), unary(TYPE_FLOAT, TYPE_FLOAT));
        g.insert("clamp-zero".into(), unary(TYPE_MONEY, TYPE_MONEY));
        g.insert("abs-money".into(), unary(TYPE_MONEY, TYPE_MONEY));
        g.insert("int-to-money".into(), unary(TYPE_INT, TYPE_MONEY));
        g.insert("int-to-float".into(), unary(TYPE_INT, TYPE_FLOAT));
        g.insert("money-to-float".into(), unary(TYPE_MONEY, TYPE_FLOAT));
        g.insert("float-to-money".into(), unary(TYPE_FLOAT, TYPE_MONEY));
        g.insert("date-year".into(), unary(TYPE_DATE, TYPE_INT));
        g.insert("date-month".into(), unary(TYPE_DATE, TYPE_INT));
        g.insert("date-day".into(), unary(TYPE_DATE, TYPE_INT));
    }

    /// CheckModule typechecks all top-level expressions in a ModuleFile.
    pub fn check_module(&mut self, mf: &taxlispv1::ModuleFile) -> Result<(), TypeError> {
        // Pass 1: register `use-proto`, `deftype`, `defstruct`, and function
        // signatures so functions in the same module can call each other
        // regardless of order.
        for expr in &mf.expressions {
            self.predeclare_top_level(expr)?;
        }
        // Pass 2: typecheck all top-level definitions and expressions.
        for expr in &mf.expressions {
            self.check_expr(expr, &Env::new(), &TypeParams::new())?;
        }
        Ok(())
    }

    fn predeclare_top_level(&mut self, expr: &taxlispv1::Expression) -> Result<(), TypeError> {
        let Some(list) = get_list(expr) else {
            return Ok(());
        };
        let elems = &list.expressions;
        if elems.is_empty() {
            return Ok(());
        }
        let head = symbol_name(&elems[0]);
        match head {
            "use-proto" => {
                if elems.len() != 3 {
                    return Err(err_at(
                        expr,
                        "use-proto requires 2 arguments: (use-proto 'pkg.path 'alias)",
                    ));
                }
                let pkg = extract_quoted_or_string(&elems[1])
                    .map_err(|e| err_at(&elems[1], &format!("use-proto package: {e}")))?;
                let alias = extract_quoted_or_string(&elems[2])
                    .map_err(|e| err_at(&elems[2], &format!("use-proto alias: {e}")))?;
                self.proto_aliases.insert(alias, pkg);
            }
            "deftype" => {
                if elems.len() != 3 {
                    return Err(err_at(
                        expr,
                        "deftype requires (deftype NameOrList TypeExpr)",
                    ));
                }
                let (name, tparams) = parse_def_header(&elems[1])?;
                self.aliases.insert(
                    name.clone(),
                    TypeAlias {
                        name,
                        type_params: tparams,
                        body: elems[2].clone(),
                    },
                );
            }
            "defstruct" => {
                if elems.len() < 3 {
                    return Err(err_at(
                        expr,
                        "defstruct requires (defstruct NameOrGeneric (field Type)...)",
                    ));
                }
                let (name, mut tparams) = parse_def_header(&elems[1])?;
                let mut tp_set: TypeParams = tparams.iter().cloned().collect();
                let mut field_start = 2;
                // Also support optional explicit type-param list in elems[2]
                // if elems[2] is a list of bare symbols (or empty list ()).
                if let Some(l2) = get_list(&elems[2])
                    && is_type_param_list(l2)
                {
                    for tp_expr in &l2.expressions {
                        let s = symbol_name(tp_expr);
                        if !tp_set.contains(s) {
                            tparams.push(s.to_string());
                            tp_set.insert(s.to_string());
                        }
                    }
                    field_start = 3;
                }
                let mut fields = Vec::new();
                for f_expr in &elems[field_start..] {
                    let fl = match get_list(f_expr) {
                        Some(fl) if fl.expressions.len() == 2 => fl,
                        _ => {
                            return Err(err_at(
                                f_expr,
                                "struct field must be (fieldName FieldType)",
                            ));
                        }
                    };
                    let fname = symbol_name(&fl.expressions[0]);
                    if fname.is_empty() {
                        return Err(err_at(
                            &fl.expressions[0],
                            "struct field name must be a symbol",
                        ));
                    }
                    let ftype = self.parse_type(&fl.expressions[1], &tp_set)?;
                    fields.push(StructField {
                        name: fname.to_string(),
                        typ: ftype,
                    });
                }
                let sd = Rc::new(StructDef {
                    name: name.clone(),
                    type_params: tparams.clone(),
                    fields,
                });
                self.structs.insert(name.clone(), sd.clone());
                let field_types = sd.fields.iter().map(|f| f.typ.clone()).collect();
                let type_vars = tparams.iter().map(|tp| tv(tp)).collect();
                self.globals.insert(
                    name,
                    Type::Fn(FnType {
                        type_params: tparams,
                        params: field_types,
                        ret: Box::new(Type::Struct(StructType {
                            def: sd,
                            type_args: type_vars,
                        })),
                    }),
                );
            }
            "def" => {
                if elems.len() < 5 {
                    return Err(err_at(
                        expr,
                        "def requires (def name ((arg Type)...) ReturnType body...)",
                    ));
                }
                let (name, tparams) = parse_def_header(&elems[1])?;
                let tp_set: TypeParams = tparams.iter().cloned().collect();
                let (_, param_types) = self.parse_param_bindings(&elems[2], &tp_set)?;
                let ret_type = self.parse_type(&elems[3], &tp_set)?;
                self.globals.insert(
                    name,
                    Type::Fn(FnType {
                        type_params: tparams,
                        params: param_types,
                        ret: Box::new(ret_type),
                    }),
                );
            }
            _ => {}
        }
        Ok(())
    }

    /// ParseType parses a type S-expression into a Type.
    pub fn parse_type(
        &self,
        expr: &taxlispv1::Expression,
        type_params: &TypeParams,
    ) -> Result<Type, TypeError> {
        if let Some(a) = get_atom(expr) {
            let Some(Primitive::Symbol(sym)) = &a.primitive else {
                return Err(err_at(expr, "expected type symbol, got literal"));
            };
            if sym.package.is_empty() {
                let name = sym.name.as_str();
                if type_params.contains(name) {
                    return Ok(tv(name));
                }
                if let Some(k) = PrimitiveKind::from_name(name) {
                    return Ok(Type::Primitive(k));
                }
                if let Some(alias) = self.aliases.get(name)
                    && alias.type_params.is_empty()
                {
                    return self.parse_type(&alias.body, type_params);
                }
                if let Some(sdef) = self.structs.get(name) {
                    if !sdef.type_params.is_empty() {
                        return Err(err_at(
                            expr,
                            &format!(
                                "generic struct {} requires {} type arguments",
                                name,
                                sdef.type_params.len()
                            ),
                        ));
                    }
                    return Ok(Type::Struct(StructType {
                        def: sdef.clone(),
                        type_args: Vec::new(),
                    }));
                }
            }
            return self.resolve_proto_type(sym, expr);
        }

        let elems = match get_list(expr) {
            Some(list) if !list.expressions.is_empty() => &list.expressions,
            _ => return Err(err_at(expr, "empty type expression")),
        };
        let head = symbol_name(&elems[0]);
        match head {
            "Slice" => {
                if elems.len() != 2 {
                    return Err(err_at(expr, "(Slice T) requires exactly 1 type argument"));
                }
                let elem_type = self.parse_type(&elems[1], type_params)?;
                Ok(slice_of(elem_type))
            }
            "Option" => {
                if elems.len() != 2 {
                    return Err(err_at(expr, "(Option T) requires exactly 1 type argument"));
                }
                let elem_type = self.parse_type(&elems[1], type_params)?;
                Ok(new_union(vec![elem_type, TYPE_NIL]))
            }
            "Table" => {
                if elems.len() != 3 {
                    return Err(err_at(expr, "(Table K V) requires 2 type arguments"));
                }
                let kt = self.parse_type(&elems[1], type_params)?;
                let vt = self.parse_type(&elems[2], type_params)?;
                Ok(Type::Table(Box::new(kt), Box::new(vt)))
            }
            "Union" => {
                if elems.len() < 2 {
                    return Err(err_at(
                        expr,
                        "(Union T1 T2 ...) requires at least 1 member type",
                    ));
                }
                let mut members = Vec::new();
                for m in &elems[1..] {
                    members.push(self.parse_type(m, type_params)?);
                }
                Ok(new_union(members))
            }
            "Fn" => {
                if elems.len() != 3 {
                    return Err(err_at(
                        expr,
                        "(Fn (P1 P2 ...) R) requires parameter list and return type",
                    ));
                }
                let Some(p_list) = get_list(&elems[1]) else {
                    return Err(err_at(&elems[1], "Fn parameter types must be a list"));
                };
                let mut params = Vec::new();
                for pe in &p_list.expressions {
                    params.push(self.parse_type(pe, type_params)?);
                }
                let ret = self.parse_type(&elems[2], type_params)?;
                Ok(fn_of(&[], params, ret))
            }
            _ => {
                if let Some(alias) = self.aliases.get(head) {
                    if elems.len() - 1 != alias.type_params.len() {
                        return Err(err_at(
                            expr,
                            &format!(
                                "type alias {} expects {} type arguments, got {}",
                                head,
                                alias.type_params.len(),
                                elems.len() - 1
                            ),
                        ));
                    }
                    let mut tp_set = TypeParams::new();
                    let mut subst = HashMap::new();
                    for (i, tp) in alias.type_params.iter().enumerate() {
                        tp_set.insert(tp.clone());
                        let arg_t = self.parse_type(&elems[i + 1], type_params)?;
                        subst.insert(tp.clone(), arg_t);
                    }
                    let body_t = self.parse_type(&alias.body, &tp_set)?;
                    return Ok(substitute(&body_t, &subst));
                }
                if let Some(sdef) = self.structs.get(head) {
                    if elems.len() - 1 != sdef.type_params.len() {
                        return Err(err_at(
                            expr,
                            &format!(
                                "struct {} expects {} type arguments, got {}",
                                head,
                                sdef.type_params.len(),
                                elems.len() - 1
                            ),
                        ));
                    }
                    let mut args = Vec::new();
                    for ae in &elems[1..] {
                        args.push(self.parse_type(ae, type_params)?);
                    }
                    return Ok(Type::Struct(StructType {
                        def: sdef.clone(),
                        type_args: args,
                    }));
                }
                Err(err_at(
                    expr,
                    &format!("unknown type constructor {}", quote(head)),
                ))
            }
        }
    }

    /// Go's `Files.FindDescriptorByName`: any declaration, including fields,
    /// oneofs, enum values, extensions, services and methods.
    fn find_descriptor_by_name(&self, name: &str) -> Option<Found> {
        if let Some(md) = self.files.message_by_name(name) {
            return Some(Found::Message(md.full_name().to_string()));
        }
        if let Some(ed) = self.files.enum_by_name(name) {
            return Some(Found::Enum(ed.full_name().to_string()));
        }
        if self.files.file_containing_symbol(name).is_some() {
            return Some(Found::Other);
        }
        let (parent, leaf) = name.rsplit_once('.')?;
        let md = self.files.message_by_name(parent)?;
        if md.fields().iter().any(|f| f.name() == leaf)
            || md.oneofs().iter().any(|o| o.name() == leaf)
        {
            return Some(Found::Other);
        }
        None
    }

    fn resolve_proto_type(
        &self,
        sym: &taxlispv1::Symbol,
        expr: &taxlispv1::Expression,
    ) -> Result<Type, TypeError> {
        let mut full_name = sym.name.clone();
        if !sym.package.is_empty() {
            if let Some(expanded) = self.proto_aliases.get(&sym.package) {
                full_name = format!("{}.{}", expanded, sym.name);
            } else {
                full_name = format!("{}.{}", sym.package, sym.name);
            }
        }
        let mut desc = self.find_descriptor_by_name(&full_name);
        if desc.is_none() && !full_name.starts_with("nexteggapp.") {
            // Also try with "nexteggapp." prefix if omitted.
            desc = self.find_descriptor_by_name(&format!("nexteggapp.{full_name}"));
        }
        match desc {
            None => Err(err_at(
                expr,
                &format!(
                    "unknown type {} (resolved as {})",
                    format_sym(sym),
                    full_name
                ),
            )),
            Some(Found::Message(n)) => Ok(Type::ProtoMessage(n)),
            Some(Found::Enum(n)) => Ok(Type::ProtoEnum(n)),
            Some(Found::Other) => Err(err_at(
                expr,
                &format!("{full_name} is not a protobuf message or enum"),
            )),
        }
    }

    /// CheckExpr typechecks an expression in lexical environment `env` and
    /// active generic type parameters `type_params`.
    pub fn check_expr(
        &mut self,
        expr: &taxlispv1::Expression,
        env: &Env,
        type_params: &TypeParams,
    ) -> Result<Type, TypeError> {
        if let Some(a) = get_atom(expr) {
            match &a.primitive {
                Some(Primitive::IntValue(_)) => return Ok(TYPE_INT),
                Some(Primitive::BoolValue(_)) => return Ok(TYPE_BOOL),
                Some(Primitive::StringValue(_)) => return Ok(TYPE_STRING),
                Some(Primitive::Money(_)) => return Ok(TYPE_MONEY),
                Some(Primitive::FloatValue(_)) => return Ok(TYPE_FLOAT),
                Some(Primitive::Date(_)) => return Ok(TYPE_DATE),
                Some(Primitive::Keyword(_)) => return Ok(TYPE_SYMBOL),
                Some(Primitive::Symbol(sym)) => {
                    if sym.package.is_empty() {
                        if let Some(t) = env.get(&sym.name) {
                            return Ok(t.clone());
                        }
                        if let Some(t) = self.globals.get(&sym.name) {
                            return Ok(t.clone());
                        }
                        return Err(err_at(
                            expr,
                            &format!("unbound symbol {}", quote(&sym.name)),
                        ));
                    }
                    // Package-qualified enum value constant, e.g.
                    // fed/FILING_STATUS_SINGLE
                    return self.resolve_proto_enum_value(sym, expr);
                }
                None => {}
            }
        }

        let elems = match get_list(expr) {
            Some(list) if !list.expressions.is_empty() => &list.expressions,
            _ => return Err(err_at(expr, "cannot evaluate empty list ()")),
        };
        let head = symbol_name(&elems[0]);

        match head {
            "use-proto" | "deftype" | "defstruct" => Ok(TYPE_NIL),

            "quote" => self.check_quote(expr, elems),

            "=" => self.check_constant(expr, elems, env, type_params),

            "def" => self.check_def(expr, elems, env, type_params),

            "fn" => self.check_fn_literal(expr, elems, env, type_params),

            "let" => self.check_let(expr, elems, env, type_params),

            "with" | "withs" => self.check_with(expr, elems, env, type_params, head),

            "if" => self.check_if(expr, elems, env, type_params),

            "cond" => self.check_cond(expr, elems, env, type_params),

            "typecase" => self.check_typecase(expr, elems, env, type_params),

            "isa" => self.check_isa(expr, elems, env, type_params),

            "make-proto" | "proto" => self.check_make_proto(expr, elems, env, type_params),

            "make-struct" => self.check_make_struct(expr, elems, env, type_params),

            "proto-with" => self.check_proto_with(expr, elems, env, type_params),

            "." => self.check_field_access(expr, elems, env, type_params),

            "has?" => self.check_has(expr, elems, env, type_params),

            "which-oneof" => self.check_which_oneof(expr, elems, env, type_params),

            "slice" => self.check_slice(expr, elems, env, type_params),

            "table" => self.check_table(expr, elems, env, type_params),

            "inst" => self.check_inst(expr, elems, env, type_params),

            "+" | "-" | "*" | "/" | "min" | "max" => {
                self.check_arithmetic(expr, head, &elems[1..], env, type_params)
            }

            "<" | "<=" | ">" | ">=" => self.check_comparison(expr, elems, env, type_params, head),

            "is" | "!=" => self.check_equality(expr, elems, env, type_params, head),

            "and" | "or" => self.check_logical(expr, elems, env, type_params, head),

            _ => self.check_call(expr, elems, env, type_params),
        }
    }

    /// Checks a `quote` form.
    fn check_quote(
        &mut self,
        expr: &taxlispv1::Expression,
        elems: &[taxlispv1::Expression],
    ) -> Result<Type, TypeError> {
        if elems.len() != 2 {
            return Err(err_at(expr, "quote requires 1 argument"));
        }
        if let Some(a) = get_atom(&elems[1])
            && let Some(Primitive::Symbol(sym)) = &a.primitive
            && !sym.package.is_empty()
            && let Ok(enum_t) = self.resolve_proto_enum_value(sym, &elems[1])
        {
            return Ok(enum_t);
        }
        Ok(TYPE_SYMBOL)
    }

    /// Checks a `=` form.
    fn check_constant(
        &mut self,
        expr: &taxlispv1::Expression,
        elems: &[taxlispv1::Expression],
        env: &Env,
        type_params: &TypeParams,
    ) -> Result<Type, TypeError> {
        // Top-level constant definition: (= name Type expr)
        if elems.len() != 4 {
            return Err(err_at(
                expr,
                "= requires explicit type annotation: (= name Type expr)",
            ));
        }
        let name = symbol_name(&elems[1]);
        if name.is_empty() {
            return Err(err_at(&elems[1], "= target must be a symbol"));
        }
        let decl_type = self.parse_type(&elems[2], type_params)?;
        let val_type = self.check_expr(&elems[3], env, type_params)?;
        if !is_assignable_to(&val_type, &decl_type) {
            return Err(err_at(
                &elems[3],
                &format!("constant {name}: value has type {val_type}, want {decl_type}"),
            ));
        }
        self.globals.insert(name.to_string(), decl_type.clone());
        Ok(decl_type)
    }

    /// Checks a `def` form.
    fn check_def(
        &mut self,
        expr: &taxlispv1::Expression,
        elems: &[taxlispv1::Expression],
        env: &Env,
        type_params: &TypeParams,
    ) -> Result<Type, TypeError> {
        if elems.len() < 5 {
            return Err(err_at(
                expr,
                "def requires (def name ((arg Type)...) ReturnType body...)",
            ));
        }
        let (name, tparams) = parse_def_header(&elems[1])?;
        let mut tp_set = type_params.clone();
        for tp in &tparams {
            tp_set.insert(tp.clone());
        }
        let (param_names, param_types) = self.parse_param_bindings(&elems[2], &tp_set)?;
        let ret_type = self.parse_type(&elems[3], &tp_set)?;
        let mut fn_env = env.clone();
        for (pn, pt) in param_names.iter().zip(&param_types) {
            fn_env.insert(pn.clone(), pt.clone());
        }
        let mut body_type: Option<Type> = None;
        let mut i = 4;
        while i < elems.len() {
            // Skip keyword metadata pairs like :cite ("IRC § 63")
            if is_keyword_atom(&elems[i]) {
                i += 2;
                continue;
            }
            body_type = Some(self.check_expr(&elems[i], &fn_env, &tp_set)?);
            i += 1;
        }
        match &body_type {
            Some(bt) if is_assignable_to(bt, &ret_type) => {}
            _ => {
                let shown = body_type
                    .as_ref()
                    .map_or_else(|| "<nil>".to_string(), Type::to_string);
                return Err(err_at(
                    expr,
                    &format!("function {name} body has type {shown}, want {ret_type}"),
                ));
            }
        }
        let fn_t = Type::Fn(FnType {
            type_params: tparams,
            params: param_types,
            ret: Box::new(ret_type),
        });
        self.globals.insert(name, fn_t.clone());
        Ok(fn_t)
    }

    /// Checks a `fn` form.
    fn check_fn_literal(
        &mut self,
        expr: &taxlispv1::Expression,
        elems: &[taxlispv1::Expression],
        env: &Env,
        type_params: &TypeParams,
    ) -> Result<Type, TypeError> {
        if elems.len() != 4 {
            return Err(err_at(
                expr,
                "fn requires (fn ((arg Type)...) ReturnType body)",
            ));
        }
        let (param_names, param_types) = self.parse_param_bindings(&elems[1], type_params)?;
        let ret_type = self.parse_type(&elems[2], type_params)?;
        let mut fn_env = env.clone();
        for (pn, pt) in param_names.iter().zip(&param_types) {
            fn_env.insert(pn.clone(), pt.clone());
        }
        let body_type = self.check_expr(&elems[3], &fn_env, type_params)?;
        if !is_assignable_to(&body_type, &ret_type) {
            return Err(err_at(
                &elems[3],
                &format!("fn body has type {body_type}, want {ret_type}"),
            ));
        }
        Ok(fn_of(&[], param_types, ret_type))
    }

    /// Checks a `let` form.
    fn check_let(
        &mut self,
        expr: &taxlispv1::Expression,
        elems: &[taxlispv1::Expression],
        env: &Env,
        type_params: &TypeParams,
    ) -> Result<Type, TypeError> {
        // Supports (let var val body) or (let var Type val body)
        if elems.len() != 4 && elems.len() != 5 {
            return Err(err_at(
                expr,
                "let requires (let var val body) or (let var Type val body)",
            ));
        }
        let vname = symbol_name(&elems[1]);
        if vname.is_empty() {
            return Err(err_at(&elems[1], "let binding variable must be a symbol"));
        }
        let (val_expr, body_expr, decl_type) = if elems.len() == 5 {
            let dt = self.parse_type(&elems[2], type_params)?;
            (&elems[3], &elems[4], Some(dt))
        } else {
            (&elems[2], &elems[3], None)
        };
        let mut vt = self.check_expr(val_expr, env, type_params)?;
        if let Some(dt) = decl_type {
            if !is_assignable_to(&vt, &dt) {
                return Err(err_at(
                    val_expr,
                    &format!("let {vname}: value has type {vt}, want {dt}"),
                ));
            }
            vt = dt;
        }
        let mut next_env = env.clone();
        next_env.insert(vname.to_string(), vt);
        self.check_expr(body_expr, &next_env, type_params)
    }

    /// Checks a `with`, `withs` form.
    fn check_with(
        &mut self,
        expr: &taxlispv1::Expression,
        elems: &[taxlispv1::Expression],
        env: &Env,
        type_params: &TypeParams,
        head: &str,
    ) -> Result<Type, TypeError> {
        // Arc multi-binding: (withs (k1 v1 k2 v2 ...) body)
        if elems.len() != 3 {
            return Err(err_at(
                expr,
                &format!("{head} requires ({head} (var1 val1 ...) body)"),
            ));
        }
        let b_exprs = match get_list(&elems[1]) {
            Some(bl) if bl.expressions.len().is_multiple_of(2) => &bl.expressions,
            _ => {
                return Err(err_at(
                    &elems[1],
                    &format!("{head} bindings must be an even-length list of (var val ...) pairs"),
                ));
            }
        };
        let mut next_env = env.clone();
        let sequential = head == "withs";
        let mut i = 0;
        while i < b_exprs.len() {
            let vname = symbol_name(&b_exprs[i]);
            if vname.is_empty() {
                return Err(err_at(
                    &b_exprs[i],
                    &format!("{head} variable must be a symbol"),
                ));
            }
            let vt = if sequential {
                self.check_expr(&b_exprs[i + 1], &next_env, type_params)?
            } else {
                self.check_expr(&b_exprs[i + 1], env, type_params)?
            };
            next_env.insert(vname.to_string(), vt);
            i += 2;
        }
        self.check_expr(&elems[2], &next_env, type_params)
    }

    /// Checks a `if` form.
    fn check_if(
        &mut self,
        expr: &taxlispv1::Expression,
        elems: &[taxlispv1::Expression],
        env: &Env,
        type_params: &TypeParams,
    ) -> Result<Type, TypeError> {
        // Arc multi-branch if: (if c1 t1 c2 t2 ... elseExpr)
        if elems.len() < 4 || !elems.len().is_multiple_of(2) {
            return Err(err_at(
                expr,
                "if requires condition/then pairs and a final else expression: (if c1 t1 ... else)",
            ));
        }
        let mut branch_types = Vec::new();
        let mut cur_env = env.clone();
        let mut i = 1;
        while i < elems.len() - 1 {
            let cond_expr = &elems[i];
            let then_expr = &elems[i + 1];
            let ct = self.check_expr(cond_expr, &cur_env, type_params)?;
            if !ct.equals(&TYPE_BOOL) {
                return Err(err_at(
                    cond_expr,
                    &format!("if condition must be Bool, got {ct}"),
                ));
            }
            let (then_env, else_env) = self.narrow_env(cond_expr, &cur_env, type_params);
            let tt = self.check_expr(then_expr, &then_env, type_params)?;
            branch_types.push(tt);
            cur_env = else_env;
            i += 2;
        }
        let else_type = self.check_expr(&elems[elems.len() - 1], &cur_env, type_params)?;
        branch_types.push(else_type);
        Ok(new_union(branch_types))
    }

    /// Checks a `cond` form.
    fn check_cond(
        &mut self,
        expr: &taxlispv1::Expression,
        elems: &[taxlispv1::Expression],
        env: &Env,
        type_params: &TypeParams,
    ) -> Result<Type, TypeError> {
        // (cond (c1 t1) (c2 t2) ... (else elseExpr))
        if elems.len() < 2 {
            return Err(err_at(
                expr,
                "cond requires at least one clause: (cond (c1 t1) ... (else elseExpr))",
            ));
        }
        let mut branch_types = Vec::new();
        let mut cur_env = env.clone();
        let mut has_else = false;
        for i in 1..elems.len() {
            let pair = match get_list(&elems[i]) {
                Some(cl) if cl.expressions.len() == 2 => &cl.expressions,
                _ => {
                    return Err(err_at(
                        &elems[i],
                        "cond clause must be a 2-element list (condition body)",
                    ));
                }
            };
            let cond_expr = &pair[0];
            let then_expr = &pair[1];
            if symbol_name(cond_expr) == "else" || is_keyword_named(cond_expr, "else") {
                if i != elems.len() - 1 {
                    return Err(err_at(
                        cond_expr,
                        "else clause must be the final clause in cond",
                    ));
                }
                has_else = true;
                let et = self.check_expr(then_expr, &cur_env, type_params)?;
                branch_types.push(et);
                break;
            }
            let ct = self.check_expr(cond_expr, &cur_env, type_params)?;
            if !ct.equals(&TYPE_BOOL) {
                return Err(err_at(
                    cond_expr,
                    &format!("cond condition must be Bool, got {ct}"),
                ));
            }
            let (then_env, else_env) = self.narrow_env(cond_expr, &cur_env, type_params);
            let tt = self.check_expr(then_expr, &then_env, type_params)?;
            branch_types.push(tt);
            cur_env = else_env;
        }
        if !has_else {
            return Err(err_at(expr, "cond requires a final (else ...) clause"));
        }
        Ok(new_union(branch_types))
    }

    /// Checks a `isa` form.
    fn check_isa(
        &mut self,
        expr: &taxlispv1::Expression,
        elems: &[taxlispv1::Expression],
        env: &Env,
        type_params: &TypeParams,
    ) -> Result<Type, TypeError> {
        if elems.len() != 3 {
            return Err(err_at(expr, "(isa expr Type) requires 2 arguments"));
        }
        self.check_expr(&elems[1], env, type_params)?;
        self.parse_type(&elems[2], type_params)?;
        Ok(TYPE_BOOL)
    }

    /// Checks a `has?` form.
    fn check_has(
        &mut self,
        expr: &taxlispv1::Expression,
        elems: &[taxlispv1::Expression],
        env: &Env,
        type_params: &TypeParams,
    ) -> Result<Type, TypeError> {
        if elems.len() != 3 {
            return Err(err_at(expr, "(has? msg 'field) requires 2 arguments"));
        }
        self.check_field_access(expr, elems, env, type_params)?;
        Ok(TYPE_BOOL)
    }

    /// Checks a `which-oneof` form.
    fn check_which_oneof(
        &mut self,
        expr: &taxlispv1::Expression,
        elems: &[taxlispv1::Expression],
        env: &Env,
        type_params: &TypeParams,
    ) -> Result<Type, TypeError> {
        if elems.len() != 3 {
            return Err(err_at(
                expr,
                "(which-oneof msg 'oneof_name) requires 2 arguments",
            ));
        }
        let target_t = self.check_expr(&elems[1], env, type_params)?;
        let Type::ProtoMessage(msg_name) = &target_t else {
            return Err(err_at(
                &elems[1],
                &format!("which-oneof target must be a protobuf message, got {target_t}"),
            ));
        };
        let oneof_name = extract_quoted_or_string(&elems[2]).map_err(|e| {
            err_at(
                &elems[2],
                &format!("which-oneof expects quoted oneof name: {e}"),
            )
        })?;
        let has = self
            .files
            .message_by_name(msg_name)
            .is_some_and(|md| md.oneofs().iter().any(|o| o.name() == oneof_name));
        if !has {
            return Err(err_at(
                &elems[2],
                &format!("message {} has no oneof {}", msg_name, quote(&oneof_name)),
            ));
        }
        Ok(new_union(vec![TYPE_SYMBOL, TYPE_NIL]))
    }

    /// Checks a `slice` form.
    fn check_slice(
        &mut self,
        expr: &taxlispv1::Expression,
        elems: &[taxlispv1::Expression],
        env: &Env,
        type_params: &TypeParams,
    ) -> Result<Type, TypeError> {
        if elems.len() == 1 {
            return Err(err_at(expr, "empty (slice) requires (empty-slice Type)"));
        }
        let mut elem_types = Vec::new();
        for el in &elems[1..] {
            elem_types.push(self.check_expr(el, env, type_params)?);
        }
        Ok(slice_of(new_union(elem_types)))
    }

    /// Checks a `table` form.
    fn check_table(
        &mut self,
        expr: &taxlispv1::Expression,
        elems: &[taxlispv1::Expression],
        env: &Env,
        type_params: &TypeParams,
    ) -> Result<Type, TypeError> {
        if elems.len() < 3 || !(elems.len() - 1).is_multiple_of(2) {
            return Err(err_at(
                expr,
                "(table k1 v1 k2 v2 ...) requires at least one key/value pair",
            ));
        }
        let mut k_types = Vec::new();
        let mut v_types = Vec::new();
        let mut i = 1;
        while i < elems.len() {
            k_types.push(self.check_expr(&elems[i], env, type_params)?);
            v_types.push(self.check_expr(&elems[i + 1], env, type_params)?);
            i += 2;
        }
        Ok(Type::Table(
            Box::new(new_union(k_types)),
            Box::new(new_union(v_types)),
        ))
    }

    /// Checks a `inst` form.
    fn check_inst(
        &mut self,
        expr: &taxlispv1::Expression,
        elems: &[taxlispv1::Expression],
        env: &Env,
        type_params: &TypeParams,
    ) -> Result<Type, TypeError> {
        // Explicit generic instantiation: (inst fnOrGlobal Type1 Type2 ...)
        if elems.len() < 2 {
            return Err(err_at(
                expr,
                "(inst fn Type1 ...) requires a generic function and type arguments",
            ));
        }
        let base_t = self.check_expr(&elems[1], env, type_params)?;
        let fn_t = match &base_t {
            Type::Fn(f) if f.type_params.len() == elems.len() - 2 => f,
            _ => {
                // Go dereferences a nil *FnType here (and panics)
                // when the target is not a function; this reports 0.
                let n = match &base_t {
                    Type::Fn(f) => f.type_params.len(),
                    _ => 0,
                };
                return Err(err_at(
                    expr,
                    &format!(
                        "inst target {} expects {} type arguments, got {}",
                        base_t,
                        n,
                        elems.len() - 2
                    ),
                ));
            }
        };
        let mut subst = HashMap::new();
        for (i, tp) in fn_t.type_params.iter().enumerate() {
            let at = self.parse_type(&elems[i + 2], type_params)?;
            subst.insert(tp.clone(), at);
        }
        let inst_params = fn_t.params.iter().map(|p| substitute(p, &subst)).collect();
        Ok(fn_of(&[], inst_params, substitute(&fn_t.ret, &subst)))
    }

    /// Checks a `<`, `<=`, `>`, `>=` form.
    fn check_comparison(
        &mut self,
        expr: &taxlispv1::Expression,
        elems: &[taxlispv1::Expression],
        env: &Env,
        type_params: &TypeParams,
        head: &str,
    ) -> Result<Type, TypeError> {
        if elems.len() != 3 {
            return Err(err_at(expr, &format!("{head} requires 2 arguments")));
        }
        let lt = self.check_expr(&elems[1], env, type_params)?;
        let rt = self.check_expr(&elems[2], env, type_params)?;
        if !lt.equals(&rt)
            || (!lt.equals(&TYPE_MONEY)
                && !lt.equals(&TYPE_FLOAT)
                && !lt.equals(&TYPE_INT)
                && !lt.equals(&TYPE_DATE))
        {
            return Err(err_at(
                expr,
                &format!(
                    "{head} requires two matching Money, Float, Int, or Date arguments, got {lt} and {rt}"
                ),
            ));
        }
        Ok(TYPE_BOOL)
    }

    /// Checks a `is`, `!=` form.
    fn check_equality(
        &mut self,
        expr: &taxlispv1::Expression,
        elems: &[taxlispv1::Expression],
        env: &Env,
        type_params: &TypeParams,
        head: &str,
    ) -> Result<Type, TypeError> {
        if elems.len() != 3 {
            return Err(err_at(expr, &format!("{head} requires 2 arguments")));
        }
        self.check_expr(&elems[1], env, type_params)?;
        self.check_expr(&elems[2], env, type_params)?;
        Ok(TYPE_BOOL)
    }

    /// Checks a `and`, `or` form.
    fn check_logical(
        &mut self,
        expr: &taxlispv1::Expression,
        elems: &[taxlispv1::Expression],
        env: &Env,
        type_params: &TypeParams,
        head: &str,
    ) -> Result<Type, TypeError> {
        if elems.len() < 2 {
            return Err(err_at(
                expr,
                &format!("{head} requires at least 1 Bool argument"),
            ));
        }
        for arg in &elems[1..] {
            let at = self.check_expr(arg, env, type_params)?;
            if !at.equals(&TYPE_BOOL) {
                return Err(err_at(
                    arg,
                    &format!("{head} argument must be Bool, got {at}"),
                ));
            }
        }
        Ok(TYPE_BOOL)
    }

    fn check_arithmetic(
        &mut self,
        expr: &taxlispv1::Expression,
        op: &str,
        args: &[taxlispv1::Expression],
        env: &Env,
        type_params: &TypeParams,
    ) -> Result<Type, TypeError> {
        if args.len() < 2 {
            return Err(err_at(expr, &format!("{op} requires at least 2 arguments")));
        }
        let mut arg_types = Vec::with_capacity(args.len());
        for a in args {
            arg_types.push(self.check_expr(a, env, type_params)?);
        }
        let first = &arg_types[0];
        match op {
            "+" | "-" | "min" | "max" => {
                if !first.equals(&TYPE_MONEY)
                    && !first.equals(&TYPE_FLOAT)
                    && !first.equals(&TYPE_INT)
                {
                    return Err(err_at(expr, &format!("{op} not supported on {first}")));
                }
                for at in &arg_types[1..] {
                    if !at.equals(first) {
                        return Err(err_at(
                            expr,
                            &format!("{op} requires all arguments to have type {first}, got {at}"),
                        ));
                    }
                }
                Ok(first.clone())
            }
            "*" => {
                if args.len() != 2 {
                    return Err(err_at(expr, "* requires 2 arguments"));
                }
                let (a, b) = (&arg_types[0], &arg_types[1]);
                let is = |t: &Type, k: &Type| t.equals(k);
                if (is(a, &TYPE_MONEY) && (is(b, &TYPE_FLOAT) || is(b, &TYPE_INT)))
                    || ((is(a, &TYPE_FLOAT) || is(a, &TYPE_INT)) && is(b, &TYPE_MONEY))
                {
                    Ok(TYPE_MONEY)
                } else if (is(a, &TYPE_FLOAT) && (is(b, &TYPE_FLOAT) || is(b, &TYPE_INT)))
                    || (is(a, &TYPE_INT) && is(b, &TYPE_FLOAT))
                {
                    Ok(TYPE_FLOAT)
                } else if is(a, &TYPE_INT) && is(b, &TYPE_INT) {
                    Ok(TYPE_INT)
                } else {
                    Err(err_at(expr, &format!("* cannot multiply {a} and {b}")))
                }
            }
            "/" => {
                if args.len() != 2 {
                    return Err(err_at(expr, "/ requires 2 arguments"));
                }
                let (a, b) = (&arg_types[0], &arg_types[1]);
                let is = |t: &Type, k: &Type| t.equals(k);
                if is(a, &TYPE_MONEY) && is(b, &TYPE_MONEY) {
                    Ok(TYPE_FLOAT)
                } else if is(a, &TYPE_MONEY) && (is(b, &TYPE_FLOAT) || is(b, &TYPE_INT)) {
                    Ok(TYPE_MONEY)
                } else if is(a, &TYPE_FLOAT) && (is(b, &TYPE_FLOAT) || is(b, &TYPE_INT)) {
                    Ok(TYPE_FLOAT)
                } else if is(a, &TYPE_INT) && is(b, &TYPE_INT) {
                    Ok(TYPE_INT)
                } else {
                    Err(err_at(expr, &format!("/ cannot divide {a} by {b}")))
                }
            }
            _ => Err(err_at(expr, &format!("unsupported operator {op}"))),
        }
    }

    fn check_call(
        &mut self,
        expr: &taxlispv1::Expression,
        elems: &[taxlispv1::Expression],
        env: &Env,
        type_params: &TypeParams,
    ) -> Result<Type, TypeError> {
        let callee_t = self.check_expr(&elems[0], env, type_params)?;
        let Type::Fn(fn_t) = callee_t else {
            return Err(err_at(
                &elems[0],
                &format!("cannot call non-function of type {callee_t}"),
            ));
        };
        let args = &elems[1..];
        if args.len() != fn_t.params.len() {
            return Err(err_at(
                expr,
                &format!(
                    "function expects {} arguments, got {}",
                    fn_t.params.len(),
                    args.len()
                ),
            ));
        }
        let mut arg_types = Vec::with_capacity(args.len());
        for a in args {
            arg_types.push(self.check_expr(a, env, type_params)?);
        }
        if fn_t.type_params.is_empty() {
            for (i, want) in fn_t.params.iter().enumerate() {
                if !is_assignable_to(&arg_types[i], want) {
                    return Err(err_at(
                        &args[i],
                        &format!(
                            "argument {} has type {}, want {}",
                            i + 1,
                            arg_types[i],
                            want
                        ),
                    ));
                }
            }
            return Ok(*fn_t.ret);
        }
        let tp_set: TypeParams = fn_t.type_params.iter().cloned().collect();
        let mut subst = HashMap::new();
        for (i, pat) in fn_t.params.iter().enumerate() {
            if let Err(e) = unify(pat, &arg_types[i], &tp_set, &mut subst) {
                return Err(err_at(&args[i], &format!("argument {}: {}", i + 1, e)));
            }
        }
        for tp in &fn_t.type_params {
            if !subst.contains_key(tp) {
                return Err(err_at(
                    expr,
                    &format!("cannot infer type parameter {tp}; use (inst fn Type...)"),
                ));
            }
        }
        Ok(substitute(&fn_t.ret, &subst))
    }

    fn check_typecase(
        &mut self,
        expr: &taxlispv1::Expression,
        elems: &[taxlispv1::Expression],
        env: &Env,
        type_params: &TypeParams,
    ) -> Result<Type, TypeError> {
        // (typecase (var targetExpr) Type1 body1 Type2 body2 ...)
        if elems.len() < 4 || !elems.len().is_multiple_of(2) {
            return Err(err_at(
                expr,
                "typecase requires (typecase (var expr) Type1 body1 ...)",
            ));
        }
        let binding = match get_list(&elems[1]) {
            Some(bl) if bl.expressions.len() == 2 => &bl.expressions,
            _ => {
                return Err(err_at(
                    &elems[1],
                    "typecase binding must be (var targetExpr)",
                ));
            }
        };
        let var_name = symbol_name(&binding[0]);
        if var_name.is_empty() {
            return Err(err_at(&binding[0], "typecase variable must be a symbol"));
        }
        let target_t = self.check_expr(&binding[1], env, type_params)?;
        let union_members: Vec<Type> = match &target_t {
            Type::Union(members) => members.clone(),
            _ => vec![target_t.clone()],
        };
        let mut covered = vec![false; union_members.len()];
        let mut has_else = false;
        let mut branch_types = Vec::new();

        let mut i = 2;
        while i < elems.len() {
            let pat_expr = &elems[i];
            let body_expr = &elems[i + 1];
            i += 2;
            if is_keyword_named(pat_expr, "else") {
                has_else = true;
                let bt = self.check_expr(body_expr, env, type_params)?;
                branch_types.push(bt);
                continue;
            }
            let case_t = self.parse_type(pat_expr, type_params)?;
            let mut matched_any = false;
            for (idx, m) in union_members.iter().enumerate() {
                if is_assignable_to(m, &case_t) {
                    covered[idx] = true;
                    matched_any = true;
                }
            }
            if !matched_any {
                return Err(err_at(
                    pat_expr,
                    &format!("typecase branch {case_t} is unreachable for target type {target_t}"),
                ));
            }
            let mut branch_env = env.clone();
            branch_env.insert(var_name.to_string(), case_t);
            let bt = self.check_expr(body_expr, &branch_env, type_params)?;
            branch_types.push(bt);
        }
        if !has_else {
            for (idx, cov) in covered.iter().enumerate() {
                if !cov {
                    return Err(err_at(
                        expr,
                        &format!(
                            "non-exhaustive typecase on {}: missing branch for {}",
                            target_t, union_members[idx]
                        ),
                    ));
                }
            }
        }
        Ok(new_union(branch_types))
    }

    fn narrow_env(
        &self,
        cond: &taxlispv1::Expression,
        env: &Env,
        type_params: &TypeParams,
    ) -> (Env, Env) {
        let mut then_env = env.clone();
        let mut else_env = env.clone();
        let elems = match get_list(cond) {
            Some(list) if !list.expressions.is_empty() => &list.expressions,
            _ => return (then_env, else_env),
        };
        let head = symbol_name(&elems[0]);
        match head {
            "nil?" => {
                if elems.len() == 2 {
                    let vname = symbol_name(&elems[1]);
                    if !vname.is_empty()
                        && let Some(orig_t) = env.get(vname)
                    {
                        then_env.insert(vname.to_string(), TYPE_NIL);
                        else_env.insert(vname.to_string(), subtract_type(orig_t, &TYPE_NIL));
                    }
                }
            }
            "isa" => {
                if elems.len() == 3 {
                    let vname = symbol_name(&elems[1]);
                    if !vname.is_empty()
                        && let Ok(target_t) = self.parse_type(&elems[2], type_params)
                        && let Some(orig_t) = env.get(vname)
                    {
                        else_env.insert(vname.to_string(), subtract_type(orig_t, &target_t));
                        then_env.insert(vname.to_string(), target_t);
                    }
                }
            }
            "not" if elems.len() == 2 => {
                let (t_sub, e_sub) = self.narrow_env(&elems[1], env, type_params);
                return (e_sub, t_sub);
            }
            _ => {}
        }
        (then_env, else_env)
    }

    fn proto_field_named(&self, msg_name: &str, name: &str) -> Option<&FieldDescriptor> {
        let md = self.files.message_by_name(msg_name)?;
        find_proto_field(md.fields(), name)
    }

    fn check_make_proto(
        &mut self,
        expr: &taxlispv1::Expression,
        elems: &[taxlispv1::Expression],
        env: &Env,
        type_params: &TypeParams,
    ) -> Result<Type, TypeError> {
        if elems.len() < 2 || !(elems.len() - 2).is_multiple_of(2) {
            return Err(err_at(
                expr,
                "(make-proto 'pkg/Type :field1 val1 ...) requires a message type and keyword/value pairs",
            ));
        }
        let sym = extract_type_symbol(&elems[1])
            .map_err(|e| err_at(&elems[1], &format!("make-proto: {e}")))?;
        let resolved = self.resolve_proto_type(&sym, &elems[1])?;
        let Type::ProtoMessage(msg_name) = &resolved else {
            return Err(err_at(
                &elems[1],
                &format!("make-proto target {resolved} is not a protobuf message"),
            ));
        };
        let mut i = 2;
        while i < elems.len() {
            let kw = keyword_of(&elems[i]);
            if kw.is_empty() {
                return Err(err_at(
                    &elems[i],
                    "make-proto field name must be a keyword (:field_name)",
                ));
            }
            let Some(fd) = self.proto_field_named(msg_name, kw) else {
                return Err(err_at(
                    &elems[i],
                    &format!("protobuf message {} has no field {}", msg_name, quote(kw)),
                ));
            };
            let want_field_t = self.proto_field_type(fd);
            let got_val_t = self.check_expr(&elems[i + 1], env, type_params)?;
            // Allow setting a field to (Union FieldType Nil) so optional
            // fields can be conditionally populated without boilerplate.
            let allowed_t = new_union(vec![want_field_t.clone(), TYPE_NIL]);
            if !is_assignable_to(&got_val_t, &allowed_t) {
                return Err(err_at(
                    &elems[i + 1],
                    &format!("field :{kw} on {msg_name} expects {want_field_t}, got {got_val_t}"),
                ));
            }
            i += 2;
        }
        match msg_name.as_str() {
            "nexteggapp.taxprotos.common.v1.Money" => Ok(TYPE_MONEY),
            "nexteggapp.taxprotos.common.v1.Quantity" => Ok(TYPE_FLOAT),
            "nexteggapp.taxprotos.common.v1.Date" => Ok(TYPE_DATE),
            _ => Ok(resolved.clone()),
        }
    }

    fn check_proto_with(
        &mut self,
        expr: &taxlispv1::Expression,
        elems: &[taxlispv1::Expression],
        env: &Env,
        type_params: &TypeParams,
    ) -> Result<Type, TypeError> {
        if elems.len() < 4 || !(elems.len() - 2).is_multiple_of(2) {
            return Err(err_at(
                expr,
                "(proto-with msg :field1 val1 ...) requires a message and keyword/value pairs",
            ));
        }
        let base_t = self.check_expr(&elems[1], env, type_params)?;
        let Type::ProtoMessage(msg_name) = &base_t else {
            return Err(err_at(
                &elems[1],
                &format!("proto-with target must be a protobuf message, got {base_t}"),
            ));
        };
        let mut i = 2;
        while i < elems.len() {
            let kw = keyword_of(&elems[i]);
            if kw.is_empty() {
                return Err(err_at(
                    &elems[i],
                    "proto-with field name must be a keyword (:field_name)",
                ));
            }
            let Some(fd) = self.proto_field_named(msg_name, kw) else {
                return Err(err_at(
                    &elems[i],
                    &format!("protobuf message {} has no field {}", msg_name, quote(kw)),
                ));
            };
            let want_field_t = self.proto_field_type(fd);
            let got_val_t = self.check_expr(&elems[i + 1], env, type_params)?;
            if !is_assignable_to(&got_val_t, &new_union(vec![want_field_t.clone(), TYPE_NIL])) {
                return Err(err_at(
                    &elems[i + 1],
                    &format!("field :{kw} on {msg_name} expects {want_field_t}, got {got_val_t}"),
                ));
            }
            i += 2;
        }
        Ok(base_t.clone())
    }

    fn check_make_struct(
        &mut self,
        expr: &taxlispv1::Expression,
        elems: &[taxlispv1::Expression],
        env: &Env,
        type_params: &TypeParams,
    ) -> Result<Type, TypeError> {
        if elems.len() < 2 || !(elems.len() - 2).is_multiple_of(2) {
            return Err(err_at(
                expr,
                "(make-struct StructType :field1 val1 ...) requires a struct type and keyword/value pairs",
            ));
        }
        let st = self.parse_type(&elems[1], type_params)?;
        let Type::Struct(struct_t) = &st else {
            return Err(err_at(
                &elems[1],
                &format!("make-struct target {st} is not a defstruct type"),
            ));
        };
        let mut seen = HashSet::new();
        let mut i = 2;
        while i < elems.len() {
            let kw = keyword_of(&elems[i]);
            if kw.is_empty() {
                return Err(err_at(
                    &elems[i],
                    "make-struct field name must be a keyword (:field)",
                ));
            }
            let Some(want_t) = struct_t.field_type(kw) else {
                return Err(err_at(
                    &elems[i],
                    &format!("struct {st} has no field :{kw}"),
                ));
            };
            let got_t = self.check_expr(&elems[i + 1], env, type_params)?;
            if !is_assignable_to(&got_t, &want_t) {
                return Err(err_at(
                    &elems[i + 1],
                    &format!("struct {st} field :{kw} expects {want_t}, got {got_t}"),
                ));
            }
            seen.insert(kw.to_string());
            i += 2;
        }
        for f in &struct_t.def.fields {
            if !seen.contains(&f.name) {
                return Err(err_at(
                    expr,
                    &format!("make-struct {} missing required field :{}", st, f.name),
                ));
            }
        }
        Ok(st.clone())
    }

    fn check_field_access(
        &mut self,
        expr: &taxlispv1::Expression,
        elems: &[taxlispv1::Expression],
        env: &Env,
        type_params: &TypeParams,
    ) -> Result<Type, TypeError> {
        if elems.len() != 3 {
            return Err(err_at(expr, "(. target 'field.path) requires 2 arguments"));
        }
        let target_t = self.check_expr(&elems[1], env, type_params)?;
        let path_str = extract_field_path(&elems[2])
            .map_err(|e| err_at(&elems[2], &format!("field access: {e}")))?;
        let mut cur_t = target_t;
        for seg in path_str.split('.') {
            if cur_t.equals(&TYPE_DATE) {
                match seg {
                    "year" | "month" | "day" => {
                        cur_t = TYPE_INT;
                        continue;
                    }
                    _ => {
                        return Err(err_at(
                            &elems[2],
                            &format!("Date has no field {}", quote(seg)),
                        ));
                    }
                }
            }
            match &cur_t {
                Type::Struct(ct) => {
                    let Some(ft) = ct.field_type(seg) else {
                        return Err(err_at(
                            &elems[2],
                            &format!("struct {} has no field {}", cur_t, quote(seg)),
                        ));
                    };
                    cur_t = ft;
                }
                Type::ProtoMessage(msg_name) => {
                    let Some(md) = self.files.message_by_name(msg_name) else {
                        return Err(err_at(
                            &elems[2],
                            &format!("protobuf message {} has no field {}", msg_name, quote(seg)),
                        ));
                    };
                    // Check if seg names a synthetic or real oneof on the
                    // message.
                    if let Some(od) = md.oneofs().iter().find(|o| o.name() == seg)
                        && !od.is_synthetic()
                    {
                        {
                            let mut members = Vec::new();
                            for &fi in od.field_indices() {
                                if let Some(fd) = md.fields().get(usize::from(fi)) {
                                    members.push(self.proto_field_type(fd));
                                }
                            }
                            members.push(TYPE_NIL);
                            cur_t = new_union(members);
                            continue;
                        }
                    }
                    let Some(fd) = find_proto_field(md.fields(), seg) else {
                        return Err(err_at(
                            &elems[2],
                            &format!("protobuf message {} has no field {}", msg_name, quote(seg)),
                        ));
                    };
                    cur_t = self.proto_field_type(fd);
                }
                _ => {
                    return Err(err_at(
                        &elems[1],
                        &format!(
                            "cannot access field {} on value of type {}",
                            quote(seg),
                            cur_t
                        ),
                    ));
                }
            }
        }
        Ok(cur_t)
    }

    /// The type of field `fd`: a slice for a repeated field, a table for a
    /// map, else the element type. Go's `protoFieldType` also takes an
    /// unused isRead flag. For proto3 messages, reading a sub-message on a
    /// tax form returns a zero message by default so chained paths like
    /// (. in 'federal.documents.w2s) work without nil-checking every
    /// intermediate message.
    fn proto_field_type(&self, fd: &FieldDescriptor) -> Type {
        match fd.kind() {
            FieldKind::List(elem) => slice_of(self.proto_singular_type(elem)),
            FieldKind::Map { key, value } => Type::Table(
                Box::new(scalar_type(key)),
                Box::new(self.proto_singular_type(value)),
            ),
            FieldKind::Singular(elem) => self.proto_singular_type(elem),
        }
    }

    fn proto_singular_type(&self, kind: SingularKind) -> Type {
        match kind {
            SingularKind::Scalar(s) => scalar_type(s),
            SingularKind::Enum(idx) => {
                Type::ProtoEnum(self.files.enumeration(idx).full_name().to_string())
            }
            SingularKind::Message(idx) => {
                let name = self.files.message(idx).full_name();
                match name {
                    "nexteggapp.taxprotos.common.v1.Money" | "taxprotos.common.v1.Money" => {
                        TYPE_MONEY
                    }
                    "nexteggapp.taxprotos.common.v1.Quantity" | "taxprotos.common.v1.Quantity" => {
                        TYPE_FLOAT
                    }
                    "nexteggapp.taxprotos.common.v1.Date" | "taxprotos.common.v1.Date" => TYPE_DATE,
                    _ => Type::ProtoMessage(name.to_string()),
                }
            }
        }
    }

    /// Go's `RangeFilesByPackage(pkg, ...)` searching each file's enums,
    /// then its messages' enums depth first, for one with value `val_name`;
    /// the enum's full name.
    fn find_enum_in_package(&self, pkg: &str, val_name: &str) -> Option<String> {
        for fd in self.files.files() {
            if fd.package.as_deref().unwrap_or("") != pkg {
                continue;
            }
            for ed in &fd.enum_type {
                if ed.value.iter().any(|v| v.name.as_deref() == Some(val_name)) {
                    return Some(qualify(pkg, ed.name.as_deref().unwrap_or("")));
                }
            }
            if let Some(found) = find_enum_in_messages(pkg, &fd.message_type, val_name) {
                return Some(found);
            }
        }
        None
    }

    fn resolve_proto_enum_value(
        &self,
        sym: &taxlispv1::Symbol,
        expr: &taxlispv1::Expression,
    ) -> Result<Type, TypeError> {
        let mut pkg_prefix = sym.package.as_str();
        if let Some(expanded) = self.proto_aliases.get(pkg_prefix) {
            pkg_prefix = expanded;
        }
        let mut raw_name = sym.name.as_str();
        if let Some(idx) = raw_name.rfind('.') {
            raw_name = &raw_name[idx + 1..];
        }
        let mut found = self.find_enum_in_package(pkg_prefix, raw_name);
        if found.is_none() && !pkg_prefix.starts_with("nexteggapp.") {
            found = self.find_enum_in_package(&format!("nexteggapp.{pkg_prefix}"), raw_name);
        }
        match found {
            Some(name) => Ok(Type::ProtoEnum(name)),
            None => Err(err_at(
                expr,
                &format!("unknown enum value {}/{}", sym.package, sym.name),
            )),
        }
    }

    fn parse_param_bindings(
        &self,
        expr: &taxlispv1::Expression,
        type_params: &TypeParams,
    ) -> Result<(Vec<String>, Vec<Type>), TypeError> {
        let Some(list) = get_list(expr) else {
            return Err(err_at(
                expr,
                "parameter list must be a list of ((name Type) ...)",
            ));
        };
        let mut names = Vec::new();
        let mut types = Vec::new();
        for item in &list.expressions {
            let pair = match get_list(item) {
                Some(p) if p.expressions.len() == 2 => &p.expressions,
                _ => {
                    return Err(err_at(
                        item,
                        "each parameter must be (name Type) with mandatory type annotation",
                    ));
                }
            };
            let pname = symbol_name(&pair[0]);
            if pname.is_empty() {
                return Err(err_at(&pair[0], "parameter name must be a symbol"));
            }
            let ptype = self.parse_type(&pair[1], type_params)?;
            names.push(pname.to_string());
            types.push(ptype);
        }
        Ok((names, types))
    }
}

fn qualify(scope: &str, name: &str) -> String {
    if scope.is_empty() {
        name.to_string()
    } else {
        format!("{scope}.{name}")
    }
}

fn find_enum_in_messages(
    scope: &str,
    ms: &[buffa_descriptor::generated::descriptor::DescriptorProto],
    val_name: &str,
) -> Option<String> {
    for md in ms {
        let md_name = qualify(scope, md.name.as_deref().unwrap_or(""));
        for ed in &md.enum_type {
            if ed.value.iter().any(|v| v.name.as_deref() == Some(val_name)) {
                return Some(qualify(&md_name, ed.name.as_deref().unwrap_or("")));
            }
        }
        if let Some(found) = find_enum_in_messages(&md_name, &md.nested_type, val_name) {
            return Some(found);
        }
    }
    None
}

fn scalar_type(s: ScalarType) -> Type {
    match s {
        ScalarType::Bool => TYPE_BOOL,
        ScalarType::Int32
        | ScalarType::Sint32
        | ScalarType::Sfixed32
        | ScalarType::Int64
        | ScalarType::Sint64
        | ScalarType::Sfixed64
        | ScalarType::Uint32
        | ScalarType::Fixed32
        | ScalarType::Uint64
        | ScalarType::Fixed64 => TYPE_INT,
        ScalarType::Float | ScalarType::Double => TYPE_FLOAT,
        ScalarType::String => TYPE_STRING,
        ScalarType::Bytes => TYPE_STRING,
    }
}

/// Go's `Fields().ByName(name)`, then `Fields().ByJSONName(name)`.
fn find_proto_field<'a>(fields: &'a [FieldDescriptor], name: &str) -> Option<&'a FieldDescriptor> {
    fields
        .iter()
        .find(|f| f.name() == name)
        .or_else(|| fields.iter().find(|f| f.json_name() == name))
}

fn subtract_type(orig: &Type, remove: &Type) -> Type {
    let Type::Union(members) = orig else {
        return orig.clone();
    };
    let kept: Vec<Type> = members
        .iter()
        .filter(|m| !is_assignable_to(m, remove))
        .cloned()
        .collect();
    if kept.is_empty() {
        return orig.clone();
    }
    new_union(kept)
}

fn is_type_param_list(l: &taxlispv1::List) -> bool {
    if l.expressions.is_empty() {
        return true;
    }
    if l.expressions.iter().any(|e| symbol_name(e).is_empty()) {
        return false;
    }
    // If all elements are single uppercase letters or type params, treat as
    // type param list.
    for e in &l.expressions {
        let s = symbol_name(e);
        if s.as_bytes().first().is_some_and(|b| b.is_ascii_lowercase()) {
            return false;
        }
    }
    true
}

fn parse_def_header(expr: &taxlispv1::Expression) -> Result<(String, Vec<String>), TypeError> {
    let s = symbol_name(expr);
    if !s.is_empty() {
        return Ok((s.to_string(), Vec::new()));
    }
    let elems = match get_list(expr) {
        Some(list) if !list.expressions.is_empty() => &list.expressions,
        _ => {
            return Err(err_at(
                expr,
                "definition name must be a symbol or (name T U ...)",
            ));
        }
    };
    let name = symbol_name(&elems[0]);
    if name.is_empty() {
        return Err(err_at(&elems[0], "definition name must be a symbol"));
    }
    let mut tparams = Vec::new();
    for tp in &elems[1..] {
        let tps = symbol_name(tp);
        if tps.is_empty() {
            return Err(err_at(tp, "type parameter must be a symbol"));
        }
        tparams.push(tps.to_string());
    }
    Ok((name.to_string(), tparams))
}

fn extract_quoted_or_string(expr: &taxlispv1::Expression) -> Result<String, String> {
    if let Some(a) = get_atom(expr) {
        match &a.primitive {
            Some(Primitive::StringValue(s)) if !s.is_empty() => return Ok(s.clone()),
            Some(Primitive::Keyword(kw)) if !kw.is_empty() => return Ok(kw.clone()),
            Some(Primitive::Symbol(sym)) => return Ok(format_sym(sym)),
            _ => {}
        }
    }
    if let Some(l) = get_list(expr)
        && l.expressions.len() == 2
        && symbol_name(&l.expressions[0]) == "quote"
    {
        return extract_quoted_or_string(&l.expressions[1]);
    }
    Err("expected symbol or string".to_string())
}

fn extract_type_symbol(expr: &taxlispv1::Expression) -> Result<taxlispv1::Symbol, String> {
    if let Some(a) = get_atom(expr) {
        match &a.primitive {
            Some(Primitive::Symbol(sym)) => return Ok((**sym).clone()),
            Some(Primitive::StringValue(s)) if !s.is_empty() => {
                if let Some(idx) = s.rfind('/')
                    && idx > 0
                {
                    return Ok(taxlispv1::Symbol {
                        package: s[..idx].to_string(),
                        name: s[idx + 1..].to_string(),
                        ..Default::default()
                    });
                }
                return Ok(taxlispv1::Symbol {
                    name: s.clone(),
                    ..Default::default()
                });
            }
            _ => {}
        }
    }
    if let Some(l) = get_list(expr)
        && l.expressions.len() == 2
        && symbol_name(&l.expressions[0]) == "quote"
    {
        return extract_type_symbol(&l.expressions[1]);
    }
    Err("expected quoted symbol or string naming a protobuf message type".to_string())
}

fn extract_field_path(expr: &taxlispv1::Expression) -> Result<String, String> {
    extract_quoted_or_string(expr)
}

fn get_atom(expr: &taxlispv1::Expression) -> Option<&taxlispv1::Atom> {
    match &expr.value {
        Some(Value::Atom(a)) => Some(a),
        _ => None,
    }
}

fn get_list(expr: &taxlispv1::Expression) -> Option<&taxlispv1::List> {
    match &expr.value {
        Some(Value::List(l)) => Some(l),
        _ => None,
    }
}

/// Go's `expr.GetAtom().GetKeyword()`: "" unless a keyword atom.
fn keyword_of(expr: &taxlispv1::Expression) -> &str {
    match get_atom(expr).and_then(|a| a.primitive.as_ref()) {
        Some(Primitive::Keyword(kw)) => kw,
        _ => "",
    }
}

fn symbol_name(expr: &taxlispv1::Expression) -> &str {
    match get_atom(expr).and_then(|a| a.primitive.as_ref()) {
        Some(Primitive::Symbol(sym)) if sym.package.is_empty() => &sym.name,
        _ => "",
    }
}

fn is_keyword_atom(expr: &taxlispv1::Expression) -> bool {
    !keyword_of(expr).is_empty()
}

fn is_keyword_named(expr: &taxlispv1::Expression, kw: &str) -> bool {
    get_atom(expr).is_some() && keyword_of(expr) == kw
}

fn format_sym(sym: &taxlispv1::Symbol) -> String {
    if sym.package.is_empty() {
        return sym.name.clone();
    }
    format!("{}/{}", sym.package, sym.name)
}

fn err_at(expr: &taxlispv1::Expression, msg: &str) -> TypeError {
    let syn = match &expr.value {
        Some(Value::Atom(a)) => a.syntax.as_option(),
        Some(Value::List(l)) => l.syntax.as_option(),
        None => None,
    };
    match syn {
        Some(syn) if !syn.file.is_empty() => TypeError::at(
            crate::SourcePos {
                file: syn.file.clone(),
                line: syn.start_line.into(),
                column: syn.start_column.into(),
            },
            msg,
        ),
        _ => TypeError::new(msg),
    }
}
