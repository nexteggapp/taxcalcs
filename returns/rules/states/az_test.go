package states_test

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	form140 "github.com/nexteggapp/taxprotos/go/us/state/az/v1/form_140"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
)

func azDependent(id string, birthYear int32) *returnsv1.Dependent {
	return &returnsv1.Dependent{
		Id:                              id,
		FirstName:                       id,
		Tin: &returnsv1.TaxpayerID{
			Kind:               returnsv1.TaxpayerIDKind_TAXPAYER_ID_KIND_SSN,
			ValidForEmployment: boolPtr(true),
		},
		DateOfBirth:                     &commonv1.Date{Year: birthYear, Month: 5, Day: 1},
		Relationship:                    returnsv1.Relationship_RELATIONSHIP_CHILD,
		MonthsLivedWithYou:              int32Ptr(12),
		CitizenOrResidentOfNorthAmerica: boolPtr(true),
		ProvidedOverHalfOwnSupport:      boolPtr(false),
	}
}

// Helper to construct ReturnInputs for an Arizona filer.
func azInputs(
	status returnsv1.FilingStatusElection,
	wages int64,
	withheld int64,
	opts ...func(*returnsv1.ReturnInputs),
) *returnsv1.ReturnInputs {
	person := func(id string, birthYear int32) *returnsv1.Person {
		return &returnsv1.Person{
			Id:          id,
			FirstName:   id,
			DateOfBirth: &commonv1.Date{Year: birthYear, Month: 6, Day: 1},
			Tin: &returnsv1.TaxpayerID{
				Kind:               returnsv1.TaxpayerIDKind_TAXPAYER_ID_KIND_SSN,
				ValidForEmployment: boolPtr(true),
			},
		}
	}
	hh := &returnsv1.Household{
		MaritalStatus: returnsv1.MaritalStatus_MARITAL_STATUS_NEVER_MARRIED,
		Taxpayer:      person("taxpayer", 1985),
	}
	if status == returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY ||
		status == returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_SEPARATELY {
		hh.MaritalStatus = returnsv1.MaritalStatus_MARITAL_STATUS_MARRIED
		hh.Spouse = person("spouse", 1987)
	}
	w2 := &formw2.FormW2{
		WagesTipsOtherCompensation: cents(wages),
		FederalTaxWithheld:         cents(wages / 10),
		StateTaxWithholding: []*commonv1.StateTaxWithholding{
			{State: "AZ", StateIncome: cents(wages), TaxWithheld: cents(withheld)},
		},
	}
	in := &returnsv1.ReturnInputs{
		SchemaVersion: 1,
		TaxYear:       2025,
		Federal: &returnsv1.Federal1040Inputs{
			Household: hh,
			Elections: &returnsv1.Elections{FilingStatus: status},
			Documents: &returnsv1.Documents{
				W2S: []*returnsv1.W2Document{{Id: "w2-az", Person: "taxpayer", Form: w2}},
			},
		},
		States: []*returnsv1.StateInputs{
			{
				State: "AZ",
				Inputs: &returnsv1.StateInputs_Az{
					Az: &returnsv1.ArizonaInputs{},
				},
			},
		},
	}
	for _, opt := range opts {
		opt(in)
	}
	return in
}

func prepareAZ(t *testing.T, in *returnsv1.ReturnInputs) (*returnsv1.ArizonaReturn, *form140.Form140) {
	t.Helper()
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine: %v", err)
	}
	out, err := eng.Prepare(in)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if len(out.GetStates()) == 0 || out.GetStates()[0].GetAz() == nil {
		t.Fatalf("expected Arizona return in outputs, got %v", out.GetStates())
	}
	az := out.GetStates()[0].GetAz()
	f := az.GetForms().GetForm_140()
	if f == nil {
		t.Fatalf("expected Form 140 in Arizona return, got %v", az.GetForms())
	}
	return az, f
}

// 1. Single middle-income filer:
// Hand calculation cited to 2025 Form 140 Instructions:
// - Page 21, Line 43: Single standard deduction = $15,750 (A.R.S. § 43-1041(A)(1)).
// - Page 21, Line 44: Taxable income = $60,000.00 - $15,750.00 = $44,250.00.
// - Page 21, Line 45: Tax = 2.5% of $44,250.00 = $1,106.25 (A.R.S. § 43-1011(A)).
// - Page 22, Line 52: Net tax = $1,106.25.
// - Page 22, Line 53 & 59: Withholding = $1,500.00.
// - Page 22, Line 61 & 79: Overpayment / refund = $1,500.00 - $1,106.25 = $393.75.
func TestArizonaSingleFiler(t *testing.T) {
	in := azInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, 60_000_00, 1_500_00)
	az, f := prepareAZ(t, in)

	want(t, "AZ Federal AGI (line 12)", f.GetFederalAdjustedGrossIncome(), 60_000_00)
	want(t, "AZ AGI (line 37/42)", f.GetArizonaAdjustedGrossIncome(), 60_000_00)
	want(t, "AZ standard deduction (line 43)", f.GetDeduction(), 15_750_00)
	want(t, "AZ taxable income (line 45)", f.GetTaxableIncome(), 44_250_00)
	want(t, "AZ tax amount (line 46)", f.GetTax(), 1_106_25)
	want(t, "AZ net tax (line 52)", f.GetTaxAfterCredits(), 1_106_25)
	want(t, "AZ tax withheld (line 53)", f.GetArizonaTaxWithheld(), 1_500_00)
	want(t, "AZ total payments (line 59)", f.GetTotalPaymentsAndRefundableCredits(), 1_500_00)
	want(t, "AZ overpayment (line 61)", f.GetOverpayment(), 393_75)
	want(t, "AZ tax due (line 60)", f.GetTaxDue(), 0)
	complete(t, "AZ", az.GetStatus(), az.GetDiagnostics())
}

// 2. Married filing jointly with 2 dependents under age 17:
// Hand calculation cited to 2025 Form 140 Instructions:
// - Page 21, Line 43: MFJ standard deduction = $31,500 (A.R.S. § 43-1041(A)(2)).
// - Page 21, Line 44: Taxable income = $80,000.00 - $31,500.00 = $48,500.00.
// - Page 21, Line 45: Tax = 2.5% of $48,500.00 = $1,212.50 (A.R.S. § 43-1011(A)).
// - Pages 21-22, Line 49: Dependent Tax Credit Worksheet:
//   Line 1: 2 dependents under 17 * $100 = $200.00 (A.R.S. § 43-1073.01).
//   Line 4: AGI $80,000 <= $400,000 MFJ threshold -> full credit $200.00.
// - Page 22, Line 52: Net tax = $1,212.50 - $200.00 = $1,012.50.
// - Page 22, Line 53 & 59: Withholding = $1,000.00.
// - Page 22, Line 60 & 80: Tax due = $1,012.50 - $1,000.00 = $12.50.
func TestArizonaMarriedFilingJointlyWithDependents(t *testing.T) {
	in := azInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY,
		80_000_00,
		1_000_00,
		func(i *returnsv1.ReturnInputs) {
			i.Federal.Household.Dependents = []*returnsv1.Dependent{
				azDependent("child1", 2015), // Age 10 in 2025 (< 17)
				azDependent("child2", 2018), // Age 7 in 2025 (< 17)
			}
		},
	)
	az, f := prepareAZ(t, in)

	if (f.GetDependentsUnder_17() + f.GetDependents_17AndOver()) != 2 {
		t.Errorf("AZ dependents count = %d, want 2", (f.GetDependentsUnder_17() + f.GetDependents_17AndOver()))
	}
	want(t, "AZ Federal AGI (line 12)", f.GetFederalAdjustedGrossIncome(), 80_000_00)
	want(t, "AZ standard deduction (line 43)", f.GetDeduction(), 31_500_00)
	want(t, "AZ taxable income (line 45)", f.GetTaxableIncome(), 48_500_00)
	want(t, "AZ tax amount (line 46)", f.GetTax(), 1_212_50)
	want(t, "AZ nonrefundable credits (line 49)", f.GetDependentTaxCredit(), 200_00)
	want(t, "AZ net tax (line 52)", f.GetTaxAfterCredits(), 1_012_50)
	want(t, "AZ withholding (line 53)", f.GetArizonaTaxWithheld(), 1_000_00)
	want(t, "AZ tax due (line 60)", f.GetTaxDue(), 12_50)
	want(t, "AZ overpayment (line 61)", f.GetOverpayment(), 0)
	complete(t, "AZ", az.GetStatus(), az.GetDiagnostics())
}

// 3. Head of household with 1 dependent under 17 and 1 dependent age 17+:
// Hand calculation cited to 2025 Form 140 Instructions:
// - Page 21, Line 43: Head of Household standard deduction = $23,625 (A.R.S. § 43-1041(A)(3)).
// - Page 21, Line 44: Taxable income = $55,000.00 - $23,625.00 = $31,375.00.
// - Page 21, Line 45: Tax = 2.5% of $31,375.00 = $784.375 -> rounded to cents = $784.38.
// - Pages 21-22, Line 49: Dependent Tax Credit Worksheet:
//   Line 1: 1 dependent under 17 * $100 = $100.00.
//   Line 2: 1 dependent age 17+ * $25 = $25.00.
//   Line 3: Base credit = $125.00.
//   Line 4: AGI $55,000 <= $200,000 HOH threshold -> allowable credit = $125.00.
// - Page 22, Line 52: Net tax = $784.38 - $125.00 = $659.38.
// - Page 22, Line 53 & 59: Withholding = $800.00.
// - Page 22, Line 61 & 79: Overpayment / refund = $800.00 - $659.38 = $140.62.
func TestArizonaHeadOfHousehold(t *testing.T) {
	in := azInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_HEAD_OF_HOUSEHOLD,
		55_000_00,
		800_00,
		func(i *returnsv1.ReturnInputs) {
			i.Federal.Household.Dependents = []*returnsv1.Dependent{
				azDependent("child", 2012),      // Age 13 (< 17)
				azDependent("older-dep", 2008),  // Age 17 (>= 17, < 19 qualifying child)
			}
		},
	)
	az, f := prepareAZ(t, in)

	if (f.GetDependentsUnder_17() + f.GetDependents_17AndOver()) != 2 {
		t.Errorf("AZ dependents count = %d, want 2", (f.GetDependentsUnder_17() + f.GetDependents_17AndOver()))
	}
	want(t, "AZ standard deduction (line 43)", f.GetDeduction(), 23_625_00)
	want(t, "AZ taxable income (line 45)", f.GetTaxableIncome(), 31_375_00)
	want(t, "AZ tax amount (line 46)", f.GetTax(), 784_38)
	want(t, "AZ nonrefundable credits (line 49)", f.GetDependentTaxCredit(), 125_00)
	want(t, "AZ net tax (line 52)", f.GetTaxAfterCredits(), 659_38)
	want(t, "AZ overpayment (line 61)", f.GetOverpayment(), 140_62)
	want(t, "AZ tax due (line 60)", f.GetTaxDue(), 0)
	complete(t, "AZ", az.GetStatus(), az.GetDiagnostics())
}

// 4. Dependent filer (taxpayer can be claimed as a dependent):
// Hand calculation cited to 2025 Form 140 Instructions:
// - Page 20, Box 8 & Line 38: Even if age 65+, Box 11 checked means Box 8 is 0,
//   line 38 exemption is $0.00 (A.R.S. § 43-1023(C)).
// - Page 21, Line 43: Arizona standard deduction is full $15,750 (no federal-style reduction
//   under A.R.S. § 43-1041).
// - Page 21, Line 44: Taxable income = max($6,000 - $15,750, 0) = $0.00.
// - Page 21, Line 45: Tax = $0.00.
// - Page 22, Line 53: Withholding = $50.00 -> Refund = $50.00.
// - Informational diagnostic AZ_DEPENDENT_FILER present; return status complete.
func TestArizonaDependentFiler(t *testing.T) {
	in := azInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		6_000_00,
		50_00,
		func(i *returnsv1.ReturnInputs) {
			i.Federal.Household.Taxpayer.CanBeClaimedAsDependent = boolPtr(true)
			i.Federal.Household.Taxpayer.DateOfBirth = &commonv1.Date{Year: 1955, Month: 1, Day: 1} // Age 70
		},
	)
	az, f := prepareAZ(t, in)

	if f.GetAge_65OrOver() != 0 {
		t.Errorf("AZ senior exemption count = %d, want 0 when claimed as dependent", f.GetAge_65OrOver())
	}
	want(t, "AZ age 65 exemption (line 38)", f.GetAge_65Exemption(), 0)
	want(t, "AZ blind exemption (line 39)", f.GetBlindExemption(), 0)
	want(t, "AZ standard deduction (line 43)", f.GetDeduction(), 15_750_00)
	want(t, "AZ taxable income (line 45)", f.GetTaxableIncome(), 0)
	want(t, "AZ tax amount (line 46)", f.GetTax(), 0)
	want(t, "AZ net tax (line 52)", f.GetTaxAfterCredits(), 0)
	want(t, "AZ overpayment (line 61)", f.GetOverpayment(), 50_00)

	// Verify informative diagnostic is issued for dependent filer
	hasDepDiag := false
	for _, d := range az.GetDiagnostics() {
		if d.GetCode() == "AZ_DEPENDENT_FILER" {
			hasDepDiag = true
			if d.GetSeverity() != returnsv1.Severity_SEVERITY_INFO {
				t.Errorf("AZ_DEPENDENT_FILER severity %s, want INFO", d.GetSeverity())
			}
		}
	}
	if !hasDepDiag {
		t.Errorf("expected AZ_DEPENDENT_FILER diagnostic, got %v", az.GetDiagnostics())
	}
	complete(t, "AZ", az.GetStatus(), az.GetDiagnostics())
}

// 5. High income filer with Dependent Tax Credit phaseout:
// Hand calculation cited to 2025 Form 140 Instructions:
// - Page 21, Line 43: Single standard deduction = $15,750.
// - Page 21, Line 44: Taxable income = $210,000.00 - $15,750.00 = $194,250.00.
// - Page 21, Line 45: Tax = 2.5% of $194,250.00 = $4,856.25 (A.R.S. § 43-1011(A)).
// - Pages 21-22, Line 49: Dependent Tax Credit Worksheet (A.R.S. § 43-1073.01):
//   Line 1: 1 dependent under 17 = $100.00.
//   Line 3: Base credit = $100.00.
//   Single threshold = $200,000. Excess = $210,000 - $200,000 = $10,000.
//   Steps = ceil($10,000 / $1,000) = 10 steps.
//   Reduction factor = 10 * 5% = 50% (0.50).
//   Remaining factor = 1.0 - 0.50 = 0.50.
//   Allowable credit = round($100.00 * 0.50) = $50.00.
// - Page 22, Line 52: Net tax = $4,856.25 - $50.00 = $4,806.25.
// - Page 22, Line 53 & 59: Withholding = $6,000.00.
// - Page 22, Line 61 & 79: Overpayment / refund = $6,000.00 - $4,806.25 = $1,193.75.
func TestArizonaHighIncomeCreditPhaseout(t *testing.T) {
	in := azInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		210_000_00,
		6_000_00,
		func(i *returnsv1.ReturnInputs) {
			i.Federal.Household.Dependents = []*returnsv1.Dependent{
				azDependent("child", 2016), // Age 9 (< 17)
			}
		},
	)
	az, f := prepareAZ(t, in)

	want(t, "AZ Federal AGI (line 12)", f.GetFederalAdjustedGrossIncome(), 210_000_00)
	want(t, "AZ standard deduction (line 43)", f.GetDeduction(), 15_750_00)
	want(t, "AZ taxable income (line 45)", f.GetTaxableIncome(), 194_250_00)
	want(t, "AZ tax amount (line 46)", f.GetTax(), 4_856_25)
	want(t, "AZ phased-out credit (line 49)", f.GetDependentTaxCredit(), 50_00)
	want(t, "AZ net tax (line 52)", f.GetTaxAfterCredits(), 4_806_25)
	want(t, "AZ overpayment (line 61)", f.GetOverpayment(), 1_193_75)
	want(t, "AZ tax due (line 60)", f.GetTaxDue(), 0)
	complete(t, "AZ", az.GetStatus(), az.GetDiagnostics())
}

// 6. Low income filer qualifying for Family Income Tax Credit:
// Hand calculation cited to 2025 Form 140 Instructions:
// - Page 21, Line 43: Single standard deduction = $15,750.
// - Page 21, Line 44: Taxable income = max($8,000 - $15,750, 0) = $0.00.
// - Page 21, Line 45: Tax before credits = $0.00.
// - Page 23, Line 50: Family Income Tax Credit Worksheet (A.R.S. § 43-1073):
//   Table III (Single): Income $8,000 <= $10,000 limit -> qualifies.
//   1 person * $40 = $40.00, capped at Single limit $120.00.
//   Nonrefundable credit limited to tax liability ($0.00) -> credit applied = $0.00.
// - Page 22, Line 52: Net tax = $0.00.
func TestArizonaLowIncomeFamilyCredit(t *testing.T) {
	in := azInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, 8_000_00, 0)
	az, f := prepareAZ(t, in)

	want(t, "AZ taxable income (line 45)", f.GetTaxableIncome(), 0)
	want(t, "AZ tax amount (line 46)", f.GetTax(), 0)
	want(t, "AZ net tax (line 52)", f.GetTaxAfterCredits(), 0)
	want(t, "AZ overpayment (line 61)", f.GetOverpayment(), 0)
	want(t, "AZ tax due (line 60)", f.GetTaxDue(), 0)
	complete(t, "AZ", az.GetStatus(), az.GetDiagnostics())
}
