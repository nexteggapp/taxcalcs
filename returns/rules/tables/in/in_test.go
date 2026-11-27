// Package in holds Indiana's state adjusted gross income tax to the 3% rate
// the 2025 Form IT-40 prints for line 8. Indiana prints no tax table.
//
// The rules' figure is Form IT-40 line 8, from Prepare on a full-year
// resident's return of wages alone: the wages are the target Indiana adjusted
// gross income plus the exemptions, and the test checks line 7 lands on the
// target before reading line 8. The expected figure comes only from the TSV
// file under sources/tables/in/2025.
package in

import (
	"path/filepath"
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	"github.com/nexteggapp/taxcalcs/returns/rules/tables/tablefile"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
)

const (
	root   = "../../../.."
	tables = root + "/sources/tables/in/2025"
	pdfs   = root + "/sources/pdfs"
)

// The 2025 Indiana exemptions (Form IT-40 line 6, Schedule 3): $1,000 for
// the filer, and as much again for a spouse on a joint return.
var exemption = map[string]int64{
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
		in := tablefile.WageInputs(status, income+exemption[status])
		in.States = []*returnsv1.StateInputs{{
			State:  "IN",
			Inputs: &returnsv1.StateInputs_In{In: &returnsv1.IndianaInputs{}},
		}}
		out, err := e.Prepare(in)
		if err != nil {
			t.Fatalf("%s at %d: %v", status, income, err)
		}
		f := out.GetStates()[0].GetIn().GetForms().GetFormIt40()
		if got := f.GetIndianaAdjustedGrossIncome().GetMinor(); got != income*100 {
			t.Fatalf("%s: Indiana adjusted gross income is %d cents, want %d dollars", status, got, income)
		}
		return f.GetStateAdjustedGrossIncomeTax().GetMinor()
	}
}

// Line 8 is line 7 times 3%, rounded to the nearest whole dollar (booklet
// page 5, Rounding Required).
// The product is rounded once, not first to the cent.
func rateTax(b tablefile.Bracket, income int64) int64 {
	return b.TaxDollars(income) * 100
}

// A flat rate has one edge, at zero, so the test also probes incomes whose
// tax lands just below, on and just above 50 cents, and a few large ones.
// At $50 and $50,050 the tax ends in exactly 50 cents.
var moreProbes = []int64{16, 17, 49, 50, 51, 50_050, 100_000, 1_000_000}

func TestTaxTables(t *testing.T) {
	c, err := tablefile.NewChecker(filepath.Join(tables, "known_gaps.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	rate, err := tablefile.ReadSchedule(filepath.Join(tables, "rate.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if err := rate.CheckPDF(pdfs); err != nil {
		t.Fatal(err)
	}
	tablefile.CheckSchedule(t, "rate.tsv", rate, 0, c, rateTax, newFigure)

	t.Run("rounding", func(t *testing.T) {
		for _, status := range rate.Statuses() {
			t.Run(status, func(t *testing.T) {
				t.Parallel()
				figure := newFigure(t)
				for _, income := range moreProbes {
					b, ok := rate.For(status, income)
					if !ok {
						t.Fatalf("no %s bracket holds %d", status, income)
					}
					c.Check(t, "rate.tsv", b.Over, income, status, rateTax(b, income), figure(t, status, income))
				}
			})
		}
	})
	c.Finish(t)
}
