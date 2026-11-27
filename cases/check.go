package cases

import (
	"fmt"

	casesv1 "github.com/nexteggapp/taxcalcs/cases/v1"
	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Outcome is how one expected figure came out.
type Outcome int

const (
	// Pass: the rules produce the expected figure.
	Pass Outcome = iota
	// Fail: they do not, and no known gap says so.
	Fail
	// Gap: they do not, as a known gap says.
	Gap
	// GapClosed: a known gap says they do not, but they do.
	GapClosed
)

func (o Outcome) String() string {
	return [...]string{"pass", "fail", "known gap", "known gap now passes"}[o]
}

// Result is one expected figure and what the rules produced for it.
type Result struct {
	Expected *casesv1.Expected
	Outcome  Outcome
	// Got is the figure the rules produced, or why there is none.
	Got string
}

// Check prepares a case's household and compares each expected figure with
// the returns.
func Check(eng *federal.Engine, forms map[string]*Form, c *casesv1.TestCase) ([]Result, error) {
	out, prepErr := eng.Prepare(c.GetInputs())
	if prepErr != nil && c.GetKnownGap() == nil {
		return nil, fmt.Errorf("preparing: %w", prepErr)
	}
	var results []Result
	for _, e := range c.GetExpected() {
		gap := e.GetKnownGap() != nil || c.GetKnownGap() != nil
		got, match, err := figure(out, forms, e)
		if prepErr != nil {
			got, match, err = "", false, prepErr
		}
		if err != nil && !gap {
			return nil, fmt.Errorf("%s line %s: %w", e.GetForm(), e.GetLine(), err)
		}
		if err != nil {
			got = err.Error()
		}
		r := Result{Expected: e, Got: got}
		switch {
		case match && gap:
			r.Outcome = GapClosed
		case match:
			r.Outcome = Pass
		case gap:
			r.Outcome = Gap
		default:
			r.Outcome = Fail
		}
		results = append(results, r)
	}
	return results, nil
}

// figure reads an expected figure's line from the returns.
func figure(out *returnsv1.ReturnOutputs, forms map[string]*Form, e *casesv1.Expected) (got string, match bool, err error) {
	f, ok := forms[e.GetForm()]
	if !ok {
		return "", false, fmt.Errorf("no form %q in the rules", e.GetForm())
	}
	field, ok := f.Fields[e.GetLine()]
	if !ok {
		return "", false, fmt.Errorf("form %s has no line %q with an output field", e.GetForm(), e.GetLine())
	}
	var found []protoreflect.Message
	find(out.ProtoReflect(), f.Message, &found)
	if int(e.GetInstance()) >= len(found) {
		return "", false, fmt.Errorf("the returns have %d copies of %s, want copy %d", len(found), f.Message, e.GetInstance())
	}
	msg := found[e.GetInstance()]
	fd := msg.Descriptor().Fields().ByName(field)
	if fd == nil {
		return "", false, fmt.Errorf("%s has no field %s", f.Message, field)
	}
	v := msg.Get(fd)
	switch want := e.GetValue().(type) {
	case *casesv1.Expected_Cents:
		if fd.Kind() != protoreflect.MessageKind || fd.Message().FullName() != "nexteggapp.taxprotos.common.v1.Money" {
			return "", false, fmt.Errorf("line %s is %s, not money", e.GetLine(), fd.Kind())
		}
		minor := v.Message().Get(fd.Message().Fields().ByName("minor")).Int()
		return fmt.Sprintf("%d cents", minor), minor == want.Cents, nil
	case *casesv1.Expected_Integer:
		var n int64
		switch fd.Kind() {
		case protoreflect.Int32Kind, protoreflect.Int64Kind, protoreflect.Sint32Kind, protoreflect.Sint64Kind:
			n = v.Int()
		case protoreflect.Uint32Kind, protoreflect.Uint64Kind:
			n = int64(v.Uint())
		default:
			return "", false, fmt.Errorf("line %s is %s, not a whole number", e.GetLine(), fd.Kind())
		}
		return fmt.Sprint(n), n == want.Integer, nil
	}
	return "", false, fmt.Errorf("no expected value")
}

// find appends every message of the given type under m, depth first, in
// field order.
func find(m protoreflect.Message, name protoreflect.FullName, found *[]protoreflect.Message) {
	if m.Descriptor().FullName() == name {
		*found = append(*found, m)
		return
	}
	fields := m.Descriptor().Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		if fd.Kind() != protoreflect.MessageKind || fd.IsMap() || !m.Has(fd) {
			continue
		}
		if fd.IsList() {
			l := m.Get(fd).List()
			for j := 0; j < l.Len(); j++ {
				find(l.Get(j).Message(), name, found)
			}
			continue
		}
		find(m.Get(fd).Message(), name, found)
	}
}
