package states_test

// There is no Go income tax calculator for West Virginia to compare against. These households are worked by
// hand from the 2025 Form IT-140 booklet ("the booklet"), and the working is
// written out line by line beside each test. Page numbers are the booklet's
// printed folios.

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	formssa1099 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_ssa_1099"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
	formit140 "github.com/nexteggapp/taxprotos/go/us/state/wv/v1/form_it140"
)

// wvInputs builds a West Virginia resident household: wages on one W-2 with
// West Virginia withholding, the taxpayer born in birthYear, and Social
// Security benefits when benefits is not zero.
func wvInputs(
	status returnsv1.FilingStatusElection,
	birthYear int32,
	wages, withheld, benefits int64,
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
		Taxpayer:      person("taxpayer", birthYear),
	}
	if status == returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY ||
		status == returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_SEPARATELY {
		hh.MaritalStatus = returnsv1.MaritalStatus_MARITAL_STATUS_MARRIED
		hh.Spouse = person("spouse", birthYear+2)
	}
	docs := &returnsv1.Documents{
		W2S: []*returnsv1.W2Document{{Id: "w2-wv", Person: "taxpayer", Form: &formw2.FormW2{
			WagesTipsOtherCompensation: cents(wages),
			FederalTaxWithheld:         cents(wages / 10),
			StateTaxWithholding: []*commonv1.StateTaxWithholding{
				{State: "WV", StateIncome: cents(wages), TaxWithheld: cents(withheld)},
			},
		}}},
	}
	if benefits > 0 {
		docs.SocialSecurity = []*returnsv1.FormSSA1099Document{{
			Id:     "ssa-wv",
			Person: "taxpayer",
			Form:   &formssa1099.FormSSA1099{BenefitsPaid: cents(benefits), NetBenefits: cents(benefits)},
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
			State:  "WV",
			Inputs: &returnsv1.StateInputs_Wv{Wv: &returnsv1.WestVirginiaInputs{}},
		}},
	}
	for _, opt := range opts {
		opt(in)
	}
	return in
}

func prepareWV(t *testing.T, in *returnsv1.ReturnInputs) (*returnsv1.WestVirginiaReturn, *formit140.FormIT140) {
	t.Helper()
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine: %v", err)
	}
	out, err := eng.Prepare(in)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if len(out.GetStates()) == 0 || out.GetStates()[0].GetWv() == nil {
		t.Fatalf("expected West Virginia return in outputs, got %v", out.GetStates())
	}
	wv := out.GetStates()[0].GetWv()
	f := wv.GetForms().GetFormIt140()
	if f == nil {
		t.Fatalf("expected Form IT-140 in West Virginia return, got %v", wv.GetForms())
	}
	return wv, f
}

// A single filer with $60,000 of wages and $2,500 withheld.
//
//   - Line 1: federal AGI $60,000.
//   - Lines 2, 3: no Schedule M modifications.
//   - Line 4: $60,000.
//   - Line 5: federal AGI is over $10,000, so no exclusion (worksheet, p. 29).
//   - Line 6: one exemption, box (a), times $2,000 = $2,000 (p. 24).
//   - Line 7: $60,000 - $2,000 = $58,000.
//   - Line 8: Tax Table (p. 36), row $58,000 to $58,050. The row prints the
//     Rate Schedule I tax on its midpoint, $58,025: $1,165.50 + 4.44% of
//     $18,025 = $1,965.81, so $1,966.
//   - Lines 10, 14: $1,966.
//   - Lines 15, 23: $2,500 withheld.
//   - Lines 25, 28: refund $2,500 - $1,966 = $534.
func TestWestVirginiaSingleFiler(t *testing.T) {
	wv, f := prepareWV(t, wvInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		1985, 60_000_00, 2_500_00, 0))

	want(t, "WV line 1", f.GetFederalAdjustedGrossIncome(), 60_000_00)
	want(t, "WV line 4", f.GetWestVirginiaAdjustedGrossIncome(), 60_000_00)
	want(t, "WV line 5", f.GetLowIncomeEarnedIncomeExclusion(), 0)
	want(t, "WV line 6", f.GetExemptionAmount(), 2_000_00)
	want(t, "WV line 7", f.GetTaxableIncome(), 58_000_00)
	want(t, "WV line 8", f.GetIncomeTax(), 1_966_00)
	want(t, "WV line 14", f.GetTotalAmountDue(), 1_966_00)
	want(t, "WV line 23", f.GetTotalPaymentsAndRefundableCredits(), 2_500_00)
	want(t, "WV line 24", f.GetBalanceDue(), 0)
	want(t, "WV line 28", f.GetRefund(), 534_00)
	if f.GetTotalExemptions() != 1 {
		t.Errorf("WV box (e) = %d, want 1", f.GetTotalExemptions())
	}
	complete(t, "WV", wv.GetStatus(), wv.GetDiagnostics())
}

// Married filing jointly, two children, $80,000 of wages and $2,000 withheld.
//
//   - Line 1: federal AGI $80,000.
//   - Line 4: $80,000.
//   - Line 5: over $10,000, no exclusion.
//   - Line 6: boxes (a) 1, (b) 1, (c) 2: four exemptions times $2,000 = $8,000.
//   - Line 7: $80,000 - $8,000 = $72,000.
//   - Line 8: Tax Table row $72,000 to $72,050, midpoint $72,025: $2,053.50 +
//     4.82% of $12,025 = $2,633.105, so $2,633.
//   - Line 14: $2,633.
//   - Line 23: $2,000 withheld.
//   - Line 24: balance due $2,633 - $2,000 = $633.
func TestWestVirginiaMarriedFilingJointlyWithChildren(t *testing.T) {
	in := wvInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY,
		1985, 80_000_00, 2_000_00, 0,
		func(i *returnsv1.ReturnInputs) {
			i.Federal.Household.Dependents = []*returnsv1.Dependent{
				azDependent("child1", 2015),
				azDependent("child2", 2018),
			}
		})
	wv, f := prepareWV(t, in)

	if f.GetTotalExemptions() != 4 {
		t.Errorf("WV box (e) = %d, want 4", f.GetTotalExemptions())
	}
	want(t, "WV line 1", f.GetFederalAdjustedGrossIncome(), 80_000_00)
	want(t, "WV line 6", f.GetExemptionAmount(), 8_000_00)
	want(t, "WV line 7", f.GetTaxableIncome(), 72_000_00)
	want(t, "WV line 8", f.GetIncomeTax(), 2_633_00)
	want(t, "WV line 14", f.GetTotalAmountDue(), 2_633_00)
	want(t, "WV line 23", f.GetTotalPaymentsAndRefundableCredits(), 2_000_00)
	want(t, "WV line 24", f.GetBalanceDue(), 633_00)
	want(t, "WV line 28", f.GetRefund(), 0)
	complete(t, "WV", wv.GetStatus(), wv.GetDiagnostics())
}

// A single filer aged 70 with $120,000 of wages, $30,000 of Social Security
// benefits and $6,000 withheld.
//
//   - Federal: provisional income $120,000 + $15,000 is far over $34,000, so
//     85% of benefits, $25,500, is taxable. Line 1: $145,500.
//   - Schedule M line 34: federal AGI is over $50,000, so 65% of the taxable
//     benefits for 2025 (p. 25): $16,575.
//   - Schedule M line 47: the smaller of $8,000 and income, less line 34:
//     $8,000 - $16,575 is below zero, so $0 (p. 26).
//   - Line 3: $16,575. Line 4: $145,500 - $16,575 = $128,925.
//   - Line 6: one exemption, $2,000. Line 7: $126,925.
//   - Line 8: over $100,000, so Rate Schedule I (p. 35): $2,053.50 + 4.82% of
//     $66,925 = $5,279.285, so $5,279.
//   - Line 23: $6,000. Line 28: refund $721.
func TestWestVirginiaHighIncomeRetiree(t *testing.T) {
	wv, f := prepareWV(t, wvInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		1955, 120_000_00, 6_000_00, 30_000_00))

	want(t, "WV line 1", f.GetFederalAdjustedGrossIncome(), 145_500_00)
	want(t, "WV Schedule M line 34", f.GetSocialSecuritySubtraction(), 16_575_00)
	want(t, "WV Schedule M line 47", f.GetSeniorCitizenModification(), 0)
	want(t, "WV line 3", f.GetSubtractions(), 16_575_00)
	want(t, "WV line 4", f.GetWestVirginiaAdjustedGrossIncome(), 128_925_00)
	want(t, "WV line 7", f.GetTaxableIncome(), 126_925_00)
	want(t, "WV line 8", f.GetIncomeTax(), 5_279_00)
	want(t, "WV line 23", f.GetTotalPaymentsAndRefundableCredits(), 6_000_00)
	want(t, "WV line 28", f.GetRefund(), 721_00)
	complete(t, "WV", wv.GetStatus(), wv.GetDiagnostics())
}

// A single filer aged 70 with $30,000 of wages and no benefits.
//
//   - Line 1: $30,000.
//   - Schedule M line 47: the smaller of $8,000 and income, less lines 29 to
//     34 ($0): $8,000. Line 3: $8,000. Line 4: $22,000.
//   - Line 6: $2,000. Line 7: $20,000.
//   - Line 8: Tax Table row $20,000 to $20,100, midpoint $20,050: $222 +
//     2.96% of $10,050 = $519.48, so $519. The printed table's row reads $519.
//   - Line 23: $600 withheld. Line 28: refund $81.
func TestWestVirginiaSeniorModification(t *testing.T) {
	wv, f := prepareWV(t, wvInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		1955, 30_000_00, 600_00, 0))

	want(t, "WV Schedule M line 47", f.GetSeniorCitizenModification(), 8_000_00)
	want(t, "WV line 4", f.GetWestVirginiaAdjustedGrossIncome(), 22_000_00)
	want(t, "WV line 7", f.GetTaxableIncome(), 20_000_00)
	want(t, "WV line 8", f.GetIncomeTax(), 519_00)
	want(t, "WV line 28", f.GetRefund(), 81_00)
	complete(t, "WV", wv.GetStatus(), wv.GetDiagnostics())
}

// Line 8 against figures the booklet prints. Each household is a single
// filer whose wages are taxable income plus the $2,000 exemption.
//
//   - $18,150: Tax Table row $18,100 to $18,200 prints $463 (p. 36).
//   - $12,050: row $12,000 to $12,100 prints $283.
//   - $25,000: row $25,000 to $25,060 prints $667.
//   - $99,999: row $99,950 to $100,000 prints $3,980 (p. 40).
//   - $117,635: Rate Schedule I's worked example, $4,831.51, rounded to $4,832
//     (p. 35).
func TestWestVirginiaPrintedTaxFigures(t *testing.T) {
	for _, c := range []struct {
		taxable int64
		tax     money.USCents
	}{
		{18_150_00, 463_00},
		{12_050_00, 283_00},
		{25_000_00, 667_00},
		{99_999_00, 3_980_00},
		{117_635_00, 4_832_00},
	} {
		_, f := prepareWV(t, wvInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
			1985, c.taxable+2_000_00, 0, 0))
		want(t, "WV line 7", f.GetTaxableIncome(), money.USCents(c.taxable))
		want(t, "WV line 8", f.GetIncomeTax(), c.tax)
	}
}

// A single filer with $7,950 of wages, under the $10,000 limit for the
// Low-Income Earned Income Exclusion Worksheet (p. 29).
//
//   - Line A: federal AGI $7,950. Line B: earned income $7,950. Line C:
//     $10,000. Line D, the smallest: $7,950, which is line 5.
//   - Line 6: $2,000. Line 7: $7,950 - $7,950 - $2,000 is below zero, so $0.
//   - Line 8: $0. Line 28: the $100 withheld is refunded.
func TestWestVirginiaLowIncomeExclusion(t *testing.T) {
	wv, f := prepareWV(t, wvInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		1985, 7_950_00, 100_00, 0))

	want(t, "WV line 5", f.GetLowIncomeEarnedIncomeExclusion(), 7_950_00)
	want(t, "WV line 7", f.GetTaxableIncome(), 0)
	want(t, "WV line 8", f.GetIncomeTax(), 0)
	want(t, "WV line 28", f.GetRefund(), 100_00)
	complete(t, "WV", wv.GetStatus(), wv.GetDiagnostics())
}
