# californiataxcalc

California personal income tax, with every rate traced to the booklet the
Franchise Tax Board printed it in.

It starts where a California return starts: federal adjusted gross income. You
hand it that figure, the Schedule CA adjustments, and what you could itemise,
and you get back California taxable income, the tax on it, and the working.

```go
income, err := californiataxcalc.TaxableIncome(2025, filing.Single,
    150_000*money.Dollar,
    californiataxcalc.Adjustments{ItemisedDeductions: 9_400 * money.Dollar})

tax, err := californiataxcalc.OrdinaryIncomeTax(2025, filing.Single, income.Value().Amount)
credit, err := californiataxcalc.ExemptionCredit(2025, filing.Single, 150_000*money.Dollar,
    californiataxcalc.Exemptions{Personal: 1})
```

```
federal AGI    $150000.00
deduction      $9400.00 (standard $5706.00, itemised $9400.00)
taxable        $140600.00
tax            $9514.44
exemptions    -$153.00
owed           $9361.44
marginal rate  9.3%

why:
  1% on $11079.00 = $110.79
  2% on $15185.00 = $303.70
  4% on $15188.00 = $607.52
  6% on $16090.00 = $965.40
  8% on $15182.00 = $1214.56
  9.3% on $67876.00 = $6312.47

unanswered:
  deduction method
  residency
```

That is [`example_test.go`](example_test.go), and its output is checked by the
suite, so the figures above cannot drift from what the library returns.

## Itemising here is a separate decision from itemising federally

Look at the example again. The filer's $9,400 of itemised deductions lost to the
federal standard deduction of $15,750 and **wins** here, because California's
standard deduction is $5,706.

That is the usual case, not an odd one. A product that carries the federal
answer across will take the standard deduction for a large number of people who
should not, and Rev. & Tax. Code § 17073.5 does not bind a filer to what the
federal return elected anyway. `TaxableIncome` makes the comparison separately
and still reports the election as unanswered, because a filer may choose
otherwise.

## California has no capital gains rate

There is deliberately no `LongTermGainsTax` in this package. Rev. & Tax. Code
§ 18031 conforms to the federal definition of gross income but not to the
federal preferential rates, so a long-term gain is taxed exactly like wages —
up to 12.3%, or 13.3% once the behavioural health surcharge is counted.

Fold the whole gain into the figure you pass to `TaxableIncome`. The federal
calculators require the opposite, and getting the two the same way round is the
mistake this note exists to prevent.

## What it computes

| | Rule | Code |
|---|---|---|
| Income tax | Rev. & Tax. Code § 17041, Schedules X, Y and Z | [`tax.go`](tax.go) |
| Taxable income | § 17072, from federal AGI via Schedule CA | [`income.go`](income.go) |
| Standard deduction | § 17073.5 | [`income.go`](income.go) |
| Exemption credits | § 17054, with the AGI limitation | [`income.go`](income.go) |
| Behavioural health surcharge | § 17043, 1% above $1,000,000 | [`tax.go`](tax.go) |
| Estimated tax and its penalty, 2026 | §§ 19136, 19136.1, 19136.3, 19521; Form 540-ES and FTB 5805 | [`estimated.go`](estimated.go) |

Estimated tax follows the FTB's forms where they and the Revenue and Taxation
Code part ways, and every result lists those places in its `Divergences`.

A nonresident or part-year resident, known from the residence periods, files
Form 540NR. FTB 5805 prorates such a filer's tax only in the annualised
method: Part III line 12 is Worksheet I, "Prorated Tax for Form 540NR
Filers", in [`worksheet_i.go`](worksheet_i.go), from the columns given in
`EstimatedFacts.WorksheetI`. The regular method takes the Form 540NR tax as
filed and last year's tax as shown, and keeps the 30%, 40%, 0% and 30%
installments; nothing in FTB 5805 or the 540-ES instructions changes them
after a move. Worksheet I doesn't say how its rates on lines C, H and N
round, and `Required` reports that as unsettled.

The surcharge was the Mental Health Services Tax until Proposition 1 renamed it
for taxable years beginning on or after 1 January 2025. The charge did not
change; only the name on line 62 did.

## Where the numbers come from

Everything for 2025 is the [2025 Personal Income Tax
Booklet](https://www.ftb.ca.gov/forms/2025/2025-540-booklet.pdf) and the
[2025 Form 540](https://www.ftb.ca.gov/forms/2025/2025-540.pdf). Text extracted
from both is checked in beside the tests, so you can verify a figure without
leaving the repository. See
[`testdata/SOURCES.md`](testdata/SOURCES.md).

The strongest check is the booklet's own arithmetic. Beside every bracket the
Franchise Tax Board prints the tax on all income below it — the cumulative sum
over every band beneath. The suite computes the tax at each of those points and
compares: **45 published base amounts across five filing statuses**. A single
wrong threshold anywhere in a schedule breaks every base amount above it.

Beneath the schedules the booklet works a full example, a joint return at
$125,000 of taxable income, and calls it $4,768.10. The suite reproduces it to
the cent — inputs and answer both chosen by the body that wrote the rule.

## What comes next

- **More tax years.** 2025 only. `TableFor` returns `ErrNoTable` for anything
  else rather than reaching for an adjacent year's brackets, so a stale build
  fails loudly instead of computing confidently under the wrong law.
- **Form 540NR.** Every figure here is a full-year resident's.
  `TaxableIncome` returns the residency question unanswered rather than assuming
  it; a part-year resident or non-resident needs apportionment this does not do.
- **Credits beyond the exemptions.** The California Earned Income Tax Credit,
  the Young Child Tax Credit and the renter's credit are not computed.
- **Alternative minimum tax.** Schedule P (540) is not modelled, and the
  exemption credit limitation this package does apply is the AGI worksheet
  rather than the tentative minimum tax limit that can also cap credits.
- **Registered domestic partners.** RDPs file as married for California and
  remain unmarried federally, so their California return starts from a
  recalculated federal AGI that this package takes as given rather than derives.

## This is software, not tax advice

It computes what the published rules say for the figures you hand it. It does
not know your circumstances, and it is not a substitute for a professional.
