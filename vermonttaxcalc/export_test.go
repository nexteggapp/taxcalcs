package vermonttaxcalc

import "github.com/nexteggapp/taxcalcs/estimated"

// Calendar is the holiday calendar the year's due dates move by, for the
// tests.
func Calendar(year int) estimated.Calendar { return estimatedYears[year].calendar }
