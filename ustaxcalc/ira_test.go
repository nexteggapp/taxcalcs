package ustaxcalc_test

import (
	"errors"
	"testing"

	"github.com/nexteggapp/taxcalcs/ustaxcalc"
	"github.com/nexteggapp/taxcalcs/working"
)

// The 2026 figures are Notice 2025-67's, and the arithmetic is § 219(g)(2).
func TestIRAFiguresAreInTheirSources(t *testing.T) {
	mustContain(t, "n-2025-67-ira.txt",
		"is increased from $7,000 to $7,500",
		"is increased from $1,000 to $1,100",
		"have adjusted gross incomes (as defined in section 219(g)(3)(A)) between $81,000 and $91,000",
		"the income phase-out range is between $129,000 and $149,000",
		"is phased out if the couple’s income is between $242,000 and $252,000",
		"remains $0 to $10,000.",
	)
	mustContain(t, "usc26-219g-2024.txt",
		"(ii) $10,000 ($20,000 in the case of a joint return).",
		"No dollar limitation shall be reduced below $200 under paragraph (1) unless (without regard to this subparagraph) such limitation is reduced to zero.",
		"Any amount determined under this paragraph which is not a multiple of $10 shall be rounded to the next lowest $10.",
		"(B) the amount applicable under paragraph (2)(A)(ii) shall be $10,000.",
	)
}

func iraDeduction(t *testing.T, status ustaxcalc.FilingStatus, b *ustaxcalc.IRAContributionBuilder, magi ustaxcalc.USCents) working.Result[ustaxcalc.USCents] {
	t.Helper()
	c, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	r, err := ustaxcalc.IRADeduction(2026, status, c, magi)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// Worked by hand from § 219(g)(2): the reduction is the limit times the
// income over the range's start, divided by its width, rounded down to $10,
// and leaves at least $200 until it is the whole limit.
func TestIRADeductionPhasesOut(t *testing.T) {
	const d = ustaxcalc.Dollar
	covered := func(amount ustaxcalc.USCents) *ustaxcalc.IRAContributionBuilder {
		return ustaxcalc.NewIRAContribution(amount).ActiveParticipant(true).SpouseActiveParticipant(false)
	}
	for _, tc := range []struct {
		name   string
		status ustaxcalc.FilingStatus
		c      *ustaxcalc.IRAContributionBuilder
		magi   ustaxcalc.USCents
		want   ustaxcalc.USCents
	}{
		// A household with $153,859 of AGI after a $7,500 deduction
		// is well past $91,000.
		{"covered, single, $161,359", ustaxcalc.Single, covered(7_500 * d), 161_359 * d, 0},
		{"covered, single, at the start", ustaxcalc.Single, covered(7_500 * d), 81_000 * d, 7_500 * d},
		// $1 over: a reduction of 75 cents rounds down to $0.
		{"covered, single, $1 over", ustaxcalc.Single, covered(7_500 * d), 81_001 * d, 7_500 * d},
		// $4,000 into $10,000 is 40% of $7,500, $3,000 off.
		{"covered, single, $85,000", ustaxcalc.Single, covered(7_500 * d), 85_000 * d, 4_500 * d},
		// $9,950 in is $7,462.50, rounded down to $7,460, leaving $40,
		// which the $200 floor raises.
		{"covered, single, $90,950", ustaxcalc.Single, covered(7_500 * d), 90_950 * d, 200 * d},
		{"covered, single, at the end", ustaxcalc.Single, covered(7_500 * d), 91_000 * d, 0},
		// A smaller contribution is deductible up to the reduced limit.
		{"covered, single, $2,000 put in", ustaxcalc.Single, covered(2_000 * d), 85_000 * d, 2_000 * d},
		// 50 or older: $8,600, and $5,000 in is half of it off.
		{"covered, 50 or older", ustaxcalc.Single, covered(8_600 * d).CatchUp(), 86_000 * d, 4_300 * d},
		// Joint, the contributor covered: $10,000 into $20,000.
		{"covered, joint", ustaxcalc.MarriedFilingJointly, covered(7_500 * d), 139_000 * d, 3_750 * d},
		// Joint, only the spouse covered: $5,000 into $10,000 from $242,000.
		{"spouse covered, joint", ustaxcalc.MarriedFilingJointly,
			ustaxcalc.NewIRAContribution(7_500 * d).ActiveParticipant(false).SpouseActiveParticipant(true), 247_000 * d, 3_750 * d},
		// Separate, covered: $0 to $10,000.
		{"covered, separate", ustaxcalc.MarriedFilingSeparately, covered(7_500 * d), 5_000 * d, 3_750 * d},
		// Separate and living apart all year counts as single.
		{"covered, separate, lived apart", ustaxcalc.MarriedFilingSeparately, covered(7_500 * d).LivedApart(), 85_000 * d, 4_500 * d},
		// Nobody covered: the whole contribution, up to the limit.
		{"not covered", ustaxcalc.Single, ustaxcalc.NewIRAContribution(9_000 * d).ActiveParticipant(false), 500_000 * d, 7_500 * d},
	} {
		if got := iraDeduction(t, tc.status, tc.c, tc.magi).Value(); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestIRADeductionAsksAboutCoverage(t *testing.T) {
	r := iraDeduction(t, ustaxcalc.MarriedFilingJointly, ustaxcalc.NewIRAContribution(7_500*ustaxcalc.Dollar), 200_000*ustaxcalc.Dollar)
	if r.Value() != 7_500*ustaxcalc.Dollar || len(r.Required()) != 2 {
		t.Errorf("%v with %d questions, want the full $7,500 and two questions", r.Value(), len(r.Required()))
	}
	for _, q := range r.Required() {
		if q.Position() != working.PositionIRADeductibility || q.Question() == "" || q.Effect() == "" {
			t.Errorf("%+v", q)
		}
	}
	c, _ := ustaxcalc.NewIRAContribution(1).Build()
	if _, err := ustaxcalc.IRADeduction(2025, ustaxcalc.Single, c, 0); !errors.Is(err, ustaxcalc.ErrNoTable) {
		t.Errorf("2025: %v, want ErrNoTable", err)
	}
	if _, err := ustaxcalc.NewIRAContribution(-1).Build(); err == nil {
		t.Error("a negative contribution was accepted")
	}
}

// The Roth ranges are Notice 2025-67's; the shared limit, the arithmetic and
// the 6% tax are §§ 408A(c) and 4973.
func TestRothFiguresAreInTheirSources(t *testing.T) {
	mustContain(t, "n-2025-67-roth.txt",
		"joint return or for taxpayers filing as a qualifying widow(er) is increased",
		"from $236,000 to $242,000.",
		"making contributions to a Roth IRA is between $242,000 and $252,000 for",
		"between $153,000 and $168,000, increased from between $150,000",
		"between $0 and $10,000.",
	)
	mustContain(t, "usc26-408a-c-2024.txt",
		"No deduction shall be allowed under section 219 for a contribution to a Roth IRA.",
		"(A) the maximum amount allowable as a deduction under section 219 with respect to such individual for such taxable year (computed without regard to subsection (g) of such section), over",
		"(ii) $15,000 ($10,000 in the case of a joint return or a married individual filing a separate return).",
		"The rules of subparagraphs (B) and (C) of section 219(g)(2) shall apply to any reduction under this subparagraph.",
		"Section 219(g)(4) shall apply for purposes of this paragraph.",
	)
	mustContain(t, "usc26-4973-2024.txt",
		"there is imposed for each taxable year a tax in an amount equal to 6 percent of the amount of the excess contributions",
		"(B) the amount allowable as a deduction under section 219 for such contributions, and",
		"For purposes of paragraphs (1)(B) and (2)(C), the amount allowable as a deduction under section 219 shall be computed without regard to section 219(g).",
		"(B) the amount allowable as a contribution under sections 408A(c)(2) and (c)(3), and",
	)
}

// The 6% tax is in total tax but not in Form 2210's tax: Schedule 2 line 8
// takes it from Form 5329, and Form 2210 line 2 takes line 8 for
// distributions only.
func TestExcessTaxIsInTotalTaxNotForm2210(t *testing.T) {
	mustContain(t, "f5329-2025-excess.txt",
		"17 Additional tax. Enter 6% (0.06) of the smaller of line 16 or the value of your traditional IRAs on December 31,",
		"25 Additional tax. Enter 6% (0.06) of the smaller of line 24 or the value of your Roth IRAs on December 31, 2025",
		"(including 2025 contributions made in 2026). Include this amount on Schedule 2 (Form 1040), line 8",
	)
	mustContain(t, "f1040s2-2025-lines-8-21.txt",
		"Additional tax on IRAs or other tax-favored accounts. Attach Form 5329 if required.",
		"Add lines 4, 7 through 16, 18, and 19. These are your total other taxes. Enter here and on Form 1040",
	)
	mustContain(t, "f1040-2025-lines-23-24.txt",
		"Other taxes, including self-employment tax, from Schedule 2, line 21",
		"Add lines 22 and 23. This is your total tax",
	)
	mustContain(t, "i2210-2025-line-2.txt", "Line 8 (additional tax on distributions only),")
}

func iraLimits(t *testing.T, status ustaxcalc.FilingStatus, b *ustaxcalc.IRAContributionBuilder, magi ustaxcalc.USCents) working.Result[ustaxcalc.IRALimits] {
	t.Helper()
	c, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	r, err := ustaxcalc.IRAContributionLimits(2026, status, c, magi)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// Worked by hand from § 408A(c)(3)(A) over the notice's ranges: the Roth
// limit falls in proportion across the range, the reduction rounds down to
// $10, and it leaves at least $200 until the limit is gone.
func TestRothLimitPhasesOut(t *testing.T) {
	const d = ustaxcalc.Dollar
	roth := func() *ustaxcalc.IRAContributionBuilder { return ustaxcalc.NewIRAContribution(0) }
	for _, tc := range []struct {
		name   string
		status ustaxcalc.FilingStatus
		c      *ustaxcalc.IRAContributionBuilder
		magi   ustaxcalc.USCents
		want   ustaxcalc.USCents
	}{
		{"single, at the start", ustaxcalc.Single, roth(), 153_000 * d, 7_500 * d},
		// Halfway through $153,000 to $168,000 halves the limit.
		{"single, halfway", ustaxcalc.Single, roth(), 160_500 * d, 3_750 * d},
		{"head of household, halfway", ustaxcalc.HeadOfHousehold, roth(), 160_500 * d, 3_750 * d},
		// $1,234 in is $617, rounded down to $610.
		{"single, $154,234", ustaxcalc.Single, roth(), 154_234 * d, 6_890 * d},
		// $14,900 in is $7,450 off, leaving $50, which the floor raises.
		{"single, $167,900", ustaxcalc.Single, roth(), 167_900 * d, 200 * d},
		{"single, at the end", ustaxcalc.Single, roth(), 168_000 * d, 0},
		// 50 or older: $8,600, halved.
		{"single, 50 or older", ustaxcalc.Single, roth().CatchUp(), 160_500 * d, 4_300 * d},
		// Joint: $5,000 into $242,000 to $252,000.
		{"joint, halfway", ustaxcalc.MarriedFilingJointly, roth(), 247_000 * d, 3_750 * d},
		{"joint, at the end", ustaxcalc.MarriedFilingJointly, roth(), 252_000 * d, 0},
		// Separate: $0 to $10,000.
		{"separate, halfway", ustaxcalc.MarriedFilingSeparately, roth(), 5_000 * d, 3_750 * d},
		// Living apart all year, § 219(g)(4) counts a separate filer single.
		{"separate, lived apart", ustaxcalc.MarriedFilingSeparately, roth().LivedApart(), 160_500 * d, 3_750 * d},
		// The joint start over the $15,000 width: $242,000 to $257,000.
		{"surviving spouse, halfway", ustaxcalc.SurvivingSpouse, roth(), 249_500 * d, 3_750 * d},
	} {
		if got := iraLimits(t, tc.status, tc.c, tc.magi).Value().RothLimit(); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

// The two kinds share the limit; what is over it draws 6%.
func TestIRAContributionsShareOneLimit(t *testing.T) {
	const d = ustaxcalc.Dollar
	for _, tc := range []struct {
		name                                                 string
		c                                                    *ustaxcalc.IRAContributionBuilder
		magi                                                 ustaxcalc.USCents
		combined, allowed, deduction, tradExcess, rothExcess ustaxcalc.USCents
		tax                                                  ustaxcalc.USCents
	}{
		// $5,000 traditional leaves $2,500 of the $7,500 for a Roth;
		// the other $2,500 of Roth is excess, and 6% of it is $150.
		{"both, over", ustaxcalc.NewIRAContribution(5_000 * d).Roth(5_000 * d).ActiveParticipant(false),
			100_000 * d, 7_500 * d, 2_500 * d, 5_000 * d, 0, 2_500 * d, 150 * d},
		{"both, within", ustaxcalc.NewIRAContribution(3_000 * d).Roth(4_500 * d).ActiveParticipant(false),
			100_000 * d, 7_500 * d, 4_500 * d, 3_000 * d, 0, 0, 0},
		// Traditional over the limit: $500 excess, $30.
		{"traditional, over", ustaxcalc.NewIRAContribution(8_000 * d).ActiveParticipant(false),
			100_000 * d, 7_500 * d, 0, 7_500 * d, 500 * d, 0, 30 * d},
		// Covered and past § 219(g)'s range: nothing deductible, and not an
		// excess, because § 4973(b) reads § 219 without (g).
		{"traditional, not deductible", ustaxcalc.NewIRAContribution(7_500 * d).ActiveParticipant(true),
			100_000 * d, 7_500 * d, 0, 0, 0, 0, 0},
		// Halfway through the Roth range: $3,750 allowed, $1,250 over, $75.
		{"Roth, phased out", ustaxcalc.NewIRAContribution(0).Roth(5_000 * d),
			160_500 * d, 7_500 * d, 3_750 * d, 0, 0, 1_250 * d, 75 * d},
		// The phased limit binds before what traditional leaves.
		{"both, phased out", ustaxcalc.NewIRAContribution(2_000 * d).Roth(4_000 * d).ActiveParticipant(false),
			160_500 * d, 7_500 * d, 3_750 * d, 2_000 * d, 0, 250 * d, 15 * d},
	} {
		l := iraLimits(t, ustaxcalc.Single, tc.c, tc.magi).Value()
		if l.Combined() != tc.combined || l.RothAllowed() != tc.allowed || l.Deduction() != tc.deduction ||
			l.TraditionalExcess() != tc.tradExcess || l.RothExcess() != tc.rothExcess || l.ExcessTax() != tc.tax {
			t.Errorf("%s: combined %v, Roth allowed %v, deduction %v, excess %v and %v, tax %v; want %v, %v, %v, %v and %v, %v",
				tc.name, l.Combined(), l.RothAllowed(), l.Deduction(), l.TraditionalExcess(), l.RothExcess(), l.ExcessTax(),
				tc.combined, tc.allowed, tc.deduction, tc.tradExcess, tc.rothExcess, tc.tax)
		}
	}
}

// A Roth contribution changes no deduction, and asks nothing about plans at
// work.
func TestRothChangesNoDeduction(t *testing.T) {
	const d = ustaxcalc.Dollar
	r := iraLimits(t, ustaxcalc.MarriedFilingJointly, ustaxcalc.NewIRAContribution(0).Roth(7_500*d), 200_000*d)
	if r.Value().Deduction() != 0 || len(r.Required()) != 0 {
		t.Errorf("deduction %v with %d questions, want none", r.Value().Deduction(), len(r.Required()))
	}
	with := iraLimits(t, ustaxcalc.Single, ustaxcalc.NewIRAContribution(4_000*d).Roth(3_000*d).ActiveParticipant(true), 85_000*d)
	without := iraDeduction(t, ustaxcalc.Single, ustaxcalc.NewIRAContribution(4_000*d).ActiveParticipant(true), 85_000*d)
	if with.Value().Deduction() != without.Value() {
		t.Errorf("deduction %v with a Roth, %v without", with.Value().Deduction(), without.Value())
	}
	if _, err := ustaxcalc.NewIRAContribution(0).Roth(-1).Build(); err == nil {
		t.Error("a negative Roth contribution was accepted")
	}
	c, _ := ustaxcalc.NewIRAContribution(0).Build()
	if _, err := ustaxcalc.IRAContributionLimits(2025, ustaxcalc.Single, c, 0); !errors.Is(err, ustaxcalc.ErrNoTable) {
		t.Errorf("2025: %v, want ErrNoTable", err)
	}
}
