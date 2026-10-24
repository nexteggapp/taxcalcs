// Package ohiotaxcalc computes Ohio's estimated individual income tax for
// 2026: whether estimated payments are needed, what is due by each date, and
// the interest penalty on an underpayment.
//
// # Only estimated tax so far
//
// There is no income tax calculator here yet. A caller passes this year's
// Ohio tax after credits in as [estimated.Figures.Tax]: IT 1040 line 10 less
// line 16. The 2026 IT 1040ES worksheet prints 2025's brackets; compute the
// tax under ORC 5747.02(A)(3)(c) as amended by HB 96.
//
// # How Ohio differs
//
// The threshold is more than $500 of tax after withholding. Last year's tax
// counts as the target only after a timely Ohio return, which is the one
// fact [EstimatedFacts] carries. The department's form, IT/SD 2210, owes no
// penalty when the year's withholding and timely payments reach the target,
// however they were spread. Otherwise each due date's cumulative shortfall
// is charged at one factor to the next due date, the last to April 15, at a
// rate fixed for the calendar year and a year of 365.25 days.
//
// # What is not built
//
// Farmers and fishermen under Ohio Adm. Code 5703-7-04 get
// [estimated.ErrUnsupported]. The statute's annualised test and its choice
// to credit withholding on the dates withheld have no form, and are reported
// in Divergences but not applied. Servicemembers with an IRS extension, who
// owe no interest penalty for the extension period, are not modelled.
// School district income tax and city income taxes take their own estimates
// and are not covered, including the IT/SD 2210's combined computation.
//
// Start at [EstimatedTax].
package ohiotaxcalc
