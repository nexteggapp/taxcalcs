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
	form1099g "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_g"
	form1099misc "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_misc"
	form1099q "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_q"
	form1099r "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_r"
	form1099s "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_s"
	form1099sa "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_sa"
	formssa1099 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_ssa_1099"
	formw2g "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2g"
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

func contextAt(t *testing.T, y int, m time.Month, d int) *model.Context {
	t.Helper()
	ctx, err := model.NewContext(y, date(y, m, d), nil)
	if err != nil {
		t.Fatalf("NewContext failed: %v", err)
	}
	return ctx
}

func docRef(id string) *factsv1.DocumentRef {
	return &factsv1.DocumentRef{Name: id}
}

func TestExpandForm1099MISC(t *testing.T) {
	ctx := contextAt(t, 2026, time.December, 31)

	for _, tc := range []struct {
		name        string
		form        *form1099misc.Form1099MISC
		wantGross   int64
		wantFed     int64
		wantState   int64
		wantLeftOut string
	}{
		{
			name: "rents and royalties only",
			form: &form1099misc.Form1099MISC{
				TaxYear:   2026,
				Rents:     usd(1000),
				Royalties: usd(500),
			},
			wantGross: 1500_00,
		},
		{
			name: "all income boxes with federal and multi-state withholding",
			form: &form1099misc.Form1099MISC{
				TaxYear:                      2026,
				Rents:                        usd(1000),
				Royalties:                    usd(500),
				OtherIncome:                  usd(200),
				FishingBoatProceeds:          usd(300),
				MedicalAndHealthCarePayments: usd(400),
				CropInsuranceProceeds:        usd(600),
				GrossProceedsPaidToAttorney:  usd(700),
				FishPurchasedForResale:       usd(100),
				FederalTaxWithheld:           usd(350),
				StateTaxWithholding: []*commonv1.StateTaxWithholding{
					{TaxWithheld: usd(100)},
					{TaxWithheld: usd(50)},
				},
			},
			wantGross: 3800_00,
			wantFed:   350_00,
			wantState: 150_00,
		},
		{
			name: "section 409A and NQDC emits left-out note",
			form: &form1099misc.Form1099MISC{
				TaxYear:                          2026,
				OtherIncome:                      usd(1000),
				NonqualifiedDeferredCompensation: usd(800),
				Section_409ADeferrals:            usd(200),
			},
			wantGross:   1800_00,
			wantLeftOut: "409A",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &scenariosv1.OtherIncomeModel{
				Id: "misc",
				Parts: []*scenariosv1.OtherIncomePart{{
					Id: "p-forms",
					Part: &scenariosv1.OtherIncomePart_FromForms_{
						FromForms: &scenariosv1.OtherIncomePart_FromForms{
							Forms: []*scenariosv1.OtherIncomePart_FromForms_Form{{
								Id:     "f-misc",
								Source: docRef("doc-misc"),
								Form: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099Misc{
									Form_1099Misc: tc.form,
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
			it := proj.GetYear().GetItems()
			if got := it.GetGross().GetMinor(); got != tc.wantGross {
				t.Errorf("Gross = %d, want %d", got, tc.wantGross)
			}
			if got := it.GetFederalWithheld().GetMinor(); got != tc.wantFed {
				t.Errorf("FederalWithheld = %d, want %d", got, tc.wantFed)
			}
			if got := it.GetStateWithheld().GetMinor(); got != tc.wantState {
				t.Errorf("StateWithheld = %d, want %d", got, tc.wantState)
			}
			if len(notes.Questions()) != 0 {
				t.Errorf("Questions = %v, want none", notes.Questions())
			}
			if tc.wantLeftOut != "" {
				var found bool
				for _, l := range notes.LeftOut() {
					if strings.Contains(l, tc.wantLeftOut) {
						found = true
					}
				}
				if !found {
					t.Errorf("LeftOut = %v, want substring %q", notes.LeftOut(), tc.wantLeftOut)
				}
			} else if len(notes.LeftOut()) != 0 {
				t.Errorf("LeftOut = %v, want none", notes.LeftOut())
			}
		})
	}
}

func TestExpandForm1099SA(t *testing.T) {
	ctx := contextAt(t, 2026, time.December, 31)

	for _, tc := range []struct {
		name              string
		gross             *commonv1.Money
		qualifiedExpenses *commonv1.Money
		rollover          *commonv1.Money
		wantGross         int64
		wantTaxable       int64
		wantPenaltyNote   bool
	}{
		{
			name:            "unset qualified expenses defaults to full distribution taxable",
			gross:           usd(2500),
			wantGross:       2500_00,
			wantTaxable:     2500_00,
			wantPenaltyNote: true,
		},
		{
			name:              "partial qualified medical expenses reduces taxable distribution",
			gross:             usd(2500),
			qualifiedExpenses: usd(1500),
			wantGross:         2500_00,
			wantTaxable:       1000_00,
			wantPenaltyNote:   true,
		},
		{
			name:              "full qualified medical expenses makes distribution tax-free",
			gross:             usd(2500),
			qualifiedExpenses: usd(2500),
			wantGross:         2500_00,
			wantTaxable:       0,
			wantPenaltyNote:   false,
		},
		{
			name:              "qualified medical expenses exceeding distribution floors taxable at zero",
			gross:             usd(2500),
			qualifiedExpenses: usd(3200),
			wantGross:         2500_00,
			wantTaxable:       0,
			wantPenaltyNote:   false,
		},
		{
			name:              "rollover and qualified expenses combined",
			gross:             usd(2500),
			rollover:          usd(500),
			qualifiedExpenses: usd(1200),
			wantGross:         2500_00,
			wantTaxable:       800_00,
			wantPenaltyNote:   true,
		},
		{
			name:            "full rollover makes distribution tax-free",
			gross:           usd(2500),
			rollover:        usd(2500),
			wantGross:       2500_00,
			wantTaxable:     0,
			wantPenaltyNote: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &scenariosv1.OtherIncomeModel{
				Id: "hsa-dist",
				Parts: []*scenariosv1.OtherIncomePart{{
					Id: "p-sa",
					Part: &scenariosv1.OtherIncomePart_FromForms_{
						FromForms: &scenariosv1.OtherIncomePart_FromForms{
							Forms: []*scenariosv1.OtherIncomePart_FromForms_Form{{
								Id:     "f-sa",
								Source: docRef("doc-sa"),
								Supplement: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099SaSupplement{
									Form_1099SaSupplement: &scenariosv1.OtherIncomePart_FromForms_Form_Form1099SASupplement{
										QualifiedExpenses: tc.qualifiedExpenses,
										Rollover:          tc.rollover,
									},
								},
								Form: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099Sa{
									Form_1099Sa: &form1099sa.Form1099SA{
										TaxYear:           2026,
										GrossDistribution: tc.gross,
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
			it := proj.GetYear().GetItems()
			if got := it.GetGross().GetMinor(); got != tc.wantGross {
				t.Errorf("Gross = %d, want %d", got, tc.wantGross)
			}
			if got := it.GetTaxable().GetMinor(); got != tc.wantTaxable {
				t.Errorf("Taxable = %d, want %d", got, tc.wantTaxable)
			}
			if len(notes.Questions()) != 0 {
				t.Errorf("Questions = %v, want none", notes.Questions())
			}
			hasPenaltyNote := false
			for _, l := range notes.LeftOut() {
				if strings.Contains(l, "20%") {
					hasPenaltyNote = true
				}
			}
			if hasPenaltyNote != tc.wantPenaltyNote {
				t.Errorf("hasPenaltyNote = %v, want %v (LeftOut: %v)", hasPenaltyNote, tc.wantPenaltyNote, notes.LeftOut())
			}
		})
	}
}

func TestExpandForm1099S(t *testing.T) {
	ctx := contextAt(t, 2026, time.December, 31)

	for _, tc := range []struct {
		name            string
		proceeds        *commonv1.Money
		basis           *commonv1.Money
		sellingExpenses *commonv1.Money
		excludedAmount  *commonv1.Money
		wantGross       int64
		wantExpenses    int64
		wantTaxable     int64
	}{
		{
			name:         "unset basis and exclusion counts full gross proceeds",
			proceeds:     usd(450000),
			wantGross:    450000_00,
			wantExpenses: 0,
			wantTaxable:  450000_00,
		},
		{
			name:            "basis and selling expenses reduce taxable gain",
			proceeds:        usd(500000),
			basis:           usd(300000),
			sellingExpenses: usd(25000),
			wantGross:       500000_00,
			wantExpenses:    325000_00,
			wantTaxable:     175000_00,
		},
		{
			name:            "basis, selling expenses, and partial section 121 exclusion",
			proceeds:        usd(800000),
			basis:           usd(250000),
			sellingExpenses: usd(30000),
			excludedAmount:  usd(250000),
			wantGross:       800000_00,
			wantExpenses:    280000_00,
			wantTaxable:     270000_00,
		},
		{
			name:            "section 121 exclusion covering entire gain results in zero taxable",
			proceeds:        usd(450000),
			basis:           usd(250000),
			sellingExpenses: usd(20000),
			excludedAmount:  usd(250000),
			wantGross:       450000_00,
			wantExpenses:    270000_00,
			wantTaxable:     0,
		},
		{
			name:            "basis exceeding gross proceeds floors gain and taxable at zero",
			proceeds:        usd(300000),
			basis:           usd(350000),
			sellingExpenses: usd(15000),
			wantGross:       300000_00,
			wantExpenses:    365000_00,
			wantTaxable:     0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &scenariosv1.OtherIncomeModel{
				Id: "home-sale",
				Parts: []*scenariosv1.OtherIncomePart{{
					Id: "p-s",
					Part: &scenariosv1.OtherIncomePart_FromForms_{
						FromForms: &scenariosv1.OtherIncomePart_FromForms{
							Forms: []*scenariosv1.OtherIncomePart_FromForms_Form{{
								Id:     "f-s",
								Source: docRef("doc-s"),
								Supplement: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099SSupplement{
									Form_1099SSupplement: &scenariosv1.OtherIncomePart_FromForms_Form_Form1099SSupplement{
										Basis:           tc.basis,
										SellingExpenses: tc.sellingExpenses,
										ExcludedAmount:  tc.excludedAmount,
									},
								},
								Form: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099S{
									Form_1099S: &form1099s.Form1099S{
										TaxYear:       2026,
										GrossProceeds: tc.proceeds,
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
			it := proj.GetYear().GetItems()
			if got := it.GetGross().GetMinor(); got != tc.wantGross {
				t.Errorf("Gross = %d, want %d", got, tc.wantGross)
			}
			if got := it.GetExpenses().GetMinor(); got != tc.wantExpenses {
				t.Errorf("Expenses = %d, want %d", got, tc.wantExpenses)
			}
			if got := it.GetTaxable().GetMinor(); got != tc.wantTaxable {
				t.Errorf("Taxable = %d, want %d", got, tc.wantTaxable)
			}
			if len(notes.Questions()) != 0 {
				t.Errorf("Questions = %v, want none", notes.Questions())
			}
		})
	}
}

func TestExpandForm1099C(t *testing.T) {
	ctx := contextAt(t, 2026, time.December, 31)

	for _, tc := range []struct {
		name           string
		discharged     *commonv1.Money
		excludedAmount *commonv1.Money
		wantGross      int64
		wantTaxable    int64
	}{
		{
			name:        "unset exclusion counts full discharged debt as taxable",
			discharged:  usd(12000),
			wantGross:   12000_00,
			wantTaxable: 12000_00,
		},
		{
			name:           "partial insolvency exclusion reduces taxable canceled debt",
			discharged:     usd(12000),
			excludedAmount: usd(7000),
			wantGross:      12000_00,
			wantTaxable:    5000_00,
		},
		{
			name:           "full bankruptcy or insolvency exclusion zeroes taxable canceled debt",
			discharged:     usd(12000),
			excludedAmount: usd(15000),
			wantGross:      12000_00,
			wantTaxable:    0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &scenariosv1.OtherIncomeModel{
				Id: "debt",
				Parts: []*scenariosv1.OtherIncomePart{{
					Id: "p-c",
					Part: &scenariosv1.OtherIncomePart_FromForms_{
						FromForms: &scenariosv1.OtherIncomePart_FromForms{
							Forms: []*scenariosv1.OtherIncomePart_FromForms_Form{{
								Id:     "f-c",
								Source: docRef("doc-c"),
								Supplement: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099CSupplement{
									Form_1099CSupplement: &scenariosv1.OtherIncomePart_FromForms_Form_Form1099CSupplement{
										ExcludedAmount: tc.excludedAmount,
									},
								},
								Form: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099C{
									Form_1099C: &form1099c.Form1099C{
										TaxYear:                 2026,
										AmountOfDebtDischarged:  tc.discharged,
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
			it := proj.GetYear().GetItems()
			if got := it.GetGross().GetMinor(); got != tc.wantGross {
				t.Errorf("Gross = %d, want %d", got, tc.wantGross)
			}
			if got := it.GetTaxable().GetMinor(); got != tc.wantTaxable {
				t.Errorf("Taxable = %d, want %d", got, tc.wantTaxable)
			}
			if len(notes.Questions()) != 0 {
				t.Errorf("Questions = %v, want none", notes.Questions())
			}
		})
	}
}

func TestExpandForm1099Q(t *testing.T) {
	ctx := contextAt(t, 2026, time.December, 31)

	for _, tc := range []struct {
		name              string
		gross             *commonv1.Money
		earnings          *commonv1.Money
		basis             *commonv1.Money
		qualifiedExpenses *commonv1.Money
		rollover          *commonv1.Money
		wantGross         int64
		wantTaxable       int64
		wantPenaltyNote   bool
	}{
		{
			name:            "unset qualified expenses taxes all earnings",
			gross:           usd(10000),
			earnings:        usd(3000),
			basis:           usd(7000),
			wantGross:       10000_00,
			wantTaxable:     3000_00,
			wantPenaltyNote: true,
		},
		{
			name:            "unset earnings and basis conservatively taxes full gross distribution",
			gross:           usd(10000),
			wantGross:       10000_00,
			wantTaxable:     10000_00,
			wantPenaltyNote: true,
		},
		{
			name:              "partial qualified education expenses pro-rates taxable earnings (Pub 970)",
			gross:             usd(10000),
			earnings:          usd(3000),
			basis:             usd(7000),
			qualifiedExpenses: usd(6000), // 40% non-qualified -> 40% of $3,000 = $1,200 taxable
			wantGross:         10000_00,
			wantTaxable:       1200_00,
			wantPenaltyNote:   true,
		},
		{
			name:              "rollover and partial qualified education expenses",
			gross:             usd(10000),
			earnings:          usd(3000),
			basis:             usd(7000),
			rollover:          usd(2000), // net distribution = $8,000
			qualifiedExpenses: usd(4000), // 50% of net covered -> 50% of $3,000 = $1,500 taxable
			wantGross:         10000_00,
			wantTaxable:       1500_00,
			wantPenaltyNote:   true,
		},
		{
			name:              "full qualified education expenses makes 529 distribution tax-free",
			gross:             usd(10000),
			earnings:          usd(3000),
			basis:             usd(7000),
			qualifiedExpenses: usd(10000),
			wantGross:         10000_00,
			wantTaxable:       0,
			wantPenaltyNote:   false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &scenariosv1.OtherIncomeModel{
				Id: "edu-dist",
				Parts: []*scenariosv1.OtherIncomePart{{
					Id: "p-q",
					Part: &scenariosv1.OtherIncomePart_FromForms_{
						FromForms: &scenariosv1.OtherIncomePart_FromForms{
							Forms: []*scenariosv1.OtherIncomePart_FromForms_Form{{
								Id:     "f-q",
								Source: docRef("doc-q"),
								Supplement: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099QSupplement{
									Form_1099QSupplement: &scenariosv1.OtherIncomePart_FromForms_Form_Form1099QSupplement{
										QualifiedExpenses: tc.qualifiedExpenses,
										Rollover:          tc.rollover,
									},
								},
								Form: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099Q{
									Form_1099Q: &form1099q.Form1099Q{
										TaxYear:           2026,
										GrossDistribution: tc.gross,
										Earnings:          tc.earnings,
										Basis:             tc.basis,
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
			it := proj.GetYear().GetItems()
			if got := it.GetGross().GetMinor(); got != tc.wantGross {
				t.Errorf("Gross = %d, want %d", got, tc.wantGross)
			}
			if got := it.GetTaxable().GetMinor(); got != tc.wantTaxable {
				t.Errorf("Taxable = %d, want %d", got, tc.wantTaxable)
			}
			if len(notes.Questions()) != 0 {
				t.Errorf("Questions = %v, want none", notes.Questions())
			}
			hasPenaltyNote := false
			for _, l := range notes.LeftOut() {
				if strings.Contains(l, "10%") {
					hasPenaltyNote = true
				}
			}
			if hasPenaltyNote != tc.wantPenaltyNote {
				t.Errorf("hasPenaltyNote = %v, want %v (LeftOut: %v)", hasPenaltyNote, tc.wantPenaltyNote, notes.LeftOut())
			}
		})
	}
}

func TestExpandScheduleK1(t *testing.T) {
	ctx := contextAt(t, 2026, time.December, 31)

	for _, tc := range []struct {
		name        string
		form        *scenariosv1.OtherIncomePart_FromForms_Form
		wantTaxable int64
		wantLeftOut string
	}{
		{
			name: "Schedule K-1 (Form 1065) partnership",
			form: &scenariosv1.OtherIncomePart_FromForms_Form{
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
			wantTaxable: 8900_00,
			wantLeftOut: "Partner basis limits",
		},
		{
			name: "Schedule K-1 (Form 1120-S) S corporation",
			form: &scenariosv1.OtherIncomePart_FromForms_Form{
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
			wantTaxable: 4300_00,
			wantLeftOut: "Shareholder basis limits",
		},
		{
			name: "Schedule K-1 (Form 1041) trust or estate",
			form: &scenariosv1.OtherIncomePart_FromForms_Form{
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
			wantTaxable: 2600_00,
			wantLeftOut: "Beneficiary basis limits",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &scenariosv1.OtherIncomeModel{
				Id: "k1",
				Parts: []*scenariosv1.OtherIncomePart{{
					Id: "p-k1",
					Part: &scenariosv1.OtherIncomePart_FromForms_{
						FromForms: &scenariosv1.OtherIncomePart_FromForms{
							Forms: []*scenariosv1.OtherIncomePart_FromForms_Form{tc.form},
						},
					},
				}},
			}

			proj, notes, err := otherincome.Expand(ctx, m)
			if err != nil {
				t.Fatalf("Expand failed: %v", err)
			}
			if got := proj.GetYear().GetItems().GetTaxable().GetMinor(); got != tc.wantTaxable {
				t.Errorf("Taxable = %d, want %d", got, tc.wantTaxable)
			}
			var found bool
			for _, l := range notes.LeftOut() {
				if strings.Contains(l, tc.wantLeftOut) {
					found = true
				}
			}
			if !found {
				t.Errorf("LeftOut = %v, want substring %q", notes.LeftOut(), tc.wantLeftOut)
			}
		})
	}
}

func TestExpandExistingFormsAndAssumptions(t *testing.T) {
	for _, tc := range []struct {
		name         string
		asOf         *commonv1.Date
		model        *scenariosv1.OtherIncomeModel
		wantGross    int64
		wantTaxable  int64
		wantFed      int64
		wantExpenses int64
		wantExpected string
		wantQuestion bool
	}{
		{
			name: "Form 1099-R with explicit taxable amount and IRA flag",
			asOf: date(2026, time.December, 31),
			model: &scenariosv1.OtherIncomeModel{
				Id:   "ret",
				Kind: scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_RETIREMENT_WITHDRAWALS,
				Parts: []*scenariosv1.OtherIncomePart{{
					Id: "p-r",
					Part: &scenariosv1.OtherIncomePart_FromForms_{
						FromForms: &scenariosv1.OtherIncomePart_FromForms{
							Forms: []*scenariosv1.OtherIncomePart_FromForms_Form{{
								Id:     "f-r",
								Source: docRef("doc-r"),
								Form: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099R{
									Form_1099R: &form1099r.Form1099R{
										TaxYear:            2026,
										GrossDistribution:  usd(10000),
										TaxableAmount:      usd(8500),
										FederalTaxWithheld: usd(1000),
										DistributionCodes:  []string{"7"},
										IraSepSimple:       true,
									},
								},
							}},
						},
					},
				}},
			},
			wantGross:   10000_00,
			wantTaxable: 8500_00,
			wantFed:     1000_00,
		},
		{
			name: "Form SSA-1099 Social Security benefits",
			asOf: date(2026, time.December, 31),
			model: &scenariosv1.OtherIncomeModel{
				Id:   "ssa",
				Kind: scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_SOCIAL_SECURITY,
				Parts: []*scenariosv1.OtherIncomePart{{
					Id: "p-ssa",
					Part: &scenariosv1.OtherIncomePart_FromForms_{
						FromForms: &scenariosv1.OtherIncomePart_FromForms{
							Forms: []*scenariosv1.OtherIncomePart_FromForms_Form{{
								Id:     "f-ssa",
								Source: docRef("doc-ssa"),
								Form: &scenariosv1.OtherIncomePart_FromForms_Form_FormSsa_1099{
									FormSsa_1099: &formssa1099.FormSSA1099{
										TaxYear:            2026,
										NetBenefits:        usd(24000),
										FederalTaxWithheld: usd(2400),
									},
								},
							}},
						},
					},
				}},
			},
			wantGross:   24000_00,
			wantTaxable: 24000_00,
			wantFed:     2400_00,
		},
		{
			name: "Form 1099-G unemployment and state refund",
			asOf: date(2026, time.December, 31),
			model: &scenariosv1.OtherIncomeModel{
				Id:   "edd",
				Kind: scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_UNEMPLOYMENT,
				Parts: []*scenariosv1.OtherIncomePart{{
					Id: "p-g",
					Part: &scenariosv1.OtherIncomePart_FromForms_{
						FromForms: &scenariosv1.OtherIncomePart_FromForms{
							Forms: []*scenariosv1.OtherIncomePart_FromForms_Form{{
								Id:     "f-g",
								Source: docRef("doc-g"),
								Form: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099G{
									Form_1099G: &form1099g.Form1099G{
										TaxYear:                  2026,
										UnemploymentCompensation: usd(6000),
										FederalTaxWithheld:       usd(600),
									},
								},
							}},
						},
					},
				}},
			},
			wantGross:   6000_00,
			wantTaxable: 6000_00,
			wantFed:     600_00,
		},
		{
			name: "Form W-2G gambling winnings",
			asOf: date(2026, time.December, 31),
			model: &scenariosv1.OtherIncomeModel{
				Id:   "casino",
				Kind: scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_GAMBLING,
				Parts: []*scenariosv1.OtherIncomePart{{
					Id: "p-w2g",
					Part: &scenariosv1.OtherIncomePart_FromForms_{
						FromForms: &scenariosv1.OtherIncomePart_FromForms{
							Forms: []*scenariosv1.OtherIncomePart_FromForms_Form{{
								Id:     "f-w2g",
								Source: docRef("doc-w2g"),
								Form: &scenariosv1.OtherIncomePart_FromForms_Form_FormW2G{
									FormW2G: &formw2g.FormW2G{
										TaxYear:            2026,
										ReportableWinnings: usd(5000),
										FederalTaxWithheld: usd(1200),
										DateWon:            date(2026, time.May, 10),
									},
								},
							}},
						},
					},
				}},
			},
			wantGross:   5000_00,
			wantTaxable: 5000_00,
			wantFed:     1200_00,
		},
		{
			name: "rental YearAmount with Schedule E line expenses and ledger credits",
			asOf: date(2026, time.June, 30),
			model: &scenariosv1.OtherIncomeModel{
				Id:   "rental",
				Kind: scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_RENTAL,
				Parts: []*scenariosv1.OtherIncomePart{
					{
						Id: "p-ledger",
						Part: &scenariosv1.OtherIncomePart_FromLedger_{
							FromLedger: &scenariosv1.OtherIncomePart_FromLedger{
								Credits: []*scenariosv1.OtherIncomePart_FromLedger_Credit{
									{Id: "c1", Date: date(2026, time.March, 1), Amount: usd(12000)},
								},
							},
						},
					},
					{
						Id: "p-year",
						Part: &scenariosv1.OtherIncomePart_YearAmount_{
							YearAmount: &scenariosv1.OtherIncomePart_YearAmount{
								Gross:     usd(24000),
								Recurring: true,
								RentalExpenses: &scenariosv1.RentalExpenses{
									Insurance:        usd(1200),
									MortgageInterest: usd(6000),
									Repairs:          usd(800),
									Taxes:            usd(2000),
								},
							},
						},
					},
				},
			},
			wantGross:    24000_00,
			wantTaxable:  24000_00,
			wantExpenses: 10000_00,
		},
		{
			name: "YearAmount stands aside when a whole-year form is present",
			asOf: date(2026, time.December, 31),
			model: &scenariosv1.OtherIncomeModel{
				Id:   "edd-conflict",
				Kind: scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_UNEMPLOYMENT,
				Parts: []*scenariosv1.OtherIncomePart{
					{
						Id: "p-g",
						Part: &scenariosv1.OtherIncomePart_FromForms_{
							FromForms: &scenariosv1.OtherIncomePart_FromForms{
								Forms: []*scenariosv1.OtherIncomePart_FromForms_Form{{
									Id:     "f-g",
									Source: docRef("doc-g"),
									Form: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099G{
										Form_1099G: &form1099g.Form1099G{
											TaxYear:                  2026,
											UnemploymentCompensation: usd(4000),
										},
									},
								}},
							},
						},
					},
					{
						Id: "p-year",
						Part: &scenariosv1.OtherIncomePart_YearAmount_{
							YearAmount: &scenariosv1.OtherIncomePart_YearAmount{
								Gross: usd(8000),
							},
						},
					},
				},
			},
			wantGross:    4000_00,
			wantTaxable:  4000_00,
			wantQuestion: true,
		},
		{
			name: "planned OneOff.HomeSale with basis and selling expenses but no exclusion",
			asOf: date(2026, time.June, 1),
			model: &scenariosv1.OtherIncomeModel{
				Id:   "home-plan",
				Kind: scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_HOME_SALE,
				Parts: []*scenariosv1.OtherIncomePart{{
					Id: "p-hs",
					Part: &scenariosv1.OtherIncomePart_OneOff_{
						OneOff: &scenariosv1.OtherIncomePart_OneOff{
							Date: date(2026, time.October, 15),
							Event: &scenariosv1.OtherIncomePart_OneOff_HomeSale_{
								HomeSale: &scenariosv1.OtherIncomePart_OneOff_HomeSale{
									Price:           usd(600000),
									Basis:           usd(350000),
									SellingExpenses: usd(30000),
								},
							},
						},
					},
				}},
			},
			wantGross:    600000_00,
			wantExpenses: 380000_00,
			wantTaxable:  220000_00,
		},
		{
			name: "planned OneOff.HomeSale with basis, selling expenses, and section 121 exclusion",
			asOf: date(2026, time.June, 1),
			model: &scenariosv1.OtherIncomeModel{
				Id:   "home-plan-excl",
				Kind: scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_HOME_SALE,
				Parts: []*scenariosv1.OtherIncomePart{{
					Id: "p-hs-excl",
					Part: &scenariosv1.OtherIncomePart_OneOff_{
						OneOff: &scenariosv1.OtherIncomePart_OneOff{
							Date: date(2026, time.October, 15),
							Event: &scenariosv1.OtherIncomePart_OneOff_HomeSale_{
								HomeSale: &scenariosv1.OtherIncomePart_OneOff_HomeSale{
									Price:           usd(700000),
									Basis:           usd(350000),
									SellingExpenses: usd(30000),
									ExcludedAmount:  usd(250000),
								},
							},
						},
					},
				}},
			},
			wantGross:    700000_00,
			wantExpenses: 380000_00,
			wantTaxable:  70000_00,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, err := model.NewContext(2026, tc.asOf, nil)
			if err != nil {
				t.Fatalf("NewContext failed: %v", err)
			}
			proj, notes, err := otherincome.Expand(ctx, tc.model)
			if err != nil {
				t.Fatalf("Expand failed: %v", err)
			}
			it := proj.GetYear().GetItems()
			if got := it.GetGross().GetMinor(); got != tc.wantGross {
				t.Errorf("Gross = %d, want %d", got, tc.wantGross)
			}
			if got := it.GetTaxable().GetMinor(); got != tc.wantTaxable {
				t.Errorf("Taxable = %d, want %d", got, tc.wantTaxable)
			}
			if got := it.GetFederalWithheld().GetMinor(); got != tc.wantFed {
				t.Errorf("FederalWithheld = %d, want %d", got, tc.wantFed)
			}
			if got := it.GetExpenses().GetMinor(); got != tc.wantExpenses {
				t.Errorf("Expenses = %d, want %d", got, tc.wantExpenses)
			}
			if got := len(notes.Questions()) > 0; got != tc.wantQuestion {
				t.Errorf("hasQuestions = %v, want %v (questions: %v)", got, tc.wantQuestion, notes.Questions())
			}
		})
	}
}
