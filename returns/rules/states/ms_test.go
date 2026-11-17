package states_test

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
	form80105 "github.com/nexteggapp/taxprotos/go/us/state/ms/v1/form_80105"
)

// There is no Go income tax calculator for Mississippi, so these households are
// checked against figures worked by hand from the 2025 Form 80-105 and its
// instructions (Form 80-100, Rev. 12/25). Page numbers are the instructions'
// printed page numbers.

func msInputs(
	status returnsv1.FilingStatusElection,
	taxpayer, spouse *returnsv1.Person,
	dependents []*returnsv1.Dependent,
	w2s []*formw2.FormW2,
) *returnsv1.ReturnInputs {
	marital := returnsv1.MaritalStatus_MARITAL_STATUS_NEVER_MARRIED
	if spouse != nil {
		marital = returnsv1.MaritalStatus_MARITAL_STATUS_MARRIED
	}
	var docs []*returnsv1.W2Document
	for i, w := range w2s {
		docs = append(docs, &returnsv1.W2Document{Id: w2Id(i), Person: taxpayer.Id, Form: w})
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
		States: []*returnsv1.StateInputs{{
			State:  "MS",
			Inputs: &returnsv1.StateInputs_Ms{Ms: &returnsv1.MississippiInputs{}},
		}},
	}
}

func msW2(wages, fedWithheld, msWithheld int64) *formw2.FormW2 {
	return &formw2.FormW2{
		WagesTipsOtherCompensation: cents(wages),
		FederalTaxWithheld:         cents(fedWithheld),
		StateTaxWithholding: []*commonv1.StateTaxWithholding{
			{State: "MS", StateIncome: cents(wages), TaxWithheld: cents(msWithheld)},
		},
	}
}

func prepareMississippi(t *testing.T, in *returnsv1.ReturnInputs) (*returnsv1.MississippiReturn, *form80105.Form80105) {
	t.Helper()
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatal(err)
	}
	out, err := eng.Prepare(in)
	if err != nil {
		t.Fatal(err)
	}
	ret := out.GetStates()[0].GetMs()
	if ret == nil {
		t.Fatalf("expected a Mississippi return, got %v", out.GetStates()[0])
	}
	return ret, ret.GetForms().GetForm_80105()
}

// Single filer, $50,000 of wages, $1,500 Mississippi tax withheld.
//   - Line 38A, 49A, 66A, 13A: wages $50,000.
//   - Lines 9 and 10: no dependents, under 65, not blind: $0 (p. 6, line 10).
//   - Line 11: single filing status exemption $6,000 (p. 5, filing status table).
//   - Line 12 and 15A: $6,000.
//   - Line 14A: single standard deduction $2,300 (p. 5, table).
//   - Line 16A: $50,000 - $2,300 - $6,000 = $41,700.
//   - Line 17: Schedule of Tax Computation (p. 27): first $10,000 at 0%,
//     remaining $31,700 at 4.4% = $1,394.80, rounded to $1,395.
//   - Line 23: $1,395. Line 24 and 28: $1,500.
//   - Line 29, 31, 34: refund $1,500 - $1,395 = $105.
func TestMississippiSingle(t *testing.T) {
	in := msInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		ilPerson("sam", 1990, 4, 2, false, false), nil, nil,
		[]*formw2.FormW2{msW2(50000_00, 5000_00, 1500_00)})
	ret, f := prepareMississippi(t, in)

	want(t, "Line 66A MS AGI", f.GetTaxpayerAdjustedGrossIncome(), money.USCents(50000_00))
	want(t, "Line 12 Total exemption", f.GetTotalExemption(), money.USCents(6000_00))
	want(t, "Line 14A Deductions", f.GetTaxpayerDeductions(), money.USCents(2300_00))
	want(t, "Line 16A Taxable income", f.GetTaxpayerTaxableIncome(), money.USCents(41700_00))
	want(t, "Line 17 Income tax", f.GetIncomeTax(), money.USCents(1395_00))
	want(t, "Line 23 Total tax", f.GetTotalTax(), money.USCents(1395_00))
	want(t, "Line 28 Total payments", f.GetTotalPayments(), money.USCents(1500_00))
	want(t, "Line 34 Refund", f.GetRefund(), money.USCents(105_00))
	want(t, "Line 37 Amount owed", f.GetAmountOwed(), money.USCents(0))
	if ret.GetStatus() != returnsv1.ReturnStatus_RETURN_STATUS_COMPLETE {
		t.Errorf("expected complete return, got %v: %v", ret.GetStatus(), ret.GetDiagnostics())
	}
}

// Married filing jointly, one earner with $90,000 of wages, two children,
// $2,800 Mississippi tax withheld. Prepared as a joint return, all in column A.
//   - Line 66A, 13A: $90,000.
//   - Line 7: 2 dependents; line 8: none; line 9: 2.
//   - Line 10: 2 x $1,500 = $3,000 (p. 6, additional exemptions).
//   - Line 11: married joint exemption $12,000 (p. 5, table).
//   - Line 12 and 15A: $15,000.
//   - Line 14A: joint standard deduction $4,600 (p. 5, table).
//   - Line 16A: $90,000 - $4,600 - $15,000 = $70,400.
//   - Line 17: ($70,400 - $10,000) x 4.4% = $2,657.60, rounded to $2,658 (p. 27).
//   - Line 28: $2,800. Line 34: refund $2,800 - $2,658 = $142.
func TestMississippiMarriedFilingJointlyWithChildren(t *testing.T) {
	in := msInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY,
		ilPerson("ann", 1985, 3, 15, false, false), ilPerson("ben", 1984, 8, 20, false, false),
		[]*returnsv1.Dependent{ilDependent("cal", 2015, 6, 1), ilDependent("dee", 2018, 9, 9)},
		[]*formw2.FormW2{msW2(90000_00, 7000_00, 2800_00)})
	_, f := prepareMississippi(t, in)

	if f.GetAdditionalExemptions() != 2 {
		t.Errorf("Line 9: got %d, want 2", f.GetAdditionalExemptions())
	}
	want(t, "Line 10 Additional exemptions", f.GetAdditionalExemptionAmount(), money.USCents(3000_00))
	want(t, "Line 11 Filing status exemption", f.GetFilingStatusExemption(), money.USCents(12000_00))
	want(t, "Line 15A Exemptions", f.GetTaxpayerExemptions(), money.USCents(15000_00))
	want(t, "Line 14A Deductions", f.GetTaxpayerDeductions(), money.USCents(4600_00))
	want(t, "Line 16A Taxable income", f.GetTaxpayerTaxableIncome(), money.USCents(70400_00))
	want(t, "Line 17 Income tax", f.GetIncomeTax(), money.USCents(2658_00))
	want(t, "Line 34 Refund", f.GetRefund(), money.USCents(142_00))
	want(t, "Line 37 Amount owed", f.GetAmountOwed(), money.USCents(0))
}

// Single filer aged 70 with $250,000 of wages and $10,000 withheld.
//   - Line 8: one box (65 or over); line 9: 1; line 10: $1,500 (p. 6).
//   - Line 12 and 15A: $6,000 + $1,500 = $7,500.
//   - Line 14A: $2,300.
//   - Line 16A: $250,000 - $2,300 - $7,500 = $240,200.
//   - Line 17: ($240,200 - $10,000) x 4.4% = $10,128.80, rounded to $10,129.
//   - Line 35, 37: owed $10,129 - $10,000 = $129.
func TestMississippiHighIncomeSenior(t *testing.T) {
	in := msInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		ilPerson("vic", 1955, 1, 10, false, false), nil, nil,
		[]*formw2.FormW2{msW2(250000_00, 50000_00, 10000_00)})
	_, f := prepareMississippi(t, in)

	want(t, "Line 10 Additional exemptions", f.GetAdditionalExemptionAmount(), money.USCents(1500_00))
	want(t, "Line 15A Exemptions", f.GetTaxpayerExemptions(), money.USCents(7500_00))
	want(t, "Line 16A Taxable income", f.GetTaxpayerTaxableIncome(), money.USCents(240200_00))
	want(t, "Line 17 Income tax", f.GetIncomeTax(), money.USCents(10129_00))
	want(t, "Line 34 Refund", f.GetRefund(), money.USCents(0))
	want(t, "Line 37 Amount owed", f.GetAmountOwed(), money.USCents(129_00))
}
