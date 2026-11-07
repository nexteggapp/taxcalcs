package golden

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear"
	"github.com/nexteggapp/taxcalcs/taxyear/federal"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	"github.com/nexteggapp/taxcalcs/taxyear/model/taxpaid"
	"github.com/nexteggapp/taxcalcs/taxyear/paid"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
	form540 "github.com/nexteggapp/taxprotos/go/us/state/ca/v1/form_540"
	formor40 "github.com/nexteggapp/taxprotos/go/us/state/or/v1/form_or40"
)

// The paid vectors: for every scenario of every taxyear test case that
// reaches the estimated-payment rules, what taxyear's answerPayments gives
// paid.Compute and what it returns. The inputs are the context's year,
// as_of and facts, the projection as the return layer left it, with its
// tax paid projections straight after Expand, the return's filing status,
// Form 2210 line 2 tax, AGI and excess Social Security tax, the tax paid
// models expanded (the scenario's, and one for each adjacent year's
// payment to a jurisdiction without one), and the scenario's overrides.
// The outputs are each jurisdiction's Estimated, the questions, the
// federal payments made, and each tax paid model's projection once the
// default part is filled, or the error's text.
//
// The variants are one 2026 case's inputs with the facts, projection or
// overrides changed, so every jurisdiction's rules, the waiver and
// withholding elections, part-year residence, the overrides, filed state
// returns and the refusals are asked too.

// paidVector is one scenario's paid.Compute.
type paidVector struct {
	Case        string   `json:"case"`
	Year        int      `json:"year"`
	AsOf        string   `json:"as_of"`
	Facts       string   `json:"facts"`
	Projection  string   `json:"projection"`
	Status      int      `json:"status"`
	Tax2210     int64    `json:"tax_2210"`
	AGI         int64    `json:"agi"`
	ExcessSS    int64    `json:"excess_social_security"`
	TaxPaid     []string `json:"tax_paid"`
	Overrides   []string `json:"overrides"`
	Error       string   `json:"error"`
	Estimated   []string `json:"estimated"`
	Questions   []string `json:"questions"`
	LeftOut     []string `json:"left_out"`
	PaidUS      int64    `json:"federal_payments"`
	Projections []string `json:"tax_paid_projections"`
}

// paidCase is what answerPayments gives paid.Compute, before the tax paid
// models are expanded.
type paidCase struct {
	name      string
	year      int
	asOf      *commonv1.Date
	facts     *factsv1.TaxYearFacts
	proj      *scenariosv1.YearProjection
	status    filing.Status
	tax       money.USCents
	agi       money.USCents
	excessSS  money.USCents
	models    []*scenariosv1.TaxPaidModel
	overrides []*scenariosv1.Override
}

// clone is a deep copy named name, for a variant to change.
func (c paidCase) clone(name string) paidCase {
	out := c
	out.name = name
	out.facts = proto.Clone(c.facts).(*factsv1.TaxYearFacts)
	out.proj = proto.Clone(c.proj).(*scenariosv1.YearProjection)
	out.overrides = nil
	for _, o := range c.overrides {
		out.overrides = append(out.overrides, proto.Clone(o).(*scenariosv1.Override))
	}
	return out
}

// run expands the tax paid models and asks paid.Compute, recording both,
// and returns the answer, nil on an error, and the expanded models.
func (c paidCase) run(t *testing.T) (paidVector, *paid.Answer, []*taxpaid.Payments) {
	t.Helper()
	ctx, err := model.NewContext(c.year, c.asOf, c.facts)
	if err != nil {
		t.Fatalf("%s: %v", c.name, err)
	}
	v := paidVector{Case: c.name, Year: c.year, AsOf: encode(t, c.asOf), Facts: encode(t, c.facts),
		Projection: encode(t, c.proj), Status: int(c.status), Tax2210: int64(c.tax), AGI: int64(c.agi),
		ExcessSS: int64(c.excessSS), TaxPaid: []string{}, Overrides: []string{},
		Estimated: []string{}, Questions: []string{}, LeftOut: []string{}, Projections: []string{}}
	b := paid.NewInputs(ctx, c.status, c.proj).Federal(c.tax, c.agi, c.excessSS).Overrides(c.overrides)
	var payments []*taxpaid.Payments
	for _, tp := range c.models {
		v.TaxPaid = append(v.TaxPaid, encode(t, tp))
		p, err := taxpaid.Expand(ctx, tp, ctx.Facts().GetAdjacentPayments())
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		payments = append(payments, p)
		b.Payments(p)
	}
	for _, o := range c.overrides {
		v.Overrides = append(v.Overrides, encode(t, o))
	}
	inputs, err := b.Build()
	if err != nil {
		t.Fatalf("%s: %v", c.name, err)
	}
	answer, err := paid.Compute(inputs)
	v.Error = errText(err)
	if err != nil {
		return v, nil, payments
	}
	for _, e := range answer.Estimated() {
		v.Estimated = append(v.Estimated, encode(t, e))
	}
	v.Questions, v.LeftOut = notesOf(t, answer.Notes())
	v.PaidUS = int64(answer.FederalPayments())
	for _, p := range payments {
		v.Projections = append(v.Projections, encode(t, p.Projection()))
	}
	return v, answer, payments
}

func TestPaid(t *testing.T) {
	files := []string{"../scenarios/v1/testdata/example.jsonc"}
	for _, dir := range []string{"../taxyear/testdata/cases", "../taxyear/testdata/fixtures"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".jsonc") {
				files = append(files, filepath.Join(dir, e.Name()))
			}
		}
	}
	const baseName = "cases/full-2026.jsonc#plan"
	out := []paidVector{}
	var base *paidCase
	for _, path := range files {
		text, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		file, err := taxyear.Parse(text)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		result, err := taxyear.Compute(file)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		for _, sc := range result.Message().GetScenarios() {
			name := filepath.Base(filepath.Dir(path)) + "/" + filepath.Base(path) + "#" + sc.GetScenario()
			in, ok := result.Inputs(sc.GetScenario())
			if !ok || sc.GetProjection() == nil || hasProblemAt(sc, "projection") || hasProblemAt(sc, "federal") {
				continue
			}
			ctx, err := model.NewContext(int(file.GetTaxYear()), file.GetAsOf(), in.GetFacts())
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			proj := proto.Clone(sc.GetProjection()).(*scenariosv1.YearProjection)
			proj.TaxPaid = nil
			models := withAdjacent(ctx, in.GetModel().GetTaxPaid())
			for _, tp := range models {
				p, err := taxpaid.Expand(ctx, tp, ctx.Facts().GetAdjacentPayments())
				if err != nil {
					t.Fatalf("%s: %v", name, err)
				}
				proj.TaxPaid = append(proj.TaxPaid, p.Projection())
			}
			ret, err := federal.Compute(ctx, proj, in)
			if err != nil {
				// No rules for the year: the scenario has no estimated answer.
				continue
			}
			c := paidCase{name: name, year: ctx.Year(), asOf: file.GetAsOf(), facts: in.GetFacts(), proj: proj,
				status: ret.Status(), tax: ret.Tax2210(), agi: ret.AGI(), excessSS: ret.ExcessSocialSecurity(),
				models: models, overrides: in.GetOverrides()}
			if name == baseName {
				b := c.clone(name)
				base = &b
			}
			v, answer, payments := c.run(t)
			out = append(out, v)
			if answer == nil {
				if !hasProblemAt(sc, "estimated") {
					t.Errorf("%s: paid.Compute fails, the scenario result has no estimated problem", name)
				}
				continue
			}
			checkPaidAnswer(t, name, sc, answer, payments)
		}
	}
	checkGzip(t, "paid/paid.json.gz", out)
	if base == nil {
		t.Fatalf("no %s", baseName)
	}
	variants := []paidVector{}
	for _, c := range paidVariants(*base) {
		v, _, _ := c.run(t)
		variants = append(variants, v)
	}
	checkGzip(t, "paid/variants.json.gz", variants)
}

// checkPaidAnswer checks the inputs are answerPayments': the answer is the
// one the scenario's result holds.
func checkPaidAnswer(t *testing.T, name string, sc *scenariosv1.ScenarioResult, answer *paid.Answer, payments []*taxpaid.Payments) {
	t.Helper()
	if len(answer.Estimated()) != len(sc.GetEstimated()) {
		t.Errorf("%s: %d estimated, the scenario result has %d", name, len(answer.Estimated()), len(sc.GetEstimated()))
	} else {
		for i, e := range answer.Estimated() {
			// The summary is added after, from the result.
			want := proto.Clone(sc.GetEstimated()[i]).(*scenariosv1.Estimated)
			want.Summary = nil
			if !proto.Equal(e, want) {
				t.Errorf("%s: estimated[%s] differs from the scenario result's", name, e.GetJurisdiction())
			}
		}
	}
	if len(payments) != len(sc.GetProjection().GetTaxPaid()) {
		t.Errorf("%s: %d tax paid projections, the scenario result has %d", name, len(payments), len(sc.GetProjection().GetTaxPaid()))
		return
	}
	for i, p := range payments {
		if !proto.Equal(p.Projection(), sc.GetProjection().GetTaxPaid()[i]) {
			t.Errorf("%s: tax_paid[%s] differs from the scenario result's", name, p.Jurisdiction())
		}
	}
}

func paidDate(y, m, d int32) *commonv1.Date { return &commonv1.Date{Year: y, Month: m, Day: d} }

func paidMoney(c int64) *commonv1.Money { return &commonv1.Money{Minor: c, Currency: "USD"} }

// paidVariants is the base case with its facts, projection or overrides
// changed.
func paidVariants(base paidCase) []paidCase {
	var out []paidCase
	vary := func(name string) paidCase { return base.clone(base.name + "~" + name) }
	// Every jurisdiction's rules: a year lived in the state, with last
	// year's tax there.
	for _, j := range estimated.Jurisdictions() {
		if j == estimated.Federal || j == estimated.California {
			continue
		}
		c := vary("lived-in-" + string(j))
		c.facts.Residence = []*factsv1.ResidencePeriod{{Id: "lived", State: string(j),
			From: paidDate(2026, 1, 1), To: paidDate(2026, 12, 31)}}
		c.facts.PriorYear = append(c.facts.PriorYear, &factsv1.PriorYearReturn{Id: "prior-" + string(j),
			Jurisdiction: string(j), Status: factsv1.PriorYearReturn_STATUS_FILED,
			TotalTax: paidMoney(412_345), AdjustedGrossIncome: paidMoney(13_000_000)})
		out = append(out, c)
	}
	// Part of the year in California, the rest in Oregon to an open end, and
	// a period that is not a day.
	c := vary("moved-ca-or")
	c.facts.Residence = []*factsv1.ResidencePeriod{
		{Id: "ca", State: "CA", From: paidDate(2026, 1, 1), To: paidDate(2026, 6, 30)},
		{Id: "or", State: "OR", From: paidDate(2026, 7, 1)},
		{Id: "bad", State: "OR", From: paidDate(2026, 13, 1)},
	}
	out = append(out, c)
	// The waiver requests, and withholding on its dates.
	reasons := []factsv1.WaiverRequest_Reason{factsv1.WaiverRequest_REASON_UNSPECIFIED, factsv1.WaiverRequest_REASON_NONE,
		factsv1.WaiverRequest_REASON_CASUALTY, factsv1.WaiverRequest_REASON_DISASTER,
		factsv1.WaiverRequest_REASON_RETIRED, factsv1.WaiverRequest_REASON_DISABLED}
	for _, r := range reasons {
		for _, j := range []string{"US", "CA"} {
			c := vary("waiver-" + j + "-" + r.String())
			c.facts.Elections = append(c.facts.Elections,
				&factsv1.Election{Id: "waiver", Choice: &factsv1.Election_Waiver{Waiver: &factsv1.WaiverRequest{
					Jurisdiction: j, Reason: r, On: paidDate(2026, 3, 1)}}},
				&factsv1.Election{Id: "dates", Choice: &factsv1.Election_WithholdingDates{WithholdingDates: &factsv1.WithholdingDates{
					Jurisdiction: j, AsWithheld: true}}})
			out = append(out, c)
		}
	}
	// No return last year, a return not filed yet, and last year's figures
	// overridden.
	c = vary("no-prior-return")
	for _, p := range c.facts.PriorYear {
		p.Status = factsv1.PriorYearReturn_STATUS_NONE
		p.Filed = nil
	}
	out = append(out, c)
	c = vary("not-filed-yet")
	partYear := false
	for _, p := range c.facts.PriorYear {
		p.Status = factsv1.PriorYearReturn_STATUS_NOT_FILED_YET
		p.FullYearResident = &partYear
		p.Filed = nil
	}
	out = append(out, c)
	c = vary("overrides")
	for _, j := range []string{"US", "CA"} {
		for i, field := range []string{"tax_for_year", "last_year_tax", "last_year_agi", "last_year_withheld"} {
			c.overrides = append(c.overrides, &scenariosv1.Override{Path: "estimated[" + j + "]." + field,
				Amount: paidMoney(int64(1_000_000 + 12_345*i))})
		}
	}
	out = append(out, c)
	// A paycheck with no date, and one on a day that does not exist: the
	// rules refuse.
	if len(base.proj.GetEmployers()) > 0 && len(base.proj.GetEmployers()[0].GetItems()) > 0 {
		c = vary("undated-paycheck")
		c.proj.GetEmployers()[0].GetItems()[0].Date = nil
		out = append(out, c)
		c = vary("bad-paycheck-date")
		c.proj.GetEmployers()[0].GetItems()[0].Date = paidDate(2026, 2, 30)
		out = append(out, c)
	}
	// A year with no rules.
	c = vary("year-2031")
	c.year = 2031
	c.asOf = paidDate(2031, 6, 1)
	out = append(out, c)
	// As of after the year, moved to Oregon: the filed state returns give
	// the year's tax, and last year's Oregon return its figures.
	c = vary("finished")
	c.asOf = paidDate(2027, 3, 1)
	c.facts.Residence = []*factsv1.ResidencePeriod{
		{Id: "ca", State: "CA", From: paidDate(2026, 1, 1), To: paidDate(2026, 8, 15)},
		{Id: "or", State: "OR", From: paidDate(2026, 8, 16)},
	}
	c.facts.Unassigned = append(c.facts.Unassigned,
		&factsv1.Document{Id: "ca-2025", Form: &factsv1.Document_CaliforniaReturn{CaliforniaReturn: &factsv1.CaliforniaReturn{
			Form_540: &form540.Form540{TaxYear: 2025, TotalTax: paidMoney(1)}}}},
		&factsv1.Document{Id: "ca-2026", Form: &factsv1.Document_CaliforniaReturn{CaliforniaReturn: &factsv1.CaliforniaReturn{
			Form_540: &form540.Form540{TaxYear: 2026, TotalTax: paidMoney(523_456)}}}},
		&factsv1.Document{Id: "or-2026", Form: &factsv1.Document_OregonReturn{OregonReturn: &factsv1.OregonReturn{
			FormOr40: &formor40.FormOR40{TaxYear: 2026, TaxAfterAllCredits: paidMoney(98_765)}}}})
	c.facts.PriorYear = append(c.facts.PriorYear, &factsv1.PriorYearReturn{Id: "prior-or", Jurisdiction: "OR",
		Status: factsv1.PriorYearReturn_STATUS_FILED, Filed: &factsv1.PriorYearReturn_Oregon{Oregon: &factsv1.OregonReturn{
			FormOr40: &formor40.FormOR40{TaxYear: 2025, TaxAfterAllCredits: paidMoney(54_321),
				FederalAdjustedGrossIncome: paidMoney(9_876_543), TaxableIncome: paidMoney(8_765_432),
				OregonTaxWithheld: paidMoney(43_210)}}}})
	out = append(out, c)
	return out
}
