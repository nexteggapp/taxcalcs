package states_test

import (
	"testing"

	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	formdr0104 "github.com/nexteggapp/taxprotos/go/us/state/co/v1/form_dr0104"
)

// Helper to construct return inputs for Colorado Form DR 0104 tests.
func coTestInputs(
	status returnsv1.FilingStatusElection,
	wages int64,
	withheld int64,
	birthYear int32,
) *returnsv1.ReturnInputs {
	in := inputs(status, wages, withheld)
	in.States = []*returnsv1.StateInputs{
		{
			State: "CO",
			Inputs: &returnsv1.StateInputs_Co{
				Co: &returnsv1.ColoradoInputs{},
			},
		},
	}
	in.Federal.Household.Taxpayer.DateOfBirth.Year = birthYear
	w2 := in.Federal.Documents.W2S[0].Form
	w2.StateTaxWithholding = []*commonv1.StateTaxWithholding{
		{
			State:       "CO",
			StateIncome: cents(wages),
			TaxWithheld: cents(withheld),
		},
	}
	return in
}

func getColoradoReturn(t *testing.T, out *returnsv1.ReturnOutputs) (*returnsv1.ColoradoReturn, *formdr0104.FormDR0104) {
	t.Helper()
	if len(out.GetStates()) == 0 {
		t.Fatal("expected at least one state return")
	}
	sr := out.GetStates()[0]
	if sr.GetState() != "CO" {
		t.Fatalf("expected state CO, got %s", sr.GetState())
	}
	ret := sr.GetCo()
	if ret == nil {
		t.Fatal("expected non-nil ColoradoReturn")
	}
	f := ret.GetForms().GetFormDr0104()
	if f == nil {
		t.Fatal("expected non-nil FormDR0104")
	}
	return ret, f
}

// 1. Single household:
// 1. Single household:
// Filer age 35 (born 1990), single, $55,000 wages, $2,000 Colorado withholding.
//   - Federal AGI: $55,000.00
//   - 2025 Federal standard deduction: $15,750.00
//   - Line 1 (Federal Taxable Income): $55,000.00 - $15,750.00 = $39,250.00
//   - Line 10 (Total Income): $39,250.00
//   - Line 11 (Subtractions): $0.00
//   - Line 12 (Colorado Taxable Income): $39,250.00
//   - Line 13 (Colorado Tax): 2025 Income Tax Table (Book 104, page 35).
//     Bracket $39,200 - $39,300, midpoint $39,250 * 0.044 = $1,727.00.
//   - Line 16 (Total Tax): $1,727.00
//   - Line 21 (Net Income Tax): $1,727.00
//   - Line 23 (Net Tax and Required Repayment): $1,727.00
//   - Line 24 (Withholding): $2,000.00
//   - Line 37 (Modified AGI for TABOR): $55,000.00
//   - Line 38 (TABOR State Sales Tax Refund): Tier 2 ($52,001 - $105,000) Single: $25.00
//     (Book 104, page 14, 42).
//   - Line 39 (Total Payments & Credits): $2,000.00 + $25.00 = $2,025.00
//   - Line 40 (Overpayment): $2,025.00 - $1,727.00 = $298.00
//   - Line 42 (Refund): $298.00
//   - Line 47 (Amount Owed): $0.00
func TestColoradoSingleHousehold(t *testing.T) {
	in := coTestInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, 55_000_00, 2_000_00, 1990)
	out := prepare(t, in)
	ret, f := getColoradoReturn(t, out)

	complete(t, "CO", ret.GetStatus(), ret.GetDiagnostics())
	want(t, "Line 1 federal taxable income", f.GetFederalTaxableIncome(), 39_250_00)
	want(t, "Line 10 total income", f.GetIncomeAfterAdditions(), 39_250_00)
	want(t, "Line 11 total subtractions", f.GetTotalSubtractions(), 0)
	want(t, "Line 12 Colorado taxable income", f.GetColoradoTaxableIncome(), 39_250_00)
	want(t, "Line 13 Colorado tax", f.GetTax(), 1_727_00)
	want(t, "Line 16 total tax", f.GetTaxBeforeCredits(), 1_727_00)
	want(t, "Line 21 net tax", f.GetNetIncomeTax(), 1_727_00)
	want(t, "Line 23 total tax and use tax", f.GetNetTax(), 1_727_00)
	want(t, "Line 24 Colorado tax withheld", f.GetColoradoTaxWithheld(), 2_000_00)
	want(t, "Line 39 total payments and credits", f.GetTotalPaymentsAndTaborRefund(), 2_025_00)
	want(t, "Line 40 overpayment", f.GetOverpayment(), 298_00)
	want(t, "Summary refund", ret.GetSummary().GetRefund(), 298_00)
	want(t, "Line 47 tax due", f.GetNetTaxDue(), 0)
}

// 2. Married Filing Jointly household:
// Taxpayer and spouse age 40 (born 1985), MFJ, $90,000 wages, $3,000 Colorado withholding.
//   - Federal AGI: $90,000.00
//   - 2025 Federal standard deduction for MFJ: $31,500.00
//   - Line 1 (Federal Taxable Income): $90,000.00 - $31,500.00 = $58,500.00
//   - Line 10 (Total Income): $58,500.00
//   - Line 12 (Colorado Taxable Income): $58,500.00
//   - Line 13 (Colorado Tax): Flat rate for taxable incomes over $50,000 (Book 104, page 35).
//     $58,500 * 4.4% = $2,574.00.
//   - Line 23 (Net Tax): $2,574.00
//   - Line 24 (Withholding): $3,000.00
//   - Line 37 (Modified AGI for TABOR): $90,000.00
//   - Line 38 (TABOR State Sales Tax Refund): Tier 2 ($52,001 - $105,000) Joint: $50.00
//     (Book 104, page 14, 42).
//   - Line 39 (Total Payments & Credits): $3,000.00 + $50.00 = $3,050.00
//   - Line 40 (Overpayment): $3,050.00 - $2,574.00 = $476.00
//   - Line 42 (Refund): $476.00
//   - Line 47 (Amount Owed): $0.00
func TestColoradoMarriedFilingJointly(t *testing.T) {
	in := coTestInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY, 90_000_00, 3_000_00, 1985)
	in.Federal.Household.Spouse.DateOfBirth.Year = 1985
	out := prepare(t, in)
	ret, f := getColoradoReturn(t, out)

	complete(t, "CO", ret.GetStatus(), ret.GetDiagnostics())
	want(t, "Line 1 federal taxable income", f.GetFederalTaxableIncome(), 58_500_00)
	want(t, "Line 12 Colorado taxable income", f.GetColoradoTaxableIncome(), 58_500_00)
	want(t, "Line 13 Colorado tax", f.GetTax(), 2_574_00)
	want(t, "Line 23 net tax", f.GetNetTax(), 2_574_00)
	want(t, "Line 24 withholding", f.GetColoradoTaxWithheld(), 3_000_00)
	want(t, "Line 39 total payments", f.GetTotalPaymentsAndTaborRefund(), 3_050_00)
	want(t, "Line 40 overpayment", f.GetOverpayment(), 476_00)
	want(t, "Summary refund", ret.GetSummary().GetRefund(), 476_00)
	want(t, "Line 47 tax due", f.GetNetTaxDue(), 0)
}

// 3. Dependent Filer:
// Single filer, age 17 (born 2008), claimed as dependent on another return.
// Wages: $3,000.00, Colorado withholding: $100.00.
//   - Federal standard deduction for dependent: $3,000 + $450 = $3,450.00
//   - Federal taxable income (Line 1): max(0, $3,000 - $3,450) = $0.00
//   - Line 12 (Colorado Taxable Income): $0.00
//   - Line 13 (Colorado Tax): $0.00
//   - Line 24 (Withholding): $100.00
//   - Line 37 (Modified AGI for TABOR): $3,000.00
//   - Line 38 (TABOR Refund): Under 18 as of Jan 1, 2025, but eligible because
//     claiming refund of Colorado wage withholding on W-2 (Book 104, page 14).
//     Tier 1 (<= $52,000) Single: $19.00 (Book 104, page 14, 42).
//   - Line 39 (Total Payments & Credits): $100.00 + $19.00 = $119.00
//   - Line 40 / 42 (Overpayment / Refund): $119.00
//   - Line 47 (Amount Owed): $0.00
func TestColoradoDependentFiler(t *testing.T) {
	in := coTestInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, 3_000_00, 100_00, 2008)
	in.Federal.Household.Taxpayer.CanBeClaimedAsDependent = boolPtr(true)
	out := prepare(t, in)
	ret, f := getColoradoReturn(t, out)

	complete(t, "CO", ret.GetStatus(), ret.GetDiagnostics())
	want(t, "Line 1 federal taxable income", f.GetFederalTaxableIncome(), 0)
	want(t, "Line 12 Colorado taxable income", f.GetColoradoTaxableIncome(), 0)
	want(t, "Line 13 Colorado tax", f.GetTax(), 0)
	want(t, "Line 24 withholding", f.GetColoradoTaxWithheld(), 100_00)
	want(t, "Line 39 total payments and TABOR", f.GetTotalPaymentsAndTaborRefund(), 119_00)
	want(t, "Line 40 overpayment", f.GetOverpayment(), 119_00)
	want(t, "Summary refund", ret.GetSummary().GetRefund(), 119_00)
	want(t, "Line 47 tax due", f.GetNetTaxDue(), 0)
}

// 4. High Income household:
// 4. High Income household:
// 4. High Income household:
// Single filer, age 45 (born 1980), wages $450,000.00, Colorado withholding $18,000.00.
// Elects standard deduction on federal return.
//   - Federal AGI: $450,000.00
//   - 2025 Federal standard deduction: $15,750.00
//   - Line 1 (Federal Taxable Income): $450,000.00 - $15,750.00 = $434,250.00
//   - Line 2 (State Income Tax Addback): $0.00 (standard deduction elected)
//   - Line 4 (Deduction Addback): AGI $450,000 exceeds $300,000 threshold.
//     Standard deduction $15,750 exceeds single limit $12,000 by $3,750.
//     Required addback: $3,750.00 (Colo. Rev. Stat. § 39-22-104(3)(p), Book 104, page 8-9).
//   - Total additions: $3,750.00
//   - Line 10 (Total Income): $434,250.00 + $3,750.00 = $438,000.00
//   - Line 12 (Colorado Taxable Income): $438,000.00
//   - Line 13 (Colorado Tax): $438,000 * 4.4% = $19,272.00 (Book 104, page 35).
//   - Line 23 (Net Tax): $19,272.00
//   - Line 24 (Withholding): $18,000.00
//   - Line 37 (Modified AGI for TABOR): $450,000.00
//   - Line 38 (TABOR State Sales Tax Refund): Tier 6 ($299,001 or more) Single: $59.00
//     (Book 104, page 14, 42).
//   - Line 39 (Total Payments & Credits): $18,000.00 + $59.00 = $18,059.00
//   - Line 40 (Overpayment): $0.00
//   - Line 43 / 47 (Net Tax Due / Amount You Owe): $19,272.00 - $18,059.00 = $1,213.00
func TestColoradoHighIncome(t *testing.T) {
	in := coTestInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, 450_000_00, 18_000_00, 1980)
	in.Federal.Elections.DeductionMethod = returnsv1.DeductionMethodElection_DEDUCTION_METHOD_ELECTION_STANDARD
	out := prepare(t, in)
	ret, f := getColoradoReturn(t, out)

	complete(t, "CO", ret.GetStatus(), ret.GetDiagnostics())
	want(t, "Line 1 federal taxable income", f.GetFederalTaxableIncome(), 434_250_00)
	want(t, "Line 10 total income with Line 4 addback", f.GetIncomeAfterAdditions(), 438_000_00)
	want(t, "Line 12 Colorado taxable income", f.GetColoradoTaxableIncome(), 438_000_00)
	want(t, "Line 13 Colorado tax", f.GetTax(), 19_272_00)
	want(t, "Line 23 total tax", f.GetNetTax(), 19_272_00)
	want(t, "Line 24 withholding", f.GetColoradoTaxWithheld(), 18_000_00)
	want(t, "Line 39 total payments and TABOR", f.GetTotalPaymentsAndTaborRefund(), 18_059_00)
	want(t, "Line 40 overpayment", f.GetOverpayment(), 0)
	want(t, "Line 43 / 47 amount you owe", f.GetNetTaxDue(), 1_213_00)
	want(t, "Summary amount owed", ret.GetSummary().GetAmountOwed(), 1_213_00)
}

// 5. Low Income household:
// Single filer, age 25 (born 2000), wages $12,000.00, Colorado withholding $0.
//   - Federal AGI: $12,000.00
//   - Federal standard deduction: $15,000.00
//   - Line 1 (Federal Taxable Income): $0.00
//   - Line 12 (Colorado Taxable Income): $0.00
//   - Line 13 (Colorado Tax): $0.00
//   - Line 24 (Withholding): $0.00
//   - Line 37 (Modified AGI for TABOR): $12,000.00
//   - Line 38 (TABOR Refund): Age 25 >= 18 as of Jan 1, 2025, so eligible for TABOR refund
//     (Book 104, page 14). Tier 1 (<= $52,000) Single: $19.00 (Book 104, page 14, 42).
//   - Line 39 (Total Payments & Credits): $19.00
//   - Line 40 / 42 (Overpayment / Refund): $19.00
//   - Line 47 (Amount Owed): $0.00
func TestColoradoLowIncome(t *testing.T) {
	in := coTestInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, 12_000_00, 0, 2000)
	out := prepare(t, in)
	ret, f := getColoradoReturn(t, out)

	complete(t, "CO", ret.GetStatus(), ret.GetDiagnostics())
	want(t, "Line 1 federal taxable income", f.GetFederalTaxableIncome(), 0)
	want(t, "Line 12 Colorado taxable income", f.GetColoradoTaxableIncome(), 0)
	want(t, "Line 13 Colorado tax", f.GetTax(), 0)
	want(t, "Line 24 withholding", f.GetColoradoTaxWithheld(), 0)
	want(t, "Line 39 total payments and TABOR", f.GetTotalPaymentsAndTaborRefund(), 19_00)
	want(t, "Line 40 overpayment", f.GetOverpayment(), 19_00)
	want(t, "Summary refund", ret.GetSummary().GetRefund(), 19_00)
	want(t, "Line 47 tax due", f.GetNetTaxDue(), 0)
}

// 6. Unsupported year:
// Colorado rules return RETURN_STATUS_INCOMPLETE when an unsupported year is requested.
func TestColoradoUnsupportedYear(t *testing.T) {
	in := coTestInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, 50_000_00, 1_000_00, 1990)
	in.TaxYear = 2024
	out := prepare(t, in)
	if len(out.GetStates()) == 0 {
		t.Fatal("expected at least one state return")
	}
	sr := out.GetStates()[0]
	if sr.GetState() != "CO" {
		t.Fatalf("expected state CO, got %s", sr.GetState())
	}
	ret := sr.GetCo()
	if ret == nil {
		t.Fatal("expected non-nil ColoradoReturn")
	}

	if ret.GetStatus() != returnsv1.ReturnStatus_RETURN_STATUS_INCOMPLETE {
		t.Errorf("status %s, want INCOMPLETE", ret.GetStatus())
	}
	if len(ret.GetDiagnostics()) == 0 || ret.GetDiagnostics()[0].GetCode() != "CO_NO_RULES_FOR_YEAR" {
		t.Errorf("expected CO_NO_RULES_FOR_YEAR diagnostic, got %v", ret.GetDiagnostics())
	}
}
