package main

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Bound says how a printed table states the top of each row.
type Bound int

const (
	// LessThan is a table whose rows read "at least X but less than Y".
	LessThan Bound = iota
	// NotOver is a table whose rows read "at least X but not over Y". Its
	// rows are written with less_than = Y + 1, since incomes are whole
	// dollars.
	NotOver
)

// Row is one row of a tax table: the tax for each printed column on taxable
// income of at least AtLeast and less than LessThan, in whole dollars.
type Row struct {
	AtLeast  int64
	LessThan int64
	Tax      []int64
}

// Spec says what to expect of the table on the pages read.
type Spec struct {
	// Columns is the number of tax columns the table prints.
	Columns int
	// Bound says how the table prints the top of a row.
	Bound Bound
	// First is the lowest at_least, and Last the highest less_than, of the
	// whole table once the bound is converted.
	First, Last int64
}

// A number as the tables print it: digits grouped by commas, perhaps after
// a dollar sign.
var numberRE = regexp.MustCompile(`^\$?\d{1,3}(,\d{3})*$`)

func parseNumber(tok string) (int64, bool) {
	if !numberRE.MatchString(tok) {
		return 0, false
	}
	n, err := strconv.ParseInt(strings.NewReplacer("$", "", ",", "").Replace(tok), 10, 64)
	return n, err == nil
}

// numberRuns splits a line into its runs of consecutive numbers. Any other
// word ends a run, so a number in a sentence beside the table stays apart
// from the table's figures. pure reports whether the line held only numbers.
func numberRuns(line string) (runs [][]int64, pure bool) {
	pure = true
	var cur []int64
	for _, tok := range strings.Fields(line) {
		if n, ok := parseNumber(tok); ok {
			cur = append(cur, n)
			continue
		}
		pure = false
		if len(cur) > 0 {
			runs = append(runs, cur)
			cur = nil
		}
	}
	if len(cur) > 0 {
		runs = append(runs, cur)
	}
	return runs, pure
}

// heading reports whether a line holds only the thousands headings the
// federal table prints above each block of rows, such as "3,000 6,000".
func heading(nums []int64) bool {
	if len(nums) == 0 || len(nums) > 3 {
		return false
	}
	for _, n := range nums {
		if n < 1000 || n%1000 != 0 {
			return false
		}
	}
	return true
}

// Extract reads the rows of a tax table from the text of its pages as
// `pdftotext -layout` lays them out.
//
// A row is a run of 2 + spec.Columns numbers on one line; a line may hold
// several rows side by side. The order the rows appear in does not matter:
// they are sorted by at_least and then checked to cover the table from
// spec.First to spec.Last without a gap or overlap. A row printed twice, as
// in a sample table, must agree with itself and is kept once.
//
// pdftotext sometimes lays a row out down the page, one number to a line,
// as it does for the federal table's $5-15 and $15-25 rows. A run of lines
// holding one number each is read as such rows, a column at a time.
func Extract(pages []string, spec Spec) ([]Row, error) {
	width := 2 + spec.Columns
	var rows []Row
	add := func(nums []int64, where string) error {
		if len(nums)%width != 0 {
			return fmt.Errorf("%s: %d numbers, not a multiple of %d", where, len(nums), width)
		}
		for i := 0; i < len(nums); i += width {
			r := Row{AtLeast: nums[i], LessThan: nums[i+1], Tax: append([]int64(nil), nums[i+2:i+width]...)}
			if spec.Bound == NotOver {
				r.LessThan++
			}
			rows = append(rows, r)
		}
		return nil
	}
	for p, page := range pages {
		var column []int64 // a row laid out down the page
		columnStart := 0
		flush := func() error {
			if len(column) == 0 {
				return nil
			}
			defer func() { column = nil }()
			n := len(column) / width
			if len(column)%width != 0 {
				return fmt.Errorf("page %d line %d: %d numbers one to a line, not a multiple of %d", p+1, columnStart, len(column), width)
			}
			nums := make([]int64, 0, len(column))
			for r := 0; r < n; r++ {
				for c := 0; c < width; c++ {
					nums = append(nums, column[c*n+r])
				}
			}
			return add(nums, fmt.Sprintf("page %d line %d", p+1, columnStart))
		}
		for l, line := range strings.Split(page, "\n") {
			where := fmt.Sprintf("page %d line %d", p+1, l+1)
			runs, pure := numberRuns(line)
			if pure && len(runs) == 1 && len(runs[0]) == 1 && !heading(runs[0]) {
				if len(column) == 0 {
					columnStart = l + 1
				}
				column = append(column, runs[0][0])
				continue
			}
			if err := flush(); err != nil {
				return nil, err
			}
			if pure && len(runs) == 1 && heading(runs[0]) {
				continue
			}
			for _, run := range runs {
				if len(run)%width == 0 {
					if err := add(run, where); err != nil {
						return nil, err
					}
				} else if pure {
					return nil, fmt.Errorf("%s: %d numbers, not a multiple of %d", where, len(run), width)
				}
				// A short run on a line with words is a figure in a
				// sentence, not a table row.
			}
		}
		if err := flush(); err != nil {
			return nil, err
		}
	}
	return check(rows, spec)
}

func check(rows []Row, spec Spec) ([]Row, error) {
	if len(rows) == 0 {
		return nil, fmt.Errorf("no rows found")
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].AtLeast < rows[j].AtLeast })
	out := rows[:1]
	for _, r := range rows[1:] {
		last := out[len(out)-1]
		if r.AtLeast == last.AtLeast {
			if !sameRow(r, last) {
				return nil, fmt.Errorf("row at %d printed twice with different figures: %v and %v", r.AtLeast, last, r)
			}
			continue
		}
		out = append(out, r)
	}
	if out[0].AtLeast != spec.First {
		return nil, fmt.Errorf("first row starts at %d, want %d", out[0].AtLeast, spec.First)
	}
	for i, r := range out {
		if r.LessThan <= r.AtLeast {
			return nil, fmt.Errorf("row at %d ends at %d", r.AtLeast, r.LessThan)
		}
		if i == 0 {
			continue
		}
		prev := out[i-1]
		if r.AtLeast != prev.LessThan {
			return nil, fmt.Errorf("row at %d follows a row ending at %d", r.AtLeast, prev.LessThan)
		}
		for c := range r.Tax {
			if r.Tax[c] < prev.Tax[c] {
				return nil, fmt.Errorf("column %d falls from %d to %d at %d", c+1, prev.Tax[c], r.Tax[c], r.AtLeast)
			}
		}
	}
	if end := out[len(out)-1].LessThan; end != spec.Last {
		return nil, fmt.Errorf("last row ends at %d, want %d", end, spec.Last)
	}
	return out, nil
}

func sameRow(a, b Row) bool {
	if a.AtLeast != b.AtLeast || a.LessThan != b.LessThan || len(a.Tax) != len(b.Tax) {
		return false
	}
	for i := range a.Tax {
		if a.Tax[i] != b.Tax[i] {
			return false
		}
	}
	return true
}

// Statuses are the filing status columns a TSV may hold, in the order it
// writes them.
var Statuses = []string{"single", "mfj", "mfs", "hoh", "qss"}

// ParseColumns reads the -columns flag: one entry per printed tax column,
// left to right, each naming the statuses it covers joined by "+", such as
// "single+mfs,mfj+qss,hoh". It returns, for each status in Statuses order
// that some column covers, the index of that column.
func ParseColumns(s string) (names []string, index []int, err error) {
	col := map[string]int{}
	parts := strings.Split(s, ",")
	for i, part := range parts {
		for _, st := range strings.Split(part, "+") {
			st = strings.TrimSpace(st)
			if !validStatus(st) {
				return nil, nil, fmt.Errorf("unknown filing status %q", st)
			}
			if _, dup := col[st]; dup {
				return nil, nil, fmt.Errorf("filing status %q named twice", st)
			}
			col[st] = i
		}
	}
	for _, st := range Statuses {
		if i, ok := col[st]; ok {
			names = append(names, st)
			index = append(index, i)
		}
	}
	return names, index, nil
}

func validStatus(s string) bool {
	for _, st := range Statuses {
		if s == st {
			return true
		}
	}
	return false
}
