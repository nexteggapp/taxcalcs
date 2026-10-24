package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// file is a small household: one job paying $5,000 twice a month, from a
// default part, as of Sep 8, 2026.
const file = `// A comment, which the parse removes.
{
  "schemaVersion": 1, "taxYear": 2026, "asOf": { "year": 2026, "month": 9, "day": 8 },
  "shared": { "model": { "employers": [
    { "id": "acme", "name": "Acme Corp", "person": "you", "payFrequency": "PAY_FREQUENCY_SEMI_MONTHLY",
      "firstPayDate": { "year": 2026, "month": 1, "day": 15 }, "work": { "state": "TX" },
      "supplemental": "SUPPLEMENTAL_METHOD_FLAT",
      "defaultPart": { "id": "acme-d", "eachPaycheck": { "gross": { "minor": "500000", "currency": "USD" } } } } ] } },
  "scenarios": [ { "id": "plan", "name": "Your plan" } ]
}`

func write(t *testing.T, text string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "year.jsonc")
	if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRun(t *testing.T) {
	path := write(t, file)
	for _, c := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{path}, 0, `"scenario": "plan"`},
		{[]string{"--summary", path}, 0, "wages $0.00 so far + $120000.00 rest"},
		{[]string{"--path", "projection.employers[acme].year.boxes.wages", path}, 0, "plan projection.employers[acme].year.boxes.wages $120000.00"},
		{[]string{"--nope", path}, 2, ""},
		{[]string{}, 2, ""},
		{[]string{write(t, strings.Replace(file, `"schemaVersion": 1`, `"schemaVersion": 9`, 1))}, 1, "schema"},
	} {
		var out, errs bytes.Buffer
		if code := run(c.args, &out, &errs); code != c.code {
			t.Errorf("%v: exit %d, want %d; stderr %s", c.args, code, c.code, errs.String())
		}
		// protojson varies its spacing from build to build, so spaces
		// are compared as one.
		if !strings.Contains(strings.Join(strings.Fields(out.String()), " "), c.want) {
			t.Errorf("%v: output lacks %q:\n%s", c.args, c.want, out.String())
		}
	}
}
