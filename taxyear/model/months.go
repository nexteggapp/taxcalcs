package model

import (
	"time"

	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/taxyear/internal/civil"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
)

// Months returns the last day of each month of the tax year: the slots of a
// model whose slots are months.
func (c *Context) Months() []civil.Date {
	out := make([]civil.Date, 0, 12)
	for m := time.January; m <= time.December; m++ {
		out = append(out, civil.EndOfMonth(c.year, m))
	}
	return out
}

// EvidenceThrough is the last day of the last month that has ended by
// as_of: a month is evidence once it has ended. It is zero before the end of
// January.
func (c *Context) EvidenceThrough() civil.Date {
	var last civil.Date
	for _, m := range c.Months() {
		if !m.After(c.asOf) {
			last = m
		}
	}
	return last
}

// EvidenceMonths is how many months have ended by as_of.
func (c *Context) EvidenceMonths() int {
	n := 0
	for _, m := range c.Months() {
		if !m.After(c.asOf) {
			n++
		}
	}
	return n
}

// MonthOf returns the last day of the month a date falls in.
func MonthOf(d civil.Date) civil.Date { return civil.EndOfMonth(d.Year(), d.Month()) }

// Percent reads a percentage Quantity as a rate in millionths, or false
// when unset.
func Percent(q *commonv1.Quantity) (money.FineRate, bool) {
	if q == nil {
		return 0, false
	}
	r := q.GetUnits() * 10_000
	for i := int32(0); i < q.GetScale(); i++ {
		r /= 10
	}
	return money.FineRate(r), true
}

// OfFine applies a rate in millionths to an amount, rounding half away from
// zero.
func OfFine(r money.FineRate, a money.USCents) money.USCents {
	return Scale(a, money.USCents(r), 1_000_000)
}

// Decimal reads a Quantity as units and a power of ten.
func Decimal(q *commonv1.Quantity) (units int64, scale int32) { return q.GetUnits(), q.GetScale() }

// FormsDue is when the year's W-2s and 1099s are due to arrive: January 31
// of the next year, moved to the next weekday. 26 U.S.C. § 6051(a) and
// § 7503.
func FormsDue(year int) civil.Date {
	d := civil.Must(year+1, time.January, 31)
	switch d.Weekday() {
	case time.Saturday:
		return d.AddDays(2)
	case time.Sunday:
		return d.AddDays(1)
	}
	return d
}
