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

// Line 15c times 5.19%, rounded to the nearest dollar.
func rateTax(b tablefile.Bracket, income int64) int64 {
	return tablefile.RoundDollars(b.TaxCents(income)) * 100
}

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
	c.Finish(t)
}
