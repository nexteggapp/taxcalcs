// Package idahotaxcalc holds Idaho's estimated tax rules for individuals for
// 2026, which are that there are none.
//
// Idaho requires estimated payments of corporations only (Idaho Code
// § 63-3036A) and charges no underpayment penalty to individuals. The package
// still implements [estimated.EstimatedTax], so a caller that loops over
// every state gets a plain answer for Idaho: no payments required, no
// installments, and a penalty of zero.
//
// # What it does not compute
//
// The automatic extension to file holds only if, by the April due date, the
// filer has paid 80% of this year's tax or 100% of the tax on last year's
// return (Idaho Code § 63-3033). That test is described in the Explanation and
// not computed: it is a condition of the extension, not an estimated payment.
// There is no Idaho income tax calculator here.
//
// Start at [EstimatedTax].
package idahotaxcalc
