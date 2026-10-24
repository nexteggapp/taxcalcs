package washingtontaxcalc

// Tax year 2025.
//
// The standard deduction and the charitable donation figures are the
// Department of Revenue's own capital gains tax page, which is where the
// inflation-adjusted amounts for a year are published. The 2.9% surcharge is
// ESSB 5813, chapter 421 of the Laws of 2025, as the Department summarised that
// session. The extracts are in testdata and tables_test.go checks every figure
// below against them.
//
// The 7% base rate is RCW 82.87.040(1)(a) and has not moved since the tax was
// created by ESSB 5096 in 2021, so it is not indexed and appears in no annual
// publication.

import "github.com/nexteggapp/taxcalcs/money"

func init() {
	const dollar = money.Dollar

	register(Table{
		Year: 2025,

		StandardDeduction: 278_000 * dollar,

		BaseRate: money.Percent(7),

		SurchargeThreshold: 1_000_000 * dollar,
		SurchargeRate:      money.Rate(290), // 2.9%

		CharitableDeductionThreshold: 278_000 * dollar,
		CharitableDeductionCap:       111_000 * dollar,
	})
}
