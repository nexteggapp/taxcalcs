// Command stategen writes the lists of states that the shared files keep:
// the dispatch and summary rules, the proto oneofs, the Go and Rust targets
// and re-exports, the Rust rule-file list and the documented states. Each
// list sits between BEGIN GENERATED and END GENERATED marker comments, and is
// written from the registry in registry.go.
//
// Add a state by adding its entry to registry.go, then run, from the
// repository root,
//
//	go run ./tools/stategen
//
// which rewrites every section. With -check it writes nothing and fails when
// a section is out of date; its test runs that check.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
)

func main() {
	check := flag.Bool("check", false, "report out-of-date sections without writing them")
	flag.Parse()
	root := "."
	if dir := os.Getenv("BUILD_WORKSPACE_DIRECTORY"); dir != "" {
		root = dir // under bazel run
	}
	stale, err := run(root, !*check)
	if err != nil {
		fmt.Fprintln(os.Stderr, "stategen:", err)
		os.Exit(1)
	}
	if *check && len(stale) > 0 {
		fmt.Fprintln(os.Stderr, "stategen: out of date, run `go run ./tools/stategen`:")
		for _, s := range stale {
			fmt.Fprintln(os.Stderr, "  "+s)
		}
		os.Exit(1)
	}
}

// run renders every section under root, and writes the ones that changed when
// write is set. It returns the sections that were out of date, as
// "path (name)".
func run(root string, write bool) ([]string, error) {
	if err := validate(states); err != nil {
		return nil, err
	}
	files := map[string]string{}
	var order []string
	var stale []string
	for _, sec := range sections {
		text, ok := files[sec.Path]
		if !ok {
			raw, err := os.ReadFile(root + "/" + sec.Path)
			if err != nil {
				return nil, err
			}
			text = string(raw)
			order = append(order, sec.Path)
		}
		lines, err := sec.Render(root)
		if err != nil {
			return nil, err
		}
		updated, err := replace(text, sec.Name, lines)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", sec.Path, err)
		}
		if updated != text {
			stale = append(stale, fmt.Sprintf("%s (%s)", sec.Path, sec.Name))
		}
		files[sec.Path] = updated
	}
	if write {
		for _, path := range order {
			info, err := os.Stat(root + "/" + path)
			if err != nil {
				return nil, err
			}
			if err := os.WriteFile(root+"/"+path, []byte(files[path]), info.Mode()); err != nil {
				return nil, err
			}
		}
	}
	return stale, nil
}

// replace puts lines between the named section's BEGIN and END markers,
// keeping the marker lines themselves.
func replace(text, name string, lines []string) (string, error) {
	begin := "BEGIN GENERATED (" + name + ")"
	end := "END GENERATED (" + name + ")"
	all := strings.Split(text, "\n")
	b, e := -1, -1
	for i, l := range all {
		switch {
		case strings.Contains(l, begin):
			if b >= 0 {
				return "", fmt.Errorf("two %q markers", begin)
			}
			b = i
		case strings.Contains(l, end):
			e = i
		}
	}
	if b < 0 || e < b {
		return "", fmt.Errorf("no %q ... %q markers", begin, end)
	}
	out := append([]string{}, all[:b+1]...)
	out = append(out, lines...)
	out = append(out, all[e:]...)
	return strings.Join(out, "\n"), nil
}

// validate checks that no two states share a postal code, a oneof field, an
// alias or a Go target, and that no state takes a field kept for another use.
func validate(ss []State) error {
	reserved := map[int]string{53: "NoIncomeTaxReturn", 99: "UnsupportedStateReturn"}
	seen := map[string]string{}
	var errs []error
	claim := func(kind, key, postal string) {
		k := kind + " " + key
		if other, ok := seen[k]; ok {
			errs = append(errs, fmt.Errorf("%s and %s share %s", other, postal, k))
		}
		seen[k] = postal
	}
	for _, s := range ss {
		if len(s.Postal) != 2 || strings.ToLower(s.Postal) != s.Postal {
			errs = append(errs, fmt.Errorf("postal code %q is not two lower-case letters", s.Postal))
		}
		if s.Name == "" || s.Form == "" || s.Alias == "" || s.FormDir == "" || s.Field < 10 {
			errs = append(errs, fmt.Errorf("%s: every field but the overrides is required, and Field starts at 10", s.Postal))
		}
		if use, ok := reserved[s.Field]; ok {
			errs = append(errs, fmt.Errorf("%s: field %d is %s's", s.Postal, s.Field, use))
		}
		claim("postal code", s.Postal, s.Postal)
		claim("field", fmt.Sprint(s.Field), s.Postal)
		claim("alias", s.Alias, s.Postal)
		claim("Go target", goTarget(s), s.Postal)
	}
	return errors.Join(errs...)
}
