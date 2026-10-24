// The shared arithmetic, on hand-worked cases.
//
// The published worked examples that exercise these helpers (Form 2210's and
// FTB 5805's payment application examples, and their tables of days per rate
// period) are tested in ustaxcalc and californiataxcalc, beside the extracts
// they come from. What is here are the cases nobody publishes: boundaries,
// compounding, months counted whole, and the annualised recapture.
package estimated_test

import (
	"math"
	"testing"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
)

func TestNextBusinessDay(t *testing.T) {
	c, err := estimated.NewCalendar("test").Holiday(d(2026, 9, 7), "Labor Day").Build()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ in, want estimated.Date }{
		{d(2026, 4, 15), d(2026, 4, 15)}, // a Wednesday
		{d(2027, 1, 31), d(2027, 2, 1)},  // Sunday to Monday
		{d(2026, 9, 5), d(2026, 9, 8)},   // Saturday, past Labor Day Monday
		{d(2026, 9, 7), d(2026, 9, 8)},   // the holiday itself
	} {
		if got := c.NextBusinessDay(tc.in); got != tc.want {
			t.Errorf("%v (%v) moves to %v, want %v", tc.in, tc.in.Weekday(), got, tc.want)
		}
	}
}

func TestCumulativeShares(t *testing.T) {
	// California's 30, 40, 0 and 30 on an amount that does not divide evenly.
	shares := []money.Rate{3000, 4000, 0, 3000}
	got, err := estimated.CumulativeShares(100_001, shares)
	if err != nil {
		t.Fatal(err)
	}
	want := []money.USCents{30_000, 70_001, 70_001, 100_001}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("running total %d is %v, want %v", i+1, got[i], want[i])
		}
	}
	if inc := estimated.Increments(got); inc[2] != 0 || inc[0]+inc[1]+inc[2]+inc[3] != 100_001 {
		t.Errorf("increments %v do not add back to the annual figure", inc)
	}
	if _, err := estimated.CumulativeShares(100, []money.Rate{2500, 2500, 2500}); err == nil {
		t.Error("shares adding to 75% were accepted")
	}
}

// The Schedule AI recapture, worked by hand with the federal percentages: a
// filer with nothing in the first quarter, a little by May and most of the
// year's income by August.
func TestAnnualisedRecapturesWhatItLowered(t *testing.T) {
	regular := []money.USCents{2_500, 2_500, 2_500, 2_500}
	taxes := []money.USCents{0, 4_000, 12_000, 12_000}
	got, err := estimated.Annualised(taxes, []money.Rate{2250, 4500, 6750, 9000}, regular)
	if err != nil {
		t.Fatal(err)
	}
	// (a) 22.5% of 0 is 0, under the regular 2,500, so 0 and 2,500 carries.
	// (b) 45% of 4,000 is 1,800, under 2,500 + 2,500, so 1,800 and 3,200 carries.
	// (c) 67.5% of 12,000 is 8,100, less 1,800 is 6,300; the regular 2,500 + 3,200
	//     is 5,700, which is smaller, so 5,700 and nothing carries.
	// (d) 90% of 12,000 is 10,800, less 7,500 is 3,300; the regular is 2,500.
	want := []money.USCents{0, 1_800, 5_700, 2_500}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("installment %d is %v, want %v", i+1, got[i], want[i])
		}
	}
}

// One payment pays the oldest shortfall first, and what is left counts toward
// the installment it was made for.
func TestApplyPaysTheOldestShortfallFirst(t *testing.T) {
	dues := []estimated.Date{d(2026, 4, 15), d(2026, 6, 15)}
	got, err := estimated.Apply(dues, []money.USCents{1_000, 1_000}, []estimated.Payment{
		payment(t, d(2026, 4, 15), 400),
		payment(t, d(2026, 5, 1), 900),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Shortfall() != 600 {
		t.Errorf("April shortfall is %v, want 600", got[0].Shortfall())
	}
	pieces := got[0].Pieces()
	if len(pieces) != 1 || pieces[0].Amount() != 600 {
		t.Fatalf("April pieces are %v", pieces)
	}
	if on, ok := pieces[0].PaidOn(); !ok || on != d(2026, 5, 1) {
		t.Errorf("April's shortfall was paid on %v, want May 1", on)
	}
	// 300 of the May payment is left for June, which is then 700 short.
	if got[1].Shortfall() != 700 || got[1].CreditedBy() != 1_300 || got[1].NeededBy() != 2_000 {
		t.Errorf("June: shortfall %v, credited %v, needed %v", got[1].Shortfall(), got[1].CreditedBy(), got[1].NeededBy())
	}
	if _, ok := got[1].Pieces()[0].PaidOn(); ok {
		t.Error("June's shortfall shows as paid")
	}
}

func series(t *testing.T) estimated.RateSeries {
	t.Helper()
	s, err := estimated.NewRateSeries().
		Period(d(2026, 4, 1), d(2026, 6, 30), money.Percent(6), "Q2").
		Period(d(2026, 7, 1), d(2026, 12, 31), money.Percent(7), "H2").
		Build()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// Simple interest on $10,000 from April 15 to August 14: 76 days at 6% and 45
// at 7%, each over 365 and rounded to the cent.
func TestSimpleDailySplitsAtTheRateChange(t *testing.T) {
	parts, unrated, err := estimated.SimpleDaily(10_000*money.Dollar, d(2026, 4, 15), d(2026, 8, 14), series(t))
	if err != nil {
		t.Fatal(err)
	}
	if !unrated.IsZero() || len(parts) != 2 {
		t.Fatalf("parts %v, unrated %v", parts, unrated)
	}
	// 1,000,000¢ × 76 × 0.06 / 365 = 12,493.15¢; × 45 × 0.07 / 365 = 8,630.14¢.
	for i, want := range []struct {
		days   int
		charge money.USCents
		source string
	}{{76, 12_493, "Q2"}, {45, 8_630, "H2"}} {
		if parts[i].Days() != want.days || parts[i].Charge() != want.charge || parts[i].Source() != want.source {
			t.Errorf("part %d: %d days, %v, %q; want %d days, %v, %q",
				i, parts[i].Days(), parts[i].Charge(), parts[i].Source(), want.days, want.charge, want.source)
		}
	}
}

// Days after the series ends are not charged, and the first of them is
// reported.
func TestSimpleDailyReportsUnratedDays(t *testing.T) {
	parts, unrated, err := estimated.SimpleDaily(10_000*money.Dollar, d(2026, 9, 15), d(2027, 4, 15), series(t))
	if err != nil {
		t.Fatal(err)
	}
	if unrated != d(2027, 1, 1) {
		t.Errorf("unrated from %v, want January 1, 2027", unrated)
	}
	if len(parts) != 1 || parts[0].To() != d(2026, 12, 31) || parts[0].Days() != 107 {
		t.Errorf("parts %+v, want one to December 31 of 107 days", parts)
	}
}

// A leap year divides by 366, so a span across into one is split at the year
// end; a span between two 365-day years is not.
func TestSimpleDailyDividesByTheYearsLength(t *testing.T) {
	s, err := estimated.NewRateSeries().Period(d(2027, 1, 1), d(2028, 12, 31), money.Percent(10), "s").Build()
	if err != nil {
		t.Fatal(err)
	}
	parts, _, err := estimated.SimpleDaily(366_00, d(2027, 12, 29), d(2028, 1, 2), s)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 2 || parts[0].Days() != 2 || parts[1].Days() != 2 {
		t.Fatalf("parts %+v, want 2 days in 2027 and 2 in 2028", parts)
	}
	// 36,600¢ × 2 × 0.10 / 365 = 20.05¢; / 366 = 20¢.
	if parts[0].Charge() != 20 || parts[1].Charge() != 20 {
		t.Errorf("charges %v and %v", parts[0].Charge(), parts[1].Charge())
	}
}

func TestCompoundDaily(t *testing.T) {
	s, err := estimated.NewRateSeries().Period(d(2026, 1, 1), d(2026, 12, 31), money.Percent(7), "s").Build()
	if err != nil {
		t.Fatal(err)
	}
	parts, _, err := estimated.CompoundDaily(1_000*money.Dollar, d(2026, 1, 1), d(2026, 12, 31), s)
	if err != nil {
		t.Fatal(err)
	}
	// 364 days, as 100,000¢ × ((1 + 0.07/365)^364 − 1).
	want := money.USCents(math.Round(100_000 * (math.Pow(1+0.07/365, 364) - 1)))
	if len(parts) != 1 || parts[0].Charge() != want {
		t.Errorf("compound interest is %v, want %v", parts[0].Charge(), want)
	}
	simple, _, _ := estimated.SimpleDaily(1_000*money.Dollar, d(2026, 1, 1), d(2026, 12, 31), s)
	if parts[0].Charge() <= simple[0].Charge() {
		t.Error("compounding charged no more than simple interest")
	}
}

func TestMonthsOrPartCountsPartMonthsWhole(t *testing.T) {
	for _, tc := range []struct {
		from, to estimated.Date
		want     int
	}{
		{d(2026, 4, 15), d(2026, 4, 15), 0},
		{d(2026, 4, 15), d(2026, 4, 16), 1},
		{d(2026, 4, 15), d(2026, 5, 15), 1},
		{d(2026, 4, 15), d(2026, 5, 16), 2},
		{d(2026, 1, 31), d(2026, 2, 28), 1},
		{d(2026, 1, 31), d(2026, 3, 1), 2},
	} {
		if got := estimated.MonthsOrPart(tc.from, tc.to); got != tc.want {
			t.Errorf("%v to %v is %d months, want %d", tc.from, tc.to, got, tc.want)
		}
	}
	p, err := estimated.Monthly(1_000*money.Dollar, d(2026, 4, 15), d(2026, 6, 16), 50, "s")
	if err != nil {
		t.Fatal(err)
	}
	// Three months or part at 0.5% of $1,000 is $15.
	if p.Months() != 3 || p.Charge() != 15*money.Dollar {
		t.Errorf("%d months, %v", p.Months(), p.Charge())
	}
}

func TestDaysAcrossAYearEnd(t *testing.T) {
	if got := d(2027, 4, 15).DaysSince(d(2026, 6, 30)); got != 289 {
		t.Errorf("June 30, 2026 to April 15, 2027 is %d days, want 289", got)
	}
}

// Credits spreads withholding by the shares whatever the facts elect, and
// CreditsAsWithheld puts it on its dates. Which one applies is the
// jurisdiction's rule, so neither reads the election.
func TestCreditsDoNotReadTheElection(t *testing.T) {
	this, err := estimated.NewFigures().Tax(10_000).Build()
	if err != nil {
		t.Fatal(err)
	}
	w, err := estimated.NewWithheld().On(d(2026, 11, 30)).Amount(4_000).Build()
	if err != nil {
		t.Fatal(err)
	}
	forward, err := estimated.NewPayment().FromPriorYear().Amount(500).Build()
	if err != nil {
		t.Fatal(err)
	}
	elected, err := estimated.NewElections().WithholdingAsWithheld().Build()
	if err != nil {
		t.Fatal(err)
	}
	f, err := estimated.NewFacts(estimated.Federal, 2026).FilingStatus(filing.Single).ThisYear(this).
		Withheld(w).Paid(forward).Elections(elected).Build()
	if err != nil {
		t.Fatal(err)
	}
	dues := []estimated.Date{d(2026, 4, 15), d(2026, 6, 15), d(2026, 9, 15), d(2027, 1, 15)}

	spread, err := estimated.Credits(f, dues, []money.Rate{2500, 2500, 2500, 2500})
	if err != nil {
		t.Fatal(err)
	}
	// 1,000 on each date, and the 500 brought forward on the first.
	if len(spread) != 5 || spread[0].On() != dues[0] || spread[4].On() != dues[3] || spread[4].Amount() != 1_000 {
		t.Errorf("spread credits %v", spread)
	}
	dated, err := estimated.CreditsAsWithheld(f, dues)
	if err != nil {
		t.Fatal(err)
	}
	if len(dated) != 2 || dated[0].On() != dues[0] || dated[0].Amount() != 500 || dated[1].On() != d(2026, 11, 30) || dated[1].Amount() != 4_000 {
		t.Errorf("dated credits %v", dated)
	}
}
