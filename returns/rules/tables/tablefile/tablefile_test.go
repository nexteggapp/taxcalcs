package tablefile

import (
	"crypto/sha256"
	"encoding/hex"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func write(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const tableHeader = "# source: s\n# pdf: x.pdf\n# sha256: %s\n# pages: 3-4\n# kind: table\n# note: a note\n"

func TestReadTable(t *testing.T) {
	path := write(t, "t.tsv", strings.Replace(tableHeader, "%s", "abc", 1)+
		"at_least\tless_than\tsingle\tmfj\n0\t5\t0\t0\n5\t15\t1\t2\n")
	tab, err := ReadTable(path)
	if err != nil {
		t.Fatal(err)
	}
	if tab.Source != "s" || tab.PDF != "x.pdf" || tab.Pages != "3-4" || len(tab.Notes) != 1 {
		t.Errorf("header = %+v", tab.Header)
	}
	want := []Row{
		{AtLeast: 0, LessThan: 5, Tax: map[string]int64{"single": 0, "mfj": 0}},
		{AtLeast: 5, LessThan: 15, Tax: map[string]int64{"single": 1, "mfj": 2}},
	}
	if !reflect.DeepEqual(tab.Rows, want) {
		t.Errorf("rows = %+v, want %+v", tab.Rows, want)
	}
}

func TestReadTableRejects(t *testing.T) {
	h := strings.Replace(tableHeader, "%s", "abc", 1)
	for name, body := range map[string]string{
		"gap":            h + "at_least\tless_than\tsingle\n0\t5\t0\n6\t15\t1\n",
		"short row":      h + "at_least\tless_than\tsingle\n0\t5\n",
		"unknown status": h + "at_least\tless_than\twidow\n0\t5\t0\n",
		"wrong kind":     strings.Replace(h, "kind: table", "kind: schedule", 1) + "at_least\tless_than\tsingle\n0\t5\t0\n",
		"no sha256":      strings.Replace(h, "# sha256: abc\n", "", 1) + "at_least\tless_than\tsingle\n0\t5\t0\n",
	} {
		if _, err := ReadTable(write(t, "t.tsv", body)); err == nil {
			t.Errorf("%s: read without error", name)
		}
	}
}

func TestCheckPDF(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.pdf"), []byte("pdf bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("pdf bytes"))
	h := Header{PDF: "x.pdf", SHA256: hex.EncodeToString(sum[:])}
	if err := h.CheckPDF(dir); err != nil {
		t.Error(err)
	}
	h.SHA256 = strings.Repeat("0", 64)
	if err := h.CheckPDF(dir); err == nil {
		t.Error("a different sha256 passed")
	}
}

const scheduleBody = "# source: s\n# pdf: x.pdf\n# sha256: abc\n# pages: 5\n# kind: schedule\n" +
	"status\tover\tbut_not_over\tbase_tax\trate\tof_excess_over\n" +
	"single\t0\t11079\t0.00\t0.01\t0\n" +
	"single\t11079\t26264\t110.79\t0.02\t11079\n" +
	"single\t26264\t\t414.49\t0.04\t26264\n" +
	"mfj\t100000\t206700\t-10172.00\t0.22\t0\n" +
	"mfj\t206700\t\t-14306.00\t0.24\t0\n"

func TestSchedule(t *testing.T) {
	s, err := ReadSchedule(write(t, "s.tsv", scheduleBody))
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Statuses(); !reflect.DeepEqual(got, []string{"single", "mfj"}) {
		t.Errorf("Statuses() = %v", got)
	}
	for _, c := range []struct {
		status string
		income int64
		over   int64
		cents  int64
	}{
		{"single", 0, 0, 0},
		{"single", 11079, 0, 110_79},
		{"single", 11080, 11079, 110_81},
		{"single", 30000, 26264, 414_49 + 149_44},
		// "At least" the first bracket's over, then rate times income less
		// the subtraction amount.
		{"mfj", 100000, 100000, 22000_00 - 10172_00},
		{"mfj", 206701, 206700, 49608_24 - 14306_00},
	} {
		b, ok := s.For(c.status, c.income)
		if !ok {
			t.Errorf("For(%s, %d): none", c.status, c.income)
			continue
		}
		if b.Over != c.over {
			t.Errorf("For(%s, %d).Over = %d, want %d", c.status, c.income, b.Over, c.over)
		}
		if got := b.TaxCents(c.income); got != c.cents {
			t.Errorf("TaxCents(%s, %d) = %d, want %d", c.status, c.income, got, c.cents)
		}
	}
	if _, ok := s.For("mfj", 99999); ok {
		t.Error("For(mfj, 99999) found a bracket below the schedule")
	}
	want := []int64{1, 5539, 11078, 11079, 11080, 18671, 26263, 26264, 26265}
	if got := s.Probes("single", 1); !reflect.DeepEqual(got, want) {
		t.Errorf("Probes(single, 1) = %v, want %v", got, want)
	}
}

func TestTaxCentsRoundsTheProduct(t *testing.T) {
	b := Bracket{BaseTax: ratOf("3201.97"), Rate: ratOf("0.093"), OfExcessOver: 72724}
	// 0.093 × 27,281 = 2,537.133, which rounds to 2,537.13.
	if got := b.TaxCents(100005); got != 3201_97+2537_13 {
		t.Errorf("TaxCents = %d", got)
	}
	b = Bracket{BaseTax: ratOf("0"), Rate: ratOf("0.093"), OfExcessOver: 0}
	// 0.093 × 5 = 0.465, which rounds half away from zero to 0.47.
	if got := b.TaxCents(5); got != 47 {
		t.Errorf("TaxCents = %d, want 47", got)
	}
}

func TestRoundDollars(t *testing.T) {
	for cents, want := range map[int64]int64{0: 0, 49: 0, 50: 1, 476810: 4768, 476850: 4769, -50: -1} {
		if got := RoundDollars(cents); got != want {
			t.Errorf("RoundDollars(%d) = %d, want %d", cents, got, want)
		}
	}
}

func TestChecker(t *testing.T) {
	path := write(t, "known_gaps.tsv", gapsHeader+
		"t.tsv\t0\t1\tsingle\t0.00\t0.01\thttps://example.com/1\n"+
		"t.tsv\t0\t2\tsingle\t0.00\t0.02\thttps://example.com/1\n")
	c, err := NewChecker(path)
	if err != nil {
		t.Fatal(err)
	}
	ft := &fakeT{}
	c.Check(ft, "t.tsv", 0, 1, "single", 0, 1) // listed, same figure
	c.Check(ft, "t.tsv", 0, 3, "single", 0, 0) // matches, not listed
	if len(ft.errors) != 0 {
		t.Errorf("unexpected errors: %v", ft.errors)
	}
	c.Check(ft, "t.tsv", 0, 2, "single", 0, 0) // listed, now matches
	c.Check(ft, "t.tsv", 0, 4, "single", 0, 5) // not listed
	if len(ft.errors) != 2 {
		t.Errorf("errors = %v, want 2", ft.errors)
	}

	ft = &fakeT{}
	c, err = NewChecker(path)
	if err != nil {
		t.Fatal(err)
	}
	c.Check(ft, "t.tsv", 0, 1, "single", 0, 9) // listed with another figure
	c.Finish(ft)                               // the probe at 2 never ran
	if len(ft.errors) != 2 {
		t.Errorf("errors = %v, want 2", ft.errors)
	}
}

func TestMissingGapsFileListsNone(t *testing.T) {
	c, err := NewChecker(filepath.Join(t.TempDir(), "known_gaps.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	ft := &fakeT{}
	c.Check(ft, "t.tsv", 0, 1, "single", 100, 100)
	c.Finish(ft)
	if len(ft.errors) != 0 {
		t.Errorf("errors = %v", ft.errors)
	}
}

func TestParseCents(t *testing.T) {
	for s, want := range map[string]int64{"0.00": 0, "113.21": 11321, "-5086.00": -508600, "-0.50": -50} {
		got, err := parseCents(s)
		if err != nil || got != want {
			t.Errorf("parseCents(%q) = %d, %v; want %d", s, got, err, want)
		}
		if back := formatCents(got); back != s {
			t.Errorf("formatCents(%d) = %q, want %q", got, back, s)
		}
	}
}

// fakeT records errors in place of failing the test.
type fakeT struct {
	testing.TB
	errors []string
}

func (f *fakeT) Helper() {}
func (f *fakeT) Errorf(format string, args ...any) {
	f.errors = append(f.errors, format)
}
func (f *fakeT) Error(args ...any) { f.errors = append(f.errors, "error") }

func ratOf(s string) *big.Rat {
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		panic(s)
	}
	return r
}

const rangeGaps = "table\tstatus\trange\tissue\treason\n" +
	"t.tsv\t*\t100..200\thttps://example.com/1\tuses the schedule, not the table\n" +
	"s.tsv\tsingle\t*\thttps://example.com/2\tkeeps cents\n"

func TestRangeGapCoveringMismatchesPasses(t *testing.T) {
	c, err := NewChecker(write(t, "known_gaps.tsv", rangeGaps))
	if err != nil {
		t.Fatal(err)
	}
	ft := &fakeT{}
	c.Check(ft, "t.tsv", 100, 100, "single", 100, 101)
	c.Check(ft, "t.tsv", 100, 199, "mfj", 100, 99)
	c.Check(ft, "s.tsv", 0, 5_000_000, "single", 100, 101)
	c.Check(ft, "t.tsv", 200, 200, "single", 300, 300) // outside the range, matches
	c.Finish(ft)
	if len(ft.errors) != 0 {
		t.Errorf("errors = %v", ft.errors)
	}
}

func TestRangeGapContainingAPassingProbeFails(t *testing.T) {
	c, err := NewChecker(write(t, "known_gaps.tsv", rangeGaps))
	if err != nil {
		t.Fatal(err)
	}
	ft := &fakeT{}
	c.Check(ft, "t.tsv", 100, 100, "single", 100, 101)
	c.Check(ft, "t.tsv", 150, 150, "hoh", 200, 200) // inside the range, now matches
	c.Check(ft, "s.tsv", 0, 1, "single", 100, 101)
	c.Finish(ft)
	if len(ft.errors) != 1 {
		t.Errorf("errors = %v, want 1", ft.errors)
	}
}

func TestMismatchOutsideEveryRangeFails(t *testing.T) {
	c, err := NewChecker(write(t, "known_gaps.tsv", rangeGaps))
	if err != nil {
		t.Fatal(err)
	}
	ft := &fakeT{}
	c.Check(ft, "t.tsv", 100, 100, "single", 100, 101)
	c.Check(ft, "s.tsv", 0, 1, "single", 100, 101)
	c.Check(ft, "t.tsv", 200, 200, "single", 300, 301) // just above the range
	c.Check(ft, "s.tsv", 0, 1, "mfj", 100, 101)        // another status
	c.Finish(ft)
	if len(ft.errors) != 2 {
		t.Errorf("errors = %v, want 2", ft.errors)
	}
}

func TestRangeGapThatCoversNoProbeFails(t *testing.T) {
	c, err := NewChecker(write(t, "known_gaps.tsv", rangeGaps))
	if err != nil {
		t.Fatal(err)
	}
	ft := &fakeT{}
	c.Check(ft, "t.tsv", 100, 100, "single", 100, 101)
	c.Finish(ft) // s.tsv was never probed
	if len(ft.errors) != 1 {
		t.Errorf("errors = %v, want 1", ft.errors)
	}
}

func TestRangeGapRejects(t *testing.T) {
	for name, line := range map[string]string{
		"bad range":   "t.tsv\t*\t200..100\thttps://example.com/1\tr\n",
		"bad status":  "t.tsv\twidow\t*\thttps://example.com/1\tr\n",
		"no reason":   "t.tsv\t*\t*\thttps://example.com/1\t\n",
		"wrong width": "t.tsv\t*\t*\thttps://example.com/1\n",
	} {
		if _, err := NewChecker(write(t, "known_gaps.tsv", line)); err == nil {
			t.Errorf("%s: read without error", name)
		}
	}
}
