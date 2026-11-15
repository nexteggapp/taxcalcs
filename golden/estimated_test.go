package golden

// The estimated tax vectors: every jurisdiction's answers for every
// household estimatedtest builds, for each year in estimatedYears, written
// by estimatedAnswers. The Rust crate estimatedtest has the same function,
// golden_answers, so each jurisdiction's Rust test compares its own rules
// with its file in one line.
//
// Amounts are integer cents, rates integer basis points (millionths for a
// fine rate), dates YYYY-MM-DD with "" for the zero Date, enums their
// String(), and text verbatim. A method's error is {"error": text} in
// place of its result.

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/estimated/all"
	"github.com/nexteggapp/taxcalcs/estimated/estimatedtest"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// estimatedYears are the tax years every jurisdiction's vectors cover.
var estimatedYears = []int{2025, 2026}

func TestEstimated(t *testing.T) {
	for _, j := range estimated.Jurisdictions() {
		checkGzip(t, "estimated/"+string(j)+".json.gz", estimatedAnswers(j, func(year int) (estimated.EstimatedTax, error) {
			return all.For(j, year)
		}))
	}
	// The reference rules below, which exercise the shared helpers and this
	// encoding before any jurisdiction is ported.
	checkGzip(t, "estimated/"+string(referenceCode)+".json.gz", estimatedAnswers(referenceCode, newReference))

	hs := map[string]any{}
	for _, j := range []estimated.Jurisdiction{estimated.Federal, estimated.California, estimated.Washington} {
		var fs []any
		for _, h := range estimatedtest.Households(j) {
			fs = append(fs, factsJSON(h))
		}
		hs[string(j)] = fs
	}
	checkGzip(t, "estimated/households.json.gz", hs)
}

// checkGzip is check for a vector too large to keep as plain JSON: the file
// is the same indented JSON, gzipped. It compares the JSON, not the gzip
// bytes, which another Go release may compress differently, and rewrites
// the file only when the JSON changed.
func checkGzip(t *testing.T, name string, v any) {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	b = append(b, '\n')
	read := func(path string) ([]byte, error) {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		z, err := gzip.NewReader(f)
		if err != nil {
			return nil, err
		}
		return io.ReadAll(z)
	}
	if *update {
		dir := "testdata"
		if ws := os.Getenv("BUILD_WORKSPACE_DIRECTORY"); ws != "" {
			dir = filepath.Join(ws, "golden", "testdata")
		}
		path := filepath.Join(dir, name)
		if old, err := read(path); err == nil && bytes.Equal(old, b) {
			return
		}
		var z bytes.Buffer
		w, err := gzip.NewWriterLevel(&z, gzip.BestCompression)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(b); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, z.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := read(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("%v; run `bazel run //golden:golden_test -- -update`", err)
	}
	if !bytes.Equal(got, b) {
		t.Errorf("testdata/%s is stale; run `bazel run //golden:golden_test -- -update`", name)
	}
}

// estimatedAnswers is every answer rulesFor(year) gives the households of j.
func estimatedAnswers(j estimated.Jurisdiction, rulesFor func(int) (estimated.EstimatedTax, error)) map[string]any {
	hs := estimatedtest.Households(j)
	years := []any{}
	for _, year := range estimatedYears {
		rules, err := rulesFor(year)
		if err != nil {
			years = append(years, map[string]any{"year": year, "error": err.Error()})
			continue
		}
		as := estimatedtest.Ask(rules, hs)
		households := []any{}
		for _, a := range as {
			households = append(households, answersJSON(a))
		}
		years = append(years, map[string]any{
			"year":            year,
			"explanation":     explanationJSON(rules.Explanation()),
			"households":      households,
			"answer_problems": strs(estimatedtest.CheckAnswers(as)),
			"copy_problems":   strs(estimatedtest.CheckCopy(rules, estimatedtest.Texts(rules, as))),
		})
	}
	return map[string]any{"jurisdiction": string(j), "years": years}
}

func strs(ss []string) []any {
	out := []any{}
	for _, s := range ss {
		out = append(out, s)
	}
	return out
}

func dateJSON(d estimated.Date) string {
	if d.IsZero() {
		return ""
	}
	return d.String()
}

func resultJSON[T any](r working.Result[T], err error, value func(T) any) map[string]any {
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	applied, required, divergences := []any{}, []any{}, []any{}
	for _, a := range r.Applied() {
		applied = append(applied, map[string]any{"cite": a.Cite(), "applied": a.Applied()})
	}
	for _, q := range r.Required() {
		required = append(required, map[string]any{
			"position": q.Position().String(), "because": q.Because(), "question": q.Question(),
			"effect": q.Effect(), "asked": q.Asked(),
		})
	}
	for _, d := range r.Divergences() {
		divergences = append(divergences, map[string]any{
			"rule": d.Rule(), "form": d.Form(), "form_source": d.FormSource(), "statute": d.Statute(),
			"statute_source": d.StatuteSource(), "uses": d.Uses().String(), "affects_household": d.AffectsHousehold(),
		})
	}
	return map[string]any{"value": value(r.Value()), "applied": applied, "required": required,
		"divergences": divergences, "needs": r.Needs()}
}

func optionJSON(o estimated.TargetOption) map[string]any {
	return map[string]any{"basis": o.Basis().String(), "percent": int64(o.Percent()), "amount": int64(o.Amount()),
		"available": o.Available(), "why_not": o.WhyNot(), "label": o.Label()}
}

func requirementJSON(r estimated.Requirement) any {
	options := []any{}
	for _, o := range r.Options() {
		options = append(options, optionJSON(o))
	}
	var chosen any
	if c, ok := r.Chosen(); ok {
		chosen = optionJSON(c)
	}
	return map[string]any{"required": r.Required(), "why": r.Why(), "target": int64(r.Target()), "exempt": r.Exempt(),
		"target_known": r.TargetKnown(), "options": options, "chosen": chosen}
}

func scheduleJSON(is []estimated.Installment) any {
	out := []any{}
	for _, i := range is {
		out = append(out, map[string]any{"number": i.Number(), "due": dateJSON(i.Due()), "needed_by": int64(i.NeededBy()),
			"credited_by": int64(i.CreditedBy()), "shortfall": int64(i.Shortfall()), "status": i.Status().String()})
	}
	return out
}

func penaltyJSON(p estimated.Penalty) any {
	lines := []any{}
	for _, l := range p.Lines() {
		parts := []any{}
		for _, pt := range l.Parts() {
			parts = append(parts, map[string]any{"kind": pt.Kind().String(), "principal": int64(pt.Principal()),
				"from": dateJSON(pt.From()), "to": dateJSON(pt.To()), "days": pt.Days(), "months": pt.Months(),
				"rate": int64(pt.Rate()), "fine_rate": int64(pt.FineRate()), "charge": int64(pt.Charge()), "source": pt.Source()})
		}
		lines = append(lines, map[string]any{"installment": l.Installment(), "due": dateJSON(l.Due()),
			"shortfall": int64(l.Shortfall()), "days": l.Days(), "parts": parts, "total": int64(l.Total())})
	}
	var outcome any
	if o, ok := p.WaiverOutcome(); ok {
		kept := []any{}
		for _, k := range o.Kept() {
			kept = append(kept, k.String())
		}
		outcome = map[string]any{"eligible": o.Eligible(), "refused": o.Refused(), "undecided": o.Undecided(),
			"reason": o.Reason(), "kept": kept}
	}
	return map[string]any{"reason": p.Reason(), "total": int64(p.Total()), "lines": lines, "waiver": p.Waiver().String(),
		"as_of": dateJSON(p.AsOf()), "upper_bound": p.UpperBound(), "rated_through": dateJSON(p.RatedThrough()),
		"waiver_outcome": outcome, "if_waived": int64(p.IfWaived()), "short_due_dates": p.ShortDueDates(),
		"summary": p.Summary()}
}

func explanationJSON(e estimated.Explanation) map[string]any {
	return map[string]any{"summary": e.Summary(), "body": strs(e.Body()), "versus_federal": e.VersusFederal(),
		"sources": strs(e.Sources())}
}

func answersJSON(a estimatedtest.Answers) map[string]any {
	req, reqErr := a.Requirement()
	sched, schedErr := a.Schedule()
	pen, penErr := a.Penalty()
	return map[string]any{
		"name":        a.Household().Name(),
		"requirement": resultJSON(req, reqErr, requirementJSON),
		"schedule":    resultJSON(sched, schedErr, scheduleJSON),
		"penalty":     resultJSON(pen, penErr, penaltyJSON),
	}
}

func figuresJSON(f estimated.Figures) map[string]any {
	c := func(a money.USCents, ok bool) any {
		if !ok {
			return nil
		}
		return int64(a)
	}
	return map[string]any{
		"tax": c(f.Tax()), "federal_agi": c(f.FederalAGI()), "state_agi": c(f.StateAGI()),
		"gross_income": c(f.GrossIncome()), "taxable_income": c(f.TaxableIncome()), "local_tax": c(f.LocalTax()),
		"withheld": c(f.Withheld()),
	}
}

// factsJSON is every accessor of a household's facts.
func factsJSON(h estimatedtest.Household) map[string]any {
	f := h.Facts()
	residence := func(ps []estimated.ResidencePeriod) []any {
		out := []any{}
		for _, p := range ps {
			out = append(out, map[string]any{"state": p.State(), "from": dateJSON(p.From()), "to": dateJSON(p.To())})
		}
		return out
	}
	var prior any
	if p, ok := f.PriorYear(); ok {
		var figs, resident any
		if fs, ok := p.Figures(); ok {
			figs = figuresJSON(fs)
		}
		if r, ok := p.FullYearResident(); ok {
			resident = r
		}
		prior = map[string]any{"filed": p.Filed(), "not_required_to_file": p.NotRequiredToFile(), "months": p.Months(),
			"figures": figs, "full_year_resident": resident}
	}
	withholding, payments, people, periods := []any{}, []any{}, []any{}, []any{}
	for _, w := range f.Withholding() {
		withholding = append(withholding, map[string]any{"on": dateJSON(w.On()), "amount": int64(w.Amount())})
	}
	for _, p := range f.Payments() {
		payments = append(payments, map[string]any{"on": dateJSON(p.On()), "amount": int64(p.Amount()), "from_prior_year": p.FromPriorYear()})
	}
	for _, p := range f.People() {
		people = append(people, map[string]any{"age": p.Age(), "retired_after_62": p.RetiredAfter62(), "disabled": p.Disabled()})
	}
	for _, p := range f.Periods() {
		periods = append(periods, map[string]any{"end": dateJSON(p.End()), "income": int64(p.Income()), "annualised_tax": int64(p.AnnualisedTax())})
	}
	pos := f.Positions()
	var waiver any
	if k, ok := pos.Waiver(); ok {
		waiver = k.String()
	}
	return map[string]any{
		"name": h.Name(), "jurisdiction": string(f.Jurisdiction()), "tax_year": f.TaxYear(),
		"filing_status": f.FilingStatus().String(), "residence": residence(f.Residence()),
		"prior_residence": residence(f.PriorResidence()), "this_year": figuresJSON(f.ThisYear()), "prior_year": prior,
		"withholding": withholding, "payments": payments, "return_filed": dateJSON(f.ReturnFiled()),
		"requirement_first_met": dateJSON(f.RequirementFirstMet()), "people": people,
		"farm_share": int64(f.FarmShare()), "fish_share": int64(f.FishShare()),
		"elections": map[string]any{"annualised": f.Elections().Annualised(), "withholding_as_withheld": f.Elections().WithholdingAsWithheld()},
		"positions": map[string]any{"waiver": waiver, "waiver_on": dateJSON(pos.WaiverOn()),
			"waiver_cause": pos.WaiverCause().String(), "born": dateJSON(pos.Born())},
		"periods": periods, "withheld_total": int64(f.WithheldTotal()), "farmer_or_fisher": f.FarmerOrFisher(),
		"has_state": f.State() != nil,
	}
}

// The reference rules: a small jurisdiction, "ZZ", written once here and
// once in the Rust crate estimatedtest's tests, so that the shared helpers
// (calendars, shares, credits, payment application, simple and compound
// interest, the standard waiver) and this encoding are checked against
// each other before any real jurisdiction is ported. They are not any
// jurisdiction's law.

const referenceCode = estimated.Jurisdiction("ZZ")

type reference struct {
	year     int
	calendar estimated.Calendar
	rates    estimated.RateSeries
}

func newReference(year int) (estimated.EstimatedTax, error) {
	if year != 2026 {
		return nil, fmt.Errorf("%w: %s %d", estimated.ErrNoRules, referenceCode, year)
	}
	d := func(y int, m time.Month, day int) estimated.Date { return estimated.MustDate(y, m, day) }
	c, err := estimated.NewCalendar("Reference holidays").Holiday(d(2026, time.June, 15), "Reference Day").Build()
	if err != nil {
		return nil, err
	}
	rates, err := estimated.NewRateSeries().
		Period(d(2026, time.January, 1), d(2026, time.June, 30), money.Percent(7), "Reference rate H1").
		FinePeriod(d(2026, time.July, 1), d(2027, time.March, 31), 108_133, "Reference rate H2").
		Build()
	if err != nil {
		return nil, err
	}
	return reference{year: year, calendar: c, rates: rates}, nil
}

func (r reference) dues() []estimated.Date {
	return estimated.MoveDates(r.calendar, []estimated.Date{
		estimated.MustDate(r.year, time.April, 15), estimated.MustDate(r.year, time.June, 15),
		estimated.MustDate(r.year, time.September, 15), estimated.MustDate(r.year+1, time.January, 15),
	})
}

var referenceShares = []money.Rate{money.Percent(25), money.Percent(25), money.Percent(25), money.Percent(25)}

func (r reference) requirement(f estimated.Facts) (estimated.Requirement, working.Notes, error) {
	var notes working.Notes
	withheld := f.WithheldTotal()
	b := estimated.NewRequirement()
	tax, known := f.ThisYear().Tax()
	if known {
		o, err := estimated.NewTargetOption(estimated.BasisThisYearTax, money.Percent(90), money.Percent(90).Of(tax)).Build()
		if err != nil {
			return estimated.Requirement{}, notes, err
		}
		b.Option(o)
	} else {
		o, err := estimated.NewTargetOption(estimated.BasisThisYearTax, money.Percent(90), 0).Unavailable(estimated.NeedsThisYearTax).Build()
		if err != nil {
			return estimated.Requirement{}, notes, err
		}
		b.Option(o)
	}
	p, hasPrior := f.PriorYear()
	priorFigures, filed := p.Figures()
	priorTax, _ := priorFigures.Tax()
	lastOpen := hasPrior && p.Filed() && filed
	if lastOpen {
		o, err := estimated.NewTargetOption(estimated.BasisPriorYearTax, money.Percent(100), priorTax).Build()
		if err != nil {
			return estimated.Requirement{}, notes, err
		}
		b.Option(o)
	} else {
		o, err := estimated.NewTargetOption(estimated.BasisPriorYearTax, money.Percent(100), 0).Unavailable("No return was filed for last year.").Build()
		if err != nil {
			return estimated.Requirement{}, notes, err
		}
		b.Option(o)
	}
	if !known {
		notes.Ask(working.PositionThisYearTax, fmt.Sprintf("What do you expect your %d tax to be?", r.year), estimated.WithoutThisYearTax(lastOpen))
	}
	notes.Rule("Reference § 1", fmt.Sprintf("withholding of %v", withheld))
	switch {
	case known && tax-withheld < 1_000*money.Dollar:
		e, err := estimated.ExceptionBelow("Tax less withholding", tax-withheld, 1_000*money.Dollar)
		if err != nil {
			return estimated.Requirement{}, notes, err
		}
		b.Required(false, fmt.Sprintf("Your tax %s.", estimated.Leaves(tax-withheld))).Exempt(e)
	case lastOpen && priorTax == 0:
		e, err := estimated.NewException("No tax due last year").Build()
		if err != nil {
			return estimated.Requirement{}, notes, err
		}
		b.Required(false, "You had no tax last year.").Exempt(e)
	case !known && !lastOpen:
		b.Required(false, "No target can be set yet.").NoTarget()
	default:
		basis, target := estimated.BasisPriorYearTax, priorTax
		if known && (!lastOpen || money.Percent(90).Of(tax) < priorTax) {
			basis, target = estimated.BasisThisYearTax, money.Percent(90).Of(tax)
		}
		b.Required(target > withheld, fmt.Sprintf("The target is %s, and %s is withheld.", target.Whole(), withheld.Whole())).Choose(basis)
	}
	req, err := b.Build()
	return req, notes, err
}

func (r reference) ledger(f estimated.Facts, req estimated.Requirement) ([]estimated.Installment, []estimated.LedgerEntry, error) {
	dues := r.dues()
	cum, err := estimated.CumulativeShares(req.Target(), referenceShares)
	if err != nil {
		return nil, nil, err
	}
	var credits []estimated.Payment
	if f.Elections().WithholdingAsWithheld() {
		credits, err = estimated.CreditsAsWithheld(f, dues)
	} else {
		credits, err = estimated.Credits(f, dues, referenceShares)
	}
	if err != nil {
		return nil, nil, err
	}
	ledger, err := estimated.Apply(dues, estimated.Increments(cum), credits)
	if err != nil {
		return nil, nil, err
	}
	var out []estimated.Installment
	for i, e := range ledger {
		b := estimated.NewInstallment(i+1, dues[i]).NeededBy(cum[i]).CreditedBy(e.CreditedBy())
		if e.CreditedBy() < cum[i] {
			b.Shortfall(e.Shortfall())
		}
		if req.Exempt() {
			b.Excused()
		}
		in, err := b.Build()
		if err != nil {
			return nil, nil, err
		}
		out = append(out, in)
	}
	return out, ledger, nil
}

func (r reference) Requirement(f estimated.Facts) (working.Result[estimated.Requirement], error) {
	req, notes, err := r.requirement(f)
	if err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	d, err := estimated.NewDivergence("The reference threshold").
		Form("The form tests tax less withholding against $1,000.", "Reference Form 1, line 3").
		Statute("The statute tests it against $1,000 or 10% of the tax.", "Reference § 2").
		Uses(working.ReadingForm).Affects(false).Build()
	if err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	return working.NewResult(req).With(notes).Diverges(d).Build()
}

func (r reference) Schedule(f estimated.Facts) (working.Result[[]estimated.Installment], error) {
	req, notes, err := r.requirement(f)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	if !req.TargetKnown() {
		return working.Result[[]estimated.Installment]{}, estimated.ErrNeedsThisYearTax
	}
	is, _, err := r.ledger(f, req)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	return working.NewResult(is).With(notes).Rule("Reference § 3", "four equal installments").Build()
}

func (r reference) Penalty(f estimated.Facts, asOf estimated.Date) (working.Result[estimated.Penalty], error) {
	req, notes, err := r.requirement(f)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !req.TargetKnown() {
		return working.Result[estimated.Penalty]{}, estimated.ErrNeedsThisYearTax
	}
	is, ledger, err := r.ledger(f, req)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	waiver, _ := f.Positions().Waiver()
	if req.Exempt() {
		p, err := estimated.NewPenalty(asOf).Waiver(waiver).Because(req, is).Build()
		if err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		return working.NewResult(p).With(notes).Build()
	}
	stop := estimated.MustDate(r.year+1, time.April, 15)
	if asOf.Before(stop) {
		stop = asOf
	}
	if filed := f.ReturnFiled(); !filed.IsZero() && filed.Before(stop) {
		stop = filed
	}
	p, unrated, err := estimated.SimplePenalty(ledger, stop, r.rates, waiver, req, is)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	b := p.ToBuilder()
	// The first shortfall also bears compound interest to the stop, as a
	// second charge on the same line, to exercise CompoundDaily.
	if len(p.Lines()) > 0 && f.Elections().Annualised() {
		b = estimated.NewPenalty(stop).Waiver(waiver).Because(req, is)
		for i, l := range p.Lines() {
			if i > 0 {
				b.Line(l)
				continue
			}
			parts, _, err := estimated.CompoundDaily(l.Shortfall(), l.Due(), stop, r.rates)
			if err != nil {
				return working.Result[estimated.Penalty]{}, err
			}
			lb := estimated.NewPenaltyLine(l.Installment(), l.Due(), l.Shortfall()).Days(l.Days())
			for _, part := range parts {
				lb.Part(part)
			}
			line, err := lb.Build()
			if err != nil {
				return working.Result[estimated.Penalty]{}, err
			}
			b.Line(line)
		}
		if !unrated.IsZero() {
			b.RatedThrough(unrated.AddDays(-1))
		}
	}
	if !unrated.IsZero() {
		notes.Ask(working.PositionUnpublishedRate, "", fmt.Sprintf("The rate for the days after %s isn't published yet.", r.rates.Ends().Long()))
	}
	if o, ok := estimated.StandardWaiver(f, "the reference authority", 62); ok && p.Total() > 0 {
		b.WaiverOutcome(o)
	}
	if !f.RequirementFirstMet().IsZero() {
		b.Context("A late start does not change the due dates.")
	}
	out, err := b.Build()
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	return working.NewResult(out).With(notes).Build()
}

func (r reference) Explanation() estimated.Explanation {
	e, _ := estimated.NewExplanation("The reference rules ask for four equal payments.").
		Paragraph("Pay a quarter of the target by each due date.").
		Paragraph("Interest runs on what is short until it is paid.").
		Source("Reference § 1").Source("Reference Form 1").Build()
	return e
}
