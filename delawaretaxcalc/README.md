# delawaretaxcalc

Delaware estimated personal income tax for 2026: whether a household has to
make estimated payments, what is due by each date, and the underpayment
penalty if the payments go as planned. It implements the shared
[`estimated.EstimatedTax`](../estimated/estimated.go) interface.

**There is no Delaware income tax calculator here yet.** The caller supplies
the year's tax.

## Delaware starts later and charges a fixed rate

| Rule | Delaware | Where |
|---|---|---|
| Who must pay | More than $800 of Delaware tax after withholding and credits | [PIT-EST (2026)](https://revenuefiles.delaware.gov/2025/PITForms_Instructions/Instructions/PIT-EST_Instructions_2026-01.pdf) item 2; 30 Del. C. § 1169(a) |
| The two tests | 90% of this year's tax; or 100% of last year's, 110% above $150,000 of last year's AGI ($75,000 for filing status 3) | PIT-EST item 10; [PIT-UND (2025)](https://revenuefiles.delaware.gov/2025/PITForms_Instructions/PIT-UND_2025-01_PaperInteractiveIPM.pdf) Part 1 |
| No penalty | Required payment less withholding of $800 or less; no liability last year; a part-year resident | PIT-UND line E; [PIT-UND instructions](https://revenuefiles.delaware.gov/2025/PITForms_Instructions/Instructions/PIT-UND_Instructions_2025-01.pdf) |
| Due dates | A quarter each on April 30, June 15, September 15 and January 15. Withholding counts in the period it was withheld | PIT-EST item 8; PIT-UND line 30 |
| The penalty | 0.0005 a day from each due date (May 1 for the first) until paid or April 30, 2027; the statute says 1 1/2% a month | PIT-UND lines 39 to 42; § 535(b) |
| Annualising | 22.5%, 45%, 67.5% and 90% of the tax on annualised income, with the federal recapture | PIT-UND Part 3 |

$40,000 of Delaware tax with nothing withheld or paid owes $36,000 by the
year, $9,000 a quarter, and a penalty of $4,567.50 figured to April 30, 2027
(`TestEstimatedPenaltyArithmetic`). The form's Short Method would charge
$4,320.00, and the result reports that beside the figure.

The code is in [`estimated.go`](estimated.go), and the 2026 figures in
[`estimated_y2026.go`](estimated_y2026.go).

## The forms are followed, and where the statute differs is reported

Each result's `Divergences` lists six places where the Division's forms and
the Delaware Code read differently: the penalty by the day rather than the
month; the first installment's days counted from May 1; the $800 test on the
required payment; the part-year exception; the late-start dates on PIT-EST;
and the Short Method. The figures follow the forms. The research behind each
is in
[`research/estimated-payments/de.md`](../research/estimated-payments/de.md).

Three questions the research left open come back in `Required` with the
default used: whether the 110% step tests Delaware or federal AGI (Delaware,
as PIT-UND says), whether no liability last year needs a twelve-month year
(yes, as PIT-EST says), and whether a fourth payment on January 16 is on time
(no, as the form's column heading says).

## No extras

Delaware's rules read nothing beyond `estimated.Facts`. Facts carrying another
jurisdiction's extras are refused.

## Not built

Farmers and fishermen, who pay 66 2/3% by January 15 or file and pay by
March 1, get `estimated.ErrUnsupported`. S corporation payments and
refundable business credits, which PIT-UND spreads evenly, have no place in
the facts. Short tax years are not covered. The City of Wilmington's earned
income and net profits taxes are not covered.
