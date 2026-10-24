# vermonttaxcalc

Vermont estimated income tax for 2026: whether a household has to make
estimated payments, what is due by each date, and the interest and penalty if
the payments go as planned. It implements the shared
[`estimated.EstimatedTax`](../estimated/estimated.go) interface.

**There is no Vermont income tax calculator here yet.** The caller supplies
the year's tax minus credits, Form IN-111 line 20 minus line 26c.

## A quarter short costs interest and 1% a month

$10,000 of tax this year, $8,000 last year, $2,000 withheld, and $1,000 paid on
each due date. The target is last year's $8,000, $2,000 a quarter. Withholding
counts $500 a quarter, so each quarter is $500 short. Figured to April 15,
2027:

| Due | Short | Interest | Penalty | Total |
|---|---|---|---|---|
| April 15, 2026 | $500 | 261 days, $27.71 | 12 months, $60 | $87.71 |
| June 15, 2026 | $500 | 200 days, $21.23 | 10 months, $50 | $71.23 |
| September 15, 2026 | $500 | 108 days, $11.47 | 7 months, $35 | $46.47 |
| January 15, 2027 | $500 | not published | 3 months, $15 | $15.00 |

The total is $220.41. The 2027 interest rate is not yet set, so the days after
January 1, 2027 are reported in `Required` rather than charged.

## Vermont is shaped differently from federal

| Rule | Vermont | Where |
|---|---|---|
| The two options | The lesser of 90% of this year's tax or 100% of last year's. No 110% tier | [IN-114](https://tax.vermont.gov/sites/tax/files/documents/IN-114-2026.pdf); [IN-152](https://tax.vermont.gov/sites/tax/files/documents/IN-152-2025.pdf) lines 1 to 3 |
| No charge | This year's tax less withholding under $500 | [IN-111 instructions](https://tax.vermont.gov/sites/tax/files/documents/IN-111-Instr-2025.pdf), line 31 |
| Due dates | A quarter each on April 15, June 15, September 15 and January 15, 2027 | [IN-114 instructions](https://tax.vermont.gov/sites/tax/files/documents/IN-114-Instr-2026.pdf) |
| Withholding | 25% on each due date, whatever the actual dates | IN-152 line 5a |
| The charge | Interest by the day, 7.75% for 2026, plus 1% for each month or part of a month, on the whole shortfall until paid in full or April 15 | IN-152 lines 7 to 15; [2026 rate memo](https://tax.vermont.gov/sites/tax/files/documents/2026%20Interest%20Rate%20Memo.pdf) |
| Farmers and fishers | Two-thirds of gross income from farming or fishing, this year or last: no estimated payments | 32 V.S.A. § 5852(a); IN-111 instructions |
| Annualising | For federal annualisers only: each federal Schedule AI installment scaled by the Vermont required payment over the federal | [IN-152A](https://tax.vermont.gov/sites/tax/files/documents/IN-152A-2025.pdf) |

The code is in [`estimated.go`](estimated.go), and the 2026 figures in
[`estimated_y2026.go`](estimated_y2026.go).

## The worksheets are followed, and where the statute differs is reported

Each result's `Divergences` lists eight places where the Department's
documents and 32 V.S.A. read differently: the $500 threshold, what a shortfall
is measured against, the prior-year figure, withholding dates, how a later
payment is applied, the end of the period, the annualised installment, and
weekend due dates. The figures follow the worksheets. The research is in
[`research/estimated-payments/vt.md`](../research/estimated-payments/vt.md).

Two readings no document settles are reported in `Required` when they change
the answer: a tax less withholding of exactly $500, which is charged, and how a
later payment reaches an earlier quarter, which follows 32 V.S.A.
§ 5859(b)(2)(B).

## What it needs that no other state does

`EstimatedFacts`, carried in `estimated.Facts.State()`:

```go
vt, err := vermonttaxcalc.NewEstimatedFacts().
    PriorYearFarmerOrFisher(false).                     // two-thirds of last year's income
    FederalAnnualised(fedLine9, ai1, ai2, ai3, ai4).    // Form 2210 line 9, Schedule AI line 27
    Build()
```

A fact left out is reported in `Required` with the default used.

## Not built

The Child Care Contribution, which the IN-114 instructions say estimated
payments must include, is not part of IN-152's tax and is not covered.
