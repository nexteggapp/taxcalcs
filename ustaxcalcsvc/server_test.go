package ustaxcalcsvc_test

import (
	"context"
	"net"
	"testing"

	usv1 "github.com/nexteggapp/taxcalcs/us/v1"
	"github.com/nexteggapp/taxcalcs/ustaxcalc"
	"github.com/nexteggapp/taxcalcs/ustaxcalcsvc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// The service is tested over a real gRPC connection rather than by calling the
// server's methods directly. Half of what this package does is the contract —
// field names, enum values, what a missing field means — and calling the Go
// method in-process would exercise none of it.
func client(t *testing.T) usv1.TaxCalculatorClient {
	t.Helper()

	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	usv1.RegisterTaxCalculatorServer(srv, ustaxcalcsvc.New())
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	return usv1.NewTaxCalculatorClient(conn)
}

func filer(year int32, s usv1.FilingStatus) *usv1.Filer {
	return &usv1.Filer{TaxYear: year, FilingStatus: s}
}

// TestParity is the test this package exists for.
//
// Every figure the service returns is //ustaxcalc's, reached through a
// conversion this package wrote by hand. A field copied into the wrong place,
// or not copied at all, produces a plausible number rather than a failure, and
// nothing in //ustaxcalc would notice. So each method is checked against the
// Go call it wraps.
func TestParity(t *testing.T) {
	c := client(t)
	ctx := context.Background()

	const (
		year     = 2026
		taxable  = 103_900 * ustaxcalc.Dollar
		gain     = 5_000 * ustaxcalc.Dollar
		itemised = 9_400 * ustaxcalc.Dollar
		profit   = 40_000 * ustaxcalc.Dollar
		wages    = 90_000 * ustaxcalc.Dollar
	)

	t.Run("OrdinaryIncomeTax", func(t *testing.T) {
		want, err := ustaxcalc.OrdinaryIncomeTax(year, ustaxcalc.Single, taxable)
		if err != nil {
			t.Fatal(err)
		}
		got, err := c.OrdinaryIncomeTax(ctx, &usv1.OrdinaryIncomeTaxRequest{
			Filer:                filer(year, usv1.FilingStatus_FILING_STATUS_SINGLE),
			TaxableOrdinaryCents: int64(taxable),
		})
		if err != nil {
			t.Fatal(err)
		}
		if got.GetLadder().GetTotalCents() != int64(want.Value().Total) {
			t.Errorf("total = %d, want %d", got.GetLadder().GetTotalCents(), want.Value().Total)
		}
		if got.GetLadder().GetMarginalRateBasisPoints() != int32(want.Value().Marginal) {
			t.Errorf("marginal = %d, want %d", got.GetLadder().GetMarginalRateBasisPoints(), want.Value().Marginal)
		}
		if got.GetLadder().GetRoomInBracketCents() != int64(want.Value().RoomInBracket) {
			t.Errorf("room = %d, want %d", got.GetLadder().GetRoomInBracketCents(), want.Value().RoomInBracket)
		}
		if len(got.GetLadder().GetBands()) != len(want.Value().Bands) {
			t.Fatalf("bands = %d, want %d", len(got.GetLadder().GetBands()), len(want.Value().Bands))
		}
		for i, b := range got.GetLadder().GetBands() {
			w := want.Value().Bands[i]
			if b.GetRateBasisPoints() != int32(w.Rate) || b.GetFromCents() != int64(w.From) ||
				b.GetToCents() != int64(w.To) || b.GetIncomeCents() != int64(w.Income) ||
				b.GetTaxCents() != int64(w.Tax) {
				t.Errorf("band %d = %+v, want %+v", i, b, w)
			}
		}
		if len(got.GetApplied()) != len(want.Applied()) {
			t.Errorf("applied = %d rules, want %d", len(got.GetApplied()), len(want.Applied()))
		}
	})

	t.Run("LongTermGainsTax", func(t *testing.T) {
		want, err := ustaxcalc.LongTermGainsTax(year, ustaxcalc.Single, taxable, gain)
		if err != nil {
			t.Fatal(err)
		}
		got, err := c.LongTermGainsTax(ctx, &usv1.LongTermGainsTaxRequest{
			Filer:                filer(year, usv1.FilingStatus_FILING_STATUS_SINGLE),
			TaxableOrdinaryCents: int64(taxable),
			NetLongTermGainCents: int64(gain),
		})
		if err != nil {
			t.Fatal(err)
		}
		g := got.GetGains()
		if g.GetAtZeroCents() != int64(want.Value().AtZero) || g.GetAtFifteenCents() != int64(want.Value().At15) ||
			g.GetAtTwentyCents() != int64(want.Value().At20) || g.GetTotalCents() != int64(want.Value().Total) {
			t.Errorf("gains = %+v, want %+v", g, want.Value())
		}
	})

	t.Run("SelfEmploymentTax", func(t *testing.T) {
		// Deliberately unequal, because these are the two fields most easily
		// swapped: they are the same figure for most filers and diverge above
		// the wage base. Equal arguments would pass whichever way round the
		// conversion put them.
		const medicare = wages + 1_000_000

		want, err := ustaxcalc.SelfEmploymentTax(year, ustaxcalc.Single, profit, wages, medicare)
		if err != nil {
			t.Fatal(err)
		}
		got, err := c.SelfEmploymentTax(ctx, &usv1.SelfEmploymentTaxRequest{
			Filer:                      filer(year, usv1.FilingStatus_FILING_STATUS_SINGLE),
			NetProfitCents:             int64(profit),
			W2SocialSecurityWagesCents: int64(wages),
			OtherMedicareWagesCents:    int64(medicare),
		})
		if err != nil {
			t.Fatal(err)
		}
		s := got.GetSeca()
		if s.GetNetEarningsCents() != int64(want.Value().NetEarnings) ||
			s.GetSocialSecurityCents() != int64(want.Value().SocialSecurity) ||
			s.GetMedicareCents() != int64(want.Value().Medicare) ||
			s.GetAdditionalMedicareCents() != int64(want.Value().AdditionalMedicare) ||
			s.GetTotalCents() != int64(want.Value().Total) ||
			s.GetAboveTheLineDeductionCents() != int64(want.Value().AboveTheLineDeduction) {
			t.Errorf("seca = %+v, want %+v", s, want.Value())
		}
	})

	t.Run("SafeHarbour", func(t *testing.T) {
		const (
			current = 20_000 * ustaxcalc.Dollar
			prior   = 18_000 * ustaxcalc.Dollar
			agi     = 120_000 * ustaxcalc.Dollar
		)
		want, err := ustaxcalc.RequiredAnnualPayment(year, ustaxcalc.Single, current, prior, agi, true)
		if err != nil {
			t.Fatal(err)
		}
		got, err := c.SafeHarbour(ctx, &usv1.SafeHarbourRequest{
			Filer:                filer(year, usv1.FilingStatus_FILING_STATUS_SINGLE),
			CurrentYearTaxCents:  int64(current),
			PriorYearTaxCents:    int64(prior),
			PriorYearAgiCents:    int64(agi),
			PriorYearReturnFiled: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		h := got.GetSafeHarbour()
		if h.GetRequiredCents() != int64(want.Value().Required) ||
			h.GetCurrentYearTestCents() != int64(want.Value().CurrentYearTest) ||
			h.GetPriorYearTestCents() != int64(want.Value().PriorYearTest) ||
			h.GetPriorYearRateBasisPoints() != int32(want.Value().PriorYearRate) {
			t.Errorf("safe harbour = %+v, want %+v", h, want.Value())
		}

		wantInst := ustaxcalc.Instalments(year, want.Value().Required)
		if len(got.GetInstalments()) != len(wantInst) {
			t.Fatalf("instalments = %d, want %d", len(got.GetInstalments()), len(wantInst))
		}
		for i, in := range got.GetInstalments() {
			w := wantInst[i]
			if int(in.GetDueYear()) != w.Due.Year() || int(in.GetDueMonth()) != int(w.Due.Month()) ||
				int(in.GetDueDay()) != w.Due.Day() || in.GetCumulativeTargetCents() != int64(w.CumulativeTarget) {
				t.Errorf("instalment %d = %+v, want %v %v", i, in, w.Due, w.CumulativeTarget)
			}
		}
	})

	t.Run("GetRateTable", func(t *testing.T) {
		want, err := ustaxcalc.TableFor(year, ustaxcalc.Single)
		if err != nil {
			t.Fatal(err)
		}
		got, err := c.GetRateTable(ctx, &usv1.GetRateTableRequest{
			TaxYear:      year,
			FilingStatus: usv1.FilingStatus_FILING_STATUS_SINGLE,
		})
		if err != nil {
			t.Fatal(err)
		}
		tb := got.GetTable()
		if tb.GetStandardDeductionCents() != int64(want.StandardDeduction) ||
			tb.GetSocialSecurityWageBaseCents() != int64(want.SocialSecurityWageBase) ||
			tb.GetMaxZeroRateAmountCents() != int64(want.MaxZeroRateAmount) ||
			tb.GetMaxFifteenRateAmountCents() != int64(want.Max15RateAmount) {
			t.Errorf("table = %+v, want %+v", tb, want)
		}
		if len(tb.GetOrdinary()) != len(want.Ordinary) {
			t.Fatalf("brackets = %d, want %d", len(tb.GetOrdinary()), len(want.Ordinary))
		}
		for i, b := range tb.GetOrdinary() {
			if b.GetOverCents() != int64(want.Ordinary[i].Over) || b.GetRateBasisPoints() != int32(want.Ordinary[i].Rate) {
				t.Errorf("bracket %d = %+v, want %+v", i, b, want.Ordinary[i])
			}
		}
	})
}

// TestDeductionElection covers the § 63(e) choice, which the service is the
// first caller able to make: until the election existed, DeductionTaken took
// the larger figure and there was no way to say otherwise.
func TestDeductionElection(t *testing.T) {
	c := client(t)
	ctx := context.Background()

	// Below the 2026 single standard deduction of $16,100, so electing to
	// itemise is a choice against the taxpayer's own interest — which § 63(e)
	// permits, and which one filing separately may be forced into.
	const itemised = 940_000

	for _, tc := range []struct {
		name         string
		election     usv1.DeductionElection
		wantItemised bool
		wantAmount   int64
		wantPosition bool
	}{
		{"unspecified takes the larger", usv1.DeductionElection_DEDUCTION_ELECTION_UNSPECIFIED, false, 1_610_000, true},
		{"standard", usv1.DeductionElection_DEDUCTION_ELECTION_STANDARD, false, 1_610_000, false},
		{"itemise the smaller total", usv1.DeductionElection_DEDUCTION_ELECTION_ITEMISE, true, itemised, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := c.Deduction(ctx, &usv1.DeductionRequest{
				Filer:              filer(2026, usv1.FilingStatus_FILING_STATUS_SINGLE),
				ItemisedTotalCents: itemised,
				Election:           tc.election,
			})
			if err != nil {
				t.Fatal(err)
			}
			if got.GetDeduction().GetAmountCents() != tc.wantAmount {
				t.Errorf("amount = %d, want %d", got.GetDeduction().GetAmountCents(), tc.wantAmount)
			}
			if got.GetDeduction().GetItemised() != tc.wantItemised {
				t.Errorf("itemised = %v, want %v", got.GetDeduction().GetItemised(), tc.wantItemised)
			}
			// An answered election is not still an open question. Reporting it
			// as one would have a product ask the user something they just
			// told it.
			if hasPosition(got.GetRequired(), usv1.PositionKind_POSITION_KIND_DEDUCTION_METHOD) != tc.wantPosition {
				t.Errorf("deduction-method position present = %v, want %v",
					!tc.wantPosition, tc.wantPosition)
			}
		})
	}
}

func TestUnknownYearIsNotFound(t *testing.T) {
	c := client(t)

	_, err := c.OrdinaryIncomeTax(context.Background(), &usv1.OrdinaryIncomeTaxRequest{
		Filer:                filer(1999, usv1.FilingStatus_FILING_STATUS_SINGLE),
		TaxableOrdinaryCents: 5_000_000,
	})
	if status.Code(err) != codes.NotFound {
		t.Errorf("code = %v, want NotFound (the year is valid; this build has no table)", status.Code(err))
	}
}

func TestMissingFilingStatusIsRefused(t *testing.T) {
	c := client(t)

	// Not a default. Computing this as a single filer because a field was unset
	// would return somebody else's tax with the confidence of a right answer.
	_, err := c.OrdinaryIncomeTax(context.Background(), &usv1.OrdinaryIncomeTaxRequest{
		Filer:                &usv1.Filer{TaxYear: 2026},
		TaxableOrdinaryCents: 5_000_000,
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("code = %v, want InvalidArgument", status.Code(err))
	}
}

func TestListTaxYearsMatchesLibrary(t *testing.T) {
	c := client(t)

	got, err := c.ListTaxYears(context.Background(), &usv1.ListTaxYearsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	want := ustaxcalc.Years()
	if len(got.GetTaxYears()) != len(want) {
		t.Fatalf("years = %v, want %d of them", got.GetTaxYears(), len(want))
	}
	for _, y := range want {
		if !containsInt32(got.GetTaxYears(), int32(y)) {
			t.Errorf("years = %v, missing %d", got.GetTaxYears(), y)
		}
	}
}

func hasPosition(ps []*usv1.Position, k usv1.PositionKind) bool {
	for _, p := range ps {
		if p.GetKind() == k {
			return true
		}
	}
	return false
}

func containsInt32(xs []int32, x int32) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
