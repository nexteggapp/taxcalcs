// Package rhodeislandtaxcalc computes Rhode Island's estimated personal
// income tax for 2026: whether estimated payments are needed, what is due by
// each date, and the interest charged on an underpayment.
//
// # Only estimated tax so far
//
// There is no income tax calculator here yet. A caller passes this year's tax
// in as [estimated.Figures.Tax]: RI-1040 line 10a less lines 14d and 14e, or
// RI-1040NR line 13a less line 17d, as RI-2210 line 1 takes it.
//
// # How Rhode Island differs
//
// The required annual payment is the smaller of 80% of this year's tax and
// 100% of last year's, at every income. There is no charge when this year's
// tax less withholding is $250 or less. The charge is simple interest at a
// rate set once for each calendar year, 12% for 2026, and due dates move for
// Rhode Island's own legal holidays. The forms are followed, and each result's
// Divergences say where the General Laws read otherwise.
//
// # What is not built
//
// Farmers and fishermen, who use 66.67% and a single January installment,
// get [estimated.ErrUnsupported]. Estates and trusts are not covered. The
// RI-2210A shortcut is not offered. For anyone who may use it, the full
// RI-2210 computation gives the same answer, apart from the rounding of the
// shortcut's printed factors.
//
// Start at [EstimatedTax].
package rhodeislandtaxcalc
