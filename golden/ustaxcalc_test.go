package golden

// The ustaxcalc vectors: every exported function and method of ustaxcalc
// other than EstimatedTax, whose answers are in estimated/US.json.gz. Each
// file under testdata/ustaxcalc holds the table cases of ustaxcalc's own
// tests and a sweep of filing statuses, years and amounts, with the years
// either side of the tables for the errors.
//
// A case is {"fn": the Go function, "args": its arguments, "out": what it
// returned}. Amounts are integer cents, rates integer basis points, a filing
// status its integer value, dates YYYY-MM-DD. A function returning a
// working.Result is encoded as resultJSON encodes it; an error is {"error":
// its text, "no_table": whether it is ustaxcalc.ErrNoTable}.

import (
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/ustaxcalc"
	"github.com/nexteggapp/taxcalcs/working"
)

// usCases collects one file's cases.
type usCases []any

func (c *usCases) add(fn string, args map[string]any, out any) {
	*c = append(*c, map[string]any{"fn": fn, "args": args, "out": out})
}

func TestUSTaxCalc(t *testing.T) {
	for name, cases := range map[string]func() usCases{
		"tables":      usTableCases,
		"ladder":      usLadderCases,
		"gains":       usGainsCases,
		"capital":     usCapitalCases,
		"deductions":  usDeductionCases,
		"seca":        usSECACases,
		"safeharbour": usSafeHarbourCases,
		"ira":         usIRACases,
		"itemised":    usItemisedCases,
		"additional":  usAdditionalCases,
	} {
		checkGzip(t, "ustaxcalc/"+name+".json.gz", map[string]any{"cases": []any(cases())})
	}
}

// usYears are the years swept: the two carried and one either side.
var usYears = []int{2024, 2025, 2026, 2027}

// usStatuses are every status, the unspecified one included.
var usStatuses = []ustaxcalc.FilingStatus{
	ustaxcalc.FilingStatusUnspecified,
	ustaxcalc.Single,
	ustaxcalc.MarriedFilingJointly,
	ustaxcalc.MarriedFilingSeparately,
	ustaxcalc.HeadOfHousehold,
	ustaxcalc.SurvivingSpouse,
}

func usErr(err error) map[string]any {
	return map[string]any{"error": err.Error(), "no_table": errors.Is(err, ustaxcalc.ErrNoTable)}
}

func usResult[T any](r working.Result[T], err error, value func(T) any) map[string]any {
	if err != nil {
		return usErr(err)
	}
	return resultJSON(r, nil, value)
}

func usCents(a ustaxcalc.USCents) any { return int64(a) }

// usAmounts are the amounts swept, in cents: negative, zero, a cent, a
// dollar, and incomes across every band, with the boundaries of each table
// added by usIncomes.
var usAmounts = []ustaxcalc.USCents{
	-5_000 * ustaxcalc.Dollar, -1, 0, 1, 99, ustaxcalc.Dollar, 399 * ustaxcalc.Dollar, 400 * ustaxcalc.Dollar,
	433_15, 5_000 * ustaxcalc.Dollar, 12_400 * ustaxcalc.Dollar, 20_000 * ustaxcalc.Dollar,
	35_000 * ustaxcalc.Dollar, 49_450 * ustaxcalc.Dollar, 50_000 * ustaxcalc.Dollar, 75_000 * ustaxcalc.Dollar,
	100_000 * ustaxcalc.Dollar, 123_456_78, 150_000 * ustaxcalc.Dollar, 192_275 * ustaxcalc.Dollar,
	250_000 * ustaxcalc.Dollar, 400_000 * ustaxcalc.Dollar, 500_000 * ustaxcalc.Dollar, 640_600 * ustaxcalc.Dollar,
	750_000 * ustaxcalc.Dollar, 1_000_000 * ustaxcalc.Dollar, 5_000_000 * ustaxcalc.Dollar,
}

// usIncomes is usAmounts with each bracket threshold of the table, a cent
// either side of it, and the gains thresholds, when the year has a table.
func usIncomes(year int, status ustaxcalc.FilingStatus) []ustaxcalc.USCents {
	out := append([]ustaxcalc.USCents(nil), usAmounts...)
	table, err := ustaxcalc.TableFor(year, status)
	if err != nil {
		return out
	}
	for _, b := range table.Ordinary[1:] {
		out = append(out, b.Over-1, b.Over, b.Over+1)
	}
	return append(out, table.MaxZeroRateAmount, table.Max15RateAmount, table.StandardDeduction)
}

func usTableJSON(t ustaxcalc.Table) any {
	brackets := []any{}
	for _, b := range t.Ordinary {
		brackets = append(brackets, map[string]any{"over": int64(b.Over), "rate": int32(b.Rate)})
	}
	return map[string]any{
		"year": t.Year, "status": int(t.Status), "ordinary": brackets,
		"standard_deduction": int64(t.StandardDeduction), "additional_aged_blind": int64(t.AdditionalAgedBlind),
		"max_zero_rate_amount": int64(t.MaxZeroRateAmount), "max_15_rate_amount": int64(t.Max15RateAmount),
		"social_security_wage_base":     int64(t.SocialSecurityWageBase),
		"additional_medicare_threshold": int64(t.AdditionalMedicareThreshold),
		"prior_year_agi_step":           int64(t.PriorYearAGIStep),
	}
}

func usTableCases() usCases {
	var c usCases
	for _, year := range usYears {
		for _, s := range usStatuses {
			t, err := ustaxcalc.TableFor(year, s)
			out := usErr0(err)
			if err == nil {
				out = map[string]any{"value": usTableJSON(t)}
			}
			c.add("TableFor", map[string]any{"year": year, "status": int(s)}, out)
		}
	}
	years := ustaxcalc.Years()
	sort.Ints(years)
	ys := []any{}
	for _, y := range years {
		ys = append(ys, y)
	}
	c.add("Years", map[string]any{}, ys)
	for _, d := range []int64{0, 1, -3, 42_581} {
		c.add("Dollars", map[string]any{"d": d}, int64(ustaxcalc.Dollars(d)))
	}
	for _, p := range []int32{0, 1, 22, 110} {
		c.add("Percent", map[string]any{"p": p}, int32(ustaxcalc.Percent(p)))
	}
	return c
}

// usErr0 is usErr for an error that may be nil.
func usErr0(err error) map[string]any {
	if err == nil {
		return nil
	}
	return usErr(err)
}

func usLadderJSON(l ustaxcalc.Ladder) any {
	bands := []any{}
	for _, b := range l.Bands {
		bands = append(bands, map[string]any{
			"rate": int32(b.Rate), "from": int64(b.From), "to": int64(b.To),
			"income": int64(b.Income), "tax": int64(b.Tax),
		})
	}
	return map[string]any{"bands": bands, "total": int64(l.Total), "marginal": int32(l.Marginal),
		"room_in_bracket": int64(l.RoomInBracket)}
}

func usLadderCases() usCases {
	var c usCases
	for _, year := range usYears[:3] {
		for _, s := range usStatuses {
			for _, income := range usIncomes(year, s) {
				r, err := ustaxcalc.OrdinaryIncomeTax(year, s, income)
				c.add("OrdinaryIncomeTax", map[string]any{"year": year, "status": int(s), "income": int64(income)},
					usResult(r, err, usLadderJSON))
			}
		}
	}
	// TestLadderAgainstPublishedBaseAmounts and TestLadderWorkedExample are
	// in the sweep: every threshold of every table, and $192,275.
	for _, p := range [][2]ustaxcalc.USCents{
		{0, 0}, {0, 100}, {1, 1}, {38_744_00, 192_275_00}, {17_570_00, 120_000_00},
		{1, 3}, {2, 3}, {-500, 1_000}, {500, -1_000}, {1_000_000_000_00, 1},
		{12_345_67, 98_765_43},
	} {
		c.add("EffectiveRate", map[string]any{"tax": int64(p[0]), "income": int64(p[1])},
			int32(ustaxcalc.EffectiveRate(p[0], p[1])))
	}
	return c
}

func usGainsJSON(g ustaxcalc.GainsTax) any {
	return map[string]any{"at_zero": int64(g.AtZero), "at_15": int64(g.At15), "at_20": int64(g.At20), "total": int64(g.Total)}
}

func usGainsCases() usCases {
	var c usCases
	d := ustaxcalc.Dollars
	gainsSwept := []ustaxcalc.USCents{-d(1_000), 0, 1, d(1_000), d(8_420), d(10_000), d(50_000), d(500_000)}
	for _, year := range usYears[:3] {
		for _, s := range usStatuses {
			for _, ordinary := range usIncomes(year, s) {
				for _, gain := range gainsSwept {
					r, err := ustaxcalc.LongTermGainsTax(year, s, ordinary, gain)
					c.add("LongTermGainsTax", map[string]any{"year": year, "status": int(s),
						"ordinary": int64(ordinary), "gain": int64(gain)}, usResult(r, err, usGainsJSON))
				}
			}
		}
	}
	// TestGainsStackOnOrdinaryIncome.
	for _, ordinary := range []ustaxcalc.USCents{d(20_000), d(10_000), d(45_000), d(100_000), d(540_000), d(600_000), d(192_275)} {
		r, err := ustaxcalc.LongTermGainsTax(2026, ustaxcalc.Single, ordinary, d(10_000))
		c.add("LongTermGainsTax", map[string]any{"year": 2026, "status": int(ustaxcalc.Single),
			"ordinary": int64(ordinary), "gain": int64(d(10_000))}, usResult(r, err, usGainsJSON))
	}
	return c
}

func usCapitalGainsArgs(g ustaxcalc.CapitalGains) map[string]any {
	return map[string]any{"short_term": int64(g.ShortTerm()), "long_term": int64(g.LongTerm()),
		"short_term_carryover": int64(g.ShortTermCarryover()), "long_term_carryover": int64(g.LongTermCarryover()),
		"distributions": int64(g.Distributions())}
}

func usCapitalNetJSON(n ustaxcalc.CapitalNet) any {
	return map[string]any{"short_term": int64(n.ShortTerm()), "long_term": int64(n.LongTerm()),
		"combined": int64(n.Combined()), "in_income": int64(n.InIncome()), "net_capital_gain": int64(n.NetCapitalGain())}
}

func usTaxWithGainsJSON(t ustaxcalc.TaxWithGains) any {
	return map[string]any{"ordinary_income": int64(t.OrdinaryIncome()), "preferential_income": int64(t.PreferentialIncome()),
		"ladder": usLadderJSON(t.Ladder()), "gains": usGainsJSON(t.Gains()), "all_ordinary": int64(t.AllOrdinary()),
		"total": int64(t.Total())}
}

func usCapitalCases() usCases {
	var c usCases
	d := ustaxcalc.Dollars

	// The builder, and its refusals.
	type gb struct{ st, lt, stc, ltc, dist ustaxcalc.USCents }
	builders := []gb{
		{}, {-d(5_000), d(12_000), 0, 0, d(1_000)}, {d(4_000), d(2_000), 0, 0, 0}, {d(3_000), -d(1_000), 0, 0, 0},
		{d(1_000), d(6_000), d(1_500), d(500), 0}, {-d(1_200), 0, 0, 0, 0}, {-d(10_000), d(2_000), 0, 0, 0},
		{-d(2_000), -d(2_500), d(1_000), d(400), 0}, {0, 0, 0, 0, d(3_000)}, {-d(1), -d(1), 0, 0, 0},
		{d(100), -d(50_000), 0, d(10_000), d(200)},
		{0, 0, -1, 0, 0}, {0, 0, 0, -1, 0}, {0, 0, 0, 0, -1},
	}
	var built []ustaxcalc.CapitalGains
	for _, b := range builders {
		g, err := ustaxcalc.NewCapitalGains().ShortTerm(b.st).LongTerm(b.lt).ShortTermCarryover(b.stc).
			LongTermCarryover(b.ltc).Distributions(b.dist).Build()
		args := map[string]any{"short_term": int64(b.st), "long_term": int64(b.lt), "short_term_carryover": int64(b.stc),
			"long_term_carryover": int64(b.ltc), "distributions": int64(b.dist)}
		if err != nil {
			c.add("CapitalGainsBuilder.Build", args, usErr(err))
			continue
		}
		c.add("CapitalGainsBuilder.Build", args, map[string]any{"value": usCapitalGainsArgs(g)})
		built = append(built, g)
	}
	for _, year := range usYears[:3] {
		for _, s := range usStatuses {
			for _, g := range built {
				r, err := ustaxcalc.NetCapitalGains(year, s, g)
				args := usCapitalGainsArgs(g)
				args["year"], args["status"] = year, int(s)
				c.add("NetCapitalGains", args, usResult(r, err, usCapitalNetJSON))
			}
		}
	}

	// TaxOnIncome, with the worksheet's cases among the sweep.
	pairs := [][2]ustaxcalc.USCents{{0, 0}, {d(5_000), d(10_000)}, {d(20_000), 0}, {0, d(20_000)}, {d(500), 0},
		{d(100_000), d(250_000)}, {-1, 0}, {0, -1}, {1, 1}}
	for _, year := range usYears[:3] {
		for _, s := range usStatuses {
			for _, taxable := range []ustaxcalc.USCents{-d(100), 0, d(10_000), d(49_500), d(50_000), d(60_000),
				d(120_000), d(300_000), d(700_000), d(2_000_000)} {
				for _, p := range pairs {
					r, err := ustaxcalc.TaxOnIncome(year, s, taxable, p[0], p[1])
					c.add("TaxOnIncome", map[string]any{"year": year, "status": int(s), "taxable": int64(taxable),
						"qualified_dividends": int64(p[0]), "net_capital_gain": int64(p[1])}, usResult(r, err, usTaxWithGainsJSON))
				}
			}
		}
	}
	// TestTaxOnIncomeTakesLine24WhenItIsSmaller.
	r, err := ustaxcalc.TaxOnIncome(2026, ustaxcalc.Single, d(49_450)+d(50)+d(500), d(500), 0)
	c.add("TaxOnIncome", map[string]any{"year": 2026, "status": int(ustaxcalc.Single), "taxable": int64(d(50_000)),
		"qualified_dividends": int64(d(500)), "net_capital_gain": int64(0)}, usResult(r, err, usTaxWithGainsJSON))

	// Aged65OrOlder, born on and around the cutoff.
	for _, year := range []int{2025, 2026} {
		for _, born := range []time.Time{
			time.Date(year-65, time.January, 1, 0, 0, 0, 0, time.UTC),
			time.Date(year-64, time.January, 1, 0, 0, 0, 0, time.UTC),
			time.Date(year-64, time.January, 2, 0, 0, 0, 0, time.UTC),
			time.Date(year-65, time.December, 31, 0, 0, 0, 0, time.UTC),
			time.Date(year-64, time.December, 31, 0, 0, 0, 0, time.UTC),
			time.Date(1990, time.June, 1, 0, 0, 0, 0, time.UTC),
			time.Date(1930, time.February, 28, 0, 0, 0, 0, time.UTC),
		} {
			c.add("Aged65OrOlder", map[string]any{"year": year, "born": born.Format("2006-01-02")},
				ustaxcalc.Aged65OrOlder(year, born))
		}
	}
	return c
}

func usDeductionJSON(d ustaxcalc.Deduction) any {
	return map[string]any{"amount": int64(d.Amount), "itemised": d.Itemised, "standard": int64(d.Standard)}
}

func usConditionsArgs(c ustaxcalc.Conditions) map[string]any {
	return map[string]any{"aged_65_or_older": c.Aged65OrOlder, "blind": c.Blind,
		"spouse_aged_65_or_older": c.SpouseAged65OrOlder, "spouse_blind": c.SpouseBlind}
}

func usDeductionCases() usCases {
	var c usCases
	d := ustaxcalc.Dollars
	var conds []ustaxcalc.Conditions
	for i := range 16 {
		conds = append(conds, ustaxcalc.Conditions{Aged65OrOlder: i&1 != 0, Blind: i&2 != 0,
			SpouseAged65OrOlder: i&4 != 0, SpouseBlind: i&8 != 0})
	}
	for _, year := range usYears {
		for _, s := range usStatuses {
			for _, cond := range conds {
				r, err := ustaxcalc.StandardDeduction(year, s, cond)
				args := usConditionsArgs(cond)
				args["year"], args["status"] = year, int(s)
				c.add("StandardDeduction", args, usResult(r, err, usCents))
			}
			for _, cond := range []ustaxcalc.Conditions{conds[0], conds[1], conds[15]} {
				for _, itemised := range []ustaxcalc.USCents{-d(100), 0, d(9_400), d(10_000), d(16_100), d(20_000), d(40_000)} {
					for _, e := range []ustaxcalc.Election{ustaxcalc.ElectLarger, ustaxcalc.ElectStandard, ustaxcalc.ElectItemise} {
						r, err := ustaxcalc.DeductionTaken(year, s, cond, itemised, e)
						args := usConditionsArgs(cond)
						args["year"], args["status"], args["itemised_total"], args["election"] = year, int(s), int64(itemised), int(e)
						c.add("DeductionTaken", args, usResult(r, err, usDeductionJSON))
					}
				}
			}
		}
	}
	return c
}

func usSECAJSON(s ustaxcalc.SECA) any {
	return map[string]any{"net_earnings": int64(s.NetEarnings), "social_security": int64(s.SocialSecurity),
		"medicare": int64(s.Medicare), "additional_medicare": int64(s.AdditionalMedicare), "total": int64(s.Total),
		"above_the_line_deduction": int64(s.AboveTheLineDeduction)}
}

func usSECACases() usCases {
	var c usCases
	d := ustaxcalc.Dollars
	for _, year := range usYears[:3] {
		for _, s := range usStatuses {
			for _, profit := range []ustaxcalc.USCents{-d(1_000), 0, d(400), d(433), 433_14, 433_15, d(10_000), d(50_000),
				d(57_273), d(150_000), d(184_500), d(200_000), d(300_000), d(1_000_000)} {
				for _, w := range [][2]ustaxcalc.USCents{{0, 0}, {d(100_000), d(100_000)}, {d(150_000), d(150_000)},
					{d(176_100), d(180_000)}, {d(184_500), d(184_500)}, {d(200_000), d(200_000)}, {d(184_500), d(300_000)}} {
					r, err := ustaxcalc.SelfEmploymentTax(year, s, profit, w[0], w[1])
					c.add("SelfEmploymentTax", map[string]any{"year": year, "status": int(s), "net_profit": int64(profit),
						"w2_social_security_wages": int64(w[0]), "other_medicare_wages": int64(w[1])}, usResult(r, err, usSECAJSON))
				}
			}
		}
	}
	return c
}

func usHarbourJSON(h ustaxcalc.SafeHarbour) any {
	return map[string]any{"required": int64(h.Required), "current_year_test": int64(h.CurrentYearTest),
		"prior_year_test": int64(h.PriorYearTest), "prior_year_rate": int32(h.PriorYearRate)}
}

func usSafeHarbourCases() usCases {
	var c usCases
	d := ustaxcalc.Dollars
	for _, year := range usYears[:3] {
		for _, s := range usStatuses {
			for _, current := range []ustaxcalc.USCents{0, d(1_000), d(20_000), d(48_169), d(71_253)} {
				for _, prior := range []ustaxcalc.USCents{0, d(2_000), d(39_400), d(42_581)} {
					for _, agi := range []ustaxcalc.USCents{d(30_000), d(75_000), d(75_000) + 1, d(140_000), d(150_000), d(150_000) + 1, d(180_000)} {
						for _, filed := range []bool{true, false} {
							r, err := ustaxcalc.RequiredAnnualPayment(year, s, current, prior, agi, filed)
							c.add("RequiredAnnualPayment", map[string]any{"year": year, "status": int(s),
								"current_year_tax": int64(current), "prior_year_tax": int64(prior),
								"prior_year_agi": int64(agi), "prior_year_filed": filed}, usResult(r, err, usHarbourJSON))
						}
					}
				}
			}
		}
	}
	for _, year := range usYears {
		for _, a := range []ustaxcalc.USCents{0, 1, 3, d(43_340), 1_234_567, -d(100)} {
			out := []any{}
			for _, in := range ustaxcalc.Instalments(year, a) {
				out = append(out, map[string]any{"due": in.Due.Format("2006-01-02"), "cumulative_target": int64(in.CumulativeTarget)})
			}
			c.add("Instalments", map[string]any{"year": year, "required_annual": int64(a)}, out)
		}
	}
	for _, w := range []ustaxcalc.USCents{0, 1, 3, d(41_808), 1_234_567} {
		for q := -1; q <= 5; q++ {
			c.add("WithholdingCreditedBy", map[string]any{"total_withholding": int64(w), "quarter": q},
				int64(ustaxcalc.WithholdingCreditedBy(w, q)))
		}
	}
	return c
}

// usIRA is the arguments to build an IRA contribution. A nil coverage is
// one not given.
type usIRA struct {
	amount, roth           ustaxcalc.USCents
	catchUp, livedApart    bool
	covered, spouseCovered *bool
}

func (a usIRA) args() map[string]any {
	var covered, spouse any
	if a.covered != nil {
		covered = *a.covered
	}
	if a.spouseCovered != nil {
		spouse = *a.spouseCovered
	}
	return map[string]any{"amount": int64(a.amount), "roth": int64(a.roth), "catch_up": a.catchUp,
		"lived_apart": a.livedApart, "covered": covered, "spouse_covered": spouse}
}

func (a usIRA) build() (ustaxcalc.IRAContribution, error) {
	b := ustaxcalc.NewIRAContribution(a.amount).Roth(a.roth)
	if a.catchUp {
		b.CatchUp()
	}
	if a.livedApart {
		b.LivedApart()
	}
	if a.covered != nil {
		b.ActiveParticipant(*a.covered)
	}
	if a.spouseCovered != nil {
		b.SpouseActiveParticipant(*a.spouseCovered)
	}
	return b.Build()
}

func usIRAContributionJSON(c ustaxcalc.IRAContribution) any {
	covered, answered := c.ActiveParticipant()
	spouse, spouseAnswered := c.SpouseActiveParticipant()
	return map[string]any{"amount": int64(c.Amount()), "roth": int64(c.Roth()), "catch_up": c.CatchUp(),
		"covered": covered, "covered_answered": answered, "spouse_covered": spouse, "spouse_answered": spouseAnswered,
		"lived_apart": c.LivedApart()}
}

func usIRALimitsJSON(l ustaxcalc.IRALimits) any {
	return map[string]any{"combined": int64(l.Combined()), "roth_limit": int64(l.RothLimit()),
		"roth_allowed": int64(l.RothAllowed()), "deduction": int64(l.Deduction()),
		"traditional_excess": int64(l.TraditionalExcess()), "roth_excess": int64(l.RothExcess()),
		"excess_tax": int64(l.ExcessTax())}
}

func usIRACases() usCases {
	var c usCases
	d := ustaxcalc.Dollars
	yes, no := true, false
	var contributions []usIRA
	for _, amount := range []ustaxcalc.USCents{0, d(7_500), d(9_000)} {
		for _, roth := range []ustaxcalc.USCents{0, d(4_000)} {
			for _, cov := range []*bool{nil, &yes, &no} {
				for _, sp := range []*bool{nil, &yes, &no} {
					contributions = append(contributions, usIRA{amount: amount, roth: roth, covered: cov, spouseCovered: sp})
				}
			}
		}
	}
	for _, cov := range []*bool{nil, &yes, &no} {
		for _, sp := range []*bool{nil, &yes, &no} {
			contributions = append(contributions, usIRA{amount: d(7_500), roth: d(1_000), catchUp: true, covered: cov, spouseCovered: sp})
		}
	}
	contributions = append(contributions,
		usIRA{amount: d(7_500), covered: &yes, spouseCovered: &no, livedApart: true},
		usIRA{amount: d(7_500), covered: &no, spouseCovered: &yes, livedApart: true},
		usIRA{roth: d(5_000), livedApart: true},
		usIRA{amount: d(8_600), covered: &yes, spouseCovered: &no, catchUp: true},
		usIRA{amount: d(5_000), roth: d(5_000), covered: &no},
		usIRA{amount: d(3_000), roth: d(4_500), covered: &no},
		usIRA{amount: d(8_000), covered: &no},
		usIRA{roth: d(7_500)},
		usIRA{amount: d(4_000), roth: d(3_000), covered: &yes},
		usIRA{amount: -1}, usIRA{roth: -1})
	var built []usIRA
	var builtC []ustaxcalc.IRAContribution
	for _, a := range contributions {
		ira, err := a.build()
		if err != nil {
			c.add("IRAContributionBuilder.Build", a.args(), usErr(err))
			continue
		}
		c.add("IRAContributionBuilder.Build", a.args(), map[string]any{"value": usIRAContributionJSON(ira)})
		built, builtC = append(built, a), append(builtC, ira)
	}
	magis := []ustaxcalc.USCents{0, d(5_000), d(81_001), d(85_000), d(90_950), d(139_000), d(160_500), d(167_900),
		d(247_000), d(249_500), d(500_000)}
	for _, year := range []int{2025, 2026, 2027} {
		for _, s := range usStatuses {
			for i, ira := range builtC {
				for _, magi := range magis {
					if year != 2026 && magi != 0 {
						continue
					}
					args := built[i].args()
					args["year"], args["status"], args["magi"] = year, int(s), int64(magi)
					r, err := ustaxcalc.IRADeduction(year, s, ira, magi)
					c.add("IRADeduction", args, usResult(r, err, usCents))
					l, err := ustaxcalc.IRAContributionLimits(year, s, ira, magi)
					c.add("IRAContributionLimits", args, usResult(l, err, usIRALimitsJSON))
				}
			}
		}
	}
	return c
}

func usItemisedCases() usCases {
	var c usCases
	d := ustaxcalc.Dollars
	for _, year := range usYears {
		for _, s := range usStatuses {
			for _, ic := range [][2]ustaxcalc.USCents{{0, 0}, {d(60_000), d(20_000)}, {d(30_000), d(1_000)},
				{d(30_000), 0}, {1, 2}, {-1, 0}, {d(5_000), -1}, {d(100_000), d(100_000)}} {
				for _, agi := range []ustaxcalc.USCents{-d(1_000), 0, d(200_000), d(300_000), d(384_350), d(400_000),
					d(640_600), d(640_601), d(700_000), d(800_000), d(3_000_000)} {
					r, err := ustaxcalc.ItemisedLimits(year, s, ic[0], ic[1], agi)
					c.add("ItemisedLimits", map[string]any{"year": year, "status": int(s), "itemised": int64(ic[0]),
						"charity": int64(ic[1]), "agi": int64(agi)}, usResult(r, err, usCents))
				}
			}
		}
	}
	return c
}

// usDist is a distribution's arguments.
type usDist struct {
	taxable ustaxcalc.USCents
	code    string
}

func usDistArgs(ds []usDist) []any {
	out := []any{}
	for _, d := range ds {
		out = append(out, map[string]any{"taxable": int64(d.taxable), "code": d.code})
	}
	return out
}

// usStateRefund is a state refund worksheet's arguments.
type usStateRefund struct {
	refund                          ustaxcalc.USCents
	status                          ustaxcalc.FilingStatus
	incomeTax, paid, deducted, item ustaxcalc.USCents
	boxes                           int
	spouseItemized                  bool
}

func (r usStateRefund) args() map[string]any {
	return map[string]any{"refund": int64(r.refund), "status": int(r.status), "prior_income_tax": int64(r.incomeTax),
		"prior_salt_paid": int64(r.paid), "prior_salt_deducted": int64(r.deducted), "prior_itemized": int64(r.item),
		"prior_boxes": r.boxes, "spouse_itemized": r.spouseItemized}
}

func usAdditionalCases() usCases {
	var c usCases
	d := ustaxcalc.Dollar

	// Distributions: the builder, then the functions over lists of them.
	for _, a := range []usDist{{1, "1"}, {0, "G"}, {-1, "1"}, {1, ""}, {8_000 * d, "7"}} {
		r, err := ustaxcalc.NewRetirementDistribution(a.taxable, a.code)
		out := map[string]any{"value": map[string]any{"taxable": int64(r.Taxable()), "code": r.Code()}}
		if err != nil {
			out = usErr(err)
		}
		c.add("NewRetirementDistribution", usDistArgs([]usDist{a})[0].(map[string]any), out)
	}
	lists := [][]usDist{
		{},
		{{8_000 * d, "1"}, {5_000 * d, "7"}},
		{{1, "1"}},
		{{1, "S"}},
		{{1_000 * d, "1"}, {2_000 * d, "2"}, {0, "G"}, {3_000 * d, "P"}, {4_000 * d, "4G"}, {500 * d, "1P"}},
		{{1_000 * d, "1"}, {2_000 * d, "2"}, {0, "G"}, {3_000 * d, "P"}, {4_000 * d, "4G"}, {500 * d, "1P"}, {1, "A"}, {1, "J7"}, {1, "A"}},
		{{12_345, "1"}, {7_77, "B1"}, {100 * d, "3H"}, {9 * d, "D"}, {1, "Q"}, {2, "QD"}},
	}
	for _, list := range lists {
		var ds []ustaxcalc.RetirementDistribution
		for _, a := range list {
			r, err := ustaxcalc.NewRetirementDistribution(a.taxable, a.code)
			if err != nil {
				panic(err)
			}
			ds = append(ds, r)
		}
		args := map[string]any{"distributions": usDistArgs(list)}
		for _, year := range []int{2024, 2025, 2026} {
			r, err := ustaxcalc.EarlyDistributionTax(year, ds)
			c.add("EarlyDistributionTax", map[string]any{"year": year, "distributions": usDistArgs(list)}, usResult(r, err, usCents))
		}
		c.add("EarlyDistributionTaxOther", args, ustaxcalc.EarlyDistributionTaxOther(ds))
		c.add("UnhandledDistributionCodes", args, strs(ustaxcalc.UnhandledDistributionCodes(ds)))
		c.add("DistributionIncome", args, int64(ustaxcalc.DistributionIncome(ds)))
	}
	c.add("HandledDistributionCodes", map[string]any{}, strs(ustaxcalc.HandledDistributionCodes()))

	// RentalLossAllowed, TestRentalLossAllowed's cases among the sweep.
	for _, s := range usStatuses {
		for _, loss := range []ustaxcalc.USCents{-1, 0, 10_000 * d, 30_000 * d} {
			for _, magi := range []ustaxcalc.USCents{-1_000 * d, 0, 20_000 * d, 50_000 * d, 60_000 * d, 90_000 * d,
				100_000 * d, 100_001 * d, 120_000 * d, 150_000 * d, 200_000 * d} {
				for _, apart := range []bool{false, true} {
					r, err := ustaxcalc.RentalLossAllowed(s, loss, magi, apart)
					c.add("RentalLossAllowed", map[string]any{"status": int(s), "loss": int64(loss), "magi": int64(magi),
						"lived_apart": apart}, usResult(r, err, usCents))
				}
			}
		}
	}

	// The state refund worksheet: TestStateRefundWorksheet's cases, the
	// builder's refusals, and each status for the years either side.
	refunds := []usStateRefund{
		{10_000 * d, ustaxcalc.Single, 6_513 * d, 9_000 * d, 9_000 * d, 23_000 * d, 0, false},
		{10_000 * d, ustaxcalc.Single, 6_513 * d, 9_000 * d, 9_000 * d, 17_750 * d, 0, false},
		{10_000 * d, ustaxcalc.Single, 6_513 * d, 9_000 * d, 9_000 * d, 17_750 * d, 1, false},
		{10_000 * d, ustaxcalc.Single, 6_513 * d, 12_000 * d, 10_000 * d, 23_000 * d, 0, false},
		{10_000 * d, ustaxcalc.Single, 6_513 * d, 9_000 * d, 9_000 * d, 0, 0, false},
		{10_000 * d, ustaxcalc.MarriedFilingSeparately, 6_513 * d, 0, 0, 3_000 * d, 0, true},
		{10_000 * d, ustaxcalc.MarriedFilingSeparately, 6_513 * d, 0, 0, 3_000 * d, 0, false},
		{500 * d, ustaxcalc.Single, 6_513 * d, 12_000 * d, 10_000 * d, 23_000 * d, 0, false},
		{3_000 * d, ustaxcalc.MarriedFilingJointly, 8_000 * d, 15_000 * d, 10_000 * d, 45_000 * d, 4, false},
		{3_000 * d, ustaxcalc.HeadOfHousehold, 8_000 * d, 8_000 * d, 8_000 * d, 30_000 * d, 2, false},
		{3_000 * d, ustaxcalc.SurvivingSpouse, 8_000 * d, 8_000 * d, 8_000 * d, 33_000 * d, 1, true},
		{-1, ustaxcalc.Single, 0, 0, 0, 0, 0, false},
		{1, ustaxcalc.Single, 0, 0, 0, -1, 0, false},
		{1, ustaxcalc.Single, 0, 0, 0, 0, 5, false},
		{1, ustaxcalc.Single, 0, 0, 0, 0, -1, false},
		{1, ustaxcalc.Single, 0, 1, 2, 0, 0, false},
		{1, ustaxcalc.FilingStatusUnspecified, 0, 0, 0, 0, 0, false},
	}
	for _, a := range refunds {
		b := ustaxcalc.NewStateRefund(a.refund, a.status).PriorIncomeTax(a.incomeTax).PriorSALT(a.paid, a.deducted).
			PriorItemized(a.item).PriorBoxes(a.boxes)
		if a.spouseItemized {
			b.SpouseItemizedSeparately()
		}
		r, err := b.Build()
		if err != nil {
			c.add("StateRefundBuilder.Build", a.args(), usErr(err))
			continue
		}
		c.add("StateRefundBuilder.Build", a.args(), map[string]any{"value": map[string]any{"refund": int64(r.Refund())}})
		for _, year := range []int{2025, 2026, 2027, 2028} {
			args := a.args()
			args["year"] = year
			res, err := ustaxcalc.StateRefundTaxable(year, r)
			c.add("StateRefundTaxable", args, usResult(res, err, usCents))
		}
	}
	return c
}
