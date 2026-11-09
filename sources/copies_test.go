package sources

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The vendored files, relative to the test's working directory.
const pdfDir = "pdfs"

// Every vendored copy is the file its source names, byte for byte, and every
// file under pdfs/ is a copy some source names.
func TestVendoredCopies(t *testing.T) {
	catalog, err := Catalog()
	if err != nil {
		t.Fatal(err)
	}
	named := map[string]bool{}
	for _, src := range catalog {
		for _, c := range src.GetCopies() {
			if c.GetPath() == "" {
				continue
			}
			named[c.GetPath()] = true
			raw, err := os.ReadFile(filepath.Join(pdfDir, c.GetPath()))
			if err != nil {
				t.Errorf("%s: %v", src.GetId(), err)
				continue
			}
			sum := sha256.Sum256(raw)
			if got := hex.EncodeToString(sum[:]); got != c.GetSha256() {
				t.Errorf("%s: %s has sha256 %s, want %s", src.GetId(), c.GetPath(), got, c.GetSha256())
			}
		}
	}
	err = filepath.WalkDir(pdfDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".pdf") {
			return err
		}
		rel, err := filepath.Rel(pdfDir, path)
		if err != nil {
			return err
		}
		if !named[filepath.ToSlash(rel)] {
			t.Errorf("%s is vendored, but no source names it", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
