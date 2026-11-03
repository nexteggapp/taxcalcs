package taxyear

import (
	"fmt"

	"google.golang.org/protobuf/reflect/protoreflect"

	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
)

// about lists the filing status and other elections, the people and where
// they lived, each with a summary of what it holds.
func (v *viewer) about() []*scenariosv1.BuilderItem {
	var out []*scenariosv1.BuilderItem
	f := v.in.GetFacts()
	for _, e := range f.GetElections() {
		out = append(out, v.election(e))
	}
	for _, p := range f.GetPeople() {
		path := "facts.people[" + p.GetId() + "]"
		item := &scenariosv1.BuilderItem{Id: path, Label: p.GetName(), Status: v.status(path), Fields: v.fields(path, p.ProtoReflect(), ""),
			Questions: v.questions("dependents[" + p.GetId() + "]"), Summary: v.personSummary(p)}
		for _, d := range v.res.GetDependents() {
			if d.GetPerson() == p.GetId() && d.GetText().GetText() != "" {
				item.Notes = append(item.Notes, d.GetText())
			}
		}
		out = append(out, item)
	}
	for _, r := range f.GetResidence() {
		path := "facts.residence[" + r.GetId() + "]"
		summary := "From " + shortDate(r.GetFrom())
		if r.GetTo() != nil {
			summary = shortDate(r.GetFrom()) + " to " + shortDate(r.GetTo())
		}
		out = append(out, &scenariosv1.BuilderItem{Id: path, Label: "Lived in " + stateName(r.GetState()), Status: v.status(path),
			Fields: v.fields(path, r.ProtoReflect(), ""), Summary: summary})
	}
	return out
}

// election is one election: its fields, and its choice as the summary. A
// choice that is a message, such as a lot method, has its own fields.
func (v *viewer) election(e *factsv1.Election) *scenariosv1.BuilderItem {
	path := "facts.elections[" + e.GetId() + "]"
	m := e.ProtoReflect()
	item := &scenariosv1.BuilderItem{Id: path, Label: "Election", Status: v.status(path)}
	which := m.WhichOneof(m.Descriptor().Oneofs().ByName("choice"))
	if which == nil {
		item.Fields = v.fields(path, m, "")
		return item
	}
	item.Label = humanise(string(which.Name()))
	if which.Kind() == protoreflect.MessageKind {
		item.Fields = v.fields(path+"."+string(which.Name()), m.Get(which).Message(), "")
	} else {
		item.Fields = v.fields(path, m, "")
	}
	switch c := e.GetChoice().(type) {
	case *factsv1.Election_LotMethod:
		item.Summary = v.brokerName(c.LotMethod.GetBroker()) + ": " + lowerFirst(enumText(c.LotMethod.ProtoReflect(), "method"))
	case *factsv1.Election_WithholdingDates:
		item.Summary = jurisdictionLabel(c.WithholdingDates.GetJurisdiction()) + ": spread evenly over the year"
		if c.WithholdingDates.GetAsWithheld() {
			item.Summary = jurisdictionLabel(c.WithholdingDates.GetJurisdiction()) + ": counted on the dates withheld"
		}
	case *factsv1.Election_Waiver:
		item.Summary = jurisdictionLabel(c.Waiver.GetJurisdiction()) + ": " + lowerFirst(enumText(c.Waiver.ProtoReflect(), "reason"))
	default:
		item.Summary = enumText(m, string(which.Name()))
	}
	return item
}

// enumText is the label of the enum value in a message's field.
func enumText(m protoreflect.Message, field string) string {
	fd := m.Descriptor().Fields().ByName(protoreflect.Name(field))
	if fd == nil || fd.Kind() != protoreflect.EnumKind {
		return ""
	}
	ev := fd.Enum().Values().ByNumber(m.Get(fd).Enum())
	if ev == nil || ev.Number() == 0 {
		return "Not chosen"
	}
	return enumLabel(fd.Enum(), ev)
}

// brokerName is a broker model's name, or its id.
func (v *viewer) brokerName(id string) string {
	for _, b := range v.in.GetModel().GetBrokers() {
		if b.GetId() == id && b.GetName() != "" {
			return b.GetName()
		}
	}
	return id
}

// dependencyWords say whether a person counts as a dependent.
var dependencyWords = map[scenariosv1.DependencyStatus]string{
	scenariosv1.DependencyStatus_DEPENDENCY_STATUS_COUNTS:         "counts as your dependent",
	scenariosv1.DependencyStatus_DEPENDENCY_STATUS_DOES_NOT_COUNT: "not your dependent",
	scenariosv1.DependencyStatus_DEPENDENCY_STATUS_COULD_COUNT:    "could count as your dependent",
	scenariosv1.DependencyStatus_DEPENDENCY_STATUS_MAY_COUNT:      "may count as your dependent",
}

// personSummary is who a person is to the filer, the year they were born,
// and whether they count as a dependent: "Parent, born 1958; could count as
// your dependent".
func (v *viewer) personSummary(p *factsv1.Person) string {
	s := enumText(p.ProtoReflect(), "relationship")
	if y := p.GetDateOfBirth().GetYear(); y != 0 {
		s += fmt.Sprintf(", born %d", y)
	}
	for _, d := range v.res.GetDependents() {
		if w, ok := dependencyWords[d.GetStatus()]; ok && d.GetPerson() == p.GetId() {
			s += "; " + w
		}
	}
	return s
}
