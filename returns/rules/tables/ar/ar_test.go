// Package ar holds Arkansas's tax computation to the 2025 Regular Income Tax
// Table printed in the Form AR1000F instructions, and to the note beneath it
// for net taxable income over $100,000.
//
// The rules' figure is Form AR1000F line 29, from Prepare on a full-year
// resident's return of wages alone: the wages are the target net taxable
// income plus the standard deduction, and the test checks line 28 lands on
// the target before reading line 29. The expected figure comes only from the
// TSV files under sources/tables/ar/2025.
package ar

import (
	"path/filepath"
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	"github.com/nexteggapp/taxcalcs/returns/rules/tables/tablefile"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
)

const (
	root   = "../../../.."
	tables = root + "/sources/tables/ar/2025"
	pdfs   = root + "/sources/pdfs"
)

// The 2025 Arkansas standard deduction, from the instructions for line 27:
// $2,470 for each filer, so $4,940 on a joint return.
var standardDeduction = map[string]int64{
	"single": 2_470,
	"mfj":    4_940,
	"mfs":    2_470,
	"hoh":    2_470,
	"qss":    2_470,
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
			State:  "AR",
			Inputs: &returnsv1.StateInputs_Ar{Ar: &returnsv1.ArkansasInputs{}},
		}}
		out, err := e.Prepare(in)
		if err != nil {
			t.Fatalf("%s at %d: %v", status, income, err)
		}
		f := out.GetStates()[0].GetAr().GetForms().GetFormAr1000F()
		if got := f.GetNetTaxableIncome().GetMinor(); got != income*100 {
			t.Fatalf("%s: net taxable income is %d cents, want %d dollars", status, got, income)
		}
		return f.GetTax().GetMinor()
	}
}

// $3,809 plus 3.9% of the excess over $100,000, entered in whole dollars.
func scheduleTax(b tablefile.Bracket, income int64) int64 {
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
	if n := len(table.Rows); n != 950 {
		t.Errorf("tax_table.tsv has %d rows; the printed table has 950", n)
	}
	tablefile.CheckTable(t, "tax_table.tsv", table, c, newFigure)

	over, err := tablefile.ReadSchedule(filepath.Join(tables, "over_100000.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if err := over.CheckPDF(pdfs); err != nil {
		t.Fatal(err)
	}
	// The table's last row is less than $100,001, and the note covers
	// "$100,001 and over".
	tablefile.CheckSchedule(t, "over_100000.tsv", over, 100_001, c, scheduleTax, newFigure)

	c.Finish(t)
}
