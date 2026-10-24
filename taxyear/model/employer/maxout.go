package employer

import (
	"errors"
	"fmt"
	"sort"

	"github.com/nexteggapp/taxcalcs/money"
	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/internal/civil"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	"github.com/nexteggapp/taxcalcs/taxyear/payroll"
	"github.com/nexteggapp/taxcalcs/ustaxcalc"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
)

// planSlot is one expected paycheck max-out may set, with what it can take.
type planSlot struct {
	job  *Job
	d    *draft
	room money.USCents
	v    money.USCents
}

// MaxOut applies a person's MaxOutWorkplacePlan or WorkplacePlanTotal part
// to their jobs, as the models doc's "Maxing out a workplace plan" says:
//
//  1. The amount is the § 402(g) limit plus the § 414(v) catch-up for the
//     person's age, or the typed total held to that, less every job's
//     deferrals so far, pre-tax and Roth, on pay dates through as_of.
//  2. It is shared equally across the person's expected regular paychecks
//     after as_of at every job with a 401(k) or 403(b), or whose plan isn't
//     said, whose share counts as Roth and is asked about. None pays out less
//     than $0 after its withholding; what one can't take goes to the
//     others.
//  3. Each job keeps its latest paystub's pre-tax and Roth split. The
//     catch-up is Roth when last year's box 3 from that employer exceeded
//     $150,000; without that W-2 it stays pre-tax and the result asks.
//  4. Box 1 falls by the pre-tax share. Computed withholding is computed
//     again by [Job.Projection]; typed withholding moves at the paycheck's
//     own rate.
//
// One-offs and final payments are never set. jobs may hold other people's
// jobs, which are left alone. It returns what it asked.
func MaxOut(ctx *model.Context, person string, part *scenariosv1.RetirementPart, jobs []*Job) (*model.Notes, error) {
	notes := &model.Notes{}
	var mine []*Job
	// typed is the deferrals on paychecks to come that a YearTotal part
	// fills, which max-out can't change.
	var soFar, typed money.USCents
	for _, j := range jobs {
		if j.Person() != person {
			continue
		}
		if j.done {
			return nil, errors.New("employer: MaxOut after Projection")
		}
		soFar += j.sumOf(j.covered()).deferrals()
		typed += j.sumOf(j.typedAhead()).deferrals()
		if hasPlan(j.m) {
			mine = append(mine, j)
		}
		if j.m.GetRetirementPlan() == scenariosv1.RetirementPlan_RETIREMENT_PLAN_UNSPECIFIED {
			// A job whose plan isn't said takes its share with no pre-tax
			// effect, as Roth, so the assumption doesn't lower the tax.
			mine = append(mine, j)
			notes.Ask(model.NewQuestion(model.ItemID(j.itemPrefix(), "retirement-plan"), j.itemPrefix(),
				fmt.Sprintf("Does %s have a 401(k) or 403(b), and are your deferrals there pre-tax or Roth?", j.m.GetName()),
				"Its paychecks to come take a share of your workplace plan, counted as Roth, with no pre-tax effect, until you say.",
				j.path()+".retirement_plan"))
		}
	}
	age, known := ctx.AgeAtYearEnd(person)
	base, catchUp, err := payroll.PlanLimit(ctx.Year(), age)
	if errors.Is(err, ustaxcalc.ErrNoTable) {
		notes.LeaveOut(fmt.Sprintf("Maxing out the workplace plan, which needs the § 402(g) limit for %d", ctx.Year()))
		return notes, nil
	}
	if err != nil {
		return nil, err
	}
	if !known {
		catchUp = 0
		notes.Ask(model.NewQuestion("retirement/"+person+"/date-of-birth", "model.retirement",
			"What is your date of birth? The workplace plan limit is higher from 50.",
			"No catch-up is counted until you say.",
			"facts.people["+person+"].date_of_birth"))
	}
	target := base + catchUp
	if t := part.GetWorkplacePlanTotal(); t != nil {
		target = min(target, model.Value(t.GetTotal()))
	}
	// What max-out would put on the typed paychecks too is kept for their
	// workings' automatic.
	all := max(0, target-soFar)
	eachShare(shareOut(mine, all, true), all, max(0, all-max(0, base-soFar)), func(s *planSlot, catch money.USCents) {
		if s.d.typedYear {
			s.job.planTyped[s.d.id] = planShare{s.v, catch}
		}
	})
	left := max(0, target-soFar-typed)
	eachShare(shareOut(mine, left, false), left, max(0, left-max(0, base-soFar-typed)), func(s *planSlot, catch money.USCents) {
		s.job.setPlan(s.d, s.v, catch, notes)
	})
	return notes, nil
}

// planShare is a paycheck's plan line and its catch-up part.
type planShare struct{ v, catch money.USCents }

// eachShare calls f with each slot and its catch-up share, cut from the
// running total so the shares add to the whole catch-up to the cent.
func eachShare(slots []*planSlot, left, catchLeft money.USCents, f func(*planSlot, money.USCents)) {
	var through, catchSoFar money.USCents
	for _, s := range slots {
		through += s.v
		catch := model.Scale(through, catchLeft, left) - catchSoFar
		catchSoFar += catch
		f(s, catch)
	}
}

// typedAhead is the paychecks after as_of and the evidence that a YearTotal
// part fills.
func (j *Job) typedAhead() []*draft {
	var out []*draft
	cut := civil.Later(j.latest, j.ctx.AsOf())
	for _, d := range j.typedYear() {
		if d.date.After(cut) {
			out = append(out, d)
		}
	}
	return out
}

// covered is the items up to the latest evidence or as_of, whichever is
// later: the made ones and the pay dates already past, whose deferrals are
// so far and which max-out can't change.
func (j *Job) covered() []*draft {
	var out []*draft
	cut := civil.Later(j.latest, j.ctx.AsOf())
	for _, d := range j.items {
		if d.status == scenariosv1.ItemStatus_ITEM_STATUS_MADE || !d.date.After(cut) {
			out = append(out, d)
		}
	}
	return out
}

func hasPlan(m *scenariosv1.EmployerModel) bool {
	return m.GetRetirementPlan() == scenariosv1.RetirementPlan_RETIREMENT_PLAN_401K ||
		m.GetRetirementPlan() == scenariosv1.RetirementPlan_RETIREMENT_PLAN_403B
}

// shareOut shares left equally across the jobs' plan slots, smallest room
// first, so a paycheck that can't take an equal share passes the rest on.
// The paychecks a YearTotal part fills are left out unless typed is set.
func shareOut(jobs []*Job, left money.USCents, typed bool) []*planSlot {
	var slots []*planSlot
	for _, j := range jobs {
		for _, d := range j.items {
			if d.planSlot && d.status == scenariosv1.ItemStatus_ITEM_STATUS_EXPECTED && d.date.After(j.ctx.AsOf()) && (typed || !d.typedYear) {
				slots = append(slots, &planSlot{job: j, d: d, room: j.room(d)})
			}
		}
	}
	order := append([]*planSlot(nil), slots...)
	sort.SliceStable(order, func(a, b int) bool { return order[a].room < order[b].room })
	for k, s := range order {
		share := left / money.USCents(len(order)-k)
		s.v = min(s.room, share)
		left -= s.v
	}
	return slots
}

// room is the most a paycheck's plan line can take and still pay out
// nothing less than $0. Social Security, Medicare, typed state withholding
// and a set amount per paycheck don't fall with a deferral. Typed federal
// and state withholding at the paystub's rate fall with taxable pay, at
// rate r, so taxable pay T must cover the fixed amounts F and r of itself:
// T is at least F / (1 - r).
func (j *Job) room(d *draft) money.USCents {
	fixed, num, den := j.fixedOut(d), money.USCents(0), d.p.taxable()
	if !d.computeFederal {
		num += d.p.federal.v
	}
	if r, of := j.stateRate(d); of > 0 && den > 0 {
		num += model.Scale(den, r, of)
	}
	least := fixed
	if pre, _ := j.split(); pre == 0 {
		// A Roth deferral lowers no withholding.
		return max(0, d.p.gross.v-j.otherPretax(d.p)-fixed-num)
	}
	switch {
	case den <= 0:
	case num >= den:
		return 0
	case num > 0:
		least = model.Scale(fixed, den, den-num) + 1
	}
	return max(0, d.p.gross.v-j.otherPretax(d.p)-least)
}

// fixedOut is what a paycheck pays out that a deferral doesn't lower:
// Social Security and Medicare, typed or at 6.2% and 1.45% of the pay they
// are on, typed state withholding, and the employer's state withholding
// per paycheck.
func (j *Job) fixedOut(d *draft) money.USCents {
	ss, medicare := d.p.ss.v, d.p.medicare.v
	if !d.p.ss.set || d.computeSS {
		ss = money.Rate(620).Of(d.p.fica())
	}
	if !d.p.medicare.set || d.computeMedicare {
		medicare = money.Rate(145).Of(d.p.fica())
	}
	out := ss + medicare
	for _, s := range d.typedStates {
		out += s.withheld.v
	}
	for _, s := range j.m.GetStateWithholding() {
		out += model.Value(s.GetPerPaycheck())
	}
	return out
}

// stateRate is the latest paystub's state withholding over its state wages,
// which a paycheck with no typed state line is withheld at.
func (j *Job) stateRate(d *draft) (withheld, of money.USCents) {
	if len(d.typedStates) > 0 || len(j.m.GetStateWithholding()) > 0 || j.regular == nil {
		return 0, 0
	}
	for _, l := range j.regular.this.states {
		withheld, of = withheld+l.withheld.v, of+l.wages.v
	}
	return withheld, of
}

// planKinds are the job's plan lines: its pre-tax kind, and whether Roth
// deferrals go on the 403(b) line.
func (j *Job) planKinds() (factsv1.PretaxKind, bool) {
	if j.m.GetRetirementPlan() == scenariosv1.RetirementPlan_RETIREMENT_PLAN_403B {
		return factsv1.PretaxKind_PRETAX_KIND_403B, true
	}
	return factsv1.PretaxKind_PRETAX_KIND_401K, false
}

// otherPretax is a paycheck's pre-tax lines other than the plan's.
func (j *Job) otherPretax(p pay) money.USCents {
	k, _ := j.planKinds()
	return p.pretaxTotal() - p.pretax[k].v
}

// setPlan sets one paycheck's plan lines to v, catch of it catch-up, split
// as the latest paystub splits pre-tax and Roth.
func (j *Job) setPlan(d *draft, v, catch money.USCents, notes *model.Notes) {
	kind, b403 := j.planKinds()
	pre, roth := j.split()
	regular := v - catch
	preTax := model.Scale(regular, pre, pre+roth)
	if catch > 0 {
		switch j.catchUp(notes) {
		case catchUpRoth:
			// All of the catch-up is Roth.
		case catchUpNone:
			v -= catch
		default:
			preTax += model.Scale(catch, pre, pre+roth)
		}
	}
	rothV := v - preTax
	before := d.p.taxable()
	d.p.setPretax(kind, setLine(preTax))
	if b403 {
		d.p.roth403b = setLine(rothV)
	} else {
		d.p.roth401k = setLine(rothV)
	}
	d.estimate(fmt.Sprintf("paycheck.pretax[%d].amount", pretaxIndex(d.p, kind)))
	after := d.p.taxable()
	if before == after {
		return
	}
	if !d.computeFederal {
		d.p.federal = setLine(model.Scale(d.p.federal.v, after, before))
	}
	for i, s := range d.typedStates {
		if s.withheld.set {
			d.typedStates[i].withheld = setLine(model.Scale(s.withheld.v, after, before))
		}
		if s.wages.set {
			d.typedStates[i].wages = setLine(s.wages.v + after - before)
		}
	}
}

// split is the latest paystub's plan lines, pre-tax and Roth, which
// max-out keeps; all pre-tax without one.
func (j *Job) split() (pre, roth money.USCents) {
	if j.m.GetRetirementPlan() == scenariosv1.RetirementPlan_RETIREMENT_PLAN_UNSPECIFIED {
		return 0, 1
	}
	if j.regular == nil {
		return 1, 0
	}
	kind, b403 := j.planKinds()
	pre, roth = j.regular.this.pretax[kind].v, j.regular.this.roth401k.v
	if b403 {
		roth = j.regular.this.roth403b.v
	}
	if pre+roth == 0 {
		return 1, 0
	}
	return pre, roth
}

// pretaxIndex is the position of a pre-tax kind in the item's written
// figures.
func pretaxIndex(p pay, kind factsv1.PretaxKind) int {
	for i, x := range p.proto().GetPretax() {
		if x.GetKind() == kind {
			return i
		}
	}
	return 0
}

// catchUpRule is how a job's catch-up is deferred.
type catchUpRule int

const (
	// catchUpSplit follows the latest paystub's pre-tax and Roth split.
	catchUpSplit catchUpRule = iota
	// catchUpRoth is all Roth.
	catchUpRoth
	// catchUpNone allows none: it must be Roth and the plan has no Roth
	// option, § 1.414(v)-2(b)(2).
	catchUpNone
)

// catchUp decides a job's catch-up. It must be Roth when last year's box 3
// from the employer exceeded $150,000, § 414(v)(7), and the plan has a Roth
// option; with none, no catch-up is allowed. Without last year's W-2, or
// without the plan's roth_option, the rule isn't applied: the catch-up
// follows the paystub's split, and the result asks.
func (j *Job) catchUp(notes *model.Notes) catchUpRule {
	w := j.m.GetLastYear().GetW2()
	if w == nil {
		notes.Ask(model.NewQuestion(
			model.ItemID(j.itemPrefix(), "last-year-w2"),
			j.itemPrefix(),
			fmt.Sprintf("Did your Social Security wages from %s last year, W-2 box 3, exceed %v?", j.m.GetName(), payroll.RothCatchUpWages),
			"Above it, a catch-up contribution must be Roth. It is counted as your paychecks split it until you say.",
			j.path()+".last_year"))
		return catchUpSplit
	}
	if model.Value(w.GetSocialSecurityWages()) <= payroll.RothCatchUpWages {
		return catchUpSplit
	}
	switch {
	case j.m.RothOption == nil:
		notes.Ask(model.NewQuestion(
			model.ItemID(j.itemPrefix(), "roth-option"),
			j.itemPrefix(),
			fmt.Sprintf("Does %s's workplace plan take Roth contributions? Your catch-up must be Roth.", j.m.GetName()),
			"Without a Roth option no catch-up is allowed. It is counted as your paychecks split it until you say.",
			j.path()+".roth_option", "Yes", "No"))
		return catchUpSplit
	case j.m.GetRothOption():
		return catchUpRoth
	}
	return catchUpNone
}
