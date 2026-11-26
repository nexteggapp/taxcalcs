// Package nc holds North Carolina's tax computation to the flat rate printed on
// the 2025 Form D-400.
//
// The rules' figure is Form D-400 line 15, from Prepare on a full-year
// resident's return of wages alone: the wages are the target taxable income
// plus North Carolina's standard deduction, and the test checks line 14 lands on
// the target before reading line 15. The expected figure comes only from the
// TSV file under sources/tables/nc/2025.
package nc

import (
	"math/big"
	"path/filepath"
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	"github.com/nexteggapp/taxcalcs/returns/rules/tables/tablefile"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
)

const (
	root   = "../../../.."
	tables = root + "/sources/tables/nc/2025"
	pdfs   = root + "/sources/pdfs"
)

// The 2025 North Carolina standard deduction for Form D-400 line 11, from the
// instructions' standard deduction chart.
var standardDeduction = map[string]int64{
	"single": 12_750,
	"mfj":    25_500,
	"mfs":    12_750,
	"hoh":    19_125,
	"qss":    25_500,
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
			State:  "NC",
			Inputs: &returnsv1.StateInputs_Nc{Nc: &returnsv1.NorthCarolinaInputs{}},
		}}
		out, err := e.Prepare(in)
		if err != nil {
			t.Fatalf("%s at %d: %v", status, income, err)
		}
		f := out.GetStates()[0].GetNc().GetForms().GetFormD400()
		if got := f.GetTaxableIncome().GetMinor(); got != income*100 {
			t.Fatalf("%s: taxable income is %d cents, want %d dollars", status, got, income)
		}
		return f.GetIncomeTax().GetMinor()
	}
}

// Line 15 is the rate times line 14, rounded once to the nearest whole
// dollar, not first to the cent.
func rateTax(b tablefile.Bracket, income int64) int64 {
	tax := new(big.Rat).Mul(b.Rate, big.NewRat(income-b.OfExcessOver, 1))
	tax.Add(tax, b.BaseTax)
	tax.Add(tax, big.NewRat(1, 2))
	return new(big.Int).Quo(tax.Num(), tax.Denom()).Int64() * 100
}

// A flat rate has one edge, at zero, so the test also probes incomes whose
// tax lands either side of 50 cents, and a few larger ones. At $247 the tax
// is $10.4975, which rounds to $10, but to $11 if first rounded to the cent.
var moreProbes = []int64{11, 12, 35, 36, 247, 50_011, 50_012, 100_000, 1_000_000}

func TestTaxRate(t *testing.T) {
	c, err := tablefile.NewChecker(filepath.Join(tables, "known_gaps.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	const name = "tax_rate.tsv"
	s, err := tablefile.ReadSchedule(filepath.Join(tables, name))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CheckPDF(pdfs); err != nil {
		t.Fatal(err)
	}
	tablefile.CheckSchedule(t, name, s, 0, c, rateTax, newFigure)

	t.Run("more", func(t *testing.T) {
		for _, status := range s.Statuses() {
			t.Run(status, func(t *testing.T) {
				t.Parallel()
				figure := newFigure(t)
				for _, income := range moreProbes {
					b, _ := s.For(status, income)
					c.Check(t, name, b.Over, income, status, rateTax(b, income), figure(t, status, income))
				}
			})
		}
	})

	c.Finish(t)
}
