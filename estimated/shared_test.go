// The helpers several states share, on hand-worked cases.
package estimated_test

import (
	"math/big"
	"testing"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/money"
)

func TestLateStartCountsTheGroupsOnOrBeforeTheDay(t *testing.T) {
	groups := []estimated.Date{d(2026, 4, 1), d(2026, 6, 1), d(2026, 9, 1)}
	for _, tc := range []struct {
		met  estimated.Date
		want int
	}{
		{estimated.Date{}, 0},
		{d(2026, 3, 31), 0},
		{d(2026, 4, 1), 1},
		{d(2026, 5, 31), 1},
		{d(2026, 6, 1), 2},
		{d(2026, 9, 1), 3},
		{d(2026, 12, 31), 3},
	} {
		if got := estimated.LateStart(tc.met, groups); got != tc.want {
			t.Errorf("met %v: starts at %d, want %d", tc.met, got, tc.want)
		}
	}
}

func TestEqualSharesPutTheOddCentWhereRoundingDoes(t *testing.T) {
	// $1,000.00 over the last three of four: 333.33, 666.67, 1,000.00.
	got, err := estimated.EqualShares(100_000, 4, 1)
	if err != nil {
		t.Fatal(err)
	}
	want := []money.USCents{0, 33_333, 66_667, 100_000}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("running total %d is %v, want %v", i+1, got[i], want[i])
		}
	}
	// From the start it is four quarters, as CumulativeShares gives.
	all, err := estimated.EqualShares(100_001, 4, 0)
	if err != nil {
		t.Fatal(err)
	}
	quarters, err := estimated.CumulativeShares(100_001, []money.Rate{2500, 2500, 2500, 2500})
	if err != nil {
		t.Fatal(err)
	}
	for i := range quarters {
		if all[i] != quarters[i] {
			t.Errorf("quarter %d: equal shares %v, cumulative shares %v", i+1, all[i], quarters[i])
		}
	}
	for _, bad := range []struct {
		annual       money.USCents
		count, first int
	}{
		{100, 4, 4}, {100, 4, -1}, {100, 0, 0}, {-100, 4, 0},
	} {
		if _, err := estimated.EqualShares(bad.annual, bad.count, bad.first); err == nil {
			t.Errorf("%v over %d from %d was accepted", bad.annual, bad.count, bad.first)
		}
	}
}

func TestResidentAllYearNeedsOnePeriodForTheWholeYear(t *testing.T) {
	period := func(state string, from, to estimated.Date) estimated.ResidencePeriod {
		p, err := estimated.NewResidencePeriod(state).From(from).To(to).Build()
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	whole := period("ND", d(2026, 1, 1), d(2026, 12, 31))
	firstHalf := period("ND", d(2026, 1, 1), d(2026, 6, 30))
	secondHalf := period("ND", d(2026, 7, 1), d(2026, 12, 31))
	elsewhere := period("MN", d(2026, 1, 1), d(2026, 12, 31))
	for name, tc := range map[string]struct {
		periods []estimated.ResidencePeriod
		want    bool
	}{
		"all year":           {[]estimated.ResidencePeriod{whole}, true},
		"two halves":         {[]estimated.ResidencePeriod{firstHalf, secondHalf}, false},
		"another state":      {[]estimated.ResidencePeriod{elsewhere}, false},
		"none":               {nil, false},
		"another state, too": {[]estimated.ResidencePeriod{elsewhere, whole}, true},
	} {
		if got := estimated.ResidentAllYear(tc.periods, estimated.NorthDakota, 2026); got != tc.want {
			t.Errorf("%s: %v, want %v", name, got, tc.want)
		}
	}
	if estimated.ResidentAllYear([]estimated.ResidencePeriod{whole}, estimated.NorthDakota, 2027) {
		t.Error("a 2026 residence counts for 2027")
	}
}

// Two columns of $1,000 each. $400 is credited by April 15 and $900 on May 1,
// so April's $600 is paid on May 1. June's column measures every credit by
// June 15 against $2,000, so $700 is under and runs to the stop.
func TestColumnsMeasureEachColumnOnItsOwn(t *testing.T) {
	dues := []estimated.Date{d(2026, 4, 15), d(2026, 6, 15), d(2026, 9, 15)}
	credits := []estimated.Payment{
		payment(t, d(2026, 5, 1), 900),
		payment(t, d(2026, 4, 15), 400),
		payment(t, d(2026, 7, 1), 100),
	}
	cols, err := estimated.Columns(dues, []money.USCents{1_000, 2_000, 3_000}, credits, d(2027, 4, 15), d(2026, 8, 1))
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != 2 {
		t.Fatalf("%d columns before the stop, want 2", len(cols))
	}
	april, june := cols[0], cols[1]
	if april.Under() != 600 || april.CreditedBy() != 400 || april.End() != d(2026, 6, 15) {
		t.Errorf("April: under %v, credited %v, ends %v", april.Under(), april.CreditedBy(), april.End())
	}
	if p := april.Pieces(); len(p) != 1 || p[0].Amount() != 600 {
		t.Fatalf("April's pieces are %v", p)
	} else if on, ok := p[0].PaidOn(); !ok || on != d(2026, 5, 1) || april.Through() != d(2026, 5, 1) {
		t.Errorf("April was paid on %v and charged through %v, want May 1", on, april.Through())
	}
	if june.Under() != 700 || june.End() != d(2026, 8, 1) {
		t.Errorf("June: under %v, ends %v; want 700 to the stop", june.Under(), june.End())
	}
	p := june.Pieces()
	if len(p) != 2 || p[0].Amount() != 100 || p[1].Amount() != 600 {
		t.Fatalf("June's pieces are %v, want 100 paid July 1 and 600 unpaid", p)
	}
	if _, ok := p[1].PaidOn(); ok || june.Through() != d(2026, 8, 1) {
		t.Errorf("June's remainder shows as paid, or is charged through %v", june.Through())
	}
	if _, err := estimated.Columns(dues, []money.USCents{1, 2}, nil, d(2027, 4, 15), d(2027, 4, 15)); err == nil {
		t.Error("two running totals for three due dates were accepted")
	}
}

func TestRoundingDivRoundsOnceToTheUnit(t *testing.T) {
	for _, tc := range []struct {
		r    estimated.Rounding
		n, d int64
		want money.USCents
	}{
		// $1,234.50 ÷ 3 is $411.50, which is $412 to the dollar.
		{estimated.RoundToDollar, 123_450, 3, 41_200},
		// $1,234.49 ÷ 3 is $411.4966…, which is $411; rounding to the cent
		// first would give $411.50 and then $412.
		{estimated.RoundToDollar, 123_449, 3, 41_100},
		{estimated.RoundToCent, 123_449, 3, 41_150},
		{estimated.RoundToCent, 1, 2, 1},
	} {
		if got := tc.r.Div(big.NewInt(tc.n), big.NewInt(tc.d)); got != tc.want {
			t.Errorf("%d ÷ %d %v is %v, want %v", tc.n, tc.d, tc.r, got, tc.want)
		}
	}
	if got := estimated.RoundToDollar.Round(12_350); got != 12_400 {
		t.Errorf("$123.50 to the dollar is %v, want $124.00", got)
	}
	if got := estimated.RoundToDollar.Round(-12_350); got != -12_400 {
		t.Errorf("-$123.50 to the dollar is %v, want -$124.00", got)
	}
}

// Maryland's 10.8133% on $10,000 for the 365 days of 2026 is $1,081.33,
// where 10.81% would give $1,081.00.
func TestSimpleDailyChargesAFineRateAsPublished(t *testing.T) {
	s, err := estimated.NewRateSeries().FinePeriod(d(2026, 1, 1), d(2026, 12, 31), 108_133, "test").Build()
	if err != nil {
		t.Fatal(err)
	}
	parts, _, err := estimated.SimpleDaily(10_000*money.Dollar, d(2025, 12, 31), d(2026, 12, 31), s)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 1 || parts[0].Charge() != 108_133 {
		t.Fatalf("parts %v, want one charge of $1,081.33", parts)
	}
	if parts[0].FineRate() != 108_133 || parts[0].Rate() != 1081 {
		t.Errorf("the part's rate is %v (%v to the basis point)", parts[0].FineRate(), parts[0].Rate())
	}
	// A rate added in basis points charges what it did before.
	bp, err := estimated.NewRateSeries().Period(d(2026, 1, 1), d(2026, 12, 31), 1081, "test").Build()
	if err != nil {
		t.Fatal(err)
	}
	same, _, err := estimated.SimpleDaily(10_000*money.Dollar, d(2025, 12, 31), d(2026, 12, 31), bp)
	if err != nil {
		t.Fatal(err)
	}
	if same[0].Charge() != 108_100 || same[0].FineRate() != 108_100 {
		t.Errorf("10.81%% charges %v at %v", same[0].Charge(), same[0].FineRate())
	}
}
