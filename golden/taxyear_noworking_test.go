//go:build wasmentry

package golden

// The taxyear vectors without the engine's working: Compute of every file
// TestTaxYear reads, through a cache made with taxyear.WithoutEngineWorking.
// That option is on the branch feat/wasm-entry, not yet merged, so this file
// builds only with the wasmentry tag, on that branch:
//
//	bazel run //golden:golden_test --define gotags=wasmentry -- -update -test.run TestTaxYearNoWorking

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nexteggapp/taxcalcs/taxyear"
)

// taxyearNoWorking is one file's Compute without the engine's working.
type taxyearNoWorking struct {
	File   string `json:"file"`
	Result string `json:"result"`
	Error  string `json:"error"`
}

func TestTaxYearNoWorking(t *testing.T) {
	out := []taxyearNoWorking{}
	for _, path := range taxyearFiles(t) {
		text, err := os.ReadFile(filepath.Join("..", path))
		if err != nil {
			t.Fatal(err)
		}
		file, err := taxyear.Parse(text)
		if err != nil {
			continue
		}
		c, err := taxyear.NewCache(len(file.GetScenarios())+1, taxyear.WithoutEngineWorking())
		if err != nil {
			t.Fatal(err)
		}
		v := taxyearNoWorking{File: path}
		r, err := c.Compute(file)
		v.Error = errText(err)
		if err == nil {
			v.Result = encode(t, r.Message())
		}
		out = append(out, v)
	}
	checkGzip(t, "taxyear/no_working.json.gz", out)
}
