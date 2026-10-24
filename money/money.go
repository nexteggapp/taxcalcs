// Package money is the money and rate types every calculator in this module
// uses.
//
// Nothing here knows about tax. It is a count of cents and a count of basis
// points, with the arithmetic that turns one into the other.
//
// # Why this is not in each calculator package
//
// There is one calculator package per jurisdiction — ustaxcalc for the federal
// rules, californiataxcalc, oregontaxcalc and washingtontaxcalc for three
// states — and they deliberately do not import each other. A state package that imported
// the federal one would drag the federal rate tables into a caller who wanted
// only California, and would suggest a dependency between the two bodies of law
// that does not exist.
//
// Without a shared package the consequence is that each of them declares its
// own USCents. Then a caller holding a federal figure cannot hand it to a state
// calculator without a conversion that does nothing, and every such conversion
// is a place to get a factor of a hundred wrong. So the money type is shared and
// the tax rules are not, which is the line worth drawing: dollars are the same
// dollars in every jurisdiction, and the rules are different in all of them.
//
// This is the same argument that gave the schemas in taxprotos their own Money
// rather than borrowing one from an application that uses them.
package money

import (
	"fmt"
	"math/big"
)

// USCents is an amount of United States currency, as an integer count of cents.
// The unit is in the name because the value is not dollars, and a type called
// USD would say it was.
//
// That is not a hypothetical confusion. USD(50_000) reads as fifty thousand
// dollars and means five hundred; both spellings compile, and one is wrong by a
// factor of a hundred. Under the old name every raw literal in this module
// carried a comment translating it back into dollars, which is what a misnamed
// type forces.
//
// Not a float. Every figure here is summed with others, and 0.10 has no exact
// binary floating point representation.
type USCents int64

// Dollar is one dollar, for writing an amount the way it is spoken:
//
//	42_581 * money.Dollar
//
// The same idiom as [time.Second]. It is what removes the need for a comment
// translating every literal.
const Dollar = USCents(100)

// Dollars returns the amount for a whole number of dollars.
//
// [Dollar] is the better spelling for a literal. This stays for the case
// [Dollar] cannot serve: an amount that is computed rather than written down.
func Dollars(d int64) USCents { return USCents(d * 100) }

// String renders the amount as dollars and cents, without a thousands
// separator. Formatting for a screen is the caller's business.
func (a USCents) String() string {
	sign := ""
	if a < 0 {
		sign, a = "-", -a
	}
	return fmt.Sprintf("%s$%d.%02d", sign, int64(a)/100, int64(a)%100)
}

// Rate is a proportion, as an integer count of basis points. One percent is
// 100 basis points, so 22% is 2200 and the 0.9% additional Medicare tax is 90.
//
// Basis points rather than percent, because published rates are not whole
// numbers. 26 U.S.C. § 1401 uses tenths of a percent and § 1402(a)(12) uses
// hundredths; Oregon's top rate is 9.9%, and Washington's additional capital
// gains rate is 2.9%.
type Rate int32

// Percent returns the rate for a whole number of percent.
func Percent(p int32) Rate { return Rate(p * 100) }

// String renders the rate as a percentage, trimming a trailing zero so that
// 2200 reads as 22% rather than 22.00%.
func (r Rate) String() string {
	whole, frac := int32(r)/100, int32(r)%100
	switch {
	case frac == 0:
		return fmt.Sprintf("%d%%", whole)
	case frac%10 == 0:
		return fmt.Sprintf("%d.%d%%", whole, frac/10)
	default:
		return fmt.Sprintf("%d.%02d%%", whole, frac)
	}
}

// Of returns r applied to a, rounded to the nearest cent with halves rounded
// away from zero.
//
// Rounding at each application rather than carrying a fraction, because a
// caller adds these together and has to be able to reproduce the total from the
// parts printed on a screen.
func (r Rate) Of(a USCents) USCents {
	product := int64(a) * int64(r)
	if product < 0 {
		return USCents((product - 5000) / 10000)
	}
	return USCents((product + 5000) / 10000)
}

// Fine is the same rate in millionths, which holds it exactly.
func (r Rate) Fine() FineRate { return FineRate(r) * 100 }

// FineRate is a proportion as an integer count of millionths. One percent is
// 10_000 and one basis point is 100, so Maryland's 10.8133% is 108_133.
//
// It exists for published interest rates that basis points cannot hold. The
// Comptroller of Maryland sets a rate with four decimal places for each
// calendar year, and cutting it to 10.81% changes the interest on a large
// shortfall by whole dollars. [Rate] stays the type for tax rates and shares,
// which are published in basis points or coarser.
type FineRate int64

// Nearest is the rate rounded to the nearest basis point, halves away from
// zero, for a caller that shows rates in basis points. It is exact when
// [FineRate.IsBasisPoints] is true.
func (r FineRate) Nearest() Rate {
	if r < 0 {
		return Rate((r - 50) / 100)
	}
	return Rate((r + 50) / 100)
}

// IsBasisPoints reports whether the rate is a whole number of basis points.
func (r FineRate) IsBasisPoints() bool { return r%100 == 0 }

// String renders the rate as a percentage with trailing zeros dropped, so
// that 108_133 reads as 10.8133% and 99_950 as 9.995%.
func (r FineRate) String() string {
	sign := ""
	if r < 0 {
		sign, r = "-", -r
	}
	whole, frac := int64(r)/10_000, int64(r)%10_000
	if frac == 0 {
		return fmt.Sprintf("%s%d%%", sign, whole)
	}
	f := fmt.Sprintf("%04d", frac)
	for f[len(f)-1] == '0' {
		f = f[:len(f)-1]
	}
	return fmt.Sprintf("%s%d.%s%%", sign, whole, f)
}

// Of returns r applied to a, rounded to the nearest cent with halves rounded
// away from zero, as [Rate.Of] does.
func (r FineRate) Of(a USCents) USCents {
	n := new(big.Int).Mul(big.NewInt(int64(a)), big.NewInt(int64(r)))
	q, rem := new(big.Int).QuoRem(n, big.NewInt(1_000_000), new(big.Int))
	if rem.CmpAbs(big.NewInt(500_000)) >= 0 {
		if n.Sign() < 0 {
			q.Sub(q, big.NewInt(1))
		} else {
			q.Add(q, big.NewInt(1))
		}
	}
	return USCents(q.Int64())
}

// Whole renders the amount in whole dollars with thousands separators, as a
// person reads money: $1,000 or −$1,200. Halves round away from zero. It is
// the form for any sentence a person reads; [USCents.String] keeps the
// cents for working and logs.
func (a USCents) Whole() string {
	sign := ""
	if a < 0 {
		sign, a = "−", -a
	}
	d := (int64(a) + 50) / 100
	s := fmt.Sprintf("%d", d)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return sign + "$" + s
}
