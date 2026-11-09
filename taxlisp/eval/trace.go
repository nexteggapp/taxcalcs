package eval

import (
	"sort"
	"strconv"
	"strings"

	"github.com/nexteggapp/taxcalcs/taxlisp/parser"
	taxlispv1 "github.com/nexteggapp/taxcalcs/taxlisp/v1"
)

// Trace records how a value was derived: the named functions called, the
// lines of forms and worksheets computed, and the conditions of all-of and
// any-of checked, each with its value, nested as they were evaluated.
//
// Set Evaluator.Trace to record one; leave it nil to evaluate without.
type Trace struct {
	// Include says which named functions to record. Calls to the others are
	// evaluated without a node, so what they call is recorded in the caller's
	// node. Nil records every named function.
	Include func(name string) bool

	// Root holds the top-level nodes.
	Root *Node

	stack []*Node
}

// Node is one step of a derivation.
type Node struct {
	// Kind is "call", "line" or "check".
	Kind string `json:"kind"`

	// Name is the function called, or the form or worksheet a line is on.
	Name string `json:"name,omitempty"`

	// Line is a line's number.
	Line string `json:"line,omitempty"`

	// Caption is a line's caption, or a check's condition as written.
	Caption string `json:"caption,omitempty"`

	// Args are a call's arguments, those simple enough to show.
	Args []Arg `json:"args,omitempty"`

	// Value is the result, as text.
	Value string `json:"value"`

	// Children are the steps evaluated to produce it, in order.
	Children []*Node `json:"children,omitempty"`
}

// Arg is a call's argument.
type Arg struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// NewTrace returns an empty trace that records the named functions include
// accepts.
func NewTrace(include func(name string) bool) *Trace {
	return &Trace{Include: include, Root: &Node{Kind: "root"}}
}

func (t *Trace) open(n *Node) {
	parent := t.Root
	if len(t.stack) > 0 {
		parent = t.stack[len(t.stack)-1]
	}
	parent.Children = append(parent.Children, n)
	t.stack = append(t.stack, n)
}

func (t *Trace) close(v Value) {
	n := t.stack[len(t.stack)-1]
	t.stack = t.stack[:len(t.stack)-1]
	if v != nil {
		n.Value = Display(v)
	}
}

func (t *Trace) includes(name string) bool {
	return name != "" && (t.Include == nil || t.Include(name))
}

// Display formats a value for a derivation: scalars in full, a message or
// struct by its type, and a slice by its length when it holds either.
func Display(v Value) string {
	switch x := v.(type) {
	case MoneyVal:
		return x.FormatCents()
	case ProtoMessageVal:
		return "<" + string(x.Msg.Descriptor().Name()) + ">"
	case StructVal:
		return "<" + x.TypeName + ">"
	case SliceVal:
		simple := true
		for _, e := range x.Elems {
			switch e.(type) {
			case ProtoMessageVal, StructVal, SliceVal, TableVal:
				simple = false
			}
		}
		if !simple {
			return "<" + strconv.Itoa(len(x.Elems)) + " items>"
		}
		parts := make([]string, len(x.Elems))
		for i, e := range x.Elems {
			parts[i] = Display(e)
		}
		return "[" + strings.Join(parts, " ") + "]"
	case TableVal:
		return "<table>"
	case ClosureVal, BuiltinVal, StructCtorVal:
		return "<fn>"
	}
	return v.String()
}

// Names returns the names of the functions a trace called, sorted.
func (t *Trace) Names() []string {
	seen := map[string]bool{}
	var walk func(n *Node)
	walk = func(n *Node) {
		if n.Kind == "call" {
			seen[n.Name] = true
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(t.Root)
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// formatCondition writes a condition as it reads in the source, on one line.
func formatCondition(e *taxlispv1.Expression) string {
	return strings.Join(strings.Fields(parser.FormatExpr(e)), " ")
}
