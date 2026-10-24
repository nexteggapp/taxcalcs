package all_test

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/estimated/all"
	"github.com/nexteggapp/taxcalcs/estimated/estimatedtest"
)

func answers(t *testing.T, code string) (estimated.EstimatedTax, []estimatedtest.Answers) {
	t.Helper()
	j := estimated.Jurisdiction(code)
	rules, err := all.For(j, 2026)
	if err != nil {
		t.Fatalf("%s: %v", j, err)
	}
	hs := estimatedtest.Households(j)
	if len(hs) < 10 {
		t.Fatalf("%s: only %d households could be built", j, len(hs))
	}
	return rules, estimatedtest.Ask(rules, hs)
}

// Every jurisdiction chooses exactly one option whenever a target is known,
// makes an exception that applies the $0 target, gives every penalty a
// reason that matches it, and sets each due date's status by one rule.
func TestEveryJurisdictionAnswersInShape(t *testing.T) {
	for _, code := range postalCodes {
		_, as := answers(t, code)
		for _, problem := range estimatedtest.CheckAnswers(as) {
			t.Errorf("%s: %s", code, problem)
		}
	}
}

// Every sentence a person reads, in every jurisdiction, follows the copy
// rules: whole dollars, no talk about the software or what was entered, no
// forbidden words, and an explanation of at most four short paragraphs.
func TestEveryJurisdictionFollowsTheCopyRules(t *testing.T) {
	for _, code := range postalCodes {
		rules, as := answers(t, code)
		for _, problem := range estimatedtest.CheckCopy(rules, estimatedtest.Texts(rules, as)) {
			t.Errorf("%s: %s", code, problem)
		}
	}
}

// The households reach a penalty cut short at an unpublished rate in most
// jurisdictions, so the check that nothing calls it a maximum is exercised.
func TestHouseholdsReachAnUnpublishedRate(t *testing.T) {
	reached := 0
	for _, code := range postalCodes {
		_, as := answers(t, code)
		for _, a := range as {
			if p, err := a.Penalty(); err == nil && !p.Value().RatedThrough().IsZero() {
				reached++
				break
			}
		}
	}
	if reached < 20 {
		t.Errorf("only %d jurisdictions stop a penalty at an unpublished rate", reached)
	}
	t.Logf("%d jurisdictions stop a penalty at an unpublished rate", reached)
}
