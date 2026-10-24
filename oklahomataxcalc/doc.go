// Package oklahomataxcalc computes Oklahoma's estimated income tax for 2026:
// whether estimated payments are needed, what is due by each date, and the
// interest on an underpayment.
//
// # Only estimated tax so far
//
// There is no income tax calculator here yet. A caller passes the year's
// Oklahoma tax after credits in as [estimated.Figures.Tax]: Form 511 line 40's
// "tax due less all credits except amounts paid on withholding, estimated
// tax, and extension payments", or OW-8-P line 3.
//
// # How Oklahoma differs
//
// The shape is federal and the numbers are not. Payments are needed from
// $500 of tax over withholding. The required annual payment is the lesser of
// 70% of this year's tax and 100% of last year's, at every income. The four
// installments are equal. The charge is called interest and is 20% a year,
// fixed by 68 O.S. § 2385.13(A), by the day. There is no interest on tax
// under $1,000, and none for a filer who was not required to file last
// year. Farmers with two-thirds of gross income from farming this year or
// last are not required to pay at all, and are built.
//
// The figures follow the Tax Commission's forms, and each result's
// Divergences lists six places where the statutes read otherwise.
//
// # What is not built
//
// Fiscal-year filers and estates, which [estimated.Facts] cannot describe.
// The January 31 waiver of the January installment in § 2385.9(A)(1), which
// the forms do not apply; it is reported, not applied. Due dates postponed by
// a Commission order after a disaster. No Oklahoma city income tax was found,
// so none is covered.
//
// Start at [EstimatedTax].
package oklahomataxcalc
