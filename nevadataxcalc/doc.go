// Package nevadataxcalc answers the shared estimated tax questions for Nevada,
// which has no individual income tax.
//
// Nevada's constitution bars an income tax on the wages or personal income of
// natural persons.
//
// For 2026 the answer is that there is nothing to estimate. Requirement is
// exempt with no target, Schedule has no due dates, and Penalty is zero. Each
// result cites the document the finding rests on, which is checked into
// testdata/.
//
// Start at [EstimatedTax].
package nevadataxcalc
