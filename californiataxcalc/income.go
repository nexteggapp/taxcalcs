package californiataxcalc

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
	// ElectLarger takes whichever figure is bigger. Right for almost everybody,
	// and the behaviour of a caller who has not thought about it.
	ElectLarger Election = iota

	// ElectStandard takes the standard deduction whatever the itemised total is.
	ElectStandard

	// ElectItemise takes the itemised total whatever the standard deduction is.
	ElectItemise
)

// Residency answers whether the filer was a California resident for the whole
// tax year.
//
// It exists so that the residency position can be answered rather than only
// reported. Form 540 is a full-year resident's return and these figures are a
// full-year resident's, so a caller who says otherwise gets an error rather
// than a number computed under the wrong form.
type Residency int

const (
	// ResidencyUnstated is the zero value. The calculators compute a full-year
	// resident's tax and return the position unanswered.
	ResidencyUnstated Residency = iota

	// FullYearResident answers the position. Nothing else changes; the figures
	// were already this filer's.
	FullYearResident

	// PartYearOrNonResident makes [TaxableIncome] refuse. Rev. & Tax. Code
	// § 17041(b) apportions such a filer's tax by the share of income from
	// California sources, on Form 540NR, which this package does not compute.
	PartYearOrNonResident
)

// Adjustments are the differences between a federal return and a California
// one, as Schedule CA (540) totals them.
//
// California conforms to the federal definition of gross income only as of a
// fixed date, so the two figures differ for anybody with tax-exempt interest
// from another state's bonds, a health savings account, or several dozen other
// items. Schedule CA is where a filer works the difference out; this struct is
// where they hand the answer over.
type Adjustments struct {
	// Subtractions is Schedule CA (540) Part I, line 27, column B: income
	// California does not tax that the federal return did. California treats
	// social security benefits this way, which is the case most filers meet.
	Subtractions money.USCents

	// Additions is Part I, line 27, column C: income California taxes that the
	// federal return did not.
	Additions money.USCents

	// ItemisedDeductions is the California itemised total from Schedule CA
	// Part II, or zero where nothing has been totalled.
	ItemisedDeductions money.USCents

	// Election settles the standard-versus-itemised choice. The zero value
	// takes the larger figure and reports the position as unanswered.
	Election Election

	// Residency settles whether the filer was a California resident all year.
	// The zero value reports the position as unanswered.
	Residency Residency
}

// Income is California taxable income and how it was reached.
type Income struct {
	// CaliforniaAGI is federal adjusted gross income after the Schedule CA
	// adjustments. Form 540 line 17.
	CaliforniaAGI money.USCents

	// Deduction is what was subtracted, and Itemised reports whether it was
	// the itemised total rather than the standard deduction.
	Deduction money.USCents
	Itemised  bool

	// Standard is the standard deduction, whether or not it was taken. A caller
	// showing its working renders both figures so the choice is checkable.
	Standard money.USCents

	// Amount is California taxable income, Form 540 line 19. This is what
	// [OrdinaryIncomeTax] and [BehavioralHealthServicesTax] want.
	Amount money.USCents
}

// TaxableIncome works California taxable income out from federal adjusted gross
// income.
//
// federalAGI is federal adjusted gross income — Form 1040 line 11b, which is
// the line Form 540 line 13 names — taken as a plain amount. This package does
// not import the federal calculators, so a caller with the figure from a
// spreadsheet, a prior-year return or another library can use it.
//
// The result is Form 540 line 19, which both [OrdinaryIncomeTax] and
// [BehavioralHealthServicesTax] take. Note that it includes any capital gain in
// full, because California taxes one at the ordinary rates.
//
// A negative result is returned as zero. Deductions exceeding income produce no
// taxable income rather than a negative amount, and the loss carryforward rules
// that would apply are not modelled here.
func TaxableIncome(year int, status filing.Status, federalAGI money.USCents, adj Adjustments) (working.Result[Income], error) {
	table, err := TableFor(year, status)
	if err != nil {
		return working.Result[Income]{}, err
	}
	if adj.Residency == PartYearOrNonResident {
		return working.Result[Income]{}, errors.New(
			"californiataxcalc: a part-year resident or non-resident files Form 540NR, " +
				"which apportions the tax by the share of income from California sources " +
				"and is not computed here")
	}

	out := Income{
		CaliforniaAGI: federalAGI - adj.Subtractions + adj.Additions,
		Standard:      table.StandardDeduction,
	}

	out.Deduction = out.Standard
	switch {
	case adj.Election == ElectItemise:
		out.Deduction, out.Itemised = adj.ItemisedDeductions, true
	case adj.Election == ElectStandard:
		// Already the standard deduction.
	case adj.ItemisedDeductions > out.Standard:
		out.Deduction, out.Itemised = adj.ItemisedDeductions, true
	}

	out.Amount = out.CaliforniaAGI - out.Deduction
	if out.Amount < 0 {
		out.Amount = 0
	}

	var notes working.Notes
	notes.Rule("Cal. Rev. & Tax. Code § 17072", fmt.Sprintf("federal adjusted gross income of %s, less %s of subtractions and plus %s of additions, gives California adjusted gross income of %s",
		federalAGI, adj.Subtractions, adj.Additions, out.CaliforniaAGI))
	notes.Rule(fmt.Sprintf("Cal. Rev. & Tax. Code § 17073.5, as adjusted for %d", table.Year), fmt.Sprintf("the California standard deduction for a %s filer is %s",
		table.Status, out.Standard))
	if adj.ItemisedDeductions > 0 {
		notes.Rule("Cal. Rev. & Tax. Code § 17073", fmt.Sprintf("itemised deductions of %s against a standard deduction of %s, so %s was taken",
			adj.ItemisedDeductions, out.Standard, out.Deduction))
	}

	if adj.Election == ElectLarger {
		notes.Require(working.PositionDeductionMethod, "California makes itemising an election of its own. Rev. & Tax. Code "+
			"§ 17073.5 does not bind a filer to what the federal return elected, and "+
			"the California standard deduction is small enough that itemising often "+
			"wins here when it lost federally")
	}
	if adj.Residency == ResidencyUnstated {
		notes.Require(working.PositionResidency, "these figures are a full-year resident's, who files Form 540. A part-year "+
			"resident or a non-resident files Form 540NR, which apportions the tax by the "+
			"share of income from California sources. Answer it with Adjustments.Residency")
	}
	return working.NewResult(out).With(notes).Build()
}

// Exemptions are the counts a filer enters on Form 540 lines 7 to 10.
//
// Counts rather than amounts, because the AGI limitation reduces the credit per
// exemption, so the arithmetic needs to know how many there are.
type Exemptions struct {
	// Personal is line 7: one for a single, separate or head of household
	// filer, two for a joint filer or surviving spouse.
	Personal int

	// Blind is line 8 and Senior is line 9, each one per qualifying person, so
	// each is 0, 1 or 2.
	Blind  int
	Senior int

	// Dependents is line 10, one per dependent, and carries a much larger
	// credit than the three above.
	Dependents int
}

// ExemptionCredit computes the Rev. & Tax. Code § 17054 credits, reduced by the
// adjusted gross income limitation where it applies.
//
// federalAGI decides the limitation, which is why it is wanted here as well as
// in [TaxableIncome]. Above the threshold for the filing status, each exemption
// loses six dollars for every whole or part step of AGI above it — a step being
// $2,500, or $1,250 on a separate return.
//
// The result is a credit against tax. A caller subtracts it from the figure
// [OrdinaryIncomeTax] returned, floors the result at zero, and only then adds
// any surcharge from [BehavioralHealthServicesTax].
func ExemptionCredit(year int, status filing.Status, federalAGI money.USCents, e Exemptions) (working.Result[money.USCents], error) {
	table, err := TableFor(year, status)
	if err != nil {
		return working.Result[money.USCents]{}, err
	}

	personalCount := money.USCents(e.Personal + e.Blind + e.Senior)
	dependentCount := money.USCents(e.Dependents)

	personal := personalCount * table.PersonalExemptionCredit
	dependent := dependentCount * table.DependentExemptionCredit

	var notes working.Notes
	notes.Rule(fmt.Sprintf("Cal. Rev. & Tax. Code § 17054, as adjusted for %d", table.Year), fmt.Sprintf("%d personal, blind and senior exemptions at %s and %d dependents at %s give %s before any limitation",
		e.Personal+e.Blind+e.Senior, table.PersonalExemptionCredit,
		e.Dependents, table.DependentExemptionCredit, personal+dependent))

	excess := federalAGI - table.ExemptionPhaseOutAGI
	if excess <= 0 {
		return working.NewResult(personal + dependent).With(notes).Build()
	}

	// The worksheet rounds the number of steps up, so a single dollar over the
	// threshold costs a whole step.
	steps := (excess + table.ExemptionPhaseOutStep - 1) / table.ExemptionPhaseOutStep
	perExemption := steps * table.ExemptionPhaseOutPerStep

	personal -= perExemption * personalCount
	if personal < 0 {
		personal = 0
	}
	dependent -= perExemption * dependentCount
	if dependent < 0 {
		dependent = 0
	}

	notes.Rule("Cal. Rev. & Tax. Code § 17054(f)", fmt.Sprintf("adjusted gross income of %s is %s above the %s threshold, which is %d step(s) of %s, so each exemption loses %s and the credit becomes %s",
		federalAGI, excess, table.ExemptionPhaseOutAGI, int64(steps),
		table.ExemptionPhaseOutStep, perExemption, personal+dependent))

	return working.NewResult(personal + dependent).With(notes).Build()
}
