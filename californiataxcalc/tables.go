package californiataxcalc

import (
	"errors"
	"fmt"

	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
)

// ErrNoTable reports that no published table is held for a tax year.
//
// Calculators return this rather than applying an adjacent year's figures.
// California indexes its brackets, its standard deduction and its exemption
// credits for inflation every year under Rev. & Tax. Code § 17041(h), so last
// year's table is wrong in a way that looks right.
var ErrNoTable = errors.New("californiataxcalc: no published rate table for this tax year")

// Bracket is one band of a rate schedule: the taxable income it starts above,
// and the rate on income in it.
type Bracket struct {
	// Over is the taxable income this bracket begins above. The lowest bracket
	// has Over zero.
	Over money.USCents
	Rate money.Rate

	// BaseTax is the tax on all income below [Over], as the Franchise Tax
	// Board prints it in the middle column of Schedule X, Y and Z. Held so the
	// test can check this library's ladder against the published figure rather
	// than against itself.
	BaseTax money.USCents
}

// Table is every published figure for one tax year and filing status.
//
// One struct rather than a lookup per figure, because the Franchise Tax Board
// publishes them together in one booklet and revises them together each year.
type Table struct {
	Year   int
	Status filing.Status

	// Ordinary is the Rev. & Tax. Code § 17041 rate schedule, lowest bracket
	// first. Schedule X for a single or separate filer, Schedule Y for a joint
	// filer or surviving spouse, Schedule Z for a head of household.
	//
	// Every kind of income is taxed by this one ladder. See the package
	// comment on why there is no separate schedule for capital gain.
	Ordinary []Bracket

	// StandardDeduction is the Rev. & Tax. Code § 17073.5 amount. It is much
	// smaller than the federal one, which is why a filer who takes the federal
	// standard deduction may still itemise in California.
	StandardDeduction money.USCents

	// PersonalExemptionCredit is the Rev. & Tax. Code § 17054 credit, per
	// exemption, for the filer, a spouse, and each additional claim for age or
	// blindness. A credit against tax rather than a deduction from income,
	// which is why it cannot be folded into [TaxableIncome].
	PersonalExemptionCredit money.USCents

	// DependentExemptionCredit is the § 17054(d) credit for each dependent. It
	// is much larger than the personal credit.
	DependentExemptionCredit money.USCents

	// ExemptionPhaseOutAGI is the federal adjusted gross income above which the
	// exemption credits are reduced, and ExemptionPhaseOutStep is the size of
	// the step the reduction counts.
	ExemptionPhaseOutAGI  money.USCents
	ExemptionPhaseOutStep money.USCents

	// ExemptionPhaseOutPerStep is the reduction per exemption for each whole or
	// part step of AGI above the threshold.
	ExemptionPhaseOutPerStep money.USCents

	// BehavioralHealthThreshold is the taxable income above which the
	// Rev. & Tax. Code § 17043 surcharge applies, and BehavioralHealthRate is
	// the rate on the excess.
	//
	// Named the Mental Health Services Tax until Proposition 1 renamed it for
	// taxable years beginning on or after 1 January 2025.
	BehavioralHealthThreshold money.USCents
	BehavioralHealthRate      money.Rate
}

// tables holds every year and status this build carries.
var tables = map[int]map[filing.Status]Table{}

// register records a year's tables. Each year's file calls it from an init.
func register(year int, byStatus map[filing.Status]Table) {
	tables[year] = byStatus
}

// TableFor returns the published figures for a tax year and filing status.
//
// It reports [ErrNoTable] for a year this build does not carry. Adding a year
// is a deliberate commit, which is what stops a stale build applying last
// year's law to this year's income.
func TableFor(year int, status filing.Status) (Table, error) {
	if status == filing.Unspecified {
		return Table{}, errors.New("californiataxcalc: no filing status given")
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
