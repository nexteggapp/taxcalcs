// Package me holds Maine's tax computation to the 2025 Maine Income Tax
// Table and Tax Rate Schedules printed in the Form 1040ME instructions.
//
// The rules' figure is Form 1040ME line 20, from Prepare on a full-year
// resident's return of wages alone: the wages are the target taxable income
// plus Maine's standard deduction and personal exemptions, and the test
// checks line 19 lands on the target before reading line 20. The expected
// figure comes only from the TSV files under sources/tables/me/2025.
package me

import (
	"path/filepath"
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	"github.com/nexteggapp/taxcalcs/returns/rules/tables/tablefile"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
)

const (
	root   = "../../../.."
	tables = root + "/sources/tables/me/2025"
	pdfs   = root + "/sources/pdfs"
)

// The 2025 Maine standard deduction (line 17) plus the $5,150 personal
// exemption (line 18) for each filer, from the Form 1040ME instructions. No
// probe reaches the incomes at which either phases out.
var deductions = map[string]int64{
	"single": 15_000 + 5_150,
	"mfj":    30_000 + 2*5_150,
	"mfs":    15_000 + 5_150,
	"hoh":    22_500 + 5_150,
	"qss":    30_000 + 5_150,
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
			State:  "ME",
			Inputs: &returnsv1.StateInputs_Me{Me: &returnsv1.MaineInputs{}},
		}}
		out, err := e.Prepare(in)
		if err != nil {
			t.Fatalf("%s at %d: %v", status, income, err)
		}
		f := out.GetStates()[0].GetMe().GetForms().GetForm_1040Me()
		if got := f.GetTaxableIncome().GetMinor(); got != income*100 {
			t.Fatalf("%s: taxable income is %d cents, want %d dollars", status, got, income)
		}
		return f.GetIncomeTax().GetMinor()
	}
}

// The schedules' tax: the base plus the rate on the excess, rounded to the
// cent, then to whole dollars for line 20.
func scheduleTax(b tablefile.Bracket, income int64) int64 {
	return tablefile.RoundDollars(b.TaxCents(income)) * 100
}

// Above the table the schedules have few edges, so the test also probes the
// table's top and a few incomes above it, all below the deduction phase-out.
var moreProbes = []int64{52_000, 52_001, 60_000, 75_000}

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
	if n := len(table.Rows); n != 521 {
		t.Errorf("tax_table.tsv has %d rows; the printed table has 521", n)
	}
	tablefile.CheckTable(t, "tax_table.tsv", table, c, newFigure)

	const name = "rate_schedules.tsv"
	s, err := tablefile.ReadSchedule(filepath.Join(tables, name))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CheckPDF(pdfs); err != nil {
		t.Fatal(err)
	}
	tablefile.CheckSchedule(t, name, s, 52_000, c, scheduleTax, newFigure)
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
