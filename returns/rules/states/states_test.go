package states_test

import (
	"fmt"
	"testing"

	"github.com/nexteggapp/taxcalcs/californiataxcalc"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/oregontaxcalc"
	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	form1040 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1040"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
)

func cents(c int64) *commonv1.Money { return &commonv1.Money{Currency: "USD", Minor: c} }

func boolPtr(b bool) *bool { return &b }

func int32Ptr(i int32) *int32 { return &i }

// A household with one W-2, of the given wages, filing as the given status,
// asking for the given state returns with the standard deduction.
func inputs(status returnsv1.FilingStatusElection, wages int64, withheld int64) *returnsv1.ReturnInputs {
	person := func(id string) *returnsv1.Person {
		return &returnsv1.Person{
			Id:          id,
			FirstName:   id,
			DateOfBirth: &commonv1.Date{Year: 1980, Month: 6, Day: 1},
			Tin: &returnsv1.TaxpayerID{
				Kind:               returnsv1.TaxpayerIDKind_TAXPAYER_ID_KIND_SSN,
				ValidForEmployment: boolPtr(true),
			},
		}
	}
	hh := &returnsv1.Household{
		MaritalStatus: returnsv1.MaritalStatus_MARITAL_STATUS_NEVER_MARRIED,
		Taxpayer:      person("ana"),
	}
	if status == returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY ||
		status == returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_SEPARATELY {
		hh.MaritalStatus = returnsv1.MaritalStatus_MARITAL_STATUS_MARRIED
		hh.Spouse = person("ben")
	}
	w2 := &formw2.FormW2{
		WagesTipsOtherCompensation: cents(wages),
		FederalTaxWithheld:         cents(wages / 10),
	}
	for _, s := range []string{"CA", "OR"} {
		w2.StateTaxWithholding = append(w2.StateTaxWithholding,
			&commonv1.StateTaxWithholding{State: s, StateIncome: cents(wages), TaxWithheld: cents(withheld)})
	}
	return &returnsv1.ReturnInputs{
		SchemaVersion: 1,
		TaxYear:       2025,
		Federal: &returnsv1.Federal1040Inputs{
			Household: hh,
			Elections: &returnsv1.Elections{FilingStatus: status},
			Documents: &returnsv1.Documents{
				W2S: []*returnsv1.W2Document{{Id: "w2", Person: "ana", Form: w2}},
			},
		},
		States: []*returnsv1.StateInputs{
			{State: "CA", Inputs: &returnsv1.StateInputs_Ca{Ca: &returnsv1.CaliforniaInputs{
				DeductionMethod:        returnsv1.DeductionMethodElection_DEDUCTION_METHOD_ELECTION_STANDARD,
				FullYearHealthCoverage: boolPtr(true),
			}}},
			{State: "OR", Inputs: &returnsv1.StateInputs_Or{Or: &returnsv1.OregonInputs{
				DeductionMethod: returnsv1.DeductionMethodElection_DEDUCTION_METHOD_ELECTION_STANDARD,
			}}},
			{State: "NY"},
		},
	}
}

var statuses = []struct {
	election returnsv1.FilingStatusElection
	status   filing.Status
	personal int
}{
	{returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, filing.Single, 1},
	{returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY, filing.MarriedFilingJointly, 2},
	{returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_SEPARATELY, filing.MarriedFilingSeparately, 1},
	{returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_HEAD_OF_HOUSEHOLD, filing.HeadOfHousehold, 1},
}

// Wages chosen to land in every bracket, across the exemption phase-outs and
// above the behavioral health threshold, with odd cents to exercise rounding.
var wages = []int64{0, 9_876_54, 31_415_92, 48_321_37, 77_777_77, 131_072_00, 262_144_33,
	400_000_01, 555_555_55, 1_234_567_89, 2_000_000_00}

func want(t *testing.T, label string, got *commonv1.Money, want money.USCents) {
	t.Helper()
	if money.USCents(got.GetMinor()) != want {
		t.Errorf("%s = %s, want %s", label, money.USCents(got.GetMinor()), want)
	}
}

func check[T any](t *testing.T, r interface{ Value() T }, err error) T {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	return r.Value()
}

// The taxlisp California and Oregon returns compute what californiataxcalc
// and oregontaxcalc compute, to the cent, for every filing status across the
// brackets.
func TestStateReturnsMatchTheGoCalculators(t *testing.T) {
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatal(err)
	}
	const withheld = 1_234_56
	for _, s := range statuses {
		for _, w := range wages {
			t.Run(fmt.Sprintf("%s/%s", s.status, money.USCents(w)), func(t *testing.T) {
				out, err := eng.Prepare(inputs(s.election, w, withheld))
				if err != nil {
					t.Fatal(err)
				}
				if got := len(out.GetStates()); got != 3 {
					t.Fatalf("%d state returns, want 3", got)
				}
				// The federal return rounds to whole dollars; both states start
				// from the AGI it reports.
				agi := money.USCents(out.GetFederal().GetSummary().GetAdjustedGrossIncome().GetMinor())
				checkCalifornia(t, out.GetStates()[0], s.status, s.personal, agi, withheld)
				checkOregon(t, out.GetStates()[1], out.GetFederal().GetForms().GetForm_1040(), s.status, agi, withheld)
				if out.GetStates()[2].GetUnsupported() == nil {
					t.Errorf("NY: %v, want unsupported", out.GetStates()[2])
				}
				var stateTax int64
				for _, st := range out.GetStates()[:2] {
					stateTax += st.GetCa().GetSummary().GetTotalTax().GetMinor() + st.GetOr().GetSummary().GetTotalTax().GetMinor()
				}
				fedTax := out.GetFederal().GetSummary().GetTotalTax().GetMinor()
				want(t, "combined total tax", out.GetSummary().GetCombinedTotalTax(), money.USCents(fedTax+stateTax))
			})
		}
	}
}

func checkCalifornia(t *testing.T, sr *returnsv1.StateReturn, status filing.Status, personal int, agi, withheld money.USCents) {
	t.Helper()
	ret := sr.GetCa()
	if sr.GetState() != "CA" || ret == nil {
		t.Fatalf("first state return %v, want California", sr)
	}
	f := ret.GetForms().GetForm_540()
	adj := californiataxcalc.Adjustments{Election: californiataxcalc.ElectStandard, Residency: californiataxcalc.FullYearResident}
	income, err := californiataxcalc.TaxableIncome(2025, status, agi, adj)
	inc := check[californiataxcalc.Income](t, income, err)
	ladder, err := californiataxcalc.OrdinaryIncomeTax(2025, status, inc.Amount)
	tax := check[californiataxcalc.Ladder](t, ladder, err).Total
	credit, err := californiataxcalc.ExemptionCredit(2025, status, agi, californiataxcalc.Exemptions{Personal: personal})
	exemptions := check[money.USCents](t, credit, err)
	surcharge, err := californiataxcalc.BehavioralHealthServicesTax(2025, status, inc.Amount)
	bhs := check[money.USCents](t, surcharge, err)

	want(t, "CA AGI", f.GetCaliforniaAdjustedGrossIncome(), inc.CaliforniaAGI)
	want(t, "CA deduction", f.GetDeduction(), inc.Deduction)
	want(t, "CA taxable income", f.GetTaxableIncome(), inc.Amount)
	want(t, "CA tax", f.GetTaxBeforeExemptionCredits(), tax)
	want(t, "CA exemption credits", f.GetExemptionCredits(), exemptions)
	want(t, "CA behavioral health tax", f.GetBehavioralHealthServicesTax(), bhs)
	total := max(tax-exemptions, 0) + bhs
	want(t, "CA total tax", f.GetTotalTax(), total)
	want(t, "CA withheld", f.GetCaliforniaTaxWithheld(), withheld)
	want(t, "CA refund", f.GetRefund(), max(withheld-total, 0))
	want(t, "CA amount owed", f.GetAmountYouOwe(), max(total-withheld, 0))
	if ret.GetStatus() != returnsv1.ReturnStatus_RETURN_STATUS_COMPLETE {
		t.Errorf("CA status %s, diagnostics %v", ret.GetStatus(), ret.GetDiagnostics())
	}
}

func checkOregon(t *testing.T, sr *returnsv1.StateReturn, f1040 *form1040.Form1040, status filing.Status, agi, withheld money.USCents) {
	t.Helper()
	ret := sr.GetOr()
	if sr.GetState() != "OR" || ret == nil {
		t.Fatalf("second state return %v, want Oregon", sr)
	}
	f := ret.GetForms().GetFormOr40()
	// The Federal tax liability subtraction worksheet: line 22 less the
	// refundable American opportunity and adoption credits. Wages alone
	// bring no premium tax credit or Schedule 2 taxes.
	liability := money.USCents(f1040.GetTaxAfterCredits().GetMinor() - f1040.GetAmericanOpportunityCredit().GetMinor() -
		f1040.GetRefundableAdoptionCredit().GetMinor())
	sub, err := oregontaxcalc.FederalTaxSubtraction(2025, status, liability, agi)
	subtraction := check[money.USCents](t, sub, err)
	adj := oregontaxcalc.Adjustments{FederalTaxSubtraction: subtraction, Election: oregontaxcalc.ElectStandard,
		Residency: oregontaxcalc.FullYearResident}
	income, err := oregontaxcalc.TaxableIncome(2025, status, agi, oregontaxcalc.Conditions{}, adj)
	inc := check[oregontaxcalc.Income](t, income, err)
	ladder, err := oregontaxcalc.OrdinaryIncomeTax(2025, status, inc.Amount)
	tax := check[oregontaxcalc.Ladder](t, ladder, err).Total
	exemptions := 1
	if status == filing.MarriedFilingJointly {
		exemptions = 2
	}
	credit, err := oregontaxcalc.ExemptionCredit(2025, status, agi, exemptions)
	exemption := check[money.USCents](t, credit, err)

	want(t, "OR federal tax subtraction", f.GetFederalTaxLiability(), subtraction)
	want(t, "OR deduction", f.GetDeduction(), inc.Deduction.Amount)
	want(t, "OR taxable income", f.GetTaxableIncome(), inc.Amount)
	want(t, "OR tax", f.GetTax(), tax)
	want(t, "OR exemption credit", f.GetExemptionCredit(), exemption)
	total := max(tax-exemption, 0)
	want(t, "OR tax after credits", f.GetTaxAfterAllCredits(), total)
	want(t, "OR withheld", f.GetOregonTaxWithheld(), withheld)
	refundable := money.USCents(f.GetEarnedIncomeCredit().GetMinor() + f.GetOregonKidsCredit().GetMinor())
	want(t, "OR refund", f.GetNetRefund(), max(withheld+refundable-total, 0))
	want(t, "OR net tax", f.GetNetTax(), max(total-(withheld+refundable), 0))
	if ret.GetStatus() != returnsv1.ReturnStatus_RETURN_STATUS_COMPLETE {
		t.Errorf("OR status %s, diagnostics %v", ret.GetStatus(), ret.GetDiagnostics())
	}
}

func prepare(t *testing.T, in *returnsv1.ReturnInputs) *returnsv1.ReturnOutputs {
	t.Helper()
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatal(err)
	}
	out, err := eng.Prepare(in)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func complete(t *testing.T, label string, status returnsv1.ReturnStatus, diags []*returnsv1.Diagnostic) {
	t.Helper()
	if status != returnsv1.ReturnStatus_RETURN_STATUS_COMPLETE {
		t.Errorf("%s status %s, diagnostics %v", label, status, diags)
	}
}

// A single renter in California without health coverage all year: the
// renter's credit, and the shared responsibility penalty at its flat amount,
// worked by hand from the 2025 Form 540 booklet and Form FTB 3853.
func TestCaliforniaRentersCreditAndSharedResponsibilityPenalty(t *testing.T) {
	in := inputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, 40_000_00, 1_234_56)
	ca := in.GetStates()[0].GetCa()
	ca.QualifiedForRentersCredit = boolPtr(true)
	ca.FullYearHealthCoverage = boolPtr(false)
	ret := prepare(t, in).GetStates()[0].GetCa()
	f := ret.GetForms().GetForm_540()
	// 34,294 taxable: 110.79 + 303.70 + 321.20 = 735.69, less the 153
	// exemption credit and the 60 renter's credit.
	want(t, "tax before credits", f.GetTaxBeforeCredits(), 582_69)
	want(t, "renter's credit", f.GetRentersCredit(), 60_00)
	want(t, "total tax", f.GetTotalTax(), 522_69)
	// The flat 950 is more than 2.5% of (40,000 - 18,353), 541.
	want(t, "payments after penalty", f.GetPaymentsAfterSharedResponsibilityPenalty(), 284_56)
	want(t, "tax due", f.GetTaxDue(), 238_13)
	want(t, "amount owed", f.GetAmountYouOwe(), 238_13)
	if ret.GetStatus() != returnsv1.ReturnStatus_RETURN_STATUS_COMPLETE || len(ret.GetDiagnostics()) != 1 {
		t.Errorf("status %s, diagnostics %v, want complete with the full-year note", ret.GetStatus(), ret.GetDiagnostics())
	}

	// Above the AGI limit there is no credit, and 2.5% of the excess over the
	// threshold, 2,041, is more than the flat amount.
	in = inputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, 100_000_00, 0)
	ca = in.GetStates()[0].GetCa()
	ca.QualifiedForRentersCredit = boolPtr(true)
	ca.FullYearHealthCoverage = boolPtr(false)
	f = prepare(t, in).GetStates()[0].GetCa().GetForms().GetForm_540()
	want(t, "renter's credit over the limit", f.GetRentersCredit(), 0)
	want(t, "penalty balance", f.GetSharedResponsibilityPenaltyBalance(), 2_041_00)
}

// A filer someone else can claim: the dependent standard deduction in both
// states, no personal exemption, no exemption credit and no penalty.
func TestDependentFiler(t *testing.T) {
	in := inputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, 1_000_00, 0)
	in.GetFederal().GetHousehold().GetTaxpayer().CanBeClaimedAsDependent = boolPtr(true)
	in.GetStates()[0].GetCa().FullYearHealthCoverage = boolPtr(false)
	out := prepare(t, in)
	ca := out.GetStates()[0].GetCa()
	f := ca.GetForms().GetForm_540()
	want(t, "CA deduction", f.GetDeduction(), 1_450_00)
	if f.GetPersonalExemptions() != 0 || !f.GetClaimedAsDependent() {
		t.Errorf("CA personal exemptions %d, claimed %v; want 0, true", f.GetPersonalExemptions(), f.GetClaimedAsDependent())
	}
	want(t, "CA exemption credits", f.GetExemptionCredits(), 0)
	want(t, "CA penalty", f.GetSharedResponsibilityPenaltyBalance(), 0)
	complete(t, "CA", ca.GetStatus(), ca.GetDiagnostics())
	or := out.GetStates()[1].GetOr()
	g := or.GetForms().GetFormOr40()
	want(t, "OR standard deduction", g.GetStandardDeduction(), 1_450_00)
	if g.GetExemptionsSelf() != 0 {
		t.Errorf("OR exemptions self %d, want 0", g.GetExemptionsSelf())
	}
	complete(t, "OR", or.GetStatus(), or.GetDiagnostics())

	// Earned income plus 450 is capped at the ordinary deduction.
	in = inputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, 20_000_00, 0)
	in.GetFederal().GetHousehold().GetTaxpayer().CanBeClaimedAsDependent = boolPtr(true)
	out = prepare(t, in)
	want(t, "CA capped deduction", out.GetStates()[0].GetCa().GetForms().GetForm_540().GetDeduction(), 5_706_00)
	want(t, "OR capped deduction", out.GetStates()[1].GetOr().GetForms().GetFormOr40().GetStandardDeduction(), 2_835_00)
}

// Oregon's political contribution credit, the savings account credit and the
// kicker, worked by hand from the 2025 Form OR-40 instructions and
// Publication OR-17.
func TestOregonCreditsAndKicker(t *testing.T) {
	in := inputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, 40_000_00, 0)
	or := in.GetStates()[1].GetOr()
	or.PoliticalContributions = cents(80_00)
	or.Oregon_529OrAbleContributions = cents(1_000_00)
	or.PriorEvenYearOregonTaxLiability = cents(5_000_00)
	ret := prepare(t, in).GetStates()[1].GetOr()
	f := ret.GetForms().GetFormOr40()
	want(t, "political credit", f.GetPoliticalContributionCredit(), 50_00)
	// 50% of 1,000 at an AGI of 40,000, capped at 180.
	want(t, "savings credit", f.GetOtherRefundableCredits(), 180_00)
	// 9.863% of 5,000 is 493.15.
	want(t, "kicker", f.GetKicker(), 493_00)
	want(t, "payments and refundable credits", f.GetTotalPaymentsAndRefundableCredits(), 673_00)
	complete(t, "OR", ret.GetStatus(), ret.GetDiagnostics())

	// Above the AGI ceiling there is no political credit, and 10% of the
	// contributions is under the cap.
	in = inputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, 120_000_00, 0)
	or = in.GetStates()[1].GetOr()
	or.PoliticalContributions = cents(80_00)
	or.Oregon_529OrAbleContributions = cents(1_000_00)
	f = prepare(t, in).GetStates()[1].GetOr().GetForms().GetFormOr40()
	want(t, "political credit over the ceiling", f.GetPoliticalContributionCredit(), 0)
	want(t, "savings credit at 10%", f.GetOtherRefundableCredits(), 100_00)
}
