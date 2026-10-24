// Package hawaiitaxcalc computes Hawaii's estimated individual income tax for
// 2026: whether estimated payments are needed, what is due by each date, and
// the underpayment penalty.
//
// # Only estimated tax so far
//
// There is no income tax calculator here yet. A caller passes the year's
// Hawaii tax in as [estimated.Figures.Tax]: the tax after every credit,
// refundable or not, but before withholding and estimated payments, which is
// Form N-210 line 3. For the annualised method the caller passes the tax after
// credits on each period's annualised income, N-210 Schedule A line 16, as
// [estimated.Period.AnnualisedTax].
//
// # How Hawaii differs
//
// The current-year test is 60% of this year's tax, not 90%. The prior-year
// test is 100% of last year's Hawaii tax at every income, with no 110% step,
// and it needs a Hawaii return for a full twelve-month year. There is no
// penalty when this year's tax less withholding is under $500. The four
// installments fall on the 20th, not the 15th: April 20, June 20, September 20
// and January 20. The penalty is a charge of two-thirds of one percent of the
// shortfall for each month or part of a month it stays unpaid, with months
// running from the 21st to the 20th, to April 20 of the next year.
//
// # What is not built
//
// Farmers and fishermen, who pay once by January 20 or file and pay by March 1
// under HRS § 235-97(f)(4) and 26 U.S.C. § 6654(i), get
// [estimated.ErrUnsupported]. So do nonresident aliens without Hawaii wage
// withholding, who pay one-half by June 20 under § 6654(j). Fiscal years,
// estates and trusts, and the deferral for service in a combat zone are not
// covered. The department's documents describe no county income tax.
//
// Start at [EstimatedTax].
package hawaiitaxcalc
