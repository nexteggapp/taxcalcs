package main

import (
	"fmt"
	"strings"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	"github.com/nexteggapp/taxcalcs/taxlisp/eval"
	"github.com/nexteggapp/taxcalcs/taxlisp/parser"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
)

// Household is a sample household and how its Form 540 was derived.
type Household struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	Description string   `json:"description"`
	Facts       []Fact   `json:"facts"`
	TotalTax    string   `json:"totalTax"`
	Refund      string   `json:"refund"`
	Owed        string   `json:"owed"`
	Derivation  *Step    `json:"derivation"`
	Notes       []string `json:"notes,omitempty"`
}

// Step is a node of a derivation, as the site shows it: a check carries its
// condition as an expression, to be written out like a formula.
type Step struct {
	*eval.Node
	Expr     *Expr   `json:"expr,omitempty"`
	Children []*Step `json:"children,omitempty"`
}

func steps(n *eval.Node) *Step {
	s := &Step{Node: n}
	if n.Kind == "check" {
		if e, err := parser.ParseExpr(n.Caption); err == nil {
			s.Expr = toExpr(e)
		}
	}
	for _, c := range n.Children {
		s.Children = append(s.Children, steps(c))
	}
	return s
}

// Fact is one of the household's inputs, as a reader would put it.
type Fact struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type sample struct {
	id, label, description string
	status                 returnsv1.FilingStatusElection
	wages, withheld        int64
	rented, claimable      bool
	children               int
	coveredAllYear         bool
}

var samples = []sample{
	{id: "single-renter-40k", label: "Single renter, $40,000",
		description: "A single filer who rents, earning $40,000 in wages, with California tax withheld.",
		status:      returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		wages:       40_000_00, withheld: 600_00, rented: true, coveredAllYear: true},
	{id: "joint-two-children-130k", label: "Married couple, two children, $130,000",
		description: "A married couple filing jointly with two children, earning $130,000. They rent, but their income is over the renter's credit limit.",
		status:      returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY,
		wages:       130_000_00, withheld: 4_000_00, rented: true, children: 2, coveredAllYear: true},
	{id: "head-of-household-60k", label: "Head of household, one child, $60,000",
		description: "A single parent filing as head of household with one child, earning $60,000.",
		status:      returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_HEAD_OF_HOUSEHOLD,
		wages:       60_000_00, withheld: 1_500_00, children: 1, coveredAllYear: true},
	{id: "single-high-income-300k", label: "Single, $300,000",
		description: "A single filer earning $300,000, over the AGI threshold, so the exemption credit is reduced.",
		status:      returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		wages:       300_000_00, withheld: 20_000_00, coveredAllYear: true},
	{id: "student-dependent-18k", label: "Student someone else can claim, $18,000",
		description: "A student whose parents can claim them, earning $18,000, who rents and had no health coverage for the year.",
		status:      returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		wages:       18_000_00, withheld: 0, rented: true, claimable: true, coveredAllYear: false},
}

func cents(c int64) *commonv1.Money { return &commonv1.Money{Currency: "USD", Minor: c} }

func boolPtr(b bool) *bool    { return &b }
func int32Ptr(n int32) *int32 { return &n }

func (s sample) inputs() *returnsv1.ReturnInputs {
	person := func(id string, born int32) *returnsv1.Person {
		return &returnsv1.Person{
			Id:          id,
			FirstName:   id,
			DateOfBirth: &commonv1.Date{Year: born, Month: 6, Day: 1},
			Tin: &returnsv1.TaxpayerID{
				Kind:               returnsv1.TaxpayerIDKind_TAXPAYER_ID_KIND_SSN,
				ValidForEmployment: boolPtr(true),
			},
		}
	}
	born := int32(1985)
	if s.claimable {
		born = 2005
	}
	hh := &returnsv1.Household{
		MaritalStatus: returnsv1.MaritalStatus_MARITAL_STATUS_NEVER_MARRIED,
		Taxpayer:      person("ana", born),
	}
	hh.Taxpayer.CanBeClaimedAsDependent = boolPtr(s.claimable)
	if s.status == returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY {
		hh.MaritalStatus = returnsv1.MaritalStatus_MARITAL_STATUS_MARRIED
		hh.Spouse = person("ben", 1984)
	}
	for i := 0; i < s.children; i++ {
		hh.Dependents = append(hh.Dependents, &returnsv1.Dependent{
			Id:        fmt.Sprintf("child%d", i+1),
			FirstName: fmt.Sprintf("Child %d", i+1),
			Tin: &returnsv1.TaxpayerID{
				Kind:               returnsv1.TaxpayerIDKind_TAXPAYER_ID_KIND_SSN,
				ValidForEmployment: boolPtr(true),
			},
			DateOfBirth:                     &commonv1.Date{Year: int32(2014 + 3*i), Month: 3, Day: 8},
			CitizenOrResidentOfNorthAmerica: boolPtr(true),
			Relationship:                    returnsv1.Relationship_RELATIONSHIP_CHILD,
			MonthsLivedWithYou:              int32Ptr(12),
			ProvidedOverHalfOwnSupport:      boolPtr(false),
			YouProvidedOverHalfSupport:      boolPtr(true),
			FullTimeStudent:                 boolPtr(false),
			PermanentlyDisabled:             boolPtr(false),
			FilesJointReturn:                boolPtr(false),
			GrossIncome:                     cents(0),
		})
	}
	w2 := &formw2.FormW2{
		WagesTipsOtherCompensation: cents(s.wages),
		FederalTaxWithheld:         cents(s.wages / 10),
		StateTaxWithholding: []*commonv1.StateTaxWithholding{
			{State: "CA", StateIncome: cents(s.wages), TaxWithheld: cents(s.withheld)},
		},
	}
	return &returnsv1.ReturnInputs{
		SchemaVersion: 1,
		TaxYear:       2025,
		Federal: &returnsv1.Federal1040Inputs{
			Household: hh,
			Elections: &returnsv1.Elections{FilingStatus: s.status},
			Documents: &returnsv1.Documents{
				W2S: []*returnsv1.W2Document{{Id: "w2", Person: "ana", Form: w2}},
			},
		},
		States: []*returnsv1.StateInputs{
			{State: "CA", Inputs: &returnsv1.StateInputs_Ca{Ca: &returnsv1.CaliforniaInputs{
				DeductionMethod:           returnsv1.DeductionMethodElection_DEDUCTION_METHOD_ELECTION_STANDARD,
				FullYearHealthCoverage:    boolPtr(s.coveredAllYear),
				QualifiedForRentersCredit: boolPtr(s.rented),
			}}},
		},
	}
}

func (s sample) facts() []Fact {
	status := map[returnsv1.FilingStatusElection]string{
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE:                 "Single",
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY: "Married filing jointly",
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_HEAD_OF_HOUSEHOLD:      "Head of household",
	}[s.status]
	yes := func(b bool) string {
		if b {
			return "Yes"
		}
		return "No"
	}
	return []Fact{
		{"Filing status", status},
		{"Wages (W-2 box 1)", dollars(s.wages)},
		{"California tax withheld", dollars(s.withheld)},
		{"Children claimed", fmt.Sprint(s.children)},
		{"Rented a California home for half the year or more", yes(s.rented)},
		{"Someone else can claim the filer", yes(s.claimable)},
		{"Health coverage all year", yes(s.coveredAllYear)},
	}
}

func dollars(c int64) string {
	d := fmt.Sprint(c / 100)
	var b strings.Builder
	for i, r := range d {
		if i > 0 && (len(d)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return fmt.Sprintf("$%s.%02d", b.String(), c%100)
}

func runHouseholds(m *Model) error {
	eng, err := federal.LoadEngine()
	if err != nil {
		return err
	}
	include := map[string]bool{}
	for _, r := range m.Rules {
		include[r.Name] = true
	}
	var runs []run
	for _, s := range samples {
		tr := eval.NewTrace(func(name string) bool { return include[name] })
		eng.Evaluator.Trace = tr
		in := s.inputs()
		out, err := eng.Prepare(in)
		eng.Evaluator.Trace = nil
		if err != nil {
			return fmt.Errorf("%s: %w", s.id, err)
		}
		var ca *returnsv1.CaliforniaReturn
		for _, st := range out.GetStates() {
			if st.GetCa() != nil {
				ca = st.GetCa()
			}
		}
		if ca == nil {
			return fmt.Errorf("%s: no California return", s.id)
		}
		h := &Household{ID: s.id, Label: s.label, Description: s.description, Facts: s.facts()}
		sum := ca.GetSummary()
		h.TotalTax = dollars(sum.GetTotalTax().GetMinor())
		h.Refund = dollars(sum.GetRefund().GetMinor())
		h.Owed = dollars(sum.GetAmountOwed().GetMinor())
		for _, d := range ca.GetDiagnostics() {
			h.Notes = append(h.Notes, d.GetMessage())
		}
		form := find(tr.Root, "ca-form-540")
		if form == nil {
			return fmt.Errorf("%s: no derivation of ca-form-540", s.id)
		}
		h.Derivation = steps(form)
		m.Households = append(m.Households, h)
		runs = append(runs, run{sample: s, in: in, out: out})
	}
	return examples(m, runs, eng)
}

func find(n *eval.Node, name string) *eval.Node {
	if n.Kind == "call" && n.Name == name {
		return n
	}
	for _, c := range n.Children {
		if f := find(c, name); f != nil {
			return f
		}
	}
	return nil
}
