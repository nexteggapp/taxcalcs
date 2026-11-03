package taxyear

import (
	"errors"
	"fmt"

	"google.golang.org/protobuf/proto"

	"github.com/nexteggapp/protodiffgen/apply"

	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
)

// The paths of an edit to the file's list of scenarios rather than to a
// scenario's inputs.
const (
	// ScenarioPath adds a scenario, with a Scenario message, or deletes one,
	// with Remove.
	ScenarioPath = "scenario"
	// ScenarioNamePath renames a scenario, with text.
	ScenarioNamePath = "scenario.name"
)

// isScenarioEdit reports whether an edit changes the list of scenarios.
func isScenarioEdit(e *scenariosv1.Edit) bool {
	return e.GetPath() == ScenarioPath || e.GetPath() == ScenarioNamePath
}

// applyScenarioEdit adds, deletes or renames a scenario. before is the file's
// result, which a delete reads to rebase the scenarios based on the one
// deleted.
func applyScenarioEdit(before Result, file *scenariosv1.TaxYearScenarios, e *scenariosv1.Edit) (*scenariosv1.TaxYearScenarios, error) {
	out := &scenariosv1.TaxYearScenarios{SchemaVersion: file.GetSchemaVersion(), TaxYear: file.GetTaxYear(), AsOf: file.GetAsOf(),
		ExportedFrom: file.GetExportedFrom(), Shared: file.GetShared(), Scenarios: append([]*scenariosv1.Scenario(nil), file.GetScenarios()...)}
	idx := scenarioIndex(file, e.GetScenario())
	switch {
	case e.GetPath() == ScenarioNamePath:
		name := e.GetValue().GetText()
		if idx < 0 {
			return nil, fmt.Errorf("taxyear: no scenario %q", e.GetScenario())
		}
		if name == "" {
			return nil, errors.New("taxyear: a scenario's name is text, and not empty")
		}
		if idx == 0 {
			return nil, errors.New("taxyear: Your plan keeps its name")
		}
		s := clone(out.Scenarios[idx])
		s.Name = name
		out.Scenarios[idx] = s
	case e.GetValue().GetMessage() != nil:
		s := &scenariosv1.Scenario{}
		if err := proto.Unmarshal(e.GetValue().GetMessage(), s); err != nil {
			return nil, fmt.Errorf("taxyear: a new scenario: %w", err)
		}
		switch {
		case idx >= 0:
			return nil, fmt.Errorf("taxyear: there is already a scenario %q", e.GetScenario())
		case s.GetId() != e.GetScenario():
			return nil, fmt.Errorf("taxyear: the new scenario's id is %q, not %q", s.GetId(), e.GetScenario())
		case scenarioIndex(file, s.GetParent()) < 0:
			return nil, fmt.Errorf("taxyear: the new scenario's parent %q is not in the file", s.GetParent())
		case s.GetName() == "":
			return nil, errors.New("taxyear: a new scenario needs a name")
		}
		out.Scenarios = append(out.Scenarios, s)
	case e.GetValue().GetRemove() != nil:
		if idx < 0 {
			return nil, fmt.Errorf("taxyear: no scenario %q", e.GetScenario())
		}
		if idx == 0 {
			return nil, errors.New("taxyear: Your plan can't be deleted")
		}
		var err error
		if out.Scenarios, err = withoutScenario(before, file, idx); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("taxyear: %s takes a Scenario message or Remove", ScenarioPath)
	}
	return out, nil
}

// withoutScenario is the file's scenarios less the one at idx. Each
// scenario based on it is based on its parent instead, with its changes
// written again so its inputs are the same.
func withoutScenario(before Result, file *scenariosv1.TaxYearScenarios, idx int) ([]*scenariosv1.Scenario, error) {
	gone := file.GetScenarios()[idx]
	parent, ok := before.Inputs(gone.GetParent())
	if !ok {
		return nil, fmt.Errorf("taxyear: scenario %q doesn't resolve", gone.GetParent())
	}
	var out []*scenariosv1.Scenario
	for i, s := range file.GetScenarios() {
		if i == idx {
			continue
		}
		if s.GetParent() != gone.GetId() {
			out = append(out, s)
			continue
		}
		in, ok := before.Inputs(s.GetId())
		if !ok {
			return nil, fmt.Errorf("taxyear: scenario %q doesn't resolve", s.GetId())
		}
		diff := &scenariosv1.ScenarioInputsDiff{}
		if err := apply.Diff(parent, in, diff); err != nil {
			return nil, fmt.Errorf("taxyear: rebasing %q: %w", s.GetId(), err)
		}
		out = append(out, &scenariosv1.Scenario{Id: s.GetId(), Name: s.GetName(), Parent: gone.GetParent(), Changes: diff})
	}
	return out, nil
}

func scenarioIndex(file *scenariosv1.TaxYearScenarios, id string) int {
	for i, s := range file.GetScenarios() {
		if s.GetId() == id {
			return i
		}
	}
	return -1
}

// scenarioTabs lists the file's scenarios, each with its rename field and
// menu, and the button that adds a scenario based on current.
func scenarioTabs(file *scenariosv1.TaxYearScenarios, current string) ([]*scenariosv1.ScenarioTab, *scenariosv1.Action) {
	taken := map[string]bool{}
	names := map[string]bool{}
	for _, s := range file.GetScenarios() {
		taken[s.GetId()] = true
		names[s.GetName()] = true
	}
	var tabs []*scenariosv1.ScenarioTab
	var add *scenariosv1.Action
	for i, s := range file.GetScenarios() {
		tab := &scenariosv1.ScenarioTab{Id: s.GetId(), Name: s.GetName(), Parent: s.GetParent()}
		copyID := freeID(taken, s.GetId()+"-copy")
		dup := &scenariosv1.Scenario{Id: copyID, Name: freeName(names, "Copy of "+s.GetName()), Parent: s.GetParent(), Changes: s.GetChanges()}
		if i == 0 {
			// A copy of Your plan is a scenario based on it that changes
			// nothing yet: Your plan's own changes are to shared.
			dup.Parent, dup.Changes = s.GetId(), nil
		}
		tab.Actions = append(tab.Actions, addAction("Duplicate", dup))
		if i > 0 {
			tab.NameField = &scenariosv1.Field{Path: ScenarioNamePath, Label: "Name",
				Kind:  &scenariosv1.FieldKind{Kind: &scenariosv1.FieldKind_Text_{Text: &scenariosv1.FieldKind_Text{}}},
				Value: &scenariosv1.Value{Kind: &scenariosv1.Value_Text{Text: s.GetName()}}}
			tab.Actions = append(tab.Actions, &scenariosv1.Action{Label: "Delete",
				Edit: &scenariosv1.Edit{Scenario: s.GetId(), Path: ScenarioPath, Value: &scenariosv1.Value{Kind: &scenariosv1.Value_Remove_{Remove: &scenariosv1.Value_Remove{}}}}})
		}
		tabs = append(tabs, tab)
		if s.GetId() == current {
			add = addAction("New scenario", &scenariosv1.Scenario{Id: freeID(taken, "scenario"), Name: freeName(names, "New scenario"), Parent: s.GetId()})
		}
	}
	return tabs, add
}

// addAction is a button whose edit adds s.
func addAction(label string, s *scenariosv1.Scenario) *scenariosv1.Action {
	b, _ := proto.MarshalOptions{Deterministic: true}.Marshal(s)
	return &scenariosv1.Action{Label: label, Edit: &scenariosv1.Edit{Scenario: s.GetId(), Path: ScenarioPath,
		Value: &scenariosv1.Value{Kind: &scenariosv1.Value_Message{Message: b}}}}
}

// freeID is base, or base and the first number from 2 that no scenario has.
func freeID(taken map[string]bool, base string) string {
	return free(taken, base, "-")
}

// freeName is name, or name and the first number from 2 that no scenario
// has.
func freeName(names map[string]bool, name string) string {
	return free(names, name, " ")
}

func free(taken map[string]bool, base, sep string) string {
	if !taken[base] {
		return base
	}
	for n := 2; ; n++ {
		if s := fmt.Sprintf("%s%s%d", base, sep, n); !taken[s] {
			return s
		}
	}
}
