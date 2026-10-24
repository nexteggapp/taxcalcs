package ustaxcalc_test

import (
	"os"
	"strings"
	"testing"

	"github.com/nexteggapp/taxcalcs/ustaxcalc"
)

// The § 6654 required annual payment, checked against the IRS's own worked
// example.
//
// Publication 505 (2026) works one taxpayer end to end and states the answer:
//
//	Your total tax on the 2025 return was $42,581, and the expected tax for
//	2026 is $71,253. Your 2025 AGI was $180,000. […] the required annual
//	payment is $46,839, the smaller of the two.
//
// That is an oracle in the strict sense — inputs and answer both published by
// the body that wrote the rule — and it is the only one this calculator has.
// The Tax Table covers OrdinaryIncomeTax exhaustively and says nothing about
// this.
//
// It is one case, so it proves the arithmetic on one path: the 110% prior-year
// test winning because AGI passed the § 6654(d)(1)(C) step. TestSafeHarbour in
// calc_test.go covers the branches the IRS did not choose to illustrate.
func TestSafeHarbourMatchesPublication505(t *testing.T) {
	const extract = "testdata/p505-2026-safe-harbour-example.txt"

	// The figures below are transcribed from the publication, so the test first
	// checks they are actually in it. Otherwise this asserts that the library
	// agrees with numbers somebody typed, which is what every hand-written test
	// already does.
	raw, err := os.ReadFile(extract)
	if err != nil {
		t.Fatalf("read %s: %v", extract, err)
	}
	printed := string(raw)

	for _, figure := range []string{"42,581", "71,253", "180,000", "64,128", "46,839"} {
		if !strings.Contains(printed, figure) {
			t.Fatalf("%s is not in %s: the extract and this test have diverged", figure, extract)
		}
	}

	const (
		priorYearTax  = 42_581 * ustaxcalc.Dollar // the 2025 return
		currentYear   = 71_253 * ustaxcalc.Dollar // expected for 2026
		priorYearAGI  = 180_000 * ustaxcalc.Dollar
		wantRequired  = 46_839 * ustaxcalc.Dollar
		wantPriorTest = 46_839 * ustaxcalc.Dollar // 110% of $42,581
		wantCurrent   = 64_128 * ustaxcalc.Dollar // 90% of $71,253
	)

	got, err := ustaxcalc.RequiredAnnualPayment(
		2026, ustaxcalc.Single, currentYear, priorYearTax, priorYearAGI, true)
	if err != nil {
		t.Fatal(err)
	}

	// The publication rounds to whole dollars. Anything under a dollar is that
	// rounding rather than a disagreement.
	near := func(a, b ustaxcalc.USCents) bool {
		d := a - b
		if d < 0 {
			d = -d
		}
		return d < 100
	}

	if !near(got.Value().Required, wantRequired) {
		t.Errorf("required annual payment is %v, Publication 505 says %v",
			got.Value().Required, wantRequired)
	}
	if !near(got.Value().CurrentYearTest, wantCurrent) {
		t.Errorf("current year test is %v, Publication 505 says %v",
			got.Value().CurrentYearTest, wantCurrent)
	}
	if !near(got.Value().PriorYearTest, wantPriorTest) {
		t.Errorf("prior year test is %v, Publication 505 says %v",
			got.Value().PriorYearTest, wantPriorTest)
	}

	// The example turns on this: $180,000 of AGI passes the § 6654(d)(1)(C)
	// step, so the prior year test is 110% and not 100%. At 100% it would be
	// $42,581 and still the smaller of the two, so the required payment alone
	// does not prove the step was applied.
	if got.Value().PriorYearRate != ustaxcalc.Percent(110) {
		t.Errorf("prior year rate is %v, want 110%% because AGI passed the step",
			got.Value().PriorYearRate)
	}
}
