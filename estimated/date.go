package estimated

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

// Date is a calendar day, with no time of day and no zone.
//
// Tax deadlines are days. A time.Time carries an instant and a location, and
// two of them for the same deadline can compare unequal because one was made
// in UTC and the other in Pacific time. Date holds only the year, month and
// day, so == means the same day.
//
// The zero Date is "no date", which [Date.IsZero] reports.
type Date struct {
	year  int
	month time.Month
	day   int
}

// NewDate returns the date, or an error if the day does not exist. February
// 30th is an error rather than March 2nd.
func NewDate(year int, month time.Month, day int) (Date, error) {
	t := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	if t.Year() != year || t.Month() != month || t.Day() != day {
		return Date{}, fmt.Errorf("estimated: %04d-%02d-%02d is not a date", year, int(month), day)
	}
	return Date{year, month, day}, nil
}

// MustDate is [NewDate] for a date written in source, which panics on a day
// that does not exist.
func MustDate(year int, month time.Month, day int) Date {
	d, err := NewDate(year, month, day)
	if err != nil {
		panic(err)
	}
	return d
}

// DateOf returns the calendar day of t in t's own location.
func DateOf(t time.Time) Date { return Date{t.Year(), t.Month(), t.Day()} }

func (d Date) Year() int             { return d.year }
func (d Date) Month() time.Month     { return d.month }
func (d Date) Day() int              { return d.day }
func (d Date) IsZero() bool          { return d == Date{} }
func (d Date) Weekday() time.Weekday { return d.Time().Weekday() }

// Time returns midnight UTC at the start of the day.
func (d Date) Time() time.Time { return time.Date(d.year, d.month, d.day, 0, 0, 0, 0, time.UTC) }

// Compare returns -1, 0 or +1 as d is before, the same as, or after o.
func (d Date) Compare(o Date) int { return d.Time().Compare(o.Time()) }

func (d Date) Before(o Date) bool { return d.Compare(o) < 0 }
func (d Date) After(o Date) bool  { return d.Compare(o) > 0 }

// AddDays returns the date n days later, or earlier for a negative n.
func (d Date) AddDays(n int) Date { return DateOf(d.Time().AddDate(0, 0, n)) }

// AddMonths returns the same day n months later. A day the later month does
// not have is clamped to its last day, so January 31st plus one month is
// February 28th rather than March 3rd.
func (d Date) AddMonths(n int) Date {
	first := time.Date(d.year, d.month+time.Month(n), 1, 0, 0, 0, 0, time.UTC)
	last := first.AddDate(0, 1, -1).Day()
	day := d.day
	if day > last {
		day = last
	}
	return Date{first.Year(), first.Month(), day}
}

// DaysSince returns the number of days from o to d: positive when d is later.
// April 15th to June 10th is 56, which is how Form 2210 and FTB 5805 count.
func (d Date) DaysSince(o Date) int {
	return int(d.Time().Sub(o.Time()).Hours() / 24)
}

// String renders the date as 2026-04-15.
func (d Date) String() string {
	if d.IsZero() {
		return "no date"
	}
	return fmt.Sprintf("%04d-%02d-%02d", d.year, int(d.month), d.day)
}

// Long renders the date the way a form prints it: April 15, 2026.
func (d Date) Long() string {
	if d.IsZero() {
		return "no date"
	}
	return fmt.Sprintf("%s %d, %d", d.month, d.day, d.year)
}

func later(a, b Date) Date {
	if a.After(b) {
		return a
	}
	return b
}

func earlier(a, b Date) Date {
	if a.Before(b) {
		return a
	}
	return b
}

// daysInYear is 365, or 366 in a leap year. Form 2210 and FTB 5805 both divide
// a year's interest by the days in that year.
func daysInYear(year int) int {
	return Date{year, time.December, 31}.DaysSince(Date{year - 1, time.December, 31})
}

// Holiday is a day a tax authority treats as a legal holiday, and its name.
type Holiday struct {
	on   Date
	name string
}

func (h Holiday) On() Date     { return h.on }
func (h Holiday) Name() string { return h.name }

// Calendar is the set of legal holidays a jurisdiction moves a due date for.
// Saturdays and Sundays are always skipped and are not listed.
//
// Which days are holidays differs by jurisdiction. Federal deadlines move for
// any legal holiday in the District of Columbia, which includes DC Emancipation
// Day; Oregon's and Rhode Island's move for their own. Each jurisdiction
// builds the calendar its own documents publish.
type Calendar struct {
	holidays []Holiday
	source   string
}

// Holidays returns the holidays, earliest first.
func (c Calendar) Holidays() []Holiday { return append([]Holiday(nil), c.holidays...) }

// Source cites the document that lists the holidays.
func (c Calendar) Source() string { return c.source }

// IsHoliday reports whether d is a listed holiday, and its name.
func (c Calendar) IsHoliday(d Date) (string, bool) {
	for _, h := range c.holidays {
		if h.on == d {
			return h.name, true
		}
	}
	return "", false
}

// IsBusinessDay reports whether d is neither a weekend nor a holiday.
func (c Calendar) IsBusinessDay(d Date) bool {
	switch d.Weekday() {
	case time.Saturday, time.Sunday:
		return false
	}
	_, holiday := c.IsHoliday(d)
	return !holiday
}

// NextBusinessDay returns d if it is a business day, and otherwise the next
// day that is. This is 26 U.S.C. § 7503 and the rule every state here follows.
func (c Calendar) NextBusinessDay(d Date) Date {
	for !c.IsBusinessDay(d) {
		d = d.AddDays(1)
	}
	return d
}

// CalendarBuilder builds a [Calendar].
type CalendarBuilder struct{ c Calendar }

// NewCalendar starts a calendar whose holidays come from source.
func NewCalendar(source string) *CalendarBuilder {
	return &CalendarBuilder{c: Calendar{source: source}}
}

// Holiday adds a holiday.
func (b *CalendarBuilder) Holiday(on Date, name string) *CalendarBuilder {
	b.c.holidays = append(b.c.holidays, Holiday{on: on, name: name})
	return b
}

// Build checks the calendar names its source and has no undated or repeated
// holiday.
func (b *CalendarBuilder) Build() (Calendar, error) {
	if b.c.source == "" {
		return Calendar{}, errors.New("estimated: a calendar needs the source that lists its holidays")
	}
	hs := append([]Holiday(nil), b.c.holidays...)
	sort.Slice(hs, func(i, j int) bool { return hs[i].on.Before(hs[j].on) })
	for i, h := range hs {
		if h.on.IsZero() || h.name == "" {
			return Calendar{}, errors.New("estimated: a holiday needs a date and a name")
		}
		if i > 0 && hs[i-1].on == h.on {
			return Calendar{}, fmt.Errorf("estimated: %v is listed twice", h.on)
		}
	}
	return Calendar{holidays: hs, source: b.c.source}, nil
}
