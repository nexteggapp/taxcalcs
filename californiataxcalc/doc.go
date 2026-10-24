// Package californiataxcalc computes California personal income tax.
//
// It is a library for writing tax software. A caller supplies figures and gets
// back a figure, the published rule that produced it, and any choice the caller
// still owes.
//
// # It does not import the federal calculators
//
// California starts from federal adjusted gross income, and this package takes
// that as a plain [money.USCents] argument rather than as a federal result
// type. Somebody holding the figure from a spreadsheet, a prior-year return or
// another library can use these calculators, and nothing here drags in the
// federal rate tables.
//
// # California has no capital gains rate
//
// Rev. & Tax. Code § 18031 conforms to the federal definition of gross income
// but not to 26 U.S.C. § 1(h), so a long-term gain is taxed at the same rates
// as wages. There is deliberately no LongTermGainsTax here. A caller must fold
// the whole gain into ordinary taxable income, and [TaxableIncome] does that
// when given it.
//
// # Rates are data, and a missing year is an error
//
// Every published figure is in a table keyed by tax year and filing status. A
// year with no table is [ErrNoTable] rather than the nearest year's figures.
// [Years] reports what this build carries.
//
// Start at [TaxableIncome], [OrdinaryIncomeTax] and [ExemptionCredit].
package californiataxcalc
