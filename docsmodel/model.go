// Command docsmodel writes the documentation site's model: the source
// catalog, the rules and figures read from the taxlisp files, the links
// between them, and the derivations of a few sample households.
//
//	bazel run //docsmodel -- -out site/public/model.json
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/nexteggapp/taxcalcs/sources"
	sourcesv1 "github.com/nexteggapp/taxcalcs/sources/v1"
	taxlispv1 "github.com/nexteggapp/taxcalcs/taxlisp/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

// Model is everything the site shows.
type Model struct {
	Jurisdiction string              `json:"jurisdiction"`
	Year         int                 `json:"year"`
	Sources      []json.RawMessage   `json:"sources"`
	Rules        []*Rule             `json:"rules"`
	FigureSets   []*FigureSet        `json:"figureSets"`
	Households   []*Household        `json:"households"`
	Examples     map[string]*Example `json:"examples"`
	// TypeIndex names every type; Types, written to their own file, are
	// the type pages.
	TypeIndex []*TypeEntry `json:"typeIndex"`
	Types     []*Type      `json:"-"`
	Problems  []string     `json:"problems"`
}

// Rule is a top-level declaration: a def, a form, a worksheet or a struct.
type Rule struct {
	Name    string   `json:"name"`
	Kind    string   `json:"kind"`
	File    string   `json:"file"`
	Line    int      `json:"line"`
	Doc     string   `json:"doc"`
	Params  []Param  `json:"params,omitempty"`
	Returns string   `json:"returns,omitempty"`
	Cites   []Cite   `json:"cites,omitempty"`
	Body    *Expr    `json:"body,omitempty"`
	Parts   []*Part  `json:"parts,omitempty"`
	Calls   []string `json:"calls,omitempty"`
	Figures []string `json:"figures,omitempty"`
	Source  string   `json:"source"`

	// Filled in from the catalog: the anchors this rule implements.
	Anchors []AnchorRef `json:"anchors,omitempty"`

	// The declaration, for finding the protobuf fields the rule reads.
	decl *taxlispv1.Expression
}

// Param is a parameter and its type.
type Param struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Doc  string `json:"doc,omitempty"`
}

// Cite is one citation: a section of law, or free text.
type Cite struct {
	Code    string `json:"code,omitempty"`
	Section string `json:"section,omitempty"`
	Sub     string `json:"sub,omitempty"`
	Text    string `json:"text,omitempty"`
	Source  string `json:"source,omitempty"`
	URL     string `json:"url,omitempty"`
}

// Part is a captioned group of a form's lines, or a worksheet's lines.
type Part struct {
	Caption string  `json:"caption"`
	Items   []*Item `json:"items"`
}

// Item is a line, or a value a form binds or sets.
type Item struct {
	Kind    string      `json:"kind"`
	Number  string      `json:"number,omitempty"`
	Name    string      `json:"name"`
	Caption string      `json:"caption,omitempty"`
	Field   string      `json:"field,omitempty"`
	Expr    *Expr       `json:"expr"`
	Anchors []AnchorRef `json:"anchors,omitempty"`
}

// AnchorRef names an anchor of a source.
type AnchorRef struct {
	Source string `json:"source"`
	Anchor string `json:"anchor"`
}

// FigureSet is a deffigures or deffigure: a year's published figures.
type FigureSet struct {
	Name    string    `json:"name"`
	Type    string    `json:"type"`
	Year    int       `json:"year"`
	Doc     string    `json:"doc"`
	File    string    `json:"file"`
	Line    int       `json:"line"`
	Figures []*Figure `json:"figures"`
	// Source is the declaration as written.
	Source string `json:"source"`
}

// Figure is one published value.
type Figure struct {
	Field  string     `json:"field"`
	Doc    string     `json:"doc,omitempty"`
	Value  string     `json:"value"`
	Rows   [][]string `json:"rows,omitempty"`
	Source string     `json:"source"`
	Anchor string     `json:"anchor"`
}

func main() {
	out := flag.String("out", "", "where to write the model; standard output if empty")
	flag.StringVar(&descriptorSet, "descriptors", "", "a FileDescriptorSet, with comments, of the return contract")
	typesOut := flag.String("types", "", "where to write the type pages' model")
	flag.Parse()
	m, err := Build()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	b, err := json.MarshalIndent(m, "", " ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *out == "" {
		os.Stdout.Write(b)
		return
	}
	if wd := os.Getenv("BUILD_WORKING_DIRECTORY"); wd != "" && !strings.HasPrefix(*out, "/") {
		*out = wd + "/" + *out
	}
	if err := os.WriteFile(*out, b, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *typesOut != "" {
		tb, err := json.Marshal(m.Types)
		if err == nil {
			err = os.WriteFile(*typesOut, tb, 0o644)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	for _, p := range m.Problems {
		fmt.Fprintln(os.Stderr, "problem:", p)
	}
}

// Build reads the catalog and the California rules, links them, and runs
// the sample households.
func Build() (*Model, error) {
	catalog, err := sources.Catalog()
	if err != nil {
		return nil, err
	}
	m := &Model{Jurisdiction: "CA", Year: 2025, Problems: []string{}}
	for _, src := range catalog {
		b, err := protojson.MarshalOptions{UseProtoNames: false}.Marshal(src)
		if err != nil {
			return nil, err
		}
		m.Sources = append(m.Sources, b)
	}
	if err := readRules(m); err != nil {
		return nil, err
	}
	link(m, catalog)
	if descriptorSet != "" {
		if err := readTypes(m, descriptorSet); err != nil {
			return nil, err
		}
	}
	if err := runHouseholds(m); err != nil {
		return nil, err
	}
	sort.Strings(m.Problems)
	return m, nil
}

// link resolves citations against the catalog, attaches each anchor to the
// rule or line that implements it, and records what does not resolve.
func link(m *Model, catalog []*sourcesv1.Source) {
	rules := map[string]*Rule{}
	for _, r := range m.Rules {
		rules[r.Name] = r
	}
	byID := map[string]*sourcesv1.Source{}
	for _, src := range catalog {
		byID[src.GetId()] = src
	}
	for _, r := range m.Rules {
		for i, c := range r.Cites {
			if c.Code == "" {
				continue
			}
			found := false
			for _, src := range catalog {
				if src.GetKind() == sourcesv1.Kind_KIND_STATUTE && src.GetDesignation() == c.Code && hasYear(src, m.Year) {
					r.Cites[i].Source = src.GetId()
					r.Cites[i].URL = strings.ReplaceAll(src.GetAnchorScheme().GetUrlTemplate(), "{section}", c.Section)
					found = true
				}
			}
			if !found {
				m.Problems = append(m.Problems, fmt.Sprintf("%s cites %s § %s, which no statute source in force for %d covers", r.Name, c.Code, c.Section, m.Year))
			}
		}
	}
	for _, src := range catalog {
		for _, a := range src.GetAnchors() {
			impl := a.GetImplementedBy()
			if impl == "" {
				continue
			}
			ref := AnchorRef{Source: src.GetId(), Anchor: a.GetId()}
			name, line, isLine := strings.Cut(impl, ":")
			r, ok := rules[name]
			if !ok {
				m.Problems = append(m.Problems, fmt.Sprintf("%s#%s is implemented by %s, which is not a rule", src.GetId(), a.GetId(), name))
				continue
			}
			if !isLine {
				r.Anchors = append(r.Anchors, ref)
				continue
			}
			it := findLine(r, line)
			if it == nil {
				m.Problems = append(m.Problems, fmt.Sprintf("%s#%s is implemented by line %s of %s, which has no such line", src.GetId(), a.GetId(), line, name))
				continue
			}
			it.Anchors = append(it.Anchors, ref)
		}
	}
	for _, fs := range m.FigureSets {
		for _, f := range fs.Figures {
			src, ok := byID[f.Source]
			if !ok {
				m.Problems = append(m.Problems, fmt.Sprintf("%s.%s cites %s, which is not in the catalog", fs.Name, f.Field, f.Source))
				continue
			}
			if !hasYear(src, fs.Year) {
				m.Problems = append(m.Problems, fmt.Sprintf("%s.%s cites %s, which is not for %d", fs.Name, f.Field, f.Source, fs.Year))
			}
			if _, ok := sources.Anchor(src, f.Anchor); !ok {
				m.Problems = append(m.Problems, fmt.Sprintf("%s.%s cites %s#%s, which the source does not declare", fs.Name, f.Field, f.Source, f.Anchor))
			}
		}
	}
	// A form's line the form's own source does not list.
	for _, r := range m.Rules {
		if r.Kind != "form" {
			continue
		}
		for _, p := range r.Parts {
			for _, it := range p.Items {
				if it.Kind == "line" && len(it.Anchors) == 0 {
					m.Problems = append(m.Problems, fmt.Sprintf("line %s of %s is in no source", it.Number, r.Name))
				}
			}
		}
	}
}

func findLine(r *Rule, n string) *Item {
	for _, p := range r.Parts {
		for _, it := range p.Items {
			if it.Kind == "line" && it.Number == n {
				return it
			}
		}
	}
	return nil
}

func hasYear(src *sourcesv1.Source, year int) bool {
	for _, y := range src.GetTaxYears() {
		if int(y) == year {
			return true
		}
	}
	return false
}
