package states_test

// North Dakota has no Go income tax calculator to test against:
// northdakotataxcalc holds only estimated-tax logic. These households are
// worked by hand from the 2025 Form ND-1, its instructions and the tax
// table and rate schedules in the 2025 booklet, line by line in the comments.

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
	formnd1 "github.com/nexteggapp/taxprotos/go/us/state/nd/v1/form_nd1"
)

// ndW2 is a Form W-2 for one person with North Dakota withholding.
func ndW2(id, person string, wages, withheld int64) *returnsv1.W2Document {
	return &returnsv1.W2Document{
		Id:     id,
		Person: person,
		Form: &formw2.FormW2{
			WagesTipsOtherCompensation: cents(wages),
			FederalTaxWithheld:         cents(wages / 10),
			StateTaxWithholding: []*commonv1.StateTaxWithholding{
				{State: "ND", StateIncome: cents(wages), TaxWithheld: cents(withheld)},
			},
		},
	}
}

// ndInputs is a North Dakota household with the given Forms W-2.
func ndInputs(status returnsv1.FilingStatusElection, w2s ...*returnsv1.W2Document) *returnsv1.ReturnInputs {
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
		Taxpayer:      person("taxpayer", 1980),
	}
	if status == returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY {
		hh.MaritalStatus = returnsv1.MaritalStatus_MARITAL_STATUS_MARRIED
		hh.Spouse = person("spouse", 1982)
	}
	return &returnsv1.ReturnInputs{
		SchemaVersion: 1,
		TaxYear:       2025,
		Federal: &returnsv1.Federal1040Inputs{
			Household: hh,
			Elections: &returnsv1.Elections{FilingStatus: status},
			Documents: &returnsv1.Documents{W2S: w2s},
		},
		States: []*returnsv1.StateInputs{{
			State:  "ND",
			Inputs: &returnsv1.StateInputs_Nd{Nd: &returnsv1.NorthDakotaInputs{}},
		}},
	}
}

func prepareND(t *testing.T, in *returnsv1.ReturnInputs) (*returnsv1.NorthDakotaReturn, *formnd1.FormND1) {
	t.Helper()
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine: %v", err)
	}
	out, err := eng.Prepare(in)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if len(out.GetStates()) == 0 || out.GetStates()[0].GetNd() == nil {
		t.Fatalf("expected North Dakota return in outputs, got %v", out.GetStates())
	}
	nd := out.GetStates()[0].GetNd()
	f := nd.GetForms().GetFormNd1()
	if f == nil {
		t.Fatalf("expected Form ND-1 in North Dakota return, got %v", nd.GetForms())
	}
	return nd, f
}

// 1. Single filer, wages $90,000, $600 of North Dakota tax withheld.
//   - Line 1a: federal AGI $90,000.00.
//   - Line 1b: federal taxable income $90,000 - $15,750 single standard
//     deduction = $74,250.00.
//   - Lines 4b, 17, 18: no additions or subtractions; taxable income $74,250.00.
//   - Line 20: 2025 Tax Table (booklet page 20), single, row $74,250-$74,300.
//     The table taxes the row's midpoint through the single schedule
//     (booklet page 27): ($74,275 - $48,475) x 1.95% = $503.10, so $503.
//   - Lines 24, 25: no credits; net tax $503.00.
//   - Lines 26, 28: withholding $600.00.
//   - Lines 29, 32: refund $600.00 - $503.00 = $97.00.
func TestNorthDakotaSingleFiler(t *testing.T) {
	in := ndInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		ndW2("w2", "taxpayer", 90_000_00, 600_00))
	nd, f := prepareND(t, in)

	want(t, "ND federal AGI (line 1a)", f.GetFederalAdjustedGrossIncome(), 90_000_00)
	want(t, "ND federal taxable income (line 1b)", f.GetFederalTaxableIncome(), 74_250_00)
	want(t, "ND taxable income (line 18)", f.GetTaxableIncome(), 74_250_00)
	want(t, "ND tax (line 20)", f.GetTax(), 503_00)
	want(t, "ND net tax liability (line 25)", f.GetNetTaxLiability(), 503_00)
	want(t, "ND withholding (line 26)", f.GetNorthDakotaTaxWithheld(), 600_00)
	want(t, "ND total payments (line 28)", f.GetTotalPayments(), 600_00)
	want(t, "ND refund (line 32)", f.GetRefund(), 97_00)
	want(t, "ND tax due (line 33)", f.GetTaxDue(), 0)
	complete(t, "ND", nd.GetStatus(), nd.GetDiagnostics())
}

// 2. Married filing jointly with two children; wages $90,000 and $60,000,
// $1,500 of North Dakota tax withheld.
//   - Line 1b: $150,000 - $31,500 joint standard deduction = $118,500.00.
//   - Line 18: taxable income $118,500.00, at least $100,000, so the joint
//     rate schedule (booklet page 27) applies, not the table.
//   - Line 20: ($118,500 - $80,975) x 1.95% = $731.7375, $731.74.
//   - Line 22, Marriage Penalty Credit Worksheet (booklet page 14):
//     line 1 $118,500 is more than $81,036; lines 3a, 3b qualified income
//     (wages) $90,000 and $60,000; line 4 $60,000 is more than $47,550;
//     line 6 = $60,000 - $15,750 = $44,250; line 7 single tax on $44,250 = $0;
//     line 8 = $118,500 - $44,250 = $74,250; line 9 single tax on $74,250 =
//     ($74,250 - $48,475) x 1.95% = $502.61; line 10 joint tax on $118,500 =
//     $731.74; line 11 = $502.61; line 12 = $229.13; line 14 = smaller of
//     $229.13 and the $312 maximum = $229.13.
//   - Line 25: $731.74 - $229.13 = $502.61.
//   - Line 32: refund $1,500.00 - $502.61 = $997.39.
func TestNorthDakotaMarriedFilingJointlyWithChildren(t *testing.T) {
	in := ndInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY,
		ndW2("w2-tp", "taxpayer", 90_000_00, 900_00),
		ndW2("w2-sp", "spouse", 60_000_00, 600_00))
	in.Federal.Household.Dependents = []*returnsv1.Dependent{
		azDependent("child1", 2015),
		azDependent("child2", 2018),
	}
	nd, f := prepareND(t, in)

	want(t, "ND federal taxable income (line 1b)", f.GetFederalTaxableIncome(), 118_500_00)
	want(t, "ND taxable income (line 18)", f.GetTaxableIncome(), 118_500_00)
	want(t, "ND tax (line 20)", f.GetTax(), 731_74)
	want(t, "ND marriage penalty credit (line 22)", f.GetMarriagePenaltyCredit(), 229_13)
	want(t, "ND total credits (line 24)", f.GetTotalCredits(), 229_13)
	want(t, "ND net tax liability (line 25)", f.GetNetTaxLiability(), 502_61)
	want(t, "ND withholding (line 26)", f.GetNorthDakotaTaxWithheld(), 1_500_00)
	want(t, "ND refund (line 32)", f.GetRefund(), 997_39)
	want(t, "ND tax due (line 33)", f.GetTaxDue(), 0)
	complete(t, "ND", nd.GetStatus(), nd.GetDiagnostics())
}

// 3. High-income single filer, wages $300,000, $4,000 withheld.
//   - Line 1b: $300,000 - $15,750 = $284,250.00 = line 18.
//   - Line 20, single rate schedule (booklet page 27): $3,828.83 + 2.50% of
//     ($284,250 - $244,825) = $3,828.83 + $985.63 = $4,814.46.
//   - Line 25: no credits; $4,814.46.
//   - Lines 33, 36: tax due $4,814.46 - $4,000.00 = $814.46.
func TestNorthDakotaHighIncome(t *testing.T) {
	in := ndInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		ndW2("w2", "taxpayer", 300_000_00, 4_000_00))
	nd, f := prepareND(t, in)

	want(t, "ND taxable income (line 18)", f.GetTaxableIncome(), 284_250_00)
	want(t, "ND tax (line 20)", f.GetTax(), 4_814_46)
	want(t, "ND net tax liability (line 25)", f.GetNetTaxLiability(), 4_814_46)
	want(t, "ND tax due (line 33)", f.GetTaxDue(), 814_46)
	want(t, "ND balance due (line 36)", f.GetBalanceDue(), 814_46)
	want(t, "ND refund (line 32)", f.GetRefund(), 0)
	complete(t, "ND", nd.GetStatus(), nd.GetDiagnostics())
}

// 4. The booklet's own tax table example (page 20): a married couple filing
// jointly with North Dakota taxable income in the $91,900-$91,950 row owes
// $214. Wages of $123,425 less the $31,500 joint standard deduction give
// $91,925. All wages are the filer's, so the spouse has no qualified income
// and no marriage penalty credit (worksheet line 4 is $0).
func TestNorthDakotaTaxTableExample(t *testing.T) {
	in := ndInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY,
		ndW2("w2", "taxpayer", 123_425_00, 214_00))
	nd, f := prepareND(t, in)

	want(t, "ND taxable income (line 18)", f.GetTaxableIncome(), 91_925_00)
	want(t, "ND tax (line 20)", f.GetTax(), 214_00)
	want(t, "ND marriage penalty credit (line 22)", f.GetMarriagePenaltyCredit(), 0)
	want(t, "ND net tax liability (line 25)", f.GetNetTaxLiability(), 214_00)
	want(t, "ND refund (line 32)", f.GetRefund(), 0)
	want(t, "ND tax due (line 33)", f.GetTaxDue(), 0)
	complete(t, "ND", nd.GetStatus(), nd.GetDiagnostics())
}
