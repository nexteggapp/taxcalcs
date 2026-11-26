// Package il holds Illinois's tax computation to the 4.95% rate the 2025 Form
// IL-1040 prints for line 12. Illinois prints no tax table.
//
// The rules' figure is Form IL-1040 line 12, from Prepare on a full-year
// resident's return of wages alone: the wages are the target net income plus
// the exemption allowance, and the test checks line 11 lands on the target
// before reading line 12. The expected figure comes only from the TSV file
// under sources/tables/il/2025.
package il

import (
	"path/filepath"
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	"github.com/nexteggapp/taxcalcs/returns/rules/tables/tablefile"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
)

const (
	root   = "../../../.."
	tables = root + "/sources/tables/il/2025"
	pdfs   = root + "/sources/pdfs"
)

// The 2025 Illinois exemption allowance (Form IL-1040 line 10): $2,850 for
// the filer, and as much again for a spouse on a joint return.
var exemption = map[string]int64{
	"single": 2_850,
	"mfj":    5_700,
	"mfs":    2_850,
	"hoh":    2_850,
	"qss":    2_850,
}

func newFigure(t *testing.T) tablefile.Figure {
	e, err := federal.LoadEngine()
	if err != nil {
		t.Fatal(err)
	}
	return func(t *testing.T, status string, income int64) int64 {
		t.Helper()
		in := tablefile.WageInputs(status, income+exemption[status])
		in.States = []*returnsv1.StateInputs{{
			State:  "IL",
			Inputs: &returnsv1.StateInputs_Il{Il: &returnsv1.IllinoisInputs{}},
		}}
		out, err := e.Prepare(in)
		if err != nil {
			t.Fatalf("%s at %d: %v", status, income, err)
		}
		f := out.GetStates()[0].GetIl().GetForms().GetFormIl1040()
		if got := f.GetNetIncome().GetMinor(); got != income*100 {
			t.Fatalf("%s: net income is %d cents, want %d dollars", status, got, income)
		}
		return f.GetTax().GetMinor()
	}
}

// Line 11 times 4.95%, in whole dollars.
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
