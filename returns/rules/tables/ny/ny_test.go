// Package ny holds New York State's tax, Form IT-201 line 39, to the 2025
// New York State tax rate schedule and the tax computation worksheets for New
// York AGI of more than $107,650, printed in the IT-201 instructions.
//
// The rules' figure is line 39, from Prepare on a return of wages alone. The
// wages are the target taxable income plus the standard deduction, and the
// test checks that line 33, New York AGI, is the wages and line 38 the
// target before reading line 39. So each probe's AGI is its taxable income
// plus the standard deduction, and above $107,650 of AGI the expected figure
// is the worksheet's.
//
// The tax table and the rate schedule come from the TSV files under
// sources/tables/ny/2025. The table is published only as a web page, which
// is vendored as retrieved and checked by its SHA-256 like the PDFs.
//
// Not covered: the New York City tax table on the same page, and the New
// York City tax in general.
// The worksheets' thresholds, recapture base amounts and incremental benefit
// amounts are transcribed below from pages 34 to 39, since they are not a
// table or a rate schedule; the calculator that applies them is written
// here, not taken from the rules.
package ny

import (
	"math/big"
	"path/filepath"
	"sort"
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	"github.com/nexteggapp/taxcalcs/returns/rules/tables/tablefile"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
)

const (
	root   = "../../../.."
	tables = root + "/sources/tables/ny/2025"
	pdfs   = root + "/sources/pdfs"
)

// The New York State standard deduction table (IT-201-I page 12, line 34),
// for a single filer no one can claim as a dependent.
var standardDeduction = map[string]int64{
	"single": 8_000,
	"mfj":    16_050,
	"mfs":    8_000,
	"hoh":    11_200,
	"qss":    16_050,
}

// band is one of the later tax computation worksheets: it applies when
// taxable income is over threshold, and phases in the incremental benefit
// over the $50,000 of New York AGI above threshold.
type band struct {
	threshold, recaptureBase, incrementalBenefit int64
}

// worksheets is one filing status's set of tax computation worksheets.
type worksheets struct {
	// The first worksheet: taxable income at the flat rate, for taxable
	// income of firstCeiling or less, phased in over AGI from $107,650 and
	// taken whole from $157,650.
	firstCeiling int64
	firstRate    *big.Rat
	bands        []band
}

// Worksheets 1 to 5 (pages 34 and 35), 7 to 10 (pages 36 and 37) and 12 to
// 15 (page 38). Worksheets 6, 11 and 16, for AGI over $25,000,000, take
// 10.9% of taxable income for every status.
var (
	joint = worksheets{161_550, big.NewRat(55, 1000), []band{
		{161_550, 333, 807},
		{323_200, 1_140, 2_747},
		{2_155_350, 3_887, 60_350},
		{5_000_000, 64_237, 32_500},
	}}
	single = worksheets{215_400, big.NewRat(6, 100), []band{
		{215_400, 568, 1_831},
		{1_077_550, 2_399, 30_172},
		{5_000_000, 32_571, 32_500},
	}}
	head = worksheets{269_300, big.NewRat(6, 100), []band{
		{269_300, 787, 2_289},
		{1_616_450, 3_076, 45_261},
		{5_000_000, 48_337, 32_500},
	}}
	byStatus = map[string]worksheets{"single": single, "mfs": single, "mfj": joint, "qss": joint, "hoh": head}
)

const (
	recaptureFloor = 107_650    // worksheets apply above this AGI
	firstFull      = 157_650    // the first worksheet's flat rate applies whole from here
	flatAGI        = 25_000_000 // above this AGI, 10.9% of taxable income
	phaseWidth     = 50_000
)

// phase is the excess of AGI over a threshold, at most $50,000, divided by
// $50,000 and rounded to the fourth decimal place, as each worksheet says.
func phase(agi, threshold int64) *big.Rat {
	excess := min(max(agi-threshold, 0), phaseWidth)
	// Round excess/50,000 to four places, half up: excess/5 ten-thousandths.
	tenThousandths := (excess*2 + 5) / 10
	return big.NewRat(tenThousandths, 10_000)
}

// line39 is the tax the instructions give for New York AGI agi and taxable
// income ti, in cents, rounded to whole dollars.
func line39(s *tablefile.Schedule, status string, agi, ti int64) int64 {
	b, ok := s.For(status, ti)
	if !ok {
		panic("no bracket")
	}
	scheduled := new(big.Rat).Mul(b.Rate, new(big.Rat).SetInt64(ti-b.OfExcessOver))
	scheduled.Add(scheduled, b.BaseTax)
	ws := byStatus[status]
	var tax *big.Rat
	switch {
	case agi <= recaptureFloor:
		tax = scheduled
	case agi > flatAGI:
		tax = new(big.Rat).Mul(big.NewRat(109, 1000), new(big.Rat).SetInt64(ti))
	case ti <= ws.firstCeiling:
		flat := new(big.Rat).Mul(ws.firstRate, new(big.Rat).SetInt64(ti))
		if agi >= firstFull {
			tax = flat
		} else {
			diff := new(big.Rat).Sub(flat, scheduled)
			tax = new(big.Rat).Add(scheduled, diff.Mul(diff, phase(agi, recaptureFloor)))
		}
	default:
		var bd band
		for _, x := range ws.bands {
			if ti > x.threshold {
				bd = x
			}
		}
		benefit := new(big.Rat).Mul(new(big.Rat).SetInt64(bd.incrementalBenefit), phase(agi, bd.threshold))
		tax = new(big.Rat).Add(scheduled, new(big.Rat).SetInt64(bd.recaptureBase))
		tax.Add(tax, benefit)
	}
	half := new(big.Rat).Add(tax, big.NewRat(1, 2))
	return new(big.Int).Quo(half.Num(), half.Denom()).Int64() * 100
}

func newFigure(t *testing.T) tablefile.Figure {
	e, err := federal.LoadEngine()
	if err != nil {
		t.Fatal(err)
	}
	return func(t *testing.T, status string, income int64) int64 {
		t.Helper()
		wages := income + standardDeduction[status]
		in := tablefile.WageInputs(status, wages)
		in.States = []*returnsv1.StateInputs{{
			State:  "NY",
			Inputs: &returnsv1.StateInputs_Ny{Ny: &returnsv1.NewYorkInputs{}},
		}}
		out, err := e.Prepare(in)
		if err != nil {
			t.Fatalf("%s at %d: %v", status, income, err)
		}
		f := out.GetStates()[0].GetNy().GetForms().GetFormIt201()
		if got := f.GetNewYorkAdjustedGrossIncome().GetMinor(); got != wages*100 {
			t.Fatalf("%s: line 33 is %d cents, want %d dollars", status, got, wages)
		}
		if got := f.GetTaxableIncome().GetMinor(); got != income*100 {
			t.Fatalf("%s: line 38 is %d cents, want %d dollars", status, got, income)
		}
		return f.GetNewYorkStateTax().GetMinor()
	}
}

// probes returns the taxable incomes to check for a status, from $65,000,
// where the table ends: each edge of the rate schedule a dollar either side
// and the middle of each bracket; and where AGI crosses $107,650, $157,650,
// each worksheet's threshold, halfway and the end of its $50,000 phase-in,
// and $25,000,000, a dollar either side of each.
func probes(s *tablefile.Schedule, status string) []int64 {
	set := map[int64]bool{}
	for _, n := range s.Probes(status, 65_000) {
		set[n] = true
	}
	std := standardDeduction[status]
	agiEdges := []int64{recaptureFloor, recaptureFloor + phaseWidth/2, firstFull, flatAGI}
	ws := byStatus[status]
	for _, bd := range ws.bands {
		agiEdges = append(agiEdges, bd.threshold, bd.threshold+phaseWidth/2, bd.threshold+phaseWidth)
		set[bd.threshold+1] = true // the first taxable income the worksheet takes
	}
	for _, a := range agiEdges {
		for _, n := range []int64{a - std - 1, a - std, a - std + 1} {
			set[n] = true
		}
	}
	var out []int64
	for n := range set {
		if n >= 65_000 {
			out = append(out, n)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func TestTaxTables(t *testing.T) {
	c, err := tablefile.NewChecker(filepath.Join(tables, "known_gaps.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := tablefile.ReadSchedule(filepath.Join(tables, "rate_schedule.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CheckPDF(pdfs); err != nil {
		t.Fatal(err)
	}
	table, err := tablefile.ReadTable(filepath.Join(tables, "tax_table.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if err := table.CheckPDF(pdfs); err != nil {
		t.Fatal(err)
	}
	if n := len(table.Rows); n != 1302 {
		t.Errorf("tax_table.tsv has %d rows; the published table has 1,302", n)
	}
	tablefile.CheckTable(t, "tax_table.tsv", table, c, newFigure)
	t.Run("rate_schedule.tsv", func(t *testing.T) {
		for _, status := range s.Statuses() {
			t.Run(status, func(t *testing.T) {
				t.Parallel()
				figure := newFigure(t)
				for _, income := range probes(s, status) {
					b, _ := s.For(status, income)
					want := line39(s, status, income+standardDeduction[status], income)
					c.Check(t, "rate_schedule.tsv", b.Over, income, status, want, figure(t, status, income))
				}
			})
		}
	})
	c.Finish(t)
}

// The phase-in rounds to four places as the worksheets say.
func TestPhase(t *testing.T) {
	for _, tc := range []struct {
		agi, threshold int64
		want           *big.Rat
	}{
		{107_650, 107_650, big.NewRat(0, 1)},
		{107_653, 107_650, big.NewRat(1, 10_000)}, // 3/50,000 = 0.00006
		{107_652, 107_650, big.NewRat(0, 1)},      // 2/50,000 = 0.00004
		{132_650, 107_650, big.NewRat(1, 2)},
		{500_000, 107_650, big.NewRat(1, 1)},
	} {
		if got := phase(tc.agi, tc.threshold); got.Cmp(tc.want) != 0 {
			t.Errorf("phase(%d, %d) = %s, want %s", tc.agi, tc.threshold, got, tc.want)
		}
	}
}
