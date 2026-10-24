# iowataxcalc

Iowa estimated individual income tax for 2026: whether a household has to make
estimated payments, what is due by each date, and the underpayment penalty if
the payments go as planned. It implements the shared
[`estimated.EstimatedTax`](../estimated/estimated.go) interface.

**There is no Iowa income tax calculator here yet.** The caller supplies the
year's tax: IA 2210 line 3, which is IA 1040 line 18 less the refundable
credits on lines 23 to 27.

## Iowa uses the federal method with its own dates and figures

| Rule | Iowa | Where |
|---|---|---|
| Who must pay | $1,000 or more of Iowa tax after withholding, from 2026 ($200 before) | [Iowa Code § 422.16(12)(a)(1)](https://www.legis.iowa.gov/docs/code/2026/422.16.pdf); [2026 IA 1040ES](https://revenue.iowa.gov/media/2725/download?inline) |
| The two tests | 90% of this year's tax, or 100% of last year's; 110% when last year's federal AGI plus Iowa net modifications exceeded $150,000 ($75,000 married filing separately) | [2025 IA 2210](https://revenue.iowa.gov/media/4399/download) lines 4 to 8 |
| Due dates | April 30, June 30, September 30, 2026 and February 1, 2027, because January 31 is a Sunday | 2026 IA 1040ES; Iowa Code § 421.9A |
| A late start | After April 1, three installments from June 30; after June 1, two from September 30; after September 1, one | 2025 IA 2210 instructions, Period Requirements First Met |
| No liability last year | A twelve-month 2025 with no Iowa tax means no penalty | 2025 IA 2210 lines 7 and 8; Iowa Admin. Code r. 701—308.6(1)(a) |
| The interest | .00027397 a day (10% a year) for 2026, to the cent, to April 30, 2027 | 2025 IA 2210 lines 23a to 24b; [IDR, October 21, 2025](https://revenue.iowa.gov/press-release/2025-10-21/idr-announces-2026-individual-income-tax-and-interest-rates) |

The code is in [`estimated.go`](estimated.go), and the 2026 figures in
[`estimated_y2026.go`](estimated_y2026.go).

## The forms are followed, except one that predates the law

No 2026 IA 2210 is published, so the 2025 form's arithmetic is used. Its $200
small-balance test was printed before the act raising the threshold to $1,000
took effect for 2026, so the act's figure is used and the form is reported as
out of date. Each result's `Divergences` lists that, the printed daily factor,
and the waiver for disability in the year before, which the form omits. The
research behind each is in
[`research/estimated-payments/ia.md`](../research/estimated-payments/ia.md).

## What it needs that no other state does

`EstimatedFacts`, carried in `estimated.Facts.State()`:

```go
ia, err := iowataxcalc.NewEstimatedFacts().
    PriorYearModifications(0). // last year's IA 1040 line 3
    Build()
```

Left out, the prior-year test assumes 110% and says so in `Required`.

## Not built

Farmers and fishers (IA 2210F) and the annualised method (IA 2210 Schedule AI)
get `estimated.ErrUnsupported`. The optional short method is not computed.
The rate from January 1, 2027 is not published, so days after 2026 are
reported in `Required` rather than charged.
