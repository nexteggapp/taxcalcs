# southdakotataxcalc

South Dakota has no state income tax, so there is no South Dakota estimated
tax. This package says so through the shared
[`estimated.EstimatedTax`](../estimated/estimated.go) interface, so a caller
asking about South Dakota gets an answer rather than an error.

| Question | Answer for 2026 |
|---|---|
| Are estimated payments required? | No. `Requirement` is exempt, with no target |
| When are they due? | Never. `Schedule` has no installments |
| What is the penalty? | Nothing. `Penalty` is zero |

The finding rests on [the Department of Revenue's Taxes
page](https://dor.sd.gov/individuals/taxes/). The extract is checked into
[`testdata/`](testdata/SOURCES.md), and the tests require each result to cite
it.
