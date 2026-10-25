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

func TestExpandLedgerAndProjections(t *testing.T) {
	for _, tc := range []struct {
		name              string
		asOf              *commonv1.Date
		model             *scenariosv1.BusinessModel
		wantSoFarReceipts int64
		wantSoFarFees     int64
		wantSoFarExpenses int64
		wantYearReceipts  int64
		wantYearFees      int64
		wantYearExpenses  int64
		wantYearNetProfit int64
		wantExpectedForms []string
	}{
		{
			name: "ledger receipts, fees, expenses, and one-off depreciation without default pace",
			asOf: date(2026, time.June, 30),
			model: &scenariosv1.BusinessModel{
				Id: "biz-ledger",
				Parts: []*scenariosv1.BusinessPart{{
					Id: "p-ledger",
					Part: &scenariosv1.BusinessPart_FromLedger_{
						FromLedger: &scenariosv1.BusinessPart_FromLedger{
							Entries: []*scenariosv1.BusinessPart_FromLedger_Entry{
								{Id: "e-1", Date: date(2026, time.February, 10), Kind: scenariosv1.BusinessEntryKind_BUSINESS_ENTRY_KIND_RECEIPT, Amount: usd(5000), Client: "ClientA"},
								{Id: "e-2", Date: date(2026, time.March, 15), Kind: scenariosv1.BusinessEntryKind_BUSINESS_ENTRY_KIND_FEE, Amount: usd(150), Client: "ClientA"},
								{Id: "e-3", Date: date(2026, time.April, 20), Kind: scenariosv1.BusinessEntryKind_BUSINESS_ENTRY_KIND_EXPENSE, Amount: usd(800)},
								{Id: "e-4", Date: date(2026, time.May, 1), Kind: scenariosv1.BusinessEntryKind_BUSINESS_ENTRY_KIND_EXPENSE, Line: scenariosv1.ScheduleCExpense_SCHEDULE_C_EXPENSE_DEPRECIATION, Amount: usd(2000)},
							},
						},
					},
				}},
			},
			wantSoFarReceipts: 5000_00,
			wantSoFarFees:     150_00,
			wantSoFarExpenses: 2800_00,
			wantYearReceipts:  5000_00,
			wantYearFees:      150_00,
			wantYearExpenses:  2800_00,
			wantYearNetProfit: 2050_00,
		},
		{
			name: "pace so far repeats recurring entries and leaves out one-off depreciation",
			asOf: date(2026, time.March, 31),
			model: &scenariosv1.BusinessModel{
				Id: "biz-pace",
				Clients: []*scenariosv1.BusinessModel_Client{
					{Name: "ClientA", Form: scenariosv1.ClientForm_CLIENT_FORM_1099_NEC},
					{Name: "PlatformB", Form: scenariosv1.ClientForm_CLIENT_FORM_1099_K},
				},
				DefaultPart: &scenariosv1.BusinessDefault{
					Id: "p-default",
					Rest: &scenariosv1.BusinessDefault_PaceSoFar_{
						PaceSoFar: &scenariosv1.BusinessDefault_PaceSoFar{},
					},
				},
				Parts: []*scenariosv1.BusinessPart{{
					Id: "p-ledger",
					Part: &scenariosv1.BusinessPart_FromLedger_{
						FromLedger: &scenariosv1.BusinessPart_FromLedger{
							Entries: []*scenariosv1.BusinessPart_FromLedger_Entry{
								{Id: "e-jan", Date: date(2026, time.January, 15), Kind: scenariosv1.BusinessEntryKind_BUSINESS_ENTRY_KIND_RECEIPT, Amount: usd(3000)},
								{Id: "e-feb", Date: date(2026, time.February, 15), Kind: scenariosv1.BusinessEntryKind_BUSINESS_ENTRY_KIND_RECEIPT, Amount: usd(3000)},
								{Id: "e-mar", Date: date(2026, time.March, 15), Kind: scenariosv1.BusinessEntryKind_BUSINESS_ENTRY_KIND_RECEIPT, Amount: usd(3000)},
								{Id: "e-exp-jan", Date: date(2026, time.January, 20), Kind: scenariosv1.BusinessEntryKind_BUSINESS_ENTRY_KIND_EXPENSE, Amount: usd(300)},
								{Id: "e-exp-feb", Date: date(2026, time.February, 20), Kind: scenariosv1.BusinessEntryKind_BUSINESS_ENTRY_KIND_EXPENSE, Amount: usd(300)},
								{Id: "e-exp-mar", Date: date(2026, time.March, 20), Kind: scenariosv1.BusinessEntryKind_BUSINESS_ENTRY_KIND_EXPENSE, Amount: usd(300)},
								// One-off depreciation in March should not repeat in Apr-Dec
								{Id: "e-depr", Date: date(2026, time.March, 25), Kind: scenariosv1.BusinessEntryKind_BUSINESS_ENTRY_KIND_EXPENSE, Line: scenariosv1.ScheduleCExpense_SCHEDULE_C_EXPENSE_DEPRECIATION, Amount: usd(1800)},
							},
						},
					},
				}},
			},
			wantSoFarReceipts: 9000_00,
			wantSoFarExpenses: 2700_00,  // 900 recurring + 1800 one-off
			wantYearReceipts:  36000_00, // 9000 + 9 * 3000
			wantYearExpenses:  5400_00,  // 2700 + 9 * 300
			wantYearNetProfit: 30600_00,
			wantExpectedForms: []string{"Form1099Nec", "Form1099K"},
		},
		{
			name: "Monthly part overrides remaining months from its start date",
			asOf: date(2026, time.June, 30),
			model: &scenariosv1.BusinessModel{
				Id: "biz-monthly",
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
									{Id: "e-jun", Date: date(2026, time.June, 10), Kind: scenariosv1.BusinessEntryKind_BUSINESS_ENTRY_KIND_RECEIPT, Amount: usd(2000)},
								},
							},
						},
					},
					{
						Id: "p-monthly",
						Part: &scenariosv1.BusinessPart_Monthly_{
							Monthly: &scenariosv1.BusinessPart_Monthly{
								From:     date(2026, time.July, 1),
								Receipts: usd(4000),
								Expenses: usd(500),
							},
						},
					},
				},
			},
			wantSoFarReceipts: 2000_00,
			wantYearReceipts:  26000_00, // 2000 + 6 * 4000
			wantYearExpenses:  3000_00,  // 6 * 500
			wantYearNetProfit: 23000_00,
		},
		{
			name: "YearTotal shares remaining year amount across remaining months",
			asOf: date(2026, time.June, 30),
			model: &scenariosv1.BusinessModel{
				Id: "biz-yeartotal",
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
									{Id: "e-1", Date: date(2026, time.March, 10), Kind: scenariosv1.BusinessEntryKind_BUSINESS_ENTRY_KIND_RECEIPT, Amount: usd(10000)},
								},
							},
						},
					},
					{
						Id: "p-yt",
						Part: &scenariosv1.BusinessPart_YearTotal_{
							YearTotal: &scenariosv1.BusinessPart_YearTotal{
								Receipts: usd(40000),
								Expenses: usd(6000),
							},
						},
					},
				},
			},
			wantSoFarReceipts: 10000_00,
			wantYearReceipts:  40000_00,
			wantYearExpenses:  6000_00,
			wantYearNetProfit: 34000_00,
		},
		{
			name: "Stopped business cuts off future months after stop date",
			asOf: date(2026, time.March, 31),
			model: &scenariosv1.BusinessModel{
				Id: "biz-stopped",
				DefaultPart: &scenariosv1.BusinessDefault{
					Id: "p-default",
					Rest: &scenariosv1.BusinessDefault_PaceSoFar_{
						PaceSoFar: &scenariosv1.BusinessDefault_PaceSoFar{},
					},
				},
				Parts: []*scenariosv1.BusinessPart{
					{
						Id: "p-monthly",
						Part: &scenariosv1.BusinessPart_Monthly_{
							Monthly: &scenariosv1.BusinessPart_Monthly{
								From:     date(2026, time.April, 1),
								Receipts: usd(5000),
							},
						},
					},
					{
						Id: "p-stops",
						Part: &scenariosv1.BusinessPart_Stops_{
							Stops: &scenariosv1.BusinessPart_Stops{
								On: date(2026, time.June, 30),
							},
						},
					},
				},
			},
			wantSoFarReceipts: 0,
			wantYearReceipts:  15000_00, // Apr, May, Jun only (3 * 5000)
			wantYearNetProfit: 15000_00,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, err := model.NewContext(2026, tc.asOf, nil)
			if err != nil {
				t.Fatalf("NewContext failed: %v", err)
			}
			proj, _, err := business.Expand(ctx, tc.model)
			if err != nil {
				t.Fatalf("Expand failed: %v", err)
			}
			soFar := proj.GetSoFar().GetBoxes()
			if got := soFar.GetGrossReceipts().GetMinor(); got != tc.wantSoFarReceipts {
				t.Errorf("SoFar GrossReceipts = %d, want %d", got, tc.wantSoFarReceipts)
			}
			if got := soFar.GetReturnsAndAllowances().GetMinor(); got != tc.wantSoFarFees {
				t.Errorf("SoFar ReturnsAndAllowances = %d, want %d", got, tc.wantSoFarFees)
			}
			if got := soFar.GetTotalExpenses().GetMinor(); got != tc.wantSoFarExpenses {
				t.Errorf("SoFar TotalExpenses = %d, want %d", got, tc.wantSoFarExpenses)
			}

			year := proj.GetYear().GetBoxes()
			if got := year.GetGrossReceipts().GetMinor(); got != tc.wantYearReceipts {
				t.Errorf("Year GrossReceipts = %d, want %d", got, tc.wantYearReceipts)
			}
			if got := year.GetReturnsAndAllowances().GetMinor(); got != tc.wantYearFees {
				t.Errorf("Year ReturnsAndAllowances = %d, want %d", got, tc.wantYearFees)
			}
			if got := year.GetTotalExpenses().GetMinor(); got != tc.wantYearExpenses {
				t.Errorf("Year TotalExpenses = %d, want %d", got, tc.wantYearExpenses)
			}
			if got := year.GetNetProfit().GetMinor(); got != tc.wantYearNetProfit {
				t.Errorf("Year NetProfit = %d, want %d", got, tc.wantYearNetProfit)
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

func TestExpandFormsAndReconciliation(t *testing.T) {
	ctx, err := model.NewContext(2026, date(2026, time.December, 31), nil)
	if err != nil {
		t.Fatalf("NewContext failed: %v", err)
	}

	for _, tc := range []struct {
		name            string
		entries         []*scenariosv1.BusinessPart_FromLedger_Entry
		forms           []*scenariosv1.BusinessPart_FromForms_Form
		wantReceipts    int64
		wantAdjustments int
	}{
		{
			name: "Form 1099-NEC with no ledger entries adds full adjustment",
			forms: []*scenariosv1.BusinessPart_FromForms_Form{{
				Id:     "f-nec",
				Source: docRef("doc-nec"),
				Client: "ClientNEC",
				Form: &scenariosv1.BusinessPart_FromForms_Form_Form_1099Nec{
					Form_1099Nec: &form1099nec.Form1099NEC{
						NonemployeeCompensation: usd(10000),
					},
				},
			}},
			wantReceipts:    10000_00,
			wantAdjustments: 1,
		},
		{
			name: "Form 1099-NEC matching ledger entries exactly produces zero adjustment items",
			entries: []*scenariosv1.BusinessPart_FromLedger_Entry{{
				Id:     "e-nec",
				Date:   date(2026, time.May, 1),
				Kind:   scenariosv1.BusinessEntryKind_BUSINESS_ENTRY_KIND_RECEIPT,
				Amount: usd(10000),
				Client: "ClientNEC",
			}},
			forms: []*scenariosv1.BusinessPart_FromForms_Form{{
				Id:     "f-nec",
				Source: docRef("doc-nec"),
				Client: "ClientNEC",
				Form: &scenariosv1.BusinessPart_FromForms_Form_Form_1099Nec{
					Form_1099Nec: &form1099nec.Form1099NEC{
						NonemployeeCompensation: usd(10000),
					},
				},
			}},
			wantReceipts:    10000_00,
			wantAdjustments: 0,
		},
		{
			name: "Form 1099-MISC aggregates all business receipt boxes and reconciles with ledger by payer name",
			entries: []*scenariosv1.BusinessPart_FromLedger_Entry{{
				Id:     "e-misc",
				Date:   date(2026, time.April, 1),
				Kind:   scenariosv1.BusinessEntryKind_BUSINESS_ENTRY_KIND_RECEIPT,
				Amount: usd(3000),
				Client: "PayerMISC",
			}},
			forms: []*scenariosv1.BusinessPart_FromForms_Form{{
				Id:     "f-misc",
				Source: docRef("doc-misc"),
				Form: &scenariosv1.BusinessPart_FromForms_Form_Form_1099Misc{
					Form_1099Misc: &form1099misc.Form1099MISC{
						Payer:                            &commonv1.Party{Name: "PayerMISC"},
						Rents:                            usd(1000),
						Royalties:                        usd(800),
						OtherIncome:                      usd(500),
						MedicalAndHealthCarePayments:     usd(600),
						CropInsuranceProceeds:            usd(400),
						FishingBoatProceeds:              usd(300),
						FishPurchasedForResale:           usd(200),
						GrossProceedsPaidToAttorney:      usd(700),
						NonqualifiedDeferredCompensation: usd(500),
					},
				},
			}},
			wantReceipts:    5000_00, // 3000 ledger + 2000 adjustment
			wantAdjustments: 1,
		},
		{
			name: "Form 1099-NEC, 1099-MISC, and 1099-K combined across multiple clients",
			entries: []*scenariosv1.BusinessPart_FromLedger_Entry{
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
			forms: []*scenariosv1.BusinessPart_FromForms_Form{
				{
					Id:     "f-nec",
					Source: docRef("doc-nec"),
					Client: "ClientNEC",
					Form: &scenariosv1.BusinessPart_FromForms_Form_Form_1099Nec{
						Form_1099Nec: &form1099nec.Form1099NEC{
							NonemployeeCompensation: usd(10000),
						},
					},
				},
				{
					Id:     "f-misc",
					Source: docRef("doc-misc"),
					Form: &scenariosv1.BusinessPart_FromForms_Form_Form_1099Misc{
						Form_1099Misc: &form1099misc.Form1099MISC{
							Payer:       &commonv1.Party{Name: "PayerMISC"},
							Rents:       usd(2000),
							Royalties:   usd(1500),
							OtherIncome: usd(500),
						},
					},
				},
				{
					Id:     "f-k",
					Source: docRef("doc-k"),
					Client: "PlatformK",
					Form: &scenariosv1.BusinessPart_FromForms_Form_Form_1099K{
						Form_1099K: &form1099k.Form1099K{
							GrossPaymentTransactions: usd(15000),
						},
					},
				},
			},
			wantReceipts:    29000_00, // 10000 + 4000 + 15000
			wantAdjustments: 3,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &scenariosv1.BusinessModel{
				Id: "biz",
				Parts: []*scenariosv1.BusinessPart{
					{
						Id: "p-ledger",
						Part: &scenariosv1.BusinessPart_FromLedger_{
							FromLedger: &scenariosv1.BusinessPart_FromLedger{
								Entries: tc.entries,
							},
						},
					},
					{
						Id: "p-forms",
						Part: &scenariosv1.BusinessPart_FromForms_{
							FromForms: &scenariosv1.BusinessPart_FromForms{
								Forms: tc.forms,
							},
						},
					},
				},
			}

			proj, _, err := business.Expand(ctx, m)
			if err != nil {
				t.Fatalf("Expand failed: %v", err)
			}
			if got := proj.GetYear().GetBoxes().GetGrossReceipts().GetMinor(); got != tc.wantReceipts {
				t.Errorf("Year GrossReceipts = %d, want %d", got, tc.wantReceipts)
			}
			var adjustments int
			for _, it := range proj.GetItems() {
				if it.GetKind() == scenariosv1.ItemKind_ITEM_KIND_ADJUSTMENT {
					adjustments++
				}
			}
			if adjustments != tc.wantAdjustments {
				t.Errorf("adjustments = %d, want %d", adjustments, tc.wantAdjustments)
			}
		})
	}
}
