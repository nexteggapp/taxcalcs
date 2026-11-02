//go:build !js

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "taxyearwasm runs in a browser: build //cmd/taxyearwasm:taxyearwasm_wasm")
	os.Exit(2)
}
