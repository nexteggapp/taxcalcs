package federal

import (
	"fmt"

	"github.com/nexteggapp/taxcalcs/money"
	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/internal/civil"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	"github.com/nexteggapp/taxcalcs/taxyear/model/retirement"
	"github.com/nexteggapp/taxcalcs/taxyear/payroll"
	"github.com/nexteggapp/taxcalcs/ustaxcalc"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
)

// Summary returns the totals and limits the return works out that no form
// line holds: the standard deduction, state and local tax by where it was
// paid, the itemised floors, the capital loss carried and each person's
// retirement limits. It is shared with the Return.
func (r *Return) Summary() *scenariosv1.ScenarioSummary { return r.summary }

// summarise fills the summary's federal parts once the return is worked
// out.
func (c *calc) summarise() error {
	s := c.r.summary
	std, err := ustaxcalc.StandardDeduction(c.ctx.Year(), c.r.status, c.conditions())
	if err != nil {
		return err
	}
	s.StandardDeduction = model.Money(model.Dollars(std.Value()))
	if l := c.gains.InIncome() - c.gains.Combined(); l > 0 {
		s.CapitalLossCarried = model.Money(-l)
	}
	return nil
}

// saltParts is state and local income tax paid in the year by where it
// was paid: withheld, estimated payments, and last year's balance.
func (c *calc) saltParts() (withheld, estimates, balance money.USCents) {
	for _, v := range c.r.stateWithheld {
		withheld += v
	}
	for _, t := range c.proj.GetTaxPaid() {
		if t.GetJurisdiction() == "US" {
			continue
		}
		for _, it := range t.GetItems() {
			d, _ := civil.FromProto(it.GetDate())
			p := it.GetPayment()
			if d.Year() != c.ctx.Year() || p.GetPurpose().GetNotTax() != nil || p.GetFromLastYear() {
				continue
			}
			if p.GetPurpose().GetBalanceFor() != 0 && int(p.GetPurpose().GetBalanceFor()) < c.ctx.Year() {
				balance += model.Value(p.GetAmount())
			} else {
				estimates += model.Value(p.GetAmount())
			}
		}
	}
	return withheld, estimates, balance
}

// itemisedSummary records the medical and charity floors under the first
// model of each kind: the floor applies to the kind's models together. It
// returns the charity floor's path and amount, or "" with no charity.
func (c *calc) itemisedSummary(medicalFloor, medicalCounted, gifts money.USCents) (string, money.USCents) {
	var medicalID, charityID string
	for _, m := range c.inputs.GetModel().GetItemised() {
		switch {
		case m.GetMedical() != nil && medicalID == "":
			medicalID = m.GetId()
		case m.GetCharity() != nil && charityID == "":
			charityID = m.GetId()
		}
	}
	s := c.r.summary
	if medicalID != "" {
		s.Itemised = append(s.Itemised, &scenariosv1.ItemisedSummary{Id: medicalID, Floor: model.Money(medicalFloor), Counted: model.Money(medicalCounted)})
	}
	if charityID != "" {
		var floor money.USCents
		if c.ctx.Year() >= 2026 {
			// § 170(b)(1)(I): gifts count above 0.5% of AGI.
			floor = min(gifts, model.Dollars(money.Rate(50).Of(max(0, c.r.agi))))
		}
		s.Itemised = append(s.Itemised, &scenariosv1.ItemisedSummary{Id: charityID, Floor: model.Money(floor), Counted: model.Money(gifts - floor)})
		at := "summary.itemised[" + charityID + "]"
		w := c.r.working(at + ".floor")
		w.Rules = append(w.Rules, rule("26 U.S.C. § 170(b)(1)(I), added by Pub. L. 119-21, sec. 70425",
			fmt.Sprintf("Gifts to charity count only above 0.5%% of adjusted gross income, %v: %v of the %v doesn't count", model.Dollars(money.Rate(50).Of(max(0, c.r.agi))), floor, gifts),
			at+".floor", "federal.schedule_a.total_gifts_to_charity"))
		c.r.term(at+".counted", nil, "federal.schedule_a.total_gifts_to_charity")
		c.r.minus(at+".counted", at+".floor")
		return at + ".floor", floor
	}
	return "", 0
}

// retirementSummary records one person's IRA year and their limits. A
// limit that needs an answer asks for it: the date of birth, for the plan
// limit's catch-up, which counts nothing until it is given; the HSA
// coverage, for someone with HSA contributions, whose limit is unset
// until it is given.
func (c *calc) retirementSummary(id string, traditional, roth, deducted money.USCents, lim *ustaxcalc.IRALimits) {
	s := &scenariosv1.RetirementSummary{Person: id, IraUsed: model.Money(traditional + roth), Deducted: model.Money(deducted)}
	if lim != nil {
		s.RothExcess, s.IraExcise = model.Money(model.Dollars(lim.RothExcess())), model.Money(model.Dollars(lim.ExcessTax()))
	}
	at := "summary.retirement[" + id + "]"
	age, aged := c.ctx.AgeAtYearEnd(id)
	if base, catchUp, err := payroll.PlanLimit(c.ctx.Year(), age); err == nil {
		s.PlanLimit = model.Money(base + catchUp)
		if !aged {
			c.r.notes.Ask(model.NewQuestion("retirement/"+id+"/date-of-birth", at+".plan_limit",
				fmt.Sprintf("What is %s date of birth? The workplace plan limit is higher from 50.", c.possessive(id)),
				"No catch-up is counted until you say.",
				"facts.people["+id+"].date_of_birth"))
		}
	}
	var employer, contributed money.USCents
	for _, e := range c.inputs.GetModel().GetEmployers() {
		if e.GetPerson() == id {
			employer += model.Value(e.GetEmployerHsa())
		}
	}
	for _, e := range c.proj.GetEmployers() {
		if c.employerOf(e.GetId()) == id {
			contributed += model.Value(e.GetYear().GetBoxes().GetHsa())
		}
	}
	people := map[string]string{}
	for _, m := range c.inputs.GetModel().GetRetirement() {
		people[m.GetId()] = m.GetPerson()
	}
	for _, r := range c.proj.GetRetirement() {
		if people[r.GetId()] == id {
			contributed += model.Value(r.GetYear().GetItems().GetHsa())
		}
	}
	s.HsaEmployer = model.Money(employer)
	limit, ok := c.hsaLimits[id]
	if !ok {
		limit, ok = retirement.HsaLimit(c.ctx, id, &c.r.notes)
	}
	switch {
	case ok:
		s.HsaLimit = model.Money(limit)
	case employer+contributed > 0 && c.ctx.Person(id).GetHsaCoverage() == factsv1.HsaCoverage_HSA_COVERAGE_UNSPECIFIED:
		c.r.notes.Ask(model.NewQuestion("retirement/"+id+"/hsa-coverage", at+".hsa_limit",
			fmt.Sprintf("What health plan coverage did %s HSA go with?", c.possessive(id)),
			"The HSA limit isn't counted until you say.",
			"facts.people["+id+"].hsa_coverage", "Self-only", "Family", "No HSA-eligible plan"))
	}
	c.r.summary.Retirement = append(c.r.summary.Retirement, s)
}

// employerOf is the person a job's model names.
func (c *calc) employerOf(job string) string {
	for _, m := range c.inputs.GetModel().GetEmployers() {
		if m.GetId() == job {
			return m.GetPerson()
		}
	}
	return ""
}

// possessive is "your" for you, the person's name with "'s", or for a
// spouse without one "your spouse's".
func (c *calc) possessive(id string) string {
	p := c.ctx.Person(id)
	switch {
	case p.GetRelationship() == factsv1.Relationship_RELATIONSHIP_YOU || p.GetName() == "" && id == "you":
		return "your"
	case p.GetName() != "":
		return p.GetName() + "'s"
	case p.GetRelationship() == factsv1.Relationship_RELATIONSHIP_SPOUSE || id == spouseID:
		return "your spouse's"
	}
	return id + "'s"
}
