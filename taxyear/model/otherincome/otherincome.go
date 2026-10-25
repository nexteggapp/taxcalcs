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
				// Form 1099-R (26 U.S.C. §§ 72, 402, 408; Form 1040 lines 4a–5b):
				// Box 1 (gross_distribution) is the total distribution. When box 2a
				// (taxable_amount) is populated and box 2b (taxable_amount_not_determined)
				// is not checked, box 2a states the taxable portion; otherwise the full
				// gross distribution is treated as taxable. Box 4 (federal_tax_withheld)
				// flows to Form 1040 line 25b, and box 14 (state_tax_withholding) sums
				// state tax withheld across states.
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
				// Form SSA-1099 (26 U.S.C. § 86; Form 1040 lines 6a–6b):
				// Box 5 (net_benefits = box 3 benefits paid less box 4 benefits repaid)
				// is the net Social Security benefit for the year; box 6 is voluntary
				// federal income tax withheld (Form 1040 line 25b).
				s := f.GetFormSsa_1099()
				x = figures{gross: model.Value(s.GetNetBenefits()), federal: model.Value(s.GetFederalTaxWithheld())}
			case f.GetForm_1099G() != nil:
				// Form 1099-G (26 U.S.C. §§ 85, 111; Schedule 1 lines 1 and 7):
				// Box 1 (unemployment_compensation) is taxable under § 85 (Schedule 1
				// line 7); box 2 (state_or_local_refund) is evaluated under the § 111
				// tax benefit rule (Schedule 1 line 1). Box 4 is federal tax withheld.
				g := f.GetForm_1099G()
				x = figures{gross: model.Value(g.GetUnemploymentCompensation()) + model.Value(g.GetStateOrLocalRefund()), federal: model.Value(g.GetFederalTaxWithheld())}
			case f.GetFormW2G() != nil:
				// Form W-2G (26 U.S.C. § 61; Schedule 1 line 8b):
				// Box 1 (reportable_winnings) is gross gambling winnings; box 4 is
				// federal income tax withheld (Form 1040 line 25b).
				w := f.GetFormW2G()
				x = figures{gross: model.Value(w.GetReportableWinnings()), federal: model.Value(w.GetFederalTaxWithheld())}
				if dw, err := civil.FromProto(w.GetDateWon()); err == nil && !dw.IsZero() {
					d = dw
				}
			case f.GetForm_1099Misc() != nil:
				// Form 1099-MISC (26 U.S.C. § 61; Schedule 1 lines 5 and 8z):
				// Sum the taxable income boxes reported on Form 1099-MISC: box 1 (rents),
				// box 2 (royalties), box 3 (other_income), box 5 (fishing_boat_proceeds),
				// box 6 (medical_and_health_care_payments), box 9 (crop_insurance_proceeds),
				// box 10 (gross_proceeds_paid_to_attorney), box 11 (fish_purchased_for_resale),
				// and box 15 (nonqualified_deferred_compensation, includible under § 409A(a)(1)).
				// Box 14 (excess_golden_parachute_payments) is already included in box 3 per
				// the Instructions for Form 1099-MISC, so it is not added again.
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
				// Form 1099-SA and Form 8889 Part II (26 U.S.C. § 223(f); Schedule 1 line 8f):
				// Form 8889 line 14a is total distributions (Form 1099-SA box 1, gross_distribution).
				// Subtracting qualified rollovers (Form 8889 line 14b, sup.GetRollover()) yields
				// net distributions on Form 8889 line 14c. Subtracting unreimbursed qualified
				// medical expenses (Form 8889 line 15, sup.GetQualifiedExpenses()), floored at zero,
				// gives the taxable HSA distribution on Form 8889 line 16 (§ 223(f)(1)–(2)).
				s := f.GetForm_1099Sa()
				sup := f.GetForm_1099SaSupplement()
				gross := model.Value(s.GetGrossDistribution())
				net := max(0, gross-model.Value(sup.GetRollover()))
				taxable := max(0, net-model.Value(sup.GetQualifiedExpenses()))
				x = figures{gross: gross, taxable: taxable, taxableSet: true}
				if taxable > 0 {
					e.notes.LeaveOut("The 20% additional tax on non-qualified HSA distributions (Form 8889 Part II)")
				}
			case f.GetForm_1099S() != nil:
				// Form 1099-S (26 U.S.C. §§ 1001, 1011, 121; Publication 523; Form 8949 / Schedule D):
				// Amount realized is Form 1099-S box 2 (gross_proceeds) less selling expenses
				// (sup.GetSellingExpenses()), and realized gain is amount realized less adjusted
				// cost basis (sup.GetBasis()), equivalent to gross - (basis + selling_expenses).
				// Gain is floored at zero because a loss on the sale of a personal residence is
				// nondeductible under 26 U.S.C. § 165(c). Subtracting the § 121 principal
				// residence exclusion (sup.GetExcludedAmount(), up to $250,000 / $500,000 joint),
				// floored at zero, gives the taxable gain.
				s := f.GetForm_1099S()
				sup := f.GetForm_1099SSupplement()
				gross := model.Value(s.GetGrossProceeds())
				expenses := model.Value(sup.GetBasis()) + model.Value(sup.GetSellingExpenses())
				gain := max(0, gross-expenses)
				taxable := max(0, gain-model.Value(sup.GetExcludedAmount()))
				x = figures{gross: gross, taxable: taxable, expenses: expenses, taxableSet: true}
				if c, err := civil.FromProto(s.GetDateOfClosing()); err == nil && !c.IsZero() {
					d = c
				}
			case f.GetForm_1099C() != nil:
				// Form 1099-C and Form 982 (26 U.S.C. §§ 61(a)(11), 108(a); Schedule 1 line 8c):
				// Discharged debt reported in Form 1099-C box 2 (amount_of_debt_discharged) is
				// ordinary cancellation-of-debt income under § 61(a)(11), reduced (not below zero)
				// by any amount excluded under § 108 on Form 982 (sup.GetExcludedAmount(), such as
				// bankruptcy, insolvency up to the extent insolvent under § 108(a)(3), or
				// qualified principal residence indebtedness).
				c := f.GetForm_1099C()
				sup := f.GetForm_1099CSupplement()
				gross := model.Value(c.GetAmountOfDebtDischarged())
				taxable := max(0, gross-model.Value(sup.GetExcludedAmount()))
				x = figures{gross: gross, taxable: taxable, taxableSet: true}
				if ed, err := civil.FromProto(c.GetDateOfIdentifiableEvent()); err == nil && !ed.IsZero() {
					d = ed
				}
			case f.GetForm_1099Q() != nil:
				// Form 1099-Q (26 U.S.C. §§ 529(c)(3), 530(d)(2), 72; Publication 970; Schedule 1 line 8z):
				// Under § 529(c)(3)(B), if adjusted qualified education expenses (sup.GetQualifiedExpenses())
				// equal or exceed the net distribution (box 1 gross_distribution less sup.GetRollover()),
				// no part of the distribution is taxable. Otherwise, the earnings portion (box 2,
				// q.GetEarnings()) is prorated by the ratio of non-qualified distributions to the
				// net distribution: taxable = earnings * (net - qual) / net. If neither box 2 (earnings)
				// nor box 3 (basis) is populated, the entire distribution is conservatively treated as earnings.
				q := f.GetForm_1099Q()
				sup := f.GetForm_1099QSupplement()
				gross := model.Value(q.GetGrossDistribution())
				earnings := model.Value(q.GetEarnings())
				if earnings == 0 && gross > 0 && q.GetBasis() == nil {
					earnings = gross
				}
				net := max(0, gross-model.Value(sup.GetRollover()))
				qual := model.Value(sup.GetQualifiedExpenses())
				var taxable money.USCents
				if net > 0 && qual < net {
					taxable = model.Scale(earnings, net-qual, net)
				}
				x = figures{gross: gross, taxable: taxable, taxableSet: true}
				if taxable > 0 {
					e.notes.LeaveOut("The 10% additional tax on non-qualified education distributions (Form 1099-Q)")
				}
			case f.GetScheduleK1_1065() != nil:
				// Schedule K-1 (Form 1065) (26 U.S.C. §§ 702, 707(c); Schedule E Part I & Part II; Schedule 1 line 5):
				// Sum the partner's distributive share of ordinary pass-through income items:
				// box 1 (ordinary_business_income), box 2 (net_rental_real_estate_income),
				// box 3 (other_net_rental_income), box 4c (total_guaranteed_payments), and
				// box 7 (royalties).
				k := f.GetScheduleK1_1065()
				gross := model.Value(k.GetOrdinaryBusinessIncome()) +
					model.Value(k.GetNetRentalRealEstateIncome()) +
					model.Value(k.GetOtherNetRentalIncome()) +
					model.Value(k.GetTotalGuaranteedPayments()) +
					model.Value(k.GetRoyalties())
				x = figures{gross: gross, taxable: gross, taxableSet: true}
				e.notes.LeaveOut("Partner basis limits, at-risk limits, and passive activity loss limitations for Schedule K-1 (Form 1065)")
			case f.GetScheduleK1_1120S() != nil:
				// Schedule K-1 (Form 1120-S) (26 U.S.C. § 1366; Schedule E Part I & Part II; Schedule 1 line 5):
				// Sum the S corporation shareholder's pro rata share of ordinary pass-through
				// income items: box 1 (ordinary_business_income), box 2 (net_rental_real_estate_income),
				// box 3 (other_net_rental_income), and box 6 (royalties).
				k := f.GetScheduleK1_1120S()
				gross := model.Value(k.GetOrdinaryBusinessIncome()) +
					model.Value(k.GetNetRentalRealEstateIncome()) +
					model.Value(k.GetOtherNetRentalIncome()) +
					model.Value(k.GetRoyalties())
				x = figures{gross: gross, taxable: gross, taxableSet: true}
				e.notes.LeaveOut("Shareholder basis limits, at-risk limits, and passive activity loss limitations for Schedule K-1 (Form 1120-S)")
			case f.GetScheduleK1_1041() != nil:
				// Schedule K-1 (Form 1041) (26 U.S.C. §§ 652, 662; Schedule E Part III; Schedule 1 line 5):
				// Sum the beneficiary's share of ordinary estate or trust income items:
				// box 5 (ordinary_business_income), box 6 (net_rental_real_estate_income),
				// box 7 (other_rental_income), and box 8 (other_portfolio_and_nonbusiness_income).
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
				// Planned home sale (26 U.S.C. §§ 1001, 1011, 121; Publication 523):
				// Total cost combines adjusted basis and selling expenses; taxable gain is
				// realized gain (gross - expenses, floored at zero per § 165(c)) less any
				// § 121 exclusion, floored at zero.
				gross := model.Value(hs.GetPrice())
				expenses := model.Value(hs.GetBasis()) + model.Value(hs.GetSellingExpenses())
				gain := max(0, gross-expenses)
				taxable := max(0, gain-model.Value(hs.GetExcludedAmount()))
				x = figures{gross: gross, taxable: taxable, expenses: expenses, taxableSet: true}
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
