package taxyear

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/nexteggapp/protodiffgen/apply"

	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
)

// ApplyEdit returns the file with one edit made, the change a builder page
// sends. It computes the file before and after, so it is [Cache.ApplyEdit]
// with a new cache.
func ApplyEdit(file *scenariosv1.TaxYearScenarios, e *scenariosv1.Edit) (*scenariosv1.TaxYearScenarios, error) {
	c, _ := NewCache(len(file.GetScenarios()) + 1)
	return c.ApplyEdit(file, e)
}

// ApplyEdit returns the file with one edit made, reusing the cache's work.
//
// An edit of a path in the inputs puts the value in the scenario's resolved
// inputs and writes the scenario's changes again as the diff from its
// parent's, so the parent is never touched. Inherit takes the parent's value.
//
// An edit of a figure in the result is an override, kept beside the
// computed figure, which it records as `was`. A figure so far or last
// year's figure corrects a fact, so its override goes in shared, for every
// scenario; a state's tax for the year goes in the scenario's changes.
//
// The edit is refused when its scenario doesn't resolve, its path names
// nothing, its value doesn't fit the field, or the file it makes has a
// problem the scenario didn't have before. The result shares messages with
// file, and neither may be changed while the other is in use.
func (c *Cache) ApplyEdit(file *scenariosv1.TaxYearScenarios, e *scenariosv1.Edit) (*scenariosv1.TaxYearScenarios, error) {
	if file == nil || e == nil {
		return nil, errors.New("taxyear: no file or no edit")
	}
	idx := -1
	for i, s := range file.GetScenarios() {
		if s.GetId() == e.GetScenario() {
			idx = i
		}
	}
	if idx < 0 {
		return nil, fmt.Errorf("taxyear: no scenario %q", e.GetScenario())
	}
	p, err := ParsePath(e.GetPath())
	if err != nil {
		return nil, err
	}
	if e.GetValue().GetKind() == nil {
		return nil, errors.New("taxyear: an edit needs a value")
	}
	before, err := c.Compute(file)
	if err != nil {
		return nil, err
	}
	if len(before.Problems()) > 0 {
		return nil, fmt.Errorf("taxyear: the file has problems: %s", before.Problems()[0].GetMessage().GetText())
	}
	in, ok := before.Inputs(e.GetScenario())
	if !ok {
		return nil, fmt.Errorf("taxyear: scenario %q doesn't resolve", e.GetScenario())
	}
	parent := file.GetShared()
	if idx > 0 {
		parent, _ = before.Inputs(file.GetScenarios()[idx].GetParent())
	}
	if parent == nil {
		parent = &scenariosv1.ScenarioInputs{}
	}
	out := &scenariosv1.TaxYearScenarios{SchemaVersion: file.GetSchemaVersion(), TaxYear: file.GetTaxYear(), AsOf: file.GetAsOf(),
		ExportedFrom: file.GetExportedFrom(), Shared: file.GetShared(), Scenarios: append([]*scenariosv1.Scenario(nil), file.GetScenarios()...)}

	var child *scenariosv1.ScenarioInputs
	switch {
	case OverridablePath(e.GetPath()):
		ov, err := overrideFor(before, e, p)
		if err != nil {
			return nil, err
		}
		if !strings.HasSuffix(e.GetPath(), "].tax_for_year") {
			sh := file.GetShared()
			out.Shared = &scenariosv1.ScenarioInputs{Facts: sh.GetFacts(), Model: sh.GetModel(), Overrides: withOverride(sh.GetOverrides(), e.GetPath(), ov)}
			break
		}
		if isReset(e.GetValue()) {
			ov = findOverride(parent.GetOverrides(), e.GetPath())
		}
		child = &scenariosv1.ScenarioInputs{Facts: in.GetFacts(), Model: in.GetModel(), Overrides: withOverride(in.GetOverrides(), e.GetPath(), ov)}
	case p.steps[0].field == "facts" || p.steps[0].field == "model":
		for k := range knownParts(in) {
			if e.GetPath() == k || strings.HasPrefix(e.GetPath(), k+".") {
				return nil, fmt.Errorf("taxyear: %s is evidence, which no scenario changes", k)
			}
		}
		child = clone(in)
		if err := setAt(child.ProtoReflect(), parent.ProtoReflect(), p.steps, e.GetValue()); err != nil {
			return nil, fmt.Errorf("taxyear: %s: %w", e.GetPath(), err)
		}
	default:
		return nil, fmt.Errorf("taxyear: %s is neither a path in the inputs nor a figure that can be overridden", e.GetPath())
	}
	if child != nil {
		diff := &scenariosv1.ScenarioInputsDiff{}
		if err := apply.Diff(parent, child, diff); err != nil {
			return nil, fmt.Errorf("taxyear: %s: %w", e.GetPath(), err)
		}
		s := file.GetScenarios()[idx]
		out.Scenarios[idx] = &scenariosv1.Scenario{Id: s.GetId(), Name: s.GetName(), Parent: s.GetParent(), Changes: diff}
	}
	if err := c.checkEdit(before, out, e.GetScenario()); err != nil {
		return nil, err
	}
	return out, nil
}

// ApplyEdits makes each edit in turn, and stops at the first refused.
func (c *Cache) ApplyEdits(file *scenariosv1.TaxYearScenarios, edits []*scenariosv1.Edit) (*scenariosv1.TaxYearScenarios, error) {
	for _, e := range edits {
		var err error
		if file, err = c.ApplyEdit(file, e); err != nil {
			return nil, err
		}
	}
	return file, nil
}

// checkEdit computes the edited file and refuses it if the file, or the
// edited scenario, has a problem it didn't have before.
func (c *Cache) checkEdit(before Result, out *scenariosv1.TaxYearScenarios, id string) error {
	after, err := c.Compute(out)
	if err != nil {
		return err
	}
	if p := after.Problems(); len(p) > 0 {
		return fmt.Errorf("taxyear: the edit leaves the file with a problem at %s: %s", p[0].GetPath(), p[0].GetMessage().GetText())
	}
	had := map[string]bool{}
	if s, ok := before.Scenario(id); ok {
		for _, p := range s.p.GetProblems() {
			had[p.GetPath()+"\x00"+p.GetMessage().GetText()] = true
		}
	}
	s, _ := after.Scenario(id)
	for _, p := range s.p.GetProblems() {
		if !had[p.GetPath()+"\x00"+p.GetMessage().GetText()] {
			return fmt.Errorf("taxyear: the edit leaves scenario %q with a problem at %s: %s", id, p.GetPath(), p.GetMessage().GetText())
		}
	}
	return nil
}

func isReset(v *scenariosv1.Value) bool {
	return v.GetInherit() != nil || v.GetRemove() != nil
}

// overrideFor is the override an edit of a figure makes, recording the
// figure computed now and its sources, or nil to remove one.
func overrideFor(r Result, e *scenariosv1.Edit, p Path) (*scenariosv1.Override, error) {
	if isReset(e.GetValue()) {
		return nil, nil
	}
	amount := e.GetValue().GetMoney()
	if amount == nil {
		return nil, fmt.Errorf("taxyear: %s is overridden with an amount", e.GetPath())
	}
	s, ok := r.Scenario(e.GetScenario())
	if !ok {
		return nil, fmt.Errorf("taxyear: no result for scenario %q", e.GetScenario())
	}
	at := p
	if strings.HasSuffix(e.GetPath(), ".so_far.items.amount") {
		at, _ = ParsePath(strings.TrimSuffix(e.GetPath(), ".amount"))
	}
	f, ok := s.Figure(at)
	auto, isAmount := f.Amount()
	if !ok || !isAmount {
		return nil, fmt.Errorf("taxyear: %s names no amount in scenario %q", e.GetPath(), e.GetScenario())
	}
	var sources []string
	for _, w := range s.p.GetWorking() {
		if w.GetPath() == e.GetPath() {
			sources = w.GetSources()
			if w.GetAutomatic() != nil {
				auto = model.Value(w.GetAutomatic())
			}
		}
	}
	return &scenariosv1.Override{Path: e.GetPath(), Amount: amount, Sources: sources, Was: model.Money(auto)}, nil
}

func findOverride(list []*scenariosv1.Override, path string) *scenariosv1.Override {
	for _, o := range list {
		if o.GetPath() == path {
			return o
		}
	}
	return nil
}

// withOverride is a copy of a list with the override at path replaced by
// o, or removed when o is nil.
func withOverride(list []*scenariosv1.Override, path string, o *scenariosv1.Override) []*scenariosv1.Override {
	var out []*scenariosv1.Override
	placed := false
	for _, x := range list {
		if x.GetPath() != path {
			out = append(out, x)
		} else if o != nil && !placed {
			out, placed = append(out, o), true
		}
	}
	if o != nil && !placed {
		out = append(out, o)
	}
	return out
}

// setAt puts a value at a path in m. base is the same message in the
// parent's inputs, which Inherit reads; it may be invalid.
func setAt(m, base protoreflect.Message, steps []step, v *scenariosv1.Value) error {
	st := steps[0]
	last := len(steps) == 1
	fd := m.Descriptor().Fields().ByName(protoreflect.Name(st.field))
	if fd == nil {
		return fmt.Errorf("%s has no field %q", m.Descriptor().Name(), st.field)
	}
	var bfd protoreflect.FieldDescriptor
	if base != nil && base.IsValid() {
		bfd = base.Descriptor().Fields().ByName(fd.Name())
	}
	if fd.IsList() {
		if !st.hasKey || fd.Kind() != protoreflect.MessageKind {
			return fmt.Errorf("%s is a list; an edit names one of its items by key", st.field)
		}
		l := m.Mutable(fd).List()
		i := keyIndex(l, fd, st.key)
		var bel protoreflect.Message
		if bfd != nil {
			if j := keyIndex(base.Get(bfd).List(), bfd, st.key); j >= 0 {
				bel = base.Get(bfd).List().Get(j).Message()
			}
		}
		if !last {
			if i < 0 {
				return fmt.Errorf("%s has no item %q", st.field, st.key)
			}
			return setAt(l.Get(i).Message(), bel, steps[1:], v)
		}
		return setItem(l, fd, i, st.key, bel, v)
	}
	if st.hasKey {
		return fmt.Errorf("%s is not a list", st.field)
	}
	if !last {
		if fd.Kind() != protoreflect.MessageKind {
			return fmt.Errorf("%s is not a message", st.field)
		}
		var b protoreflect.Message
		if bfd != nil && base.Has(bfd) {
			b = base.Get(bfd).Message()
		}
		return setAt(m.Mutable(fd).Message(), b, steps[1:], v)
	}
	return setField(m, fd, base, bfd, v)
}

// keyIndex is the position of a list item by its key, or -1.
func keyIndex(l protoreflect.List, fd protoreflect.FieldDescriptor, key string) int {
	for i := 0; i < l.Len(); i++ {
		if k, _ := listKey(fd, l.Get(i).Message(), i); k == key {
			return i
		}
	}
	return -1
}

// setItem adds, replaces, removes or resets one list item.
func setItem(l protoreflect.List, fd protoreflect.FieldDescriptor, i int, key string, base protoreflect.Message, v *scenariosv1.Value) error {
	switch v.GetKind().(type) {
	case *scenariosv1.Value_Remove_:
		if i < 0 {
			return fmt.Errorf("there is no item %q to remove", key)
		}
		removeAt(l, i)
	case *scenariosv1.Value_Inherit_:
		switch {
		case base != nil:
			el := protoreflect.ValueOfMessage(proto.Clone(base.Interface()).ProtoReflect())
			if i >= 0 {
				l.Set(i, el)
			} else {
				l.Append(el)
			}
		case i >= 0:
			removeAt(l, i)
		}
	case *scenariosv1.Value_Message:
		el := l.NewElement()
		if err := (proto.UnmarshalOptions{Merge: true}).Unmarshal(v.GetMessage(), el.Message().Interface()); err != nil {
			return err
		}
		if k, _ := listKey(fd, el.Message(), l.Len()); k != key {
			return fmt.Errorf("the item's key is %q, not %q", k, key)
		}
		if i >= 0 {
			l.Set(i, el)
		} else {
			l.Append(el)
		}
	default:
		return fmt.Errorf("a list item is set whole, as a message")
	}
	return nil
}

func removeAt(l protoreflect.List, i int) {
	for j := i; j+1 < l.Len(); j++ {
		l.Set(j, l.Get(j+1))
	}
	l.Truncate(l.Len() - 1)
}

// setField sets one field from a value that fits it.
func setField(m protoreflect.Message, fd protoreflect.FieldDescriptor, base protoreflect.Message, bfd protoreflect.FieldDescriptor, v *scenariosv1.Value) error {
	switch x := v.GetKind().(type) {
	case *scenariosv1.Value_Inherit_:
		if bfd != nil && base.Has(bfd) {
			val := base.Get(bfd)
			if fd.Kind() == protoreflect.MessageKind {
				val = protoreflect.ValueOfMessage(proto.Clone(val.Message().Interface()).ProtoReflect())
			}
			m.Set(fd, val)
		} else {
			m.Clear(fd)
		}
		return nil
	case *scenariosv1.Value_Remove_:
		m.Clear(fd)
		return nil
	case *scenariosv1.Value_Message:
		if fd.Kind() != protoreflect.MessageKind {
			return fmt.Errorf("%s is not a message", fd.Name())
		}
		nm := m.NewField(fd).Message()
		if err := (proto.UnmarshalOptions{Merge: true}).Unmarshal(x.Message, nm.Interface()); err != nil {
			return err
		}
		m.Set(fd, protoreflect.ValueOfMessage(nm))
		return nil
	}
	val, err := scalarFor(fd, v)
	if err != nil {
		return err
	}
	if !val.IsValid() {
		m.Clear(fd)
		return nil
	}
	m.Set(fd, val)
	return nil
}

// scalarFor converts a value to a field's type. An invalid value with no
// error clears the field: "not sure" for an optional yes or no.
func scalarFor(fd protoreflect.FieldDescriptor, v *scenariosv1.Value) (protoreflect.Value, error) {
	mismatch := fmt.Errorf("%s doesn't take %T", fd.Name(), v.GetKind())
	switch fd.Kind() {
	case protoreflect.MessageKind:
		var m proto.Message
		switch fd.Message().FullName() {
		case moneyName:
			m = v.GetMoney()
		case dateName:
			m = v.GetDate()
		case quantityName:
			m = v.GetNumber()
		}
		if m == nil || !m.ProtoReflect().IsValid() {
			return protoreflect.Value{}, mismatch
		}
		return protoreflect.ValueOfMessage(proto.Clone(m).ProtoReflect()), nil
	case protoreflect.EnumKind:
		ev := fd.Enum().Values().ByName(protoreflect.Name(v.GetChoice()))
		if ev == nil || ev.Number() == 0 {
			return protoreflect.Value{}, fmt.Errorf("%q is not one of %s's options", v.GetChoice(), fd.Name())
		}
		return protoreflect.ValueOfEnum(ev.Number()), nil
	case protoreflect.BoolKind:
		switch v.GetYesNo() {
		case scenariosv1.YesNo_YES_NO_YES:
			return protoreflect.ValueOfBool(true), nil
		case scenariosv1.YesNo_YES_NO_NO:
			return protoreflect.ValueOfBool(false), nil
		case scenariosv1.YesNo_YES_NO_NOT_SURE:
			if fd.HasPresence() {
				return protoreflect.Value{}, nil
			}
		}
		return protoreflect.Value{}, mismatch
	case protoreflect.StringKind:
		if _, ok := v.GetKind().(*scenariosv1.Value_Text); !ok {
			return protoreflect.Value{}, mismatch
		}
		return protoreflect.ValueOfString(v.GetText()), nil
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Int64Kind, protoreflect.Sint64Kind:
		n, ok := whole(v.GetNumber())
		if !ok {
			return protoreflect.Value{}, mismatch
		}
		if fd.Kind() == protoreflect.Int32Kind || fd.Kind() == protoreflect.Sint32Kind {
			if n < math.MinInt32 || n > math.MaxInt32 {
				return protoreflect.Value{}, fmt.Errorf("%d is too large for %s", n, fd.Name())
			}
			return protoreflect.ValueOfInt32(int32(n)), nil
		}
		return protoreflect.ValueOfInt64(n), nil
	}
	return protoreflect.Value{}, fmt.Errorf("%s can't be edited", fd.Name())
}

// whole is a quantity as a whole number, if it is one.
func whole(q *commonv1.Quantity) (int64, bool) {
	if q == nil || q.GetScale() < 0 {
		return 0, false
	}
	n := q.GetUnits()
	for i := int32(0); i < q.GetScale(); i++ {
		if n%10 != 0 {
			return 0, false
		}
		n /= 10
	}
	return n, true
}
