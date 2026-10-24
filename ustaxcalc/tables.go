package ustaxcalc

import (
	"errors"
	"fmt"
)

// ErrNoTable reports that no published table is held for a tax year.
//
// Calculators return this rather than applying an adjacent year's figures. A
// table that is right in December is wrong in January, and a silent fallback
// produces a confident number under the wrong law.
var ErrNoTable = errors.New("ustaxcalc: no published rate table for this tax year")

// Bracket is one band of the rate table: the threshold it starts above, and the
// rate on income in it.
type Bracket struct {
	// Over is the taxable income this bracket begins above. The lowest bracket
	// has Over zero.
	Over USCents
	Rate Rate
}

// Table is every published figure for one tax year and filing status.
//
// One struct rather than a lookup per figure, because they are published
// together in one revenue procedure and revised together each year.
type Table struct {
	Year   int
	Status FilingStatus

	// Ordinary is the § 1(j)(2) rate table, lowest bracket first.
	Ordinary []Bracket

	// StandardDeduction is the § 63(c)(2) amount.
	StandardDeduction USCents

	// AdditionalAgedBlind is the § 63(f) amount, per qualifying condition. A
	// taxpayer who is both 65 or older and blind gets it twice.
	AdditionalAgedBlind USCents

	// MaxZeroRateAmount and Max15RateAmount are the § 1(j)(5)(B) thresholds.
	// Long-term gains are taxed at zero up to the first, 15% up to the second,
	// and 20% above it. Both are measured against total taxable income rather
	// than against the gain.
	MaxZeroRateAmount USCents
	Max15RateAmount   USCents

	// SocialSecurityWageBase caps the old age, survivors and disability
	// insurance part of self-employment tax. Published by the Social Security
	// Administration rather than by the IRS, so it does not come from the same
	// revenue procedure as the rest of this table.
	SocialSecurityWageBase USCents

	// AdditionalMedicareThreshold is the § 1401(b)(2) figure over which the
	// extra 0.9% applies. Statutory and not adjusted for inflation.
	AdditionalMedicareThreshold USCents

	// PriorYearAGIStep is the § 6654(d)(1)(C)(i) prior-year adjusted gross
	// income above which the safe harbour rises from 100% to 110% of the prior
	// year's tax. Statutory and not adjusted for inflation.
	PriorYearAGIStep USCents
}

// tables holds every year and status this build carries.
var tables = map[int]map[FilingStatus]Table{}

// register records a year's tables. Each year's file calls it from an init.
func register(year int, byStatus map[FilingStatus]Table) {
	tables[year] = byStatus
}

// TableFor returns the published figures for a tax year and filing status.
//
// It reports [ErrNoTable] for a year this build does not carry. Adding a year
// is a deliberate commit, which is what stops a stale build applying last
// year's law to this year's income.
func TableFor(year int, status FilingStatus) (Table, error) {
	if status == FilingStatusUnspecified {
		return Table{}, errors.New("ustaxcalc: no filing status given")
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
