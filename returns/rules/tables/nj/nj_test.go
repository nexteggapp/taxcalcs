// Package nj holds New Jersey's tax computation to the 2025 Tax Table and Tax
// Rate Schedules printed in the Form NJ-1040 instructions.
//
// The rules' figure is Form NJ-1040 line 43, from Prepare on a full-year
// resident's return of wages alone: the wages are the target taxable income
// plus the regular exemptions, and the test checks line 42 lands on the
// target before reading line 43. The expected figure comes only from the TSV
// files under sources/tables/nj/2025.
package nj

import (
	"path/filepath"
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	"github.com/nexteggapp/taxcalcs/returns/rules/tables/tablefile"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
)

const (
	root   = "../../../.."
	tables = root + "/sources/tables/nj/2025"
	pdfs   = root + "/sources/pdfs"
)

// The 2025 regular exemption, $1,000 for the filer and, on a joint return,
// $1,000 for the spouse (Form NJ-1040 lines 6 and 13). A filer under 65 with
// no dependents claims nothing else.
var exemptions = map[string]int64{
	"single": 1_000,
	"mfj":    2_000,
	"mfs":    1_000,
	"hoh":    1_000,
	"qss":    1_000,
}

func newFigure(t *testing.T) tablefile.Figure {
	e, err := federal.LoadEngine()
	if err != nil {
		t.Fatal(err)
	}
	return func(t *testing.T, status string, income int64) int64 {
		t.Helper()
		in := tablefile.WageInputs(status, income+exemptions[status])
		in.States = []*returnsv1.StateInputs{{
			State:  "NJ",
			Inputs: &returnsv1.StateInputs_Nj{Nj: &returnsv1.NewJerseyInputs{}},
		}}
		out, err := e.Prepare(in)
		if err != nil {
			t.Fatalf("%s at %d: %v", status, income, err)
		}
		f := out.GetStates()[0].GetNj().GetForms().GetFormNj1040()
		if got := f.GetTaxableIncome().GetMinor(); got != income*100 {
			t.Fatalf("%s: taxable income is %d cents, want %d dollars", status, got, income)
		}
		return f.GetTax().GetMinor()
	}
}

// The schedules' tax: line 42 times the rate, less the amount to subtract,
// in dollars and cents. The booklet lets a filer round every line to whole
// dollars, but does not require it, so the test holds the rules to the cent.
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
	if n := len(table.Rows); n != 2000 {
		t.Errorf("tax_table.tsv has %d rows; the printed table has 2,000", n)
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
