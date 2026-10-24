// Package itemised expands an ItemisedModel, one source of an itemised
// deduction, into items: a mortgage's monthly interest, a property's tax
// installments, gifts to charity and medical payments. Each reaches one
// Schedule A input. See docs/models.md.
package itemised

import (
	"fmt"
	"sort"

	"github.com/nexteggapp/taxcalcs/money"
	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/internal/civil"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
)

// item is one itemised item while it is made.
type item struct {
	id     string
	date   civil.Date
	status scenariosv1.ItemStatus
	kind   scenariosv1.ItemKind
	origin *scenariosv1.Origin
	amount money.USCents
	payee  string
	// est lists the fields a rule filled, by path in the item.
	est []string
}

type expansion struct {
	ctx   *model.Context
	m     *scenariosv1.ItemisedModel
	notes *model.Notes
	items []item
	// whole is a whole-year document's figure, such as a 1098's box 1,
	// and wholePart the part that holds it.
	whole     money.USCents
	hasWhole  bool
	wholePart string
	through   civil.Date
}

func (e *expansion) prefix() string { return model.ItemID("itemised", e.m.GetId()) }
func (e *expansion) path() string   { return fmt.Sprintf("model.itemised[%s]", e.m.GetId()) }

func (e *expansion) add(id string, d civil.Date, status scenariosv1.ItemStatus, kind scenariosv1.ItemKind, basis scenariosv1.Origin_Basis, part string, evidence []string, amount money.USCents, payee string) {
	e.items = append(e.items, item{id: id, date: d, status: status, kind: kind,
		origin: &scenariosv1.Origin{Basis: basis, Part: part, Evidence: evidence}, amount: amount, payee: payee})
	if status == scenariosv1.ItemStatus_ITEM_STATUS_MADE {
		e.through = civil.Later(e.through, d)
	}
}

const (
	made     = scenariosv1.ItemStatus_ITEM_STATUS_MADE
	expected = scenariosv1.ItemStatus_ITEM_STATUS_EXPECTED
	document = scenariosv1.Origin_BASIS_DOCUMENT
	ledger   = scenariosv1.Origin_BASIS_LEDGER
	assumed  = scenariosv1.Origin_BASIS_ASSUMED
)

// Expand expands one itemised model.
func Expand(ctx *model.Context, m *scenariosv1.ItemisedModel) (*scenariosv1.ItemisedProjection, *model.Notes, error) {
	e := &expansion{ctx: ctx, m: m, notes: &model.Notes{}}
	var err error
	switch {
	case m.GetMortgage() != nil:
		err = e.mortgage(m.GetMortgage())
	case m.GetPropertyTax() != nil:
		err = e.propertyTax(m.GetPropertyTax())
	case m.GetCharity() != nil:
		err = e.charity(m.GetCharity())
	case m.GetMedical() != nil:
		err = e.medical(m.GetMedical())
	}
	if err != nil {
		return nil, nil, err
	}
	return e.projection(), e.notes, nil
}

// loanState is a mortgage's loan as the evidence leaves it.
type loanState struct {
	balance, payment money.USCents
	rate             *commonv1.Quantity
	latest, end      civil.Date
}

// mortgage: a 1098 states the year's interest; statements are a month's
// interest each; the months after are a Refinanced loan's from its start,
// else a YearTotal's equal shares, else the default, amortised from the
// latest statement's balance and rate, or the loan's.
func (e *expansion) mortgage(m *scenariosv1.MortgageModel) error {
	st, err := e.mortgageEvidence(m)
	if err != nil || e.hasWhole {
		return err
	}
	var months []civil.Date
	for _, mo := range e.ctx.Months() {
		if mo.After(model.MonthOf(st.latest)) && (st.end.IsZero() || !mo.After(model.MonthOf(st.end))) && (!st.latest.IsZero() || mo.After(e.ctx.EvidenceThrough())) {
			months = append(months, mo)
		}
	}
	if e.restOfYear(m.GetParts(), scenariosv1.ItemKind_ITEM_KIND_INTEREST, "interest") {
		return nil
	}
	shares, sharePart := e.mortgageYearTotal(m, len(months))
	if st.balance == 0 && shares == nil && len(months) > 0 && e.repeatInterest(m, months) {
		return nil
	}
	e.amortise(m, st, months, shares, sharePart)
	return nil
}

// mortgageEvidence reads a mortgage's 1098s, statements and end.
func (e *expansion) mortgageEvidence(m *scenariosv1.MortgageModel) (loanState, error) {
	loan := m.GetLoan()
	st := loanState{balance: model.Value(loan.GetBalance()), payment: model.Value(loan.GetPayment()), rate: loan.GetRate()}
	for _, p := range m.GetParts() {
		switch {
		case p.GetFromForm_1098() != nil:
			if model.Usable(p.GetFromForm_1098().GetSource()) {
				e.whole += model.Value(p.GetFromForm_1098().GetForm().GetMortgageInterestReceived())
				e.hasWhole, e.wholePart = true, p.GetId()
			}
		case p.GetFromStatements() != nil:
			if err := e.statements(p, &st); err != nil {
				return st, err
			}
		case p.GetRepaid() != nil:
			st.end, _ = civil.FromProto(p.GetRepaid().GetOn())
		case p.GetPaidOff() != nil:
			if d, _ := civil.FromProto(p.GetPaidOff().GetOn()); d.After(st.latest) {
				st.end = d
			}
		}
	}
	for _, p := range m.GetParts() {
		if p.GetPaidFromBank() != nil {
			e.bankMonths(p, &st)
		}
	}
	return st, nil
}

// bankMonths adds, for each bank payment in a month no statement covers,
// that month's interest: the latest statement's, else a month of the
// loan's rate on its balance. It is estimated, and with neither the result
// asks. The payments are evidence, so the latest evidence date moves.
func (e *expansion) bankMonths(p *scenariosv1.MortgagePart, st *loanState) {
	covered := map[string]bool{}
	for _, it := range e.items {
		covered[model.MonthOf(it.date).String()] = true
	}
	interest, ok := e.latestInterest()
	if !ok {
		pct, _ := model.Percent(st.rate)
		interest, ok = model.OfFine(pct, st.balance)/12, st.balance > 0
	}
	for _, pay := range p.GetPaidFromBank().GetPayments() {
		d, err := civil.FromProto(pay.GetDate())
		if err != nil || d.IsZero() || pay.GetMatches().GetNotThisSource() != nil || covered[model.MonthOf(d).String()] {
			continue
		}
		covered[model.MonthOf(d).String()] = true
		if !ok {
			e.notes.Ask(model.NewQuestion(model.ItemID(e.prefix(), "loan"), e.prefix(),
				fmt.Sprintf("What are the balance, rate and monthly payment of your loan with %s?", e.m.GetName()),
				"A month paid from the bank with no statement counts no interest until you say.", e.path()+".mortgage.loan"))
		}
		e.add(model.ItemID(e.prefix(), d.String(), "interest", pay.GetId()), d, made, scenariosv1.ItemKind_ITEM_KIND_INTEREST,
			ledger, p.GetId(), []string{pay.GetId()}, interest, "")
		e.items[len(e.items)-1].est = []string{"itemised.amount"}
		st.latest = civil.Later(st.latest, d)
	}
}

// restOfYear adds a RestOfYear part's amount still to come, on its date or
// December 31, and reports whether there was one: it replaces what the
// default would fill after the evidence.
func (e *expansion) restOfYear(parts any, kind scenariosv1.ItemKind, word string) bool {
	var found bool
	for _, r := range restParts(parts) {
		d, _ := civil.FromProto(r.rest.GetDate())
		if d.IsZero() {
			d = e.ctx.YearEnd()
		}
		e.add(model.ItemID(e.prefix(), d.String(), word, r.id), d, expected, kind, assumed, r.id, nil, model.Value(r.rest.GetAmount()), "")
		found = true
	}
	return found
}

// restPart is a RestOfYear part and its id.
type restPart struct {
	id   string
	rest *scenariosv1.RestOfYear
}

// restParts finds the RestOfYear parts among any itemised model's parts.
func restParts(parts any) []restPart {
	var out []restPart
	add := func(id string, r *scenariosv1.RestOfYear) {
		if r != nil {
			out = append(out, restPart{id, r})
		}
	}
	switch ps := parts.(type) {
	case []*scenariosv1.MortgagePart:
		for _, p := range ps {
			add(p.GetId(), p.GetRestOfYear())
		}
	case []*scenariosv1.PropertyTaxPart:
		for _, p := range ps {
			add(p.GetId(), p.GetRestOfYear())
		}
	case []*scenariosv1.CharityPart:
		for _, p := range ps {
			add(p.GetId(), p.GetRestOfYear())
		}
	case []*scenariosv1.MedicalPart:
		for _, p := range ps {
			add(p.GetId(), p.GetRestOfYear())
		}
	}
	return out
}

// statements adds each statement's month of interest, and keeps the latest
// one's balance and rate.
func (e *expansion) statements(p *scenariosv1.MortgagePart, st *loanState) error {
	for _, s := range p.GetFromStatements().GetStatements() {
		if !model.Usable(s.GetSource()) || s.GetMatches().GetNotThisSource() != nil {
			continue
		}
		d, err := civil.FromProto(s.GetStatement().GetDate())
		if err != nil || d.IsZero() {
			return fmt.Errorf("%s: statement %s has no date", e.path(), s.GetId())
		}
		e.add(model.ItemID(e.prefix(), d.String(), "interest", s.GetId()), d, made, scenariosv1.ItemKind_ITEM_KIND_INTEREST,
			document, p.GetId(), []string{s.GetId()}, model.Value(s.GetStatement().GetInterest()), "")
		if d.After(st.latest) {
			st.latest = d
			st.balance = model.Value(s.GetStatement().GetBalance())
			if s.GetStatement().GetRate() != nil {
				st.rate = s.GetStatement().GetRate()
			}
		}
	}
	return nil
}

// repeatInterest fills the months with the latest statement's interest and
// asks for the loan, when neither the statements nor the model give a
// balance to amortise. It reports whether it did.
func (e *expansion) repeatInterest(m *scenariosv1.MortgageModel, months []civil.Date) bool {
	repeat, ok := e.latestInterest()
	if !ok {
		return false
	}
	e.notes.Ask(model.NewQuestion(model.ItemID(e.prefix(), "loan"), e.prefix(),
		fmt.Sprintf("What are the balance, rate and monthly payment of your loan with %s?", e.m.GetName()),
		fmt.Sprintf("Each month still to come counts the latest statement's interest, %v, until you say.", repeat),
		e.path()+".mortgage.loan"))
	for _, mo := range months {
		e.add(model.ItemID(e.prefix(), mo.String(), "interest"), mo, expected, scenariosv1.ItemKind_ITEM_KIND_INTEREST, assumed, m.GetDefaultPart().GetId(), nil, repeat, "")
	}
	return true
}

// amortise fills the months: a month's interest is the balance at the
// rate, and the payment less it comes off the balance. A Refinanced part
// takes over from its start; a YearTotal's shares replace the interest.
func (e *expansion) amortise(m *scenariosv1.MortgageModel, st loanState, months []civil.Date, shares []money.USCents, sharePart string) {
	part := m.GetDefaultPart().GetId()
	refinanced := map[string]bool{}
	for i, mo := range months {
		for _, p := range m.GetParts() {
			if rf := p.GetRefinanced(); rf != nil && !refinanced[p.GetId()] {
				if from, _ := civil.FromProto(rf.GetFrom()); !from.After(mo) {
					refinanced[p.GetId()] = true
					part, st.rate = p.GetId(), rf.GetLoan().GetRate()
					st.balance, st.payment = model.Value(rf.GetLoan().GetBalance()), model.Value(rf.GetLoan().GetPayment())
				}
			}
		}
		pct, _ := model.Percent(st.rate)
		interest := model.OfFine(pct, st.balance) / 12
		if st.payment > interest {
			st.balance -= st.payment - interest
		}
		filledBy := part
		if shares != nil {
			interest, filledBy = shares[i], sharePart
		}
		e.add(model.ItemID(e.prefix(), mo.String(), "interest"), mo, expected, scenariosv1.ItemKind_ITEM_KIND_INTEREST, assumed, filledBy, nil, interest, "")
	}
}

// mortgageYearTotal is a YearTotal's equal shares of the year less the
// evidence, or nil without one.
func (e *expansion) mortgageYearTotal(m *scenariosv1.MortgageModel, n int) ([]money.USCents, string) {
	for _, p := range m.GetParts() {
		if yt := p.GetYearTotal(); yt != nil && n > 0 {
			return model.Share(max(0, model.Value(yt.GetInterest())-e.madeTotal()), n), p.GetId()
		}
	}
	return nil, ""
}

// latestInterest is the latest statement's interest.
func (e *expansion) latestInterest() (money.USCents, bool) {
	var at civil.Date
	var v money.USCents
	found := false
	for _, it := range e.items {
		if it.status == made && it.kind == scenariosv1.ItemKind_ITEM_KIND_INTEREST && !it.date.Before(at) {
			at, v, found = it.date, it.amount, true
		}
	}
	return v, found
}

func (e *expansion) madeTotal() money.USCents {
	var t money.USCents
	for _, it := range e.items {
		if it.status == made {
			t += it.amount
		}
	}
	return t
}

// installment is one property tax due date and its amount.
type installment struct {
	due    civil.Date
	amount money.USCents
	paid   bool
}

// propertyTax: the slots are this year's bill's due dates, else last
// year's moved to this year, else the typed ones. A bank payment fills the
// nearest installment whose due date it precedes, within 120 days. The
// unpaid installments after the latest payment are a PlannedInstallment's,
// a YearTotal's share, or the bill's amount.
func (e *expansion) propertyTax(m *scenariosv1.PropertyTaxModel) error {
	slots := e.schedule(m)
	if err := e.bankPayments(m, slots); err != nil {
		return err
	}
	if e.restOfYear(m.GetParts(), scenariosv1.ItemKind_ITEM_KIND_INSTALLMENT, "installment") {
		return nil
	}
	var open []*installment
	for i := range slots {
		if !slots[i].paid && slots[i].due.After(e.through) && slots[i].due.Year() == e.ctx.Year() {
			open = append(open, &slots[i])
		}
	}
	var shares []money.USCents
	var sharePart string
	for _, p := range m.GetParts() {
		if yt := p.GetYearTotal(); yt != nil && len(open) > 0 {
			shares, sharePart = model.Share(max(0, model.Value(yt.GetTotal())-e.madeTotal()), len(open)), p.GetId()
		}
	}
	for i, s := range open {
		amount, on, part := plannedInstallment(m, s)
		if shares != nil {
			amount, part = shares[i], sharePart
		}
		e.add(model.ItemID(e.prefix(), s.due.String(), "installment"), on, expected, scenariosv1.ItemKind_ITEM_KIND_INSTALLMENT, assumed, part, nil, amount, "")
	}
	return nil
}

// bankPayments adds each bank payment, filling the installment it pays.
func (e *expansion) bankPayments(m *scenariosv1.PropertyTaxModel, slots []installment) error {
	for _, p := range m.GetParts() {
		for _, pay := range p.GetPaidFromBank().GetPayments() {
			if pay.GetMatches().GetNotThisSource() != nil {
				continue
			}
			d, err := civil.FromProto(pay.GetDate())
			if err != nil || d.IsZero() {
				return fmt.Errorf("%s: payment %s has no date", e.path(), pay.GetId())
			}
			slot := fillSlot(slots, d, pay.GetMatches())
			id := model.ItemID(e.prefix(), d.String(), "installment", pay.GetId())
			if slot != nil {
				slot.paid = true
				id = model.ItemID(e.prefix(), slot.due.String(), "installment")
			} else {
				e.notes.Ask(model.NewQuestion(model.ItemID(e.prefix(), "payment", pay.GetId()), id,
					fmt.Sprintf("Which installment is your %v payment on %s for?", model.Value(pay.GetAmount()), d.Short()),
					"It counts as a payment of its own until you say.",
					fmt.Sprintf("%s.property_tax.parts[%s].paid_from_bank.payments[%s].matches", e.path(), p.GetId(), pay.GetId())))
			}
			e.add(id, d, made, scenariosv1.ItemKind_ITEM_KIND_INSTALLMENT, ledger, p.GetId(), []string{pay.GetId()}, model.Value(pay.GetAmount()), "")
		}
	}
	return nil
}

// plannedInstallment is an open installment's amount, date and part: a
// PlannedInstallment's for its due date, else the bill's on the due date
// by the default part.
func plannedInstallment(m *scenariosv1.PropertyTaxModel, s *installment) (money.USCents, civil.Date, string) {
	amount, on, part := s.amount, s.due, m.GetDefaultPart().GetId()
	for _, p := range m.GetParts() {
		pi := p.GetPlannedInstallment()
		if pi == nil {
			continue
		}
		if due, _ := civil.FromProto(pi.GetDue()); due == s.due {
			part = p.GetId()
			if d, _ := civil.FromProto(pi.GetPaid()); !d.IsZero() {
				on = d
			}
			if pi.GetAmount() != nil {
				amount = model.Value(pi.GetAmount())
			}
		}
	}
	return amount, on, part
}

// schedule is the year's installments by the order in propertyTax's
// comment.
func (e *expansion) schedule(m *scenariosv1.PropertyTaxModel) []installment {
	var out []installment
	for _, p := range m.GetParts() {
		for _, b := range p.GetFromBill().GetBills() {
			if !model.Usable(b.GetSource()) {
				continue
			}
			for _, in := range b.GetBill().GetInstallments() {
				if d, err := civil.FromProto(in.GetDue()); err == nil && !d.IsZero() {
					out = append(out, installment{due: d, amount: model.Value(in.GetAmount())})
				}
			}
		}
	}
	if len(out) == 0 {
		for _, in := range m.GetLastYear().GetBill().GetInstallments() {
			if d, err := civil.FromProto(in.GetDue()); err == nil && !d.IsZero() {
				moved, err := civil.New(d.Year()+1, d.Month(), d.Day())
				if err == nil {
					out = append(out, installment{due: moved, amount: model.Value(in.GetAmount())})
				}
			}
		}
	}
	if len(out) == 0 {
		for _, t := range m.GetDueDates() {
			if d, err := civil.FromProto(t); err == nil && !d.IsZero() {
				out = append(out, installment{due: d})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].due.Before(out[j].due) })
	return out
}

// fillSlot is the installment a payment fills: the one its answer names,
// else the nearest unpaid whose due date it precedes within 120 days.
func fillSlot(slots []installment, d civil.Date, m *scenariosv1.Match) *installment {
	if s := m.GetSlot(); s != nil {
		want, _ := civil.FromProto(s)
		for i := range slots {
			if slots[i].due == want {
				return &slots[i]
			}
		}
	}
	for i := range slots {
		if !slots[i].paid && !slots[i].due.Before(d) && slots[i].due.DaysFrom(d) <= 120 {
			return &slots[i]
		}
	}
	return nil
}

// charity: every gift is additive; a receipt and its bank entry are one
// item; LikeLastYear repeats last year's gifts dated after its start.
func (e *expansion) charity(m *scenariosv1.CharityModel) error {
	kind := scenariosv1.ItemKind_ITEM_KIND_GIFT
	for _, p := range m.GetParts() {
		switch {
		case p.GetGiftFromBank() != nil:
			for _, g := range p.GetGiftFromBank().GetGifts() {
				d, _ := civil.FromProto(g.GetDate())
				e.add(model.ItemID(e.prefix(), d.String(), "gift", g.GetId()), d, made, kind, ledger, p.GetId(), []string{g.GetId()}, model.Value(g.GetAmount()), g.GetPayee())
			}
		case p.GetGiftWithReceipt() != nil:
			for _, g := range p.GetGiftWithReceipt().GetGifts() {
				if !model.Usable(g.GetSource()) {
					continue
				}
				d, _ := civil.FromProto(g.GetReceipt().GetDate())
				e.add(model.ItemID(e.prefix(), d.String(), "gift", g.GetId()), d, made, kind, document, p.GetId(), []string{g.GetId()}, model.Value(g.GetReceipt().GetAmount()), g.GetReceipt().GetPayee())
			}
		}
	}
	e.restOfYear(m.GetParts(), kind, "gift")
	for _, p := range m.GetParts() {
		switch {
		case p.GetPlannedGift() != nil && p.GetPlannedGift().GetActual().GetIs() == "":
			g := p.GetPlannedGift()
			d, _ := civil.FromProto(g.GetDate())
			e.add(model.ItemID(e.prefix(), d.String(), "gift", p.GetId()), d, expected, kind, assumed, p.GetId(), nil, model.Value(g.GetAmount()), g.GetPayee())
		case p.GetLikeLastYear() != nil:
			from, _ := civil.FromProto(p.GetLikeLastYear().GetFrom())
			if from.IsZero() {
				from = civil.Later(e.through, e.ctx.AsOf()).AddDays(1)
			}
			for i, g := range m.GetLastYear() {
				last, err := civil.FromProto(g.GetDate())
				if err != nil || last.IsZero() {
					continue
				}
				d, err := civil.New(e.ctx.Year(), last.Month(), last.Day())
				if err != nil || d.Before(from) {
					continue
				}
				e.add(model.ItemID(e.prefix(), d.String(), "gift", fmt.Sprintf("%s-%d", p.GetId(), i)), d, expected, kind, assumed, p.GetId(), nil, model.Value(g.GetAmount()), g.GetPayee())
			}
		}
	}
	return nil
}

// medical: every payment is additive; a YearTotal is the year's total,
// evidence included, adding the total less the evidence, floored at zero,
// dated December 31. Once the evidence exceeds it, it stands aside.
func (e *expansion) medical(m *scenariosv1.MedicalModel) error {
	kind := scenariosv1.ItemKind_ITEM_KIND_PAYMENT
	for _, p := range m.GetParts() {
		switch {
		case p.GetPaidFromBank() != nil:
			for _, x := range p.GetPaidFromBank().GetPayments() {
				d, _ := civil.FromProto(x.GetDate())
				e.add(model.ItemID(e.prefix(), d.String(), "payment", x.GetId()), d, made, kind, ledger, p.GetId(), []string{x.GetId()}, model.Value(x.GetAmount()), x.GetPayee())
			}
		case p.GetPaidWithReceipt() != nil:
			for _, x := range p.GetPaidWithReceipt().GetPayments() {
				if !model.Usable(x.GetSource()) {
					continue
				}
				d, _ := civil.FromProto(x.GetReceipt().GetDate())
				e.add(model.ItemID(e.prefix(), d.String(), "payment", x.GetId()), d, made, kind, document, p.GetId(), []string{x.GetId()}, model.Value(x.GetReceipt().GetAmount()), x.GetReceipt().GetPayee())
			}
		}
	}
	e.restOfYear(m.GetParts(), kind, "payment")
	for _, p := range m.GetParts() {
		switch {
		case p.GetPlannedExpense() != nil && p.GetPlannedExpense().GetActual().GetIs() == "":
			x := p.GetPlannedExpense()
			d, _ := civil.FromProto(x.GetDate())
			e.add(model.ItemID(e.prefix(), d.String(), "payment", p.GetId()), d, expected, kind, assumed, p.GetId(), nil, model.Value(x.GetAmount()), x.GetPayee())
		case p.GetYearTotal() != nil:
			total, have := model.Value(p.GetYearTotal().GetTotal()), e.madeTotal()
			if have > total {
				e.notes.Ask(model.NewQuestion(model.ItemID(e.prefix(), "stands-aside", p.GetId()), e.prefix(),
					fmt.Sprintf("Your medical payments so far, %v, are more than your total for the year, %v.", have, total),
					"The total counts for nothing until it is changed or removed.",
					fmt.Sprintf("%s.medical.parts[%s]", e.path(), p.GetId()), "Change it", "Remove it"))
				continue
			}
			e.add(model.ItemID(e.prefix(), e.ctx.YearEnd().String(), "payment", p.GetId()), e.ctx.YearEnd(), expected, kind, assumed, p.GetId(), nil, total-have, "")
		}
	}
	return nil
}

func (e *expansion) projection() *scenariosv1.ItemisedProjection {
	out := &scenariosv1.ItemisedProjection{Id: e.m.GetId(), SoFarThrough: e.through.Proto()}
	runs := model.Runs{}
	for i := range e.items {
		r := runs.For(e.items[i].status == made, e.items[i].origin.GetPart())
		e.items[i].amount = r.Round("amount", e.items[i].amount)
	}
	e.toWhole()
	var soFar, rest money.USCents
	for _, it := range e.items {
		out.Items = append(out.Items, &scenariosv1.Item{Id: it.id, Date: it.date.Proto(), Status: it.status, Kind: it.kind, Origin: it.origin, Estimated: it.est,
			Figures: &scenariosv1.Item_Itemised{Itemised: &scenariosv1.ItemisedFigures{Amount: model.Money(it.amount), Payee: it.payee}}})
		if it.status == made {
			soFar += it.amount
		} else {
			rest += it.amount
		}
	}
	model.SortItems(out.Items)
	year := soFar + rest
	yearBox, soFarBox := year, soFar
	if e.hasWhole {
		yearBox, soFarBox = e.whole, e.whole
	}
	out.SoFar = &scenariosv1.ItemisedTotals{Items: model.Money(soFar), Boxes: e.boxes(soFarBox)}
	out.Rest = &scenariosv1.ItemisedTotals{Items: model.Money(rest), Boxes: e.boxes(yearBox - soFarBox)}
	out.Year = &scenariosv1.ItemisedTotals{Items: model.Money(year), Boxes: e.boxes(yearBox)}
	if e.m.GetMortgage() != nil && !e.hasWhole {
		out.Expected = []*scenariosv1.ExpectedDocument{{Form: "Form1098", Due: model.FormsDue(e.ctx.Year()).Proto()}}
	}
	return out
}

// toWhole brings the rounded items to a whole-year document's figure, which
// Schedule A takes: an adjustment on the year's last day holds what the
// statements leave out, or count beyond it, so the year's items are the
// 1098's interest.
func (e *expansion) toWhole() {
	if !e.hasWhole {
		return
	}
	var have money.USCents
	for _, it := range e.items {
		have += it.amount
	}
	if d := model.Dollars(e.whole) - have; d != 0 {
		e.add(model.ItemID(e.prefix(), e.ctx.YearEnd().String(), "1098", e.wholePart), e.ctx.YearEnd(), made,
			scenariosv1.ItemKind_ITEM_KIND_ADJUSTMENT, document, e.wholePart, nil, d, "")
	}
}

// boxes puts an amount on the Schedule A input the model's kind reaches.
func (e *expansion) boxes(a money.USCents) *scenariosv1.ScheduleAInputs {
	b := &scenariosv1.ScheduleAInputs{}
	switch {
	case e.m.GetMortgage() != nil:
		b.MortgageInterest = model.Money(a)
	case e.m.GetPropertyTax() != nil:
		b.RealEstateTaxes = model.Money(a)
	case e.m.GetCharity() != nil:
		b.GiftsByCash = model.Money(a)
	case e.m.GetMedical() != nil:
		b.Medical = model.Money(a)
	}
	return b
}
