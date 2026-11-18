package states_test

// There is no Go income tax calculator for the District of Columbia, so these tests check the D-40 against figures worked by hand
// from the 2025 D-40 booklet: the standard deduction amounts (booklet page 13
// and the "DC Basic Standard Deduction" note), the tax tables for taxable
// income of $100,000 or less (pages 93 to 102) and Calculation I, the tax rate
// schedule (page 20).

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
	formd40 "github.com/nexteggapp/taxprotos/go/us/state/dc/v1/form_d40"
)

func dcW2(wages, fedWithheld, dcWithheld int64) *formw2.FormW2 {
	return &formw2.FormW2{
		WagesTipsOtherCompensation: cents(wages),
		FederalTaxWithheld:         cents(fedWithheld),
		StateTaxWithholding: []*commonv1.StateTaxWithholding{
			{State: "DC", StateIncome: cents(wages), TaxWithheld: cents(dcWithheld)},
		},
	}
}

// dcPrepare builds the household, prepares it, and returns the D-40 with its
// return.
func dcPrepare(
	t *testing.T,
	status returnsv1.FilingStatusElection,
	taxpayer, spouse *returnsv1.Person,
	dependents []*returnsv1.Dependent,
	w2s []*formw2.FormW2,
) (*returnsv1.DistrictOfColumbiaReturn, *formd40.FormD40) {
	t.Helper()
	in := ilInputs(status, taxpayer, spouse, dependents, w2s, 0)
	in.States = []*returnsv1.StateInputs{{
		State:  "DC",
		Inputs: &returnsv1.StateInputs_Dc{Dc: &returnsv1.DistrictOfColumbiaInputs{}},
	}}

	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatal(err)
	}
	out, err := eng.Prepare(in)
	if err != nil {
		t.Fatal(err)
	}
	ret := out.GetStates()[0].GetDc()
	if ret == nil {
		t.Fatal("expected a District of Columbia return")
	}
	if ret.GetStatus() != returnsv1.ReturnStatus_RETURN_STATUS_COMPLETE {
		t.Errorf("expected a complete return, got %v: %v", ret.GetStatus(), ret.GetDiagnostics())
	}
	return ret, ret.GetForms().GetFormD40()
}

// A single filer with $60,000 of wages.
//   - Line 4 federal AGI and line 16 DC AGI: $60,000 (no additions or subtractions).
//   - Line 18: $15,000, the single standard deduction (booklet, "DC Basic Standard Deduction").
//   - Line 19: $60,000 - $15,000 = $45,000.
//   - Line 20: the tax table row $45,000-45,049 prints $2,527 (booklet p. 96;
//     $2,200 + 6.5% of the $5,025 over $40,000 at the row's midpoint, $2,526.63).
//   - Line 26 total tax: $2,527. Line 31 withheld: $3,000.
//   - Line 38 overpaid and line 43 refund: $3,000 - $2,527 = $473.
func TestDistrictOfColumbiaSingle(t *testing.T) {
	ret, f := dcPrepare(t,
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		ilPerson("dana", 1990, 4, 2, false, false), nil, nil,
		[]*formw2.FormW2{dcW2(60000_00, 6000_00, 3000_00)})

	if f.GetFilingStatus() != formd40.FilingStatus_FILING_STATUS_SINGLE {
		t.Errorf("filing status = %v", f.GetFilingStatus())
	}
	want(t, "Line 4 federal AGI", f.GetFederalAdjustedGrossIncome(), money.USCents(60000_00))
	want(t, "Line 16 DC AGI", f.GetDcAdjustedGrossIncome(), money.USCents(60000_00))
	want(t, "Line 18 deduction", f.GetDeduction(), money.USCents(15000_00))
	want(t, "Line 19 taxable income", f.GetTaxableIncome(), money.USCents(45000_00))
	want(t, "Line 20 tax", f.GetTax(), money.USCents(2527_00))
	want(t, "Line 26 total tax", f.GetTotalTax(), money.USCents(2527_00))
	want(t, "Line 31 withheld", f.GetDcTaxWithheld(), money.USCents(3000_00))
	want(t, "Line 36 payments", f.GetTotalPaymentsAndRefundableCredits(), money.USCents(3000_00))
	want(t, "Line 37 tax due", f.GetTaxDue(), money.USCents(0))
	want(t, "Line 43 refund", f.GetRefund(), money.USCents(473_00))
	want(t, "Summary refund", ret.GetSummary().GetRefund(), money.USCents(473_00))
}

// A single filer whose taxable income falls in the 8.5% band of the tax table.
//   - Line 16: $80,000. Line 18: $15,000. Line 19: $65,000.
//   - Line 20: the table row $65,000-65,049 prints $3,926 (booklet p. 98).
//     The rate schedule at the midpoint gives $3,500 + 8.5% of $5,025 =
//     $3,927.13, a dollar more: from $60,000 every row of the printed table is
//     a dollar below the schedule, and the booklet says to use the table.
//   - Line 31 withheld: $4,000. Line 43 refund: $4,000 - $3,926 = $74.
func TestDistrictOfColumbiaTaxTableUpperBand(t *testing.T) {
	_, f := dcPrepare(t,
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		ilPerson("eli", 1985, 1, 9, false, false), nil, nil,
		[]*formw2.FormW2{dcW2(80000_00, 9000_00, 4000_00)})

	want(t, "Line 19 taxable income", f.GetTaxableIncome(), money.USCents(65000_00))
	want(t, "Line 20 tax", f.GetTax(), money.USCents(3926_00))
	want(t, "Line 43 refund", f.GetRefund(), money.USCents(74_00))
}

// Married filing jointly with two children, wages of $95,000 and $45,000.
//   - Line 16: $140,000. No federal earned income credit at this income, so
//     lines 27c and 27d are zero.
//   - Line 18: $30,000, the joint standard deduction.
//   - Line 19: $140,000 - $30,000 = $110,000, over $100,000, so Calculation I.
//   - Line 20: $3,500 + 8.5% of the $50,000 over $60,000 = $3,500 + $4,250 = $7,750.
//   - Line 31 withheld: $6,000 + $2,500 = $8,500.
//   - Line 43 refund: $8,500 - $7,750 = $750.
func TestDistrictOfColumbiaMarriedFilingJointly(t *testing.T) {
	ret, f := dcPrepare(t,
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY,
		ilPerson("fran", 1984, 6, 1, false, false),
		ilPerson("gus", 1983, 2, 14, false, false),
		[]*returnsv1.Dependent{ilDependent("hal", 2015, 3, 3), ilDependent("ivy", 2018, 7, 7)},
		[]*formw2.FormW2{dcW2(95000_00, 9000_00, 6000_00), dcW2(45000_00, 3000_00, 2500_00)})

	want(t, "Line 16 DC AGI", f.GetDcAdjustedGrossIncome(), money.USCents(140000_00))
	want(t, "Line 18 deduction", f.GetDeduction(), money.USCents(30000_00))
	want(t, "Line 19 taxable income", f.GetTaxableIncome(), money.USCents(110000_00))
	want(t, "Line 20 tax", f.GetTax(), money.USCents(7750_00))
	want(t, "Line 27d EITC", f.GetEarnedIncomeCreditWithChildren(), money.USCents(0))
	want(t, "Line 31 withheld", f.GetDcTaxWithheld(), money.USCents(8500_00))
	want(t, "Line 43 refund", f.GetRefund(), money.USCents(750_00))
	want(t, "Line 42 amount due", f.GetTotalAmountDue(), money.USCents(0))
	want(t, "Summary tax", ret.GetSummary().GetTotalTax(), money.USCents(7750_00))
}

// A retired couple filing jointly, both 65 or older, with $80,000 of wages
// from one of them.
//   - Line 16: $80,000.
//   - Line 18: $30,000 + 2 x $1,600 additional standard deduction (married,
//     born before January 2, 1961) = $33,200.
//   - Line 19: $80,000 - $33,200 = $46,800.
//   - Line 20: the table row $46,800-46,849 prints $2,644 (booklet p. 96).
//   - Line 31 withheld: $2,000. Line 37 tax due and line 42: $644.
func TestDistrictOfColumbiaRetiredCouple(t *testing.T) {
	_, f := dcPrepare(t,
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY,
		ilPerson("jo", 1950, 5, 5, false, false),
		ilPerson("kim", 1952, 8, 8, false, false),
		nil,
		[]*formw2.FormW2{dcW2(80000_00, 7000_00, 2000_00)})

	want(t, "Line 18 deduction", f.GetDeduction(), money.USCents(33200_00))
	want(t, "Line 19 taxable income", f.GetTaxableIncome(), money.USCents(46800_00))
	want(t, "Line 20 tax", f.GetTax(), money.USCents(2644_00))
	want(t, "Line 37 tax due", f.GetTaxDue(), money.USCents(644_00))
	want(t, "Line 42 amount due", f.GetTotalAmountDue(), money.USCents(644_00))
	want(t, "Line 43 refund", f.GetRefund(), money.USCents(0))
}

// A high-income single filer whose only federal itemized deduction is the $25,000
// of DC income tax withheld.
//   - Federally, $25,000 of state income tax (under the $40,000 cap) beats the
//     $15,750 standard deduction, so the federal return itemizes, and line 17
//     must take the same type (booklet, Line 17). DC does not allow state and
//     local income taxes (booklet, Line 18).
//   - Line 16: $301,000.
//   - Line 18, Calculation F (booklet p. 20): a $25,000 itemized, b $25,000 of
//     state and local taxes (Schedule A line 7), c $0, d and e $0, f $0. DC AGI
//     is over $200,000, so g $0, h $0, k $101,000, l $5,050, m $0, n $0.
//   - Line 19: $301,000.
//   - Line 20, Calculation I: $19,650 + 9.25% of the $51,000 over $250,000 =
//     $19,650 + $4,717.50 = $24,367.50, $24,368 rounded to the dollar.
//   - Line 31 withheld: $25,000. Line 43 refund: $25,000 - $24,368 = $632.
func TestDistrictOfColumbiaHighIncome(t *testing.T) {
	ret, f := dcPrepare(t,
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		ilPerson("lee", 1975, 10, 10, false, false), nil, nil,
		[]*formw2.FormW2{dcW2(301000_00, 60000_00, 25000_00)})

	if !f.GetItemizesDeductions() {
		t.Error("expected line 17 to follow the federal return and itemize")
	}
	want(t, "Line 18 deduction", f.GetDeduction(), money.USCents(0))
	want(t, "Line 19 taxable income", f.GetTaxableIncome(), money.USCents(301000_00))
	want(t, "Line 20 tax", f.GetTax(), money.USCents(24368_00))
	want(t, "Line 43 refund", f.GetRefund(), money.USCents(632_00))
	want(t, "Summary amount owed", ret.GetSummary().GetAmountOwed(), money.USCents(0))
}
