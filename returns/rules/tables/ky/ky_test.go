// Package ky holds Kentucky's tax computation to the flat rate printed on the
// 2025 Form 740.
//
// The rules' figure is Form 740 line 12, from Prepare on a full-year
// resident's return of wages alone: the wages are the target taxable income
// plus Kentucky's standard deduction, and the test checks line 11 lands on
// the target before reading line 12. The expected figure comes only from the
// TSV file under sources/tables/ky/2025.
package ky

import (
	"path/filepath"
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	"github.com/nexteggapp/taxcalcs/returns/rules/tables/tablefile"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
)

const (
	root   = "../../../.."
	tables = root + "/sources/tables/ky/2025"
	pdfs   = root + "/sources/pdfs"
)

// The 2025 Kentucky standard deduction, printed on Form 740 line 10: one
// amount per return, whatever the filing status.
const standardDeduction = 3_270

func newFigure(t *testing.T) tablefile.Figure {
	e, err := federal.LoadEngine()
	if err != nil {
		t.Fatal(err)
	}
	return func(t *testing.T, status string, income int64) int64 {
		t.Helper()
		in := tablefile.WageInputs(status, income+standardDeduction)
		in.States = []*returnsv1.StateInputs{{
			State:  "KY",
			Inputs: &returnsv1.StateInputs_Ky{Ky: &returnsv1.KentuckyInputs{}},
		}}
		out, err := e.Prepare(in)
		if err != nil {
			t.Fatalf("%s at %d: %v", status, income, err)
		}
		f := out.GetStates()[0].GetKy().GetForms().GetForm_740()
		if got := f.GetTaxableIncome().GetMinor(); got != income*100 {
			t.Fatalf("%s: taxable income is %d cents, want %d dollars", status, got, income)
		}
		return f.GetTax().GetMinor()
	}
}

// Line 12 is the rate times line 11, entered in whole dollars.
func rateTax(b tablefile.Bracket, income int64) int64 {
	return b.TaxDollars(income) * 100
}

// A flat rate has one edge, at zero, so the test also probes incomes whose
// tax lands either side of 50 cents, and a few larger ones.
var moreProbes = []int64{12, 13, 37, 38, 46_730, 100_012, 100_013, 1_000_000}

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
