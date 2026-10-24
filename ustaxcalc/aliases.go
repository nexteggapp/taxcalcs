package ustaxcalc

import (
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// The money, filing status and result types used to be declared in this
// package, and are now declared in [money], [filing] and [working] so that the
// state calculators can use them without importing the federal rules. This file
// is what keeps them reachable from here.
//
// # These are aliases, not wrappers
//
// An alias makes two names for one type, so a [Rate] and a [money.Rate] are the
// same type to the compiler and to reflection. Nothing converts, nothing can
// drift, and a caller can mix the two names freely.
//
// Moving the names instead would break every caller of a module published for
// other people to use, and buy nothing: the types genuinely are the same, so
// making callers say so is churn rather than clarity.
//
// # One name did change
//
// USD is gone, replaced by [USCents]. That one is a break on purpose, and the
// only one here. The type has always held cents, so USD(50_000) read as fifty
// thousand dollars and meant five hundred. Keeping an alias would preserve
// exactly the confusion the rename exists to end, so there is none. A caller
// updating gets a compile error naming the type, which is the outcome worth
// having.

// USCents is an amount of United States currency, as an integer count of cents.
type USCents = money.USCents

// Rate is a proportion, as an integer count of basis points.
type Rate = money.Rate

// Dollar is one dollar, for writing an amount the way it is spoken.
const Dollar = money.Dollar

// Dollars returns the amount for a whole number of dollars.
func Dollars(d int64) USCents { return money.Dollars(d) }

// Percent returns the rate for a whole number of percent.
func Percent(p int32) Rate { return money.Percent(p) }

// FilingStatus is the status a return is filed under.
type FilingStatus = filing.Status

const (
	FilingStatusUnspecified = filing.Unspecified
	Single                  = filing.Single
	MarriedFilingJointly    = filing.MarriedFilingJointly
	MarriedFilingSeparately = filing.MarriedFilingSeparately
	HeadOfHousehold         = filing.HeadOfHousehold
	SurvivingSpouse         = filing.SurvivingSpouse
)

// Rule is a published rule and what applying it to the caller's figures did.
type Rule = working.Rule

// Required names a position the caller has not supplied.
type Required = working.Required

// PositionKind names a choice that the figures alone do not determine.
type PositionKind = working.PositionKind

// Result is a computed figure, the rules that produced it, and the positions
// the caller still owes.
type Result[T any] = working.Result[T]

const (
	PositionUnspecified            = working.PositionUnspecified
	PositionDeductionMethod        = working.PositionDeductionMethod
	PositionLotSelection           = working.PositionLotSelection
	PositionIRADeductibility       = working.PositionIRADeductibility
	PositionSubstantiallyIdentical = working.PositionSubstantiallyIdentical
	PositionTradeOrBusiness        = working.PositionTradeOrBusiness
	PositionDependentQualifies     = working.PositionDependentQualifies
)
