package taxyear_test

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"

	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
)

// update rewrites the golden views: `bazel run //taxyear:taxyear_test --
// -test.run=TestBuilderViewGolden -update`.
var update = flag.Bool("update", false, "rewrite testdata/builder")

func readCase(t *testing.T, name string) *scenariosv1.TaxYearScenarios {
	t.Helper()
	text, err := os.ReadFile("testdata/cases/" + name + ".jsonc")
	if err != nil {
		t.Fatal(err)
	}
	f, err := taxyear.Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func view(t *testing.T, f *scenariosv1.TaxYearScenarios, scenario string) *scenariosv1.BuilderView {
	t.Helper()
	r, err := taxyear.Compute(f)
	if err != nil {
		t.Fatal(err)
	}
	v, err := taxyear.BuildView(f, r, scenario)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// TestBuilderViewGolden builds one scenario's view of four households and
// compares it with testdata/builder.
func TestBuilderViewGolden(t *testing.T) {
	for _, c := range []struct{ file, scenario string }{
		{"simple-2026", "plan"},
		{"full-everything", "raise-freelance-stops"},
		{"moved-2026", "raise"},
		{"full-override", "plan"},
	} {
		t.Run(c.file, func(t *testing.T) {
			got := view(t, readCase(t, c.file), c.scenario)
			name := filepath.Join("testdata", "builder", c.file+"."+c.scenario+".textproto")
			if *update {
				dir := os.Getenv("BUILD_WORKSPACE_DIRECTORY")
				if dir == "" {
					t.Fatal("-update runs under bazel run")
				}
				b, err := prototext.MarshalOptions{Multiline: true}.Marshal(got)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "taxyear", name), b, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			text, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			want := &scenariosv1.BuilderView{}
			if err := prototext.Unmarshal(text, want); err != nil {
				t.Fatal(err)
			}
			if !proto.Equal(got, want) {
				g, _ := prototext.MarshalOptions{Multiline: true}.Marshal(got)
				w, _ := prototext.MarshalOptions{Multiline: true}.Marshal(want)
				t.Errorf("the view differs from %s at:\n%s\nRun the -update command in builder_test.go.", name, firstDiff(string(g), string(w)))
			}
		})
	}
}

// firstDiff is a few lines around the first line that differs.
func firstDiff(a, b string) string {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := 0; i < len(al) || i < len(bl); i++ {
		if i >= len(al) || i >= len(bl) || al[i] != bl[i] {
			lo := max(i-3, 0)
			return "got:\n" + strings.Join(al[lo:min(i+3, len(al))], "\n") + "\nwant:\n" + strings.Join(bl[lo:min(i+3, len(bl))], "\n")
		}
	}
	return ""
}

// find returns the item with an id, at any depth.
func find(v *scenariosv1.BuilderView, id string) *scenariosv1.BuilderItem {
	var walk func(items []*scenariosv1.BuilderItem) *scenariosv1.BuilderItem
	walk = func(items []*scenariosv1.BuilderItem) *scenariosv1.BuilderItem {
		for _, it := range items {
			if it.GetId() == id {
				return it
			}
			if c := walk(it.GetChildren()); c != nil {
				return c
			}
		}
		return nil
	}
	for _, g := range v.GetGroups() {
		if it := walk(g.GetItems()); it != nil {
			return it
		}
	}
	return nil
}

// field returns the field at a path, at any depth.
func field(t *testing.T, v *scenariosv1.BuilderView, path string) *scenariosv1.Field {
	t.Helper()
	var walk func(items []*scenariosv1.BuilderItem) *scenariosv1.Field
	walk = func(items []*scenariosv1.BuilderItem) *scenariosv1.Field {
		for _, it := range items {
			for _, f := range it.GetFields() {
				if f.GetPath() == path {
					return f
				}
			}
			if f := walk(it.GetChildren()); f != nil {
				return f
			}
		}
		return nil
	}
	for _, g := range v.GetGroups() {
		if f := walk(g.GetItems()); f != nil {
			return f
		}
	}
	t.Fatalf("no field %s", path)
	return nil
}

func money(minor int64) *scenariosv1.Value {
	return &scenariosv1.Value{Kind: &scenariosv1.Value_Money{Money: &commonv1.Money{Minor: minor, Currency: "USD"}}}
}

var inherit = &scenariosv1.Value{Kind: &scenariosv1.Value_Inherit_{Inherit: &scenariosv1.Value_Inherit{}}}

func edit(t *testing.T, f *scenariosv1.TaxYearScenarios, scenario, path string, v *scenariosv1.Value) *scenariosv1.TaxYearScenarios {
	t.Helper()
	out, err := taxyear.ApplyEdit(f, &scenariosv1.Edit{Scenario: scenario, Path: path, Value: v})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestEditChangesOnlyItsScenario edits Raise in Oct's new pay in "Raise,
// and freelance stops", which is based on it. The edit is in the child's
// changes alone, the field shows Changed here and the new figure, and
// Inherit brings back the raise's own value and inputs.
func TestEditChangesOnlyItsScenario(t *testing.T) {
	const id, path = "raise-freelance-stops", "model.employers[acme].parts[acme-0fjb8x60].pay_rate.paycheck.gross"
	f := readCase(t, "full-everything")
	before := field(t, view(t, f, id), path)
	if before.GetStatus().GetKind() != scenariosv1.StatusKind_STATUS_KIND_CHANGED_IN_PARENT || before.GetStatus().GetChangedIn() != "raise" {
		t.Errorf("before the edit the status is %v, want changed in raise", before.GetStatus())
	}

	edited := edit(t, f, id, path, money(600000))
	for i, s := range f.GetScenarios() {
		if s.GetId() != id && edited.GetScenarios()[i] != s {
			t.Errorf("scenario %s changed", s.GetId())
		}
	}
	if edited.GetShared() != f.GetShared() {
		t.Error("shared changed")
	}
	v := view(t, edited, id)
	after := field(t, v, path)
	if after.GetValue().GetMoney().GetMinor() != 600000 || after.GetStatus().GetKind() != scenariosv1.StatusKind_STATUS_KIND_CHANGED_HERE {
		t.Errorf("after the edit the field is %v with status %v, want $6,000.00 changed here", after.GetValue(), after.GetStatus())
	}
	if find(v, "model.employers[acme]").GetThisScenario().GetAmount().GetMinor() == find(view(t, f, id), "model.employers[acme]").GetThisScenario().GetAmount().GetMinor() {
		t.Error("the job's gross pay for the year didn't move")
	}

	reset := edit(t, edited, id, path, inherit)
	back := field(t, view(t, reset, id), path)
	if back.GetValue().GetMoney().GetMinor() != 583333 || back.GetStatus().GetKind() != scenariosv1.StatusKind_STATUS_KIND_CHANGED_IN_PARENT {
		t.Errorf("after Inherit the field is %v with status %v, want the raise's $5,833.33", back.GetValue(), back.GetStatus())
	}
	r0, _ := taxyear.Compute(f)
	r1, _ := taxyear.Compute(reset)
	in0, _ := r0.Inputs(id)
	in1, _ := r1.Inputs(id)
	if !proto.Equal(in0, in1) {
		t.Error("after Inherit the scenario's inputs differ from the file's")
	}
}

// TestEditOverrideSitsBesideAutomatic overrides Acme's gross pay so far
// from Your plan. The override goes in shared, recording the figure it
// replaces; the field shows both; Inherit removes it.
func TestEditOverrideSitsBesideAutomatic(t *testing.T) {
	const path = "projection.employers[acme].so_far.items.gross"
	f := readCase(t, "full-everything")
	auto := field(t, view(t, f, "plan"), path).GetValue().GetMoney().GetMinor()

	edited := edit(t, f, "raise", path, money(auto+100000))
	if len(edited.GetShared().GetOverrides()) != len(f.GetShared().GetOverrides())+1 {
		t.Fatalf("shared has %d overrides, want one more", len(edited.GetShared().GetOverrides()))
	}
	for i, s := range f.GetScenarios() {
		if edited.GetScenarios()[i] != s {
			t.Errorf("scenario %s changed", s.GetId())
		}
	}
	got := field(t, view(t, edited, "plan"), path)
	if !got.GetOverridden() || got.GetValue().GetMoney().GetMinor() != auto+100000 || got.GetAutomatic().GetMoney().GetMinor() != auto {
		t.Errorf("the field is %v, want overridden to %d beside %d", got, auto+100000, auto)
	}

	removed := field(t, view(t, edit(t, edited, "plan", path, inherit), "plan"), path)
	if removed.GetOverridden() || removed.GetValue().GetMoney().GetMinor() != auto {
		t.Errorf("after Inherit the field is %v, want %d and no override", removed, auto)
	}
}

// TestEditActions sends an Add offer and a question's answer, as the page
// would.
func TestEditActions(t *testing.T) {
	f := readCase(t, "full-everything")
	v := view(t, f, "raise")
	var add *scenariosv1.Action
	for _, a := range find(v, "model.employers[acme]").GetActions() {
		if a.GetLabel() == "Add: one off" {
			add = a
		}
	}
	if add == nil {
		t.Fatal("Acme offers no one-off")
	}
	edited, err := taxyear.ApplyEdit(f, add.GetEdit())
	if err != nil {
		t.Fatal(err)
	}
	if c := find(view(t, edited, "raise"), add.GetEdit().GetPath()); c == nil || c.GetStatus().GetKind() != scenariosv1.StatusKind_STATUS_KIND_CHANGED_HERE {
		t.Errorf("the one-off is %v, want it changed here", c)
	}

	rosa := find(v, "facts.people[rosa]")
	if len(rosa.GetQuestions()) == 0 || len(rosa.GetQuestions()[0].GetAnswers()) != 3 {
		t.Fatalf("Rosa's questions are %v, want answers as edits", rosa.GetQuestions())
	}
	yes := rosa.GetQuestions()[0].GetAnswers()[0]
	edited, err = taxyear.ApplyEdit(f, yes.GetEdit())
	if err != nil {
		t.Fatal(err)
	}
	after := find(view(t, edited, "raise"), "facts.people[rosa]")
	if len(after.GetQuestions()) == 0 || after.GetStatus() != nil {
		t.Errorf("after answering, Rosa is %v", after)
	}
}

// TestEditRefused refuses edits that name nothing, don't fit, or change
// evidence.
func TestEditRefused(t *testing.T) {
	f := readCase(t, "full-everything")
	var known string
	for _, c := range find(view(t, f, "plan"), "model.employers[acme]").GetChildren() {
		if c.GetLabel() == "From paystubs" {
			known = c.GetId()
		}
	}
	text := &scenariosv1.Value{Kind: &scenariosv1.Value_Text{Text: "x"}}
	for _, c := range []struct {
		name, scenario, path string
		value                *scenariosv1.Value
	}{
		{"no scenario", "nope", "model.employers[acme].name", text},
		{"no model", "plan", "model.employers[nope].name", text},
		{"no field", "plan", "model.employers[acme].nope", text},
		{"wrong kind", "plan", "model.employers[acme].first_pay_date", text},
		{"evidence", "raise", known, &scenariosv1.Value{Kind: &scenariosv1.Value_Remove_{Remove: &scenariosv1.Value_Remove{}}}},
		{"not overridable", "plan", "federal.form_1040.tax", money(1)},
		{"no value", "plan", "model.employers[acme].name", nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := taxyear.ApplyEdit(f, &scenariosv1.Edit{Scenario: c.scenario, Path: c.path, Value: c.value}); err == nil {
				t.Error("the edit was made")
			}
		})
	}
}
