package virginiataxcalc

import (
	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/money"
)

// Calendar is the holiday calendar the year's rules move dates by. It is
// exported to the tests only, so they can show a date moving: no 2026 date
// the rules use falls on a weekend or holiday.
func Calendar(year int) estimated.Calendar { return estimatedYears[year].calendar }

// RatePeriod is one period of the year's rates, for the tests.
type RatePeriod struct {
	From, To estimated.Date
	Rate     money.Rate
	Factor   string // the printed daily factor, or empty
}

// Rates are the year's rate periods, for the tests.
func Rates(year int) []RatePeriod {
	var out []RatePeriod
	for _, p := range estimatedYears[year].rates {
		r := RatePeriod{From: p.from, To: p.to, Rate: p.rate}
		if p.printed {
			r.Factor = p.factor.Periods()[0].Factor().String()
		}
		out = append(out, r)
	}
	return out
}
