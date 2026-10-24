// These tests moved here with the types. They were in ustaxcalc/calc_test.go
// while the money types were declared there, and they never tested anything
// federal: rounding a percentage and formatting a dollar figure are the same
// operations whichever jurisdiction asked for them.
package money_test

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/money"
)

func TestRateAndAmountFormatting(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{money.Dollars(1_234).String(), "$1234.00"},
		{money.USCents(-50).String(), "-$0.50"},
		{money.USCents(19_297_925).String(), "$192979.25"},
		{money.Percent(22).String(), "22%"},
		{money.Rate(90).String(), "0.9%"},
		{money.Rate(9235).String(), "92.35%"},

		// Oregon's top rate and Washington's additional rate, which are why
		// this is basis points rather than whole percent.
		{money.Rate(990).String(), "9.9%"},
		{money.Rate(290).String(), "2.9%"},
	} {
		if tc.in != tc.want {
			t.Errorf("formatted %q, want %q", tc.in, tc.want)
		}
	}
}

// TestRateOfRoundsHalvesAwayFromZero pins the rounding, because a caller sums
// these and has to be able to reproduce a total from the parts on a screen.
func TestRateOfRoundsHalvesAwayFromZero(t *testing.T) {
	for _, tc := range []struct {
		rate money.Rate
		of   money.USCents
		want money.USCents
	}{
		{money.Percent(50), money.USCents(1), money.USCents(1)},   // half a cent up
		{money.Percent(50), money.USCents(-1), money.USCents(-1)}, // and away from zero
		{money.Percent(10), money.USCents(4), money.USCents(0)},
		{money.Percent(10), money.USCents(5), money.USCents(1)},
	} {
		if got := tc.rate.Of(tc.of); got != tc.want {
			t.Errorf("%s of %d cents is %d, want %d", tc.rate, int64(tc.of), int64(got), int64(tc.want))
		}
	}
}

func TestFineRateHoldsWhatBasisPointsCannot(t *testing.T) {
	for _, c := range []struct {
		r       money.FineRate
		printed string
		nearest money.Rate
		exact   bool
	}{
		{108_133, "10.8133%", 1081, false},
		{99_950, "9.995%", 1000, false},
		{70_000, "7%", 700, true},
		{9_011, "0.9011%", 90, false},
		{money.Percent(22).Fine(), "22%", 2200, true},
		{-108_133, "-10.8133%", -1081, false},
	} {
		if got := c.r.String(); got != c.printed {
			t.Errorf("%d prints as %q, want %q", int64(c.r), got, c.printed)
		}
		if got := c.r.Nearest(); got != c.nearest {
			t.Errorf("%v to the nearest basis point is %d, want %d", c.r, got, c.nearest)
		}
		if got := c.r.IsBasisPoints(); got != c.exact {
			t.Errorf("%v IsBasisPoints is %v, want %v", c.r, got, c.exact)
		}
	}
}

func TestFineRateOfRoundsHalvesAwayFromZero(t *testing.T) {
	for _, c := range []struct {
		r    money.FineRate
		a    money.USCents
		want money.USCents
	}{
		// 10.8133% of $1,000 is $108.133.
		{108_133, 1_000 * money.Dollar, 10_813},
		// 10.8133% of $10,000 is exactly $1,081.33.
		{108_133, 10_000 * money.Dollar, 108_133},
		// 50% of 1 cent is half a cent, rounded away from zero.
		{500_000, 1, 1},
		{500_000, -1, -1},
		{499_999, 1, 0},
	} {
		if got := c.r.Of(c.a); got != c.want {
			t.Errorf("%v of %v is %v, want %v", c.r, c.a, got, c.want)
		}
	}
	// A fine rate that is a whole number of basis points gives what the
	// basis points give.
	for _, a := range []money.USCents{1, 49, 50, 12_345, -12_345, 9_876_543_21} {
		r := money.Rate(1_081)
		if r.Fine().Of(a) != r.Of(a) {
			t.Errorf("%v of %v: fine %v, basis points %v", r, a, r.Fine().Of(a), r.Of(a))
		}
	}
}

func TestWholeIsDollarsWithSeparators(t *testing.T) {
	for _, c := range []struct {
		a    money.USCents
		want string
	}{
		{0, "$0"},
		{49, "$0"},
		{50, "$1"},
		{1_000 * money.Dollar, "$1,000"},
		{574_249, "$5,742"},
		{1_234_567_89, "$1,234,568"},
		{-1_200 * money.Dollar, "−$1,200"},
		{-150, "−$2"},
	} {
		if got := c.a.Whole(); got != c.want {
			t.Errorf("%d cents: %q, want %q", int64(c.a), got, c.want)
		}
	}
}
