package californiataxcalc

import (
	"fmt"

	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// Band is one bracket of a rate schedule and what the taxpayer's income put in
// it.
//
// Returned even for bands the income never reached, with Income and Tax zero,
// because a caller showing its working renders the whole ladder and tints the
// row the taxpayer stopped in.
type Band struct {
	Rate money.Rate

	// From and To bound the band. To is zero on the top band, which has no
	// upper bound.
	From, To money.USCents

	// Income is how much of the taxpayer's California taxable income fell in
	// this band, and Tax is the rate applied to it.
	Income money.USCents
	Tax    money.USCents
}

// Ladder is the rate schedule applied to one taxpayer's California taxable
// income.
type Ladder struct {
	Bands []Band

	// Total is the sum of every band's tax.
	Total money.USCents

	// Marginal is the rate on the next dollar of income.
	Marginal money.Rate

	// RoomInBracket is how much more income fits before the next band starts.
	// Zero on the top band.
	RoomInBracket money.USCents
}

// OrdinaryIncomeTax applies the Rev. & Tax. Code § 17041 rate schedule to
// California taxable income.
//
// taxable is income after the California deduction, from [TaxableIncome]. It
// includes any capital gain in full: California has no preferential rate for
// one, so a caller must not hold a gain back the way the federal calculators
// require.
//
// This is the tax before credits. The exemption credits from [ExemptionCredit]
// are subtracted from it, and the surcharge from
// [BehavioralHealthServicesTax] is added after them.
//
// A negative amount is treated as zero. Deductions exceeding income produce no
// tax rather than a refundable negative one.
func OrdinaryIncomeTax(year int, status filing.Status, taxable money.USCents) (working.Result[Ladder], error) {
	table, err := TableFor(year, status)
	if err != nil {
		return working.Result[Ladder]{}, err
	}
	return ladderFrom(table, taxable)
}

// ladderFrom is the computation, split out so that the table test can reuse it
// without a second lookup.
func ladderFrom(table Table, taxable money.USCents) (working.Result[Ladder], error) {
	if taxable < 0 {
		taxable = 0
	}

	ladder := Ladder{Marginal: table.Ordinary[0].Rate}
	for i, bracket := range table.Ordinary {
		band := Band{Rate: bracket.Rate, From: bracket.Over}
		if i+1 < len(table.Ordinary) {
			band.To = table.Ordinary[i+1].Over
		}

		// How much income falls inside this band. The top band has no ceiling,
		// so everything above its floor lands in it.
		top := taxable
		if band.To != 0 && top > band.To {
			top = band.To
		}
		if top > band.From {
			band.Income = top - band.From
			band.Tax = bracket.Rate.Of(band.Income)
			ladder.Total += band.Tax
			ladder.Marginal = bracket.Rate
			if band.To != 0 {
				ladder.RoomInBracket = band.To - taxable
			} else {
				ladder.RoomInBracket = 0
			}
		}
		ladder.Bands = append(ladder.Bands, band)
	}

	return working.NewResult(ladder).Rule(fmt.Sprintf("Cal. Rev. & Tax. Code § 17041, as adjusted for %d", table.Year), fmt.Sprintf("%s of California taxable income through the %s schedule for a %s filer gives %s",
		taxable, table.Ordinary[len(table.Ordinary)-1].Rate, table.Status, ladder.Total)).Build()
}

// BehavioralHealthServicesTax computes the Rev. & Tax. Code § 17043 surcharge
// on taxable income above one million dollars.
//
// One per cent of the excess, and nothing at all below the threshold. Proposition
// 1 renamed it from the Mental Health Services Tax for taxable years beginning
// on or after 1 January 2025; the charge itself did not change.
//
// It is a tax rather than a rate band, so it is added after the exemption
// credits have reduced the tax from [OrdinaryIncomeTax] and is not itself
// reduced by them.
func BehavioralHealthServicesTax(year int, status filing.Status, taxable money.USCents) (working.Result[money.USCents], error) {
	table, err := TableFor(year, status)
	if err != nil {
		return working.Result[money.USCents]{}, err
	}

	excess := taxable - table.BehavioralHealthThreshold
	if excess <= 0 {
		return working.NewResult(money.USCents(0)).Rule("Cal. Rev. & Tax. Code § 17043", fmt.Sprintf("taxable income of %s is not above %s, so no surcharge is due",
			taxable, table.BehavioralHealthThreshold)).Build()
	}

	tax := table.BehavioralHealthRate.Of(excess)
	return working.NewResult(tax).Rule("Cal. Rev. & Tax. Code § 17043", fmt.Sprintf("%s of taxable income above %s, at %s, gives %s",
		excess, table.BehavioralHealthThreshold, table.BehavioralHealthRate, tax)).Build()
}

// EffectiveRate returns tax as a proportion of income, rounded to the nearest
// basis point.
//
// Zero for zero income, rather than an error. A taxpayer with no income has an
// effective rate of nothing, and every caller of this is rendering a percentage
// beside a marginal one.
func EffectiveRate(tax, income money.USCents) money.Rate {
	if income <= 0 {
		return 0
	}
	return money.Rate((int64(tax)*10000 + int64(income)/2) / int64(income))
}
