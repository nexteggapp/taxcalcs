package taxyear

import (
	"fmt"
	"strings"

	"google.golang.org/protobuf/proto"

	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	"github.com/nexteggapp/taxcalcs/taxyear/model/employer"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
)

// A job's item is built around how its pay for the year is counted. The
// method field offers each way that applies, with the year's pay it would
// give; choosing one sends the whole job with that way's parts. Below it
// are the bonuses and last day, then the job's setup and the W-2 it should
// produce, closed.

// The ways a job's pay is counted: the values of its method field.
const (
	payLatest   = "same_as_latest"
	payRate     = "pay_rate"
	payYear     = "year_total"
	payLastYear = "last_year"
)

// payWays are the ways in the order offered, with their labels and
// descriptions.
var payWays = []struct{ value, label, description string }{
	{payLatest, "Paid so far, then the rest like my latest paycheck", "Your paystubs to date, then each payday repeats the latest regular paycheck."},
	{payRate, "Paid so far, then the rest at a pay rate I give", "Your paystubs to date, then a new rate from the day you give, as after a raise."},
	{payYear, "I know the full year's pay", "The year's pay as you type it. It replaces the paystubs and everything else here."},
	{payLastYear, "Same as last year's W-2", "Last year's pay, shared over this year's paydays, for a job with no paystubs yet."},
}

// employerItem is one job: its wages counted, how its pay is counted, and
// its extras, evidence, setup and W-2 boxes as parts.
func (v *viewer) employerItem(s sourced) *scenariosv1.BuilderItem {
	e, _ := s.top.Interface().(*scenariosv1.EmployerModel)
	proj := "projection.employers[" + s.id + "]"
	item := &scenariosv1.BuilderItem{Id: s.path, Label: v.modelName(s), Status: v.status(s.path), Summary: v.paySummary(e, proj)}
	v.figures(item, proj+".year.boxes.wages")
	byID := map[string]*scenariosv1.Source{}
	sourcesIn(s.top, byID, false)
	if f := v.payMethod(s, e, proj, byID); f != nil {
		item.Fields = []*scenariosv1.Field{f}
	}
	if way(e) != payYear {
		item.Children = append(item.Children, v.payExtras(s, proj))
	}
	if ev := v.partsOf(s, nil, "from_paystubs", "from_deposits", "from_w2", "ended"); len(ev) > 0 {
		item.Children = append(item.Children, &scenariosv1.BuilderItem{Id: s.path + "#evidence", Label: "Paystubs and deposits", Children: ev})
	}
	item.Children = append(item.Children, v.jobSetup(s), v.w2Boxes(s, proj))
	item.Questions = v.questions(proj, s.list+"/"+s.id, s.path)
	return item
}

// employerProj is a job's projection in the scenario's result, or nil.
func (v *viewer) employerProj(id string) *scenariosv1.EmployerProjection {
	for _, p := range v.res.GetProjection().GetEmployers() {
		if p.GetId() == id {
			return p
		}
	}
	return nil
}

// pretaxWords name the pre-tax lines in a sentence.
var pretaxWords = map[factsv1.PretaxKind]string{
	factsv1.PretaxKind_PRETAX_KIND_401K:           "401(k)",
	factsv1.PretaxKind_PRETAX_KIND_403B:           "403(b)",
	factsv1.PretaxKind_PRETAX_KIND_HSA:            "HSA",
	factsv1.PretaxKind_PRETAX_KIND_OTHER_BENEFITS: "benefits",
}

// paySummary is the job's heading sentence: "Jordan Reyes. Pay for 2026:
// $140,000. Wages counted: $124,848 after pre-tax 401(k), HSA and
// benefits."
func (v *viewer) paySummary(e *scenariosv1.EmployerModel, proj string) string {
	var out []string
	for _, p := range v.in.GetFacts().GetPeople() {
		if p.GetId() == e.GetPerson() {
			out = append(out, p.GetName()+".")
		}
	}
	year := v.employerProj(e.GetId()).GetYear().GetItems()
	out = append(out, "Pay for "+fmt.Sprint(v.year)+": "+moneyText(year.GetGross())+".")
	counted := "Wages counted: " + moneyText(moneyAt(v.res, proj+".year.boxes.wages"))
	var words []string
	for _, p := range year.GetPretax() {
		if w, ok := pretaxWords[p.GetKind()]; ok && p.GetAmount().GetMinor() != 0 {
			words = append(words, w)
		}
	}
	if len(words) > 0 {
		counted += " after pre-tax " + joinAnd(words)
	}
	return strings.Join(append(out, counted+"."), " ")
}

// way is how a job's pay is counted now, or "" when a W-2 for the year
// states it.
func way(e *scenariosv1.EmployerModel) string {
	kinds := map[string]bool{}
	for _, p := range e.GetParts() {
		_, k, _ := partCase(p.ProtoReflect())
		kinds[k] = true
	}
	switch {
	case kinds["from_w2"]:
		return ""
	case kinds["year_total"]:
		return payYear
	case kinds["pay_rate"]:
		return payRate
	case e.GetDefaultPart().GetEachPaycheck() != nil && !hasPayEvidence(e):
		return payLastYear
	}
	return payLatest
}

// hasPayEvidence reports whether a job has paystubs or deposits.
func hasPayEvidence(e *scenariosv1.EmployerModel) bool {
	for _, p := range e.GetParts() {
		if p.GetFromPaystubs() != nil || p.GetFromDeposits() != nil {
			return true
		}
	}
	return false
}

// payMethod is the field that chooses how the job's pay is counted, or nil
// when a W-2 for the year states it. An option is offered when it applies
// and its preview can be computed; the chosen one always is.
func (v *viewer) payMethod(s sourced, e *scenariosv1.EmployerModel, proj string, byID map[string]*scenariosv1.Source) *scenariosv1.Field {
	chosen := way(e)
	if chosen == "" {
		return nil
	}
	method := &scenariosv1.FieldKind_Method{}
	for _, w := range payWays {
		opt := &scenariosv1.MethodOption{Value: w.value, Label: w.label, Description: w.description}
		if w.value == chosen {
			opt.Preview = &scenariosv1.FormLine{Label: "Pay for the year", ThisScenario: moneyAt(v.res, proj+".year.items.gross")}
			opt.Fields, opt.Lines = v.payForm(s, e, proj, byID)
		} else {
			m, ok := v.payModel(s, e, w.value)
			if !ok {
				continue
			}
			gross, ok := v.previewGross(m)
			if !ok {
				continue
			}
			b, err := proto.MarshalOptions{Deterministic: true}.Marshal(m)
			if err != nil {
				continue
			}
			opt.Preview = &scenariosv1.FormLine{Label: "Pay for the year", ThisScenario: gross}
			opt.Edit = v.edit(s.path, &scenariosv1.Value{Kind: &scenariosv1.Value_Message{Message: b}})
		}
		method.Options = append(method.Options, opt)
	}
	return &scenariosv1.Field{Path: s.path, Label: "How should we count this year's pay?",
		Kind:  &scenariosv1.FieldKind{Kind: &scenariosv1.FieldKind_Method_{Method: method}},
		Value: &scenariosv1.Value{Kind: &scenariosv1.Value_Choice{Choice: chosen}}}
}

// payModel is the job with its pay counted another way: its assumed parts
// for the old way removed and the new way's added, filled as the
// projection offers them. It is false when the way doesn't apply.
func (v *viewer) payModel(s sourced, e *scenariosv1.EmployerModel, w string) (*scenariosv1.EmployerModel, bool) {
	m := proto.Clone(e).(*scenariosv1.EmployerModel)
	evidence := hasPayEvidence(e)
	switch w {
	case payLatest:
		if !evidence {
			return nil, false
		}
		dropParts(m, "pay_rate", "year_total")
		m.DefaultPart = &scenariosv1.EmployerDefault{Id: v.defaultID(e), Rest: &scenariosv1.EmployerDefault_SameAsLatest{SameAsLatest: &scenariosv1.SameAsLatest{}}}
	case payRate, payYear:
		offer := v.offered(e.GetId(), w)
		if offer == nil {
			return nil, false
		}
		if w == payRate {
			dropParts(m, "year_total")
		} else {
			dropParts(m, "pay_rate", "year_total", "one_off", "ends")
		}
		offer.Id = newPartID(e.GetId(), v.id, s.path, w, len(m.GetParts()))
		m.Parts = append(m.Parts, offer)
	case payLastYear:
		each, ok := employer.LastYearPaycheck(v.year, e)
		if !ok || evidence {
			return nil, false
		}
		dropParts(m, "pay_rate", "year_total")
		m.DefaultPart = &scenariosv1.EmployerDefault{Id: v.defaultID(e), Rest: &scenariosv1.EmployerDefault_EachPaycheck{EachPaycheck: each}}
	}
	return m, true
}

// offered is a copy of the part of a kind the job's projection offers to
// add, filled with its defaults, or nil.
func (v *viewer) offered(id, kind string) *scenariosv1.EmployerPart {
	for _, p := range v.employerProj(id).GetCanAdd() {
		if _, k, _ := partCase(p.ProtoReflect()); k == kind {
			return proto.Clone(p).(*scenariosv1.EmployerPart)
		}
	}
	return nil
}

// dropParts removes a job's parts of the given kinds.
func dropParts(m *scenariosv1.EmployerModel, kinds ...string) {
	kept := m.Parts[:0]
	for _, p := range m.GetParts() {
		if _, k, _ := partCase(p.ProtoReflect()); !has(kinds, k) {
			kept = append(kept, p)
		}
	}
	m.Parts = kept
}

// defaultID is the job's default part's id, or a new one.
func (v *viewer) defaultID(e *scenariosv1.EmployerModel) string {
	if id := e.GetDefaultPart().GetId(); id != "" {
		return id
	}
	return newPartID(e.GetId(), v.id, "default_part")
}

// withEmployer is the scenario's inputs with one job replaced.
func (v *viewer) withEmployer(m *scenariosv1.EmployerModel) *scenariosv1.ScenarioInputs {
	in := proto.Clone(v.in).(*scenariosv1.ScenarioInputs)
	for i, e := range in.GetModel().GetEmployers() {
		if e.GetId() == m.GetId() {
			in.Model.Employers[i] = m
		}
	}
	return in
}

// previewGross is the year's pay a job would have, expanded on its own,
// or false when the scenario would have a problem with it. Its gross pay
// depends on nothing the other models change.
func (v *viewer) previewGross(m *scenariosv1.EmployerModel) (*commonv1.Money, bool) {
	ctx, err := model.NewContext(v.year, v.asOf, v.in.GetFacts())
	if err != nil || len(Validate(ctx, v.withEmployer(m))) > 0 {
		return nil, false
	}
	j, err := employer.Expand(ctx, m, v.in.GetOverrides())
	if err != nil {
		return nil, false
	}
	p, err := j.Projection()
	if err != nil || p.GetYear().GetItems().GetGross() == nil {
		return nil, false
	}
	return p.GetYear().GetItems().GetGross(), true
}
