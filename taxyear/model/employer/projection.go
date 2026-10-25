package employer

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/nexteggapp/taxcalcs/money"
	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/internal/civil"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	"github.com/nexteggapp/taxcalcs/taxyear/payroll"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
)

// commonMoney is the proto money type, named for brevity.
type commonMoney = commonv1.Money

// OverridePrefix is the start of the path of an override of an employer's
// figures so far: "projection.employers[acme].so_far.items.".
func OverridePrefix(id string) string {
	return fmt.Sprintf("projection.employers[%s].so_far.items.", id)
}

// soFarThrough is the day the figures so far run to: the latest evidence
// date, else as_of.
func (j *Job) soFarThrough() civil.Date {
	if !j.latest.IsZero() {
		return j.latest
	}
	return j.ctx.AsOf()
}

// applyOverrides adds, for each override of a figure so far, an ADJUSTMENT
// dated so_far_through that keeps the items adding up to the typed figure.
func (j *Job) applyOverrides(overrides []*scenariosv1.Override) error {
	prefix := OverridePrefix(j.m.GetId())
	for _, o := range overrides {
		if !strings.HasPrefix(o.GetPath(), prefix) {
			continue
		}
		field := strings.TrimPrefix(o.GetPath(), prefix)
		have := j.sumOf(j.made())
		var adj pay
		if err := setField(&adj, have, field, model.Value(o.GetAmount())); err != nil {
			return fmt.Errorf("override %s: %w", o.GetPath(), err)
		}
		j.stateWagesFollow(&adj, have, field)
		var auto pay
		_ = setField(&auto, have, field, 0)
		automatic := -auto.lineSum()
		w := &scenariosv1.Working{Path: o.GetPath(), Automatic: model.Money(model.Dollars(automatic)), Sources: j.sourceIDs()}
		if o.GetWas() != nil {
			w.SourcesChanged = model.Value(o.GetWas()) != automatic
		}
		j.workings = append(j.workings, w)
		adj = adj.nonzero()
		if adj.empty() {
			continue
		}
		j.items = append(j.items, &draft{
			id: model.ItemID(j.itemPrefix(), "override", field), date: j.soFarThrough(),
			status: scenariosv1.ItemStatus_ITEM_STATUS_MADE, kind: scenariosv1.ItemKind_ITEM_KIND_ADJUSTMENT,
			origin: &scenariosv1.Origin{Basis: scenariosv1.Origin_BASIS_OVERRIDE, Override: o.GetPath()},
			p:      adj,
		})
	}
	return nil
}

// stateWagesFollow moves the state wages so far with an override of gross
// pay or a pre-tax line, which moves box 1: in the state the figures so far
// were earned in, the latest when there are several. California's wages
// keep the HSA, which it taxes. A state's own wages line, typed, stands.
func (j *Job) stateWagesFollow(adj *pay, have pay, field string) {
	if strings.HasPrefix(field, "states[") || len(have.states) == 0 {
		return
	}
	st := have.states[len(have.states)-1].state
	for _, w := range j.workStates(j.soFarThrough()) {
		if _, ok := have.state(w.state); ok {
			st = w.state
		}
	}
	d := adj.gross.v
	for k, l := range adj.pretax {
		if k == factsv1.PretaxKind_PRETAX_KIND_HSA && st == "CA" {
			continue
		}
		d -= l.v
	}
	if d != 0 {
		adj.states[adj.stateIndex(st)].wages = setLine(d)
	}
}

// sourceIDs are the ids of the parts and evidence the figures so far rest
// on.
func (j *Job) sourceIDs() []string {
	seen := map[string]bool{}
	var out []string
	for _, d := range j.made() {
		for _, id := range append([]string{d.origin.GetPart()}, d.origin.GetEvidence()...) {
			if id != "" && !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out
}

// Working returns each override's working: the figure computed today, and
// whether it differs from the one the person saw when they typed it.
func (j *Job) Working() []*scenariosv1.Working { return j.workings }

// setField sets, on adj, the line a so-far field path names to want less
// what the items have.
func setField(adj *pay, have pay, field string, want money.USCents) error {
	switch field {
	case "gross":
		adj.gross = setLine(want - have.gross.v)
	case "federal_withheld":
		adj.federal = setLine(want - have.federal.v)
	case "social_security_withheld":
		adj.ss = setLine(want - have.ss.v)
	case "medicare_withheld":
		adj.medicare = setLine(want - have.medicare.v)
	case "roth_401k":
		adj.roth401k = setLine(want - have.roth401k.v)
	case "roth_403b":
		adj.roth403b = setLine(want - have.roth403b.v)
	case "net":
		adj.net = setLine(want - have.net.v)
	default:
		return setListField(adj, have, field, want)
	}
	return nil
}

// setListField handles pretax[i].amount and states[XX].state_income or
// .tax_withheld. A pre-tax line is named by its position in the figures so
// far.
func setListField(adj *pay, have pay, field string, want money.USCents) error {
	open, close := strings.Index(field, "["), strings.Index(field, "]")
	if open < 0 || close < open {
		return fmt.Errorf("%q is not a line of pay figures", field)
	}
	list, key, rest := field[:open], field[open+1:close], field[close+1:]
	switch {
	case list == "pretax" && rest == ".amount":
		i, err := strconv.Atoi(key)
		kinds := have.proto().GetPretax()
		if err != nil || i < 0 || i >= len(kinds) {
			return fmt.Errorf("%q names no pre-tax line so far", field)
		}
		k := kinds[i].GetKind()
		adj.setPretax(k, setLine(want-have.pretax[k].v))
	case list == "states" && (rest == ".state_income" || rest == ".tax_withheld"):
		h, _ := have.state(key)
		i := adj.stateIndex(key)
		if rest == ".state_income" {
			adj.states[i].wages = setLine(want - h.wages.v)
		} else {
			adj.states[i].withheld = setLine(want - h.withheld.v)
		}
	default:
		return fmt.Errorf("%q is not a line of pay figures", field)
	}
	return nil
}

// Projection computes the withholding left unset, in date order, and
// returns the job's projection and what it asked. Call it once, after
// [MaxOut].
func (j *Job) Projection() (*scenariosv1.EmployerProjection, error) {
	if j.done {
		return nil, fmt.Errorf("%s: Projection called twice", j.path())
	}
	j.done = true
	var auto pay
	if j.hsaTyped != 0 || len(j.planTyped) > 0 {
		var err error
		if auto, err = j.splitTyped(); err != nil {
			return nil, err
		}
	}
	if err := j.withhold(); err != nil {
		return nil, err
	}
	j.roundItems()
	j.typedWorking(auto)
	out := &scenariosv1.EmployerProjection{Id: j.m.GetId(), SoFarThrough: j.latest.Proto()}
	var made, expected []*draft
	for _, d := range j.items {
		out.Items = append(out.Items, d.proto())
		if d.status == scenariosv1.ItemStatus_ITEM_STATUS_MADE {
			made = append(made, d)
		} else {
			expected = append(expected, d)
		}
	}
	model.SortItems(out.Items)
	base, err := payroll.WageBase(j.ctx.Year())
	if err != nil {
		return nil, err
	}
	hsaSoFar, hsaRest := j.employerHsa()
	caSoFar, caRest := californiaHsa(j.sumOf(made), j.sumOf(expected), hsaSoFar, hsaRest)
	soFar, rest, year := j.totals(made, base, hsaSoFar, caSoFar), j.totals(expected, base, hsaRest, caRest), j.totals(j.items, base, 0, 0)
	year.Boxes = combineBoxes(soFar.GetBoxes(), rest.GetBoxes(), 1)
	year.Boxes.SocialSecurityWages = model.Money(min(model.Value(year.GetBoxes().GetMedicareWages()), model.Dollars(base)))
	if len(j.w2s) > 0 {
		// The W-2 states the boxes; an override of a figure so far moves
		// them as it moves the items.
		boxes := w2Boxes(j.w2s)
		var typed []*draft
		for _, d := range j.items {
			if d.origin.GetOverride() != "" {
				typed = append(typed, d)
			}
		}
		if len(typed) > 0 {
			boxes = combineBoxes(boxes, j.totals(typed, base, 0, 0).GetBoxes(), 1)
		}
		soFar.Boxes, year.Boxes = boxes, boxes
	}
	rest.Boxes = combineBoxes(year.GetBoxes(), soFar.GetBoxes(), -1)
	out.SoFar, out.Rest, out.Year = soFar, rest, year
	if len(j.w2s) == 0 && len(j.items) > 0 {
		out.Expected = []*scenariosv1.ExpectedDocument{{Form: "FormW2", Due: model.FormsDue(j.ctx.Year()).Proto()}}
	}
	out.CanAdd = j.canAdd()
	if over := j.hsaInWages + j.hsaInIncome; over != 0 {
		d := model.Dollars
		out.HsaOverInWages, out.HsaOverInIncome = model.Money(d(j.hsaInWages)), model.Money(d(j.hsaInIncome))
		out.HsaOver = model.Money(d(j.hsaInWages) + d(j.hsaInIncome))
	}
	return out, nil
}

func (d *draft) proto() *scenariosv1.Item {
	return &scenariosv1.Item{
		Id: d.id, Date: d.date.Proto(), Status: d.status, Kind: d.kind, Origin: d.origin,
		Estimated: append([]string(nil), d.estimated...),
		Figures:   &scenariosv1.Item_Paycheck{Paycheck: d.p.proto()},
		TakeHome:  d.takeHome.money(),
	}
}

// employerHsa splits the employer's HSA contribution for the year between
// so far and the rest by pay date: each pay date carries an equal share.
func (j *Job) employerHsa() (soFar, rest money.USCents) {
	total := model.Value(j.m.GetEmployerHsa())
	if total == 0 || len(j.slots) == 0 {
		return 0, total
	}
	through, made := j.soFarThrough(), 0
	for _, d := range j.slots {
		if !d.After(through) {
			made++
		}
	}
	soFar = model.Scale(total, money.USCents(made), money.USCents(len(j.slots)))
	return soFar, total - soFar
}

// totals is the items' totals, and box totals worked out from them: box 1
// is gross less pre-tax lines; box 3 gross less HSA and other benefits,
// capped at the wage base for this employer; box 5 the same, uncapped; box
// 16 the items' state wages, or box 1 split by days for a job worked across
// states. A job's California wages from its paychecks' lines add the
// employer's own HSA; split by days they are box 1's share. hsa is
// the employer's own HSA for the period, which box 12 code W adds. Rounding
// happens once: so far's and the rest's box figures are each rounded to the
// dollar, half away from zero, and Projection makes the year their sum.
func (j *Job) totals(ds []*draft, base, hsa, caHsa money.USCents) *scenariosv1.PayTotals {
	p := j.sumOf(ds)
	var take line
	for _, d := range ds {
		take = take.plus(d.takeHome)
	}
	d := model.Dollars
	b := &scenariosv1.W2Boxes{
		Wages:                  model.Money(d(p.taxable())),
		FederalWithheld:        model.Money(d(p.federal.v)),
		SocialSecurityWages:    model.Money(min(d(p.fica()), d(base))),
		SocialSecurityWithheld: model.Money(d(p.ss.v)),
		MedicareWages:          model.Money(d(p.fica())),
		MedicareWithheld:       model.Money(d(p.medicare.v)),
		K401:                   model.Money(d(p.pretax[factsv1.PretaxKind_PRETAX_KIND_401K].v)),
		K403B:                  model.Money(d(p.pretax[factsv1.PretaxKind_PRETAX_KIND_403B].v)),
		Roth_401K:              model.Money(d(p.roth401k.v)),
		Roth_403B:              model.Money(d(p.roth403b.v)),
		Hsa:                    model.Money(d(p.pretax[factsv1.PretaxKind_PRETAX_KIND_HSA].v + hsa)),
	}
	if split := j.m.GetWork().GetSplit(); split != nil {
		withheld := map[string]money.USCents{}
		for _, s := range p.states {
			withheld[s.state] = s.withheld.v
		}
		// California's line has the employer's HSA, which it taxes, beside
		// its wages, as without a split.
		ca := func(st *scenariosv1.W2Boxes_State, wages money.USCents) *scenariosv1.W2Boxes_State {
			if st.State == "CA" {
				st.EmployerHsa = model.Money(d(caHsa))
				wages += d(caHsa)
			}
			st.Wages = model.Money(wages)
			return st
		}
		seen := map[string]bool{}
		for _, w := range j.workStates(j.ctx.YearEnd()) {
			seen[w.state] = true
			b.States = append(b.States, ca(&scenariosv1.W2Boxes_State{State: w.state, Withheld: model.Money(d(withheld[w.state]))},
				d(model.Scale(stateBase(w.state, p), w.days, w.total))))
		}
		// Tax a state withheld was paid there, whatever the split, so its
		// line keeps it, with the wages the items give it.
		for _, s := range p.states {
			if !seen[s.state] && (s.withheld.v != 0 || s.wages.v != 0) {
				b.States = append(b.States, ca(&scenariosv1.W2Boxes_State{State: s.state, Withheld: model.Money(d(s.withheld.v))}, d(s.wages.v)))
			}
		}
	} else {
		added := false
		items := p.proto()
		for _, s := range items.GetStates() {
			st := &scenariosv1.W2Boxes_State{State: s.GetState(), Withheld: model.Money(d(model.Value(s.GetTaxWithheld())))}
			wages := d(model.Value(s.GetStateIncome()))
			if s.GetState() == "CA" {
				added = true
				if len(p.states) == 1 {
					// Earned in California alone, the wages are box 1 and the
					// payroll HSA, which California doesn't exclude (Schedule
					// CA (540) line 1h adds code W back), each rounded, so they
					// add up as displayed. The paystubs' and
					// parts' own California lines aren't relied on; an
					// override stands, by what it adds.
					wages = model.Value(b.GetWages()) + shownHsa(items) + d(typedCA(ds))
				}
				// The employer's HSA is rounded once and added, so the
				// wages are the pay and it; it is set when there is none,
				// as the pay is the wages less it.
				st.EmployerHsa = model.Money(d(caHsa))
				wages += d(caHsa)
			}
			st.Wages = model.Money(wages)
			b.States = append(b.States, st)
		}
		if !added && caHsa != 0 {
			b.States = append(b.States, &scenariosv1.W2Boxes_State{State: "CA", Wages: model.Money(d(caHsa)), Withheld: model.Money(0), EmployerHsa: model.Money(d(caHsa))})
		}
	}
	return &scenariosv1.PayTotals{Items: p.proto(), TakeHome: take.money(), Boxes: b}
}

// shownHsa is the payroll HSA line of a period's item totals.
func shownHsa(f *factsv1.PayFigures) money.USCents {
	var out money.USCents
	for _, x := range f.GetPretax() {
		if x.GetKind() == factsv1.PretaxKind_PRETAX_KIND_HSA {
			out += model.Value(x.GetAmount())
		}
	}
	return out
}

// typedCA is what an override of California's wages so far adds beyond
// box 1 and the payroll HSA. A part has no field for California wages,
// so a part's California line, as a year total's, follows box
// 1 like the rest; an override of gross pay or a pre-tax line moves the
// line with box 1, and adds nothing.
func typedCA(ds []*draft) money.USCents {
	var out money.USCents
	for _, d := range ds {
		if !strings.HasSuffix(d.origin.GetOverride(), ".states[CA].state_income") {
			continue
		}
		if l, ok := d.p.state("CA"); ok {
			out += l.wages.v - stateBase("CA", d.p)
		}
	}
	return out
}

// californiaHsa is California's share of the employer's own HSA so far
// and for the rest of the year: California taxes it, so its wages include
// it, where other states follow the federal exclusion. Each period's share
// is in proportion to that period's California wages among its state
// wages. With no state wages to come, the rest goes where so far's went
// when that was California alone.
func californiaHsa(soFar, rest pay, hsaSoFar, hsaRest money.USCents) (money.USCents, money.USCents) {
	share := func(p pay, hsa money.USCents) (money.USCents, bool) {
		var ca, all money.USCents
		for _, s := range p.states {
			all += s.wages.v
			if s.state == "CA" {
				ca += s.wages.v
			}
		}
		if all == 0 {
			return 0, false
		}
		return model.Scale(hsa, ca, all), true
	}
	caSoFar, ok := share(soFar, hsaSoFar)
	caRest, okRest := share(rest, hsaRest)
	if !okRest && ok && caSoFar == hsaSoFar {
		caRest = hsaRest
	}
	return caSoFar, caRest
}

// w2Boxes adds the W-2s' boxes.
func w2Boxes(ws []*formw2.FormW2) *scenariosv1.W2Boxes {
	b := &scenariosv1.W2Boxes{}
	add := func(dst **commonMoney, m *commonMoney) { *dst = model.Money(model.Value(*dst) + model.Value(m)) }
	states := map[string]*scenariosv1.W2Boxes_State{}
	var order []string
	for _, w := range ws {
		add(&b.Wages, w.GetWagesTipsOtherCompensation())
		add(&b.FederalWithheld, w.GetFederalTaxWithheld())
		add(&b.SocialSecurityWages, w.GetSocialSecurityWages())
		add(&b.SocialSecurityWithheld, w.GetSocialSecurityTaxWithheld())
		add(&b.MedicareWages, w.GetMedicareWagesAndTips())
		add(&b.MedicareWithheld, w.GetMedicareTaxWithheld())
		for _, c := range w.GetCodedAmounts() {
			switch strings.ToUpper(c.GetCode()) {
			case "D":
				add(&b.K401, c.GetAmount())
			case "E":
				add(&b.K403B, c.GetAmount())
			case "AA":
				add(&b.Roth_401K, c.GetAmount())
			case "BB":
				add(&b.Roth_403B, c.GetAmount())
			case "W":
				add(&b.Hsa, c.GetAmount())
			}
		}
		for _, s := range w.GetStateTaxWithholding() {
			st, ok := states[s.GetState()]
			if !ok {
				st = &scenariosv1.W2Boxes_State{State: s.GetState()}
				states[s.GetState()] = st
				order = append(order, s.GetState())
			}
			add(&st.Wages, s.GetStateIncome())
			add(&st.Withheld, s.GetTaxWithheld())
		}
	}
	for _, s := range order {
		b.States = append(b.States, states[s])
	}
	for _, m := range []**commonMoney{&b.Wages, &b.FederalWithheld, &b.SocialSecurityWages, &b.SocialSecurityWithheld,
		&b.MedicareWages, &b.MedicareWithheld, &b.K401, &b.K403B, &b.Roth_401K, &b.Roth_403B, &b.Hsa} {
		// A W-2 prints cents; its boxes are a source, rounded once here.
		*m = model.Money(model.Dollars(model.Value(*m)))
	}
	for _, st := range b.States {
		st.Wages, st.Withheld = model.Money(model.Dollars(model.Value(st.Wages))), model.Money(model.Dollars(model.Value(st.Withheld)))
	}
	return b
}

// combineBoxes is a's boxes plus sign times b's, field by field.
func combineBoxes(a, b *scenariosv1.W2Boxes, sign money.USCents) *scenariosv1.W2Boxes {
	d := func(x, y *commonMoney) *commonMoney { return model.Money(model.Value(x) + sign*model.Value(y)) }
	out := &scenariosv1.W2Boxes{
		Wages: d(a.GetWages(), b.GetWages()), FederalWithheld: d(a.GetFederalWithheld(), b.GetFederalWithheld()),
		SocialSecurityWages:    d(a.GetSocialSecurityWages(), b.GetSocialSecurityWages()),
		SocialSecurityWithheld: d(a.GetSocialSecurityWithheld(), b.GetSocialSecurityWithheld()),
		MedicareWages:          d(a.GetMedicareWages(), b.GetMedicareWages()),
		MedicareWithheld:       d(a.GetMedicareWithheld(), b.GetMedicareWithheld()),
		K401:                   d(a.GetK401(), b.GetK401()), K403B: d(a.GetK403B(), b.GetK403B()),
		Roth_401K: d(a.GetRoth_401K(), b.GetRoth_401K()), Roth_403B: d(a.GetRoth_403B(), b.GetRoth_403B()),
		Hsa: d(a.GetHsa(), b.GetHsa()),
	}
	have := map[string]*scenariosv1.W2Boxes_State{}
	for _, s := range b.GetStates() {
		have[s.GetState()] = s
	}
	seen := map[string]bool{}
	for _, s := range a.GetStates() {
		seen[s.GetState()] = true
		o := have[s.GetState()]
		st := &scenariosv1.W2Boxes_State{State: s.GetState(),
			Wages: d(s.GetWages(), o.GetWages()), Withheld: d(s.GetWithheld(), o.GetWithheld())}
		if s.GetEmployerHsa() != nil || o.GetEmployerHsa() != nil {
			st.EmployerHsa = d(s.GetEmployerHsa(), o.GetEmployerHsa())
		}
		out.States = append(out.States, st)
	}
	for _, s := range b.GetStates() {
		if !seen[s.GetState()] {
			st := &scenariosv1.W2Boxes_State{State: s.GetState(),
				Wages: d(nil, s.GetWages()), Withheld: d(nil, s.GetWithheld())}
			if s.GetEmployerHsa() != nil {
				st.EmployerHsa = d(nil, s.GetEmployerHsa())
			}
			out.States = append(out.States, st)
		}
	}
	return out
}

// canAdd lists the part kinds that can be added now, each filled with its
// defaults. Ids are left to the caller.
func (j *Job) canAdd() []*scenariosv1.EmployerPart {
	if len(j.w2s) > 0 {
		return nil
	}
	var out []*scenariosv1.EmployerPart
	rate := &scenariosv1.EmployerPart_PayRate{}
	if !j.latest.IsZero() {
		rate.From = j.latest.AddDays(1).Proto()
	}
	if j.regular != nil {
		rate.Paycheck = j.regular.this.proto()
	}
	out = append(out, &scenariosv1.EmployerPart{Part: &scenariosv1.EmployerPart_PayRate_{PayRate: rate}})
	out = append(out, &scenariosv1.EmployerPart{Part: &scenariosv1.EmployerPart_OneOff_{OneOff: &scenariosv1.EmployerPart_OneOff{
		Date: j.ctx.YearEnd().Proto(), Kind: factsv1.PaycheckKind_PAYCHECK_KIND_BONUS, Amount: model.Money(0)}}})
	hasEnd, hasTotal := false, false
	for _, p := range j.m.GetParts() {
		hasEnd = hasEnd || p.GetEnds() != nil || p.GetEnded() != nil
		hasTotal = hasTotal || p.GetYearTotal() != nil
	}
	if !hasEnd && len(j.slots) > 0 {
		out = append(out, &scenariosv1.EmployerPart{Part: &scenariosv1.EmployerPart_Ends_{Ends: &scenariosv1.EmployerPart_Ends{
			LastPaycheck: j.slots[len(j.slots)-1].Proto()}}})
	}
	if !hasTotal {
		total := &scenariosv1.EmployerPart_YearTotal{Year: j.sumOf(j.items).proto()}
		if j.regular != nil {
			total.TypedAgainst = j.regular.date.Proto()
		}
		out = append(out, &scenariosv1.EmployerPart{Part: &scenariosv1.EmployerPart_YearTotal_{YearTotal: total}})
	}
	return out
}
