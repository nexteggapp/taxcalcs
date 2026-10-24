// Package vermonttaxcalc computes Vermont's estimated income tax for 2026:
// whether estimated payments are needed, what is due by each date, and the
// interest and penalty on an underpayment.
//
// # Only estimated tax so far
//
// There is no income tax calculator here yet. A caller passes this year's
// Vermont tax minus credits in as [estimated.Figures.Tax]: Form IN-111 line
// 20 minus line 26c, as IN-152 line 1 measures it. Withholding is lines 26a,
// 26d and 26e together. The Child Care Contribution, IN-111 line 21, is not
// part of that tax.
//
// # How Vermont differs
//
// The required annual payment is the lesser of 90% of this year's tax and
// 100% of last year's, with no 110% tier. No penalty or interest is charged
// when this year's tax less withholding is under $500. Each quarter's
// shortfall is charged interest by the day at the year's rate and a penalty
// of 1% for each month or part of a month, on the whole shortfall until it
// is paid in full. A filer with two-thirds of gross income from farming or
// fishing, this year or last, makes no estimated payments.
//
// # What is not built
//
// The Child Care Contribution's estimated payments are not covered. The
// annualised method needs the federal Schedule AI figures in
// [EstimatedFacts]; it does not read [estimated.Facts.Periods].
//
// Start at [EstimatedTax].
package vermonttaxcalc
