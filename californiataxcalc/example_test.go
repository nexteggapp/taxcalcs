package californiataxcalc_test

import (
	"fmt"
	"log"

	"github.com/nexteggapp/taxcalcs/californiataxcalc"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
)

// Example works one salaried California filer from federal adjusted gross
// income to tax owed.
//
// It starts from the federal figure because California does, and it takes that
// figure as a plain amount: nothing here imports the federal calculators.
//
// The filer itemises. Their $9,400 of itemised deductions lost to the federal
// standard deduction, and wins here, because California's standard deduction is
// $5,706. That is the usual case rather than an odd one, and it is why a
// product cannot carry the federal answer across.
func Example() {
	const (
		year     = 2025
		agi      = 150_000 * money.Dollar
		itemised = 9_400 * money.Dollar
	)
	status := filing.Single

	income, err := californiataxcalc.TaxableIncome(year, status, agi, californiataxcalc.Adjustments{
		ItemisedDeductions: itemised,
	})
	if err != nil {
		log.Fatal(err)
	}

	tax, err := californiataxcalc.OrdinaryIncomeTax(year, status, income.Value().Amount)
	if err != nil {
		log.Fatal(err)
	}

	credit, err := californiataxcalc.ExemptionCredit(year, status, agi,
		californiataxcalc.Exemptions{Personal: 1})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("federal AGI    %v\n", money.USCents(agi))
	fmt.Printf("deduction      %v (standard %v, itemised %v)\n",
		income.Value().Deduction, income.Value().Standard, money.USCents(itemised))
	fmt.Printf("taxable        %v\n", income.Value().Amount)
	fmt.Printf("tax            %v\n", tax.Value().Total)
	fmt.Printf("exemptions    -%v\n", credit.Value())
	fmt.Printf("owed           %v\n", tax.Value().Total-credit.Value())
	fmt.Printf("marginal rate  %v\n", tax.Value().Marginal)

	fmt.Println("\nwhy:")
	for _, b := range tax.Value().Bands {
		if b.Income == 0 {
			continue
		}
		fmt.Printf("  %v on %v = %v\n", b.Rate, b.Income, b.Tax)
	}

	fmt.Println("\nunanswered:")
	for _, r := range income.Required() {
		fmt.Printf("  %v\n", r.Position())
	}

	// Output:
	// federal AGI    $150000.00
	// deduction      $9400.00 (standard $5706.00, itemised $9400.00)
	// taxable        $140600.00
	// tax            $9514.44
	// exemptions    -$153.00
	// owed           $9361.44
	// marginal rate  9.3%
	//
	// why:
	//   1% on $11079.00 = $110.79
	//   2% on $15185.00 = $303.70
	//   4% on $15188.00 = $607.52
	//   6% on $16090.00 = $965.40
	//   8% on $15182.00 = $1214.56
	//   9.3% on $67876.00 = $6312.47
	//
	// unanswered:
	//   deduction method
	//   residency
}
