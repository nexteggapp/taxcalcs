package ustaxcalc_test

import (
	"bufio"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/nexteggapp/taxcalcs/ustaxcalc"
)

// The IRS Tax Table is the closest thing to an exhaustive oracle these
// calculators have.
//
// Every other test here checks a figure somebody chose: a bracket boundary, a
// round number, a case that once broke. The Tax Table is the IRS's own answer
// for every $50 band of taxable income up to $100,000, across four filing
// statuses — around eight thousand cases nobody selected, which is what makes
// it worth more than the same count of hand-written ones.
//
// testdata/i1040-2025-tax-table.txt is pdftotext -layout over the 2025 Form
// 1040 instructions, bounded by the document's own headings and filtered to
// rows that begin with a band. Income bands sit side by side on each printed
// row, each with four statuses. See testdata/SOURCES.md for where the PDF came
// from and how to reproduce the extract.
//
// Scraping a PDF makes assumptions, and the one that matters is that the
// extract is the whole table. assertTableIsComplete checks that rather than
// trusting it.
//
// # Why the midpoint
//
// The table gives one figure per $50 band, and the IRS computes it at the
// band's midpoint: a filer with taxable income anywhere in $50,000 to $50,050
// pays the tax on $50,025. So the comparison is against the midpoint and not
// against either edge, and a calculator that is exact does not round to the
// table by accident.
//
// # What it does not check
//
// Everything above $100,000, which the table does not cover and the Tax
// Computation Worksheet handles instead. And the table is whole dollars, so an
// error smaller than half a cent of rounding is invisible to it.

const taxTableFile = "testdata/i1040-2025-tax-table.txt"

// tableStatuses is the column order the printed table uses, left to right.
var tableStatuses = []ustaxcalc.FilingStatus{
	ustaxcalc.Single,
	ustaxcalc.MarriedFilingJointly,
	ustaxcalc.MarriedFilingSeparately,
	ustaxcalc.HeadOfHousehold,
}

type tableCase struct {
	atLeast     ustaxcalc.USCents
	lessThan    ustaxcalc.USCents
	taxByStatus [4]ustaxcalc.USCents
}

// roundToDollar rounds to whole dollars, half up, which is how the printed
// table renders a figure that lands on a half.
func roundToDollar(a ustaxcalc.USCents) ustaxcalc.USCents {
	return (a + 50) / 100 * 100
}

func TestAgainstIRSTaxTable(t *testing.T) {
	cases := readTaxTable(t)

	assertTableIsComplete(t, cases)

	var checked, mismatched int
	for _, c := range cases {
		// The band's midpoint, which is the income the printed figure is for.
		midpoint := (c.atLeast + c.lessThan) / 2

		for i, status := range tableStatuses {
			want := c.taxByStatus[i]

			got, err := ustaxcalc.OrdinaryIncomeTax(2025, status, midpoint)
			if err != nil {
				t.Fatalf("OrdinaryIncomeTax(2025, %v, %v): %v", status, midpoint, err)
			}
			checked++

			// The printed figure is this calculation rounded to whole
			// dollars, half up — $302.50 is printed as $303. Rounding and
			// comparing exactly is a stronger claim than allowing fifty cents
			// of slack, which would also accept a genuine forty-nine cent
			// error in either direction.
			if roundToDollar(got.Value().Total) != want {
				mismatched++
				if mismatched <= 10 {
					t.Errorf("taxable income %v, %v: got %v (rounds to %v), table says %v",
						midpoint, status, got.Value().Total, roundToDollar(got.Value().Total), want)
				}
			}
		}
	}

	if mismatched > 10 {
		t.Errorf("... and %d further mismatches", mismatched-10)
	}
	t.Logf("checked %d cases from the 2025 IRS Tax Table across %d bands", checked, len(cases))
}

// assertTableIsComplete checks that the extract covers the whole printed table
// with no holes and no repeats.
//
// This is the check that makes scraping a PDF defensible rather than hopeful.
// Text extraction drops things quietly, and a count does not notice: an earlier
// version asserted only "at least 1,900 bands" and passed while missing eighty
// of them between $94,000 and $99,000 — two whole page columns, in the highest
// income region the table covers, lost because the last page has a short third
// column and the filter required a full row.
//
// The printed table runs from $0 to $100,000 without a break, so every band's
// floor must be the previous band's ceiling. That catches a dropped column, a
// truncated page, and a repeated row, none of which a total would show.
func assertTableIsComplete(t *testing.T, cases []tableCase) {
	t.Helper()

	if len(cases) == 0 {
		t.Fatalf("parsed nothing from %s", taxTableFile)
	}

	sorted := append([]tableCase(nil), cases...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].atLeast < sorted[j].atLeast })

	if sorted[0].atLeast != 0 {
		t.Errorf("the extract starts at %v, the printed table starts at $0", sorted[0].atLeast)
	}
	if last := sorted[len(sorted)-1].lessThan; last != ustaxcalc.Dollars(100_000) {
		t.Errorf("the extract ends at %v, the printed table ends at $100,000", last)
	}

	for i := 1; i < len(sorted); i++ {
		prev, cur := sorted[i-1], sorted[i]
		if cur.atLeast == prev.lessThan {
			continue
		}
		if cur.atLeast == knownGapFrom && prev.lessThan == knownGapTo {
			continue
		}
		if cur.atLeast < prev.lessThan {
			// Two bands claiming the same income. The Sample Table the
			// instructions print to show you how to read the table is a real
			// row with real figures, so it passes every value check and shows
			// up only here.
			t.Errorf("bands overlap: %v–%v and %v–%v both cover income in that range",
				prev.atLeast, prev.lessThan, cur.atLeast, cur.lessThan)
			continue
		}
		t.Errorf("hole in the extract: nothing covers %v to %v", prev.lessThan, cur.atLeast)
	}
}

// The one part of the table that does not survive extraction.
//
// Below $25 the printed rows are narrow enough that pdftotext breaks each into
// separate column fragments, and reassembling them would need a parser that
// understands the page layout rather than one that reads whitespace. The two
// bands lost are $5–$15 and $15–$25, where the tax is $1 and $2.
//
// Named here rather than tolerated by a fuzzy threshold, so that it is the only
// gap allowed and any other one fails.
const (
	knownGapTo   = 5 * ustaxcalc.Dollar
	knownGapFrom = 25 * ustaxcalc.Dollar
)

// readTaxTable parses the extract.
//
// Each printed row holds three independent income bands, so a row yields three
// cases. A row that does not parse cleanly is skipped rather than failed: the
// extract is text scraped from a PDF and the page furniture between column
// blocks does not always come through, which is why the count is asserted above
// instead.
func readTaxTable(t *testing.T) []tableCase {
	t.Helper()

	f, err := os.Open(taxTableFile)
	if err != nil {
		t.Fatalf("open %s: %v", taxTableFile, err)
	}
	defer f.Close()

	var out []tableCase
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		fields := strings.Fields(scan.Text())

		// Blocks of six — at least, but less than, and four statuses — laid
		// side by side. Usually three of them, but the table's last page has a
		// short third column reading "or over", so take however many complete
		// blocks the row has rather than requiring a full row. Requiring one
		// silently discarded eighty bands between $94,000 and $99,000.
		for b := 0; b < len(fields)/6; b++ {
			var nums [6]ustaxcalc.USCents
			ok := true
			for i := 0; i < 6; i++ {
				v, err := parseDollars(fields[b*6+i])
				if err != nil {
					ok = false
					break
				}
				nums[i] = v
			}
			if !ok {
				continue
			}
			// A band always runs upward. Anything else is the parser having
			// found six numbers that are not a row.
			if nums[1] <= nums[0] {
				continue
			}
			out = append(out, tableCase{
				atLeast:     nums[0],
				lessThan:    nums[1],
				taxByStatus: [4]ustaxcalc.USCents{nums[2], nums[3], nums[4], nums[5]},
			})
		}
	}
	if err := scan.Err(); err != nil {
		t.Fatalf("read %s: %v", taxTableFile, err)
	}
	return out
}

func parseDollars(s string) (ustaxcalc.USCents, error) {
	n, err := strconv.ParseInt(strings.ReplaceAll(s, ",", ""), 10, 64)
	if err != nil {
		return 0, err
	}
	return ustaxcalc.Dollars(n), nil
}
