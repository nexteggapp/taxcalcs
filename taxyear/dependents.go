package taxyear

import (
	"fmt"

	"github.com/nexteggapp/taxcalcs/money"
	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
	form1040 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1040"
)

// relativeIncomeLimit is a qualifying relative's gross income limit, the
// exemption amount of 26 U.S.C. § 152(d)(1)(B) as each year's revenue
// procedure sets it.
var relativeIncomeLimit = map[int]money.USCents{2025: money.Dollars(5_200), 2026: money.Dollars(5_300)}

// The catalogue's questions a qualifying relative waits on.
const (
	incomeQuestion  = "dependent.gross_income_under_limit"
	supportQuestion = "dependent.more_than_half_support"
)

// dependents works out whether each person other than you and your spouse
// counts as your dependent: a qualifying child by
// relationship, residence and age; otherwise a qualifying relative by
// income and support, asked as questions. It asks those questions in notes.
func dependents(ctx *model.Context, notes *model.Notes) []*scenariosv1.Dependency {
	var out []*scenariosv1.Dependency
	for _, p := range ctx.Facts().GetPeople() {
		switch p.GetRelationship() {
		case factsv1.Relationship_RELATIONSHIP_YOU, factsv1.Relationship_RELATIONSHIP_SPOUSE:
			continue
		}
		out = append(out, dependency(ctx, p, notes))
	}
	return out
}

func dependency(ctx *model.Context, p *factsv1.Person, notes *model.Notes) *scenariosv1.Dependency {
	d := &scenariosv1.Dependency{Person: p.GetId()}
	say := func(status scenariosv1.DependencyStatus, test scenariosv1.DependencyTest, text string) *scenariosv1.Dependency {
		d.Status, d.Test, d.Text = status, test, &scenariosv1.Statement{Text: text}
		return d
	}
	age, born := ctx.AgeAtYearEnd(p.GetId())
	childKind := p.GetRelationship() == factsv1.Relationship_RELATIONSHIP_CHILD || p.GetRelationship() == factsv1.Relationship_RELATIONSHIP_SIBLING
	child := scenariosv1.DependencyTest_DEPENDENCY_TEST_QUALIFYING_CHILD
	relative := scenariosv1.DependencyTest_DEPENDENCY_TEST_QUALIFYING_RELATIVE
	switch {
	case childKind && !born:
		return say(scenariosv1.DependencyStatus_DEPENDENCY_STATUS_UNKNOWN, child, "Whether they count as your dependent goes by their date of birth")
	case childKind && p.GetMonthsLivedWithYou() >= 7 && (p.GetPermanentlyDisabled() || age < 19 || (age < 24 && p.GetFullTimeStudent())):
		return say(scenariosv1.DependencyStatus_DEPENDENCY_STATUS_COUNTS, child, "Counts as your qualifying child")
	case p.GetRelationship() == factsv1.Relationship_RELATIONSHIP_NOT_RELATED && p.GetMonthsLivedWithYou() < 12:
		return say(scenariosv1.DependencyStatus_DEPENDENCY_STATUS_DOES_NOT_COUNT, 0, "Doesn’t count as your dependent: they didn’t live with you all year")
	}
	limit, ok := relativeIncomeLimit[ctx.Year()]
	if !ok {
		return say(scenariosv1.DependencyStatus_DEPENDENCY_STATUS_UNKNOWN, relative, fmt.Sprintf("Whether %s as your dependent for %d isn’t worked out yet", counts(p), ctx.Year()))
	}
	income, support := askRelative(ctx, p, limit, notes, d)
	no, yes := factsv1.Reply_REPLY_NO, factsv1.Reply_REPLY_YES
	switch {
	case income == no:
		return say(scenariosv1.DependencyStatus_DEPENDENCY_STATUS_DOES_NOT_COUNT, relative, "Doesn’t count as your dependent: their income is over the limit")
	case support == no:
		return say(scenariosv1.DependencyStatus_DEPENDENCY_STATUS_DOES_NOT_COUNT, relative, "Doesn’t count as your dependent: you didn’t provide more than half of their support")
	case income == yes && support == yes:
		return say(scenariosv1.DependencyStatus_DEPENDENCY_STATUS_COUNTS, relative, "Counts as your qualifying relative")
	case income == factsv1.Reply_REPLY_UNSPECIFIED || support == factsv1.Reply_REPLY_UNSPECIFIED:
		return say(scenariosv1.DependencyStatus_DEPENDENCY_STATUS_COULD_COUNT, relative, "Could count as your qualifying relative")
	}
	return say(scenariosv1.DependencyStatus_DEPENDENCY_STATUS_MAY_COUNT, relative, "May count as your qualifying relative")
}

// counts is "Rosa counts", or "they count" for someone unnamed.
func counts(p *factsv1.Person) string {
	if p.GetName() == "" {
		return "they count"
	}
	return p.GetName() + " counts"
}

// askRelative asks the qualifying relative's two questions, answered or
// not, and returns their replies.
func askRelative(ctx *model.Context, p *factsv1.Person, limit money.USCents, notes *model.Notes, d *scenariosv1.Dependency) (income, support factsv1.Reply) {
	whose := "their"
	if p.GetName() != "" {
		whose = p.GetName() + "’s"
	}
	ask := func(question, text, effect string) factsv1.Reply {
		id := question + "." + p.GetId()
		d.Questions = append(d.Questions, id)
		notes.Ask(model.NewQuestion(id, "dependents["+p.GetId()+"]", text, effect, "facts.positions", "Yes", "No", "Not sure"))
		return reply(ctx.Facts(), question, p.GetId())
	}
	income = ask(incomeQuestion, fmt.Sprintf("Was %s gross income under %s?", whose, limit.Whole()),
		"Until it is answered yes, they could count as your qualifying relative; no means they don’t.")
	support = ask(supportQuestion, fmt.Sprintf("Did you provide more than half of %s support?", whose),
		"Until it is answered yes, they could count as your qualifying relative; no means they don’t.")
	return income, support
}

// reply is the facts' answer to a catalogue question about a person.
func reply(f *factsv1.TaxYearFacts, question, person string) factsv1.Reply {
	for _, pos := range f.GetPositions() {
		if pos.GetQuestion() != question {
			continue
		}
		for _, s := range pos.GetSubjects() {
			if s == person {
				return pos.GetReply()
			}
		}
	}
	return factsv1.Reply_REPLY_UNSPECIFIED
}

// dependentRows lists those who count as the Form 1040 dependents block
// names them. The credit checkboxes stay clear: the credits aren't worked
// out yet.
func dependentRows(ctx *model.Context, ds []*scenariosv1.Dependency) []*form1040.Dependent {
	var out []*form1040.Dependent
	for _, d := range ds {
		if d.GetStatus() != scenariosv1.DependencyStatus_DEPENDENCY_STATUS_COUNTS {
			continue
		}
		p := ctx.Person(d.GetPerson())
		out = append(out, &form1040.Dependent{Name: p.GetName(), Relationship: relationshipWords[p.GetRelationship()]})
	}
	return out
}

// relationshipWords is a relationship as the dependents block prints it.
var relationshipWords = map[factsv1.Relationship]string{
	factsv1.Relationship_RELATIONSHIP_CHILD: "child", factsv1.Relationship_RELATIONSHIP_PARENT: "parent",
	factsv1.Relationship_RELATIONSHIP_SIBLING: "sibling", factsv1.Relationship_RELATIONSHIP_OTHER_RELATIVE: "relative",
	factsv1.Relationship_RELATIONSHIP_NOT_RELATED: "member of household",
}
