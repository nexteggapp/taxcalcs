# floridataxcalc

Florida has no personal income tax, so there is no Florida estimated tax. This
package says so through the shared
[`estimated.EstimatedTax`](../estimated/estimated.go) interface, so a caller
asking about Florida gets an answer rather than an error.

| Question | Answer for 2026 |
|---|---|
| Are estimated payments required? | No. `Requirement` is exempt, with no target |
| When are they due? | Never. `Schedule` has no installments |
| What is the penalty? | Nothing. `Penalty` is zero |

The finding rests on [the Department of Revenue's Tax Information for New
Residents,
GT-800025](https://floridarevenue.com/Forms_library/current/brochure/gt800025.pdf).
The extract is checked into [`testdata/`](testdata/SOURCES.md), and the tests
require each result to cite it.
