package business_test

import (
	"testing"
	"time"

	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	"github.com/nexteggapp/taxcalcs/taxyear/model/business"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
	form1099k "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_k"
	form1099misc "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_misc"
	form1099nec "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_nec"
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

func TestExpandLedgerEntries(t *testing.T) {
	ctx := contextAt(t, 2026, time.June, 30)

	m := &scenariosv1.BusinessModel{
		Id: "biz-1",
		Parts: []*scenariosv1.BusinessPart{
			{
				Id: "p-ledger",
				Part: &scenariosv1.BusinessPart_FromLedger_{
					FromLedger: &scenariosv1.BusinessPart_FromLedger{
						Entries: []*scenariosv1.BusinessPart_FromLedger_Entry{
							{
								Id:     "e-1",
								Date:   date(2026, time.February, 10),
								Kind:   scenariosv1.BusinessEntryKind_BUSINESS_ENTRY_KIND_RECEIPT,
								Amount: usd(5000),
								Client: "ClientA",
							},
							{
								Id:     "e-2",
								Date:   date(2026, time.March, 15),
								Kind:   scenariosv1.BusinessEntryKind_BUSINESS_ENTRY_KIND_FEE,
								Amount: usd(150),
								Client: "ClientA",
							},
							{
								Id:     "e-3",
								Date:   date(2026, time.April, 20),
								Kind:   scenariosv1.BusinessEntryKind_BUSINESS_ENTRY_KIND_EXPENSE,
								Amount: usd(800),
							},
							{
								Id:     "e-4-depr",
								Date:   date(2026, time.May, 1),
								Kind:   scenariosv1.BusinessEntryKind_BUSINESS_ENTRY_KIND_EXPENSE,
								Line:   scenariosv1.ScheduleCExpense_SCHEDULE_C_EXPENSE_DEPRECIATION,
								Amount: usd(2000),
							},
						},
					},
				},
			},
		},
	}

	proj, _, err := business.Expand(ctx, m)
	if err != nil {
		t.Fatalf("Expand failed: %v", err)
	}

	if len(proj.GetItems()) != 4 {
		t.Fatalf("Got %d items, want 4", len(proj.GetItems()))
	}

	// Verify soFar figures: Receipts 5000, Fees 150, Expenses 2800 (800+2000)
	if got := proj.GetSoFar().GetBoxes().GetGrossReceipts().GetMinor(); got != 500000 {
		t.Errorf("GrossReceipts = %d, want 500000", got)
	}
	if got := proj.GetSoFar().GetBoxes().GetReturnsAndAllowances().GetMinor(); got != 15000 {
		t.Errorf("ReturnsAndAllowances = %d, want 15000", got)
	}
	if got := proj.GetSoFar().GetBoxes().GetTotalExpenses().GetMinor(); got != 280000 {
		t.Errorf("TotalExpenses = %d, want 280000", got)
	}
	// NetProfit = 5000 - 150 - 2800 = 2050
	if got := proj.GetSoFar().GetBoxes().GetNetProfit().GetMinor(); got != 205000 {
		t.Errorf("NetProfit = %d, want 205000", got)
	}
}

func TestExpandFormsAndReconciliation(t *testing.T) {
	ctx := contextAt(t, 2026, time.December, 31)

	m := &scenariosv1.BusinessModel{
		Id: "biz-forms",
		Parts: []*scenariosv1.BusinessPart{
			{
				Id: "p-ledger",
				Part: &scenariosv1.BusinessPart_FromLedger_{
					FromLedger: &scenariosv1.BusinessPart_FromLedger{
						Entries: []*scenariosv1.BusinessPart_FromLedger_Entry{
							{
								Id:     "e-nec",
								Date:   date(2026, time.March, 1),
								Kind:   scenariosv1.BusinessEntryKind_BUSINESS_ENTRY_KIND_RECEIPT,
								Amount: usd(8000),
								Client: "ClientNEC",
							},
							{
								Id:     "e-misc",
								Date:   date(2026, time.April, 1),
								Kind:   scenariosv1.BusinessEntryKind_BUSINESS_ENTRY_KIND_RECEIPT,
								Amount: usd(3000),
								Client: "PayerMISC",
							},
						},
					},
				},
			},
			{
				Id: "p-forms",
				Part: &scenariosv1.BusinessPart_FromForms_{
					FromForms: &scenariosv1.BusinessPart_FromForms{
						Forms: []*scenariosv1.BusinessPart_FromForms_Form{
							{
								Id:     "f-nec",
								Source: docRef("doc-nec"),
								Client: "ClientNEC",
								Form: &scenariosv1.BusinessPart_FromForms_Form_Form_1099Nec{
									Form_1099Nec: &form1099nec.Form1099NEC{
										NonemployeeCompensation: usd(10000), // 10000 vs 8000 in ledger -> adjustment +2000
									},
								},
							},
							{
								Id:     "f-misc",
								Source: docRef("doc-misc"),
								Form: &scenariosv1.BusinessPart_FromForms_Form_Form_1099Misc{
									Form_1099Misc: &form1099misc.Form1099MISC{
										Payer: &commonv1.Party{
											Name: "PayerMISC",
										},
										Rents:       usd(2000),
										Royalties:   usd(1500),
										OtherIncome: usd(500),
										// Total stated = 4000 vs 3000 in ledger -> adjustment +1000
									},
								},
							},
							{
								Id:     "f-k",
								Source: docRef("doc-k"),
								Client: "PlatformK",
								Form: &scenariosv1.BusinessPart_FromForms_Form_Form_1099K{
									Form_1099K: &form1099k.Form1099K{
										GrossPaymentTransactions: usd(15000), // No ledger entries -> adjustment +15000
									},
								},
							},
						},
					},
				},
			},
		},
	}

	proj, _, err := business.Expand(ctx, m)
	if err != nil {
		t.Fatalf("Expand failed: %v", err)
	}

	// Stated:
	// ClientNEC: 10,000 (8,000 ledger + 2,000 adjustment)
	// PayerMISC: 4,000 (3,000 ledger + 1,000 adjustment)
	// PlatformK: 15,000 (0 ledger + 15,000 adjustment)
	// Total gross receipts = 29,000
	if got := proj.GetYear().GetBoxes().GetGrossReceipts().GetMinor(); got != 2900000 {
		t.Errorf("Year GrossReceipts = %d, want 2900000", got)
	}

	// Check that adjustments were added
	var adjustments int
	for _, it := range proj.GetItems() {
		if it.GetKind() == scenariosv1.ItemKind_ITEM_KIND_ADJUSTMENT {
			adjustments++
		}
	}
	if adjustments != 3 {
		t.Errorf("Expected 3 adjustments, got %d", adjustments)
	}
}

func TestExpandPaceAndExpectedDocuments(t *testing.T) {
	// Evidence through March 31
	ctx := contextAt(t, 2026, time.March, 31)

	m := &scenariosv1.BusinessModel{
		Id: "biz-pace",
		Clients: []*scenariosv1.BusinessModel_Client{
			{
				Name: "ClientA",
				Form: scenariosv1.ClientForm_CLIENT_FORM_1099_NEC,
			},
			{
				Name: "PlatformB",
				Form: scenariosv1.ClientForm_CLIENT_FORM_1099_K,
			},
		},
		DefaultPart: &scenariosv1.BusinessDefault{
			Id: "p-default",
			Rest: &scenariosv1.BusinessDefault_PaceSoFar_{
				PaceSoFar: &scenariosv1.BusinessDefault_PaceSoFar{},
			},
		},
		Parts: []*scenariosv1.BusinessPart{
			{
				Id: "p-ledger",
				Part: &scenariosv1.BusinessPart_FromLedger_{
					FromLedger: &scenariosv1.BusinessPart_FromLedger{
						Entries: []*scenariosv1.BusinessPart_FromLedger_Entry{
							{
								Id:     "e-jan",
								Date:   date(2026, time.January, 15),
								Kind:   scenariosv1.BusinessEntryKind_BUSINESS_ENTRY_KIND_RECEIPT,
								Amount: usd(3000),
							},
							{
								Id:     "e-feb",
								Date:   date(2026, time.February, 15),
								Kind:   scenariosv1.BusinessEntryKind_BUSINESS_ENTRY_KIND_RECEIPT,
								Amount: usd(3000),
							},
							{
								Id:     "e-mar",
								Date:   date(2026, time.March, 15),
								Kind:   scenariosv1.BusinessEntryKind_BUSINESS_ENTRY_KIND_RECEIPT,
								Amount: usd(3000),
							},
						},
					},
				},
			},
		},
	}

	proj, _, err := business.Expand(ctx, m)
	if err != nil {
		t.Fatalf("Expand failed: %v", err)
	}

	// Should expect 1099-NEC and 1099-K documents
	if len(proj.GetExpected()) != 2 {
		t.Fatalf("Expected 2 expected documents, got %d", len(proj.GetExpected()))
	}
	expectedForms := map[string]bool{}
	for _, exp := range proj.GetExpected() {
		expectedForms[exp.GetForm()] = true
	}
	if !expectedForms["Form1099Nec"] || !expectedForms["Form1099K"] {
		t.Errorf("Expected Form1099Nec and Form1099K in Expected, got %v", expectedForms)
	}

	// Total receipts made so far: 9,000 across 3 months -> average 3,000/month
	// Remaining 9 months expected at 3,000/month -> 27,000 rest
	// Year total: 36,000
	if got := proj.GetSoFar().GetBoxes().GetGrossReceipts().GetMinor(); got != 900000 {
		t.Errorf("SoFar GrossReceipts = %d, want 900000", got)
	}
	if got := proj.GetYear().GetBoxes().GetGrossReceipts().GetMinor(); got != 3600000 {
		t.Errorf("Year GrossReceipts = %d, want 3600000", got)
	}
}
