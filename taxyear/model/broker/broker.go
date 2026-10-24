// Package broker expands a BrokerModel, one broker's sales, into sale lots.
//
// It has no slots: every part adds items on its dates. The holdings
// register is as of January 1; the year's purchases add lots to it, and a
// trade sells the lots it names or, by the elected method, first in first
// out; a 1099-B line is an item, and a trade the line reports
// is the same item. A planned sale sells at a typed price or today's, 10%
// lower or higher. See docs/models.md.
package broker

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/nexteggapp/taxcalcs/money"
	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/internal/civil"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
	form1099b "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_b"
)

// micro is the scale shares are counted at: millionths of a share.
const micro = 6

func microShares(q *commonv1.Quantity) int64 {
	u, s := q.GetUnits(), q.GetScale()
	for ; s < micro; s++ {
		u *= 10
	}
	for ; s > micro; s-- {
		u /= 10
	}
	return u
}

func quantity(m int64) *commonv1.Quantity { return &commonv1.Quantity{Units: m, Scale: micro} }

// lot is a lot in the register, with the shares not yet sold.
type lot struct {
	id       string
	holding  string
	acquired civil.Date
	shares   int64
	left     int64
	basis    money.USCents
}

type expansion struct {
	ctx   *model.Context
	m     *scenariosv1.BrokerModel
	notes *model.Notes
	items []*scenariosv1.Item
	lots  []*lot
	fifo  bool
	// through is the latest trade or form date.
	through civil.Date
}

func (e *expansion) prefix() string { return model.ItemID("brokers", e.m.GetId()) }
func (e *expansion) path() string   { return fmt.Sprintf("model.brokers[%s]", e.m.GetId()) }

// Expand expands one broker model.
func Expand(ctx *model.Context, m *scenariosv1.BrokerModel) (*scenariosv1.BrokerProjection, *model.Notes, error) {
	e := &expansion{ctx: ctx, m: m, notes: &model.Notes{}, fifo: true}
	for _, el := range ctx.Facts().GetElections() {
		if lm := el.GetLotMethod(); lm != nil && lm.GetBroker() == m.GetId() {
			e.fifo = lm.GetMethod() != factsv1.LotMethod_METHOD_SPECIFIC_LOTS
		}
	}
	for _, h := range m.GetHoldings() {
		for _, l := range h.GetLots() {
			d, _ := civil.FromProto(l.GetAcquired())
			sh := microShares(l.GetShares())
			e.lots = append(e.lots, &lot{id: l.GetId(), holding: h.GetId(), acquired: d, shares: sh, left: sh, basis: model.Value(l.GetBasis())})
		}
	}
	sort.SliceStable(e.lots, func(a, b int) bool { return e.lots[a].acquired.Before(e.lots[b].acquired) })
	reported := e.forms()
	if err := e.trades(reported); err != nil {
		return nil, nil, err
	}
	if err := e.planned(); err != nil {
		return nil, nil, err
	}
	return e.projection(), e.notes, nil
}

// forms adds each 1099-B line, and returns the date and proceeds of each,
// so a trade a line reports is not counted twice.
func (e *expansion) forms() map[string]bool {
	reported := map[string]bool{}
	for _, p := range e.m.GetParts() {
		for _, st := range p.GetFromForms().GetStatements() {
			if !model.Usable(st.GetSource()) {
				continue
			}
			for i, t := range st.GetStatement().GetTransactions() {
				sold, _ := civil.FromProto(t.GetDateSoldOrDisposed())
				acq, _ := civil.FromProto(t.GetDateAcquired())
				term := scenariosv1.GainTerm_GAIN_TERM_SHORT
				if t.GetHoldingPeriod() == form1099b.Form1099B_HOLDING_PERIOD_LONG_TERM || (t.GetHoldingPeriod() == form1099b.Form1099B_HOLDING_PERIOD_UNSPECIFIED && longTerm(acq, sold)) {
					term = scenariosv1.GainTerm_GAIN_TERM_LONG
				}
				box := printedBox(t.GetForm_8949Checkbox(), term)
				if box == scenariosv1.Form8949Box_FORM_8949_BOX_UNSPECIFIED {
					box = form8949(term, true, t.GetBasisReportedToIrs() && !t.GetNoncoveredSecurity())
				}
				proceeds, basis, wash := model.Value(t.GetProceeds()), model.Value(t.GetCostOrOtherBasis()), model.Value(t.GetWashSaleLossDisallowed())
				reported[fmt.Sprintf("%s|%d", sold, proceeds)] = true
				e.through = civil.Later(e.through, sold)
				e.items = append(e.items, &scenariosv1.Item{
					Id: model.ItemID(e.prefix(), sold.String(), "sale_lot", fmt.Sprintf("%s-%d", st.GetId(), i)), Date: sold.Proto(),
					Status: scenariosv1.ItemStatus_ITEM_STATUS_MADE, Kind: scenariosv1.ItemKind_ITEM_KIND_SALE_LOT,
					Origin: &scenariosv1.Origin{Basis: scenariosv1.Origin_BASIS_DOCUMENT, Part: p.GetId(), Evidence: []string{st.GetId()}},
					Figures: &scenariosv1.Item_Sale{Sale: &scenariosv1.SaleLot{Acquired: acq.Proto(), Proceeds: model.Money(proceeds),
						Basis: model.Money(basis), WashSaleDisallowed: model.Money(wash), Term: term, Gain: model.Money(proceeds - basis + wash), Box: box}},
				})
			}
		}
	}
	return reported
}

// longTerm reports whether shares bought on acquired and sold on sold were
// held more than one year, 26 U.S.C. § 1222.
func longTerm(acquired, sold civil.Date) bool {
	if acquired.IsZero() || sold.IsZero() {
		return false
	}
	anniversary, err := civil.New(acquired.Year()+1, acquired.Month(), acquired.Day())
	if err != nil {
		anniversary = civil.Must(acquired.Year()+1, time.March, 1)
	}
	return sold.After(anniversary)
}

// printedBox is the Form 8949 box a 1099-B's checkbox prints, A to F, or
// unspecified when it prints none or one that disagrees with the term: a
// short-term box for a long-term sale is a misreading, not the form.
func printedBox(checkbox string, term scenariosv1.GainTerm) scenariosv1.Form8949Box {
	boxes := map[string]scenariosv1.Form8949Box{
		"A": scenariosv1.Form8949Box_FORM_8949_BOX_A, "B": scenariosv1.Form8949Box_FORM_8949_BOX_B, "C": scenariosv1.Form8949Box_FORM_8949_BOX_C,
		"D": scenariosv1.Form8949Box_FORM_8949_BOX_D, "E": scenariosv1.Form8949Box_FORM_8949_BOX_E, "F": scenariosv1.Form8949Box_FORM_8949_BOX_F,
	}
	box := boxes[strings.ToUpper(strings.TrimSpace(checkbox))]
	if box == scenariosv1.Form8949Box_FORM_8949_BOX_UNSPECIFIED {
		return box
	}
	if (box >= scenariosv1.Form8949Box_FORM_8949_BOX_D) != (term == scenariosv1.GainTerm_GAIN_TERM_LONG) {
		return scenariosv1.Form8949Box_FORM_8949_BOX_UNSPECIFIED
	}
	return box
}

func form8949(term scenariosv1.GainTerm, onForm, basisReported bool) scenariosv1.Form8949Box {
	long := term == scenariosv1.GainTerm_GAIN_TERM_LONG
	switch {
	case !onForm && long:
		return scenariosv1.Form8949Box_FORM_8949_BOX_F
	case !onForm:
		return scenariosv1.Form8949Box_FORM_8949_BOX_C
	case basisReported && long:
		return scenariosv1.Form8949Box_FORM_8949_BOX_D
	case basisReported:
		return scenariosv1.Form8949Box_FORM_8949_BOX_A
	case long:
		return scenariosv1.Form8949Box_FORM_8949_BOX_E
	}
	return scenariosv1.Form8949Box_FORM_8949_BOX_B
}

// trades runs the year's purchases and trades in date order against the
// register, which is as of January 1: a purchase adds a lot, and a trade
// sells the lots it names, else first in, first out. Basis comes from the
// register.
func (e *expansion) trades(reported map[string]bool) error {
	events, err := e.events()
	if err != nil {
		return err
	}
	for _, ev := range events {
		if ev.buy != nil {
			e.buy(ev.buy, ev.date)
			continue
		}
		tr := ev.t
		proceeds := model.Value(tr.GetProceeds())
		used := e.sell(tr.GetHolding(), tr.GetLots(), microShares(tr.GetShares()), tr.GetId(), tr.GetBasis() == nil)
		e.through = civil.Later(e.through, ev.date)
		if reported[fmt.Sprintf("%s|%d", ev.date, proceeds)] {
			continue
		}
		if tr.GetBasis() != nil {
			e.reportedTrade(tr, ev.part, ev.date, used)
			continue
		}
		e.addSales(ev.date, tr.GetHolding(), tr.GetId(), used, proceeds, microShares(tr.GetShares()),
			scenariosv1.ItemStatus_ITEM_STATUS_MADE, &scenariosv1.Origin{Basis: scenariosv1.Origin_BASIS_LEDGER, Part: ev.part, Evidence: []string{tr.GetId()}})
	}
	return nil
}

// event is a trade or a purchase, on its date.
type event struct {
	t    *scenariosv1.BrokerPart_FromTrades_Trade
	buy  *scenariosv1.BrokerPart_Purchases_Purchase
	part string
	date civil.Date
}

// events are the year's trades and purchases in date order, a day's
// purchases before its sales.
func (e *expansion) events() ([]event, error) {
	var events []event
	for _, p := range e.m.GetParts() {
		for _, t := range p.GetFromTrades().GetTrades() {
			d, err := civil.FromProto(t.GetDate())
			if err != nil || d.IsZero() {
				return nil, fmt.Errorf("%s: trade %s has no date", e.path(), t.GetId())
			}
			events = append(events, event{t: t, part: p.GetId(), date: d})
		}
		for _, b := range p.GetPurchases().GetPurchases() {
			d, err := civil.FromProto(b.GetDate())
			if err != nil || d.IsZero() {
				return nil, fmt.Errorf("%s: purchase %s has no date", e.path(), b.GetId())
			}
			events = append(events, event{buy: b, part: p.GetId(), date: d})
		}
	}
	sort.SliceStable(events, func(a, b int) bool {
		if c := events[a].date.Compare(events[b].date); c != 0 {
			return c < 0
		}
		return events[a].buy != nil && events[b].buy == nil
	})
	return events, nil
}

// reportedTrade is a trade whose basis the broker reports: one item at
// that basis, its acquired date and box as given, else the lots' date and
// the box the holding period gives. The lots it sold still leave the
// register.
func (e *expansion) reportedTrade(tr *scenariosv1.BrokerPart_FromTrades_Trade, part string, on civil.Date, used []sold) {
	acquired, _ := civil.FromProto(tr.GetAcquired())
	if acquired.IsZero() && len(used) > 0 && used[0].l != nil {
		acquired = used[0].l.acquired
	}
	term := scenariosv1.GainTerm_GAIN_TERM_SHORT
	if longTerm(acquired, on) {
		term = scenariosv1.GainTerm_GAIN_TERM_LONG
	}
	box := tr.GetBox()
	if box == scenariosv1.Form8949Box_FORM_8949_BOX_UNSPECIFIED {
		box = form8949(term, false, false)
	} else if box >= scenariosv1.Form8949Box_FORM_8949_BOX_D {
		term = scenariosv1.GainTerm_GAIN_TERM_LONG
	} else {
		term = scenariosv1.GainTerm_GAIN_TERM_SHORT
	}
	proceeds, basis, wash := model.Value(tr.GetProceeds()), model.Value(tr.GetBasis()), model.Value(tr.GetWashSaleDisallowed())
	e.items = append(e.items, &scenariosv1.Item{Id: model.ItemID(e.prefix(), on.String(), "sale_lot", tr.GetId()), Date: on.Proto(),
		Status: scenariosv1.ItemStatus_ITEM_STATUS_MADE, Kind: scenariosv1.ItemKind_ITEM_KIND_SALE_LOT,
		Origin: &scenariosv1.Origin{Basis: scenariosv1.Origin_BASIS_LEDGER, Part: part, Evidence: []string{tr.GetId()}},
		Figures: &scenariosv1.Item_Sale{Sale: &scenariosv1.SaleLot{Holding: tr.GetHolding(), Acquired: acquired.Proto(), Shares: tr.GetShares(),
			Proceeds: model.Money(proceeds), Basis: model.Money(basis), WashSaleDisallowed: model.Money(wash), Term: term,
			Gain: model.Money(proceeds - basis + wash), Box: box}}})
}

// buy adds a purchase to the register as a lot of its own, bought on its
// date at its cost. The register is as of January 1, so the year's
// purchases are the lots it lacks.
func (e *expansion) buy(b *scenariosv1.BrokerPart_Purchases_Purchase, on civil.Date) {
	sh := microShares(b.GetShares())
	e.lots = append(e.lots, &lot{id: b.GetId(), holding: b.GetHolding(), acquired: on, shares: sh, left: sh, basis: model.Value(b.GetCost())})
	sort.SliceStable(e.lots, func(a, c int) bool { return e.lots[a].acquired.Before(e.lots[c].acquired) })
	e.through = civil.Later(e.through, on)
}

// sold is one lot's part of a sale.
type sold struct {
	l      *lot
	shares int64
}

// sell takes shares from a holding's lots: those named, else first in first
// out. Under the specific-lots method a sale naming none is asked about,
// and so is one the lots don't cover, unless ask is false because the
// broker reports the sale's basis.
func (e *expansion) sell(holding string, named []string, shares int64, entry string, ask bool) []sold {
	var out []sold
	want := map[string]bool{}
	for _, id := range named {
		want[id] = true
	}
	if len(named) == 0 && !e.fifo && ask {
		e.notes.Ask(model.NewQuestion(model.ItemID(e.prefix(), "lots", entry), e.prefix(),
			fmt.Sprintf("Which lots did your sale %s at %s sell?", entry, e.m.GetName()),
			"The earliest-bought shares are counted as sold until you say.",
			e.path()+".parts"))
	}
	for _, l := range e.lots {
		if shares == 0 {
			break
		}
		if l.holding != holding || l.left == 0 || (len(named) > 0 && !want[l.id]) {
			continue
		}
		n := min(l.left, shares)
		l.left -= n
		shares -= n
		out = append(out, sold{l, n})
	}
	if shares > 0 && ask {
		e.notes.Ask(model.NewQuestion(model.ItemID(e.prefix(), "basis", entry), e.prefix(),
			fmt.Sprintf("What did the shares sold in %s at %s cost? The lots held don't cover the sale.", entry, e.m.GetName()),
			"The shares the lots don't cover are counted with no basis until you say.",
			e.path()+".holdings"))
		out = append(out, sold{nil, shares})
	}
	return out
}

// addSales adds one item per holding period a sale's lots fall in.
func (e *expansion) addSales(on civil.Date, holding, entry string, used []sold, proceeds money.USCents, total int64, status scenariosv1.ItemStatus, origin *scenariosv1.Origin) {
	type group struct {
		shares   int64
		basis    money.USCents
		acquired civil.Date
		lots     []string
	}
	groups := map[scenariosv1.GainTerm]*group{}
	for _, u := range used {
		term := scenariosv1.GainTerm_GAIN_TERM_SHORT
		var basis money.USCents
		var acq civil.Date
		if u.l != nil {
			basis = model.Scale(u.l.basis, money.USCents(u.shares), money.USCents(u.l.shares))
			acq = u.l.acquired
			if longTerm(acq, on) {
				term = scenariosv1.GainTerm_GAIN_TERM_LONG
			}
		}
		g := groups[term]
		if g == nil {
			g = &group{acquired: acq}
			groups[term] = g
		}
		g.shares += u.shares
		g.basis += basis
		if u.l != nil {
			g.lots = append(g.lots, u.l.id)
		}
	}
	for _, term := range []scenariosv1.GainTerm{scenariosv1.GainTerm_GAIN_TERM_SHORT, scenariosv1.GainTerm_GAIN_TERM_LONG} {
		g := groups[term]
		if g == nil {
			continue
		}
		id := model.ItemID(e.prefix(), on.String(), "sale_lot", entry)
		if len(groups) > 1 {
			id = model.ItemID(id, map[scenariosv1.GainTerm]string{scenariosv1.GainTerm_GAIN_TERM_SHORT: "short", scenariosv1.GainTerm_GAIN_TERM_LONG: "long"}[term])
		}
		p := model.Scale(proceeds, money.USCents(g.shares), money.USCents(max(total, 1)))
		lotID := ""
		if len(g.lots) == 1 {
			lotID = g.lots[0]
		}
		e.items = append(e.items, &scenariosv1.Item{Id: id, Date: on.Proto(), Status: status, Kind: scenariosv1.ItemKind_ITEM_KIND_SALE_LOT, Origin: origin,
			Figures: &scenariosv1.Item_Sale{Sale: &scenariosv1.SaleLot{Holding: holding, Lot: lotID, Acquired: g.acquired.Proto(),
				Shares: quantity(g.shares), Proceeds: model.Money(p), Basis: model.Money(g.basis), Term: term,
				Gain: model.Money(p - g.basis), Box: form8949(term, false, false)}}})
	}
}

// planned adds the planned sales and gains.
func (e *expansion) planned() error {
	for _, p := range e.m.GetParts() {
		switch {
		case p.GetPlannedSale() != nil && p.GetPlannedSale().GetActual().GetIs() == "":
			s := p.GetPlannedSale()
			d, err := civil.FromProto(s.GetDate())
			if err != nil || d.IsZero() {
				return fmt.Errorf("%s.parts[%s].planned_sale.date is not a day", e.path(), p.GetId())
			}
			shares := microShares(s.GetShares())
			price := e.price(s)
			proceeds := model.Scale(price, money.USCents(shares), 1_000_000)
			var named []string
			if s.GetLot() != "" {
				named = []string{s.GetLot()}
			}
			used := e.sell(s.GetHolding(), named, shares, p.GetId(), true)
			before := len(e.items)
			e.addSales(d, s.GetHolding(), p.GetId(), used, proceeds, shares, scenariosv1.ItemStatus_ITEM_STATUS_EXPECTED,
				&scenariosv1.Origin{Basis: scenariosv1.Origin_BASIS_ASSUMED, Part: p.GetId()})
			for _, it := range e.items[before:] {
				if model.Value(it.GetSale().GetGain()) < 0 {
					e.washSale(p.GetId(), s.GetHolding(), d)
				}
			}
		case p.GetPlannedGain() != nil && p.GetPlannedGain().GetActual().GetIs() == "":
			g := p.GetPlannedGain()
			d, err := civil.FromProto(g.GetDate())
			if err != nil || d.IsZero() {
				return fmt.Errorf("%s.parts[%s].planned_gain.date is not a day", e.path(), p.GetId())
			}
			e.items = append(e.items, &scenariosv1.Item{Id: model.ItemID(e.prefix(), d.String(), "sale_lot", p.GetId()), Date: d.Proto(),
				Status: scenariosv1.ItemStatus_ITEM_STATUS_EXPECTED, Kind: scenariosv1.ItemKind_ITEM_KIND_SALE_LOT,
				Origin:  &scenariosv1.Origin{Basis: scenariosv1.Origin_BASIS_ASSUMED, Part: p.GetId()},
				Figures: &scenariosv1.Item_Sale{Sale: &scenariosv1.SaleLot{Term: g.GetTerm(), Gain: model.Money(model.Value(g.GetAmount())), Box: form8949(g.GetTerm(), false, false)}}})
		}
	}
	return nil
}

// price is a planned sale's price per share: typed, or the holding's price
// on as_of, 10% lower or higher.
func (e *expansion) price(s *scenariosv1.BrokerPart_PlannedSale) money.USCents {
	if s.GetPerShare() != nil {
		return model.Value(s.GetPerShare())
	}
	var today money.USCents
	for _, h := range e.m.GetHoldings() {
		if h.GetId() == s.GetHolding() {
			today = model.Value(h.GetPrice())
		}
	}
	switch s.GetAssumption() {
	case scenariosv1.PriceAssumption_PRICE_ASSUMPTION_TEN_PERCENT_LOWER:
		return model.Scale(today, 9, 10)
	case scenariosv1.PriceAssumption_PRICE_ASSUMPTION_TEN_PERCENT_HIGHER:
		return model.Scale(today, 11, 10)
	}
	return today
}

// washSale asks about a planned sale at a loss with a purchase of the same
// holding within 30 days either side, 26 U.S.C. § 1091.
func (e *expansion) washSale(part, holding string, on civil.Date) {
	for _, p := range e.m.GetParts() {
		for _, b := range p.GetPurchases().GetPurchases() {
			d, _ := civil.FromProto(b.GetDate())
			if b.GetHolding() == holding && !d.IsZero() && abs(d.DaysFrom(on)) <= 30 {
				e.notes.Ask(model.NewQuestion(model.ItemID(e.prefix(), "wash-sale", part), e.prefix(),
					fmt.Sprintf("Your planned sale on %s is at a loss, and you bought the same holding on %s. Is it a wash sale?", on.Short(), d.Short()),
					"The loss counts in full until you say.",
					fmt.Sprintf("%s.parts[%s]", e.path(), part), "Yes", "No"))
				return
			}
		}
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func (e *expansion) projection() *scenariosv1.BrokerProjection {
	model.SortItems(e.items)
	model.RoundItems(e.items)
	out := &scenariosv1.BrokerProjection{Id: e.m.GetId(), Items: e.items, SoFarThrough: e.through.Proto()}
	var made, rest []*scenariosv1.Item
	for _, it := range e.items {
		if it.GetStatus() == scenariosv1.ItemStatus_ITEM_STATUS_MADE {
			made = append(made, it)
		} else {
			rest = append(rest, it)
		}
	}
	out.SoFar, out.Rest, out.Year = totals(made), totals(rest), totals(e.items)
	out.Planned = e.plannedTotals()
	if len(e.items) > 0 {
		hasForm := false
		for _, p := range e.m.GetParts() {
			hasForm = hasForm || p.GetFromForms() != nil
		}
		if !hasForm {
			out.Expected = []*scenariosv1.ExpectedDocument{{Form: "Form1099B", Due: model.FormsDue(e.ctx.Year()).Proto()}}
		}
	}
	return out
}

// plannedTotals adds each planned sale's or planned gain's lots, by part.
func (e *expansion) plannedTotals() []*scenariosv1.PlannedSaleTotals {
	var out []*scenariosv1.PlannedSaleTotals
	for _, p := range e.m.GetParts() {
		if p.GetPlannedSale() == nil && p.GetPlannedGain() == nil {
			continue
		}
		var short, long money.USCents
		for _, it := range e.items {
			if it.GetOrigin().GetPart() != p.GetId() {
				continue
			}
			if s := it.GetSale(); s.GetTerm() == scenariosv1.GainTerm_GAIN_TERM_LONG {
				long += model.Value(s.GetGain())
			} else {
				short += model.Value(s.GetGain())
			}
		}
		out = append(out, &scenariosv1.PlannedSaleTotals{Id: p.GetId(), Gain: model.Money(short + long),
			ShortTermGain: model.Money(short), LongTermGain: model.Money(long)})
	}
	return out
}

// totals adds the lots, and each Form 8949 box's columns.
func totals(items []*scenariosv1.Item) *scenariosv1.BrokerTotals {
	var proceeds, basis, short, long money.USCents
	boxes := map[scenariosv1.Form8949Box]*scenariosv1.Form8949Totals{}
	for _, it := range items {
		s := it.GetSale()
		proceeds += model.Value(s.GetProceeds())
		basis += model.Value(s.GetBasis())
		if s.GetTerm() == scenariosv1.GainTerm_GAIN_TERM_LONG {
			long += model.Value(s.GetGain())
		} else {
			short += model.Value(s.GetGain())
		}
		b := boxes[s.GetBox()]
		if b == nil {
			b = &scenariosv1.Form8949Totals{Box: s.GetBox(), Proceeds: model.Money(0), Basis: model.Money(0), Adjustments: model.Money(0), Gain: model.Money(0)}
			boxes[s.GetBox()] = b
		}
		b.Proceeds = model.Money(model.Value(b.Proceeds) + model.Value(s.GetProceeds()))
		b.Basis = model.Money(model.Value(b.Basis) + model.Value(s.GetBasis()))
		b.Adjustments = model.Money(model.Value(b.Adjustments) + model.Value(s.GetWashSaleDisallowed()))
		b.Gain = model.Money(model.Value(b.Gain) + model.Value(s.GetGain()))
	}
	out := &scenariosv1.BrokerTotals{Items: &scenariosv1.SaleTotals{Proceeds: model.Money(proceeds), Basis: model.Money(basis),
		ShortTermGain: model.Money(short), LongTermGain: model.Money(long)}}
	for k := scenariosv1.Form8949Box_FORM_8949_BOX_A; k <= scenariosv1.Form8949Box_FORM_8949_BOX_F; k++ {
		if b := boxes[k]; b != nil {
			out.Boxes = append(out.Boxes, b)
		}
	}
	return out
}
