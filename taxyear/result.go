package taxyear

import (
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/nexteggapp/taxcalcs/money"
	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
)

// Result is a computed tax year. It is immutable: Proto returns a copy.
type Result struct {
	p      *scenariosv1.TaxYearResult
	inputs map[string]*scenariosv1.ScenarioInputs
}

func newResult(p *scenariosv1.TaxYearResult) Result { return Result{p: p} }

// Proto returns the result as the TaxYearResult message, a copy.
func (r Result) Proto() *scenariosv1.TaxYearResult { return clone(r.p) }

// Message returns the TaxYearResult message itself, not a copy, for a
// caller that only reads it. It is shared with the [Cache] that computed
// it and with later results that reuse its scenarios, and must not be
// changed.
func (r Result) Message() *scenariosv1.TaxYearResult { return r.p }

// Inputs returns a scenario's resolved inputs, and false for a scenario
// that didn't resolve. Like [Resolved.Inputs], they share messages with the
// cache and the file's other scenarios, and must not be changed.
func (r Result) Inputs(id string) (*scenariosv1.ScenarioInputs, bool) {
	in, ok := r.inputs[id]
	return in, ok
}

// Problems returns the file's problems; when there are any, no scenario
// was computed.
func (r Result) Problems() []*scenariosv1.Problem {
	var out []*scenariosv1.Problem
	for _, p := range r.p.GetProblems() {
		out = append(out, clone(p))
	}
	return out
}

// Scenario returns one scenario's result by id.
func (r Result) Scenario(id string) (Scenario, bool) {
	for _, s := range r.p.GetScenarios() {
		if s.GetScenario() == id {
			return Scenario{p: s}, true
		}
	}
	return Scenario{}, false
}

// Scenarios returns every scenario's result, in the file's order.
func (r Result) Scenarios() []Scenario {
	var out []Scenario
	for _, s := range r.p.GetScenarios() {
		out = append(out, Scenario{p: s})
	}
	return out
}

// Scenario is one scenario's computed figures.
type Scenario struct {
	p *scenariosv1.ScenarioResult
}

// ID is the scenario's id.
func (s Scenario) ID() string { return s.p.GetScenario() }

// Proto returns the scenario's result, a copy.
func (s Scenario) Proto() *scenariosv1.ScenarioResult { return clone(s.p) }

// Figure returns the figure a path names in the scenario's result, and
// false when the path names nothing.
func (s Scenario) Figure(p Path) (Figure, bool) {
	v, fd, ok := lookup(s.p.ProtoReflect(), p)
	if !ok {
		return Figure{}, false
	}
	f := Figure{path: p}
	if fd.Kind() == protoreflect.MessageKind && !fd.IsList() {
		if m, isMoney := v.Message().Interface().(*commonv1.Money); isMoney {
			f.amount, f.isAmount = money.USCents(m.GetMinor()), true
		}
	}
	f.value = v
	f.field = fd
	for _, w := range s.p.GetWorking() {
		if w.GetPath() == p.String() {
			f.working = w
		}
	}
	return f, true
}

// Figure is one value in a scenario's result, and its working when the
// result holds one.
type Figure struct {
	path     Path
	value    protoreflect.Value
	field    protoreflect.FieldDescriptor
	amount   money.USCents
	isAmount bool
	working  *scenariosv1.Working
}

// Path is the figure's path.
func (f Figure) Path() Path { return f.path }

// Amount returns the figure's amount, and false when it isn't money.
func (f Figure) Amount() (money.USCents, bool) { return f.amount, f.isAmount }

// Working returns how the figure was made, or nil.
func (f Figure) Working() *scenariosv1.Working {
	if f.working == nil {
		return nil
	}
	return clone(f.working)
}

// Value returns the figure as a reflected proto value, for a figure that
// isn't money.
func (f Figure) Value() protoreflect.Value { return f.value }
