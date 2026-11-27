// Package ks holds Kansas's tax computation to the 2025 Kansas Tax Table
// printed in the Form K-40 instructions, for taxable income to $100,000, and
// to the Tax Computation Worksheet for $100,001 and over.
//
// The rules' figure is Form K-40 line 8, from Prepare on a full-year
// resident's return of wages alone: the wages are the target taxable income
// plus the standard deduction and the exemption allowance, and the test
// checks line 7 lands on the target before reading line 8. The expected
// figure comes only from the TSV files under sources/tables/ks/2025.
package ks

import (
	"path/filepath"
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	"github.com/nexteggapp/taxcalcs/returns/rules/tables/tablefile"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
)

const (
	root   = "../../../.."
	tables = root + "/sources/tables/ks/2025"
	pdfs   = root + "/sources/pdfs"
)

// The 2025 Kansas standard deduction (Form K-40 line 4) plus the exemption
// allowance (line 5): $9,160 for the filer, $18,320 on a joint return, and
// $2,320 more for a head of household.
var standardDeduction = map[string]int64{
	"single": 3_605 + 9_160,
	"mfj":    8_240 + 18_320,
	"mfs":    4_120 + 9_160,
	"hoh":    6_180 + 9_160 + 2_320,
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
			State:  "KS",
			Inputs: &returnsv1.StateInputs_Ks{Ks: &returnsv1.KansasInputs{}},
		}}
		out, err := e.Prepare(in)
		if err != nil {
			t.Fatalf("%s at %d: %v", status, income, err)
		}
		f := out.GetStates()[0].GetKs().GetForms().GetFormK40()
		if got := f.GetTaxableIncome().GetMinor(); got != income*100 {
			t.Fatalf("%s: taxable income is %d cents, want %d dollars", status, got, income)
		}
		return f.GetTax().GetMinor()
	}
}

// The worksheet: line 7 times .0558 less the subtraction amount, entered in
// whole dollars.
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
	if n := len(table.Rows); n != 2000 {
		t.Errorf("tax_table.tsv has %d rows; the printed table has 2,000", n)
	}
	tablefile.CheckTable(t, "tax_table.tsv", table, c, newFigure)

	sched, err := tablefile.ReadSchedule(filepath.Join(tables, "tax_computation_worksheet.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if err := sched.CheckPDF(pdfs); err != nil {
		t.Fatal(err)
	}
	// The table covers taxable income to $100,000; the worksheet is for
	// $100,001 and over.
	tablefile.CheckSchedule(t, "tax_computation_worksheet.tsv", sched, 100_001, c, scheduleTax, newFigure)

	c.Finish(t)
}
