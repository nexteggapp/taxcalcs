# montanataxcalc

Montana estimated income tax for 2026: whether a household has to make
estimated payments, what is due by each date, and the interest on
underpayment of estimated tax if the payments go as planned. It implements
the shared [`estimated.EstimatedTax`](../estimated/estimated.go) interface.

**There is no Montana income tax calculator here yet.** The caller supplies
the year's tax after nonrefundable credits, Form EST-I Part I line 1.

## Montana follows the federal shape, with these differences

| Rule | Montana | Where |
|---|---|---|
| Who must pay | $500 or more after withholding and credits | Mont. Code Ann. § 15-30-2512(1)(b)(i); [EST-I](https://revenuefiles.mt.gov/files/Forms/EST-I/EST-I-2025.pdf) line 4 |
| The two tests | 90% of this year's tax, or 100% of last year's from a filed twelve-month return; no 110% tier | § 15-30-2512(1)(a) |
| Exempt outright | Farmers and ranchers; retirement after 62 or disability this year or last; no liability last year; a nonresident not required to file last year; a composite return participant | § 15-30-2512(1)(b), (6); ARM 42.17.314 |
| The rate | One rate for the whole year, the one in effect when the return is due: 2027's, 7%, at the printed 0.000192 a day | § 15-1-216(7); EST-I line 14; [Publication 1](https://revenuefiles.mt.gov/files/Forms/Publication-1/Publication-1-2026.pdf) |
| January 31 | Filing and paying by then takes the interest off January; in 2027 it moves to Monday, February 1 | § 15-30-2512(5)(d); §§ 1-1-216, 1-1-307 |

The code is in [`estimated.go`](estimated.go), and the 2026 figures in
[`estimated_y2026.go`](estimated_y2026.go). No 2026 EST-I is published; the
2025 form's arithmetic is used with the 2026 dates.

## The form is followed, and where the statute differs is reported

Each result's `Divergences` lists seven places where EST-I, Publication 1 and
the statutes read differently, among them the printed daily factor, the
statute's approximately-equal-installments exception the form omits, and the
1.5% monthly late payment penalty the form does not compute. The research
behind them is in
[`research/estimated-payments/mt.md`](../research/estimated-payments/mt.md).

## What it needs that no other state does

`EstimatedFacts`, carried in `estimated.Facts.State()`:

```go
mt, err := montanataxcalc.NewEstimatedFacts().
    RefundableCredits(0).                  // Form 2 lines 15 to 17
    CompositeReturn(false).                // ARM 42.17.314(5)
    ApproximatelyEqualInstallments(false). // § 15-30-2512(5)(e)
    Build()
```

A fact left out is reported in `Required` with the default used.
Retirement and disability come from `estimated.Person`.

## Not built

The hardship extension and short tax years. Montana has no local income tax.
