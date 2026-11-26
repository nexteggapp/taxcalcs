// Package us holds the federal tax computation to the 2025 Tax Table and Tax
// Computation Worksheet printed in the Form 1040 instructions.
//
// The rules' figure is Form 1040 line 16, from Prepare on a return of wages
// alone: the wages are the target taxable income plus the standard
// deduction, and the test checks line 15 lands on the target before reading
// line 16. The expected figure comes only from the TSV files under
// sources/tables/us/2025.
package us

import (
	"path/filepath"
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	"github.com/nexteggapp/taxcalcs/returns/rules/tables/tablefile"
)

const (
	root   = "../../../.."
	tables = root + "/sources/tables/us/2025"
	pdfs   = root + "/sources/pdfs"
)

// The 2025 standard deduction for a filer under 65 and not blind, from the
// Standard Deduction Chart for Most People (Form 1040 instructions, line
// 12e).
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
		out, err := e.Prepare(tablefile.WageInputs(status, income+standardDeduction[status]))
		if err != nil {
			t.Fatalf("%s at %d: %v", status, income, err)
		}
		f := out.GetFederal().GetForms().GetForm_1040()
		if got := f.GetTaxableIncome().GetMinor(); got != income*100 {
			t.Fatalf("%s: taxable income is %d cents, want %d dollars", status, got, income)
		}
		return f.GetTax().GetMinor()
	}
}

// The worksheet's tax, (a) × (b) − (d), rounded to whole dollars as line 16
// is entered.
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
	if n := len(table.Rows); n != 2062 {
		t.Errorf("tax_table.tsv has %d rows; the printed table has 2,062", n)
	}
	tablefile.CheckTable(t, "tax_table.tsv", table, c, newFigure)

	tcw, err := tablefile.ReadSchedule(filepath.Join(tables, "tax_computation_worksheet.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if err := tcw.CheckPDF(pdfs); err != nil {
		t.Fatal(err)
	}
	// The worksheet is for taxable income of $100,000 or more; below that the
	// table above applies.
	tablefile.CheckSchedule(t, "tax_computation_worksheet.tsv", tcw, 100_000, c, worksheetTax, newFigure)

	c.Finish(t)
}
