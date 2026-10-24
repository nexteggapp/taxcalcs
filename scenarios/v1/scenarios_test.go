// Package v1_test checks the TaxYearScenarios schema against the example in
// docs/scenarios.md, and checks that every element
// of the schema is documented.
//
// testdata/example.jsonc is that example, copied verbatim. It must parse
// strictly; each scenario must resolve, by applying the
// diffs down its parent chain with protodiffgen's apply package; and the
// resolved inputs must survive the wire and JSON unchanged.
package v1_test

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/bufbuild/protocompile"
	"github.com/nexteggapp/protodiffgen/apply"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
)

// The schema sources, for the documentation check, and the example.
//
//go:embed *.proto testdata/example.jsonc
var files embed.FS

// importPrefix is what the proto_library's import_prefix puts before the
// module-relative path of a hand-written file. The generated
// inputs_diff.proto has no prefix.
const importPrefix = "taxcalcs/"

// strict parses strictly: an unknown field is an error.
var strict = protojson.UnmarshalOptions{DiscardUnknown: false}

// TestExampleParsesStrictly parses the doc's example and checks a few of its
// values reached the fields they name.
func TestExampleParsesStrictly(t *testing.T) {
	file := readExample(t)
	if file.GetSchemaVersion() != 1 || file.GetTaxYear() != 2026 {
		t.Errorf("schema_version %d, tax_year %d; want 1, 2026", file.GetSchemaVersion(), file.GetTaxYear())
	}
	acme := file.GetShared().GetModel().GetEmployers()
	if len(acme) != 1 || acme[0].GetId() != "acme" {
		t.Fatalf("employers = %v, want one, acme", acme)
	}
	stub := acme[0].GetParts()[0].GetFromPaystubs().GetStubs()[0]
	if got := stub.GetPaystub().GetThisPaycheck().GetNet().GetMinor(); got != 410218 {
		t.Errorf("the paystub's net is %d minor units, want 410218", got)
	}
	if acme[0].GetDefaultPart().GetSameAsLatest() == nil {
		t.Error("the default part is not same_as_latest")
	}
	if len(file.GetScenarios()) != 2 {
		t.Errorf("%d scenarios, want 2", len(file.GetScenarios()))
	}
}

// TestUnknownFieldIsRefused checks the parse is strict, which is what catches
// a computed figure added to the file.
func TestUnknownFieldIsRefused(t *testing.T) {
	text := strings.Replace(string(stripComments(t, exampleText(t))), `"taxYear": 2026,`, `"taxYear": 2026, "taxOwed": 1,`, 1)
	if !strings.Contains(text, "taxOwed") {
		t.Fatal("the example has no top-level taxYear to add a field beside")
	}
	if err := strict.Unmarshal([]byte(text), &scenariosv1.TaxYearScenarios{}); err == nil {
		t.Error("an unknown field was accepted")
	}
}

// TestScenariosResolve applies each scenario's diff to its parent's inputs,
// checks apply.Validate accepts it, and checks the raise reached Acme.
func TestScenariosResolve(t *testing.T) {
	resolved := resolveAll(t, readExample(t))
	plan, raise := resolved["plan"], resolved["raise"]
	if plan == nil || raise == nil {
		t.Fatalf("resolved %d scenarios, want plan and raise", len(resolved))
	}
	if !proto.Equal(plan, readExample(t).GetShared()) {
		t.Error("Your plan has no changes, so its inputs should equal shared")
	}
	parts := raise.GetModel().GetEmployers()[0].GetParts()
	if len(parts) != 3 {
		t.Fatalf("Raise in Oct's Acme has %d parts, want 3", len(parts))
	}
	payRate := parts[2].GetPayRate()
	if parts[2].GetId() != "acme-v2k8n5zd" || payRate.GetPaycheck().GetGross().GetMinor() != 625000 {
		t.Errorf("the added part is %v, want the $6,250 pay rate", parts[2])
	}
	if len(plan.GetModel().GetEmployers()[0].GetParts()) != 2 {
		t.Error("applying Raise in Oct changed Your plan's inputs")
	}
}

// TestResolvedInputsRoundTrip checks that each scenario's resolved inputs
// survive the wire and JSON, and that diffing a parent with a child's inputs
// gives back the child's stored diff, which is therefore in normal form.
func TestResolvedInputsRoundTrip(t *testing.T) {
	file := readExample(t)
	resolved := resolveAll(t, file)
	for _, s := range file.GetScenarios() {
		got := resolved[s.GetId()]
		b, err := proto.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		wire := &scenariosv1.ScenarioInputs{}
		if err := proto.Unmarshal(b, wire); err != nil {
			t.Fatal(err)
		}
		j, err := protojson.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		fromJSON := &scenariosv1.ScenarioInputs{}
		if err := strict.Unmarshal(j, fromJSON); err != nil {
			t.Fatal(err)
		}
		if !proto.Equal(wire, got) || !proto.Equal(fromJSON, got) {
			t.Errorf("%s: the resolved inputs changed in a round trip", s.GetId())
		}

		parent := file.GetShared()
		if s.GetParent() != "" {
			parent = resolved[s.GetParent()]
		}
		d := &scenariosv1.ScenarioInputsDiff{}
		if err := apply.Diff(parent, got, d); err != nil {
			t.Fatalf("%s: Diff: %v", s.GetId(), err)
		}
		if !proto.Equal(d, changes(s)) {
			t.Errorf("%s: Diff(parent, inputs) = %v, want the stored %v", s.GetId(), d, changes(s))
		}
	}

	b, err := protojson.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	again := &scenariosv1.TaxYearScenarios{}
	if err := strict.Unmarshal(b, again); err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(again, file) {
		t.Error("the file changed in a JSON round trip")
	}
}

// TestProtosAreDocumented fails for each message, field, oneof, enum or
// enum value of scenarios/v1 without a leading comment, the generated
// inputs_diff.proto included. It parses the sources, since compiled
// descriptors carry no comments, and resolves the taxprotos and protodiffgen
// imports from the descriptors the Go packages register.
func TestProtosAreDocumented(t *testing.T) {
	entries, err := files.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	sources := map[string][]byte{}
	var paths []string
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".proto") {
			continue
		}
		b, err := files.ReadFile(e.Name())
		if err != nil {
			t.Fatal(err)
		}
		p := importPrefix + "scenarios/v1/" + e.Name()
		if e.Name() == "inputs_diff.proto" {
			p = "scenarios/v1/" + e.Name()
		}
		sources[p] = b
		paths = append(paths, p)
	}
	if len(paths) < 11 {
		t.Fatalf("found only %v", paths)
	}
	c := protocompile.Compiler{
		SourceInfoMode: protocompile.SourceInfoStandard,
		Resolver: protocompile.ResolverFunc(func(p string) (protocompile.SearchResult, error) {
			if b, ok := sources[p]; ok {
				return protocompile.SearchResult{Source: io.NopCloser(bytes.NewReader(b))}, nil
			}
			fd, err := protoregistry.GlobalFiles.FindFileByPath(p)
			if err != nil {
				return protocompile.SearchResult{}, fmt.Errorf("%s: %w", p, err)
			}
			return protocompile.SearchResult{Desc: fd}, nil
		}),
	}
	compiled, err := c.Compile(context.Background(), paths...)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range compiled {
		checkDocumented(t, f)
	}
}

// resolveAll returns each scenario's inputs: shared with each diff down the
// scenario's parent chain applied in turn. It fails the test on a scenario
// whose parent is missing or comes later, or whose diff apply.Validate
// refuses.
func resolveAll(t *testing.T, file *scenariosv1.TaxYearScenarios) map[string]*scenariosv1.ScenarioInputs {
	t.Helper()
	resolved := map[string]*scenariosv1.ScenarioInputs{}
	for i, s := range file.GetScenarios() {
		parent := file.GetShared()
		switch {
		case i == 0 && (s.GetId() != "plan" || s.GetParent() != ""):
			t.Fatalf("the first scenario is %q with parent %q; want Your plan, with none", s.GetId(), s.GetParent())
		case i > 0 && resolved[s.GetParent()] == nil:
			t.Fatalf("%s: parent %q is not an earlier scenario", s.GetId(), s.GetParent())
		case i > 0:
			parent = resolved[s.GetParent()]
		}
		if err := apply.Validate(parent, changes(s)); err != nil {
			t.Fatalf("%s: Validate: %v", s.GetId(), err)
		}
		next, err := apply.Apply(parent, changes(s))
		if err != nil {
			t.Fatalf("%s: Apply: %v", s.GetId(), err)
		}
		resolved[s.GetId()] = next.(*scenariosv1.ScenarioInputs)
	}
	return resolved
}

// changes returns a scenario's diff, or an empty one when it has none.
func changes(s *scenariosv1.Scenario) *scenariosv1.ScenarioInputsDiff {
	if s.GetChanges() == nil {
		return &scenariosv1.ScenarioInputsDiff{}
	}
	return s.GetChanges()
}

// readExample parses testdata/example.jsonc strictly.
func readExample(t *testing.T) *scenariosv1.TaxYearScenarios {
	t.Helper()
	file := &scenariosv1.TaxYearScenarios{}
	if err := strict.Unmarshal(stripComments(t, exampleText(t)), file); err != nil {
		t.Fatalf("parse testdata/example.jsonc: %v", err)
	}
	return file
}

// exampleText returns testdata/example.jsonc as written.
func exampleText(t *testing.T) []byte {
	t.Helper()
	b, err := files.ReadFile("testdata/example.jsonc")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// stripComments removes JSONC comments: "//" to the end of the line, and
// "/*" to "*/". Text inside a JSON string is kept as is, so a string holding
// "//" is not cut. Each comment's newlines are kept, so a JSON error's line
// number still points into the original.
func stripComments(t *testing.T, in []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	for i := 0; i < len(in); i++ {
		switch {
		case in[i] == '"':
			end := stringEnd(in, i)
			out.Write(in[i:end])
			i = end - 1
		case bytes.HasPrefix(in[i:], []byte("//")):
			for i < len(in) && in[i] != '\n' {
				i++
			}
			if i < len(in) {
				out.WriteByte('\n')
			}
		case bytes.HasPrefix(in[i:], []byte("/*")):
			end := bytes.Index(in[i+2:], []byte("*/"))
			if end < 0 {
				t.Fatal("a /* comment is not closed")
			}
			out.Write(bytes.Repeat([]byte("\n"), bytes.Count(in[i:i+2+end+2], []byte("\n"))))
			i += 2 + end + 1
		default:
			out.WriteByte(in[i])
		}
	}
	return out.Bytes()
}

// stringEnd returns the index just past the JSON string that starts at
// in[start], a double quote, or len(in) if it is not closed.
func stringEnd(in []byte, start int) int {
	for i := start + 1; i < len(in); i++ {
		switch in[i] {
		case '\\':
			i++
		case '"':
			return i + 1
		}
	}
	return len(in)
}

// TestStripComments checks the helper keeps strings and drops both kinds of
// comment.
func TestStripComments(t *testing.T) {
	in := "{ // a\n \"u\": \"http://x/*y*/\", /* b\n c */ \"q\": \"\\\"//\" }"
	want := "{ \n \"u\": \"http://x/*y*/\", \n \"q\": \"\\\"//\" }"
	if got := string(stripComments(t, []byte(in))); got != want {
		t.Errorf("stripComments(%q) = %q, want %q", in, got, want)
	}
}

// checkDocumented fails for each message, field, oneof, enum, enum value or
// extension in fd without a leading comment. It mirrors the check
// protodiffgen runs on its own schemas, in its generate/generate_test.go.
func checkDocumented(t *testing.T, fd protoreflect.FileDescriptor) {
	t.Helper()
	locs := fd.SourceLocations()
	check := func(d protoreflect.Descriptor) {
		if strings.TrimSpace(locs.ByDescriptor(d).LeadingComments) == "" {
			t.Errorf("%s: %s has no doc comment", fd.Path(), d.FullName())
		}
	}
	var enums func(protoreflect.EnumDescriptors)
	enums = func(es protoreflect.EnumDescriptors) {
		for i := 0; i < es.Len(); i++ {
			check(es.Get(i))
			for j := 0; j < es.Get(i).Values().Len(); j++ {
				check(es.Get(i).Values().Get(j))
			}
		}
	}
	exts := func(xs protoreflect.ExtensionDescriptors) {
		for i := 0; i < xs.Len(); i++ {
			check(xs.Get(i))
		}
	}
	var messages func(protoreflect.MessageDescriptors)
	messages = func(ms protoreflect.MessageDescriptors) {
		for i := 0; i < ms.Len(); i++ {
			md := ms.Get(i)
			if md.IsMapEntry() {
				continue
			}
			check(md)
			for j := 0; j < md.Fields().Len(); j++ {
				check(md.Fields().Get(j))
			}
			for j := 0; j < md.Oneofs().Len(); j++ {
				if !md.Oneofs().Get(j).IsSynthetic() {
					check(md.Oneofs().Get(j))
				}
			}
			enums(md.Enums())
			exts(md.Extensions())
			messages(md.Messages())
		}
	}
	messages(fd.Messages())
	enums(fd.Enums())
	exts(fd.Extensions())
}
