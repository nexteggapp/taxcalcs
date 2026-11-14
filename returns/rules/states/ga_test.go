package states_test

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	form1099r "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_r"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
	form500 "github.com/nexteggapp/taxprotos/go/us/state/ga/v1/form_500"
)

// georgiataxcalc computes only estimated tax and has no income tax
// calculator, so these households are worked by hand from the 2025 Form 500
// and the IT-511 booklet: the standard deduction on line 11 ($12,000, or
// $24,000 joint), $4,000 a dependent on line 14, and 5.19% rounded to the
// dollar on line 16.

func gaW2(wages, fedWithheld, gaWithheld int64) *formw2.FormW2 {
	return &formw2.FormW2{
		WagesTipsOtherCompensation: cents(wages),
		FederalTaxWithheld:         cents(fedWithheld),
		StateTaxWithholding: []*commonv1.StateTaxWithholding{
			{State: "GA", StateIncome: cents(wages), TaxWithheld: cents(gaWithheld)},
		},
	}
}

func gaInputs(
	status returnsv1.FilingStatusElection,
	taxpayer, spouse *returnsv1.Person,
	dependents []*returnsv1.Dependent,
	w2s []*formw2.FormW2,
	pensions []*returnsv1.Form1099RDocument,
) *returnsv1.ReturnInputs {
	marital := returnsv1.MaritalStatus_MARITAL_STATUS_NEVER_MARRIED
	if spouse != nil {
		marital = returnsv1.MaritalStatus_MARITAL_STATUS_MARRIED
	}
	var w2Docs []*returnsv1.W2Document
	for i, w := range w2s {
		person := taxpayer.Id
		if i == 1 && spouse != nil {
			person = spouse.Id
		}
		w2Docs = append(w2Docs, &returnsv1.W2Document{Id: w2Id(i), Person: person, Form: w})
	}
	return &returnsv1.ReturnInputs{
		SchemaVersion: 1,
		TaxYear:       2025,
		Federal: &returnsv1.Federal1040Inputs{
			Household: &returnsv1.Household{
				MaritalStatus: marital,
				Taxpayer:      taxpayer,
				Spouse:        spouse,
				Dependents:    dependents,
			},
			Elections: &returnsv1.Elections{FilingStatus: status},
			Documents: &returnsv1.Documents{W2S: w2Docs, RetirementDistributions: pensions},
		},
		States: []*returnsv1.StateInputs{
			{State: "GA", Inputs: &returnsv1.StateInputs_Ga{Ga: &returnsv1.GeorgiaInputs{}}},
		},
	}
}

func prepareGeorgia(t *testing.T, in *returnsv1.ReturnInputs) (*returnsv1.GeorgiaReturn, *form500.Form500) {
	t.Helper()
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatal(err)
	}
	out, err := eng.Prepare(in)
	if err != nil {
		t.Fatal(err)
	}
	ret := out.GetStates()[0].GetGa()
	if ret == nil {
		t.Fatal("expected a Georgia return")
	}
	return ret, ret.GetForms().GetForm_500()
}

// A single filer with $60,000 of wages.
//   - Line 8, 10: $60,000; line 9: $0
//   - Line 11: $12,000; line 13: $48,000; line 14: $0; line 15c: $48,000
//   - Line 16: $48,000 x 5.19% = $2,491.20, rounded to $2,491
//   - Line 24, 28: $2,800 withheld; line 30, 46: $2,800 - $2,491 = $309
func TestGeorgiaSingle(t *testing.T) {
	p := ilPerson("john", 1988, 5, 12, false, false)
	ret, f := prepareGeorgia(t, gaInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		p, nil, nil,
		[]*formw2.FormW2{gaW2(60000_00, 6000_00, 2800_00)}, nil))

	want(t, "Line 8 Federal AGI", f.GetFederalAdjustedGrossIncome(), money.USCents(60000_00))
	want(t, "Line 9 Schedule 1", f.GetSchedule_1Adjustments(), 0)
	want(t, "Line 10 Georgia AGI", f.GetGeorgiaAdjustedGrossIncome(), money.USCents(60000_00))
	want(t, "Line 11 Standard deduction", f.GetStandardDeduction(), money.USCents(12000_00))
	want(t, "Line 13", f.GetIncomeAfterDeductions(), money.USCents(48000_00))
	want(t, "Line 15c Taxable income", f.GetTaxableIncome(), money.USCents(48000_00))
	want(t, "Line 16 Tax", f.GetTax(), money.USCents(2491_00))
	want(t, "Line 23 Balance", f.GetTaxAfterCredits(), money.USCents(2491_00))
	want(t, "Line 24 Withheld", f.GetGeorgiaTaxWithheld(), money.USCents(2800_00))
	want(t, "Line 28 Total payments", f.GetTotalPayments(), money.USCents(2800_00))
	want(t, "Line 46 Refund", f.GetRefund(), money.USCents(309_00))
	want(t, "Line 45 Amount owed", f.GetAmountOwed(), 0)
	if f.GetFilingStatus() != form500.FilingStatus_FILING_STATUS_SINGLE {
		t.Errorf("filing status = %v, want single", f.GetFilingStatus())
	}
	if ret.GetStatus() != returnsv1.ReturnStatus_RETURN_STATUS_COMPLETE {
		t.Errorf("status = %v: %v", ret.GetStatus(), ret.GetDiagnostics())
	}
	want(t, "Summary refund", ret.GetSummary().GetRefund(), money.USCents(309_00))
}

// Married filing jointly with two children and $140,000 of wages.
//   - Line 8, 10: $140,000
//   - Line 11: $24,000; line 13: $116,000
//   - Line 7c: 2; line 14: 2 x $4,000 = $8,000; line 15c: $108,000
//   - Line 16: $108,000 x 5.19% = $5,605.20, rounded to $5,605
//   - Line 24, 28: $4,000 + $1,500 = $5,500; line 29, 45: $5,605 - $5,500 = $105
func TestGeorgiaMarriedFilingJointlyWithChildren(t *testing.T) {
	p1 := ilPerson("alice", 1985, 3, 15, false, false)
	p2 := ilPerson("bob", 1984, 8, 20, false, false)
	deps := []*returnsv1.Dependent{ilDependent("cara", 2017, 6, 1), ilDependent("dan", 2019, 2, 9)}
	ret, f := prepareGeorgia(t, gaInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY,
		p1, p2, deps,
		[]*formw2.FormW2{gaW2(95000_00, 10000_00, 4000_00), gaW2(45000_00, 4500_00, 1500_00)}, nil))

	if f.GetTotalDependents() != 2 {
		t.Errorf("Line 7c = %d, want 2", f.GetTotalDependents())
	}
	want(t, "Line 8 Federal AGI", f.GetFederalAdjustedGrossIncome(), money.USCents(140000_00))
	want(t, "Line 11 Standard deduction", f.GetStandardDeduction(), money.USCents(24000_00))
	want(t, "Line 14 Dependent exemption", f.GetDependentExemption(), money.USCents(8000_00))
	want(t, "Line 15c Taxable income", f.GetTaxableIncome(), money.USCents(108000_00))
	want(t, "Line 16 Tax", f.GetTax(), money.USCents(5605_00))
	want(t, "Line 28 Total payments", f.GetTotalPayments(), money.USCents(5500_00))
	want(t, "Line 29 Balance due", f.GetBalanceDue(), money.USCents(105_00))
	want(t, "Line 45 Amount owed", f.GetAmountOwed(), money.USCents(105_00))
	want(t, "Line 46 Refund", f.GetRefund(), 0)
	if f.GetFilingStatus() != form500.FilingStatus_FILING_STATUS_MARRIED_FILING_JOINTLY {
		t.Errorf("filing status = %v, want married filing jointly", f.GetFilingStatus())
	}
	if ret.GetStatus() != returnsv1.ReturnStatus_RETURN_STATUS_COMPLETE {
		t.Errorf("status = %v: %v", ret.GetStatus(), ret.GetDiagnostics())
	}
}

// A single retiree, 66 at the end of 2025, with a $90,000 taxable pension.
//   - Line 8: $90,000
//   - Schedule 1 page 2: line 14 $90,000, line 16 $65,000 (65 or older),
//     line 17 $65,000; Schedule 1 line 7a $65,000, line 14 -$65,000
//   - Line 9: -$65,000; line 10: $25,000; line 11: $12,000; line 15c: $13,000
//   - Line 16: $13,000 x 5.19% = $674.70, rounded to $675
//   - Line 24, 28: $2,000 withheld from the pension; line 46: $1,325
func TestGeorgiaRetiree(t *testing.T) {
	p := ilPerson("ruth", 1959, 3, 2, false, false)
	pension := &returnsv1.Form1099RDocument{
		Id:     "1099r-pension",
		Person: "ruth",
		Form: &form1099r.Form1099R{
			GrossDistribution: cents(90000_00),
			TaxableAmount:     cents(90000_00),
			DistributionCodes: []string{"7"},
			StateTaxWithholding: []*commonv1.StateTaxWithholding{
				{State: "GA", StateIncome: cents(90000_00), TaxWithheld: cents(2000_00)},
			},
		},
	}
	ret, f := prepareGeorgia(t, gaInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		p, nil, nil, nil, []*returnsv1.Form1099RDocument{pension}))

	want(t, "Line 8 Federal AGI", f.GetFederalAdjustedGrossIncome(), money.USCents(90000_00))
	want(t, "Line 9 Schedule 1", f.GetSchedule_1Adjustments(), money.USCents(-65000_00))
	want(t, "Line 10 Georgia AGI", f.GetGeorgiaAdjustedGrossIncome(), money.USCents(25000_00))
	want(t, "Line 15c Taxable income", f.GetTaxableIncome(), money.USCents(13000_00))
	want(t, "Line 16 Tax", f.GetTax(), money.USCents(675_00))
	want(t, "Line 24 Withheld", f.GetGeorgiaTaxWithheld(), money.USCents(2000_00))
	want(t, "Line 46 Refund", f.GetRefund(), money.USCents(1325_00))
	want(t, "Line 45 Amount owed", f.GetAmountOwed(), 0)
	if ret.GetStatus() != returnsv1.ReturnStatus_RETURN_STATUS_COMPLETE {
		t.Errorf("status = %v: %v", ret.GetStatus(), ret.GetDiagnostics())
	}
}

// A low-income filer may be owed the Low Income Credit, which is not
// computed, so the return says so and is incomplete.
func TestGeorgiaLowIncomeCreditWarning(t *testing.T) {
	p := ilPerson("lee", 1999, 1, 1, false, false)
	ret, f := prepareGeorgia(t, gaInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		p, nil, nil,
		[]*formw2.FormW2{gaW2(15000_00, 0, 100_00)}, nil))

	// $15,000 - $12,000 = $3,000; x 5.19% = $155.70, rounded to $156.
	want(t, "Line 16 Tax", f.GetTax(), money.USCents(156_00))
	want(t, "Line 45 Amount owed", f.GetAmountOwed(), money.USCents(56_00))
	found := false
	for _, d := range ret.GetDiagnostics() {
		if d.GetCode() == "GA_LOW_INCOME_CREDIT_NOT_MODELLED" {
			found = true
		}
	}
	if !found || ret.GetStatus() != returnsv1.ReturnStatus_RETURN_STATUS_INCOMPLETE {
		t.Errorf("want an incomplete return with the low income credit warning, got %v: %v",
			ret.GetStatus(), ret.GetDiagnostics())
	}
}
