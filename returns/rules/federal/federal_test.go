package federal_test

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	form1040 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1040"
	form1098 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1098"
	form1098e "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1098_e"
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
