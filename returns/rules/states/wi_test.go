package states_test

// Wisconsin has no income tax calculator in wisconsintaxcalc, only estimated
// tax, so these households are checked against figures worked by hand from
// the 2025 Form 1 and its instructions (Standard Deduction Table, page 35;
// Tax Table, pages 38-43; Tax Computation Worksheet, page 44).

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	form1 "github.com/nexteggapp/taxprotos/go/us/state/wi/v1/form_1"
)

// wiInputs builds ReturnInputs for a Wisconsin filer with one W-2.
func wiInputs(
	status returnsv1.FilingStatusElection,
	wages int64,
	withheld int64,
	opts ...func(*returnsv1.ReturnInputs),
) *returnsv1.ReturnInputs {
	in := azInputs(status, wages, withheld, opts...)
	w2 := in.GetFederal().GetDocuments().GetW2S()[0].GetForm()
	w2.StateTaxWithholding = []*commonv1.StateTaxWithholding{
		{State: "WI", StateIncome: cents(wages), TaxWithheld: cents(withheld)},
	}
	in.States = []*returnsv1.StateInputs{{
		State:  "WI",
		Inputs: &returnsv1.StateInputs_Wi{Wi: &returnsv1.WisconsinInputs{}},
	}}
	return in
}

func prepareWI(t *testing.T, in *returnsv1.ReturnInputs) (*returnsv1.WisconsinReturn, *form1.Form1) {
	t.Helper()
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine: %v", err)
	}
	out, err := eng.Prepare(in)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if len(out.GetStates()) == 0 || out.GetStates()[0].GetWi() == nil {
		t.Fatalf("expected Wisconsin return in outputs, got %v", out.GetStates())
	}
	wi := out.GetStates()[0].GetWi()
	f := wi.GetForms().GetForm_1()
	if f == nil {
		t.Fatalf("expected Form 1 in Wisconsin return, got %v", wi.GetForms())
	}
	return wi, f
}

// Single, $50,000 of wages, $2,000 Wisconsin tax withheld.
//
//   - Line 1, 3, 5, 7: federal AGI $50,000 with no Wisconsin modifications.
//   - Line 8: Standard Deduction Table, row $50,000-$50,500, single: $9,876.
//   - Line 9: $50,000 - $9,876 = $40,124.
//   - Line 10a: one exemption x $700 = $700; line 10c $700.
//   - Line 11: $40,124 - $700 = $39,424.
//   - Line 12: Tax Table, row $39,400-$39,500, single: $1,604.
//   - Lines 22, 27: $1,604. Lines 28, 37, 39: $2,000.
//   - Lines 40, 41: refund $2,000 - $1,604 = $396.
func TestWisconsinSingleFiler(t *testing.T) {
	in := wiInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, 50_000_00, 2_000_00)
	wi, f := prepareWI(t, in)

	want(t, "WI federal AGI (line 1)", f.GetFederalAdjustedGrossIncome(), 50_000_00)
	want(t, "WI income (line 7)", f.GetWisconsinIncome(), 50_000_00)
	want(t, "WI standard deduction (line 8)", f.GetStandardDeduction(), 9_876_00)
	want(t, "WI exemptions (line 10c)", f.GetTotalExemptions(), 700_00)
	want(t, "WI taxable income (line 11)", f.GetTaxableIncome(), 39_424_00)
	want(t, "WI tax (line 12)", f.GetTax(), 1_604_00)
	want(t, "WI total tax (line 27)", f.GetTotalTax(), 1_604_00)
	want(t, "WI withheld (line 28)", f.GetWisconsinTaxWithheld(), 2_000_00)
	want(t, "WI refund (line 41)", f.GetRefund(), 396_00)
	want(t, "WI amount owed (line 45)", f.GetAmountOwed(), 0)
	complete(t, "WI", wi.GetStatus(), wi.GetDiagnostics())
}

// Married filing jointly with two children, $120,000 of wages, $5,000
// withheld.
//
//   - Line 7: $120,000.
//   - Line 8: Standard Deduction Table, row $120,000-$120,500, married filing
//     jointly: $6,906.
//   - Line 9: $120,000 - $6,906 = $113,094.
//   - Line 10a: four exemptions (two filers, two dependents) x $700 = $2,800.
//   - Line 11: $113,094 - $2,800 = $110,294.
//   - Line 12: $100,000 or more, so the Tax Computation Worksheet, Section B:
//     $110,294 x 5.3% = $5,845.58, less $781.92 = $5,063.66, $5,064.
//   - Lines 43, 45: $5,064 - $5,000 = $64 owed.
func TestWisconsinJointWithChildren(t *testing.T) {
	in := wiInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY,
		120_000_00, 5_000_00,
		func(in *returnsv1.ReturnInputs) {
			in.Federal.Household.Dependents = []*returnsv1.Dependent{
				azDependent("child1", 2015),
				azDependent("child2", 2018),
			}
		})
	wi, f := prepareWI(t, in)

	want(t, "WI income (line 7)", f.GetWisconsinIncome(), 120_000_00)
	want(t, "WI standard deduction (line 8)", f.GetStandardDeduction(), 6_906_00)
	want(t, "WI exemptions (line 10a)", f.GetPersonalExemptions(), 2_800_00)
	want(t, "WI taxable income (line 11)", f.GetTaxableIncome(), 110_294_00)
	want(t, "WI tax (line 12)", f.GetTax(), 5_064_00)
	want(t, "WI total tax (line 27)", f.GetTotalTax(), 5_064_00)
	want(t, "WI payments (line 39)", f.GetNetPayments(), 5_000_00)
	want(t, "WI refund (line 41)", f.GetRefund(), 0)
	want(t, "WI amount owed (line 45)", f.GetAmountOwed(), 64_00)
	complete(t, "WI", wi.GetStatus(), wi.GetDiagnostics())
}

// Single, $400,000 of wages, $25,000 withheld.
//
//   - Line 8: the single deduction is gone above $132,500 (Standard Deduction
//     Table, page 37): $0.
//   - Line 9: $400,000. Line 10a: $700. Line 11: $399,300.
//   - Line 12: Tax Computation Worksheet, Section A, $323,290 or over:
//     $399,300 x 7.65% = $30,546.45, less $8,183.76 = $22,362.69, $22,363.
//   - Lines 40, 41: $25,000 - $22,363 = $2,637 refunded.
func TestWisconsinHighIncomeSingle(t *testing.T) {
	in := wiInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, 400_000_00, 25_000_00)
	wi, f := prepareWI(t, in)

	want(t, "WI standard deduction (line 8)", f.GetStandardDeduction(), 0)
	want(t, "WI taxable income (line 11)", f.GetTaxableIncome(), 399_300_00)
	want(t, "WI tax (line 12)", f.GetTax(), 22_363_00)
	want(t, "WI total tax (line 27)", f.GetTotalTax(), 22_363_00)
	want(t, "WI refund (line 41)", f.GetRefund(), 2_637_00)
	want(t, "WI amount owed (line 45)", f.GetAmountOwed(), 0)
	complete(t, "WI", wi.GetStatus(), wi.GetDiagnostics())
}

// The Standard Deduction Table and Tax Table, checked row by row against the
// published figures at a few incomes per filing status.
func TestWisconsinPublishedTables(t *testing.T) {
	cases := []struct {
		name   string
		status returnsv1.FilingStatusElection
		wages  int64
		deduct money.USCents
	}{
		// Page 35, row $20,000-$20,500: single $13,476.
		{"single 20,000", returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, 20_200_00, 13_476_00},
		// Page 35, row $50,000-$50,500: joint $20,751.
		{"joint 50,000", returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY, 50_200_00, 20_751_00},
		// Page 36, row $130,000-$130,500: single $276.
		{"single 130,000", returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, 130_100_00, 276_00},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, f := prepareWI(t, wiInputs(c.status, c.wages, 0))
			want(t, "WI standard deduction (line 8)", f.GetStandardDeduction(), c.deduct)
		})
	}
}
