// Package texastaxcalc answers the shared estimated tax questions for Texas,
// which has no individual income tax.
//
// Texas's constitution, as amended by H.J.R. 38 and adopted by the voters on
// November 5, 2019, bars a tax on the net incomes of individuals.
//
// For 2026 the answer is that there is nothing to estimate. Requirement is
// exempt with no target, Schedule has no due dates, and Penalty is zero. Each
// result cites the document the finding rests on, which is checked into
// testdata/.
//
// Start at [EstimatedTax].
package texastaxcalc
