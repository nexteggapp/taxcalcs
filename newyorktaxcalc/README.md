# newyorktaxcalc

New York estimated income tax for 2026: whether a household has to make
estimated payments, what is due by each date, and the underpayment penalty if
the payments go as planned. One computation covers New York State, New York
City and Yonkers income tax and the MCTMT, as Forms IT-2105 and IT-2105.9 do.
It implements the shared [`estimated.EstimatedTax`](../estimated/estimated.go)
interface.

**There is no New York income tax calculator here yet.** The caller supplies
the four taxes' total, with the City and Yonkers part as `LocalTax`.

## New York follows the federal rules, with these differences

| Rule | New York | Where |
|---|---|---|
| Who must pay | $300 or more after withholding, for each of the state, City and Yonkers taxes; any MCTMT | [IT-2105.9-I](https://www.tax.ny.gov/pdf/2025/inc/it2105_9i_2025.pdf) line 15; Tax Law § 685(d)(1) |
| The two tests | 90% of this year's tax, or 100% of last year's; 110% above $150,000 of last year's New York AGI or MCTD earnings ($75,000 separately) | § 685(c)(3); [IT-2105-I](https://www.tax.ny.gov/pdf/2026/inc/it2105i_2026.pdf) |
| No-liability exception | Needs residence, or New York source income, all last year | § 685(d)(2) |
| The rate | The federal short-term rate plus 5½ points, not below 7½%, simple, by quarter: 8.5% April to June 2026, 9.5% from July | § 697(j); DTF quarterly rate pages |

The code is in [`estimated.go`](estimated.go), and the 2026 figures in
[`estimated_y2026.go`](estimated_y2026.go). No 2026 IT-2105.9 is published;
the 2025 instructions' arithmetic is used with the 2026 dates and rates.

## The form is followed, and where the statute differs is reported

Each result's `Divergences` lists three: January 31, 2027 falls on a Sunday,
which General Construction Law § 25-a would move; the instructions omit the
statute's casualty waiver; and the voucher's late-start catch-up, which the
penalty form does not use. The research is in
[`research/estimated-payments/ny.md`](../research/estimated-payments/ny.md).

## What it needs that no other state does

`EstimatedFacts`, carried in `estimated.Facts.State()`:

```go
ny, err := newyorktaxcalc.NewEstimatedFacts().
    SubjectTo(true, false, false).          // New York City, Yonkers, MCTMT
    PriorYearMCTDEarnings(0).               // the MCTD 110% step
    PriorYearNewYorkSourceIncome(false).    // the no-liability exception for a nonresident
    Build()
```

A fact left out is reported in `Required` when it changes the figure, with the
default used.

## Not built

Farmers and fishermen get `estimated.ErrUnsupported`. The exception for
installments due after death, the 90-day extension after a spouse's death,
and estates and trusts are not covered.
