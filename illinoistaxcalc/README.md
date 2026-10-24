# illinoistaxcalc

Illinois estimated individual income tax for 2026: whether a household has to
make estimated payments, what is due by each date, and the late-payment
penalty if the payments go as planned. It implements the shared
[`estimated.EstimatedTax`](../estimated/estimated.go) interface.

**There is no Illinois income tax calculator here yet.** The caller supplies
the year's tax after credits.

## The penalty is a flat charge, not interest

Each amount paid late is charged once: 2% if it is paid within 30 days of its
due date, 10% if it is paid later or never. This is the IL-2210 instructions'
worked example, moved to 2026 dates: $1,800 of tax, payments of $100, $200,
$750, $250 and $400 against four $405 installments.

```
2026-04-15  $200.00 paid 2026-06-09, 55 days late: $200.00 x 10% = $20.00
2026-04-15  $105.00 paid 2026-09-08, 146 days late: $105.00 x 10% = $10.50
2026-06-15  $405.00 paid 2026-09-08, 85 days late: $405.00 x 10% = $40.50
2026-09-15  $165.00 paid 2027-01-19, 126 days late: $165.00 x 10% = $16.50
2027-01-15  $85.00 paid 2027-01-19, 4 days late: $85.00 x 2% = $1.70
2027-01-15  $320.00 paid 2027-05-10, 115 days late: $320.00 x 10% = $32.00
total $121.20
```

The form's answer is $121.20.

| Rule | Illinois | Where |
|---|---|---|
| Who must pay | Tax after credits, withholding and pass-through withholding over $1,000 | [IL-1040-ES](https://tax.illinois.gov/content/dam/soi/en/web/tax/forms/incometax/documents/currentyear/individual/il-1040-es.pdf) |
| The target | The lesser of 90% of this year's tax and 100% of last year's. No 110% tier | [IL-2210](https://tax.illinois.gov/content/dam/soi/en/web/tax/forms/incometax/documents/currentyear/individual/il-2210.pdf) lines 6 and 7 |
| The dates | A quarter each on April 15, June 15, September 15 and January 15 | IL-1040-ES |
| Withholding | A quarter of it in each quarter, or the actual quarters with pay stubs; pass-through withholding in the quarter the entity's year ended | [IL-2210 instructions](https://tax.illinois.gov/content/dam/soi/en/web/tax/forms/incometax/documents/currentyear/individual/il-2210-instr.pdf), lines 10a and 10b |
| No penalty | $1,000 or less this year; no return required, or no tax due after withholding, last year; farmers; 65 or older in a nursing home | IL-2210 instructions |
| The charge | 2% for 1 to 30 days late, 10% after | IL-2210 Penalty Worksheet 1 |

The code is in [`estimated.go`](estimated.go), and the 2026 figures in
[`estimated_y2026.go`](estimated_y2026.go).

## The forms are followed, and where the law differs is reported

Each result's `Divergences` lists nine places where the forms and the Income
Tax Act or the department's rules read differently. Two change a number
often: the form excuses the penalty when last year's return was fully covered
by withholding, where the Act asks for no liability at all; and the form
applies each quarter's withholding to that quarter only, where the rule
applies it to the oldest unpaid quarter. The research is in
[`research/estimated-payments/il.md`](../research/estimated-payments/il.md).

## What it needs that no other state does

`EstimatedFacts`, carried in `estimated.Facts.State()`:

```go
il, err := illinoistaxcalc.NewEstimatedFacts().
    NursingHome(false).                                              // IL-1040 line 34b
    PassThroughWithholding(estimated.MustDate(2026, 12, 31), 400*money.Dollar). // line 27
    InterestOnLateInstallments(false).
    Build()
```

Whether interest also runs on a late installment is not settled by any
Illinois document. IL-2210 computes none, and neither does this unless asked.
A fact left out is reported in `Required` with the default used.

## Not built

The exceptions for underpayments after a death, combat-zone extensions,
innocent spouses and bankruptcy estates are reported, not applied. The penalty
on tax unpaid with the return, Penalty Worksheet 2, is not computed.
