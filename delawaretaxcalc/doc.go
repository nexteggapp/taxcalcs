// Package delawaretaxcalc computes Delaware's estimated personal income tax
// for 2026: whether estimated payments are needed, what is due by each date,
// and the underpayment penalty.
//
// # Only estimated tax so far
//
// There is no income tax calculator here yet. A caller passes the year's
// Delaware tax in as [estimated.Figures.Tax]: the tax after credits, Form
// PIT-RES line 33 less line 34, or PIT-NON line 48. A nonresident's tax is
// the prorated figure PIT-NON computes. For the annualised method the caller
// passes [estimated.Period.AnnualisedTax] as PIT-UND Part 3 line 18: the tax
// on each period's annualised Delaware income, with the full standard
// deduction and the personal credits taken in every period.
//
// # How Delaware differs
//
// A declaration is needed above $800 of tax after withholding, and the
// penalty form stops when the required annual payment less withholding is
// $800 or less. The tests are 90% of this year's tax or 100% of last year's,
// 110% above $150,000 of last year's AGI ($75,000 for filing status 3). The
// first installment is due April 30. Withholding counts in the period it was
// withheld. The penalty is 1 1/2% a month in the statute and 0.0005 a day on
// the form, to April 30 of the next year. A part-year resident owes no
// penalty.
//
// # What is not built
//
// Farmers and fishermen, who pay 66 2/3% by January 15 or file and pay by
// March 1, get [estimated.ErrUnsupported]. S corporation payments and
// refundable business credits, which PIT-UND spreads evenly over the four
// quarters, have no place in [estimated.Facts]. Short tax years are not
// covered. The City of Wilmington's earned income and net profits taxes are
// not covered; whether either takes estimated payments was not found.
//
// Start at [EstimatedTax].
package delawaretaxcalc
