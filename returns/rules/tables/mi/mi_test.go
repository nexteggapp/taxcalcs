// Package mi holds Michigan's tax computation to the flat rate printed on
// the 2025 Form MI-1040.
//
// The rules' figure is Form MI-1040 line 17, from Prepare on a full-year
// resident's return of wages alone: the wages are the target taxable income
// plus Michigan's exemption allowance, and the test checks line 16 lands on
// the target before reading line 17. The expected figure comes only from the
// TSV file under sources/tables/mi/2025.
package mi

import (
	"path/filepath"
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	"github.com/nexteggapp/taxcalcs/returns/rules/tables/tablefile"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
)

const (
	root   = "../../../.."
	tables = root + "/sources/tables/mi/2025"
	pdfs   = root + "/sources/pdfs"
)

// The 2025 Michigan exemption allowance for Form MI-1040 line 9a: $5,800
// for the filer, and for the spouse on a joint return. The test's filers
// have no dependents or special exemptions.
var exemptions = map[string]int64{
	"single": 5_800,
	"mfj":    11_600,
	"mfs":    5_800,
}

func newFigure(t *testing.T) tablefile.Figure {
	e, err := federal.LoadEngine()
	if err != nil {
		t.Fatal(err)
	}
	return func(t *testing.T, status string, income int64) int64 {
		t.Helper()
		in := tablefile.WageInputs(status, income+exemptions[status])
		in.States = []*returnsv1.StateInputs{{
			State:  "MI",
			Inputs: &returnsv1.StateInputs_Mi{Mi: &returnsv1.MichiganInputs{}},
		}}
		out, err := e.Prepare(in)
		if err != nil {
			t.Fatalf("%s at %d: %v", status, income, err)
		}
		f := out.GetStates()[0].GetMi().GetForms().GetFormMi1040()
		if got := f.GetTaxableIncome().GetMinor(); got != income*100 {
			t.Fatalf("%s: taxable income is %d cents, want %d dollars", status, got, income)
		}
		return f.GetTax().GetMinor()
	}
}

// Line 17 is the rate times line 16, rounded once to the nearest whole
// dollar, not first to the cent.
func rateTax(b tablefile.Bracket, income int64) int64 {
	return b.TaxDollars(income) * 100
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
