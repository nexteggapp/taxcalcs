# kentuckytaxcalc

Kentucky estimated individual income tax for 2026: whether a household has to
make estimated payments, what is due by each date, and the underpayment
penalty if the payments go as planned. It implements the shared
[`estimated.EstimatedTax`](../estimated/estimated.go) interface.

**There is no Kentucky income tax calculator here yet.** The caller supplies
the year's tax.

## Kentucky is the federal shape with a lower threshold

[KRS 141.305](https://apps.legislature.ky.gov/law/statutes/statute.aspx?id=49196)
adopts 26 U.S.C. § 6654 with exceptions.

| Rule | Kentucky | Where |
|---|---|---|
| Who must pay | More than $5,000 of income without Kentucky withholding, or AGI at the filing threshold; and more than $500 of tax after withholding | KRS 141.305(1), (2); Form 2210-K line 5 |
| The two tests | 90% of this year's tax, or 100% of last year's; 110% above $150,000 of last year's Kentucky AGI ($75,000 married filing separately) | 2026 voucher instructions, 42A740-S4 |
| Due dates | April 15, June 15, September 15, January 15; 25% each, withholding spread evenly | 42A740-S4; Form 2210-K lines 9 and 10 |
| The January payment | Excused for a return filed and paid by February 1, 2027 only if the requirement arose after September 1, 2026 | 42A740-S4 |
| The penalty | Simple interest by the day at 9% for 2026, to April 15, 2027 | Form 2210-K (2025) lines 17 to 21 |
| Waivers | Reasonable cause only, under KRS 131.175 and 103 KAR 1:040 | KRS 141.305(3)(e) |

The code is in [`estimated.go`](estimated.go), and the 2026 figures in
[`estimated_y2026.go`](estimated_y2026.go).

## The 2026 rate is an open question

Form 2210-K charges 9% for 2026. The department set the 2026 tax interest rate
at 7%, and KRS 141.305(3)(d) can be read to use that rate without the 2%
added for unpaid tax. The penalty follows the form, and reports the question
in `Required`. KRS 131.183 was amended from July 15, 2026, after the form was
printed; what that changed was not established.

## The forms are followed, and where the statute differs is reported

Each result's `Divergences` lists four: exactly $500, the 2026 rate, the
February 1 return, and crediting withholding when withheld. The research
behind each is in
[`research/estimated-payments/ky.md`](../research/estimated-payments/ky.md).

## What it needs that no other state does

`EstimatedFacts`, carried in `estimated.Facts.State()`:

```go
ky, err := kentuckytaxcalc.NewEstimatedFacts().
    NonWageIncome(20_000 * money.Dollar). // KRS 141.305(1)(a)
    FilingThresholdMet(true).             // KRS 141.305(1)(b)
    PriorYearUSResident(true).            // Form 2210-K's no-liability exception
    CombinedReturn(false).                // married filing separately on one Form 740
    Build()
```

A fact left out is reported in `Required` with the default used.

## Not built

Farmers and fishers get `estimated.ErrUnsupported`. Local occupational
license taxes are not covered. The department's documents could not be
downloaded; see [`testdata/SOURCES.md`](testdata/SOURCES.md).
