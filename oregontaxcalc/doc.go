// Package oregontaxcalc computes Oregon personal income tax, and Oregon's
// estimated tax and its underpayment interest.
//
// It is a library for writing tax software. A caller supplies figures and gets
// back a figure, the published rule that produced it, and any choice the caller
// still owes.
//
// # It does not import the federal calculators
//
// Oregon starts from federal adjusted gross income and subtracts part of the
// federal tax itself, so it needs two federal figures. Both are plain
// [money.USCents] arguments rather than a federal result type. Somebody holding
// them from a spreadsheet or another library can use these calculators.
//
// # Oregon rounds each bracket to a whole dollar
//
// This is the thing most likely to surprise. Oregon's published figures are not
// the exact arithmetic: the tax in each bracket is rounded to the nearest whole
// dollar and the rounded amounts are summed. A joint filer at $50,000 owes
// $3,756, where the exact sum is $3,755.00 and rounding the exact sum gives
// $3,755. Both published rate charts and all 502 bands of the published tax
// table agree with the per-bracket rule and disagree with the other two, which
// is what [OrdinaryIncomeTax] implements. See tables_test.go.
//
// # Oregon has no capital gains rate
//
// ORS 316.048 taxes Oregon taxable income at one set of rates, so a long-term
// gain is taxed like wages. There is deliberately no LongTermGainsTax here.
//
// # Rates are data, and a missing year is an error
//
// Every published figure is in a table keyed by tax year and filing status. A
// year with no table is [ErrNoTable] rather than the nearest year's figures.
// [Years] reports what this build carries.
//
// # Estimated tax
//
// [EstimatedTax] is Oregon's estimated tax rules for 2026, as the shared
// estimated.EstimatedTax interface: whether payments are needed, what is due
// by each date, and the underpayment interest. The prior-year test is 100% at
// every income. Farmers and fishers with two-thirds of their gross income
// from it owe nothing. Due dates move for Oregon's own legal holidays, which
// [EstimatedCalendar] returns. Form OR-10 charges interest on a running
// balance from one event to the next, each line rounded to a whole dollar,
// and that is what is computed. The caller passes the tax in as
// estimated.Figures.Tax, after all credits, with Oregon's own facts in
// [EstimatedFacts].
//
// Part-year residents get estimated.ErrUnsupported. Form OR-10's Exception 5,
// for a first-year S corporation shareholder, is not applied, and a change of
// filing status between the two years is left to the caller, who passes last
// year's tax already combined or allocated.
//
// Start at [TaxableIncome], [OrdinaryIncomeTax] and [FederalTaxSubtraction],
// or at [EstimatedTax].
package oregontaxcalc
