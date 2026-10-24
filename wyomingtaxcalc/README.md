# wyomingtaxcalc

Wyoming has no individual income tax, so there is no Wyoming estimated tax.
This package says so through the shared
[`estimated.EstimatedTax`](../estimated/estimated.go) interface, so a caller
asking about Wyoming gets an answer rather than an error.

| Question | Answer for 2026 |
|---|---|
| Are estimated payments required? | No. `Requirement` is exempt, with no target |
| When are they due? | Never. `Schedule` has no installments |
| What is the penalty? | Nothing. `Penalty` is zero |

The finding rests on [Wyoming Statutes title 39, chapter
7](https://wyoleg.gov/statutes/compress/title39.pdf) and [the State of
Wyoming's About Wyoming page](https://www.wyo.gov/about-wyoming). The extracts
are checked into [`testdata/`](testdata/SOURCES.md), and the tests require each
result to cite them.
