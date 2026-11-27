// Package ut holds Utah's tax computation to the 2025 flat rate
// printed on Form TC-40 line 10 and in its instructions.
//
// The rules' figure is Form TC-40 line 10, from Prepare on a return of wages
// alone: Utah taxable income on line 9 is federal adjusted gross income with
// no Utah additions or subtractions, so the wages are the target taxable
// income, and the test checks line 9 lands on the target before reading line
// 10. The expected figure comes only from the TSV
// file under sources/tables/ut/2025.
package ut

import (
	"path/filepath"
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	"github.com/nexteggapp/taxcalcs/returns/rules/tables/tablefile"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
)

const (
	root   = "../../../.."
	tables = root + "/sources/tables/ut/2025"
	pdfs   = root + "/sources/pdfs"
)

func newFigure(t *testing.T) tablefile.Figure {
	e, err := federal.LoadEngine()
	if err != nil {
		t.Fatal(err)
	}
	return func(t *testing.T, status string, income int64) int64 {
		t.Helper()
		in := tablefile.WageInputs(status, income)
		in.States = []*returnsv1.StateInputs{{
			State:  "UT",
			Inputs: &returnsv1.StateInputs_Ut{Ut: &returnsv1.UtahInputs{}},
		}}
		out, err := e.Prepare(in)
		if err != nil {
			t.Fatalf("%s at %d: %v", status, income, err)
		}
		f := out.GetStates()[0].GetUt().GetForms().GetFormTc40()
		if got := f.GetUtahTaxableIncome().GetMinor(); got != income*100 {
			t.Fatalf("%s: line 9 is %d cents, want %d dollars", status, got, income)
		}
		return f.GetTaxBeforeCredit().GetMinor()
	}
}

// The tax rounded once to whole dollars, as the form enters it.
func flatTax(b tablefile.Bracket, income int64) int64 {
	return b.TaxDollars(income) * 100
}

// A flat rate has one edge, at zero. These incomes add the points where the
// rounding turns: 4.5% of $11 is $0.495 and of $12 is $0.54, of $33 is
// $1.485 and of $34 is $1.53; the rest spread the check over the
// incomes a return reaches.
var spread = []int64{11, 12, 33, 34, 1_000, 25_000, 65_146, 100_000, 250_000, 1_000_000, 10_000_000}

func TestTaxTables(t *testing.T) {
	c, err := tablefile.NewChecker(filepath.Join(tables, "known_gaps.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := tablefile.ReadSchedule(filepath.Join(tables, "flat_rate.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CheckPDF(pdfs); err != nil {
		t.Fatal(err)
	}
	tablefile.CheckSchedule(t, "flat_rate.tsv", s, 0, c, flatTax, newFigure)
	t.Run("spread", func(t *testing.T) {
		for _, status := range s.Statuses() {
			t.Run(status, func(t *testing.T) {
				t.Parallel()
				figure := newFigure(t)
				for _, income := range spread {
					b, _ := s.For(status, income)
					c.Check(t, "flat_rate.tsv", b.Over, income, status, flatTax(b, income), figure(t, status, income))
				}
			})
		}
	})
	c.Finish(t)
}
