package taxyear

import (
	"fmt"

	"github.com/nexteggapp/taxcalcs/money"
	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
)

// handleUnassigned processes documents in facts.unassigned: asks a question
// for each, emits NotIncluded notes for non-income or limited documents, and
// conservatively counts income and withholding so tax is never understated.
func handleUnassigned(ctx *model.Context, in *scenariosv1.ScenarioInputs, proj *scenariosv1.YearProjection, notes *model.Notes) {
	unassigned := in.GetFacts().GetUnassigned()
	if len(unassigned) == 0 {
		return
	}
	var items []*scenariosv1.Item
	d := ctx.YearEnd()
	for _, doc := range unassigned {
		name := docFormName(doc)
		notes.Ask(model.NewQuestion(
			model.ItemID("unassigned", doc.GetId()),
			"unassigned",
			fmt.Sprintf("Which source does this %s belong to?", name),
			"Until it is attached to a source, this document's income counts under a placeholder source so tax is never understated.",
			fmt.Sprintf("facts.unassigned[%s]", doc.GetId()),
		))

		var gross, taxable, fedWithheld money.USCents
		hasIncome := false

		switch {
		case doc.GetFormW2() != nil:
			w := doc.GetFormW2()
			gross = model.Value(w.GetWagesTipsOtherCompensation())
			taxable = gross
			fedWithheld = model.Value(w.GetFederalTaxWithheld())
			hasIncome = true
		case doc.GetForm_1099Int() != nil:
			i := doc.GetForm_1099Int()
			gross = model.Value(i.GetInterestIncome()) + model.Value(i.GetSavingsBondAndTreasuryInterest())
			taxable = gross
			fedWithheld = model.Value(i.GetFederalTaxWithheld())
			hasIncome = true
		case doc.GetForm_1099Div() != nil:
			div := doc.GetForm_1099Div()
			gross = model.Value(div.GetOrdinaryDividends()) + model.Value(div.GetTotalCapitalGainDistribution())
			taxable = gross
			fedWithheld = model.Value(div.GetFederalTaxWithheld())
			hasIncome = true
		case doc.GetForm_1099Oid() != nil:
			oid := doc.GetForm_1099Oid()
			gross = model.Value(oid.GetOriginalIssueDiscount()) + model.Value(oid.GetOtherPeriodicInterest()) +
				model.Value(oid.GetMarketDiscount()) + model.Value(oid.GetOriginalIssueDiscountOnTreasuryObligations())
			taxable = gross
			fedWithheld = model.Value(oid.GetFederalIncomeTaxWithheld())
			hasIncome = true
		case doc.GetForm_1099Nec() != nil:
			nec := doc.GetForm_1099Nec()
			gross = model.Value(nec.GetNonemployeeCompensation())
			taxable = gross
			fedWithheld = model.Value(nec.GetFederalTaxWithheld())
			hasIncome = true
		case doc.GetForm_1099K() != nil:
			k := doc.GetForm_1099K()
			gross = model.Value(k.GetGrossPaymentTransactions())
			taxable = gross
			fedWithheld = model.Value(k.GetFederalTaxWithheld())
			hasIncome = true
		case doc.GetForm_1099Misc() != nil:
			m := doc.GetForm_1099Misc()
			gross = model.Value(m.GetOtherIncome()) +
				model.Value(m.GetRents()) +
				model.Value(m.GetRoyalties()) +
				model.Value(m.GetFishingBoatProceeds()) +
				model.Value(m.GetMedicalAndHealthCarePayments()) +
				model.Value(m.GetCropInsuranceProceeds()) +
				model.Value(m.GetGrossProceedsPaidToAttorney()) +
				model.Value(m.GetFishPurchasedForResale()) +
				model.Value(m.GetNonqualifiedDeferredCompensation())
			taxable = gross
			fedWithheld = model.Value(m.GetFederalTaxWithheld())
			hasIncome = true
		case doc.GetForm_1099R() != nil:
			r := doc.GetForm_1099R()
			gross = model.Value(r.GetGrossDistribution())
			taxable = gross
			if r.GetTaxableAmount() != nil && !r.GetTaxableAmountNotDetermined() {
				taxable = model.Value(r.GetTaxableAmount())
			}
			fedWithheld = model.Value(r.GetFederalTaxWithheld())
			hasIncome = true
		case doc.GetForm_1099G() != nil:
			g := doc.GetForm_1099G()
			gross = model.Value(g.GetUnemploymentCompensation()) + model.Value(g.GetStateOrLocalRefund())
			taxable = gross
			fedWithheld = model.Value(g.GetFederalTaxWithheld())
			hasIncome = true
		case doc.GetFormSsa_1099() != nil:
			ssa := doc.GetFormSsa_1099()
			gross = model.Value(ssa.GetNetBenefits())
			taxable = gross
			fedWithheld = model.Value(ssa.GetFederalTaxWithheld())
			hasIncome = true
		case doc.GetFormW2G() != nil:
			w2g := doc.GetFormW2G()
			gross = model.Value(w2g.GetReportableWinnings())
			taxable = gross
			fedWithheld = model.Value(w2g.GetFederalTaxWithheld())
			hasIncome = true
		case doc.GetForm_1099Sa() != nil:
			sa := doc.GetForm_1099Sa()
			gross = model.Value(sa.GetGrossDistribution())
			taxable = gross
			hasIncome = true
			notes.LeaveOut("The 20% additional tax on unassigned Form 1099-SA distributions (Form 8889 Part II)")
		case doc.GetForm_1099S() != nil:
			s := doc.GetForm_1099S()
			gross = model.Value(s.GetGrossProceeds())
			taxable = gross
			hasIncome = true
			notes.LeaveOut("Cost basis and § 121 principal residence exclusion for unassigned Form 1099-S real estate proceeds")
		case doc.GetForm_1099C() != nil:
			c := doc.GetForm_1099C()
			gross = model.Value(c.GetAmountOfDebtDischarged())
			taxable = gross
			hasIncome = true
			notes.LeaveOut("Form 982 insolvency or bankruptcy exclusions for unassigned Form 1099-C canceled debt")
		case doc.GetForm_1099Q() != nil:
			q := doc.GetForm_1099Q()
			gross = model.Value(q.GetGrossDistribution())
			taxable = model.Value(q.GetEarnings())
			if taxable == 0 && gross > 0 {
				taxable = gross
			}
			hasIncome = true
			notes.LeaveOut("The 10% additional tax and qualified education expense exclusion for unassigned Form 1099-Q distributions")
		case doc.GetScheduleK1_1065() != nil:
			k := doc.GetScheduleK1_1065()
			gross = model.Value(k.GetOrdinaryBusinessIncome()) +
				model.Value(k.GetNetRentalRealEstateIncome()) +
				model.Value(k.GetOtherNetRentalIncome()) +
				model.Value(k.GetTotalGuaranteedPayments()) +
				model.Value(k.GetRoyalties())
			taxable = gross
			hasIncome = true
			notes.LeaveOut("Partner basis limits, at-risk limits, and passive activity loss limitations for unassigned Schedule K-1 (Form 1065)")
		case doc.GetScheduleK1_1120S() != nil:
			k := doc.GetScheduleK1_1120S()
			gross = model.Value(k.GetOrdinaryBusinessIncome()) +
				model.Value(k.GetNetRentalRealEstateIncome()) +
				model.Value(k.GetOtherNetRentalIncome()) +
				model.Value(k.GetRoyalties())
			taxable = gross
			hasIncome = true
			notes.LeaveOut("Shareholder basis limits, at-risk limits, and passive activity loss limitations for unassigned Schedule K-1 (Form 1120-S)")
		case doc.GetScheduleK1_1041() != nil:
			k := doc.GetScheduleK1_1041()
			gross = model.Value(k.GetOrdinaryBusinessIncome()) +
				model.Value(k.GetNetRentalRealEstateIncome()) +
				model.Value(k.GetOtherRentalIncome()) +
				model.Value(k.GetOtherPortfolioAndNonbusinessIncome())
			taxable = gross
			hasIncome = true
			notes.LeaveOut("Beneficiary basis limits and passive activity loss limitations for unassigned Schedule K-1 (Form 1041)")
		case doc.GetForm_1095A() != nil:
			notes.LeaveOut("Form 1095-A Premium Tax Credit reconciliation, until attached to a healthcare model")
		case doc.GetForm_1098E() != nil:
			notes.LeaveOut("Form 1098-E student loan interest deduction, until attached to a loan model")
		case doc.GetForm_1098T() != nil:
			notes.LeaveOut("Form 1098-T education credits or tuition deduction, until attached to an education model")
		case doc.GetForm_5498() != nil:
			notes.LeaveOut("Form 5498 IRA contributions, until attached to a retirement model")
		case doc.GetForm_5498Sa() != nil:
			notes.LeaveOut("Form 5498-SA HSA contributions, until attached to a retirement model")
		case doc.GetForm_1098() != nil:
			notes.LeaveOut("Form 1098 mortgage interest deduction, until attached to a mortgage model")
		}

		if hasIncome && (gross > 0 || taxable > 0 || fedWithheld > 0) {
			fig := &scenariosv1.OtherIncomeFigures{
				Gross:           model.Money(gross),
				Taxable:         model.Money(taxable),
				FederalWithheld: model.Money(fedWithheld),
			}
			items = append(items, &scenariosv1.Item{
				Id:     model.ItemID("unassigned", d.String(), "doc", doc.GetId()),
				Date:   d.Proto(),
				Status: scenariosv1.ItemStatus_ITEM_STATUS_MADE,
				Kind:   scenariosv1.ItemKind_ITEM_KIND_PAYMENT,
				Origin: &scenariosv1.Origin{
					Basis:    scenariosv1.Origin_BASIS_DOCUMENT,
					Evidence: []string{doc.GetId()},
				},
				Figures: &scenariosv1.Item_OtherIncome{OtherIncome: fig},
			})
		}
	}

	if len(items) > 0 {
		proj.OtherIncome = append(proj.OtherIncome, &scenariosv1.OtherIncomeProjection{
			Id:    "unassigned",
			Items: items,
		})
	}
}

func docFormName(doc *factsv1.Document) string {
	switch {
	case doc.GetFormW2() != nil:
		return "Form W-2"
	case doc.GetPaystub() != nil:
		return "Paystub"
	case doc.GetForm_1099Int() != nil:
		return "Form 1099-INT"
	case doc.GetForm_1099Div() != nil:
		return "Form 1099-DIV"
	case doc.GetForm_1099B() != nil:
		return "Form 1099-B"
	case doc.GetForm_1099Nec() != nil:
		return "Form 1099-NEC"
	case doc.GetForm_1099K() != nil:
		return "Form 1099-K"
	case doc.GetForm_1099R() != nil:
		return "Form 1099-R"
	case doc.GetForm_1099G() != nil:
		return "Form 1099-G"
	case doc.GetFormSsa_1099() != nil:
		return "Form SSA-1099"
	case doc.GetFormW2G() != nil:
		return "Form W-2G"
	case doc.GetForm_1098() != nil:
		return "Form 1098"
	case doc.GetForm_1099Misc() != nil:
		return "Form 1099-MISC"
	case doc.GetForm_1098E() != nil:
		return "Form 1098-E"
	case doc.GetForm_1098T() != nil:
		return "Form 1098-T"
	case doc.GetForm_5498() != nil:
		return "Form 5498"
	case doc.GetForm_1099Sa() != nil:
		return "Form 1099-SA"
	case doc.GetForm_5498Sa() != nil:
		return "Form 5498-SA"
	case doc.GetForm_1095A() != nil:
		return "Form 1095-A"
	case doc.GetScheduleK1_1065() != nil:
		return "Schedule K-1 (Form 1065)"
	case doc.GetScheduleK1_1120S() != nil:
		return "Schedule K-1 (Form 1120-S)"
	case doc.GetScheduleK1_1041() != nil:
		return "Schedule K-1 (Form 1041)"
	case doc.GetForm_1099Oid() != nil:
		return "Form 1099-OID"
	case doc.GetForm_1099S() != nil:
		return "Form 1099-S"
	case doc.GetForm_1099C() != nil:
		return "Form 1099-C"
	case doc.GetForm_1099Q() != nil:
		return "Form 1099-Q"
	}
	return "document"
}
