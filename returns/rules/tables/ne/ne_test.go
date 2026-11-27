// Package ne holds Nebraska's tax computation to the 2025 Tax Calculation
// Schedule, the Nebraska Tax Table and the worksheet printed at the end of
// the table for income above it.
//
// Form 1040N line 15 lets paper filers use the Nebraska Tax Table and has
// everyone else use the Tax Calculation Schedule. Both are printed for the
// same line, so the test holds the rules to each.
//
// The rules' figure is line 15, from Prepare on a full-year resident's return
// of wages alone: the wages are the target taxable income plus the Nebraska
// standard deduction, and the test checks line 14 lands on the target before
// reading line 15. The expected figure comes only from the TSV file under
// sources/tables/ne/2025.
package ne

import (
	"path/filepath"
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	"github.com/nexteggapp/taxcalcs/returns/rules/tables/tablefile"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
)

const (
	root   = "../../../.."
	tables = root + "/sources/tables/ne/2025"
	pdfs   = root + "/sources/pdfs"
)

// The 2025 Nebraska standard deduction for a filer under 65 and not blind,
// from the Form 1040N line 6 instructions. A qualifying surviving spouse
// takes the joint amount.
var standardDeduction = map[string]int64{
	"single": 8_600,
	"mfj":    17_200,
	"mfs":    8_600,
	"hoh":    12_600,
	"qss":    17_200,
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
			State:  "NE",
			Inputs: &returnsv1.StateInputs_Ne{Ne: &returnsv1.NebraskaInputs{}},
		}}
		out, err := e.Prepare(in)
		if err != nil {
			t.Fatalf("%s at %d: %v", status, income, err)
		}
		f := out.GetStates()[0].GetNe().GetForms().GetForm_1040N()
		if got := f.GetTaxableIncome().GetMinor(); got != income*100 {
			t.Fatalf("%s: taxable income is %d cents, want %d dollars", status, got, income)
		}
		return f.GetIncomeTax().GetMinor()
	}
}

// The schedule's tax: the base tax plus the rate times the excess, rounded
// once to the whole dollar Form 1040N is filled in with.
func scheduleTax(b tablefile.Bracket, income int64) int64 {
	return b.TaxDollars(income) * 100
}

// A one-row worksheet has a single edge, so the test also probes a few
// incomes above it.
var worksheetProbes = []int64{77_770, 77_771, 100_000, 250_005, 1_000_000}

func TestTaxTables(t *testing.T) {
	c, err := tablefile.NewChecker(filepath.Join(tables, "known_gaps.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	const name = "tax_calculation_schedule.tsv"
	s, err := tablefile.ReadSchedule(filepath.Join(tables, name))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CheckPDF(pdfs); err != nil {
		t.Fatal(err)
	}
	tablefile.CheckSchedule(t, name, s, 0, c, scheduleTax, newFigure)

	table, err := tablefile.ReadTable(filepath.Join(tables, "tax_table.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if err := table.CheckPDF(pdfs); err != nil {
		t.Fatal(err)
	}
	if n := len(table.Rows); n != 777 {
		t.Errorf("tax_table.tsv has %d rows; the printed table has 777", n)
	}
	tablefile.CheckTable(t, "tax_table.tsv", table, c, newFigure)

	const ws = "over_table_worksheet.tsv"
	w, err := tablefile.ReadSchedule(filepath.Join(tables, ws))
	if err != nil {
		t.Fatal(err)
	}
	if err := w.CheckPDF(pdfs); err != nil {
		t.Fatal(err)
	}
	// The worksheet is for income over $77,760, where the table ends.
	tablefile.CheckSchedule(t, ws, w, 77_761, c, scheduleTax, newFigure)
	t.Run("more", func(t *testing.T) {
		for _, status := range w.Statuses() {
			t.Run(status, func(t *testing.T) {
				t.Parallel()
				figure := newFigure(t)
				for _, income := range worksheetProbes {
					b, _ := w.For(status, income)
					c.Check(t, ws, b.Over, income, status, scheduleTax(b, income), figure(t, status, income))
				}
			})
		}
	})

	c.Finish(t)
}
