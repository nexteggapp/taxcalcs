# taxcalcs

US federal and state tax return rules, written in taxlisp and run by one
engine in Go and in Rust, and `taxyear`, which computes a household's whole
tax year from its facts and a set of what-if scenarios.

Every rate and threshold cites the document it came from, and that document's
text is checked in under [`sources`](sources). Money is an integer count of
cents, never a float. When the law leaves a choice to the filer, such as
whether to itemise, the result says which choice it made and why, instead of
guessing silently.

## Example

The return preparer takes a `ReturnInputs` message and gives a
`ReturnOutputs`: the federal and state returns, form by form, and, when asked,
each jurisdiction's estimated tax.
[`docs/estimated-tax.md`](docs/estimated-tax.md#asking-for-estimated-tax) has
a Go example.

A whole year, from a scenarios file:

```bash
bazel run //cmd/taxyear -- --summary "$PWD/taxyear/testdata/example.jsonc"
```

```
Tax year 2026 as of 2026-09-08: YEAR_STATUS_IN_PROGRESS

plan
  acme: wages $11666.00 so far + $128334.00 rest = $140000.00; federal withheld $0.00 + $20473.00 = $20473.00; 24 items
  federal: AGI $140000.00, taxable $123900.00, tax $22334.00, total tax $22334.00, payments $20473.00, owed $1861.00, overpaid $0.00
  estimated US: ESTIMATED_OUTCOME_COMPUTED, required false, target $20100.60, penalty $0.00
  question federal/filing-status: What is your filing status?
  ...
```

## Packages

| Package | What it does |
|---|---|
| [`returns/v1`](returns/v1), [`returns/preparer`](returns/preparer) | The return preparer's protobuf contract (`ReturnInputs`, `ReturnOutputs`, estimated tax), and its gRPC server |
| [`returns/rules`](returns/rules) | The federal and state rules in taxlisp, with their figures and citations, and the Go engine that runs them |
| [`taxlisp`](taxlisp) | The taxlisp language: parser, typechecker and evaluator |
| [`sources`](sources) | The catalog of source documents the rules cite |
| [`scenarios/v1`](scenarios/v1) | `TaxYearScenarios`, the input to `taxyear`, and `TaxYearResult`, its output |
| [`taxyear`](taxyear) | Computes a tax year from a `TaxYearScenarios` file |
| [`cmd/taxyear`](cmd/taxyear) | The command-line tool for `taxyear` |
| [`money`](money), [`filing`](filing) | The money type and filing status `taxyear` uses |
| [`us/v1`](us/v1) | The estimated tax answer type `scenarios/v1` reports |
| [`rust`](rust) | The Rust port: the same taxlisp engine and rules |

## Documentation

- [`docs/models.md`](docs/models.md): the models of a household's income
  sources, and their parts
- [`docs/scenarios.md`](docs/scenarios.md): the `TaxYearScenarios` file, and
  scenarios stored as diffs of their parent
- [`docs/results.md`](docs/results.md): the projection and the
  `TaxYearResult`
- [`docs/taxyear-cli.md`](docs/taxyear-cli.md): the `taxyear` command
- [`docs/estimated-tax.md`](docs/estimated-tax.md): estimated payments and the
  underpayment penalty, with each state's rules
- `research/estimated-payments/`: notes on each state's estimated-tax law, with
  their sources
- [`sources/catalog`](sources/catalog): where each checked-in source
  document came from

## Contributing

The repository builds and tests on its own, from a clone, with no other
checkout. Run every command below from the repository root. Pull requests run
the same two checks in [`.github/workflows/ci.yml`](.github/workflows/ci.yml).

The schemas come from [taxprotos](https://github.com/nexteggapp/taxprotos) and
the scenario diffs from [protodiffgen](https://github.com/nexteggapp/protodiffgen),
both pinned with `git_override` in [`MODULE.bazel`](MODULE.bazel).

With Bazel (the version is in [`.bazelversion`](.bazelversion)):

```bash
bazel test //...
```

To build against a local checkout of taxprotos:

```bash
bazel test //... --override_module=nexteggapp_taxprotos=/path/to/taxprotos
```

`scenarios/v1/inputs_diff.proto` is generated from the scenario schemas and
checked in for reading. `bazel run //scenarios/v1:write_inputs_diff` updates it,
and `bazel test //...` fails when it is stale.

With Go:

```bash
go test ./...
```

This runs the tests of the packages that need no generated code (`money`,
`filing` and `tools/stategen`). Everything that uses generated protobuf code,
the rules, the preparer, `cmd/`, `scenarios/` and `taxyear/` among them,
builds only with Bazel; each such directory has its own `go.mod` that keeps it
out of the root module.

## Using it from Bazel

```starlark
# MODULE.bazel
bazel_dep(name = "nexteggapp_taxcalcs", version = "0.1.0")
git_override(
    module_name = "nexteggapp_taxcalcs",
    remote = "https://github.com/nexteggapp/taxcalcs.git",
    commit = "<commit>",
)
```

`git_override` applies only in the root module, so a module that depends on
taxcalcs also needs the `git_override` entries for `nexteggapp_taxprotos` and
`protodiffgen` from this repository's `MODULE.bazel`.

## License

Apache 2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE).

This is software, not tax advice. It computes what the published rules say for
the figures you give it, and it comes with no warranty.
