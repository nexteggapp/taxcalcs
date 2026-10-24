package model

import (
	"sort"
	"time"

	"github.com/nexteggapp/taxcalcs/taxyear/internal/civil"
)

// Cadence is how often a source pays, read from the dates it paid so far:
// a number of months, or for gaps under 25 days a number of days.
type Cadence struct {
	months int
	days   int
}

// CadenceOf returns the median gap between dates, so monthly, quarterly and
// semiannual payers each fall out of their own evidence. With one date, or
// none, it is annual. A gap of 25 days or more is counted in months.
func CadenceOf(dates []civil.Date) Cadence {
	ds := append([]civil.Date(nil), dates...)
	sort.Slice(ds, func(i, j int) bool { return ds[i].Before(ds[j]) })
	var gaps []int
	for i := 1; i < len(ds); i++ {
		if g := ds[i].DaysFrom(ds[i-1]); g > 0 {
			gaps = append(gaps, g)
		}
	}
	if len(gaps) == 0 {
		return Cadence{months: 12}
	}
	sort.Ints(gaps)
	median := gaps[len(gaps)/2]
	if len(gaps)%2 == 0 {
		median = (gaps[len(gaps)/2-1] + gaps[len(gaps)/2]) / 2
	}
	if median < 25 {
		return Cadence{days: median}
	}
	months := int(float64(median)/30.44 + 0.5)
	return Cadence{months: max(1, months)}
}

// Months is the cadence in months, or 0 for one counted in days.
func (c Cadence) Months() int { return c.months }

// Next returns the date one step after d. A month step keeps the day of
// the month, a month's last day staying its last day, and clamps to a
// shorter month's end.
func (c Cadence) Next(d civil.Date) civil.Date {
	if c.months == 0 {
		return d.AddDays(c.days)
	}
	m := int(d.Month()) - 1 + c.months
	year, month := d.Year()+m/12, time.Month(m%12+1)
	end := civil.EndOfMonth(year, month)
	if d == civil.EndOfMonth(d.Year(), d.Month()) || d.Day() >= end.Day() {
		return end
	}
	return civil.Must(year, month, d.Day())
}

// Through returns the dates after the last one, step by step, up to and
// including until.
func (c Cadence) Through(last, until civil.Date) []civil.Date {
	var out []civil.Date
	for d := c.Next(last); !d.After(until); d = c.Next(d) {
		out = append(out, d)
	}
	return out
}
