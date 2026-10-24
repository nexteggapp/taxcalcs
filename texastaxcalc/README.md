# texastaxcalc

Texas's constitution bars a tax on individuals' net income, so there is no
Texas estimated tax. This package says so through the shared
[`estimated.EstimatedTax`](../estimated/estimated.go) interface, so a caller
asking about Texas gets an answer rather than an error.

| Question | Answer for 2026 |
|---|---|
| Are estimated payments required? | No. `Requirement` is exempt, with no target |
| When are they due? | Never. `Schedule` has no installments |
| What is the penalty? | Nothing. `Penalty` is zero |

The finding rests on [H.J.R. 38 (2019),
enrolled](https://capitol.texas.gov/tlodocs/86R/billtext/pdf/HJ00038E.pdf) and
[the Governor's proclamation of the 2019
results](https://gov.texas.gov/news/post/governor-abbott-issues-proclamation-declaring-the-results-of-the-recent-constitutional-amendment-election).
The extracts are checked into [`testdata/`](testdata/SOURCES.md), and the tests
require each result to cite them.
