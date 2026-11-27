package cases

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	casesv1 "github.com/nexteggapp/taxcalcs/cases/v1"
	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	"google.golang.org/protobuf/encoding/prototext"
)

//go:embed */*/*.textproto
var caseFS embed.FS

// Load reads every case file, cases/<jurisdiction>/<year>/<id>.textproto,
// in path order.
func Load() ([]Loaded, error) {
	var paths []string
	err := fs.WalkDir(caseFS, ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".textproto") {
			paths = append(paths, p)
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	var out []Loaded
	for _, p := range paths {
		raw, err := caseFS.ReadFile(p)
		if err != nil {
			return nil, err
		}
		c := &casesv1.TestCase{}
		if err := prototext.Unmarshal(raw, c); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		out = append(out, Loaded{Jurisdiction: strings.Split(p, "/")[0], Path: path.Clean(p), Case: c})
	}
	return out, nil
}

// Run checks every loaded case, filling in its Results.
func Run(eng *federal.Engine, forms map[string]*Form, loaded []Loaded) error {
	for i := range loaded {
		r, err := Check(eng, forms, loaded[i].Case)
		if err != nil {
			return fmt.Errorf("%s: %w", loaded[i].Path, err)
		}
		loaded[i].Results = r
	}
	return nil
}
