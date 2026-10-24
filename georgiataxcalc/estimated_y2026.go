package georgiataxcalc

import (
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
)

// Georgia estimated tax for tax year 2026.
//
// Every figure here is checked by estimated_test.go against an extract in
// testdata/, listed in testdata/SOURCES.md:
//
//   - the 70% and 100% tests, the equal installments, the late-start chart,
//     withholding treated as paid equally, Exceptions 1 to 3 and their 70% and
//     90%, the January 31 rule, the April 15 stop and the 9% rate: Form 500
//     UET (Rev. 07/15/25), "For tax years 2025 and later";
//   - the due dates, the weekend and holiday rule and the requirement test
//     with its $1,000: Form 500-ES (Rev. 05/12/25), which the Department
//     lists as the 2026 voucher;
//   - the 4.99% rate, the $15,000 and $30,000 standard deductions and the
//     $5,000 dependent exemption: HB 463 (2026), applicable to taxable years
//     beginning on or after January 1, 2026. The 500-ES still prints the 2025
//     figures, $12,000, $24,000 and $4,000, and the divergence says so.
//
// The 9% is fixed by the form rather than published as a series, so the rate
// runs to the April 15, 2027 stop and no day goes unrated.
//
// No Georgia holiday list is checked in. The four 2026 due dates fall on a
// Wednesday, a Monday, a Tuesday and a Friday, and the test checks the
// computed dates against the ones the 500-ES prints.

func init() {
	d := estimated.MustDate

	calendar, err := estimated.NewCalendar("weekends only; no Georgia holiday list is checked in").Build()
	if err != nil {
		panic(err)
	}
	rates, err := estimated.NewRateSeries().
		Period(d(2026, time.January, 1), d(2027, time.April, 15), money.Percent(9),
			"Form 500 UET (Rev. 07/15/25), line 20: 9 percent a year").
		Build()
	if err != nil {
		panic(err)
	}

	registerEstimated(estimatedYear{
		year:           2026,
		rate:           499,
		thisYearShare:  money.Percent(70),
		priorYearShare: money.Percent(100),
		unwithheld:     1_000 * money.Dollar,
		exemption:      5_000 * money.Dollar,
		standard: map[filing.Status]money.USCents{
			filing.Single:                  15_000 * money.Dollar,
			filing.HeadOfHousehold:         15_000 * money.Dollar,
			filing.MarriedFilingSeparately: 15_000 * money.Dollar,
			filing.MarriedFilingJointly:    30_000 * money.Dollar,
		},
		formExemption: 4_000 * money.Dollar,
		formStandard: map[filing.Status]money.USCents{
			filing.Single:                  12_000 * money.Dollar,
			filing.HeadOfHousehold:         12_000 * money.Dollar,
			filing.MarriedFilingSeparately: 12_000 * money.Dollar,
			filing.MarriedFilingJointly:    24_000 * money.Dollar,
		},
		dues: []estimated.Date{
			d(2026, time.April, 15),
			d(2026, time.June, 15),
			d(2026, time.September, 15),
			d(2027, time.January, 15),
		},
		laterGroups: []estimated.Date{
			d(2026, time.April, 1),
			d(2026, time.June, 1),
			d(2026, time.September, 1),
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
		annualisedShare: money.Percent(70),
		actualShare:     money.Percent(90),
		rates:           rates,
	})
}
