// Package wa holds Washington's capital gains excise tax to the 2025 rates
// in RCW 82.87.040 and the 2025 standard deduction the Department of Revenue
// publishes.
//
// The rules' figure is the return's total tax, from Prepare on a return
// whose only income is one long-term sale reported on a Form 1099-B. The
// test searches for the gain that puts taxable Washington capital gains on
// the target, and checks it lands there before reading the tax. The expected
// figure comes only from the TSV file under sources/tables/wa/2025.
// TestStandardDeduction holds the deduction itself to the published
// $278,000.
package wa

import (
	"path/filepath"
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	"github.com/nexteggapp/taxcalcs/returns/rules/tables/tablefile"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	form1099b "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_b"
)

const (
	root   = "../../../.."
	tables = root + "/sources/tables/wa/2025"
	pdfs   = root + "/sources/pdfs"
)

// The 2025 standard deduction, from the Department of Revenue's capital
// gains tax page (catalogued as wa-dor-capital-gains-2025, with no copy
// kept): "The standard deduction for 2025 is $278,000."
const standardDeduction = 278_000

// brokerGain is a broker statement with one long-term sale of the given
// gain, in whole dollars.
func brokerGain(gain int64) []*returnsv1.BrokerStatement {
	usd := func(d int64) *commonv1.Money { return &commonv1.Money{Currency: "USD", Minor: d * 100} }
	return []*returnsv1.BrokerStatement{{
		Id:     "broker",
		Person: "filer",
		Transactions: []*returnsv1.BrokerTransaction{{
			Id: "sale",
			Form: &form1099b.Form1099B{
				DescriptionOfProperty: "Index fund shares",
				DateAcquired:          &commonv1.Date{Year: 2015, Month: 3, Day: 2},
				DateSoldOrDisposed:    &commonv1.Date{Year: 2025, Month: 9, Day: 15},
				Proceeds:              usd(gain + 100_000),
				CostOrOtherBasis:      usd(100_000),
				HoldingPeriod:         form1099b.Form1099B_HOLDING_PERIOD_LONG_TERM,
				BasisReportedToIrs:    true,
			},
		}},
	}}
}

func newFigure(t *testing.T) tablefile.Figure {
	e, err := federal.LoadEngine()
	if err != nil {
		t.Fatal(err)
	}
	prepare := func(t *testing.T, status string, wages int64) (taxable, tax int64) {
		t.Helper()
		in := tablefile.WageInputs(status, wages)
		// The "wages" are a long-term gain instead: see the package comment.
		in.GetFederal().GetDocuments().W2S = nil
		in.GetFederal().GetDocuments().Brokers = brokerGain(wages)
		in.States = []*returnsv1.StateInputs{{
			State:  "WA",
			Inputs: &returnsv1.StateInputs_Wa{Wa: &returnsv1.WashingtonInputs{}},
		}}
		out, err := e.Prepare(in)
		if err != nil {
			t.Fatalf("%s at wages %d: %v", status, wages, err)
		}
		f := out.GetStates()[0].GetWa().GetForms().GetCapitalGains()
		return f.GetTaxableWashingtonCapitalGains().GetMinor(), f.GetTotalTax().GetMinor()
	}
	offset := int64(-1)
	return func(t *testing.T, status string, income int64) int64 {
		t.Helper()
		return solve(t, status, income, &offset, prepare)
	}
}

// solve finds wages that put taxable income on income dollars and returns
// the tax there. offset is the wages above taxable income the last
// probe needed, carried from probe to probe; -1 starts it from a return of
// $50,000. It steps by the miss, and falls back to bisection when stepping
// does not land, as when a deduction is held back at zero income or changes
// in steps.
func solve(t *testing.T, status string, income int64, offset *int64,
	prepare func(t *testing.T, status string, wages int64) (taxable, tax int64)) int64 {
	t.Helper()
	if *offset < 0 {
		taxable, _ := prepare(t, status, 50_000)
		*offset = 50_000 - taxable/100
	}
	want := income * 100
	lo, hi := int64(-1), int64(-1) // wages known to fall short of and pass the target
	w := income + *offset
	for range 6 {
		taxable, tax := prepare(t, status, w)
		if taxable == want {
			*offset = w - income
			return tax
		}
		if taxable < want {
			lo = w
		} else {
			hi = w
		}
		if taxable == 0 {
			break
		}
		w += income - taxable/100
	}
	if lo < 0 {
		lo = 0
	}
	if hi < 0 {
		for hi = lo + 1024; ; hi *= 2 {
			if taxable, _ := prepare(t, status, hi); taxable >= want {
				break
			}
		}
	}
	for lo+1 < hi {
		mid := (lo + hi) / 2
		taxable, tax := prepare(t, status, mid)
		switch {
		case taxable == want:
			*offset = mid - income
			return tax
		case taxable < want:
			lo = mid
		default:
			hi = mid
		}
	}
	if taxable, tax := prepare(t, status, hi); taxable == want {
		*offset = hi - income
		return tax
	}
	t.Fatalf("%s: no whole-dollar wages put taxable income on %d dollars", status, income)
	return 0
}

// The tax to the cent: the form does not round it to whole dollars.
func scheduleTax(b tablefile.Bracket, income int64) int64 {
	return b.TaxCents(income)
}

// checkSpread checks a schedule at more incomes than its edges, for a top
// bracket whose only edge is its start.
func checkSpread(t *testing.T, name string, s *tablefile.Schedule, incomes []int64, c *tablefile.Checker) {
	t.Run(name+" spread", func(t *testing.T) {
		for _, status := range s.Statuses() {
			t.Run(status, func(t *testing.T) {
				t.Parallel()
				figure := newFigure(t)
				for _, income := range incomes {
					b, ok := s.For(status, income)
					if !ok {
						t.Fatalf("%s: no %s bracket holds %d", name, status, income)
					}
					c.Check(t, name, b.Over, income, status, scheduleTax(b, income), figure(t, status, income))
				}
			})
		}
	})
}

func TestTaxTables(t *testing.T) {
	c, err := tablefile.NewChecker(filepath.Join(tables, "known_gaps.tsv"))
	if err != nil {
		t.Fatal(err)
	}

	s, err := tablefile.ReadSchedule(filepath.Join(tables, "capital_gains_tax.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CheckPDF(pdfs); err != nil {
		t.Fatal(err)
	}
	tablefile.CheckSchedule(t, "capital_gains_tax.tsv", s, 0, c, scheduleTax, newFigure)
	checkSpread(t, "capital_gains_tax.tsv", s, []int64{100_000, 2_500_000, 10_000_000}, c)

	c.Finish(t)
}

// The deduction is the published $278,000: a gain of that much leaves no
// taxable gain, and a dollar more leaves one dollar.
func TestStandardDeduction(t *testing.T) {
	e, err := federal.LoadEngine()
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"single", "mfj", "mfs"} {
		for _, gain := range []int64{standardDeduction - 1, standardDeduction, standardDeduction + 1} {
			in := tablefile.WageInputs(status, 0)
			in.GetFederal().GetDocuments().W2S = nil
			in.GetFederal().GetDocuments().Brokers = brokerGain(gain)
			in.States = []*returnsv1.StateInputs{{
				State:  "WA",
				Inputs: &returnsv1.StateInputs_Wa{Wa: &returnsv1.WashingtonInputs{}},
			}}
			out, err := e.Prepare(in)
			if err != nil {
				t.Fatalf("%s at gain %d: %v", status, gain, err)
			}
			f := out.GetStates()[0].GetWa().GetForms().GetCapitalGains()
			want := max(gain-standardDeduction, 0) * 100
			if got := f.GetTaxableWashingtonCapitalGains().GetMinor(); got != want {
				t.Errorf("%s at gain %d: taxable gain is %d cents, want %d", status, gain, got, want)
			}
		}
	}
}
