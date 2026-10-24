// Package payroll is how an employer withholds from a paycheck: federal
// income tax by Publication 15-T's percentage method, supplemental wages,
// Social Security and Medicare tax, and the workplace plan limits.
//
// The income tax brackets and standard deduction are ustaxcalc's. What this
// package adds is the payroll rules that read them. Each constant names its
// authority. The Publication 15-T construction is read from the 2025
// publication's Worksheet 1A; the 2026 publication was not checked against
// it, and these figures belong in ustaxcalc with their primary source once it
// is.
package payroll

import (
	"errors"
	"fmt"

	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/ustaxcalc"
	"github.com/nexteggapp/taxcalcs/working"
)

const (
	// SocialSecurityRate is the employee's old-age, survivors and disability
	// insurance tax, 26 U.S.C. § 3101(a).
	SocialSecurityRate = money.Rate(620)

	// MedicareRate is the employee's hospital insurance tax, § 3101(b)(1).
	MedicareRate = money.Rate(145)

	// AdditionalMedicareRate is the extra hospital insurance tax,
	// § 3101(b)(2).
	AdditionalMedicareRate = money.Rate(90)

	// AdditionalMedicareWithholding is the wages from one employer over which
	// the employer withholds the extra tax, whatever the filing status,
	// § 3102(f)(1).
	AdditionalMedicareWithholding = 200_000 * money.Dollar

	// SupplementalRate is the optional flat rate on supplemental wages,
	// 26 C.F.R. § 31.3402(g)-1(a)(7)(iii)(F).
	SupplementalRate = money.Rate(2200)

	// MandatorySupplementalRate applies to supplemental wages from one
	// employer over MandatoryThreshold in the year, § 31.3402(g)-1(a)(2):
	// the highest rate in § 1.
	MandatorySupplementalRate = money.Rate(3700)

	// MandatoryThreshold is $1 million of supplemental wages in the year,
	// § 31.3402(g)-1(a)(2).
	MandatoryThreshold = 1_000_000 * money.Dollar

	// RothCatchUpWages is last year's Social Security wages from an employer
	// over which a catch-up contribution must be Roth, § 414(v)(7), as
	// adjusted for 2026 by Notice 2025-67.
	RothCatchUpWages = 150_000 * money.Dollar
)

// Frequency is how often a job pays.
type Frequency int

const (
	// FrequencyUnspecified is not a frequency.
	FrequencyUnspecified Frequency = iota
	// Weekly pays 52 times a year.
	Weekly
	// Biweekly pays 26 times a year.
	Biweekly
	// SemiMonthly pays 24 times a year.
	SemiMonthly
	// Monthly pays 12 times a year.
	Monthly
)

// Periods is the number of pay periods a year, Publication 15-T's Table 3.
func (f Frequency) Periods() int {
	switch f {
	case Weekly:
		return 52
	case Biweekly:
		return 26
	case SemiMonthly:
		return 24
	case Monthly:
		return 12
	}
	return 0
}

// W4 is what Worksheet 1A reads from a Form W-4.
type W4 struct {
	status       filing.Status
	multipleJobs bool
	dependents   money.USCents
	otherIncome  money.USCents
	deductions   money.USCents
	extra        money.USCents
	exempt       bool
}

// Status is Step 1(c).
func (w W4) Status() filing.Status { return w.status }

// MultipleJobs is Step 2's checkbox.
func (w W4) MultipleJobs() bool { return w.multipleJobs }

// W4Builder builds a [W4].
type W4Builder struct{ w W4 }

// NewW4 starts a W-4 with the filing status of Step 1(c). A W-4 the
// employee never gave is this alone, as Publication 15-T treats one.
func NewW4(status filing.Status) *W4Builder { return &W4Builder{w: W4{status: status}} }

// MultipleJobs checks Step 2.
func (b *W4Builder) MultipleJobs() *W4Builder { b.w.multipleJobs = true; return b }

// Dependents sets Step 3.
func (b *W4Builder) Dependents(a money.USCents) *W4Builder { b.w.dependents = a; return b }

// OtherIncome sets Step 4(a).
func (b *W4Builder) OtherIncome(a money.USCents) *W4Builder { b.w.otherIncome = a; return b }

// Deductions sets Step 4(b).
func (b *W4Builder) Deductions(a money.USCents) *W4Builder { b.w.deductions = a; return b }

// Extra sets Step 4(c), extra withholding each pay period.
func (b *W4Builder) Extra(a money.USCents) *W4Builder { b.w.extra = a; return b }

// Exempt checks the exemption box below Step 4(c).
func (b *W4Builder) Exempt() *W4Builder { b.w.exempt = true; return b }

// Build refuses a status Publication 15-T has no table for, and negative
// amounts.
func (b *W4Builder) Build() (W4, error) {
	if b.w.status == filing.Unspecified {
		return W4{}, errors.New("payroll: a W-4 needs a filing status")
	}
	for _, a := range []money.USCents{b.w.dependents, b.w.otherIncome, b.w.deductions, b.w.extra} {
		if a < 0 {
			return W4{}, fmt.Errorf("payroll: a W-4 amount of %v is negative", a)
		}
	}
	return b.w, nil
}

// line1g is Worksheet 1A line 1g: the deduction the standard tables build
// in when Step 2 is not checked.
func line1g(s filing.Status) money.USCents {
	if s == filing.MarriedFilingJointly {
		return 12_900 * money.Dollar
	}
	return 8_600 * money.Dollar
}

// tableStatus is the Publication 15-T table a W-4 status reads: married
// filing jointly; single or married filing separately; head of household.
func tableStatus(s filing.Status) filing.Status {
	switch s {
	case filing.MarriedFilingJointly, filing.SurvivingSpouse:
		return filing.MarriedFilingJointly
	case filing.HeadOfHousehold:
		return filing.HeadOfHousehold
	}
	return filing.Single
}

// FederalWithholding is Worksheet 1A of Publication 15-T, the percentage
// method for automated payroll systems: the federal income tax withheld from
// one paycheck of taxable wages, gross less pre-tax lines.
//
// The annual tables are built from the year's § 1(j) brackets and § 63(c)
// standard deduction as Publication 15-T builds them: the standard table
// taxes annual wages less the standard deduction plus line 1g's amount, and
// the Step 2 table taxes twice the wages less the standard deduction, halved.
// The published tables round their thresholds to the dollar, so a figure here
// can differ from one read off them by a few cents.
func FederalWithholding(year int, w W4, taxable money.USCents, periods int) (money.USCents, working.Rule, error) {
	if periods <= 0 {
		return 0, working.Rule{}, fmt.Errorf("payroll: %d pay periods a year", periods)
	}
	if w.exempt {
		r, err := working.NewRule("Publication 15-T, Worksheet 1A").Applied("The W-4 claims exemption, so no federal income tax is withheld").Build()
		return 0, r, err
	}
	status := tableStatus(w.status)
	table, err := ustaxcalc.TableFor(year, status)
	if err != nil {
		return 0, working.Rule{}, err
	}
	annual := taxable*money.USCents(periods) + w.otherIncome
	adjusted := annual - w.deductions
	var tentative money.USCents
	if w.multipleJobs {
		ladder, err := ustaxcalc.OrdinaryIncomeTax(year, status, max(0, 2*adjusted-table.StandardDeduction))
		if err != nil {
			return 0, working.Rule{}, err
		}
		tentative = halve(ladder.Value().Total)
	} else {
		adjusted -= line1g(w.status)
		ladder, err := ustaxcalc.OrdinaryIncomeTax(year, status, max(0, adjusted-(table.StandardDeduction-line1g(w.status))))
		if err != nil {
			return 0, working.Rule{}, err
		}
		tentative = ladder.Value().Total
	}
	per := divide(tentative, periods)
	credit := divide(w.dependents, periods)
	withheld := max(0, per-credit) + w.extra
	r, err := working.NewRule("Publication 15-T, Worksheet 1A").Applied(fmt.Sprintf(
		"%v of taxable wages a paycheck, %d paychecks a year, withheld at the %s table%s: %v",
		taxable, periods, status, map[bool]string{true: " with Step 2 checked", false: ""}[w.multipleJobs], withheld)).Build()
	return withheld, r, err
}

// FlatSupplemental is the federal tax withheld on a supplemental payment by
// the flat rate: 22%, and 37% on what takes the employer's supplemental
// wages for the year past $1 million. before is the supplemental wages paid
// earlier in the year.
func FlatSupplemental(amount, before money.USCents) money.USCents {
	over := max(0, before+amount-MandatoryThreshold)
	over = min(over, amount)
	return SupplementalRate.Of(amount-over) + MandatorySupplementalRate.Of(over)
}

// SocialSecurity is the Social Security tax on a paycheck's Social Security
// wages, given the wages the job has already paid in the year: nothing past
// the wage base.
func SocialSecurity(year int, wages, before money.USCents) (money.USCents, error) {
	base, err := WageBase(year)
	if err != nil {
		return 0, err
	}
	taxed := max(0, min(wages, base-before))
	return SocialSecurityRate.Of(taxed), nil
}

// WageBase is the year's Social Security wage base, from ustaxcalc's tables.
func WageBase(year int) (money.USCents, error) {
	t, err := ustaxcalc.TableFor(year, filing.Single)
	if err != nil {
		return 0, err
	}
	return t.SocialSecurityWageBase, nil
}

// Medicare is the Medicare tax on a paycheck's Medicare wages, given the
// job's Medicare wages earlier in the year: 1.45%, and 0.9% more on wages
// past $200,000 from this employer.
func Medicare(wages, before money.USCents) money.USCents {
	over := max(0, before+wages-AdditionalMedicareWithholding)
	over = min(over, max(0, wages))
	return MedicareRate.Of(wages) + AdditionalMedicareRate.Of(over)
}

// PlanLimit is a person's § 402(g) limit for the year with the § 414(v)
// catch-up their age at the end of the year allows: $23,500 for 2025, plus
// $7,500 from 50, or $11,250 at 60 to 63 (Notice 2024-80); $24,500 for 2026,
// plus $8,000 from 50, or $11,250 at 60 to 63 (Notice 2025-67). It returns
// the base and the catch-up separately, and an error for a year with no
// figures.
func PlanLimit(year, age int) (base, catchUp money.USCents, err error) {
	var older money.USCents
	switch year {
	case 2025:
		base, older = 23_500*money.Dollar, 7_500*money.Dollar
	case 2026:
		base, older = 24_500*money.Dollar, 8_000*money.Dollar
	default:
		return 0, 0, fmt.Errorf("payroll: no § 402(g) limit for %d: %w", year, ustaxcalc.ErrNoTable)
	}
	switch {
	case age >= 60 && age <= 63:
		catchUp = 11_250 * money.Dollar
	case age >= 50:
		catchUp = older
	}
	return base, catchUp, nil
}

// NoWageTax reports whether a state taxes no wages, so nothing is withheld
// for it: Alaska, Florida, Nevada, New Hampshire, South Dakota, Tennessee,
// Texas, Washington and Wyoming.
func NoWageTax(state string) bool {
	switch state {
	case "AK", "FL", "NV", "NH", "SD", "TN", "TX", "WA", "WY":
		return true
	}
	return false
}

func halve(a money.USCents) money.USCents { return divide(a, 2) }

// divide rounds a / n half away from zero.
func divide(a money.USCents, n int) money.USCents {
	d := money.USCents(n)
	if a < 0 {
		return -((-a + d/2) / d)
	}
	return (a + d/2) / d
}
