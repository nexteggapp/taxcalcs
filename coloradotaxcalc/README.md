# coloradotaxcalc

Colorado estimated individual income tax for 2026: whether a household has to
make estimated payments, what is due by each date, and the underpayment
penalty if the payments go as planned. It implements the shared
[`estimated.EstimatedTax`](../estimated/estimated.go) interface.

**There is no Colorado income tax calculator here yet.** The caller supplies
the year's tax.

## Colorado asks for less than federal, and charges more for missing it

| Rule | Colorado | Where |
|---|---|---|
| Who must pay | More than $1,000 of Colorado tax after withholding and credits | [DR 0104EP (2026)](https://tax.colorado.gov/sites/default/files/documents/DR_0104EP_2026.pdf) |
| The two tests | 70% of this year's tax; or 100% of last year's, 110% above $150,000 of last year's federal AGI ($75,000 separately), for a filed twelve-month Colorado return | DR 0104EP; C.R.S. § 39-22-605(6) |
| Due dates | A quarter each on April 15, June 15, September 15 and January 15. Withholding counts in equal parts unless the filer shows its dates | DR 0104EP; § 39-22-605(5), (8) |
| The interest | Colorado's regular rate for each calendar year: 11% for 2026, 10% for 2027, by the day, to April 15 | [DR 0204 (2025)](https://tax.colorado.gov/sites/default/files/documents/DR0204_2025.pdf) Part 3; [CDOR's rate table](https://tax.colorado.gov/tax-topics-penalties-and-interest) |
| Annualising | Only for a filer who also annualises federally: 17.5%, 35%, 52.5% and 70% of the tax at 4.4% on annualised income | DR 0204 Part 4 |

$40,000 of Colorado tax with nothing withheld or paid owes $28,000 by the
year, $7,000 a quarter, and a penalty of $1,970.74 figured to April 15, 2027
(`TestEstimatedPenaltyAcrossTwoRatePeriods`).

The code is in [`estimated.go`](estimated.go), and the 2026 figures in
[`estimated_y2026.go`](estimated_y2026.go).

## The forms are followed, and where the statute differs is reported

Each result's `Divergences` lists five places where the department's forms and
C.R.S. § 39-22-605 read differently: exactly $1,000 of tax, which the form
exempts and the statute does not; the 2025 rates DR 0204 prints; the
no-liability exception and the waivers, which only the statute has; and the
weekend rule. The figures follow the forms, except that a 2026 underpayment is
charged 2026's and 2027's rates. The research behind each is in
[`research/estimated-payments/co.md`](../research/estimated-payments/co.md).

## What it needs that no other state does

`EstimatedFacts`, carried in `estimated.Facts.State()`:

```go
co, err := coloradotaxcalc.NewEstimatedFacts().
    PriorYearFarmerOrFisher(false). // § 39-22-605(2)(a)(II)
    FederalAnnualised(false).       // § 39-22-605(6)(d)
    Build()
```

A fact left out is reported in `Required` with the default used.

## Not built

Farmers and fishermen, who make one payment by January 15, get
`estimated.ErrUnsupported`. Local occupational privilege taxes are not
covered. Denver's is withheld by employers and takes no estimated payments;
whether any other city's does was not found.
