// Package ustaxcalc computes United States federal income tax.
//
// It is a library for writing tax software. A caller supplies figures and gets
// back a figure, the published rule that produced it, and any choice the caller
// still owes.
//
// # Facts and positions
//
// Part of a tax figure is arithmetic on numbers. The rest depends on a choice
// the numbers do not determine: whether to itemise, which lots a sale consumed,
// whether a traditional IRA contribution is deductible given workplace-plan
// coverage, whether two securities are substantially identical for the
// wash-sale rule, whether an activity is a trade or business.
//
// A calculator never picks one of those silently. It either takes the position
// as an argument or returns it in [Result.Required], and the caller decides
// what that means. A tax product turns it into a question. A preparer's script
// reads it from a configuration. Something else renders a form field.
//
// # Rates are data, and a missing year is an error
//
// Every published figure is in a table keyed by tax year and filing status. A
// year with no table is [ErrNoTable] rather than the nearest year's figures,
// because a table that is right in December is wrong in January and a silent
// fallback produces a confident number under the wrong law.
//
// # Money
//
// [USCents] is an integer count of cents and [Rate] is an integer count of basis
// points, so nothing here is a binary float. Tax figures are summed, and 0.10
// has no exact float representation.
//
// Start at [OrdinaryIncomeTax], [LongTermGainsTax], [SelfEmploymentTax] and
// [RequiredAnnualPayment]. [NetCapitalGains] nets Schedule D, and
// [TaxOnIncome] puts qualified dividends and net capital gain through the
// gains rates and the rest through the bands.
package ustaxcalc
