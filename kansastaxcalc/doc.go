// Package kansastaxcalc computes Kansas's estimated individual income tax for
// 2026: whether estimated payments are needed, what is due by each date, and
// the underpayment penalty.
//
// # Only estimated tax so far
//
// There is no income tax calculator here yet. A caller passes the tax in as
// [estimated.Figures.Tax], Form K-40 line 18, with the refundable credits and
// the credit for tax paid on the K-120S in [EstimatedFacts]. A nonresident
// counts only Kansas-source income, as the K-40ES says.
//
// # How Kansas differs
//
// The threshold is $500. The tests are 90% of this year's tax and 100% of
// last year's, with no 110% tier. Schedule K-210 takes last year's figure as
// zero when no return was filed or it showed no tax. The penalty is figured
// column by column: the total short at each due date is charged by the day
// until the next one, at a rate fixed for each calendar year, 8% for 2026.
//
// # What is not built
//
// Farmers and fishers, who make one payment by January 15, get
// [estimated.ErrUnsupported]. The local intangibles tax on interest and
// dividends is billed by the county after the year and takes no estimates.
//
// Start at [EstimatedTax].
package kansastaxcalc
