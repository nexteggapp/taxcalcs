# nebraskataxcalc

Nebraska estimated income tax for 2026: whether a household has to make
estimated payments, what is due by each date, and the underpayment penalty if
the payments go as planned. It implements the shared
[`estimated.EstimatedTax`](../estimated/estimated.go) interface.

**There is no Nebraska income tax calculator here yet.** The caller supplies
the year's tax after credits, Form 2210N line 3.

## Nebraska follows the federal rules, with these differences

| Rule | Nebraska | Where |
|---|---|---|
| Who must pay | $500 or more of tax after credits, less withholding | [Form 2210N](https://revenue.nebraska.gov/sites/default/files/doc/tax-forms/2026/f_2210n.pdf) line 6; Neb. Rev. Stat. § 77-2769 |
| The two tests | 90% of this year's tax, or 100% of last year's; 110% above $150,000 of last year's federal AGI ($75,000 separately) | Form 2210N lines 4 to 8 |
| Exceptions | No liability last year as a U.S. citizen or resident; a first-time Nebraska filer; under $500 | Form 2210N, Exceptions to the Penalty |
| The rate | 8% a year, simple, the delinquent tax rate set every other year | Form 2210N line 22a; REG-20-007.03; [Revenue Ruling 99-26-2](https://revenue.nebraska.gov/sites/default/files/doc/legal/rulings/rr992401_interest_rate.pdf) |
| Filed by January 31 | The balance counts as paid on January 15 | Form 2210N, line 9 instructions |

The code is in [`estimated.go`](estimated.go), and the 2026 figures in
[`estimated_y2026.go`](estimated_y2026.go).

## The form is followed, and where the statute or regulation differs is reported

Each result's `Divergences` lists four places: the regulation's out-of-date
$300 threshold, the citizenship condition the regulation omits, the
first-time filer exception no statute or regulation states, and the
voucher worksheet's late-start catch-up that Form 2210N does not use. The
research is in
[`research/estimated-payments/ne.md`](../research/estimated-payments/ne.md).

## What it needs that no other state does

`EstimatedFacts`, carried in `estimated.Facts.State()`:

```go
ne, err := nebraskataxcalc.NewEstimatedFacts().
    FirstTimeFiler(false).
    PriorYearUSCitizenOrResident(true). // the no-liability exception
    FarmerLastYear(false).               // two-thirds from farming last year
    Build()
```

`JanuaryThirtyFirstMoves` answers whether a return filed on Monday, February
1, 2027 counts; Form 2210N does not say. A fact left out is reported in
`Required` when it changes the figure, with the default used.

## Not built

Farmers, ranchers and fishermen get `estimated.ErrUnsupported`. Nebraska has
no local income tax.
