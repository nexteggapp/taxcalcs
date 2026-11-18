package states_test

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	form1099r "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_r"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
	formit540 "github.com/nexteggapp/taxprotos/go/us/state/la/v1/form_it540"
)

// There is no Go income tax calculator for Louisiana, so these households are
// worked by hand from the 2025 Form IT-540 and its instructions (IT-540i),
// line by line, and the rules are checked against that working.

func laPerson(id string, birthYear int32) *returnsv1.Person {
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

// laInputs builds a Louisiana filer with one W-2, whose state box reports
// Louisiana withholding.
func laInputs(
	status returnsv1.FilingStatusElection,
	wages int64,
	withheld int64,
	opts ...func(*returnsv1.ReturnInputs),
) *returnsv1.ReturnInputs {
	hh := &returnsv1.Household{
		MaritalStatus: returnsv1.MaritalStatus_MARITAL_STATUS_NEVER_MARRIED,
		Taxpayer:      laPerson("taxpayer", 1985),
	}
	if status == returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY {
		hh.MaritalStatus = returnsv1.MaritalStatus_MARITAL_STATUS_MARRIED
		hh.Spouse = laPerson("spouse", 1987)
	}
	docs := &returnsv1.Documents{}
	if wages > 0 {
		docs.W2S = []*returnsv1.W2Document{{
			Id:     "w2-la",
			Person: "taxpayer",
			Form: &formw2.FormW2{
				WagesTipsOtherCompensation: cents(wages),
				FederalTaxWithheld:         cents(wages / 10),
				StateTaxWithholding: []*commonv1.StateTaxWithholding{
					{State: "LA", StateIncome: cents(wages), TaxWithheld: cents(withheld)},
				},
			},
		}}
	}
	in := &returnsv1.ReturnInputs{
		SchemaVersion: 1,
		TaxYear:       2025,
		Federal: &returnsv1.Federal1040Inputs{
			Household: hh,
			Elections: &returnsv1.Elections{FilingStatus: status},
			Documents: docs,
		},
		States: []*returnsv1.StateInputs{{
			State:  "LA",
			Inputs: &returnsv1.StateInputs_La{La: &returnsv1.LouisianaInputs{}},
		}},
	}
	for _, opt := range opts {
		opt(in)
	}
	return in
}

func prepareLA(t *testing.T, in *returnsv1.ReturnInputs) (*returnsv1.LouisianaReturn, *formit540.FormIT540) {
	t.Helper()
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine: %v", err)
	}
	out, err := eng.Prepare(in)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if len(out.GetStates()) == 0 || out.GetStates()[0].GetLa() == nil {
		t.Fatalf("expected a Louisiana return, got %v", out.GetStates())
	}
	la := out.GetStates()[0].GetLa()
	f := la.GetForms().GetFormIt540()
	if f == nil {
		t.Fatalf("expected Form IT-540 in the Louisiana return, got %v", la.GetForms())
	}
	return la, f
}

// A single filer with $60,000 of wages and $1,500 of Louisiana withholding.
//
//   - Line 7: federal AGI $60,000 (IT-540i line 7; no Schedule E items).
//   - Line 8: $12,500, filing status 1 (IT-540i line 8; La. R.S. 47:294).
//   - Line 10: $60,000 - $12,500 = $47,500.
//   - Line 11: $47,500 x 3% = $1,425 (IT-540i line 11; La. R.S. 47:32).
//   - Lines 13, 19, 22, 24: $1,425, with no credits.
//   - Lines 27, 31: $1,500 withheld.
//   - Lines 32, 34, 36, 38: refund $1,500 - $1,425 = $75.
func TestLouisianaSingleFiler(t *testing.T) {
	in := laInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, 60_000_00, 1_500_00)
	la, f := prepareLA(t, in)

	if f.GetFilingStatus() != formit540.FilingStatus_FILING_STATUS_SINGLE {
		t.Errorf("filing status = %v", f.GetFilingStatus())
	}
	want(t, "LA line 7", f.GetAdjustedGrossIncome(), 60_000_00)
	want(t, "LA line 8", f.GetStandardDeduction(), 12_500_00)
	want(t, "LA line 10", f.GetTaxableIncome(), 47_500_00)
	want(t, "LA line 11", f.GetIncomeTax(), 1_425_00)
	want(t, "LA line 22", f.GetAdjustedIncomeTax(), 1_425_00)
	want(t, "LA line 24", f.GetTotalTax(), 1_425_00)
	want(t, "LA line 27", f.GetLouisianaTaxWithheld(), 1_500_00)
	want(t, "LA line 31", f.GetTotalCreditsAndPayments(), 1_500_00)
	want(t, "LA line 38", f.GetRefund(), 75_00)
	want(t, "LA line 47", f.GetBalanceDue(), 0)
	want(t, "LA summary tax", la.GetSummary().GetTotalTax(), 1_425_00)
	want(t, "LA summary refund", la.GetSummary().GetRefund(), 75_00)
	complete(t, "LA", la.GetStatus(), la.GetDiagnostics())
}

// Married filing jointly with two children, $90,000 of wages and $1,800 of
// Louisiana withholding. The dependents carry no exemption since Act 11 of
// the 2024 Third Extraordinary Session repealed it (IT-540i, What's New).
//
//   - Line 7: $90,000.
//   - Line 8: $25,000, filing status 2 (IT-540i line 8).
//   - Line 10: $90,000 - $25,000 = $65,000.
//   - Line 11: $65,000 x 3% = $1,950.
//   - Line 16: no federal earned income credit at this income, so none here.
//   - Line 24: $1,950. Line 31: $1,800.
//   - Line 39 and 47: $1,950 - $1,800 = $150 owed.
func TestLouisianaMarriedWithChildren(t *testing.T) {
	in := laInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY, 90_000_00, 1_800_00,
		func(in *returnsv1.ReturnInputs) {
			in.Federal.Household.Dependents = []*returnsv1.Dependent{
				azDependent("child1", 2015),
				azDependent("child2", 2018),
			}
		})
	la, f := prepareLA(t, in)

	if got := f.GetDependents(); got != 2 {
		t.Errorf("dependents = %d, want 2", got)
	}
	want(t, "LA line 7", f.GetAdjustedGrossIncome(), 90_000_00)
	want(t, "LA line 8", f.GetStandardDeduction(), 25_000_00)
	want(t, "LA line 10", f.GetTaxableIncome(), 65_000_00)
	want(t, "LA line 11", f.GetIncomeTax(), 1_950_00)
	want(t, "LA line 16", f.GetEarnedIncomeCredit(), 0)
	want(t, "LA line 24", f.GetTotalTax(), 1_950_00)
	want(t, "LA line 31", f.GetTotalCreditsAndPayments(), 1_800_00)
	want(t, "LA line 38", f.GetRefund(), 0)
	want(t, "LA line 39", f.GetAmountOwed(), 150_00)
	want(t, "LA line 47", f.GetBalanceDue(), 150_00)
	want(t, "LA summary owed", la.GetSummary().GetAmountOwed(), 150_00)
	complete(t, "LA", la.GetStatus(), la.GetDiagnostics())
}

// A retired couple, both 65 or older, with no wages: a $60,000 pension paid
// to the taxpayer, with $1,000 of Louisiana tax withheld, and a $20,000 IRA
// distribution paid to the spouse.
//
//   - Schedule E line 1: federal AGI $80,000 (Form 1040 lines 4b + 5b).
//   - Schedule E line 3: $80,000, with no additions.
//   - Worksheet for Code 06E (IT-540i page 6; La. R.S. 47:44.1): line 5(a)
//     the smaller of $60,000 and $12,000 = $12,000; line 5(b) the smaller of
//     $20,000 and $12,000 = $12,000; line 6 $24,000 on a joint return.
//   - Schedule E line 4H: $24,000. Line 5: $80,000 - $24,000 = $56,000.
//   - Line 7: $56,000. Line 8: $25,000. Line 10: $31,000.
//   - Line 11: $31,000 x 3% = $930.
//   - Line 27: $1,000 withheld on the Form 1099-R.
//   - Line 38: refund $1,000 - $930 = $70.
func TestLouisianaRetiredCouple(t *testing.T) {
	in := laInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY, 0, 0,
		func(in *returnsv1.ReturnInputs) {
			in.Federal.Household.Taxpayer.DateOfBirth = &commonv1.Date{Year: 1955, Month: 3, Day: 1}
			in.Federal.Household.Spouse.DateOfBirth = &commonv1.Date{Year: 1957, Month: 3, Day: 1}
			in.Federal.Documents.RetirementDistributions = []*returnsv1.Form1099RDocument{
				{
					Id:     "1099r-pension",
					Person: "taxpayer",
					Form: &form1099r.Form1099R{
						GrossDistribution: cents(60_000_00),
						TaxableAmount:     cents(60_000_00),
						DistributionCodes: []string{"7"},
						StateTaxWithholding: []*commonv1.StateTaxWithholding{
							{State: "LA", TaxWithheld: cents(1_000_00)},
						},
					},
				},
				{
					Id:     "1099r-ira",
					Person: "spouse",
					Form: &form1099r.Form1099R{
						GrossDistribution: cents(20_000_00),
						TaxableAmount:     cents(20_000_00),
						DistributionCodes: []string{"7"},
						IraSepSimple:      true,
					},
				},
			}
		})
	la, f := prepareLA(t, in)

	if got := f.GetAge_65OrOlder(); got != 2 {
		t.Errorf("age 65 or older = %d, want 2", got)
	}
	want(t, "LA Schedule E line 1", f.GetScheduleEFederalAdjustedGrossIncome(), 80_000_00)
	want(t, "LA Schedule E line 3", f.GetScheduleETotal(), 80_000_00)
	want(t, "LA Schedule E code 06E", f.GetRetirementIncomeExemption(), 24_000_00)
	want(t, "LA Schedule E line 4H", f.GetTotalExemptIncome(), 24_000_00)
	want(t, "LA Schedule E line 5", f.GetScheduleELouisianaAdjustedGrossIncome(), 56_000_00)
	want(t, "LA line 7", f.GetAdjustedGrossIncome(), 56_000_00)
	want(t, "LA line 10", f.GetTaxableIncome(), 31_000_00)
	want(t, "LA line 11", f.GetIncomeTax(), 930_00)
	want(t, "LA line 24", f.GetTotalTax(), 930_00)
	want(t, "LA line 27", f.GetLouisianaTaxWithheld(), 1_000_00)
	want(t, "LA line 38", f.GetRefund(), 70_00)
	want(t, "LA line 47", f.GetBalanceDue(), 0)
	complete(t, "LA", la.GetStatus(), la.GetDiagnostics())
}

// A head of household with one child and $20,000 of wages, nothing withheld.
//
//   - Federal earned income credit, one qualifying child, 2025 (Rev. Proc.
//     2024-40 section 2.06): 34% of $20,000 = $6,800, capped at $4,328; the
//     $20,000 of AGI is below the $23,350 phase-out threshold, so $4,328.
//   - Line 8: $25,000, filing status 4. Line 10: $0. Line 11: $0.
//   - Line 16: 5% of $4,328 = $216.40, rounded to $216 (IT-540i line 16;
//     La. R.S. 47:297.8).
//   - Line 18: $216. Line 19: $0. Line 20: $216 - $0 = $216.
//   - Line 25: $216 from line 20. Line 31: $216.
//   - Line 38: refund $216.
//
// The return also carries the diagnostic that the refundable child care
// credit, open to filers with federal AGI of $25,000 or less, is not built.
func TestLouisianaEarnedIncomeCredit(t *testing.T) {
	in := laInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_HEAD_OF_HOUSEHOLD, 20_000_00, 0,
		func(in *returnsv1.ReturnInputs) {
			in.Federal.Household.MaritalStatus = returnsv1.MaritalStatus_MARITAL_STATUS_NEVER_MARRIED
			in.Federal.Household.Dependents = []*returnsv1.Dependent{azDependent("child1", 2016)}
		})
	_, f := prepareLA(t, in)

	want(t, "LA line 8", f.GetStandardDeduction(), 25_000_00)
	want(t, "LA line 11", f.GetIncomeTax(), 0)
	want(t, "LA line 16", f.GetEarnedIncomeCredit(), 216_00)
	want(t, "LA line 20", f.GetPriority_2CreditOverpayment(), 216_00)
	want(t, "LA line 25", f.GetPriority_2CreditOverpaymentApplied(), 216_00)
	want(t, "LA line 31", f.GetTotalCreditsAndPayments(), 216_00)
	want(t, "LA line 38", f.GetRefund(), 216_00)
}

// A single filer with $60,000 of wages, $1,500 of Louisiana withholding, and
// $30,010 of unreimbursed medical expenses, who itemizes federally.
//
//   - Federal Schedule A line 4: $30,010 - 7.5% of $60,000 ($4,500) = $25,510.
//     Line 5a: the $1,500 of state income tax withheld. Line 17: $27,010,
//     above the $15,750 federal standard deduction, so the return itemizes.
//   - Line 9A: $27,010. Line 9B: $25,510. Line 9C: $15,750.
//   - Line 9D: $25,510 - $15,750 = $9,760 (IT-540i lines 9A to 9D;
//     La. R.S. 47:293(3)).
//   - Line 10: $60,000 - $12,500 - $9,760 = $37,740.
//   - Line 11: $37,740 x 3% = $1,132.20, rounded to $1,132.
//   - Line 38: refund $1,500 - $1,132 = $368.
func TestLouisianaExcessMedicalDeduction(t *testing.T) {
	in := laInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, 60_000_00, 1_500_00,
		func(in *returnsv1.ReturnInputs) {
			in.Federal.Deductions = &returnsv1.Deductions{
				ItemizedComplete: boolPtr(true),
				Medical:          &returnsv1.MedicalExpenses{UnreimbursedMedicalAndDental: cents(30_010_00)},
			}
		})
	la, f := prepareLA(t, in)

	want(t, "LA line 9A", f.GetFederalItemizedDeductions(), 27_010_00)
	want(t, "LA line 9B", f.GetFederalMedicalAndDentalDeduction(), 25_510_00)
	want(t, "LA line 9C", f.GetFederalStandardDeduction(), 15_750_00)
	want(t, "LA line 9D", f.GetExcessFederalItemizedDeductions(), 9_760_00)
	want(t, "LA line 10", f.GetTaxableIncome(), 37_740_00)
	want(t, "LA line 11", f.GetIncomeTax(), 1_132_00)
	want(t, "LA line 38", f.GetRefund(), 368_00)
	complete(t, "LA", la.GetStatus(), la.GetDiagnostics())
}
