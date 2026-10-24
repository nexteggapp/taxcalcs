// Package montanataxcalc computes Montana's estimated income tax for 2026:
// whether estimated payments are needed, what is due by each date, and the
// interest on underpayment of estimated tax.
//
// # Only estimated tax so far
//
// There is no income tax calculator here yet. A caller passes the tax in as
// [estimated.Figures.Tax], Form 2 line 10 with any lump-sum and recapture
// taxes, which is EST-I Part I line 1, and the refundable credits in
// [EstimatedFacts].
//
// # How Montana differs
//
// The threshold is $500 and there is no 110% tier. Farmers and ranchers, and
// anybody who retired after 62 or became disabled this year or last, owe no
// estimated tax. The rate is the one in effect when the return is due, so
// 2026 estimates are charged at the 2027 rate for every day. A January 31
// that falls on a weekend moves to the next business day.
//
// # What is not built
//
// The hardship extension of § 15-30-2512(7) and short tax years. Montana has
// no local income tax.
//
// Start at [EstimatedTax].
package montanataxcalc
