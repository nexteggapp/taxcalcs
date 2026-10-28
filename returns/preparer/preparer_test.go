package preparer_test

import (
	"context"
	"strings"
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/preparer"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	formw2v1 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
)

func money(dollars int64) *commonv1.Money {
	return &commonv1.Money{Currency: "USD", Minor: dollars * 100}
}

func TestListTaxYears(t *testing.T) {
	srv, err := preparer.NewServer()
	if err != nil {
		t.Fatalf("NewServer() failed: %v", err)
	}
	resp, err := srv.ListTaxYears(context.Background(), &returnsv1.ListTaxYearsRequest{})
	if err != nil {
		t.Fatalf("ListTaxYears() failed: %v", err)
	}
	if len(resp.GetTaxYears()) != 2 {
		t.Fatalf("len(TaxYears) = %d, want 2", len(resp.GetTaxYears()))
	}
	if resp.GetTaxYears()[0].GetTaxYear() != 2025 || resp.GetTaxYears()[1].GetTaxYear() != 2026 {
		t.Errorf("TaxYears = [%d, %d], want [2025, 2026]",
			resp.GetTaxYears()[0].GetTaxYear(),
			resp.GetTaxYears()[1].GetTaxYear())
	}
	if resp.GetTaxYears()[0].GetRulesVersion().GetVersion() != "2025.1.0" {
		t.Errorf("2025 RulesVersion = %q, want 2025.1.0", resp.GetTaxYears()[0].GetRulesVersion().GetVersion())
	}
}

func TestDescribeInputs(t *testing.T) {
	srv, err := preparer.NewServer()
	if err != nil {
		t.Fatalf("NewServer() failed: %v", err)
	}
	resp, err := srv.DescribeInputs(context.Background(), &returnsv1.DescribeInputsRequest{
		Inputs: &returnsv1.ReturnInputs{
			TaxYear: 2025,
			Federal: &returnsv1.Federal1040Inputs{
				Household: &returnsv1.Household{
					MaritalStatus: returnsv1.MaritalStatus_MARITAL_STATUS_NEVER_MARRIED,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("DescribeInputs() failed: %v", err)
	}
	if len(resp.GetFields()) == 0 {
		t.Fatal("DescribeInputs() returned 0 fields")
	}

	byPath := make(map[string]*returnsv1.InputFieldDescriptor)
	for _, f := range resp.GetFields() {
		byPath[f.GetPath()] = f
	}
	msField, ok := byPath["federal.household.marital_status"]
	if !ok {
		t.Fatal("missing field descriptor for federal.household.marital_status")
	}
	if !msField.GetPopulated() {
		t.Error("federal.household.marital_status Populated = false, want true")
	}
	if !msField.GetRequiredForCompletion() {
		t.Error("federal.household.marital_status RequiredForCompletion = false, want true")
	}

	dobField, ok := byPath["federal.household.taxpayer.date_of_birth"]
	if !ok {
		t.Fatal("missing field descriptor for federal.household.taxpayer.date_of_birth")
	}
	if dobField.GetPopulated() {
		t.Error("federal.household.taxpayer.date_of_birth Populated = true, want false")
	}
	if !dobField.GetRequiredForCompletion() {
		t.Error("federal.household.taxpayer.date_of_birth RequiredForCompletion = false, want true")
	}
}

func TestPrepareWithWorkingAndStates(t *testing.T) {
	srv, err := preparer.NewServer()
	if err != nil {
		t.Fatalf("NewServer() failed: %v", err)
	}
	resp, err := srv.Prepare(context.Background(), &returnsv1.PrepareRequest{
		IncludeWorking: true,
		Inputs: &returnsv1.ReturnInputs{
			TaxYear: 2025,
			Federal: &returnsv1.Federal1040Inputs{
				Household: &returnsv1.Household{
					MaritalStatus: returnsv1.MaritalStatus_MARITAL_STATUS_NEVER_MARRIED,
					Taxpayer: &returnsv1.Person{
						Id:          "you",
						DateOfBirth: &commonv1.Date{Year: 1990, Month: 4, Day: 10},
					},
				},
				Documents: &returnsv1.Documents{
					W2S: []*returnsv1.W2Document{
						{
							Id:     "w2-1",
							Person: "you",
							Form: &formw2v1.FormW2{
								WagesTipsOtherCompensation: money(100000),
								FederalTaxWithheld:         money(15000),
							},
						},
					},
				},
			},
			States: []*returnsv1.StateInputs{
				{State: "TX"},
				{State: "CA"},
			},
		},
	})
	if err != nil {
		t.Fatalf("Prepare() failed: %v", err)
	}
	out := resp.GetOutputs()
	if out.GetFederal().GetStatus() != returnsv1.ReturnStatus_RETURN_STATUS_COMPLETE {
		t.Errorf("Federal.Status = %v, want RETURN_STATUS_COMPLETE", out.GetFederal().GetStatus())
	}
	if len(out.GetStates()) != 2 {
		t.Fatalf("len(States) = %d, want 2", len(out.GetStates()))
	}
	if out.GetStates()[0].GetUnsupported().GetStatus() != returnsv1.ReturnStatus_RETURN_STATUS_NOT_REQUIRED {
		t.Errorf("TX status = %v, want RETURN_STATUS_NOT_REQUIRED", out.GetStates()[0].GetUnsupported().GetStatus())
	}
	if out.GetStates()[1].GetUnsupported().GetStatus() != returnsv1.ReturnStatus_RETURN_STATUS_INCOMPLETE {
		t.Errorf("CA status = %v, want RETURN_STATUS_INCOMPLETE", out.GetStates()[1].GetUnsupported().GetStatus())
	}

	working := out.GetWorking()
	if working == nil || len(working.GetCells()) == 0 {
		t.Fatal("Working trace was not populated when IncludeWorking = true")
	}
	foundAGI := false
	for _, cell := range working.GetCells() {
		if cell.GetPath() == "federal.forms.form_1040.adjusted_gross_income" {
			foundAGI = true
			if cell.GetState() != returnsv1.CellState_CELL_STATE_KNOWN {
				t.Errorf("AGI cell state = %v, want CELL_STATE_KNOWN", cell.GetState())
			}
			if len(cell.GetCitations()) == 0 {
				t.Error("expected citations on AGI working cell")
			}
		}
	}
	if !foundAGI {
		t.Error("did not find federal.forms.form_1040.adjusted_gross_income in Working.Cells")
	}
}

func TestBuildSubmission(t *testing.T) {
	srv, err := preparer.NewServer()
	if err != nil {
		t.Fatalf("NewServer() failed: %v", err)
	}

	inputs := &returnsv1.ReturnInputs{
		TaxYear: 2025,
		Federal: &returnsv1.Federal1040Inputs{
			Household: &returnsv1.Household{
				MaritalStatus: returnsv1.MaritalStatus_MARITAL_STATUS_NEVER_MARRIED,
				Taxpayer: &returnsv1.Person{
					Id:          "you",
					FirstName:   "Ada",
					LastName:    "Lovelace",
					DateOfBirth: &commonv1.Date{Year: 1990, Month: 12, Day: 10},
				},
			},
			Documents: &returnsv1.Documents{
				W2S: []*returnsv1.W2Document{
					{
						Id:     "w2-1",
						Person: "you",
						Form: &formw2v1.FormW2{
							WagesTipsOtherCompensation: money(85000),
							FederalTaxWithheld:         money(12000),
						},
					},
				},
			},
		},
	}

	// 1. Without TIN digits, BuildSubmission should report EFILE_MISSING_TAXPAYER_TIN.
	missingResp, err := srv.BuildSubmission(context.Background(), &returnsv1.BuildSubmissionRequest{
		Inputs: inputs,
	})
	if err != nil {
		t.Fatalf("BuildSubmission(missing TIN) failed: %v", err)
	}
	if len(missingResp.GetSubmissions()) != 1 {
		t.Fatalf("len(Submissions) = %d, want 1", len(missingResp.GetSubmissions()))
	}
	sub := missingResp.GetSubmissions()[0]
	if sub.GetReadyToTransmit() {
		t.Error("ReadyToTransmit = true when TIN is missing, want false")
	}
	if len(sub.GetDiagnostics()) == 0 || sub.GetDiagnostics()[0].GetCode() != "EFILE_MISSING_TAXPAYER_TIN" {
		t.Errorf("Diagnostics = %v, want EFILE_MISSING_TAXPAYER_TIN", sub.GetDiagnostics())
	}

	// 2. With TIN digits provided, BuildSubmission should succeed and emit XML payload.
	inputs.Federal.Household.Taxpayer.Tin = &returnsv1.TaxpayerID{
		Kind: returnsv1.TaxpayerIDKind_TAXPAYER_ID_KIND_SSN,
		Value: &returnsv1.TaxpayerID_Number{
			Number: "123456789",
		},
	}
	readyResp, err := srv.BuildSubmission(context.Background(), &returnsv1.BuildSubmissionRequest{
		Inputs: inputs,
	})
	if err != nil {
		t.Fatalf("BuildSubmission(with TIN) failed: %v", err)
	}
	readySub := readyResp.GetSubmissions()[0]
	if !readySub.GetReadyToTransmit() {
		t.Fatalf("ReadyToTransmit = false, want true (diags: %v)", readySub.GetDiagnostics())
	}
	xmlPayload := string(readySub.GetPayload())
	if !strings.Contains(xmlPayload, "<PrimarySSN>123456789</PrimarySSN>") {
		t.Errorf("XML payload missing PrimarySSN: %s", xmlPayload)
	}
	if !strings.Contains(xmlPayload, "<AdjustedGrossIncomeAmt>85000</AdjustedGrossIncomeAmt>") {
		t.Errorf("XML payload missing AdjustedGrossIncomeAmt: %s", xmlPayload)
	}
}
