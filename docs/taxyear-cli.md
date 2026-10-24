# The taxyear command

`taxyear` computes a tax year from a [`TaxYearScenarios` file](scenarios.md)
and prints the result. It reads one JSONC file, computes every scenario as of
the file's `as_of`, and prints the [`TaxYearResult`](results.md) as JSON, a
short text summary, or one figure per scenario. The source is
[`cmd/taxyear`](../cmd/taxyear/main.go).

## Usage

```
taxyear FILE.jsonc            the whole TaxYearResult, as JSON
taxyear --summary FILE.jsonc  a short text summary of each scenario
taxyear --path P FILE.jsonc   one figure of each scenario, by field path
```

| Flag | Meaning |
|---|---|
| `--summary` | Print a short text summary instead of JSON |
| `--path P` | Print the figure at field path `P` in each scenario's result. It takes precedence over `--summary`. |

The command builds with Bazel, because it uses generated protobuf code. With
`bazel run`, pass an absolute path, since Bazel runs the binary in another
directory:

```sh
bazel run //cmd/taxyear -- --summary "$PWD/scenarios/v1/testdata/example.jsonc"
```

## Input

The file is the protobuf JSON mapping of `TaxYearScenarios`, with `//` and
`/* */` comments allowed. It is parsed strictly: an unknown field, a `oneof`
with no case set, or an enum written as its `UNSPECIFIED` value is refused.
[scenarios.md](scenarios.md#the-jsonc-format) gives the format, and
[`scenarios/v1/testdata/example.jsonc`](../scenarios/v1/testdata/example.jsonc)
is a complete example.

## Output

**JSON**, by default: the whole `TaxYearResult`, indented, with protobuf JSON
field names. [results.md](results.md) describes every field.

**`--summary`** prints the year's status, then for each scenario:

- each problem, with its path;
- each job's W-2 wages and federal withholding, so far plus the rest of the
  year, and its item count;
- the federal return's AGI, taxable income, tax, total tax, payments, amount
  owed and overpayment;
- each jurisdiction's estimated-payment outcome, requirement, target and
  penalty;
- each open question, with its id.

**`--path P`** prints one tab-separated line per scenario that has a figure at
`P`: the scenario id, the path, and the value. Money prints as dollars and
cents. A scenario with no figure at `P` is skipped.

```sh
$ bazel run //cmd/taxyear -- --path projection.employers[acme].year.boxes.wages "$PWD/year.jsonc"
plan	projection.employers[acme].year.boxes.wages	$120000.00
```

[Field paths](scenarios.md#field-paths) use proto field names, not the
camelCase JSON names: `federal.form_1040.total_tax`, not
`federal.form1040.totalTax`.

## Exit codes

| Code | When |
|---|---|
| 0 | Every scenario computed with no problems |
| 1 | The parse refuses the file, and the reasons go to standard error. Or the file or a scenario has problems; the output is still printed, problems included. |
| 2 | Bad flags, not exactly one file, a file that can't be read, or no scenario with a figure at `--path` |

## Checking an edit

To check a change to a scenario before using it, edit the file and run:

```sh
bazel run //cmd/taxyear -- --summary "$PWD/year.jsonc"
```

Exit code 0 means every scenario resolved and computed. Any problem is printed
with its path in the file, such as `scenarios[raise]` or
`model.employers[acme].default_part`.
