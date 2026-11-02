package taxyear

import (
	"crypto/sha256"
	"fmt"
	"math/big"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
)

// about lists the filing status and other elections, the people and where
// they lived.
func (v *viewer) about() []*scenariosv1.BuilderItem {
	var out []*scenariosv1.BuilderItem
	f := v.in.GetFacts()
	for _, e := range f.GetElections() {
		path := "facts.elections[" + e.GetId() + "]"
		label := "Election"
		if which := e.ProtoReflect().WhichOneof(e.ProtoReflect().Descriptor().Oneofs().ByName("choice")); which != nil {
			label = humanise(string(which.Name()))
		}
		out = append(out, &scenariosv1.BuilderItem{Id: path, Label: label, Status: v.status(path), Fields: v.fields(path, e.ProtoReflect(), "")})
	}
	for _, p := range f.GetPeople() {
		path := "facts.people[" + p.GetId() + "]"
		item := &scenariosv1.BuilderItem{Id: path, Label: p.GetName(), Status: v.status(path), Fields: v.fields(path, p.ProtoReflect(), ""),
			Questions: v.questions("dependents[" + p.GetId() + "]")}
		for _, d := range v.res.GetDependents() {
			if d.GetPerson() == p.GetId() && d.GetText().GetText() != "" {
				item.Notes = append(item.Notes, d.GetText())
			}
		}
		out = append(out, item)
	}
	for _, r := range f.GetResidence() {
		path := "facts.residence[" + r.GetId() + "]"
		out = append(out, &scenariosv1.BuilderItem{Id: path, Label: "Lived in " + r.GetState(), Status: v.status(path), Fields: v.fields(path, r.ProtoReflect(), "")})
	}
	return out
}

// headlines are each list's figure for the year, relative to its
// projection, as the spec's row table gives them.
var headlines = map[string][]string{
	"employers":  {"year.items.gross"},
	"businesses": {"year.boxes.net_profit"},
	"accounts":   {"year.items.interest", "year.items.ordinary_dividends"},
	"brokers":    {"year.items.short_term_gain", "year.items.long_term_gain"},
	"retirement": {"year.items.traditional_ira", "year.items.roth_ira", "year.items.hsa"},
	"itemised":   {"year.items"},
	"tax_paid":   {"year.items"},
}

// modelItems lists the models in the given lists, one item each.
func (v *viewer) modelItems(lists ...string) []*scenariosv1.BuilderItem {
	var out []*scenariosv1.BuilderItem
	for _, list := range lists {
		for _, s := range models(v.in.GetModel()) {
			if s.list == list {
				out = append(out, v.modelItem(s))
			}
		}
	}
	return out
}

// modelItem is one model: its figure, details and figures so far as
// fields, its year's lines, and its parts as children.
func (v *viewer) modelItem(s sourced) *scenariosv1.BuilderItem {
	inner := s.path
	if o := s.top.Descriptor().Oneofs().ByName("kind"); o != nil {
		if which := s.top.WhichOneof(o); which != nil {
			inner = s.path + "." + string(which.Name())
		}
	}
	proj := "projection." + s.list + "[" + s.id + "]"
	item := &scenariosv1.BuilderItem{Id: s.path, Label: v.modelLabel(s), Method: method(s.msg), Status: v.status(s.path)}
	byID := map[string]*scenariosv1.Source{}
	sourcesIn(s.top, byID, false)
	item.Sources = sourcesIn(s.top, nil, true)
	var paths []string
	for _, h := range headlines[s.list] {
		paths = append(paths, proj+"."+h)
	}
	if s.list == "other_income" {
		paths = []string{"summary.other_income[" + s.id + "].taxable"}
	}
	v.figures(item, paths...)
	item.Fields = append(v.fields(inner, s.msg, ""), v.soFar(s.list, proj, byID)...)
	for _, l := range v.linesAt(proj + ".year.boxes") {
		item.Displays = append(item.Displays, &scenariosv1.Display{Kind: &scenariosv1.Display_Figure{Figure: l}})
	}
	item.Displays = append(item.Displays, v.summaryDisplays(s)...)
	item.Children = v.parts(s, inner)
	item.Actions = v.canAdd(s, inner, proj)
	item.Questions = v.questions(proj, s.list+"/"+s.id, s.path)
	return item
}

// modelLabel is a model's name, or what names it best without one.
func (v *viewer) modelLabel(s sourced) string {
	if o := s.top.Descriptor().Oneofs().ByName("kind"); o != nil && s.top.WhichOneof(o) != nil {
		return humanise(string(s.top.WhichOneof(o).Name()))
	}
	for _, m := range []protoreflect.Message{s.top, s.msg} {
		if fd := m.Descriptor().Fields().ByName("name"); fd != nil && m.Get(fd).String() != "" {
			return m.Get(fd).String()
		}
	}
	if fd := s.top.Descriptor().Fields().ByName("person"); fd != nil {
		for _, p := range v.in.GetFacts().GetPeople() {
			if p.GetId() == s.top.Get(fd).String() {
				return p.GetName()
			}
		}
	}
	if fd := s.top.Descriptor().Fields().ByName("jurisdiction"); fd != nil {
		return s.top.Get(fd).String()
	}
	return s.id
}

// method is a model's default part, as one line: "Same as latest".
func method(m protoreflect.Message) string {
	fd := m.Descriptor().Fields().ByName("default_part")
	if fd == nil || !m.Has(fd) {
		return ""
	}
	dp := m.Get(fd).Message()
	if o := dp.Descriptor().Oneofs().ByName("rest"); o != nil {
		if which := dp.WhichOneof(o); which != nil {
			return humanise(string(which.Name()))
		}
	}
	return ""
}

// summaryDisplays are the limits the scenario's summary holds for a model:
// an itemised deduction's floor, a person's retirement limits, how other
// income reaches the return.
func (v *viewer) summaryDisplays(s sourced) []*scenariosv1.Display {
	var out []*scenariosv1.Display
	switch s.list {
	case "itemised":
		at := "summary.itemised[" + s.id + "]"
		if moneyAt(v.res, at+".floor") != nil {
			out = append(out, &scenariosv1.Display{Kind: &scenariosv1.Display_LimitLines{LimitLines: &scenariosv1.LimitLines{
				ForYear: v.line("projection.itemised["+s.id+"].year.items", "For the year"),
				Limit:   v.line(at+".floor", "Doesn't count"),
				Counted: v.line(at+".counted", "Counted"),
			}}})
		}
	case "retirement":
		person := s.top.Get(s.top.Descriptor().Fields().ByName("person")).String()
		for _, l := range v.linesAt("summary.retirement[" + person + "]") {
			out = append(out, &scenariosv1.Display{Kind: &scenariosv1.Display_Figure{Figure: l}})
		}
	case "other_income":
		for _, l := range v.linesAt("summary.other_income[" + s.id + "]") {
			out = append(out, &scenariosv1.Display{Kind: &scenariosv1.Display_Figure{Figure: l}})
		}
	}
	return out
}

// linesAt is formLines for the message at a path in the scenario's result.
func (v *viewer) linesAt(path string) []*scenariosv1.FormLine {
	p, err := ParsePath(path)
	if err != nil {
		return nil
	}
	val, fd, ok := lookup(v.res.ProtoReflect(), p)
	if !ok || wholeList(fd, p) || fd.Kind() != protoreflect.MessageKind {
		return nil
	}
	return v.formLines(path, val.Message().Interface())
}

// parts lists a model's parts, then its default part, as children. A known
// part holds evidence and shows its sources; an assumed part's fields can
// be edited, and it can be removed.
func (v *viewer) parts(s sourced, inner string) []*scenariosv1.BuilderItem {
	var out []*scenariosv1.BuilderItem
	known := partKinds[s.partName()].known
	for i := 0; s.parts != nil && i < s.parts.Len(); i++ {
		part := s.parts.Get(i).Message()
		id, kind, body := partCase(part)
		path := inner + ".parts[" + id + "]"
		child := &scenariosv1.BuilderItem{Id: path, Label: humanise(kind), Status: v.status(path)}
		if has(known, kind) {
			child.Sources = sourcesIn(part, nil, false)
		} else if body != nil {
			child.Fields = v.fields(path+"."+kind, body, "")
			child.Actions = []*scenariosv1.Action{{Label: "Remove", Edit: v.edit(path, &scenariosv1.Value{Kind: &scenariosv1.Value_Remove_{Remove: &scenariosv1.Value_Remove{}}})}}
		}
		out = append(out, child)
	}
	if fd := s.msg.Descriptor().Fields().ByName("default_part"); fd != nil && s.msg.Has(fd) {
		path := inner + ".default_part"
		out = append(out, &scenariosv1.BuilderItem{Id: path, Label: "The rest of the year", Method: method(s.msg), Status: v.status(path),
			Fields: v.fields(path, s.msg.Get(fd).Message(), "")})
	}
	return out
}

// canAdd offers each part kind the projection says can be added now,
// filled with its defaults, under a new id.
func (v *viewer) canAdd(s sourced, inner, proj string) []*scenariosv1.Action {
	p, err := ParsePath(proj + ".can_add")
	if err != nil {
		return nil
	}
	val, fd, ok := lookup(v.res.ProtoReflect(), p)
	if !ok || !wholeList(fd, p) {
		return nil
	}
	var out []*scenariosv1.Action
	l := val.List()
	for i := 0; i < l.Len(); i++ {
		part := l.Get(i).Message()
		if part.Descriptor().Name() == "ItemisedPartChoice" {
			o := part.Descriptor().Oneofs().ByName("part")
			which := part.WhichOneof(o)
			if which == nil || inner != s.path+"."+string(which.Name()) {
				continue
			}
			part = part.Get(which).Message()
		}
		_, kind, _ := partCase(part)
		n := 0
		if s.parts != nil {
			n = s.parts.Len()
		}
		id := newPartID(s.id, v.id, inner, kind, n)
		m := proto.Clone(part.Interface())
		m.ProtoReflect().Set(m.ProtoReflect().Descriptor().Fields().ByName("id"), protoreflect.ValueOfString(id))
		b, err := proto.MarshalOptions{Deterministic: true}.Marshal(m)
		if err != nil {
			continue
		}
		out = append(out, &scenariosv1.Action{Label: "Add: " + strings.ToLower(humanise(kind)),
			Edit: v.edit(inner+".parts["+id+"]", &scenariosv1.Value{Kind: &scenariosv1.Value_Message{Message: b}})})
	}
	return out
}

// newPartID is a model's id and 8 letters and digits, the same for the
// same scenario, model, kind and number of parts, so a view is
// reproducible.
func newPartID(model string, keys ...any) string {
	h := sha256.Sum256([]byte(fmt.Sprint(keys...)))
	s := new(big.Int).SetBytes(h[:]).Text(36)
	return model + "-" + s[:8]
}

func (v *viewer) edit(path string, val *scenariosv1.Value) *scenariosv1.Edit {
	return &scenariosv1.Edit{Scenario: v.id, Path: path, Value: val}
}

// taxPaid lists each jurisdiction's payments, with its tax paid models as
// children.
func (v *viewer) taxPaid() []*scenariosv1.BuilderItem {
	var out []*scenariosv1.BuilderItem
	placed := map[string]bool{}
	for _, e := range v.res.GetEstimated() {
		j := e.GetJurisdiction()
		at := "estimated[" + j + "]"
		item := &scenariosv1.BuilderItem{Id: at, Label: jurisdictionLabel(j)}
		v.figures(item, at+".summary.with_return")
		if l := v.line(at+".tax_for_year", "Tax for the year"); l != nil {
			item.Displays = append(item.Displays, &scenariosv1.Display{Kind: &scenariosv1.Display_Figure{Figure: l}})
		}
		for _, l := range v.linesAt(at + ".summary") {
			item.Displays = append(item.Displays, &scenariosv1.Display{Kind: &scenariosv1.Display_Figure{Figure: l}})
		}
		if j != "US" {
			item.Fields = append(item.Fields, v.override(at+".tax_for_year", "Tax for the year", nil))
		}
		for _, s := range models(v.in.GetModel()) {
			if s.list == "tax_paid" && s.top.Get(s.top.Descriptor().Fields().ByName("jurisdiction")).String() == j {
				placed[s.id] = true
				item.Children = append(item.Children, v.modelItem(s))
			}
		}
		for _, c := range append(item.GetChildren(), &scenariosv1.BuilderItem{Status: v.overrideStatus(at + ".tax_for_year")}) {
			if c.GetStatus() != nil {
				item.Status = c.GetStatus()
				break
			}
		}
		item.Questions = v.questions(at)
		out = append(out, item)
	}
	for _, s := range models(v.in.GetModel()) {
		if s.list == "tax_paid" && !placed[s.id] {
			out = append(out, v.modelItem(s))
		}
	}
	return out
}

func jurisdictionLabel(j string) string {
	if j == "US" {
		return "Federal"
	}
	return j
}

// lastYear lists last year's figures for each jurisdiction, which can be
// overridden, then the carryovers.
func (v *viewer) lastYear() []*scenariosv1.BuilderItem {
	var out []*scenariosv1.BuilderItem
	for _, e := range v.res.GetEstimated() {
		j := e.GetJurisdiction()
		at := "estimated[" + j + "]"
		item := &scenariosv1.BuilderItem{Id: "last_year[" + j + "]", Label: jurisdictionLabel(j) + " return"}
		var sources []*scenariosv1.Source
		for _, p := range v.in.GetFacts().GetPriorYear() {
			if p.GetJurisdiction() == j {
				item.Id = "facts.prior_year[" + p.GetId() + "]"
				sources = sourcesIn(p.ProtoReflect(), nil, false)
				item.Fields = v.fields(item.Id, p.ProtoReflect(), "")
				item.Status = v.status(item.Id)
				break
			}
		}
		item.Sources = sources
		for _, f := range []struct{ name, label string }{{"last_year_tax", "Last year's tax"}, {"last_year_agi", "Last year's AGI"}, {"last_year_withheld", "Last year's withholding"}} {
			if moneyAt(v.res, at+"."+f.name) != nil {
				item.Fields = append(item.Fields, v.override(at+"."+f.name, f.label, sources))
			}
		}
		if len(item.Fields) > 0 {
			out = append(out, item)
		}
	}
	for _, c := range v.in.GetFacts().GetCarryovers() {
		path := "facts.carryovers[" + c.GetId() + "]"
		ev := c.ProtoReflect().Descriptor().Fields().ByName("kind").Enum().Values().ByNumber(protoreflect.EnumNumber(c.GetKind()))
		label := "Carryover"
		if ev != nil {
			label = enumLabel(c.ProtoReflect().Descriptor().Fields().ByName("kind").Enum(), ev)
		}
		out = append(out, &scenariosv1.BuilderItem{Id: path, Label: label, Status: v.status(path), Fields: v.fields(path, c.ProtoReflect(), "")})
	}
	return out
}

// soFar lists a model's figures so far as fields that can be overridden.
// Tax paid has none.
func (v *viewer) soFar(list, proj string, byID map[string]*scenariosv1.Source) []*scenariosv1.Field {
	if list == "tax_paid" {
		return nil
	}
	p, _ := ParsePath(proj + ".so_far.items")
	val, fd, ok := lookup(v.res.ProtoReflect(), p)
	if !ok || fd.Kind() != protoreflect.MessageKind {
		return nil
	}
	var leaves []leaf
	if _, isMoney := val.Message().Interface().(*commonv1.Money); isMoney {
		leaves = []leaf{{path: proj + ".so_far.items.amount", label: "Amount"}}
	} else {
		leaves = moneyLeaves(proj+".so_far.items", "", val.Message())
	}
	var out []*scenariosv1.Field
	for _, l := range leaves {
		var sources []*scenariosv1.Source
		for _, id := range v.working[l.path].GetSources() {
			if s, ok := byID[id]; ok {
				sources = append(sources, s)
			}
		}
		out = append(out, v.override(l.path, "So far: "+strings.ToLower(l.label[:1])+l.label[1:], sources))
	}
	return out
}

// override is a figure that can be overridden, as a money field: the
// figure the scenario uses, and beside it the library's.
func (v *viewer) override(path, label string, sources []*scenariosv1.Source) *scenariosv1.Field {
	money := func(m *commonv1.Money) *scenariosv1.Value {
		if m == nil {
			return nil
		}
		return &scenariosv1.Value{Kind: &scenariosv1.Value_Money{Money: m}}
	}
	lookupPath := strings.TrimSuffix(path, ".so_far.items.amount") + ".so_far.items"
	if !strings.HasSuffix(path, ".so_far.items.amount") {
		lookupPath = path
	}
	f := &scenariosv1.Field{Path: path, Label: label, Kind: &scenariosv1.FieldKind{Kind: &scenariosv1.FieldKind_Money_{Money: &scenariosv1.FieldKind_Money{}}},
		Value: money(moneyAt(v.res, lookupPath)), YourPlan: money(moneyAt(v.planRes, lookupPath)), Status: v.overrideStatus(path), AutomaticSources: sources}
	f.Automatic = f.Value
	for _, o := range v.in.GetOverrides() {
		if o.GetPath() == path {
			f.Overridden = true
			f.Value = money(o.GetAmount())
		}
	}
	if w := v.working[path]; w != nil {
		if w.GetAutomatic() != nil {
			f.Automatic = money(w.GetAutomatic())
		}
		f.SourcesChanged = w.GetSourcesChanged()
	}
	return f
}

// overrideStatus is status for an override, which is keyed by its path.
func (v *viewer) overrideStatus(path string) *scenariosv1.Status {
	find := func(in *scenariosv1.ScenarioInputs) *scenariosv1.Override {
		for _, o := range in.GetOverrides() {
			if o.GetPath() == path {
				return o
			}
		}
		return nil
	}
	for i := 0; i+1 < len(v.chain); i++ {
		if proto.Equal(find(v.inputs[v.chain[i]]), find(v.inputs[v.chain[i+1]])) {
			continue
		}
		if i == 0 {
			return &scenariosv1.Status{Kind: scenariosv1.StatusKind_STATUS_KIND_CHANGED_HERE}
		}
		return &scenariosv1.Status{Kind: scenariosv1.StatusKind_STATUS_KIND_CHANGED_IN_PARENT, ChangedIn: v.chain[i], ChangedInName: v.names[v.chain[i]]}
	}
	return nil
}
