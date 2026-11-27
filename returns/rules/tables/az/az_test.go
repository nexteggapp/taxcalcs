// Package az holds Arizona's tax computation to the 2.5% rate the 2025 Form
// 140 instructions print for line 46. Arizona prints no tax table.
//
// The rules' figure is Form 140 line 46, from Prepare on a full-year
// resident's return of wages alone: the wages are the target taxable income
// plus the standard deduction, and the test checks line 45 lands on the
// target before reading line 46. The expected figure comes only from the TSV
// file under sources/tables/az/2025.
package az

import (
	"path/filepath"
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	"github.com/nexteggapp/taxcalcs/returns/rules/tables/tablefile"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
)

const (
	root   = "../../../.."
	tables = root + "/sources/tables/az/2025"
	pdfs   = root + "/sources/pdfs"
)

// The 2025 Arizona standard deduction, from the instructions' "Your Standard
// Deduction" chart for line 43.
var standardDeduction = map[string]int64{
	"single": 15_750,
	"mfj":    31_500,
	"mfs":    15_750,
	"hoh":    23_625,
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
			State:  "AZ",
			Inputs: &returnsv1.StateInputs_Az{Az: &returnsv1.ArizonaInputs{}},
		}}
		out, err := e.Prepare(in)
		if err != nil {
			t.Fatalf("%s at %d: %v", status, income, err)
		}
		f := out.GetStates()[0].GetAz().GetForms().GetForm_140()
		if got := f.GetTaxableIncome().GetMinor(); got != income*100 {
			t.Fatalf("%s: taxable income is %d cents, want %d dollars", status, got, income)
		}
		return f.GetTax().GetMinor()
	}
}

// Line 46 is line 45 times 2.5%, entered in whole dollars: the instructions
// say to round to the nearest whole dollar, 50 cents or more up (page 4,
// Tips for Preparing Your Return).
// The product is rounded once, not first to the cent.
func rateTax(b tablefile.Bracket, income int64) int64 {
	return b.TaxDollars(income) * 100
}

// A flat rate has one edge, at zero, so the test also probes incomes whose
// tax lands just below, on and just above 50 cents, and a few large ones.
// At $20 and $100,020 the tax ends in exactly 50 cents.
var moreProbes = []int64{19, 20, 21, 50_000, 100_019, 100_020, 100_021, 1_000_000}

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
