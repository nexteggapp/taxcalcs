// Package taxpaid expands a TaxPaidModel, one jurisdiction's tax payments,
// into dated payment items. Its slots are the jurisdiction's estimated
// payment due dates, which the estimated-payment rules give.
//
// [Expand] reads the known payments and the planned ones; the default part,
// SpreadTheRest, needs the rules' target, so [Payments.Spread] fills it
// once taxyear/paid has asked the rules. See docs/models.md.
package taxpaid

import (
	"fmt"

	"github.com/nexteggapp/taxcalcs/money"
	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/internal/civil"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
)

// Payment is one payment the rules read: its date, amount, whether it was
// last year's overpayment, and whether it is made by as_of.
type Payment struct {
	on           civil.Date
	amount       money.USCents
	fromLastYear bool
	made         bool
}

// On is the payment's date.
func (p Payment) On() civil.Date { return p.on }

// Amount is the payment's amount.
func (p Payment) Amount() money.USCents { return p.amount }

// FromLastYear reports whether it is last year's overpayment applied.
func (p Payment) FromLastYear() bool { return p.fromLastYear }

// Made reports whether it is made by as_of, or is last year's overpayment.
func (p Payment) Made() bool { return p.made }

// Payments is one jurisdiction's payments being expanded.
type Payments struct {
	ctx   *model.Context
	m     *scenariosv1.TaxPaidModel
	items []*scenariosv1.Item
	notes model.Notes
	// planned holds the due dates a PlannedPayment fills.
	planned map[string]bool
	spread  bool
	// dues are the rules' due dates, once Open has been given them.
	dues []civil.Date
}

// Jurisdiction is US or a state's postal code.
func (p *Payments) Jurisdiction() string { return p.m.GetJurisdiction() }

// Notes returns what the expansion asked.
func (p *Payments) Notes() *model.Notes { return &p.notes }

// PaymentsFrom is when payments could first be expected, or zero for
// January 1.
func (p *Payments) PaymentsFrom() civil.Date {
	d, _ := civil.FromProto(p.m.GetPaymentsFrom())
	return d
}

func (p *Payments) prefix() string { return model.ItemID("tax_paid", p.m.GetId()) }
func (p *Payments) path() string   { return fmt.Sprintf("model.tax_paid[%s]", p.m.GetId()) }

// Expand reads a tax paid model's payments made, last year's overpayment
// and planned payments. A payment counts toward the year its purpose says;
// one with no purpose counts toward neither, and the result asks.
func Expand(ctx *model.Context, m *scenariosv1.TaxPaidModel, adjacent []*factsv1.AdjacentPayment) (*Payments, error) {
	p := &Payments{ctx: ctx, m: m, planned: map[string]bool{}}
	for _, part := range m.GetParts() {
		var err error
		switch {
		case part.GetPaymentMade() != nil:
			err = p.made(part)
		case part.GetOverpaymentApplied() != nil:
			err = p.overpayment(part)
		case part.GetPlannedPayment() != nil:
			err = p.plannedPayment(part)
		}
		if err != nil {
			return nil, err
		}
	}
	for _, a := range adjacent {
		if a.GetJurisdiction() != m.GetJurisdiction() || !ForYear(a, ctx.Year()) {
			continue
		}
		d, err := civil.FromProto(a.GetDate())
		if err != nil || d.IsZero() {
			return nil, fmt.Errorf("facts.adjacent_payments[%s].date is not a day", a.GetId())
		}
		p.items = append(p.items, &scenariosv1.Item{
			Id: model.ItemID(p.prefix(), d.String(), "payment", a.GetId()), Date: d.Proto(),
			Status: scenariosv1.ItemStatus_ITEM_STATUS_MADE, Kind: scenariosv1.ItemKind_ITEM_KIND_PAYMENT,
			Origin: &scenariosv1.Origin{Basis: scenariosv1.Origin_BASIS_LEDGER, Evidence: []string{a.GetId()}},
			Figures: &scenariosv1.Item_Payment{Payment: &scenariosv1.TaxPaymentFigures{Amount: a.GetAmount(), Purpose: a.GetPurpose(),
				Counts: a.GetPurpose().GetEstimatedFor() == int32(ctx.Year())}},
		})
	}
	p.spread = m.GetDefaultPart().GetSpreadTheRest() != nil
	return p, nil
}

// ForYear reports whether an adjacent year's payment is for the year: an
// estimated payment toward it, or its balance paid with the return. Only
// an estimated payment counts toward the year's installments.
func ForYear(a *factsv1.AdjacentPayment, year int) bool {
	p := a.GetPurpose()
	return p.GetEstimatedFor() == int32(year) || p.GetBalanceFor() == int32(year)
}

// made reads one PaymentMade: its date and amount as typed, else from the
// confirmation, else the transfer. A transfer and a confirmation are one
// item.
func (p *Payments) made(part *scenariosv1.TaxPaidPart) error {
	pm := part.GetPaymentMade()
	date := pm.GetDate()
	if date == nil {
		date = pm.GetConfirmation().GetConfirmation().GetPaidOn()
	}
	if date == nil {
		date = pm.GetTransfer().GetDate()
	}
	d, err := civil.FromProto(date)
	if err != nil || d.IsZero() {
		return fmt.Errorf("%s.parts[%s].payment_made has no date", p.path(), part.GetId())
	}
	amount := pm.GetAmount()
	if amount == nil {
		amount = pm.GetConfirmation().GetConfirmation().GetAmount()
	}
	if amount == nil {
		amount = pm.GetTransfer().GetAmount()
	}
	origin := &scenariosv1.Origin{Basis: scenariosv1.Origin_BASIS_ASSUMED, Part: part.GetId()}
	if pm.GetTransfer() != nil {
		origin.Basis = scenariosv1.Origin_BASIS_LEDGER
		origin.Evidence = append(origin.Evidence, pm.GetTransfer().GetId())
	}
	if pm.GetConfirmation() != nil {
		origin.Basis = scenariosv1.Origin_BASIS_DOCUMENT
		origin.Evidence = append(origin.Evidence, pm.GetConfirmation().GetId())
	}
	id := model.ItemID(p.prefix(), d.String(), "payment", part.GetId())
	counts := pm.GetPurpose().GetEstimatedFor() == int32(p.ctx.Year())
	if pm.GetPurpose() == nil {
		p.notes.Ask(model.NewQuestion(
			model.ItemID(p.prefix(), "purpose", part.GetId()),
			id,
			fmt.Sprintf("What was your %v payment to %s on %s for?", model.Value(amount), p.m.GetJurisdiction(), d.Short()),
			"It counts toward neither year until you say.",
			fmt.Sprintf("%s.parts[%s].payment_made.purpose", p.path(), part.GetId()),
			fmt.Sprintf("Estimated tax for %d", p.ctx.Year()), fmt.Sprintf("Tax for %d", p.ctx.Year()-1), "Not tax"))
	}
	p.items = append(p.items, &scenariosv1.Item{
		Id: id, Date: d.Proto(), Status: scenariosv1.ItemStatus_ITEM_STATUS_MADE,
		Kind: scenariosv1.ItemKind_ITEM_KIND_PAYMENT, Origin: origin,
		Figures: &scenariosv1.Item_Payment{Payment: &scenariosv1.TaxPaymentFigures{Amount: model.Money(model.Value(amount)), Purpose: pm.GetPurpose(), Counts: counts}},
	})
	return nil
}

func (p *Payments) overpayment(part *scenariosv1.TaxPaidPart) error {
	o := part.GetOverpaymentApplied()
	d, err := civil.FromProto(o.GetCredited())
	if err != nil || d.IsZero() {
		d = p.ctx.YearStart()
	}
	p.items = append(p.items, &scenariosv1.Item{
		Id: model.ItemID(p.prefix(), d.String(), "payment", part.GetId()), Date: d.Proto(),
		Status: scenariosv1.ItemStatus_ITEM_STATUS_MADE, Kind: scenariosv1.ItemKind_ITEM_KIND_PAYMENT,
		Origin: &scenariosv1.Origin{Basis: scenariosv1.Origin_BASIS_DOCUMENT, Part: part.GetId(), Evidence: []string{o.GetPriorYear()}},
		Figures: &scenariosv1.Item_Payment{Payment: &scenariosv1.TaxPaymentFigures{
			Amount: model.Money(model.Value(o.GetAmount())), Counts: true, FromLastYear: true}},
	})
	return nil
}

func (p *Payments) plannedPayment(part *scenariosv1.TaxPaidPart) error {
	pp := part.GetPlannedPayment()
	d, err := civil.FromProto(pp.GetDue())
	if err != nil || d.IsZero() {
		return fmt.Errorf("%s.parts[%s].planned_payment.due is not a day", p.path(), part.GetId())
	}
	p.planned[d.String()] = true
	p.items = append(p.items, slotItem(p.prefix(), d, part.GetId(), model.Value(pp.GetAmount())))
	return nil
}

func slotItem(prefix string, due civil.Date, part string, amount money.USCents) *scenariosv1.Item {
	return &scenariosv1.Item{
		Id: model.ItemID(prefix, due.String(), "payment"), Date: due.Proto(),
		Status: scenariosv1.ItemStatus_ITEM_STATUS_EXPECTED, Kind: scenariosv1.ItemKind_ITEM_KIND_PAYMENT,
		Origin:  &scenariosv1.Origin{Basis: scenariosv1.Origin_BASIS_ASSUMED, Part: part},
		Figures: &scenariosv1.Item_Payment{Payment: &scenariosv1.TaxPaymentFigures{Amount: model.Money(amount), Counts: true}},
	}
}

// Payments returns every payment that counts toward this year, in date
// order.
func (p *Payments) Payments() []Payment {
	var out []Payment
	items := append([]*scenariosv1.Item(nil), p.items...)
	model.SortItems(items)
	model.RoundItems(items)
	for _, it := range items {
		f := it.GetPayment()
		if !f.GetCounts() {
			continue
		}
		d, _ := civil.FromProto(it.GetDate())
		out = append(out, Payment{on: d, amount: model.Value(f.GetAmount()), fromLastYear: f.GetFromLastYear(),
			made: f.GetFromLastYear() || it.GetStatus() == scenariosv1.ItemStatus_ITEM_STATUS_MADE})
	}
	return out
}

// Open returns the due dates after as_of the default part may fill: on or
// after payments_from, and not named by a PlannedPayment. It returns none
// when the default part is not SpreadTheRest.
func (p *Payments) Open(dues []civil.Date) []civil.Date {
	p.dues = dues
	if !p.spread {
		return nil
	}
	from := p.PaymentsFrom()
	var out []civil.Date
	for _, d := range dues {
		if d.After(p.ctx.AsOf()) && !p.planned[d.String()] && (from.IsZero() || !d.Before(from)) {
			out = append(out, d)
		}
	}
	return out
}

// Spread fills the open due dates with equal payments adding to total, the
// rest of the target, as SpreadTheRest says.
func (p *Payments) Spread(open []civil.Date, total money.USCents) {
	if total <= 0 || len(open) == 0 {
		return
	}
	for i, v := range model.Share(total, len(open)) {
		p.items = append(p.items, slotItem(p.prefix(), open[i], p.m.GetDefaultPart().GetId(), v))
	}
}

// Projection returns the jurisdiction's projection.
func (p *Payments) Projection() *scenariosv1.TaxPaidProjection {
	items := append([]*scenariosv1.Item(nil), p.items...)
	model.SortItems(items)
	model.RoundItems(items)
	out := &scenariosv1.TaxPaidProjection{Id: p.m.GetId(), Jurisdiction: p.m.GetJurisdiction(), Items: items,
		SoFarThrough: p.ctx.AsOf().Proto()}
	var made, expected []*scenariosv1.Item
	for _, it := range items {
		if it.GetStatus() == scenariosv1.ItemStatus_ITEM_STATUS_MADE {
			made = append(made, it)
		} else {
			expected = append(expected, it)
		}
	}
	out.SoFar, out.Rest, out.Year = totals(made), totals(expected), totals(items)
	out.CanAdd = []*scenariosv1.TaxPaidPart{{Part: &scenariosv1.TaxPaidPart_PlannedPayment_{PlannedPayment: &scenariosv1.TaxPaidPart_PlannedPayment{
		Due: p.nextDue().Proto(), Amount: model.Money(0)}}}}
	return out
}

// nextDue is the day a new planned payment defaults to: the first due date
// after as_of that no planned payment names, or the day after as_of while
// the rules' due dates aren't known.
func (p *Payments) nextDue() civil.Date {
	for _, d := range p.dues {
		if d.After(p.ctx.AsOf()) && !p.planned[d.String()] {
			return d
		}
	}
	return p.ctx.AsOf().AddDays(1)
}

func totals(items []*scenariosv1.Item) *scenariosv1.TaxPaidTotals {
	var all, counted, over money.USCents
	for _, it := range items {
		f := it.GetPayment()
		v := model.Value(f.GetAmount())
		all += v
		if f.GetCounts() {
			counted += v
		}
		if f.GetFromLastYear() {
			over += v
		}
	}
	return &scenariosv1.TaxPaidTotals{Items: model.Money(all), Counted: model.Money(counted), OverpaymentApplied: model.Money(over)}
}
