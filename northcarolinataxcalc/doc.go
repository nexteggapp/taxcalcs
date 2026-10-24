// Package northcarolinataxcalc computes North Carolina's estimated income tax
// for 2026: whether estimated payments are needed, what is due by each date,
// and the interest on underpaid estimated tax.
//
// # Only estimated tax so far
//
// There is no income tax calculator here yet. A caller passes the tax in as
// [estimated.Figures.Tax], Form D-422 line 3: Form D-400 line 15 less the
// credits on line 16.
//
// # How North Carolina differs
//
// There is no 110% tier, and the no-liability exception has no twelve-month
// or residence condition. Nothing waives the interest for a casualty,
// retirement or disability. The rate is set by the Secretary of Revenue for
// each half-year, 7% for 2026. A return filed and paid by January 31 counts
// as paid on January 15.
//
// # What is not built
//
// Farmers and fishermen, who make one installment by January 15 or file and
// pay by March 1, get [estimated.ErrUnsupported]. Estates and trusts are
// outside the section. North Carolina has no local income tax.
//
// Start at [EstimatedTax].
package northcarolinataxcalc
