package estimated

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/nexteggapp/taxcalcs/money"
)

// RatePeriod is an interest rate and the days it applies to, both ends
// included, with the notice that set it.
//
// The rate is held in millionths, so that a rate published to four decimal
// places, as Maryland's 10.8133% is, is charged as published.
type RatePeriod struct {
	from, to Date
	rate     money.FineRate
	source   string
}

func (p RatePeriod) From() Date { return p.from }
func (p RatePeriod) To() Date   { return p.to }

// Rate is the rate to the nearest basis point. It is exact for a period
// added with [RateSeriesBuilder.Period].
func (p RatePeriod) Rate() money.Rate { return p.rate.Nearest() }

// FineRate is the rate as published.
func (p RatePeriod) FineRate() money.FineRate { return p.rate }

// Source cites the notice, revenue ruling or page that published the rate.
func (p RatePeriod) Source() string { return p.source }

// RateSeries is a run of rate periods with no gap between them.
//
// A rate is a dated series rather than a number because an underpayment can
// span two periods with different rates, and the penalty forms charge each
// span at its own. The series ends where the last published rate ends; days
// after that have no rate, and the accrual helpers report them rather than
// carry the last rate forward.
type RateSeries struct{ periods []RatePeriod }

// Periods returns the periods, earliest first.
func (s RateSeries) Periods() []RatePeriod { return append([]RatePeriod(nil), s.periods...) }

// Ends is the last day the series has a rate for.
func (s RateSeries) Ends() Date {
	if len(s.periods) == 0 {
		return Date{}
	}
	return s.periods[len(s.periods)-1].to
}

// On returns the period that covers d.
func (s RateSeries) On(d Date) (RatePeriod, bool) {
	for _, p := range s.periods {
		if !d.Before(p.from) && !d.After(p.to) {
			return p, true
		}
	}
	return RatePeriod{}, false
}

// RateSeriesBuilder builds a [RateSeries].
type RateSeriesBuilder struct{ s RateSeries }

func NewRateSeries() *RateSeriesBuilder { return &RateSeriesBuilder{} }

// Period adds the rate for the days from through to, both included.
func (b *RateSeriesBuilder) Period(from, to Date, rate money.Rate, source string) *RateSeriesBuilder {
	return b.FinePeriod(from, to, rate.Fine(), source)
}

// FinePeriod adds a rate that basis points cannot hold for the days from
// through to, both included.
func (b *RateSeriesBuilder) FinePeriod(from, to Date, rate money.FineRate, source string) *RateSeriesBuilder {
	b.s.periods = append(b.s.periods, RatePeriod{from: from, to: to, rate: rate, source: source})
	return b
}

// Build checks every period is sourced and runs forward, and that each starts
// the day after the one before ends. A gap would leave days with no rate that
// nothing reported, and an overlap would give a day two.
func (b *RateSeriesBuilder) Build() (RateSeries, error) {
	ps := append([]RatePeriod(nil), b.s.periods...)
	if len(ps) == 0 {
		return RateSeries{}, errors.New("estimated: a rate series needs at least one period")
	}
	for i, p := range ps {
		switch {
		case p.from.IsZero() || p.to.IsZero() || p.to.Before(p.from):
			return RateSeries{}, fmt.Errorf("estimated: rate period %v to %v does not run forward", p.from, p.to)
		case p.rate < 0:
			return RateSeries{}, fmt.Errorf("estimated: rate %v is negative", p.rate)
		case p.source == "":
			return RateSeries{}, fmt.Errorf("estimated: the rate from %v has no source", p.from)
		case i > 0 && p.from != ps[i-1].to.AddDays(1):
			return RateSeries{}, fmt.Errorf("estimated: the rate from %v does not follow the period ending %v", p.from, ps[i-1].to)
		}
	}
	return RateSeries{periods: ps}, nil
}

// segment is a run of days under one rate and one length of year.
type segment struct {
	from, to Date // interest for the days after from, up to and including to
	period   RatePeriod
}

// segments splits the days after from up to and including to at each rate
// period's end, and at a year end where a leap year starts or stops. A span
// that crosses into a year of the same length stays whole, as FTB 5805 counts
// July 1, 2025 to April 15, 2026 as one span of 289 days. It returns the first
// day no rate covers, or the zero Date when every day is covered.
func segments(from, to Date, s RateSeries) ([]segment, Date) {
	var out []segment
	cur := from
	for cur.Before(to) {
		day := cur.AddDays(1)
		p, ok := s.On(day)
		if !ok {
			return out, day
		}
		end := earlier(to, p.to)
		if daysInYear(day.year) != daysInYear(day.year+1) {
			end = earlier(end, Date{day.year, 12, 31})
		}
		out = append(out, segment{from: cur, to: end, period: p})
		cur = end
	}
	return out, Date{}
}

// roundDiv divides n by d, rounding half away from zero, as [money.Rate.Of]
// does.
func roundDiv(n, d *big.Int) money.USCents {
	q, r := new(big.Int).QuoRem(n, d, new(big.Int))
	if new(big.Int).Mul(new(big.Int).Abs(r), big.NewInt(2)).Cmp(new(big.Int).Abs(d)) >= 0 {
		if n.Sign()*d.Sign() < 0 {
			q.Sub(q, big.NewInt(1))
		} else {
			q.Add(q, big.NewInt(1))
		}
	}
	return money.USCents(q.Int64())
}

// SimpleDaily accrues simple interest on principal for the days after from up
// to and including to, one part for each rate period and calendar year the
// days fall in.
//
// Each part is principal × days ÷ days in the year × rate, rounded to the
// cent, with the rate as published: the computation on Form 2210's penalty worksheet and FTB 5805
// Worksheet II, which divide by 365, or 366 in a leap year. A part never
// spans years of different lengths.
//
// unrated is the first day the series has no rate for, or the zero Date. The
// parts stop the day before it, and the caller must report the gap.
func SimpleDaily(principal money.USCents, from, to Date, s RateSeries) (parts []PenaltyPart, unrated Date, err error) {
	segs, unrated := segments(from, to, s)
	for _, g := range segs {
		days := g.to.DaysSince(g.from)
		n := new(big.Int).Mul(big.NewInt(int64(principal)), big.NewInt(int64(days)))
		n.Mul(n, big.NewInt(int64(g.period.rate)))
		d := big.NewInt(int64(1_000_000 * daysInYear(g.to.year)))
		p, err := NewPenaltyPart(PartInterest, principal, 0).FineRate(g.period.rate).
			Span(g.from, g.to).Days(days).Charge(roundDiv(n, d)).Source(g.period.source).Build()
		if err != nil {
			return nil, Date{}, err
		}
		parts = append(parts, p)
	}
	return parts, unrated, nil
}

// CompoundDaily accrues interest compounded daily on principal, as 26 U.S.C.
// § 6622 compounds interest on a federal underpayment of tax and as several
// states compound their estimated tax penalty.
//
// Each part's charge is the balance at its start, principal plus the charges
// before it, times (1 + rate ÷ days in the year) raised to the part's days,
// less the balance; rounded to the cent. [PenaltyPart.Principal] is the
// original principal throughout.
func CompoundDaily(principal money.USCents, from, to Date, s RateSeries) (parts []PenaltyPart, unrated Date, err error) {
	const prec = 256
	segs, unrated := segments(from, to, s)
	balance := new(big.Float).SetPrec(prec).SetInt64(int64(principal))
	for _, g := range segs {
		days := g.to.DaysSince(g.from)
		daily := new(big.Float).SetPrec(prec).Quo(
			new(big.Float).SetPrec(prec).SetInt64(int64(g.period.rate)),
			new(big.Float).SetPrec(prec).SetInt64(int64(1_000_000*daysInYear(g.to.year))))
		factor := new(big.Float).SetPrec(prec).SetInt64(1)
		step := new(big.Float).SetPrec(prec).Add(new(big.Float).SetPrec(prec).SetInt64(1), daily)
		for i := 0; i < days; i++ {
			factor.Mul(factor, step)
		}
		grown := new(big.Float).SetPrec(prec).Mul(balance, factor)
		interest := new(big.Float).SetPrec(prec).Sub(grown, balance)
		charge := roundFloat(interest)
		p, err := NewPenaltyPart(PartInterest, principal, 0).FineRate(g.period.rate).
			Span(g.from, g.to).Days(days).Charge(charge).Source(g.period.source).Build()
		if err != nil {
			return nil, Date{}, err
		}
		parts = append(parts, p)
		balance.Add(balance, new(big.Float).SetPrec(prec).SetInt64(int64(charge)))
	}
	return parts, unrated, nil
}

func roundFloat(f *big.Float) money.USCents {
	half := new(big.Float).SetPrec(f.Prec()).SetFloat64(0.5)
	if f.Sign() < 0 {
		half.Neg(half)
	}
	i, _ := new(big.Float).SetPrec(f.Prec()).Add(f, half).Int(nil)
	return money.USCents(i.Int64())
}

// MonthsOrPart counts the months from from to to with any part of a month
// counted as a whole one. April 15th to May 15th is one month; to May 16th is
// two. Zero when to is not after from.
func MonthsOrPart(from, to Date) int {
	if !to.After(from) {
		return 0
	}
	k := 1
	for from.AddMonths(k).Before(to) {
		k++
	}
	return k
}

// Monthly charges monthlyRate of principal for each month or part of a month
// from from to to, as a state that charges "per month or fraction of a month"
// does. The charge is rounded once, on the whole span.
func Monthly(principal money.USCents, from, to Date, monthlyRate money.Rate, source string) (PenaltyPart, error) {
	months := MonthsOrPart(from, to)
	n := new(big.Int).Mul(big.NewInt(int64(principal)), big.NewInt(int64(monthlyRate)))
	n.Mul(n, big.NewInt(int64(months)))
	charge := roundDiv(n, big.NewInt(10_000))
	return NewPenaltyPart(PartMonthly, principal, monthlyRate).
		Span(from, to).Months(months).Charge(charge).Source(source).Build()
}
