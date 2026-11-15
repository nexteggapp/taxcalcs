package states_test

// Rhode Island has no Go income tax calculator (rhodeislandtaxcalc holds only
// estimated tax), so these tests check the RI-1040 rules against figures
// worked out by hand from the 2025 RI-1040 instructions, the 2025 Tax Rate
// Schedule and Worksheets and the 2025 RI Tax Tables. Page numbers are the
// PDF's own pages, as sources/catalog/ri/2025 counts them:
//   - instructions page 4, Line 4: standard deduction $10,900 single, $21,800
//     married joint, $16,350 head of household.
//   - instructions page 5, Line 6: each exemption is $5,100.
//   - worksheets page 1, Standard Deduction and Exemption Worksheets: above
//     $254,250 of modified federal AGI, the excess is divided by $7,250 and
//     rounded up; one to four steps leave 0.8000, 0.6000, 0.4000 or 0.2000 of
//     the full amount, and an excess above $29,000 leaves nothing.
//   - tax tables pages 2 to 7, RI Tax Table: taxable income below $100,000,
//     in $50 rows. A row's tax is the rate schedule's tax at its midpoint,
//     rounded to the dollar; the first row, $0 to $50, prints $0.
//   - tax tables page 1, Tax Computation Worksheet, from $100,000: 3.75% to
//     $79,900; 4.75% less $799.00 to $181,650; 5.99% less $3,051.46 above.

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
	formri1040 "github.com/nexteggapp/taxprotos/go/us/state/ri/v1/form_ri1040"
)

// riInputs is a household with one W-2 of the given wages, Rhode Island
// withholding and no other income, filing an RI-1040.
func riInputs(
	status returnsv1.FilingStatusElection,
	wages int64,
	withheld int64,
	opts ...func(*returnsv1.ReturnInputs),
) *returnsv1.ReturnInputs {
	person := func(id string, birthYear int32) *returnsv1.Person {
		return &returnsv1.Person{
			Id:          id,
			FirstName:   id,
			DateOfBirth: &commonv1.Date{Year: birthYear, Month: 6, Day: 1},
			Tin: &returnsv1.TaxpayerID{
				Kind:               returnsv1.TaxpayerIDKind_TAXPAYER_ID_KIND_SSN,
				ValidForEmployment: boolPtr(true),
			},
		}
	}
	hh := &returnsv1.Household{
		MaritalStatus: returnsv1.MaritalStatus_MARITAL_STATUS_NEVER_MARRIED,
		Taxpayer:      person("taxpayer", 1980),
	}
	if status == returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY {
		hh.MaritalStatus = returnsv1.MaritalStatus_MARITAL_STATUS_MARRIED
		hh.Spouse = person("spouse", 1982)
	}
	w2 := &formw2.FormW2{
		WagesTipsOtherCompensation: cents(wages),
		FederalTaxWithheld:         cents(wages / 10),
		StateTaxWithholding: []*commonv1.StateTaxWithholding{
			{State: "RI", StateIncome: cents(wages), TaxWithheld: cents(withheld)},
		},
	}
	in := &returnsv1.ReturnInputs{
		SchemaVersion: 1,
		TaxYear:       2025,
		Federal: &returnsv1.Federal1040Inputs{
			Household: hh,
			Elections: &returnsv1.Elections{FilingStatus: status},
			Documents: &returnsv1.Documents{
				W2S: []*returnsv1.W2Document{{Id: "w2-ri", Person: "taxpayer", Form: w2}},
			},
		},
		States: []*returnsv1.StateInputs{{
			State:  "RI",
			Inputs: &returnsv1.StateInputs_Ri{Ri: &returnsv1.RhodeIslandInputs{}},
		}},
	}
	for _, opt := range opts {
		opt(in)
	}
	return in
}

func prepareRI(t *testing.T, in *returnsv1.ReturnInputs) (*returnsv1.RhodeIslandReturn, *formri1040.FormRI1040) {
	t.Helper()
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine: %v", err)
	}
	out, err := eng.Prepare(in)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if len(out.GetStates()) == 0 || out.GetStates()[0].GetRi() == nil {
		t.Fatalf("expected Rhode Island return in outputs, got %v", out.GetStates())
	}
	ri := out.GetStates()[0].GetRi()
	f := ri.GetForms().GetFormRi1040()
	if f == nil {
		t.Fatalf("expected Form RI-1040 in Rhode Island return, got %v", ri.GetForms())
	}
	return ri, f
}

// A single filer with $60,000 of wages and $2,000 withheld.
//   - Line 1: federal AGI $60,000.00; line 2: no modifications; line 3 $60,000.00.
//   - Line 4 (page 4): single standard deduction $10,900.00. Line 5: $49,100.00.
//   - Line 6 (page 5): one exemption, $5,100.00. Line 7: $44,000.00.
//   - Line 8 (RI Tax Table, row $44,000 to $44,050, printed tax $1,651):
//     the midpoint $44,025 x 3.75% = $1,650.94, rounded to $1,651.00.
//   - Lines 10a and 13a: $1,651.00, with no credits or other taxes.
//   - Line 14a and 14i: $2,000.00 withheld.
//   - Lines 16 and 17: $2,000.00 - $1,651.00 = $349.00 refunded.
func TestRhodeIslandSingle(t *testing.T) {
	ri, f := prepareRI(t, riInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, 60_000_00, 2_000_00))

	want(t, "RI federal AGI (line 1)", f.GetFederalAdjustedGrossIncome(), 60_000_00)
	want(t, "RI modified AGI (line 3)", f.GetModifiedFederalAdjustedGrossIncome(), 60_000_00)
	want(t, "RI standard deduction (line 4)", f.GetStandardDeduction(), 10_900_00)
	want(t, "RI exemption amount (line 6)", f.GetExemptionAmount(), 5_100_00)
	want(t, "RI taxable income (line 7)", f.GetTaxableIncome(), 44_000_00)
	want(t, "RI income tax (line 8)", f.GetIncomeTax(), 1_651_00)
	want(t, "RI total tax (line 13a)", f.GetTotalTaxAndContributions(), 1_651_00)
	want(t, "RI withholding (line 14a)", f.GetRhodeIslandTaxWithheld(), 2_000_00)
	want(t, "RI net payments (line 14i)", f.GetNetPayments(), 2_000_00)
	want(t, "RI amount due (line 15c)", f.GetTotalAmountDue(), 0)
	want(t, "RI refund (line 17)", f.GetRefund(), 349_00)
	complete(t, "RI", ri.GetStatus(), ri.GetDiagnostics())
}

// A married couple filing jointly with two children, $120,000 of wages and
// $4,000 withheld. Their federal earned income credit is nil at this income.
//   - Lines 1 and 3: $120,000.00.
//   - Line 4 (page 4): married joint standard deduction $21,800.00. Line 5: $98,200.00.
//   - Line 6 (page 5): four exemptions, 4 x $5,100.00 = $20,400.00. Line 7: $77,800.00.
//   - Line 8 (RI Tax Table, row $77,800 to $77,850, printed tax $2,918):
//     the midpoint $77,825 x 3.75% = $2,918.44, rounded to $2,918.00.
//   - Line 14a: $4,000.00. Lines 16 and 17: $4,000.00 - $2,918.00 = $1,082.00.
func TestRhodeIslandMarriedJointWithChildren(t *testing.T) {
	in := riInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY,
		120_000_00,
		4_000_00,
		func(i *returnsv1.ReturnInputs) {
			i.Federal.Household.Dependents = []*returnsv1.Dependent{
				azDependent("child1", 2015),
				azDependent("child2", 2018),
			}
		},
	)
	ri, f := prepareRI(t, in)

	if f.GetExemptions() != 4 {
		t.Errorf("RI exemptions (line 6 box) = %d, want 4", f.GetExemptions())
	}
	want(t, "RI standard deduction (line 4)", f.GetStandardDeduction(), 21_800_00)
	want(t, "RI exemption amount (line 6)", f.GetExemptionAmount(), 20_400_00)
	want(t, "RI taxable income (line 7)", f.GetTaxableIncome(), 77_800_00)
	want(t, "RI income tax (line 8)", f.GetIncomeTax(), 2_918_00)
	want(t, "RI earned income credit (line 14d)", f.GetEarnedIncomeCredit(), 0)
	want(t, "RI total payments (line 14g)", f.GetTotalPayments(), 4_000_00)
	want(t, "RI refund (line 17)", f.GetRefund(), 1_082_00)
	want(t, "RI amount due (line 15c)", f.GetTotalAmountDue(), 0)
	complete(t, "RI", ri.GetStatus(), ri.GetDiagnostics())
}

// A single filer with $200,000 of wages, below the $254,250 phase-out, and
// $7,500 withheld.
//   - Lines 1 and 3: $200,000.00.
//   - Line 4: $10,900.00. Line 5: $189,100.00.
//   - Line 6: one exemption, $5,100.00. Line 7: $184,000.00.
//   - Line 8 (Tax Computation Worksheet, top band, over $181,650):
//     $184,000.00 x 5.99% = $11,021.60, less $3,051.46 = $7,970.14.
//   - Line 14a: $7,500.00. Lines 15a and 15c: $7,970.14 - $7,500.00 = $470.14 due.
func TestRhodeIslandHighIncome(t *testing.T) {
	ri, f := prepareRI(t, riInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, 200_000_00, 7_500_00))

	want(t, "RI taxable income (line 7)", f.GetTaxableIncome(), 184_000_00)
	want(t, "RI income tax (line 8)", f.GetIncomeTax(), 7_970_14)
	want(t, "RI balance due (line 15a)", f.GetBalanceDue(), 470_14)
	want(t, "RI amount due (line 15c)", f.GetTotalAmountDue(), 470_14)
	want(t, "RI refund (line 17)", f.GetRefund(), 0)
	complete(t, "RI", ri.GetStatus(), ri.GetDiagnostics())
}

// The middle band: a single filer with $120,000 of wages.
//   - Line 7: $120,000.00 - $10,900.00 - $5,100.00 = $104,000.00.
//   - Line 8 (Tax Computation Worksheet, from $100,000; band $79,900 to
//     $181,650):
//     $104,000.00 x 4.75% = $4,940.00, less $799.00 = $4,141.00.
func TestRhodeIslandMiddleBand(t *testing.T) {
	_, f := prepareRI(t, riInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, 120_000_00, 4_141_00))

	want(t, "RI taxable income (line 7)", f.GetTaxableIncome(), 104_000_00)
	want(t, "RI income tax (line 8)", f.GetIncomeTax(), 4_141_00)
	want(t, "RI amount due (line 15c)", f.GetTotalAmountDue(), 0)
	want(t, "RI refund (line 17)", f.GetRefund(), 0)
}

// A single filer with $270,000 of wages, in the phase-out, and $12,000
// withheld.
//   - Lines 1 and 3: $270,000.00.
//   - Standard Deduction Worksheet: line 1 $10,900.00; line 2 $270,000.00;
//     line 4 $254,250.00; line 5 $15,750.00, not more than $29,000; line 6
//     $15,750 / $7,250 = 2.17, increased to 3; line 7 0.4000; line 8
//     $10,900.00 x 0.4000 = $4,360.00. Line 4: $4,360.00. Line 5: $265,640.00.
//   - Exemption Worksheet: line 1 one exemption, $5,100.00; lines 2 to 7 as
//     above; line 8 $5,100.00 x 0.4000 = $2,040.00. Line 6: $2,040.00.
//   - Line 7: $265,640.00 - $2,040.00 = $263,600.00.
//   - Line 8 (Tax Computation Worksheet, top band): $263,600.00 x 5.99% =
//     $15,789.64, less $3,051.46 = $12,738.18.
//   - Line 14a: $12,000.00. Line 15c: $12,738.18 - $12,000.00 = $738.18 due.
func TestRhodeIslandPhaseOut(t *testing.T) {
	ri, f := prepareRI(t, riInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, 270_000_00, 12_000_00))

	want(t, "RI modified AGI (line 3)", f.GetModifiedFederalAdjustedGrossIncome(), 270_000_00)
	want(t, "RI standard deduction (line 4)", f.GetStandardDeduction(), 4_360_00)
	want(t, "RI income after deduction (line 5)", f.GetIncomeAfterDeduction(), 265_640_00)
	want(t, "RI exemption amount (line 6)", f.GetExemptionAmount(), 2_040_00)
	want(t, "RI taxable income (line 7)", f.GetTaxableIncome(), 263_600_00)
	want(t, "RI income tax (line 8)", f.GetIncomeTax(), 12_738_18)
	want(t, "RI amount due (line 15c)", f.GetTotalAmountDue(), 738_18)
	want(t, "RI summary deductions", ri.GetSummary().GetTotalDeductions(), 6_400_00)
	complete(t, "RI", ri.GetStatus(), ri.GetDiagnostics())
}

// A married couple filing jointly with $261,500 of wages: an excess of
// exactly one $7,250 step.
//   - Lines 1 and 3: $261,500.00. Two exemptions.
//   - Worksheets: line 5 $261,500 - $254,250 = $7,250.00; line 6 exactly 1;
//     line 7 0.8000.
//   - Line 4: $21,800.00 x 0.8000 = $17,440.00. Line 5: $244,060.00.
//   - Line 6: 2 x $5,100.00 = $10,200.00, x 0.8000 = $8,160.00.
//   - Line 7: $244,060.00 - $8,160.00 = $235,900.00.
//   - Line 8: $235,900.00 x 5.99% = $14,130.41, less $3,051.46 = $11,078.95.
//   - Line 14a: $11,000.00. Line 15c: $78.95 due.
func TestRhodeIslandPhaseOutOneStep(t *testing.T) {
	ri, f := prepareRI(t, riInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY, 261_500_00, 11_000_00))

	if f.GetExemptions() != 2 {
		t.Errorf("RI exemptions (line 6 box) = %d, want 2", f.GetExemptions())
	}
	want(t, "RI standard deduction (line 4)", f.GetStandardDeduction(), 17_440_00)
	want(t, "RI exemption amount (line 6)", f.GetExemptionAmount(), 8_160_00)
	want(t, "RI taxable income (line 7)", f.GetTaxableIncome(), 235_900_00)
	want(t, "RI income tax (line 8)", f.GetIncomeTax(), 11_078_95)
	want(t, "RI amount due (line 15c)", f.GetTotalAmountDue(), 78_95)
	complete(t, "RI", ri.GetStatus(), ri.GetDiagnostics())
}

// A married couple filing jointly with $300,000 of wages, past the phase-out.
//   - Lines 1 and 3: $300,000.00. Worksheets, line 5: $45,750.00, more than
//     $29,000, so the standard deduction and exemptions are $0.
//   - Line 7: $300,000.00.
//   - Line 8: $300,000.00 x 5.99% = $17,970.00, less $3,051.46 = $14,918.54.
//   - Line 14a: $15,000.00. Line 17: $81.46 refunded.
func TestRhodeIslandPhasedOutFully(t *testing.T) {
	ri, f := prepareRI(t, riInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY, 300_000_00, 15_000_00))

	want(t, "RI standard deduction (line 4)", f.GetStandardDeduction(), 0)
	want(t, "RI exemption amount (line 6)", f.GetExemptionAmount(), 0)
	want(t, "RI taxable income (line 7)", f.GetTaxableIncome(), 300_000_00)
	want(t, "RI income tax (line 8)", f.GetIncomeTax(), 14_918_54)
	want(t, "RI refund (line 17)", f.GetRefund(), 81_46)
	complete(t, "RI", ri.GetStatus(), ri.GetDiagnostics())
}

// The first rows of the RI Tax Table, for single filers whose wages leave a
// little taxable income after the $10,900 deduction and $5,100 exemption.
//   - $16,030 of wages: line 7 $30.00, in the first row ($0 to $50), which
//     prints $0 although its midpoint, $25 x 3.75% = $0.94, would round to $1.
//   - $16,075 of wages: line 7 $75.00, in the row $50 to $100, printed tax
//     $3: the midpoint $75 x 3.75% = $2.81, rounded to $3.00.
//   - $116,000 of wages: line 7 $100,000.00, where the table stops; the Tax
//     Computation Worksheet gives $100,000.00 x 4.75% = $4,750.00, less
//     $799.00 = $3,951.00. The table's last row, $99,950 to $100,000,
//     prints $3,950.
func TestRhodeIslandTaxTableEdges(t *testing.T) {
	for _, c := range []struct {
		wages, taxable, tax int64
	}{
		{16_030_00, 30_00, 0},
		{16_075_00, 75_00, 3_00},
		{116_000_00, 100_000_00, 3_951_00},
	} {
		_, f := prepareRI(t, riInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, c.wages, 0))
		want(t, "RI taxable income (line 7)", f.GetTaxableIncome(), money.USCents(c.taxable))
		want(t, "RI income tax (line 8)", f.GetIncomeTax(), money.USCents(c.tax))
	}
}
