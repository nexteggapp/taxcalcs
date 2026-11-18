package states_test

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/money"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
	formtc40 "github.com/nexteggapp/taxprotos/go/us/state/ut/v1/form_tc40"
)

// There is no Go Utah income tax calculator to check these against. Each case is worked by hand from the 2025 Form TC-40 and its
// instructions instead, line by line.

func utInputs(
	status returnsv1.FilingStatusElection,
	wages int64,
	withheld int64,
	dependents []*returnsv1.Dependent,
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
	if status == returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY {
		hh.MaritalStatus = returnsv1.MaritalStatus_MARITAL_STATUS_MARRIED
		hh.Spouse = person("ben", 1984)
	}
	w2 := &formw2.FormW2{
		WagesTipsOtherCompensation: cents(wages),
		FederalTaxWithheld:         cents(wages / 10),
		StateTaxWithholding: []*commonv1.StateTaxWithholding{
			{State: "UT", StateIncome: cents(wages), TaxWithheld: cents(withheld)},
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
			{State: "UT", Inputs: &returnsv1.StateInputs_Ut{Ut: &returnsv1.UtahInputs{}}},
		},
	}
}

func utChild(id string, birthYear int32) *returnsv1.Dependent {
	return &returnsv1.Dependent{
		Id:                              id,
		FirstName:                       id,
		Relationship:                    returnsv1.Relationship_RELATIONSHIP_CHILD,
		DateOfBirth:                     &commonv1.Date{Year: birthYear, Month: 5, Day: 1},
		MonthsLivedWithYou:              int32Ptr(12),
		MainHomeInUsOverHalfYear:        boolPtr(true),
		CitizenOrResidentOfNorthAmerica: boolPtr(true),
		ProvidedOverHalfOwnSupport:      boolPtr(false),
		Tin: &returnsv1.TaxpayerID{
			Kind:               returnsv1.TaxpayerIDKind_TAXPAYER_ID_KIND_SSN,
			ValidForEmployment: boolPtr(true),
		},
	}
}

func prepareUT(t *testing.T, in *returnsv1.ReturnInputs) (*returnsv1.UtahReturn, *formtc40.FormTC40, *returnsv1.ReturnOutputs) {
	t.Helper()
	out := prepare(t, in)
	if len(out.GetStates()) != 1 {
		t.Fatalf("got %d state returns, want 1", len(out.GetStates()))
	}
	ut := out.GetStates()[0].GetUt()
	if ut == nil {
		t.Fatalf("no Utah return: %v", out.GetStates()[0])
	}
	complete(t, "UT", ut.GetStatus(), ut.GetDiagnostics())
	return ut, ut.GetForms().GetFormTc40(), out
}

// Single, $60,000 of wages, $2,500 of Utah tax withheld.
//   - Line 4, 6, 9: federal AGI $60,000.
//   - Line 10: $60,000 x 4.5% = $2,700.
//   - Line 11: no dependents, $0. Line 12: federal standard deduction $15,750.
//   - Line 16: $15,750 x 6% = $945.
//   - Line 17: $18,213 (single). Line 18: $60,000 - $18,213 = $41,787.
//   - Line 19: $41,787 x 1.3% = $543.23, rounded to $543.
//   - Line 20: $945 - $543 = $402. Line 22: $2,700 - $402 = $2,298.
//   - Line 32: $2,298. Line 33: $2,500. Line 42 refund: $202.
func TestUtahSingle(t *testing.T) {
	ut, f, _ := prepareUT(t, utInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, 60_000_00, 2_500_00, nil))

	if f.GetFilingStatus() != formtc40.FilingStatus_FILING_STATUS_SINGLE {
		t.Errorf("filing status = %v, want single", f.GetFilingStatus())
	}
	want(t, "Line 4 federal AGI", f.GetFederalAdjustedGrossIncome(), 60_000_00)
	want(t, "Line 9 Utah taxable income", f.GetUtahTaxableIncome(), 60_000_00)
	want(t, "Line 10 Utah tax", f.GetTaxBeforeCredit(), 2_700_00)
	want(t, "Line 11 personal exemption", f.GetPersonalExemption(), 0)
	want(t, "Line 12 federal deduction", f.GetFederalDeduction(), 15_750_00)
	want(t, "Line 16 initial credit", f.GetInitialCredit(), 945_00)
	want(t, "Line 17 base phase-out", f.GetPhaseOutBase(), 18_213_00)
	want(t, "Line 18 income subject to phase-out", f.GetIncomeSubjectToPhaseOut(), 41_787_00)
	want(t, "Line 19 phase-out", f.GetPhaseOutAmount(), 543_00)
	want(t, "Line 20 taxpayer tax credit", f.GetTaxpayerTaxCredit(), 402_00)
	want(t, "Line 22 Utah income tax", f.GetUtahIncomeTax(), 2_298_00)
	want(t, "Line 32 total tax", f.GetTotalTax(), 2_298_00)
	want(t, "Line 33 withholding", f.GetUtahTaxWithheld(), 2_500_00)
	want(t, "Line 38 payments", f.GetTotalPaymentsAndRefundableCredits(), 2_500_00)
	want(t, "Line 41 total due", f.GetAmountOwed(), 0)
	want(t, "Line 42 refund", f.GetOverpayment(), 202_00)
	want(t, "Summary refund", ut.GetSummary().GetRefund(), 202_00)
	want(t, "Summary tax", ut.GetSummary().GetTotalTax(), 2_298_00)
}

// Married filing jointly, $140,000 of wages, two children aged 4 and 10, $5,000
// withheld.
//   - Box 2a: 2 dependents 16 or younger. 2d: 2.
//   - Line 10: $140,000 x 4.5% = $6,300.
//   - Line 11: 2 x $2,111 = $4,222. Line 12: $31,500. Line 13: $35,722.
//   - Line 16: $35,722 x 6% = $2,143.32, rounded to $2,143.
//   - Line 17: $36,426. Line 18: $103,574. Line 19: $1,346.46, rounded to $1,346.
//   - Line 20: $797. Line 22: $6,300 - $797 = $5,503.
//   - Line 24: the child tax credit for the 4-year-old is $1,000 less 10% of
//     ($140,000 - $54,000), which is nothing; no earned income credit. $0.
//   - Line 32: $5,503. Line 33: $5,000. Line 39 and 41 due: $503.
func TestUtahMarriedFilingJointlyWithChildren(t *testing.T) {
	kids := []*returnsv1.Dependent{utChild("c1", 2021), utChild("c2", 2015)}
	ut, f, _ := prepareUT(t, utInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY, 140_000_00, 5_000_00, kids))

	if got := f.GetQualifyingDependents(); got != 2 {
		t.Errorf("Line 2d = %d, want 2", got)
	}
	want(t, "Line 10 Utah tax", f.GetTaxBeforeCredit(), 6_300_00)
	want(t, "Line 11 personal exemption", f.GetPersonalExemption(), 4_222_00)
	want(t, "Line 12 federal deduction", f.GetFederalDeduction(), 31_500_00)
	want(t, "Line 13 exemption and deduction", f.GetExemptionAndDeduction(), 35_722_00)
	want(t, "Line 16 initial credit", f.GetInitialCredit(), 2_143_00)
	want(t, "Line 17 base phase-out", f.GetPhaseOutBase(), 36_426_00)
	want(t, "Line 19 phase-out", f.GetPhaseOutAmount(), 1_346_00)
	want(t, "Line 20 taxpayer tax credit", f.GetTaxpayerTaxCredit(), 797_00)
	want(t, "Line 22 Utah income tax", f.GetUtahIncomeTax(), 5_503_00)
	want(t, "Line 24 apportionable credits", f.GetApportionableNonrefundableCredits(), 0)
	want(t, "Line 32 total tax", f.GetTotalTax(), 5_503_00)
	want(t, "Line 39 tax due", f.GetTaxDue(), 503_00)
	want(t, "Line 41 total due", f.GetAmountOwed(), 503_00)
	want(t, "Line 42 refund", f.GetOverpayment(), 0)
	want(t, "Summary owed", ut.GetSummary().GetAmountOwed(), 503_00)
}

// Single, $300,000 of wages, $13,000 withheld: the taxpayer tax credit is
// entirely phased out.
//   - Line 10: $13,500. Line 16: $945.
//   - Line 18: $300,000 - $18,213 = $281,787. Line 19: $3,663.23, rounded to $3,663.
//   - Line 20: $0. Line 22: $13,500. Line 41 due: $500.
func TestUtahHighIncome(t *testing.T) {
	ut, f, _ := prepareUT(t, utInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, 300_000_00, 13_000_00, nil))

	want(t, "Line 10 Utah tax", f.GetTaxBeforeCredit(), 13_500_00)
	want(t, "Line 16 initial credit", f.GetInitialCredit(), 945_00)
	want(t, "Line 19 phase-out", f.GetPhaseOutAmount(), 3_663_00)
	want(t, "Line 20 taxpayer tax credit", f.GetTaxpayerTaxCredit(), 0)
	want(t, "Line 22 Utah income tax", f.GetUtahIncomeTax(), 13_500_00)
	want(t, "Line 32 total tax", f.GetTotalTax(), 13_500_00)
	want(t, "Line 41 total due", f.GetAmountOwed(), 500_00)
	want(t, "Summary owed", ut.GetSummary().GetAmountOwed(), 500_00)
}

// Head of household, $22,000 of wages, one child aged 3, $300 withheld. Federal
// AGI is below the $23,625 head of household standard deduction, so the filer
// is a qualified exempt taxpayer (line 21) and line 22 is zero.
func TestUtahQualifiedExempt(t *testing.T) {
	kids := []*returnsv1.Dependent{utChild("c1", 2022)}
	ut, f, _ := prepareUT(t, utInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_HEAD_OF_HOUSEHOLD, 22_000_00, 300_00, kids))

	if !f.GetQualifiedExemptTaxpayer() {
		t.Error("Line 21: want a qualified exempt taxpayer")
	}
	want(t, "Line 10 Utah tax", f.GetTaxBeforeCredit(), 990_00)
	want(t, "Line 22 Utah income tax", f.GetUtahIncomeTax(), 0)
	want(t, "Line 32 total tax", f.GetTotalTax(), 0)
	want(t, "Line 42 refund", f.GetOverpayment(), 300_00)
	want(t, "Summary refund", ut.GetSummary().GetRefund(), 300_00)
}

// Married filing jointly, $50,000 of wages, one child aged 2, $1,000 withheld.
//   - Line 10: $2,250. Line 11: $2,111. Line 13: $33,611.
//   - Line 16: $2,016.66, rounded to $2,017. Line 18: $13,574.
//   - Line 19: $176.46, rounded to $176. Line 20: $1,841. Line 22: $409.
//   - Child tax credit (AT): modified AGI $50,000 is under $54,000, so $1,000.
//   - Earned income credit (AM): 20% of the federal credit, rounded.
//   - Line 24 is more than line 23, so lines 25, 27 and 32 are zero, and the
//     whole $1,000 withheld is refunded.
func TestUtahChildAndEarnedIncomeCredits(t *testing.T) {
	kids := []*returnsv1.Dependent{utChild("c1", 2023)}
	ut, f, out := prepareUT(t, utInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY, 50_000_00, 1_000_00, kids))

	want(t, "Line 16 initial credit", f.GetInitialCredit(), 2_017_00)
	want(t, "Line 19 phase-out", f.GetPhaseOutAmount(), 176_00)
	want(t, "Line 20 taxpayer tax credit", f.GetTaxpayerTaxCredit(), 1_841_00)
	want(t, "Line 22 Utah income tax", f.GetUtahIncomeTax(), 409_00)

	fedEITC := out.GetFederal().GetForms().GetForm_1040().GetEarnedIncomeCredit().GetMinor()
	if fedEITC <= 0 {
		t.Fatalf("want a federal earned income credit, got %d", fedEITC)
	}
	utEITC := money.USCents((fedEITC*20 + 5000) / 10000 * 100)
	want(t, "Line 24 apportionable credits", f.GetApportionableNonrefundableCredits(), 1_000_00+utEITC)
	want(t, "Line 25 tax after credits", f.GetTaxAfterApportionableCredits(), 0)
	want(t, "Line 32 total tax", f.GetTotalTax(), 0)
	want(t, "Line 42 refund", f.GetOverpayment(), 1_000_00)
	want(t, "Summary refund", ut.GetSummary().GetRefund(), 1_000_00)
}
