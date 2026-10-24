package taxyear

import (
	"testing"

	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
)

// Form 8949 rounds by column: the gain is the rounded proceeds less the
// rounded basis, not the exact gain rounded. $100.40 less $50.60 is $49.80,
// which rounds to $50; the columns give $100 less $51, $49.
func TestForm8949RoundsByColumn(t *testing.T) {
	soFar := []*scenariosv1.Form8949Totals{{Box: scenariosv1.Form8949Box_FORM_8949_BOX_A,
		Proceeds: model.Money(10_040), Basis: model.Money(5_060), Adjustments: model.Money(0), Gain: model.Money(4_980)}}
	year := brokerYear(soFar, nil)
	if got := year[0].GetGain().GetMinor(); got != 4_900 {
		t.Errorf("the gain is %d, want 4900", got)
	}
	if got := soFar[0].GetGain().GetMinor(); got != 4_900 {
		t.Errorf("so far's gain is %d, want 4900", got)
	}
}
