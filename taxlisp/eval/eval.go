// Package eval implements the runtime evaluator for typechecked taxlisp modules.
package eval

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"

	taxlispv1 "github.com/nexteggapp/taxcalcs/taxlisp/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"
)

const (
	protoMoneyFullName    = protoreflect.FullName("nexteggapp.taxprotos.common.v1.Money")
	protoQuantityFullName = protoreflect.FullName("nexteggapp.taxprotos.common.v1.Quantity")
	protoDateFullName     = protoreflect.FullName("nexteggapp.taxprotos.common.v1.Date")
)

// Value is a runtime value in taxlisp.
type Value interface {
	String() string
	isValue()
}

// NilVal represents the singleton `nil` value of type `Nil`.
type NilVal struct{}

// Nil is the singleton NilVal instance.
var Nil Value = NilVal{}

func (NilVal) String() string { return "nil" }
func (NilVal) isValue()       {}

// BoolVal is a boolean runtime value.
type BoolVal struct{ V bool }

func (b BoolVal) String() string {
	if b.V {
		return "true"
	}
	return "false"
}
func (BoolVal) isValue() {}

// IntVal is a 64-bit signed integer runtime value.
type IntVal struct{ V int64 }

func (i IntVal) String() string { return fmt.Sprintf("%d", i.V) }
func (IntVal) isValue()         {}

// StringVal is a UTF-8 string runtime value.
type StringVal struct{ V string }

func (s StringVal) String() string { return fmt.Sprintf("%q", s.V) }
func (StringVal) isValue()         {}

// SymbolVal is a quoted symbol runtime value (including enum literals).
type SymbolVal struct {
	Package string
	Name    string
}

func (s SymbolVal) FullName() string {
	if s.Package == "" {
		return s.Name
	}
	return s.Package + "/" + s.Name
}
func (s SymbolVal) String() string { return "'" + s.FullName() }
func (SymbolVal) isValue()         {}

// MoneyVal is an exact decimal USD monetary amount backed by *big.Rat.
type MoneyVal struct {
	R *big.Rat
}

// NewMoneyRat creates a MoneyVal from a *big.Rat (cloning it).
func NewMoneyRat(r *big.Rat) MoneyVal {
	c := new(big.Rat)
	if r != nil {
		c.Set(r)
	}
	return MoneyVal{R: c}
}

// NewMoneyCents creates a MoneyVal from integer cents.
func NewMoneyCents(cents int64) MoneyVal {
	return MoneyVal{R: big.NewRat(cents, 100)}
}

// ParseMoney parses a decimal string into a MoneyVal.
func ParseMoney(s string) (MoneyVal, error) {
	r, ok := new(big.Rat).SetString(strings.TrimSpace(s))
	if !ok {
		return MoneyVal{}, fmt.Errorf("invalid money literal %q", s)
	}
	return MoneyVal{R: r}, nil
}

// Cents returns the amount rounded to the nearest cent (half-away-from-zero).
func (m MoneyVal) Cents() int64 {
	rounded := roundRatScale(m.R, 100)
	num := new(big.Int).Mul(rounded.Num(), big.NewInt(100))
	num.Quo(num, rounded.Denom())
	return num.Int64()
}

// Dollars returns the amount rounded to the nearest whole dollar (half-away-from-zero).
func (m MoneyVal) Dollars() int64 {
	rounded := roundRatScale(m.R, 1)
	return new(big.Int).Quo(rounded.Num(), rounded.Denom()).Int64()
}

// FormatCents formats the MoneyVal to 2 decimal places (e.g., "1250.50").
func (m MoneyVal) FormatCents() string {
	return formatRatFixed(m.R, 2)
}

func (m MoneyVal) String() string {
	return fmt.Sprintf("#usd%q", m.FormatCents())
}
func (MoneyVal) isValue() {}

// FloatVal is a 64-bit IEEE-754 floating-point runtime value.
type FloatVal struct {
	V float64
}

func (f FloatVal) String() string {
	s := strconv.FormatFloat(f.V, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}
func (FloatVal) isValue() {}

// DateVal is a calendar date value.
type DateVal struct {
	Year  int32
	Month int32
	Day   int32
}

func (d DateVal) String() string {
	return fmt.Sprintf("#date\"%04d-%02d-%02d\"", d.Year, d.Month, d.Day)
}
func (DateVal) isValue() {}

func compareDate(a, b DateVal) int {
	if a.Year != b.Year {
		if a.Year < b.Year {
			return -1
		}
		return 1
	}
	if a.Month != b.Month {
		if a.Month < b.Month {
			return -1
		}
		return 1
	}
	if a.Day != b.Day {
		if a.Day < b.Day {
			return -1
		}
		return 1
	}
	return 0
}

// SliceVal is an immutable contiguous slice of values.
type SliceVal struct {
	Elems []Value
}

func (s SliceVal) String() string {
	parts := make([]string, len(s.Elems))
	for i, e := range s.Elems {
		parts[i] = e.String()
	}
	return "[" + strings.Join(parts, " ") + "]"
}
func (SliceVal) isValue() {}

// TableEntry is a key-value pair in a TableVal.
type TableEntry struct {
	Key   Value
	Value Value
}

// TableVal is an immutable lookup table.
type TableVal struct {
	Entries []TableEntry
}

func (t TableVal) String() string {
	parts := make([]string, 0, len(t.Entries)*2+1)
	parts = append(parts, "table")
	for _, e := range t.Entries {
		parts = append(parts, e.Key.String(), e.Value.String())
	}
	return "(" + strings.Join(parts, " ") + ")"
}
func (TableVal) isValue() {}

// StructVal is an instance of a user-defined `defstruct`.
type StructVal struct {
	TypeName string
	Fields   map[string]Value
}

func (s StructVal) String() string {
	return fmt.Sprintf("<%s %v>", s.TypeName, s.Fields)
}
func (StructVal) isValue() {}

// ProtoMessageVal wraps a protobuf message.
type ProtoMessageVal struct {
	Msg protoreflect.Message
}

// Proto returns the underlying proto.Message.
func (p ProtoMessageVal) Proto() proto.Message {
	return p.Msg.Interface()
}

func (p ProtoMessageVal) String() string {
	return fmt.Sprintf("<%s>", p.Msg.Descriptor().FullName())
}
func (ProtoMessageVal) isValue() {}

// ProtoEnumVal wraps a protobuf enum value.
type ProtoEnumVal struct {
	Desc   protoreflect.EnumDescriptor
	Number protoreflect.EnumNumber
	Name   string
}

func (e ProtoEnumVal) String() string {
	return fmt.Sprintf("'%s/%s", e.Desc.FullName().Parent(), e.Name)
}
func (ProtoEnumVal) isValue() {}

// ClosureVal is a lexical closure created by `fn` or `def`.
type ClosureVal struct {
	Name       string
	ParamNames []string
	Body       *taxlispv1.Expression
	Env        *Env
}

func (c ClosureVal) String() string {
	if c.Name != "" {
		return fmt.Sprintf("<fn %s>", c.Name)
	}
	return "<fn>"
}
func (ClosureVal) isValue() {}

// StructCtorVal constructs a user-defined `defstruct` from positional arguments.
type StructCtorVal struct {
	Name       string
	FieldNames []string
}

func (s StructCtorVal) String() string { return fmt.Sprintf("<struct-ctor %s>", s.Name) }
func (StructCtorVal) isValue()         {}

// BuiltinVal is a built-in function.
type BuiltinVal struct {
	Name string
	Fn   func(ev *Evaluator, args []Value, expr *taxlispv1.Expression) (Value, error)
}

func (b BuiltinVal) String() string { return fmt.Sprintf("<builtin %s>", b.Name) }
func (BuiltinVal) isValue()         {}

// Env is a lexical environment frame.
type Env struct {
	Parent *Env
	Vars   map[string]Value
}

// NewEnv creates a child environment frame.
func NewEnv(parent *Env) *Env {
	return &Env{
		Parent: parent,
		Vars:   map[string]Value{},
	}
}

// Get looks up a variable in the lexical chain.
func (e *Env) Get(name string) (Value, bool) {
	for cur := e; cur != nil; cur = cur.Parent {
		if v, ok := cur.Vars[name]; ok {
			return v, true
		}
	}
	return nil, false
}

// Set binds a variable in the current environment frame.
func (e *Env) Set(name string, v Value) {
	e.Vars[name] = v
}

// Evaluator executes taxlisp modules and expressions.
type Evaluator struct {
	Files        *protoregistry.Files
	Types        *protoregistry.Types
	ProtoAliases map[string]string
	Globals      *Env
}

// NewEvaluator creates a new Evaluator initialized with all built-in functions.
func NewEvaluator(files *protoregistry.Files, types *protoregistry.Types) *Evaluator {
	if files == nil {
		files = protoregistry.GlobalFiles
	}
	if types == nil {
		types = protoregistry.GlobalTypes
	}
	ev := &Evaluator{
		Files:        files,
		Types:        types,
		ProtoAliases: map[string]string{},
		Globals:      NewEnv(nil),
	}
	ev.registerBuiltins()
	return ev
}

func (ev *Evaluator) registerBuiltins() {
	ev.Globals.Set("nil", Nil)
	builtins := []BuiltinVal{
		{
			Name: "not",
			Fn: func(_ *Evaluator, args []Value, expr *taxlispv1.Expression) (Value, error) {
				b, ok := args[0].(BoolVal)
				if !ok {
					return nil, errAt(expr, "not expects Bool, got %T", args[0])
				}
				return BoolVal{V: !b.V}, nil
			},
		},
		{
			Name: "nil?",
			Fn: func(_ *Evaluator, args []Value, _ *taxlispv1.Expression) (Value, error) {
				_, isNil := args[0].(NilVal)
				return BoolVal{V: isNil}, nil
			},
		},
		{
			Name: "empty-slice",
			Fn: func(_ *Evaluator, _ []Value, _ *taxlispv1.Expression) (Value, error) {
				return SliceVal{Elems: nil}, nil
			},
		},
		{
			Name: "len",
			Fn: func(_ *Evaluator, args []Value, expr *taxlispv1.Expression) (Value, error) {
				s, ok := args[0].(SliceVal)
				if !ok {
					return nil, errAt(expr, "len expects Slice, got %T", args[0])
				}
				return IntVal{V: int64(len(s.Elems))}, nil
			},
		},
		{
			Name: "empty?",
			Fn: func(_ *Evaluator, args []Value, expr *taxlispv1.Expression) (Value, error) {
				s, ok := args[0].(SliceVal)
				if !ok {
					return nil, errAt(expr, "empty? expects Slice, got %T", args[0])
				}
				return BoolVal{V: len(s.Elems) == 0}, nil
			},
		},
		{
			Name: "elt",
			Fn: func(_ *Evaluator, args []Value, expr *taxlispv1.Expression) (Value, error) {
				s, ok := args[0].(SliceVal)
				if !ok {
					return nil, errAt(expr, "elt expects Slice, got %T", args[0])
				}
				idx, ok := args[1].(IntVal)
				if !ok {
					return nil, errAt(expr, "elt expects Int index, got %T", args[1])
				}
				if idx.V < 0 || int(idx.V) >= len(s.Elems) {
					return nil, errAt(expr, "elt index %d out of bounds (len %d)", idx.V, len(s.Elems))
				}
				return s.Elems[idx.V], nil
			},
		},
		{
			Name: "subslice",
			Fn: func(_ *Evaluator, args []Value, expr *taxlispv1.Expression) (Value, error) {
				s, ok := args[0].(SliceVal)
				if !ok {
					return nil, errAt(expr, "subslice expects Slice, got %T", args[0])
				}
				start, ok1 := args[1].(IntVal)
				end, ok2 := args[2].(IntVal)
				if !ok1 || !ok2 {
					return nil, errAt(expr, "subslice expects Int bounds")
				}
				lo, hi := int(start.V), int(end.V)
				if lo < 0 {
					lo = 0
				}
				if hi > len(s.Elems) {
					hi = len(s.Elems)
				}
				if lo > hi {
					lo = hi
				}
				out := make([]Value, hi-lo)
				copy(out, s.Elems[lo:hi])
				return SliceVal{Elems: out}, nil
			},
		},
		{
			Name: "append",
			Fn: func(_ *Evaluator, args []Value, expr *taxlispv1.Expression) (Value, error) {
				s1, ok1 := args[0].(SliceVal)
				s2, ok2 := args[1].(SliceVal)
				if !ok1 || !ok2 {
					return nil, errAt(expr, "append expects two Slices")
				}
				out := make([]Value, 0, len(s1.Elems)+len(s2.Elems))
				out = append(out, s1.Elems...)
				out = append(out, s2.Elems...)
				return SliceVal{Elems: out}, nil
			},
		},
		{
			Name: "map",
			Fn: func(ev *Evaluator, args []Value, expr *taxlispv1.Expression) (Value, error) {
				fn := args[0]
				s, ok := args[1].(SliceVal)
				if !ok {
					return nil, errAt(expr, "map expects Slice as second argument, got %T", args[1])
				}
				out := make([]Value, len(s.Elems))
				for i, elem := range s.Elems {
					v, err := ev.Apply(fn, []Value{elem}, expr)
					if err != nil {
						return nil, err
					}
					out[i] = v
				}
				return SliceVal{Elems: out}, nil
			},
		},
		{
			Name: "keep",
			Fn: func(ev *Evaluator, args []Value, expr *taxlispv1.Expression) (Value, error) {
				fn := args[0]
				s, ok := args[1].(SliceVal)
				if !ok {
					return nil, errAt(expr, "keep expects Slice as second argument, got %T", args[1])
				}
				var out []Value
				for _, elem := range s.Elems {
					v, err := ev.Apply(fn, []Value{elem}, expr)
					if err != nil {
						return nil, err
					}
					b, ok := v.(BoolVal)
					if !ok {
						return nil, errAt(expr, "keep predicate must return Bool, got %T", v)
					}
					if b.V {
						out = append(out, elem)
					}
				}
				return SliceVal{Elems: out}, nil
			},
		},
		{
			Name: "fold",
			Fn: func(ev *Evaluator, args []Value, expr *taxlispv1.Expression) (Value, error) {
				fn := args[0]
				acc := args[1]
				s, ok := args[2].(SliceVal)
				if !ok {
					return nil, errAt(expr, "fold expects Slice as third argument, got %T", args[2])
				}
				var err error
				for _, elem := range s.Elems {
					acc, err = ev.Apply(fn, []Value{acc, elem}, expr)
					if err != nil {
						return nil, err
					}
				}
				return acc, nil
			},
		},
		{
			Name: "some",
			Fn: func(ev *Evaluator, args []Value, expr *taxlispv1.Expression) (Value, error) {
				fn := args[0]
				s, ok := args[1].(SliceVal)
				if !ok {
					return nil, errAt(expr, "some expects Slice as second argument, got %T", args[1])
				}
				for _, elem := range s.Elems {
					v, err := ev.Apply(fn, []Value{elem}, expr)
					if err != nil {
						return nil, err
					}
					if b, ok := v.(BoolVal); ok && b.V {
						return BoolVal{V: true}, nil
					}
				}
				return BoolVal{V: false}, nil
			},
		},
		{
			Name: "all",
			Fn: func(ev *Evaluator, args []Value, expr *taxlispv1.Expression) (Value, error) {
				fn := args[0]
				s, ok := args[1].(SliceVal)
				if !ok {
					return nil, errAt(expr, "all expects Slice as second argument, got %T", args[1])
				}
				for _, elem := range s.Elems {
					v, err := ev.Apply(fn, []Value{elem}, expr)
					if err != nil {
						return nil, err
					}
					if b, ok := v.(BoolVal); !ok || !b.V {
						return BoolVal{V: false}, nil
					}
				}
				return BoolVal{V: true}, nil
			},
		},
		{
			Name: "sum-money",
			Fn: func(_ *Evaluator, args []Value, expr *taxlispv1.Expression) (Value, error) {
				s, ok := args[0].(SliceVal)
				if !ok {
					return nil, errAt(expr, "sum-money expects Slice, got %T", args[0])
				}
				total := big.NewRat(0, 1)
				for _, elem := range s.Elems {
					m, ok := elem.(MoneyVal)
					if !ok {
						return nil, errAt(expr, "sum-money element must be Money, got %T", elem)
					}
					total.Add(total, m.R)
				}
				return MoneyVal{R: total}, nil
			},
		},
		{
			Name: "sum",
			Fn: func(ev *Evaluator, args []Value, expr *taxlispv1.Expression) (Value, error) {
				fn := args[0]
				s, ok := args[1].(SliceVal)
				if !ok {
					return nil, errAt(expr, "sum expects Slice as second argument, got %T", args[1])
				}
				total := big.NewRat(0, 1)
				for _, elem := range s.Elems {
					v, err := ev.Apply(fn, []Value{elem}, expr)
					if err != nil {
						return nil, err
					}
					m, ok := v.(MoneyVal)
					if !ok {
						return nil, errAt(expr, "sum function must return Money, got %T", v)
					}
					total.Add(total, m.R)
				}
				return MoneyVal{R: total}, nil
			},
		},
		{
			Name: "lookup",
			Fn: func(ev *Evaluator, args []Value, expr *taxlispv1.Expression) (Value, error) {
				tbl, ok := args[0].(TableVal)
				if !ok {
					return nil, errAt(expr, "lookup expects Table as first argument, got %T", args[0])
				}
				key := args[1]
				for _, entry := range tbl.Entries {
					if ev.ValuesEqual(entry.Key, key) {
						return entry.Value, nil
					}
				}
				return Nil, nil
			},
		},
		{
			Name: "lookup-or",
			Fn: func(ev *Evaluator, args []Value, expr *taxlispv1.Expression) (Value, error) {
				tbl, ok := args[0].(TableVal)
				if !ok {
					return nil, errAt(expr, "lookup-or expects Table as first argument, got %T", args[0])
				}
				key := args[1]
				fallback := args[2]
				for _, entry := range tbl.Entries {
					if ev.ValuesEqual(entry.Key, key) {
						return entry.Value, nil
					}
				}
				return fallback, nil
			},
		},
		{
			Name: "round-dollar",
			Fn: func(_ *Evaluator, args []Value, expr *taxlispv1.Expression) (Value, error) {
				m, ok := args[0].(MoneyVal)
				if !ok {
					return nil, errAt(expr, "round-dollar expects Money, got %T", args[0])
				}
				return MoneyVal{R: roundRatScale(m.R, 1)}, nil
			},
		},
		{
			Name: "round-cents",
			Fn: func(_ *Evaluator, args []Value, expr *taxlispv1.Expression) (Value, error) {
				m, ok := args[0].(MoneyVal)
				if !ok {
					return nil, errAt(expr, "round-cents expects Money, got %T", args[0])
				}
				return MoneyVal{R: roundRatScale(m.R, 100)}, nil
			},
		},
		{
			Name: "round-rate4",
			Fn: func(_ *Evaluator, args []Value, expr *taxlispv1.Expression) (Value, error) {
				f, ok := args[0].(FloatVal)
				if !ok {
					return nil, errAt(expr, "round-rate4 expects Float, got %T", args[0])
				}
				return FloatVal{V: math.Round(f.V*10000) / 10000}, nil
			},
		},
		{
			Name: "floor-float",
			Fn: func(_ *Evaluator, args []Value, expr *taxlispv1.Expression) (Value, error) {
				f, ok := args[0].(FloatVal)
				if !ok {
					return nil, errAt(expr, "floor-float expects Float, got %T", args[0])
				}
				return FloatVal{V: math.Floor(f.V)}, nil
			},
		},
		{
			Name: "ceil-float",
			Fn: func(_ *Evaluator, args []Value, expr *taxlispv1.Expression) (Value, error) {
				f, ok := args[0].(FloatVal)
				if !ok {
					return nil, errAt(expr, "ceil-float expects Float, got %T", args[0])
				}
				return FloatVal{V: math.Ceil(f.V)}, nil
			},
		},
		{
			Name: "floor-qty",
			Fn: func(_ *Evaluator, args []Value, expr *taxlispv1.Expression) (Value, error) {
				q, ok := args[0].(QuantityVal)
				if !ok {
					return nil, errAt(expr, "floor-qty expects Quantity, got %T", args[0])
				}
				return QuantityVal{R: floorRat(q.R)}, nil
			},
		},
		{
			Name: "ceil-qty",
			Fn: func(_ *Evaluator, args []Value, expr *taxlispv1.Expression) (Value, error) {
				q, ok := args[0].(QuantityVal)
				if !ok {
					return nil, errAt(expr, "ceil-qty expects Quantity, got %T", args[0])
				}
				return QuantityVal{R: ceilRat(q.R)}, nil
			},
		},
		{
			Name: "clamp-zero",
			Fn: func(_ *Evaluator, args []Value, expr *taxlispv1.Expression) (Value, error) {
				m, ok := args[0].(MoneyVal)
				if !ok {
					return nil, errAt(expr, "clamp-zero expects Money, got %T", args[0])
				}
				if m.R.Sign() < 0 {
					return MoneyVal{R: big.NewRat(0, 1)}, nil
				}
				return NewMoneyRat(m.R), nil
			},
		},
		{
			Name: "abs-money",
			Fn: func(_ *Evaluator, args []Value, expr *taxlispv1.Expression) (Value, error) {
				m, ok := args[0].(MoneyVal)
				if !ok {
					return nil, errAt(expr, "abs-money expects Money, got %T", args[0])
				}
				return MoneyVal{R: new(big.Rat).Abs(m.R)}, nil
			},
		},
		{
			Name: "int-to-money",
			Fn: func(_ *Evaluator, args []Value, expr *taxlispv1.Expression) (Value, error) {
				i, ok := args[0].(IntVal)
				if !ok {
					return nil, errAt(expr, "int-to-money expects Int, got %T", args[0])
				}
				return MoneyVal{R: big.NewRat(i.V, 1)}, nil
			},
		},
		{
			Name: "int-to-float",
			Fn: func(_ *Evaluator, args []Value, expr *taxlispv1.Expression) (Value, error) {
				i, ok := args[0].(IntVal)
				if !ok {
					return nil, errAt(expr, "int-to-float expects Int, got %T", args[0])
				}
				return FloatVal{V: float64(i.V)}, nil
			},
		},
		{
			Name: "money-to-float",
			Fn: func(_ *Evaluator, args []Value, expr *taxlispv1.Expression) (Value, error) {
				m, ok := args[0].(MoneyVal)
				if !ok {
					return nil, errAt(expr, "money-to-float expects Money, got %T", args[0])
				}
				f, _ := m.R.Float64()
				return FloatVal{V: f}, nil
			},
		},
		{
			Name: "float-to-money",
			Fn: func(_ *Evaluator, args []Value, expr *taxlispv1.Expression) (Value, error) {
				f, ok := args[0].(FloatVal)
				if !ok {
					return nil, errAt(expr, "float-to-money expects Float, got %T", args[0])
				}
				return MoneyVal{R: floatToRat(f.V)}, nil
			},
		},
		{
			Name: "date-year",
			Fn: func(_ *Evaluator, args []Value, expr *taxlispv1.Expression) (Value, error) {
				d, ok := args[0].(DateVal)
				if !ok {
					return nil, errAt(expr, "date-year expects Date, got %T", args[0])
				}
				return IntVal{V: int64(d.Year)}, nil
			},
		},
		{
			Name: "date-month",
			Fn: func(_ *Evaluator, args []Value, expr *taxlispv1.Expression) (Value, error) {
				d, ok := args[0].(DateVal)
				if !ok {
					return nil, errAt(expr, "date-month expects Date, got %T", args[0])
				}
				return IntVal{V: int64(d.Month)}, nil
			},
		},
		{
			Name: "date-day",
			Fn: func(_ *Evaluator, args []Value, expr *taxlispv1.Expression) (Value, error) {
				d, ok := args[0].(DateVal)
				if !ok {
					return nil, errAt(expr, "date-day expects Date, got %T", args[0])
				}
				return IntVal{V: int64(d.Day)}, nil
			},
		},
	}
	for _, b := range builtins {
		ev.Globals.Set(b.Name, b)
	}
}

// EvalModule evaluates all top-level forms in a ModuleFile, registering `use-proto`,
// `defstruct`, `=`, and `def` declarations in `ev.Globals`.
func (ev *Evaluator) EvalModule(mf *taxlispv1.ModuleFile) (Value, error) {
	var last Value = Nil
	for _, expr := range mf.GetExpressions() {
		v, err := ev.EvalExpr(expr, ev.Globals)
		if err != nil {
			return nil, err
		}
		last = v
	}
	return last, nil
}

// Call invokes a named top-level function with the provided arguments.
func (ev *Evaluator) Call(name string, args ...Value) (Value, error) {
	fn, ok := ev.Globals.Get(name)
	if !ok {
		return nil, fmt.Errorf("undefined function %q", name)
	}
	return ev.Apply(fn, args, nil)
}

// Apply calls a runtime function value (`ClosureVal`, `BuiltinVal`, or `StructCtorVal`).
func (ev *Evaluator) Apply(fn Value, args []Value, callSite *taxlispv1.Expression) (Value, error) {
	switch f := fn.(type) {
	case BuiltinVal:
		return f.Fn(ev, args, callSite)
	case StructCtorVal:
		if len(args) != len(f.FieldNames) {
			return nil, errAt(callSite, "struct %s expects %d arguments, got %d", f.Name, len(f.FieldNames), len(args))
		}
		fields := make(map[string]Value, len(f.FieldNames))
		for i, name := range f.FieldNames {
			fields[name] = args[i]
		}
		return StructVal{TypeName: f.Name, Fields: fields}, nil
	case ClosureVal:
		if len(args) != len(f.ParamNames) {
			return nil, errAt(callSite, "function %s expects %d arguments, got %d", f.Name, len(f.ParamNames), len(args))
		}
		frame := NewEnv(f.Env)
		for i, p := range f.ParamNames {
			frame.Set(p, args[i])
		}
		return ev.EvalExpr(f.Body, frame)
	default:
		return nil, errAt(callSite, "cannot call non-function value %v (%T)", fn, fn)
	}
}

// EvalExpr evaluates a single taxlisp Expression in the given lexical environment.
func (ev *Evaluator) EvalExpr(expr *taxlispv1.Expression, env *Env) (Value, error) {
	switch v := expr.GetValue().(type) {
	case *taxlispv1.Expression_Atom:
		return ev.evalAtom(expr, v.Atom, env)
	case *taxlispv1.Expression_List:
		return ev.evalList(expr, v.List, env)
	default:
		return Nil, nil
	}
}

func (ev *Evaluator) evalAtom(expr *taxlispv1.Expression, a *taxlispv1.Atom, env *Env) (Value, error) {
	switch p := a.GetPrimitive().(type) {
	case *taxlispv1.Atom_BoolValue:
		return BoolVal{V: p.BoolValue}, nil
	case *taxlispv1.Atom_IntValue:
		return IntVal{V: p.IntValue}, nil
	case *taxlispv1.Atom_StringValue:
		return StringVal{V: p.StringValue}, nil
	case *taxlispv1.Atom_Money:
		return MoneyVal{R: big.NewRat(p.Money.GetMinor(), 100)}, nil
	case *taxlispv1.Atom_FloatValue:
		return FloatVal{V: p.FloatValue}, nil
	case *taxlispv1.Atom_Date:
		d := p.Date
		return DateVal{Year: d.GetYear(), Month: d.GetMonth(), Day: d.GetDay()}, nil
	case *taxlispv1.Atom_Keyword:
		return SymbolVal{Name: ":" + p.Keyword}, nil
	case *taxlispv1.Atom_Symbol:
		name := fullSymbolName(p.Symbol)
		if name == "nil" {
			return Nil, nil
		}
		val, ok := env.Get(name)
		if !ok {
			return nil, errAt(expr, "unbound symbol %s", name)
		}
		return val, nil
	default:
		return Nil, nil
	}
}

func (ev *Evaluator) evalList(expr *taxlispv1.Expression, list *taxlispv1.List, env *Env) (Value, error) {
	elems := list.GetExpressions()
	if len(elems) == 0 {
		return Nil, nil
	}
	head := symbolName(elems[0])
	switch head {
	case "use-proto":
		pkg, err := extractQuotedOrString(elems[1])
		if err != nil {
			return nil, errAt(elems[1], "%v", err)
		}
		alias, err := extractQuotedOrString(elems[2])
		if err != nil {
			return nil, errAt(elems[2], "%v", err)
		}
		ev.ProtoAliases[alias] = pkg
		return Nil, nil

	case "deftype":
		return Nil, nil

	case "defstruct":
		name, err := parseDefName(elems[1])
		if err != nil {
			return nil, err
		}
		fieldStart := 2
		if l2 := elems[2].GetList(); l2 != nil && isBareSymbolList(l2) {
			fieldStart = 3
		}
		var fieldNames []string
		for _, fExpr := range elems[fieldStart:] {
			fl := fExpr.GetList()
			if fl != nil && len(fl.GetExpressions()) == 2 {
				fieldNames = append(fieldNames, symbolName(fl.GetExpressions()[0]))
			}
		}
		ctor := StructCtorVal{Name: name, FieldNames: fieldNames}
		ev.Globals.Set(name, ctor)
		return ctor, nil

	case "=":
		name := symbolName(elems[1])
		val, err := ev.EvalExpr(elems[3], env)
		if err != nil {
			return nil, err
		}
		ev.Globals.Set(name, val)
		return val, nil

	case "def":
		name, err := parseDefName(elems[1])
		if err != nil {
			return nil, err
		}
		paramNames, err := parseParamNames(elems[2])
		if err != nil {
			return nil, err
		}
		bodyStart := 4
		for bodyStart+1 < len(elems) && elems[bodyStart].GetAtom() != nil && elems[bodyStart].GetAtom().GetKeyword() != "" {
			bodyStart += 2
		}
		body := wrapDo(elems[bodyStart:])
		cl := ClosureVal{
			Name:       name,
			ParamNames: paramNames,
			Body:       body,
			Env:        ev.Globals,
		}
		ev.Globals.Set(name, cl)
		return cl, nil

	case "fn":
		paramNames, err := parseParamNames(elems[1])
		if err != nil {
			return nil, err
		}
		body := wrapDo(elems[3:])
		return ClosureVal{
			ParamNames: paramNames,
			Body:       body,
			Env:        env,
		}, nil

	case "let":
		varName := symbolName(elems[1])
		valIdx := 2
		bodyStart := 3
		if len(elems) >= 5 {
			valIdx = 3
			bodyStart = 4
		}
		val, err := ev.EvalExpr(elems[valIdx], env)
		if err != nil {
			return nil, err
		}
		child := NewEnv(env)
		child.Set(varName, val)
		return ev.evalSeq(elems[bodyStart:], child)

	case "with", "withs":
		bList := elems[1].GetList().GetExpressions()
		child := NewEnv(env)
		evalFrame := env
		if head == "withs" {
			evalFrame = child
		}
		for i := 0; i < len(bList); i += 2 {
			bExpr := bList[i]
			valExpr := bList[i+1]
			var varName string
			if bl := bExpr.GetList(); bl != nil && len(bl.GetExpressions()) == 2 {
				varName = symbolName(bl.GetExpressions()[0])
			} else {
				varName = symbolName(bExpr)
			}
			val, err := ev.EvalExpr(valExpr, evalFrame)
			if err != nil {
				return nil, err
			}
			child.Set(varName, val)
		}
		return ev.evalSeq(elems[2:], child)

	case "do":
		return ev.evalSeq(elems[1:], env)

	case "if":
		rest := elems[1:]
		for i := 0; i+1 < len(rest); i += 2 {
			condVal, err := ev.EvalExpr(rest[i], env)
			if err != nil {
				return nil, err
			}
			b, ok := condVal.(BoolVal)
			if !ok {
				return nil, errAt(rest[i], "if condition must evaluate to Bool, got %v", condVal)
			}
			if b.V {
				return ev.EvalExpr(rest[i+1], env)
			}
		}
		return ev.EvalExpr(rest[len(rest)-1], env)

	case "cond":
		for _, clause := range elems[1:] {
			pair := clause.GetList().GetExpressions()
			condExpr := pair[0]
			thenExpr := pair[1]
			if symbolName(condExpr) == "else" || (condExpr.GetAtom() != nil && condExpr.GetAtom().GetKeyword() == "else") {
				return ev.EvalExpr(thenExpr, env)
			}
			condVal, err := ev.EvalExpr(condExpr, env)
			if err != nil {
				return nil, err
			}
			b, ok := condVal.(BoolVal)
			if !ok {
				return nil, errAt(condExpr, "cond condition must evaluate to Bool, got %v", condVal)
			}
			if b.V {
				return ev.EvalExpr(thenExpr, env)
			}
		}
		return Nil, nil

	case "typecase":
		bh := elems[1].GetList().GetExpressions()
		varName := symbolName(bh[0])
		scrutVal, err := ev.EvalExpr(bh[1], env)
		if err != nil {
			return nil, err
		}
		branches := elems[2:]
		for i := 0; i < len(branches); i += 2 {
			typeExpr := branches[i]
			bodyExpr := branches[i+1]
			if ev.valueMatchesTypeExpr(scrutVal, typeExpr) {
				child := NewEnv(env)
				child.Set(varName, scrutVal)
				return ev.EvalExpr(bodyExpr, child)
			}
		}
		return nil, errAt(expr, "non-exhaustive typecase for runtime value %v (%T)", scrutVal, scrutVal)

	case "isa":
		val, err := ev.EvalExpr(elems[1], env)
		if err != nil {
			return nil, err
		}
		return BoolVal{V: ev.valueMatchesTypeExpr(val, elems[2])}, nil

	case "quote":
		sym := elems[1].GetAtom().GetSymbol()
		if sym == nil {
			return nil, errAt(elems[1], "quote expects a symbol")
		}
		return SymbolVal{Package: sym.GetPackage(), Name: sym.GetName()}, nil

	case "slice":
		out := make([]Value, len(elems)-1)
		for i, e := range elems[1:] {
			v, err := ev.EvalExpr(e, env)
			if err != nil {
				return nil, err
			}
			out[i] = v
		}
		return SliceVal{Elems: out}, nil

	case "table":
		var entries []TableEntry
		for i := 1; i < len(elems); i += 2 {
			k, err := ev.EvalExpr(elems[i], env)
			if err != nil {
				return nil, err
			}
			v, err := ev.EvalExpr(elems[i+1], env)
			if err != nil {
				return nil, err
			}
			entries = append(entries, TableEntry{Key: k, Value: v})
		}
		return TableVal{Entries: entries}, nil

	case "inst":
		// Type arguments are erased at runtime; evaluate the underlying generic function.
		return ev.EvalExpr(elems[1], env)

	case "make-struct":
		// (make-struct StructType :field1 val1 :field2 val2 ...)
		typeName := extractStructName(elems[1])
		fields := map[string]Value{}
		for i := 2; i < len(elems); i += 2 {
			kw := elems[i].GetAtom().GetKeyword()
			val, err := ev.EvalExpr(elems[i+1], env)
			if err != nil {
				return nil, err
			}
			fields[kw] = val
		}
		return StructVal{TypeName: typeName, Fields: fields}, nil

	case "make-proto", "proto":
		return ev.evalMakeProto(expr, elems[1:], env)

	case "proto-with":
		return ev.evalProtoWith(expr, elems[1:], env)

	case ".":
		return ev.evalFieldAccess(expr, elems[1:], env)

	case "has?":
		return ev.evalHasField(expr, elems[1:], env)

	case "which-oneof":
		return ev.evalWhichOneof(expr, elems[1:], env)

	case "+", "-", "*", "/", "min", "max":
		return ev.evalArithmetic(expr, head, elems[1:], env)

	case "<", "<=", ">", ">=":
		return ev.evalComparison(expr, head, elems[1], elems[2], env)

	case "is":
		lv, err := ev.EvalExpr(elems[1], env)
		if err != nil {
			return nil, err
		}
		rv, err := ev.EvalExpr(elems[2], env)
		if err != nil {
			return nil, err
		}
		return BoolVal{V: ev.ValuesEqual(lv, rv)}, nil

	case "!=":
		lv, err := ev.EvalExpr(elems[1], env)
		if err != nil {
			return nil, err
		}
		rv, err := ev.EvalExpr(elems[2], env)
		if err != nil {
			return nil, err
		}
		return BoolVal{V: !ev.ValuesEqual(lv, rv)}, nil

	case "and":
		for _, arg := range elems[1:] {
			v, err := ev.EvalExpr(arg, env)
			if err != nil {
				return nil, err
			}
			b, ok := v.(BoolVal)
			if !ok {
				return nil, errAt(arg, "and expects Bool, got %v", v)
			}
			if !b.V {
				return BoolVal{V: false}, nil
			}
		}
		return BoolVal{V: true}, nil

	case "or":
		for _, arg := range elems[1:] {
			v, err := ev.EvalExpr(arg, env)
			if err != nil {
				return nil, err
			}
			b, ok := v.(BoolVal)
			if !ok {
				return nil, errAt(arg, "or expects Bool, got %v", v)
			}
			if b.V {
				return BoolVal{V: true}, nil
			}
		}
		return BoolVal{V: false}, nil

	default:
		fnVal, err := ev.EvalExpr(elems[0], env)
		if err != nil {
			return nil, err
		}
		args := make([]Value, len(elems)-1)
		for i, a := range elems[1:] {
			v, err := ev.EvalExpr(a, env)
			if err != nil {
				return nil, err
			}
			args[i] = v
		}
		return ev.Apply(fnVal, args, expr)
	}
}

func (ev *Evaluator) evalSeq(exprs []*taxlispv1.Expression, env *Env) (Value, error) {
	var last Value = Nil
	var err error
	for _, e := range exprs {
		last, err = ev.EvalExpr(e, env)
		if err != nil {
			return nil, err
		}
	}
	return last, nil
}

func (ev *Evaluator) evalArithmetic(expr *taxlispv1.Expression, op string, args []*taxlispv1.Expression, env *Env) (Value, error) {
	vals := make([]Value, len(args))
	for i, a := range args {
		v, err := ev.EvalExpr(a, env)
		if err != nil {
			return nil, err
		}
		vals[i] = v
	}

	switch op {
	case "+", "-", "min", "max":
		if op == "-" && len(vals) == 1 {
			switch v := vals[0].(type) {
			case MoneyVal:
				return MoneyVal{R: new(big.Rat).Neg(v.R)}, nil
			case FloatVal:
				return FloatVal{V: -v.V}, nil
			case IntVal:
				return IntVal{V: -v.V}, nil
			}
		}
		switch first := vals[0].(type) {
		case MoneyVal:
			acc := new(big.Rat).Set(first.R)
			for _, v := range vals[1:] {
				r := v.(MoneyVal).R
				switch op {
				case "+":
					acc.Add(acc, r)
				case "-":
					acc.Sub(acc, r)
				case "min":
					if r.Cmp(acc) < 0 {
						acc.Set(r)
					}
				case "max":
					if r.Cmp(acc) > 0 {
						acc.Set(r)
					}
				}
			}
			return MoneyVal{R: acc}, nil
		case FloatVal:
			acc := first.V
			for _, v := range vals[1:] {
				fv := v.(FloatVal).V
				switch op {
				case "+":
					acc += fv
				case "-":
					acc -= fv
				case "min":
					if fv < acc {
						acc = fv
					}
				case "max":
					if fv > acc {
						acc = fv
					}
				}
			}
			return FloatVal{V: acc}, nil
		case IntVal:
			acc := first.V
			for _, v := range vals[1:] {
				iv := v.(IntVal).V
				switch op {
				case "+":
					acc += iv
				case "-":
					acc -= iv
				case "min":
					if iv < acc {
						acc = iv
					}
				case "max":
					if iv > acc {
						acc = iv
					}
				}
			}
			return IntVal{V: acc}, nil
		default:
			return nil, errAt(expr, "unsupported type %T for %s", first, op)
		}

	case "*":
		lv, rv := vals[0], vals[1]
		switch l := lv.(type) {
		case MoneyVal:
			switch r := rv.(type) {
			case FloatVal:
				return MoneyVal{R: new(big.Rat).Mul(l.R, floatToRat(r.V))}, nil
			case IntVal:
				return MoneyVal{R: new(big.Rat).Mul(l.R, big.NewRat(r.V, 1))}, nil
			}
		case FloatVal:
			switch r := rv.(type) {
			case MoneyVal:
				return MoneyVal{R: new(big.Rat).Mul(floatToRat(l.V), r.R)}, nil
			case FloatVal:
				return FloatVal{V: l.V * r.V}, nil
			case IntVal:
				return FloatVal{V: l.V * float64(r.V)}, nil
			}
		case IntVal:
			switch r := rv.(type) {
			case MoneyVal:
				return MoneyVal{R: new(big.Rat).Mul(big.NewRat(l.V, 1), r.R)}, nil
			case FloatVal:
				return FloatVal{V: float64(l.V) * r.V}, nil
			case IntVal:
				return IntVal{V: l.V * r.V}, nil
			}
		}
		return nil, errAt(expr, "unsupported types %T and %T for *", lv, rv)

	case "/":
		lv, rv := vals[0], vals[1]
		switch l := lv.(type) {
		case MoneyVal:
			switch r := rv.(type) {
			case FloatVal:
				if r.V == 0 {
					return nil, errAt(expr, "division by zero")
				}
				return MoneyVal{R: new(big.Rat).Quo(l.R, floatToRat(r.V))}, nil
			case IntVal:
				if r.V == 0 {
					return nil, errAt(expr, "division by zero")
				}
				return MoneyVal{R: new(big.Rat).Quo(l.R, big.NewRat(r.V, 1))}, nil
			case MoneyVal:
				if r.R.Sign() == 0 {
					return nil, errAt(expr, "division by zero")
				}
				quo := new(big.Rat).Quo(l.R, r.R)
				f, _ := quo.Float64()
				return FloatVal{V: f}, nil
			}
		case FloatVal:
			switch r := rv.(type) {
			case FloatVal:
				if r.V == 0 {
					return nil, errAt(expr, "division by zero")
				}
				return FloatVal{V: l.V / r.V}, nil
			case IntVal:
				if r.V == 0 {
					return nil, errAt(expr, "division by zero")
				}
				return FloatVal{V: l.V / float64(r.V)}, nil
			}
		case IntVal:
			r := rv.(IntVal)
			if r.V == 0 {
				return nil, errAt(expr, "integer division by zero")
			}
			return IntVal{V: l.V / r.V}, nil
		}
		return nil, errAt(expr, "unsupported types %T and %T for /", lv, rv)
	}
	return nil, errAt(expr, "unknown arithmetic op %s", op)
}

func (ev *Evaluator) evalComparison(expr *taxlispv1.Expression, op string, leftExpr, rightExpr *taxlispv1.Expression, env *Env) (Value, error) {
	lv, err := ev.EvalExpr(leftExpr, env)
	if err != nil {
		return nil, err
	}
	rv, err := ev.EvalExpr(rightExpr, env)
	if err != nil {
		return nil, err
	}
	var cmp int
	switch l := lv.(type) {
	case MoneyVal:
		cmp = l.R.Cmp(rv.(MoneyVal).R)
	case FloatVal:
		r := rv.(FloatVal).V
		if l.V < r {
			cmp = -1
		} else if l.V > r {
			cmp = 1
		}
	case IntVal:
		r := rv.(IntVal).V
		if l.V < r {
			cmp = -1
		} else if l.V > r {
			cmp = 1
		}
	case DateVal:
		cmp = compareDate(l, rv.(DateVal))
	default:
		return nil, errAt(expr, "cannot compare %T with %s", lv, op)
	}

	switch op {
	case "<":
		return BoolVal{V: cmp < 0}, nil
	case "<=":
		return BoolVal{V: cmp <= 0}, nil
	case ">":
		return BoolVal{V: cmp > 0}, nil
	case ">=":
		return BoolVal{V: cmp >= 0}, nil
	default:
		return BoolVal{V: false}, nil
	}
}

// ValuesEqual reports whether two runtime values are equal.
func (ev *Evaluator) ValuesEqual(a, b Value) bool {
	switch av := a.(type) {
	case NilVal:
		_, ok := b.(NilVal)
		return ok
	case BoolVal:
		bv, ok := b.(BoolVal)
		return ok && av.V == bv.V
	case IntVal:
		bv, ok := b.(IntVal)
		return ok && av.V == bv.V
	case StringVal:
		bv, ok := b.(StringVal)
		return ok && av.V == bv.V
	case MoneyVal:
		bv, ok := b.(MoneyVal)
		return ok && av.R.Cmp(bv.R) == 0
	case FloatVal:
		bv, ok := b.(FloatVal)
		return ok && av.V == bv.V
	case DateVal:
		bv, ok := b.(DateVal)
		return ok && compareDate(av, bv) == 0
	case SymbolVal:
		switch bv := b.(type) {
		case SymbolVal:
			return av.Package == bv.Package && shortEnumName(av.Name) == shortEnumName(bv.Name)
		case ProtoEnumVal:
			return shortEnumName(av.Name) == shortEnumName(bv.Name)
		}
		return false
	case ProtoEnumVal:
		switch bv := b.(type) {
		case ProtoEnumVal:
			return av.Desc.FullName() == bv.Desc.FullName() && av.Number == bv.Number
		case SymbolVal:
			return shortEnumName(av.Name) == shortEnumName(bv.Name)
		}
		return false
	case ProtoMessageVal:
		bv, ok := b.(ProtoMessageVal)
		return ok && proto.Equal(av.Msg.Interface(), bv.Msg.Interface())
	default:
		return false
	}
}

func shortEnumName(s string) string {
	if idx := strings.LastIndex(s, "."); idx >= 0 {
		return s[idx+1:]
	}
	return s
}

func (ev *Evaluator) valueMatchesTypeExpr(val Value, typeExpr *taxlispv1.Expression) bool {
	if a := typeExpr.GetAtom(); a != nil && a.GetSymbol() != nil {
		sym := a.GetSymbol()
		if sym.GetPackage() == "" {
			switch sym.GetName() {
			case "Nil":
				_, ok := val.(NilVal)
				return ok
			case "Bool":
				_, ok := val.(BoolVal)
				return ok
			case "Int":
				_, ok := val.(IntVal)
				return ok
			case "String":
				_, ok := val.(StringVal)
				return ok
			case "Money":
				_, ok := val.(MoneyVal)
				return ok
			case "Float":
				_, ok := val.(FloatVal)
				return ok
			case "Date":
				_, ok := val.(DateVal)
				return ok
			default:
				if sv, ok := val.(StructVal); ok {
					return sv.TypeName == sym.GetName()
				}
			}
		} else {
			desc, err := ev.resolveProtoDescriptor(sym.GetPackage(), sym.GetName())
			if err == nil {
				switch d := desc.(type) {
				case protoreflect.MessageDescriptor:
					pm, ok := val.(ProtoMessageVal)
					return ok && pm.Msg.Descriptor().FullName() == d.FullName()
				case protoreflect.EnumDescriptor:
					pe, ok := val.(ProtoEnumVal)
					return ok && pe.Desc.FullName() == d.FullName()
				}
			}
		}
	}
	if l := typeExpr.GetList(); l != nil && len(l.GetExpressions()) > 0 {
		head := symbolName(l.GetExpressions()[0])
		switch head {
		case "Union":
			for _, m := range l.GetExpressions()[1:] {
				if ev.valueMatchesTypeExpr(val, m) {
					return true
				}
			}
			return false
		case "Slice":
			_, ok := val.(SliceVal)
			return ok
		default:
			if sv, ok := val.(StructVal); ok {
				return sv.TypeName == head
			}
		}
	}
	return false
}

// WrapProto wraps a Go `proto.Message` into a taxlisp `Value` (`ProtoMessageVal`, `MoneyVal`, `FloatVal`, or `DateVal`).
func WrapProto(msg proto.Message) Value {
	if msg == nil {
		return Nil
	}
	pm := msg.ProtoReflect()
	if !pm.IsValid() {
		return Nil
	}
	return wrapReflectMessage(pm)
}

func wrapReflectMessage(pm protoreflect.Message) Value {
	switch pm.Descriptor().FullName() {
	case protoMoneyFullName:
		fds := pm.Descriptor().Fields()
		minor := pm.Get(fds.ByName("minor")).Int()
		return MoneyVal{R: big.NewRat(minor, 100)}
	case protoQuantityFullName:
		fds := pm.Descriptor().Fields()
		units := pm.Get(fds.ByName("units")).Int()
		scale := pm.Get(fds.ByName("scale")).Int()
		return FloatVal{V: float64(units) / math.Pow10(int(scale))}
	case protoDateFullName:
		fds := pm.Descriptor().Fields()
		return DateVal{
			Year:  int32(pm.Get(fds.ByName("year")).Int()),
			Month: int32(pm.Get(fds.ByName("month")).Int()),
			Day:   int32(pm.Get(fds.ByName("day")).Int()),
		}
	default:
		return ProtoMessageVal{Msg: pm}
	}
}

func (ev *Evaluator) newProtoMessage(md protoreflect.MessageDescriptor) protoreflect.Message {
	if mt, err := ev.Types.FindMessageByName(md.FullName()); err == nil {
		return mt.New()
	}
	return dynamicpb.NewMessage(md)
}

func (ev *Evaluator) evalMakeProto(expr *taxlispv1.Expression, args []*taxlispv1.Expression, env *Env) (Value, error) {
	sym, err := extractQuotedSymbol(args[0])
	if err != nil {
		return nil, errAt(args[0], "%v", err)
	}
	desc, err := ev.resolveProtoDescriptor(sym.GetPackage(), sym.GetName())
	if err != nil {
		return nil, errAt(args[0], "%v", err)
	}
	md, ok := desc.(protoreflect.MessageDescriptor)
	if !ok {
		return nil, errAt(args[0], "%s is not a protobuf message", desc.FullName())
	}
	msg := ev.newProtoMessage(md)
	if err := ev.populateProtoFields(expr, msg, args[1:], env); err != nil {
		return nil, err
	}
	return wrapReflectMessage(msg), nil
}

func (ev *Evaluator) evalProtoWith(expr *taxlispv1.Expression, args []*taxlispv1.Expression, env *Env) (Value, error) {
	baseVal, err := ev.EvalExpr(args[0], env)
	if err != nil {
		return nil, err
	}
	pm, ok := baseVal.(ProtoMessageVal)
	if !ok {
		return nil, errAt(args[0], "proto-with target must be a protobuf message, got %T", baseVal)
	}
	cloned := proto.Clone(pm.Msg.Interface()).ProtoReflect()
	if err := ev.populateProtoFields(expr, cloned, args[1:], env); err != nil {
		return nil, err
	}
	return ProtoMessageVal{Msg: cloned}, nil
}

func (ev *Evaluator) populateProtoFields(expr *taxlispv1.Expression, msg protoreflect.Message, kvArgs []*taxlispv1.Expression, env *Env) error {
	md := msg.Descriptor()
	for i := 0; i < len(kvArgs); i += 2 {
		kwAtom := kvArgs[i].GetAtom()
		fieldName := kwAtom.GetKeyword()
		fd := md.Fields().ByName(protoreflect.Name(fieldName))
		if fd == nil {
			return errAt(kvArgs[i], "protobuf message %s has no field %q", md.FullName(), fieldName)
		}
		val, err := ev.EvalExpr(kvArgs[i+1], env)
		if err != nil {
			return err
		}
		if err := ev.setProtoField(kvArgs[i+1], msg, fd, val); err != nil {
			return err
		}
	}
	return nil
}

func (ev *Evaluator) setProtoField(site *taxlispv1.Expression, msg protoreflect.Message, fd protoreflect.FieldDescriptor, val Value) error {
	if _, isNil := val.(NilVal); isNil {
		msg.Clear(fd)
		return nil
	}
	if fd.IsList() {
		sv, ok := val.(SliceVal)
		if !ok {
			return errAt(site, "repeated field %s expects Slice, got %T", fd.Name(), val)
		}
		list := msg.Mutable(fd).List()
		list.Truncate(0)
		for _, elem := range sv.Elems {
			pv, err := ev.valueToProtoFieldValue(site, fd, elem)
			if err != nil {
				return err
			}
			list.Append(pv)
		}
		return nil
	}
	pv, err := ev.valueToProtoFieldValue(site, fd, val)
	if err != nil {
		return err
	}
	msg.Set(fd, pv)
	return nil
}

func (ev *Evaluator) valueToProtoFieldValue(site *taxlispv1.Expression, fd protoreflect.FieldDescriptor, val Value) (protoreflect.Value, error) {
	switch fd.Kind() {
	case protoreflect.BoolKind:
		b, ok := val.(BoolVal)
		if !ok {
			return protoreflect.Value{}, errAt(site, "field %s expects Bool, got %T", fd.Name(), val)
		}
		return protoreflect.ValueOfBool(b.V), nil
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		i, ok := val.(IntVal)
		if !ok {
			return protoreflect.Value{}, errAt(site, "field %s expects Int, got %T", fd.Name(), val)
		}
		return protoreflect.ValueOfInt32(int32(i.V)), nil
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		i, ok := val.(IntVal)
		if !ok {
			return protoreflect.Value{}, errAt(site, "field %s expects Int, got %T", fd.Name(), val)
		}
		return protoreflect.ValueOfInt64(i.V), nil
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		i, ok := val.(IntVal)
		if !ok {
			return protoreflect.Value{}, errAt(site, "field %s expects Int, got %T", fd.Name(), val)
		}
		return protoreflect.ValueOfUint32(uint32(i.V)), nil
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		i, ok := val.(IntVal)
		if !ok {
			return protoreflect.Value{}, errAt(site, "field %s expects Int, got %T", fd.Name(), val)
		}
		return protoreflect.ValueOfUint64(uint64(i.V)), nil
	case protoreflect.FloatKind:
		f, ok := val.(FloatVal)
		if !ok {
			return protoreflect.Value{}, errAt(site, "field %s expects Float, got %T", fd.Name(), val)
		}
		return protoreflect.ValueOfFloat32(float32(f.V)), nil
	case protoreflect.DoubleKind:
		f, ok := val.(FloatVal)
		if !ok {
			return protoreflect.Value{}, errAt(site, "field %s expects Float, got %T", fd.Name(), val)
		}
		return protoreflect.ValueOfFloat64(f.V), nil
	case protoreflect.StringKind:
		s, ok := val.(StringVal)
		if !ok {
			return protoreflect.Value{}, errAt(site, "field %s expects String, got %T", fd.Name(), val)
		}
		return protoreflect.ValueOfString(s.V), nil
	case protoreflect.EnumKind:
		switch v := val.(type) {
		case ProtoEnumVal:
			return protoreflect.ValueOfEnum(v.Number), nil
		case SymbolVal:
			rawName := v.Name
			if idx := strings.LastIndex(rawName, "."); idx >= 0 {
				rawName = rawName[idx+1:]
			}
			evd := fd.Enum().Values().ByName(protoreflect.Name(rawName))
			if evd == nil {
				return protoreflect.Value{}, errAt(site, "enum %s has no value %s", fd.Enum().FullName(), v.Name)
			}
			return protoreflect.ValueOfEnum(evd.Number()), nil
		default:
			return protoreflect.Value{}, errAt(site, "field %s expects enum %s, got %T", fd.Name(), fd.Enum().FullName(), val)
		}
	case protoreflect.MessageKind:
		md := fd.Message()
		switch md.FullName() {
		case protoMoneyFullName:
			m, ok := val.(MoneyVal)
			if !ok {
				return protoreflect.Value{}, errAt(site, "field %s expects Money, got %T", fd.Name(), val)
			}
			sub := ev.newProtoMessage(md)
			minor := m.Cents()
			fds := md.Fields()
			sub.Set(fds.ByName("currency"), protoreflect.ValueOfString("USD"))
			if minor != 0 {
				sub.Set(fds.ByName("minor"), protoreflect.ValueOfInt64(minor))
			}
			return protoreflect.ValueOfMessage(sub), nil
		case protoQuantityFullName:
			f, ok := val.(FloatVal)
			if !ok {
				return protoreflect.Value{}, errAt(site, "field %s expects Float, got %T", fd.Name(), val)
			}
			sub := ev.newProtoMessage(md)
			units, scale := floatToQuantityUnitsScale(f.V)
			fds := md.Fields()
			if units != 0 {
				sub.Set(fds.ByName("units"), protoreflect.ValueOfInt64(units))
			}
			if scale != 0 {
				sub.Set(fds.ByName("scale"), protoreflect.ValueOfInt32(scale))
			}
			return protoreflect.ValueOfMessage(sub), nil
		case protoDateFullName:
			d, ok := val.(DateVal)
			if !ok {
				return protoreflect.Value{}, errAt(site, "field %s expects Date, got %T", fd.Name(), val)
			}
			sub := ev.newProtoMessage(md)
			fds := md.Fields()
			sub.Set(fds.ByName("year"), protoreflect.ValueOfInt32(d.Year))
			sub.Set(fds.ByName("month"), protoreflect.ValueOfInt32(d.Month))
			sub.Set(fds.ByName("day"), protoreflect.ValueOfInt32(d.Day))
			return protoreflect.ValueOfMessage(sub), nil
		default:
			pm, ok := val.(ProtoMessageVal)
			if !ok {
				return protoreflect.Value{}, errAt(site, "field %s expects %s, got %T", fd.Name(), md.FullName(), val)
			}
			return protoreflect.ValueOfMessage(pm.Msg), nil
		}
	default:
		return protoreflect.Value{}, errAt(site, "unsupported field kind %v", fd.Kind())
	}
}

func (ev *Evaluator) evalFieldAccess(expr *taxlispv1.Expression, args []*taxlispv1.Expression, env *Env) (Value, error) {
	cur, err := ev.EvalExpr(args[0], env)
	if err != nil {
		return nil, err
	}
	pathStr, err := extractQuotedOrString(args[1])
	if err != nil {
		return nil, errAt(args[1], "%v", err)
	}
	segments := strings.Split(pathStr, ".")
	for _, seg := range segments {
		switch target := cur.(type) {
		case DateVal:
			switch seg {
			case "year":
				cur = IntVal{V: int64(target.Year)}
			case "month":
				cur = IntVal{V: int64(target.Month)}
			case "day":
				cur = IntVal{V: int64(target.Day)}
			default:
				return nil, errAt(args[1], "Date has no field %q", seg)
			}
		case StructVal:
			fv, ok := target.Fields[seg]
			if !ok {
				return nil, errAt(args[1], "struct %s has no field %q", target.TypeName, seg)
			}
			cur = fv
		case ProtoMessageVal:
			next, err := ev.readProtoFieldOrOneof(args[1], target.Msg, seg)
			if err != nil {
				return nil, err
			}
			cur = next
		default:
			return nil, errAt(expr, "cannot access field %q on value %v (%T)", seg, cur, cur)
		}
	}
	return cur, nil
}

func (ev *Evaluator) readProtoFieldOrOneof(site *taxlispv1.Expression, msg protoreflect.Message, seg string) (Value, error) {
	md := msg.Descriptor()
	if od := md.Oneofs().ByName(protoreflect.Name(seg)); od != nil && !od.IsSynthetic() {
		activeFd := msg.WhichOneof(od)
		if activeFd == nil {
			return Nil, nil
		}
		return ev.readProtoFieldValue(msg, activeFd), nil
	}
	fd := md.Fields().ByName(protoreflect.Name(seg))
	if fd == nil {
		return nil, errAt(site, "protobuf message %s has no field or oneof %q", md.FullName(), seg)
	}
	if fd.ContainingOneof() != nil && !fd.ContainingOneof().IsSynthetic() && !msg.Has(fd) {
		return Nil, nil
	}
	return ev.readProtoFieldValue(msg, fd), nil
}

func (ev *Evaluator) readProtoFieldValue(msg protoreflect.Message, fd protoreflect.FieldDescriptor) Value {
	if fd.IsList() {
		list := msg.Get(fd).List()
		elems := make([]Value, list.Len())
		for i := 0; i < list.Len(); i++ {
			elems[i] = ev.protoScalarOrMessageToValue(fd, list.Get(i))
		}
		return SliceVal{Elems: elems}
	}
	if fd.Kind() == protoreflect.MessageKind && !msg.Has(fd) {
		switch fd.Message().FullName() {
		case protoMoneyFullName:
			return MoneyVal{R: big.NewRat(0, 1)}
		case protoQuantityFullName:
			return FloatVal{V: 0}
		case protoDateFullName:
			return DateVal{}
		default:
			return ProtoMessageVal{Msg: ev.newProtoMessage(fd.Message())}
		}
	}
	return ev.protoScalarOrMessageToValue(fd, msg.Get(fd))
}

func (ev *Evaluator) protoScalarOrMessageToValue(fd protoreflect.FieldDescriptor, pv protoreflect.Value) Value {
	switch fd.Kind() {
	case protoreflect.BoolKind:
		return BoolVal{V: pv.Bool()}
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind,
		protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return IntVal{V: pv.Int()}
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind,
		protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return IntVal{V: int64(pv.Uint())}
	case protoreflect.FloatKind:
		return FloatVal{V: float64(pv.Float())}
	case protoreflect.DoubleKind:
		return FloatVal{V: pv.Float()}
	case protoreflect.StringKind:
		return StringVal{V: pv.String()}
	case protoreflect.EnumKind:
		num := pv.Enum()
		evd := fd.Enum().Values().ByNumber(num)
		name := ""
		if evd != nil {
			name = string(evd.Name())
		}
		return ProtoEnumVal{Desc: fd.Enum(), Number: num, Name: name}
	case protoreflect.MessageKind:
		return wrapReflectMessage(pv.Message())
	default:
		return Nil
	}
}

func (ev *Evaluator) evalHasField(expr *taxlispv1.Expression, args []*taxlispv1.Expression, env *Env) (Value, error) {
	targetVal, err := ev.EvalExpr(args[0], env)
	if err != nil {
		return nil, err
	}
	pm, ok := targetVal.(ProtoMessageVal)
	if !ok {
		return nil, errAt(args[0], "has? target must be a protobuf message, got %T", targetVal)
	}
	fieldName, err := extractQuotedOrString(args[1])
	if err != nil {
		return nil, errAt(args[1], "%v", err)
	}
	fd := pm.Msg.Descriptor().Fields().ByName(protoreflect.Name(fieldName))
	if fd == nil {
		return nil, errAt(args[1], "protobuf message %s has no field %q", pm.Msg.Descriptor().FullName(), fieldName)
	}
	return BoolVal{V: pm.Msg.Has(fd)}, nil
}

func (ev *Evaluator) evalWhichOneof(expr *taxlispv1.Expression, args []*taxlispv1.Expression, env *Env) (Value, error) {
	targetVal, err := ev.EvalExpr(args[0], env)
	if err != nil {
		return nil, err
	}
	pm, ok := targetVal.(ProtoMessageVal)
	if !ok {
		return nil, errAt(args[0], "which-oneof target must be a protobuf message, got %T", targetVal)
	}
	oneofName, err := extractQuotedOrString(args[1])
	if err != nil {
		return nil, errAt(args[1], "%v", err)
	}
	od := pm.Msg.Descriptor().Oneofs().ByName(protoreflect.Name(oneofName))
	if od == nil {
		return nil, errAt(args[1], "protobuf message %s has no oneof %q", pm.Msg.Descriptor().FullName(), oneofName)
	}
	active := pm.Msg.WhichOneof(od)
	if active == nil {
		return StringVal{V: ""}, nil
	}
	return StringVal{V: string(active.Name())}, nil
}

func (ev *Evaluator) resolveProtoDescriptor(pkgAlias, name string) (protoreflect.Descriptor, error) {
	fullPkg, ok := ev.ProtoAliases[pkgAlias]
	if !ok {
		fullPkg = pkgAlias
	}
	fullName := protoreflect.FullName(fullPkg + "." + name)
	return ev.Files.FindDescriptorByName(fullName)
}

// roundRatScale rounds r to the nearest 1/scale using half-away-from-zero rounding.
func roundRatScale(r *big.Rat, scale int64) *big.Rat {
	if r == nil || r.Sign() == 0 {
		return big.NewRat(0, 1)
	}
	sign := r.Sign()
	abs := new(big.Rat).Abs(r)
	scaled := new(big.Rat).Mul(abs, big.NewRat(scale, 1))
	// Add 1/2 for half-up on magnitude (half-away-from-zero).
	scaled.Add(scaled, big.NewRat(1, 2))
	intPart := new(big.Int).Quo(scaled.Num(), scaled.Denom())
	if sign < 0 {
		intPart.Neg(intPart)
	}
	return new(big.Rat).SetFrac(intPart, big.NewInt(scale))
}

func floorRat(r *big.Rat) *big.Rat {
	if r == nil || r.Sign() == 0 {
		return big.NewRat(0, 1)
	}
	intPart := new(big.Int).Div(r.Num(), r.Denom())
	return new(big.Rat).SetInt(intPart)
}

func ceilRat(r *big.Rat) *big.Rat {
	if r == nil || r.Sign() == 0 {
		return big.NewRat(0, 1)
	}
	neg := new(big.Rat).Neg(r)
	floored := floorRat(neg)
	return floored.Neg(floored)
}

func formatRatFixed(r *big.Rat, decimals int) string {
	if r == nil {
		return "0.00"
	}
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	rounded := roundRatScale(r, scale.Int64())
	sign := ""
	if rounded.Sign() < 0 {
		sign = "-"
	}
	abs := new(big.Rat).Abs(rounded)
	scaledNum := new(big.Int).Mul(abs.Num(), scale)
	scaledNum.Quo(scaledNum, abs.Denom())
	whole := new(big.Int).Quo(scaledNum, scale)
	frac := new(big.Int).Rem(scaledNum, scale)
	if decimals == 0 {
		return fmt.Sprintf("%s%s", sign, whole.String())
	}
	return fmt.Sprintf("%s%s.%0*s", sign, whole.String(), decimals, frac.String())
}

func floatToRat(f float64) *big.Rat {
	if math.IsNaN(f) || math.IsInf(f, 0) || f == 0 {
		return big.NewRat(0, 1)
	}
	s := strconv.FormatFloat(f, 'g', -1, 64)
	if r, ok := new(big.Rat).SetString(s); ok {
		return r
	}
	r := new(big.Rat).SetFloat64(f)
	if r == nil {
		return big.NewRat(0, 1)
	}
	return r
}

func floatToQuantityUnitsScale(f float64) (int64, int32) {
	if f == 0 || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, 0
	}
	s := strconv.FormatFloat(f, 'f', 9, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimRight(s, ".")
	}
	if s == "" || s == "-0" || s == "0" {
		return 0, 0
	}
	if idx := strings.IndexByte(s, '.'); idx >= 0 {
		scale := int32(len(s) - idx - 1)
		digits := s[:idx] + s[idx+1:]
		n, _ := strconv.ParseInt(digits, 10, 64)
		return n, scale
	}
	n, _ := strconv.ParseInt(s, 10, 64)
	return n, 0
}

func parseDefName(expr *taxlispv1.Expression) (string, error) {
	if a := expr.GetAtom(); a != nil && a.GetSymbol() != nil {
		return fullSymbolName(a.GetSymbol()), nil
	}
	if l := expr.GetList(); l != nil && len(l.GetExpressions()) > 0 {
		return symbolName(l.GetExpressions()[0]), nil
	}
	return "", errAt(expr, "invalid definition header")
}

func parseParamNames(expr *taxlispv1.Expression) ([]string, error) {
	l := expr.GetList()
	if l == nil {
		return nil, errAt(expr, "parameter list must be a list")
	}
	names := make([]string, len(l.GetExpressions()))
	for i, p := range l.GetExpressions() {
		pl := p.GetList()
		if pl == nil || len(pl.GetExpressions()) != 2 {
			return nil, errAt(p, "parameter must be (name Type)")
		}
		names[i] = symbolName(pl.GetExpressions()[0])
	}
	return names, nil
}

func isBareSymbolList(l *taxlispv1.List) bool {
	for _, e := range l.GetExpressions() {
		s := symbolName(e)
		if s == "" || (s[0] >= 'a' && s[0] <= 'z') {
			return false
		}
	}
	return true
}

func extractStructName(expr *taxlispv1.Expression) string {
	if s := symbolName(expr); s != "" {
		return s
	}
	if l := expr.GetList(); l != nil && len(l.GetExpressions()) > 0 {
		return symbolName(l.GetExpressions()[0])
	}
	return ""
}

func wrapDo(exprs []*taxlispv1.Expression) *taxlispv1.Expression {
	if len(exprs) == 1 {
		return exprs[0]
	}
	all := make([]*taxlispv1.Expression, 0, len(exprs)+1)
	all = append(all, &taxlispv1.Expression{
		Value: &taxlispv1.Expression_Atom{
			Atom: &taxlispv1.Atom{
				Primitive: &taxlispv1.Atom_Symbol{
					Symbol: &taxlispv1.Symbol{Name: "do"},
				},
			},
		},
	})
	all = append(all, exprs...)
	return &taxlispv1.Expression{
		Value: &taxlispv1.Expression_List{
			List: &taxlispv1.List{Expressions: all},
		},
	}
}

func symbolName(expr *taxlispv1.Expression) string {
	if expr == nil {
		return ""
	}
	a := expr.GetAtom()
	if a == nil || a.GetSymbol() == nil {
		return ""
	}
	return fullSymbolName(a.GetSymbol())
}

func fullSymbolName(s *taxlispv1.Symbol) string {
	if s == nil {
		return ""
	}
	if s.GetPackage() == "" {
		return s.GetName()
	}
	return s.GetPackage() + "/" + s.GetName()
}

func extractQuotedOrString(expr *taxlispv1.Expression) (string, error) {
	if a := expr.GetAtom(); a != nil {
		if sv, ok := a.GetPrimitive().(*taxlispv1.Atom_StringValue); ok {
			return sv.StringValue, nil
		}
		if a.GetSymbol() != nil {
			return fullSymbolName(a.GetSymbol()), nil
		}
	}
	if l := expr.GetList(); l != nil && len(l.GetExpressions()) == 2 && symbolName(l.GetExpressions()[0]) == "quote" {
		if a := l.GetExpressions()[1].GetAtom(); a != nil && a.GetSymbol() != nil {
			return fullSymbolName(a.GetSymbol()), nil
		}
	}
	return "", fmt.Errorf("expected quoted symbol or string")
}

func extractQuotedSymbol(expr *taxlispv1.Expression) (*taxlispv1.Symbol, error) {
	if a := expr.GetAtom(); a != nil && a.GetSymbol() != nil {
		return a.GetSymbol(), nil
	}
	if l := expr.GetList(); l != nil && len(l.GetExpressions()) == 2 && symbolName(l.GetExpressions()[0]) == "quote" {
		if a := l.GetExpressions()[1].GetAtom(); a != nil && a.GetSymbol() != nil {
			return a.GetSymbol(), nil
		}
	}
	return nil, fmt.Errorf("expected quoted symbol 'alias/TypeName")
}

func errAt(expr *taxlispv1.Expression, format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	if expr == nil {
		return fmt.Errorf("%s", msg)
	}
	var syn *taxlispv1.SyntaxDetails
	switch v := expr.GetValue().(type) {
	case *taxlispv1.Expression_Atom:
		syn = v.Atom.GetSyntax()
	case *taxlispv1.Expression_List:
		syn = v.List.GetSyntax()
	}
	if syn != nil && syn.GetStartLine() > 0 {
		file := syn.GetFile()
		if file == "" {
			file = "<input>"
		}
		return fmt.Errorf("%s:%d:%d: %s", file, syn.GetStartLine(), syn.GetStartColumn(), msg)
	}
	return fmt.Errorf("%s", msg)
}
