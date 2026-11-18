package states_test

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
	formit1040 "github.com/nexteggapp/taxprotos/go/us/state/oh/v1/form_it1040"
)

// There is no Go income tax calculator for Ohio, so these households are worked by hand from the 2025
// Ohio IT 1040 and its instructions booklet. Page numbers are the booklet's
// printed pages. Two of the households are the booklet's own worked examples.

// ohInputs builds return inputs for an Ohio full-year resident.
func ohInputs(
	status returnsv1.FilingStatusElection,
	taxpayer *returnsv1.Person,
	spouse *returnsv1.Person,
	dependents []*returnsv1.Dependent,
	w2s []*formw2.FormW2,
	ohIn *returnsv1.OhioInputs,
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
		States: []*returnsv1.StateInputs{
			{State: "OH", Inputs: &returnsv1.StateInputs_Oh{Oh: ohIn}},
		},
	}
}

func ohW2(wages, fedWithheld, ohWithheld int64) *formw2.FormW2 {
	return &formw2.FormW2{
		WagesTipsOtherCompensation: cents(wages),
		FederalTaxWithheld:         cents(fedWithheld),
		StateTaxWithholding: []*commonv1.StateTaxWithholding{
			{State: "OH", StateIncome: cents(wages), TaxWithheld: cents(ohWithheld)},
		},
	}
}

func prepareOhio(t *testing.T, in *returnsv1.ReturnInputs) (*returnsv1.OhioReturn, *formit1040.FormIT1040) {
	t.Helper()
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatal(err)
	}
	out, err := eng.Prepare(in)
	if err != nil {
		t.Fatal(err)
	}
	ret := out.GetStates()[0].GetOh()
	if ret == nil {
		t.Fatal("expected Ohio return")
	}
	if ret.GetStatus() != returnsv1.ReturnStatus_RETURN_STATUS_COMPLETE {
		t.Errorf("expected complete return, got %v: %v", ret.GetStatus(), ret.GetDiagnostics())
	}
	return ret, ret.GetForms().GetFormIt1040()
}

// 1. Single filer: the booklet's "Mitchell" bracket example (p. 18), whose
// taxable nonbusiness income is $68,050.
//   - Line 1 federal AGI: wages $70,200.00
//   - Lines 2a, 2b: none; line 3 Ohio AGI $70,200.00
//   - MAGI (p. 8): line 3 plus business income deduction (none) = $70,200
//   - Line 4: one exemption; MAGI $40,001-$80,000 gives $2,150 (p. 17 table)
//   - Line 5: $70,200 - $2,150 = $68,050; line 6 none; line 7 $68,050
//   - Line 8a (p. 18 brackets): $342 + 2.75% x ($68,050 - $26,050)
//     = $342 + $1,155 = $1,497, as the booklet works it
//   - Lines 8c, 10, 13: $1,497 (no credits, penalty or use tax)
//   - Line 14 withheld $1,400.00; line 17 $1,400.00; line 19 $1,400.00
//   - Line 20 / 22 amount due: $1,497 - $1,400 = $97.00
func TestOhioSingle(t *testing.T) {
	in := ohInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		ilPerson("mitchell", 1988, 5, 12, false, false), nil, nil,
		[]*formw2.FormW2{ohW2(70200_00, 7000_00, 1400_00)},
		&returnsv1.OhioInputs{},
	)
	ret, f := prepareOhio(t, in)

	want(t, "Line 1 Federal AGI", f.GetFederalAdjustedGrossIncome(), money.USCents(70200_00))
	want(t, "Line 3 Ohio AGI", f.GetOhioAdjustedGrossIncome(), money.USCents(70200_00))
	if f.GetExemptionCount() != 1 {
		t.Errorf("exemption count: got %d, want 1", f.GetExemptionCount())
	}
	want(t, "Line 4 Exemption amount", f.GetExemptionAmount(), money.USCents(2150_00))
	want(t, "Line 7 Taxable nonbusiness income", f.GetTaxableNonbusinessIncome(), money.USCents(68050_00))
	want(t, "Line 8a Nonbusiness tax", f.GetNonbusinessIncomeTax(), money.USCents(1497_00))
	want(t, "Line 13 Total tax", f.GetTotalTax(), money.USCents(1497_00))
	want(t, "Line 14 Withheld", f.GetOhioTaxWithheld(), money.USCents(1400_00))
	want(t, "Line 17 Total payments", f.GetTotalPayments(), money.USCents(1400_00))
	want(t, "Line 22 Amount due", f.GetAmountDue(), money.USCents(97_00))
	want(t, "Line 26 Refund", f.GetRefund(), money.USCents(0))
	want(t, "Summary tax", ret.GetSummary().GetTotalTax(), money.USCents(1497_00))
	want(t, "Summary owed", ret.GetSummary().GetAmountOwed(), money.USCents(97_00))
}

// 2. Married filing jointly with a child: the booklet's "John and Mary"
// exemption example (p. 17), MAGI $75,000 and three exemptions.
//   - Line 1 federal AGI: $50,000 + $25,000 = $75,000.00; line 3 $75,000.00
//   - Line 4: three exemptions x $2,150 (MAGI $40,001-$80,000, p. 17)
//     = $6,450, as the booklet works it
//   - Line 5 / 7: $75,000 - $6,450 = $68,550
//   - Line 8a (p. 18): $342 + 2.75% x ($68,550 - $26,050) = $342 + $1,168.75
//     = $1,510.75, whole dollars $1,511
//   - Line 9 nonrefundable credits, entered from the Schedule of Credits: $60
//   - Line 10 / 13: $1,511 - $60 = $1,451
//   - Line 14 withheld: $1,100 + $500 = $1,600; line 17 / 19 $1,600
//   - Line 23 / 26 refund: $1,600 - $1,451 = $149.00
func TestOhioMarriedFilingJointly(t *testing.T) {
	in := ohInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY,
		ilPerson("john", 1985, 3, 15, false, false),
		ilPerson("mary", 1986, 8, 20, false, false),
		[]*returnsv1.Dependent{ilDependent("patrick", 2015, 6, 1)},
		[]*formw2.FormW2{ohW2(50000_00, 4000_00, 1100_00), ohW2(25000_00, 2000_00, 500_00)},
		&returnsv1.OhioInputs{NonrefundableCredits: cents(60_00)},
	)
	_, f := prepareOhio(t, in)

	want(t, "Line 3 Ohio AGI", f.GetOhioAdjustedGrossIncome(), money.USCents(75000_00))
	if f.GetExemptionCount() != 3 {
		t.Errorf("exemption count: got %d, want 3", f.GetExemptionCount())
	}
	if f.GetFilingStatus() != formit1040.FilingStatus_FILING_STATUS_MARRIED_FILING_JOINTLY {
		t.Errorf("filing status: got %v", f.GetFilingStatus())
	}
	want(t, "Line 4 Exemption amount", f.GetExemptionAmount(), money.USCents(6450_00))
	want(t, "Line 7 Taxable nonbusiness income", f.GetTaxableNonbusinessIncome(), money.USCents(68550_00))
	want(t, "Line 8a Nonbusiness tax", f.GetNonbusinessIncomeTax(), money.USCents(1511_00))
	want(t, "Line 9 Nonrefundable credits", f.GetNonrefundableCredits(), money.USCents(60_00))
	want(t, "Line 13 Total tax", f.GetTotalTax(), money.USCents(1451_00))
	want(t, "Line 17 Total payments", f.GetTotalPayments(), money.USCents(1600_00))
	want(t, "Line 23 Overpayment", f.GetOverpayment(), money.USCents(149_00))
	want(t, "Line 26 Refund", f.GetRefund(), money.USCents(149_00))
	want(t, "Line 22 Amount due", f.GetAmountDue(), money.USCents(0))
}

// 3. High-income single filer above the exemption cutoff, with an addition,
// business income and an estimated payment.
//   - Line 1 federal AGI: wages $800,000.00
//   - Line 2a additions (non-Ohio municipal bond interest): $500.00
//   - Line 3 Ohio AGI: $800,500.00; MAGI $800,500 (p. 8)
//   - Line 4: MAGI $750,000 or greater gives $0 (p. 17 table)
//   - Line 5: $800,500; line 6 taxable business income entered: $10,000
//   - Line 7: $800,500 - $10,000 = $790,500
//   - Line 8a (p. 18): $2,394.32 + 3.125% x ($790,500 - $100,000)
//     = $2,394.32 + $21,578.125 = $23,972.445, whole dollars $23,972
//   - Line 8b: 3% of $10,000 = $300 (p. 11, flat 3% on business income)
//   - Line 8c / 10 / 13: $23,972 + $300 = $24,272
//   - Line 14 withheld $20,000; line 15 estimated payments $1,000
//   - Line 17 / 19: $21,000; line 20 / 22 amount due: $24,272 - $21,000 = $3,272.00
func TestOhioHighIncome(t *testing.T) {
	in := ohInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		ilPerson("dana", 1965, 2, 2, false, false), nil, nil,
		[]*formw2.FormW2{ohW2(800000_00, 200000_00, 20000_00)},
		&returnsv1.OhioInputs{
			Additions:             cents(500_00),
			TaxableBusinessIncome: cents(10000_00),
			EstimatedPayments:     cents(1000_00),
		},
	)
	_, f := prepareOhio(t, in)

	want(t, "Line 3 Ohio AGI", f.GetOhioAdjustedGrossIncome(), money.USCents(800500_00))
	want(t, "Line 4 Exemption amount", f.GetExemptionAmount(), money.USCents(0))
	want(t, "Line 7 Taxable nonbusiness income", f.GetTaxableNonbusinessIncome(), money.USCents(790500_00))
	want(t, "Line 8a Nonbusiness tax", f.GetNonbusinessIncomeTax(), money.USCents(23972_00))
	want(t, "Line 8b Business tax", f.GetBusinessIncomeTax(), money.USCents(300_00))
	want(t, "Line 13 Total tax", f.GetTotalTax(), money.USCents(24272_00))
	want(t, "Line 17 Total payments", f.GetTotalPayments(), money.USCents(21000_00))
	want(t, "Line 22 Amount due", f.GetAmountDue(), money.USCents(3272_00))
	want(t, "Line 26 Refund", f.GetRefund(), money.USCents(0))
}
