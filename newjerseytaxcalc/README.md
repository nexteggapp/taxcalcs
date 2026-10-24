# newjerseytaxcalc

New Jersey estimated gross income tax for 2026: whether a household has to
make estimated payments, what is due by each date, and the interest on an
underpayment if the payments go as planned. It implements the shared
[`estimated.EstimatedTax`](../estimated/estimated.go) interface.

**There is no New Jersey income tax calculator here yet.** The caller supplies
the year's tax, Form NJ-1040 line 50.

## New Jersey keeps the older federal rules

| Rule | New Jersey | Where |
|---|---|---|
| Who must pay | More than $400 after withholding and credits; nobody at or under the $10,000 or $20,000 filing threshold | [NJ-1040-ES](https://nj.gov/treasury/taxation/pdf/current/1040esi.pdf); [NJ-2210](https://www.nj.gov/treasury/taxation/pdf/current/2210.pdf) line 3 |
| The two tests | 80% of this year's tax, or 100% of last year's; the 110% tier never applies | NJ-2210 lines 4a to 5; N.J.S.A. 54A:9-6(c) |
| Exceptions | A quarter is free of interest when payments reach last year's tax, the tax on last year's income at this year's rates, 80% of the tax on annualized income, or 90% of the tax on income to date | NJ-2210 Part II; N.J.S.A. 54A:9-6(d) |
| The interest | Prime plus 3%, 10% for 2026, for each month or part of a month | NJ-2210 Option 2; [TB-21(R)](https://www.nj.gov/treasury/taxation/pdf/pubs/tb/tb21r.pdf); N.J.S.A. 54:49-3 |
| January | A return filed and paid by February 15 replaces the January installment | N.J.S.A. 54A:8-4(h) |

The code is in [`estimated.go`](estimated.go), and the 2026 figures in
[`estimated_y2026.go`](estimated_y2026.go). No 2026 NJ-2210 is published; the
2025 form's arithmetic is used with the 2026 dates and rate.

## The 2027 rate is not set yet

NJ-2210's rate schedule gives a month the rate of the year it starts in, from
the 16th: 2026's 10% runs through months starting January 15, 2027. The rate
for months from January 16, 2027 is set from the prime rate on December 1,
2026, so those months are not charged and the result says so in `Required`.

## The form is followed, and where the statute differs is reported

Each result's `Divergences` lists seven places, among them exactly $400,
the late-start installments the form does not use, and the statute's 110%
in Exceptions 1 and 2. The research is in
[`research/estimated-payments/nj.md`](../research/estimated-payments/nj.md).

## What it needs that no other state does

`EstimatedFacts`, carried in `estimated.Facts.State()`:

```go
nj, err := newjerseytaxcalc.NewEstimatedFacts().
    RefundableCredits(0).                                    // NJ-1040 lines 56, 58 to 65
    PriorYearIncomeAtThisYearsRates(4_200 * money.Dollar).   // Exception 2
    TaxOnIncomeToDate(march, may, august).                   // Exception 4
    Build()
```

`PriorYearOverpaymentInApril` and `FebruaryFifteenthMoves` answer two
questions the published sources leave open. A fact left out is reported in
`Required` when it changes the figure, with the default used.

## Not built

Farmers get `estimated.ErrUnsupported`. Form NJ-2210NR for nonresidents,
estates and trusts, and the exception for installments due after death.
