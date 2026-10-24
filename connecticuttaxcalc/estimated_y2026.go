package connecticuttaxcalc

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
//   - the $1,000 threshold, the 90% and 100% tests, the 25% installment, the
//     due dates, the weekend and holiday rule, the January 31 file-and-pay
//     date, the April 15 stop and the 1% a month: Form CT-1040ES (Rev. 01/26),
//     for 2026;
//   - the line arithmetic, the installment, withholding and PE Tax Credit
//     percentages, the annualised periods and percentages, the months from the
//     16th to the 15th, Boxes E, F and G and the January 31 rule's effect on
//     Schedule B: Form CT-2210 (Rev. 12/25) and its instructions, for 2025,
//     which are the latest published;
//   - the same rules in Conn. Gen. Stat. §§ 12-701(a)(12) and 12-722, and the
//     2026 Supplement, which leaves both unchanged.
//
// The 2026 CT-2210 is not published. The 2025 form's arithmetic is used with
// the 2026 dates from CT-1040ES, and its Schedule A period ends a year on. No
// figure it prints is a yearly rate: the 1% a month is § 12-722(a)'s.
//
// The rate is set by statute, not by notice, so the months from January 1,
// 2027 to the April 15, 2027 stop have a rate and nothing is reported as
// unpublished.
//
// No Connecticut holiday list is checked in. § 12-722(c) does not move a due
// date; CT-1040ES does, for a Saturday, Sunday or legal holiday. The four 2026
// due dates it prints are weekdays, and the test checks the computed dates
// against them. A later year whose date falls on a Connecticut holiday needs
// that list before it ships.

func init() {
	d := estimated.MustDate

	calendar, err := estimated.NewCalendar("weekends only; no Connecticut holiday list is checked in").Build()
	if err != nil {
		panic(err)
	}

	registerEstimated(estimatedYear{
		year:           2026,
		threshold:      1_000 * money.Dollar,
		thisYearShare:  money.Percent(90),
		priorYearShare: money.Percent(100),
		shares:         []money.Rate{money.Percent(25), money.Percent(25), money.Percent(25), money.Percent(25)},
		annualised:     []money.Rate{2250, 4500, 6750, 9000},
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
		monthlyRate: money.Percent(1),
	})
}
