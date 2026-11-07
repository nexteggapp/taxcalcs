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
	liability := money.USCents(f1040.GetTaxAfterCredits().GetMinor() - f1040.GetEarnedIncomeCredit().GetMinor() -
		f1040.GetAdditionalChildTaxCredit().GetMinor() - f1040.GetAmericanOpportunityCredit().GetMinor())
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
	want(t, "OR refund", f.GetNetRefund(), max(withheld-total, 0))
	want(t, "OR net tax", f.GetNetTax(), max(total-withheld, 0))
	if ret.GetStatus() != returnsv1.ReturnStatus_RETURN_STATUS_COMPLETE {
		t.Errorf("OR status %s, diagnostics %v", ret.GetStatus(), ret.GetDiagnostics())
	}
}
