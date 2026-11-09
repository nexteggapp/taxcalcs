package expand_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nexteggapp/taxcalcs/taxlisp/eval"
	"github.com/nexteggapp/taxcalcs/taxlisp/expand"
	"github.com/nexteggapp/taxcalcs/taxlisp/parser"
	"github.com/nexteggapp/taxcalcs/taxlisp/types"
	"google.golang.org/protobuf/reflect/protoregistry"
)

const src = `
(defstruct Figures
  (tax_year Int)
  (credit Money)
  (step Money))

(deffigures figures-2025 Figures 2025
  (from some-booklet-2025
    (credit #usd"153.00" :at line-7)
    (step #usd"2500.00" :at worksheet)))

; Over the limit?
(def over? ((agi Money) (limit Money)) Bool
  (> agi limit))

; A small worksheet.
(defworksheet credit-worksheet ((f Figures) (agi Money) (exemptions Int)) Money
  :cite ((law "Code" "1"))
  (line 1 "AGI" agi)
  (line 2 "Steps over the limit"
        (if (over? line1 #usd"10000.00") 1.0 0.0))
  (line 3 "Credit" (- (* (. f 'credit) (int-to-float exemptions)) (* #usd"6.00" line2)))
  (result line3))

(def qualifies? ((agi Money) (renter Bool)) Bool
  (all-of renter (not (over? agi #usd"50000.00"))))
`

func load(t *testing.T) *eval.Evaluator {
	t.Helper()
	mf, err := parser.ParseFile("test.tlisp", src)
	if err != nil {
		t.Fatal(err)
	}
	if err := expand.File(mf); err != nil {
		t.Fatal(err)
	}
	if err := types.NewChecker(protoregistry.GlobalFiles).CheckModule(mf); err != nil {
		t.Fatal(err)
	}
	ev := eval.NewEvaluator(nil, nil)
	if _, err := ev.EvalModule(mf); err != nil {
		t.Fatal(err)
	}
	return ev
}

func TestWorksheetTrace(t *testing.T) {
	ev := load(t)
	ev.Trace = eval.NewTrace(nil)
	figs, _ := ev.Globals.Get("figures-2025")
	v, err := ev.Call("credit-worksheet", figs, eval.NewMoneyCents(2000000), eval.IntVal{V: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got := eval.Display(v); got != "300.00" {
		t.Errorf("credit = %s, want 300.00", got)
	}
	call := ev.Trace.Root.Children[0]
	if call.Kind != "call" || call.Name != "credit-worksheet" || len(call.Children) != 3 {
		b, _ := json.MarshalIndent(ev.Trace.Root, "", " ")
		t.Fatalf("unexpected trace:\n%s", b)
	}
	line2 := call.Children[1]
	if line2.Kind != "line" || line2.Line != "2" || line2.Value != "1.0" || line2.Children[0].Name != "over?" {
		b, _ := json.MarshalIndent(line2, "", " ")
		t.Errorf("line 2:\n%s", b)
	}
}

func TestAllOfRecordsEachCheck(t *testing.T) {
	ev := load(t)
	ev.Trace = eval.NewTrace(nil)
	v, err := ev.Call("qualifies?", eval.NewMoneyCents(6000000), eval.BoolVal{V: true})
	if err != nil {
		t.Fatal(err)
	}
	if v != (eval.BoolVal{V: false}) {
		t.Errorf("qualifies? = %v, want false", v)
	}
	checks := ev.Trace.Root.Children[0].Children
	if len(checks) != 2 || checks[0].Value != "true" || checks[1].Value != "false" ||
		!strings.Contains(checks[1].Caption, "over?") {
		b, _ := json.MarshalIndent(checks, "", " ")
		t.Errorf("checks:\n%s", b)
	}
}
