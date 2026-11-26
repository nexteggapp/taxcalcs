// Package md holds Maryland's state tax computation to the 2025 Maryland Tax
// Table and tax computation worksheet schedules printed in the resident
// booklet. The county (local) tax on line 28 is not covered.
//
// The rules' figure is Form 502 line 21, from Prepare on a full-year
// resident's return with no federal income: the target taxable net income
// plus the standard deduction and the full personal exemptions is entered as
// a Maryland addition, so line 20 lands on the target without the exemption
// shrinking at higher federal AGI. The test checks line 20 before reading
// line 21. The expected figure comes only from the TSV files under
// sources/tables/md/2025.
package md

import (
	"path/filepath"
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	"github.com/nexteggapp/taxcalcs/returns/rules/tables/tablefile"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
)

const (
	root   = "../../../.."
	tables = root + "/sources/tables/md/2025"
	pdfs   = root + "/sources/pdfs"
)

// The 2025 Maryland standard deduction (Instruction 16) plus the personal
// exemptions of $3,200 each (Instruction 10) a filer with federal AGI of zero
// takes: one, or two on a joint return.
var deductions = map[string]int64{
	"single": 3_350 + 3_200,
	"mfj":    6_700 + 2*3_200,
	"mfs":    3_350 + 3_200,
	"hoh":    6_700 + 3_200,
	"qss":    6_700 + 3_200,
}

func newFigure(t *testing.T) tablefile.Figure {
	e, err := federal.LoadEngine()
	if err != nil {
		t.Fatal(err)
	}
	return func(t *testing.T, status string, income int64) int64 {
		t.Helper()
		in := tablefile.WageInputs(status, 0)
		in.States = []*returnsv1.StateInputs{{
			State: "MD",
			Inputs: &returnsv1.StateInputs_Md{Md: &returnsv1.MarylandInputs{
				OtherAdditions: &commonv1.Money{Currency: "USD", Minor: (income + deductions[status]) * 100},
			}},
		}}
		out, err := e.Prepare(in)
		if err != nil {
			t.Fatalf("%s at %d: %v", status, income, err)
		}
		f := out.GetStates()[0].GetMd().GetForms().GetForm_502()
		if got := f.GetTaxableNetIncome().GetMinor(); got != income*100 {
			t.Fatalf("%s: taxable net income is %d cents, want %d dollars", status, got, income)
		}
		return f.GetMarylandTax().GetMinor()
	}
}

// The worksheet's tax: (a) less (b), times (d), plus (f), entered on line 21
// in whole dollars.
func worksheetTax(b tablefile.Bracket, income int64) int64 {
	return b.TaxDollars(income) * 100
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

	schedules, err := tablefile.ReadSchedule(filepath.Join(tables, "rate_schedules.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if err := schedules.CheckPDF(pdfs); err != nil {
		t.Fatal(err)
	}
	// The table covers taxable net income below $100,000; the schedules
	// cover $100,000 or more.
	tablefile.CheckSchedule(t, "rate_schedules.tsv", schedules, 100_000, c, worksheetTax, newFigure)

	c.Finish(t)
}
