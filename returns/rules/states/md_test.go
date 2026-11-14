package states_test

// Maryland Form 502. marylandtaxcalc holds only estimated-tax logic (its
// doc.go says there is no income tax calculator there yet), so these
// households are worked by hand from the 2025 Form 502 and the 2025 Maryland
// resident tax booklet, line by line, in the comments.

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
	form502 "github.com/nexteggapp/taxprotos/go/us/state/md/v1/form_502"
)

func mdW2(wages, fedWithheld, mdWithheld int64) *formw2.FormW2 {
	return &formw2.FormW2{
		WagesTipsOtherCompensation: cents(wages),
		FederalTaxWithheld:         cents(fedWithheld),
		StateTaxWithholding: []*commonv1.StateTaxWithholding{
			{State: "MD", StateIncome: cents(wages), TaxWithheld: cents(mdWithheld)},
		},
	}
}

func mdInputs(
	status returnsv1.FilingStatusElection,
	taxpayer, spouse *returnsv1.Person,
	dependents []*returnsv1.Dependent,
	w2s []*formw2.FormW2,
	county string,
	rateBP int32,
) *returnsv1.ReturnInputs {
	in := ilInputs(status, taxpayer, spouse, dependents, w2s, 0)
	in.States = []*returnsv1.StateInputs{{
		State: "MD",
		Inputs: &returnsv1.StateInputs_Md{Md: &returnsv1.MarylandInputs{
			County:         county,
			LocalTaxRateBp: rateBP,
		}},
	}}
	return in
}

func prepareMaryland(t *testing.T, in *returnsv1.ReturnInputs) (*returnsv1.MarylandReturn, *form502.Form502) {
	t.Helper()
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatal(err)
	}
	out, err := eng.Prepare(in)
	if err != nil {
		t.Fatal(err)
	}
	ret := out.GetStates()[0].GetMd()
	if ret == nil {
		t.Fatal("expected a Maryland return")
	}
	if ret.GetStatus() != returnsv1.ReturnStatus_RETURN_STATUS_COMPLETE {
		t.Errorf("expected a complete return, got %v: %v", ret.GetStatus(), ret.GetDiagnostics())
	}
	return ret, ret.GetForms().GetForm_502()
}

// A single filer in Montgomery County with $60,000 of wages.
//
//   - Line 1, federal AGI: $60,000. No additions or subtractions, so line 16
//     is $60,000.
//   - Line 17: standard deduction $3,350 for a single filer (Instruction 16).
//   - Line 18: $60,000 - $3,350 = $56,650.
//   - Exemption A: one exemption; federal AGI is $100,000 or less, so it is
//     $3,200 (Exemption Amount Chart, Instruction 10). Line 19: $3,200.
//   - Line 20: $56,650 - $3,200 = $53,450.
//   - Line 21: under $100,000, so the tax table (Instruction 17). Its row
//     $53,450 to $53,500 prints $2,488: Schedule I at the row's middle,
//     $90 + 4.75% x ($53,475 - $3,000) = $2,487.56.
//   - Line 28: Montgomery County .0320 (Local Tax Rate Chart, Instruction 19):
//     $53,450 x .032 = $1,710.40, so $1,710.
//   - Line 34: $2,488 + $1,710 = $4,198. Line 40 is the same.
//   - Line 41: $3,000 withheld. Line 47 and 52: $4,198 - $3,000 = $1,198.
func TestMarylandSingle(t *testing.T) {
	p := ilPerson("john", 1988, 5, 12, false, false)
	in := mdInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		p, nil, nil, []*formw2.FormW2{mdW2(60000_00, 6000_00, 3000_00)}, "Montgomery County", 320)
	ret, f := prepareMaryland(t, in)

	want(t, "Line 1", f.GetFederalAdjustedGrossIncome(), money.USCents(60000_00))
	want(t, "Line 16", f.GetMarylandAdjustedGrossIncome(), money.USCents(60000_00))
	want(t, "Line 17", f.GetDeduction(), money.USCents(3350_00))
	want(t, "Line 19", f.GetExemptionAmount(), money.USCents(3200_00))
	want(t, "Line 20", f.GetTaxableNetIncome(), money.USCents(53450_00))
	want(t, "Line 21", f.GetMarylandTax(), money.USCents(2488_00))
	want(t, "Line 28", f.GetLocalTax(), money.USCents(1710_00))
	want(t, "Line 34", f.GetTotalMarylandAndLocalTax(), money.USCents(4198_00))
	want(t, "Line 41", f.GetMarylandAndLocalTaxWithheld(), money.USCents(3000_00))
	want(t, "Line 46", f.GetTotalPaymentsAndCredits(), money.USCents(3000_00))
	want(t, "Line 48", f.GetOverpayment(), money.USCents(0))
	want(t, "Line 52", f.GetTotalAmountDue(), money.USCents(1198_00))
	want(t, "summary tax", ret.GetSummary().GetTotalTax(), money.USCents(4198_00))
	want(t, "summary owed", ret.GetSummary().GetAmountOwed(), money.USCents(1198_00))
}

// A married couple filing jointly in Harford County, wages of $95,000 and
// $45,000, with two children.
//
//   - Line 1: $140,000. Line 16: $140,000.
//   - Line 17: standard deduction $6,700 for a joint return (Instruction 16).
//   - Line 18: $140,000 - $6,700 = $133,300.
//   - Exemptions: federal AGI is $150,000 or less, so each is $3,200 on the
//     joint column of the Exemption Amount Chart. A: 2 x $3,200 = $6,400;
//     C: 2 x $3,200 = $6,400; D and line 19: $12,800.
//   - Line 20: $133,300 - $12,800 = $120,500.
//   - Line 21: $100,000 or more, so Schedule II of the Tax Computation
//     Worksheet (Instruction 17): $90 + 4.75% x ($120,500 - $3,000) =
//     $5,671.25, so $5,671.
//   - Line 28: Harford County .0306 (Instruction 19): $120,500 x .0306 =
//     $3,687.30, so $3,687.
//   - Line 34: $5,671 + $3,687 = $9,358.
//   - Line 41: $6,500 + $3,500 = $10,000 withheld. Line 48 and 50:
//     $10,000 - $9,358 = $642 refunded.
func TestMarylandMarriedFilingJointlyWithChildren(t *testing.T) {
	p1 := ilPerson("alice", 1985, 3, 15, false, false)
	p2 := ilPerson("bob", 1984, 8, 20, false, false)
	kids := []*returnsv1.Dependent{ilDependent("kid1", 2015, 6, 1), ilDependent("kid2", 2018, 9, 9)}
	in := mdInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY,
		p1, p2, kids,
		[]*formw2.FormW2{mdW2(95000_00, 9000_00, 6500_00), mdW2(45000_00, 3000_00, 3500_00)},
		"Harford County", 306)
	ret, f := prepareMaryland(t, in)

	want(t, "Line 1", f.GetFederalAdjustedGrossIncome(), money.USCents(140000_00))
	want(t, "Line 17", f.GetDeduction(), money.USCents(6700_00))
	want(t, "Exemption A", f.GetPersonalExemption(), money.USCents(6400_00))
	want(t, "Exemption C", f.GetDependentExemption(), money.USCents(6400_00))
	want(t, "Line 19", f.GetExemptionAmount(), money.USCents(12800_00))
	want(t, "Line 20", f.GetTaxableNetIncome(), money.USCents(120500_00))
	want(t, "Line 21", f.GetMarylandTax(), money.USCents(5671_00))
	want(t, "Line 28", f.GetLocalTax(), money.USCents(3687_00))
	want(t, "Line 34", f.GetTotalMarylandAndLocalTax(), money.USCents(9358_00))
	want(t, "Line 46", f.GetTotalPaymentsAndCredits(), money.USCents(10000_00))
	want(t, "Line 48", f.GetOverpayment(), money.USCents(642_00))
	want(t, "Line 50", f.GetRefund(), money.USCents(642_00))
	want(t, "Line 52", f.GetTotalAmountDue(), money.USCents(0))
	want(t, "summary refund", ret.GetSummary().GetRefund(), money.USCents(642_00))
}

// A single filer in Baltimore City with $600,000 of wages.
//
//   - Line 1: $600,000. Line 17: standard deduction $3,350.
//   - Line 18: $596,650.
//   - Exemption A: federal AGI is over $150,000, so the single column of the
//     Exemption Amount Chart allows $0. Line 19: $0.
//   - Line 20: $596,650.
//   - Line 21: Schedule I, "$500,001 to $1,000,000: $27,135.00 plus 6.25% of
//     excess over $500,000" (Instruction 17): $27,135 + 6.25% x $96,650 =
//     $33,175.63, so $33,176.
//   - Line 28: Baltimore City .0320: $596,650 x .032 = $19,092.80, so $19,093.
//   - Line 34: $33,176 + $19,093 = $52,269.
//   - Line 41: $40,000 withheld. Line 52: $52,269 - $40,000 = $12,269.
func TestMarylandHighIncome(t *testing.T) {
	p := ilPerson("carol", 1970, 1, 2, false, false)
	in := mdInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		p, nil, nil, []*formw2.FormW2{mdW2(600000_00, 150000_00, 40000_00)}, "Baltimore City", 320)
	ret, f := prepareMaryland(t, in)

	want(t, "Line 1", f.GetFederalAdjustedGrossIncome(), money.USCents(600000_00))
	want(t, "Line 19", f.GetExemptionAmount(), money.USCents(0))
	want(t, "Line 20", f.GetTaxableNetIncome(), money.USCents(596650_00))
	want(t, "Line 21", f.GetMarylandTax(), money.USCents(33176_00))
	want(t, "Line 28", f.GetLocalTax(), money.USCents(19093_00))
	want(t, "Line 34", f.GetTotalMarylandAndLocalTax(), money.USCents(52269_00))
	want(t, "Line 52", f.GetTotalAmountDue(), money.USCents(12269_00))
	want(t, "summary owed", ret.GetSummary().GetAmountOwed(), money.USCents(12269_00))
}
