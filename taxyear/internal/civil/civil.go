// Package civil is a calendar date with no time zone, for the dates a tax
// year's items fall on.
package civil

import (
	"fmt"
	"time"

	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
)

// Date is a calendar day. The zero Date is no date.
type Date struct {
	t time.Time
}

// New returns the date, or an error when the day does not exist.
func New(year int, month time.Month, day int) (Date, error) {
	t := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	if t.Year() != year || t.Month() != month || t.Day() != day {
		return Date{}, fmt.Errorf("civil: %04d-%02d-%02d is not a day", year, int(month), day)
	}
	return Date{t: t}, nil
}

// Must is New for a date known to exist. It panics otherwise.
func Must(year int, month time.Month, day int) Date {
	d, err := New(year, month, day)
	if err != nil {
		panic(err)
	}
	return d
}

// FromProto converts a proto date. A nil date is the zero Date and no error;
// a partial or impossible one is an error.
func FromProto(d *commonv1.Date) (Date, error) {
	if d == nil {
		return Date{}, nil
	}
	return New(int(d.GetYear()), time.Month(d.GetMonth()), int(d.GetDay()))
}

// Proto returns the date as a proto, or nil for the zero Date.
func (d Date) Proto() *commonv1.Date {
	if d.IsZero() {
		return nil
	}
	return &commonv1.Date{Year: int32(d.t.Year()), Month: int32(d.t.Month()), Day: int32(d.t.Day())}
}

// IsZero reports whether d is no date.
func (d Date) IsZero() bool { return d.t.IsZero() }

// Year, Month and Day are the date's parts.
func (d Date) Year() int { return d.t.Year() }

// Month is the date's month.
func (d Date) Month() time.Month { return d.t.Month() }

// Day is the date's day of the month.
func (d Date) Day() int { return d.t.Day() }

// Weekday is the date's day of the week.
func (d Date) Weekday() time.Weekday { return d.t.Weekday() }

// AddDays returns the date n days later, or earlier for a negative n.
func (d Date) AddDays(n int) Date { return Date{t: d.t.AddDate(0, 0, n)} }

// Compare returns -1, 0 or 1 as d is before, the same as or after o.
func (d Date) Compare(o Date) int { return d.t.Compare(o.t) }

// Before reports whether d is before o.
func (d Date) Before(o Date) bool { return d.t.Before(o.t) }

// After reports whether d is after o.
func (d Date) After(o Date) bool { return d.t.After(o.t) }

// DaysFrom returns the days from o to d: positive when d is later.
func (d Date) DaysFrom(o Date) int { return int(d.t.Sub(o.t).Hours() / 24) }

// String is the ISO form, 2026-08-14, or "" for the zero Date.
func (d Date) String() string {
	if d.IsZero() {
		return ""
	}
	y, m, day := d.t.Date()
	if y < 0 || y > 9999 {
		return d.t.Format("2006-01-02")
	}
	// Written by hand: time.Format is slow enough to show when dates key
	// maps.
	b := []byte("0000-00-00")
	for i, v := range []int{y / 1000, y / 100 % 10, y / 10 % 10, y % 10} {
		b[i] = byte('0' + v)
	}
	b[5], b[6] = byte('0'+int(m)/10), byte('0'+int(m)%10)
	b[8], b[9] = byte('0'+day/10), byte('0'+day%10)
	return string(b)
}

// Long is the date as a sentence writes it, "Aug 14, 2026".
func (d Date) Long() string { return d.t.Format("Jan 2, 2006") }

// Short is the date without its year, "Aug 14".
func (d Date) Short() string { return d.t.Format("Jan 2") }

// EndOfMonth returns the last day of the month of year and month.
func EndOfMonth(year int, month time.Month) Date {
	return Date{t: time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC)}
}

// BusinessDayOnOrBefore returns d, or the Friday before when d falls on a
// weekend, as payroll moves a pay date. Holidays are not moved.
func (d Date) BusinessDayOnOrBefore() Date {
	switch d.Weekday() {
	case time.Saturday:
		return d.AddDays(-1)
	case time.Sunday:
		return d.AddDays(-2)
	}
	return d
}

// Later returns the later of a and b, treating the zero Date as earliest.
func Later(a, b Date) Date {
	if a.After(b) {
		return a
	}
	return b
}
