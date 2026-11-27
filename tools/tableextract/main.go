// Command tableextract writes a printed tax table as a TSV file under
// sources/tables/, from the text pdftotext lays out for the table's pages.
//
// pdftotext (from poppler-utils) is not a Bazel dependency, so the tool is
// run by hand, from the repository root. For the federal 2025 Tax Table:
//
//	pdftotext -layout sources/pdfs/us/2025/i1040gi.pdf /tmp/i1040gi.txt
//	go run ./tools/tableextract \
//	    -source us-irs-1040-instructions-2025 -pdf us/2025/i1040gi.pdf \
//	    -pages 68-79 -columns single,mfj+qss,mfs,hoh -bound less-than \
//	    -first 0 -last 100000 \
//	    -note 'The married filing jointly column is also the qualifying surviving spouse column; it is repeated as qss.' \
//	    /tmp/i1040gi.txt > sources/tables/us/2025/tax_table.tsv
//
// California's 2025 table prints "but not over" and three columns:
//
//	pdftotext -layout sources/pdfs/ca/2025/2025-540-booklet.pdf /tmp/ca540.txt
//	go run ./tools/tableextract \
//	    -source ca-ftb-540-booklet-2025 -pdf ca/2025/2025-540-booklet.pdf \
//	    -pages 69-73 -columns single+mfs,mfj+qss,hoh -bound not-over \
//	    -first 1 -last 100001 \
//	    -note 'Column "1 or 3" is single and married/RDP filing separately; column "2 or 5" is married/RDP filing jointly and qualifying surviving spouse/RDP. Each is repeated for both statuses.' \
//	    /tmp/ca540.txt > sources/tables/ca/2025/tax_table.tsv
//
// The text file is the whole document; -pages picks the PDF pages, counting
// from 1, by the form feeds pdftotext puts between pages. -columns names the
// filing statuses each printed tax column covers, left to right; a column
// that covers two statuses is written twice. -bound says whether the table
// prints each row's top as "less than" or "not over"; a "not over" row is
// written with less_than one dollar above it. The tool fails unless the rows
// it reads run from -first to -last without a gap, and each column never
// falls.
//
// The header records the PDF's SHA-256, read from -pdfroot (default
// sources/pdfs), so the tables test fails when the PDF is replaced and the
// table not extracted again.
//
// Some tables print more than the rows on their pages: headings with four
// thousands figures to a line, page numbers, a heading on the same line as
// a row, or a misprint. Two repeatable flags edit the text of the pages
// before it is read, and each must match at least once:
//
//   - -drop REGEXP blanks every match of the Go regular expression in each
//     line.
//   - -replace 'OLD=>NEW' replaces each occurrence of the literal text OLD.
//
// The header notes each edit, so the TSV says how its text was changed.
// -bound over-not-over reads rows printed "over X but not over Y", writing
// at_least X + 1 and less_than Y + 1.
//
// A table published only as a web page is read from the vendored HTML with
// -html ID in place of -pages: the section from the element with that id to
// the next <h2>, each table row laid out on a line. New York's 2025 table:
//
//	go run ./tools/tableextract \
//	    -source ny-dtf-it-201-tax-tables-2025 -pdf ny/2025/2025-it-201-tax-tables.htm \
//	    -html nys -columns single+mfs,mfj+qss,hoh -first 0 -last 65000 \
//	    -note '...' \
//	    sources/pdfs/ny/2025/2025-it-201-tax-tables.htm > sources/tables/ny/2025/tax_table.tsv
//
// Rate schedules are short, and are transcribed by hand; their files say so.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type notes []string

func (n *notes) String() string     { return strings.Join(*n, "; ") }
func (n *notes) Set(s string) error { *n = append(*n, s); return nil }

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "tableextract:", err)
		os.Exit(1)
	}
}

func run(args []string, w io.Writer) error {
	fs := flag.NewFlagSet("tableextract", flag.ContinueOnError)
	source := fs.String("source", "", "catalog source id")
	pdf := fs.String("pdf", "", "the PDF's path under -pdfroot")
	pdfRoot := fs.String("pdfroot", "sources/pdfs", "directory the vendored PDFs are under")
	pages := fs.String("pages", "", "PDF pages the table is on, such as 68-79")
	columns := fs.String("columns", "", "filing statuses of each tax column, such as single+mfs,mfj+qss,hoh")
	bound := fs.String("bound", "less-than", "how rows print their top: less-than or not-over")
	first := fs.Int64("first", 0, "the first row's at_least")
	last := fs.Int64("last", 0, "the last row's less_than")
	htmlSection := fs.String("html", "", "read the input as an HTML page, from the element with this id to the next <h2>")
	var ns, drops, replaces notes
	fs.Var(&ns, "note", "a note line for the header; repeatable")
	fs.Var(&drops, "drop", "a regular expression whose matches are blanked from each line before reading; repeatable")
	fs.Var(&replaces, "replace", "OLD=>NEW: literal text replaced before reading; repeatable")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("want one argument, the pdftotext -layout output or the HTML page")
	}
	if *htmlSection != "" {
		if *pages != "" {
			return fmt.Errorf("-html reads one page; -pages does not apply")
		}
		*pages = "1"
	}
	if *source == "" || *pdf == "" || *pages == "" || *columns == "" {
		return fmt.Errorf("-source, -pdf, -pages and -columns are required")
	}
	from, to, err := parsePages(*pages)
	if err != nil {
		return err
	}
	names, index, err := ParseColumns(*columns)
	if err != nil {
		return err
	}
	spec := Spec{Columns: len(strings.Split(*columns, ",")), First: *first, Last: *last}
	switch *bound {
	case "less-than":
		spec.Bound = LessThan
	case "not-over":
		spec.Bound = NotOver
	case "over-not-over":
		spec.Bound = OverNotOver
	default:
		return fmt.Errorf("-bound %q: want less-than, not-over or over-not-over", *bound)
	}

	text, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return err
	}
	all := strings.Split(string(text), "\f")
	if *htmlSection != "" {
		page, err := HTMLText(string(text), *htmlSection)
		if err != nil {
			return err
		}
		all = []string{page}
	}
	if to > len(all) {
		return fmt.Errorf("-pages %s: the text has %d pages", *pages, len(all))
	}
	pageText, err := edit(all[from-1:to], drops, replaces)
	if err != nil {
		return err
	}
	rows, err := Extract(pageText, spec)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(filepath.Join(*pdfRoot, *pdf))
	if err != nil {
		return err
	}
	sum := sha256.Sum256(raw)

	var b strings.Builder
	fmt.Fprintf(&b, "# source: %s\n", *source)
	fmt.Fprintf(&b, "# pdf: %s\n", *pdf)
	fmt.Fprintf(&b, "# sha256: %s\n", hex.EncodeToString(sum[:]))
	fmt.Fprintf(&b, "# pages: %s\n", *pages)
	fmt.Fprintf(&b, "# kind: table\n")
	switch spec.Bound {
	case NotOver:
		fmt.Fprintf(&b, "# note: The table prints \"at least\" and \"but not over\"; less_than is one dollar above \"but not over\".\n")
	case OverNotOver:
		fmt.Fprintf(&b, "# note: The table prints \"over\" and \"but not over\"; at_least and less_than are each one dollar above the printed figures.\n")
	}
	for _, d := range drops {
		fmt.Fprintf(&b, "# note: Before reading, text matching %q was blanked from each line.\n", d)
	}
	for _, r := range replaces {
		old, new, _ := strings.Cut(r, "=>")
		fmt.Fprintf(&b, "# note: Before reading, %q was replaced with %q.\n", old, new)
	}
	if *htmlSection != "" {
		fmt.Fprintf(&b, "# note: Read from the HTML page's section with id %q, one table row to a line.\n", *htmlSection)
	}
	for _, n := range ns {
		fmt.Fprintf(&b, "# note: %s\n", n)
	}
	fmt.Fprintf(&b, "# note: Extracted by tools/tableextract.\n")
	fmt.Fprintf(&b, "at_least\tless_than\t%s\n", strings.Join(names, "\t"))
	for _, r := range rows {
		fmt.Fprintf(&b, "%d\t%d", r.AtLeast, r.LessThan)
		for _, i := range index {
			fmt.Fprintf(&b, "\t%d", r.Tax[i])
		}
		b.WriteString("\n")
	}
	_, err = io.WriteString(w, b.String())
	return err
}

// edit applies the -drop and -replace edits to the pages' text. Each edit
// must change something, so a stale flag is not kept by mistake.
func edit(pages []string, drops, replaces []string) ([]string, error) {
	out := append([]string(nil), pages...)
	for _, r := range replaces {
		old, new, ok := strings.Cut(r, "=>")
		if !ok || old == "" {
			return nil, fmt.Errorf("-replace %q: want OLD=>NEW", r)
		}
		n := 0
		for i := range out {
			n += strings.Count(out[i], old)
			out[i] = strings.ReplaceAll(out[i], old, new)
		}
		if n == 0 {
			return nil, fmt.Errorf("-replace %q: %q is not in the pages", r, old)
		}
	}
	for _, d := range drops {
		re, err := regexp.Compile(d)
		if err != nil {
			return nil, fmt.Errorf("-drop %q: %v", d, err)
		}
		n := 0
		for i, page := range out {
			lines := strings.Split(page, "\n")
			for j, line := range lines {
				lines[j] = re.ReplaceAllStringFunc(line, func(m string) string {
					n++
					return strings.Repeat(" ", len(m))
				})
			}
			out[i] = strings.Join(lines, "\n")
		}
		if n == 0 {
			return nil, fmt.Errorf("-drop %q matched nothing", d)
		}
	}
	return out, nil
}

func parsePages(s string) (from, to int, err error) {
	a, b, ok := strings.Cut(s, "-")
	if !ok {
		b = a
	}
	from, err1 := strconv.Atoi(a)
	to, err2 := strconv.Atoi(b)
	if err1 != nil || err2 != nil || from < 1 || to < from {
		return 0, 0, fmt.Errorf("-pages %q: want N or N-M, counting from 1", s)
	}
	return from, to, nil
}
