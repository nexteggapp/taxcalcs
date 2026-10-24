package estimatedtest

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// CheckSource returns each sentence over [MaxSentence] characters in the
// string literals of one package's Go source, whether or not any test
// household reaches it. files maps each file's name, as problems report it,
// to its contents; they must all be one package.
//
// A literal counts as copy unless it is plainly not read as a sentence:
//   - an argument to a call in notShownCalls, such as an error or a rule's
//     working note, which the payments API does not return;
//   - the value of a field, constant, variable or function parameter whose
//     name says it only feeds a working note (says, applied);
//   - a citation, which a person sees as the title of a document, not as
//     prose: an explanation's sources, the sources a divergence's Form and
//     Statute take after their text, a rate period's source, and any field,
//     constant, variable or parameter named cite, citation or source.
//
// A format string is measured with each verb filled in: %s with the longest
// value the argument can take, read from the literals it is built from (see
// [resolver]); %d with "2026" for a year and "12" otherwise; any other verb
// with "110%" for a share or rate, "September 30, 2027" for a date and
// "$123,456" otherwise. A %s argument whose longest value cannot be read
// from the source is a problem too, until bounds gives it one.
//
// A literal with no space is a word or a key, not copy.
func CheckSource(files map[string][]byte) ([]string, error) {
	fs := token.NewFileSet()
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var parsed []*ast.File
	for _, name := range names {
		f, err := parser.ParseFile(fs, name, files[name], 0)
		if err != nil {
			return nil, err
		}
		parsed = append(parsed, f)
	}
	pkg := ""
	if len(names) > 0 {
		pkg = path.Dir(names[0])
	}
	r := newResolver(pkg, parsed)
	var out []string
	for _, l := range r.labels {
		s, ok := r.longest(l.expr, 0)
		if !ok {
			continue // the literal check reports an argument it cannot bound
		}
		if n := utf8.RuneCountInString(s + l.suffix); n > MaxSentence {
			where := fs.Position(l.expr.Pos())
			out = append(out, fmt.Sprintf("%s:%d: an option label of %d characters, more than %d: %q", where.Filename, where.Line, n, MaxSentence, s+l.suffix))
		}
	}
	for _, f := range parsed {
		var stack []ast.Node
		ast.Inspect(f, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			stack = append(stack, n)
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			s, err := strconv.Unquote(lit.Value)
			if err != nil || !strings.Contains(s, " ") {
				return true
			}
			where := fs.Position(lit.Pos())
			at := fmt.Sprintf("%s:%d", where.Filename, where.Line)
			calls, name := consumers(stack)
			if notShownName.MatchString(name) || citationName.MatchString(name) || citations[pkg+":"+name] {
				return true
			}
			for _, c := range calls {
				ps := r.params[c.name]
				switch {
				case notShownCalls[c.name], citeCalls[c.name], citedArgs[c.name] && c.arg >= 1:
					return true
				case c.arg < len(ps) && (notShownName.MatchString(ps[c.arg]) || citationName.MatchString(ps[c.arg])):
					return true
				}
			}
			if len(calls) > 0 && calls[0].name == "Sprintf" && calls[0].arg == 0 {
				filled, unbounded := r.fill(s, calls[0].call.Args[1:])
				for _, u := range unbounded {
					out = append(out, fmt.Sprintf("%s: no bound for the %%s argument %s; add one to bounds", at, u))
				}
				s = filled
			}
			for _, f := range fragments {
				if m := f.re.FindString(s); m != "" {
					out = append(out, fmt.Sprintf("%s: %q is %s", at, m, f.rule))
				}
			}
			for _, sentence := range Sentences(s) {
				if n := utf8.RuneCountInString(sentence); n > MaxSentence {
					out = append(out, fmt.Sprintf("%s: a sentence of %d characters, more than %d: %q", at, n, MaxSentence, sentence))
				}
			}
			return true
		})
	}
	return out, nil
}

// notShownCalls take text the payments API never returns: errors, a rule's
// working note and its citation, and a calendar's holidays.
var notShownCalls = map[string]bool{
	"Errorf": true, "errors.New": true, "panic": true, "Panicf": true,
	"Rule": true, "NewRule": true, "Holiday": true, "NewCalendar": true, "MustCompile": true,
}

// citedArgs take a citation after their text, as a divergence's Form and
// Statute do. citeCalls take nothing but citations.
var (
	citedArgs = map[string]bool{"Form": true, "Statute": true}
	citeCalls = map[string]bool{"Source": true, "Period": true, "FinePeriod": true}
)

var (
	// citationName is a field, constant, variable or parameter holding a
	// citation.
	citationName = regexp.MustCompile(`(?i)cite|citation|source`)
	// notShownName is one holding only a working note's text.
	notShownName = regexp.MustCompile(`(?i)says|applied`)
)

// citations are constants used only as citations whose names don't say so,
// keyed by package directory and name.
var citations = map[string]bool{
	// Appended to divergence sources.
	"louisianataxcalc:outOfDate": true,
}

// bounds gives the longest value of a %s argument the source alone cannot
// bound, keyed by package directory and the argument's expression. Each
// value is a string as long as the longest the argument takes.
var bounds = map[string]string{
	// The longest authority any jurisdiction passes to StandardWaiver.
	"estimated:authority": "the Minnesota Department of Revenue",
	// An exception's what, as long as a label leaves room for: each
	// jurisdiction's own check measures the label it builds.
	"estimated:what": strings.Repeat("x", MaxSentence-len(" of $123,456 or less")),
	// A function-valued field formatting a date.
	"missouritaxcalc:y.date(unrated.AddDays(-1))": "September 30, 2027",
	// At most the two clauses the requirement adds.
	`districtofcolumbiataxcalc:strings.Join(residenceQuestions, " and whether ")`: "the 110% option is open and whether having no DC tax that year excuses you",
	// A factor as D-2210 prints it.
	"districtofcolumbiataxcalc:y.factors[i]": "0.0265",
	// near's rounded amount, or the full figures.
	"iowataxcalc:leaves": "is $123,456, under $123,456",
	// The coupon's date is February 15, whose only holiday is Washington's
	// Birthday; otherwise it is "a weekend".
	"rhodeislandtaxcalc:holidayName(y.calendar, y.couponBy)": "Washington's Birthday",
}

// consumers names the calls a literal is an argument to, nearest first,
// passing through fmt.Sprintf and string concatenation, and the name it is
// bound to: a struct field's key, or the constant or variable it is
// assigned to.
func consumers(stack []ast.Node) (calls []call, name string) {
	for i := len(stack) - 2; i >= 0; i-- {
		child := stack[i+1]
		switch p := stack[i].(type) {
		case *ast.CallExpr:
			for j, a := range p.Args {
				if a == child {
					calls = append(calls, call{callName(p), j, p})
				}
			}
		case *ast.KeyValueExpr:
			if id, ok := p.Key.(*ast.Ident); ok && name == "" {
				name = id.Name
			}
		case *ast.ValueSpec:
			if name == "" && len(p.Names) > 0 {
				name = p.Names[0].Name
			}
		case *ast.AssignStmt:
			if name == "" {
				name = lastName(p.Lhs[0])
			}
		}
	}
	return calls, name
}

// call is a function or method a literal is passed to, and the argument's
// position.
type call struct {
	name string
	arg  int
	call *ast.CallExpr
}

// callName is "errors.New" for that package function, and the bare method
// or function name otherwise.
func callName(c *ast.CallExpr) string {
	switch f := c.Fun.(type) {
	case *ast.SelectorExpr:
		if x, ok := f.X.(*ast.Ident); ok && x.Name == "errors" {
			return "errors." + f.Sel.Name
		}
		return f.Sel.Name
	case *ast.Ident:
		return f.Name
	}
	return ""
}

// lastName is x for x, a.x and a.b.x.
func lastName(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return e.Sel.Name
	}
	return ""
}

// resolver finds the longest string an expression can be, from the
// literals the package builds it from. It knows a local name by every value
// its function assigns to it, a package name by its declarations, a field
// by every composite literal and assignment that sets it, a function by its
// return statements, and the library's money and date formatters by their
// widest output. It cannot see through a parameter or a function it does
// not know; bounds covers those.
type resolver struct {
	pkg     string
	values  map[string][]ast.Expr // package-level names
	fields  map[string][]ast.Expr // fields, by composite literal key or x.f assignment
	returns map[string][]ast.Expr
	params  map[string][]string
	// scopes are the function bodies, each with the names it assigns. A
	// name is read from the innermost body around its use that assigns it.
	scopes []scope
	// label is the longest option label the package sets, and labels the
	// labels it builds, for [CheckSource] to measure.
	label  string
	labels []builtLabel
	busy   map[ast.Expr]bool
}

type scope struct {
	from, to token.Pos
	values   map[string][]ast.Expr
	appends  map[string][]ast.Expr
}

// builtLabel is an option label the package sets: the expression, and what
// the library adds to it.
type builtLabel struct {
	expr   ast.Expr
	suffix string
}

func newResolver(pkg string, files []*ast.File) *resolver {
	r := &resolver{pkg: pkg, values: map[string][]ast.Expr{}, fields: map[string][]ast.Expr{},
		returns: map[string][]ast.Expr{}, params: map[string][]string{}, busy: map[ast.Expr]bool{}}
	addFunc := func(name string, t *ast.FuncType, body *ast.BlockStmt) {
		var names []string
		for _, field := range t.Params.List {
			if len(field.Names) == 0 {
				names = append(names, "")
			}
			for _, n := range field.Names {
				names = append(names, n.Name)
			}
		}
		r.params[name] = names
		if body == nil {
			return
		}
		sc := scope{from: body.Pos(), to: body.End(), values: map[string][]ast.Expr{}, appends: map[string][]ast.Expr{}}
		ast.Inspect(body, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.FuncLit:
				return false
			case *ast.ReturnStmt:
				if len(n.Results) > 0 {
					r.returns[name] = append(r.returns[name], n.Results[0])
				}
			case *ast.AssignStmt:
				for i, rhs := range n.Rhs {
					if i >= len(n.Lhs) {
						break
					}
					id, ok := n.Lhs[i].(*ast.Ident)
					if !ok {
						continue
					}
					if n.Tok == token.ADD_ASSIGN {
						sc.appends[id.Name] = append(sc.appends[id.Name], rhs)
					} else {
						sc.values[id.Name] = append(sc.values[id.Name], rhs)
					}
				}
			case *ast.ValueSpec:
				for i, v := range n.Values {
					if i < len(n.Names) {
						sc.values[n.Names[i].Name] = append(sc.values[n.Names[i].Name], v)
					}
				}
			}
			return true
		})
		r.scopes = append(r.scopes, sc)
	}
	for _, f := range files {
		for _, d := range f.Decls {
			if g, ok := d.(*ast.GenDecl); ok {
				for _, spec := range g.Specs {
					if v, ok := spec.(*ast.ValueSpec); ok {
						for i, val := range v.Values {
							if i < len(v.Names) {
								r.values[v.Names[i].Name] = append(r.values[v.Names[i].Name], val)
							}
						}
					}
				}
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CallExpr:
				switch name := callName(n); {
				case name == "Label" && len(n.Args) == 1:
					r.labels = append(r.labels, builtLabel{n.Args[0], ""})
				case name == "ExceptionBelow" && len(n.Args) == 3:
					r.labels = append(r.labels, builtLabel{n.Args[0], " under " + widest["Whole"]})
				case name == "ExceptionAtMost" && len(n.Args) == 3:
					r.labels = append(r.labels, builtLabel{n.Args[0], " of " + widest["Whole"] + " or less"})
				}
			case *ast.FuncDecl:
				addFunc(n.Name.Name, n.Type, n.Body)
			case *ast.AssignStmt:
				for i, rhs := range n.Rhs {
					if i >= len(n.Lhs) {
						break
					}
					if lit, ok := rhs.(*ast.FuncLit); ok {
						addFunc(lastName(n.Lhs[i]), lit.Type, lit.Body)
						continue
					}
					if sel, ok := n.Lhs[i].(*ast.SelectorExpr); ok && n.Tok != token.ADD_ASSIGN {
						r.fields[sel.Sel.Name] = append(r.fields[sel.Sel.Name], rhs)
					}
				}
			case *ast.KeyValueExpr:
				if id, ok := n.Key.(*ast.Ident); ok {
					r.fields[id.Name] = append(r.fields[id.Name], n.Value)
				}
			}
			return true
		})
	}
	// The library's own labels, such as "110% of last year's tax", are
	// shorter than this floor.
	r.label = strings.Repeat("x", 30)
	for _, l := range r.labels {
		s, ok := r.longest(l.expr, 0)
		if !ok {
			s = strings.Repeat("x", MaxSentence)
		}
		s += l.suffix
		if utf8.RuneCountInString(s) > utf8.RuneCountInString(r.label) {
			r.label = s
		}
	}
	return r
}

// lookup is every value assigned to name, read from the innermost function
// body around pos that assigns it, or else from the package's declarations.
func (r *resolver) lookup(name string, pos token.Pos) (values, additions []ast.Expr) {
	var best *scope
	for i := range r.scopes {
		sc := &r.scopes[i]
		if pos < sc.from || pos >= sc.to || len(sc.values[name]) == 0 {
			continue
		}
		if best == nil || sc.to-sc.from < best.to-best.from {
			best = sc
		}
	}
	if best != nil {
		return best.values[name], best.appends[name]
	}
	return r.values[name], nil
}

// widest are the library calls whose output the resolver knows, each as
// its widest value.
var widest = map[string]string{
	"Whole":   "$123,456",
	"Long":    "September 30, 2027",
	"Leaves":  "leaves a $123,456 refund",
	"Left":    "left a $123,456 refund",
	"Month":   "September",
	"month":   "September",
	"Weekday": "Wednesday",
	"Itoa":    "12",
	"Sprint":  "12",
}

// timeLayout widens a time.Format layout to its widest date.
var timeLayout = strings.NewReplacer("January", "September", "Monday", "Wednesday",
	"Jan", "Sep", "Mon", "Wed", "2006", "2027", "02", "30", "2", "30", "01", "12")

// longest is the longest string e can be, and whether the source bounds it.
// An expression that refers back to itself adds nothing on the second pass.
func (r *resolver) longest(e ast.Expr, depth int) (string, bool) {
	if depth > 12 {
		return "", false
	}
	if b, ok := bounds[r.pkg+":"+types.ExprString(e)]; ok {
		return b, true
	}
	if r.busy[e] {
		return "", true
	}
	r.busy[e] = true
	defer delete(r.busy, e)
	switch e := e.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return e.Value, true
		}
		s, err := strconv.Unquote(e.Value)
		return s, err == nil
	case *ast.ParenExpr:
		return r.longest(e.X, depth+1)
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return "", false
		}
		a, ok1 := r.longest(e.X, depth+1)
		b, ok2 := r.longest(e.Y, depth+1)
		return a + b, ok1 && ok2
	case *ast.SliceExpr:
		if hi, ok := e.High.(*ast.BasicLit); ok && e.Low == nil {
			if n, err := strconv.Atoi(hi.Value); err == nil {
				return strings.Repeat("X", n), true
			}
		}
		s, ok := r.longest(e.X, depth+1)
		if lo, isLit := e.Low.(*ast.BasicLit); isLit && e.High == nil {
			if n, err := strconv.Atoi(lo.Value); err == nil && n <= len(s) {
				s = s[n:]
			}
		}
		return s, ok
	case *ast.IndexExpr:
		return r.longestElement(e.X, depth+1)
	case *ast.CallExpr:
		name := callName(e)
		switch {
		case name == "Sprintf" && len(e.Args) > 0:
			format, ok := r.longest(e.Args[0], depth+1)
			if !ok {
				return "", false
			}
			s, unbounded := r.fillAt(format, e.Args[1:], depth+1)
			return s, len(unbounded) == 0
		case name == "String":
			if sel, ok := e.Fun.(*ast.SelectorExpr); ok {
				inner := lastName(sel.X)
				if c, ok := sel.X.(*ast.CallExpr); ok {
					inner = callName(c)
				}
				if widest[inner] != "" {
					return widest[inner], true
				}
			}
		case name == "Whole":
			if sel, ok := e.Fun.(*ast.SelectorExpr); ok {
				if cents, ok := r.amount(sel.X, depth+1); ok {
					return wholeDollars(cents), true
				}
			}
			return widest[name], true
		case widest[name] != "":
			return widest[name], true
		case name == "Label":
			return r.label, true
		case name == "Format" && len(e.Args) == 1:
			if layout, ok := r.longest(e.Args[0], depth+1); ok {
				return timeLayout.Replace(layout), true
			}
		case name == "TrimSuffix" || name == "TrimSpace" || name == "ToLower":
			if len(e.Args) > 0 {
				return r.longest(e.Args[0], depth+1)
			}
		case name == "ToUpper" || name == "capital":
			if len(e.Args) > 0 {
				s, ok := r.longest(e.Args[0], depth+1)
				if s != "" {
					s = strings.ToUpper(s[:1]) + s[1:]
				}
				return s, ok
			}
		}
		if rets := r.returns[name]; len(rets) > 0 {
			return r.longestOf(rets, nil, depth)
		}
		// A local bound to a function, such as words := estimated.Leaves.
		if id, ok := e.Fun.(*ast.Ident); ok {
			fns, _ := r.lookup(id.Name, id.Pos())
			best, any := "", false
			for _, fn := range fns {
				n := lastName(fn)
				s := widest[n]
				if s == "" {
					if rets := r.returns[n]; len(rets) > 0 {
						var ok bool
						if s, ok = r.longestOf(rets, nil, depth); !ok {
							return "", false
						}
					} else {
						return "", false
					}
				}
				any = true
				if utf8.RuneCountInString(s) > utf8.RuneCountInString(best) {
					best = s
				}
			}
			return best, any
		}
		return "", false
	case *ast.Ident:
		if rate, ok := r.rate(e, depth+1); ok {
			return rate, true
		}
		values, additions := r.lookup(e.Name, e.Pos())
		return r.longestOf(values, additions, depth)
	case *ast.SelectorExpr:
		if s := widest[e.Sel.Name]; s != "" {
			return s, true
		}
		if rate, ok := r.rate(e, depth+1); ok {
			return rate, true
		}
		return r.longestOf(r.fields[e.Sel.Name], nil, depth)
	}
	return "", false
}

// rate is the widest a rate can print, as "110%", when the package sets it
// only with money.Percent.
func (r *resolver) rate(e ast.Expr, depth int) (string, bool) {
	if depth > 12 {
		return "", false
	}
	switch e := e.(type) {
	case *ast.CallExpr:
		if callName(e) == "Percent" && len(e.Args) == 1 {
			if lit, ok := e.Args[0].(*ast.BasicLit); ok && lit.Kind == token.INT {
				return lit.Value + "%", true
			}
		}
	case *ast.Ident, *ast.SelectorExpr:
		var values []ast.Expr
		if id, ok := e.(*ast.Ident); ok {
			values, _ = r.lookup(id.Name, id.Pos())
		} else {
			values = r.fields[lastName(e)]
		}
		if len(values) == 0 {
			return "", false
		}
		best := ""
		for _, v := range values {
			s, ok := r.rate(v, depth+1)
			if !ok {
				return "", false
			}
			if len(s) > len(best) {
				best = s
			}
		}
		return best, true
	}
	return "", false
}

// amount is the largest a money expression can be, in cents, when the
// package sets it only from constants such as 1_000 * money.Dollar.
func (r *resolver) amount(e ast.Expr, depth int) (int64, bool) {
	if depth > 12 {
		return 0, false
	}
	switch e := e.(type) {
	case *ast.BasicLit:
		if e.Kind != token.INT {
			return 0, false
		}
		n, err := strconv.ParseInt(strings.ReplaceAll(e.Value, "_", ""), 0, 64)
		return n, err == nil
	case *ast.ParenExpr:
		return r.amount(e.X, depth+1)
	case *ast.BinaryExpr:
		a, ok1 := r.amount(e.X, depth+1)
		b, ok2 := r.amount(e.Y, depth+1)
		if !ok1 || !ok2 {
			return 0, false
		}
		switch e.Op {
		case token.MUL:
			return a * b, true
		case token.ADD:
			return a + b, true
		}
		return 0, false
	case *ast.CallExpr:
		// A conversion, as money.USCents(100_000).
		if len(e.Args) == 1 && strings.HasSuffix(types.ExprString(e.Fun), "USCents") {
			return r.amount(e.Args[0], depth+1)
		}
		rets := r.returns[callName(e)]
		if len(rets) == 0 {
			return 0, false
		}
		var best int64
		for _, v := range rets {
			n, ok := r.amount(v, depth+1)
			if !ok {
				return 0, false
			}
			best = max(best, n)
		}
		return best, true
	case *ast.Ident, *ast.SelectorExpr:
		name := lastName(e)
		if name == "Dollar" {
			return 100, true
		}
		var values []ast.Expr
		if id, ok := e.(*ast.Ident); ok {
			values, _ = r.lookup(id.Name, id.Pos())
		} else {
			values = r.fields[name]
		}
		if len(values) == 0 {
			return 0, false
		}
		var best int64
		for _, v := range values {
			n, ok := r.amount(v, depth+1)
			if !ok {
				return 0, false
			}
			best = max(best, n)
		}
		return best, true
	}
	return 0, false
}

// wholeDollars is cents as money.USCents.Whole prints them: "$1,000".
func wholeDollars(cents int64) string {
	d := strconv.FormatInt((cents+50)/100, 10)
	for i := len(d) - 3; i > 0; i -= 3 {
		d = d[:i] + "," + d[i:]
	}
	return "$" + d
}

// longestElement is the longest element of the composite literals x can be.
func (r *resolver) longestElement(x ast.Expr, depth int) (string, bool) {
	var lits []ast.Expr
	switch x := x.(type) {
	case *ast.CompositeLit:
		lits = []ast.Expr{x}
	case *ast.Ident:
		lits, _ = r.lookup(x.Name, x.Pos())
	case *ast.SelectorExpr:
		lits = r.fields[x.Sel.Name]
	}
	best, any := "", false
	for _, l := range lits {
		c, ok := l.(*ast.CompositeLit)
		if !ok {
			return "", false
		}
		for _, el := range c.Elts {
			if kv, ok := el.(*ast.KeyValueExpr); ok {
				el = kv.Value
			}
			s, ok := r.longest(el, depth+1)
			if !ok {
				return "", false
			}
			any = true
			if utf8.RuneCountInString(s) > utf8.RuneCountInString(best) {
				best = s
			}
		}
	}
	return best, any
}

// longestOf is the longest of values, with the longest of each addition
// appended, and whether every one is bounded.
func (r *resolver) longestOf(values, additions []ast.Expr, depth int) (string, bool) {
	if len(values) == 0 {
		return "", false
	}
	best := ""
	for _, v := range values {
		s, ok := r.longest(v, depth+1)
		if !ok {
			return "", false
		}
		if utf8.RuneCountInString(s) > utf8.RuneCountInString(best) {
			best = s
		}
	}
	for _, a := range additions {
		s, ok := r.longest(a, depth+1)
		if !ok {
			return "", false
		}
		best += s
	}
	return best, true
}

var verb = regexp.MustCompile(`%[-+# 0]*[0-9]*(\.[0-9]+)?[a-zA-Z%]`)

var (
	yearArg  = regexp.MustCompile(`(?i)year|last|next`)
	shareArg = regexp.MustCompile(`(?i)share|percent|pct|rate`)
	dateArg  = regexp.MustCompile(`(?i)date|\bdue\b|through|\bday\b|\bon\b|\bfrom\b`)
)

// fill fills each verb of a format string, as [CheckSource] describes, and
// returns the %s arguments it could not bound.
func (r *resolver) fill(s string, args []ast.Expr) (string, []string) {
	return r.fillAt(s, args, 0)
}

func (r *resolver) fillAt(s string, args []ast.Expr, depth int) (string, []string) {
	var unbounded []string
	next := 0
	filled := verb.ReplaceAllStringFunc(s, func(v string) string {
		if v == "%%" {
			return "%"
		}
		var arg ast.Expr
		if next < len(args) {
			arg = args[next]
		}
		next++
		expr := ""
		if arg != nil {
			expr = types.ExprString(arg)
		}
		switch v[len(v)-1] {
		case 's':
			if arg == nil {
				return ""
			}
			if s, ok := r.longest(arg, depth+1); ok {
				return s
			}
			unbounded = append(unbounded, expr)
			return widest["Whole"]
		case 'd':
			if yearArg.MatchString(expr) {
				return "2026"
			}
			return "12"
		}
		if arg != nil {
			if rate, ok := r.rate(arg, depth+1); ok {
				return rate
			}
			if c, ok := arg.(*ast.CallExpr); ok && widest[callName(c)] != "" {
				return widest[callName(c)]
			}
		}
		switch {
		case shareArg.MatchString(expr):
			return "110%"
		case dateArg.MatchString(expr):
			return widest["Long"]
		default:
			return widest["Whole"]
		}
	})
	return filled, unbounded
}
