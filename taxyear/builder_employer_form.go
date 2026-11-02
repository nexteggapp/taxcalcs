package taxyear

import (
	"strings"

	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
)

// payForm is the chosen way's short form: what can be typed for it, and
// the pay so far and still to come.
func (v *viewer) payForm(s sourced, e *scenariosv1.EmployerModel, proj string, byID map[string]*scenariosv1.Source) ([]*scenariosv1.Field, []*scenariosv1.FormLine) {
	var fields []*scenariosv1.Field
	inner := s.path
	switch way(e) {
	case payLatest:
		fields = v.soFarShown(proj, byID)
	case payRate:
		for _, p := range e.GetParts() {
			if p.GetPayRate() != nil {
				at := inner + ".parts[" + p.GetId() + "].pay_rate"
				fields = append(v.pick(v.fields(at, p.GetPayRate().ProtoReflect(), ""), map[string]string{
					at + ".from": "From", at + ".paycheck.gross": "Pay per paycheck, before tax"}), v.soFarShown(proj, byID)...)
			}
		}
	case payYear:
		for _, p := range e.GetParts() {
			if p.GetYearTotal() != nil {
				at := inner + ".parts[" + p.GetId() + "].year_total"
				fields = v.pick(v.fields(at, p.GetYearTotal().ProtoReflect(), ""), map[string]string{
					at + ".year.gross": "Pay for the year, before tax", at + ".year.federal_withheld": "Federal tax withheld (optional)"})
			}
		}
		return fields, nil
	case payLastYear:
		at := inner + ".default_part"
		fields = v.pick(v.fields(at, e.GetDefaultPart().ProtoReflect(), ""), map[string]string{
			at + ".each_paycheck.gross": "Pay per paycheck, from last year's W-2"})
	}
	return fields, v.payLines(e.GetId(), proj)
}

// pick keeps the fields at the given paths, in the given labels' order of
// the fields, relabelled.
func (v *viewer) pick(fields []*scenariosv1.Field, labels map[string]string) []*scenariosv1.Field {
	var out []*scenariosv1.Field
	for _, f := range fields {
		if l, ok := labels[f.GetPath()]; ok {
			f.Label = l
			out = append(out, f)
		}
	}
	return out
}

// soFarShown are the job's figures so far that need a person: those
// overridden, those whose sources changed, and gross pay so far when no
// paystub gives it. The rest come from paystubs and aren't shown.
func (v *viewer) soFarShown(proj string, byID map[string]*scenariosv1.Source) []*scenariosv1.Field {
	var out []*scenariosv1.Field
	for _, f := range v.soFar("employers", proj, byID) {
		missing := f.GetValue() == nil && strings.HasSuffix(f.GetPath(), ".so_far.items.gross")
		if f.GetOverridden() || f.GetSourcesChanged() || missing {
			out = append(out, f)
		}
	}
	return out
}

// payLines are the pay so far, with how many paychecks and through when,
// and the pay still to come.
func (v *viewer) payLines(id, proj string) []*scenariosv1.FormLine {
	p := v.employerProj(id)
	var made, extra, rest int
	for _, it := range p.GetItems() {
		switch {
		case it.GetKind() == scenariosv1.ItemKind_ITEM_KIND_ADJUSTMENT:
		case it.GetStatus() == scenariosv1.ItemStatus_ITEM_STATUS_EXPECTED:
			rest++
		case it.GetKind() == scenariosv1.ItemKind_ITEM_KIND_PAYCHECK:
			made++
		default:
			extra++
		}
	}
	var out []*scenariosv1.FormLine
	if made+extra > 0 {
		what := plural(made, "paycheck")
		if extra > 0 {
			what += " and " + plural(extra, "other payment")
		}
		if d := p.GetSoFarThrough(); d != nil {
			what += " through " + shortDate(d)
		}
		out = append(out, v.line(proj+".so_far.items.gross", "Paid so far: "+what))
	}
	if rest > 0 {
		out = append(out, v.line(proj+".rest.items.gross", "Still to come: "+plural(rest, "payday")))
	}
	var kept []*scenariosv1.FormLine
	for _, l := range out {
		if l != nil {
			kept = append(kept, l)
		}
	}
	return kept
}

// extraLabels name a job's bonuses and last day, as parts and as the
// buttons that add them.
var extraLabels = map[string][2]string{
	"one_off": {"Bonus or one-time pay", "+ Bonus or one-time pay"},
	"ends":    {"Last day at this job", "+ Last day at this job"},
}

// payExtras is "Anything else this year?": the job's bonuses and last day,
// and buttons to add them. A raise is the pay rate way, not an extra.
func (v *viewer) payExtras(s sourced, proj string) *scenariosv1.BuilderItem {
	sec := &scenariosv1.BuilderItem{Id: s.path + "#extras", Label: "Anything else this year?", StartOpen: true}
	sec.Children = v.partsOf(s, extraLabels, "one_off", "ends")
	for _, a := range v.canAdd(s, s.path, proj) {
		k := a.GetEdit().GetPath()
		for _, p := range v.employerProj(s.id).GetCanAdd() {
			if _, kind, _ := partCase(p.ProtoReflect()); strings.Contains(k, ".parts[") && a.GetLabel() == "Add: "+strings.ToLower(humanise(kind)) {
				if l, ok := extraLabels[kind]; ok {
					a.Label = l[1]
					sec.Actions = append(sec.Actions, a)
				}
			}
		}
	}
	return sec
}

// partsOf is a job's parts of the given kinds as items, labelled by labels
// where it names them.
func (v *viewer) partsOf(s sourced, labels map[string][2]string, kinds ...string) []*scenariosv1.BuilderItem {
	want := map[string]string{}
	for i := 0; s.parts != nil && i < s.parts.Len(); i++ {
		id, k, _ := partCase(s.parts.Get(i).Message())
		if has(kinds, k) {
			want[s.path+".parts["+id+"]"] = k
		}
	}
	var out []*scenariosv1.BuilderItem
	for _, c := range v.parts(s, s.path) {
		if k, ok := want[c.GetId()]; ok {
			if l, ok := labels[k]; ok {
				c.Label = l[0]
			}
			out = append(out, c)
		}
	}
	return out
}

// setupFields are the job's settings, in the order shown.
var setupFields = []string{"pay_frequency", "first_pay_date", "retirement_plan", "roth_option", "supplemental", "employer_hsa"}

// jobSetup is the job's settings, closed.
func (v *viewer) jobSetup(s sourced) *scenariosv1.BuilderItem {
	all := map[string]*scenariosv1.Field{}
	for _, f := range v.fields(s.path, s.top, "") {
		all[f.GetPath()] = f
	}
	sec := &scenariosv1.BuilderItem{Id: s.path + "#setup", Label: "Job setup"}
	for _, name := range setupFields {
		if f, ok := all[s.path+"."+name]; ok {
			sec.Fields = append(sec.Fields, f)
		}
	}
	return sec
}

// w2Box is a W-2 box's number and label.
type w2Box struct{ box, label string }

// w2Boxes are the W-2's boxes by the projection's field name.
var w2Boxes = map[string]w2Box{
	"wages": {"1", "Wages"}, "federal_withheld": {"2", "Federal tax withheld"},
	"social_security_wages": {"3", "Social Security wages"}, "social_security_withheld": {"4", "Social Security tax withheld"},
	"medicare_wages": {"5", "Medicare wages"}, "medicare_withheld": {"6", "Medicare tax withheld"},
	"k401": {"12 D", "401(k)"}, "k403b": {"12 E", "403(b)"}, "roth_401k": {"12 AA", "Roth 401(k)"},
	"roth_403b": {"12 BB", "Roth 403(b)"}, "hsa": {"12 W", "HSA"},
}

// w2Boxes is the W-2 the job should produce, box by box, closed. A
// state's lines are boxes 16 and 17.
func (v *viewer) w2Boxes(s sourced, proj string) *scenariosv1.BuilderItem {
	sec := &scenariosv1.BuilderItem{Id: s.path + "#w2", Label: "The W-2 this should produce"}
	at := proj + ".year.boxes."
	for _, l := range v.linesAt(proj + ".year.boxes") {
		name := strings.TrimPrefix(l.GetPath(), at)
		if b, ok := w2Boxes[name]; ok {
			l.Line, l.Label = b.box, b.label
		} else if st, field, ok := strings.Cut(strings.TrimPrefix(name, "states["), "]."); ok && strings.HasPrefix(name, "states[") {
			switch field {
			case "wages":
				l.Line, l.Label = "16", st+" wages"
			case "withheld":
				l.Line, l.Label = "17", st+" tax withheld"
			default:
				continue
			}
		}
		sec.Displays = append(sec.Displays, &scenariosv1.Display{Kind: &scenariosv1.Display_Figure{Figure: l}})
	}
	return sec
}
