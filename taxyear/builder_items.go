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

// headlines are each list's figure for the year, relative to its
// projection, as the spec's row table gives them.
var headlines = map[string][]string{
	"employers":  {"year.boxes.wages"},
	"businesses": {"year.boxes.net_profit"},
	"accounts":   {"year.items.interest", "year.items.treasury_interest", "year.items.ordinary_dividends", "year.items.capital_gain_distributions"},
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
			if s.list != list {
				continue
			}
			if mw, ok := ways[waysKey(s)]; ok {
				out = append(out, v.waysItem(s, mw))
			} else {
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
	switch {
	case s.list == "other_income":
		paths = []string{"summary.other_income[" + s.id + "].taxable"}
	case s.list == "retirement":
		paths = []string{"summary.retirement[" + s.top.Get(s.top.Descriptor().Fields().ByName("person")).String() + "].deducted"}
	case s.list == "itemised" && moneyAt(v.res, "summary.itemised["+s.id+"].counted") != nil:
		paths = []string{"summary.itemised[" + s.id + "].counted"}
	}
	v.figures(item, paths...)
	item.Summary = v.modelSummary(s, proj)
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

// modelLabel is a model's name with what it is, as the page shows it:
// "Schwab interest and dividends", "Schwab shares sold".
func (v *viewer) modelLabel(s sourced) string {
	name := v.modelName(s)
	proj := "projection." + s.list + "[" + s.id + "]"
	switch s.list {
	case "accounts":
		var what []string
		if sumAt(v.res, proj+".year.items.interest", proj+".year.items.treasury_interest").GetMinor() != 0 {
			what = append(what, "interest")
		}
		if sumAt(v.res, proj+".year.items.ordinary_dividends", proj+".year.items.capital_gain_distributions").GetMinor() != 0 {
			what = append(what, "dividends")
		}
		if len(what) == 0 {
			what = []string{"interest"}
		}
		return name + " " + joinAnd(what)
	case "brokers":
		return name + " shares sold"
	case "retirement":
		return name + "'s IRA and HSA"
	case "tax_paid":
		return "Estimated payments"
	}
	return name
}

// modelName is a model's name, or what names it best without one.
func (v *viewer) modelName(s sourced) string {
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
	return humanise(strings.ReplaceAll(s.id, "-", "_"))
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
		out = append(out, &scenariosv1.Action{Label: addLabel(kind),
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

func jurisdictionLabel(j string) string {
	if j == "US" {
		return "Federal"
	}
	return stateName(j)
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
		out = append(out, v.override(l.path, "So far: "+lowerFirst(l.label), sources))
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
