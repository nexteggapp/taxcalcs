package eval_test

import (
	"fmt"
	"strings"

	"github.com/nexteggapp/taxcalcs/taxlisp/eval"
	"github.com/nexteggapp/taxcalcs/taxlisp/parser"
	"github.com/nexteggapp/taxcalcs/taxlisp/types"
)

// A trace answers "why?" about a result: here, why a filer who rents gets
// no renter's credit. The answer, $0.00, alone says nothing; the trace shows
// the condition that failed and the figures it compared.
func ExampleTrace() {
	src := `
; The renter's credit for a single filer: $60 for one who rented, whom
; no one else can claim, and whose AGI is within the limit.
(def renters-credit ((agi Money) (rented Bool) (claimable Bool)) Money
  (if (all-of rented
              (not claimable)
              (within-renters-limit? agi))
      #usd"60.00"
      #usd"0.00"))

(def within-renters-limit? ((agi Money)) Bool
  (<= agi #usd"52421.00"))
`
	mf, err := parser.ParseFile("credit.tlisp", src)
	if err != nil {
		panic(err)
	}
	if err := types.NewChecker(nil).CheckModule(mf); err != nil {
		panic(err)
	}
	ev := eval.NewEvaluator(nil, nil)
	if _, err := ev.EvalModule(mf); err != nil {
		panic(err)
	}

	// Record every named function. Without a trace, evaluation is unchanged.
	ev.Trace = eval.NewTrace(nil)
	credit, err := ev.Call("renters-credit", eval.NewMoneyCents(60_000_00), eval.BoolVal{V: true}, eval.BoolVal{V: false})
	if err != nil {
		panic(err)
	}
	fmt.Println("credit:", eval.Display(credit))
	printTrace(ev.Trace.Root, 0)

	// Output:
	// credit: 0.00
	// renters-credit(agi=60000.00 rented=true claimable=false) = 0.00
	//   ✓ rented
	//   ✓ (not claimable)
	//   ✗ (within-renters-limit? agi)
	//     within-renters-limit?(agi=60000.00) = false
}

// printTrace writes a derivation as an indented tree.
func printTrace(n *eval.Node, depth int) {
	if n.Kind != "root" {
		indent := strings.Repeat("  ", depth)
		switch n.Kind {
		case "call":
			var args []string
			for _, a := range n.Args {
				args = append(args, a.Name+"="+a.Value)
			}
			fmt.Printf("%s%s(%s) = %s\n", indent, n.Name, strings.Join(args, " "), n.Value)
		case "check":
			mark := "✓"
			if n.Value == "false" {
				mark = "✗"
			}
			fmt.Printf("%s%s %s\n", indent, mark, n.Caption)
		case "line":
			fmt.Printf("%sline %s  %s = %s\n", indent, n.Line, n.Caption, n.Value)
		}
		depth++
	}
	for _, c := range n.Children {
		printTrace(c, depth)
	}
}
