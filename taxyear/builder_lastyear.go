package taxyear

import (
	"fmt"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
)

// lastYearUses say what each of last year's figures is used for.
var lastYearUses = []struct{ name, label, help string }{
	{"last_year_tax", "Last year's tax", "Sets the safe-harbour target: pay this much during the year (110% of it when last year's income was over $150,000) and there is no penalty."},
	{"last_year_agi", "Last year's income (AGI)", "Over $150,000, the safe-harbour target is 110% of last year's tax."},
	{"last_year_withheld", "Last year's withholding", "Read by the estimated-payment rules beside last year's tax."},
}

// lastYear lists last year's return for each jurisdiction, with where its
// figures come from and what each is used for, then the carryovers.
func (v *viewer) lastYear() []*scenariosv1.BuilderItem {
	var out []*scenariosv1.BuilderItem
	for _, e := range v.res.GetEstimated() {
		if item := v.lastYearReturn(e.GetJurisdiction()); item != nil {
			out = append(out, item)
		}
	}
	for _, c := range v.in.GetFacts().GetCarryovers() {
		path := "facts.carryovers[" + c.GetId() + "]"
		label, use := "Carryover", "to use this year"
		switch c.GetKind() {
		case factsv1.CarryoverKind_CARRYOVER_KIND_SHORT_TERM_CAPITAL_LOSS, factsv1.CarryoverKind_CARRYOVER_KIND_LONG_TERM_CAPITAL_LOSS:
			use = "cuts this year's investment gains, then up to $3,000 of other income"
		case factsv1.CarryoverKind_CARRYOVER_KIND_CHARITABLE_CONTRIBUTION:
			use = "adds to this year's gifts to charity"
		}
		fd := c.ProtoReflect().Descriptor().Fields().ByName("kind")
		if ev := fd.Enum().Values().ByNumber(protoreflect.EnumNumber(c.GetKind())); ev != nil {
			label = enumLabel(fd.Enum(), ev)
		}
		out = append(out, &scenariosv1.BuilderItem{Id: path, Label: label + " carried over", Status: v.status(path),
			Fields: v.fields(path, c.ProtoReflect(), ""), Summary: moneyText(c.GetAmount()) + ": " + use})
	}
	return out
}

// lastYearReturn is one jurisdiction's return last year: where its figures
// come from, each figure with what it is used for, and its details,
// closed. Nil when there is nothing to show.
func (v *viewer) lastYearReturn(j string) *scenariosv1.BuilderItem {
	at := "estimated[" + j + "]"
	name := "federal"
	if j != "US" {
		name = stateName(j)
	}
	item := &scenariosv1.BuilderItem{Id: "last_year[" + j + "]", Label: fmt.Sprintf("Your %d %s return", v.year-1, name)}
	var prior *factsv1.PriorYearReturn
	for _, p := range v.in.GetFacts().GetPriorYear() {
		if p.GetJurisdiction() == j {
			prior = p
		}
	}
	var sources []*scenariosv1.Source
	if prior != nil {
		item.Id = "facts.prior_year[" + prior.GetId() + "]"
		item.Status = v.status(item.Id)
		sources = sourcesIn(prior.ProtoReflect(), nil, false)
		if f := v.priorYearMethod(item.Id, prior); f != nil {
			item.Fields = append(item.Fields, f)
		}
	}
	for _, u := range lastYearUses {
		if moneyAt(v.res, at+"."+u.name) != nil {
			f := v.override(at+"."+u.name, u.label, sources)
			f.Help = u.help
			item.Fields = append(item.Fields, f)
		}
	}
	if len(item.Fields) == 0 {
		return nil
	}
	item.Summary = "Tax " + moneyText(moneyAt(v.res, at+".last_year_tax")) + "; income " + moneyText(moneyAt(v.res, at+".last_year_agi"))
	if prior != nil {
		details := &scenariosv1.BuilderItem{Id: item.Id + "#details", Label: "Details", Sources: sources}
		for _, f := range v.fields(item.Id, prior.ProtoReflect(), "") {
			if !has([]string{item.Id + ".total_tax", item.Id + ".adjusted_gross_income", item.Id + ".figures_from"}, f.GetPath()) {
				details.Fields = append(details.Fields, f)
			}
		}
		item.Children = append(item.Children, details)
	}
	return item
}

// priorYearMethod chooses where last year's figures come from: the
// uploaded return, figures typed in, or no return. Each is a whole
// PriorYearReturn put at the path.
func (v *viewer) priorYearMethod(path string, p *factsv1.PriorYearReturn) *scenariosv1.Field {
	chosen := "typed"
	switch {
	case p.GetFiled() != nil:
		chosen = "uploaded"
	case p.GetStatus() == factsv1.PriorYearReturn_STATUS_NONE:
		chosen = "none"
	}
	at := "estimated[" + p.GetJurisdiction() + "]"
	uploaded := v.uploadedReturn(p.GetId())
	typed := &factsv1.PriorYearReturn{Id: p.GetId(), Jurisdiction: p.GetJurisdiction(), People: p.GetPeople(), Status: factsv1.PriorYearReturn_STATUS_FILED,
		TotalTax: moneyAt(v.res, at+".last_year_tax"), AdjustedGrossIncome: moneyAt(v.res, at+".last_year_agi"),
		FiguresFrom: factsv1.PriorYearReturn_FIGURES_FROM_ENTERED, FullYearResident: p.FullYearResident, Itemised: p.Itemised, CarriesForward: p.GetCarriesForward()}
	if typed.Itemised == nil && p.GetFederal() != nil {
		it := p.GetFederal().GetForm_1040().GetItemized()
		typed.Itemised = &it
	}
	none := &factsv1.PriorYearReturn{Id: p.GetId(), Jurisdiction: p.GetJurisdiction(), People: p.GetPeople(), Status: factsv1.PriorYearReturn_STATUS_NONE}
	options := []struct {
		value, label, description, text string
		m                               *factsv1.PriorYearReturn
	}{
		{"uploaded", fmt.Sprintf("From my uploaded %d return", v.year-1), "The figures as the return you uploaded states them.", "", uploaded},
		{"typed", "I'll type them", "Last year's tax and income as you type them, from the return you filed.", "You type last year's tax and income", typed},
		{"none", "No return last year", "There is no safe-harbour target from last year, so payments must cover this year's tax.", "No safe-harbour target from last year", none},
	}
	method := &scenariosv1.FieldKind_Method{}
	for _, o := range options {
		opt := &scenariosv1.MethodOption{Value: o.value, Label: o.label, Description: o.description}
		switch {
		case o.value == chosen:
			opt.Preview = &scenariosv1.FormLine{Label: "Last year's tax", ThisScenario: moneyAt(v.res, at+".last_year_tax")}
			if chosen == "typed" {
				opt.Fields = v.pick(v.fields(path, p.ProtoReflect(), ""), map[string]string{
					path + ".total_tax": "Last year's total tax", path + ".adjusted_gross_income": "Last year's income (AGI)"})
			}
		case o.m == nil || !v.priorYearValid(o.m):
			continue
		default:
			b, err := proto.MarshalOptions{Deterministic: true}.Marshal(o.m)
			if err != nil {
				continue
			}
			opt.Edit = v.edit(path, &scenariosv1.Value{Kind: &scenariosv1.Value_Message{Message: b}})
			if o.text != "" {
				opt.PreviewText = o.text
			} else {
				opt.Preview = &scenariosv1.FormLine{Label: "Last year's tax", ThisScenario: firstMoney(o.m.ProtoReflect(), "total_tax")}
			}
		}
		method.Options = append(method.Options, opt)
	}
	return &scenariosv1.Field{Path: path, Label: "Where do last year's figures come from?",
		Kind:  &scenariosv1.FieldKind{Kind: &scenariosv1.FieldKind_Method_{Method: method}},
		Value: &scenariosv1.Value{Kind: &scenariosv1.Value_Choice{Choice: chosen}}}
}

// uploadedReturn is a prior year return with the return itself, from any
// scenario's inputs, or nil.
func (v *viewer) uploadedReturn(id string) *factsv1.PriorYearReturn {
	for _, sid := range append(append([]string(nil), v.chain...), "plan") {
		for _, p := range v.inputs[sid].GetFacts().GetPriorYear() {
			if p.GetId() == id && p.GetFiled() != nil {
				return p
			}
		}
	}
	for _, in := range v.inputs {
		for _, p := range in.GetFacts().GetPriorYear() {
			if p.GetId() == id && p.GetFiled() != nil {
				return p
			}
		}
	}
	return nil
}

// priorYearValid reports whether the scenario has no problem with a prior
// year return in place of its own.
func (v *viewer) priorYearValid(p *factsv1.PriorYearReturn) bool {
	in := proto.Clone(v.in).(*scenariosv1.ScenarioInputs)
	for i, q := range in.GetFacts().GetPriorYear() {
		if q.GetId() == p.GetId() {
			in.Facts.PriorYear[i] = p
		}
	}
	ctx, err := model.NewContext(v.year, v.asOf, in.GetFacts())
	return err == nil && len(Validate(ctx, in)) == 0
}

// firstMoney is the first money field of a name under a message, or nil.
func firstMoney(m protoreflect.Message, name protoreflect.Name) *commonv1.Money {
	var out *commonv1.Money
	m.Range(func(fd protoreflect.FieldDescriptor, val protoreflect.Value) bool {
		if fd.Kind() != protoreflect.MessageKind || fd.IsList() || fd.IsMap() {
			return true
		}
		if x, ok := val.Message().Interface().(*commonv1.Money); ok {
			if fd.Name() == name {
				out = x
				return false
			}
			return true
		}
		out = firstMoney(val.Message(), name)
		return out == nil
	})
	return out
}
