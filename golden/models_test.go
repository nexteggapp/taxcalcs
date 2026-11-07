package golden

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/nexteggapp/taxcalcs/money"
	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear"
	"github.com/nexteggapp/taxcalcs/taxyear/internal/civil"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	"github.com/nexteggapp/taxcalcs/taxyear/model/account"
	"github.com/nexteggapp/taxcalcs/taxyear/model/broker"
	"github.com/nexteggapp/taxcalcs/taxyear/model/business"
	"github.com/nexteggapp/taxcalcs/taxyear/model/itemised"
	"github.com/nexteggapp/taxcalcs/taxyear/model/otherincome"
	"github.com/nexteggapp/taxcalcs/taxyear/model/retirement"
	"github.com/nexteggapp/taxcalcs/taxyear/model/taxpaid"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
)

// The model vectors: for every scenario of every taxyear test case, each
// model the packages under taxyear/model expand (all but the jobs), with
// what the package's Expand returns, as taxyear/models.go drives it.

// modelsContext is one scenario's year, as_of and facts, and its models.
// Scenarios with the same context share one, and a model is recorded once
// per context.
type modelsContext struct {
	Cases     []string      `json:"cases"`
	Year      int           `json:"year"`
	AsOf      string        `json:"as_of"`
	Facts     string        `json:"facts"`
	Error     string        `json:"error"`
	HsaLimits []hsaVector   `json:"hsa_limits"`
	Models    []modelVector `json:"models"`
}

// hsaVector is retirement.HsaLimit for one person.
type hsaVector struct {
	Person    string   `json:"person"`
	Limit     int64    `json:"limit"`
	Ok        bool     `json:"ok"`
	Questions []string `json:"questions"`
	LeftOut   []string `json:"left_out"`
}

// modelVector is one model's expansion. Projection is the package's
// projection; for tax paid it is Projection() straight after Expand, and
// Spread the projection after Open and Spread with the federal due dates.
type modelVector struct {
	List       string          `json:"list"`
	ID         string          `json:"id"`
	Model      string          `json:"model"`
	Projection string          `json:"projection"`
	Error      string          `json:"error"`
	Questions  []string        `json:"questions"`
	LeftOut    []string        `json:"left_out"`
	PlanPart   string          `json:"plan_part,omitempty"`
	Payments   []paymentVector `json:"payments,omitempty"`
	Open       [][3]int        `json:"open,omitempty"`
	Spread     string          `json:"spread,omitempty"`
}

// paymentVector is one taxpaid.Payment.
type paymentVector struct {
	On           [3]int `json:"on"`
	Amount       int64  `json:"amount"`
	FromLastYear bool   `json:"from_last_year"`
	Made         bool   `json:"made"`
}

func ymd(d civil.Date) [3]int { return [3]int{d.Year(), int(d.Month()), d.Day()} }

func notesOf(t *testing.T, n *model.Notes) ([]string, []string) {
	qs := []string{}
	for _, q := range n.Questions() {
		qs = append(qs, encode(t, q.Proto()))
	}
	left := append([]string{}, n.LeftOut()...)
	return qs, left
}

// spreadTotal is the rest of the year's target Spread is given: odd cents,
// so the shares and the rounding both show.
const spreadTotal = money.USCents(1_234_567)

func TestModels(t *testing.T) {
	var files []string
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
	var out []*modelsContext
	byKey := map[string]*modelsContext{}
	seen := map[string]bool{}
	for _, path := range files {
		text, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		file, err := taxyear.Parse(text)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		resolved, _ := taxyear.Resolve(file)
		for _, r := range resolved {
			name := filepath.Base(filepath.Dir(path)) + "/" + filepath.Base(path) + "#" + r.Scenario().GetId()
			in := r.Inputs()
			asOf, facts := encode(t, file.GetAsOf()), encode(t, in.GetFacts())
			key := strings.Join([]string{string(rune(file.GetTaxYear())), asOf, facts}, "|")
			c := byKey[key]
			if c == nil {
				c = &modelsContext{Year: int(file.GetTaxYear()), AsOf: asOf, Facts: facts, HsaLimits: []hsaVector{}, Models: []modelVector{}}
				byKey[key] = c
				out = append(out, c)
				ctx, err := model.NewContext(int(file.GetTaxYear()), file.GetAsOf(), in.GetFacts())
				c.Error = errText(err)
				if err == nil {
					for _, p := range ctx.Facts().GetPeople() {
						n := &model.Notes{}
						l, ok := retirement.HsaLimit(ctx, p.GetId(), n)
						qs, left := notesOf(t, n)
						c.HsaLimits = append(c.HsaLimits, hsaVector{Person: p.GetId(), Limit: int64(l), Ok: ok, Questions: qs, LeftOut: left})
					}
				}
			}
			c.Cases = append(c.Cases, name)
			if c.Error != "" {
				continue
			}
			ctx, _ := model.NewContext(int(file.GetTaxYear()), file.GetAsOf(), in.GetFacts())
			for _, v := range expandAll(t, ctx, in.GetModel()) {
				k := key + "|" + v.List + "|" + v.Model
				if seen[k] {
					continue
				}
				seen[k] = true
				c.Models = append(c.Models, v)
			}
		}
	}
	check(t, "models/models.json", out)
}

// expandAll expands every model but the jobs.
func expandAll(t *testing.T, ctx *model.Context, m *scenariosv1.TaxYearModel) []modelVector {
	var out []modelVector
	add := func(list, id string, mm proto.Message, p proto.Message, n *model.Notes, err error) *modelVector {
		v := modelVector{List: list, ID: id, Model: encode(t, mm), Error: errText(err), Questions: []string{}, LeftOut: []string{}}
		if err == nil {
			v.Projection = encode(t, p)
			if n != nil {
				v.Questions, v.LeftOut = notesOf(t, n)
			}
		}
		out = append(out, v)
		return &out[len(out)-1]
	}
	for _, b := range m.GetBusinesses() {
		p, n, err := business.Expand(ctx, b)
		add("businesses", b.GetId(), b, p, n, err)
	}
	for _, a := range m.GetAccounts() {
		p, n, err := account.Expand(ctx, a)
		add("accounts", a.GetId(), a, p, n, err)
	}
	for _, b := range m.GetBrokers() {
		p, n, err := broker.Expand(ctx, b)
		add("brokers", b.GetId(), b, p, n, err)
	}
	for _, it := range m.GetItemised() {
		p, n, err := itemised.Expand(ctx, it)
		add("itemised", it.GetId(), it, p, n, err)
	}
	for _, o := range m.GetOtherIncome() {
		p, n, err := otherincome.Expand(ctx, o)
		add("other_income", o.GetId(), o, p, n, err)
	}
	for _, r := range m.GetRetirement() {
		p, err := retirement.Expand(ctx, r)
		v := add("retirement", r.GetId(), r, p, nil, err)
		if part := retirement.PlanPart(r); part != nil {
			v.PlanPart = part.GetId()
		}
	}
	for _, tp := range withAdjacent(ctx, m.GetTaxPaid()) {
		p, err := taxpaid.Expand(ctx, tp, ctx.Facts().GetAdjacentPayments())
		if err != nil {
			add("tax_paid", tp.GetId(), tp, nil, nil, err)
			continue
		}
		v := add("tax_paid", tp.GetId(), tp, p.Projection(), p.Notes(), nil)
		for _, pay := range p.Payments() {
			v.Payments = append(v.Payments, paymentVector{On: ymd(pay.On()), Amount: int64(pay.Amount()), FromLastYear: pay.FromLastYear(), Made: pay.Made()})
		}
		y := ctx.Year()
		dues := []civil.Date{civil.Must(y, time.April, 15), civil.Must(y, time.June, 15), civil.Must(y, time.September, 15), civil.Must(y+1, time.January, 15)}
		open := p.Open(dues)
		for _, d := range open {
			v.Open = append(v.Open, ymd(d))
		}
		p.Spread(open, spreadTotal)
		v.Spread = encode(t, p.Projection())
	}
	return out
}

// withAdjacent is taxyear's: the tax paid models, and an empty one for each
// jurisdiction an adjacent year's payment for this year was made to that
// has no model.
func withAdjacent(ctx *model.Context, ms []*scenariosv1.TaxPaidModel) []*scenariosv1.TaxPaidModel {
	has := map[string]bool{}
	for _, t := range ms {
		has[t.GetJurisdiction()] = true
	}
	out := ms
	for _, a := range ctx.Facts().GetAdjacentPayments() {
		j := a.GetJurisdiction()
		if has[j] || !taxpaid.ForYear(a, ctx.Year()) {
			continue
		}
		has[j] = true
		out = append(out[:len(out):len(out)], &scenariosv1.TaxPaidModel{Id: "tax-paid-" + strings.ToLower(j), Jurisdiction: j})
	}
	return out
}

// civilVector is one date's arithmetic, or the error New gives.
type civilVector struct {
	In       [3]int `json:"in"`
	Error    string `json:"error"`
	String   string `json:"string"`
	Long     string `json:"long"`
	Short    string `json:"short"`
	Weekday  int    `json:"weekday"`
	Plus     [3]int `json:"plus_40"`
	Minus    [3]int `json:"minus_400"`
	EndMonth [3]int `json:"end_of_month"`
	Business [3]int `json:"business_day"`
	MonthOf  [3]int `json:"month_of"`
	Proto    string `json:"proto"`
}

type pairVector struct {
	A       [3]int `json:"a"`
	B       [3]int `json:"b"`
	Days    int    `json:"days_from"`
	Compare int    `json:"compare"`
	Later   [3]int `json:"later"`
}

type cadenceVector struct {
	Dates   [][3]int `json:"dates"`
	Months  int      `json:"months"`
	Next    [3]int   `json:"next"`
	Through [][3]int `json:"through"`
}

type helpersVector struct {
	Civil      []civilVector   `json:"civil"`
	Pairs      []pairVector    `json:"pairs"`
	Cadences   []cadenceVector `json:"cadences"`
	Shares     [][]int64       `json:"shares"`
	Scales     [][4]int64      `json:"scales"`
	Dollars    [][2]int64      `json:"dollars"`
	Percents   [][4]int64      `json:"percents"`
	FormsDue   [][4]int        `json:"forms_due"`
	Rounds     [][]int64       `json:"rounds"`
	Contexts   []ctxVector     `json:"contexts"`
	FromProtos []fromProto     `json:"from_protos"`
}

type fromProto struct {
	In    [3]int `json:"in"`
	Nil   bool   `json:"nil"`
	Out   [3]int `json:"out"`
	Zero  bool   `json:"zero"`
	Error string `json:"error"`
}

type ctxVector struct {
	Year     int      `json:"year"`
	AsOf     [3]int   `json:"as_of"`
	Facts    string   `json:"facts"`
	Error    string   `json:"error"`
	Through  [3]int   `json:"evidence_through"`
	Months   int      `json:"evidence_months"`
	Finished bool     `json:"finished"`
	LivedIn  []string `json:"lived_in"`
	Ages     []int    `json:"ages"`
}

func TestModelHelpers(t *testing.T) {
	var h helpersVector
	for _, in := range [][3]int{{2026, 1, 5}, {2026, 12, 31}, {999, 10, 10}, {2024, 2, 29}, {2025, 2, 29}, {2026, 13, 1},
		{2026, 0, 1}, {2026, 4, 31}, {2026, 8, 14}, {2026, 8, 15}, {2026, 8, 16}, {2000, 2, 29}, {1900, 2, 29}, {1, 1, 1}, {1, 1, 2},
		{0, 0, 0}, {2027, 1, 31}, {2031, 3, 1}, {-1, 6, 1}, {10000, 1, 1}} {
		v := civilVector{In: in}
		d, err := civil.New(in[0], time.Month(in[1]), in[2])
		v.Error = errText(err)
		if err == nil {
			v.String, v.Long, v.Short, v.Weekday = d.String(), d.Long(), d.Short(), int(d.Weekday())
			v.Plus, v.Minus = ymd(d.AddDays(40)), ymd(d.AddDays(-400))
			v.EndMonth = ymd(civil.EndOfMonth(d.Year(), d.Month()))
			v.Business, v.MonthOf = ymd(d.BusinessDayOnOrBefore()), ymd(model.MonthOf(d))
			v.Proto = encode(t, d.Proto())
		}
		h.Civil = append(h.Civil, v)
	}
	for _, in := range [][3]int{{2026, 3, 4}, {0, 0, 0}, {2026, 2, 30}, {2026, 0, 0}, {0, 1, 1}} {
		v := fromProto{In: in}
		d, err := civil.FromProto(&commonv1.Date{Year: int32(in[0]), Month: int32(in[1]), Day: int32(in[2])})
		v.Error, v.Out, v.Zero = errText(err), ymd(d), d.IsZero()
		h.FromProtos = append(h.FromProtos, v)
	}
	d, err := civil.FromProto(nil)
	h.FromProtos = append(h.FromProtos, fromProto{Nil: true, Out: ymd(d), Zero: d.IsZero(), Error: errText(err)})
	dates := []civil.Date{civil.Must(2026, 1, 1), civil.Must(2026, 3, 15), civil.Must(2025, 12, 31), civil.Must(1, 1, 1),
		civil.Must(2026, 3, 15), civil.Must(1500, 6, 1), {}}
	for _, a := range dates {
		for _, b := range dates {
			h.Pairs = append(h.Pairs, pairVector{A: ymd(a), B: ymd(b), Days: a.DaysFrom(b), Compare: a.Compare(b), Later: ymd(civil.Later(a, b))})
		}
	}
	for _, ds := range [][][3]int{
		{}, {{2026, 3, 1}},
		{{2026, 1, 31}, {2026, 2, 28}, {2026, 3, 31}},
		{{2026, 3, 31}, {2026, 1, 31}, {2026, 2, 28}, {2026, 4, 30}},
		{{2026, 1, 15}, {2026, 4, 15}, {2026, 7, 15}},
		{{2026, 1, 2}, {2026, 1, 16}, {2026, 1, 30}, {2026, 2, 13}},
		{{2026, 1, 2}, {2026, 1, 9}, {2026, 1, 16}},
		{{2026, 1, 31}, {2026, 7, 31}},
		{{2026, 1, 30}, {2026, 2, 28}, {2026, 3, 30}},
		{{2026, 1, 5}, {2026, 1, 5}, {2026, 1, 5}},
		{{2025, 11, 30}, {2026, 5, 31}},
		{{2026, 1, 10}, {2026, 1, 20}, {2026, 2, 25}, {2026, 3, 1}, {2026, 6, 1}},
	} {
		var in []civil.Date
		v := cadenceVector{Dates: [][3]int{}, Through: [][3]int{}}
		for _, x := range ds {
			in = append(in, civil.Must(x[0], time.Month(x[1]), x[2]))
			v.Dates = append(v.Dates, x)
		}
		c := model.CadenceOf(in)
		v.Months = c.Months()
		last := civil.Must(2026, 1, 31)
		if len(in) > 0 {
			last = in[len(in)-1]
		}
		v.Next = ymd(c.Next(last))
		for _, d := range c.Through(last, civil.Must(2026, 12, 31)) {
			v.Through = append(v.Through, ymd(d))
		}
		h.Cadences = append(h.Cadences, v)
	}
	for _, s := range [][2]int64{{100, 3}, {-100, 3}, {0, 4}, {1_234_567, 12}, {-1_234_567, 12}, {5, 7}, {7, 1}} {
		row := []int64{s[0], s[1]}
		for _, v := range model.Share(money.USCents(s[0]), int(s[1])) {
			row = append(row, int64(v))
		}
		h.Shares = append(h.Shares, row)
	}
	for _, s := range [][3]int64{{100, 1, 3}, {200, 1, 3}, {-200, 1, 3}, {150, 1, 2}, {-150, 1, 2}, {150, -1, 2}, {150, 1, -2},
		{-150, -1, -2}, {99, 7, 0}, {1_000_000, 123_456, 1_000_000}, {12_345_678, 9, 10}, {12_345_678, 11, 10}} {
		h.Scales = append(h.Scales, [4]int64{s[0], s[1], s[2], int64(model.Scale(money.USCents(s[0]), money.USCents(s[1]), money.USCents(s[2])))})
	}
	for _, c := range []int64{0, 49, 50, 51, 149, 150, -49, -50, -51, -150, 123_456} {
		h.Dollars = append(h.Dollars, [2]int64{c, int64(model.Dollars(money.USCents(c)))})
	}
	for _, q := range [][2]int64{{6, 0}, {45, 1}, {4567, 3}, {5, 0}, {123456789, 8}, {-25, 1}, {7, -1}} {
		r, ok := model.Percent(&commonv1.Quantity{Units: q[0], Scale: int32(q[1])})
		okv := int64(0)
		if ok {
			okv = 1
		}
		h.Percents = append(h.Percents, [4]int64{q[0], q[1], int64(r), okv})
	}
	if _, ok := model.Percent(nil); ok {
		t.Fatal("Percent(nil) is set")
	}
	for y := 2020; y <= 2036; y++ {
		d := model.FormsDue(y)
		h.FormsDue = append(h.FormsDue, [4]int{y, d.Year(), int(d.Month()), d.Day()})
	}
	for _, run := range [][]int64{{583_333, 583_333, 583_334}, {49, 49, 49, 49}, {-149, 50, 50, 99}, {150, -150, 150}} {
		r := model.NewRounder()
		row := []int64{}
		for _, v := range run {
			row = append(row, v, int64(r.Round("line", money.USCents(v))))
		}
		h.Rounds = append(h.Rounds, row)
	}
	h.Contexts = contextVectors(t)
	check(t, "models/helpers.json", h)
}

// contextVectors is each test case's context read back, and some contexts
// NewContext refuses.
func contextVectors(t *testing.T) []ctxVector {
	type in struct {
		year  int
		asOf  *commonv1.Date
		facts string
	}
	var ins []in
	for _, y := range []int{1899, 10000} {
		ins = append(ins, in{year: y, asOf: &commonv1.Date{Year: 2026, Month: 1, Day: 1}})
	}
	ins = append(ins, in{year: 2026}, in{year: 2026, asOf: &commonv1.Date{Year: 2026, Month: 2, Day: 30}})
	for _, path := range []string{"../taxyear/testdata/cases/full-2026.jsonc", "../taxyear/testdata/cases/moved-2026.jsonc",
		"../taxyear/testdata/cases/full-2025.jsonc", "../taxyear/testdata/cases/simple-2026.jsonc"} {
		text, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		file, err := taxyear.Parse(text)
		if err != nil {
			t.Fatal(err)
		}
		f := encode(t, file.GetShared().GetFacts())
		for _, a := range []*commonv1.Date{file.GetAsOf(), {Year: file.GetTaxYear(), Month: 1, Day: 30},
			{Year: file.GetTaxYear(), Month: 1, Day: 31}, {Year: file.GetTaxYear() + 1, Month: 1, Day: 1}} {
			ins = append(ins, in{year: int(file.GetTaxYear()), asOf: a, facts: f})
		}
	}
	var out []ctxVector
	for _, c := range ins {
		v := ctxVector{Year: c.year, Facts: c.facts, LivedIn: []string{}, Ages: []int{}}
		if c.asOf != nil {
			v.AsOf = [3]int{int(c.asOf.GetYear()), int(c.asOf.GetMonth()), int(c.asOf.GetDay())}
		}
		facts := decodeFacts(t, c.facts)
		ctx, err := model.NewContext(c.year, c.asOf, facts)
		v.Error = errText(err)
		if err == nil {
			v.Through, v.Months, v.Finished = ymd(ctx.EvidenceThrough()), ctx.EvidenceMonths(), ctx.Finished()
			for d := ctx.YearStart().AddDays(-3); !d.After(ctx.YearEnd().AddDays(3)); d = d.AddDays(5) {
				v.LivedIn = append(v.LivedIn, ctx.LivedIn(d))
			}
			for _, p := range ctx.Facts().GetPeople() {
				age, ok := ctx.AgeAtYearEnd(p.GetId())
				if !ok {
					age = -1
				}
				v.Ages = append(v.Ages, age)
			}
		}
		out = append(out, v)
	}
	return out
}

// decodeFacts is the facts a vector's base64 holds, or nil for "".
func decodeFacts(t *testing.T, b64 string) *factsv1.TaxYearFacts {
	if b64 == "" {
		return nil
	}
	b, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatal(err)
	}
	f := &factsv1.TaxYearFacts{}
	if err := proto.Unmarshal(b, f); err != nil {
		t.Fatal(err)
	}
	return f
}
