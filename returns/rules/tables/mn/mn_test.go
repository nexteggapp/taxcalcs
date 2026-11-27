// Package mn holds Minnesota's tax computation to the 2025 Minnesota Tax
// Tables and Tax Rate Schedules printed in the Form M1 instructions.
//
// The rules' figure is Form M1 line 10, from Prepare on a full-year
// resident's return of wages alone. The wages start at the target taxable
// income plus the standard deduction; where the deduction limitation moves
// line 9 off the target, the wages are raised or a Schedule M1M subtraction
// (line 7) takes off the excess, and the test checks line 9 lands on the
// target before reading line 10. The expected figure comes only from the TSV
// files under sources/tables/mn/2025.
//
// tax_table.tsv was extracted from the plain pdftotext -layout text with
//
//	go run ./tools/tableextract -source mn-dor-form-m1-instructions-2025 \
//	    -pdf mn/2025/2025-form-m1-instructions.pdf -pages 32-38 \
//	    -columns single,mfj+qss,mfs,hoh -first 0 -last 86800 \
//	    -drop '^ *3[2-8]$' -drop '86,800 & over' -note ... /tmp/mn.txt
//
// The drops blank the page numbers and the figure in the sentence after the
// last row.
package mn

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
	tables = root + "/sources/tables/mn/2025"
	pdfs   = root + "/sources/pdfs"
)

// The 2025 Minnesota standard deduction before the limitation, from the
// Standard Deduction Table for line 4.
var standardDeduction = map[string]int64{
	"single": 14_950,
	"mfj":    29_900,
	"mfs":    14_950,
	"hoh":    22_500,
	"qss":    29_900,
}

func newFigure(t *testing.T) tablefile.Figure {
	e, err := federal.LoadEngine()
	if err != nil {
		t.Fatal(err)
	}
	return func(t *testing.T, status string, income int64) int64 {
		t.Helper()
		wages, subtractions := income+standardDeduction[status], int64(0)
		for range 5 {
			in := tablefile.WageInputs(status, wages)
			in.States = []*returnsv1.StateInputs{{
				State: "MN",
				Inputs: &returnsv1.StateInputs_Mn{Mn: &returnsv1.MinnesotaInputs{
					Subtractions: &commonv1.Money{Currency: "USD", Minor: subtractions * 100},
				}},
			}}
			out, err := e.Prepare(in)
			if err != nil {
				t.Fatalf("%s at %d: %v", status, income, err)
			}
			f := out.GetStates()[0].GetMn().GetForms().GetFormM1()
			got := f.GetTaxableIncome().GetMinor()
			switch {
			case got == income*100:
				return f.GetTax().GetMinor()
			case got%100 != 0:
				t.Fatalf("%s: taxable income is %d cents", status, got)
			case got > income*100:
				subtractions += got/100 - income
			default:
				wages += income - got/100
			}
		}
		t.Fatalf("%s: line 9 did not land on %d dollars", status, income)
		return 0
	}
}

// The schedules' tax: the base plus the rate on the excess, rounded once to
// whole dollars for line 10, as the instructions' rounding rule says.
func scheduleTax(b tablefile.Bracket, income int64) int64 {
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
	if n := len(table.Rows); n != 869 {
		t.Errorf("tax_table.tsv has %d rows; the printed table has 869", n)
	}
	tablefile.CheckTable(t, "tax_table.tsv", table, c, newFigure)

	s, err := tablefile.ReadSchedule(filepath.Join(tables, "rate_schedules.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CheckPDF(pdfs); err != nil {
		t.Fatal(err)
	}
	// The schedules are for line 9 of $86,800 or more.
	tablefile.CheckSchedule(t, "rate_schedules.tsv", s, 86_800, c, scheduleTax, newFigure)

	c.Finish(t)
}
