package all_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nexteggapp/taxcalcs/estimated/estimatedtest"
)

// Every sentence in the estimated-tax source is at most 80 characters,
// including copy no test household reaches: a real person may still read it.
func TestEverySentenceInTheSourceIsShort(t *testing.T) {
	var files []string
	for _, pattern := range []string{"../../*/estimated*.go", "../../estimated/*.go"} {
		m, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, m...)
	}
	packages := map[string]map[string][]byte{}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		name := strings.TrimPrefix(file, "../../")
		dir := filepath.Dir(name)
		if packages[dir] == nil {
			packages[dir] = map[string][]byte{}
		}
		packages[dir][name] = src
	}
	// The 51 jurisdictions, the federal rules and the shared package.
	if len(packages) < 53 {
		t.Fatalf("only %d packages' source was found; the test's data is missing some", len(packages))
	}
	for _, files := range packages {
		problems, err := estimatedtest.CheckSource(files)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range problems {
			t.Error(p)
		}
	}
}

// The fragment check catches a modal or possessive left without its word,
// and passes the same sentence written whole.
func TestFragmentsAreCaught(t *testing.T) {
	for src, want := range map[string]bool{
		`It can for a casualty.`:                           true,
		`It can also if you retired after 62.`:             true,
		`Nor can it for a casualty.`:                       true,
		`Nor is there if you had no tax.`:                  true,
		`No penalty when either year's is under $500.`:     true,
		`It can waive it for a casualty.`:                  false,
		`It can also do so if you retired after 62.`:       false,
		`Nor is there any if you had no tax.`:              false,
		`No penalty when either year's tax is under $500.`: false,
		`90% of this year's tax or 100% of last year's.`:   false,
	} {
		file := "package x\n\nvar s = fmt.Sprint(" + strconv.Quote(src) + ")\n"
		problems, err := estimatedtest.CheckSource(map[string][]byte{"x/x.go": []byte(file)})
		if err != nil {
			t.Fatal(err)
		}
		if got := len(problems) > 0; got != want {
			t.Errorf("%q: flagged %v, want %v: %v", src, got, want, problems)
		}
	}
}
