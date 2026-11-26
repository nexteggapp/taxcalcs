// Package ca holds California's tax computation to the 2025 California Tax
// Table and Tax Rate Schedules printed in the Form 540 booklet.
//
// The rules' figure is Form 540 line 31, from Prepare on a full-year
// resident's return of wages alone: the wages are the target taxable income
// plus California's standard deduction, and the test checks line 19 lands on
// the target before reading line 31. The expected figure comes only from the
// TSV files under sources/tables/ca/2025.
package ca

import (
	"path/filepath"
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	"github.com/nexteggapp/taxcalcs/returns/rules/tables/tablefile"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
)

const (
	root   = "../../../.."
	tables = root + "/sources/tables/ca/2025"
	pdfs   = root + "/sources/pdfs"
)

// The 2025 California standard deduction, from the booklet's Standard
// Deduction chart for Form 540 line 18. Head of household takes the joint
// amount.
var standardDeduction = map[string]int64{
	"single": 5_706,
	"mfj":    11_412,
	"mfs":    5_706,
	"hoh":    11_412,
	"qss":    11_412,
}

func newFigure(t *testing.T) tablefile.Figure {
	e, err := federal.LoadEngine()
	if err != nil {
		t.Fatal(err)
	}
	return func(t *testing.T, status string, income int64) int64 {
		t.Helper()
		in := tablefile.WageInputs(status, income+standardDeduction[status])
		yes := true
		in.States = []*returnsv1.StateInputs{{
			State: "CA",
			Inputs: &returnsv1.StateInputs_Ca{Ca: &returnsv1.CaliforniaInputs{
				DeductionMethod:        returnsv1.DeductionMethodElection_DEDUCTION_METHOD_ELECTION_STANDARD,
				FullYearHealthCoverage: &yes,
			}},
		}}
		out, err := e.Prepare(in)
		if err != nil {
			t.Fatalf("%s at %d: %v", status, income, err)
		}
		f := out.GetStates()[0].GetCa().GetForms().GetForm_540()
		if got := f.GetTaxableIncome().GetMinor(); got != income*100 {
			t.Fatalf("%s: taxable income is %d cents, want %d dollars", status, got, income)
		}
		return f.GetTaxBeforeExemptionCredits().GetMinor()
	}
}

// The schedules' tax as the booklet figures it: the excess times the rate,
// rounded to the cent, plus the base tax, then rounded to whole dollars for
// line 31.
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
	if n := len(table.Rows); n != 1001 {
		t.Errorf("tax_table.tsv has %d rows; the printed table has 1,001", n)
	}
	tablefile.CheckTable(t, "tax_table.tsv", table, c, newFigure)

	schedules, err := tablefile.ReadSchedule(filepath.Join(tables, "rate_schedules.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if err := schedules.CheckPDF(pdfs); err != nil {
		t.Fatal(err)
	}
	// The booklet says to use the schedules only for taxable income over
	// $100,000; the table covers $100,000 or less.
	tablefile.CheckSchedule(t, "rate_schedules.tsv", schedules, 100_001, c, scheduleTax, newFigure)

	c.Finish(t)
}
