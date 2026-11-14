package states_test

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
	formmi1040 "github.com/nexteggapp/taxprotos/go/us/state/mi/v1/form_mi1040"
)

// There is no Michigan income tax calculator in Go to compare against, so the
// figures below are worked by hand from the 2025 Form MI-1040 and its
// instruction book (MI-1040 Book), which are catalogued under
// sources/catalog/mi/2025. The book says to report whole dollars, rounding
// 50 cents and over up (Book p. 5, "Rounding numbers").

func miW2(wages, fedWithheld, miWithheld int64) *formw2.FormW2 {
	return &formw2.FormW2{
		WagesTipsOtherCompensation: cents(wages),
		FederalTaxWithheld:         cents(fedWithheld),
		StateTaxWithholding: []*commonv1.StateTaxWithholding{
			{State: "MI", StateIncome: cents(wages), TaxWithheld: cents(miWithheld)},
		},
	}
}

// miPrepare prepares the return the inputs describe, asking only for
// Michigan's, and returns it.
func miPrepare(t *testing.T, in *returnsv1.ReturnInputs, miIn *returnsv1.MichiganInputs) *returnsv1.MichiganReturn {
	t.Helper()
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatal(err)
	}
	in.States = []*returnsv1.StateInputs{
		{State: "MI", Inputs: &returnsv1.StateInputs_Mi{Mi: miIn}},
	}
	out, err := eng.Prepare(in)
	if err != nil {
		t.Fatal(err)
	}
	ret := out.GetStates()[0].GetMi()
	if ret == nil {
		t.Fatal("expected a Michigan return")
	}
	return ret
}

// 1. Single filer with wages.
//
//   - Line 7: single.
//   - Line 9a: 1 exemption x $5,800 = $5,800 (form line 9a; Book p. 10, Line 9a).
//   - Line 9f: $5,800.
//   - Line 10: federal AGI = wages $51,000 (Book p. 10, Line 10).
//   - Lines 11, 13: no Schedule 1 items, $0.
//   - Line 12: $51,000. Line 14: $51,000.
//   - Line 15: $5,800 from line 9f.
//   - Line 16: $51,000 - $5,800 = $45,200.
//   - Line 17: $45,200 x 4.25% = $1,921.00 (Book p. 10, Line 17).
//   - Line 21: no credits, $1,921. Line 25: $1,921.
//   - Line 31: Michigan withholding $2,000 (Book p. 12, Line 31).
//   - Line 34: $2,000.
//   - Line 37: $2,000 - $1,921 = $79. Line 39 refund: $79. Line 36: $0.
func TestMichiganSingle(t *testing.T) {
	in := ilInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		ilPerson("mia", 1990, 4, 2, false, false), nil, nil,
		[]*formw2.FormW2{miW2(51000_00, 5000_00, 2000_00)},
		0,
	)
	ret := miPrepare(t, in, &returnsv1.MichiganInputs{})
	f := ret.GetForms().GetFormMi1040()

	if f.GetFilingStatus() != formmi1040.FilingStatus_FILING_STATUS_SINGLE {
		t.Errorf("filing status = %v, want single", f.GetFilingStatus())
	}
	if f.GetExemptions() != 1 {
		t.Errorf("line 9a exemptions = %d, want 1", f.GetExemptions())
	}
	want(t, "Line 9a", f.GetPersonalExemption(), money.USCents(5800_00))
	want(t, "Line 9f", f.GetTotalExemptions(), money.USCents(5800_00))
	want(t, "Line 10", f.GetFederalAdjustedGrossIncome(), money.USCents(51000_00))
	want(t, "Line 14", f.GetIncomeSubjectToTax(), money.USCents(51000_00))
	want(t, "Line 15", f.GetExemptionAllowance(), money.USCents(5800_00))
	want(t, "Line 16", f.GetTaxableIncome(), money.USCents(45200_00))
	want(t, "Line 17", f.GetTax(), money.USCents(1921_00))
	want(t, "Line 25", f.GetTotalTaxLiability(), money.USCents(1921_00))
	want(t, "Line 31", f.GetMichiganTaxWithheld(), money.USCents(2000_00))
	want(t, "Line 34", f.GetTotalRefundableCreditsAndPayments(), money.USCents(2000_00))
	want(t, "Line 36", f.GetAmountOwed(), money.USCents(0))
	want(t, "Line 39", f.GetRefund(), money.USCents(79_00))

	want(t, "summary tax", ret.GetSummary().GetTotalTax(), money.USCents(1921_00))
	want(t, "summary refund", ret.GetSummary().GetRefund(), money.USCents(79_00))
	if ret.GetStatus() != returnsv1.ReturnStatus_RETURN_STATUS_COMPLETE {
		t.Errorf("status = %v, want complete: %v", ret.GetStatus(), ret.GetDiagnostics())
	}
}

// 2. Married filing jointly, one earner, two children, the federal earned
// income credit and a homestead property tax credit.
//
//   - Line 7: married filing jointly.
//   - Line 9a: 4 exemptions (filer, spouse, two dependents) x $5,800 =
//     $23,200 (Book p. 10, Line 9a: dependents count).
//   - Line 10: federal AGI = wages $25,000.
//   - Line 14: $25,000. Line 15: $23,200.
//   - Line 16: $25,000 - $23,200 = $1,800.
//   - Line 17: $1,800 x 4.25% = $76.50, rounded up to $77 (Book p. 5).
//   - Line 25: $77.
//   - Line 26: property tax credit from MI-1040CR, given as $600.
//   - Line 28a: federal EITC. Two qualifying children, earned income $25,000
//     sits on the plateau between the earned income amount ($17,880) and the
//     joint phase-out threshold, so the credit is the 2025 maximum, $7,152
//     (Rev. Proc. 2024-40, section 3.07).
//   - Line 28b: $7,152 x 30% = $2,145.60, rounded to $2,146 (Book p. 11,
//     Line 28).
//   - Line 31: withholding $300.
//   - Line 34: $600 + $2,146 + $300 = $3,046.
//   - Line 37: $3,046 - $77 = $2,969. Line 39 refund: $2,969.
func TestMichiganMarriedFilingJointlyWithChildren(t *testing.T) {
	in := ilInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY,
		ilPerson("ada", 1988, 1, 9, false, false),
		ilPerson("ben", 1987, 7, 30, false, false),
		[]*returnsv1.Dependent{
			ilDependent("cal", 2016, 3, 3),
			ilDependent("dee", 2019, 11, 21),
		},
		[]*formw2.FormW2{miW2(25000_00, 0, 300_00)},
		0,
	)
	ret := miPrepare(t, in, &returnsv1.MichiganInputs{PropertyTaxCredit: cents(600_00)})
	f := ret.GetForms().GetFormMi1040()

	if f.GetFilingStatus() != formmi1040.FilingStatus_FILING_STATUS_MARRIED_FILING_JOINTLY {
		t.Errorf("filing status = %v, want married filing jointly", f.GetFilingStatus())
	}
	if f.GetExemptions() != 4 {
		t.Errorf("line 9a exemptions = %d, want 4", f.GetExemptions())
	}
	want(t, "Line 9a", f.GetPersonalExemption(), money.USCents(23200_00))
	want(t, "Line 10", f.GetFederalAdjustedGrossIncome(), money.USCents(25000_00))
	want(t, "Line 16", f.GetTaxableIncome(), money.USCents(1800_00))
	want(t, "Line 17", f.GetTax(), money.USCents(77_00))
	want(t, "Line 25", f.GetTotalTaxLiability(), money.USCents(77_00))
	want(t, "Line 26", f.GetPropertyTaxCredit(), money.USCents(600_00))
	want(t, "Line 28a", f.GetFederalEarnedIncomeCredit(), money.USCents(7152_00))
	want(t, "Line 28b", f.GetEarnedIncomeCredit(), money.USCents(2146_00))
	want(t, "Line 31", f.GetMichiganTaxWithheld(), money.USCents(300_00))
	want(t, "Line 34", f.GetTotalRefundableCreditsAndPayments(), money.USCents(3046_00))
	want(t, "Line 36", f.GetAmountOwed(), money.USCents(0))
	want(t, "Line 39", f.GetRefund(), money.USCents(2969_00))
}

// 3. High-income single filer, a qualified disabled veteran, with Schedule 1
// additions and subtractions, who owes.
//
//   - Line 9a: 1 x $5,800 = $5,800.
//   - Line 9c: 1 qualified disabled veteran x $500 = $500 (Book p. 10,
//     Line 9c).
//   - Line 9f: $6,300.
//   - Line 10: federal AGI = wages $250,000.
//   - Line 11: Schedule 1 additions, given as $1,000 (e.g. interest on
//     another state's bonds, Schedule 1 line 1).
//   - Line 12: $251,000.
//   - Line 13: Schedule 1 subtractions, given as $2,000 (e.g. U.S. bond
//     interest, Schedule 1 line 10).
//   - Line 14: $249,000. Line 15: $6,300.
//   - Line 16: $249,000 - $6,300 = $242,700.
//   - Line 17: $242,700 x 4.25% = $10,314.75, rounded to $10,315.
//   - Line 25: $10,315.
//   - Line 31: withholding $9,000. Line 32: estimated payments $1,000.
//   - Line 34: $10,000.
//   - Line 36: $10,315 - $10,000 = $315 owed. Line 39: $0.
func TestMichiganHighIncomeOwes(t *testing.T) {
	in := ilInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		ilPerson("vic", 1970, 2, 14, false, false), nil, nil,
		[]*formw2.FormW2{miW2(250000_00, 50000_00, 9000_00)},
		0,
	)
	ret := miPrepare(t, in, &returnsv1.MichiganInputs{
		OtherAdditions:            cents(1000_00),
		OtherSubtractions:         cents(2000_00),
		EstimatedTaxPayments:      cents(1000_00),
		QualifiedDisabledVeterans: 1,
	})
	f := ret.GetForms().GetFormMi1040()

	want(t, "Line 9a", f.GetPersonalExemption(), money.USCents(5800_00))
	want(t, "Line 9c", f.GetDisabledVeteranExemption(), money.USCents(500_00))
	want(t, "Line 9f", f.GetTotalExemptions(), money.USCents(6300_00))
	want(t, "Line 10", f.GetFederalAdjustedGrossIncome(), money.USCents(250000_00))
	want(t, "Line 11", f.GetAdditions(), money.USCents(1000_00))
	want(t, "Line 12", f.GetTotalIncome(), money.USCents(251000_00))
	want(t, "Line 13", f.GetSubtractions(), money.USCents(2000_00))
	want(t, "Line 14", f.GetIncomeSubjectToTax(), money.USCents(249000_00))
	want(t, "Line 16", f.GetTaxableIncome(), money.USCents(242700_00))
	want(t, "Line 17", f.GetTax(), money.USCents(10315_00))
	want(t, "Line 25", f.GetTotalTaxLiability(), money.USCents(10315_00))
	want(t, "Line 32", f.GetEstimatedTaxPayments(), money.USCents(1000_00))
	want(t, "Line 34", f.GetTotalRefundableCreditsAndPayments(), money.USCents(10000_00))
	want(t, "Line 36", f.GetAmountOwed(), money.USCents(315_00))
	want(t, "Line 39", f.GetRefund(), money.USCents(0))
	want(t, "summary owed", ret.GetSummary().GetAmountOwed(), money.USCents(315_00))
}

// 4. A filer someone else can claim as a dependent takes $1,500 on line 9e in
// place of line 9a (form line 9 NOTE; Book p. 10, Line 9e).
//
//   - Line 9a: 0. Line 9e: $1,500. Line 9f: $1,500.
//   - Line 10: wages $8,000. Line 16: $8,000 - $1,500 = $6,500.
//   - Line 17: $6,500 x 4.25% = $276.25, rounded to $276.
//   - Line 31: withholding $300. Line 39 refund: $24.
func TestMichiganClaimedAsDependent(t *testing.T) {
	in := ilInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		ilPerson("kit", 2006, 5, 5, false, true), nil, nil,
		[]*formw2.FormW2{miW2(8000_00, 0, 300_00)},
		0,
	)
	ret := miPrepare(t, in, &returnsv1.MichiganInputs{})
	f := ret.GetForms().GetFormMi1040()

	if !f.GetClaimedAsDependent() {
		t.Error("line 9e box not checked")
	}
	want(t, "Line 9a", f.GetPersonalExemption(), money.USCents(0))
	want(t, "Line 9e", f.GetDependentFilerExemption(), money.USCents(1500_00))
	want(t, "Line 16", f.GetTaxableIncome(), money.USCents(6500_00))
	want(t, "Line 17", f.GetTax(), money.USCents(276_00))
	want(t, "Line 39", f.GetRefund(), money.USCents(24_00))
}
