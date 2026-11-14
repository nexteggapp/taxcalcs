package states_test

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
	formpitres "github.com/nexteggapp/taxprotos/go/us/state/de/v1/form_pit_res"
)

// Delaware has no Go income tax calculator to test against, so these figures
// are worked by hand from the 2025 Form PIT-RES and its instructions. The
// rate schedule is 30 Del. C. § 1102(a)(14):
//
//	0% to $2,000; 2.2% to $5,000; 3.9% to $10,000; 4.8% to $20,000;
//	5.2% to $25,000; 5.55% to $60,000; 6.6% above.
//
// The tax on the first $60,000 is therefore $0 + $66 + $195 + $480 + $260 +
// $1,942.50 = $2,943.50. Every household has taxable income of at least
// $60,000, where the instructions for line 24 send the filer to the
// schedule rather than the tax table.

func deInputs(
	status returnsv1.FilingStatusElection,
	taxpayer *returnsv1.Person,
	spouse *returnsv1.Person,
	dependents []*returnsv1.Dependent,
	w2s []*formw2.FormW2,
) *returnsv1.ReturnInputs {
	marital := returnsv1.MaritalStatus_MARITAL_STATUS_NEVER_MARRIED
	if spouse != nil {
		marital = returnsv1.MaritalStatus_MARITAL_STATUS_MARRIED
	}
	var w2Docs []*returnsv1.W2Document
	for i, w := range w2s {
		personID := taxpayer.Id
		if i == 1 && spouse != nil {
			personID = spouse.Id
		}
		w2Docs = append(w2Docs, &returnsv1.W2Document{Id: w2Id(i), Person: personID, Form: w})
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
			Documents: &returnsv1.Documents{W2S: w2Docs},
		},
		States: []*returnsv1.StateInputs{{
			State:  "DE",
			Inputs: &returnsv1.StateInputs_De{De: &returnsv1.DelawareInputs{}},
		}},
	}
}

func deW2(wages, fedWithheld, deWithheld int64) *formw2.FormW2 {
	return &formw2.FormW2{
		WagesTipsOtherCompensation: cents(wages),
		FederalTaxWithheld:         cents(fedWithheld),
		StateTaxWithholding: []*commonv1.StateTaxWithholding{
			{State: "DE", StateIncome: cents(wages), TaxWithheld: cents(deWithheld)},
		},
	}
}

func prepareDE(t *testing.T, in *returnsv1.ReturnInputs) (*returnsv1.DelawareReturn, *formpitres.FormPITRES) {
	t.Helper()
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatal(err)
	}
	out, err := eng.Prepare(in)
	if err != nil {
		t.Fatal(err)
	}
	ret := out.GetStates()[0].GetDe()
	if ret == nil {
		t.Fatalf("expected a Delaware return, got %v", out.GetStates()[0])
	}
	if ret.GetStatus() != returnsv1.ReturnStatus_RETURN_STATUS_COMPLETE {
		t.Errorf("expected a complete return, got %v: %v", ret.GetStatus(), ret.GetDiagnostics())
	}
	return ret, ret.GetForms().GetFormPitRes()
}

// A single filer, 35, with $80,000 of wages and $3,500 of Delaware tax
// withheld.
//
//	Line 1   federal AGI                                    $80,000
//	Line 4   no additions                                   $80,000
//	Line 12  no subtractions: Delaware AGI                  $80,000
//	Line 20  standard deduction, filing status 1 (instr. Line 20a)  $3,250
//	Line 22  total deductions                                $3,250
//	Line 23  taxable income                                 $76,750
//	Line 24  $2,943.50 + 6.6% x $16,750 ($1,105.50)          $4,049
//	Line 27a one exemption x $110 (instr. Line 27a)            $110
//	Line 32  total non-refundable credits                      $110
//	Line 33  balance: $4,049 - $110                          $3,939
//	Line 35  Delaware tax withheld                           $3,500
//	Line 41  balance due: $3,939 - $3,500                      $439
//	Line 46  net balance due                                   $439
func TestDelawareSingle(t *testing.T) {
	in := deInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		ilPerson("dana", 1990, 4, 2, false, false), nil, nil,
		[]*formw2.FormW2{deW2(80000_00, 9000_00, 3500_00)},
	)
	ret, f := prepareDE(t, in)

	if f.GetFilingStatus() != formpitres.FilingStatus_FILING_STATUS_SINGLE {
		t.Errorf("filing status = %v, want single", f.GetFilingStatus())
	}
	want(t, "Line 1 federal AGI", f.GetFederalAdjustedGrossIncome(), money.USCents(80000_00))
	want(t, "Line 12 Delaware AGI", f.GetDelawareAdjustedGrossIncome(), money.USCents(80000_00))
	want(t, "Line 20 standard deduction", f.GetDeduction(), money.USCents(3250_00))
	want(t, "Line 22 total deductions", f.GetTotalDeductions(), money.USCents(3250_00))
	want(t, "Line 23 taxable income", f.GetTaxableIncome(), money.USCents(76750_00))
	want(t, "Line 24 tax", f.GetTax(), money.USCents(4049_00))
	want(t, "Line 27a personal credits", f.GetPersonalCredits(), money.USCents(110_00))
	want(t, "Line 32 non-refundable credits", f.GetTotalNonrefundableCredits(), money.USCents(110_00))
	want(t, "Line 33 balance", f.GetTaxAfterCredits(), money.USCents(3939_00))
	want(t, "Line 35 withheld", f.GetDelawareTaxWithheld(), money.USCents(3500_00))
	want(t, "Line 41 balance due", f.GetBalanceDue(), money.USCents(439_00))
	want(t, "Line 42 overpayment", f.GetOverpayment(), money.USCents(0))
	want(t, "Line 46 net balance due", f.GetAmountOwed(), money.USCents(439_00))
	want(t, "Line 47 net refund", f.GetRefund(), money.USCents(0))
	want(t, "summary tax", ret.GetSummary().GetTotalTax(), money.USCents(3939_00))
}

// A married couple filing jointly, both under 60, with two children and
// $140,100 of wages, $6,500 of Delaware tax withheld.
//
//	Line 1   federal AGI: $95,100 + $45,000                $140,100
//	Line 12  Delaware AGI                                  $140,100
//	Line 20  standard deduction, filing status 2 (instr. Line 20a)  $6,500
//	Line 23  taxable income                                $133,600
//	Line 24  $2,943.50 + 6.6% x $73,600 ($4,857.60) = $7,801.10    $7,801
//	Line 27a four exemptions (two spouses, two children) x $110    $440
//	Line 31  no federal child care credit                        $0
//	Line 33  balance: $7,801 - $440                          $7,361
//	Line 34  no federal earned income credit at this income      $0
//	Line 35  withheld: $4,000 + $2,500                       $6,500
//	Line 41  balance due: $7,361 - $6,500                      $861
func TestDelawareMarriedFilingJointlyWithChildren(t *testing.T) {
	in := deInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY,
		ilPerson("alex", 1985, 3, 15, false, false),
		ilPerson("blair", 1986, 8, 20, false, false),
		[]*returnsv1.Dependent{ilDependent("casey", 2015, 6, 1), ilDependent("drew", 2018, 9, 9)},
		[]*formw2.FormW2{deW2(95100_00, 9000_00, 4000_00), deW2(45000_00, 3000_00, 2500_00)},
	)
	_, f := prepareDE(t, in)

	if f.GetFilingStatus() != formpitres.FilingStatus_FILING_STATUS_JOINT {
		t.Errorf("filing status = %v, want joint", f.GetFilingStatus())
	}
	want(t, "Line 1 federal AGI", f.GetFederalAdjustedGrossIncome(), money.USCents(140100_00))
	want(t, "Line 20 standard deduction", f.GetDeduction(), money.USCents(6500_00))
	want(t, "Line 23 taxable income", f.GetTaxableIncome(), money.USCents(133600_00))
	want(t, "Line 24 tax", f.GetTax(), money.USCents(7801_00))
	if got := f.GetPersonalCreditExemptions(); got != 4 {
		t.Errorf("Line 27a exemptions = %d, want 4", got)
	}
	want(t, "Line 27a personal credits", f.GetPersonalCredits(), money.USCents(440_00))
	want(t, "Line 31 child care credit", f.GetChildCareCredit(), money.USCents(0))
	want(t, "Line 33 balance", f.GetTaxAfterCredits(), money.USCents(7361_00))
	want(t, "Line 34 earned income credit", f.GetEarnedIncomeCredit(), money.USCents(0))
	want(t, "Line 35 withheld", f.GetDelawareTaxWithheld(), money.USCents(6500_00))
	want(t, "Line 41 balance due", f.GetBalanceDue(), money.USCents(861_00))
	want(t, "Line 46 net balance due", f.GetAmountOwed(), money.USCents(861_00))
	want(t, "Line 47 net refund", f.GetRefund(), money.USCents(0))
}

// A single filer, 62, with $250,000 of wages and $15,000 of Delaware tax
// withheld. Being 60 or over earns the additional personal credit, but not
// the additional standard deduction, which starts at 65.
//
//	Line 12  Delaware AGI                                  $250,000
//	Line 20  standard deduction                              $3,250
//	Line 21  no 65-or-over or blind boxes                        $0
//	Line 23  taxable income                                $246,750
//	Line 24  $2,943.50 + 6.6% x $186,750 ($12,325.50)        $15,269
//	Line 27a one exemption x $110                              $110
//	Line 27b one 60-or-over box x $110 (instr. Line 27b)       $110
//	Line 32  total non-refundable credits                      $220
//	Line 33  balance: $15,269 - $220                        $15,049
//	Line 35  withheld                                       $15,000
//	Line 41  balance due                                        $49
func TestDelawareHighIncomeOver60(t *testing.T) {
	in := deInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		ilPerson("emery", 1963, 2, 10, false, false), nil, nil,
		[]*formw2.FormW2{deW2(250000_00, 50000_00, 15000_00)},
	)
	_, f := prepareDE(t, in)

	want(t, "Line 12 Delaware AGI", f.GetDelawareAdjustedGrossIncome(), money.USCents(250000_00))
	want(t, "Line 21 additional standard deduction", f.GetAdditionalStandardDeduction(), money.USCents(0))
	want(t, "Line 23 taxable income", f.GetTaxableIncome(), money.USCents(246750_00))
	want(t, "Line 24 tax", f.GetTax(), money.USCents(15269_00))
	want(t, "Line 27a personal credits", f.GetPersonalCredits(), money.USCents(110_00))
	want(t, "Line 27b additional personal credits", f.GetAdditionalPersonalCredits(), money.USCents(110_00))
	want(t, "Line 32 non-refundable credits", f.GetTotalNonrefundableCredits(), money.USCents(220_00))
	want(t, "Line 33 balance", f.GetTaxAfterCredits(), money.USCents(15049_00))
	want(t, "Line 41 balance due", f.GetBalanceDue(), money.USCents(49_00))
	want(t, "Line 46 net balance due", f.GetAmountOwed(), money.USCents(49_00))
}
