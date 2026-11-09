// Package cites finds the citations in the taxlisp rules and checks each one
// against the source catalog.
//
// A rule's :cite list holds two kinds of citation:
//
//	(law "26 U.S.C." "24" "(h)(4)")   a section of a statute or regulation
//	(doc "us-irs-1040-instructions-2025" "line-12e")   a document, or a part of one
//
// A law cite names the statute by its designation, so the same rule can cite
// each year's edition; the section must be one the statute's catalog entry
// lists, with its heading. A doc cite names a catalog source by id, and an
// anchor the source declares.
package cites

import (
	_ "embed"
	"fmt"
	"sort"
	"strings"

	sourcesv1 "github.com/nexteggapp/taxcalcs/sources/v1"
	"github.com/nexteggapp/taxcalcs/taxlisp/parser"
	taxlispv1 "github.com/nexteggapp/taxcalcs/taxlisp/v1"
)

// Cite is one citation in a rule.
type Cite struct {
	// File and Line say where the :cite is.
	File string
	Line int

	// Law, Section and Sub are a law cite's designation, section and
	// subsection.
	Law, Section, Sub string

	// Source and Anchor are a doc cite's source id and, if it names one, the
	// anchor.
	Source, Anchor string

	// Text is a citation written as a plain string, which Check rejects.
	Text string
}

func (c Cite) String() string {
	switch {
	case c.Law != "":
		return fmt.Sprintf("(law %q %q %q)", c.Law, c.Section, c.Sub)
	case c.Source != "":
		return fmt.Sprintf("(doc %q %q)", c.Source, c.Anchor)
	}
	return fmt.Sprintf("%q", c.Text)
}

// Find returns the citations in a rule file.
func Find(path, src string) ([]Cite, error) {
	mf, err := parser.ParseFile(path, src)
	if err != nil {
		return nil, err
	}
	var out []Cite
	var walk func(e *taxlispv1.Expression) error
	walk = func(e *taxlispv1.Expression) error {
		elems := e.GetList().GetExpressions()
		for i, x := range elems {
			if x.GetAtom().GetKeyword() == "cite" && i+1 < len(elems) {
				line := int(e.GetList().GetSyntax().GetStartLine())
				for _, c := range elems[i+1].GetList().GetExpressions() {
					cite, err := read(c)
					if err != nil {
						return fmt.Errorf("%s:%d: %w", path, line, err)
					}
					cite.File, cite.Line = path, line
					out = append(out, cite)
				}
			}
			if err := walk(x); err != nil {
				return err
			}
		}
		return nil
	}
	for _, e := range mf.GetExpressions() {
		if err := walk(e); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Read reads one element of a :cite list.
func Read(e *taxlispv1.Expression) (Cite, error) { return read(e) }

func read(e *taxlispv1.Expression) (Cite, error) {
	if s, ok := e.GetAtom().GetPrimitive().(*taxlispv1.Atom_StringValue); ok {
		return Cite{Text: s.StringValue}, nil
	}
	elems := e.GetList().GetExpressions()
	if len(elems) == 0 {
		return Cite{}, fmt.Errorf("a citation is (law ...), (doc ...) or a string")
	}
	var args []string
	for _, a := range elems[1:] {
		s, ok := a.GetAtom().GetPrimitive().(*taxlispv1.Atom_StringValue)
		if !ok {
			return Cite{}, fmt.Errorf("a citation's parts are strings: %s", parser.FormatExpr(e))
		}
		args = append(args, s.StringValue)
	}
	switch head := elems[0].GetAtom().GetSymbol().GetName(); {
	case head == "law" && (len(args) == 2 || len(args) == 3):
		c := Cite{Law: args[0], Section: args[1]}
		if len(args) == 3 {
			c.Sub = args[2]
		}
		return c, nil
	case head == "doc" && (len(args) == 1 || len(args) == 2):
		c := Cite{Source: args[0]}
		if len(args) == 2 {
			c.Anchor = args[1]
		}
		return c, nil
	}
	return Cite{}, fmt.Errorf("not a citation: %s", parser.FormatExpr(e))
}

// labels.tsv says how each citation reads in a return's working. It is
// generated from the catalog, which the Rust engine does not have, so the Go
// and Rust preparers read this same file; TestLabelsAreCurrent keeps it in
// step with the catalog.
//
//go:embed labels.tsv
var labelsTSV string

type label struct{ authority, url string }

var labels = func() map[string]label {
	m := map[string]label{}
	for _, line := range strings.Split(strings.TrimSpace(labelsTSV), "\n") {
		f := strings.Split(line, "\t")
		if len(f) == 5 {
			m[f[0]+"\t"+f[1]+"\t"+f[2]] = label{f[3], f[4]}
		}
	}
	return m
}()

// Text is how a citation reads in a return's working, and the address of its
// official text: "26 U.S.C. § 24(h)(4)", or "Form 4797 Instructions (2025),
// Line 8".
func Text(c Cite) (authority, url string) {
	switch {
	case c.Law != "":
		l := labels["law\t"+c.Law+"\t"]
		return c.Law + " § " + c.Section + c.Sub, strings.ReplaceAll(l.url, "{section}", c.Section)
	case c.Source != "":
		if l, ok := labels["doc\t"+c.Source+"\t"+c.Anchor]; ok {
			return l.authority, l.url
		}
		if c.Anchor != "" {
			return c.Source + ", " + c.Anchor, ""
		}
		return c.Source, ""
	}
	return c.Text, ""
}

// Labels returns the contents of labels.tsv for a catalog: for each statute
// or regulation, the address of a section; for each other source and each of
// its anchors, how a doc cite of it reads and its address. One line each:
// kind, key, anchor, authority and URL, separated by tabs.
func Labels(catalog []*sourcesv1.Source) string {
	var lines []string
	seen := map[string]bool{}
	for _, src := range catalog {
		if k := src.GetKind(); k == sourcesv1.Kind_KIND_STATUTE || k == sourcesv1.Kind_KIND_REGULATION {
			if !seen[src.GetDesignation()] {
				seen[src.GetDesignation()] = true
				lines = append(lines, strings.Join([]string{"law", src.GetDesignation(), "", "", src.GetAnchorScheme().GetUrlTemplate()}, "\t"))
			}
		}
		name := src.GetDesignation()
		switch src.GetKind() {
		case sourcesv1.Kind_KIND_FORM, sourcesv1.Kind_KIND_INSTRUCTIONS, sourcesv1.Kind_KIND_PUBLICATION:
			if len(src.GetTaxYears()) == 1 {
				name = fmt.Sprintf("%s (%d)", name, src.GetTaxYears()[0])
			}
		}
		var url string
		if len(src.GetCopies()) > 0 {
			url = src.GetCopies()[0].GetUrl()
		}
		lines = append(lines, strings.Join([]string{"doc", src.GetId(), "", name, url}, "\t"))
		for _, a := range src.GetAnchors() {
			lines = append(lines, strings.Join([]string{"doc", src.GetId(), a.GetId(), name + ", " + a.GetTitle(), url}, "\t"))
		}
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n") + "\n"
}

// Check returns an error for each citation the catalog does not support: a
// plain string, a law no statute or regulation in the catalog is designated
// by, a section that statute does not list, an unknown source, or an anchor
// the source does not declare.
func Check(cites []Cite, catalog []*sourcesv1.Source) []error {
	byID := map[string]*sourcesv1.Source{}
	laws := map[string]map[string]bool{}
	for _, src := range catalog {
		byID[src.GetId()] = src
		if k := src.GetKind(); k != sourcesv1.Kind_KIND_STATUTE && k != sourcesv1.Kind_KIND_REGULATION {
			continue
		}
		if laws[src.GetDesignation()] == nil {
			laws[src.GetDesignation()] = map[string]bool{}
		}
		for _, p := range src.GetAnchorScheme().GetCited() {
			laws[src.GetDesignation()][p.GetNumber()] = true
		}
	}
	var errs []error
	fail := func(c Cite, format string, args ...any) {
		errs = append(errs, fmt.Errorf("%s:%d: %s: %s", c.File, c.Line, c, fmt.Sprintf(format, args...)))
	}
	for _, c := range cites {
		switch {
		case c.Text != "":
			fail(c, "a citation written as a string; write (law ...) or (doc ...)")
		case c.Law != "":
			sections, ok := laws[c.Law]
			if !ok {
				fail(c, "no statute or regulation in the catalog is designated %q", c.Law)
			} else if !sections[c.Section] {
				fail(c, "§ %s is not among the sections the catalog lists for %s", c.Section, c.Law)
			}
		default:
			src, ok := byID[c.Source]
			if !ok {
				fail(c, "no source %q in the catalog", c.Source)
				continue
			}
			if c.Anchor == "" {
				continue
			}
			found := false
			for _, a := range src.GetAnchors() {
				found = found || a.GetId() == c.Anchor
			}
			if !found {
				fail(c, "%s declares no anchor %q", c.Source, c.Anchor)
			}
		}
	}
	return errs
}
