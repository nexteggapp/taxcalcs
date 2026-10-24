package washingtontaxcalc_test

import (
	"fmt"
	"log"

	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/washingtontaxcalc"
)

// Example works one large share sale through Washington's capital gains excise
// tax.
//
// There is no salary in this example and no filing status, because Washington
// taxes neither. What it taxes is the sale.
//
// The gain here is big enough to meet both rates. The first million of taxable
// gain is at 7%; everything above it is at 7% and then at 2.9% again, so the
// top of the gain meets 9.9% in total.
func Example() {
	const (
		year = 2025
		gain = 2_278_000 * money.Dollar
	)

	tax, err := washingtontaxcalc.ExciseTax(year, gain, washingtontaxcalc.Deductions{}, washingtontaxcalc.Facts{})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Washington gain  %v\n", money.USCents(gain))
	fmt.Printf("deduction       -%v\n", tax.Value().StandardDeduction)
	fmt.Printf("taxable          %v\n", tax.Value().Taxable)
	fmt.Printf("tax              %v\n", tax.Value().Total)

	fmt.Println("\nwhy:")
	for _, r := range tax.Applied() {
		fmt.Printf("  %s\n", r.Cite())
	}

	fmt.Println("\nunanswered:")
	for _, r := range tax.Required() {
		fmt.Printf("  %v\n", r.Position())
	}

	// Output:
	// Washington gain  $2278000.00
	// deduction       -$278000.00
	// taxable          $2000000.00
	// tax              $169000.00
	//
	// why:
	//   RCW 82.87.060(1), as adjusted for 2025
	//   RCW 82.87.040(1)(a)
	//   RCW 82.87.040(1)(b), as enacted by ESSB 5813, ch. 421, Laws of 2025
	//
	// unanswered:
	//   capital asset situs
	//   lot selection
}
