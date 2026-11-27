// Package ct holds Connecticut's tax computation to Table B, Initial Tax
// Calculation, printed in the 2025 Form CT-1040 instructions. The booklet
// prints no tax table; it points to tables on the DRS website.
//
// The rules' figure is line 4 of the rules' Tax Calculation Schedule, which
// takes Connecticut taxable income (line 3) through Table B. Line 4 is not a
// field of the return, and Tables A, C, D and E, read at Connecticut AGI, sit
// on either side of it, so Prepare cannot isolate it. The test instead calls,
// through Engine.Evaluator.Call, the same functions line 4 composes:
// round-dollar of state-bracket-tax on ct-schedule for the filing status.
// The expected figure comes only from the TSV file under
// sources/tables/ct/2025.
package ct

import (
	"path/filepath"
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	"github.com/nexteggapp/taxcalcs/returns/rules/tables/tablefile"
	"github.com/nexteggapp/taxcalcs/taxlisp/eval"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

const (
	root   = "../../../.."
	tables = root + "/sources/tables/ct/2025"
	pdfs   = root + "/sources/pdfs"
)

var federalStatus = map[string]protoreflect.Name{
	"single": "FILING_STATUS_SINGLE",
	"mfj":    "FILING_STATUS_MARRIED_FILING_JOINTLY",
	"mfs":    "FILING_STATUS_MARRIED_FILING_SEPARATELY",
	"hoh":    "FILING_STATUS_HEAD_OF_HOUSEHOLD",
	"qss":    "FILING_STATUS_QUALIFYING_SURVIVING_SPOUSE",
}

func statusValue(t *testing.T, status string) eval.ProtoEnumVal {
	t.Helper()
	et, err := protoregistry.GlobalTypes.FindEnumByName("nexteggapp.taxprotos.us.federal.v1.FilingStatus")
	if err != nil {
		t.Fatal(err)
	}
	d := et.Descriptor()
	v := d.Values().ByName(federalStatus[status])
	if v == nil {
		t.Fatalf("no federal filing status for %q", status)
	}
	return eval.ProtoEnumVal{Desc: d, Number: v.Number(), Name: string(v.Name())}
}

func newFigure(t *testing.T) tablefile.Figure {
	e, err := federal.LoadEngine()
	if err != nil {
		t.Fatal(err)
	}
	return func(t *testing.T, status string, income int64) int64 {
		t.Helper()
		call := func(name string, args ...eval.Value) eval.Value {
			t.Helper()
			v, err := e.Evaluator.Call(name, args...)
			if err != nil {
				t.Fatalf("%s at %d: %s: %v", status, income, name, err)
			}
			return v
		}
		rows := call("ct-schedule", statusValue(t, status))
		tax := call("state-bracket-tax", rows, eval.NewMoneyCents(income*100), eval.BoolVal{V: false})
		m, ok := call("round-dollar", tax).(eval.MoneyVal)
		if !ok {
			t.Fatalf("round-dollar did not return money")
		}
		return m.Cents()
	}
}

// Base plus rate times the excess, entered on line 4 in whole dollars, as the
// printed examples round it.
func tableBTax(b tablefile.Bracket, income int64) int64 {
	return tablefile.RoundDollars(b.TaxCents(income)) * 100
}

func TestTaxTables(t *testing.T) {
	c, err := tablefile.NewChecker(filepath.Join(tables, "known_gaps.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	tableB, err := tablefile.ReadSchedule(filepath.Join(tables, "table_b.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if err := tableB.CheckPDF(pdfs); err != nil {
		t.Fatal(err)
	}
	tablefile.CheckSchedule(t, "table_b.tsv", tableB, 0, c, tableBTax, newFigure)
	c.Finish(t)
}
