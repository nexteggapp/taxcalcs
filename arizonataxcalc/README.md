# arizonataxcalc

Arizona estimated individual income tax for 2026: whether a household has to
make estimated payments, what is due by each date, and the underpayment
penalty if the payments go as planned. It implements the shared
[`estimated.EstimatedTax`](../estimated/estimated.go) interface.

**There is no Arizona income tax calculator here yet.** The caller supplies
the year's tax after credits.

## Arizona tests income, not tax, before anything is required

| Rule | Arizona | Where |
|---|---|---|
| Who must pay | Arizona gross income over $75,000 ($150,000 joint) both last year and this year | [A.R.S. § 43-581(A)](https://www.azleg.gov/ars/43/00581.htm); [2026 Form 140ES instructions](https://azdor.gov/sites/default/files/document/FORMS_INDIVIDUAL_2026_140iES.pdf) |
| The target | The smaller of 90% of this year's tax and 100% of last year's. No 110% tier | § 43-581(A); 140ES worksheet lines 22 to 24 |
| No penalty | Tax after withholding and credits under $1,000; or a federal § 6654 exception: no federal liability last year, or federal tax less federal withholding under $1,000 | § 43-581(E); [Form 221 (2025)](https://azdor.gov/sites/default/files/document/FORMS_INDIVIDUAL_2025_221_f.pdf) line 1 |
| Dates | A quarter on April 15, June 15, September 15 and January 15; filing and paying by February 1, 2027 replaces January | 2026 Form 140ES |
| The penalty | Interest at the federal short-term rate plus 3 points, 6% then 7% in 2026, by quarter, to April 15; compounded January 1; never over 10% of the shortfall | [§ 42-1125(Q)](https://www.azleg.gov/ars/42/01125.htm), [§ 42-1123](https://www.azleg.gov/ars/42/01123.htm); Form 221 Part B; [ADOR interest rates](https://azdor.gov/business/interest-rates) |

The code is in [`estimated.go`](estimated.go), and the 2026 figures in
[`estimated_y2026.go`](estimated_y2026.go).

## A worked example

$44,444.44 of tax and $48,000 last year, so a target of $40,000 and $10,000 a
quarter. Nothing paid until $10,000 on July 31 and $30,000 on September 15:

```go
rules, _ := arizonataxcalc.EstimatedTax(2026)
p, _ := rules.Penalty(facts, estimated.MustDate(2027, 4, 15))
```

```
$356.71
1 2026-04-15 to 2026-06-30, 76 days at 6%: $124.93
1 2026-06-30 to 2026-07-31, 31 days at 7%: $59.45
2 2026-06-15 to 2026-06-30, 15 days at 6%: $24.66
2 2026-06-30 to 2026-09-15, 77 days at 7%: $147.67
```

Form 221 rounds the total to $357, and the result's `Applied` says so.

## The forms are followed, and where the statute differs is reported

Each result's `Divergences` lists four: Form 221's "less than the threshold"
against the statute's "exceeds"; the forms' condition of a filed (and, on the
140ES, required) 2025 return for the prior-year option, which the statute does
not state; Form 221's April 15 stop against the statute's "period of
nonpayment"; and Form 221's printed 2025 rates against the 2026 rates used. The
research behind them is in
[`research/estimated-payments/az.md`](../research/estimated-payments/az.md).

## What it needs that no other state does

`EstimatedFacts`, carried in `estimated.Facts.State()`:

```go
az, err := arizonataxcalc.NewEstimatedFacts().
    NoFederalLiabilityLastYear(false).                // the § 6654(e)(2) exception
    FederalTaxLessWithholding(9_000 * money.Dollar).  // the § 6654(e)(1) exception
    PriorYearReturnRequired(true).                    // the 140ES condition
    Build()
```

Arizona gross income comes from `estimated.Figures.GrossIncome`, this year's
and last year's. A fact left out is reported in `Required` with the default
used.

## Not built

Farmers and fishermen (one installment) and nonresident aliens (three) get
`estimated.ErrUnsupported`. The rate from January 1, 2027 is not published, so
days after December 31, 2026 are counted but not charged, and `Required` says
so.
