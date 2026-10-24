package oregontaxcalc

import (
	"errors"
	"fmt"

	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// Election is which deduction method a taxpayer has chosen, where they have.
type Election int

const (
	// ElectLarger takes whichever figure is bigger.
	ElectLarger Election = iota

	// ElectStandard takes the standard deduction whatever the itemised total is.
	ElectStandard

	// ElectItemise takes the itemised total whatever the standard deduction is.
	ElectItemise
)

// Conditions are the facts about a taxpayer that change the standard
// deduction.
type Conditions struct {
	// Aged65OrOlder and Blind each add one unit of the addition for age or
	// blindness. A taxpayer who is both gets it twice. Oregon measures age at
	// 1 January of the following year, so somebody turning 65 on New Year's Day
	// qualifies for the year before.
	Aged65OrOlder bool
	Blind         bool

	// SpouseAged65OrOlder and SpouseBlind add the same again on a joint
	// return. They are ignored for every other filing status.
	SpouseAged65OrOlder bool
	SpouseBlind         bool

	// SpouseItemises reports that a separately filing spouse itemised, which
	// makes this filer's standard deduction nothing at all. It is ignored for
	// every status but married filing separately.
	//
	// A rule with no federal counterpart in this form: the federal code makes
	// the filer itemise too, where Oregon leaves them able to take a standard
	// deduction of zero.
	SpouseItemises bool
}

// Residency answers whether the filer was an Oregon resident for the whole tax
// year.
//
// It exists so that the residency position can be answered rather than only
// reported. Form OR-40 is a full-year resident's return, so a caller who says
// otherwise gets an error rather than a number computed under the wrong form.
type Residency int

const (
	// ResidencyUnstated is the zero value. The calculators compute a full-year
	// resident's tax and return the position unanswered.
	ResidencyUnstated Residency = iota

	// FullYearResident answers the position and changes nothing else.
	FullYearResident

	// PartYearOrNonResident makes [TaxableIncome] refuse. Such a filer uses
	// Form OR-40-P or OR-40-N, which prorate the tax by the share of income
	// from Oregon sources and are not computed here.
	PartYearOrNonResident
)

// Adjustments are the differences between a federal return and an Oregon one,
// as Schedule OR-ASC totals them, plus the deduction the filer takes.
type Adjustments struct {
	// Additions is the Schedule OR-ASC Section A total: income Oregon taxes
	// that the federal return did not.
	Additions money.USCents

	// Subtractions is the Section B total, and does not include the federal tax
	// subtraction, which [FederalTaxSubtraction] computes because it is capped
	// by income.
	Subtractions money.USCents

	// FederalTaxSubtraction is what [FederalTaxSubtraction] returned. Passed in
	// rather than recomputed, so that a caller who has already worked it out
	// and shown it to a user cannot get a different figure here.
	FederalTaxSubtraction money.USCents

	// ItemisedDeductions is the Schedule OR-A total, or zero where nothing has
	// been totalled.
	ItemisedDeductions money.USCents

	// Election settles the standard-versus-itemised choice. The zero value
	// takes the larger figure and reports the position as unanswered.
	Election Election

	// Residency settles whether the filer was an Oregon resident all year. The
	// zero value reports the position as unanswered.
	Residency Residency
}

// Deduction is what was subtracted, and which method produced it.
type Deduction struct {
	// Amount is the deduction taken, and Itemised reports whether it is the
	// itemised total.
	Amount   money.USCents
	Itemised bool

	// Standard is the standard deduction including any addition for age or
	// blindness, whether or not it was taken.
	Standard money.USCents

	// SpouseItemises records that the standard deduction was forced to nothing
	// because a separately filing spouse itemised.
	SpouseItemises bool
}

// Income is Oregon taxable income and how it was reached.
type Income struct {
	// OregonAGI is federal adjusted gross income after the Schedule OR-ASC
	// additions and subtractions. Form OR-40 line 7.
	OregonAGI money.USCents

	// Deduction is the deduction taken and how.
	Deduction Deduction

	// FederalTaxSubtraction is what was subtracted for federal income tax.
	FederalTaxSubtraction money.USCents

	// Amount is Oregon taxable income, Form OR-40 line 19. This is what
	// [OrdinaryIncomeTax] wants.
	Amount money.USCents
}

// StandardDeduction returns the ORS 316.695(9) deduction with any addition for
// age or blindness.
func StandardDeduction(year int, status filing.Status, c Conditions) (working.Result[money.USCents], error) {
	table, err := TableFor(year, status)
	if err != nil {
		return working.Result[money.USCents]{}, err
	}
	amount, units, zeroed := standardFrom(table, status, c)

	var notes working.Notes
	notes.Rule(fmt.Sprintf("Or. Rev. Stat. § 316.695(9), as adjusted for %d", year), fmt.Sprintf("the standard deduction for a %s filer is %s", status, table.StandardDeduction))
	if zeroed {
		notes.Rule("Or. Rev. Stat. § 316.695(9)(c)", "the spouse on the other separate return itemised, so this filer's standard deduction is nothing")
		return working.NewResult(money.USCents(0)).With(notes).Build()
	}
	if units > 0 {
		notes.Rule("Or. Rev. Stat. § 316.695(9)(b)", fmt.Sprintf("%d addition(s) of %s for age or blindness bring it to %s",
			units, table.AdditionalAgedBlind, amount))
	}
	return working.NewResult(amount).With(notes).Build()
}

func standardFrom(table Table, status filing.Status, c Conditions) (amount money.USCents, units int, zeroed bool) {
	if status == filing.MarriedFilingSeparately && c.SpouseItemises {
		return 0, 0, true
	}
	if c.Aged65OrOlder {
		units++
	}
	if c.Blind {
		units++
	}
	if status == filing.MarriedFilingJointly || status == filing.SurvivingSpouse {
		if c.SpouseAged65OrOlder {
			units++
		}
		if c.SpouseBlind {
			units++
		}
	}
	return table.StandardDeduction + money.USCents(units)*table.AdditionalAgedBlind, units, false
}

// TaxableIncome works Oregon taxable income out from federal adjusted gross
// income.
//
// federalAGI is federal adjusted gross income — Form 1040 line 11, which is
// what Form OR-40 line 7 asks for — taken as a plain amount so that a caller
// with the figure from anywhere can use it.
//
// The result is Form OR-40 line 19, which [OrdinaryIncomeTax] takes.
//
// A negative result is returned as zero.
func TaxableIncome(year int, status filing.Status, federalAGI money.USCents, c Conditions, adj Adjustments) (working.Result[Income], error) {
	table, err := TableFor(year, status)
	if err != nil {
		return working.Result[Income]{}, err
	}
	if adj.Residency == PartYearOrNonResident {
		return working.Result[Income]{}, errors.New(
			"oregontaxcalc: a part-year resident files Form OR-40-P and a non-resident " +
				"Form OR-40-N, both of which prorate the tax by the share of income from " +
				"Oregon sources and are not computed here")
	}

	standard, units, zeroed := standardFrom(table, status, c)

	out := Income{
		OregonAGI:             federalAGI + adj.Additions - adj.Subtractions,
		FederalTaxSubtraction: adj.FederalTaxSubtraction,
	}
	out.Deduction = Deduction{Amount: standard, Standard: standard, SpouseItemises: zeroed}
	switch {
	case adj.Election == ElectItemise:
		out.Deduction.Amount, out.Deduction.Itemised = adj.ItemisedDeductions, true
	case adj.Election == ElectStandard:
		// Already the standard deduction.
	case adj.ItemisedDeductions > standard:
		out.Deduction.Amount, out.Deduction.Itemised = adj.ItemisedDeductions, true
	}

	out.Amount = out.OregonAGI - out.Deduction.Amount - out.FederalTaxSubtraction
	if out.Amount < 0 {
		out.Amount = 0
	}

	var notes working.Notes
	notes.Rule("Or. Rev. Stat. § 316.048", fmt.Sprintf("federal adjusted gross income of %s, plus %s of additions and less %s of subtractions, gives Oregon adjusted gross income of %s",
		federalAGI, adj.Additions, adj.Subtractions, out.OregonAGI))
	notes.Rule(fmt.Sprintf("Or. Rev. Stat. § 316.695(9), as adjusted for %d", table.Year), fmt.Sprintf("the standard deduction for a %s filer, after %d addition(s) for age or blindness, is %s",
		table.Status, units, standard))
	if adj.FederalTaxSubtraction > 0 {
		notes.Rule("Or. Rev. Stat. § 316.680(1)(b)", fmt.Sprintf("%s of federal income tax is subtracted", adj.FederalTaxSubtraction))
	}

	if adj.Election == ElectLarger {
		notes.Require(working.PositionDeductionMethod, "Oregon makes itemising an election of its own under ORS 316.695, and "+
			"does not bind a filer to what the federal return elected")
	}
	if adj.Residency == ResidencyUnstated {
		notes.Require(working.PositionResidency, "these figures are a full-year resident's, who files Form OR-40. A part-year "+
			"resident files Form OR-40-P and a non-resident Form OR-40-N, both of which "+
			"prorate the tax by the share of income from Oregon sources. Answer it with "+
			"Adjustments.Residency")
	}
	return working.NewResult(out).With(notes).Build()
}
