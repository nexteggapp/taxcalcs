package states_test

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/money"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	form1099b "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_b"
	capitalgains "github.com/nexteggapp/taxprotos/go/us/state/wa/v1/capital_gains"
)

// A household with wages and one long-term sale on a Form 1099-B, asking for
// the Washington capital gains excise tax return.
func waInputs(status returnsv1.FilingStatusElection, ltGain int64, wa *returnsv1.WashingtonInputs) *returnsv1.ReturnInputs {
	in := inputs(status, 150_000_00, 0)
	in.Federal.Documents.Brokers = []*returnsv1.BrokerStatement{{
		Id:     "broker",
		Person: "ana",
		Transactions: []*returnsv1.BrokerTransaction{{
			Id: "lt-sale",
			Form: &form1099b.Form1099B{
				DescriptionOfProperty: "Index fund shares",
				DateAcquired:          &commonv1.Date{Year: 2015, Month: 3, Day: 2},
				DateSoldOrDisposed:    &commonv1.Date{Year: 2025, Month: 9, Day: 15},
				Proceeds:              cents(ltGain + 100_000_00),
				CostOrOtherBasis:      cents(100_000_00),
				HoldingPeriod:         form1099b.Form1099B_HOLDING_PERIOD_LONG_TERM,
				BasisReportedToIrs:    true,
			},
		}},
	}}
	in.States = []*returnsv1.StateInputs{{
		State:  "WA",
		Inputs: &returnsv1.StateInputs_Wa{Wa: wa},
	}}
	return in
}

// Each household's Washington return matches what washingtontaxcalc.ExciseTax
// computed on the same gain and deductions, frozen in waOracle when that
// package was removed: the standard deduction, the charitable
// deduction above its threshold and below its cap, the 7% base rate, and the
// 2.9% surcharge above $1,000,000.
func TestWashingtonCapitalGainsMatchesTheGoCalculator(t *testing.T) {
	cases := []struct {
		name        string
		status      returnsv1.FilingStatusElection
		ltGain      int64
		donations   int64
		family      int64
		payments    int64
		wantTax     money.USCents // worked by hand, as a check on both
		wantRequire bool          // whether any tax is due
	}{
		// $200,000 of gain is below the $278,000 standard deduction: no tax.
		{"below the deduction", returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
			200_000_00, 0, 0, 0, 0, false},
		// $700,000 - $278,000 = $422,000 at 7% = $29,540.
		{"joint above the deduction", returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY,
			700_000_00, 0, 0, 0, 29_540_00, true},
		// Donations of $400,000 deduct $400,000 - $278,000 = $122,000, capped at
		// $111,000. $1,500,000 - $278,000 - $111,000 = $1,111,000 taxable:
		// 7% = $77,770, plus 2.9% of the $111,000 above $1,000,000 = $3,219,
		// $80,989 in all. $50,000 paid leaves $30,989 owed.
		{"charitable deduction and surcharge", returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
			1_500_000_00, 400_000_00, 0, 50_000_00, 80_989_00, true},
		// Donations of $300,000 deduct $22,000; a $100,000 family-owned small
		// business deduction. $600,000 - $278,000 - $22,000 - $100,000 =
		// $200,000 at 7% = $14,000. $20,000 paid gives $6,000 back.
		{"charity below the cap and family business", returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY,
			600_000_00, 300_000_00, 100_000_00, 20_000_00, 14_000_00, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			wa := &returnsv1.WashingtonInputs{
				WashingtonCharityDonations:       cents(c.donations),
				QualifiedFamilyBusinessDeduction: cents(c.family),
				PaymentsMade:                     cents(c.payments),
			}
			out := prepare(t, waInputs(c.status, c.ltGain, wa))
			r := out.GetStates()[0].GetWa()
			if r == nil {
				t.Fatalf("WA: %v, want a Washington return", out.GetStates()[0])
			}

			oracle, ok := waOracle[c.ltGain]
			if !ok {
				t.Fatalf("no frozen figures for a gain of %d", c.ltGain)
			}
			if oracle.Total != c.wantTax {
				t.Fatalf("washingtontaxcalc total %d, worked by hand %d", oracle.Total, c.wantTax)
			}

			cg := r.GetForms().GetCapitalGains()
			want(t, "federal net long-term gain", cg.GetFederalNetLongTermCapitalGain(), money.USCents(c.ltGain))
			want(t, "allocated gain", cg.GetAllocatedCapitalGain(), oracle.WashingtonCapitalGains)
			want(t, "standard deduction", cg.GetStandardDeduction(), oracle.StandardDeduction)
			want(t, "charitable deduction", cg.GetCharitableDeduction(), oracle.CharitableDeduction)
			want(t, "family business deduction", cg.GetFamilyOwnedSmallBusinessDeduction(), oracle.FamilyBusinessDeduction)
			want(t, "taxable", cg.GetTaxableWashingtonCapitalGains(), oracle.Taxable)
			want(t, "base tax", cg.GetTaxAtBaseRate(), oracle.BaseTax)
			want(t, "surcharge", cg.GetTaxAtSurchargeRate(), oracle.SurchargeTax)
			want(t, "total tax", cg.GetTotalTax(), oracle.Total)
			want(t, "net tax due", cg.GetNetTaxDue(), oracle.Total)

			wantStatus := capitalgains.FilingStatus_FILING_STATUS_INDIVIDUAL
			if c.status == returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY {
				wantStatus = capitalgains.FilingStatus_FILING_STATUS_JOINT
			}
			if cg.GetFilingStatus() != wantStatus {
				t.Errorf("filing status = %v, want %v", cg.GetFilingStatus(), wantStatus)
			}

			s := r.GetSummary()
			want(t, "summary tax", s.GetTotalTax(), oracle.Total)
			want(t, "summary payments", s.GetTotalPaymentsAndRefundableCredits(), money.USCents(c.payments))
			owed, refund := oracle.Total-money.USCents(c.payments), money.USCents(0)
			if owed < 0 {
				owed, refund = 0, -owed
			}
			want(t, "amount owed", s.GetAmountOwed(), owed)
			want(t, "refund", s.GetRefund(), refund)
			complete(t, "WA", r.GetStatus(), r.GetDiagnostics())

			noTax := false
			for _, d := range r.GetDiagnostics() {
				if d.GetCode() == "WA_NO_CAPITAL_GAINS_TAX" {
					noTax = true
				}
			}
			if noTax == c.wantRequire {
				t.Errorf("WA_NO_CAPITAL_GAINS_TAX present = %v, want %v", noTax, !c.wantRequire)
			}
		})
	}
}

// A WA request with no Washington inputs still prepares the return.
func TestWashingtonWithoutInputs(t *testing.T) {
	in := waInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, 500_000_00, nil)
	in.States = []*returnsv1.StateInputs{{State: "WA"}}
	r := prepare(t, in).GetStates()[0].GetWa()
	if r == nil {
		t.Fatal("want a Washington return")
	}
	// $500,000 - $278,000 = $222,000 at 7% = $15,540.
	want(t, "tax", r.GetSummary().GetTotalTax(), 15_540_00)
}

// waFigures are washingtontaxcalc.ExciseTax's figures for one household.
type waFigures struct {
	WashingtonCapitalGains, StandardDeduction, CharitableDeduction, FamilyBusinessDeduction,
	Taxable, BaseTax, SurchargeTax, Total money.USCents
}

// waOracle is what washingtontaxcalc.ExciseTax gave each household of
// TestWashingtonCapitalGainsMatchesTheGoCalculator, by long-term gain.
var waOracle = map[int64]waFigures{
	200_000_00:   {200_000_00, 278_000_00, 0, 0, 0, 0, 0, 0},
	700_000_00:   {700_000_00, 278_000_00, 0, 0, 422_000_00, 29_540_00, 0, 29_540_00},
	1_500_000_00: {1_500_000_00, 278_000_00, 111_000_00, 0, 1_111_000_00, 77_770_00, 3_219_00, 80_989_00},
	600_000_00:   {600_000_00, 278_000_00, 22_000_00, 100_000_00, 200_000_00, 14_000_00, 0, 14_000_00},
}
