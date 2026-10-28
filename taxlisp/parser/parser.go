// Package parser parses and formats taxlisp S-expression source files (.tlisp)
// into and from the canonical taxlisp.v1.ModuleFile protobuf AST.
package parser

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"

	taxlispv1 "github.com/nexteggapp/taxcalcs/taxlisp/v1"
)

// ParseFile parses a taxlisp source string into a ModuleFile protobuf AST.
func ParseFile(path, src string) (*taxlispv1.ModuleFile, error) {
	p := &parser{
		path: path,
		src:  src,
		line: 1,
		col:  1,
	}
	exprs, err := p.parseTopLevel()
	if err != nil {
		return nil, err
	}
	return &taxlispv1.ModuleFile{
		Path:        path,
		Expressions: exprs,
	}, nil
}

// ParseExpr parses a single S-expression from src.
func ParseExpr(src string) (*taxlispv1.Expression, error) {
	mf, err := ParseFile("<expr>", src)
	if err != nil {
		return nil, err
	}
	if len(mf.GetExpressions()) != 1 {
		return nil, fmt.Errorf("expected 1 expression, got %d", len(mf.GetExpressions()))
	}
	return mf.GetExpressions()[0], nil
}

// FormatFile formats a ModuleFile into canonical taxlisp S-expression text.
func FormatFile(mf *taxlispv1.ModuleFile) string {
	var b strings.Builder
	for i, expr := range mf.GetExpressions() {
		if i > 0 {
			b.WriteString("\n\n")
		}
		writeLeadingComments(&b, exprSyntax(expr), 0)
		formatExpr(&b, expr, 0)
	}
	if len(mf.GetExpressions()) > 0 {
		b.WriteByte('\n')
	}
	return b.String()
}

// FormatExpr formats a single Expression into canonical S-expression text.
func FormatExpr(expr *taxlispv1.Expression) string {
	var b strings.Builder
	formatExpr(&b, expr, 0)
	return b.String()
}

type parser struct {
	path string
	src  string
	pos  int
	line int
	col  int
}

func (p *parser) parseTopLevel() ([]*taxlispv1.Expression, error) {
	var out []*taxlispv1.Expression
	for {
		comments := p.skipWhitespaceAndComments()
		if p.eof() {
			break
		}
		expr, err := p.parseExpr(comments)
		if err != nil {
			return nil, err
		}
		out = append(out, expr)
	}
	return out, nil
}

func (p *parser) parseExpr(comments []string) (*taxlispv1.Expression, error) {
	if comments == nil {
		comments = p.skipWhitespaceAndComments()
	}
	if p.eof() {
		return nil, p.errorf("unexpected EOF")
	}
	startLine, startCol := p.line, p.col
	ch := p.peek()

	switch ch {
	case '(':
		return p.parseList(')', startLine, startCol, comments)
	case '[':
		return p.parseBracketFn(startLine, startCol, comments)
	case '\'':
		p.advance()
		inner, err := p.parseExpr(nil)
		if err != nil {
			return nil, err
		}
		syn := p.makeSyntax(startLine, startCol, p.line, p.col, comments)
		return &taxlispv1.Expression{
			Value: &taxlispv1.Expression_List{
				List: &taxlispv1.List{
					Expressions: []*taxlispv1.Expression{
						{
							Value: &taxlispv1.Expression_Atom{
								Atom: &taxlispv1.Atom{
									Primitive: &taxlispv1.Atom_Symbol{
										Symbol: &taxlispv1.Symbol{Name: "quote"},
									},
									Syntax: syn,
								},
							},
						},
						inner,
					},
					Syntax: syn,
				},
			},
		}, nil
	case '"':
		s, err := p.parseString()
		if err != nil {
			return nil, err
		}
		syn := p.makeSyntax(startLine, startCol, p.line, p.col, comments)
		return &taxlispv1.Expression{
			Value: &taxlispv1.Expression_Atom{
				Atom: &taxlispv1.Atom{
					Primitive: &taxlispv1.Atom_StringValue{StringValue: s},
					Syntax:    syn,
				},
			},
		}, nil
	case '#':
		return p.parseTaggedLiteral(startLine, startCol, comments)
	case ')', ']':
		return nil, p.errorf("unexpected closing delimiter %q", ch)
	default:
		return p.parseAtom(startLine, startCol, comments)
	}
}

func (p *parser) parseList(closeDelim rune, startLine, startCol int, comments []string) (*taxlispv1.Expression, error) {
	p.advance() // consume '('
	var elems []*taxlispv1.Expression
	for {
		innerComments := p.skipWhitespaceAndComments()
		if p.eof() {
			return nil, fmt.Errorf("%s:%d:%d: unclosed list '(' (expected %q)", p.path, startLine, startCol, closeDelim)
		}
		if p.peek() == closeDelim {
			p.advance()
			break
		}
		e, err := p.parseExpr(innerComments)
		if err != nil {
			return nil, err
		}
		elems = append(elems, e)
	}
	syn := p.makeSyntax(startLine, startCol, p.line, p.col, comments)
	return &taxlispv1.Expression{
		Value: &taxlispv1.Expression_List{
			List: &taxlispv1.List{
				Expressions: elems,
				Syntax:      syn,
			},
		},
	}, nil
}

// parseBracketFn parses `[...]` as `(bracket ...)` so it can represent a slice
// literal or typed bracket form cleanly without ambiguity.
func (p *parser) parseBracketFn(startLine, startCol int, comments []string) (*taxlispv1.Expression, error) {
	p.advance() // consume '['
	var elems []*taxlispv1.Expression
	for {
		innerComments := p.skipWhitespaceAndComments()
		if p.eof() {
			return nil, fmt.Errorf("%s:%d:%d: unclosed '['", p.path, startLine, startCol)
		}
		if p.peek() == ']' {
			p.advance()
			break
		}
		e, err := p.parseExpr(innerComments)
		if err != nil {
			return nil, err
		}
		elems = append(elems, e)
	}
	syn := p.makeSyntax(startLine, startCol, p.line, p.col, comments)
	head := &taxlispv1.Expression{
		Value: &taxlispv1.Expression_Atom{
			Atom: &taxlispv1.Atom{
				Primitive: &taxlispv1.Atom_Symbol{
					Symbol: &taxlispv1.Symbol{Name: "slice"},
				},
				Syntax: syn,
			},
		},
	}
	all := append([]*taxlispv1.Expression{head}, elems...)
	return &taxlispv1.Expression{
		Value: &taxlispv1.Expression_List{
			List: &taxlispv1.List{
				Expressions: all,
				Syntax:      syn,
			},
		},
	}, nil
}

func (p *parser) parseTaggedLiteral(startLine, startCol int, comments []string) (*taxlispv1.Expression, error) {
	p.advance() // consume '#'
	tagStart := p.pos
	for !p.eof() && (unicode.IsLetter(p.peek()) || unicode.IsDigit(p.peek())) {
		p.advance()
	}
	tag := p.src[tagStart:p.pos]
	if p.eof() || p.peek() != '"' {
		return nil, p.errorf("expected quoted string after #%s", tag)
	}
	raw, err := p.parseString()
	if err != nil {
		return nil, err
	}
	syn := p.makeSyntax(startLine, startCol, p.line, p.col, comments)
	switch tag {
	case "usd":
		m, err := parseUSD(raw)
		if err != nil {
			return nil, fmt.Errorf("%s:%d:%d: invalid #usd literal %q: %w", p.path, startLine, startCol, raw, err)
		}
		return &taxlispv1.Expression{
			Value: &taxlispv1.Expression_Atom{
				Atom: &taxlispv1.Atom{
					Primitive: &taxlispv1.Atom_Money{Money: m},
					Syntax:    syn,
				},
			},
		}, nil
	case "qty":
		q, err := parseQuantity(raw)
		if err != nil {
			return nil, fmt.Errorf("%s:%d:%d: invalid #qty literal %q: %w", p.path, startLine, startCol, raw, err)
		}
		return &taxlispv1.Expression{
			Value: &taxlispv1.Expression_Atom{
				Atom: &taxlispv1.Atom{
					Primitive: &taxlispv1.Atom_Quantity{Quantity: q},
					Syntax:    syn,
				},
			},
		}, nil
	case "date":
		d, err := parseDate(raw)
		if err != nil {
			return nil, fmt.Errorf("%s:%d:%d: invalid #date literal %q: %w", p.path, startLine, startCol, raw, err)
		}
		return &taxlispv1.Expression{
			Value: &taxlispv1.Expression_Atom{
				Atom: &taxlispv1.Atom{
					Primitive: &taxlispv1.Atom_Date{Date: d},
					Syntax:    syn,
				},
			},
		}, nil
	default:
		return nil, fmt.Errorf("%s:%d:%d: unknown reader tag #%s", p.path, startLine, startCol, tag)
	}
}

func (p *parser) parseAtom(startLine, startCol int, comments []string) (*taxlispv1.Expression, error) {
	tokStart := p.pos
	for !p.eof() && !isDelimiter(p.peek()) {
		p.advance()
	}
	tok := p.src[tokStart:p.pos]
	if tok == "" {
		return nil, p.errorf("unexpected character %q", p.peek())
	}
	syn := p.makeSyntax(startLine, startCol, p.line, p.col, comments)

	if strings.HasPrefix(tok, ":") && len(tok) > 1 {
		return &taxlispv1.Expression{
			Value: &taxlispv1.Expression_Atom{
				Atom: &taxlispv1.Atom{
					Primitive: &taxlispv1.Atom_Keyword{Keyword: tok[1:]},
					Syntax:    syn,
				},
			},
		}, nil
	}
	if tok == "true" || tok == "false" {
		return &taxlispv1.Expression{
			Value: &taxlispv1.Expression_Atom{
				Atom: &taxlispv1.Atom{
					Primitive: &taxlispv1.Atom_BoolValue{BoolValue: tok == "true"},
					Syntax:    syn,
				},
			},
		}, nil
	}
	if n, err := strconv.ParseInt(tok, 10, 64); err == nil {
		return &taxlispv1.Expression{
			Value: &taxlispv1.Expression_Atom{
				Atom: &taxlispv1.Atom{
					Primitive: &taxlispv1.Atom_IntValue{IntValue: n},
					Syntax:    syn,
				},
			},
		}, nil
	}

	sym := parseSymbolToken(tok)
	return &taxlispv1.Expression{
		Value: &taxlispv1.Expression_Atom{
			Atom: &taxlispv1.Atom{
				Primitive: &taxlispv1.Atom_Symbol{Symbol: sym},
				Syntax:    syn,
			},
		},
	}, nil
}

func parseSymbolToken(tok string) *taxlispv1.Symbol {
	// Division operator "/" is an unqualified symbol.
	if tok == "/" {
		return &taxlispv1.Symbol{Name: "/"}
	}
	if idx := strings.IndexByte(tok, '/'); idx > 0 && idx < len(tok)-1 {
		return &taxlispv1.Symbol{
			Package: tok[:idx],
			Name:    tok[idx+1:],
		}
	}
	return &taxlispv1.Symbol{Name: tok}
}

func (p *parser) parseString() (string, error) {
	startLine, startCol := p.line, p.col
	p.advance() // consume opening '"'
	var b strings.Builder
	for !p.eof() {
		ch := p.peek()
		if ch == '"' {
			p.advance()
			return b.String(), nil
		}
		if ch == '\\' {
			p.advance()
			if p.eof() {
				return "", fmt.Errorf("%s:%d:%d: unclosed string literal", p.path, startLine, startCol)
			}
			esc := p.peek()
			p.advance()
			switch esc {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case '"':
				b.WriteByte('"')
			case '\\':
				b.WriteByte('\\')
			default:
				return "", p.errorf("invalid string escape \\%c", esc)
			}
			continue
		}
		b.WriteRune(ch)
		p.advance()
	}
	return "", fmt.Errorf("%s:%d:%d: unclosed string literal", p.path, startLine, startCol)
}

func (p *parser) skipWhitespaceAndComments() []string {
	var comments []string
	for !p.eof() {
		ch := p.peek()
		if unicode.IsSpace(ch) {
			p.advance()
			continue
		}
		if ch == ';' {
			p.advance() // consume ';'
			start := p.pos
			for !p.eof() && p.peek() != '\n' {
				p.advance()
			}
			lineText := strings.TrimRight(p.src[start:p.pos], "\r")
			comments = append(comments, lineText)
			continue
		}
		break
	}
	return comments
}

func (p *parser) makeSyntax(startLine, startCol, endLine, endCol int, comments []string) *taxlispv1.SyntaxDetails {
	return &taxlispv1.SyntaxDetails{
		File:            p.path,
		StartLine:       int32(startLine),
		StartColumn:     int32(startCol),
		EndLine:         int32(endLine),
		EndColumn:       int32(endCol),
		LeadingComments: comments,
	}
}

func (p *parser) eof() bool {
	return p.pos >= len(p.src)
}

func (p *parser) peek() rune {
	r, _ := utf8.DecodeRuneInString(p.src[p.pos:])
	return r
}

func (p *parser) advance() {
	if p.eof() {
		return
	}
	r, w := utf8.DecodeRuneInString(p.src[p.pos:])
	p.pos += w
	if r == '\n' {
		p.line++
		p.col = 1
	} else {
		p.col++
	}
}

func (p *parser) errorf(format string, args ...any) error {
	return fmt.Errorf("%s:%d:%d: %s", p.path, p.line, p.col, fmt.Sprintf(format, args...))
}

func isDelimiter(r rune) bool {
	return unicode.IsSpace(r) || r == '(' || r == ')' || r == '[' || r == ']' || r == '"' || r == ';' || r == '\''
}

func parseUSD(s string) (*commonv1.Money, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("empty money string")
	}
	neg := false
	switch s[0] {
	case '-':
		neg = true
		s = s[1:]
	case '+':
		s = s[1:]
	}
	parts := strings.Split(s, ".")
	if len(parts) > 2 {
		return nil, fmt.Errorf("too many decimal points")
	}
	dollars, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return nil, err
	}
	var cents int64
	if len(parts) == 2 {
		frac := parts[1]
		switch len(frac) {
		case 0:
			cents = 0
		case 1:
			c, err := strconv.ParseInt(frac, 10, 64)
			if err != nil {
				return nil, err
			}
			cents = c * 10
		case 2:
			c, err := strconv.ParseInt(frac, 10, 64)
			if err != nil {
				return nil, err
			}
			cents = c
		default:
			return nil, fmt.Errorf("USD literal %q has more than 2 decimal places", s)
		}
	}
	minor := dollars*100 + cents
	if neg {
		minor = -minor
	}
	return &commonv1.Money{
		Currency: "USD",
		Minor:    minor,
	}, nil
}

func parseQuantity(s string) (*commonv1.Quantity, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("empty quantity string")
	}
	neg := false
	switch s[0] {
	case '-':
		neg = true
		s = s[1:]
	case '+':
		s = s[1:]
	}
	parts := strings.Split(s, ".")
	if len(parts) > 2 {
		return nil, fmt.Errorf("too many decimal points")
	}
	var scale int32
	combined := parts[0]
	if len(parts) == 2 {
		scale = int32(len(parts[1]))
		combined += parts[1]
	}
	units, err := strconv.ParseInt(combined, 10, 64)
	if err != nil {
		return nil, err
	}
	if neg {
		units = -units
	}
	return &commonv1.Quantity{
		Units: units,
		Scale: scale,
	}, nil
}

func parseDate(s string) (*commonv1.Date, error) {
	parts := strings.Split(strings.TrimSpace(s), "-")
	if len(parts) != 3 {
		return nil, fmt.Errorf("expected YYYY-MM-DD")
	}
	y, err := strconv.ParseInt(parts[0], 10, 32)
	if err != nil || y < 1 || y > 9999 {
		return nil, fmt.Errorf("invalid year %q", parts[0])
	}
	m, err := strconv.ParseInt(parts[1], 10, 32)
	if err != nil || m < 1 || m > 12 {
		return nil, fmt.Errorf("invalid month %q", parts[1])
	}
	d, err := strconv.ParseInt(parts[2], 10, 32)
	if err != nil || d < 1 || d > 31 {
		return nil, fmt.Errorf("invalid day %q", parts[2])
	}
	return &commonv1.Date{
		Year:  int32(y),
		Month: int32(m),
		Day:   int32(d),
	}, nil
}

func exprSyntax(e *taxlispv1.Expression) *taxlispv1.SyntaxDetails {
	switch v := e.GetValue().(type) {
	case *taxlispv1.Expression_Atom:
		return v.Atom.GetSyntax()
	case *taxlispv1.Expression_List:
		return v.List.GetSyntax()
	default:
		return nil
	}
}

func writeLeadingComments(b *strings.Builder, syn *taxlispv1.SyntaxDetails, indent int) {
	if syn == nil {
		return
	}
	prefix := strings.Repeat(" ", indent)
	for _, c := range syn.GetLeadingComments() {
		b.WriteString(prefix)
		b.WriteByte(';')
		b.WriteString(c)
		b.WriteByte('\n')
	}
}

func formatExpr(b *strings.Builder, e *taxlispv1.Expression, indent int) {
	switch v := e.GetValue().(type) {
	case *taxlispv1.Expression_Atom:
		formatAtom(b, v.Atom)
	case *taxlispv1.Expression_List:
		elems := v.List.GetExpressions()
		if len(elems) == 2 && isSymbolNamed(elems[0], "", "quote") {
			b.WriteByte('\'')
			formatExpr(b, elems[1], indent+1)
			return
		}
		b.WriteByte('(')
		for i, child := range elems {
			if i > 0 {
				b.WriteByte(' ')
			}
			formatExpr(b, child, indent+1)
		}
		b.WriteByte(')')
	}
}

func formatAtom(b *strings.Builder, a *taxlispv1.Atom) {
	switch p := a.GetPrimitive().(type) {
	case *taxlispv1.Atom_Symbol:
		if p.Symbol.GetPackage() != "" {
			b.WriteString(p.Symbol.GetPackage())
			b.WriteByte('/')
		}
		b.WriteString(p.Symbol.GetName())
	case *taxlispv1.Atom_Keyword:
		b.WriteByte(':')
		b.WriteString(p.Keyword)
	case *taxlispv1.Atom_StringValue:
		b.WriteString(strconv.Quote(p.StringValue))
	case *taxlispv1.Atom_IntValue:
		b.WriteString(strconv.FormatInt(p.IntValue, 10))
	case *taxlispv1.Atom_BoolValue:
		if p.BoolValue {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case *taxlispv1.Atom_Money:
		b.WriteString(`#usd"`)
		b.WriteString(FormatUSD(p.Money))
		b.WriteByte('"')
	case *taxlispv1.Atom_Quantity:
		b.WriteString(`#qty"`)
		b.WriteString(FormatQuantity(p.Quantity))
		b.WriteByte('"')
	case *taxlispv1.Atom_Date:
		b.WriteString(fmt.Sprintf(`#date"%04d-%02d-%02d"`, p.Date.GetYear(), p.Date.GetMonth(), p.Date.GetDay()))
	}
}

// FormatUSD formats a Money message as an exact decimal string (e.g. "15750.00" or "-45.50").
func FormatUSD(m *commonv1.Money) string {
	minor := m.GetMinor()
	neg := minor < 0
	if neg {
		minor = -minor
	}
	dollars := minor / 100
	cents := minor % 100
	sign := ""
	if neg {
		sign = "-"
	}
	if cents == 0 {
		return fmt.Sprintf("%s%d.00", sign, dollars)
	}
	return fmt.Sprintf("%s%d.%02d", sign, dollars, cents)
}

// FormatQuantity formats a Quantity message as an exact decimal string.
func FormatQuantity(q *commonv1.Quantity) string {
	units := q.GetUnits()
	scale := int(q.GetScale())
	neg := units < 0
	if neg {
		units = -units
	}
	digits := strconv.FormatInt(units, 10)
	sign := ""
	if neg {
		sign = "-"
	}
	if scale <= 0 {
		return sign + digits + strings.Repeat("0", -scale)
	}
	if len(digits) <= scale {
		digits = strings.Repeat("0", scale-len(digits)+1) + digits
	}
	split := len(digits) - scale
	return sign + digits[:split] + "." + digits[split:]
}

func isSymbolNamed(e *taxlispv1.Expression, pkg, name string) bool {
	a := e.GetAtom()
	if a == nil {
		return false
	}
	sym := a.GetSymbol()
	if sym == nil {
		return false
	}
	return sym.GetPackage() == pkg && sym.GetName() == name
}
