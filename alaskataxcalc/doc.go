// Package alaskataxcalc answers the shared estimated tax questions for Alaska,
// which has no individual income tax.
//
// Alaska does not tax the income of individuals, and employers withhold no
// Alaska income tax.
//
// For 2026 the answer is that there is nothing to estimate. Requirement is
// exempt with no target, Schedule has no due dates, and Penalty is zero. Each
// result cites the document the finding rests on, which is checked into
// testdata/.
//
// Start at [EstimatedTax].
package alaskataxcalc
