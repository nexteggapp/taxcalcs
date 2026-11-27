// Package ri holds Rhode Island's tax computation to the 2025 Rhode Island
// Tax Table and Tax Computation Worksheet, printed in the RI tax tables.
//
// The rules' figure is Form RI-1040 line 8, from Prepare on a return of wages
// alone. The test searches for the wages that put line 7, taxable income, on
// the target, and checks it lands there before reading line 8. The expected
// figure comes only from the TSV files under sources/tables/ri/2025.
package ri

import (
	"path/filepath"
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	"github.com/nexteggapp/taxcalcs/returns/rules/tables/tablefile"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
)

const (
	root   = "../../../.."
	tables = root + "/sources/tables/ri/2025"
	pdfs   = root + "/sources/pdfs"
)

func newFigure(t *testing.T) tablefile.Figure {
	e, err := federal.LoadEngine()
	if err != nil {
		t.Fatal(err)
	}
	prepare := func(t *testing.T, status string, wages int64) (taxable, tax int64) {
		t.Helper()
		in := tablefile.WageInputs(status, wages)
		in.States = []*returnsv1.StateInputs{{
			State:  "RI",
			Inputs: &returnsv1.StateInputs_Ri{Ri: &returnsv1.RhodeIslandInputs{}},
		}}
		out, err := e.Prepare(in)
		if err != nil {
			t.Fatalf("%s at wages %d: %v", status, wages, err)
		}
		f := out.GetStates()[0].GetRi().GetForms().GetFormRi1040()
		return f.GetTaxableIncome().GetMinor(), f.GetIncomeTax().GetMinor()
	}
	offset := int64(-1)
	return func(t *testing.T, status string, income int64) int64 {
		t.Helper()
		return solve(t, status, income, &offset, prepare)
	}
}

// solve finds wages that put taxable income on income dollars and returns
// the tax there. offset is the wages above taxable income the last
// probe needed, carried from probe to probe; -1 starts it from a return of
// $50,000. It steps by the miss, and falls back to bisection when stepping
// does not land, as when a deduction is held back at zero income or changes
// in steps.
func solve(t *testing.T, status string, income int64, offset *int64,
	prepare func(t *testing.T, status string, wages int64) (taxable, tax int64)) int64 {
	t.Helper()
	if *offset < 0 {
		taxable, _ := prepare(t, status, 50_000)
		*offset = 50_000 - taxable/100
	}
	want := income * 100
	lo, hi := int64(-1), int64(-1) // wages known to fall short of and pass the target
	w := income + *offset
	for range 6 {
		taxable, tax := prepare(t, status, w)
		if taxable == want {
			*offset = w - income
			return tax
		}
		if taxable < want {
			lo = w
		} else {
			hi = w
		}
		if taxable == 0 {
			break
		}
		w += income - taxable/100
	}
	if lo < 0 {
		lo = 0
	}
	if hi < 0 {
		for hi = lo + 1024; ; hi *= 2 {
			if taxable, _ := prepare(t, status, hi); taxable >= want {
				break
			}
		}
	}
	for lo+1 < hi {
		mid := (lo + hi) / 2
		taxable, tax := prepare(t, status, mid)
		switch {
		case taxable == want:
			*offset = mid - income
			return tax
		case taxable < want:
			lo = mid
		default:
			hi = mid
		}
	}
	if taxable, tax := prepare(t, status, hi); taxable == want {
		*offset = hi - income
		return tax
	}
	t.Fatalf("%s: no whole-dollar wages put taxable income on %d dollars", status, income)
	return 0
}

// The tax to the cent: the form does not round it to whole dollars.
func scheduleTax(b tablefile.Bracket, income int64) int64 {
	return b.TaxCents(income)
}

// checkSpread checks a schedule at more incomes than its edges, for a top
// bracket whose only edge is its start.
func checkSpread(t *testing.T, name string, s *tablefile.Schedule, incomes []int64, c *tablefile.Checker) {
	t.Run(name+" spread", func(t *testing.T) {
		for _, status := range s.Statuses() {
			t.Run(status, func(t *testing.T) {
				t.Parallel()
				figure := newFigure(t)
				for _, income := range incomes {
					b, ok := s.For(status, income)
					if !ok {
						t.Fatalf("%s: no %s bracket holds %d", name, status, income)
					}
					c.Check(t, name, b.Over, income, status, scheduleTax(b, income), figure(t, status, income))
				}
			})
		}
	})
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

	tcw, err := tablefile.ReadSchedule(filepath.Join(tables, "tax_computation_worksheet.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if err := tcw.CheckPDF(pdfs); err != nil {
		t.Fatal(err)
	}
	// The worksheet is for taxable income of $100,000 or more; below that the\ntable applies.
	tablefile.CheckSchedule(t, "tax_computation_worksheet.tsv", tcw, 100_000, c, scheduleTax, newFigure)
	checkSpread(t, "tax_computation_worksheet.tsv", tcw, []int64{1_000_000, 10_000_000}, c)

	c.Finish(t)
}
