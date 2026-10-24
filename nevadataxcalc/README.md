# nevadataxcalc

Nevada's constitution bars an income tax on individuals, so there is no Nevada
estimated tax. This package says so through the shared
[`estimated.EstimatedTax`](../estimated/estimated.go) interface, so a caller
asking about Nevada gets an answer rather than an error.

| Question | Answer for 2026 |
|---|---|
| Are estimated payments required? | No. `Requirement` is exempt, with no target |
| When are they due? | Never. `Schedule` has no installments |
| What is the penalty? | Nothing. `Penalty` is zero |

The finding rests on [the Nevada Constitution, Article 10, Section
1(9)](https://www.leg.state.nv.us/Const/NvConst.html). The extract is checked
into [`testdata/`](testdata/SOURCES.md), and the tests require each result to
cite it.
