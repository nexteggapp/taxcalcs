package ustaxcalc

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/nexteggapp/taxcalcs/working"
)

// IRAContribution is one person's traditional and Roth IRA contributions for
// a year, and the facts 26 U.S.C. § 219 reads to decide how much of the
// traditional one is deductible.
type IRAContribution struct {
	amount, roth                  USCents
	catchUp                       bool
	covered, coveredAnswered      bool
	spouseCovered, spouseAnswered bool
	livedApart                    bool
}

// Amount is the traditional contribution for the year.
func (c IRAContribution) Amount() USCents { return c.amount }

// Roth is the Roth contribution for the year.
func (c IRAContribution) Roth() USCents { return c.roth }

// CatchUp reports a contributor who is 50 or older by the end of the year.
func (c IRAContribution) CatchUp() bool { return c.catchUp }

// ActiveParticipant reports whether the contributor is covered by a
// retirement plan at work, as § 219(g)(5) defines it, and whether the
// caller said.
func (c IRAContribution) ActiveParticipant() (covered, answered bool) {
	return c.covered, c.coveredAnswered
}

// SpouseActiveParticipant reports whether the contributor's spouse is
// covered by a retirement plan at work, and whether the caller said.
func (c IRAContribution) SpouseActiveParticipant() (covered, answered bool) {
	return c.spouseCovered, c.spouseAnswered
}

// LivedApart reports a married couple filing separately who lived apart all
// year, whom § 219(g)(4) treats as unmarried.
func (c IRAContribution) LivedApart() bool { return c.livedApart }

// IRAContributionBuilder builds an [IRAContribution].
type IRAContributionBuilder struct{ c IRAContribution }

func NewIRAContribution(amount USCents) *IRAContributionBuilder {
	return &IRAContributionBuilder{c: IRAContribution{amount: amount}}
}

// Roth sets the year's Roth contribution.
func (b *IRAContributionBuilder) Roth(amount USCents) *IRAContributionBuilder {
	b.c.roth = amount
	return b
}

func (b *IRAContributionBuilder) CatchUp() *IRAContributionBuilder {
	b.c.catchUp = true
	return b
}

func (b *IRAContributionBuilder) ActiveParticipant(covered bool) *IRAContributionBuilder {
	b.c.covered, b.c.coveredAnswered = covered, true
	return b
}

func (b *IRAContributionBuilder) SpouseActiveParticipant(covered bool) *IRAContributionBuilder {
	b.c.spouseCovered, b.c.spouseAnswered = covered, true
	return b
}

func (b *IRAContributionBuilder) LivedApart() *IRAContributionBuilder {
	b.c.livedApart = true
	return b
}

// Build refuses a negative contribution.
func (b *IRAContributionBuilder) Build() (IRAContribution, error) {
	if b.c.amount < 0 || b.c.roth < 0 {
		return IRAContribution{}, errors.New("ustaxcalc: an IRA contribution cannot be negative")
	}
	return b.c, nil
}

// phaseOut is a § 219(g) range: the deduction falls from the full limit at
// from to nothing at from plus width.
type phaseOut struct{ from, width USCents }

// iraYear is one year's published § 219 figures.
type iraYear struct {
	limit, catchUp USCents
	source         string
	// The phase-out ranges, by who is covered and how the return is filed.
	single, joint, spouseOnly, separate phaseOut
	// survivingSpouse takes the joint applicable amount, which the notice
	// gives for a qualifying widow(er), over the $10,000 width § 219(g)(2)(A)(ii)
	// gives every return but a joint one.
	survivingSpouse phaseOut
	// The § 408A(c)(3) Roth ranges. The notice gives a qualifying
	// widow(er) the joint start; the width is the $15,000 § 408A(c)(3)(A)(ii)
	// gives every return but a joint or separate one.
	rothSingle, rothJoint, rothSeparate, rothSurvivingSpouse phaseOut
}

// iraYears are the published figures. 2026 is Notice 2025-67; no other year
// is held.
var iraYears = map[int]iraYear{
	2026: {
		limit:           7_500 * Dollar,
		catchUp:         1_100 * Dollar,
		source:          "Notice 2025-67",
		single:          phaseOut{81_000 * Dollar, 10_000 * Dollar},
		joint:           phaseOut{129_000 * Dollar, 20_000 * Dollar},
		spouseOnly:      phaseOut{242_000 * Dollar, 10_000 * Dollar},
		separate:        phaseOut{0, 10_000 * Dollar},
		survivingSpouse: phaseOut{129_000 * Dollar, 10_000 * Dollar},

		rothSingle:          phaseOut{153_000 * Dollar, 15_000 * Dollar},
		rothJoint:           phaseOut{242_000 * Dollar, 10_000 * Dollar},
		rothSeparate:        phaseOut{0, 10_000 * Dollar},
		rothSurvivingSpouse: phaseOut{242_000 * Dollar, 15_000 * Dollar},
	},
}

// IRADeduction is the deductible part of a traditional IRA contribution
// under § 219(b)(1) and (g): the contribution, up to the year's limit, less
// the reduction for a taxpayer or spouse covered by a plan at work.
//
// magi is adjusted gross income figured without the IRA deduction, as
// § 219(g)(3)(A) has it. The exclusions it also adds back, such as foreign
// earned income and student loan interest, are the caller's to include. The
// limit is not compared with the contributor's compensation; a caller whose
// compensation is under the limit passes the smaller contribution.
//
// A year with no published figures is [ErrNoTable].
func IRADeduction(year int, status FilingStatus, c IRAContribution, magi USCents) (Result[USCents], error) {
	y, ok := iraYears[year]
	if !ok {
		return Result[USCents]{}, fmt.Errorf("%w: no IRA deduction limits for %d", ErrNoTable, year)
	}
	limit := y.limit
	var b working.Notes
	cite := fmt.Sprintf("26 U.S.C. § 219(b)(1), (b)(5); %s", y.source)
	if c.catchUp {
		limit += y.catchUp
		b.Rule(cite, fmt.Sprintf("the %d limit is %s, and %s more at 50 or older, %s", year, y.limit, y.catchUp, limit))
	} else {
		b.Rule(cite, fmt.Sprintf("the %d limit is %s", year, limit))
	}

	married := (status == MarriedFilingJointly || status == MarriedFilingSeparately) && !c.livedApart
	covered, answered := c.ActiveParticipant()
	if !answered {
		b.Ask(working.PositionIRADeductibility, "Are you covered by a retirement plan at work?",
			"If you are, the IRA deduction is reduced or lost at higher incomes; until then, the full contribution is deducted.")
	}
	spouseCovered := false
	if married {
		var spouseAnswered bool
		spouseCovered, spouseAnswered = c.SpouseActiveParticipant()
		if !spouseAnswered {
			b.Ask(working.PositionIRADeductibility, "Is your spouse covered by a retirement plan at work?",
				"If your spouse is, your IRA deduction is reduced or lost at higher incomes; until then, it isn't reduced for that.")
		}
	}

	var r phaseOut
	var which string
	switch {
	case !covered && !spouseCovered:
		deduction := min(c.amount, limit)
		b.Rule("26 U.S.C. § 219(g)(1)", fmt.Sprintf("neither you nor a spouse is covered by a plan at work, so %s of the %s contribution is deductible", deduction, c.amount))
		return working.NewResult(deduction).With(b).Build()
	case status == MarriedFilingSeparately && married:
		r, which = y.separate, "a married person filing separately"
	case covered && status == MarriedFilingJointly:
		r, which = y.joint, "a covered contributor on a joint return"
	case covered && status == SurvivingSpouse:
		r, which = y.survivingSpouse, "a covered qualifying surviving spouse"
	case covered:
		r, which = y.single, "a covered single filer or head of household"
	default:
		r, which = y.spouseOnly, "a contributor not covered whose spouse is"
	}

	reduced := phasedOut(limit, magi, r)
	deduction := min(c.amount, reduced)
	b.Rule(fmt.Sprintf("26 U.S.C. § 219(g)(1) to (3), (7); %s", y.source),
		fmt.Sprintf("for %s the %d range is %s to %s; modified AGI of %s leaves a limit of %s, so %s of the %s contribution is deductible",
			which, year, r.from, r.from+r.width, magi, reduced, deduction, c.amount))
	return working.NewResult(deduction).With(b).Build()
}

// phasedOut is limit after a § 219(g)(2) reduction across r, which
// § 408A(c)(3)(A) also uses for a Roth: the reduction bears to the limit the
// ratio of income over the start of the range to the range's width, is
// rounded to the next lowest $10, and leaves no less than $200 unless it is
// the whole limit.
func phasedOut(limit, magi USCents, r phaseOut) USCents {
	over := max(magi-r.from, 0)
	switch {
	case over >= r.width:
		return 0
	case over == 0:
		return limit
	}
	reduction := new(big.Int).Mul(big.NewInt(int64(limit)), big.NewInt(int64(over)))
	reduction.Quo(reduction, big.NewInt(int64(r.width)))
	ten := int64(10 * Dollar)
	return max(limit-USCents(reduction.Int64()/ten*ten), 200*Dollar)
}

// IRALimits is one person's year under the limit their traditional and Roth
// IRA contributions share.
type IRALimits struct {
	combined, rothLimit, rothAllowed, deduction USCents
	traditionalExcess, rothExcess, excessTax    USCents
}

// Combined is the limit the two kinds of contribution share: the § 219(b)
// limit, with the age-50 addition, before any reduction for income.
func (l IRALimits) Combined() USCents { return l.combined }

// RothLimit is the most that could go to a Roth IRA at this income, before
// any traditional contribution is taken off: § 408A(c)(3)(A).
func (l IRALimits) RothLimit() USCents { return l.rothLimit }

// RothAllowed is the Roth contribution allowed after the traditional one:
// the smaller of RothLimit and what the traditional contribution leaves of
// Combined, § 408A(c)(2).
func (l IRALimits) RothAllowed() USCents { return l.rothAllowed }

// Deduction is the deductible part of the traditional contribution, as
// [IRADeduction] has it. A Roth contribution is never deducted.
func (l IRALimits) Deduction() USCents { return l.deduction }

// TraditionalExcess is the traditional contribution over Combined,
// § 4973(b)(1). A contribution that is allowed but not deductible is not an
// excess.
func (l IRALimits) TraditionalExcess() USCents { return l.traditionalExcess }

// RothExcess is the Roth contribution over RothAllowed, § 4973(f)(1).
func (l IRALimits) RothExcess() USCents { return l.rothExcess }

// ExcessTax is the § 4973(a) tax: 6% of the two excesses. Form 5329 lines
// 17 and 25 carry it to Schedule 2 line 8, and so into Form 1040's total
// tax. Form 2210 line 2 takes line 8 for the tax on distributions only, so
// it is not part of the tax that sets the required annual payment.
func (l IRALimits) ExcessTax() USCents { return l.excessTax }

// IRAContributionLimits works out one person's traditional and Roth IRA year:
// the shared limit, the Roth limit at this income, the traditional deduction,
// and any excess with the 6% tax on it.
//
// magi is adjusted gross income figured without the IRA deduction, as
// § 219(g)(3)(A) has it; § 408A(c)(3)(B)(i) reads the same figure for the
// Roth, but without income from converting to a Roth IRA, which the caller
// leaves out. As in [IRADeduction], the limit is not compared with the
// person's compensation.
//
// The 6% tax is on this year's excess only. An excess left in from an
// earlier year, § 4973(b)(2) and (f)(2), is the caller's to add, and the tax
// is no more than 6% of the accounts' value at the end of the year, which
// this does not know. A contribution withdrawn under § 408(d)(4) is treated
// as not made, so the caller passes the contributions net of it.
//
// A year with no published figures is [ErrNoTable].
func IRAContributionLimits(year int, status FilingStatus, c IRAContribution, magi USCents) (Result[IRALimits], error) {
	y, ok := iraYears[year]
	if !ok {
		return Result[IRALimits]{}, fmt.Errorf("%w: no IRA contribution limits for %d", ErrNoTable, year)
	}
	var l IRALimits
	var b working.Notes
	l.combined = y.limit
	if c.catchUp {
		l.combined += y.catchUp
	}
	if c.amount > 0 {
		d, err := IRADeduction(year, status, c, magi)
		if err != nil {
			return Result[IRALimits]{}, err
		}
		b.Applied(d.Applied()...).Required(d.Required()...)
		l.deduction = d.Value()
	}

	var r phaseOut
	var which string
	married := (status == MarriedFilingJointly || status == MarriedFilingSeparately) && !c.livedApart
	switch {
	case status == MarriedFilingJointly:
		r, which = y.rothJoint, "a joint return"
	case status == MarriedFilingSeparately && married:
		r, which = y.rothSeparate, "a married person filing separately"
	case status == SurvivingSpouse:
		r, which = y.rothSurvivingSpouse, "a qualifying surviving spouse"
	default:
		r, which = y.rothSingle, "a single filer or head of household"
	}
	l.rothLimit = phasedOut(l.combined, magi, r)
	l.rothAllowed = min(l.rothLimit, max(l.combined-c.amount, 0))
	after := ""
	if c.amount > 0 {
		after = fmt.Sprintf("; after %s of traditional contributions, %s may go to a Roth", c.amount, l.rothAllowed)
	}
	b.Rule(fmt.Sprintf("26 U.S.C. § 408A(c)(1) to (3); %s", y.source),
		fmt.Sprintf("the %d limit is %s across traditional and Roth IRAs; for %s the Roth range is %s to %s, and modified AGI of %s leaves a Roth limit of %s%s. A Roth contribution is never deducted",
			year, l.combined, which, r.from, r.from+r.width, magi, l.rothLimit, after))

	l.traditionalExcess = max(c.amount-l.combined, 0)
	l.rothExcess = max(c.roth-l.rothAllowed, 0)
	if excess := l.traditionalExcess + l.rothExcess; excess > 0 {
		l.excessTax = (excess*6 + 50) / 100
		b.Rule("26 U.S.C. § 4973(a), (b)(1), (f)(1)",
			fmt.Sprintf("%s of traditional and %s of Roth contributions are over the limits; the tax is 6%% of %s, %s",
				l.traditionalExcess, l.rothExcess, excess, l.excessTax))
	}
	return working.NewResult(l).With(b).Build()
}
