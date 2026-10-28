// Package types implements the static type system and typechecker for taxlisp,
// a strongly typed, Arc-inspired functional Lisp with slices, tables, union
// types, explicit generics, and native protobuf interaction.
package types

import (
	"fmt"
	"sort"
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	taxlispv1 "github.com/nexteggapp/taxcalcs/taxlisp/v1"
)

// Type represents a static type in taxlisp.
type Type interface {
	String() string
	Equals(other Type) bool
	isType()
}

// PrimitiveKind enumerates the built-in scalar types.
type PrimitiveKind string

const (
	PrimMoney  PrimitiveKind = "Money"
	PrimFloat  PrimitiveKind = "Float"
	PrimInt    PrimitiveKind = "Int"
	PrimBool   PrimitiveKind = "Bool"
	PrimString PrimitiveKind = "String"
	PrimDate   PrimitiveKind = "Date"
	PrimSymbol PrimitiveKind = "Symbol"
	PrimNil    PrimitiveKind = "Nil"
)

// PrimitiveType is a built-in scalar type.
type PrimitiveType struct {
	Kind PrimitiveKind
}

func (p *PrimitiveType) String() string { return string(p.Kind) }
func (p *PrimitiveType) Equals(o Type) bool {
	op, ok := o.(*PrimitiveType)
	return ok && p.Kind == op.Kind
}
func (*PrimitiveType) isType() {}

var (
	TypeMoney  = &PrimitiveType{Kind: PrimMoney}
	TypeFloat  = &PrimitiveType{Kind: PrimFloat}
	TypeInt    = &PrimitiveType{Kind: PrimInt}
	TypeBool   = &PrimitiveType{Kind: PrimBool}
	TypeString = &PrimitiveType{Kind: PrimString}
	TypeDate   = &PrimitiveType{Kind: PrimDate}
	TypeSymbol = &PrimitiveType{Kind: PrimSymbol}
	TypeNil    = &PrimitiveType{Kind: PrimNil}
)

// TypeVar is a generic type parameter bound by `(def (fname T U) ...)` or
// `(defstruct Name (T U) ...)`.
type TypeVar struct {
	Name string
}

func (tv *TypeVar) String() string { return tv.Name }
func (tv *TypeVar) Equals(o Type) bool {
	ot, ok := o.(*TypeVar)
	return ok && tv.Name == ot.Name
}
func (*TypeVar) isType() {}

// SliceType is a contiguous slice `(Slice Elem)`.
type SliceType struct {
	Elem Type
}

func (s *SliceType) String() string { return fmt.Sprintf("(Slice %s)", s.Elem.String()) }
func (s *SliceType) Equals(o Type) bool {
	os, ok := o.(*SliceType)
	return ok && s.Elem.Equals(os.Elem)
}
func (*SliceType) isType() {}

// TableType is an immutable map `(Table Key Value)`.
type TableType struct {
	Key   Type
	Value Type
}

func (t *TableType) String() string {
	return fmt.Sprintf("(Table %s %s)", t.Key.String(), t.Value.String())
}
func (t *TableType) Equals(o Type) bool {
	ot, ok := o.(*TableType)
	return ok && t.Key.Equals(ot.Key) && t.Value.Equals(ot.Value)
}
func (*TableType) isType() {}

// UnionType is an unordered, flattened, deduplicated union `(Union T1 T2 ...)`.
type UnionType struct {
	Members []Type
}

// NewUnion constructs a flattened, deduplicated UnionType (or returns the sole
// member type if only one distinct member remains).
func NewUnion(types ...Type) Type {
	var flat []Type
	var add func(Type)
	add = func(t Type) {
		if u, ok := t.(*UnionType); ok {
			for _, m := range u.Members {
				add(m)
			}
			return
		}
		for _, existing := range flat {
			if existing.Equals(t) {
				return
			}
		}
		flat = append(flat, t)
	}
	for _, t := range types {
		add(t)
	}
	if len(flat) == 1 {
		return flat[0]
	}
	sort.Slice(flat, func(i, j int) bool {
		return flat[i].String() < flat[j].String()
	})
	return &UnionType{Members: flat}
}

func (u *UnionType) String() string {
	parts := make([]string, len(u.Members))
	for i, m := range u.Members {
		parts[i] = m.String()
	}
	return "(Union " + strings.Join(parts, " ") + ")"
}

func (u *UnionType) Equals(o Type) bool {
	ou, ok := o.(*UnionType)
	if !ok || len(u.Members) != len(ou.Members) {
		return false
	}
	for i := range u.Members {
		if !u.Members[i].Equals(ou.Members[i]) {
			return false
		}
	}
	return true
}
func (*UnionType) isType() {}

// FnType is a function type `(Fn (P1 P2 ...) Return)` or a generic function
// with TypeParams.
type FnType struct {
	TypeParams []string
	Params     []Type
	Return     Type
}

func (f *FnType) String() string {
	ps := make([]string, len(f.Params))
	for i, p := range f.Params {
		ps[i] = p.String()
	}
	if len(f.TypeParams) > 0 {
		return fmt.Sprintf("(forall (%s) (Fn (%s) %s))", strings.Join(f.TypeParams, " "), strings.Join(ps, " "), f.Return.String())
	}
	return fmt.Sprintf("(Fn (%s) %s)", strings.Join(ps, " "), f.Return.String())
}

func (f *FnType) Equals(o Type) bool {
	of, ok := o.(*FnType)
	if !ok || len(f.TypeParams) != len(of.TypeParams) || len(f.Params) != len(of.Params) {
		return false
	}
	for i := range f.TypeParams {
		if f.TypeParams[i] != of.TypeParams[i] {
			return false
		}
	}
	for i := range f.Params {
		if !f.Params[i].Equals(of.Params[i]) {
			return false
		}
	}
	return f.Return.Equals(of.Return)
}
func (*FnType) isType() {}

// StructField describes a single named field of a StructDef.
type StructField struct {
	Name string
	Type Type
}

// StructDef describes a nominal struct defined via `defstruct`.
type StructDef struct {
	Name       string
	TypeParams []string
	Fields     []StructField
}

// StructType is an instantiated user-defined struct type.
type StructType struct {
	Def      *StructDef
	TypeArgs []Type
}

func (s *StructType) String() string {
	if len(s.TypeArgs) == 0 {
		return s.Def.Name
	}
	args := make([]string, len(s.TypeArgs))
	for i, a := range s.TypeArgs {
		args[i] = a.String()
	}
	return fmt.Sprintf("(%s %s)", s.Def.Name, strings.Join(args, " "))
}

func (s *StructType) Equals(o Type) bool {
	os, ok := o.(*StructType)
	if !ok || s.Def.Name != os.Def.Name || len(s.TypeArgs) != len(os.TypeArgs) {
		return false
	}
	for i := range s.TypeArgs {
		if !s.TypeArgs[i].Equals(os.TypeArgs[i]) {
			return false
		}
	}
	return true
}
func (*StructType) isType() {}

// FieldType returns the instantiated type of field `name` on this struct.
func (s *StructType) FieldType(name string) (Type, bool) {
	subst := map[string]Type{}
	for i, tp := range s.Def.TypeParams {
		if i < len(s.TypeArgs) {
			subst[tp] = s.TypeArgs[i]
		}
	}
	for _, f := range s.Def.Fields {
		if f.Name == name {
			return Substitute(f.Type, subst), true
		}
	}
	return nil, false
}

// ProtoMessageType is a protobuf message type backed by a MessageDescriptor.
type ProtoMessageType struct {
	Desc protoreflect.MessageDescriptor
}

func (p *ProtoMessageType) String() string { return string(p.Desc.FullName()) }
func (p *ProtoMessageType) Equals(o Type) bool {
	op, ok := o.(*ProtoMessageType)
	return ok && p.Desc.FullName() == op.Desc.FullName()
}
func (*ProtoMessageType) isType() {}

// ProtoEnumType is a protobuf enum type backed by an EnumDescriptor.
type ProtoEnumType struct {
	Desc protoreflect.EnumDescriptor
}

func (p *ProtoEnumType) String() string { return string(p.Desc.FullName()) }
func (p *ProtoEnumType) Equals(o Type) bool {
	op, ok := o.(*ProtoEnumType)
	return ok && p.Desc.FullName() == op.Desc.FullName()
}
func (*ProtoEnumType) isType() {}

// IsAssignableTo reports whether a value of type `got` can be used where a
// value of type `want` is expected (including Union introduction & subsetting).
func IsAssignableTo(got, want Type) bool {
	if got.Equals(want) {
		return true
	}
	if uGot, ok := got.(*UnionType); ok {
		for _, m := range uGot.Members {
			if !IsAssignableTo(m, want) {
				return false
			}
		}
		return true
	}
	if uWant, ok := want.(*UnionType); ok {
		for _, m := range uWant.Members {
			if IsAssignableTo(got, m) {
				return true
			}
		}
		return false
	}
	if sGot, ok := got.(*SliceType); ok {
		if sWant, ok := want.(*SliceType); ok {
			return IsAssignableTo(sGot.Elem, sWant.Elem)
		}
	}
	return false
}

// Substitute replaces TypeVars in t according to subst.
func Substitute(t Type, subst map[string]Type) Type {
	if len(subst) == 0 {
		return t
	}
	switch v := t.(type) {
	case *TypeVar:
		if rep, ok := subst[v.Name]; ok {
			return rep
		}
		return v
	case *SliceType:
		return &SliceType{Elem: Substitute(v.Elem, subst)}
	case *TableType:
		return &TableType{
			Key:   Substitute(v.Key, subst),
			Value: Substitute(v.Value, subst),
		}
	case *UnionType:
		members := make([]Type, len(v.Members))
		for i, m := range v.Members {
			members[i] = Substitute(m, subst)
		}
		return NewUnion(members...)
	case *FnType:
		filtered := map[string]Type{}
		for k, val := range subst {
			shadowed := false
			for _, tp := range v.TypeParams {
				if tp == k {
					shadowed = true
					break
				}
			}
			if !shadowed {
				filtered[k] = val
			}
		}
		params := make([]Type, len(v.Params))
		for i, p := range v.Params {
			params[i] = Substitute(p, filtered)
		}
		return &FnType{
			TypeParams: v.TypeParams,
			Params:     params,
			Return:     Substitute(v.Return, filtered),
		}
	case *StructType:
		args := make([]Type, len(v.TypeArgs))
		for i, a := range v.TypeArgs {
			args[i] = Substitute(a, subst)
		}
		return &StructType{Def: v.Def, TypeArgs: args}
	default:
		return t
	}
}

// Unify matches pattern type `pat` (containing TypeVars in `typeParams`) against
// concrete type `arg`, recording bindings in `subst`.
func Unify(pat, arg Type, typeParams map[string]bool, subst map[string]Type) error {
	switch p := pat.(type) {
	case *TypeVar:
		if typeParams[p.Name] {
			if existing, ok := subst[p.Name]; ok {
				if IsAssignableTo(arg, existing) {
					return nil
				}
				if IsAssignableTo(existing, arg) {
					subst[p.Name] = arg
					return nil
				}
				// Merge into union if different? No, require compatible typeunless unified.
				return fmt.Errorf("type parameter %s bound to conflicting types %s and %s", p.Name, existing, arg)
			}
			subst[p.Name] = arg
			return nil
		}
		if !IsAssignableTo(arg, p) {
			return fmt.Errorf("got %s, want %s", arg, p)
		}
		return nil
	case *SliceType:
		as, ok := arg.(*SliceType)
		if !ok {
			return fmt.Errorf("got %s, want %s", arg, p)
		}
		return Unify(p.Elem, as.Elem, typeParams, subst)
	case *TableType:
		at, ok := arg.(*TableType)
		if !ok {
			return fmt.Errorf("got %s, want %s", arg, p)
		}
		if err := Unify(p.Key, at.Key, typeParams, subst); err != nil {
			return err
		}
		return Unify(p.Value, at.Value, typeParams, subst)
	case *FnType:
		af, ok := arg.(*FnType)
		if !ok || len(p.Params) != len(af.Params) {
			return fmt.Errorf("got %s, want %s", arg, p)
		}
		for i := range p.Params {
			if err := Unify(p.Params[i], af.Params[i], typeParams, subst); err != nil {
				return err
			}
		}
		return Unify(p.Return, af.Return, typeParams, subst)
	case *StructType:
		as, ok := arg.(*StructType)
		if !ok || p.Def.Name != as.Def.Name || len(p.TypeArgs) != len(as.TypeArgs) {
			return fmt.Errorf("got %s, want %s", arg, p)
		}
		for i := range p.TypeArgs {
			if err := Unify(p.TypeArgs[i], as.TypeArgs[i], typeParams, subst); err != nil {
				return err
			}
		}
		return nil
	case *UnionType:
		// If pattern is `(Union T Nil)` and arg is `(Union Concrete Nil)`:
		if au, ok := arg.(*UnionType); ok && len(p.Members) == 2 && len(au.Members) == 2 {
			// Match non-Nil against non-Nil when Nil is in both.
			pNonNil := nonNilMember(p)
			aNonNil := nonNilMember(au)
			if pNonNil != nil && aNonNil != nil {
				return Unify(pNonNil, aNonNil, typeParams, subst)
			}
		}
		if !IsAssignableTo(arg, Substitute(p, subst)) {
			return fmt.Errorf("got %s, want %s", arg, p)
		}
		return nil
	default:
		if !IsAssignableTo(arg, p) {
			return fmt.Errorf("got %s, want %s", arg, p)
		}
		return nil
	}
}

func nonNilMember(u *UnionType) Type {
	if len(u.Members) != 2 {
		return nil
	}
	if u.Members[0].Equals(TypeNil) {
		return u.Members[1]
	}
	if u.Members[1].Equals(TypeNil) {
		return u.Members[0]
	}
	return nil
}

// TypeAlias defines a named or generic type alias via `(deftype ...)`.
type TypeAlias struct {
	Name       string
	TypeParams []string
	Body       *taxlispv1.Expression
}

// Checker holds the type environment, proto descriptor registry, and user
// struct/type definitions across one or more modules.
type Checker struct {
	Files        *protoregistry.Files
	ProtoAliases map[string]string // alias -> proto package prefix
	Structs      map[string]*StructDef
	Aliases      map[string]*TypeAlias
	Globals      map[string]Type
}

// NewChecker creates a new typechecker backed by the given protobuf file
// registry (defaults to protoregistry.GlobalFiles if nil).
func NewChecker(files *protoregistry.Files) *Checker {
	if files == nil {
		files = protoregistry.GlobalFiles
	}
	c := &Checker{
		Files:        files,
		ProtoAliases: map[string]string{},
		Structs:      map[string]*StructDef{},
		Aliases:      map[string]*TypeAlias{},
		Globals:      map[string]Type{},
	}
	c.registerBuiltins()
	return c
}

func (c *Checker) registerBuiltins() {
	c.Globals["nil"] = TypeNil
	c.Globals["not"] = &FnType{Params: []Type{TypeBool}, Return: TypeBool}
	c.Globals["nil?"] = &FnType{
		TypeParams: []string{"T"},
		Params:     []Type{&TypeVar{Name: "T"}},
		Return:     TypeBool,
	}
	c.Globals["empty-slice"] = &FnType{
		TypeParams: []string{"T"},
		Params:     []Type{},
		Return:     &SliceType{Elem: &TypeVar{Name: "T"}},
	}
	c.Globals["len"] = &FnType{
		TypeParams: []string{"T"},
		Params:     []Type{&SliceType{Elem: &TypeVar{Name: "T"}}},
		Return:     TypeInt,
	}
	c.Globals["empty?"] = &FnType{
		TypeParams: []string{"T"},
		Params:     []Type{&SliceType{Elem: &TypeVar{Name: "T"}}},
		Return:     TypeBool,
	}
	c.Globals["elt"] = &FnType{
		TypeParams: []string{"T"},
		Params:     []Type{&SliceType{Elem: &TypeVar{Name: "T"}}, TypeInt},
		Return:     &TypeVar{Name: "T"},
	}
	c.Globals["subslice"] = &FnType{
		TypeParams: []string{"T"},
		Params:     []Type{&SliceType{Elem: &TypeVar{Name: "T"}}, TypeInt, TypeInt},
		Return:     &SliceType{Elem: &TypeVar{Name: "T"}},
	}
	c.Globals["append"] = &FnType{
		TypeParams: []string{"T"},
		Params:     []Type{&SliceType{Elem: &TypeVar{Name: "T"}}, &SliceType{Elem: &TypeVar{Name: "T"}}},
		Return:     &SliceType{Elem: &TypeVar{Name: "T"}},
	}
	c.Globals["map"] = &FnType{
		TypeParams: []string{"T", "U"},
		Params: []Type{
			&FnType{Params: []Type{&TypeVar{Name: "T"}}, Return: &TypeVar{Name: "U"}},
			&SliceType{Elem: &TypeVar{Name: "T"}},
		},
		Return: &SliceType{Elem: &TypeVar{Name: "U"}},
	}
	c.Globals["keep"] = &FnType{
		TypeParams: []string{"T"},
		Params: []Type{
			&FnType{Params: []Type{&TypeVar{Name: "T"}}, Return: TypeBool},
			&SliceType{Elem: &TypeVar{Name: "T"}},
		},
		Return: &SliceType{Elem: &TypeVar{Name: "T"}},
	}
	c.Globals["fold"] = &FnType{
		TypeParams: []string{"T", "Acc"},
		Params: []Type{
			&FnType{Params: []Type{&TypeVar{Name: "Acc"}, &TypeVar{Name: "T"}}, Return: &TypeVar{Name: "Acc"}},
			&TypeVar{Name: "Acc"},
			&SliceType{Elem: &TypeVar{Name: "T"}},
		},
		Return: &TypeVar{Name: "Acc"},
	}
	c.Globals["some"] = &FnType{
		TypeParams: []string{"T"},
		Params: []Type{
			&FnType{Params: []Type{&TypeVar{Name: "T"}}, Return: TypeBool},
			&SliceType{Elem: &TypeVar{Name: "T"}},
		},
		Return: TypeBool,
	}
	c.Globals["all"] = &FnType{
		TypeParams: []string{"T"},
		Params: []Type{
			&FnType{Params: []Type{&TypeVar{Name: "T"}}, Return: TypeBool},
			&SliceType{Elem: &TypeVar{Name: "T"}},
		},
		Return: TypeBool,
	}
	c.Globals["sum-money"] = &FnType{
		Params: []Type{&SliceType{Elem: TypeMoney}},
		Return: TypeMoney,
	}
	c.Globals["sum"] = &FnType{
		TypeParams: []string{"T"},
		Params: []Type{
			&FnType{Params: []Type{&TypeVar{Name: "T"}}, Return: TypeMoney},
			&SliceType{Elem: &TypeVar{Name: "T"}},
		},
		Return: TypeMoney,
	}
	c.Globals["lookup"] = &FnType{
		TypeParams: []string{"K", "V"},
		Params: []Type{
			&TableType{Key: &TypeVar{Name: "K"}, Value: &TypeVar{Name: "V"}},
			&TypeVar{Name: "K"},
		},
		Return: NewUnion(&TypeVar{Name: "V"}, TypeNil),
	}
	c.Globals["lookup-or"] = &FnType{
		TypeParams: []string{"K", "V"},
		Params: []Type{
			&TableType{Key: &TypeVar{Name: "K"}, Value: &TypeVar{Name: "V"}},
			&TypeVar{Name: "K"},
			&TypeVar{Name: "V"},
		},
		Return: &TypeVar{Name: "V"},
	}
	c.Globals["round-dollar"] = &FnType{Params: []Type{TypeMoney}, Return: TypeMoney}
	c.Globals["round-cents"] = &FnType{Params: []Type{TypeMoney}, Return: TypeMoney}
	c.Globals["round-rate4"] = &FnType{Params: []Type{TypeFloat}, Return: TypeFloat}
	c.Globals["clamp-zero"] = &FnType{Params: []Type{TypeMoney}, Return: TypeMoney}
	c.Globals["abs-money"] = &FnType{Params: []Type{TypeMoney}, Return: TypeMoney}
	c.Globals["int-to-money"] = &FnType{Params: []Type{TypeInt}, Return: TypeMoney}
	c.Globals["int-to-float"] = &FnType{Params: []Type{TypeInt}, Return: TypeFloat}
	c.Globals["money-to-float"] = &FnType{Params: []Type{TypeMoney}, Return: TypeFloat}
	c.Globals["float-to-money"] = &FnType{Params: []Type{TypeFloat}, Return: TypeMoney}
	c.Globals["date-year"] = &FnType{Params: []Type{TypeDate}, Return: TypeInt}
	c.Globals["date-month"] = &FnType{Params: []Type{TypeDate}, Return: TypeInt}
	c.Globals["date-day"] = &FnType{Params: []Type{TypeDate}, Return: TypeInt}
}

// CheckModule typechecks all top-level expressions in a ModuleFile.
func (c *Checker) CheckModule(mf *taxlispv1.ModuleFile) error {
	// Pass 1: register `use-proto`, `deftype`, `defstruct`, and function signatures
	// so functions in the same module can call each other regardless of order.
	for _, expr := range mf.GetExpressions() {
		if err := c.predeclareTopLevel(expr); err != nil {
			return err
		}
	}
	// Pass 2: typecheck all top-level definitions and expressions.
	for _, expr := range mf.GetExpressions() {
		if _, err := c.CheckExpr(expr, map[string]Type{}, map[string]bool{}); err != nil {
			return err
		}
	}
	return nil
}

func (c *Checker) predeclareTopLevel(expr *taxlispv1.Expression) error {
	list := expr.GetList()
	if list == nil || len(list.GetExpressions()) == 0 {
		return nil
	}
	elems := list.GetExpressions()
	head := symbolName(elems[0])
	switch head {
	case "use-proto":
		if len(elems) != 3 {
			return errAt(expr, "use-proto requires 2 arguments: (use-proto 'pkg.path 'alias)")
		}
		pkg, err := extractQuotedOrString(elems[1])
		if err != nil {
			return errAt(elems[1], "use-proto package: %v", err)
		}
		alias, err := extractQuotedOrString(elems[2])
		if err != nil {
			return errAt(elems[2], "use-proto alias: %v", err)
		}
		c.ProtoAliases[alias] = pkg
	case "deftype":
		if len(elems) != 3 {
			return errAt(expr, "deftype requires (deftype NameOrList TypeExpr)")
		}
		name, tparams, err := parseDefHeader(elems[1])
		if err != nil {
			return err
		}
		c.Aliases[name] = &TypeAlias{
			Name:       name,
			TypeParams: tparams,
			Body:       elems[2],
		}
	case "defstruct":
		if len(elems) < 3 {
			return errAt(expr, "defstruct requires (defstruct NameOrGeneric (field Type)...)")
		}
		name, tparams, err := parseDefHeader(elems[1])
		if err != nil {
			return err
		}
		tpSet := map[string]bool{}
		for _, tp := range tparams {
			tpSet[tp] = true
		}
		fieldStart := 2
		// Also support optional explicit type-param list in elems[2] if elems[2] is a list of bare symbols (or empty list ()).
		if l2 := elems[2].GetList(); l2 != nil && isTypeParamList(l2) {
			for _, tpExpr := range l2.GetExpressions() {
				s := symbolName(tpExpr)
				if !tpSet[s] {
					tparams = append(tparams, s)
					tpSet[s] = true
				}
			}
			fieldStart = 3
		}
		var fields []StructField
		for _, fExpr := range elems[fieldStart:] {
			fl := fExpr.GetList()
			if fl == nil || len(fl.GetExpressions()) != 2 {
				return errAt(fExpr, "struct field must be (fieldName FieldType)")
			}
			fname := symbolName(fl.GetExpressions()[0])
			if fname == "" {
				return errAt(fl.GetExpressions()[0], "struct field name must be a symbol")
			}
			ftype, err := c.ParseType(fl.GetExpressions()[1], tpSet)
			if err != nil {
				return err
			}
			fields = append(fields, StructField{Name: fname, Type: ftype})
		}
		sd := &StructDef{
			Name:       name,
			TypeParams: tparams,
			Fields:     fields,
		}
		c.Structs[name] = sd
		fieldTypes := make([]Type, len(fields))
		for i, f := range fields {
			fieldTypes[i] = f.Type
		}
		typeVars := make([]Type, len(tparams))
		for i, tp := range tparams {
			typeVars[i] = &TypeVar{Name: tp}
		}
		c.Globals[name] = &FnType{
			TypeParams: tparams,
			Params:     fieldTypes,
			Return:     &StructType{Def: sd, TypeArgs: typeVars},
		}
	case "def":
		if len(elems) < 5 {
			return errAt(expr, "def requires (def name ((arg Type)...) ReturnType body...)")
		}
		name, tparams, err := parseDefHeader(elems[1])
		if err != nil {
			return err
		}
		tpSet := map[string]bool{}
		for _, tp := range tparams {
			tpSet[tp] = true
		}
		_, paramTypes, err := c.parseParamBindings(elems[2], tpSet)
		if err != nil {
			return err
		}
		retType, err := c.ParseType(elems[3], tpSet)
		if err != nil {
			return err
		}
		c.Globals[name] = &FnType{
			TypeParams: tparams,
			Params:     paramTypes,
			Return:     retType,
		}
	}
	return nil
}

// ParseType parses a type S-expression into a Type.
func (c *Checker) ParseType(expr *taxlispv1.Expression, typeParams map[string]bool) (Type, error) {
	if a := expr.GetAtom(); a != nil {
		sym := a.GetSymbol()
		if sym == nil {
			return nil, errAt(expr, "expected type symbol, got literal")
		}
		if sym.GetPackage() == "" {
			name := sym.GetName()
			if typeParams[name] {
				return &TypeVar{Name: name}, nil
			}
			switch PrimitiveKind(name) {
			case PrimMoney:
				return TypeMoney, nil
			case PrimFloat:
				return TypeFloat, nil
			case PrimInt:
				return TypeInt, nil
			case PrimBool:
				return TypeBool, nil
			case PrimString:
				return TypeString, nil
			case PrimDate:
				return TypeDate, nil
			case PrimSymbol:
				return TypeSymbol, nil
			case PrimNil:
				return TypeNil, nil
			}
			if alias, ok := c.Aliases[name]; ok && len(alias.TypeParams) == 0 {
				return c.ParseType(alias.Body, typeParams)
			}
			if sdef, ok := c.Structs[name]; ok {
				if len(sdef.TypeParams) != 0 {
					return nil, errAt(expr, "generic struct %s requires %d type arguments", name, len(sdef.TypeParams))
				}
				return &StructType{Def: sdef}, nil
			}
		}
		return c.resolveProtoType(sym, expr)
	}

	list := expr.GetList()
	if list == nil || len(list.GetExpressions()) == 0 {
		return nil, errAt(expr, "empty type expression")
	}
	elems := list.GetExpressions()
	head := symbolName(elems[0])
	switch head {
	case "Slice":
		if len(elems) != 2 {
			return nil, errAt(expr, "(Slice T) requires exactly 1 type argument")
		}
		elemType, err := c.ParseType(elems[1], typeParams)
		if err != nil {
			return nil, err
		}
		return &SliceType{Elem: elemType}, nil
	case "Option":
		if len(elems) != 2 {
			return nil, errAt(expr, "(Option T) requires exactly 1 type argument")
		}
		elemType, err := c.ParseType(elems[1], typeParams)
		if err != nil {
			return nil, err
		}
		return NewUnion(elemType, TypeNil), nil
	case "Table":
		if len(elems) != 3 {
			return nil, errAt(expr, "(Table K V) requires 2 type arguments")
		}
		kt, err := c.ParseType(elems[1], typeParams)
		if err != nil {
			return nil, err
		}
		vt, err := c.ParseType(elems[2], typeParams)
		if err != nil {
			return nil, err
		}
		return &TableType{Key: kt, Value: vt}, nil
	case "Union":
		if len(elems) < 2 {
			return nil, errAt(expr, "(Union T1 T2 ...) requires at least 1 member type")
		}
		var members []Type
		for _, m := range elems[1:] {
			mt, err := c.ParseType(m, typeParams)
			if err != nil {
				return nil, err
			}
			members = append(members, mt)
		}
		return NewUnion(members...), nil
	case "Fn":
		if len(elems) != 3 {
			return nil, errAt(expr, "(Fn (P1 P2 ...) R) requires parameter list and return type")
		}
		pList := elems[1].GetList()
		if pList == nil {
			return nil, errAt(elems[1], "Fn parameter types must be a list")
		}
		var params []Type
		for _, pe := range pList.GetExpressions() {
			pt, err := c.ParseType(pe, typeParams)
			if err != nil {
				return nil, err
			}
			params = append(params, pt)
		}
		ret, err := c.ParseType(elems[2], typeParams)
		if err != nil {
			return nil, err
		}
		return &FnType{Params: params, Return: ret}, nil
	default:
		if alias, ok := c.Aliases[head]; ok {
			if len(elems)-1 != len(alias.TypeParams) {
				return nil, errAt(expr, "type alias %s expects %d type arguments, got %d", head, len(alias.TypeParams), len(elems)-1)
			}
			tpSet := map[string]bool{}
			subst := map[string]Type{}
			for i, tp := range alias.TypeParams {
				tpSet[tp] = true
				argT, err := c.ParseType(elems[i+1], typeParams)
				if err != nil {
					return nil, err
				}
				subst[tp] = argT
			}
			bodyT, err := c.ParseType(alias.Body, tpSet)
			if err != nil {
				return nil, err
			}
			return Substitute(bodyT, subst), nil
		}
		if sdef, ok := c.Structs[head]; ok {
			if len(elems)-1 != len(sdef.TypeParams) {
				return nil, errAt(expr, "struct %s expects %d type arguments, got %d", head, len(sdef.TypeParams), len(elems)-1)
			}
			var args []Type
			for _, ae := range elems[1:] {
				at, err := c.ParseType(ae, typeParams)
				if err != nil {
					return nil, err
				}
				args = append(args, at)
			}
			return &StructType{Def: sdef, TypeArgs: args}, nil
		}
		return nil, errAt(expr, "unknown type constructor %q", head)
	}
}

func (c *Checker) resolveProtoType(sym *taxlispv1.Symbol, expr *taxlispv1.Expression) (Type, error) {
	fullName := sym.GetName()
	if pkgAlias := sym.GetPackage(); pkgAlias != "" {
		if expanded, ok := c.ProtoAliases[pkgAlias]; ok {
			fullName = expanded + "." + sym.GetName()
		} else {
			fullName = pkgAlias + "." + sym.GetName()
		}
	}
	desc, err := c.Files.FindDescriptorByName(protoreflect.FullName(fullName))
	if err != nil {
		// Also try with "nexteggapp." prefix if omitted.
		if !strings.HasPrefix(fullName, "nexteggapp.") {
			desc, err = c.Files.FindDescriptorByName(protoreflect.FullName("nexteggapp." + fullName))
		}
	}
	if err != nil {
		return nil, errAt(expr, "unknown type %s (resolved as %s)", formatSym(sym), fullName)
	}
	switch d := desc.(type) {
	case protoreflect.MessageDescriptor:
		return &ProtoMessageType{Desc: d}, nil
	case protoreflect.EnumDescriptor:
		return &ProtoEnumType{Desc: d}, nil
	default:
		return nil, errAt(expr, "%s is not a protobuf message or enum", fullName)
	}
}

// CheckExpr typechecks an expression in lexical environment `env` and active
// generic type parameters `typeParams`.
func (c *Checker) CheckExpr(expr *taxlispv1.Expression, env map[string]Type, typeParams map[string]bool) (Type, error) {
	if a := expr.GetAtom(); a != nil {
		switch p := a.GetPrimitive().(type) {
		case *taxlispv1.Atom_IntValue:
			return TypeInt, nil
		case *taxlispv1.Atom_BoolValue:
			return TypeBool, nil
		case *taxlispv1.Atom_StringValue:
			return TypeString, nil
		case *taxlispv1.Atom_Money:
			return TypeMoney, nil
		case *taxlispv1.Atom_FloatValue:
			return TypeFloat, nil
		case *taxlispv1.Atom_Date:
			return TypeDate, nil
		case *taxlispv1.Atom_Keyword:
			return TypeSymbol, nil
		case *taxlispv1.Atom_Symbol:
			sym := p.Symbol
			if sym.GetPackage() == "" {
				if t, ok := env[sym.GetName()]; ok {
					return t, nil
				}
				if t, ok := c.Globals[sym.GetName()]; ok {
					return t, nil
				}
				return nil, errAt(expr, "unbound symbol %q", sym.GetName())
			}
			// Package-qualified enum value constant, e.g. fed/FILING_STATUS_SINGLE
			return c.resolveProtoEnumValue(sym, expr)
		}
	}

	list := expr.GetList()
	if list == nil || len(list.GetExpressions()) == 0 {
		return nil, errAt(expr, "cannot evaluate empty list ()")
	}
	elems := list.GetExpressions()
	head := symbolName(elems[0])

	switch head {
	case "use-proto", "deftype", "defstruct":
		return TypeNil, nil

	case "quote":
		if len(elems) != 2 {
			return nil, errAt(expr, "quote requires 1 argument")
		}
		if a := elems[1].GetAtom(); a != nil && a.GetSymbol() != nil && a.GetSymbol().GetPackage() != "" {
			if enumT, err := c.resolveProtoEnumValue(a.GetSymbol(), elems[1]); err == nil {
				return enumT, nil
			}
		}
		return TypeSymbol, nil

	case "=":
		// Top-level constant definition: (= name Type expr)
		if len(elems) != 4 {
			return nil, errAt(expr, "= requires explicit type annotation: (= name Type expr)")
		}
		name := symbolName(elems[1])
		if name == "" {
			return nil, errAt(elems[1], "= target must be a symbol")
		}
		declType, err := c.ParseType(elems[2], typeParams)
		if err != nil {
			return nil, err
		}
		valType, err := c.CheckExpr(elems[3], env, typeParams)
		if err != nil {
			return nil, err
		}
		if !IsAssignableTo(valType, declType) {
			return nil, errAt(elems[3], "constant %s: value has type %s, want %s", name, valType, declType)
		}
		c.Globals[name] = declType
		return declType, nil

	case "def":
		if len(elems) < 5 {
			return nil, errAt(expr, "def requires (def name ((arg Type)...) ReturnType body...)")
		}
		name, tparams, err := parseDefHeader(elems[1])
		if err != nil {
			return nil, err
		}
		tpSet := copySet(typeParams)
		for _, tp := range tparams {
			tpSet[tp] = true
		}
		paramNames, paramTypes, err := c.parseParamBindings(elems[2], tpSet)
		if err != nil {
			return nil, err
		}
		retType, err := c.ParseType(elems[3], tpSet)
		if err != nil {
			return nil, err
		}
		fnEnv := copyEnv(env)
		for i, pn := range paramNames {
			fnEnv[pn] = paramTypes[i]
		}
		var bodyType Type
		for i := 4; i < len(elems); i++ {
			// Skip keyword metadata pairs like :cite ("IRC § 63")
			if isKeywordAtom(elems[i]) {
				i++
				continue
			}
			bt, err := c.CheckExpr(elems[i], fnEnv, tpSet)
			if err != nil {
				return nil, err
			}
			bodyType = bt
		}
		if bodyType == nil || !IsAssignableTo(bodyType, retType) {
			return nil, errAt(expr, "function %s body has type %v, want %s", name, bodyType, retType)
		}
		fnT := &FnType{TypeParams: tparams, Params: paramTypes, Return: retType}
		c.Globals[name] = fnT
		return fnT, nil

	case "fn":
		if len(elems) != 4 {
			return nil, errAt(expr, "fn requires (fn ((arg Type)...) ReturnType body)")
		}
		paramNames, paramTypes, err := c.parseParamBindings(elems[1], typeParams)
		if err != nil {
			return nil, err
		}
		retType, err := c.ParseType(elems[2], typeParams)
		if err != nil {
			return nil, err
		}
		fnEnv := copyEnv(env)
		for i, pn := range paramNames {
			fnEnv[pn] = paramTypes[i]
		}
		bodyType, err := c.CheckExpr(elems[3], fnEnv, typeParams)
		if err != nil {
			return nil, err
		}
		if !IsAssignableTo(bodyType, retType) {
			return nil, errAt(elems[3], "fn body has type %s, want %s", bodyType, retType)
		}
		return &FnType{Params: paramTypes, Return: retType}, nil

	case "let":
		// Supports (let var val body) or (let var Type val body)
		if len(elems) != 4 && len(elems) != 5 {
			return nil, errAt(expr, "let requires (let var val body) or (let var Type val body)")
		}
		vname := symbolName(elems[1])
		if vname == "" {
			return nil, errAt(elems[1], "let binding variable must be a symbol")
		}
		var valExpr, bodyExpr *taxlispv1.Expression
		var declType Type
		if len(elems) == 5 {
			var err error
			declType, err = c.ParseType(elems[2], typeParams)
			if err != nil {
				return nil, err
			}
			valExpr = elems[3]
			bodyExpr = elems[4]
		} else {
			valExpr = elems[2]
			bodyExpr = elems[3]
		}
		vt, err := c.CheckExpr(valExpr, env, typeParams)
		if err != nil {
			return nil, err
		}
		if declType != nil {
			if !IsAssignableTo(vt, declType) {
				return nil, errAt(valExpr, "let %s: value has type %s, want %s", vname, vt, declType)
			}
			vt = declType
		}
		nextEnv := copyEnv(env)
		nextEnv[vname] = vt
		return c.CheckExpr(bodyExpr, nextEnv, typeParams)

	case "with", "withs":
		// Arc multi-binding: (withs (k1 v1 k2 v2 ...) body)
		if len(elems) != 3 {
			return nil, errAt(expr, "%s requires (%s (var1 val1 ...) body)", head, head)
		}
		bindList := elems[1].GetList()
		if bindList == nil || len(bindList.GetExpressions())%2 != 0 {
			return nil, errAt(elems[1], "%s bindings must be an even-length list of (var val ...) pairs", head)
		}
		bExprs := bindList.GetExpressions()
		nextEnv := copyEnv(env)
		evalEnv := env
		if head == "withs" {
			evalEnv = nextEnv
		}
		for i := 0; i < len(bExprs); i += 2 {
			vname := symbolName(bExprs[i])
			if vname == "" {
				return nil, errAt(bExprs[i], "%s variable must be a symbol", head)
			}
			vt, err := c.CheckExpr(bExprs[i+1], evalEnv, typeParams)
			if err != nil {
				return nil, err
			}
			nextEnv[vname] = vt
		}
		return c.CheckExpr(elems[2], nextEnv, typeParams)

	case "if":
		// Arc multi-branch if: (if c1 t1 c2 t2 ... elseExpr)
		if len(elems) < 4 || len(elems)%2 != 0 {
			return nil, errAt(expr, "if requires condition/then pairs and a final else expression: (if c1 t1 ... else)")
		}
		var branchTypes []Type
		curEnv := copyEnv(env)
		for i := 1; i < len(elems)-1; i += 2 {
			condExpr := elems[i]
			thenExpr := elems[i+1]
			ct, err := c.CheckExpr(condExpr, curEnv, typeParams)
			if err != nil {
				return nil, err
			}
			if !ct.Equals(TypeBool) {
				return nil, errAt(condExpr, "if condition must be Bool, got %s", ct)
			}
			thenEnv, elseEnv := c.narrowEnv(condExpr, curEnv, typeParams)
			tt, err := c.CheckExpr(thenExpr, thenEnv, typeParams)
			if err != nil {
				return nil, err
			}
			branchTypes = append(branchTypes, tt)
			curEnv = elseEnv
		}
		elseType, err := c.CheckExpr(elems[len(elems)-1], curEnv, typeParams)
		if err != nil {
			return nil, err
		}
		branchTypes = append(branchTypes, elseType)
		return NewUnion(branchTypes...), nil

	case "typecase":
		return c.checkTypecase(expr, elems, env, typeParams)

	case "isa":
		if len(elems) != 3 {
			return nil, errAt(expr, "(isa expr Type) requires 2 arguments")
		}
		if _, err := c.CheckExpr(elems[1], env, typeParams); err != nil {
			return nil, err
		}
		if _, err := c.ParseType(elems[2], typeParams); err != nil {
			return nil, err
		}
		return TypeBool, nil

	case "make-proto":
		return c.checkMakeProto(expr, elems, env, typeParams)

	case "make-struct":
		return c.checkMakeStruct(expr, elems, env, typeParams)

	case "proto-with":
		return c.checkProtoWith(expr, elems, env, typeParams)

	case ".":
		return c.checkFieldAccess(expr, elems, env, typeParams)

	case "has?":
		if len(elems) != 3 {
			return nil, errAt(expr, "(has? msg 'field) requires 2 arguments")
		}
		if _, err := c.checkFieldAccess(expr, elems, env, typeParams); err != nil {
			return nil, err
		}
		return TypeBool, nil

	case "which-oneof":
		if len(elems) != 3 {
			return nil, errAt(expr, "(which-oneof msg 'oneof_name) requires 2 arguments")
		}
		targetT, err := c.CheckExpr(elems[1], env, typeParams)
		if err != nil {
			return nil, err
		}
		pm, ok := targetT.(*ProtoMessageType)
		if !ok {
			return nil, errAt(elems[1], "which-oneof target must be a protobuf message, got %s", targetT)
		}
		oneofName, err := extractQuotedOrString(elems[2])
		if err != nil {
			return nil, errAt(elems[2], "which-oneof expects quoted oneof name: %v", err)
		}
		od := pm.Desc.Oneofs().ByName(protoreflect.Name(oneofName))
		if od == nil {
			return nil, errAt(elems[2], "message %s has no oneof %q", pm.Desc.FullName(), oneofName)
		}
		return NewUnion(TypeSymbol, TypeNil), nil

	case "slice":
		if len(elems) == 1 {
			return nil, errAt(expr, "empty (slice) requires (empty-slice Type)")
		}
		var elemTypes []Type
		for _, el := range elems[1:] {
			et, err := c.CheckExpr(el, env, typeParams)
			if err != nil {
				return nil, err
			}
			elemTypes = append(elemTypes, et)
		}
		return &SliceType{Elem: NewUnion(elemTypes...)}, nil

	case "table":
		if len(elems) < 3 || (len(elems)-1)%2 != 0 {
			return nil, errAt(expr, "(table k1 v1 k2 v2 ...) requires at least one key/value pair")
		}
		var kTypes, vTypes []Type
		for i := 1; i < len(elems); i += 2 {
			kt, err := c.CheckExpr(elems[i], env, typeParams)
			if err != nil {
				return nil, err
			}
			vt, err := c.CheckExpr(elems[i+1], env, typeParams)
			if err != nil {
				return nil, err
			}
			kTypes = append(kTypes, kt)
			vTypes = append(vTypes, vt)
		}
		return &TableType{Key: NewUnion(kTypes...), Value: NewUnion(vTypes...)}, nil

	case "inst":
		// Explicit generic instantiation: (inst fnOrGlobal Type1 Type2 ...)
		if len(elems) < 2 {
			return nil, errAt(expr, "(inst fn Type1 ...) requires a generic function and type arguments")
		}
		baseT, err := c.CheckExpr(elems[1], env, typeParams)
		if err != nil {
			return nil, err
		}
		fnT, ok := baseT.(*FnType)
		if !ok || len(fnT.TypeParams) != len(elems)-2 {
			return nil, errAt(expr, "inst target %s expects %d type arguments, got %d", baseT, len(fnT.TypeParams), len(elems)-2)
		}
		subst := map[string]Type{}
		for i, tp := range fnT.TypeParams {
			at, err := c.ParseType(elems[i+2], typeParams)
			if err != nil {
				return nil, err
			}
			subst[tp] = at
		}
		instParams := make([]Type, len(fnT.Params))
		for i, p := range fnT.Params {
			instParams[i] = Substitute(p, subst)
		}
		return &FnType{
			Params: instParams,
			Return: Substitute(fnT.Return, subst),
		}, nil

	case "+", "-", "*", "/", "min", "max":
		return c.checkArithmetic(expr, head, elems[1:], env, typeParams)

	case "<", "<=", ">", ">=":
		if len(elems) != 3 {
			return nil, errAt(expr, "%s requires 2 arguments", head)
		}
		lt, err := c.CheckExpr(elems[1], env, typeParams)
		if err != nil {
			return nil, err
		}
		rt, err := c.CheckExpr(elems[2], env, typeParams)
		if err != nil {
			return nil, err
		}
		if !lt.Equals(rt) || (!lt.Equals(TypeMoney) && !lt.Equals(TypeFloat) && !lt.Equals(TypeInt) && !lt.Equals(TypeDate)) {
			return nil, errAt(expr, "%s requires two matching Money, Float, Int, or Date arguments, got %s and %s", head, lt, rt)
		}
		return TypeBool, nil

	case "is", "!=":
		if len(elems) != 3 {
			return nil, errAt(expr, "%s requires 2 arguments", head)
		}
		if _, err := c.CheckExpr(elems[1], env, typeParams); err != nil {
			return nil, err
		}
		if _, err := c.CheckExpr(elems[2], env, typeParams); err != nil {
			return nil, err
		}
		return TypeBool, nil

	case "and", "or":
		if len(elems) < 2 {
			return nil, errAt(expr, "%s requires at least 1 Bool argument", head)
		}
		for _, arg := range elems[1:] {
			at, err := c.CheckExpr(arg, env, typeParams)
			if err != nil {
				return nil, err
			}
			if !at.Equals(TypeBool) {
				return nil, errAt(arg, "%s argument must be Bool, got %s", head, at)
			}
		}
		return TypeBool, nil

	default:
		return c.checkCall(expr, elems, env, typeParams)
	}
}

func (c *Checker) checkArithmetic(expr *taxlispv1.Expression, op string, args []*taxlispv1.Expression, env map[string]Type, typeParams map[string]bool) (Type, error) {
	if len(args) < 2 {
		return nil, errAt(expr, "%s requires at least 2 arguments", op)
	}
	argTypes := make([]Type, len(args))
	for i, a := range args {
		at, err := c.CheckExpr(a, env, typeParams)
		if err != nil {
			return nil, err
		}
		argTypes[i] = at
	}
	first := argTypes[0]
	switch op {
	case "+", "-", "min", "max":
		if !first.Equals(TypeMoney) && !first.Equals(TypeFloat) && !first.Equals(TypeInt) {
			return nil, errAt(expr, "%s not supported on %s", op, first)
		}
		for _, at := range argTypes[1:] {
			if !at.Equals(first) {
				return nil, errAt(expr, "%s requires all arguments to have type %s, got %s", op, first, at)
			}
		}
		return first, nil
	case "*":
		if len(args) != 2 {
			return nil, errAt(expr, "* requires 2 arguments")
		}
		a, b := argTypes[0], argTypes[1]
		switch {
		case a.Equals(TypeMoney) && (b.Equals(TypeFloat) || b.Equals(TypeInt)):
			return TypeMoney, nil
		case (a.Equals(TypeFloat) || a.Equals(TypeInt)) && b.Equals(TypeMoney):
			return TypeMoney, nil
		case a.Equals(TypeFloat) && (b.Equals(TypeFloat) || b.Equals(TypeInt)):
			return TypeFloat, nil
		case a.Equals(TypeInt) && b.Equals(TypeFloat):
			return TypeFloat, nil
		case a.Equals(TypeInt) && b.Equals(TypeInt):
			return TypeInt, nil
		default:
			return nil, errAt(expr, "* cannot multiply %s and %s", a, b)
		}
	case "/":
		if len(args) != 2 {
			return nil, errAt(expr, "/ requires 2 arguments")
		}
		a, b := argTypes[0], argTypes[1]
		switch {
		case a.Equals(TypeMoney) && b.Equals(TypeMoney):
			return TypeFloat, nil
		case a.Equals(TypeMoney) && (b.Equals(TypeFloat) || b.Equals(TypeInt)):
			return TypeMoney, nil
		case a.Equals(TypeFloat) && (b.Equals(TypeFloat) || b.Equals(TypeInt)):
			return TypeFloat, nil
		case a.Equals(TypeInt) && b.Equals(TypeInt):
			return TypeInt, nil
		default:
			return nil, errAt(expr, "/ cannot divide %s by %s", a, b)
		}
	}
	return nil, errAt(expr, "unsupported operator %s", op)
}

func (c *Checker) checkCall(expr *taxlispv1.Expression, elems []*taxlispv1.Expression, env map[string]Type, typeParams map[string]bool) (Type, error) {
	calleeT, err := c.CheckExpr(elems[0], env, typeParams)
	if err != nil {
		return nil, err
	}
	fnT, ok := calleeT.(*FnType)
	if !ok {
		return nil, errAt(elems[0], "cannot call non-function of type %s", calleeT)
	}
	args := elems[1:]
	if len(args) != len(fnT.Params) {
		return nil, errAt(expr, "function expects %d arguments, got %d", len(fnT.Params), len(args))
	}
	argTypes := make([]Type, len(args))
	for i, a := range args {
		at, err := c.CheckExpr(a, env, typeParams)
		if err != nil {
			return nil, err
		}
		argTypes[i] = at
	}
	if len(fnT.TypeParams) == 0 {
		for i, want := range fnT.Params {
			if !IsAssignableTo(argTypes[i], want) {
				return nil, errAt(args[i], "argument %d has type %s, want %s", i+1, argTypes[i], want)
			}
		}
		return fnT.Return, nil
	}
	tpSet := map[string]bool{}
	for _, tp := range fnT.TypeParams {
		tpSet[tp] = true
	}
	subst := map[string]Type{}
	for i, pat := range fnT.Params {
		if err := Unify(pat, argTypes[i], tpSet, subst); err != nil {
			return nil, errAt(args[i], "argument %d: %v", i+1, err)
		}
	}
	for _, tp := range fnT.TypeParams {
		if _, ok := subst[tp]; !ok {
			return nil, errAt(expr, "cannot infer type parameter %s; use (inst fn Type...)", tp)
		}
	}
	return Substitute(fnT.Return, subst), nil
}

func (c *Checker) checkTypecase(expr *taxlispv1.Expression, elems []*taxlispv1.Expression, env map[string]Type, typeParams map[string]bool) (Type, error) {
	// (typecase (var targetExpr) Type1 body1 Type2 body2 ...)
	if len(elems) < 4 || len(elems)%2 != 0 {
		return nil, errAt(expr, "typecase requires (typecase (var expr) Type1 body1 ...)")
	}
	bindList := elems[1].GetList()
	if bindList == nil || len(bindList.GetExpressions()) != 2 {
		return nil, errAt(elems[1], "typecase binding must be (var targetExpr)")
	}
	varName := symbolName(bindList.GetExpressions()[0])
	if varName == "" {
		return nil, errAt(bindList.GetExpressions()[0], "typecase variable must be a symbol")
	}
	targetT, err := c.CheckExpr(bindList.GetExpressions()[1], env, typeParams)
	if err != nil {
		return nil, err
	}
	var unionMembers []Type
	if u, ok := targetT.(*UnionType); ok {
		unionMembers = append(unionMembers, u.Members...)
	} else {
		unionMembers = []Type{targetT}
	}
	covered := make([]bool, len(unionMembers))
	hasElse := false
	var branchTypes []Type

	for i := 2; i < len(elems); i += 2 {
		patExpr := elems[i]
		bodyExpr := elems[i+1]
		if isKeywordNamed(patExpr, "else") {
			hasElse = true
			bt, err := c.CheckExpr(bodyExpr, env, typeParams)
			if err != nil {
				return nil, err
			}
			branchTypes = append(branchTypes, bt)
			continue
		}
		caseT, err := c.ParseType(patExpr, typeParams)
		if err != nil {
			return nil, err
		}
		matchedAny := false
		for idx, m := range unionMembers {
			if IsAssignableTo(m, caseT) {
				covered[idx] = true
				matchedAny = true
			}
		}
		if !matchedAny {
			return nil, errAt(patExpr, "typecase branch %s is unreachable for target type %s", caseT, targetT)
		}
		branchEnv := copyEnv(env)
		branchEnv[varName] = caseT
		bt, err := c.CheckExpr(bodyExpr, branchEnv, typeParams)
		if err != nil {
			return nil, err
		}
		branchTypes = append(branchTypes, bt)
	}
	if !hasElse {
		for idx, cov := range covered {
			if !cov {
				return nil, errAt(expr, "non-exhaustive typecase on %s: missing branch for %s", targetT, unionMembers[idx])
			}
		}
	}
	return NewUnion(branchTypes...), nil
}

func (c *Checker) narrowEnv(cond *taxlispv1.Expression, env map[string]Type, typeParams map[string]bool) (thenEnv, elseEnv map[string]Type) {
	thenEnv = copyEnv(env)
	elseEnv = copyEnv(env)
	list := cond.GetList()
	if list == nil || len(list.GetExpressions()) == 0 {
		return thenEnv, elseEnv
	}
	elems := list.GetExpressions()
	head := symbolName(elems[0])
	switch head {
	case "nil?":
		if len(elems) == 2 {
			vname := symbolName(elems[1])
			if vname != "" {
				if origT, ok := env[vname]; ok {
					thenEnv[vname] = TypeNil
					elseEnv[vname] = subtractType(origT, TypeNil)
				}
			}
		}
	case "isa":
		if len(elems) == 3 {
			vname := symbolName(elems[1])
			if vname != "" {
				if targetT, err := c.ParseType(elems[2], typeParams); err == nil {
					if origT, ok := env[vname]; ok {
						thenEnv[vname] = targetT
						elseEnv[vname] = subtractType(origT, targetT)
					}
				}
			}
		}
	case "not":
		if len(elems) == 2 {
			tSub, eSub := c.narrowEnv(elems[1], env, typeParams)
			return eSub, tSub
		}
	}
	return thenEnv, elseEnv
}

func subtractType(orig, remove Type) Type {
	u, ok := orig.(*UnionType)
	if !ok {
		return orig
	}
	var kept []Type
	for _, m := range u.Members {
		if !IsAssignableTo(m, remove) {
			kept = append(kept, m)
		}
	}
	if len(kept) == 0 {
		return orig
	}
	return NewUnion(kept...)
}

func (c *Checker) checkMakeProto(expr *taxlispv1.Expression, elems []*taxlispv1.Expression, env map[string]Type, typeParams map[string]bool) (Type, error) {
	if len(elems) < 2 || (len(elems)-2)%2 != 0 {
		return nil, errAt(expr, "(make-proto 'pkg/Type :field1 val1 ...) requires a message type and keyword/value pairs")
	}
	sym, err := extractTypeSymbol(elems[1])
	if err != nil {
		return nil, errAt(elems[1], "make-proto: %v", err)
	}
	resolved, err := c.resolveProtoType(sym, elems[1])
	if err != nil {
		return nil, err
	}
	pm, ok := resolved.(*ProtoMessageType)
	if !ok {
		return nil, errAt(elems[1], "make-proto target %s is not a protobuf message", resolved)
	}
	for i := 2; i < len(elems); i += 2 {
		kw := elems[i].GetAtom().GetKeyword()
		if kw == "" {
			return nil, errAt(elems[i], "make-proto field name must be a keyword (:field_name)")
		}
		fd := findProtoField(pm.Desc, kw)
		if fd == nil {
			return nil, errAt(elems[i], "protobuf message %s has no field %q", pm.Desc.FullName(), kw)
		}
		wantFieldT := c.protoFieldType(fd, false)
		gotValT, err := c.CheckExpr(elems[i+1], env, typeParams)
		if err != nil {
			return nil, err
		}
		// Allow setting a field to (Union FieldType Nil) so optional fields can
		// be conditionally populated without boilerplate.
		allowedT := NewUnion(wantFieldT, TypeNil)
		if !IsAssignableTo(gotValT, allowedT) {
			return nil, errAt(elems[i+1], "field :%s on %s expects %s, got %s", kw, pm.Desc.FullName(), wantFieldT, gotValT)
		}
	}
	return pm, nil
}

func (c *Checker) checkProtoWith(expr *taxlispv1.Expression, elems []*taxlispv1.Expression, env map[string]Type, typeParams map[string]bool) (Type, error) {
	if len(elems) < 4 || (len(elems)-2)%2 != 0 {
		return nil, errAt(expr, "(proto-with msg :field1 val1 ...) requires a message and keyword/value pairs")
	}
	baseT, err := c.CheckExpr(elems[1], env, typeParams)
	if err != nil {
		return nil, err
	}
	pm, ok := baseT.(*ProtoMessageType)
	if !ok {
		return nil, errAt(elems[1], "proto-with target must be a protobuf message, got %s", baseT)
	}
	for i := 2; i < len(elems); i += 2 {
		kw := elems[i].GetAtom().GetKeyword()
		if kw == "" {
			return nil, errAt(elems[i], "proto-with field name must be a keyword (:field_name)")
		}
		fd := findProtoField(pm.Desc, kw)
		if fd == nil {
			return nil, errAt(elems[i], "protobuf message %s has no field %q", pm.Desc.FullName(), kw)
		}
		wantFieldT := c.protoFieldType(fd, false)
		gotValT, err := c.CheckExpr(elems[i+1], env, typeParams)
		if err != nil {
			return nil, err
		}
		if !IsAssignableTo(gotValT, NewUnion(wantFieldT, TypeNil)) {
			return nil, errAt(elems[i+1], "field :%s on %s expects %s, got %s", kw, pm.Desc.FullName(), wantFieldT, gotValT)
		}
	}
	return pm, nil
}

func (c *Checker) checkMakeStruct(expr *taxlispv1.Expression, elems []*taxlispv1.Expression, env map[string]Type, typeParams map[string]bool) (Type, error) {
	if len(elems) < 2 || (len(elems)-2)%2 != 0 {
		return nil, errAt(expr, "(make-struct StructType :field1 val1 ...) requires a struct type and keyword/value pairs")
	}
	st, err := c.ParseType(elems[1], typeParams)
	if err != nil {
		return nil, err
	}
	structT, ok := st.(*StructType)
	if !ok {
		return nil, errAt(elems[1], "make-struct target %s is not a defstruct type", st)
	}
	seen := map[string]bool{}
	for i := 2; i < len(elems); i += 2 {
		kw := elems[i].GetAtom().GetKeyword()
		if kw == "" {
			return nil, errAt(elems[i], "make-struct field name must be a keyword (:field)")
		}
		wantT, ok := structT.FieldType(kw)
		if !ok {
			return nil, errAt(elems[i], "struct %s has no field :%s", structT, kw)
		}
		gotT, err := c.CheckExpr(elems[i+1], env, typeParams)
		if err != nil {
			return nil, err
		}
		if !IsAssignableTo(gotT, wantT) {
			return nil, errAt(elems[i+1], "struct %s field :%s expects %s, got %s", structT, kw, wantT, gotT)
		}
		seen[kw] = true
	}
	for _, f := range structT.Def.Fields {
		if !seen[f.Name] {
			return nil, errAt(expr, "make-struct %s missing required field :%s", structT, f.Name)
		}
	}
	return structT, nil
}

func (c *Checker) checkFieldAccess(expr *taxlispv1.Expression, elems []*taxlispv1.Expression, env map[string]Type, typeParams map[string]bool) (Type, error) {
	if len(elems) != 3 {
		return nil, errAt(expr, "(. target 'field.path) requires 2 arguments")
	}
	targetT, err := c.CheckExpr(elems[1], env, typeParams)
	if err != nil {
		return nil, err
	}
	pathStr, err := extractFieldPath(elems[2])
	if err != nil {
		return nil, errAt(elems[2], "field access: %v", err)
	}
	segments := strings.Split(pathStr, ".")
	curT := targetT
	for _, seg := range segments {
		switch ct := curT.(type) {
		case *StructType:
			ft, ok := ct.FieldType(seg)
			if !ok {
				return nil, errAt(elems[2], "struct %s has no field %q", ct, seg)
			}
			curT = ft
		case *ProtoMessageType:
			// Check if seg names a synthetic or real oneof on the message.
			if od := ct.Desc.Oneofs().ByName(protoreflect.Name(seg)); od != nil && !od.IsSynthetic() {
				var members []Type
				for i := 0; i < od.Fields().Len(); i++ {
					members = append(members, c.protoFieldType(od.Fields().Get(i), false))
				}
				members = append(members, TypeNil)
				curT = NewUnion(members...)
				continue
			}
			fd := findProtoField(ct.Desc, seg)
			if fd == nil {
				return nil, errAt(elems[2], "protobuf message %s has no field %q", ct.Desc.FullName(), seg)
			}
			curT = c.protoFieldType(fd, true)
		default:
			return nil, errAt(elems[1], "cannot access field %q on value of type %s", seg, curT)
		}
	}
	return curT, nil
}

func (c *Checker) protoFieldType(fd protoreflect.FieldDescriptor, isRead bool) Type {
	elemT := c.protoScalarOrMessageType(fd)
	if fd.IsList() {
		return &SliceType{Elem: elemT}
	}
	if fd.IsMap() {
		kt := c.protoScalarOrMessageType(fd.MapKey())
		vt := c.protoScalarOrMessageType(fd.MapValue())
		return &TableType{Key: kt, Value: vt}
	}
	// If the field is inside a real (non-synthetic) oneof, reading it by field
	// name may be unset, so it has type (Union T Nil) on read? Or default?
	// Note: for proto3 messages, reading a sub-message on a tax form returns a
	// zero message by default so chained paths like (. in 'federal.documents.w2s)
	// work without nil-checking every intermediate message!
	_ = isRead
	return elemT
}

func (c *Checker) protoScalarOrMessageType(fd protoreflect.FieldDescriptor) Type {
	switch fd.Kind() {
	case protoreflect.BoolKind:
		return TypeBool
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind,
		protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind,
		protoreflect.Uint32Kind, protoreflect.Fixed32Kind, protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return TypeInt
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		return TypeFloat
	case protoreflect.StringKind:
		return TypeString
	case protoreflect.EnumKind:
		return &ProtoEnumType{Desc: fd.Enum()}
	case protoreflect.MessageKind, protoreflect.GroupKind:
		md := fd.Message()
		switch md.FullName() {
		case "nexteggapp.taxprotos.common.v1.Money", "taxprotos.common.v1.Money":
			return TypeMoney
		case "nexteggapp.taxprotos.common.v1.Quantity", "taxprotos.common.v1.Quantity":
			return TypeFloat
		case "nexteggapp.taxprotos.common.v1.Date", "taxprotos.common.v1.Date":
			return TypeDate
		default:
			return &ProtoMessageType{Desc: md}
		}
	default:
		return TypeString
	}
}

func (c *Checker) resolveProtoEnumValue(sym *taxlispv1.Symbol, expr *taxlispv1.Expression) (Type, error) {
	pkgPrefix := sym.GetPackage()
	if expanded, ok := c.ProtoAliases[pkgPrefix]; ok {
		pkgPrefix = expanded
	}
	valName := protoreflect.Name(sym.GetName())
	var found protoreflect.EnumDescriptor
	c.Files.RangeFilesByPackage(protoreflect.FullName(pkgPrefix), func(fd protoreflect.FileDescriptor) bool {
		found = findEnumWithValue(fd.Enums(), valName)
		if found != nil {
			return false
		}
		found = findEnumInMessages(fd.Messages(), valName)
		return found == nil
	})
	if found == nil && !strings.HasPrefix(pkgPrefix, "nexteggapp.") {
		c.Files.RangeFilesByPackage(protoreflect.FullName("nexteggapp."+pkgPrefix), func(fd protoreflect.FileDescriptor) bool {
			found = findEnumWithValue(fd.Enums(), valName)
			if found != nil {
				return false
			}
			found = findEnumInMessages(fd.Messages(), valName)
			return found == nil
		})
	}
	if found == nil {
		return nil, errAt(expr, "unknown enum value %s/%s", sym.GetPackage(), sym.GetName())
	}
	return &ProtoEnumType{Desc: found}, nil
}

func findEnumWithValue(es protoreflect.EnumDescriptors, valName protoreflect.Name) protoreflect.EnumDescriptor {
	for i := 0; i < es.Len(); i++ {
		ed := es.Get(i)
		if ed.Values().ByName(valName) != nil {
			return ed
		}
	}
	return nil
}

func findEnumInMessages(ms protoreflect.MessageDescriptors, valName protoreflect.Name) protoreflect.EnumDescriptor {
	for i := 0; i < ms.Len(); i++ {
		md := ms.Get(i)
		if ed := findEnumWithValue(md.Enums(), valName); ed != nil {
			return ed
		}
		if ed := findEnumInMessages(md.Messages(), valName); ed != nil {
			return ed
		}
	}
	return nil
}

func findProtoField(md protoreflect.MessageDescriptor, name string) protoreflect.FieldDescriptor {
	if fd := md.Fields().ByName(protoreflect.Name(name)); fd != nil {
		return fd
	}
	if fd := md.Fields().ByJSONName(name); fd != nil {
		return fd
	}
	return nil
}

func (c *Checker) parseParamBindings(expr *taxlispv1.Expression, typeParams map[string]bool) ([]string, []Type, error) {
	list := expr.GetList()
	if list == nil {
		return nil, nil, errAt(expr, "parameter list must be a list of ((name Type) ...)")
	}
	var names []string
	var types []Type
	for _, item := range list.GetExpressions() {
		pair := item.GetList()
		if pair == nil || len(pair.GetExpressions()) != 2 {
			return nil, nil, errAt(item, "each parameter must be (name Type) with mandatory type annotation")
		}
		pname := symbolName(pair.GetExpressions()[0])
		if pname == "" {
			return nil, nil, errAt(pair.GetExpressions()[0], "parameter name must be a symbol")
		}
		ptype, err := c.ParseType(pair.GetExpressions()[1], typeParams)
		if err != nil {
			return nil, nil, err
		}
		names = append(names, pname)
		types = append(types, ptype)
	}
	return names, types, nil
}

func isTypeParamList(l *taxlispv1.List) bool {
	if len(l.GetExpressions()) == 0 {
		return true
	}
	for _, e := range l.GetExpressions() {
		if symbolName(e) == "" {
			return false
		}
	}
	// If it has 2 symbols, check whether the second symbol is a known type or primitive—if all elements are single uppercase letters or type params, treat as type param list.
	for _, e := range l.GetExpressions() {
		s := symbolName(e)
		if len(s) > 0 && s[0] >= 'a' && s[0] <= 'z' {
			return false
		}
	}
	return true
}

func parseDefHeader(expr *taxlispv1.Expression) (string, []string, error) {
	if s := symbolName(expr); s != "" {
		return s, nil, nil
	}
	list := expr.GetList()
	if list == nil || len(list.GetExpressions()) == 0 {
		return "", nil, errAt(expr, "definition name must be a symbol or (name T U ...)")
	}
	elems := list.GetExpressions()
	name := symbolName(elems[0])
	if name == "" {
		return "", nil, errAt(elems[0], "definition name must be a symbol")
	}
	var tparams []string
	for _, tp := range elems[1:] {
		tps := symbolName(tp)
		if tps == "" {
			return "", nil, errAt(tp, "type parameter must be a symbol")
		}
		tparams = append(tparams, tps)
	}
	return name, tparams, nil
}

func extractQuotedOrString(expr *taxlispv1.Expression) (string, error) {
	if a := expr.GetAtom(); a != nil {
		if a.GetStringValue() != "" {
			return a.GetStringValue(), nil
		}
		if kw := a.GetKeyword(); kw != "" {
			return kw, nil
		}
		if sym := a.GetSymbol(); sym != nil {
			return formatSym(sym), nil
		}
	}
	if l := expr.GetList(); l != nil && len(l.GetExpressions()) == 2 && symbolName(l.GetExpressions()[0]) == "quote" {
		return extractQuotedOrString(l.GetExpressions()[1])
	}
	return "", fmt.Errorf("expected symbol or string")
}

func extractTypeSymbol(expr *taxlispv1.Expression) (*taxlispv1.Symbol, error) {
	if a := expr.GetAtom(); a != nil {
		if sym := a.GetSymbol(); sym != nil {
			return sym, nil
		}
		if s := a.GetStringValue(); s != "" {
			if idx := strings.LastIndexByte(s, '/'); idx > 0 {
				return &taxlispv1.Symbol{Package: s[:idx], Name: s[idx+1:]}, nil
			}
			return &taxlispv1.Symbol{Name: s}, nil
		}
	}
	if l := expr.GetList(); l != nil && len(l.GetExpressions()) == 2 && symbolName(l.GetExpressions()[0]) == "quote" {
		return extractTypeSymbol(l.GetExpressions()[1])
	}
	return nil, fmt.Errorf("expected quoted symbol or string naming a protobuf message type")
}

func extractFieldPath(expr *taxlispv1.Expression) (string, error) {
	return extractQuotedOrString(expr)
}

func symbolName(expr *taxlispv1.Expression) string {
	a := expr.GetAtom()
	if a == nil || a.GetSymbol() == nil || a.GetSymbol().GetPackage() != "" {
		return ""
	}
	return a.GetSymbol().GetName()
}

func isKeywordAtom(expr *taxlispv1.Expression) bool {
	a := expr.GetAtom()
	return a != nil && a.GetKeyword() != ""
}

func isKeywordNamed(expr *taxlispv1.Expression, kw string) bool {
	a := expr.GetAtom()
	return a != nil && a.GetKeyword() == kw
}

func formatSym(sym *taxlispv1.Symbol) string {
	if sym.GetPackage() == "" {
		return sym.GetName()
	}
	return sym.GetPackage() + "/" + sym.GetName()
}

func copyEnv(env map[string]Type) map[string]Type {
	out := make(map[string]Type, len(env))
	for k, v := range env {
		out[k] = v
	}
	return out
}

func copySet(s map[string]bool) map[string]bool {
	out := make(map[string]bool, len(s))
	for k, v := range s {
		out[k] = v
	}
	return out
}

func errAt(expr *taxlispv1.Expression, format string, args ...any) error {
	var syn *taxlispv1.SyntaxDetails
	if a := expr.GetAtom(); a != nil {
		syn = a.GetSyntax()
	} else if l := expr.GetList(); l != nil {
		syn = l.GetSyntax()
	}
	msg := fmt.Sprintf(format, args...)
	if syn != nil && syn.GetFile() != "" {
		return fmt.Errorf("%s:%d:%d: %s", syn.GetFile(), syn.GetStartLine(), syn.GetStartColumn(), msg)
	}
	return fmt.Errorf("%s", msg)
}
