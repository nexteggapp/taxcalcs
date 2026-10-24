package oregontaxcalc_test

import (
	"fmt"
	"log"

	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/oregontaxcalc"
)

// Example works one salaried Oregon filer from federal adjusted gross income to
// tax owed.
//
// It needs two federal figures rather than one, because Oregon lets a filer
// subtract part of the federal income tax they paid. Both are plain amounts:
// nothing here imports the federal calculators.
//
// The federal tax subtraction is the largest single line on this return —
// larger than Oregon's standard deduction, which is $2,835.
func Example() {
	const (
		year       = 2025
		agi        = 95_000 * money.Dollar
		federalTax = 12_000 * money.Dollar
		exemptions = 1
	)
	status := filing.Single

	subtraction, err := oregontaxcalc.FederalTaxSubtraction(year, status, federalTax, agi)
	if err != nil {
		log.Fatal(err)
	}

	income, err := oregontaxcalc.TaxableIncome(year, status, agi,
		oregontaxcalc.Conditions{},
		oregontaxcalc.Adjustments{FederalTaxSubtraction: subtraction.Value()})
	if err != nil {
		log.Fatal(err)
	}

	tax, err := oregontaxcalc.OrdinaryIncomeTax(year, status, income.Value().Amount)
	if err != nil {
		log.Fatal(err)
	}

	credit, err := oregontaxcalc.ExemptionCredit(year, status, agi, exemptions)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("federal AGI     %v\n", money.USCents(agi))
	fmt.Printf("deduction       %v\n", income.Value().Deduction.Amount)
	fmt.Printf("federal tax     %v subtracted, capped at $8500.00\n", income.Value().FederalTaxSubtraction)
	fmt.Printf("taxable         %v\n", income.Value().Amount)
	fmt.Printf("tax             %v\n", tax.Value().Total)
	fmt.Printf("exemptions     -%v\n", credit.Value())
	fmt.Printf("owed            %v\n", tax.Value().Total-credit.Value())

	fmt.Println("\nwhy, each bracket rounded to a whole dollar as Oregon does:")
	for _, b := range tax.Value().Bands {
		if b.Income == 0 {
			continue
		}
		fmt.Printf("  %v on %v = %v\n", b.Rate, b.Income, b.Tax)
	}

	// Output:
	// federal AGI     $95000.00
	// deduction       $2835.00
	// federal tax     $8500.00 subtracted, capped at $8500.00
	// taxable         $83665.00
	// tax             $7010.00
	// exemptions     -$256.00
	// owed            $6754.00
	//
	// why, each bracket rounded to a whole dollar as Oregon does:
	//   4.75% on $4400.00 = $209.00
	//   6.75% on $6700.00 = $452.00
	//   8.75% on $72565.00 = $6349.00
}
