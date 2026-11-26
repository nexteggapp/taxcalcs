// Package ga holds Georgia's tax computation to the 5.19% rate the 2025
// IT-511 booklet prints for Form 500 line 16. Georgia prints no tax table.
//
// The rules' figure is Form 500 line 16, from Prepare on a full-year
// resident's return of wages alone: the wages are the target taxable income
// plus the Georgia standard deduction, and the test checks line 15c lands on
// the target before reading line 16. The expected figure comes only from the
// TSV file under sources/tables/ga/2025.
package ga

import (
	"path/filepath"
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	"github.com/nexteggapp/taxcalcs/returns/rules/tables/tablefile"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
)

const (
	root   = "../../../.."
	tables = root + "/sources/tables/ga/2025"
	pdfs   = root + "/sources/pdfs"
)

// The 2025 Georgia standard deduction, from the instructions for line 11:
// $24,000 for status B (married filing jointly), $12,000 for A, C and D.
var standardDeduction = map[string]int64{
	"single": 12_000,
	"mfj":    24_000,
	"mfs":    12_000,
	"hoh":    12_000,
	"qss":    12_000,
}

func newFigure(t *testing.T) tablefile.Figure {
	e, err := federal.LoadEngine()
	if err != nil {
		t.Fatal(err)
	}
	return func(t *testing.T, status string, income int64) int64 {
		t.Helper()
		in := tablefile.WageInputs(status, income+standardDeduction[status])
		in.States = []*returnsv1.StateInputs{{
			State:  "GA",
			Inputs: &returnsv1.StateInputs_Ga{Ga: &returnsv1.GeorgiaInputs{}},
		}}
		out, err := e.Prepare(in)
		if err != nil {
			t.Fatalf("%s at %d: %v", status, income, err)
		}
		f := out.GetStates()[0].GetGa().GetForms().GetForm_500()
		if got := f.GetTaxableIncome().GetMinor(); got != income*100 {
			t.Fatalf("%s: taxable income is %d cents, want %d dollars", status, got, income)
		}
		return f.GetTax().GetMinor()
	}
}

// Line 16 is line 15c times 5.19%, rounded to the nearest dollar, as the
// line 16 instructions say (IT-511 page 17).
// The product is rounded once, not first to the cent.
func rateTax(b tablefile.Bracket, income int64) int64 {
	return b.TaxDollars(income) * 100
}

// A flat rate has one edge, at zero, so the test also probes incomes whose
// tax lands just below, on and just above 50 cents, and a few large ones.
// At $5,000 the tax is exactly $259.50. At $183 it is $9.4977, which rounds
// to $9, but to $10 if first rounded to the cent.
var moreProbes = []int64{9, 10, 183, 4_999, 5_000, 5_001, 100_000, 1_000_000}

func TestTaxTables(t *testing.T) {
	c, err := tablefile.NewChecker(filepath.Join(tables, "known_gaps.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	rate, err := tablefile.ReadSchedule(filepath.Join(tables, "rate.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if err := rate.CheckPDF(pdfs); err != nil {
		t.Fatal(err)
	}
	tablefile.CheckSchedule(t, "rate.tsv", rate, 0, c, rateTax, newFigure)

	t.Run("rounding", func(t *testing.T) {
		for _, status := range rate.Statuses() {
			t.Run(status, func(t *testing.T) {
				t.Parallel()
				figure := newFigure(t)
				for _, income := range moreProbes {
					b, ok := rate.For(status, income)
					if !ok {
						t.Fatalf("no %s bracket holds %d", status, income)
					}
					c.Check(t, "rate.tsv", b.Over, income, status, rateTax(b, income), figure(t, status, income))
				}
			})
		}
	})
	c.Finish(t)
}
