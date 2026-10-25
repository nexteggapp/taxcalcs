// Package business expands a BusinessModel, one self-employment business,
// into receipts, fees, expenses and months.
//
// Its slots are months, filled as an account's are: a YearTotal for the
// lines it states, then evidence, then Monthly from its start, then the
// default, the pace so far. A client's 1099-NEC or 1099-K states that
// client's receipts for the year, and an ADJUSTMENT reconciles the bank
// receipts from the client to it. See docs/models.md.
package business

import (
	"fmt"

	"github.com/nexteggapp/taxcalcs/money"
	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/internal/civil"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
)

// lines are receipts, fees and expenses.
type lines [3]money.USCents

const (
	receipts = iota
	fees
	expenses
)

func (l lines) plus(o lines) lines {
	for i := range l {
		l[i] += o[i]
	}
	return l
}

func (l lines) proto(client string) *scenariosv1.BusinessFigures {
	return &scenariosv1.BusinessFigures{Receipts: model.Money(l[receipts]), Fees: model.Money(l[fees]), Expenses: model.Money(l[expenses]), Client: client}
}

type item struct {
	id     string
	date   civil.Date
	status scenariosv1.ItemStatus
	kind   scenariosv1.ItemKind
	origin *scenariosv1.Origin
	l      lines
	client string
}

type expansion struct {
	ctx   *model.Context
	m     *scenariosv1.BusinessModel
	notes *model.Notes
	items []item
	end   civil.Date
	// stated is each client's receipts a form states.
	stated map[string]money.USCents
	// oneOff holds the items of expenses bought once, on Schedule C line
	// 13, which the pace so far leaves out.
	oneOff map[string]bool
}

func (e *expansion) prefix() string { return model.ItemID("businesses", e.m.GetId()) }
func (e *expansion) path() string   { return fmt.Sprintf("model.businesses[%s]", e.m.GetId()) }

// Expand expands one business model.
func Expand(ctx *model.Context, m *scenariosv1.BusinessModel) (*scenariosv1.BusinessProjection, *model.Notes, error) {
	e := &expansion{ctx: ctx, m: m, notes: &model.Notes{}, stated: map[string]money.USCents{}, oneOff: map[string]bool{}}
	if err := e.evidence(); err != nil {
		return nil, nil, err
	}
	e.reconcile()
	if err := e.rest(); err != nil {
		return nil, nil, err
	}
	return e.projection(), e.notes, nil
}

func (e *expansion) evidence() error {
	for _, p := range e.m.GetParts() {
		switch {
		case p.GetFromLedger() != nil:
			for _, en := range p.GetFromLedger().GetEntries() {
				if en.GetMatches().GetNotThisSource() != nil {
					continue
				}
				d, err := civil.FromProto(en.GetDate())
				if err != nil || d.IsZero() {
					return fmt.Errorf("%s: entry %s has no date", e.path(), en.GetId())
				}
				var l lines
				kind, word := scenariosv1.ItemKind_ITEM_KIND_RECEIPT, "receipt"
				switch en.GetKind() {
				case scenariosv1.BusinessEntryKind_BUSINESS_ENTRY_KIND_FEE:
					l[fees], kind, word = model.Value(en.GetAmount()), scenariosv1.ItemKind_ITEM_KIND_FEE, "fee"
				case scenariosv1.BusinessEntryKind_BUSINESS_ENTRY_KIND_EXPENSE:
					l[expenses], kind, word = model.Value(en.GetAmount()), scenariosv1.ItemKind_ITEM_KIND_EXPENSE, "expense"
				default:
					l[receipts] = model.Value(en.GetAmount())
				}
				id := model.ItemID(e.prefix(), d.String(), word, en.GetId())
				e.oneOff[id] = en.GetLine() == scenariosv1.ScheduleCExpense_SCHEDULE_C_EXPENSE_DEPRECIATION
				e.items = append(e.items, item{id: id, date: d,
					status: scenariosv1.ItemStatus_ITEM_STATUS_MADE, kind: kind, l: l, client: en.GetClient(),
					origin: &scenariosv1.Origin{Basis: scenariosv1.Origin_BASIS_LEDGER, Part: p.GetId(), Evidence: []string{en.GetId()}}})
			}
		case p.GetFromForms() != nil:
			for _, f := range p.GetFromForms().GetForms() {
				if !model.Usable(f.GetSource()) {
					continue
				}
				switch {
				case f.GetForm_1099Nec() != nil:
					// Form 1099-NEC box 1 (nonemployee_compensation) reports gross self-employment
					// receipts from the client for Schedule C Part I line 1 (26 U.S.C. §§ 61, 1402).
					e.stated[formClient(f.GetClient(), f.GetForm_1099Nec().GetPayer().GetName())] += model.Value(f.GetForm_1099Nec().GetNonemployeeCompensation())
				case f.GetForm_1099K() != nil:
					// Form 1099-K box 1a (gross_payment_transactions) reports gross payment card /
					// third-party network transactions under 26 U.S.C. § 6050W for Schedule C line 1.
					e.stated[formClient(f.GetClient(), f.GetForm_1099K().GetFiler().GetName())] += model.Value(f.GetForm_1099K().GetGrossPaymentTransactions())
				case f.GetForm_1099Misc() != nil:
					// Form 1099-MISC attached to a trade or business (Schedule C Part I line 1):
					// Sum the gross business-related payment boxes reported by the payer: box 1 (rents),
					// box 2 (royalties), box 3 (other_income), box 5 (fishing_boat_proceeds), box 6
					// (medical_and_health_care_payments), box 9 (crop_insurance_proceeds), box 10
					// (gross_proceeds_paid_to_attorney), box 11 (fish_purchased_for_resale), and
					// box 15 (nonqualified_deferred_compensation).
					m := f.GetForm_1099Misc()
					receipts := model.Value(m.GetOtherIncome()) +
						model.Value(m.GetMedicalAndHealthCarePayments()) +
						model.Value(m.GetCropInsuranceProceeds()) +
						model.Value(m.GetFishingBoatProceeds()) +
						model.Value(m.GetFishPurchasedForResale()) +
						model.Value(m.GetGrossProceedsPaidToAttorney()) +
						model.Value(m.GetNonqualifiedDeferredCompensation()) +
						model.Value(m.GetRents()) +
						model.Value(m.GetRoyalties())
					e.stated[formClient(f.GetClient(), m.GetPayer().GetName())] += receipts
				}
			}
		case p.GetStopped() != nil:
			e.end, _ = civil.FromProto(p.GetStopped().GetOn())
		case p.GetStops() != nil:
			if d, _ := civil.FromProto(p.GetStops().GetOn()); d.After(e.ctx.EvidenceThrough()) {
				e.end = d
			}
		}
	}
	return nil
}

// formClient is the client a form is from: the form's client, or when it
// names none, its payer. A form states that client's receipts for the
// whole year, so the ledger's receipts from them are reconciled to it
// rather than added to it.
func formClient(client, payer string) string {
	if client != "" {
		return client
	}
	return payer
}

// reconcile adds, for each client a form states, an ADJUSTMENT for the form
// less the bank receipts from that client, dated December 31.
func (e *expansion) reconcile() {
	for client, want := range e.stated {
		var have money.USCents
		for _, it := range e.items {
			if it.client == client {
				have += it.l[receipts]
			}
		}
		if want == have {
			continue
		}
		var l lines
		l[receipts] = want - have
		e.items = append(e.items, item{id: model.ItemID(e.prefix(), "client", client, "adjustment"), date: e.ctx.YearEnd(),
			status: scenariosv1.ItemStatus_ITEM_STATUS_MADE, kind: scenariosv1.ItemKind_ITEM_KIND_ADJUSTMENT, l: l, client: client,
			origin: &scenariosv1.Origin{Basis: scenariosv1.Origin_BASIS_DOCUMENT}})
	}
}

// rest fills the months after the evidence. Receipts from a client a form
// states are left out of the pace, since the form states that client's
// year.
func (e *expansion) rest() error {
	evidence, inMonth := e.evidenceByMonth()
	var months []civil.Date
	for _, mo := range e.ctx.Months() {
		if mo.After(e.ctx.EvidenceThrough()) && (e.end.IsZero() || !model.MonthOf(e.end).Before(mo)) {
			months = append(months, mo)
		}
	}
	pace, paceMonths := e.pace(inMonth)
	for idx, mo := range months {
		l, part := e.month(mo, pace, paceMonths[mo.String()])
		if p, shares := e.yearTotal(evidence, len(months), idx); p != "" {
			for i, v := range shares {
				if v != nil {
					l[i] = *v
				}
			}
			part = p
		}
		for i := range l {
			l[i] = max(0, l[i]-inMonth[mo.String()][i])
		}
		if l == (lines{}) {
			continue
		}
		e.items = append(e.items, item{id: model.ItemID(e.prefix(), mo.String(), "month"), date: mo,
			status: scenariosv1.ItemStatus_ITEM_STATUS_EXPECTED, kind: scenariosv1.ItemKind_ITEM_KIND_MONTH, l: l,
			origin: &scenariosv1.Origin{Basis: scenariosv1.Origin_BASIS_ASSUMED, Part: part}})
	}
	return nil
}

// evidenceByMonth adds the made items, the whole year and each month's,
// leaving out receipts from a client a form states.
func (e *expansion) evidenceByMonth() (lines, map[string]lines) {
	var evidence lines
	inMonth := map[string]lines{}
	for _, it := range e.items {
		if it.status != scenariosv1.ItemStatus_ITEM_STATUS_MADE {
			continue
		}
		l := it.l
		if _, ok := e.stated[it.client]; ok && it.client != "" {
			l[receipts] = 0
		}

		evidence = evidence.plus(l)
		k := model.MonthOf(it.date).String()
		inMonth[k] = inMonth[k].plus(l)
	}
	return evidence, inMonth
}

// pace is the default, the pace so far: the months with entries recur at
// the median gap between them, at the average month's figures, through the
// year's end. Expenses bought once, on Schedule C line 13, and receipts
// from a client a form states, are left out of the average. One month so far recurs a year on. It returns the average
// and the months it falls on.
func (e *expansion) pace(inMonth map[string]lines) (lines, map[string]bool) {
	recurring := map[string]lines{}
	for _, it := range e.items {
		if it.status != scenariosv1.ItemStatus_ITEM_STATUS_MADE {
			continue
		}
		l := it.l
		if _, ok := e.stated[it.client]; ok && it.client != "" {
			l[receipts] = 0
		}
		if e.oneOff[it.id] {
			l[expenses] = 0
		}
		k := model.MonthOf(it.date).String()
		recurring[k] = recurring[k].plus(l)
	}
	var months []civil.Date
	var total lines
	for _, mo := range e.ctx.Months() {
		if _, ok := inMonth[mo.String()]; ok && !mo.After(e.ctx.EvidenceThrough().AddDays(31)) {
			months = append(months, mo)
			total = total.plus(recurring[mo.String()])
		}
	}
	on := map[string]bool{}
	if len(months) == 0 || e.m.GetDefaultPart() == nil {
		return lines{}, on
	}
	var avg lines
	for i := range avg {
		avg[i] = total[i] / money.USCents(len(months))
	}
	for _, d := range model.CadenceOf(months).Through(months[len(months)-1], e.ctx.YearEnd()) {
		on[d.String()] = true
	}
	// The month of as_of is the next source's less what is in it.
	if asOf := model.MonthOf(e.ctx.AsOf()); inMonth[asOf.String()] != (lines{}) {
		on[asOf.String()] = true
	}
	return avg, on
}

// month is a month's figures by a Monthly part from its start, else the
// pace so far on the months its cadence falls on, and the part.
func (e *expansion) month(mo civil.Date, pace lines, paced bool) (lines, string) {
	l, part := lines{}, e.m.GetDefaultPart().GetId()
	if paced {
		l = pace
	}
	for _, p := range e.m.GetParts() {
		if mon := p.GetMonthly(); mon != nil {
			if from, _ := civil.FromProto(mon.GetFrom()); !from.After(mo) {
				l, part = lines{model.Value(mon.GetReceipts()), 0, model.Value(mon.GetExpenses())}, p.GetId()
			}
		}
	}
	return l, part
}

// yearTotal returns a YearTotal's part and, for each line it states, the
// month idx's share of the year less the evidence.
func (e *expansion) yearTotal(evidence lines, n, idx int) (string, [3]*money.USCents) {
	var out [3]*money.USCents
	for _, p := range e.m.GetParts() {
		yt := p.GetYearTotal()
		if yt == nil {
			continue
		}
		for i, stated := range [3]*commonv1.Money{yt.GetReceipts(), yt.GetFees(), yt.GetExpenses()} {
			if stated != nil {
				v := model.Share(max(0, model.Value(stated)-evidence[i]), n)[idx]
				out[i] = &v
			}
		}
		return p.GetId(), out
	}
	return "", out
}

func (e *expansion) projection() *scenariosv1.BusinessProjection {
	out := &scenariosv1.BusinessProjection{Id: e.m.GetId(), SoFarThrough: e.ctx.EvidenceThrough().Proto()}
	e.roundItems()
	var made, rest lines
	for _, it := range e.items {
		out.Items = append(out.Items, &scenariosv1.Item{Id: it.id, Date: it.date.Proto(), Status: it.status, Kind: it.kind,
			Origin: it.origin, Figures: &scenariosv1.Item_Business{Business: it.l.proto(it.client)}})
		if it.status == scenariosv1.ItemStatus_ITEM_STATUS_MADE {
			made = made.plus(it.l)
		} else {
			rest = rest.plus(it.l)
		}
	}
	model.SortItems(out.Items)
	out.SoFar = totals(made)
	out.Rest = totals(rest)
	out.Year = totals(made.plus(rest))
	for _, c := range e.m.GetClients() {
		if c.GetForm() == scenariosv1.ClientForm_CLIENT_FORM_1099_NEC || c.GetForm() == scenariosv1.ClientForm_CLIENT_FORM_1099_K {
			if _, ok := e.stated[c.GetName()]; !ok {
				form := "Form1099Nec"
				if c.GetForm() == scenariosv1.ClientForm_CLIENT_FORM_1099_K {
					form = "Form1099K"
				}
				out.Expected = append(out.Expected, &scenariosv1.ExpectedDocument{Form: form, Due: model.FormsDue(e.ctx.Year()).Proto()})
			}
		}
	}
	return out
}

// totals is Schedule C's lines from the items: line 1 the receipts, line 2
// the fees and refunds, line 28 the expenses, line 31 what is left.
func totals(l lines) *scenariosv1.BusinessTotals {
	return &scenariosv1.BusinessTotals{Items: l.proto(""), Boxes: &scenariosv1.ScheduleCLines{
		GrossReceipts: model.Money(l[receipts]), ReturnsAndAllowances: model.Money(l[fees]),
		TotalExpenses: model.Money(l[expenses]), NetProfit: model.Money(l[receipts] - l[fees] - l[expenses])}}
}

// roundItems rounds each item's lines to the dollar with a running total
// per line, the made items first (see [model.Rounder]).
func (e *expansion) roundItems() {
	runs := model.Runs{}
	for i := range e.items {
		it := &e.items[i]
		r := runs.For(it.status == scenariosv1.ItemStatus_ITEM_STATUS_MADE, it.origin.GetPart())
		for k := range it.l {
			it.l[k] = r.Round(fmt.Sprint(k), it.l[k])
		}
	}
}
