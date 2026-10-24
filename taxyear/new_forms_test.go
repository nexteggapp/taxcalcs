package taxyear_test

import (
	"strings"
	"testing"

	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
	form1095a "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1095_a"
	form1098e "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1098_e"
	form1098t "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1098_t"
	form1099c "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_c"
	form1099misc "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_misc"
	form1099oid "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_oid"
	form1099q "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_q"
	form1099s "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_s"
	form1099sa "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_sa"
	form5498 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_5498"
	form5498sa "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_5498_sa"
	schedulek1 "github.com/nexteggapp/taxprotos/go/us/federal/v1/schedule_k1"
)

func docRef(id string) *factsv1.DocumentRef {
	return &factsv1.DocumentRef{Name: id}
}

// TestForm1099OIDInAccountModel verifies that Form 1099-OID attached to an
// AccountModel expands interest, treasury interest, and tax-exempt interest.
func TestForm1099OIDInAccountModel(t *testing.T) {
	f := others()
	oid := &form1099oid.Form1099OID{
		TaxYear:               2026,
		OriginalIssueDiscount: usd(1000),
		OtherPeriodicInterest: usd(200),
		MarketDiscount:        usd(50),
		AcquisitionPremium:    usd(30),
		OriginalIssueDiscountOnTreasuryObligations: usd(300),
		TaxExemptOid: usd(150),
	}
	f.GetShared().GetModel().Accounts = []*scenariosv1.AccountModel{{
		Id:   "bank-oid",
		Name: "Treasury Bank",
		Parts: []*scenariosv1.AccountPart{{
			Id: "p1",
			Part: &scenariosv1.AccountPart_FromForms_{
				FromForms: &scenariosv1.AccountPart_FromForms{
					Forms: []*scenariosv1.AccountPart_FromForms_Form{{
						Id:     "oid-1",
						Source: docRef("doc-oid-1"),
						Form:   &scenariosv1.AccountPart_FromForms_Form_Form_1099Oid{Form_1099Oid: oid},
					}},
				},
			},
		}},
		DefaultPart: &scenariosv1.AccountDefault{
			Id:   "bank-d",
			Rest: &scenariosv1.AccountDefault_PaceSoFar_{PaceSoFar: &scenariosv1.AccountDefault_PaceSoFar{}},
		},
	}}
	r, err := taxyear.Compute(f)
	if err != nil {
		t.Fatalf("Compute failed: %v", err)
	}
	res := r.Proto().GetScenarios()[0]
	if p := res.GetProblems(); len(p) > 0 {
		t.Fatalf("unexpected problems: %v", p)
	}
	// Taxable interest = (1000 + 200 + 50 - 30) + 300 = 1520
	taxableInterest := res.GetFederal().GetForm_1040().GetTaxableInterest().GetMinor()
	if taxableInterest != 152000 {
		t.Errorf("TaxableInterest = %d, want 152000 ($1520.00)", taxableInterest)
	}
	taxExempt := res.GetFederal().GetForm_1040().GetTaxExemptInterest().GetMinor()
	if taxExempt != 15000 {
		t.Errorf("TaxExemptInterest = %d, want 15000 ($150.00)", taxExempt)
	}
}

// TestForm1099MISCInBusinessModel verifies that Form 1099-MISC attached to a
// BusinessModel reconciles receipts properly.
func TestForm1099MISCInBusinessModel(t *testing.T) {
	f := others()
	misc := &form1099misc.Form1099MISC{
		TaxYear:     2026,
		Payer:       &commonv1.Party{Name: "ClientCorp"},
		OtherIncome: usd(6000),
		Rents:       usd(2000),
	}
	f.GetShared().GetModel().Businesses = []*scenariosv1.BusinessModel{{
		Id:     "biz-misc",
		Name:   "Consulting",
		Person: "you",
		Parts: []*scenariosv1.BusinessPart{{
			Id: "p1",
			Part: &scenariosv1.BusinessPart_FromForms_{
				FromForms: &scenariosv1.BusinessPart_FromForms{
					Forms: []*scenariosv1.BusinessPart_FromForms_Form{{
						Id:     "misc-1",
						Source: docRef("doc-misc-1"),
						Form:   &scenariosv1.BusinessPart_FromForms_Form_Form_1099Misc{Form_1099Misc: misc},
					}},
				},
			},
		}},
		DefaultPart: &scenariosv1.BusinessDefault{
			Id:   "biz-d",
			Rest: &scenariosv1.BusinessDefault_PaceSoFar_{PaceSoFar: &scenariosv1.BusinessDefault_PaceSoFar{}},
		},
	}}
	r, err := taxyear.Compute(f)
	if err != nil {
		t.Fatalf("Compute failed: %v", err)
	}
	res := r.Proto().GetScenarios()[0]
	if p := res.GetProblems(); len(p) > 0 {
		t.Fatalf("unexpected problems: %v", p)
	}
	bizIncome := res.GetFederal().GetSchedule_1().GetBusinessIncomeOrLoss().GetMinor()
	if bizIncome != 800000 {
		t.Errorf("BusinessIncome = %d, want 800000 ($8000.00)", bizIncome)
	}
}

// TestNewFormsInOtherIncomeModel verifies that Form 1099-MISC, 1099-SA, 1099-S,
// 1099-C, 1099-Q, and Schedule K-1 forms count toward taxable income and raise
// appropriate questions and NotIncluded notes.
func TestNewFormsInOtherIncomeModel(t *testing.T) {
	f := others()
	f.GetShared().GetModel().OtherIncome = []*scenariosv1.OtherIncomeModel{
		{
			Id:    "other-misc",
			Payer: "Misc Income",
			Parts: []*scenariosv1.OtherIncomePart{{
				Id: "p-misc",
				Part: &scenariosv1.OtherIncomePart_FromForms_{
					FromForms: &scenariosv1.OtherIncomePart_FromForms{
						Forms: []*scenariosv1.OtherIncomePart_FromForms_Form{{
							Id:     "f-misc",
							Source: docRef("ref-misc"),
							Form: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099Misc{
								Form_1099Misc: &form1099misc.Form1099MISC{
									TaxYear:            2026,
									OtherIncome:        usd(1500),
									FederalTaxWithheld: usd(150),
								},
							},
						}},
					},
				},
			}},
		},
		{
			Id:    "hsa-dist",
			Payer: "HSA Distribution",
			Parts: []*scenariosv1.OtherIncomePart{{
				Id: "p-sa",
				Part: &scenariosv1.OtherIncomePart_FromForms_{
					FromForms: &scenariosv1.OtherIncomePart_FromForms{
						Forms: []*scenariosv1.OtherIncomePart_FromForms_Form{{
							Id:     "f-sa",
							Source: docRef("ref-sa"),
							Form: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099Sa{
								Form_1099Sa: &form1099sa.Form1099SA{
									TaxYear:           2026,
									GrossDistribution: usd(800),
								},
							},
						}},
					},
				},
			}},
		},
		{
			Id:    "debt-cancel",
			Payer: "Canceled Debt",
			Parts: []*scenariosv1.OtherIncomePart{{
				Id: "p-c",
				Part: &scenariosv1.OtherIncomePart_FromForms_{
					FromForms: &scenariosv1.OtherIncomePart_FromForms{
						Forms: []*scenariosv1.OtherIncomePart_FromForms_Form{{
							Id:     "f-c",
							Source: docRef("ref-c"),
							Form: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099C{
								Form_1099C: &form1099c.Form1099C{
									TaxYear:                2026,
									AmountOfDebtDischarged: usd(2000),
								},
							},
						}},
					},
				},
			}},
		},
		{
			Id:    "k1-income",
			Payer: "Partnership K-1",
			Parts: []*scenariosv1.OtherIncomePart{{
				Id: "p-k1",
				Part: &scenariosv1.OtherIncomePart_FromForms_{
					FromForms: &scenariosv1.OtherIncomePart_FromForms{
						Forms: []*scenariosv1.OtherIncomePart_FromForms_Form{{
							Id:     "f-k1",
							Source: docRef("ref-k1"),
							Form: &scenariosv1.OtherIncomePart_FromForms_Form_ScheduleK1_1065{
								ScheduleK1_1065: &schedulek1.ScheduleK1Form1065{
									TaxYear:                2026,
									OrdinaryBusinessIncome: usd(3500),
								},
							},
						}},
					},
				},
			}},
		},
		{
			Id:    "real-estate",
			Payer: "Real Estate Sale",
			Parts: []*scenariosv1.OtherIncomePart{{
				Id: "p-s",
				Part: &scenariosv1.OtherIncomePart_FromForms_{
					FromForms: &scenariosv1.OtherIncomePart_FromForms{
						Forms: []*scenariosv1.OtherIncomePart_FromForms_Form{{
							Id:     "f-s",
							Source: docRef("ref-s"),
							Form: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099S{
								Form_1099S: &form1099s.Form1099S{
									TaxYear:       2026,
									GrossProceeds: usd(50000),
								},
							},
						}},
					},
				},
			}},
		},
		{
			Id:    "edu-dist",
			Payer: "529 Distribution",
			Parts: []*scenariosv1.OtherIncomePart{{
				Id: "p-q",
				Part: &scenariosv1.OtherIncomePart_FromForms_{
					FromForms: &scenariosv1.OtherIncomePart_FromForms{
						Forms: []*scenariosv1.OtherIncomePart_FromForms_Form{{
							Id:     "f-q",
							Source: docRef("ref-q"),
							Form: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099Q{
								Form_1099Q: &form1099q.Form1099Q{
									TaxYear:           2026,
									GrossDistribution: usd(5000),
									Earnings:          usd(1000),
								},
							},
						}},
					},
				},
			}},
		},
	}
	r, err := taxyear.Compute(f)
	if err != nil {
		t.Fatalf("Compute failed: %v", err)
	}
	res := r.Proto().GetScenarios()[0]
	// Withholding from 1099-MISC should be $150
	withheld := res.GetFederal().GetForm_1040().GetWithheldFrom_1099().GetMinor()
	if withheld != 15000 {
		t.Errorf("WithheldFrom_1099 = %d, want 15000 ($150.00)", withheld)
	}

	// Verify questions were asked for HSA and 1099-C
	hasHsaQuestion := false
	hasDebtQuestion := false
	for _, q := range res.GetQuestions() {
		if strings.Contains(q.GetQuestion().GetText(), "qualified medical expenses") {
			hasHsaQuestion = true
		}
		if strings.Contains(q.GetQuestion().GetText(), "insolvency") {
			hasDebtQuestion = true
		}
	}
	if !hasHsaQuestion {
		t.Errorf("Expected HSA question not found in %v", res.GetQuestions())
	}
	if !hasDebtQuestion {
		t.Errorf("Expected canceled debt question not found in %v", res.GetQuestions())
	}
}

// TestNewFormsInRetirementModel verifies that Form 5498 and Form 5498-SA
// contributions are tracked in RetirementProjection.
func TestNewFormsInRetirementModel(t *testing.T) {
	f := others()
	f.GetShared().GetModel().Retirement = []*scenariosv1.RetirementModel{{
		Id:     "ret-1",
		Person: "you",
		Parts: []*scenariosv1.RetirementPart{{
			Id: "p-forms",
			Part: &scenariosv1.RetirementPart_FromForms_{
				FromForms: &scenariosv1.RetirementPart_FromForms{
					Forms: []*scenariosv1.RetirementPart_FromForms_Form{
						{
							Id:     "f-5498",
							Source: docRef("ref-5498"),
							Form: &scenariosv1.RetirementPart_FromForms_Form_Form_5498{
								Form_5498: &form5498.Form5498{
									TaxYear:              2026,
									IraContributions:     usd(3000),
									RothIraContributions: usd(2000),
								},
							},
						},
						{
							Id:     "f-5498sa",
							Source: docRef("ref-5498sa"),
							Form: &scenariosv1.RetirementPart_FromForms_Form_Form_5498Sa{
								Form_5498Sa: &form5498sa.Form5498SA{
									TaxYear:                      2026,
									TotalContributionsMadeInYear: usd(1500),
								},
							},
						},
					},
				},
			},
		}},
	}}
	r, err := taxyear.Compute(f)
	if err != nil {
		t.Fatalf("Compute failed: %v", err)
	}
	res := r.Proto().GetScenarios()[0]
	retProj := res.GetProjection().GetRetirement()[0]
	if got := retProj.GetYear().GetItems().GetTraditionalIra().GetMinor(); got != 300000 {
		t.Errorf("TraditionalIra = %d, want 300000", got)
	}
	if got := retProj.GetYear().GetItems().GetRothIra().GetMinor(); got != 200000 {
		t.Errorf("RothIra = %d, want 200000", got)
	}
	if got := retProj.GetYear().GetItems().GetHsa().GetMinor(); got != 150000 {
		t.Errorf("Hsa = %d, want 150000", got)
	}
}

// TestUnassignedDocuments verifies that unassigned documents are never dropped
// silently: questions are asked, income and withholding are counted, and
// informational forms generate NotIncluded notes.
func TestUnassignedDocuments(t *testing.T) {
	f := others()
	f.GetShared().GetModel().OtherIncome = nil
	f.GetShared().GetFacts().Unassigned = []*factsv1.Document{
		{
			Id: "unassigned-misc",
			Form: &factsv1.Document_Form_1099Misc{
				Form_1099Misc: &form1099misc.Form1099MISC{
					TaxYear:            2026,
					OtherIncome:        usd(4000),
					FederalTaxWithheld: usd(400),
				},
			},
		},
		{
			Id: "unassigned-1095a",
			Form: &factsv1.Document_Form_1095A{
				Form_1095A: &form1095a.Form1095A{
					TaxYear: 2026,
				},
			},
		},
		{
			Id: "unassigned-1098e",
			Form: &factsv1.Document_Form_1098E{
				Form_1098E: &form1098e.Form1098E{
					TaxYear:                     2026,
					StudentLoanInterestReceived: usd(500),
				},
			},
		},
		{
			Id: "unassigned-1098t",
			Form: &factsv1.Document_Form_1098T{
				Form_1098T: &form1098t.Form1098T{
					TaxYear:                             2026,
					PaymentsReceivedForQualifiedTuition: usd(8000),
				},
			},
		},
	}
	r, err := taxyear.Compute(f)
	if err != nil {
		t.Fatalf("Compute failed: %v", err)
	}
	res := r.Proto().GetScenarios()[0]

	// Verify question was asked for unassigned 1099-MISC and 1095-A
	hasMiscQuestion := false
	has1095Question := false
	for _, q := range res.GetQuestions() {
		if strings.Contains(q.GetQuestion().GetText(), "Form 1099-MISC") {
			hasMiscQuestion = true
		}
		if strings.Contains(q.GetQuestion().GetText(), "Form 1095-A") {
			has1095Question = true
		}
	}
	if !hasMiscQuestion {
		t.Errorf("Expected question for unassigned Form 1099-MISC not found")
	}
	if !has1095Question {
		t.Errorf("Expected question for unassigned Form 1095-A not found")
	}

	// Verify unassigned income was included
	withheld := res.GetFederal().GetForm_1040().GetWithheldFrom_1099().GetMinor()
	if withheld != 40000 {
		t.Errorf("WithheldFrom_1099 = %d, want 40000 ($400.00)", withheld)
	}

	// Verify NotIncluded notes exist for 1095-A, 1098-E, 1098-T
	has1095Note := false
	has1098ENote := false
	has1098TNote := false
	for _, ni := range res.GetNotIncluded() {
		text := ni.GetText().GetText()
		if strings.Contains(text, "Form 1095-A") {
			has1095Note = true
		}
		if strings.Contains(text, "Form 1098-E") {
			has1098ENote = true
		}
		if strings.Contains(text, "Form 1098-T") {
			has1098TNote = true
		}
	}
	if !has1095Note {
		t.Errorf("Expected NotIncluded note for Form 1095-A")
	}
	if !has1098ENote {
		t.Errorf("Expected NotIncluded note for Form 1098-E")
	}
	if !has1098TNote {
		t.Errorf("Expected NotIncluded note for Form 1098-T")
	}
}
