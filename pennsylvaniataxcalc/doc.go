// Package pennsylvaniataxcalc computes Pennsylvania's estimated personal
// income tax for 2026: whether estimated payments are needed, what is due by
// each date, and the underpayment penalty.
//
// # Only estimated tax so far
//
// There is no income tax calculator here yet. Pennsylvania taxes income at
// one flat rate, 3.07% for 2026, and a caller passes the tax in as
// [estimated.Figures.Tax], net of the Tax Forgiveness, resident and other
// credits, with those credits in [EstimatedFacts].
//
// # How Pennsylvania differs
//
// The rules start from income received without Pennsylvania withholding,
// $14,000 for 2026, rather than from a tax amount alone. The prior-year test
// is last year's taxable income at this year's rate. The penalty is always
// measured against 90% of this year's tax; the prior-year test and the
// income-to-date test (REV-1630 Exceptions 1 and 2) excuse an installment
// instead of lowering it. A filer whose requirement arises late in the year
// has the earlier installments excused only under REV-1630's special
// exception. The interest is charged at a daily factor the department prints
// for each calendar year.
//
// # What is not built
//
// Farmers, who file REV-1630A, get [estimated.ErrUnsupported]. The exception
// for installments due on or after the taxpayer's death (72 P.S. § 7352(d)(1))
// is not applied, because [estimated.Facts] carries no date of death. Local
// earned income taxes take their own estimates and are not covered.
//
// Start at [EstimatedTax].
package pennsylvaniataxcalc
