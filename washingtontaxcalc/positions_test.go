package washingtontaxcalc_test

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/washingtontaxcalc"
	"github.com/nexteggapp/taxcalcs/working"
)

// TestPositionsCanBeAnswered checks that both positions this package reports
// can also be settled.
//
// These two are the whole of what Washington's tax turns on that its figures do
// not carry, so a caller who has done the allocation work needs a way to say so
// and stop being told about it.
func TestPositionsCanBeAnswered(t *testing.T) {
	const gain = 2_000_000 * money.Dollar

	unstated, err := washingtontaxcalc.ExciseTax(2025, gain,
		washingtontaxcalc.Deductions{}, washingtontaxcalc.Facts{})
	if err != nil {
		t.Fatalf("ExciseTax: %v", err)
	}
	for _, want := range []working.PositionKind{
		working.PositionCapitalAssetSitus,
		working.PositionLotSelection,
	} {
		if !hasPosition(unstated.Required(), want) {
			t.Errorf("%v does not come back as a position when unstated", want)
		}
	}

	answered, err := washingtontaxcalc.ExciseTax(2025, gain,
		washingtontaxcalc.Deductions{},
		washingtontaxcalc.Facts{GainAllocated: true, LotsIdentified: true})
	if err != nil {
		t.Fatalf("ExciseTax: %v", err)
	}
	if answered.Needs() {
		t.Errorf("answering both positions still leaves %d unanswered", len(answered.Required()))
	}

	// Answering a position says something about the filer, not about the
	// arithmetic, so the tax must not move.
	if answered.Value().Total != unstated.Value().Total {
		t.Errorf("answering the positions changed the tax from %v to %v",
			unstated.Value().Total, answered.Value().Total)
	}

	// One at a time, so a caller who has settled the lots but not the
	// allocation still hears about the allocation.
	lots, err := washingtontaxcalc.ExciseTax(2025, gain,
		washingtontaxcalc.Deductions{}, washingtontaxcalc.Facts{LotsIdentified: true})
	if err != nil {
		t.Fatalf("ExciseTax: %v", err)
	}
	if hasPosition(lots.Required(), working.PositionLotSelection) {
		t.Error("lot selection still comes back after being answered")
	}
	if !hasPosition(lots.Required(), working.PositionCapitalAssetSitus) {
		t.Error("answering lot selection also silenced the allocation question")
	}
}

func hasPosition(rs []working.Required, want working.PositionKind) bool {
	for _, r := range rs {
		if r.Position() == want {
			return true
		}
	}
	return false
}
