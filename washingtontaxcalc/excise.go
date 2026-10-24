package washingtontaxcalc

import (
	"fmt"

	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// Deductions are what a filer subtracts from their Washington capital gains
// before the rates apply.
//
// The standard deduction is not here: it is the same for everybody and comes
// from the table. These are the ones that depend on what the filer did.
type Deductions struct {
	// CharitableDonations is qualified donations to Washington-directed
	// organisations. Only the part above the year's threshold is deductible,
	// and the deduction is capped, so giving away a million does not remove a
	// million of gain.
	CharitableDonations money.USCents

	// FamilyOwnedSmallBusiness is gain from the sale of all or substantially
	// all of a qualified family-owned small business, which RCW 82.87.070
	// deducts in full.
	FamilyOwnedSmallBusiness money.USCents
}

// Facts are the things about a filer, rather than about their figures, that the
// tax turns on.
//
// Both start false and each unanswered one comes back in
// [working.Result.Required]. They exist so that those positions can be answered
// rather than only reported: a caller who has done the work says so, and stops
// being told about it.
type Facts struct {
	// GainAllocated confirms that the caller has already allocated the gain to
	// Washington under RCW 82.87.100 and removed the classes RCW 82.87.050
	// exempts.
	//
	// This package cannot check it. Allocation of gain on intangible property
	// turns on where the individual was domiciled when the sale settled, which
	// is a fact about the seller that no figure carries.
	GainAllocated bool

	// LotsIdentified confirms that the lots each sale consumed are settled, so
	// that basis and holding period are known and the gain really is long-term.
	LotsIdentified bool
}

// ExciseTax is the tax on a year's Washington capital gains, and the working.
type ExciseTaxResult struct {
	// WashingtonCapitalGains is what the caller supplied, before deductions.
	WashingtonCapitalGains money.USCents

	// StandardDeduction, CharitableDeduction and FamilyBusinessDeduction are
	// what was actually subtracted, which for the charitable one is not what
	// was given.
	StandardDeduction       money.USCents
	CharitableDeduction     money.USCents
	FamilyBusinessDeduction money.USCents

	// Taxable is the gain the rates applied to. Zero where the deductions
	// covered everything, which is the ordinary case.
	Taxable money.USCents

	// AtBaseRate and AtSurchargeRate split Taxable at the surcharge threshold.
	// The surcharge is on top of the base rate rather than instead of it, so
	// AtSurchargeRate is taxed at both and appears in both figures.
	AtBaseRate      money.USCents
	AtSurchargeRate money.USCents

	// BaseTax and SurchargeTax are what each rate produced, and Total is their
	// sum.
	BaseTax      money.USCents
	SurchargeTax money.USCents
	Total        money.USCents
}

// ExciseTax computes the chapter 82.87 RCW excise tax on a year's Washington
// capital gains.
//
// washingtonCapitalGains is long-term capital gain allocated to Washington and
// with the exempt classes already removed. That is a narrower figure than a
// federal Schedule D total and this package cannot derive it: real estate,
// retirement accounts, timber, commercial fishing privileges and depreciable
// business assets are all exempt, and gain is allocated to Washington by where
// the seller was domiciled rather than by where the asset was. The allocation
// question comes back in [working.Result.Required].
//
// A loss, or a gain below the standard deduction, produces no tax and no
// filing obligation.
func ExciseTax(year int, washingtonCapitalGains money.USCents, d Deductions, f Facts) (working.Result[ExciseTaxResult], error) {
	table, err := TableFor(year)
	if err != nil {
		return working.Result[ExciseTaxResult]{}, err
	}

	out := ExciseTaxResult{
		WashingtonCapitalGains: washingtonCapitalGains,
		StandardDeduction:      table.StandardDeduction,
	}

	// Only donations above the threshold count, and only up to the cap.
	if over := d.CharitableDonations - table.CharitableDeductionThreshold; over > 0 {
		out.CharitableDeduction = over
		if out.CharitableDeduction > table.CharitableDeductionCap {
			out.CharitableDeduction = table.CharitableDeductionCap
		}
	}
	out.FamilyBusinessDeduction = d.FamilyOwnedSmallBusiness

	out.Taxable = washingtonCapitalGains -
		out.StandardDeduction - out.CharitableDeduction - out.FamilyBusinessDeduction
	if out.Taxable < 0 {
		out.Taxable = 0
	}

	var positions working.Notes
	if !f.GainAllocated {
		positions.Require(working.PositionCapitalAssetSitus, "RCW 82.87.100 allocates the gain on intangible property to Washington by "+
			"where the individual was domiciled when the sale settled, not by where the "+
			"asset or the broker was. Whether a gain is Washington's at all is a fact "+
			"about the seller that these figures do not carry. Answer it with "+
			"Facts.GainAllocated")
	}
	if !f.LotsIdentified {
		positions.Require(working.PositionLotSelection, "which lots a sale consumed decides its basis and whether the gain is "+
			"long-term at all, and RCW 82.87.040 reaches only long-term gain. Answer it "+
			"with Facts.LotsIdentified")
	}

	if out.Taxable == 0 {
		return working.NewResult(out).
			Rule(fmt.Sprintf("RCW 82.87.060(1), as adjusted for %d", table.Year),
				fmt.Sprintf("%s of Washington capital gains is not above the deductions of %s, so no tax is due and no return is required",
					washingtonCapitalGains,
					out.StandardDeduction+out.CharitableDeduction+out.FamilyBusinessDeduction)).
			With(positions).
			Build()
	}

	out.AtBaseRate = out.Taxable
	if over := out.Taxable - table.SurchargeThreshold; over > 0 {
		out.AtSurchargeRate = over
	}

	out.BaseTax = table.BaseRate.Of(out.AtBaseRate)
	out.SurchargeTax = table.SurchargeRate.Of(out.AtSurchargeRate)
	out.Total = out.BaseTax + out.SurchargeTax

	var notes working.Notes
	notes.Rule(fmt.Sprintf("RCW 82.87.060(1), as adjusted for %d", table.Year), fmt.Sprintf("%s of Washington capital gains, less a standard deduction of %s and %s of other deductions, leaves %s taxable",
		washingtonCapitalGains, out.StandardDeduction,
		out.CharitableDeduction+out.FamilyBusinessDeduction, out.Taxable))
	notes.Rule("RCW 82.87.040(1)(a)", fmt.Sprintf("%s on all %s of taxable gain gives %s", table.BaseRate, out.AtBaseRate, out.BaseTax))
	if out.AtSurchargeRate > 0 {
		notes.Rule("RCW 82.87.040(1)(b), as enacted by ESSB 5813, ch. 421, Laws of 2025", fmt.Sprintf("a further %s on the %s above %s gives %s, so the top of the gain meets %s in total",
			table.SurchargeRate, out.AtSurchargeRate, table.SurchargeThreshold,
			out.SurchargeTax, table.BaseRate+table.SurchargeRate))
	}

	return working.NewResult(out).With(notes).With(positions).Build()
}
