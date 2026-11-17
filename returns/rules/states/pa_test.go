package states_test

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	form1099div "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_div"
	form1099int "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_int"
	form1099r "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_r"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
	formpa40 "github.com/nexteggapp/taxprotos/go/us/state/pa/v1/form_pa40"
)

// There is no Go income tax calculator for Pennsylvania, so
// these households are worked by hand from the 2025 Form PA-40 and its
// instructions (PA-40 IN). The working is in each test's comment, line by
// line.

// A household asking for a Pennsylvania return, with the given documents.
func paInputs(
	status returnsv1.FilingStatusElection,
	taxpayer, spouse *returnsv1.Person,
	dependents []*returnsv1.Dependent,
	docs *returnsv1.Documents,
	paIn *returnsv1.PennsylvaniaInputs,
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
			{State: "PA", Inputs: &returnsv1.StateInputs_Pa{Pa: paIn}},
		},
	}
}

// A Form W-2 whose box 16 Pennsylvania wages equal box 1.
func paW2(id, person string, wages, paWithheld int64) *returnsv1.W2Document {
	return &returnsv1.W2Document{
		Id:     id,
		Person: person,
		Form: &formw2.FormW2{
			WagesTipsOtherCompensation: cents(wages),
			FederalTaxWithheld:         cents(wages / 10),
			StateTaxWithholding: []*commonv1.StateTaxWithholding{
				{State: "PA", StateIncome: cents(wages), TaxWithheld: cents(paWithheld)},
			},
		},
	}
}

func preparePA(t *testing.T, in *returnsv1.ReturnInputs) (*returnsv1.PennsylvaniaReturn, *formpa40.FormPA40) {
	t.Helper()
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatal(err)
	}
	out, err := eng.Prepare(in)
	if err != nil {
		t.Fatal(err)
	}
	ret := out.GetStates()[0].GetPa()
	if ret == nil {
		t.Fatalf("expected a Pennsylvania return, got %v", out.GetStates()[0])
	}
	if ret.GetStatus() != returnsv1.ReturnStatus_RETURN_STATUS_COMPLETE {
		t.Errorf("status = %v: %v", ret.GetStatus(), ret.GetDiagnostics())
	}
	return ret, ret.GetForms().GetFormPa40()
}

// 1. Single filer with one job.
//   - Line 1a: $50,000.00, W-2 box 16 (PA-40 IN, Line 1a: box 16 usually
//     shows PA-taxable compensation when box 17 has PA withholding).
//   - Line 1c: $50,000.00; lines 2 to 8 are nothing.
//   - Line 9 and 11: $50,000.00 (no line 10 deductions).
//   - Line 12: $50,000 x 0.0307 = $1,535.00 (PA-40 line 12).
//   - Line 13: $1,600.00 from box 17.
//   - Line 20: eligibility income is line 9, $50,000, above $8,750, the last
//     column of Eligibility Income Table 1 for no dependents, so line 21 is
//     nothing.
//   - Line 24: $1,600.00. Line 29: $1,600 - $1,535 = $65.00, all refunded.
func TestPennsylvaniaSingle(t *testing.T) {
	p := ilPerson("dana", 1990, 4, 2, false, false)
	in := paInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		p, nil, nil,
		&returnsv1.Documents{W2S: []*returnsv1.W2Document{paW2("w2_0", "dana", 50000_00, 1600_00)}},
		&returnsv1.PennsylvaniaInputs{},
	)
	ret, f := preparePA(t, in)

	if f.GetFilingStatus() != formpa40.FilingStatus_FILING_STATUS_SINGLE {
		t.Errorf("filing status = %v, want single", f.GetFilingStatus())
	}
	want(t, "Line 1a Gross compensation", f.GetGrossCompensation(), money.USCents(50000_00))
	want(t, "Line 9 Total PA taxable income", f.GetTotalTaxableIncome(), money.USCents(50000_00))
	want(t, "Line 11 Adjusted PA taxable income", f.GetAdjustedTaxableIncome(), money.USCents(50000_00))
	want(t, "Line 12 PA tax liability", f.GetTaxLiability(), money.USCents(1535_00))
	want(t, "Line 13 PA tax withheld", f.GetTaxWithheld(), money.USCents(1600_00))
	want(t, "Line 21 Tax Forgiveness", f.GetTaxForgivenessCredit(), money.USCents(0))
	want(t, "Line 24 Total payments and credits", f.GetTotalPaymentsAndCredits(), money.USCents(1600_00))
	want(t, "Line 26 Tax due", f.GetTaxDue(), money.USCents(0))
	want(t, "Line 29 Overpayment", f.GetOverpayment(), money.USCents(65_00))
	want(t, "Line 30 Refund", f.GetRefund(), money.USCents(65_00))
	want(t, "Summary total tax", ret.GetSummary().GetTotalTax(), money.USCents(1535_00))
}

// 2. Married filing jointly, two children, low wages: partial Tax Forgiveness.
//   - Line 1a: $25,000 + $7,600 = $32,600.00 (W-2 box 16).
//   - Line 9 and 11: $32,600.00.
//   - Line 12: $32,600 x 0.0307 = $1,000.82, rounded to $1,001.00 (PA-40 IN:
//     round amounts to the nearest dollar).
//   - Line 13: $768 + $233 = $1,001.00.
//   - Line 19a: married; line 19b: 2 dependents (Schedule SP Section II).
//   - Line 20: eligibility income $32,600 (Schedule SP line 1 is PA-40 line 9;
//     no nontaxable income).
//   - Schedule SP line 15: Eligibility Income Table 2, married with 2
//     dependent children: $32,000 is 100%, $32,250 90%, $32,500 80%, $32,750
//     70%. $32,600 does not exceed $32,750, so 70%.
//   - Line 21: Schedule SP line 14 is $1,001 (no resident credit), x 0.70 =
//     $700.70, rounded to $701.00.
//   - Line 24: $1,001 + $701 = $1,702.00. Line 29: $1,702 - $1,001 = $701.00.
func TestPennsylvaniaMarriedFilingJointlyWithChildren(t *testing.T) {
	p1 := ilPerson("erin", 1988, 1, 20, false, false)
	p2 := ilPerson("finn", 1987, 7, 9, false, false)
	kids := []*returnsv1.Dependent{
		ilDependent("gail", 2017, 3, 3),
		ilDependent("hank", 2020, 10, 30),
	}
	in := paInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY,
		p1, p2, kids,
		&returnsv1.Documents{W2S: []*returnsv1.W2Document{
			paW2("w2_0", "erin", 25000_00, 768_00),
			paW2("w2_1", "finn", 7600_00, 233_00),
		}},
		&returnsv1.PennsylvaniaInputs{},
	)
	_, f := preparePA(t, in)

	if f.GetFilingStatus() != formpa40.FilingStatus_FILING_STATUS_MARRIED_FILING_JOINTLY {
		t.Errorf("filing status = %v, want married filing jointly", f.GetFilingStatus())
	}
	if f.GetTaxForgivenessStatus() != formpa40.TaxForgivenessStatus_TAX_FORGIVENESS_STATUS_MARRIED {
		t.Errorf("line 19a = %v, want married", f.GetTaxForgivenessStatus())
	}
	if f.GetTaxForgivenessDependents() != 2 {
		t.Errorf("line 19b = %d, want 2", f.GetTaxForgivenessDependents())
	}
	want(t, "Line 1a Gross compensation", f.GetGrossCompensation(), money.USCents(32600_00))
	want(t, "Line 9 Total PA taxable income", f.GetTotalTaxableIncome(), money.USCents(32600_00))
	want(t, "Line 12 PA tax liability", f.GetTaxLiability(), money.USCents(1001_00))
	want(t, "Line 13 PA tax withheld", f.GetTaxWithheld(), money.USCents(1001_00))
	want(t, "Line 20 Eligibility income", f.GetEligibilityIncome(), money.USCents(32600_00))
	want(t, "Line 21 Tax Forgiveness", f.GetTaxForgivenessCredit(), money.USCents(701_00))
	want(t, "Line 24 Total payments and credits", f.GetTotalPaymentsAndCredits(), money.USCents(1702_00))
	want(t, "Line 26 Tax due", f.GetTaxDue(), money.USCents(0))
	want(t, "Line 30 Refund", f.GetRefund(), money.USCents(701_00))
}

// 3. Retired couple: pension, interest and dividends, estimated payments.
//   - The $40,000 pension (Form 1099-R) is retirement income Pennsylvania
//     does not tax (PA-40 IN, Line 1a: exclude qualifying retirement
//     benefits), so line 1a is nothing.
//   - Line 2: $20,000.00 taxable interest (Form 1099-INT box 1).
//   - Line 3: $3,000 ordinary dividends + $1,000 capital gain distributions
//     (Form 1099-DIV boxes 1a and 2a) = $4,000.00.
//   - Line 9 and 11: $24,000.00.
//   - Line 12: $24,000 x 0.0307 = $736.80, rounded to $737.00.
//   - Line 15: $800.00 estimated payments; line 18: $800.00.
//   - Line 20: $24,000 eligibility income, above $15,250, the last column of
//     Eligibility Income Table 2 for no dependents, so line 21 is nothing.
//   - Line 24: $800.00. Line 29: $800 - $737 = $63.00, refunded.
func TestPennsylvaniaRetiredCouple(t *testing.T) {
	p1 := ilPerson("ivan", 1955, 2, 14, false, false)
	p2 := ilPerson("jane", 1957, 6, 6, false, false)
	docs := &returnsv1.Documents{
		RetirementDistributions: []*returnsv1.Form1099RDocument{{
			Id:     "1099r_0",
			Person: "ivan",
			Form: &form1099r.Form1099R{
				GrossDistribution: cents(40000_00),
				TaxableAmount:     cents(40000_00),
			},
		}},
		Interest: []*returnsv1.Form1099INTDocument{{
			Id:     "int_0",
			Person: "ivan",
			Form:   &form1099int.Form1099INT{InterestIncome: cents(20000_00)},
		}},
		Dividends: []*returnsv1.Form1099DIVDocument{{
			Id:     "div_0",
			Person: "jane",
			Form: &form1099div.Form1099DIV{
				OrdinaryDividends:            cents(3000_00),
				TotalCapitalGainDistribution: cents(1000_00),
			},
		}},
	}
	in := paInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY,
		p1, p2, nil, docs,
		&returnsv1.PennsylvaniaInputs{EstimatedTaxPayments: cents(800_00)},
	)
	_, f := preparePA(t, in)

	want(t, "Line 1a Gross compensation", f.GetGrossCompensation(), money.USCents(0))
	want(t, "Line 2 Interest income", f.GetInterestIncome(), money.USCents(20000_00))
	want(t, "Line 3 Dividends and capital gains distributions", f.GetDividendAndCapitalGainsDistributionsIncome(), money.USCents(4000_00))
	want(t, "Line 9 Total PA taxable income", f.GetTotalTaxableIncome(), money.USCents(24000_00))
	want(t, "Line 12 PA tax liability", f.GetTaxLiability(), money.USCents(737_00))
	want(t, "Line 18 Estimated payments", f.GetTotalEstimatedPaymentsAndCredits(), money.USCents(800_00))
	want(t, "Line 21 Tax Forgiveness", f.GetTaxForgivenessCredit(), money.USCents(0))
	want(t, "Line 24 Total payments and credits", f.GetTotalPaymentsAndCredits(), money.USCents(800_00))
	want(t, "Line 26 Tax due", f.GetTaxDue(), money.USCents(0))
	want(t, "Line 30 Refund", f.GetRefund(), money.USCents(63_00))
}

// 4. A loss in one class does not reduce another: a $5,000 business loss on
// line 4 is left out of line 9 (PA-40 line 9: do not add losses).
//   - Line 1a: $40,000.00; line 4: -$5,000.00; line 9: $40,000.00.
//   - Line 12: $40,000 x 0.0307 = $1,228.00. Line 13: $1,228.00. Line 26: nothing.
func TestPennsylvaniaLossDoesNotOffset(t *testing.T) {
	p := ilPerson("kim", 1980, 8, 8, false, false)
	in := paInputs(
		returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		p, nil, nil,
		&returnsv1.Documents{W2S: []*returnsv1.W2Document{paW2("w2_0", "kim", 40000_00, 1228_00)}},
		&returnsv1.PennsylvaniaInputs{BusinessIncomeOrLoss: cents(-5000_00)},
	)
	_, f := preparePA(t, in)

	want(t, "Line 4 Business loss", f.GetBusinessIncomeOrLoss(), money.USCents(-5000_00))
	want(t, "Line 9 Total PA taxable income", f.GetTotalTaxableIncome(), money.USCents(40000_00))
	want(t, "Line 12 PA tax liability", f.GetTaxLiability(), money.USCents(1228_00))
	want(t, "Line 26 Tax due", f.GetTaxDue(), money.USCents(0))
	want(t, "Line 29 Overpayment", f.GetOverpayment(), money.USCents(0))
}
