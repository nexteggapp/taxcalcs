// Package massachusettstaxcalc computes Massachusetts's estimated personal
// income tax for 2026: whether estimated payments are needed, what is due by
// each date, and the underpayment penalty.
//
// # Only estimated tax so far
//
// There is no income tax calculator here yet. A caller passes the year's
// Massachusetts tax after credits, including the 4% surtax, as
// [estimated.Figures.Tax].
//
// # How Massachusetts differs
//
// The required annual payment is the lesser of 80% of this year's tax and
// 100% of last year's, with no 110% tier. The threshold is $400. The penalty
// is simple interest at a rate set each quarter, and the annualised method
// uses 20%, 40%, 60% and 80%. There is no January 31 file-and-pay relief.
//
// # What is left open
//
// No source settles whether a requirement first met after March 31 lowers the
// earlier installments. The department's reading is used and reported in
// Required; [EstimatedFacts] can choose the statute's. Whether a part-year
// prior return opens the 100% test is reported the same way.
//
// # What is not built
//
// Farmers and fishermen, who owe 66 2/3% and no penalty after filing and
// paying by March 1, get [estimated.ErrUnsupported]. Fiduciaries (Form
// M-2210F) and fiscal years are not covered.
//
// Start at [EstimatedTax].
package massachusettstaxcalc
