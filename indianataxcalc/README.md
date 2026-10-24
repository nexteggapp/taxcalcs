# indianataxcalc

Indiana estimated income tax for 2026: whether a household has to make
estimated payments, what is due by each date, and the underpayment penalty if
the payments go as planned. It implements the shared
[`estimated.EstimatedTax`](../estimated/estimated.go) interface.

**There is no Indiana income tax calculator here yet.** The caller supplies
the year's state and county tax together.

## The penalty is a flat 10%, not interest

$10,000 of 2026 state and county tax ($1,500 of it county), $4,000 withheld,
last year's tax $8,000 on $90,000 of federal AGI, and $1,000 paid on each date
except the third, paid on October 1. Rendered from `Requirement`, `Schedule`
and `Penalty`:

```
target $8000.00   (100% of last year's, less than 90% of $10,000)
2026-04-15  needed $2000.00  credited $2000.00  short $0.00     met
2026-06-15  needed $4000.00  credited $4000.00  short $0.00     met
2026-09-15  needed $6000.00  credited $5000.00  short $1000.00  short
2027-01-15  needed $8000.00  credited $8000.00  short $0.00     met
penalty to April 15, 2027: $100.00
```

The October payment counts in the January period and makes up September's
shortfall, but the 10% on September stands. It would be the same $100 if the
payment had been one day late.

| Rule | Indiana | Where |
|---|---|---|
| Who must pay | $1,000 or more of state and county tax after withholding and the pass-through entity tax credit | [Schedule IT-2210](https://forms.in.gov/Download.aspx?id=16926) line 6 |
| The two tests | 90% of this year's tax, or 100% of last year's; 110% above $150,000 of last year's federal AGI ($75,000 separately) | IT-2210 lines 4, 7 and CAUTION box |
| Due dates | April 15, June 15, September 15, 2026 and January 15, 2027; a quarter each | [Form ES-40](https://forms.in.gov/Download.aspx?id=16906); IT-2210 line 14 |
| The charge | 10% of each period's shortfall; a late payment counts in the next period; catching up does not reduce it | IT-2210 lines 16 to 21; [Bulletin #3](https://www.in.gov/dor/files/ib03.pdf) |
| January | Filing and paying by January 31 (February 1, 2027) excuses the fourth installment | Bulletin #3 |
| Annualised method | 22.5%, 45%, 67.5% and 90% of annualised tax | [Schedule IT-2210A](https://forms.in.gov/Download.aspx?id=16928) |

The code is in [`estimated.go`](estimated.go), and the 2026 figures in
[`estimated_y2026.go`](estimated_y2026.go).

## Two divergences, and four open questions

Each result's `Divergences` lists where the forms and the Indiana Code differ:
ES-40's "more than $1,000" against the statute's "less than $1,000" (the
figure follows IT-2210 and the statute), and the 2025 IT-2210's "N/A" for a
filer with no return last year, which predates IC 6-3-4-4.1(a)(3), in force
from January 1, 2026; that rule is computed from the law.

`Required` carries what the research could not settle, with the default used:
whether the federal 12-month condition on last year's return applies (default:
no, as the forms say), whether the federal casualty, retirement and disability
waivers apply (default: the penalty is shown in full), and which year's filing
status the $75,000 step reads, where IT-2210 and IT-2210A disagree (default:
last year's if given, as IT-2210 says). The research is in
[`research/estimated-payments/in.md`](../research/estimated-payments/in.md).

## What it needs that no other state does

`EstimatedFacts`, carried in `estimated.Facts.State()`:

```go
in, err := indianataxcalc.NewEstimatedFacts().
    PassThroughEntityTaxCredit(0).                // Schedule 5 line 3, counted with withholding
    PriorYearAnnualisedTax(1_231 * money.Dollar). // part-year resident last year: IT-2210 line 7
    PriorYearMarriedSeparately(false).            // the $75,000 step
    TwelveMonthRule(false).                       // the unsettled 12-month condition
    Build()
```

County tax goes in `estimated.Figures.LocalTax` as well as in `Tax`. A fact
left out is reported in `Required` with the default used.

## Not built

Farmers and fishermen (IT-2210 Section A) get `estimated.ErrUnsupported`.
Figures are kept to the cent where the form rounds each entry to whole
dollars. Bulletin #3's rules for nonresident partners' and beneficiaries'
withholding are not applied.
