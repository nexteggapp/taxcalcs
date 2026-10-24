// Package marylandtaxcalc computes Maryland's estimated income tax for 2026:
// whether estimated payments are needed, what is due by each date, and the
// interest on an underpayment.
//
// # Only estimated tax so far
//
// There is no income tax calculator here yet. A caller passes the year's
// Maryland and local tax after credits as [estimated.Figures.Tax], which is
// Form 502UP line 7, and the county or Baltimore City tax inside it as
// [estimated.Figures.LocalTax]. Last year's figure is Form 502UP line 9a.
//
// # How Maryland differs
//
// The threshold is $500 of tax after withholding, county tax included. The
// prior-year test is 110% for everyone. Withholding counts on the dates it
// was withheld. Uneven income is handled by the share of the year's income
// received by each period's end, not by annualising. Form 502UP's columns are
// cumulative, so a shortfall still unpaid is charged again at every later
// date. The charge is interest at a rate set for each calendar year.
//
// # What is not built
//
// Farmers and fishermen, who pay once under Form 502UP's special
// calculation, get [estimated.ErrUnsupported]. The rule that $500 or more of
// unwithheld gambling or lottery winnings be declared and paid within 60 days
// is not applied, because [estimated.Facts] carries no such income. The
// separate penalty of up to 25% under Tax-General § 13-702 is not computed,
// as Form 502UP does not compute it.
//
// Start at [EstimatedTax].
package marylandtaxcalc
