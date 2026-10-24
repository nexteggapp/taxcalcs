# idahotaxcalc

**Idaho does not require individuals to make estimated tax payments, and has no
individual underpayment penalty.** This package says so through the shared
[`estimated.EstimatedTax`](../estimated/estimated.go) interface, so a caller
that asks every state gets a plain answer for Idaho:

| Question | Idaho's answer | Where |
|---|---|---|
| Are estimated payments required? | No. The estimated tax law reaches corporations only | [Idaho Code § 63-3036A](https://legislature.idaho.gov/statutesrules/idstat/title63/t63ch30/sect63-3036A/); [Form 40 instructions (2025)](https://tax.idaho.gov/wp-content/uploads/forms/EIN00046/EIN00046_03-02-2026.pdf) |
| What is due by each date? | Nothing; there are no installments | the same |
| What is the penalty? | Zero | the same |

There is no Idaho income tax calculator here.

## What does bite: the extension prepayment

The automatic six-month extension to file holds only if, by the April due date,
withholding and payments reach 80% of this year's tax or 100% of the tax on last
year's return; a shortfall of $50 or less does not count. Missing it costs 2% a
month on the balance from the original due date
([Idaho Code § 63-3033](https://legislature.idaho.gov/statutesrules/idstat/title63/t63ch30/sect63-3033/),
[Form 51](https://tax.idaho.gov/wp-content/uploads/forms/EFO00092/EFO00092_09-18-2025.pdf)).
The Explanation describes this. The package does not compute it, because it is
a condition of the extension rather than an estimated payment.

## Where the form and the statute differ

The Form 40 instructions word the extension's prior-year test as "what you
paid" last year; § 63-3033(a) and Form 51 say the tax on last year's return.
Each result's `Divergences` reports it. The research is in
[`research/estimated-payments/id.md`](../research/estimated-payments/id.md).

The code is in [`estimated.go`](estimated.go), and the 2026 figures in
[`estimated_y2026.go`](estimated_y2026.go).
