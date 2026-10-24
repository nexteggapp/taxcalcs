package taxyear

import (
	"fmt"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/nexteggapp/taxcalcs/money"
	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
)

// A job applies its own overrides, as its withholding reads them; tax paid
// has none. Every other model's figure so far is overridden here, on its
// finished projection: an ADJUSTMENT item dated so_far_through carries the
// difference, and so far's and the year's totals and boxes move with it, so
// every total that reads the figure reads the typed one.

// figuresCase is the Item figures field each list's items use.
var figuresCase = map[string]protoreflect.Name{
	"businesses": "business", "accounts": "account", "brokers": "sale",
	"retirement": "contribution", "itemised": "itemised", "other_income": "other_income",
}

// overridesAt are the overrides of one model's figures so far, by the line
// each names after so_far.items.
func overridesAt(overrides []*scenariosv1.Override, at string) []*scenariosv1.Override {
	var out []*scenariosv1.Override
	for _, o := range overrides {
		if strings.HasPrefix(o.GetPath(), at+".so_far.items.") {
			out = append(out, o)
		}
	}
	return out
}

// overridden returns a copy of a model's finished projection with its
// overrides applied, and each override's working: the figure computed
// today and the sources it rests on.
func overridden(list, at string, p proto.Message, overrides []*scenariosv1.Override) (proto.Message, []*scenariosv1.Working, error) {
	p = proto.Clone(p)
	m := p.ProtoReflect()
	var out []*scenariosv1.Working
	for _, o := range overrides {
		line := strings.TrimPrefix(o.GetPath(), at+".so_far.items.")
		have, err := lineOf(list, m, "so_far", line)
		if err != nil {
			return nil, nil, fmt.Errorf("override %s: %w", o.GetPath(), err)
		}
		w := &scenariosv1.Working{Path: o.GetPath(), Automatic: model.Money(have), Sources: madeSources(m)}
		if o.GetWas() != nil {
			w.SourcesChanged = model.Value(o.GetWas()) != have
		}
		out = append(out, w)
		delta := model.Value(o.GetAmount()) - have
		if delta == 0 {
			continue
		}
		it, err := adjustment(list, at, m, line, delta)
		if err != nil {
			return nil, nil, fmt.Errorf("override %s: %w", o.GetPath(), err)
		}
		items := m.Mutable(m.Descriptor().Fields().ByName("items")).List()
		items.Append(protoreflect.ValueOfMessage(it.ProtoReflect()))
		for _, period := range []string{"so_far", "year"} {
			addLine(list, m, period, line, delta, it)
		}
	}
	if list == "brokers" {
		b := p.(*scenariosv1.BrokerProjection)
		b.GetYear().Boxes = brokerYear(b.GetSoFar().GetBoxes(), b.GetRest().GetBoxes())
	}
	sortItems(m)
	return p, out, nil
}

// lineOf is a period's item line of a projection: the money a line names
// in its items, or for a list whose items are one amount, that amount.
func lineOf(list string, m protoreflect.Message, period, line string) (money.USCents, error) {
	items, fd, err := itemsLine(list, m, period, line)
	if err != nil {
		return 0, err
	}
	if fd == nil {
		return moneyOf(items.Interface().(*commonv1.Money)), nil
	}
	if !items.Has(fd) {
		return 0, nil
	}
	return moneyOf(items.Get(fd).Message().Interface().(*commonv1.Money)), nil
}

// itemsLine finds a period's items message and the field a line names in
// it; a nil field means the items are the amount itself.
func itemsLine(list string, m protoreflect.Message, period, line string) (protoreflect.Message, protoreflect.FieldDescriptor, error) {
	pf := m.Descriptor().Fields().ByName(protoreflect.Name(period))
	totals := m.Mutable(pf).Message()
	items := totals.Mutable(totals.Descriptor().Fields().ByName("items")).Message()
	if _, ok := items.Interface().(*commonv1.Money); ok {
		if line != "amount" {
			return nil, nil, fmt.Errorf("%q is not a line of the figures so far", line)
		}
		return items, nil, nil
	}
	if list == "other_income" && line == "amount" {
		line = "gross"
	}
	fd := items.Descriptor().Fields().ByName(protoreflect.Name(line))
	if fd == nil || fd.Kind() != protoreflect.MessageKind || fd.Message().FullName() != "nexteggapp.taxprotos.common.v1.Money" {
		return nil, nil, fmt.Errorf("%q is not a line of the figures so far", line)
	}
	return items, fd, nil
}

// adjustment is the ADJUSTMENT item that carries delta on the line.
func adjustment(list, at string, m protoreflect.Message, line string, delta money.USCents) (*scenariosv1.Item, error) {
	id := m.Get(m.Descriptor().Fields().ByName("id")).String()
	through, _ := m.Get(m.Descriptor().Fields().ByName("so_far_through")).Message().Interface().(*commonv1.Date)
	it := &scenariosv1.Item{
		Id: model.ItemID(list, id, "override", line), Date: through,
		Status: scenariosv1.ItemStatus_ITEM_STATUS_MADE, Kind: scenariosv1.ItemKind_ITEM_KIND_ADJUSTMENT,
		Origin: &scenariosv1.Origin{Basis: scenariosv1.Origin_BASIS_OVERRIDE, Override: at + ".so_far.items." + line},
	}
	r := it.ProtoReflect()
	fd := r.Descriptor().Fields().ByName(figuresCase[list])
	if fd == nil {
		return nil, fmt.Errorf("no figures for %s", list)
	}
	f := r.Mutable(fd).Message()
	switch list {
	case "itemised":
		f.Interface().(*scenariosv1.ItemisedFigures).Amount = model.Money(delta)
		return it, nil
	case "retirement":
		kinds := map[string]scenariosv1.ContributionKind{"traditional_ira": scenariosv1.ContributionKind_CONTRIBUTION_KIND_TRADITIONAL_IRA,
			"roth_ira": scenariosv1.ContributionKind_CONTRIBUTION_KIND_ROTH_IRA, "hsa": scenariosv1.ContributionKind_CONTRIBUTION_KIND_HSA}
		k, ok := kinds[line]
		if !ok {
			return nil, fmt.Errorf("%q is not a line of the figures so far", line)
		}
		c := f.Interface().(*scenariosv1.ContributionFigures)
		c.Kind, c.Amount = k, model.Money(delta)
		return it, nil
	case "brokers":
		s := f.Interface().(*scenariosv1.SaleLot)
		s.Term, s.Box = brokerTerm(m, line)
		switch line {
		case "proceeds":
			s.Proceeds, s.Gain = model.Money(delta), model.Money(delta)
		case "basis":
			s.Basis, s.Gain = model.Money(delta), model.Money(-delta)
		case "short_term_gain", "long_term_gain":
			s.Gain = model.Money(delta)
		default:
			return nil, fmt.Errorf("%q is not a line of the figures so far", line)
		}
		return it, nil
	}
	if list == "other_income" && line == "amount" {
		line = "gross"
	}
	lf := f.Descriptor().Fields().ByName(protoreflect.Name(line))
	if lf == nil {
		return nil, fmt.Errorf("%q is not a line of the figures so far", line)
	}
	f.Set(lf, protoreflect.ValueOfMessage(model.Money(delta).ProtoReflect()))
	return it, nil
}

// brokerTerm is the term and Form 8949 box a broker's adjustment lot goes
// in: a gain line's own term, else the term of the larger gain so far; the
// box is the first so far has for the term, else A or D.
func brokerTerm(m protoreflect.Message, line string) (scenariosv1.GainTerm, scenariosv1.Form8949Box) {
	b := m.Interface().(*scenariosv1.BrokerProjection)
	term := scenariosv1.GainTerm_GAIN_TERM_SHORT
	switch {
	case line == "long_term_gain":
		term = scenariosv1.GainTerm_GAIN_TERM_LONG
	case line == "short_term_gain":
	default:
		s, l := model.Value(b.GetSoFar().GetItems().GetShortTermGain()), model.Value(b.GetSoFar().GetItems().GetLongTermGain())
		if abs(l) > abs(s) {
			term = scenariosv1.GainTerm_GAIN_TERM_LONG
		}
	}
	long := term == scenariosv1.GainTerm_GAIN_TERM_LONG
	for _, x := range b.GetSoFar().GetBoxes() {
		if isLong(x.GetBox()) == long {
			return term, x.GetBox()
		}
	}
	if long {
		return term, scenariosv1.Form8949Box_FORM_8949_BOX_D
	}
	return term, scenariosv1.Form8949Box_FORM_8949_BOX_A
}

func isLong(b scenariosv1.Form8949Box) bool { return b >= scenariosv1.Form8949Box_FORM_8949_BOX_D }

func abs(v money.USCents) money.USCents {
	if v < 0 {
		return -v
	}
	return v
}

// addLine adds delta to a period's line and to the boxes the line reaches.
func addLine(list string, m protoreflect.Message, period, line string, delta money.USCents, it *scenariosv1.Item) {
	items, fd, _ := itemsLine(list, m, period, line)
	add := func(dst protoreflect.Message, fd protoreflect.FieldDescriptor, d money.USCents) {
		var v money.USCents
		if dst.Has(fd) {
			v = moneyOf(dst.Get(fd).Message().Interface().(*commonv1.Money))
		}
		dst.Set(fd, protoreflect.ValueOfMessage(model.Money(v+d).ProtoReflect()))
	}
	if fd == nil {
		items.Interface().(*commonv1.Money).Minor += int64(delta)
	} else {
		add(items, fd, delta)
		if list == "other_income" && line == "amount" && items.Has(items.Descriptor().Fields().ByName("taxable")) {
			add(items, items.Descriptor().Fields().ByName("taxable"), delta)
		}
	}
	totals := m.Mutable(m.Descriptor().Fields().ByName(protoreflect.Name(period))).Message()
	switch list {
	case "accounts", "other_income":
		boxes := totals.Mutable(totals.Descriptor().Fields().ByName("boxes")).Message()
		add(boxes, boxes.Descriptor().Fields().ByName(fd.Name()), delta)
		if list == "other_income" && line == "amount" && boxes.Has(boxes.Descriptor().Fields().ByName("taxable")) {
			add(boxes, boxes.Descriptor().Fields().ByName("taxable"), delta)
		}
	case "itemised":
		// The kind's one Schedule A input, the one the year sets.
		year := m.Get(m.Descriptor().Fields().ByName("year")).Message()
		yb := year.Get(year.Descriptor().Fields().ByName("boxes")).Message()
		var input protoreflect.FieldDescriptor
		yb.Range(func(b protoreflect.FieldDescriptor, _ protoreflect.Value) bool {
			input = b
			return false
		})
		if input != nil {
			boxes := totals.Mutable(totals.Descriptor().Fields().ByName("boxes")).Message()
			add(boxes, input, delta)
		}
	case "businesses":
		c := totals.Interface().(*scenariosv1.BusinessTotals)
		if c.Boxes == nil {
			c.Boxes = &scenariosv1.ScheduleCLines{}
		}
		at := map[string]**commonv1.Money{"receipts": &c.Boxes.GrossReceipts, "fees": &c.Boxes.ReturnsAndAllowances, "expenses": &c.Boxes.TotalExpenses}
		sign := map[string]money.USCents{"receipts": 1, "fees": -1, "expenses": -1}
		if dst, ok := at[line]; ok {
			*dst = model.Money(model.Value(*dst) + delta)
			c.Boxes.NetProfit = model.Money(model.Value(c.Boxes.GetNetProfit()) + sign[line]*delta)
		}
	case "brokers":
		if period != "so_far" {
			return
		}
		c := totals.Interface().(*scenariosv1.BrokerTotals)
		s := it.GetSale()
		switch line {
		case "short_term_gain", "long_term_gain":
		default:
			// A proceeds or basis line moves the lot's gain, which is its term's.
			other := "short_term_gain"
			if s.GetTerm() == scenariosv1.GainTerm_GAIN_TERM_LONG {
				other = "long_term_gain"
			}
			for _, p := range []string{"so_far", "year"} {
				i, f, _ := itemsLine(list, m, p, other)
				add(i, f, model.Value(s.GetGain()))
			}
		}
		var box *scenariosv1.Form8949Totals
		for _, b := range c.GetBoxes() {
			if b.GetBox() == s.GetBox() {
				box = b
			}
		}
		if box == nil {
			box = &scenariosv1.Form8949Totals{Box: s.GetBox(), Proceeds: model.Money(0), Basis: model.Money(0), Adjustments: model.Money(0), Gain: model.Money(0)}
			c.Boxes = append(c.Boxes, box)
		}
		box.Proceeds = model.Money(model.Value(box.GetProceeds()) + model.Value(s.GetProceeds()))
		box.Basis = model.Money(model.Value(box.GetBasis()) + model.Value(s.GetBasis()))
		if line == "short_term_gain" || line == "long_term_gain" {
			// A gain typed on its own is an adjustment, Form 8949 column (g).
			box.Adjustments = model.Money(model.Value(box.GetAdjustments()) + delta)
		}
		box.Gain = model.Money(model.Value(box.GetProceeds()) - model.Value(box.GetBasis()) + model.Value(box.GetAdjustments()))
	}
}

// madeSources are the ids of the parts and evidence a projection's items
// so far rest on.
func madeSources(m protoreflect.Message) []string {
	seen := map[string]bool{}
	var out []string
	items := m.Get(m.Descriptor().Fields().ByName("items")).List()
	for i := 0; i < items.Len(); i++ {
		it := items.Get(i).Message().Interface().(*scenariosv1.Item)
		if it.GetStatus() != scenariosv1.ItemStatus_ITEM_STATUS_MADE || it.GetKind() == scenariosv1.ItemKind_ITEM_KIND_ADJUSTMENT {
			continue
		}
		for _, id := range append([]string{it.GetOrigin().GetPart()}, it.GetOrigin().GetEvidence()...) {
			if id != "" && !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out
}

// sortItems puts a projection's items back in date order.
func sortItems(m protoreflect.Message) {
	l := m.Get(m.Descriptor().Fields().ByName("items")).List()
	items := make([]*scenariosv1.Item, l.Len())
	for i := range items {
		items[i] = l.Get(i).Message().Interface().(*scenariosv1.Item)
	}
	model.SortItems(items)
	for i, it := range items {
		l.Set(i, protoreflect.ValueOfMessage(it.ProtoReflect()))
	}
}

// withOverrideWorkings merges each override's working into a model's
// workings: the computed figure and sources go on the working of the same
// path, or a new one.
func withOverrideWorkings(ws, overrides []*scenariosv1.Working) []*scenariosv1.Working {
	out := append([]*scenariosv1.Working(nil), ws...)
	for _, o := range overrides {
		var found bool
		for i, w := range out {
			if w.GetPath() == o.GetPath() {
				c := proto.Clone(w).(*scenariosv1.Working)
				c.Automatic, c.Sources, c.SourcesChanged = o.GetAutomatic(), o.GetSources(), o.GetSourcesChanged()
				out[i], found = c, true
			}
		}
		if !found {
			out = append(out, o)
		}
	}
	return out
}
