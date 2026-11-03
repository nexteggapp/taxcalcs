package taxyear

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
)

// waysItem is a model drawn around its ways: the method field, then the
// changes on top, the evidence and the model's details as closed parts.
func (v *viewer) waysItem(s sourced, mw modelWays) *scenariosv1.BuilderItem {
	item := v.modelItem(s)
	item.Method, item.Actions, item.Children = "", nil, nil
	proj := "projection." + s.list + "[" + s.id + "]"
	byID := map[string]*scenariosv1.Source{}
	sourcesIn(s.top, byID, false)
	chosen := chosenWay(mw, s.top)
	shown := map[string]bool{}
	item.Fields = nil
	if len(mw.ways) > 0 {
		f := v.wayField(s, mw, chosen, proj, byID)
		item.Fields = []*scenariosv1.Field{f}
		for _, o := range f.GetKind().GetMethod().GetOptions() {
			for _, ff := range o.GetFields() {
				shown[ff.GetPath()] = true
			}
		}
	}
	if len(mw.extras) > 0 && chosen != mw.hideExtras {
		item.Children = append(item.Children, v.extrasSection(s, mw.extras, proj))
	}
	known := partKinds[s.partName()].known
	if ev := v.partsOf(s, nil, known...); len(ev) > 0 {
		item.Children = append(item.Children, &scenariosv1.BuilderItem{Id: s.path + "#evidence", Label: "Documents and bank entries so far", Children: ev})
	}
	inner := innerPath(s)
	details := &scenariosv1.BuilderItem{Id: s.path + "#details", Label: "Details", Sources: item.Sources}
	for _, f := range v.fields(inner, s.msg, "") {
		if !shown[f.GetPath()] {
			details.Fields = append(details.Fields, f)
		}
	}
	item.Sources = nil
	if len(details.Fields)+len(details.Sources) > 0 {
		item.Children = append(item.Children, details)
	}
	return item
}

// wayField is the method field for a model: each way that applies, with
// its preview, and the chosen one's form.
func (v *viewer) wayField(s sourced, mw modelWays, chosen, proj string, byID map[string]*scenariosv1.Source) *scenariosv1.Field {
	method := &scenariosv1.FieldKind_Method{}
	for _, w := range mw.ways {
		opt := &scenariosv1.MethodOption{Value: w.value, Label: w.label, Description: w.description}
		if w.value == chosen {
			opt.Preview = &scenariosv1.FormLine{Label: mw.figureLabel, ThisScenario: sumIn(v.res.ProtoReflect(), proj+".year.", mw.figure)}
			opt.Fields, opt.Lines = v.wayForm(s, w, proj, byID), v.wayLines(s, mw, proj)
			method.Options = append(method.Options, opt)
			continue
		}
		m, ok := v.withWay(s, w)
		if !ok {
			continue
		}
		figure, ok := v.previewModel(s, mw, m)
		if !ok {
			continue
		}
		b, err := proto.MarshalOptions{Deterministic: true}.Marshal(m)
		if err != nil {
			continue
		}
		soFar := sumIn(v.res.ProtoReflect(), proj+".so_far.", mw.figure)
		switch {
		case w.typed != "":
			opt.PreviewText = w.typed
		case w.empty != "" && figure.GetMinor() == soFar.GetMinor():
			opt.PreviewText = w.empty
		default:
			opt.Preview = &scenariosv1.FormLine{Label: mw.figureLabel, ThisScenario: figure}
		}
		opt.Edit = v.edit(s.path, &scenariosv1.Value{Kind: &scenariosv1.Value_Message{Message: b}})
		method.Options = append(method.Options, opt)
	}
	return &scenariosv1.Field{Path: s.path, Label: mw.question,
		Kind:  &scenariosv1.FieldKind{Kind: &scenariosv1.FieldKind_Method_{Method: method}},
		Value: &scenariosv1.Value{Kind: &scenariosv1.Value_Choice{Choice: chosen}}}
}

// wayForm is the chosen way's fields: the added part's, or the model's
// the way names, then the figures so far that are overridden.
func (v *viewer) wayForm(s sourced, w way, proj string, byID map[string]*scenariosv1.Source) []*scenariosv1.Field {
	var fields []*scenariosv1.Field
	base := s.path
	if w.add == "" {
		fields = v.fields(innerPath(s), s.msg, "")
	} else {
		for i := 0; s.parts != nil && i < s.parts.Len(); i++ {
			id, k, body := partCase(s.parts.Get(i).Message())
			if k == w.add && body != nil {
				base = innerPath(s) + ".parts[" + id + "]." + k
				fields = v.fields(base, body, "")
			}
		}
	}
	if w.form != nil {
		labels := map[string]string{}
		for rel, l := range w.form {
			labels[base+"."+rel] = l
		}
		fields = v.pick(fields, labels)
	} else if w.add == "" {
		fields = nil
	}
	for _, f := range v.soFar(s.list, proj, byID) {
		if f.GetOverridden() || f.GetSourcesChanged() {
			fields = append(fields, f)
		}
	}
	return fields
}

// wayLines are the figure so far and for the rest of the year.
func (v *viewer) wayLines(s sourced, mw modelWays, proj string) []*scenariosv1.FormLine {
	label := "So far"
	if d := moneyDate(v, proj+".so_far_through"); d != "" {
		label = "So far, through " + d
	}
	var out []*scenariosv1.FormLine
	for _, l := range []struct{ label, at string }{{label, ".so_far."}, {"Rest of the year", ".rest."}} {
		this := sumIn(v.res.ProtoReflect(), proj+l.at, mw.figure)
		line := &scenariosv1.FormLine{Label: l.label, ThisScenario: this}
		if v.planRes != nil {
			line.YourPlan = sumIn(v.planRes.ProtoReflect(), proj+l.at, mw.figure)
		}
		out = append(out, line)
	}
	return out
}

// moneyDate is a date in the result as "Aug 31", or "".
func moneyDate(v *viewer, path string) string {
	p, err := ParsePath(path)
	if err != nil {
		return ""
	}
	val, fd, ok := lookup(v.res.ProtoReflect(), p)
	if !ok || wholeList(fd, p) || fd.Kind() != protoreflect.MessageKind {
		return ""
	}
	if d, ok := val.Message().Interface().(*commonv1.Date); ok && d.GetYear() != 0 {
		return shortDate(d)
	}
	return ""
}

// extrasSection is "Anything else this year?": the model's changes on
// top, and buttons to add them.
func (v *viewer) extrasSection(s sourced, extras map[string][2]string, proj string) *scenariosv1.BuilderItem {
	sec := &scenariosv1.BuilderItem{Id: s.path + "#extras", Label: "Anything else this year?", StartOpen: true}
	kinds := make([]string, 0, len(extras))
	for k := range extras {
		kinds = append(kinds, k)
	}
	sec.Children = v.partsOf(s, extras, kinds...)
	for _, a := range v.canAdd(s, innerPath(s), proj) {
		for kind, l := range extras {
			if a.GetLabel() == addLabel(kind) {
				a.Label = l[1]
				sec.Actions = append(sec.Actions, a)
			}
		}
	}
	return sec
}

// innerPath is the path of the message a model's parts are on.
func innerPath(s sourced) string {
	if s.list == "itemised" {
		return s.path + "." + waysKey(s)
	}
	return s.path
}
