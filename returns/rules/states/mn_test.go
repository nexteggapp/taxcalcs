package states_test

// Minnesota Form M1. minnesotataxcalc holds only estimated-tax logic (its
// doc.go says there is no income tax calculator there yet), so these
// households are worked by hand from the 2025 Form M1 and its instructions.
// Page numbers are the instructions PDF's pages, counted from 1.

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
	formm1 "github.com/nexteggapp/taxprotos/go/us/state/mn/v1/form_m1"
)

func mnInputs(
	status returnsv1.FilingStatusElection,
	birthYear int32,
	wages int64,
	withheld int64,
	mn *returnsv1.MinnesotaInputs,
	opts ...func(*returnsv1.ReturnInputs),
) *returnsv1.ReturnInputs {
	person := func(id string, year int32) *returnsv1.Person {
		return &returnsv1.Person{
			Id:          id,
			FirstName:   id,
			DateOfBirth: &commonv1.Date{Year: year, Month: 6, Day: 1},
			Tin: &returnsv1.TaxpayerID{
				Kind:               returnsv1.TaxpayerIDKind_TAXPAYER_ID_KIND_SSN,
				ValidForEmployment: boolPtr(true),
			},
		}
	}
	hh := &returnsv1.Household{
		MaritalStatus: returnsv1.MaritalStatus_MARITAL_STATUS_NEVER_MARRIED,
		Taxpayer:      person("taxpayer", birthYear),
	}
	if status == returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY {
		hh.MaritalStatus = returnsv1.MaritalStatus_MARITAL_STATUS_MARRIED
		hh.Spouse = person("spouse", birthYear)
	}
	w2 := &formw2.FormW2{
		WagesTipsOtherCompensation: cents(wages),
		FederalTaxWithheld:         cents(wages / 10),
		StateTaxWithholding: []*commonv1.StateTaxWithholding{
			{State: "MN", StateIncome: cents(wages), TaxWithheld: cents(withheld)},
		},
	}
	in := &returnsv1.ReturnInputs{
		SchemaVersion: 1,
		TaxYear:       2025,
		Federal: &returnsv1.Federal1040Inputs{
			Household: hh,
			Elections: &returnsv1.Elections{FilingStatus: status},
			Documents: &returnsv1.Documents{
				W2S: []*returnsv1.W2Document{{Id: "w2-mn", Person: "taxpayer", Form: w2}},
			},
		},
		States: []*returnsv1.StateInputs{
			{State: "MN", Inputs: &returnsv1.StateInputs_Mn{Mn: mn}},
		},
	}
	for _, opt := range opts {
		opt(in)
	}
	return in
}

func prepareMN(t *testing.T, in *returnsv1.ReturnInputs) (*returnsv1.MinnesotaReturn, *formm1.FormM1) {
	t.Helper()
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine: %v", err)
	}
	out, err := eng.Prepare(in)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if len(out.GetStates()) == 0 || out.GetStates()[0].GetMn() == nil {
		t.Fatalf("expected Minnesota return in outputs, got %v", out.GetStates())
	}
	mn := out.GetStates()[0].GetMn()
	f := mn.GetForms().GetFormM1()
	if f == nil {
		t.Fatalf("expected Form M1 in Minnesota return, got %v", mn.GetForms())
	}
	return mn, f
}

// A single filer with $60,000 of wages and $2,800 withheld.
//   - Line 1: federal AGI $60,000.
//   - Line 4: Standard Deduction Table for Line 4 (page 13), single, no boxes:
//     $14,950. AGI is under $238,950, so Worksheet A does not apply.
//   - Line 5: no dependents, $0.
//   - Line 9: $60,000 - $14,950 = $45,050.
//   - Line 10: 2025 Tax Tables (pages 32-38), the $45,000-$45,100 row, single.
//     The table taxes the middle of the row by the single rate schedule (page
//     39): $1,742.50 + 6.80% x ($45,050 - $32,570) = $2,591.14, so $2,591.
//   - Line 19: $2,591. Line 20 and 23: $2,800. Line 24: refund $209.
func TestMinnesotaSingleFiler(t *testing.T) {
	in := mnInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, 1985,
		60_000_00, 2_800_00, &returnsv1.MinnesotaInputs{})
	mn, f := prepareMN(t, in)

	want(t, "MN federal AGI (line 1)", f.GetFederalAdjustedGrossIncome(), 60_000_00)
	want(t, "MN deduction (line 4)", f.GetDeduction(), 14_950_00)
	want(t, "MN exemptions (line 5)", f.GetDependentExemptions(), 0)
	want(t, "MN taxable income (line 9)", f.GetTaxableIncome(), 45_050_00)
	want(t, "MN tax (line 10)", f.GetTax(), 2_591_00)
	want(t, "MN total tax (line 19)", f.GetTotalTax(), 2_591_00)
	want(t, "MN withheld (line 20)", f.GetMinnesotaTaxWithheld(), 2_800_00)
	want(t, "MN total payments (line 23)", f.GetTotalPayments(), 2_800_00)
	want(t, "MN refund (line 24)", f.GetRefund(), 209_00)
	want(t, "MN amount owed (line 26)", f.GetAmountOwed(), 0)
	complete(t, "MN", mn.GetStatus(), mn.GetDiagnostics())
}

// Married filing jointly, two children, $120,000 of wages, $5,000 withheld,
// a $1,000 Child and Working Family Credit from Schedule M1CWFC.
//   - Line 4: Standard Deduction Table for Line 4 (page 13), married filing
//     joint, no boxes: $29,900.
//   - Line 5: Worksheet for Line 5 (page 14): 2 x $5,200 = $10,400; AGI is under
//     the $358,550 joint threshold, so no reduction.
//   - Line 8: $29,900 + $10,400 = $40,300. Line 9: $120,000 - $40,300 = $79,700.
//   - Line 10: 2025 Tax Tables, the $79,700-$79,800 row (page 38), married
//     filing jointly: $4,733.
//   - Line 22: $1,000 from Schedule M1REF. Line 23: $5,000 + $1,000 = $6,000.
//   - Line 24: $6,000 - $4,733 = refund $1,267.
func TestMinnesotaMarriedFilingJointlyWithChildren(t *testing.T) {
	in := mnInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY, 1985,
		120_000_00, 5_000_00,
		&returnsv1.MinnesotaInputs{ChildAndWorkingFamilyCredits: cents(1_000_00)},
		func(i *returnsv1.ReturnInputs) {
			i.Federal.Household.Dependents = []*returnsv1.Dependent{
				azDependent("child1", 2015),
				azDependent("child2", 2018),
			}
		},
	)
	mn, f := prepareMN(t, in)

	want(t, "MN deduction (line 4)", f.GetDeduction(), 29_900_00)
	want(t, "MN exemptions (line 5)", f.GetDependentExemptions(), 10_400_00)
	want(t, "MN total subtractions (line 8)", f.GetTotalSubtractions(), 40_300_00)
	want(t, "MN taxable income (line 9)", f.GetTaxableIncome(), 79_700_00)
	want(t, "MN tax (line 10)", f.GetTax(), 4_733_00)
	want(t, "MN refundable credits (line 22)", f.GetRefundableCredits(), 1_000_00)
	want(t, "MN total payments (line 23)", f.GetTotalPayments(), 6_000_00)
	want(t, "MN refund (line 24)", f.GetRefund(), 1_267_00)
	complete(t, "MN", mn.GetStatus(), mn.GetDiagnostics())
}

// A single filer aged 67 with $400,000 of wages and $30,000 withheld, whose
// standard deduction is limited.
//   - Line 4: Standard Deduction Table (page 13), single, one box (65 or
//     older): $16,950. Worksheet A for Line 4 (page 14): step 2 = $330,300 -
//     $238,950 = $91,350; step 3 = 3% = $2,741 (rounded); step 4 = $400,000 -
//     $330,300 = $69,700; step 5 = 10% = $6,970; step 6 = $9,711; step 8 = 80%
//     of $16,950 = $13,560; step 9 = $9,711; step 10 = $16,950 - $9,711 =
//     $7,239.
//   - Line 9: $400,000 - $7,239 = $392,761.
//   - Line 10: over $86,800, so the single rate schedule (page 39):
//     $13,996.80 + 9.85% x ($392,761 - $198,630) = $33,118.70, so $33,119.
//   - Line 26: $33,119 - $30,000 = $3,119 owed.
//
// With $20,000 of Schedule M1SA itemized deductions, line 4 is $20,000
// instead, line 9 is $380,000 and line 10 is $13,996.80 + 9.85% x $181,370 =
// $31,861.75, so $31,862.
func TestMinnesotaHighIncomeSenior(t *testing.T) {
	in := mnInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, 1958,
		400_000_00, 30_000_00, &returnsv1.MinnesotaInputs{})
	mn, f := prepareMN(t, in)

	want(t, "MN deduction (line 4)", f.GetDeduction(), 7_239_00)
	want(t, "MN taxable income (line 9)", f.GetTaxableIncome(), 392_761_00)
	want(t, "MN tax (line 10)", f.GetTax(), 33_119_00)
	want(t, "MN total tax (line 19)", f.GetTotalTax(), 33_119_00)
	want(t, "MN amount owed (line 26)", f.GetAmountOwed(), 3_119_00)
	want(t, "MN refund (line 24)", f.GetRefund(), 0)
	complete(t, "MN", mn.GetStatus(), mn.GetDiagnostics())

	in = mnInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, 1958,
		400_000_00, 30_000_00, &returnsv1.MinnesotaInputs{ItemizedDeductions: cents(20_000_00)})
	_, f = prepareMN(t, in)
	if !f.GetItemized() {
		t.Errorf("MN itemized = false, want true")
	}
	want(t, "MN itemized deduction (line 4)", f.GetDeduction(), 20_000_00)
	want(t, "MN tax with itemized deductions (line 10)", f.GetTax(), 31_862_00)
}
