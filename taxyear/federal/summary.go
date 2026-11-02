package federal

import (
	"fmt"
	"strings"

	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	"github.com/nexteggapp/taxcalcs/taxyear/model/retirement"
	"github.com/nexteggapp/taxcalcs/taxyear/payroll"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
	form1040 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1040"
)

// summary reads the totals and limits no form line holds from the engine's
// outputs and inputs: the standard deduction, state and local tax by where
// it was paid, the itemised floors, the capital loss carried, each person's
// retirement limits and each other income model's part of the return.
type summary struct {
	ctx  *model.Context
	proj *scenariosv1.YearProjection
	in   *scenariosv1.ScenarioInputs
	r    *Return
	out  *scenariosv1.ScenarioSummary
}

func summarise(ctx *model.Context, proj *scenariosv1.YearProjection, in *scenariosv1.ScenarioInputs, r *Return) *scenariosv1.ScenarioSummary {
	s := &summary{ctx: ctx, proj: proj, in: in, r: r, out: &scenariosv1.ScenarioSummary{}}
	fed := r.out.GetFederal()
	s.out.StandardDeduction = fed.GetWorkpapers().GetStandardDeduction().GetEffectiveStandardDeduction()
	f, sd := r.form.GetForm_1040(), r.form.GetScheduleD()
	if l := model.Value(f.GetCapitalGainOrLoss()) - model.Value(sd.GetNetGainOrLoss()); l > 0 {
		s.out.CapitalLossCarried = model.Money(-l)
	}
	s.salt()
	s.itemised()
	s.retirement()
	s.otherIncome()
	return s.out
}

// salt is Schedule A line 5a by where it was paid, from the inputs, and
// the § 164(b)(7) limit where the engine held the taxes to it.
func (s *summary) salt() {
	var withheld, estimates money.USCents
	for _, w := range s.r.inputs.GetFederal().GetDocuments().GetW2S() {
		for _, st := range w.GetForm().GetStateTaxWithholding() {
			withheld += model.Value(st.GetTaxWithheld())
		}
	}
	for _, p := range s.r.inputs.GetFederal().GetPayments().GetEstimatedPayments() {
		if p.GetJurisdiction() != "US" {
			estimates += model.Value(p.GetAmount())
		}
	}
	balance := s.r.inputs.GetFederal().GetDeductions().GetStateAndLocalTaxes().GetBalancePaidForPriorYears()
	out := &scenariosv1.SaltSummary{Withholding: model.Money(withheld), Estimates: model.Money(estimates), LastYearBalance: money2(balance)}
	sa := s.r.form.GetScheduleA()
	if after := model.Value(sa.GetStateAndLocalTaxesAfterCap()); after < model.Value(sa.GetTotalStateAndLocalTaxes()) {
		out.Cap = model.Money(after)
	}
	s.out.Salt = out
}

// itemised records the medical and charity floors under the first model of
// each kind: what Schedule A doesn't count of each.
func (s *summary) itemised() {
	var medicalID, charityID string
	for _, m := range s.in.GetModel().GetItemised() {
		switch {
		case m.GetMedical() != nil && medicalID == "":
			medicalID = m.GetId()
		case m.GetCharity() != nil && charityID == "":
			charityID = m.GetId()
		}
	}
	sa := s.r.form.GetScheduleA()
	if medicalID != "" {
		counted := model.Value(sa.GetDeductibleMedicalAndDental())
		s.out.Itemised = append(s.out.Itemised, &scenariosv1.ItemisedSummary{Id: medicalID,
			Floor: model.Money(model.Value(sa.GetMedicalAndDentalExpenses()) - counted), Counted: model.Money(counted)})
	}
	if charityID != "" {
		gifts := model.Value(sa.GetGiftsByCashOrCheck()) + model.Value(sa.GetGiftsOtherThanCashOrCheck()) + model.Value(sa.GetCharitableCarryoverFromPriorYear())
		counted := model.Value(sa.GetTotalGiftsToCharity())
		floor := gifts - counted
		s.out.Itemised = append(s.out.Itemised, &scenariosv1.ItemisedSummary{Id: charityID, Floor: model.Money(floor), Counted: model.Money(counted)})
		s.out.ItemisedLimitation = model.Money(floor)
		if floor > 0 {
			s.charityWorking("summary.itemised["+charityID+"].floor", floor, gifts)
		}
	}
	s.out.ItemisedBracketLimit = model.Money(0)
}

// charityWorking explains what Schedule A line 14 doesn't count of the
// gifts, and adds line 14 up from the gifts less it.
func (s *summary) charityWorking(at string, floor, gifts money.USCents) {
	cite := "26 U.S.C. § 170(b)(1)(I), added by Pub. L. 119-21, sec. 70425; 26 U.S.C. § 170(b)(1)(G)"
	s.r.work = append(s.r.work, &scenariosv1.Working{Path: at, Rules: []*scenariosv1.AppliedRule{{Cite: cite,
		Applied: &scenariosv1.Statement{Text: fmt.Sprintf("Of %v of gifts to charity, %v doesn't count: from 2026 gifts count only above 0.5%% of adjusted gross income, and up to 60%% of it", gifts, floor),
			States: []string{at, "federal.schedule_a.total_gifts_to_charity"}}}}})
	line := "federal.schedule_a.total_gifts_to_charity"
	w := &scenariosv1.Working{Path: line}
	for _, p := range []string{"gifts_by_cash_or_check", "gifts_other_than_cash_or_check", "charitable_carryover_from_prior_year"} {
		w.Terms = append(w.Terms, &scenariosv1.Term{Of: &scenariosv1.Term_Path{Path: "federal.schedule_a." + p}, Sign: 1})
	}
	w.Terms = append(w.Terms, &scenariosv1.Term{Of: &scenariosv1.Term_Path{Path: at}, Sign: -1})
	for _, x := range s.r.work {
		if x.GetPath() == line {
			x.Terms = w.Terms
			return
		}
	}
	s.r.work = append(s.r.work, w)
}

// retirement records each person's IRA year and limits. A limit that needs
// an answer asks for it: the date of birth, for the plan limit's catch-up;
// the HSA coverage, for someone with HSA contributions.
func (s *summary) retirement() {
	var ids []string
	seen := map[string]bool{}
	hh := s.r.inputs.GetFederal().GetHousehold()
	add := func(id string) {
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	add(hh.GetTaxpayer().GetId())
	if s.r.status == filing.MarriedFilingJointly {
		add(hh.GetSpouse().GetId())
	}
	for _, m := range s.in.GetModel().GetRetirement() {
		add(m.GetPerson())
	}
	ira := s.r.out.GetFederal().GetWorkpapers().GetIraDeduction()
	for _, id := range ids {
		out := &scenariosv1.RetirementSummary{Person: id}
		for _, p := range s.r.inputs.GetFederal().GetRetirement().GetPersons() {
			if p.GetPerson() == id {
				out.IraUsed = model.Money(model.Value(p.GetTraditionalIraContributions()) + model.Value(p.GetRothIraContributions()))
			}
		}
		deducted := model.Value(ira.GetTaxpayerDeduction())
		if id != hh.GetTaxpayer().GetId() {
			deducted = model.Value(ira.GetSpouseDeduction())
		}
		for i, h := range s.r.inputs.GetFederal().GetHealth().GetHsaAccounts() {
			if f := s.r.out.GetFederal().GetForm_8889S(); h.GetPerson() == id && i < len(f) {
				deducted += model.Value(f[i].GetHsaDeduction())
			}
		}
		out.Deducted = model.Money(deducted)
		if f := s.form5329(id); f != nil {
			out.RothExcess = model.Money(model.Value(f.GetCurrentYearRothIraExcessContributions()))
			out.IraExcise = model.Money(model.Value(f.GetAdditionalTaxOnTraditionalIraExcess()) + model.Value(f.GetAdditionalTaxOnRothIraExcess()))
		}
		s.limits(id, out)
		s.out.Retirement = append(s.out.Retirement, out)
	}
}

// form5329 is a person's Form 5329, matched by the name the engine prints
// on it, or for a return with one person, the only one.
func (s *summary) form5329(id string) *form1040.Form5329 {
	forms := s.r.form.GetForm_5329()
	hh := s.r.inputs.GetFederal().GetHousehold()
	for _, p := range []*returnsv1.Person{hh.GetTaxpayer(), hh.GetSpouse()} {
		if p.GetId() != id || p.GetFirstName() == "" {
			continue
		}
		for _, f := range forms {
			if strings.TrimSpace(f.GetIndividualName()) == strings.TrimSpace(p.GetFirstName()) {
				return f
			}
		}
	}
	if len(forms) == 1 && (hh.GetSpouse() == nil || hh.GetSpouse().GetId() == "") && id == hh.GetTaxpayer().GetId() {
		return forms[0]
	}
	return nil
}

// limits fills a person's workplace plan and HSA limits, which are the
// projection's, and asks what they need.
func (s *summary) limits(id string, out *scenariosv1.RetirementSummary) {
	at := "summary.retirement[" + id + "]"
	age, aged := s.ctx.AgeAtYearEnd(id)
	if base, catchUp, err := payroll.PlanLimit(s.ctx.Year(), age); err == nil {
		out.PlanLimit = model.Money(base + catchUp)
		if !aged {
			s.r.notes.Ask(model.NewQuestion("retirement/"+id+"/date-of-birth", at+".plan_limit",
				fmt.Sprintf("What is %s date of birth? The workplace plan limit is higher from 50.", s.possessive(id)),
				"No catch-up is counted until you say.", "facts.people["+id+"].date_of_birth"))
		}
	}
	var employer, contributed money.USCents
	for _, e := range s.in.GetModel().GetEmployers() {
		if e.GetPerson() == id {
			employer += model.Value(e.GetEmployerHsa())
		}
	}
	for _, w := range s.r.inputs.GetFederal().GetDocuments().GetW2S() {
		if w.GetPerson() == id {
			for _, c := range w.GetForm().GetCodedAmounts() {
				if c.GetCode() == "W" {
					contributed += model.Value(c.GetAmount())
				}
			}
		}
	}
	for _, h := range s.r.inputs.GetFederal().GetHealth().GetHsaAccounts() {
		if h.GetPerson() == id {
			contributed += model.Value(h.GetDirectContributions())
		}
	}
	out.HsaEmployer = model.Money(employer)
	limit, ok := retirement.HsaLimit(s.ctx, id, &s.r.notes)
	switch {
	case ok:
		out.HsaLimit = model.Money(limit)
	case employer+contributed > 0 && s.ctx.Person(id).GetHsaCoverage() == factsv1.HsaCoverage_HSA_COVERAGE_UNSPECIFIED:
		s.r.notes.Ask(model.NewQuestion("retirement/"+id+"/hsa-coverage", at+".hsa_limit",
			fmt.Sprintf("What health plan coverage did %s HSA go with?", s.possessive(id)),
			"The HSA limit isn't counted until you say.",
			"facts.people["+id+"].hsa_coverage", "Self-only", "Family", "No HSA-eligible plan"))
	}
}

// possessive is "your" for you, the person's name with "'s", or for a
// spouse without one "your spouse's".
func (s *summary) possessive(id string) string {
	p := s.ctx.Person(id)
	switch {
	case p.GetRelationship() == factsv1.Relationship_RELATIONSHIP_YOU || p.GetName() == "" && id == youID:
		return "your"
	case p.GetName() != "":
		return p.GetName() + "'s"
	case p.GetRelationship() == factsv1.Relationship_RELATIONSHIP_SPOUSE || id == spouseID:
		return "your spouse's"
	}
	return id + "'s"
}

// otherIncome records each other income model's part of the return. A line
// the engine works out over several models, such as the taxable part of
// Social Security benefits or a state refund, is shared by each model's
// part of what went into it.
func (s *summary) otherIncome() {
	f, sch1 := s.r.form.GetForm_1040(), s.r.form.GetSchedule_1()
	docs := s.r.inputs.GetFederal().GetDocuments()
	var ssAll, refundAll, earlyAll money.USCents
	for _, d := range docs.GetSocialSecurity() {
		ssAll += model.Value(d.GetForm().GetNetBenefits())
	}
	for _, d := range docs.GetGovernmentPayments() {
		refundAll += model.Value(d.GetForm().GetStateOrLocalRefund())
	}
	for _, d := range docs.GetRetirementDistributions() {
		earlyAll += early(d)
	}
	var earlyTax money.USCents
	for _, x := range s.r.form.GetForm_5329() {
		earlyTax += model.Value(x.GetAdditionalTaxOnEarlyDistributions())
	}
	byID := map[string]*scenariosv1.OtherIncomeModel{}
	for _, m := range s.in.GetModel().GetOtherIncome() {
		byID[m.GetId()] = m
	}
	for _, o := range s.proj.GetOtherIncome() {
		id := "other_income/" + o.GetId()
		out := &scenariosv1.OtherIncomeSummary{Id: o.GetId()}
		var withheld, taxable, earlyPart money.USCents
		known := true
		for _, it := range o.GetItems() {
			withheld += model.Value(it.GetOtherIncome().GetFederalWithheld())
		}
		switch byID[o.GetId()].GetKind() {
		case scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_RETIREMENT_WITHDRAWALS:
			for _, d := range docs.GetRetirementDistributions() {
				if strings.HasPrefix(d.GetId(), id+"/") {
					taxable += model.Value(d.GetForm().GetTaxableAmount())
					earlyPart += early(d)
					// A distribution without a code is left out.
					known = known && len(d.GetForm().GetDistributionCodes()) > 0
				}
			}
		case scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_SOCIAL_SECURITY:
			for _, d := range docs.GetSocialSecurity() {
				if d.GetId() == id {
					net := model.Value(d.GetForm().GetNetBenefits())
					out.NetBenefits = model.Money(net)
					taxable = model.Scale(model.Value(f.GetSocialSecurityBenefitsTaxable()), net, ssAll)
				}
			}
		case scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_STATE_REFUND:
			for _, d := range docs.GetGovernmentPayments() {
				if d.GetId() == id {
					taxable = model.Scale(model.Value(sch1.GetTaxableStateLocalRefunds()), model.Value(d.GetForm().GetStateOrLocalRefund()), refundAll)
				}
			}
		case scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_RENTAL:
			for _, p := range s.r.inputs.GetFederal().GetRentals() {
				if p.GetId() == id {
					taxable = model.Value(p.GetRentsReceived()) - model.Value(p.GetOtherExpenses())
				}
			}
			if taxable < 0 {
				// The loss allowed is Schedule E's, shared by the losses.
				known = len(s.r.inputs.GetFederal().GetRentals()) == 1
				allowed := model.Value(sch1.GetRentalRoyaltyPartnershipSCorpTrust())
				if known {
					out.LossCarried = model.Money(max(0, -taxable+min(0, allowed)))
					taxable = min(0, allowed)
				}
			}
		case scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_ALIMONY, scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_HOME_SALE:
			known = false
		default:
			for _, it := range o.GetItems() {
				x := it.GetOtherIncome()
				if x.GetTaxable() != nil {
					taxable += model.Value(x.GetTaxable())
				} else {
					taxable += model.Value(x.GetGross())
				}
			}
		}
		out.Withheld = model.Money(withheld)
		if known {
			out.Taxable = model.Money(taxable)
		}
		if earlyPart > 0 {
			out.EarlyDistributionTax = model.Money(model.Scale(earlyTax, earlyPart, earlyAll))
		}
		s.out.OtherIncome = append(s.out.OtherIncome, out)
	}
}

// early is a 1099-R's taxable amount coded 1, an early distribution with
// no known exception, which Form 5329 line 1 counts.
func early(d *returnsv1.Form1099RDocument) money.USCents {
	for _, c := range d.GetForm().GetDistributionCodes() {
		if c == "1" {
			return model.Value(d.GetForm().GetTaxableAmount())
		}
	}
	return 0
}
