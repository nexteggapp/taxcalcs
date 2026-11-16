package federal_test

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
)

// Estimated tax answers one entry per jurisdiction asked for, in order, and
// none when it isn't asked for. The federal, California and Oregon rules
// answer; until a state's rules land, its answer is unsupported.
func TestEstimatedAnswersEachJurisdictionAskedFor(t *testing.T) {
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() failed: %v", err)
	}
	base := func() *returnsv1.ReturnInputs {
		return &returnsv1.ReturnInputs{
			SchemaVersion: 1,
			TaxYear:       2026,
			States:        []*returnsv1.StateInputs{{State: "CA"}, {State: "OR"}},
		}
	}

	for _, tc := range []struct {
		name     string
		estimate *returnsv1.EstimatedTaxInputs
		want     []string
	}{
		{"not asked for", nil, nil},
		{"federal and every state", &returnsv1.EstimatedTaxInputs{
			AsOf:     date(2026, 6, 1),
			ThisYear: &returnsv1.EstimatedTaxInputs_LastYearOnly_{LastYearOnly: &returnsv1.EstimatedTaxInputs_LastYearOnly{}},
		}, []string{"US", "CA", "OR"}},
		{"those named", &returnsv1.EstimatedTaxInputs{
			AsOf:          date(2026, 6, 1),
			Jurisdictions: []string{"OR", "US"},
			ThisYear:      &returnsv1.EstimatedTaxInputs_LastYearOnly_{LastYearOnly: &returnsv1.EstimatedTaxInputs_LastYearOnly{}},
		}, []string{"OR", "US"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := base()
			in.EstimatedTax = tc.estimate
			out, err := eng.Prepare(in)
			if err != nil {
				t.Fatalf("Prepare() failed: %v", err)
			}
			got := out.GetEstimated()
			if len(got) != len(tc.want) {
				t.Fatalf("got %d answers, want %d", len(got), len(tc.want))
			}
			for i, a := range got {
				if a.GetJurisdiction() != tc.want[i] {
					t.Errorf("answer %d is for %q, want %q", i, a.GetJurisdiction(), tc.want[i])
				}
				want := returnsv1.EstimatedOutcome_ESTIMATED_OUTCOME_UNSUPPORTED
				if a.GetJurisdiction() == "US" || a.GetJurisdiction() == "CA" || a.GetJurisdiction() == "OR" {
					want = returnsv1.EstimatedOutcome_ESTIMATED_OUTCOME_COMPUTED
				}
				if a.GetOutcome() != want {
					t.Errorf("answer %d outcome = %v, want %v", i, a.GetOutcome(), want)
				}
			}
		})
	}
}
