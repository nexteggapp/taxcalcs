// Package paid answers each jurisdiction's estimated-payment rules for a
// scenario, and fills each tax paid model's default part from the answer.
//
// It reads the rules' inputs from the projection: withholding from the
// employer and other income items on their dates, payments from the tax paid
// items, last year's return from the facts. The rules run twice. The first
// pass, over the payments made by as_of and the planned ones, gives what is
// still needed; SpreadTheRest shares it equally across the open due dates;
// the second pass, over every payment, gives the installments and the
// penalty.
package paid

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/estimated/all"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/internal/civil"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	"github.com/nexteggapp/taxcalcs/taxyear/model/taxpaid"
	usv1 "github.com/nexteggapp/taxcalcs/us/v1"
	"github.com/nexteggapp/taxcalcs/working"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
)

// Inputs is what the rules read for a scenario, besides the facts.
type Inputs struct {
	ctx        *model.Context
	status     filing.Status
	federalTax money.USCents
	federalAGI money.USCents
	excessSS   money.USCents
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
	return &InputsBuilder{in: Inputs{ctx: ctx, status: status, proj: proj, payments: map[string]*taxpaid.Payments{}}}
}

// Federal sets the federal tax the rules measure, Form 2210 line 2, the
// adjusted gross income, and Schedule 3 line 11's excess Social Security
// tax, which counts as withholding.
func (b *InputsBuilder) Federal(tax, agi, excessSS money.USCents) *InputsBuilder {
	b.in.federalTax, b.in.federalAGI, b.in.excessSS = tax, agi, excessSS
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
	rules, err := all.For(estimated.Jurisdiction(j), in.ctx.Year())
	switch {
	case errors.Is(err, estimated.ErrNoRules):
		e.Rules = &usv1.EstimatedAnswer{Outcome: usv1.EstimatedOutcome_ESTIMATED_OUTCOME_NO_RULES,
			Reason: fmt.Sprintf("No estimated-payment rules are built for %s for %d.", j, in.ctx.Year()), ThisYearTaxKnown: taxKnown}
		return e, nil
	case err != nil:
		return nil, err
	}
	r := &runner{in: in, j: j, rules: rules, tax: tax, taxKnown: taxKnown, prior: prior, notes: notes}
	e.Rules = r.answer()
	return e, nil
}

// filedTax is a state's tax for a finished year from the year's own filed
// return, the line the prior-year rules read as last year's tax: Form
// 540's total tax, or Form OR-40's tax after all credits. While the year is in progress, or
// without the return, the state's tax isn't worked out.
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
	rules    estimated.EstimatedTax
	tax      money.USCents
	taxKnown bool
	prior    prior
	notes    *model.Notes
	// withheld is the withholding the facts pass, worked out once: each
	// set of facts the rules are asked about passes the same.
	withheld []estimated.Withheld
	settled  bool
}

func (r *runner) answer() *usv1.EstimatedAnswer {
	out, err := r.compute()
	switch {
	case err == nil:
		return out
	case errors.Is(err, estimated.ErrUnsupported):
		return &usv1.EstimatedAnswer{Outcome: usv1.EstimatedOutcome_ESTIMATED_OUTCOME_UNSUPPORTED, Reason: err.Error(), ThisYearTaxKnown: r.taxKnown}
	default:
		return &usv1.EstimatedAnswer{Outcome: usv1.EstimatedOutcome_ESTIMATED_OUTCOME_REFUSED, Reason: err.Error(), ThisYearTaxKnown: r.taxKnown}
	}
}

// compute asks the rules for the requirement over the payments made, fills
// the default part from the schedule over every payment, then asks for the
// installments and the penalty with and without the planned payments.
func (r *runner) compute() (*usv1.EstimatedAnswer, error) {
	out := &usv1.EstimatedAnswer{Outcome: usv1.EstimatedOutcome_ESTIMATED_OUTCOME_COMPUTED, ThisYearTaxKnown: r.taxKnown}
	n := newNotes(r.j, r.notes)
	defer func() { out.Questions, out.Divergences = n.questions, n.divergences }()
	madeFacts, err := r.facts(true)
	if err != nil {
		return nil, err
	}
	req, err := r.rules.Requirement(madeFacts)
	if err != nil {
		return nil, err
	}
	n.add(req.Required(), req.Divergences())
	out.Requirement = requirementOf(req.Value())
	out.Explanation = explanationOf(r.rules.Explanation())
	if !req.Value().TargetKnown() {
		return out, nil
	}
	planned, err := r.facts(false)
	if err != nil {
		return nil, err
	}
	pass1, err := r.rules.Schedule(planned)
	if err != nil {
		return nil, err
	}
	n.add(pass1.Required(), pass1.Divergences())
	r.spread(pass1.Value())
	return out, r.schedule(out, madeFacts, n)
}

// schedule fills the installments, what would meet each, and the
// penalties, over every payment once the default part is filled.
func (r *runner) schedule(out *usv1.EstimatedAnswer, madeFacts estimated.Facts, n *notes) error {
	today, err := edate(r.in.ctx.AsOf())
	if err != nil {
		return err
	}
	before, err := r.rules.Schedule(madeFacts)
	if err != nil {
		return err
	}
	out.ToMeet = toMeet(before.Value(), today)
	allFacts, err := r.facts(false)
	if err != nil {
		return err
	}
	sched, err := r.rules.Schedule(allFacts)
	if err != nil {
		return err
	}
	n.add(sched.Required(), sched.Divergences())
	if len(before.Value()) != len(sched.Value()) {
		return fmt.Errorf("paid: %d installments over the payments made, and %d over all of them", len(before.Value()), len(sched.Value()))
	}
	planned := map[estimated.Date]bool{}
	if p := r.in.payments[r.j]; p != nil {
		for _, x := range p.Payments() {
			if d, err := edate(x.On()); err == nil && !x.Made() && x.Amount() > 0 {
				planned[d] = true
			}
		}
	}
	for k, i := range sched.Value() {
		out.Installments = append(out.Installments, installmentOf(i, before.Value()[k], today, planned[i.Due()]))
	}
	asOf := estimated.MustDate(r.in.ctx.Year()+1, time.December, 31)
	if out.Penalty, err = r.penalty(allFacts, asOf, n); err != nil {
		return err
	}
	out.PenaltyWithoutPlanned, err = r.penalty(madeFacts, asOf, nil)
	return err
}

// penalty asks for the penalty as of a day, or nil while it needs this
// year's tax. n, when given, collects its questions.
func (r *runner) penalty(f estimated.Facts, asOf estimated.Date, n *notes) (*usv1.EstimatedPenalty, error) {
	pen, err := r.rules.Penalty(f, asOf)
	if errors.Is(err, estimated.ErrNeedsThisYearTax) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if n != nil {
		n.add(pen.Required(), pen.Divergences())
	}
	return penaltyOf(pen.Value()), nil
}

// spread fills the tax paid model's open due dates with equal payments
// reaching the target: what the last installment still needs over every
// payment made and planned.
func (r *runner) spread(installments []estimated.Installment) {
	p := r.in.payments[r.j]
	if p == nil || len(installments) == 0 {
		return
	}
	var dues []civil.Date
	for _, i := range installments {
		d, err := civil.New(i.Due().Year(), i.Due().Month(), i.Due().Day())
		if err == nil {
			dues = append(dues, d)
		}
	}
	open := p.Open(dues)
	if len(open) == 0 {
		return
	}
	last := installments[len(installments)-1]
	p.Spread(open, last.NeededBy()-last.CreditedBy())
}

// facts builds the rules' facts: this year's figures, last year's return,
// residence, withholding, the payments — made by as_of only, or every one
// that counts — and the elections.
func (r *runner) facts(madeOnly bool) (estimated.Facts, error) {
	b := estimated.NewFacts(estimated.Jurisdiction(r.j), r.in.ctx.Year()).FilingStatus(r.in.status)
	for _, step := range []func(*estimated.FactsBuilder) error{
		r.thisYear, r.priorYear, r.residence, r.withholding,
		func(b *estimated.FactsBuilder) error { return r.payments(b, madeOnly) },
	} {
		if err := step(b); err != nil {
			return estimated.Facts{}, err
		}
	}
	return r.elections(b)
}

func (r *runner) thisYear(b *estimated.FactsBuilder) error {
	fig := estimated.NewFigures()
	if r.taxKnown {
		fig.Tax(r.tax)
	}
	fig.FederalAGI(r.in.federalAGI)
	if v, ok := r.stateAGI(); ok {
		fig.StateAGI(v)
	}
	this, err := fig.Build()
	if err != nil {
		return err
	}
	b.ThisYear(this)
	return nil
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

func (r *runner) priorYear(b *estimated.FactsBuilder) error {
	pr, ok, err := r.priorFacts()
	if ok {
		b.PriorYear(pr)
	}
	return err
}

// residence passes the facts' residence periods, an open end read as
// December 31.
func (r *runner) residence(b *estimated.FactsBuilder) error {
	for _, res := range r.in.ctx.Facts().GetResidence() {
		from, err1 := civil.FromProto(res.GetFrom())
		to, err2 := civil.FromProto(res.GetTo())
		if err1 != nil || err2 != nil {
			continue
		}
		if from.IsZero() {
			from = r.in.ctx.YearStart()
		}
		if to.IsZero() {
			to = r.in.ctx.YearEnd()
		}
		f, _ := edate(from)
		t, _ := edate(to)
		period, err := estimated.NewResidencePeriod(res.GetState()).From(f).To(t).Build()
		if err != nil {
			return err
		}
		b.Residence(period)
	}
	return nil
}

// payments passes the jurisdiction's payments that count, and when
// payments could first be expected.
func (r *runner) payments(b *estimated.FactsBuilder, madeOnly bool) error {
	p := r.in.payments[r.j]
	if p == nil {
		return nil
	}
	for _, x := range p.Payments() {
		if madeOnly && !x.Made() {
			continue
		}
		pb := estimated.NewPayment().Amount(x.Amount())
		if x.FromLastYear() {
			pb.FromPriorYear()
		} else {
			d, err := edate(x.On())
			if err != nil {
				return err
			}
			pb.On(d)
		}
		pay, err := pb.Build()
		if err != nil {
			return err
		}
		b.Paid(pay)
	}
	if from := p.PaymentsFrom(); !from.IsZero() {
		d, err := edate(from)
		if err != nil {
			return err
		}
		b.RequirementFirstMet(d)
	}
	return nil
}

// elections passes the withholding-dates election and a waiver request,
// and builds the facts.
func (r *runner) elections(b *estimated.FactsBuilder) (estimated.Facts, error) {
	el := estimated.NewElections()
	for _, x := range r.in.ctx.Facts().GetElections() {
		if w := x.GetWithholdingDates(); w != nil && w.GetJurisdiction() == r.j && w.GetAsWithheld() {
			el.WithholdingAsWithheld()
		}
		if w := x.GetWaiver(); w != nil && w.GetJurisdiction() == r.j {
			pos, err := positionsOf(w)
			if err != nil {
				return estimated.Facts{}, err
			}
			b.Positions(pos)
		}
	}
	e, err := el.Build()
	if err != nil {
		return estimated.Facts{}, err
	}
	return b.Elections(e).Build()
}

func (r *runner) priorFacts() (estimated.PriorReturn, bool, error) {
	p := r.prior
	if p.status == factsv1.PriorYearReturn_STATUS_UNSPECIFIED && !p.taxSet {
		return estimated.PriorReturn{}, false, nil
	}
	b := estimated.NewPriorReturn()
	if p.fullYear != nil {
		b.FullYearResident(*p.fullYear)
	}
	if p.status == factsv1.PriorYearReturn_STATUS_NONE {
		pr, err := b.Filed(false).Build()
		return pr, true, err
	}
	b.Filed(true)
	if p.taxSet {
		f := estimated.NewFigures().Tax(p.tax)
		if p.agiSet {
			f.FederalAGI(p.agi)
		}
		if p.withheldSet {
			f.Withheld(p.withheld)
		}
		if p.stateAGISet {
			f.StateAGI(p.stateAGI)
		}
		if p.taxableSet {
			f.TaxableIncome(p.taxable)
		}
		fig, err := f.Build()
		if err != nil {
			return estimated.PriorReturn{}, false, err
		}
		b.Figures(fig)
	}
	pr, err := b.Build()
	return pr, true, err
}

// withholding adds the jurisdiction's withholding on each item's date:
// federal or that state's, from the employer and other income items. For
// US, excess Social Security tax is withholding dated December 31, as Form
// 2210 line 6 counts it.
func (r *runner) withholding(b *estimated.FactsBuilder) error {
	if !r.settled {
		w, err := r.workOutWithholding()
		if err != nil {
			return err
		}
		r.withheld, r.settled = w, true
	}
	b.Withheld(r.withheld...)
	return nil
}

func (r *runner) workOutWithholding() ([]estimated.Withheld, error) {
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
	var out []estimated.Withheld
	for _, w := range settled {
		d, err := edate(w.on)
		if err != nil {
			return nil, err
		}
		built, err := estimated.NewWithheld().On(d).Amount(w.v).Build()
		if err != nil {
			return nil, err
		}
		out = append(out, built)
	}
	return out, nil
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

func positionsOf(w *factsv1.WaiverRequest) (estimated.Positions, error) {
	b := estimated.NewPositions()
	var on estimated.Date
	if d, err := civil.FromProto(w.GetOn()); err == nil && !d.IsZero() {
		on, _ = edate(d)
	}
	switch w.GetReason() {
	case factsv1.WaiverRequest_REASON_UNSPECIFIED:
		return estimated.Positions{}, nil
	case factsv1.WaiverRequest_REASON_NONE:
		b.Waiver(estimated.WaiverNone)
	case factsv1.WaiverRequest_REASON_CASUALTY:
		b.WaiverFor(estimated.CauseCasualty, on)
	case factsv1.WaiverRequest_REASON_DISASTER:
		b.WaiverFor(estimated.CauseDisaster, on)
	case factsv1.WaiverRequest_REASON_RETIRED:
		b.WaiverFor(estimated.CauseRetirement, on)
	case factsv1.WaiverRequest_REASON_DISABLED:
		b.WaiverFor(estimated.CauseDisability, on)
	}
	return b.Build()
}

func edate(d civil.Date) (estimated.Date, error) {
	if d.IsZero() {
		return estimated.Date{}, errors.New("paid: a date is missing")
	}
	return estimated.NewDate(d.Year(), d.Month(), d.Day())
}

func wire(d estimated.Date) *usv1.CalendarDate {
	if d.IsZero() {
		return nil
	}
	return &usv1.CalendarDate{Year: int32(d.Year()), Month: int32(d.Month()), Day: int32(d.Day())}
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

func (n *notes) add(required []working.Required, divergences []estimated.Divergence) {
	for _, q := range required {
		key := fmt.Sprintf("q%d:%s", q.Position(), q.Because())
		if n.seen[key] {
			continue
		}
		n.seen[key] = true
		n.questions = append(n.questions, &usv1.EstimatedQuestion{Position: q.Position().String(), Because: q.Because(), Question: q.Question(), Effect: q.Effect()})
		text, effect := q.Question(), q.Effect()
		if text == "" {
			text = q.Because()
		}
		if effect == "" {
			effect = q.Because()
		}
		n.scenario.Ask(model.NewQuestion(fmt.Sprintf("estimated/%s/%s", n.j, strings.ReplaceAll(q.Position().String(), " ", "-")),
			fmt.Sprintf("estimated[%s]", n.j), text, effect, "facts"))
	}
	for _, d := range divergences {
		key := "d:" + d.Rule()
		if n.seen[key] {
			continue
		}
		n.seen[key] = true
		uses := usv1.DivergenceReading_DIVERGENCE_READING_UNSPECIFIED
		switch d.Uses() {
		case working.ReadingForm:
			uses = usv1.DivergenceReading_DIVERGENCE_READING_FORM
		case working.ReadingStatute:
			uses = usv1.DivergenceReading_DIVERGENCE_READING_STATUTE
		}
		n.divergences = append(n.divergences, &usv1.EstimatedDivergence{Rule: d.Rule(), Form: d.Form(), FormSource: d.FormSource(),
			Statute: d.Statute(), StatuteSource: d.StatuteSource(), Uses: uses, AffectsHousehold: d.AffectsHousehold()})
	}
}
