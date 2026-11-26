// Package mt holds Montana's tax computation to the 2025 Ordinary Income Tax
// Rates printed in the Form 2 instructions.
//
// The rules' figure is the ordinary income tax on worksheet line 12, which
// with no capital gains is all of Form 2 line 8, from Prepare on a full-year
// resident's return of wages alone: the wages are the target taxable income
// plus the federal standard deduction, and the test checks line 7 lands on
// the target before reading the tax. The expected figure comes only from the
// TSV file under sources/tables/mt/2025.
package mt

import (
	"path/filepath"
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	"github.com/nexteggapp/taxcalcs/returns/rules/tables/tablefile"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
)

const (
	root   = "../../../.."
	tables = root + "/sources/tables/mt/2025"
	pdfs   = root + "/sources/pdfs"
)

// Form 2 line 2 is the federal standard deduction for a filer under 65
// (Form 1040 instructions, line 12e).
var standardDeduction = map[string]int64{
	"single": 15_750,
	"mfj":    31_500,
	"mfs":    15_750,
	"hoh":    23_625,
	"qss":    31_500,
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
			State:  "MT",
			Inputs: &returnsv1.StateInputs_Mt{Mt: &returnsv1.MontanaInputs{}},
		}}
		out, err := e.Prepare(in)
		if err != nil {
			t.Fatalf("%s at %d: %v", status, income, err)
		}
		f := out.GetStates()[0].GetMt().GetForms().GetForm_2()
		if got := f.GetMontanaTaxableIncome().GetMinor(); got != income*100 {
			t.Fatalf("%s: taxable income is %d cents, want %d dollars", status, got, income)
		}
		if got := f.GetTaxBeforeCredits().GetMinor(); got != f.GetOrdinaryIncomeTax().GetMinor() {
			t.Fatalf("%s at %d: line 8 is %d cents, but the ordinary income tax is %d", status, income, got, f.GetOrdinaryIncomeTax().GetMinor())
		}
		return f.GetOrdinaryIncomeTax().GetMinor()
	}
}

// Income times the rate, less the "Less" amount, in whole dollars as Form 2
// is filled in.
func rateTax(b tablefile.Bracket, income int64) int64 {
	return b.TaxDollars(income) * 100
}

func TestOrdinaryIncomeTaxRates(t *testing.T) {
	c, err := tablefile.NewChecker(filepath.Join(tables, "known_gaps.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	const name = "ordinary_income_tax_rates.tsv"
	s, err := tablefile.ReadSchedule(filepath.Join(tables, name))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CheckPDF(pdfs); err != nil {
		t.Fatal(err)
	}
	tablefile.CheckSchedule(t, name, s, 0, c, rateTax, newFigure)
	c.Finish(t)
}
