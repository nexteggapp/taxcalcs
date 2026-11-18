package states_test

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	form1099r "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_r"
	formssa1099 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_ssa_1099"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
	formia1040 "github.com/nexteggapp/taxprotos/go/us/state/ia/v1/form_ia1040"
)

// There is no Go income tax calculator for Iowa, so these households are worked
// by hand from the 2025 IA 1040, its expanded instructions and the 2025 Iowa
// Income Tax Reduction Worksheet (41-146). Federal figures are the 2025
// federal return's.

func iaInputs(
	status returnsv1.FilingStatusElection,
	taxpayer, spouse *returnsv1.Person,
	dependents []*returnsv1.Dependent,
	docs *returnsv1.Documents,
	ia *returnsv1.IowaInputs,
) *returnsv1.ReturnInputs {
	marital := returnsv1.MaritalStatus_MARITAL_STATUS_NEVER_MARRIED
	if spouse != nil {
		marital = returnsv1.MaritalStatus_MARITAL_STATUS_MARRIED
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
			Documents: docs,
		},
		States: []*returnsv1.StateInputs{
			{State: "IA", Inputs: &returnsv1.StateInputs_Ia{Ia: ia}},
		},
	}
}

func iaW2(id, person string, wages, fedWithheld, iaWithheld int64) *returnsv1.W2Document {
	return &returnsv1.W2Document{
		Id:     id,
		Person: person,
		Form: &formw2.FormW2{
			WagesTipsOtherCompensation: cents(wages),
			FederalTaxWithheld:         cents(fedWithheld),
			StateTaxWithholding: []*commonv1.StateTaxWithholding{
				{State: "IA", StateIncome: cents(wages), TaxWithheld: cents(iaWithheld)},
			},
		},
	}
}

func prepareIowa(t *testing.T, in *returnsv1.ReturnInputs) (*returnsv1.IowaReturn, *formia1040.FormIA1040) {
	t.Helper()
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatal(err)
	}
	out, err := eng.Prepare(in)
	if err != nil {
		t.Fatal(err)
	}
	ret := out.GetStates()[0].GetIa()
	if ret == nil {
		t.Fatalf("expected an Iowa return, got %v", out.GetStates()[0])
	}
	return ret, ret.GetForms().GetFormIa1040()
}

// A single filer, 37, with wages of $60,100 and $2,000 of Iowa tax withheld.
//
//	Federal: AGI $60,100; standard deduction $15,750; taxable income $44,350.
//	Step 3 a: 1 personal credit x $40 = $40 (instructions, Exemption Credits). d: $40.
//	Line 2: $44,350. Line 3: no Schedule 1 modifications, $0. Line 4: $44,350.
//	Line 5: $44,350 x 3.8% = $1,685.30, $1,685 (instructions, Line 5).
//	Line 7: $1,685. Line 8: $40. Line 11: $40.
//	Line 12: $1,685 - $40 = $1,645. Tax reduction worksheet (41-146): line 1 is
//	  $44,350 + $15,750 = $60,100; line 3 is $60,100 - $9,000 = $51,100, more
//	  than $1,645, so line 12 stays $1,645.
//	Lines 14, 16, 18: $1,645. Line 19: no surtax, $0. Lines 20, 22: $1,645.
//	Line 28: $2,000. Line 30: $2,000.
//	Line 31a and 32: $2,000 - $1,645 = $355 refunded. Line 34: $0.
func TestIowaSingle(t *testing.T) {
	p := ilPerson("jo", 1988, 5, 12, false, false)
	in := iaInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, p, nil, nil,
		&returnsv1.Documents{W2S: []*returnsv1.W2Document{iaW2("w2_0", "jo", 60100_00, 6000_00, 2000_00)}},
		&returnsv1.IowaInputs{})
	ret, f := prepareIowa(t, in)

	want(t, "1c federal AGI", f.GetFederalAdjustedGrossIncome(), money.USCents(60100_00))
	want(t, "1d federal deduction", f.GetFederalStandardOrItemizedDeduction(), money.USCents(15750_00))
	want(t, "2 federal taxable income", f.GetFederalTaxableIncome(), money.USCents(44350_00))
	want(t, "3 net modifications", f.GetNetIowaModifications(), money.USCents(0))
	want(t, "4 Iowa taxable income", f.GetIowaTaxableIncome(), money.USCents(44350_00))
	want(t, "5 Iowa tax", f.GetIowaTax(), money.USCents(1685_00))
	want(t, "8 exemption credits", f.GetTotalExemptionCredit(), money.USCents(40_00))
	want(t, "12 balance", f.GetBalanceAfterCredits(), money.USCents(1645_00))
	want(t, "18 state tax", f.GetStateTax(), money.USCents(1645_00))
	want(t, "22 total tax", f.GetTotalTaxAndContributions(), money.USCents(1645_00))
	want(t, "28 withheld", f.GetIowaTaxWithheld(), money.USCents(2000_00))
	want(t, "30 payments", f.GetTotalPaymentsAndRefundableCredits(), money.USCents(2000_00))
	want(t, "32 refund", f.GetRefund(), money.USCents(355_00))
	want(t, "37 amount due", f.GetTotalAmountDue(), money.USCents(0))
	if f.GetSpecialTaxComputation() {
		t.Error("special tax computation box checked; the flat rate applies")
	}
	if ret.GetStatus() != returnsv1.ReturnStatus_RETURN_STATUS_COMPLETE {
		t.Errorf("status %v: %v", ret.GetStatus(), ret.GetDiagnostics())
	}
}

// Married filing jointly, both about 40, two children, wages of $90,000 and
// $40,000, $4,500 of Iowa tax withheld, in a school district with a 10% surtax.
//
//	Federal: AGI $130,000; standard deduction $31,500; taxable income $98,500.
//	Step 3 a: 2 personal credits x $40 = $80. c: 2 dependents x $40 = $80. d: $160.
//	Line 4: $98,500. Line 5: $98,500 x 3.8% = $3,743. The alternate tax
//	  (instructions, Line 5 worksheet) is ($130,000 - $13,500) x 4.3% = $5,009.50,
//	  more than $3,743, so line 5 is $3,743.
//	Line 11: $160. Line 12: $3,743 - $160 = $3,583. Line 18: $3,583.
//	Line 19: $3,583 x 10% = $358.30, $358. Lines 20, 22: $3,941.
//	Line 25: no federal EITC at this income, $0. Line 28: $4,500. Line 30: $4,500.
//	Line 32: $4,500 - $3,941 = $559 refunded.
func TestIowaMarriedFilingJointlyWithChildren(t *testing.T) {
	p1 := ilPerson("ann", 1985, 3, 15, false, false)
	p2 := ilPerson("ben", 1984, 8, 20, false, false)
	kids := []*returnsv1.Dependent{ilDependent("cal", 2020, 1, 10), ilDependent("dee", 2017, 6, 2)}
	in := iaInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY, p1, p2, kids,
		&returnsv1.Documents{W2S: []*returnsv1.W2Document{
			iaW2("w2_0", "ann", 90000_00, 9000_00, 3000_00),
			iaW2("w2_1", "ben", 40000_00, 4000_00, 1500_00),
		}},
		&returnsv1.IowaInputs{SchoolDistrictSurtaxPercent: 10})
	_, f := prepareIowa(t, in)

	want(t, "2 federal taxable income", f.GetFederalTaxableIncome(), money.USCents(98500_00))
	want(t, "a personal credit", f.GetPersonalCredit(), money.USCents(80_00))
	want(t, "c dependent credit", f.GetDependentCredit(), money.USCents(80_00))
	want(t, "4 Iowa taxable income", f.GetIowaTaxableIncome(), money.USCents(98500_00))
	want(t, "5 Iowa tax", f.GetIowaTax(), money.USCents(3743_00))
	want(t, "11 total credits", f.GetTotalCredits(), money.USCents(160_00))
	want(t, "12 balance", f.GetBalanceAfterCredits(), money.USCents(3583_00))
	want(t, "19 surtax", f.GetSchoolDistrictAndEmsSurtax(), money.USCents(358_00))
	want(t, "22 total tax", f.GetTotalTaxAndContributions(), money.USCents(3941_00))
	want(t, "25 Iowa EITC", f.GetEarnedIncomeCredit(), money.USCents(0))
	want(t, "30 payments", f.GetTotalPaymentsAndRefundableCredits(), money.USCents(4500_00))
	want(t, "32 refund", f.GetRefund(), money.USCents(559_00))
	want(t, "34 amount owed", f.GetAmountOwed(), money.USCents(0))
}

// A retired couple filing jointly, both 70: wages of $50,000, a pension of
// $60,000 and Social Security of $30,000; $1,500 of Iowa tax withheld from
// the wages and $1,000 from the pension.
//
//	Federal: taxable Social Security is the 85% cap, $25,500; AGI $135,500;
//	  standard deduction $31,500 + 2 x $1,600 = $34,700; enhanced deduction for
//	  seniors 2 x $6,000 = $12,000 (AGI under $150,000); taxable income $88,800.
//	Step 3 a: 2 x $40 = $80. b: 2 filers 65 or older x $20 = $40. d: $120.
//	Schedule 1 line 5: Social Security $25,500. Line 7: pension $60,000, both
//	  55 or older. Line 21: -$85,500.
//	Line 4: $88,800 - $85,500 = $3,300.
//	Line 5: $3,300 x 3.8% = $125.40, $125. Alternate tax: line 1g is $3,300 +
//	  $34,700 + $12,000 = $50,000; less $32,000 (a filer 65 or older) is
//	  $18,000; x 4.3% = $774, more than $125, so line 5 is $125. The
//	  low-income exemption needs $32,000 or less, so it does not apply.
//	Line 12: $125 - $120 = $5. Lines 18, 20, 22: $5.
//	Line 28: $2,500. Line 32: $2,500 - $5 = $2,495 refunded.
func TestIowaRetiredCouple(t *testing.T) {
	p1 := ilPerson("ed", 1955, 4, 1, false, false)
	p2 := ilPerson("flo", 1955, 9, 9, false, false)
	in := iaInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY, p1, p2, nil,
		&returnsv1.Documents{
			W2S: []*returnsv1.W2Document{iaW2("w2_0", "ed", 50000_00, 5000_00, 1500_00)},
			RetirementDistributions: []*returnsv1.Form1099RDocument{{
				Id:     "1099r_0",
				Person: "flo",
				Form: &form1099r.Form1099R{
					GrossDistribution: cents(60000_00),
					TaxableAmount:     cents(60000_00),
					DistributionCodes: []string{"7"},
					StateTaxWithholding: []*commonv1.StateTaxWithholding{
						{State: "IA", TaxWithheld: cents(1000_00)},
					},
				},
			}},
			SocialSecurity: []*returnsv1.FormSSA1099Document{{
				Id:     "ssa_0",
				Person: "ed",
				Form:   &formssa1099.FormSSA1099{BenefitsPaid: cents(30000_00), NetBenefits: cents(30000_00)},
			}},
		},
		&returnsv1.IowaInputs{})
	ret, f := prepareIowa(t, in)

	want(t, "1c federal AGI", f.GetFederalAdjustedGrossIncome(), money.USCents(135500_00))
	want(t, "1d federal deduction", f.GetFederalStandardOrItemizedDeduction(), money.USCents(34700_00))
	want(t, "1f enhanced deduction for seniors", f.GetEnhancedDeductionForSeniors(), money.USCents(12000_00))
	want(t, "2 federal taxable income", f.GetFederalTaxableIncome(), money.USCents(88800_00))
	want(t, "b age credit", f.GetAge_65OrBlindCredit(), money.USCents(40_00))
	want(t, "3 net modifications", f.GetNetIowaModifications(), money.USCents(-85500_00))
	want(t, "4 Iowa taxable income", f.GetIowaTaxableIncome(), money.USCents(3300_00))
	want(t, "5 Iowa tax", f.GetIowaTax(), money.USCents(125_00))
	want(t, "8 exemption credits", f.GetTotalExemptionCredit(), money.USCents(120_00))
	want(t, "12 balance", f.GetBalanceAfterCredits(), money.USCents(5_00))
	want(t, "22 total tax", f.GetTotalTaxAndContributions(), money.USCents(5_00))
	want(t, "28 withheld", f.GetIowaTaxWithheld(), money.USCents(2500_00))
	want(t, "32 refund", f.GetRefund(), money.USCents(2495_00))
	if ret.GetStatus() != returnsv1.ReturnStatus_RETURN_STATUS_COMPLETE {
		t.Errorf("status %v: %v", ret.GetStatus(), ret.GetDiagnostics())
	}
}

// A single filer, 30, with wages of $8,000: the low-income exemption
// (instructions, Line 4) takes the tax away.
func TestIowaSingleLowIncomeExemption(t *testing.T) {
	p := ilPerson("gus", 1995, 2, 2, false, false)
	in := iaInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE, p, nil, nil,
		&returnsv1.Documents{W2S: []*returnsv1.W2Document{iaW2("w2_0", "gus", 8000_00, 0, 100_00)}},
		&returnsv1.IowaInputs{})
	_, f := prepareIowa(t, in)

	// Wages of $8,000: federal taxable income $0, Iowa taxable income $0; line 1
	// of the exemption test is $0 + $8,000 (the deduction, as far as it reduced
	// federal taxable income) = $8,000, at most $9,000, so the low-income
	// exemption applies and line 5 is $0. Withholding of $100 is refunded with
	// the Iowa EITC, 15% of the federal credit.
	want(t, "4 Iowa taxable income", f.GetIowaTaxableIncome(), money.USCents(0))
	want(t, "5 Iowa tax", f.GetIowaTax(), money.USCents(0))
	want(t, "22 total tax", f.GetTotalTaxAndContributions(), money.USCents(0))
	if !f.GetSpecialTaxComputation() {
		t.Error("the low-income exemption should check the special computation box")
	}
}
