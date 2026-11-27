// Package wv holds West Virginia's tax computation to the 2025 West
// Virginia Tax Table and Tax Rate Schedules printed in the Form IT-140
// booklet.
//
// The rules' figure is Form IT-140 line 8, from Prepare on a return whose
// only income is taxable interest. Interest is used rather than wages
// because the low-income earned income exclusion on line 5 takes all of a
// low earner's wages, so wages cannot reach a small taxable income. The test
// searches for the interest that puts line 7, taxable income, on the target,
// and checks it lands there before reading line 8. The expected figure
// comes only from the TSV files under sources/tables/wv/2025.
//
// The table starts at $25, so taxable income from $1 to $24 is not checked
// for the statuses that use it.
package wv

import (
	"path/filepath"
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	"github.com/nexteggapp/taxcalcs/returns/rules/tables/tablefile"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	form1099int "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_int"
)

const (
	root   = "../../../.."
	tables = root + "/sources/tables/wv/2025"
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
		// Interest in place of the wages: see the package comment.
		docs := in.GetFederal().GetDocuments()
		docs.W2S = nil
		docs.Interest = []*returnsv1.Form1099INTDocument{{
			Id:     "int",
			Person: "filer",
			Form: &form1099int.Form1099INT{
				InterestIncome: &commonv1.Money{Currency: "USD", Minor: wages * 100},
			},
		}}
		in.States = []*returnsv1.StateInputs{{
			State:  "WV",
			Inputs: &returnsv1.StateInputs_Wv{Wv: &returnsv1.WestVirginiaInputs{}},
		}}
		out, err := e.Prepare(in)
		if err != nil {
			t.Fatalf("%s at wages %d: %v", status, wages, err)
		}
		f := out.GetStates()[0].GetWv().GetForms().GetFormIt140()
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

// The tax rounded once to whole dollars, as the form enters it.
func scheduleTax(b tablefile.Bracket, income int64) int64 {
	return b.TaxDollars(income) * 100
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
	if n := len(table.Rows); n != 1702 {
		t.Errorf("tax_table.tsv has %d rows; the printed table has 1,702", n)
	}
	tablefile.CheckTable(t, "tax_table.tsv", table, c, newFigure)

	rs1, err := tablefile.ReadSchedule(filepath.Join(tables, "rate_schedule_i.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if err := rs1.CheckPDF(pdfs); err != nil {
		t.Fatal(err)
	}
	// Rate Schedule I is for taxable income over $100,000; below that the
	// table applies. The table's last row ends below $100,000, so the schedule
	// is checked from $100,000 itself.
	tablefile.CheckSchedule(t, "rate_schedule_i.tsv", rs1, 100_000, c, scheduleTax, newFigure)
	checkSpread(t, "rate_schedule_i.tsv", rs1, []int64{250_000, 1_000_000, 10_000_000}, c)

	rs2, err := tablefile.ReadSchedule(filepath.Join(tables, "rate_schedule_ii.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if err := rs2.CheckPDF(pdfs); err != nil {
		t.Fatal(err)
	}
	tablefile.CheckSchedule(t, "rate_schedule_ii.tsv", rs2, 0, c, scheduleTax, newFigure)
	checkSpread(t, "rate_schedule_ii.tsv", rs2, []int64{100_000, 1_000_000, 10_000_000}, c)

	c.Finish(t)
}
