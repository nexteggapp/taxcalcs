package cites_test

import (
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/rules/cites"
	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	"github.com/nexteggapp/taxcalcs/returns/rules/states"
	"github.com/nexteggapp/taxcalcs/sources"
	sourcesv1 "github.com/nexteggapp/taxcalcs/sources/v1"
)

// Every citation in every rule file, federal and state, resolves in the
// source catalog.
func TestEveryCitationResolves(t *testing.T) {
	catalog, err := sources.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	fed, err := federal.Files()
	if err != nil {
		t.Fatal(err)
	}
	st, err := states.Files()
	if err != nil {
		t.Fatal(err)
	}
	var all []cites.Cite
	for dir, files := range map[string][]states.File{"returns/rules/federal/": fed, "returns/rules/states/": st} {
		for _, f := range files {
			cs, err := cites.Find(dir+f.Name, f.Source)
			if err != nil {
				t.Fatal(err)
			}
			all = append(all, cs...)
		}
	}
	if len(all) == 0 {
		t.Fatal("found no citations")
	}
	for _, err := range cites.Check(all, catalog) {
		t.Error(err)
	}
}

var declaration = regexp.MustCompile(`(?m)^\((?:def|defform|defworksheet) \(?([^\s()]+)`)

// Every anchor the catalog says a rule implements names a rule that exists,
// in any jurisdiction.
func TestEveryImplementingRuleExists(t *testing.T) {
	catalog, err := sources.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	fed, err := federal.Files()
	if err != nil {
		t.Fatal(err)
	}
	st, err := states.Files()
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, f := range append(fed, st...) {
		for _, m := range declaration.FindAllStringSubmatch(f.Source, -1) {
			names[m[1]] = true
		}
	}
	for _, src := range catalog {
		for _, a := range src.GetAnchors() {
			name, _, _ := strings.Cut(a.GetImplementedBy(), ":")
			if name != "" && !names[name] {
				t.Errorf("%s#%s is implemented by %s, which no rule file declares", src.GetId(), a.GetId(), name)
			}
		}
	}
}

var update = flag.Bool("update", false, "rewrite labels.tsv from the catalog")

// labels.tsv, which the Go and Rust preparers read, matches the catalog.
func TestLabelsAreCurrent(t *testing.T) {
	catalog, err := sources.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	want := cites.Labels(catalog)
	if *update {
		dir := os.Getenv("BUILD_WORKSPACE_DIRECTORY")
		if dir == "" {
			t.Fatal("-update needs bazel run")
		}
		if err := os.WriteFile(filepath.Join(dir, "returns/rules/cites/labels.tsv"), []byte(want), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile("labels.tsv")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Error("labels.tsv is stale: bazel run //returns/rules/cites:cites_test -- -update")
	}
}

func TestText(t *testing.T) {
	for _, tc := range []struct {
		cite cites.Cite
		want string
	}{
		{cites.Cite{Law: "26 U.S.C.", Section: "24", Sub: "(h)(4)"}, "26 U.S.C. § 24(h)(4)"},
		{cites.Cite{Source: "us-irs-1040-instructions-2025", Anchor: "line-12e"}, "Form 1040 Instructions (2025), Line 12e – Standard Deduction Chart for Most People"},
		{cites.Cite{Text: "Form 1040"}, "Form 1040"},
	} {
		if got, _ := cites.Text(tc.cite); got != tc.want {
			t.Errorf("Text(%v) = %q, want %q", tc.cite, got, tc.want)
		}
	}
}

func TestCheckRejectsWhatTheCatalogDoesNotSupport(t *testing.T) {
	catalog := []*sourcesv1.Source{
		{
			Id:           "us-usc-26-2025",
			Kind:         sourcesv1.Kind_KIND_STATUTE,
			Designation:  "26 U.S.C.",
			AnchorScheme: &sourcesv1.AnchorScheme{Cited: []*sourcesv1.CitedPart{{Number: "24", Heading: "Child tax credit"}}},
		},
		{
			Id:      "us-irs-1040-instructions-2025",
			Kind:    sourcesv1.Kind_KIND_INSTRUCTIONS,
			Anchors: []*sourcesv1.Anchor{{Id: "line-12e"}},
		},
	}
	src := `
(def good ((x Int)) Int
  :cite ((law "26 U.S.C." "24" "(h)(4)") (doc "us-irs-1040-instructions-2025" "line-12e")
         (doc "us-irs-1040-instructions-2025"))
  x)
(def bad ((x Int)) Int
  :cite ("26 U.S.C. § 24" (law "26 U.S.C." "9999") (law "Cal. Rev. & Tax. Code" "17041")
         (doc "no-such-source") (doc "us-irs-1040-instructions-2025" "line-99"))
  x)`
	cs, err := cites.Find("test.tlisp", src)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 8 {
		t.Fatalf("found %d citations, want 8", len(cs))
	}
	errs := cites.Check(cs, catalog)
	if len(errs) != 5 {
		t.Errorf("got %d errors, want one for each of the 5 bad citations:", len(errs))
		for _, err := range errs {
			t.Log(err)
		}
	}
	for _, err := range errs {
		if line := err.Error(); line[:len("test.tlisp:6:")] != "test.tlisp:6:" {
			t.Errorf("error is not about the bad rule: %v", err)
		}
	}
}
