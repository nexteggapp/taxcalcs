// Package ia holds Iowa's tax computation to the 3.8% rate the 2025 IA 1040
// prints for line 5. Iowa prints no tax table.
//
// The rules' figure is IA 1040 line 5, from Prepare on a full-year resident's
// return of wages alone. Iowa starts from federal taxable income, so the
// wages are the target taxable income plus the federal standard deduction,
// and the test checks line 4 lands on the target before reading line 5. The
// expected figure comes only from the TSV file under sources/tables/ia/2025.
package ia

import (
	"path/filepath"
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	"github.com/nexteggapp/taxcalcs/returns/rules/tables/tablefile"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
)

const (
	root   = "../../../.."
	tables = root + "/sources/tables/ia/2025"
	pdfs   = root + "/sources/pdfs"
)

// The 2025 federal standard deduction for a filer under 65 and not blind,
// from the Form 1040 instructions.
var standardDeduction = map[string]int64{
	"single": 15_750,
	"mfj":    31_500,
	"mfs":    15_750,
	"hoh":    23_625,
	"qss":    31_500,
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
			State:  "IA",
			Inputs: &returnsv1.StateInputs_Ia{Ia: &returnsv1.IowaInputs{}},
		}}
		out, err := e.Prepare(in)
		if err != nil {
			t.Fatalf("%s at %d: %v", status, income, err)
		}
		f := out.GetStates()[0].GetIa().GetForms().GetFormIa1040()
		if got := f.GetIowaTaxableIncome().GetMinor(); got != income*100 {
			t.Fatalf("%s: taxable income is %d cents, want %d dollars", status, got, income)
		}
		return f.GetIowaTax().GetMinor()
	}
}

// Line 4 times 3.8%, entered in whole dollars.
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
