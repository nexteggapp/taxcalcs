# louisianataxcalc

Louisiana estimated individual income tax for 2026: whether a household has to
make estimated payments, what is due by each date, and the underpayment
penalty if the payments go as planned. It implements the shared
[`estimated.EstimatedTax`](../estimated/estimated.go) interface.

**There is no Louisiana income tax calculator here yet.** The caller supplies
the year's tax.

## Act 307 of 2026 is computed, not the older form

Act 307 changed the rules for 2026 after the department's latest penalty form,
[R-210R-i (1/26)](https://dam.ldr.la.gov/taxforms/210Ri-1-26.pdf), was
printed. This package follows the Act where the form predates it, and every
result's `Divergences` names the out-of-date document.

| Rule | R-210R-i (1/26) and LDR's page | Act 307, computed here |
|---|---|---|
| This year's share | 90% | 70% |
| The penalty stops | April 15 | May 15, 2027 |
| Annualized income (Exception 4) | offered | repealed |
| Last year's facts (Exception 3) | last year's taxable income at this year's rate | last year's facts at this year's rate and standard deduction |

## Louisiana is shaped differently from federal

| Rule | Louisiana | Where |
|---|---|---|
| Who must pay | Tax after credits and withholding over $1,000, or $2,000 on a joint return | [IT-540ESi (2026)](https://dam.ldr.la.gov/taxforms/IT540ESi-2026.pdf); R-210R-i Exception 1 |
| Required annual payment | The lesser of 70% of this year's tax and 100% of last year's; no 110% tier | [R.S. 47:118](https://legis.la.gov/legis/LawPrint.aspx?d=101109) as amended; R-210R-i lines 1 to 3 |
| A late start | The amount is split among the dates left: 1/3, 1/2 or all of it | R-210R-i lines 4 and 5 |
| Exceptions | Last year's tax, last year's facts, or 70% of the tax on income earned to date each excuse an installment they reach | R-210R-i Exceptions 2, 3 and 5 |
| The penalty | 0.000329 a day, to the cent, on each shortfall until paid | R-210R-i line 18 |

The code is in [`estimated.go`](estimated.go), and the 2026 figures in
[`estimated_y2026.go`](estimated_y2026.go). Sources are in
[`testdata/SOURCES.md`](testdata/SOURCES.md).

## What it needs that no other state does

`EstimatedFacts`, carried in `estimated.Facts.State()`:

```go
la, err := louisianataxcalc.NewEstimatedFacts().
    PriorYearFactsTax(4_200 * money.Dollar). // last year's facts at 3% and this year's standard deduction
    StatuteJointThreshold(false).            // a joint return at the department's $2,000
    Build()
```

A fact left out is reported in `Required` with the default used.

## Not built

Farmers and fishermen get `estimated.ErrUnsupported`. Spreading withholding by
the months in each period, which R-210R-i allows without defining the months,
is not offered.
