// Package northdakotataxcalc computes North Dakota's estimated income tax for
// 2026: whether estimated payments are needed, what is due by each date, and
// the interest on underpaid estimated tax.
//
// # Only estimated tax so far
//
// There is no income tax calculator here yet. A caller passes the tax in as
// [estimated.Figures.Tax], the net tax liability: Form ND-1 line 25, the tax
// less credits other than withholding and estimated payments.
//
// # How North Dakota differs
//
// Estimates are owed only by a filer who must pay federal estimated tax and
// whose North Dakota net tax liability last year was $1,000 or more. There is
// no 110% tier and no twelve-month test, and a filer who moved in last year
// must meet 90% of this year's tax. The interest is a fixed 12% a year.
// Schedule ND-1UT applies a payment to its own quarter first and rounds the
// day fraction to three places.
//
// # What is not built
//
// Qualified farmers, who use 66 2/3% and the March 1 waivers, get
// [estimated.ErrUnsupported]. Fiscal and short years, estates and trusts are
// outside it. North Dakota has no local income tax.
//
// Start at [EstimatedTax].
package northdakotataxcalc
