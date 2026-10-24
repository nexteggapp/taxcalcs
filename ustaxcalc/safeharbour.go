package ustaxcalc

import (
	"fmt"
	"time"

	"github.com/nexteggapp/taxcalcs/working"
)

// SafeHarbour is the total that has to be paid across the year to avoid the
// § 6654 underpayment penalty, and which of the two tests produced it.
type SafeHarbour struct {
	// Required is the lesser of the two figures below, which is what § 6654(d)(1)(B)
	// asks for.
	Required USCents

	// CurrentYearTest is 90% of this year's tax.
	CurrentYearTest USCents

	// PriorYearTest is 100% of last year's tax, or 110% where last year's
	// adjusted gross income passed the § 6654(d)(1)(C) step.
	PriorYearTest USCents

	// PriorYearRate is the percentage the prior year test used, so a caller can
	// state why it was one or the other.
	PriorYearRate Rate
}

// RequiredAnnualPayment returns what § 6654 requires be paid across the year.
//
// currentYearTax is the estimate of this year's liability, which moves as the
// year goes on. priorYearTax and priorYearAGI come from the return filed for
// last year and do not move at all. The prior year test is the one worth
// leaning on for that reason: it is a fixed target, and meeting it avoids the
// penalty whatever this year turns out to be.
//
// It does not model the § 6654(d)(2) annualised income instalment method, which
// can lower an instalment for someone whose income arrived unevenly. A taxpayer
// entitled to that pays no more than this says, so the figure is safe in the
// direction that matters.
//
// A prior year that is not a full twelve months, or in which the taxpayer filed
// no return, disqualifies the prior year test under § 6654(d)(1)(B)(ii). Pass
// priorYearFiled false in that case and the current year test is used alone.
func RequiredAnnualPayment(year int, status FilingStatus, currentYearTax, priorYearTax, priorYearAGI USCents, priorYearFiled bool) (Result[SafeHarbour], error) {
	table, err := TableFor(year, status)
	if err != nil {
		return Result[SafeHarbour]{}, err
	}

	harbour := SafeHarbour{CurrentYearTest: Percent(90).Of(currentYearTax)}
	var n working.Notes
	n.Rule("26 U.S.C. § 6654(d)(1)(B)(i)",
		fmt.Sprintf("90%% of this year's estimated tax of %s is %s", currentYearTax, harbour.CurrentYearTest))

	if !priorYearFiled {
		harbour.Required = harbour.CurrentYearTest
		n.Rule("26 U.S.C. § 6654(d)(1)(B)(ii)", "no return was filed for a full prior year, so only the current year test is available")
		return working.NewResult(harbour).With(n).Build()
	}

	harbour.PriorYearRate = Percent(100)
	if priorYearAGI > table.PriorYearAGIStep {
		harbour.PriorYearRate = Percent(110)
	}
	harbour.PriorYearTest = harbour.PriorYearRate.Of(priorYearTax)

	harbour.Required = harbour.CurrentYearTest
	if harbour.PriorYearTest < harbour.Required {
		harbour.Required = harbour.PriorYearTest
	}

	n.Rule("26 U.S.C. § 6654(d)(1)(C)(i)",
		fmt.Sprintf("last year's adjusted gross income of %s against the %s step makes the prior year test %s of %s, or %s",
			priorYearAGI, table.PriorYearAGIStep, harbour.PriorYearRate, priorYearTax, harbour.PriorYearTest))
	n.Rule("26 U.S.C. § 6654(d)(1)(B)", fmt.Sprintf("the lesser of the two tests is %s", harbour.Required))

	return working.NewResult(harbour).With(n).Build()
}

// Instalment is one quarterly payment: when it is due, and what the running
// total has to reach by then.
type Instalment struct {
	Due time.Time

	// CumulativeTarget is what must have been paid by this date, counting
	// everything paid for earlier instalments. § 6654(d)(1)(A) makes each
	// instalment 25% of the required annual payment, and the test is applied to
	// the running total rather than to one quarter in isolation.
	CumulativeTarget USCents
}

// Instalments returns the four § 6654(c) payment dates and the running total
// due by each.
//
// The dates are the statutory April 15, June 15 and September 15 of the tax
// year and January 15 of the next. They are not moved for a weekend or a public
// holiday, which § 7503 does do. A caller rendering a deadline should apply that
// shift itself, because which days are holidays is not something this package
// should carry a calendar for.
func Instalments(year int, requiredAnnual USCents) []Instalment {
	dates := []time.Time{
		time.Date(year, time.April, 15, 0, 0, 0, 0, time.UTC),
		time.Date(year, time.June, 15, 0, 0, 0, 0, time.UTC),
		time.Date(year, time.September, 15, 0, 0, 0, 0, time.UTC),
		time.Date(year+1, time.January, 15, 0, 0, 0, 0, time.UTC),
	}

	out := make([]Instalment, len(dates))
	for i, due := range dates {
		// Computed from the annual figure rather than by accumulating a
		// quarter, so rounding cannot make the fourth target miss the total.
		out[i] = Instalment{
			Due:              due,
			CumulativeTarget: Rate(int32(2500 * (i + 1))).Of(requiredAnnual),
		}
	}
	return out
}

// WithholdingCreditedBy returns how much of a year's withholding counts toward
// the target at one instalment date.
//
// § 6654(g) treats tax withheld as paid in equal parts across the four
// instalments however it actually came out, so someone who fixes their W-4 in
// December still gets a quarter of that withholding credited against the April
// instalment. This is why withholding and estimated payments cannot be added up
// the same way.
//
// quarter is 1 through 4. Anything outside that returns zero.
func WithholdingCreditedBy(totalWithholding USCents, quarter int) USCents {
	if quarter < 1 || quarter > 4 {
		return 0
	}
	return Rate(int32(2500 * quarter)).Of(totalWithholding)
}
