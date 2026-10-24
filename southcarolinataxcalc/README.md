# southcarolinataxcalc

South Carolina estimated individual income tax for 2026: whether a household
has to make estimated payments, what is due by each date, and the underpayment
penalty if the payments go as planned. It implements the shared
[`estimated.EstimatedTax`](../estimated/estimated.go) interface.

**There is no South Carolina income tax calculator here yet.** The caller
supplies the year's tax, figured under Act 110 of 2026.

## South Carolina follows the federal rules, with four changes

S.C. Code § 12-54-55(1) computes the penalty "in the same manner" as 26 U.S.C.
§ 6654. What differs:

| Rule | South Carolina | Where |
|---|---|---|
| The threshold | No penalty when tax less withholding is under $100 | [SC2210](https://dor.sc.gov/sites/dor/files/forms/SC2210_2025.pdf) Part I line 6; § 12-6-3910(A)(1) |
| Income earned in another state | No penalty on an underpayment from personal service income that the other state withheld tax on | [SC1040ES](https://dor.sc.gov/sites/dor/files/forms/SC1040ES_2026.pdf); § 12-54-55(1) |
| Due dates | South Carolina's legal holidays move them, including Confederate Memorial Day | [Revenue Procedure #23-1](https://dor.sc.gov/resources-site/lawandpolicy/Advisory%20Opinions/RP23-1.pdf) |
| Filing by January 31 | Excuses the January payment, and the earlier shortfalls stop accruing on January 15 | SC2210 instructions |

The rest is federal: 90% of this year's tax or 100% of last year's (110% above
$150,000 of AGI, $75,000 married filing separately), a quarter by each date,
simple interest at the department's rate, 6% from April to June 2026 and 7%
after ([IL #26-19](https://dor.sc.gov/sites/dor/files/policies/IL26-19.pdf)).

The code is in [`estimated.go`](estimated.go), and the 2026 figures in
[`estimated_y2026.go`](estimated_y2026.go).

## A worked example

$10,000 of tax, last year's $8,000 on $90,000 of AGI, and one $4,000 payment
on August 14, 2026:

```
You expect to owe $10000.00 of South Carolina tax after $0.00 of withholding, which is $100.00 or more, and withholding falls short of the $8000.00 required, so you need estimated payments of $8000.00, a quarter by each date.
installment 1: $2000.00 short, $42.25
installment 2: $2000.00 short, $22.19
installment 3: $2000.00 short, $41.04
installment 4: $2000.00 short, $0.00
penalty to 2026-12-31 $105.48
```

April's $42.25 is 76 days at 6% and 45 days at 7%. January's days are in
2027, whose rate is not published; figured to April 15, 2027, the result says
so in `Required`.

## The forms are followed, and where they disagree is reported

Each result's `Divergences` lists nine places where the forms part from each
other or from the statute. Two change a figure here against what the 2025
SC2210 prints: the rate for each period, which the form prints as .07
throughout; and the January 31 date, which the 2026 SC1040ES moves to
February 1. The 2026 worksheet predates Act 110, so the tax is the caller's to
figure under the act. The research behind each is in
[`research/estimated-payments/sc.md`](../research/estimated-payments/sc.md).

## What it needs that no other state does

`EstimatedFacts`, carried in `estimated.Facts.State()`:

```go
sc, err := southcarolinataxcalc.NewEstimatedFacts().
    PriorYearFarmerOrFisher(false).          // two-thirds of last year's income
    UnderpaymentFromOtherStateIncome(false). // § 12-54-55(1)
    NonresidentContractor(false).            // 2% contract withholding
    Build()
```

A fact left out is reported in `Required` with the default used.
`EstimatedCalendar(2026)` returns the holidays the due dates move for.

## Not built

Farmers and fishermen get `estimated.ErrUnsupported`. The SC2210's box E, for a
joint return in only one of the two years, is not applied. Whether the
nonresident contractor exemption also excuses the penalty is not stated
anywhere, and the penalty is figured as the SC2210 has it.
