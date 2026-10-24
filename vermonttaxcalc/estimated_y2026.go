package vermonttaxcalc

import (
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/money"
)

// Estimated tax for tax year 2026.
//
// The figures come from these documents:
//
//   - the 90% and 100% shares and the four due dates: the 2026 IN-114 and
//     its instructions, and 32 V.S.A. §§ 5851 and 5852;
//   - the $500 threshold: the 2025 IN-111 instructions, line 31;
//   - the quarter shares, the 25% of withholding, the April 15 stop, the
//     365-day basis and the 1% a month: the 2025 IN-152, the latest
//     published;
//   - the 7.75% rate for 2026: the Department's memo of November 4, 2025;
//   - the holidays: the Department's State Holiday Closures page.
//
// estimated_test.go checks them against the statute extracts in testdata/
// and, for the Department's documents, which tax.vermont.gov would not
// serve, against the quotes in testdata/research-vt.md. The April 15 stop,
// the 365-day basis and the holiday dates have no quote there and are not
// checked against any text. testdata/SOURCES.md lists every such gap.
//
// The 2026 IN-152 is not published. The 2025 worksheet's arithmetic is used
// with the 2026 dates and rate.
//
// The rate for 2027 is set under 32 V.S.A. § 3108 by December 15, 2026 and
// is not published. The rates end on December 31, 2026 and the penalty
// reports the gap.

func init() {
	d := estimated.MustDate

	// The State Holiday Closures page's 2026 and 2027 columns. Its 2026
	// Thanksgiving row reads "Thursday, November 28" and "Friday, November
	// 29", which are a Saturday and a Sunday in 2026, so those two are left
	// out. No due date is near them.
	cb := estimated.NewCalendar("Vermont Department of Taxes, State Holiday Closures (tax.vermont.gov/node/10121), retrieved 2026-10-20")
	for _, h := range []struct {
		on   estimated.Date
		name string
	}{
		{d(2026, time.January, 1), "New Year's Day"},
		{d(2026, time.January, 19), "Martin Luther King, Jr. Day"},
		{d(2026, time.February, 16), "Presidents' Day"},
		{d(2026, time.March, 3), "Town Meeting Day"},
		{d(2026, time.May, 25), "Memorial Day"},
		{d(2026, time.July, 3), "Independence Day"},
		{d(2026, time.September, 7), "Labor Day"},
		{d(2026, time.October, 12), "Indigenous People's Day"},
		{d(2026, time.November, 11), "Veterans' Day"},
		{d(2026, time.December, 25), "Christmas Day"},
		{d(2027, time.January, 1), "New Year's Day"},
		{d(2027, time.January, 18), "Martin Luther King, Jr. Day"},
		{d(2027, time.February, 15), "Presidents' Day"},
		{d(2027, time.March, 2), "Town Meeting Day"},
		{d(2027, time.May, 31), "Memorial Day"},
		{d(2027, time.June, 18), "Juneteenth"},
		{d(2027, time.July, 5), "Independence Day"},
		{d(2027, time.September, 6), "Labor Day"},
		{d(2027, time.October, 11), "Indigenous People's Day"},
		{d(2027, time.November, 11), "Veterans' Day"},
		{d(2027, time.November, 25), "Thanksgiving Day"},
		{d(2027, time.November, 26), "Day After Thanksgiving"},
		{d(2027, time.December, 24), "Christmas Day"},
	} {
		cb.Holiday(h.on, h.name)
	}
	calendar, err := cb.Build()
	if err != nil {
		panic(err)
	}

	rates, err := estimated.NewRateSeries().
		Period(d(2026, time.January, 1), d(2026, time.December, 31), 775,
			"Vermont Department of Taxes, 2026 Interest Rate memo, November 4, 2025: 7.75% for 2026 under 32 V.S.A. § 3108").
		Build()
	if err != nil {
		panic(err)
	}

	quarter := money.Percent(25)
	registerEstimated(estimatedYear{
		year:      2026,
		thisYear:  money.Percent(90),
		priorYear: money.Percent(100),
		threshold: 500 * money.Dollar,
		shares:    []money.Rate{quarter, quarter, quarter, quarter},
		dues: []estimated.Date{
			d(2026, time.April, 15),
			d(2026, time.June, 15),
			d(2026, time.September, 15),
			d(2027, time.January, 15),
		},
		calendar: calendar,
		stop:     d(2027, time.April, 15),
		rates:    rates,
		dayBasis: 365,
		monthly:  money.Percent(1),
	})
}
