# michigantaxcalc

Michigan estimated income tax for 2026: whether a household has to make
estimated payments, what is due by each date, and the interest and penalty if
the payments go as planned. It implements the shared
[`estimated.EstimatedTax`](../estimated/estimated.go) interface.

**There is no Michigan income tax calculator here yet.** The caller supplies
the year's tax. City income taxes, Detroit's among them, take their own
estimates and are not covered.

## A shortfall costs interest and a separate penalty

The MI-2210 instructions' own example: $2,000 due each quarter, $1,000 paid
April 15 and $2,000 on June 10, then $2,000 on each later date.

```go
mi, _ := michigantaxcalc.EstimatedTax(2026)
res, _ := mi.Penalty(facts, estimated.MustDate(2027, 4, 15))
```

```
total $178.00
April 15, 2026 short $1000.00: interest $13.00 (56 days) flat $100.00 (0 days)
June 15, 2026 short $1000.00: interest $3.00 (15 days) interest $17.00 (77 days)
September 15, 2026 short $1000.00: interest $23.00 (107 days) interest $3.00 (15 days)
January 15, 2027 short $1000.00: interest $19.00 (90 days)
```

The June payment paid April's shortfall first, so interest rolls forward a
quarter at a time. The 10% penalty is on April alone: for the penalty a
payment counts only in the quarter it arrives.

| Rule | Michigan | Where |
|---|---|---|
| Who must pay | More than $500 of tax after withholding and credits | [MCL 206.301(1)](https://www.legislature.mi.gov/Laws/MCL?objectName=mcl-206-301); [2026 MI-1040ES](https://www.michigan.gov/taxes/-/media/Project/Websites/taxes/Forms/IIT/TY2025/MI-1040ES-(2026).pdf) |
| The target | The smaller of 90% of this year's tax and 100% of last year's, 110% above $150,000 of last year's AGI ($75,000 separate) | MI-1040ES; [2025 MI-2210](https://www.michigan.gov/taxes/-/media/Project/Websites/taxes/Forms/IIT/TY2025/MI-2210.pdf) lines 4 to 7 |
| The dates | A quarter each on April 15, June 15, September 15 and January 15 | MCL 206.301(1); MI-1040ES |
| Interest | 0.0002324, 0.0002150 and 0.0002129 a day, in whole dollars, to April 15, 2027 | MI-2210 Part 2; RAB 2026-5; RAB 2026-10 |
| Penalty | 25% (at least $25) for a quarter with nothing paid, 10% (at least $10) for one paid short | MI-2210 Part 3; RAB 2021-17 |
| No penalty, interest still due | Estimates not required last year; no return required last year; return filed and paid by January 31 (February 1, 2027) | MCL 205.24(6); MI-2210; RAB 2021-17 |

The code is in [`estimated.go`](estimated.go), and the 2026 figures in
[`estimated_y2026.go`](estimated_y2026.go). The statutes' figures are checked
against extracts in [`testdata/`](testdata/SOURCES.md). Treasury's are checked
against the research's verified quotes, because www.michigan.gov refused the
downloads, and some have no quote; `SOURCES.md` lists them.

## The forms are followed, and where the statute differs is reported

Each result's `Divergences` lists three: the printed daily factor against the
statute's monthly rate, the 25% and 10% penalties applied by arithmetic
against the statute's negligence and intentional-disregard tests, and January
31, 2027, a Sunday, which the MI-1040ES words two ways and MCL 8.6 moves to
February 1.

RAB 2021-17 Example 7 charges its fourth quarter $59 by applying a later
payment to an earlier quarter's shortfall. The RAB's own text and the MI-2210
say payments are never carried back for the penalty, so the package charges
$29 there; the test says why.

## What it needs that no other state does

`EstimatedFacts`, carried in `estimated.Facts.State()`:

```go
mi, err := michigantaxcalc.NewEstimatedFacts().
    SeafaringShare(0).                       // MCL 206.301(6)
    PriorYearEstimatesRequired(true).        // MCL 205.24(6)
    PriorYearMarriedFilingSeparately(false). // the 2025 MI-2210 line 4 step
    Build()
```

A fact left out is worked out where the 2025 return allows, and otherwise
reported in `Required` with the default used. So are the questions the
research left open: whether interest is charged when no 2025 return was
required, whether a January 16, 2027 payment counts, and whether a quarter
paid late is charged 25% or 10%.

## Not built

Farmers, fishermen and seafarers get `estimated.ErrUnsupported`. Fiduciaries
and city income taxes are not covered.
