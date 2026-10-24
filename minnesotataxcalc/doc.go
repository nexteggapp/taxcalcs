// Package minnesotataxcalc computes Minnesota's estimated income tax for
// 2026: whether estimated payments are needed, what is due by each date, and
// the underpayment penalty.
//
// # Only estimated tax so far
//
// There is no income tax calculator here yet. A caller passes the tax in as
// [estimated.Figures.Tax], Form M1 line 17, and the refundable credits, Form
// M1 line 22, in [EstimatedFacts].
//
// # How Minnesota differs
//
// The threshold is $500 of tax after withholding and refundable credits.
// Refundable credits count as payments spread evenly over the four dates. The
// no-liability exception needs a full year of Minnesota residence last year.
// The rate is fixed once for the whole tax year, 7% for 2026. Schedule M15
// charges each quarter's whole shortfall until it is paid in full, and counts
// the tax paid with a return filed by January 31 as paid on January 15.
//
// # What is not built
//
// Farmers and commercial fishermen, who make one payment by January 15 at
// 66.7%, get [estimated.ErrUnsupported]. The optional short method is not
// computed; the regular method is, and the result reports how the two differ.
//
// Start at [EstimatedTax].
package minnesotataxcalc
