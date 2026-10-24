// Package georgiataxcalc computes Georgia's estimated income tax for
// individuals for 2026: whether estimated payments are needed, what is due by
// each date, and the underpayment penalty.
//
// # Only estimated tax so far
//
// There is no income tax calculator here. A caller passes the year's Georgia
// tax after credits, Form 500 UET line 3, as [estimated.Figures.Tax], and
// gross income as [estimated.Figures.GrossIncome]. The dependents and
// estimated deductions the requirement test reads go in [EstimatedFacts].
//
// # How Georgia differs
//
// Estimated tax is required when gross income exceeds the dependent
// exemptions, the estimated deductions and $1,000, rather than at a tax
// amount. The tests are 70% of this year's tax or 100% of last year's, with
// no 110% tier. A late starter pays in fewer, equal installments. Each
// payment counts first toward its own period's installment. Three exceptions
// excuse an installment, and the penalty is a fixed 9% a year by the day.
//
// The statute could not be read from an official host, so the rules follow
// the Department of Revenue's forms, with the 2026 figures from HB 463.
//
// # What is not built
//
// Farmers and fishermen, who file by March 1 or are charged on the last
// quarter alone, get [estimated.ErrUnsupported]. Exception 4, for a
// decedent's estate or testamentary trust, is not applied: these are the
// rules for individuals. Fiscal years are not supported; Form 500 UET tells
// fiscal-year filers to call the Department.
//
// Start at [EstimatedTax].
package georgiataxcalc
