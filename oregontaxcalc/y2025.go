package oregontaxcalc

// Tax year 2025.
//
// The brackets, the standard deduction, the exemption credit and the federal
// tax subtraction maximum are the four-year table of "Miscellaneous Oregon
// income tax information" in Publication OR-17, whose last column is 2025. The
// phase-out steps and the additions for age and blindness are the 2025 Form
// OR-40 instructions. The extracts are in testdata and tables_test.go checks
// every figure below against them.

import (
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
)

func init() {
	const (
		// ORS 316.037. Four rates, shared by both charts, which differ only in
		// where the bands start.
		r475   = money.Rate(475) // 4.75%
		r675   = money.Rate(675) // 6.75%
		r875   = money.Rate(875) // 8.75%
		r99    = money.Rate(990) // 9.9%
		dollar = money.Dollar
	)

	// Chart S. Single, or married filing separately.
	chartS := []Bracket{
		{Over: 0, Rate: r475},
		{Over: 4_400 * dollar, Rate: r675},
		{Over: 11_100 * dollar, Rate: r875},
		{Over: 125_000 * dollar, Rate: r99},
	}

	// Chart J. Married filing jointly, head of household, or qualifying
	// surviving spouse. Every threshold is twice Chart S's except the top,
	// which is twice $125,000 exactly.
	chartJ := []Bracket{
		{Over: 0, Rate: r475},
		{Over: 8_800 * dollar, Rate: r675},
		{Over: 22_200 * dollar, Rate: r875},
		{Over: 250_000 * dollar, Rate: r99},
	}

	// Table 4. The federal tax subtraction shrinks in five steps and then
	// stops. A separate filer's figures are half a single filer's throughout.
	subtractionSingle := []SubtractionStep{
		{AtLeast: 0, Maximum: 8_500 * dollar},
		{AtLeast: 125_000 * dollar, Maximum: 6_800 * dollar},
		{AtLeast: 130_000 * dollar, Maximum: 5_100 * dollar},
		{AtLeast: 135_000 * dollar, Maximum: 3_400 * dollar},
		{AtLeast: 140_000 * dollar, Maximum: 1_700 * dollar},
		{AtLeast: 145_000 * dollar, Maximum: 0},
	}
	subtractionSeparate := []SubtractionStep{
		{AtLeast: 0, Maximum: 4_250 * dollar},
		{AtLeast: 125_000 * dollar, Maximum: 3_400 * dollar},
		{AtLeast: 130_000 * dollar, Maximum: 2_550 * dollar},
		{AtLeast: 135_000 * dollar, Maximum: 1_700 * dollar},
		{AtLeast: 140_000 * dollar, Maximum: 850 * dollar},
		{AtLeast: 145_000 * dollar, Maximum: 0},
	}
	subtractionJoint := []SubtractionStep{
		{AtLeast: 0, Maximum: 8_500 * dollar},
		{AtLeast: 250_000 * dollar, Maximum: 6_800 * dollar},
		{AtLeast: 260_000 * dollar, Maximum: 5_100 * dollar},
		{AtLeast: 270_000 * dollar, Maximum: 3_400 * dollar},
		{AtLeast: 280_000 * dollar, Maximum: 1_700 * dollar},
		{AtLeast: 290_000 * dollar, Maximum: 0},
	}

	byStatus := map[filing.Status]Table{
		filing.Single: {
			Ordinary:                chartS,
			StandardDeduction:       2_835 * dollar,
			AdditionalAgedBlind:     1_200 * dollar,
			ExemptionCreditAGILimit: 100_000 * dollar,
			FederalTaxSubtraction:   subtractionSingle,
		},
		filing.MarriedFilingSeparately: {
			Ordinary:            chartS,
			StandardDeduction:   2_835 * dollar,
			AdditionalAgedBlind: 1_000 * dollar,
			// Halved with everything else on a separate return.
			ExemptionCreditAGILimit: 100_000 * dollar,
			FederalTaxSubtraction:   subtractionSeparate,
		},
		filing.MarriedFilingJointly: {
			Ordinary:                chartJ,
			StandardDeduction:       5_670 * dollar,
			AdditionalAgedBlind:     1_000 * dollar,
			ExemptionCreditAGILimit: 200_000 * dollar,
			FederalTaxSubtraction:   subtractionJoint,
		},
		filing.SurvivingSpouse: {
			Ordinary:                chartJ,
			StandardDeduction:       5_670 * dollar,
			AdditionalAgedBlind:     1_000 * dollar,
			ExemptionCreditAGILimit: 200_000 * dollar,
			FederalTaxSubtraction:   subtractionJoint,
		},
		filing.HeadOfHousehold: {
			Ordinary:          chartJ,
			StandardDeduction: 4_560 * dollar,
			// A head of household is unmarried, so the larger addition.
			AdditionalAgedBlind:     1_200 * dollar,
			ExemptionCreditAGILimit: 100_000 * dollar,
			FederalTaxSubtraction:   subtractionJoint,
		},
	}

	for status, t := range byStatus {
		t.Year = 2025
		t.Status = status
		t.ExemptionCredit = 256 * dollar
		byStatus[status] = t
	}

	register(2025, byStatus)
}
