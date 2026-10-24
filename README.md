# taxcalcs

US federal and state tax calculators in Go, and `taxyear`, which computes a
household's whole tax year from its facts and a set of what-if scenarios.

Every rate and threshold cites the document it came from, and that document's
text is checked in beside the tests. Money is an integer count of cents, never
a float. When the law leaves a choice to the filer, such as whether to itemise,
the result returns the choice unanswered, with the rule that makes it a choice,
instead of guessing.

## Example

One salaried filer, gross pay to tax owed:

```go
salary := ustaxcalc.Dollars(120_000)

// What they could itemise. DeductionTaken makes the comparison, not you.
deduction, err := ustaxcalc.DeductionTaken(2026, ustaxcalc.Single, ustaxcalc.Conditions{}, ustaxcalc.Dollars(9_400))

taxable := salary - deduction.Value().Amount
tax, err := ustaxcalc.OrdinaryIncomeTax(2026, ustaxcalc.Single, taxable)
```

```
salary         $120000.00
deduction      $16100.00 (standard $16100.00, itemised $9400.00)
taxable        $103900.00
tax            $17570.00
marginal rate  22%, with $1800.00 of room before the next band

why:
  10% on $12400.00 = $1240.00
  12% on $38000.00 = $4560.00
  22% on $53500.00 = $11770.00

unanswered:
  deduction method: § 63(e) makes itemising an election rather than an
  outcome. A taxpayer may itemise a smaller total, and one filing separately
  must itemise if their spouse does
```

The code is [`ustaxcalc/example_test.go`](ustaxcalc/example_test.go), and the
test suite checks its output. `OrdinaryIncomeTax` takes taxable ordinary income:
income after the deduction, with long-term capital gains removed, because
`LongTermGainsTax` taxes those at their own rates. `tax.Value().Bands` is the
`why` block and `deduction.Required()` is the `unanswered` block.

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
| [`ustaxcalc`](ustaxcalc) | Federal: ordinary income tax, long-term gains, self-employment tax, standard and itemised deductions, IRA limits, safe harbour and estimated payments. Tax years 2025 and 2026. |
| [`californiataxcalc`](californiataxcalc/README.md) | California income tax for 2025, and estimated tax for 2026 |
| [`oregontaxcalc`](oregontaxcalc/README.md) | Oregon income tax for 2025, and estimated tax for 2026 |
| [`washingtontaxcalc`](washingtontaxcalc/README.md) | Washington's capital gains excise tax for 2025 (the state has no income tax), and its estimated payments for 2026 |
| `<state>taxcalc` | Estimated tax and the underpayment penalty for 2026, one package for each state and the District of Columbia |
| [`estimated`](estimated) | The interface every jurisdiction's estimated-tax calculator implements; [`estimated/all`](estimated/all) finds any of them by jurisdiction |
| [`money`](money), [`filing`](filing), [`working`](working) | The money type, filing status, and the result type that carries its working, shared by every calculator |
| [`us/v1`](us/v1), [`ustaxcalcsvc`](ustaxcalcsvc) | A protobuf contract for the federal calculators, and a gRPC server for it |
| [`scenarios/v1`](scenarios/v1) | `TaxYearScenarios`, the input to `taxyear`, and `TaxYearResult`, its output |
| [`taxyear`](taxyear) | Computes a tax year from a `TaxYearScenarios` file |
| [`cmd/taxyear`](cmd/taxyear) | The command-line tool for `taxyear` |

The calculators import only the standard library. The state packages do not
import each other or `ustaxcalc`: they take federal figures, such as adjusted
gross income, as plain amounts. `TableFor` returns `ErrNoTable` for a year the
build does not carry, and `Years()` lists the years it does.

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
- Each package's `testdata/SOURCES.md`: where each checked-in source document
  came from and how it was extracted

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

This runs the calculators' tests. The packages under `cmd/`, `scenarios/`,
`taxyear/` and `ustaxcalcsvc/` use generated protobuf code, so they build only
with Bazel; each of those directories has its own `go.mod` that keeps it out of
the root module.

To use the calculators from another Go module:

```bash
go get github.com/nexteggapp/taxcalcs@main
```

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
