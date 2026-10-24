// Package arkansastaxcalc computes Arkansas's estimated individual income tax
// for 2026: whether estimated payments are needed, what is due by each date,
// and the underpayment penalty.
//
// # Only estimated tax so far
//
// There is no income tax calculator here yet. A caller passes the year's
// Arkansas net tax, Form AR1000F line 38 or AR1000NR line 38D, in as
// [estimated.Figures.Tax].
//
// # How Arkansas differs
//
// The target is the smaller of 90% of this year's net tax and 100% of last
// year's, with no 110% tier at any income. There is no penalty when this
// year's tax less Arkansas withholding is $1,000 or less. The penalty is a
// fixed 10% a year on each quarter's shortfall, by the day, to April 15 of the
// next year. Form AR2210 treats every payment made by the January due date as
// paid in four equal parts on the due dates, and treats a return filed and
// paid by January 31 as removing the penalty for the year.
//
// # Built on documents, not the Code
//
// The Arkansas Code's estimated tax sections were not retrieved, because the
// official Code is hosted by a commercial publisher. The rules follow the
// Department of Finance and Administration's forms: AR1000ES for 2026 and
// AR2210, its instructions and AR2210A for 2025. Where the forms and the
// instructions' restatement of the law disagree, the result's Divergences say
// so, and what the documents leave open is reported in Required.
//
// # What is not built
//
// Farmers with two-thirds of gross income from farming, who pay by February
// 15 or file by March 15, get [estimated.ErrUnsupported]. Local income taxes
// are not covered; the department's documents mention none.
//
// Start at [EstimatedTax].
package arkansastaxcalc
