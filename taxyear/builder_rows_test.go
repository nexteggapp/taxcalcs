package taxyear_test

import (
	"testing"

	"google.golang.org/protobuf/proto"

	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear"
)

// counted adds the figures of a group's items that count toward its
// total, in this scenario or Your plan.
func counted(items []*scenariosv1.BuilderItem, plan bool) int64 {
	var n int64
	for _, it := range items {
		if it.GetNotCounted() {
			continue
		}
		if plan {
			n += it.GetYourPlan().GetAmount().GetMinor()
		} else {
			n += it.GetThisScenario().GetAmount().GetMinor()
		}
	}
	return n
}

// TestBuilderRowsAddUp checks that Income's and Deductions' rows add up
// to their totals in every scenario, beside Your plan's, and that the
// itemised deductions' parts add up to it.
func TestBuilderRowsAddUp(t *testing.T) {
	for _, c := range []string{"simple-2026", "full-everything", "moved-2026", "full-override", "full-2026", "full-hsa-over", "full-year-total-plan"} {
		f := readCase(t, c)
		for _, s := range f.GetScenarios() {
			v := view(t, f, s.GetId())
			if len(v.GetProblems()) > 0 {
				continue
			}
			for _, g := range v.GetGroups() {
				if g.GetId() != "income" && g.GetId() != "deductions" {
					continue
				}
				if got, want := counted(g.GetItems(), false), g.GetTotal().GetThisScenario().GetMinor(); got != want {
					t.Errorf("%s/%s: %s rows add up to %d, total %d", c, s.GetId(), g.GetId(), got, want)
				}
				if s.GetId() != "plan" {
					if got, want := counted(g.GetItems(), true), g.GetTotal().GetYourPlan().GetMinor(); got != want {
						t.Errorf("%s/%s: %s rows in Your plan add up to %d, total %d", c, s.GetId(), g.GetId(), got, want)
					}
				}
			}
			if it := find(v, "deductions.itemised"); it != nil && !it.GetNotCounted() {
				if got, want := counted(it.GetChildren(), false), it.GetThisScenario().GetAmount().GetMinor(); got != want {
					t.Errorf("%s/%s: itemised parts add up to %d, itemised %d", c, s.GetId(), got, want)
				}
			}
		}
	}
}

// payMethod is a job's "How should we count this year's pay?" field.
func payMethod(t *testing.T, v *scenariosv1.BuilderView, job string) *scenariosv1.Field {
	t.Helper()
	for _, f := range find(v, job).GetFields() {
		if f.GetKind().GetMethod() != nil {
			return f
		}
	}
	t.Fatalf("%s has no pay method", job)
	return nil
}

// TestPayMethodOptions chooses each way of counting Acme's pay in every
// scenario. The edit applies, the way is then the one chosen, and the
// year's pay is the preview it showed.
func TestPayMethodOptions(t *testing.T) {
	const job = "model.employers[acme]"
	f := readCase(t, "full-everything")
	for _, s := range f.GetScenarios() {
		for _, o := range payMethod(t, view(t, f, s.GetId()), job).GetKind().GetMethod().GetOptions() {
			if o.GetEdit() == nil {
				continue
			}
			edited, err := taxyear.ApplyEdit(f, o.GetEdit())
			if err != nil {
				t.Errorf("%s: choosing %s: %v", s.GetId(), o.GetValue(), err)
				continue
			}
			after := view(t, edited, s.GetId())
			m := payMethod(t, after, job)
			if m.GetValue().GetChoice() != o.GetValue() {
				t.Errorf("%s: after choosing %s the way is %s", s.GetId(), o.GetValue(), m.GetValue().GetChoice())
			}
			for _, a := range m.GetKind().GetMethod().GetOptions() {
				if a.GetValue() == o.GetValue() && o.GetPreview() != nil && !proto.Equal(a.GetPreview().GetThisScenario(), o.GetPreview().GetThisScenario()) {
					t.Errorf("%s: %s previewed %v, gave %v", s.GetId(), o.GetValue(), o.GetPreview().GetThisScenario(), a.GetPreview().GetThisScenario())
				}
			}
		}
	}
}

// TestPayMethodLastYear removes Acme's paystubs and deposits. Like my
// latest paycheck is then not offered, and last year's W-2 is.
func TestPayMethodLastYear(t *testing.T) {
	f := readCase(t, "full-everything")
	acme := f.GetShared().GetModel().GetEmployers()[0]
	var kept []*scenariosv1.EmployerPart
	for _, p := range acme.GetParts() {
		if p.GetFromPaystubs() == nil && p.GetFromDeposits() == nil {
			kept = append(kept, p)
		}
	}
	acme.Parts = kept
	options := map[string]*scenariosv1.MethodOption{}
	for _, o := range payMethod(t, view(t, f, "plan"), "model.employers[acme]").GetKind().GetMethod().GetOptions() {
		options[o.GetValue()] = o
	}
	last, ok := options["last_year"]
	if !ok || options["same_as_latest"].GetEdit() != nil {
		t.Fatalf("options are %v, want last year's W-2 and no other way to choose like the latest", options)
	}
	edited, err := taxyear.ApplyEdit(f, last.GetEdit())
	if err != nil {
		t.Fatal(err)
	}
	m := payMethod(t, view(t, edited, "plan"), "model.employers[acme]")
	if m.GetValue().GetChoice() != "last_year" {
		t.Errorf("after choosing last year's W-2 the way is %s", m.GetValue().GetChoice())
	}
}

// methodFields lists every method field in a view, with its item's id.
func methodFields(v *scenariosv1.BuilderView) map[string]*scenariosv1.Field {
	out := map[string]*scenariosv1.Field{}
	var walk func(items []*scenariosv1.BuilderItem)
	walk = func(items []*scenariosv1.BuilderItem) {
		for _, it := range items {
			for _, f := range it.GetFields() {
				if f.GetKind().GetMethod() != nil {
					out[it.GetId()] = f
				}
			}
			walk(it.GetChildren())
		}
	}
	for _, g := range v.GetGroups() {
		walk(g.GetItems())
	}
	return out
}

// TestMethodOptionsApply chooses every option of every method field in
// every scenario of full-everything. Each edit applies, the option is then
// the one chosen, and an option that previewed a figure gives it.
func TestMethodOptionsApply(t *testing.T) {
	f := readCase(t, "full-everything")
	for _, s := range f.GetScenarios() {
		for id, m := range methodFields(view(t, f, s.GetId())) {
			for _, o := range m.GetKind().GetMethod().GetOptions() {
				if o.GetEdit() == nil {
					continue
				}
				if (o.GetPreview() == nil) == (o.GetPreviewText() == "") {
					t.Errorf("%s/%s/%s: want a preview figure or words, not both or neither", s.GetId(), id, o.GetValue())
				}
				edited, err := taxyear.ApplyEdit(f, o.GetEdit())
				if err != nil {
					t.Errorf("%s/%s: choosing %s: %v", s.GetId(), id, o.GetValue(), err)
					continue
				}
				after, ok := methodFields(view(t, edited, s.GetId()))[id]
				if !ok || after.GetValue().GetChoice() != o.GetValue() {
					t.Errorf("%s/%s: after choosing %s the way is %s", s.GetId(), id, o.GetValue(), after.GetValue().GetChoice())
					continue
				}
				for _, a := range after.GetKind().GetMethod().GetOptions() {
					if a.GetValue() == o.GetValue() && o.GetPreview() != nil && !proto.Equal(a.GetPreview().GetThisScenario(), o.GetPreview().GetThisScenario()) {
						t.Errorf("%s/%s: %s previewed %v, gave %v", s.GetId(), id, o.GetValue(), o.GetPreview().GetThisScenario(), a.GetPreview().GetThisScenario())
					}
				}
			}
		}
	}
}
