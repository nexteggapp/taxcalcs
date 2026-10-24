package ohiotaxcalc

import "github.com/nexteggapp/taxcalcs/estimated"

// What the external tests reach that a caller does not. None of it is part of
// the package's API.

// FactorFor is the IT/SD 2210 line 15 factor a year's rules work out for the
// days after from up to and including to, at the given rates, as printed. It
// is empty when no day of the span has a rate.
func FactorFor(year int, from, to estimated.Date, rates estimated.RateSeries) (string, error) {
	y := estimatedYears[year]
	y.rates = rates
	sf, err := y.factor(from, to)
	if err != nil || !sf.charged {
		return "", err
	}
	return sf.factor.String(), nil
}

// CalendarFor is the holiday calendar a year's rules move due dates by.
func CalendarFor(year int) estimated.Calendar { return estimatedYears[year].calendar }

// RatesFor is the rate series a year's rules charge at.
func RatesFor(year int) estimated.RateSeries { return estimatedYears[year].rates }
