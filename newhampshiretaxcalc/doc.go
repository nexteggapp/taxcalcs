// Package newhampshiretaxcalc answers the shared estimated tax questions for
// New Hampshire, which has no individual income tax.
//
// New Hampshire does not tax wages, and its interest and dividends tax, RSA
// chapter 77, was repealed from January 1, 2025. Its business profits tax is
// not covered.
//
// For 2026 the answer is that there is nothing to estimate. Requirement is
// exempt with no target, Schedule has no due dates, and Penalty is zero. Each
// result cites the document the finding rests on, which is checked into
// testdata/.
//
// Start at [EstimatedTax].
package newhampshiretaxcalc
