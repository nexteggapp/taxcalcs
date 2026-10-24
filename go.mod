module github.com/nexteggapp/taxcalcs

go 1.25.0

// The calculators (ustaxcalc, estimated and the state packages) import only
// the standard library. These requirements are for the packages that use
// generated protobuf code, which build only with Bazel: cmd/, scenarios/,
// taxyear/ and ustaxcalcsvc/. Each of those directories has a go.mod of its
// own that keeps it out of this module's `go test ./...`. MODULE.bazel reads
// this file for their Go dependencies, so `go mod tidy` would remove entries
// that Bazel needs.
require (
	github.com/bufbuild/protocompile v0.14.1
	github.com/nexteggapp/protodiffgen v0.0.0-20261024151729-b2e70a47b74c
	google.golang.org/grpc v1.83.0
	google.golang.org/protobuf v1.36.12
)

require (
	github.com/google/go-cmp v0.7.0 // indirect
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260803160001-6ac0973c030d // indirect
)
