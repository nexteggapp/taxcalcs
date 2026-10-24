# districtofcolumbiataxcalc

District of Columbia estimated individual income tax for 2026: whether a
household has to make estimated payments, what is due by each date, and the
underpayment interest if the payments go as planned. It implements the shared
[`estimated.EstimatedTax`](../estimated/estimated.go) interface.

**There is no DC income tax calculator here yet.** The caller supplies the
year's tax, D-40 line 24 less line 30.

## The District charges interest at printed factors, not by the day

| Rule | District of Columbia | Where |
|---|---|---|
| Who must pay | $100 or more of DC tax after withholding and credits | [D-40ES (REV. 03/2026)](https://otr.cfo.dc.gov/sites/default/files/dc/sites/otr/publication/attachments/2026_D40ES_Book_wLinks04012026.pdf) |
| The two tests | 90% of this year's tax; or 110% of last year's at every income, for a filer who filed a DC return and lived in DC all of last year | D-40ES; [D-2210 (05/2025)](https://otr.cfo.dc.gov/sites/default/files/dc/sites/otr/publication/attachments/2025_D-2210_021226.pdf) lines 1 to 5 |
| Due dates | A quarter each on April 15, June 15, September 15 and January 15. Withholding counts in equal parts unless the filer shows all its dates | D-40ES; D.C. Code § 47-4203(d)(3) |
| The interest | Each period's cumulative underpayment times .0175, .0265, .0351 or .0259, the factors for the spans to the next due date and to April 15 | D-2210 lines 7 to 11 |
| No interest | Under $100 left after withholding and payments; no DC tax last year as a full-year resident; under 10% of the year's tax left | D-2210 instructions |
| Annualising | The required payment prorated by the income to each period | D-2210 line 7 instructions |

$40,000 of DC tax with nothing withheld or paid owes $36,000 by the year,
$9,000 a quarter, and interest of $2,515 figured to April 15, 2027
(`TestEstimatedInterestAtThePrintedFactors`).

The code is in [`estimated.go`](estimated.go), and the 2026 figures in
[`estimated_y2026.go`](estimated_y2026.go).

## The forms are followed, and where the D.C. Code differs is reported

Each result's `Divergences` lists eleven places where OTR's forms and D.C.
Code §§ 47-4201, 47-4203 and 47-1812.08(i) read differently. The largest are
the interest factors, which the statute replaces with 10% compounded daily
over the days to payment rounded up to the month's end; the 10% exception,
which the statute does not have; the full-year residence condition on the
110% test and the no-liability exception; and the annualised method, which
the statute computes at 22.5%, 45%, 67.5% and 90% of annualised tax. The
figures follow the forms. The research behind each is in
[`research/estimated-payments/dc.md`](../research/estimated-payments/dc.md).

No fact here is the District's alone, so the package has no extras type and
refuses facts that carry one.

## Not built

The statute's exception for the year of the taxpayer's death is reported and
not applied, because the facts carry no date of death. The statute's waivers
for retirement, disability and casualty are the Mayor's to grant, and the
interest is shown before them. The District has no separate rule for farmers
or fishermen, and no local income tax.
