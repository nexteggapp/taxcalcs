// Package nd holds North Dakota's tax computation to the 2025 Tax Table and
// Tax Rate Schedules printed in the individual income tax booklet.
//
// The rules' figure is Form ND-1 line 20, from Prepare on a full-year
// resident's return of wages alone. North Dakota starts from federal taxable
// income, so the wages are the target taxable income plus the federal
// standard deduction, and the test checks line 18 lands on the target before
// reading line 20. The expected figure comes only from the TSV files under
// sources/tables/nd/2025.
package nd

import (
	"path/filepath"
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	"github.com/nexteggapp/taxcalcs/returns/rules/tables/tablefile"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
)

const (
	root   = "../../../.."
	tables = root + "/sources/tables/nd/2025"
	pdfs   = root + "/sources/pdfs"
)

// The 2025 federal standard deduction for a filer under 65 and not blind,
// from the Standard Deduction Chart for Most People (Form 1040 instructions,
// line 12e).
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
			State:  "ND",
			Inputs: &returnsv1.StateInputs_Nd{Nd: &returnsv1.NorthDakotaInputs{}},
		}}
		out, err := e.Prepare(in)
		if err != nil {
			t.Fatalf("%s at %d: %v", status, income, err)
		}
		f := out.GetStates()[0].GetNd().GetForms().GetFormNd1()
		if got := f.GetTaxableIncome().GetMinor(); got != income*100 {
			t.Fatalf("%s: taxable income is %d cents, want %d dollars", status, got, income)
		}
		return f.GetTax().GetMinor()
	}
}

// The schedules' tax: the base tax plus the rate times the amount over, to
// the cent. The booklet gives no rounding for it.
func scheduleTax(b tablefile.Bracket, income int64) int64 {
	return b.TaxCents(income)
}

func TestTaxTables(t *testing.T) {
	c, err := tablefile.NewChecker(filepath.Join(tables, "known_gaps.tsv"))
	if err != nil {
		t.Fatal(err)
	}

	table, err := tablefile.ReadTable(filepath.Join(tables, "tax_table.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if err := table.CheckPDF(pdfs); err != nil {
		t.Fatal(err)
	}
	if n := len(table.Rows); n != 1195 {
		t.Errorf("tax_table.tsv has %d rows; the printed table has 1,195", n)
	}
	tablefile.CheckTable(t, "tax_table.tsv", table, c, newFigure)

	schedules, err := tablefile.ReadSchedule(filepath.Join(tables, "rate_schedules.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if err := schedules.CheckPDF(pdfs); err != nil {
		t.Fatal(err)
	}
	// The schedules apply to taxable income of $100,000 or more. Below that
	// the table applies, so the probes start at $100,000.
	tablefile.CheckSchedule(t, "rate_schedules.tsv", schedules, 100_000, c, scheduleTax, newFigure)

	c.Finish(t)
}
