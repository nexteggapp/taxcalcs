// The packages under cmd/ use generated protobuf code, so they build only
// with Bazel. This file keeps them out of the root module's go test ./...
module github.com/nexteggapp/taxcalcs/cmd

go 1.25.0
