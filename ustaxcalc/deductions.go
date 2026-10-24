package ustaxcalc

import (
	"fmt"

	"github.com/nexteggapp/taxcalcs/working"
)

// Deduction is what is subtracted from adjusted gross income, and which method
// produced it.
type Deduction struct {
	// Amount is the deduction taken.
	Amount USCents

	// Itemised reports whether Amount is the itemised total rather than the
	// standard deduction.
	Itemised bool

	// Standard is the standard deduction including any addition for age or
	// blindness, whether or not it was the one taken. A caller showing its
	// working renders both figures so the choice is checkable.
	Standard USCents
}

// Conditions are the facts about a taxpayer that change the standard
// deduction.
type Conditions struct {
	// Aged65OrOlder and Blind each add one unit of the § 63(f) amount. A
	// taxpayer who is both gets it twice.
	Aged65OrOlder bool
	Blind         bool

	// SpouseAged65OrOlder and SpouseBlind add the same again on a joint
	// return. They are ignored for every other filing status.
	SpouseAged65OrOlder bool
	SpouseBlind         bool
}

// StandardDeduction returns the § 63(c) deduction with any § 63(f) addition for
// age or blindness.
func StandardDeduction(year int, status FilingStatus, c Conditions) (Result[USCents], error) {
	table, err := TableFor(year, status)
	if err != nil {
		return Result[USCents]{}, err
	}
	amount, units := standardFrom(table, status, c)

	b := working.NewResult(amount).
		Rule(fmt.Sprintf("26 U.S.C. § 63(c)(2), as adjusted for %d", year),
			fmt.Sprintf("the standard deduction for a %s filer is %s", status, table.StandardDeduction))
	if units > 0 {
		b.Rule("26 U.S.C. § 63(f)", fmt.Sprintf("%d addition(s) of %s for age or blindness bring it to %s",
			units, table.AdditionalAgedBlind, amount))
	}
	return b.Build()
}

func standardFrom(table Table, status FilingStatus, c Conditions) (amount USCents, units int) {
	if c.Aged65OrOlder {
		units++
	}
	if c.Blind {
		units++
	}
	if status == MarriedFilingJointly || status == SurvivingSpouse {
		if c.SpouseAged65OrOlder {
			units++
		}
		if c.SpouseBlind {
			units++
		}
	}
	return table.StandardDeduction + USCents(units)*table.AdditionalAgedBlind, units
}

// Election is which deduction method a taxpayer has chosen, where they have.
type Election int

const (
	// ElectLarger takes whichever figure is bigger. Right for almost everybody,
	// and the behaviour of a caller who has not thought about it.
	ElectLarger Election = iota

	// ElectStandard takes the standard deduction whatever the itemised total is.
	ElectStandard

	// ElectItemise takes the itemised total whatever the standard deduction is.
	//
	// A taxpayer filing separately must itemise if their spouse does, however
	// much smaller their own total is, so this is not merely the irrational
	// case.
	ElectItemise
)

// DeductionTaken chooses between the standard deduction and an itemised total.
//
// itemisedTotal is what the taxpayer's itemised deductions come to, or zero
// where nothing has been totalled.
//
// election settles the § 63(e) choice. Pass [ElectLarger] and the bigger figure
// wins, which is what a taxpayer choosing freely does. The position is still
// reported in that case, because the choice was made by this function rather
// than by the taxpayer. Passing either of the others answers it, and the
// position is not returned.
func DeductionTaken(year int, status FilingStatus, c Conditions, itemisedTotal USCents, election Election) (Result[Deduction], error) {
	table, err := TableFor(year, status)
	if err != nil {
		return Result[Deduction]{}, err
	}
	standard, units := standardFrom(table, status, c)

	taken := Deduction{Amount: standard, Standard: standard}
	switch {
	case election == ElectItemise:
		taken.Amount, taken.Itemised = itemisedTotal, true
	case election == ElectStandard:
		// Already the standard deduction.
	case itemisedTotal > standard:
		taken.Amount, taken.Itemised = itemisedTotal, true
	}

	b := working.NewResult(taken).
		Rule(fmt.Sprintf("26 U.S.C. § 63(c), as adjusted for %d", year),
			fmt.Sprintf("the standard deduction is %s after %d addition(s) for age or blindness", standard, units))
	if itemisedTotal > 0 {
		b.Rule("26 U.S.C. § 63(e)", fmt.Sprintf("itemised deductions of %s against a standard deduction of %s, so %s was taken",
			itemisedTotal, standard, taken.Amount))
	}
	if election == ElectLarger {
		b.Require(PositionDeductionMethod, "§ 63(e) makes itemising an election rather than an outcome. A taxpayer "+
			"may itemise a smaller total, and one filing separately must itemise if "+
			"their spouse does")
	}
	return b.Build()
}
