# oklahomataxcalc

Oklahoma estimated income tax for 2026: whether a household has to make
estimated payments, what is due by each date, and the interest on an
underpayment if the payments go as planned. It implements the shared
[`estimated.EstimatedTax`](../estimated/estimated.go) interface.

**There is no Oklahoma income tax calculator here yet.** The caller supplies
the year's tax after credits.

## The shape is federal and the numbers are not

| Rule | Oklahoma | Where |
|---|---|---|
| Who must pay | Tax $500 or more over withholding, and withholding under the required annual payment | [OW-8-ES](https://oklahoma.gov/content/dam/ok/en/tax/documents/forms/individuals/current/OW-8-ES.pdf); 68 O.S. § 2385.7(A) |
| Required annual payment | The lesser of 70% of this year's tax and 100% of last year's 12-month return. No 110% tier | OW-8-ES line 9; [OW-8-P](https://oklahoma.gov/content/dam/ok/en/tax/documents/forms/other/OW-8-P.pdf) lines 4 to 8; § 2385.9(B) |
| No interest | Tax under $1,000; tax under $500 over withholding; no return required last year; farmers with two-thirds of gross income from farming this year or last | OW-8-P lines 3, 6 and 7; § 2385.7(B), § 2385.13(C) |
| Due dates | April 15, June 15, September 15, January 15, a quarter each; moved for a weekend or a 25 O.S. § 82.1 holiday | OW-8-ES; § 2385.9(A) |
| The interest | 20% a year, by the day over 365, to April 15 of the next year, payments applied to the earliest balance | OW-8-P Part 3; § 2385.13(A), (B) |
| Annualised method | 17.5%, 35%, 52.5% and 70% | [OW-8-P-SUP-I](https://oklahoma.gov/content/dam/ok/en/tax/documents/forms/individuals/current/OW-8-P-SUP-I.pdf), [OW-8-ES-SUP](https://oklahoma.gov/content/dam/ok/en/tax/documents/forms/individuals/current/OW-8-ES-SUP.pdf) |

The code is in [`estimated.go`](estimated.go), and the 2026 figures in
[`estimated_y2026.go`](estimated_y2026.go).

## $10,000 of tax and nothing paid costs $931.10

70% of $10,000 is $7,000, so $1,750 is due on each date. Unpaid to April 15,
2027, the four quarters are charged $350.00, $291.51, $203.29 and $86.30
(`TestEstimatedInterestArithmetic`).

## The forms are followed, and where the statutes differ is reported

Each result's `Divergences` lists six places where the Tax Commission's forms
and the statutes read differently: no liability last year, the January 31
waiver, who must pay, married individuals, the $1,000 test and the farmer
share. The figures follow the forms. Questions the research could not settle
are reported in `Required` with the default used. The research is in
[`research/estimated-payments/ok.md`](../research/estimated-payments/ok.md).

## What it needs that no other state does

`EstimatedFacts`, carried in `estimated.Facts.State()`:

```go
ok, err := oklahomataxcalc.NewEstimatedFacts().
    PriorYearFarmShare(0). // last year's farm share of gross income
    Build()
```

Left out, it is reported in `Required` and taken as under two-thirds.

## Not built

Fiscal-year filers and estates, which the shared facts cannot describe. The
January 31 waiver, which the forms do not apply; it is reported. Due dates
postponed by a Commission order after a disaster.
