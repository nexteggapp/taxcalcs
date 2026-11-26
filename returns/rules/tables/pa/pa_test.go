// Package pa holds Pennsylvania's tax computation to the 2025 flat rate
// printed on Form PA-40 line 12 and in its instructions.
//
// The rules' figure is Form PA-40 line 12, from Prepare on a return of wages
// alone: Pennsylvania allows no standard deduction or exemption, so the wages
// are the target taxable income, and the test checks line 11 lands on the
// target before reading line 12. The expected figure comes only from the TSV
// file under sources/tables/pa/2025.
package pa

import (
	"path/filepath"
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	"github.com/nexteggapp/taxcalcs/returns/rules/tables/tablefile"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
)

const (
	root   = "../../../.."
	tables = root + "/sources/tables/pa/2025"
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
			State:  "PA",
			Inputs: &returnsv1.StateInputs_Pa{Pa: &returnsv1.PennsylvaniaInputs{}},
		}}
		out, err := e.Prepare(in)
		if err != nil {
			t.Fatalf("%s at %d: %v", status, income, err)
		}
		f := out.GetStates()[0].GetPa().GetForms().GetFormPa40()
		if got := f.GetAdjustedTaxableIncome().GetMinor(); got != income*100 {
			t.Fatalf("%s: line 11 is %d cents, want %d dollars", status, got, income)
		}
		return f.GetTaxLiability().GetMinor()
	}
}

// The tax rounded once to whole dollars, as the form enters it.
func flatTax(b tablefile.Bracket, income int64) int64 {
	return b.TaxDollars(income) * 100
}

// A flat rate has one edge, at zero. These incomes add the points where the
// rounding turns: 3.07% of $16 is $0.4912 and of $17 is $0.5219, of $48 is
// $1.4736 and of $49 is $1.5043; the rest spread the check over the
// incomes a return reaches.
var spread = []int64{16, 17, 48, 49, 1_000, 25_000, 65_146, 100_000, 250_000, 1_000_000, 10_000_000}

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
