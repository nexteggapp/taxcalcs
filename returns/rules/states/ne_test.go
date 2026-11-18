package states_test

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
	form1040n "github.com/nexteggapp/taxprotos/go/us/state/ne/v1/form_1040n"
)

// There is no Go income tax calculator for Nebraska, so these households are worked
// by hand from the 2025 Form 1040N, its instructions (the 2025 Nebraska
// Individual Income Tax Booklet) and the 2025 Tax Calculation Schedule, and
// the rules are tested against that working.

func neChild(id string, birthYear int32) *returnsv1.Dependent {
	return &returnsv1.Dependent{
		Id:        id,
		FirstName: id,
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

// neInputs is a Nebraska household with one W-2: the taxpayer, and a spouse
// on a married return, born in birthYear.
func neInputs(
	status returnsv1.FilingStatusElection,
	birthYear int32,
	wages int64,
	withheld int64,
	dependents ...*returnsv1.Dependent,
) *returnsv1.ReturnInputs {
	person := func(id string) *returnsv1.Person {
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
		Taxpayer:      person("taxpayer"),
		Dependents:    dependents,
	}
	if status == returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY {
		hh.MaritalStatus = returnsv1.MaritalStatus_MARITAL_STATUS_MARRIED
		hh.Spouse = person("spouse")
	}
	w2 := &formw2.FormW2{
		WagesTipsOtherCompensation: cents(wages),
		FederalTaxWithheld:         cents(wages / 10),
		StateTaxWithholding: []*commonv1.StateTaxWithholding{
			{State: "NE", StateIncome: cents(wages), TaxWithheld: cents(withheld)},
		},
	}
	return &returnsv1.ReturnInputs{
		SchemaVersion: 1,
		TaxYear:       2025,
		Federal: &returnsv1.Federal1040Inputs{
			Household: hh,
			Elections: &returnsv1.Elections{FilingStatus: status},
			Documents: &returnsv1.Documents{
				W2S: []*returnsv1.W2Document{{Id: "w2-ne", Person: "taxpayer", Form: w2}},
			},
		},
		States: []*returnsv1.StateInputs{{
			State:  "NE",
			Inputs: &returnsv1.StateInputs_Ne{Ne: &returnsv1.NebraskaInputs{}},
		}},
	}
}

func prepareNE(t *testing.T, in *returnsv1.ReturnInputs) (*returnsv1.NebraskaReturn, *form1040n.Form1040N) {
	t.Helper()
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine: %v", err)
	}
	out, err := eng.Prepare(in)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if len(out.GetStates()) == 0 || out.GetStates()[0].GetNe() == nil {
		t.Fatalf("expected a Nebraska return, got %v", out.GetStates())
	}
	ne := out.GetStates()[0].GetNe()
	f := ne.GetForms().GetForm_1040N()
	if f == nil {
		t.Fatalf("expected Form 1040N in the Nebraska return, got %v", ne.GetForms())
	}
	return ne, f
}

// A single filer with $60,000 of wages and $2,500 withheld.
//
//   - Line 4: one exemption, for the filer (instructions, line 4a).
//   - Line 5: federal AGI $60,000.
//   - Line 6: single, no boxes on line 2a: $8,600 (instructions page 13,
//     Nebraska Standard Deduction Chart). Lines 7-9 are blank; line 10 $8,600.
//   - Line 14: $60,000 - $8,600 = $51,400.
//   - Line 15: Tax Calculation Schedule, single, over $38,870:
//     $1,543.28 + 5.20% x ($51,400 - $38,870) = $1,543.28 + $651.56
//     = $2,194.84, entered as $2,195 (whole dollars, instructions page 3).
//   - Line 17: $2,195.
//   - Line 18: $171 x 1 = $171 (instructions, line 18).
//   - Line 35: $2,195 - $171 = $2,024.
//   - Line 36 and 53: $2,500 withheld.
//   - Line 60 and 63: $2,500 - $2,024 = $476 refunded.
func TestNebraskaSingle(t *testing.T) {
	ne, f := prepareNE(t, neInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, 1985, 60_000_00, 2_500_00))

	if f.GetTotalExemptions() != 1 {
		t.Errorf("line 4 = %d, want 1", f.GetTotalExemptions())
	}
	want(t, "line 5", f.GetFederalAdjustedGrossIncome(), 60_000_00)
	want(t, "line 6", f.GetStandardDeduction(), 8_600_00)
	want(t, "line 10", f.GetDeduction(), 8_600_00)
	want(t, "line 14", f.GetTaxableIncome(), 51_400_00)
	want(t, "line 15", f.GetIncomeTax(), 2_195_00)
	want(t, "line 18", f.GetPersonalExemptionCredit(), 171_00)
	want(t, "line 35", f.GetTaxAfterNonrefundableCredits(), 2_024_00)
	want(t, "line 53", f.GetTotalPayments(), 2_500_00)
	want(t, "line 59", f.GetAmountOwed(), 0)
	want(t, "line 63", f.GetRefund(), 476_00)
	want(t, "summary tax", ne.GetSummary().GetTotalTax(), 2_024_00)
	want(t, "summary refund", ne.GetSummary().GetRefund(), 476_00)
	complete(t, "NE", ne.GetStatus(), ne.GetDiagnostics())
}

// Married filing jointly with two children, $100,000 of wages and $3,000
// withheld.
//
//   - Line 4: 1 + 1 + 2 dependents = 4 exemptions (instructions, line 4).
//   - Line 6: married filing jointly, no boxes: $17,200 (chart, page 13).
//   - Line 14: $100,000 - $17,200 = $82,800.
//   - Line 15: schedule, married filing jointly, over $77,730:
//     $3,086.10 + 5.20% x ($82,800 - $77,730) = $3,086.10 + $263.64
//     = $3,349.74, entered as $3,350.
//   - Line 18: $171 x 4 = $684.
//   - Line 35: $3,350 - $684 = $2,666.
//   - Line 44: no federal earned income credit at this income, so none.
//   - Line 63: $3,000 - $2,666 = $334 refunded.
func TestNebraskaMarriedFilingJointlyWithChildren(t *testing.T) {
	ne, f := prepareNE(t, neInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY, 1985, 100_000_00, 3_000_00,
		neChild("child-a", 2015), neChild("child-b", 2018)))

	if f.GetTotalExemptions() != 4 {
		t.Errorf("line 4 = %d, want 4", f.GetTotalExemptions())
	}
	want(t, "line 6", f.GetStandardDeduction(), 17_200_00)
	want(t, "line 14", f.GetTaxableIncome(), 82_800_00)
	want(t, "line 15", f.GetIncomeTax(), 3_350_00)
	want(t, "line 18", f.GetPersonalExemptionCredit(), 684_00)
	want(t, "line 35", f.GetTaxAfterNonrefundableCredits(), 2_666_00)
	want(t, "line 44", f.GetEarnedIncomeCredit(), 0)
	want(t, "line 53", f.GetTotalPayments(), 3_000_00)
	want(t, "line 63", f.GetRefund(), 334_00)
	want(t, "summary tax", ne.GetSummary().GetTotalTax(), 2_666_00)
	complete(t, "NE", ne.GetStatus(), ne.GetDiagnostics())
}

// A high-income single filer: $250,000 of wages, $12,000 withheld.
//
//   - Line 6: $8,600. Line 14: $250,000 - $8,600 = $241,400.
//   - Line 15: $1,543.28 + 5.20% x ($241,400 - $38,870) = $1,543.28 +
//     $10,531.56 = $12,074.84, entered as $12,075. Nebraska does not phase out
//     the personal exemption credit, so line 18 is $171.
//   - Line 35: $12,075 - $171 = $11,904.
//   - Line 59: $11,904 - $12,000 is negative, so nothing is due; line 63 is
//     $96 refunded.
func TestNebraskaHighIncomeSingle(t *testing.T) {
	ne, f := prepareNE(t, neInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, 1980, 250_000_00, 12_000_00))

	want(t, "line 14", f.GetTaxableIncome(), 241_400_00)
	want(t, "line 15", f.GetIncomeTax(), 12_075_00)
	want(t, "line 35", f.GetTaxAfterNonrefundableCredits(), 11_904_00)
	want(t, "line 59", f.GetAmountOwed(), 0)
	want(t, "line 63", f.GetRefund(), 96_00)
	complete(t, "NE", ne.GetStatus(), ne.GetDiagnostics())
}

// A married couple both 65 or older, still working: $50,000 of wages and
// $500 withheld, so they owe.
//
//   - Line 2a: two boxes, both 65 or older.
//   - Line 6: married filing jointly with two boxes: $20,500 (chart, page 13:
//     $17,200 plus $1,650 for each box).
//   - Line 14: $50,000 - $20,500 = $29,500.
//   - Line 15: schedule, married filing jointly, over $8,040 but not over
//     $48,250: $197.78 + 3.51% x ($29,500 - $8,040) = $197.78 + $753.25 =
//     $951.03, entered as $951.
//   - Line 18: $171 x 2 = $342. Line 35: $951 - $342 = $609.
//   - Line 59: $609 - $500 = $109 due.
func TestNebraskaSeniorsOwe(t *testing.T) {
	ne, f := prepareNE(t, neInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY, 1955, 50_000_00, 500_00))

	if f.GetAge_65OrOlder() != 2 {
		t.Errorf("line 2a = %d, want 2", f.GetAge_65OrOlder())
	}
	want(t, "line 6", f.GetStandardDeduction(), 20_500_00)
	want(t, "line 14", f.GetTaxableIncome(), 29_500_00)
	want(t, "line 15", f.GetIncomeTax(), 951_00)
	want(t, "line 18", f.GetPersonalExemptionCredit(), 342_00)
	want(t, "line 35", f.GetTaxAfterNonrefundableCredits(), 609_00)
	want(t, "line 59", f.GetAmountOwed(), 109_00)
	want(t, "line 63", f.GetRefund(), 0)
	want(t, "summary owed", ne.GetSummary().GetAmountOwed(), 109_00)
	complete(t, "NE", ne.GetStatus(), ne.GetDiagnostics())
}
