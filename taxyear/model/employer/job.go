// Package employer expands an EmployerModel into its paychecks, by the rules
// in docs/models.md.
//
// [Expand] reads the job's evidence and fills its pay dates; [MaxOut] then
// sets the plan line on a person's expected paychecks across their jobs;
// [Job.Projection] computes the withholding left unset, in date order, and
// the totals.
package employer

import (
	"fmt"
	"time"

	"github.com/nexteggapp/taxcalcs/money"
	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/internal/civil"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	"github.com/nexteggapp/taxcalcs/taxyear/payroll"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
)

// Job is one employer model being expanded. It is made by [Expand], changed
// only by [MaxOut], and read by [Job.Projection].
type Job struct {
	ctx   *model.Context
	m     *scenariosv1.EmployerModel
	slots []civil.Date
	items []*draft
	notes model.Notes

	stubs    []*stub
	deposits []*deposit
	w2s      []*formw2.FormW2
	w2Parts  []string

	// latest is the latest evidence date; zero with no evidence.
	latest civil.Date
	// regular is the latest regular paystub, which SameAsLatest repeats.
	regular *stub
	// end is the last pay date, known or assumed; zero for none.
	end civil.Date
	// filled holds the pay dates evidence fills, by date. A paystub can
	// fill a pay date after its own date, so these are skipped when the
	// assumed parts fill the rest.
	filled map[string]*draft
	// ytdAt is the date of the latest paystub with a year-to-date column,
	// through which its ADJUSTMENT covers the pay dates; zero for none.
	ytdAt civil.Date

	// workings are the overrides' workings.
	workings []*scenariosv1.Working

	done bool
	// hsaInWages and hsaInIncome are the job's HSA contributions over the
	// person's limit: taken off the paychecks to come, and on paychecks
	// already made (HoldHsa).
	hsaInWages, hsaInIncome money.USCents
	// hsaTyped is the part of hsaInIncome on paychecks a YearTotal part
	// fills, which the split would otherwise have taken off them.
	hsaTyped money.USCents
	// planTyped is what max-out would set on each paycheck a YearTotal
	// part fills, by item id; it sets none of them.
	planTyped map[string]planShare
}

// draft is one item while the job is expanded.
type draft struct {
	id       string
	date     civil.Date
	status   scenariosv1.ItemStatus
	kind     scenariosv1.ItemKind
	origin   *scenariosv1.Origin
	p        pay
	takeHome line
	// estimated lists fields filled by a rule, by path in the item.
	estimated []string

	// What the withholding pass computes on an expected item.
	computeFederal, computeSS, computeMedicare, computeStates bool
	// supplemental marks a bonus or vest withheld by the supplemental rules,
	// and federalRate a typed rate for it.
	supplemental bool
	federalRate  *money.FineRate
	stateRate    *money.FineRate
	// planSlot marks an expected regular paycheck max-out may set.
	planSlot bool
	// typedYear marks a paycheck a YearTotal part fills, whose figures
	// the HSA limit doesn't change.
	typedYear bool
	// typedStates holds state lines a part typed, which win.
	typedStates []stateLine
	// stateDiff is each state's wages less its base on the paystub copied:
	// box 1, and for California box 1 with the payroll HSA (see stateBase).
	stateDiff map[string]money.USCents
}

func (d *draft) estimate(path string) {
	for _, e := range d.estimated {
		if e == path {
			return
		}
	}
	d.estimated = append(d.estimated, path)
}

// ID is the model's id.
func (j *Job) ID() string { return j.m.GetId() }

// Person is the id of the person whose job it is.
func (j *Job) Person() string { return j.m.GetPerson() }

// Notes returns what the expansion asked and left out.
func (j *Job) Notes() *model.Notes { return &j.notes }

// Expand reads an employer model's evidence and fills its pay dates by the
// models doc's order: a whole-year part, then evidence, then an assumed
// period part, then the default part. Withholding left unset is computed by
// [Job.Projection], after [MaxOut] has had its turn. overrides are the
// scenario's overrides naming this model's figures so far.
func Expand(ctx *model.Context, m *scenariosv1.EmployerModel, overrides []*scenariosv1.Override) (*Job, error) {
	j := &Job{ctx: ctx, m: m, planTyped: map[string]planShare{}}
	first, err := civil.FromProto(m.GetFirstPayDate())
	if err != nil {
		return nil, fmt.Errorf("employers[%s].first_pay_date: %w", m.GetId(), err)
	}
	starts, err := civil.FromProto(m.GetStarts())
	if err != nil {
		return nil, fmt.Errorf("employers[%s].starts: %w", m.GetId(), err)
	}
	j.slots = payDates(ctx.Year(), frequencyOf(m.GetPayFrequency()), first, starts)
	if err := j.readEvidence(); err != nil {
		return nil, err
	}
	j.matchEvidence()
	j.yearToDate()
	// An override of a figure so far is in the evidence a year total is
	// measured against. A W-2 states the year, so with one the override
	// adjusts what the W-2 gives.
	early := len(j.w2s) == 0
	if early {
		if err := j.applyOverrides(overrides); err != nil {
			return nil, err
		}
	}
	if err := j.fill(); err != nil {
		return nil, err
	}
	j.fromW2()
	if !early {
		if err := j.applyOverrides(overrides); err != nil {
			return nil, err
		}
	}
	return j, nil
}

func frequencyOf(f scenariosv1.PayFrequency) payroll.Frequency {
	switch f {
	case scenariosv1.PayFrequency_PAY_FREQUENCY_WEEKLY:
		return payroll.Weekly
	case scenariosv1.PayFrequency_PAY_FREQUENCY_BIWEEKLY:
		return payroll.Biweekly
	case scenariosv1.PayFrequency_PAY_FREQUENCY_SEMI_MONTHLY:
		return payroll.SemiMonthly
	case scenariosv1.PayFrequency_PAY_FREQUENCY_MONTHLY:
		return payroll.Monthly
	}
	return payroll.FrequencyUnspecified
}

// PayDates returns a job's pay dates in the year: from first_pay_date by the
// frequency, none before starts. Twice-monthly and monthly pay dates on a
// weekend move to the Friday before, as payroll moves them.
func PayDates(year int, f scenariosv1.PayFrequency, first, starts civil.Date) []civil.Date {
	return payDates(year, frequencyOf(f), first, starts)
}

func payDates(year int, f payroll.Frequency, first, starts civil.Date) []civil.Date {
	jan1 := civil.Must(year, time.January, 1)
	dec31 := civil.Must(year, time.December, 31)
	var out []civil.Date
	switch f {
	case payroll.Weekly, payroll.Biweekly:
		step := 7
		if f == payroll.Biweekly {
			step = 14
		}
		at := first
		if at.IsZero() {
			// The first Friday of the year.
			at = jan1.AddDays((int(time.Friday) - int(jan1.Weekday()) + 7) % 7)
		}
		for !at.Before(jan1) {
			at = at.AddDays(-step)
		}
		for at = at.AddDays(step); !at.After(dec31); at = at.AddDays(step) {
			out = append(out, at)
		}
	case payroll.SemiMonthly:
		d1, d2 := semiMonthlyDays(first)
		for m := time.January; m <= time.December; m++ {
			out = append(out, dayIn(year, m, d1).BusinessDayOnOrBefore(), dayIn(year, m, d2).BusinessDayOnOrBefore())
		}
	case payroll.Monthly:
		day := 31
		if !first.IsZero() && first.Day() < civil.EndOfMonth(first.Year(), first.Month()).Day() {
			day = first.Day()
		}
		for m := time.January; m <= time.December; m++ {
			out = append(out, dayIn(year, m, day).BusinessDayOnOrBefore())
		}
	}
	kept := out[:0]
	for _, d := range out {
		if starts.IsZero() || !d.Before(starts) {
			kept = append(kept, d)
		}
	}
	return kept
}

// semiMonthlyDays is the two days of the month a twice-monthly job pays: the
// 15th and the last day, unless first_pay_date names another day d before
// the 15th, which pays d and d + 15. 31 stands for the month's last day.
func semiMonthlyDays(first civil.Date) (int, int) {
	if first.IsZero() || first.Day() >= 15 {
		return 15, 31
	}
	return first.Day(), first.Day() + 15
}

// dayIn is day of the month, or its last day when the month is shorter.
func dayIn(year int, m time.Month, day int) civil.Date {
	end := civil.EndOfMonth(year, m)
	if day >= end.Day() {
		return end
	}
	return civil.Must(year, m, day)
}

// halfPeriod is how far a paystub or deposit may be from a pay date and
// still be that pay date's.
func (j *Job) halfPeriod() int {
	switch frequencyOf(j.m.GetPayFrequency()) {
	case payroll.Weekly:
		return 3
	case payroll.Biweekly:
		return 7
	case payroll.SemiMonthly:
		return 7
	}
	return 15
}

// path is the model's path in the scenario's inputs.
func (j *Job) path() string { return fmt.Sprintf("model.employers[%s]", j.m.GetId()) }

// itemPrefix is the start of every item id: "employers/acme".
func (j *Job) itemPrefix() string { return model.ItemID("employers", j.m.GetId()) }

func (j *Job) slotID(d civil.Date) string { return model.ItemID(j.itemPrefix(), d.String(), "regular") }

func kindWord(k factsv1.PaycheckKind) string {
	switch k {
	case factsv1.PaycheckKind_PAYCHECK_KIND_BONUS:
		return "bonus"
	case factsv1.PaycheckKind_PAYCHECK_KIND_VEST:
		return "vest"
	case factsv1.PaycheckKind_PAYCHECK_KIND_FINAL:
		return "final"
	}
	return "regular"
}

func itemKind(k factsv1.PaycheckKind) scenariosv1.ItemKind {
	switch k {
	case factsv1.PaycheckKind_PAYCHECK_KIND_BONUS:
		return scenariosv1.ItemKind_ITEM_KIND_BONUS
	case factsv1.PaycheckKind_PAYCHECK_KIND_VEST:
		return scenariosv1.ItemKind_ITEM_KIND_VEST
	case factsv1.PaycheckKind_PAYCHECK_KIND_FINAL:
		return scenariosv1.ItemKind_ITEM_KIND_FINAL
	}
	return scenariosv1.ItemKind_ITEM_KIND_PAYCHECK
}

// made returns the items read from evidence.
func (j *Job) made() []*draft {
	var out []*draft
	for _, d := range j.items {
		if d.status == scenariosv1.ItemStatus_ITEM_STATUS_MADE {
			out = append(out, d)
		}
	}
	return out
}

func (j *Job) sumOf(ds []*draft) pay {
	ps := make([]pay, len(ds))
	for i, d := range ds {
		ps[i] = d.p
	}
	return sumPay(ps)
}
