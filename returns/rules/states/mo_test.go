package states_test

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
	formmo1040 "github.com/nexteggapp/taxprotos/go/us/state/mo/v1/form_mo1040"
)

// missouritaxcalc holds only estimated-tax logic, so there is no Go income
// tax calculator to compare against. Each household below is worked by hand
// from the 2025 Form MO-1040 and its instructions; the page numbers are the
// instruction booklet's printed pages.

func moInputs(
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
		States: []*returnsv1.StateInputs{{
			State:  "MO",
			Inputs: &returnsv1.StateInputs_Mo{Mo: &returnsv1.MissouriInputs{}},
		}},
	}
}

func moW2(wages, fedWithheld, moWithheld int64) *formw2.FormW2 {
	return &formw2.FormW2{
		WagesTipsOtherCompensation: cents(wages),
		FederalTaxWithheld:         cents(fedWithheld),
		StateTaxWithholding: []*commonv1.StateTaxWithholding{
			{State: "MO", StateIncome: cents(wages), TaxWithheld: cents(moWithheld)},
		},
	}
}

func prepareMissouri(t *testing.T, in *returnsv1.ReturnInputs) (*returnsv1.MissouriReturn, *formmo1040.FormMO1040) {
	t.Helper()
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatal(err)
	}
	out, err := eng.Prepare(in)
	if err != nil {
		t.Fatal(err)
	}
	ret := out.GetStates()[0].GetMo()
	if ret == nil {
		t.Fatalf("expected a Missouri return, got %v", out.GetStates()[0])
	}
	if ret.GetStatus() != returnsv1.ReturnStatus_RETURN_STATUS_COMPLETE {
		t.Errorf("expected a complete return, got %v: %v", ret.GetStatus(), ret.GetDiagnostics())
	}
	return ret, ret.GetForms().GetFormMo1040()
}

// Single, $60,000 of wages, $2,500 of Missouri tax withheld.
//
//   - Line 1Y: federal AGI, $60,000 (instructions p. 6, Line 1).
//   - Lines 2 to 5: no modifications; line 6, $60,000.
//   - Line 7Y: 100%, one column only (p. 7, Line 7).
//   - Line 9: the tax on the federal return (p. 7, Line 9). Federal taxable
//     income is $60,000 - $15,750 = $44,250. The federal rules here figure
//     the tax from the rate schedule on the exact income, 10% of $11,925
//     plus 12% of $32,325 = $5,071.50, $5,072. (The IRS tax table, figured
//     at the row's midpoint, would print $5,075; that is a federal matter,
//     and Missouri takes whatever the federal return shows.)
//   - Line 12: line 6 is in $50,001 to $100,000, so 15% (p. 8, Line 12).
//   - Line 13: 15% of $5,072 = $760.80, $761, under the $5,000 limit (p. 8).
//   - Line 14: the single standard deduction, $15,750 (p. 8, Line 14).
//   - Line 25: $761 + $15,750 = $16,511; line 26: $60,000 - $16,511 = $43,489.
//   - Lines 27Y and 29Y: $43,489.
//   - Line 30Y: tax chart (p. 21, Section A), over $9,191: $256 + 4.7% of
//     $34,298 = $256 + $1,612.01 = $1,868.01, $1,868 (Section B rounds).
//   - Line 36: $1,868. Line 37: $2,500. Line 49 and 53: refund $632.
func TestMissouriSingle(t *testing.T) {
	p := ilPerson("pat", 1990, 4, 2, false, false)
	in := moInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, p, nil, nil,
		[]*formw2.FormW2{moW2(60000_00, 6000_00, 2500_00)})
	_, f := prepareMissouri(t, in)

	want(t, "1Y federal AGI", f.GetFederalAdjustedGrossIncomeYourself(), money.USCents(60000_00))
	want(t, "6 Missouri AGI", f.GetTotalMissouriAdjustedGrossIncome(), money.USCents(60000_00))
	if f.GetIncomePercentageYourself() != 100 || f.GetFederalTaxPercentage() != 15 {
		t.Errorf("7Y = %d, 12 = %d; want 100 and 15", f.GetIncomePercentageYourself(), f.GetFederalTaxPercentage())
	}
	want(t, "9 federal tax", f.GetFederalTax(), money.USCents(5072_00))
	want(t, "13 federal tax deduction", f.GetFederalIncomeTaxDeduction(), money.USCents(761_00))
	want(t, "14 standard deduction", f.GetStandardOrItemizedDeduction(), money.USCents(15750_00))
	want(t, "26 subtotal", f.GetIncomeAfterDeductions(), money.USCents(43489_00))
	want(t, "29Y taxable income", f.GetTaxableIncomeYourself(), money.USCents(43489_00))
	want(t, "30Y tax", f.GetTaxYourself(), money.USCents(1868_00))
	want(t, "36 total tax", f.GetTotalTax(), money.USCents(1868_00))
	want(t, "45 payments", f.GetTotalPaymentsAndCredits(), money.USCents(2500_00))
	want(t, "53 refund", f.GetRefund(), money.USCents(632_00))
	want(t, "56 amount due", f.GetAmountDue(), money.USCents(0))
}

// Married filing combined, two children, wages of $80,000 (filer, $2,000 of
// Missouri tax withheld) and $40,000 (spouse, $1,000 withheld).
//
//   - Lines 1Y and 1S: the AGI worksheet (p. 7) splits by each spouse's
//     wages: $80,000 and $40,000. Line 6: $120,000.
//   - Line 7: $80,000 / $120,000 = 66.7%, rounded to 67%; 7S is 33% (p. 7).
//   - Line 9: federal taxable income $120,000 - $31,500 = $88,500; from the
//     rate schedule, 10% of $23,850 plus 12% of $64,650 = $10,143 (the tax
//     table would print $10,146, as for the single filer); less the $4,400
//     child tax credit for two children leaves $5,743 (Form 1040 line 22).
//   - Line 12: $100,001 to $125,000, so 5%. Line 13: 5% of $5,743 = $287.15,
//     $287.
//   - Line 14: the combined standard deduction, $31,500.
//   - Line 25: $31,787; line 26: $120,000 - $31,787 = $88,213.
//   - Line 27Y: 67% of $88,213 = $59,102.71, $59,103; 27S: 33% = $29,110.29,
//     $29,110.
//   - Line 30Y: $256 + 4.7% of ($59,103 - $9,191) = $256 + $2,345.86 =
//     $2,601.86, $2,602. Line 30S: $256 + 4.7% of ($29,110 - $9,191) = $256 +
//     $936.19 = $1,192.19, $1,192 (p. 21).
//   - Line 36: $3,794. Line 37: $3,000. Line 54 and 56: $794 due.
func TestMissouriMarriedFilingCombined(t *testing.T) {
	p1 := ilPerson("alex", 1985, 3, 15, false, false)
	p2 := ilPerson("sam", 1986, 8, 20, false, false)
	kids := []*returnsv1.Dependent{
		ilDependent("kid1", 2015, 6, 1),
		ilDependent("kid2", 2018, 9, 10),
	}
	in := moInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY, p1, p2, kids,
		[]*formw2.FormW2{moW2(80000_00, 7000_00, 2000_00), moW2(40000_00, 3000_00, 1000_00)})
	_, f := prepareMissouri(t, in)

	want(t, "1Y federal AGI", f.GetFederalAdjustedGrossIncomeYourself(), money.USCents(80000_00))
	want(t, "1S federal AGI", f.GetFederalAdjustedGrossIncomeSpouse(), money.USCents(40000_00))
	if f.GetIncomePercentageYourself() != 67 || f.GetIncomePercentageSpouse() != 33 || f.GetFederalTaxPercentage() != 5 {
		t.Errorf("7Y = %d, 7S = %d, 12 = %d; want 67, 33 and 5",
			f.GetIncomePercentageYourself(), f.GetIncomePercentageSpouse(), f.GetFederalTaxPercentage())
	}
	want(t, "9 federal tax", f.GetFederalTax(), money.USCents(5743_00))
	want(t, "13 federal tax deduction", f.GetFederalIncomeTaxDeduction(), money.USCents(287_00))
	want(t, "14 standard deduction", f.GetStandardOrItemizedDeduction(), money.USCents(31500_00))
	want(t, "26 subtotal", f.GetIncomeAfterDeductions(), money.USCents(88213_00))
	want(t, "27Y", f.GetApportionedIncomeYourself(), money.USCents(59103_00))
	want(t, "27S", f.GetApportionedIncomeSpouse(), money.USCents(29110_00))
	want(t, "30Y tax", f.GetTaxYourself(), money.USCents(2602_00))
	want(t, "30S tax", f.GetTaxSpouse(), money.USCents(1192_00))
	want(t, "36 total tax", f.GetTotalTax(), money.USCents(3794_00))
	want(t, "45 payments", f.GetTotalPaymentsAndCredits(), money.USCents(3000_00))
	want(t, "53 refund", f.GetRefund(), money.USCents(0))
	want(t, "56 amount due", f.GetAmountDue(), money.USCents(794_00))
}

// Single, $300,000 of wages, $14,000 of Missouri tax withheld.
//
//   - Line 6: $300,000. Line 12: $125,001 or more, so 0%; line 13: $0
//     whatever the federal tax (p. 8).
//   - Line 14: $15,750. Line 26: $284,250.
//   - Line 30Y: $256 + 4.7% of ($284,250 - $9,191) = $256 + $12,927.77 =
//     $13,183.77, $13,184 (p. 21).
//   - Line 36: $13,184. Line 37: $14,000. Line 53: refund $816.
func TestMissouriHighIncome(t *testing.T) {
	p := ilPerson("lee", 1975, 1, 20, false, false)
	in := moInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, p, nil, nil,
		[]*formw2.FormW2{moW2(300000_00, 70000_00, 14000_00)})
	_, f := prepareMissouri(t, in)

	if f.GetFederalTaxPercentage() != 0 {
		t.Errorf("12 = %d, want 0", f.GetFederalTaxPercentage())
	}
	want(t, "13 federal tax deduction", f.GetFederalIncomeTaxDeduction(), money.USCents(0))
	want(t, "26 subtotal", f.GetIncomeAfterDeductions(), money.USCents(284250_00))
	want(t, "30Y tax", f.GetTaxYourself(), money.USCents(13184_00))
	want(t, "36 total tax", f.GetTotalTax(), money.USCents(13184_00))
	want(t, "53 refund", f.GetRefund(), money.USCents(816_00))
	want(t, "56 amount due", f.GetAmountDue(), money.USCents(0))
}
