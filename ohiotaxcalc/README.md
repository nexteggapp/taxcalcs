# ohiotaxcalc

Ohio estimated individual income tax for 2026: whether a household has to make
estimated payments, what is due by each date, and the interest penalty if the
payments go as planned. It implements the shared
[`estimated.EstimatedTax`](../estimated/estimated.go) interface.

**There is no Ohio income tax calculator here yet.** The caller supplies the
year's tax after credits.

**School district and city income taxes are not covered.** Each takes its own
estimated payments.

## Ohio is shaped differently from federal

| Rule | Ohio | Where |
|---|---|---|
| Who must pay | More than $500 of tax after credits, less Ohio withholding | [IT 1040ES (2026)](https://dam.assets.ohio.gov/image/upload/v1735926006/tax.ohio.gov/forms/ohio_individual/individual/2026/ites-instructions-fi.pdf) |
| The target | The lesser of 90% of this year's tax and 100% of last year's. Last year's counts only after a timely Ohio return. No 110% tier | [IT/SD 2210 (2025)](https://dam.assets.ohio.gov/image/upload/v1762982566/tax.ohio.gov/forms/ohio_individual/individual/2025/itsd2210-fi.pdf) Part I |
| The whole-year test | No penalty when withholding and payments made by January 15 reach the target, however they were spread | IT/SD 2210 lines 1, 5 and 7 |
| The charge | Each date's cumulative shortfall, at one factor to the next date, the last to April 15. 7% for 2026, over a 365.25-day year, in whole dollars | IT/SD 2210 Part II and page 4 |

The code is in [`estimated.go`](estimated.go), and the 2026 figures in
[`estimated_y2026.go`](estimated_y2026.go).

## A worked case

$44,444.44 of tax, so a $40,000 target, and nothing paid. From
`TestEstimatedPenaltyArithmetic`:

| Due | Short | Factor | Charge |
|---|---|---|---|
| April 15, 2026 | $10,000 | 0.011691 | $117 |
| June 15, 2026 | $20,000 | 0.017632 | $353 |
| September 15, 2026 | $30,000 | 0.020507, to December 31 | $615 |
| January 15, 2027 | $40,000 | none: the 2027 rate is not published | $0 |

The penalty is $1,085 so far, and `Required` names the missing 2027 rate.
The 2026 IT/SD 2210 is not published either. Its factors are worked out by
the formula the 2025 form prints, which gives each of that form's eight
factors exactly.

## The form is followed, and where the statute differs is reported

Each result's `Divergences` lists seven places where the department's forms
and ORC 5747.09 read differently. Examples are the timely-return condition,
the whole-year tests, the end of each span, and the 2026 worksheet's
out-of-date tax brackets. The figures follow the forms. The research behind
each is in [`research/estimated-payments/oh.md`](../research/estimated-payments/oh.md).

## What it needs that no other state does

`EstimatedFacts`, carried in `estimated.Facts.State()`:

```go
oh, err := ohiotaxcalc.NewEstimatedFacts().
    PriorYearReturnTimely(true). // IT/SD 2210 line 6
    Build()
```

Left out, it is reported in `Required`, and last year's tax is not offered.

## Not built

Farmers and fishermen (Ohio Adm. Code 5703-7-04) get
`estimated.ErrUnsupported`. The statute's annualised test and its choice of
the dates withheld have no form and are not applied. Servicemembers with an
IRS extension are not modelled.
