package working_test

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/working"
)

func TestResultCarriesItsWorking(t *testing.T) {
	d, err := working.NewDivergence("A rule").Form("does one thing", "Form 1").Statute("says another", "§ 1").
		Uses(working.ReadingForm).Affects(true).Build()
	if err != nil {
		t.Fatal(err)
	}
	var n working.Notes
	n.Rule("§ 2", "noted first").Require(working.PositionResidency, "residence was not given").Diverges(d)
	r, err := working.NewResult(42).Rule("§ 1", "did a thing").With(n).Rule("§ 3", "after the notes").Build()
	if err != nil {
		t.Fatal(err)
	}
	if r.Value() != 42 {
		t.Errorf("Value = %d, want 42", r.Value())
	}
	var cites []string
	for _, a := range r.Applied() {
		cites = append(cites, a.Cite())
	}
	if got, want := len(cites), 3; got != want || cites[0] != "§ 1" || cites[1] != "§ 2" || cites[2] != "§ 3" {
		t.Errorf("Applied cites = %v, want § 1, § 2, § 3", cites)
	}
	if !r.Needs() || r.Required()[0].Position() != working.PositionResidency || r.Required()[0].Because() != "residence was not given" {
		t.Errorf("Required = %v", r.Required())
	}
	if len(r.Divergences()) != 1 || r.Divergences()[0].Rule() != "A rule" {
		t.Errorf("Divergences = %v", r.Divergences())
	}

	// Notes changed after With do not reach the result.
	n.Rule("§ 4", "too late")
	if len(r.Applied()) != 3 {
		t.Errorf("a note added after Build reached the result")
	}
}

func TestResultCannotBeChanged(t *testing.T) {
	values := []int{1, 2, 3}
	r, err := working.NewResult(values).Rule("§ 1", "listed them").Build()
	if err != nil {
		t.Fatal(err)
	}
	values[0] = 99
	got := r.Value()
	if got[0] != 1 {
		t.Errorf("changing the slice given to the builder changed the result: %v", got)
	}
	got[1] = 99
	if r.Value()[1] != 2 {
		t.Errorf("changing the slice Value returned changed the result: %v", r.Value())
	}
	applied := r.Applied()
	applied[0] = working.Rule{}
	if r.Applied()[0].Cite() != "§ 1" {
		t.Errorf("changing the slice Applied returned changed the result")
	}
}

func TestBuildRefusesWorkingNobodyCanCheck(t *testing.T) {
	for name, b := range map[string]*working.ResultBuilder[int]{
		"rule without a cite":       working.NewResult(1).Rule("", "did a thing"),
		"rule without what it did":  working.NewResult(1).Rule("§ 1", ""),
		"position without a kind":   working.NewResult(1).Require(working.PositionUnspecified, "because"),
		"position without a reason": working.NewResult(1).Require(working.PositionResidency, ""),
		"divergence half made":      working.NewResult(1).Diverges(working.Divergence{}),
	} {
		if _, err := b.Build(); err == nil {
			t.Errorf("%s: Build accepted it", name)
		}
	}
	if _, err := working.NewRule("§ 1").Build(); err == nil {
		t.Error("NewRule accepted a rule that says nothing")
	}
	if r, err := working.NewRule("§ 1").Applied("did it").Build(); err != nil || r.Cite() != "§ 1" || r.Applied() != "did it" {
		t.Errorf("NewRule = %v, %v", r, err)
	}
	if _, err := working.NewRequired(working.PositionLotSelection).Build(); err == nil {
		t.Error("NewRequired accepted a position with no reason")
	}
	if r, err := working.NewRequired(working.PositionLotSelection).Because("FIFO").Build(); err != nil || r.Position() != working.PositionLotSelection {
		t.Errorf("NewRequired = %v, %v", r, err)
	}
}

// A divergence says which reading the figures follow and whether it changes
// anything for these facts, and is refused without either.
func TestDivergenceNeedsItsReadingAndWhetherItAffects(t *testing.T) {
	base := func() *working.DivergenceBuilder {
		return working.NewDivergence("A rule").Form("does one thing", "Form 1").Statute("says another", "§ 1")
	}
	if _, err := base().Affects(false).Build(); err == nil {
		t.Error("a divergence with no reading was accepted")
	}
	if _, err := base().Uses(working.ReadingStatute).Build(); err == nil {
		t.Error("a divergence that does not say whether it affects the facts was accepted")
	}
	d, err := base().Uses(working.ReadingStatute).Affects(false).Build()
	if err != nil {
		t.Fatal(err)
	}
	if d.Uses() != working.ReadingStatute || d.AffectsHousehold() {
		t.Errorf("uses %v, affects %v", d.Uses(), d.AffectsHousehold())
	}
}

// A position asked as a question carries the question and its effect, and is
// refused without an effect or with a question that isn't one.
func TestAskCarriesAQuestionAndItsEffect(t *testing.T) {
	var n working.Notes
	n.Ask(working.PositionThisYearTax, "What do you expect your 2026 tax to be?", "Without it, the target can only come from last year's tax.")
	n.Ask(working.PositionUnpublishedRate, "", "Interest after Dec 31, 2026 isn't included until the rate is published.")
	r, err := working.NewResult(1).With(n).Build()
	if err != nil {
		t.Fatal(err)
	}
	q := r.Required()
	if !q[0].Asked() || q[0].Question() != "What do you expect your 2026 tax to be?" || q[0].Effect() != "Without it, the target can only come from last year's tax." {
		t.Errorf("%+v", q[0])
	}
	if q[1].Question() != "" || q[1].Effect() == "" || q[1].Because() != q[1].Effect() {
		t.Errorf("%+v", q[1])
	}
	for _, bad := range []func(*working.Notes){
		func(n *working.Notes) { n.Ask(working.PositionThisYearTax, "What is it?", "") },
		func(n *working.Notes) { n.Ask(working.PositionThisYearTax, "Your 2026 tax.", "It sets the target.") },
	} {
		var n working.Notes
		bad(&n)
		if _, err := working.NewResult(1).With(n).Build(); err == nil {
			t.Error("a malformed question was accepted")
		}
	}
}
