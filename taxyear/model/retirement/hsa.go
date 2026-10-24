package retirement

import (
	"fmt"

	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
)

// hsaAnnual is § 223(b)(2)'s annual limitation by year, self-only and
// family: Rev. Proc. 2024-25 § 2.01(1) for 2025, Rev. Proc. 2025-19
// § 2.01(1) for 2026.
var hsaAnnual = map[int][2]money.USCents{
	2025: {money.Dollars(4_300), money.Dollars(8_550)},
	2026: {money.Dollars(4_400), money.Dollars(8_750)},
}

// hsaCatchUp is § 223(b)(3)(B)'s additional contribution amount for 2009
// and after, for someone 55 or older by the year's end.
var hsaCatchUp = money.Dollars(1_000)

// LastMonthRule is the catalogue's question for § 223(b)(8): was the
// person an eligible individual on December 1, and will they stay one
// through the testing period, to December 31 of next year?
const LastMonthRule = "hsa.last_month_rule"

// HsaLimit is a person's HSA limit for the year, § 223(b)(1): the monthly
// limitations, each a twelfth of the annual one with the catch-up, for the
// months they had the coverage, months unsaid being the whole year. For
// someone covered part of the year, the last-month rule, § 223(b)(8),
// counts every month when they were covered on December 1 and stay covered
// through the testing period; that is asked, and until it is answered yes
// the limit counts the months covered. No HSA-eligible plan is a limit of
// nothing. False while the coverage is unsaid, or without the year's
// figures.
func HsaLimit(ctx *model.Context, person string, notes *model.Notes) (money.USCents, bool) {
	p := ctx.Person(person)
	age, aged := ctx.AgeAtYearEnd(person)
	months := int(p.GetHsaMonths())
	if months <= 0 || months > 12 {
		months = 12
	}
	if months < 12 && p.GetHsaCoverage() != factsv1.HsaCoverage_HSA_COVERAGE_NONE && p.GetHsaCoverage() != factsv1.HsaCoverage_HSA_COVERAGE_UNSPECIFIED {
		if lastMonth(ctx, person, notes) {
			months = 12
		}
	}
	return limitFor(ctx.Year(), p.GetHsaCoverage(), months, age, aged)
}

func limitFor(year int, coverage factsv1.HsaCoverage, months, age int, aged bool) (money.USCents, bool) {
	annual, ok := hsaAnnual[year]
	if !ok {
		return 0, false
	}
	var limit money.USCents
	switch coverage {
	case factsv1.HsaCoverage_HSA_COVERAGE_NONE:
		// Without an HSA-eligible health plan nobody is an eligible
		// individual, § 223(c)(1), so the limit is nothing.
		return 0, true
	case factsv1.HsaCoverage_HSA_COVERAGE_SELF_ONLY:
		limit = annual[0]
	case factsv1.HsaCoverage_HSA_COVERAGE_FAMILY:
		limit = annual[1]
	default:
		return 0, false
	}
	if aged && age >= 55 {
		limit += hsaCatchUp
	}
	return limit * money.USCents(months) / 12, true
}

// lastMonth asks the last-month rule's question about the person and
// reports whether it is answered yes.
func lastMonth(ctx *model.Context, person string, notes *model.Notes) bool {
	name := ctx.Person(person).GetName()
	if name == "" {
		name = "you"
	}
	next := ctx.Year() + 1
	notes.Ask(model.NewQuestion(LastMonthRule+"."+person, "summary.retirement["+person+"].hsa_limit",
		fmt.Sprintf("Was %s covered by an HSA-eligible health plan on December 1, %d, and will that coverage last through December 31, %d?", name, ctx.Year(), next),
		fmt.Sprintf("Until it is answered yes, the HSA limit counts the months covered. Yes counts the whole year, under the last-month rule; leaving the coverage before the end of %d would put the extra in that year's income, with a 10%% additional tax.", next),
		"facts.positions", "Yes", "No", "Not sure"))
	for _, pos := range ctx.Facts().GetPositions() {
		if pos.GetQuestion() != LastMonthRule {
			continue
		}
		for _, s := range pos.GetSubjects() {
			if s == person {
				return pos.GetReply() == factsv1.Reply_REPLY_YES
			}
		}
	}
	return false
}
