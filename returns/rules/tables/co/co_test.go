// Package co holds Colorado's tax computation to the 2025 Colorado Income Tax
// Table printed in the DR 0104 booklet, and to the worksheet beneath it for
// taxable income over $50,000.
//
// The rules' figure is DR 0104 line 13, from Prepare on a full-year
// resident's return of wages alone. Colorado starts from federal taxable
// income, so the wages are the target taxable income plus the federal
// standard deduction, and the test checks line 12 lands on the target before
// reading line 13. The expected figure comes only from the TSV files under
// sources/tables/co/2025.
package co

import (
	"path/filepath"
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	"github.com/nexteggapp/taxcalcs/returns/rules/tables/tablefile"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
)

const (
	root   = "../../../.."
	tables = root + "/sources/tables/co/2025"
	pdfs   = root + "/sources/pdfs"
)

// The 2025 federal standard deduction for a filer under 65 and not blind,
// from the Form 1040 instructions.
var federalStandardDeduction = map[string]int64{
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
		in := tablefile.WageInputs(status, income+federalStandardDeduction[status])
		in.States = []*returnsv1.StateInputs{{
			State:  "CO",
			Inputs: &returnsv1.StateInputs_Co{Co: &returnsv1.ColoradoInputs{}},
		}}
		out, err := e.Prepare(in)
		if err != nil {
			t.Fatalf("%s at %d: %v", status, income, err)
		}
		f := out.GetStates()[0].GetCo().GetForms().GetFormDr0104()
		if got := f.GetColoradoTaxableIncome().GetMinor(); got != income*100 {
			t.Fatalf("%s: taxable income is %d cents, want %d dollars", status, got, income)
		}
		return f.GetTax().GetMinor()
	}
}

// Taxable income times .044, entered in whole dollars.
func worksheetTax(b tablefile.Bracket, income int64) int64 {
	return tablefile.RoundDollars(b.TaxCents(income)) * 100
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
	if n := len(table.Rows); n != 504 {
		t.Errorf("tax_table.tsv has %d rows; the printed table has 504", n)
	}
	tablefile.CheckTable(t, "tax_table.tsv", table, c, newFigure)

	worksheet, err := tablefile.ReadSchedule(filepath.Join(tables, "over_50000_worksheet.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if err := worksheet.CheckPDF(pdfs); err != nil {
		t.Fatal(err)
	}
	// The table's last row is not over $50,000; the worksheet is for income
	// over it.
	tablefile.CheckSchedule(t, "over_50000_worksheet.tsv", worksheet, 50_001, c, worksheetTax, newFigure)

	c.Finish(t)
}
