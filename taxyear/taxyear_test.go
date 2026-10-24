package taxyear_test

import (
	_ "embed"
	"fmt"
	"math"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
)

// example is docs/scenarios.md's JSONC example, verbatim.
//
//go:embed testdata/example.jsonc
var example []byte

func readExample(t *testing.T) *scenariosv1.TaxYearScenarios {
	t.Helper()
	f, err := taxyear.Parse(example)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// usd is dollars and cents written as a literal, rounded to the cent.
func usd(d float64) *commonv1.Money {
	return &commonv1.Money{Minor: int64(math.Round(d * 100)), Currency: "USD"}
}

func date(m, d int32) *commonv1.Date { return &commonv1.Date{Year: 2026, Month: m, Day: d} }

func compute(t *testing.T, f *scenariosv1.TaxYearScenarios) *scenariosv1.TaxYearResult {
	t.Helper()
	r, err := taxyear.Compute(f)
	if err != nil {
		t.Fatal(err)
	}
	return r.Proto()
}

func figure(t *testing.T, r taxyear.Result, scenario, path string) int64 {
	t.Helper()
	s, ok := r.Scenario(scenario)
	if !ok {
		t.Fatalf("no scenario %s", scenario)
	}
	p, err := taxyear.ParsePath(path)
	if err != nil {
		t.Fatal(err)
	}
	f, ok := s.Figure(p)
	if !ok {
		t.Fatalf("%s: no figure at %s", scenario, path)
	}
	a, ok := f.Amount()
	if !ok {
		t.Fatalf("%s: %s is not an amount", scenario, path)
	}
	return int64(a)
}

// TestExampleEndToEnd computes the doc's example. Acme pays $5,833.33 twice
// a month; the Aug 14 paystub and Aug 31 deposit are so far. The paystub has
// no year-to-date column, so the 14 pay dates before it copy it, as
// expected paychecks, and the year comes to $140,000. Sep 15 to Dec 31
// repeat the paystub in Your plan, and pay $6,250 from Oct 1 in Raise in
// Oct. The paystub prints no federal line, so the expected paychecks
// compute it by Publication 15-T for a single filer: $139,999.92 a year
// less the $16,100 standard deduction gives $22,333.98, $930.58 a paycheck.
// Box figures are so far's and the rest's, each in whole dollars, and items
// are shown in whole dollars too.
func TestExampleEndToEnd(t *testing.T) {
	r, err := taxyear.Compute(readExample(t))
	if err != nil {
		t.Fatal(err)
	}
	if p := r.Problems(); len(p) > 0 {
		t.Fatalf("problems: %v", p)
	}
	for _, c := range []struct {
		scenario, path string
		want           int64
	}{
		// The Aug 14 paystub and the Aug 31 deposit are two parts, each
		// $5,833.33 rounded on its own: so far adds $5,833 twice.
		{"plan", "projection.employers[acme].so_far.boxes.wages", 1_166_600},
		{"plan", "projection.employers[acme].rest.boxes.wages", 12_833_400},
		// Items show whole dollars: $930.58 withheld and $4,102.18 taken home.
		{"plan", "projection.employers[acme].items[employers/acme/2026-12-31/regular].paycheck.federal_withheld", 93_100},
		{"plan", "projection.employers[acme].items[employers/acme/2026-08-31/regular].take_home", 410_200},
		{"raise", "projection.employers[acme].rest.boxes.wages", 13_083_400},
		{"raise", "projection.employers[acme].items[employers/acme/2026-10-15/regular].paycheck.gross", 625_000},
		{"plan", "federal.form_1040.w2_wages", 14_000_000},
		{"plan", "federal.form_1040.taxable_income", 14_000_000 - 1_610_000},
		// $1,240 + $4,560 + $12,166, and 24% of $18,200 over $105,700.
		{"plan", "federal.form_1040.tax", 2_233_400},
	} {
		if got := figure(t, r, c.scenario, c.path); got != c.want {
			t.Errorf("%s %s = %d, want %d", c.scenario, c.path, got, c.want)
		}
	}
	plan, _ := r.Scenario("plan")
	raise, _ := r.Scenario("raise")
	if len(plan.Proto().GetEstimated()) == 0 || plan.Proto().GetEstimated()[0].GetJurisdiction() != "US" {
		t.Errorf("estimated %v, want US first", plan.Proto().GetEstimated())
	}
	if n := len(raise.Proto().GetProjection().GetEmployers()[0].GetItems()); n != 24 {
		t.Errorf("Raise in Oct has %d items, want one a pay date", n)
	}
}

func TestParseRefusesUnknownField(t *testing.T) {
	bad := strings.Replace(string(example), `"taxYear": 2026,`, `"taxYear": 2026, "taxOwed": 1,`, 1)
	if _, err := taxyear.Parse([]byte(bad)); err == nil {
		t.Error("an unknown field was accepted")
	}
}

func TestParseRefusesUnspecifiedEnum(t *testing.T) {
	bad := strings.Replace(string(example), `"PAY_FREQUENCY_SEMI_MONTHLY"`, `"PAY_FREQUENCY_UNSPECIFIED"`, 1)
	if _, err := taxyear.Parse([]byte(bad)); err == nil || !strings.Contains(err.Error(), "PAY_FREQUENCY_UNSPECIFIED") {
		t.Errorf("an UNSPECIFIED enum gave %v", err)
	}
}

func TestParseRefusesEmptyOneof(t *testing.T) {
	bad := strings.Replace(string(example), `"defaultPart": { "id": "acme-m9t3r7xe", "sameAsLatest": {} }`, `"defaultPart": { "id": "acme-m9t3r7xe" }`, 1)
	if bad == string(example) {
		t.Fatal("the example has no default part to empty")
	}
	if _, err := taxyear.Parse([]byte(bad)); err == nil || !strings.Contains(err.Error(), "rest") {
		t.Errorf("a oneof with no case gave %v", err)
	}
}

// fileProblems computes and returns the file's problems and every
// scenario's, as text.
func fileProblems(t *testing.T, f *scenariosv1.TaxYearScenarios) string {
	t.Helper()
	r := compute(t, f)
	var out []string
	for _, p := range r.GetProblems() {
		out = append(out, p.GetPath()+": "+p.GetMessage().GetText())
	}
	for _, s := range r.GetScenarios() {
		for _, p := range s.GetProblems() {
			out = append(out, s.GetScenario()+" "+p.GetPath()+": "+p.GetMessage().GetText())
		}
	}
	return strings.Join(out, "\n")
}

func acme(f *scenariosv1.TaxYearScenarios) *scenariosv1.EmployerModel {
	return f.GetShared().GetModel().GetEmployers()[0]
}

// TestValidate breaks the example one way at a time and checks each break
// is a problem naming its place: the checks that need only the file,
// and the models doc's "Validate" checks.
func TestValidate(t *testing.T) {
	for _, c := range []struct {
		name, want string
		breakIt    func(f *scenariosv1.TaxYearScenarios)
	}{
		{"schema version", "schema_version", func(f *scenariosv1.TaxYearScenarios) { f.SchemaVersion = 2 }},
		{"a tin", "tin", func(f *scenariosv1.TaxYearScenarios) {
			acme(f).GetParts()[0].GetFromPaystubs().GetStubs()[0].GetPaystub().Employer = &commonv1.Party{Name: "Acme", Tin: "12-3456789"}
		}},
		{"nine digits", "nine digits", func(f *scenariosv1.TaxYearScenarios) { acme(f).Name = "Acme 123-45-6789" }},
		{"plan first", "Your plan", func(f *scenariosv1.TaxYearScenarios) { f.Scenarios[0], f.Scenarios[1] = f.Scenarios[1], f.Scenarios[0] }},
		{"1 ids unique", "used again", func(f *scenariosv1.TaxYearScenarios) { acme(f).GetParts()[1].Id = acme(f).GetParts()[0].GetId() }},
		{"3 evidence ids unique", "used twice", func(f *scenariosv1.TaxYearScenarios) {
			acme(f).GetParts()[1].GetFromDeposits().GetDeposits()[0].Id = "acme-p7r2k9sd"
		}},
		{"3 one known part of each kind", "at most one from_deposits", func(f *scenariosv1.TaxYearScenarios) {
			p := proto.Clone(acme(f).GetParts()[1]).(*scenariosv1.EmployerPart)
			p.Id = "acme-zzzzzzzz"
			p.GetFromDeposits().GetDeposits()[0].Id = "acme-dzzzzzzz"
			acme(f).Parts = append(acme(f).Parts, p)
		}},
		{"3 known parts in shared", "is in shared", func(f *scenariosv1.TaxYearScenarios) {
			add := f.GetScenarios()[1].GetChanges().GetModel().GetEmployers()[0].GetPatch().GetParts()[0]
			add.GetAdd().Part = &scenariosv1.EmployerPart_Ended_{Ended: &scenariosv1.EmployerPart_Ended{LastPaycheck: date(8, 31)}}
		}},
		{"4 default part", "default part", func(f *scenariosv1.TaxYearScenarios) { acme(f).DefaultPart = nil }},
		{"5 period starts", "start on", func(f *scenariosv1.TaxYearScenarios) {
			for _, id := range []string{"acme-r1", "acme-r2"} {
				acme(f).Parts = append(acme(f).Parts, &scenariosv1.EmployerPart{Id: id,
					Part: &scenariosv1.EmployerPart_PayRate_{PayRate: &scenariosv1.EmployerPart_PayRate{From: date(10, 1), Paycheck: &factsv1.PayFigures{Gross: usd(1)}}}})
			}
		}},
		{"5 period before starts", "before the job", func(f *scenariosv1.TaxYearScenarios) {
			acme(f).Starts = date(9, 1)
			acme(f).Parts = append(acme(f).Parts, &scenariosv1.EmployerPart{Id: "acme-early",
				Part: &scenariosv1.EmployerPart_PayRate_{PayRate: &scenariosv1.EmployerPart_PayRate{From: date(8, 1), Paycheck: &factsv1.PayFigures{Gross: usd(1)}}}})
		}},
		{"6 one end", "one end", func(f *scenariosv1.TaxYearScenarios) {
			for _, id := range []string{"acme-e1", "acme-e2"} {
				acme(f).Parts = append(acme(f).Parts, &scenariosv1.EmployerPart{Id: id, Part: &scenariosv1.EmployerPart_Ends_{Ends: &scenariosv1.EmployerPart_Ends{LastPaycheck: date(12, 15)}}})
			}
		}},
		{"6 known end by as_of", "after as_of", func(f *scenariosv1.TaxYearScenarios) {
			acme(f).Parts = append(acme(f).Parts, &scenariosv1.EmployerPart{Id: "acme-e1", Part: &scenariosv1.EmployerPart_Ended_{Ended: &scenariosv1.EmployerPart_Ended{LastPaycheck: date(12, 15)}}})
		}},
		{"7 one year total", "one total", func(f *scenariosv1.TaxYearScenarios) {
			for _, id := range []string{"acme-y1", "acme-y2"} {
				acme(f).Parts = append(acme(f).Parts, &scenariosv1.EmployerPart{Id: id, Part: &scenariosv1.EmployerPart_YearTotal_{YearTotal: &scenariosv1.EmployerPart_YearTotal{
					Year: &factsv1.PayFigures{Gross: usd(1)}, TypedAgainst: date(8, 14)}}})
			}
		}},
		{"7 one plan part a person", "at most one plan", func(f *scenariosv1.TaxYearScenarios) {
			max := &scenariosv1.RetirementPart_MaxOutWorkplacePlan_{MaxOutWorkplacePlan: &scenariosv1.RetirementPart_MaxOutWorkplacePlan{}}
			f.GetShared().GetModel().Retirement = []*scenariosv1.RetirementModel{{Id: "you-r", Person: "you", Parts: []*scenariosv1.RetirementPart{
				{Id: "you-r1", Part: max}, {Id: "you-r2", Part: max}}}}
		}},
		{"8 matches a pay date", "not one of the job's pay dates", func(f *scenariosv1.TaxYearScenarios) {
			acme(f).GetParts()[1].GetFromDeposits().GetDeposits()[0].Matches = &scenariosv1.Match{To: &scenariosv1.Match_Slot{Slot: date(8, 30)}}
		}},
		{"8 actual names evidence", "names no evidence", func(f *scenariosv1.TaxYearScenarios) {
			acme(f).Parts = append(acme(f).Parts, &scenariosv1.EmployerPart{Id: "acme-b1", Part: &scenariosv1.EmployerPart_OneOff_{OneOff: &scenariosv1.EmployerPart_OneOff{
				Date: date(12, 1), Kind: factsv1.PaycheckKind_PAYCHECK_KIND_BONUS, Amount: usd(1), Actual: &scenariosv1.Actual{Is: "acme-nothing"}}}})
		}},
		{"8 override path", "can be overridden", func(f *scenariosv1.TaxYearScenarios) {
			f.GetShared().Overrides = []*scenariosv1.Override{{Path: "federal.form_1040.tax", Amount: usd(1)}}
		}},
		{"9 supplemental", "supplemental", func(f *scenariosv1.TaxYearScenarios) {
			acme(f).Supplemental = scenariosv1.SupplementalMethod_SUPPLEMENTAL_METHOD_UNSPECIFIED
		}},
		{"2 item ids unique", "share the id", func(f *scenariosv1.TaxYearScenarios) {
			planned := func(id string) *scenariosv1.TaxPaidPart {
				return &scenariosv1.TaxPaidPart{Id: id, Part: &scenariosv1.TaxPaidPart_PlannedPayment_{PlannedPayment: &scenariosv1.TaxPaidPart_PlannedPayment{Due: date(9, 15), Amount: usd(100)}}}
			}
			f.GetShared().GetModel().TaxPaid = []*scenariosv1.TaxPaidModel{{Id: "us", Jurisdiction: "US", Parts: []*scenariosv1.TaxPaidPart{planned("us-1"), planned("us-2")},
				DefaultPart: &scenariosv1.TaxPaidDefault{Id: "us-d", Rest: &scenariosv1.TaxPaidDefault_SpreadTheRest_{SpreadTheRest: &scenariosv1.TaxPaidDefault_SpreadTheRest{}}}}}
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := readExample(t)
			c.breakIt(f)
			got := fileProblems(t, f)
			if !strings.Contains(got, c.want) {
				t.Errorf("problems:\n%s\nwant one mentioning %q", got, c.want)
			}
		})
	}
	if got := fileProblems(t, readExample(t)); got != "" {
		t.Errorf("the example has problems:\n%s", got)
	}
}

// TestAGIWorkingTakesAwayAdjustments wants AGI's working to add total
// income and take away the adjustments.
func TestAGIWorkingTakesAwayAdjustments(t *testing.T) {
	r := compute(t, readExample(t))
	var signs []string
	for _, w := range r.GetScenarios()[0].GetWorking() {
		if w.GetPath() != "federal.form_1040.adjusted_gross_income" {
			continue
		}
		for _, term := range w.GetTerms() {
			signs = append(signs, fmt.Sprintf("%s %d", term.GetPath(), term.GetSign()))
		}
	}
	want := "federal.form_1040.total_income 1, federal.form_1040.adjustments_to_income -1"
	if got := strings.Join(signs, ", "); got != want {
		t.Errorf("AGI's terms are %q, want %q", got, want)
	}
}

// TestDependents checks each test a person is measured by, and that a
// qualifying relative waits on its two questions until they are answered.
func TestDependents(t *testing.T) {
	f := readExample(t)
	f.Shared.Facts.People = []*factsv1.Person{
		{Id: "you", Relationship: factsv1.Relationship_RELATIONSHIP_YOU},
		{Id: "kid", Name: "Sam", Relationship: factsv1.Relationship_RELATIONSHIP_CHILD, MonthsLivedWithYou: 12, DateOfBirth: &commonv1.Date{Year: 2016, Month: 3, Day: 1}},
		{Id: "baby", Relationship: factsv1.Relationship_RELATIONSHIP_CHILD, MonthsLivedWithYou: 12},
		{Id: "rosa", Name: "Rosa", Relationship: factsv1.Relationship_RELATIONSHIP_PARENT},
		{Id: "friend", Relationship: factsv1.Relationship_RELATIONSHIP_NOT_RELATED, MonthsLivedWithYou: 6},
		{Id: "aunt", Relationship: factsv1.Relationship_RELATIONSHIP_OTHER_RELATIVE},
	}
	answer := func(q, who string, r factsv1.Reply) *factsv1.Position {
		return &factsv1.Position{Id: q + "." + who, Question: q, Subjects: []string{who}, Answer: &factsv1.Position_Reply{Reply: r}}
	}
	f.Shared.Facts.Positions = []*factsv1.Position{
		answer("dependent.gross_income_under_limit", "aunt", factsv1.Reply_REPLY_YES),
		answer("dependent.more_than_half_support", "aunt", factsv1.Reply_REPLY_YES),
	}
	s := compute(t, f).GetScenarios()[0]
	var got []string
	for _, d := range s.GetDependents() {
		got = append(got, fmt.Sprintf("%s: %s (%d questions)", d.GetPerson(), d.GetText().GetText(), len(d.GetQuestions())))
	}
	want := []string{
		"kid: Counts as your qualifying child (0 questions)",
		"baby: Whether they count as your dependent goes by their date of birth (0 questions)",
		"rosa: Could count as your qualifying relative (2 questions)",
		"friend: Doesn’t count as your dependent: they didn’t live with you all year (0 questions)",
		"aunt: Counts as your qualifying relative (2 questions)",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("dependents:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	asked := map[string]string{}
	for _, q := range s.GetQuestions() {
		asked[q.GetId()] = q.GetQuestion().GetText()
	}
	if q := asked["dependent.gross_income_under_limit.rosa"]; q != "Was Rosa’s gross income under $5,300?" {
		t.Errorf("Rosa's income question is %q", q)
	}
	var rows []string
	for _, d := range s.GetFederal().GetForm_1040().GetDependents() {
		rows = append(rows, d.GetName()+" "+d.GetRelationship())
	}
	if strings.Join(rows, ", ") != "Sam child,  relative" {
		t.Errorf("the 1040's dependents are %q", rows)
	}
}

// exported_from names the build that exported the file, which may hold
// nine digits in a row, and is no identifier.
func TestExportedFromIsOpaque(t *testing.T) {
	f := readExample(t)
	f.ExportedFrom = "y2026.41/d3/123456789"
	if p := compute(t, f).GetProblems(); len(p) > 0 {
		t.Errorf("problems %v, want none for exported_from", p)
	}
}
