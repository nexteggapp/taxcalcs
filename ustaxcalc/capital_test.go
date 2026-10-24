package ustaxcalc_test

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/ustaxcalc"
	"github.com/nexteggapp/taxcalcs/working"
)

// The loss limit, the netting lines and the worksheet's first lines, found in
// the IRS's own documents.
func TestCapitalRulesAreInTheirSources(t *testing.T) {
	mustContain(t, "f1040sd-2025-lines.txt",
		"7 Net short-term capital gain or (loss). Combine lines 1a through 6 in column (h).",
		"15 Net long-term capital gain or (loss). Combine lines 8a through 14 in column (h).",
		"16 Combine lines 7 and 15 and enter the result",
		"21 If line 16 is a loss, enter here and on Form 1040, 1040-SR, or 1040-NR, line 7a, the smaller of:")
	mustContain(t, "i1040sd-2025-capital-losses.txt",
		"You can deduct capital losses up to the amount of your capital gains plus $3,000 ($1,500 if married filing separately).")
	mustContain(t, "i1040-2025-qdcg-worksheet.txt",
		"Enter the smaller of line 15 or line 16 of Schedule D. If either line 15 or line 16 is blank or a loss, enter -0-.",
		"4. Add lines 2 and 3",
		"5. Subtract line 4 from line 1. If zero or less, enter -0-",
		"10. Enter the smaller of line 1 or line 4")
	mustContain(t, "i1040-2025-standard-deduction.txt", "You were born before January 2, 1961.")
}

func gains(t *testing.T, b *ustaxcalc.CapitalGainsBuilder) ustaxcalc.CapitalGains {
	t.Helper()
	g, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	return g
}

// The limit the code applies is the one Schedule D line 21 prints.
func TestCapitalLossLimitIsScheduleDs(t *testing.T) {
	m := regexp.MustCompile(`\(\$([0-9,]+)\), or if married filing separately, \(\$([0-9,]+)\)`).FindStringSubmatch(extract(t, "f1040sd-2025-lines.txt"))
	if m == nil {
		t.Fatal("line 21's limits are not in the extract")
	}
	limit := func(s string) ustaxcalc.USCents {
		n, err := strconv.Atoi(strings.ReplaceAll(s, ",", ""))
		if err != nil {
			t.Fatal(err)
		}
		return ustaxcalc.Dollars(int64(n))
	}
	for _, year := range []int{2025, 2026} {
		for status, want := range map[ustaxcalc.FilingStatus]ustaxcalc.USCents{
			ustaxcalc.Single:                  limit(m[1]),
			ustaxcalc.MarriedFilingJointly:    limit(m[1]),
			ustaxcalc.MarriedFilingSeparately: limit(m[2]),
		} {
			res, err := ustaxcalc.NetCapitalGains(year, status, gains(t, ustaxcalc.NewCapitalGains().ShortTerm(-ustaxcalc.Dollars(10_000)).LongTerm(ustaxcalc.Dollars(2_000))))
			if err != nil {
				t.Fatal(err)
			}
			n := res.Value()
			if n.Combined() != -ustaxcalc.Dollars(8_000) || n.InIncome() != -want || n.NetCapitalGain() != 0 {
				t.Errorf("%d %v: combined %v, in income %v, net capital gain %v; want -$8,000, -%v, 0",
					year, status, n.Combined(), n.InIncome(), n.NetCapitalGain(), want)
			}
		}
	}
}

func TestNetCapitalGains(t *testing.T) {
	d := ustaxcalc.Dollars
	for _, tc := range []struct {
		name                           string
		g                              *ustaxcalc.CapitalGainsBuilder
		short, long, inIncome, ncgWant ustaxcalc.USCents
	}{
		{"a short-term loss against a long-term gain and distributions",
			ustaxcalc.NewCapitalGains().ShortTerm(-d(5_000)).LongTerm(d(12_000)).Distributions(d(1_000)),
			-d(5_000), d(13_000), d(8_000), d(8_000)},
		{"both gains: only the long-term part is net capital gain",
			ustaxcalc.NewCapitalGains().ShortTerm(d(4_000)).LongTerm(d(2_000)),
			d(4_000), d(2_000), d(6_000), d(2_000)},
		{"a long-term loss leaves no net capital gain",
			ustaxcalc.NewCapitalGains().ShortTerm(d(3_000)).LongTerm(-d(1_000)),
			d(3_000), -d(1_000), d(2_000), 0},
		{"carryovers come off their own side",
			ustaxcalc.NewCapitalGains().ShortTerm(d(1_000)).ShortTermCarryover(d(1_500)).LongTerm(d(6_000)).LongTermCarryover(d(500)),
			-d(500), d(5_500), d(5_000), d(5_000)},
		{"a loss within the limit is deducted whole",
			ustaxcalc.NewCapitalGains().ShortTerm(-d(1_200)),
			-d(1_200), 0, -d(1_200), 0},
	} {
		res, err := ustaxcalc.NetCapitalGains(2026, ustaxcalc.Single, gains(t, tc.g))
		if err != nil {
			t.Fatal(err)
		}
		n := res.Value()
		if n.ShortTerm() != tc.short || n.LongTerm() != tc.long || n.InIncome() != tc.inIncome || n.NetCapitalGain() != tc.ncgWant {
			t.Errorf("%s: lines 7 and 15 %v and %v, in income %v, net capital gain %v; want %v, %v, %v, %v",
				tc.name, n.ShortTerm(), n.LongTerm(), n.InIncome(), n.NetCapitalGain(), tc.short, tc.long, tc.inIncome, tc.ncgWant)
		}
		if len(res.Applied()) == 0 {
			t.Errorf("%s: no working", tc.name)
		}
	}
	if _, err := ustaxcalc.NewCapitalGains().LongTermCarryover(-1).Build(); err == nil {
		t.Error("a negative carryover was accepted")
	}
	if _, err := ustaxcalc.NewCapitalGains().Distributions(-1).Build(); err == nil {
		t.Error("negative distributions were accepted")
	}
	if _, err := ustaxcalc.NetCapitalGains(1999, ustaxcalc.Single, ustaxcalc.CapitalGains{}); err == nil {
		t.Error("a year with no table was accepted")
	}
}

// The worksheet is the bands on what is left after the preferential income,
// and the gains rates stacked on top: the same figures the two calculators
// give separately.
func TestTaxOnIncomeIsTheWorksheet(t *testing.T) {
	d := ustaxcalc.Dollars
	res, err := ustaxcalc.TaxOnIncome(2026, ustaxcalc.Single, d(60_000), d(5_000), d(10_000))
	if err != nil {
		t.Fatal(err)
	}
	v := res.Value()
	if v.OrdinaryIncome() != d(45_000) || v.PreferentialIncome() != d(15_000) {
		t.Errorf("ordinary %v and preferential %v, want $45,000 and $15,000", v.OrdinaryIncome(), v.PreferentialIncome())
	}
	ladder, _ := ustaxcalc.OrdinaryIncomeTax(2026, ustaxcalc.Single, d(45_000))
	lt, _ := ustaxcalc.LongTermGainsTax(2026, ustaxcalc.Single, d(45_000), d(15_000))
	if v.Ladder().Total != ladder.Value().Total || v.Gains() != lt.Value() || v.Total() != ladder.Value().Total+lt.Value().Total {
		t.Errorf("total %v, want %v + %v", v.Total(), ladder.Value().Total, lt.Value().Total)
	}
	if !has(res.Required(), working.PositionLotSelection) {
		t.Error("a net capital gain did not ask which lots were sold")
	}

	// Preferential income is limited to taxable income.
	capped, err := ustaxcalc.TaxOnIncome(2026, ustaxcalc.Single, d(10_000), d(20_000), 0)
	if err != nil {
		t.Fatal(err)
	}
	if capped.Value().PreferentialIncome() != d(10_000) || capped.Value().OrdinaryIncome() != 0 || capped.Value().Total() != 0 {
		t.Errorf("preferential %v, ordinary %v, tax %v; want $10,000, 0, 0 at the 0%% rate",
			capped.Value().PreferentialIncome(), capped.Value().OrdinaryIncome(), capped.Value().Total())
	}
	if has(capped.Required(), working.PositionLotSelection) {
		t.Error("qualified dividends alone asked which lots were sold")
	}
}

// Line 25 takes the smaller of the worksheet's tax and the tax on everything
// at the ordinary rates. Between the top of the 0% gains rate and the top of
// the 12% band, a dividend taxed at 15% costs more than it would at 12%.
func TestTaxOnIncomeTakesLine24WhenItIsSmaller(t *testing.T) {
	table, err := ustaxcalc.TableFor(2026, ustaxcalc.Single)
	if err != nil {
		t.Fatal(err)
	}
	var top12 ustaxcalc.USCents
	for i, b := range table.Ordinary {
		if b.Rate == ustaxcalc.Percent(12) {
			top12 = table.Ordinary[i+1].Over
		}
	}
	if top12 <= table.MaxZeroRateAmount {
		t.Skipf("no window: the 12%% band ends at %v and the 0%% rate at %v", top12, table.MaxZeroRateAmount)
	}
	ordinary := table.MaxZeroRateAmount + ustaxcalc.Dollars(50)
	dividends := min(ustaxcalc.Dollars(500), top12-ordinary)
	res, err := ustaxcalc.TaxOnIncome(2026, ustaxcalc.Single, ordinary+dividends, dividends, 0)
	if err != nil {
		t.Fatal(err)
	}
	v := res.Value()
	if v.Total() != v.AllOrdinary() || v.AllOrdinary() >= v.Ladder().Total+v.Gains().Total {
		t.Errorf("total %v, all-ordinary %v, worksheet %v; want the smaller all-ordinary figure", v.Total(), v.AllOrdinary(), v.Ladder().Total+v.Gains().Total)
	}
}

func TestAged65OrOlder(t *testing.T) {
	for _, tc := range []struct {
		born time.Time
		want bool
	}{
		{time.Date(1961, time.January, 1, 0, 0, 0, 0, time.UTC), true},
		{time.Date(1961, time.January, 2, 0, 0, 0, 0, time.UTC), false},
		{time.Date(1960, time.December, 31, 23, 0, 0, 0, time.FixedZone("PST", -8*3600)), true},
		{time.Date(1990, time.June, 1, 0, 0, 0, 0, time.UTC), false},
	} {
		if got := ustaxcalc.Aged65OrOlder(2025, tc.born); got != tc.want {
			t.Errorf("born %v: 65 or older for 2025 is %v, want %v", tc.born.Format("2006-01-02"), got, tc.want)
		}
	}
}
