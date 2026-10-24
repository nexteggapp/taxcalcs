package kentuckytaxcalc

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
//   - the $5,000 income test and the $500 threshold: KRS 141.305(1)(a) and
//     (2), and Form 2210-K (2025) line 5;
//   - the 90%, 100% and 110% tests, the $150,000 and $75,000 steps, the due
//     dates, and the September 1 and February 1, 2027 dates of the January
//     note: the 2026 voucher instructions, 42A740-S4 (9-25), with the dates
//     also on the 2026 Form 740-ES vouchers, 42A740ES (10/25);
//   - the 25% installments and withholding, the annualised periods and
//     percentages, the April 15 stop and the 9% rate for 2026: Form 2210-K
//     (2025), 42A740-S1 (10-25), the latest published, used for 2026 because
//     KRS 141.305 is unchanged since 2019;
//   - the weekend rule: KY-TAM-19-02.
//
// revenue.ky.gov refused every download on 2026-10-20, so the department's
// documents are checked against the quotes in
// research/estimated-payments/ky.md, copied into testdata/research-ky.md. The
// statutes and 103 KAR 1:040 are checked against the Legislative Research
// Commission's own text.
//
// The rate from January 1, 2027 is set under KRS 131.183(1)(b)1.b by November
// 15, 2026 and is not published. The series ends on December 31, 2026 and the
// penalty reports the gap.
//
// No Kentucky holiday list is checked in. The four 2026 due dates are
// weekdays, so they are moved for weekends only, and the test checks the
// computed dates against the printed ones. January 31, 2027 is a Sunday, and
// moving it gives the February 1, 2027 the voucher instructions print.

func init() {
	d := estimated.MustDate

	calendar, err := estimated.NewCalendar("weekends only; no Kentucky holiday list is checked in").Build()
	if err != nil {
		panic(err)
	}

	rates, err := estimated.NewRateSeries().
		Period(d(2026, time.January, 1), d(2026, time.December, 31), money.Percent(9), "Form 2210-K (2025), 42A740-S1 (10-25), line 21: 9% for calendar year 2026").
		Build()
	if err != nil {
		panic(err)
	}

	registerEstimated(estimatedYear{
		year:                  2026,
		incomeTest:            5_000 * money.Dollar,
		threshold:             500 * money.Dollar,
		priorYearStep:         150_000 * money.Dollar,
		priorYearStepSeparate: 75_000 * money.Dollar,
		dues: []estimated.Date{
			d(2026, time.April, 15),
			d(2026, time.June, 15),
			d(2026, time.September, 15),
			d(2027, time.January, 15),
		},
		calendar:     calendar,
		lateStart:    d(2026, time.September, 1),
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
