// Every Washington figure in this library, checked against what the Department
// of Revenue publishes.
//
// The oracle is weaker than California's or Oregon's, and it is worth being
// plain about why. Those two states print a table of their own answers — a rate
// schedule with cumulative base amounts, a tax table with a figure for every
// band — so a test can check arithmetic against arithmetic. Washington prints
// no such table, because the tax is two multiplications and there is nothing to
// tabulate. What can be checked is that the rates, the threshold and the
// deductions this library carries are the ones the Department states, and that
// the arithmetic behaves at the edges. That is what these tests do.
package washingtontaxcalc_test

import (
	"os"
	"strings"
	"testing"

	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/washingtontaxcalc"
	"github.com/nexteggapp/taxcalcs/working"
)

// TestFiguresMatchTheDepartmentsPage checks the deduction figures against the
// page that publishes them.
func TestFiguresMatchTheDepartmentsPage(t *testing.T) {
	text := readFile(t, "testdata/dor-wa-2025-deductions.txt")

	// The page carries two years, so the test has to pin which sentence it is
	// reading rather than just looking for a number.
	if !strings.Contains(text, "The standard deduction for 2025 is $278,000") {
		t.Fatal("the Department's page no longer states the 2025 standard deduction")
	}
	if !strings.Contains(text, "cannot exceed $111,000 for 2025") {
		t.Error("the Department's page no longer states the 2025 charitable deduction cap")
	}
	if !strings.Contains(text, "Charitable donations in excess of $278,000 for 2025") {
		t.Error("the Department's page no longer states the 2025 charitable donation threshold")
	}
	if !strings.Contains(text, "7% tax on the sale or exchange of long-term capital assets") {
		t.Error("the Department's page no longer states the 7% base rate")
	}

	table, err := washingtontaxcalc.TableFor(2025)
	if err != nil {
		t.Fatalf("TableFor: %v", err)
	}
	for _, tc := range []struct {
		name string
		got  money.USCents
		want money.USCents
	}{
		{"standard deduction", table.StandardDeduction, 278_000 * money.Dollar},
		{"charitable threshold", table.CharitableDeductionThreshold, 278_000 * money.Dollar},
		{"charitable cap", table.CharitableDeductionCap, 111_000 * money.Dollar},
		{"surcharge threshold", table.SurchargeThreshold, 1_000_000 * money.Dollar},
	} {
		if tc.got != tc.want {
			t.Errorf("%s is %v, the Department says %v", tc.name, tc.got, tc.want)
		}
	}
	if want := money.Percent(7); table.BaseRate != want {
		t.Errorf("base rate is %v, want %v", table.BaseRate, want)
	}
}

// TestSurchargeMatchesTheLegislation checks the 2.9% rate and its threshold
// against the Department's summary of the bill that created them.
func TestSurchargeMatchesTheLegislation(t *testing.T) {
	text := readFile(t, "testdata/dor-wa-2025-rate-surcharge.txt")

	for _, want := range []string{
		"ESSB 5813",
		"Chapter 421,",
		"additional 2.9% tax",
		"exceeding $1 million",
		"in addition to the",
		"tax year 2025",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the legislation summary no longer says %q", want)
		}
	}

	table, err := washingtontaxcalc.TableFor(2025)
	if err != nil {
		t.Fatalf("TableFor: %v", err)
	}
	if want := money.Rate(290); table.SurchargeRate != want {
		t.Errorf("surcharge rate is %v, want %v", table.SurchargeRate, want)
	}

	// "In addition to" is the load-bearing phrase. The top of a large gain
	// meets 9.9%, not 2.9%.
	if want := money.Rate(990); table.BaseRate+table.SurchargeRate != want {
		t.Errorf("the combined top rate is %v, want %v", table.BaseRate+table.SurchargeRate, want)
	}
}

// TestCitationsMatchTheStatute checks that each subsection this library cites
// says what it is cited for.
//
// This is the test that catches a plausible-looking wrong citation, and it was
// written because there were three. The standard deduction was cited to
// RCW 82.87.020(1), which is the definitions section; the base rate to
// 82.87.040(1), which is really 82.87.040(1)(a); and the surcharge to
// 82.87.040(2), which says the levy is necessary for the support of state
// government. All three produced a `Rule.Cite()` that a reader would have
// followed to the wrong place.
//
// A citation is a claim about a document, and it is the kind of claim nothing
// else here would check.
func TestCitationsMatchTheStatute(t *testing.T) {
	statute := readFile(t, "testdata/rcw-82-87-extract.txt")

	for _, tc := range []struct {
		cite  string
		says  string
		about string
	}{
		{"82.87.060", "A standard deduction of $250,000 per individual", "the standard deduction"},
		{"82.87.040", "seven percent multiplied by an individual's Washington capital gains", "the base rate"},
		{"82.87.040", "2.90 percent multiplied by the portion of an individual's Washington capital gains exceeding $1,000,000", "the surcharge"},
		{"82.87.080", "minimum qualifying charitable donation amount equals $250,000", "the charitable donation floor"},
		{"82.87.080", "may not exceed $100,000 for the taxable year", "the charitable donation cap"},
		{"82.87.150", "the department must adjust the applicable amounts", "the annual inflation adjustment"},
		{"82.87.100", "domiciled in this state", "the allocation of gain on intangible property"},
	} {
		if !strings.Contains(statute, "RCW "+tc.cite) {
			t.Errorf("the extract no longer carries RCW %s", tc.cite)
			continue
		}
		if !strings.Contains(statute, tc.says) {
			t.Errorf("RCW %s, cited for %s, no longer says %q", tc.cite, tc.about, tc.says)
		}
	}

	// The load-bearing interpretive point. "Washington capital gains" is a
	// defined term, and the definition puts the deductions inside it. That is
	// what makes the surcharge threshold a figure measured after the standard
	// deduction rather than before it, which is how this library computes it
	// and how the Department describes it.
	if !strings.Contains(statute, `"Washington capital gains" means an individual's adjusted capital gain, as modified in RCW  82.87.060`) {
		t.Error("RCW 82.87.020 no longer defines Washington capital gains as modified by the deductions section")
	}
}

// TestGainBelowTheDeductionIsNotTaxed checks the ordinary case, which is that
// nothing is owed and no return is due.
func TestGainBelowTheDeductionIsNotTaxed(t *testing.T) {
	for _, gain := range []money.USCents{
		0,
		-50_000 * money.Dollar, // a net loss
		277_999 * money.Dollar,
		278_000 * money.Dollar, // exactly the deduction
	} {
		got, err := washingtontaxcalc.ExciseTax(2025, gain, washingtontaxcalc.Deductions{}, washingtontaxcalc.Facts{})
		if err != nil {
			t.Fatalf("ExciseTax(%v): %v", gain, err)
		}
		if got.Value().Total != 0 {
			t.Errorf("a gain of %v is taxed %v, want nothing", gain, got.Value().Total)
		}
		if got.Value().Taxable != 0 {
			t.Errorf("a gain of %v leaves %v taxable, want nothing", gain, got.Value().Taxable)
		}
	}
}

// TestBaseRateOnly checks a gain that clears the deduction but not the
// surcharge threshold.
func TestBaseRateOnly(t *testing.T) {
	// $500,000 of gain, less the $278,000 deduction, leaves $222,000 at 7%.
	got, err := washingtontaxcalc.ExciseTax(2025, 500_000*money.Dollar, washingtontaxcalc.Deductions{}, washingtontaxcalc.Facts{})
	if err != nil {
		t.Fatalf("ExciseTax: %v", err)
	}
	if want := 222_000 * money.Dollar; got.Value().Taxable != want {
		t.Errorf("taxable gain is %v, want %v", got.Value().Taxable, money.USCents(want))
	}
	if want := 15_540 * money.Dollar; got.Value().Total != want {
		t.Errorf("tax is %v, want %v", got.Value().Total, money.USCents(want))
	}
	if got.Value().SurchargeTax != 0 {
		t.Errorf("surcharge is %v below the threshold, want nothing", got.Value().SurchargeTax)
	}
}

// TestSurchargeAppliesAboveOneMillionOfTaxableGain pins the thing most likely
// to be got wrong: the threshold is measured against gain after the standard
// deduction, and the surcharge is on top of the base rate.
func TestSurchargeAppliesAboveOneMillionOfTaxableGain(t *testing.T) {
	// $1,278,000 of gain, less $278,000, is exactly $1,000,000 taxable. At the
	// threshold and not above it, so no surcharge at all.
	at, err := washingtontaxcalc.ExciseTax(2025, 1_278_000*money.Dollar, washingtontaxcalc.Deductions{}, washingtontaxcalc.Facts{})
	if err != nil {
		t.Fatalf("ExciseTax: %v", err)
	}
	if want := 1_000_000 * money.Dollar; at.Value().Taxable != want {
		t.Fatalf("taxable gain is %v, want %v", at.Value().Taxable, money.USCents(want))
	}
	if at.Value().SurchargeTax != 0 {
		t.Errorf("surcharge at exactly $1,000,000 of taxable gain is %v, want nothing", at.Value().SurchargeTax)
	}
	if want := 70_000 * money.Dollar; at.Value().Total != want {
		t.Errorf("tax at exactly $1,000,000 of taxable gain is %v, want %v", at.Value().Total, money.USCents(want))
	}

	// A further $500,000 of gain is taxed at 7% and then at 2.9% again.
	over, err := washingtontaxcalc.ExciseTax(2025, 1_778_000*money.Dollar, washingtontaxcalc.Deductions{}, washingtontaxcalc.Facts{})
	if err != nil {
		t.Fatalf("ExciseTax: %v", err)
	}
	if want := 1_500_000 * money.Dollar; over.Value().Taxable != want {
		t.Fatalf("taxable gain is %v, want %v", over.Value().Taxable, money.USCents(want))
	}
	if want := 500_000 * money.Dollar; over.Value().AtSurchargeRate != want {
		t.Errorf("%v met the surcharge, want %v", over.Value().AtSurchargeRate, money.USCents(want))
	}
	// 7% of $1,500,000 is $105,000; 2.9% of $500,000 is $14,500.
	if want := 105_000 * money.Dollar; over.Value().BaseTax != want {
		t.Errorf("base tax is %v, want %v", over.Value().BaseTax, money.USCents(want))
	}
	if want := 14_500 * money.Dollar; over.Value().SurchargeTax != want {
		t.Errorf("surcharge is %v, want %v", over.Value().SurchargeTax, money.USCents(want))
	}
	if want := 119_500 * money.Dollar; over.Value().Total != want {
		t.Errorf("total tax is %v, want %v", over.Value().Total, money.USCents(want))
	}
}

// TestCharitableDeductionIsBothFlooredAndCapped checks the rule that only
// donations above the threshold count, and only up to the cap.
func TestCharitableDeductionIsBothFlooredAndCapped(t *testing.T) {
	for _, tc := range []struct {
		name    string
		donated money.USCents
		want    money.USCents
	}{
		{"below the threshold", 200_000 * money.Dollar, 0},
		{"at the threshold", 278_000 * money.Dollar, 0},
		{"just above", 288_000 * money.Dollar, 10_000 * money.Dollar},
		{"at the cap", 389_000 * money.Dollar, 111_000 * money.Dollar},
		{"far above the cap", 5_000_000 * money.Dollar, 111_000 * money.Dollar},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := washingtontaxcalc.ExciseTax(2025, 2_000_000*money.Dollar,
				washingtontaxcalc.Deductions{CharitableDonations: tc.donated}, washingtontaxcalc.Facts{})
			if err != nil {
				t.Fatalf("ExciseTax: %v", err)
			}
			if got.Value().CharitableDeduction != tc.want {
				t.Errorf("donating %v deducts %v, want %v",
					tc.donated, got.Value().CharitableDeduction, tc.want)
			}
		})
	}
}

// TestEveryResultAsksAboutAllocation checks that the question this package
// cannot answer always comes back.
func TestEveryResultAsksAboutAllocation(t *testing.T) {
	for _, gain := range []money.USCents{0, 500_000 * money.Dollar, 5_000_000 * money.Dollar} {
		got, err := washingtontaxcalc.ExciseTax(2025, gain, washingtontaxcalc.Deductions{}, washingtontaxcalc.Facts{})
		if err != nil {
			t.Fatalf("ExciseTax: %v", err)
		}
		if !got.Needs() {
			t.Fatalf("a gain of %v came back with nothing unanswered", gain)
		}
		var found bool
		for _, r := range got.Required() {
			if r.Position() == working.PositionCapitalAssetSitus {
				found = true
			}
		}
		if !found {
			t.Errorf("a gain of %v does not ask where the gain is allocated", gain)
		}
	}
}

// TestUnknownYearIsAnError checks that a year this build does not carry is
// refused. 2024 matters here: the deduction was $270,000 and there was no
// surcharge at all, so serving 2025's figures would be wrong twice.
func TestUnknownYearIsAnError(t *testing.T) {
	for _, year := range []int{2024, 2026} {
		if _, err := washingtontaxcalc.TableFor(year); err == nil {
			t.Errorf("TableFor(%d) returned figures; this build carries only %v",
				year, washingtontaxcalc.Years())
		}
	}
	if _, err := washingtontaxcalc.ExciseTax(2024, 5_000_000*money.Dollar, washingtontaxcalc.Deductions{}, washingtontaxcalc.Facts{}); err == nil {
		t.Error("ExciseTax(2024) computed a tax under an unknown year's law")
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}
