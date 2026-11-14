package states_test

// Hawaii has no Go income tax calculator (hawaiitaxcalc holds only estimated
// tax), so each household below is worked by hand from the 2025 Form N-11 and
// its instructions, line by line. The tax-table figures were checked against
// the published Tax Table (instructions pages 36 to 47); the rate-schedule
// figures come from the 2025 Tax Rate Schedules (instructions page 48).

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
	formn11 "github.com/nexteggapp/taxprotos/go/us/state/hi/v1/form_n11"
)

func hiInputs(
	status returnsv1.FilingStatusElection,
	taxpayer, spouse *returnsv1.Person,
	dependents []*returnsv1.Dependent,
	w2s []*formw2.FormW2,
	hiIn *returnsv1.HawaiiInputs,
) *returnsv1.ReturnInputs {
	marital := returnsv1.MaritalStatus_MARITAL_STATUS_NEVER_MARRIED
	if spouse != nil {
		marital = returnsv1.MaritalStatus_MARITAL_STATUS_MARRIED
	}
	var docs []*returnsv1.W2Document
	for i, w := range w2s {
		person := taxpayer.Id
		if i == 1 && spouse != nil {
			person = spouse.Id
		}
		docs = append(docs, &returnsv1.W2Document{Id: w2Id(i), Person: person, Form: w})
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
			Documents: &returnsv1.Documents{W2S: docs},
		},
		States: []*returnsv1.StateInputs{
			{State: "HI", Inputs: &returnsv1.StateInputs_Hi{Hi: hiIn}},
		},
	}
}

func hiW2(wages, fedWithheld, hiWithheld int64) *formw2.FormW2 {
	return &formw2.FormW2{
		WagesTipsOtherCompensation: cents(wages),
		FederalTaxWithheld:         cents(fedWithheld),
		StateTaxWithholding: []*commonv1.StateTaxWithholding{
			{State: "HI", StateIncome: cents(wages), TaxWithheld: cents(hiWithheld)},
		},
	}
}

func prepareHawaii(t *testing.T, in *returnsv1.ReturnInputs) (*returnsv1.HawaiiReturn, *formn11.FormN11) {
	t.Helper()
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatal(err)
	}
	out, err := eng.Prepare(in)
	if err != nil {
		t.Fatal(err)
	}
	ret := out.GetStates()[0].GetHi()
	if ret == nil {
		t.Fatal("expected a Hawaii return")
	}
	return ret, ret.GetForms().GetFormN11()
}

// Single, $60,000 of wages, $2,800 of Hawaii tax withheld.
//   - Line 7, federal AGI: $60,000. No additions or subtractions: line 20 $60,000.
//   - Line 23, standard deduction, filing status 1: $4,400 (form line 23).
//   - Line 24: $60,000 - $4,400 = $55,600.
//   - Line 6e: 1 exemption. Line 25: 1 x $1,144 = $1,144 (form line 25).
//   - Line 26: $55,600 - $1,144 = $54,456.
//   - Line 27: under $100,000, so the Tax Table: row $54,450-$54,500, single
//     column, $3,031. (Schedule I at the midpoint $54,475:
//     $2,539 + 7.60% x $6,475 = $3,031.10.)
//   - Lines 33 and 35: none. Line 36: $3,031.
//   - Line 37 and 41: $2,800. Line 48, amount owed: $3,031 - $2,800 = $231.
func TestHawaiiSingle(t *testing.T) {
	p := ilPerson("kai", 1988, 5, 12, false, false)
	in := hiInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, p, nil, nil,
		[]*formw2.FormW2{hiW2(60000_00, 6000_00, 2800_00)}, &returnsv1.HawaiiInputs{})
	ret, f := prepareHawaii(t, in)

	want(t, "Line 7 federal AGI", f.GetFederalAdjustedGrossIncome(), money.USCents(60000_00))
	want(t, "Line 20 Hawaii AGI", f.GetHawaiiAdjustedGrossIncome(), money.USCents(60000_00))
	want(t, "Line 23 standard deduction", f.GetStandardDeduction(), money.USCents(4400_00))
	want(t, "Line 25 exemptions", f.GetExemptions(), money.USCents(1144_00))
	want(t, "Line 26 taxable income", f.GetTaxableIncome(), money.USCents(54456_00))
	want(t, "Line 27 tax", f.GetTax(), money.USCents(3031_00))
	want(t, "Line 36 balance", f.GetBalance(), money.USCents(3031_00))
	want(t, "Line 41 total payments", f.GetTotalPayments(), money.USCents(2800_00))
	want(t, "Line 47a refund", f.GetRefund(), money.USCents(0))
	want(t, "Line 48 amount owed", f.GetAmountOwed(), money.USCents(231_00))
	if f.GetExemptionsClaimed() != 1 {
		t.Errorf("line 6e: got %d exemptions, want 1", f.GetExemptionsClaimed())
	}
	if ret.GetStatus() != returnsv1.ReturnStatus_RETURN_STATUS_COMPLETE {
		t.Errorf("expected a complete return, got %v: %v", ret.GetStatus(), ret.GetDiagnostics())
	}
}

// Married filing jointly, two earners ($95,000 and $45,000), two children;
// $6,000 and $2,000 of Hawaii tax withheld.
//   - Line 7 and 20: $140,000.
//   - Line 23, filing status 2: $8,800. Line 24: $131,200.
//   - Line 6e: 2 filers + 2 dependents = 4. Line 25: 4 x $1,144 = $4,576.
//   - Line 26: $131,200 - $4,576 = $126,624.
//   - Line 27: $100,000 or over, so Schedule II: over $96,000,
//     $5,078 + 7.60% x $30,624 = $5,078 + $2,327.42 = $7,405.42, $7,405.
//   - Line 36: $7,405. Line 41: $8,000.
//   - Line 42 and 47a: $8,000 - $7,405 = $595.
func TestHawaiiMarriedFilingJointlyWithChildren(t *testing.T) {
	a := ilPerson("ana", 1985, 3, 1, false, false)
	b := ilPerson("ben", 1984, 7, 9, false, false)
	kids := []*returnsv1.Dependent{ilDependent("c1", 2015, 4, 2), ilDependent("c2", 2018, 9, 30)}
	in := hiInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY, a, b, kids,
		[]*formw2.FormW2{hiW2(95000_00, 9000_00, 6000_00), hiW2(45000_00, 3000_00, 2000_00)},
		&returnsv1.HawaiiInputs{})
	ret, f := prepareHawaii(t, in)

	want(t, "Line 20 Hawaii AGI", f.GetHawaiiAdjustedGrossIncome(), money.USCents(140000_00))
	want(t, "Line 23 standard deduction", f.GetStandardDeduction(), money.USCents(8800_00))
	want(t, "Line 25 exemptions", f.GetExemptions(), money.USCents(4576_00))
	want(t, "Line 26 taxable income", f.GetTaxableIncome(), money.USCents(126624_00))
	want(t, "Line 27 tax", f.GetTax(), money.USCents(7405_00))
	want(t, "Line 41 total payments", f.GetTotalPayments(), money.USCents(8000_00))
	want(t, "Line 42 overpaid", f.GetOverpayment(), money.USCents(595_00))
	want(t, "Line 47a refund", f.GetRefund(), money.USCents(595_00))
	want(t, "Line 48 amount owed", f.GetAmountOwed(), money.USCents(0))
	want(t, "Summary refund", ret.GetSummary().GetRefund(), money.USCents(595_00))
}

// Single, $400,000 of wages, $30,000 of Hawaii tax withheld.
//   - Line 20: $400,000. Line 23: $4,400. Line 24: $395,600.
//   - Line 25: 1 x $1,144. Line 26: $394,456.
//   - Line 27: Schedule I, over $325,000: $25,966 + 11.00% x $69,456
//     = $25,966 + $7,640.16 = $33,606.16, $33,606.
//   - Line 41: $30,000. Line 48: $33,606 - $30,000 = $3,606.
func TestHawaiiHighIncome(t *testing.T) {
	p := ilPerson("lei", 1970, 1, 15, false, false)
	in := hiInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, p, nil, nil,
		[]*formw2.FormW2{hiW2(400000_00, 90000_00, 30000_00)}, &returnsv1.HawaiiInputs{})
	ret, f := prepareHawaii(t, in)

	want(t, "Line 26 taxable income", f.GetTaxableIncome(), money.USCents(394456_00))
	want(t, "Line 27 tax", f.GetTax(), money.USCents(33606_00))
	want(t, "Line 48 amount owed", f.GetAmountOwed(), money.USCents(3606_00))
	want(t, "Summary amount owed", ret.GetSummary().GetAmountOwed(), money.USCents(3606_00))
}

// Head of household with one child, $25,000 of wages, $500 withheld, and
// the refundable food/excise and renters' credits entered as Form N-311 and
// Schedule X figure them ($220 and $100).
//   - Line 20: $25,000. Line 23, filing status 4: $6,424. Line 24: $18,576.
//   - Line 6e: 2. Line 25: $2,288. Line 26: $16,288.
//   - Line 27: Tax Table row $16,250-$16,300, head of household column, $262.
//   - Line 33: $220 + $100 = $320. Line 34: $262 - $320 = -$58.
//   - Line 36: -$58. Line 41: $500. Line 42: $500 - (-$58) = $558.
func TestHawaiiHeadOfHouseholdRefundableCredits(t *testing.T) {
	p := ilPerson("mele", 1990, 6, 6, false, false)
	kid := []*returnsv1.Dependent{ilDependent("c1", 2016, 2, 2)}
	in := hiInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_HEAD_OF_HOUSEHOLD, p, nil, kid,
		[]*formw2.FormW2{hiW2(25000_00, 0, 500_00)},
		&returnsv1.HawaiiInputs{FoodExciseTaxCredit: cents(220_00), LowIncomeRentersCredit: cents(100_00)})
	_, f := prepareHawaii(t, in)

	want(t, "Line 23 standard deduction", f.GetStandardDeduction(), money.USCents(6424_00))
	want(t, "Line 26 taxable income", f.GetTaxableIncome(), money.USCents(16288_00))
	want(t, "Line 27 tax", f.GetTax(), money.USCents(262_00))
	want(t, "Line 33 refundable credits", f.GetTotalRefundableCredits(), money.USCents(320_00))
	want(t, "Line 36 balance", f.GetBalance(), money.USCents(-58_00))
	want(t, "Line 47a refund", f.GetRefund(), money.USCents(558_00))
	want(t, "Line 48 amount owed", f.GetAmountOwed(), money.USCents(0))
}
