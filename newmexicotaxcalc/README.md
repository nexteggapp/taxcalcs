# newmexicotaxcalc

New Mexico estimated personal income tax for 2026: whether a household has to
make estimated payments, what is due by each date, and the underpayment
penalty if the payments go as planned. It implements the shared
[`estimated.EstimatedTax`](../estimated/estimated.go) interface.

**There is no New Mexico income tax calculator here yet.** The caller supplies
the year's tax after rebates and credits, RPD-41272 line 4.

## New Mexico follows the federal shape, with these differences

| Rule | New Mexico | Where |
|---|---|---|
| Who owes a penalty | Tax less withholding of $1,000 or more | NMSA 1978 § 7-2-12.2(H)(1); [RPD-41272](https://realfile.tax.newmexico.gov/2025rpd-41272.pdf) line 4 |
| The two tests | 90% of this year's tax, or 100% of last year's from a filed twelve-month return; no 110% tier | § 7-2-12.2(B) |
| Exempt | First-year residents; full-year residents with no liability last year | § 7-2-12.2(H)(2), (M) |
| A late start | Instead of an annualized method: 50% by June 15, 75% by September 15 or 100% by January 15 | § 7-2-12.2(C); RPD-41272 instruction 9 |
| The rate | The federal underpayment rate each quarter: 6% April to June 2026, 7% from July; the first 2027 quarter's rate also covers April 1 to 15 | § 7-1-67(B); [Penalty Interest Rates](https://www.tax.newmexico.gov/individuals/file-your-taxes-overview/penalty-interest-rates/) |

The code is in [`estimated.go`](estimated.go), and the 2026 figures in
[`estimated_y2026.go`](estimated_y2026.go). No 2026 RPD-41272 is published;
the 2025 form's arithmetic is used with the 2026 dates and rates.

## The form is followed, and where the statute differs is reported

Each result's `Divergences` lists four places. The largest: § 7-2-12.2(H)(3)
imposes no penalty when withholding and estimated payments reach the
required annual payment, with no word on timing, while the form requires
timely installments. The research is in
[`research/estimated-payments/nm.md`](../research/estimated-payments/nm.md).

## What it needs that no other state does

`EstimatedFacts`, carried in `estimated.Facts.State()`:

```go
nm, err := newmexicotaxcalc.NewEstimatedFacts().
    FirstYearResident(false). // § 7-2-12.2(M)
    FarmerLastYear(false).    // two-thirds from farming or ranching last year
    Build()
```

`JanuaryThirtyFirstMoves` answers whether a return filed on Monday, February
1, 2027 counts; no source says. A fact left out is reported in `Required`
when it changes the figure, with the default used.

## Not built

Farmers and ranchers get `estimated.ErrUnsupported`. New Mexico has no local
income tax.
