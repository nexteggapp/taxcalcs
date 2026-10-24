# mainetaxcalc

Maine estimated individual income tax for 2026: whether a household has to make
estimated payments, what is due by each date, and the underpayment penalty if
the payments go as planned. It implements the shared
[`estimated.EstimatedTax`](../estimated/estimated.go) interface.

**There is no Maine income tax calculator here yet.** The caller supplies the
year's tax.

## Maine is shaped differently from federal

| Rule | Maine | Where |
|---|---|---|
| Who must pay | This year's tax after withholding of $1,000 or more, and last year's liability of $1,000 or more | [36 M.R.S. § 5228(2)](https://legislature.maine.gov/statutes/36/title36sec5228.html) |
| The target | The smaller of 90% of this year's tax and 100% of last year's, for a 12-month year. No 110% tier | § 5228(3) |
| An unusual event | Non-withheld income $500,000 or more above the same period last year: 90% of the tax on it is due with that installment, and neither last year's tax nor the $1,000 test on it covers it | § 5228(1)(D), (3), (5)(D), (11) |
| Dates | April 15, June 15, September 15, 2026 and January 15, 2027, a quarter each, moved for weekends and Maine holidays; none moves in 2026 | Form 1040ES-ME (2026); §§ 153(2), 5228(4); [4 M.R.S. § 1051](https://legislature.maine.gov/statutes/4/title4sec1051-1.html) |
| January relief | No penalty on the fourth installment for a return filed and paid in full by February 1, 2027 (January 31 is a Sunday) | § 5228(9) |
| The interest | .75% a month for 2026, a part of a month as a whole one, compounded monthly, to April 15, 2027 | Form 2210ME (2025); [§ 186](https://legislature.maine.gov/statutes/36/title36sec186.html) |

The code is in [`estimated.go`](estimated.go), and the 2026 figures in
[`estimated_y2026.go`](estimated_y2026.go).

For $100,000 of 2026 tax, last year's $20,000, and nothing paid, the target is
$20,000, $5,000 a quarter, and the penalty to April 15, 2027 is $767.98. That
counts the months through 2026 only; the 2027 rate is not carried, and
`Required` says so.

## The forms are followed, and where the statute differs is reported

Each result's `Divergences` lists four: how last year's liability is measured,
what the $1,000 test on this year subtracts, when withholding counts as paid,
and the January 31 relief that the 2026 Form 1040ES-ME leaves out. The research
behind each is in
[`research/estimated-payments/me.md`](../research/estimated-payments/me.md).

## What it needs that no other state does

`EstimatedFacts`, carried in `estimated.Facts.State()`, answers the unusual
event:

```go
me, err := mainetaxcalc.NewEstimatedFacts().
    IncomeNotWithheld(1, 50_000*money.Dollar, 50_000*money.Dollar). // Jan to Mar: this year, last year
    IncomeNotWithheld(2, 700_000*money.Dollar, 100_000*money.Dollar).
    IncomeNotWithheld(3, 0, 0).
    IncomeNotWithheld(4, 0, 0).
    UnusualEventTax(2, 42_000*money.Dollar). // Maine tax on the $600,000 excess
    PassThroughWaiver(false).
    Build()
```

Or `NoUnusualEvent()`. Left out, no event is assumed and `Required` says so.

## Not built, and not checked

Farmers and persons who fish commercially get `estimated.ErrUnsupported`.
Maine Revenue Services' PDFs could not be downloaded, so the form figures are
checked against the research file's verified quotes; see
[`testdata/SOURCES.md`](testdata/SOURCES.md). Form 2210ME's precomputed rate
table and its rounding were not seen.
