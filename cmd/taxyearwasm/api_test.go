package main

import (
	"os"
	"testing"

	"google.golang.org/protobuf/proto"

	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
)

// TestAPI parses a case, computes it, edits it and computes it again,
// through the functions the JavaScript API calls.
func TestAPI(t *testing.T) {
	text, err := os.ReadFile("../../taxyear/testdata/cases/full-everything.jsonc")
	if err != nil {
		t.Fatal(err)
	}
	a := newAPI(false)
	file, err := a.parse(text)
	if err != nil {
		t.Fatal(err)
	}
	page := func(file []byte, scenario string) *scenariosv1.BuilderPage {
		t.Helper()
		b, err := a.compute(file, scenario)
		if err != nil {
			t.Fatal(err)
		}
		p := &scenariosv1.BuilderPage{}
		if err := proto.Unmarshal(b, p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	if p := page(file, ""); len(p.GetResult().GetScenarios()) != 4 || p.GetView().GetScenario() != "plan" || len(p.GetView().GetGroups()) == 0 {
		t.Fatalf("the page has %d scenarios and view %q", len(p.GetResult().GetScenarios()), p.GetView().GetScenario())
	}
	edits, _ := proto.Marshal(&scenariosv1.Edits{Edits: []*scenariosv1.Edit{{
		Scenario: "raise", Path: "model.employers[acme].parts[acme-0fjb8x60].pay_rate.paycheck.gross",
		Value: &scenariosv1.Value{Kind: &scenariosv1.Value_Money{Money: &commonv1.Money{Minor: 600000, Currency: "USD"}}},
	}}})
	edited, err := a.applyEdit(file, edits)
	if err != nil {
		t.Fatal(err)
	}
	if got := page(edited, "raise").GetView().GetScenario(); got != "raise" {
		t.Errorf("the view is of %q", got)
	}
	if _, err := a.applyEdit(file, nil); err == nil {
		t.Error("an empty Edits was applied")
	}
	if _, err := a.compute([]byte{0xff}, ""); err == nil {
		t.Error("bytes that aren't a file were computed")
	}
}
