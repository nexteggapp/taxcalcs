// Package expand rewrites the declarations that describe tax forms and their
// figures into the core forms the typechecker and evaluator know.
//
//	(defform name ((arg Type) ...) OutputProto :cite (...) item ...)
//	(defworksheet name ((arg Type) ...) ResultType :cite (...) item ... (result expr))
//
// A form or worksheet is a list of numbered, captioned lines. Its items are:
//
//	(part "caption" item ...)          a group of lines, for the reader
//	(line N "caption" expr)            line N, bound as lineN for later lines
//	(line N "caption" expr :field f)   on a form, also the output's field f
//	(attach name "caption" expr)       a schedule or worksheet the form uses
//	(bind name expr)                   a value the lines share
//	(set :field expr)                  an output field that is not a line
//
// A form returns its output proto with the fields its lines and sets name. A
// worksheet returns its result. Each line is recorded in a derivation.
//
//	(deffigures name Type year (from source-id (field value :at anchor) ...) ...)
//	(deffigure name Type year :src source-id :at anchor value)
//
// A figure set is a struct of the year's published figures, each naming the
// part of the source that prints it. The struct's tax_year is the year.
//
// The expansions keep the declaration's leading comments, which are its
// documentation, on the def they produce.
package expand

import (
	"fmt"
	"regexp"

	taxlispv1 "github.com/nexteggapp/taxcalcs/taxlisp/v1"
)

// File expands every declaration in mf, in place.
func File(mf *taxlispv1.ModuleFile) error {
	for i, e := range mf.GetExpressions() {
		out, err := Expr(e)
		if err != nil {
			return fmt.Errorf("%s: %w", mf.GetPath(), err)
		}
		mf.Expressions[i] = out
	}
	return nil
}

// Expr expands one top-level declaration. Anything else is returned as is.
func Expr(e *taxlispv1.Expression) (*taxlispv1.Expression, error) {
	l := e.GetList()
	if l == nil || len(l.GetExpressions()) == 0 {
		return e, nil
	}
	switch Head(e) {
	case "defform":
		return form(e, true)
	case "defworksheet":
		return form(e, false)
	case "deffigures":
		return figures(e)
	case "deffigure":
		return figure(e)
	}
	return e, nil
}

// Head returns the symbol a list starts with, or "".
func Head(e *taxlispv1.Expression) string {
	l := e.GetList()
	if l == nil || len(l.GetExpressions()) == 0 {
		return ""
	}
	return l.GetExpressions()[0].GetAtom().GetSymbol().GetName()
}

// LineName is the symbol a line is bound to: line31, line1a.
func LineName(n string) string {
	return "line" + nonWord.ReplaceAllString(n, "")
}

var nonWord = regexp.MustCompile(`[^A-Za-z0-9]`)

// LineNumber returns a line's number as written: 31, "1a".
func LineNumber(e *taxlispv1.Expression) (string, bool) {
	switch p := e.GetAtom().GetPrimitive().(type) {
	case *taxlispv1.Atom_IntValue:
		return fmt.Sprint(p.IntValue), true
	case *taxlispv1.Atom_StringValue:
		return p.StringValue, true
	}
	return "", false
}

func form(e *taxlispv1.Expression, isForm bool) (*taxlispv1.Expression, error) {
	kind := Head(e)
	elems := e.GetList().GetExpressions()
	if len(elems) < 4 {
		return nil, errAt(e, "%s requires a name, parameters and a type", kind)
	}
	name := elems[1].GetAtom().GetSymbol().GetName()
	header := []*taxlispv1.Expression{sym("def"), elems[1], elems[2], elems[3]}
	i := 4
	for ; i+1 < len(elems) && elems[i].GetAtom().GetKeyword() != ""; i += 2 {
		header = append(header, elems[i], elems[i+1])
	}

	var bindings []*taxlispv1.Expression
	var fields []*taxlispv1.Expression
	var result *taxlispv1.Expression
	var walk func(items []*taxlispv1.Expression) error
	walk = func(items []*taxlispv1.Expression) error {
		for _, it := range items {
			parts := it.GetList().GetExpressions()
			switch Head(it) {
			case "part":
				if len(parts) < 2 || parts[1].GetAtom().GetStringValue() == "" {
					return errAt(it, "a part needs a caption")
				}
				if err := walk(parts[2:]); err != nil {
					return err
				}
			case "line":
				if len(parts) != 4 && len(parts) != 6 {
					return errAt(it, "(line N \"caption\" expr [:field name])")
				}
				n, ok := LineNumber(parts[1])
				if !ok {
					return errAt(parts[1], "a line number is an integer or a string")
				}
				caption := parts[2].GetAtom().GetStringValue()
				if caption == "" {
					return errAt(it, "line %s needs a caption", n)
				}
				bindings = append(bindings, sym(LineName(n)), list(
					sym("traced-line"), str(name), str(n), str(caption), parts[3]))
				if len(parts) == 6 {
					if !isForm || parts[4].GetAtom().GetKeyword() != "field" {
						return errAt(it, "only a form's line can name a :field")
					}
					fields = append(fields, kw(parts[5].GetAtom().GetSymbol().GetName()), sym(LineName(n)))
				}
			case "attach":
				if len(parts) != 4 {
					return errAt(it, "(attach name \"caption\" expr)")
				}
				bindings = append(bindings, parts[1], parts[3])
			case "bind":
				if len(parts) != 3 {
					return errAt(it, "(bind name expr)")
				}
				bindings = append(bindings, parts[1], parts[2])
			case "set":
				if !isForm || len(parts) != 3 || parts[1].GetAtom().GetKeyword() == "" {
					return errAt(it, "(set :field expr), on a form")
				}
				fields = append(fields, parts[1], parts[2])
			case "result":
				if isForm || len(parts) != 2 {
					return errAt(it, "(result expr), once, on a worksheet")
				}
				result = parts[1]
			default:
				return errAt(it, "%s cannot hold %s", kind, Head(it))
			}
		}
		return nil
	}
	if err := walk(elems[i:]); err != nil {
		return nil, err
	}
	if isForm {
		result = list(append([]*taxlispv1.Expression{sym("proto"), quote(elems[3])}, fields...)...)
	} else if result == nil {
		return nil, errAt(e, "a worksheet needs a (result expr)")
	}
	body := list(sym("withs"), list(bindings...), result)
	out := list(append(header, body)...)
	out.GetList().Syntax = e.GetList().GetSyntax()
	return out, nil
}

func figures(e *taxlispv1.Expression) (*taxlispv1.Expression, error) {
	elems := e.GetList().GetExpressions()
	if len(elems) < 5 {
		return nil, errAt(e, "(deffigures name Type year (from source (field value :at anchor) ...) ...)")
	}
	args := []*taxlispv1.Expression{sym("make-struct"), elems[2], kw("tax_year"), elems[3]}
	for _, group := range elems[4:] {
		g := group.GetList().GetExpressions()
		if Head(group) != "from" || len(g) < 2 || g[1].GetAtom().GetSymbol() == nil {
			return nil, errAt(group, "figures are grouped as (from source-id (field value :at anchor) ...)")
		}
		for _, f := range g[2:] {
			fl := f.GetList().GetExpressions()
			if len(fl) != 4 || fl[0].GetAtom().GetSymbol() == nil || fl[2].GetAtom().GetKeyword() != "at" {
				return nil, errAt(f, "a figure is (field value :at anchor)")
			}
			args = append(args, kw(fl[0].GetAtom().GetSymbol().GetName()), fl[1])
		}
	}
	out := list(sym("="), elems[1], elems[2], list(args...))
	out.GetList().Syntax = e.GetList().GetSyntax()
	return out, nil
}

func figure(e *taxlispv1.Expression) (*taxlispv1.Expression, error) {
	elems := e.GetList().GetExpressions()
	if len(elems) != 9 || elems[4].GetAtom().GetKeyword() != "src" || elems[6].GetAtom().GetKeyword() != "at" {
		return nil, errAt(e, "(deffigure name Type year :src source-id :at anchor value)")
	}
	out := list(sym("="), elems[1], elems[2], elems[8])
	out.GetList().Syntax = e.GetList().GetSyntax()
	return out, nil
}

func sym(name string) *taxlispv1.Expression {
	return &taxlispv1.Expression{Value: &taxlispv1.Expression_Atom{Atom: &taxlispv1.Atom{
		Primitive: &taxlispv1.Atom_Symbol{Symbol: &taxlispv1.Symbol{Name: name}}}}}
}

func str(s string) *taxlispv1.Expression {
	return &taxlispv1.Expression{Value: &taxlispv1.Expression_Atom{Atom: &taxlispv1.Atom{
		Primitive: &taxlispv1.Atom_StringValue{StringValue: s}}}}
}

func kw(name string) *taxlispv1.Expression {
	return &taxlispv1.Expression{Value: &taxlispv1.Expression_Atom{Atom: &taxlispv1.Atom{
		Primitive: &taxlispv1.Atom_Keyword{Keyword: name}}}}
}

func list(es ...*taxlispv1.Expression) *taxlispv1.Expression {
	return &taxlispv1.Expression{Value: &taxlispv1.Expression_List{List: &taxlispv1.List{Expressions: es}}}
}

func quote(e *taxlispv1.Expression) *taxlispv1.Expression {
	return list(sym("quote"), e)
}

func errAt(e *taxlispv1.Expression, format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	if syn := e.GetList().GetSyntax(); syn != nil {
		return fmt.Errorf("%s:%d:%d: %s", syn.GetFile(), syn.GetStartLine(), syn.GetStartColumn(), msg)
	}
	if syn := e.GetAtom().GetSyntax(); syn != nil {
		return fmt.Errorf("%s:%d:%d: %s", syn.GetFile(), syn.GetStartLine(), syn.GetStartColumn(), msg)
	}
	return fmt.Errorf("%s", msg)
}
