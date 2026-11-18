package states_test

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
	form1040me "github.com/nexteggapp/taxprotos/go/us/state/me/v1/form_1040me"
)

// There is no Go income tax calculator for Maine to compare with. Each household below is worked by hand from the
// 2025 Form 1040ME and its instructions, line by line. The tax on line 20 is
// from the 2025 Tax Rate Schedules (instructions, page 12); where the taxable
// income falls in the booklet's tax table, the table's figure is checked too.

func meW2(wages, fedWithheld, meWithheld int64) *formw2.FormW2 {
	return &formw2.FormW2{
		WagesTipsOtherCompensation: cents(wages),
		FederalTaxWithheld:         cents(fedWithheld),
		StateTaxWithholding: []*commonv1.StateTaxWithholding{
			{State: "ME", StateIncome: cents(wages), TaxWithheld: cents(meWithheld)},
		},
	}
}

func meInputs(
	status returnsv1.FilingStatusElection,
	taxpayer, spouse *returnsv1.Person,
	dependents []*returnsv1.Dependent,
	w2s []*formw2.FormW2,
) *returnsv1.ReturnInputs {
	marital := returnsv1.MaritalStatus_MARITAL_STATUS_NEVER_MARRIED
	if status == returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY {
		marital = returnsv1.MaritalStatus_MARITAL_STATUS_MARRIED
	}
	var docs []*returnsv1.W2Document
	for i, w := range w2s {
		person := taxpayer.Id
		if i == 1 && spouse != nil {
			person = spouse.Id
		}
		docs = append(docs, &returnsv1.W2Document{Id: w2Id(i), Person: person, Form: w})
	}
	return &returnsv1.ReturnInputs{
		SchemaVersion: 1,
		TaxYear:       2025,
		Federal: &returnsv1.Federal1040Inputs{
			Household: &returnsv1.Household{
				MaritalStatus: marital,
				Taxpayer:      taxpayer,
				Spouse:        spouse,
				Dependents:    dependents,
			},
			Elections: &returnsv1.Elections{FilingStatus: status},
			Documents: &returnsv1.Documents{W2S: docs},
		},
		States: []*returnsv1.StateInputs{{
			State:  "ME",
			Inputs: &returnsv1.StateInputs_Me{Me: &returnsv1.MaineInputs{}},
		}},
	}
}

func prepareMaine(t *testing.T, in *returnsv1.ReturnInputs) (*returnsv1.MaineReturn, *form1040me.Form1040ME) {
	t.Helper()
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatal(err)
	}
	out, err := eng.Prepare(in)
	if err != nil {
		t.Fatal(err)
	}
	ret := out.GetStates()[0].GetMe()
	if ret == nil {
		t.Fatal("expected Maine return")
	}
	return ret, ret.GetForms().GetForm_1040Me()
}

// 1. Single filer, wages only.
//   - Line 14 federal AGI: $60,000 (W-2 wages).
//   - Line 16 Maine AGI: $60,000 (no modifications).
//   - Line 17: $15,000, Maine Standard Deduction Chart, single, no boxes on
//     lines 12a-12d; line 16 is not over $100,000, so no worksheet.
//   - Line 18: 1 exemption x $5,150 = $5,150 (instructions, line 13: single, 1).
//   - Line 19: $60,000 - $15,000 - $5,150 = $39,850.
//   - Line 20: Schedule #1, $1,554 + 6.75% x ($39,850 - $26,800)
//     = $1,554 + $880.875 = $2,434.875, rounded $2,435. The tax table's row
//     $39,800-$39,900, single, also gives $2,435.
//   - Lines 22, 24: $2,435.
//   - Line 25a, 25f: $2,500 withheld.
//   - Line 28 overpaid, 33, 34b refund: $2,500 - $2,435 = $65. Line 35: $0.
func TestMaineSingle(t *testing.T) {
	p := ilPerson("ann", 1990, 4, 2, false, false)
	in := meInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		p, nil, nil, []*formw2.FormW2{meW2(60000_00, 6000_00, 2500_00)})
	ret, f := prepareMaine(t, in)

	want(t, "Line 14", f.GetFederalAdjustedGrossIncome(), money.USCents(60000_00))
	want(t, "Line 16", f.GetMaineAdjustedGrossIncome(), money.USCents(60000_00))
	want(t, "Line 17", f.GetDeduction(), money.USCents(15000_00))
	want(t, "Line 18", f.GetExemption(), money.USCents(5150_00))
	want(t, "Line 19", f.GetTaxableIncome(), money.USCents(39850_00))
	want(t, "Line 20", f.GetIncomeTax(), money.USCents(2435_00))
	want(t, "Line 24", f.GetNetTax(), money.USCents(2435_00))
	want(t, "Line 25f", f.GetTotalPayments(), money.USCents(2500_00))
	want(t, "Line 34b", f.GetRefund(), money.USCents(65_00))
	want(t, "Line 35", f.GetAmountDue(), money.USCents(0))
	if ret.GetStatus() != returnsv1.ReturnStatus_RETURN_STATUS_COMPLETE {
		t.Errorf("expected complete return, got %v: %v", ret.GetStatus(), ret.GetDiagnostics())
	}
}

// 2. Married filing jointly, two earners, two children.
//   - Line 14: $90,000 + $40,000 = $130,000.
//   - Line 17: $30,000, chart, married filing jointly, no boxes; line 16 is
//     not over $200,050.
//   - Line 13: 2 exemptions (children are not exemptions on line 13; they go
//     on line 13a and to Schedule A). Line 18: 2 x $5,150 = $10,300.
//   - Line 19: $130,000 - $30,000 - $10,300 = $89,700.
//   - Line 20: Schedule #3, $3,109 + 6.75% x ($89,700 - $53,600)
//     = $3,109 + $2,436.75 = $5,545.75, rounded $5,546.
//   - Line 25a, 25f: $3,000 + $1,500 = $4,500.
//   - Line 29 underpaid, 35 amount due: $5,546 - $4,500 = $1,046.
func TestMaineMarriedFilingJointlyWithChildren(t *testing.T) {
	p1 := ilPerson("bea", 1985, 3, 15, false, false)
	p2 := ilPerson("cal", 1984, 8, 20, false, false)
	kids := []*returnsv1.Dependent{
		ilDependent("dee", 2015, 6, 1),
		ilDependent("eli", 2018, 9, 9),
	}
	in := meInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY,
		p1, p2, kids, []*formw2.FormW2{meW2(90000_00, 8000_00, 3000_00), meW2(40000_00, 3000_00, 1500_00)})
	_, f := prepareMaine(t, in)

	if got := f.GetExemptions(); got != 2 {
		t.Errorf("Line 13: got %d, want 2", got)
	}
	if got := f.GetQualifyingChildrenAndDependents(); got != 2 {
		t.Errorf("Line 13a: got %d, want 2", got)
	}
	want(t, "Line 16", f.GetMaineAdjustedGrossIncome(), money.USCents(130000_00))
	want(t, "Line 17", f.GetDeduction(), money.USCents(30000_00))
	want(t, "Line 18", f.GetExemption(), money.USCents(10300_00))
	want(t, "Line 19", f.GetTaxableIncome(), money.USCents(89700_00))
	want(t, "Line 20", f.GetIncomeTax(), money.USCents(5546_00))
	want(t, "Line 25f", f.GetTotalPayments(), money.USCents(4500_00))
	want(t, "Line 29", f.GetUnderpaid(), money.USCents(1046_00))
	want(t, "Line 34b", f.GetRefund(), money.USCents(0))
	want(t, "Line 35", f.GetAmountDue(), money.USCents(1046_00))
}

// 3. Single filer aged 70 with high income: the deduction is phased out
// entirely.
//   - Line 12a checked: 1 box. Chart: single, 1 box, $17,000.
//   - Line 16: $180,000.
//   - Deduction worksheet: line 3 $180,000 - $100,000 = $80,000; line 4
//     $75,000; line 5 $80,000 / $75,000 > 1, so 1.0000; line 6 $17,000;
//     line 7 $17,000; line 8, line 17: $0.
//   - Line 18: $180,000 is not over $333,450, so 1 x $5,150 = $5,150.
//   - Line 19: $180,000 - $0 - $5,150 = $174,850.
//   - Line 20: Schedule #1, $4,028 + 7.15% x ($174,850 - $63,450)
//     = $4,028 + $7,965.10 = $11,993.10, rounded $11,993.
//   - Line 25f: $10,000. Line 35: $1,993.
func TestMaineSingleSeniorHighIncome(t *testing.T) {
	p := ilPerson("flo", 1955, 1, 10, false, false)
	in := meInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		p, nil, nil, []*formw2.FormW2{meW2(180000_00, 30000_00, 10000_00)})
	_, f := prepareMaine(t, in)

	if got := f.GetAge_65OrOlder(); got != 1 {
		t.Errorf("Line 12a: got %d, want 1", got)
	}
	want(t, "Line 17", f.GetDeduction(), money.USCents(0))
	want(t, "Line 18", f.GetExemption(), money.USCents(5150_00))
	want(t, "Line 19", f.GetTaxableIncome(), money.USCents(174850_00))
	want(t, "Line 20", f.GetIncomeTax(), money.USCents(11993_00))
	want(t, "Line 25f", f.GetTotalPayments(), money.USCents(10000_00))
	want(t, "Line 35", f.GetAmountDue(), money.USCents(1993_00))
}

// 4. Married filing jointly in the deduction phase-out range.
//   - Line 16: $150,000 + $110,000 = $260,000.
//   - Deduction worksheet: line 3 $260,000 - $200,050 = $59,950; line 4
//     $150,000; line 5 0.39966..., entered to four places as 0.3997; line 6
//     $30,000; line 7 $30,000 x 0.3997 = $11,991; line 8, line 17: $18,009.
//   - Line 18: $260,000 is not over $400,100, so 2 x $5,150 = $10,300.
//   - Line 19: $260,000 - $18,009 - $10,300 = $231,691.
//   - Line 20: Schedule #3, $8,057 + 7.15% x ($231,691 - $126,900)
//     = $8,057 + $7,492.5565 = $15,549.5565, rounded $15,550.
//   - Line 25f: $8,000 + $6,000 = $14,000. Line 35: $1,550.
func TestMaineMarriedFilingJointlyPhaseout(t *testing.T) {
	p1 := ilPerson("gus", 1975, 2, 2, false, false)
	p2 := ilPerson("hal", 1976, 3, 3, false, false)
	in := meInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY,
		p1, p2, nil, []*formw2.FormW2{meW2(150000_00, 25000_00, 8000_00), meW2(110000_00, 15000_00, 6000_00)})
	_, f := prepareMaine(t, in)

	want(t, "Line 16", f.GetMaineAdjustedGrossIncome(), money.USCents(260000_00))
	want(t, "Line 17", f.GetDeduction(), money.USCents(18009_00))
	want(t, "Line 18", f.GetExemption(), money.USCents(10300_00))
	want(t, "Line 19", f.GetTaxableIncome(), money.USCents(231691_00))
	want(t, "Line 20", f.GetIncomeTax(), money.USCents(15550_00))
	want(t, "Line 25f", f.GetTotalPayments(), money.USCents(14000_00))
	want(t, "Line 35", f.GetAmountDue(), money.USCents(1550_00))
}
