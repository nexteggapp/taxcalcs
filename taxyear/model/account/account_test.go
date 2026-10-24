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

func TestExpandCredits(t *testing.T) {
	ctx := contextAt(t, 2026, time.June, 30)

	m := &scenariosv1.AccountModel{
		Id:   "acct-credits",
		Name: "Test Bank",
		Parts: []*scenariosv1.AccountPart{
			{
				Id: "p-credits",
				Part: &scenariosv1.AccountPart_FromCredits_{
					FromCredits: &scenariosv1.AccountPart_FromCredits{
						Credits: []*scenariosv1.AccountPart_FromCredits_Credit{
							{
								Id:     "c-1",
								Date:   date(2026, time.January, 31),
								Kind:   scenariosv1.CreditKind_CREDIT_KIND_INTEREST,
								Amount: usd(100),
							},
							{
								Id:     "c-2",
								Date:   date(2026, time.February, 28),
								Kind:   scenariosv1.CreditKind_CREDIT_KIND_TREASURY_INTEREST,
								Amount: usd(50),
							},
							{
								Id:     "c-3",
								Date:   date(2026, time.March, 31),
								Kind:   scenariosv1.CreditKind_CREDIT_KIND_DIVIDEND,
								Amount: usd(300),
							},
							{
								Id:     "c-4",
								Date:   date(2026, time.April, 30),
								Kind:   scenariosv1.CreditKind_CREDIT_KIND_CAPITAL_GAIN_DISTRIBUTION,
								Amount: usd(150),
							},
							{
								Id:     "c-5",
								Date:   date(2026, time.May, 31),
								Kind:   scenariosv1.CreditKind_CREDIT_KIND_TAX_EXEMPT_INTEREST,
								Amount: usd(80),
							},
							{
								Id:     "c-6",
								Date:   date(2026, time.June, 30),
								Kind:   scenariosv1.CreditKind_CREDIT_KIND_FOREIGN_TAX,
								Amount: usd(25),
							},
						},
					},
				},
			},
		},
	}

	proj, _, err := account.Expand(ctx, m)
	if err != nil {
		t.Fatalf("Expand failed: %v", err)
	}

	if len(proj.GetItems()) != 6 {
		t.Fatalf("Got %d items, want 6", len(proj.GetItems()))
	}

	boxes := proj.GetSoFar().GetBoxes()
	if got := boxes.GetInterest().GetMinor(); got != 10000 {
		t.Errorf("Interest = %d, want 10000", got)
	}
	if got := boxes.GetTreasuryInterest().GetMinor(); got != 5000 {
		t.Errorf("TreasuryInterest = %d, want 5000", got)
	}
	if got := boxes.GetOrdinaryDividends().GetMinor(); got != 30000 {
		t.Errorf("OrdinaryDividends = %d, want 30000", got)
	}
	if got := boxes.GetCapitalGainDistributions().GetMinor(); got != 15000 {
		t.Errorf("CapitalGainDistributions = %d, want 15000", got)
	}
	if got := boxes.GetTaxExemptInterest().GetMinor(); got != 8000 {
		t.Errorf("TaxExemptInterest = %d, want 8000", got)
	}
	if got := boxes.GetForeignTaxPaid().GetMinor(); got != 2500 {
		t.Errorf("ForeignTaxPaid = %d, want 2500", got)
	}

	// Without forms, 1099-INT and 1099-DIV should be expected
	if len(proj.GetExpected()) != 2 {
		t.Fatalf("Expected 2 expected forms, got %d", len(proj.GetExpected()))
	}
}

func TestExpandForm1099INTAndDIV(t *testing.T) {
	ctx := contextAt(t, 2026, time.December, 31)

	m := &scenariosv1.AccountModel{
		Id:   "acct-forms",
		Name: "Investment Brokerage",
		Parts: []*scenariosv1.AccountPart{
			{
				Id: "p-forms",
				Part: &scenariosv1.AccountPart_FromForms_{
					FromForms: &scenariosv1.AccountPart_FromForms{
						Forms: []*scenariosv1.AccountPart_FromForms_Form{
							{
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
							},
							{
								Id:     "f-div",
								Source: docRef("doc-div"),
								Form: &scenariosv1.AccountPart_FromForms_Form_Form_1099Div{
									Form_1099Div: &form1099div.Form1099DIV{
										OrdinaryDividends:              usd(2500),
										QualifiedDividends:             usd(1800),
										TotalCapitalGainDistribution:   usd(600),
										Section_199ADividends:          usd(150),
										ExemptInterestDividends:        usd(100),
										ForeignTaxPaid:                 usd(70),
									},
								},
							},
						},
					},
				},
			},
		},
	}

	proj, _, err := account.Expand(ctx, m)
	if err != nil {
		t.Fatalf("Expand failed: %v", err)
	}

	boxes := proj.GetYear().GetBoxes()
	if got := boxes.GetInterest().GetMinor(); got != 120000 {
		t.Errorf("Interest = %d, want 120000", got)
	}
	if got := boxes.GetTreasuryInterest().GetMinor(); got != 40000 {
		t.Errorf("TreasuryInterest = %d, want 40000", got)
	}
	// Exempt interest from INT (250) + exempt dividends from DIV (100) = 350
	if got := boxes.GetTaxExemptInterest().GetMinor(); got != 35000 {
		t.Errorf("TaxExemptInterest = %d, want 35000", got)
	}
	// Foreign tax from INT (30) + DIV (70) = 100
	if got := boxes.GetForeignTaxPaid().GetMinor(); got != 10000 {
		t.Errorf("ForeignTaxPaid = %d, want 10000", got)
	}
	if got := boxes.GetOrdinaryDividends().GetMinor(); got != 250000 {
		t.Errorf("OrdinaryDividends = %d, want 250000", got)
	}
	if got := boxes.GetQualifiedDividends().GetMinor(); got != 180000 {
		t.Errorf("QualifiedDividends = %d, want 180000", got)
	}
	if got := boxes.GetCapitalGainDistributions().GetMinor(); got != 60000 {
		t.Errorf("CapitalGainDistributions = %d, want 60000", got)
	}
	if got := boxes.GetSection_199ADividends().GetMinor(); got != 15000 {
		t.Errorf("Section199A = %d, want 15000", got)
	}

	// When forms are present, no 1099-INT or 1099-DIV should be expected
	if len(proj.GetExpected()) != 0 {
		t.Errorf("Expected 0 expected documents when forms present, got %d", len(proj.GetExpected()))
	}
}

func TestExpandForm1099OID(t *testing.T) {
	ctx := contextAt(t, 2026, time.December, 31)

	m := &scenariosv1.AccountModel{
		Id:   "acct-oid",
		Name: "Bond Portfolio",
		Parts: []*scenariosv1.AccountPart{
			{
				Id: "p-oid",
				Part: &scenariosv1.AccountPart_FromForms_{
					FromForms: &scenariosv1.AccountPart_FromForms{
						Forms: []*scenariosv1.AccountPart_FromForms_Form{
							{
								Id:     "f-oid",
								Source: docRef("doc-oid"),
								Form: &scenariosv1.AccountPart_FromForms_Form_Form_1099Oid{
									Form_1099Oid: &form1099oid.Form1099OID{
										OriginalIssueDiscount:                             usd(1000),
										OtherPeriodicInterest:                             usd(200),
										MarketDiscount:                                    usd(150),
										AcquisitionPremium:                                usd(50),
										BondPremium:                                       usd(100),
										OriginalIssueDiscountOnTreasuryObligations:        usd(300),
										TaxExemptOid:                                      usd(450),
									},
								},
							},
						},
					},
				},
			},
		},
	}

	proj, _, err := account.Expand(ctx, m)
	if err != nil {
		t.Fatalf("Expand failed: %v", err)
	}

	boxes := proj.GetYear().GetBoxes()
	// Taxable OID = 1000 + 200 + 150 - 50 - 100 = 1200
	if got := boxes.GetInterest().GetMinor(); got != 120000 {
		t.Errorf("Taxable OID Interest = %d, want 120000", got)
	}
	if got := boxes.GetTreasuryInterest().GetMinor(); got != 30000 {
		t.Errorf("Treasury OID = %d, want 30000", got)
	}
	if got := boxes.GetTaxExemptInterest().GetMinor(); got != 45000 {
		t.Errorf("TaxExempt OID = %d, want 45000", got)
	}
}

func TestReconcileCreditsWithForm(t *testing.T) {
	ctx := contextAt(t, 2026, time.December, 31)

	m := &scenariosv1.AccountModel{
		Id:   "acct-recon",
		Name: "Checking & Savings",
		Parts: []*scenariosv1.AccountPart{
			{
				Id: "p-credits",
				Part: &scenariosv1.AccountPart_FromCredits_{
					FromCredits: &scenariosv1.AccountPart_FromCredits{
						Credits: []*scenariosv1.AccountPart_FromCredits_Credit{
							{
								Id:     "c-1",
								Date:   date(2026, time.March, 31),
								Kind:   scenariosv1.CreditKind_CREDIT_KIND_INTEREST,
								Amount: usd(400),
							},
						},
					},
				},
			},
			{
				Id: "p-forms",
				Part: &scenariosv1.AccountPart_FromForms_{
					FromForms: &scenariosv1.AccountPart_FromForms{
						Forms: []*scenariosv1.AccountPart_FromForms_Form{
							{
								Id:     "f-int",
								Source: docRef("doc-int"),
								Form: &scenariosv1.AccountPart_FromForms_Form_Form_1099Int{
									Form_1099Int: &form1099int.Form1099INT{
										InterestIncome: usd(650), // 650 form vs 400 credit -> adjustment +250
									},
								},
							},
						},
					},
				},
			},
		},
	}

	proj, _, err := account.Expand(ctx, m)
	if err != nil {
		t.Fatalf("Expand failed: %v", err)
	}

	// Should have credit item + adjustment item
	var hasAdjustment bool
	for _, it := range proj.GetItems() {
		if it.GetKind() == scenariosv1.ItemKind_ITEM_KIND_ADJUSTMENT {
			hasAdjustment = true
			if got := it.GetAccount().GetInterest().GetMinor(); got != 25000 {
				t.Errorf("Adjustment interest = %d, want 25000", got)
			}
		}
	}
	if !hasAdjustment {
		t.Errorf("Expected adjustment item to reconcile credit with 1099-INT")
	}

	if got := proj.GetYear().GetBoxes().GetInterest().GetMinor(); got != 65000 {
		t.Errorf("Year Interest = %d, want 65000", got)
	}
}
