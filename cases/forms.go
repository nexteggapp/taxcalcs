// Package cases runs the end-to-end test cases under cases/: each a
// household and the figures expected on its returns, with where each figure
// came from. docs/testing.md describes them.
package cases

import (
	"fmt"
	"strings"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	"github.com/nexteggapp/taxcalcs/returns/rules/states"
	"github.com/nexteggapp/taxcalcs/taxlisp/expand"
	"github.com/nexteggapp/taxcalcs/taxlisp/parser"
	taxlispv1 "github.com/nexteggapp/taxcalcs/taxlisp/v1"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Form is a form the rules prepare: its output message, and the output field
// each of its lines fills.
type Form struct {
	Name    string
	Message protoreflect.FullName
	// Fields maps a line as the form prints it, such as "31a", to its output
	// field. A line with no field is not in the output and cannot be checked.
	Fields map[string]protoreflect.Name
	// Lines lists the form's lines in order.
	Lines []string
}

// Forms reads every defform in the rules.
func Forms() (map[string]*Form, error) {
	fed, err := federal.Files()
	if err != nil {
		return nil, err
	}
	st, err := states.Files()
	if err != nil {
		return nil, err
	}
	files := append(fed, st...)
	aliases := map[string]string{}
	var decls []*taxlispv1.Expression
	for _, f := range files {
		mf, err := parser.ParseFile(f.Name, f.Source)
		if err != nil {
			return nil, err
		}
		for _, e := range mf.GetExpressions() {
			switch expand.Head(e) {
			case "use-proto":
				if el := e.GetList().GetExpressions(); len(el) == 3 {
					aliases[quoted(el[2])] = quoted(el[1])
				}
			case "defform":
				decls = append(decls, e)
			}
		}
	}
	forms := map[string]*Form{}
	for _, e := range decls {
		el := e.GetList().GetExpressions()
		if len(el) < 4 {
			continue
		}
		name := el[1].GetAtom().GetSymbol().GetName()
		out := el[3].GetAtom().GetSymbol()
		pkg, ok := aliases[out.GetPackage()]
		if !ok {
			return nil, fmt.Errorf("form %s: unknown proto alias %q", name, out.GetPackage())
		}
		f := &Form{Name: name, Message: protoreflect.FullName(pkg + "." + out.GetName()), Fields: map[string]protoreflect.Name{}}
		lines(el[4:], f)
		forms[name] = f
	}
	return forms, nil
}

// lines records the lines among a form's items, inside parts or not.
func lines(items []*taxlispv1.Expression, f *Form) {
	for _, it := range items {
		pe := it.GetList().GetExpressions()
		switch expand.Head(it) {
		case "part":
			lines(pe[2:], f)
		case "line":
			n, ok := expand.LineNumber(pe[1])
			if !ok {
				continue
			}
			f.Lines = append(f.Lines, n)
			if len(pe) == 6 {
				f.Fields[n] = protoreflect.Name(pe[5].GetAtom().GetSymbol().GetName())
			}
		}
	}
}

// quoted is the symbol a quote form names, as pkg/name or name.
func quoted(e *taxlispv1.Expression) string {
	el := e.GetList().GetExpressions()
	if expand.Head(e) != "quote" || len(el) != 2 {
		return ""
	}
	s := el[1].GetAtom().GetSymbol()
	if s.GetPackage() != "" {
		return s.GetPackage() + "/" + s.GetName()
	}
	return strings.TrimSpace(s.GetName())
}
