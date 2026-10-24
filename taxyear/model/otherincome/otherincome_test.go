package otherincome_test

import (
	"strings"
	"testing"
	"time"

	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	"github.com/nexteggapp/taxcalcs/taxyear/model/otherincome"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
	form1099c "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_c"
	form1099misc "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_misc"
	form1099q "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_q"
	form1099s "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_s"
	form1099sa "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_sa"
	schedulek1 "github.com/nexteggapp/taxprotos/go/us/federal/v1/schedule_k1"
)

func usd(major int64) *commonv1.Money {
	return &commonv1.Money{
		Currency: "USD",
		Minor:    major * 100,
	}
}

func date(y int, m time.Month, d int) *commonv1.Date {
	return &commonv1.Date{
		Year:  int32(y),
		Month: int32(m),
		Day:   int32(d),
	}
}

func context2026(t *testing.T) *model.Context {
	ctx, err := model.NewContext(2026, date(2026, time.December, 31), nil)
	if err != nil {
		t.Fatalf("NewContext failed: %v", err)
	}
	return ctx
}

func docRef(id string) *factsv1.DocumentRef {
	return &factsv1.DocumentRef{Name: id}
}

func TestExpandForm1099MISC(t *testing.T) {
	ctx := context2026(t)
	m := &scenariosv1.OtherIncomeModel{
		Id:    "misc-income",
		Payer: "Various",
		Parts: []*scenariosv1.OtherIncomePart{{
			Id: "p1",
			Part: &scenariosv1.OtherIncomePart_FromForms_{
				FromForms: &scenariosv1.OtherIncomePart_FromForms{
					Forms: []*scenariosv1.OtherIncomePart_FromForms_Form{{
						Id:     "f1",
						Source: docRef("doc-misc-1"),
						Form: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099Misc{
							Form_1099Misc: &form1099misc.Form1099MISC{
								TaxYear:                         2026,
								Rents:                           usd(1200),
								Royalties:                       usd(300),
								OtherIncome:                     usd(500),
								FishingBoatProceeds:             usd(100),
								MedicalAndHealthCarePayments:    usd(250),
								CropInsuranceProceeds:           usd(150),
								GrossProceedsPaidToAttorney:     usd(800),
								NonqualifiedDeferredCompensation: usd(400),
								FederalTaxWithheld:              usd(350),
								StateTaxWithholding: []*commonv1.StateTaxWithholding{{
									State:       "OR",
									TaxWithheld: usd(75),
								}},
							},
						},
					}},
				},
			},
		}},
	}

	proj, notes, err := otherincome.Expand(ctx, m)
	if err != nil {
		t.Fatalf("Expand failed: %v", err)
	}

	if len(proj.GetItems()) != 1 {
		t.Fatalf("Got %d items, want 1", len(proj.GetItems()))
	}

	item := proj.GetItems()[0].GetOtherIncome()
	// Gross = 1200+300+500+100+250+150+800+400 = 3700
	if got := item.GetGross().GetMinor(); got != 370000 {
		t.Errorf("Gross = %d, want 370000 ($3700.00)", got)
	}
	if got := item.GetFederalWithheld().GetMinor(); got != 35000 {
		t.Errorf("FederalWithheld = %d, want 35000 ($350.00)", got)
	}
	if got := item.GetStateWithheld().GetMinor(); got != 7500 {
		t.Errorf("StateWithheld = %d, want 7500 ($75.00)", got)
	}

	// Should emit a note regarding § 409A penalties
	hasNote := false
	for _, n := range notes.LeftOut() {
		if strings.Contains(n, "409A") {
			hasNote = true
			break
		}
	}
	if !hasNote {
		t.Errorf("Expected note regarding § 409A / golden parachute, got %v", notes.LeftOut())
	}
}

func TestExpandForm1099SA(t *testing.T) {
	ctx := context2026(t)
	m := &scenariosv1.OtherIncomeModel{
		Id:    "hsa-dist",
		Payer: "HSA Bank",
		Parts: []*scenariosv1.OtherIncomePart{{
			Id: "p1",
			Part: &scenariosv1.OtherIncomePart_FromForms_{
				FromForms: &scenariosv1.OtherIncomePart_FromForms{
					Forms: []*scenariosv1.OtherIncomePart_FromForms_Form{{
						Id:     "f1",
						Source: docRef("doc-sa"),
						Form: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099Sa{
							Form_1099Sa: &form1099sa.Form1099SA{
								TaxYear:           2026,
								GrossDistribution: usd(2500),
							},
						},
					}},
				},
			},
		}},
	}

	proj, notes, err := otherincome.Expand(ctx, m)
	if err != nil {
		t.Fatalf("Expand failed: %v", err)
	}

	if len(proj.GetItems()) != 1 {
		t.Fatalf("Got %d items, want 1", len(proj.GetItems()))
	}
	item := proj.GetItems()[0].GetOtherIncome()
	if got := item.GetGross().GetMinor(); got != 250000 {
		t.Errorf("Gross = %d, want 250000", got)
	}
	if got := item.GetTaxable().GetMinor(); got != 250000 {
		t.Errorf("Taxable = %d, want 250000", got)
	}

	// Question regarding qualified medical expenses must be asked
	hasQuestion := false
	for _, q := range notes.Questions() {
		if strings.Contains(q.Proto().GetQuestion().GetText(), "qualified medical expenses") {
			hasQuestion = true
			break
		}
	}
	if !hasQuestion {
		t.Errorf("Expected question for qualified medical expenses, got: %v", notes.Questions())
	}

	// Note regarding 20% penalty must be emitted
	hasNote := false
	for _, n := range notes.LeftOut() {
		if strings.Contains(n, "20% additional tax") {
			hasNote = true
			break
		}
	}
	if !hasNote {
		t.Errorf("Expected note for 20% additional tax, got: %v", notes.LeftOut())
	}
}

func TestExpandForm1099S(t *testing.T) {
	ctx := context2026(t)
	m := &scenariosv1.OtherIncomeModel{
		Id:    "re-sale",
		Payer: "Title Company",
		Parts: []*scenariosv1.OtherIncomePart{{
			Id: "p1",
			Part: &scenariosv1.OtherIncomePart_FromForms_{
				FromForms: &scenariosv1.OtherIncomePart_FromForms{
					Forms: []*scenariosv1.OtherIncomePart_FromForms_Form{{
						Id:     "f1",
						Source: docRef("doc-s"),
						Form: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099S{
							Form_1099S: &form1099s.Form1099S{
								TaxYear:       2026,
								GrossProceeds: usd(450000),
								DateOfClosing: date(2026, time.June, 15),
							},
						},
					}},
				},
			},
		}},
	}

	proj, notes, err := otherincome.Expand(ctx, m)
	if err != nil {
		t.Fatalf("Expand failed: %v", err)
	}

	if len(proj.GetItems()) != 1 {
		t.Fatalf("Got %d items, want 1", len(proj.GetItems()))
	}
	it := proj.GetItems()[0]
	if it.GetDate().GetDay() != 15 || it.GetDate().GetMonth() != 6 {
		t.Errorf("Expected date 2026-06-15, got %v", it.GetDate())
	}
	if got := it.GetOtherIncome().GetGross().GetMinor(); got != 45000000 {
		t.Errorf("Gross = %d, want 45000000", got)
	}

	hasQuestion := false
	for _, q := range notes.Questions() {
		if strings.Contains(q.Proto().GetQuestion().GetText(), "cost basis") {
			hasQuestion = true
			break
		}
	}
	if !hasQuestion {
		t.Errorf("Expected cost basis question, got: %v", notes.Questions())
	}
}

func TestExpandForm1099C(t *testing.T) {
	ctx := context2026(t)
	m := &scenariosv1.OtherIncomeModel{
		Id:    "canceled-debt",
		Payer: "Bank of Debt",
		Parts: []*scenariosv1.OtherIncomePart{{
			Id: "p1",
			Part: &scenariosv1.OtherIncomePart_FromForms_{
				FromForms: &scenariosv1.OtherIncomePart_FromForms{
					Forms: []*scenariosv1.OtherIncomePart_FromForms_Form{{
						Id:     "f1",
						Source: docRef("doc-c"),
						Form: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099C{
							Form_1099C: &form1099c.Form1099C{
								TaxYear:                 2026,
								AmountOfDebtDischarged:  usd(12000),
								DateOfIdentifiableEvent: date(2026, time.September, 1),
							},
						},
					}},
				},
			},
		}},
	}

	proj, notes, err := otherincome.Expand(ctx, m)
	if err != nil {
		t.Fatalf("Expand failed: %v", err)
	}

	if len(proj.GetItems()) != 1 {
		t.Fatalf("Got %d items, want 1", len(proj.GetItems()))
	}
	it := proj.GetItems()[0]
	if it.GetDate().GetMonth() != 9 || it.GetDate().GetDay() != 1 {
		t.Errorf("Expected date 2026-09-01, got %v", it.GetDate())
	}
	if got := it.GetOtherIncome().GetTaxable().GetMinor(); got != 1200000 {
		t.Errorf("Taxable = %d, want 1200000", got)
	}

	hasQuestion := false
	for _, q := range notes.Questions() {
		if strings.Contains(q.Proto().GetQuestion().GetText(), "insolvency") {
			hasQuestion = true
			break
		}
	}
	if !hasQuestion {
		t.Errorf("Expected insolvency question, got: %v", notes.Questions())
	}
}

func TestExpandForm1099Q(t *testing.T) {
	ctx := context2026(t)
	m := &scenariosv1.OtherIncomeModel{
		Id:    "college-fund",
		Payer: "529 Plan",
		Parts: []*scenariosv1.OtherIncomePart{{
			Id: "p1",
			Part: &scenariosv1.OtherIncomePart_FromForms_{
				FromForms: &scenariosv1.OtherIncomePart_FromForms{
					Forms: []*scenariosv1.OtherIncomePart_FromForms_Form{{
						Id:     "f1",
						Source: docRef("doc-q"),
						Form: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099Q{
							Form_1099Q: &form1099q.Form1099Q{
								TaxYear:           2026,
								GrossDistribution: usd(10000),
								Earnings:          usd(3000),
							},
						},
					}},
				},
			},
		}},
	}

	proj, notes, err := otherincome.Expand(ctx, m)
	if err != nil {
		t.Fatalf("Expand failed: %v", err)
	}

	if len(proj.GetItems()) != 1 {
		t.Fatalf("Got %d items, want 1", len(proj.GetItems()))
	}
	item := proj.GetItems()[0].GetOtherIncome()
	if got := item.GetGross().GetMinor(); got != 1000000 {
		t.Errorf("Gross = %d, want 1000000", got)
	}
	if got := item.GetTaxable().GetMinor(); got != 300000 {
		t.Errorf("Taxable = %d, want 300000", got)
	}

	hasQuestion := false
	for _, q := range notes.Questions() {
		if strings.Contains(q.Proto().GetQuestion().GetText(), "higher education expenses") {
			hasQuestion = true
			break
		}
	}
	if !hasQuestion {
		t.Errorf("Expected education expense question, got: %v", notes.Questions())
	}
}

func TestExpandScheduleK1(t *testing.T) {
	ctx := context2026(t)
	m := &scenariosv1.OtherIncomeModel{
		Id:    "k1-pass-through",
		Payer: "Partnership, LLC",
		Parts: []*scenariosv1.OtherIncomePart{{
			Id: "p1",
			Part: &scenariosv1.OtherIncomePart_FromForms_{
				FromForms: &scenariosv1.OtherIncomePart_FromForms{
					Forms: []*scenariosv1.OtherIncomePart_FromForms_Form{
						{
							Id:     "f-1065",
							Source: docRef("doc-1065"),
							Form: &scenariosv1.OtherIncomePart_FromForms_Form_ScheduleK1_1065{
								ScheduleK1_1065: &schedulek1.ScheduleK1Form1065{
									TaxYear:                   2026,
									OrdinaryBusinessIncome:    usd(5000),
									NetRentalRealEstateIncome: usd(2000),
									OtherNetRentalIncome:      usd(500),
									TotalGuaranteedPayments:   usd(1000),
									Royalties:                 usd(400),
								},
							},
						},
						{
							Id:     "f-1120s",
							Source: docRef("doc-1120s"),
							Form: &scenariosv1.OtherIncomePart_FromForms_Form_ScheduleK1_1120S{
								ScheduleK1_1120S: &schedulek1.ScheduleK1Form1120S{
									TaxYear:                   2026,
									OrdinaryBusinessIncome:    usd(3000),
									NetRentalRealEstateIncome: usd(1000),
									OtherNetRentalIncome:      usd(200),
									Royalties:                 usd(100),
								},
							},
						},
						{
							Id:     "f-1041",
							Source: docRef("doc-1041"),
							Form: &scenariosv1.OtherIncomePart_FromForms_Form_ScheduleK1_1041{
								ScheduleK1_1041: &schedulek1.ScheduleK1Form1041{
									TaxYear:                            2026,
									OrdinaryBusinessIncome:             usd(1500),
									NetRentalRealEstateIncome:          usd(500),
									OtherRentalIncome:                  usd(250),
									OtherPortfolioAndNonbusinessIncome: usd(350),
								},
							},
						},
					},
				},
			},
		}},
	}

	proj, notes, err := otherincome.Expand(ctx, m)
	if err != nil {
		t.Fatalf("Expand failed: %v", err)
	}

	if len(proj.GetItems()) != 3 {
		t.Fatalf("Got %d items, want 3", len(proj.GetItems()))
	}

	byOrigin := map[string]int64{}
	for _, it := range proj.GetItems() {
		for _, ev := range it.GetOrigin().GetEvidence() {
			byOrigin[ev] = it.GetOtherIncome().GetTaxable().GetMinor()
		}
	}

	// 1065: 5000+2000+500+1000+400 = 8900
	if got := byOrigin["f-1065"]; got != 890000 {
		t.Errorf("1065 Taxable = %d, want 890000", got)
	}
	// 1120-S: 3000+1000+200+100 = 4300
	if got := byOrigin["f-1120s"]; got != 430000 {
		t.Errorf("1120S Taxable = %d, want 430000", got)
	}
	// 1041: 1500+500+250+350 = 2600
	if got := byOrigin["f-1041"]; got != 260000 {
		t.Errorf("1041 Taxable = %d, want 260000", got)
	}

	// Verify basis limit notes are emitted
	if len(notes.LeftOut()) < 3 {
		t.Errorf("Expected at least 3 NotIncluded notes for K-1 basis limits, got: %d", len(notes.LeftOut()))
	}
}
