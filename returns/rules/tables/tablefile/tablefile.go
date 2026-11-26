// Package tablefile reads the tax tables and rate schedules under
// sources/tables/, checks each against the PDF it was taken from, and keeps
// the known gaps the tables tests allow.
//
// Each file is a TSV with a header of "# key: value" lines:
//
//	# source: <catalog source id>
//	# pdf: <path under sources/pdfs>
//	# sha256: <sha256 of that PDF>
//	# pages: <PDF pages, counting from 1>
//	# kind: table | schedule
//	# note: <one line; repeatable>
//
// A table's columns are at_least, less_than and one per filing status, in
// whole dollars. A schedule's columns are status, over, but_not_over,
// base_tax, rate and of_excess_over; but_not_over is empty for the top
// bracket.
package tablefile

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Header is what a file says about where its figures came from.
type Header struct {
	Source string
	PDF    string
	SHA256 string
	Pages  string
	Kind   string
	Notes  []string
}

// CheckPDF fails unless the PDF the header names, under pdfRoot, has the
// SHA-256 the header records.
func (h Header) CheckPDF(pdfRoot string) error {
	raw, err := os.ReadFile(filepath.Join(pdfRoot, h.PDF))
	if err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	if got := hex.EncodeToString(sum[:]); got != h.SHA256 {
		return fmt.Errorf("%s has sha256 %s, but the table was extracted from %s; extract it again", h.PDF, got, h.SHA256)
	}
	return nil
}

// Table is a printed tax table.
type Table struct {
	Header
	// Statuses are the filing status columns, in file order.
	Statuses []string
	Rows     []Row
}

// Row is the tax, in whole dollars, for taxable income of at least AtLeast
// and less than LessThan, by filing status.
type Row struct {
	AtLeast  int64
	LessThan int64
	Tax      map[string]int64
}

// Schedule is a rate schedule: brackets by filing status.
type Schedule struct {
	Header
	Brackets []Bracket
}

// Bracket is one line of a rate schedule.
type Bracket struct {
	Status string
	Over   int64
	// ButNotOver is the top of the bracket; Top is set instead for the
	// highest bracket, which has none.
	ButNotOver   int64
	Top          bool
	BaseTax      *big.Rat
	Rate         *big.Rat
	OfExcessOver int64
}

// TaxCents is base_tax + rate × (income − of_excess_over), with the product
// rounded to the cent, half away from zero, before it is added.
func (b Bracket) TaxCents(income int64) int64 {
	excess := new(big.Rat).SetInt64(income - b.OfExcessOver)
	product := new(big.Rat).Mul(excess, b.Rate)
	return ratCents(product) + ratCents(b.BaseTax)
}

// For returns the bracket for the status that holds the income: over it and
// not over its top. The lowest bracket also holds income equal to its over,
// for a schedule such as the federal Tax Computation Worksheet whose first
// line reads "at least".
func (s *Schedule) For(status string, income int64) (Bracket, bool) {
	first := true
	for _, b := range s.Brackets {
		if b.Status != status {
			continue
		}
		above := income > b.Over || (first && income == b.Over)
		first = false
		if above && (b.Top || income <= b.ButNotOver) {
			return b, true
		}
	}
	return Bracket{}, false
}

// Statuses returns the filing statuses the schedule covers, in file order.
func (s *Schedule) Statuses() []string {
	var out []string
	seen := map[string]bool{}
	for _, b := range s.Brackets {
		if !seen[b.Status] {
			seen[b.Status] = true
			out = append(out, b.Status)
		}
	}
	return out
}

// RoundDollars rounds cents to whole dollars, half away from zero, as the
// forms round an amount of 50 cents or more up.
func RoundDollars(cents int64) int64 {
	if cents < 0 {
		return -RoundDollars(-cents)
	}
	return (cents + 50) / 100
}

func ratCents(r *big.Rat) int64 {
	c := new(big.Rat).Mul(r, big.NewRat(100, 1))
	num, den := c.Num(), c.Denom()
	q, m := new(big.Int).QuoRem(num, den, new(big.Int))
	// Round half away from zero.
	if new(big.Int).Mul(new(big.Int).Abs(m), big.NewInt(2)).Cmp(den) >= 0 {
		if num.Sign() < 0 {
			q.Sub(q, big.NewInt(1))
		} else {
			q.Add(q, big.NewInt(1))
		}
	}
	return q.Int64()
}

// ReadTable reads a file of kind table.
func ReadTable(path string) (*Table, error) {
	h, cols, rows, err := read(path, "table")
	if err != nil {
		return nil, err
	}
	if len(cols) < 3 || cols[0] != "at_least" || cols[1] != "less_than" {
		return nil, fmt.Errorf("%s: columns %v, want at_least, less_than and filing statuses", path, cols)
	}
	t := &Table{Header: h, Statuses: cols[2:]}
	for _, st := range t.Statuses {
		if !validStatus(st) {
			return nil, fmt.Errorf("%s: unknown filing status column %q", path, st)
		}
	}
	for _, f := range rows {
		n, err := ints(f.fields)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %v", path, f.line, err)
		}
		r := Row{AtLeast: n[0], LessThan: n[1], Tax: map[string]int64{}}
		for i, st := range t.Statuses {
			r.Tax[st] = n[2+i]
		}
		if r.LessThan <= r.AtLeast {
			return nil, fmt.Errorf("%s:%d: row ends at or below its start", path, f.line)
		}
		if k := len(t.Rows); k > 0 && t.Rows[k-1].LessThan != r.AtLeast {
			return nil, fmt.Errorf("%s:%d: row at %d follows a row ending at %d", path, f.line, r.AtLeast, t.Rows[k-1].LessThan)
		}
		t.Rows = append(t.Rows, r)
	}
	if len(t.Rows) == 0 {
		return nil, fmt.Errorf("%s: no rows", path)
	}
	return t, nil
}

// ReadSchedule reads a file of kind schedule.
func ReadSchedule(path string) (*Schedule, error) {
	h, cols, rows, err := read(path, "schedule")
	if err != nil {
		return nil, err
	}
	want := []string{"status", "over", "but_not_over", "base_tax", "rate", "of_excess_over"}
	if strings.Join(cols, ",") != strings.Join(want, ",") {
		return nil, fmt.Errorf("%s: columns %v, want %v", path, cols, want)
	}
	s := &Schedule{Header: h}
	for _, f := range rows {
		v := f.fields
		where := fmt.Sprintf("%s:%d", path, f.line)
		if !validStatus(v[0]) {
			return nil, fmt.Errorf("%s: unknown filing status %q", where, v[0])
		}
		b := Bracket{Status: v[0]}
		if b.Over, err = strconv.ParseInt(v[1], 10, 64); err != nil {
			return nil, fmt.Errorf("%s: over: %v", where, err)
		}
		if v[2] == "" {
			b.Top = true
		} else if b.ButNotOver, err = strconv.ParseInt(v[2], 10, 64); err != nil {
			return nil, fmt.Errorf("%s: but_not_over: %v", where, err)
		}
		var ok bool
		if b.BaseTax, ok = new(big.Rat).SetString(v[3]); !ok {
			return nil, fmt.Errorf("%s: base_tax %q", where, v[3])
		}
		if b.Rate, ok = new(big.Rat).SetString(v[4]); !ok {
			return nil, fmt.Errorf("%s: rate %q", where, v[4])
		}
		if b.OfExcessOver, err = strconv.ParseInt(v[5], 10, 64); err != nil {
			return nil, fmt.Errorf("%s: of_excess_over: %v", where, err)
		}
		if k := len(s.Brackets); k > 0 {
			prev := s.Brackets[k-1]
			if prev.Status == b.Status && (prev.Top || prev.ButNotOver != b.Over) {
				return nil, fmt.Errorf("%s: bracket over %d does not follow the one before", where, b.Over)
			}
		}
		s.Brackets = append(s.Brackets, b)
	}
	if len(s.Brackets) == 0 {
		return nil, fmt.Errorf("%s: no brackets", path)
	}
	return s, nil
}

type record struct {
	line   int
	fields []string
}

// read splits a file into its header, its column names and its rows, each
// row as wide as the column names.
func read(path, kind string) (Header, []string, []record, error) {
	file, err := os.Open(path)
	if err != nil {
		return Header{}, nil, nil, err
	}
	defer file.Close()
	var (
		h    Header
		cols []string
		rows []record
	)
	sc := bufio.NewScanner(file)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			if cols != nil {
				return h, nil, nil, fmt.Errorf("%s:%d: header line after the column names", path, n)
			}
			key, val, ok := strings.Cut(strings.TrimSpace(strings.TrimPrefix(line, "#")), ":")
			if !ok {
				return h, nil, nil, fmt.Errorf("%s:%d: header line without a key", path, n)
			}
			val = strings.TrimSpace(val)
			switch key {
			case "source":
				h.Source = val
			case "pdf":
				h.PDF = val
			case "sha256":
				h.SHA256 = val
			case "pages":
				h.Pages = val
			case "kind":
				h.Kind = val
			case "note":
				h.Notes = append(h.Notes, val)
			default:
				return h, nil, nil, fmt.Errorf("%s:%d: unknown header key %q", path, n, key)
			}
			continue
		}
		fields := strings.Split(line, "\t")
		if cols == nil {
			cols = fields
			continue
		}
		if len(fields) != len(cols) {
			return h, nil, nil, fmt.Errorf("%s:%d: %d fields, want %d", path, n, len(fields), len(cols))
		}
		rows = append(rows, record{line: n, fields: fields})
	}
	if err := sc.Err(); err != nil {
		return h, nil, nil, err
	}
	if h.Source == "" || h.PDF == "" || h.SHA256 == "" || h.Pages == "" {
		return h, nil, nil, fmt.Errorf("%s: header needs source, pdf, sha256 and pages", path)
	}
	if h.Kind != kind {
		return h, nil, nil, fmt.Errorf("%s: kind %q, want %q", path, h.Kind, kind)
	}
	return h, cols, rows, nil
}

func ints(fields []string) ([]int64, error) {
	out := make([]int64, len(fields))
	for i, f := range fields {
		n, err := strconv.ParseInt(f, 10, 64)
		if err != nil {
			return nil, err
		}
		out[i] = n
	}
	return out, nil
}

var statuses = []string{"single", "mfj", "mfs", "hoh", "qss"}

func validStatus(s string) bool {
	for _, st := range statuses {
		if s == st {
			return true
		}
	}
	return false
}
