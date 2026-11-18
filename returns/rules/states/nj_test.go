package states_test

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	form1099r "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_r"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
	formnj1040 "github.com/nexteggapp/taxprotos/go/us/state/nj/v1/form_nj1040"
)

// There is no Go income tax calculator for New Jersey, so these households are
// checked against figures worked out by hand from the 2025 Form NJ-1040, its
// instructions, and the printed 2025 New Jersey Tax Table. Page numbers are
// the instructions' printed pages.

// njInputs builds return inputs for a New Jersey household.
func njInputs(
	status returnsv1.FilingStatusElection,
	taxpayer, spouse *returnsv1.Person,
	dependents []*returnsv1.Dependent,
	docs *returnsv1.Documents,
	nj *returnsv1.NewJerseyInputs,
) *returnsv1.ReturnInputs {
	marital := returnsv1.MaritalStatus_MARITAL_STATUS_NEVER_MARRIED
	if spouse != nil {
		marital = returnsv1.MaritalStatus_MARITAL_STATUS_MARRIED
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
			Documents: docs,
		},
		States: []*returnsv1.StateInputs{
			{State: "NJ", Inputs: &returnsv1.StateInputs_Nj{Nj: nj}},
		},
	}
}

func njW2(id, person string, wages, fedWithheld, njWithheld int64) *returnsv1.W2Document {
	return &returnsv1.W2Document{
		Id:     id,
		Person: person,
		Form: &formw2.FormW2{
			WagesTipsOtherCompensation: cents(wages),
			FederalTaxWithheld:         cents(fedWithheld),
			StateTaxWithholding: []*commonv1.StateTaxWithholding{
				{State: "NJ", StateIncome: cents(wages), TaxWithheld: cents(njWithheld)},
			},
		},
	}
}

func njPrepare(t *testing.T, in *returnsv1.ReturnInputs) (*returnsv1.NewJerseyReturn, *formnj1040.FormNJ1040) {
	t.Helper()
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatal(err)
	}
	out, err := eng.Prepare(in)
	if err != nil {
		t.Fatal(err)
	}
	ret := out.GetStates()[0].GetNj()
	if ret == nil {
		t.Fatal("expected a New Jersey return")
	}
	return ret, ret.GetForms().GetFormNj1040()
}

// A single tenant with wages, taking the property tax deduction.
//
//   - Line 6: one regular exemption, x $1,000 (p. 8). Line 13: $1,000.
//   - Line 15: wages $60,000. Line 27: $60,000. Line 29: $60,000.
//   - Line 38: $1,000. Line 39: $60,000 - $1,000 = $59,000.
//   - Line 40a: rent $15,000 x 18% = $2,700 (p. 25).
//   - Line 41: Worksheet H deduction, the property tax up to $15,000: $2,700.
//   - Line 42: $59,000 - $2,700 = $56,300.
//   - Line 43: Tax Table, row $56,300-$56,350, status 1: $1,619 (p. 54).
//     The same from Table A on the row's middle: $56,325 x .05525 -
//     $1,492.50 = $1,619.46, rounded to $1,619 (p. 63).
//   - Line 54: $1,619. Line 55: withheld $2,000. Line 66: $2,000.
//   - Line 68: $2,000 - $1,619 = $381. Line 80: refund $381.
func TestNewJerseySingleTenant(t *testing.T) {
	p := ilPerson("sam", 1990, 4, 2, false, false)
	in := njInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		p, nil, nil,
		&returnsv1.Documents{W2S: []*returnsv1.W2Document{njW2("w2_0", "sam", 60000_00, 6000_00, 2000_00)}},
		&returnsv1.NewJerseyInputs{
			PropertyTaxesPaid:    cents(2700_00),
			PropertyTaxDeduction: cents(2700_00),
		},
	)
	ret, f := njPrepare(t, in)

	want(t, "Line 13 Exemptions", f.GetTotalExemptionAmount(), money.USCents(1000_00))
	want(t, "Line 15 Wages", f.GetWages(), money.USCents(60000_00))
	want(t, "Line 29 Gross income", f.GetGrossIncome(), money.USCents(60000_00))
	want(t, "Line 39 Taxable income", f.GetTaxableIncomeBeforePropertyTax(), money.USCents(59000_00))
	want(t, "Line 41 Property tax deduction", f.GetPropertyTaxDeduction(), money.USCents(2700_00))
	want(t, "Line 42 NJ taxable income", f.GetTaxableIncome(), money.USCents(56300_00))
	want(t, "Line 43 Tax", f.GetTax(), money.USCents(1619_00))
	want(t, "Line 54 Total tax due", f.GetTotalTaxDue(), money.USCents(1619_00))
	want(t, "Line 66 Payments", f.GetTotalPaymentsAndCredits(), money.USCents(2000_00))
	want(t, "Line 80 Refund", f.GetRefund(), money.USCents(381_00))
	want(t, "Line 79 Balance due", f.GetBalanceDue(), money.USCents(0))
	if ret.GetStatus() != returnsv1.ReturnStatus_RETURN_STATUS_COMPLETE {
		t.Errorf("expected a complete return, got %v: %v", ret.GetStatus(), ret.GetDiagnostics())
	}
}

// A married couple filing jointly with two children, one aged 5 or younger,
// homeowners taking the property tax deduction and the child tax credit.
//
//   - Line 6: two regular exemptions, $2,000. Line 10: two qualified dependent
//     children x $1,500 = $3,000 (p. 9). Line 13: $5,000.
//   - Line 15: wages $50,000 + $25,000 = $75,000. Lines 27 and 29: $75,000.
//   - Line 39: $75,000 - $5,000 = $70,000.
//   - Line 41: property taxes $9,000, under the $15,000 limit: $9,000.
//   - Line 42: $70,000 - $9,000 = $61,000.
//   - Line 43: Tax Table, row $61,000-$61,050, status 2: $1,075 (p. 54).
//     From Table B on the middle: $61,025 x .0245 - $420 = $1,075.11 (p. 63).
//   - Line 55: withheld $1,200 + $400 = $1,600.
//   - Line 65: line 42 over $60,000, not over $80,000: $200 for the one child
//     aged 5 or younger (p. 46).
//   - Line 66: $1,600 + $200 = $1,800. Line 68: $1,800 - $1,075 = $725.
//   - Line 80: refund $725.
func TestNewJerseyMarriedFilingJointlyWithChildren(t *testing.T) {
	p1 := ilPerson("ana", 1985, 3, 15, false, false)
	p2 := ilPerson("ben", 1986, 8, 20, false, false)
	in := njInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY,
		p1, p2,
		[]*returnsv1.Dependent{ilDependent("cal", 2015, 6, 1), ilDependent("dee", 2021, 2, 10)},
		&returnsv1.Documents{W2S: []*returnsv1.W2Document{
			njW2("w2_0", "ana", 50000_00, 3000_00, 1200_00),
			njW2("w2_1", "ben", 25000_00, 1000_00, 400_00),
		}},
		&returnsv1.NewJerseyInputs{
			PropertyTaxesPaid:    cents(9000_00),
			PropertyTaxDeduction: cents(9000_00),
			ChildTaxCredit:       cents(200_00),
		},
	)
	ret, f := njPrepare(t, in)

	want(t, "Line 13 Exemptions", f.GetTotalExemptionAmount(), money.USCents(5000_00))
	want(t, "Line 15 Wages", f.GetWages(), money.USCents(75000_00))
	want(t, "Line 39 Taxable income", f.GetTaxableIncomeBeforePropertyTax(), money.USCents(70000_00))
	want(t, "Line 42 NJ taxable income", f.GetTaxableIncome(), money.USCents(61000_00))
	want(t, "Line 43 Tax", f.GetTax(), money.USCents(1075_00))
	want(t, "Line 55 Withheld", f.GetTaxWithheld(), money.USCents(1600_00))
	want(t, "Line 65 Child tax credit", f.GetChildTaxCredit(), money.USCents(200_00))
	want(t, "Line 66 Payments", f.GetTotalPaymentsAndCredits(), money.USCents(1800_00))
	want(t, "Line 80 Refund", f.GetRefund(), money.USCents(725_00))
	want(t, "Line 79 Balance due", f.GetBalanceDue(), money.USCents(0))
	if ret.GetStatus() != returnsv1.ReturnStatus_RETURN_STATUS_COMPLETE {
		t.Errorf("expected a complete return, got %v: %v", ret.GetStatus(), ret.GetDiagnostics())
	}
}

// A retired couple, both 70, living on a $140,000 pension.
//
//   - Lines 6 and 7: two regular and two senior exemptions, $4,000 (p. 8).
//   - Line 20a: taxable pension $140,000. Line 27: $140,000.
//   - Line 28a: line 27 is $125,001-$150,000 for a joint return, so line B is
//     25% of $100,000 = $25,000, the lesser of it and line A (p. 22): $25,000.
//   - Line 29: $140,000 - $25,000 = $115,000.
//   - Line 39: $115,000 - $4,000 = $111,000.
//   - Line 41: property taxes $12,000: $12,000. Line 42: $99,000.
//   - Line 43: Tax Table, row $99,000-$99,050, status 2: $2,696 (p. 54).
//     From Table B on the middle: $99,025 x .05525 - $2,775 = $2,696.13.
//   - Line 55: withheld on Form 1099-R, $6,000.
//   - Line 68: $6,000 - $2,696 = $3,304. Line 80: refund $3,304.
func TestNewJerseyRetiredCouple(t *testing.T) {
	p1 := ilPerson("eve", 1955, 1, 5, false, false)
	p2 := ilPerson("gus", 1955, 7, 9, false, false)
	in := njInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY,
		p1, p2, nil,
		&returnsv1.Documents{RetirementDistributions: []*returnsv1.Form1099RDocument{{
			Id:     "1099r_0",
			Person: "eve",
			Form: &form1099r.Form1099R{
				GrossDistribution: cents(140000_00),
				TaxableAmount:     cents(140000_00),
				DistributionCodes: []string{"7"},
				StateTaxWithholding: []*commonv1.StateTaxWithholding{
					{State: "NJ", StateIncome: cents(140000_00), TaxWithheld: cents(6000_00)},
				},
			},
		}}},
		&returnsv1.NewJerseyInputs{
			PensionExclusion:     cents(25000_00),
			PropertyTaxesPaid:    cents(12000_00),
			PropertyTaxDeduction: cents(12000_00),
		},
	)
	ret, f := njPrepare(t, in)

	want(t, "Line 13 Exemptions", f.GetTotalExemptionAmount(), money.USCents(4000_00))
	want(t, "Line 20a Taxable pensions", f.GetTaxablePensions(), money.USCents(140000_00))
	want(t, "Line 28a Pension exclusion", f.GetPensionExclusion(), money.USCents(25000_00))
	want(t, "Line 29 Gross income", f.GetGrossIncome(), money.USCents(115000_00))
	want(t, "Line 42 NJ taxable income", f.GetTaxableIncome(), money.USCents(99000_00))
	want(t, "Line 43 Tax", f.GetTax(), money.USCents(2696_00))
	want(t, "Line 55 Withheld", f.GetTaxWithheld(), money.USCents(6000_00))
	want(t, "Line 80 Refund", f.GetRefund(), money.USCents(3304_00))
	if ret.GetStatus() != returnsv1.ReturnStatus_RETURN_STATUS_COMPLETE {
		t.Errorf("expected a complete return, got %v: %v", ret.GetStatus(), ret.GetDiagnostics())
	}
}

// Above the Tax Table the Tax Rate Schedules apply, to the cent.
//
//   - Single, wages $250,000. Line 13: $1,000. Line 42: $249,000.
//   - Line 43: Table A, $249,000 x .0637 - $2,126.25 = $13,735.05 (p. 63).
func TestNewJerseyRateSchedule(t *testing.T) {
	p := ilPerson("hal", 1980, 1, 1, false, false)
	in := njInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		p, nil, nil,
		&returnsv1.Documents{W2S: []*returnsv1.W2Document{njW2("w2_0", "hal", 250000_00, 50000_00, 14000_00)}},
		&returnsv1.NewJerseyInputs{},
	)
	_, f := njPrepare(t, in)

	want(t, "Line 42 NJ taxable income", f.GetTaxableIncome(), money.USCents(249000_00))
	want(t, "Line 43 Tax", f.GetTax(), money.USCents(13735_05))
	want(t, "Line 80 Refund", f.GetRefund(), money.USCents(264_95))
}
