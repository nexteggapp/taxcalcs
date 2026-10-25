package taxyear

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/nexteggapp/protodiffgen/apply"

	"github.com/nexteggapp/taxcalcs/money"
	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
)

//go:embed testdata/example.jsonc
var cacheExample string

// uncached is Compute as it was before the cache: the file's checks,
// Resolve, CheckScenarios, then every scenario computed.
func uncached(file *scenariosv1.TaxYearScenarios) *scenariosv1.TaxYearResult {
	out := &scenariosv1.TaxYearResult{SchemaVersion: file.GetSchemaVersion(), TaxYear: file.GetTaxYear(),
		AsOf: file.GetAsOf(), ComputedBy: ComputedBy}
	out.Problems = append(CheckFile(file), CheckIDs(file.GetShared())...)
	if len(out.Problems) > 0 {
		return out
	}
	out.Status = yearStatus(file)
	resolved, problems := Resolve(file)
	compared := CheckScenarios(file, resolved)
	if out.Problems = compared[""]; len(out.Problems) > 0 {
		return out
	}
	byID := map[string]Resolved{}
	for _, r := range resolved {
		byID[r.Scenario().GetId()] = r
	}
	for _, s := range file.GetScenarios() {
		res := &scenariosv1.ScenarioResult{Scenario: s.GetId()}
		res.Problems = append(problems[s.GetId()], compared[s.GetId()]...)
		if r, ok := byID[s.GetId()]; ok && len(res.Problems) == 0 {
			computeScenario(nil, file, r.Inputs(), res)
		}
		out.Scenarios = append(out.Scenarios, res)
	}
	return out
}

// variants are the example with one edit each, in an order that edits a
// scenario, then shared, then breaks a chain, so a cache sees each kind.
var variants = []struct {
	name     string
	old, new string
}{
	{"the example", "", ""},
	{"a raise edited", `"minor": "625000"`, `"minor": "700000"`},
	{"the raise back", "", ""},
	{"shared edited", `"minor": "583333"`, `"minor": "600000"`},
	{"a missing parent", `"parent": "plan"`, `"parent": "nope"`},
	{"a diff that doesn't apply", `"add": { "id": "acme-v2k8n5zd"`, `"add": { "id": "acme-h4c8w2pq"`},
	{"an identifier in shared", `"Aug 14 paystub"`, `"123456789"`},
	{"a second child", `] } } ] } } }
  ]`, `] } } ] } } },
    { "id": "raise2", "name": "Raise again", "parent": "raise" }
  ]`},
	{"the example again", "", ""},
}

func variant(t *testing.T, old, new string) *scenariosv1.TaxYearScenarios {
	t.Helper()
	text := cacheExample
	if old != "" {
		if !strings.Contains(text, old) {
			t.Fatalf("the example has no %q", old)
		}
		text = strings.Replace(text, old, new, 1)
	}
	f, err := Parse([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// TestCacheMatchesUncached computes each variant through caches of two
// sizes, one small enough to drop entries, and through no cache, and wants
// one result.
func TestCacheMatchesUncached(t *testing.T) {
	small, _ := NewCache(1)
	large, _ := NewCache(64)
	for _, v := range variants {
		f := variant(t, v.old, v.new)
		want := uncached(f)
		for name, c := range map[string]*Cache{"no cache": nil, "small": small, "large": large} {
			var got Result
			var err error
			if c == nil {
				got, err = Compute(f)
			} else {
				got, err = c.Compute(f)
			}
			if err != nil {
				t.Fatal(err)
			}
			if !proto.Equal(want, got.Proto()) {
				t.Errorf("%s, %s: the result differs from the uncached one: %s", v.name, name, firstDifference(want, got.Proto()))
			}
		}
	}
}

// TestCacheRecomputesOnlyWhatChanged edits the raise, then shared, and
// checks which scenario results are the cache's earlier ones.
func TestCacheRecomputesOnlyWhatChanged(t *testing.T) {
	c, _ := NewCache(8)
	results := func(f *scenariosv1.TaxYearScenarios) map[string]*scenariosv1.ScenarioResult {
		r, err := c.Compute(f)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]*scenariosv1.ScenarioResult{}
		for _, s := range r.p.GetScenarios() {
			out[s.GetScenario()] = s
		}
		return out
	}
	before := results(variant(t, "", ""))
	raise := results(variant(t, `"minor": "625000"`, `"minor": "700000"`))
	if raise["plan"] != before["plan"] {
		t.Error("an edit to the raise recomputed Your plan")
	}
	if raise["raise"] == before["raise"] {
		t.Error("an edit to the raise did not recompute it")
	}
	shared := results(variant(t, `"minor": "583333"`, `"minor": "600000"`))
	if shared["plan"] == before["plan"] || shared["raise"] == before["raise"] {
		t.Error("an edit to shared did not recompute every scenario")
	}
}

// TestCacheKeepsCopies changes a file after computing it, and wants the
// cache's next result unchanged.
func TestCacheKeepsCopies(t *testing.T) {
	c, _ := NewCache(8)
	f := variant(t, "", "")
	first, _ := c.Compute(f)
	g := proto.Clone(f).(*scenariosv1.TaxYearScenarios)
	f.Shared.Model.Employers[0].Name = "123456789"
	// The raise's added part: apply may keep it rather than copy it.
	added := f.GetScenarios()[1].GetChanges().GetModel().GetEmployers()[0].GetPatch().GetParts()[0].GetAdd()
	added.GetPayRate().GetPaycheck().GetGross().Minor = 1
	again, _ := c.Compute(g)
	if !proto.Equal(first.Proto(), again.Proto()) {
		t.Error("changing a computed file changed the cache's result for its copy")
	}
}

func TestNewCacheRefusesNoSize(t *testing.T) {
	if _, err := NewCache(0); err == nil {
		t.Error("NewCache(0) did not fail")
	}
}

// firstDifference is the first line where two results' text differs.
func firstDifference(want, got proto.Message) string {
	opts := prototext.MarshalOptions{Multiline: true}
	w, g := strings.Split(opts.Format(want), "\n"), strings.Split(opts.Format(got), "\n")
	for i := 0; i < len(w) && i < len(g); i++ {
		if w[i] != g[i] {
			return "line " + strconv.Itoa(i+1) + ": want " + w[i] + ", got " + g[i]
		}
	}
	return "one is longer"
}

// cases parses every case in testdata/cases, and the doc example.
func cases(t *testing.T) map[string]*scenariosv1.TaxYearScenarios {
	t.Helper()
	dir := "testdata/cases"
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]*scenariosv1.TaxYearScenarios{"example": variant(t, "", "")}
	for _, e := range entries {
		text, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if out[e.Name()], err = Parse(text); err != nil {
			t.Fatalf("%s: %v", e.Name(), err)
		}
	}
	return out
}

// TestApplyChangesMatchesApply wants ApplyChanges, which hands apply only
// what a diff touches, to give what apply.Apply gives on the whole parent,
// for every scenario of every case.
func TestApplyChangesMatchesApply(t *testing.T) {
	for name, file := range cases(t) {
		resolved, _ := Resolve(file)
		byID := map[string]*scenariosv1.ScenarioInputs{}
		for _, r := range resolved {
			byID[r.Scenario().GetId()] = r.Inputs()
		}
		for _, r := range resolved {
			parent := file.GetShared()
			if p := r.Scenario().GetParent(); p != "" {
				parent = byID[p]
			}
			changes := r.Scenario().GetChanges()
			if changes == nil {
				changes = &scenariosv1.ScenarioInputsDiff{}
			}
			want, err := apply.Apply(parent, changes)
			if err != nil {
				t.Fatalf("%s %s: %v", name, r.Scenario().GetId(), err)
			}
			if !proto.Equal(want, r.Inputs()) {
				t.Errorf("%s %s: %s", name, r.Scenario().GetId(), firstDifference(want, r.Inputs()))
			}
		}
	}
}

// TestComputeLeavesInputsAlone computes every scenario of every case and
// wants each scenario's resolved inputs unchanged after: they share
// messages with their parent's, and a cache reuses them between calls.
func TestComputeLeavesInputsAlone(t *testing.T) {
	for name, file := range cases(t) {
		resolved, _ := Resolve(file)
		before := map[string]*scenariosv1.ScenarioInputs{}
		for _, r := range resolved {
			before[r.Scenario().GetId()] = clone(r.Inputs())
		}
		for _, r := range resolved {
			computeScenario(nil, file, r.Inputs(), &scenariosv1.ScenarioResult{})
		}
		for _, r := range resolved {
			if !proto.Equal(before[r.Scenario().GetId()], r.Inputs()) {
				t.Errorf("%s: computing changed %s's inputs: %s", name, r.Scenario().GetId(), firstDifference(before[r.Scenario().GetId()], r.Inputs()))
			}
		}
	}
}

func TestKnownKinds(t *testing.T) {
	got, ok := KnownKinds((&scenariosv1.EmployerPart{}).ProtoReflect().Descriptor())
	var names []string
	for _, f := range got {
		names = append(names, string(f.Name()))
	}
	if want := "from_paystubs from_deposits from_w2 ended"; !ok || strings.Join(names, " ") != want {
		t.Errorf("KnownKinds(EmployerPart) = %v, %v; want %s", names, ok, want)
	}
	if got, ok := KnownKinds((&scenariosv1.RetirementPart{}).ProtoReflect().Descriptor()); !ok || len(got) != 1 || got[0].Name() != "from_forms" {
		t.Errorf("KnownKinds(RetirementPart) = %v, %v; want [from_forms], true", got, ok)
	}
	if _, ok := KnownKinds((&scenariosv1.EmployerModel{}).ProtoReflect().Descriptor()); ok {
		t.Error("KnownKinds(EmployerModel) is a part")
	}
}

// TestCanAddPartsAreValid adds each part the full household's projections
// offer, one at a time, to its model in shared, and wants Your plan to
// compute without a problem.
func TestCanAddPartsAreValid(t *testing.T) {
	file := cases(t)["full-2026.jsonc"]
	r, err := Compute(file)
	if err != nil {
		t.Fatal(err)
	}
	proj := r.Message().GetScenarios()[0].GetProjection().ProtoReflect()
	offered := 0
	proj.Range(func(list protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		for i := 0; i < v.List().Len(); i++ {
			p := v.List().Get(i).Message()
			id := p.Get(p.Descriptor().Fields().ByName("id")).String()
			canAdd := p.Get(p.Descriptor().Fields().ByName("can_add")).List()
			for j := 0; j < canAdd.Len(); j++ {
				offered++
				part := canAdd.Get(j).Message()
				if part.Descriptor().Name() == "ItemisedPartChoice" {
					part = part.Get(part.WhichOneof(part.Descriptor().Oneofs().ByName("part"))).Message()
				}
				if problems := withPart(t, file, string(list.Name()), id, part); problems != "" {
					t.Errorf("%s[%s] can_add %s: %s", list.Name(), id, part.WhichOneof(part.Descriptor().Oneofs().ByName("part")).Name(), problems)
				}
			}
		}
		return true
	})
	if offered == 0 {
		t.Error("nothing is offered")
	}
}

// withPart computes the file with a copy of part added to a model in
// shared, and returns Your plan's problems.
func withPart(t *testing.T, file *scenariosv1.TaxYearScenarios, list, id string, part protoreflect.Message) string {
	f := clone(file)
	models := f.Shared.Model.ProtoReflect()
	l := models.Mutable(models.Descriptor().Fields().ByName(protoreflect.Name(list))).List()
	for i := 0; i < l.Len(); i++ {
		m := l.Get(i).Message()
		if m.Get(m.Descriptor().Fields().ByName("id")).String() != id {
			continue
		}
		if kind := m.Descriptor().Oneofs().ByName("kind"); kind != nil {
			m = m.Mutable(m.WhichOneof(kind)).Message()
		}
		added := proto.Clone(part.Interface()).ProtoReflect()
		added.Set(added.Descriptor().Fields().ByName("id"), protoreflect.ValueOfString(id+"-added"))
		m.Mutable(m.Descriptor().Fields().ByName("parts")).List().Append(protoreflect.ValueOfMessage(added))
	}
	r, err := Compute(f)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, p := range append(r.Message().GetProblems(), r.Message().GetScenarios()[0].GetProblems()...) {
		out = append(out, p.GetPath()+": "+p.GetMessage().GetText())
	}
	return strings.Join(out, "; ")
}

// TestWorkingsAddUp wants every working's terms, in every scenario of
// every case, to add up to its figure: a path term by its amount, an item
// term by the item's amount of the same figure.
func TestWorkingsAddUp(t *testing.T) {
	for name, file := range cases(t) {
		r, err := Compute(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range r.Message().GetScenarios() {
			w := newWorkings(s)
			items := map[string]*scenariosv1.Item{}
			for _, list := range allItems(s.GetProjection()) {
				for _, it := range list {
					items[it.GetId()] = it
				}
			}
			for _, x := range s.GetWorking() {
				if len(x.GetTerms()) == 0 {
					continue
				}
				total, ok := w.amount(x.GetPath())
				if !ok {
					t.Errorf("%s %s: %s has a working and no amount", name, s.GetScenario(), x.GetPath())
					continue
				}
				var sum money.USCents
				for _, term := range x.GetTerms() {
					v, ok := w.amount(term.GetPath())
					if term.GetItem() != "" {
						v, ok = itemAmount(items[term.GetItem()], x.GetPath())
					}
					if !ok {
						t.Errorf("%s %s: %s's term %v has no amount", name, s.GetScenario(), x.GetPath(), term)
					}
					sum += money.USCents(term.GetSign()) * v
				}
				if sum != total {
					t.Errorf("%s %s: %s is %v and its terms add up to %v", name, s.GetScenario(), x.GetPath(), total, sum)
				}
			}
		}
	}
}

// itemAmount is an item's amount of the figure a working's path names:
// for ".items.gross" the item's gross, and for a total that is itself
// money, its amount.
func itemAmount(it *scenariosv1.Item, path string) (money.USCents, bool) {
	if it == nil {
		return 0, false
	}
	m := it.ProtoReflect()
	figures := m.Get(m.WhichOneof(m.Descriptor().Oneofs().ByName("figures"))).Message()
	field := "amount"
	if _, f, ok := strings.Cut(path, ".items."); ok {
		field = f
	}
	fd := figures.Descriptor().Fields().ByName(protoreflect.Name(field))
	if fd == nil || !figures.Has(fd) {
		return 0, false
	}
	return money.USCents(figures.Get(fd).Message().Interface().(*commonv1.Money).GetMinor()), true
}

// TestApplyChangesPrunesLists checks ApplyChanges against apply.Apply where
// the pruning has the most to put back: a patched model beside one left
// alone, a removed part, a patch that moves a part to another oneof case,
// an added part and an added model.
func TestApplyChangesPrunesLists(t *testing.T) {
	parent := &scenariosv1.ScenarioInputs{}
	changes := &scenariosv1.ScenarioInputsDiff{}
	for m, text := range map[proto.Message]string{parent: `model {
		employers { id: "acme" name: "Acme"
			parts { id: "a" pay_rate { paycheck { gross { minor: 100 currency: "USD" } } } }
			parts { id: "b" pay_rate { paycheck { gross { minor: 200 currency: "USD" } } } }
			parts { id: "c" one_off { amount { minor: 300 currency: "USD" } } } }
		employers { id: "beta" name: "Beta" parts { id: "x" one_off { amount { minor: 1 currency: "USD" } } } } }
		overrides { path: "federal.form_1040.tax" amount { minor: 5 currency: "USD" } }`,
		changes: `model {
		employers { id: "acme" patch { name: "Acme 2"
			parts { id: "a" remove {} }
			parts { id: "b" patch { one_off { amount { minor: 5 currency: "USD" } } } }
			parts { id: "d" add { id: "d" one_off { amount { minor: 7 currency: "USD" } } } } } }
		employers { id: "gamma" add { id: "gamma" name: "Gamma" } } }`} {
		if err := prototext.Unmarshal([]byte(text), m); err != nil {
			t.Fatal(err)
		}
	}
	before := clone(parent)
	want, err := apply.Apply(parent, changes)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ApplyChanges(parent, changes)
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(want, got) {
		t.Errorf("ApplyChanges differs from apply.Apply: %s", firstDifference(want, got))
	}
	if !proto.Equal(before, parent) {
		t.Error("ApplyChanges changed the parent")
	}
	if got.GetModel().GetEmployers()[1] != parent.GetModel().GetEmployers()[1] {
		t.Error("the model the changes leave alone is a copy, not shared")
	}
	if got.GetModel().GetEmployers()[0].GetParts()[1] != parent.GetModel().GetEmployers()[0].GetParts()[2] {
		t.Error("the part the changes leave alone is a copy, not shared")
	}
}

// TestCacheKeepsModelsAnEditLeaves edits the raise's new pay on the full
// household, and wants the raise's business, charity and broker
// projections to be the ones the cache finished before the edit, and its
// job's to be new.
func TestCacheKeepsModelsAnEditLeaves(t *testing.T) {
	text, err := os.ReadFile("testdata/cases/full-2026.jsonc")
	if err != nil {
		t.Fatal(err)
	}
	c, _ := NewCache(16)
	raise := func(gross string) *scenariosv1.ScenarioResult {
		s := string(text)
		at := strings.Index(s, `"id": "raise"`)
		s = s[:at] + strings.Replace(s[at:], `"gross": { "minor": "583333"`, `"gross": { "minor": "`+gross+`"`, 1)
		f, err := Parse([]byte(s))
		if err != nil {
			t.Fatal(err)
		}
		r, err := c.Compute(f)
		if err != nil {
			t.Fatal(err)
		}
		res, _ := r.Scenario("raise")
		return res.p
	}
	before, after := raise("583333"), raise("600000")
	if before.GetProjection().GetBusinesses()[0] != after.GetProjection().GetBusinesses()[0] ||
		before.GetProjection().GetItemised()[0] != after.GetProjection().GetItemised()[0] ||
		before.GetProjection().GetBrokers()[0] != after.GetProjection().GetBrokers()[0] {
		t.Error("an edit to the raise's pay re-expanded a model it leaves alone")
	}
	if before.GetProjection().GetEmployers()[0] == after.GetProjection().GetEmployers()[0] {
		t.Error("an edit to the raise's pay kept the job's old projection")
	}
}

// TestFiguresAreWholeDollars wants every amount in every scenario's result
// in whole dollars, as displayed, so a total of displayed parts is the
// total displayed: rounding happens once, at each source. A working's exact
// figure, before rounding, is the exception.
func TestFiguresAreWholeDollars(t *testing.T) {
	for name, file := range cases(t) {
		r, err := Compute(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range r.Message().GetScenarios() {
			s = clone(s)
			for _, w := range s.GetWorking() {
				w.Exact = nil
			}
			// can_add holds parts to add, inputs for the household to
			// change, not figures.
			for _, e := range s.GetProjection().GetEmployers() {
				e.CanAdd = nil
			}
			seen := map[string]bool{}
			wholeDollars(s.ProtoReflect(), "", func(path string, minor int64) {
				if key := pathShape(path); !seen[key] {
					seen[key] = true
					t.Errorf("%s %s: %s is %d cents, not whole dollars", name, s.GetScenario(), path, minor)
				}
			})
		}
	}
}

// wholeDollars calls bad with every Money under m that holds cents.
func wholeDollars(m protoreflect.Message, at string, bad func(string, int64)) {
	if money, ok := m.Interface().(*commonv1.Money); ok {
		if money.GetMinor()%100 != 0 {
			bad(at, money.GetMinor())
		}
		return
	}
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		switch {
		case fd.Message() == nil || fd.IsMap():
		case fd.IsList():
			for i := 0; i < v.List().Len(); i++ {
				wholeDollars(v.List().Get(i).Message(), fmt.Sprintf("%s.%s[%d]", at, fd.Name(), i), bad)
			}
		default:
			wholeDollars(v.Message(), at+"."+string(fd.Name()), bad)
		}
		return true
	})
}

// pathShape is a path with its indexes left out, so a failure is reported
// once a figure.
func pathShape(path string) string {
	var b strings.Builder
	skip := false
	for _, r := range path {
		switch {
		case r == '[':
			skip = true
		case r == ']':
			skip = false
		case !skip:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// TestSummariesAddUp checks the summary figures on the full
// household against the figures they add: wages against the jobs, what's
// credited against withholding and payments, the balance against the tax,
// and state and local tax against Schedule A.
func TestSummariesAddUp(t *testing.T) {
	r, err := Compute(cases(t)["full-2026.jsonc"])
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range r.Message().GetScenarios() {
		sum := s.GetSummary()
		var gross int64
		for _, e := range s.GetProjection().GetEmployers() {
			gross += e.GetYear().GetItems().GetGross().GetMinor()
		}
		if sum.GetWagesGross().GetMinor() != gross || gross == 0 {
			t.Errorf("%s: wages %v, want the jobs' %d", s.GetScenario(), sum.GetWagesGross(), gross)
		}
		salt := sum.GetSalt()
		sa := s.GetFederal().GetScheduleA()
		if got := salt.GetWithholding().GetMinor() + salt.GetEstimates().GetMinor() + salt.GetLastYearBalance().GetMinor(); got != sa.GetStateLocalIncomeOrSalesTaxes().GetMinor() {
			t.Errorf("%s: state tax paid by where adds to %d, want Schedule A's %v", s.GetScenario(), got, sa.GetStateLocalIncomeOrSalesTaxes())
		}
		if sum.GetStandardDeduction().GetMinor() == 0 || len(sum.GetRetirement()) == 0 {
			t.Errorf("%s: summary %v, want a standard deduction and each saver", s.GetScenario(), sum)
		}
		for _, e := range s.GetEstimated() {
			x := e.GetSummary()
			// The amount with the return is the tax less what is credited,
			// plus what is applied to next year, each set.
			if x.GetAppliedToNextYear() == nil || x.GetCredited() == nil {
				t.Errorf("%s %s: applied %v, credited %v, want both set", s.GetScenario(), e.GetJurisdiction(), x.GetAppliedToNextYear(), x.GetCredited())
			}
			// Payments to come are planned, not paid: estimated is what was
			// made.
			if x.GetEstimated().GetMinor() != x.GetPaid().GetMinor() {
				t.Errorf("%s %s: estimated %v, want the payments made, %v", s.GetScenario(), e.GetJurisdiction(), x.GetEstimated(), x.GetPaid())
			}
			if e.GetJurisdiction() != "US" {
				continue
			}
			f := s.GetFederal().GetForm_1040()
			if x.GetCredited().GetMinor() != f.GetTotalPayments().GetMinor() {
				t.Errorf("%s: credited %v, want the return's payments %v", s.GetScenario(), x.GetCredited(), f.GetTotalPayments())
			}
			if x.GetWithReturn().GetMinor() != f.GetAmountOwed().GetMinor()-f.GetOverpaid().GetMinor() {
				t.Errorf("%s: with the return %v, want owed %v less overpaid %v", s.GetScenario(), x.GetWithReturn(), f.GetAmountOwed(), f.GetOverpaid())
			}
		}
	}
}

// TestSpreadPaymentsAreNotPaid: in the full household's "leaves", the tax
// paid model's SpreadTheRest suggests payments for the due dates ahead.
// They are planned, not paid, so the return's line 26 counts only the
// payments made.
func TestSpreadPaymentsAreNotPaid(t *testing.T) {
	r, err := Compute(cases(t)["full-2026.jsonc"])
	if err != nil {
		t.Fatal(err)
	}
	leaves, _ := r.Scenario("leaves")
	s := leaves.p
	var made, ahead int64
	for _, tp := range s.GetProjection().GetTaxPaid() {
		if tp.GetJurisdiction() != "US" {
			continue
		}
		made, ahead = tp.GetSoFar().GetCounted().GetMinor(), tp.GetRest().GetCounted().GetMinor()
	}
	if ahead == 0 {
		t.Fatal("leaves suggests no payments ahead; the test needs some")
	}
	if got := s.GetFederal().GetForm_1040().GetEstimatedTaxPayments().GetMinor(); got != made {
		t.Errorf("line 26 is %d, want the payments made, %d, not the %d suggested", got, made, ahead)
	}
}

// TestSpouseHasARetirementEntry: a married couple filing jointly where
// only you save. Sam, the spouse, has no retirement model and no date of
// birth, and still has an entry: the plan limit without a catch-up, and
// a question for the date of birth at it.
func TestSpouseHasARetirementEntry(t *testing.T) {
	f := variant(t, `"facts": { "taxYear": 2026 },`, `"facts": { "taxYear": 2026,
      "people": [ { "id": "you", "relationship": "RELATIONSHIP_YOU", "dateOfBirth": { "year": 1980, "month": 1, "day": 1 } },
                  { "id": "sam", "name": "Sam", "relationship": "RELATIONSHIP_SPOUSE" } ],
      "elections": [ { "id": "filing-status", "filingStatus": "FILING_STATUS_MARRIED_FILING_JOINTLY" } ] },`)
	r, err := Compute(f)
	if err != nil {
		t.Fatal(err)
	}
	plan, _ := r.Scenario("plan")
	s := plan.p
	if p := s.GetProblems(); len(p) > 0 {
		t.Fatalf("problems %v", p)
	}
	byPerson := map[string]*scenariosv1.RetirementSummary{}
	for _, x := range s.GetSummary().GetRetirement() {
		byPerson[x.GetPerson()] = x
	}
	for _, id := range []string{"you", "sam"} {
		if got := byPerson[id].GetPlanLimit().GetMinor(); got != 2_450_000 {
			t.Errorf("%s's plan limit %d, want $24,500", id, got)
		}
	}
	var asked bool
	for _, q := range s.GetQuestions() {
		if q.GetId() == "retirement/sam/date-of-birth" && q.GetAt() == "summary.retirement[sam].plan_limit" {
			asked = true
		}
	}
	if !asked {
		t.Errorf("no question for Sam's date of birth at the plan limit: %v", s.GetQuestions())
	}
}

// TestSpouseBeforeTheirEntry: filing jointly before the spouse's entry is in
// the facts. The return still has a spouse, named "spouse", with a retirement
// entry: nothing deducted, the plan limit without a catch-up, and the date of
// birth asked.
func TestSpouseBeforeTheirEntry(t *testing.T) {
	f := variant(t, `"facts": { "taxYear": 2026 },`, `"facts": { "taxYear": 2026,
      "elections": [ { "id": "filing-status", "filingStatus": "FILING_STATUS_MARRIED_FILING_JOINTLY" } ] },`)
	r, err := Compute(f)
	if err != nil {
		t.Fatal(err)
	}
	plan, _ := r.Scenario("plan")
	var entry *scenariosv1.RetirementSummary
	for _, x := range plan.p.GetSummary().GetRetirement() {
		if x.GetPerson() == "spouse" {
			entry = x
		}
	}
	if entry == nil || entry.GetDeducted() == nil || entry.GetDeducted().GetMinor() != 0 || entry.GetPlanLimit().GetMinor() != 2_450_000 {
		t.Fatalf("the spouse's entry %v, want $0 deducted and a $24,500 plan limit", entry)
	}
	var asked bool
	for _, q := range plan.p.GetQuestions() {
		asked = asked || q.GetAt() == "summary.retirement[spouse].plan_limit"
	}
	if !asked {
		t.Error("no question for the spouse's date of birth")
	}
}

// TestBalanceWithoutAModel: full-2025 has no California tax paid model,
// and paid California's $371 balance in April 2026. The payment is listed
// under California and leaves nothing to pay with the return, without
// counting toward the year's installments.
func TestBalanceWithoutAModel(t *testing.T) {
	r, err := Compute(cases(t)["full-2025.jsonc"])
	if err != nil {
		t.Fatal(err)
	}
	plan, _ := r.Scenario("plan")
	s := plan.p
	var found bool
	for _, tp := range s.GetProjection().GetTaxPaid() {
		if tp.GetJurisdiction() != "CA" {
			continue
		}
		for _, it := range tp.GetItems() {
			p := it.GetPayment()
			if p.GetPurpose().GetBalanceFor() == 2025 && p.GetAmount().GetMinor() == 37100 && !p.GetCounts() {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("no uncounted $371 California balance payment under tax paid")
	}
	for _, e := range s.GetEstimated() {
		if e.GetJurisdiction() != "CA" {
			continue
		}
		sum := e.GetSummary()
		if sum.GetLeftWithReturn() == nil || sum.GetLeftWithReturn().GetMinor() != 0 || sum.GetPaid().GetMinor() != 0 {
			t.Errorf("California left with the return %v, paid %v; want $0 and $0", sum.GetLeftWithReturn(), sum.GetPaid())
		}
	}
}

// TestHsaExcessReachesTheReturn: $3,000 a paycheck to an HSA, self-only.
// The two paychecks made hold $6,000 against a $4,400 limit, so the 22 to
// come lose all of theirs to wages and $1,600 made is other income on
// Schedule 1; the excess's working adds up.
func TestHsaExcessReachesTheReturn(t *testing.T) {
	text := strings.Replace(cacheExample, `"facts": { "taxYear": 2026 }`, `"facts": { "taxYear": 2026, "people": [ { "id": "you",
		"relationship": "RELATIONSHIP_YOU", "dateOfBirth": { "year": 1980, "month": 1, "day": 1 }, "hsaCoverage": "HSA_COVERAGE_SELF_ONLY" } ] }`, 1)
	text = strings.Replace(text, `"thisPaycheck": { "gross": { "minor": "583333", "currency": "USD" },`,
		`"thisPaycheck": { "gross": { "minor": "583333", "currency": "USD" }, "pretax": [ { "kind": "PRETAX_KIND_HSA", "amount": { "minor": "300000", "currency": "USD" } } ],`, 1)
	f, err := Parse([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	r, err := Compute(f)
	if err != nil {
		t.Fatal(err)
	}
	s := r.Message().GetScenarios()[0]
	if p := append(r.Message().GetProblems(), s.GetProblems()...); len(p) > 0 {
		t.Fatalf("problems %v", p)
	}
	e := s.GetProjection().GetEmployers()[0]
	if e.GetHsaOverInIncome().GetMinor() != 160_000 || e.GetHsaOver().GetMinor() != 6_760_000 {
		t.Errorf("over %v, in income %v; want $67,600 and $1,600", e.GetHsaOver(), e.GetHsaOverInIncome())
	}
	if got := s.GetFederal().GetSchedule_1().GetTotalOtherIncome().GetMinor(); got != 160_000 {
		t.Errorf("Schedule 1 other income is %d, want the $1,600 made", got)
	}
	w := newWorkings(s)
	for _, x := range s.GetWorking() {
		if x.GetPath() != "projection.employers[acme].hsa_over" {
			continue
		}
		var sum money.USCents
		for _, term := range x.GetTerms() {
			v, _ := w.amount(term.GetPath())
			sum += v
		}
		if total, _ := w.amount(x.GetPath()); sum != total || total == 0 {
			t.Errorf("hsa_over is %v and its terms add to %v", total, sum)
		}
		return
	}
	t.Error("hsa_over has no working")
}

// TestHsaOnAPayChange: $1,000 a paycheck to an HSA, self-only, typed on a
// pay change from Oct 1 rather than a paystub. The six paychecks put in
// $6,000 against a $4,400 limit, so $1,600 stays in box 1: box 1 is gross
// less the pre-tax lines, the HSA's being the $4,400 counted, and the HSA
// asked is that and the $1,600 in wages.
func TestHsaOnAPayChange(t *testing.T) {
	text := strings.Replace(cacheExample, `"facts": { "taxYear": 2026 }`, `"facts": { "taxYear": 2026, "people": [ { "id": "you",
		"relationship": "RELATIONSHIP_YOU", "dateOfBirth": { "year": 1980, "month": 1, "day": 1 }, "hsaCoverage": "HSA_COVERAGE_SELF_ONLY" } ] }`, 1)
	rate := `{ "id": "acme-hsa-rate", "payRate": { "from": { "year": 2026, "month": 10, "day": 1 },
            "paycheck": { "gross": { "minor": "583333", "currency": "USD" }, "pretax": [ { "kind": "PRETAX_KIND_HSA", "amount": { "minor": "100000", "currency": "USD" } } ] } } },
          { "id": "acme-b6n3x8tf", "fromDeposits"`
	if !strings.Contains(text, `{ "id": "acme-b6n3x8tf", "fromDeposits"`) {
		t.Fatal("the example has no Acme deposits part")
	}
	text = strings.Replace(text, `{ "id": "acme-b6n3x8tf", "fromDeposits"`, rate, 1)
	f, err := Parse([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	s := mustPlan(t, f)
	e := s.GetProjection().GetEmployers()[0]
	y := e.GetYear()
	var hsa, pretax int64
	for _, x := range y.GetItems().GetPretax() {
		pretax += x.GetAmount().GetMinor()
		if x.GetKind().String() == "PRETAX_KIND_HSA" {
			hsa += x.GetAmount().GetMinor()
		}
	}
	if e.GetHsaOverInWages().GetMinor() != 160_000 || hsa != 440_000 {
		t.Errorf("HSA counted %d and over in wages %v, want $4,400 and $1,600", hsa, e.GetHsaOverInWages())
	}
	if got, want := y.GetBoxes().GetWages().GetMinor(), y.GetItems().GetGross().GetMinor()-pretax; got != want {
		t.Errorf("box 1 %d, want gross less the pre-tax lines, %d", got, want)
	}
}

// TestNoHsaPlanHoldsNothing: $100 a paycheck to an HSA through payroll,
// but no HSA-eligible plan. The limit is nothing, so the paychecks to come
// put none of theirs aside, and what was made is other income.
func TestNoHsaPlanHoldsNothing(t *testing.T) {
	text := strings.Replace(cacheExample, `"facts": { "taxYear": 2026 }`, `"facts": { "taxYear": 2026, "people": [ { "id": "you",
		"relationship": "RELATIONSHIP_YOU", "dateOfBirth": { "year": 1980, "month": 1, "day": 1 }, "hsaCoverage": "HSA_COVERAGE_NONE" } ] }`, 1)
	text = strings.Replace(text, `"thisPaycheck": { "gross": { "minor": "583333", "currency": "USD" },`,
		`"thisPaycheck": { "gross": { "minor": "583333", "currency": "USD" }, "pretax": [ { "kind": "PRETAX_KIND_HSA", "amount": { "minor": "10000", "currency": "USD" } } ],`, 1)
	f, err := Parse([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	s := mustPlan(t, f)
	e := s.GetProjection().GetEmployers()[0]
	var hsa int64
	for _, x := range e.GetYear().GetItems().GetPretax() {
		if x.GetKind().String() == "PRETAX_KIND_HSA" {
			hsa += x.GetAmount().GetMinor()
		}
	}
	if e.GetHsaOver().GetMinor() != 240_000 || e.GetHsaOverInIncome().GetMinor() != 20_000 {
		t.Errorf("over %v, in income %v, want all $2,400 over and the $200 made in income", e.GetHsaOver(), e.GetHsaOverInIncome())
	}
	if hsa != 20_000 {
		t.Errorf("HSA through payroll counted %d, want the $200 made, the rest cut", hsa)
	}
}
