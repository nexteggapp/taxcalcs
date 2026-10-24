package washingtontaxcalc

import (
	"errors"
	"fmt"

	"github.com/nexteggapp/taxcalcs/money"
)

// ErrNoTable reports that no published table is held for a tax year.
//
// Calculators return this rather than applying an adjacent year's figures. The
// Department of Revenue adjusts the standard deduction for inflation every
// year under RCW 82.87.150, so last year's figure is wrong in a way that looks
// right.
var ErrNoTable = errors.New("washingtontaxcalc: no published figures for this tax year")

// Table is every published figure for one tax year.
//
// Not keyed by filing status, unlike every other jurisdiction in this module.
// RCW 82.87.060(1) gives one deduction "per individual, married couple, or
// domestic partnership", so a couple filing jointly get the same amount as one
// person rather than twice it. Nothing else here varies by status either.
type Table struct {
	Year int

	// StandardDeduction is the RCW 82.87.060(1) amount, which the statute sets
	// at $250,000 and RCW 82.87.150 adjusts for inflation each October. Gains
	// below it are not taxed and no return is due.
	StandardDeduction money.USCents

	// BaseRate is the RCW 82.87.040(1)(a) rate on all taxable Washington capital
	// gains.
	BaseRate money.Rate

	// SurchargeThreshold is the gain above which RCW 82.87.040(1)(b) adds
	// SurchargeRate on top of BaseRate. Added by ESSB 5813, chapter 421 of the
	// Laws of 2025, with effect from 1 January 2025.
	//
	// Measured against taxable gains after the standard deduction, not against
	// the gross gain.
	SurchargeThreshold money.USCents
	SurchargeRate      money.Rate

	// CharitableDeductionThreshold is the amount of qualified charitable
	// donations a filer must exceed before any of them are deductible, and
	// CharitableDeductionCap is the most that may be deducted however much was
	// given.
	CharitableDeductionThreshold money.USCents
	CharitableDeductionCap       money.USCents
}

// tables holds every year this build carries.
var tables = map[int]Table{}

// register records a year's figures. Each year's file calls it from an init.
func register(t Table) {
	tables[t.Year] = t
}

// TableFor returns the published figures for a tax year.
//
// It reports [ErrNoTable] for a year this build does not carry.
func TableFor(year int) (Table, error) {
	t, ok := tables[year]
	if !ok {
		return Table{}, fmt.Errorf("%w: %d", ErrNoTable, year)
	}
	return t, nil
}

// Years returns every tax year this build carries figures for, unordered.
func Years() []int {
	years := make([]int, 0, len(tables))
	for y := range tables {
		years = append(years, y)
	}
	return years
}
