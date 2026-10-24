// Package louisianataxcalc computes Louisiana's estimated individual income
// tax for 2026: whether estimated payments are needed, what is due by each
// date, and the underpayment penalty.
//
// # Only estimated tax so far
//
// There is no income tax calculator here yet. Louisiana taxes income at one
// rate, 3% for 2026, and a caller passes the tax in as
// [estimated.Figures.Tax], after credits.
//
// # Act 307 of 2026
//
// Act 307 changed the rules for 2026 after the department's latest penalty
// form was printed: the share of this year's tax falls from 90% to 70%, the
// penalty runs to May 15 rather than April 15, the annualized income
// exception is repealed, and the test on last year's facts uses this year's
// standard deduction. This package computes the Act, and every result's
// Divergences name the documents that predate it.
//
// # How Louisiana differs
//
// The threshold is $1,000 of tax after credits and withholding, or $2,000 on
// a joint return. The required annual payment is the lesser of 70% of this
// year's tax and 100% of last year's, with no 110% tier. A filer who first
// meets the requirement after April 1 divides it among the dates left. The
// penalty is a fixed 12% a year, charged at a printed 0.000329 a day. Last
// year's tax, last year's facts at this year's rate, and 70% of the tax on
// the income earned to each period end each excuse an installment they reach.
//
// # What is not built
//
// Farmers and fishermen, who make one payment by January 15 at 66 2/3%, get
// [estimated.ErrUnsupported]. Spreading withholding by the months in each
// period, which R-210R allows but does not define, is not offered.
//
// Start at [EstimatedTax].
package louisianataxcalc
