# Testing

Every figure the rules compute cites the document it comes from. This
document plans how the tests are brought up to the same standard: every
expected value in a test says where it came from, and the figures that matter
are checked against something other than this code.

Today almost nothing is. Most expected values are either outputs of earlier
versions of this code, frozen, or worked out by hand by the person who wrote
the rule. Both catch a change. Neither catches a rule that was wrong from the
start. The plan labels every expected value with its provenance, adds checks
against published tables and worked examples, runs an independent model as a
cross-check, and publishes every end-to-end case on the docs site.

## Contents

1. [Kinds of test](#kinds-of-test)
2. [Provenance](#provenance)
3. [Case files](#case-files)
4. [Coverage today](#coverage-today)
5. [Authoritative sources](#authoritative-sources)
6. [Tax tables and properties](#tax-tables-and-properties)
7. [Independently worked cases](#independently-worked-cases)
8. [Cross-model harness](#cross-model-harness)
9. [Test case browser](#test-case-browser)
10. [Order of work](#order-of-work)

## Kinds of test

| Kind | What it checks | Expected values from |
|---|---|---|
| Engine | The taxlisp parser, typechecker and evaluator; Go and Rust agree | The language's own definition |
| Rule unit tests | One worksheet or line at a time | Hand-worked from the form |
| Tax tables | Every row of a published tax table | The vendored PDF |
| Properties | Relations that hold for every household, such as more income never lowering tax | The relation itself |
| End-to-end cases | A household in, the whole return or estimated tax answer out, line by line | Labelled per figure, see [provenance](#provenance) |
| Cross-model | Generated households against another model | A report, never an oracle |
| `taxyear` | The whole-year projection and its scenarios | Unit tests |

Only end-to-end cases are catalogued one by one. The rest are summarised.

## Provenance

Each expected figure in an end-to-end case carries a provenance: where the
figure came from. From strongest to weakest:

| Provenance | The expected figure is | Counts as authoritative |
|---|---|---|
| `PUBLISHED_EXAMPLE` | Printed in an agency's worked example: form instructions, an IRS publication such as Pub 505, a state booklet | Yes |
| `PUBLISHED_TABLE` | A row of a published tax table or rate schedule | Yes |
| `INDEPENDENTLY_WORKED` | Worked from the form by two independent workers who did not see the rules, and agreed (see [below](#independently-worked-cases)) | No, shown separately |
| `HAND_WORKED_REVIEWED` | Worked from the form by the rule's author and checked by someone else | No, shown separately |
| `HAND_WORKED` | Worked from the form by the rule's author | No |
| `FROZEN_REGRESSION` | Output of an earlier version of this code, kept so a change shows | No |

Only the first two count towards coverage. An agency printed the figure, so
it does not depend on anyone's reading of the form.

The household has a provenance of its own, separate from its figures:
`AGENCY_EXAMPLE` (from a worked example), `EFILE_TEST_SCENARIO` (from a
government e-file test scenario, such as the IRS MeF ATS scenarios), or
`CONSTRUCTED`. E-file test scenarios print the inputs and leave the computed
lines blank, so a case built on one has an agency's household and figures
worked from the form.

Every authoritative or worked figure names its source anchor, the same way
the rules cite theirs, and the cite checker checks it.

## Case files

Each end-to-end case is one textproto file,
`cases/<jurisdiction>/<year>/<case>.textproto`, read by the Go and the Rust
tests alike. Federal-only cases are under `cases/us/`, and cases with more
than one state under `cases/multi/`. A state case lists the federal lines it
depends on as well.

A case is a `TestCase` message (`cases/v1/case.proto`):

- an id, a title, topic tags, and the household's provenance and source;
- the `ReturnInputs`, exactly as the preparer takes them;
- the expected figures: for each asserted line, the form, the line, the
  amount in cents, its provenance and source anchor, and for worked figures
  the working, step by step. A line a case does not list is not checked.

Cases may run ahead of the rules. A case can assert a line the rules do not
compute yet, such as one the source catalog marks `not_modeled`, by listing
it as a known gap with the reason. The runner reports known gaps without
failing, and fails when a known gap starts passing, so the marker is removed
and the gain is recorded. As the rules grow, more of each case passes, and the
browser shows each case's passing share. A case's inputs must still fit
`ReturnInputs`: a situation the input schema cannot express waits for the
schema.

Tax tables are not cases: each is one data file extracted from its PDF. The
frozen estimated tax vectors stay in `golden/testdata/estimated`, labelled
`FROZEN_REGRESSION` by a manifest.

## Coverage today

Counted at commit `08e30bf`.

| Test set | Cases | Provenance |
|---|---|---|
| Federal return tests, `returns/rules/federal/federal_test.go` | 44 test functions, many households each | `HAND_WORKED`, with the working in comments |
| State return tests, `returns/rules/states/<st>_test.go` | 39 states, DC and Washington (California and Oregon are in `states_test.go`), 2 to 8 test functions each, about 170 | `HAND_WORKED`. About 6 households are published examples (Ohio 2, Kentucky 2, West Virginia 1, South Carolina 1) |
| California and Oregon to the cent, `states/testdata/go_calculators_2025.json` | 44 households | `FROZEN_REGRESSION` (the deleted Go calculators) |
| Estimated tax, `golden/testdata/estimated` | 2,436 household-years over 51 jurisdictions and the reference rules | `FROZEN_REGRESSION` (the deleted Go calculators) |
| Go and Rust parity, `golden/testdata/returns` and `taxlisp` | 417 return and eval vectors, plus the taxlisp IR vectors | Regenerated from the Go engine |
| `taxyear` tests | about 60 test functions in the top package | Unit tests |

No case checks a published table row or a cross-model result. Of the 42
jurisdictions with a return (41 states and DC), only California and Oregon are
held to the cent across the brackets, and only against the old code. The
rest are checked against nothing but their rule author's own working. The
case files make this a generated report rather than a count by hand.

## Authoritative sources

Checked against each source's own page in November 2026. Federal works are in
the public domain (17 U.S.C. 105). State works are not covered by that rule;
each source's catalog entry records its reuse terms, as citations already do.

| Source | Covers | Licence | Use |
|---|---|---|---|
| [2025 Form 1040 instructions](https://www.irs.gov/pub/irs-pdf/i1040gi.pdf): Tax Table and Tax Computation Worksheet | Federal tax on taxable income, every filing status | Public domain | Already vendored. `PUBLISHED_TABLE`. |
| [Pub 505](https://www.irs.gov/publications/p505) (2025 and 2026), [Pub 17](https://www.irs.gov/pub/irs-pdf/p17.pdf) | Worked examples of single rules: estimated tax, withholding, credits | Public domain | Vendor. `PUBLISHED_EXAMPLE` where an example prints a figure. |
| [IRS MeF ATS scenarios, tax year 2025](https://www.irs.gov/e-file-providers/tax-year-2025-form-1040-series-and-extensions-modernized-e-file-mef-assurance-testing-system-ats-information) | 15 scenarios: Form 1040 (8), 1040-SS, 4868, 1040-NR (5) | Public domain | Vendor. Households only; the computed lines are blank. |
| State resident booklets with a tax table | Tax by income row. The vendored PDFs appear to hold full tables for AR, CA, HI, KS, MD, MN, MT, NJ, OK, OR, RI, SC and WI. Each is confirmed when its test is written | Per state; none read carries a copyright notice | Already vendored. `PUBLISHED_TABLE`. |
| State tables published apart from the booklet | NY, VA, CT and SC (SC1040TT) | Per state | Vendor beside the booklet. |
| State e-file test packages | NY publishes Pub 95-T, whose scenarios also leave the computed lines blank. SC posts 2025 developer test cases. Most states give packages only to enrolled e-file developers | Per state | Public ones only: NY and SC. |
| [PolicyEngine-US](https://github.com/PolicyEngine/policyengine-us) | Federal, all states and DC, 2025 | AGPL-3.0 | Cross-model harness. Never vendored. |
| [PSL Tax-Calculator](https://github.com/PSLmodels/Tax-Calculator) | Federal income and payroll tax | CC0-1.0 | A second federal cross-check, later. |

Not used: NBER TAXSIM, which states no licence and codes state law only
through 2018 to 2021; and IRS Direct File, which covers tax year 2024 only and
whose tests check its own output.

Most states compute tax by a formula, a flat rate or a rate schedule, and
print no table. For those, the printed rate schedule is the authoritative
source: the test computes the tax at each bracket edge and inside each
bracket from the schedule's printed figures. Worked examples, where the
booklet has any, and independently worked cases cover the other lines.

The goal is every jurisdiction: each of the 41 states with an income tax
return and DC has its tax computation held to its printed table or schedule,
and each of the 9 states without one (Alaska, Florida, Nevada, New Hampshire,
South Dakota, Tennessee, Texas, Washington for wages, and Wyoming) has a case
that its return is empty. Washington's capital gains tax is held to its
printed rate and deduction.

## Tax tables and properties

**Tax tables.** Every row of every published tax table is reproduced. A table
row gives the tax for an income range, computed at the range's midpoint. A
checked-in extractor reads the table from the vendored PDF once, into a data
file beside it that records the PDF's SHA-256. The test runs the rule at each
row for each filing status and requires the printed figure. The federal Tax
Table has about 2,000 rows and four filing-status columns.

**Properties**, over generated households:

- more wages never mean less tax, before credits that phase in;
- a dollar more income never changes tax by more than the top rate on that
  dollar, except at a cliff the form prints, which the test names;
- filing status relations the law fixes, such as married filing jointly
  brackets being twice the single brackets where the statute says so;
- every amount follows its form's rounding rule, checked at the boundaries;
- Go and Rust agree on every generated household.

## Independently worked cases

Most state lines have no published table or example. For those, figures are
worked by two independent workers, people or AI models, under these rules:

- A worker sees only the household and the vendored form and instructions,
  never the rules or the existing tests.
- Two workers do each case separately. A figure is accepted only when both
  agree; a disagreement is settled from the form by a person.
- The working is data: each step's formula, its inputs, and the source anchor
  of each constant. A checker confirms that every constant appears on its
  cited page, and a second checker, which does not use the engine, redoes the
  arithmetic.

Such figures are `INDEPENDENTLY_WORKED`. They are shown apart from published
figures, because two workers can share a misreading. Households are chosen to
reach every line the source catalog lists for the form.

## Cross-model harness

The harness runs generated households through this engine and through another
model, and reports the differences form line by form line.

- **The first model is PolicyEngine-US**, federal and state income tax, tax
  year 2025. It is built and run by Bazel from its own module,
  `crossmodel/policyengine`, which the main module takes as a
  `dev_dependency`, so a module that depends on taxcalcs never sees it. The
  module pins Python with `rules_python` and installs `policyengine-us` from
  a lock file with hashes. A run is reproducible, and works offline once the
  wheels are in Bazel's repository cache. PolicyEngine-US is AGPL-3.0, so its
  code is never checked in or shipped with taxcalcs; only its output figures
  are kept.
- **Households** come from a seeded generator over inputs both models
  understand: wages, interest, dividends, capital gains, Social Security,
  pensions, dependants, filing status and state. The seed is in the report.
- **A checked-in mapping** pairs our form lines with the other model's
  variables. A line with no counterpart is listed as unmapped.
- **The report** gives, per jurisdiction and line, how many households agree
  to the dollar, and the largest differences with their inputs.
- **Triage**: each difference is recorded as our bug (with an issue), their
  bug (with an upstream issue), a legitimate difference of reading (with
  the citation for ours), or unmodeled: a figure our rules take as an input
  rather than computing, which the generated households leave empty. The
  unmodeled rows are where cases running ahead of the rules are written. An
  untriaged difference fails the harness.

The harness is a report and a source of test households, never an oracle. A
difference that turns out to be our bug becomes a case whose figures are
worked from the form, not taken from the other model.

The Go side, which prepares our returns and writes the report, is in the main
module under `crossmodel/`. Its target is tagged `manual`, so
`bazel test //...` does not build it and needs no Python. It runs as
`bazel run //crossmodel:report`, and in a CI job that is not required. Its
latest report is checked in.

## Test case browser

The docs site gets a Tests section, built by `docsmodel` from the case files.

- **An index** of every case, filtered by jurisdiction, form, provenance and
  topic tag, with a short list of featured scenarios.
- **A page per case**: the household, then each form with every asserted
  line's expected figure, the engine's figure, and its provenance. A line
  opens into its working and its sources, on the walkthrough component the
  site already uses for sample households. A failing case is shown failing.
- **A coverage map per form**: each line the source catalog lists, coloured by
  the strongest provenance that checks it, so the lines with no authoritative
  check are visible.
- **Unit tests**: a count per package and the rules they exercise.

## Order of work

Each step is one pull request, with the `bazel` and `go` checks green.

1. This document.
2. The case file format and its runner, the published examples moved into it,
   and the coverage report.
3. The federal Tax Table test.
4. State tax computation tests for every jurisdiction: the printed table
   where there is one, otherwise the printed rate schedule or flat rate.
5. Property tests.
6. The cross-model harness with PolicyEngine-US, and its first report.
7. Cases from published examples and e-file test scenarios, and a pilot of
   independently worked cases in two states.
8. The test case browser.

A bug a new test finds is filed as an issue or fixed in its own pull request
with its citation. A test is never loosened to pass.

Out of scope: US territories. The rules do not cover Puerto Rico, Guam, the
US Virgin Islands, the Northern Mariana Islands or American Samoa, nor Form
1040-SS, so there is nothing yet to test.
