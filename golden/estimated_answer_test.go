package golden

// The taxlisp estimated tax answers, held to the Go vectors.
//
// TestEstimatedTaxlisp prepares every household in a landed jurisdiction's
// inputs file with the federal engine, and compares the answers with
// testdata/estimated/<code>.json.gz. Both sides are first put in one
// comparable shape, cmpYear: the vector's parts the proto answer also
// holds, and nothing else.
//
// TestEstimatedAnswerMapping checks that shape is right before any
// jurisdiction lands: it converts each Go answer to an EstimatedAnswer, as
// taxlisp would give it, and compares that with the vector the same way.
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
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/estimated/all"
	"github.com/nexteggapp/taxcalcs/estimated/estimatedtest"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	"github.com/nexteggapp/taxcalcs/returns/rules/states"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	"github.com/nexteggapp/taxcalcs/taxlisp/eval"
	"github.com/nexteggapp/taxcalcs/taxlisp/expand"
	"github.com/nexteggapp/taxcalcs/taxlisp/parser"
	"github.com/nexteggapp/taxcalcs/taxlisp/types"
	taxlispv1 "github.com/nexteggapp/taxcalcs/taxlisp/v1"
	"github.com/nexteggapp/taxcalcs/working"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// estimatedLanded are the jurisdictions whose taxlisp estimated tax rules
// have landed, and whose answers must match their vectors. A jurisdiction
// joins when its file matches.
var estimatedLanded = []estimated.Jurisdiction{
	estimated.Federal,
	estimated.Alabama,
	estimated.Colorado,
	estimated.Delaware,
	estimated.Iowa,
	estimated.Kansas,
	estimated.Kentucky,
	estimated.Minnesota,
	estimated.Montana,
	estimated.NorthCarolina,
	estimated.Nebraska,
	estimated.NewMexico,
	estimated.Oklahoma,
	estimated.Oregon,
	estimated.RhodeIsland,
	estimated.SouthCarolina,
	estimated.WestVirginia,
	referenceCode,
}

func TestEstimatedTaxlisp(t *testing.T) {
	if len(estimatedLanded) == 0 {
		return
	}
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
// shared machinery: the rules newReference writes in Go.
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

// TestEstimatedAnswerMapping converts every Go answer to an EstimatedAnswer
// and compares it with the vector as TestEstimatedTaxlisp does, so the
// comparison is known to lose nothing the proto holds before taxlisp gives
// any answer.
func TestEstimatedAnswerMapping(t *testing.T) {
	codes := append(estimated.Jurisdictions(), referenceCode)
	for _, j := range codes {
		t.Run(string(j), func(t *testing.T) {
			rulesFor := func(year int) (estimated.EstimatedTax, error) { return all.For(j, year) }
			if j == referenceCode {
				rulesFor = newReference
			}
			ins, err := readInputs(j)
			if err != nil {
				t.Fatal(err)
			}
			hs := estimatedtest.Households(j)
			answers := map[int][]*returnsv1.EstimatedAnswer{}
			for _, year := range estimatedYears {
				rules, yearErr := rulesFor(year)
				var as []estimatedtest.Answers
				if yearErr == nil {
					as = estimatedtest.Ask(rules, hs)
				}
				for i, h := range hs {
					var a *returnsv1.EstimatedAnswer
					if yearErr != nil {
						a = refusedAnswer(j, yearErr)
					} else {
						a = goAnswer(j, as[i], rules.Explanation())
					}
					// Through the wire, as Prepare's answers come.
					b, err := proto.Marshal(a)
					if err != nil {
						t.Fatal(err)
					}
					back := &returnsv1.EstimatedAnswer{}
					if err := proto.Unmarshal(b, back); err != nil {
						t.Fatal(err)
					}
					answers[year] = append(answers[year], back)
					if got := ins[year][i].name; got != h.Name() {
						t.Fatalf("%d: inputs household %d is %q, want %q", year, i, got, h.Name())
					}
				}
			}
			compareWithVector(t, j, ins, answers)
		})
	}

	// The Facts no proto holds, for the record.
	files := map[estimated.Jurisdiction]map[string]any{}
	for _, j := range codes {
		f, err := estimatedInputsFile(j)
		if err != nil {
			t.Fatal(err)
		}
		files[j] = f
	}
	t.Logf("Facts with no place in the protos: %s", strings.Join(unmappedPaths(files), ", "))
}

// compareWithVector compares answers, per year in the order of the inputs
// file's households, with j's vector.
func compareWithVector(t *testing.T, j estimated.Jurisdiction, ins map[int][]namedInputs, answers map[int][]*returnsv1.EstimatedAnswer) {
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

// outcomeOf is the outcome a Go error stands for.
func outcomeOf(text string) string {
	switch {
	case strings.HasPrefix(text, estimated.ErrNoRules.Error()):
		return "no rules"
	case strings.HasPrefix(text, estimated.ErrUnsupported.Error()):
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

func answerYear(j estimated.Jurisdiction, year int, ins []namedInputs, as []*returnsv1.EstimatedAnswer) cmpYear {
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
	returnsv1.InstallmentStatus_INSTALLMENT_STATUS_MET:         estimated.StatusMet.String(),
	returnsv1.InstallmentStatus_INSTALLMENT_STATUS_SHORT:       estimated.StatusShort.String(),
	returnsv1.InstallmentStatus_INSTALLMENT_STATUS_NOTHING_DUE: estimated.StatusNothingDue.String(),
	returnsv1.InstallmentStatus_INSTALLMENT_STATUS_EXCUSED:     estimated.StatusExcused.String(),
}

func answerHousehold(j estimated.Jurisdiction, in namedInputs, a *returnsv1.EstimatedAnswer) map[string]any {
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
			uses = working.ReadingForm.String()
		case returnsv1.DivergenceReading_DIVERGENCE_READING_STATUTE:
			uses = working.ReadingStatute.String()
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
		basis, percent = estimated.BasisException.String(), 0
	case o.GetBasis() == returnsv1.TargetBasis_TARGET_BASIS_LAST_YEAR && o.GetId() == "last-year-income":
		basis, percent = estimated.BasisPriorYearIncome.String(), 10_000
	case m != nil && (m[1] == "this-year") == (o.GetBasis() == returnsv1.TargetBasis_TARGET_BASIS_THIS_YEAR) &&
		(m[1] == "last-year") == (o.GetBasis() == returnsv1.TargetBasis_TARGET_BASIS_LAST_YEAR):
		whole, _ := strconv.ParseInt(m[2], 10, 64)
		frac, _ := strconv.ParseInt("0"+m[3], 10, 64)
		percent = whole*100 + frac
		basis = estimated.BasisThisYearTax.String()
		if m[1] == "last-year" {
			basis = estimated.BasisPriorYearTax.String()
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
			days = num(int64(fromProto(l.GetPaidOn()).DaysSince(fromProto(i.GetDue()))))
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

func fromProto(d *commonv1.Date) estimated.Date {
	if d == nil {
		return estimated.Date{}
	}
	out, err := estimated.NewDate(int(d.GetYear()), time.Month(d.GetMonth()), int(d.GetDay()))
	if err != nil {
		return estimated.Date{}
	}
	return out
}

// The Go answers as EstimatedAnswers: what taxlisp is to give.

func refusedAnswer(j estimated.Jurisdiction, err error) *returnsv1.EstimatedAnswer {
	outcome := returnsv1.EstimatedOutcome_ESTIMATED_OUTCOME_REFUSED
	switch {
	case errors.Is(err, estimated.ErrNoRules):
		outcome = returnsv1.EstimatedOutcome_ESTIMATED_OUTCOME_NO_RULES
	case errors.Is(err, estimated.ErrUnsupported):
		outcome = returnsv1.EstimatedOutcome_ESTIMATED_OUTCOME_UNSUPPORTED
	}
	return &returnsv1.EstimatedAnswer{Jurisdiction: string(j), Outcome: outcome, Reason: err.Error()}
}

// goAnswer is one household's Go answers as an EstimatedAnswer.
func goAnswer(j estimated.Jurisdiction, a estimatedtest.Answers, e estimated.Explanation) *returnsv1.EstimatedAnswer {
	req, reqErr := a.Requirement()
	if reqErr != nil {
		return refusedAnswer(j, reqErr)
	}
	sched, schedErr := a.Schedule()
	pen, penErr := a.Penalty()
	_, taxKnown := a.Household().Facts().ThisYear().Tax()
	out := &returnsv1.EstimatedAnswer{
		Jurisdiction:     string(j),
		Outcome:          returnsv1.EstimatedOutcome_ESTIMATED_OUTCOME_COMPUTED,
		Requirement:      requirementProto(req.Value()),
		Explanation:      &returnsv1.EstimatedExplanation{Summary: e.Summary(), Paragraphs: e.Body(), VersusFederal: e.VersusFederal(), Sources: e.Sources()},
		ThisYearTaxKnown: taxKnown,
	}
	if schedErr == nil {
		for _, i := range sched.Value() {
			out.Installments = append(out.Installments, &returnsv1.EstimatedInstallment{
				Number: int32(i.Number()), Due: protoDate(i.Due()), NeededBy: cents(i.NeededBy()),
				CreditedBy: cents(i.CreditedBy()), Shortfall: cents(i.Shortfall()), Status: installmentProto(i.Status()),
			})
		}
	}
	if penErr == nil {
		out.Penalty = penaltyProto(pen.Value())
		out.PenaltyWithoutPlanned = penaltyProto(pen.Value())
	}
	var seenQ []*returnsv1.EstimatedQuestion
	var seenD []*returnsv1.EstimatedDivergence
	add := func(rs []working.Required, ds []working.Divergence) {
		for _, r := range rs {
			q := &returnsv1.EstimatedQuestion{Position: r.Position().String(), Because: r.Because(), Question: r.Question(), Effect: r.Effect()}
			if !containsProto(seenQ, q) {
				seenQ = append(seenQ, q)
			}
		}
		for _, d := range ds {
			uses := returnsv1.DivergenceReading_DIVERGENCE_READING_FORM
			if d.Uses() == working.ReadingStatute {
				uses = returnsv1.DivergenceReading_DIVERGENCE_READING_STATUTE
			}
			p := &returnsv1.EstimatedDivergence{Rule: d.Rule(), Form: d.Form(), FormSource: d.FormSource(), Statute: d.Statute(),
				StatuteSource: d.StatuteSource(), Uses: uses, AffectsHousehold: d.AffectsHousehold()}
			if !containsProto(seenD, p) {
				seenD = append(seenD, p)
			}
		}
	}
	add(req.Required(), req.Divergences())
	if schedErr == nil {
		add(sched.Required(), sched.Divergences())
	}
	if penErr == nil {
		add(pen.Required(), pen.Divergences())
	}
	out.Questions, out.Divergences = seenQ, seenD
	return out
}

func containsProto[M proto.Message](l []M, m M) bool {
	for _, x := range l {
		if proto.Equal(x, m) {
			return true
		}
	}
	return false
}

func requirementProto(r estimated.Requirement) *returnsv1.EstimatedRequirement {
	status := returnsv1.RequirementStatus_REQUIREMENT_STATUS_COVERED
	switch {
	case r.Required():
		status = returnsv1.RequirementStatus_REQUIREMENT_STATUS_REQUIRED
	case r.Exempt():
		status = returnsv1.RequirementStatus_REQUIREMENT_STATUS_EXEMPT
	case !r.TargetKnown():
		status = returnsv1.RequirementStatus_REQUIREMENT_STATUS_UNKNOWN
	}
	out := &returnsv1.EstimatedRequirement{Status: status, Why: r.Why(), Target: cents(r.Target())}
	chosen, hasChosen := r.Chosen()
	chosenAt := -1
	for i, o := range r.Options() {
		if hasChosen && reflect.DeepEqual(o, chosen) {
			chosenAt = i
		}
	}
	for i, o := range r.Options() {
		os := returnsv1.OptionStatus_OPTION_STATUS_AVAILABLE
		switch {
		case i == chosenAt:
			os = returnsv1.OptionStatus_OPTION_STATUS_CHOSEN
		case !o.Available():
			os = returnsv1.OptionStatus_OPTION_STATUS_UNAVAILABLE
		}
		out.Options = append(out.Options, &returnsv1.EstimatedTargetOption{
			Id: optionIDOf(o), Label: o.Label(), Basis: basisProto(o.Basis()), Amount: cents(o.Amount()), Status: os, WhyNot: o.WhyNot(),
		})
	}
	return out
}

// optionIDOf is an option's id: this-year-<percent>, last-year-<percent>,
// last-year-income or exception-<label, as words joined by hyphens>.
func optionIDOf(o estimated.TargetOption) string {
	pct := func(r money.Rate) string {
		if r%100 == 0 {
			return strconv.Itoa(int(r / 100))
		}
		return fmt.Sprintf("%d.%02d", r/100, r%100)
	}
	switch o.Basis() {
	case estimated.BasisThisYearTax:
		return "this-year-" + pct(o.Percent())
	case estimated.BasisPriorYearTax:
		return "last-year-" + pct(o.Percent())
	case estimated.BasisPriorYearIncome:
		return "last-year-income"
	}
	words := regexp.MustCompile(`[^a-z0-9]+`).Split(strings.ToLower(o.Label()), -1)
	return "exception-" + strings.Trim(strings.Join(words, "-"), "-")
}

func basisProto(b estimated.Basis) returnsv1.TargetBasis {
	switch b {
	case estimated.BasisThisYearTax:
		return returnsv1.TargetBasis_TARGET_BASIS_THIS_YEAR
	case estimated.BasisPriorYearTax, estimated.BasisPriorYearIncome:
		return returnsv1.TargetBasis_TARGET_BASIS_LAST_YEAR
	case estimated.BasisException:
		return returnsv1.TargetBasis_TARGET_BASIS_EXCEPTION
	}
	return returnsv1.TargetBasis_TARGET_BASIS_UNSPECIFIED
}

func installmentProto(s estimated.Status) returnsv1.InstallmentStatus {
	for p, name := range installmentNames {
		if name == s.String() {
			return p
		}
	}
	return returnsv1.InstallmentStatus_INSTALLMENT_STATUS_UNSPECIFIED
}

func penaltyProto(p estimated.Penalty) *returnsv1.EstimatedPenalty {
	out := &returnsv1.EstimatedPenalty{
		Total: cents(p.Total()), FiguredTo: protoDate(p.AsOf()), Summary: p.Summary(), IfWaived: cents(p.IfWaived()),
		UpperBound: !p.RatedThrough().IsZero(), RatedThrough: protoDate(p.AsOf()),
	}
	if out.UpperBound {
		out.RatedThrough = protoDate(p.RatedThrough())
	}
	if o, ok := p.WaiverOutcome(); ok {
		switch {
		case o.Eligible():
			out.WaiverOutcome = returnsv1.WaiverOutcome_WAIVER_OUTCOME_ELIGIBLE
		case o.Refused():
			out.WaiverOutcome = returnsv1.WaiverOutcome_WAIVER_OUTCOME_REFUSED
		case o.Undecided():
			out.WaiverOutcome = returnsv1.WaiverOutcome_WAIVER_OUTCOME_UNDECIDED
		}
		out.WaiverOutcomeReason = o.Reason()
	}
	for _, l := range p.Lines() {
		out.Instalments = append(out.Instalments, &returnsv1.EstimatedPenalty_InstalmentPenalty{
			Number: int32(l.Installment()), Shortfall: cents(l.Shortfall()), PaidOn: protoDate(l.Due().AddDays(l.Days())), Penalty: cents(l.Total()),
		})
	}
	return out
}
