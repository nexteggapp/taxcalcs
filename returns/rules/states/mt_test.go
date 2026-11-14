package states_test

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
)

// montanataxcalc holds only estimated-tax logic, so there is no Go oracle for
// Form 2. These households are worked by hand from the 2025 Form 2 and its
// instructions; the working is in the comment above each test.

// mtW2 is a Form W-2 with Montana wages and withholding.
func mtW2(wages, fedWithheld, mtWithheld int64) *formw2.FormW2 {
	return &formw2.FormW2{
		WagesTipsOtherCompensation: cents(wages),
		FederalTaxWithheld:         cents(fedWithheld),
		StateTaxWithholding: []*commonv1.StateTaxWithholding{
			{State: "MT", StateIncome: cents(wages), TaxWithheld: cents(mtWithheld)},
		},
	}
}

// mtPrepare prepares a 2025 return with a Montana return and gives back the
// Montana part.
func mtPrepare(t *testing.T, status returnsv1.FilingStatusElection, taxpayer, spouse *returnsv1.Person,
	dependents []*returnsv1.Dependent, w2s []*formw2.FormW2) *returnsv1.MontanaReturn {
	t.Helper()

	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatal(err)
	}
	in := ilInputs(status, taxpayer, spouse, dependents, w2s, 0)
	in.States = []*returnsv1.StateInputs{{
		State:  "MT",
		Inputs: &returnsv1.StateInputs_Mt{Mt: &returnsv1.MontanaInputs{}},
	}}
	out, err := eng.Prepare(in)
	if err != nil {
		t.Fatal(err)
	}
	ret := out.GetStates()[0].GetMt()
	if ret == nil {
		t.Fatal("expected a Montana return")
	}
	if ret.GetStatus() != returnsv1.ReturnStatus_RETURN_STATUS_COMPLETE {
		t.Errorf("expected complete return, got %v: %v", ret.GetStatus(), ret.GetDiagnostics())
	}
	return ret
}

// 1. Single, 37, wages $60,000, Montana withholding $2,500.
//   - Line 1, federal AGI (Form 1040 line 11b): $60,000
//   - Line 2, Form 1040 lines 12e and 13b: the 2025 single standard deduction,
//     $15,750; no Schedule 1-A deduction
//   - Line 3: $60,000 - $15,750 = $44,250
//   - Lines 4 to 6: none; line 7, Montana taxable income: $44,250
//   - Worksheet: no capital gains, so line 12 is the Ordinary Income Tax Table
//     on $44,250 (instructions p. 12, single: $21,100 or greater, 5.9% less
//     $253): $2,610.75, rounded $2,611, less $253 = $2,358
//   - Line 8 = line 10: $2,358
//   - Line 11a and 11, withholding: $2,500; line 21: $2,500
//   - Line 23 and 26, refund: $2,500 - $2,358 = $142
func TestMontanaSingle(t *testing.T) {
	p := ilPerson("john", 1988, 5, 12, false, false)
	ret := mtPrepare(t, returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, p, nil, nil,
		[]*formw2.FormW2{mtW2(60000_00, 6000_00, 2500_00)})
	f := ret.GetForms().GetForm_2()

	want(t, "Line 1 Federal AGI", f.GetFederalAdjustedGrossIncome(), money.USCents(60000_00))
	want(t, "Line 2 Federal deductions", f.GetFederalDeductions(), money.USCents(15750_00))
	want(t, "Line 7 Montana taxable income", f.GetMontanaTaxableIncome(), money.USCents(44250_00))
	want(t, "Line 8 Tax", f.GetTaxBeforeCredits(), money.USCents(2358_00))
	want(t, "Line 10 Tax after credits", f.GetTaxAfterNonrefundableCredits(), money.USCents(2358_00))
	want(t, "Line 21 Total payments", f.GetTotalPayments(), money.USCents(2500_00))
	want(t, "Line 26 Refund", f.GetRefund(), money.USCents(142_00))
	want(t, "Line 22 Tax due", f.GetTaxDue(), money.USCents(0))
}

// 2. Married filing jointly, two children, wages $94,100 and $45,000,
// Montana withholding $4,000 and $2,000.
//   - Line 1: $139,100
//   - Line 2: the 2025 joint standard deduction, $31,500
//   - Line 3 = line 7: $107,600
//   - Ordinary Income Tax Table (instructions p. 12, joint: $42,200 or
//     greater, 5.9% less $506): $6,348.40, rounded $6,348, less $506 = $5,842
//   - Line 8 = line 10: $5,842. The children change nothing: Montana has no
//     dependent exemption, and the federal EIC is nil at this income, so
//     line 15 is nil.
//   - Line 21: $6,000; line 26, refund: $6,000 - $5,842 = $158
func TestMontanaMarriedFilingJointly(t *testing.T) {
	p1 := ilPerson("alice", 1985, 3, 15, false, false)
	p2 := ilPerson("bob", 1984, 8, 20, false, false)
	kids := []*returnsv1.Dependent{
		ilDependent("kid1", 2015, 6, 1),
		ilDependent("kid2", 2018, 9, 1),
	}
	ret := mtPrepare(t, returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY, p1, p2, kids,
		[]*formw2.FormW2{mtW2(94100_00, 9000_00, 4000_00), mtW2(45000_00, 4000_00, 2000_00)})
	f := ret.GetForms().GetForm_2()

	want(t, "Line 1 Federal AGI", f.GetFederalAdjustedGrossIncome(), money.USCents(139100_00))
	want(t, "Line 2 Federal deductions", f.GetFederalDeductions(), money.USCents(31500_00))
	want(t, "Line 7 Montana taxable income", f.GetMontanaTaxableIncome(), money.USCents(107600_00))
	want(t, "Line 8 Tax", f.GetTaxBeforeCredits(), money.USCents(5842_00))
	want(t, "Line 15 Earned income credit", f.GetEarnedIncomeCredit(), money.USCents(0))
	want(t, "Line 21 Total payments", f.GetTotalPayments(), money.USCents(6000_00))
	want(t, "Line 26 Refund", f.GetRefund(), money.USCents(158_00))
}

// 4. Married filing jointly, both 70, wages $60,000 and $40,000, Montana
// withholding $2,000.
//   - Line 1: $100,000
//   - Line 2: Form 1040 line 12e, the joint standard deduction $31,500 plus
//     $1,600 for each spouse 65 or older = $34,700; line 13b, the Schedule
//     1-A enhanced deduction for seniors, $6,000 each with income under
//     $150,000 = $12,000; together $46,700
//   - Line 3: $53,300
//   - Line 6: both 65 or older, $11,320 (form line 6)
//   - Line 7: $53,300 - $11,320 = $41,980
//   - Ordinary Income Tax Table, joint, below $42,200 at 4.7%: $1,973.06,
//     rounded $1,973
//   - Line 21: $2,000; line 26, refund: $27
func TestMontanaRetiredCouple(t *testing.T) {
	p1 := ilPerson("ruth", 1955, 2, 10, false, false)
	p2 := ilPerson("sam", 1954, 7, 4, false, false)
	ret := mtPrepare(t, returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY, p1, p2, nil,
		[]*formw2.FormW2{mtW2(60000_00, 5000_00, 1200_00), mtW2(40000_00, 3000_00, 800_00)})
	f := ret.GetForms().GetForm_2()

	want(t, "Line 2 Federal deductions", f.GetFederalDeductions(), money.USCents(46700_00))
	want(t, "Line 6 Age 65 subtraction", f.GetAge_65Subtraction(), money.USCents(11320_00))
	want(t, "Line 7 Montana taxable income", f.GetMontanaTaxableIncome(), money.USCents(41980_00))
	want(t, "Line 8 Tax", f.GetTaxBeforeCredits(), money.USCents(1973_00))
	want(t, "Line 26 Refund", f.GetRefund(), money.USCents(27_00))
}

// 3. Single, high income: wages $250,000, Montana withholding $13,000.
//   - Line 1: $250,000; line 2: $15,750; line 3 = line 7: $234,250
//   - Ordinary Income Tax Table, single, 5.9% less $253: $13,820.75,
//     rounded $13,821, less $253 = $13,568
//   - Line 21: $13,000; line 22, tax due: $13,568 - $13,000 = $568
func TestMontanaHighIncome(t *testing.T) {
	p := ilPerson("dana", 1970, 1, 20, false, false)
	ret := mtPrepare(t, returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, p, nil, nil,
		[]*formw2.FormW2{mtW2(250000_00, 50000_00, 13000_00)})
	f := ret.GetForms().GetForm_2()

	want(t, "Line 7 Montana taxable income", f.GetMontanaTaxableIncome(), money.USCents(234250_00))
	want(t, "Line 8 Tax", f.GetTaxBeforeCredits(), money.USCents(13568_00))
	want(t, "Line 21 Total payments", f.GetTotalPayments(), money.USCents(13000_00))
	want(t, "Line 22 Tax due", f.GetTaxDue(), money.USCents(568_00))
	want(t, "Line 26 Refund", f.GetRefund(), money.USCents(0))
}
