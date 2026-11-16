package golden

// The taxlisp estimated tax answers, held to the frozen vectors.
//
// TestEstimatedTaxlisp prepares every household in each jurisdiction's
// inputs file, testdata/estimated/inputs/<code>.json.gz, with the federal
// engine, and compares the answers with testdata/estimated/<code>.json.gz,
// the answers the Go calculators gave for the same households (see
// testdata/estimated/README.md). Both sides are first put in one comparable
// shape, cmpYear: the vector's parts the proto answer also holds, and
// nothing else.
//
// What the comparison leaves out, because EstimatedAnswer doesn't hold it:
//   - every method's applied rules (the working notes);
//   - which method (requirement, schedule, penalty) asked each question or
//     reported each divergence, a question's "asked" flag and each method's
//     "needs": the answer has one list of each, which is compared with the
//     three methods' lists joined in that order without repeats;
//   - the text of a schedule or penalty error after a computed requirement,
//     such as ErrNeedsThisYearTax: the answer only leaves the installments
//     empty (requirement status unknown) or the penalty unset;
//   - an option's percent for last year's income, which its id doesn't
//     carry (always 100%);
//   - the penalty's reason, waiver kind, short_due_dates, the Go upper_bound
//     (a target that can only be too high; the proto's upper_bound is a rate
//     not yet published, which the vector's rated_through carries), the
//     waiver outcome's kept charges, and each line's parts;
//   - each year's answer_problems and copy_problems, which are the Go test
//     machinery's checks, not answers.

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	"github.com/nexteggapp/taxcalcs/returns/rules/states"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	"github.com/nexteggapp/taxcalcs/taxlisp/eval"
	"github.com/nexteggapp/taxcalcs/taxlisp/expand"
	"github.com/nexteggapp/taxcalcs/taxlisp/parser"
	"github.com/nexteggapp/taxcalcs/taxlisp/types"
	taxlispv1 "github.com/nexteggapp/taxcalcs/taxlisp/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// estimatedLanded are the jurisdictions whose answers must match their
// vectors: every one, and the reference rules, ZZ. The order is that of
// estimated/taxlisp.json.
var estimatedLanded = []string{
	"US",
	"AK",
	"AL",
	"AR",
	"AZ",
	"CA",
	"CO",
	"CT",
	"DC",
	"DE",
	"FL",
	"GA",
	"HI",
	"ID",
	"IL",
	"IN",
	"IA",
	"KS",
	"KY",
	"LA",
	"MA",
	"MD",
	"ME",
	"MI",
	"MN",
	"MO",
	"MS",
	"MT",
	"NC",
	"ND",
	"NE",
	"NH",
	"NM",
	"NV",
	"NJ",
	"NY",
	"OH",
	"OK",
	"OR",
	"PA",
	"RI",
	"SC",
	"SD",
	"TN",
	"TX",
	"UT",
	"VA",
	"VT",
	"WA",
	"WI",
	"WV",
	"WY",
	referenceCode,
}

// estimatedYears are the tax years every jurisdiction's vectors cover.
var estimatedYears = []int{2025, 2026}

// referenceCode is the reference rules' jurisdiction: a small one, written
// in testdata/estimated/reference.tlisp to exercise the shared machinery,
// which is not any jurisdiction's law.
const referenceCode = "ZZ"

func TestEstimatedTaxlisp(t *testing.T) {
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatal(err)
	}
	ref, err := referenceEngine()
	if err != nil {
		t.Fatal(err)
	}
	// Every answer, for the Rust engine's test of the same rules
	// (rust/returns/tests/estimated_golden.rs).
	var cases []estimatedCase
	for _, j := range estimatedLanded {
		t.Run(string(j), func(t *testing.T) {
			ins, err := readInputs(j)
			if err != nil {
				t.Fatal(err)
			}
			prepare := eng.Prepare
			if j == referenceCode {
				// The reference rules are a test file, not production
				// rules: their own engine answers ZZ alone.
				prepare = ref.prepare
			}
			answers := map[int][]*returnsv1.EstimatedAnswer{}
			for _, year := range estimatedYears {
				for _, h := range ins[year] {
					out, err := prepare(h.inputs)
					if err != nil {
						t.Fatalf("%d %q: Prepare: %v", year, h.name, err)
					}
					if n := len(out.GetEstimated()); n != 1 {
						t.Fatalf("%d %q: %d estimated answers, want 1", year, h.name, n)
					}
					a := out.GetEstimated()[0]
					if j != referenceCode {
						// The reference rules fill no worksheet.
						for _, p := range worksheetProblems(h.inputs, a) {
							t.Errorf("%d %q: the worksheet's %s", year, h.name, p)
						}
					}
					answers[year] = append(answers[year], a)
					cases = append(cases, estimatedCase{
						Jurisdiction: string(j), Year: year, Name: h.name, Inputs: encode(t, h.inputs),
						Estimated: encode(t, &returnsv1.ReturnOutputs{Estimated: out.GetEstimated()}),
					})
				}
			}
			compareWithVector(t, j, ins, answers)
		})
	}
	check(t, "estimated/taxlisp.json", cases)
}

// estimatedCase is one household's taxlisp answer: its inputs, and a
// ReturnOutputs holding only the estimated answers, each in base64.
type estimatedCase struct {
	Jurisdiction string `json:"jurisdiction"`
	Year         int    `json:"year"`
	Name         string `json:"name"`
	Inputs       string `json:"inputs"`
	Estimated    string `json:"estimated"`
}

// referenceRules is the reference jurisdiction ZZ's rules in taxlisp, on the
// shared machinery, as the Go reference rules that wrote its vector were.
const referenceRules = "testdata/estimated/reference.tlisp"

// refEngine is the federal and state rules with the reference rules after
// them, answering through reference-prepare.
type refEngine struct{ ev *eval.Evaluator }

// referenceEngine loads the rules as federal.LoadEngine does, and the
// reference rules after them.
func referenceEngine() (refEngine, error) {
	fed, err := federal.Files()
	if err != nil {
		return refEngine{}, err
	}
	sts, err := states.Files()
	if err != nil {
		return refEngine{}, err
	}
	for i := range sts {
		sts[i].Name = "states/" + sts[i].Name
	}
	src, err := os.ReadFile(referenceRules)
	if err != nil {
		return refEngine{}, err
	}
	files := append(append(fed, sts...), states.File{Name: referenceRules, Source: string(src)})
	combined := &taxlispv1.ModuleFile{}
	for _, f := range files {
		mod, err := parser.ParseFile(f.Name, f.Source)
		if err != nil {
			return refEngine{}, fmt.Errorf("parsing %s: %w", f.Name, err)
		}
		if err := expand.File(mod); err != nil {
			return refEngine{}, err
		}
		combined.Expressions = append(combined.Expressions, mod.GetExpressions()...)
	}
	if err := types.NewChecker(protoregistry.GlobalFiles).CheckModule(combined); err != nil {
		return refEngine{}, fmt.Errorf("typechecking the reference rules: %w", err)
	}
	ev := eval.NewEvaluator(protoregistry.GlobalFiles, protoregistry.GlobalTypes)
	if _, err := ev.EvalModule(combined); err != nil {
		return refEngine{}, fmt.Errorf("initializing the reference rules: %w", err)
	}
	return refEngine{ev}, nil
}

// prepare is Engine.Prepare with reference-prepare in place of
// prepare-return.
func (e refEngine) prepare(in *returnsv1.ReturnInputs) (*returnsv1.ReturnOutputs, error) {
	v, err := e.ev.Call("reference-prepare", eval.ProtoMessageVal{Msg: in.ProtoReflect()})
	if err != nil {
		return nil, err
	}
	pm, ok := v.(eval.ProtoMessageVal)
	if !ok {
		return nil, fmt.Errorf("reference-prepare returned %T", v)
	}
	b, err := proto.Marshal(pm.Msg.Interface())
	if err != nil {
		return nil, err
	}
	out := &returnsv1.ReturnOutputs{}
	return out, proto.Unmarshal(b, out)
}

// compareWithVector compares answers, per year in the order of the inputs
// file's households, with j's vector.
func compareWithVector(t *testing.T, j string, ins map[int][]namedInputs, answers map[int][]*returnsv1.EstimatedAnswer) {
	t.Helper()
	var vector struct {
		Years []map[string]any `json:"years"`
	}
	if err := readGzipJSON("estimated/"+string(j)+".json.gz", &vector); err != nil {
		t.Fatal(err)
	}
	if len(vector.Years) != len(estimatedYears) {
		t.Fatalf("the vector has %d years, want %d", len(vector.Years), len(estimatedYears))
	}
	for i, year := range estimatedYears {
		want := vectorYear(vector.Years[i])
		got := answerYear(j, year, ins[year], answers[year])
		if want.Error != got.Error || !jsonEqual(want.Explanation, got.Explanation) {
			t.Errorf("%d: got error %q, explanation %s; want error %q, explanation %s",
				year, got.Error, mustJSON(got.Explanation), want.Error, mustJSON(want.Explanation))
		}
		if len(want.Households) != len(got.Households) {
			t.Errorf("%d: got %d households, want %d", year, len(got.Households), len(want.Households))
			continue
		}
		for k := range want.Households {
			if !jsonEqual(want.Households[k], got.Households[k]) {
				t.Errorf("%d %q:\n got %s\nwant %s", year, want.Households[k]["name"],
					mustJSON(got.Households[k]), mustJSON(want.Households[k]))
			}
		}
	}
}

// cmpYear is one year of answers in the shape both sides are compared in:
// an error for the whole year, or an explanation and each household.
type cmpYear struct {
	Error       string
	Explanation any
	Households  []map[string]any
}

func jsonEqual(a, b any) bool { return bytes.Equal(mustJSON(a), mustJSON(b)) }

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func readGzipJSON(name string, v any) error {
	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		return err
	}
	defer f.Close()
	z, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	b, err := io.ReadAll(z)
	if err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	return d.Decode(v)
}

// The texts the Go calculators' errors began with, which the vectors hold.
const (
	errNoRules     = "estimated: no estimated tax rules for this jurisdiction and year"
	errUnsupported = "estimated: these facts need rules this build does not implement"
)

// outcomeOf is the outcome a Go error stands for.
func outcomeOf(text string) string {
	switch {
	case strings.HasPrefix(text, errNoRules):
		return "no rules"
	case strings.HasPrefix(text, errUnsupported):
		return "unsupported"
	default:
		return "refused"
	}
}

// The vector's side.

func vectorYear(y map[string]any) cmpYear {
	if e, ok := y["error"].(string); ok {
		return cmpYear{Error: e}
	}
	out := cmpYear{Explanation: y["explanation"]}
	for _, h := range y["households"].([]any) {
		out.Households = append(out.Households, vectorHousehold(h.(map[string]any)))
	}
	return out
}

func vectorHousehold(h map[string]any) map[string]any {
	req, sched, pen := h["requirement"].(map[string]any), h["schedule"].(map[string]any), h["penalty"].(map[string]any)
	out := map[string]any{"name": h["name"]}
	if e, ok := req["error"].(string); ok {
		out["outcome"], out["error"] = outcomeOf(e), e
		// The answer has one reason; the three methods must agree on it.
		if sched["error"] != e {
			out["schedule_error"] = sched["error"]
		}
		if pen["error"] != e {
			out["penalty_error"] = pen["error"]
		}
		return out
	}
	r := req["value"].(map[string]any)
	options := []any{}
	for _, o := range r["options"].([]any) {
		options = append(options, o)
	}
	out["requirement"] = map[string]any{
		"required": r["required"], "exempt": r["exempt"], "target_known": r["target_known"],
		"why": r["why"], "target": r["target"], "options": options, "chosen": r["chosen"],
	}
	if _, ok := sched["error"]; ok {
		out["schedule_error"] = true
	} else {
		out["schedule"] = sched["value"]
	}
	if _, ok := pen["error"]; ok {
		out["penalty_error"] = true
	} else {
		p := pen["value"].(map[string]any)
		lines := []any{}
		for _, l := range p["lines"].([]any) {
			l := l.(map[string]any)
			lines = append(lines, map[string]any{"installment": l["installment"], "due": l["due"],
				"shortfall": l["shortfall"], "days": l["days"], "total": l["total"]})
		}
		var outcome any
		if o, ok := p["waiver_outcome"].(map[string]any); ok {
			outcome = map[string]any{"eligible": o["eligible"], "refused": o["refused"], "undecided": o["undecided"], "reason": o["reason"]}
		}
		out["penalty"] = map[string]any{"total": p["total"], "lines": lines, "as_of": p["as_of"],
			"rated_through": p["rated_through"], "waiver_outcome": outcome, "if_waived": p["if_waived"], "summary": p["summary"]}
	}
	var questions, divergences []any
	for _, m := range []map[string]any{req, sched, pen} {
		for _, q := range listOf(m["required"]) {
			q := q.(map[string]any)
			questions = appendNew(questions, map[string]any{"position": q["position"], "because": q["because"],
				"question": q["question"], "effect": q["effect"]})
		}
		for _, d := range listOf(m["divergences"]) {
			divergences = appendNew(divergences, d)
		}
	}
	out["questions"], out["divergences"] = orEmpty(questions), orEmpty(divergences)
	return out
}

func listOf(v any) []any {
	l, _ := v.([]any)
	return l
}

func orEmpty(l []any) []any {
	if l == nil {
		return []any{}
	}
	return l
}

// appendNew appends v unless an equal entry is already there.
func appendNew(l []any, v any) []any {
	for _, x := range l {
		if jsonEqual(x, v) {
			return l
		}
	}
	return append(l, v)
}

// The answer's side.

func answerYear(j string, year int, ins []namedInputs, as []*returnsv1.EstimatedAnswer) cmpYear {
	// A year with no rules is one error for every household.
	noRules := len(as) > 0
	for _, a := range as {
		noRules = noRules && a.GetOutcome() == returnsv1.EstimatedOutcome_ESTIMATED_OUTCOME_NO_RULES && a.GetReason() == as[0].GetReason()
	}
	if noRules {
		return cmpYear{Error: as[0].GetReason()}
	}
	out := cmpYear{}
	for i, a := range as {
		h := answerHousehold(j, ins[i], a)
		if a.GetOutcome() == returnsv1.EstimatedOutcome_ESTIMATED_OUTCOME_COMPUTED {
			e := explanationCmp(a.GetExplanation())
			if out.Explanation == nil {
				out.Explanation = e
			} else if !jsonEqual(out.Explanation, e) {
				h["explanation"] = e
			}
		}
		out.Households = append(out.Households, h)
	}
	return out
}

func explanationCmp(e *returnsv1.EstimatedExplanation) any {
	return map[string]any{"summary": e.GetSummary(), "body": orEmpty(anys(e.GetParagraphs())),
		"versus_federal": e.GetVersusFederal(), "sources": orEmpty(anys(e.GetSources()))}
}

func anys(ss []string) []any {
	var out []any
	for _, s := range ss {
		out = append(out, s)
	}
	return out
}

func moneyCmp(m *commonv1.Money) any { return json.Number(strconv.FormatInt(m.GetMinor(), 10)) }

func dateCmp(d *commonv1.Date) string {
	if d == nil {
		return ""
	}
	return fmt.Sprintf("%04d-%02d-%02d", d.GetYear(), d.GetMonth(), d.GetDay())
}

func num(n int64) json.Number { return json.Number(strconv.FormatInt(n, 10)) }

var outcomeNames = map[returnsv1.EstimatedOutcome]string{
	returnsv1.EstimatedOutcome_ESTIMATED_OUTCOME_NO_RULES:    "no rules",
	returnsv1.EstimatedOutcome_ESTIMATED_OUTCOME_UNSUPPORTED: "unsupported",
	returnsv1.EstimatedOutcome_ESTIMATED_OUTCOME_REFUSED:     "refused",
}

var installmentNames = map[returnsv1.InstallmentStatus]string{
	returnsv1.InstallmentStatus_INSTALLMENT_STATUS_MET:         "met",
	returnsv1.InstallmentStatus_INSTALLMENT_STATUS_SHORT:       "short",
	returnsv1.InstallmentStatus_INSTALLMENT_STATUS_NOTHING_DUE: "nothing due",
	returnsv1.InstallmentStatus_INSTALLMENT_STATUS_EXCUSED:     "excused",
}

func answerHousehold(j string, in namedInputs, a *returnsv1.EstimatedAnswer) map[string]any {
	out := map[string]any{"name": in.name}
	if a.GetJurisdiction() != string(j) {
		out["jurisdiction"] = a.GetJurisdiction()
	}
	if a.GetOutcome() != returnsv1.EstimatedOutcome_ESTIMATED_OUTCOME_COMPUTED {
		name, ok := outcomeNames[a.GetOutcome()]
		if !ok {
			name = a.GetOutcome().String()
		}
		out["outcome"], out["error"] = name, a.GetReason()
		return out
	}
	if a.GetReason() != "" {
		out["reason"] = a.GetReason()
	}

	// The requirement.
	r := a.GetRequirement()
	options, chosen := []any{}, any(nil)
	for _, o := range r.GetOptions() {
		c := optionCmp(o)
		options = append(options, c)
		if o.GetStatus() == returnsv1.OptionStatus_OPTION_STATUS_CHOSEN {
			chosen = c
		}
	}
	status := r.GetStatus()
	out["requirement"] = map[string]any{
		"required": status == returnsv1.RequirementStatus_REQUIREMENT_STATUS_REQUIRED,
		"exempt":   status == returnsv1.RequirementStatus_REQUIREMENT_STATUS_EXEMPT,
		// COVERED is neither required, exempt nor unknown; anything else
		// unset here shows as a difference.
		"target_known": status != returnsv1.RequirementStatus_REQUIREMENT_STATUS_UNKNOWN && status != returnsv1.RequirementStatus_REQUIREMENT_STATUS_UNSPECIFIED,
		"why":          r.GetWhy(), "target": moneyCmp(r.GetTarget()), "options": options, "chosen": chosen,
	}

	// The schedule: an error in Go when no target could be measured.
	byNumber := map[int32]*returnsv1.EstimatedInstallment{}
	if status == returnsv1.RequirementStatus_REQUIREMENT_STATUS_UNKNOWN && len(a.GetInstallments()) == 0 {
		out["schedule_error"] = true
	} else {
		sched := []any{}
		for _, i := range a.GetInstallments() {
			byNumber[i.GetNumber()] = i
			st, ok := installmentNames[i.GetStatus()]
			if !ok {
				st = i.GetStatus().String()
			}
			sched = append(sched, map[string]any{"number": num(int64(i.GetNumber())), "due": dateCmp(i.GetDue()),
				"needed_by": moneyCmp(i.GetNeededBy()), "credited_by": moneyCmp(i.GetCreditedBy()),
				"shortfall": moneyCmp(i.GetShortfall()), "status": st})
		}
		out["schedule"] = sched
	}

	// The penalty.
	if a.GetPenalty() == nil {
		out["penalty_error"] = true
	} else {
		out["penalty"] = penaltyCmp(a.GetPenalty(), byNumber)
		if !proto.Equal(a.GetPenalty(), a.GetPenaltyWithoutPlanned()) {
			// Every payment is made by as_of, so the two are one.
			out["penalty_without_planned"] = penaltyCmp(a.GetPenaltyWithoutPlanned(), byNumber)
		}
	}
	if len(a.GetToMeet()) > 0 {
		// as_of is after every due date, so nothing is still to pay.
		out["to_meet"] = len(a.GetToMeet())
	}

	questions, divergences := []any{}, []any{}
	for _, q := range a.GetQuestions() {
		questions = append(questions, map[string]any{"position": q.GetPosition(), "because": q.GetBecause(),
			"question": q.GetQuestion(), "effect": q.GetEffect()})
	}
	for _, d := range a.GetDivergences() {
		uses := d.GetUses().String()
		switch d.GetUses() {
		case returnsv1.DivergenceReading_DIVERGENCE_READING_FORM:
			uses = "the form"
		case returnsv1.DivergenceReading_DIVERGENCE_READING_STATUTE:
			uses = "the statute"
		}
		divergences = append(divergences, map[string]any{"rule": d.GetRule(), "form": d.GetForm(),
			"form_source": d.GetFormSource(), "statute": d.GetStatute(), "statute_source": d.GetStatuteSource(),
			"uses": uses, "affects_household": d.GetAffectsHousehold()})
	}
	out["questions"], out["divergences"] = questions, divergences

	// This year's tax is known exactly when the inputs give it.
	given := false
	for _, cj := range in.inputs.GetEstimatedTax().GetCallerEstimate().GetJurisdictions() {
		given = given || (cj.GetJurisdiction() == string(j) && cj.Tax != nil)
	}
	if a.GetThisYearTaxKnown() != given {
		out["this_year_tax_known"] = a.GetThisYearTaxKnown()
	}
	return out
}

var optionID = regexp.MustCompile(`^(this-year|last-year)-([0-9]+)(?:\.([0-9]{2}))?$`)

// optionCmp is an option as the vector has it: its basis and percent come
// from its id.
func optionCmp(o *returnsv1.EstimatedTargetOption) map[string]any {
	basis, percent := "id "+o.GetId(), int64(-1)
	switch m := optionID.FindStringSubmatch(o.GetId()); {
	case o.GetBasis() == returnsv1.TargetBasis_TARGET_BASIS_EXCEPTION && strings.HasPrefix(o.GetId(), "exception-"):
		basis, percent = "an exception", 0
	case o.GetBasis() == returnsv1.TargetBasis_TARGET_BASIS_LAST_YEAR && o.GetId() == "last-year-income":
		basis, percent = "last year's income at this year's rate", 10_000
	case m != nil && (m[1] == "this-year") == (o.GetBasis() == returnsv1.TargetBasis_TARGET_BASIS_THIS_YEAR) &&
		(m[1] == "last-year") == (o.GetBasis() == returnsv1.TargetBasis_TARGET_BASIS_LAST_YEAR):
		whole, _ := strconv.ParseInt(m[2], 10, 64)
		frac, _ := strconv.ParseInt("0"+m[3], 10, 64)
		percent = whole*100 + frac
		basis = "this year's tax"
		if m[1] == "last-year" {
			basis = "last year's tax"
		}
	}
	return map[string]any{"basis": basis, "percent": num(percent), "amount": moneyCmp(o.GetAmount()),
		"available": o.GetStatus() != returnsv1.OptionStatus_OPTION_STATUS_UNAVAILABLE,
		"why_not":   o.GetWhyNot(), "label": o.GetLabel()}
}

func penaltyCmp(p *returnsv1.EstimatedPenalty, byNumber map[int32]*returnsv1.EstimatedInstallment) map[string]any {
	lines := []any{}
	for _, l := range p.GetInstalments() {
		due, days := "", any(nil)
		if i, ok := byNumber[l.GetNumber()]; ok {
			due = dateCmp(i.GetDue())
			days = num(daysBetween(i.GetDue(), l.GetPaidOn()))
		}
		lines = append(lines, map[string]any{"installment": num(int64(l.GetNumber())), "due": due,
			"shortfall": moneyCmp(l.GetShortfall()), "days": days, "total": moneyCmp(l.GetPenalty())})
	}
	var outcome any
	if o := p.GetWaiverOutcome(); o != returnsv1.WaiverOutcome_WAIVER_OUTCOME_UNSPECIFIED {
		outcome = map[string]any{"eligible": o == returnsv1.WaiverOutcome_WAIVER_OUTCOME_ELIGIBLE,
			"refused": o == returnsv1.WaiverOutcome_WAIVER_OUTCOME_REFUSED, "undecided": o == returnsv1.WaiverOutcome_WAIVER_OUTCOME_UNDECIDED,
			"reason": p.GetWaiverOutcomeReason()}
	}
	through := ""
	if p.GetUpperBound() {
		through = dateCmp(p.GetRatedThrough())
	} else if dateCmp(p.GetRatedThrough()) != dateCmp(p.GetFiguredTo()) {
		through = "rated through " + dateCmp(p.GetRatedThrough()) + " but no upper bound"
	}
	return map[string]any{"total": moneyCmp(p.GetTotal()), "lines": lines, "as_of": dateCmp(p.GetFiguredTo()),
		"rated_through": through, "waiver_outcome": outcome, "if_waived": moneyCmp(p.GetIfWaived()), "summary": p.GetSummary()}
}

// daysBetween is the number of days from a to b, as the vectors count them:
// April 15th to June 10th is 56. A missing date is the zero time.
func daysBetween(a, b *commonv1.Date) int64 {
	at := func(d *commonv1.Date) time.Time {
		if d == nil {
			return time.Time{}
		}
		return time.Date(int(d.GetYear()), time.Month(d.GetMonth()), int(d.GetDay()), 0, 0, 0, 0, time.UTC)
	}
	return int64(at(b).Sub(at(a)).Hours() / 24)
}
