package states_test

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
	form760 "github.com/nexteggapp/taxprotos/go/us/state/va/v1/form_760"
)

// There is no Go income tax calculator for Virginia to compare against, so
// these figures are worked by hand from the 2025 Form 760 and its
// instructions:
//   - Form 760, Exemptions: Section A is $930 for the filer, the spouse on a
//     joint return and each dependent (Va. Code § 58.1-322.03).
//   - Instructions page 17, Line 11: standard deduction $8,750 for Filing
//     Status 1 and 3, $17,500 for Filing Status 2.
//   - Instructions page 41, Tax Rate Schedule: 2% to $3,000; $60 + 3% to
//     $5,000; $120 + 5% to $17,000; $720 + 5.75% above, rounded to whole
//     dollars (Va. Code § 58.1-320).

// vaInputs builds ReturnInputs for a Virginia filer with one W-2.
func vaInputs(
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
			{State: "VA", StateIncome: cents(wages), TaxWithheld: cents(withheld)},
		},
	}
	in := &returnsv1.ReturnInputs{
		SchemaVersion: 1,
		TaxYear:       2025,
		Federal: &returnsv1.Federal1040Inputs{
			Household: hh,
			Elections: &returnsv1.Elections{FilingStatus: status},
			Documents: &returnsv1.Documents{
				W2S: []*returnsv1.W2Document{{Id: "w2-va", Person: "taxpayer", Form: w2}},
			},
		},
		States: []*returnsv1.StateInputs{
			{
				State:  "VA",
				Inputs: &returnsv1.StateInputs_Va{Va: &returnsv1.VirginiaInputs{}},
			},
		},
	}
	for _, opt := range opts {
		opt(in)
	}
	return in
}

func prepareVA(t *testing.T, in *returnsv1.ReturnInputs) (*returnsv1.VirginiaReturn, *form760.Form760) {
	t.Helper()
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine: %v", err)
	}
	out, err := eng.Prepare(in)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if len(out.GetStates()) == 0 || out.GetStates()[0].GetVa() == nil {
		t.Fatalf("expected Virginia return in outputs, got %v", out.GetStates())
	}
	va := out.GetStates()[0].GetVa()
	f := va.GetForms().GetForm_760()
	if f == nil {
		t.Fatalf("expected Form 760 in Virginia return, got %v", va.GetForms())
	}
	return va, f
}

// Single filer, $60,000 of wages, $2,800 withheld:
//   - Line 1, 3, 9: VAGI $60,000.
//   - Line 11: standard deduction $8,750; line 12: one Section A exemption $930.
//   - Line 14: $9,680. Line 15: taxable income $50,320.
//   - Line 16: $720 + 5.75% x $33,320 = $720 + $1,915.90 = $2,635.90, rounded $2,636.
//   - Line 19a, 26: $2,800. Line 28, 36: refund $2,800 - $2,636 = $164.
func TestVirginiaSingleFiler(t *testing.T) {
	va, f := prepareVA(t, vaInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, 60_000_00, 2_800_00))

	want(t, "VA line 9", f.GetVirginiaAdjustedGrossIncome(), 60_000_00)
	want(t, "VA line 11", f.GetStandardDeduction(), 8_750_00)
	want(t, "VA line 12", f.GetExemptions(), 930_00)
	want(t, "VA line 15", f.GetTaxableIncome(), 50_320_00)
	want(t, "VA line 16", f.GetTax(), 2_636_00)
	want(t, "VA line 18", f.GetNetTax(), 2_636_00)
	want(t, "VA line 26", f.GetTotalPaymentsAndCredits(), 2_800_00)
	want(t, "VA line 36", f.GetRefund(), 164_00)
	want(t, "VA line 35", f.GetAmountYouOwe(), 0)
	complete(t, "VA", va.GetStatus(), va.GetDiagnostics())
}

// Married filing jointly with two children, $120,000 of wages, $5,000 withheld:
//   - Line 9: VAGI $120,000.
//   - Line 11: standard deduction $17,500.
//   - Line 12: Section A is you, spouse and two dependents, 4 x $930 = $3,720.
//   - Line 14: $21,220. Line 15: taxable income $98,780.
//   - Line 16: $720 + 5.75% x $81,780 = $720 + $4,702.35 = $5,422.35, rounded $5,422.
//   - Line 17: no spouse tax adjustment entered. Line 18: $5,422.
//   - Line 27, 35: owed $5,422 - $5,000 = $422.
func TestVirginiaMarriedFilingJointlyWithChildren(t *testing.T) {
	in := vaInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY, 120_000_00, 5_000_00,
		func(i *returnsv1.ReturnInputs) {
			i.Federal.Household.Dependents = []*returnsv1.Dependent{
				azDependent("child1", 2015),
				azDependent("child2", 2018),
			}
		})
	va, f := prepareVA(t, in)

	want(t, "VA line 9", f.GetVirginiaAdjustedGrossIncome(), 120_000_00)
	want(t, "VA line 11", f.GetStandardDeduction(), 17_500_00)
	want(t, "VA line 12", f.GetExemptions(), 3_720_00)
	want(t, "VA line 15", f.GetTaxableIncome(), 98_780_00)
	want(t, "VA line 16", f.GetTax(), 5_422_00)
	want(t, "VA line 18", f.GetNetTax(), 5_422_00)
	want(t, "VA line 26", f.GetTotalPaymentsAndCredits(), 5_000_00)
	want(t, "VA line 35", f.GetAmountYouOwe(), 422_00)
	want(t, "VA line 36", f.GetRefund(), 0)
	complete(t, "VA", va.GetStatus(), va.GetDiagnostics())
}

// High-income single filer, $250,000 of wages, $14,000 withheld:
//   - Line 15: $250,000 - $8,750 - $930 = $240,320.
//   - Line 16: $720 + 5.75% x $223,320 = $720 + $12,840.90 = $13,560.90, rounded $13,561.
//   - Line 36: refund $14,000 - $13,561 = $439.
func TestVirginiaHighIncomeSingle(t *testing.T) {
	va, f := prepareVA(t, vaInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, 250_000_00, 14_000_00))

	want(t, "VA line 15", f.GetTaxableIncome(), 240_320_00)
	want(t, "VA line 16", f.GetTax(), 13_561_00)
	want(t, "VA line 36", f.GetRefund(), 439_00)
	want(t, "VA line 35", f.GetAmountYouOwe(), 0)
	complete(t, "VA", va.GetStatus(), va.GetDiagnostics())
}

// The instructions' own example (page 41, Tax Rate Schedule): taxable income
// of $90,000 is taxed $720 + 0.0575 x $73,000 = $4,917.50, rounded to $4,918.
// A single filer with $99,680 of wages has $90,000 of taxable income after
// the $8,750 deduction and the $930 exemption.
func TestVirginiaRateScheduleExample(t *testing.T) {
	_, f := prepareVA(t, vaInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, 99_680_00, 4_918_00))

	want(t, "VA line 15", f.GetTaxableIncome(), 90_000_00)
	want(t, "VA line 16", f.GetTax(), 4_918_00)
	want(t, "VA line 35", f.GetAmountYouOwe(), 0)
	want(t, "VA line 36", f.GetRefund(), 0)
}
