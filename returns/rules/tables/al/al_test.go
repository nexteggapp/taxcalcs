// Package al holds Alabama's tax computation to the 2025 Tax Table printed
// in the Form 40 booklet and the worksheet beside it for taxable income over
// $100,000.
//
// The rules' figure is the rule for Form 40 line 17, al-tax, called through
// Engine.Evaluator.Call with the 2025 figures and the target taxable income.
// Prepare is not used because line 16 depends on the federal income tax
// deduction, which moves with the wages, so a return of wages alone cannot
// land on every target. The expected figure comes only from the TSV files
// under sources/tables/al/2025.
package al

import (
	"path/filepath"
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	"github.com/nexteggapp/taxcalcs/returns/rules/tables/tablefile"
	"github.com/nexteggapp/taxcalcs/taxlisp/eval"
)

const (
	root   = "../../../.."
	tables = root + "/sources/tables/al/2025"
	pdfs   = root + "/sources/pdfs"
)

func newFigure(t *testing.T) tablefile.Figure {
	e, err := federal.LoadEngine()
	if err != nil {
		t.Fatal(err)
	}
	year, err := e.Evaluator.Call("al-year", eval.IntVal{V: 2025})
	if err != nil {
		t.Fatal(err)
	}
	return func(t *testing.T, status string, income int64) int64 {
		t.Helper()
		v, err := e.Evaluator.Call("al-tax", year, eval.BoolVal{V: status == "mfj"}, eval.NewMoneyCents(income*100))
		if err != nil {
			t.Fatalf("%s at %d: %v", status, income, err)
		}
		m, ok := v.(eval.MoneyVal)
		if !ok {
			t.Fatalf("al-tax returned %T", v)
		}
		return m.Cents()
	}
}

// The worksheet's tax: (taxable income − $100,000) × 5% plus the base, which
// Form 40 enters rounded to whole dollars.
func worksheetTax(b tablefile.Bracket, income int64) int64 {
	return tablefile.RoundDollars(b.TaxCents(income)) * 100
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
	if n := len(table.Rows); n != 1001 {
		t.Errorf("tax_table.tsv has %d rows; the printed table has 1,001", n)
	}
	tablefile.CheckTable(t, "tax_table.tsv", table, c, newFigure)

	worksheet, err := tablefile.ReadSchedule(filepath.Join(tables, "over_100000_worksheet.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if err := worksheet.CheckPDF(pdfs); err != nil {
		t.Fatal(err)
	}
	// The table covers taxable income less than $100,000 and the worksheet
	// income over it; neither prints $100,000 itself, so it is not probed.
	tablefile.CheckSchedule(t, "over_100000_worksheet.tsv", worksheet, 100_001, c, worksheetTax, newFigure)

	c.Finish(t)
}
