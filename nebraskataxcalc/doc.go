// Package nebraskataxcalc computes Nebraska's estimated income tax for 2026:
// whether estimated payments are needed, what is due by each date, and the
// underpayment penalty.
//
// # Only estimated tax so far
//
// There is no income tax calculator here yet. A caller passes the tax in as
// [estimated.Figures.Tax], Form 2210N line 3: Form 1040N line 35 less the
// refundable credits.
//
// # How Nebraska differs
//
// The threshold is $500, and a first-time Nebraska filer owes no penalty.
// The penalty is the state's delinquent tax rate, 8% a year, simple, set
// every other year under § 45-104.02. Otherwise the rules are the federal
// ones: 90% or 100%, 110% above $150,000 of last year's federal AGI, four
// quarters, and payments applied to the oldest shortfall first.
//
// # What is not built
//
// Farmers, ranchers and fishermen, who pay once by January 15 or file and
// pay by March 1, get [estimated.ErrUnsupported]. Tax paid for a nonresident
// by a pass-through entity is not dated on the entity's year end; pass it as
// a payment on that date.
//
// Start at [EstimatedTax].
package nebraskataxcalc
