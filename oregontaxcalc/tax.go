package oregontaxcalc

import (
	"fmt"

	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// Band is one bracket of the rate chart and what the taxpayer's income put in
// it.
//
// Returned even for bands the income never reached, with Income and Tax zero,
// because a caller showing its working renders the whole ladder.
type Band struct {
	Rate money.Rate

	// From and To bound the band. To is zero on the top band, which has no
	// upper bound.
	From, To money.USCents

	// Income is how much of the taxpayer's Oregon taxable income fell in this
	// band. Tax is the rate applied to it and then rounded to a whole dollar,
	// which is what Oregon's own published figures do. See [roundToDollar].
	Income money.USCents
	Tax    money.USCents
}

// Ladder is the rate chart applied to one taxpayer's Oregon taxable income.
type Ladder struct {
	Bands []Band

	// Total is the sum of every band's rounded tax, so it is always a whole
	// number of dollars.
	Total money.USCents

	// Marginal is the rate on the next dollar of income.
	Marginal money.Rate

	// RoomInBracket is how much more income fits before the next band starts.
	// Zero on the top band.
	RoomInBracket money.USCents
}

// roundToDollar rounds to whole dollars, halves away from zero.
//
// Oregon applies this to each bracket rather than to the total, which is not a
// detail. A joint filer at $50,000: the exact bracket amounts are $418.00,
// $904.50 and $2,432.50, summing to $3,755.00. Oregon's published rate chart
// says $3,756, which is what rounding each one first gives. Rounding only the
// total would give $3,755 and be wrong against every figure Oregon prints.
func roundToDollar(a money.USCents) money.USCents {
	if a < 0 {
		return -((-a + 50) / 100 * 100)
	}
	return (a + 50) / 100 * 100
}

// OrdinaryIncomeTax applies the ORS 316.037 rate chart to Oregon taxable
// income.
//
// taxable is income after the Oregon deduction and the federal tax subtraction,
// from [TaxableIncome]. It includes any capital gain in full, because Oregon
// has no preferential rate for one.
//
// This is the tax before credits. The exemption credit from [ExemptionCredit]
// is subtracted from it.
//
// A negative amount is treated as zero.
func OrdinaryIncomeTax(year int, status filing.Status, taxable money.USCents) (working.Result[Ladder], error) {
	table, err := TableFor(year, status)
	if err != nil {
		return working.Result[Ladder]{}, err
	}
	return ladderFrom(table, taxable)
}

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

		top := taxable
		if band.To != 0 && top > band.To {
			top = band.To
		}
		if top > band.From {
			band.Income = top - band.From
			band.Tax = roundToDollar(bracket.Rate.Of(band.Income))
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

	return working.NewResult(ladder).Rule(fmt.Sprintf("Or. Rev. Stat. § 316.037, as adjusted for %d", table.Year), fmt.Sprintf("%s of Oregon taxable income through the %s chart for a %s filer gives %s, each bracket rounded to a whole dollar",
		taxable, table.Ordinary[len(table.Ordinary)-1].Rate, table.Status, ladder.Total)).Build()
}

// FederalTaxSubtraction returns how much federal income tax an Oregon filer may
// subtract.
//
// federalTaxLiability starts from Form 1040 line 22 and is reduced by the
// refundable credits other than the earned income credit, which is what line 10
// of the worksheet in the Form OR-40 instructions works out. It reaches Form
// OR-40 at line 10.
//
// federalAGI is Form 1040 line 11, and decides the cap, which falls in five
// steps and reaches nothing.
//
// Both are plain amounts. This package does not import the federal
// calculators, so the figures can come from anywhere.
//
// Oregon is unusual in allowing this at all, and it is the single largest
// subtraction on most Oregon returns.
func FederalTaxSubtraction(year int, status filing.Status, federalTaxLiability, federalAGI money.USCents) (working.Result[money.USCents], error) {
	table, err := TableFor(year, status)
	if err != nil {
		return working.Result[money.USCents]{}, err
	}
	if federalTaxLiability < 0 {
		federalTaxLiability = 0
	}

	// The last step whose floor the income reaches.
	maximum := table.FederalTaxSubtraction[0].Maximum
	for _, step := range table.FederalTaxSubtraction {
		if federalAGI >= step.AtLeast {
			maximum = step.Maximum
		}
	}

	allowed := federalTaxLiability
	if allowed > maximum {
		allowed = maximum
	}

	return working.NewResult(allowed).Rule(fmt.Sprintf("Or. Rev. Stat. § 316.680(1)(b), as adjusted for %d", table.Year), fmt.Sprintf("federal tax of %s against a maximum of %s at %s of adjusted gross income gives a subtraction of %s",
		federalTaxLiability, maximum, federalAGI, allowed)).Build()
}

// ExemptionCredit returns the ORS 316.085 credit.
//
// exemptions is the number claimed, which is the filer, a spouse on a joint
// return, and each dependent.
//
// The credit disappears entirely above the income limit rather than tapering,
// so a dollar of extra income can cost a family several hundred. That cliff is
// Oregon's rule and not a simplification here.
func ExemptionCredit(year int, status filing.Status, federalAGI money.USCents, exemptions int) (working.Result[money.USCents], error) {
	table, err := TableFor(year, status)
	if err != nil {
		return working.Result[money.USCents]{}, err
	}
	if exemptions < 0 {
		exemptions = 0
	}

	if federalAGI > table.ExemptionCreditAGILimit {
		return working.NewResult(money.USCents(0)).Rule("Or. Rev. Stat. § 316.085(4)", fmt.Sprintf("adjusted gross income of %s is above %s, so the credit is nothing for all %d exemption(s)",
			federalAGI, table.ExemptionCreditAGILimit, exemptions)).Build()
	}

	credit := money.USCents(exemptions) * table.ExemptionCredit
	return working.NewResult(credit).Rule(fmt.Sprintf("Or. Rev. Stat. § 316.085, as adjusted for %d", table.Year), fmt.Sprintf("%d exemption(s) at %s give %s", exemptions, table.ExemptionCredit, credit)).Build()
}

// EffectiveRate returns tax as a proportion of income, rounded to the nearest
// basis point.
func EffectiveRate(tax, income money.USCents) money.Rate {
	if income <= 0 {
		return 0
	}
	return money.Rate((int64(tax)*10000 + int64(income)/2) / int64(income))
}
