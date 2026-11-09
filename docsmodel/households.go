package main

import (
	"fmt"
	"strings"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	"github.com/nexteggapp/taxcalcs/taxlisp/eval"
	"github.com/nexteggapp/taxcalcs/taxlisp/parser"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	form1099div "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_div"
	form1099r "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_r"
	formssa1099 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_ssa_1099"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
)

// The forms the sample households are followed through.
const (
	form540  = "ca-form-540"
	form1040 = "fed-form-1040"
)

// Household is a sample household and how one of its forms was derived.
type Household struct {
	ID string `json:"id"`
	// Form is the form rule the household is followed through.
	Form        string   `json:"form"`
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
	// form is the form the household is followed through.
	form            string
	status          returnsv1.FilingStatusElection
	wages, withheld int64
	// fedWithheld is the federal tax withheld on the W-2; a California
	// sample has a tenth of its wages withheld.
	fedWithheld       int64
	rented, claimable bool
	children          int
	coveredAllYear    bool
	// For the federal samples: the filer's and spouse's years of birth, if
	// not the usual ones; social security benefits for each of them; a
	// pension and the tax withheld from it; and dividends.
	born, spouseBorn         int32
	benefits, spouseBenefits int64
	pension, pensionWithheld int64
	dividends, qualified     int64
}

// The California samples come first, and the first is the one the pages
// that explain taxcalcs show.
var samples = []sample{
	{id: "single-renter-40k", form: form540, label: "Single renter, $40,000",
		description: "A single filer who rents, earning $40,000 in wages, with California tax withheld.",
		status:      returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		wages:       40_000_00, withheld: 600_00, rented: true, coveredAllYear: true},
	{id: "joint-two-children-130k", form: form540, label: "Married couple, two children, $130,000",
		description: "A married couple filing jointly with two children, earning $130,000. They rent, but their income is over the renter's credit limit.",
		status:      returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY,
		wages:       130_000_00, withheld: 4_000_00, rented: true, children: 2, coveredAllYear: true},
	{id: "head-of-household-60k", form: form540, label: "Head of household, one child, $60,000",
		description: "A single parent filing as head of household with one child, earning $60,000.",
		status:      returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_HEAD_OF_HOUSEHOLD,
		wages:       60_000_00, withheld: 1_500_00, children: 1, coveredAllYear: true},
	{id: "single-high-income-300k", form: form540, label: "Single, $300,000",
		description: "A single filer earning $300,000, over the AGI threshold, so the exemption credit is reduced.",
		status:      returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		wages:       300_000_00, withheld: 20_000_00, coveredAllYear: true},
	{id: "student-dependent-18k", form: form540, label: "Student someone else can claim, $18,000",
		description: "A student whose parents can claim them, earning $18,000, who rents and had no health coverage for the year.",
		status:      returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		wages:       18_000_00, withheld: 0, rented: true, claimable: true, coveredAllYear: false},

	{id: "us-single-40k", form: form1040, label: "Single, $40,000",
		description: "A single filer earning $40,000 in wages, who takes the standard deduction.",
		status:      returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		wages:       40_000_00, fedWithheld: 3_500_00},
	{id: "us-joint-two-children-130k", form: form1040, label: "Married couple, two children, $130,000",
		description: "A married couple filing jointly with two children under 17, earning $130,000, who take the child tax credit.",
		status:      returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY,
		wages:       130_000_00, fedWithheld: 9_000_00, children: 2},
	{id: "us-head-of-household-25k", form: form1040, label: "Head of household, one child, $25,000",
		description: "A single parent filing as head of household with one child, earning $25,000. Most of the child tax credit is refundable, and the household takes the earned income credit.",
		status:      returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_HEAD_OF_HOUSEHOLD,
		wages:       25_000_00, fedWithheld: 600_00, children: 1},
	{id: "us-retired-couple", form: form1040, label: "Retired couple, social security and a pension",
		description: "A married couple, both over 65, filing jointly, with $36,000 of social security benefits and a $30,000 pension. Part of their benefits is taxable, and their age adds to the standard deduction.",
		status:      returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY,
		born:        1955, spouseBorn: 1957, benefits: 20_000_00, spouseBenefits: 16_000_00,
		pension: 30_000_00, pensionWithheld: 3_000_00},
	{id: "us-single-dividends-170k", form: form1040, label: "Single, $150,000 and $20,000 of dividends",
		description: "A single filer earning $150,000, with $20,000 of dividends, $18,000 of them qualified. The qualified dividends are taxed at 15% on the Qualified Dividends and Capital Gain Tax Worksheet.",
		status:      returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		wages:       150_000_00, fedWithheld: 24_000_00, dividends: 20_000_00, qualified: 18_000_00},
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
	if s.born != 0 {
		born = s.born
	}
	spouseBorn := int32(1984)
	if s.spouseBorn != 0 {
		spouseBorn = s.spouseBorn
	}
	hh := &returnsv1.Household{
		MaritalStatus: returnsv1.MaritalStatus_MARITAL_STATUS_NEVER_MARRIED,
		Taxpayer:      person("ana", born),
	}
	hh.Taxpayer.CanBeClaimedAsDependent = boolPtr(s.claimable)
	if s.status == returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY {
		hh.MaritalStatus = returnsv1.MaritalStatus_MARITAL_STATUS_MARRIED
		hh.Spouse = person("ben", spouseBorn)
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
	if s.form == form1040 {
		return &returnsv1.ReturnInputs{
			SchemaVersion: 1,
			TaxYear:       2025,
			Federal: &returnsv1.Federal1040Inputs{
				Household: hh,
				Elections: &returnsv1.Elections{FilingStatus: s.status},
				Documents: s.federalDocuments(),
			},
		}
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

// federalDocuments are a federal sample's information returns: a W-2 for
// its wages, a Form SSA-1099 for each person's benefits, a Form 1099-R for
// its pension and a Form 1099-DIV for its dividends.
func (s sample) federalDocuments() *returnsv1.Documents {
	docs := &returnsv1.Documents{}
	if s.wages > 0 {
		docs.W2S = append(docs.W2S, &returnsv1.W2Document{Id: "w2", Person: "ana", Form: &formw2.FormW2{
			WagesTipsOtherCompensation: cents(s.wages),
			FederalTaxWithheld:         cents(s.fedWithheld),
			SocialSecurityWages:        cents(min(s.wages, 176_100_00)),
			SocialSecurityTaxWithheld:  cents(min(s.wages, 176_100_00) * 62 / 1000),
			MedicareWagesAndTips:       cents(s.wages),
			MedicareTaxWithheld:        cents(s.wages * 145 / 10000),
		}})
	}
	for _, b := range []struct {
		person string
		amount int64
	}{{"ana", s.benefits}, {"ben", s.spouseBenefits}} {
		if b.amount > 0 {
			docs.SocialSecurity = append(docs.SocialSecurity, &returnsv1.FormSSA1099Document{
				Id: "ssa-" + b.person, Person: b.person,
				Form: &formssa1099.FormSSA1099{NetBenefits: cents(b.amount)},
			})
		}
	}
	if s.pension > 0 {
		docs.RetirementDistributions = append(docs.RetirementDistributions, &returnsv1.Form1099RDocument{
			Id: "pension", Person: "ana",
			Form: &form1099r.Form1099R{
				GrossDistribution:  cents(s.pension),
				TaxableAmount:      cents(s.pension),
				FederalTaxWithheld: cents(s.pensionWithheld),
				DistributionCodes:  []string{"7"},
			},
		})
	}
	if s.dividends > 0 {
		docs.Dividends = append(docs.Dividends, &returnsv1.Form1099DIVDocument{
			Id: "dividends", Person: "ana",
			Form: &form1099div.Form1099DIV{
				OrdinaryDividends:  cents(s.dividends),
				QualifiedDividends: cents(s.qualified),
			},
		})
	}
	return docs
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
	if s.form == form1040 {
		facts := []Fact{{"Filing status", status}}
		add := func(label string, c int64) {
			if c > 0 {
				facts = append(facts, Fact{label, dollars(c)})
			}
		}
		add("Wages (W-2 box 1)", s.wages)
		add("Federal tax withheld from wages", s.fedWithheld)
		add("Social security benefits", s.benefits+s.spouseBenefits)
		add("Pension (Form 1099-R)", s.pension)
		add("Federal tax withheld from the pension", s.pensionWithheld)
		add("Dividends (Form 1099-DIV box 1a)", s.dividends)
		add("Qualified dividends (box 1b)", s.qualified)
		facts = append(facts, Fact{"Children claimed", fmt.Sprint(s.children)})
		if s.born != 0 {
			facts = append(facts, Fact{"Born", fmt.Sprintf("%d and %d", s.born, s.spouseBorn)})
		}
		return facts
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
		if s.form == "" {
			return fmt.Errorf("%s: no form", s.id)
		}
		tr := eval.NewTrace(func(name string) bool { return include[name] })
		eng.Evaluator.Trace = tr
		in := s.inputs()
		out, err := eng.Prepare(in)
		eng.Evaluator.Trace = nil
		if err != nil {
			return fmt.Errorf("%s: %w", s.id, err)
		}
		h := &Household{ID: s.id, Form: s.form, Label: s.label, Description: s.description, Facts: s.facts()}
		var sum *returnsv1.Summary
		var diags []*returnsv1.Diagnostic
		if s.form == form540 {
			var ca *returnsv1.CaliforniaReturn
			for _, st := range out.GetStates() {
				if st.GetCa() != nil {
					ca = st.GetCa()
				}
			}
			if ca == nil {
				return fmt.Errorf("%s: no California return", s.id)
			}
			sum, diags = ca.GetSummary(), ca.GetDiagnostics()
			runs = append(runs, run{sample: s, in: in, out: out})
		} else {
			sum, diags = out.GetFederal().GetSummary(), out.GetFederal().GetDiagnostics()
		}
		h.TotalTax = dollars(sum.GetTotalTax().GetMinor())
		h.Refund = dollars(sum.GetRefund().GetMinor())
		h.Owed = dollars(sum.GetAmountOwed().GetMinor())
		for _, d := range diags {
			h.Notes = append(h.Notes, d.GetMessage())
		}
		form := find(tr.Root, s.form)
		if form == nil {
			return fmt.Errorf("%s: no derivation of %s", s.id, s.form)
		}
		h.Derivation = steps(form)
		m.Households = append(m.Households, h)
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
