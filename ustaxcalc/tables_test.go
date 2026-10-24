package ustaxcalc_test

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/nexteggapp/taxcalcs/ustaxcalc"
)

// TestTablesMatchRevenueProcedure checks every 2026 figure against the text of
// Rev. Proc. 2025-32.
//
// The extract is checked in rather than downloaded, so a failure means this
// package changed rather than that the IRS republished. Refreshing it is a
// deliberate commit, which is also how a new tax year enters.
//
// It reads the figures out of the prose rather than comparing against a second
// list of constants. A second list would be the same transcription mistake
// written twice.
func TestTablesMatchRevenueProcedure(t *testing.T) {
	extract := readExtract(t)

	t.Run("brackets", func(t *testing.T) {
		// Each table in the revenue procedure is a run of "Not over $X" and
		// "Over $X but not over $Y" lines under a heading naming the status.
		for _, tc := range []struct {
			heading string
			status  ustaxcalc.FilingStatus
		}{
			{"TABLE 1", ustaxcalc.MarriedFilingJointly},
			{"TABLE 1", ustaxcalc.SurvivingSpouse},
			{"TABLE 2", ustaxcalc.HeadOfHousehold},
			{"TABLE 3", ustaxcalc.Single},
			{"TABLE 4", ustaxcalc.MarriedFilingSeparately},
		} {
			t.Run(tc.status.String(), func(t *testing.T) {
				want := thresholdsUnder(t, extract, tc.heading)
				table, err := ustaxcalc.TableFor(2026, tc.status)
				if err != nil {
					t.Fatalf("TableFor(2026, %s): %v", tc.status, err)
				}

				// The first bracket starts at zero and has no threshold in the
				// document, so the published list is one shorter.
				var got []ustaxcalc.USCents
				for _, b := range table.Ordinary[1:] {
					got = append(got, b.Over)
				}
				if len(got) != len(want) {
					t.Fatalf("%s has %d thresholds, %s publishes %d: %v vs %v",
						tc.status, len(got), tc.heading, len(want), got, want)
				}
				for i := range want {
					if got[i] != want[i] {
						t.Errorf("%s bracket %d starts over %s, %s says %s",
							tc.status, i+2, got[i], tc.heading, want[i])
					}
				}
			})
		}
	})

	t.Run("rates are the seven statutory ones", func(t *testing.T) {
		want := []ustaxcalc.Rate{
			ustaxcalc.Percent(10), ustaxcalc.Percent(12), ustaxcalc.Percent(22),
			ustaxcalc.Percent(24), ustaxcalc.Percent(32), ustaxcalc.Percent(35),
			ustaxcalc.Percent(37),
		}
		for _, status := range allStatuses() {
			table, err := ustaxcalc.TableFor(2026, status)
			if err != nil {
				t.Fatalf("TableFor(2026, %s): %v", status, err)
			}
			if len(table.Ordinary) != len(want) {
				t.Fatalf("%s has %d brackets, want %d", status, len(table.Ordinary), len(want))
			}
			for i, b := range table.Ordinary {
				if b.Rate != want[i] {
					t.Errorf("%s bracket %d is %s, want %s", status, i+1, b.Rate, want[i])
				}
			}
		}
	})

	t.Run("standard deduction", func(t *testing.T) {
		section := standardDeductionSection(t, extract)
		// § 3.14(1) gives one line per status.
		for _, tc := range []struct {
			line   string
			status ustaxcalc.FilingStatus
			want   ustaxcalc.USCents
		}{
			{"Married Individuals Filing Joint Returns and Surviving Spouses", ustaxcalc.MarriedFilingJointly, ustaxcalc.Dollars(32_200)},
			{"Married Individuals Filing Joint Returns and Surviving Spouses", ustaxcalc.SurvivingSpouse, ustaxcalc.Dollars(32_200)},
			{"Heads of Households", ustaxcalc.HeadOfHousehold, ustaxcalc.Dollars(24_150)},
			{"Unmarried Individuals (other than Surviving Spouses and Heads of", ustaxcalc.Single, ustaxcalc.Dollars(16_100)},
			{"Married Individuals Filing Separate Returns", ustaxcalc.MarriedFilingSeparately, ustaxcalc.Dollars(16_100)},
		} {
			published := amountOnLineAfter(t, section, tc.line)
			if published != tc.want {
				t.Fatalf("the extract gives %s for %q, the test expected %s", published, tc.line, tc.want)
			}
			table, err := ustaxcalc.TableFor(2026, tc.status)
			if err != nil {
				t.Fatalf("TableFor(2026, %s): %v", tc.status, err)
			}
			if table.StandardDeduction != published {
				t.Errorf("%s standard deduction is %s, the revenue procedure says %s",
					tc.status, table.StandardDeduction, published)
			}
		}
	})

	t.Run("additional deduction for age or blindness", func(t *testing.T) {
		// § 3.14(3) states the base amount and the larger one for an unmarried
		// individual who is not a surviving spouse.
		if !strings.Contains(extract, "for the aged or the blind is $1,650") {
			t.Fatal("the extract no longer states the $1,650 additional deduction")
		}
		if !strings.Contains(extract, "increased to $2,050 if the individual is also unmarried and") {
			t.Fatal("the extract no longer states the $2,050 unmarried amount")
		}
		for _, tc := range []struct {
			status ustaxcalc.FilingStatus
			want   ustaxcalc.USCents
		}{
			{ustaxcalc.MarriedFilingJointly, ustaxcalc.Dollars(1_650)},
			{ustaxcalc.MarriedFilingSeparately, ustaxcalc.Dollars(1_650)},
			// A surviving spouse is excluded from the larger amount by name.
			{ustaxcalc.SurvivingSpouse, ustaxcalc.Dollars(1_650)},
			{ustaxcalc.Single, ustaxcalc.Dollars(2_050)},
			{ustaxcalc.HeadOfHousehold, ustaxcalc.Dollars(2_050)},
		} {
			table, err := ustaxcalc.TableFor(2026, tc.status)
			if err != nil {
				t.Fatalf("TableFor(2026, %s): %v", tc.status, err)
			}
			if table.AdditionalAgedBlind != tc.want {
				t.Errorf("%s additional deduction is %s, want %s", tc.status, table.AdditionalAgedBlind, tc.want)
			}
		}
	})

	t.Run("capital gains thresholds", func(t *testing.T) {
		// § 3.03 is a table of two amounts per status.
		for _, tc := range []struct {
			line   string
			status ustaxcalc.FilingStatus
		}{
			{"Married Individuals Filing Joint Returns and", ustaxcalc.MarriedFilingJointly},
			{"Married Individuals Filing Joint Returns and", ustaxcalc.SurvivingSpouse},
			{"Married Individuals Filing Separate Returns", ustaxcalc.MarriedFilingSeparately},
			{"Heads of Household", ustaxcalc.HeadOfHousehold},
			{"All Other Individuals", ustaxcalc.Single},
		} {
			zero, fifteen := twoAmountsOnLine(t, extract, tc.line)
			table, err := ustaxcalc.TableFor(2026, tc.status)
			if err != nil {
				t.Fatalf("TableFor(2026, %s): %v", tc.status, err)
			}
			if table.MaxZeroRateAmount != zero {
				t.Errorf("%s zero rate amount is %s, the revenue procedure says %s",
					tc.status, table.MaxZeroRateAmount, zero)
			}
			if table.Max15RateAmount != fifteen {
				t.Errorf("%s 15%% rate amount is %s, the revenue procedure says %s",
					tc.status, table.Max15RateAmount, fifteen)
			}
		}
	})
}

// TestTables2025MatchSources checks the 2025 figures against the documents they
// came from.
//
// Separate from the 2026 test because 2025 has two sources rather than one. The
// rate tables and capital gains thresholds are Rev. Proc. 2024-40 and are
// checked the same way. The standard deduction is not: legislation during 2025
// raised the § 63(c) amounts above what that revenue procedure published, so
// the figures are the ones the IRS printed on the form.
func TestTables2025MatchSources(t *testing.T) {
	revProc := readExtractFile(t, "rp-2024-40-extract.txt")

	t.Run("brackets", func(t *testing.T) {
		for _, tc := range []struct {
			heading string
			status  ustaxcalc.FilingStatus
		}{
			{"TABLE 1", ustaxcalc.MarriedFilingJointly},
			{"TABLE 1", ustaxcalc.SurvivingSpouse},
			{"TABLE 2", ustaxcalc.HeadOfHousehold},
			{"TABLE 3", ustaxcalc.Single},
			{"TABLE 4", ustaxcalc.MarriedFilingSeparately},
		} {
			t.Run(tc.status.String(), func(t *testing.T) {
				want := thresholdsUnder(t, revProc, tc.heading)
				table, err := ustaxcalc.TableFor(2025, tc.status)
				if err != nil {
					t.Fatalf("TableFor(2025, %s): %v", tc.status, err)
				}
				var got []ustaxcalc.USCents
				for _, b := range table.Ordinary[1:] {
					got = append(got, b.Over)
				}
				if len(got) != len(want) {
					t.Fatalf("%s has %d thresholds, the revenue procedure publishes %d: %v vs %v",
						tc.status, len(got), len(want), got, want)
				}
				for i := range got {
					if got[i] != want[i] {
						t.Errorf("%s threshold %d is %s, the revenue procedure says %s",
							tc.status, i+1, got[i], want[i])
					}
				}
			})
		}
	})

	t.Run("capital gains thresholds", func(t *testing.T) {
		for _, tc := range []struct {
			line   string
			status ustaxcalc.FilingStatus
		}{
			{"Married Individuals Filing Joint Returns and", ustaxcalc.MarriedFilingJointly},
			{"Married Individuals Filing Joint Returns and", ustaxcalc.SurvivingSpouse},
			{"Married Individuals Filing Separate Returns", ustaxcalc.MarriedFilingSeparately},
			{"Heads of Household", ustaxcalc.HeadOfHousehold},
			{"All Other Individuals", ustaxcalc.Single},
		} {
			zero, fifteen := twoAmountsOnLine(t, revProc, tc.line)
			table, err := ustaxcalc.TableFor(2025, tc.status)
			if err != nil {
				t.Fatalf("TableFor(2025, %s): %v", tc.status, err)
			}
			if table.MaxZeroRateAmount != zero {
				t.Errorf("%s zero rate amount is %s, the revenue procedure says %s",
					tc.status, table.MaxZeroRateAmount, zero)
			}
			if table.Max15RateAmount != fifteen {
				t.Errorf("%s 15%% rate amount is %s, the revenue procedure says %s",
					tc.status, table.Max15RateAmount, fifteen)
			}
		}
	})

	// The standard deduction is checked against the Standard Deduction
	// Worksheet in the 2025 Form 1040 instructions rather than against a
	// revenue procedure, because legislation during 2025 superseded the
	// revenue procedure's § 63(c) figures.
	//
	// The worksheet states both parts outright — the amount per status, and
	// "$1,600 ($2,000 if single or head of household)" for the § 63(f)
	// addition — so both are checked directly rather than derived.
	//
	// An earlier version of this test read Chart A instead, which looks like
	// the same numbers and is not: Chart A is the gross-income threshold for
	// having to file at all. It agrees for four statuses and puts married
	// filing separately at $5.
	t.Run("standard deduction matches the printed worksheet", func(t *testing.T) {
		printed := readExtractFile(t, "i1040-2025-standard-deduction.txt")

		for _, status := range []ustaxcalc.FilingStatus{
			ustaxcalc.Single, ustaxcalc.MarriedFilingJointly, ustaxcalc.MarriedFilingSeparately,
			ustaxcalc.HeadOfHousehold, ustaxcalc.SurvivingSpouse,
		} {
			table, err := ustaxcalc.TableFor(2025, status)
			if err != nil {
				t.Fatalf("TableFor(2025, %s): %v", status, err)
			}
			if !strings.Contains(printed, "$"+commaDollars(table.StandardDeduction)) {
				t.Errorf("%s standard deduction is %s, which the worksheet does not print",
					status, table.StandardDeduction)
			}
			if !strings.Contains(printed, "$"+commaDollars(table.AdditionalAgedBlind)) {
				t.Errorf("%s § 63(f) addition is %s, which the worksheet does not print",
					status, table.AdditionalAgedBlind)
			}
		}

		// The worksheet gives the larger addition to exactly single and head of
		// household. Checking that the two amounts appear is not enough on its
		// own: it would pass with every status given the same one.
		for _, tc := range []struct {
			status ustaxcalc.FilingStatus
			want   ustaxcalc.USCents
		}{
			{ustaxcalc.Single, ustaxcalc.Dollars(2_000)},
			{ustaxcalc.HeadOfHousehold, ustaxcalc.Dollars(2_000)},
			{ustaxcalc.MarriedFilingJointly, ustaxcalc.Dollars(1_600)},
			{ustaxcalc.MarriedFilingSeparately, ustaxcalc.Dollars(1_600)},
			{ustaxcalc.SurvivingSpouse, ustaxcalc.Dollars(1_600)},
		} {
			table, err := ustaxcalc.TableFor(2025, tc.status)
			if err != nil {
				t.Fatal(err)
			}
			if table.AdditionalAgedBlind != tc.want {
				t.Errorf("%s § 63(f) addition is %s, the worksheet says %s",
					tc.status, table.AdditionalAgedBlind, tc.want)
			}
		}

		// The wage base is read off the printed Schedule SE, appended to the
		// same extract.
		table, err := ustaxcalc.TableFor(2025, ustaxcalc.Single)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(printed, "$"+commaDollars(table.SocialSecurityWageBase)) {
			t.Errorf("wage base %s does not appear on the printed Schedule SE", table.SocialSecurityWageBase)
		}
	})
}

// commaDollars renders a whole-dollar amount the way the forms print it.
func commaDollars(a ustaxcalc.USCents) string {
	whole := int64(a) / 100
	s := strconv.FormatInt(whole, 10)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// TestUnknownYearIsAnError is the behaviour that stops a stale build applying
// one year's law to another year's income.
func TestUnknownYearIsAnError(t *testing.T) {
	for _, year := range []int{2024, 2027, 1998} {
		if _, err := ustaxcalc.TableFor(year, ustaxcalc.Single); !errors.Is(err, ustaxcalc.ErrNoTable) {
			t.Errorf("TableFor(%d) gave %v, want ErrNoTable", year, err)
		}
	}
	if _, err := ustaxcalc.OrdinaryIncomeTax(2024, ustaxcalc.Single, ustaxcalc.Dollars(50_000)); !errors.Is(err, ustaxcalc.ErrNoTable) {
		t.Errorf("OrdinaryIncomeTax for an unheld year gave %v, want ErrNoTable", err)
	}
}

// TestUnspecifiedStatusIsAnError checks that a caller who forgot to set a
// status gets an error rather than a single filer's tax.
func TestUnspecifiedStatusIsAnError(t *testing.T) {
	if _, err := ustaxcalc.TableFor(2026, ustaxcalc.FilingStatusUnspecified); err == nil {
		t.Fatal("TableFor with no filing status succeeded, want an error")
	}
}

// TestBracketsAscend guards the table itself: a threshold out of order would
// silently mis-tax a band rather than fail.
func TestBracketsAscend(t *testing.T) {
	for _, year := range ustaxcalc.Years() {
		for _, status := range allStatuses() {
			table, err := ustaxcalc.TableFor(year, status)
			if err != nil {
				t.Fatalf("TableFor(%d, %s): %v", year, status, err)
			}
			if table.Ordinary[0].Over != 0 {
				t.Errorf("%d %s starts over %s, want 0", year, status, table.Ordinary[0].Over)
			}
			for i := 1; i < len(table.Ordinary); i++ {
				if table.Ordinary[i].Over <= table.Ordinary[i-1].Over {
					t.Errorf("%d %s bracket %d starts at %s, not above %s",
						year, status, i+1, table.Ordinary[i].Over, table.Ordinary[i-1].Over)
				}
				if table.Ordinary[i].Rate <= table.Ordinary[i-1].Rate {
					t.Errorf("%d %s bracket %d is %s, not above %s",
						year, status, i+1, table.Ordinary[i].Rate, table.Ordinary[i-1].Rate)
				}
			}
			if table.MaxZeroRateAmount >= table.Max15RateAmount {
				t.Errorf("%d %s zero rate amount %s is not below the 15%% amount %s",
					year, status, table.MaxZeroRateAmount, table.Max15RateAmount)
			}
		}
	}
}

func allStatuses() []ustaxcalc.FilingStatus {
	return []ustaxcalc.FilingStatus{
		ustaxcalc.Single,
		ustaxcalc.MarriedFilingJointly,
		ustaxcalc.MarriedFilingSeparately,
		ustaxcalc.HeadOfHousehold,
		ustaxcalc.SurvivingSpouse,
	}
}

func readExtractFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

func readExtract(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("testdata/rp-2025-32-extract.txt")
	if err != nil {
		t.Fatalf("reading the revenue procedure extract: %v", err)
	}
	return string(b)
}

var amountPattern = regexp.MustCompile(`\$([\d,]+(?:\.\d+)?)`)

// parseAmount turns "$105,700" or "$206,583.50" into a USCents.
func parseAmount(t *testing.T, s string) ustaxcalc.USCents {
	t.Helper()
	s = strings.ReplaceAll(strings.TrimPrefix(s, "$"), ",", "")
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		t.Fatalf("parsing the amount %q out of the revenue procedure: %v", s, err)
	}
	return ustaxcalc.USCents(int64(f*100 + 0.5))
}

// overAtLineStart matches the threshold a bracket begins above.
//
// Anchored at the start of a line and case sensitive, because those are the two
// things that tell a threshold apart from the other amounts in the same row. A
// row reads "Over $24,800 but not over / $100,800 ... $2,480 plus 12% of the
// excess / over $24,800": the capitalised "Over" at the line start is the
// threshold, the lower-case "over" is the same figure repeated inside the rate
// sentence, and $2,480 is the tax on the bands below.
//
// The tables do not wrap the same way as each other. TABLE 3 puts "not over
// $50,400" on one line while TABLE 1 breaks between "not over" and its amount,
// so matching on "not over" reads the base amount out of one table and the
// threshold out of another.
var overAtLineStart = regexp.MustCompile(`(?m)^[ \t]*Over[ \t]+\$([\d,]+)`)

// thresholdsUnder reads the bracket thresholds out of one numbered table.
func thresholdsUnder(t *testing.T, extract, heading string) []ustaxcalc.USCents {
	t.Helper()
	start := strings.Index(extract, heading)
	if start < 0 {
		t.Fatalf("the extract has no %s", heading)
	}
	rest := extract[start+len(heading):]
	// Stop at the next table, so one table's rows cannot be read into another.
	if end := strings.Index(rest, "TABLE "); end >= 0 {
		rest = rest[:end]
	}

	var out []ustaxcalc.USCents
	for _, m := range overAtLineStart.FindAllStringSubmatch(rest, -1) {
		out = append(out, parseAmount(t, m[1]))
	}
	if len(out) == 0 {
		t.Fatalf("found no thresholds under %s", heading)
	}
	return out
}

// standardDeductionSection narrows the extract to § 3.14(1), whose row headings
// repeat the filing status names used by the capital gains table in § 3.03.
// Searching the whole extract finds whichever comes first, which is the wrong
// one.
func standardDeductionSection(t *testing.T, extract string) string {
	t.Helper()
	start := strings.Index(extract, "amounts under § 63(c)(2) are as follows")
	if start < 0 {
		t.Fatal("the extract no longer has the § 63(c)(2) standard deduction table")
	}
	section := extract[start:]
	if end := strings.Index(section, "(2) Dependent"); end >= 0 {
		section = section[:end]
	}
	return section
}

// amountOnLineAfter returns the first dollar amount on the line containing
// marker, or on the line after it where the row wrapped.
func amountOnLineAfter(t *testing.T, extract, marker string) ustaxcalc.USCents {
	t.Helper()
	lines := strings.Split(extract, "\n")
	for i, line := range lines {
		if !strings.Contains(line, marker) {
			continue
		}
		for _, candidate := range lines[i:min(i+3, len(lines))] {
			if m := amountPattern.FindStringSubmatch(candidate); m != nil {
				return parseAmount(t, m[1])
			}
		}
	}
	t.Fatalf("the extract has no amount for %q", marker)
	return 0
}

// twoAmountsOnLine returns the two dollar amounts on the capital gains row for
// one filing status.
func twoAmountsOnLine(t *testing.T, extract, marker string) (zero, fifteen ustaxcalc.USCents) {
	t.Helper()
	// Read only the § 3.03 table, so the standard deduction rows with similar
	// headings cannot be matched instead.
	start := strings.Index(extract, "Maximum Zero")
	if start < 0 {
		t.Fatal("the extract has no capital gains table")
	}
	section := extract[start:]
	if end := strings.Index(section, ".04 Adoption Credit"); end >= 0 {
		section = section[:end]
	}

	for _, line := range strings.Split(section, "\n") {
		if !strings.Contains(line, marker) {
			continue
		}
		m := amountPattern.FindAllStringSubmatch(line, -1)
		if len(m) < 2 {
			continue
		}
		return parseAmount(t, m[0][1]), parseAmount(t, m[1][1])
	}
	t.Fatal(fmt.Sprintf("the capital gains table has no row with two amounts for %q", marker))
	return 0, 0
}
