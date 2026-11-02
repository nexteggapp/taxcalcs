package taxyear

import (
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"

	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
)

// evidenceTypes are messages a field never edits: documents as read, and
// whole returns. A scenario changes none of them.
var evidenceTypes = map[protoreflect.FullName]bool{
	"nexteggapp.taxprotos.facts.v1.Paystub":             true,
	"nexteggapp.taxprotos.facts.v1.Document":            true,
	"nexteggapp.taxprotos.facts.v1.FederalReturn":       true,
	"nexteggapp.taxprotos.facts.v1.CaliforniaReturn":    true,
	"nexteggapp.taxprotos.facts.v1.OregonReturn":        true,
	"nexteggapp.taxprotos.facts.v1.PaymentConfirmation": true,
	"nexteggapp.taxprotos.facts.v1.MortgageStatement":   true,
	"nexteggapp.taxprotos.facts.v1.PropertyTaxBill":     true,
	"nexteggapp.taxprotos.facts.v1.Receipt":             true,
	"nexteggapp.taxprotos.facts.v1.Form1099BStatement":  true,
	docRefName: true,
	ledgerName: true,
}

// skipped are fields shown some other way: as the item's id, as children,
// or as offers.
var skipped = map[protoreflect.Name]bool{"id": true, "parts": true, "default_part": true, "can_add": true}

// fields lists a message's editable values as fields, by reflection, in
// field order: each set case of a oneof, each set message's own fields,
// and each list item's by its key. Forms and refs are left out.
func (v *viewer) fields(path string, m protoreflect.Message, label string) []*scenariosv1.Field {
	var out []*scenariosv1.Field
	fields := m.Descriptor().Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		if skipped[fd.Name()] || fd.IsMap() {
			continue
		}
		if o := fd.ContainingOneof(); o != nil && !o.IsSynthetic() && m.WhichOneof(o) != fd {
			continue
		}
		p := path + "." + string(fd.Name())
		lab := joinLabel(label, humanise(string(fd.Name())))
		switch {
		case fd.IsList():
			if fd.Kind() != protoreflect.MessageKind || skipMessage(fd.Message()) {
				continue
			}
			l := m.Get(fd).List()
			for k := 0; k < l.Len(); k++ {
				key, elLabel := listKey(fd, l.Get(k).Message(), k)
				out = append(out, v.fields(p+"["+key+"]", l.Get(k).Message(), joinLabel(lab, elLabel))...)
			}
		case fd.Kind() == protoreflect.MessageKind && !leafMessage(fd.Message()):
			if !skipMessage(fd.Message()) && m.Has(fd) {
				out = append(out, v.fields(p, m.Get(fd).Message(), lab)...)
			}
		default:
			out = append(out, &scenariosv1.Field{Path: p, Label: lab, Kind: v.kindOf(fd), Value: valueOf(m, fd),
				YourPlan: v.planValue(p), Status: v.status(p)})
		}
	}
	return out
}

func leafMessage(d protoreflect.MessageDescriptor) bool {
	n := d.FullName()
	return n == moneyName || n == dateName || n == quantityName
}

func skipMessage(d protoreflect.MessageDescriptor) bool {
	return evidenceTypes[d.FullName()] || strings.HasPrefix(string(d.FullName()), "nexteggapp.taxprotos.us.")
}

// planValue is a path's value in Your plan, or nil in Your plan itself.
func (v *viewer) planValue(path string) *scenariosv1.Value {
	if v.id == "plan" || v.plan == nil {
		return nil
	}
	p, err := ParsePath(path)
	if err != nil || len(p.steps) == 0 {
		return nil
	}
	parent := Path{steps: p.steps[:len(p.steps)-1]}
	m := v.plan.ProtoReflect()
	if len(parent.steps) > 0 {
		val, fd, ok := lookup(m, parent)
		if !ok || fd.Kind() != protoreflect.MessageKind || wholeList(fd, parent) {
			return nil
		}
		m = val.Message()
	}
	last := p.steps[len(p.steps)-1]
	fd := m.Descriptor().Fields().ByName(protoreflect.Name(last.field))
	if fd == nil || fd.IsList() {
		return nil
	}
	return valueOf(m, fd)
}

// kindOf is the control for a field.
func (v *viewer) kindOf(fd protoreflect.FieldDescriptor) *scenariosv1.FieldKind {
	k := &scenariosv1.FieldKind{}
	name := string(fd.Name())
	switch fd.Kind() {
	case protoreflect.MessageKind:
		switch fd.Message().FullName() {
		case moneyName:
			signed := strings.Contains(name, "gain") || strings.Contains(name, "loss") || strings.Contains(name, "adjust")
			k.Kind = &scenariosv1.FieldKind_Money_{Money: &scenariosv1.FieldKind_Money{Signed: signed}}
		case dateName:
			k.Kind = &scenariosv1.FieldKind_Date_{Date: &scenariosv1.FieldKind_Date{}}
		default:
			if strings.Contains(name, "rate") || strings.Contains(name, "percent") {
				k.Kind = &scenariosv1.FieldKind_Percent_{Percent: &scenariosv1.FieldKind_Percent{}}
			} else {
				k.Kind = &scenariosv1.FieldKind_Number_{Number: &scenariosv1.FieldKind_Number{}}
			}
		}
	case protoreflect.EnumKind:
		c := &scenariosv1.FieldKind_Choice{}
		values := fd.Enum().Values()
		for i := 0; i < values.Len(); i++ {
			if ev := values.Get(i); ev.Number() != 0 {
				c.Options = append(c.Options, &scenariosv1.Option{Value: string(ev.Name()), Label: enumLabel(fd.Enum(), ev)})
			}
		}
		k.Kind = &scenariosv1.FieldKind_Choice_{Choice: c}
	case protoreflect.BoolKind:
		k.Kind = &scenariosv1.FieldKind_YesNo_{YesNo: &scenariosv1.FieldKind_YesNo{NotSure: fd.HasPresence()}}
	case protoreflect.StringKind:
		if name == "person" {
			p := &scenariosv1.FieldKind_Person{}
			for _, person := range v.in.GetFacts().GetPeople() {
				p.Options = append(p.Options, &scenariosv1.Option{Value: person.GetId(), Label: person.GetName()})
			}
			k.Kind = &scenariosv1.FieldKind_Person_{Person: p}
		} else {
			k.Kind = &scenariosv1.FieldKind_Text_{Text: &scenariosv1.FieldKind_Text{}}
		}
	default:
		k.Kind = &scenariosv1.FieldKind_Number_{Number: &scenariosv1.FieldKind_Number{}}
	}
	return k
}

// valueOf is a field's value as a Value, or nil when it is unset.
func valueOf(m protoreflect.Message, fd protoreflect.FieldDescriptor) *scenariosv1.Value {
	if fd.HasPresence() && !m.Has(fd) {
		return nil
	}
	val := m.Get(fd)
	switch fd.Kind() {
	case protoreflect.MessageKind:
		switch x := val.Message().Interface().(type) {
		case *commonv1.Money:
			return &scenariosv1.Value{Kind: &scenariosv1.Value_Money{Money: x}}
		case *commonv1.Date:
			return &scenariosv1.Value{Kind: &scenariosv1.Value_Date{Date: x}}
		case *commonv1.Quantity:
			return &scenariosv1.Value{Kind: &scenariosv1.Value_Number{Number: x}}
		}
	case protoreflect.EnumKind:
		if ev := fd.Enum().Values().ByNumber(val.Enum()); ev != nil && ev.Number() != 0 {
			return &scenariosv1.Value{Kind: &scenariosv1.Value_Choice{Choice: string(ev.Name())}}
		}
	case protoreflect.BoolKind:
		yn := scenariosv1.YesNo_YES_NO_NO
		if val.Bool() {
			yn = scenariosv1.YesNo_YES_NO_YES
		}
		return &scenariosv1.Value{Kind: &scenariosv1.Value_YesNo{YesNo: yn}}
	case protoreflect.StringKind:
		if val.String() != "" {
			return &scenariosv1.Value{Kind: &scenariosv1.Value_Text{Text: val.String()}}
		}
	case protoreflect.Int32Kind, protoreflect.Int64Kind, protoreflect.Sint32Kind, protoreflect.Sint64Kind:
		return &scenariosv1.Value{Kind: &scenariosv1.Value_Number{Number: &commonv1.Quantity{Units: val.Int()}}}
	case protoreflect.Uint32Kind, protoreflect.Uint64Kind:
		return &scenariosv1.Value{Kind: &scenariosv1.Value_Number{Number: &commonv1.Quantity{Units: int64(val.Uint())}}}
	}
	return nil
}
