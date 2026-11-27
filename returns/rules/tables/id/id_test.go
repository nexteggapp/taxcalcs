// Package id holds Idaho's tax computation to the worksheet the 2025 Form 40
// instructions print for line 20: 5.3% of taxable income above a zero-rate
// amount. Idaho prints no tax table.
//
// The rules' figure is Form 40 line 20, from Prepare on a full-year
// resident's return of wages alone: the wages are the target taxable income
// plus the standard deduction, and the test checks line 19 lands on the
// target before reading line 20. The expected figure comes only from the TSV
// file under sources/tables/id/2025.
package id

import (
	"path/filepath"
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	"github.com/nexteggapp/taxcalcs/returns/rules/tables/tablefile"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
)

const (
	root   = "../../../.."
	tables = root + "/sources/tables/id/2025"
	pdfs   = root + "/sources/pdfs"
)

// The 2025 standard deduction for a filer under 65 and not blind, which Idaho
// takes from the federal return (Form 40 line 16).
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
			State:  "ID",
			Inputs: &returnsv1.StateInputs_Id{Id: &returnsv1.IdahoInputs{}},
		}}
		out, err := e.Prepare(in)
		if err != nil {
			t.Fatalf("%s at %d: %v", status, income, err)
		}
		f := out.GetStates()[0].GetId().GetForms().GetForm_40()
		if got := f.GetTaxableIncome().GetMinor(); got != income*100 {
			t.Fatalf("%s: taxable income is %d cents, want %d dollars", status, got, income)
		}
		return f.GetTax().GetMinor()
	}
}

// The worksheet: 5.3% of the excess over the zero-rate amount, in whole
// dollars.
func worksheetTax(b tablefile.Bracket, income int64) int64 {
	return tablefile.RoundDollars(b.TaxCents(income)) * 100
}

func TestTaxTables(t *testing.T) {
	c, err := tablefile.NewChecker(filepath.Join(tables, "known_gaps.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	worksheet, err := tablefile.ReadSchedule(filepath.Join(tables, "tax_worksheet.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if err := worksheet.CheckPDF(pdfs); err != nil {
		t.Fatal(err)
	}
	tablefile.CheckSchedule(t, "tax_worksheet.tsv", worksheet, 0, c, worksheetTax, newFigure)
	c.Finish(t)
}
