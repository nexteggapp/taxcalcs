package retirement_test

import (
	"testing"
	"time"

	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	"github.com/nexteggapp/taxcalcs/taxyear/model/retirement"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
	form5498 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_5498"
	form5498sa "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_5498_sa"
)

func usd(dollars int64) *commonv1.Money {
	return &commonv1.Money{Minor: dollars * 100, Currency: "USD"}
}

func date(year int, month time.Month, day int) *commonv1.Date {
	return &commonv1.Date{
		Year:  int32(year),
		Month: int32(month),
		Day:   int32(day),
	}
}

func docRef(id string) *factsv1.DocumentRef {
	return &factsv1.DocumentRef{Name: id}
}

func contextAt(t *testing.T, y int, m time.Month, d int) *model.Context {
	ctx, err := model.NewContext(y, date(y, m, d), nil)
	if err != nil {
		t.Fatalf("NewContext failed: %v", err)
	}
	return ctx
}

func TestExpandDirectContributions(t *testing.T) {
	ctx := contextAt(t, 2026, time.December, 31)

	m := &scenariosv1.RetirementModel{
		Id:     "ret-direct",
		Person: "person-1",
		Parts: []*scenariosv1.RetirementPart{
			{
				Id: "p-trad",
				Part: &scenariosv1.RetirementPart_IraContribution_{
					IraContribution: &scenariosv1.RetirementPart_IraContribution{
						Date:   date(2026, time.April, 1),
						Kind:   scenariosv1.IraKind_IRA_KIND_TRADITIONAL,
						Amount: usd(3500),
					},
				},
			},
			{
				Id: "p-roth",
				Part: &scenariosv1.RetirementPart_IraContribution_{
					IraContribution: &scenariosv1.RetirementPart_IraContribution{
						Date:   date(2026, time.May, 1),
						Kind:   scenariosv1.IraKind_IRA_KIND_ROTH,
						Amount: usd(3500),
					},
				},
			},
			{
				Id: "p-hsa",
				Part: &scenariosv1.RetirementPart_HsaContribution_{
					HsaContribution: &scenariosv1.RetirementPart_HsaContribution{
						Date:   date(2026, time.June, 1),
						Amount: usd(2000),
					},
				},
			},
		},
	}

	proj, err := retirement.Expand(ctx, m)
	if err != nil {
		t.Fatalf("Expand failed: %v", err)
	}

	if len(proj.GetItems()) != 3 {
		t.Fatalf("Expected 3 items, got %d", len(proj.GetItems()))
	}

	rest := proj.GetRest().GetItems()
	if got := rest.GetTraditionalIra().GetMinor(); got != 350000 {
		t.Errorf("Traditional IRA = %d, want 350000", got)
	}
	if got := rest.GetRothIra().GetMinor(); got != 350000 {
		t.Errorf("Roth IRA = %d, want 350000", got)
	}
	if got := rest.GetHsa().GetMinor(); got != 200000 {
		t.Errorf("HSA = %d, want 200000", got)
	}

	year := proj.GetYear().GetItems()
	if got := year.GetTraditionalIra().GetMinor(); got != 350000 {
		t.Errorf("Year Traditional IRA = %d, want 350000", got)
	}
	if got := year.GetRothIra().GetMinor(); got != 350000 {
		t.Errorf("Year Roth IRA = %d, want 350000", got)
	}
	if got := year.GetHsa().GetMinor(); got != 200000 {
		t.Errorf("Year HSA = %d, want 200000", got)
	}
}

func TestExpandForms5498And5498SA(t *testing.T) {
	ctx := contextAt(t, 2026, time.December, 31)

	m := &scenariosv1.RetirementModel{
		Id:     "ret-forms",
		Person: "person-2",
		Parts: []*scenariosv1.RetirementPart{
			{
				Id: "p-forms",
				Part: &scenariosv1.RetirementPart_FromForms_{
					FromForms: &scenariosv1.RetirementPart_FromForms{
						Forms: []*scenariosv1.RetirementPart_FromForms_Form{
							{
								Id:     "f-5498",
								Source: docRef("doc-5498"),
								Form: &scenariosv1.RetirementPart_FromForms_Form_Form_5498{
									Form_5498: &form5498.Form5498{
										IraContributions:     usd(4000),
										RothIraContributions: usd(3000),
									},
								},
							},
							{
								Id:     "f-5498sa",
								Source: docRef("doc-5498sa"),
								Form: &scenariosv1.RetirementPart_FromForms_Form_Form_5498Sa{
									Form_5498Sa: &form5498sa.Form5498SA{
										TotalContributionsMadeInYear:        usd(2500),
										ContributionsMadeInNextYearForYear: usd(1500),
										ArcherMsaContributions:             usd(400),
									},
								},
							},
						},
					},
				},
			},
		},
	}

	proj, err := retirement.Expand(ctx, m)
	if err != nil {
		t.Fatalf("Expand failed: %v", err)
	}

	// f-5498 emits 2 items (trad + roth), f-5498sa emits 1 item (hsa sum = 2500 + 1500 + 400 = 4400)
	if len(proj.GetItems()) != 3 {
		t.Fatalf("Expected 3 items, got %d", len(proj.GetItems()))
	}

	soFar := proj.GetSoFar().GetItems()
	if got := soFar.GetTraditionalIra().GetMinor(); got != 400000 {
		t.Errorf("SoFar Traditional IRA = %d, want 400000", got)
	}
	if got := soFar.GetRothIra().GetMinor(); got != 300000 {
		t.Errorf("SoFar Roth IRA = %d, want 300000", got)
	}
	if got := soFar.GetHsa().GetMinor(); got != 440000 {
		t.Errorf("SoFar HSA = %d, want 440000", got)
	}

	year := proj.GetYear().GetItems()
	if got := year.GetTraditionalIra().GetMinor(); got != 400000 {
		t.Errorf("Year Traditional IRA = %d, want 400000", got)
	}
	if got := year.GetRothIra().GetMinor(); got != 300000 {
		t.Errorf("Year Roth IRA = %d, want 300000", got)
	}
	if got := year.GetHsa().GetMinor(); got != 440000 {
		t.Errorf("Year HSA = %d, want 440000", got)
	}
}

func TestPlanPart(t *testing.T) {
	p1 := &scenariosv1.RetirementPart{
		Id: "p-maxout",
		Part: &scenariosv1.RetirementPart_MaxOutWorkplacePlan_{
			MaxOutWorkplacePlan: &scenariosv1.RetirementPart_MaxOutWorkplacePlan{},
		},
	}
	m1 := &scenariosv1.RetirementModel{
		Id:    "m1",
		Parts: []*scenariosv1.RetirementPart{p1},
	}
	if got := retirement.PlanPart(m1); got != p1 {
		t.Errorf("PlanPart(m1) = %v, want %v", got, p1)
	}

	p2 := &scenariosv1.RetirementPart{
		Id: "p-total",
		Part: &scenariosv1.RetirementPart_WorkplacePlanTotal_{
			WorkplacePlanTotal: &scenariosv1.RetirementPart_WorkplacePlanTotal{
				Total: usd(23000),
			},
		},
	}
	m2 := &scenariosv1.RetirementModel{
		Id:    "m2",
		Parts: []*scenariosv1.RetirementPart{p2},
	}
	if got := retirement.PlanPart(m2); got != p2 {
		t.Errorf("PlanPart(m2) = %v, want %v", got, p2)
	}

	m3 := &scenariosv1.RetirementModel{
		Id: "m3",
		Parts: []*scenariosv1.RetirementPart{
			{
				Id: "p-ira",
				Part: &scenariosv1.RetirementPart_IraContribution_{
					IraContribution: &scenariosv1.RetirementPart_IraContribution{},
				},
			},
		},
	}
	if got := retirement.PlanPart(m3); got != nil {
		t.Errorf("PlanPart(m3) = %v, want nil", got)
	}
}
