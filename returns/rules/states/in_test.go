package states_test

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
)

// There is no Indiana income tax calculator in Go to compare against
// (indianataxcalc holds only estimated tax), so the figures below are worked
// by hand from the 2025 Form IT-40, its Schedules 3, 5 and CT-40, and the
// IT-40 booklet, all catalogued under sources/catalog/in/2025. The booklet
// says to round every entry to the nearest whole dollar (Booklet p. 5,
// "Rounding Required").

// inW2 is a W-2 with Indiana state withholding and, when countyWithheld is
// not zero, a county row in boxes 18 to 20.
func inW2(wages, fedWithheld, stateWithheld, countyWithheld int64) *formw2.FormW2 {
	w := &formw2.FormW2{
		WagesTipsOtherCompensation: cents(wages),
		FederalTaxWithheld:         cents(fedWithheld),
		StateTaxWithholding: []*commonv1.StateTaxWithholding{
			{State: "IN", StateIncome: cents(wages), TaxWithheld: cents(stateWithheld)},
		},
	}
	if countyWithheld != 0 {
		w.LocalTaxWithholding = []*commonv1.LocalTaxWithholding{
			{Locality: "COUNTY", TaxWithheld: cents(countyWithheld), LocalIncome: cents(wages)},
		}
	}
	return w
}

// inPrepare prepares the return the inputs describe, asking only for
// Indiana's, and returns it.
func inPrepare(t *testing.T, in *returnsv1.ReturnInputs, inIn *returnsv1.IndianaInputs) *returnsv1.IndianaReturn {
	t.Helper()
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatal(err)
	}
	in.States = []*returnsv1.StateInputs{
		{State: "IN", Inputs: &returnsv1.StateInputs_In{In: inIn}},
	}
	out, err := eng.Prepare(in)
	if err != nil {
		t.Fatal(err)
	}
	ret := out.GetStates()[0].GetIn()
	if ret == nil {
		t.Fatal("expected an Indiana return")
	}
	return ret
}

// 1. Single filer with wages, living in Marion County, who owes.
//
//   - Line 1: federal AGI = wages $50,000.
//   - Lines 2 and 4: no add-backs or deductions, $0. Lines 3 and 5: $50,000.
//   - Line 6: Schedule 3 line 1, $1,000 (not married filing jointly).
//   - Line 7: $50,000 - $1,000 = $49,000.
//   - Line 8: $49,000 x 3% = $1,470.
//   - Line 9: Schedule CT-40: Marion, code 49, rate .0202 (CT-40 rate chart);
//     $49,000 x .0202 = $989.80, rounded to $990.
//   - Line 11: $1,470 + $990 = $2,460.
//   - Line 12: Schedule 5 line 1 state tax withheld $1,400, line 2 county tax
//     withheld $900 (W-2 boxes 17 and 19, Booklet Schedule 5 lines 1 and 2);
//     no earned income credit (the federal credit is zero at $50,000).
//     Total $2,300.
//   - Line 14: $2,300. Line 15: $2,460.
//   - Line 23: $2,460 - $2,300 = $160. Line 26: $160. Line 21: $0.
func TestIndianaSingleOwes(t *testing.T) {
	in := ilInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		ilPerson("ivy", 1990, 4, 2, false, false), nil, nil,
		[]*formw2.FormW2{inW2(50000_00, 5000_00, 1400_00, 900_00)},
		0,
	)
	ret := inPrepare(t, in, &returnsv1.IndianaInputs{CountyCode: "49"})
	f := ret.GetForms().GetFormIt40()

	want(t, "Line 1", f.GetFederalAdjustedGrossIncome(), money.USCents(50000_00))
	want(t, "Line 5", f.GetIncomeAfterDeductions(), money.USCents(50000_00))
	want(t, "Line 6", f.GetExemptions(), money.USCents(1000_00))
	want(t, "Line 7", f.GetIndianaAdjustedGrossIncome(), money.USCents(49000_00))
	want(t, "Line 8", f.GetStateAdjustedGrossIncomeTax(), money.USCents(1470_00))
	want(t, "Line 9", f.GetCountyTax(), money.USCents(990_00))
	want(t, "Line 11", f.GetTotalTaxes(), money.USCents(2460_00))
	want(t, "Line 12", f.GetCredits(), money.USCents(2300_00))
	want(t, "Line 14", f.GetTotalCredits(), money.USCents(2300_00))
	want(t, "Line 15", f.GetTaxesCarriedForward(), money.USCents(2460_00))
	want(t, "Line 21", f.GetRefund(), money.USCents(0))
	want(t, "Line 23", f.GetTaxDue(), money.USCents(160_00))
	want(t, "Line 26", f.GetAmountOwed(), money.USCents(160_00))

	want(t, "summary tax", ret.GetSummary().GetTotalTax(), money.USCents(2460_00))
	want(t, "summary owed", ret.GetSummary().GetAmountOwed(), money.USCents(160_00))
	if f.GetCountyLived() != "49" {
		t.Errorf("county lived = %q, want 49", f.GetCountyLived())
	}
	if ret.GetStatus() != returnsv1.ReturnStatus_RETURN_STATUS_COMPLETE {
		t.Errorf("status = %v, want complete: %v", ret.GetStatus(), ret.GetDiagnostics())
	}
}

// 2. Married filing jointly, one earner, two children under 19, the federal
// earned income credit, living in Hamilton County.
//
//   - Line 1: federal AGI = wages $25,000.
//   - Line 6: Schedule 3 line 1 $2,000 (married filing jointly); line 2,
//     2 dependents x $1,000 = $2,000; line 3, 2 dependent children under 19
//     x $1,500 = $3,000. Total $7,000.
//   - Line 7: $25,000 - $7,000 = $18,000.
//   - Line 8: $18,000 x 3% = $540.
//   - Line 9: Hamilton, code 29, rate .011; $18,000 x .011 = $198.
//   - Line 11: $738.
//   - Line 12: state tax withheld $300; Schedule 5 line 6 earned income
//     credit = federal credit x 10% (Booklet, Schedule 5 line 6: "Your 2025
//     EIC will be your federal EIC multiplied by 10%"). The federal credit
//     with two qualifying children at $25,000 joint is the 2025 maximum,
//     $7,152 (Rev. Proc. 2024-40, section 3.07), so $715.20, rounded to $715.
//     Total $1,015.
//   - Line 16: $1,015 - $738 = $277. Lines 18 and 21: refund $277.
func TestIndianaMarriedFilingJointlyWithChildren(t *testing.T) {
	in := ilInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY,
		ilPerson("ada", 1988, 1, 9, false, false),
		ilPerson("ben", 1987, 7, 30, false, false),
		[]*returnsv1.Dependent{
			ilDependent("cal", 2016, 3, 3),
			ilDependent("dee", 2019, 11, 21),
		},
		[]*formw2.FormW2{inW2(25000_00, 0, 300_00, 0)},
		0,
	)
	ret := inPrepare(t, in, &returnsv1.IndianaInputs{CountyCode: "29"})
	f := ret.GetForms().GetFormIt40()

	want(t, "Line 1", f.GetFederalAdjustedGrossIncome(), money.USCents(25000_00))
	want(t, "Line 6", f.GetExemptions(), money.USCents(7000_00))
	want(t, "Line 7", f.GetIndianaAdjustedGrossIncome(), money.USCents(18000_00))
	want(t, "Line 8", f.GetStateAdjustedGrossIncomeTax(), money.USCents(540_00))
	want(t, "Line 9", f.GetCountyTax(), money.USCents(198_00))
	want(t, "Line 11", f.GetTotalTaxes(), money.USCents(738_00))
	want(t, "Line 12", f.GetCredits(), money.USCents(1015_00))
	want(t, "Line 16", f.GetOverpaymentBeforeDonations(), money.USCents(277_00))
	want(t, "Line 18", f.GetOverpayment(), money.USCents(277_00))
	want(t, "Line 21", f.GetRefund(), money.USCents(277_00))
	want(t, "Line 26", f.GetAmountOwed(), money.USCents(0))
	want(t, "summary refund", ret.GetSummary().GetRefund(), money.USCents(277_00))
	if f.GetSpouseCountyLived() != "29" {
		t.Errorf("spouse county lived = %q, want 29", f.GetSpouseCountyLived())
	}
}

// 3. A retired single filer aged 70 with part-time wages, in Allen County.
//
//   - Line 1: federal AGI = wages $30,000.
//   - Line 6: Schedule 3 line 1 $1,000; line 4, one box (65 or older) x
//     $1,000; line 5, 65 or older with line 1 under $40,000, one box x $500.
//     Total $2,500.
//   - Line 7: $30,000 - $2,500 = $27,500.
//   - Line 8: $27,500 x 3% = $825.
//   - Line 9: Allen, code 02, rate .0159; $27,500 x .0159 = $437.25, rounded
//     to $437.
//   - Line 11: $1,262.
//   - Line 12: state tax withheld $600, Schedule 5 line 4 estimated tax paid
//     $300; no earned income credit (over 64, no children). Total $900.
//   - Line 23: $1,262 - $900 = $362. Line 26: $362.
func TestIndianaRetiredSingle(t *testing.T) {
	in := ilInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		ilPerson("ray", 1955, 2, 14, false, false), nil, nil,
		[]*formw2.FormW2{inW2(30000_00, 2000_00, 600_00, 0)},
		0,
	)
	ret := inPrepare(t, in, &returnsv1.IndianaInputs{
		CountyCode:           "02",
		EstimatedTaxPayments: cents(300_00),
	})
	f := ret.GetForms().GetFormIt40()

	want(t, "Line 6", f.GetExemptions(), money.USCents(2500_00))
	want(t, "Line 7", f.GetIndianaAdjustedGrossIncome(), money.USCents(27500_00))
	want(t, "Line 8", f.GetStateAdjustedGrossIncomeTax(), money.USCents(825_00))
	want(t, "Line 9", f.GetCountyTax(), money.USCents(437_00))
	want(t, "Line 11", f.GetTotalTaxes(), money.USCents(1262_00))
	want(t, "Line 12", f.GetCredits(), money.USCents(900_00))
	want(t, "Line 23", f.GetTaxDue(), money.USCents(362_00))
	want(t, "Line 26", f.GetAmountOwed(), money.USCents(362_00))
}

// 4. A high-income joint return whose spouses lived in different counties on
// January 1, with add-backs and a deduction.
//
//   - Line 1: federal AGI = wages $200,000.
//   - Line 2: add-backs $1,000. Line 3: $201,000.
//   - Line 4: deductions $3,000 (e.g. the renter's deduction). Line 5:
//     $198,000.
//   - Line 6: $2,000. Line 7: $196,000.
//   - Line 8: $196,000 x 3% = $5,880.
//   - Line 9: Schedule CT-40 with both columns. Line 1B, the spouse's share,
//     $80,000; line 1A, the rest, $116,000. Line 2A Lake, code 45, .015;
//     line 2B Porter, code 64, .005. Line 3A $1,740; line 3B $400. Lines 4
//     and 7: $2,140.
//   - Line 11: $5,880 + $2,140 = $8,020.
//   - Line 12: state tax withheld $5,000 + estimated tax $1,000 = $6,000.
//   - Line 23: $8,020 - $6,000 = $2,020. Line 26: $2,020.
func TestIndianaHighIncomeSpousesInDifferentCounties(t *testing.T) {
	in := ilInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY,
		ilPerson("max", 1980, 6, 1, false, false),
		ilPerson("nia", 1981, 8, 8, false, false),
		nil,
		[]*formw2.FormW2{inW2(200000_00, 30000_00, 5000_00, 0)},
		0,
	)
	ret := inPrepare(t, in, &returnsv1.IndianaInputs{
		AddBacks:             cents(1000_00),
		OtherDeductions:      cents(3000_00),
		CountyCode:           "45",
		SpouseCountyCode:     "64",
		SpouseShareOfIncome:  cents(80000_00),
		EstimatedTaxPayments: cents(1000_00),
	})
	f := ret.GetForms().GetFormIt40()

	want(t, "Line 2", f.GetAddBacks(), money.USCents(1000_00))
	want(t, "Line 3", f.GetIncomeWithAddBacks(), money.USCents(201000_00))
	want(t, "Line 4", f.GetDeductions(), money.USCents(3000_00))
	want(t, "Line 5", f.GetIncomeAfterDeductions(), money.USCents(198000_00))
	want(t, "Line 6", f.GetExemptions(), money.USCents(2000_00))
	want(t, "Line 7", f.GetIndianaAdjustedGrossIncome(), money.USCents(196000_00))
	want(t, "Line 8", f.GetStateAdjustedGrossIncomeTax(), money.USCents(5880_00))
	want(t, "Line 9", f.GetCountyTax(), money.USCents(2140_00))
	want(t, "Line 11", f.GetTotalTaxes(), money.USCents(8020_00))
	want(t, "Line 12", f.GetCredits(), money.USCents(6000_00))
	want(t, "Line 26", f.GetAmountOwed(), money.USCents(2020_00))
	if f.GetSpouseCountyLived() != "64" {
		t.Errorf("spouse county lived = %q, want 64", f.GetSpouseCountyLived())
	}
}

// 5. Without a county code the county tax is left at zero and the return
// carries a warning, so it is not complete.
func TestIndianaNoCountyWarns(t *testing.T) {
	in := ilInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		ilPerson("zed", 1990, 4, 2, false, false), nil, nil,
		[]*formw2.FormW2{inW2(50000_00, 5000_00, 1400_00, 0)},
		0,
	)
	ret := inPrepare(t, in, &returnsv1.IndianaInputs{})
	f := ret.GetForms().GetFormIt40()

	want(t, "Line 8", f.GetStateAdjustedGrossIncomeTax(), money.USCents(1470_00))
	want(t, "Line 9", f.GetCountyTax(), money.USCents(0))
	found := false
	for _, d := range ret.GetDiagnostics() {
		if d.GetCode() == "IN_COUNTY_NOT_GIVEN" {
			found = true
		}
	}
	if !found {
		t.Errorf("no IN_COUNTY_NOT_GIVEN diagnostic: %v", ret.GetDiagnostics())
	}
}
