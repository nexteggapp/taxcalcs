package taxyear

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/nexteggapp/taxcalcs/money"
	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
)

// Every model's box totals round once, as the employers' do: so far's and
// the rest's are each rounded to the dollar, half away from zero, and the
// year's is their sum. Item totals stay exact, since they are the items'
// sum. The return then reads whole dollars.

// periods rounds so far's and the rest's Money fields and sets the year's to
// their sum. The three are the same message type, holding Money fields.
func periods(soFar, rest, year proto.Message) {
	if year == nil || !year.ProtoReflect().IsValid() {
		return
	}
	s, r, y := soFar.ProtoReflect(), rest.ProtoReflect(), year.ProtoReflect()
	fields := y.Descriptor().Fields()
	for i := 0; i < fields.Len(); i++ {
		f := fields.Get(i)
		if f.Kind() != protoreflect.MessageKind || f.IsList() || f.Message().FullName() != "nexteggapp.taxprotos.common.v1.Money" {
			continue
		}
		sv, rv := dollars(s, f), dollars(r, f)
		if s.IsValid() {
			s.Set(f, protoreflect.ValueOfMessage(model.Money(sv).ProtoReflect()))
		}
		if r.IsValid() {
			r.Set(f, protoreflect.ValueOfMessage(model.Money(rv).ProtoReflect()))
		}
		if y.Has(f) || sv+rv != 0 {
			y.Set(f, protoreflect.ValueOfMessage(model.Money(sv+rv).ProtoReflect()))
		}
	}
}

func dollars(m protoreflect.Message, f protoreflect.FieldDescriptor) money.USCents {
	if !m.IsValid() || !m.Has(f) {
		return 0
	}
	v, _ := m.Get(f).Message().Interface().(*commonv1.Money)
	return model.Dollars(model.Value(v))
}

// brokerYear rounds each Form 8949 box's columns for so far and the rest,
// working out the gain from the rounded columns as the form does,
// and adds them by box.
func brokerYear(soFar, rest []*scenariosv1.Form8949Totals) []*scenariosv1.Form8949Totals {
	byBox := map[scenariosv1.Form8949Box]*scenariosv1.Form8949Totals{}
	add := func(list []*scenariosv1.Form8949Totals) {
		for _, b := range list {
			for _, m := range []*commonv1.Money{b.GetProceeds(), b.GetBasis(), b.GetAdjustments()} {
				if m != nil {
					m.Minor = int64(model.Dollars(money.USCents(m.GetMinor())))
				}
			}
			// The form rounds by column: the gain is the rounded proceeds
			// less the rounded basis, plus the rounded adjustments.
			b.Gain = model.Money(model.Value(b.GetProceeds()) - model.Value(b.GetBasis()) + model.Value(b.GetAdjustments()))
			t := byBox[b.GetBox()]
			if t == nil {
				t = &scenariosv1.Form8949Totals{Box: b.GetBox(), Proceeds: model.Money(0), Basis: model.Money(0), Adjustments: model.Money(0), Gain: model.Money(0)}
				byBox[b.GetBox()] = t
			}
			t.Proceeds = model.Money(model.Value(t.GetProceeds()) + model.Value(b.GetProceeds()))
			t.Basis = model.Money(model.Value(t.GetBasis()) + model.Value(b.GetBasis()))
			t.Adjustments = model.Money(model.Value(t.GetAdjustments()) + model.Value(b.GetAdjustments()))
			t.Gain = model.Money(model.Value(t.GetGain()) + model.Value(b.GetGain()))
		}
	}
	add(soFar)
	add(rest)
	var out []*scenariosv1.Form8949Totals
	for k := scenariosv1.Form8949Box_FORM_8949_BOX_A; k <= scenariosv1.Form8949Box_FORM_8949_BOX_F; k++ {
		if t := byBox[k]; t != nil {
			out = append(out, t)
		}
	}
	return out
}
