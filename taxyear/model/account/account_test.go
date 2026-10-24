package account_test

import (
	"testing"
	"time"

	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	"github.com/nexteggapp/taxcalcs/taxyear/model/account"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
	form1099div "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_div"
	form1099int "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_int"
	form1099oid "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_oid"
)

func usd(dollars int64) *commonv1.Money {
	return &commonv1.Money{Minor: dollars * 100, Currency: "USD"}
}

func pct(p int64) *commonv1.Quantity {
	return &commonv1.Quantity{Units: p, Scale: 0}
}

func date(year int, month time.Month, day int) *commonv1.Date {
	return &commonv1.Date{
		Year:  int32(year),
		Month: int32(month),
		Day:   int32(day),
	}
}

func docRef(id string) *factsv1.DocumentRef {
	return &factsv1.DocumentRef{VaultRef: id, Name: id}
}

func TestExpandAccountForms(t *testing.T) {
	ctx, err := model.NewContext(2026, date(2026, time.December, 31), nil)
	if err != nil {
		t.Fatalf("NewContext failed: %v", err)
	}

	for _, tc := range []struct {
		name                  string
		forms                 []*scenariosv1.AccountPart_FromForms_Form
		wantInterest          int64
		wantTreasury          int64
		wantExempt            int64
		wantForeign           int64
		wantOrdinary          int64
		wantQualified         int64
		wantGainDistributions int64
		wantSection199A       int64
	}{
		{
			name: "Form 1099-INT alone",
			forms: []*scenariosv1.AccountPart_FromForms_Form{{
				Id:     "f-int",
				Source: docRef("doc-int"),
				Form: &scenariosv1.AccountPart_FromForms_Form_Form_1099Int{
					Form_1099Int: &form1099int.Form1099INT{
						InterestIncome:                  usd(1200),
						SavingsBondAndTreasuryInterest: usd(400),
						TaxExemptInterest:              usd(250),
						ForeignTaxPaid:                 usd(30),
					},
				},
			}},
			wantInterest: 1200_00,
			wantTreasury: 400_00,
			wantExempt:   250_00,
			wantForeign:  30_00,
		},
		{
			name: "Form 1099-DIV alone",
			forms: []*scenariosv1.AccountPart_FromForms_Form{{
				Id:     "f-div",
				Source: docRef("doc-div"),
				Form: &scenariosv1.AccountPart_FromForms_Form_Form_1099Div{
					Form_1099Div: &form1099div.Form1099DIV{
						OrdinaryDividends:            usd(2500),
						QualifiedDividends:           usd(1800),
						TotalCapitalGainDistribution: usd(600),
						Section_199ADividends:        usd(150),
						ExemptInterestDividends:      usd(100),
						ForeignTaxPaid:               usd(70),
					},
				},
			}},
			wantOrdinary:          2500_00,
			wantQualified:         1800_00,
			wantGainDistributions: 600_00,
			wantSection199A:       150_00,
			wantExempt:            100_00,
			wantForeign:           70_00,
		},
		{
			name: "Form 1099-OID nets acquisition and bond premium against OID, periodic interest, and market discount",
			forms: []*scenariosv1.AccountPart_FromForms_Form{{
				Id:     "f-oid",
				Source: docRef("doc-oid"),
				Form: &scenariosv1.AccountPart_FromForms_Form_Form_1099Oid{
					Form_1099Oid: &form1099oid.Form1099OID{
						OriginalIssueDiscount:                      usd(1000),
						OtherPeriodicInterest:                      usd(200),
						MarketDiscount:                             usd(150),
						AcquisitionPremium:                         usd(50),
						BondPremium:                                usd(100),
						OriginalIssueDiscountOnTreasuryObligations: usd(300),
						TaxExemptOid:                               usd(450),
					},
				},
			}},
			wantInterest: 1200_00, // 1000 + 200 + 150 - 50 - 100
			wantTreasury: 300_00,
			wantExempt:   450_00,
		},
		{
			name: "Form 1099-OID where bond premium exceeds OID floors taxable interest at zero",
			forms: []*scenariosv1.AccountPart_FromForms_Form{{
				Id:     "f-oid-neg",
				Source: docRef("doc-oid-neg"),
				Form: &scenariosv1.AccountPart_FromForms_Form_Form_1099Oid{
					Form_1099Oid: &form1099oid.Form1099OID{
						OriginalIssueDiscount:                      usd(200),
						BondPremium:                                usd(350),
						OriginalIssueDiscountOnTreasuryObligations: usd(100),
					},
				},
			}},
			wantInterest: 0,
			wantTreasury: 100_00,
		},
		{
			name: "Form 1099-INT, 1099-DIV, and 1099-OID combined with corrected form replacement",
			forms: []*scenariosv1.AccountPart_FromForms_Form{
				{
					Id:     "f-int-old",
					Source: docRef("doc-int-old"),
					Form: &scenariosv1.AccountPart_FromForms_Form_Form_1099Int{
						Form_1099Int: &form1099int.Form1099INT{
							InterestIncome: usd(9999), // Replaced by f-int-new, so counts for nothing
						},
					},
				},
				{
					Id:       "f-int-new",
					Source:   docRef("doc-int-new"),
					Replaces: []*factsv1.DocumentRef{docRef("doc-int-old")},
					Form: &scenariosv1.AccountPart_FromForms_Form_Form_1099Int{
						Form_1099Int: &form1099int.Form1099INT{
							InterestIncome:    usd(500),
							TaxExemptInterest: usd(100),
							ForeignTaxPaid:    usd(20),
						},
					},
				},
				{
					Id:     "f-div",
					Source: docRef("doc-div"),
					Form: &scenariosv1.AccountPart_FromForms_Form_Form_1099Div{
						Form_1099Div: &form1099div.Form1099DIV{
							OrdinaryDividends:       usd(1000),
							QualifiedDividends:      usd(800),
							ExemptInterestDividends: usd(50),
							ForeignTaxPaid:          usd(30),
						},
					},
				},
				{
					Id:     "f-oid",
					Source: docRef("doc-oid"),
					Form: &scenariosv1.AccountPart_FromForms_Form_Form_1099Oid{
						Form_1099Oid: &form1099oid.Form1099OID{
							OriginalIssueDiscount: usd(300),
							TaxExemptOid:          usd(50),
						},
					},
				},
			},
			wantInterest:  800_00, // 500 INT + 300 OID
			wantExempt:    200_00, // 100 INT + 50 DIV + 50 OID
			wantForeign:   50_00,  // 20 INT + 30 DIV
			wantOrdinary:  1000_00,
			wantQualified: 800_00,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &scenariosv1.AccountModel{
				Id:   "acct",
				Name: "Brokerage",
				Parts: []*scenariosv1.AccountPart{{
					Id: "p-forms",
					Part: &scenariosv1.AccountPart_FromForms_{
						FromForms: &scenariosv1.AccountPart_FromForms{
							Forms: tc.forms,
						},
					},
				}},
			}

			proj, _, err := account.Expand(ctx, m)
			if err != nil {
				t.Fatalf("Expand failed: %v", err)
			}
			boxes := proj.GetYear().GetBoxes()
			if got := boxes.GetInterest().GetMinor(); got != tc.wantInterest {
				t.Errorf("Interest = %d, want %d", got, tc.wantInterest)
			}
			if got := boxes.GetTreasuryInterest().GetMinor(); got != tc.wantTreasury {
				t.Errorf("TreasuryInterest = %d, want %d", got, tc.wantTreasury)
			}
			if got := boxes.GetTaxExemptInterest().GetMinor(); got != tc.wantExempt {
				t.Errorf("TaxExemptInterest = %d, want %d", got, tc.wantExempt)
			}
			if got := boxes.GetForeignTaxPaid().GetMinor(); got != tc.wantForeign {
				t.Errorf("ForeignTaxPaid = %d, want %d", got, tc.wantForeign)
			}
			if got := boxes.GetOrdinaryDividends().GetMinor(); got != tc.wantOrdinary {
				t.Errorf("OrdinaryDividends = %d, want %d", got, tc.wantOrdinary)
			}
			if got := boxes.GetQualifiedDividends().GetMinor(); got != tc.wantQualified {
				t.Errorf("QualifiedDividends = %d, want %d", got, tc.wantQualified)
			}
			if got := boxes.GetCapitalGainDistributions().GetMinor(); got != tc.wantGainDistributions {
				t.Errorf("CapitalGainDistributions = %d, want %d", got, tc.wantGainDistributions)
			}
			if got := boxes.GetSection_199ADividends().GetMinor(); got != tc.wantSection199A {
				t.Errorf("Section199A = %d, want %d", got, tc.wantSection199A)
			}
			if len(proj.GetExpected()) != 0 {
				t.Errorf("Expected 0 expected documents when forms are present, got %d", len(proj.GetExpected()))
			}
		})
	}
}

func TestExpandCreditsAndProjections(t *testing.T) {
	for _, tc := range []struct {
		name               string
		asOf               *commonv1.Date
		model              *scenariosv1.AccountModel
		wantYearInterest   int64
		wantYearTreasury   int64
		wantYearOrdinary   int64
		wantYearQualified  int64
		wantYearGains      int64
		wantYearExempt     int64
		wantYearForeign    int64
		wantAdjustmentInt  int64
		wantExpectedForms  []string
	}{
		{
			name: "all credit kinds with qualified dividend share from last_year_qualified_share",
			asOf: date(2026, time.June, 30),
			model: &scenariosv1.AccountModel{
				Id:                     "acct-credits",
				Name:                   "Bank",
				LastYearQualifiedShare: pct(60),
				Parts: []*scenariosv1.AccountPart{{
					Id: "p-credits",
					Part: &scenariosv1.AccountPart_FromCredits_{
						FromCredits: &scenariosv1.AccountPart_FromCredits{
							Credits: []*scenariosv1.AccountPart_FromCredits_Credit{
								{Id: "c-1", Date: date(2026, time.January, 31), Kind: scenariosv1.CreditKind_CREDIT_KIND_INTEREST, Amount: usd(100)},
								{Id: "c-2", Date: date(2026, time.February, 28), Kind: scenariosv1.CreditKind_CREDIT_KIND_TREASURY_INTEREST, Amount: usd(50)},
								{Id: "c-3", Date: date(2026, time.March, 31), Kind: scenariosv1.CreditKind_CREDIT_KIND_DIVIDEND, Amount: usd(300)},
								{Id: "c-4", Date: date(2026, time.April, 30), Kind: scenariosv1.CreditKind_CREDIT_KIND_CAPITAL_GAIN_DISTRIBUTION, Amount: usd(150)},
								{Id: "c-5", Date: date(2026, time.May, 31), Kind: scenariosv1.CreditKind_CREDIT_KIND_TAX_EXEMPT_INTEREST, Amount: usd(80)},
								{Id: "c-6", Date: date(2026, time.June, 30), Kind: scenariosv1.CreditKind_CREDIT_KIND_FOREIGN_TAX, Amount: usd(25)},
							},
						},
					},
				}},
			},
			wantYearInterest:  100_00,
			wantYearTreasury:  50_00,
			wantYearOrdinary:  300_00,
			wantYearQualified: 180_00, // 60% of 300
			wantYearGains:     150_00,
			wantYearExempt:    80_00,
			wantYearForeign:   25_00,
			wantExpectedForms: []string{"Form1099Int", "Form1099Div"},
		},
		{
			name: "reconciling ledger credits with 1099-INT adds December 31 adjustment",
			asOf: date(2026, time.December, 31),
			model: &scenariosv1.AccountModel{
				Id:   "acct-recon",
				Name: "Savings",
				Parts: []*scenariosv1.AccountPart{
					{
						Id: "p-credits",
						Part: &scenariosv1.AccountPart_FromCredits_{
							FromCredits: &scenariosv1.AccountPart_FromCredits{
								Credits: []*scenariosv1.AccountPart_FromCredits_Credit{
									{Id: "c-1", Date: date(2026, time.March, 31), Kind: scenariosv1.CreditKind_CREDIT_KIND_INTEREST, Amount: usd(400)},
								},
							},
						},
					},
					{
						Id: "p-forms",
						Part: &scenariosv1.AccountPart_FromForms_{
							FromForms: &scenariosv1.AccountPart_FromForms{
								Forms: []*scenariosv1.AccountPart_FromForms_Form{{
									Id:     "f-int",
									Source: docRef("doc-int"),
									Form: &scenariosv1.AccountPart_FromForms_Form_Form_1099Int{
										Form_1099Int: &form1099int.Form1099INT{
											InterestIncome: usd(650),
										},
									},
								}},
							},
						},
					},
				},
			},
			wantYearInterest:  650_00,
			wantAdjustmentInt: 250_00,
		},
		{
			name: "AtRate part computes monthly interest on balance for remaining months",
			asOf: date(2026, time.June, 30),
			model: &scenariosv1.AccountModel{
				Id:      "acct-rate",
				Name:    "High Yield Savings",
				Balance: usd(120000),
				Parts: []*scenariosv1.AccountPart{{
					Id: "p-rate",
					Part: &scenariosv1.AccountPart_AtRate_{
						AtRate: &scenariosv1.AccountPart_AtRate{
							From: date(2026, time.July, 1),
							Rate: pct(6), // 6% of $120,000 = $7,200/yr = $600/mo for 6 months (Jul-Dec) = $3,600
						},
					},
				}},
			},
			wantYearInterest:  3600_00,
			wantExpectedForms: []string{"Form1099Int"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, err := model.NewContext(2026, tc.asOf, nil)
			if err != nil {
				t.Fatalf("NewContext failed: %v", err)
			}
			proj, _, err := account.Expand(ctx, tc.model)
			if err != nil {
				t.Fatalf("Expand failed: %v", err)
			}
			boxes := proj.GetYear().GetBoxes()
			if got := boxes.GetInterest().GetMinor(); got != tc.wantYearInterest {
				t.Errorf("Year Interest = %d, want %d", got, tc.wantYearInterest)
			}
			if got := boxes.GetTreasuryInterest().GetMinor(); got != tc.wantYearTreasury {
				t.Errorf("Year TreasuryInterest = %d, want %d", got, tc.wantYearTreasury)
			}
			if got := boxes.GetOrdinaryDividends().GetMinor(); got != tc.wantYearOrdinary {
				t.Errorf("Year OrdinaryDividends = %d, want %d", got, tc.wantYearOrdinary)
			}
			if got := boxes.GetQualifiedDividends().GetMinor(); got != tc.wantYearQualified {
				t.Errorf("Year QualifiedDividends = %d, want %d", got, tc.wantYearQualified)
			}
			if got := boxes.GetCapitalGainDistributions().GetMinor(); got != tc.wantYearGains {
				t.Errorf("Year CapitalGainDistributions = %d, want %d", got, tc.wantYearGains)
			}
			if got := boxes.GetTaxExemptInterest().GetMinor(); got != tc.wantYearExempt {
				t.Errorf("Year TaxExemptInterest = %d, want %d", got, tc.wantYearExempt)
			}
			if got := boxes.GetForeignTaxPaid().GetMinor(); got != tc.wantYearForeign {
				t.Errorf("Year ForeignTaxPaid = %d, want %d", got, tc.wantYearForeign)
			}

			var adjInt int64
			for _, it := range proj.GetItems() {
				if it.GetKind() == scenariosv1.ItemKind_ITEM_KIND_ADJUSTMENT {
					adjInt += it.GetAccount().GetInterest().GetMinor()
				}
			}
			if adjInt != tc.wantAdjustmentInt {
				t.Errorf("Adjustment interest = %d, want %d", adjInt, tc.wantAdjustmentInt)
			}

			if len(proj.GetExpected()) != len(tc.wantExpectedForms) {
				t.Fatalf("Expected len = %d, want %d", len(proj.GetExpected()), len(tc.wantExpectedForms))
			}
			for i, wantForm := range tc.wantExpectedForms {
				if got := proj.GetExpected()[i].GetForm(); got != wantForm {
					t.Errorf("Expected[%d].Form = %q, want %q", i, got, wantForm)
				}
			}
		})
	}
}
