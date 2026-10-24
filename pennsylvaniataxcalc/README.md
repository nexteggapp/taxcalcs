# pennsylvaniataxcalc

Pennsylvania estimated personal income tax for 2026: whether a household has
to make estimated payments, what is due by each date, and the underpayment
penalty if the payments go as planned. It implements the shared
[`estimated.EstimatedTax`](../estimated/estimated.go) interface, so
`estimated/all.For("PA", 2026)` finds it.

**There is no Pennsylvania income tax calculator here yet.** The caller supplies
the year's tax.

## Pennsylvania is shaped differently from federal

| Rule | Pennsylvania | Where |
|---|---|---|
| Who must pay | $14,000 or more of income without PA withholding, and $430 or more of tax after withholding and credits | [REV-413 (I)](https://www.pa.gov/content/dam/copapwp-pagov/en/revenue/documents/formsandpublications/formsforindividuals/pit/documents/2026/2026_rev-413i.pdf) |
| The two tests | 90% of this year's tax before credits, less the credits; or last year's taxable income at 3.07% less last year's Tax Forgiveness. No 110% tier | REV-413 (I) lines 9 to 14; [REV-1630](https://www.pa.gov/content/dam/copapwp-pagov/en/revenue/documents/formsandpublications/formsforindividuals/pit/documents/2025/2025_rev-1630.pdf) |
| What the penalty is on | The shortfall against 90% of this year's tax, every quarter. The prior-year test and the income-to-date test excuse a quarter; they do not lower it | REV-1630 Sections I and II |
| A late start | A quarter is still due on each date; the earlier ones are excused only if the filer owed no estimates last year and pays the whole catch-up by the first date that applies | REV-1630, Determining the Underpayment Amount |
| The interest | 0.000192 a day for 2026, in whole dollars, to April 15 | REV-1630 Section III; [REV-1611](https://www.pa.gov/content/dam/copapwp-pagov/en/revenue/documents/formsandpublications/otherforms/documents/rev-1611.pdf) |

The code is in [`estimated.go`](estimated.go), and the 2026 figures in
[`estimated_y2026.go`](estimated_y2026.go).

## The forms are followed, and where the statute differs is reported

Each result's `Divergences` lists five places where the department's forms and
the Tax Reform Code read differently: the $14,000 boundary, the late-start
installments, the January 31 return, the prior-year test after a part-year
return, and the printed daily factor. The figures follow the forms. The
research behind each is in
[`research/estimated-payments/pa.md`](../research/estimated-payments/pa.md).

## What it needs that no other state does

`EstimatedFacts`, carried in `estimated.Facts.State()`:

```go
pa, err := pennsylvaniataxcalc.NewEstimatedFacts().
    IncomeWithoutWithholding(60_000 * money.Dollar). // REV-414 (I) line 1
    Credits(0).                                      // PA-40 lines 21 to 23
    PriorYearForgiveness(0).                         // last year's PA-40 line 21
    PriorYearFullForgiveness(false).
    Build()
```

A fact left out is reported in `Required` with the default used.

## Not built

Farmers (REV-1630A) get `estimated.ErrUnsupported`. The exception for
installments due on or after the taxpayer's death is not applied. Local earned
income taxes are not covered.
