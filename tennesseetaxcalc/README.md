# tennesseetaxcalc

Tennessee does not tax wages, and its Hall tax on interest and dividends was
repealed from 2021, so there is no Tennessee estimated tax. This package says
so through the shared [`estimated.EstimatedTax`](../estimated/estimated.go)
interface, so a caller asking about Tennessee gets an answer rather than an
error.

| Question | Answer for 2026 |
|---|---|
| Are estimated payments required? | No. `Requirement` is exempt, with no target |
| When are they due? | Never. `Schedule` has no installments |
| What is the penalty? | Nothing. `Penalty` is zero |

The finding rests on [the Department of Revenue's Hall Income Tax
page](https://www.tn.gov/revenue/taxes/hall-income-tax.html) and [the Tennessee
Constitution, Article II, Section 28, in the Tennessee Blue Book
2025-2026](https://publications.tnsosfiles.com/pub/blue_book/25-26/25-26_TennesseeState.pdf).
The Department's page is checked into [`testdata/`](testdata/SOURCES.md) as an
extract. The Blue Book could not be downloaded, so the constitution's sentence
is checked against the research file's verified quote instead. The tests
require each result to cite both.
