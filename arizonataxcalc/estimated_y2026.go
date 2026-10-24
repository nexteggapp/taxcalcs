package arizonataxcalc

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
//   - the $75,000 and $150,000 income thresholds, the 90% and 100% tests,
//     the due dates and the February 1, 2027 file-and-pay date: the 2026
//     Form 140ES and its instructions, and A.R.S. § 43-581(A);
//   - the $1,000 Arizona threshold: A.R.S. § 43-581(E)(2) and Form 221;
//   - the $1,000 federal exception, the line arithmetic, the quarterly rate
//     periods, the January 1 compounding, the April 15 stop and the
//     annualised periods and percentages: the 2025 Form 221 and its
//     instructions, which are the latest published;
//   - the 10% cap: A.R.S. § 42-1125(Q) and Form 221 line 29;
//   - the 6% and 7% rates for 2026: the department's Interest Rates page.
//
// The 2026 Form 221 is not published. The 2025 form's arithmetic is used with
// 2026's dates and rates; its printed 7% is 2025's rate, and the divergence
// "The 2026 rates" says so.
//
// The rate from January 1, 2027 is not published. The series ends on
// December 31, 2026 and the penalty reports the gap. April 15, 2027 is the
// 2025 form's April 15 a year on.
//
// No Arizona holiday list is checked in. The four 2026 due dates the 140ES
// prints are weekdays, and the test checks the computed dates against them.

func init() {
	d := estimated.MustDate

	calendar, err := estimated.NewCalendar("weekends only; no Arizona holiday list is checked in").Build()
	if err != nil {
		panic(err)
	}

	// Form 221 Part B charges one rate period per calendar quarter. The
	// department's page publishes July to December 2026 as one row at 7%; it
	// is split by quarter here as the form splits its rate periods. The
	// series starts with the quarter the first installment falls in.
	const page = "ADOR, Interest Rates"
	rates, err := estimated.NewRateSeries().
		Period(d(2026, time.April, 1), d(2026, time.June, 30), money.Percent(6), page+", 4/1/2026 - 6/30/2026").
		Period(d(2026, time.July, 1), d(2026, time.September, 30), money.Percent(7), page+", 7/1/2026 - 12/31/2026").
		Period(d(2026, time.October, 1), d(2026, time.December, 31), money.Percent(7), page+", 7/1/2026 - 12/31/2026").
		Build()
	if err != nil {
		panic(err)
	}

	registerEstimated(estimatedYear{
		year:             2026,
		incomeThreshold:  75_000 * money.Dollar,
		jointThreshold:   150_000 * money.Dollar,
		taxThreshold:     1_000 * money.Dollar,
		federalThreshold: 1_000 * money.Dollar,
		thisYearPercent:  money.Percent(90),
		priorYearPercent: money.Percent(100),
		penaltyCap:       money.Percent(10),
		dues: []estimated.Date{
			d(2026, time.April, 15),
			d(2026, time.June, 15),
			d(2026, time.September, 15),
			d(2027, time.January, 15),
		},
		calendar:     calendar,
		fileAndPayBy: d(2027, time.February, 1),
		stop:         d(2027, time.April, 15),
		periodEnds: []estimated.Date{
			d(2026, time.March, 31),
			d(2026, time.May, 31),
			d(2026, time.August, 31),
			d(2026, time.December, 31),
		},
		annualisedPercentages: []money.Rate{2250, 4500, 6750, 9000},
		rates:                 rates,
	})
}
