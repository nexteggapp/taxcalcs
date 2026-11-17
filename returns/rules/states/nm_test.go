package states_test

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
	formpit1 "github.com/nexteggapp/taxprotos/go/us/state/nm/v1/form_pit1"
)

// There is no Go income tax calculator for New Mexico, so these households are
// worked by hand from the 2025 Form PIT-1, its instructions and the rate
// schedules of NMSA 1978 7-2-7. The federal standard deduction for 2025 is
// $15,750 single and $31,500 joint.
//
// Line 18 is figured from the statutory rate schedule. The department's tax
// tables price each $50 band of taxable income at its midpoint, so the table
// figure can differ from these by a dollar or so.

func nmInputs(
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
			State:  "NM",
			Inputs: &returnsv1.StateInputs_Nm{Nm: &returnsv1.NewMexicoInputs{}},
		}},
	}
}

func nmW2(wages, fedWithheld, nmWithheld int64) *formw2.FormW2 {
	return &formw2.FormW2{
		WagesTipsOtherCompensation: cents(wages),
		FederalTaxWithheld:         cents(fedWithheld),
		StateTaxWithholding: []*commonv1.StateTaxWithholding{
			{State: "NM", StateIncome: cents(wages), TaxWithheld: cents(nmWithheld)},
		},
	}
}

func prepareNM(t *testing.T, in *returnsv1.ReturnInputs) *formpit1.FormPIT1 {
	t.Helper()
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatal(err)
	}
	out, err := eng.Prepare(in)
	if err != nil {
		t.Fatal(err)
	}
	ret := out.GetStates()[0].GetNm()
	if ret == nil {
		t.Fatal("expected New Mexico return")
	}
	return ret.GetForms().GetFormPit1()
}

// A single filer with $30,100 of wages.
//
//   - Line 9 federal AGI: $30,100.
//   - Line 12 federal standard deduction: $15,750.
//   - Line 13: none; the deduction is for joint and head of household filers
//     (instructions, line 13).
//   - Line 14 worksheet (instructions page PIT-1-26): AGI $30,100 is at or
//     below $36,667. Line 3: $30,100 - $20,000 = $10,100. Line 5: $10,100 x
//     0.15 = $1,515. Line 6: $2,500 - $1,515 = $985. Line 8: $985 x 1
//     exemption = $985.
//   - Line 17: $30,100 - $15,750 - $985 = $13,365.
//   - Line 18, single schedule (NMSA 1978 7-2-7): $5,500 x 1.5% = $82.50,
//     plus ($13,365 - $5,500) x 3.2% = $251.68; $334.18, rounded $334.
//   - Line 22: $334. Line 25: no federal EIC at this income, $0.
//   - Line 27 withheld: $500. Line 32: $500.
//   - Line 39 overpayment and line 42 refund: $500 - $334 = $166.
func TestNewMexicoSingle(t *testing.T) {
	in := nmInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		ilPerson("ana", 1990, 4, 2, false, false), nil, nil,
		[]*formw2.FormW2{nmW2(30100_00, 2000_00, 500_00)})
	f := prepareNM(t, in)

	want(t, "Line 9", f.GetFederalAdjustedGrossIncome(), money.USCents(30100_00))
	want(t, "Line 12", f.GetFederalDeduction(), money.USCents(15750_00))
	want(t, "Line 13", f.GetCertainDependentsDeduction(), money.USCents(0))
	want(t, "Line 14", f.GetLowAndMiddleIncomeExemption(), money.USCents(985_00))
	want(t, "Line 17", f.GetTaxableIncome(), money.USCents(13365_00))
	want(t, "Line 18", f.GetTax(), money.USCents(334_00))
	want(t, "Line 22", f.GetNetIncomeTax(), money.USCents(334_00))
	want(t, "Line 25", f.GetWorkingFamiliesTaxCredit(), money.USCents(0))
	want(t, "Line 27", f.GetTaxWithheld(), money.USCents(500_00))
	want(t, "Line 32", f.GetTotalPaymentsAndCredits(), money.USCents(500_00))
	want(t, "Line 38", f.GetTotalDue(), money.USCents(0))
	want(t, "Line 42", f.GetRefund(), money.USCents(166_00))
	if f.GetExemptions() != 1 {
		t.Errorf("box 5 exemptions = %d, want 1", f.GetExemptions())
	}
}

// A married couple filing jointly with two children and $70,200 of wages.
//
//   - Line 9 federal AGI: $70,200.
//   - Line 12 federal standard deduction: $31,500.
//   - Line 13 worksheet (instructions, line 13): 2 dependents less 1 = 1;
//     1 x $4,000 = $4,000.
//   - Line 14: AGI is above $55,000, so no exemption.
//   - Line 17: $70,200 - $31,500 - $4,000 = $34,700.
//   - Line 18, joint schedule (NMSA 1978 7-2-7): $8,000 x 1.5% = $120, plus
//     $17,000 x 3.2% = $544, plus ($34,700 - $25,000) x 4.3% = $417.10;
//     $1,081.10, rounded $1,081.
//   - Line 25: the federal EIC for two children ends below this income, $0.
//   - Line 27 withheld: $1,500. Line 32: $1,500.
//   - Line 42 refund: $1,500 - $1,081 = $419.
func TestNewMexicoMarriedFilingJointlyWithChildren(t *testing.T) {
	in := nmInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY,
		ilPerson("ben", 1986, 1, 9, false, false),
		ilPerson("cara", 1987, 6, 30, false, false),
		[]*returnsv1.Dependent{ilDependent("dee", 2020, 3, 1), ilDependent("eli", 2017, 9, 15)},
		[]*formw2.FormW2{nmW2(70200_00, 3000_00, 1500_00)})
	f := prepareNM(t, in)

	want(t, "Line 9", f.GetFederalAdjustedGrossIncome(), money.USCents(70200_00))
	want(t, "Line 12", f.GetFederalDeduction(), money.USCents(31500_00))
	want(t, "Line 13", f.GetCertainDependentsDeduction(), money.USCents(4000_00))
	want(t, "Line 14", f.GetLowAndMiddleIncomeExemption(), money.USCents(0))
	want(t, "Line 17", f.GetTaxableIncome(), money.USCents(34700_00))
	want(t, "Line 18", f.GetTax(), money.USCents(1081_00))
	want(t, "Line 25", f.GetWorkingFamiliesTaxCredit(), money.USCents(0))
	want(t, "Line 32", f.GetTotalPaymentsAndCredits(), money.USCents(1500_00))
	want(t, "Line 38", f.GetTotalDue(), money.USCents(0))
	want(t, "Line 42", f.GetRefund(), money.USCents(419_00))
	if f.GetExemptions() != 4 {
		t.Errorf("box 5 exemptions = %d, want 4", f.GetExemptions())
	}
}

// A single filer with $300,000 of wages, taxed in the top band.
//
//   - Line 9: $300,000. Line 12: $15,750. Lines 13 and 14: none.
//   - Line 17: $284,250.
//   - Line 18, single schedule (NMSA 1978 7-2-7): $82.50 + $11,000 x 3.2%
//     ($352) + $17,000 x 4.3% ($731) + $33,000 x 4.7% ($1,551) + $143,500 x
//     4.9% ($7,031.50) + ($284,250 - $210,000) x 5.9% ($4,380.75) =
//     $14,128.75, rounded $14,129.
//   - Line 27 withheld: $14,000. Line 33 tax due and line 38: $129.
func TestNewMexicoHighIncomeSingle(t *testing.T) {
	in := nmInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		ilPerson("fay", 1970, 11, 5, false, false), nil, nil,
		[]*formw2.FormW2{nmW2(300000_00, 60000_00, 14000_00)})
	f := prepareNM(t, in)

	want(t, "Line 17", f.GetTaxableIncome(), money.USCents(284250_00))
	want(t, "Line 18", f.GetTax(), money.USCents(14129_00))
	want(t, "Line 22", f.GetNetIncomeTax(), money.USCents(14129_00))
	want(t, "Line 32", f.GetTotalPaymentsAndCredits(), money.USCents(14000_00))
	want(t, "Line 33", f.GetTaxDue(), money.USCents(129_00))
	want(t, "Line 38", f.GetTotalDue(), money.USCents(129_00))
	want(t, "Line 42", f.GetRefund(), money.USCents(0))
}
