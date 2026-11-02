package federal

import (
	"fmt"
	"strings"

	"google.golang.org/protobuf/proto"

	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/internal/civil"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	"github.com/nexteggapp/taxcalcs/taxyear/model/retirement"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
	form1040 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1040"
	form1098 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1098"
	form1099b "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_b"
	form1099div "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_div"
	form1099g "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_g"
	form1099int "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_int"
	form1099misc "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_misc"
	form1099r "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_r"
	formssa1099 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_ssa_1099"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
	formw2g "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2g"
)

// Inputs is the rules engine's inputs for a scenario: the household and
// elections from the facts, and one document per model carrying the
// model's full-year figures from the projection. A form a model holds as
// a known part is copied and its projected boxes set over it; a 1099-B's
// lines are copied as they are. The notes say what the inputs leave out.
func Inputs(ctx *model.Context, proj *scenariosv1.YearProjection, in *scenariosv1.ScenarioInputs) (*returnsv1.ReturnInputs, *model.Notes) {
	facts := in.GetFacts()
	if facts == nil {
		facts = &factsv1.TaxYearFacts{}
	}
	status, _ := FilingStatus(facts)
	a := &adapter{ctx: ctx, proj: proj, in: in, facts: facts, status: status, notes: &model.Notes{},
		fed: &returnsv1.Federal1040Inputs{
			Documents:  &returnsv1.Documents{},
			Deductions: &returnsv1.Deductions{StateAndLocalTaxes: &returnsv1.StateLocalTaxes{}},
			Payments:   &returnsv1.Payments{},
			Elections:  &returnsv1.Elections{FilingStatus: electionOf(status)},
		}}
	a.household()
	a.elections()
	a.wages()
	a.accounts()
	a.brokers()
	a.businesses()
	a.retirement()
	a.otherIncome()
	a.itemised()
	a.payments()
	a.priorYear()
	a.stateRefunds()
	a.leftOut()
	return &returnsv1.ReturnInputs{SchemaVersion: 1, TaxYear: int32(ctx.Year()), Federal: a.fed}, a.notes
}

// adapter holds one scenario's inputs as they are built.
type adapter struct {
	ctx    *model.Context
	proj   *scenariosv1.YearProjection
	in     *scenariosv1.ScenarioInputs
	facts  *factsv1.TaxYearFacts
	status filing.Status
	fed    *returnsv1.Federal1040Inputs
	// notes holds what the inputs leave out.
	notes *model.Notes
	// refunds are last year's state refunds, in the year's 1099-Gs.
	refunds []refund
}

// refund is a state refund's 1099-G and its model's word on whether the
// year that deducted the tax itemised, nil when unsaid.
type refund struct {
	doc      *returnsv1.Form1099GDocument
	itemised *bool
}

func (r refund) amount() money.USCents { return model.Value(r.doc.GetForm().GetStateOrLocalRefund()) }

func electionOf(s filing.Status) returnsv1.FilingStatusElection {
	switch s {
	case filing.MarriedFilingJointly:
		return returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY
	case filing.MarriedFilingSeparately:
		return returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_SEPARATELY
	case filing.HeadOfHousehold:
		return returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_HEAD_OF_HOUSEHOLD
	case filing.SurvivingSpouse:
		return returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_QUALIFYING_SURVIVING_SPOUSE
	}
	return returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE
}

func maritalOf(s filing.Status) returnsv1.MaritalStatus {
	switch s {
	case filing.MarriedFilingJointly, filing.MarriedFilingSeparately:
		return returnsv1.MaritalStatus_MARITAL_STATUS_MARRIED
	case filing.SurvivingSpouse:
		return returnsv1.MaritalStatus_MARITAL_STATUS_WIDOWED
	}
	return returnsv1.MaritalStatus_MARITAL_STATUS_NEVER_MARRIED
}

// spouseID is the spouse's id on a joint return whose facts don't hold
// them yet.
const spouseID = "spouse"

// youID is the filer's id when the facts don't hold them.
const youID = "you"

func (a *adapter) married() bool {
	return a.status == filing.MarriedFilingJointly || a.status == filing.MarriedFilingSeparately
}

// household fills the filer, the spouse on a joint or separate return, and
// the people the facts say count as dependents.
func (a *adapter) household() {
	h := &returnsv1.Household{MaritalStatus: maritalOf(a.status), Residence: a.facts.GetResidence()}
	for _, p := range a.facts.GetPeople() {
		switch p.GetRelationship() {
		case factsv1.Relationship_RELATIONSHIP_YOU:
			h.Taxpayer = person(p)
		case factsv1.Relationship_RELATIONSHIP_SPOUSE:
			if a.married() {
				h.Spouse = person(p)
			}
		default:
			if d := a.dependent(p); d != nil {
				h.Dependents = append(h.Dependents, d)
			}
		}
	}
	if h.Taxpayer == nil {
		h.Taxpayer = &returnsv1.Person{Id: youID}
	}
	if a.status == filing.MarriedFilingJointly && h.Spouse == nil {
		h.Spouse = &returnsv1.Person{Id: spouseID}
	}
	a.fed.Household = h
}

func person(p *factsv1.Person) *returnsv1.Person {
	out := &returnsv1.Person{Id: p.GetId(), FirstName: p.GetName(), Blind: proto.Bool(p.GetBlind())}
	if p.GetDateOfBirth().GetYear() != 0 {
		out.DateOfBirth = p.GetDateOfBirth()
	}
	if p.GetPermanentlyDisabled() {
		out.PermanentlyDisabled = proto.Bool(true)
	}
	if p.GetFullTimeStudent() {
		out.FullTimeStudent = proto.Bool(true)
	}
	return out
}

var relationships = map[factsv1.Relationship]returnsv1.Relationship{
	factsv1.Relationship_RELATIONSHIP_CHILD:          returnsv1.Relationship_RELATIONSHIP_CHILD,
	factsv1.Relationship_RELATIONSHIP_PARENT:         returnsv1.Relationship_RELATIONSHIP_PARENT,
	factsv1.Relationship_RELATIONSHIP_SIBLING:        returnsv1.Relationship_RELATIONSHIP_SIBLING,
	factsv1.Relationship_RELATIONSHIP_OTHER_RELATIVE: returnsv1.Relationship_RELATIONSHIP_OTHER,
	factsv1.Relationship_RELATIONSHIP_NOT_RELATED:    returnsv1.Relationship_RELATIONSHIP_NOT_RELATED,
}

// dependent is a person other than the filer and spouse, with the replies
// the facts hold to the qualifying relative questions. A yes to "was their
// gross income under the limit" is given as no gross income.
func (a *adapter) dependent(p *factsv1.Person) *returnsv1.Dependent {
	d := &returnsv1.Dependent{Id: p.GetId(), FirstName: p.GetName(), Relationship: relationships[p.GetRelationship()],
		MonthsLivedWithYou: proto.Int32(p.GetMonthsLivedWithYou()), FullTimeStudent: proto.Bool(p.GetFullTimeStudent()),
		PermanentlyDisabled: proto.Bool(p.GetPermanentlyDisabled())}
	if p.GetDateOfBirth().GetYear() != 0 {
		d.DateOfBirth = p.GetDateOfBirth()
	}
	switch reply(a.facts, "dependent.gross_income_under_limit", p.GetId()) {
	case factsv1.Reply_REPLY_YES:
		d.GrossIncome = model.Money(0)
	case factsv1.Reply_REPLY_NO:
		return nil
	}
	switch reply(a.facts, "dependent.more_than_half_support", p.GetId()) {
	case factsv1.Reply_REPLY_YES:
		d.YouProvidedOverHalfSupport = proto.Bool(true)
	case factsv1.Reply_REPLY_NO:
		d.YouProvidedOverHalfSupport = proto.Bool(false)
	}
	return d
}

// reply is the facts' answer to a catalogue question about a person.
func reply(f *factsv1.TaxYearFacts, question, person string) factsv1.Reply {
	for _, pos := range f.GetPositions() {
		if pos.GetQuestion() != question {
			continue
		}
		for _, s := range pos.GetSubjects() {
			if s == person {
				return pos.GetReply()
			}
		}
	}
	return factsv1.Reply_REPLY_UNSPECIFIED
}

func (a *adapter) elections() {
	for _, e := range a.facts.GetElections() {
		switch e.GetDeductionMethod() {
		case factsv1.DeductionMethod_DEDUCTION_METHOD_STANDARD:
			a.fed.Elections.DeductionMethod = returnsv1.DeductionMethodElection_DEDUCTION_METHOD_ELECTION_STANDARD
		case factsv1.DeductionMethod_DEDUCTION_METHOD_ITEMISED:
			a.fed.Elections.DeductionMethod = returnsv1.DeductionMethodElection_DEDUCTION_METHOD_ELECTION_ITEMIZED
		}
	}
}

// personOf is the person a model names, or the filer.
func (a *adapter) personOf(id string) string {
	if id != "" {
		return id
	}
	return a.fed.GetHousehold().GetTaxpayer().GetId()
}

// wages adds a W-2 per job, with the job's projected year in its boxes.
// A job's known W-2 is the base, so its other boxes carry over.
func (a *adapter) wages() {
	byID := map[string]*scenariosv1.EmployerModel{}
	for _, m := range a.in.GetModel().GetEmployers() {
		byID[m.GetId()] = m
	}
	for _, e := range a.proj.GetEmployers() {
		m := byID[e.GetId()]
		w := &formw2.FormW2{}
		for _, p := range m.GetParts() {
			if f := p.GetFromW2(); f != nil && model.Usable(f.GetSource()) {
				w = clone(f.GetW2())
			}
		}
		if w.GetEmployer() == nil && m.GetName() != "" {
			w.Employer = &commonv1.Party{Name: m.GetName()}
		}
		b := e.GetYear().GetBoxes()
		w.WagesTipsOtherCompensation, w.FederalTaxWithheld = money2(b.GetWages()), money2(b.GetFederalWithheld())
		w.SocialSecurityWages, w.SocialSecurityTaxWithheld = money2(b.GetSocialSecurityWages()), money2(b.GetSocialSecurityWithheld())
		w.MedicareWagesAndTips, w.MedicareTaxWithheld = money2(b.GetMedicareWages()), money2(b.GetMedicareWithheld())
		w.CodedAmounts = codedAmounts(w.GetCodedAmounts(), map[string]*commonv1.Money{
			"D": b.GetK401(), "E": b.GetK403B(), "AA": b.GetRoth_401K(), "BB": b.GetRoth_403B(), "W": b.GetHsa()})
		deferred := model.Value(b.GetK401()) + model.Value(b.GetK403B()) + model.Value(b.GetRoth_401K()) + model.Value(b.GetRoth_403B())
		w.RetirementPlan = w.GetRetirementPlan() || m.GetCoveredByPlan() || deferred > 0
		w.StateTaxWithholding = nil
		for _, s := range b.GetStates() {
			w.StateTaxWithholding = append(w.StateTaxWithholding, &commonv1.StateTaxWithholding{State: s.GetState(),
				StateIncome: money2(s.GetWages()), TaxWithheld: money2(s.GetWithheld())})
		}
		a.fed.Documents.W2S = append(a.fed.Documents.W2S, &returnsv1.W2Document{Id: "employers/" + e.GetId(),
			Person: a.personOf(m.GetPerson()), Form: w})
	}
}

// codedAmounts is box 12 with the projected codes set: the known W-2's
// other codes are kept, and a projected code with no amount is left off.
func codedAmounts(known []*formw2.FormW2_CodedAmount, projected map[string]*commonv1.Money) []*formw2.FormW2_CodedAmount {
	var out []*formw2.FormW2_CodedAmount
	for _, c := range known {
		if _, ok := projected[c.GetCode()]; !ok {
			out = append(out, c)
		}
	}
	for _, code := range []string{"D", "E", "AA", "BB", "W"} {
		if v := projected[code]; model.Value(v) != 0 {
			out = append(out, &formw2.FormW2_CodedAmount{Code: code, Amount: money2(v)})
		}
	}
	return out
}

// money2 copies an amount, unset as zero dollars.
func money2(m *commonv1.Money) *commonv1.Money { return model.Money(model.Value(m)) }

// accounts adds a 1099-INT and a 1099-DIV per account, each where the
// account's year has figures of its kind.
func (a *adapter) accounts() {
	byID := map[string]*scenariosv1.AccountModel{}
	for _, m := range a.in.GetModel().GetAccounts() {
		byID[m.GetId()] = m
	}
	for _, acct := range a.proj.GetAccounts() {
		m := byID[acct.GetId()]
		var knownInt *form1099int.Form1099INT
		var knownDiv *form1099div.Form1099DIV
		for _, p := range m.GetParts() {
			for _, f := range p.GetFromForms().GetForms() {
				if !model.Usable(f.GetSource()) {
					continue
				}
				if x := f.GetForm_1099Int(); x != nil {
					knownInt = x
				}
				if x := f.GetForm_1099Div(); x != nil {
					knownDiv = x
				}
			}
		}
		b := acct.GetYear().GetBoxes()
		payer := &commonv1.Party{Name: m.GetName()}
		interest := model.Value(b.GetInterest()) + model.Value(b.GetTreasuryInterest()) + model.Value(b.GetTaxExemptInterest())
		if interest != 0 || knownInt != nil {
			f := &form1099int.Form1099INT{Payer: payer}
			if knownInt != nil {
				f = clone(knownInt)
			}
			f.InterestIncome, f.SavingsBondAndTreasuryInterest = money2(b.GetInterest()), money2(b.GetTreasuryInterest())
			f.TaxExemptInterest = money2(b.GetTaxExemptInterest())
			if b.GetForeignTaxPaid() != nil && model.Value(b.GetOrdinaryDividends()) == 0 {
				f.ForeignTaxPaid = money2(b.GetForeignTaxPaid())
			}
			a.fed.Documents.Interest = append(a.fed.Documents.Interest, &returnsv1.Form1099INTDocument{
				Id: "accounts/" + acct.GetId() + "/1099-int", Person: a.personOf(""), Form: f})
		}
		dividends := model.Value(b.GetOrdinaryDividends()) + model.Value(b.GetCapitalGainDistributions())
		if dividends != 0 || knownDiv != nil {
			f := &form1099div.Form1099DIV{Payer: payer}
			if knownDiv != nil {
				f = clone(knownDiv)
			}
			f.OrdinaryDividends, f.QualifiedDividends = money2(b.GetOrdinaryDividends()), money2(b.GetQualifiedDividends())
			f.TotalCapitalGainDistribution, f.Section_199ADividends = money2(b.GetCapitalGainDistributions()), money2(b.GetSection_199ADividends())
			f.ForeignTaxPaid = money2(b.GetForeignTaxPaid())
			a.fed.Documents.Dividends = append(a.fed.Documents.Dividends, &returnsv1.Form1099DIVDocument{
				Id: "accounts/" + acct.GetId() + "/1099-div", Person: a.personOf(""), Form: f})
		}
	}
}

// brokers adds a statement per broker: each sale lot from a 1099-B is that
// line, copied; each other sale lot, traded or planned, is a 1099-B line
// made from the lot, in the Form 8949 box the projection puts it in.
func (a *adapter) brokers() {
	byID := map[string]*scenariosv1.BrokerModel{}
	for _, m := range a.in.GetModel().GetBrokers() {
		byID[m.GetId()] = m
	}
	for _, b := range a.proj.GetBrokers() {
		m := byID[b.GetId()]
		lines := statementLines(m)
		st := &returnsv1.BrokerStatement{Id: "brokers/" + b.GetId(), Person: a.personOf("")}
		for _, it := range b.GetItems() {
			s := it.GetSale()
			if s == nil {
				continue
			}
			f := lines[it.GetId()]
			if f == nil {
				f = saleLine(m, it, s)
			} else {
				f = clone(f)
			}
			st.Transactions = append(st.Transactions, &returnsv1.BrokerTransaction{Id: it.GetId(), Form: f})
		}
		if len(st.Transactions) > 0 {
			a.fed.Documents.Brokers = append(a.fed.Documents.Brokers, st)
		}
	}
}

// statementLines is each 1099-B line by the id of the sale lot item the
// broker model makes from it.
func statementLines(m *scenariosv1.BrokerModel) map[string]*form1099b.Form1099B {
	out := map[string]*form1099b.Form1099B{}
	for _, p := range m.GetParts() {
		for _, st := range p.GetFromForms().GetStatements() {
			if !model.Usable(st.GetSource()) {
				continue
			}
			for i, t := range st.GetStatement().GetTransactions() {
				sold, _ := civil.FromProto(t.GetDateSoldOrDisposed())
				id := model.ItemID(model.ItemID("brokers", m.GetId()), sold.String(), "sale_lot", fmt.Sprintf("%s-%d", st.GetId(), i))
				out[id] = t
			}
		}
	}
	return out
}

var boxLetters = map[scenariosv1.Form8949Box]string{
	scenariosv1.Form8949Box_FORM_8949_BOX_A: "A", scenariosv1.Form8949Box_FORM_8949_BOX_B: "B", scenariosv1.Form8949Box_FORM_8949_BOX_C: "C",
	scenariosv1.Form8949Box_FORM_8949_BOX_D: "D", scenariosv1.Form8949Box_FORM_8949_BOX_E: "E", scenariosv1.Form8949Box_FORM_8949_BOX_F: "F",
}

// saleLine is a 1099-B line for a sale lot no 1099-B reports yet.
func saleLine(m *scenariosv1.BrokerModel, it *scenariosv1.Item, s *scenariosv1.SaleLot) *form1099b.Form1099B {
	term := form1099b.Form1099B_HOLDING_PERIOD_SHORT_TERM
	if s.GetTerm() == scenariosv1.GainTerm_GAIN_TERM_LONG {
		term = form1099b.Form1099B_HOLDING_PERIOD_LONG_TERM
	}
	box := s.GetBox()
	reported := box == scenariosv1.Form8949Box_FORM_8949_BOX_A || box == scenariosv1.Form8949Box_FORM_8949_BOX_D
	return &form1099b.Form1099B{Payer: &commonv1.Party{Name: m.GetName()}, Form_8949Checkbox: boxLetters[box],
		DescriptionOfProperty: s.GetHolding(), DateAcquired: s.GetAcquired(), DateSoldOrDisposed: it.GetDate(),
		Proceeds: money2(s.GetProceeds()), CostOrOtherBasis: money2(s.GetBasis()), WashSaleLossDisallowed: money2(s.GetWashSaleDisallowed()),
		HoldingPeriod: term, BasisReportedToIrs: reported, NoncoveredSecurity: !reported}
}

// businesses adds a Schedule C per business from its projected lines. The
// expenses are one total, as the projection keeps them.
func (a *adapter) businesses() {
	byID := map[string]*scenariosv1.BusinessModel{}
	for _, m := range a.in.GetModel().GetBusinesses() {
		byID[m.GetId()] = m
	}
	for _, b := range a.proj.GetBusinesses() {
		m := byID[b.GetId()]
		l := b.GetYear().GetBoxes()
		a.fed.Businesses = append(a.fed.Businesses, &returnsv1.Business{Id: "businesses/" + b.GetId(), Owner: a.personOf(m.GetPerson()),
			Name: m.GetName(), MateriallyParticipated: proto.Bool(true),
			CashAndUnreportedReceipts: money2(l.GetGrossReceipts()), ReturnsAndAllowances: money2(l.GetReturnsAndAllowances()),
			Expenses: &returnsv1.BusinessExpenses{OtherExpenses: []*returnsv1.OtherExpenseLine{
				{Description: "Expenses", Amount: money2(l.GetTotalExpenses())}}}})
	}
}

// retirement adds each person's IRA contributions, and their HSA with the
// coverage the facts give and the contributions made directly.
func (a *adapter) retirement() {
	people := map[string]string{}
	for _, m := range a.in.GetModel().GetRetirement() {
		people[m.GetId()] = a.personOf(m.GetPerson())
	}
	type totals struct{ traditional, roth, hsa money.USCents }
	by := map[string]*totals{}
	var order []string
	for _, r := range a.proj.GetRetirement() {
		id := people[r.GetId()]
		if by[id] == nil {
			by[id] = &totals{}
			order = append(order, id)
		}
		t := r.GetYear().GetItems()
		by[id].traditional += model.Value(t.GetTraditionalIra())
		by[id].roth += model.Value(t.GetRothIra())
		by[id].hsa += model.Value(t.GetHsa())
	}
	ret := &returnsv1.Retirement{}
	for _, id := range order {
		t := by[id]
		if t.traditional != 0 || t.roth != 0 {
			ret.Persons = append(ret.Persons, &returnsv1.PersonRetirement{Person: id,
				TraditionalIraContributions: model.Money(t.traditional), RothIraContributions: model.Money(t.roth)})
		}
	}
	if len(ret.Persons) > 0 {
		a.fed.Retirement = ret
	}
	health := &returnsv1.Health{}
	seen := map[string]bool{}
	add := func(id string, direct money.USCents) {
		if seen[id] {
			return
		}
		seen[id] = true
		p := a.ctx.Person(id)
		cov := coverage(p.GetHsaCoverage())
		if cov == returnsv1.HDHPCoverageType_HDHP_COVERAGE_TYPE_UNSPECIFIED && direct == 0 && a.payrollHsa(id) == 0 {
			return
		}
		h := &returnsv1.PersonHSA{Person: id, AnnualCoverage: cov, DirectContributions: model.Money(direct)}
		if m := p.GetHsaMonths(); m > 0 && m < 12 && cov != returnsv1.HDHPCoverageType_HDHP_COVERAGE_TYPE_NONE {
			// The facts give how many months, not which: the last ones, so a
			// yes to the last-month rule question, § 223(b)(8), holds.
			h.AnnualCoverage = returnsv1.HDHPCoverageType_HDHP_COVERAGE_TYPE_UNSPECIFIED
			if reply(a.facts, retirement.LastMonthRule, id) == factsv1.Reply_REPLY_YES {
				h.ElectedLastMonthRule = proto.Bool(true)
			}
			for i := int32(1); i <= 12; i++ {
				c := cov
				if i <= 12-m {
					c = returnsv1.HDHPCoverageType_HDHP_COVERAGE_TYPE_NONE
				}
				h.MonthlyCoverage = append(h.MonthlyCoverage, &returnsv1.HSAMonthlyCoverage{Month: i, Coverage: c})
			}
		}
		health.HsaAccounts = append(health.HsaAccounts, h)
	}
	for _, id := range order {
		add(id, by[id].hsa)
	}
	for _, m := range a.in.GetModel().GetEmployers() {
		add(a.personOf(m.GetPerson()), 0)
	}
	if len(health.HsaAccounts) > 0 {
		a.fed.Health = health
	}
}

// payrollHsa is a person's box 12 code W across their jobs.
func (a *adapter) payrollHsa(id string) money.USCents {
	var t money.USCents
	for _, w := range a.fed.GetDocuments().GetW2S() {
		if w.GetPerson() != id {
			continue
		}
		for _, c := range w.GetForm().GetCodedAmounts() {
			if c.GetCode() == "W" {
				t += model.Value(c.GetAmount())
			}
		}
	}
	return t
}

func coverage(c factsv1.HsaCoverage) returnsv1.HDHPCoverageType {
	switch c {
	case factsv1.HsaCoverage_HSA_COVERAGE_NONE:
		return returnsv1.HDHPCoverageType_HDHP_COVERAGE_TYPE_NONE
	case factsv1.HsaCoverage_HSA_COVERAGE_SELF_ONLY:
		return returnsv1.HDHPCoverageType_HDHP_COVERAGE_TYPE_SELF_ONLY
	case factsv1.HsaCoverage_HSA_COVERAGE_FAMILY:
		return returnsv1.HDHPCoverageType_HDHP_COVERAGE_TYPE_FAMILY
	}
	return returnsv1.HDHPCoverageType_HDHP_COVERAGE_TYPE_UNSPECIFIED
}

// otherIncome adds each other income model's year as the form its kind
// arrives on. A model's known form is the base.
func (a *adapter) otherIncome() {
	byID := map[string]*scenariosv1.OtherIncomeModel{}
	for _, m := range a.in.GetModel().GetOtherIncome() {
		byID[m.GetId()] = m
	}
	docs := a.fed.Documents
	for _, o := range a.proj.GetOtherIncome() {
		m := byID[o.GetId()]
		y := o.GetYear().GetBoxes()
		if y == nil {
			y = o.GetYear().GetItems()
		}
		gross, withheld := money2(y.GetGross()), money2(y.GetFederalWithheld())
		taxable := gross
		if y.GetTaxable() != nil {
			taxable = money2(y.GetTaxable())
		}
		id, person, payer := "other_income/"+o.GetId(), a.personOf(""), &commonv1.Party{Name: m.GetPayer()}
		known := knownOther(m)
		switch m.GetKind() {
		case scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_RETIREMENT_WITHDRAWALS:
			for i, f := range a.distributions(o, payer, known.GetForm_1099R()) {
				docs.RetirementDistributions = append(docs.RetirementDistributions, &returnsv1.Form1099RDocument{
					Id: fmt.Sprintf("%s/%d", id, i), Person: person, Form: f})
			}
		case scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_SOCIAL_SECURITY:
			f := &formssa1099.FormSSA1099{}
			if k := known.GetFormSsa_1099(); k != nil {
				f = clone(k)
			}
			f.NetBenefits, f.BenefitsPaid, f.FederalTaxWithheld = gross, gross, withheld
			docs.SocialSecurity = append(docs.SocialSecurity, &returnsv1.FormSSA1099Document{Id: id, Person: person, Form: f})
		case scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_UNEMPLOYMENT:
			f := &form1099g.Form1099G{Payer: payer}
			if k := known.GetForm_1099G(); k != nil {
				f = clone(k)
			}
			f.UnemploymentCompensation, f.FederalTaxWithheld = gross, withheld
			docs.GovernmentPayments = append(docs.GovernmentPayments, &returnsv1.Form1099GDocument{Id: id, Person: person, Form: f})
		case scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_STATE_REFUND:
			f := &form1099g.Form1099G{Payer: payer}
			if k := known.GetForm_1099G(); k != nil {
				f = clone(k)
			}
			f.StateOrLocalRefund, f.FederalTaxWithheld = gross, withheld
			if m.RefundYearItemised != nil && !m.GetRefundYearItemised() {
				// Its own model says the year that deducted the tax didn't
				// itemise, so none of it is income; its withholding counts.
				f.StateOrLocalRefund = model.Money(0)
			}
			doc := &returnsv1.Form1099GDocument{Id: id, Person: person, Form: f}
			docs.GovernmentPayments = append(docs.GovernmentPayments, doc)
			a.refunds = append(a.refunds, refund{doc: doc, itemised: m.RefundYearItemised})
		case scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_GAMBLING:
			f := &formw2g.FormW2G{Payer: payer}
			if k := known.GetFormW2G(); k != nil {
				f = clone(k)
			}
			f.ReportableWinnings, f.FederalTaxWithheld = gross, withheld
			docs.W2Gs = append(docs.W2Gs, &returnsv1.W2GDocument{Id: id, Person: person, Form: f})
		case scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_RENTAL:
			a.fed.Rentals = append(a.fed.Rentals, &returnsv1.RentalProperty{Id: id, Owner: person, Description: m.GetProperty(),
				RentsReceived: gross, OtherExpenses: money2(y.GetExpenses())})
			if model.Value(withheld) != 0 {
				a.fed.Payments.OtherFederalWithholding = model.Money(model.Value(a.fed.Payments.GetOtherFederalWithholding()) + model.Value(withheld))
			}
		case scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_ALIMONY:
			a.notes.LeaveOut("Alimony received: the rules engine has no input for it yet, so it is left out of Schedule 1 line 2a")
		case scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_HOME_SALE:
			a.notes.LeaveOut("The gain on a home sale over the § 121 exclusion: the projection doesn't hold the sale's dates and basis the rules need")
		default:
			f := &form1099misc.Form1099MISC{Payer: payer}
			if k := known.GetForm_1099Misc(); k != nil {
				f = clone(k)
			}
			f.OtherIncome, f.FederalTaxWithheld = taxable, withheld
			docs.Miscellaneous = append(docs.Miscellaneous, &returnsv1.Form1099MISCDocument{Id: id, Person: person, Form: f})
		}
	}
}

// distributions is a model's year as one 1099-R per box 7 code and IRA
// checkbox, as a payer issues them. A distribution without a code, or with
// a negative taxable amount, can't be taxed: it is left out, and only its
// withholding counts. The model's known 1099-R is the base.
func (a *adapter) distributions(o *scenariosv1.OtherIncomeProjection, payer *commonv1.Party, known *form1099r.Form1099R) []*form1099r.Form1099R {
	byKey := map[string]*form1099r.Form1099R{}
	var out []*form1099r.Form1099R
	var withheldOnly *form1099r.Form1099R
	for _, it := range o.GetItems() {
		x := it.GetOtherIncome()
		var codes []string
		for _, c := range x.GetDistributionCodes() {
			if c = strings.TrimSpace(c); c != "" {
				codes = append(codes, c)
			}
		}
		if len(codes) == 0 || x.GetTaxable() != nil && model.Value(x.GetTaxable()) < 0 {
			a.notes.LeaveOut("A Form 1099-R distribution without a box 7 code, or with a negative taxable amount, which can't be taxed; it is left out of lines 4 and 5")
			if w := model.Value(x.GetFederalWithheld()); w != 0 {
				if withheldOnly == nil {
					withheldOnly = &form1099r.Form1099R{Payer: payer, GrossDistribution: model.Money(0), TaxableAmount: model.Money(0), FederalTaxWithheld: model.Money(0)}
				}
				withheldOnly.FederalTaxWithheld = model.Money(model.Value(withheldOnly.GetFederalTaxWithheld()) + w)
			}
			continue
		}
		key := fmt.Sprint(codes, x.GetIra())
		f := byKey[key]
		if f == nil {
			f = &form1099r.Form1099R{Payer: payer}
			if known != nil {
				f = clone(known)
			}
			f.GrossDistribution, f.TaxableAmount, f.FederalTaxWithheld = model.Money(0), model.Money(0), model.Money(0)
			f.DistributionCodes, f.IraSepSimple = codes, x.GetIra()
			byKey[key] = f
			out = append(out, f)
		}
		gross := model.Value(x.GetGross())
		taxable := gross
		if x.GetTaxable() != nil {
			taxable = model.Value(x.GetTaxable())
		}
		f.GrossDistribution = model.Money(model.Value(f.GetGrossDistribution()) + gross)
		f.TaxableAmount = model.Money(model.Value(f.GetTaxableAmount()) + taxable)
		f.FederalTaxWithheld = model.Money(model.Value(f.GetFederalTaxWithheld()) + model.Value(x.GetFederalWithheld()))
	}
	if withheldOnly != nil {
		out = append(out, withheldOnly)
	}
	return out
}

// knownOther is the last usable form an other income model holds, or nil.
func knownOther(m *scenariosv1.OtherIncomeModel) *scenariosv1.OtherIncomePart_FromForms_Form {
	var out *scenariosv1.OtherIncomePart_FromForms_Form
	for _, p := range m.GetParts() {
		for _, f := range p.GetFromForms().GetForms() {
			if model.Usable(f.GetSource()) {
				out = f
			}
		}
	}
	return out
}

func (a *adapter) prior() *returnsv1.PriorYear {
	if a.fed.PriorYear == nil {
		a.fed.PriorYear = &returnsv1.PriorYear{}
	}
	return a.fed.PriorYear
}

// itemised adds Schedule A's inputs: medical expenses, real estate tax, a
// Form 1098 per mortgage and a cash gift per charity model.
func (a *adapter) itemised() {
	byID := map[string]*scenariosv1.ItemisedModel{}
	for _, m := range a.in.GetModel().GetItemised() {
		byID[m.GetId()] = m
	}
	d := a.fed.Deductions
	var medical, realEstate money.USCents
	for _, it := range a.proj.GetItemised() {
		m := byID[it.GetId()]
		b := it.GetYear().GetBoxes()
		medical += model.Value(b.GetMedical())
		realEstate += model.Value(b.GetRealEstateTaxes())
		if v := model.Value(b.GetMortgageInterest()); v != 0 || m.GetMortgage() != nil {
			f := &form1098.Form1098{Lender: &commonv1.Party{Name: m.GetName()}}
			for _, p := range m.GetMortgage().GetParts() {
				if k := p.GetFromForm_1098(); k != nil && model.Usable(k.GetSource()) {
					f = clone(k.GetForm())
				}
			}
			f.MortgageInterestReceived = model.Money(v)
			doc := &returnsv1.Form1098Document{Id: "itemised/" + it.GetId(), Person: a.personOf(""), Form: f,
				Use: returnsv1.MortgageUse_MORTGAGE_USE_HOME_ACQUISITION}
			if bal := m.GetMortgage().GetLoan().GetBalance(); bal != nil {
				doc.AverageBalance = money2(bal)
			}
			a.fed.Documents.MortgageInterest = append(a.fed.Documents.MortgageInterest, doc)
		}
		if v := model.Value(b.GetGiftsByCash()); v != 0 {
			d.CharitableGifts = append(d.CharitableGifts, &returnsv1.CharitableGift{DoneeName: m.GetName(),
				LimitCategory: returnsv1.CharitableLimitCategory_CHARITABLE_LIMIT_CATEGORY_CASH_PUBLIC_CHARITY_60_PCT,
				CashAmount:    model.Money(v), HasWrittenAcknowledgment: proto.Bool(true)})
		}
	}
	if medical != 0 {
		d.Medical = &returnsv1.MedicalExpenses{UnreimbursedMedicalAndDental: model.Money(medical)}
	}
	d.StateAndLocalTaxes.RealEstateTaxes = model.Money(realEstate)
}

// payments adds the year's tax payments made: federal estimated payments
// and last year's overpayment applied, and state and local payments dated
// in the year, which Schedule A line 5a deducts. A state payment toward an
// earlier year's balance is that line's balance for prior years.
func (a *adapter) payments() {
	p := a.fed.Payments
	var applied, balance money.USCents
	for _, t := range a.proj.GetTaxPaid() {
		us := t.GetJurisdiction() == "US"
		for _, it := range t.GetItems() {
			x := it.GetPayment()
			d, _ := civil.FromProto(it.GetDate())
			amount := model.Value(x.GetAmount())
			switch {
			case us && x.GetCounts() && x.GetFromLastYear():
				applied += amount
			case us && x.GetCounts() && it.GetStatus() == scenariosv1.ItemStatus_ITEM_STATUS_MADE:
				p.EstimatedPayments = append(p.EstimatedPayments, &returnsv1.EstimatedPayment{Jurisdiction: "US",
					DatePaid: it.GetDate(), DesignatedTaxYear: int32(a.ctx.Year()), Amount: model.Money(amount)})
			case us, d.Year() != a.ctx.Year(), x.GetPurpose().GetNotTax() != nil, x.GetFromLastYear():
			case x.GetPurpose().GetBalanceFor() != 0 && int(x.GetPurpose().GetBalanceFor()) < a.ctx.Year():
				balance += amount
			default:
				p.EstimatedPayments = append(p.EstimatedPayments, &returnsv1.EstimatedPayment{Jurisdiction: t.GetJurisdiction(),
					DatePaid: it.GetDate(), DesignatedTaxYear: int32(a.ctx.Year()), Amount: model.Money(amount)})
			}
		}
	}
	p.PriorYearFederalOverpaymentApplied = model.Money(applied)
	a.fed.Deductions.StateAndLocalTaxes.BalancePaidForPriorYears = model.Money(balance)
}

// priorYear adds last year's federal return and its carryovers: the
// facts' carryovers, else what last year's return says it carries forward.
func (a *adapter) priorYear() {
	py := a.prior()
	prior := a.priorFederal()
	if prior != nil {
		f := prior.GetFederal()
		if f != nil {
			py.PriorFederalReturn = f
		}
		agi, tax := prior.GetAdjustedGrossIncome(), prior.GetTotalTax()
		if agi == nil {
			agi = f.GetForm_1040().GetAdjustedGrossIncome()
		}
		if tax == nil {
			tax = f.GetForm_1040().GetTotalTax()
		}
		py.PriorYearAgi, py.PriorYearTotalTax = agi, tax
		if prior.FullYearResident != nil {
			py.PriorYearFullYearCitizenOrResident = proto.Bool(prior.GetFullYearResident())
		}
	}
	short := a.carryover(factsv1.CarryoverKind_CARRYOVER_KIND_SHORT_TERM_CAPITAL_LOSS)
	long := a.carryover(factsv1.CarryoverKind_CARRYOVER_KIND_LONG_TERM_CAPITAL_LOSS)
	if short != 0 || long != 0 {
		py.CapitalLossCarryover = &returnsv1.CapitalLossCarryover{ShortTermCarryover: model.Money(short), LongTermCarryover: model.Money(long)}
	}
	if c := a.carryover(factsv1.CarryoverKind_CARRYOVER_KIND_CHARITABLE_CONTRIBUTION); c != 0 {
		py.CharitableCarryovers = append(py.CharitableCarryovers, &returnsv1.CharitableCarryover{OriginTaxYear: int32(a.ctx.Year() - 1),
			Cash_60PctAmount: model.Money(c)})
	}
}

// stateRefunds gives the State and Local Income Tax Refund Worksheet what
// it reads of last year: a refund is income only as far as deducting the
// tax lowered last year's tax. With last year's Form 1040 and Schedule A
// in the facts, it gets their figures. Without them, a refund from a year
// that itemised counts in full and the result asks for them; with no word
// on itemising, the refunds whose own model doesn't say count in full and
// the result asks. Counting in full is given to the worksheet as a year
// whose state income tax all lowered its tax.
func (a *adapter) stateRefunds() {
	var undecided, itemised money.USCents
	for _, r := range a.refunds {
		switch {
		case r.itemised == nil:
			undecided += r.amount()
		case *r.itemised:
			itemised += r.amount()
		}
	}
	if undecided+itemised == 0 {
		return
	}
	prior := a.priorFederal()
	f, sa := prior.GetFederal().GetForm_1040(), prior.GetFederal().GetScheduleA()
	said := prior != nil && prior.Itemised != nil
	switch {
	case f != nil && f.GetItemized() && sa != nil:
		if s, ok := a.priorSchedule(f, sa); ok {
			a.prior().ItemizedSummary = s
			return
		}
	case f != nil && !f.GetItemized(), said && !prior.GetItemised():
		a.zeroUndecided()
		if itemised == 0 {
			a.prior().ItemizedSummary = &returnsv1.PriorYearItemizedSummary{ItemizedInPriorYear: proto.Bool(false)}
			return
		}
	case f == nil && !said && undecided > 0:
		a.notes.Ask(model.NewQuestion("federal/state-refund", "federal.schedule_1.taxable_state_local_refunds",
			"Did you itemise deductions on last year's federal return?", "Your state refund counts as income until you say.",
			"facts.prior_year"))
		if itemised == 0 {
			a.countInFull()
			return
		}
	}
	a.notes.LeaveOut(fmt.Sprintf("How much of a state refund is taxable: without your %d return, all of it is counted", a.ctx.Year()-1))
	a.notes.Ask(model.NewQuestion("federal/state-refund-schedule-a", "federal.schedule_1.taxable_state_local_refunds",
		"What were the figures on last year's Schedule A?", "All of your state refund counts as income until you give them.",
		"facts.prior_year"))
	a.countInFull()
}

// zeroUndecided takes the refunds whose model doesn't say out of income:
// last year didn't itemise.
func (a *adapter) zeroUndecided() {
	for _, r := range a.refunds {
		if r.itemised == nil {
			r.doc.Form.StateOrLocalRefund = model.Money(0)
		}
	}
}

// countInFull gives the worksheet a last year whose state income tax, at
// least the refunds, all lowered its tax.
func (a *adapter) countInFull() {
	var total money.USCents
	for _, r := range a.refunds {
		total += r.amount()
	}
	all := model.Money(total)
	a.prior().ItemizedSummary = &returnsv1.PriorYearItemizedSummary{ItemizedInPriorYear: proto.Bool(true),
		PriorScheduleALine_5AIncomeTax: all, PriorScheduleALine_5DTotalSalt: all, PriorScheduleALine_5ECappedSalt: all,
		PriorTotalItemizedDeductions: all, PriorStandardDeduction: model.Money(0)}
}

// priorSchedule is the worksheet's figures from last year's Form 1040 and
// Schedule A, with last year's standard deduction from the rules engine.
// It reports false when the engine has no rules for last year.
func (a *adapter) priorSchedule(f *form1040.Form1040, sa *form1040.ScheduleA) (*returnsv1.PriorYearItemizedSummary, bool) {
	std, ok := priorStandardDeduction(a.ctx, f)
	if !ok {
		return nil, false
	}
	total := sa.GetTotalItemizedDeductions()
	if total == nil {
		total = f.GetDeduction()
	}
	return &returnsv1.PriorYearItemizedSummary{ItemizedInPriorYear: proto.Bool(true),
		DeductedSalesTaxInsteadOfIncomeTax: proto.Bool(sa.GetElectedGeneralSalesTaxes()),
		PriorScheduleALine_5AIncomeTax:     sa.GetStateLocalIncomeOrSalesTaxes(),
		PriorScheduleALine_5DTotalSalt:     sa.GetTotalStateAndLocalTaxes(),
		PriorScheduleALine_5ECappedSalt:    model.Money(min(model.Value(sa.GetStateAndLocalTaxesAfterCap()), model.Value(sa.GetTotalStateAndLocalTaxes()))),
		PriorTotalItemizedDeductions:       total, PriorStandardDeduction: std, PriorTaxableIncome: f.GetTaxableIncome()}, true
}

// priorFederal is last year's federal return in the facts, the household's
// own before a separate one's, or nil.
func (a *adapter) priorFederal() *factsv1.PriorYearReturn {
	var out *factsv1.PriorYearReturn
	for _, p := range a.facts.GetPriorYear() {
		if p.GetJurisdiction() == "US" && (out == nil || len(p.GetPeople()) == 0 && len(out.GetPeople()) > 0) {
			out = p
		}
	}
	return out
}

// carryover is what last year carried into this one of a kind.
func (a *adapter) carryover(kind factsv1.CarryoverKind) money.USCents {
	var t money.USCents
	found := false
	for _, co := range a.facts.GetCarryovers() {
		if co.GetKind() == kind {
			t, found = t+model.Value(co.GetAmount()), true
		}
	}
	if found {
		return t
	}
	for _, p := range a.facts.GetPriorYear() {
		if p.GetJurisdiction() != "US" || len(p.GetPeople()) > 0 {
			continue
		}
		for _, cf := range p.GetCarriesForward() {
			if cf.GetKind() == kind {
				t += model.Value(cf.GetAmount())
			}
		}
	}
	return t
}

// leftOut notes what the engine could work out but the models hold no
// figures for.
func (a *adapter) leftOut() {
	a.notes.LeaveOut("Education credits, and other credits that need forms the models don't hold yet, such as Form 1098-T")
	if y := a.ctx.Year(); y >= 2025 && y <= 2028 {
		a.notes.LeaveOut("The deductions for tips, overtime and car loan interest, whose figures the models don't hold yet")
	}
}

// FilingStatus returns the household's elected filing status, or single
// and true when none is elected.
func FilingStatus(facts *factsv1.TaxYearFacts) (filing.Status, bool) {
	for _, e := range facts.GetElections() {
		if s := e.GetFilingStatus(); s != form1040.FilingStatus_FILING_STATUS_UNSPECIFIED {
			return statusOf(s), false
		}
	}
	return filing.Single, true
}

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

// clone copies a message, for a caller that must not share it.
func clone[T proto.Message](m T) T { return proto.Clone(m).(T) }
