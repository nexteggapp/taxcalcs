# georgiataxcalc

Georgia estimated income tax for individuals for 2026: whether a household has
to make estimated payments, what is due by each date, and the underpayment
penalty if the payments go as planned. It implements the shared
[`estimated.EstimatedTax`](../estimated/estimated.go) interface.

**There is no Georgia income tax calculator here yet.** The caller supplies the
year's tax.

## Georgia is shaped differently from federal

| Rule | Georgia | Where |
|---|---|---|
| Who must pay | Gross income over the dependent exemptions ($5,000 each), the estimated deductions ($15,000 standard, $30,000 joint) and $1,000 | [Form 500-ES](https://dor.georgia.gov/document/document/2026-500-es-estimated-tax-individuals-and-fiduciaries/download); [HB 463 (2026)](https://www.legis.ga.gov/api/legislation/document/20252026/249080) |
| The two tests | 70% of this year's tax or 100% of last year's. No 110% tier | [Form 500 UET](https://dor.georgia.gov/document/document/beginning-or-after-january-1-2025-500-uet-underpayment-estimated-tax-individuals/download) lines 4 to 8 |
| The installments | Equal, on April 15, June 15, September 15 and January 15; fewer for a late starter | Form 500 UET Instruction B |
| Which shortfall a payment pays | Its own period's installment first; only the excess makes up an earlier one | Form 500 UET lines 9 to 12, and its example |
| Exceptions | Last year's income at this year's 4.99%; 70% of the tax on annualised income; 90% of the tax on income to date, not for January | Form 500 UET Instruction D |
| January 31 | A return filed and paid by then counts as paid on January 15 | Form 500 UET Instruction F |
| The penalty | 9% a year, by the day, to April 15 | Form 500 UET line 20 |

The code is in [`estimated.go`](estimated.go), and the 2026 figures in
[`estimated_y2026.go`](estimated_y2026.go).

## The statute could not be read

O.C.G.A. §§ 48-7-114 to 48-7-120 are published officially only through
LexisNexis. Every rule follows the Department's forms. The one divergence
reported is the 500-ES printing 2025's deduction and exemption, which HB 463
raised for 2026; the act's figures are used. Where the forms leave a question
open, such as whether last year's return must have been filed or which payment
pays an earlier shortfall, the result's `Required` names the default used. The
research is in
[`research/estimated-payments/ga.md`](../research/estimated-payments/ga.md).

## What it needs that no other state does

`EstimatedFacts`, carried in `estimated.Facts.State()`:

```go
ga, err := georgiataxcalc.NewEstimatedFacts().
    Dependents(2).                     // dependent exemptions on Form 500
    Deductions(15_000 * money.Dollar). // estimated deductions, standard or itemised
    Build()
```

## Not built

Farmers and fishermen get `estimated.ErrUnsupported`. Exception 4, for estates
and testamentary trusts, and fiscal years are not covered.
