package employer

import (
	"fmt"
	"sort"

	"github.com/nexteggapp/taxcalcs/money"
	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/internal/civil"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
	form1040 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1040"
)

// pairWindow is how many days from a paystub a deposit may be and pair with
// it.
const pairWindow = 3

// stub is one usable paystub.
type stub struct {
	id, part string
	date     civil.Date
	kind     factsv1.PaycheckKind
	this     pay
	ytd      pay
	hasYTD   bool
	status   form1040.FilingStatus
	match    *scenariosv1.Match
	// item is the item the stub is; set by matching.
	item *draft
	// paired are the deposits paired with it.
	paired []*deposit
}

// deposit is one payroll deposit.
type deposit struct {
	id, part string
	date     civil.Date
	amount   money.USCents
	match    *scenariosv1.Match
	used     bool
}

// readEvidence collects the job's usable paystubs, deposits and W-2s. A
// document the household removed, and an entry answered as not this
// source's, count for nothing.
func (j *Job) readEvidence() error {
	replaced := map[string]bool{}
	for _, part := range j.m.GetParts() {
		for _, r := range part.GetFromW2().GetReplaces() {
			replaced[refKey(r)] = true
		}
	}
	for _, part := range j.m.GetParts() {
		var err error
		switch {
		case part.GetFromPaystubs() != nil:
			err = j.readStubs(part)
		case part.GetFromDeposits() != nil:
			err = j.readDeposits(part)
		case part.GetFromW2() != nil:
			w := part.GetFromW2()
			if model.Usable(w.GetSource()) && !replaced[refKey(w.GetSource())] &&
				(w.GetW2().GetTaxYear() == 0 || int(w.GetW2().GetTaxYear()) == j.ctx.Year() || w.GetSource().GetUseAnyway()) {
				j.w2s = append(j.w2s, w.GetW2())
				j.w2Parts = append(j.w2Parts, part.GetId())
			}
		}
		if err != nil {
			return err
		}
	}
	sort.SliceStable(j.stubs, func(a, b int) bool { return j.stubs[a].date.Before(j.stubs[b].date) })
	sort.SliceStable(j.deposits, func(a, b int) bool { return j.deposits[a].date.Before(j.deposits[b].date) })
	for _, s := range j.stubs {
		j.latest = civil.Later(j.latest, s.date)
		if s.kind == factsv1.PaycheckKind_PAYCHECK_KIND_REGULAR {
			j.regular = s
		}
	}
	for _, d := range j.deposits {
		j.latest = civil.Later(j.latest, d.date)
	}
	if len(j.w2s) > 0 {
		j.latest = civil.Later(j.latest, j.ctx.YearEnd())
	}
	return nil
}

// readStubs reads a part's paystubs. Another year's paystub counts only
// when the household says to use it anyway.
func (j *Job) readStubs(part *scenariosv1.EmployerPart) error {
	for _, s := range part.GetFromPaystubs().GetStubs() {
		if !model.Usable(s.GetSource()) || s.GetMatches().GetNotThisSource() != nil {
			continue
		}
		d, err := civil.FromProto(s.GetPaystub().GetPayDate())
		if err != nil || d.IsZero() {
			return fmt.Errorf("%s: paystub %s has no pay date", j.path(), s.GetId())
		}
		if d.Year() != j.ctx.Year() && !s.GetSource().GetUseAnyway() {
			continue
		}
		st := &stub{id: s.GetId(), part: part.GetId(), date: d, kind: s.GetPaystub().GetKind(),
			this: payOf(s.GetPaystub().GetThisPaycheck()), match: s.GetMatches(),
			status: s.GetPaystub().GetWithholdingStatus()}
		if y := s.GetPaystub().GetYearToDate(); y != nil {
			st.ytd, st.hasYTD = payOf(y), true
		}
		j.stubs = append(j.stubs, st)
	}
	return nil
}

// readDeposits reads a part's deposits in the tax year.
func (j *Job) readDeposits(part *scenariosv1.EmployerPart) error {
	for _, dep := range part.GetFromDeposits().GetDeposits() {
		if dep.GetMatches().GetNotThisSource() != nil {
			continue
		}
		d, err := civil.FromProto(dep.GetDate())
		if err != nil || d.IsZero() {
			return fmt.Errorf("%s: deposit %s has no date", j.path(), dep.GetId())
		}
		if d.Year() != j.ctx.Year() {
			continue
		}
		j.deposits = append(j.deposits, &deposit{id: dep.GetId(), part: part.GetId(), date: d,
			amount: model.Value(dep.GetAmount()), match: dep.GetMatches()})
	}
	return nil
}

// refKey identifies a document by its store reference, or its name when it
// has none.
func refKey(r *factsv1.DocumentRef) string {
	if r.GetVaultRef() != "" {
		return "v:" + r.GetVaultRef()
	}
	return "n:" + r.GetName()
}

// matchEvidence turns the evidence into items: each regular paystub fills
// the pay date nearest it, a bonus, vest or final one is additive, deposits
// pair with the paystub they are, and the rest fill a pay date no paystub
// fills or are extra paychecks.
func (j *Job) matchEvidence() {
	filled := map[string]*draft{}
	j.filled = filled
	for _, s := range j.stubs {
		j.placeStub(s, filled)
	}
	j.pairExplicit(filled)
	for _, s := range j.stubs {
		if s.this.net.set {
			j.pairByNet(s)
		}
	}
	for _, s := range j.stubs {
		if !s.this.net.set {
			j.pairByDate(s)
		}
	}
	for _, s := range j.stubs {
		var take money.USCents
		for _, d := range s.paired {
			take += d.amount
			s.item.origin.Evidence = append(s.item.origin.Evidence, d.id)
		}
		if len(s.paired) > 0 {
			s.item.takeHome = setLine(take)
		}
	}
	j.slotDeposits(filled)
}

// placeStub makes a paystub's item.
func (j *Job) placeStub(s *stub, filled map[string]*draft) {
	d := &draft{date: s.date, status: scenariosv1.ItemStatus_ITEM_STATUS_MADE, kind: itemKind(s.kind),
		origin: &scenariosv1.Origin{Basis: scenariosv1.Origin_BASIS_DOCUMENT, Part: s.part, Evidence: []string{s.id}},
		p:      s.this.clone()}
	s.item = d
	j.items = append(j.items, d)
	if s.kind != factsv1.PaycheckKind_PAYCHECK_KIND_REGULAR {
		d.id = model.ItemID(j.itemPrefix(), s.date.String(), kindWord(s.kind), s.id)
		return
	}
	slot, ok := j.slotFor(s.date, s.match, filled)
	if !ok {
		d.id = model.ItemID(j.itemPrefix(), s.date.String(), "regular", s.id)
		j.notes.Ask(model.NewQuestion(
			model.ItemID(j.itemPrefix(), "paystub", s.id),
			d.id,
			fmt.Sprintf("Which pay date is your %s paystub from %s for?", s.date.Short(), j.m.GetName()),
			"It counts as an extra paycheck until you say.",
			fmt.Sprintf("%s.parts[%s].from_paystubs.stubs[%s].matches", j.path(), s.part, s.id),
			"A pay date", "Not this job's"))
		return
	}
	d.id = j.slotID(slot)
	filled[slot.String()] = d
}

// slotFor returns the pay date a paystub or deposit fills: the one its
// answer names, else the nearest within half a pay period that nothing has
// filled.
func (j *Job) slotFor(at civil.Date, m *scenariosv1.Match, filled map[string]*draft) (civil.Date, bool) {
	if m.GetSlot() != nil {
		d, err := civil.FromProto(m.GetSlot())
		if err == nil && filled[d.String()] == nil {
			return d, true
		}
	}
	best, bestDays := civil.Date{}, j.halfPeriod()+1
	for _, s := range j.slots {
		days := abs(at.DaysFrom(s))
		if days < bestDays && filled[s.String()] == nil {
			best, bestDays = s, days
		}
	}
	return best, !best.IsZero()
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// pairExplicit pairs each deposit whose answer names an item with that
// paystub's item.
func (j *Job) pairExplicit(filled map[string]*draft) {
	for _, d := range j.deposits {
		if d.match.GetItem() == "" {
			continue
		}
		for _, s := range j.stubs {
			if s.item.id == d.match.GetItem() {
				s.paired = append(s.paired, d)
				d.used = true
			}
		}
	}
}

// pairByNet pairs the deposits within three days of a paystub that sum to
// its net pay, so split deposits pair too. Two or more sets that do are a
// question; until it is answered the set nearest in date pairs, and of
// sets as near, the one with fewer deposits.
func (j *Job) pairByNet(s *stub) {
	cands := j.candidates(s)
	if len(cands) > 12 {
		cands = cands[:12]
	}
	var sets [][]*deposit
	for mask := 1; mask < 1<<len(cands); mask++ {
		var t money.USCents
		var set []*deposit
		for i, c := range cands {
			if mask&(1<<i) != 0 {
				t += c.amount
				set = append(set, c)
			}
		}
		if t == s.this.net.v {
			sets = append(sets, set)
		}
	}
	j.pairNearest(s, sets)
}

// candidates are the deposits within three days of a paystub that nothing
// has paired yet.
func (j *Job) candidates(s *stub) []*deposit {
	var out []*deposit
	for _, d := range j.deposits {
		if !d.used && d.match.GetSlot() == nil && abs(d.date.DaysFrom(s.date)) <= pairWindow {
			out = append(out, d)
		}
	}
	return out
}

// pairByDate pairs a paystub with no net pay with the deposits within three
// days of it, those on one day taken as one set. Sets on two or more days
// are a question; until it is answered the set nearest in date pairs.
func (j *Job) pairByDate(s *stub) {
	byDay := map[string][]*deposit{}
	var order []string
	for _, d := range j.candidates(s) {
		if _, ok := byDay[d.date.String()]; !ok {
			order = append(order, d.date.String())
		}
		byDay[d.date.String()] = append(byDay[d.date.String()], d)
	}
	var sets [][]*deposit
	for _, k := range order {
		sets = append(sets, byDay[k])
	}
	j.pairNearest(s, sets)
}

// pairNearest pairs the set nearest the paystub in date, asking when there
// is more than one.
func (j *Job) pairNearest(s *stub, sets [][]*deposit) {
	if len(sets) == 0 {
		return
	}
	distance := func(set []*deposit) int {
		n := 0
		for _, d := range set {
			n += abs(d.date.DaysFrom(s.date))
		}
		return n
	}
	sort.SliceStable(sets, func(a, b int) bool {
		if da, db := distance(sets[a]), distance(sets[b]); da != db {
			return da < db
		}
		return len(sets[a]) < len(sets[b])
	})
	if len(sets) > 1 {
		j.notes.Ask(model.NewQuestion(
			model.ItemID(j.itemPrefix(), "paystub", s.id, "deposits"),
			s.item.id,
			fmt.Sprintf("Which deposits are your %s paystub from %s? More than one set could be.", s.date.Short(), j.m.GetName()),
			"The deposits nearest the paystub's date are counted as it until you say.",
			fmt.Sprintf("%s.parts[%s].from_deposits.deposits", j.path(), sets[0][0].part)))
	}
	for _, d := range sets[0] {
		d.used = true
		s.paired = append(s.paired, d)
	}
}

// slotDeposits fills the pay dates no paystub fills from the deposits left:
// the deposits nearest one pay date, within three days of each other, are
// that paycheck, copying the latest regular paystub before it. Any other
// deposit is an extra paycheck with nothing withheld.
func (j *Job) slotDeposits(filled map[string]*draft) {
	for _, d := range j.deposits {
		if d.used {
			continue
		}
		slot, ok := j.slotFor(d.date, d.match, filled)
		if !ok {
			j.extraDeposit(d)
			continue
		}
		set := []*deposit{d}
		d.used = true
		for _, o := range j.deposits {
			if !o.used && o.match.GetSlot() == nil && abs(o.date.DaysFrom(d.date)) <= pairWindow {
				if s2, ok := j.slotFor(o.date, nil, filled); ok && s2 == slot {
					o.used = true
					set = append(set, o)
				}
			}
		}
		filled[slot.String()] = j.depositPaycheck(slot, set)
	}
}

// depositPaycheck is the paycheck a set of deposits is: the template, the
// latest regular paystub before it, copied and marked as estimated, paid as
// the deposits. With no template it is gross pay of the deposits, nothing
// withheld, and the result asks.
func (j *Job) depositPaycheck(slot civil.Date, set []*deposit) *draft {
	var take money.USCents
	var ids []string
	for _, d := range set {
		take += d.amount
		ids = append(ids, d.id)
	}
	d := &draft{id: j.slotID(slot), date: slot, status: scenariosv1.ItemStatus_ITEM_STATUS_MADE,
		kind:     scenariosv1.ItemKind_ITEM_KIND_PAYCHECK,
		origin:   &scenariosv1.Origin{Basis: scenariosv1.Origin_BASIS_LEDGER, Part: set[0].part, Evidence: ids},
		takeHome: setLine(take)}
	if p, tmpl, ok := j.template(slot); ok {
		d.p = p
		d.origin.Template = tmpl
		d.estimated = copiedPaths(d.p)
	} else {
		d.p = pay{gross: setLine(take), federal: setLine(0), ss: setLine(0), medicare: setLine(0)}
		d.estimated = []string{"paycheck.gross"}
		j.notes.Ask(model.NewQuestion(
			model.ItemID(j.itemPrefix(), "deposit", set[0].id),
			d.id,
			fmt.Sprintf("What was your gross pay on %s from %s? There is no paystub to read it from.", slot.Short(), j.m.GetName()),
			fmt.Sprintf("Your %v deposit counts as gross pay with nothing withheld until a paystub arrives.", take),
			fmt.Sprintf("%s.default_part.each_paycheck", j.path())))
	}
	j.items = append(j.items, d)
	return d
}

// template is the paycheck a pay date with no paystub of its own copies:
// the latest regular paystub on or before it; else the first after it;
// else the assumed PayRate in effect on it, or the earliest. It returns the
// figures, net left unset, and the paystub's or part's id.
func (j *Job) template(at civil.Date) (pay, string, bool) {
	if s := j.regularBefore(at); s != nil {
		p := s.this.clone()
		p.net = line{}
		return p, s.id, true
	}
	var inEffect, earliest *scenariosv1.EmployerPart
	var inFrom, earliestFrom civil.Date
	for _, part := range j.m.GetParts() {
		r := part.GetPayRate()
		if r == nil {
			continue
		}
		from, _ := civil.FromProto(r.GetFrom())
		if !from.After(at) && (inEffect == nil || !from.Before(inFrom)) {
			inEffect, inFrom = part, from
		}
		if earliest == nil || from.Before(earliestFrom) {
			earliest, earliestFrom = part, from
		}
	}
	best := inEffect
	if best == nil {
		best = earliest
	}
	if best == nil {
		return pay{}, "", false
	}
	p := payOf(best.GetPayRate().GetPaycheck())
	p.net = line{}
	return p, best.GetId(), true
}

// regularBefore is the latest regular paystub on or before the date, else
// the earliest after it.
func (j *Job) regularBefore(at civil.Date) *stub {
	var before, after *stub
	for _, s := range j.stubs {
		if s.kind != factsv1.PaycheckKind_PAYCHECK_KIND_REGULAR {
			continue
		}
		if !s.date.After(at) {
			before = s
		} else if after == nil {
			after = s
		}
	}
	if before != nil {
		return before
	}
	return after
}

// extraDeposit is a deposit no paystub or pay date explains: an extra
// paycheck, its gross the template's gross, or the deposit's with no
// template, and nothing withheld. The result asks what it is.
func (j *Job) extraDeposit(dep *deposit) {
	dep.used = true
	d := &draft{id: model.ItemID(j.itemPrefix(), dep.date.String(), "deposit", dep.id), date: dep.date,
		status: scenariosv1.ItemStatus_ITEM_STATUS_MADE, kind: scenariosv1.ItemKind_ITEM_KIND_DEPOSIT,
		origin:   &scenariosv1.Origin{Basis: scenariosv1.Origin_BASIS_LEDGER, Part: dep.part, Evidence: []string{dep.id}},
		takeHome: setLine(dep.amount)}
	gross := dep.amount
	if p, tmpl, ok := j.template(dep.date); ok && p.gross.set {
		gross = p.gross.v
		d.origin.Template = tmpl
	}
	d.p = pay{gross: setLine(gross), federal: setLine(0), ss: setLine(0), medicare: setLine(0)}
	d.estimated = []string{"paycheck.gross"}
	j.items = append(j.items, d)
	j.notes.Ask(model.NewQuestion(
		model.ItemID(j.itemPrefix(), "deposit", dep.id),
		d.id,
		fmt.Sprintf("What is the %v deposit on %s from %s?", dep.amount, dep.date.Short(), j.m.GetName()),
		fmt.Sprintf("It counts as %v of extra pay with nothing withheld until you say.", gross),
		fmt.Sprintf("%s.parts[%s].from_deposits.deposits[%s].matches", j.path(), dep.part, dep.id),
		"A pay date", "Part of a paystub", "Not pay"))
}

// copiedPaths lists every set line of copied figures, which a deposit's
// paycheck marks as estimated.
func copiedPaths(p pay) []string {
	var out []string
	add := func(l line, path string) {
		if l.set {
			out = append(out, path)
		}
	}
	add(p.gross, "paycheck.gross")
	for i, k := range pretaxKinds {
		if l, ok := p.pretax[k]; ok && l.set {
			out = append(out, fmt.Sprintf("paycheck.pretax[%d].amount", i))
		}
	}
	add(p.federal, "paycheck.federal_withheld")
	add(p.ss, "paycheck.social_security_withheld")
	add(p.medicare, "paycheck.medicare_withheld")
	add(p.roth401k, "paycheck.roth_401k")
	add(p.roth403b, "paycheck.roth_403b")
	for _, s := range p.states {
		add(s.wages, fmt.Sprintf("paycheck.states[%s].state_income", s.state))
		add(s.withheld, fmt.Sprintf("paycheck.states[%s].tax_withheld", s.state))
	}
	return out
}

// yearToDate adds an ADJUSTMENT at the latest paystub with a year-to-date
// column, for the difference between that column and the items through its
// date: pay dates with neither a paystub nor a deposit, and anything the
// paystubs read differently.
func (j *Job) yearToDate() {
	var at *stub
	for _, s := range j.stubs {
		if s.hasYTD {
			at = s
		}
	}
	if at == nil {
		return
	}
	j.ytdAt = at.date
	var through []*draft
	for _, d := range j.made() {
		if !d.date.After(at.date) {
			through = append(through, d)
		}
	}
	got := j.sumOf(through)
	var diff pay
	pick := func(want, have line) line {
		if !want.set {
			return line{}
		}
		return setLine(want.v - have.v)
	}
	diff.gross, diff.federal = pick(at.ytd.gross, got.gross), pick(at.ytd.federal, got.federal)
	diff.ss = pick(at.ytd.ss, got.ss)
	diff.medicare = pick(at.ytd.medicare, got.medicare)
	diff.roth401k = pick(at.ytd.roth401k, got.roth401k)
	diff.roth403b = pick(at.ytd.roth403b, got.roth403b)
	for k, v := range at.ytd.pretax {
		diff.setPretax(k, pick(v, got.pretax[k]))
	}
	for _, s := range at.ytd.states {
		h, _ := got.state(s.state)
		i := diff.stateIndex(s.state)
		diff.states[i].wages = pick(s.wages, h.wages)
		diff.states[i].withheld = pick(s.withheld, h.withheld)
	}
	diff = diff.nonzero()
	if diff.empty() {
		return
	}
	j.items = append(j.items, &draft{
		id: at.item.id + "/adjustment", date: at.date, status: scenariosv1.ItemStatus_ITEM_STATUS_MADE,
		kind:   scenariosv1.ItemKind_ITEM_KIND_ADJUSTMENT,
		origin: &scenariosv1.Origin{Basis: scenariosv1.Origin_BASIS_DOCUMENT, Part: at.part, Evidence: []string{at.id}},
		p:      diff,
	})
}
