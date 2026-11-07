package golden

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/preparer"
	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	"github.com/nexteggapp/taxcalcs/taxlisp/eval"
	"github.com/nexteggapp/taxcalcs/taxlisp/parser"
	"github.com/nexteggapp/taxcalcs/taxlisp/types"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// returnsCase is one ReturnInputs and what the federal engine prepares from
// it: Engine.Prepare's ReturnOutputs and Engine.PrepareFederal's
// FederalReturn, or the error text. FederalSame is set, and Federal left
// empty, when PrepareFederal's return equals Prepare's outputs.federal.
type returnsCase struct {
	Name         string `json:"name"`
	Inputs       string `json:"inputs"`
	Outputs      string `json:"outputs"`
	OutputsError string `json:"outputs_error"`
	FederalSame  bool   `json:"federal_same"`
	Federal      string `json:"federal"`
	FederalError string `json:"federal_error"`
}

// rfRecorder is the federal engine with every input it prepares recorded.
type rfRecorder struct {
	*federal.Engine
}

var (
	rfCurrent string
	rfCases   []returnsCase
	rfSeen    = map[string]bool{}
)

func rfEngine() (*rfRecorder, error) {
	eng, err := federal.DefaultEngine()
	if err != nil {
		return nil, err
	}
	return &rfRecorder{Engine: eng}, nil
}

// rfRecord prepares in both ways and keeps the result, once per distinct
// input.
func rfRecord(t *testing.T, name string, in *returnsv1.ReturnInputs) {
	key := encode(t, in)
	if rfSeen[key] {
		return
	}
	rfSeen[key] = true
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatal(err)
	}
	c := returnsCase{Name: name, Inputs: key}
	out, err := eng.Prepare(in)
	c.OutputsError = retErrText(err)
	if err == nil {
		c.Outputs = encode(t, out)
	}
	fed, err := eng.PrepareFederal(in)
	c.FederalError = retErrText(err)
	if err == nil {
		if out != nil && proto.Equal(fed, out.GetFederal()) {
			c.FederalSame = true
		} else {
			c.Federal = encode(t, fed)
		}
	}
	rfCases = append(rfCases, c)
}

func (r *rfRecorder) record(in *returnsv1.ReturnInputs) {
	if in == nil {
		in = &returnsv1.ReturnInputs{}
	}
	rfPending = append(rfPending, rfInput{rfCurrent, proto.Clone(in).(*returnsv1.ReturnInputs)})
}

type rfInput struct {
	name string
	in   *returnsv1.ReturnInputs
}

var rfPending []rfInput

func (r *rfRecorder) Prepare(in *returnsv1.ReturnInputs) (*returnsv1.ReturnOutputs, error) {
	r.record(in)
	return r.Engine.Prepare(in)
}

func (r *rfRecorder) PrepareFederal(in *returnsv1.ReturnInputs) (*returnsv1.FederalReturn, error) {
	r.record(in)
	return r.Engine.PrepareFederal(in)
}

// TestReturnsGolden writes returns/federal.json from every return the
// federal rule tests prepare, the first-slice fixture, and each of those
// returns for the other supported tax year; and returns/preparer.json from
// the ReturnPreparer service's methods.
func TestReturnsGolden(t *testing.T) {
	for _, tc := range rfTests {
		rfCurrent = tc.name
		t.Run(tc.name, tc.fn)
	}
	rfPending = append(rfPending, rfInput{"FirstSlice", rfFirstSlice(t)})

	base := rfPending
	for _, p := range base {
		rfRecord(t, p.name, p.in)
	}
	// The same returns for the other year, so both years' parameters run
	// through every rule file.
	for _, p := range base {
		other := proto.Clone(p.in).(*returnsv1.ReturnInputs)
		switch other.GetTaxYear() {
		case 2025:
			other.TaxYear = 2026
		case 2026:
			other.TaxYear = 2025
		default:
			continue
		}
		rfRecord(t, p.name+"/OtherYear", other)
	}
	check(t, "returns/federal.json", rfCases)

	check(t, "returns/preparer.json", preparerCases(t))
}

func rfFirstSlice(t *testing.T) *returnsv1.ReturnInputs {
	b, err := os.ReadFile("../returns/v1/testdata/first_slice.jsonc")
	if err != nil {
		t.Fatal(err)
	}
	in := &returnsv1.ReturnInputs{}
	if err := (protojson.UnmarshalOptions{}).Unmarshal(rfStripComments(b), in); err != nil {
		t.Fatal(err)
	}
	return in
}

// rfStripComments drops // and /* */ comments outside JSON strings.
func rfStripComments(in []byte) []byte {
	var out bytes.Buffer
	for i := 0; i < len(in); i++ {
		switch {
		case in[i] == '"':
			end := len(in)
			for j := i + 1; j < len(in); j++ {
				if in[j] == '\\' {
					j++
					continue
				}
				if in[j] == '"' {
					end = j + 1
					break
				}
			}
			out.Write(in[i:end])
			i = end - 1
		case bytes.HasPrefix(in[i:], []byte("//")):
			for i < len(in) && in[i] != '\n' {
				i++
			}
			if i < len(in) {
				out.WriteByte('\n')
			}
		case bytes.HasPrefix(in[i:], []byte("/*")):
			end := bytes.Index(in[i+2:], []byte("*/"))
			if end < 0 {
				return out.Bytes()
			}
			i += 2 + end + 1
		default:
			out.WriteByte(in[i])
		}
	}
	return out.Bytes()
}

// preparerCase is one ReturnPreparer call: the method, its encoded request,
// and its encoded response or error text.
type preparerCase struct {
	Name     string `json:"name"`
	Method   string `json:"method"`
	Request  string `json:"request"`
	Response string `json:"response"`
	Error    string `json:"error"`
}

func ppMoney(dollars int64) *commonv1.Money {
	return &commonv1.Money{Currency: "USD", Minor: dollars * 100}
}

func ppInputs(status returnsv1.MaritalStatus, tin, spouseTIN string, wages, withheld int64, states ...string) *returnsv1.ReturnInputs {
	hh := &returnsv1.Household{
		MaritalStatus: status,
		Taxpayer: &returnsv1.Person{
			Id:          "you",
			FirstName:   "Ada",
			LastName:    "Lovelace",
			DateOfBirth: &commonv1.Date{Year: 1990, Month: 12, Day: 10},
		},
	}
	if tin != "" {
		hh.Taxpayer.Tin = &returnsv1.TaxpayerID{
			Kind:  returnsv1.TaxpayerIDKind_TAXPAYER_ID_KIND_SSN,
			Value: &returnsv1.TaxpayerID_Number{Number: tin},
		}
	}
	if status == returnsv1.MaritalStatus_MARITAL_STATUS_MARRIED {
		hh.LivedWithSpouseAnyTime = proto.Bool(true)
		hh.Spouse = &returnsv1.Person{
			Id:          "spouse",
			FirstName:   "Charles",
			LastName:    "Babbage",
			DateOfBirth: &commonv1.Date{Year: 1989, Month: 2, Day: 3},
		}
		if spouseTIN != "" {
			hh.Spouse.Tin = &returnsv1.TaxpayerID{Value: &returnsv1.TaxpayerID_Number{Number: spouseTIN}}
		}
	}
	in := &returnsv1.ReturnInputs{
		TaxYear: 2025,
		Federal: &returnsv1.Federal1040Inputs{
			Household: hh,
			Documents: &returnsv1.Documents{
				W2S: []*returnsv1.W2Document{{
					Id:     "w2-1",
					Person: "you",
					Form: &formw2.FormW2{
						WagesTipsOtherCompensation: ppMoney(wages),
						FederalTaxWithheld:         ppMoney(withheld),
					},
				}},
			},
		},
	}
	for _, s := range states {
		in.States = append(in.States, &returnsv1.StateInputs{State: s})
	}
	return in
}

func preparerCases(t *testing.T) []preparerCase {
	srv, err := preparer.NewServer()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	var cases []preparerCase
	add := func(name, method string, req, resp proto.Message, err error) {
		c := preparerCase{Name: name, Method: method, Request: encode(t, req), Error: retErrText(err)}
		if err == nil {
			c.Response = encode(t, resp)
		}
		cases = append(cases, c)
	}

	{
		req := &returnsv1.ListTaxYearsRequest{}
		resp, err := srv.ListTaxYears(ctx, req)
		add("ListTaxYears", "ListTaxYears", req, resp, err)
	}

	single := returnsv1.MaritalStatus_MARITAL_STATUS_NEVER_MARRIED
	married := returnsv1.MaritalStatus_MARITAL_STATUS_MARRIED
	describe := []struct {
		name string
		in   *returnsv1.ReturnInputs
	}{
		{"Empty", nil},
		{"MaritalStatusOnly", &returnsv1.ReturnInputs{
			TaxYear: 2025,
			Federal: &returnsv1.Federal1040Inputs{
				Household: &returnsv1.Household{MaritalStatus: single},
			},
		}},
		{"Married", ppInputs(married, "123456789", "", 120000, 15000, "TX")},
		{"FirstSlice", rfFirstSlice(t)},
	}
	for _, d := range describe {
		req := &returnsv1.DescribeInputsRequest{Inputs: d.in}
		resp, err := srv.DescribeInputs(ctx, req)
		add("DescribeInputs/"+d.name, "DescribeInputs", req, resp, err)
	}

	prepare := []struct {
		name    string
		working bool
		in      *returnsv1.ReturnInputs
	}{
		{"Empty", false, nil},
		{"EmptyWithWorking", true, nil},
		{"WorkingAndStates", true, ppInputs(single, "", "", 100000, 15000, "TX", "CA")},
		{"StatesLowercase", false, ppInputs(single, "", "", 50000, 4000, " wa ", "ny")},
		{"FirstSliceWithWorking", true, rfFirstSlice(t)},
	}
	for _, p := range prepare {
		req := &returnsv1.PrepareRequest{Inputs: p.in, IncludeWorking: p.working}
		resp, err := srv.Prepare(ctx, req)
		add("Prepare/"+p.name, "Prepare", req, resp, err)
	}

	submit := []struct {
		name          string
		in            *returnsv1.ReturnInputs
		jurisdictions []string
	}{
		{"MissingTIN", ppInputs(single, "", "", 85000, 12000), nil},
		{"Ready", ppInputs(single, "123456789", "", 85000, 12000), nil},
		{"ReadyOwes", ppInputs(single, " 987654321 ", "", 85000, 2000), nil},
		{"MarriedMissingSpouseTIN", ppInputs(married, "123456789", "", 150000, 20000), nil},
		{"MarriedReady", ppInputs(married, "123456789", "987654321", 150000, 20000, "TX", "CA"), nil},
		{"Jurisdictions", ppInputs(single, "123456789", "", 85000, 12000, "TX"), []string{"us", " ca", "US"}},
		{"Empty", nil, nil},
		{"FirstSlice", rfFirstSlice(t), nil},
	}
	for _, s := range submit {
		req := &returnsv1.BuildSubmissionRequest{Inputs: s.in, Jurisdictions: s.jurisdictions}
		resp, err := srv.BuildSubmission(ctx, req)
		add("BuildSubmission/"+s.name, "BuildSubmission", req, resp, err)
	}
	return cases
}

// retEvalCase is one taxlisp module: the source, and the printed value of its
// last form (Value.String()) and, when that value is a protobuf message, its
// encoding; or the stage ("parse", "check" or "eval") that failed and the
// error text.
type retEvalCase struct {
	Name        string `json:"name"`
	Source      string `json:"source"`
	Checked     bool   `json:"checked"`
	Value       string `json:"value"`
	Message     string `json:"message"`
	MessageType string `json:"message_type"`
	Stage       string `json:"stage"`
	Error       string `json:"error"`
}

// retErrText is err's text with the protobuf module's randomized space
// after "proto:" (a space or a no-break space, chosen per binary so that no
// one depends on the text) made a plain space, so the vector is stable.
func retErrText(err error) string {
	return strings.ReplaceAll(errText(err), "proto:\u00a0", "proto: ")
}

// TestTaxlispEvalGolden writes returns/eval.json from retEvalSources.
func TestTaxlispEvalGolden(t *testing.T) {
	var cases []retEvalCase
	for _, s := range retEvalSources {
		c := retEvalCase{Name: s.name, Source: s.src, Checked: !s.unchecked}
		mf, err := parser.ParseFile("test.tlisp", s.src)
		if err != nil {
			c.Stage, c.Error = "parse", err.Error()
			cases = append(cases, c)
			continue
		}
		if !s.unchecked {
			if err := types.NewChecker(nil).CheckModule(mf); err != nil {
				c.Stage, c.Error = "check", err.Error()
				cases = append(cases, c)
				continue
			}
		}
		ev := eval.NewEvaluator(nil, nil)
		v, err := ev.EvalModule(mf)
		if err != nil {
			c.Stage, c.Error = "eval", retErrText(err)
			cases = append(cases, c)
			continue
		}
		c.Value = v.String()
		if pm, ok := v.(eval.ProtoMessageVal); ok {
			c.MessageType = string(pm.Msg.Descriptor().FullName())
			c.Message = encode(t, pm.Proto())
		}
		cases = append(cases, c)
	}
	check(t, "returns/eval.json", cases)
}
