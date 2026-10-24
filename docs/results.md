# Results

The [`taxyear`](../taxyear) package computes a tax year from a
[`TaxYearScenarios` file](scenarios.md). For each scenario it returns a
**projection** of every model into dated items, the federal return, an
estimated-payment answer per jurisdiction, the questions left open, and the
**working** behind each figure. The output is one `TaxYearResult` message,
defined in [`scenarios/v1/result.proto`](../scenarios/v1/result.proto) and
[`scenarios/v1/projection.proto`](../scenarios/v1/projection.proto).

## Computing from Go

```go
file, err := taxyear.Parse(jsonc) // JSON with comments
if err != nil { ... }
result, err := taxyear.Compute(file)
if err != nil { ... } // only for a nil file

plan, _ := result.Scenario("plan")
path, _ := taxyear.ParsePath("federal.form_1040.total_tax")
if f, ok := plan.Figure(path); ok {
	amount, _ := f.Amount() // money.USCents
	working := f.Working()  // nil when the figure has none
}
```

`Result` and `Scenario` are immutable; `Proto()` returns a copy of the
message. A [`Cache`](../taxyear/cache.go), from `taxyear.NewCache(size)`,
computes the same result and reuses unchanged scenarios between calls.

## TaxYearResult

| Field | Meaning |
|---|---|
| `schema_version`, `tax_year`, `as_of` | Copied from the file |
| `computed_by` | The library and version |
| `status` | `IN_PROGRESS` when `as_of` falls in the year, `FINISHED` after it, `NO_RULES` when this build has no federal rate tables for the year |
| `problems` | Problems with the file as a whole. When set, no scenario is computed. |
| `scenarios` | One `ScenarioResult` per scenario, in the file's order |

Each `ScenarioResult` holds:

| Field | Meaning |
|---|---|
| `projection` | Every model expanded into items, with totals |
| `federal` | The federal return: Form 1040 and the schedules this build fills |
| `estimated` | The estimated-payment answer for each jurisdiction |
| `states` | Each state lived or worked in, with the residency |
| `summary` | Totals and limits no form holds, such as the SALT cap and each person's retirement limits |
| `dependents` | Whether each other person counts as a dependent |
| `questions` | Judgement calls the figures needed and the inputs don't answer |
| `not_included` | What the figures leave out |
| `working` | How figures were made, one per path |
| `problems` | This scenario's problems. The other scenarios still compute. |

Every figure is named by a [field path](scenarios.md#field-paths) into its
`ScenarioResult`.

## The projection

`YearProjection` has one list per model kind, in the order of
[`TaxYearModel`](models.md#the-models). Each entry has the same shape:

| Field | Meaning |
|---|---|
| `id` | The model's id |
| `items` | Dated items, by date, then id |
| `so_far_through` | The last day the evidence covers |
| `so_far` | Totals of the made items |
| `rest` | Totals of the expected items |
| `year` | Totals of every item |
| `expected` | Documents still to arrive, such as the W-2 |
| `can_add` | Each part kind that can be added now, filled with its defaults |

An **item** is one dated amount: a paycheck, a month's interest, a sale lot, a
payment. Its `status` is `MADE` (read from evidence) or `EXPECTED` (from an
assumed part). Its `origin` names the part and evidence it came from. Its
`estimated` field lists the paths a rule filled, such as
`paycheck.federal_withheld`.

Item ids are the same in every scenario, such as
`employers/acme/2026-10-15/regular`, so two scenarios' items compare by id.

Each totals message has two layers:

- **Item totals** (`items`) are the sum of the items, in the items' terms.
- **Box totals** (`boxes`) are in a form's terms: W-2 boxes for a job,
  1099-INT and 1099-DIV boxes for an account, Form 8949 boxes for a broker,
  Schedule C lines for a business, Schedule A inputs for an itemised model. A
  form that states the year, such as a W-2, states the box totals itself.

An `ADJUSTMENT` item covers a difference from a document's year-to-date
figures, or an [override](scenarios.md#overrides).

## The federal return

`federal` is taxprotos' `FederalReturn`, filled in Form 1040's order:
self-employment tax, Schedule D, adjusted gross income, the deduction,
taxable income, the tax, then payments. This build fills:

| Form | Lines |
|---|---|
| Form 1040 | Filing status, wages (1a), interest and dividends (2, 3), retirement income (4, 5), Social Security benefits received (6a, not the taxable part), capital gain (7), total income (9), adjustments (10), AGI (11), deduction (12), taxable income (15), tax (16), other taxes (23), total tax (24), withholding (25), estimated payments (26), total payments (33), overpaid (34) or owed (37) |
| Schedule 1 | Business income, alimony, state refunds, unemployment, rental, other income; HSA and IRA deductions, half of self-employment tax |
| Schedule 2 | Self-employment tax, the 10% tax on early distributions, the § 4973 excess contributions tax |
| Schedule 3 | Social Security tax withheld over the limit (line 11) |
| Schedule A | Medical after the floor, state and local taxes after the cap, mortgage interest, gifts, total |
| Schedule D | Capital gains and loss carryovers |

Without an elected filing status, single is used and the result asks.
A state refund is taxable only as far as the State and Local Income Tax
Refund Worksheet in the Form 1040 instructions finds, from last year's Form
1040 and Schedule A in the facts. Schedule A without its line 17 reads the
itemised total from Form 1040 line 12. Without the return, a refund from a
year that itemised counts in full, `not_included` says so, and the result
asks for it.
`not_included` lists what the return leaves out where it could apply: tax
credits, the Alternative Minimum Tax, the Net Investment Income Tax, the
qualified business income deduction and the Additional Medicare Tax.

`states` gives each state's residency only. State income tax is not worked
out yet, and `not_included` says so.

## Estimated payments

`estimated` has one entry per jurisdiction: `US`, then each state with a tax
paid model, a residence period or W-2 wages, by postal code. Each entry holds
this year's tax as the rules measure it, last year's tax, AGI and withholding,
and `rules`: the [estimated package's](estimated-tax.md) answer field for
field (requirement, installments, penalty, explanation, questions,
divergences).

The rules run twice. The first pass counts the payments made by `as_of` and
the planned ones, and finds what is still needed. A `spread_the_rest` default
shares that equally across the open due dates. The second pass counts every
payment and gives the installments and the penalty.

`summary` adds up the jurisdiction's withholding, payments made, payments
still expected, and what is due with the return. A state's `tax_for_year` is
unset while the year is in progress, unless a scenario
[overrides](scenarios.md#overrides) it.

Only payments made count on the return. Planned payments and the spread
default change only the penalty "if the planned payments are made".

## Working

A `Working` explains one figure, by `path`. Its `terms` are what the figure
adds up: other figures by path, or items by id, each with a sign. `parts` and
`sources` name the model parts and evidence it rests on. `rules` cites each
published rule applied and what it did. `exact` is the figure before
rounding. For an override, `automatic` is the computed figure.

## Rounding

Every figure is whole dollars, rounded half away from zero.

- **Items** are rounded per part with a running total, as
  [models.md](models.md#rounding) describes.
- **Item totals** are the sum of the rounded items.
- **Box totals** round so far's and the rest's once each. The year's is their
  sum.
- **Return lines** computed by a rule, such as the tax on taxable income, are
  rounded once, where they are computed.
- **Every total adds the figures under it** and is never rounded again from
  cents: AGI, the deduction, total tax, total payments. For example
  `total_payments` is `total_withheld` plus `estimated_tax_payments` plus
  Schedule 3 line 11, and `total_tax` is `tax` plus `other_taxes`.

The estimated-payment rules read the rounded totals.
