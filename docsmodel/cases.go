package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/nexteggapp/taxcalcs/cases"
	casesv1 "github.com/nexteggapp/taxcalcs/cases/v1"
	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
)

// TestCase is an end-to-end case from cases/, as the test case browser
// shows it: the household, and each expected figure with where it came from
// and what the rules produce today.
type TestCase struct {
	ID           string        `json:"id"`
	Title        string        `json:"title"`
	Jurisdiction string        `json:"jurisdiction"`
	Year         int32         `json:"year"`
	Tags         []string      `json:"tags"`
	Origin       string        `json:"origin"`
	OriginSource *CaseSource   `json:"originSource,omitempty"`
	OriginNote   string        `json:"originNote,omitempty"`
	Note         string        `json:"note,omitempty"`
	Facts        []Fact        `json:"facts"`
	Figures      []*CaseFigure `json:"figures"`
	Passing      int           `json:"passing"`
	Gaps         int           `json:"gaps"`
	Path         string        `json:"path"`
}

// CaseSource is a source a case cites.
type CaseSource struct {
	ID     string `json:"id"`
	Anchor string `json:"anchor,omitempty"`
	Page   int32  `json:"page,omitempty"`
}

// CaseFigure is one expected figure.
type CaseFigure struct {
	Form       string      `json:"form"`
	Line       string      `json:"line"`
	Caption    string      `json:"caption"`
	Expected   string      `json:"expected"`
	Got        string      `json:"got"`
	Outcome    string      `json:"outcome"`
	Provenance string      `json:"provenance"`
	Source     *CaseSource `json:"source,omitempty"`
	Working    []CaseStep  `json:"working,omitempty"`
	Gap        string      `json:"gap,omitempty"`
	GapIssue   string      `json:"gapIssue,omitempty"`
}

// CaseStep is one step of a worked figure.
type CaseStep struct {
	What       string      `json:"what"`
	Arithmetic string      `json:"arithmetic"`
	Source     *CaseSource `json:"source,omitempty"`
}

// FormCoverage is, for one form, each line the rules implement and the
// strongest provenance of a case figure that checks it ("" for none).
type FormCoverage struct {
	Form  string         `json:"form"`
	Lines []LineCoverage `json:"lines"`
}

// LineCoverage is one line of a FormCoverage.
type LineCoverage struct {
	Line       string `json:"line"`
	Caption    string `json:"caption"`
	Provenance string `json:"provenance,omitempty"`
}

var provenanceNames = map[casesv1.Provenance]string{
	casesv1.Provenance_PROVENANCE_PUBLISHED_EXAMPLE:    "published example",
	casesv1.Provenance_PROVENANCE_PUBLISHED_TABLE:      "published table",
	casesv1.Provenance_PROVENANCE_INDEPENDENTLY_WORKED: "independently worked",
	casesv1.Provenance_PROVENANCE_HAND_WORKED_REVIEWED: "hand-worked, reviewed",
	casesv1.Provenance_PROVENANCE_HAND_WORKED:          "hand-worked",
	casesv1.Provenance_PROVENANCE_FROZEN_REGRESSION:    "frozen regression",
}

var originNames = map[casesv1.OriginKind]string{
	casesv1.OriginKind_ORIGIN_KIND_AGENCY_EXAMPLE:      "an agency's worked example",
	casesv1.OriginKind_ORIGIN_KIND_EFILE_TEST_SCENARIO: "a government e-file test scenario",
	casesv1.OriginKind_ORIGIN_KIND_CONSTRUCTED:         "made up for the test",
}

func caseSource(r *casesv1.SourceRef) *CaseSource {
	if r.GetId() == "" {
		return nil
	}
	return &CaseSource{ID: r.GetId(), Anchor: r.GetAnchor(), Page: r.GetPage()}
}

func usd(cents int64) string {
	neg := cents < 0
	if neg {
		cents = -cents
	}
	d := fmt.Sprint(cents / 100)
	for i := len(d) - 3; i > 0; i -= 3 {
		d = d[:i] + "," + d[i:]
	}
	s := fmt.Sprintf("$%s.%02d", d, cents%100)
	if neg {
		return "-" + s
	}
	return s
}

func moneyUSD(m *commonv1.Money) string { return usd(m.GetMinor()) }

// caseFacts summarises a household's inputs in a few lines.
func caseFacts(in *returnsv1.ReturnInputs) []Fact {
	fed := in.GetFederal()
	hh := fed.GetHousehold()
	var facts []Fact
	status := strings.TrimPrefix(fed.GetElections().GetFilingStatus().String(), "FILING_STATUS_ELECTION_")
	facts = append(facts, Fact{Label: "Filing status", Value: strings.ToLower(strings.ReplaceAll(status, "_", " "))})
	for _, p := range []*returnsv1.Person{hh.GetTaxpayer(), hh.GetSpouse()} {
		if p == nil {
			continue
		}
		label := "Taxpayer"
		if p == hh.GetSpouse() {
			label = "Spouse"
		}
		facts = append(facts, Fact{Label: label, Value: fmt.Sprintf("%s, born %d", p.GetFirstName(), p.GetDateOfBirth().GetYear())})
	}
	if n := len(hh.GetDependents()); n > 0 {
		facts = append(facts, Fact{Label: "Dependants", Value: fmt.Sprint(n)})
	}
	docs := fed.GetDocuments()
	for _, w := range docs.GetW2S() {
		facts = append(facts, Fact{Label: "W-2 wages (" + w.GetPerson() + ")", Value: moneyUSD(w.GetForm().GetWagesTipsOtherCompensation())})
	}
	for _, d := range docs.GetInterest() {
		facts = append(facts, Fact{Label: "1099-INT interest (" + d.GetPerson() + ")", Value: moneyUSD(d.GetForm().GetInterestIncome())})
	}
	for _, d := range docs.GetRetirementDistributions() {
		facts = append(facts, Fact{Label: "1099-R taxable (" + d.GetPerson() + ")", Value: moneyUSD(d.GetForm().GetTaxableAmount())})
	}
	for _, d := range docs.GetSocialSecurity() {
		facts = append(facts, Fact{Label: "SSA-1099 benefits (" + d.GetPerson() + ")", Value: moneyUSD(d.GetForm().GetNetBenefits())})
	}
	for _, s := range in.GetStates() {
		facts = append(facts, Fact{Label: "State return", Value: s.GetState()})
	}
	return facts
}

// runCases reads the case files, checks each with the rules, and builds the
// browser's cases and coverage map.
func runCases(m *Model) error {
	eng, err := federal.DefaultEngine()
	if err != nil {
		return err
	}
	forms, err := cases.Forms()
	if err != nil {
		return err
	}
	loaded, err := cases.Load()
	if err != nil {
		return err
	}
	if err := cases.Run(eng, forms, loaded); err != nil {
		return err
	}
	strongest := map[string]map[string]casesv1.Provenance{}
	for _, l := range loaded {
		c := l.Case
		tc := &TestCase{
			ID: c.GetId(), Title: c.GetTitle(), Jurisdiction: strings.ToUpper(l.Jurisdiction),
			Year: c.GetInputs().GetTaxYear(), Tags: c.GetTags(), Origin: originNames[c.GetOrigin().GetKind()],
			OriginSource: caseSource(c.GetOrigin().GetSource()), OriginNote: c.GetOrigin().GetNote(),
			Note: c.GetNote(), Facts: caseFacts(c.GetInputs()), Path: "cases/" + l.Path,
		}
		if tc.Tags == nil {
			tc.Tags = []string{}
		}
		for _, r := range l.Results {
			e := r.Expected
			f := &CaseFigure{
				Form: e.GetForm(), Line: e.GetLine(), Caption: forms[e.GetForm()].Captions[e.GetLine()],
				Expected: usd(e.GetCents()), Got: r.Got, Outcome: r.Outcome.String(),
				Provenance: provenanceNames[e.GetProvenance()], Source: caseSource(e.GetSource()),
			}
			if _, ok := e.GetValue().(*casesv1.Expected_Integer); ok {
				f.Expected = fmt.Sprint(e.GetInteger())
			} else if g := strings.TrimSuffix(r.Got, " cents"); g != r.Got {
				var n int64
				fmt.Sscan(g, &n)
				f.Got = usd(n)
			}
			gap := e.GetKnownGap()
			if gap == nil {
				gap = c.GetKnownGap()
			}
			if gap != nil {
				f.Gap, f.GapIssue = gap.GetReason(), gap.GetIssue()
			}
			for _, s := range e.GetWorking() {
				f.Working = append(f.Working, CaseStep{What: s.GetWhat(), Arithmetic: s.GetArithmetic(), Source: caseSource(s.GetSource())})
			}
			switch r.Outcome {
			case cases.Pass:
				tc.Passing++
			case cases.Gap:
				tc.Gaps++
			}
			tc.Figures = append(tc.Figures, f)
			if strongest[e.GetForm()] == nil {
				strongest[e.GetForm()] = map[string]casesv1.Provenance{}
			}
			// A lower enum value is a stronger provenance.
			if p, ok := strongest[e.GetForm()][e.GetLine()]; !ok || e.GetProvenance() < p {
				strongest[e.GetForm()][e.GetLine()] = e.GetProvenance()
			}
		}
		m.TestCases = append(m.TestCases, tc)
	}
	var names []string
	for f := range strongest {
		names = append(names, f)
	}
	sort.Strings(names)
	for _, name := range names {
		fc := &FormCoverage{Form: name}
		for _, line := range forms[name].Lines {
			lc := LineCoverage{Line: line, Caption: forms[name].Captions[line]}
			if p, ok := strongest[name][line]; ok {
				lc.Provenance = provenanceNames[p]
			}
			fc.Lines = append(fc.Lines, lc)
		}
		m.TestCoverage = append(m.TestCoverage, fc)
	}
	return nil
}
