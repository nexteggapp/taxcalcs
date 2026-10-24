# hawaiitaxcalc

Hawaii estimated individual income tax for 2026: whether a household has to
make estimated payments, what is due by each date, and the underpayment
penalty if the payments go as planned. It implements the shared
[`estimated.EstimatedTax`](../estimated/estimated.go) interface.

**There is no Hawaii income tax calculator here yet.** The caller supplies the
year's tax after credits, Form N-210 line 3.

## Hawaii asks for less than federal, on the 20th, and charges by the month

| Rule | Hawaii | Where |
|---|---|---|
| Who must pay | $500 or more of Hawaii tax after withholding and credits | [Tax Facts 2019-3](https://files.hawaii.gov/tax/legal/taxfacts/tf2019-3.pdf); HRS § 235-97(a)(6) |
| The two tests | 60% of this year's tax; or 100% of last year's at every income, for a filed twelve-month Hawaii return. No 110% step | [Form N-210](https://files.hawaii.gov/tax/forms/2025/n210_i.pdf) lines 6 to 8; [HRS § 235-97(f)(4)](https://data.capitol.hawaii.gov/hrscurrent/Vol04_Ch0201-0257/HRS0235/HRS_0235-0097.htm) |
| Due dates | A quarter each on April 20, June 20, September 20 and January 20. In 2026 June 20 is a Saturday and September 20 a Sunday, so those are due June 22 and September 21 | HRS §§ 235-97(a)(2), 231-21; Tax Facts 2019-3 question 8 |
| Withholding | In equal parts on the due dates, unless the filer shows its dates | HRS § 235-97(g); N-210 box D |
| The charge | .00667 of the shortfall for each month or part of a month, a month running from the 21st to the 20th, to April 20 of the next year | [N-210 instructions](https://files.hawaii.gov/tax/forms/2025/n210ins.pdf) Part IV; HRS § 235-97(f) |
| Annualising | 15%, 30%, 45% and 60% of the tax on annualised income, with the federal recapture | N-210 Schedule A |

$40,000 of Hawaii tax with nothing withheld or paid owes $24,000 by the year,
$6,000 a quarter. Left unpaid to April 20, 2027 that is 12, 10, 7 and 3 months,
the figures N-210 line 19 prints, and a penalty of $1,280.64
(`TestEstimatedPenaltyMonthsMatchTheForm`).

The code is in [`estimated.go`](estimated.go), and the 2026 figures in
[`estimated_y2026.go`](estimated_y2026.go).

## The forms are followed, and where the law differs is reported

Each result's `Divergences` lists three places where the department's forms
and the law read differently:

- the no-liability exception, which the forms give only to a full-year Hawaii
  resident and the adopted 26 U.S.C. § 6654(e)(2) gives to any U.S. citizen or
  resident;
- the monthly factor, .00667 on the form and two-thirds of one percent in the
  statute;
- a late start, where HAR § 18-235-97(e)(2)(A) spreads the year over the
  remaining dates and Form N-210 still charges a quarter on each.

The figures follow the forms. The research behind each is in
[`research/estimated-payments/hi.md`](../research/estimated-payments/hi.md).

## What it needs that no other state does

`EstimatedFacts`, carried in `estimated.Facts.State()`:

```go
hi, err := hawaiitaxcalc.NewEstimatedFacts().
    PriorYearFarmerOrFisher(false).      // N-210 instructions; § 6654(i)(2)(B)
    NonresidentAlienWithoutWages(false). // N-210 Part III; § 6654(j)
    Build()
```

A fact left out is reported in `Required` with the default used.

## Not built

Farmers and fishermen, and nonresident aliens without Hawaii wage withholding,
get `estimated.ErrUnsupported`. Fiscal years, estates and trusts, and the
deferral for service in a combat zone are not covered. Whether the Sunday
January 31, 2027 file-and-pay date moves to February 1 is left open in
`Required`.
