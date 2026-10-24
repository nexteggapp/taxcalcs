// Package mainetaxcalc computes Maine's estimated individual income tax for
// 2026: whether estimated payments are needed, what is due by each date, and
// the underpayment penalty.
//
// # Only estimated tax so far
//
// There is no income tax calculator here yet. A caller passes this year's
// Maine tax after credits in as [estimated.Figures.Tax], including the 2%
// surcharge on high incomes from 2026 and excluding any pass-through entity
// tax under 36 M.R.S. chapter 815, subchapter 3, which § 5228(1)(B) leaves
// out. Last year's is Form 2210ME line 5: Form 1040ME line 24 less lines 25c
// to 25e.
//
// # How Maine differs
//
// No payments are needed if this year's tax after withholding, or last
// year's liability, is under $1,000. There is no 110% tier. Income not
// subject to Maine withholding that is $500,000 or more above the same period
// of last year is an unusual event: 90% of the tax on it is due with that
// period's installment, whatever last year's tax was. That fact is in
// [EstimatedFacts]. The penalty is interest by the month, a part of a month
// counting as a whole one, compounded monthly.
//
// # What is not built
//
// Farmers and persons who fish commercially, who make one payment at 66 2/3%,
// get [estimated.ErrUnsupported]. Fiscal-year filers are not covered: the
// dates and the rate are a calendar year's. The research found no rule of
// Maine's own for first-year or part-year residents, so none is applied.
//
// Start at [EstimatedTax].
package mainetaxcalc
