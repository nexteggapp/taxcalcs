// Package ms holds Mississippi's tax computation to the Schedule of Tax
// Computation printed in the 2025 Form 80-100 instructions.
//
// The rules' figure is Form 80-105 line 17, from Prepare on a full-year
// resident's return of wages alone: the wages are the target taxable income
// plus Mississippi's standard deduction and filing status exemption, and the
// test checks line 16A lands on the target before reading line 17. The
// expected figure comes only from the TSV file under sources/tables/ms/2025.
//
// A married couple's spouse has no income, so everything is in column A. A
// combined return that splits income between the spouses' columns, each with
// its own first $10,000, is not covered.
package ms

import (
	"path/filepath"
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	"github.com/nexteggapp/taxcalcs/returns/rules/tables/tablefile"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
)

const (
	root   = "../../../.."
	tables = root + "/sources/tables/ms/2025"
	pdfs   = root + "/sources/pdfs"
)

// Lines 14A and 15A for the 2025 Form 80-105, from the instructions' filing
// status table: the standard deduction plus the filing status exemption. A
// married person filing separately takes half the joint deduction, $2,300,
// and half the joint exemption, $6,000.
var deductions = map[string]int64{
	"single": 2_300 + 6_000,
	"mfj":    4_600 + 12_000,
	"mfs":    2_300 + 6_000,
	"hoh":    3_400 + 8_000,
}

func newFigure(t *testing.T) tablefile.Figure {
	e, err := federal.LoadEngine()
	if err != nil {
		t.Fatal(err)
	}
	return func(t *testing.T, status string, income int64) int64 {
		t.Helper()
		in := tablefile.WageInputs(status, income+deductions[status])
		in.States = []*returnsv1.StateInputs{{
			State:  "MS",
			Inputs: &returnsv1.StateInputs_Ms{Ms: &returnsv1.MississippiInputs{}},
		}}
		out, err := e.Prepare(in)
		if err != nil {
			t.Fatalf("%s at %d: %v", status, income, err)
		}
		f := out.GetStates()[0].GetMs().GetForms().GetForm_80105()
		if got := f.GetTaxpayerTaxableIncome().GetMinor(); got != income*100 {
			t.Fatalf("%s: taxable income is %d cents, want %d dollars", status, got, income)
		}
		return f.GetIncomeTax().GetMinor()
	}
}

// The schedule's tax, rounded to the nearest whole dollar. The product is
// rounded once, to the dollar, not first to the cent.
func scheduleTax(b tablefile.Bracket, income int64) int64 {
	return b.TaxDollars(income) * 100
}

// Incomes whose 4.4% lands either side of 50 cents, and a few larger ones,
// beyond the schedule's edge at $10,000. At $10,034 the tax is $1.496, which
// rounds to $1, but to $2 if first rounded to the cent.
var moreProbes = []int64{10_011, 10_012, 10_034, 10_035, 60_011, 60_012, 100_000, 1_000_000}

func TestScheduleOfTaxComputation(t *testing.T) {
	c, err := tablefile.NewChecker(filepath.Join(tables, "known_gaps.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	const name = "schedule_of_tax_computation.tsv"
	s, err := tablefile.ReadSchedule(filepath.Join(tables, name))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CheckPDF(pdfs); err != nil {
		t.Fatal(err)
	}
	tablefile.CheckSchedule(t, name, s, 0, c, scheduleTax, newFigure)

	t.Run("more", func(t *testing.T) {
		for _, status := range s.Statuses() {
			t.Run(status, func(t *testing.T) {
				t.Parallel()
				figure := newFigure(t)
				for _, income := range moreProbes {
					b, _ := s.For(status, income)
					c.Check(t, name, b.Over, income, status, scheduleTax(b, income), figure(t, status, income))
				}
			})
		}
	})

	c.Finish(t)
}
