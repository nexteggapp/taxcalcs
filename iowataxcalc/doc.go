// Package iowataxcalc computes Iowa's estimated individual income tax for
// 2026: whether estimated payments are needed, what is due by each date, and
// the underpayment penalty.
//
// # Only estimated tax so far
//
// There is no income tax calculator here yet. A caller passes the tax in as
// [estimated.Figures.Tax]: IA 2210 line 3, which is IA 1040 line 18 less the
// refundable credits on lines 23 to 27. Line 18 comes before the school
// district and EMS surtax on line 19, so the surtax is not in it, as IA 2210
// line 1 leaves it out. Last year's tax is the same figure from last year's
// return. A filer under the low-income exemption, or a nonresident whose Iowa
// income is all withheld, composite or PTET income, passes the tax that
// leaves.
//
// # How Iowa differs
//
// Iowa Code § 422.16(12)(d)(3) adopts the federal method at Iowa's interest
// rate, so the shape is federal: the smaller of 90% of this year's tax and
// 100% or 110% of last year's, four equal installments, interest by the day.
// The installments fall at the end of April, June, September and January; the
// threshold is $1,000 from 2026; a filer who first meets the requirement after
// April 1 pays in fewer installments; the 110% test adds Iowa's net
// modifications to federal AGI, which [EstimatedFacts] carries; and the
// penalty runs to April 30.
//
// # What is not built
//
// Farmers and fishers, who file IA 2210F, and the annualised method on IA
// 2210 Schedule AI get [estimated.ErrUnsupported]. The optional short method
// on IA 2210 lines 9 to 16 is not computed; the regular method is. Fiscal-year
// filers are not covered.
//
// Start at [EstimatedTax].
package iowataxcalc
