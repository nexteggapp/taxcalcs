# Estimated tax vectors

These files hold the estimated tax answers the taxlisp rules are tested
against. Most of them were written by the Go estimated tax calculators
(`estimated`, `estimated/all` and the `<state>taxcalc` packages) before
estimated tax moved into taxlisp. Those calculators are being removed, so the
files they wrote are now **frozen**: nothing regenerates them, and
`bazel run //golden:golden_test -- -update` leaves them alone. Change one only
by hand, with the reason in the commit, when the law or a fixed mistake says
the old answer was wrong.

| File | What it is | How it was made |
| --- | --- | --- |
| `<code>.json.gz` | Every answer (requirement, schedule, penalty, explanation) for each household of one jurisdiction, for 2025 and 2026. `ZZ` is the reference rules. | Written by the Go calculators from the households of `estimated/estimatedtest`; frozen. |
| `inputs/<code>.json.gz` | The same households as `ReturnInputs` (protojson), keyed as the answers are. Only `inputs` is read; `state_extras` and `unmapped` record what the Go facts held that the protos didn't. | Written from the Go households; frozen. |
| `households.json.gz` | The Go facts of the federal, California and Washington households, field by field, for the Rust `estimatedtest` crate. | Written from the Go households; frozen. |
| `ca_worksheet_i.json` | A California household that annualises with Worksheet I, which no household above does. Its expected answers are in `golden/estimated_worksheet_ca_test.go`. | Written from the Go calculator's Worksheet I test; frozen. |
| `reference.tlisp` | The reference rules, ZZ, in taxlisp: a small jurisdiction that exercises the shared machinery and is not any jurisdiction's law. | Hand-written. |
| `taxlisp.json` | Every taxlisp answer, for the Rust engine's test (`rust/returns/tests/estimated_golden.rs`). | Written by `golden_test -update` from the taxlisp rules; regenerated whenever the rules change. |

`golden/estimated_answer_test.go` prepares every household in `inputs/` with
the taxlisp rules and compares the answers with `<code>.json.gz`, leaving out
only what `EstimatedAnswer` has no place for (its doc comment lists it). The
worksheet tests (`golden/estimated_worksheet_*_test.go`) check that each
answer's form agrees with the answer's own figures.
