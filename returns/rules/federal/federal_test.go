package federal_test

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	form1040 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1040"
	form1095a "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1095_a"
	form1098 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1098"
	form1098e "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1098_e"
	form1098t "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1098_t"
	form1099b "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_b"
	form1099div "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_div"
	form1099g "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_g"
	form1099int "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_int"
	form1099k "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_k"
	form1099nec "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_nec"
	form1099r "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_r"
	formssa1099 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_ssa_1099"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
	schedulek1 "github.com/nexteggapp/taxprotos/go/us/federal/v1/schedule_k1"
)

func usd(dollars int64) *commonv1.Money {
	return &commonv1.Money{Currency: "USD", Minor: dollars * 100}
}

func cents(c int64) *commonv1.Money {
	return &commonv1.Money{Currency: "USD", Minor: c}
}

func date(y, m, d int32) *commonv1.Date {
	return &commonv1.Date{Year: y, Month: m, Day: d}
}

func boolPtr(b bool) *bool    { return &b }
func int32Ptr(v int32) *int32 { return &v }

func assertMoneyDollars(t *testing.T, label string, got *commonv1.Money, wantDollars int64) {
	t.Helper()
	wantMinor := wantDollars * 100
	if got.GetMinor() != wantMinor {
		t.Errorf("%s = %d cents ($%.2f), want %d cents ($%d.00)",
			label, got.GetMinor(), float64(got.GetMinor())/100.0, wantMinor, wantDollars)
	}
}

func TestLoadEngineTypechecksAllModules(t *testing.T) {
	eng, err := federal.LoadEngine()
	if err != nil {
		t.Fatalf("LoadEngine() failed: %v", err)
	}
	if eng.Module == nil || len(eng.Module.GetExpressions()) == 0 {
		t.Fatalf("LoadEngine() produced empty module")
	}
}

func TestPrepareFirstSliceRivera2025(t *testing.T) {
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() failed: %v", err)
	}

	inputs := &returnsv1.ReturnInputs{
		SchemaVersion: 1,
		TaxYear:       2025,
		Federal: &returnsv1.Federal1040Inputs{
			Household: &returnsv1.Household{
				DependentsComplete:     boolPtr(true),
				MaritalStatus:          returnsv1.MaritalStatus_MARITAL_STATUS_MARRIED,
				LivedWithSpouseAnyTime: boolPtr(true),
				DigitalAssets:          boolPtr(false),
				Taxpayer: &returnsv1.Person{
					Id:        "alex",
					FirstName: "Alex",
					LastName:  "Rivera",
					Tin: &returnsv1.TaxpayerID{
						Kind:               returnsv1.TaxpayerIDKind_TAXPAYER_ID_KIND_SSN,
						ValidForEmployment: boolPtr(true),
					},
					DateOfBirth:             date(1986, 5, 14),
					Citizenship:             returnsv1.CitizenshipStatus_CITIZENSHIP_STATUS_US_CITIZEN,
					Blind:                   boolPtr(false),
					CanBeClaimedAsDependent: boolPtr(false),
				},
				Spouse: &returnsv1.Person{
					Id:        "sam",
					FirstName: "Sam",
					LastName:  "Rivera",
					Tin: &returnsv1.TaxpayerID{
						Kind:               returnsv1.TaxpayerIDKind_TAXPAYER_ID_KIND_SSN,
						ValidForEmployment: boolPtr(true),
					},
					DateOfBirth:             date(1988, 9, 22),
					Citizenship:             returnsv1.CitizenshipStatus_CITIZENSHIP_STATUS_US_CITIZEN,
					Blind:                   boolPtr(false),
					CanBeClaimedAsDependent: boolPtr(false),
				},
				Dependents: []*returnsv1.Dependent{
					{
						Id:        "maya",
						FirstName: "Maya",
						LastName:  "Rivera",
						Tin: &returnsv1.TaxpayerID{
							Kind:               returnsv1.TaxpayerIDKind_TAXPAYER_ID_KIND_SSN,
							ValidForEmployment: boolPtr(true),
						},
						DateOfBirth:                     date(2016, 3, 8),
						CitizenOrResidentOfNorthAmerica: boolPtr(true),
						Relationship:                    returnsv1.Relationship_RELATIONSHIP_CHILD,
						MonthsLivedWithYou:              int32Ptr(12),
						ProvidedOverHalfOwnSupport:      boolPtr(false),
						YouProvidedOverHalfSupport:      boolPtr(true),
						FullTimeStudent:                 boolPtr(false),
						PermanentlyDisabled:             boolPtr(false),
						FilesJointReturn:                boolPtr(false),
						GrossIncome:                     usd(0),
					},
				},
			},
			Documents: &returnsv1.Documents{
				Complete: boolPtr(true),
				W2S: []*returnsv1.W2Document{
					{
						Id:     "w2-alex",
						Person: "alex",
						Form: &formw2.FormW2{
							WagesTipsOtherCompensation: usd(185000),
							FederalTaxWithheld:         usd(29500),
							SocialSecurityWages:        usd(176100),
							SocialSecurityTaxWithheld:  cents(1091820),
							MedicareWagesAndTips:       usd(185000),
							MedicareTaxWithheld:        cents(268250),
							RetirementPlan:             true,
							CodedAmounts: []*formw2.FormW2_CodedAmount{
								{Code: "W", Amount: usd(4500)},
							},
							StateTaxWithholding: []*commonv1.StateTaxWithholding{
								{State: "CA", StateIncome: usd(185000), TaxWithheld: usd(14800)},
							},
						},
					},
					{
						Id:     "w2-sam",
						Person: "sam",
						Form: &formw2.FormW2{
							WagesTipsOtherCompensation: usd(95000),
							FederalTaxWithheld:         usd(12500),
							SocialSecurityWages:        usd(95000),
							SocialSecurityTaxWithheld:  usd(5890),
							MedicareWagesAndTips:       usd(95000),
							MedicareTaxWithheld:        cents(137750),
							RetirementPlan:             true,
							StateTaxWithholding: []*commonv1.StateTaxWithholding{
								{State: "CA", StateIncome: usd(95000), TaxWithheld: usd(6200)},
							},
						},
					},
				},
				Interest: []*returnsv1.Form1099INTDocument{
					{
						Id:     "int-ally",
						Person: "alex",
						Form: &form1099int.Form1099INT{
							Payer:                          &commonv1.Party{Name: "Ally Bank"},
							InterestIncome:                 usd(3200),
							SavingsBondAndTreasuryInterest: usd(800),
						},
					},
				},
				Dividends: []*returnsv1.Form1099DIVDocument{
					{
						Id:     "div-vanguard",
						Person: "alex",
						Form: &form1099div.Form1099DIV{
							Payer:                        &commonv1.Party{Name: "Vanguard"},
							OrdinaryDividends:            usd(4500),
							QualifiedDividends:           usd(3800),
							TotalCapitalGainDistribution: usd(1200),
							ForeignTaxPaid:               usd(240),
						},
					},
				},
				Brokers: []*returnsv1.BrokerStatement{
					{
						Id:     "broker-vanguard",
						Person: "alex",
						Transactions: []*returnsv1.BrokerTransaction{
							{
								Id: "tx-st-1",
								Form: &form1099b.Form1099B{
									DescriptionOfProperty: "100 sh Example Tech Inc",
									DateAcquired:          date(2025, 2, 10),
									DateSoldOrDisposed:    date(2025, 8, 19),
									Proceeds:              usd(12000),
									CostOrOtherBasis:      usd(10500),
									HoldingPeriod:         form1099b.Form1099B_HOLDING_PERIOD_SHORT_TERM,
									BasisReportedToIrs:    true,
								},
							},
							{
								Id: "tx-lt-1",
								Form: &form1099b.Form1099B{
									DescriptionOfProperty: "200 sh Index ETF",
									DateAcquired:          date(2021, 4, 12),
									DateSoldOrDisposed:    date(2025, 10, 3),
									Proceeds:              usd(35000),
									CostOrOtherBasis:      usd(21000),
									HoldingPeriod:         form1099b.Form1099B_HOLDING_PERIOD_LONG_TERM,
									BasisReportedToIrs:    true,
								},
							},
						},
					},
				},
				MortgageInterest: []*returnsv1.Form1098Document{
					{
						Id:             "1098-main",
						Person:         "alex",
						Use:            returnsv1.MortgageUse_MORTGAGE_USE_HOME_ACQUISITION,
						DebtIncurredOn: date(2021, 6, 15),
						AverageBalance: usd(680000),
						Form: &form1098.Form1098{
							MortgageInterestReceived: usd(22400),
						},
					},
				},
			},
			Health: &returnsv1.Health{
				HsaAccounts: []*returnsv1.PersonHSA{
					{
						Person:              "alex",
						AnnualCoverage:      returnsv1.HDHPCoverageType_HDHP_COVERAGE_TYPE_FAMILY,
						DirectContributions: usd(4050),
					},
				},
			},
			Deductions: &returnsv1.Deductions{
				ItemizedComplete: boolPtr(true),
				StateAndLocalTaxes: &returnsv1.StateLocalTaxes{
					RealEstateTaxes:       usd(14200),
					PersonalPropertyTaxes: usd(450),
				},
				CharitableGifts: []*returnsv1.CharitableGift{
					{
						DoneeName:                "SF-Marin Food Bank",
						DateContributed:          date(2025, 12, 10),
						LimitCategory:            returnsv1.CharitableLimitCategory_CHARITABLE_LIMIT_CATEGORY_CASH_PUBLIC_CHARITY_60_PCT,
						CashAmount:               usd(3500),
						HasWrittenAcknowledgment: boolPtr(true),
					},
				},
			},
			Payments: &returnsv1.Payments{
				Complete: boolPtr(true),
				EstimatedPayments: []*returnsv1.EstimatedPayment{
					{
						Jurisdiction:      "US",
						DatePaid:          date(2025, 4, 15),
						DesignatedTaxYear: 2025,
						Amount:            usd(2000),
					},
				},
			},
			PriorYear: &returnsv1.PriorYear{
				PriorYearAgi: usd(268000),
				CapitalLossCarryover: &returnsv1.CapitalLossCarryover{
					ShortTermCarryover: usd(1000),
					LongTermCarryover:  usd(2500),
				},
			},
			Elections: &returnsv1.Elections{
				FilingStatus:    returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY,
				DeductionMethod: returnsv1.DeductionMethodElection_DEDUCTION_METHOD_ELECTION_UNSPECIFIED,
				ForeignTax:      returnsv1.ForeignTaxElection_FOREIGN_TAX_ELECTION_DIRECT_CREDIT_NO_1116,
			},
		},
	}

	out, err := eng.Prepare(inputs)
	if err != nil {
		t.Fatalf("Prepare() failed: %v", err)
	}

	if out.GetHeader().GetTaxYear() != 2025 {
		t.Errorf("Header.TaxYear = %d, want 2025", out.GetHeader().GetTaxYear())
	}

	fed := out.GetFederal()
	if fed.GetStatus() != returnsv1.ReturnStatus_RETURN_STATUS_COMPLETE {
		t.Errorf("Federal.Status = %v, want COMPLETE", fed.GetStatus())
	}

	f1040 := fed.GetForms().GetForm_1040()
	if f1040.GetFilingStatus() != form1040.FilingStatus_FILING_STATUS_MARRIED_FILING_JOINTLY {
		t.Errorf("FilingStatus = %v, want MFJ", f1040.GetFilingStatus())
	}
	if len(f1040.GetDependents()) != 1 || !f1040.GetDependents()[0].GetChildTaxCredit() {
		t.Fatalf("Dependents = %+v, want 1 CTC child", f1040.GetDependents())
	}

	// Income checks:
	// W-2 wages: 185,000 + 95,000 = 280,000
	// Taxable interest: 3,200 + 800 = 4,000
	// Ordinary dividends: 4,500 (Qualified: 3,800)
	// Schedule D:
	//   Short-term: +1,500 - 1,000 carryover = +500
	//   Long-term:  +14,000 + 1,200 cap gain dist - 2,500 carryover = +12,700
	//   Combined net capital gain: +13,200
	// Total income: 280,000 + 4,000 + 4,500 + 13,200 = 301,700
	assertMoneyDollars(t, "Form1040.TotalWages", f1040.GetTotalWages(), 280000)
	assertMoneyDollars(t, "Form1040.TaxableInterest", f1040.GetTaxableInterest(), 4000)
	assertMoneyDollars(t, "Form1040.QualifiedDividends", f1040.GetQualifiedDividends(), 3800)
	assertMoneyDollars(t, "Form1040.OrdinaryDividends", f1040.GetOrdinaryDividends(), 4500)
	assertMoneyDollars(t, "Form1040.CapitalGainOrLoss", f1040.GetCapitalGainOrLoss(), 13200)
	assertMoneyDollars(t, "Form1040.TotalIncome", f1040.GetTotalIncome(), 301700)

	// Form 8889 HSA:
	// Family limit 2025 = 8,550; employer W-2 code W = 4,500; remaining = 4,050; direct = 4,050 -> deduction = 4,050
	if len(fed.GetForm_8889S()) != 1 {
		t.Fatalf("Form8889 count = %d, want 1", len(fed.GetForm_8889S()))
	}
	assertMoneyDollars(t, "Form8889.HsaDeduction", fed.GetForm_8889S()[0].GetHsaDeduction(), 4050)
	assertMoneyDollars(t, "Form1040.AdjustmentsToIncome", f1040.GetAdjustmentsToIncome(), 4050)
	// AGI = 301,700 - 4,050 = 297,650
	assertMoneyDollars(t, "Form1040.AdjustedGrossIncome", f1040.GetAdjustedGrossIncome(), 297650)

	// Schedule A vs Standard Deduction:
	// CA withholding: 14,800 + 6,200 = 21,000
	// Real estate taxes: 14,200; Personal property taxes: 450 -> Total SALT = 35,650 (under $40,000 OBBBA cap!)
	// Mortgage interest: 22,400
	// Charitable gifts: 3,500
	// Total Itemized = 35,650 + 22,400 + 3,500 = 61,550 vs Standard Deduction = 31,500
	schedA := fed.GetForms().GetScheduleA()
	assertMoneyDollars(t, "ScheduleA.TotalStateAndLocalTaxes", schedA.GetTotalStateAndLocalTaxes(), 35650)
	assertMoneyDollars(t, "ScheduleA.StateAndLocalTaxesAfterCap", schedA.GetStateAndLocalTaxesAfterCap(), 35650)
	assertMoneyDollars(t, "ScheduleA.TotalItemizedDeductions", schedA.GetTotalItemizedDeductions(), 61550)
	if !f1040.GetItemized() {
		t.Errorf("Form1040.Itemized = false, want true")
	}
	assertMoneyDollars(t, "Form1040.Deduction", f1040.GetDeduction(), 61550)
	// Taxable income = 297,650 - 61,550 = 236,100
	assertMoneyDollars(t, "Form1040.TaxableIncome", f1040.GetTaxableIncome(), 236100)

	// QDCGT Worksheet:
	// Line 1 taxable income = 236,100
	// Line 2 qualified dividends = 3,800
	// Line 3 net capital gain = 12,700
	// Line 4 total preferential = 16,500
	// Line 5 ordinary taxable income = 219,600
	// All 16,500 of preferential income is in the 15% band (above 96,700 and below 600,050):
	// Line 18 (15% tax) = 16,500 * 0.15 = 2,475
	// Line 22 (ordinary bracket tax on 219,600 MFJ 2025):
	//   35,302 + 0.24 * (219,600 - 206,700) = 35,302 + 3,096 = 38,398
	// Line 25 total QDCGT tax = 38,398 + 2,475 = 40,873
	qdcgt := fed.GetWorkpapers().GetQdcgt()
	assertMoneyDollars(t, "QDCGT.Line4Preferential", qdcgt.GetLine_4TotalPreferentialIncome(), 16500)
	assertMoneyDollars(t, "QDCGT.Line5Ordinary", qdcgt.GetLine_5OrdinaryTaxableIncome(), 219600)
	assertMoneyDollars(t, "QDCGT.Line18TaxAt15Pct", qdcgt.GetLine_18TaxAtFifteenPercent(), 2475)
	assertMoneyDollars(t, "QDCGT.Line22OrdinaryTax", qdcgt.GetLine_22TaxOnOrdinaryIncome(), 38398)
	assertMoneyDollars(t, "Form1040.Tax", f1040.GetTax(), 40873)

	// Credits:
	// Child Tax Credit (1 child, AGI 297,650 < 400,000) = 2,200
	// Foreign Tax Credit = 240
	// Total credits = 2,440 -> Tax after credits = 40,873 - 2,440 = 38,433
	assertMoneyDollars(t, "Form1040.ChildTaxCredit", f1040.GetChildTaxCredit(), 2200)
	assertMoneyDollars(t, "Form1040.NonrefundableCredits", f1040.GetNonrefundableCredits(), 240)
	assertMoneyDollars(t, "Form1040.TaxAfterCredits", f1040.GetTaxAfterCredits(), 38433)

	// Additional Medicare Tax (Form 8959):
	// Combined Medicare wages = 280,000 > 250,000 MFJ threshold -> 0.9% of 30,000 = $270
	// Net Investment Income Tax (Form 8960):
	// NII = 4,000 + 4,500 + 13,200 = 21,700; MAGI excess over 250,000 = 47,650 -> 3.8% of 21,700 = $825
	// Schedule 2 Part II = 270 + 825 = 1,095
	// Total tax (line 24) = 38,433 + 1,095 = 39,528
	sched2 := fed.GetForms().GetSchedule_2()
	assertMoneyDollars(t, "Schedule2.AdditionalMedicareTax", sched2.GetAdditionalMedicareTax(), 270)
	assertMoneyDollars(t, "Schedule2.NetInvestmentIncomeTax", sched2.GetNetInvestmentIncomeTax(), 825)
	assertMoneyDollars(t, "Form1040.TotalTax", f1040.GetTotalTax(), 39528)

	// Payments:
	// W-2 withholding = 29,500 + 12,500 = 42,000
	// Estimated tax payments = 2,000
	// Total payments = 44,000
	// Overpaid / Refund = 44,000 - 39,528 = 4,472
	assertMoneyDollars(t, "Form1040.TotalWithheld", f1040.GetTotalWithheld(), 42000)
	assertMoneyDollars(t, "Form1040.EstimatedTaxPayments", f1040.GetEstimatedTaxPayments(), 2000)
	assertMoneyDollars(t, "Form1040.TotalPayments", f1040.GetTotalPayments(), 44000)
	assertMoneyDollars(t, "Form1040.Refunded", f1040.GetRefunded(), 4472)
}

func TestWorkpapersSSBenefitsSimplifiedPensionIRAStudentLoanStateRefund(t *testing.T) {
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() failed: %v", err)
	}

	inputs := &returnsv1.ReturnInputs{
		SchemaVersion: 1,
		TaxYear:       2025,
		Federal: &returnsv1.Federal1040Inputs{
			Household: &returnsv1.Household{
				MaritalStatus: returnsv1.MaritalStatus_MARITAL_STATUS_NEVER_MARRIED,
				Taxpayer: &returnsv1.Person{
					Id:          "pat",
					FirstName:   "Pat",
					LastName:    "Lee",
					DateOfBirth: date(1962, 7, 10), // Age 63 in 2025
					Tin: &returnsv1.TaxpayerID{
						Kind:               returnsv1.TaxpayerIDKind_TAXPAYER_ID_KIND_SSN,
						ValidForEmployment: boolPtr(true),
					},
				},
			},
			Documents: &returnsv1.Documents{
				W2S: []*returnsv1.W2Document{
					{
						Id:     "w2-pat",
						Person: "pat",
						Form: &formw2.FormW2{
							WagesTipsOtherCompensation: usd(60000),
							FederalTaxWithheld:         usd(7000),
							RetirementPlan:             true,
						},
					},
				},
				RetirementDistributions: []*returnsv1.Form1099RDocument{
					{
						Id:     "1099r-ira-qcd",
						Person: "pat",
						Form: &form1099r.Form1099R{
							GrossDistribution:          usd(5000),
							TaxableAmountNotDetermined: true,
							IraSepSimple:               true,
						},
						QualifiedCharitableDistribution: usd(2000),
					},
					{
						Id:     "1099r-pension",
						Person: "pat",
						Form: &form1099r.Form1099R{
							GrossDistribution:          usd(18000),
							TaxableAmountNotDetermined: true,
							IraSepSimple:               false,
						},
						SimplifiedMethod: &returnsv1.SimplifiedMethod{
							CostInContract:          usd(52000),
							AnnuityStart:            date(2024, 1, 1),
							AgeAtStart:              int32Ptr(62), // Table 1 (61-65): 260 months -> $200/mo -> $2,400/yr tax-free
							RecoveredInEarlierYears: usd(2400),
						},
					},
				},
				SocialSecurity: []*returnsv1.FormSSA1099Document{
					{
						Id:     "ssa-pat",
						Person: "pat",
						Form: &formssa1099.FormSSA1099{
							BenefitsPaid: usd(24000),
							NetBenefits:  usd(24000),
						},
					},
				},
				GovernmentPayments: []*returnsv1.Form1099GDocument{
					{
						Id:     "1099g-ca",
						Person: "pat",
						Form: &form1099g.Form1099G{
							StateOrLocalRefund: usd(1200),
						},
					},
				},
				StudentLoanInterest: []*returnsv1.Form1098EDocument{
					{
						Id:     "1098e-pat",
						Person: "pat",
						Form: &form1098e.Form1098E{
							StudentLoanInterestReceived: usd(2000),
						},
					},
				},
			},
			Retirement: &returnsv1.Retirement{
				Persons: []*returnsv1.PersonRetirement{
					{
						Person:                      "pat",
						TraditionalIraContributions: usd(8000),
					},
				},
			},
			PriorYear: &returnsv1.PriorYear{
				ItemizedSummary: &returnsv1.PriorYearItemizedSummary{
					ItemizedInPriorYear:                boolPtr(true),
					DeductedSalesTaxInsteadOfIncomeTax: boolPtr(false),
					PriorScheduleALine_5AIncomeTax:     usd(9000),
					PriorAllowableSalesTax:             usd(2000),
					PriorScheduleALine_5DTotalSalt:     usd(13000),
					PriorScheduleALine_5ECappedSalt:    usd(10000),
					PriorTotalItemizedDeductions:       usd(18000),
					PriorStandardDeduction:             usd(14600),
				},
			},
		},
	}

	fed, err := eng.PrepareFederal(inputs)
	if err != nil {
		t.Fatalf("PrepareFederal() failed: %v", err)
	}

	wp := fed.GetWorkpapers()
	// 1. State Tax Refund Worksheet:
	// Prior SALT benefit: non-income SALT = 13,000 - 9,000 = 4,000; + 2,000 sales tax = 6,000;
	// 10,000 capped SALT - 6,000 = 4,000; itemized excess = 18,000 - 14,600 = 3,400 -> min(1,200, 3,400) = 1,200
	assertMoneyDollars(t, "StateTaxRefund.TaxableRefund", wp.GetStateTaxRefund().GetTaxableRefund(), 1200)

	// 2. Simplified Method Worksheet:
	// 52,000 / 260 = $200/mo * 12 = $2,400 tax-free; 18,000 - 2,400 = $15,600 taxable
	if len(wp.GetSimplifiedMethodPensions()) != 1 {
		t.Fatalf("SimplifiedMethodPensions len = %d, want 1", len(wp.GetSimplifiedMethodPensions()))
	}
	sm := wp.GetSimplifiedMethodPensions()[0]
	assertMoneyDollars(t, "SimplifiedMethod.TaxFreePortion", sm.GetCurrentYearTaxFreePortion(), 2400)
	assertMoneyDollars(t, "SimplifiedMethod.TaxablePortion", sm.GetTaxablePortion(), 15600)
	assertMoneyDollars(t, "SimplifiedMethod.RemainingBasis", sm.GetRemainingBasisForNextYear(), 47200)

	// 3. IRA QCD: 5,000 gross - 2,000 QCD = 3,000 taxable
	f1040 := fed.GetForms().GetForm_1040()
	assertMoneyDollars(t, "Form1040.IraDistributions", f1040.GetIraDistributions(), 5000)
	assertMoneyDollars(t, "Form1040.IraDistributionsTaxable", f1040.GetIraDistributionsTaxable(), 3000)
	assertMoneyDollars(t, "Form1040.PensionsTaxable", f1040.GetPensionsAndAnnuitiesTaxable(), 15600)

	// 4. Social Security Benefits Worksheet & IRA Deduction Worksheet:
	// Other income before SS = 60,000 + 3,000 + 15,600 + 1,200 = 79,800
	// Half of SS (24,000) = 12,000 -> provisional = 91,800 -> 85% of 24,000 = 20,400 taxable SS!
	// MAGI for IRA (Single covered by workplace plan) = 79,800 + 20,400 = 100,200 > 89,000 phaseout end -> $0 IRA deduction, $8,000 nondeductible!
	assertMoneyDollars(t, "SSBenefits.TaxableBenefits", wp.GetSsBenefits().GetLine_18TaxableBenefits(), 20400)
	assertMoneyDollars(t, "IRADeduction.TotalIraDeduction", wp.GetIraDeduction().GetTotalIraDeduction(), 0)
	assertMoneyDollars(t, "IRADeduction.TaxpayerNondeductible", wp.GetIraDeduction().GetTaxpayerNondeductible(), 8000)

	// 5. Student Loan Interest Worksheet:
	// MAGI = 100,200 > 100,000 (85,000 + 15,000) -> fully phased out ($0 deductible, $2,000 disallowed)
	assertMoneyDollars(t, "StudentLoan.DisallowedAmount", wp.GetStudentLoanInterest().GetDisallowedAmount(), 2000)
	assertMoneyDollars(t, "StudentLoan.DeductibleAmount", wp.GetStudentLoanInterest().GetDeductibleAmount(), 0)
}

func TestSchedule1AOBBBADeductionsAnd2025Vs2026Params(t *testing.T) {
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() failed: %v", err)
	}

	makeInputs := func(year int32) *returnsv1.ReturnInputs {
		return &returnsv1.ReturnInputs{
			SchemaVersion: 1,
			TaxYear:       year,
			Federal: &returnsv1.Federal1040Inputs{
				Household: &returnsv1.Household{
					MaritalStatus: returnsv1.MaritalStatus_MARITAL_STATUS_NEVER_MARRIED,
					Taxpayer: &returnsv1.Person{
						Id:          "dana",
						FirstName:   "Dana",
						DateOfBirth: date(1958, 1, 1), // 65+ (with Jan 1 rule)
						Tin: &returnsv1.TaxpayerID{
							Kind:               returnsv1.TaxpayerIDKind_TAXPAYER_ID_KIND_SSN,
							ValidForEmployment: boolPtr(true),
						},
					},
				},
				Documents: &returnsv1.Documents{
					W2S: []*returnsv1.W2Document{
						{
							Id:     "w2-dana",
							Person: "dana",
							Form: &formw2.FormW2{
								WagesTipsOtherCompensation: usd(80000),
								FederalTaxWithheld:         usd(8000),
							},
							QualifiedTips:     usd(12000),
							QualifiedOvertime: usd(6000),
						},
					},
				},
				Deductions: &returnsv1.Deductions{
					Schedule_1A: &returnsv1.Schedule1ADeductions{
						CarLoans: []*returnsv1.QualifiedCarLoan{
							{
								Vin:                             "1HGCM82633A004352",
								LoanOriginatedOn:                date(2025, 3, 1),
								InterestPaid:                    usd(3200),
								FinalAssemblyInUsNewPersonalUse: boolPtr(true),
							},
						},
					},
				},
			},
		}
	}

	fed2025, err := eng.PrepareFederal(makeInputs(2025))
	if err != nil {
		t.Fatalf("PrepareFederal(2025) failed: %v", err)
	}
	fed2026, err := eng.PrepareFederal(makeInputs(2026))
	if err != nil {
		t.Fatalf("PrepareFederal(2026) failed: %v", err)
	}

	// Schedule 1-A checks (AGI = $80,000):
	// - Qualified tips = $12,000 (MAGI $80,000 <= $150,000)
	// - Qualified overtime = $6,000 (MAGI $80,000 <= $150,000)
	// - Vehicle loan interest = $3,200 (MAGI $80,000 <= $100,000)
	// - Enhanced senior deduction = $6,000 - 6% * ($80,000 - $75,000) = $6,000 - $300 = $5,700
	// Total Schedule 1-A = 12,000 + 6,000 + 3,200 + 5,700 = $26,900
	s1a := fed2025.GetForms().GetSchedule_1A()
	assertMoneyDollars(t, "Schedule1A.TipsDeduction", s1a.GetQualifiedTipsDeduction(), 12000)
	assertMoneyDollars(t, "Schedule1A.OvertimeDeduction", s1a.GetQualifiedOvertimeDeduction(), 6000)
	assertMoneyDollars(t, "Schedule1A.VehicleLoanDeduction", s1a.GetPassengerVehicleLoanInterestDeduction(), 3200)
	assertMoneyDollars(t, "Schedule1A.EnhancedSeniorDeduction", s1a.GetEnhancedDeductionForSeniors(), 5700)
	assertMoneyDollars(t, "Schedule1A.TotalAdditionalDeductions", s1a.GetTotalAdditionalDeductions(), 26900)

	// 2025 vs 2026 Standard Deduction (Single age 65+):
	// 2025: 15,750 + 2,000 = 17,750
	// 2026: 16,100 + 2,050 = 18,150
	assertMoneyDollars(t, "2025 StandardDeduction", fed2025.GetForms().GetForm_1040().GetDeduction(), 17750)
	assertMoneyDollars(t, "2026 StandardDeduction", fed2026.GetForms().GetForm_1040().GetDeduction(), 18150)
}

func TestHomeSaleSection121AndCapitalLossCarryforward(t *testing.T) {
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() failed: %v", err)
	}

	inputs := &returnsv1.ReturnInputs{
		SchemaVersion: 1,
		TaxYear:       2025,
		Federal: &returnsv1.Federal1040Inputs{
			Household: &returnsv1.Household{
				MaritalStatus: returnsv1.MaritalStatus_MARITAL_STATUS_NEVER_MARRIED,
				Taxpayer: &returnsv1.Person{
					Id:          "jordan",
					FirstName:   "Jordan",
					DateOfBirth: date(1985, 4, 10),
					Tin: &returnsv1.TaxpayerID{
						Kind:               returnsv1.TaxpayerIDKind_TAXPAYER_ID_KIND_SSN,
						ValidForEmployment: boolPtr(true),
					},
				},
			},
			Documents: &returnsv1.Documents{
				W2S: []*returnsv1.W2Document{
					{
						Id:     "w2-jordan",
						Person: "jordan",
						Form: &formw2.FormW2{
							WagesTipsOtherCompensation: usd(100000),
						},
					},
				},
			},
			Investments: &returnsv1.Investments{
				HomeSales: []*returnsv1.HomeSale{
					{
						Id:                              "home-1",
						Person:                          "jordan",
						Description:                     "123 Main St",
						DateAcquired:                    date(2018, 6, 1),
						DateSold:                        date(2025, 7, 1),
						SellingPrice:                    usd(750000),
						SellingExpenses:                 usd(30000),
						CostAndImprovements:             usd(440000), // Gain = 750k - 470k = 280k; Single exclusion = 250k -> 30k taxable LT gain
						DaysOwnedInLast_5Years:          1825,
						DaysUsedByTaxpayerInLast_5Years: 1825,
					},
				},
				OtherSales: []*returnsv1.OtherSale{
					{
						Id:           "st-loss-1",
						Person:       "jordan",
						Description:  "Startup Stock",
						DateAcquired: date(2025, 1, 15),
						DateSold:     date(2025, 9, 15),
						Proceeds:     usd(2000),
						Basis:        usd(40000), // -$38,000 short-term loss
					},
				},
			},
		},
	}

	fed, err := eng.PrepareFederal(inputs)
	if err != nil {
		t.Fatalf("PrepareFederal() failed: %v", err)
	}

	// Net short-term = -38,000; Net long-term (from home sale after $250k § 121 exclusion) = +30,000
	// Combined Schedule D net = -8,000 -> Form 1040 line 7 allowable loss = -3,000
	// Next year short-term capital loss carryover = 38,000 - (3,000 + 30,000) = 5,000!
	schedD := fed.GetForms().GetScheduleD()
	assertMoneyDollars(t, "ScheduleD.NetShortTerm", schedD.GetNetShortTermGainOrLoss(), -38000)
	assertMoneyDollars(t, "ScheduleD.NetLongTerm", schedD.GetNetLongTermGainOrLoss(), 30000)
	assertMoneyDollars(t, "ScheduleD.AllowableLoss", schedD.GetAllowableLoss(), 3000)
	assertMoneyDollars(t, "Form1040.CapitalGainOrLoss", fed.GetForms().GetForm_1040().GetCapitalGainOrLoss(), -3000)
	assertMoneyDollars(t, "NextYearShortTermCarryover",
		fed.GetWorkpapers().GetCapitalLossCarryover().GetNextYearShortTermCarryover(), 5000)
}

func TestScheduleCScheduleSESelfEmployedHealthAndQBI(t *testing.T) {
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() failed: %v", err)
	}

	// MFJ household where Alex is self-employed:
	// - W-2 wages of $150,000 (Social Security wages = $150,000, so remaining 2025 OASDI wage base = $176,100 - $150,000 = $26,100)
	// - Schedule C business "biz-1" (not SSTB):
	//   - Linked 1099-NEC = $80,000 (with $5,000 backup withholding)
	//   - Linked 1099-K = $20,000 (reported in error = $2,000 -> $18,000 business gross)
	//   - Cash and unreported receipts = $12,000 -> Total Line 1 = $110,000
	//   - Returns and allowances = $2,000 -> Line 3 = $108,000
	//   - Cost of goods sold: Beg $5,000 + Purchases net of personal use $9,000 + Materials $2,000 - End $6,000 = $10,000 -> Line 5 = $98,000
	//   - Vehicle: 10,000 business miles * $0.70/mi (2025) = $7,000 + $400 parking/tolls = $7,400
	//   - Meals (50% limit): $2,000 * 50% = $1,000
	//   - Supplies = $1,600, Utilities = $2,000 -> Total Line 28 expenses = $12,000 -> Line 29 tentative profit = $86,000
	//   - Simplified Home Office: 200 sq ft * $5 = $1,000 -> Line 31 Net Profit = $85,000
	// - Schedule SE:
	//   - Line 2 net business profit = $85,000
	//   - Line 4a (92.35%) = $78,498
	//   - Remaining OASDI wage base = $176,100 - $150,000 = $26,100
	//   - OASDI tax (12.4% of $26,100) = $3,236.40 -> $3,236
	//   - Medicare tax (2.9% of $78,498) = $2,276.44 -> $2,276
	//   - Total SE tax = $5,512; 50% SE tax deduction = $2,756
	// - Self-employed SEP contribution = $10,000
	// - Self-employed health insurance (Form 7206) = $6,000 (fully allowed since $85,000 - $2,756 - $10,000 = $72,244 >= $6,000)
	// - REIT dividends on 1099-DIV = $2,000
	// - Form 8995 QBI Deduction:
	//   - Net QBI = $85,000 - ($2,756 + $10,000 + $6,000) = $66,244
	//   - 20% of QBI = $13,248.80 -> $13,249
	//   - 20% of REIT dividends ($2,000) = $400
	//   - Total QBI deduction = $13,649
	inputs := &returnsv1.ReturnInputs{
		SchemaVersion: 1,
		TaxYear:       2025,
		Federal: &returnsv1.Federal1040Inputs{
			Household: &returnsv1.Household{
				MaritalStatus:          returnsv1.MaritalStatus_MARITAL_STATUS_MARRIED,
				LivedWithSpouseAnyTime: boolPtr(true),
				Taxpayer: &returnsv1.Person{
					Id:          "alex",
					FirstName:   "Alex",
					LastName:    "Morgan",
					DateOfBirth: date(1984, 3, 12),
					Tin: &returnsv1.TaxpayerID{
						Kind:               returnsv1.TaxpayerIDKind_TAXPAYER_ID_KIND_SSN,
						ValidForEmployment: boolPtr(true),
					},
				},
				Spouse: &returnsv1.Person{
					Id:          "sam",
					FirstName:   "Sam",
					LastName:    "Morgan",
					DateOfBirth: date(1986, 8, 22),
					Tin: &returnsv1.TaxpayerID{
						Kind:               returnsv1.TaxpayerIDKind_TAXPAYER_ID_KIND_SSN,
						ValidForEmployment: boolPtr(true),
					},
				},
			},
			Documents: &returnsv1.Documents{
				W2S: []*returnsv1.W2Document{
					{
						Id:     "w2-alex",
						Person: "alex",
						Form: &formw2.FormW2{
							WagesTipsOtherCompensation: usd(150000),
							SocialSecurityWages:        usd(150000),
							MedicareWagesAndTips:       usd(150000),
							FederalTaxWithheld:         usd(24000),
						},
					},
				},
				NonemployeeCompensation: []*returnsv1.Form1099NECDocument{
					{
						Id:       "nec-1",
						Person:   "alex",
						Business: "biz-1",
						Form: &form1099nec.Form1099NEC{
							NonemployeeCompensation: usd(80000),
							FederalTaxWithheld:      usd(5000),
						},
					},
				},
				PaymentNetworks: []*returnsv1.Form1099KDocument{
					{
						Id:              "k-1",
						Person:          "alex",
						Business:        "biz-1",
						ReportedInError: usd(2000),
						Form: &form1099k.Form1099K{
							GrossPaymentTransactions: usd(20000),
						},
					},
				},
				Dividends: []*returnsv1.Form1099DIVDocument{
					{
						Id:     "div-reit",
						Person: "sam",
						Form: &form1099div.Form1099DIV{
							Payer:                 &commonv1.Party{Name: "Vanguard REIT"},
							OrdinaryDividends:     usd(2000),
							Section_199ADividends: usd(2000),
						},
					},
				},
			},
			Businesses: []*returnsv1.Business{
				{
					Id:                        "biz-1",
					Owner:                     "alex",
					Name:                      "Morgan Design Studio",
					PrincipalBusiness:         "Industrial Design",
					BusinessCode:              "541400",
					AccountingMethod:          returnsv1.AccountingMethod_ACCOUNTING_METHOD_CASH,
					MateriallyParticipated:    boolPtr(true),
					CashAndUnreportedReceipts: usd(12000),
					ReturnsAndAllowances:      usd(2000),
					CostOfGoodsSold: &returnsv1.CostOfGoodsSold{
						BeginningInventory:        usd(5000),
						PurchasesNetOfPersonalUse: usd(9000),
						MaterialsAndSupplies:      usd(2000),
						EndingInventory:           usd(6000),
					},
					Expenses: &returnsv1.BusinessExpenses{
						Supplies:   usd(1600),
						Utilities:  usd(2000),
						MealsGross: usd(2000),
					},
					Vehicles: []*returnsv1.Vehicle{
						{
							Description:         "2023 Honda CR-V",
							DatePlacedInService: date(2023, 5, 1),
							BusinessMiles:       10000,
							CommutingMiles:      2000,
							OtherPersonalMiles:  3000,
							ParkingAndTolls:     usd(400),
							HasWrittenEvidence:  boolPtr(true),
						},
					},
					HomeOffice: &returnsv1.HomeOffice{
						Method:              returnsv1.HomeOfficeMethod_HOME_OFFICE_METHOD_SIMPLIFIED,
						BusinessSquareFeet:  200,
						TotalHomeSquareFeet: 2000,
					},
				},
			},
			Retirement: &returnsv1.Retirement{
				Persons: []*returnsv1.PersonRetirement{
					{
						Person:                        "alex",
						SelfEmployedPlanContributions: usd(10000),
					},
				},
			},
			Health: &returnsv1.Health{
				SelfEmployedHealthInsurance: []*returnsv1.SelfEmployedHealthInsurance{
					{
						Person:                  "alex",
						Activity:                "biz-1",
						HealthAndDentalPremiums: usd(6000),
						EligibleMonths:          12,
					},
				},
			},
		},
	}

	fed, err := eng.PrepareFederal(inputs)
	if err != nil {
		t.Fatalf("PrepareFederal() failed: %v", err)
	}

	schedCs := fed.GetForms().GetScheduleC()
	if len(schedCs) != 1 {
		t.Fatalf("len(ScheduleC) = %d, want 1", len(schedCs))
	}
	sc := schedCs[0]
	assertMoneyDollars(t, "ScheduleC.GrossReceipts", sc.GetGrossReceiptsOrSales(), 110000)
	assertMoneyDollars(t, "ScheduleC.COGS", sc.GetCostOfGoodsSold(), 10000)
	assertMoneyDollars(t, "ScheduleC.GrossIncome", sc.GetGrossIncome(), 98000)
	assertMoneyDollars(t, "ScheduleC.CarAndTruck", sc.GetCarAndTruckExpenses(), 7400)
	assertMoneyDollars(t, "ScheduleC.DeductibleMeals", sc.GetDeductibleMeals(), 1000)
	assertMoneyDollars(t, "ScheduleC.TotalExpenses", sc.GetTotalExpensesBeforeHomeOffice(), 12000)
	assertMoneyDollars(t, "ScheduleC.TentativeProfit", sc.GetTentativeProfitOrLoss(), 86000)
	assertMoneyDollars(t, "ScheduleC.HomeOffice", sc.GetExpensesForBusinessUseOfHome(), 1000)
	assertMoneyDollars(t, "ScheduleC.NetProfit", sc.GetNetProfitOrLoss(), 85000)

	schedSEs := fed.GetForms().GetScheduleSe()
	if len(schedSEs) != 1 {
		t.Fatalf("len(ScheduleSE) = %d, want 1", len(schedSEs))
	}
	se := schedSEs[0]
	assertMoneyDollars(t, "ScheduleSE.NetEarningsAfterReduction", se.GetNetEarningsAfterReduction(), 78498)
	assertMoneyDollars(t, "ScheduleSE.WageBaseRemaining", se.GetWageBaseRemaining(), 26100)
	assertMoneyDollars(t, "ScheduleSE.SocialSecurityTax", se.GetSocialSecurityTax(), 3236)
	assertMoneyDollars(t, "ScheduleSE.MedicareTax", se.GetMedicareTax(), 2276)
	assertMoneyDollars(t, "ScheduleSE.SelfEmploymentTax", se.GetSelfEmploymentTax(), 5512)
	assertMoneyDollars(t, "ScheduleSE.OneHalfDeduction", se.GetOneHalfDeduction(), 2756)

	s1 := fed.GetForms().GetSchedule_1()
	assertMoneyDollars(t, "Schedule1.BusinessIncome", s1.GetBusinessIncomeOrLoss(), 85000)
	assertMoneyDollars(t, "Schedule1.DeductibleSETax", s1.GetDeductiblePartOfSelfEmploymentTax(), 2756)
	assertMoneyDollars(t, "Schedule1.SelfEmployedRetirement", s1.GetSelfEmployedRetirementPlans(), 10000)
	assertMoneyDollars(t, "Schedule1.SelfEmployedHealthInsurance", s1.GetSelfEmployedHealthInsurance(), 6000)

	f1040 := fed.GetForms().GetForm_1040()
	assertMoneyDollars(t, "Form1040.QBIDeduction", f1040.GetQualifiedBusinessIncomeDeduction(), 13649)
	assertMoneyDollars(t, "Form1040.WithheldFrom1099", f1040.GetWithheldFrom_1099(), 5000)
}

func TestScheduleERentalRealEstateAllowanceAndPassThroughK1s(t *testing.T) {
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() failed: %v", err)
	}

	// Single filer with:
	// - W-2 wages = $120,000
	// - Active-participation rental property with net loss of -$20,000:
	//   - Rents = $18,000, Mortgage interest = $14,000, Taxes = $6,000, Repairs = $3,000, Depreciation = $15,000 -> Total expenses = $38,000 -> Net = -$20,000
	// - Partnership K-1 (Form 1065) where partner materially participated:
	//   - Ordinary business income = $10,000
	//   - Interest income (flows to Schedule B) = $1,000
	//   - Ordinary dividends (flows to Schedule B) = $2,000 (Qualified = $1,500)
	//   - Net long-term capital gain (flows to Schedule D Line 12) = $5,000
	// - Check § 469(i) active-participation rental allowance:
	//   - MAGI before rental loss = $120,000 (W-2) + $1,000 (K-1 int) + $2,000 (K-1 div) + $5,000 (K-1 LTCG) + $10,000 (K-1 nonpassive ordinary) = $138,000
	//   - Excess over $100,000 = $38,000; 50% reduction = $19,000
	//   - Maximum § 469(i) allowance = $25,000 - $19,000 = $6,000 -> Deductible rental loss = -$6,000!
	// - Total Schedule E income (Line 41) = -$6,000 (Part I) + $10,000 (Part II) = +$4,000!
	inputs := &returnsv1.ReturnInputs{
		SchemaVersion: 1,
		TaxYear:       2025,
		Federal: &returnsv1.Federal1040Inputs{
			Household: &returnsv1.Household{
				MaritalStatus: returnsv1.MaritalStatus_MARITAL_STATUS_NEVER_MARRIED,
				Taxpayer: &returnsv1.Person{
					Id:          "taylor",
					FirstName:   "Taylor",
					LastName:    "Reed",
					DateOfBirth: date(1982, 11, 5),
					Tin: &returnsv1.TaxpayerID{
						Kind:               returnsv1.TaxpayerIDKind_TAXPAYER_ID_KIND_SSN,
						ValidForEmployment: boolPtr(true),
					},
				},
			},
			Documents: &returnsv1.Documents{
				W2S: []*returnsv1.W2Document{
					{
						Id:     "w2-taylor",
						Person: "taylor",
						Form: &formw2.FormW2{
							WagesTipsOtherCompensation: usd(120000),
							SocialSecurityWages:        usd(120000),
							MedicareWagesAndTips:       usd(120000),
						},
					},
				},
				PartnershipK1S: []*returnsv1.ScheduleK1Form1065Document{
					{
						Id:          "k1-1065-1",
						Person:      "taylor",
						PassThrough: "pt-1",
						Form: &schedulek1.ScheduleK1Form1065{
							Partnership:            &commonv1.Party{Name: "Apex Ventures LP"},
							PartnershipEin:         "987654321",
							PartnerType:            schedulek1.ScheduleK1Form1065_PARTNER_TYPE_LIMITED_PARTNER_OR_OTHER_LLC_MEMBER,
							OrdinaryBusinessIncome: usd(10000),
							InterestIncome:         usd(1000),
							OrdinaryDividends:      usd(2000),
							QualifiedDividends:     usd(1500),
							NetLongTermCapitalGain: usd(5000),
						},
					},
				},
			},
			Rentals: []*returnsv1.RentalProperty{
				{
					Id:                  "rent-1",
					Owner:               "taylor",
					Description:         "456 Oak Ave Duplex",
					PropertyType:        returnsv1.RentalPropertyType_RENTAL_PROPERTY_TYPE_MULTI_FAMILY,
					FairRentalDays:      365,
					PersonalUseDays:     0,
					ActiveParticipation: boolPtr(true),
					RentsReceived:       usd(18000),
					MortgageInterest:    usd(14000),
					Taxes:               usd(6000),
					Repairs:             usd(3000),
					Depreciation:        usd(15000),
				},
			},
			PassThroughs: []*returnsv1.PassThrough{
				{
					Id:                     "pt-1",
					Owner:                  "taylor",
					EntityName:             "Apex Ventures LP",
					Ein:                    "987654321",
					MateriallyParticipated: boolPtr(true),
				},
			},
		},
	}

	fed, err := eng.PrepareFederal(inputs)
	if err != nil {
		t.Fatalf("PrepareFederal() failed: %v", err)
	}

	se := fed.GetForms().GetScheduleE()
	if len(se.GetProperties()) != 1 {
		t.Fatalf("len(ScheduleE.Properties) = %d, want 1", len(se.GetProperties()))
	}
	prop := se.GetProperties()[0]
	assertMoneyDollars(t, "Rental.NetIncomeOrLoss", prop.GetNetIncomeOrLoss(), -20000)
	assertMoneyDollars(t, "Rental.DeductibleLossAfterLimitation", prop.GetDeductibleLossAfterLimitation(), -6000)
	assertMoneyDollars(t, "ScheduleE.TotalPartnershipAndSCorp", se.GetTotalPartnershipAndSCorpIncomeOrLoss(), 10000)
	assertMoneyDollars(t, "ScheduleE.TotalSupplementalIncome", se.GetTotalSupplementalIncomeOrLoss(), 4000)

	// Verify K-1 portfolio flow-through to Schedule B & Schedule D & Form 1040
	assertMoneyDollars(t, "ScheduleB.TaxableInterest", fed.GetForms().GetScheduleB().GetTaxableInterest(), 1000)
	assertMoneyDollars(t, "ScheduleB.TotalOrdinaryDividends", fed.GetForms().GetScheduleB().GetTotalOrdinaryDividends(), 2000)
	assertMoneyDollars(t, "Form1040.QualifiedDividends", fed.GetForms().GetForm_1040().GetQualifiedDividends(), 1500)
	assertMoneyDollars(t, "ScheduleD.LongTermFromPassThrough", fed.GetForms().GetScheduleD().GetLongTermFromPassThroughEntities(), 5000)
	assertMoneyDollars(t, "Form1040.QBIDeduction", fed.GetForms().GetForm_1040().GetQualifiedBusinessIncomeDeduction(), 2000)
}

func TestQBIAboveThresholdWageLimitAndSSTBPhaseout(t *testing.T) {
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() failed: %v", err)
	}

	// Single filer above the 2025 QBI phase-in range ($197,300 + $50,000 = $247,300):
	// Case 1: Non-SSTB S-Corp K-1 with $300,000 QBI (Box 17 Code V) and $80,000 W-2 wages (Box 17 Code W) ->
	//   20% of QBI = $60,000, but 50% of W-2 wages ($80,000 * 50%) caps QBI deduction at $40,000!
	// Case 2: SSTB S-Corp K-1 above $247,300 -> SSTB QBI deduction completely phases out to $0!
	makeInputs := func(sstb bool) *returnsv1.ReturnInputs {
		return &returnsv1.ReturnInputs{
			SchemaVersion: 1,
			TaxYear:       2025,
			Federal: &returnsv1.Federal1040Inputs{
				Household: &returnsv1.Household{
					MaritalStatus: returnsv1.MaritalStatus_MARITAL_STATUS_NEVER_MARRIED,
					Taxpayer: &returnsv1.Person{
						Id:          "casey",
						FirstName:   "Casey",
						DateOfBirth: date(1980, 2, 14),
						Tin: &returnsv1.TaxpayerID{
							Kind:               returnsv1.TaxpayerIDKind_TAXPAYER_ID_KIND_SSN,
							ValidForEmployment: boolPtr(true),
						},
					},
				},
				Documents: &returnsv1.Documents{
					W2S: []*returnsv1.W2Document{
						{
							Id:     "w2-casey",
							Person: "casey",
							Form: &formw2.FormW2{
								WagesTipsOtherCompensation: usd(80000),
							},
						},
					},
					SCorporationK1S: []*returnsv1.ScheduleK1Form1120SDocument{
						{
							Id:          "k1-1120s-1",
							Person:      "casey",
							PassThrough: "scorp-1",
							Form: &schedulek1.ScheduleK1Form1120S{
								Corporation:            &commonv1.Party{Name: "Casey Corp"},
								CorporationEin:         "112233445",
								OrdinaryBusinessIncome: usd(300000),
								OtherInformation: []*schedulek1.ScheduleK1Form1120S_CodedAmount{
									{Code: "V", Amount: usd(300000)},
									{Code: "W", Amount: usd(80000)},
								},
							},
						},
					},
				},
				PassThroughs: []*returnsv1.PassThrough{
					{
						Id:                       "scorp-1",
						Owner:                    "casey",
						EntityName:               "Casey Corp",
						Ein:                      "112233445",
						MateriallyParticipated:   boolPtr(true),
						SpecifiedServiceBusiness: boolPtr(sstb),
					},
				},
			},
		}
	}

	nonSSTB, err := eng.PrepareFederal(makeInputs(false))
	if err != nil {
		t.Fatalf("PrepareFederal(nonSSTB) failed: %v", err)
	}
	assertMoneyDollars(t, "NonSSTB.QBIDeduction", nonSSTB.GetForms().GetForm_1040().GetQualifiedBusinessIncomeDeduction(), 40000)

	withSSTB, err := eng.PrepareFederal(makeInputs(true))
	if err != nil {
		t.Fatalf("PrepareFederal(withSSTB) failed: %v", err)
	}
	assertMoneyDollars(t, "SSTB.QBIDeduction", withSSTB.GetForms().GetForm_1040().GetQualifiedBusinessIncomeDeduction(), 0)
}

func TestCreditsForm2441Form8863Form5695AndCleanVehicle(t *testing.T) {
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() failed: %v", err)
	}

	// MFJ household with:
	// - Combined W-2 wages = $140,000 ($80,000 + $60,000) -> AGI = $140,000
	// - 1 qualifying child under age 13 ("leo", age 4) with $4,000 of child care expenses ->
	//   Form 2441 caps at $3,000 for 1 child * 20% (AGI > $43,000) = $600 Child & Dependent Care Credit
	// - 1 college student ("maya", age 20, full-time student) with Form 1098-T Box 1 = $6,000 tuition, Box 5 = $2,000 scholarship ->
	//   Net AOTC qualified expenses = $4,000 -> Tentative AOTC = $2,500 (MAGI $140,000 <= $160,000 MFJ) ->
	//   40% Refundable AOTC = $1,000 (Form 1040 line 29), 60% Nonrefundable AOTC = $1,500 (Schedule 3 line 3)
	// - Form 5695 Residential Clean Energy: $10,000 solar electric costs * 30% = $3,000
	// - Form 5695 Home Improvement: $2,000 envelope costs * 30% = $600
	// - Form 8936 Clean Vehicle: $3,500 tentative credit on new EV (MAGI $140,000 <= $300,000 MFJ)
	inputs := &returnsv1.ReturnInputs{
		SchemaVersion: 1,
		TaxYear:       2025,
		Federal: &returnsv1.Federal1040Inputs{
			Household: &returnsv1.Household{
				MaritalStatus:          returnsv1.MaritalStatus_MARITAL_STATUS_MARRIED,
				LivedWithSpouseAnyTime: boolPtr(true),
				Taxpayer: &returnsv1.Person{
					Id:          "chris",
					FirstName:   "Chris",
					DateOfBirth: date(1983, 4, 10),
					Tin: &returnsv1.TaxpayerID{
						Kind:               returnsv1.TaxpayerIDKind_TAXPAYER_ID_KIND_SSN,
						ValidForEmployment: boolPtr(true),
					},
				},
				Spouse: &returnsv1.Person{
					Id:          "pat",
					FirstName:   "Pat",
					DateOfBirth: date(1985, 9, 20),
					Tin: &returnsv1.TaxpayerID{
						Kind:               returnsv1.TaxpayerIDKind_TAXPAYER_ID_KIND_SSN,
						ValidForEmployment: boolPtr(true),
					},
				},
				Dependents: []*returnsv1.Dependent{
					{
						Id:                              "leo",
						FirstName:                       "Leo",
						DateOfBirth:                     date(2021, 5, 1),
						Relationship:                    returnsv1.Relationship_RELATIONSHIP_CHILD,
						MonthsLivedWithYou:              int32Ptr(12),
						CitizenOrResidentOfNorthAmerica: boolPtr(true),
						Tin: &returnsv1.TaxpayerID{
							Kind:               returnsv1.TaxpayerIDKind_TAXPAYER_ID_KIND_SSN,
							ValidForEmployment: boolPtr(true),
						},
					},
					{
						Id:                              "maya",
						FirstName:                       "Maya",
						DateOfBirth:                     date(2005, 8, 15),
						Relationship:                    returnsv1.Relationship_RELATIONSHIP_CHILD,
						MonthsLivedWithYou:              int32Ptr(12),
						FullTimeStudent:                 boolPtr(true),
						CitizenOrResidentOfNorthAmerica: boolPtr(true),
						Tin: &returnsv1.TaxpayerID{
							Kind:               returnsv1.TaxpayerIDKind_TAXPAYER_ID_KIND_SSN,
							ValidForEmployment: boolPtr(true),
						},
					},
				},
			},
			Documents: &returnsv1.Documents{
				W2S: []*returnsv1.W2Document{
					{
						Id:     "w2-chris",
						Person: "chris",
						Form: &formw2.FormW2{
							WagesTipsOtherCompensation: usd(80000),
							FederalTaxWithheld:         usd(9000),
						},
					},
					{
						Id:     "w2-pat",
						Person: "pat",
						Form: &formw2.FormW2{
							WagesTipsOtherCompensation: usd(60000),
							FederalTaxWithheld:         usd(6000),
						},
					},
				},
				Tuition: []*returnsv1.Form1098TDocument{
					{
						Id:      "1098t-maya",
						Person:  "maya",
						Student: "stu-maya",
						Form: &form1098t.Form1098T{
							PaymentsReceivedForQualifiedTuition: usd(6000),
							ScholarshipsOrGrants:                usd(2000),
							AtLeastHalfTimeStudent:              true,
						},
					},
				},
			},
			Credits: &returnsv1.Credits{
				DependentCare: &returnsv1.DependentCare{
					PersonExpenses: []*returnsv1.QualifyingPersonCareExpense{
						{
							Person:            "leo",
							QualifiedExpenses: usd(4000),
						},
					},
				},
				Education: []*returnsv1.StudentEducation{
					{
						Id:                      "stu-maya",
						Person:                  "maya",
						CreditElection:          returnsv1.EducationCreditElection_EDUCATION_CREDIT_ELECTION_AMERICAN_OPPORTUNITY,
						EnrolledAtLeastHalfTime: boolPtr(true),
					},
				},
				ResidentialEnergy: &returnsv1.ResidentialEnergy{
					SolarElectricCosts:    usd(10000),
					EnvelopeAndAuditCosts: usd(2000),
				},
				CleanVehicles: []*returnsv1.CleanVehicle{
					{
						Vin:                   "5YJ3E1EA7PF000001",
						DatePlacedInService:   date(2025, 6, 15),
						PreviouslyOwned:       boolPtr(false),
						TentativeCreditAmount: usd(3500),
					},
				},
			},
		},
	}

	fed, err := eng.PrepareFederal(inputs)
	if err != nil {
		t.Fatalf("PrepareFederal() failed: %v", err)
	}

	s3 := fed.GetForms().GetSchedule_3()
	assertMoneyDollars(t, "Schedule3.DependentCareCredit", s3.GetChildAndDependentCareCredit(), 600)
	assertMoneyDollars(t, "Schedule3.EducationCredits", s3.GetEducationCredits(), 1500)
	assertMoneyDollars(t, "Schedule3.ResidentialCleanEnergy", s3.GetResidentialCleanEnergyCredit(), 3000)
	assertMoneyDollars(t, "Schedule3.HomeImprovementCredit", s3.GetEnergyEfficientHomeImprovementCredit(), 600)
	assertMoneyDollars(t, "Schedule3.OtherNonrefundableCredits", s3.GetTotalOtherNonrefundableCredits(), 3500)

	f1040 := fed.GetForms().GetForm_1040()
	assertMoneyDollars(t, "Form1040.AmericanOpportunityCredit", f1040.GetAmericanOpportunityCredit(), 1000)
}

func TestEarnedIncomeCreditAndSaversCredit(t *testing.T) {
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() failed: %v", err)
	}

	// Single filer age 30 with 1 qualifying child ("mia", age 6):
	// - W-2 wages = $22,000
	// - Roth IRA contribution = $2,000
	// - Saver's Credit (Form 8880): AGI $22,000 <= $23,750 (2025 Single 50% tier) ->
	//   50% of $2,000 = $1,000 tentative Saver's Credit, capped at tax liability ($22,000 - $15,750 std ded = $6,250 taxable -> $625 tax) = $625!
	// - Earned Income Credit (§ 32, 1 child in 2025):
	//   - Phase-in: 34% of $22,000 = $7,480 -> capped at max credit $4,328
	//   - AGI $22,000 <= $23,350 phaseout start -> full $4,328 EIC!
	inputs := &returnsv1.ReturnInputs{
		SchemaVersion: 1,
		TaxYear:       2025,
		Federal: &returnsv1.Federal1040Inputs{
			Household: &returnsv1.Household{
				MaritalStatus: returnsv1.MaritalStatus_MARITAL_STATUS_NEVER_MARRIED,
				Taxpayer: &returnsv1.Person{
					Id:          "elena",
					FirstName:   "Elena",
					DateOfBirth: date(1995, 3, 10),
					Tin: &returnsv1.TaxpayerID{
						Kind:               returnsv1.TaxpayerIDKind_TAXPAYER_ID_KIND_SSN,
						ValidForEmployment: boolPtr(true),
					},
				},
				Dependents: []*returnsv1.Dependent{
					{
						Id:                              "mia",
						FirstName:                       "Mia",
						DateOfBirth:                     date(2019, 7, 1),
						Relationship:                    returnsv1.Relationship_RELATIONSHIP_CHILD,
						MonthsLivedWithYou:              int32Ptr(12),
						CitizenOrResidentOfNorthAmerica: boolPtr(true),
						Tin: &returnsv1.TaxpayerID{
							Kind:               returnsv1.TaxpayerIDKind_TAXPAYER_ID_KIND_SSN,
							ValidForEmployment: boolPtr(true),
						},
					},
				},
			},
			Documents: &returnsv1.Documents{
				W2S: []*returnsv1.W2Document{
					{
						Id:     "w2-elena",
						Person: "elena",
						Form: &formw2.FormW2{
							WagesTipsOtherCompensation: usd(22000),
						},
					},
				},
			},
			Retirement: &returnsv1.Retirement{
				Persons: []*returnsv1.PersonRetirement{
					{
						Person:               "elena",
						RothIraContributions: usd(2000),
					},
				},
			},
		},
	}

	fed, err := eng.PrepareFederal(inputs)
	if err != nil {
		t.Fatalf("PrepareFederal() failed: %v", err)
	}

	assertMoneyDollars(t, "Schedule3.SaversCredit", fed.GetForms().GetSchedule_3().GetRetirementSavingsContributionsCredit(), 625)
	assertMoneyDollars(t, "Form1040.EarnedIncomeCredit", fed.GetForms().GetForm_1040().GetEarnedIncomeCredit(), 4328)
}

func TestForm8606ProRataRothConversionForm6251AMTAndForm1040XAmendment(t *testing.T) {
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() failed: %v", err)
	}

	// Single filer with:
	// - W-2 wages = $100,000 (with $20,000 withheld)
	// - Form 8606 Roth conversion of $50,000:
	//   - Prior traditional IRA basis = $20,000
	//   - Year-end remaining traditional IRA value = $50,000 -> Total denominator (FMV $50k + conversion $50k) = $100,000
	//   - Pro-rata nontaxable ratio = $20,000 / $100,000 = 20% -> $10,000 tax-free, $40,000 taxable IRA distribution!
	// - Post-2017 NOL carryforward = $10,000 -> reduces Schedule 1 additional income by -$10,000
	// - Specified private activity bond interest (Form 1099-INT Box 9) = $80,000 (tax-exempt for regular tax, added back for Form 6251 AMT!)
	// - Form 1040-X Amendment baseline (`original_figures`):
	//   - Originally reported AGI = $100,000, Total Tax = $13,841, Tax Withheld = $20,000 (Original overpayment = $6,159)
	//   - Corrected AGI = $100,000 (wages) + $40,000 (taxable Roth conversion) - $10,000 (NOL) = $130,000!
	inputs := &returnsv1.ReturnInputs{
		SchemaVersion: 1,
		TaxYear:       2025,
		Federal: &returnsv1.Federal1040Inputs{
			Household: &returnsv1.Household{
				MaritalStatus: returnsv1.MaritalStatus_MARITAL_STATUS_NEVER_MARRIED,
				Taxpayer: &returnsv1.Person{
					Id:          "morgan",
					FirstName:   "Morgan",
					DateOfBirth: date(1978, 5, 20),
					Tin: &returnsv1.TaxpayerID{
						Kind:               returnsv1.TaxpayerIDKind_TAXPAYER_ID_KIND_SSN,
						ValidForEmployment: boolPtr(true),
					},
				},
			},
			Documents: &returnsv1.Documents{
				W2S: []*returnsv1.W2Document{
					{
						Id:     "w2-morgan",
						Person: "morgan",
						Form: &formw2.FormW2{
							WagesTipsOtherCompensation: usd(100000),
							FederalTaxWithheld:         usd(20000),
						},
					},
				},
				Interest: []*returnsv1.Form1099INTDocument{
					{
						Id:     "int-pab",
						Person: "morgan",
						Form: &form1099int.Form1099INT{
							Payer:                                &commonv1.Party{Name: "Airport Revenue Bonds"},
							TaxExemptInterest:                    usd(80000),
							SpecifiedPrivateActivityBondInterest: usd(80000),
						},
					},
				},
			},
			Retirement: &returnsv1.Retirement{
				Persons: []*returnsv1.PersonRetirement{
					{
						Person: "morgan",
						IraBasis: &returnsv1.IRABasis{
							PriorTraditionalIraBasis:   usd(20000),
							YearEndTraditionalIraValue: usd(50000),
							RothConversions:            usd(50000),
						},
					},
				},
			},
			PriorYear: &returnsv1.PriorYear{
				NolCarryover: &returnsv1.NOLCarryover{
					Post_2017NolCarryover: usd(10000),
				},
			},
		},
		Amendment: &returnsv1.Amendment{
			Federal: &returnsv1.JurisdictionAmendment{
				Kind:                  returnsv1.AmendmentKind_AMENDMENT_KIND_AMENDED,
				OverpaymentOnOriginal: usd(6159),
				Baseline: &returnsv1.JurisdictionAmendment_OriginalFigures{
					OriginalFigures: &returnsv1.OriginalFigures{
						FilingStatus:        form1040.FilingStatus_FILING_STATUS_SINGLE,
						AdjustedGrossIncome: usd(100000),
						Deductions:          usd(15750),
						TaxableIncome:       usd(84250),
						Tax:                 usd(13841),
						TotalTax:            usd(13841),
						TaxWithheld:         usd(20000),
					},
				},
			},
		},
	}

	fed, err := eng.PrepareFederal(inputs)
	if err != nil {
		t.Fatalf("PrepareFederal() failed: %v", err)
	}

	f1040 := fed.GetForms().GetForm_1040()
	assertMoneyDollars(t, "Form1040.IraDistributions", f1040.GetIraDistributions(), 50000)
	assertMoneyDollars(t, "Form1040.IraDistributionsTaxable", f1040.GetIraDistributionsTaxable(), 40000)
	assertMoneyDollars(t, "Form1040.AdditionalIncome", f1040.GetAdditionalIncome(), -10000)
	assertMoneyDollars(t, "Form1040.AGI", f1040.GetAdjustedGrossIncome(), 130000)

	// Check Form 6251 AMT:
	// - Regular taxable income = $130,000 - $15,750 = $114,250; Regular tax = $20,267
	// - AMTI = $114,250 + $15,750 (std ded addback) + $80,000 (PAB interest) = $210,000
	// - 2025 Single AMT exemption = $88,100 -> AMT taxable = $210,000 - $88,100 = $121,900
	// - Tentative Minimum Tax = 26% of $121,900 = $31,694
	// - AMT (Schedule 2 line 1) = $31,694 - $20,267 = $11,427 -> Total tax = $31,694!
	assertMoneyDollars(t, "Schedule2.AMT", fed.GetForms().GetSchedule_2().GetAlternativeMinimumTax(), 11427)
	assertMoneyDollars(t, "Form1040.TotalTax", f1040.GetTotalTax(), 31694)

	// Check Form 1040-X AmendmentResult:
	// - Column A AGI = $100,000; Column B Net Change AGI = +$30,000; Column C Corrected AGI = $130,000
	// - Column A Total Tax = $13,841; Column B Net Change Total Tax = +$17,853; Column C Total Tax = $31,694
	// - Net payments remaining after original $6,159 refund = $20,000 - $6,159 = $13,841
	// - Additional amount owed on Form 1040-X line 20 = $31,694 - $13,841 = $17,853!
	am := fed.GetAmendment()
	assertMoneyDollars(t, "Amendment.ColB.AGI", am.GetColumnBNetChange().GetAdjustedGrossIncome(), 30000)
	assertMoneyDollars(t, "Amendment.ColC.AGI", am.GetColumnCCorrected().GetAdjustedGrossIncome(), 130000)
	assertMoneyDollars(t, "Amendment.ColB.TotalTax", am.GetColumnBNetChange().GetTotalTax(), 17853)
	assertMoneyDollars(t, "Amendment.AdditionalAmountOwed", am.GetAdditionalAmountOwed(), 17853)
}

func qty(units int64, scale int32) *commonv1.Quantity {
	return &commonv1.Quantity{Units: units, Scale: scale}
}

func TestForm4562DepreciationForm4797RecaptureForm6252InstallmentAndForm8824LikeKindExchange(t *testing.T) {
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() failed: %v", err)
	}

	// Single filer in 2025 with:
	// 1. Schedule C Business ("biz-1", gross receipts $250,000) with 3 depreciable assets:
	//    - Asset 1: Heavy SUV (GVWR > 6,000 lbs) placed in service 2025-02-15, cost $60,000,
	//      100% business use, 5-yr recovery, elected $50,000 § 179 -> capped at $31,300 SUV
	//      limit (§ 179(b)(5)), remaining $28,700 gets 100% OBBBA bonus depreciation = $28,700
	//      (total deduction = $60,000).
	//    - Asset 2: 5-yr equipment placed in service 2025-06-01, cost $50,000, opted out of bonus ->
	//      MACRS 200% DB HY Year 1 (20%) = $10,000; AMT 150% DB HY Year 1 (15%) = $7,500 ->
	//      AMT depreciation adjustment = $2,500.
	//    - Asset 3: Luxury passenger auto placed in service 2025-03-01, cost $40,000, 100% business
	//      use, bonus claimed -> capped at § 280F Year 1 limit = $20,400.
	//    -> Form 4562 totals: § 179 = $31,300; Bonus = $49,100 ($28,700 + $20,400); MACRS = $10,000;
	//       Total depreciation = $90,400; AMT adjustment = $2,500.
	// 2. Form 4797 standalone sale of § 1245 equipment acquired 2021-03-01 for $80,000, depreciation
	//    allowed = $50,000 (adj basis = $30,000), sold 2025-07-01 for $95,000 ->
	//    realized gain = $65,000; § 1245 ordinary recapture = $50,000; § 1231 gain = $15,000.
	// 3. Form 8824 Like-Kind Exchange: relinquished real property adjusted basis = $200,000,
	//    replacement FMV = $300,000, boot received = $25,000 -> realized gain = $125,000,
	//    recognized § 1231 gain = $25,000, deferred gain = $100,000, replacement basis = $200,000.
	// 4. Form 6252 first-year installment sale: selling price = $100,000, cost basis = $60,000,
	//    depreciation allowed = $10,000, ordinary depreciation recapture = $10,000 (§ 453(i) immediate
	//    ordinary recapture = $10,000; installment basis = $60,000; gross profit = $40,000; contract
	//    price = $100,000 -> GP ratio = 40%); payments received this year = $25,000 ->
	//    taxable installment LTCG = $10,000.
	// 5. Prior-year nonrecaptured net § 1231 losses = $6,000:
	//    - Gross § 1231 gain = $15,000 (sale) + $25,000 (Form 8824) = $40,000
	//    - Lookback recharacterized as ordinary income = $6,000
	//    - Net § 1231 LTCG carried to Schedule D Line 11 = $34,000 (+ $10,000 installment LTCG = $44,000)
	//    - Total Form 4797 ordinary gain on Schedule 1 Line 4 = $50,000 (1245) + $10,000 (6252) + $6,000 (lookback) = $66,000.
	inputs := &returnsv1.ReturnInputs{
		SchemaVersion: 1,
		TaxYear:       2025,
		Federal: &returnsv1.Federal1040Inputs{
			Household: &returnsv1.Household{
				MaritalStatus: returnsv1.MaritalStatus_MARITAL_STATUS_NEVER_MARRIED,
				Taxpayer: &returnsv1.Person{
					Id:          "taylor",
					FirstName:   "Taylor",
					DateOfBirth: date(1980, 4, 10),
					Tin: &returnsv1.TaxpayerID{
						Kind:               returnsv1.TaxpayerIDKind_TAXPAYER_ID_KIND_SSN,
						ValidForEmployment: boolPtr(true),
					},
				},
			},
			Businesses: []*returnsv1.Business{
				{
					Id:                        "biz-1",
					Owner:                     "taylor",
					Name:                      "Taylor Logistics",
					MateriallyParticipated:    boolPtr(true),
					CashAndUnreportedReceipts: usd(250000),
					Assets: []*returnsv1.DepreciableAsset{
						{
							Id:                  "suv-1",
							Description:         "Heavy Cargo SUV",
							DatePlacedInService: date(2025, 2, 15),
							CostBasis:           usd(60000),
							BusinessUsePercent:  qty(100, 0),
							RecoveryPeriodYears: qty(5, 0),
							Section_179Elected:  usd(50000),
							ListedPropertyKind:  returnsv1.ListedPropertyKind_LISTED_PROPERTY_KIND_HEAVY_SUV,
						},
						{
							Id:                      "equip-1",
							Description:             "CNC Milling Machine",
							DatePlacedInService:     date(2025, 6, 1),
							CostBasis:               usd(50000),
							BusinessUsePercent:      qty(100, 0),
							RecoveryPeriodYears:     qty(5, 0),
							OptOutBonusDepreciation: boolPtr(true),
						},
						{
							Id:                  "auto-1",
							Description:         "Client Sedan",
							DatePlacedInService: date(2025, 3, 1),
							CostBasis:           usd(40000),
							BusinessUsePercent:  qty(100, 0),
							RecoveryPeriodYears: qty(5, 0),
							ListedPropertyKind:  returnsv1.ListedPropertyKind_LISTED_PROPERTY_KIND_PASSENGER_AUTO,
						},
					},
				},
			},
			Investments: &returnsv1.Investments{
				BusinessPropertySales: []*returnsv1.BusinessPropertySale{
					{
						Id:                     "sale-1245",
						Description:            "Used Forklift",
						DateAcquired:           date(2021, 3, 1),
						DateSold:               date(2025, 7, 1),
						GrossSalesPrice:        usd(95000),
						CostOrOtherBasis:       usd(80000),
						DepreciationAllowed:    usd(50000),
						IsSection_1245Property: boolPtr(true),
					},
				},
				LikeKindExchanges: []*returnsv1.LikeKindExchange{
					{
						Id:                      "lke-1",
						DescriptionRelinquished: "Warehouse A",
						DescriptionReplacement:  "Warehouse B",
						DateTransferred:         date(2025, 4, 1),
						DateReceived:            date(2025, 5, 15),
						FmvReplacementProperty:  usd(300000),
						BootReceived:            usd(25000),
						AdjustedBasisGivenUp:    usd(200000),
					},
				},
				InstallmentSales: []*returnsv1.InstallmentSale{
					{
						Id:                            "inst-1",
						Description:                   "Commercial Lot",
						DateAcquired:                  date(2019, 1, 10),
						DateSold:                      date(2025, 8, 1),
						LongTerm:                      boolPtr(true),
						SellingPrice:                  usd(100000),
						CostBasis:                     usd(60000),
						DepreciationAllowed:           usd(10000),
						OrdinaryDepreciationRecapture: usd(10000),
						PaymentsReceivedThisYear:      usd(25000),
					},
				},
			},
			PriorYear: &returnsv1.PriorYear{
				NonrecapturedNetSection_1231Losses: usd(6000),
			},
		},
	}

	fed, err := eng.PrepareFederal(inputs)
	if err != nil {
		t.Fatalf("PrepareFederal() failed: %v", err)
	}

	wp := fed.GetWorkpapers()
	f4562 := wp.GetForm_4562()
	assertMoneyDollars(t, "Form4562.Section179Allowed", f4562.GetTotalSection_179Allowed(), 31300)
	assertMoneyDollars(t, "Form4562.BonusDepreciation", f4562.GetTotalBonusDepreciation(), 49100)
	assertMoneyDollars(t, "Form4562.MACRSDepreciation", f4562.GetTotalMacrsDepreciation(), 10000)
	assertMoneyDollars(t, "Form4562.TotalDepreciation", f4562.GetTotalDepreciationDeduction(), 90400)
	assertMoneyDollars(t, "Form4562.AMTAdjustment", f4562.GetAmtDepreciationAdjustment(), 2500)

	// Schedule C net profit = $250,000 - $90,400 = $159,600
	assertMoneyDollars(t, "ScheduleC.NetProfit", fed.GetForms().GetScheduleC()[0].GetNetProfitOrLoss(), 159600)

	f8824 := wp.GetForm_8824()
	assertMoneyDollars(t, "Form8824.RealizedGain", f8824.GetTotalRealizedGain(), 125000)
	assertMoneyDollars(t, "Form8824.RecognizedGain", f8824.GetTotalRecognizedGain(), 25000)
	assertMoneyDollars(t, "Form8824.DeferredGain", f8824.GetTotalDeferredGain(), 100000)
	assertMoneyDollars(t, "Form8824.ReplacementBasis", f8824.GetReplacementPropertyBasis(), 200000)

	f6252 := wp.GetForm_6252()
	assertMoneyDollars(t, "Form6252.RecaptureOrdinary", f6252.GetYearOfSaleRecaptureOrdinaryIncome(), 10000)
	assertMoneyDollars(t, "Form6252.TaxableInstallmentGain", f6252.GetTotalTaxableInstallmentGain(), 10000)

	f4797 := wp.GetForm_4797()
	assertMoneyDollars(t, "Form4797.Gross1231Gain", f4797.GetGrossSection_1231GainOrLoss(), 40000)
	assertMoneyDollars(t, "Form4797.LookbackRecapture", f4797.GetLookbackRecapturedAsOrdinary(), 6000)
	assertMoneyDollars(t, "Form4797.Net1231ToScheduleD", f4797.GetNetSection_1231LtcgToScheduleD(), 34000)
	assertMoneyDollars(t, "Form4797.OrdinaryRecapture", f4797.GetOrdinaryDepreciationRecapture(), 60000)
	assertMoneyDollars(t, "Form4797.TotalOrdinaryGain", f4797.GetTotalOrdinaryGainOrLoss(), 66000)

	// Schedule 1 Line 4 (other_gains_or_losses) = $66,000; Schedule D Line 11 = $34,000 + $10,000 = $44,000
	assertMoneyDollars(t, "Schedule1.OtherGainsOrLosses", fed.GetForms().GetSchedule_1().GetOtherGainsOrLosses(), 66000)
	assertMoneyDollars(t, "ScheduleD.LongTermFrom4797And6252", fed.GetForms().GetScheduleD().GetLongTermGainFromForm_4797AndOthers(), 44000)
}

func TestForm8582DispositionsAndPTPForm7203SCorpBasisForm4952AndDualBasisAMT(t *testing.T) {
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() failed: %v", err)
	}

	// Scenario:
	// 1. Rental #1 ("rent-disposed") has a complete taxable disposition in 2025:
	//    Current rental loss -$5,000 + prior suspended passive loss $15,000 = $20,000 unlocked under IRC § 469(g).
	// 2. PTP Partnership K-1 ("ptp-1") has a passive loss of -$8,000:
	//    Isolated under IRC § 469(k), suspended in full ($8,000).
	// 3. S-Corp K-1 ("scorp-1", material participation):
	//    Beginning stock basis $10,000, distributions $14,000 -> $4,000 excess distribution LTCG (Form 7203 / Schedule D),
	//    stock basis reduced to $0; direct shareholder debt basis $12,000 absorbs -$9,000 of the -$15,000 ordinary loss
	//    Wait: $12,000 debt basis absorbs $12,000 of the -$15,000 ordinary loss, disallowing $3,000 to carry forward.
	// 4. Dual-basis AMT: ISO lot sold in 2025 with regular basis $20,000, AMT basis $50,000, proceeds $70,000
	//    -> Regular LTCG = $50,000, AMT LTCG = $20,000 -> Form 6251 Line 2k adjustment = -$30,000.
	// 5. Form 4952: Investment interest expense $12,000 with $5,000 taxable interest + $3,000 elected capital gain = $8,000 allowed, $4,000 carryforward.
	// 6. Form 8801: Prior-year AMT $6,000 with $2,000 exclusion items -> $4,000 net minimum tax on deferral items + $1,000 prior carryforward = $5,000 credit.
	inputs := &returnsv1.ReturnInputs{
		SchemaVersion: 1,
		TaxYear:       2025,
		Federal: &returnsv1.Federal1040Inputs{
			Household: &returnsv1.Household{
				MaritalStatus: returnsv1.MaritalStatus_MARITAL_STATUS_NEVER_MARRIED,
				Taxpayer: &returnsv1.Person{
					Id:          "tp",
					FirstName:   "Taylor",
					LastName:    "Vance",
					DateOfBirth: date(1982, 4, 10),
					Tin: &returnsv1.TaxpayerID{
						Kind:               returnsv1.TaxpayerIDKind_TAXPAYER_ID_KIND_SSN,
						ValidForEmployment: boolPtr(true),
					},
				},
				Dependents: []*returnsv1.Dependent{
					{
						Id:                              "dep-custodial",
						FirstName:                       "Sam",
						LastName:                        "Vance",
						DateOfBirth:                     date(2018, 6, 1),
						CitizenOrResidentOfNorthAmerica: boolPtr(true),
						Relationship:                    returnsv1.Relationship_RELATIONSHIP_CHILD,
						MonthsLivedWithYou:              int32Ptr(10),
						YouProvidedOverHalfSupport:      boolPtr(true),
						CustodialRelease:                returnsv1.CustodialRelease_CUSTODIAL_RELEASE_RELEASED_BY_YOU,
						Tin: &returnsv1.TaxpayerID{
							Kind:               returnsv1.TaxpayerIDKind_TAXPAYER_ID_KIND_SSN,
							ValidForEmployment: boolPtr(true),
						},
					},
				},
			},
			Documents: &returnsv1.Documents{
				W2S: []*returnsv1.W2Document{
					{
						Id:     "w2-1",
						Person: "tp",
						Form: &formw2.FormW2{
							WagesTipsOtherCompensation: usd(180000),
						},
					},
				},
				Interest: []*returnsv1.Form1099INTDocument{
					{
						Id: "int-1",
						Form: &form1099int.Form1099INT{
							InterestIncome: usd(5000),
						},
					},
				},
				PartnershipK1S: []*returnsv1.ScheduleK1Form1065Document{
					{
						Id:          "k1-ptp",
						PassThrough: "ptp-1",
						Form: &schedulek1.ScheduleK1Form1065{
							Partnership:            &commonv1.Party{Name: "Energy MLP LP"},
							PartnershipEin:         "770000001",
							OrdinaryBusinessIncome: usd(-8000),
						},
					},
				},
				SCorporationK1S: []*returnsv1.ScheduleK1Form1120SDocument{
					{
						Id:          "k1-scorp",
						PassThrough: "scorp-1",
						Form: &schedulek1.ScheduleK1Form1120S{
							Corporation:            &commonv1.Party{Name: "Vance Labs Inc"},
							CorporationEin:         "770000002",
							OrdinaryBusinessIncome: usd(-15000),
						},
					},
				},
			},
			Rentals: []*returnsv1.RentalProperty{
				{
					Id:                         "rent-disposed",
					Owner:                      "tp",
					Description:                "Sold Condo",
					PropertyType:               returnsv1.RentalPropertyType_RENTAL_PROPERTY_TYPE_SINGLE_FAMILY,
					ActiveParticipation:        boolPtr(true),
					CompleteTaxableDisposition: boolPtr(true),
					RentsReceived:              usd(10000),
					OtherExpenses:              usd(15000),
				},
			},
			PassThroughs: []*returnsv1.PassThrough{
				{
					Id:                        "ptp-1",
					Owner:                     "tp",
					EntityName:                "Energy MLP LP",
					PubliclyTradedPartnership: boolPtr(true),
					MateriallyParticipated:    boolPtr(false),
				},
				{
					Id:                     "scorp-1",
					Owner:                  "tp",
					EntityName:             "Vance Labs Inc",
					MateriallyParticipated: boolPtr(true),
					SCorpBasis: &returnsv1.SCorpBasisInputs{
						BeginningStockBasis: usd(10000),
						Distributions:       usd(14000),
						BeginningDebtBasis:  usd(12000),
					},
				},
			},
			Investments: &returnsv1.Investments{
				OtherSales: []*returnsv1.OtherSale{
					{
						Id:           "iso-sale",
						Person:       "tp",
						Description:  "1000 sh Acme ISO",
						DateAcquired: date(2023, 5, 1),
						DateSold:     date(2025, 9, 1),
						Proceeds:     usd(70000),
						Basis:        usd(20000),
						AmtBasis:     usd(50000),
					},
				},
			},
			Deductions: &returnsv1.Deductions{
				InvestmentInterest: &returnsv1.InvestmentInterest{
					InterestPaid:                            usd(12000),
					ElectedCapitalGainAndQualifiedDividends: usd(3000),
				},
			},
			PriorYear: &returnsv1.PriorYear{
				PassiveActivityCarryovers: []*returnsv1.PassiveActivityCarryover{
					{
						Activity:                      "rent-disposed",
						RegularOperatingLossCarryover: usd(15000),
					},
				},
				AmtCreditCarryover: &returnsv1.AMTCreditCarryover{
					CreditCarryforward:    usd(1000),
					PriorYearAmtPaid:      usd(6000),
					PriorYearExclusionAmt: usd(2000),
				},
			},
		},
	}

	fed, err := eng.PrepareFederal(inputs)
	if err != nil {
		t.Fatalf("PrepareFederal() failed: %v", err)
	}

	wp := fed.GetWorkpapers()

	// Form 8582:
	// - Disposed rental ("rent-disposed"): -$5,000 current + -$15,000 prior = $20,000 unlocked under § 469(g)
	// - PTP ("ptp-1"): -$8,000 suspended in isolated PTP basket under § 469(k)
	f8582 := wp.GetForm_8582()
	assertMoneyDollars(t, "Form8582.DisposedUnlockedLosses", f8582.GetUnlockedDispositionLosses(), 20000)
	assertMoneyDollars(t, "Form8582.PTPSuspendedLosses", f8582.GetSuspendedPtpLossesCarryforward(), 8000)

	// Form 7203:
	// - Excess distribution gain = $14,000 - $10,000 = $4,000 (flows to Schedule D LTCG)
	// - Allowable loss from debt basis = $12,000; disallowed loss carryforward = $3,000; ending stock & debt basis = $0
	f7203 := wp.GetForm_7203()
	assertMoneyDollars(t, "Form7203.ExcessDistributionsGain", f7203.GetTaxableExcessDistributionsAndRepayments(), 4000)
	assertMoneyDollars(t, "Form7203.AllowableLosses", f7203.GetAllowedSCorpLosses(), 12000)
	assertMoneyDollars(t, "Form7203.DisallowedCarryforward", f7203.GetDisallowedSCorpLossesCarryforward(), 3000)
	assertMoneyDollars(t, "Form7203.EndingStockBasis", f7203.GetEndingStockBasis(), 0)
	assertMoneyDollars(t, "Form7203.EndingDebtBasis", f7203.GetEndingDebtBasis(), 0)

	// Schedule D LTCG includes $50,000 ISO sale + $4,000 S-Corp excess distribution = $54,000
	assertMoneyDollars(t, "ScheduleD.NetLTCG", fed.GetForms().GetScheduleD().GetNetLongTermGainOrLoss(), 54000)

	// Form 4952: $5,000 interest + $3,000 elected preferential = $8,000 allowed, $4,000 carryforward
	f4952 := wp.GetForm_4952()
	assertMoneyDollars(t, "Form4952.DeductibleInterest", f4952.GetDeductibleInvestmentInterest(), 8000)
	assertMoneyDollars(t, "Form4952.Carryforward", f4952.GetDisallowedCarryforwardToNextYear(), 4000)

	// Form 8332: Custodial parent who released claim gets $0 Child Tax Credit
	assertMoneyDollars(t, "Form1040.ChildTaxCredit", fed.GetForms().GetForm_1040().GetChildTaxCredit(), 0)

	// Form 8801: $1,000 carryforward + ($6,000 - $2,000) deferral AMT = $5,000 Schedule 3 Line 8 total nonrefundable credits
	assertMoneyDollars(t, "Schedule3.TotalOtherNonrefundableCredits", fed.GetForms().GetSchedule_3().GetTotalOtherNonrefundableCredits(), 5000)
}

func TestForm2555Form1116ScheduleFAndJScheduleHKiddieTaxForm5329Pub974AndForm2210(t *testing.T) {
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() failed: %v", err)
	}

	// Sub-scenario 1: Form 2555 (FEIE + Housing Exclusion + Stacking Tax), Form 1116 (Multi-Basket FTC + 10-Yr Carryovers),
	// Schedule F (Farming + SE Tax), Schedule H (Household Employment), Form 8814 (Parent's Election),
	// Form 4684 (Qualified Disaster Loss), and Form 5329 Parts II-IX.
	inputs1 := &returnsv1.ReturnInputs{
		SchemaVersion: 1,
		TaxYear:       2025,
		Federal: &returnsv1.Federal1040Inputs{
			Household: &returnsv1.Household{
				MaritalStatus: returnsv1.MaritalStatus_MARITAL_STATUS_NEVER_MARRIED,
				Taxpayer: &returnsv1.Person{
					Id:          "tp",
					FirstName:   "Elena",
					LastName:    "Vance",
					DateOfBirth: date(1980, 5, 10),
					Tin: &returnsv1.TaxpayerID{
						Kind:               returnsv1.TaxpayerIDKind_TAXPAYER_ID_KIND_SSN,
						ValidForEmployment: boolPtr(true),
					},
				},
				HouseholdEmployment: &returnsv1.HouseholdEmployment{
					SocialSecurityWages:        usd(10000),
					MedicareWages:              usd(10000),
					FederalIncomeTaxWithheld:   usd(500),
					FutaTaxableWages:           usd(7000),
					QualifiesForFullFutaCredit: boolPtr(true),
				},
				Form_8814Children: []*returnsv1.ChildDividendInclusion{
					{
						Dependent:                "child-1",
						TaxableInterest:          usd(1700),
						OrdinaryDividends:        usd(2000),
						QualifiedDividends:       usd(1000),
						CapitalGainDistributions: usd(1300),
						FederalIncomeTaxWithheld: usd(150),
					},
				},
			},
			Documents: &returnsv1.Documents{
				W2S: []*returnsv1.W2Document{
					{
						Id:     "w2-expat",
						Person: "tp",
						Form: &formw2.FormW2{
							WagesTipsOtherCompensation: usd(160000),
							FederalTaxWithheld:         usd(10000),
						},
					},
				},
			},
			ForeignEarnedIncome: []*returnsv1.ForeignEarnedIncome{
				{
					Person:                   "tp",
					ForeignCountry:           "CH",
					BonaFideResidence:        boolPtr(true),
					QualifyingDaysInTaxYear:  365,
					ForeignEarnedWages:       usd(160000),
					QualifiedHousingExpenses: usd(30800),
				},
			},
			Farms: []*returnsv1.FarmBusiness{
				{
					Id:                     "farm-1",
					Owner:                  "tp",
					PrincipalProduct:       "Vance Orchard",
					RaisedProductsSales:    usd(90000),
					OperatingExpenses:      usd(40000),
					MateriallyParticipated: boolPtr(true),
				},
			},
			Retirement: &returnsv1.Retirement{
				Persons: []*returnsv1.PersonRetirement{
					{
						Person: "tp",
						Form_5329: &returnsv1.Form5329Inputs{
							TaxableEducationOrAbleDistributions: usd(2000),
							PriorExcessTraditionalIra:           usd(3000),
							PriorExcessRothIra:                  usd(1000),
							ExcessCoverdellEsaContributions:     usd(500),
							RmdRequired:                         usd(10000),
							RmdActuallyDistributed:              usd(4000),
							RmdShortfallWaiverRequested:         usd(1000),
							RmdShortfallTimelyCorrected:         usd(2000),
						},
					},
				},
			},
			Deductions: &returnsv1.Deductions{
				CasualtyLosses: []*returnsv1.CasualtyLoss{
					{
						Description:            "FEMA Hurricane Damage",
						CostBasis:              usd(25000),
						FmvBefore:              usd(40000),
						FmvAfter:               usd(10000),
						InsuranceReimbursement: usd(14500),
						QualifiedDisasterLoss:  boolPtr(true),
					},
				},
			},
			Credits: &returnsv1.Credits{
				ForeignTaxes: []*returnsv1.ForeignTaxItem{
					{
						Basket:                    returnsv1.ForeignIncomeBasket_FOREIGN_INCOME_BASKET_PASSIVE,
						Country:                   "GB",
						GrossForeignIncome:        usd(10000),
						DefinitelyRelatedExpenses: usd(1000),
						ForeignTaxesPaid:          usd(500),
					},
					{
						Basket:                    returnsv1.ForeignIncomeBasket_FOREIGN_INCOME_BASKET_GENERAL,
						Country:                   "DE",
						GrossForeignIncome:        usd(15000),
						DefinitelyRelatedExpenses: usd(2000),
						ForeignTaxesPaid:          usd(900),
					},
				},
			},
			PriorYear: &returnsv1.PriorYear{
				ForeignTaxCreditCarryovers: []*returnsv1.ForeignTaxCreditCarryover{
					{
						OriginTaxYear:          2023,
						GeneralBasketCarryover: usd(300),
					},
					{
						OriginTaxYear:          2012, // Expired (> 10 years before 2025)
						GeneralBasketCarryover: usd(5000),
					},
				},
			},
		},
	}

	fed1, err := eng.PrepareFederal(inputs1)
	if err != nil {
		t.Fatalf("PrepareFederal(inputs1) failed: %v", err)
	}
	wp1 := fed1.GetWorkpapers()

	// Form 2555:
	// - Base housing = 16% of $130,000 = $20,800; housing exclusion = $30,800 - $20,800 = $10,000
	// - FEIE exclusion = $130,000; total exclusion = $140,000
	f2555 := wp1.GetForm_2555()
	assertMoneyDollars(t, "Form2555.HousingExclusion", f2555.GetForeignHousingExclusion(), 10000)
	assertMoneyDollars(t, "Form2555.FEIEExclusion", f2555.GetForeignEarnedIncomeExclusion(), 130000)
	assertMoneyDollars(t, "Form2555.TotalExclusion", f2555.GetTotalExclusion(), 140000)
	if f2555.GetStackedTax().GetMinor() <= 0 {
		t.Errorf("Form2555.StackedTax = %d, expected > 0 under § 911(f)", f2555.GetStackedTax().GetMinor())
	}

	// Schedule F: $90,000 gross - $40,000 expenses = $50,000 net farm profit (flows to Schedule 1 Line 6 and Schedule SE Line 1a)
	sf := wp1.GetScheduleF()
	assertMoneyDollars(t, "ScheduleF.GrossFarmIncome", sf.GetGrossFarmIncome(), 90000)
	assertMoneyDollars(t, "ScheduleF.TotalFarmExpenses", sf.GetTotalFarmExpenses(), 40000)
	assertMoneyDollars(t, "ScheduleF.NetFarmProfitOrLoss", sf.GetNetFarmProfitOrLoss(), 50000)
	assertMoneyDollars(t, "Schedule1.FarmIncomeOrLoss", fed1.GetForms().GetSchedule_1().GetFarmIncomeOrLoss(), 50000)
	assertMoneyDollars(t, "ScheduleSE.NetFarmProfitOrLoss", fed1.GetForms().GetScheduleSe()[0].GetNetFarmProfitOrLoss(), 50000)

	// Form 8814: $5,000 total child income -> $135 child bracket tax, $460 qual div, $598 cap gain dist, $1,242 ordinary income
	f8814 := wp1.GetForm_8814()
	assertMoneyDollars(t, "Form8814.ChildBracketTax", f8814.GetChildBracketTax(), 135)
	assertMoneyDollars(t, "Form8814.IncludedQualifiedDividends", f8814.GetIncludedQualifiedDividends(), 460)
	assertMoneyDollars(t, "Form8814.IncludedCapitalGainDistributions", f8814.GetIncludedCapitalGainDistributions(), 598)
	assertMoneyDollars(t, "Form8814.IncludedOrdinaryIncome", f8814.GetIncludedOrdinaryIncome(), 1242)

	// Form 4684: $25,000 basis - $14,500 insurance - $500 disaster floor = $10,000 net qualified disaster loss
	// Increases Single standard deduction from $15,750 to $25,750
	f4684 := wp1.GetForm_4684()
	assertMoneyDollars(t, "Form4684.NetQualifiedDisasterLoss", f4684.GetNetQualifiedDisasterLoss(), 10000)
	assertMoneyDollars(t, "StandardDeduction.EffectiveWithDisasterLoss", wp1.GetStandardDeduction().GetEffectiveStandardDeduction(), 25750)

	// Schedule H: $1,530 SS/Medicare + $500 withholding + $42 FUTA = $2,072 total household employment taxes
	sh := wp1.GetScheduleH()
	assertMoneyDollars(t, "ScheduleH.SSAndMedicare", sh.GetSocialSecurityAndMedicareTax(), 1530)
	assertMoneyDollars(t, "ScheduleH.FUTA", sh.GetFutaTax(), 42)
	assertMoneyDollars(t, "ScheduleH.Total", sh.GetTotalHouseholdEmploymentTax(), 2072)
	assertMoneyDollars(t, "Schedule2.HouseholdEmploymentTaxes", fed1.GetForms().GetSchedule_2().GetHouseholdEmploymentTaxes(), 2072)

	// Form 5329: Part II ($200) + Part III ($180) + Part IV ($60) + Parts V-VIII ($30) + Part IX ($950) = $1,420
	f5329 := wp1.GetForm_5329()
	assertMoneyDollars(t, "Form5329.Part2EducationABLE", f5329.GetPart_2EducationAbleDistributionTax(), 200)
	assertMoneyDollars(t, "Form5329.Part3ExcessTradIRA", f5329.GetPart_3ExcessTraditionalIraTax(), 180)
	assertMoneyDollars(t, "Form5329.Part4ExcessRothIRA", f5329.GetPart_4ExcessRothIraTax(), 60)
	assertMoneyDollars(t, "Form5329.Parts5To8OtherExcess", f5329.GetParts_5To_8OtherExcessContributionTax(), 30)
	assertMoneyDollars(t, "Form5329.Part9RMDShortfall", f5329.GetPart_9RmdShortfallTax(), 950)
	assertMoneyDollars(t, "Form5329.TotalTax", f5329.GetTotalForm_5329Tax(), 1420)

	// Form 1116: Multi-basket FTC (passive $500 + general $900 + valid 2023 carryover $300 = $1,700 total FTC; expired 2012 carryover ignored)
	f1116 := wp1.GetForm_1116()
	assertMoneyDollars(t, "Form1116.PassiveCredit", f1116.GetPassiveBasketCredit(), 500)
	assertMoneyDollars(t, "Form1116.GeneralCredit", f1116.GetGeneralBasketCredit(), 1200)
	assertMoneyDollars(t, "Form1116.TotalFTC", f1116.GetTotalForeignTaxCredit(), 1700)

	// Sub-scenario 2: Schedule J 3-Year Farm Income Averaging, Pub. 974 Iterative SEHI-PTC, and Form 2210 + Schedule AI
	inputs2 := &returnsv1.ReturnInputs{
		SchemaVersion: 1,
		TaxYear:       2025,
		Federal: &returnsv1.Federal1040Inputs{
			Household: &returnsv1.Household{
				MaritalStatus: returnsv1.MaritalStatus_MARITAL_STATUS_NEVER_MARRIED,
				Taxpayer: &returnsv1.Person{
					Id:          "farmer",
					FirstName:   "Grant",
					LastName:    "Miller",
					DateOfBirth: date(1978, 3, 15),
					Tin: &returnsv1.TaxpayerID{
						Kind:               returnsv1.TaxpayerIDKind_TAXPAYER_ID_KIND_SSN,
						ValidForEmployment: boolPtr(true),
					},
				},
			},
			Farms: []*returnsv1.FarmBusiness{
				{
					Id:                     "farm-main",
					Owner:                  "farmer",
					PrincipalProduct:       "Miller Grain",
					RaisedProductsSales:    usd(95000),
					OperatingExpenses:      usd(35000),
					MateriallyParticipated: boolPtr(true),
				},
			},
			Health: &returnsv1.Health{
				SelfEmployedHealthInsurance: []*returnsv1.SelfEmployedHealthInsurance{
					{
						Person:                  "farmer",
						HealthAndDentalPremiums: usd(7200),
					},
				},
			},
			Documents: &returnsv1.Documents{
				MarketplaceCoverage: []*returnsv1.Form1095ADocument{
					{
						Id:     "1095a-1",
						Person: "farmer",
						Form: &form1095a.Form1095A{
							TotalMonthlyEnrollmentPremiums:         usd(7200),
							TotalSecondLowestCostSilverPlanPremium: usd(7200),
							TotalAdvancePaymentOfPremiumTaxCredit:  usd(3600),
						},
					},
				},
			},
			PriorYear: &returnsv1.PriorYear{
				PriorYearAgi:      usd(40000),
				PriorYearTotalTax: usd(8000),
				ScheduleJHistory: &returnsv1.ScheduleJPriorYears{
					ElectedFarmIncome:        usd(30000),
					YearMinus_3FilingStatus:  "SINGLE",
					YearMinus_3TaxableIncome: usd(10000),
					YearMinus_2FilingStatus:  "SINGLE",
					YearMinus_2TaxableIncome: usd(10000),
					YearMinus_1FilingStatus:  "SINGLE",
					YearMinus_1TaxableIncome: usd(10000),
				},
			},
			Payments: &returnsv1.Payments{
				AnnualizedIncome: &returnsv1.AnnualizedIncomePeriods{
					Period_1Agi: usd(2000),
					Period_2Agi: usd(5000),
					Period_3Agi: usd(10000),
				},
			},
		},
	}

	fed2, err := eng.PrepareFederal(inputs2)
	if err != nil {
		t.Fatalf("PrepareFederal(inputs2) failed: %v", err)
	}
	wp2 := fed2.GetWorkpapers()

	// Pub. 974 Iterative SEHI <-> PTC:
	// - Total premium = $7,200, SLCSP = $7,200, APTC = $3,600
	// - Iterative SEHI deduction is reduced by total PTC so SEHI + PTC = $7,200
	sehiDed := fed2.GetForms().GetSchedule_1().GetSelfEmployedHealthInsurance().GetMinor()
	if sehiDed <= 0 || sehiDed >= 720000 {
		t.Errorf("Schedule1.SelfEmployedHealthInsurance = %d cents, expected between $0 and $7,200 after Pub. 974 PTC iteration", sehiDed)
	}

	// Schedule J: $30,000 elected farm income -> $10,000 (1/3) added to each of the 3 prior years
	sj := wp2.GetScheduleJ()
	assertMoneyDollars(t, "ScheduleJ.OneThirdElectedFarmIncome", sj.GetOneThirdElectedFarmIncome(), 10000)
	if sj.GetAveragedTax().GetMinor() <= 0 {
		t.Errorf("ScheduleJ.AveragedTax = %d, expected > 0", sj.GetAveragedTax().GetMinor())
	}

	// Form 2210 + Schedule AI:
	// Back-loaded Q4 income means Annualized Income Installment Method penalty is lower than Regular Method penalty
	f2210 := wp2.GetForm_2210()
	if f2210.GetRegularMethodPenalty().GetMinor() <= f2210.GetAnnualizedIncomeMethodPenalty().GetMinor() {
		t.Errorf("Form2210: expected RegularMethodPenalty (%d) > AnnualizedIncomeMethodPenalty (%d)",
			f2210.GetRegularMethodPenalty().GetMinor(), f2210.GetAnnualizedIncomeMethodPenalty().GetMinor())
	}
	if fed2.GetForms().GetForm_1040().GetEstimatedTaxPenalty().GetMinor() != f2210.GetNetEstimatedTaxPenalty().GetMinor() {
		t.Errorf("Form1040.EstimatedTaxPenalty (%d) != Form2210.NetEstimatedTaxPenalty (%d)",
			fed2.GetForms().GetForm_1040().GetEstimatedTaxPenalty().GetMinor(), f2210.GetNetEstimatedTaxPenalty().GetMinor())
	}

	// Sub-scenario 3: Form 8615 Kiddie Tax
	inputs3 := &returnsv1.ReturnInputs{
		SchemaVersion: 1,
		TaxYear:       2025,
		Federal: &returnsv1.Federal1040Inputs{
			Household: &returnsv1.Household{
				MaritalStatus: returnsv1.MaritalStatus_MARITAL_STATUS_NEVER_MARRIED,
				Taxpayer: &returnsv1.Person{
					Id:                      "kid",
					FirstName:               "Leo",
					LastName:                "Vance",
					DateOfBirth:             date(2010, 8, 1),
					CanBeClaimedAsDependent: boolPtr(true),
					Tin: &returnsv1.TaxpayerID{
						Kind:               returnsv1.TaxpayerIDKind_TAXPAYER_ID_KIND_SSN,
						ValidForEmployment: boolPtr(true),
					},
				},
				KiddieTax: &returnsv1.KiddieTaxParentInfo{
					ParentName:          "Elena Vance",
					ParentFilingStatus:  "SINGLE",
					ParentTaxableIncome: usd(250000), // 35% marginal bracket
				},
			},
			Documents: &returnsv1.Documents{
				Interest: []*returnsv1.Form1099INTDocument{
					{
						Id:     "int-kid",
						Person: "kid",
						Form: &form1099int.Form1099INT{
							InterestIncome: usd(12700),
						},
					},
				},
			},
		},
	}

	fed3, err := eng.PrepareFederal(inputs3)
	if err != nil {
		t.Fatalf("PrepareFederal(inputs3) failed: %v", err)
	}
	f8615 := fed3.GetWorkpapers().GetForm_8615()
	// Unearned income = $12,700; threshold = $2,700 -> net unearned income = $10,000
	// From $250,000 to $260,000 Single (2025 35% threshold = $250,525):
	// $525 * 32% ($168) + $9,475 * 35% ($3,316.25) = $3,484 parental rate tax
	// + child's 10% rate on remaining $1,350 taxable income ($135) = $3,619 total Kiddie Tax
	assertMoneyDollars(t, "Form8615.ChildUnearnedIncome", f8615.GetChildUnearnedIncome(), 12700)
	assertMoneyDollars(t, "Form8615.NetUnearnedIncome", f8615.GetNetUnearnedIncome(), 10000)
	assertMoneyDollars(t, "Form8615.ParentalRateTaxOnUnearned", f8615.GetParentalRateTaxOnUnearned(), 3484)
	assertMoneyDollars(t, "Form8615.TotalKiddieTax", f8615.GetTotalKiddieTax(), 3619)
	assertMoneyDollars(t, "Form1040.TaxWithKiddieTax", fed3.GetForms().GetForm_1040().GetTax(), 3619)
}
