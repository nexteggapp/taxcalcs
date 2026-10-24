// Package ustaxcalcsvc serves the US federal calculators over the
// nexteggapp.taxcalcs.us.v1.TaxCalculator contract.
//
// It exists so the calculators can be used from something other than Go. The
// arithmetic is entirely //ustaxcalc's; this package converts, and holds no tax
// rule of its own. A rule that appeared here would be one a Go caller could not
// reach and no test in //ustaxcalc would cover.
//
// # Where to run it
//
// Nowhere public. Every method takes somebody's income and tells them what they
// owe, and every figure comes from tables compiled into the binary, so there is
// no computational reason for any of it to leave the machine that asked.
// Register it in-process, compile it to WebAssembly and call it in a browser
// tab, or bind a loopback socket. Standing it up as an internet endpoint would
// be collecting financial data to no purpose.
package ustaxcalcsvc

import (
	"context"
	"errors"
	"fmt"

	usv1 "github.com/nexteggapp/taxcalcs/us/v1"
	"github.com/nexteggapp/taxcalcs/ustaxcalc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Server implements usv1.TaxCalculatorServer.
//
// It holds nothing. The calculators are pure functions over tables compiled in,
// so one value serves every caller and the zero value is ready.
type Server struct {
	usv1.UnimplementedTaxCalculatorServer
}

// New returns a server. Stateless, so the result can be shared freely.
func New() *Server { return &Server{} }

// filer reads the two fields every calculator needs.
//
// A missing filer and an unspecified status are both InvalidArgument rather
// than defaults. Defaulting a filing status would compute somebody's tax as a
// single filer because a field was left unset, and return it with the same
// confidence as a correct answer.
func filer(f *usv1.Filer) (int, ustaxcalc.FilingStatus, error) {
	if f == nil {
		return 0, 0, status.Error(codes.InvalidArgument, "filer is required")
	}
	s, ok := filingStatuses[f.GetFilingStatus()]
	if !ok {
		return 0, 0, status.Error(codes.InvalidArgument, "filing_status is required")
	}
	return int(f.GetTaxYear()), s, nil
}

// fail maps a calculator's error onto a status code.
//
// ErrNoTable is NotFound and not InvalidArgument: the year is a perfectly valid
// year, this build simply carries no table for it, and a caller can find out
// which years it does carry from ListTaxYears.
func fail(err error) error {
	if errors.Is(err, ustaxcalc.ErrNoTable) {
		return status.Error(codes.NotFound, err.Error())
	}
	return status.Error(codes.InvalidArgument, err.Error())
}

func (s *Server) ListTaxYears(_ context.Context, _ *usv1.ListTaxYearsRequest) (*usv1.ListTaxYearsResponse, error) {
	years := ustaxcalc.Years()
	out := make([]int32, 0, len(years))
	for _, y := range years {
		out = append(out, int32(y))
	}
	// Years is documented as unordered, and a wire contract that returns a list
	// in an arbitrary order invites a caller to depend on today's accident.
	sortInt32(out)
	return &usv1.ListTaxYearsResponse{TaxYears: out}, nil
}

func (s *Server) GetRateTable(_ context.Context, req *usv1.GetRateTableRequest) (*usv1.GetRateTableResponse, error) {
	st, ok := filingStatuses[req.GetFilingStatus()]
	if !ok {
		return nil, status.Error(codes.InvalidArgument, "filing_status is required")
	}
	t, err := ustaxcalc.TableFor(int(req.GetTaxYear()), st)
	if err != nil {
		return nil, fail(err)
	}
	return &usv1.GetRateTableResponse{Table: rateTable(t)}, nil
}

func (s *Server) OrdinaryIncomeTax(_ context.Context, req *usv1.OrdinaryIncomeTaxRequest) (*usv1.OrdinaryIncomeTaxResponse, error) {
	year, st, err := filer(req.GetFiler())
	if err != nil {
		return nil, err
	}
	res, err := ustaxcalc.OrdinaryIncomeTax(year, st, ustaxcalc.USCents(req.GetTaxableOrdinaryCents()))
	if err != nil {
		return nil, fail(err)
	}
	return &usv1.OrdinaryIncomeTaxResponse{
		Ladder:   ladder(res.Value()),
		Applied:  rules(res.Applied()),
		Required: positions(res.Required()),
	}, nil
}

func (s *Server) LongTermGainsTax(_ context.Context, req *usv1.LongTermGainsTaxRequest) (*usv1.LongTermGainsTaxResponse, error) {
	year, st, err := filer(req.GetFiler())
	if err != nil {
		return nil, err
	}
	res, err := ustaxcalc.LongTermGainsTax(year, st,
		ustaxcalc.USCents(req.GetTaxableOrdinaryCents()),
		ustaxcalc.USCents(req.GetNetLongTermGainCents()))
	if err != nil {
		return nil, fail(err)
	}
	return &usv1.LongTermGainsTaxResponse{
		Gains:    gains(res.Value()),
		Applied:  rules(res.Applied()),
		Required: positions(res.Required()),
	}, nil
}

func (s *Server) Deduction(_ context.Context, req *usv1.DeductionRequest) (*usv1.DeductionResponse, error) {
	year, st, err := filer(req.GetFiler())
	if err != nil {
		return nil, err
	}

	var election ustaxcalc.Election
	switch req.GetElection() {
	case usv1.DeductionElection_DEDUCTION_ELECTION_UNSPECIFIED:
		election = ustaxcalc.ElectLarger
	case usv1.DeductionElection_DEDUCTION_ELECTION_STANDARD:
		election = ustaxcalc.ElectStandard
	case usv1.DeductionElection_DEDUCTION_ELECTION_ITEMISE:
		election = ustaxcalc.ElectItemise
	default:
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("unknown election %v", req.GetElection()))
	}

	res, err := ustaxcalc.DeductionTaken(year, st, conditions(req.GetFiler()),
		ustaxcalc.USCents(req.GetItemisedTotalCents()), election)
	if err != nil {
		return nil, fail(err)
	}
	return &usv1.DeductionResponse{
		Deduction: deduction(res.Value()),
		Applied:   rules(res.Applied()),
		Required:  positions(res.Required()),
	}, nil
}

func (s *Server) SelfEmploymentTax(_ context.Context, req *usv1.SelfEmploymentTaxRequest) (*usv1.SelfEmploymentTaxResponse, error) {
	year, st, err := filer(req.GetFiler())
	if err != nil {
		return nil, err
	}
	res, err := ustaxcalc.SelfEmploymentTax(year, st,
		ustaxcalc.USCents(req.GetNetProfitCents()),
		ustaxcalc.USCents(req.GetW2SocialSecurityWagesCents()),
		ustaxcalc.USCents(req.GetOtherMedicareWagesCents()))
	if err != nil {
		return nil, fail(err)
	}
	return &usv1.SelfEmploymentTaxResponse{
		Seca:     seca(res.Value()),
		Applied:  rules(res.Applied()),
		Required: positions(res.Required()),
	}, nil
}

func (s *Server) SafeHarbour(_ context.Context, req *usv1.SafeHarbourRequest) (*usv1.SafeHarbourResponse, error) {
	year, st, err := filer(req.GetFiler())
	if err != nil {
		return nil, err
	}
	res, err := ustaxcalc.RequiredAnnualPayment(year, st,
		ustaxcalc.USCents(req.GetCurrentYearTaxCents()),
		ustaxcalc.USCents(req.GetPriorYearTaxCents()),
		ustaxcalc.USCents(req.GetPriorYearAgiCents()),
		req.GetPriorYearReturnFiled())
	if err != nil {
		return nil, fail(err)
	}
	return &usv1.SafeHarbourResponse{
		SafeHarbour: safeHarbour(res.Value()),
		Instalments: instalments(ustaxcalc.Instalments(year, res.Value().Required)),
		Applied:     rules(res.Applied()),
		Required:    positions(res.Required()),
	}, nil
}

func sortInt32(xs []int32) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j] < xs[j-1]; j-- {
			xs[j], xs[j-1] = xs[j-1], xs[j]
		}
	}
}
