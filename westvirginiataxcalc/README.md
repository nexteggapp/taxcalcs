# westvirginiataxcalc

West Virginia estimated personal income tax for 2026: whether a household has
to make estimated payments, what is due by each date, and the underpayment
penalty if the payments go as planned. It implements the shared
[`estimated.EstimatedTax`](../estimated/estimated.go) interface.

**There is no West Virginia income tax calculator here yet.** The caller
supplies the year's tax after credits, Form IT-210 Part I line 3.

## West Virginia follows the federal shape with its own numbers

| Rule | West Virginia | Where |
|---|---|---|
| Who must pay | $600 or more of tax after withholding, unless that is under 10% of the tax | [IT-140ESI](https://tax.wv.gov/Documents/TaxForms/it140esi.pdf) (Sept 2023) |
| The two tests | 90% of this year's tax, or 100% of last year's for a 12-month return. No 110% tier | [IT-210](https://tax.wv.gov/Documents/PIT/2025/it140.PersonalIncomeTaxFormsAndInstructions.2025.pdf) (2025), Part I lines 6 to 8 |
| No tax last year | A target of zero while this year's tax is not more than $5,000 | IT-210 Part I line 8 |
| The dates | A quarter by April 15, June 15, September 15 and January 15, moved past weekends and § 2-2-1 holidays. A return filed and paid by January 31, 2027, a Sunday, so by February 1, excuses January | IT-140ESI; IT-210 Part IV; W. Va. Code §§ 11-10-18a(i), 2-2-1(e) |
| The interest | 0.000315 a day for 2026 (11.5%), in whole dollars, to April 15, 2027 | IT-210 Part IV Section B; [TSD-365](https://tax.wv.gov/Documents/TSD/tsd365.pdf) |

The code is in [`estimated.go`](estimated.go), and the 2026 figures in
[`estimated_y2026.go`](estimated_y2026.go).

## The forms are followed, and where the Code differs is reported

Each result's `Divergences` lists eight places where the Tax Division's
documents and the West Virginia Code read differently. One changes a number
for many filers: IT-210 lowers each installment to a quarter of last year's
tax, where § 11-10-18a measures the shortfall against 90% of this year's tax
and lets last year's figure only excuse an installment. The figures follow
IT-210. The research is in
[`research/estimated-payments/wv.md`](../research/estimated-payments/wv.md).

## What it needs that no other state does

`EstimatedFacts`, carried in `estimated.Facts.State()`:

```go
wv, err := westvirginiataxcalc.NewEstimatedFacts().
    PriorYearUSCitizenOrResident(true). // § 11-10-18a(e)(2)(C)
    Build()
```

It matters only for a filer who was not required to file last year. A fact
left out is reported in `Required` with the default used.

## Not built

Farmers get `estimated.ErrUnsupported`. IT-210's short method is not built.
Wage and other withholding cannot be put on their dates separately.
