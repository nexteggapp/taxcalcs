package estimated

import (
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/nexteggapp/taxcalcs/money"
)

// Some penalty forms do not charge an annual rate. They print a factor, a
// decimal already worked out from the rate, and the filer multiplies by it:
// Pennsylvania's REV-1611 prints 0.000192 a day for 7%, and Maryland's Form
// 502UP prints one factor for each span between due dates. The printed factor
// is rounded, so the form's figure and the rate's figure differ, and a
// calculator that follows the form must use the factor as printed.

// Factor is an interest factor as a form prints it.
type Factor struct {
	units   int64 // the digits, without the point
	places  int   // how many of them follow the point
	printed string
}

// ParseFactor reads a factor written as the form prints it, as "0.000192".
// It refuses anything but digits with at most one point, and a factor of
// zero.
func ParseFactor(printed string) (Factor, error) {
	whole, frac, _ := strings.Cut(printed, ".")
	digits := whole + frac
	if digits == "" || strings.Count(printed, ".") > 1 {
		return Factor{}, fmt.Errorf("estimated: %q is not a printed factor", printed)
	}
	var units int64
	for _, c := range digits {
		if c < '0' || c > '9' {
			return Factor{}, fmt.Errorf("estimated: %q is not a printed factor", printed)
		}
		if units > (1<<62)/10 {
			return Factor{}, fmt.Errorf("estimated: %q has too many digits", printed)
		}
		units = units*10 + int64(c-'0')
	}
	if units == 0 {
		return Factor{}, fmt.Errorf("estimated: a factor of %q charges nothing", printed)
	}
	return Factor{units: units, places: len(frac), printed: printed}, nil
}

// String is the factor as printed.
func (f Factor) String() string { return f.printed }

// Rounding is how a form rounds a charge it computes from a factor.
type Rounding int

const (
	RoundingUnspecified Rounding = iota

	// RoundToCent rounds each charge to the nearest cent, half up.
	RoundToCent

	// RoundToDollar rounds each charge to the nearest whole dollar, half up,
	// as REV-1630 lines 15a to 15c do.
	RoundToDollar
)

func (r Rounding) String() string {
	switch r {
	case RoundToCent:
		return "to the cent"
	case RoundToDollar:
		return "to the whole dollar"
	default:
		return "unspecified rounding"
	}
}

func (r Rounding) unit() int64 {
	if r == RoundToDollar {
		return int64(money.Dollar)
	}
	return 1
}

// Div is n ÷ d cents rounded to the unit r names, halves away from zero, so
// half up for amounts that are not negative. It is the rounding for a form
// that rounds each charge or installment to the whole dollar without first
// rounding it to the cent.
func (r Rounding) Div(n, d *big.Int) money.USCents {
	u := big.NewInt(r.unit())
	return roundDiv(n, new(big.Int).Mul(d, u)) * money.USCents(r.unit())
}

// Round rounds a to the unit r names, halves away from zero.
func (r Rounding) Round(a money.USCents) money.USCents {
	return r.Div(big.NewInt(int64(a)), big.NewInt(1))
}

// Of is principal × times × the factor, rounded half up as r says. times is
// the days for a daily factor and 1 for a factor printed for a whole span.
func (f Factor) Of(principal money.USCents, times int, r Rounding) money.USCents {
	n := new(big.Int).Mul(big.NewInt(int64(principal)), big.NewInt(int64(times)))
	n.Mul(n, big.NewInt(f.units))
	d := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(f.places)), nil)
	d.Mul(d, big.NewInt(r.unit()))
	return roundDiv(n, d) * money.USCents(r.unit())
}

// FactorPeriod is a daily factor and the days it applies to, both ends
// included, with the annual rate it stands for and the document that printed
// it.
type FactorPeriod struct {
	from, to Date
	factor   Factor
	annual   money.Rate
	source   string
}

func (p FactorPeriod) From() Date       { return p.from }
func (p FactorPeriod) To() Date         { return p.to }
func (p FactorPeriod) Factor() Factor   { return p.factor }
func (p FactorPeriod) Rate() money.Rate { return p.annual }

// Source cites the document that printed the factor.
func (p FactorPeriod) Source() string { return p.source }

// FactorSeries is a run of daily factors with no gap between them, and how
// the form rounds what each one charges.
//
// Like a [RateSeries], it ends where the last printed factor ends. Days after
// that have no factor, and [DailyFactor] reports them rather than carry the
// last one forward.
type FactorSeries struct {
	periods  []FactorPeriod
	rounding Rounding
}

// Periods returns the periods, earliest first.
func (s FactorSeries) Periods() []FactorPeriod { return append([]FactorPeriod(nil), s.periods...) }

// Rounding is how the form rounds each part.
func (s FactorSeries) Rounding() Rounding { return s.rounding }

// On returns the period that covers d.
func (s FactorSeries) On(d Date) (FactorPeriod, bool) {
	for _, p := range s.periods {
		if !d.Before(p.from) && !d.After(p.to) {
			return p, true
		}
	}
	return FactorPeriod{}, false
}

// FactorSeriesBuilder builds a [FactorSeries].
type FactorSeriesBuilder struct{ s FactorSeries }

// NewFactorSeries starts a series whose parts are rounded as r says.
func NewFactorSeries(r Rounding) *FactorSeriesBuilder {
	return &FactorSeriesBuilder{s: FactorSeries{rounding: r}}
}

// Period adds a daily factor for the days from through to, both included.
// annual is the rate the factor was worked out from, for the working.
func (b *FactorSeriesBuilder) Period(from, to Date, factor Factor, annual money.Rate, source string) *FactorSeriesBuilder {
	b.s.periods = append(b.s.periods, FactorPeriod{from: from, to: to, factor: factor, annual: annual, source: source})
	return b
}

// Build checks the rounding is known, and that every period has a factor
// and a source, runs forward, and starts the day after the one before ends.
func (b *FactorSeriesBuilder) Build() (FactorSeries, error) {
	ps := append([]FactorPeriod(nil), b.s.periods...)
	switch {
	case b.s.rounding != RoundToCent && b.s.rounding != RoundToDollar:
		return FactorSeries{}, errors.New("estimated: a factor series needs the form's rounding")
	case len(ps) == 0:
		return FactorSeries{}, errors.New("estimated: a factor series needs at least one period")
	}
	for i, p := range ps {
		switch {
		case p.from.IsZero() || p.to.IsZero() || p.to.Before(p.from):
			return FactorSeries{}, fmt.Errorf("estimated: factor period %v to %v does not run forward", p.from, p.to)
		case p.factor.units == 0:
			return FactorSeries{}, fmt.Errorf("estimated: the period from %v has no factor", p.from)
		case p.annual < 0:
			return FactorSeries{}, fmt.Errorf("estimated: rate %v is negative", p.annual)
		case p.source == "":
			return FactorSeries{}, fmt.Errorf("estimated: the factor from %v has no source", p.from)
		case i > 0 && p.from != ps[i-1].to.AddDays(1):
			return FactorSeries{}, fmt.Errorf("estimated: the factor from %v does not follow the period ending %v", p.from, ps[i-1].to)
		}
	}
	return FactorSeries{periods: ps, rounding: b.s.rounding}, nil
}

// DailyFactor charges principal at a printed daily factor for the days after
// from up to and including to, one part for each period the days fall in.
//
// Each part is principal × days × factor, rounded as the series says. This
// is REV-1630 lines 14a to 15c: the days in each calendar year times the
// factor REV-1611 prints for that year, in whole dollars.
//
// unrated is the first day the series has no factor for, or the zero Date.
// The parts stop the day before it, and the caller must report the gap.
func DailyFactor(principal money.USCents, from, to Date, s FactorSeries) (parts []PenaltyPart, unrated Date, err error) {
	cur := from
	for cur.Before(to) {
		day := cur.AddDays(1)
		p, ok := s.On(day)
		if !ok {
			return parts, day, nil
		}
		end := earlier(to, p.to)
		days := end.DaysSince(cur)
		part, err := NewPenaltyPart(PartInterest, principal, p.annual).
			Span(cur, end).Days(days).Charge(p.factor.Of(principal, days, s.rounding)).Source(p.source).Build()
		if err != nil {
			return nil, Date{}, err
		}
		parts = append(parts, part)
		cur = end
	}
	return parts, Date{}, nil
}

// SpanFactor charges principal once at a factor a form prints for a whole
// span, as Maryland's Form 502UP prints one for each span between due dates.
// annual is the rate the factor stands for, for the working.
func SpanFactor(principal money.USCents, from, to Date, f Factor, annual money.Rate, r Rounding, source string) (PenaltyPart, error) {
	if r != RoundToCent && r != RoundToDollar {
		return PenaltyPart{}, errors.New("estimated: a factor's charge needs the form's rounding")
	}
	if f.units == 0 {
		return PenaltyPart{}, errors.New("estimated: no factor given")
	}
	return NewPenaltyPart(PartInterest, principal, annual).
		Span(from, to).Days(to.DaysSince(from)).Charge(f.Of(principal, 1, r)).Source(source).Build()
}
