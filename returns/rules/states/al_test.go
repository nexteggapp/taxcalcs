package states_test

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
	form40 "github.com/nexteggapp/taxprotos/go/us/state/al/v1/form_40"
)

// There is no Go income tax calculator for Alabama, so these households are
// checked against figures worked by hand from the 2025 Form 40 booklet: the
// Standard Deduction chart (page 9), the dependent exemption chart (Part III
// instructions), the Federal Income Tax Deduction Worksheet (page 31) and the
// tax table, whose rows are $100 wide and are taxed at their midpoint, 2% on
// the first $500 ($1,000 joint), 4% on the next $2,500 ($5,000 joint) and 5%
// above, rounded to the dollar. Above $100,000 the rates apply to the exact
// figure.

func alW2(wages, fedWithheld, alWithheld int64) *formw2.FormW2 {
	return &formw2.FormW2{
		WagesTipsOtherCompensation: cents(wages),
		FederalTaxWithheld:         cents(fedWithheld),
		StateTaxWithholding: []*commonv1.StateTaxWithholding{
			{State: "AL", StateIncome: cents(wages), TaxWithheld: cents(alWithheld)},
		},
	}
}

func alInputs(
	status returnsv1.FilingStatusElection,
	taxpayer, spouse *returnsv1.Person,
	dependents []*returnsv1.Dependent,
	w2s []*formw2.FormW2,
) *returnsv1.ReturnInputs {
	in := ilInputs(status, taxpayer, spouse, dependents, w2s, 0)
	in.States = []*returnsv1.StateInputs{{
		State:  "AL",
		Inputs: &returnsv1.StateInputs_Al{Al: &returnsv1.AlabamaInputs{}},
	}}
	return in
}

func prepareAlabama(t *testing.T, in *returnsv1.ReturnInputs) *form40.FormAL40 {
	t.Helper()
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatal(err)
	}
	out, err := eng.Prepare(in)
	if err != nil {
		t.Fatal(err)
	}
	ret := out.GetStates()[0].GetAl()
	if ret == nil {
		t.Fatal("expected Alabama return")
	}
	if ret.GetStatus() != returnsv1.ReturnStatus_RETURN_STATUS_COMPLETE {
		t.Errorf("expected complete return, got %v: %v", ret.GetStatus(), ret.GetDiagnostics())
	}
	return ret.GetForms().GetForm_40()
}

// Single, $50,000 of wages, $2,000 Alabama tax withheld.
//   - Line 10 AGI: $50,000.
//   - Line 11 standard deduction: Single, AGI $35,500 and above: $2,500 (chart, page 9).
//   - Line 12 federal tax deduction: worksheet line 1 is Form 1040 line 22
//     as the federal return states it. Federal taxable income $50,000 -
//     $15,750 = $34,250, which the federal Tax Table taxes at its row's
//     midpoint, $34,275: $1,192.50 + 12% x $22,350 = $3,874.50, $3,875. No
//     refundable credits, so worksheet line 6 is $3,875.
//   - Line 13 personal exemption: Single, $1,500 (line 1).
//   - Line 15: $2,500 + $3,875 + $1,500 = $7,875. Line 16: $42,125.
//   - Line 17: row $42,100-$42,200 at $42,150: $10 + $100 + 5% x $39,150
//     = $2,067.50, $2,068.
//   - Line 22 withheld $2,000; line 30 owed $68.
func TestAlabamaSingle(t *testing.T) {
	p := ilPerson("sam", 1990, 4, 2, false, false)
	f := prepareAlabama(t, alInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		p, nil, nil, []*formw2.FormW2{alW2(50000_00, 5000_00, 2000_00)}))

	want(t, "Line 10 AGI", f.GetAdjustedGrossIncome(), money.USCents(50000_00))
	want(t, "Line 11 Standard deduction", f.GetDeduction(), money.USCents(2500_00))
	want(t, "Line 12 Federal tax deduction", f.GetFederalTaxDeduction(), money.USCents(3875_00))
	want(t, "Line 13 Personal exemption", f.GetPersonalExemption(), money.USCents(1500_00))
	want(t, "Line 16 Taxable income", f.GetTaxableIncome(), money.USCents(42125_00))
	want(t, "Line 17 Income tax", f.GetIncomeTax(), money.USCents(2068_00))
	want(t, "Line 27 Total payments", f.GetTotalPayments(), money.USCents(2000_00))
	want(t, "Line 30 Amount owed", f.GetAmountOwed(), money.USCents(68_00))
	want(t, "Line 35 Refund", f.GetRefund(), money.USCents(0))
}

// Married filing jointly, $60,000 and $20,000 of wages, two children aged 5
// and 8, $3,000 Alabama tax withheld.
//   - Line 10 AGI: $80,000.
//   - Line 11: Married filing joint, AGI $35,500 and above: $5,000.
//   - Line 12: federal taxable income $80,000 - $31,500 = $48,500, which the
//     Tax Table taxes at its row's midpoint, $48,525: $2,385 + 12% x
//     $24,675 = $5,346; less the child tax credit 2 x $2,200 = $4,400, Form
//     1040 line 22 is $946. No refundable credits: $946.
//   - Line 13: Married filing joint, $3,000 (line 2).
//   - Line 14: AGI $50,001-$100,000, $500 each, 2 x $500 = $1,000.
//   - Line 15: $5,000 + $946 + $3,000 + $1,000 = $9,946. Line 16: $70,054.
//   - Line 17: row $70,000-$70,100 at $70,050: $20 + $200 + 5% x $64,050
//     = $3,422.50, $3,423.
//   - Line 22 withheld $3,000; line 30 owed $423.
func TestAlabamaMarriedFilingJointlyWithChildren(t *testing.T) {
	p1 := ilPerson("ann", 1986, 1, 9, false, false)
	p2 := ilPerson("ben", 1985, 6, 30, false, false)
	kids := []*returnsv1.Dependent{
		ilDependent("cal", 2020, 3, 3),
		ilDependent("dee", 2017, 7, 7),
	}
	f := prepareAlabama(t, alInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY,
		p1, p2, kids, []*formw2.FormW2{alW2(60000_00, 3000_00, 2200_00), alW2(20000_00, 1000_00, 800_00)}))

	want(t, "Line 10 AGI", f.GetAdjustedGrossIncome(), money.USCents(80000_00))
	want(t, "Line 11 Standard deduction", f.GetDeduction(), money.USCents(5000_00))
	want(t, "Line 12 Federal tax deduction", f.GetFederalTaxDeduction(), money.USCents(946_00))
	want(t, "Line 13 Personal exemption", f.GetPersonalExemption(), money.USCents(3000_00))
	want(t, "Line 14 Dependent exemption", f.GetDependentExemption(), money.USCents(1000_00))
	want(t, "Line 16 Taxable income", f.GetTaxableIncome(), money.USCents(70054_00))
	want(t, "Line 17 Income tax", f.GetIncomeTax(), money.USCents(3423_00))
	want(t, "Line 27 Total payments", f.GetTotalPayments(), money.USCents(3000_00))
	want(t, "Line 30 Amount owed", f.GetAmountOwed(), money.USCents(423_00))
}

// Single, $150,000 of wages, $7,000 Alabama tax withheld.
//   - Line 11: $2,500. Line 13: $1,500.
//   - Line 12: federal taxable income $134,250, over the table, so the rates:
//     $1,192.50 + 12% x $36,550 + 22% x $54,875 + 24% x $30,900 = $25,067.
//   - Line 15: $29,067. Line 16: $120,933, over $100,000, so the rates on the
//     exact figure: $10 + $100 + 5% x $117,933 = $6,006.65, $6,007.
//   - Line 22 withheld $7,000; line 32 overpaid and line 35 refund $993.
func TestAlabamaHighIncomeSingle(t *testing.T) {
	p := ilPerson("max", 1970, 11, 11, false, false)
	f := prepareAlabama(t, alInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		p, nil, nil, []*formw2.FormW2{alW2(150000_00, 25000_00, 7000_00)}))

	want(t, "Line 12 Federal tax deduction", f.GetFederalTaxDeduction(), money.USCents(25067_00))
	want(t, "Line 16 Taxable income", f.GetTaxableIncome(), money.USCents(120933_00))
	want(t, "Line 17 Income tax", f.GetIncomeTax(), money.USCents(6007_00))
	want(t, "Line 27 Total payments", f.GetTotalPayments(), money.USCents(7000_00))
	want(t, "Line 35 Refund", f.GetRefund(), money.USCents(993_00))
	want(t, "Line 30 Amount owed", f.GetAmountOwed(), money.USCents(0))
}
