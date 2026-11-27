// Package dc holds the District of Columbia's tax computation to the 2025
// tax tables for income of $100,000 or less printed in the D-40 booklet, and
// to Calculation I/Tax Rate Schedule for taxable income over $100,000.
//
// The rules' figure is D-40 line 20, from Prepare on a full-year resident's
// return of wages alone: the wages are the target taxable income plus the
// District's standard deduction, and the test checks line 19 lands on the
// target before reading line 20. The expected figure comes only from the TSV
// files under sources/tables/dc/2025.
package dc

import (
	"path/filepath"
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	"github.com/nexteggapp/taxcalcs/returns/rules/tables/tablefile"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
)

const (
	root   = "../../../.."
	tables = root + "/sources/tables/dc/2025"
	pdfs   = root + "/sources/pdfs"
)

// The 2025 District standard deduction, from the booklet's chart for line 18.
var standardDeduction = map[string]int64{
	"single": 15_000,
	"mfj":    30_000,
	"mfs":    15_000,
	"hoh":    22_500,
	"qss":    30_000,
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
			State:  "DC",
			Inputs: &returnsv1.StateInputs_Dc{Dc: &returnsv1.DistrictOfColumbiaInputs{}},
		}}
		out, err := e.Prepare(in)
		if err != nil {
			t.Fatalf("%s at %d: %v", status, income, err)
		}
		f := out.GetStates()[0].GetDc().GetForms().GetFormD40()
		if got := f.GetTaxableIncome().GetMinor(); got != income*100 {
			t.Fatalf("%s: taxable income is %d cents, want %d dollars", status, got, income)
		}
		return f.GetTax().GetMinor()
	}
}

// Calculation I: base plus rate times the excess, entered in whole dollars.
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
	if n := len(table.Rows); n != 2001 {
		t.Errorf("tax_table.tsv has %d rows; the printed table has 2,001", n)
	}
	tablefile.CheckTable(t, "tax_table.tsv", table, c, newFigure)

	calc, err := tablefile.ReadSchedule(filepath.Join(tables, "calculation_i.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if err := calc.CheckPDF(pdfs); err != nil {
		t.Fatal(err)
	}
	// The tables cover $100,000 or less; Calculation I is for more.
	tablefile.CheckSchedule(t, "calculation_i.tsv", calc, 100_001, c, scheduleTax, newFigure)

	c.Finish(t)
}
