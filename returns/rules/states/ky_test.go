package states_test

// Kentucky Form 740 tests.
//
// kentuckytaxcalc holds only estimated-tax logic, so there is no Go income tax
// calculator to compare against. Each household's figures are worked out by
// hand from the 2025 Form 740 and Schedule ITC (sources/catalog/ky/2025), line
// by line in the comment above each test.

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	form1099r "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_r"
	formssa1099 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_ssa_1099"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
	form740 "github.com/nexteggapp/taxprotos/go/us/state/ky/v1/form_740"
)

func kyW2(id, person string, wages, fedWithheld, kyWithheld int64) *returnsv1.W2Document {
	return &returnsv1.W2Document{
		Id:     id,
		Person: person,
		Form: &formw2.FormW2{
			WagesTipsOtherCompensation: cents(wages),
			FederalTaxWithheld:         cents(fedWithheld),
			StateTaxWithholding: []*commonv1.StateTaxWithholding{
				{State: "KY", StateIncome: cents(wages), TaxWithheld: cents(kyWithheld)},
			},
		},
	}
}

func kyInputs(
	status returnsv1.FilingStatusElection,
	hh *returnsv1.Household,
	docs *returnsv1.Documents,
	ky *returnsv1.KentuckyInputs,
) *returnsv1.ReturnInputs {
	return &returnsv1.ReturnInputs{
		SchemaVersion: 1,
		TaxYear:       2025,
		Federal: &returnsv1.Federal1040Inputs{
			Household: hh,
			Elections: &returnsv1.Elections{FilingStatus: status},
			Documents: docs,
		},
		States: []*returnsv1.StateInputs{
			{State: "KY", Inputs: &returnsv1.StateInputs_Ky{Ky: ky}},
		},
	}
}

func prepareKentucky(t *testing.T, in *returnsv1.ReturnInputs) (*returnsv1.KentuckyReturn, *form740.Form740) {
	t.Helper()
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatal(err)
	}
	out, err := eng.Prepare(in)
	if err != nil {
		t.Fatal(err)
	}
	ret := out.GetStates()[0].GetKy()
	if ret == nil {
		t.Fatalf("expected a Kentucky return, got %v", out.GetStates()[0])
	}
	return ret, ret.GetForms().GetForm_740()
}

// 1. Single filer, wages only.
//
//   - Line 5, federal AGI: $50,000 of wages.
//   - Lines 6 to 9: no Schedule M items, so Kentucky AGI is $50,000.
//   - Line 10: the standard deduction printed on line 10, $3,270.
//   - Line 11: $50,000 - $3,270 = $46,730.
//   - Line 12: $46,730 x 4% (printed on line 12) = $1,869.20, entered in whole
//     dollars as $1,869.
//   - Line 17: no personal tax credits (Schedule ITC, Section B): under 65,
//     not blind.
//   - Line 19: $1,869.
//   - Lines 20 and 21: family size 1; modified gross income $50,000 is over
//     the table's last row for family size one ($20,815), so 0% (Schedule ITC,
//     Section C).
//   - Line 26 and 28: $1,869.
//   - Line 31a: $1,800 withheld; line 32: $1,800.
//   - Line 36: $1,869 - $1,800 = $69 owed.
func TestKentuckySingle(t *testing.T) {
	hh := &returnsv1.Household{
		MaritalStatus: returnsv1.MaritalStatus_MARITAL_STATUS_NEVER_MARRIED,
		Taxpayer:      ilPerson("sam", 1990, 4, 2, false, false),
	}
	docs := &returnsv1.Documents{W2S: []*returnsv1.W2Document{
		kyW2("w2_0", "sam", 50000_00, 5000_00, 1800_00),
	}}
	ret, f := prepareKentucky(t, kyInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, hh, docs,
		&returnsv1.KentuckyInputs{}))

	if f.GetFilingStatus() != form740.FilingStatus_FILING_STATUS_SINGLE {
		t.Errorf("filing status = %v", f.GetFilingStatus())
	}
	want(t, "Line 5", f.GetFederalAdjustedGrossIncome(), money.USCents(50000_00))
	want(t, "Line 9", f.GetKentuckyAdjustedGrossIncome(), money.USCents(50000_00))
	want(t, "Line 10", f.GetDeductions(), money.USCents(3270_00))
	want(t, "Line 11", f.GetTaxableIncome(), money.USCents(46730_00))
	want(t, "Line 12", f.GetTax(), money.USCents(1869_00))
	want(t, "Line 17", f.GetPersonalTaxCredits(), money.USCents(0))
	want(t, "Line 19", f.GetTaxBothColumns(), money.USCents(1869_00))
	if f.GetFamilySize() != 1 || f.GetFamilySizeCreditPercent() != 0 {
		t.Errorf("family size %d at %d%%, want 1 at 0%%", f.GetFamilySize(), f.GetFamilySizeCreditPercent())
	}
	want(t, "Line 21", f.GetFamilySizeTaxCredit(), money.USCents(0))
	want(t, "Line 28", f.GetTotalTaxLiability(), money.USCents(1869_00))
	want(t, "Line 31a", f.GetKentuckyTaxWithheld(), money.USCents(1800_00))
	want(t, "Line 32", f.GetTotalPayments(), money.USCents(1800_00))
	want(t, "Line 36", f.GetAmountOwed(), money.USCents(69_00))
	want(t, "Line 37", f.GetOverpayment(), money.USCents(0))
	want(t, "Line 41", f.GetRefund(), money.USCents(0))
	want(t, "Summary tax", ret.GetSummary().GetTotalTax(), money.USCents(1869_00))
	want(t, "Summary owed", ret.GetSummary().GetAmountOwed(), money.USCents(69_00))
	if ret.GetStatus() != returnsv1.ReturnStatus_RETURN_STATUS_COMPLETE {
		t.Errorf("status = %v: %v", ret.GetStatus(), ret.GetDiagnostics())
	}
}

// 2. Married filing jointly, two children aged 9 and 6, modest wages.
//
//   - Line 5: $30,000 + $6,000 = $36,000 of wages.
//   - Line 9: $36,000.
//   - Line 10: one standard deduction on a joint return, $3,270 (line 10).
//   - Line 11: $36,000 - $3,270 = $32,730.
//   - Line 12: $32,730 x 4% = $1,309.20, so $1,309.
//   - Line 17: no personal tax credits.
//   - Line 19: $1,309.
//   - Line 20: family size four: a married couple with two qualifying
//     children (Schedule ITC, Section C instructions).
//   - Modified gross income: the greater of federal AGI $36,000 and Kentucky
//     AGI $36,000 (worksheet line (j)) = $36,000.
//   - Line 21: family size four, MGI over $34,722 and not over $36,008: 70%
//     (Schedule ITC table). $1,309 x 70% = $916.30, so $916.
//   - Line 22: $1,309 - $916 = $393.
//   - Line 24: no federal child and dependent care credit, so $0.
//   - Lines 26 and 28: $393.
//   - Line 31a: $1,000 + $200 = $1,200 withheld.
//   - Line 37 and 41: $1,200 - $393 = $807 refunded.
func TestKentuckyMarriedFilingJointlyWithChildren(t *testing.T) {
	hh := &returnsv1.Household{
		MaritalStatus: returnsv1.MaritalStatus_MARITAL_STATUS_MARRIED,
		Taxpayer:      ilPerson("ana", 1988, 2, 10, false, false),
		Spouse:        ilPerson("ben", 1987, 6, 1, false, false),
		Dependents: []*returnsv1.Dependent{
			ilDependent("cal", 2016, 3, 3),
			ilDependent("dee", 2019, 9, 9),
		},
	}
	docs := &returnsv1.Documents{W2S: []*returnsv1.W2Document{
		kyW2("w2_0", "ana", 30000_00, 0, 1000_00),
		kyW2("w2_1", "ben", 6000_00, 0, 200_00),
	}}
	ret, f := prepareKentucky(t, kyInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY, hh, docs,
		&returnsv1.KentuckyInputs{}))

	if f.GetFilingStatus() != form740.FilingStatus_FILING_STATUS_MARRIED_FILING_JOINTLY {
		t.Errorf("filing status = %v", f.GetFilingStatus())
	}
	want(t, "Line 5", f.GetFederalAdjustedGrossIncome(), money.USCents(36000_00))
	want(t, "Line 10", f.GetDeductions(), money.USCents(3270_00))
	want(t, "Line 11", f.GetTaxableIncome(), money.USCents(32730_00))
	want(t, "Line 12", f.GetTax(), money.USCents(1309_00))
	want(t, "Line 19", f.GetTaxBothColumns(), money.USCents(1309_00))
	if f.GetFamilySize() != 4 || f.GetFamilySizeCreditPercent() != 70 {
		t.Errorf("family size %d at %d%%, want 4 at 70%%", f.GetFamilySize(), f.GetFamilySizeCreditPercent())
	}
	want(t, "Line 21", f.GetFamilySizeTaxCredit(), money.USCents(916_00))
	want(t, "Line 22", f.GetTaxAfterFamilySizeTaxCredit(), money.USCents(393_00))
	want(t, "Line 24", f.GetChildAndDependentCareCredit(), money.USCents(0))
	want(t, "Line 28", f.GetTotalTaxLiability(), money.USCents(393_00))
	want(t, "Line 32", f.GetTotalPayments(), money.USCents(1200_00))
	want(t, "Line 37", f.GetOverpayment(), money.USCents(807_00))
	want(t, "Line 41", f.GetRefund(), money.USCents(807_00))
	want(t, "Line 36", f.GetAmountOwed(), money.USCents(0))
	want(t, "Summary refund", ret.GetSummary().GetRefund(), money.USCents(807_00))
}

// 3. Retired couple, both over 65: a pension and social security.
//
//   - Federal: a $80,000 pension (1099-R box 2a) and $30,000 of social
//     security. Provisional income $80,000 + $15,000 = $95,000 is far over
//     the joint $44,000 base, so 85% is taxable: $25,500 (federal Social
//     Security Benefits Worksheet). Federal AGI $105,500.
//   - Line 5: $105,500.
//   - Line 8: Schedule M subtractions: the taxable social security,
//     $25,500, which Kentucky does not tax, and the pension income
//     exclusion, $31,110 (KRS 141.019), entered as an other subtraction:
//     $56,610.
//   - Line 9: $105,500 - $56,610 = $48,890.
//   - Line 10: $3,270. Line 11: $45,620.
//   - Line 12: $45,620 x 4% = $1,824.80, so $1,825.
//   - Line 17: both 65 or older: $40 + $40 = $80 (Schedule ITC, Section B,
//     lines 1 and 5, line 12 on a joint return).
//   - Line 18 and 19: $1,825 - $80 = $1,745.
//   - Line 21: family size two, MGI the greater of $105,500 and $48,890 is
//     over $28,130: 0%.
//   - Lines 26 and 28: $1,745.
//   - Line 31b: $2,000 of estimated payments. Line 32: $2,000.
//   - Lines 37 and 41: $2,000 - $1,745 = $255 refunded.
func TestKentuckyRetiredCouple(t *testing.T) {
	hh := &returnsv1.Household{
		MaritalStatus: returnsv1.MaritalStatus_MARITAL_STATUS_MARRIED,
		Taxpayer:      ilPerson("eve", 1952, 1, 5, false, false),
		Spouse:        ilPerson("fay", 1954, 7, 7, false, false),
	}
	docs := &returnsv1.Documents{
		RetirementDistributions: []*returnsv1.Form1099RDocument{{
			Id:     "1099r_0",
			Person: "eve",
			Form: &form1099r.Form1099R{
				GrossDistribution: cents(80000_00),
				TaxableAmount:     cents(80000_00),
			},
		}},
		SocialSecurity: []*returnsv1.FormSSA1099Document{
			{Id: "ssa_0", Person: "eve", Form: &formssa1099.FormSSA1099{BenefitsPaid: cents(18000_00), NetBenefits: cents(18000_00)}},
			{Id: "ssa_1", Person: "fay", Form: &formssa1099.FormSSA1099{BenefitsPaid: cents(12000_00), NetBenefits: cents(12000_00)}},
		},
	}
	ret, f := prepareKentucky(t, kyInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY, hh, docs,
		&returnsv1.KentuckyInputs{
			OtherSubtractions:    cents(31110_00),
			EstimatedTaxPayments: cents(2000_00),
		}))

	want(t, "Line 5", f.GetFederalAdjustedGrossIncome(), money.USCents(105500_00))
	want(t, "Line 8", f.GetSubtractions(), money.USCents(56610_00))
	want(t, "Line 9", f.GetKentuckyAdjustedGrossIncome(), money.USCents(48890_00))
	want(t, "Line 11", f.GetTaxableIncome(), money.USCents(45620_00))
	want(t, "Line 12", f.GetTax(), money.USCents(1825_00))
	want(t, "Line 17", f.GetPersonalTaxCredits(), money.USCents(80_00))
	want(t, "Line 19", f.GetTaxBothColumns(), money.USCents(1745_00))
	if f.GetFamilySize() != 2 || f.GetFamilySizeCreditPercent() != 0 {
		t.Errorf("family size %d at %d%%, want 2 at 0%%", f.GetFamilySize(), f.GetFamilySizeCreditPercent())
	}
	want(t, "Line 28", f.GetTotalTaxLiability(), money.USCents(1745_00))
	want(t, "Line 31b", f.GetEstimatedTaxPayments(), money.USCents(2000_00))
	want(t, "Line 41", f.GetRefund(), money.USCents(255_00))
	want(t, "Summary tax", ret.GetSummary().GetTotalTax(), money.USCents(1745_00))
}

// The Schedule ITC instructions' own examples: a single filer with one
// qualifying child and MGI of $22,650 is family size two at 80%; a joint
// return with two qualifying children and MGI of $33,000 is family size four
// at 90%.
func TestKentuckyFamilySizeExamples(t *testing.T) {
	cases := []struct {
		name    string
		status  returnsv1.FilingStatusElection
		spouse  bool
		kids    int
		wages   int64
		size    int32
		percent int32
	}{
		{"example 1", returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_HEAD_OF_HOUSEHOLD, false, 1, 22650_00, 2, 80},
		{"example 2", returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY, true, 2, 33000_00, 4, 90},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			hh := &returnsv1.Household{
				MaritalStatus: returnsv1.MaritalStatus_MARITAL_STATUS_NEVER_MARRIED,
				Taxpayer:      ilPerson("gus", 1985, 1, 1, false, false),
			}
			if c.spouse {
				hh.MaritalStatus = returnsv1.MaritalStatus_MARITAL_STATUS_MARRIED
				hh.Spouse = ilPerson("hal", 1985, 1, 1, false, false)
			}
			for i := 0; i < c.kids; i++ {
				hh.Dependents = append(hh.Dependents, ilDependent([]string{"ivy", "jo"}[i], 2015, 1, 1))
			}
			docs := &returnsv1.Documents{W2S: []*returnsv1.W2Document{
				kyW2("w2_0", "gus", c.wages, 0, 0),
			}}
			_, f := prepareKentucky(t, kyInputs(c.status, hh, docs, &returnsv1.KentuckyInputs{}))
			if f.GetFamilySize() != c.size || f.GetFamilySizeCreditPercent() != c.percent {
				t.Errorf("family size %d at %d%%, want %d at %d%%",
					f.GetFamilySize(), f.GetFamilySizeCreditPercent(), c.size, c.percent)
			}
		})
	}
}
