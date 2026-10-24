package ustaxcalc_test

import (
	"fmt"
	"log"

	"github.com/nexteggapp/taxcalcs/ustaxcalc"
)

// Example works one salaried filer from gross pay to tax owed.
//
// The point of running it end to end is that no calculator here takes gross
// income. OrdinaryIncomeTax wants taxable ordinary income, which is what is
// left after the deduction and with any long-term gain removed, and handing it
// a salary would overstate the tax by whatever the deduction was worth.
func Example() {
	const year = 2026
	status := ustaxcalc.Single
	salary := ustaxcalc.Dollars(120_000)

	// What the filer could itemise. Below the standard deduction here, so the
	// standard one wins; DeductionTaken makes that comparison rather than the
	// caller.
	itemised := ustaxcalc.Dollars(9_400)

	deduction, err := ustaxcalc.DeductionTaken(year, status, ustaxcalc.Conditions{}, itemised, ustaxcalc.ElectLarger)
	if err != nil {
		log.Fatal(err)
	}

	taxable := salary - deduction.Value().Amount
	tax, err := ustaxcalc.OrdinaryIncomeTax(year, status, taxable)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("salary         %v\n", salary)
	fmt.Printf("deduction      %v (standard %v, itemised %v)\n",
		deduction.Value().Amount, deduction.Value().Standard, itemised)
	fmt.Printf("taxable        %v\n", taxable)
	fmt.Printf("tax            %v\n", tax.Value().Total)
	fmt.Printf("marginal rate  %v, with %v of room before the next band\n",
		tax.Value().Marginal, tax.Value().RoomInBracket)

	fmt.Println("\nwhy:")
	for _, r := range tax.Value().Bands {
		if r.Income == 0 {
			continue
		}
		fmt.Printf("  %v on %v = %v\n", r.Rate, r.Income, r.Tax)
	}

	fmt.Println("\nunanswered:")
	for _, r := range deduction.Required() {
		fmt.Printf("  %v: %s\n", r.Position(), r.Because())
	}

	// Output:
	// salary         $120000.00
	// deduction      $16100.00 (standard $16100.00, itemised $9400.00)
	// taxable        $103900.00
	// tax            $17570.00
	// marginal rate  22%, with $1800.00 of room before the next band
	//
	// why:
	//   10% on $12400.00 = $1240.00
	//   12% on $38000.00 = $4560.00
	//   22% on $53500.00 = $11770.00
	//
	// unanswered:
	//   deduction method: § 63(e) makes itemising an election rather than an outcome. A taxpayer may itemise a smaller total, and one filing separately must itemise if their spouse does
}
