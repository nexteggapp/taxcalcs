package states_test

import (
	"testing"

	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
)

func ncInputs(
	status returnsv1.FilingStatusElection,
	wages int64,
	withheld int64,
	dependents []*returnsv1.Dependent,
	spouseItemizes bool,
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
		Taxpayer:      person("ana", 1985),
		Dependents:    dependents,
	}
	if status == returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY ||
		status == returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_SEPARATELY {
		hh.MaritalStatus = returnsv1.MaritalStatus_MARITAL_STATUS_MARRIED
		hh.Spouse = person("ben", 1984)
		if spouseItemizes {
			hh.SpouseItemizesSeparately = boolPtr(true)
		}
	}
	w2 := &formw2.FormW2{
		WagesTipsOtherCompensation: cents(wages),
		FederalTaxWithheld:         cents(wages / 10),
		StateTaxWithholding: []*commonv1.StateTaxWithholding{
			{
				State:       "NC",
				StateIncome: cents(wages),
				TaxWithheld: cents(withheld),
			},
		},
	}
	return &returnsv1.ReturnInputs{
		SchemaVersion: 1,
		TaxYear:       2025,
		Federal: &returnsv1.Federal1040Inputs{
			Household: hh,
			Elections: &returnsv1.Elections{FilingStatus: status},
			Documents: &returnsv1.Documents{
				W2S: []*returnsv1.W2Document{{Id: "w2-1", Person: "ana", Form: w2}},
			},
		},
		States: []*returnsv1.StateInputs{
			{State: "NC", Inputs: &returnsv1.StateInputs_Nc{Nc: &returnsv1.NorthCarolinaInputs{}}},
		},
	}
}

func ncChild(id string, birthYear int32) *returnsv1.Dependent {
	return &returnsv1.Dependent{
		Id:                              id,
		FirstName:                       id,
		Relationship:                    returnsv1.Relationship_RELATIONSHIP_CHILD,
		DateOfBirth:                     &commonv1.Date{Year: birthYear, Month: 5, Day: 1},
		MonthsLivedWithYou:              int32Ptr(12),
		CitizenOrResidentOfNorthAmerica: boolPtr(true),
		ProvidedOverHalfOwnSupport:      boolPtr(false),
		Tin: &returnsv1.TaxpayerID{
			Kind:               returnsv1.TaxpayerIDKind_TAXPAYER_ID_KIND_SSN,
			ValidForEmployment: boolPtr(true),
		},
	}
}

// Case 1: Single filer with moderate income ($45,000 W-2).
// Hand calculation:
//   - Federal AGI (Form D-400 Line 6): $45,000.00
//   - NC Standard Deduction (Form D-401 page 14): $12,750.00
//   - Child Deduction (Line 10b): $0.00
//   - NC Taxable Income (Line 14): $45,000.00 - $12,750.00 = $32,250.00
//   - NC Income Tax (Line 15, 4.25% flat rate, Form D-401 page 14):
//     $32,250.00 * 0.0425 = $1,370.625 -> rounds to $1,371.00 (Form D-401 page 4)
//   - NC Tax Withheld (Line 20a): $1,500.00
//   - Total Payments (Line 23): $1,500.00
//   - Overpayment / Refund (Line 28 & Line 34): $1,500.00 - $1,371.00 = $129.00
func TestNorthCarolinaSingleModerate(t *testing.T) {
	in := ncInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, 45_000_00, 1_500_00, nil, false)
	out := prepare(t, in)
	if len(out.GetStates()) != 1 {
		t.Fatalf("got %d states, want 1", len(out.GetStates()))
	}
	nc := out.GetStates()[0].GetNc()
	if nc == nil {
		t.Fatalf("NC return is nil: %v", out.GetStates()[0])
	}
	complete(t, "NC", nc.GetStatus(), nc.GetDiagnostics())

	f := nc.GetForms().GetFormD400()
	want(t, "Line 6 Fed AGI", f.GetFederalAdjustedGrossIncome(), 45_000_00)
	want(t, "Line 10b Child deduction", f.GetChildDeduction(), 0)
	want(t, "Line 11 NC Standard deduction", f.GetDeduction(), 12_750_00)
	want(t, "Line 12a Total deductions", f.GetTotalDeductions(), 12_750_00)
	want(t, "Line 14 NC Taxable income", f.GetTaxableIncome(), 32_250_00)
	want(t, "Line 15 NC Income tax", f.GetIncomeTax(), 1_371_00)
	want(t, "Line 19 Total tax", f.GetTotalTax(), 1_371_00)
	want(t, "Line 20a NC Withheld", f.GetTaxpayerTaxWithheld(), 1_500_00)
	want(t, "Line 23 Total payments", f.GetTotalPayments(), 1_500_00)
	want(t, "Line 28 Overpayment", f.GetOverpayment(), 129_00)
	want(t, "Line 26a Tax due", f.GetTaxDue(), 0)
	want(t, "Summary refund", nc.GetSummary().GetRefund(), 129_00)
}

// Case 2: Married Filing Jointly with 2 qualifying children and $95,000 income.
// Hand calculation:
//   - Federal AGI (Line 6): $95,000.00
//   - NC Standard Deduction (Form D-401 page 14): $25,500.00
//   - Qualifying Children (Line 10a): 2
//   - Child Deduction per child (Form D-401 page 13, Child Deduction Table):
//     MFJ with federal AGI in ($80,000, $100,000] bracket = $1,500.00 per child.
//   - Total Child Deduction (Line 10b): 2 * $1,500.00 = $3,000.00
//   - Total Deductions (Line 12a): $25,500.00 + $3,000.00 = $28,500.00
//   - NC Taxable Income (Line 14): $95,000.00 - $28,500.00 = $66,500.00
//   - NC Income Tax (Line 15, 4.25%, Form D-401 page 14):
//     $66,500.00 * 0.0425 = $2,826.25 -> rounds to $2,826.00
//   - NC Tax Withheld (Line 20a): $3,000.00
//   - Overpayment / Refund (Line 28 & Line 34): $3,000.00 - $2,826.00 = $174.00
func TestNorthCarolinaMarriedFilingJointlyWithChildren(t *testing.T) {
	children := []*returnsv1.Dependent{
		ncChild("c1", 2016),
		ncChild("c2", 2018),
	}
	in := ncInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY, 95_000_00, 3_000_00, children, false)
	out := prepare(t, in)
	nc := out.GetStates()[0].GetNc()
	complete(t, "NC", nc.GetStatus(), nc.GetDiagnostics())

	f := nc.GetForms().GetFormD400()
	want(t, "Line 6 Fed AGI", f.GetFederalAdjustedGrossIncome(), 95_000_00)
	want(t, "Line 10b Child deduction", f.GetChildDeduction(), 3_000_00)
	want(t, "Line 11 NC Standard deduction", f.GetDeduction(), 25_500_00)
	want(t, "Line 12a Total deductions", f.GetTotalDeductions(), 28_500_00)
	want(t, "Line 14 NC Taxable income", f.GetTaxableIncome(), 66_500_00)
	want(t, "Line 15 NC Income tax", f.GetIncomeTax(), 2_826_00)
	want(t, "Line 19 Total tax", f.GetTotalTax(), 2_826_00)
	want(t, "Line 20a NC Withheld", f.GetTaxpayerTaxWithheld(), 3_000_00)
	want(t, "Line 28 Overpayment", f.GetOverpayment(), 174_00)
	want(t, "Summary refund", nc.GetSummary().GetRefund(), 174_00)
}

// Case 3: Head of Household with low income ($28,000 W-2) and 1 child.
// Hand calculation:
//   - Federal AGI (Line 6): $28,000.00
//   - NC Standard Deduction (Form D-401 page 14): $19,125.00
//   - Child Deduction per child (Form D-401 page 13, Child Deduction Table):
//     HOH with federal AGI up to $30,000 = $3,000.00 maximum per child.
//   - Total Child Deduction (Line 10b): 1 * $3,000.00 = $3,000.00
//   - Total Deductions (Line 12a): $19,125.00 + $3,000.00 = $22,125.00
//   - NC Taxable Income (Line 14): $28,000.00 - $22,125.00 = $5,875.00
//   - NC Income Tax (Line 15, 4.25%, Form D-401 page 14):
//     $5,875.00 * 0.0425 = $249.6875 -> rounds to $250.00
//   - NC Tax Withheld (Line 20a): $200.00
//   - Tax Due (Line 26a): $250.00 - $200.00 = $50.00
//   - Overpayment (Line 28): $0.00
func TestNorthCarolinaHeadOfHouseholdLowIncome(t *testing.T) {
	children := []*returnsv1.Dependent{
		ncChild("kid", 2020),
	}
	in := ncInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_HEAD_OF_HOUSEHOLD, 28_000_00, 200_00, children, false)
	out := prepare(t, in)
	nc := out.GetStates()[0].GetNc()
	complete(t, "NC", nc.GetStatus(), nc.GetDiagnostics())

	f := nc.GetForms().GetFormD400()
	want(t, "Line 6 Fed AGI", f.GetFederalAdjustedGrossIncome(), 28_000_00)
	want(t, "Line 10b Child deduction", f.GetChildDeduction(), 3_000_00)
	want(t, "Line 11 NC Standard deduction", f.GetDeduction(), 19_125_00)
	want(t, "Line 12a Total deductions", f.GetTotalDeductions(), 22_125_00)
	want(t, "Line 14 NC Taxable income", f.GetTaxableIncome(), 5_875_00)
	want(t, "Line 15 NC Income tax", f.GetIncomeTax(), 250_00)
	want(t, "Line 19 Total tax", f.GetTotalTax(), 250_00)
	want(t, "Line 20a NC Withheld", f.GetTaxpayerTaxWithheld(), 200_00)
	want(t, "Line 26a Tax due", f.GetTaxDue(), 50_00)
	want(t, "Line 28 Overpayment", f.GetOverpayment(), 0)
	want(t, "Summary amount owed", nc.GetSummary().GetAmountOwed(), 50_00)
}

// Case 4: High income single filer ($250,000 W-2) with 1 dependent child.
// Hand calculation:
//   - Federal AGI (Line 6): $250,000.00
//   - NC Standard Deduction (Form D-401 page 14): $12,750.00
//   - Child Deduction (Form D-401 page 13, Child Deduction Table):
//     Single with federal AGI over $70,000 = $0.00.
//   - Total Deductions (Line 12a): $12,750.00
//   - NC Taxable Income (Line 14): $250,000.00 - $12,750.00 = $237,250.00
//   - NC Income Tax (Line 15, 4.25%, Form D-401 page 14):
//     $237,250.00 * 0.0425 = $10,083.125 -> rounds to $10,083.00
//   - NC Tax Withheld (Line 20a): $11,000.00
//   - Overpayment / Refund (Line 28 & Line 34): $11,000.00 - $10,083.00 = $917.00
func TestNorthCarolinaHighIncomeSingle(t *testing.T) {
	children := []*returnsv1.Dependent{
		ncChild("kid", 2017),
	}
	in := ncInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, 250_000_00, 11_000_00, children, false)
	out := prepare(t, in)
	nc := out.GetStates()[0].GetNc()
	complete(t, "NC", nc.GetStatus(), nc.GetDiagnostics())

	f := nc.GetForms().GetFormD400()
	want(t, "Line 6 Fed AGI", f.GetFederalAdjustedGrossIncome(), 250_000_00)
	want(t, "Line 10b Child deduction fully phased out", f.GetChildDeduction(), 0)
	want(t, "Line 11 NC Standard deduction", f.GetDeduction(), 12_750_00)
	want(t, "Line 12a Total deductions", f.GetTotalDeductions(), 12_750_00)
	want(t, "Line 14 NC Taxable income", f.GetTaxableIncome(), 237_250_00)
	want(t, "Line 15 NC Income tax", f.GetIncomeTax(), 10_083_00)
	want(t, "Line 19 Total tax", f.GetTotalTax(), 10_083_00)
	want(t, "Line 20a NC Withheld", f.GetTaxpayerTaxWithheld(), 11_000_00)
	want(t, "Line 28 Overpayment", f.GetOverpayment(), 917_00)
	want(t, "Summary refund", nc.GetSummary().GetRefund(), 917_00)
}

// Case 5: Low income single filer below standard deduction ($8,000 W-2).
// Hand calculation:
//   - Federal AGI (Line 6): $8,000.00
//   - NC Standard Deduction (Form D-401 page 14): $12,750.00
//   - NC Taxable Income (Line 14): $8,000.00 - $12,750.00 = -$4,750.00 -> clamped to $0.00
//     (Form D-401 page 14: "If North Carolina taxable income is zero or less, enter a zero on Line 15")
//   - NC Income Tax (Line 15): $0.00
//   - NC Tax Withheld (Line 20a): $150.00
//   - Overpayment / Refund (Line 28 & Line 34): $150.00
func TestNorthCarolinaLowIncomeBelowDeduction(t *testing.T) {
	in := ncInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, 8_000_00, 150_00, nil, false)
	out := prepare(t, in)
	nc := out.GetStates()[0].GetNc()
	complete(t, "NC", nc.GetStatus(), nc.GetDiagnostics())

	f := nc.GetForms().GetFormD400()
	want(t, "Line 6 Fed AGI", f.GetFederalAdjustedGrossIncome(), 8_000_00)
	want(t, "Line 11 NC Standard deduction", f.GetDeduction(), 12_750_00)
	want(t, "Line 14 NC Taxable income zero floor", f.GetTaxableIncome(), 0)
	want(t, "Line 15 NC Income tax", f.GetIncomeTax(), 0)
	want(t, "Line 19 Total tax", f.GetTotalTax(), 0)
	want(t, "Line 20a NC Withheld", f.GetTaxpayerTaxWithheld(), 150_00)
	want(t, "Line 28 Overpayment", f.GetOverpayment(), 150_00)
	want(t, "Summary refund", nc.GetSummary().GetRefund(), 150_00)
}

// Case 6: Married Filing Separately with spouse itemizing deductions.
// Hand calculation:
//   - Under N.C. Gen. Stat. § 105-153.5(a)(1) & Form D-401 page 14:
//     If spouse claims itemized deductions, the standard deduction is $0.
//   - Federal AGI (Line 6): $50,000.00
//   - NC Standard Deduction (Line 11): $0.00
//   - NC Taxable Income (Line 14): $50,000.00
//   - NC Income Tax (Line 15, 4.25%): $50,000.00 * 0.0425 = $2,125.00
//   - NC Tax Withheld (Line 20a): $2,500.00
//   - Overpayment / Refund (Line 28): $2,500.00 - $2,125.00 = $375.00
func TestNorthCarolinaMFSWithSpouseItemizing(t *testing.T) {
	in := ncInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_SEPARATELY, 50_000_00, 2_500_00, nil, true)
	out := prepare(t, in)
	nc := out.GetStates()[0].GetNc()
	complete(t, "NC", nc.GetStatus(), nc.GetDiagnostics())

	f := nc.GetForms().GetFormD400()
	want(t, "Line 6 Fed AGI", f.GetFederalAdjustedGrossIncome(), 50_000_00)
	want(t, "Line 11 Standard deduction is 0 when spouse itemizes", f.GetDeduction(), 0)
	want(t, "Line 14 NC Taxable income", f.GetTaxableIncome(), 50_000_00)
	want(t, "Line 15 NC Income tax", f.GetIncomeTax(), 2_125_00)
	want(t, "Line 28 Overpayment", f.GetOverpayment(), 375_00)
}
