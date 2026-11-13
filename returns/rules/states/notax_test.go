package states_test

import (
	"strings"
	"testing"

	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
)

// Each state with no individual income tax returns that no return is due:
// complete, zero tax, nothing paid, and one informational diagnostic citing
// the state's law. The household's federal return is unchanged by them.
func TestNoIncomeTaxStates(t *testing.T) {
	cases := []struct {
		state string
		cite  string // a citation the diagnostic must carry
	}{
		{"AK", "Alaska Department of Revenue"},
		{"FL", "Fla. Const. art. VII, § 5(a)"},
		{"NV", "Nev. Const. art. 10, § 1(9)"},
		{"NH", "N.H. RSA ch. 77"},
		{"SD", "South Dakota Department of Revenue"},
		{"TN", "Tenn. Const. art. II, § 28"},
		{"TX", "Tex. Const. art. VIII, § 24-a"},
		{"WY", "Wyo. Stat. tit. 39, ch. 7"},
	}
	for _, c := range cases {
		t.Run(c.state, func(t *testing.T) {
			in := inputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY, 120_000_00, 0)
			in.States = []*returnsv1.StateInputs{{State: c.state}}
			out := prepare(t, in)
			if len(out.GetStates()) != 1 {
				t.Fatalf("got %d state returns, want 1", len(out.GetStates()))
			}
			sr := out.GetStates()[0]
			if sr.GetState() != c.state {
				t.Errorf("state = %q, want %q", sr.GetState(), c.state)
			}
			r := sr.GetNoIncomeTax()
			if r == nil {
				t.Fatalf("%s: %v, want a no-income-tax return", c.state, sr)
			}
			if r.GetStatus() != returnsv1.ReturnStatus_RETURN_STATUS_COMPLETE {
				t.Errorf("status = %v, want complete", r.GetStatus())
			}
			s := r.GetSummary()
			if s.GetJurisdiction() != c.state {
				t.Errorf("jurisdiction = %q, want %q", s.GetJurisdiction(), c.state)
			}
			want(t, "total tax", s.GetTotalTax(), 0)
			want(t, "payments", s.GetTotalPaymentsAndRefundableCredits(), 0)
			want(t, "refund", s.GetRefund(), 0)
			want(t, "amount owed", s.GetAmountOwed(), 0)

			diags := r.GetDiagnostics()
			if len(diags) != 1 {
				t.Fatalf("got %d diagnostics, want 1: %v", len(diags), diags)
			}
			d := diags[0]
			if d.GetCode() != c.state+"_NO_INCOME_TAX" {
				t.Errorf("code = %q", d.GetCode())
			}
			if d.GetSeverity() != returnsv1.Severity_SEVERITY_INFO {
				t.Errorf("severity = %v, want info", d.GetSeverity())
			}
			found := false
			for _, ct := range d.GetCitations() {
				if strings.Contains(ct.GetAuthority(), c.cite) {
					found = true
				}
			}
			if !found {
				t.Errorf("citations %v lack %q", d.GetCitations(), c.cite)
			}
			if c.state == "NH" && !strings.Contains(d.GetMessage(), "January 1, 2025") {
				t.Errorf("NH message %q does not say the interest and dividends tax was repealed from 2025", d.GetMessage())
			}

			// The state adds nothing to the household's combined tax.
			if got, fed := out.GetSummary().GetCombinedTotalTax().GetMinor(), out.GetFederal().GetSummary().GetTotalTax().GetMinor(); got != fed {
				t.Errorf("combined tax %d, want the federal tax %d", got, fed)
			}
		})
	}
}
