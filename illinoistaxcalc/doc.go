// Package illinoistaxcalc computes Illinois's estimated individual income tax
// for 2026: whether estimated payments are needed, what is due by each date,
// and the late-payment penalty for underpaying them.
//
// # Only estimated tax so far
//
// There is no income tax calculator here yet. The caller passes this year's
// and last year's tax in as [estimated.Figures.Tax]: Form IL-1040 line 14
// plus the line 22 surcharges, less the credits on lines 15, 16, 17, 28, 29
// and 30, as IL-2210 Step 2 lines 1 to 3 take it. Pass-through withholding,
// line 27, goes in [EstimatedFacts].
//
// # How Illinois differs
//
// The penalty is a flat 2% of an amount paid 1 to 30 days late and 10% of an
// amount paid later or not at all, not interest by the day. There is no 110%
// prior-year tier. Farmers owe no estimates, nor does a filer 65 or older
// living permanently in a nursing home. There is no January 31 filing waiver.
// Whether interest also runs on a late installment is not settled by any
// Illinois document; it is not charged unless [EstimatedFacts] asks for it.
//
// # What is not built
//
// The exceptions for underpayments after the taxpayer's death, combat-zone
// extensions, innocent spouses and bankruptcy estates (86 Ill. Adm. Code
// 100.8010(g)) are reported as divergences and not applied, because
// [estimated.Facts] does not carry them. A prior-year overpayment that came
// partly from payments made after last year's due date is treated as one
// credit. The late-payment penalty for tax unpaid with the return, IL-2210
// Penalty Worksheet 2, is not computed.
//
// Start at [EstimatedTax].
package illinoistaxcalc
