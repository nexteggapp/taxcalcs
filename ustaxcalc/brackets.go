package ustaxcalc

import (
	"fmt"

	"github.com/nexteggapp/taxcalcs/working"
)

// Band is one bracket of the rate table and what the taxpayer's income put in
// it.
//
// Returned even for bands the income never reached, with Income and Tax zero,
// because a caller showing its working renders the whole ladder and tints the
// row the taxpayer stopped in.
type Band struct {
	Rate Rate

	// From and To bound the band. To is zero on the top band, which has no
	// upper bound.
	From, To USCents

	// Income is how much of the taxpayer's ordinary taxable income fell in this
	// band, and Tax is the rate applied to it.
	Income USCents
	Tax    USCents
}

// Ladder is the rate table applied to one taxpayer's ordinary taxable income.
type Ladder struct {
	Bands []Band

	// Total is the sum of every band's tax.
	Total USCents

	// Marginal is the rate on the next dollar of ordinary income.
	Marginal Rate

	// RoomInBracket is how much more ordinary income fits before the next band
	// starts. Zero on the top band.
	RoomInBracket USCents
}

// OrdinaryIncomeTax applies the § 1(j)(2) rate table to ordinary taxable
// income.
//
// taxableOrdinary is income after deductions and with net long-term capital
// gain already removed, because gains are taxed by [LongTermGainsTax] at their
// own rates rather than by this ladder. Passing total taxable income here would
// tax the gain twice.
//
// A negative amount is treated as zero. Deductions exceeding income produce no
// tax rather than a refundable negative one.
func OrdinaryIncomeTax(year int, status FilingStatus, taxableOrdinary USCents) (Result[Ladder], error) {
	table, err := TableFor(year, status)
	if err != nil {
		return Result[Ladder]{}, err
	}
	return ladderFrom(table, taxableOrdinary)
}

// ladderFrom is the computation, split out so that the gains calculator and the
// sensitivity a caller runs can reuse it without a second table lookup.
func ladderFrom(table Table, taxableOrdinary USCents) (Result[Ladder], error) {
	if taxableOrdinary < 0 {
		taxableOrdinary = 0
	}

	ladder := Ladder{Marginal: table.Ordinary[0].Rate}
	for i, bracket := range table.Ordinary {
		band := Band{Rate: bracket.Rate, From: bracket.Over}
		if i+1 < len(table.Ordinary) {
			band.To = table.Ordinary[i+1].Over
		}

		// How much income falls inside this band. The top band has no ceiling,
		// so everything above its floor lands in it.
		top := taxableOrdinary
		if band.To != 0 && top > band.To {
			top = band.To
		}
		if top > band.From {
			band.Income = top - band.From
			band.Tax = bracket.Rate.Of(band.Income)
			ladder.Total += band.Tax
			ladder.Marginal = bracket.Rate
			if band.To != 0 {
				ladder.RoomInBracket = band.To - taxableOrdinary
			} else {
				ladder.RoomInBracket = 0
			}
		}
		ladder.Bands = append(ladder.Bands, band)
	}

	return working.NewResult(ladder).
		Rule(fmt.Sprintf("26 U.S.C. § 1(j)(2), as adjusted for %d", table.Year),
			fmt.Sprintf("%s of ordinary taxable income through the %s brackets for a %s filer gives %s",
				taxableOrdinary, table.Ordinary[len(table.Ordinary)-1].Rate, table.Status, ladder.Total)).
		Build()
}

// EffectiveRate returns tax as a proportion of income, rounded to the nearest
// basis point.
//
// Zero for zero income, rather than an error. A taxpayer with no income has an
// effective rate of nothing, and every caller of this is rendering a percentage
// beside a marginal one.
func EffectiveRate(tax, income USCents) Rate {
	if income <= 0 {
		return 0
	}
	return Rate((int64(tax)*10000 + int64(income)/2) / int64(income))
}
