// Package preparer implements the stateless ReturnPreparer gRPC service backed
// by the taxlisp federal rule engine.
package preparer

import (
	"context"
	"encoding/xml"
	"fmt"
	"sort"
	"strings"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	taxlispv1 "github.com/nexteggapp/taxcalcs/taxlisp/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// noIncomeTaxStates lists US states without a general individual wage/income tax.
var noIncomeTaxStates = map[string]bool{
	"AK": true,
	"FL": true,
	"NH": true,
	"NV": true,
	"SD": true,
	"TN": true,
	"TX": true,
	"WA": true,
	"WY": true,
}

// Server implements returnsv1.ReturnPreparerServer using the embedded federal
// taxlisp rule engine.
type Server struct {
	returnsv1.UnimplementedReturnPreparerServer
	engine    *federal.Engine
	defnCites map[string][]*returnsv1.Citation
}

// NewServer constructs a ReturnPreparerServer initialized with the shared
// default federal taxlisp Engine.
func NewServer() (*Server, error) {
	eng, err := federal.DefaultEngine()
	if err != nil {
		return nil, err
	}
	return NewServerWithEngine(eng), nil
}

// NewServerWithEngine constructs a ReturnPreparerServer using the provided
// federal taxlisp Engine.
func NewServerWithEngine(eng *federal.Engine) *Server {
	return &Server{
		engine:    eng,
		defnCites: indexDefnCitations(eng.Module),
	}
}

// ListTaxYears returns the tax years supported by the engine and their
// rule-pack versions.
func (s *Server) ListTaxYears(_ context.Context, _ *returnsv1.ListTaxYearsRequest) (*returnsv1.ListTaxYearsResponse, error) {
	supportedNoTaxStates := []string{"AK", "FL", "NH", "NV", "SD", "TN", "TX", "WA", "WY"}
	return &returnsv1.ListTaxYearsResponse{
		TaxYears: []*returnsv1.SupportedTaxYear{
			{
				TaxYear: 2025,
				RulesVersion: &returnsv1.RulesVersion{
					TaxYear: 2025,
					Version: "2025.1.0",
					Status:  returnsv1.RulesStatus_RULES_STATUS_FINAL,
					PublishedOn: &commonv1.Date{
						Year:  2025,
						Month: 11,
						Day:   1,
					},
				},
				SupportedStates: supportedNoTaxStates,
			},
			{
				TaxYear: 2026,
				RulesVersion: &returnsv1.RulesVersion{
					TaxYear: 2026,
					Version: "2026.1.0",
					Status:  returnsv1.RulesStatus_RULES_STATUS_PRELIMINARY,
					PublishedOn: &commonv1.Date{
						Year:  2025,
						Month: 11,
						Day:   15,
					},
				},
				SupportedStates: supportedNoTaxStates,
			},
		},
	}, nil
}

// DescribeInputs inspects partial ReturnInputs and returns the annotated input
// fields with their population status and completion requirements.
func (s *Server) DescribeInputs(_ context.Context, req *returnsv1.DescribeInputsRequest) (*returnsv1.DescribeInputsResponse, error) {
	inputs := req.GetInputs()
	if inputs == nil {
		inputs = &returnsv1.ReturnInputs{}
	}
	var fields []*returnsv1.InputFieldDescriptor
	visited := make(map[string]bool)
	collectInputDescriptors(inputs.ProtoReflect(), inputs.ProtoReflect().Descriptor(), "", inputs, visited, &fields)
	return &returnsv1.DescribeInputsResponse{
		Fields: fields,
	}, nil
}

func collectInputDescriptors(
	msg protoreflect.Message,
	md protoreflect.MessageDescriptor,
	prefix string,
	root *returnsv1.ReturnInputs,
	visited map[string]bool,
	out *[]*returnsv1.InputFieldDescriptor,
) {
	fields := md.Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		path := string(fd.Name())
		if prefix != "" {
			path = prefix + "." + path
		}
		if visited[path] {
			continue
		}
		visited[path] = true

		populated := isFieldPopulated(msg, fd)
		if opts := fd.Options(); opts != nil && proto.HasExtension(opts, returnsv1.E_Input) {
			spec, ok := proto.GetExtension(opts, returnsv1.E_Input).(*returnsv1.InputSpec)
			if ok && spec != nil {
				*out = append(*out, &returnsv1.InputFieldDescriptor{
					Path:                  path,
					Spec:                  spec,
					Populated:             populated,
					RequiredForCompletion: isRequiredForCompletion(path, spec, root),
				})
			}
		}

		// Recurse into singular composite submessages so nested input fields
		// (e.g. federal.household.*, federal.elections.*) are discovered.
		if fd.Kind() == protoreflect.MessageKind && !fd.IsList() && !fd.IsMap() {
			subMD := fd.Message()
			pkg := string(subMD.FullName().Parent())
			if strings.HasPrefix(pkg, "nexteggapp.taxcalcs.returns.v1") {
				var subMsg protoreflect.Message
				if msg != nil && msg.IsValid() && msg.Has(fd) {
					subMsg = msg.Get(fd).Message()
				}
				collectInputDescriptors(subMsg, subMD, path, root, visited, out)
			}
		}
	}
}

func isFieldPopulated(msg protoreflect.Message, fd protoreflect.FieldDescriptor) bool {
	if msg == nil || !msg.IsValid() {
		return false
	}
	if fd.IsList() {
		return msg.Get(fd).List().Len() > 0
	}
	if fd.IsMap() {
		return msg.Get(fd).Map().Len() > 0
	}
	return msg.Has(fd)
}

func isRequiredForCompletion(path string, spec *returnsv1.InputSpec, root *returnsv1.ReturnInputs) bool {
	switch path {
	case "tax_year",
		"federal.household.marital_status",
		"federal.household.taxpayer.date_of_birth":
		return true
	case "federal.household.spouse.date_of_birth":
		ms := root.GetFederal().GetHousehold().GetMaritalStatus()
		return ms == returnsv1.MaritalStatus_MARITAL_STATUS_MARRIED
	default:
		return spec.GetCategory() == returnsv1.InputCategory_INPUT_CATEGORY_HOUSEHOLD && spec.GetRequiredWhen() == ""
	}
}

// Prepare computes the federal and requested state tax returns for the given
// ReturnInputs.
func (s *Server) Prepare(_ context.Context, req *returnsv1.PrepareRequest) (*returnsv1.PrepareResponse, error) {
	inputs := req.GetInputs()
	if inputs == nil {
		inputs = &returnsv1.ReturnInputs{}
	}
	outputs, err := s.engine.Prepare(inputs)
	if err != nil {
		return nil, fmt.Errorf("evaluating federal return: %w", err)
	}

	if len(inputs.GetStates()) > 0 {
		outputs.States = prepareStates(inputs)
	}

	if req.GetIncludeWorking() {
		outputs.Working = s.buildWorkingTrace(inputs, outputs)
	}

	return &returnsv1.PrepareResponse{
		Outputs: outputs,
	}, nil
}

func prepareStates(inputs *returnsv1.ReturnInputs) []*returnsv1.StateReturn {
	states := make([]*returnsv1.StateReturn, 0, len(inputs.GetStates()))
	for _, st := range inputs.GetStates() {
		code := strings.ToUpper(strings.TrimSpace(st.GetState()))
		if noIncomeTaxStates[code] {
			states = append(states, &returnsv1.StateReturn{
				State: code,
				Return: &returnsv1.StateReturn_Unsupported{
					Unsupported: &returnsv1.UnsupportedStateReturn{
						Status: returnsv1.ReturnStatus_RETURN_STATUS_NOT_REQUIRED,
						Summary: &returnsv1.Summary{
							Jurisdiction:                      code,
							TotalIncome:                       &commonv1.Money{},
							AdjustedGrossIncome:               &commonv1.Money{},
							TotalDeductions:                   &commonv1.Money{},
							TaxableIncome:                     &commonv1.Money{},
							TotalTax:                          &commonv1.Money{},
							TotalPaymentsAndRefundableCredits: &commonv1.Money{},
							Refund:                            &commonv1.Money{},
							AmountOwed:                        &commonv1.Money{},
						},
						Diagnostics: []*returnsv1.Diagnostic{
							{
								Code:     "STATE_NO_INCOME_TAX",
								Severity: returnsv1.Severity_SEVERITY_INFO,
								Message:  fmt.Sprintf("%s does not impose a general individual income tax.", code),
								FieldPaths: []string{
									"states",
								},
							},
						},
					},
				},
			})
			continue
		}
		states = append(states, &returnsv1.StateReturn{
			State: code,
			Return: &returnsv1.StateReturn_Unsupported{
				Unsupported: &returnsv1.UnsupportedStateReturn{
					Status: returnsv1.ReturnStatus_RETURN_STATUS_INCOMPLETE,
					Diagnostics: []*returnsv1.Diagnostic{
						{
							Code:     "STATE_RULES_NOT_YET_LOADED",
							Severity: returnsv1.Severity_SEVERITY_WARNING,
							Message:  fmt.Sprintf("State income tax calculator for %s is not yet loaded in this engine.", code),
							FieldPaths: []string{
								"states",
							},
						},
					},
				},
			},
		})
	}
	return states
}

// BuildSubmission validates ReturnInputs and assembles e-file transmission
// payloads for requested jurisdictions.
func (s *Server) BuildSubmission(ctx context.Context, req *returnsv1.BuildSubmissionRequest) (*returnsv1.BuildSubmissionResponse, error) {
	prepResp, err := s.Prepare(ctx, &returnsv1.PrepareRequest{
		Inputs:         req.GetInputs(),
		IncludeWorking: false,
	})
	if err != nil {
		return nil, err
	}
	outputs := prepResp.GetOutputs()
	inputs := req.GetInputs()
	if inputs == nil {
		inputs = &returnsv1.ReturnInputs{}
	}

	jurisdictions := req.GetJurisdictions()
	if len(jurisdictions) == 0 {
		jurisdictions = []string{"US"}
		for _, st := range inputs.GetStates() {
			if code := strings.ToUpper(strings.TrimSpace(st.GetState())); code != "" {
				jurisdictions = append(jurisdictions, code)
			}
		}
	}

	submissions := make([]*returnsv1.EFileSubmission, 0, len(jurisdictions))
	for _, j := range jurisdictions {
		code := strings.ToUpper(strings.TrimSpace(j))
		if code == "US" {
			submissions = append(submissions, buildFederalSubmission(inputs, outputs))
		} else {
			submissions = append(submissions, &returnsv1.EFileSubmission{
				Jurisdiction:    code,
				ReadyToTransmit: false,
				Diagnostics: []*returnsv1.Diagnostic{
					{
						Code:     "EFILE_STATE_UNSUPPORTED",
						Severity: returnsv1.Severity_SEVERITY_WARNING,
						Message:  fmt.Sprintf("State e-file transmission payload generation for %s is not supported yet.", code),
					},
				},
			})
		}
	}

	return &returnsv1.BuildSubmissionResponse{
		Outputs:     outputs,
		Submissions: submissions,
	}, nil
}

type mefSubmissionEnvelope struct {
	XMLName       xml.Name `xml:"Return"`
	ReturnVersion string   `xml:"returnVersion,attr"`
	TaxYear       int32    `xml:"ReturnHeader>TaxYr"`
	TaxpayerTIN   string   `xml:"ReturnHeader>Filer>PrimarySSN"`
	SpouseTIN     string   `xml:"ReturnHeader>Filer>SpouseSSN,omitempty"`
	FilingStatus  string   `xml:"ReturnData>IRS1040>IndividualReturnFilingStatusCd"`
	TotalIncome   int64    `xml:"ReturnData>IRS1040>TotalIncomeAmt"`
	AGI           int64    `xml:"ReturnData>IRS1040>AdjustedGrossIncomeAmt"`
	TaxableIncome int64    `xml:"ReturnData>IRS1040>TaxableIncomeAmt"`
	TotalTax      int64    `xml:"ReturnData>IRS1040>TotalTaxAmt"`
	TotalPayments int64    `xml:"ReturnData>IRS1040>TotalPaymentsAmt"`
	RefundAmt     int64    `xml:"ReturnData>IRS1040>RefundAmt,omitempty"`
	AmountOwedAmt int64    `xml:"ReturnData>IRS1040>AmountOwedAmt,omitempty"`
}

func buildFederalSubmission(inputs *returnsv1.ReturnInputs, outputs *returnsv1.ReturnOutputs) *returnsv1.EFileSubmission {
	var diags []*returnsv1.Diagnostic
	fed := outputs.GetFederal()
	if fed.GetStatus() != returnsv1.ReturnStatus_RETURN_STATUS_COMPLETE {
		diags = append(diags, &returnsv1.Diagnostic{
			Code:       "EFILE_RETURN_INCOMPLETE",
			Severity:   returnsv1.Severity_SEVERITY_ERROR,
			Message:    "Federal return status must be RETURN_STATUS_COMPLETE before building an e-file submission.",
			FieldPaths: []string{"federal.status"},
		})
	}

	hh := inputs.GetFederal().GetHousehold()
	tpTIN := extractTINValue(hh.GetTaxpayer().GetTin())
	if tpTIN == "" {
		diags = append(diags, &returnsv1.Diagnostic{
			Code:       "EFILE_MISSING_TAXPAYER_TIN",
			Severity:   returnsv1.Severity_SEVERITY_ERROR,
			Message:    "Primary taxpayer SSN/ITIN is required in federal.household.taxpayer.tin.number for IRS MeF e-file transmission.",
			FieldPaths: []string{"federal.household.taxpayer.tin.number"},
			Citations: []*returnsv1.Citation{
				{Authority: "26 U.S.C. § 6109(a)"},
			},
		})
	}

	spTIN := extractTINValue(hh.GetSpouse().GetTin())
	if hh.GetMaritalStatus() == returnsv1.MaritalStatus_MARITAL_STATUS_MARRIED && spTIN == "" {
		diags = append(diags, &returnsv1.Diagnostic{
			Code:       "EFILE_MISSING_SPOUSE_TIN",
			Severity:   returnsv1.Severity_SEVERITY_ERROR,
			Message:    "Spouse SSN/ITIN is required in federal.household.spouse.tin.number for married returns.",
			FieldPaths: []string{"federal.household.spouse.tin.number"},
			Citations: []*returnsv1.Citation{
				{Authority: "26 U.S.C. § 6109(a)"},
			},
		})
	}

	ready := len(diags) == 0
	var payload []byte
	if ready {
		summary := fed.GetSummary()
		env := mefSubmissionEnvelope{
			ReturnVersion: fmt.Sprintf("%d.1.0", outputs.GetHeader().GetTaxYear()),
			TaxYear:       outputs.GetHeader().GetTaxYear(),
			TaxpayerTIN:   tpTIN,
			SpouseTIN:     spTIN,
			FilingStatus:  summary.GetFilingStatus().String(),
			TotalIncome:   summary.GetTotalIncome().GetMinor() / 100,
			AGI:           summary.GetAdjustedGrossIncome().GetMinor() / 100,
			TaxableIncome: summary.GetTaxableIncome().GetMinor() / 100,
			TotalTax:      summary.GetTotalTax().GetMinor() / 100,
			TotalPayments: summary.GetTotalPaymentsAndRefundableCredits().GetMinor() / 100,
			RefundAmt:     summary.GetRefund().GetMinor() / 100,
			AmountOwedAmt: summary.GetAmountOwed().GetMinor() / 100,
		}
		if b, err := xml.MarshalIndent(env, "", "  "); err == nil {
			payload = b
		}
	}

	return &returnsv1.EFileSubmission{
		Jurisdiction:    "US",
		ReadyToTransmit: ready,
		Diagnostics:     diags,
		Payload:         payload,
	}
}

func extractTINValue(tin *returnsv1.TaxpayerID) string {
	if tin == nil {
		return ""
	}
	return strings.TrimSpace(tin.GetNumber())
}

// buildWorkingTrace constructs a cell-by-cell lineage trace for populated
// fields on ReturnOutputs.Federal.
func (s *Server) buildWorkingTrace(inputs *returnsv1.ReturnInputs, outputs *returnsv1.ReturnOutputs) *returnsv1.Working {
	var cells []*returnsv1.Cell
	var missingInputs []string
	if inputs.GetTaxYear() == 0 {
		missingInputs = append(missingInputs, "tax_year")
	}
	if inputs.GetFederal().GetHousehold().GetMaritalStatus() == returnsv1.MaritalStatus_MARITAL_STATUS_UNSPECIFIED {
		missingInputs = append(missingInputs, "federal.household.marital_status")
	}

	fed := outputs.GetFederal()
	if fed != nil {
		defaultCites := s.defnCites["prepare-federal-return"]
		if fed.GetSummary() != nil {
			collectMessageCells(fed.GetSummary().ProtoReflect(), "federal.summary", defaultCites, &cells)
		}
		if fed.GetForms() != nil {
			collectMessageCells(fed.GetForms().ProtoReflect(), "federal.forms", defaultCites, &cells)
		}
		if fed.GetWorkpapers() != nil {
			collectMessageCells(fed.GetWorkpapers().ProtoReflect(), "federal.workpapers", defaultCites, &cells)
		}
	}

	sort.Slice(cells, func(i, j int) bool {
		return cells[i].GetPath() < cells[j].GetPath()
	})

	return &returnsv1.Working{
		Cells:         cells,
		MissingInputs: missingInputs,
	}
}

func collectMessageCells(
	msg protoreflect.Message,
	prefix string,
	defaultCites []*returnsv1.Citation,
	out *[]*returnsv1.Cell,
) {
	if !msg.IsValid() {
		return
	}
	md := msg.Descriptor()
	fields := md.Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		if !msg.Has(fd) {
			continue
		}
		path := prefix + "." + string(fd.Name())
		if fd.IsList() || fd.IsMap() {
			continue
		}
		if fd.Kind() == protoreflect.MessageKind {
			subMsg := msg.Get(fd).Message()
			msgName := string(fd.Message().Name())
			if msgName == "Money" || msgName == "Quantity" || msgName == "Date" {
				*out = append(*out, &returnsv1.Cell{
					Path:      path,
					State:     returnsv1.CellState_CELL_STATE_KNOWN,
					Source:    classifyCellSource(path),
					Formula:   fmt.Sprintf("Computed by taxlisp federal rule pack (%s)", path),
					Citations: citationsForCellPath(path, defaultCites),
				})
				continue
			}
			collectMessageCells(subMsg, path, defaultCites, out)
			continue
		}
		*out = append(*out, &returnsv1.Cell{
			Path:      path,
			State:     returnsv1.CellState_CELL_STATE_KNOWN,
			Source:    classifyCellSource(path),
			Formula:   fmt.Sprintf("Computed by taxlisp federal rule pack (%s)", path),
			Citations: citationsForCellPath(path, defaultCites),
		})
	}
}

func classifyCellSource(path string) returnsv1.CellSource {
	switch {
	case strings.Contains(path, "standard_deduction"):
		return returnsv1.CellSource_CELL_SOURCE_STATUTORY_PARAMETER
	case strings.Contains(path, "deduction_amount") || strings.Contains(path, "filing_status"):
		return returnsv1.CellSource_CELL_SOURCE_ELECTION
	default:
		return returnsv1.CellSource_CELL_SOURCE_COMPUTED
	}
}

func citationsForCellPath(path string, fallback []*returnsv1.Citation) []*returnsv1.Citation {
	switch {
	case strings.Contains(path, "schedule_c"):
		return []*returnsv1.Citation{{Authority: "26 U.S.C. § 162"}, {Authority: "Schedule C (Form 1040) Instructions"}}
	case strings.Contains(path, "schedule_e"):
		return []*returnsv1.Citation{{Authority: "26 U.S.C. § 469"}, {Authority: "Schedule E (Form 1040) Instructions"}}
	case strings.Contains(path, "schedule_se"):
		return []*returnsv1.Citation{{Authority: "26 U.S.C. § 1401"}, {Authority: "26 U.S.C. § 1402"}}
	case strings.Contains(path, "form_8995"):
		return []*returnsv1.Citation{{Authority: "26 U.S.C. § 199A"}}
	case strings.Contains(path, "form_6251"):
		return []*returnsv1.Citation{{Authority: "26 U.S.C. § 55"}}
	case strings.Contains(path, "form_8606"):
		return []*returnsv1.Citation{{Authority: "26 U.S.C. § 408(d)(2)"}}
	case strings.Contains(path, "form_8812"):
		return []*returnsv1.Citation{{Authority: "26 U.S.C. § 24"}}
	case strings.Contains(path, "form_8863"):
		return []*returnsv1.Citation{{Authority: "26 U.S.C. § 25A"}}
	case strings.Contains(path, "form_2441"):
		return []*returnsv1.Citation{{Authority: "26 U.S.C. § 21"}}
	default:
		if len(fallback) > 0 {
			return fallback
		}
		return []*returnsv1.Citation{{Authority: "26 U.S.C. § 1"}, {Authority: "Form 1040 Instructions"}}
	}
}

func indexDefnCitations(mod *taxlispv1.ModuleFile) map[string][]*returnsv1.Citation {
	result := make(map[string][]*returnsv1.Citation)
	if mod == nil {
		return result
	}
	for _, expr := range mod.GetExpressions() {
		list := expr.GetList()
		if list == nil || len(list.GetExpressions()) < 3 {
			continue
		}
		elems := list.GetExpressions()
		head := elems[0].GetAtom().GetSymbol().GetName()
		if head != "def" && head != "defn" {
			continue
		}
		name := elems[1].GetAtom().GetSymbol().GetName()
		if name == "" {
			continue
		}
		for i := 2; i+1 < len(elems); i++ {
			if elems[i].GetAtom().GetKeyword() == "cite" {
				val := elems[i+1]
				if c := val.GetAtom().GetStringValue(); c != "" {
					result[name] = append(result[name], &returnsv1.Citation{Authority: c})
				} else if subList := val.GetList(); subList != nil {
					for _, item := range subList.GetExpressions() {
						if sc := item.GetAtom().GetStringValue(); sc != "" {
							result[name] = append(result[name], &returnsv1.Citation{Authority: sc})
						}
					}
				}
			}
		}
	}
	return result
}
