package indianataxcalc

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
//   - the four 2026 due dates: the 2026 Form ES-40;
//   - the $1,000 threshold, the 90%, 100% and 110% tests, the $150,000 and
//     $75,000 steps, the quarters, the installment periods and the 10%
//     penalty: the 2025 Schedule IT-2210 and its instructions, which are the
//     latest published;
//   - the annualised percentages: the 2025 Schedule IT-2210A;
//   - the January 31 file-and-pay date, the weekend rule and the treatment of
//     late payments: Information Bulletin #3 (March 2023);
//   - the zero prior-year tax rule, effective January 1, 2026, and the 10% in
//     the statute: the Indiana Code as quoted by
//     research/estimated-payments/in.md, because iga.in.gov could not be
//     fetched.
//
// No 2026 IT-2210 is published. The 2025 form's arithmetic is used with the
// 2026 dates. The DOR's 2026 Legislative Synopsis lists no 2026 change to the
// threshold, the percentages, the dates or the 10% rate; SEA 243 (2026)
// rewords the penalty's reference to IC 6-8.1-10-2.1(b) from July 1, 2026
// without changing it.
//
// No Indiana holiday list is checked in. The four 2026 due dates and
// February 1, 2027 are weekdays, so only weekends move a date, and the tests
// check the computed dates against the printed ones.

func init() {
	d := estimated.MustDate

	calendar, err := estimated.NewCalendar("weekends only; no Indiana holiday list is checked in").Build()
	if err != nil {
		panic(err)
	}

	registerEstimated(estimatedYear{
		year:         2026,
		threshold:    1_000 * money.Dollar,
		thisYear:     money.Percent(90),
		prior:        money.Percent(100),
		priorHigh:    money.Percent(110),
		step:         150_000 * money.Dollar,
		stepSeparate: 75_000 * money.Dollar,
		rate:         money.Percent(10),
		dues: []estimated.Date{
			d(2026, time.April, 15),
			d(2026, time.June, 15),
			d(2026, time.September, 15),
			d(2027, time.January, 15),
		},
		calendar:     calendar,
		fileAndPayBy: d(2027, time.January, 31),
		periodEnds: []estimated.Date{
			d(2026, time.March, 31),
			d(2026, time.May, 31),
			d(2026, time.August, 31),
			d(2026, time.December, 31),
		},
		annualised:   []money.Rate{2250, 4500, 6750, 9000},
		zeroPriorTax: true,
	})
}
