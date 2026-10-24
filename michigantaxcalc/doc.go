// Package michigantaxcalc computes Michigan's estimated income tax for 2026:
// whether estimated payments are needed, what is due by each date, and the
// interest and penalty on a shortfall.
//
// # Only estimated tax so far
//
// There is no income tax calculator here yet. A caller passes the year's tax
// in as [estimated.Figures.Tax], after credits: MI-1040 line 21 less lines 26,
// 27, 28b, 29 and 30, as the 2025 MI-2210 line 5 takes it.
//
// # How Michigan differs
//
// The required payment, the four equal quarters and the period interest runs
// follow the federal computation, which MCL 206.301(11) adopts. The charge is
// two things. Interest accrues by the day at a factor Treasury prints for each
// half-year, with a later payment applied to the oldest shortfall first. A
// flat penalty of 25% (at least $25) for a quarter with nothing paid, or 10%
// (at least $10) for one paid short, is figured on each quarter alone, with a
// payment counted only in the quarter it arrives. The exceptions for last
// year and the January 31 rule remove the flat penalty and leave the
// interest.
//
// # What is not built
//
// Farmers, fishermen and seafarers, with two-thirds of gross income from
// those activities, get [estimated.ErrUnsupported]. Fiduciaries are not
// covered.
//
// Michigan's city income taxes, Detroit's among them, take their own
// estimated payments under their own thresholds, dates and penalties, and are
// out of scope here.
//
// Start at [EstimatedTax].
package michigantaxcalc
