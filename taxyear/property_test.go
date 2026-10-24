package taxyear_test

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
)

// slots are Acme's twice-monthly pay dates before the example's as_of,
// Sep 8, 2026: the 15th and the last day, a weekend moved to the Friday.
var slots = []*commonv1.Date{
	date(1, 15), date(1, 30), date(2, 13), date(2, 27), date(3, 13), date(3, 31), date(4, 15), date(4, 30),
	date(5, 15), date(5, 29), date(6, 15), date(6, 30), date(7, 15), date(7, 31), date(8, 14), date(8, 31),
}

func cents(v float64) *commonv1.Money { return usd(v) }

// household makes a random household from the example: some of Acme's pay
// dates with a paystub, some with deposits, split or not, some extra
// deposits and a bonus; maybe a second job starting later; and in Raise in
// Oct a random raise, bonus and end.
func household(r *rand.Rand) *scenariosv1.TaxYearScenarios {
	f, err := taxyear.Parse(example)
	if err != nil {
		panic(err)
	}
	a := f.GetShared().GetModel().GetEmployers()[0]
	var stubs []*scenariosv1.EmployerPart_FromPaystubs_Stub
	var deps []*scenariosv1.EmployerPart_FromDeposits_Deposit
	for i, d := range slots {
		gross := 5833.33 + float64(r.Intn(3))*100
		fig := &factsv1.PayFigures{Gross: cents(gross), FederalWithheld: cents(700), Net: cents(gross - 1700),
			Pretax: []*factsv1.PayFigures_Pretax{{Kind: factsv1.PretaxKind_PRETAX_KIND_401K, Amount: cents(float64(r.Intn(5)) * 100)}}}
		if r.Intn(3) == 0 {
			fig.Net = nil
		}
		switch r.Intn(4) {
		case 0:
			stubs = append(stubs, &scenariosv1.EmployerPart_FromPaystubs_Stub{Id: fmt.Sprintf("acme-s%02d", i), Source: &factsv1.DocumentRef{Name: "stub"},
				Paystub: &factsv1.Paystub{PayDate: d, Kind: factsv1.PaycheckKind_PAYCHECK_KIND_REGULAR, ThisPaycheck: fig}})
			if fig.Net != nil && r.Intn(2) == 0 {
				deps = append(deps, &scenariosv1.EmployerPart_FromDeposits_Deposit{Id: fmt.Sprintf("acme-d%02da", i), Date: d, Amount: cents(gross - 1700)})
			}
		case 1:
			deps = append(deps, &scenariosv1.EmployerPart_FromDeposits_Deposit{Id: fmt.Sprintf("acme-d%02da", i), Date: d, Amount: cents(3000)},
				&scenariosv1.EmployerPart_FromDeposits_Deposit{Id: fmt.Sprintf("acme-d%02db", i), Date: d, Amount: cents(1102.18)})
		}
	}
	if r.Intn(2) == 0 {
		deps = append(deps, &scenariosv1.EmployerPart_FromDeposits_Deposit{Id: "acme-dx", Date: date(6, 22), Amount: cents(250)})
	}
	if r.Intn(2) == 0 {
		stubs = append(stubs, &scenariosv1.EmployerPart_FromPaystubs_Stub{Id: "acme-bonus", Source: &factsv1.DocumentRef{Name: "bonus"},
			Paystub: &factsv1.Paystub{PayDate: date(3, 20), Kind: factsv1.PaycheckKind_PAYCHECK_KIND_BONUS, ThisPaycheck: &factsv1.PayFigures{Gross: cents(5000), FederalWithheld: cents(1100)}}})
	}
	if len(stubs) > 0 && r.Intn(2) == 0 {
		last := stubs[len(stubs)-1]
		last.Paystub.YearToDate = &factsv1.PayFigures{Gross: cents(float64(len(slots)) * 5800)}
	}
	a.Parts = []*scenariosv1.EmployerPart{
		{Id: "acme-h4c8w2pq", Part: &scenariosv1.EmployerPart_FromPaystubs_{FromPaystubs: &scenariosv1.EmployerPart_FromPaystubs{Stubs: stubs}}},
		{Id: "acme-b6n3x8tf", Part: &scenariosv1.EmployerPart_FromDeposits_{FromDeposits: &scenariosv1.EmployerPart_FromDeposits{Deposits: deps}}},
	}
	if r.Intn(2) == 0 {
		f.GetShared().GetModel().Employers = append(f.GetShared().GetModel().Employers, &scenariosv1.EmployerModel{
			Id: "beta", Name: "Beta", Person: "you", PayFrequency: scenariosv1.PayFrequency_PAY_FREQUENCY_BIWEEKLY,
			FirstPayDate: date(1, 9), Starts: date(int32(r.Intn(10)+1), 1), Work: a.GetWork(), Supplemental: a.GetSupplemental(),
			DefaultPart: &scenariosv1.EmployerDefault{Id: "beta-default", Rest: &scenariosv1.EmployerDefault_EachPaycheck{EachPaycheck: &factsv1.PayFigures{Gross: cents(2000)}}},
		})
	}
	raise := f.GetScenarios()[1].GetChanges().GetModel().GetEmployers()[0].GetPatch()
	add := func(p *scenariosv1.EmployerPart) {
		raise.Parts = append(raise.Parts, &scenariosv1.EmployerModelDiff_PartsChange{Id: proto.String(p.GetId()), Op: &scenariosv1.EmployerModelDiff_PartsChange_Add{Add: p}})
	}
	raise.Parts = nil
	add(&scenariosv1.EmployerPart{Id: "acme-raise", Part: &scenariosv1.EmployerPart_PayRate_{PayRate: &scenariosv1.EmployerPart_PayRate{
		From: date(int32(r.Intn(2)+9), int32(r.Intn(28)+1)), Paycheck: &factsv1.PayFigures{Gross: cents(6000 + float64(r.Intn(1000)))}}}})
	if r.Intn(2) == 0 {
		add(&scenariosv1.EmployerPart{Id: "acme-oneoff", Part: &scenariosv1.EmployerPart_OneOff_{OneOff: &scenariosv1.EmployerPart_OneOff{
			Date: date(12, int32(r.Intn(20)+1)), Kind: factsv1.PaycheckKind_PAYCHECK_KIND_BONUS, Amount: cents(float64(r.Intn(20000)))}}})
	}
	if r.Intn(2) == 0 {
		add(&scenariosv1.EmployerPart{Id: "acme-ends", Part: &scenariosv1.EmployerPart_Ends_{Ends: &scenariosv1.EmployerPart_Ends{LastPaycheck: date(11, int32(r.Intn(28)+1))}}})
	}
	return f
}

// eachProjection calls fn for each employer projection in every scenario.
func eachEmployer(res *scenariosv1.TaxYearResult, fn func(scenario string, p *scenariosv1.EmployerProjection)) {
	for _, s := range res.GetScenarios() {
		for _, p := range s.GetProjection().GetEmployers() {
			fn(s.GetScenario(), p)
		}
	}
}

func mustCompute(t *testing.T, seed int64, f *scenariosv1.TaxYearScenarios) *scenariosv1.TaxYearResult {
	t.Helper()
	res := compute(t, f)
	var problems []string
	for _, p := range res.GetProblems() {
		problems = append(problems, p.GetPath()+": "+p.GetMessage().GetText())
	}
	for _, s := range res.GetScenarios() {
		for _, p := range s.GetProblems() {
			problems = append(problems, s.GetScenario()+" "+p.GetPath()+": "+p.GetMessage().GetText())
		}
	}
	if len(problems) > 0 {
		t.Fatalf("seed %d: problems:\n%s", seed, strings.Join(problems, "\n"))
	}
	return res
}

// TestTotalsEqualItems: every projection's item totals are the sum of its
// items, so far the made ones and the rest the expected ones.
func TestTotalsEqualItems(t *testing.T) {
	for seed := int64(0); seed < 200; seed++ {
		res := mustCompute(t, seed, household(rand.New(rand.NewSource(seed))))
		eachEmployer(res, func(s string, p *scenariosv1.EmployerProjection) {
			var so, rest, all [5]int64
			add := func(to *[5]int64, f *factsv1.PayFigures) {
				to[0] += f.GetGross().GetMinor()
				to[1] += f.GetFederalWithheld().GetMinor()
				to[2] += f.GetSocialSecurityWithheld().GetMinor()
				to[3] += f.GetMedicareWithheld().GetMinor()
				for _, x := range f.GetPretax() {
					to[4] += x.GetAmount().GetMinor()
				}
			}
			for _, it := range p.GetItems() {
				add(&all, it.GetPaycheck())
				if it.GetStatus() == scenariosv1.ItemStatus_ITEM_STATUS_MADE {
					add(&so, it.GetPaycheck())
				} else {
					add(&rest, it.GetPaycheck())
				}
			}
			for _, c := range []struct {
				name string
				want [5]int64
				got  *scenariosv1.PayTotals
			}{{"so_far", so, p.GetSoFar()}, {"rest", rest, p.GetRest()}, {"year", all, p.GetYear()}} {
				var got [5]int64
				add(&got, c.got.GetItems())
				if got != c.want {
					t.Errorf("seed %d %s %s %s: totals %v, items add to %v", seed, s, p.GetId(), c.name, got, c.want)
				}
			}
			box1 := dollars(so[0]-so[4]) + dollars(rest[0]-rest[4])
			if got := p.GetYear().GetBoxes().GetWages().GetMinor(); got != box1 && len(p.GetItems()) > 0 {
				t.Errorf("seed %d %s %s: box 1 %d, want so far's and the rest's gross less pre-tax in dollars, added: %d", seed, s, p.GetId(), got, box1)
			}
			if p.GetYear().GetBoxes().GetWages().GetMinor() != p.GetSoFar().GetBoxes().GetWages().GetMinor()+p.GetRest().GetBoxes().GetWages().GetMinor() {
				t.Errorf("seed %d %s %s: box 1 for the year isn't so far's plus the rest's", seed, s, p.GetId())
			}
		})
	}
}

// TestIDsAreStableAcrossScenarios: a pay date's item has the same id in
// every scenario, whichever part fills it, and every evidence item is the
// same in every scenario.
func TestIDsAreStableAcrossScenarios(t *testing.T) {
	for seed := int64(0); seed < 200; seed++ {
		res := mustCompute(t, seed, household(rand.New(rand.NewSource(seed))))
		byDate := map[string]string{}
		made := map[string]map[string]bool{}
		eachEmployer(res, func(s string, p *scenariosv1.EmployerProjection) {
			for _, it := range p.GetItems() {
				if it.GetStatus() == scenariosv1.ItemStatus_ITEM_STATUS_MADE {
					if made[s] == nil {
						made[s] = map[string]bool{}
					}
					made[s][it.GetId()] = true
				}
				if it.GetKind() != scenariosv1.ItemKind_ITEM_KIND_PAYCHECK || strings.Count(it.GetId(), "/") != 3 {
					continue
				}
				d := it.GetDate()
				key := fmt.Sprintf("%s/%04d-%02d-%02d", p.GetId(), d.GetYear(), d.GetMonth(), d.GetDay())
				want := "employers/" + key + "/regular"
				if it.GetId() != want && it.GetStatus() == scenariosv1.ItemStatus_ITEM_STATUS_EXPECTED {
					t.Errorf("seed %d %s: the pay date %s's item is %s, want %s", seed, s, key, it.GetId(), want)
				}
				if prev, ok := byDate[key]; ok && prev != it.GetId() {
					t.Errorf("seed %d: pay date %s is %s in one scenario and %s in %s", seed, key, prev, it.GetId(), s)
				}
				byDate[key] = it.GetId()
			}
		})
		if !proto.Equal(mapMessage(made["plan"]), mapMessage(made["raise"])) {
			t.Errorf("seed %d: the made items differ between scenarios: %v and %v", seed, made["plan"], made["raise"])
		}
	}
}

// dollars rounds cents to whole dollars, half away from zero.
func dollars(c int64) int64 {
	if c < 0 {
		return -dollars(-c)
	}
	return (c + 50) / 100 * 100
}

func mapMessage(m map[string]bool) *factsv1.Person {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	sortStrings(keys)
	return &factsv1.Person{Name: strings.Join(keys, ",")}
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// TestEvidenceIsNeverRefused: adding any paystub or deposit to a household
// that computes never gives it a problem, even after an assumed end, a
// planned change's start, or the paystub a total was typed against. The
// new evidence counts, and assumed parts it contradicts stand aside.
func TestEvidenceIsNeverRefused(t *testing.T) {
	for seed := int64(0); seed < 200; seed++ {
		r := rand.New(rand.NewSource(seed))
		f := household(r)
		mustCompute(t, seed, f)
		a := f.GetShared().GetModel().GetEmployers()[0]
		for n := 0; n < 1+r.Intn(4); n++ {
			on := date(int32(r.Intn(12)+1), int32(r.Intn(28)+1))
			switch r.Intn(3) {
			case 0:
				a.GetParts()[0].GetFromPaystubs().Stubs = append(a.GetParts()[0].GetFromPaystubs().Stubs, &scenariosv1.EmployerPart_FromPaystubs_Stub{
					Id: fmt.Sprintf("acme-new%d", n), Source: &factsv1.DocumentRef{Name: "new"},
					Paystub: &factsv1.Paystub{PayDate: on, Kind: factsv1.PaycheckKind(r.Intn(4) + 1), ThisPaycheck: &factsv1.PayFigures{Gross: cents(float64(r.Intn(9000)))}}})
			default:
				a.GetParts()[1].GetFromDeposits().Deposits = append(a.GetParts()[1].GetFromDeposits().Deposits, &scenariosv1.EmployerPart_FromDeposits_Deposit{
					Id: fmt.Sprintf("acme-newd%d", n), Date: on, Amount: cents(float64(r.Intn(9000)))})
			}
		}
		mustCompute(t, seed, f)
	}
}
