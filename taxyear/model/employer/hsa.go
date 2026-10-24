package employer

import (
	"errors"
	"fmt"

	"github.com/nexteggapp/taxcalcs/money"
	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
)

// hsaKind is the pre-tax line HSA contributions through payroll are on.
const hsaKind = factsv1.PretaxKind_PRETAX_KIND_HSA

// HoldHsa holds a person's HSA contributions through payroll, with their
// employers' own, to their limit. Payroll contributions through a
// cafeteria plan are employer contributions (Instructions for Form 8889,
// line 9), and what employers put in over the limit is income (Excess
// Employer Contributions). The excess comes off the payroll HSA lines of
// the paychecks to come, the last job's first, so it is in their wages,
// Social Security and Medicare wages, and the tax withheld on them; what
// is over on paychecks already made is other income. A state's wage lines
// stay: California, which taxes HSA contributions, has them in already.
// Call it after MaxOut and before Projection.
func HoldHsa(person string, limit money.USCents, jobs []*Job) error {
	var mine []*Job
	var asked, employer money.USCents
	for _, j := range jobs {
		if j.Person() != person {
			continue
		}
		if j.done {
			return errors.New("employer: HoldHsa after Projection")
		}
		mine = append(mine, j)
		asked += j.sumOf(j.items).pretaxLine(hsaKind).v
		employer += model.Value(j.m.GetEmployerHsa())
	}
	over := max(0, asked-max(0, limit-employer))
	// Employers' own contributions over the limit are income too, with no
	// payroll line to take them off.
	if len(mine) > 0 && employer > limit {
		mine[len(mine)-1].hsaInIncome += employer - limit
	}
	if over == 0 && employer <= limit {
		return nil
	}
	for k := len(mine) - 1; k >= 0 && over > 0; k-- {
		j := mine[k]
		cut := j.cutHsa(over)
		j.hsaInWages += cut
		over -= cut
	}
	// What is left is on paychecks already made: the last job's, as the
	// cut runs from it.
	for k := len(mine) - 1; k >= 0 && over > 0; k-- {
		j := mine[k]
		made := j.sumOf(j.made()).pretaxLine(hsaKind).v
		take := min(over, made)
		j.hsaInIncome += take
		over -= take
	}
	for _, j := range mine {
		j.hsaWorking(limit, asked, employer)
	}
	return nil
}

// cutHsa takes up to want off the payroll HSA lines of the paychecks to
// come, the latest first, and returns what it took.
func (j *Job) cutHsa(want money.USCents) money.USCents {
	var took money.USCents
	for k := len(j.items) - 1; k >= 0 && took < want; k-- {
		d := j.items[k]
		if d.status != scenariosv1.ItemStatus_ITEM_STATUS_EXPECTED {
			continue
		}
		l := d.p.pretaxLine(hsaKind)
		if !l.set || l.v <= 0 {
			continue
		}
		c := min(l.v, want-took)
		// Box 1 rises by the cut. California's wages, which hold the
		// contribution, stay as they were (see stateBase); a state
		// following the federal exclusion rises with box 1. Social
		// Security and Medicare wages rise too, so their tax is worked out
		// again on them.
		d.p.setPretax(hsaKind, setLine(l.v-c))
		d.computeSS, d.computeMedicare = true, true
		d.estimated = append(d.estimated, fmt.Sprintf("paycheck.pretax[%v].amount", hsaKind))
		took += c
	}
	return took
}

// hsaWorking records how the job's excess was made.
func (j *Job) hsaWorking(limit, asked, employer money.USCents) {
	over := j.hsaInWages + j.hsaInIncome
	if over == 0 {
		return
	}
	at := fmt.Sprintf("projection.employers[%s]", j.m.GetId())
	j.workings = append(j.workings, &scenariosv1.Working{Path: at + ".hsa_over",
		Terms: []*scenariosv1.Term{
			{Of: &scenariosv1.Term_Path{Path: at + ".hsa_over_in_wages"}, Sign: 1},
			{Of: &scenariosv1.Term_Path{Path: at + ".hsa_over_in_income"}, Sign: 1},
		},
		Rules: []*scenariosv1.AppliedRule{{Cite: "26 U.S.C. § 223(b) and § 106(d); Instructions for Form 8889, line 9 and Excess Employer Contributions",
			Applied: &scenariosv1.Statement{Text: fmt.Sprintf("%v through payroll and %v from employers is over the %v limit by %v; %v of it is this job's",
				money.USCents(model.Dollars(asked)), money.USCents(model.Dollars(employer)), money.USCents(model.Dollars(limit)), money.USCents(model.Dollars(asked+employer-limit)), money.USCents(model.Dollars(over))),
				States: []string{at + ".hsa_over"}}}}})
}
