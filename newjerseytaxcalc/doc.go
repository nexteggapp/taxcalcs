// Package newjerseytaxcalc computes New Jersey's estimated gross income tax
// for 2026: whether estimated payments are needed, what is due by each date,
// and the interest on an underpayment.
//
// # Only estimated tax so far
//
// There is no income tax calculator here yet. A caller passes the tax in as
// [estimated.Figures.Tax], Form NJ-1040 line 50, and the refundable credits
// on lines 56 and 58 to 65 in [EstimatedFacts].
//
// # How New Jersey differs
//
// The current-year test is 80%, and the threshold is $400. Four exceptions
// take the interest off a quarter whose payments reach them, among them last
// year's income at this year's rates. Interest is prime plus 3%, 10% for
// 2026, for each month or part of a month. A return filed and paid by
// February 15 replaces the January installment.
//
// # What is not built
//
// Farmers, who pay in full by January 15, get [estimated.ErrUnsupported].
// Nonresidents' Form NJ-2210NR scales Exceptions 2 to 4 by the New Jersey
// income percentage; a caller applies it to the amounts it passes. The
// exception for an installment due after the taxpayer's death is not
// applied. Estates and trusts are not covered.
//
// Start at [EstimatedTax].
package newjerseytaxcalc
