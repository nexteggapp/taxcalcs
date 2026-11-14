package states_test

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
	formar1000f "github.com/nexteggapp/taxprotos/go/us/state/ar/v1/form_ar1000f"
)

// arkansastaxcalc has no income tax calculator, so these households are
// worked by hand from the 2025 Form AR1000F and its instructions
// ("Instructions" below), and the comments give the working line by line.

// arInputs builds ReturnInputs for an Arkansas filer with one W-2.
func arInputs(
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
	if status == returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY {
		hh.MaritalStatus = returnsv1.MaritalStatus_MARITAL_STATUS_MARRIED
		hh.Spouse = person("spouse", 1987)
	}
	w2 := &formw2.FormW2{
		WagesTipsOtherCompensation: cents(wages),
		FederalTaxWithheld:         cents(wages / 10),
		StateTaxWithholding: []*commonv1.StateTaxWithholding{
			{State: "AR", StateIncome: cents(wages), TaxWithheld: cents(withheld)},
		},
	}
	in := &returnsv1.ReturnInputs{
		SchemaVersion: 1,
		TaxYear:       2025,
		Federal: &returnsv1.Federal1040Inputs{
			Household: hh,
			Elections: &returnsv1.Elections{FilingStatus: status},
			Documents: &returnsv1.Documents{
				W2S: []*returnsv1.W2Document{{Id: "w2-ar", Person: "taxpayer", Form: w2}},
			},
		},
		States: []*returnsv1.StateInputs{
			{
				State:  "AR",
				Inputs: &returnsv1.StateInputs_Ar{Ar: &returnsv1.ArkansasInputs{}},
			},
		},
	}
	for _, opt := range opts {
		opt(in)
	}
	return in
}

func prepareAR(t *testing.T, in *returnsv1.ReturnInputs) (*returnsv1.ArkansasReturn, *formar1000f.FormAR1000F) {
	t.Helper()
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine: %v", err)
	}
	out, err := eng.Prepare(in)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if len(out.GetStates()) == 0 || out.GetStates()[0].GetAr() == nil {
		t.Fatalf("expected Arkansas return in outputs, got %v", out.GetStates())
	}
	ar := out.GetStates()[0].GetAr()
	f := ar.GetForms().GetFormAr1000F()
	if f == nil {
		t.Fatalf("expected Form AR1000F in Arkansas return, got %v", ar.GetForms())
	}
	return ar, f
}

// 1. Single, $50,000 of wages, $1,800 of Arkansas tax withheld.
//   - Line 7A: one box (yourself) x $29 = $29 (Instructions, line 7A).
//   - Lines 8, 23, 25: $50,000.
//   - Line 27: standard deduction for status 1, $2,470 (Instructions, line 27).
//   - Line 28: $50,000 - $2,470 = $47,530.
//   - Line 29: Regular Income Tax Table, row $47,500 to $47,600 = $1,434.
//   - Line 34: $29. Line 38: $1,434 - $29 = $1,405.
//   - Lines 39A, 44: $1,800. Line 47 and 50: $1,800 - $1,405 = $395 refund.
func TestArkansasSingleFiler(t *testing.T) {
	ar, f := prepareAR(t, arInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, 50_000_00, 1_800_00))

	want(t, "AR personal credits (line 7A)", f.GetPersonalCredits(), 29_00)
	want(t, "AR total income (line 23)", f.GetTotalIncome(), 50_000_00)
	want(t, "AR deduction (line 27)", f.GetDeduction(), 2_470_00)
	want(t, "AR net taxable income (line 28)", f.GetNetTaxableIncome(), 47_530_00)
	want(t, "AR tax (line 29)", f.GetTax(), 1_434_00)
	want(t, "AR net tax (line 38)", f.GetNetTax(), 1_405_00)
	want(t, "AR total payments (line 44)", f.GetTotalPayments(), 1_800_00)
	want(t, "AR refund (line 50)", f.GetRefund(), 395_00)
	want(t, "AR amount due (line 51)", f.GetAmountDue(), 0)
	complete(t, "AR", ar.GetStatus(), ar.GetDiagnostics())
}

// 2. Married filing jointly, two children, $90,000 of wages, $2,500 withheld.
//   - Line 7A: two boxes (yourself, spouse) x $29 = $58 (Instructions, line 7A).
//   - Line 7B: two dependents x $29 = $58. Line 7C: $116.
//   - Line 27: standard deduction for status 2, $4,940 (Instructions, line 27).
//   - Line 28: $90,000 - $4,940 = $85,060.
//   - Line 29: Regular Income Tax Table, row $85,001 to $85,101 = $2,897.
//   - Line 38: $2,897 - $116 = $2,781.
//   - Line 51: $2,781 - $2,500 = $281 due.
func TestArkansasMarriedFilingJointlyWithChildren(t *testing.T) {
	in := arInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY,
		90_000_00,
		2_500_00,
		func(i *returnsv1.ReturnInputs) {
			i.Federal.Household.Dependents = []*returnsv1.Dependent{
				azDependent("child1", 2015),
				azDependent("child2", 2018),
			}
		},
	)
	ar, f := prepareAR(t, in)

	if f.GetPersonalCreditBoxes() != 2 || f.GetDependents() != 2 {
		t.Errorf("AR boxes = %d, dependents = %d, want 2 and 2", f.GetPersonalCreditBoxes(), f.GetDependents())
	}
	want(t, "AR total personal credits (line 7C)", f.GetTotalPersonalCredits(), 116_00)
	want(t, "AR deduction (line 27)", f.GetDeduction(), 4_940_00)
	want(t, "AR net taxable income (line 28)", f.GetNetTaxableIncome(), 85_060_00)
	want(t, "AR tax (line 29)", f.GetTax(), 2_897_00)
	want(t, "AR net tax (line 38)", f.GetNetTax(), 2_781_00)
	want(t, "AR total payments (line 44)", f.GetTotalPayments(), 2_500_00)
	want(t, "AR refund (line 50)", f.GetRefund(), 0)
	want(t, "AR amount due (line 51)", f.GetAmountDue(), 281_00)
	complete(t, "AR", ar.GetStatus(), ar.GetDiagnostics())
}

// 3. Single, $150,000 of wages, $5,500 withheld: above the table.
//   - Line 28: $150,000 - $2,470 = $147,530.
//   - Line 29: the table's note for $100,001 and over: $3,809 + 3.9% of
//     $47,530 = $3,809 + $1,853.67, rounded to whole dollars = $5,663.
//   - Line 38: $5,663 - $29 = $5,634.
//   - Line 51: $5,634 - $5,500 = $134 due.
func TestArkansasHighIncomeSingle(t *testing.T) {
	ar, f := prepareAR(t, arInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, 150_000_00, 5_500_00))

	want(t, "AR net taxable income (line 28)", f.GetNetTaxableIncome(), 147_530_00)
	want(t, "AR tax (line 29)", f.GetTax(), 5_663_00)
	want(t, "AR net tax (line 38)", f.GetNetTax(), 5_634_00)
	want(t, "AR amount due (line 51)", f.GetAmountDue(), 134_00)
	want(t, "AR refund (line 50)", f.GetRefund(), 0)
	complete(t, "AR", ar.GetStatus(), ar.GetDiagnostics())
}
