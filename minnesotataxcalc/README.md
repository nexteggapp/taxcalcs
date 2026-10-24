# minnesotataxcalc

Minnesota estimated income tax for 2026: whether a household has to make
estimated payments, what is due by each date, and the underpayment penalty if
the payments go as planned. It implements the shared
[`estimated.EstimatedTax`](../estimated/estimated.go) interface.

**There is no Minnesota income tax calculator here yet.** The caller supplies
the year's tax, Form M1 line 17.

## Minnesota follows the federal shape, with these differences

| Rule | Minnesota | Where |
|---|---|---|
| Who must pay | $500 or more of tax after withholding and refundable credits | [Schedule M15](https://www.revenue.state.mn.us/sites/default/files/2026-10/m15-26.pdf) line 3 |
| The two tests | 90% of this year's tax, or 100% of last year's; 110% above $150,000 of last year's AGI, with no separate figure for married filing separately | M15 lines 4 and 5 |
| Credits | Refundable credits count as payments, spread evenly like withholding | M15 line 14; Minn. Stat. § 289A.25 subd. 8 |
| No-liability exception | Needs a full year of Minnesota residence last year | M15 instructions; § 289A.25 subd. 6 |
| The rate | One rate for the whole tax year, 7% for 2026, by the day ÷ 365 | M15 line 20; Minn. Stat. § 270C.40 subd. 4 |
| A shortfall paid in part | The whole shortfall is charged until it is paid in full | M15 line 17 |
| Filed by January 31 | The tax paid with the return counts as paid on January 15 | M15 line 14 instructions |

The code is in [`estimated.go`](estimated.go), and the 2026 figures in
[`estimated_y2026.go`](estimated_y2026.go). The 2026 M15 is marked "FINAL
DRAFT — 10/1/26".

## The form is followed, and where the statute differs is reported

Each result's `Divergences` lists seven places where the department's
publications and the statutes read differently, among them the line 17 rule,
the January 31 rule and the optional short method's 2% factor. The figures
follow the form. The research behind them is in
[`research/estimated-payments/mn.md`](../research/estimated-payments/mn.md).

## What it needs that no other state does

`EstimatedFacts`, carried in `estimated.Facts.State()`:

```go
mn, err := minnesotataxcalc.NewEstimatedFacts().
    RefundableCredits(0).                // Form M1 line 22
    PriorYearStepOnMinnesotaShare(true). // only asked of a part-year resident
    Build()
```

A fact left out is reported in `Required` with the default used.

## Not built

Farmers and commercial fishermen get `estimated.ErrUnsupported`. The optional
short method is not computed.
