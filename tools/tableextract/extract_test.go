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
