package estimated_test

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/money"
)

func factor(t *testing.T, printed string) estimated.Factor {
	t.Helper()
	f, err := estimated.ParseFactor(printed)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestParseFactor(t *testing.T) {
	for _, ok := range []string{"0.000192", "0.0192", "1", ".5"} {
		if f, err := estimated.ParseFactor(ok); err != nil || f.String() != ok {
			t.Errorf("ParseFactor(%q) = %v, %v", ok, f, err)
		}
	}
	for _, bad := range []string{"", ".", "0", "0.000", "-0.1", "0.00a", "0.1.2", "1e-4"} {
		if _, err := estimated.ParseFactor(bad); err == nil {
			t.Errorf("ParseFactor(%q) was accepted", bad)
		}
	}
}

// The printed factor is used as printed, not the rate it came from: $10,000
// for 56 days at 0.000192 is $107.52, where 7% ÷ 365 would give $107.40.
func TestDailyFactorUsesThePrintedFactor(t *testing.T) {
	f := factor(t, "0.000192")
	for _, tc := range []struct {
		r    estimated.Rounding
		want money.USCents
	}{
		{estimated.RoundToCent, 10_752},
		{estimated.RoundToDollar, 108 * money.Dollar},
	} {
		s, err := estimated.NewFactorSeries(tc.r).Period(d(2026, 1, 1), d(2026, 12, 31), f, money.Percent(7), "REV-1611").Build()
		if err != nil {
			t.Fatal(err)
		}
		parts, unrated, err := estimated.DailyFactor(10_000*money.Dollar, d(2026, 4, 15), d(2026, 6, 10), s)
		if err != nil || !unrated.IsZero() {
			t.Fatalf("%v, unrated %v", err, unrated)
		}
		if len(parts) != 1 || parts[0].Days() != 56 || parts[0].Charge() != tc.want || parts[0].Rate() != money.Percent(7) || parts[0].Source() != "REV-1611" {
			t.Errorf("rounded %v: %+v, want one part of 56 days charging %v", tc.r, parts, tc.want)
		}
	}
}

// A span crossing into a period with another factor is charged in two
// parts, each rounded on its own, and days past the last factor are reported.
func TestDailyFactorSplitsAtEachPeriodAndStopsWhereTheyEnd(t *testing.T) {
	s, err := estimated.NewFactorSeries(estimated.RoundToDollar).
		Period(d(2024, 1, 1), d(2024, 12, 31), factor(t, "0.000219"), money.Percent(8), "REV-1611 2024").
		Period(d(2025, 1, 1), d(2025, 12, 31), factor(t, "0.000192"), money.Percent(7), "REV-1611 2025").
		Build()
	if err != nil {
		t.Fatal(err)
	}
	parts, unrated, err := estimated.DailyFactor(1_000*money.Dollar, d(2024, 12, 1), d(2026, 1, 10), s)
	if err != nil {
		t.Fatal(err)
	}
	if unrated != d(2026, 1, 1) {
		t.Errorf("unrated from %v, want 2026-01-01", unrated)
	}
	// 30 days at 0.000219 is $6.57, so $7; 365 at 0.000192 is $70.08, so $70.
	if len(parts) != 2 || parts[0].Days() != 30 || parts[0].Charge() != 7*money.Dollar ||
		parts[1].Days() != 365 || parts[1].Charge() != 70*money.Dollar {
		t.Errorf("parts %+v, want 30 days for $7 and 365 for $70", parts)
	}
}

// Maryland's Form 502UP prints one factor per span between due dates; for
// 2025 the first is 0.0192, from April 15 to June 15.
func TestSpanFactor(t *testing.T) {
	p, err := estimated.SpanFactor(1_000*money.Dollar, d(2025, 4, 15), d(2025, 6, 15), factor(t, "0.0192"), 1148, estimated.RoundToCent, "Form 502UP (2025)")
	if err != nil {
		t.Fatal(err)
	}
	if p.Charge() != 1_920 || p.Days() != 61 {
		t.Errorf("charge %v over %d days, want $19.20 over 61", p.Charge(), p.Days())
	}
	if _, err := estimated.SpanFactor(100, d(2025, 4, 15), d(2025, 6, 15), estimated.Factor{}, 0, estimated.RoundToCent, "x"); err == nil {
		t.Error("a zero factor was accepted")
	}
	if _, err := estimated.SpanFactor(100, d(2025, 4, 15), d(2025, 6, 15), factor(t, "0.0192"), 0, estimated.RoundingUnspecified, "x"); err == nil {
		t.Error("no rounding was accepted")
	}
}

func TestFactorSeriesBuildRefusesGapsAndUnsourcedFactors(t *testing.T) {
	f := factor(t, "0.000192")
	for name, b := range map[string]*estimated.FactorSeriesBuilder{
		"no rounding": estimated.NewFactorSeries(estimated.RoundingUnspecified).Period(d(2026, 1, 1), d(2026, 12, 31), f, 700, "x"),
		"empty":       estimated.NewFactorSeries(estimated.RoundToCent),
		"no source":   estimated.NewFactorSeries(estimated.RoundToCent).Period(d(2026, 1, 1), d(2026, 12, 31), f, 700, ""),
		"no factor":   estimated.NewFactorSeries(estimated.RoundToCent).Period(d(2026, 1, 1), d(2026, 12, 31), estimated.Factor{}, 700, "x"),
		"backwards":   estimated.NewFactorSeries(estimated.RoundToCent).Period(d(2026, 12, 31), d(2026, 1, 1), f, 700, "x"),
		"gap": estimated.NewFactorSeries(estimated.RoundToCent).
			Period(d(2025, 1, 1), d(2025, 12, 30), f, 700, "x").Period(d(2026, 1, 1), d(2026, 12, 31), f, 700, "x"),
	} {
		if _, err := b.Build(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
