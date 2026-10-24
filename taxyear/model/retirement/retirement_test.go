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

func TestExpandRetirement(t *testing.T) {
	ctx, err := model.NewContext(2026, date(2026, time.December, 31), nil)
	if err != nil {
		t.Fatalf("NewContext failed: %v", err)
	}

	for _, tc := range []struct {
		name          string
		parts         []*scenariosv1.RetirementPart
		wantItems     int
		wantSoFarTrad int64
		wantSoFarRoth int64
		wantSoFarHsa  int64
		wantRestTrad  int64
		wantRestRoth  int64
		wantRestHsa   int64
		wantYearTrad  int64
		wantYearRoth  int64
		wantYearHsa   int64
	}{
		{
			name: "assumed direct Traditional IRA, Roth IRA, and HSA contributions",
			parts: []*scenariosv1.RetirementPart{
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
			wantItems:    3,
			wantRestTrad: 3500_00,
			wantRestRoth: 3500_00,
			wantRestHsa:  2000_00,
			wantYearTrad: 3500_00,
			wantYearRoth: 3500_00,
			wantYearHsa:  2000_00,
		},
		{
			name: "Form 5498 Traditional and Roth IRA contributions",
			parts: []*scenariosv1.RetirementPart{{
				Id: "p-forms",
				Part: &scenariosv1.RetirementPart_FromForms_{
					FromForms: &scenariosv1.RetirementPart_FromForms{
						Forms: []*scenariosv1.RetirementPart_FromForms_Form{{
							Id:     "f-5498",
							Source: docRef("doc-5498"),
							Form: &scenariosv1.RetirementPart_FromForms_Form_Form_5498{
								Form_5498: &form5498.Form5498{
									IraContributions:     usd(4000),
									RothIraContributions: usd(3000),
								},
							},
						}},
					},
				},
			}},
			wantItems:     2,
			wantSoFarTrad: 4000_00,
			wantSoFarRoth: 3000_00,
			wantYearTrad:  4000_00,
			wantYearRoth:  3000_00,
		},
		{
			name: "Form 5498-SA HSA and Archer MSA contributions",
			parts: []*scenariosv1.RetirementPart{{
				Id: "p-forms",
				Part: &scenariosv1.RetirementPart_FromForms_{
					FromForms: &scenariosv1.RetirementPart_FromForms{
						Forms: []*scenariosv1.RetirementPart_FromForms_Form{{
							Id:     "f-5498sa",
							Source: docRef("doc-5498sa"),
							Form: &scenariosv1.RetirementPart_FromForms_Form_Form_5498Sa{
								Form_5498Sa: &form5498sa.Form5498SA{
									TotalContributionsMadeInYear:        usd(2500),
									ContributionsMadeInNextYearForYear: usd(1500),
									ArcherMsaContributions:             usd(400),
								},
							},
						}},
					},
				},
			}},
			wantItems:    1,
			wantSoFarHsa: 4400_00,
			wantYearHsa:  4400_00,
		},
		{
			name: "combined Form 5498, Form 5498-SA, and assumed contributions",
			parts: []*scenariosv1.RetirementPart{
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
											IraContributions: usd(5000),
										},
									},
								},
								{
									Id:     "f-5498sa",
									Source: docRef("doc-5498sa"),
									Form: &scenariosv1.RetirementPart_FromForms_Form_Form_5498Sa{
										Form_5498Sa: &form5498sa.Form5498SA{
											TotalContributionsMadeInYear: usd(3000),
										},
									},
								},
							},
						},
					},
				},
				{
					Id: "p-trad-next-apr",
					Part: &scenariosv1.RetirementPart_IraContribution_{
						IraContribution: &scenariosv1.RetirementPart_IraContribution{
							Date:   date(2027, time.April, 10),
							Kind:   scenariosv1.IraKind_IRA_KIND_TRADITIONAL,
							Amount: usd(2000),
						},
					},
				},
				{
					Id: "p-hsa-direct",
					Part: &scenariosv1.RetirementPart_HsaContribution_{
						HsaContribution: &scenariosv1.RetirementPart_HsaContribution{
							Amount: usd(1400), // unset date defaults to YearEnd
						},
					},
				},
			},
			wantItems:     4,
			wantSoFarTrad: 5000_00,
			wantSoFarHsa:  3000_00,
			wantRestTrad:  2000_00,
			wantRestHsa:   1400_00,
			wantYearTrad:  7000_00,
			wantYearHsa:   4400_00,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &scenariosv1.RetirementModel{
				Id:     "ret",
				Person: "you",
				Parts:  tc.parts,
			}
			proj, err := retirement.Expand(ctx, m)
			if err != nil {
				t.Fatalf("Expand failed: %v", err)
			}
			if len(proj.GetItems()) != tc.wantItems {
				t.Fatalf("len(Items) = %d, want %d", len(proj.GetItems()), tc.wantItems)
			}

			soFar := proj.GetSoFar().GetItems()
			if got := soFar.GetTraditionalIra().GetMinor(); got != tc.wantSoFarTrad {
				t.Errorf("SoFar TraditionalIra = %d, want %d", got, tc.wantSoFarTrad)
			}
			if got := soFar.GetRothIra().GetMinor(); got != tc.wantSoFarRoth {
				t.Errorf("SoFar RothIra = %d, want %d", got, tc.wantSoFarRoth)
			}
			if got := soFar.GetHsa().GetMinor(); got != tc.wantSoFarHsa {
				t.Errorf("SoFar Hsa = %d, want %d", got, tc.wantSoFarHsa)
			}

			rest := proj.GetRest().GetItems()
			if got := rest.GetTraditionalIra().GetMinor(); got != tc.wantRestTrad {
				t.Errorf("Rest TraditionalIra = %d, want %d", got, tc.wantRestTrad)
			}
			if got := rest.GetRothIra().GetMinor(); got != tc.wantRestRoth {
				t.Errorf("Rest RothIra = %d, want %d", got, tc.wantRestRoth)
			}
			if got := rest.GetHsa().GetMinor(); got != tc.wantRestHsa {
				t.Errorf("Rest Hsa = %d, want %d", got, tc.wantRestHsa)
			}

			year := proj.GetYear().GetItems()
			if got := year.GetTraditionalIra().GetMinor(); got != tc.wantYearTrad {
				t.Errorf("Year TraditionalIra = %d, want %d", got, tc.wantYearTrad)
			}
			if got := year.GetRothIra().GetMinor(); got != tc.wantYearRoth {
				t.Errorf("Year RothIra = %d, want %d", got, tc.wantYearRoth)
			}
			if got := year.GetHsa().GetMinor(); got != tc.wantYearHsa {
				t.Errorf("Year Hsa = %d, want %d", got, tc.wantYearHsa)
			}
		})
	}
}

func TestPlanPart(t *testing.T) {
	for _, tc := range []struct {
		name       string
		parts      []*scenariosv1.RetirementPart
		wantPartID string
	}{
		{
			name: "MaxOutWorkplacePlan returned",
			parts: []*scenariosv1.RetirementPart{{
				Id: "p-maxout",
				Part: &scenariosv1.RetirementPart_MaxOutWorkplacePlan_{
					MaxOutWorkplacePlan: &scenariosv1.RetirementPart_MaxOutWorkplacePlan{},
				},
			}},
			wantPartID: "p-maxout",
		},
		{
			name: "WorkplacePlanTotal returned",
			parts: []*scenariosv1.RetirementPart{{
				Id: "p-total",
				Part: &scenariosv1.RetirementPart_WorkplacePlanTotal_{
					WorkplacePlanTotal: &scenariosv1.RetirementPart_WorkplacePlanTotal{Total: usd(23000)},
				},
			}},
			wantPartID: "p-total",
		},
		{
			name: "non-workplace parts return nil",
			parts: []*scenariosv1.RetirementPart{{
				Id: "p-ira",
				Part: &scenariosv1.RetirementPart_IraContribution_{
					IraContribution: &scenariosv1.RetirementPart_IraContribution{},
				},
			}},
			wantPartID: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &scenariosv1.RetirementModel{Id: "ret", Parts: tc.parts}
			got := retirement.PlanPart(m)
			if tc.wantPartID == "" {
				if got != nil {
					t.Errorf("PlanPart = %v, want nil", got)
				}
			} else if got.GetId() != tc.wantPartID {
				t.Errorf("PlanPart.Id = %q, want %q", got.GetId(), tc.wantPartID)
			}
		})
	}
}
