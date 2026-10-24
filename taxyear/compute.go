// Package taxyear computes a tax year from a TaxYearScenarios file: for each
// scenario, the federal return, the estimated-payment answer per
// jurisdiction, a projection of every model into dated items, the questions
// left open and the working behind the figures. See docs/results.md.
//
// [Compute] is the entry point. It parses nothing: [Parse] reads a JSONC
// file. Each scenario's inputs are shared with the diffs down its parent
// chain applied ([Resolve]); each is checked ([Validate]); each model
// expands by the rules in docs/models.md, in the packages under
// taxyear/model; then the return layer, taxyear/federal, and the
// estimated-payment rules, taxyear/paid, read the projection's totals.
package taxyear

import (
	"strings"

	"errors"
	"fmt"
	"time"

	"google.golang.org/protobuf/proto"

	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/federal"
	"github.com/nexteggapp/taxcalcs/taxyear/internal/civil"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	"github.com/nexteggapp/taxcalcs/taxyear/model/employer"
	"github.com/nexteggapp/taxcalcs/taxyear/model/retirement"
	"github.com/nexteggapp/taxcalcs/taxyear/model/taxpaid"
	"github.com/nexteggapp/taxcalcs/taxyear/paid"
	"github.com/nexteggapp/taxcalcs/ustaxcalc"
	form1040 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1040"
)

// ComputedBy names this library in a result.
const ComputedBy = "github.com/nexteggapp/taxcalcs/taxyear 0.1.0"

// Compute computes every scenario in the file. A problem with the file as a
// whole is in the result's problems and stops every scenario; a problem in
// one scenario is in its own problems, and the others still compute. The
// error is for a nil file only. [Cache.Compute] is the same, reusing work
// from earlier calls.
func Compute(file *scenariosv1.TaxYearScenarios) (Result, error) {
	c, _ := NewCache(len(file.GetScenarios()) + 1)
	return c.Compute(file)
}

// compute is Compute through a cache.
func compute(file *scenariosv1.TaxYearScenarios, c *Cache) Result {
	out := &scenariosv1.TaxYearResult{SchemaVersion: file.GetSchemaVersion(), TaxYear: file.GetTaxYear(),
		AsOf: file.GetAsOf(), ComputedBy: ComputedBy}
	root, shared := c.sharedFor(file)
	out.Problems = append(checkFileWith(file, shared.identifiers), shared.ids...)
	if len(out.Problems) > 0 {
		return newResult(out)
	}
	out.Status = yearStatus(file)
	out.Problems = append(out.Problems, shared.checks.problems...)
	if len(out.Problems) > 0 {
		return newResult(out)
	}
	result := newResult(out)
	result.inputs = map[string]*scenariosv1.ScenarioInputs{}
	for _, r := range c.resolve(file, root, shared) {
		res := r.failed
		if res == nil {
			res = c.computed(file, r.entry, r.id)
			if r.entry.inputs != nil {
				result.inputs[r.id] = r.entry.inputs
			}
		}
		out.Scenarios = append(out.Scenarios, res)
	}
	return result
}

// cachedScenario is one scenario in the file's order: its cache entry, or
// the result of a scenario that failed before it had one.
type cachedScenario struct {
	id     string
	entry  *scenarioEntry
	failed *scenariosv1.ScenarioResult
}

// resolve finds each scenario's entry down its parent chain, by the rules
// [Resolve] states.
func (c *Cache) resolve(file *scenariosv1.TaxYearScenarios, root key, shared *sharedEntry) []cachedScenario {
	type resolved struct {
		key    key
		inputs *scenariosv1.ScenarioInputs
	}
	byID := map[string]resolved{}
	var out []cachedScenario
	for i, s := range file.GetScenarios() {
		if text := chainProblem(i, s, func(id string) bool { return byID[id].inputs != nil }); text != "" {
			res := &scenariosv1.ScenarioResult{Scenario: s.GetId(), Problems: []*scenariosv1.Problem{problem("scenarios["+s.GetId()+"]", text)}}
			out = append(out, cachedScenario{id: s.GetId(), failed: res})
			continue
		}
		parent := resolved{key: root, inputs: shared.inputs}
		if i > 0 {
			parent = byID[s.GetParent()]
		}
		k := scenarioKey(parent.key, s)
		e := c.scenarioFor(k, parent.inputs, s, shared)
		if e.inputs != nil {
			byID[s.GetId()] = resolved{key: k, inputs: e.inputs}
		}
		out = append(out, cachedScenario{id: s.GetId(), entry: e})
	}
	return out
}

func yearStatus(file *scenariosv1.TaxYearScenarios) scenariosv1.YearStatus {
	if _, err := ustaxcalc.TableFor(int(file.GetTaxYear()), ustaxcalc.Single); err != nil {
		return scenariosv1.YearStatus_YEAR_STATUS_NO_RULES
	}
	asOf, _ := civil.FromProto(file.GetAsOf())
	if asOf.After(civil.Must(int(file.GetTaxYear()), time.December, 31)) {
		return scenariosv1.YearStatus_YEAR_STATUS_FINISHED
	}
	return scenariosv1.YearStatus_YEAR_STATUS_IN_PROGRESS
}

// computeScenario fills one scenario's result from its resolved inputs,
// keeping each model's finished projection in c, which may be nil.
func computeScenario(c *Cache, file *scenariosv1.TaxYearScenarios, in *scenariosv1.ScenarioInputs, res *scenariosv1.ScenarioResult) {
	ctx, err := model.NewContext(int(file.GetTaxYear()), file.GetAsOf(), in.GetFacts())
	if err != nil {
		res.Problems = append(res.Problems, problem("as_of", err.Error()))
		return
	}
	ms := newModels(c, ctx, in)
	if res.Problems = validateWith(ctx, in, ms); len(res.Problems) > 0 {
		return
	}
	notes := &model.Notes{}
	res.Dependents = dependents(ctx, notes)
	var working []*scenariosv1.Working
	proj, payments, err := project(ms, in, notes, &working)
	if err != nil {
		res.Problems = append(res.Problems, problem("model", err.Error()))
		return
	}
	res.Projection = proj
	res.Working = working
	if dup := duplicateItem(proj); dup != "" {
		res.Problems = append(res.Problems, problem("projection", fmt.Sprintf("Two items share the id %q.", dup)))
		return
	}
	ret, err := federal.Compute(ctx, proj, in)
	switch {
	case errors.Is(err, ustaxcalc.ErrNoTable):
		notes.LeaveOut(fmt.Sprintf("The federal return: this build has no rate tables for %d", ctx.Year()))
	case err != nil:
		res.Problems = append(res.Problems, problem("federal", err.Error()))
		return
	}
	if ret != nil {
		if err := answerPayments(ctx, in, proj, payments, ret, res, notes); err != nil {
			res.Problems = append(res.Problems, problem("estimated", err.Error()))
			return
		}
		res.Federal = ret.Form()
		if f := res.Federal.GetForm_1040(); f != nil {
			f.Dependents = dependentRows(ctx, res.Dependents)
		}
		res.Working = append(res.Working, ret.Working()...)
		notes.Merge(ret.Notes())
	}
	res.States = states(ctx, proj)
	summarise(ctx, in, ret, res)
	addWorkings(res, ms.working)
	for _, q := range notes.Questions() {
		res.Questions = append(res.Questions, q.Proto())
	}
	for _, t := range notes.LeftOut() {
		res.NotIncluded = append(res.NotIncluded, &scenariosv1.NotIncluded{Text: &scenariosv1.Statement{Text: t}, ChangesFederalTax: true})
	}
	res.NotIncluded = append(res.NotIncluded, &scenariosv1.NotIncluded{Text: &scenariosv1.Statement{Text: "State income tax, which isn't worked out yet"}})
}

// answerPayments runs the estimated-payment rules, fills the tax paid
// models' defaults, and finishes the return's payments lines.
func answerPayments(ctx *model.Context, in *scenariosv1.ScenarioInputs, proj *scenariosv1.YearProjection,
	payments []*taxpaid.Payments, ret *federal.Return, res *scenariosv1.ScenarioResult, notes *model.Notes) error {
	b := paid.NewInputs(ctx, ret.Status(), proj).Federal(ret.Tax2210(), ret.AGI(), ret.ExcessSocialSecurity()).Overrides(in.GetOverrides())
	for _, p := range payments {
		b.Payments(p)
	}
	inputs, err := b.Build()
	if err != nil {
		return err
	}
	answer, err := paid.Compute(inputs)
	if err != nil {
		return err
	}
	res.Estimated = answer.Estimated()
	notes.Merge(answer.Notes())
	proj.TaxPaid = nil
	for _, p := range payments {
		proj.TaxPaid = append(proj.TaxPaid, p.Projection())
	}
	ret.Pay(answer.FederalPayments())
	ret.SplitOverpayment(filedFederal(ctx))
	return nil
}

// holdHsa holds each person's HSA contributions through payroll to their
// limit, where it is known.
func holdHsa(ctx *model.Context, m *scenariosv1.TaxYearModel, jobs []*employer.Job, notes *model.Notes) error {
	seen := map[string]bool{}
	for _, e := range m.GetEmployers() {
		if seen[e.GetPerson()] {
			continue
		}
		seen[e.GetPerson()] = true
		limit, ok := retirement.HsaLimit(ctx, e.GetPerson(), notes)
		if !ok {
			continue
		}
		if err := employer.HoldHsa(e.GetPerson(), limit, jobs); err != nil {
			return err
		}
	}
	return nil
}

// filedFederal is the year's own filed Form 1040 once the year is over, or
// nil.
func filedFederal(ctx *model.Context) *form1040.Form1040 {
	if !ctx.Finished() {
		return nil
	}
	for _, d := range ctx.Facts().GetUnassigned() {
		if f := d.GetFederalReturn().GetForm_1040(); f != nil && model.Usable(d.GetSource()) && int(f.GetTaxYear()) == ctx.Year() {
			return f
		}
	}
	return nil
}

// project expands every model, then runs the adjustment parts: each
// person's workplace plan, then each job's computed withholding.
func project(ms *modelSet, in *scenariosv1.ScenarioInputs, notes *model.Notes, working *[]*scenariosv1.Working) (*scenariosv1.YearProjection, []*taxpaid.Payments, error) {
	ctx, m := ms.ctx, in.GetModel()
	proj := &scenariosv1.YearProjection{}
	var jobs []*employer.Job
	for _, e := range m.GetEmployers() {
		j, err := employer.Expand(ctx, e, in.GetOverrides())
		if err != nil {
			return nil, nil, err
		}
		jobs = append(jobs, j)
	}
	for _, r := range m.GetRetirement() {
		if part := retirement.PlanPart(r); part != nil {
			n, err := employer.MaxOut(ctx, r.GetPerson(), part, jobs)
			if err != nil {
				return nil, nil, err
			}
			notes.Merge(n)
		}
		p, err := ms.retirement(r)
		if err != nil {
			return nil, nil, err
		}
		proj.Retirement = append(proj.Retirement, p)
	}
	if err := holdHsa(ctx, m, jobs, notes); err != nil {
		return nil, nil, err
	}
	for _, j := range jobs {
		p, err := j.Projection()
		if err != nil {
			return nil, nil, err
		}
		proj.Employers = append(proj.Employers, p)
		notes.Merge(j.Notes())
		*working = append(*working, j.Working()...)
	}
	if err := ms.expandOthers(m, proj, notes); err != nil {
		return nil, nil, err
	}
	var payments []*taxpaid.Payments
	for _, t := range withAdjacent(ctx, m.GetTaxPaid()) {
		p, err := taxpaid.Expand(ctx, t, ctx.Facts().GetAdjacentPayments())
		if err != nil {
			return nil, nil, err
		}
		notes.Merge(p.Notes())
		payments = append(payments, p)
		proj.TaxPaid = append(proj.TaxPaid, p.Projection())
	}
	return proj, payments, nil
}

// withAdjacent is the tax paid models, and an empty one for each
// jurisdiction an adjacent year's payment for this year was made to that
// has no model, so the payment is counted. It is named tax-paid-<code>.
func withAdjacent(ctx *model.Context, ms []*scenariosv1.TaxPaidModel) []*scenariosv1.TaxPaidModel {
	has := map[string]bool{}
	for _, t := range ms {
		has[t.GetJurisdiction()] = true
	}
	out := ms
	for _, a := range ctx.Facts().GetAdjacentPayments() {
		j := a.GetJurisdiction()
		if has[j] || !taxpaid.ForYear(a, ctx.Year()) {
			continue
		}
		has[j] = true
		out = append(out[:len(out):len(out)], &scenariosv1.TaxPaidModel{Id: "tax-paid-" + strings.ToLower(j), Jurisdiction: j})
	}
	return out
}

// duplicateItem returns an item id two items share, checking the models
// doc's Validate check 2, or "".
func duplicateItem(p *scenariosv1.YearProjection) string {
	seen := map[string]bool{}
	for _, items := range allItems(p) {
		for _, it := range items {
			if seen[it.GetId()] {
				return it.GetId()
			}
			seen[it.GetId()] = true
		}
	}
	return ""
}

// allItems lists every projection's items.
func allItems(p *scenariosv1.YearProjection) [][]*scenariosv1.Item {
	var out [][]*scenariosv1.Item
	for _, x := range p.GetEmployers() {
		out = append(out, x.GetItems())
	}
	for _, x := range p.GetBusinesses() {
		out = append(out, x.GetItems())
	}
	for _, x := range p.GetAccounts() {
		out = append(out, x.GetItems())
	}
	for _, x := range p.GetBrokers() {
		out = append(out, x.GetItems())
	}
	for _, x := range p.GetRetirement() {
		out = append(out, x.GetItems())
	}
	for _, x := range p.GetItemised() {
		out = append(out, x.GetItems())
	}
	for _, x := range p.GetOtherIncome() {
		out = append(out, x.GetItems())
	}
	for _, x := range p.GetTaxPaid() {
		out = append(out, x.GetItems())
	}
	return out
}

// states lists each state lived or worked in, with its residency, and no
// return: state income tax isn't worked out yet.
func states(ctx *model.Context, proj *scenariosv1.YearProjection) []*scenariosv1.StateResult {
	lived := map[string]int{}
	var order []string
	for _, r := range ctx.Facts().GetResidence() {
		if _, ok := lived[r.GetState()]; !ok {
			order = append(order, r.GetState())
		}
		lived[r.GetState()]++
	}
	worked := map[string]bool{}
	for _, e := range proj.GetEmployers() {
		for _, s := range e.GetYear().GetBoxes().GetStates() {
			if _, ok := lived[s.GetState()]; !ok && !worked[s.GetState()] {
				worked[s.GetState()] = true
				order = append(order, s.GetState())
			}
		}
	}
	var out []*scenariosv1.StateResult
	for _, s := range order {
		r := scenariosv1.Residency_RESIDENCY_NONRESIDENT
		switch {
		case lived[s] > 0 && len(lived) == 1:
			r = scenariosv1.Residency_RESIDENCY_RESIDENT
		case lived[s] > 0:
			r = scenariosv1.Residency_RESIDENCY_PART_YEAR
		}
		out = append(out, &scenariosv1.StateResult{Jurisdiction: s, Residency: r})
	}
	return out
}

// clone copies a message, for a caller that must not share it.
func clone[T proto.Message](m T) T { return proto.Clone(m).(T) }
