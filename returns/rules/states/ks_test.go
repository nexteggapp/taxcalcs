package states_test

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
	formk40 "github.com/nexteggapp/taxprotos/go/us/state/ks/v1/form_k40"
)

// kansastaxcalc holds only estimated-tax logic, so these figures are worked by
// hand from the 2025 Form K-40 and its instructions (ip25.pdf). Every Tax
// Table figure below was checked against the printed table.

// ksInputs builds return inputs for a Kansas resident with wages only.
func ksInputs(
	status returnsv1.FilingStatusElection,
	taxpayer *returnsv1.Person,
	spouse *returnsv1.Person,
	dependents []*returnsv1.Dependent,
	w2s []*formw2.FormW2,
) *returnsv1.ReturnInputs {
	in := ilInputs(status, taxpayer, spouse, dependents, w2s, 0)
	in.States = []*returnsv1.StateInputs{{
		State:  "KS",
		Inputs: &returnsv1.StateInputs_Ks{Ks: &returnsv1.KansasInputs{}},
	}}
	return in
}

func ksW2(wages, fedWithheld, ksWithheld int64) *formw2.FormW2 {
	return &formw2.FormW2{
		WagesTipsOtherCompensation: cents(wages),
		FederalTaxWithheld:         cents(fedWithheld),
		StateTaxWithholding: []*commonv1.StateTaxWithholding{
			{State: "KS", StateIncome: cents(wages), TaxWithheld: cents(ksWithheld)},
		},
	}
}

func prepareKansas(t *testing.T, in *returnsv1.ReturnInputs) (*returnsv1.KansasReturn, *formk40.FormK40) {
	t.Helper()
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatal(err)
	}
	out, err := eng.Prepare(in)
	if err != nil {
		t.Fatal(err)
	}
	ret := out.GetStates()[0].GetKs()
	if ret == nil {
		t.Fatal("expected Kansas return")
	}
	if ret.GetStatus() != returnsv1.ReturnStatus_RETURN_STATUS_COMPLETE {
		t.Errorf("expected complete return, got %v: %v", ret.GetStatus(), ret.GetDiagnostics())
	}
	return ret, ret.GetForms().GetFormK40()
}

// Single filer with $50,000 of wages and $2,000 of Kansas withholding.
//   - Line 1 federal AGI: $50,000.
//   - Line 2 modifications: none; line 3 Kansas AGI $50,000.
//   - Line 4 standard deduction, single: $3,605 (instructions p. 6, Line 4).
//   - Exemptions, single: $9,160 (instructions p. 2); line 5 $9,160.
//   - Line 6: $12,765. Line 7: $50,000 - $12,765 = $37,235.
//   - Line 8: Tax Table row 37,201-37,250, single column: $1,990
//     (5.2% of $23,000 + 5.58% of the $14,225.50 above it at the row's
//     midpoint, $1,989.78).
//   - Lines 12, 16, 18: $1,990. Line 19 withheld $2,000; line 27 $2,000.
//   - Line 33 overpayment and line 43 refund: $10. Line 32: $0.
func TestKansasSingle(t *testing.T) {
	in := ksInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		ilPerson("ann", 1990, 3, 4, false, false), nil, nil,
		[]*formw2.FormW2{ksW2(50000_00, 5000_00, 2000_00)},
	)
	ret, f := prepareKansas(t, in)

	want(t, "Line 1 Federal AGI", f.GetFederalAdjustedGrossIncome(), money.USCents(50000_00))
	want(t, "Line 3 Kansas AGI", f.GetKansasAdjustedGrossIncome(), money.USCents(50000_00))
	want(t, "Line 4 Standard deduction", f.GetStandardOrItemizedDeduction(), money.USCents(3605_00))
	want(t, "Line 5 Exemption allowance", f.GetExemptionAllowance(), money.USCents(9160_00))
	want(t, "Line 7 Taxable income", f.GetTaxableIncome(), money.USCents(37235_00))
	want(t, "Line 8 Tax", f.GetTax(), money.USCents(1990_00))
	want(t, "Line 18 Total tax balance", f.GetTotalTaxBalance(), money.USCents(1990_00))
	want(t, "Line 19 Withheld", f.GetKansasTaxWithheld(), money.USCents(2000_00))
	want(t, "Line 27 Total refundable credits", f.GetTotalRefundableCredits(), money.USCents(2000_00))
	want(t, "Line 43 Refund", f.GetRefund(), money.USCents(10_00))
	want(t, "Line 32 Amount owed", f.GetAmountOwed(), money.USCents(0))
	want(t, "Summary tax", ret.GetSummary().GetTotalTax(), money.USCents(1990_00))
}

// Married filing jointly, two earners and two children.
//   - Line 1: $90,000 + $30,000 = $120,000; line 3 the same.
//   - Line 4 standard deduction, married filing joint: $8,240.
//   - Exemptions: $18,320 for married filing joint, plus 2 dependents x $2,320
//     = $4,640 (instructions p. 2); total and line 5: $22,960.
//   - Line 6: $31,200. Line 7: $120,000 - $31,200 = $88,800.
//   - Line 8: Tax Table row 88,751-88,800, married filing joint column: $4,779
//     (5.2% of $46,000 + 5.58% of $42,775.50, $4,778.87).
//   - No federal EITC at this income, so lines 17 and 22 are $0.
//   - Line 19 withheld: $4,000 + $1,500 = $5,500.
//   - Line 43 refund: $5,500 - $4,779 = $721.
func TestKansasMarriedFilingJointlyWithChildren(t *testing.T) {
	in := ksInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY,
		ilPerson("bob", 1984, 1, 2, false, false),
		ilPerson("cat", 1986, 6, 7, false, false),
		[]*returnsv1.Dependent{ilDependent("dan", 2015, 8, 9), ilDependent("eve", 2018, 10, 11)},
		[]*formw2.FormW2{ksW2(90000_00, 8000_00, 4000_00), ksW2(30000_00, 2000_00, 1500_00)},
	)
	_, f := prepareKansas(t, in)

	if f.GetFilingStatus() != formk40.FilingStatus_FILING_STATUS_MARRIED_FILING_JOINTLY {
		t.Errorf("filing status: got %v", f.GetFilingStatus())
	}
	want(t, "Line 1 Federal AGI", f.GetFederalAdjustedGrossIncome(), money.USCents(120000_00))
	want(t, "Line 4 Standard deduction", f.GetStandardOrItemizedDeduction(), money.USCents(8240_00))
	want(t, "Dependent exemption", f.GetDependentExemption(), money.USCents(4640_00))
	want(t, "Line 5 Exemption allowance", f.GetExemptionAllowance(), money.USCents(22960_00))
	want(t, "Line 7 Taxable income", f.GetTaxableIncome(), money.USCents(88800_00))
	want(t, "Line 8 Tax", f.GetTax(), money.USCents(4779_00))
	want(t, "Line 17 EITC", f.GetEarnedIncomeCredit(), money.USCents(0))
	want(t, "Line 18 Total tax balance", f.GetTotalTaxBalance(), money.USCents(4779_00))
	want(t, "Line 19 Withheld", f.GetKansasTaxWithheld(), money.USCents(5500_00))
	want(t, "Line 43 Refund", f.GetRefund(), money.USCents(721_00))
	want(t, "Line 32 Amount owed", f.GetAmountOwed(), money.USCents(0))
}

// High-income single filer: taxable income over $100,000 uses the Tax
// Computation Worksheet (instructions p. 34).
//   - Line 1: $250,000. Line 4: $3,605. Line 5: $9,160. Line 7: $237,235.
//   - Line 8: $237,235 x 5.58% = $13,237.71, less the $87 the worksheet
//     subtracts, $13,150.71, entered as $13,151.
//   - Line 19 withheld: $13,000. Line 28 underpayment and line 32: $151.
func TestKansasHighIncomeSingle(t *testing.T) {
	in := ksInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		ilPerson("fay", 1975, 2, 3, false, false), nil, nil,
		[]*formw2.FormW2{ksW2(250000_00, 50000_00, 13000_00)},
	)
	_, f := prepareKansas(t, in)

	want(t, "Line 7 Taxable income", f.GetTaxableIncome(), money.USCents(237235_00))
	want(t, "Line 8 Tax", f.GetTax(), money.USCents(13151_00))
	want(t, "Line 28 Underpayment", f.GetUnderpayment(), money.USCents(151_00))
	want(t, "Line 32 Amount owed", f.GetAmountOwed(), money.USCents(151_00))
	want(t, "Line 43 Refund", f.GetRefund(), money.USCents(0))
}

// Single filer aged 70 with $40,000 of wages.
//   - Line 4: the worksheet for people 65 or older or blind, single with one
//     box checked: $4,455 (instructions p. 6).
//   - Line 5: $9,160. Line 7: $40,000 - $13,615 = $26,385.
//   - Line 8: Tax Table row 26,351-26,400, single column: $1,384.
//   - Line 19 withheld: $1,300. Line 32 amount owed: $84.
func TestKansasSingleOver65(t *testing.T) {
	in := ksInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		ilPerson("gus", 1955, 4, 5, false, false), nil, nil,
		[]*formw2.FormW2{ksW2(40000_00, 3000_00, 1300_00)},
	)
	_, f := prepareKansas(t, in)

	want(t, "Line 4 Standard deduction", f.GetStandardOrItemizedDeduction(), money.USCents(4455_00))
	want(t, "Line 7 Taxable income", f.GetTaxableIncome(), money.USCents(26385_00))
	want(t, "Line 8 Tax", f.GetTax(), money.USCents(1384_00))
	want(t, "Line 32 Amount owed", f.GetAmountOwed(), money.USCents(84_00))
}
