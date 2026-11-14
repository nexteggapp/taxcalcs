package states_test

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
	formit201 "github.com/nexteggapp/taxprotos/go/us/state/ny/v1/form_it201"
)

// There is no Go income tax calculator for New York (newyorktaxcalc holds
// only estimated tax), so each household below is worked by hand from the
// 2025 Form IT-201 and its instructions (IT-201-I). Page numbers are the
// instructions'.

func nyW2(wages, fedWithheld, nyWithheld int64) *formw2.FormW2 {
	return &formw2.FormW2{
		WagesTipsOtherCompensation: cents(wages),
		FederalTaxWithheld:         cents(fedWithheld),
		StateTaxWithholding: []*commonv1.StateTaxWithholding{
			{State: "NY", StateIncome: cents(wages), TaxWithheld: cents(nyWithheld)},
		},
	}
}

func nyInputs(
	status returnsv1.FilingStatusElection,
	taxpayer, spouse *returnsv1.Person,
	dependents []*returnsv1.Dependent,
	w2s []*formw2.FormW2,
	ny *returnsv1.NewYorkInputs,
) *returnsv1.ReturnInputs {
	in := ilInputs(status, taxpayer, spouse, dependents, w2s, 0)
	in.States = []*returnsv1.StateInputs{{
		State:  "NY",
		Inputs: &returnsv1.StateInputs_Ny{Ny: ny},
	}}
	return in
}

func prepareNY(t *testing.T, in *returnsv1.ReturnInputs) (*returnsv1.NewYorkReturn, *formit201.FormIT201) {
	t.Helper()
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatal(err)
	}
	out, err := eng.Prepare(in)
	if err != nil {
		t.Fatal(err)
	}
	ret := out.GetStates()[0].GetNy()
	if ret == nil {
		t.Fatal("expected New York return")
	}
	return ret, ret.GetForms().GetFormIt201()
}

// 1. Single New York City resident with wages.
//   - Line 19, 33: federal and New York AGI $90,000.
//   - Line 34: standard deduction, single, item C No: $8,000 (p. 12 table).
//   - Line 37/38: $82,000. New York AGI is $107,650 or less and line 38 is
//     $65,000 or more, so the NYS rate schedule applies (p. 12, line 39).
//   - Line 39: single schedule, over $80,650: $4,271 + 6% x $1,350 = $4,352
//     (p. 34).
//   - Line 47: $82,000 (p. 13: no Charitable Gifts Trust deduction).
//   - Line 47a: $65,000 or more, so the NYC rate schedule: single, over
//     $50,000: $1,813 + 3.876% x $32,000 = $3,053.32, $3,053 (p. 41).
//   - Line 61: $4,352 + $3,053 = $7,405.
//   - Lines 72-73: $4,500 NYS withheld + $3,000 NYC withheld = $7,500 (line 76).
//   - Line 77/78: refund $95.
func TestNewYorkSingleNYC(t *testing.T) {
	p := ilPerson("dana", 1990, 2, 3, false, false)
	ret, f := prepareNY(t, nyInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		p, nil, nil,
		[]*formw2.FormW2{nyW2(90000_00, 9000_00, 4500_00)},
		&returnsv1.NewYorkInputs{NewYorkCityResident: true, NycTaxWithheld: cents(3000_00)},
	))

	want(t, "Line 33 NY AGI", f.GetNewYorkAdjustedGrossIncome(), money.USCents(90000_00))
	want(t, "Line 34 Deduction", f.GetDeduction(), money.USCents(8000_00))
	want(t, "Line 37 Taxable income", f.GetTaxableIncome(), money.USCents(82000_00))
	want(t, "Line 39 NYS tax", f.GetNewYorkStateTax(), money.USCents(4352_00))
	want(t, "Line 47 NYC taxable income", f.GetNycTaxableIncome(), money.USCents(82000_00))
	want(t, "Line 47a NYC tax", f.GetNycResidentTax(), money.USCents(3053_00))
	want(t, "Line 61 Total taxes", f.GetTotalTaxes(), money.USCents(7405_00))
	want(t, "Line 76 Total payments", f.GetTotalPayments(), money.USCents(7500_00))
	want(t, "Line 78 Refund", f.GetRefund(), money.USCents(95_00))
	want(t, "Line 80 Owed", f.GetAmountOwed(), money.USCents(0))
	if ret.GetStatus() != returnsv1.ReturnStatus_RETURN_STATUS_COMPLETE {
		t.Errorf("expected complete return, got %v: %v", ret.GetStatus(), ret.GetDiagnostics())
	}
}

// 2. Married filing jointly, two children, living in Yonkers.
//   - Line 33: $100,000 + $30,000 = $130,000.
//   - Line 34: $16,050 (p. 12 table). Line 36: 2 x $1,000 = $2,000 (p. 12).
//   - Line 37/38: $130,000 - $16,050 - $2,000 = $111,950.
//   - New York AGI is over $107,650 and line 38 is $161,550 or less:
//     Tax computation worksheet 1 (p. 34).
//     3: $111,950 x 5.5% = $6,157.25.
//     4: schedule, over $27,900: $1,202 + 5.5% x $84,050 = $5,824.75.
//     5: $332.50. 6: $130,000 - $107,650 = $22,350. 7: 0.4470.
//     8: $332.50 x 0.4470 = $148.63. 9: $5,824.75 + $148.63 = $5,973.38.
//   - Line 39/46: $5,973.
//   - Line 55: Yonkers surcharge, an input: $150. Line 58: $150.
//   - Line 61: $6,123.
//   - Lines 72, 74: $3,800 + $2,000 NYS withheld, $100 Yonkers withheld: $5,900.
//   - Line 80: owed $223.
func TestNewYorkJointWithChildrenYonkers(t *testing.T) {
	p1 := ilPerson("erin", 1984, 4, 1, false, false)
	p2 := ilPerson("frank", 1983, 9, 9, false, false)
	kids := []*returnsv1.Dependent{
		ilDependent("gus", 2020, 6, 1),
		ilDependent("hana", 2017, 1, 15),
	}
	ret, f := prepareNY(t, nyInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY,
		p1, p2, kids,
		[]*formw2.FormW2{nyW2(100000_00, 8000_00, 3800_00), nyW2(30000_00, 2000_00, 2000_00)},
		&returnsv1.NewYorkInputs{
			YonkersResidentSurcharge: cents(150_00),
			YonkersTaxWithheld:       cents(100_00),
		},
	))

	want(t, "Line 33 NY AGI", f.GetNewYorkAdjustedGrossIncome(), money.USCents(130000_00))
	want(t, "Line 34 Deduction", f.GetDeduction(), money.USCents(16050_00))
	want(t, "Line 36 Dependent exemption", f.GetDependentExemption(), money.USCents(2000_00))
	want(t, "Line 37 Taxable income", f.GetTaxableIncome(), money.USCents(111950_00))
	want(t, "Line 39 NYS tax", f.GetNewYorkStateTax(), money.USCents(5973_00))
	want(t, "Line 47a NYC tax", f.GetNycResidentTax(), money.USCents(0))
	want(t, "Line 58 NYC and Yonkers", f.GetTotalNycAndYonkersTaxes(), money.USCents(150_00))
	want(t, "Line 61 Total taxes", f.GetTotalTaxes(), money.USCents(6123_00))
	want(t, "Line 76 Total payments", f.GetTotalPayments(), money.USCents(5900_00))
	want(t, "Line 78 Refund", f.GetRefund(), money.USCents(0))
	want(t, "Line 80 Owed", f.GetAmountOwed(), money.USCents(223_00))
	if ret.GetStatus() != returnsv1.ReturnStatus_RETURN_STATUS_COMPLETE {
		t.Errorf("expected complete return, got %v: %v", ret.GetStatus(), ret.GetDiagnostics())
	}
}

// 3. High-income single New York City resident.
//   - Line 33: $400,000. Line 34: $8,000. Line 37/38: $392,000.
//   - New York AGI over $215,400 and line 38 over $215,400 but not over
//     $1,077,550: Tax computation worksheet 8 (p. 35).
//     3: schedule, over $215,400: $12,356 + 6.85% x $176,600 = $24,453.10.
//     4: recapture base $568. 5: incremental benefit $1,831.
//     6: $184,600. 7: $50,000. 8: 1.0000. 9: $1,831.
//     10: $24,453.10 + $568 + $1,831 = $26,852.10, $26,852 (6.85% of $392,000).
//   - Line 47a: NYC schedule, over $50,000: $1,813 + 3.876% x $342,000 =
//     $15,068.92, $15,069 (p. 41).
//   - Line 61: $26,852 + $15,069 = $41,921.
//   - Lines 72-73: $27,000 + $15,000 = $42,000. Line 78: refund $79.
func TestNewYorkHighIncomeNYC(t *testing.T) {
	p := ilPerson("ivy", 1975, 7, 7, false, false)
	ret, f := prepareNY(t, nyInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		p, nil, nil,
		[]*formw2.FormW2{nyW2(400000_00, 90000_00, 27000_00)},
		&returnsv1.NewYorkInputs{NewYorkCityResident: true, NycTaxWithheld: cents(15000_00)},
	))

	want(t, "Line 37 Taxable income", f.GetTaxableIncome(), money.USCents(392000_00))
	want(t, "Line 39 NYS tax", f.GetNewYorkStateTax(), money.USCents(26852_00))
	want(t, "Line 47a NYC tax", f.GetNycResidentTax(), money.USCents(15069_00))
	want(t, "Line 61 Total taxes", f.GetTotalTaxes(), money.USCents(41921_00))
	want(t, "Line 76 Total payments", f.GetTotalPayments(), money.USCents(42000_00))
	want(t, "Line 78 Refund", f.GetRefund(), money.USCents(79_00))
	want(t, "Line 80 Owed", f.GetAmountOwed(), money.USCents(0))
	if ret.GetStatus() != returnsv1.ReturnStatus_RETURN_STATUS_COMPLETE {
		t.Errorf("expected complete return, got %v: %v", ret.GetStatus(), ret.GetDiagnostics())
	}
}
