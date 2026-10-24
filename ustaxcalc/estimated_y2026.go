package ustaxcalc

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
//   - the $1,000 threshold, the 90%, 100% and 110% tests, the $150,000 and
//     $75,000 steps and the payment dates: the 2026 Form 1040-ES;
//   - the January 31 file-and-pay date, the April 15 stop and the Schedule AI
//     periods and percentages: the 2025 Form 2210 and its instructions, whose
//     rules are unchanged for 2026 and whose 2026 edition is not yet published;
//   - the legal holidays: Publication 509 (2026);
//   - the underpayment rates: Rev. Rul. 2026-5, 2026-10 and 2026-15.
//
// The rate for January 1 to April 15, 2027 is set by a revenue ruling the IRS
// publishes in November 2026, and it is not out. The series ends on
// December 31, 2026 and the penalty reports the gap rather than assume a rate.
//
// Publication 509 (2027) is not out either, so 2027 has no holidays here.
// The 2027 due dates the 2026 Form 1040-ES prints, January 15 and February 1,
// are weekdays, and the test checks the computed dates against them. The
// April 15, 2027 stop is the 2025 instructions' April 15 a year on; the
// penalty worksheet does not move it for a weekend, so no holiday list is
// needed for it.

func init() {
	calendar, err := estimated.NewCalendar("Publication 509 (2026), Legal holidays").
		Holiday(date(2026, time.January, 1), "New Year's Day").
		Holiday(date(2026, time.January, 19), "Birthday of Martin Luther King, Jr.").
		Holiday(date(2026, time.February, 16), "Washington's Birthday").
		Holiday(date(2026, time.April, 16), "District of Columbia Emancipation Day").
		Holiday(date(2026, time.May, 25), "Memorial Day").
		Holiday(date(2026, time.June, 19), "Juneteenth National Independence Day").
		Holiday(date(2026, time.July, 3), "Independence Day (observed)").
		Holiday(date(2026, time.September, 7), "Labor Day").
		Holiday(date(2026, time.October, 12), "Indigenous Peoples' Day (Columbus Day)").
		Holiday(date(2026, time.November, 11), "Veterans Day").
		Holiday(date(2026, time.November, 26), "Thanksgiving Day").
		Holiday(date(2026, time.December, 25), "Christmas Day").
		Build()
	if err != nil {
		panic(err)
	}

	// § 6621(b): one rate per calendar quarter. The series starts with the
	// quarter the first installment falls in; the first quarter's rate never
	// applies to a 2026 underpayment.
	rates, err := estimated.NewRateSeries().
		Period(date(2026, time.April, 1), date(2026, time.June, 30), money.Percent(6), "Rev. Rul. 2026-5, 2026-8 I.R.B. 542").
		Period(date(2026, time.July, 1), date(2026, time.September, 30), money.Percent(7), "Rev. Rul. 2026-10, 2026-22 I.R.B. 1515").
		Period(date(2026, time.October, 1), date(2026, time.December, 31), money.Percent(7), "Rev. Rul. 2026-15, 2026-36 I.R.B. 216").
		Build()
	if err != nil {
		panic(err)
	}

	registerEstimated(estimatedYear{
		year:         2026,
		threshold:    1_000 * Dollar,
		calendar:     calendar,
		fileAndPayBy: date(2027, time.January, 31),
		stop:         date(2027, time.April, 15),
		periodEnds: []estimated.Date{
			date(2026, time.March, 31),
			date(2026, time.May, 31),
			date(2026, time.August, 31),
			date(2026, time.December, 31),
		},
		rates: rates,
	})
}
