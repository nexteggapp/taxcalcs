package taxyear

import (
	"fmt"
	"math/rand"
	"os"
	"sort"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/nexteggapp/taxcalcs/money"
	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
)

// overridable lists the figures so far an override can name in a result,
// with their values: each money line of every projection's so far items,
// and a job's pre-tax and state lines.
func overridable(s *scenariosv1.ScenarioResult) map[string]int64 {
	out := map[string]int64{}
	proj := s.GetProjection().ProtoReflect()
	proj.Range(func(list protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if list.Name() == "tax_paid" {
			return true
		}
		for i := 0; i < v.List().Len(); i++ {
			p := v.List().Get(i).Message()
			id := p.Get(p.Descriptor().Fields().ByName("id")).String()
			sf := p.Descriptor().Fields().ByName("so_far")
			if sf == nil || !p.Has(sf) {
				continue
			}
			at := fmt.Sprintf("projection.%s[%s].so_far.items", list.Name(), id)
			items := p.Get(sf).Message().Get(p.Get(sf).Message().Descriptor().Fields().ByName("items")).Message()
			if m, ok := items.Interface().(*commonv1.Money); ok {
				out[at+".amount"] = m.GetMinor()
				continue
			}
			items.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
				if fd.Kind() != protoreflect.MessageKind || fd.IsMap() {
					return true
				}
				if !fd.IsList() {
					if m, ok := v.Message().Interface().(*commonv1.Money); ok {
						out[at+"."+string(fd.Name())] = m.GetMinor()
					}
					return true
				}
				for k := 0; k < v.List().Len(); k++ {
					switch x := v.List().Get(k).Message().Interface().(type) {
					case interface{ GetAmount() *commonv1.Money }:
						if fd.Name() == "pretax" {
							out[fmt.Sprintf("%s.pretax[%d].amount", at, k)] = x.GetAmount().GetMinor()
						}
					case interface {
						GetState() string
						GetStateIncome() *commonv1.Money
						GetTaxWithheld() *commonv1.Money
					}:
						out[fmt.Sprintf("%s.states[%s].state_income", at, x.GetState())] = x.GetStateIncome().GetMinor()
						out[fmt.Sprintf("%s.states[%s].tax_withheld", at, x.GetState())] = x.GetTaxWithheld().GetMinor()
					}
				}
				return true
			})
		}
		return true
	})
	return out
}

// addsUp lists each way the scenario's figures fail to add up: a working
// whose terms don't add to its figure; a year figure of a projection that
// isn't its so far and its rest; a so far or rest item total that isn't its
// items; and state and local tax by where it was paid that isn't
// Schedule A's line 5a.
func addsUp(s *scenariosv1.ScenarioResult) []string {
	var out []string
	w := newWorkings(s)
	items := map[string]*scenariosv1.Item{}
	for _, list := range allItems(s.GetProjection()) {
		for _, it := range list {
			items[it.GetId()] = it
		}
	}
	for _, x := range s.GetWorking() {
		if len(x.GetTerms()) == 0 {
			continue
		}
		total, _ := w.amount(x.GetPath())
		var sum money.USCents
		for _, term := range x.GetTerms() {
			v, _ := w.amount(term.GetPath())
			if term.GetItem() != "" {
				v, _ = itemAmount(items[term.GetItem()], x.GetPath())
			}
			sum += money.USCents(term.GetSign()) * v
		}
		if sum != total {
			out = append(out, fmt.Sprintf("%s is %v and its working adds up to %v", x.GetPath(), total, sum))
		}
	}
	proj := s.GetProjection().ProtoReflect()
	proj.Range(func(list protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		for i := 0; i < v.List().Len(); i++ {
			p := v.List().Get(i).Message()
			at := fmt.Sprintf("projection.%s[%s]", list.Name(), p.Get(p.Descriptor().Fields().ByName("id")).String())
			year := p.Descriptor().Fields().ByName("year")
			if year == nil || !p.Has(year) {
				continue
			}
			for _, f := range moneyFields(p.Get(year).Message(), "") {
				y, _ := w.amount(at + ".year." + f)
				sf, _ := w.amount(at + ".so_far." + f)
				r, _ := w.amount(at + ".rest." + f)
				if y != sf+r {
					out = append(out, fmt.Sprintf("%s.year.%s is %v, so far %v and the rest %v", at, f, y, sf, r))
				}
			}
			carried := carrying(p.Get(p.Descriptor().Fields().ByName("items")).List())
			for _, period := range []string{"so_far", "rest"} {
				fd := p.Descriptor().Fields().ByName(protoreflect.Name(period))
				if !p.Has(fd) {
					continue
				}
				for _, f := range moneyFields(p.Get(fd).Message(), "") {
					ids := carried[period+"."+f]
					if len(ids) == 0 || !strings.HasPrefix(f, "items") {
						continue
					}
					total, _ := w.amount(at + "." + period + "." + f)
					var sum money.USCents
					for _, id := range ids {
						v, _ := itemAmount(items[id], at+"."+period+"."+f)
						sum += v
					}
					// Items are rounded together, so the total is their exact
					// sum rounded: within a dollar of each rounded alone.
					if d := total - sum; d > money.Dollar*money.USCents(len(ids)) || -d > money.Dollar*money.USCents(len(ids)) {
						out = append(out, fmt.Sprintf("%s.%s.%s is %v and its items add up to %v", at, period, f, total, sum))
					}
				}
			}
		}
		return true
	})
	// A job's California wages earned there alone are box 1, the payroll
	// HSA and the employer's, in whole dollars; a W-2's, as printed.
	for _, e := range s.GetProjection().GetEmployers() {
		if len(e.GetExpected()) == 0 {
			continue
		}
		for name, p := range map[string]*scenariosv1.PayTotals{"so_far": e.GetSoFar(), "rest": e.GetRest()} {
			states := p.GetItems().GetStates()
			if len(states) != 1 || states[0].GetState() != "CA" || e.GetSoFar() == nil {
				continue
			}
			var hsa int64
			for _, x := range p.GetItems().GetPretax() {
				if x.GetKind() == factsv1.PretaxKind_PRETAX_KIND_HSA {
					hsa += x.GetAmount().GetMinor()
				}
			}
			for _, st := range p.GetBoxes().GetStates() {
				if st.GetState() != "CA" {
					continue
				}
				if want := p.GetBoxes().GetWages().GetMinor() + hsa + st.GetEmployerHsa().GetMinor(); st.GetWages().GetMinor() != want && !typedCA(e) {
					out = append(out, fmt.Sprintf("projection.employers[%s].%s.boxes.states[CA].wages is %v, box 1, the payroll HSA and the employer's add up to %d", e.GetId(), name, st.GetWages(), want))
				}
			}
		}
	}
	// An itemised model's year is what Schedule A takes from it: a 1098's
	// interest, its statements brought to it.
	for _, m := range s.GetProjection().GetItemised() {
		var box int64
		boxes := m.GetYear().GetBoxes().ProtoReflect()
		boxes.Range(func(_ protoreflect.FieldDescriptor, v protoreflect.Value) bool {
			box += v.Message().Interface().(*commonv1.Money).GetMinor()
			return true
		})
		if items := m.GetYear().GetItems().GetMinor(); items != box {
			out = append(out, fmt.Sprintf("projection.itemised[%s].year.items is %d, and Schedule A takes %d from it", m.GetId(), items, box))
		}
	}
	// The itemised total adds up from Schedule A's lines and what the
	// limits take off.
	if sa := s.GetFederal().GetScheduleA(); sa.GetTotalItemizedDeductions() != nil {
		var found bool
		for _, x := range s.GetWorking() {
			found = found || x.GetPath() == "federal.schedule_a.total_itemized_deductions" && len(x.GetTerms()) > 0
		}
		if !found {
			out = append(out, "federal.schedule_a.total_itemized_deductions has no working that adds up")
		}
	}
	if v := s.GetSummary().GetItemisedBracketLimit().GetMinor(); v < 0 {
		out = append(out, fmt.Sprintf("summary.itemised_bracket_limit is %d, below zero", v))
	}
	salt := s.GetSummary().GetSalt()
	if sa := s.GetFederal().GetScheduleA(); sa != nil && salt != nil {
		if got := salt.GetWithholding().GetMinor() + salt.GetEstimates().GetMinor() + salt.GetLastYearBalance().GetMinor(); got != sa.GetStateLocalIncomeOrSalesTaxes().GetMinor() {
			out = append(out, fmt.Sprintf("state tax paid by where adds to %d, Schedule A line 5a is %v", got, sa.GetStateLocalIncomeOrSalesTaxes()))
		}
	}
	return out
}

// typedCA reports whether a job's California wages so far were
// overridden, which then stand as typed.
func typedCA(e *scenariosv1.EmployerProjection) bool {
	for _, it := range e.GetItems() {
		if strings.HasSuffix(it.GetOrigin().GetOverride(), ".states[CA].state_income") {
			return true
		}
	}
	return false
}

// TestOverridesAddUp overrides random figures so far in every case and
// scenario and wants every figure to add up as it did before: an
// override's figure flows into every total that reads it.
func TestOverridesAddUp(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	names := []string{}
	all := cases(t)
	for name := range all {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		file := all[name]
		base, err := Compute(file)
		if err != nil {
			t.Fatal(err)
		}
		plan := base.Message().GetScenarios()[0]
		paths := overridable(plan)
		keys := make([]string, 0, len(paths))
		for k := range paths {
			if OverridablePath(k) {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		if len(keys) == 0 {
			continue
		}
		for round := 0; round < 4; round++ {
			f := proto.Clone(file).(*scenariosv1.TaxYearScenarios)
			if f.Shared == nil {
				f.Shared = &scenariosv1.ScenarioInputs{}
			}
			var chosen []string
			// A path the file already overrides isn't overridden twice.
			picked := map[string]bool{}
			for _, o := range f.GetShared().GetOverrides() {
				picked[o.GetPath()] = true
			}
			for n := 1 + r.Intn(3); n > 0; n-- {
				k := keys[r.Intn(len(keys))]
				if picked[k] {
					continue
				}
				picked[k] = true
				was := paths[k]
				want := was + int64(r.Intn(200_001)) - 100_000
				if want < 0 {
					want = was / 2
				}
				want = want / 100 * 100
				f.Shared.Overrides = append(f.Shared.Overrides, &scenariosv1.Override{Path: k, Amount: &commonv1.Money{Minor: want, Currency: "USD"}})
				chosen = append(chosen, fmt.Sprintf("%s=%d", k, want))
			}
			res, err := Compute(f)
			if err != nil {
				t.Errorf("%s with %v: %v", name, chosen, err)
				continue
			}
			for _, s := range res.Message().GetScenarios() {
				if p := s.GetProblems(); len(p) > 0 {
					t.Errorf("%s %s with %v: problems %v", name, s.GetScenario(), chosen, p)
					continue
				}
				for _, bad := range addsUp(s) {
					t.Errorf("%s %s with %v: %s", name, s.GetScenario(), chosen, bad)
				}
			}
		}
	}
}

// TestOverridesTakeEffect: each figure so far, overridden alone, is the
// typed figure in the result.
func TestOverridesTakeEffect(t *testing.T) {
	file := cases(t)["full-2026.jsonc"]
	base, err := Compute(file)
	if err != nil {
		t.Fatal(err)
	}
	paths := overridable(base.Message().GetScenarios()[0])
	keys := make([]string, 0, len(paths))
	for k := range paths {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		f := proto.Clone(file).(*scenariosv1.TaxYearScenarios)
		want := paths[k] + 12_300
		f.Shared.Overrides = append(f.Shared.Overrides, &scenariosv1.Override{Path: k, Amount: &commonv1.Money{Minor: want, Currency: "USD"}})
		res, err := Compute(f)
		if err != nil {
			t.Errorf("%s: %v", k, err)
			continue
		}
		s := res.Message().GetScenarios()[0]
		if got, ok := overridable(s)[k]; !ok || got != want {
			t.Errorf("overriding %s to %d: the result has %d", k, want, got)
		}
	}
}

// TestOverridesReachTheReturn: an override moves every figure that reads
// it by the same amount. In the full household's plan, $1,000 more gross
// pay so far at Acme is $1,000 more box 1 and California wages so far, and
// on Form 1040 line 1a; $1,000 more California tax withheld so far is
// $1,000 more state tax on Schedule A and in its summary by where; and
// $1,000 more mortgage interest so far is $1,000 more on Schedule A.
func TestOverridesReachTheReturn(t *testing.T) {
	file := cases(t)["full-2026.jsonc"]
	compute := func(path string, by int64) *scenariosv1.ScenarioResult {
		f := proto.Clone(file).(*scenariosv1.TaxYearScenarios)
		if path != "" {
			base := overridable(mustPlan(t, file))[path]
			f.Shared.Overrides = append(f.Shared.Overrides, &scenariosv1.Override{Path: path, Amount: &commonv1.Money{Minor: base + by, Currency: "USD"}})
		}
		return mustPlan(t, f)
	}
	base := compute("", 0)
	for _, c := range []struct {
		path  string
		reads []string
	}{
		{"projection.employers[acme].so_far.items.gross", []string{
			"projection.employers[acme].so_far.boxes.wages", "projection.employers[acme].so_far.boxes.states[CA].wages",
			"projection.employers[acme].year.boxes.states[CA].wages", "federal.form_1040.w2_wages"}},
		{"projection.employers[acme].so_far.items.states[CA].tax_withheld", []string{
			"projection.employers[acme].year.boxes.states[CA].withheld", "summary.salt.withholding", "federal.schedule_a.state_local_income_or_sales_taxes"}},
		{"projection.itemised[mortgage].so_far.items.amount", []string{
			"projection.itemised[mortgage].year.boxes.mortgage_interest", "federal.schedule_a.total_mortgage_interest_and_points"}},
	} {
		s := compute(c.path, 100_000)
		for _, at := range c.reads {
			was, ok1 := figureAt(base, at)
			got, ok2 := figureAt(s, at)
			if !ok1 || !ok2 || got-was != 100_000 {
				t.Errorf("$1,000 more at %s: %s is %d, was %d (found %v %v), want $1,000 more", c.path, at, got, was, ok1, ok2)
			}
		}
	}
}

func mustPlan(t *testing.T, f *scenariosv1.TaxYearScenarios) *scenariosv1.ScenarioResult {
	t.Helper()
	r, err := Compute(f)
	if err != nil {
		t.Fatal(err)
	}
	s := r.Message().GetScenarios()[0]
	if p := s.GetProblems(); len(p) > 0 {
		t.Fatalf("problems %v", p)
	}
	return s
}

// figureAt is the money at a path in a result: a field path whose list
// items are named by id, or a state's line by its state.
func figureAt(s *scenariosv1.ScenarioResult, path string) (int64, bool) {
	m := s.ProtoReflect()
	for _, part := range strings.Split(path, ".") {
		name, key, _ := strings.Cut(strings.TrimSuffix(part, "]"), "[")
		fd := m.Descriptor().Fields().ByName(protoreflect.Name(name))
		if fd == nil {
			return 0, false
		}
		if !fd.IsList() {
			if !m.Has(fd) {
				return 0, false
			}
			m = m.Get(fd).Message()
			continue
		}
		l := m.Get(fd).List()
		var found bool
		for i := 0; i < l.Len(); i++ {
			x := l.Get(i).Message()
			for _, k := range []protoreflect.Name{"id", "state"} {
				if kf := x.Descriptor().Fields().ByName(k); kf != nil && x.Get(kf).String() == key {
					m, found = x, true
				}
			}
		}
		if !found {
			return 0, false
		}
	}
	v, ok := m.Interface().(*commonv1.Money)
	return v.GetMinor(), ok
}

// TestItemisedLimitsAreParts: $1,000,000 of gross pay so far at Acme puts
// the full household in the 37% bracket, so the itemised total is Schedule
// A's lines less the charity floor and the 2/37 limit, each a figure with
// a working, and the total's working lists both.
func TestItemisedLimitsAreParts(t *testing.T) {
	f := proto.Clone(cases(t)["full-2026.jsonc"]).(*scenariosv1.TaxYearScenarios)
	f.Shared.Overrides = append(f.Shared.Overrides, &scenariosv1.Override{Path: "projection.employers[acme].so_far.items.gross",
		Amount: &commonv1.Money{Minor: 100_000_000, Currency: "USD"}})
	s := mustPlan(t, f)
	for _, bad := range addsUp(s) {
		t.Error(bad)
	}
	floor, ok1 := figureAt(s, "summary.itemised[charity].floor")
	limit, ok2 := figureAt(s, "summary.itemised_bracket_limit")
	if !ok1 || !ok2 || floor <= 0 || limit <= 0 {
		t.Fatalf("charity floor %d (%v) and 2/37 limit %d (%v), want both above zero", floor, ok1, limit, ok2)
	}
	if both, _ := figureAt(s, "summary.itemised_limitation"); both != floor+limit {
		t.Errorf("itemised_limitation %d, want the floor and the 2/37 limit, %d", both, floor+limit)
	}
	terms := map[string]int32{}
	paths := map[string]bool{}
	for _, x := range s.GetWorking() {
		paths[x.GetPath()] = len(x.GetRules()) > 0
		if x.GetPath() == "federal.schedule_a.total_itemized_deductions" {
			for _, term := range x.GetTerms() {
				terms[term.GetPath()] = term.GetSign()
			}
		}
	}
	if terms["summary.itemised[charity].floor"] != -1 || terms["summary.itemised_bracket_limit"] != -1 {
		t.Errorf("the itemised total's terms %v, want both limits taken off", terms)
	}
	if !paths["summary.itemised[charity].floor"] || !paths["summary.itemised_bracket_limit"] {
		t.Errorf("the limits' workings %v, want a rule for each", paths)
	}
}

// TestSocialSecurityOverIsWithholding: a second job from Oct 1 at $20,000
// a paycheck puts the year's Social Security withheld over the limit, so
// Schedule 3 credits the excess. The US withholding is income tax
// withheld and that excess, in whole dollars, and what is credited
// is the withholding and the payments made.
func TestSocialSecurityOverIsWithholding(t *testing.T) {
	f := proto.Clone(cases(t)["full-2026.jsonc"]).(*scenariosv1.TaxYearScenarios)
	usd := func(c int64) *commonv1.Money { return &commonv1.Money{Minor: c, Currency: "USD"} }
	oct := &commonv1.Date{Year: 2026, Month: 10, Day: 1}
	f.Shared.Model.Employers = append(f.Shared.Model.Employers, &scenariosv1.EmployerModel{
		Id: "added-1", Name: "New job", Person: "you", PayFrequency: scenariosv1.PayFrequency_PAY_FREQUENCY_SEMI_MONTHLY,
		Starts: oct, Work: &scenariosv1.WorkPlace{Place: &scenariosv1.WorkPlace_WhereLived_{WhereLived: &scenariosv1.WorkPlace_WhereLived{}}},
		Supplemental: scenariosv1.SupplementalMethod_SUPPLEMENTAL_METHOD_FLAT,
		Parts: []*scenariosv1.EmployerPart{{Id: "added-1-rate", Part: &scenariosv1.EmployerPart_PayRate_{PayRate: &scenariosv1.EmployerPart_PayRate{
			From: oct, Paycheck: &factsv1.PayFigures{Gross: usd(2_000_000)}}}}},
		DefaultPart: &scenariosv1.EmployerDefault{Id: "added-1-default", Rest: &scenariosv1.EmployerDefault_EachPaycheck{EachPaycheck: &factsv1.PayFigures{Gross: usd(0)}}},
	})
	s := mustPlan(t, f)
	over := s.GetFederal().GetSchedule_3().GetExcessSocialSecurityWithheld().GetMinor()
	if over <= 0 {
		t.Fatalf("Schedule 3 excess Social Security %d, want some: the test needs it", over)
	}
	for _, e := range s.GetEstimated() {
		if e.GetJurisdiction() != "US" {
			continue
		}
		x := e.GetSummary()
		if got, want := x.GetWithholding().GetMinor(), s.GetFederal().GetForm_1040().GetTotalWithheld().GetMinor()+over; got != want || x.GetSocialSecurityOver().GetMinor() != over {
			t.Errorf("withholding %d with %v over, want income tax withheld and the %d over, %d", got, x.GetSocialSecurityOver(), over, want)
		}
		if got, want := x.GetCredited().GetMinor(), x.GetWithholding().GetMinor()+x.GetPaid().GetMinor(); got != want {
			t.Errorf("credited %d, want the withholding and the payments made, %d", got, want)
		}
	}
}

// A typed year total holds against every adjustment: an override of a
// figure so far, the HSA limit and a workplace plan total change only the
// paychecks the model works out. Each figure an adjustment would have
// changed has its computed value as its working's automatic.
func TestTypedYearTotalHolds(t *testing.T) {
	at := "projection.employers[acme].year.items."
	for _, c := range []struct {
		file string
		want map[string]int64
		auto []string
	}{
		{"full-year-total-hsa.jsonc", map[string]int64{"states[CA].tax_withheld": 792_700, "federal_withheld": 1_863_700}, nil},
		{"full-year-total-plan.jsonc", map[string]int64{"states[CA].tax_withheld": 792_700, "federal_withheld": 1_863_700},
			[]string{"pretax[0].amount"}},
	} {
		r, err := Compute(cases(t)[c.file])
		if err != nil {
			t.Fatal(err)
		}
		plan, _ := r.Scenario("plan")
		res := plan.Proto()
		if p := res.GetProblems(); len(p) > 0 {
			t.Fatalf("%s: problems %v", c.file, p)
		}
		for path, want := range c.want {
			if got, _ := figureAt(res, at+path); got != want {
				t.Errorf("%s: %s = %d, want the typed %d", c.file, path, got, want)
			}
		}
		autos := map[string]bool{}
		for _, w := range res.GetWorking() {
			if w.GetAutomatic() != nil {
				autos[w.GetPath()] = true
			}
		}
		for _, path := range c.auto {
			if !autos[at+path] {
				t.Errorf("%s: %s has no automatic", c.file, path)
			}
		}
	}
}

// Max-out across a job ending and a typed new one leaves every paycheck to
// come paying out at least $0, items being whole dollars.
func TestMaxOutAcrossAJobChangePaysOut(t *testing.T) {
	text, err := os.ReadFile("testdata/fixtures/full-maxout-job-change.jsonc")
	if err != nil {
		t.Fatal(err)
	}
	f, err := Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Compute(f)
	if err != nil {
		t.Fatal(err)
	}
	plan, _ := r.Scenario("plan")
	for _, e := range plan.Proto().GetProjection().GetEmployers() {
		for _, it := range e.GetItems() {
			if it.GetStatus() != scenariosv1.ItemStatus_ITEM_STATUS_EXPECTED || it.GetDate().GetMonth() < 9 {
				continue
			}
			c := it.GetPaycheck()
			out := c.GetFederalWithheld().GetMinor() + c.GetSocialSecurityWithheld().GetMinor() + c.GetMedicareWithheld().GetMinor()
			for _, x := range c.GetPretax() {
				out += x.GetAmount().GetMinor()
			}
			for _, s := range c.GetStates() {
				out += s.GetTaxWithheld().GetMinor()
			}
			if left := c.GetGross().GetMinor() - out; left < -100 {
				t.Errorf("%s pays out %d", it.GetId(), left)
			}
		}
	}
}

// An override of an other income model's taxable amount so far, where its
// items leave taxable unset and so count their gross, keeps every item
// adding up to the totals: so far, the rest and the year. The amounts are
// fixed, so it fails the same way every run.
func TestOtherIncomeTaxableOverrideAddsUp(t *testing.T) {
	f := proto.Clone(cases(t)["simple-upload-view.jsonc"]).(*scenariosv1.TaxYearScenarios)
	f.Shared.Overrides = append(f.Shared.Overrides, &scenariosv1.Override{Path: "projection.other_income[ssa].so_far.items.taxable",
		Amount: &commonv1.Money{Minor: 2_366_000, Currency: "USD"}})
	res, err := Compute(f)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range res.Message().GetScenarios() {
		if p := s.GetProblems(); len(p) > 0 {
			t.Fatalf("%s: problems %v", s.GetScenario(), p)
		}
		for _, bad := range addsUp(s) {
			t.Errorf("%s: %s", s.GetScenario(), bad)
		}
		if got, _ := figureAt(s, "projection.other_income[ssa].so_far.items.taxable"); got != 2_366_000 {
			t.Errorf("%s: taxable so far %d, want the typed $23,660", s.GetScenario(), got)
		}
	}
}
