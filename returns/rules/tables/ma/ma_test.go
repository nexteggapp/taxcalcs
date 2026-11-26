// Package ma holds Massachusetts's tax on 5.0% income to the 5.0% rate and
// the 4% surtax printed in the 2025 Form 1 instructions.
//
// The rules' figures are Form 1 line 22 and line 28b, from Prepare on a
// full-year resident's return of wages alone: the wages are the target line
// 21 plus the personal exemption, and the test checks line 21 lands on the
// target before reading either line. The expected figures come only from the
// TSV files under sources/tables/ma/2025.
//
// Not covered: the 2025 tax table for line 21 of $24,000 or less, the
// optional 5.85% rate, the 8.5% and 12% income of line 23, and the
// long-term gains of Schedule D.
package ma

import (
	"path/filepath"
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	"github.com/nexteggapp/taxcalcs/returns/rules/tables/tablefile"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	formma1 "github.com/nexteggapp/taxprotos/go/us/state/ma/v1/form_1"
)

const (
	root   = "../../../.."
	tables = root + "/sources/tables/ma/2025"
	pdfs   = root + "/sources/pdfs"
)

// The 2025 personal exemption, Form 1 line 2a.
var exemption = map[string]int64{
	"single": 4_400,
	"mfj":    8_800,
	"mfs":    4_400,
	"hoh":    6_800,
}

// newFigure makes a Figure that reads one line of Form 1.
func newFigure(line func(*formma1.FormMA1) int64) tablefile.NewFigure {
	return func(t *testing.T) tablefile.Figure {
		e, err := federal.LoadEngine()
		if err != nil {
			t.Fatal(err)
		}
		return func(t *testing.T, status string, income int64) int64 {
			t.Helper()
			in := tablefile.WageInputs(status, income+exemption[status])
			in.States = []*returnsv1.StateInputs{{
				State:  "MA",
				Inputs: &returnsv1.StateInputs_Ma{Ma: &returnsv1.MassachusettsInputs{}},
			}}
			out, err := e.Prepare(in)
			if err != nil {
				t.Fatalf("%s at %d: %v", status, income, err)
			}
			f := out.GetStates()[0].GetMa().GetForms().GetForm_1()
			if got := f.GetTaxableIncome().GetMinor(); got != income*100 {
				t.Fatalf("%s: line 21 is %d cents, want %d dollars", status, got, income)
			}
			return line(f)
		}
	}
}

// Each line is the rate times the income, entered in whole dollars.
func rateTax(b tablefile.Bracket, income int64) int64 {
	return tablefile.RoundDollars(b.TaxCents(income)) * 100
}

func check(t *testing.T, c *tablefile.Checker, name string, from int64, more []int64, line func(*formma1.FormMA1) int64) {
	s, err := tablefile.ReadSchedule(filepath.Join(tables, name))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CheckPDF(pdfs); err != nil {
		t.Fatal(err)
	}
	figures := newFigure(line)
	tablefile.CheckSchedule(t, name, s, from, c, rateTax, figures)
	// A flat rate has few edges, so the test also probes incomes whose tax
	// lands either side of 50 cents, and a few larger ones.
	t.Run(name+"/more", func(t *testing.T) {
		for _, status := range s.Statuses() {
			t.Run(status, func(t *testing.T) {
				t.Parallel()
				figure := figures(t)
				for _, income := range more {
					b, _ := s.For(status, income)
					c.Check(t, name, b.Over, income, status, rateTax(b, income), figure(t, status, income))
				}
			})
		}
	})
}

func TestTaxRates(t *testing.T) {
	c, err := tablefile.NewChecker(filepath.Join(tables, "known_gaps.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	// Line 22 above the table, which covers $24,000 or less.
	check(t, c, "tax_rate.tsv", 24_001, []int64{24_009, 24_010, 50_000, 1_083_150, 2_000_000},
		func(f *formma1.FormMA1) int64 { return f.GetTaxOnTaxableIncome().GetMinor() })
	check(t, c, "surtax.tsv", 24_001, []int64{1_083_162, 1_083_163, 2_000_000},
		func(f *formma1.FormMA1) int64 { return f.GetSurtax().GetMinor() })
	c.Finish(t)
}
