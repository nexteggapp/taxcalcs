// Package districtofcolumbiataxcalc computes the District of Columbia's
// estimated individual income tax for 2026: whether estimated payments are
// needed, what is due by each date, and the underpayment interest.
//
// # Only estimated tax so far
//
// There is no income tax calculator here yet. A caller passes the year's DC
// tax in as [estimated.Figures.Tax]: D-40 line 24 less line 30, the tax after
// the non-refundable and refundable credits, and zero if the credits are
// larger. D-2210 line 1 starts from that figure.
//
// # How the District differs
//
// The District calls its charge underpayment interest, not a penalty. The
// threshold is $100, not $1,000. The prior-year test is 110% of last year's
// tax for everyone, and the form allows it only to a filer who lived in DC
// all of last year. D-2210 charges each period's cumulative underpayment at a
// factor it prints for the span to the next due date, rather than interest by
// the day; the statute says 10% a year compounded daily, over the days to
// payment rounded up to the end of the month. The form also excuses a balance
// under 10% of the year's tax, and spreads the annualised method by income
// rather than annualising it.
//
// # What is not built
//
// The District has no separate rule for farmers or fishermen, so they are
// under the general rules. The statute's exception for the year of the
// taxpayer's death (D.C. Code § 47-4203(e)(5)) is reported and not applied,
// because [estimated.Facts] carries no date of death. The District has no
// local income tax.
//
// Start at [EstimatedTax].
package districtofcolumbiataxcalc
