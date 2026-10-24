// Package alabamataxcalc computes Alabama's estimated individual income tax
// for 2026: whether estimated payments are needed, what is due by each date,
// and the underpayment penalty.
//
// # Only estimated tax so far
//
// There is no income tax calculator here yet. A caller passes the year's tax
// in as [estimated.Figures.Tax]: Form 2210AL Part I line 3, which is the net
// tax after credits (Form 40 line 18, or Form 40NR line 20) less the other
// payments on Schedule CP, Section B, line 1. Last year's Alabama AGI goes in
// the prior return's [estimated.Figures.StateAGI].
//
// # How Alabama differs
//
// Code of Ala. § 40-18-80 adopts the federal rules, 26 U.S.C. § 6654, with a
// $500 threshold in place of $1,000. The department's rate follows the
// federal underpayment rate each quarter. The forms leave out three rules the
// statute keeps: the January 31 file-and-pay rule, the exception for a year
// with no liability, and the rules for nonresident aliens. The figures follow
// the forms, and each result's Divergences say where the statute reads
// otherwise.
//
// # What is not built
//
// Farmers and fishermen, who file Form 2210AL Part IV, get
// [estimated.ErrUnsupported]. Local occupational taxes are not covered; no
// source found shows that any of them takes estimated payments.
//
// Start at [EstimatedTax].
package alabamataxcalc
