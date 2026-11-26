package tablefile

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

var gapsOut = flag.String("gaps_out", "", "write every mismatch found to this file, in the per-probe known_gaps form")

// Gap is one probe at which the rules give a different figure from the
// printed one, recorded while an issue about it is open.
type Gap struct {
	// Table is the TSV's file name.
	Table string
	// Row is the row's at_least, or the bracket's over.
	Row    int64
	Income int64
	Status string
	// Expected and Got are in cents.
	Expected int64
	Got      int64
	Issue    string
}

type gapKey struct {
	table  string
	income int64
	status string
}

func (g Gap) key() gapKey { return gapKey{g.Table, g.Income, g.Status} }

// RangeGap is a known gap covering every probe of a table in an income
// range, for one status or all of them.
type RangeGap struct {
	Table string
	// Status is a filing status, or "*" for all of them.
	Status string
	// All is set when the range is "*": every probe of the table.
	All      bool
	AtLeast  int64
	LessThan int64
	Issue    string
	Reason   string
	line     int
}

func (r RangeGap) covers(table, status string, income int64) bool {
	return r.Table == table &&
		(r.Status == "*" || r.Status == status) &&
		(r.All || (income >= r.AtLeast && income < r.LessThan))
}

func (r RangeGap) String() string {
	rng := "*"
	if !r.All {
		rng = fmt.Sprintf("%d..%d", r.AtLeast, r.LessThan)
	}
	return fmt.Sprintf("%s %s %s (%s)", r.Table, r.Status, rng, r.Issue)
}

const gapsHeader = `# Probes at which the rules give a different figure from the printed one.
# Each stays listed while its issue is open. The test fails if a listed probe
# starts to match, or gives a figure other than the one listed.
# Amounts are in dollars and cents.
table	row	income	status	expected	got	issue
`

// Checker compares the rules' figures with the printed ones, allowing the
// gaps listed in a known_gaps file. It is safe for parallel subtests.
type Checker struct {
	path   string
	mu     sync.Mutex
	known  map[gapKey]Gap
	seen   map[gapKey]bool
	ranges []RangeGap
	hits   []int // probes each range covered
	found  []Gap
}

// NewChecker reads the known gaps at path. A missing file lists none.
func NewChecker(path string) (*Checker, error) {
	c := &Checker{path: path, known: map[gapKey]Gap{}, seen: map[gapKey]bool{}}
	file, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	sc := bufio.NewScanner(file)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if f[0] == "table" {
			continue
		}
		switch len(f) {
		case 5:
			r, err := parseRange(f)
			if err != nil {
				return nil, fmt.Errorf("%s:%d: %v", path, n, err)
			}
			r.line = n
			c.ranges = append(c.ranges, r)
		case 7:
			g, err := parseProbe(f)
			if err != nil {
				return nil, fmt.Errorf("%s:%d: %v", path, n, err)
			}
			if _, dup := c.known[g.key()]; dup {
				return nil, fmt.Errorf("%s:%d: listed twice", path, n)
			}
			c.known[g.key()] = g
		default:
			return nil, fmt.Errorf("%s:%d: %d fields, want 5 for a range or 7 for one probe", path, n, len(f))
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	c.hits = make([]int, len(c.ranges))
	return c, nil
}

func parseRange(f []string) (RangeGap, error) {
	r := RangeGap{Table: f[0], Status: f[1], Issue: f[3], Reason: f[4]}
	if r.Status != "*" && !validStatus(r.Status) {
		return r, fmt.Errorf("unknown filing status %q", r.Status)
	}
	if f[2] == "*" {
		r.All = true
	} else {
		lo, hi, ok := strings.Cut(f[2], "..")
		var err1, err2 error
		r.AtLeast, err1 = strconv.ParseInt(lo, 10, 64)
		r.LessThan, err2 = strconv.ParseInt(hi, 10, 64)
		if !ok || err1 != nil || err2 != nil || r.LessThan <= r.AtLeast {
			return r, fmt.Errorf("range %q: want at_least..less_than or *", f[2])
		}
	}
	if r.Issue == "" || r.Reason == "" {
		return r, fmt.Errorf("a range gap needs an issue URL and a reason")
	}
	return r, nil
}

func parseProbe(f []string) (Gap, error) {
	g := Gap{Table: f[0], Status: f[3], Issue: f[6]}
	var errs []error
	var err error
	g.Row, err = strconv.ParseInt(f[1], 10, 64)
	errs = append(errs, err)
	g.Income, err = strconv.ParseInt(f[2], 10, 64)
	errs = append(errs, err)
	g.Expected, err = parseCents(f[4])
	errs = append(errs, err)
	g.Got, err = parseCents(f[5])
	errs = append(errs, err)
	if err := errors.Join(errs...); err != nil {
		return g, err
	}
	if g.Issue == "" {
		return g, fmt.Errorf("a known gap needs an issue URL")
	}
	return g, nil
}

// Check reports a test error unless the rules' figure, got, is the printed
// one, want, or the mismatch is covered by a known gap: a range gap, or a
// per-probe gap listing that same figure. A probe a gap covers that now
// matches is an error too, so the list is kept exact.
func (c *Checker) Check(t testing.TB, table string, row, income int64, status string, want, got int64) {
	t.Helper()
	g := Gap{Table: table, Row: row, Income: income, Status: status, Expected: want, Got: got}
	c.mu.Lock()
	known, listed := c.known[g.key()]
	c.seen[g.key()] = true
	var inRanges []RangeGap
	for i, r := range c.ranges {
		if r.covers(table, status, income) {
			c.hits[i]++
			inRanges = append(inRanges, r)
		}
	}
	if want != got {
		g.Issue = known.Issue
		if g.Issue == "" && len(inRanges) > 0 {
			g.Issue = inRanges[0].Issue
		}
		c.found = append(c.found, g)
	}
	c.mu.Unlock()
	switch {
	case want == got && listed:
		t.Errorf("%s row %d, income %d, %s: now gives the printed %s; remove the known gap (%s)",
			table, row, income, status, formatCents(want), known.Issue)
	case want == got && len(inRanges) > 0:
		t.Errorf("%s row %d, income %d, %s: now gives the printed %s, inside the known gap %v; split or remove it",
			table, row, income, status, formatCents(want), inRanges[0])
	case want != got && listed && (known.Got != got || known.Expected != want):
		t.Errorf("%s row %d, income %d, %s: rules give %s and the PDF prints %s; the known gap lists %s and %s",
			table, row, income, status, formatCents(got), formatCents(want), formatCents(known.Got), formatCents(known.Expected))
	case want != got && !listed && len(inRanges) == 0:
		t.Errorf("%s row %d, income %d, %s: rules give %s, the PDF prints %s",
			table, row, income, status, formatCents(got), formatCents(want))
	}
}

// Finish reports known gaps that no probe reached, and with -gaps_out
// writes every mismatch found as per-probe gaps, keeping the issue of each
// one already covered. Call it after every Check.
func (c *Checker) Finish(t testing.TB) {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, g := range c.known {
		if !c.seen[k] {
			t.Errorf("%s: known gap at %s row %d, income %d, %s is no longer probed", c.path, g.Table, g.Row, g.Income, g.Status)
		}
	}
	for i, r := range c.ranges {
		if c.hits[i] == 0 {
			t.Errorf("%s:%d: known gap %v covers no probe", c.path, r.line, r)
		}
	}
	if *gapsOut == "" {
		return
	}
	sort.Slice(c.found, func(i, j int) bool {
		a, b := c.found[i], c.found[j]
		if a.Table != b.Table {
			return a.Table < b.Table
		}
		if a.Income != b.Income {
			return a.Income < b.Income
		}
		return statusOrder(a.Status) < statusOrder(b.Status)
	})
	var sb strings.Builder
	sb.WriteString(gapsHeader)
	for _, g := range c.found {
		fmt.Fprintf(&sb, "%s\t%d\t%d\t%s\t%s\t%s\t%s\n", g.Table, g.Row, g.Income, g.Status,
			formatCents(g.Expected), formatCents(g.Got), g.Issue)
	}
	if err := os.WriteFile(*gapsOut, []byte(sb.String()), 0o644); err != nil {
		t.Error(err)
	}
}

func statusOrder(s string) int {
	for i, st := range statuses {
		if s == st {
			return i
		}
	}
	return len(statuses)
}

func formatCents(c int64) string {
	sign := ""
	if c < 0 {
		sign, c = "-", -c
	}
	return fmt.Sprintf("%s%d.%02d", sign, c/100, c%100)
}

func parseCents(s string) (int64, error) {
	whole, frac, ok := strings.Cut(s, ".")
	if !ok || len(frac) != 2 {
		return 0, fmt.Errorf("amount %q: want dollars and two-digit cents", s)
	}
	d, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, err
	}
	f, err := strconv.ParseInt(frac, 10, 64)
	if err != nil {
		return 0, err
	}
	if strings.HasPrefix(whole, "-") {
		return d*100 - f, nil
	}
	return d*100 + f, nil
}
