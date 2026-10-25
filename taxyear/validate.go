package taxyear

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/internal/civil"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	"github.com/nexteggapp/taxcalcs/taxyear/model/employer"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
)

// CheckFile runs the checks that need only the file and no parent
// scenario: the version and year, and no identifiers in any form. Each
// failure is a problem with its path. See docs/scenarios.md.
func CheckFile(file *scenariosv1.TaxYearScenarios) []*scenariosv1.Problem {
	var out []*scenariosv1.Problem
	headerChecks(file, &out)
	identifiers("", file.ProtoReflect(), &out)
	return out
}

// headerChecks checks the version, the year and as_of.
func headerChecks(file *scenariosv1.TaxYearScenarios, out *[]*scenariosv1.Problem) {
	if file.GetSchemaVersion() != 1 {
		*out = append(*out, problem("schema_version", fmt.Sprintf("Schema version %d is not one this build reads; it reads 1.", file.GetSchemaVersion())))
	}
	if y := file.GetShared().GetFacts().GetTaxYear(); y != 0 && y != file.GetTaxYear() {
		*out = append(*out, problem("shared.facts.tax_year", fmt.Sprintf("The facts are for %d and the file for %d.", y, file.GetTaxYear())))
	}
	if _, err := civil.FromProto(file.GetAsOf()); err != nil || file.GetAsOf() == nil {
		*out = append(*out, problem("as_of", "as_of is not a day."))
	}
}

// checkFileWith is CheckFile with the identifiers under shared already
// found.
func checkFileWith(file *scenariosv1.TaxYearScenarios, shared []*scenariosv1.Problem) []*scenariosv1.Problem {
	var out []*scenariosv1.Problem
	headerChecks(file, &out)
	file.ProtoReflect().Range(func(f protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if f.Name() == "shared" {
			out = append(out, shared...)
		} else {
			identifiersIn("", false, f, v, &out)
		}
		return true
	})
	return out
}

// cleared are the fields the facts doc clears on every form.
var cleared = map[protoreflect.Name]bool{
	"tin": true, "account_number": true, "control_number": true, "state_id": true,
	"payer_routing_number": true, "refund_routing_number": true, "refund_account_number": true,
	"account_or_escrow_number": true, "marketplace_identifier": true, "marketplace_assigned_policy_number": true,
	"spouse_tin": true, "account_beneficiary_tin": true, "proprietor_tin": true,
	"employer_id_number": true, "taxpayer_tin": true, "buyer_tin": true, "filer_tin": true,
}

// opaque are text fields that name a build or a reference, never a
// person or an account, which the nine-digit check skips.
var opaque = map[protoreflect.FullName]bool{
	"nexteggapp.taxcalcs.scenarios.v1.TaxYearScenarios.exported_from": true,
}

// nineDigits is nine digits in a row, with or without dashes between them.
var nineDigits = regexp.MustCompile(`\d(?:-?\d){8}`)

// identifiers records each cleared field that is set, each Party address or
// telephone, and each text field holding nine digits in a row.
func identifiers(path string, m protoreflect.Message, out *[]*scenariosv1.Problem) {
	party := m.Descriptor().Name() == "Party"
	m.Range(func(f protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		identifiersIn(path, party, f, v, out)
		return true
	})
}

// identifiersIn is identifiers on one field of a message at path.
func identifiersIn(path string, party bool, f protoreflect.FieldDescriptor, v protoreflect.Value, out *[]*scenariosv1.Problem) {
	p := string(f.Name())
	if path != "" {
		p = path + "." + p
	}
	switch {
	case f.Kind() == protoreflect.MessageKind && f.IsList():
		for i := 0; i < v.List().Len(); i++ {
			identifiers(p+"["+strconv.Itoa(i)+"]", v.List().Get(i).Message(), out)
		}
	case f.Kind() == protoreflect.MessageKind && !f.IsMap():
		identifiers(p, v.Message(), out)
	case f.Kind() == protoreflect.StringKind && !f.IsList() && !f.IsMap() && !opaque[f.FullName()]:
		s := v.String()
		switch {
		case cleared[f.Name()] || (party && (f.Name() == "address" || f.Name() == "telephone")):
			*out = append(*out, problem(p, "This field must be empty: the file carries nothing that identifies a person or reaches an account."))
		case nineDigits.MatchString(s):
			*out = append(*out, problem(p, "This holds nine digits in a row, which could be a Social Security number or an account number."))
		}
	}
}

// kinds says, for one model's part message, which oneof cases are known,
// which may appear more than once, which are ends, which is the known end,
// which are whole-year or rest-of-year parts, of which a model holds one,
// and which are period parts.
type kinds struct {
	known    []string
	multi    []string
	ends     []string
	knownEnd string
	totals   []string
	periods  []string
}

func has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

const pkg = "nexteggapp.taxcalcs.scenarios.v1."

var partKinds = map[protoreflect.FullName]kinds{
	pkg + "EmployerPart":    {known: []string{"from_paystubs", "from_deposits", "from_w2", "ended"}, multi: []string{"from_w2"}, ends: []string{"ended", "ends"}, knownEnd: "ended", totals: []string{"year_total"}, periods: []string{"pay_rate"}},
	pkg + "BusinessPart":    {known: []string{"from_ledger", "from_forms", "stopped"}, ends: []string{"stopped", "stops"}, knownEnd: "stopped", totals: []string{"year_total"}, periods: []string{"monthly"}},
	pkg + "AccountPart":     {known: []string{"from_credits", "from_forms", "closed"}, ends: []string{"closed", "closes"}, knownEnd: "closed", totals: []string{"year_total"}, periods: []string{"at_rate"}},
	pkg + "BrokerPart":      {known: []string{"from_trades", "from_forms", "purchases"}},
	pkg + "MortgagePart":    {known: []string{"from_form_1098", "from_statements", "paid_from_bank", "repaid"}, ends: []string{"repaid", "paid_off"}, knownEnd: "repaid", totals: []string{"year_total", "rest_of_year"}, periods: []string{"refinanced"}},
	pkg + "PropertyTaxPart": {known: []string{"from_bill", "paid_from_bank"}, totals: []string{"year_total", "rest_of_year"}},
	pkg + "CharityPart":     {known: []string{"gift_from_bank", "gift_with_receipt"}, totals: []string{"rest_of_year"}},
	pkg + "MedicalPart":     {known: []string{"paid_from_bank", "paid_with_receipt"}, totals: []string{"year_total", "rest_of_year"}},
	pkg + "OtherIncomePart": {known: []string{"from_forms", "from_ledger"}, totals: []string{"year_amount"}},
	pkg + "TaxPaidPart":     {known: []string{"payment_made", "overpayment_applied"}, multi: []string{"payment_made", "overpayment_applied"}},
	pkg + "RetirementPart":  {known: []string{"from_forms"}},
}

// KnownKinds returns a model part message's known cases, in field order:
// the cases that hold evidence, or an end that has happened. Every other
// case is assumed. It returns false for a message that isn't a model's
// part, and no cases and true for a part with none, as RetirementPart.
func KnownKinds(part protoreflect.MessageDescriptor) ([]protoreflect.FieldDescriptor, bool) {
	k, ok := partKinds[part.FullName()]
	if !ok {
		return nil, false
	}
	var out []protoreflect.FieldDescriptor
	fields := part.Fields()
	for i := 0; i < fields.Len(); i++ {
		if f := fields.Get(i); has(k.known, string(f.Name())) {
			out = append(out, f)
		}
	}
	return out, true
}

// sourced is one model in a scenario's inputs, found by reflection.
type sourced struct {
	list  string
	id    string
	msg   protoreflect.Message // the model, or for itemised its kind's model
	top   protoreflect.Message // the model message itself
	parts protoreflect.List
	path  string
}

// models lists every model in a TaxYearModel.
func models(m *scenariosv1.TaxYearModel) []sourced {
	var out []sourced
	r := m.ProtoReflect()
	fields := r.Descriptor().Fields()
	for i := 0; i < fields.Len(); i++ {
		f := fields.Get(i)
		l := r.Get(f).List()
		for k := 0; k < l.Len(); k++ {
			top := l.Get(k).Message()
			id := top.Get(top.Descriptor().Fields().ByName("id")).String()
			inner := top
			if o := top.Descriptor().Oneofs().ByName("kind"); o != nil {
				if which := top.WhichOneof(o); which != nil {
					inner = top.Get(which).Message()
				}
			}
			s := sourced{list: string(f.Name()), id: id, msg: inner, top: top, path: fmt.Sprintf("model.%s[%s]", f.Name(), id)}
			if pf := inner.Descriptor().Fields().ByName("parts"); pf != nil {
				s.parts = inner.Get(pf).List()
			}
			out = append(out, s)
		}
	}
	return out
}

// partCase returns a part's id and the name of its oneof case.
func partCase(part protoreflect.Message) (string, string, protoreflect.Message) {
	id := part.Get(part.Descriptor().Fields().ByName("id")).String()
	o := part.Descriptor().Oneofs().ByName("part")
	if o == nil {
		return id, "", nil
	}
	which := part.WhichOneof(o)
	if which == nil {
		return id, "", nil
	}
	return id, string(which.Name()), part.Get(which).Message()
}

// Validate runs the models doc's checks on one scenario's resolved inputs:
// each failure is a problem with its path in the inputs. Check 2, that no
// two items share an id, runs on the result. See docs/models.md.
func Validate(ctx *model.Context, in *scenariosv1.ScenarioInputs) []*scenariosv1.Problem {
	return validateWith(ctx, in, &modelSet{ctx: ctx})
}

// validateWith is Validate with each model's own checks kept in ms's
// cache: they read the model and the context alone. The ids each model
// records are replayed into one map, so an id two models share is still a
// repeat.
func validateWith(ctx *model.Context, in *scenariosv1.ScenarioInputs, ms *modelSet) []*scenariosv1.Problem {
	v := &validator{ctx: ctx, ids: map[string]string{}}
	v.facts(in)
	for _, s := range models(in.GetModel()) {
		c := ms.checked(s, func() *checkEntry { return checkModel(ctx, s) })
		for _, e := range c.ids {
			v.id(e.id, e.path)
		}
		v.out = append(v.out, c.problems...)
	}
	v.plans(in)
	v.overridePaths(in)
	sort.SliceStable(v.out, func(i, j int) bool { return v.out[i].GetPath() < v.out[j].GetPath() })
	return v.out
}

type validator struct {
	ctx *model.Context
	// idsOnly runs checks 1 and 3's id checks alone.
	idsOnly bool
	out     []*scenariosv1.Problem
	// ids maps every id to the path it was first seen at.
	ids map[string]string
	// record, when set, collects each id instead of checking it.
	record *[]idEvent
}

// idEvent is one id a model's checks record, at its path.
type idEvent struct{ id, path string }

// checkEntry is one model's own checks: the ids it records, in order, and
// every other problem. Nothing in it is changed once it is made.
type checkEntry struct {
	used     uint64
	ids      []idEvent
	problems []*scenariosv1.Problem
}

// checkModel runs one model's checks, recording its ids.
func checkModel(ctx *model.Context, s sourced) *checkEntry {
	r := &validator{ctx: ctx, ids: map[string]string{}, record: &[]idEvent{}}
	r.model(s)
	return &checkEntry{ids: *r.record, problems: r.out}
}

func (v *validator) fail(path, text string) { v.out = append(v.out, problem(path, text)) }

// id records an id, failing check 1 on a repeat.
func (v *validator) id(id, path string) {
	if v.record != nil {
		*v.record = append(*v.record, idEvent{id, path})
		return
	}
	if id == "" {
		v.fail(path, "An id is required.")
		return
	}
	if first, ok := v.ids[id]; ok {
		v.fail(path, fmt.Sprintf("The id %q is used again; it is %s's.", id, first))
		return
	}
	v.ids[id] = path
}

func (v *validator) facts(in *scenariosv1.ScenarioInputs) {
	f := in.GetFacts()
	for _, p := range f.GetPeople() {
		v.id(p.GetId(), fmt.Sprintf("facts.people[%s]", p.GetId()))
	}
	for _, r := range f.GetResidence() {
		v.id(r.GetId(), fmt.Sprintf("facts.residence[%s]", r.GetId()))
	}
	for _, e := range f.GetElections() {
		v.id(e.GetId(), fmt.Sprintf("facts.elections[%s]", e.GetId()))
	}
	for _, p := range f.GetPositions() {
		v.id(p.GetId(), fmt.Sprintf("facts.positions[%s]", p.GetId()))
	}
	for _, c := range f.GetCarryovers() {
		v.id(c.GetId(), fmt.Sprintf("facts.carryovers[%s]", c.GetId()))
	}
	for _, p := range f.GetPriorYear() {
		v.id(p.GetId(), fmt.Sprintf("facts.prior_year[%s]", p.GetId()))
	}
	for _, u := range f.GetUnassigned() {
		v.id(u.GetId(), fmt.Sprintf("facts.unassigned[%s]", u.GetId()))
	}
}

// CheckIDs runs the id checks alone, 1 and the first of 3, on shared
// before any scenario is resolved: apply can't key a list with a repeated
// id, so a repeat there would be reported as every scenario failing.
func CheckIDs(in *scenariosv1.ScenarioInputs) []*scenariosv1.Problem {
	v := &validator{idsOnly: true, ids: map[string]string{}}
	v.facts(in)
	for _, s := range models(in.GetModel()) {
		v.model(s)
	}
	for _, p := range v.out {
		p.Path = "shared." + p.GetPath()
	}
	return v.out
}

// tally is what one model's parts hold, as the checks count it.
type tally struct {
	evidence map[string]bool
	count    map[string]int
	ends     []string
	totals   []string
	starts   []start
}

// model runs checks 1 and 3 to 9 on one model.
func (v *validator) model(s sourced) {
	v.id(s.id, s.path)
	t := &tally{evidence: map[string]bool{}, count: map[string]int{}}
	for i := 0; s.parts != nil && i < s.parts.Len(); i++ {
		v.part(s, s.parts.Get(i).Message(), t)
	}
	if v.idsOnly {
		return
	}
	if len(t.ends) > 1 {
		v.fail(s.path, "A model has at most one end part.")
	}
	if len(t.totals) > 1 {
		v.fail(s.path, "A model has at most one total for the year or for the rest of it.")
	}
	seen := map[string]string{}
	for _, st := range t.starts {
		key := st.at.String()
		if other, ok := seen[key]; ok {
			when := "an unset start"
			if !st.at.IsZero() {
				when = st.at.Long()
			}
			v.fail(fmt.Sprintf("%s.parts[%s]", s.path, st.part), fmt.Sprintf("Two changes, this and %s, start on %s.", other, when))
		}
		seen[key] = st.part
	}
	v.defaultPart(s)
	v.answers(s, t.evidence)
	if s.list == "employers" {
		v.employer(s, t.starts)
	}
}

// part checks one part's id and kind, and tallies it.
func (v *validator) part(s sourced, part protoreflect.Message, t *tally) {
	k, known := partKinds[s.partName()]
	id, kind, body := partCase(part)
	path := fmt.Sprintf("%s.parts[%s]", s.path, id)
	v.id(id, path)
	switch {
	case kind == "":
		v.fail(path, "The part has no kind set.")
		return
	case !known:
		return
	}
	if has(k.known, kind) {
		collectEvidence(body, path, t.evidence, v)
	}
	if v.idsOnly {
		return
	}
	t.count[kind]++
	if has(k.known, kind) && t.count[kind] > 1 && !has(k.multi, kind) {
		v.fail(path, fmt.Sprintf("A model has at most one %s part.", kind))
	}
	if has(k.ends, kind) {
		t.ends = append(t.ends, kind)
		if kind == k.knownEnd {
			v.knownEnd(body, path)
		}
	}
	if has(k.totals, kind) {
		t.totals = append(t.totals, id)
	}
	if has(k.periods, kind) {
		d, err := civil.FromProto(dateField(body, "from"))
		if err != nil {
			v.fail(path+"."+kind+".from", "This is not a day.")
		}
		t.starts = append(t.starts, start{id, d})
	}
}

func (s sourced) partName() protoreflect.FullName {
	if s.parts == nil {
		return ""
	}
	return s.parts.NewElement().Message().Descriptor().FullName()
}

// collectEvidence records each evidence entry's id in a known part: every
// message inside it with an id field. Check 3: ids are unique in a model.
func collectEvidence(m protoreflect.Message, path string, seen map[string]bool, v *validator) {
	m.Range(func(f protoreflect.FieldDescriptor, val protoreflect.Value) bool {
		if f.Kind() != protoreflect.MessageKind || !f.IsList() {
			return true
		}
		l := val.List()
		for i := 0; i < l.Len(); i++ {
			e := l.Get(i).Message()
			idf := e.Descriptor().Fields().ByName("id")
			if idf == nil {
				continue
			}
			id := e.Get(idf).String()
			p := fmt.Sprintf("%s.%s[%s]", path, f.Name(), id)
			switch {
			case id == "":
				v.fail(p, "An evidence entry needs an id.")
			case seen[id]:
				v.fail(p, fmt.Sprintf("The evidence id %q is used twice in this model.", id))
			}
			seen[id] = true
		}
		return true
	})
	for _, name := range []protoreflect.Name{"transfer", "confirmation"} {
		if f := m.Descriptor().Fields().ByName(name); f != nil && m.Has(f) {
			e := m.Get(f).Message()
			if id := e.Get(e.Descriptor().Fields().ByName("id")).String(); id != "" {
				if seen[id] {
					v.fail(path, fmt.Sprintf("The evidence id %q is used twice in this model.", id))
				}
				seen[id] = true
			}
		}
	}
}

// commonDate is the proto date type, named for brevity.
type commonDate = commonv1.Date

func dateField(m protoreflect.Message, name protoreflect.Name) *commonDate {
	f := m.Descriptor().Fields().ByName(name)
	if f == nil || !m.Has(f) {
		return nil
	}
	d, _ := m.Get(f).Message().Interface().(*commonDate)
	return d
}

// knownEnd checks 6: a known end is on or before as_of.
func (v *validator) knownEnd(body protoreflect.Message, path string) {
	d := dateField(body, "last_paycheck")
	if d == nil {
		d = dateField(body, "on")
	}
	at, err := civil.FromProto(d)
	if err != nil || at.IsZero() {
		v.fail(path, "An end needs its date.")
		return
	}
	if at.After(v.ctx.AsOf()) {
		v.fail(path, fmt.Sprintf("A known end, on %s, can't be after as_of, %s; record it as expected instead.", at.Long(), v.ctx.AsOf().Long()))
	}
}

// defaultPart checks 4: a model with slots after its evidence has a default
// part. A model whose year a whole-year document states needs none.
func (v *validator) defaultPart(s sourced) {
	f := s.msg.Descriptor().Fields().ByName("default_part")
	if f == nil || s.msg.Has(f) {
		return
	}
	if v.ctx.Finished() {
		return
	}
	for i := 0; s.parts != nil && i < s.parts.Len(); i++ {
		_, kind, _ := partCase(s.parts.Get(i).Message())
		if kind == "from_w2" || kind == "from_form_1098" || kind == "ended" || kind == "stopped" || kind == "closed" || kind == "repaid" {
			return
		}
	}
	v.fail(s.path+".default_part", "A model with dates still to come needs a default part.")
}

// answers checks 8: a planned part's actual names evidence in the model.
func (v *validator) answers(s sourced, evidence map[string]bool) {
	for i := 0; s.parts != nil && i < s.parts.Len(); i++ {
		id, kind, body := partCase(s.parts.Get(i).Message())
		if body == nil {
			continue
		}
		f := body.Descriptor().Fields().ByName("actual")
		if f == nil || !body.Has(f) {
			continue
		}
		a, _ := body.Get(f).Message().Interface().(*scenariosv1.Actual)
		path := fmt.Sprintf("%s.parts[%s].%s.actual", s.path, id, kind)
		for _, e := range append([]string{a.GetIs()}, a.GetIsNot()...) {
			if e != "" && !evidence[e] {
				v.fail(path, fmt.Sprintf("%q names no evidence in this model.", e))
			}
		}
	}
}

// start is a period part's start, zero when unset.
type start struct {
	part string
	at   civil.Date
}

// employer runs the employer's own checks: required fields (9), matches
// naming a pay date (8), and period parts within the job (5).
func (v *validator) employer(s sourced, starts []start) {
	m, _ := s.top.Interface().(*scenariosv1.EmployerModel)
	need := func(ok bool, field, text string) {
		if !ok {
			v.fail(s.path+"."+field, text)
		}
	}
	need(m.GetPerson() != "", "person", "Whose job it is is required.")
	need(m.GetPayFrequency() != scenariosv1.PayFrequency_PAY_FREQUENCY_UNSPECIFIED, "pay_frequency", "How often the job pays is required.")
	need(m.GetSupplemental() != scenariosv1.SupplementalMethod_SUPPLEMENTAL_METHOD_UNSPECIFIED, "supplemental", "How the employer withholds from a bonus is required.")
	weekly := m.GetPayFrequency() == scenariosv1.PayFrequency_PAY_FREQUENCY_WEEKLY || m.GetPayFrequency() == scenariosv1.PayFrequency_PAY_FREQUENCY_BIWEEKLY
	need(!weekly || m.GetFirstPayDate() != nil, "first_pay_date", "A weekly or every-other-week job needs its first pay date.")
	first, _ := civil.FromProto(m.GetFirstPayDate())
	from, _ := civil.FromProto(m.GetStarts())
	slots := map[string]bool{}
	for _, d := range employer.PayDates(v.ctx.Year(), m.GetPayFrequency(), first, from) {
		slots[d.String()] = true
	}
	end := v.employerParts(s, m, slots)
	for _, st := range starts {
		path := fmt.Sprintf("%s.parts[%s]", s.path, st.part)
		if !st.at.IsZero() && !from.IsZero() && st.at.Before(from) {
			v.fail(path, fmt.Sprintf("A change can't start before the job does, on %s.", from.Long()))
		}
		if !st.at.IsZero() && !end.IsZero() && st.at.After(end) {
			v.fail(path, fmt.Sprintf("A change can't start after the job's planned end on %s.", end.Long()))
		}
	}
}

// employerParts checks a job's one-offs have their date and amount, and
// that every answer naming a pay date names one of the job's. It returns
// the job's assumed end, or zero.
func (v *validator) employerParts(s sourced, m *scenariosv1.EmployerModel, slots map[string]bool) civil.Date {
	var end civil.Date
	for _, p := range m.GetParts() {
		path := fmt.Sprintf("%s.parts[%s]", s.path, p.GetId())
		if e := p.GetEnds(); e != nil {
			end, _ = civil.FromProto(e.GetLastPaycheck())
		}
		if o := p.GetOneOff(); o != nil {
			if o.GetDate() == nil || o.GetAmount() == nil {
				v.fail(path+".one_off", "A one-off needs its date and amount.")
			}
		}
		var matches []*scenariosv1.Match
		for _, st := range p.GetFromPaystubs().GetStubs() {
			matches = append(matches, st.GetMatches())
		}
		for _, d := range p.GetFromDeposits().GetDeposits() {
			matches = append(matches, d.GetMatches())
		}
		for _, mt := range matches {
			if mt.GetSlot() == nil {
				continue
			}
			if d, err := civil.FromProto(mt.GetSlot()); err != nil || !slots[d.String()] {
				v.fail(path, fmt.Sprintf("An answer names %s, which is not one of the job's pay dates.", d))
			}
		}
	}
	return end
}

// plans checks 7: a person has at most one plan adjustment part.
func (v *validator) plans(in *scenariosv1.ScenarioInputs) {
	byPerson := map[string]int{}
	for _, m := range in.GetModel().GetRetirement() {
		for _, p := range m.GetParts() {
			if p.GetMaxOutWorkplacePlan() != nil || p.GetWorkplacePlanTotal() != nil {
				byPerson[m.GetPerson()]++
				if byPerson[m.GetPerson()] > 1 {
					v.fail(fmt.Sprintf("model.retirement[%s].parts[%s]", m.GetId(), p.GetId()), "A person has at most one plan for their workplace plan.")
				}
			}
		}
	}
}

// overridePaths checks the scenarios doc's check 8: an override names a
// path in its table.
func (v *validator) overridePaths(in *scenariosv1.ScenarioInputs) {
	for _, o := range in.GetOverrides() {
		if !OverridablePath(o.GetPath()) {
			v.fail(fmt.Sprintf("overrides[%s]", o.GetPath()), "Only a figure so far, last year's figures, or a state's tax can be overridden.")
		}
	}
}

// OverridablePath reports whether a path is one the scenarios doc's table
// lets an override name: a figure so far in item terms, last year's tax,
// AGI or withholding, or a state's tax for the year.
func OverridablePath(p string) bool {
	switch {
	case strings.HasPrefix(p, "projection.") && strings.Contains(p, "].so_far.items."):
		return true
	case strings.HasPrefix(p, "estimated["):
		for _, f := range []string{"].last_year_tax", "].last_year_agi", "].last_year_withheld", "].tax_for_year"} {
			if strings.HasSuffix(p, f) {
				return !(f == "].tax_for_year" && strings.HasPrefix(p, "estimated[US]"))
			}
		}
	}
	return false
}

// CheckScenarios runs the checks that compare a scenario with shared: no
// scenario changes or adds a known part, every override sits where the
// table puts it, and Your plan's changes hold only what they may.
func CheckScenarios(file *scenariosv1.TaxYearScenarios, resolved []Resolved) map[string][]*scenariosv1.Problem {
	checks := checkShared(file.GetShared())
	out := map[string][]*scenariosv1.Problem{}
	if len(checks.problems) > 0 {
		out[""] = checks.problems
	}
	for _, r := range resolved {
		if p := checks.scenario(r.Inputs()); len(p) > 0 {
			out[r.Scenario().GetId()] = p
		}
	}
	return out
}

// sharedChecks is what CheckScenarios reads from shared: its known parts
// and overrides, and the problems with shared alone.
type sharedChecks struct {
	known     map[string]proto.Message
	overrides map[string]bool
	problems  []*scenariosv1.Problem
}

func checkShared(shared *scenariosv1.ScenarioInputs) sharedChecks {
	c := sharedChecks{known: knownParts(shared), overrides: map[string]bool{}}
	for _, o := range shared.GetOverrides() {
		c.overrides[o.GetPath()] = true
		if strings.HasSuffix(o.GetPath(), "].tax_for_year") {
			c.problems = append(c.problems, problem("shared.overrides["+o.GetPath()+"]", "A state's tax for the year is overridden in a scenario's changes, not in shared."))
		}
	}
	return c
}

// scenario runs the checks on one scenario's resolved inputs, in path
// order.
func (c sharedChecks) scenario(in *scenariosv1.ScenarioInputs) []*scenariosv1.Problem {
	var out []*scenariosv1.Problem
	mine := knownParts(in)
	paths := make([]string, 0, len(mine))
	for path := range mine {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		// A part shared with shared, not copied, is equal to it.
		if s, ok := c.known[path]; !ok || (s != mine[path] && !proto.Equal(s, mine[path])) {
			out = append(out, problem(path, "A known part, which holds evidence, is in shared; no scenario adds or changes one."))
		}
	}
	for _, o := range in.GetOverrides() {
		if !c.overrides[o.GetPath()] && !strings.HasSuffix(o.GetPath(), "].tax_for_year") {
			out = append(out, problem("overrides["+o.GetPath()+"]", "A figure so far or last year's figure is overridden in shared, for every scenario."))
		}
	}
	return out
}

// knownParts maps each known part's path to the part.
func knownParts(in *scenariosv1.ScenarioInputs) map[string]proto.Message {
	out := map[string]proto.Message{}
	for _, s := range models(in.GetModel()) {
		k := partKinds[s.partName()]
		for i := 0; s.parts != nil && i < s.parts.Len(); i++ {
			part := s.parts.Get(i).Message()
			id, kind, _ := partCase(part)
			if has(k.known, kind) {
				out[fmt.Sprintf("%s.parts[%s]", s.path, id)] = part.Interface()
			}
		}
	}
	return out
}
