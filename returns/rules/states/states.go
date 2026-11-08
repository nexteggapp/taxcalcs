// Package states holds the taxlisp rule modules for state individual income
// tax returns: California Form 540 and Oregon Form OR-40.
//
// The modules are not a program of their own. They read the prepared federal
// return, so [federal.LoadEngine] loads them after the federal rules into one
// module, and the federal prepare-return calls prepare-state-returns.
package states

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
)

//go:embed *.tlisp ca/*.tlisp or/*.tlisp
var ruleFS embed.FS

// File is one rule module: its path under this directory, which names it in
// error messages, and its source.
type File struct {
	Name   string
	Source string
}

// Files returns every rule module in load order: the shared 00_states.tlisp,
// then each state's directory, each in lexical order.
func Files() ([]File, error) {
	var names []string
	err := fs.WalkDir(ruleFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			names = append(names, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("reading embedded state rules: %w", err)
	}
	sort.Strings(names)
	files := make([]File, 0, len(names))
	for _, name := range names {
		raw, err := ruleFS.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", name, err)
		}
		files = append(files, File{Name: name, Source: string(raw)})
	}
	return files, nil
}
