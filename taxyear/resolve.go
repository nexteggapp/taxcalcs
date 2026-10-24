package taxyear

import (
	"fmt"

	"github.com/nexteggapp/protodiffgen/apply"

	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
)

// Resolved is one scenario's inputs: shared with each diff down its parent
// chain applied in turn, Your plan's first.
type Resolved struct {
	scenario *scenariosv1.Scenario
	inputs   *scenariosv1.ScenarioInputs
}

// Scenario is the scenario.
func (r Resolved) Scenario() *scenariosv1.Scenario { return r.scenario }

// Inputs is its resolved inputs. They share messages with the file's
// shared inputs and with its parent's, and callers must not change them.
func (r Resolved) Inputs() *scenariosv1.ScenarioInputs { return r.inputs }

// Resolve applies each scenario's changes down its parent chain with
// protodiffgen's apply, which validates each diff against its parent
// before applying it. Your plan must come first, with id "plan" and no
// parent, and be the only scenario with none; every other parent must be an earlier
// scenario, so no chain loops. A scenario whose diff fails, or whose parent
// failed, is reported as a problem and left out; the others still resolve.
func Resolve(file *scenariosv1.TaxYearScenarios) ([]Resolved, map[string][]*scenariosv1.Problem) {
	problems := map[string][]*scenariosv1.Problem{}
	byID := map[string]*scenariosv1.ScenarioInputs{}
	var out []Resolved
	for i, s := range file.GetScenarios() {
		path := fmt.Sprintf("scenarios[%s]", s.GetId())
		fail := func(text string) {
			problems[s.GetId()] = append(problems[s.GetId()], problem(path, text))
		}
		if text := chainProblem(i, s, func(id string) bool { return byID[id] != nil }); text != "" {
			fail(text)
			continue
		}
		parent := file.GetShared()
		if i > 0 {
			parent = byID[s.GetParent()]
		}
		inputs, err := ApplyChanges(parent, s.GetChanges())
		if err != nil {
			fail(changesFailed(err))
			continue
		}
		byID[s.GetId()] = inputs
		out = append(out, Resolved{scenario: s, inputs: inputs})
	}
	return out, problems
}

// chainProblem is why the scenario at index i can't resolve before its
// changes are applied, or "". resolved reports whether an earlier scenario
// resolved.
func chainProblem(i int, s *scenariosv1.Scenario, resolved func(id string) bool) string {
	switch {
	case i == 0 && (s.GetId() != "plan" || s.GetParent() != ""):
		return fmt.Sprintf("The first scenario must be Your plan, with id plan and no parent; it is %q with parent %q.", s.GetId(), s.GetParent())
	case i > 0 && s.GetParent() == "":
		return "Only Your plan has no parent."
	case i > 0 && !resolved(s.GetParent()):
		return fmt.Sprintf("Its parent, %q, is not an earlier scenario that resolved.", s.GetParent())
	case resolved(s.GetId()):
		return "Another scenario has the same id."
	}
	return ""
}

// ApplyChanges applies one scenario's changes to its parent's inputs, as
// [Resolve] does, and returns apply's error when they don't apply.
// apply.Apply validates the diff before applying it, so it is not
// validated separately. Neither argument is changed.
//
// apply copies and checks the whole of the message it is given, and the
// parent is mostly evidence the changes don't touch. So apply is given
// only the part of the parent the changes touch (see [prune]), and the
// rest is put back afterwards, shared with the parent rather than copied.
// The result therefore shares messages with the parent, and neither may be
// changed while the other is in use.
func ApplyChanges(parent *scenariosv1.ScenarioInputs, changes *scenariosv1.ScenarioInputsDiff) (*scenariosv1.ScenarioInputs, error) {
	if changes == nil {
		changes = &scenariosv1.ScenarioInputsDiff{}
	}
	if parent == nil {
		parent = &scenariosv1.ScenarioInputs{}
	}
	p := prune(parent.ProtoReflect(), changes.ProtoReflect())
	next, err := apply.Apply(p.given.Interface(), changes)
	if err != nil {
		return nil, err
	}
	out := next.(*scenariosv1.ScenarioInputs)
	p.restore(out.ProtoReflect())
	return out, nil
}

// changesFailed is the problem with a scenario whose changes don't apply.
func changesFailed(err error) string {
	return fmt.Sprintf("Its changes don't apply to its parent: %v.", err)
}

func problem(path, text string) *scenariosv1.Problem {
	return &scenariosv1.Problem{Path: path, Message: &scenariosv1.Statement{Text: text}}
}
