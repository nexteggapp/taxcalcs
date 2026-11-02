package taxyear_test

import (
	"testing"

	"google.golang.org/protobuf/proto"

	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear"
)

func tab(t *testing.T, v *scenariosv1.BuilderView, id string) *scenariosv1.ScenarioTab {
	t.Helper()
	for _, s := range v.GetScenarios() {
		if s.GetId() == id {
			return s
		}
	}
	t.Fatalf("the view has no scenario %q", id)
	return nil
}

func action(t *testing.T, actions []*scenariosv1.Action, label string) *scenariosv1.Edit {
	t.Helper()
	for _, a := range actions {
		if a.GetLabel() == label {
			return a.GetEdit()
		}
	}
	t.Fatalf("no %q among %v", label, actions)
	return nil
}

func apply(t *testing.T, f *scenariosv1.TaxYearScenarios, e *scenariosv1.Edit) *scenariosv1.TaxYearScenarios {
	t.Helper()
	out, err := taxyear.ApplyEdit(f, e)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func inputs(t *testing.T, f *scenariosv1.TaxYearScenarios, id string) *scenariosv1.ScenarioInputs {
	t.Helper()
	r, err := taxyear.Compute(f)
	if err != nil {
		t.Fatal(err)
	}
	in, ok := r.Inputs(id)
	if !ok {
		t.Fatalf("scenario %q doesn't resolve", id)
	}
	return in
}

// TestScenarioNewAndDuplicate adds a scenario from the view's New scenario
// button and duplicates Raise in Oct from its menu. The new scenario is based
// on the one shown and changes nothing; the copy has the same inputs.
func TestScenarioNewAndDuplicate(t *testing.T) {
	f := readCase(t, "full-everything")
	v := view(t, f, "raise")
	added := apply(t, f, v.GetNewScenario().GetEdit())
	s := added.GetScenarios()[len(added.GetScenarios())-1]
	if s.GetId() != "scenario" || s.GetName() != "New scenario" || s.GetParent() != "raise" {
		t.Errorf("the new scenario is %v", s)
	}
	if !proto.Equal(inputs(t, added, s.GetId()), inputs(t, f, "raise")) {
		t.Error("the new scenario's inputs differ from its parent's")
	}
	again := view(t, added, "raise").GetNewScenario().GetEdit()
	if again.GetScenario() != "scenario-2" {
		t.Errorf("the next new scenario's id is %q", again.GetScenario())
	}

	dup := apply(t, f, action(t, tab(t, v, "raise").GetActions(), "Duplicate"))
	c := dup.GetScenarios()[len(dup.GetScenarios())-1]
	if c.GetId() != "raise-copy" || c.GetName() != "Copy of Raise in Oct" || c.GetParent() != "plan" {
		t.Errorf("the copy is %v", c)
	}
	if !proto.Equal(inputs(t, dup, c.GetId()), inputs(t, f, "raise")) {
		t.Error("the copy's inputs differ from the original's")
	}
	planCopy := apply(t, f, action(t, tab(t, v, "plan").GetActions(), "Duplicate"))
	if !proto.Equal(inputs(t, planCopy, "plan-copy"), inputs(t, f, "plan")) {
		t.Error("the copy of Your plan differs from it")
	}
}

// TestScenarioRenameAndDelete renames a scenario through its name field, and
// deletes Raise in Oct, which "Raise, and freelance stops" is based on. That
// scenario is rebased onto Your plan with the same inputs.
func TestScenarioRenameAndDelete(t *testing.T) {
	f := readCase(t, "full-everything")
	v := view(t, f, "raise")
	nf := tab(t, v, "raise").GetNameField()
	if nf == nil || tab(t, v, "plan").GetNameField() != nil {
		t.Fatal("want a name field on Raise in Oct and none on Your plan")
	}
	renamed := apply(t, f, &scenariosv1.Edit{Scenario: "raise", Path: nf.GetPath(), Value: &scenariosv1.Value{Kind: &scenariosv1.Value_Text{Text: "Raise in Nov"}}})
	if got := tab(t, view(t, renamed, "plan"), "raise").GetName(); got != "Raise in Nov" {
		t.Errorf("the name is %q", got)
	}

	before := inputs(t, f, "raise-freelance-stops")
	gone := apply(t, f, action(t, tab(t, v, "raise").GetActions(), "Delete"))
	for _, s := range gone.GetScenarios() {
		if s.GetId() == "raise" {
			t.Fatal("raise is still in the file")
		}
		if s.GetId() == "raise-freelance-stops" && s.GetParent() != "plan" {
			t.Errorf("the child's parent is %q", s.GetParent())
		}
	}
	if !proto.Equal(inputs(t, gone, "raise-freelance-stops"), before) {
		t.Error("the rebased scenario's inputs changed")
	}
	for _, a := range tab(t, v, "plan").GetActions() {
		if a.GetLabel() == "Delete" {
			t.Error("Your plan offers Delete")
		}
	}
}

// TestScenarioEditRefused refuses scenario edits that don't fit.
func TestScenarioEditRefused(t *testing.T) {
	f := readCase(t, "full-everything")
	remove := &scenariosv1.Value{Kind: &scenariosv1.Value_Remove_{Remove: &scenariosv1.Value_Remove{}}}
	text := &scenariosv1.Value{Kind: &scenariosv1.Value_Text{Text: "x"}}
	dupe, _ := proto.Marshal(&scenariosv1.Scenario{Id: "raise", Name: "Again", Parent: "plan"})
	orphan, _ := proto.Marshal(&scenariosv1.Scenario{Id: "new", Name: "New", Parent: "nope"})
	for _, c := range []struct {
		name, scenario, path string
		value                *scenariosv1.Value
	}{
		{"delete Your plan", "plan", taxyear.ScenarioPath, remove},
		{"delete nothing", "nope", taxyear.ScenarioPath, remove},
		{"rename Your plan", "plan", taxyear.ScenarioNamePath, text},
		{"empty name", "raise", taxyear.ScenarioNamePath, &scenariosv1.Value{Kind: &scenariosv1.Value_Text{}}},
		{"id taken", "raise", taxyear.ScenarioPath, &scenariosv1.Value{Kind: &scenariosv1.Value_Message{Message: dupe}}},
		{"no parent", "new", taxyear.ScenarioPath, &scenariosv1.Value{Kind: &scenariosv1.Value_Message{Message: orphan}}},
		{"text", "raise", taxyear.ScenarioPath, text},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := taxyear.ApplyEdit(f, &scenariosv1.Edit{Scenario: c.scenario, Path: c.path, Value: c.value}); err == nil {
				t.Error("the edit was made")
			}
		})
	}
}
