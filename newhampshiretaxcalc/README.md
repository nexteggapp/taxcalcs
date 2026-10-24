# newhampshiretaxcalc

New Hampshire has no individual income tax for 2026. It does not tax wages, and
its interest and dividends tax was repealed from January 1, 2025, so there is
no New Hampshire estimated tax. This package says so through the shared
[`estimated.EstimatedTax`](../estimated/estimated.go) interface, so a caller
asking about New Hampshire gets an answer rather than an error.

| Question | Answer for 2026 |
|---|---|
| Are estimated payments required? | No. `Requirement` is exempt, with no target |
| When are they due? | Never. `Schedule` has no installments |
| What is the penalty? | Nothing. `Penalty` is zero |

New Hampshire's business profits tax is not covered.

The finding rests on [RSA chapter
77](https://gc.nh.gov/rsa/html/V/77/77-mrg.htm) and [the Department of Revenue
Administration's Interest & Dividends Tax
page](https://www.revenue.nh.gov/taxes-glance/interest-dividends-tax). The
extracts are checked into [`testdata/`](testdata/SOURCES.md), and the tests
require each result to cite them.
