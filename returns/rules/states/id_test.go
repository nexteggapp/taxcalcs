package states_test

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/money"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
	form40 "github.com/nexteggapp/taxprotos/go/us/state/id/v1/form_40"
)

// There is no Idaho income tax calculator in Go (idahotaxcalc carries only
// Idaho's estimated tax rules), so each household is worked by hand from the
// 2025 Form 40 and its instructions, which the same PDF carries:
//   - line 16: the standard deduction, $15,750 / $31,500 / $23,625, plus
//     $1,600 a box on line 12a or 12b if married ($2,000 if single or head
//     of household);
//   - line 20: 5.3% of line 19 above $4,811 ($9,622 for a joint return, a
//     head of household or a surviving spouse), in whole dollars;
//   - line 24: $205 a qualifying child aged 16 or under;
//   - line 31: the $10 permanent building fund tax;
//   - line 43: the $155 food tax credit for each member of the household.

func idW2(wages, withheld int64) *formw2.FormW2 {
	return &formw2.FormW2{
		WagesTipsOtherCompensation: cents(wages),
		FederalTaxWithheld:         cents(wages / 10),
		StateTaxWithholding: []*commonv1.StateTaxWithholding{
			{State: "ID", StateIncome: cents(wages), TaxWithheld: cents(withheld)},
		},
	}
}

func idInputs(status returnsv1.FilingStatusElection, people []*returnsv1.Person, deps []*returnsv1.Dependent, w2s []*formw2.FormW2) *returnsv1.ReturnInputs {
	hh := &returnsv1.Household{
		MaritalStatus: returnsv1.MaritalStatus_MARITAL_STATUS_NEVER_MARRIED,
		Taxpayer:      people[0],
		Dependents:    deps,
	}
	if len(people) > 1 {
		hh.MaritalStatus = returnsv1.MaritalStatus_MARITAL_STATUS_MARRIED
		hh.Spouse = people[1]
	}
	var docs []*returnsv1.W2Document
	for i, w := range w2s {
		docs = append(docs, &returnsv1.W2Document{Id: w2Id(i), Person: people[i%len(people)].GetId(), Form: w})
	}
	return &returnsv1.ReturnInputs{
		SchemaVersion: 1,
		TaxYear:       2025,
		Federal: &returnsv1.Federal1040Inputs{
			Household: hh,
			Elections: &returnsv1.Elections{FilingStatus: status},
			Documents: &returnsv1.Documents{W2S: docs},
		},
		States: []*returnsv1.StateInputs{{
			State:  "ID",
			Inputs: &returnsv1.StateInputs_Id{Id: &returnsv1.IdahoInputs{}},
		}},
	}
}

func prepareID(t *testing.T, in *returnsv1.ReturnInputs) (*returnsv1.IdahoReturn, *form40.Form40) {
	t.Helper()
	out := prepare(t, in)
	ret := out.GetStates()[0].GetId()
	if ret == nil {
		t.Fatalf("no Idaho return: %v", out.GetStates()[0])
	}
	complete(t, "Idaho", ret.GetStatus(), ret.GetDiagnostics())
	return ret, ret.GetForms().GetForm_40()
}

// Single, $60,000 of wages, $2,500 withheld.
//   - Line 19: $60,000 - $15,750 = $44,250
//   - Line 20: ($44,250 - $4,811) x 5.3% = $2,090.27, so $2,090
//   - Line 32: $2,090 + $10 = $2,100
//   - Line 50: $155 + $2,500 = $2,655; line 55: $555 refund
func TestIdahoSingle(t *testing.T) {
	ret, f := prepareID(t, idInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		[]*returnsv1.Person{ilPerson("ann", 1995, 3, 1, false, false)}, nil,
		[]*formw2.FormW2{idW2(60000_00, 2500_00)}))

	want(t, "Line 7 Federal AGI", f.GetFederalAdjustedGrossIncome(), 60000_00)
	want(t, "Line 16 Standard deduction", f.GetStandardDeduction(), 15750_00)
	want(t, "Line 19 Taxable income", f.GetTaxableIncome(), 44250_00)
	want(t, "Line 20 Tax", f.GetTax(), 2090_00)
	want(t, "Line 31 Permanent building fund", f.GetPermanentBuildingFundTax(), 10_00)
	want(t, "Line 32 Total tax", f.GetTotalTax(), 2100_00)
	want(t, "Line 43 Food tax credit", f.GetFoodTaxCredit(), 155_00)
	want(t, "Line 46 Withheld", f.GetIdahoTaxWithheld(), 2500_00)
	want(t, "Line 50 Total payments", f.GetTotalPaymentsAndCredits(), 2655_00)
	want(t, "Line 56 Refund", f.GetRefund(), 555_00)
	want(t, "Line 54 Total due", f.GetTotalDue(), 0)
	want(t, "Summary refund", ret.GetSummary().GetRefund(), money.USCents(555_00))
	if f.GetFilingStatus() != form40.FilingStatus_FILING_STATUS_SINGLE {
		t.Errorf("filing status = %v, want single", f.GetFilingStatus())
	}
}

// Married filing jointly, $70,000 and $50,000 of wages, two children aged 8
// and 12, $4,000 and $2,000 withheld.
//   - Line 19: $120,000 - $31,500 = $88,500
//   - Line 20: ($88,500 - $9,622) x 5.3% = $4,180.53, so $4,181
//   - Line 24: 2 x $205 = $410; line 26: $3,771; line 32: $3,781
//   - Line 43: 4 x $155 = $620; line 50: $6,620; line 55: $2,839 refund
func TestIdahoMarriedFilingJointlyWithChildren(t *testing.T) {
	_, f := prepareID(t, idInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY,
		[]*returnsv1.Person{ilPerson("bea", 1985, 4, 2, false, false), ilPerson("cal", 1984, 9, 9, false, false)},
		[]*returnsv1.Dependent{ilDependent("dot", 2017, 5, 5), ilDependent("eli", 2013, 6, 6)},
		[]*formw2.FormW2{idW2(70000_00, 4000_00), idW2(50000_00, 2000_00)}))

	want(t, "Line 7 Federal AGI", f.GetFederalAdjustedGrossIncome(), 120000_00)
	want(t, "Line 16 Standard deduction", f.GetStandardDeduction(), 31500_00)
	want(t, "Line 19 Taxable income", f.GetTaxableIncome(), 88500_00)
	want(t, "Line 20 Tax", f.GetTax(), 4181_00)
	want(t, "Line 24 Child tax credit", f.GetChildTaxCredit(), 410_00)
	want(t, "Line 26 Tax after credits", f.GetTaxAfterCredits(), 3771_00)
	want(t, "Line 32 Total tax", f.GetTotalTax(), 3781_00)
	want(t, "Line 43 Food tax credit", f.GetFoodTaxCredit(), 620_00)
	want(t, "Line 50 Total payments", f.GetTotalPaymentsAndCredits(), 6620_00)
	want(t, "Line 56 Refund", f.GetRefund(), 2839_00)
	if f.GetHouseholdTotal() != 4 {
		t.Errorf("line 6d = %d, want 4", f.GetHouseholdTotal())
	}
}

// Single, $300,000 of wages, $15,000 withheld.
//   - Line 19: $300,000 - $15,750 = $284,250
//   - Line 20: ($284,250 - $4,811) x 5.3% = $14,810.27, so $14,810
//   - Line 32: $14,820; line 50: $15,155; line 55: $335 refund
func TestIdahoHighIncomeSingle(t *testing.T) {
	_, f := prepareID(t, idInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		[]*returnsv1.Person{ilPerson("fay", 1975, 7, 7, false, false)}, nil,
		[]*formw2.FormW2{idW2(300000_00, 15000_00)}))

	want(t, "Line 19 Taxable income", f.GetTaxableIncome(), 284250_00)
	want(t, "Line 20 Tax", f.GetTax(), 14810_00)
	want(t, "Line 32 Total tax", f.GetTotalTax(), 14820_00)
	want(t, "Line 50 Total payments", f.GetTotalPaymentsAndCredits(), 15155_00)
	want(t, "Line 56 Refund", f.GetRefund(), 335_00)
}

// Married filing jointly, both 70, $80,000 of wages between them, $1,000
// withheld.
//   - Line 16: $31,500 + 2 x $1,600 = $34,700; line 17: $45,300
//   - Line 18: the federal Schedule 1-A deduction for seniors, $6,000 each
//   - Line 19: $33,300; line 20: ($33,300 - $9,622) x 5.3% = $1,254.93, so $1,255
//   - Line 32: $1,265; line 50: $310 + $1,000 = $1,310; line 55: $45 refund
func TestIdahoSeniorsMarriedFilingJointly(t *testing.T) {
	_, f := prepareID(t, idInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY,
		[]*returnsv1.Person{ilPerson("gus", 1955, 1, 10, false, false), ilPerson("hal", 1955, 2, 20, false, false)}, nil,
		[]*formw2.FormW2{idW2(50000_00, 600_00), idW2(30000_00, 400_00)}))

	if f.GetAge_65OrOlder() != 2 {
		t.Errorf("line 12a = %d, want 2", f.GetAge_65OrOlder())
	}
	want(t, "Line 16 Standard deduction", f.GetStandardDeduction(), 34700_00)
	want(t, "Line 17 Income after deduction", f.GetIncomeAfterDeduction(), 45300_00)
	want(t, "Line 18 QBI and Schedule 1-A", f.GetQualifiedBusinessIncomeDeduction(), 12000_00)
	want(t, "Line 19 Taxable income", f.GetTaxableIncome(), 33300_00)
	want(t, "Line 20 Tax", f.GetTax(), 1255_00)
	want(t, "Line 32 Total tax", f.GetTotalTax(), 1265_00)
	want(t, "Line 50 Total payments", f.GetTotalPaymentsAndCredits(), 1310_00)
	want(t, "Line 56 Refund", f.GetRefund(), 45_00)
}
