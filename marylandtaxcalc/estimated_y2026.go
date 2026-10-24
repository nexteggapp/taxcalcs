package marylandtaxcalc

import (
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/money"
)

// Estimated tax for tax year 2026.
//
// Every figure here is checked by estimated_test.go against an extract in
// testdata/, listed in testdata/SOURCES.md:
//
//   - the $500 threshold, the quarter due by each date, the four due dates,
//     the January 31 file-and-pay date and the weekend rule: the 2026 Payment
//     Voucher Worksheet (PVW), with Tax-General §§ 1-201, 10-815, 10-820 and
//     10-902;
//   - the $500 exception, the 90% and 110% tests, the first-time filer rule,
//     the income periods, withholding on its dates, the cumulative columns and
//     the months-late rule: Form 502UP (2025) and its instructions, the latest
//     published, with Tax-General §§ 13-602 and 13-604;
//   - the 10.8133% rate for 2026: Form 502UP (2025) and the 2025 resident
//     booklet;
//   - the 9.995% rate for 2027: Maryland Register 53:18, September 4, 2026.
//
// The 2026 Form 502UP is not published, so the interest factors it will
// print for 2026's spans are not known and none is carried here. The 2025
// form's factors are for 2025's rates and are not used. The interest is
// charged by the month under § 13-604(a), and the penalty says so in
// Required.
//
// No Maryland holiday list is checked in. The four 2026 due dates the PVW
// prints, and April 15, 2027, are weekdays; January 31, 2027 is a Sunday and
// moves to Monday, February 1 under § 1-201(b).

func init() {
	d := estimated.MustDate

	calendar, err := estimated.NewCalendar("weekends only; no Maryland holiday list is checked in").Build()
	if err != nil {
		panic(err)
	}

	rates, err := estimated.NewRateSeries().
		FinePeriod(d(2026, time.January, 1), d(2026, time.December, 31), 108_133, "Form 502UP (2025), Line 16: 0.9011% a month after December 31, 2025; 2025 resident booklet: 10.8133% a year").
		FinePeriod(d(2027, time.January, 1), d(2027, time.December, 31), 99_950, "Maryland Register 53:18 (September 4, 2026): 9.995% a year for 2027").
		Build()
	if err != nil {
		panic(err)
	}

	registerEstimated(estimatedYear{
		year:           2026,
		threshold:      500 * money.Dollar,
		thisYearShare:  money.Percent(90),
		priorYearShare: money.Percent(110),
		dues: []estimated.Date{
			d(2026, time.April, 15),
			d(2026, time.June, 15),
			d(2026, time.September, 15),
			d(2027, time.January, 15),
		},
		calendar:     calendar,
		fileAndPayBy: d(2027, time.January, 31),
		stop:         d(2027, time.April, 15),
		periodEnds: []estimated.Date{
			d(2026, time.March, 31),
			d(2026, time.May, 31),
			d(2026, time.August, 31),
			d(2026, time.December, 31),
		},
		rates: rates,
	})
}
