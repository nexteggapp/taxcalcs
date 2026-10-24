// Package account expands an AccountModel, one bank or brokerage account's
// interest and dividends, into credits and months.
//
// Its slots are months. A month is evidence once it has ended by as_of; the
// month holding as_of gets the next source's amount less what is already in
// it, floored at zero. The fill order is the employer's: a YearTotal for the
// lines it states, then evidence, then AtRate from its start, then the
// default, the pace so far. A 1099-INT or 1099-DIV states the year's boxes,
// and the credits stay as items. See docs/models.md.
package account

import (
	"fmt"

	"github.com/nexteggapp/taxcalcs/money"
	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/internal/civil"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
)

// figures is an account item's lines, indexed as the lines below.
type figures [8]money.USCents

const (
	interest = iota
	treasury
	ordinary
	qualified
	gainDistributions
	section199a
	exempt
	foreign
)

func (f figures) plus(o figures) figures {
	for i := range f {
		f[i] += o[i]
	}
	return f
}

func (f figures) proto() *scenariosv1.AccountFigures {
	m := model.Money
	return &scenariosv1.AccountFigures{Interest: m(f[interest]), TreasuryInterest: m(f[treasury]),
		OrdinaryDividends: m(f[ordinary]), QualifiedDividends: m(f[qualified]),
		CapitalGainDistributions: m(f[gainDistributions]), Section_199ADividends: m(f[section199a]),
		TaxExemptInterest: m(f[exempt]), ForeignTaxPaid: m(f[foreign])}
}

type item struct {
	id     string
	date   civil.Date
	status scenariosv1.ItemStatus
	kind   scenariosv1.ItemKind
	origin *scenariosv1.Origin
	f      figures
	est    []string
}

// Expand expands one account model.
func Expand(ctx *model.Context, m *scenariosv1.AccountModel) (*scenariosv1.AccountProjection, *model.Notes, error) {
	e := &expansion{ctx: ctx, m: m, notes: &model.Notes{}}
	if err := e.evidence(); err != nil {
		return nil, nil, err
	}
	if err := e.rest(); err != nil {
		return nil, nil, err
	}
	e.reconcile()
	return e.projection(), e.notes, nil
}

// reconcile makes the made items add up to the year's 1099s, which state
// the whole year: an ADJUSTMENT on December 31 for each line the forms
// state that the credits don't, such as foreign tax a bank entry never
// shows. The qualified share is the boxes' alone, as the items don't carry
// it.
func (e *expansion) reconcile() {
	if len(e.forms) == 0 {
		return
	}
	want := e.formBoxes()
	var have, diff figures
	for _, it := range e.items {
		if it.status == scenariosv1.ItemStatus_ITEM_STATUS_MADE {
			have = have.plus(it.f)
		}
	}
	for l := range it8 {
		if l != qualified {
			diff[l] = want[l] - have[l]
		}
	}
	if diff == (figures{}) {
		return
	}
	e.items = append(e.items, item{id: model.ItemID(e.prefix(), "forms", "adjustment"), date: e.ctx.YearEnd(),
		status: scenariosv1.ItemStatus_ITEM_STATUS_MADE, kind: scenariosv1.ItemKind_ITEM_KIND_ADJUSTMENT,
		origin: &scenariosv1.Origin{Basis: scenariosv1.Origin_BASIS_DOCUMENT}, f: diff})
}

type expansion struct {
	ctx   *model.Context
	m     *scenariosv1.AccountModel
	notes *model.Notes
	items []item
	forms []*scenariosv1.AccountPart_FromForms_Form
	end   civil.Date
}

func (e *expansion) prefix() string { return model.ItemID("accounts", e.m.GetId()) }
func (e *expansion) path() string   { return fmt.Sprintf("model.accounts[%s]", e.m.GetId()) }

func (e *expansion) evidence() error {
	replaced := map[string]bool{}
	for _, p := range e.m.GetParts() {
		for _, f := range p.GetFromForms().GetForms() {
			for _, r := range f.GetReplaces() {
				replaced[r.GetVaultRef()+"|"+r.GetName()] = true
			}
		}
	}
	for _, p := range e.m.GetParts() {
		switch {
		case p.GetFromCredits() != nil:
			for _, c := range p.GetFromCredits().GetCredits() {
				if c.GetMatches().GetNotThisSource() != nil {
					continue
				}
				d, err := civil.FromProto(c.GetDate())
				if err != nil || d.IsZero() {
					return fmt.Errorf("%s: credit %s has no date", e.path(), c.GetId())
				}
				var f figures
				f[lineOf(c.GetKind())] = model.Value(c.GetAmount())
				e.items = append(e.items, item{id: model.ItemID(e.prefix(), d.String(), "credit", c.GetId()), date: d,
					status: scenariosv1.ItemStatus_ITEM_STATUS_MADE, kind: scenariosv1.ItemKind_ITEM_KIND_CREDIT,
					origin: &scenariosv1.Origin{Basis: scenariosv1.Origin_BASIS_LEDGER, Part: p.GetId(), Evidence: []string{c.GetId()}}, f: f})
			}
		case p.GetFromForms() != nil:
			for _, f := range p.GetFromForms().GetForms() {
				if model.Usable(f.GetSource()) && !replaced[f.GetSource().GetVaultRef()+"|"+f.GetSource().GetName()] {
					e.forms = append(e.forms, f)
				}
			}
		case p.GetClosed() != nil:
			d, _ := civil.FromProto(p.GetClosed().GetOn())
			e.end = d
		case p.GetCloses() != nil:
			d, _ := civil.FromProto(p.GetCloses().GetOn())
			if d.After(e.ctx.EvidenceThrough()) {
				e.end = d
			}
		}
	}
	return nil
}

func lineOf(k scenariosv1.CreditKind) int {
	switch k {
	case scenariosv1.CreditKind_CREDIT_KIND_TREASURY_INTEREST:
		return treasury
	case scenariosv1.CreditKind_CREDIT_KIND_DIVIDEND:
		return ordinary
	case scenariosv1.CreditKind_CREDIT_KIND_CAPITAL_GAIN_DISTRIBUTION:
		return gainDistributions
	case scenariosv1.CreditKind_CREDIT_KIND_TAX_EXEMPT_INTEREST:
		return exempt
	case scenariosv1.CreditKind_CREDIT_KIND_FOREIGN_TAX:
		return foreign
	}
	return interest
}

// rest adds the year-end distributions and fills the rest of the year. A
// whole-year form states the year, so a model with one fills nothing more.
func (e *expansion) rest() error {
	if err := e.distributions(); err != nil || len(e.forms) > 0 {
		return err
	}
	var months []civil.Date
	for _, mo := range e.ctx.Months() {
		if mo.After(e.ctx.EvidenceThrough()) && (e.end.IsZero() || !model.MonthOf(e.end).Before(mo)) {
			months = append(months, mo)
		}
	}
	if len(months) == 0 {
		return nil
	}
	rates, err := e.rates()
	if err != nil {
		return err
	}
	var rateFrom civil.Date
	if len(rates) > 0 {
		rateFrom = rates[0].start
	}
	if e.m.GetDefaultPart() != nil {
		e.pace(months[len(months)-1], rateFrom)
	} else {
		e.notes.Ask(model.NewQuestion(model.ItemID(e.prefix(), "default"), e.prefix(),
			fmt.Sprintf("What will %s pay for the rest of the year?", e.m.GetName()), "Nothing is counted for its months after the evidence until you say.",
			e.path()+".default_part"))
	}
	monthly := map[string]*item{}
	for _, mo := range months {
		for k := range rates {
			if !rates[k].start.After(mo) {
				m := e.monthItem(monthly, mo, rates[k].part.GetId())
				m.f[interest] = e.monthInterest(rates[k].part.GetAtRate())
			}
		}
	}
	e.yearTotal(months, monthly)
	e.keep(monthly, months)
	return nil
}

// pace is the default, the pace so far: each kind of credit recurs at the
// median gap between its credits so far, at their average amount, on the
// cadence's dates through until. A date in a month already ended gets
// nothing, since the month's evidence is in; one in the month of as_of gets
// the average less what is already in the month. Interest stops where an
// AtRate part starts.
func (e *expansion) pace(until, rateFrom civil.Date) {
	dates := map[int][]civil.Date{}
	total := map[int]money.USCents{}
	inMonth := map[string]money.USCents{}
	for _, it := range e.items {
		if it.status != scenariosv1.ItemStatus_ITEM_STATUS_MADE {
			continue
		}
		for l, v := range it.f {
			if v != 0 {
				dates[l] = append(dates[l], it.date)
				total[l] += v
				inMonth[fmt.Sprint(l, model.MonthOf(it.date))] += v
			}
		}
	}
	for l := range it8 {
		ds := dates[l]
		// Foreign tax is what has been taken so far: it follows dividends
		// from abroad, which needn't recur, so it isn't paced.
		if len(ds) == 0 || l == foreign {
			continue
		}
		avg := total[l] / money.USCents(len(ds))
		last := ds[0]
		for _, d := range ds {
			last = civil.Later(last, d)
		}
		for _, d := range model.CadenceOf(ds).Through(last, until) {
			if !d.After(e.ctx.EvidenceThrough()) || (l == interest && !rateFrom.IsZero() && !d.Before(rateFrom)) {
				continue
			}
			var f figures
			f[l] = max(0, avg-inMonth[fmt.Sprint(l, model.MonthOf(d))])
			e.items = append(e.items, item{id: model.ItemID(e.prefix(), d.String(), lineWord[l]), date: d,
				status: scenariosv1.ItemStatus_ITEM_STATUS_EXPECTED, kind: scenariosv1.ItemKind_ITEM_KIND_CREDIT,
				origin: &scenariosv1.Origin{Basis: scenariosv1.Origin_BASIS_ASSUMED, Part: e.m.GetDefaultPart().GetId()}, f: f})
		}
	}
}

// it8 ranges over the eight lines.
var it8 [8]struct{}

// lineWord names a line in an item id.
var lineWord = [8]string{"interest", "treasury_interest", "dividend", "qualified_dividend", "capital_gain_distribution", "section_199a_dividend", "tax_exempt_interest", "foreign_tax"}

// monthItem returns the month's item, made when first asked for.
func (e *expansion) monthItem(monthly map[string]*item, mo civil.Date, part string) *item {
	m := monthly[mo.String()]
	if m == nil {
		m = &item{id: model.ItemID(e.prefix(), mo.String(), "month"), date: mo,
			status: scenariosv1.ItemStatus_ITEM_STATUS_EXPECTED, kind: scenariosv1.ItemKind_ITEM_KIND_MONTH,
			origin: &scenariosv1.Origin{Basis: scenariosv1.Origin_BASIS_ASSUMED, Part: part}}
		monthly[mo.String()] = m
	}
	return m
}

// keep adds the month items with any figure, in month order.
func (e *expansion) keep(monthly map[string]*item, months []civil.Date) {
	for _, mo := range months {
		if m := monthly[mo.String()]; m != nil && m.f != (figures{}) {
			e.items = append(e.items, *m)
		}
	}
}

// distributions adds each YearEndDistribution, on its date or December 31,
// unless its answer says a credit is it.
func (e *expansion) distributions() error {
	for _, p := range e.m.GetParts() {
		y := p.GetYearEndDistribution()
		if y == nil || y.GetActual().GetIs() != "" {
			continue
		}
		d, err := civil.FromProto(y.GetAmounts().GetDate())
		if err != nil {
			return err
		}
		if d.IsZero() {
			d = e.ctx.YearEnd()
		}
		var f figures
		f[ordinary] = model.Value(y.GetAmounts().GetOrdinaryDividends())
		f[gainDistributions] = model.Value(y.GetAmounts().GetCapitalGainDistributions())
		e.items = append(e.items, item{id: model.ItemID(e.prefix(), d.String(), "distribution", p.GetId()), date: d,
			status: scenariosv1.ItemStatus_ITEM_STATUS_EXPECTED, kind: scenariosv1.ItemKind_ITEM_KIND_DISTRIBUTION,
			origin: &scenariosv1.Origin{Basis: scenariosv1.Origin_BASIS_ASSUMED, Part: p.GetId()}, f: f})
	}
	return nil
}

// rate is an AtRate part with the day it takes effect.
type rate struct {
	start civil.Date
	part  *scenariosv1.AccountPart
}

// rates are the AtRate parts, each from its start clipped to the day after
// the evidence.
func (e *expansion) rates() ([]rate, error) {
	var out []rate
	for _, p := range e.m.GetParts() {
		if r := p.GetAtRate(); r != nil {
			from, err := civil.FromProto(r.GetFrom())
			if err != nil {
				return nil, err
			}
			start := e.ctx.EvidenceThrough().AddDays(1)
			if from.After(start) {
				start = from
			}
			out = append(out, rate{start, p})
		}
	}
	return out, nil
}

// yearTotal replaces, for each line a YearTotal states, every expected
// figure on that line with equal shares of the year less the evidence, one
// a remaining month.
func (e *expansion) yearTotal(months []civil.Date, monthly map[string]*item) {
	for _, p := range e.m.GetParts() {
		yt := p.GetYearTotal()
		if yt == nil {
			continue
		}
		lines := [8]*commonv1.Money{yt.GetInterest(), yt.GetTreasuryInterest(), yt.GetOrdinaryDividends(), yt.GetQualifiedDividends(),
			yt.GetCapitalGainDistributions(), yt.GetSection_199ADividends(), yt.GetTaxExemptInterest(), yt.GetForeignTaxPaid()}
		var evidence figures
		for i := range e.items {
			if e.items[i].status == scenariosv1.ItemStatus_ITEM_STATUS_MADE {
				evidence = evidence.plus(e.items[i].f)
			}
		}
		for l, stated := range lines {
			if stated == nil {
				continue
			}
			for i := range e.items {
				if e.items[i].status == scenariosv1.ItemStatus_ITEM_STATUS_EXPECTED && e.items[i].kind != scenariosv1.ItemKind_ITEM_KIND_DISTRIBUTION {
					e.items[i].f[l] = 0
				}
			}
			for i, v := range model.Share(max(0, model.Value(stated)-evidence[l]), len(months)) {
				m := e.monthItem(monthly, months[i], p.GetId())
				m.f[l] = v
				m.origin.Part = p.GetId()
			}
		}
	}
	kept := e.items[:0]
	for _, it := range e.items {
		if it.status == scenariosv1.ItemStatus_ITEM_STATUS_MADE || it.f != (figures{}) {
			kept = append(kept, it)
		}
	}
	e.items = kept
}

// monthInterest is a month's interest at a rate a year on the balance the
// part sets, or the model's balance plus what it adds.
func (e *expansion) monthInterest(r *scenariosv1.AccountPart_AtRate) money.USCents {
	balance := model.Value(e.m.GetBalance())
	if r.GetSetBalance() != nil {
		balance = model.Value(r.GetSetBalance())
	} else {
		balance += model.Value(r.GetAdd())
	}
	rate, _ := model.Percent(r.GetRate())
	return model.OfFine(rate, balance) / 12
}

func (e *expansion) projection() *scenariosv1.AccountProjection {
	out := &scenariosv1.AccountProjection{Id: e.m.GetId(), SoFarThrough: e.ctx.EvidenceThrough().Proto()}
	runs := model.Runs{}
	for i := range e.items {
		r := runs.For(e.items[i].status == scenariosv1.ItemStatus_ITEM_STATUS_MADE, e.items[i].origin.GetPart())
		for l := range e.items[i].f {
			e.items[i].f[l] = r.Round(lineWord[l], e.items[i].f[l])
		}
	}
	var made, rest, all figures
	for _, it := range e.items {
		out.Items = append(out.Items, &scenariosv1.Item{Id: it.id, Date: it.date.Proto(), Status: it.status, Kind: it.kind,
			Origin: it.origin, Estimated: it.est, Figures: &scenariosv1.Item_Account{Account: it.f.proto()}})
		all = all.plus(it.f)
		if it.status == scenariosv1.ItemStatus_ITEM_STATUS_MADE {
			made = made.plus(it.f)
		} else {
			rest = rest.plus(it.f)
		}
	}
	model.SortItems(out.Items)
	yearBoxes := e.boxes(all)
	soFarBoxes := e.boxes(made)
	if len(e.forms) > 0 {
		yearBoxes = e.formBoxes()
		soFarBoxes = yearBoxes
	}
	var restBoxes figures
	for l := range restBoxes {
		restBoxes[l] = yearBoxes[l] - soFarBoxes[l]
	}
	out.SoFar = &scenariosv1.AccountTotals{Items: made.proto(), Boxes: soFarBoxes.proto()}
	out.Rest = &scenariosv1.AccountTotals{Items: rest.proto(), Boxes: restBoxes.proto()}
	out.Year = &scenariosv1.AccountTotals{Items: all.proto(), Boxes: yearBoxes.proto()}
	if len(e.forms) == 0 {
		due := model.FormsDue(e.ctx.Year()).Proto()
		if all[interest]+all[treasury]+all[exempt] > 0 {
			out.Expected = append(out.Expected, &scenariosv1.ExpectedDocument{Form: "Form1099Int", Due: due})
		}
		if all[ordinary]+all[gainDistributions] > 0 {
			out.Expected = append(out.Expected, &scenariosv1.ExpectedDocument{Form: "Form1099Div", Due: due})
		}
	}
	return out
}

// boxes are the item totals with the qualified share of ordinary dividends
// estimated from last year's, until this year's 1099-DIV arrives: the
// model's last_year_qualified_share, else last year's 1099-DIV. With no
// such form, none is counted as qualified, so tax is not understated.
func (e *expansion) boxes(f figures) figures {
	ly := e.m.GetLastYear().GetForm()
	if f[qualified] == 0 && f[ordinary] > 0 {
		if share, ok := model.Percent(e.m.GetLastYearQualifiedShare()); ok {
			f[qualified] = model.OfFine(share, f[ordinary])
		} else if o := model.Value(ly.GetOrdinaryDividends()); o > 0 {
			f[qualified] = model.Scale(f[ordinary], model.Value(ly.GetQualifiedDividends()), o)
		}
	}
	return f
}

func (e *expansion) formBoxes() figures {
	var f figures
	for _, form := range e.forms {
		if i := form.GetForm_1099Int(); i != nil {
			f[interest] += model.Value(i.GetInterestIncome())
			f[treasury] += model.Value(i.GetSavingsBondAndTreasuryInterest())
			f[exempt] += model.Value(i.GetTaxExemptInterest())
			f[foreign] += model.Value(i.GetForeignTaxPaid())
		}
		if d := form.GetForm_1099Div(); d != nil {
			f[ordinary] += model.Value(d.GetOrdinaryDividends())
			f[qualified] += model.Value(d.GetQualifiedDividends())
			f[gainDistributions] += model.Value(d.GetTotalCapitalGainDistribution())
			f[section199a] += model.Value(d.GetSection_199ADividends())
			f[exempt] += model.Value(d.GetExemptInterestDividends())
			f[foreign] += model.Value(d.GetForeignTaxPaid())
		}
	}
	return f
}
