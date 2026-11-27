package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Two blocks side by side, a thousands heading, a sample row in a sentence
// that repeats a real row, and the $5-15 and $15-25 rows laid out down the
// page, one number to a line, as pdftotext lays out the federal table.
const federalLike = `
 2025 Tax Table     See the instructions for line 16.
 income on Form 1040, line 15, is $25,300. First, they      0  5   0  0  0  0
         0           5            0         0           0    0
         5
        15
                    15
                    25
                                  1
                                  2
                                            1
                                            2
                                                        1
                                                        2
                                                             1
                                                             2
                                       1,000
        25          50            4         4           4    4     1,000     1,025       101       101      101    101
        50       1,000            6         6           6    6     1,025     1,050       104       104      104    104
68                       Need more information or forms? Visit IRS.gov.
`

func TestExtractFederalLayout(t *testing.T) {
	rows, err := Extract([]string{federalLike}, Spec{Columns: 4, Bound: LessThan, First: 0, Last: 1050})
	if err != nil {
		t.Fatal(err)
	}
	want := []Row{
		{0, 5, []int64{0, 0, 0, 0}},
		{5, 15, []int64{1, 1, 1, 1}},
		{15, 25, []int64{2, 2, 2, 2}},
		{25, 50, []int64{4, 4, 4, 4}},
		{50, 1000, []int64{6, 6, 6, 6}},
		{1000, 1025, []int64{101, 101, 101, 101}},
		{1025, 1050, []int64{104, 104, 104, 104}},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("rows = %v\nwant   %v", rows, want)
	}
}

// California prints "but not over", dollar signs on the first row, and a
// note in capitals beside the last block.
const californiaLike = `
       $1     $50            $0         $0           $0      151     250          2          2          2
       51      150            1          1            1
  87,151  87,250        4,548     2,302     2,673   OVER $100,000 YOU MUST COMPUTE YOUR
`

func TestExtractCaliforniaLayout(t *testing.T) {
	rows, err := Extract([]string{californiaLike, ""}, Spec{Columns: 3, Bound: NotOver, First: 1, Last: 251})
	if err == nil {
		t.Fatalf("a gap between 251 and 87,151 went unnoticed: %v", rows)
	}
	page := strings.Replace(californiaLike, "87,151  87,250", "251     87,250", 1)
	rows, err = Extract([]string{page}, Spec{Columns: 3, Bound: NotOver, First: 1, Last: 87251})
	if err != nil {
		t.Fatal(err)
	}
	want := []Row{
		{1, 51, []int64{0, 0, 0}},
		{51, 151, []int64{1, 1, 1}},
		{151, 251, []int64{2, 2, 2}},
		{251, 87251, []int64{4548, 2302, 2673}},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("rows = %v\nwant   %v", rows, want)
	}
}

func TestExtractRejects(t *testing.T) {
	spec := Spec{Columns: 1, Bound: LessThan, First: 0, Last: 20}
	for name, page := range map[string]string{
		"gap":                "0 5 0\n6 20 1\n",
		"falling column":     "0 5 3\n5 20 2\n",
		"conflicting repeat": "0 5 0\n5 20 1\n5 20 2\n",
		"wrong first":        "1 5 0\n5 20 1\n",
		"wrong last":         "0 5 0\n5 25 1\n",
		"odd one-number run": "0 5 0\n5\n20\n",
		"odd run on its own": "0 5 0 5\n",
		"nothing at all":     "Tax Table\n",
		"row ends before it": "0 5 0\n5 5 1\n5 20 1\n",
	} {
		if rows, err := Extract([]string{page}, spec); err == nil {
			t.Errorf("%s: no error, rows %v", name, rows)
		}
	}
}

func TestParseColumns(t *testing.T) {
	names, index, err := ParseColumns("single+mfs,mfj+qss,hoh")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"single", "mfj", "mfs", "hoh", "qss"}; !reflect.DeepEqual(names, want) {
		t.Errorf("names = %v, want %v", names, want)
	}
	if want := []int{0, 1, 0, 2, 1}; !reflect.DeepEqual(index, want) {
		t.Errorf("index = %v, want %v", index, want)
	}
	for _, bad := range []string{"single,widow", "single,single"} {
		if _, _, err := ParseColumns(bad); err == nil {
			t.Errorf("ParseColumns(%q): no error", bad)
		}
	}
}

func TestRunWritesHeaderAndRows(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.pdf"), []byte("pdf"), 0o644); err != nil {
		t.Fatal(err)
	}
	text := "cover\f0 5 0 1\n5 10 1 2\f"
	if err := os.WriteFile(filepath.Join(dir, "x.txt"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	err := run([]string{
		"-source", "src", "-pdf", "x.pdf", "-pdfroot", dir, "-pages", "2",
		"-columns", "single+mfs,mfj", "-first", "0", "-last", "10", "-note", "hello",
		filepath.Join(dir, "x.txt"),
	}, &out)
	if err != nil {
		t.Fatal(err)
	}
	want := "# source: src\n# pdf: x.pdf\n# sha256: " + sha256Hex("pdf") + "\n# pages: 2\n# kind: table\n" +
		"# note: hello\n# note: Extracted by tools/tableextract.\n" +
		"at_least\tless_than\tsingle\tmfj\tmfs\n0\t5\t0\t1\t0\n5\t10\t1\t2\t1\n"
	if got := out.String(); got != want {
		t.Errorf("output:\n%s\nwant:\n%s", got, want)
	}
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func TestExtractOverNotOver(t *testing.T) {
	// "Over $9 but not over $50", as Massachusetts prints it, holds whole
	// dollars from 10 through 50.
	rows, err := Extract([]string{"$9 $50 $1\n50 100 4\n100 150 2134\n"}, Spec{Columns: 1, Bound: OverNotOver, First: 10, Last: 151})
	if err != nil {
		t.Fatal(err)
	}
	want := []Row{{10, 51, []int64{1}}, {51, 101, []int64{4}}, {101, 151, []int64{2134}}}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("rows = %v, want %v", rows, want)
	}
}

func TestNumberWithoutCommas(t *testing.T) {
	for tok, want := range map[string]int64{"2134": 2134, "$2134": 2134, "2,134": 2134} {
		if n, ok := parseNumber(tok); !ok || n != want {
			t.Errorf("parseNumber(%q) = %d, %v", tok, n, ok)
		}
	}
	if _, ok := parseNumber("21,34"); ok {
		t.Error("parseNumber(21,34) read a misgrouped number")
	}
}

func TestEdit(t *testing.T) {
	pages := []string{"0 5 0 7,000 14,000\n12\n", "35,950 21,000 1,656\n"}
	got, err := edit(pages, []string{` 7,000 14,000$`, `^12$`}, []string{"35,950 21,000=>35,950 36,000"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"0 5 0             \n  \n", "35,950 36,000 1,656\n"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("edit = %q, want %q", got, want)
	}
	for _, bad := range [][2][]string{
		{{`nowhere`}, nil},
		{nil, {"nowhere=>x"}},
		{nil, {"no arrow"}},
		{{`(`}, nil},
	} {
		if _, err := edit(pages, bad[0], bad[1]); err == nil {
			t.Errorf("edit(%q, %q): no error", bad[0], bad[1])
		}
	}
}

func TestHTMLText(t *testing.T) {
	doc := `<h2>Other</h2><table><tr><td>1</td><td>2</td></tr></table>
<h2><a id="t"></a>Table</h2><p>Note&#160;one</p><table>
<tr><th>At least</th><th>But less than</th><th>Tax</th></tr>
<tr>
<td><span>63,</span>350</td>
<td><span>63,</span>400</td>
<td>3,321</td>
</tr></table>
<h2><a id="u"></a>Next</h2><tr><td>9</td></tr>`
	got, err := HTMLText(doc, "t")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := Extract([]string{got}, Spec{Columns: 1, First: 63350, Last: 63400})
	if err != nil {
		t.Fatalf("%v\n%s", err, got)
	}
	if len(rows) != 1 || rows[0].Tax[0] != 3321 {
		t.Errorf("rows = %v, from\n%s", rows, got)
	}
	if _, err := HTMLText(doc, "missing"); err == nil {
		t.Error("a missing id is not an error")
	}
}
