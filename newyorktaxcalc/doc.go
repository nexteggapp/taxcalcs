// Package newyorktaxcalc computes New York's estimated income tax for 2026:
// whether estimated payments are needed, what is due by each date, and the
// underpayment penalty. One computation covers the New York State, New York
// City and Yonkers income taxes and the MCTMT, as Forms IT-2105 and
// IT-2105.9 do.
//
// # Only estimated tax so far
//
// There is no income tax calculator here yet. A caller passes the four
// taxes' total as [estimated.Figures.Tax], the City and Yonkers part as
// [estimated.Figures.LocalTax], last year's New York AGI as
// [estimated.Figures.StateAGI], and which taxes apply in [EstimatedFacts].
//
// # How New York differs
//
// The threshold is $300 for each of the state, City and Yonkers taxes. The
// prior-year test becomes 110% above $150,000 of last year's New York AGI or
// MCTD self-employment earnings. The rate is New York's own, the federal
// short-term rate plus 5½ points with a 7½% floor, set each quarter and
// charged as simple interest.
//
// # What is not built
//
// Farmers and fishermen, who file federal Form 2210F, get
// [estimated.ErrUnsupported]. The exception for an installment due after
// the taxpayer's death, the 90-day extension after a spouse's death, and
// estates and trusts are not covered. New York City's business taxes are
// separate and not covered.
//
// Start at [EstimatedTax].
package newyorktaxcalc
