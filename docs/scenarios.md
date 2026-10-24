# Tax year scenarios

`TaxYearScenarios` is one tax year's inputs, as one protobuf message. It holds
the household's facts, a [model](models.md) of every source of income,
deduction and payment, and one or more **scenarios**. A scenario is one plan
for the rest of the year, such as "Raise in Oct". The first scenario is always
**Your plan**.

The file holds inputs only. No figure in it is worked out.
[`taxyear.Compute`](../taxyear/compute.go) and the
[`taxyear` command](taxyear-cli.md) compute every scenario from it; the output
is described in [results.md](results.md).

The messages are in [`scenarios/v1/scenarios.proto`](../scenarios/v1/scenarios.proto)
and [`scenarios/v1/inputs.proto`](../scenarios/v1/inputs.proto).

## The file

```
TaxYearScenarios { schema_version, tax_year, as_of, exported_from, ScenarioInputs shared, repeated Scenario scenarios }
ScenarioInputs   { TaxYearFacts facts, TaxYearModel model, repeated Override overrides }
Scenario         { id, name, parent, ScenarioInputsDiff changes }
Override         { path, Money amount, repeated sources, Money was }
```

| Field | Meaning |
|---|---|
| `schema_version` | Always 1. A reader refuses a version it doesn't know. |
| `tax_year` | The tax year, as four digits. |
| `as_of` | Today. Anything dated after it is the rest of the year. |
| `exported_from` | The version of the inputs the file came from, in the exporting program's own form. Opaque. |
| `shared` | The inputs every scenario starts from. |
| `scenarios` | Your plan first, then the others. |

`TaxYearFacts` comes from [taxprotos](https://github.com/nexteggapp/taxprotos):
the people, residence, elections, positions, carryovers, last year's returns,
documents not yet assigned to a model, and adjacent years' payments.

## Shared inputs

`shared` holds the facts, each model's details, every known part with its
evidence, each model's default part, and overrides of figures so far. A
**known part** is one that holds evidence, such as paystubs; see
[models.md](models.md#parts). Known parts live only in `shared`, because
evidence is the same in every scenario.

## Scenarios as diffs

A scenario's `changes` is a `ScenarioInputsDiff`: a change to its parent's
inputs. [protodiffgen](https://github.com/nexteggapp/protodiffgen) generates
the diff message from [`inputs.diff.textproto`](../scenarios/v1/inputs.diff.textproto),
and its `apply` package validates and applies it.

The diff config makes these choices:

- Every form, document ref, ledger ref, `PayFigures`, `Money`, `Date` and
  `Quantity` is **atomic**: replaced whole, never patched.
- Every list of models, parts, inline evidence and facts is **keyed** by `id`.
  A change to one item is `add`, `remove` or `patch`, named by the item's key.
- Overrides are keyed by `path`.
- A list whose items have no id, such as a part's `replaces`, is replaced
  whole.
- A diff message's `clear` lists the field numbers that become unset.

This change adds a `payRate` part to the `acme` employer:

```jsonc
"changes": { "model": { "employers": [ { "id": "acme", "patch": { "parts": [
  { "id": "acme-v2k8n5zd", "add": { "id": "acme-v2k8n5zd", "payRate": {
      "from": { "year": 2026, "month": 10, "day": 1 },
      "paycheck": { "gross": { "minor": "625000", "currency": "USD" } } } } }
] } } ] } }
```

### The parent chain

A scenario's inputs are `shared` with each diff down its `parent` chain
applied in turn, Your plan's first. A later diff wins over an earlier one.
[`taxyear.Resolve`](../taxyear/resolve.go) applies these rules:

1. The first scenario is Your plan, with id `plan` and no `parent`.
2. Your plan's changes apply to `shared`. Every other scenario names a
   `parent`.
3. A parent must be an earlier scenario that resolved, so no chain loops.
4. Scenario ids are unique.
5. No scenario adds or changes a known part.

A scenario that breaks a rule, or whose diff doesn't apply, is reported in its
own `problems` and left out. A scenario whose parent failed fails too. The
others still compute.

## Field paths

A **field path** names one value in a message: proto field names joined by
dots, and a list item by its key in brackets.

```
path = step *("." step)
step = field-name ["[" key "]"]
```

The key is the item's first string field among `id`, `jurisdiction`, `state`,
`scenario`, `path` and `person`. An item with none of those is named by its
position, from 0. A key may contain dots and slashes, as item ids do.

Examples, in a scenario's result:

- `projection.employers[acme].year.boxes.wages`: box 1 of Acme's W-2 for the
  year.
- `federal.form_1040.w2_wages`: Form 1040 line 1a.
- `estimated[CA].rules.requirement`: California's estimated-payment
  requirement.

The same grammar names places in the inputs, such as
`model.employers[acme].default_part`. [`taxyear.ParsePath`](../taxyear/path.go)
parses one.

## Overrides

An **override** is a value a person typed in place of a computed figure.
`path` names the figure in the scenario's result. `sources` records the ids of
the parts and evidence the figure came from when it was typed. `was` records
the figure the person saw.

Only these paths can be overridden:

| Path | Figure | Goes in |
|---|---|---|
| `projection.<list>[<id>].so_far.items.<line>` | A figure so far, in item terms | `shared` |
| `estimated[<j>].last_year_tax`, `.last_year_agi`, `.last_year_withheld` | Last year's figures for a jurisdiction | `shared` |
| `estimated[<state>].tax_for_year` | A state's tax for the year, while it isn't worked out | A scenario's `changes` |

An override of a figure so far adds an `ADJUSTMENT` item dated
`so_far_through`, so the items still add up to the typed figure. The figure's
working records the computed value as `automatic`. It sets `sources_changed`
when today's computed figure differs from `was`.

## The JSONC format

On disk the file is the protobuf JSON mapping of `TaxYearScenarios`, with
comments. Field names are camelCase. `Money` is `{ "minor": "<cents>",
"currency": "USD" }`, and `Date` is `{ "year", "month", "day" }`.
[`testdata/example.jsonc`](../scenarios/v1/testdata/example.jsonc) is a
complete file:

```jsonc
// TaxYearScenarios, schema version 1: the inputs to one tax year.
{
  "schemaVersion": 1, "taxYear": 2026,
  "asOf": { "year": 2026, "month": 9, "day": 8 },
  "shared": { "model": { "employers": [
    { "id": "acme", "name": "Acme Corp", "person": "you",
      "payFrequency": "PAY_FREQUENCY_SEMI_MONTHLY",
      "firstPayDate": { "year": 2026, "month": 1, "day": 15 },
      "work": { "state": "TX" }, "supplemental": "SUPPLEMENTAL_METHOD_FLAT",
      "defaultPart": { "id": "acme-d", "eachPaycheck": {
        "gross": { "minor": "500000", "currency": "USD" } } } } ] } },
  "scenarios": [ { "id": "plan", "name": "Your plan" } ]
}
```

A document ref's `vaultRef` is the caller's opaque reference to the stored
document. The calculators never read it.

[`taxyear.Parse`](../taxyear/parse.go) reads the file strictly. It removes
`//` and `/* */` comments, keeping line numbers. It then refuses:

- an unknown field, which catches a computed figure added to the file;
- a message whose `oneof` has no case set;
- an enum written as its `UNSPECIFIED` value.

Before any scenario is computed, the file as a whole is checked:

- `schema_version` is 1;
- `shared.facts.tax_year`, when set, equals `tax_year`;
- `as_of` is a real day;
- no identifiers: fields such as `tin` and `account_number` are empty, no
  party has an address or telephone, and no text field holds nine digits in
  a row, with or without dashes.

A failure here is in the result's top-level `problems`, and no scenario is
computed.
