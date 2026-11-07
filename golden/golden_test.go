package golden

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"google.golang.org/protobuf/proto"
)

var update = flag.Bool("update", false, "rewrite the vectors in testdata instead of checking them")

// check writes v as testdata/name in JSON when -update is set, and otherwise
// fails unless the checked-in file holds exactly that JSON.
func check(t *testing.T, name string, v any) {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	b = append(b, '\n')
	if *update {
		dir := "testdata"
		// Under `bazel run`, write into the source tree rather than the
		// runfiles.
		if ws := os.Getenv("BUILD_WORKSPACE_DIRECTORY"); ws != "" {
			dir = filepath.Join(ws, "golden", "testdata")
		}
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("%v; run `bazel run //golden:golden_test -- -update`", err)
	}
	if !bytes.Equal(got, b) {
		t.Errorf("testdata/%s is stale; run `bazel run //golden:golden_test -- -update`", name)
	}
}

// encode is m's deterministic binary encoding in base64, or "" for nil.
func encode(t *testing.T, m proto.Message) string {
	t.Helper()
	if m == nil {
		return ""
	}
	b, err := proto.MarshalOptions{Deterministic: true}.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(b)
}

// errText is err's message, or "" for nil.
func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
