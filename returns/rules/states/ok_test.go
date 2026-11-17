package states_test

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
	form511 "github.com/nexteggapp/taxprotos/go/us/state/ok/v1/form_511"
)

// There is no Go income tax calculator for Oklahoma, so these
// households are worked by hand from the 2025 Form 511 packet (instructions
// and tax table) and 68 O.S. §§ 2355, 2357 and 2358. Page numbers are the
// packet's printed page numbers.

// okInputs builds a household with one W-2 asking for an Oklahoma return.
func okInputs(
	status returnsv1.FilingStatusElection,
	taxpayerBorn int32,
	wages int64,
	withheld int64,
	dependentsBorn ...int32,
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
		Taxpayer:      person("taxpayer", taxpayerBorn),
	}
	if status == returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY {
		hh.MaritalStatus = returnsv1.MaritalStatus_MARITAL_STATUS_MARRIED
		hh.Spouse = person("spouse", taxpayerBorn+2)
	}
	for i, born := range dependentsBorn {
		hh.Dependents = append(hh.Dependents, azDependent(string(rune('a'+i))+"-child", born))
	}
	w2 := &formw2.FormW2{
		WagesTipsOtherCompensation: cents(wages),
		FederalTaxWithheld:         cents(wages / 10),
		StateTaxWithholding: []*commonv1.StateTaxWithholding{
			{State: "OK", StateIncome: cents(wages), TaxWithheld: cents(withheld)},
		},
	}
	return &returnsv1.ReturnInputs{
		SchemaVersion: 1,
		TaxYear:       2025,
		Federal: &returnsv1.Federal1040Inputs{
			Household: hh,
			Elections: &returnsv1.Elections{FilingStatus: status},
			Documents: &returnsv1.Documents{
				W2S: []*returnsv1.W2Document{{Id: "w2-ok", Person: "taxpayer", Form: w2}},
			},
		},
		States: []*returnsv1.StateInputs{{
			State:  "OK",
			Inputs: &returnsv1.StateInputs_Ok{Ok: &returnsv1.OklahomaInputs{}},
		}},
	}
}

func prepareOK(t *testing.T, in *returnsv1.ReturnInputs) (*returnsv1.OklahomaReturn, *form511.Form511) {
	t.Helper()
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine: %v", err)
	}
	out, err := eng.Prepare(in)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if len(out.GetStates()) == 0 || out.GetStates()[0].GetOk() == nil {
		t.Fatalf("expected Oklahoma return in outputs, got %v", out.GetStates())
	}
	ok := out.GetStates()[0].GetOk()
	f := ok.GetForms().GetForm_511()
	if f == nil {
		t.Fatalf("expected Form 511 in Oklahoma return, got %v", ok.GetForms())
	}
	return ok, f
}

// A single filer with $50,000 of wages and $2,000 withheld.
//
//   - Line 1: federal AGI $50,000; no subtractions, out-of-state income or
//     additions, so line 7 is $50,000 and line 9 is $50,000.
//   - Line 10: single standard deduction $6,350 (page 10, line 10).
//   - Line 11: one regular exemption x $1,000 = $1,000 (page 10, line 11).
//   - Line 12: $7,350. Line 13: $50,000 - $7,350 = $42,650.
//   - Line 14: tax table row $42,650-$42,700, single column = $1,839
//     (page 32). The table taxes the midpoint, $42,675: $153.50 on the first
//     $7,200 plus 4.75% of $35,475 = $1,838.56, which rounds to $1,839.
//   - Lines 15-17: no credits. Line 18 and line 20: $1,839.
//   - Line 21: withholding $2,000. Line 33: $2,000.
//   - Lines 34 and 38: refund $2,000 - $1,839 = $161.
func TestOklahomaSingleFiler(t *testing.T) {
	in := okInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, 1985, 50_000_00, 2_000_00)
	ok, f := prepareOK(t, in)

	want(t, "OK federal AGI (line 1)", f.GetFederalAdjustedGrossIncome(), 50_000_00)
	want(t, "OK AGI (line 7)", f.GetOklahomaAdjustedGrossIncome(), 50_000_00)
	want(t, "OK deduction (line 10)", f.GetDeduction(), 6_350_00)
	want(t, "OK exemptions (line 11)", f.GetExemptionAmount(), 1_000_00)
	want(t, "OK taxable income (line 13)", f.GetTaxableIncome(), 42_650_00)
	want(t, "OK tax (line 14)", f.GetIncomeTax(), 1_839_00)
	want(t, "OK tax after credits (line 18)", f.GetTaxAfterCredits(), 1_839_00)
	want(t, "OK withholding (line 21)", f.GetOklahomaTaxWithheld(), 2_000_00)
	want(t, "OK total payments (line 33)", f.GetTotalPaymentsAndCredits(), 2_000_00)
	want(t, "OK refund (line 38)", f.GetRefund(), 161_00)
	want(t, "OK amount owed (line 42)", f.GetAmountOwed(), 0)
	want(t, "OK summary tax", ok.GetSummary().GetTotalTax(), 1_839_00)
	complete(t, "OK", ok.GetStatus(), ok.GetDiagnostics())
}

// Married filing jointly with two children (born 2015 and 2018), $80,000 of
// wages and $2,500 withheld.
//
//   - Federal: taxable income $80,000 - $31,500 = $48,500; tax $5,343; child
//     tax credit 2 x $2,200 = $4,400, all of it nonrefundable; no earned
//     income credit at this income.
//   - Line 10: joint standard deduction $12,700 (page 10, line 10).
//   - Line 11: two regular exemptions and two dependents = 4 x $1,000 =
//     $4,000 (page 9, exemptions; page 10, line 11).
//   - Line 13: $80,000 - $16,700 = $63,300.
//   - Line 14: tax table row $63,300-$63,350, married filing joint column =
//     $2,631 (page 34). Midpoint $63,325: $307 on the first $14,400 plus
//     4.75% of $48,925 = $2,630.94, rounded to $2,631.
//   - Line 15: federal AGI is $100,000 or less, so the greater of 20% of the
//     federal child care credit ($0) and 5% of the federal child tax credit
//     ($4,400 x 5% = $220), times Oklahoma AGI over federal AGI (100%) =
//     $220 (page 11, line 15; Schedule 511-F).
//   - Line 18: $2,631 - $220 = $2,411.
//   - Line 21: withholding $2,500. Line 38: refund $2,500 - $2,411 = $89.
func TestOklahomaMarriedWithChildren(t *testing.T) {
	in := okInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY, 1985, 80_000_00, 2_500_00, 2015, 2018)
	ok, f := prepareOK(t, in)

	want(t, "OK deduction (line 10)", f.GetDeduction(), 12_700_00)
	want(t, "OK exemptions (line 11)", f.GetExemptionAmount(), 4_000_00)
	want(t, "OK taxable income (line 13)", f.GetTaxableIncome(), 63_300_00)
	want(t, "OK tax (line 14)", f.GetIncomeTax(), 2_631_00)
	want(t, "OK child tax credit (line 15)", f.GetChildCareOrChildTaxCredit(), 220_00)
	want(t, "OK tax after credits (line 18)", f.GetTaxAfterCredits(), 2_411_00)
	want(t, "OK total payments (line 33)", f.GetTotalPaymentsAndCredits(), 2_500_00)
	want(t, "OK refund (line 38)", f.GetRefund(), 89_00)
	want(t, "OK amount owed (line 42)", f.GetAmountOwed(), 0)
	if f.GetTotalExemptions() != 4 {
		t.Errorf("OK total exemptions = %d, want 4", f.GetTotalExemptions())
	}
	complete(t, "OK", ok.GetStatus(), ok.GetDiagnostics())
}

// A retired single filer, born 1955, still earning $250,000 of wages, with
// $11,000 withheld.
//
//   - Line 10: single standard deduction $6,350.
//   - Line 11: one regular exemption. The special exemption for those 65 or
//     older needs federal AGI of $15,000 or less on a single return (page 9),
//     so none: $1,000.
//   - Line 13: $250,000 - $7,350 = $242,650.
//   - Line 14: $100,000 or more, so the computation worksheet for single
//     filers (page 38): $242,650 - $100,000 = $142,650; x 0.0475 = $6,775.88,
//     rounded to $6,776; plus $4,562 = $11,338.
//   - Line 15: federal AGI over $100,000, so no credit.
//   - Line 21: withholding $11,000. Line 39 and 42: owed $11,338 - $11,000 =
//     $338.
func TestOklahomaRetiredHighIncome(t *testing.T) {
	in := okInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, 1955, 250_000_00, 11_000_00)
	ok, f := prepareOK(t, in)

	want(t, "OK exemptions (line 11)", f.GetExemptionAmount(), 1_000_00)
	want(t, "OK taxable income (line 13)", f.GetTaxableIncome(), 242_650_00)
	want(t, "OK tax (line 14)", f.GetIncomeTax(), 11_338_00)
	want(t, "OK tax after credits (line 18)", f.GetTaxAfterCredits(), 11_338_00)
	want(t, "OK total payments (line 33)", f.GetTotalPaymentsAndCredits(), 11_000_00)
	want(t, "OK refund (line 38)", f.GetRefund(), 0)
	want(t, "OK tax due (line 39)", f.GetTaxDue(), 338_00)
	want(t, "OK amount owed (line 42)", f.GetAmountOwed(), 338_00)
	complete(t, "OK", ok.GetStatus(), ok.GetDiagnostics())
}

// The tax table's own example (page 27): Mr. and Mrs. Jones file jointly
// with Oklahoma taxable income of $14,793 and read $325 from the $14,750 -
// $14,800 row. Wages of $29,493 less the $12,700 standard deduction and two
// $1,000 exemptions give that taxable income.
func TestOklahomaTaxTableExample(t *testing.T) {
	in := okInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY, 1985, 29_493_00, 0)
	_, f := prepareOK(t, in)

	want(t, "OK taxable income (line 13)", f.GetTaxableIncome(), 14_793_00)
	want(t, "OK tax (line 14)", f.GetIncomeTax(), 325_00)
}
