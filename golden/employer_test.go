package golden

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear"
	"github.com/nexteggapp/taxcalcs/taxyear/internal/civil"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	"github.com/nexteggapp/taxcalcs/taxyear/model/employer"
	"github.com/nexteggapp/taxcalcs/taxyear/model/retirement"
	"github.com/nexteggapp/taxcalcs/taxyear/payroll"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
)

// The employer vectors (the scenario's employer and retirement models stand
// for its model, the rest of which the jobs never read): for every scenario of every taxyear test case, at
// its own as_of and at a few others, the jobs driven as taxyear's project
// drives them (Expand each, MaxOut for each retirement plan part, HoldHsa
// for each person with a known HSA limit, then Projection), with every
// result taxyear takes from them; and the payroll functions over a sweep.

// employerScenario is one scenario's context and model, and what the jobs
// gave. A step's error stops the steps after it, as it stops project.
type employerScenario struct {
	Cases      []string           `json:"cases"`
	Year       int                `json:"year"`
	AsOf       string             `json:"as_of"`
	Facts      string             `json:"facts"`
	Employers  []string           `json:"employers"`
	Retirement []string           `json:"retirement"`
	Overrides  []string           `json:"overrides"`
	Error      string             `json:"error"`
	PayDates   []payDatesVector   `json:"pay_dates"`
	Expand     []expandVector     `json:"expand"`
	MaxOut     []maxOutVector     `json:"max_out"`
	HoldHsa    []holdHsaVector    `json:"hold_hsa"`
	Jobs       []jobProjectVector `json:"jobs"`
}

// payDatesVector is employer.PayDates for one employer model, or why its
// dates can't be read.
type payDatesVector struct {
	ID    string   `json:"id"`
	Error string   `json:"error"`
	Dates [][3]int `json:"dates"`
}

type expandVector struct {
	ID     string `json:"id"`
	Person string `json:"person"`
	Error  string `json:"error"`
}

type maxOutVector struct {
	Retirement string   `json:"retirement"`
	Person     string   `json:"person"`
	Part       string   `json:"part"`
	Error      string   `json:"error"`
	Questions  []string `json:"questions"`
	LeftOut    []string `json:"left_out"`
}

type holdHsaVector struct {
	Person    string   `json:"person"`
	Limit     int64    `json:"limit"`
	Ok        bool     `json:"ok"`
	Questions []string `json:"questions"`
	LeftOut   []string `json:"left_out"`
	Error     string   `json:"error"`
}

type jobProjectVector struct {
	ID         string   `json:"id"`
	Projection string   `json:"projection"`
	Error      string   `json:"error"`
	Questions  []string `json:"questions"`
	LeftOut    []string `json:"left_out"`
	Working    []string `json:"working"`
}

func TestEmployer(t *testing.T) {
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
	var out []*employerScenario
	byKey := map[string]*employerScenario{}
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
		y := file.GetTaxYear()
		asOfs := []*commonv1.Date{file.GetAsOf(), {Year: y, Month: 3, Day: 1}, {Year: y, Month: 8, Day: 15}, {Year: y, Month: 12, Day: 31}}
		for _, r := range resolved {
			in := r.Inputs()
			m := in.GetModel()
			hasPlan := false
			for _, rm := range m.GetRetirement() {
				hasPlan = hasPlan || retirement.PlanPart(rm) != nil
			}
			if len(m.GetEmployers()) == 0 && !hasPlan {
				continue
			}
			name := filepath.Base(filepath.Dir(path)) + "/" + filepath.Base(path) + "#" + r.Scenario().GetId()
			facts := encode(t, in.GetFacts())
			employers, retirements := []string{}, []string{}
			for _, e := range m.GetEmployers() {
				employers = append(employers, encode(t, e))
			}
			for _, rm := range m.GetRetirement() {
				retirements = append(retirements, encode(t, rm))
			}
			mm := strings.Join(employers, ",") + ";" + strings.Join(retirements, ",")
			overrides := []string{}
			for _, o := range in.GetOverrides() {
				overrides = append(overrides, encode(t, o))
			}
			for i, a := range asOfs {
				asOf := encode(t, a)
				key := strings.Join(append([]string{string(rune(y)), asOf, facts, mm}, overrides...), "|")
				label := name
				if i > 0 {
					label = name + "@" + dateText(a)
				}
				if s := byKey[key]; s != nil {
					s.Cases = append(s.Cases, label)
					continue
				}
				s := &employerScenario{Cases: []string{label}, Year: int(y), AsOf: asOf, Facts: facts, Employers: employers, Retirement: retirements, Overrides: overrides,
					PayDates: []payDatesVector{}, Expand: []expandVector{}, MaxOut: []maxOutVector{}, HoldHsa: []holdHsaVector{}, Jobs: []jobProjectVector{}}
				byKey[key] = s
				out = append(out, s)
				runJobs(t, s, int(y), a, in)
			}
		}
	}
	checkGzip(t, "employer/employer.json.gz", out)
}

// dateText is an as_of as text.
func dateText(d *commonv1.Date) string {
	x, err := civil.FromProto(d)
	if err != nil {
		return err.Error()
	}
	return x.String()
}

// runJobs drives the jobs as taxyear's project does.
func runJobs(t *testing.T, s *employerScenario, year int, asOf *commonv1.Date, in *scenariosv1.ScenarioInputs) {
	ctx, err := model.NewContext(year, asOf, in.GetFacts())
	s.Error = errText(err)
	if err != nil {
		return
	}
	m := in.GetModel()
	for _, e := range m.GetEmployers() {
		v := payDatesVector{ID: e.GetId(), Dates: [][3]int{}}
		first, err1 := civil.FromProto(e.GetFirstPayDate())
		starts, err2 := civil.FromProto(e.GetStarts())
		switch {
		case err1 != nil:
			v.Error = err1.Error()
		case err2 != nil:
			v.Error = err2.Error()
		default:
			for _, d := range employer.PayDates(ctx.Year(), e.GetPayFrequency(), first, starts) {
				v.Dates = append(v.Dates, ymd(d))
			}
		}
		s.PayDates = append(s.PayDates, v)
	}
	var jobs []*employer.Job
	failed := false
	for _, e := range m.GetEmployers() {
		j, err := employer.Expand(ctx, e, in.GetOverrides())
		s.Expand = append(s.Expand, expandVector{ID: e.GetId(), Person: e.GetPerson(), Error: errText(err)})
		if err != nil {
			failed = true
			continue
		}
		jobs = append(jobs, j)
	}
	if failed {
		return
	}
	for _, r := range m.GetRetirement() {
		part := retirement.PlanPart(r)
		if part == nil {
			continue
		}
		n, err := employer.MaxOut(ctx, r.GetPerson(), part, jobs)
		v := maxOutVector{Retirement: r.GetId(), Person: r.GetPerson(), Part: part.GetId(), Error: errText(err), Questions: []string{}, LeftOut: []string{}}
		if err == nil {
			v.Questions, v.LeftOut = notesOf(t, n)
		}
		s.MaxOut = append(s.MaxOut, v)
		if err != nil {
			return
		}
	}
	seen := map[string]bool{}
	for _, e := range m.GetEmployers() {
		if seen[e.GetPerson()] {
			continue
		}
		seen[e.GetPerson()] = true
		n := &model.Notes{}
		limit, ok := retirement.HsaLimit(ctx, e.GetPerson(), n)
		v := holdHsaVector{Person: e.GetPerson(), Limit: int64(limit), Ok: ok}
		if ok {
			v.Error = errText(employer.HoldHsa(e.GetPerson(), limit, jobs))
		}
		v.Questions, v.LeftOut = notesOf(t, n)
		s.HoldHsa = append(s.HoldHsa, v)
		if v.Error != "" {
			return
		}
	}
	for _, j := range jobs {
		p, err := j.Projection()
		v := jobProjectVector{ID: j.ID(), Error: errText(err), Questions: []string{}, LeftOut: []string{}, Working: []string{}}
		if err == nil {
			v.Projection = encode(t, p)
			v.Questions, v.LeftOut = notesOf(t, j.Notes())
			for _, w := range j.Working() {
				v.Working = append(v.Working, encode(t, w))
			}
		}
		s.Jobs = append(s.Jobs, v)
		if err != nil {
			return
		}
	}
	// A second Projection is refused.
	if len(jobs) > 0 {
		_, err := jobs[0].Projection()
		s.Jobs = append(s.Jobs, jobProjectVector{ID: jobs[0].ID(), Error: errText(err), Questions: []string{}, LeftOut: []string{}, Working: []string{}})
	}
}

// payrollVectors is the payroll package over a sweep.
type payrollVectors struct {
	Withholding  []withholdingVector `json:"withholding"`
	W4Errors     []w4ErrorVector     `json:"w4_errors"`
	Supplemental [][3]int64          `json:"supplemental"`
	Social       []socialVector      `json:"social_security"`
	WageBases    []wageBaseVector    `json:"wage_bases"`
	Medicare     [][3]int64          `json:"medicare"`
	PlanLimits   []planLimitVector   `json:"plan_limits"`
	NoWageTax    map[string]bool     `json:"no_wage_tax"`
}

// w4Spec is a W-4 as the sweep builds it.
type w4Spec struct {
	Status       int   `json:"status"`
	MultipleJobs bool  `json:"multiple_jobs"`
	Dependents   int64 `json:"dependents"`
	OtherIncome  int64 `json:"other_income"`
	Deductions   int64 `json:"deductions"`
	Extra        int64 `json:"extra"`
	Exempt       bool  `json:"exempt"`
}

func (s w4Spec) build() (payroll.W4, error) {
	b := payroll.NewW4(filing.Status(s.Status)).Dependents(money.USCents(s.Dependents)).
		OtherIncome(money.USCents(s.OtherIncome)).Deductions(money.USCents(s.Deductions)).Extra(money.USCents(s.Extra))
	if s.MultipleJobs {
		b.MultipleJobs()
	}
	if s.Exempt {
		b.Exempt()
	}
	return b.Build()
}

type withholdingVector struct {
	Year     int    `json:"year"`
	W4       w4Spec `json:"w4"`
	Taxable  int64  `json:"taxable"`
	Periods  int    `json:"periods"`
	Withheld int64  `json:"withheld"`
	Cite     string `json:"cite"`
	Applied  string `json:"applied"`
	Error    string `json:"error"`
}

type w4ErrorVector struct {
	W4    w4Spec `json:"w4"`
	Error string `json:"error"`
}

type socialVector struct {
	Year   int    `json:"year"`
	Wages  int64  `json:"wages"`
	Before int64  `json:"before"`
	Tax    int64  `json:"tax"`
	Error  string `json:"error"`
}

type wageBaseVector struct {
	Year  int    `json:"year"`
	Base  int64  `json:"base"`
	Error string `json:"error"`
}

type planLimitVector struct {
	Year    int    `json:"year"`
	Age     int    `json:"age"`
	Base    int64  `json:"base"`
	CatchUp int64  `json:"catch_up"`
	Error   string `json:"error"`
}

func TestPayroll(t *testing.T) {
	v := payrollVectors{NoWageTax: map[string]bool{}}
	var w4s []w4Spec
	for _, st := range []int{1, 2, 3, 4, 5} {
		w4s = append(w4s, w4Spec{Status: st}, w4Spec{Status: st, MultipleJobs: true},
			w4Spec{Status: st, Dependents: 400_000, OtherIncome: 1_000_000, Deductions: 1_500_000, Extra: 2_500},
			w4Spec{Status: st, MultipleJobs: true, Dependents: 200_000, Deductions: 3_000_000})
	}
	w4s = append(w4s, w4Spec{Status: 1, Exempt: true}, w4Spec{Status: 2, Exempt: true, MultipleJobs: true, Extra: 1_000})
	for _, year := range []int{2024, 2025, 2026, 2027} {
		for _, w := range w4s {
			b, err := w.build()
			if err != nil {
				t.Fatal(err)
			}
			for _, taxable := range []int64{-50_000, 0, 1, 33_333, 150_000, 583_333, 1_234_567, 4_000_000, 25_000_001} {
				for _, periods := range []int{-1, 0, 1, 12, 24, 26, 52} {
					got, rule, err := payroll.FederalWithholding(year, b, money.USCents(taxable), periods)
					v.Withholding = append(v.Withholding, withholdingVector{Year: year, W4: w, Taxable: taxable, Periods: periods,
						Withheld: int64(got), Cite: rule.Cite(), Applied: rule.Applied(), Error: errText(err)})
				}
			}
		}
	}
	for _, w := range []w4Spec{{}, {Status: 6}, {Status: 1, Dependents: -1}, {Status: 2, OtherIncome: -100},
		{Status: 3, Deductions: -5}, {Status: 4, Extra: -123_456}, {Status: 0, Extra: -1}, {Status: 5}} {
		_, err := w.build()
		v.W4Errors = append(v.W4Errors, w4ErrorVector{W4: w, Error: errText(err)})
	}
	amounts := []int64{-100_000, 0, 1, 99, 1_000_000, 5_000_000, 99_999_999, 100_000_000, 250_000_000}
	for _, a := range amounts {
		for _, b := range amounts {
			v.Supplemental = append(v.Supplemental, [3]int64{a, b, int64(payroll.FlatSupplemental(money.USCents(a), money.USCents(b)))})
			v.Medicare = append(v.Medicare, [3]int64{a, b, int64(payroll.Medicare(money.USCents(a), money.USCents(b)))})
		}
	}
	for _, year := range []int{2023, 2024, 2025, 2026, 2027} {
		base, err := payroll.WageBase(year)
		v.WageBases = append(v.WageBases, wageBaseVector{Year: year, Base: int64(base), Error: errText(err)})
		for _, w := range []int64{-500, 0, 1_000_000, 50_000_000} {
			for _, b := range []int64{0, 15_000_000, 17_000_000, 18_000_000, 18_400_000, 30_000_000} {
				tax, err := payroll.SocialSecurity(year, money.USCents(w), money.USCents(b))
				v.Social = append(v.Social, socialVector{Year: year, Wages: w, Before: b, Tax: int64(tax), Error: errText(err)})
			}
		}
		for _, age := range []int{-1, 0, 30, 49, 50, 59, 60, 61, 63, 64, 90} {
			base, catchUp, err := payroll.PlanLimit(year, age)
			v.PlanLimits = append(v.PlanLimits, planLimitVector{Year: year, Age: age, Base: int64(base), CatchUp: int64(catchUp), Error: errText(err)})
		}
	}
	for _, s := range []string{"AK", "AL", "CA", "FL", "NV", "NH", "NY", "SD", "TN", "TX", "WA", "WY", "", "ak", "DC"} {
		v.NoWageTax[s] = payroll.NoWageTax(s)
	}
	checkGzip(t, "employer/payroll.json.gz", v)
}
