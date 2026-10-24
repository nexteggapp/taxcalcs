// Nevada's estimated tax rules for 2026, which are that there is no individual
// income tax to estimate.
//
// There is no arithmetic to check. The oracle is the document the finding
// rests on: each quoted sentence is compared against its extract in testdata/,
// and each result must cite it.
package nevadataxcalc_test

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/nevadataxcalc"
	"github.com/nexteggapp/taxcalcs/working"
)

var day = estimated.MustDate

const code = estimated.Jurisdiction("NV")

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func rules(t *testing.T) estimated.EstimatedTax {
	t.Helper()
	return must(nevadataxcalc.EstimatedTax(2026))
}

func extract(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return strings.Join(strings.Fields(string(raw)), " ")
}

// basis is each document the finding rests on, a sentence it contains, and
// the citation the rules give it.
var basis = []struct{ file, quote, cite string }{
	{
		file:  "nv-const-art10-sec1-9.txt",
		quote: "No income tax shall be levied upon the wages or personal income of natural persons.",
		cite:  "Nev. Const. art. 10, § 1(9)",
	},
}

// facts is a household with every figure a state with an income tax would
// read, so that the answer is seen not to depend on any of them.
func facts(t *testing.T, withTax bool) estimated.Facts {
	t.Helper()
	fb := estimated.NewFigures().FederalAGI(250_000 * money.Dollar)
	if withTax {
		fb.Tax(12_000 * money.Dollar)
	}
	last := must(estimated.NewFigures().Tax(9_000 * money.Dollar).Withheld(1_000 * money.Dollar).Build())
	return must(estimated.NewFacts(code, 2026).FilingStatus(filing.MarriedFilingJointly).
		ThisYear(must(fb.Build())).
		PriorYear(must(estimated.NewPriorReturn().Filed(true).Figures(last).Build())).
		Withheld(must(estimated.NewWithheld().On(day(2026, 6, 30)).Amount(500 * money.Dollar).Build())).
		Paid(must(estimated.NewPayment().On(day(2026, 4, 15)).Amount(2_000 * money.Dollar).Build())).
		Build())
}

// Each quoted sentence is in its document, and every result cites it with
// the quote.
func TestEstimatedFindingIsInItsSources(t *testing.T) {
	for _, b := range basis {
		if !strings.Contains(extract(t, b.file), b.quote) {
			t.Errorf("%q is not in %s", b.quote, b.file)
		}
	}
	f := facts(t, true)
	req := must(rules(t).Requirement(f))
	sched := must(rules(t).Schedule(f))
	pen := must(rules(t).Penalty(f, day(2027, 4, 15)))
	for name, applied := range map[string][]working.Rule{"Requirement": req.Applied(), "Schedule": sched.Applied(), "Penalty": pen.Applied()} {
		if len(applied) != len(basis) {
			t.Errorf("%s cites %d documents, want %d", name, len(applied), len(basis))
			continue
		}
		for i, b := range basis {
			if applied[i].Cite() != b.cite || !strings.Contains(applied[i].Applied(), b.quote) {
				t.Errorf("%s rule %d is %q: %q; want %q quoting %q", name, i+1, applied[i].Cite(), applied[i].Applied(), b.cite, b.quote)
			}
		}
	}
}

// No payments are required and no penalty can arise, whatever the
// household's figures, and whether or not this year's tax is given.
func TestEstimatedNothingToEstimate(t *testing.T) {
	for _, withTax := range []bool{true, false} {
		res := must(rules(t).Requirement(facts(t, withTax)))
		r := res.Value()
		if r.Required() || !r.Exempt() || r.Target() != 0 || len(r.Options()) != 1 || !r.TargetKnown() {
			t.Errorf("with tax %v: required %v, exempt %v, target %v, %d options", withTax, r.Required(), r.Exempt(), r.Target(), len(r.Options()))
		}
		// The one option is the exception, and it is the target.
		if c, chosen := r.Chosen(); !chosen || c.Basis() != estimated.BasisException || c.Label() != "No income tax" || c.Amount() != 0 {
			t.Errorf("with tax %v: chosen %v %q at %v", withTax, chosen, c.Label(), c.Amount())
		}
		for _, s := range []string{"no Nevada income tax to estimate", "2026"} {
			if !strings.Contains(r.Why(), s) {
				t.Errorf("with tax %v: the reason %q does not say %q", withTax, r.Why(), s)
			}
		}
		if len(res.Required()) != 0 || len(res.Divergences()) != 0 {
			t.Errorf("with tax %v: %d questions and %d divergences, want none", withTax, len(res.Required()), len(res.Divergences()))
		}
	}
}

func TestEstimatedScheduleHasNoDueDates(t *testing.T) {
	res := must(rules(t).Schedule(facts(t, true)))
	if got := res.Value(); len(got) != 0 {
		t.Errorf("%d installments, want none", len(got))
	}
}

func TestEstimatedPenaltyIsZero(t *testing.T) {
	asOf := day(2027, 4, 15)
	p := must(rules(t).Penalty(facts(t, false), asOf)).Value()
	if p.Total() != 0 || len(p.Lines()) != 0 || p.AsOf() != asOf || p.UpperBound() || p.Reason() != "No income tax." {
		t.Errorf("penalty %v over %d lines to %v, upper bound %v; want zero to %v", p.Total(), len(p.Lines()), p.AsOf(), p.UpperBound(), asOf)
	}
	if _, err := rules(t).Penalty(facts(t, false), estimated.Date{}); err == nil {
		t.Error("a penalty with no date to figure it to was accepted")
	}
}

func TestEstimatedRefusesWhatIsNotBuilt(t *testing.T) {
	for _, year := range []int{2025, 2027} {
		if _, err := nevadataxcalc.EstimatedTax(year); !errors.Is(err, estimated.ErrNoRules) {
			t.Errorf("%d: %v, want ErrNoRules", year, err)
		}
	}
	this := must(estimated.NewFigures().Build())
	for name, f := range map[string]estimated.Facts{
		"another state's facts": must(estimated.NewFacts(estimated.California, 2026).FilingStatus(filing.Single).ThisYear(this).Build()),
		"2027":                  must(estimated.NewFacts(code, 2027).FilingStatus(filing.Single).ThisYear(this).Build()),
		"extras":                must(estimated.NewFacts(code, 2026).FilingStatus(filing.Single).ThisYear(this).State("extras").Build()),
	} {
		if _, err := rules(t).Requirement(f); err == nil {
			t.Errorf("%s: Requirement accepted them", name)
		}
		if _, err := rules(t).Schedule(f); err == nil {
			t.Errorf("%s: Schedule accepted them", name)
		}
		if _, err := rules(t).Penalty(f, day(2027, 4, 15)); err == nil {
			t.Errorf("%s: Penalty accepted them", name)
		}
	}
}

func TestEstimatedExplanationSaysThereIsNothingToEstimate(t *testing.T) {
	e := rules(t).Explanation()
	text := e.Summary() + " " + strings.Join(e.Body(), " ") + " " + e.VersusFederal()
	for _, s := range []string{"nothing can be penalised as late", "Federal estimated tax", "wages or personal income"} {
		if !strings.Contains(text, s) {
			t.Errorf("the explanation does not mention %q", s)
		}
	}
	if len(e.Sources()) != len(basis) {
		t.Errorf("the explanation cites %d sources, want %d", len(e.Sources()), len(basis))
	}
}
