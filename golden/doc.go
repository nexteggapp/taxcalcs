// Package golden holds the golden vectors that the Go and Rust
// implementations of taxcalcs must agree on: the taxlisp engine's parsed IR
// and typecheck results, each jurisdiction's estimated tax answers, and the
// computed tax years, each written from fixed inputs.
//
// The vectors are written by this package's test and read by the Rust tests
// under rust/. The test fails when a checked-in vector is not what the Go
// code computes today; to rewrite them, run
//
//	bazel run //golden:golden_test -- -update
//
// Each vector file is JSON. A protobuf message in one is its deterministic
// binary encoding, in base64, so the Rust side compares the decoded message
// rather than bytes that two encoders may order differently. The exception
// is the estimated tax households, testdata/estimated/inputs, whose
// ReturnInputs are protojson so they can be read and edited as data once the
// Go calculators that built them are gone.
package golden
