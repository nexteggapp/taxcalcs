package taxyear

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/nexteggapp/taxcalcs/taxyear/model"
)

// TestCompareLegacy computes every case with the old federal code and the
// rules engine and prints the headline figures of each. Temporary.
func TestCompareLegacy(t *testing.T) {
	all := cases(t)
	for _, dir := range []string{"testdata/fixtures"} {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			text, _ := os.ReadFile(dir + "/" + e.Name())
			f, err := Parse(text)
			if err == nil {
				all["fixtures/"+e.Name()] = f
			}
		}
	}
	var names []string
	for n := range all {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	fmt.Fprintln(&b, "| case | scenario | total tax old | new | AGI old | new | taxable old | new | refund(-)/owed old | new |")
	for _, n := range names {
		useLegacy = true
		old, _ := Compute(all[n])
		useLegacy = false
		cur, _ := Compute(all[n])
		for i, s := range old.Proto().GetScenarios() {
			c := cur.Proto().GetScenarios()[i]
			fo, fc := s.GetFederal().GetForm_1040(), c.GetFederal().GetForm_1040()
			ob := int64(model.Value(fo.GetAmountOwed()) - model.Value(fo.GetOverpaid()))
			cb := int64(model.Value(fc.GetAmountOwed()) - model.Value(fc.GetOverpaid()))
			mark := ""
			if model.Value(fo.GetTotalTax()) != model.Value(fc.GetTotalTax()) || model.Value(fo.GetAdjustedGrossIncome()) != model.Value(fc.GetAdjustedGrossIncome()) || model.Value(fo.GetTaxableIncome()) != model.Value(fc.GetTaxableIncome()) || ob != cb {
				mark = " *"
			}
			fmt.Fprintf(&b, "| %s | %s%s | %d | %d | %d | %d | %d | %d | %d | %d |\n", n, s.GetScenario(), mark,
				model.Value(fo.GetTotalTax())/100, model.Value(fc.GetTotalTax())/100,
				model.Value(fo.GetAdjustedGrossIncome())/100, model.Value(fc.GetAdjustedGrossIncome())/100,
				model.Value(fo.GetTaxableIncome())/100, model.Value(fc.GetTaxableIncome())/100, ob/100, cb/100)
			for _, p := range c.GetProblems() {
				fmt.Fprintf(&b, "  problem %s: %s\n", p.GetPath(), p.GetMessage().GetText())
			}
		}
	}
	t.Log("\n" + b.String())
}
