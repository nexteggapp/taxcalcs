package parser_test

import (
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/nexteggapp/taxcalcs/taxlisp/parser"
	taxlispv1 "github.com/nexteggapp/taxcalcs/taxlisp/v1"
)

func TestParseAndRoundTrip(t *testing.T) {
	src := `; Top-level comment
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(use-proto 'nexteggapp.taxprotos.us.federal.v1 'fed)

(= std-deduction #usd"15750.00")

(def (sum-by T) ((f (Fn (T) Money)) (items (Slice T))) Money
  (fold (fn ((acc Money) (item T)) Money (+ acc (f item))) #usd"0.00" items))

(def sample ((in ret/ReturnInputs)) (Union Money Nil)
  (withs (rate #qty"0.0765"
          dt #date"2025-04-15"
          xs [1 2 3])
    (if (> (len xs) 0)
        (round-dollar (* std-deduction rate))
        nil)))
`
	mf1, err := parser.ParseFile("sample.tlisp", src)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}
	if len(mf1.GetExpressions()) != 5 {
		t.Fatalf("got %d top-level expressions, want 5", len(mf1.GetExpressions()))
	}
	firstComments := mf1.GetExpressions()[0].GetList().GetSyntax().GetLeadingComments()
	if len(firstComments) != 1 || firstComments[0] != " Top-level comment" {
		t.Errorf("leading comments = %v, want [\" Top-level comment\"]", firstComments)
	}

	formatted := parser.FormatFile(mf1)
	mf2, err := parser.ParseFile("sample.tlisp", formatted)
	if err != nil {
		t.Fatalf("re-parse of formatted output failed: %v\nformatted:\n%s", err, formatted)
	}
	formatted2 := parser.FormatFile(mf2)
	if formatted != formatted2 {
		t.Errorf("FormatFile is not idempotent:\nfirst:\n%s\nsecond:\n%s", formatted, formatted2)
	}

	// Clear SyntaxDetails and verify structural proto equality.
	stripSyntax(mf1)
	stripSyntax(mf2)
	if !proto.Equal(mf1, mf2) {
		t.Errorf("ASTs differ after round-trip:\nmf1=%v\nmf2=%v", mf1, mf2)
	}
}

func TestSymbolPackageSplitting(t *testing.T) {
	e, err := parser.ParseExpr("fed/Form1040")
	if err != nil {
		t.Fatal(err)
	}
	sym := e.GetAtom().GetSymbol()
	if sym.GetPackage() != "fed" || sym.GetName() != "Form1040" {
		t.Errorf("got package=%q name=%q, want fed / Form1040", sym.GetPackage(), sym.GetName())
	}

	div, err := parser.ParseExpr("(/ a b)")
	if err != nil {
		t.Fatal(err)
	}
	divSym := div.GetList().GetExpressions()[0].GetAtom().GetSymbol()
	if divSym.GetPackage() != "" || divSym.GetName() != "/" {
		t.Errorf("division symbol = %v, want unqualified '/'", divSym)
	}
}

func TestSyntaxErrors(t *testing.T) {
	cases := []string{
		"(unclosed",
		"[unclosed",
		")",
		`#usd"12.345"`,
		`#date"2025-13-01"`,
		`#unknown"abc"`,
		`"unclosed string`,
	}
	for _, tc := range cases {
		if _, err := parser.ParseFile("bad.tlisp", tc); err == nil {
			t.Errorf("ParseFile(%q) succeeded, expected error", tc)
		}
	}
}

func stripSyntax(mf *taxlispv1.ModuleFile) {
	for _, e := range mf.GetExpressions() {
		stripExprSyntax(e)
	}
}

func stripExprSyntax(e *taxlispv1.Expression) {
	if a := e.GetAtom(); a != nil {
		a.Syntax = nil
	}
	if l := e.GetList(); l != nil {
		l.Syntax = nil
		for _, child := range l.GetExpressions() {
			stripExprSyntax(child)
		}
	}
}
