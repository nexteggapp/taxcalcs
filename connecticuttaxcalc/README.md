# connecticuttaxcalc

Connecticut estimated income tax for 2026: whether a household has to make
estimated payments, what is due by each date, and the interest Connecticut
charges if the payments go as planned. It implements the shared
[`estimated.EstimatedTax`](../estimated/estimated.go) interface, so a registry
can return it for `"CT"` and 2026.

**There is no Connecticut income tax calculator here yet.** The caller supplies
the year's tax, Form CT-1040 line 14.

## Connecticut's tests are federal; its charge is not

| Rule | Connecticut | Where |
|---|---|---|
| Who must pay | $1,000 or more of tax after withholding and the Pass-Through Entity Tax Credit | [CT-1040ES (Rev. 01/26)](https://portal.ct.gov/-/media/drs/forms/2025/income/ct1040es-flat0126.pdf); Conn. Gen. Stat. § 12-722(j)(1) |
| The two tests | 90% of this year's tax, or 100% of the tax on last year's 12-month return. No 110% step at any income | CT-1040ES worksheet lines 13 to 15; § 12-701(a)(12) |
| Due dates | A quarter each on April 15, June 15, September 15 and January 15; none moves in 2026 | CT-1040ES, When to File; § 12-722(c), (d)(1) |
| No liability last year | No interest, for a resident, or a nonresident with Connecticut-source income. Without that income, 90% of this year's tax | [CT-2210 (Rev. 12/25)](https://portal.ct.gov/-/media/drs/forms/2025/income/ct-2210_1225.pdf) Boxes E, F and G; § 12-722(j)(2) |
| The PE Tax Credit | Counted like withholding, a quarter by each date | CT-2210 Part 3 lines 12 to 13 |
| The charge | Interest of 1% for each month or part of a month, the months running from the 16th to the 15th, to April 15, 2027 | CT-2210 Schedule B; § 12-722(a), (b) |
| Waivers | None for casualty, disaster, retirement or disability | § 12-722; CT-2210 |

The code is in [`estimated.go`](estimated.go), and the 2026 figures in
[`estimated_y2026.go`](estimated_y2026.go).

## The forms are followed, and where the statute differs is reported

Each result's `Divergences` lists three places where the department's forms and
§ 12-722 read differently: the PE Tax Credit in the $1,000 test, the PE Tax
Credit as a payment by each due date, and how far a return filed and paid by
January 31 reaches. The figures follow the forms. The research behind each is
in [`research/estimated-payments/ct.md`](../research/estimated-payments/ct.md).

## What it needs that no other state does

`EstimatedFacts`, carried in `estimated.Facts.State()`:

```go
ct, err := connecticuttaxcalc.NewEstimatedFacts().
    PETaxCredit(2_000 * money.Dollar).         // Schedule CT-PE line 1
    PriorYearConnecticutSourceIncome(false).   // only read for a nonresident last year
    Build()
```

A fact left out is reported in `Required` with the default used.

## Not built

Farmers and fishermen (§ 12-722(m)) get `estimated.ErrUnsupported`. Trusts and
estates are not covered. No local income tax is covered.
