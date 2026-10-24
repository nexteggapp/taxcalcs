# arkansastaxcalc

Arkansas estimated individual income tax for 2026: whether a household has to
make estimated payments, what is due by each date, and the underpayment
penalty if the payments go as planned. It implements the shared
[`estimated.EstimatedTax`](../estimated/estimated.go) interface.

**There is no Arkansas income tax calculator here yet.** The caller supplies
the year's Arkansas net tax, Form AR1000F line 38.

## The department's own example gives $67

The AR2210 instructions work one penalty: $5,000 of tax, $2,000 paid, $3,000
of tax last year. On the 2026 dates:

```go
rules, _ := arkansastaxcalc.EstimatedTax(2026)
f, _ := estimated.NewFacts("AR", 2026).FilingStatus(filing.Single).
    ThisYear(thisYear).    // Tax(5_000 * money.Dollar)
    PriorYear(lastYear).   // a filed 2025 return with Tax(3_000 * money.Dollar)
    Withheld(withheld).    // 2_000 * money.Dollar
    Build()
p, _ := rules.Penalty(f, estimated.MustDate(2027, time.April, 15))
p.Value().Total() // $67.00: $4, $13, $17 + $15, $18
```

`TestEstimatedPenaltyMatchesTheAR2210Example` checks each charge.

## Arkansas is close to federal, and differs where it counts

| Rule | Arkansas | Where |
|---|---|---|
| Who must pay | More than $1,000 of tax after Arkansas withholding | [AR2210](https://www.dfa.arkansas.gov/wp-content/uploads/2025_AR2210_Penalty_for_Underpayment_of_EstimatedTax_1.pdf) line 4; [AR1000ES](https://www.dfa.arkansas.gov/wp-content/uploads/2026_Final_AR1000ES_2.pdf) |
| The target | The smaller of 90% of this year's net tax and 100% of last year's. No 110% tier | AR2210 lines 2, 5 and 6 |
| Due dates | A quarter each on April 15, June 15, September 15 and January 15 | AR1000ES vouchers |
| When payments count | Withholding and every payment made by January 15, in four equal parts on the due dates | AR2210 line 8; [AR2210 instructions](https://www.dfa.arkansas.gov/wp-content/uploads/2025_AR2210_Instructions.pdf), example |
| The penalty | 10% a year, days ÷ 365, to April 15, each charge in whole dollars | AR2210 lines 15 and 16 |
| No penalty | Return filed and paid by January 31; no liability last year as a resident throughout; casualty, or retirement after 62 or disability, if the Commissioner agrees | AR2210 instructions, Exceptions 2 to 5 |
| Uneven income | AR2210A, at a printed .00027397 a day | [AR2210A](https://www.dfa.arkansas.gov/wp-content/uploads/2025_AR2210A_AnnualizedPenalty_for_Underpayment_of_EstimatedTax.pdf) |

The code is in [`estimated.go`](estimated.go), and the 2026 figures in
[`estimated_y2026.go`](estimated_y2026.go).

## The Code was not read, so the forms are followed and the gaps are reported

The Arkansas Code's estimated tax sections were not retrieved; the official
Code is hosted by a commercial publisher. The figures follow the DFA's forms.
Each result's `Divergences` lists five places where a form and page 1 of the
AR2210 instructions, which restate the law, read differently, and a sixth for
AR2210A. Where the documents leave a number open, `Required` says so and names
the default: whether exactly $1,000 is inside the rules, whether an estimated
payment counts from the day it is made, and whether the January 31 date moves
off a Sunday. The research is in
[`research/estimated-payments/ar.md`](../research/estimated-payments/ar.md).

## Not built

Farmers with two-thirds of gross income from farming get
`estimated.ErrUnsupported`. Local income taxes are not covered.
