package taxyear

import (
	"errors"
	"fmt"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
)

// BuildView describes the builder page for one scenario of a computed file,
// beside Your plan: each group's items with their figures, fields, sources
// and computed lines. r must be Compute's result for file. An item exists
// because the scenario's inputs hold what it shows: a person, a residence,
// a model, a jurisdiction with payments.
func BuildView(file *scenariosv1.TaxYearScenarios, r Result, scenario string) (*scenariosv1.BuilderView, error) {
	v, err := newViewer(file, r, scenario)
	if err != nil {
		return nil, err
	}
	out := &scenariosv1.BuilderView{Scenario: scenario, Problems: r.Message().GetProblems()}
	out.Scenarios, out.NewScenario = scenarioTabs(file, scenario)
	if len(out.Problems) > 0 {
		return out, nil
	}
	out.Problems = v.res.GetProblems()
	if v.in == nil || len(out.Problems) > 0 {
		return out, nil
	}
	groups := []*scenariosv1.Group{
		{Id: "about", Label: "About you", Items: v.about()},
		{Id: "income", Label: "Income", Total: v.line("federal.form_1040.total_income", "Total income"), Items: v.income()},
		{Id: "deductions", Label: "Deductions", Total: v.takenOff(), Items: v.deductions()},
		{Id: "tax_paid", Label: "Tax paid", Items: v.taxPaid()},
		{Id: "last_year", Label: "From last year", Items: v.lastYear()},
	}
	for _, g := range groups {
		if len(g.Items) > 0 {
			out.Groups = append(out.Groups, g)
		}
	}
	out.Lines = v.form1040()
	out.Headline = v.headline()
	out.Questions = v.unplaced()
	out.NotIncluded = v.res.GetNotIncluded()
	return out, nil
}

// viewer holds what BuildView reads: the scenario's and Your plan's inputs
// and results, and the inputs up the scenario's parent chain.
type viewer struct {
	id       string
	chain    []string // the scenario, then each parent up to Your plan
	names    map[string]string
	inputs   map[string]*scenariosv1.ScenarioInputs
	in, plan *scenariosv1.ScenarioInputs
	res      *scenariosv1.ScenarioResult
	planRes  *scenariosv1.ScenarioResult // nil when the scenario is Your plan
	working  map[string]*scenariosv1.Working
	asked    map[string]bool // questions placed on an item
	year     int
	asOf     *commonv1.Date
}

func newViewer(file *scenariosv1.TaxYearScenarios, r Result, id string) (*viewer, error) {
	if r.Message() == nil {
		return nil, errors.New("taxyear: no result")
	}
	v := &viewer{year: int(file.GetTaxYear()), asOf: file.GetAsOf(), id: id, names: map[string]string{}, inputs: map[string]*scenariosv1.ScenarioInputs{}, working: map[string]*scenariosv1.Working{}, asked: map[string]bool{}}
	parents := map[string]string{}
	for _, s := range file.GetScenarios() {
		v.names[s.GetId()] = s.GetName()
		parents[s.GetId()] = s.GetParent()
		if in, ok := r.Inputs(s.GetId()); ok {
			v.inputs[s.GetId()] = in
		}
	}
	if _, ok := v.names[id]; !ok {
		return nil, fmt.Errorf("taxyear: no scenario %q", id)
	}
	for at, seen := id, map[string]bool{}; at != "" && !seen[at]; at = parents[at] {
		seen[at] = true
		v.chain = append(v.chain, at)
	}
	v.in, v.plan = v.inputs[id], v.inputs["plan"]
	s, _ := r.Scenario(id)
	v.res = s.p
	if v.res == nil {
		v.res = &scenariosv1.ScenarioResult{Scenario: id}
	}
	if id != "plan" {
		if p, ok := r.Scenario("plan"); ok {
			v.planRes = p.p
		}
	}
	for _, w := range v.res.GetWorking() {
		v.working[w.GetPath()] = w
	}
	return v, nil
}

// status compares a path's value in each scenario up the chain with its
// parent's, stopping at Your plan.
func (v *viewer) status(path string) *scenariosv1.Status {
	p, err := ParsePath(path)
	if err != nil {
		return nil
	}
	for i := 0; i+1 < len(v.chain); i++ {
		a, b := v.inputs[v.chain[i]], v.inputs[v.chain[i+1]]
		if a == nil || b == nil || sameAt(a.ProtoReflect(), b.ProtoReflect(), p) {
			continue
		}
		if i == 0 {
			return &scenariosv1.Status{Kind: scenariosv1.StatusKind_STATUS_KIND_CHANGED_HERE}
		}
		return &scenariosv1.Status{Kind: scenariosv1.StatusKind_STATUS_KIND_CHANGED_IN_PARENT, ChangedIn: v.chain[i], ChangedInName: v.names[v.chain[i]]}
	}
	return nil
}

// sameAt reports whether two messages hold the same value at a path.
func sameAt(a, b protoreflect.Message, p Path) bool {
	va, fa, oka := lookup(a, p)
	vb, _, okb := lookup(b, p)
	switch {
	case !oka || !okb:
		return oka == okb
	case wholeList(fa, p):
		la, lb := va.List(), vb.List()
		if la.Len() != lb.Len() {
			return false
		}
		for i := 0; i < la.Len(); i++ {
			if !sameValue(fa, la.Get(i), lb.Get(i)) {
				return false
			}
		}
		return true
	}
	return sameValue(fa, va, vb)
}

// wholeList reports whether lookup found a whole list rather than one of
// its items.
func wholeList(fd protoreflect.FieldDescriptor, p Path) bool {
	return fd.IsList() && !p.steps[len(p.steps)-1].hasKey
}

func sameValue(fd protoreflect.FieldDescriptor, a, b protoreflect.Value) bool {
	if fd.Kind() == protoreflect.MessageKind || fd.Kind() == protoreflect.GroupKind {
		ma, mb := a.Message().Interface(), b.Message().Interface()
		return ma == mb || proto.Equal(ma, mb)
	}
	return a.Equal(b)
}

// moneyAt is the money at a path in a result, or nil.
func moneyAt(res *scenariosv1.ScenarioResult, path string) *commonv1.Money {
	if res == nil {
		return nil
	}
	p, err := ParsePath(path)
	if err != nil {
		return nil
	}
	val, fd, ok := lookup(res.ProtoReflect(), p)
	if !ok || wholeList(fd, p) || fd.Kind() != protoreflect.MessageKind {
		return nil
	}
	m, _ := val.Message().Interface().(*commonv1.Money)
	return m
}

// sumAt adds the money at paths in a result; nil when none is set.
func sumAt(res *scenariosv1.ScenarioResult, paths ...string) *commonv1.Money {
	var out *commonv1.Money
	for _, p := range paths {
		if m := moneyAt(res, p); m != nil {
			if out == nil {
				out = &commonv1.Money{Currency: m.GetCurrency()}
			}
			out.Minor += m.GetMinor()
		}
	}
	return out
}

// figures is the item figure at paths in this scenario and Your plan.
func (v *viewer) figures(item *scenariosv1.BuilderItem, paths ...string) {
	path := ""
	if len(paths) == 1 {
		path = paths[0]
	}
	if m := sumAt(v.res, paths...); m != nil {
		item.ThisScenario = &scenariosv1.Figure{Amount: m, Path: path}
	}
	if m := sumAt(v.planRes, paths...); m != nil {
		item.YourPlan = &scenariosv1.Figure{Amount: m, Path: path}
	}
}

// line is one computed figure, or nil when neither scenario has it.
func (v *viewer) line(path, label string) *scenariosv1.FormLine {
	this, plan := moneyAt(v.res, path), moneyAt(v.planRes, path)
	if this == nil && plan == nil {
		return nil
	}
	return &scenariosv1.FormLine{Path: path, Label: label, ThisScenario: this, YourPlan: plan, Working: v.working[path]}
}

// form1040 is Form 1040's lines that are not zero in this scenario or Your
// plan, with their numbers and plain labels.
func (v *viewer) form1040() []*scenariosv1.FormLine {
	lines := v.formLines("federal.form_1040", v.res.GetFederal().GetForm_1040())
	for _, l := range lines {
		if fl, ok := form1040Line(l.GetPath()); ok {
			l.Line, l.Label = fl.number, fl.label
		}
		if l.GetPath() == form1040Prefix+"deduction" {
			l.Label = "Standard deduction"
			if v.res.GetFederal().GetForm_1040().GetItemized() {
				l.Label = "Itemised deductions"
			}
		}
	}
	return lines
}

// formLines lists a result message's money leaves that are not zero in
// this scenario or Your plan, as lines, by reflection.
func (v *viewer) formLines(at string, m proto.Message) []*scenariosv1.FormLine {
	var out []*scenariosv1.FormLine
	if m == nil || !m.ProtoReflect().IsValid() {
		return nil
	}
	for _, leaf := range moneyLeaves(at, "", m.ProtoReflect()) {
		l := v.line(leaf.path, leaf.label)
		if l != nil && (l.GetThisScenario().GetMinor() != 0 || l.GetYourPlan().GetMinor() != 0) {
			out = append(out, l)
		}
	}
	return out
}

// leaf is one money field found by reflection.
type leaf struct{ path, label string }

// moneyLeaves lists the set money fields under a message, with their paths
// and labels, list items by key.
func moneyLeaves(at, label string, m protoreflect.Message) []leaf {
	var out []leaf
	fields := m.Descriptor().Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		if fd.Kind() != protoreflect.MessageKind || !(m.Has(fd) || fd.IsList()) {
			continue
		}
		name := at + "." + string(fd.Name())
		lab := joinLabel(label, humanise(string(fd.Name())))
		if fd.IsList() {
			l := m.Get(fd).List()
			for k := 0; k < l.Len(); k++ {
				el := l.Get(k).Message()
				key, elLabel := listKey(fd, el, k)
				out = append(out, moneyLeaves(name+"["+key+"]", joinLabel(lab, elLabel), el)...)
			}
			continue
		}
		if fd.Message().FullName() == moneyName {
			out = append(out, leaf{path: name, label: lab})
			continue
		}
		out = append(out, moneyLeaves(name, lab, m.Get(fd).Message())...)
	}
	return out
}

const (
	moneyName    = "nexteggapp.taxprotos.common.v1.Money"
	dateName     = "nexteggapp.taxprotos.common.v1.Date"
	quantityName = "nexteggapp.taxprotos.common.v1.Quantity"
	docRefName   = "nexteggapp.taxprotos.facts.v1.DocumentRef"
	ledgerName   = "nexteggapp.taxprotos.facts.v1.LedgerRef"
)

// listKey is a list item's key in a path, as element finds it, and its
// label: the key, else its first enum's value, else its position from 1.
func listKey(fd protoreflect.FieldDescriptor, el protoreflect.Message, i int) (string, string) {
	d := fd.Message()
	for _, name := range keyFields {
		if kf := d.Fields().ByName(name); kf != nil && kf.Kind() == protoreflect.StringKind {
			k := el.Get(kf).String()
			return k, k
		}
	}
	fields := d.Fields()
	for j := 0; j < fields.Len(); j++ {
		if f := fields.Get(j); f.Kind() == protoreflect.EnumKind && !f.IsList() {
			if ev := f.Enum().Values().ByNumber(el.Get(f).Enum()); ev != nil {
				return fmt.Sprint(i), enumLabel(f.Enum(), ev)
			}
		}
	}
	return fmt.Sprint(i), fmt.Sprint(i + 1)
}

func joinLabel(a, b string) string {
	if a == "" || b == "" {
		return a + b
	}
	if _, state := stateNames[a]; state {
		return a + " " + lowerFirst(b)
	}
	return a + ", " + lowerFirst(b)
}

// lowerFirst lowers a label's first letter, unless it starts an acronym.
func lowerFirst(b string) string {
	for _, w := range properWords {
		if strings.HasPrefix(b, w) {
			return b
		}
	}
	if len(b) > 1 && b[1] >= 'a' && b[1] <= 'z' {
		return strings.ToLower(b[:1]) + b[1:]
	}
	return b
}

// humanise turns a field or case name into a label: "pay_rate" is "Pay
// rate".
func humanise(name string) string {
	if p, ok := plainNames[name]; ok {
		return p
	}
	words := strings.Split(name, "_")
	for i, w := range words {
		if a, ok := acronyms[w]; ok {
			words[i] = a
		}
	}
	s := strings.Join(words, " ")
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// acronyms are words humanise writes in capitals.
var acronyms = map[string]string{
	"hsa": "HSA", "ira": "IRA", "agi": "AGI", "w2": "W-2", "w4": "W-4", "401k": "401(k)", "403b": "403(b)",
	"salt": "SALT", "rmd": "RMD", "us": "US", "ss": "SS", "qbi": "QBI", "amt": "AMT", "1098": "1098", "id": "ID",
}

// enumLabel is an enum value's label, without the enum's prefix:
// PAY_FREQUENCY_BIWEEKLY is "Biweekly".
func enumLabel(e protoreflect.EnumDescriptor, v protoreflect.EnumValueDescriptor) string {
	name := string(v.Name())
	if zero := e.Values().ByNumber(0); zero != nil {
		prefix := strings.TrimSuffix(string(zero.Name()), "UNSPECIFIED")
		name = strings.TrimPrefix(name, prefix)
	}
	if p, ok := enumNames[name]; ok {
		return p
	}
	return humanise(strings.ToLower(name))
}

// sourcesIn lists the documents and bank entries under a message, once
// each, and indexes the evidence entries they belong to by id. A model's
// parts list their own, so noParts leaves them out.
func sourcesIn(m protoreflect.Message, byID map[string]*scenariosv1.Source, noParts bool) []*scenariosv1.Source {
	var out []*scenariosv1.Source
	seen := map[string]bool{}
	var walk func(m protoreflect.Message)
	walk = func(m protoreflect.Message) {
		var id string
		var src *scenariosv1.Source
		m.Range(func(fd protoreflect.FieldDescriptor, val protoreflect.Value) bool {
			switch {
			case fd.Name() == "id" && fd.Kind() == protoreflect.StringKind:
				id = val.String()
			case fd.Kind() != protoreflect.MessageKind, noParts && fd.Name() == "parts":
			case fd.IsList():
				for i := 0; i < val.List().Len(); i++ {
					walk(val.List().Get(i).Message())
				}
			case fd.Message().FullName() == docRefName:
				ref := val.Message().Interface().(*factsv1.DocumentRef)
				src = &scenariosv1.Source{Label: ref.GetName(), Ref: &scenariosv1.Source_Document{Document: ref}}
			case fd.Message().FullName() == ledgerName:
				ref := val.Message().Interface().(*factsv1.LedgerRef)
				src = &scenariosv1.Source{Label: ledgerLabel(ref), Ref: &scenariosv1.Source_Ledger{Ledger: ref}}
			case strings.HasPrefix(string(fd.Message().FullName()), "nexteggapp.taxprotos."):
			default:
				walk(val.Message())
			}
			return true
		})
		if src == nil {
			return
		}
		if id != "" && byID != nil {
			byID[id] = src
		}
		k := src.String()
		if !seen[k] {
			seen[k] = true
			out = append(out, src)
		}
	}
	walk(m)
	return out
}

func ledgerLabel(ref *factsv1.LedgerRef) string {
	if d := ref.GetPosted(); d != nil {
		return fmt.Sprintf("Bank entry, %04d-%02d-%02d", d.GetYear(), d.GetMonth(), d.GetDay())
	}
	return "Bank entry"
}

// question places each question asked at one of prefixes on an item.
func (v *viewer) questions(prefixes ...string) []*scenariosv1.QuestionView {
	var out []*scenariosv1.QuestionView
	for _, q := range v.res.GetQuestions() {
		if v.asked[q.GetId()] {
			continue
		}
		for _, p := range prefixes {
			if q.GetAt() == p || strings.HasPrefix(q.GetAt(), p+"/") || strings.HasPrefix(q.GetAt(), p+".") || strings.HasPrefix(q.GetAt(), p+"[") {
				v.asked[q.GetId()] = true
				out = append(out, v.questionView(q))
				break
			}
		}
	}
	return out
}

// unplaced is every question no item holds.
func (v *viewer) unplaced() []*scenariosv1.QuestionView {
	var out []*scenariosv1.QuestionView
	for _, q := range v.res.GetQuestions() {
		if !v.asked[q.GetId()] {
			out = append(out, v.questionView(q))
		}
	}
	return out
}

// questionView offers a question's answers as edits where the answer is a
// position on a person: Yes, No or Not sure.
func (v *viewer) questionView(q *scenariosv1.Question) *scenariosv1.QuestionView {
	out := &scenariosv1.QuestionView{Question: q}
	person := strings.TrimSuffix(strings.TrimPrefix(q.GetAt(), "dependents["), "]")
	if q.GetAnswerPath() != "facts.positions" || person == q.GetAt() || !strings.HasSuffix(q.GetId(), "."+person) {
		return out
	}
	catalogue := strings.TrimSuffix(q.GetId(), "."+person)
	id := q.GetId()
	for _, pos := range v.in.GetFacts().GetPositions() {
		if pos.GetQuestion() == catalogue && has(pos.GetSubjects(), person) {
			id = pos.GetId()
		}
	}
	replies := map[string]factsv1.Reply{"Yes": factsv1.Reply_REPLY_YES, "No": factsv1.Reply_REPLY_NO, "Not sure": factsv1.Reply_REPLY_NOT_SURE}
	for _, a := range q.GetAnswers() {
		r, ok := replies[a.GetText()]
		if !ok {
			return &scenariosv1.QuestionView{Question: q}
		}
		b, _ := proto.MarshalOptions{Deterministic: true}.Marshal(&factsv1.Position{Id: id, Question: catalogue, Subjects: []string{person}, Answer: &factsv1.Position_Reply{Reply: r}})
		out.Answers = append(out.Answers, &scenariosv1.Action{Label: a.GetText(), Edit: &scenariosv1.Edit{
			Scenario: v.id, Path: "facts.positions[" + id + "]", Value: &scenariosv1.Value{Kind: &scenariosv1.Value_Message{Message: b}},
		}})
	}
	return out
}
