// Package sources loads the catalog of published texts the rules rely on, and
// checks that it is consistent with itself.
//
// Each source is a textproto under catalog/, one file per source, grouped by
// jurisdiction and tax year. The vendored copies of their documents are under
// pdfs/.
package sources

import (
	"embed"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"

	sourcesv1 "github.com/nexteggapp/taxcalcs/sources/v1"
	"google.golang.org/protobuf/encoding/prototext"
)

//go:embed catalog
var catalogFS embed.FS

// Catalog returns every source, in id order.
func Catalog() ([]*sourcesv1.Source, error) {
	var out []*sourcesv1.Source
	err := fs.WalkDir(catalogFS, "catalog", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".textproto") {
			return err
		}
		raw, err := catalogFS.ReadFile(path)
		if err != nil {
			return err
		}
		src := &sourcesv1.Source{}
		if err := prototext.Unmarshal(raw, src); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		out = append(out, src)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("reading the source catalog: %w", err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].GetId() < out[j].GetId() })
	return out, nil
}

var (
	idPattern     = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
	datePattern   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

// Check reports every problem in the catalog that can be found without the
// rules: malformed or repeated ids, missing fields, links to sources that do
// not exist, and copies vendored without leave to reuse them.
func Check(catalog []*sourcesv1.Source) []error {
	var errs []error
	fail := func(src *sourcesv1.Source, format string, args ...any) {
		errs = append(errs, fmt.Errorf("%s: %s", src.GetId(), fmt.Sprintf(format, args...)))
	}
	ids := map[string]bool{}
	for _, src := range catalog {
		if !idPattern.MatchString(src.GetId()) {
			fail(src, "id is not lower-case words joined by hyphens")
		}
		if ids[src.GetId()] {
			fail(src, "id is used twice")
		}
		ids[src.GetId()] = true
	}
	for _, src := range catalog {
		if src.GetKind() == sourcesv1.Kind_KIND_UNSPECIFIED {
			fail(src, "kind is not set")
		}
		if src.GetTitle() == "" || src.GetDesignation() == "" {
			fail(src, "title and designation are required")
		}
		if src.GetPublisher().GetJurisdiction() == "" || src.GetPublisher().GetAgency() == "" {
			fail(src, "publisher needs a jurisdiction and an agency")
		}
		if len(src.GetTaxYears()) == 0 {
			fail(src, "no tax years")
		}
		for _, s := range src.GetSupersedes() {
			if !ids[s] {
				fail(src, "supersedes %q, which is not in the catalog", s)
			}
		}
		for _, c := range src.GetCopies() {
			if !sha256Pattern.MatchString(c.GetSha256()) {
				fail(src, "copy %s has no valid sha256", c.GetUrl())
			}
			if !datePattern.MatchString(c.GetRetrieved()) {
				fail(src, "copy %s has no retrieval date", c.GetUrl())
			}
			if c.GetPath() != "" && !Vendorable(src) {
				fail(src, "copy %s is vendored, but the publisher's reuse terms do not allow it", c.GetUrl())
			}
		}
		anchors := map[string]bool{}
		for _, a := range src.GetAnchors() {
			if !idPattern.MatchString(a.GetId()) {
				fail(src, "anchor %q is not lower-case words joined by hyphens", a.GetId())
			}
			if anchors[a.GetId()] {
				fail(src, "anchor %q is declared twice", a.GetId())
			}
			anchors[a.GetId()] = true
			if a.GetTitle() == "" {
				fail(src, "anchor %q has no title", a.GetId())
			}
			if a.GetCoverage() == nil {
				fail(src, "anchor %q says neither what implements it nor why it is left out", a.GetId())
			}
			if r := a.GetAt().GetRegion(); r != nil && (r.GetX() < 0 || r.GetY() < 0 || r.GetX()+r.GetWidth() > 1.0001 || r.GetY()+r.GetHeight() > 1.0001) {
				fail(src, "anchor %q has a region outside the page", a.GetId())
			}
		}
	}
	return errs
}

// Vendorable reports whether the publisher's terms let us keep a copy.
func Vendorable(src *sourcesv1.Source) bool {
	switch src.GetPublisher().GetReuse() {
	case sourcesv1.Reuse_REUSE_PUBLIC_DOMAIN, sourcesv1.Reuse_REUSE_PUBLIC_RECORD:
		return true
	}
	return false
}

// Anchor returns the anchor of src with the given id.
func Anchor(src *sourcesv1.Source, id string) (*sourcesv1.Anchor, bool) {
	for _, a := range src.GetAnchors() {
		if a.GetId() == id {
			return a, true
		}
	}
	return nil, false
}
