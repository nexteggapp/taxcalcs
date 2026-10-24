package employer

import (
	"fmt"
	"sort"

	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/internal/civil"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	"github.com/nexteggapp/taxcalcs/taxyear/payroll"
	form1040 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1040"
)

// workShare is one state the work on a date is done in, and its share.
type workShare struct {
	state       string
	days, total money.USCents
}

// workStates returns where the job's work is done on a date, by `work`:
// one state; the state lived in that day; or each state of a split by its
// days. Never the copied paystub's states.
func (j *Job) workStates(on civil.Date) []workShare {
	w := j.m.GetWork()
	switch {
	case w.GetState() != "":
		return []workShare{{w.GetState(), 1, 1}}
	case w.GetSplit() != nil:
		var total money.USCents
		for _, s := range w.GetSplit().GetDays() {
			total += money.USCents(s.GetDays())
		}
		var out []workShare
		for _, s := range w.GetSplit().GetDays() {
			if total > 0 {
				out = append(out, workShare{s.GetState(), money.USCents(s.GetDays()), total})
			}
		}
		return out
	}
	if s := j.ctx.LivedIn(on); s != "" {
		return []workShare{{s, 1, 1}}
	}
	if j.regular != nil && len(j.regular.this.states) > 0 {
		return []workShare{{j.regular.this.states[0].state, 1, 1}}
	}
	return nil
}

// w4 is the W-4 federal withholding is figured from: the model's, else the
// latest paystub's filing status alone, else single alone.
func (j *Job) w4() (payroll.W4, error) {
	f := j.m.GetW4().GetForm()
	if f == nil {
		status := filing.Single
		if j.regular != nil && j.regular.status != form1040.FilingStatus_FILING_STATUS_UNSPECIFIED {
			status = statusOf(j.regular.status)
		}
		return payroll.NewW4(status).Build()
	}
	b := payroll.NewW4(statusOf(f.GetFilingStatus())).
		Dependents(model.Value(f.GetDependents())).
		OtherIncome(model.Value(f.GetOtherIncome())).
		Deductions(model.Value(f.GetDeductions())).
		Extra(model.Value(f.GetExtraWithholding()))
	if f.GetMultipleJobs() {
		b.MultipleJobs()
	}
	if f.GetExempt() {
		b.Exempt()
	}
	return b.Build()
}

// statusOf maps a form's filing status; unset reads as single.
func statusOf(s form1040.FilingStatus) filing.Status {
	switch s {
	case form1040.FilingStatus_FILING_STATUS_MARRIED_FILING_JOINTLY:
		return filing.MarriedFilingJointly
	case form1040.FilingStatus_FILING_STATUS_MARRIED_FILING_SEPARATELY:
		return filing.MarriedFilingSeparately
	case form1040.FilingStatus_FILING_STATUS_HEAD_OF_HOUSEHOLD:
		return filing.HeadOfHousehold
	case form1040.FilingStatus_FILING_STATUS_QUALIFYING_SURVIVING_SPOUSE:
		return filing.SurvivingSpouse
	}
	return filing.Single
}

// withhold computes every withholding line left unset on the expected
// items, in date order: federal income tax by Publication 15-T or the
// supplemental rules, Social Security up to the wage base and Medicare with
// the extra 0.9% over $200,000 for this job, and each state's.
func (j *Job) withhold() error {
	sort.SliceStable(j.items, func(a, b int) bool {
		if c := j.items[a].date.Compare(j.items[b].date); c != 0 {
			return c < 0
		}
		return j.items[a].id < j.items[b].id
	})
	w4, err := j.w4()
	if err != nil {
		return fmt.Errorf("%s.w4: %w", j.path(), err)
	}
	periods := frequencyOf(j.m.GetPayFrequency()).Periods()
	var ficaBefore, supplementalBefore money.USCents
	for _, d := range j.items {
		if d.status == scenariosv1.ItemStatus_ITEM_STATUS_EXPECTED && d.kind != scenariosv1.ItemKind_ITEM_KIND_ADJUSTMENT {
			if err := j.withholdOne(d, w4, periods, ficaBefore, supplementalBefore); err != nil {
				return err
			}
		}
		ficaBefore += d.p.fica()
		if d.kind == scenariosv1.ItemKind_ITEM_KIND_BONUS || d.kind == scenariosv1.ItemKind_ITEM_KIND_VEST {
			supplementalBefore += d.p.gross.v
		}
	}
	return nil
}

func (j *Job) withholdOne(d *draft, w4 payroll.W4, periods int, ficaBefore, supplementalBefore money.USCents) error {
	year := j.ctx.Year()
	ss, err := payroll.SocialSecurity(year, d.p.fica(), ficaBefore)
	if err != nil {
		return err
	}
	switch {
	case d.computeSS:
		d.p.ss = setLine(ss)
		d.estimate("paycheck.social_security_withheld")
	case d.p.ss.v > ss:
		// A copied or typed line stops at the wage base for this employer.
		d.p.ss = setLine(ss)
		d.estimate("paycheck.social_security_withheld")
	}
	if d.computeMedicare {
		d.p.medicare = setLine(payroll.Medicare(d.p.fica(), ficaBefore))
		d.estimate("paycheck.medicare_withheld")
	}
	if d.computeFederal {
		v, err := j.federal(d, w4, periods, supplementalBefore)
		if err != nil {
			return err
		}
		d.p.federal = setLine(v)
		d.estimate("paycheck.federal_withheld")
	}
	if d.computeStates {
		j.states(d)
	}
	return nil
}

// federal is the federal income tax withheld from one expected item.
func (j *Job) federal(d *draft, w4 payroll.W4, periods int, supplementalBefore money.USCents) (money.USCents, error) {
	year := j.ctx.Year()
	if !d.supplemental {
		if periods == 0 {
			return 0, fmt.Errorf("%s.pay_frequency is not set", j.path())
		}
		v, _, err := payroll.FederalWithholding(year, w4, d.p.taxable(), periods)
		return v, err
	}
	amount := d.p.gross.v
	if d.federalRate != nil {
		return ofFine(*d.federalRate, amount), nil
	}
	if j.m.GetSupplemental() == scenariosv1.SupplementalMethod_SUPPLEMENTAL_METHOD_AGGREGATE && periods > 0 {
		with := j.periodPaycheck(d.date)
		if with != nil {
			both, _, err := payroll.FederalWithholding(year, w4, with.p.taxable()+amount, periods)
			if err != nil {
				return 0, err
			}
			return max(0, both-with.p.federal.v), nil
		}
		v, _, err := payroll.FederalWithholding(year, w4, amount, periods)
		return v, err
	}
	return payroll.FlatSupplemental(amount, supplementalBefore), nil
}

// periodPaycheck is the regular paycheck a one-off is paid with: the first
// on or after its date.
func (j *Job) periodPaycheck(on civil.Date) *draft {
	for _, d := range j.items {
		if d.kind == scenariosv1.ItemKind_ITEM_KIND_PAYCHECK && !d.date.Before(on) {
			return d
		}
	}
	return nil
}

// states writes an expected item's state lines for where the work is done
// on its date. A line a part typed wins; then the employer's
// state_withholding, for a regular paycheck; then the latest paystub's rate
// for that state. A state that taxes no wages has its wages and nothing
// withheld. Failing all, the result asks for the amount per paycheck and
// counts nothing withheld.
func (j *Job) states(d *draft) {
	var lines []stateLine
	for _, w := range j.workStates(d.date) {
		typed, ok := typedLine(d.typedStates, w.state)
		if !ok {
			// A state that taxes no wages still has its wages, with
			// nothing withheld.
			wages := model.Scale(stateBase(w.state, d.p)+d.stateDiff[w.state], w.days, w.total)
			lines = append(lines, stateLine{state: w.state, wages: setLine(wages), withheld: setLine(j.stateWithheld(d, w.state, wages))})
			d.estimate(fmt.Sprintf("paycheck.states[%s].state_income", w.state))
			if !payroll.NoWageTax(w.state) {
				d.estimate(fmt.Sprintf("paycheck.states[%s].tax_withheld", w.state))
			}
			continue
		}
		l := typed
		if !l.wages.set {
			l.wages = setLine(model.Scale(stateBase(l.state, d.p)+d.stateDiff[l.state], w.days, w.total))
			d.estimate(fmt.Sprintf("paycheck.states[%s].state_income", l.state))
		}
		if !l.withheld.set {
			l.withheld = setLine(j.stateWithheld(d, l.state, l.wages.v))
			d.estimate(fmt.Sprintf("paycheck.states[%s].tax_withheld", l.state))
		}
		lines = append(lines, l)
	}
	d.p.states = lines
}

// typedLine is the line a part typed for a state, if any.
func typedLine(lines []stateLine, state string) (stateLine, bool) {
	for _, l := range lines {
		if l.state == state {
			return l, true
		}
	}
	return stateLine{}, false
}

// stateBase is the part of a paycheck a state's wages follow: box 1, and
// for California box 1 with the payroll HSA, which California doesn't
// exclude. A paystub's state wages are this and a difference of their own,
// so a paycheck to come with another HSA line keeps California's wages
// with it.
func stateBase(state string, p pay) money.USCents {
	if state == "CA" {
		return p.taxable() + p.pretaxLine(hsaKind).v
	}
	return p.taxable()
}

// stateWithheld is one state's withholding on an expected item: a typed
// rate for a one-off; the employer's state_withholding for a regular
// paycheck; the latest paystub's rate for that state; for a new state, the
// latest paystub's rate across its states, asked about.
func (j *Job) stateWithheld(d *draft, state string, wages money.USCents) money.USCents {
	if payroll.NoWageTax(state) {
		return 0
	}
	if d.supplemental && d.stateRate != nil {
		return ofFine(*d.stateRate, wages)
	}
	if !d.supplemental && d.kind == scenariosv1.ItemKind_ITEM_KIND_PAYCHECK {
		for _, s := range j.m.GetStateWithholding() {
			if s.GetState() == state {
				return model.Value(s.GetPerPaycheck())
			}
		}
	}
	if j.regular != nil {
		if l, ok := j.regular.this.state(state); ok && l.wages.v > 0 {
			return model.Scale(wages, l.withheld.v, l.wages.v)
		}
	}
	// A new state: taxcalcs has no state's withholding method, so the
	// latest paystub's rate across its states stands in, and the result
	// asks for the amount.
	var withheld, of money.USCents
	if j.regular != nil {
		for _, l := range j.regular.this.states {
			withheld, of = withheld+l.withheld.v, of+l.wages.v
		}
	}
	effect := fmt.Sprintf("Nothing is counted as withheld for %s until you say.", state)
	if of > 0 {
		effect = fmt.Sprintf("%s withholding is counted at your latest paystub's state rate until you say.", state)
	}
	j.notes.Ask(model.NewQuestion(
		model.ItemID(j.itemPrefix(), "state-withholding", state),
		j.itemPrefix(),
		fmt.Sprintf("How much %s income tax does %s withhold from each paycheck?", state, j.m.GetName()),
		effect, j.path()+".state_withholding"))
	if of == 0 {
		return 0
	}
	return model.Scale(wages, withheld, of)
}
