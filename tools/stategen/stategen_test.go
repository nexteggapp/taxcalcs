package main

import (
	"strings"
	"testing"
)

// The checked-in sections are what the registry generates. Run
// `go run ./tools/stategen` from the repository root when this fails.
func TestGeneratedSectionsAreCurrent(t *testing.T) {
	stale, err := run("../..", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range stale {
		t.Errorf("%s is out of date: run `go run ./tools/stategen`", s)
	}
}

func TestValidateCatchesClashes(t *testing.T) {
	clash := []State{
		{Postal: "aa", Name: "A", Form: "Form 1", Field: 60, Alias: "a1", FormDir: "form_1"},
		{Postal: "bb", Name: "B", Form: "Form 1", Field: 60, Alias: "b1", FormDir: "form_1"},
		{Postal: "cc", Name: "C", Form: "Form 2", Field: 53, Alias: "c2", FormDir: "form_2"},
	}
	err := validate(clash)
	if err == nil {
		t.Fatal("validate passed states that clash")
	}
	for _, want := range []string{"share field 60", "share Go target form_1_go_proto", "field 53 is NoIncomeTaxReturn's"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("validate's error %q does not say %q", err, want)
		}
	}
	if err := validate(states); err != nil {
		t.Errorf("the registry does not validate: %v", err)
	}
}
