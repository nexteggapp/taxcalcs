package federal

import (
	"fmt"
	"strings"

	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// formsPrefix is where the engine's working cells for the return's forms
// start.
const formsPrefix = "federal.forms."

// working is the engine's working cells for the return's lines, keyed by
// the line's path in the result, federal.<form>.<line>. A cell is kept for
// a line with an amount other than zero; its formula and citations become
// the line's rule.
func working(out *returnsv1.ReturnOutputs) []*scenariosv1.Working {
	forms := out.GetFederal().GetForms().ProtoReflect()
	var list []*scenariosv1.Working
	for _, c := range out.GetWorking().GetCells() {
		rest, ok := strings.CutPrefix(c.GetPath(), formsPrefix)
		if !ok || !nonZero(forms, rest) {
			continue
		}
		path := "federal." + rest
		var cites []string
		for _, ci := range c.GetCitations() {
			cites = append(cites, ci.GetAuthority())
		}
		list = append(list, &scenariosv1.Working{Path: path, Rules: []*scenariosv1.AppliedRule{{
			Cite: strings.Join(cites, "; "), Applied: &scenariosv1.Statement{Text: c.GetFormula(), States: []string{path}}}}})
	}
	return list
}

// nonZero reports whether the money field at a dotted path in m is set and
// not zero.
func nonZero(m protoreflect.Message, path string) bool {
	names := strings.Split(path, ".")
	for i, n := range names {
		fd := m.Descriptor().Fields().ByName(protoreflect.Name(n))
		if fd == nil || fd.IsList() || fd.IsMap() || !m.Has(fd) || fd.Kind() != protoreflect.MessageKind {
			return false
		}
		v := m.Get(fd).Message()
		if i == len(names)-1 {
			x, ok := v.Interface().(*commonv1.Money)
			return ok && x.GetMinor() != 0
		}
		m = v
	}
	return false
}

// diagnostics adds the engine's warnings and errors to what the return
// leaves out, and each input it says is missing as a question.
func (r *Return) diagnostics() {
	for _, d := range r.out.GetFederal().GetDiagnostics() {
		switch d.GetSeverity() {
		case returnsv1.Severity_SEVERITY_WARNING, returnsv1.Severity_SEVERITY_ERROR:
			r.notes.LeaveOut(d.GetMessage())
		}
	}
	for _, p := range r.out.GetWorking().GetMissingInputs() {
		r.notes.Ask(model.NewQuestion("federal/missing/"+p, "federal",
			fmt.Sprintf("The federal return needs %s.", p), "It is worked out without it until it is given.", "facts"))
	}
}
