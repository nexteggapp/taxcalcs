// Package wisconsintaxcalc computes Wisconsin's estimated individual income
// tax for 2026: whether estimated payments are needed, what is due by each
// date, and the underpayment interest.
//
// # Only estimated tax so far
//
// There is no income tax calculator here yet. A caller passes the tax in as
// [estimated.Figures.Tax]: Schedule U line 3, the net tax (Form 1 line 22 or
// Form 1NPR line 52) less refundable credits. Last year's tax is the same
// figure from last year's return, combined or split as the Schedule U
// instructions say when the filing status changed.
//
// # How Wisconsin differs
//
// The threshold is $500 of tax after withholding. The required annual
// payment is the smaller of 90% of this year's tax and 100% of last year's,
// with no 110% tier. The charge is interest at 12% a year, set by Wis. Stat.
// § 71.84(1), by the day. Schedule U applies a payment first to the
// installment of its own period, and carries what is left back to the most
// recent earlier shortfall. A return filed and paid by January 31 replaces
// the January payment only when nothing was due by the earlier dates. The
// no-tax-last-year exception needs full-year Wisconsin residence.
//
// # What is not built
//
// Farmers and fishers, with two-thirds of gross income from farming or
// fishing in either year, get [estimated.ErrUnsupported]. Estates and trusts
// are not covered. Schedule U's short method is not used: the regular method
// is always allowed, and the two differ by cents.
//
// Start at [EstimatedTax].
package wisconsintaxcalc
