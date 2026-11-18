package states_test

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
	formin111 "github.com/nexteggapp/taxprotos/go/us/state/vt/v1/form_in111"
)

// There is no Go income tax calculator for Vermont, so these households are
// worked by hand from the 2025 Form IN-111 and its instructions ("Instr."
// below, pages as printed).

func vtInputs(
	status returnsv1.FilingStatusElection,
	taxpayer, spouse *returnsv1.Person,
	dependents []*returnsv1.Dependent,
	w2s []*formw2.FormW2,
	vtIn *returnsv1.VermontInputs,
) *returnsv1.ReturnInputs {
	marital := returnsv1.MaritalStatus_MARITAL_STATUS_NEVER_MARRIED
	if spouse != nil {
		marital = returnsv1.MaritalStatus_MARITAL_STATUS_MARRIED
	}
	var docs []*returnsv1.W2Document
	for i, w := range w2s {
		id := taxpayer.Id
		if i == 1 && spouse != nil {
			id = spouse.Id
		}
		docs = append(docs, &returnsv1.W2Document{Id: w2Id(i), Person: id, Form: w})
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
		States: []*returnsv1.StateInputs{
			{State: "VT", Inputs: &returnsv1.StateInputs_Vt{Vt: vtIn}},
		},
	}
}

func vtW2(wages, fedWithheld, vtWithheld int64) *formw2.FormW2 {
	return &formw2.FormW2{
		WagesTipsOtherCompensation: cents(wages),
		FederalTaxWithheld:         cents(fedWithheld),
		StateTaxWithholding: []*commonv1.StateTaxWithholding{
			{State: "VT", StateIncome: cents(wages), TaxWithheld: cents(vtWithheld)},
		},
	}
}

func prepareVermont(t *testing.T, in *returnsv1.ReturnInputs) (*returnsv1.VermontReturn, *formin111.FormIN111) {
	t.Helper()
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatal(err)
	}
	out, err := eng.Prepare(in)
	if err != nil {
		t.Fatal(err)
	}
	ret := out.GetStates()[0].GetVt()
	if ret == nil {
		t.Fatal("expected a Vermont return")
	}
	if ret.GetStatus() != returnsv1.ReturnStatus_RETURN_STATUS_COMPLETE {
		t.Errorf("expected a complete return, got %v: %v", ret.GetStatus(), ret.GetDiagnostics())
	}
	return ret, ret.GetForms().GetFormIn111()
}

// A single filer with wages and charitable gifts, taxed from the tax table.
//
//	Line 1  federal AGI: wages                                  $55,000
//	Line 3  no modifications                                    $55,000
//	Line 4  standard deduction, single (Instr. p. 6, line 4)     $7,650
//	Line 5e one exemption x $5,300 (form line 5e)                $5,300
//	Line 6  $7,650 + $5,300                                     $12,950
//	Line 7  $55,000 - $12,950                                   $42,050
//	Line 8  tax table, row 42,000-42,100, single (Instr. p. 13)  $1,409
//	        (the midpoint $42,050 x 3.35% = $1,408.68)
//	Line 11 contributions $2,000; line 12 x 5% = $100; line 13   $100
//	Line 14 $1,409 - $100                                        $1,309
//	Line 23 total Vermont taxes                                  $1,309
//	Line 26a withheld                                            $1,800
//	Line 29 refund: $1,800 - $1,309                                $491
func TestVermontSingle(t *testing.T) {
	p := ilPerson("ann", 1990, 4, 2, false, false)
	in := vtInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		p, nil, nil,
		[]*formw2.FormW2{vtW2(55000_00, 5000_00, 1800_00)},
		&returnsv1.VermontInputs{CharitableContributions: cents(2000_00)})
	ret, f := prepareVermont(t, in)

	want(t, "Line 1", f.GetFederalAdjustedGrossIncome(), money.USCents(55000_00))
	want(t, "Line 4", f.GetStandardDeduction(), money.USCents(7650_00))
	want(t, "Line 5e", f.GetPersonalExemptionAmount(), money.USCents(5300_00))
	want(t, "Line 7", f.GetTaxableIncome(), money.USCents(42050_00))
	want(t, "Line 8", f.GetIncomeTax(), money.USCents(1409_00))
	want(t, "Line 13", f.GetCharitableContributionCredit(), money.USCents(100_00))
	want(t, "Line 14", f.GetIncomeTaxAfterCharitableCredit(), money.USCents(1309_00))
	want(t, "Line 23", f.GetTotalTaxes(), money.USCents(1309_00))
	want(t, "Line 26f", f.GetTotalPayments(), money.USCents(1800_00))
	want(t, "Line 29", f.GetRefund(), money.USCents(491_00))
	want(t, "Line 32", f.GetAmountDue(), money.USCents(0))
	want(t, "summary tax", ret.GetSummary().GetTotalTax(), money.USCents(1309_00))
}

// Married filing jointly with two children, taxed from rate schedule Y-1.
//
//	Line 1  federal AGI: $90,000 + $40,000 wages               $130,000
//	Line 4  standard deduction, joint (Instr. p. 6, line 4)      $15,300
//	Line 5d 1 + 1 + 2 dependents = 4; line 5e 4 x $5,300         $21,200
//	Line 6  $15,300 + $21,200                                    $36,500
//	Line 7  $130,000 - $36,500                                   $93,500
//	Line 8  Schedule Y-1 (Instr. p. 13): over $82,500, base $2,764
//	        plus 6.6% of $11,000 = $726                           $3,490
//	        (the same steps as the instructions' worked example)
//	Line 23 total Vermont taxes                                   $3,490
//	Line 26a withheld $4,000 + $1,500                             $5,500
//	Line 29 refund: $5,500 - $3,490                               $2,010
func TestVermontMarriedFilingJointly(t *testing.T) {
	p1 := ilPerson("cal", 1985, 3, 15, false, false)
	p2 := ilPerson("dee", 1986, 8, 20, false, false)
	kids := []*returnsv1.Dependent{
		ilDependent("eve", 2015, 6, 1),
		ilDependent("fay", 2018, 9, 9),
	}
	in := vtInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY,
		p1, p2, kids,
		[]*formw2.FormW2{vtW2(90000_00, 8000_00, 4000_00), vtW2(40000_00, 3000_00, 1500_00)},
		&returnsv1.VermontInputs{})
	_, f := prepareVermont(t, in)

	want(t, "Line 1", f.GetFederalAdjustedGrossIncome(), money.USCents(130000_00))
	want(t, "Line 4", f.GetStandardDeduction(), money.USCents(15300_00))
	if f.GetTotalExemptions() != 4 {
		t.Errorf("Line 5d = %d, want 4", f.GetTotalExemptions())
	}
	want(t, "Line 5e", f.GetPersonalExemptionAmount(), money.USCents(21200_00))
	want(t, "Line 7", f.GetTaxableIncome(), money.USCents(93500_00))
	want(t, "Line 8", f.GetIncomeTax(), money.USCents(3490_00))
	want(t, "Line 23", f.GetTotalTaxes(), money.USCents(3490_00))
	want(t, "Line 26f", f.GetTotalPayments(), money.USCents(5500_00))
	want(t, "Line 29", f.GetRefund(), money.USCents(2010_00))
	want(t, "Line 32", f.GetAmountDue(), money.USCents(0))
}

// A single filer over 65 with federal AGI over $150,000, where the 3%
// minimum tax applies and the charitable credit is capped.
//
//	Line 1  federal AGI: wages                                 $180,000
//	Line 2  subtractions on Schedule IN-112                    -$60,000
//	Line 3  $180,000 - $60,000                                 $120,000
//	Line 4  single, one age box (Instr. p. 6, line 4 chart)      $8,900
//	Line 5e one exemption                                        $5,300
//	Line 6  $8,900 + $5,300                                     $14,200
//	Line 7  $120,000 - $14,200                                 $105,800
//	Line 8  Schedule X: over $75,000, base $3,345 plus 6.6% of
//	        $30,800 = $2,032.80, so $5,378; but line 1 is over
//	        $150,000, so the greater of that and 3% of $180,000
//	        (Instr. p. 6, line 8)                                $5,400
//	Line 11 contributions $30,000; line 12 $1,500; line 13 the
//	        $1,000 limit                                         $1,000
//	Line 14 $5,400 - $1,000                                      $4,400
//	Line 26a withheld                                            $7,000
//	Line 29 refund: $7,000 - $4,400                              $2,600
func TestVermontHighIncomeMinimumTax(t *testing.T) {
	p := ilPerson("gus", 1955, 1, 10, false, false)
	in := vtInputs(returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
		p, nil, nil,
		[]*formw2.FormW2{vtW2(180000_00, 30000_00, 7000_00)},
		&returnsv1.VermontInputs{
			OtherSubtractions:       cents(60000_00),
			CharitableContributions: cents(30000_00),
		})
	_, f := prepareVermont(t, in)

	want(t, "Line 2", f.GetNetModifications(), money.USCents(-60000_00))
	want(t, "Line 3", f.GetModifiedAdjustedGrossIncome(), money.USCents(120000_00))
	want(t, "Line 4", f.GetStandardDeduction(), money.USCents(8900_00))
	want(t, "Line 7", f.GetTaxableIncome(), money.USCents(105800_00))
	want(t, "Line 8", f.GetIncomeTax(), money.USCents(5400_00))
	want(t, "Line 13", f.GetCharitableContributionCredit(), money.USCents(1000_00))
	want(t, "Line 14", f.GetIncomeTaxAfterCharitableCredit(), money.USCents(4400_00))
	want(t, "Line 29", f.GetRefund(), money.USCents(2600_00))
	want(t, "Line 32", f.GetAmountDue(), money.USCents(0))
}
