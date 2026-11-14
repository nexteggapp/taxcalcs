package states_test

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
	form1 "github.com/nexteggapp/taxprotos/go/us/state/ma/v1/form_1"
)

// massachusettstaxcalc has no income tax calculator, so these households are
// worked by hand from the 2025 Form 1 and its instructions. Each line's
// working is in the comment above its test.

// A household asking for a Massachusetts return. The first W-2 is the
// taxpayer's, the second the spouse's.
func maInputs(
	status returnsv1.FilingStatusElection,
	taxpayer, spouse *returnsv1.Person,
	dependents []*returnsv1.Dependent,
	w2s []*formw2.FormW2,
	maIn *returnsv1.MassachusettsInputs,
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
			{State: "MA", Inputs: &returnsv1.StateInputs_Ma{Ma: maIn}},
		},
	}
}

// A W-2 with Social Security and Medicare tax at 6.2% and 1.45% of wages, the
// Social Security wage base ignored, and Massachusetts tax withheld.
func maW2(wages, maWithheld int64) *formw2.FormW2 {
	return &formw2.FormW2{
		WagesTipsOtherCompensation: cents(wages),
		FederalTaxWithheld:         cents(wages / 10),
		SocialSecurityWages:        cents(wages),
		SocialSecurityTaxWithheld:  cents(wages * 62 / 1000),
		MedicareWagesAndTips:       cents(wages),
		MedicareTaxWithheld:        cents(wages * 145 / 10000),
		StateTaxWithholding: []*commonv1.StateTaxWithholding{
			{State: "MA", StateIncome: cents(wages), TaxWithheld: cents(maWithheld)},
		},
	}
}

func prepareMA(t *testing.T, in *returnsv1.ReturnInputs) (*returnsv1.MassachusettsReturn, *form1.FormMA1) {
	t.Helper()
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatal(err)
	}
	out, err := eng.Prepare(in)
	if err != nil {
		t.Fatal(err)
	}
	ret := out.GetStates()[0].GetMa()
	if ret == nil {
		t.Fatal("expected a Massachusetts return")
	}
	return ret, ret.GetForms().GetForm_1()
}

// Single, $60,000 of wages, $18,000 of rent.
//   - Line 3, 10: $60,000 wages; total 5.0% income $60,000.
//   - Line 11a: Social Security $3,720 + Medicare $870, not more than $2,000
//     (instructions, line 11) = $2,000.
//   - Line 14: $18,000 / 2 = $9,000, not more than $4,000 (instructions,
//     line 14) = $4,000.
//   - Line 16: $6,000. Line 17: $54,000.
//   - Line 2a, 18: single, $4,400 (form, line 2a).
//   - Line 19, 21: $49,600. Over $24,000, so line 22 = $49,600 x .05 = $2,480.
//   - Line 28b: $49,600 is under $1,083,150, no surtax. Line 28, 32, 37: $2,480.
//   - Line 38a, 51: $2,600 withheld. Line 52, 54: refund $120.
func TestMassachusettsSingleRenter(t *testing.T) {
	ret, f := prepareMA(t, maInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		ilPerson("ana", 1990, 3, 4, false, false), nil, nil,
		[]*formw2.FormW2{maW2(60000_00, 2600_00)},
		&returnsv1.MassachusettsInputs{RentPaid: cents(18000_00)},
	))
	if f.GetFilingStatus() != form1.FilingStatus_FILING_STATUS_SINGLE {
		t.Errorf("filing status = %v", f.GetFilingStatus())
	}
	want(t, "line 10", f.GetTotalIncome(), money.USCents(60000_00))
	want(t, "line 11a", f.GetTaxpayerRetirementContributions(), money.USCents(2000_00))
	want(t, "line 14", f.GetRentalDeduction(), money.USCents(4000_00))
	want(t, "line 17", f.GetIncomeAfterDeductions(), money.USCents(54000_00))
	want(t, "line 2g", f.GetTotalExemptions(), money.USCents(4400_00))
	want(t, "line 21", f.GetTaxableIncome(), money.USCents(49600_00))
	want(t, "line 22", f.GetTaxOnTaxableIncome(), money.USCents(2480_00))
	want(t, "line 28b", f.GetSurtax(), money.USCents(0))
	want(t, "line 28", f.GetTotalTax(), money.USCents(2480_00))
	want(t, "line 51", f.GetTotalPayments(), money.USCents(2600_00))
	want(t, "line 54", f.GetRefund(), money.USCents(120_00))
	want(t, "line 55", f.GetAmountOwed(), money.USCents(0))
	want(t, "summary tax", ret.GetSummary().GetTotalTax(), money.USCents(2480_00))
}

// Married filing jointly, two children, $90,000 and $50,000 of wages.
//   - Line 3, 10: $140,000.
//   - Line 11a, 11b: each spouse paid more than $2,000 of Social Security and
//     Medicare; $2,000 each. Line 16: $4,000. Line 17: $136,000.
//   - Line 2a: married filing jointly, $8,800. Line 2b: 2 x $1,000 = $2,000.
//     Line 2g, 18: $10,800.
//   - Line 19, 21: $125,200. Line 22: x .05 = $6,260. Line 28, 37: $6,260.
//   - Line 38a: $4,000 + $2,200 = $6,200 withheld.
//   - Line 46: Child and Family Tax Credit entered as 2 x $440 = $880
//     (instructions, line 46). The federal EIC is nothing at this income.
//   - Line 51: $6,200 + $880 = $7,080. Line 54: refund $820.
func TestMassachusettsJointWithChildren(t *testing.T) {
	_, f := prepareMA(t, maInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY,
		ilPerson("ana", 1985, 1, 1, false, false), ilPerson("ben", 1984, 2, 2, false, false),
		[]*returnsv1.Dependent{ilDependent("cy", 2015, 5, 5), ilDependent("di", 2018, 6, 6)},
		[]*formw2.FormW2{maW2(90000_00, 4000_00), maW2(50000_00, 2200_00)},
		&returnsv1.MassachusettsInputs{ChildAndFamilyTaxCredit: cents(880_00)},
	))
	if f.GetFilingStatus() != form1.FilingStatus_FILING_STATUS_MARRIED_FILING_JOINTLY {
		t.Errorf("filing status = %v", f.GetFilingStatus())
	}
	if f.GetDependents() != 2 {
		t.Errorf("dependents = %d, want 2", f.GetDependents())
	}
	want(t, "line 10", f.GetTotalIncome(), money.USCents(140000_00))
	want(t, "line 11b", f.GetSpouseRetirementContributions(), money.USCents(2000_00))
	want(t, "line 16", f.GetTotalDeductions(), money.USCents(4000_00))
	want(t, "line 2b", f.GetDependentExemption(), money.USCents(2000_00))
	want(t, "line 2g", f.GetTotalExemptions(), money.USCents(10800_00))
	want(t, "line 21", f.GetTaxableIncome(), money.USCents(125200_00))
	want(t, "line 28", f.GetTotalTax(), money.USCents(6260_00))
	want(t, "line 38", f.GetMassachusettsTaxWithheld(), money.USCents(6200_00))
	want(t, "line 48", f.GetTotalRefundableCredits(), money.USCents(880_00))
	want(t, "line 51", f.GetTotalPayments(), money.USCents(7080_00))
	want(t, "line 54", f.GetRefund(), money.USCents(820_00))
}

// Single, 70 years old, $1,500,000 of wages: the surtax applies.
//   - Line 10: $1,500,000. Line 11a: $2,000. Line 17: $1,498,000.
//   - Line 2a: $4,400. Line 2c: 65 or over before 2026, 1 x $1,700 (the form
//     prints $1,700; the instructions' text still says $700). Line 18: $6,100.
//   - Line 19, 21: $1,491,900. Line 22: x .05 = $74,595.
//   - Line 28b: Schedule 4% Surtax: ($1,491,900 - $1,083,150) x .04 =
//     $408,750 x .04 = $16,350 (instructions, "4% Surtax": the 2025 threshold
//     is $1,083,150).
//   - Line 28, 37: $74,595 + $16,350 = $90,945.
//   - Line 38a, 51: $92,000 withheld. Line 54: refund $1,055.
func TestMassachusettsHighIncomeSurtax(t *testing.T) {
	_, f := prepareMA(t, maInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		ilPerson("ana", 1955, 7, 7, false, false), nil, nil,
		[]*formw2.FormW2{maW2(1500000_00, 92000_00)},
		&returnsv1.MassachusettsInputs{},
	))
	want(t, "line 2c", f.GetAge_65Exemption(), money.USCents(1700_00))
	want(t, "line 18", f.GetExemptionAmount(), money.USCents(6100_00))
	want(t, "line 21", f.GetTaxableIncome(), money.USCents(1491900_00))
	want(t, "line 22", f.GetTaxOnTaxableIncome(), money.USCents(74595_00))
	want(t, "line 28a", f.GetIncomeTax(), money.USCents(74595_00))
	want(t, "line 28b", f.GetSurtax(), money.USCents(16350_00))
	want(t, "line 28", f.GetTotalTax(), money.USCents(90945_00))
	want(t, "line 54", f.GetRefund(), money.USCents(1055_00))
}

// Single, $30,000 of wages: line 21 is under $24,000, so the tax table applies.
//   - Line 11a: $1,860 + $435 = $2,295, not more than $2,000. Line 17: $28,000.
//   - Line 18: $4,400. Line 19, 21: $23,600.
//   - Line 22: the tax table row "more than $23,550, not more than $23,600"
//     taxes its middle, $23,575 x .05 = $1,178.75, shown as $1,179 (one
//     dollar under $23,600 x .05 = $1,180).
//   - Line 38a, 51: $1,000 withheld. Line 55: tax due $179.
func TestMassachusettsTaxTable(t *testing.T) {
	_, f := prepareMA(t, maInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		ilPerson("ana", 1995, 9, 9, false, false), nil, nil,
		[]*formw2.FormW2{maW2(30000_00, 1000_00)},
		&returnsv1.MassachusettsInputs{},
	))
	want(t, "line 21", f.GetTaxableIncome(), money.USCents(23600_00))
	want(t, "line 22", f.GetTaxOnTaxableIncome(), money.USCents(1179_00))
	want(t, "line 55", f.GetAmountOwed(), money.USCents(179_00))
}
