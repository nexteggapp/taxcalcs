// Package tennesseetaxcalc answers the shared estimated tax questions for
// Tennessee, which has no individual income tax.
//
// Tennessee's constitution bars a state or local tax on payroll or earned
// income, and its Hall tax on interest and dividends was repealed for tax
// periods beginning on or after January 1, 2021.
//
// For 2026 the answer is that there is nothing to estimate. Requirement is
// exempt with no target, Schedule has no due dates, and Penalty is zero. Each
// result cites the document the finding rests on, which is checked into
// testdata/.
//
// Start at [EstimatedTax].
package tennesseetaxcalc
