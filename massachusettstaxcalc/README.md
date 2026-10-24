# massachusettstaxcalc

Massachusetts estimated personal income tax for 2026: whether a household has
to make estimated payments, what is due by each date, and the underpayment
penalty if the payments go as planned. It implements the shared
[`estimated.EstimatedTax`](../estimated/estimated.go) interface.

**There is no Massachusetts income tax calculator here yet.** The caller
supplies the year's tax after credits, including the 4% surtax.

## A worked example

$12,000 of 2026 tax, $4,000 withheld, $9,000 of tax on the 2025 return, and
$1,000 paid on each due date. 100% of last year's tax is less than 80% of this
year's, so the target is $9,000, $2,250 a quarter. Withholding counts $1,000
on each date, and each date's $2,000 pays the oldest shortfall first, so the
shortfalls are $250, $500, $750 and $1,000, the first three paid on the next
date. The penalty to April 15, 2027 is **$30.39**, for the days of 2026. The
rate from January 1, 2027 is not published, and the result says so in
`Required`. `TestEstimatedReadmeExample` reproduces it.

## Massachusetts is federal in shape, not in numbers

| Rule | Massachusetts | Where |
|---|---|---|
| Who must pay | More than $400 of tax on income not subject to withholding | [Form 1-ES (2026)](https://www.mass.gov/doc/2026-form-1-es-estimated-tax-payment-vouchers-instructions-and-worksheets/download); M.G.L. c. 62B § 13 |
| The target | The lesser of 80% of this year's tax and 100% of last year's; no 110% tier | [Form M-2210](https://www.mass.gov/doc/2025-form-m-2210-underpayment-of-massachusetts-estimated-income-tax/download) lines 4 to 6; c. 62B § 14(c) |
| Due dates | April 15, June 15, September 15, January 15; none moves in 2026 | Form 1-ES (2026); c. 4 § 7 for the holidays |
| No penalty | Tax after credits and withholding of $400 or less; no liability last year as a full-year resident | M-2210 exceptions; c. 62B § 14(d) |
| Annualised | 20%, 40%, 60% and 80% of the annualised tax | M-2210 Part 3 |
| The charge | Simple interest by the day: 7% April to June 2026, 8% after, to April 15 | [TIR 26-5](https://www.mass.gov/technical-information-release/tir-26-5-interest-rate-on-overpayments-and-underpayments); M-2210 Part 2 |

The code is in [`estimated.go`](estimated.go), and the 2026 figures in
[`estimated_y2026.go`](estimated_y2026.go).

## Where the forms and the statute differ, both are reported

Each result's `Divergences` lists four: the $400 floor (the form excuses
exactly $400, the statute does not), daily compounding, a requirement first
met after March 31, and April 15 falling outside the form's rate lines. The
research behind each is in
[`research/estimated-payments/ma.md`](../research/estimated-payments/ma.md).

## A late start is an open question

Form 1-ES and AP 241 have a filer whose requirement arises after March 31 pay
over the remaining dates. c. 62B § 14(c) keeps four installments of 25%. No
source says which sets the penalty. The department's reading is the default
and is reported in `Required`; `EstimatedFacts` chooses the other:

```go
ma, err := massachusettstaxcalc.NewEstimatedFacts().
    LateStart(massachusettstaxcalc.LateStartFourQuarters). // or LateStartRemainingDates
    PriorYearFarmerOrFisher(false).                        // c. 62B § 14(h), last year's return
    PartYearPriorReturnCounts(true).                       // a 2025 part-year return opens the 100% test
    Build()
```

## Not built

Farmers and fishermen get `estimated.ErrUnsupported`. Fiduciaries (Form
M-2210F) and fiscal years are not covered.
