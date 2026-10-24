package employer

import (
	"fmt"
	"sort"

	"github.com/nexteggapp/taxcalcs/money"
	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/internal/civil"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
)

// period is a PayRate part with the day it takes effect.
type period struct {
	part  *scenariosv1.EmployerPart
	from  civil.Date // as typed; zero when unset
	start civil.Date // clipped to the day after the evidence
}

// fill makes the expected items: the covered pay dates nothing explains,
// the pay dates after the evidence up to any end, then the one-offs and a
// final payment.
func (j *Job) fill() error {
	if err := j.resolveEnd(); err != nil {
		return err
	}
	j.gaps()
	before := j.sumOf(j.items)
	rest := j.restSlots()
	periods, err := j.periods()
	if err != nil {
		return err
	}
	var drafts []*draft
	for _, slot := range rest {
		d := j.expected(slot, periods)
		if d == nil {
			continue
		}
		drafts = append(drafts, d)
		j.items = append(j.items, d)
	}
	if err := j.yearTotal(drafts, before); err != nil {
		return err
	}
	if err := j.oneOffs(); err != nil {
		return err
	}
	return nil
}

// gaps fills each covered pay date, up to the latest evidence, that has
// neither a paystub nor a deposit and that no year-to-date column covers:
// an expected paycheck copying the template, marked estimated, so nothing
// is understated. The template is the latest regular paystub before it,
// else the first after it, else an assumed PayRate. A W-2 states the year,
// so a job with one has no gaps.
func (j *Job) gaps() {
	if len(j.w2s) > 0 || j.latest.IsZero() {
		return
	}
	starts, _ := civil.FromProto(j.m.GetStarts())
	for _, slot := range j.slots {
		switch {
		case slot.After(j.latest), j.filled[slot.String()] != nil,
			!j.ytdAt.IsZero() && !slot.After(j.ytdAt),
			!starts.IsZero() && slot.Before(starts),
			!j.end.IsZero() && slot.After(j.end):
			continue
		}
		p, tmpl, ok := j.template(slot)
		if !ok {
			continue
		}
		d := &draft{id: j.slotID(slot), date: slot, status: scenariosv1.ItemStatus_ITEM_STATUS_EXPECTED,
			kind: scenariosv1.ItemKind_ITEM_KIND_PAYCHECK, p: p, estimated: copiedPaths(p),
			origin:         &scenariosv1.Origin{Basis: scenariosv1.Origin_BASIS_ASSUMED, Template: tmpl},
			computeFederal: !p.federal.set, computeSS: !p.ss.set, computeMedicare: !p.medicare.set, computeStates: true,
			stateDiff: map[string]money.USCents{}}
		for _, st := range p.states {
			d.stateDiff[st.state] = st.wages.v - stateBase(st.state, p)
		}
		d.p.states = nil
		j.filled[slot.String()] = d
		j.items = append(j.items, d)
	}
}

// restStart is the first day the assumed parts fill: the day after the
// evidence, else the job's first day, else January 1.
func (j *Job) restStart() civil.Date {
	if !j.latest.IsZero() {
		return j.latest.AddDays(1)
	}
	if s, err := civil.FromProto(j.m.GetStarts()); err == nil && !s.IsZero() {
		return s
	}
	return j.ctx.YearStart()
}

// restSlots are the pay dates after the evidence and up to any end. A W-2
// states the year, so a job with one has none.
func (j *Job) restSlots() []civil.Date {
	if len(j.w2s) > 0 {
		return nil
	}
	var out []civil.Date
	for _, s := range j.slots {
		if s.Before(j.restStart()) || (!j.end.IsZero() && s.After(j.end)) || j.filled[s.String()] != nil {
			continue
		}
		out = append(out, s)
	}
	return out
}

// resolveEnd reads the job's end part. A known end with evidence after it
// counts the evidence and asks about the date; an assumed end on or before
// the latest evidence stands aside.
func (j *Job) resolveEnd() error {
	for _, part := range j.m.GetParts() {
		switch {
		case part.GetEnded() != nil:
			d, err := civil.FromProto(part.GetEnded().GetLastPaycheck())
			if err != nil || d.IsZero() {
				return fmt.Errorf("%s.parts[%s].ended.last_paycheck is not a day", j.path(), part.GetId())
			}
			j.end = d
			if j.latest.After(d) && len(j.w2s) == 0 {
				j.notes.Ask(model.NewQuestion(
					model.ItemID(j.itemPrefix(), "ended", part.GetId()),
					j.itemPrefix(),
					fmt.Sprintf("Your evidence from %s runs to %s, after the last paycheck on %s. When was the last paycheck?", j.m.GetName(), j.latest.Short(), d.Short()),
					"The evidence after it counts.",
					fmt.Sprintf("%s.parts[%s].ended.last_paycheck", j.path(), part.GetId())))
			}
		case part.GetEnds() != nil:
			d, err := civil.FromProto(part.GetEnds().GetLastPaycheck())
			if err != nil || d.IsZero() {
				return fmt.Errorf("%s.parts[%s].ends.last_paycheck is not a day", j.path(), part.GetId())
			}
			// Evidence after an assumed end makes it stand aside. cmd/taxyear
			// can't see when the end was added, so an end on the day of
			// the latest evidence still counts.
			if d.Before(j.latest) {
				j.standAside(part.GetId(), fmt.Sprintf("Your %s evidence from %s is after this job's planned end on %s.", j.latest.Short(), j.m.GetName(), d.Short()), "Record the job as ended instead")
				continue
			}
			j.end = d
			j.finalPayment(part.GetId(), d, part.GetEnds().GetFinal())
		}
	}
	return nil
}

// standAside records that an assumed part counts for nothing, and why.
func (j *Job) standAside(part, why string, answers ...string) {
	j.notes.Ask(model.NewQuestion(
		model.ItemID(j.itemPrefix(), "stands-aside", part),
		j.itemPrefix(),
		why,
		"This change counts for nothing until it is changed or removed.",
		fmt.Sprintf("%s.parts[%s]", j.path(), part),
		append(answers, "Remove it")...))
}

// periods returns the PayRate parts that count, each from the day it takes
// effect. An unset start is the day after the evidence, an earlier one is
// clipped to it, and of several clipped to one day the latest-started
// counts; the others stand aside.
func (j *Job) periods() ([]period, error) {
	var ps []period
	for _, part := range j.m.GetParts() {
		r := part.GetPayRate()
		if r == nil {
			continue
		}
		from, err := civil.FromProto(r.GetFrom())
		if err != nil {
			return nil, fmt.Errorf("%s.parts[%s].pay_rate.from: %w", j.path(), part.GetId(), err)
		}
		start := j.restStart()
		if !from.IsZero() && from.After(start) {
			start = from
		}
		ps = append(ps, period{part: part, from: from, start: start})
	}
	byStart := map[string][]period{}
	for _, p := range ps {
		byStart[p.start.String()] = append(byStart[p.start.String()], p)
	}
	var out []period
	for _, group := range byStart {
		sort.SliceStable(group, func(a, b int) bool { return started(group[a]).Before(started(group[b])) })
		for _, p := range group[:len(group)-1] {
			j.standAside(p.part.GetId(), fmt.Sprintf("Your pay change from %s at %s starts before the evidence ends, and a later one replaces it.", started(p).Short(), j.m.GetName()))
		}
		out = append(out, group[len(group)-1])
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].start.Before(out[b].start) })
	return out, nil
}

// started is the day a period part says it starts: its from, or for an
// unset one the day it is clipped to, which is the latest possible.
func started(p period) civil.Date {
	if p.from.IsZero() {
		return p.start
	}
	return p.from
}

// expected is the item that fills one pay date after the evidence: the
// period part in effect on it, else the default part.
func (j *Job) expected(slot civil.Date, periods []period) *draft {
	d := &draft{id: j.slotID(slot), date: slot, status: scenariosv1.ItemStatus_ITEM_STATUS_EXPECTED,
		kind: scenariosv1.ItemKind_ITEM_KIND_PAYCHECK, planSlot: true, computeSS: true, computeMedicare: true, computeStates: true,
		origin: &scenariosv1.Origin{Basis: scenariosv1.Origin_BASIS_ASSUMED}}
	var active *period
	for i := range periods {
		if !periods[i].start.After(slot) {
			active = &periods[i]
		}
	}
	if active != nil {
		d.origin.Part = active.part.GetId()
		j.typed(d, active.part.GetPayRate().GetPaycheck())
		return d
	}
	def := j.m.GetDefaultPart()
	d.origin.Part = def.GetId()
	switch {
	case def.GetEachPaycheck() != nil:
		j.typed(d, def.GetEachPaycheck())
	case j.regular == nil && len(j.deposits) == 0 && j.hasYearTotal():
		// Nothing to repeat: the year total fills the pay date, and
		// withholding it doesn't state is computed.
		d.computeFederal = true
	case def.GetSameAsLatest() != nil:
		if !j.sameAsLatest(d) {
			return nil
		}
	default:
		j.notes.Ask(model.NewQuestion(
			model.ItemID(j.itemPrefix(), "default"),
			j.itemPrefix(),
			fmt.Sprintf("What will %s pay for the rest of the year?", j.m.GetName()),
			"Nothing is counted for its pay dates after the evidence until you say.",
			j.path()+".default_part"))
		return nil
	}
	return d
}

// typed fills an item from typed figures: a pre-tax line left unset is
// none, and a withholding line left unset is computed.
func (j *Job) typed(d *draft, f *factsv1.PayFigures) {
	d.p = payOf(f)
	d.p.net = line{}
	d.computeFederal = !d.p.federal.set
	d.computeSS, d.computeMedicare = !d.p.ss.set, !d.p.medicare.set
	d.typedStates = append([]stateLine(nil), d.p.states...)
	d.p.states = nil
}

// sameAsLatest fills an item from the latest regular paystub's current
// column. Its states are worked out from where the work is done, never the
// paystub's; its Social Security and Medicare tax at their rates.
func (j *Job) sameAsLatest(d *draft) bool {
	if j.regular == nil {
		if len(j.deposits) > 0 {
			last := j.deposits[len(j.deposits)-1]
			d.p = pay{gross: setLine(last.amount)}
			d.estimated = []string{"paycheck.gross"}
			d.computeFederal = true
			j.notes.Ask(model.NewQuestion(
				model.ItemID(j.itemPrefix(), "no-paystub"),
				j.itemPrefix(),
				fmt.Sprintf("What is gross pay on each paycheck from %s? There is no paystub to repeat.", j.m.GetName()),
				fmt.Sprintf("Each pay date counts the latest deposit, %v, as gross pay until you say.", last.amount),
				j.path()+".default_part.each_paycheck"))
			return true
		}
		j.notes.Ask(model.NewQuestion(
			model.ItemID(j.itemPrefix(), "no-paystub"),
			j.itemPrefix(),
			fmt.Sprintf("What is gross pay on each paycheck from %s?", j.m.GetName()),
			"Nothing is counted for its pay dates until you say.",
			j.path()+".default_part.each_paycheck"))
		return false
	}
	d.p = j.regular.this.clone()
	d.p.net = line{}
	d.computeFederal = !d.p.federal.set
	d.stateDiff = map[string]money.USCents{}
	for _, s := range d.p.states {
		d.stateDiff[s.state] = s.wages.v - stateBase(s.state, j.regular.this)
	}
	d.p.states = nil
	return true
}

// hasYearTotal reports a YearTotal part.
func (j *Job) hasYearTotal() bool {
	for _, p := range j.m.GetParts() {
		if p.GetYearTotal() != nil {
			return true
		}
	}
	return false
}

// yearTotal applies a YearTotal part: each line it states is the year less
// the evidence, shared equally across the pay dates after the evidence. It
// stands aside once a paystub newer than the one it was typed against is
// read.
func (j *Job) yearTotal(drafts []*draft, before pay) error {
	var part *scenariosv1.EmployerPart
	for _, p := range j.m.GetParts() {
		if p.GetYearTotal() != nil {
			part = p
		}
	}
	if part == nil {
		return nil
	}
	yt := part.GetYearTotal()
	typed, err := civil.FromProto(yt.GetTypedAgainst())
	if err != nil {
		return fmt.Errorf("%s.parts[%s].year_total.typed_against: %w", j.path(), part.GetId(), err)
	}
	for _, s := range j.stubs {
		if typed.IsZero() || s.date.After(typed) {
			j.standAside(part.GetId(), fmt.Sprintf("Your total for the year from %s was typed before your %s paystub arrived.", j.m.GetName(), s.date.Short()), "Type it again")
			return nil
		}
	}
	j.shareYear(part, payOf(yt.GetYear()), before, drafts)
	return nil
}

// yearTotalWorking records that a YearTotal part states the year figure at
// path, under the job's projection.
func (j *Job) yearTotalWorking(path string, part *scenariosv1.EmployerPart) {
	j.working(fmt.Sprintf("projection.employers[%s].%s", j.m.GetId(), path)).Parts = []string{part.GetId()}
}

// working is the job's working at path, made when first asked for.
func (j *Job) working(path string) *scenariosv1.Working {
	for _, w := range j.workings {
		if w.GetPath() == path {
			return w
		}
	}
	w := &scenariosv1.Working{Path: path}
	j.workings = append(j.workings, w)
	return w
}

// leftOver is the item that carries what a YearTotal states and no pay
// date can: an expected adjustment on December 31, whose figures are
// fixed. It is made when first asked for.
func (j *Job) leftOver(part *scenariosv1.EmployerPart, adj **draft) *draft {
	if *adj == nil {
		*adj = &draft{id: model.ItemID(j.itemPrefix(), "year-total", "adjustment"), date: j.ctx.YearEnd(),
			status: scenariosv1.ItemStatus_ITEM_STATUS_EXPECTED, kind: scenariosv1.ItemKind_ITEM_KIND_ADJUSTMENT,
			origin: &scenariosv1.Origin{Basis: scenariosv1.Origin_BASIS_ASSUMED, Part: part.GetId()}, typedYear: true}
		j.items = append(j.items, *adj)
	}
	return *adj
}

// shareYear shares each line a YearTotal states, less the evidence, across
// the drafts. Withholding it doesn't state is computed. With no drafts, the
// lines go on one adjustment.
func (j *Job) shareYear(part *scenariosv1.EmployerPart, year, soFar pay, drafts []*draft) {
	var adj *draft
	spread := func(want, have line, set func(d *draft, v money.USCents), path string) {
		if !want.set {
			return
		}
		if path != "pretax" {
			j.yearTotalWorking("year.items."+path, part)
		}
		left := want.v - have.v
		if left < 0 {
			j.notes.Ask(model.NewQuestion(
				model.ItemID(j.itemPrefix(), "year-total", path),
				j.itemPrefix(),
				fmt.Sprintf("Your total for the year from %s is less than so far, for %s.", j.m.GetName(), path),
				"Nothing more is counted for it this year.",
				fmt.Sprintf("%s.parts[%s].year_total.year", j.path(), part.GetId())))
			left = 0
		}
		if len(drafts) == 0 {
			set(j.leftOver(part, &adj), left)
			return
		}
		for i, v := range model.Share(left, len(drafts)) {
			set(drafts[i], v)
		}
	}
	spread(year.gross, soFar.gross, func(d *draft, v money.USCents) { d.p.gross = setLine(v) }, "gross")
	for _, k := range pretaxKinds {
		k := k
		spread(year.pretax[k], soFar.pretax[k], func(d *draft, v money.USCents) { d.p.setPretax(k, setLine(v)) }, "pretax")
	}
	spread(year.roth401k, soFar.roth401k, func(d *draft, v money.USCents) { d.p.roth401k = setLine(v) }, "roth_401k")
	spread(year.roth403b, soFar.roth403b, func(d *draft, v money.USCents) { d.p.roth403b = setLine(v) }, "roth_403b")
	spread(year.federal, soFar.federal, func(d *draft, v money.USCents) { d.p.federal = setLine(v) }, "federal_withheld")
	spread(year.ss, soFar.ss, func(d *draft, v money.USCents) { d.p.ss = setLine(v) }, "social_security_withheld")
	spread(year.medicare, soFar.medicare, func(d *draft, v money.USCents) { d.p.medicare = setLine(v) }, "medicare_withheld")
	for _, d := range drafts {
		d.origin.Part = part.GetId()
		d.typedYear = true
		d.computeFederal = !year.federal.set
		d.computeSS, d.computeMedicare = !year.ss.set, !year.medicare.set
	}
	j.shareStates(part, year, soFar, drafts, &adj)
}

// shareStates shares each state line a YearTotal states, less the
// evidence, as typed state lines, which win. A state's line is shared over
// the drafts whose work is done in that state, so each paycheck's state
// follows its date. California's wages are box 1 and the payroll HSA, as
// its year's box is (see totals), so only its withholding is typed.
// A line with no draft in its state goes on the adjustment.
func (j *Job) shareStates(part *scenariosv1.EmployerPart, year, soFar pay, drafts []*draft, adj **draft) {
	for _, s := range year.states {
		if s.withheld.set {
			j.yearTotalWorking(fmt.Sprintf("year.items.states[%s].tax_withheld", s.state), part)
		}
		if s.wages.set && s.state != "CA" {
			j.yearTotalWorking(fmt.Sprintf("year.items.states[%s].state_income", s.state), part)
		}
		var in []*draft
		for _, d := range drafts {
			for _, w := range j.workStates(d.date) {
				if w.state == s.state {
					in = append(in, d)
					break
				}
			}
		}
		have, _ := soFar.state(s.state)
		if len(in) == 0 {
			l := stateLine{state: s.state}
			if s.wages.set && s.state != "CA" {
				l.wages = setLine(max(0, s.wages.v-have.wages.v))
			}
			if s.withheld.set {
				l.withheld = setLine(max(0, s.withheld.v-have.withheld.v))
			}
			if l.wages.set || l.withheld.set {
				a := j.leftOver(part, adj)
				a.p.states = append(a.p.states, l)
			}
			continue
		}
		var wages, withheld []money.USCents
		if s.wages.set && s.state != "CA" {
			wages = model.Share(max(0, s.wages.v-have.wages.v), len(in))
		}
		if s.withheld.set {
			withheld = model.Share(max(0, s.withheld.v-have.withheld.v), len(in))
		}
		for i, d := range in {
			l := stateLine{state: s.state}
			if wages != nil {
				l.wages = setLine(wages[i])
			}
			if withheld != nil {
				l.withheld = setLine(withheld[i])
			}
			d.typedStates = append(d.typedStates, l)
		}
	}
}

// oneOffs adds each planned bonus or vest. Evidence of a bonus or vest
// within 31 days of one is asked about, "Is this the one you planned?";
// until it is answered both count.
func (j *Job) oneOffs() error {
	for _, part := range j.m.GetParts() {
		o := part.GetOneOff()
		if o == nil {
			continue
		}
		date, err := civil.FromProto(o.GetDate())
		if err != nil || date.IsZero() {
			return fmt.Errorf("%s.parts[%s].one_off.date is not a day", j.path(), part.GetId())
		}
		if j.knownFrom(part, date) {
			continue
		}
		kind := o.GetKind()
		if kind != factsv1.PaycheckKind_PAYCHECK_KIND_VEST {
			kind = factsv1.PaycheckKind_PAYCHECK_KIND_BONUS
		}
		d := &draft{id: model.ItemID(j.itemPrefix(), date.String(), kindWord(kind), part.GetId()), date: date,
			status: scenariosv1.ItemStatus_ITEM_STATUS_EXPECTED, kind: itemKind(kind),
			origin:         &scenariosv1.Origin{Basis: scenariosv1.Origin_BASIS_ASSUMED, Part: part.GetId()},
			p:              pay{gross: setLine(model.Value(o.GetAmount()))},
			computeFederal: true, computeSS: true, computeMedicare: true, computeStates: true, supplemental: true,
			federalRate: fineRate(o.GetFederalRate()), stateRate: fineRate(o.GetStateRate())}
		j.items = append(j.items, d)
	}
	return nil
}

// knownFrom reports whether a planned one-off's answer says a paystub is it,
// and otherwise asks about each bonus or vest paystub within 31 days.
func (j *Job) knownFrom(part *scenariosv1.EmployerPart, date civil.Date) bool {
	actual := part.GetOneOff().GetActual()
	if actual.GetIs() != "" {
		return true
	}
	not := map[string]bool{}
	for _, id := range actual.GetIsNot() {
		not[id] = true
	}
	for _, s := range j.stubs {
		if s.kind != factsv1.PaycheckKind_PAYCHECK_KIND_BONUS && s.kind != factsv1.PaycheckKind_PAYCHECK_KIND_VEST {
			continue
		}
		if not[s.id] || abs(s.date.DaysFrom(date)) > 31 {
			continue
		}
		j.notes.Ask(model.NewQuestion(
			model.ItemID(j.itemPrefix(), "planned", part.GetId(), s.id),
			s.item.id,
			fmt.Sprintf("Is your %s %s paystub from %s the one you planned for %s?", s.date.Short(), kindWord(s.kind), j.m.GetName(), date.Short()),
			"Both count until you say.",
			fmt.Sprintf("%s.parts[%s].one_off.actual", j.path(), part.GetId()),
			"Yes", "No"))
	}
	return false
}

// finalPayment adds the payment an assumed end pays with the last paycheck,
// withheld as given.
func (j *Job) finalPayment(part string, on civil.Date, f *scenariosv1.EmployerPart_Ends_FinalPayment) {
	if f == nil || model.Value(f.GetAmount()) == 0 {
		return
	}
	d := &draft{id: model.ItemID(j.itemPrefix(), on.String(), "final", part), date: on,
		status: scenariosv1.ItemStatus_ITEM_STATUS_EXPECTED, kind: scenariosv1.ItemKind_ITEM_KIND_FINAL,
		origin:    &scenariosv1.Origin{Basis: scenariosv1.Origin_BASIS_ASSUMED, Part: part},
		p:         pay{gross: setLine(model.Value(f.GetAmount())), federal: setLine(model.Value(f.GetFederalWithheld()))},
		computeSS: true, computeMedicare: true, computeStates: true}
	for _, st := range j.workStates(on) {
		d.typedStates = append(d.typedStates, stateLine{state: st.state, withheld: setLine(model.Scale(model.Value(f.GetStateWithheld()), st.days, st.total))})
	}
	j.items = append(j.items, d)
}

// fineRate reads a percentage as a rate in millionths, or nil when unset.
func fineRate(q *commonv1.Quantity) *money.FineRate {
	if q == nil {
		return nil
	}
	r := money.FineRate(q.GetUnits() * 10_000)
	for i := int32(0); i < q.GetScale(); i++ {
		r /= 10
	}
	return &r
}

// ofFine applies a rate in millionths, rounding half away from zero.
func ofFine(r money.FineRate, a money.USCents) money.USCents {
	return model.Scale(a, money.USCents(r), 1_000_000)
}
