// Package arizonataxcalc computes Arizona's estimated individual income tax
// for 2026: whether estimated payments are needed, what is due by each date,
// and the underpayment penalty.
//
// # Only estimated tax so far
//
// There is no income tax calculator here yet. Arizona taxes income at one
// flat rate, 2.5% for 2026, and a caller passes the tax in as
// [estimated.Figures.Tax], after credits, as Form 221 line 4 has it.
//
// # How Arizona differs
//
// Whether payments are needed turns on income, not on tax: Arizona gross
// income over $75,000, or $150,000 on a joint return, both this year and last.
// Arizona gross income is federal adjusted gross income for a full-year
// resident, and the part reported to Arizona otherwise. The target is the
// smaller of 90% of this year's tax and 100% of last year's, with no 110% tier.
// There is no penalty when tax after withholding and credits is under $1,000,
// or when a federal exception under 26 U.S.C. § 6654 applies. The penalty is
// interest at the federal short-term rate plus three points, compounded on
// January 1, and never more than 10% of the underpayment.
//
// # What is not built
//
// Farmers and fishermen, who make one installment, and nonresident aliens,
// who make three, get [estimated.ErrUnsupported]. No Arizona holiday list is
// checked in; no 2026 due date falls on one.
//
// Start at [EstimatedTax].
package arizonataxcalc
