// Package southcarolinataxcalc computes South Carolina's estimated individual
// income tax for 2026: whether estimated payments are needed, what is due by
// each date, and the underpayment penalty.
//
// # Only estimated tax so far
//
// There is no income tax calculator here yet. A caller passes the year's tax
// in as [estimated.Figures.Tax]: SC2210 Part I line 3, the tax after credits
// less refundable credits. For 2026 that tax is figured under Act 110 of 2026,
// not the SC1040ES worksheet, which predates it.
//
// # How South Carolina differs
//
// S.C. Code § 12-54-55(1) adopts the federal rules of 26 U.S.C. § 6654 with a
// $100 threshold in place of $1,000, and charges no penalty on an underpayment
// from personal service income earned in another state that withheld its own
// tax. South Carolina's own legal holidays move a due date. When the return
// is filed and paid by the end of January, the SC2210 ends the earlier
// installments' penalty on January 15. The rate follows the federal
// underpayment rate, as the department publishes it.
//
// # What is not built
//
// Farmers and fishermen, with two-thirds of gross income from farming and
// fishing in either year, get [estimated.ErrUnsupported]. The SC2210's box E,
// for a joint return in one of the two years only, is not applied: the caller
// supplies last year's tax. Nonresident aliens are not treated apart.
//
// Start at [EstimatedTax].
package southcarolinataxcalc
