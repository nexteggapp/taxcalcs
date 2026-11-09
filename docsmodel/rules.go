package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/nexteggapp/taxcalcs/returns/rules/states"
	"github.com/nexteggapp/taxcalcs/taxlisp/expand"
	"github.com/nexteggapp/taxcalcs/taxlisp/parser"
	taxlispv1 "github.com/nexteggapp/taxcalcs/taxlisp/v1"
)

// Expr is an expression as the site renders it.
type Expr struct {
	// K is "list", "sym", "kw", "str", "int", "float", "money", "bool" or
	// "quote".
	K string  `json:"k"`
	V string  `json:"v,omitempty"`
	A []*Expr `json:"a,omitempty"`
}

// documented says which rule files the site documents: the shared state
// helpers and California's.
func documented(name string) bool {
	return name == "00_states.tlisp" || strings.HasPrefix(name, "ca/")
}

func readRules(m *Model) error {
	files, err := states.Files()
	if err != nil {
		return err
	}
	for _, f := range files {
		if !documented(f.Name) {
			continue
		}
		path := "returns/rules/states/" + f.Name
		mf, err := parser.ParseFile(path, f.Source)
		if err != nil {
			return err
		}
		lines := strings.Split(f.Source, "\n")
		for _, e := range mf.GetExpressions() {
			if expand.Head(e) == "use-proto" {
				if el := e.GetList().GetExpressions(); len(el) == 3 {
					protoAliases[quoted(el[2])] = quoted(el[1])
				}
			}
			if err := readDecl(m, path, lines, e); err != nil {
				return err
			}
		}
	}
	names := map[string]bool{}
	for _, r := range m.Rules {
		names[r.Name] = true
	}
	sets := map[string]bool{}
	for _, fs := range m.FigureSets {
		sets[fs.Name] = true
	}
	for _, r := range m.Rules {
		r.Calls, r.Figures = references(r, names, sets)
	}
	// A figure's description is its struct field's comment.
	for _, fs := range m.FigureSets {
		for _, r := range m.Rules {
			if r.Kind != "struct" || r.Name != fs.Type {
				continue
			}
			for _, f := range fs.Figures {
				for _, p := range r.Params {
					if p.Name == f.Field {
						f.Doc = p.Doc
					}
				}
			}
		}
	}
	return nil
}

func readDecl(m *Model, path string, lines []string, e *taxlispv1.Expression) error {
	elems := e.GetList().GetExpressions()
	syn := e.GetList().GetSyntax()
	if len(elems) < 2 {
		return nil
	}
	start, end := int(syn.GetStartLine()), int(syn.GetEndLine())
	doc := docAbove(lines, start)
	src := strings.Join(lines[start-1:min(end, len(lines))], "\n")
	head := expand.Head(e)
	name := elems[1].GetAtom().GetSymbol().GetName()
	switch head {
	case "def", "defform", "defworksheet":
		if name == "" {
			// A generic def names itself in a list: (def (name T) ...).
			name = elems[1].GetList().GetExpressions()[0].GetAtom().GetSymbol().GetName()
		}
		r := &Rule{Name: name, File: path, Line: start, Doc: doc, Source: src, decl: e}
		switch head {
		case "def":
			r.Kind = "def"
			if strings.HasSuffix(name, "?") {
				r.Kind = "predicate"
			}
		case "defform":
			r.Kind = "form"
		case "defworksheet":
			r.Kind = "worksheet"
		}
		for _, p := range elems[2].GetList().GetExpressions() {
			pe := p.GetList().GetExpressions()
			if len(pe) == 2 {
				r.Params = append(r.Params, Param{Name: pe[0].GetAtom().GetSymbol().GetName(), Type: parser.FormatExpr(pe[1])})
			}
		}
		r.Returns = parser.FormatExpr(elems[3])
		i := 4
		for ; i+1 < len(elems) && elems[i].GetAtom().GetKeyword() != ""; i += 2 {
			if elems[i].GetAtom().GetKeyword() == "cite" {
				r.Cites = cites(elems[i+1])
			}
		}
		if head == "def" {
			if i < len(elems) {
				r.Body = toExpr(elems[len(elems)-1])
			}
		} else {
			r.Parts = parts(elems[i:])
		}
		m.Rules = append(m.Rules, r)
	case "defstruct":
		m.Rules = append(m.Rules, &Rule{Name: name, Kind: "struct", File: path, Line: start, Doc: doc, Source: src, Params: structFields(elems[2:])})
	case "deffigures":
		year, _ := strconv.Atoi(parser.FormatExpr(elems[3]))
		fs := &FigureSet{Name: name, Type: parser.FormatExpr(elems[2]), Year: year, Doc: doc, File: path, Line: start, Source: src}
		for _, g := range elems[4:] {
			ge := g.GetList().GetExpressions()
			srcID := ge[1].GetAtom().GetSymbol().GetName()
			for _, f := range ge[2:] {
				fe := f.GetList().GetExpressions()
				fs.Figures = append(fs.Figures, &Figure{
					Field:  fe[0].GetAtom().GetSymbol().GetName(),
					Value:  literal(fe[1]),
					Source: srcID,
					Anchor: fe[3].GetAtom().GetSymbol().GetName(),
				})
			}
		}
		m.FigureSets = append(m.FigureSets, fs)
	case "deffigure":
		year, _ := strconv.Atoi(parser.FormatExpr(elems[3]))
		fs := &FigureSet{Name: name, Type: parser.FormatExpr(elems[2]), Year: year, Doc: doc, File: path, Line: start, Source: src}
		f := &Figure{
			Field:  name,
			Source: elems[5].GetAtom().GetSymbol().GetName(),
			Anchor: elems[7].GetAtom().GetSymbol().GetName(),
			Value:  literal(elems[8]),
			Rows:   rows(elems[8]),
		}
		fs.Figures = []*Figure{f}
		m.FigureSets = append(m.FigureSets, fs)
	}
	return nil
}

// docAbove is the block of comment lines directly above line n (1-based).
func docAbove(lines []string, n int) string {
	var out []string
	for i := n - 2; i >= 0; i-- {
		t := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(t, ";") {
			break
		}
		out = append([]string{strings.TrimSpace(strings.TrimLeft(t, ";"))}, out...)
	}
	return joinParagraphs(out)
}

// joinParagraphs joins comment lines into paragraphs, splitting at empty
// lines.
func joinParagraphs(lines []string) string {
	var paras []string
	var cur []string
	for _, l := range lines {
		if l == "" {
			if len(cur) > 0 {
				paras = append(paras, strings.Join(cur, " "))
				cur = nil
			}
			continue
		}
		cur = append(cur, l)
	}
	if len(cur) > 0 {
		paras = append(paras, strings.Join(cur, " "))
	}
	return strings.Join(paras, "\n\n")
}

func cites(e *taxlispv1.Expression) []Cite {
	var out []Cite
	for _, c := range e.GetList().GetExpressions() {
		if s, ok := c.GetAtom().GetPrimitive().(*taxlispv1.Atom_StringValue); ok {
			out = append(out, Cite{Text: s.StringValue})
			continue
		}
		ce := c.GetList().GetExpressions()
		if expand.Head(c) == "law" && len(ce) >= 3 {
			ct := Cite{Code: ce[1].GetAtom().GetStringValue(), Section: ce[2].GetAtom().GetStringValue()}
			if len(ce) > 3 {
				ct.Sub = ce[3].GetAtom().GetStringValue()
			}
			out = append(out, ct)
		}
	}
	return out
}

func parts(items []*taxlispv1.Expression) []*Part {
	var out []*Part
	loose := &Part{}
	for _, it := range items {
		if expand.Head(it) == "part" {
			pe := it.GetList().GetExpressions()
			p := &Part{Caption: pe[1].GetAtom().GetStringValue()}
			for _, x := range pe[2:] {
				if item := toItem(x); item != nil {
					p.Items = append(p.Items, item)
				}
			}
			out = append(out, p)
			continue
		}
		if item := toItem(it); item != nil {
			loose.Items = append(loose.Items, item)
		}
	}
	if len(loose.Items) > 0 {
		out = append([]*Part{loose}, out...)
	}
	return out
}

func toItem(e *taxlispv1.Expression) *Item {
	pe := e.GetList().GetExpressions()
	switch expand.Head(e) {
	case "line":
		n, _ := expand.LineNumber(pe[1])
		it := &Item{Kind: "line", Number: n, Name: expand.LineName(n), Caption: pe[2].GetAtom().GetStringValue(), Expr: toExpr(pe[3])}
		if len(pe) == 6 {
			it.Field = pe[5].GetAtom().GetSymbol().GetName()
		}
		return it
	case "attach":
		return &Item{Kind: "attach", Name: pe[1].GetAtom().GetSymbol().GetName(), Caption: pe[2].GetAtom().GetStringValue(), Expr: toExpr(pe[3])}
	case "bind":
		return &Item{Kind: "bind", Name: pe[1].GetAtom().GetSymbol().GetName(), Expr: toExpr(pe[2])}
	case "set":
		return &Item{Kind: "set", Name: pe[1].GetAtom().GetKeyword(), Field: pe[1].GetAtom().GetKeyword(), Expr: toExpr(pe[2])}
	case "result":
		return &Item{Kind: "result", Name: "result", Expr: toExpr(pe[1])}
	}
	return nil
}

func structFields(fields []*taxlispv1.Expression) []Param {
	var out []Param
	doc := ""
	for _, f := range fields {
		fe := f.GetList().GetExpressions()
		if c := f.GetList().GetSyntax().GetLeadingComments(); len(c) > 0 {
			for i := range c {
				c[i] = strings.TrimSpace(c[i])
			}
			doc = joinParagraphs(c)
		}
		if len(fe) == 2 {
			out = append(out, Param{Name: fe[0].GetAtom().GetSymbol().GetName(), Type: parser.FormatExpr(fe[1]), Doc: doc})
		}
	}
	return out
}

func toExpr(e *taxlispv1.Expression) *Expr {
	if l := e.GetList(); l != nil {
		es := l.GetExpressions()
		if expand.Head(e) == "quote" && len(es) == 2 {
			return &Expr{K: "quote", A: []*Expr{toExpr(es[1])}}
		}
		x := &Expr{K: "list"}
		for _, c := range es {
			x.A = append(x.A, toExpr(c))
		}
		return x
	}
	a := e.GetAtom()
	switch p := a.GetPrimitive().(type) {
	case *taxlispv1.Atom_Symbol:
		return &Expr{K: "sym", V: parser.FormatExpr(e)}
	case *taxlispv1.Atom_Keyword:
		return &Expr{K: "kw", V: p.Keyword}
	case *taxlispv1.Atom_StringValue:
		return &Expr{K: "str", V: p.StringValue}
	case *taxlispv1.Atom_IntValue:
		return &Expr{K: "int", V: strconv.FormatInt(p.IntValue, 10)}
	case *taxlispv1.Atom_FloatValue:
		return &Expr{K: "float", V: strconv.FormatFloat(p.FloatValue, 'g', -1, 64)}
	case *taxlispv1.Atom_Money:
		return &Expr{K: "money", V: fmt.Sprintf("%d.%02d", p.Money.GetMinor()/100, abs(p.Money.GetMinor()%100))}
	case *taxlispv1.Atom_BoolValue:
		return &Expr{K: "bool", V: strconv.FormatBool(p.BoolValue)}
	}
	return &Expr{K: "sym", V: parser.FormatExpr(e)}
}

func abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

// literal is a figure's value as text.
func literal(e *taxlispv1.Expression) string {
	x := toExpr(e)
	if x.K == "list" {
		return parser.FormatExpr(e)
	}
	return x.V
}

// rows reads a table of brackets: (StateBracket low high rate-bp) entries.
func rows(e *taxlispv1.Expression) [][]string {
	l := e.GetList()
	if l == nil {
		return nil
	}
	var out [][]string
	for _, r := range l.GetExpressions() {
		re := r.GetList().GetExpressions()
		if len(re) != 4 {
			continue
		}
		out = append(out, []string{literal(re[1]), literal(re[2]), literal(re[3])})
	}
	return out
}

// references lists the rules a rule calls and the figures it reads: a field
// of a figure struct, as Type.field, or a figure set by name.
func references(r *Rule, rules, sets map[string]bool) (calls, figures []string) {
	figTypes := map[string]string{}
	for _, p := range r.Params {
		if strings.HasSuffix(p.Type, "Year") {
			figTypes[p.Name] = p.Type
		}
	}
	seenC, seenF := map[string]bool{}, map[string]bool{}
	var walk func(x *Expr)
	walk = func(x *Expr) {
		if x == nil {
			return
		}
		switch x.K {
		case "sym":
			if rules[x.V] && x.V != r.Name && !seenC[x.V] {
				seenC[x.V] = true
				calls = append(calls, x.V)
			}
			if sets[x.V] && !seenF[x.V] {
				seenF[x.V] = true
				figures = append(figures, x.V)
			}
		case "list":
			if len(x.A) == 3 && x.A[0].K == "sym" && x.A[0].V == "." && x.A[1].K == "sym" && x.A[2].K == "quote" {
				if t, ok := figTypes[x.A[1].V]; ok {
					f := t + "." + x.A[2].A[0].V
					if !seenF[f] {
						seenF[f] = true
						figures = append(figures, f)
					}
				}
			}
		}
		for _, c := range x.A {
			walk(c)
		}
	}
	walk(r.Body)
	for _, p := range r.Parts {
		for _, it := range p.Items {
			walk(it.Expr)
		}
	}
	return calls, figures
}
