package rhodeislandtaxcalc

import "github.com/nexteggapp/taxcalcs/estimated"

// Holidays exposes a year's holiday calendar to the external tests, so that
// each date can be checked against the rule in § 25-1-1 that makes it one.
func Holidays(year int) []estimated.Holiday { return estimatedYears[year].calendar.Holidays() }
