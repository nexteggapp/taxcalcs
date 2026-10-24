package all_test

import (
	"errors"
	"testing"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/estimated/all"
	"github.com/nexteggapp/taxcalcs/filing"
)

// postalCodes is written out here rather than read from
// estimated.Jurisdictions, so that a jurisdiction missing from both the
// constants and the registry still fails.
var postalCodes = []string{
	"US",
	"AL", "AK", "AZ", "AR", "CA", "CO", "CT", "DE", "DC", "FL",
	"GA", "HI", "ID", "IL", "IN", "IA", "KS", "KY", "LA", "ME",
	"MD", "MA", "MI", "MN", "MS", "MO", "MT", "NE", "NV", "NH",
	"NJ", "NM", "NY", "NC", "ND", "OH", "OK", "OR", "PA", "RI",
	"SC", "SD", "TN", "TX", "UT", "VT", "VA", "WA", "WV", "WI",
	"WY",
}

// Every state, the District of Columbia and the federal government has 2026
// rules registered.
func TestForFindsEveryJurisdiction(t *testing.T) {
	if len(postalCodes) != 52 {
		t.Fatalf("%d postal codes listed, want 50 states, DC and US", len(postalCodes))
	}
	for _, code := range postalCodes {
		j := estimated.Jurisdiction(code)
		rules, err := all.For(j, 2026)
		if err != nil {
			t.Errorf("%s 2026: %v", j, err)
			continue
		}
		if rules.Explanation().Summary() == "" {
			t.Errorf("%s 2026 has no explanation", j)
		}
		// Each jurisdiction's rules refuse facts about another, so this
		// fails when the registry maps a code to the wrong package.
		if _, err := rules.Requirement(minimalFacts(t, j)); err != nil {
			t.Errorf("%s 2026 refuses its own facts: %v", j, err)
		}
	}
}

// minimalFacts is a single filer with nothing else said.
func minimalFacts(t *testing.T, j estimated.Jurisdiction) estimated.Facts {
	t.Helper()
	this, err := estimated.NewFigures().Build()
	if err != nil {
		t.Fatal(err)
	}
	f, err := estimated.NewFacts(j, 2026).FilingStatus(filing.Single).ThisYear(this).Build()
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// estimated.Jurisdictions lists the same 52 codes, once each.
func TestJurisdictionsListsEveryCode(t *testing.T) {
	want := map[string]bool{}
	for _, c := range postalCodes {
		want[c] = true
	}
	got := estimated.Jurisdictions()
	if len(got) != len(want) {
		t.Fatalf("Jurisdictions has %d entries, want %d", len(got), len(want))
	}
	seen := map[estimated.Jurisdiction]bool{}
	for _, j := range got {
		if !want[string(j)] || seen[j] {
			t.Errorf("Jurisdictions lists %q unexpectedly or twice", j)
		}
		seen[j] = true
	}
}

// A year or jurisdiction that is not built is ErrNoRules, and never the
// nearest year's rules.
func TestForRefusesWhatIsNotBuilt(t *testing.T) {
	for _, code := range postalCodes {
		for _, year := range []int{2025, 2027} {
			if _, err := all.For(estimated.Jurisdiction(code), year); !errors.Is(err, estimated.ErrNoRules) {
				t.Errorf("%s %d: got %v, want ErrNoRules", code, year, err)
			}
		}
	}
	for _, j := range []estimated.Jurisdiction{"PR", "GU", "XX", ""} {
		if _, err := all.For(j, 2026); !errors.Is(err, estimated.ErrNoRules) {
			t.Errorf("%q 2026: got %v, want ErrNoRules", j, err)
		}
	}
}
