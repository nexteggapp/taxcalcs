package districtofcolumbiataxcalc

import (
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/money"
)

// District of Columbia estimated tax for tax year 2026.
//
// Every figure here is checked by estimated_test.go against an extract in
// testdata/, listed in testdata/SOURCES.md:
//
//   - the $100 threshold, the 90% and 110% tests, the due dates and the
//     weekend and holiday rule: D-40ES (REV. 03/2026), the 2026 booklet;
//   - the line arithmetic, the $100 and 10% exceptions, the full-year
//     residence condition and the four interest factors: D-2210 (revised
//     05/2025), the form for 2025, which is the latest published;
//   - the 10% rate: D.C. Code § 47-4201(d)(2); the April 15 stop and the
//     annualisation periods of 3, 5 and 8 months: D.C. Code § 47-4203;
//   - the later declaration dates for a requirement first met after April 1:
//     D.C. Code § 47-1812.08(i)(4).
//
// No 2026 D-2210 is published. The 2025 form's arithmetic is used with the
// 2026 figures. Its factors are not 2025 figures: they are the District's
// fixed 10% rate for spans of 61, 92, 122 and 90 days between the due dates
// and April 15, and 2026's spans have the same lengths, so the printed
// factors are used as they stand.
//
// No District holiday list is checked in. D-40ES moves a date that falls on a
// Saturday, Sunday or legal holiday without saying which holidays count. The
// four 2026 due dates D-40ES prints are weekdays, and the test checks the
// computed dates against them.

func init() {
	d := estimated.MustDate

	calendar, err := estimated.NewCalendar("weekends only; D-40ES (REV. 03/2026) moves a date for a legal holiday without listing them").Build()
	if err != nil {
		panic(err)
	}

	var factors []estimated.Factor
	for _, printed := range []string{".0175", ".0265", ".0351", ".0259"} {
		f, err := estimated.ParseFactor(printed)
		if err != nil {
			panic(err)
		}
		factors = append(factors, f)
	}

	registerEstimated(estimatedYear{
		year:            2026,
		threshold:       100 * money.Dollar,
		thisYearPercent: money.Percent(90),
		priorPercent:    money.Percent(110),
		remaining:       money.Percent(10),
		dues: []estimated.Date{
			d(2026, time.April, 15),
			d(2026, time.June, 15),
			d(2026, time.September, 15),
			d(2027, time.January, 15),
		},
		laterGroups: []estimated.Date{
			d(2026, time.April, 2),
			d(2026, time.June, 2),
			d(2026, time.September, 2),
		},
		calendar: calendar,
		stop:     d(2027, time.April, 15),
		factors:  factors,
		rate:     money.Percent(10),
		periodEnds: []estimated.Date{
			d(2026, time.March, 31),
			d(2026, time.May, 31),
			d(2026, time.August, 31),
			d(2026, time.December, 31),
		},
	})
}
