// Package coloradotaxcalc computes Colorado's estimated individual income tax
// for 2026: whether estimated payments are needed, what is due by each date,
// and the underpayment penalty.
//
// # Only estimated tax so far
//
// There is no income tax calculator here yet. A caller passes the year's
// Colorado tax in as [estimated.Figures.Tax]: the tax after every credit
// except withholding and estimated payments, DR 0204 line 1. For the
// annualised method the caller passes the Colorado taxable income to each
// period end as [estimated.Period.Income], and the rules apply the 4.4% rate
// as DR 0204 Part 4 does.
//
// # How Colorado differs
//
// The current-year test is 70% of this year's tax, not 90%. The prior-year
// test is 100% of last year's Colorado tax, or 110% when last year's federal
// adjusted gross income was over $150,000 ($75,000 married filing
// separately), and it needs a Colorado return for a full twelve-month year.
// The penalty is interest at Colorado's regular rate, which is set once for
// each calendar year: 11% for 2026 and 10% for 2027. The annualised method is
// open only to a filer who also annualises federally.
//
// # What is not built
//
// Farmers and fishermen, who make one payment by January 15 under
// C.R.S. § 39-22-605(10), get [estimated.ErrUnsupported]. Local occupational
// privilege taxes are not covered. Denver's is withheld by employers and takes
// no estimated payments; whether any other city's does was not found.
//
// Start at [EstimatedTax].
package coloradotaxcalc
