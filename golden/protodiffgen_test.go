package golden

import (
	"context"
	"encoding/base64"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/bufbuild/protocompile"
	"github.com/nexteggapp/protodiffgen/apply"
	"github.com/nexteggapp/protodiffgen/generate"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"

	// Registers nextgui.xyz/protodiffgen/v1/config.proto, which the test
	// schema's generated diff file imports.
	_ "github.com/nexteggapp/protodiffgen/v1"

	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear"
)

// The protodiffgen vectors, for the Rust port of protodiffgen's apply
// package in rust/protodiffgen.
//
// scenarios.json walks every scenario of every taxyear test case down its
// parent chain, as taxyear/resolve.go does, and records what apply.Apply
// gives for its changes against its parent's inputs, and apply.Diff from the
// parent to the result. refusals.json holds diffs of the real schema that
// Validate refuses, one for each refusal the schema can reach.
//
// pdgtest.json does the same for protodiffgen's own test schema, copied into
// testdata/protodiffgen/pdgtest, which has every shape the package handles
// (maps, indexed and ordered lists, enum keys) where the taxcalcs schema has
// only unordered keyed lists. It holds the schema's FileDescriptorSet with
// the generated diff file, and the cases of apply's own tests.

// pdgMessages is a table of encoded messages, each held once, so the many
// scenarios that share inputs don't repeat them.
type pdgMessages struct {
	list []string
	at   map[string]int
}

// add returns m's index in the table, or -1 for nil.
func (p *pdgMessages) add(t *testing.T, m proto.Message) int {
	t.Helper()
	if m == nil {
		return -1
	}
	s := encode(t, m)
	if i, ok := p.at[s]; ok {
		return i
	}
	if p.at == nil {
		p.at = map[string]int{}
	}
	p.at[s] = len(p.list)
	p.list = append(p.list, s)
	return len(p.list) - 1
}

// pdgScenario is one scenario: its parent's inputs, its changes, and what
// apply.Apply gives, the result or the error. Diff is apply.Diff from the
// parent to the result, when there is one.
type pdgScenario struct {
	ID        string `json:"id"`
	Parent    int    `json:"parent"`
	Changes   int    `json:"changes"`
	Result    int    `json:"result"`
	Error     string `json:"error"`
	Diff      int    `json:"diff"`
	DiffError string `json:"diff_error"`
}

type pdgCase struct {
	File      string        `json:"file"`
	Scenarios []pdgScenario `json:"scenarios"`
}

type pdgScenarios struct {
	Messages []string  `json:"messages"`
	Cases    []pdgCase `json:"cases"`
}

// pdgCaseFiles is every taxyear test case and fixture.
func pdgCaseFiles(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, dir := range []string{"../taxyear/testdata/cases", "../taxyear/testdata/fixtures"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".jsonc") {
				out = append(out, filepath.Join(dir, e.Name()))
			}
		}
	}
	sort.Strings(out)
	return out
}

func TestProtodiffgenScenarios(t *testing.T) {
	var msgs pdgMessages
	var out pdgScenarios
	for _, path := range pdgCaseFiles(t) {
		text, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		file, err := taxyear.Parse(text)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		c := pdgCase{File: strings.TrimPrefix(path, "../")}
		byID := map[string]*scenariosv1.ScenarioInputs{}
		for i, s := range file.GetScenarios() {
			// The chain rules of taxyear's chainProblem: such a scenario is
			// never applied.
			switch {
			case i == 0 && (s.GetId() != "plan" || s.GetParent() != ""),
				i > 0 && s.GetParent() == "",
				i > 0 && byID[s.GetParent()] == nil,
				byID[s.GetId()] != nil:
				continue
			}
			parent := file.GetShared()
			if i > 0 {
				parent = byID[s.GetParent()]
			}
			if parent == nil {
				parent = &scenariosv1.ScenarioInputs{}
			}
			changes := s.GetChanges()
			if changes == nil {
				changes = &scenariosv1.ScenarioInputsDiff{}
			}
			v := pdgScenario{ID: s.GetId(), Parent: msgs.add(t, parent), Changes: msgs.add(t, changes), Result: -1, Diff: -1}
			got, err := apply.Apply(parent, changes)
			v.Error = errText(err)
			if err == nil {
				result := got.(*scenariosv1.ScenarioInputs)
				byID[s.GetId()] = result
				v.Result = msgs.add(t, result)
				d := &scenariosv1.ScenarioInputsDiff{}
				if err := apply.Diff(parent, result, d); err != nil {
					v.DiffError = err.Error()
				} else {
					v.Diff = msgs.add(t, d)
					back, err := apply.Apply(parent, d)
					if err != nil || !apply.Equal(back, result, d) {
						t.Fatalf("%s %s: Diff then Apply doesn't round-trip: %v", path, s.GetId(), err)
					}
				}
			}
			c.Scenarios = append(c.Scenarios, v)
		}
		out.Cases = append(out.Cases, c)
	}
	out.Messages = msgs.list
	check(t, "protodiffgen/scenarios.json", out)
}

// pdgRefusal is a diff Validate refuses, and the error.
type pdgRefusal struct {
	Name    string `json:"name"`
	Parent  int    `json:"parent"`
	Changes int    `json:"changes"`
	Error   string `json:"error"`
	Result  int    `json:"result"`
}

// pdgTyped is a call with messages of the wrong types, and its error.
type pdgTyped struct {
	Name  string `json:"name"`
	Error string `json:"error"`
}

type pdgRefusals struct {
	Messages []string     `json:"messages"`
	Refusals []pdgRefusal `json:"refusals"`
	Typed    []pdgTyped   `json:"typed"`
}

// pdgUnknown adds an unknown field to m.
func pdgUnknown(m protoreflect.Message) {
	m.SetUnknown(protowire.AppendVarint(protowire.AppendTag(nil, 9999, protowire.VarintType), 1))
}

// pdgSub is m's message field name, made if unset.
func pdgSub(m protoreflect.Message, name protoreflect.Name) protoreflect.Message {
	return m.Mutable(m.Descriptor().Fields().ByName(name)).Message()
}

func TestProtodiffgenRefusals(t *testing.T) {
	text, err := os.ReadFile("../taxyear/testdata/cases/full-2026.jsonc")
	if err != nil {
		t.Fatal(err)
	}
	file, err := taxyear.Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	shared := file.GetShared()
	// withDoc has an unassigned document, an item of a keyed list whose
	// items are atomic.
	withDoc := proto.Clone(shared).(*scenariosv1.ScenarioInputs)
	docs := &scenariosv1.ScenarioInputs{}
	if err := prototext.Unmarshal([]byte(`facts { unassigned { id: "doc-1" } unassigned { id: "doc-2" } }`), docs); err != nil {
		t.Fatal(err)
	}
	proto.Merge(withDoc, docs)
	dupPeople := proto.Clone(shared).(*scenariosv1.ScenarioInputs)
	dupPeople.Facts.People = append(dupPeople.Facts.People, dupPeople.Facts.People[0])
	unknownA := proto.Clone(shared).(*scenariosv1.ScenarioInputs)
	pdgUnknown(unknownA.Model.ProtoReflect())

	tests := []struct {
		name   string
		parent *scenariosv1.ScenarioInputs
		d      string
		edit   func(d protoreflect.Message)
	}{
		{"removes a key the list lacks", shared, `facts { people { id: "nobody" remove {} } }`, nil},
		{"adds a key the list has", shared, `facts { people { id: "you" add { id: "you" } } }`, nil},
		{"patches a key the list lacks", shared, `facts { people { id: "nobody" patch { name: "N" } } }`, nil},
		{"two changes for one key", shared, `facts { people { id: "rosa" patch { name: "R" } } people { id: "rosa" remove {} } }`, nil},
		{"a change with no op", shared, `facts { people { id: "you" } }`, nil},
		{"the add's key differs", shared, `facts { people { id: "new" add { id: "other" } } }`, nil},
		{"the patch changes the key", shared, `facts { people { id: "you" patch { id: "me" } } }`, nil},
		{"the patch clears the key", shared, `facts { people { id: "you" patch { clear: 1 } } }`, nil},
		{"the patch keeps the key", shared, `facts { people { id: "you" patch { id: "you" name: "Y" } } }`, nil},
		{"an atomic item's patch changes the key", withDoc, `facts { unassigned { id: "doc-1" patch { id: "doc-3" } } }`, nil},
		{"an atomic item's patch replaces it", withDoc, `facts { unassigned { id: "doc-1" patch { id: "doc-1" source { } } } }`, nil},
		{"removes an atomic item", withDoc, `facts { unassigned { id: "doc-2" remove {} } }`, nil},
		{"clears a field the target doesn't have", shared, `clear: 99`, nil},
		{"clears a repeated field", shared, `clear: 3`, nil},
		{"clears a field without explicit presence", shared, `facts { people { id: "you" patch { clear: 2 } } }`, nil},
		{"both sets and clears one field", shared, `facts {} clear: 1`, nil},
		{"clears a message", shared, `clear: 2`, nil},
		{"a nested refusal", shared, `model { employers { id: "acme" patch { parts { id: "nope" remove {} } } } }`, nil},
		{"a deeper nested refusal", shared, `model { employers { id: "acme" patch { parts { id: "acme-1m6wsfr0" patch { from_paystubs { stubs { id: "employer.acme.stub.2026-01-15.0" add { id: "employer.acme.stub.2026-01-15.0" } } } } } } } }`, nil},
		{"an add with two items for one key", shared, `model { employers { id: "new" add { id: "new" parts { id: "p" } parts { id: "p" } } } }`, nil},
		{"an add with nested unknown fields", shared, `model { employers { id: "new" add { id: "new" parts { id: "p" } } } }`, func(d protoreflect.Message) {
			w := pdgSub(d, "model").Get(pdgSub(d, "model").Descriptor().Fields().ByName("employers")).List().Get(0).Message()
			item := w.Get(w.Descriptor().Fields().ByName("add")).Message()
			pdgUnknown(item.Get(item.Descriptor().Fields().ByName("parts")).List().Get(0).Message())
		}},
		{"unknown fields in d", shared, `facts { people { id: "you" patch { name: "Y" } } }`, func(d protoreflect.Message) {
			f := pdgSub(d, "facts")
			w := f.Get(f.Descriptor().Fields().ByName("people")).List().Get(0).Message()
			pdgUnknown(w.Get(w.Descriptor().Fields().ByName("patch")).Message())
		}},
		{"unknown fields at d's root", shared, ``, func(d protoreflect.Message) { pdgUnknown(d) }},
		{"unknown fields in a", unknownA, `facts { people { id: "you" patch { name: "Y" } } }`, nil},
		{"a with two items for one key", dupPeople, ``, nil},
		{"a new oneof case starts from empty", shared, `model { employers { id: "acme" patch { parts { id: "acme-1m6wsfr0" patch { from_deposits { } } } } } }`, nil},
	}
	var msgs pdgMessages
	var out pdgRefusals
	for _, tt := range tests {
		d := &scenariosv1.ScenarioInputsDiff{}
		if err := prototext.Unmarshal([]byte(tt.d), d); err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		if tt.edit != nil {
			tt.edit(d.ProtoReflect())
		}
		got, err := apply.Apply(tt.parent, d)
		v := pdgRefusal{Name: tt.name, Parent: msgs.add(t, tt.parent), Changes: msgs.add(t, d), Error: errText(err), Result: -1}
		if err == nil {
			v.Result = msgs.add(t, got)
		}
		out.Refusals = append(out.Refusals, v)
	}
	diffErr := func(a, b proto.Message, out proto.Message) string { return errText(apply.Diff(a, b, out)) }
	_, applyErr := apply.Apply(&scenariosv1.ScenarioInputs{}, &scenariosv1.ScenarioInputs{})
	out.Typed = []pdgTyped{
		{"Apply with a diff that is not a patch body", errText(applyErr)},
		{"Validate with a target of another type", errText(apply.Validate(&scenariosv1.TaxYearModel{}, &scenariosv1.ScenarioInputsDiff{}))},
		{"Diff with a and b of different types", diffErr(&scenariosv1.ScenarioInputs{}, &scenariosv1.TaxYearModel{}, &scenariosv1.ScenarioInputsDiff{})},
		{"Diff with a diff for another target", diffErr(&scenariosv1.TaxYearModel{}, &scenariosv1.TaxYearModel{}, &scenariosv1.ScenarioInputsDiff{})},
		{"Diff with a with two items for one key", diffErr(dupPeople, shared, &scenariosv1.ScenarioInputsDiff{})},
		{"Diff with b with two items for one key", diffErr(shared, dupPeople, &scenariosv1.ScenarioInputsDiff{})},
		{"Diff with a with unknown fields", diffErr(unknownA, shared, &scenariosv1.ScenarioInputsDiff{})},
	}
	out.Messages = msgs.list
	check(t, "protodiffgen/refusals.json", out)
}

// The test schema.

// pdgSchema compiles the test schema and generates its diff file, as
// protodiffgen's internal/testschema does.
func pdgSchema(t *testing.T) (*descriptorpb.FileDescriptorSet, *protoregistry.Files) {
	t.Helper()
	const root = "testdata/protodiffgen/pdgtest"
	sources := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".proto") {
			return err
		}
		b, err := os.ReadFile(path)
		rel, _ := filepath.Rel(root, path)
		sources["pdgtest/"+filepath.ToSlash(rel)] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(root + "/config.textproto")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for name := range sources {
		names = append(names, name)
	}
	sort.Strings(names)
	c := protocompile.Compiler{Resolver: protocompile.WithStandardImports(&protocompile.SourceResolver{
		Accessor: protocompile.SourceAccessorFromMap(sources),
	})}
	files, err := c.Compile(context.Background(), names...)
	if err != nil {
		t.Fatal(err)
	}
	set := &descriptorpb.FileDescriptorSet{}
	seen := map[string]bool{}
	var add func(fd protoreflect.FileDescriptor)
	add = func(fd protoreflect.FileDescriptor) {
		if seen[fd.Path()] {
			return
		}
		seen[fd.Path()] = true
		for i := 0; i < fd.Imports().Len(); i++ {
			add(fd.Imports().Get(i).FileDescriptor)
		}
		set.File = append(set.File, protodesc.ToFileDescriptorProto(fd))
	}
	for _, f := range files {
		add(f)
	}
	text, err := generate.File(set, config)
	if err != nil {
		t.Fatal(err)
	}
	const diffPath = "protodiffgen_test/diff.proto"
	byPath := map[string]protoreflect.FileDescriptor{}
	for _, f := range files {
		byPath[f.Path()] = f
	}
	c = protocompile.Compiler{Resolver: protocompile.WithStandardImports(protocompile.ResolverFunc(func(path string) (protocompile.SearchResult, error) {
		switch {
		case path == diffPath:
			return protocompile.SearchResult{Source: strings.NewReader(string(text))}, nil
		case byPath[path] != nil:
			return protocompile.SearchResult{Desc: byPath[path]}, nil
		}
		if fd, err := protoregistry.GlobalFiles.FindFileByPath(path); err == nil {
			return protocompile.SearchResult{Desc: fd}, nil
		}
		return protocompile.SearchResult{}, fs.ErrNotExist
	}))}
	diff, err := c.Compile(context.Background(), diffPath)
	if err != nil {
		t.Fatalf("compiling the generated file: %v\n%s", err, text)
	}
	add(diff[0])
	reg, err := protodesc.NewFiles(set)
	if err != nil {
		t.Fatal(err)
	}
	return set, reg
}

// pdgCheck is a Validate or Apply case: a, d, and what Apply gives.
type pdgCheck struct {
	Name   string `json:"name"`
	A      string `json:"a"`
	D      string `json:"d"`
	Error  string `json:"error"`
	Result string `json:"result"`
}

// pdgDiffCase is a Diff case: a, b and what Diff gives.
type pdgDiffCase struct {
	Name  string `json:"name"`
	A     string `json:"a"`
	B     string `json:"b"`
	Diff  string `json:"diff"`
	Error string `json:"error"`
}

type pdgEqualCase struct {
	X     string `json:"x"`
	Y     string `json:"y"`
	Equal bool   `json:"equal"`
}

// pdgStep is one step of an overlap's path.
type pdgStep struct {
	List  string `json:"list"`
	Key   string `json:"key"`
	Index int    `json:"index"`
}

type pdgItem struct {
	Text  string    `json:"text"`
	Field int       `json:"field"`
	Path  []pdgStep `json:"path"`
}

type pdgOverlapCase struct {
	Name  string    `json:"name"`
	D1    string    `json:"d1"`
	D2    string    `json:"d2"`
	Items []pdgItem `json:"items"`
	Back  []pdgItem `json:"back"`
	Error string    `json:"error"`
}

// pdgPair is Diff over a pair of the test messages, for a broader check of
// the canonical diff.
type pdgPair struct {
	A     int    `json:"a"`
	B     int    `json:"b"`
	Diff  string `json:"diff"`
	Error string `json:"error"`
}

type pdgTest struct {
	Schema   string           `json:"schema"`
	Validate []pdgCheck       `json:"validate"`
	Apply    []pdgCheck       `json:"apply"`
	Diff     []pdgDiffCase    `json:"diff"`
	Equal    []pdgEqualCase   `json:"equal"`
	Overlap  []pdgOverlapCase `json:"overlap"`
	Target   []pdgCheck       `json:"target"`
	Roots    []string         `json:"roots"`
	Pairs    []pdgPair        `json:"pairs"`
}

func TestProtodiffgenTestSchema(t *testing.T) {
	set, reg := pdgSchema(t)
	parse := func(name protoreflect.FullName, s string) *dynamicpb.Message {
		t.Helper()
		d, err := reg.FindDescriptorByName(name)
		if err != nil {
			t.Fatal(err)
		}
		m := dynamicpb.NewMessage(d.(protoreflect.MessageDescriptor))
		if err := prototext.Unmarshal([]byte(s), m); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return m
	}
	rootOf := func(s string) *dynamicpb.Message { return parse("pdgtest.v1.Root", s) }
	diffOf := func(s string) *dynamicpb.Message { return parse("pdgtest.v1.diff.RootDiff", s) }
	body := diffOf(``).Descriptor()
	enc := func(m proto.Message) string { return encode(t, m) }
	out := pdgTest{Schema: encode(t, set)}

	// apply's TestValidateRefusals and TestValidateAccepts.
	const base = `
		name: "r"
		count: 1
		members { id: "a" name: "Ada" lines { text: "x" } lines { text: "y" } }
		members { id: "b" }
		counts { key: "k" value: 1 }
		configs { key: 1 value { theme: "t" } }
		legacy { entries { key: "e" } }
		scores: 1 scores: 2
		cash: "c"
		coins { heads: true value: 1 }
	`
	refusals := []struct {
		name, a, d string
		unknown    string // "a" or "d": add an unknown field there
	}{
		{"adds a key the list has", base, `members { id: "a" add { id: "a" } }`, ""},
		{"adds a key the map has", base, `configs { key: 1 add {} }`, ""},
		{"removes a key the list lacks", base, `members { id: "z" remove {} }`, ""},
		{"removes a key the map lacks", base, `counts { key: "z" remove {} }`, ""},
		{"patches a key the list lacks", base, `members { id: "z" patch { name: "Z" } }`, ""},
		{"patches a key the map lacks", base, `configs { key: 9 patch { theme: "u" } }`, ""},
		{"two entries for one key", base, `members { id: "a" remove {} } members { id: "a" patch {} }`, ""},
		{"two entries for one map key", base, `counts { key: "k" set: 2 } counts { key: "k" remove {} }`, ""},
		{"a change wrapper with no op", base, `members { id: "a" }`, ""},
		{"a map change with no op", base, `counts { key: "k" }`, ""},
		{"an indexed edit with no op", base, `scores { index: 0 }`, ""},
		{"a wrapper key that differs from its add's key", base, `members { id: "c" add { id: "d" } }`, ""},
		{"a patch that changes a keyed item's key", base, `members { id: "a" patch { id: "z" } }`, ""},
		{"a patch that changes an atomic item's key", base, `coins { heads: true patch { heads: false } }`, ""},
		{"a patch that clears an optional key", base, `legacy { entries { key: "e" patch { unset: 1 } } }`, ""},
		{"clears a field without explicit presence", base, `unset: 1`, ""},
		{"clears a repeated field", base, `unset: 17`, ""},
		{"clears a field the target doesn't have", base, `unset: 99`, ""},
		{"both sets and clears one field", base, `count: 2 unset: 2`, ""},
		{"an order missing a key", base, `members_order: "a"`, ""},
		{"an order naming a removed key", base, `members { id: "b" remove {} } members_order: "a" members_order: "b"`, ""},
		{"an order naming a key twice", base, `members_order: "a" members_order: "b" members_order: "a"`, ""},
		{"a patch index outside [0, n)", base, `scores { index: 2 patch: 5 }`, ""},
		{"a remove index outside [0, n)", base, `members { id: "a" patch { lines { index: 2 remove {} } } }`, ""},
		{"an insert index outside [0, n]", base, `scores { index: 3 insert_before { items: 1 } }`, ""},
		{"a second insert at one index", base, `scores { index: 2 insert_before { items: 1 } } scores { index: 2 insert_before { items: 2 } }`, ""},
		{"a patch and a remove at one index", base, `scores { index: 0 patch: 3 } scores { index: 0 remove {} }`, ""},
		{"two patches at one index", base, `scores { index: 1 patch: 3 } scores { index: 1 patch: 4 }`, ""},
		{"an insert with no elements", base, `scores { index: 0 insert_before {} }`, ""},
		{"an optional key unset in an add", base, `legacy { entries { key: "f" add { value: 1 } } }`, ""},
		{"an add with an item that has two items for one key", base, `groups { number: 5 add { number: 5 seats { code: "x" } seats { code: "x" } } }`, ""},
		{"accepts an insert of messages", base, `lines { index: 0 insert_before { items { text: "t" } } }`, ""},
		{"accepts a map add", base, `configs { key: 2 add { theme: "u" } }`, ""},
		{"unknown fields in d", base, `members { id: "a" patch { name: "n" } }`, "d"},
		{"unknown fields in a", base + ` settings { theme: "t" }`, ``, "a"},
		{"a keyed list with two items for one key", `members { id: "a" } members { id: "a" }`, ``, ""},
		{"a keyed list with an unset optional key", `legacy { entries { value: 1 } }`, ``, ""},
		{"a nested keyed list with two items for one key", `groups { number: 1 seats { code: "x" } seats { code: "x" } }`, ``, ""},
		{"a nested keyed list with an unset optional key", `members { id: "a" legacy { entries { key: "k" } entries { value: 2 } } }`, ``, ""},
		{"accepts a patch that keeps the key", `members { id: "a" } scores: 1 scores: 2 cash: "c" counts { key: "k" value: 1 }`, `members { id: "a" patch { id: "a" } }`, ""},
		{"accepts inserts and a remove", `members { id: "a" } scores: 1 scores: 2 cash: "c" counts { key: "k" value: 1 }`, `scores { index: 0 insert_before { items: 9 } } scores { index: 0 remove {} } scores { index: 2 insert_before { items: 8 } }`, ""},
		{"accepts a new oneof case and a clear of the old", `members { id: "a" } scores: 1 scores: 2 cash: "c" counts { key: "k" value: 1 }`, `credit { units: 1 } unset: 23`, ""},
		{"accepts map sets", `members { id: "a" } scores: 1 scores: 2 cash: "c" counts { key: "k" value: 1 }`, `counts { key: "k" set: 2 } counts { key: "n" set: 3 }`, ""},
		{"accepts a clear twice", `members { id: "a" } scores: 1 scores: 2 cash: "c" counts { key: "k" value: 1 }`, `unset: 2 unset: 2`, ""},
		{"accepts a patch of a removed oneof case", `card { holder: "h" }`, `card { limit { units: 2 } }`, ""},
		{"accepts a nested map patch", base, `configs { key: 1 patch { extra { key: "x" set: "y" } labels { index: 0 insert_before { items: "l" } } } }`, ""},
		{"accepts a tree patch", `tree { id: "r" children { id: "a" children { id: "b" } } }`, `tree { children { id: "a" patch { children { id: "c" add { id: "c" weight: 1 } } children { id: "b" remove {} } } } }`, ""},
		{"accepts enum keys", `tagged { color: COLOR_RED note: "r" } tagged { color: COLOR_BLUE }`, `tagged { color: COLOR_GREEN add { color: COLOR_GREEN } } tagged { color: COLOR_RED patch { note: "s" } } tagged_order: COLOR_GREEN tagged_order: COLOR_BLUE tagged_order: COLOR_RED`, ""},
		{"accepts indexed messages", `lines { text: "a" } lines { text: "b" } waits { seconds: 1 }`, `lines { index: 1 patch { qty: 2 } } lines { index: 0 remove {} } lines { index: 2 insert_before { items { text: "c" } } } waits { index: 0 patch { nanos: 5 } }`, ""},
		{"accepts legacy and modern fields", `legacy { label: "l" level: LEVEL_HIGH codes: 1 levels: LEVEL_LOW } modern { code: "c" shade: SHADE_DARK plain: 2 stamps: 3 }`, `legacy { label: "m" levels { index: 0 patch: LEVEL_HIGH } codes { codes: 4 } } modern { shade: SHADE_LIGHT plain: 0 stamps { stamps: 5 } }`, ""},
		{"accepts atomic and wrapper fields", `budget { units: 1 } frozen { theme: "f" } note { value: "n" } created { seconds: 3 }`, `budget { nanos: 2 } frozen { color: COLOR_RED } note { value: "m" } created { nanos: 4 } prices { prices { units: 9 } } tags { tags: "t" }`, ""},
		{"accepts wallet changes", `wallet { key: "w" value { units: 1 } }`, `wallet { key: "w" set { units: 2 } } wallet { key: "v" set { nanos: 1 } }`, ""},
	}
	for _, tt := range refusals {
		a, d := rootOf(tt.a), diffOf(tt.d)
		switch tt.unknown {
		case "a":
			pdgUnknown(a.ProtoReflect().Get(a.Descriptor().Fields().ByName("settings")).Message())
		case "d":
			w := d.Get(d.Descriptor().Fields().ByName("members")).List().Get(0).Message()
			pdgUnknown(w.Get(w.Descriptor().Fields().ByName("patch")).Message())
		}
		v := pdgCheck{Name: tt.name, A: enc(a), D: enc(d), Error: errText(apply.Validate(a, d))}
		got, err := apply.Apply(a, d)
		if errText(err) != v.Error {
			t.Fatalf("%s: Apply's error %v differs from Validate's %s", tt.name, err, v.Error)
		}
		if err == nil {
			v.Result = enc(got)
		}
		out.Validate = append(out.Validate, v)
	}

	// apply's TestApply.
	applies := []struct{ name, a, d string }{
		{"an insert comes before its index's element, whatever is done to it", `scores: 1 scores: 2 scores: 3`,
			`scores { index: 1 remove {} } scores { index: 1 insert_before { items: 7 items: 8 } } scores { index: 0 patch: 5 } scores { index: 3 insert_before { items: 9 } }`},
		{"adds go after the kept items in key order, unless the order says otherwise", `members { id: "b" } members { id: "a" }`,
			`members { id: "d" add { id: "d" } } members { id: "c" add { id: "c" } }`},
		{"an order field sorts the list", `members { id: "b" } members { id: "a" }`,
			`members { id: "c" add { id: "c" } } members_order: "c" members_order: "a" members_order: "b"`},
		{"a new oneof case starts from empty", `card { holder: "h" }`, `card { limit { units: 1 } }`},
		{"a different oneof case replaces the oneof", `cash: "c"`, `card { holder: "h" }`},
		{"patching an unset message starts it from empty", ``, `settings { theme: "t" }`},
		{"clearing a oneof case's field clears the case", `cash: "c"`, `unset: 23`},
		{"a map with message values is patched by key", `configs { key: 1 value { theme: "t" } }`,
			`configs { key: 1 patch { color: COLOR_RED } } configs { key: 2 add { theme: "u" } }`},
		{"scalars, atomics and replaced lists are set", `name: "a" count: 1 ratio: 1 weight: 2 blob: "b" flag: true color: COLOR_BLUE budget { units: 1 nanos: 2 } tags: "x" prices { units: 1 }`,
			`name: "b" count: 0 ratio: 0 blob: "" flag: false color: COLOR_UNSPECIFIED budget { currency: "USD" } tags { } prices { prices { units: 2 } prices { units: 3 } } unset: 4`},
		{"a frozen field is atomic", `frozen { theme: "a" labels: "l" }`, `frozen { color: COLOR_GREEN }`},
		{"an unordered keyed list keeps its base order", `groups { number: 3 } groups { number: 1 title: "o" } groups { number: 2 }`,
			`groups { number: 1 patch { title: "p" seats { code: "s" add { code: "s" row: 4 } } } } groups { number: 0 add { number: 0 } } groups { number: 3 remove {} }`},
		{"an ordered nested list is sorted by its order", `groups { number: 1 seats { code: "a" } seats { code: "b" } }`,
			`groups { number: 1 patch { seats { code: "c" add { code: "c" aisle: false } } seats_order: "b" seats_order: "c" seats_order: "a" } }`},
		{"enum keys", `tagged { color: COLOR_RED note: "r" } tagged { color: COLOR_BLUE }`,
			`tagged { color: COLOR_GREEN add { color: COLOR_GREEN } } tagged { color: COLOR_RED patch { note: "s" } } tagged_order: COLOR_GREEN tagged_order: COLOR_BLUE tagged_order: COLOR_RED`},
		{"bool keys of atomic items", `coins { heads: true value: 1 }`, `coins { heads: false add { value: 2 } } coins { heads: true patch { heads: true value: 3 } }`},
		{"indexed message lists", `lines { text: "a" } lines { text: "b" } lines { text: "c" }`,
			`lines { index: 1 patch { qty: 2 } } lines { index: 0 remove {} } lines { index: 3 insert_before { items { text: "d" } items { text: "e" } } } lines { index: 2 insert_before { items { text: "x" } } }`},
		{"indexed lists inside items", `members { id: "a" lines { text: "x" } }`, `members { id: "a" patch { lines { index: 0 insert_before { items { text: "w" } } } lines { index: 0 patch { qty: 1 } } } }`},
		{"indexed well-known types", `waits { seconds: 1 } waits { seconds: 2 }`, `waits { index: 0 patch { nanos: 5 } } waits { index: 1 remove {} }`},
		{"maps of scalars", `counts { key: "a" value: 1 } counts { key: "b" value: 2 }`, `counts { key: "a" remove {} } counts { key: "b" set: 3 } counts { key: "c" set: 0 }`},
		{"maps of atomic messages", `wallet { key: "w" value { units: 1 } }`, `wallet { key: "w" set { units: 2 } } wallet { key: "v" set { nanos: 1 } }`},
		{"nested maps and indexed lists", `settings { labels: "a" labels: "b" extra { key: "x" value: "y" } }`,
			`settings { labels { index: 2 insert_before { items: "c" } } labels { index: 0 patch: "z" } extra { key: "x" remove {} } extra { key: "q" set: "r" } }`},
		{"a recursive keyed list", `tree { id: "r" children { id: "a" children { id: "b" } } }`,
			`tree { weight: 2 children { id: "a" patch { children { id: "c" add { id: "c" weight: 1 } } children { id: "b" remove {} } } } }`},
		{"proto2 and editions fields", `legacy { label: "l" level: LEVEL_HIGH codes: 1 levels: LEVEL_LOW entries { key: "k" value: 1 } } modern { code: "c" shade: SHADE_DARK plain: 2 stamps: 3 }`,
			`legacy { label: "m" levels { index: 0 patch: LEVEL_HIGH } levels { index: 1 insert_before { items: LEVEL_LOW } } codes { codes: 4 } entries { key: "k" patch { value: 2 } } entries { key: "j" add { key: "j" } } unset: 2 } modern { shade: SHADE_LIGHT plain: 0 stamps { stamps: 5 } unset: 1 }`},
		{"wrapper and timestamp fields", `note { value: "n" } created { seconds: 3 }`, `note { value: "m" } created { nanos: 4 }`},
		{"clearing every explicit field", `count: 1 weight: 1 legacy { label: "x" } budget { units: 1 } settings {} created {} note {} card {} tree {} modern {}`,
			`unset: 2 unset: 4 unset: 8 unset: 9 unset: 10 unset: 12 unset: 13 unset: 24 unset: 28 unset: 30`},
	}
	for _, tt := range applies {
		a, d := rootOf(tt.a), diffOf(tt.d)
		got, err := apply.Apply(a, d)
		v := pdgCheck{Name: tt.name, A: enc(a), D: enc(d), Error: errText(err)}
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		v.Result = enc(got)
		out.Apply = append(out.Apply, v)
	}

	// apply's TestDiff and TestDiffRefusesBadInput.
	diffs := []struct{ name, a, b string }{
		{"removed then added elements pair up as patches, and extra adds insert before the next kept element",
			`scores: 1 scores: 2 scores: 3 scores: 4`, `scores: 1 scores: 7 scores: 8 scores: 9 scores: 4`},
		{"extra removes stay removes, and adds at the end insert at n", `scores: 1 scores: 2 scores: 3`, `scores: 9 scores: 3 scores: 4`},
		{"keyed changes are in key order and the order is written only when it differs from the default",
			`members { id: "b" } members { id: "a" name: "A" }`, `members { id: "a" name: "B" } members { id: "c" }`},
		{"a changed order is the complete final order", `members { id: "a" } members { id: "b" }`, `members { id: "b" } members { id: "a" }`},
		{"an UNORDERED list's order is not part of the diff", `groups { number: 1 } groups { number: 2 }`, `groups { number: 2 } groups { number: 1 }`},
		{"an implicit field set to zero is set, not cleared", `name: "x" count: 1`, `count: 0`},
		{"an explicit field that becomes unset is cleared", `count: 1 weight: 1 settings {} created {}`, ``},
		{"a new oneof case is patched from empty", `cash: "c"`, `card {}`},
		{"a oneof case that becomes unset is cleared", `card { holder: "h" }`, ``},
		{"a oneof case that stays is patched", `card { holder: "h" }`, `card { holder: "i" limit { units: 1 } }`},
		{"floats compare by bits", `ratio: nan weight: 0`, `ratio: nan weight: -0`},
		{"maps", `counts { key: "a" value: 1 } counts { key: "b" value: 2 } configs { key: 1 value { theme: "t" } } configs { key: 2 value { } } wallet { key: "w" value { units: 1 } }`,
			`counts { key: "b" value: 3 } counts { key: "c" value: 0 } configs { key: 1 value { theme: "u" } } configs { key: 3 value { color: COLOR_RED } } wallet { key: "w" value { units: 1 } } wallet { key: "x" value { } }`},
		{"indexed messages are patched", `lines { text: "a" } lines { text: "b" } lines { text: "c" }`, `lines { text: "a" qty: 1 } lines { text: "c" } lines { text: "d" } lines { text: "e" }`},
		{"indexed well-known types", `waits { seconds: 1 } waits { seconds: 2 }`, `waits { seconds: 3 } waits { seconds: 2 } waits { nanos: 1 }`},
		{"enum keys and their order", `tagged { color: COLOR_RED } tagged { color: COLOR_BLUE note: "b" }`, `tagged { color: COLOR_BLUE note: "c" } tagged { color: COLOR_GREEN } tagged { color: COLOR_RED }`},
		{"atomic keyed items", `coins { heads: true value: 1 } coins { heads: false }`, `coins { heads: true value: 2 }`},
		{"nested keyed lists", `groups { number: 1 seats { code: "a" row: 1 } seats { code: "b" } } members { id: "m" lines { text: "x" } }`,
			`groups { number: 1 seats { code: "b" aisle: true } seats { code: "a" row: 2 } } members { id: "m" lines { text: "x" } lines { text: "y" } }`},
		{"atomic, replaced and frozen fields", `budget { units: 1 } prices { units: 1 } tags: "a" frozen { theme: "f" }`, `budget { units: 1 nanos: 1 } prices { units: 1 } tags: "a" tags: "b" frozen { theme: "g" }`},
		{"proto2 and editions fields", `legacy { label: "l" level: LEVEL_HIGH codes: 1 levels: LEVEL_LOW entries { key: "k" value: 1 } } modern { code: "c" shade: SHADE_DARK plain: 2 stamps: 3 }`,
			`legacy { level: LEVEL_LOW codes: 2 levels: LEVEL_HIGH levels: LEVEL_LOW entries { key: "k" } entries { key: "j" value: 3 } } modern { shade: SHADE_DARK plain: 0 stamps: 3 stamps: 4 }`},
		{"a recursive tree", `tree { id: "r" children { id: "a" children { id: "b" weight: 1 } } }`, `tree { id: "r" children { id: "a" children { id: "b" weight: 2 } children { id: "c" } } children { id: "d" } }`},
		{"wrappers and timestamps", `note { value: "n" } created { seconds: 3 }`, `note { value: "m" } created { seconds: 3 nanos: 4 }`},
		{"a set from unset", ``, `settings { theme: "t" labels: "l" extra { key: "k" value: "v" } } legacy { entries { key: "e" } } members { id: "a" lines { text: "x" } }`},
	}
	for _, tt := range diffs {
		got := dynamicpb.NewMessage(body)
		err := apply.Diff(rootOf(tt.a), rootOf(tt.b), got)
		v := pdgDiffCase{Name: tt.name, A: enc(rootOf(tt.a)), B: enc(rootOf(tt.b)), Error: errText(err)}
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		v.Diff = enc(got)
		out.Diff = append(out.Diff, v)
	}
	for _, tt := range []struct{ name, a, b string }{
		{"b with two items for one key", ``, `members { id: "a" } members { id: "a" }`},
		{"a with unknown fields", `blob: "x"`, ``},
	} {
		a := rootOf(tt.a)
		if tt.a != "" {
			pdgUnknown(a.ProtoReflect())
		}
		err := apply.Diff(a, rootOf(tt.b), dynamicpb.NewMessage(body))
		out.Diff = append(out.Diff, pdgDiffCase{Name: tt.name, A: enc(a), B: enc(rootOf(tt.b)), Error: errText(err)})
	}

	// apply's TestEqual.
	for _, tt := range []struct {
		x, y string
	}{
		{`groups { number: 1 title: "a" } groups { number: 2 }`, `groups { number: 2 } groups { number: 1 title: "a" }`},
		{`groups { number: 1 title: "a" } groups { number: 2 }`, `groups { number: 2 } groups { number: 1 title: "b" }`},
		{`members { id: "a" } members { id: "b" }`, `members { id: "b" } members { id: "a" }`},
		{`groups { number: 1 seats { code: "x" } seats { code: "y" } }`, `groups { number: 1 seats { code: "y" } seats { code: "x" } }`},
		{`tree { children { id: "a" } children { id: "b" children { id: "c" } children { id: "d" } } }`, `tree { children { id: "b" children { id: "d" } children { id: "c" } } children { id: "a" } }`},
		{`ratio: nan`, `ratio: nan`},
		{`ratio: 0`, `ratio: -0`},
		{`weight: nan`, `weight: nan`},
		{`count: 0`, ``},
		{`name: ""`, ``},
		{`scores: 1 scores: 2`, `scores: 2 scores: 1`},
		{`counts { key: "a" value: 1 } counts { key: "b" value: 2 }`, `counts { key: "b" value: 2 } counts { key: "a" value: 1 }`},
		{`counts { key: "a" value: 1 }`, `counts { key: "a" value: 2 }`},
		{`groups { number: 1 } groups { number: 1 }`, `groups { number: 1 } groups { number: 1 }`},
		{`groups { number: 1 title: "a" } groups { number: 1 }`, `groups { number: 1 } groups { number: 1 title: "a" }`},
		{`blob: "a"`, `blob: "a"`},
		{`blob: "a"`, `blob: "b"`},
		{`cash: ""`, ``},
		{`coins { heads: true } coins { heads: false }`, `coins { heads: false } coins { heads: true }`},
		{`legacy { entries { key: "a" } entries { key: "b" } }`, `legacy { entries { key: "b" } entries { key: "a" } }`},
	} {
		x, y := rootOf(tt.x), rootOf(tt.y)
		out.Equal = append(out.Equal, pdgEqualCase{X: enc(x), Y: enc(y), Equal: apply.Equal(x, y, diffOf(``))})
	}
	{
		x, y := rootOf(`blob: "a"`), rootOf(`blob: "a"`)
		pdgUnknown(x.ProtoReflect())
		out.Equal = append(out.Equal, pdgEqualCase{X: enc(x), Y: enc(y), Equal: apply.Equal(x, y, diffOf(``))})
	}

	// apply's TestOverlap and TestItem.
	items := func(its []apply.Item) []pdgItem {
		var out []pdgItem
		for _, it := range its {
			v := pdgItem{Text: it.String(), Field: int(it.Field()), Path: []pdgStep{}}
			for _, s := range it.Path() {
				st := pdgStep{List: string(s.List()), Index: s.Index()}
				if s.Key().IsValid() {
					st.Key = fmt.Sprint(s.Key().Interface())
				}
				v.Path = append(v.Path, st)
			}
			out = append(out, v)
		}
		return out
	}
	for _, tt := range []struct{ name, d1, d2 string }{
		{"different top-level fields", `name: "a"`, `count: 1`},
		{"one top-level field", `name: "a"`, `name: "b"`},
		{"set and clear of one field", `count: 1`, `unset: 2`},
		{"different subfields of one singular message", `settings { theme: "t" }`, `settings { color: COLOR_RED }`},
		{"clearing a message overlaps changes inside it", `unset: 10`, `settings { theme: "t" }`},
		{"different fields of one keyed item", `members { id: "a" patch { name: "A" } }`, `members { id: "a" patch { color: COLOR_RED } }`},
		{"one field of one keyed item", `members { id: "a" patch { name: "A" } }`, `members { id: "a" patch { name: "B" } }`},
		{"different keyed items", `members { id: "a" remove {} }`, `members { id: "b" remove {} }`},
		{"a remove and a change inside the item", `members { id: "a" remove {} }`, `members { id: "a" patch { lines { index: 0 remove {} } } }`},
		{"two adds of one key", `members { id: "c" add { id: "c" } }`, `members { id: "c" add { id: "c" name: "C" } }`},
		{"nested keyed items", `groups { number: 1 patch { seats { code: "x" patch { row: 1 } } } }`, `groups { number: 1 patch { seats { code: "x" patch { row: 2 } } } }`},
		{"two orders", `members_order: "a"`, `members_order: "a"`},
		{"an order and an item change", `members_order: "a"`, `members { id: "a" patch { name: "A" } }`},
		{"setting an atomic message", `budget { units: 1 }`, `budget { nanos: 1 }`},
		{"replacing a list", `tags { tags: "a" }`, `tags {}`},
		{"different cases of one oneof", `cash: "c"`, `card { holder: "h" }`},
		{"a map key", `counts { key: "k" set: 1 }`, `counts { key: "k" remove {} }`},
		{"patches of one indexed element", `lines { index: 1 patch { text: "a" } }`, `lines { index: 1 patch { qty: 1 } }`},
		{"inserts at one index", `scores { index: 2 insert_before { items: 1 } }`, `scores { index: 2 insert_before { items: 2 } }`},
		{"an insert and a remove at one index", `scores { index: 2 insert_before { items: 1 } }`, `scores { index: 2 remove {} }`},
		{"edits at different indexes", `scores { index: 1 remove {} }`, `scores { index: 2 insert_before { items: 1 } }`},
		{"enum keys", `tagged { color: COLOR_RED remove {} }`, `tagged { color: COLOR_RED patch { note: "n" } }`},
		{"a quoted key", `groups { number: 3 patch { seats { code: "x y" remove {} } } }`, `groups { number: 3 patch { seats { code: "x y" patch {} } } }`},
		{"an empty key", `members { id: "" remove {} }`, `members { id: "" remove {} }`},
		{"a key with quotes", `members { id: "a\"b\\c\n\t\x01é" remove {} }`, `members { id: "a\"b\\c\n\t\x01é" remove {} }`},
		{"a dotted key", `members { id: "a.b-c_d" remove {} }`, `members { id: "a.b-c_d" remove {} }`},
		{"indexed steps", `lines { index: 4 remove {} }`, `lines { index: 4 remove {} }`},
		{"bool keys", `coins { heads: true remove {} }`, `coins { heads: true patch { heads: true } }`},
		{"several items", `name: "a" members { id: "a" patch { name: "A" lines { index: 0 remove {} } } } members { id: "b" remove {} } settings { theme: "t" } unset: 2`,
			`name: "b" members { id: "a" patch { name: "B" lines { index: 0 patch { qty: 1 } } } } members { id: "b" remove {} } settings { labels { index: 0 remove {} } } count: 3`},
		{"changes inside a singular message, both direct", `settings { theme: "t" } legacy { label: "a" }`, `settings { extra { key: "k" set: "v" } theme: "u" } legacy { entries { key: "e" remove {} } }`},
		{"a new oneof case and a patch of another", `card { holder: "h" }`, `credit { units: 1 }`},
		{"nested map items", `configs { key: 1 patch { theme: "a" } }`, `configs { key: 1 remove {} }`},
		{"tree items", `tree { children { id: "a" patch { children { id: "b" patch { weight: 1 } } } } }`, `tree { children { id: "a" patch { children { id: "b" patch { weight: 2 } } } } }`},
	} {
		d1, d2 := diffOf(tt.d1), diffOf(tt.d2)
		got, err := apply.Overlap(d1, d2)
		back, berr := apply.Overlap(d2, d1)
		if err != nil || berr != nil {
			t.Fatalf("%s: %v %v", tt.name, err, berr)
		}
		out.Overlap = append(out.Overlap, pdgOverlapCase{Name: tt.name, D1: enc(d1), D2: enc(d2), Items: items(got), Back: items(back)})
	}
	{
		member := parse("pdgtest.v1.diff.MemberDiff", ``)
		_, err := apply.Overlap(diffOf(``), member)
		out.Overlap = append(out.Overlap, pdgOverlapCase{Name: "diffs of two types", D1: enc(diffOf(``)), D2: enc(member), Error: errText(err)})
	}

	// apply's TestTargetRefusals and TestTargetLeavesItsMessage.
	targets := []struct {
		name, a, d, unknown string
	}{
		{"a repeated key", `members { id: "a" } members { id: "a" }`, ``, ""},
		{"unknown fields", `settings { theme: "t" }`, ``, "a"},
		{"a diff", `name: "r" members { id: "a" name: "Ada" }`, `name: "s" members { id: "a" patch { name: "Bea" } }`, ""},
		{"a refused diff", `name: "r" members { id: "a" name: "Ada" }`, `members { id: "b" patch { name: "Bea" } }`, ""},
	}
	for _, tt := range targets {
		a := rootOf(tt.a)
		if tt.unknown == "a" {
			pdgUnknown(a.Mutable(a.Descriptor().Fields().ByName("settings")).Message())
		}
		d := diffOf(tt.d)
		v := pdgCheck{Name: tt.name, A: enc(a), D: enc(d)}
		target, err := apply.NewTarget(a, body)
		if err != nil {
			v.Error = "NewTarget: " + err.Error()
		} else if next, err := target.Apply(d); err != nil {
			v.Error = "Apply: " + err.Error()
		} else {
			v.Result = enc(next.Message())
		}
		out.Target = append(out.Target, v)
	}
	{
		_, err := (apply.Target{}).Apply(diffOf(``))
		out.Target = append(out.Target, pdgCheck{Name: "the zero Target", Error: "Apply: " + errText(err)})
		target, err := apply.NewTarget(rootOf(``), body)
		if err != nil {
			t.Fatal(err)
		}
		_, err = target.Apply(rootOf(``))
		out.Target = append(out.Target, pdgCheck{Name: "a diff of the wrong type", A: enc(rootOf(``)), D: enc(rootOf(``)), Error: "Apply: " + errText(err)})
	}

	// Diff over every pair of the targets above that Validate accepts.
	var roots []*dynamicpb.Message
	seen := map[string]bool{}
	addRoot := func(b64 string) {
		if b64 == "" || seen[b64] {
			return
		}
		seen[b64] = true
		m := dynamicpb.NewMessage(rootOf(``).Descriptor())
		b := pdgBytes(t, b64)
		if err := proto.Unmarshal(b, m); err != nil {
			t.Fatal(err)
		}
		if len(m.GetUnknown()) > 0 || apply.Validate(m, diffOf(``)) != nil {
			return
		}
		roots = append(roots, m)
		out.Roots = append(out.Roots, b64)
	}
	for _, v := range out.Apply {
		addRoot(v.A)
		addRoot(v.Result)
	}
	for _, v := range out.Diff {
		addRoot(v.A)
		addRoot(v.B)
	}
	for i, a := range roots {
		for j, b := range roots {
			got := dynamicpb.NewMessage(body)
			err := apply.Diff(a, b, got)
			p := pdgPair{A: i, B: j, Error: errText(err)}
			if err == nil {
				p.Diff = enc(got)
				back, err := apply.Apply(a, got)
				if err != nil || !apply.Equal(back, b, got) {
					t.Fatalf("pair %d %d: Diff then Apply doesn't round-trip: %v", i, j, err)
				}
			}
			out.Pairs = append(out.Pairs, p)
		}
	}
	check(t, "protodiffgen/pdgtest.json", out)
}

// pdgBytes decodes a base64 vector field.
func pdgBytes(t *testing.T, b64 string) []byte {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
