package californiataxcalc_test

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/californiataxcalc"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// TestResidencyCanBeAnswered checks that the position this package reports can
// also be settled, which is what stops it being noise.
//
// An unanswered position that no argument can answer is worse than an
// unreported one: it tells a caller something is undecided and gives them no
// way to decide it.
func TestResidencyCanBeAnswered(t *testing.T) {
	agi := 150_000 * money.Dollar

	unstated, err := californiataxcalc.TaxableIncome(2025, filing.Single, agi,
		californiataxcalc.Adjustments{Election: californiataxcalc.ElectStandard})
	if err != nil {
		t.Fatalf("TaxableIncome: %v", err)
	}
	if !hasPosition(unstated.Required(), working.PositionResidency) {
		t.Error("an unstated residency does not come back as a position")
	}

	answered, err := californiataxcalc.TaxableIncome(2025, filing.Single, agi,
		californiataxcalc.Adjustments{
			Election:  californiataxcalc.ElectStandard,
			Residency: californiataxcalc.FullYearResident,
		})
	if err != nil {
		t.Fatalf("TaxableIncome: %v", err)
	}
	if hasPosition(answered.Required(), working.PositionResidency) {
		t.Error("residency still comes back after being answered")
	}
	if answered.Value().Amount != unstated.Value().Amount {
		t.Errorf("answering residency changed taxable income from %v to %v",
			unstated.Value().Amount, answered.Value().Amount)
	}

	// A filer who says they were not a full-year resident gets an error rather
	// than a Form 540 figure computed for somebody else.
	if _, err := californiataxcalc.TaxableIncome(2025, filing.Single, agi,
		californiataxcalc.Adjustments{Residency: californiataxcalc.PartYearOrNonResident}); err == nil {
		t.Error("a part-year resident got a Form 540 figure instead of an error")
	}
}

// TestDeductionMethodCanBeAnswered checks the same for the § 63(e) election
// California poses on its own account.
func TestDeductionMethodCanBeAnswered(t *testing.T) {
	agi := 150_000 * money.Dollar

	larger, err := californiataxcalc.TaxableIncome(2025, filing.Single, agi,
		californiataxcalc.Adjustments{ItemisedDeductions: 9_400 * money.Dollar})
	if err != nil {
		t.Fatalf("TaxableIncome: %v", err)
	}
	if !hasPosition(larger.Required(), working.PositionDeductionMethod) {
		t.Error("letting the library choose does not report the election")
	}

	elected, err := californiataxcalc.TaxableIncome(2025, filing.Single, agi,
		californiataxcalc.Adjustments{
			ItemisedDeductions: 9_400 * money.Dollar,
			Election:           californiataxcalc.ElectItemise,
		})
	if err != nil {
		t.Fatalf("TaxableIncome: %v", err)
	}
	if hasPosition(elected.Required(), working.PositionDeductionMethod) {
		t.Error("the election still comes back after being made")
	}

	// Electing the standard deduction takes it even though itemising is worth
	// more here, because that is the filer's to decide.
	standard, err := californiataxcalc.TaxableIncome(2025, filing.Single, agi,
		californiataxcalc.Adjustments{
			ItemisedDeductions: 9_400 * money.Dollar,
			Election:           californiataxcalc.ElectStandard,
		})
	if err != nil {
		t.Fatalf("TaxableIncome: %v", err)
	}
	if want := 5_706 * money.Dollar; standard.Value().Deduction != want {
		t.Errorf("electing the standard deduction took %v, want %v",
			standard.Value().Deduction, money.USCents(want))
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
