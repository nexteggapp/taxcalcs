package northdakotataxcalc

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
//   - the federal condition, the $1,000 prior-year gate, the 90% and 100%
//     tests, the mover rule and the quarters: N.D.C.C. §§ 57-38-62(1) and
//     57-38-63, and Form ND-1ES for 2026, which prints the 2026 dates;
//   - the $1,000 threshold on this year's tax: § 57-38-62(3) and ND-1ES
//     line 10;
//   - Items C and D, the payment application rule, the 12% rate, the day
//     count rounded to three places and the $5.00 rule: Schedule ND-1UT for
//     2025, the latest the department publishes;
//   - the quarter of withholding deemed withheld by each date: ND-1UT
//     line 8; the installments are equal quarters of the required payment,
//     § 57-38-63 and ND-1UT line 7;
//   - the annualized percentages: federal Form 2210 Schedule AI, which
//     ND-1UT Item B adopts;
//   - the weekend and holiday rule: § 1-03-05, with the holidays of
//     § 1-03-01.
//
// The 12% rate is fixed rather than reset each period, so every day to
// April 15, 2027 has a rate.
//
// The calendar holds weekends only. No 2026 due date, and not April 15, 2027,
// falls on a § 1-03-01 holiday, and the test checks the computed dates
// against both the printed ones and that list.
//
// fourthQuarterBy is the federal date for 2026: January 31, 2027 is a Sunday,
// so the next business day, February 1. Schedule ND-1UT for 2025 printed
// February 1, 2026 for the same rule, a Sunday.

func init() {
	calendar, err := estimated.NewCalendar("weekends only; no 2026 due date falls on a holiday in N.D.C.C. § 1-03-01").Build()
	if err != nil {
		panic(err)
	}
	registerEstimated(estimatedYear{
		year:           2026,
		threshold:      1_000 * money.Dollar,
		priorGate:      1_000 * money.Dollar,
		thisYearShare:  money.Percent(90),
		priorYearShare: money.Percent(100),
		shares:         []money.Rate{money.Percent(25), money.Percent(25), money.Percent(25), money.Percent(25)},
		annualised:     []money.Rate{2250, 4500, 6750, 9000},
		dues: []estimated.Date{
			date(2026, time.April, 15),
			date(2026, time.June, 15),
			date(2026, time.September, 15),
			date(2027, time.January, 15),
		},
		calendar: calendar,
		periodEnds: []estimated.Date{
			date(2026, time.March, 31),
			date(2026, time.May, 31),
			date(2026, time.August, 31),
			date(2026, time.December, 31),
		},
		itemD: []estimated.Date{
			date(2026, time.April, 16),
			date(2026, time.June, 1),
			date(2026, time.September, 1),
		},
		fourthQuarterBy: date(2027, time.February, 1),
		stop:            date(2027, time.April, 15),
		rate:            money.Percent(12),
		dayBasis:        365,
		places:          3,
		smallInterest:   5 * money.Dollar,
	})
}
