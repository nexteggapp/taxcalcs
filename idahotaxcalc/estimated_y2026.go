package idahotaxcalc

import "github.com/nexteggapp/taxcalcs/money"

// Idaho for tax year 2026.
//
// No figure here is an estimated tax figure, because Idaho has none for
// individuals. They are the extension prepayment test and the interest rate
// the Explanation describes. Each is checked by estimated_test.go against an
// extract in testdata/, listed in testdata/SOURCES.md:
//
//   - the 80% and 100% tests, the $50 minimum and the 2% a month penalty:
//     Idaho Code § 63-3033, and Form 51, which states the same tests for the
//     2025 extension; no edition for the 2026 return is published, and
//     § 63-3033 was last amended in 2014;
//   - the 6% interest rate for 2027, the year a 2026 balance unpaid at the
//     due date accrues in: the Tax Commission's Interest Rates page;
//   - that individuals owe no estimated payments: Idaho Code § 63-3036A and
//     the 2025 Form 40 instructions.

func init() {
	registerEstimated(estimatedYear{
		year:               2026,
		extensionThisYear:  money.Percent(80),
		extensionPriorYear: money.Percent(100),
		extensionMinimum:   50 * money.Dollar,
		extensionPenalty:   money.Percent(2),
		interest:           money.Percent(6),
	})
}
