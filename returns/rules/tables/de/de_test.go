// Package de holds Delaware's tax computation to the 2025 State Income Tax
// Table for taxable income under $60,000, and the State Income Tax Schedule
// for $60,000 or more, which the Form PIT-RES instructions for line 24 send
// filers to.
//
// The rules' figure is Form PIT-RES line 24, from Prepare on a full-year
// resident's return of wages alone: the wages are the target taxable income
// plus the Delaware standard deduction, and the test checks line 23 lands on
// the target before reading line 24. The expected figure comes only from the
// TSV files under sources/tables/de/2025.
package de

import (
	"path/filepath"
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	"github.com/nexteggapp/taxcalcs/returns/rules/tables/tablefile"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
)

const (
	root   = "../../../.."
	tables = root + "/sources/tables/de/2025"
	pdfs   = root + "/sources/pdfs"
)

// The 2025 Delaware standard deduction, from the instructions for line
// 20a: $6,500 on a joint return, $3,250 otherwise.
var standardDeduction = map[string]int64{
	"single": 3_250,
	"mfj":    6_500,
	"mfs":    3_250,
	"hoh":    3_250,
	"qss":    3_250,
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
			State:  "DE",
			Inputs: &returnsv1.StateInputs_De{De: &returnsv1.DelawareInputs{}},
		}}
		out, err := e.Prepare(in)
		if err != nil {
			t.Fatalf("%s at %d: %v", status, income, err)
		}
		f := out.GetStates()[0].GetDe().GetForms().GetFormPitRes()
		if got := f.GetTaxableIncome().GetMinor(); got != income*100 {
			t.Fatalf("%s: taxable income is %d cents, want %d dollars", status, got, income)
		}
		return f.GetTax().GetMinor()
	}
}

// $2,943.50 plus 6.6% of the excess over $60,000, rounded to the dollar as
// the printed example rounds it.
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
	if n := len(table.Rows); n != 1162 {
		t.Errorf("tax_table.tsv has %d rows; the printed table has 1,162", n)
	}
	tablefile.CheckTable(t, "tax_table.tsv", table, c, newFigure)

	schedule, err := tablefile.ReadSchedule(filepath.Join(tables, "over_60000_schedule.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if err := schedule.CheckPDF(pdfs); err != nil {
		t.Fatal(err)
	}
	// The schedule is for $60,000 or more; the table is for less.
	tablefile.CheckSchedule(t, "over_60000_schedule.tsv", schedule, 60_000, c, scheduleTax, newFigure)

	c.Finish(t)
}
