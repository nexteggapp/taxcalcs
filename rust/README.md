# taxcalcs in Rust

A Rust port of the Go module at the repository root, with the same packages
and the same answers. The Go code is unchanged and remains the reference: the
two are held together by golden vectors that a Go test writes and the Rust
tests read, so a figure the Rust code computes matches the Go code's to the
cent.

```
cargo test --workspace              # everything, including the golden vectors
cargo clippy --workspace --all-targets -- -D warnings
cargo fmt --all --check
cargo build -p taxcalcs-taxyearwasm --target wasm32-unknown-unknown --release
```

Bazel builds and tests the crates that have code with rules_rust, from the
same Cargo.lock and toolchain; `bazel test //...` runs their tests, golden
vectors included, beside the Go ones:

```
bazel test //rust/...
```

A crate's BUILD.bazel lists its dependencies again, because Bazel does not
read Cargo.toml for them. Clippy and rustfmt run under Cargo only.

## Layout

One crate per Go package, at the same path under `rust/`, except that the
state calculators sit under `states/`, and the taxlisp engine and taxyear are
each one crate with the Go packages as modules.

| Crate | Go package |
| --- | --- |
| `proto` | the generated protobuf code for every schema (see below) |
| `money`, `filing`, `working` | `money`, `filing`, `working` |
| `estimated`, `estimated/all`, `estimated/estimatedtest` | the same |
| `ustaxcalc`, `states/<state>taxcalc` | `ustaxcalc` and each `<state>taxcalc` |
| `taxlisp` | `taxlisp/parser`, `taxlisp/types`, `taxlisp/eval` |
| `returns` | `returns/rules/federal`, `returns/preparer` |
| `protodiffgen` | `github.com/nexteggapp/protodiffgen/apply` |
| `taxyear` | `taxyear` and the packages under it |
| `taxyearwasm` | `cmd/taxyearwasm` |
| `golden` | reads `../golden/testdata` for tests |

Each crate is named `taxcalcs-<name>` and its siblings depend on it under the
Go package name, so `money::USCents` reads as it does in Go.

## Protobuf

`proto/descriptors.binpb` is a serialized FileDescriptorSet of every schema
the crates use and the files they import, which Bazel writes:

```
bazel run //rust/proto:write_descriptors
```

`bazel test //rust/proto:descriptors_test` fails when it is stale. The proto
crate's build script generates buffa code from it, so Cargo needs no protoc,
and the taxlisp engine reads the same descriptors at run time to reflect over
messages. The modules are re-exported under Go's import names: `taxlispv1`,
`returnsv1`, `scenariosv1`, `usv1`, `commonv1`, `factsv1`, `federalv1`,
`configv1`, `cav1`, `orv1`.

## Golden vectors

`golden/` at the repository root is a Go test that writes, from fixed inputs,
what the Go code computes: the parsed taxlisp IR and typecheck results, every
jurisdiction's estimated tax answers, and the computed tax years. The Rust
tests read the same files and must reproduce them exactly.

```
bazel run //golden:golden_test -- -update   # rewrite the vectors
bazel test //golden:golden_test             # fails when one is stale
```

## Porting rules

The port is literal. A reader with the Go file open beside the Rust one
should find the same functions in the same order, under snake_case names, and
the same doc comments, adapted.

- **Same answers, same words.** Every figure, every sentence a person reads
  and every error message is the Go text, byte for byte, because the vectors
  compare them.
- **Go's arithmetic.** Go's `int` is 64 bits, so it is `i64`. Integer
  division truncates toward zero in both. `math/big.Rat` is
  `num_rational::BigRational`. Where Go formats a float, match `strconv`'s
  output exactly, including its exponent form.
- **Go's ordering.** Go never relies on map order for output; where it sorts,
  sort the same way, and where a slice keeps insertion order, keep it.
- **Errors are typed.** A library returns `Result` with an error type whose
  `Display` is the Go error's text. No `unwrap` or `expect` outside tests, no
  `unsafe`, no panic on a path Go does not panic on.
- **Docs.** `missing_docs` is denied, and clippy runs with `-D warnings`.
