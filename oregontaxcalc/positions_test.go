package oregontaxcalc_test

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/oregontaxcalc"
	"github.com/nexteggapp/taxcalcs/working"
)

// TestPositionsCanBeAnswered checks that both positions this package reports
// can also be settled.
//
// An unanswered position that no argument can answer is worse than an
// unreported one: it tells a caller something is undecided and gives them no
// way to decide it.
func TestPositionsCanBeAnswered(t *testing.T) {
	const agi = 95_000 * money.Dollar

	unstated, err := oregontaxcalc.TaxableIncome(2025, filing.Single, agi,
		oregontaxcalc.Conditions{},
		oregontaxcalc.Adjustments{ItemisedDeductions: 4_000 * money.Dollar})
	if err != nil {
		t.Fatalf("TaxableIncome: %v", err)
	}
	for _, want := range []working.PositionKind{
		working.PositionDeductionMethod,
		working.PositionResidency,
	} {
		if !hasPosition(unstated.Required(), want) {
			t.Errorf("%v does not come back as a position when unstated", want)
		}
	}

	answered, err := oregontaxcalc.TaxableIncome(2025, filing.Single, agi,
		oregontaxcalc.Conditions{},
		oregontaxcalc.Adjustments{
			ItemisedDeductions: 4_000 * money.Dollar,
			Election:           oregontaxcalc.ElectItemise,
			Residency:          oregontaxcalc.FullYearResident,
		})
	if err != nil {
		t.Fatalf("TaxableIncome: %v", err)
	}
	if answered.Needs() {
		t.Errorf("answering both positions still leaves %d unanswered", len(answered.Required()))
	}

	// A filer who says they were not a full-year resident gets an error rather
	// than a Form OR-40 figure computed for somebody else.
	if _, err := oregontaxcalc.TaxableIncome(2025, filing.Single, agi,
		oregontaxcalc.Conditions{},
		oregontaxcalc.Adjustments{Residency: oregontaxcalc.PartYearOrNonResident}); err == nil {
		t.Error("a part-year resident got a Form OR-40 figure instead of an error")
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
