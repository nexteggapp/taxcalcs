package taxyear_test

import (
	"strings"
	"testing"

	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
	form1099c "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_c"
	form1099misc "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_misc"
	form1099oid "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_oid"
	form1099q "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_q"
	form1099s "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_s"
	form1099sa "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_sa"
	form5498 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_5498"
	form5498sa "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_5498_sa"
	schedulek1 "github.com/nexteggapp/taxprotos/go/us/federal/v1/schedule_k1"
)

func docRef(id string) *factsv1.DocumentRef {
	return &factsv1.DocumentRef{Name: id}
}

// TestForm1099OIDInAccountModel verifies that Form 1099-OID attached to an
// AccountModel expands interest, treasury interest, and tax-exempt interest.
func TestForm1099OIDInAccountModel(t *testing.T) {
	f := others()
	oid := &form1099oid.Form1099OID{
		TaxYear:               2026,
		OriginalIssueDiscount: usd(1000),
		OtherPeriodicInterest: usd(200),
		MarketDiscount:        usd(50),
		AcquisitionPremium:    usd(30),
		OriginalIssueDiscountOnTreasuryObligations: usd(300),
		TaxExemptOid: usd(150),
	}
	f.GetShared().GetModel().Accounts = []*scenariosv1.AccountModel{{
		Id:   "bank-oid",
		Name: "Treasury Bank",
		Parts: []*scenariosv1.AccountPart{{
			Id: "p1",
			Part: &scenariosv1.AccountPart_FromForms_{
				FromForms: &scenariosv1.AccountPart_FromForms{
					Forms: []*scenariosv1.AccountPart_FromForms_Form{{
						Id:     "oid-1",
						Source: docRef("doc-oid-1"),
						Form:   &scenariosv1.AccountPart_FromForms_Form_Form_1099Oid{Form_1099Oid: oid},
					}},
				},
			},
		}},
		DefaultPart: &scenariosv1.AccountDefault{
			Id:   "bank-d",
			Rest: &scenariosv1.AccountDefault_PaceSoFar_{PaceSoFar: &scenariosv1.AccountDefault_PaceSoFar{}},
		},
	}}
	r, err := taxyear.Compute(f)
	if err != nil {
		t.Fatalf("Compute failed: %v", err)
	}
	res := r.Proto().GetScenarios()[0]
	if p := res.GetProblems(); len(p) > 0 {
		t.Fatalf("unexpected problems: %v", p)
	}
	// Taxable interest = (1000 + 200 + 50 - 30) + 300 = 1520
	taxableInterest := res.GetFederal().GetForm_1040().GetTaxableInterest().GetMinor()
	if taxableInterest != 152000 {
		t.Errorf("TaxableInterest = %d, want 152000 ($1520.00)", taxableInterest)
	}
	taxExempt := res.GetFederal().GetForm_1040().GetTaxExemptInterest().GetMinor()
	if taxExempt != 15000 {
		t.Errorf("TaxExemptInterest = %d, want 15000 ($150.00)", taxExempt)
	}
}

// TestForm1099MISCInBusinessModel verifies that Form 1099-MISC attached to a
// BusinessModel reconciles receipts properly.
func TestForm1099MISCInBusinessModel(t *testing.T) {
	f := others()
	misc := &form1099misc.Form1099MISC{
		TaxYear:     2026,
		Payer:       &commonv1.Party{Name: "ClientCorp"},
		OtherIncome: usd(6000),
		Rents:       usd(2000),
	}
	f.GetShared().GetModel().Businesses = []*scenariosv1.BusinessModel{{
		Id:     "biz-misc",
		Name:   "Consulting",
		Person: "you",
		Parts: []*scenariosv1.BusinessPart{{
			Id: "p1",
			Part: &scenariosv1.BusinessPart_FromForms_{
				FromForms: &scenariosv1.BusinessPart_FromForms{
					Forms: []*scenariosv1.BusinessPart_FromForms_Form{{
						Id:     "misc-1",
						Source: docRef("doc-misc-1"),
						Form:   &scenariosv1.BusinessPart_FromForms_Form_Form_1099Misc{Form_1099Misc: misc},
					}},
				},
			},
		}},
		DefaultPart: &scenariosv1.BusinessDefault{
			Id:   "biz-d",
			Rest: &scenariosv1.BusinessDefault_PaceSoFar_{PaceSoFar: &scenariosv1.BusinessDefault_PaceSoFar{}},
		},
	}}
	r, err := taxyear.Compute(f)
	if err != nil {
		t.Fatalf("Compute failed: %v", err)
	}
	res := r.Proto().GetScenarios()[0]
	if p := res.GetProblems(); len(p) > 0 {
		t.Fatalf("unexpected problems: %v", p)
	}
	bizIncome := res.GetFederal().GetSchedule_1().GetBusinessIncomeOrLoss().GetMinor()
	if bizIncome != 800000 {
		t.Errorf("BusinessIncome = %d, want 800000 ($8000.00)", bizIncome)
	}
}

// TestNewFormsInOtherIncomeModel verifies deterministic calculation of Form
// 1099-MISC, 1099-SA, 1099-S, 1099-C, 1099-Q, and Schedule K-1 forms in
// OtherIncomeModel without raising ambiguous questions.
func TestNewFormsInOtherIncomeModel(t *testing.T) {
	for _, tc := range []struct {
		name               string
		form               *scenariosv1.OtherIncomePart_FromForms_Form
		wantOtherIncome    int64
		wantCapitalGain    int64
		wantWithheld1099   int64
		wantNotIncludedSub string
	}{
		{
			name: "Form 1099-MISC with federal withholding",
			form: &scenariosv1.OtherIncomePart_FromForms_Form{
				Id:     "f-misc",
				Source: docRef("ref-misc"),
				Form: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099Misc{
					Form_1099Misc: &form1099misc.Form1099MISC{
						TaxYear:            2026,
						OtherIncome:        usd(1500),
						FederalTaxWithheld: usd(150),
					},
				},
			},
			wantOtherIncome:  1500_00,
			wantWithheld1099: 150_00,
		},
		{
			name: "Form 1099-SA with partial qualified medical expenses",
			form: &scenariosv1.OtherIncomePart_FromForms_Form{
				Id:     "f-sa",
				Source: docRef("ref-sa"),
				Supplement: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099SaSupplement{
					Form_1099SaSupplement: &scenariosv1.OtherIncomePart_FromForms_Form_Form1099SASupplement{
						QualifiedExpenses: usd(500),
					},
				},
				Form: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099Sa{
					Form_1099Sa: &form1099sa.Form1099SA{
						TaxYear:           2026,
						GrossDistribution: usd(800),
					},
				},
			},
			wantOtherIncome:    300_00,
			wantNotIncludedSub: "20% additional tax",
		},
		{
			name: "Form 1099-SA fully covered by qualified medical expenses",
			form: &scenariosv1.OtherIncomePart_FromForms_Form{
				Id:     "f-sa-full",
				Source: docRef("ref-sa-full"),
				Supplement: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099SaSupplement{
					Form_1099SaSupplement: &scenariosv1.OtherIncomePart_FromForms_Form_Form1099SASupplement{
						QualifiedExpenses: usd(800),
					},
				},
				Form: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099Sa{
					Form_1099Sa: &form1099sa.Form1099SA{
						TaxYear:           2026,
						GrossDistribution: usd(800),
					},
				},
			},
			wantOtherIncome: 0,
		},
		{
			name: "Form 1099-C with partial Form 982 insolvency exclusion",
			form: &scenariosv1.OtherIncomePart_FromForms_Form{
				Id:     "f-c",
				Source: docRef("ref-c"),
				Supplement: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099CSupplement{
					Form_1099CSupplement: &scenariosv1.OtherIncomePart_FromForms_Form_Form1099CSupplement{
						ExcludedAmount: usd(1200),
					},
				},
				Form: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099C{
					Form_1099C: &form1099c.Form1099C{
						TaxYear:                2026,
						AmountOfDebtDischarged: usd(2000),
					},
				},
			},
			wantOtherIncome: 800_00,
		},
		{
			name: "Form 1099-S with cost basis, selling expenses, and section 121 exclusion",
			form: &scenariosv1.OtherIncomePart_FromForms_Form{
				Id:     "f-s",
				Source: docRef("ref-s"),
				Supplement: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099SSupplement{
					Form_1099SSupplement: &scenariosv1.OtherIncomePart_FromForms_Form_Form1099SSupplement{
						Basis:           usd(200000),
						SellingExpenses: usd(20000),
						ExcludedAmount:  usd(250000),
					},
				},
				Form: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099S{
					Form_1099S: &form1099s.Form1099S{
						TaxYear:       2026,
						GrossProceeds: usd(500000),
					},
				},
			},
			wantOtherIncome: 0,
			wantCapitalGain: 30000_00, // 500k - 200k - 20k - 250k = 30k capital gain
		},
		{
			name: "Form 1099-Q with partial qualified education expenses",
			form: &scenariosv1.OtherIncomePart_FromForms_Form{
				Id:     "f-q",
				Source: docRef("ref-q"),
				Supplement: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099QSupplement{
					Form_1099QSupplement: &scenariosv1.OtherIncomePart_FromForms_Form_Form1099QSupplement{
						QualifiedExpenses: usd(3000), // 60% covered -> 40% of $1,000 earnings = $400 taxable
					},
				},
				Form: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099Q{
					Form_1099Q: &form1099q.Form1099Q{
						TaxYear:           2026,
						GrossDistribution: usd(5000),
						Earnings:          usd(1000),
					},
				},
			},
			wantOtherIncome:    400_00,
			wantNotIncludedSub: "10% additional tax",
		},
		{
			name: "Schedule K-1 (Form 1065) ordinary business income",
			form: &scenariosv1.OtherIncomePart_FromForms_Form{
				Id:     "f-k1",
				Source: docRef("ref-k1"),
				Form: &scenariosv1.OtherIncomePart_FromForms_Form_ScheduleK1_1065{
					ScheduleK1_1065: &schedulek1.ScheduleK1Form1065{
						TaxYear:                2026,
						OrdinaryBusinessIncome: usd(3500),
					},
				},
			},
			wantOtherIncome:    3500_00,
			wantNotIncludedSub: "Partner basis limits",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := others()
			f.GetShared().GetModel().Brokers = nil
			f.GetShared().GetModel().OtherIncome = []*scenariosv1.OtherIncomeModel{{
				Id:    "other-src",
				Payer: "Payer",
				Parts: []*scenariosv1.OtherIncomePart{{
					Id: "p-forms",
					Part: &scenariosv1.OtherIncomePart_FromForms_{
						FromForms: &scenariosv1.OtherIncomePart_FromForms{
							Forms: []*scenariosv1.OtherIncomePart_FromForms_Form{tc.form},
						},
					},
				}},
			}}
			r, err := taxyear.Compute(f)
			if err != nil {
				t.Fatalf("Compute failed: %v", err)
			}
			res := r.Proto().GetScenarios()[0]
			if p := res.GetProblems(); len(p) > 0 {
				t.Fatalf("unexpected problems: %v", p)
			}
			if got := res.GetFederal().GetSchedule_1().GetTotalOtherIncome().GetMinor(); got != tc.wantOtherIncome {
				t.Errorf("TotalOtherIncome = %d, want %d", got, tc.wantOtherIncome)
			}
			if got := res.GetFederal().GetForm_1040().GetCapitalGainOrLoss().GetMinor(); got != tc.wantCapitalGain {
				t.Errorf("CapitalGainOrLoss = %d, want %d", got, tc.wantCapitalGain)
			}
			if tc.wantCapitalGain != 0 {
				if got := res.GetFederal().GetScheduleD().GetNetLongTermGainOrLoss().GetMinor(); got != tc.wantCapitalGain {
					t.Errorf("Schedule D NetLongTermGainOrLoss = %d, want %d", got, tc.wantCapitalGain)
				}
			}
			if got := res.GetFederal().GetForm_1040().GetWithheldFrom_1099().GetMinor(); got != tc.wantWithheld1099 {
				t.Errorf("WithheldFrom_1099 = %d, want %d", got, tc.wantWithheld1099)
			}
			for _, q := range res.GetQuestions() {
				if strings.HasPrefix(q.GetId(), "other_income/") {
					t.Errorf("unexpected other_income question: %v", q)
				}
			}
			if tc.wantNotIncludedSub != "" {
				var found bool
				for _, ni := range res.GetNotIncluded() {
					if strings.Contains(ni.GetText().GetText(), tc.wantNotIncludedSub) {
						found = true
					}
				}
				if !found {
					t.Errorf("NotIncluded = %v, want substring %q", res.GetNotIncluded(), tc.wantNotIncludedSub)
				}
			}
		})
	}
}

// TestNewFormsInRetirementModel verifies that Form 5498 and Form 5498-SA
// contributions are tracked in RetirementProjection.
func TestNewFormsInRetirementModel(t *testing.T) {
	f := others()
	f.GetShared().GetModel().Retirement = []*scenariosv1.RetirementModel{{
		Id:     "ret-1",
		Person: "you",
		Parts: []*scenariosv1.RetirementPart{{
			Id: "p-forms",
			Part: &scenariosv1.RetirementPart_FromForms_{
				FromForms: &scenariosv1.RetirementPart_FromForms{
					Forms: []*scenariosv1.RetirementPart_FromForms_Form{
						{
							Id:     "f-5498",
							Source: docRef("ref-5498"),
							Form: &scenariosv1.RetirementPart_FromForms_Form_Form_5498{
								Form_5498: &form5498.Form5498{
									TaxYear:              2026,
									IraContributions:     usd(3000),
									RothIraContributions: usd(2000),
								},
							},
						},
						{
							Id:     "f-5498sa",
							Source: docRef("ref-5498sa"),
							Form: &scenariosv1.RetirementPart_FromForms_Form_Form_5498Sa{
								Form_5498Sa: &form5498sa.Form5498SA{
									TaxYear:                      2026,
									TotalContributionsMadeInYear: usd(1500),
								},
							},
						},
					},
				},
			},
		}},
	}}
	r, err := taxyear.Compute(f)
	if err != nil {
		t.Fatalf("Compute failed: %v", err)
	}
	res := r.Proto().GetScenarios()[0]
	retProj := res.GetProjection().GetRetirement()[0]
	if got := retProj.GetYear().GetItems().GetTraditionalIra().GetMinor(); got != 300000 {
		t.Errorf("TraditionalIra = %d, want 300000", got)
	}
	if got := retProj.GetYear().GetItems().GetRothIra().GetMinor(); got != 200000 {
		t.Errorf("RothIra = %d, want 200000", got)
	}
	if got := retProj.GetYear().GetItems().GetHsa().GetMinor(); got != 150000 {
		t.Errorf("Hsa = %d, want 150000", got)
	}
}

// TestHomeSaleScheduleDRouting verifies that taxable gains from home sales
// (both Form 1099-S and planned OneOff.HomeSale) are routed to Schedule D
// as long-term capital gain rather than ordinary income on Schedule 1.
func TestHomeSaleScheduleDRouting(t *testing.T) {
	for _, tc := range []struct {
		name          string
		model         *scenariosv1.OtherIncomeModel
		wantCapGain   int64
		wantSchedule1 int64
	}{
		{
			name: "Form 1099-S with Kind set to HOME_SALE",
			model: &scenariosv1.OtherIncomeModel{
				Id:    "home-sale-form",
				Kind:  scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_HOME_SALE,
				Payer: "Title Company",
				Parts: []*scenariosv1.OtherIncomePart{{
					Id: "part-forms",
					Part: &scenariosv1.OtherIncomePart_FromForms_{
						FromForms: &scenariosv1.OtherIncomePart_FromForms{
							Forms: []*scenariosv1.OtherIncomePart_FromForms_Form{{
								Id:     "form-1099s",
								Source: docRef("doc-1099s"),
								Supplement: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099SSupplement{
									Form_1099SSupplement: &scenariosv1.OtherIncomePart_FromForms_Form_Form1099SSupplement{
										Basis:           usd(300000),
										SellingExpenses: usd(30000),
										ExcludedAmount:  usd(250000),
									},
								},
								Form: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099S{
									Form_1099S: &form1099s.Form1099S{
										TaxYear:       2026,
										GrossProceeds: usd(650000),
									},
								},
							}},
						},
					},
				}},
			},
			wantCapGain:   70000_00, // 650k - 300k - 30k - 250k = 70k
			wantSchedule1: 0,
		},
		{
			name: "Planned OneOff.HomeSale",
			model: &scenariosv1.OtherIncomeModel{
				Id:    "home-sale-planned",
				Kind:  scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_HOME_SALE,
				Payer: "Home Buyer",
				Parts: []*scenariosv1.OtherIncomePart{{
					Id: "part-oneoff",
					Part: &scenariosv1.OtherIncomePart_OneOff_{
						OneOff: &scenariosv1.OtherIncomePart_OneOff{
							Date: date(6, 15),
							Event: &scenariosv1.OtherIncomePart_OneOff_HomeSale_{
								HomeSale: &scenariosv1.OtherIncomePart_OneOff_HomeSale{
									Price:           usd(800000),
									Basis:           usd(400000),
									SellingExpenses: usd(40000),
									ExcludedAmount:  usd(250000),
								},
							},
						},
					},
				}},
			},
			wantCapGain:   110000_00, // 800k - 400k - 40k - 250k = 110k
			wantSchedule1: 0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := others()
			f.GetShared().GetModel().Brokers = nil
			f.GetShared().GetModel().OtherIncome = []*scenariosv1.OtherIncomeModel{tc.model}
			r, err := taxyear.Compute(f)
			if err != nil {
				t.Fatalf("Compute failed: %v", err)
			}
			res := r.Proto().GetScenarios()[0]
			if p := res.GetProblems(); len(p) > 0 {
				t.Fatalf("unexpected problems: %v", p)
			}
			// Must not be on Schedule 1
			if got := res.GetFederal().GetSchedule_1().GetTotalOtherIncome().GetMinor(); got != tc.wantSchedule1 {
				t.Errorf("Schedule 1 TotalOtherIncome = %d, want %d", got, tc.wantSchedule1)
			}
			// Must route to Form 1040 line 7 (CapitalGainOrLoss)
			if got := res.GetFederal().GetForm_1040().GetCapitalGainOrLoss().GetMinor(); got != tc.wantCapGain {
				t.Errorf("Form 1040 CapitalGainOrLoss = %d, want %d", got, tc.wantCapGain)
			}
			// Must route to Schedule D NetLongTermGainOrLoss
			if got := res.GetFederal().GetScheduleD().GetNetLongTermGainOrLoss().GetMinor(); got != tc.wantCapGain {
				t.Errorf("Schedule D NetLongTermGainOrLoss = %d, want %d", got, tc.wantCapGain)
			}
			// Summary should reflect the taxable gain
			if sums := res.GetSummary().GetOtherIncome(); len(sums) > 0 {
				if got := sums[0].GetTaxable().GetMinor(); got != tc.wantCapGain {
					t.Errorf("Summary OtherIncome Taxable = %d, want %d", got, tc.wantCapGain)
				}
			}
			// No unexpected questions or exclusions
			for _, q := range res.GetQuestions() {
				if strings.HasPrefix(q.GetId(), "other_income/") {
					t.Errorf("unexpected other_income question: %v", q)
				}
			}
		})
	}
}
