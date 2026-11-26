// Package la holds Louisiana's tax computation to the flat rate printed on
// the 2025 Form IT-540.
//
// The rules' figure is Form IT-540 line 11, from Prepare on a full-year
// resident's return of wages alone: the wages are the target taxable income
// plus Louisiana's standard deduction, and the test checks line 10 lands on
// the target before reading line 11. The expected figure comes only from the
// TSV file under sources/tables/la/2025.
package la

import (
	"path/filepath"
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	"github.com/nexteggapp/taxcalcs/returns/rules/tables/tablefile"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
)

const (
	root   = "../../../.."
	tables = root + "/sources/tables/la/2025"
	pdfs   = root + "/sources/pdfs"
)

// The 2025 Louisiana standard deduction for Form IT-540 line 8, from the
// instructions' "Louisiana Standard Deduction – Line 8".
var standardDeduction = map[string]int64{
	"single": 12_500,
	"mfj":    25_000,
	"mfs":    12_500,
	"hoh":    25_000,
	"qss":    25_000,
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
			State:  "LA",
			Inputs: &returnsv1.StateInputs_La{La: &returnsv1.LouisianaInputs{}},
		}}
		out, err := e.Prepare(in)
		if err != nil {
			t.Fatalf("%s at %d: %v", status, income, err)
		}
		f := out.GetStates()[0].GetLa().GetForms().GetFormIt540()
		if got := f.GetTaxableIncome().GetMinor(); got != income*100 {
			t.Fatalf("%s: taxable income is %d cents, want %d dollars", status, got, income)
		}
		return f.GetIncomeTax().GetMinor()
	}
}

// Line 11 is the rate times line 10, rounded to the nearest dollar.
func rateTax(b tablefile.Bracket, income int64) int64 {
	return tablefile.RoundDollars(b.TaxCents(income)) * 100
}

// A flat rate has one edge, at zero, so the test also probes incomes whose
// tax lands either side of 50 cents, and a few larger ones.
var moreProbes = []int64{16, 17, 49, 50, 50_016, 50_017, 100_000, 1_000_000}

func TestTaxRate(t *testing.T) {
	c, err := tablefile.NewChecker(filepath.Join(tables, "known_gaps.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	const name = "tax_rate.tsv"
	s, err := tablefile.ReadSchedule(filepath.Join(tables, name))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CheckPDF(pdfs); err != nil {
		t.Fatal(err)
	}
	tablefile.CheckSchedule(t, name, s, 0, c, rateTax, newFigure)

	t.Run("more", func(t *testing.T) {
		for _, status := range s.Statuses() {
			t.Run(status, func(t *testing.T) {
				t.Parallel()
				figure := newFigure(t)
				for _, income := range moreProbes {
					b, _ := s.For(status, income)
					c.Check(t, name, b.Over, income, status, rateTax(b, income), figure(t, status, income))
				}
			})
		}
	})

	c.Finish(t)
}
