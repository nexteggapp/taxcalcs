// Package mo holds Missouri's tax computation to the 2025 Tax Chart printed
// in the Form MO-1040 instructions.
//
// The rules' figure is Form MO-1040 line 30Y, from Prepare on a full-year
// resident's return with no federal income, so no federal tax deduction:
// the target taxable income plus the standard deduction, and for head of
// household and qualifying surviving spouse the additional exemption, is
// entered as a Missouri addition. A spouse on a joint return has no income,
// so line 29Y holds all of it. The test checks line 29Y lands on the target
// before reading line 30Y. The expected figure comes only from the TSV file
// under sources/tables/mo/2025.
package mo

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
	tables = root + "/sources/tables/mo/2025"
	pdfs   = root + "/sources/pdfs"
)

// Line 14 is the federal standard deduction for a filer under 65 (Form 1040
// instructions, line 12e); line 15 adds $1,400 for head of household and
// qualifying surviving spouse (Form MO-1040 instructions, line 15).
var deductions = map[string]int64{
	"single": 15_750,
	"mfj":    31_500,
	"mfs":    15_750,
	"hoh":    23_625 + 1_400,
	"qss":    31_500 + 1_400,
}

func newFigure(t *testing.T) tablefile.Figure {
	e, err := federal.LoadEngine()
	if err != nil {
		t.Fatal(err)
	}
	return func(t *testing.T, status string, income int64) int64 {
		t.Helper()
		in := tablefile.WageInputs(status, 0)
		in.States = []*returnsv1.StateInputs{{
			State: "MO",
			Inputs: &returnsv1.StateInputs_Mo{Mo: &returnsv1.MissouriInputs{
				Additions: &commonv1.Money{Currency: "USD", Minor: (income + deductions[status]) * 100},
			}},
		}}
		out, err := e.Prepare(in)
		if err != nil {
			t.Fatalf("%s at %d: %v", status, income, err)
		}
		f := out.GetStates()[0].GetMo().GetForms().GetFormMo1040()
		if got := f.GetTaxableIncomeYourself().GetMinor(); got != income*100 {
			t.Fatalf("%s: line 29Y is %d cents, want %d dollars", status, got, income)
		}
		return f.GetTaxYourself().GetMinor()
	}
}

// Section B: the excess times the rate, plus the base tax, rounded to the
// nearest whole dollar.
func chartTax(b tablefile.Bracket, income int64) int64 {
	return tablefile.RoundDollars(b.TaxCents(income)) * 100
}

func TestTaxChart(t *testing.T) {
	c, err := tablefile.NewChecker(filepath.Join(tables, "known_gaps.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := tablefile.ReadSchedule(filepath.Join(tables, "tax_chart.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CheckPDF(pdfs); err != nil {
		t.Fatal(err)
	}
	tablefile.CheckSchedule(t, "tax_chart.tsv", s, 0, c, chartTax, newFigure)
	c.Finish(t)
}
