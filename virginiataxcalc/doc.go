// Package virginiataxcalc computes Virginia's estimated individual income
// tax for 2026: whether estimated payments are needed, what is due by each
// date, and the addition to tax for an underpayment.
//
// # Only estimated tax so far
//
// There is no income tax calculator here yet. A caller passes the tax in as
// [estimated.Figures.Tax], after the Spouse Tax Adjustment and credits, as
// Form 760C line 1 takes it.
//
// # How Virginia differs
//
// The first installment is due May 1, and the addition runs to May 1 of the
// next year. Each installment is measured against the lesser of 90% of this
// year's tax and 100% of the tax on last year's return, with no 110% tier. A
// payment on an installment date pays that installment before an earlier
// shortfall. Four exceptions cancel the addition for an installment without
// lowering it, and there is no addition when the year's underpayments total
// $1,000 or less. The addition is interest at the federal underpayment rate
// plus 2%, charged at the daily .00025 Form 760C prints for 9%.
//
// # What is not built
//
// Farmers, fishermen and merchant seamen, who file Form 760F, get
// [estimated.ErrUnsupported]. Fiscal years and estates and trusts are not
// built.
//
// Start at [EstimatedTax].
package virginiataxcalc
