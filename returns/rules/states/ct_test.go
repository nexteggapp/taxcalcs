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
// worked by hand from the 2025 Form CT-1040 instructions (Rev. 12/25): the
// Tax Calculation Schedule on page 19 and its tables, by PDF page:
//   - Table A, Personal Exemptions (page 19);
//   - Table B, Initial Tax Calculation (page 20), the rate schedules of
//     Conn. Gen. Stat. § 12-700(a);
//   - Table C, 2% Tax Rate Phase-Out Add-Back (page 21);
//   - Table D, Tax Recapture (page 22);
//   - Table E, Personal Tax Credits (page 23).
//
// Each table row reads "more than" its lower bound and "less than or equal
// to" its upper one, and Tables A, C, D and E are read at Connecticut AGI.
// Every line of the schedule is in whole dollars.

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

// 1. Single filer, $40,000 of wages: a partial personal exemption and a
// personal tax credit.
//
//   - Line 1 federal AGI: $40,000.00. Lines 2 and 4: none. Line 5: $40,000.00,
//     above the single no-tax ceiling of $15,000.
//   - Tax Calculation Schedule:
//   - line 1: $40,000
//   - line 2, Table A single, more than $39,000 up to $40,000: $5,000
//   - line 3: $40,000 - $5,000 = $35,000
//   - line 4, Table B single: $200 plus 4.5% of the excess over $10,000;
//     $25,000 x .045 = $1,125; $200 + $1,125 = $1,325
//   - line 5, Table C single, $56,500 or less: $0
//   - line 6, Table D single, $105,000 or less: $0
//   - line 7: $1,325
//   - line 8, Table E single, more than $33,300 up to $60,000: .10
//   - line 9: $1,325 x .10 = $132.50, entered as $133
//   - line 10: $1,325 - $133 = $1,192
//   - Line 6: $1,192.00. Lines 7 to 13: none. Lines 14, 16, 17: $1,192.00.
//   - Line 18 withheld: $1,300.00. Line 21: $1,300.00.
//   - Line 22 overpayment and line 25 refund: $1,300 - $1,192 = $108.00.
//   - Summary: deductions $5,000.00 (the exemption), taxable $35,000.00.
func TestConnecticutSingle(t *testing.T) {
	in := ctInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		ilPerson("dana", 1990, 2, 3, false, false), nil, nil,
		[]*formw2.FormW2{ctW2(40000_00, 3000_00, 1300_00)},
		&returnsv1.ConnecticutInputs{},
	)
	ret, f := prepareCT(t, in)

	want(t, "Line 1 Federal AGI", f.GetFederalAdjustedGrossIncome(), money.USCents(40000_00))
	want(t, "Line 5 Connecticut AGI", f.GetConnecticutAdjustedGrossIncome(), money.USCents(40000_00))
	want(t, "Line 6 Income tax", f.GetIncomeTaxBeforeCredits(), money.USCents(1192_00))
	want(t, "Line 14 Connecticut income tax", f.GetIncomeTax(), money.USCents(1192_00))
	want(t, "Line 16 Total tax", f.GetTotalTax(), money.USCents(1192_00))
	want(t, "Line 18 Withheld", f.GetWithholding(), money.USCents(1300_00))
	want(t, "Line 21 Total payments", f.GetTotalPayments(), money.USCents(1300_00))
	want(t, "Line 25 Refund", f.GetRefund(), money.USCents(108_00))
	want(t, "Line 30 Total amount due", f.GetTotalAmountDue(), money.USCents(0))
	if f.GetFilingStatus() != formct1040.FilingStatus_FILING_STATUS_SINGLE {
		t.Errorf("filing status = %v, want single", f.GetFilingStatus())
	}
	want(t, "Summary deductions", ret.GetSummary().GetTotalDeductions(), money.USCents(5000_00))
	want(t, "Summary taxable income", ret.GetSummary().GetTaxableIncome(), money.USCents(35000_00))
	want(t, "Summary total tax", ret.GetSummary().GetTotalTax(), money.USCents(1192_00))
	complete(t, "CT", ret.GetStatus(), ret.GetDiagnostics())
}

// 2. Married filing jointly, $46,500 of wages and two children: the full
// joint exemption, and Table B's own joint example.
//
//   - Line 1 federal AGI: $46,500.00. Line 5: $46,500.00, above the joint
//     no-tax ceiling of $24,000.
//   - Tax Calculation Schedule:
//   - line 2, Table A joint, $48,000 or less: $24,000
//   - line 3: $46,500 - $24,000 = $22,500
//   - line 4, Table B joint: $400 plus 4.5% of the excess over $20,000;
//     $2,500 x .045 = $112.50, entered as $113 (the instructions' example:
//     "Line 3 is $22,500, Line 4 is $513"); $513
//   - lines 5 and 6, Tables C and D: $0
//   - line 7: $513
//   - line 8, Table E joint, more than $41,500 up to $50,000: .15
//   - line 9: $513 x .15 = $76.95, entered as $77
//   - line 10: $513 - $77 = $436
//   - Line 6: $436.00. Line 18 withheld: $500.00. Line 25 refund: $64.00.
func TestConnecticutMarriedFilingJointlyLowIncome(t *testing.T) {
	in := ctInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY,
		ilPerson("alex", 1985, 3, 15, false, false),
		ilPerson("blair", 1984, 8, 20, false, false),
		[]*returnsv1.Dependent{ilDependent("casey", 2016, 6, 1), ilDependent("drew", 2019, 9, 9)},
		[]*formw2.FormW2{ctW2(46500_00, 0, 500_00)},
		&returnsv1.ConnecticutInputs{},
	)
	ret, f := prepareCT(t, in)

	want(t, "Line 5 Connecticut AGI", f.GetConnecticutAdjustedGrossIncome(), money.USCents(46500_00))
	want(t, "Line 6 Income tax", f.GetIncomeTaxBeforeCredits(), money.USCents(436_00))
	want(t, "Line 16 Total tax", f.GetTotalTax(), money.USCents(436_00))
	want(t, "Line 25 Refund", f.GetRefund(), money.USCents(64_00))
	want(t, "Summary deductions", ret.GetSummary().GetTotalDeductions(), money.USCents(24000_00))
	want(t, "Summary taxable income", ret.GetSummary().GetTaxableIncome(), money.USCents(22500_00))
	complete(t, "CT", ret.GetStatus(), ret.GetDiagnostics())
}

// 3. Married filing jointly, two earners and two children, $140,000 of wages:
// the 2% phase-out add-back and a property tax credit.
//
//   - Line 1 federal AGI: $95,000 + $45,000 = $140,000.00. Line 5: $140,000.00.
//   - Tax Calculation Schedule:
//   - line 2, Table A joint, more than $71,000: $0
//   - line 3: $140,000
//   - line 4, Table B joint: $4,000 plus 5.5% of the excess over $100,000;
//     $40,000 x .055 = $2,200; $6,200
//   - line 5, Table C joint, more than $135,500 up to $140,500: $400
//   - line 6, Table D joint, $210,000 or less: $0
//   - line 7: $6,200 + $400 = $6,600
//   - line 8, Table E joint, more than $100,500: .00; line 9: $0
//   - line 10: $6,600
//   - Line 6: $6,600.00. Line 11 property tax credit, input from Schedule 3:
//     $300.00. Lines 12 and 14: $6,300.00. Lines 16, 17: $6,300.00.
//   - Line 18 withheld: $4,000 + $2,000 = $6,000.00. Line 21: $6,000.00.
//   - Line 26 tax due and line 30: $6,300 - $6,000 = $300.00. Line 25: $0.00.
func TestConnecticutMarriedFilingJointly(t *testing.T) {
	in := ctInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY,
		ilPerson("alex", 1985, 3, 15, false, false),
		ilPerson("blair", 1984, 8, 20, false, false),
		[]*returnsv1.Dependent{ilDependent("casey", 2016, 6, 1), ilDependent("drew", 2019, 9, 9)},
		[]*formw2.FormW2{ctW2(95000_00, 9000_00, 4000_00), ctW2(45000_00, 3500_00, 2000_00)},
		&returnsv1.ConnecticutInputs{PropertyTaxCredit: cents(300_00)},
	)
	_, f := prepareCT(t, in)

	want(t, "Line 5 Connecticut AGI", f.GetConnecticutAdjustedGrossIncome(), money.USCents(140000_00))
	want(t, "Line 6 Income tax", f.GetIncomeTaxBeforeCredits(), money.USCents(6600_00))
	want(t, "Line 11 Property tax credit", f.GetPropertyTaxCredit(), money.USCents(300_00))
	want(t, "Line 14 Connecticut income tax", f.GetIncomeTax(), money.USCents(6300_00))
	want(t, "Line 18 Withheld", f.GetWithholding(), money.USCents(6000_00))
	want(t, "Line 21 Total payments", f.GetTotalPayments(), money.USCents(6000_00))
	want(t, "Line 25 Refund", f.GetRefund(), money.USCents(0))
	want(t, "Line 26 Tax due", f.GetTaxDue(), money.USCents(300_00))
	want(t, "Line 30 Total amount due", f.GetTotalAmountDue(), money.USCents(300_00))
}

// 4. Head of household with one child and $250,000 of wages: the add-back
// and the recapture at the head of household columns.
//
//   - Line 1 federal AGI and line 5: $250,000.00.
//   - Tax Calculation Schedule:
//   - line 2, Table A head of household, more than $56,000: $0
//   - line 3: $250,000
//   - line 4, Table B head of household: $7,600 plus 6.0% of the excess over
//     $160,000; $90,000 x .06 = $5,400; $13,000
//   - line 5, Table C head of household, more than $114,500: $400
//   - line 6, Table D head of household, more than $240,000 up to $320,000:
//     $400
//   - line 7: $13,000 + $400 + $400 = $13,800
//   - line 8, Table E head of household, more than $78,500: .00; line 9: $0
//   - line 10: $13,800
//   - Line 6 and lines 14, 16, 17: $13,800.00.
//   - Line 18 withheld: $14,000.00. Line 25 refund: $200.00.
func TestConnecticutHeadOfHousehold(t *testing.T) {
	in := ctInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_HEAD_OF_HOUSEHOLD,
		ilPerson("frankie", 1978, 4, 12, false, false), nil,
		[]*returnsv1.Dependent{ilDependent("gale", 2012, 7, 4)},
		[]*formw2.FormW2{ctW2(250000_00, 50000_00, 14000_00)},
		&returnsv1.ConnecticutInputs{},
	)
	ret, f := prepareCT(t, in)

	if f.GetFilingStatus() != formct1040.FilingStatus_FILING_STATUS_HEAD_OF_HOUSEHOLD {
		t.Errorf("filing status = %v, want head of household", f.GetFilingStatus())
	}
	want(t, "Line 5 Connecticut AGI", f.GetConnecticutAdjustedGrossIncome(), money.USCents(250000_00))
	want(t, "Line 6 Income tax", f.GetIncomeTaxBeforeCredits(), money.USCents(13800_00))
	want(t, "Line 16 Total tax", f.GetTotalTax(), money.USCents(13800_00))
	want(t, "Line 25 Refund", f.GetRefund(), money.USCents(200_00))
	want(t, "Summary deductions", ret.GetSummary().GetTotalDeductions(), money.USCents(0))
	complete(t, "CT", ret.GetStatus(), ret.GetDiagnostics())
}

// 5. High-income single filer reaching the top rate, with a Schedule 1
// subtraction and estimated payments: the largest add-back and recapture.
//
//   - Line 1 federal AGI: $605,000.00. Line 4 subtraction, input from
//     Schedule 1: $5,000.00. Line 5: $600,000.00.
//   - Tax Calculation Schedule:
//   - line 2, Table A single, more than $44,000: $0
//   - line 3: $600,000
//   - line 4, Table B single: $31,250 plus 6.99% of the excess over $500,000;
//     $100,000 x .0699 = $6,990; $38,240
//   - line 5, Table C single, more than $101,500: $250
//   - line 6, Table D single, more than $540,000: $3,400
//   - line 7: $38,240 + $250 + $3,400 = $41,890
//   - line 8, Table E single, more than $64,500: .00; line 9: $0
//   - line 10: $41,890
//   - Line 6 and lines 14, 16, 17: $41,890.00.
//   - Line 18 withheld: $39,000.00. Line 19 estimated: $2,000.00. Line 21:
//     $41,000.00.
//   - Line 26 and line 30: $41,890 - $41,000 = $890.00.
func TestConnecticutHighIncome(t *testing.T) {
	in := ctInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		ilPerson("emery", 1960, 1, 5, false, false), nil, nil,
		[]*formw2.FormW2{ctW2(605000_00, 150000_00, 39000_00)},
		&returnsv1.ConnecticutInputs{
			Subtractions:      cents(5000_00),
			EstimatedPayments: cents(2000_00),
		},
	)
	_, f := prepareCT(t, in)

	want(t, "Line 1 Federal AGI", f.GetFederalAdjustedGrossIncome(), money.USCents(605000_00))
	want(t, "Line 5 Connecticut AGI", f.GetConnecticutAdjustedGrossIncome(), money.USCents(600000_00))
	want(t, "Line 6 Income tax", f.GetIncomeTaxBeforeCredits(), money.USCents(41890_00))
	want(t, "Line 16 Total tax", f.GetTotalTax(), money.USCents(41890_00))
	want(t, "Line 21 Total payments", f.GetTotalPayments(), money.USCents(41000_00))
	want(t, "Line 25 Refund", f.GetRefund(), money.USCents(0))
	want(t, "Line 30 Total amount due", f.GetTotalAmountDue(), money.USCents(890_00))
}

// 6. Single filer at the no-tax ceiling: $15,000 of wages.
//
//   - Line 5: $15,000.00, which is $15,000 or less, so line 6 is $0 (and the
//     schedule would agree: Table A gives $15,000, leaving no taxable income).
//   - Line 18 withheld: $50.00, all refunded on line 25.
func TestConnecticutAtNoTaxCeiling(t *testing.T) {
	in := ctInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		ilPerson("hollis", 2000, 9, 30, false, false), nil, nil,
		[]*formw2.FormW2{ctW2(15000_00, 0, 50_00)},
		&returnsv1.ConnecticutInputs{},
	)
	ret, f := prepareCT(t, in)

	want(t, "Line 6 Income tax", f.GetIncomeTaxBeforeCredits(), money.USCents(0))
	want(t, "Line 25 Refund", f.GetRefund(), money.USCents(50_00))
	want(t, "Summary taxable income", ret.GetSummary().GetTaxableIncome(), money.USCents(0))
	complete(t, "CT", ret.GetStatus(), ret.GetDiagnostics())
}
