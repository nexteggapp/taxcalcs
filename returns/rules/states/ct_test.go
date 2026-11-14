package states_test

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
	formct1040 "github.com/nexteggapp/taxprotos/go/us/state/ct/v1/form_ct1040"
)

// connecticuttaxcalc holds only estimated-tax logic, so these households are
// worked by hand from the 2025 Form CT-1040 (Rev. 12/25) and the rate
// schedules of Conn. Gen. Stat. § 12-700(a).
//
// The personal exemption (Table A), the 3% phase-out add-back (Table C), the
// benefit recapture (Table D) and the personal tax credit decimal (Table E)
// are inputs to the rules, not computed by them. The values each test passes
// are stated as inputs: the tests check what the rules do with them, not the
// tables.

func ctInputs(
	status returnsv1.FilingStatusElection,
	taxpayer, spouse *returnsv1.Person,
	dependents []*returnsv1.Dependent,
	w2s []*formw2.FormW2,
	ct *returnsv1.ConnecticutInputs,
) *returnsv1.ReturnInputs {
	in := ilInputs(status, taxpayer, spouse, dependents, w2s, 0)
	in.States = []*returnsv1.StateInputs{{
		State:  "CT",
		Inputs: &returnsv1.StateInputs_Ct{Ct: ct},
	}}
	return in
}

func ctW2(wages, fedWithheld, ctWithheld int64) *formw2.FormW2 {
	return &formw2.FormW2{
		WagesTipsOtherCompensation: cents(wages),
		FederalTaxWithheld:         cents(fedWithheld),
		StateTaxWithholding: []*commonv1.StateTaxWithholding{
			{State: "CT", StateIncome: cents(wages), TaxWithheld: cents(ctWithheld)},
		},
	}
}

func prepareCT(t *testing.T, in *returnsv1.ReturnInputs) (*returnsv1.ConnecticutReturn, *formct1040.FormCT1040) {
	t.Helper()
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatal(err)
	}
	out, err := eng.Prepare(in)
	if err != nil {
		t.Fatal(err)
	}
	ret := out.GetStates()[0].GetCt()
	if ret == nil {
		t.Fatal("expected Connecticut return")
	}
	return ret, ret.GetForms().GetFormCt1040()
}

// 1. Single filer, $30,000 of wages, with the personal exemption and the
// personal tax credit.
//
//   - Line 1 federal AGI: $30,000.00. Lines 2 and 4: none. Line 5: $30,000.00.
//   - Tax Calculation Schedule (instructions):
//   - personal exemption, input from Table A: $15,000.00
//   - taxable: $30,000 - $15,000 = $15,000.00
//   - initial tax, § 12-700(a) single: 2% of $10,000 = $200.00, plus 4.5% of
//     $5,000 = $225.00; $425.00
//   - 3% phase-out add-back (Table C) and recapture (Table D), inputs: none
//   - personal tax credit decimal, input from Table E: .40; $425 x .40 = $170.00
//   - Line 6: $425 - $170 = $255.00. Lines 7 to 13: none. Lines 14, 16, 17: $255.00.
//   - Line 18 withheld: $400.00. Line 21: $400.00.
//   - Line 22 overpayment and line 25 refund: $400 - $255 = $145.00. Line 30: $0.00.
func TestConnecticutSingle(t *testing.T) {
	in := ctInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		ilPerson("dana", 1990, 2, 3, false, false), nil, nil,
		[]*formw2.FormW2{ctW2(30000_00, 1500_00, 400_00)},
		&returnsv1.ConnecticutInputs{
			PersonalExemption:           cents(15000_00),
			PersonalTaxCreditHundredths: 40,
		},
	)
	ret, f := prepareCT(t, in)

	want(t, "Line 1 Federal AGI", f.GetFederalAdjustedGrossIncome(), money.USCents(30000_00))
	want(t, "Line 5 Connecticut AGI", f.GetConnecticutAdjustedGrossIncome(), money.USCents(30000_00))
	want(t, "Line 6 Income tax", f.GetIncomeTaxBeforeCredits(), money.USCents(255_00))
	want(t, "Line 14 Connecticut income tax", f.GetIncomeTax(), money.USCents(255_00))
	want(t, "Line 16 Total tax", f.GetTotalTax(), money.USCents(255_00))
	want(t, "Line 18 Withheld", f.GetWithholding(), money.USCents(400_00))
	want(t, "Line 21 Total payments", f.GetTotalPayments(), money.USCents(400_00))
	want(t, "Line 25 Refund", f.GetRefund(), money.USCents(145_00))
	want(t, "Line 30 Total amount due", f.GetTotalAmountDue(), money.USCents(0))
	if f.GetFilingStatus() != formct1040.FilingStatus_FILING_STATUS_SINGLE {
		t.Errorf("filing status = %v, want single", f.GetFilingStatus())
	}
	want(t, "Summary total tax", ret.GetSummary().GetTotalTax(), money.USCents(255_00))
}

// 2. Married filing jointly, two earners and two children, with the 3%
// phase-out add-back and a property tax credit.
//
//   - Line 1 federal AGI: $95,000 + $45,000 = $140,000.00. Line 5: $140,000.00.
//   - Tax Calculation Schedule:
//   - personal exemption, input from Table A: none (joint exemption fully
//     phased out at this income)
//   - initial tax, § 12-700(a) joint: 2% of $20,000 = $400.00, 4.5% of $80,000
//     = $3,600.00, 5.5% of $40,000 = $2,200.00; $6,200.00
//   - 3% phase-out add-back, input from Table C: $160.00
//   - recapture and personal tax credit, inputs: none
//   - Line 6: $6,360.00. Line 11 property tax credit, input from Schedule 3:
//     $300.00. Line 12 and line 14: $6,060.00. Lines 16, 17: $6,060.00.
//   - Line 18 withheld: $4,000 + $2,000 = $6,000.00. Line 21: $6,000.00.
//   - Line 26 tax due and line 30: $6,060 - $6,000 = $60.00. Line 25: $0.00.
func TestConnecticutMarriedFilingJointly(t *testing.T) {
	in := ctInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY,
		ilPerson("alex", 1985, 3, 15, false, false),
		ilPerson("blair", 1984, 8, 20, false, false),
		[]*returnsv1.Dependent{ilDependent("casey", 2016, 6, 1), ilDependent("drew", 2019, 9, 9)},
		[]*formw2.FormW2{ctW2(95000_00, 9000_00, 4000_00), ctW2(45000_00, 3500_00, 2000_00)},
		&returnsv1.ConnecticutInputs{
			PhaseOutAddBack:   cents(160_00),
			PropertyTaxCredit: cents(300_00),
		},
	)
	_, f := prepareCT(t, in)

	want(t, "Line 5 Connecticut AGI", f.GetConnecticutAdjustedGrossIncome(), money.USCents(140000_00))
	want(t, "Line 6 Income tax", f.GetIncomeTaxBeforeCredits(), money.USCents(6360_00))
	want(t, "Line 11 Property tax credit", f.GetPropertyTaxCredit(), money.USCents(300_00))
	want(t, "Line 14 Connecticut income tax", f.GetIncomeTax(), money.USCents(6060_00))
	want(t, "Line 18 Withheld", f.GetWithholding(), money.USCents(6000_00))
	want(t, "Line 21 Total payments", f.GetTotalPayments(), money.USCents(6000_00))
	want(t, "Line 25 Refund", f.GetRefund(), money.USCents(0))
	want(t, "Line 26 Tax due", f.GetTaxDue(), money.USCents(60_00))
	want(t, "Line 30 Total amount due", f.GetTotalAmountDue(), money.USCents(60_00))
}

// 3. High-income single filer reaching the top rate, with the add-back, the
// benefit recapture, a Schedule 1 subtraction and estimated payments.
//
//   - Line 1 federal AGI: $605,000.00. Line 4 subtraction, input from
//     Schedule 1: $5,000.00. Line 5: $600,000.00.
//   - Tax Calculation Schedule:
//   - personal exemption and personal tax credit, inputs: none
//   - initial tax, § 12-700(a) single: $200.00 (2% of $10,000) + $1,800.00
//     (4.5% of $40,000) + $2,750.00 (5.5% of $50,000) + $6,000.00 (6% of
//     $100,000) + $3,250.00 (6.5% of $50,000) + $17,250.00 (6.9% of $250,000)
//   - $6,990.00 (6.99% of $100,000) = $38,240.00
//   - add-back, input from Table C: $200.00; recapture, input from Table D:
//     $3,150.00
//   - Line 6: $38,240 + $200 + $3,150 = $41,590.00. Lines 14, 16, 17: $41,590.00.
//   - Line 18 withheld: $39,000.00. Line 19 estimated: $2,000.00. Line 21: $41,000.00.
//   - Line 26 and line 30: $41,590 - $41,000 = $590.00.
func TestConnecticutHighIncome(t *testing.T) {
	in := ctInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		ilPerson("emery", 1960, 1, 5, false, false), nil, nil,
		[]*formw2.FormW2{ctW2(605000_00, 150000_00, 39000_00)},
		&returnsv1.ConnecticutInputs{
			Subtractions:      cents(5000_00),
			PhaseOutAddBack:   cents(200_00),
			BenefitRecapture:  cents(3150_00),
			EstimatedPayments: cents(2000_00),
		},
	)
	_, f := prepareCT(t, in)

	want(t, "Line 1 Federal AGI", f.GetFederalAdjustedGrossIncome(), money.USCents(605000_00))
	want(t, "Line 5 Connecticut AGI", f.GetConnecticutAdjustedGrossIncome(), money.USCents(600000_00))
	want(t, "Line 6 Income tax", f.GetIncomeTaxBeforeCredits(), money.USCents(41590_00))
	want(t, "Line 16 Total tax", f.GetTotalTax(), money.USCents(41590_00))
	want(t, "Line 21 Total payments", f.GetTotalPayments(), money.USCents(41000_00))
	want(t, "Line 25 Refund", f.GetRefund(), money.USCents(0))
	want(t, "Line 30 Total amount due", f.GetTotalAmountDue(), money.USCents(590_00))
}
