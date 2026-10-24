# northdakotataxcalc

North Dakota estimated income tax for 2026: whether a household has to make
estimated payments, what is due by each date, and the interest on underpaid
estimated tax if the payments go as planned. It implements the shared
[`estimated.EstimatedTax`](../estimated/estimated.go) interface.

**There is no North Dakota income tax calculator here yet.** The caller
supplies the year's net tax liability, Form ND-1 line 25.

## North Dakota follows the federal rules, with these differences

| Rule | North Dakota | Where |
|---|---|---|
| Who must pay | Only a filer who must pay federal estimated tax, and whose 2025 North Dakota net tax liability was $1,000 or more; not required to file counts as zero | N.D.C.C. § 57-38-62(1); [ND-1ES](https://www.tax.nd.gov/sites/www/files/documents/forms/individual/2025-iit/28709-form-nd-1es-2026.pdf) |
| Who owes interest | $1,000 or more of tax less withholding | § 57-38-62(3) |
| The two tests | 90% of this year's tax, or 100% of last year's; no 110% tier and no twelve-month test; a filer who moved in last year has only the 90% test | § 57-38-62(1)(a), (b) |
| Late income | A nonresident or new resident with no North Dakota income until after April 15 pays thirds, halves or all in January | [ND-1UT](https://www.tax.nd.gov/sites/www/files/documents/forms/individual/2025-iit/28704-schedule-nd-1ut-2025.pdf) Item D |
| Federal January exception | If the federal January installment was not required, North Dakota's is zero | ND-1UT Item C |
| Annualized method | Only if used on the federal Form 2210 | ND-1UT Item B |
| The interest | A fixed 12% a year; the day fraction rounded to three places; a payment pays its own quarter first | ND-1UT Part 3 |
| Waivers | Good cause, at the Tax Commissioner's discretion; IRS disaster extensions carry over | § 57-38-45(5) |

The code is in [`estimated.go`](estimated.go), and the 2026 figures in
[`estimated_y2026.go`](estimated_y2026.go). The 2025 ND-1UT is the latest
published; its arithmetic is used with the 2026 dates.

## The form is followed, and where the statute differs is reported

Each result's `Divergences` lists five places. Among them, ND-1UT applies a
payment between due dates to the next quarter and only offsets an earlier
shortfall as of that next due date, where § 57-38-62(3) runs interest to the
day the portion is paid. The research is in
[`research/estimated-payments/nd.md`](../research/estimated-payments/nd.md).

## What it needs that no other state does

`EstimatedFacts`, carried in `estimated.Facts.State()`:

```go
nd, err := northdakotataxcalc.NewEstimatedFacts().
    FederalEstimatesRequired(true).              // subject to 26 U.S.C. § 6654
    FederalFourthQuarterExcused(false).          // ND-1UT Item C
    AnnualisedFederally(true).                   // ND-1UT Item B
    FirstNorthDakotaIncome(estimated.MustDate(2026, time.June, 3)). // ND-1UT Item D
    Build()
```

A fact left out is reported in `Required` when it changes the figure, with the
default used.

## Not built

Qualified farmers, who use 66 2/3% and the March 1 waivers, get
`estimated.ErrUnsupported`. Fiscal and short years, estates and trusts, and
the separate-to-joint recomputation of last year's liability are left to the
caller. North Dakota has no local income tax.
