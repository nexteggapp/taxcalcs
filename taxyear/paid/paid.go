// Package paid answers each jurisdiction's estimated-payment rules for a
// scenario, and fills each tax paid model's default part from the answer.
//
// It reads the rules' inputs from the projection: withholding from the
// employer and other income items on their dates, payments from the tax paid
// items, last year's return from the facts. The rules are the taxlisp
// estimated tax rules, asked through the return preparer as
// ReturnOutputs.estimated, with this year's figures given as a caller
// estimate. They are asked as of the end of the next year, so the penalty
// runs as far as the rules let it, and the payments planned are left out or
// put in to tell them apart from the payments made. The rules run three
// times. The first pass, over the payments made, gives the requirement, what
// each due date has been credited, and the penalty without the planned
// payments. The second, over every payment, gives what is still needed;
// SpreadTheRest shares it equally across the open due dates; the third, over
// every payment again, gives the installments and the penalty.
package paid

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/returns/preparer"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/internal/civil"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	"github.com/nexteggapp/taxcalcs/taxyear/model/taxpaid"
	usv1 "github.com/nexteggapp/taxcalcs/us/v1"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
	"google.golang.org/protobuf/proto"
)

// Inputs is what the rules read for a scenario, besides the facts.
type Inputs struct {
	ctx        *model.Context
	status     filing.Status
	federalTax money.USCents
	federalAGI money.USCents
	excessSS   money.USCents
	stateTax   map[string]money.USCents
	proj       *scenariosv1.YearProjection
	payments   map[string]*taxpaid.Payments
	overrides  []*scenariosv1.Override
}

// InputsBuilder builds [Inputs].
type InputsBuilder struct{ in Inputs }

// NewInputs starts the inputs for a scenario's context, filing status and
// projection, its employer and other income items holding the
// withholding.
func NewInputs(ctx *model.Context, status filing.Status, proj *scenariosv1.YearProjection) *InputsBuilder {
	return &InputsBuilder{in: Inputs{ctx: ctx, status: status, proj: proj, payments: map[string]*taxpaid.Payments{},
		stateTax: map[string]money.USCents{}}}
}

// Federal sets the federal tax the rules measure, Form 2210 line 2, the
// adjusted gross income, and Schedule 3 line 11's excess Social Security
// tax, which counts as withholding.
func (b *InputsBuilder) Federal(tax, agi, excessSS money.USCents) *InputsBuilder {
	b.in.federalTax, b.in.federalAGI, b.in.excessSS = tax, agi, excessSS
	return b
}

// State sets a state's tax for the year from the state return the rules
// prepared, which the rules measure when there is no filed return.
func (b *InputsBuilder) State(state string, tax money.USCents) *InputsBuilder {
	b.in.stateTax[state] = tax
	return b
}

// Payments adds a jurisdiction's expanded tax paid model.
func (b *InputsBuilder) Payments(p *taxpaid.Payments) *InputsBuilder {
	b.in.payments[p.Jurisdiction()] = p
	return b
}

// Overrides sets the scenario's overrides, of which the estimated[j] ones
// are read.
func (b *InputsBuilder) Overrides(o []*scenariosv1.Override) *InputsBuilder {
	b.in.overrides = o
	return b
}

// Build refuses inputs with no context.
func (b *InputsBuilder) Build() (Inputs, error) {
	if b.in.ctx == nil {
		return Inputs{}, errors.New("paid: no context")
	}
	return b.in, nil
}

// Answer is every jurisdiction's estimated figures, and what was asked.
type Answer struct {
	estimated []*scenariosv1.Estimated
	notes     model.Notes
	paidUS    money.USCents
}

// Estimated returns one Estimated per jurisdiction, US first.
func (a *Answer) Estimated() []*scenariosv1.Estimated { return a.estimated }

// Notes returns the questions the rules returned.
func (a *Answer) Notes() *model.Notes { return &a.notes }

// FederalPayments is the year's federal estimated payments made, every one that
// counts, last year's overpayment and planned payments included.
func (a *Answer) FederalPayments() money.USCents { return a.paidUS }

// Compute answers every jurisdiction: US, each with a tax paid model, and
// each state lived or worked in. A tax paid model's SpreadTheRest default
// is filled as it goes, so its projection is final after this.
func Compute(in Inputs) (*Answer, error) {
	out := &Answer{}
	for _, j := range jurisdictions(in) {
		e, err := one(in, j, &out.notes)
		if err != nil {
			return nil, fmt.Errorf("estimated[%s]: %w", j, err)
		}
		out.estimated = append(out.estimated, e)
	}
	if p := in.payments["US"]; p != nil {
		// The return counts the payments made. A planned payment, and the
		// rest of the target SpreadTheRest shares out, are suggestions for
		// the due dates: they reach only the penalty if they are made.
		for _, x := range p.Payments() {
			if x.Made() {
				out.paidUS += x.Amount()
			}
		}
	}
	return out, nil
}

// jurisdictions is US, then the states in postal-code order.
func jurisdictions(in Inputs) []string {
	seen := map[string]bool{"US": true}
	for j := range in.payments {
		seen[j] = true
	}
	for _, r := range in.ctx.Facts().GetResidence() {
		seen[r.GetState()] = true
	}
	for _, e := range in.proj.GetEmployers() {
		for _, s := range e.GetYear().GetBoxes().GetStates() {
			seen[s.GetState()] = true
		}
	}
	var states []string
	for j := range seen {
		if j != "US" && j != "" {
			states = append(states, j)
		}
	}
	sort.Strings(states)
	return append([]string{"US"}, states...)
}

// override returns an estimated[j].<field> override's amount.
func (in Inputs) override(j, field string) (money.USCents, bool) {
	path := fmt.Sprintf("estimated[%s].%s", j, field)
	for _, o := range in.overrides {
		if o.GetPath() == path {
			return model.Value(o.GetAmount()), true
		}
	}
	return 0, false
}

func one(in Inputs, j string, notes *model.Notes) (*scenariosv1.Estimated, error) {
	e := &scenariosv1.Estimated{Jurisdiction: j}
	// The rules take this year's tax in whole dollars, as the return states
	// it and as they take the withholding.
	tax, taxKnown := model.Dollars(in.federalTax), j == "US"
	if j != "US" {
		tax, taxKnown = filedTax(in, j)
		if !taxKnown {
			tax, taxKnown = in.stateTax[j]
		}
	}
	if v, ok := in.override(j, "tax_for_year"); ok {
		tax, taxKnown = v, true
	}
	if taxKnown {
		e.TaxForYear = model.Money(tax)
	}
	prior := priorReturn(in, j)
	if v, ok := in.override(j, "last_year_tax"); ok {
		prior.tax, prior.taxSet = v, true
	}
	if v, ok := in.override(j, "last_year_agi"); ok {
		prior.agi, prior.agiSet = v, true
	}
	if v, ok := in.override(j, "last_year_withheld"); ok {
		prior.withheld, prior.withheldSet = v, true
	}
	e.LastYearTax = model.MoneyIfSet(prior.tax, prior.taxSet)
	e.LastYearAgi = model.MoneyIfSet(prior.agi, prior.agiSet)
	if _, overridden := in.override(j, "last_year_agi"); j != "US" && prior.stateAGISet && !overridden {
		e.LastYearAgi = model.Money(prior.stateAGI)
	}
	e.LastYearWithheld = model.MoneyIfSet(prior.withheld, prior.withheldSet)
	r := &runner{in: in, j: j, tax: tax, taxKnown: taxKnown, prior: prior, notes: notes}
	e.Rules = r.answer()
	return e, nil
}

// filedTax is a state's tax for a finished year from the year's own filed
// return, the line the prior-year rules read as last year's tax: Form
// 540's total tax, or Form OR-40's tax after all credits. While the year is
// in progress, or without the return, the tax is the one the rules
// prepared, when they prepared one.
func filedTax(in Inputs, j string) (money.USCents, bool) {
	if !in.ctx.Finished() {
		return 0, false
	}
	year := int32(in.ctx.Year())
	for _, d := range in.ctx.Facts().GetUnassigned() {
		if !model.Usable(d.GetSource()) {
			continue
		}
		if f := d.GetCaliforniaReturn().GetForm_540(); j == "CA" && f != nil && f.GetTaxYear() == year {
			return model.Cents(f.GetTotalTax())
		}
		if f := d.GetOregonReturn().GetFormOr40(); j == "OR" && f != nil && f.GetTaxYear() == year {
			return model.Cents(f.GetTaxAfterAllCredits())
		}
	}
	return 0, false
}

// prior is last year's figures for one jurisdiction.
type prior struct {
	status                                               factsv1.PriorYearReturn_Status
	tax, agi, withheld, stateAGI, taxable                money.USCents
	taxSet, agiSet, withheldSet, stateAGISet, taxableSet bool
	fullYear                                             *bool
}

func priorReturn(in Inputs, j string) prior {
	out := stateOrFederalPrior(in, j)
	// A state return need not print the federal AGI it starts from, so a
	// state's last-year AGI falls back to last year's federal return.
	if j != "US" && !out.agiSet {
		if us := stateOrFederalPrior(in, "US"); us.agiSet {
			out.agi, out.agiSet = us.agi, true
		}
	}
	return out
}

func stateOrFederalPrior(in Inputs, j string) prior {
	for _, p := range in.ctx.Facts().GetPriorYear() {
		if p.GetJurisdiction() != j || len(p.GetPeople()) > 0 {
			continue
		}
		out := prior{status: p.GetStatus()}
		if p.FullYearResident != nil {
			v := p.GetFullYearResident()
			out.fullYear = &v
		}
		switch {
		case p.GetFederal() != nil:
			f := p.GetFederal().GetForm_1040()
			out.tax, out.taxSet = model.Value(f.GetTotalTax()), true
			out.agi, out.agiSet = model.Value(f.GetAdjustedGrossIncome()), true
			out.withheld, out.withheldSet = model.Value(f.GetTotalWithheld()), true
		case p.GetCalifornia() != nil:
			f := p.GetCalifornia().GetForm_540()
			out.tax, out.taxSet = model.Cents(f.GetTotalTax())
			out.agi, out.agiSet = model.Cents(f.GetFederalAdjustedGrossIncome())
			out.stateAGI, out.stateAGISet = model.Cents(f.GetCaliforniaAdjustedGrossIncome())
			out.taxable, out.taxableSet = model.Cents(f.GetTaxableIncome())
			out.withheld, out.withheldSet = model.Cents(f.GetCaliforniaTaxWithheld())
		case p.GetOregon() != nil:
			f := p.GetOregon().GetFormOr40()
			out.tax, out.taxSet = model.Cents(f.GetTaxAfterAllCredits())
			out.agi, out.agiSet = model.Cents(f.GetFederalAdjustedGrossIncome())
			out.taxable, out.taxableSet = model.Cents(f.GetTaxableIncome())
			out.withheld, out.withheldSet = model.Cents(f.GetOregonTaxWithheld())
		case p.GetTotalTax() != nil:
			out.tax, out.taxSet = model.Value(p.GetTotalTax()), true
			out.agi, out.agiSet = model.Cents(p.GetAdjustedGrossIncome())
		}
		return out
	}
	return prior{}
}

// runner asks one jurisdiction's rules.
type runner struct {
	in       Inputs
	j        string
	tax      money.USCents
	taxKnown bool
	prior    prior
	notes    *model.Notes
	// withheld is the withholding the facts pass, worked out once: each
	// request the rules are asked passes the same.
	withheld []dated
	settled  bool
	// asked is each answer by its request, so a request the same as an
	// earlier one, such as every payment when none is planned, isn't asked
	// again.
	asked map[string]*returnsv1.EstimatedAnswer
}

func (r *runner) answer() *usv1.EstimatedAnswer {
	out, err := r.compute()
	if err != nil {
		return &usv1.EstimatedAnswer{Outcome: usv1.EstimatedOutcome_ESTIMATED_OUTCOME_REFUSED, Reason: err.Error(), ThisYearTaxKnown: r.taxKnown}
	}
	return out
}

// compute asks the rules for the requirement over the payments made, fills
// the default part from the schedule over every payment, then asks for the
// installments and the penalty with the planned payments. The installments'
// credits and the penalty without the planned payments are the first
// pass's.
func (r *runner) compute() (*usv1.EstimatedAnswer, error) {
	made, err := r.ask(true)
	if err != nil {
		return nil, err
	}
	switch made.GetOutcome() {
	case returnsv1.EstimatedOutcome_ESTIMATED_OUTCOME_COMPUTED:
	case returnsv1.EstimatedOutcome_ESTIMATED_OUTCOME_NO_RULES:
		return &usv1.EstimatedAnswer{Outcome: usv1.EstimatedOutcome_ESTIMATED_OUTCOME_NO_RULES,
			Reason: fmt.Sprintf("No estimated-payment rules are built for %s for %d.", r.j, r.in.ctx.Year()), ThisYearTaxKnown: r.taxKnown}, nil
	case returnsv1.EstimatedOutcome_ESTIMATED_OUTCOME_UNSUPPORTED:
		return &usv1.EstimatedAnswer{Outcome: usv1.EstimatedOutcome_ESTIMATED_OUTCOME_UNSUPPORTED, Reason: made.GetReason(), ThisYearTaxKnown: r.taxKnown}, nil
	default:
		return &usv1.EstimatedAnswer{Outcome: usv1.EstimatedOutcome_ESTIMATED_OUTCOME_REFUSED, Reason: made.GetReason(), ThisYearTaxKnown: r.taxKnown}, nil
	}
	out := &usv1.EstimatedAnswer{Outcome: usv1.EstimatedOutcome_ESTIMATED_OUTCOME_COMPUTED, ThisYearTaxKnown: r.taxKnown}
	n := newNotes(r.j, r.notes)
	defer func() { out.Questions, out.Divergences = n.questions, n.divergences }()
	out.Requirement = requirementOf(made.GetRequirement())
	out.Explanation = explanationOf(made.GetExplanation())
	if !out.Requirement.GetTargetKnown() {
		n.add(made)
		return out, nil
	}
	// The questions are those over every payment, once the default part is
	// filled: the penalty without the planned payments, and the one before
	// the default part was filled, are only steps, and ask nothing.
	pass1, err := r.ask(false)
	if err != nil {
		return nil, err
	}
	all := pass1
	if r.spread(pass1.GetInstallments()) {
		if all, err = r.ask(false); err != nil {
			return nil, err
		}
	}
	n.add(all)
	if all.GetOutcome() != returnsv1.EstimatedOutcome_ESTIMATED_OUTCOME_COMPUTED {
		return nil, fmt.Errorf("paid: the rules answered %v over every payment: %s", all.GetOutcome(), all.GetReason())
	}
	today := r.in.ctx.AsOf()
	before, sched := made.GetInstallments(), all.GetInstallments()
	out.ToMeet = toMeet(before, today)
	if len(before) != len(sched) {
		return nil, fmt.Errorf("paid: %d installments over the payments made, and %d over all of them", len(before), len(sched))
	}
	planned := map[string]bool{}
	if p := r.in.payments[r.j]; p != nil {
		for _, x := range p.Payments() {
			if !x.Made() && x.Amount() > 0 && !x.On().IsZero() {
				planned[x.On().String()] = true
			}
		}
	}
	for k, i := range sched {
		out.Installments = append(out.Installments, installmentOf(i, before[k], today, planned[date(i.GetDue()).String()]))
	}
	out.Penalty = penaltyOf(all.GetPenalty(), sched, r.taxKnown, out.Requirement.GetExempt())
	out.PenaltyWithoutPlanned = penaltyOf(made.GetPenalty(), before, r.taxKnown, out.Requirement.GetExempt())
	return out, nil
}

var (
	serverOnce sync.Once
	srv        *preparer.Server
	srvErr     error
)

// server is the return preparer over the shared rules engine, which is
// loaded and typechecked once.
func server() (*preparer.Server, error) {
	serverOnce.Do(func() { srv, srvErr = preparer.NewServer() })
	return srv, srvErr
}

// ask runs the rules over the payments made, or over every one that
// counts, and returns the jurisdiction's answer.
func (r *runner) ask(madeOnly bool) (*returnsv1.EstimatedAnswer, error) {
	in, err := r.request(madeOnly)
	if err != nil {
		return nil, err
	}
	key, err := proto.MarshalOptions{Deterministic: true}.Marshal(in)
	if err != nil {
		return nil, err
	}
	if a, ok := r.asked[string(key)]; ok {
		return a, nil
	}
	s, err := server()
	if err != nil {
		return nil, err
	}
	resp, err := s.Prepare(context.Background(), &returnsv1.PrepareRequest{Inputs: in})
	if err != nil {
		return nil, fmt.Errorf("rules engine: %w", err)
	}
	for _, a := range resp.GetOutputs().GetEstimated() {
		if a.GetJurisdiction() == r.j {
			if r.asked == nil {
				r.asked = map[string]*returnsv1.EstimatedAnswer{}
			}
			r.asked[string(key)] = a
			return a, nil
		}
	}
	return nil, fmt.Errorf("paid: the rules gave no estimated answer for %s", r.j)
}

// spread fills the tax paid model's open due dates with equal payments
// reaching the target: what the last installment still needs over every
// payment made and planned. It reports whether it added any.
func (r *runner) spread(installments []*returnsv1.EstimatedInstallment) bool {
	p := r.in.payments[r.j]
	if p == nil || len(installments) == 0 {
		return false
	}
	var dues []civil.Date
	for _, i := range installments {
		if d := date(i.GetDue()); !d.IsZero() {
			dues = append(dues, d)
		}
	}
	open := p.Open(dues)
	if len(open) == 0 {
		return false
	}
	last := installments[len(installments)-1]
	rest := model.Value(last.GetNeededBy()) - model.Value(last.GetCreditedBy())
	p.Spread(open, rest)
	return rest > 0
}

// stateAGI is this year's AGI as the state measures it: federal AGI less
// Treasury interest, which no state taxes, and in California plus every
// HSA contribution, which California doesn't exclude (Schedule CA line 1h
// adds W-2 box 12 code W, and takes back the federal HSA deduction). For
// someone who didn't live in California all year it is Form 540NR line
// 32, the California-source AGI: California wages, plus the rest of that
// AGI for the days lived there. A state lived in for none of the year has
// none.
func (r *runner) stateAGI() (money.USCents, bool) {
	if r.j == "US" {
		return 0, false
	}
	days, all := r.daysLivedIn(r.j)
	if days == 0 {
		return 0, false
	}
	agi := r.in.federalAGI
	for _, a := range r.in.proj.GetAccounts() {
		agi -= model.Value(a.GetYear().GetBoxes().GetTreasuryInterest())
	}
	if r.j != "CA" {
		return agi, true
	}
	var wagesAll, wagesHere money.USCents
	for _, e := range r.in.proj.GetEmployers() {
		b := e.GetYear().GetBoxes()
		agi += model.Value(b.GetHsa())
		wagesAll += model.Value(b.GetWages()) + model.Value(b.GetHsa())
		for _, st := range b.GetStates() {
			if st.GetState() == "CA" {
				wagesHere += model.Value(st.GetWages())
			}
		}
	}
	for _, x := range r.in.proj.GetRetirement() {
		agi += model.Value(x.GetYear().GetItems().GetHsa())
	}
	if days == all {
		return agi, true
	}
	return wagesHere + model.Scale(agi-wagesAll, money.USCents(days), money.USCents(all)), true
}

// daysLivedIn is the days of the year the residence periods put in a
// state, an open end read as December 31, and the year's days.
func (r *runner) daysLivedIn(state string) (int, int) {
	start, end := r.in.ctx.YearStart(), r.in.ctx.YearEnd()
	all := end.DaysFrom(start) + 1
	days := 0
	for _, p := range r.in.ctx.Facts().GetResidence() {
		if p.GetState() != state {
			continue
		}
		from, err1 := civil.FromProto(p.GetFrom())
		to, err2 := civil.FromProto(p.GetTo())
		if err1 != nil || err2 != nil {
			continue
		}
		if from.IsZero() || from.Before(start) {
			from = start
		}
		if to.IsZero() || to.After(end) {
			to = end
		}
		if !to.Before(from) {
			days += to.DaysFrom(from) + 1
		}
	}
	return min(days, all), all
}

// withholding is the jurisdiction's withholding on each item's date:
// federal or that state's, from the employer and other income items. For
// US, excess Social Security tax is withholding dated December 31, as Form
// 2210 line 6 counts it.
func (r *runner) withholding() ([]dated, error) {
	if !r.settled {
		w, err := r.workOutWithholding()
		if err != nil {
			return nil, err
		}
		r.withheld, r.settled = w, true
	}
	return r.withheld, nil
}

func (r *runner) workOutWithholding() ([]dated, error) {
	byDate := map[string]money.USCents{}
	dates := map[string]civil.Date{}
	add := func(on *civil.Date, v money.USCents) error {
		byDate[on.String()] += v
		dates[on.String()] = *on
		return nil
	}
	if err := r.collectWithholding(add); err != nil {
		return nil, err
	}
	settled := settle(byDate, dates)
	r.toBoxes(settled)
	start, end := r.in.ctx.YearStart(), r.in.ctx.YearEnd()
	for _, w := range settled {
		if w.on.IsZero() {
			return nil, errors.New("paid: a date is missing")
		}
		if w.on.Before(start) || w.on.After(end) {
			return nil, fmt.Errorf("paid: withholding on %v is outside tax year %d", w.on, r.in.ctx.Year())
		}
	}
	return settled, nil
}

// toBoxes makes the withholding add up to the year's figures in whole
// dollars, as the return shows them: the rules take rounded
// figures, so the difference from the exact sum goes on the last date. The
// employers' year boxes are already so far's and the rest's rounded and
// added; other income's withholding is rounded as a whole.
func (r *runner) toBoxes(ws []dated) {
	if len(ws) == 0 {
		return
	}
	var exact, want money.USCents
	for _, w := range ws {
		exact += w.v
	}
	for _, e := range r.in.proj.GetEmployers() {
		b := e.GetYear().GetBoxes()
		if r.j == "US" {
			want += model.Value(b.GetFederalWithheld())
			continue
		}
		for _, s := range b.GetStates() {
			if s.GetState() == r.j {
				want += model.Value(s.GetWithheld())
			}
		}
	}
	var other money.USCents
	for _, o := range r.in.proj.GetOtherIncome() {
		for _, it := range o.GetItems() {
			if r.j == "US" {
				other += model.Value(it.GetOtherIncome().GetFederalWithheld())
			}
		}
	}
	want += model.Dollars(other)
	if r.j == "US" {
		want += r.in.excessSS
	}
	if last := &ws[len(ws)-1]; last.v+want-exact >= 0 {
		last.v += want - exact
	}
}

// dated is an amount on a date.
type dated struct {
	on civil.Date
	v  money.USCents
}

// settle returns the withholding by date with each negative amount, such as
// a year-to-date adjustment correcting earlier paychecks, taken off the
// dates before it, latest first, so no date's withholding is negative.
func settle(byDate map[string]money.USCents, dates map[string]civil.Date) []dated {
	var out []dated
	for k, v := range byDate {
		out = append(out, dated{dates[k], v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].on.Before(out[j].on) })
	for i := range out {
		for k := i - 1; out[i].v < 0 && k >= 0; k-- {
			take := min(out[k].v, -out[i].v)
			out[k].v -= take
			out[i].v += take
		}
		out[i].v = max(out[i].v, 0)
	}
	kept := out[:0]
	for _, d := range out {
		if d.v != 0 {
			kept = append(kept, d)
		}
	}
	return kept
}

// collectWithholding passes each item's withholding for the jurisdiction
// to add.
func (r *runner) collectWithholding(add func(*civil.Date, money.USCents) error) error {
	for _, e := range r.in.proj.GetEmployers() {
		for _, it := range e.GetItems() {
			d, err := civil.FromProto(it.GetDate())
			if err != nil {
				return err
			}
			p := it.GetPaycheck()
			v := model.Value(p.GetFederalWithheld())
			if r.j != "US" {
				v = 0
				for _, s := range p.GetStates() {
					if s.GetState() == r.j {
						v += model.Value(s.GetTaxWithheld())
					}
				}
			}
			if err := add(&d, v); err != nil {
				return err
			}
		}
	}
	for _, o := range r.in.proj.GetOtherIncome() {
		for _, it := range o.GetItems() {
			d, err := civil.FromProto(it.GetDate())
			if err != nil {
				return err
			}
			v := model.Value(it.GetOtherIncome().GetFederalWithheld())
			if r.j != "US" {
				v = 0
			}
			if err := add(&d, v); err != nil {
				return err
			}
		}
	}
	if r.j == "US" {
		end := r.in.ctx.YearEnd()
		return add(&end, r.in.excessSS)
	}
	return nil
}

// notes collects one jurisdiction's questions and divergences, each once,
// and adds the questions to the scenario's.
type notes struct {
	j           string
	scenario    *model.Notes
	questions   []*usv1.EstimatedQuestion
	divergences []*usv1.EstimatedDivergence
	seen        map[string]bool
}

func newNotes(j string, scenario *model.Notes) *notes {
	return &notes{j: j, scenario: scenario, seen: map[string]bool{}}
}

// add adds an answer's questions and divergences that aren't there yet.
func (n *notes) add(a *returnsv1.EstimatedAnswer) {
	for _, q := range a.GetQuestions() {
		key := fmt.Sprintf("q%s:%s", q.GetPosition(), q.GetBecause())
		if n.seen[key] {
			continue
		}
		n.seen[key] = true
		n.questions = append(n.questions, &usv1.EstimatedQuestion{Position: q.GetPosition(), Because: q.GetBecause(), Question: q.GetQuestion(), Effect: q.GetEffect()})
		text, effect := q.GetQuestion(), q.GetEffect()
		if text == "" {
			text = q.GetBecause()
		}
		if effect == "" {
			effect = q.GetBecause()
		}
		n.scenario.Ask(model.NewQuestion(fmt.Sprintf("estimated/%s/%s", n.j, strings.ReplaceAll(q.GetPosition(), " ", "-")),
			fmt.Sprintf("estimated[%s]", n.j), text, effect, "facts"))
	}
	for _, d := range a.GetDivergences() {
		key := "d:" + d.GetRule()
		if n.seen[key] {
			continue
		}
		n.seen[key] = true
		uses := usv1.DivergenceReading_DIVERGENCE_READING_UNSPECIFIED
		switch d.GetUses() {
		case returnsv1.DivergenceReading_DIVERGENCE_READING_FORM:
			uses = usv1.DivergenceReading_DIVERGENCE_READING_FORM
		case returnsv1.DivergenceReading_DIVERGENCE_READING_STATUTE:
			uses = usv1.DivergenceReading_DIVERGENCE_READING_STATUTE
		}
		n.divergences = append(n.divergences, &usv1.EstimatedDivergence{Rule: d.GetRule(), Form: d.GetForm(), FormSource: d.GetFormSource(),
			Statute: d.GetStatute(), StatuteSource: d.GetStatuteSource(), Uses: uses, AffectsHousehold: d.GetAffectsHousehold()})
	}
}
