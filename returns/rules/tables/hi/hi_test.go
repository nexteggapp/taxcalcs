// Package hi holds Hawaii's tax computation to the 2025 Tax Table printed in
// the Form N-11 instructions, for taxable income under $100,000, and to the
// Tax Rate Schedules for $100,000 or more.
//
// The rules' figure is Form N-11 line 27, from Prepare on a full-year
// resident's return of wages alone: the wages are the target taxable income
// plus the standard deduction and $1,144 for each exemption, and the test
// checks line 26 lands on the target before reading line 27. The expected
// figure comes only from the TSV files under sources/tables/hi/2025.
package hi

import (
	"path/filepath"
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	"github.com/nexteggapp/taxcalcs/returns/rules/tables/tablefile"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
)

const (
	root   = "../../../.."
	tables = root + "/sources/tables/hi/2025"
	pdfs   = root + "/sources/pdfs"
)

// The 2025 Hawaii standard deduction (Form N-11 line 23: $4,400, $8,800 on a
// joint return or for a qualifying surviving spouse, $6,424 for a head of
// household) plus $1,144 for each exemption (line 25): one for the filer and
// one for a spouse on a joint return.
var standardDeduction = map[string]int64{
	"single": 4_400 + 1_144,
	"mfj":    8_800 + 2*1_144,
	"mfs":    4_400 + 1_144,
	"hoh":    6_424 + 1_144,
	"qss":    8_800 + 1_144,
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
			State:  "HI",
			Inputs: &returnsv1.StateInputs_Hi{Hi: &returnsv1.HawaiiInputs{}},
		}}
		out, err := e.Prepare(in)
		if err != nil {
			t.Fatalf("%s at %d: %v", status, income, err)
		}
		f := out.GetStates()[0].GetHi().GetForms().GetFormN11()
		if got := f.GetTaxableIncome().GetMinor(); got != income*100 {
			t.Fatalf("%s: taxable income is %d cents, want %d dollars", status, got, income)
		}
		return f.GetTax().GetMinor()
	}
}

// The schedules: base plus rate times the excess, entered in whole dollars.
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

	sched, err := tablefile.ReadSchedule(filepath.Join(tables, "rate_schedules.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if err := sched.CheckPDF(pdfs); err != nil {
		t.Fatal(err)
	}
	// The table is for taxable income under $100,000, the schedules for
	// $100,000 or more.
	tablefile.CheckSchedule(t, "rate_schedules.tsv", sched, 100_000, c, scheduleTax, newFigure)

	c.Finish(t)
}
