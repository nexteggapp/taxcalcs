// Package kentuckytaxcalc computes Kentucky's estimated individual income tax
// for 2026: whether estimated payments are needed, what is due by each date,
// and the underpayment penalty.
//
// # Only estimated tax so far
//
// There is no income tax calculator here yet. A caller passes this year's
// Kentucky tax as [estimated.Figures.Tax], after credits including refundable
// ones, as Form 2210-K line 5 measures it, and last year's Kentucky AGI as
// [estimated.Figures.StateAGI] on the prior return.
//
// # How Kentucky differs
//
// KRS 141.305 adopts the federal rules, so the shape is federal: 90% of this
// year's tax or 100% of last year's, 110% above $150,000 of last year's
// Kentucky AGI, four quarters, and interest by the day to April 15. The
// threshold is $500, and payments are needed only with more than $5,000 of
// income without Kentucky withholding or AGI at the filing threshold. The
// rate is fixed for the calendar year. The January voucher is excused for
// filing early only when the requirement arose after September 1, and
// withholding is always spread evenly.
//
// # The 2026 rate is not settled
//
// Form 2210-K charges 9% for 2026. The department set the 2026 tax interest
// rate at 7%, and KRS 141.305(3)(d) can be read to use it. The figure follows
// the form; the result's Divergences give both readings and Required reports
// the question.
//
// # What is not built
//
// Farmers and fishers with two-thirds of gross income from farming or fishing
// get [estimated.ErrUnsupported]. Decedents' estates and trusts are not
// individuals and are not covered. Local occupational license taxes are not
// administered by the Department of Revenue and are not covered.
//
// Start at [EstimatedTax].
package kentuckytaxcalc
