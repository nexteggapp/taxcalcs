// Package golden holds the golden vectors that the Go and Rust
// implementations of taxcalcs must agree on: the taxlisp engine's parsed IR
// and typecheck results, the prepared returns, the estimated tax answers,
// protodiffgen's diffs of the taxyear test cases' scenarios, and what
// taxyear's models and payroll make of those cases, each from fixed inputs.
//
// Most vectors are written by this package's test and read by the Rust tests
// under rust/. The test fails when a checked-in vector is not what the Go
// code computes today; to rewrite them, run
//
//	bazel run //golden:golden_test -- -update
//
// Each vector file is JSON. A protobuf message in one is its deterministic
// binary encoding, in base64, so the Rust side compares the decoded message
// rather than bytes that two encoders may order differently.
//
// The estimated tax vectors under testdata/estimated are the exception: the
// answers there, and the households they answer (whose ReturnInputs are
// protojson, to be read and edited as data), were written by the Go
// estimated tax calculators before those were removed, and are now frozen.
// The test holds the taxlisp rules to them and rewrites only
// testdata/estimated/taxlisp.json. See testdata/estimated/README.md.
package golden
