package cases

import (
	"flag"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	casesv1 "github.com/nexteggapp/taxcalcs/cases/v1"
	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	"github.com/nexteggapp/taxcalcs/sources"
	sourcesv1 "github.com/nexteggapp/taxcalcs/sources/v1"
)

var update = flag.Bool("update", false, "rewrite COVERAGE.md instead of checking it")

// load reads every case file.
func load(t *testing.T) []Loaded {
	t.Helper()
	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	return loaded
}

// TestCases prepares every case's household and checks each expected
// figure, then checks COVERAGE.md.
func TestCases(t *testing.T) {
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatal(err)
	}
	forms, err := Forms()
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := sources.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]*sourcesv1.Source{}
	for _, s := range catalog {
		byID[s.GetId()] = s
	}
	loaded := load(t)
	ids := map[string]string{}
	for i := range loaded {
		l := &loaded[i]
		c := l.Case
		t.Run(l.Path, func(t *testing.T) {
			if want := strings.TrimSuffix(filepath.Base(l.Path), ".textproto"); c.GetId() != want {
				t.Errorf("id %q, want the file's name %q", c.GetId(), want)
			}
			if prev, ok := ids[c.GetId()]; ok {
				t.Errorf("id %q is also used by %s", c.GetId(), prev)
			}
			ids[c.GetId()] = l.Path
			if c.GetOrigin().GetKind() == casesv1.OriginKind_ORIGIN_KIND_UNSPECIFIED {
				t.Error("origin.kind is not set")
			}
			checkRef(t, byID, "origin.source", c.GetOrigin().GetSource(), false)
			for _, e := range c.GetExpected() {
				if e.GetProvenance() == casesv1.Provenance_PROVENANCE_UNSPECIFIED {
					t.Errorf("%s line %s: provenance is not set", e.GetForm(), e.GetLine())
				}
				frozen := e.GetProvenance() == casesv1.Provenance_PROVENANCE_FROZEN_REGRESSION
				checkRef(t, byID, e.GetForm()+" line "+e.GetLine(), e.GetSource(), !frozen)
				for _, s := range e.GetWorking() {
					checkRef(t, byID, e.GetForm()+" line "+e.GetLine()+" working", s.GetSource(), false)
				}
			}
			results, err := Check(eng, forms, c)
			if err != nil {
				t.Fatal(err)
			}
			l.Results = results
			for _, r := range results {
				e := r.Expected
				switch r.Outcome {
				case Fail:
					t.Errorf("%s line %s: got %s, want %s", e.GetForm(), e.GetLine(), r.Got, want(e))
				case GapClosed:
					t.Errorf("%s line %s: the known gap is closed (got %s); remove its known_gap", e.GetForm(), e.GetLine(), r.Got)
				case Gap:
					t.Logf("%s line %s: known gap, got %s, want %s: %s", e.GetForm(), e.GetLine(), r.Got, want(e), gapReason(c, e))
				}
			}
		})
	}
	if t.Failed() {
		return
	}
	report := Report(loaded, forms, catalog)
	if *update {
		path := "COVERAGE.md"
		if ws := os.Getenv("BUILD_WORKSPACE_DIRECTORY"); ws != "" {
			path = filepath.Join(ws, "cases", path)
		}
		if err := os.WriteFile(path, []byte(report), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile("COVERAGE.md")
	if err != nil || string(got) != report {
		t.Errorf("COVERAGE.md is stale; run `bazel run //cases:cases_test -- -update`")
	}
}

// checkRef fails unless ref names a catalog source and, if given, one of its
// anchors. A nil ref passes unless it is required.
func checkRef(t *testing.T, byID map[string]*sourcesv1.Source, what string, ref *casesv1.SourceRef, required bool) {
	t.Helper()
	if ref.GetId() == "" {
		if required {
			t.Errorf("%s: no source", what)
		}
		return
	}
	src, ok := byID[ref.GetId()]
	if !ok {
		t.Errorf("%s: source %q is not in the catalog", what, ref.GetId())
		return
	}
	if ref.GetAnchor() != "" {
		if _, ok := sources.Anchor(src, ref.GetAnchor()); !ok {
			t.Errorf("%s: source %q has no anchor %q", what, ref.GetId(), ref.GetAnchor())
		}
	}
}

func want(e *casesv1.Expected) string {
	if c, ok := e.GetValue().(*casesv1.Expected_Cents); ok {
		return strconv.FormatInt(c.Cents, 10) + " cents"
	}
	return strconv.FormatInt(e.GetInteger(), 10)
}

func gapReason(c *casesv1.TestCase, e *casesv1.Expected) string {
	if g := e.GetKnownGap(); g != nil {
		return g.GetReason()
	}
	return c.GetKnownGap().GetReason()
}
