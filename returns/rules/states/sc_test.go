package states_test

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
	formsc1040 "github.com/nexteggapp/taxprotos/go/us/state/sc/v1/form_sc1040"
)

// There is no Go income tax calculator for South Carolina, so these tests check
// the SC1040 against figures worked by hand from the 2025 SC1040, its
// instructions (SC1040 Instructions) and the SC1040TT tax tables.
//
// The SC1040TT tables give, for each row, the tax on the middle of the row:
// 0% up to $3,560, 3% from $3,560 to $17,830, and 6% less $642 above that.
// Rows are $50 wide below $7,000 and $100 wide from $7,000 to $100,000; from
// $100,000 the rate schedule applies to the income itself (6% less $642).

func scInputs(
	status returnsv1.FilingStatusElection,
	taxpayer *returnsv1.Person,
	spouse *returnsv1.Person,
	dependents []*returnsv1.Dependent,
	w2s []*formw2.FormW2,
	scIn *returnsv1.SouthCarolinaInputs,
) *returnsv1.ReturnInputs {
	marital := returnsv1.MaritalStatus_MARITAL_STATUS_NEVER_MARRIED
	if spouse != nil {
		marital = returnsv1.MaritalStatus_MARITAL_STATUS_MARRIED
	}
	var w2Docs []*returnsv1.W2Document
	for i, w := range w2s {
		personID := taxpayer.Id
		if i == 1 && spouse != nil {
			personID = spouse.Id
		}
		w2Docs = append(w2Docs, &returnsv1.W2Document{Id: w2Id(i), Person: personID, Form: w})
	}
	return &returnsv1.ReturnInputs{
		SchemaVersion: 1,
		TaxYear:       2025,
		Federal: &returnsv1.Federal1040Inputs{
			Household: &returnsv1.Household{
				MaritalStatus: marital,
				Taxpayer:      taxpayer,
				Spouse:        spouse,
				Dependents:    dependents,
			},
			Elections: &returnsv1.Elections{FilingStatus: status},
			Documents: &returnsv1.Documents{W2S: w2Docs},
		},
		States: []*returnsv1.StateInputs{
			{State: "SC", Inputs: &returnsv1.StateInputs_Sc{Sc: scIn}},
		},
	}
}

func scW2(wages, fedWithheld, scWithheld int64) *formw2.FormW2 {
	return &formw2.FormW2{
		WagesTipsOtherCompensation: cents(wages),
		FederalTaxWithheld:         cents(fedWithheld),
		StateTaxWithholding: []*commonv1.StateTaxWithholding{
			{State: "SC", StateIncome: cents(wages), TaxWithheld: cents(scWithheld)},
		},
	}
}

func prepareSC(t *testing.T, in *returnsv1.ReturnInputs) (*returnsv1.SouthCarolinaReturn, *formsc1040.FormSC1040) {
	t.Helper()
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatal(err)
	}
	out, err := eng.Prepare(in)
	if err != nil {
		t.Fatal(err)
	}
	ret := out.GetStates()[0].GetSc()
	if ret == nil {
		t.Fatalf("expected a South Carolina return, got %v", out.GetStates()[0])
	}
	if ret.GetStatus() != returnsv1.ReturnStatus_RETURN_STATUS_COMPLETE {
		t.Errorf("expected a complete return, got %v: %v", ret.GetStatus(), ret.GetDiagnostics())
	}
	return ret, ret.GetForms().GetFormSc1040()
}

// A single filer with wages.
//
//   - Line 1: federal taxable income, $60,000 wages less the $15,750 2025
//     federal standard deduction = $44,250.
//   - Lines 2, 4: no additions or subtractions; no dependents (line w $0).
//   - Line 5: $44,250.
//   - Line 6: SC1040TT row $44,200–$44,300, middle $44,250:
//     $44,250 × 6% − $642 = $2,655 − $642 = $2,013.
//   - Lines 10, 15: $2,013.
//   - Line 16: SC withholding $2,500. Line 23: $2,500.
//   - Line 24: $2,500 − $2,013 = $487 overpayment; line 30 refund $487.
func TestSouthCarolinaSingle(t *testing.T) {
	in := scInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		ilPerson("sam", 1990, 4, 2, false, false), nil, nil,
		[]*formw2.FormW2{scW2(60000_00, 6000_00, 2500_00)},
		&returnsv1.SouthCarolinaInputs{},
	)
	ret, f := prepareSC(t, in)

	want(t, "Line 1 Federal taxable income", f.GetFederalTaxableIncome(), money.USCents(44250_00))
	want(t, "Line 4 Total subtractions", f.GetTotalSubtractions(), money.USCents(0))
	want(t, "Line 5 Income subject to tax", f.GetIncomeSubjectToTax(), money.USCents(44250_00))
	want(t, "Line 6 Tax", f.GetTax(), money.USCents(2013_00))
	want(t, "Line 15 Tax after credits", f.GetTaxAfterNonrefundableCredits(), money.USCents(2013_00))
	want(t, "Line 16 SC withholding", f.GetSouthCarolinaTaxWithheld(), money.USCents(2500_00))
	want(t, "Line 23 Total payments", f.GetTotalPayments(), money.USCents(2500_00))
	want(t, "Line 24 Overpayment", f.GetOverpayment(), money.USCents(487_00))
	want(t, "Line 30 Refund", f.GetRefund(), money.USCents(487_00))
	want(t, "Line 34 Balance due", f.GetBalanceDue(), money.USCents(0))
	want(t, "Summary total tax", ret.GetSummary().GetTotalTax(), money.USCents(2013_00))
}

// A married couple filing jointly with two children, one under 6.
//
//   - Line 1: wages $95,000 + $45,000 = $140,000 less the $31,500 2025
//     joint standard deduction = $108,500.
//   - Line t: one dependent under 6 on December 31 × $4,930 = $4,930
//     (instructions, line t worksheet).
//   - Line w: two dependents × $4,930 = $9,860 (instructions, line w
//     worksheet).
//   - Line 4: $4,930 + $9,860 = $14,790.
//   - Line 5: $108,500 − $14,790 = $93,710.
//   - Line 6: SC1040TT row $93,700–$93,800, middle $93,750:
//     $93,750 × 6% − $642 = $5,625 − $642 = $4,983.
//   - Line 11: child and dependent care credit, an input, $100.
//   - Line 14: $100. Line 15: $4,983 − $100 = $4,883.
//   - Line 16: withholding $4,000 + $2,000 = $6,000. Line 23: $6,000.
//   - Line 24: $6,000 − $4,883 = $1,117; line 30 refund $1,117.
func TestSouthCarolinaMarriedFilingJointlyWithChildren(t *testing.T) {
	in := scInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY,
		ilPerson("ann", 1985, 3, 15, false, false),
		ilPerson("ben", 1984, 8, 20, false, false),
		[]*returnsv1.Dependent{
			ilDependent("cal", 2021, 6, 1),
			ilDependent("dee", 2015, 9, 9),
		},
		[]*formw2.FormW2{scW2(95000_00, 9000_00, 4000_00), scW2(45000_00, 3000_00, 2000_00)},
		&returnsv1.SouthCarolinaInputs{ChildAndDependentCareCredit: cents(100_00)},
	)
	_, f := prepareSC(t, in)

	if f.GetDependents() != 2 || f.GetDependentsUnder_6() != 1 {
		t.Errorf("dependents = %d, under 6 = %d; want 2 and 1", f.GetDependents(), f.GetDependentsUnder_6())
	}
	want(t, "Line 1 Federal taxable income", f.GetFederalTaxableIncome(), money.USCents(108500_00))
	want(t, "Line t Dependents under 6", f.GetYoungDependentsDeduction(), money.USCents(4930_00))
	want(t, "Line w Dependent exemption", f.GetDependentExemption(), money.USCents(9860_00))
	want(t, "Line 4 Total subtractions", f.GetTotalSubtractions(), money.USCents(14790_00))
	want(t, "Line 5 Income subject to tax", f.GetIncomeSubjectToTax(), money.USCents(93710_00))
	want(t, "Line 6 Tax", f.GetTax(), money.USCents(4983_00))
	want(t, "Line 14 Nonrefundable credits", f.GetTotalNonrefundableCredits(), money.USCents(100_00))
	want(t, "Line 15 Tax after credits", f.GetTaxAfterNonrefundableCredits(), money.USCents(4883_00))
	want(t, "Line 23 Total payments", f.GetTotalPayments(), money.USCents(6000_00))
	want(t, "Line 30 Refund", f.GetRefund(), money.USCents(1117_00))
	want(t, "Line 34 Balance due", f.GetBalanceDue(), money.USCents(0))
}

// A high-income single filer aged 65, with retirement income from their own
// plan.
//
//   - Line 1: wages $250,000 less the $15,750 standard deduction and the
//     $2,000 additional deduction for age 65 = $232,250. (The federal senior
//     deduction is phased out: 6% of $175,000 over $75,000 exceeds $6,000.)
//   - Line p-1: qualified retirement income, an input, $4,000; age 65 or
//     older, so up to $10,000 (instructions, line p): $4,000.
//   - Line q-1: $15,000 less the $4,000 on line p-1 = $11,000 (instructions,
//     line q-1 worksheet).
//   - Line 4: $4,000 + $11,000 = $15,000.
//   - Line 5: $232,250 − $15,000 = $217,250.
//   - Line 6: $100,000 or more, so the rate schedule:
//     $217,250 × 6% − $642 = $13,035 − $642 = $12,393.
//   - Line 16: withholding $12,000. Line 23: $12,000.
//   - Line 25: $12,393 − $12,000 = $393; line 34 balance due $393.
//
// The retirement income is given only as the SC input, so it is not in the
// federal figures here; the SC lines are what the test checks.
func TestSouthCarolinaHighIncomeSenior(t *testing.T) {
	in := scInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		ilPerson("eve", 1960, 2, 10, false, false), nil, nil,
		[]*formw2.FormW2{scW2(250000_00, 50000_00, 12000_00)},
		&returnsv1.SouthCarolinaInputs{QualifiedRetirementIncomeTaxpayer: cents(4000_00)},
	)
	_, f := prepareSC(t, in)

	if f.GetTaxpayers_65OrOlder() != 1 {
		t.Errorf("taxpayers 65 or older = %d, want 1", f.GetTaxpayers_65OrOlder())
	}
	want(t, "Line 1 Federal taxable income", f.GetFederalTaxableIncome(), money.USCents(232250_00))
	want(t, "Line p-1 Retirement deduction", f.GetRetirementDeductionTaxpayer(), money.USCents(4000_00))
	want(t, "Line q-1 Age 65 deduction", f.GetAge_65DeductionTaxpayer(), money.USCents(11000_00))
	want(t, "Line 4 Total subtractions", f.GetTotalSubtractions(), money.USCents(15000_00))
	want(t, "Line 5 Income subject to tax", f.GetIncomeSubjectToTax(), money.USCents(217250_00))
	want(t, "Line 6 Tax", f.GetTax(), money.USCents(12393_00))
	want(t, "Line 23 Total payments", f.GetTotalPayments(), money.USCents(12000_00))
	want(t, "Line 25 Amount due", f.GetAmountDue(), money.USCents(393_00))
	want(t, "Line 30 Refund", f.GetRefund(), money.USCents(0))
	want(t, "Line 34 Balance due", f.GetBalanceDue(), money.USCents(393_00))
}

// The SC1040TT's worked example of the rate schedule: income subject to tax
// of $101,000 gives $101,000 × 6% − $642 = $5,418. Wages of $116,750 less the
// $15,750 standard deduction put $101,000 on line 1 and line 5.
//
// The tax tables, from the same booklet: $17,900–$18,000 is $435, and
// $3,600–$3,650 is $2. Wages of $33,700 and $19,375 put $17,950 and $3,625
// on line 5.
func TestSouthCarolinaPublishedTaxFigures(t *testing.T) {
	for _, c := range []struct {
		wages, income, tax int64
	}{
		{116750_00, 101000_00, 5418_00},
		{33700_00, 17950_00, 435_00},
		{19375_00, 3625_00, 2_00},
	} {
		in := scInputs(
			returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
			ilPerson("fay", 1992, 1, 5, false, false), nil, nil,
			[]*formw2.FormW2{scW2(c.wages, 0, 0)},
			&returnsv1.SouthCarolinaInputs{},
		)
		_, f := prepareSC(t, in)
		want(t, "Line 5 Income subject to tax", f.GetIncomeSubjectToTax(), money.USCents(c.income))
		want(t, "Line 6 Tax", f.GetTax(), money.USCents(c.tax))
	}
}
