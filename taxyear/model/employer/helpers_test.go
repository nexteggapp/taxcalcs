package employer_test

import (
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/money"
	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/internal/civil"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	"github.com/nexteggapp/taxcalcs/taxyear/model/employer"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
)

// usd is an amount in dollars and cents, written as a literal: usd(5833.33).
func usd(d float64) *commonv1.Money {
	c := int64(d*100 + 0.5)
	if d < 0 {
		c = int64(d*100 - 0.5)
	}
	return &commonv1.Money{Minor: c, Currency: "USD"}
}

func day(m time.Month, d int) *commonv1.Date {
	return &commonv1.Date{Year: 2026, Month: int32(m), Day: int32(d)}
}

func ctxAt(t *testing.T, m time.Month, d int, facts *factsv1.TaxYearFacts) *model.Context {
	t.Helper()
	c, err := model.NewContext(2026, day(m, d), facts)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// paycheck is $5,833.33 gross with a $500 401(k) line, $700 federal and
// $300 California withheld, and $4,000 net.
func paycheck() *factsv1.PayFigures {
	return &factsv1.PayFigures{
		Gross:                  usd(5833.33),
		Pretax:                 []*factsv1.PayFigures_Pretax{{Kind: factsv1.PretaxKind_PRETAX_KIND_401K, Amount: usd(500)}},
		FederalWithheld:        usd(700),
		SocialSecurityWithheld: usd(361.67),
		MedicareWithheld:       usd(84.58),
		States:                 []*commonv1.StateTaxWithholding{{State: "CA", StateIncome: usd(5333.33), TaxWithheld: usd(300)}},
		Net:                    usd(4000),
	}
}

func stub(id string, m time.Month, d int, kind factsv1.PaycheckKind, f *factsv1.PayFigures) *scenariosv1.EmployerPart_FromPaystubs_Stub {
	return &scenariosv1.EmployerPart_FromPaystubs_Stub{Id: id, Source: &factsv1.DocumentRef{Name: id},
		Paystub: &factsv1.Paystub{PayDate: day(m, d), Kind: kind, ThisPaycheck: f}}
}

func regular(id string, m time.Month, d int) *scenariosv1.EmployerPart_FromPaystubs_Stub {
	return stub(id, m, d, factsv1.PaycheckKind_PAYCHECK_KIND_REGULAR, paycheck())
}

func stubs(id string, s ...*scenariosv1.EmployerPart_FromPaystubs_Stub) *scenariosv1.EmployerPart {
	return &scenariosv1.EmployerPart{Id: id, Part: &scenariosv1.EmployerPart_FromPaystubs_{FromPaystubs: &scenariosv1.EmployerPart_FromPaystubs{Stubs: s}}}
}

func deposit(id string, m time.Month, d int, amount float64) *scenariosv1.EmployerPart_FromDeposits_Deposit {
	return &scenariosv1.EmployerPart_FromDeposits_Deposit{Id: id, Date: day(m, d), Amount: usd(amount),
		Source: &factsv1.LedgerRef{VaultRef: "txn_" + id, Posted: day(m, d)}}
}

func deposits(id string, d ...*scenariosv1.EmployerPart_FromDeposits_Deposit) *scenariosv1.EmployerPart {
	return &scenariosv1.EmployerPart{Id: id, Part: &scenariosv1.EmployerPart_FromDeposits_{FromDeposits: &scenariosv1.EmployerPart_FromDeposits{Deposits: d}}}
}

// job is a twice-monthly 401(k) job in California, flat supplemental, that
// repeats its latest paystub.
func job(parts ...*scenariosv1.EmployerPart) *scenariosv1.EmployerModel {
	return &scenariosv1.EmployerModel{
		Id: "acme", Name: "Acme Corp", Person: "you", PayFrequency: scenariosv1.PayFrequency_PAY_FREQUENCY_SEMI_MONTHLY,
		FirstPayDate: day(time.January, 15), RetirementPlan: scenariosv1.RetirementPlan_RETIREMENT_PLAN_401K,
		Work: &scenariosv1.WorkPlace{Place: &scenariosv1.WorkPlace_State{State: "CA"}}, Supplemental: scenariosv1.SupplementalMethod_SUPPLEMENTAL_METHOD_FLAT,
		Parts:       parts,
		DefaultPart: &scenariosv1.EmployerDefault{Id: "acme-default", Rest: &scenariosv1.EmployerDefault_SameAsLatest{SameAsLatest: &scenariosv1.SameAsLatest{}}},
	}
}

// expand expands and projects one job with no max-out.
func expand(t *testing.T, ctx *model.Context, m *scenariosv1.EmployerModel, overrides ...*scenariosv1.Override) (*scenariosv1.EmployerProjection, *employer.Job) {
	t.Helper()
	j, err := employer.Expand(ctx, m, overrides)
	if err != nil {
		t.Fatal(err)
	}
	p, err := j.Projection()
	if err != nil {
		t.Fatal(err)
	}
	return p, j
}

func item(t *testing.T, p *scenariosv1.EmployerProjection, id string) *scenariosv1.Item {
	t.Helper()
	for _, it := range p.GetItems() {
		if it.GetId() == id {
			return it
		}
	}
	ids := []string{}
	for _, it := range p.GetItems() {
		ids = append(ids, it.GetId())
	}
	t.Fatalf("no item %s; items are %v", id, ids)
	return nil
}

func cents(m *commonv1.Money) money.USCents { return money.USCents(m.GetMinor()) }

func questionIDs(j *employer.Job) []string {
	var out []string
	for _, q := range j.Notes().Questions() {
		out = append(out, q.ID())
	}
	return out
}

func hasQuestion(j *employer.Job, id string) bool {
	for _, q := range questionIDs(j) {
		if q == id {
			return true
		}
	}
	return false
}

func dates(ds []civil.Date) []string {
	var out []string
	for _, d := range ds {
		out = append(out, d.String())
	}
	return out
}
