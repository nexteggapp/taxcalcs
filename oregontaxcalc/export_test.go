package oregontaxcalc

import (
	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/money"
)

// WorksheetInterest runs the Form OR-10 underpayment interest worksheet on
// the given dates and daily factors. It is here so that estimated_test.go can
// reproduce the instructions' 2025 example with 2025's dates and factor,
// which the 2026 rules do not carry. It returns the worksheet lines that
// charged interest, in order.
func WorksheetInterest(dues []estimated.Date, required, withheld []money.USCents, payments []estimated.Payment, stop estimated.Date, s estimated.FactorSeries) ([]estimated.PenaltyPart, estimated.Date, error) {
	periods, unrated, err := worksheet(dues, required, withheld, payments, stop, s)
	var parts []estimated.PenaltyPart
	for _, p := range periods {
		parts = append(parts, p.parts...)
	}
	return parts, unrated, err
}
