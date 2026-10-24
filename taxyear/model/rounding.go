package model

import (
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/nexteggapp/taxcalcs/money"
	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
)

// Rounder rounds a run of items' amounts to the dollar, line by line, so
// every total of them adds the displayed parts and is no further from the
// exact sum than rounding it would be (docs/results.md). Each amount is shown
// as the change in its line's rounded running total, half away from zero, so
// a run of $5,833.33 paychecks shows $5,833, $5,833, $5,834. A run is one
// part's items so far, or for the rest of the year, each with its own
// Rounder: a part's line then adds to its own exact sum rounded, the year is
// so far's and the rest's sum, and every total adds the items shown.
type Rounder struct {
	exact map[string]money.USCents
}

// NewRounder starts a run.
func NewRounder() *Rounder { return &Rounder{exact: map[string]money.USCents{}} }

// Round returns the next amount of the named line, rounded.
func (r *Rounder) Round(line string, v money.USCents) money.USCents {
	before := r.exact[line]
	r.exact[line] = before + v
	return Dollars(before+v) - Dollars(before)
}

// RoundItems rounds the money of each item's figures with a [Rounder] for
// each run (see [RunOf]): every singular amount of the figures message, by
// its field. A sale lot's gain is then its rounded
// proceeds less its rounded basis plus its rounded wash sale adjustment, as
// Form 8949's columns work it out; a lot with no proceeds, a planned gain,
// rounds its gain alone.
func RoundItems(items []*scenariosv1.Item) {
	runs := map[string]*Rounder{}
	for _, it := range items {
		k := RunOf(it.GetStatus() == scenariosv1.ItemStatus_ITEM_STATUS_MADE, it.GetOrigin().GetPart())
		if runs[k] == nil {
			runs[k] = NewRounder()
		}
		roundFigures(runs[k], it)
	}
}

// RunOf names the run an item is rounded in: its part's, so far or for the
// rest of the year.
func RunOf(made bool, part string) string {
	if made {
		return "made/" + part
	}
	return "expected/" + part
}

// Runs keeps a Rounder for each run.
type Runs map[string]*Rounder

// For returns the run's Rounder.
func (rs Runs) For(made bool, part string) *Rounder {
	k := RunOf(made, part)
	if rs[k] == nil {
		rs[k] = NewRounder()
	}
	return rs[k]
}

func roundFigures(r *Rounder, it *scenariosv1.Item) {
	m := it.ProtoReflect()
	which := m.WhichOneof(m.Descriptor().Oneofs().ByName("figures"))
	if which == nil {
		return
	}
	f := m.Get(which).Message()
	sale := it.GetSale()
	f.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if fd.IsList() || fd.Message() == nil || fd.Message().FullName() != "nexteggapp.taxprotos.common.v1.Money" {
			return true
		}
		if sale != nil && fd.Name() == "gain" && sale.GetProceeds() != nil {
			return true
		}
		money := v.Message().Interface().(*commonv1.Money)
		money.Minor = int64(r.Round(string(which.Name())+"."+string(fd.Name()), Value(money)))
		return true
	})
	if sale != nil && sale.GetProceeds() != nil {
		sale.Gain = Money(Value(sale.GetProceeds()) - Value(sale.GetBasis()) + Value(sale.GetWashSaleDisallowed()))
	}
	if it.GetTakeHome() != nil {
		it.TakeHome.Minor = int64(r.Round("take_home", Value(it.GetTakeHome())))
	}
}
