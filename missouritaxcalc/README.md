# missouritaxcalc

Missouri estimated income tax for 2026: whether a household has to make
estimated payments, what is due by each date, and the addition to tax for
underpayment if the payments go as planned. It implements the shared
[`estimated.EstimatedTax`](../estimated/estimated.go) interface.

**There is no Missouri income tax calculator here yet.** The caller supplies
the year's tax after credits, MO-2210 Part I line 1.

## Missouri keeps the older federal rules

| Rule | Missouri | Where |
|---|---|---|
| Who must pay | $100 of estimated tax after withholding; the penalty form stops under $500 | [MO-1040ES](https://dor.mo.gov/forms/MO-1040ES_2026.pdf); [MO-2210](https://dor.mo.gov/forms/MO-2210_2025.pdf) line 4 |
| The two tests | 90% of this year's tax, or 100% of last year's from a twelve-month return showing a liability; no 110% tier | MO-2210 Part I; RSMo 143.761.4(1) |
| Exceptions | Each installment is excused when payments reach last year's tax, last year's income at this year's rates, or (first three only) 90% of the tax on annualized or actual income to date | MO-2210 lines 20 to 24 |
| A late start | Fewer installments: three from June 15, two from September 15, one in January | MO-2210 instruction B; RSMo 143.531 |
| The rate | By the day ÷ 365, each calendar year at its own rate: 7% for 2026 and 2027 | MO-2210 lines 27a to 28b; RSMo 32.065 |
| Filed by January 31 | The balance counts as paid on January 15 | MO-2210 instruction H; RSMo 143.521.6 |

The code is in [`estimated.go`](estimated.go), and the 2026 figures in
[`estimated_y2026.go`](estimated_y2026.go). No 2026 MO-2210 is published; the
2025 form's arithmetic is used with the 2026 dates and rates.

## The form is followed, and where the statute differs is reported

Each result's `Divergences` lists five places. The largest: the statute
measures every underpayment against 90% of this year's tax and lets the
prior-year test only excuse an installment, as Pennsylvania's does, while the
form charges the shortfall against the smaller of the two. The research
behind the others is in
[`research/estimated-payments/mo.md`](../research/estimated-payments/mo.md).

## What it needs that no other state does

`EstimatedFacts`, carried in `estimated.Facts.State()`:

```go
mo, err := missouritaxcalc.NewEstimatedFacts().
    PriorYearIncomeAtThisYearsRates(4_000 * money.Dollar). // Exception 2
    TaxOnIncomeToDate(march, may, august).                 // Exception 4
    NoCombatZone().                                        // instruction I
    Build()
```

`StatutoryThreshold` and `LineFiveAsPrinted` answer two questions the form
and statute leave open. A fact left out is reported in `Required` when it
changes the figure, with the default used.

## Not built

Farmers get `estimated.ErrUnsupported`; fishing is not farming in Missouri.
The optional short method is not computed. Kansas City's and St. Louis's
earnings taxes are not covered.
