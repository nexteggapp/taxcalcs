// Package wyomingtaxcalc answers the shared estimated tax questions for
// Wyoming, which has no individual income tax.
//
// Wyoming has no individual income tax. The chapter of its tax statutes headed
// income taxes holds one section, repealed in 1998.
//
// For 2026 the answer is that there is nothing to estimate. Requirement is
// exempt with no target, Schedule has no due dates, and Penalty is zero. Each
// result cites the document the finding rests on, which is checked into
// testdata/.
//
// Start at [EstimatedTax].
package wyomingtaxcalc
