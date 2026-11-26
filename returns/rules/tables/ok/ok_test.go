// Package ok holds Oklahoma's tax computation to the 2025 Oklahoma Income
// Tax Table and the tax computation worksheets for $100,000 or more, printed
// in the Form 511 packet.
//
// The rules' figure is Form 511 line 14a, from Prepare on a return of wages
// alone. The test searches for the wages that put line 13, Oklahoma taxable
// income, on the target, and checks it lands there before reading line 14a.
// The expected figure comes only from the TSV files under
// sources/tables/ok/2025.
package ok

import (
	"path/filepath"
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	"github.com/nexteggapp/taxcalcs/returns/rules/tables/tablefile"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
)

const (
	root   = "../../../.."
	tables = root + "/sources/tables/ok/2025"
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
			State:  "OK",
			Inputs: &returnsv1.StateInputs_Ok{Ok: &returnsv1.OklahomaInputs{}},
		}}
		out, err := e.Prepare(in)
		if err != nil {
			t.Fatalf("%s at wages %d: %v", status, wages, err)
		}
		f := out.GetStates()[0].GetOk().GetForms().GetForm_511()
		return f.GetTaxableIncome().GetMinor(), f.GetTax().GetMinor()
	}
	offset := int64(-1)
	return func(t *testing.T, status string, income int64) int64 {
		t.Helper()
		return solve(t, status, income, &offset, prepare)
	}
}

// solve finds wages that put taxable income on income dollars and returns
// the tax there. offset is the wages above taxable income the last probe
// needed, carried from probe to probe; -1 starts it from a return of $50,000.
// It steps by the miss, and falls back to bisection when stepping does not
// land, as when a deduction is held back at zero income or changes in steps.
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

// The tax rounded once to whole dollars, as the form enters it.
func scheduleTax(b tablefile.Bracket, income int64) int64 {
	return b.TaxDollars(income) * 100
}

// The worksheets have one open bracket each, so its edges are only $100,000
// and a dollar above. These incomes spread the check over the bracket.
var spread = []int64{100_002, 125_000, 250_000, 1_000_000, 10_000_000}

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

	ws, err := tablefile.ReadSchedule(filepath.Join(tables, "worksheets.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ws.CheckPDF(pdfs); err != nil {
		t.Fatal(err)
	}
	tablefile.CheckSchedule(t, "worksheets.tsv", ws, 100_000, c, scheduleTax, newFigure)
	t.Run("worksheets.tsv spread", func(t *testing.T) {
		for _, status := range ws.Statuses() {
			t.Run(status, func(t *testing.T) {
				t.Parallel()
				figure := newFigure(t)
				for _, income := range spread {
					b, _ := ws.For(status, income)
					c.Check(t, "worksheets.tsv", b.Over, income, status, scheduleTax(b, income), figure(t, status, income))
				}
			})
		}
	})

	c.Finish(t)
}
