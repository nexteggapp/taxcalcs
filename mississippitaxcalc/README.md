# mississippitaxcalc

Mississippi estimated income tax for 2026: whether a household has to make
estimated payments, what is due by each date, and the interest on an
underestimate if the payments go as planned. It implements the shared
[`estimated.EstimatedTax`](../estimated/estimated.go) interface.

**There is no Mississippi income tax calculator here yet.** The caller
supplies the year's liability, Form 80-105 line 20.

## Mississippi is shaped differently from federal

| Rule | Mississippi | Where |
|---|---|---|
| Who must pay | Liability over $200, with less than 80% of it withheld | [Form 80-106](https://www.dor.ms.gov/sites/default/files/tax-forms/individual/80106258%201.pdf) |
| The two tests | 80% of this year's liability, or 100% of last year's; no 110% tier | [Form 80-320](https://www.dor.ms.gov/sites/default/files/tax-forms/individual/80320258%201.pdf) lines 1 to 4 |
| Withholding | All of it counts toward April 15 | Form 80-320 line 6 |
| The charge | ½% a month on the running underestimate, from each due date to the next or until paid; the last column runs to April 15 | Form 80-320 lines 7 to 10; Miss. Code § 27-7-327(e) |
| Not there | Annualized method, January 31 rule, retirement or casualty waiver | |

The code is in [`estimated.go`](estimated.go), and the 2026 figures in
[`estimated_y2026.go`](estimated_y2026.go). No 2026 Form 80-320 is published;
the Rev. 09/25 form's arithmetic is used with the 2026 dates.

## The form is followed, and where the statute or rule differs is reported

Each result's `Divergences` lists three: Rule 100 still prints a 1% monthly
rate; the statute has no prior-year test at all; and the form and Rule 100(3)
treat a zero prior-year liability differently. The figures follow Form
80-320. The research behind them is in
[`research/estimated-payments/ms.md`](../research/estimated-payments/ms.md).

## What it needs that no other state does

`EstimatedFacts`, carried in `estimated.Facts.State()`:

```go
ms, err := mississippitaxcalc.NewEstimatedFacts().
    PartMonthCountsWhole(true).                          // how a part month is charged
    PriorYearIncomeAtThisYearsRates(3_000 * money.Dollar). // Rule 100(3), for a non-filer
    Build()
```

Both answer questions the published rules leave open. Left out, each is
reported in `Required` when it changes the figure, with the default used.

## Not built

Farmers and fishermen get `estimated.ErrUnsupported`.
