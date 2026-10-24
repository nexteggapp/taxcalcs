// Every Oregon figure in this library, checked against what Oregon publishes.
//
// The oracle is text extracted from the 2025 Form OR-40 instructions and from
// Publication OR-17. See testdata/SOURCES.md for which PDF, how, and what the
// checks do and do not prove.
//
// The strongest of them is the tax table. Oregon prints its own answer for
// every band of taxable income below $50,000 across two columns — 1,004 cases
// nobody here chose, which is worth more than the same number of hand-written
// ones. It is also what pinned Oregon's rounding rule, which no prose in either
// document states.
package oregontaxcalc_test

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/oregontaxcalc"
)

// tableRow matches one band and its two printed taxes. Applied to every match
// on a line rather than anchored, because the printed table sets up to four
// bands side by side across the page and a row-shaped pattern would silently
// drop the columns that do not line up.
var tableRow = regexp.MustCompile(`([\d,]+)\s*—\s*([\d,]+)\s+([\d,]+)\s+([\d,]+)`)

type tableCase struct {
	atLeast, lessThan money.USCents

	// taxS is the Column S figure, for a single or separate filer. taxJ is
	// Column J, for everybody else.
	taxS, taxJ money.USCents
}

func readTaxTable(t *testing.T) []tableCase {
	t.Helper()

	raw, err := os.ReadFile("testdata/or40-2025-tax-table.txt")
	if err != nil {
		t.Fatalf("read the tax table extract: %v", err)
	}

	var cases []tableCase
	for _, line := range strings.Split(string(raw), "\n") {
		for _, m := range tableRow.FindAllStringSubmatch(line, -1) {
			cases = append(cases, tableCase{
				atLeast:  parseWholeDollars(t, m[1]),
				lessThan: parseWholeDollars(t, m[2]),
				taxS:     parseWholeDollars(t, m[3]),
				taxJ:     parseWholeDollars(t, m[4]),
			})
		}
	}

	// Printed left to right across four columns, so document order is not
	// income order.
	sortByIncome(cases)
	return cases
}

func sortByIncome(cases []tableCase) {
	for i := 1; i < len(cases); i++ {
		for j := i; j > 0 && cases[j].atLeast < cases[j-1].atLeast; j-- {
			cases[j], cases[j-1] = cases[j-1], cases[j]
		}
	}
}

// assertTableIsComplete checks that the extract covers the whole printed table
// with no holes and no repeats.
//
// This is what makes scraping a PDF defensible rather than hopeful, and it
// earned its place here: the first version of this extract filtered on rows
// that began with a band and silently dropped the whole $0 to $4,000 column,
// because that column's bands are narrower than the rest and sit on their own
// lines. It parsed 387 bands, looked healthy, and was missing a fifth of the
// table.
//
// Oregon's bands are not a constant width — $20 near zero, then $30, $50 and
// finally $100 — so a count or a step size would not do. What holds throughout
// is that the table is continuous: every band's floor is the previous band's
// ceiling, from $0 to $50,000.
func assertTableIsComplete(t *testing.T, cases []tableCase) {
	t.Helper()

	if len(cases) == 0 {
		t.Fatal("the tax table extract is empty")
	}
	if cases[0].atLeast != 0 {
		t.Errorf("the table starts at %v, want $0", cases[0].atLeast)
	}

	prev := money.USCents(0)
	for _, c := range cases {
		if c.atLeast != prev {
			t.Fatalf("the table jumps from %v to %v; a band is missing or repeated",
				prev, c.atLeast)
		}
		if c.lessThan <= c.atLeast {
			t.Fatalf("band %v to %v does not move forwards", c.atLeast, c.lessThan)
		}
		prev = c.lessThan
	}
	if want := 50_000 * money.Dollar; prev != want {
		t.Errorf("the table ends at %v, want %v", prev, money.USCents(want))
	}
}

// TestAgainstOregonTaxTable checks this library against Oregon's own answer for
// every band the printed table covers.
//
// The table gives one figure per band and Oregon computes it at the band's
// midpoint, so the comparison is against the midpoint rather than either edge.
func TestAgainstOregonTaxTable(t *testing.T) {
	cases := readTaxTable(t)
	assertTableIsComplete(t, cases)

	columns := []struct {
		status filing.Status
		pick   func(tableCase) money.USCents
	}{
		{filing.Single, func(c tableCase) money.USCents { return c.taxS }},
		{filing.MarriedFilingJointly, func(c tableCase) money.USCents { return c.taxJ }},
	}

	var checked, mismatched int
	for _, c := range cases {
		midpoint := (c.atLeast + c.lessThan) / 2

		for _, col := range columns {
			want := col.pick(c)

			got, err := oregontaxcalc.OrdinaryIncomeTax(2025, col.status, midpoint)
			if err != nil {
				t.Fatalf("OrdinaryIncomeTax(2025, %v, %v): %v", col.status, midpoint, err)
			}
			checked++

			if got.Value().Total != want {
				mismatched++
				if mismatched <= 10 {
					t.Errorf("taxable income %v, %v: got %v, the table says %v",
						midpoint, col.status, got.Value().Total, want)
				}
			}
		}
	}
	if mismatched > 10 {
		t.Errorf("... and %d further mismatches", mismatched-10)
	}
	t.Logf("checked %d cases from the 2025 Oregon tax table across %d bands", checked, len(cases))
}

// TestRateChartBaseAmounts checks the four figures Oregon prints for income
// above what the tax table covers.
//
// These are the only published figures for the top of the table, and they are
// what first showed that Oregon rounds each bracket rather than the total: the
// exact sum for a joint filer at $50,000 is $3,755.00 and Oregon prints $3,756.
func TestRateChartBaseAmounts(t *testing.T) {
	text := readFile(t, "testdata/or40-2025-rate-charts.txt")

	for _, tc := range []struct {
		name    string
		status  filing.Status
		at      money.USCents
		want    money.USCents
		printed string
	}{
		{"Chart S at $50,000", filing.Single, 50_000 * money.Dollar, 4_065 * money.Dollar, "$4,065"},
		{"Chart S at $125,000", filing.Single, 125_000 * money.Dollar, 10_627 * money.Dollar, "$10,627"},
		{"Chart J at $50,000", filing.MarriedFilingJointly, 50_000 * money.Dollar, 3_756 * money.Dollar, "$3,756"},
		{"Chart J at $250,000", filing.MarriedFilingJointly, 250_000 * money.Dollar, 21_256 * money.Dollar, "$21,256"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(text, tc.printed) {
				t.Fatalf("the rate charts no longer print %s", tc.printed)
			}
			got, err := oregontaxcalc.OrdinaryIncomeTax(2025, tc.status, tc.at)
			if err != nil {
				t.Fatalf("OrdinaryIncomeTax: %v", err)
			}
			if got.Value().Total != tc.want {
				t.Errorf("tax on %v is %v, the chart says %v", tc.at, got.Value().Total, tc.want)
			}
		})
	}

	// The charts also state the rates above those points.
	for _, want := range []string{"8.75%", "9.9%"} {
		if !strings.Contains(text, want) {
			t.Errorf("the rate charts no longer print %s", want)
		}
	}
}

// TestBracketsMatchPublicationOR17 checks the thresholds against the four-year
// table whose last column is 2025.
func TestBracketsMatchPublicationOR17(t *testing.T) {
	text := readFile(t, "testdata/or17-2025-annual-figures.txt")

	// The extract carries four years side by side, so the test has to pin that
	// 2025 is the column it is reading. Every figure below is the last on its
	// row.
	if !strings.Contains(text, "for tax years 2022–2025") {
		t.Fatal("the extract no longer says which tax years it covers")
	}

	for _, want := range []string{
		"First $4,400",   // Chart S, 4.75%
		"$4,401–$11,100", // Chart S, 6.75%
		"First $8,800",   // Chart J, 4.75%
		"$8,801–$22,200", // Chart J, 6.75%
	} {
		if !strings.Contains(text, want) {
			t.Errorf("Publication OR-17 no longer prints %q for 2025", want)
		}
	}

	single, err := oregontaxcalc.TableFor(2025, filing.Single)
	if err != nil {
		t.Fatalf("TableFor: %v", err)
	}
	wantS := []money.USCents{0, 4_400 * money.Dollar, 11_100 * money.Dollar, 125_000 * money.Dollar}
	for i, want := range wantS {
		if single.Ordinary[i].Over != want {
			t.Errorf("Chart S bracket %d starts over %v, want %v", i, single.Ordinary[i].Over, want)
		}
	}

	joint, err := oregontaxcalc.TableFor(2025, filing.MarriedFilingJointly)
	if err != nil {
		t.Fatalf("TableFor: %v", err)
	}
	wantJ := []money.USCents{0, 8_800 * money.Dollar, 22_200 * money.Dollar, 250_000 * money.Dollar}
	for i, want := range wantJ {
		if joint.Ordinary[i].Over != want {
			t.Errorf("Chart J bracket %d starts over %v, want %v", i, joint.Ordinary[i].Over, want)
		}
	}
}

// TestStandardDeductionMatchesTable5 checks the five figures and the additions
// for age and blindness.
func TestStandardDeductionMatchesTable5(t *testing.T) {
	text := readFile(t, "testdata/or40-2025-standard-deduction.txt")

	for _, tc := range []struct {
		status  filing.Status
		printed string
		want    money.USCents
	}{
		{filing.Single, "$2,835", 2_835 * money.Dollar},
		{filing.MarriedFilingJointly, "$5,670", 5_670 * money.Dollar},
		{filing.MarriedFilingSeparately, "$2,835", 2_835 * money.Dollar},
		{filing.HeadOfHousehold, "$4,560", 4_560 * money.Dollar},
		{filing.SurvivingSpouse, "$5,670", 5_670 * money.Dollar},
	} {
		if !strings.Contains(text, tc.printed) {
			t.Errorf("Table 5 no longer prints %s", tc.printed)
		}
		table, err := oregontaxcalc.TableFor(2025, tc.status)
		if err != nil {
			t.Fatalf("TableFor: %v", err)
		}
		if table.StandardDeduction != tc.want {
			t.Errorf("%s standard deduction is %v, Table 5 says %v",
				tc.status, table.StandardDeduction, tc.want)
		}
	}

	// The additions differ by status, which is easy to get backwards.
	if !strings.Contains(text, "$1,200 if single or head of household") {
		t.Error("the instructions no longer give the larger addition to single and head of household filers")
	}
	for _, tc := range []struct {
		status filing.Status
		want   money.USCents
	}{
		{filing.Single, 1_200 * money.Dollar},
		{filing.HeadOfHousehold, 1_200 * money.Dollar},
		{filing.MarriedFilingJointly, 1_000 * money.Dollar},
		{filing.MarriedFilingSeparately, 1_000 * money.Dollar},
		{filing.SurvivingSpouse, 1_000 * money.Dollar},
	} {
		table, err := oregontaxcalc.TableFor(2025, tc.status)
		if err != nil {
			t.Fatalf("TableFor: %v", err)
		}
		if table.AdditionalAgedBlind != tc.want {
			t.Errorf("%s addition for age or blindness is %v, want %v",
				tc.status, table.AdditionalAgedBlind, tc.want)
		}
	}
}

// TestSpouseItemisingRemovesTheStandardDeduction checks the rule that has no
// federal counterpart in this form.
func TestSpouseItemisingRemovesTheStandardDeduction(t *testing.T) {
	text := readFile(t, "testdata/or40-2025-standard-deduction.txt")
	if !strings.Contains(text, "If your spouse itemizes deductions, your standard deduction is $0") &&
		!strings.Contains(text, "If spouse claims itemized deductions") {
		t.Error("Table 5 no longer states the separate-return rule")
	}

	got, err := oregontaxcalc.StandardDeduction(2025, filing.MarriedFilingSeparately,
		oregontaxcalc.Conditions{SpouseItemises: true})
	if err != nil {
		t.Fatalf("StandardDeduction: %v", err)
	}
	if got.Value() != 0 {
		t.Errorf("standard deduction with an itemising spouse is %v, want nothing", got.Value())
	}

	// And the addition for age does not resurrect it.
	aged, err := oregontaxcalc.StandardDeduction(2025, filing.MarriedFilingSeparately,
		oregontaxcalc.Conditions{SpouseItemises: true, Aged65OrOlder: true})
	if err != nil {
		t.Fatalf("StandardDeduction: %v", err)
	}
	if aged.Value() != 0 {
		t.Errorf("standard deduction with an itemising spouse and an aged filer is %v, want nothing", aged.Value())
	}
}

// TestFederalTaxSubtractionMatchesTable4 checks every step of the phase-out.
func TestFederalTaxSubtractionMatchesTable4(t *testing.T) {
	text := readFile(t, "testdata/or40-2025-federal-tax-subtraction.txt")
	for _, want := range []string{"$8,500", "$6,800", "$5,100", "$3,400", "$1,700", "$4,250", "$850"} {
		if !strings.Contains(text, want) {
			t.Errorf("Table 4 no longer prints %s", want)
		}
	}

	// A federal tax liability far larger than any cap, so the answer is the cap
	// itself at every step.
	const huge = 99_999 * money.Dollar

	for _, tc := range []struct {
		status filing.Status
		agi    money.USCents
		want   money.USCents
	}{
		{filing.Single, 124_999 * money.Dollar, 8_500 * money.Dollar},
		{filing.Single, 125_000 * money.Dollar, 6_800 * money.Dollar},
		{filing.Single, 130_000 * money.Dollar, 5_100 * money.Dollar},
		{filing.Single, 135_000 * money.Dollar, 3_400 * money.Dollar},
		{filing.Single, 140_000 * money.Dollar, 1_700 * money.Dollar},
		{filing.Single, 145_000 * money.Dollar, 0},
		{filing.MarriedFilingSeparately, 0, 4_250 * money.Dollar},
		{filing.MarriedFilingSeparately, 140_000 * money.Dollar, 850 * money.Dollar},
		{filing.MarriedFilingJointly, 249_999 * money.Dollar, 8_500 * money.Dollar},
		{filing.MarriedFilingJointly, 250_000 * money.Dollar, 6_800 * money.Dollar},
		{filing.MarriedFilingJointly, 290_000 * money.Dollar, 0},
		{filing.HeadOfHousehold, 250_000 * money.Dollar, 6_800 * money.Dollar},
	} {
		got, err := oregontaxcalc.FederalTaxSubtraction(2025, tc.status, huge, tc.agi)
		if err != nil {
			t.Fatalf("FederalTaxSubtraction: %v", err)
		}
		if got.Value() != tc.want {
			t.Errorf("%s at %v of AGI may subtract %v, Table 4 says %v",
				tc.status, tc.agi, got.Value(), tc.want)
		}
	}

	// Below the cap the filer's own liability is the limit.
	small, err := oregontaxcalc.FederalTaxSubtraction(2025, filing.Single, 1_200*money.Dollar, 0)
	if err != nil {
		t.Fatalf("FederalTaxSubtraction: %v", err)
	}
	if want := 1_200 * money.Dollar; small.Value() != want {
		t.Errorf("a federal tax of %v gives a subtraction of %v", money.USCents(want), small.Value())
	}
}

// TestExemptionCreditIsACliff checks that the credit vanishes rather than
// tapering, because that is what Oregon does and it is worth being sure of.
func TestExemptionCreditIsACliff(t *testing.T) {
	text := readFile(t, "testdata/or17-2025-annual-figures.txt")
	if !strings.Contains(text, "$256") {
		t.Error("Publication OR-17 no longer prints the $256 exemption credit for 2025")
	}
	if !strings.Contains(text, "the exemption credit amount is $0") {
		t.Error("Publication OR-17 no longer states that the credit goes to zero")
	}

	at, err := oregontaxcalc.ExemptionCredit(2025, filing.Single, 100_000*money.Dollar, 2)
	if err != nil {
		t.Fatalf("ExemptionCredit: %v", err)
	}
	if want := 512 * money.Dollar; at.Value() != want {
		t.Errorf("credit at exactly the limit is %v, want %v", at.Value(), money.USCents(want))
	}

	over, err := oregontaxcalc.ExemptionCredit(2025, filing.Single, 100_001*money.Dollar, 2)
	if err != nil {
		t.Fatalf("ExemptionCredit: %v", err)
	}
	if over.Value() != 0 {
		t.Errorf("credit one dollar over the limit is %v, want nothing", over.Value())
	}

	// A joint return gets twice the room.
	joint, err := oregontaxcalc.ExemptionCredit(2025, filing.MarriedFilingJointly, 200_000*money.Dollar, 4)
	if err != nil {
		t.Fatalf("ExemptionCredit: %v", err)
	}
	if want := 1_024 * money.Dollar; joint.Value() != want {
		t.Errorf("joint credit at the limit is %v, want %v", joint.Value(), money.USCents(want))
	}
}

// TestUnknownYearIsAnError checks that a year this build does not carry is
// refused rather than served from an adjacent year.
func TestUnknownYearIsAnError(t *testing.T) {
	for _, year := range []int{2024, 2026} {
		if _, err := oregontaxcalc.TableFor(year, filing.Single); err == nil {
			t.Errorf("TableFor(%d) returned a table; this build carries only %v",
				year, oregontaxcalc.Years())
		}
	}
	if _, err := oregontaxcalc.OrdinaryIncomeTax(2024, filing.Single, 100*money.Dollar); err == nil {
		t.Error("OrdinaryIncomeTax(2024) computed a tax under an unknown year's law")
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

// parseWholeDollars turns "49,900" into cents. The printed table carries no
// fractions, so anything with a decimal point is a parse the caller did not
// mean.
func parseWholeDollars(t *testing.T, s string) money.USCents {
	t.Helper()

	n, err := strconv.ParseInt(strings.ReplaceAll(s, ",", ""), 10, 64)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return money.USCents(n) * money.Dollar
}
