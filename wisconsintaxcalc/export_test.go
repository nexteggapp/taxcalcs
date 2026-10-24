package wisconsintaxcalc

import "github.com/nexteggapp/taxcalcs/estimated"

// Calendar and Rates expose a year's holiday calendar and rate series to the
// external tests, which check them against the statutes.
func Calendar(year int) estimated.Calendar { return estimatedYears[year].calendar }
func Rates(year int) estimated.RateSeries  { return estimatedYears[year].rates }
