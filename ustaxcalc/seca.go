package ustaxcalc

import (
	"fmt"

	"github.com/nexteggapp/taxcalcs/working"
)

// SECA is self-employment tax, split into the parts that are computed
// differently.
type SECA struct {
	// NetEarnings is net profit reduced under § 1402(a)(12), which is the base
	// the rates below apply to.
	NetEarnings USCents

	// SocialSecurity is the § 1401(a) old age, survivors and disability
	// insurance part, capped by the wage base.
	SocialSecurity USCents

	// Medicare is the § 1401(b)(1) hospital insurance part, which has no cap.
	Medicare USCents

	// AdditionalMedicare is the § 1401(b)(2) extra 0.9% on combined wages and
	// self-employment earnings above the threshold.
	AdditionalMedicare USCents

	// Total is the sum of the three.
	Total USCents

	// AboveTheLineDeduction is the § 164(f) deduction of half the tax. It
	// excludes AdditionalMedicare, which § 164(f)(1) does not reach.
	AboveTheLineDeduction USCents
}

// Rates fixed by § 1401 rather than adjusted for inflation.
const (
	// § 1402(a)(12) reduces net earnings to 92.35% before the rates apply.
	netEarningsFactor = Rate(9235)

	socialSecurityRate     = Rate(1240) // § 1401(a), 12.4%
	medicareRate           = Rate(290)  // § 1401(b)(1), 2.9%
	additionalMedicareRate = Rate(90)   // § 1401(b)(2), 0.9%

	// § 1402(b)(2). Below this, there is no self-employment tax at all.
	minimumNetEarnings = 400 * Dollar
)

// SelfEmploymentTax computes the tax on net earnings from self-employment.
//
// netProfit is profit from the trade or business before this tax. w2SocialSecurityWages
// is the Social Security wages already reported on the taxpayer's Forms W-2,
// which matters because those wages consume the wage base first: someone whose
// salary already exceeded it owes no Social Security part on their
// self-employment earnings at all. Passing zero for a taxpayer who does have
// W-2 wages overstates the tax, and that interaction is the reason this cannot
// be estimated from net profit alone.
//
// otherMedicareWages is wages counting toward the additional Medicare
// threshold, which for most taxpayers is the same figure as
// w2SocialSecurityWages but diverges once the wage base is passed, because
// Medicare wages are uncapped.
func SelfEmploymentTax(year int, status FilingStatus, netProfit, w2SocialSecurityWages, otherMedicareWages USCents) (Result[SECA], error) {
	table, err := TableFor(year, status)
	if err != nil {
		return Result[SECA]{}, err
	}

	const tradeOrBusiness = "self-employment tax reaches a trade or business under § 162 and not a " +
		"hobby under § 183, and the figures alone do not say which this is"

	var seca SECA
	seca.NetEarnings = netEarningsFactor.Of(netProfit)
	if seca.NetEarnings < minimumNetEarnings {
		return working.NewResult(SECA{NetEarnings: seca.NetEarnings}).
			Rule("26 U.S.C. § 1402(b)(2)", fmt.Sprintf("net earnings of %s are under $400, so no self-employment tax is due", seca.NetEarnings)).
			Require(PositionTradeOrBusiness, tradeOrBusiness).
			Build()
	}

	// The wage base is consumed by W-2 wages first. § 1402(b)(1).
	room := table.SocialSecurityWageBase - w2SocialSecurityWages
	if room < 0 {
		room = 0
	}
	ssBase := seca.NetEarnings
	if ssBase > room {
		ssBase = room
	}
	seca.SocialSecurity = socialSecurityRate.Of(ssBase)
	seca.Medicare = medicareRate.Of(seca.NetEarnings)

	// The additional Medicare tax is on the combined figure over the threshold,
	// and wages are counted first.
	combined := seca.NetEarnings + otherMedicareWages
	if over := combined - table.AdditionalMedicareThreshold; over > 0 {
		if over > seca.NetEarnings {
			// The wages alone already passed the threshold, so all of the
			// self-employment earnings are over it. What the wages owe is
			// withheld by the employer and is not this calculator's to report.
			over = seca.NetEarnings
		}
		seca.AdditionalMedicare = additionalMedicareRate.Of(over)
	}

	seca.Total = seca.SocialSecurity + seca.Medicare + seca.AdditionalMedicare
	seca.AboveTheLineDeduction = Percent(50).Of(seca.SocialSecurity + seca.Medicare)

	return working.NewResult(seca).
		Rule("26 U.S.C. § 1402(a)(12)", fmt.Sprintf("%s of net profit reduced to %s of net earnings", netProfit, seca.NetEarnings)).
		Rule("26 U.S.C. § 1401(a)", fmt.Sprintf("12.4%% on %s, being the %s wage base less %s of W-2 Social Security wages, gives %s",
			ssBase, table.SocialSecurityWageBase, w2SocialSecurityWages, seca.SocialSecurity)).
		Rule("26 U.S.C. § 1401(b)(1)", fmt.Sprintf("2.9%% on all %s of net earnings gives %s", seca.NetEarnings, seca.Medicare)).
		Rule("26 U.S.C. § 164(f)", fmt.Sprintf("half of %s is deductible above the line", seca.SocialSecurity+seca.Medicare)).
		Require(PositionTradeOrBusiness, tradeOrBusiness).
		Build()
}
