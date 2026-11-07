package golden

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/nexteggapp/taxcalcs/returns/preparer"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear"
	"github.com/nexteggapp/taxcalcs/taxyear/federal"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	"github.com/nexteggapp/taxcalcs/taxyear/model/taxpaid"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
	form1040 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1040"
)

// The federal vectors: for every scenario of every taxyear test case that
// reaches the return layer, what taxyear's computeScenario gives
// federal.Compute (the context's year, as_of and facts, the projection and
// the scenario's resolved inputs) and what it returns. The projection is
// the scenario result's, with its tax paid projections as they were before
// the estimated-payment rules filled them in: each tax paid model's
// Projection straight after Expand, as computeScenario's project makes it.
//
// NoWorking is the same return without the engine's working, which the
// wasm entry point asks for (Compute's withWorking on feat/wasm-entry): the
// engine's outputs as the preparer gives them without IncludeWorking, and
// what Compute then keeps: the working the summary adds, and the notes
// without the engine's missing-input questions.

// federalVector is one scenario's federal.Compute.
type federalVector struct {
	Case       string `json:"case"`
	Year       int    `json:"year"`
	AsOf       string `json:"as_of"`
	Facts      string `json:"facts"`
	Projection string `json:"projection"`
	Inputs     string `json:"inputs"`
	Error      string `json:"error"`
	// Filed is the year's filed Form 1040 that taxyear splits the
	// overpayment by, or "".
	Filed        string            `json:"filed"`
	HasRules     bool              `json:"has_rules"`
	Status       int               `json:"status"`
	ReturnInputs string            `json:"return_inputs"`
	Outputs      string            `json:"outputs"`
	Form         string            `json:"form"`
	Summary      string            `json:"summary"`
	Working      []string          `json:"working"`
	Questions    []string          `json:"questions"`
	LeftOut      []string          `json:"left_out"`
	AGI          int64             `json:"agi"`
	TotalTax     int64             `json:"total_tax"`
	ExcessSS     int64             `json:"excess_social_security"`
	Tax2210      int64             `json:"tax_2210"`
	Excess       int64             `json:"excess_contribution_tax"`
	Split        string            `json:"split"`
	NoWorking    *federalNoWorking `json:"no_working,omitempty"`
}

// federalNoWorking is the return without the engine's working.
type federalNoWorking struct {
	// Outputs is "" for the outputs with their working left off.
	Outputs   string   `json:"outputs"`
	Working   []string `json:"working"`
	Questions []string `json:"questions"`
}

func TestFederal(t *testing.T) {
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
	srv, err := preparer.NewServer()
	if err != nil {
		t.Fatal(err)
	}
	out := []federalVector{}
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
			if !ok || sc.GetProjection() == nil || hasProblemAt(sc, "projection") {
				continue
			}
			ctx, err := model.NewContext(int(file.GetTaxYear()), file.GetAsOf(), in.GetFacts())
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			proj := proto.Clone(sc.GetProjection()).(*scenariosv1.YearProjection)
			proj.TaxPaid = nil
			for _, tp := range withAdjacent(ctx, in.GetModel().GetTaxPaid()) {
				p, err := taxpaid.Expand(ctx, tp, ctx.Facts().GetAdjacentPayments())
				if err != nil {
					t.Fatalf("%s: %v", name, err)
				}
				proj.TaxPaid = append(proj.TaxPaid, p.Projection())
			}
			v := federalVector{Case: name, Year: ctx.Year(), AsOf: encode(t, file.GetAsOf()), Facts: encode(t, in.GetFacts()),
				Projection: encode(t, proj), Inputs: encode(t, in), HasRules: federal.HasRules(ctx.Year()),
				Working: []string{}, Questions: []string{}, LeftOut: []string{}}
			ret, err := federal.Compute(ctx, proto.Clone(proj).(*scenariosv1.YearProjection), in)
			v.Error = errText(err)
			if err != nil {
				out = append(out, v)
				continue
			}
			v.Status = int(ret.Status())
			v.ReturnInputs, v.Outputs, v.Form = encode(t, ret.Inputs()), encode(t, ret.Outputs()), encode(t, ret.Form())
			v.Summary = encode(t, ret.Summary())
			for _, w := range ret.Working() {
				v.Working = append(v.Working, encode(t, w))
			}
			v.Questions, v.LeftOut = notesOf(t, ret.Notes())
			v.AGI, v.TotalTax, v.ExcessSS = int64(ret.AGI()), int64(ret.TotalTax()), int64(ret.ExcessSocialSecurity())
			v.Tax2210, v.Excess = int64(ret.Tax2210()), int64(federal.ExcessContributionTax(ret.Form()))
			v.NoWorking = noWorking(t, srv, ret)
			filed := filedFederal(ctx)
			v.Filed = encode(t, filed)
			ret.SplitOverpayment(filed)
			v.Split = encode(t, ret.Form().GetForm_1040())

			// The projection is computeScenario's: the return it makes is the
			// one the scenario's result holds.
			got, want := proto.Clone(ret.Form()).(*factsv1.FederalReturn), proto.Clone(sc.GetFederal()).(*factsv1.FederalReturn)
			got.GetForm_1040().Dependents, want.GetForm_1040().Dependents = nil, nil
			if !proto.Equal(got, want) {
				t.Errorf("%s: the federal return differs from the scenario result's", name)
			}
			out = append(out, v)
		}
	}
	checkGzip(t, "federal/federal.json.gz", out)
}

func hasProblemAt(sc *scenariosv1.ScenarioResult, path string) bool {
	for _, p := range sc.GetProblems() {
		if p.GetPath() == path {
			return true
		}
	}
	return false
}

// noWorking is ret as Compute makes it without the engine's working: the
// outputs the preparer gives for the same inputs without IncludeWorking,
// the working the summary adds (the charity floor's statement, and line
// 14's terms on a line of its own), and the notes but the engine's
// missing-input questions, which come only from the working.
func noWorking(t *testing.T, srv *preparer.Server, ret *federal.Return) *federalNoWorking {
	resp, err := srv.Prepare(context.Background(), &returnsv1.PrepareRequest{Inputs: ret.Inputs()})
	if err != nil {
		t.Fatal(err)
	}
	out := &federalNoWorking{Working: []string{}, Questions: []string{}}
	// The outputs are kept only where they are not the outputs with the
	// working left off, as the preparer builds the working last.
	with := proto.Clone(ret.Outputs()).(*returnsv1.ReturnOutputs)
	with.Working = nil
	if !proto.Equal(with, resp.GetOutputs()) {
		out.Outputs = encode(t, resp.GetOutputs())
	}
	const line = "federal.schedule_a.total_gifts_to_charity"
	var terms *scenariosv1.Working
	for _, w := range ret.Working() {
		switch {
		case strings.HasPrefix(w.GetPath(), "summary."):
			out.Working = append(out.Working, encode(t, w))
		case w.GetPath() == line && len(w.GetTerms()) > 0:
			terms = &scenariosv1.Working{Path: line, Terms: w.GetTerms()}
		}
	}
	if terms != nil {
		out.Working = append(out.Working, encode(t, terms))
	}
	for _, q := range ret.Notes().Questions() {
		if !strings.HasPrefix(q.ID(), "federal/missing/") {
			out.Questions = append(out.Questions, encode(t, q.Proto()))
		}
	}
	return out
}

// filedFederal is taxyear's: the year's own filed Form 1040 once the year
// is over, or nil.
func filedFederal(ctx *model.Context) *form1040.Form1040 {
	if !ctx.Finished() {
		return nil
	}
	for _, d := range ctx.Facts().GetUnassigned() {
		if f := d.GetFederalReturn().GetForm_1040(); f != nil && model.Usable(d.GetSource()) && int(f.GetTaxYear()) == ctx.Year() {
			return f
		}
	}
	return nil
}
