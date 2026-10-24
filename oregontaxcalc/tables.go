package oregontaxcalc

import (
	"errors"
	"fmt"

	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
)

// ErrNoTable reports that no published table is held for a tax year.
//
// Calculators return this rather than applying an adjacent year's figures.
// Oregon indexes its brackets, its standard deduction and its exemption credit
// every year under ORS 316.011, so last year's table is wrong in a way that
// looks right.
var ErrNoTable = errors.New("oregontaxcalc: no published rate table for this tax year")

// Bracket is one band of the rate chart: the taxable income it starts above,
// and the rate on income in it.
type Bracket struct {
	// Over is the taxable income this bracket begins above. The lowest bracket
	// has Over zero.
	Over money.USCents
	Rate money.Rate
}

// SubtractionStep is one row of the Table 4 phase-out: the federal adjusted
// gross income it applies from, and the largest federal tax subtraction
// allowed there.
type SubtractionStep struct {
	// AtLeast is the federal adjusted gross income this row starts at. The
	// first row starts at zero.
	AtLeast money.USCents

	// Maximum is the largest subtraction allowed. Zero on the last row, where
	// the subtraction is gone entirely.
	Maximum money.USCents
}

// Table is every published figure for one tax year and filing status.
type Table struct {
	Year   int
	Status filing.Status

	// Ordinary is the ORS 316.037 rate chart, lowest bracket first. Chart S for
	// a single or separate filer, Chart J for everybody else.
	Ordinary []Bracket

	// StandardDeduction is the ORS 316.695(9) amount before any addition for
	// age or blindness.
	//
	// A separate filer whose spouse itemises gets nothing at all, which is not
	// in this figure because it depends on what the spouse did. See
	// [Deduction.SpouseItemises].
	StandardDeduction money.USCents

	// AdditionalAgedBlind is the addition per qualifying condition. Oregon
	// gives an unmarried filer more, the same way the federal rules do, so this
	// differs by status rather than being one figure.
	AdditionalAgedBlind money.USCents

	// ExemptionCredit is the ORS 316.085 credit per exemption, and
	// ExemptionCreditAGILimit is the federal adjusted gross income above which
	// it is nothing at all.
	//
	// A cliff rather than a taper: a dollar over the limit costs the whole
	// credit for every exemption.
	ExemptionCredit         money.USCents
	ExemptionCreditAGILimit money.USCents

	// FederalTaxSubtraction is the ORS 316.680(1)(b) phase-out, lowest income
	// first. Oregon is one of the few states that lets a filer subtract part of
	// their federal income tax.
	FederalTaxSubtraction []SubtractionStep
}

// tables holds every year and status this build carries.
var tables = map[int]map[filing.Status]Table{}

// register records a year's tables. Each year's file calls it from an init.
func register(year int, byStatus map[filing.Status]Table) {
	tables[year] = byStatus
}

// TableFor returns the published figures for a tax year and filing status.
//
// It reports [ErrNoTable] for a year this build does not carry.
func TableFor(year int, status filing.Status) (Table, error) {
	if status == filing.Unspecified {
		return Table{}, errors.New("oregontaxcalc: no filing status given")
	}
	byStatus, ok := tables[year]
	if !ok {
		return Table{}, fmt.Errorf("%w: %d", ErrNoTable, year)
	}
	t, ok := byStatus[status]
	if !ok {
		return Table{}, fmt.Errorf("%w: %d has no table for %s", ErrNoTable, year, status)
	}
	return t, nil
}

// Years returns every tax year this build carries a table for, unordered.
func Years() []int {
	years := make([]int, 0, len(tables))
	for y := range tables {
		years = append(years, y)
	}
	return years
}
