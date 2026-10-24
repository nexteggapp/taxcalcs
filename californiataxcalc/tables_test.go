// Every California figure in this library, checked against the booklet the
// Franchise Tax Board publishes.
//
// The oracle is text extracted from the 2025 Personal Income Tax Booklet. See
// testdata/SOURCES.md for which PDF, how it was extracted, and what the checks
// do and do not prove.
//
// The rate schedules are the strong case. The booklet prints, for every one of
// the twenty-seven brackets, the tax on all income below it — the middle column
// of Schedule X, Y and Z. That figure is the Franchise Tax Board's own
// arithmetic over the brackets beneath, so checking this library's ladder
// against it checks the whole ladder and not just one threshold.
package californiataxcalc_test

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/nexteggapp/taxcalcs/californiataxcalc"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
)

// bracketRow matches one printed bracket: the base tax, the rate, and the
// income the rate applies above. Anchored at the end of the line, because "of
// the amount over" is the last column the booklet prints.
var bracketRow = regexp.MustCompile(`([\d,]+\.\d{2})\s+\+\s+([\d.]+)%\s+\$?\s*([\d,]+)\s*$`)

type printedBracket struct {
	base money.USCents
	rate money.Rate
	over money.USCents
}

// readSchedules parses the extract into the three printed rate schedules, in
// the order the booklet prints them: X, then Y, then Z.
//
// A schedule is split off wherever a row's "of the amount over" is zero, which
// is the first row of each. That is the document's own structure rather than a
// line count, so a schedule gaining or losing a bracket is a test failure and
// not a silent reshuffle.
func readSchedules(t *testing.T) [][]printedBracket {
	t.Helper()

	raw, err := os.ReadFile("testdata/ftb-540-2025-rate-schedules.txt")
	if err != nil {
		t.Fatalf("read the rate schedule extract: %v", err)
	}

	var schedules [][]printedBracket
	var current []printedBracket
	for _, line := range strings.Split(string(raw), "\n") {
		m := bracketRow.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		b := printedBracket{
			base: parseAmount(t, m[1]),
			rate: parseRate(t, m[2]),
			over: parseAmount(t, m[3]),
		}
		if b.over == 0 && len(current) > 0 {
			schedules = append(schedules, current)
			current = nil
		}
		current = append(current, b)
	}
	if len(current) > 0 {
		schedules = append(schedules, current)
	}
	return schedules
}

// TestExtractIsComplete checks the extract holds all three schedules with all
// nine brackets each, before any figure is compared.
//
// Scraping a PDF drops things quietly. A count is the weakest form of this
// check and it is still worth making, because the failure it catches — a
// schedule clipped by a page break — would otherwise show up as a test that
// passes while checking two thirds of the table.
func TestExtractIsComplete(t *testing.T) {
	schedules := readSchedules(t)

	if len(schedules) != 3 {
		t.Fatalf("the extract holds %d rate schedules, want 3 (X, Y and Z)", len(schedules))
	}
	for i, s := range schedules {
		if len(s) != 9 {
			t.Errorf("schedule %d has %d brackets, want 9", i, len(s))
		}
	}

	// The schedules are told apart by their order, so the extract has to name
	// them in that order for the order to mean anything.
	text := readFile(t, "testdata/ftb-540-2025-rate-schedules.txt")
	x, y, z := strings.Index(text, "Schedule X"), strings.Index(text, "Schedule Y"), strings.Index(text, "Schedule Z")
	if x < 0 || y < 0 || z < 0 {
		t.Fatal("the extract no longer names Schedule X, Y and Z")
	}
	if !(x < y && y < z) {
		t.Fatal("the extract no longer prints Schedule X, then Y, then Z")
	}

	// And each has to say which filers it is for, because that is the only
	// thing tying Schedule Z to a head of household rather than to anybody else.
	for _, want := range []string{
		"Single or Married/RDP Filing Separately",
		"Married/RDP Filing Jointly or Qualifying",
		"Head of Household",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the extract no longer says %q", want)
		}
	}
}

// TestTablesMatchThePrintedSchedules compares every threshold and rate this
// library carries against the booklet.
func TestTablesMatchThePrintedSchedules(t *testing.T) {
	schedules := readSchedules(t)
	if len(schedules) != 3 {
		t.Fatal("the extract is incomplete; see TestExtractIsComplete")
	}

	for _, tc := range []struct {
		status   filing.Status
		schedule int
	}{
		{filing.Single, 0},
		{filing.MarriedFilingSeparately, 0},
		{filing.MarriedFilingJointly, 1},
		{filing.SurvivingSpouse, 1},
		{filing.HeadOfHousehold, 2},
	} {
		t.Run(tc.status.String(), func(t *testing.T) {
			table, err := californiataxcalc.TableFor(2025, tc.status)
			if err != nil {
				t.Fatalf("TableFor: %v", err)
			}
			printed := schedules[tc.schedule]

			if len(table.Ordinary) != len(printed) {
				t.Fatalf("this library carries %d brackets, the booklet prints %d",
					len(table.Ordinary), len(printed))
			}
			for i, want := range printed {
				got := table.Ordinary[i]
				if got.Over != want.over {
					t.Errorf("bracket %d starts over %v, the booklet says %v", i, got.Over, want.over)
				}
				if got.Rate != want.rate {
					t.Errorf("bracket %d is at %v, the booklet says %v", i, got.Rate, want.rate)
				}
				if got.BaseTax != want.base {
					t.Errorf("bracket %d carries a base tax of %v, the booklet says %v", i, got.BaseTax, want.base)
				}
			}
		})
	}
}

// TestLadderReproducesEveryPublishedBaseAmount is the check that the brackets
// are right rather than merely transcribed.
//
// The base tax the booklet prints beside a bracket is the tax on income at
// exactly that bracket's floor, worked out by the Franchise Tax Board over
// every band beneath it. So computing the tax at each floor and comparing
// against the printed figure checks twenty-seven cumulative sums, and a single
// wrong threshold or rate anywhere in a schedule breaks every base above it.
func TestLadderReproducesEveryPublishedBaseAmount(t *testing.T) {
	checked := 0
	for _, status := range []filing.Status{
		filing.Single,
		filing.MarriedFilingSeparately,
		filing.MarriedFilingJointly,
		filing.SurvivingSpouse,
		filing.HeadOfHousehold,
	} {
		table, err := californiataxcalc.TableFor(2025, status)
		if err != nil {
			t.Fatalf("TableFor: %v", err)
		}
		for i, b := range table.Ordinary {
			got, err := californiataxcalc.OrdinaryIncomeTax(2025, status, b.Over)
			if err != nil {
				t.Fatalf("OrdinaryIncomeTax: %v", err)
			}
			if got.Value().Total != b.BaseTax {
				t.Errorf("%s bracket %d: tax on %v is %v, the booklet prints %v",
					status, i, b.Over, got.Value().Total, b.BaseTax)
			}
			checked++
		}
	}
	t.Logf("checked %d published base amounts across five filing statuses", checked)
}

// TestTheBookletsWorkedExample runs the example the Franchise Tax Board prints
// under the rate schedules.
//
// Inputs and answer both chosen by the body that wrote the rule, which is what
// makes it worth more than a case picked here. The booklet works Chris and Pat
// Smith's joint return at $125,000 of taxable income through Schedule Y and
// gets $4,768.10.
func TestTheBookletsWorkedExample(t *testing.T) {
	const (
		taxable = 125_000 * money.Dollar
		want    = 4_768*money.Dollar + 10
	)

	// The example is in the extract, so it cannot drift from what the booklet
	// prints without the extract being refreshed.
	text := readFile(t, "testdata/ftb-540-2025-rate-schedules.txt")
	for _, phrase := range []string{"$ 125,000", "$4,768.10", "9,916"} {
		if !strings.Contains(text, phrase) {
			t.Fatalf("the extract no longer contains %q from the worked example", phrase)
		}
	}

	got, err := californiataxcalc.OrdinaryIncomeTax(2025, filing.MarriedFilingJointly, taxable)
	if err != nil {
		t.Fatalf("OrdinaryIncomeTax: %v", err)
	}
	if got.Value().Total != want {
		t.Errorf("tax on %v filing jointly is %v, the booklet works it out as %v",
			taxable, got.Value().Total, money.USCents(want))
	}
}

// TestStandardDeductionMatchesTheChart checks the five figures against the
// California Standard Deduction Chart for Most People.
func TestStandardDeductionMatchesTheChart(t *testing.T) {
	text := readFile(t, "testdata/ftb-540-2025-standard-deduction.txt")

	for _, tc := range []struct {
		status  filing.Status
		printed string
		want    money.USCents
	}{
		{filing.Single, "$5,706", 5_706 * money.Dollar},
		{filing.MarriedFilingJointly, "$11,412", 11_412 * money.Dollar},
		{filing.MarriedFilingSeparately, "$5,706", 5_706 * money.Dollar},
		{filing.HeadOfHousehold, "$11,412", 11_412 * money.Dollar},
		{filing.SurvivingSpouse, "$11,412", 11_412 * money.Dollar},
	} {
		if !strings.Contains(text, tc.printed) {
			t.Errorf("the chart no longer prints %s", tc.printed)
		}
		table, err := californiataxcalc.TableFor(2025, tc.status)
		if err != nil {
			t.Fatalf("TableFor: %v", err)
		}
		if table.StandardDeduction != tc.want {
			t.Errorf("%s standard deduction is %v, the chart says %v",
				tc.status, table.StandardDeduction, tc.want)
		}
	}

	// The chart says this in words, and it is the fact that makes a California
	// return worth itemising when the federal one was not.
	if !strings.Contains(text, "less than the federal") {
		t.Error("the chart no longer says the California amounts are less than the federal ones")
	}
}

// TestExemptionCreditsMatchTheForm checks the credits against the amounts
// pre-printed on Form 540.
func TestExemptionCreditsMatchTheForm(t *testing.T) {
	text := readFile(t, "testdata/ftb-540-2025-exemption-credits.txt")

	for _, want := range []string{"$153", "$475"} {
		if !strings.Contains(text, want) {
			t.Errorf("Form 540 no longer prints %s", want)
		}
	}

	table, err := californiataxcalc.TableFor(2025, filing.Single)
	if err != nil {
		t.Fatalf("TableFor: %v", err)
	}
	if want := 153 * money.Dollar; table.PersonalExemptionCredit != want {
		t.Errorf("personal exemption credit is %v, the form prints %v", table.PersonalExemptionCredit, want)
	}
	if want := 475 * money.Dollar; table.DependentExemptionCredit != want {
		t.Errorf("dependent exemption credit is %v, the form prints %v", table.DependentExemptionCredit, want)
	}
}

// TestBehavioralHealthTaxMatchesTheWorksheet checks the threshold and the rate
// against the Line 62 worksheet.
func TestBehavioralHealthTaxMatchesTheWorksheet(t *testing.T) {
	text := readFile(t, "testdata/ftb-540-2025-behavioral-health-tax.txt")

	for _, want := range []string{"$1,000,000", "1%"} {
		if !strings.Contains(text, want) {
			t.Errorf("the worksheet no longer states %s", want)
		}
	}

	// The worksheet is the whole rule: subtract a million, take one per cent.
	got, err := californiataxcalc.BehavioralHealthServicesTax(2025, filing.Single, 1_500_000*money.Dollar)
	if err != nil {
		t.Fatalf("BehavioralHealthServicesTax: %v", err)
	}
	if want := 5_000 * money.Dollar; got.Value() != want {
		t.Errorf("surcharge on $1,500,000 is %v, the worksheet gives %v", got.Value(), want)
	}

	// And nothing at all at the threshold itself, because § 17043 says "in
	// excess of".
	at, err := californiataxcalc.BehavioralHealthServicesTax(2025, filing.Single, 1_000_000*money.Dollar)
	if err != nil {
		t.Fatalf("BehavioralHealthServicesTax: %v", err)
	}
	if at.Value() != 0 {
		t.Errorf("surcharge at exactly $1,000,000 is %v, want nothing", at.Value())
	}
}

// TestExemptionCreditPhaseOut works the AGI Limitation Worksheet by hand.
//
// A single filer $6,000 over the $252,203 threshold: $6,000 divided by the
// $2,500 step is 2.4, rounded up to 3 steps, so $18 comes off each exemption.
// One personal exemption of $153 becomes $135, and one dependent's $475
// becomes $457.
func TestExemptionCreditPhaseOut(t *testing.T) {
	agi := 258_203 * money.Dollar

	got, err := californiataxcalc.ExemptionCredit(2025, filing.Single, agi,
		californiataxcalc.Exemptions{Personal: 1, Dependents: 1})
	if err != nil {
		t.Fatalf("ExemptionCredit: %v", err)
	}

	want := (153-18)*money.Dollar + (475-18)*money.Dollar
	if got.Value() != want {
		t.Errorf("credit is %v, the worksheet gives %v", got.Value(), want)
	}

	// Below the threshold nothing is lost.
	under, err := californiataxcalc.ExemptionCredit(2025, filing.Single, 252_203*money.Dollar,
		californiataxcalc.Exemptions{Personal: 1, Dependents: 1})
	if err != nil {
		t.Fatalf("ExemptionCredit: %v", err)
	}
	if want := 628 * money.Dollar; under.Value() != want {
		t.Errorf("credit at the threshold is %v, want the full %v", under.Value(), want)
	}
}

// TestUnknownYearIsAnError checks that a year this build does not carry is
// refused rather than served from an adjacent year.
func TestUnknownYearIsAnError(t *testing.T) {
	for _, year := range []int{2024, 2026} {
		if _, err := californiataxcalc.TableFor(year, filing.Single); err == nil {
			t.Errorf("TableFor(%d) returned a table; this build carries only %v",
				year, californiataxcalc.Years())
		}
	}
	if _, err := californiataxcalc.OrdinaryIncomeTax(2024, filing.Single, 100*money.Dollar); err == nil {
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

// parseAmount turns "$11,079" or "72,219.84" into cents.
func parseAmount(t *testing.T, s string) money.USCents {
	t.Helper()

	s = strings.NewReplacer("$", "", ",", "", " ", "").Replace(s)
	whole, frac, hasFrac := strings.Cut(s, ".")
	n, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		t.Fatalf("parse amount %q: %v", s, err)
	}
	out := money.USCents(n) * money.Dollar
	if hasFrac {
		c, err := strconv.ParseInt((frac + "00")[:2], 10, 64)
		if err != nil {
			t.Fatalf("parse cents in %q: %v", s, err)
		}
		out += money.USCents(c)
	}
	return out
}

// parseRate turns "9.30" into basis points, without going through a float.
func parseRate(t *testing.T, s string) money.Rate {
	t.Helper()

	whole, frac, _ := strings.Cut(s, ".")
	w, err := strconv.ParseInt(whole, 10, 32)
	if err != nil {
		t.Fatalf("parse rate %q: %v", s, err)
	}
	f, err := strconv.ParseInt((frac + "00")[:2], 10, 32)
	if err != nil {
		t.Fatalf("parse rate fraction in %q: %v", s, err)
	}
	return money.Rate(w*100 + f)
}
