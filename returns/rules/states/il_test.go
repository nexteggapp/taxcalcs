package states_test

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
	formil1040 "github.com/nexteggapp/taxprotos/go/us/state/il/v1/form_il1040"
)

// Helper to construct basic Illinois return inputs.
func ilInputs(
	status returnsv1.FilingStatusElection,
	taxpayer *returnsv1.Person,
	spouse *returnsv1.Person,
	dependents []*returnsv1.Dependent,
	w2s []*formw2.FormW2,
	propertyTax int64,
) *returnsv1.ReturnInputs {
	var marital returnsv1.MaritalStatus
	if status == returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY ||
		status == returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_SEPARATELY {
		marital = returnsv1.MaritalStatus_MARITAL_STATUS_MARRIED
	} else {
		marital = returnsv1.MaritalStatus_MARITAL_STATUS_NEVER_MARRIED
	}

	hh := &returnsv1.Household{
		MaritalStatus: marital,
		Taxpayer:      taxpayer,
		Spouse:        spouse,
		Dependents:    dependents,
	}

	var w2Docs []*returnsv1.W2Document
	for i, w := range w2s {
		personId := taxpayer.Id
		if i == 1 && spouse != nil {
			personId = spouse.Id
		}
		w2Docs = append(w2Docs, &returnsv1.W2Document{
			Id:     w2Id(i),
			Person: personId,
			Form:   w,
		})
	}

	ilIn := &returnsv1.IllinoisInputs{}
	if propertyTax > 0 {
		ilIn.PropertyTaxPaid = cents(propertyTax)
	}

	return &returnsv1.ReturnInputs{
		SchemaVersion: 1,
		TaxYear:       2025,
		Federal: &returnsv1.Federal1040Inputs{
			Household: hh,
			Elections: &returnsv1.Elections{FilingStatus: status},
			Documents: &returnsv1.Documents{W2S: w2Docs},
		},
		States: []*returnsv1.StateInputs{
			{
				State:  "IL",
				Inputs: &returnsv1.StateInputs_Il{Il: ilIn},
			},
		},
	}
}

func w2Id(i int) string {
	return []string{"w2_0", "w2_1", "w2_2"}[i]
}

func ilPerson(id string, y, m, d int32, blind bool, claimable bool) *returnsv1.Person {
	return &returnsv1.Person{
		Id:                      id,
		FirstName:               id,
		DateOfBirth:             &commonv1.Date{Year: y, Month: m, Day: d},
		Blind:                   boolPtr(blind),
		CanBeClaimedAsDependent: boolPtr(claimable),
		Tin: &returnsv1.TaxpayerID{
			Kind:               returnsv1.TaxpayerIDKind_TAXPAYER_ID_KIND_SSN,
			ValidForEmployment: boolPtr(true),
		},
	}
}

func ilDependent(id string, y, m, d int32) *returnsv1.Dependent {
	return &returnsv1.Dependent{
		Id:                              id,
		FirstName:                       id,
		DateOfBirth:                     &commonv1.Date{Year: y, Month: m, Day: d},
		Relationship:                    returnsv1.Relationship_RELATIONSHIP_CHILD,
		MonthsLivedWithYou:              int32Ptr(12),
		MainHomeInUsOverHalfYear:        boolPtr(true),
		CitizenOrResidentOfNorthAmerica: boolPtr(true),
		Tin: &returnsv1.TaxpayerID{
			Kind:               returnsv1.TaxpayerIDKind_TAXPAYER_ID_KIND_SSN,
			ValidForEmployment: boolPtr(true),
		},
	}
}

func int32Ptr(i int32) *int32 { return &i }

func ilW2(wages, fedWithheld, ilWithheld int64) *formw2.FormW2 {
	return &formw2.FormW2{
		WagesTipsOtherCompensation: cents(wages),
		FederalTaxWithheld:         cents(fedWithheld),
		StateTaxWithholding: []*commonv1.StateTaxWithholding{
			{State: "IL", StateIncome: cents(wages), TaxWithheld: cents(ilWithheld)},
		},
	}
}

// 1. Single filer with wage income and residential property tax credit.
// Hand calculations:
//   - Federal AGI (Line 1): $60,000.00
//   - Total Income (Line 4): $60,000.00
//   - Base Income (Line 9): $60,000.00
//   - Line 10a exemption: $2,850.00 (Instructions p. 8, Line 10a table: Single, line C unchecked)
//   - Exemption allowance (Line 10): $2,850.00
//   - Net income (Line 11): $60,000 - $2,850 = $57,150.00
//   - Tax amount (Line 12): $57,150 * 4.95% = $2,828.925 -> round $2,829.00 (Instructions p. 4, 8)
//   - Total tax (Line 14): $2,829.00
//   - Property tax credit (Line 16): 5% of $4,000 = $200.00 (Instructions p. 9; Schedule ICR line 4g)
//   - Total credits (Line 18): $200.00
//   - Net tax (Line 19 / 24): $2,829 - $200 = $2,629.00
//   - Withheld (Line 25): $2,800.00
//   - Total payments (Line 31): $2,800.00
//   - Overpayment / Refund (Line 32, 37, 38): $2,800 - $2,629 = $171.00
//   - Tax due (Line 41): $0.00
func TestIllinoisSingle(t *testing.T) {
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatal(err)
	}

	p := ilPerson("john", 1988, 5, 12, false, false)
	w2 := ilW2(60000_00, 6000_00, 2800_00)
	in := ilInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		p, nil, nil,
		[]*formw2.FormW2{w2},
		4000_00,
	)

	out, err := eng.Prepare(in)
	if err != nil {
		t.Fatal(err)
	}

	ret := out.GetStates()[0].GetIl()
	if ret == nil {
		t.Fatal("expected Illinois return")
	}
	f := ret.GetForms().GetFormIl1040()

	want(t, "Line 1 Federal AGI", f.GetFederalAdjustedGrossIncome(), money.USCents(60000_00))
	want(t, "Line 9 Base income", f.GetBaseIncome(), money.USCents(60000_00))
	want(t, "Line 10 Exemption allowance", f.GetExemptionAllowance(), money.USCents(2850_00))
	want(t, "Line 11 Net income", f.GetNetIncome(), money.USCents(57150_00))
	want(t, "Line 12 Tax amount", f.GetTax(), money.USCents(2829_00))
	want(t, "Line 16 Property tax credit", f.GetPropertyTaxAndEducationCredits(), money.USCents(200_00))
	want(t, "Line 18 Total credits", f.GetTotalCredits(), money.USCents(200_00))
	want(t, "Line 19 Net tax", f.GetTaxAfterCredits(), money.USCents(2629_00))
	want(t, "Line 25 Withheld", f.GetIllinoisTaxWithheld(), money.USCents(2800_00))
	want(t, "Line 31 Total payments", f.GetTotalPaymentsAndRefundableCredits(), money.USCents(2800_00))
	want(t, "Line 37 Overpayment", f.GetOverpaymentAfterPenaltyAndDonations(), money.USCents(171_00))
	want(t, "Line 41 Tax due", f.GetAmountOwed(), money.USCents(0))

	if ret.GetStatus() != returnsv1.ReturnStatus_RETURN_STATUS_COMPLETE {
		t.Errorf("expected complete return, got %v: %v", ret.GetStatus(), ret.GetDiagnostics())
	}
}

// 2. Married filing jointly with two earners, one dependent child, and property tax paid.
// Hand calculations:
//   - Federal AGI (Line 1): $95,000 + $45,000 = $140,000.00
//   - Base Income (Line 9): $140,000.00
//   - Line 10a personal exemptions: 2 * $2,850 = $5,700.00 (Instructions p. 8, Line 10a table: MFJ)
//   - Line 10d dependent exemptions: 1 * $2,850 = $2,850.00 (Instructions p. 8, Line 10d; Sch. IL-E/EITC Step 2)
//   - Exemption allowance (Line 10): $5,700 + $2,850 = $8,550.00
//   - Net income (Line 11): $140,000 - $8,550 = $131,450.00
//   - Tax amount (Line 12 / 14): $131,450 * 4.95% = $6,506.775 -> round $6,507.00 (Instructions p. 4, 8)
//   - Property tax credit (Line 16): 5% of $6,000 = $300.00 (Instructions p. 9; Schedule ICR line 4g)
//   - Net tax (Line 19 / 24): $6,507 - $300 = $6,207.00
//   - Withholding (Line 25): $4,000 + $2,000 = $6,000.00
//   - Total payments (Line 31): $6,000.00
//   - Tax due (Line 33 / 41): $6,207 - $6,000 = $207.00
//   - Overpayment (Line 37): $0.00
func TestIllinoisMarriedFilingJointly(t *testing.T) {
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatal(err)
	}

	p1 := ilPerson("alice", 1985, 3, 15, false, false)
	p2 := ilPerson("bob", 1984, 8, 20, false, false)
	dep := ilDependent("charlie", 2017, 6, 1) // age 8
	w2a := ilW2(95000_00, 10000_00, 4000_00)
	w2b := ilW2(45000_00, 4500_00, 2000_00)

	in := ilInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY,
		p1, p2,
		[]*returnsv1.Dependent{dep},
		[]*formw2.FormW2{w2a, w2b},
		6000_00,
	)

	out, err := eng.Prepare(in)
	if err != nil {
		t.Fatal(err)
	}

	ret := out.GetStates()[0].GetIl()
	f := ret.GetForms().GetFormIl1040()

	want(t, "Line 1 Federal AGI", f.GetFederalAdjustedGrossIncome(), money.USCents(140000_00))
	want(t, "Line 9 Base income", f.GetBaseIncome(), money.USCents(140000_00))
	want(t, "Line 10 Exemption allowance", f.GetExemptionAllowance(), money.USCents(8550_00))
	want(t, "Line 11 Net income", f.GetNetIncome(), money.USCents(131450_00))
	want(t, "Line 12 Tax amount", f.GetTax(), money.USCents(6507_00))
	want(t, "Line 16 Property tax credit", f.GetPropertyTaxAndEducationCredits(), money.USCents(300_00))
	want(t, "Line 19 Net tax", f.GetTaxAfterCredits(), money.USCents(6207_00))
	want(t, "Line 25 Withheld", f.GetIllinoisTaxWithheld(), money.USCents(6000_00))
	want(t, "Line 31 Total payments", f.GetTotalPaymentsAndRefundableCredits(), money.USCents(6000_00))
	want(t, "Line 37 Overpayment", f.GetOverpaymentAfterPenaltyAndDonations(), money.USCents(0))
	want(t, "Line 41 Tax due", f.GetAmountOwed(), money.USCents(207_00))
	if f.GetFilingStatus() != formil1040.FilingStatus_FILING_STATUS_MARRIED_FILING_JOINTLY {
		t.Errorf("filing status = %v, want MFJ", f.GetFilingStatus())
	}
}

// 3. Dependent filer: single student whose parents claim them, with income above $2,850.
// Hand calculations:
//   - Federal AGI (Line 1): $4,500.00
//   - Base Income (Line 9): $4,500.00
//   - Line 10a exemption: $0.00 (Instructions p. 8, Line 10a table: Single, Line C checked, base income > $2,850 -> $0)
//   - Exemption allowance (Line 10): $0.00
//   - Net income (Line 11): $4,500.00
//   - Tax amount (Line 12 / 14 / 24): $4,500 * 4.95% = $222.75 -> round $223.00 (Instructions p. 4, 8)
//   - Withholding (Line 25): $150.00
//   - Total payments (Line 31): $150.00
//   - Tax due (Line 33 / 41): $223 - $150 = $73.00
//   - Overpayment (Line 37): $0.00
func TestIllinoisDependentFiler(t *testing.T) {
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatal(err)
	}

	p := ilPerson("emma", 2004, 9, 10, false, true) // can be claimed as dependent
	w2 := ilW2(4500_00, 200_00, 150_00)
	in := ilInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		p, nil, nil,
		[]*formw2.FormW2{w2},
		0,
	)

	out, err := eng.Prepare(in)
	if err != nil {
		t.Fatal(err)
	}

	ret := out.GetStates()[0].GetIl()
	f := ret.GetForms().GetFormIl1040()

	want(t, "Line 1 Federal AGI", f.GetFederalAdjustedGrossIncome(), money.USCents(4500_00))
	want(t, "Line 9 Base income", f.GetBaseIncome(), money.USCents(4500_00))
	want(t, "Line 10 Exemption allowance", f.GetExemptionAllowance(), money.USCents(0))
	want(t, "Line 11 Net income", f.GetNetIncome(), money.USCents(4500_00))
	want(t, "Line 12 Tax amount", f.GetTax(), money.USCents(223_00))
	want(t, "Line 25 Withheld", f.GetIllinoisTaxWithheld(), money.USCents(150_00))
	want(t, "Line 41 Tax due", f.GetAmountOwed(), money.USCents(73_00))
	want(t, "Line 37 Overpayment", f.GetOverpaymentAfterPenaltyAndDonations(), money.USCents(0))
}

// 4. High income filer: Single filer with AGI > $250,000 threshold.
// Hand calculations:
//   - Federal AGI (Line 1): $350,000.00
//   - Base Income (Line 9): $350,000.00
//   - Exemption allowance (Line 10): $0.00 (Instructions p. 8, Income Exceptions: Single filer with AGI > $250,000)
//   - Net income (Line 11): $350,000.00
//   - Tax amount (Line 12 / 14 / 24): $350,000 * 4.95% = $17,325.00 (Instructions p. 8)
//   - Property tax paid: $12,000.00, but Property tax credit (Line 16): $0.00 (Instructions p. 9, Income Exceptions)
//   - Total credits (Line 18): $0.00
//   - Net tax (Line 19 / 24): $17,325.00
//   - Withholding (Line 25): $18,000.00
//   - Total payments (Line 31): $18,000.00
//   - Overpayment / Refund (Line 32, 37, 38): $18,000 - $17,325 = $675.00
//   - Tax due (Line 41): $0.00
func TestIllinoisHighIncomePhaseout(t *testing.T) {
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatal(err)
	}

	p := ilPerson("victor", 1975, 11, 30, false, false)
	w2 := ilW2(350000_00, 75000_00, 18000_00)
	in := ilInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		p, nil, nil,
		[]*formw2.FormW2{w2},
		12000_00,
	)

	out, err := eng.Prepare(in)
	if err != nil {
		t.Fatal(err)
	}

	ret := out.GetStates()[0].GetIl()
	f := ret.GetForms().GetFormIl1040()

	want(t, "Line 1 Federal AGI", f.GetFederalAdjustedGrossIncome(), money.USCents(350000_00))
	want(t, "Line 10 Exemption allowance", f.GetExemptionAllowance(), money.USCents(0))
	want(t, "Line 11 Net income", f.GetNetIncome(), money.USCents(350000_00))
	want(t, "Line 12 Tax amount", f.GetTax(), money.USCents(17325_00))
	want(t, "Line 16 Property tax credit", f.GetPropertyTaxAndEducationCredits(), money.USCents(0))
	want(t, "Line 19 Net tax", f.GetTaxAfterCredits(), money.USCents(17325_00))
	want(t, "Line 25 Withheld", f.GetIllinoisTaxWithheld(), money.USCents(18000_00))
	want(t, "Line 37 Overpayment", f.GetOverpaymentAfterPenaltyAndDonations(), money.USCents(675_00))
	want(t, "Line 41 Tax due", f.GetAmountOwed(), money.USCents(0))

	// Verify that the informational diagnostic for AGI limits is present.
	foundPropTaxDiag := false
	foundExemptDiag := false
	for _, d := range ret.GetDiagnostics() {
		if d.GetCode() == "IL_PROPERTY_TAX_AGI_LIMIT" {
			foundPropTaxDiag = true
		}
		if d.GetCode() == "IL_EXEMPTION_AGI_LIMIT" {
			foundExemptDiag = true
		}
	}
	if !foundPropTaxDiag || !foundExemptDiag {
		t.Errorf("expected AGI limit diagnostics, got %v", ret.GetDiagnostics())
	}
}

// 5. Low income filer with 1 qualifying child under age 12, eligible for Illinois EITC and Child Tax Credit.
// Hand calculations:
//   - Single parent with 1 qualifying child age 5
//   - Wages: $22,000.00
//   - Federal AGI (Line 1): $22,000.00
//   - Line 10a (self): $2,850.00 (Instructions p. 8)
//   - Line 10d (1 dependent): $2,850.00 (Instructions p. 8; Sch. IL-E/EITC Step 2 Line 1)
//   - Exemption allowance (Line 10): $5,700.00
//   - Net income (Line 11): $22,000 - $5,700 = $16,300.00
//   - Tax amount (Line 12 / 14 / 24): $16,300 * 4.95% = $806.85 -> round $807.00 (Instructions p. 4, 8)
//   - Federal EITC is computed by federal engine.
//   - Illinois EITC (Line 29): 20% of Federal EITC, rounded to whole dollar (Instructions p. 12; 35 ILCS 5/212)
//   - Illinois Child Tax Credit (Line 30): 40% of Illinois EITC, rounded to whole dollar (Instructions p. 12; 35 ILCS 5/212.1)
//   - Withholding (Line 25): $500.00
//   - Total payments (Line 31): $500 + Line 29 + Line 30
//   - Overpayment / Refund (Line 32, 37, 38): Line 31 - $807.00
func TestIllinoisLowIncomeEITCAndChildTaxCredit(t *testing.T) {
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatal(err)
	}

	p := ilPerson("maria", 1995, 4, 18, false, false)
	dep := ilDependent("diego", 2020, 7, 22) // age 5 (under 12)
	w2 := ilW2(22000_00, 200_00, 500_00)
	in := ilInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		p, nil,
		[]*returnsv1.Dependent{dep},
		[]*formw2.FormW2{w2},
		0,
	)

	out, err := eng.Prepare(in)
	if err != nil {
		t.Fatal(err)
	}

	ret := out.GetStates()[0].GetIl()
	f := ret.GetForms().GetFormIl1040()

	want(t, "Line 1 Federal AGI", f.GetFederalAdjustedGrossIncome(), money.USCents(22000_00))
	want(t, "Line 10 Exemption allowance", f.GetExemptionAllowance(), money.USCents(5700_00))
	want(t, "Line 11 Net income", f.GetNetIncome(), money.USCents(16300_00))
	want(t, "Line 12 Tax amount", f.GetTax(), money.USCents(807_00))

	// Federal EITC for 2025 single 1 child at $22,000 wages:
	fedEITC := out.GetFederal().GetForms().GetForm_1040().GetEarnedIncomeCredit().GetMinor()
	if fedEITC <= 0 {
		t.Fatalf("expected positive federal EITC, got %d", fedEITC)
	}

	// 20% of federal EITC rounded to whole dollar
	wantEITC := money.USCents((fedEITC*20 + 5000) / 10000 * 100)
	want(t, "Line 29 Illinois EITC", f.GetEarnedIncomeCredit(), wantEITC)

	// 40% of Illinois EITC rounded to whole dollar
	wantCTC := money.USCents((int64(wantEITC)*40 + 5000) / 10000 * 100)
	wantTotalPayments := money.USCents(500_00) + wantEITC + wantCTC
	want(t, "Line 31 Total payments", f.GetTotalPaymentsAndRefundableCredits(), wantTotalPayments)

	wantRefund := wantTotalPayments - money.USCents(807_00)
	want(t, "Line 37 Overpayment", f.GetOverpaymentAfterPenaltyAndDonations(), wantRefund)
	want(t, "Line 41 Tax due", f.GetAmountOwed(), money.USCents(0))

	if ret.GetStatus() != returnsv1.ReturnStatus_RETURN_STATUS_COMPLETE {
		t.Errorf("expected complete return, got %v: %v", ret.GetStatus(), ret.GetDiagnostics())
	}
}
