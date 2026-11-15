package main

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

// A section is one generated run of lines in a checked-in file, between a
// BEGIN and an END marker comment.
type section struct {
	// Path is the file, from the repository root.
	Path string
	// Name names the section in its markers.
	Name string
	// Render returns the section's lines, given the repository root.
	Render func(root string) ([]string, error)
}

// sections is every generated section, in the order they are written.
var sections = []section{
	{"returns/rules/states/00_states.tlisp", "use-proto", useProto},
	{"returns/rules/states/00_states.tlisp", "dispatch", dispatch},
	{"returns/rules/federal/09_payments_and_1040.tlisp", "summary", summary},
	{"returns/v1/state_inputs.proto", "oneof", oneof("Inputs")},
	{"returns/v1/outputs.proto", "oneof", oneof("Return")},
	{"rust/returns/src/rules/states.rs", "rule-files", ruleFiles},
	{"rust/proto/BUILD.bazel", "proto-deps", protoDeps},
	{"rust/proto/src/lib.rs", "re-exports", reExports},
	{"taxprotosgo/BUILD.bazel", "go-proto-libraries", goProtoLibraries},
	{"returns/rules/states/BUILD.bazel", "go-deps", goDeps},
	{"scenarios/v1/BUILD.bazel", "go-deps", goDeps},
	{"docsmodel/rules.go", "documented-states", documentedStates},
}

// byPostal is the registry in postal-code order, and byField in the order of
// the oneof field numbers.
func byPostal() []State {
	out := append([]State(nil), states...)
	sort.Slice(out, func(i, j int) bool { return out[i].Postal < out[j].Postal })
	return out
}

func byField() []State {
	out := append([]State(nil), states...)
	sort.Slice(out, func(i, j int) bool { return out[i].Field < out[j].Field })
	return out
}

// messageName is the state's name as the proto messages spell it, each word
// capitalized: DistrictOfColumbia.
func messageName(s State) string {
	var b strings.Builder
	for _, w := range strings.Fields(s.Name) {
		b.WriteString(strings.ToUpper(w[:1]) + w[1:])
	}
	return b.String()
}

// prepareRule is the taxlisp rule that prepares the state's return.
func prepareRule(s State) string {
	return "prepare-" + strings.ToLower(strings.ReplaceAll(s.Name, " ", "-")) + "-return"
}

// goTarget is the name of the state form's go_proto_library.
func goTarget(s State) string {
	if s.GoTarget != "" {
		return s.GoTarget
	}
	return s.FormDir + "_go_proto"
}

// protoLabel is the state form's proto_library in taxprotos.
func protoLabel(s State) string {
	return fmt.Sprintf("@nexteggapp_taxprotos//us/state/%s/v1/%s:%s_proto", s.Postal, s.FormDir, s.FormDir)
}

func useProto(string) ([]string, error) {
	var out []string
	for _, s := range byPostal() {
		out = append(out, fmt.Sprintf("(use-proto 'nexteggapp.taxprotos.us.state.%s.v1 '%s)", s.Postal, s.Alias))
	}
	return out, nil
}

func dispatch(string) ([]string, error) {
	var out []string
	for _, s := range byPostal() {
		if s.HandDispatch {
			continue
		}
		out = append(out,
			fmt.Sprintf("           ((has? s '%s)", s.Postal),
			"            (proto 'ret/StateReturn",
			fmt.Sprintf("              :state %q", strings.ToUpper(s.Postal)),
			fmt.Sprintf("              :%s (%s inputs (. s '%s) fed-ret tax-year)))", s.Postal, prepareRule(s), s.Postal))
	}
	return out, nil
}

func summary(string) ([]string, error) {
	var out []string
	for _, s := range byPostal() {
		out = append(out, fmt.Sprintf("    ((has? s '%s) (. s '%s.summary))", s.Postal, s.Postal))
	}
	return out, nil
}

// oneof renders the StateInputs or StateReturn oneof fields, kind being
// "Inputs" or "Return".
func oneof(kind string) func(string) ([]string, error) {
	return func(string) ([]string, error) {
		var out []string
		for i, s := range byField() {
			comment := fmt.Sprintf("%s %s.", s.Name, s.Form)
			if kind == "Inputs" {
				comment = fmt.Sprintf("%s %s inputs.", s.Name, s.Form)
				if s.InputsComment != "" {
					comment = s.InputsComment
				}
			} else if s.ReturnComment != "" {
				comment = s.ReturnComment
			}
			if i > 0 {
				out = append(out, "")
			}
			out = append(out,
				"    // "+comment,
				fmt.Sprintf("    %s%s %s = %d;", messageName(s), kind, s.Postal, s.Field))
		}
		return out, nil
	}
}

// ruleFiles lists every rule file under returns/rules/states in the order
// the Go package loads them: by path.
func ruleFiles(root string) ([]string, error) {
	dir := filepath.Join(root, "returns/rules/states")
	var names []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".tlisp") {
			rel, err := filepath.Rel(dir, path)
			if err != nil {
				return err
			}
			names = append(names, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("listing the state rule files: %w", err)
	}
	sort.Strings(names)
	var out []string
	for _, n := range names {
		out = append(out,
			"    (",
			fmt.Sprintf("        %q,", n),
			fmt.Sprintf("        include_str!(%q),", "../../../../returns/rules/states/"+n),
			"    ),")
	}
	return out, nil
}

func protoDeps(string) ([]string, error) {
	var out []string
	for _, s := range byPostal() {
		out = append(out, fmt.Sprintf("        %q,", protoLabel(s)))
	}
	return out, nil
}

func reExports(string) ([]string, error) {
	var out []string
	for _, s := range byPostal() {
		doc := s.Name + "'s forms."
		if s.RustDoc != "" {
			doc = s.RustDoc
		}
		module := s.Postal
		if module == "in" {
			module = "r#in" // in is a Rust keyword
		}
		out = append(out,
			fmt.Sprintf("/// `nexteggapp.taxprotos.us.state.%s.v1`: %s", s.Postal, doc),
			fmt.Sprintf("pub use nexteggapp::taxprotos::us::state::%s::v1 as %sv1;", module, s.Postal))
	}
	return out, nil
}

func goProtoLibraries(string) ([]string, error) {
	var out []string
	for i, s := range byPostal() {
		if i > 0 {
			out = append(out, "")
		}
		out = append(out,
			"go_proto_library(",
			fmt.Sprintf("    name = %q,", goTarget(s)),
			fmt.Sprintf("    importpath = %q,", fmt.Sprintf("github.com/nexteggapp/taxprotos/go/us/state/%s/v1/%s", s.Postal, s.FormDir)),
			fmt.Sprintf("    proto = %q,", protoLabel(s)),
			`    visibility = ["//visibility:public"],`,
			`    deps = [":common_go_proto"],`,
			")")
	}
	return out, nil
}

func goDeps(string) ([]string, error) {
	var out []string
	for _, s := range byPostal() {
		out = append(out, fmt.Sprintf("        %q,", "//taxprotosgo:"+goTarget(s)))
	}
	return out, nil
}

// documentedStates is every state directory the site documents: each
// state's own, and notax/, which the states with no income tax share.
func documentedStates(string) ([]string, error) {
	dirs := []string{"notax"}
	for _, s := range states {
		dirs = append(dirs, s.Postal)
	}
	sort.Strings(dirs)
	var out []string
	line := "\t"
	for _, d := range dirs {
		word := fmt.Sprintf("%q,", d)
		if len(line)+1+len(word) > 76 && line != "\t" {
			out = append(out, line)
			line = "\t"
		}
		if line != "\t" {
			line += " "
		}
		line += word
	}
	return append(out, line), nil
}
