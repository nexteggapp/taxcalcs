package taxyear

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	"github.com/nexteggapp/taxcalcs/taxyear/model/account"
	"github.com/nexteggapp/taxcalcs/taxyear/model/business"
	"github.com/nexteggapp/taxcalcs/taxyear/model/itemised"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
)

// A model other than a job is drawn the way a job is: a method field
// choosing how its year is worked out, the changes that sit on top below
// it, then its evidence and setup, closed. ways says, for each kind of
// model, what each way removes and adds. A way is a change to the model's
// assumed parts and default part, so choosing it is one Edit of the whole
// model.

// way is one way of working out a model's year.
type way struct {
	value, label, description string
	// drop are the assumed part kinds the way removes.
	drop []string
	// add is the part kind the way adds, filled as the projection offers
	// it. Empty adds none.
	add string
	// rest is the default part's case the way sets. Empty leaves it.
	rest string
	// typed is set for a way whose figure the household types: the words
	// its preview shows until it is chosen.
	typed string
	// empty is the words its preview shows when the part it adds, as
	// offered, adds nothing to the figure so far.
	empty string
	// form labels the chosen way's fields, by path below the added part's
	// body, or below the model for a way that adds none.
	form map[string]string
	// needs says whether the way applies to a model. Nil: always.
	needs func(m protoreflect.Message) bool
}

// modelWays is how one kind of model is drawn.
type modelWays struct {
	question string
	ways     []way
	// hideExtras is the way that replaces everything, under which the
	// changes on top are hidden.
	hideExtras string
	// extras are the changes on top: a part kind's label as a part and as
	// the button that adds it.
	extras map[string][2]string
	// figure is the way's figure, below a projection's totals, added up,
	// and what it is called.
	figure      []string
	figureLabel string
}

// hasPart reports whether a model has a part of one of the kinds.
func hasPart(kinds ...string) func(m protoreflect.Message) bool {
	return func(m protoreflect.Message) bool {
		l := partsOfModel(m)
		for i := 0; l != nil && i < l.Len(); i++ {
			if _, k, _ := partCase(l.Get(i).Message()); has(kinds, k) {
				return true
			}
		}
		return false
	}
}

// hasField reports whether a model's field is set.
func hasField(name protoreflect.Name) func(m protoreflect.Message) bool {
	return func(m protoreflect.Message) bool {
		fd := m.Descriptor().Fields().ByName(name)
		if fd == nil {
			return false
		}
		if fd.IsList() {
			return m.Get(fd).List().Len() > 0
		}
		return m.Has(fd)
	}
}

const typeTotal = "You type the total"

// ways are the kinds of model drawn with a method field, by list, or for
// an itemised model by its kind.
var ways = map[string]modelWays{
	"businesses": {
		question: "How should we count this year's profit?",
		ways: []way{
			{value: "pace_so_far", label: "Earned so far, then the same pace", description: "Your bank entries and forms to date, then each month repeats the pace so far.",
				drop: []string{"monthly", "year_total"}, rest: "pace_so_far", needs: hasPart("from_ledger", "from_forms")},
			{value: "monthly", label: "Earned so far, plus what I expect for the rest", description: "Your entries to date, then the money in and costs you give for each month from a day.",
				drop: []string{"monthly", "year_total"}, add: "monthly", empty: "You type money in and costs for each month",
				form: map[string]string{"from": "From", "receipts": "Money in each month", "expenses": "Costs each month"}},
			{value: "year_total", label: "I know the year's profit", description: "The year's money in and costs as you type them. They replace everything else here.",
				drop: []string{"monthly", "year_total", "stops"}, add: "year_total", typed: "You type the year's money in and costs",
				form: map[string]string{"receipts": "Money in for the year", "fees": "Fees and refunds for the year", "expenses": "Costs for the year"}},
		},
		hideExtras:  "year_total",
		extras:      map[string][2]string{"stops": {"Stops on a date", "+ Stops on a date"}},
		figure:      []string{"boxes.net_profit"},
		figureLabel: "Profit for the year",
	},
	"accounts": {
		question: "How should we count this year's interest and dividends?",
		ways: []way{
			{value: "pace_so_far", label: "Paid so far, then the same pace", description: "What the account has paid to date, then each month repeats the pace so far.",
				drop: []string{"at_rate", "year_total"}, rest: "pace_so_far", needs: hasPart("from_credits", "from_forms")},
			{value: "at_rate", label: "Paid so far, then interest at a rate I give", description: "What the account has paid to date, then interest at the rate you give from a day.",
				drop: []string{"at_rate", "year_total"}, add: "at_rate", empty: "You type the rate and balance",
				form: map[string]string{"from": "From", "rate": "Interest rate, % a year", "set_balance": "Balance", "add": "Added to the balance"}},
			{value: "year_total", label: "I know the total", description: "The year's interest and dividends as you type them. They replace everything else here.",
				drop: []string{"at_rate", "year_total", "year_end_distribution", "closes"}, add: "year_total", typed: typeTotal,
				form: map[string]string{"interest": "Interest", "treasury_interest": "Of which US Treasury interest", "ordinary_dividends": "Dividends",
					"qualified_dividends": "Of which qualified dividends", "capital_gain_distributions": "Fund gains paid out"}},
		},
		hideExtras: "year_total",
		extras: map[string][2]string{
			"year_end_distribution": {"Year-end fund payout", "+ Year-end fund payout"},
			"closes":                {"Account closes", "+ Account closes"},
		},
		figure:      []string{"items.interest", "items.treasury_interest", "items.ordinary_dividends", "items.capital_gain_distributions"},
		figureLabel: "Interest and dividends for the year",
	},
	"mortgage": {
		question: "How should we count this year's mortgage interest?",
		ways: []way{
			{value: "amortised", label: "From the loan's balance, rate and payment", description: "Your statements to date, then the interest the loan's terms give.",
				drop: []string{"year_total", "rest_of_year"}, rest: "amortised",
				form: map[string]string{"mortgage.loan.balance": "Loan balance", "mortgage.loan.rate": "Interest rate, % a year", "mortgage.loan.payment": "Monthly payment"}},
			{value: "rest_of_year", label: "Paid so far, plus an amount for the rest", description: "Your statements to date, plus the interest you give for the rest of the year.",
				drop: []string{"year_total", "rest_of_year"}, add: "rest_of_year", empty: "You type the amount for the rest of the year", form: map[string]string{"amount": "Interest for the rest of the year"}},
			{value: "year_total", label: "I know the total", description: "The year's interest as you type it, as on the 1098. It replaces everything else here.",
				drop: []string{"year_total", "rest_of_year", "refinanced", "paid_off"}, add: "year_total", typed: typeTotal},
		},
		hideExtras: "year_total",
		extras: map[string][2]string{
			"refinanced": {"Refinanced", "+ Refinanced"},
			"paid_off":   {"Paid off", "+ Paid off"},
		},
		figure: []string{"items"}, figureLabel: "Interest for the year",
	},
	"property_tax": {
		question: "How should we count this year's property tax?",
		ways: []way{
			{value: "like_last_year", label: "Like last year", description: "The installments still to come are last year's bill again.",
				drop: []string{"year_total", "rest_of_year"}, rest: "like_last_year", needs: hasField("last_year")},
			{value: "rest_of_year", label: "Paid so far, plus an amount for the rest", description: "What you have paid to date, plus the amount you give for the rest of the year.",
				drop: []string{"year_total", "rest_of_year"}, add: "rest_of_year", empty: "You type the amount for the rest of the year", form: map[string]string{"amount": "Still to pay this year", "date": "Paid on"}},
			{value: "year_total", label: "I know the total", description: "The year's property tax as you type it. It replaces everything else here.",
				drop: []string{"year_total", "rest_of_year", "planned_installment"}, add: "year_total", typed: typeTotal},
		},
		hideExtras: "year_total",
		extras:     map[string][2]string{"planned_installment": {"A payment I plan", "+ A payment I plan"}},
		figure:     []string{"items"}, figureLabel: "Property tax for the year",
	},
	"charity": {
		question: "How should we count this year's gifts?",
		ways: []way{
			{value: "so_far", label: "Given so far", description: "Only the gifts you have made to date.", drop: []string{"like_last_year", "rest_of_year"}},
			{value: "like_last_year", label: "Given so far, then like last year", description: "Your gifts to date, then last year's gifts again for the rest of the year.",
				drop: []string{"like_last_year", "rest_of_year"}, add: "like_last_year", needs: hasField("last_year")},
			{value: "rest_of_year", label: "Given so far, plus an amount for the rest", description: "Your gifts to date, plus the amount you give for the rest of the year.",
				drop: []string{"like_last_year", "rest_of_year"}, add: "rest_of_year", empty: "You type the amount for the rest of the year", form: map[string]string{"amount": "Still to give this year", "date": "Given on"}},
		},
		extras: map[string][2]string{"planned_gift": {"A gift I plan", "+ A gift I plan"}},
		figure: []string{"items"}, figureLabel: "Gifts for the year",
	},
	"medical": {
		question: "How should we count this year's medical costs?",
		ways: []way{
			{value: "so_far", label: "Paid so far", description: "Only the costs you have paid to date.", drop: []string{"year_total", "rest_of_year"}},
			{value: "rest_of_year", label: "Paid so far, plus an amount for the rest", description: "Your costs to date, plus the amount you give for the rest of the year.",
				drop: []string{"year_total", "rest_of_year"}, add: "rest_of_year", empty: "You type the amount for the rest of the year", form: map[string]string{"amount": "Still to pay this year", "date": "Paid on"}},
			{value: "year_total", label: "I know the total", description: "The year's medical costs as you type them. They replace everything else here.",
				drop: []string{"year_total", "rest_of_year", "planned_expense"}, add: "year_total", typed: typeTotal},
		},
		hideExtras: "year_total",
		extras:     map[string][2]string{"planned_expense": {"A cost I plan", "+ A cost I plan"}},
		figure:     []string{"items"}, figureLabel: "Costs for the year",
	},
	"brokers": {
		extras: map[string][2]string{
			"planned_sale": {"A sale I plan", "+ A sale I plan"},
			"planned_gain": {"A gain I expect", "+ A gain I expect"},
		},
	},
}

// waysKey is the key in ways for a model: its list, or for an itemised
// model its kind.
func waysKey(s sourced) string {
	if s.list == "itemised" {
		if o := s.top.Descriptor().Oneofs().ByName("kind"); o != nil {
			if w := s.top.WhichOneof(o); w != nil {
				return string(w.Name())
			}
		}
	}
	return s.list
}

// innerOf is the message a model's parts and default part are on: the
// model, or for an itemised model its kind's message.
func innerOf(top protoreflect.Message) protoreflect.Message {
	if o := top.Descriptor().Oneofs().ByName("kind"); o != nil {
		if w := top.WhichOneof(o); w != nil {
			return top.Mutable(w).Message()
		}
	}
	return top
}

// partsOfModel is a model's parts, or nil.
func partsOfModel(top protoreflect.Message) protoreflect.List {
	in := top
	if o := top.Descriptor().Oneofs().ByName("kind"); o != nil {
		if w := top.WhichOneof(o); w != nil {
			in = top.Get(w).Message()
		}
	}
	fd := in.Descriptor().Fields().ByName("parts")
	if fd == nil {
		return nil
	}
	return in.Get(fd).List()
}

// defaultCase is the case of a model's default part, or "".
func defaultCase(top protoreflect.Message) string {
	in := top
	if o := top.Descriptor().Oneofs().ByName("kind"); o != nil {
		if w := top.WhichOneof(o); w != nil {
			in = top.Get(w).Message()
		}
	}
	fd := in.Descriptor().Fields().ByName("default_part")
	if fd == nil || !in.Has(fd) {
		return ""
	}
	d := in.Get(fd).Message()
	if o := d.Descriptor().Oneofs().ByName("rest"); o != nil {
		if w := d.WhichOneof(o); w != nil {
			return string(w.Name())
		}
	}
	return ""
}

// chosenWay is the way a model is worked out now: the first way whose part
// it has, else the first that adds none and whose default it has, else the
// first that adds none.
func chosenWay(mw modelWays, top protoreflect.Message) string {
	for _, w := range mw.ways {
		if w.add != "" && hasPart(w.add)(top) {
			return w.value
		}
	}
	dc := defaultCase(top)
	for _, w := range mw.ways {
		if w.add == "" && w.rest != "" && w.rest == dc {
			return w.value
		}
	}
	for _, w := range mw.ways {
		if w.add == "" {
			return w.value
		}
	}
	return ""
}

// withWay is a copy of a model worked out another way, or false when the
// way doesn't apply or the projection offers no part for it.
func (v *viewer) withWay(s sourced, w way) (proto.Message, bool) {
	if w.needs != nil && !w.needs(s.msg) {
		return nil, false
	}
	m := proto.Clone(s.top.Interface())
	top := m.ProtoReflect()
	in := innerOf(top)
	pf := in.Descriptor().Fields().ByName("parts")
	l := in.Mutable(pf).List()
	kept := in.NewField(pf).List()
	for i := 0; i < l.Len(); i++ {
		if _, k, _ := partCase(l.Get(i).Message()); !has(w.drop, k) {
			kept.Append(l.Get(i))
		}
	}
	if w.add != "" {
		part := v.offeredPart(s, w.add)
		if part == nil {
			return nil, false
		}
		part.Set(part.Descriptor().Fields().ByName("id"), protoreflect.ValueOfString(newPartID(s.id, v.id, s.path, w.add, kept.Len())))
		kept.Append(protoreflect.ValueOfMessage(part))
	}
	in.Set(pf, protoreflect.ValueOfList(kept))
	if w.rest != "" {
		df := in.Descriptor().Fields().ByName("default_part")
		d := in.NewField(df).Message()
		id := newPartID(s.id, v.id, "default_part")
		if in.Has(df) {
			id = in.Get(df).Message().Get(d.Descriptor().Fields().ByName("id")).String()
		}
		d.Set(d.Descriptor().Fields().ByName("id"), protoreflect.ValueOfString(id))
		rf := d.Descriptor().Fields().ByName(protoreflect.Name(w.rest))
		d.Set(rf, protoreflect.ValueOfMessage(d.NewField(rf).Message()))
		in.Set(df, protoreflect.ValueOfMessage(d))
	}
	return m, true
}

// offeredPart is a copy of the part of a kind the model's projection
// offers to add, filled with its defaults, or nil.
func (v *viewer) offeredPart(s sourced, kind string) protoreflect.Message {
	p, err := ParsePath("projection." + s.list + "[" + s.id + "].can_add")
	if err != nil {
		return nil
	}
	val, fd, ok := lookup(v.res.ProtoReflect(), p)
	if !ok || !wholeList(fd, p) {
		return nil
	}
	l := val.List()
	for i := 0; i < l.Len(); i++ {
		part := l.Get(i).Message()
		if part.Descriptor().Name() == "ItemisedPartChoice" {
			w := part.WhichOneof(part.Descriptor().Oneofs().ByName("part"))
			if w == nil || string(w.Name()) != waysKey(s) {
				continue
			}
			part = part.Get(w).Message()
		}
		if _, k, _ := partCase(part); k == kind {
			return proto.Clone(part.Interface()).ProtoReflect()
		}
	}
	return nil
}

// previewModel is a model's figure for the year, expanded on its own, or
// false when the scenario would have a problem with it.
func (v *viewer) previewModel(s sourced, mw modelWays, m proto.Message) (*commonv1.Money, bool) {
	ctx, err := model.NewContext(v.year, v.asOf, v.in.GetFacts())
	if err != nil || len(Validate(ctx, v.withModel(s, m))) > 0 {
		return nil, false
	}
	var proj proto.Message
	switch x := m.(type) {
	case *scenariosv1.BusinessModel:
		proj, _, err = business.Expand(ctx, x)
	case *scenariosv1.AccountModel:
		proj, _, err = account.Expand(ctx, x)
	case *scenariosv1.ItemisedModel:
		proj, _, err = itemised.Expand(ctx, x)
	default:
		return nil, false
	}
	if err != nil {
		return nil, false
	}
	return sumIn(proj.ProtoReflect(), "year.", mw.figure), true
}

// sumIn adds the money at paths below a prefix in a message.
func sumIn(m protoreflect.Message, prefix string, paths []string) *commonv1.Money {
	out := &commonv1.Money{Currency: "USD"}
	for _, p := range paths {
		pp, err := ParsePath(prefix + p)
		if err != nil {
			continue
		}
		val, fd, ok := lookup(m, pp)
		if !ok || wholeList(fd, pp) || fd.Kind() != protoreflect.MessageKind {
			continue
		}
		if x, ok := val.Message().Interface().(*commonv1.Money); ok {
			out.Minor += x.GetMinor()
		}
	}
	return out
}

// withModel is the scenario's inputs with one model replaced.
func (v *viewer) withModel(s sourced, m proto.Message) *scenariosv1.ScenarioInputs {
	in := proto.Clone(v.in).(*scenariosv1.ScenarioInputs)
	r := in.GetModel().ProtoReflect()
	fd := r.Descriptor().Fields().ByName(protoreflect.Name(s.list))
	l := r.Mutable(fd).List()
	for i := 0; i < l.Len(); i++ {
		el := l.Get(i).Message()
		if el.Get(el.Descriptor().Fields().ByName("id")).String() == s.id {
			l.Set(i, protoreflect.ValueOfMessage(m.ProtoReflect()))
		}
	}
	return in
}
