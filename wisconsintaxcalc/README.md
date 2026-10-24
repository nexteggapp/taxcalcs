# wisconsintaxcalc

Wisconsin estimated individual income tax for 2026: whether a household has
to make estimated payments, what is due by each date, and the underpayment
interest if the payments go as planned. It implements the shared
[`estimated.EstimatedTax`](../estimated/estimated.go) interface.

**There is no Wisconsin income tax calculator here yet.** The caller supplies
the year's tax: Schedule U line 3, net tax less refundable credits.

## Wisconsin's rules

| Rule | Wisconsin | Where |
|---|---|---|
| Who must pay | $500 or more of tax after withholding and credits | [2026 Form 1-ES Instructions](https://www.revenue.wi.gov/TaxForms2026/2026-Form1-ES-Inst.pdf); Wis. Stat. § 71.09(11)(a) |
| The two tests | 90% of this year's tax or 100% of last year's on a 12-month return. No 110% tier | 1-ES; [Schedule U](https://www.revenue.wi.gov/TaxForms2025/2025-ScheduleU.pdf) lines 4, 7 and 8; § 71.09(13) |
| Due dates | A quarter each by April 15, June 15, September 15 and January 15. A Saturday, Sunday or § 995.20 holiday moves to the next business day | 1-ES; § 71.09(12); § 990.001(4) |
| Applying payments | A payment covers its own period first. The rest goes back to the most recent shortfall, then forward. A payment after January 15 goes to January first | [Schedule U instructions](https://www.revenue.wi.gov/TaxForms2025/2025-ScheduleU-Inst.pdf), lines 17, 22 and 23 |
| The interest | 12% a year, days ÷ 365, to the date paid or April 15, 2027 | Schedule U lines 25 to 29; § 71.84(1) |
| No interest | Under $500 after withholding; no tax last year as a full-year resident (code 02); a federal disaster extension (code 10) | Schedule U instructions, Exceptions; § 71.09(11) |
| January 31 | Filing and paying by February 1, 2027 replaces January only if nothing was due earlier | § 71.09(14); Schedule U instructions, line 17 |

The code is in [`estimated.go`](estimated.go), and the 2026 figures in
[`estimated_y2026.go`](estimated_y2026.go).

## The forms are followed, and where the statute differs is reported

Each result's `Divergences` lists three places where the department's forms
and Chapter 71 read differently: the reasonable-cause condition on the
retirement waiver, active duty abroad treated as a waiver rather than outside
the section, and the 1-ES's later first payment dates. The figures follow the
forms. The research is in
[`research/estimated-payments/wi.md`](../research/estimated-payments/wi.md).

## What it needs that no other state does

`EstimatedFacts`, carried in `estimated.Facts.State()`:

```go
wi, err := wisconsintaxcalc.NewEstimatedFacts().
    PriorYearFarmerOrFisher(false).  // § 71.09(1)(a)
    FederalDisasterExtension(false). // code 10
    ActiveDutyOutsideUS(false).      // code 07
    Build()
```

A fact left out is reported in `Required` with the default used.

## Not built

Farmers and fishers (code 04) get `estimated.ErrUnsupported`. Estates and
trusts are not covered. The short method in Schedule U Part II is not used.
