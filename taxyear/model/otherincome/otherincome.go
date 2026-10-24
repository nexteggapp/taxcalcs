// Package otherincome expands an OtherIncomeModel, one source of other
// income, into its items.
//
// It has no slots. A whole-year form states the source's year: its assumed
// parts stand aside, except a one-off whose answer lists the form as a
// different payment. A YearAmount is the year's figures, evidence included:
// it adds the total less the evidence, floored at zero, monthly from the
// month after the evidence for a recurring source, else on December 31, and
// stands aside once the evidence exceeds it. See docs/models.md.
package otherincome

import (
	"fmt"
	"time"

	"github.com/nexteggapp/taxcalcs/money"
	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/internal/civil"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
)

type figures struct {
	gross, taxable, federal, state, expenses money.USCents
	taxableSet                               bool
	codes                                    []string
	ira                                      bool
}

func (f figures) proto() *scenariosv1.OtherIncomeFigures {
	out := &scenariosv1.OtherIncomeFigures{Gross: model.Money(f.gross), FederalWithheld: model.Money(f.federal),
		StateWithheld: model.Money(f.state), Expenses: model.Money(f.expenses), DistributionCodes: f.codes, Ira: f.ira}
	if f.taxableSet {
		out.Taxable = model.Money(f.taxable)
	}
	return out
}

func (f figures) plus(o figures) figures {
	f.gross += o.gross
	f.federal += o.federal
	f.state += o.state
	f.expenses += o.expenses
	f.taxable += o.taxableOrGross()
	f.taxableSet = true
	return f
}

func (f figures) taxableOrGross() money.USCents {
	if f.taxableSet {
		return f.taxable
	}
	return f.gross
}

type expansion struct {
	ctx     *model.Context
	m       *scenariosv1.OtherIncomeModel
	notes   *model.Notes
	items   []*scenariosv1.Item
	forms   []string
	through civil.Date
}

func (e *expansion) prefix() string { return model.ItemID("other_income", e.m.GetId()) }
func (e *expansion) path() string   { return fmt.Sprintf("model.other_income[%s]", e.m.GetId()) }

func (e *expansion) add(id string, d civil.Date, status scenariosv1.ItemStatus, basis scenariosv1.Origin_Basis, part string, evidence []string, f figures) {
	e.items = append(e.items, &scenariosv1.Item{Id: id, Date: d.Proto(), Status: status, Kind: scenariosv1.ItemKind_ITEM_KIND_PAYMENT,
		Origin: &scenariosv1.Origin{Basis: basis, Part: part, Evidence: evidence}, Figures: &scenariosv1.Item_OtherIncome{OtherIncome: f.proto()}})
	if status == scenariosv1.ItemStatus_ITEM_STATUS_MADE {
		e.through = civil.Later(e.through, d)
	}
}

// Expand expands one other income model.
func Expand(ctx *model.Context, m *scenariosv1.OtherIncomeModel) (*scenariosv1.OtherIncomeProjection, *model.Notes, error) {
	e := &expansion{ctx: ctx, m: m, notes: &model.Notes{}}
	e.evidence()
	if err := e.assumed(); err != nil {
		return nil, nil, err
	}
	return e.projection(), e.notes, nil
}

func (e *expansion) evidence() {
	made := scenariosv1.ItemStatus_ITEM_STATUS_MADE
	for _, p := range e.m.GetParts() {
		for _, f := range p.GetFromForms().GetForms() {
			if !model.Usable(f.GetSource()) {
				continue
			}
			var x figures
			d := e.ctx.YearEnd()
			switch {
			case f.GetForm_1099R() != nil:
				r := f.GetForm_1099R()
				x = figures{gross: model.Value(r.GetGrossDistribution()), federal: model.Value(r.GetFederalTaxWithheld()),
					codes: r.GetDistributionCodes(), ira: r.GetIraSepSimple()}
				if r.GetTaxableAmount() != nil && !r.GetTaxableAmountNotDetermined() {
					x.taxable, x.taxableSet = model.Value(r.GetTaxableAmount()), true
				}
				for _, s := range r.GetStateTaxWithholding() {
					x.state += model.Value(s.GetTaxWithheld())
				}
				if pd, err := civil.FromProto(r.GetDateOfPayment()); err == nil && !pd.IsZero() {
					d = pd
				}
			case f.GetFormSsa_1099() != nil:
				s := f.GetFormSsa_1099()
				x = figures{gross: model.Value(s.GetNetBenefits()), federal: model.Value(s.GetFederalTaxWithheld())}
			case f.GetForm_1099G() != nil:
				g := f.GetForm_1099G()
				x = figures{gross: model.Value(g.GetUnemploymentCompensation()) + model.Value(g.GetStateOrLocalRefund()), federal: model.Value(g.GetFederalTaxWithheld())}
			case f.GetFormW2G() != nil:
				w := f.GetFormW2G()
				x = figures{gross: model.Value(w.GetReportableWinnings()), federal: model.Value(w.GetFederalTaxWithheld())}
				if dw, err := civil.FromProto(w.GetDateWon()); err == nil && !dw.IsZero() {
					d = dw
				}
			case f.GetForm_1099Misc() != nil:
				m := f.GetForm_1099Misc()
				gross := model.Value(m.GetOtherIncome()) +
					model.Value(m.GetRents()) +
					model.Value(m.GetRoyalties()) +
					model.Value(m.GetFishingBoatProceeds()) +
					model.Value(m.GetMedicalAndHealthCarePayments()) +
					model.Value(m.GetCropInsuranceProceeds()) +
					model.Value(m.GetGrossProceedsPaidToAttorney()) +
					model.Value(m.GetFishPurchasedForResale()) +
					model.Value(m.GetNonqualifiedDeferredCompensation())
				x = figures{gross: gross, federal: model.Value(m.GetFederalTaxWithheld())}
				for _, s := range m.GetStateTaxWithholding() {
					x.state += model.Value(s.GetTaxWithheld())
				}
				if m.GetSection_409ADeferrals() != nil || m.GetNonqualifiedDeferredCompensation() != nil {
					e.notes.LeaveOut("Any excise tax on excess golden parachute payments or § 409A penalties on Form 1099-MISC")
				}
			case f.GetForm_1099Sa() != nil:
				s := f.GetForm_1099Sa()
				gross := model.Value(s.GetGrossDistribution())
				x = figures{gross: gross, taxable: gross, taxableSet: true}
				e.notes.Ask(model.NewQuestion(model.ItemID(e.prefix(), f.GetId(), "hsa_expenses"), e.prefix(),
					"Were the HSA/MSA distributions used for qualified medical expenses?",
					"Distributions not used for qualified medical expenses are taxable and may be subject to an additional 20% tax.",
					e.path()))
				e.notes.LeaveOut("The 20% additional tax on non-qualified HSA distributions (Form 8889 Part II)")
			case f.GetForm_1099S() != nil:
				s := f.GetForm_1099S()
				gross := model.Value(s.GetGrossProceeds())
				x = figures{gross: gross, taxable: gross, taxableSet: true}
				if c, err := civil.FromProto(s.GetDateOfClosing()); err == nil && !c.IsZero() {
					d = c
				}
				e.notes.Ask(model.NewQuestion(model.ItemID(e.prefix(), f.GetId(), "real_estate_basis"), e.prefix(),
					"What was the cost basis and § 121 exclusion for the real estate sale?",
					"Gross proceeds from Form 1099-S are counted as income until basis and exclusions are known.",
					e.path()))
				e.notes.LeaveOut("Cost basis and § 121 principal residence exclusion for Form 1099-S real estate proceeds")
			case f.GetForm_1099C() != nil:
				c := f.GetForm_1099C()
				gross := model.Value(c.GetAmountOfDebtDischarged())
				x = figures{gross: gross, taxable: gross, taxableSet: true}
				if ed, err := civil.FromProto(c.GetDateOfIdentifiableEvent()); err == nil && !ed.IsZero() {
					d = ed
				}
				e.notes.Ask(model.NewQuestion(model.ItemID(e.prefix(), f.GetId(), "debt_exclusion"), e.prefix(),
					"Does any insolvency, bankruptcy, or qualified principal residence indebtedness exclusion apply to the canceled debt?",
					"Canceled debt on Form 1099-C is treated as taxable income unless an exclusion (Form 982) applies.",
					e.path()))
				e.notes.LeaveOut("Form 982 insolvency or bankruptcy exclusions for canceled debt on Form 1099-C")
			case f.GetForm_1099Q() != nil:
				q := f.GetForm_1099Q()
				gross := model.Value(q.GetGrossDistribution())
				taxable := model.Value(q.GetEarnings())
				if taxable == 0 && gross > 0 {
					taxable = gross
				}
				x = figures{gross: gross, taxable: taxable, taxableSet: true}
				e.notes.Ask(model.NewQuestion(model.ItemID(e.prefix(), f.GetId(), "education_expenses"), e.prefix(),
					"Were the 1099-Q education distributions used for qualified higher education expenses?",
					"Earnings are taxable and may be subject to a 10% additional tax if not used for qualified expenses.",
					e.path()))
				e.notes.LeaveOut("The 10% additional tax and qualified education expense exclusion for Form 1099-Q distributions")
			case f.GetScheduleK1_1065() != nil:
				k := f.GetScheduleK1_1065()
				gross := model.Value(k.GetOrdinaryBusinessIncome()) +
					model.Value(k.GetNetRentalRealEstateIncome()) +
					model.Value(k.GetOtherNetRentalIncome()) +
					model.Value(k.GetTotalGuaranteedPayments()) +
					model.Value(k.GetRoyalties())
				x = figures{gross: gross, taxable: gross, taxableSet: true}
				e.notes.LeaveOut("Partner basis limits, at-risk limits, and passive activity loss limitations for Schedule K-1 (Form 1065)")
			case f.GetScheduleK1_1120S() != nil:
				k := f.GetScheduleK1_1120S()
				gross := model.Value(k.GetOrdinaryBusinessIncome()) +
					model.Value(k.GetNetRentalRealEstateIncome()) +
					model.Value(k.GetOtherNetRentalIncome()) +
					model.Value(k.GetRoyalties())
				x = figures{gross: gross, taxable: gross, taxableSet: true}
				e.notes.LeaveOut("Shareholder basis limits, at-risk limits, and passive activity loss limitations for Schedule K-1 (Form 1120-S)")
			case f.GetScheduleK1_1041() != nil:
				k := f.GetScheduleK1_1041()
				gross := model.Value(k.GetOrdinaryBusinessIncome()) +
					model.Value(k.GetNetRentalRealEstateIncome()) +
					model.Value(k.GetOtherRentalIncome()) +
					model.Value(k.GetOtherPortfolioAndNonbusinessIncome())
				x = figures{gross: gross, taxable: gross, taxableSet: true}
				e.notes.LeaveOut("Beneficiary basis limits and passive activity loss limitations for Schedule K-1 (Form 1041)")
			}
			e.forms = append(e.forms, f.GetId())
			e.add(model.ItemID(e.prefix(), d.String(), "payment", f.GetId()), d, made, scenariosv1.Origin_BASIS_DOCUMENT, p.GetId(), []string{f.GetId()}, x)
		}
		for _, c := range p.GetFromLedger().GetCredits() {
			d, _ := civil.FromProto(c.GetDate())
			e.add(model.ItemID(e.prefix(), d.String(), "payment", c.GetId()), d, made, scenariosv1.Origin_BASIS_LEDGER, p.GetId(), []string{c.GetId()},
				figures{gross: model.Value(c.GetAmount())})
		}
	}
}

func (e *expansion) evidenceTotal() figures {
	var t figures
	for _, it := range e.items {
		if it.GetStatus() == scenariosv1.ItemStatus_ITEM_STATUS_MADE {
			t = t.plus(read(it.GetOtherIncome()))
		}
	}
	return t
}

func read(f *scenariosv1.OtherIncomeFigures) figures {
	x := figures{gross: model.Value(f.GetGross()), federal: model.Value(f.GetFederalWithheld()), state: model.Value(f.GetStateWithheld()),
		expenses: model.Value(f.GetExpenses()), codes: f.GetDistributionCodes(), ira: f.GetIra()}
	if f.GetTaxable() != nil {
		x.taxable, x.taxableSet = model.Value(f.GetTaxable()), true
	}
	return x
}

// assumed adds the YearAmount and one-offs, unless a whole-year form stands
// them aside.
func (e *expansion) assumed() error {
	expected := scenariosv1.ItemStatus_ITEM_STATUS_EXPECTED
	for _, p := range e.m.GetParts() {
		switch {
		case p.GetYearAmount() != nil:
			if len(e.forms) > 0 {
				e.standAside(p.GetId(), "A form states this source's year, so the total for the year counts for nothing.")
				continue
			}
			e.yearAmount(p)
		case p.GetOneOff() != nil:
			o := p.GetOneOff()
			if o.GetActual().GetIs() != "" {
				continue
			}
			if e.formNotListed(o.GetActual()) {
				e.standAside(p.GetId(), "A form states this source's year. Is your planned payment on it?")
				continue
			}
			d, err := civil.FromProto(o.GetDate())
			if err != nil || d.IsZero() {
				return fmt.Errorf("%s.parts[%s].one_off.date is not a day", e.path(), p.GetId())
			}
			var x figures
			if dist := o.GetDistribution(); dist != nil {
				x = figures{gross: model.Value(dist.GetGross()), federal: model.Value(dist.GetFederalWithheld())}
				if dist.GetTaxable() != nil {
					x.taxable, x.taxableSet = model.Value(dist.GetTaxable()), true
				}
				if dist.GetCode() != "" {
					x.codes = []string{dist.GetCode()}
				}
			}
			if hs := o.GetHomeSale(); hs != nil {
				x = figures{gross: model.Value(hs.GetPrice()), expenses: model.Value(hs.GetBasis())}
			}
			e.add(model.ItemID(e.prefix(), d.String(), "payment", p.GetId()), d, expected, scenariosv1.Origin_BASIS_ASSUMED, p.GetId(), nil, x)
		}
	}
	return nil
}

// formNotListed reports whether a form states the year and the answer
// doesn't list every form as a different payment.
func (e *expansion) formNotListed(a *scenariosv1.Actual) bool {
	not := map[string]bool{}
	for _, id := range a.GetIsNot() {
		not[id] = true
	}
	for _, f := range e.forms {
		if !not[f] {
			return true
		}
	}
	return false
}

func (e *expansion) standAside(part, why string) {
	e.notes.Ask(model.NewQuestion(model.ItemID(e.prefix(), "stands-aside", part), e.prefix(), why,
		"This change counts for nothing until it is changed or removed.",
		fmt.Sprintf("%s.parts[%s]", e.path(), part), "Remove it"))
}

// yearAmount adds the year's figures less the evidence.
func (e *expansion) yearAmount(p *scenariosv1.OtherIncomePart) {
	y := p.GetYearAmount()
	have := e.evidenceTotal()
	want := figures{gross: model.Value(y.GetGross()), federal: model.Value(y.GetFederalWithheld()), expenses: model.Value(y.GetExpenses())}
	if r := y.GetRentalExpenses(); r != nil {
		want.expenses = rentalExpenses(r)
	}
	if y.GetTaxable() != nil {
		want.taxable, want.taxableSet = model.Value(y.GetTaxable()), true
	}
	if have.gross > want.gross {
		e.standAside(p.GetId(), fmt.Sprintf("The evidence so far, %v, is more than your total for the year, %v.", have.gross, want.gross))
		return
	}
	left := figures{gross: want.gross - have.gross, federal: max(0, want.federal-have.federal), expenses: max(0, want.expenses-have.expenses)}
	if want.taxableSet {
		left.taxable, left.taxableSet = max(0, want.taxable-have.taxableOrGross()), true
	}
	dates := []civil.Date{e.ctx.YearEnd()}
	if y.GetRecurring() {
		dates = nil
		start := civil.Later(e.through, e.ctx.EvidenceThrough())
		for m := time.January; m <= time.December; m++ {
			if mo := civil.EndOfMonth(e.ctx.Year(), m); mo.After(start) {
				dates = append(dates, mo)
			}
		}
		if len(dates) == 0 {
			return
		}
	}
	gross, taxable := model.Share(left.gross, len(dates)), model.Share(left.taxable, len(dates))
	fed, exp := model.Share(left.federal, len(dates)), model.Share(left.expenses, len(dates))
	for i, d := range dates {
		x := figures{gross: gross[i], federal: fed[i], expenses: exp[i]}
		if left.taxableSet {
			x.taxable, x.taxableSet = taxable[i], true
		}
		id := model.ItemID(e.prefix(), d.String(), "payment", p.GetId())
		e.add(id, d, scenariosv1.ItemStatus_ITEM_STATUS_EXPECTED, scenariosv1.Origin_BASIS_ASSUMED, p.GetId(), nil, x)
	}
}

// rentalExpenses adds a rental's Schedule E lines 5 to 19, depreciation
// included.
func rentalExpenses(r *scenariosv1.RentalExpenses) money.USCents {
	var t money.USCents
	m := r.ProtoReflect()
	fields := m.Descriptor().Fields()
	for i := 0; i < fields.Len(); i++ {
		if v, ok := m.Get(fields.Get(i)).Message().Interface().(*commonv1.Money); ok && m.Has(fields.Get(i)) {
			t += model.Value(v)
		}
	}
	return t
}

func (e *expansion) projection() *scenariosv1.OtherIncomeProjection {
	model.SortItems(e.items)
	model.RoundItems(e.items)
	out := &scenariosv1.OtherIncomeProjection{Id: e.m.GetId(), Items: e.items, SoFarThrough: e.through.Proto()}
	var made, rest figures
	for _, it := range e.items {
		if it.GetStatus() == scenariosv1.ItemStatus_ITEM_STATUS_MADE {
			made = made.plus(read(it.GetOtherIncome()))
		} else {
			rest = rest.plus(read(it.GetOtherIncome()))
		}
	}
	year := made.plus(rest)
	out.SoFar = &scenariosv1.OtherIncomeTotals{Items: made.proto(), Boxes: made.proto()}
	out.Rest = &scenariosv1.OtherIncomeTotals{Items: rest.proto(), Boxes: rest.proto()}
	out.Year = &scenariosv1.OtherIncomeTotals{Items: year.proto(), Boxes: year.proto()}
	if len(e.forms) == 0 && len(e.items) > 0 {
		if form := formFor(e.m.GetKind()); form != "" {
			out.Expected = []*scenariosv1.ExpectedDocument{{Form: form, Due: model.FormsDue(e.ctx.Year()).Proto()}}
		}
	}
	return out
}

func formFor(k scenariosv1.OtherIncomeKind) string {
	switch k {
	case scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_RETIREMENT_WITHDRAWALS:
		return "Form1099R"
	case scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_SOCIAL_SECURITY:
		return "FormSsa1099"
	case scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_UNEMPLOYMENT, scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_STATE_REFUND:
		return "Form1099G"
	case scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_GAMBLING:
		return "FormW2G"
	}
	return ""
}
